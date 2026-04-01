package serv

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
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

var baseUpgrader = websocket.Upgrader{
	EnableCompression: true,
	ReadBufferSize:    1024,
	WriteBufferSize:   1024,
	HandshakeTimeout:  10 * time.Second,
	Subprotocols:      []string{"graphql-ws", "graphql-transport-ws"},
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
	c         context.Context
	sessions  map[string]wsState
	conn      *websocket.Conn
	connMutex sync.Mutex
	done      chan bool

	w  http.ResponseWriter
	r  *http.Request
	ah auth.HandlerFunc
}

type wsState struct {
	ID   string
	m    *core.Member
	done chan bool
}

// apiV1Ws handles the websocket connection
func (s *graphjinService) apiV1Ws(w http.ResponseWriter, r *http.Request, ah auth.HandlerFunc) {
	upgrader := baseUpgrader
	upgrader.CheckOrigin = s.checkWebSocketOrigin

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		renderErr(w, err)
		return
	}
	defer conn.Close() //nolint:errcheck
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

func (s *graphjinService) checkWebSocketOrigin(r *http.Request) bool {
	rawOrigin := strings.TrimSpace(r.Header.Get("Origin"))
	if rawOrigin == "" {
		return true
	}

	origin, ok := canonicalOrigin(rawOrigin)
	if !ok {
		return false
	}

	for _, allowed := range s.conf.AllowedOrigins {
		allowed = strings.TrimSpace(allowed)
		if allowed == "*" {
			return true
		}
		if normalized, ok := canonicalOrigin(allowed); ok && normalized == origin {
			return true
		}
	}

	expected, ok := requestOrigin(r)
	return ok && expected == origin
}

func requestOrigin(r *http.Request) (string, bool) {
	host := firstHeaderValue(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = strings.TrimSpace(r.Host)
	}
	if host == "" {
		return "", false
	}

	scheme := strings.ToLower(firstHeaderValue(r.Header.Get("X-Forwarded-Proto")))
	switch {
	case scheme != "":
	case r.TLS != nil:
		scheme = "https"
	default:
		scheme = "http"
	}

	return canonicalOrigin(scheme + "://" + host)
}

func canonicalOrigin(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}

	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", false
	}
	if u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return "", false
	}

	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host), true
}

func firstHeaderValue(value string) string {
	if idx := strings.Index(value, ","); idx != -1 {
		value = value[:idx]
	}
	return strings.TrimSpace(value)
}

type authHeaders struct {
	UserIDProvider string      `json:"X-User-ID-Provider"`
	UserRole       string      `json:"X-User-Role"`
	UserID         interface{} `json:"X-User-ID"`
}

// subSwitch handles the websocket message types
func (s *graphjinService) subSwitch(wc *wsConn, req wsReq) (err error) {
	switch req.Type {
	case "connection_init":
		if err = setHeaders(req, wc.r); err != nil {
			return
		}
		if wc.c, err = wc.ah(wc.w, wc.r); err != nil {
			return
		}
		if s.conf.AuthFailBlock && !auth.IsAuth(wc.c) {
			err = auth.Err401
			return
		}

		wc.connMutex.Lock()
		err = wc.conn.WritePreparedMessage(initMsg)
		wc.connMutex.Unlock()

		if err != nil {
			return
		}

	case "start", "subscribe":
		var p gqlReq
		if err = json.Unmarshal(req.Payload, &p); err != nil {
			break
		}

		c := wc.c
		if s.conf.Auth.Development {
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

		// Check for _discovery subscription
		if isDiscoverySubscription(p.Query) {
			database := extractDiscoveryDatabase(p.Vars)
			ds, subErr := s.gj.SubscribeDiscovery(c, database)
			if subErr != nil {
				err = subErr
				break
			}
			st := wsState{ID: req.ID, done: make(chan bool)}
			wc.sessions[st.ID] = st
			useNext := req.Type == "subscribe"
			go s.waitForDiscoveryData(wc, &st, ds, useNext)
			break
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
			if st.m != nil {
				st.m.Unsubscribe()
			}
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

// waitForData waits for data from the subscription
func (s *graphjinService) waitForData(wc *wsConn, st *wsState, useNext bool) {
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

			wc.connMutex.Lock()
			err = wc.conn.WriteMessage(websocket.TextMessage, msg)
			wc.connMutex.Unlock()

			if err != nil {
				s.zlog.Error("Subscription", []zapcore.Field{zap.Error(err)}...)
				sendError(wc, st.ID, err) //nolint:errcheck
				return
			}

		case v := <-st.done:
			if v {
				return
			}

		case v := <-wc.done:
			if v {
				return
			}
		}
	}
}

// allowedWSHeaders is the set of headers that clients are permitted to set
// via the WebSocket connection_init payload.
var allowedWSHeaders = map[string]bool{
	"authorization":   true,
	"x-request-id":    true,
	"x-correlation-id": true,
}

// setHeaders sets the headers from the payload
func setHeaders(req wsReq, r *http.Request) (err error) {
	if len(req.Payload) == 0 {
		return
	}
	var p map[string]interface{}
	if err = json.Unmarshal(req.Payload, &p); err != nil {
		return
	}
	for k, v := range p {
		if !allowedWSHeaders[strings.ToLower(k)] {
			continue
		}
		switch v1 := v.(type) {
		case string:
			r.Header.Set(k, v1)
		case json.Number:
			r.Header.Set(k, v1.String())
		}
	}
	return
}

// isDiscoverySubscription checks if the query is a _discovery subscription.
func isDiscoverySubscription(query string) bool {
	q := strings.TrimSpace(query)
	// Match: subscription { _discovery ... } or subscription name { _discovery ... }
	return strings.Contains(q, "_discovery")
}

// extractDiscoveryDatabase extracts the database name from subscription variables.
func extractDiscoveryDatabase(vars json.RawMessage) string {
	if len(vars) == 0 {
		return ""
	}
	var v map[string]any
	if err := json.Unmarshal(vars, &v); err != nil {
		return ""
	}
	if db, ok := v["database"].(string); ok {
		return db
	}
	return ""
}

// waitForDiscoveryData waits for discovery document updates and sends them over WebSocket.
func (s *graphjinService) waitForDiscoveryData(wc *wsConn, st *wsState, ds *core.DiscoverySubscription, useNext bool) {
	var buf bytes.Buffer

	ptype := "data"
	if useNext {
		ptype = "next"
	}

	enc := json.NewEncoder(&buf)
	for {
		select {
		case doc := <-ds.Result:
			if doc == nil {
				continue
			}

			payload := map[string]any{
				"_discovery": map[string]any{
					"database":     doc.Database,
					"hash":         doc.Hash,
					"generated_at": doc.GeneratedAt.Format("2006-01-02T15:04:05Z"),
					"content":      doc.Markdown,
				},
			}
			data, err := json.Marshal(payload)
			if err != nil {
				continue
			}

			res := wsRes{ID: st.ID, Type: ptype}
			res.Payload.Data = data

			if err := enc.Encode(res); err != nil {
				break
			}
			msg := buf.Bytes()
			buf.Reset()

			wc.connMutex.Lock()
			err = wc.conn.WriteMessage(websocket.TextMessage, msg)
			wc.connMutex.Unlock()

			if err != nil {
				ds.Unsubscribe()
				return
			}

		case <-st.done:
			ds.Unsubscribe()
			return
		}
	}
}

// sendError sends an error message to the client
func sendError(wc *wsConn, id string, cerr error) (err error) {
	m := wsRes{ID: id, Type: "error"}
	m.Payload.Errors = []core.Error{{Message: cerr.Error()}}

	msg, err := json.Marshal(m)
	if err != nil {
		return
	}

	wc.connMutex.Lock()
	defer wc.connMutex.Unlock()
	err = wc.conn.WriteMessage(websocket.TextMessage, msg)
	return
}
