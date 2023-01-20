package serv

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	core "github.com/dosco/graphjin/v2/core"
	"github.com/dosco/graphjin/v2/serv/auth"
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

type wsState struct {
	c    context.Context
	conn *websocket.Conn
	req  wsReq
	ah   auth.HandlerFunc
	exit bool
	done chan bool

	w http.ResponseWriter
	r *http.Request
}

func (s *service) apiV1Ws(w http.ResponseWriter, r *http.Request, ah auth.HandlerFunc) {
	var m *core.Member
	var err error

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		renderErr(w, err)
		return
	}
	defer conn.Close()
	conn.SetReadLimit(2048)

	st := wsState{
		c:    r.Context(),
		done: make(chan bool),
		conn: conn,
		ah:   ah,
		w:    w,
		r:    r,
	}

	for {
		var b []byte

		if _, b, err = conn.ReadMessage(); err != nil {
			break
		}

		if err = json.Unmarshal(b, &st.req); err != nil {
			break
		}

		if err = s.subSwitch(&st); err != nil {
			break
		}

		if st.exit {
			break
		}
	}

	if err != nil {
		s.zlog.Error("Subscription", []zapcore.Field{zap.Error(err)}...)
		sendError(&st, err) //nolint:errcheck
	}

	m.Unsubscribe()
	st.done <- true
}

type authHeaders struct {
	UserIDProvider string      `json:"X-User-ID-Provider"`
	UserRole       string      `json:"X-User-Role"`
	UserID         interface{} `json:"X-User-ID"`
}

func (s *service) subSwitch(st *wsState) (err error) {
	switch st.req.Type {
	case "connection_init":
		if err = setHeaders(st); err != nil {
			return
		}
		if st.c, err = st.ah(st.w, st.r); err != nil {
			return
		}
		if s.conf.Serv.AuthFailBlock && !auth.IsAuth(st.c) {
			err = auth.Err401
			return
		}
		if err = st.conn.WritePreparedMessage(initMsg); err != nil {
			return
		}

	case "start", "subscribe":
		var p gqlReq
		if err = json.Unmarshal(st.req.Payload, &p); err != nil {
			return
		}

		if s.conf.Serv.Auth.Development {
			var x authHeaders
			if err = json.Unmarshal(p.Vars, &x); err != nil {
				return
			}
			if x.UserIDProvider != "" {
				st.c = context.WithValue(st.c, core.UserIDProviderKey, x.UserIDProvider)
			}
			if x.UserRole != "" {
				st.c = context.WithValue(st.c, core.UserRoleKey, x.UserRole)
			}
			if x.UserID != nil {
				st.c = context.WithValue(st.c, core.UserIDKey, x.UserID)
			}
		}

		var m *core.Member
		if m, err = s.gj.Subscribe(st.c, p.Query, p.Vars, nil); err != nil {
			return
		}
		go s.waitForData(st, m)
		return

	case "complete", "connection_terminate", "stop":
		st.exit = true

	default:
		err = fmt.Errorf("unknown message type: %s", st.req.Type)
	}
	return
}

func (s *service) waitForData(st *wsState, m *core.Member) {
	var buf bytes.Buffer

	var ptype string
	var err error

	if st.req.Type == "subscribe" {
		ptype = "next"
	} else {
		ptype = "data"
	}

	enc := json.NewEncoder(&buf)
	for {
		select {
		case v := <-m.Result:
			m := wsRes{ID: st.req.ID, Type: ptype}
			m.Payload.Data = v.Data
			m.Payload.Errors = v.Errors

			if err = enc.Encode(m); err != nil {
				break
			}
			msg := buf.Bytes()
			buf.Reset()
			err = st.conn.WriteMessage(websocket.TextMessage, msg)

		case v := <-st.done:
			if v {
				return
			}
		}

		if err != nil {
			s.zlog.Error("Subscription", []zapcore.Field{zap.Error(err)}...)
			sendError(st, err) //nolint:errcheck
			break
		}
	}
}

func setHeaders(st *wsState) (err error) {
	if len(st.req.Payload) == 0 {
		return
	}
	var p map[string]interface{}
	if err = json.Unmarshal(st.req.Payload, &p); err != nil {
		return
	}
	for k, v := range p {
		switch v1 := v.(type) {
		case string:
			st.r.Header.Set(k, v1)
		case json.Number:
			st.r.Header.Set(k, v1.String())
		}
	}
	return
}

func sendError(st *wsState, cerr error) (err error) {
	m := wsRes{ID: st.req.ID, Type: "error"}
	m.Payload.Errors = []core.Error{{Message: cerr.Error()}}

	msg, err := json.Marshal(m)
	if err != nil {
		return
	}
	err = st.conn.WriteMessage(websocket.TextMessage, msg)
	return
}
