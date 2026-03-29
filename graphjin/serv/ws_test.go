package serv

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dosco/graphjin/auth/v3"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

func newWebSocketTestServer(t *testing.T, allowedOrigins []string) *httptest.Server {
	t.Helper()

	logger := zap.NewNop()
	svc := &graphjinService{
		conf: &Config{
			Serv: Serv{
				AllowedOrigins: allowedOrigins,
			},
		},
		log:  logger.Sugar(),
		zlog: logger,
	}

	hs := &HttpService{}
	hs.Store(svc)

	ah, err := auth.NewAuthHandlerFunc(auth.Auth{Type: "none"})
	if err != nil {
		t.Fatalf("new auth handler: %v", err)
	}

	return httptest.NewServer(hs.GraphQL(ah))
}

func TestWebSocketRejectsCrossOriginByDefault(t *testing.T) {
	server := newWebSocketTestServer(t, nil)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	header := http.Header{
		"Origin": {"https://evil.example"},
	}

	_, resp, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err == nil {
		t.Fatal("expected cross-origin websocket handshake to fail")
	}
	if resp == nil || resp.StatusCode != 403 {
		t.Fatalf("expected HTTP 403, got %+v", resp)
	}
}

func TestWebSocketAllowsConfiguredOrigin(t *testing.T) {
	server := newWebSocketTestServer(t, []string{"https://allowed.example"})
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	header := http.Header{
		"Origin": {"https://allowed.example"},
	}

	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("expected configured origin to succeed, got err=%v resp=%+v", err, resp)
	}
	defer conn.Close()

	if err := conn.WriteControl(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
		time.Now().Add(time.Second)); err != nil {
		t.Fatalf("send close control: %v", err)
	}
}

func TestWebSocketAllowsSameOriginAndNoOrigin(t *testing.T) {
	server := newWebSocketTestServer(t, nil)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	tests := []struct {
		name   string
		origin string
	}{
		{name: "same origin", origin: server.URL},
		{name: "no origin"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			header := http.Header{}
			if tt.origin != "" {
				header["Origin"] = []string{tt.origin}
			}

			conn, resp, err := websocket.DefaultDialer.Dial(wsURL, header)
			if err != nil {
				t.Fatalf("expected websocket handshake to succeed, got err=%v resp=%+v", err, resp)
			}
			defer conn.Close()

			if err := conn.WriteControl(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
				time.Now().Add(time.Second)); err != nil {
				t.Fatalf("send close control: %v", err)
			}
		})
	}
}
