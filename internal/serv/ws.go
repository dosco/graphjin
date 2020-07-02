package serv

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"

	"github.com/dosco/super-graph/core"
	"github.com/gorilla/websocket"
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

var upgrader = websocket.Upgrader{
	EnableCompression: true,
	ReadBufferSize:    1024,
	WriteBufferSize:   1024,
	HandshakeTimeout:  60 * time.Second,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

var initMsg *websocket.PreparedMessage

func init() {
	var buf bytes.Buffer
	var err error

	enc := json.NewEncoder(&buf)

	enc.Encode(gqlWsReq{ID: "1", Type: "connection_ack"})
	initMsg, err = websocket.NewPreparedMessage(1, buf.Bytes())
	buf.Reset()

	if err != nil {
		panic(err)
	}
}

func apiV1Ws(w http.ResponseWriter, r *http.Request) {
	var m *core.Member

	ctx := r.Context()

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		renderErr(w, err)
		return
	}
	defer conn.Close()

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)

	var mt int
	var b []byte
	var run bool

	for {
		if mt, b, err = conn.ReadMessage(); err != nil {
			break
		}

		msg := gqlWsReq{}

		if err = json.Unmarshal(b, &msg); err != nil {
			continue
		}

		if msg.Type == "connection_init" {
			if err = conn.WritePreparedMessage(initMsg); err != nil {
				break
			}
			continue
		}

		if msg.Type == "start" && !run {
			m, err = sg.Subscribe(ctx, msg.Payload.Query, msg.Payload.Vars)
			if err != nil {
				break
			}
			run = true

			go func() {
				for v := range m.Result {
					if !run {
						break
					}

					enc.Encode(gqlWsResp{ID: "1", Type: "data", Payload: v})
					dataMsg := buf.Bytes()
					buf.Reset()

					if err = conn.WriteMessage(mt, dataMsg); err != nil {
						break
					}
				}
			}()
			continue
		}

		if msg.Type == "stop" && run {
			run = false
			sg.UnSubscribe(ctx, m)
		}
	}

	if err != nil {
		log.Printf("ERR %s", err)
	}

	run = false
	sg.UnSubscribe(ctx, m)
}
