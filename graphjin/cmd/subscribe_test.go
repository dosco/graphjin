package main

import (
	"bytes"
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
)

func TestResolveGraphQLURL(t *testing.T) {
	cases := []struct {
		in, want, wantErr string
	}{
		{"http://localhost:8080/api/v1/mcp/message", "http://localhost:8080/api/v1/graphql", ""},
		{"https://api.example.com/api/v1/mcp/message", "https://api.example.com/api/v1/graphql", ""},
		{"http://10.0.0.5", "http://10.0.0.5/api/v1/graphql", ""},
		{"", "", "no GraphJin server"},
	}
	for _, c := range cases {
		got, err := resolveGraphQLURL(c.in)
		if c.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("resolveGraphQLURL(%q) err = %v, want substring %q", c.in, err, c.wantErr)
			}
			continue
		}
		if err != nil {
			t.Errorf("resolveGraphQLURL(%q) unexpected err: %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("resolveGraphQLURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRunSubscribeStream_NextThenComplete(t *testing.T) {
	var gotReq map[string]any
	var gotMethod, gotAccept, gotAuth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotAccept = r.Header.Get("Accept")
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotReq)

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		fmt.Fprint(w, "event: next\ndata: {\"data\":{\"users\":[{\"id\":1}]}}\n\n")
		flusher.Flush()
		fmt.Fprint(w, ": ping\n\n")
		flusher.Flush()
		fmt.Fprint(w, "event: next\ndata: {\"data\":{\"users\":[{\"id\":2}]}}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "event: complete\ndata: \n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	var emitted []string
	emit := func(p json.RawMessage) error {
		emitted = append(emitted, string(p))
		return nil
	}

	var stderr bytes.Buffer
	err := runSubscribeStream(
		context.Background(),
		srv.URL,
		"Bearer test-token",
		[]string{"X-Trace: t1"},
		"subscription { users { id } }",
		map[string]any{"limit": 10},
		"",
		emit,
		&stderr,
	)
	if err != nil {
		t.Fatalf("runSubscribeStream: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotAccept != "text/event-stream" {
		t.Errorf("accept = %q, want text/event-stream", gotAccept)
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("authorization = %q", gotAuth)
	}
	if gotReq["query"] != "subscription { users { id } }" {
		t.Errorf("query in body = %v", gotReq["query"])
	}
	if vars, ok := gotReq["variables"].(map[string]any); !ok || vars["limit"] != float64(10) {
		t.Errorf("variables in body = %v", gotReq["variables"])
	}

	if len(emitted) != 2 {
		t.Fatalf("emitted %d frames, want 2: %v", len(emitted), emitted)
	}
	if !strings.Contains(emitted[0], `"id":1`) || !strings.Contains(emitted[1], `"id":2`) {
		t.Errorf("emitted frames = %v", emitted)
	}
}

func TestRunSubscribeStream_ContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		fmt.Fprint(w, "event: next\ndata: {\"data\":{\"x\":1}}\n\n")
		flusher.Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	emitted := make(chan struct{}, 1)
	emit := func(p json.RawMessage) error {
		select {
		case emitted <- struct{}{}:
		default:
		}
		return nil
	}

	var wg sync.WaitGroup
	wg.Add(1)
	var got error
	go func() {
		defer wg.Done()
		got = runSubscribeStream(ctx, srv.URL, "", nil, "subscription { x }", nil, "", emit, io.Discard)
	}()

	select {
	case <-emitted:
	case <-time.After(2 * time.Second):
		cancel()
		wg.Wait()
		t.Fatal("never received first frame")
	}

	cancel()
	wg.Wait()
	if got != nil {
		t.Errorf("runSubscribeStream after cancel = %v, want nil", got)
	}
}

func TestRunSubscribeStream_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, "unauthorized")
	}))
	defer srv.Close()

	err := runSubscribeStream(
		context.Background(), srv.URL, "", nil,
		"subscription { x }", nil, "",
		func(json.RawMessage) error { return nil },
		io.Discard,
	)
	if err == nil {
		t.Fatal("expected error on 401, got nil")
	}
	if !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "graphjin cli setup") {
		t.Errorf("unexpected error: %v", err)
	}
}
