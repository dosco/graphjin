package serv

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"

	"github.com/dosco/super-graph/core"
	"github.com/dosco/super-graph/internal/serv/internal/auth"
	ws "github.com/gorilla/websocket"
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
	Type    string            `json:"type,omitempty"`
	Payload map[string]string `json:"payload,omitempty"`
}

var upgrader = ws.Upgrader{
	EnableCompression: true,
	ReadBufferSize:    1024,
	WriteBufferSize:   1024,
	HandshakeTimeout:  10 * time.Second,
	Subprotocols:      []string{"graphql-ws"},
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

func apiV1Ws(w http.ResponseWriter, r *http.Request) {
	var m *core.Member
	var run bool

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		renderErr(w, err)
		return
	}
	defer conn.Close()

	var msg gqlWsReq
	var b []byte

	done := make(chan bool)
	ctx := r.Context()

	for {
		if _, b, err = conn.ReadMessage(); err != nil {
			break
		}
		if err = json.Unmarshal(b, &msg); err != nil {
			log.Println(err)
			continue
		}

		switch msg.Type {
		case "connection_init":
			var initReq wsConnInit
			if err = json.Unmarshal(b, &initReq); err != nil {
				break
			}
			handler, _ := auth.WithAuth(http.HandlerFunc(func (writer http.ResponseWriter, request *http.Request) {
				ctx = request.Context()
				err = conn.WritePreparedMessage(initMsg)
				if err != nil {
					err = sendError(conn, err)
				}
			}), &conf.Auth)
			for k, v := range initReq.Payload {
				r.Header.Set(k, v)
			}
			handler.ServeHTTP(w, r)

		case "start":
			if run {
				continue
			}
			m, err = sg.Subscribe(ctx, msg.Payload.Query, msg.Payload.Vars)
			if err == nil {
				go waitForData(done, conn, m)
				run = true
			}

		case "stop":
			m.Unsubscribe()
			done <- true
			run = false

		default:
			log.Println("subscription: unknown type: ", msg.Type)
		}

		if err != nil {
			err = sendError(conn, err)
			break
		}
	}

	if err != nil {
		log.Printf("ERR %s", err)
	}

	m.Unsubscribe()
}

func waitForData(done chan bool, conn *ws.Conn, m *core.Member) {
	var buf bytes.Buffer
	var err error

	enc := json.NewEncoder(&buf)

	for {
		select {
		case v := <-m.Result:
			res := gqlWsResp{ID: "1", Type: "data"}
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
			err = sendError(conn, err)
			break
		}
	}

	if err != nil && isDev() {
		log.Printf("ERR %s", err)
	}
}

func sendError(conn *ws.Conn, err error) error {
	res := gqlWsError{ID: "1", Type: "error"}
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
