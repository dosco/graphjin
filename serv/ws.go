package serv

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/dosco/graphjin/core"
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

func (s *service) apiV1Ws(w http.ResponseWriter, r *http.Request) {
	var m *core.Member
	var ready bool
	var err error

	ct := r.Context()
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		renderErr(w, err)
		return
	}
	defer c.Close()
	c.SetReadLimit(2048)

	var v wsReq

	done := make(chan bool)
	for {
		var b []byte

		if _, b, err = c.ReadMessage(); err != nil {
			break
		}

		if err = json.Unmarshal(b, &v); err != nil {
			break
		}

		if ready {
			if v.Type != "connection_terminate" &&
				v.Type != "stop" &&
				v.Type != "complete" {
				err = fmt.Errorf("unknown message type: %s", v.Type)
			}
			break
		}

		if ready, err = s.subSwitch(ct, c, v, done); err != nil {
			if err1 := sendError(ct, c, err, v.ID); err1 != nil {
				err = err1
			}
			break
		}
	}

	if err != nil {
		s.zlog.Error("Subscription", []zapcore.Field{zap.Error(err)}...)
	}

	m.Unsubscribe()
	done <- true
}

func (s *service) subSwitch(
	ct context.Context, c *websocket.Conn, v wsReq, done chan bool) (bool, error) {

	switch v.Type {
	case "connection_init":
		if err := c.WritePreparedMessage(initMsg); err != nil {
			return false, err
		}

		/*
			hfn := func(w http.ResponseWriter, r *http.Request) {
				if err := c.WritePreparedMessage(initMsg); err != nil {
					s.zlog.Error("Websockets", []zapcore.Field{zap.Error(err)}...)
				}
				ct = r.Context()
			}

			handler, err := auth.WithAuth(http.HandlerFunc(hfn), &s.conf.Auth, s.zlog)
			if err != nil {
				return false, err
			}

			if len(v.Payload) == 0 {
				handler.ServeHTTP(w, r)
				break
			}

			var p map[string]interface{}
			if err := json.Unmarshal(v.Payload, &p); err != nil {
				s.zlog.Error("Websockets", []zapcore.Field{zap.Error(err)}...)
				break
			}

			for k, v := range p {
				switch v1 := v.(type) {
				case string:
					r.Header.Set(k, v1)
				case json.Number:
					r.Header.Set(k, v1.String())
				}
			}
			handler.ServeHTTP(w, r)
		*/

	case "start", "subscribe":
		var p gqlReq
		if err := json.Unmarshal(v.Payload, &p); err != nil {
			return false, err
		}

		if s.conf.Serv.Auth.SubsCredsInVars {
			type authHeaders struct {
				UserIDProvider string      `json:"X-User-ID-Provider"`
				UserRole       string      `json:"X-User-Role"`
				UserID         interface{} `json:"X-User-ID"`
			}

			var x authHeaders
			if err := json.Unmarshal(p.Vars, &x); err == nil {
				if x.UserIDProvider != "" {
					ct = context.WithValue(ct, core.UserIDProviderKey, x.UserIDProvider)
				}
				if x.UserRole != "" {
					ct = context.WithValue(ct, core.UserRoleKey, x.UserRole)
				}
				if x.UserID != nil {
					ct = context.WithValue(ct, core.UserIDKey, x.UserID)
				}
			} else {
				return false, err
			}
		}

		m, err := s.gj.Subscribe(ct, p.Query, p.Vars, nil)
		if err != nil {
			return false, err
		}

		go s.waitForData(ct, done, c, m, v)
		return true, nil

	default:
		return false, fmt.Errorf("unknown message type: %s", v.Type)
	}

	return false, nil
}

func (s *service) waitForData(
	ct context.Context, done chan bool, c *websocket.Conn,
	m *core.Member, req wsReq) {
	var buf bytes.Buffer

	var ptype string
	var err error

	if req.Type == "subscribe" {
		ptype = "next"
	} else {
		ptype = "data"
	}

	enc := json.NewEncoder(&buf)

	for {
		select {
		case v := <-m.Result:
			m := wsRes{ID: req.ID, Type: ptype}
			m.Payload.Data = v.Data
			m.Payload.Errors = v.Errors

			if err = enc.Encode(m); err != nil {
				break
			}
			msg := buf.Bytes()
			buf.Reset()

			err = c.WriteMessage(websocket.TextMessage, msg)
		case v := <-done:
			if v {
				return
			}
		}

		if err != nil {
			if err1 := sendError(ct, c, err, req.ID); err != nil {
				err = err1
			}
			s.zlog.Error("Websockets", []zapcore.Field{zap.Error(err)}...)
			break
		}
	}
}

func sendError(ct context.Context, c *websocket.Conn, err error, id string) error {
	m := wsRes{ID: id, Type: "error"}
	m.Payload.Errors = []core.Error{{Message: err.Error()}}

	msg, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if err := c.WriteMessage(websocket.TextMessage, msg); err != nil {
		return err
	}
	return nil
}
