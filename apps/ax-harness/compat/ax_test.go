// Package compat checks the pinned Ax behavior that the harness relies on, over real HTTP and Goja.
package compat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	ax "github.com/ax-llm/ax/packages/go"
	axgoja "github.com/ax-llm/ax/packages/go/runtime/goja"
)

// probe records callback metadata only, to prove callback placement.
type probe struct {
	mu       sync.Mutex
	events   []string
	executed int
}

func (p *probe) invoke(ctx context.Context, args map[string]ax.Value) (ax.Value, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, "started")
	status := "completed"
	defer func() { p.events = append(p.events, status) }()
	if ctx.Err() != nil {
		status = "cancelled"
		return nil, ctx.Err()
	}
	query, ok := args["query"].(string)
	if !ok || query == "" {
		status = "invalid"
		return ax.Object("error", "invalid arguments"), nil
	}
	if query == "denied" {
		status = "denied"
		return ax.Object("error", "permission denied"), nil
	}
	p.executed++
	return ax.Object("reference", "REF-42"), nil
}
func (p *probe) check(t *testing.T, executed int, events ...string) {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.executed != executed || strings.Join(p.events, ",") != strings.Join(events, ",") {
		t.Fatalf("execution=%d events=%v", p.executed, p.events)
	}
}
func object(v ax.Value) map[string]ax.Value { m, _ := v.(map[string]ax.Value); return m }
func encoded(v any) string                  { b, _ := json.Marshal(v); return string(b) }

// Every request crosses Ax's HTTPTransport. Only the external model is scripted.
type requestLog struct {
	mu    sync.Mutex
	items []map[string]ax.Value
}

func (r *requestLog) snapshot() []map[string]ax.Value {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]map[string]ax.Value(nil), r.items...)
}
func modelServer(t *testing.T, messages ...map[string]ax.Value) (*ax.OpenAICompatibleClient, *requestLog) {
	t.Helper()
	requests := &requestLog{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.mu.Lock()
		defer requests.mu.Unlock()
		var body map[string]ax.Value
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			http.Error(w, "bad body", 400)
			return
		}
		requests.items = append(requests.items, body)
		if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer synthetic-test-key" {
			t.Error("unexpected method/auth")
		}
		n := len(requests.items) - 1
		if n >= len(messages) {
			t.Errorf("unexpected model request %d", n+1)
			http.Error(w, "fixture exhausted", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ax.Object("id", fmt.Sprint("response-", n), "model", "fixture", "choices", ax.Array(ax.Object("index", 0, "message", messages[n], "finish_reason", "stop"))))
	}))
	t.Cleanup(server.Close)
	return ax.NewOpenAICompatibleClient(ax.Object("base_url", server.URL, "api_key", "synthetic-test-key", "model", "fixture")), requests
}
func answer(s string) map[string]ax.Value { return ax.Object("role", "assistant", "content", s) }
func TestAgentGojaTwoCallbacksHTTP(t *testing.T) {
	p := &probe{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	runtime := axgoja.NewRuntime(axgoja.WithCallable("lookup", func(v ax.Value) (ax.Value, error) { return p.invoke(ctx, object(v)) }))
	client, requests := modelServer(t,
		answer(`{"javascriptCode":"final('Find reference', {})"}`),
		answer(`{"javascriptCode":"const first=lookup({query:'reference'}); const denied=lookup({query:'denied'}); final('Report reference', {first,denied});"}`),
		answer(`Answer: REF-42`))
	agent := ax.NewAgent("question:string -> answer:string", ax.Object("runtime", runtime, "directResponse", "off", "validationRetries", 0))
	defer agent.CloseRuntimeSession()
	out, err := agent.Forward(ctx, client, ax.Object("question", "Find reference"), nil)
	if err != nil || object(out)["answer"] != "REF-42" {
		t.Fatalf("output=%v error=%v requests=%d", out, err, len(requests.snapshot()))
	}
	p.check(t, 1, "started", "completed", "started", "denied")
	if len(requests.snapshot()) != 3 || !strings.Contains(encoded(requests.snapshot()[2]), "REF-42") || !strings.Contains(encoded(requests.snapshot()[2]), "permission denied") {
		t.Fatal("responder did not receive actual callback outcomes")
	}
}

func TestHTTPStreamingCancellation(t *testing.T) {
	observed := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"id\":\"s1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"first\"}}]}\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
			close(observed)
		case <-time.After(5 * time.Second):
		}
	}))
	defer server.Close()
	client := ax.NewOpenAICompatibleClient(ax.Object("base_url", server.URL, "api_key", "synthetic-test-key", "model", "fixture"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := client.StreamEvents(ctx, ax.Object("chat_prompt", ax.Array(ax.Object("role", "user", "content", "hello"))), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if !stream.Next() {
		t.Fatalf("missing first event: %v", stream.Err())
	}
	cancel()
	if stream.Next() || stream.Err() == nil {
		t.Fatal("cancelled stream did not fail")
	}
	select {
	case <-observed:
	case <-time.After(time.Second):
		t.Fatal("HTTP upstream did not observe cancellation")
	}
}
