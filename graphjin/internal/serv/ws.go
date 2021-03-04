package serv

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/dosco/graphjin/core"
	"github.com/dosco/graphjin/internal/serv/internal/auth"
	ws "github.com/gorilla/websocket"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type gqlWsReq struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Payload gqlReq `json:"payload"`
}

type gqlWsResp struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Payload struct {
		Data   json.RawMessage `json:"data"`
		Errors []string        `json:"errors,omitempty"`
	} `json:"payload"`
}

type gqlWsError struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Payload struct {
		Error string `json:"error"`
	} `json:"payload"`
}

type wsConnInit struct {
	Type    string                 `json:"type,omitempty"`
	Payload map[string]interface{} `json:"payload,omitempty"`
}

var upgrader = ws.Upgrader{
	EnableCompression: true,
	ReadBufferSize:    1024,
	WriteBufferSize:   1024,
	HandshakeTimeout:  10 * time.Second,
	Subprotocols:      []string{"graphql-ws", "graphql-transport-ws"},
	CheckOrigin:       func(r *http.Request) bool { return true },
}

var initMsg *ws.PreparedMessage

func init() {
	msg, err := json.Marshal(gqlWsReq{ID: "1", Type: "connection_ack"})
	if err != nil {
		panic(err)
	}

	initMsg, err = ws.NewPreparedMessage(ws.TextMessage, msg)
	if err != nil {
		panic(err)
	}
}

func (sc *ServConfig) apiV1Ws(w http.ResponseWriter, r *http.Request) {
	var m *core.Member
	var run bool

	ctx := r.Context()
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		renderErr(w, err)
		return
	}
	defer conn.Close()
	conn.SetReadLimit(2048)

	var msg gqlWsReq
	var b []byte

	done := make(chan bool)
	for {
		if _, b, err = conn.ReadMessage(); err != nil {
			sc.zlog.Error("Websockets", []zapcore.Field{zap.Error(err)}...)
			break
		}

		if err = json.Unmarshal(b, &msg); err != nil {
			sc.zlog.Error("Websockets", []zapcore.Field{zap.Error(err)}...)
			continue
		}

		switch msg.Type {
		case "connection_init":
			var initReq wsConnInit

			d := json.NewDecoder(bytes.NewReader(b))
			d.UseNumber()

			if err = d.Decode(&initReq); err != nil {
				sc.zlog.Error("Websockets", []zapcore.Field{zap.Error(err)}...)
				break
			}

			hfn := func(writer http.ResponseWriter, request *http.Request) {
				ctx = request.Context()
				err = conn.WritePreparedMessage(initMsg)
			}

			handler, _ := auth.WithAuth(http.HandlerFunc(hfn), &sc.conf.Auth)

			if err != nil {
				break
			}

			for k, v := range initReq.Payload {
				switch v1 := v.(type) {
				case string:
					r.Header.Set(k, v1)
				case json.Number:
					r.Header.Set(k, v1.String())
				}
			}
			handler.ServeHTTP(w, r)

		case "start", "subscribe":
			if run {
				continue
			}

			if sc.conf.Serv.Auth.SubsCredsInVars {
				type authHeaders struct {
					UserIDProvider string `json:"X-User-ID-Provider"`
					UserID         string `json:"X-User-ID"`
					UserRole       string `json:"X-User-Role"`
				}
				var x authHeaders
				if err = json.Unmarshal(msg.Payload.Vars, &x); err == nil {
					if x.UserIDProvider != "" {
						ctx = context.WithValue(ctx, core.UserIDProviderKey, x.UserIDProvider)
					}
					if x.UserID != "" {
						ctx = context.WithValue(ctx, core.UserIDKey, x.UserID)
					}
					if x.UserRole != "" {
						ctx = context.WithValue(ctx, core.UserRoleKey, x.UserRole)
					}

				}
			}

			m, err = gj.Subscribe(ctx, msg.Payload.Query, msg.Payload.Vars, nil)
			if err == nil {
				go sc.waitForData(done, conn, m, msg)
				run = true
			}

		case "stop":
			m.Unsubscribe()
			done <- true
			run = false

		default:
			fields := []zapcore.Field{
				zap.String("msg_type", msg.Type),
				zap.Error(errors.New("unknown message type")),
			}
			sc.zlog.Error("Subscription Error", fields...)
		}

		if err != nil {
			err = sendError(conn, err, msg.ID)
			break
		}
	}

	if err != nil {
		sc.zlog.Error("Subscription Error", []zapcore.Field{zap.Error(err)}...)
	}

	m.Unsubscribe()
	done <- true
}

func (sc *ServConfig) waitForData(done chan bool, conn *ws.Conn, m *core.Member, req gqlWsReq) {
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
			res := gqlWsResp{ID: req.ID, Type: ptype}
			res.Payload.Data = v.Data

			if v.Error != "" {
				res.Payload.Errors = []string{v.Error}
			}

			if err = enc.Encode(res); err != nil {
				continue
			}
			msg := buf.Bytes()
			buf.Reset()

			if err = conn.WriteMessage(ws.TextMessage, msg); err != nil {
				continue
			}
		case v := <-done:
			if v {
				return
			}
		}

		if err != nil {
			err = sendError(conn, err, req.ID)
			break
		}
	}

	if err != nil && isDev() {
		sc.zlog.Error("Websockets", []zapcore.Field{zap.Error(err)}...)
	}
}

func sendError(conn *ws.Conn, err error, id string) error {
	res := gqlWsError{ID: id, Type: "error"}
	res.Payload.Error = err.Error()

	msg, err := json.Marshal(res)
	if err != nil {
		return err
	}
	if err := conn.WriteMessage(ws.TextMessage, msg); err != nil {
		return err
	}
	return nil
}
