package serv

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/dosco/graphjin/auth/v3"
	"github.com/dosco/graphjin/core/v3"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type wsReq struct {
	ID      string          `json:"id"`
	Type    string          `json:"type,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type wsRes struct {
	ID      string  `json:"id"`
	Type    string  `json:"type,omitempty"`
	Payload Payload `json:"payload"`
}

type Payload struct {
	Data   json.RawMessage `json:"data,omitempty"`
	Errors []core.Error    `json:"errors,omitempty"`
}

var upgrader = websocket.Upgrader{
	EnableCompression: true,
	ReadBufferSize:    1024,
	WriteBufferSize:   1024,
	HandshakeTimeout:  10 * time.Second,
	Subprotocols:      []string{"graphql-ws", "graphql-transport-ws"},
	CheckOrigin:       func(r *http.Request) bool { return true },
}

var initMsg *websocket.PreparedMessage

func init() {
	msg, err := json.Marshal(wsReq{ID: "1", Type: "connection_ack"})
	if err != nil {
		panic(err)
	}

	initMsg, err = websocket.NewPreparedMessage(websocket.TextMessage, msg)
	if err != nil {
		panic(err)
	}
}

type wsConn struct {
	c        context.Context
	sessions map[string]wsState
	conn     *websocket.Conn
	done     chan bool

	w  http.ResponseWriter
	r  *http.Request
	ah auth.HandlerFunc
}

type wsState struct {
	ID   string
	m    *core.Member
	done chan bool
}

func (s *service) apiV1Ws(w http.ResponseWriter, r *http.Request, ah auth.HandlerFunc) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		renderErr(w, err)
		return
	}
	defer conn.Close()
	conn.SetReadLimit(2048)

	wc := wsConn{
		c:        r.Context(),
		sessions: make(map[string]wsState),
		conn:     conn,
		done:     make(chan bool),
		w:        w,
		r:        r,
		ah:       ah,
	}

	for {
		var b []byte
		var req wsReq

		if _, b, err = conn.ReadMessage(); err != nil {
			break
		}

		if err = json.Unmarshal(b, &req); err != nil {
			break
		}

		if err = s.subSwitch(&wc, req); err != nil {
			break
		}
	}

	if e, ok := err.(*websocket.CloseError); !ok ||
		(e.Code != websocket.CloseNormalClosure && e.Code != websocket.CloseGoingAway) {
		s.zlog.Error("Subscription", []zapcore.Field{zap.Error(err)}...)
	}

	for _, st := range wc.sessions {
		st.m.Unsubscribe()
	}
	wc.done <- true
}

type authHeaders struct {
	UserIDProvider string      `json:"X-User-ID-Provider"`
	UserRole       string      `json:"X-User-Role"`
	UserID         interface{} `json:"X-User-ID"`
}

func (s *service) subSwitch(wc *wsConn, req wsReq) (err error) {
	switch req.Type {
	case "connection_init":
		if err = setHeaders(req, wc.r); err != nil {
			return
		}
		if wc.c, err = wc.ah(wc.w, wc.r); err != nil {
			return
		}
		if s.conf.Serv.AuthFailBlock && !auth.IsAuth(wc.c) {
			err = auth.Err401
			return
		}
		if err = wc.conn.WritePreparedMessage(initMsg); err != nil {
			return
		}

	case "start", "subscribe":
		var p gqlReq
		if err = json.Unmarshal(req.Payload, &p); err != nil {
			break
		}

		c := wc.c
		if s.conf.Serv.Auth.Development {
			var x authHeaders
			if err = json.Unmarshal(p.Vars, &x); err != nil {
				break
			}
			if x.UserIDProvider != "" {
				c = context.WithValue(c, core.UserIDProviderKey, x.UserIDProvider)
			}
			if x.UserRole != "" {
				c = context.WithValue(c, core.UserRoleKey, x.UserRole)
			}
			if x.UserID != nil {
				c = context.WithValue(c, core.UserIDKey, x.UserID)
			}
		}

		st := wsState{ID: req.ID, done: make(chan bool)}
		if st.m, err = s.gj.Subscribe(c, p.Query, p.Vars, nil); err != nil {
			break
		}
		wc.sessions[st.ID] = st
		useNext := req.Type == "subscribe"

		go s.waitForData(wc, &st, useNext)

	case "complete", "connection_terminate", "stop":
		if st, ok := wc.sessions[req.ID]; ok {
			st.done <- true
			st.m.Unsubscribe()
			delete(wc.sessions, req.ID)
		}

	default:
		err = fmt.Errorf("unknown message type: %s", req.Type)
	}

	if err != nil {
		sendError(wc, req.ID, err) //nolint:errcheck
	}
	return
}

func (s *service) waitForData(wc *wsConn, st *wsState, useNext bool) {
	var buf bytes.Buffer

	var ptype string
	var err error

	if useNext {
		ptype = "next"
	} else {
		ptype = "data"
	}

	enc := json.NewEncoder(&buf)
	for {
		select {
		case v := <-st.m.Result:
			res := wsRes{ID: st.ID, Type: ptype}
			res.Payload.Data = v.Data
			res.Payload.Errors = v.Errors

			if err = enc.Encode(res); err != nil {
				break
			}
			msg := buf.Bytes()
			buf.Reset()
			err = wc.conn.WriteMessage(websocket.TextMessage, msg)

		case v := <-st.done:
			if v {
				return
			}

		case v := <-wc.done:
			if v {
				return
			}
		}

		if err != nil {
			s.zlog.Error("Subscription", []zapcore.Field{zap.Error(err)}...)
			sendError(wc, st.ID, err) //nolint:errcheck
			break
		}
	}
}

func setHeaders(req wsReq, r *http.Request) (err error) {
	if len(req.Payload) == 0 {
		return
	}
	var p map[string]interface{}
	if err = json.Unmarshal(req.Payload, &p); err != nil {
		return
	}
	for k, v := range p {
		switch v1 := v.(type) {
		case string:
			r.Header.Set(k, v1)
		case json.Number:
			r.Header.Set(k, v1.String())
		}
	}
	return
}

func sendError(wc *wsConn, id string, cerr error) (err error) {
	m := wsRes{ID: id, Type: "error"}
	m.Payload.Errors = []core.Error{{Message: cerr.Error()}}

	msg, err := json.Marshal(m)
	if err != nil {
		return
	}
	err = wc.conn.WriteMessage(websocket.TextMessage, msg)
	return
}
