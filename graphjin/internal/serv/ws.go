package serv

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"

	"github.com/dosco/super-graph/core"
	ws "github.com/gorilla/websocket"
)

type gqlWsReq struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Payload gqlReq `json:"payload"`
}

type gqlWsResp struct {
	ID      string       `json:"id"`
	Type    string       `json:"type"`
	Payload *core.Result `json:"payload"`
}

var upgrader = ws.Upgrader{
	EnableCompression: true,
	ReadBufferSize:    1024,
	WriteBufferSize:   1024,
	HandshakeTimeout:  30 * time.Second,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

var initMsg *ws.PreparedMessage

func init() {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)

	err := enc.Encode(gqlWsReq{ID: "1", Type: "connection_ack"})
	if err != nil {
		panic(err)
	}

	initMsg, err = ws.NewPreparedMessage(ws.TextMessage, buf.Bytes())
	if err != nil {
		panic(err)
	}
}

func apiV1Ws(w http.ResponseWriter, r *http.Request) {
	var m *core.Member
	var run bool

	ctx := r.Context()

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		renderErr(w, err)
		return
	}
	defer conn.Close()

	var b []byte
	done := make(chan bool)

	for {
		if _, b, err = conn.ReadMessage(); err != nil {
			break
		}

		msg := gqlWsReq{}

		if err = json.Unmarshal(b, &msg); err != nil {
			continue
		}

		switch msg.Type {
		case "connection_init":
			err = conn.WritePreparedMessage(initMsg)

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
			log.Println("subscription: uknown type: ", msg.Type)
		}

		if err != nil {
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
	enc := json.NewEncoder(&buf)

	for {
		select {
		case v := <-m.Result:
			res := gqlWsResp{ID: "1", Type: "data", Payload: v}
			if err := enc.Encode(res); err != nil {
				break
			}
			msg := buf.Bytes()
			buf.Reset()

			if err := conn.WriteMessage(ws.TextMessage, msg); err != nil {
				break
			}
		case v := <-done:
			if v {
				return
			}
		}
	}
}
