package serve

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func collect(t *testing.T, c *client) (Frame, string, Frame) {
	t.Helper()
	var response Frame
	var body strings.Builder
	timeout := time.After(10 * time.Second)
	for {
		select {
		case frame, ok := <-c.frames:
			if !ok {
				t.Fatal("serve hung up before exit")
			}
			switch frame.Type {
			case "response":
				response = frame
			case "chunk":
				body.Write(frame.Data)
			case "exit":
				return response, body.String(), frame
			}
		case <-timeout:
			t.Fatal("no exit frame")
		}
	}
}

func TestGatewayMakesTheModelCall(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer org-key" || string(body) != `{"model":"m"}` {
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Request-Id", "req-1")
		// A body larger than one chunk arrives whole and in order.
		_, _ = io.WriteString(w, strings.Repeat("a", chunkBytes)+"tail")
	}))
	defer provider.Close()
	socket := startServer(t, &Server{})

	c := dial(t, socket, Frame{Type: "fetch", Request: &FetchRequest{
		URL: provider.URL + "/v1/chat", Method: "POST",
		Headers: map[string]string{"Authorization": "Bearer org-key", "Content-Length": "999"},
		Body:    `{"model":"m"}`,
	}})
	response, body, exit := collect(t, c)
	if exit.Code == nil || *exit.Code != 0 || exit.Error != "" {
		t.Fatalf("exit = %+v", exit)
	}
	if response.Status != 200 || response.Headers["X-Request-Id"] != "req-1" || response.Headers["Content-Type"] != "text/event-stream" {
		t.Fatalf("response = %+v", response)
	}
	if body != strings.Repeat("a", chunkBytes)+"tail" {
		t.Fatalf("body has %d bytes", len(body))
	}
}

func TestGatewayPassesProviderErrorsThrough(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"rate limited"}`, http.StatusTooManyRequests)
	}))
	defer provider.Close()
	socket := startServer(t, &Server{})
	response, body, exit := collect(t, dial(t, socket, Frame{Type: "fetch", Request: &FetchRequest{URL: provider.URL}}))
	if response.Status != 429 || !strings.Contains(body, "rate limited") || *exit.Code != 0 {
		t.Fatalf("response = %+v body = %q exit = %+v", response, body, exit)
	}
}

func TestGatewayRefusesWhatIsNotAnHTTPCall(t *testing.T) {
	socket := startServer(t, &Server{})
	for _, req := range []*FetchRequest{nil, {URL: "file:///etc/passwd"}, {URL: "unix:///run/openneko/serve.sock"}} {
		_, _, exit := collect(t, dial(t, socket, Frame{Type: "fetch", Request: req}))
		if exit.Code == nil || *exit.Code == 0 || exit.Error == "" {
			t.Fatalf("%+v: exit = %+v", req, exit)
		}
	}
}

func TestGatewayKeepsAQueryKeyOutOfErrors(t *testing.T) {
	socket := startServer(t, &Server{})
	_, _, exit := collect(t, dial(t, socket, Frame{Type: "fetch", Request: &FetchRequest{URL: "http://127.0.0.1:1/v1/models?key=org-key"}}))
	if exit.Error == "" || strings.Contains(exit.Error, "org-key") {
		t.Fatalf("exit = %+v", exit)
	}
}

func TestGatewayCancelsWhenTheWorkerHangsUp(t *testing.T) {
	cancelled := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(cancelled)
	}))
	defer provider.Close()
	socket := startServer(t, &Server{})
	c := dial(t, socket, Frame{Type: "fetch", Request: &FetchRequest{URL: provider.URL}})
	if frame := <-c.frames; frame.Type != "response" {
		t.Fatalf("first frame = %+v", frame)
	}
	_ = c.conn.Close()
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("the model call outlived the worker's connection")
	}
}
