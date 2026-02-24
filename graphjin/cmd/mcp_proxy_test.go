package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func init() {
	if log == nil {
		log = newLogger(false).Sugar()
	}
}

func TestNormalizeMCPServerURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "IP address only",
			input:    "10.0.0.5",
			expected: "http://10.0.0.5/api/v1/mcp/message",
		},
		{
			name:     "IP with port",
			input:    "10.0.0.5:8080",
			expected: "http://10.0.0.5:8080/api/v1/mcp/message",
		},
		{
			name:     "hostname only",
			input:    "localhost",
			expected: "http://localhost/api/v1/mcp/message",
		},
		{
			name:     "hostname with port",
			input:    "localhost:8080",
			expected: "http://localhost:8080/api/v1/mcp/message",
		},
		{
			name:     "full HTTP URL without path",
			input:    "http://example.com",
			expected: "http://example.com/api/v1/mcp/message",
		},
		{
			name:     "full HTTP URL with just slash",
			input:    "http://example.com/",
			expected: "http://example.com/api/v1/mcp/message",
		},
		{
			name:     "full HTTP URL with custom path",
			input:    "http://example.com/custom/mcp",
			expected: "http://example.com/custom/mcp",
		},
		{
			name:     "HTTPS URL",
			input:    "https://secure.example.com",
			expected: "https://secure.example.com/api/v1/mcp/message",
		},
		{
			name:     "HTTPS URL with path",
			input:    "https://secure.example.com/api/v1/mcp/message",
			expected: "https://secure.example.com/api/v1/mcp/message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeMCPServerURL(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeMCPServerURL(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestProxy_DisconnectedRequestReturnsConnectionLostError(t *testing.T) {
	var out bytes.Buffer
	p := &mcpProxy{
		serverURL:        "http://127.0.0.1:1/api/v1/mcp/message",
		httpClient:       &http.Client{Timeout: 50 * time.Millisecond},
		stdin:            strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"execute_graphql"}}` + "\n"),
		stdout:           &out,
		initialBackoff:   10 * time.Millisecond,
		maxBackoff:       20 * time.Millisecond,
		probeTimeout:     10 * time.Millisecond,
		reconnectWakeCh:  make(chan struct{}, 1),
		reconnectResetCh: make(chan struct{}, 1),
	}

	ctx, cancel := context.WithCancel(context.Background())
	err := p.run(ctx)
	cancel()
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}

	line := strings.TrimSpace(out.String())
	if line == "" {
		t.Fatalf("expected JSON-RPC error response")
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}

	if got := fmt.Sprintf("%v", resp["id"]); got != "1" {
		t.Fatalf("expected id=1, got %v", resp["id"])
	}

	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object, got %T", resp["error"])
	}

	msg := fmt.Sprintf("%v", errObj["message"])
	if msg != "GraphJin MCP proxy is disconnected from the upstream server. This tool call was not executed. Background reconnect is active; please retry this same tool call shortly." {
		t.Fatalf("unexpected error message: %s", msg)
	}

	if code := fmt.Sprintf("%v", errObj["code"]); code != "-32001" {
		t.Fatalf("expected code -32001, got %s", code)
	}

	data, ok := errObj["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected error.data object, got %T", errObj["data"])
	}
	if data["type"] != "UPSTREAM_DISCONNECTED" {
		t.Fatalf("expected type UPSTREAM_DISCONNECTED, got %v", data["type"])
	}
	if data["retryable"] != true {
		t.Fatalf("expected retryable=true, got %v", data["retryable"])
	}
	if data["background_reconnect"] != true {
		t.Fatalf("expected background_reconnect=true, got %v", data["background_reconnect"])
	}
	if data["tool_name"] != "execute_graphql" {
		t.Fatalf("expected tool_name execute_graphql, got %v", data["tool_name"])
	}
}

func TestProxy_NotificationRequestWritesNoResponse(t *testing.T) {
	var out bytes.Buffer
	p := &mcpProxy{
		serverURL:        "http://127.0.0.1:1/api/v1/mcp/message",
		httpClient:       &http.Client{Timeout: 50 * time.Millisecond},
		stdin:            strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/changed"}` + "\n"),
		stdout:           &out,
		initialBackoff:   10 * time.Millisecond,
		maxBackoff:       20 * time.Millisecond,
		probeTimeout:     10 * time.Millisecond,
		reconnectWakeCh:  make(chan struct{}, 1),
		reconnectResetCh: make(chan struct{}, 1),
	}

	ctx, cancel := context.WithCancel(context.Background())
	err := p.run(ctx)
	cancel()
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if strings.TrimSpace(out.String()) != "" {
		t.Fatalf("expected no response for notification, got: %q", out.String())
	}
}

func TestProxy_ReconnectBackoffInterruptedByRequest(t *testing.T) {
	var out bytes.Buffer
	pr, pw := io.Pipe()
	defer pr.Close()
	defer pw.Close()

	probeTimes := make(chan time.Time, 10)
	p := &mcpProxy{
		serverURL: "http://127.0.0.1:1/api/v1/mcp/message",
		httpClient: &http.Client{
			Timeout: 50 * time.Millisecond,
		},
		stdin:            pr,
		stdout:           &out,
		initialBackoff:   500 * time.Millisecond,
		maxBackoff:       time.Second,
		probeTimeout:     20 * time.Millisecond,
		reconnectWakeCh:  make(chan struct{}, 1),
		reconnectResetCh: make(chan struct{}, 1),
		forwardFn: func(ctx context.Context, body []byte) ([]byte, error) {
			return nil, errors.New("connection refused")
		},
		probeFn: func(ctx context.Context) error {
			probeTimes <- time.Now()
			return errors.New("probe failed")
		},
	}
	p.markDisconnected(errors.New("startup disconnect"))
	p.signalReconnectWake()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- p.run(ctx)
	}()

	first := <-probeTimes

	time.Sleep(50 * time.Millisecond)
	if _, err := pw.Write([]byte(`{"jsonrpc":"2.0","id":7,"method":"tools/list"}` + "\n")); err != nil {
		t.Fatalf("write stdin failed: %v", err)
	}
	pw.Close()

	second := <-probeTimes
	if second.Sub(first) >= 300*time.Millisecond {
		t.Fatalf("expected immediate reconnect probe after request reset, got delay %v", second.Sub(first))
	}

	cancel()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("run failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run did not stop")
	}
}

func TestProxy_RecoversWithoutRestart(t *testing.T) {
	var online atomic.Bool
	online.Store(false)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !online.Load() {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"jsonrpc":"2.0","id":2,"result":{"ok":true}}`)
	}))
	defer srv.Close()

	var out bytes.Buffer
	pr, pw := io.Pipe()
	defer pr.Close()

	p := &mcpProxy{
		serverURL:        srv.URL,
		httpClient:       &http.Client{Timeout: 200 * time.Millisecond},
		stdin:            pr,
		stdout:           &out,
		initialBackoff:   20 * time.Millisecond,
		maxBackoff:       100 * time.Millisecond,
		probeTimeout:     50 * time.Millisecond,
		reconnectWakeCh:  make(chan struct{}, 1),
		reconnectResetCh: make(chan struct{}, 1),
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- p.run(ctx)
	}()

	if _, err := pw.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}` + "\n")); err != nil {
		t.Fatalf("write first request failed: %v", err)
	}

	time.Sleep(30 * time.Millisecond)
	online.Store(true)

	if _, err := pw.Write([]byte(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n")); err != nil {
		t.Fatalf("write second request failed: %v", err)
	}
	pw.Close()

	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("run failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run did not finish")
	}
	cancel()

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected 2 responses, got %d: %q", len(lines), out.String())
	}
	if !strings.Contains(lines[0], `"result":{"tools":[]}`) {
		t.Fatalf("expected first response to be fallback tools list, got %s", lines[0])
	}
	if !strings.Contains(lines[1], `"result":{"ok":true}`) {
		t.Fatalf("expected second response to be success, got %s", lines[1])
	}
}

func TestProxy_HTTP4xxDoesNotMarkDisconnected(t *testing.T) {
	var probeCount atomic.Int32
	var reqCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := reqCount.Add(1)
		if n == 1 {
			http.Error(w, "bad input", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"jsonrpc":"2.0","id":2,"result":{"ok":true}}`)
	}))
	defer srv.Close()

	var out bytes.Buffer
	p := &mcpProxy{
		serverURL:        srv.URL,
		httpClient:       &http.Client{Timeout: 200 * time.Millisecond},
		stdin:            strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}` + "\n" + `{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n"),
		stdout:           &out,
		initialBackoff:   20 * time.Millisecond,
		maxBackoff:       100 * time.Millisecond,
		probeTimeout:     50 * time.Millisecond,
		reconnectWakeCh:  make(chan struct{}, 1),
		reconnectResetCh: make(chan struct{}, 1),
		probeFn: func(ctx context.Context) error {
			probeCount.Add(1)
			return nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	err := p.run(ctx)
	cancel()
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}

	if p.isDisconnected() {
		t.Fatal("expected proxy to remain connected after 4xx")
	}
	if probeCount.Load() != 0 {
		t.Fatalf("expected no reconnect probes, got %d", probeCount.Load())
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 responses, got %d: %q", len(lines), out.String())
	}
	if !strings.Contains(lines[0], "proxy error: http error: 400") {
		t.Fatalf("expected first response to be 4xx proxy error, got %s", lines[0])
	}
	if !strings.Contains(lines[1], `"result":{"ok":true}`) {
		t.Fatalf("expected second response to be success, got %s", lines[1])
	}
}

func TestBackoffResetSemantics(t *testing.T) {
	p := &mcpProxy{
		initialBackoff: 20 * time.Millisecond,
		maxBackoff:     200 * time.Millisecond,
	}

	cur := p.initialBackoff
	cur = nextBackoff(cur, p.initialBackoff, p.maxBackoff) // 40ms
	cur = nextBackoff(cur, p.initialBackoff, p.maxBackoff) // 80ms
	cur = p.initialBackoff                                 // reset

	if cur != 20*time.Millisecond {
		t.Fatalf("expected reset to initial backoff, got %v", cur)
	}
}

func TestProxy_DisconnectedInitializeReturnsFallbackSuccess(t *testing.T) {
	var out bytes.Buffer
	p := &mcpProxy{
		serverURL:        "http://127.0.0.1:1/api/v1/mcp/message",
		httpClient:       &http.Client{Timeout: 50 * time.Millisecond},
		stdin:            strings.NewReader(`{"jsonrpc":"2.0","id":42,"method":"initialize","params":{}}` + "\n"),
		stdout:           &out,
		initialBackoff:   10 * time.Millisecond,
		maxBackoff:       20 * time.Millisecond,
		probeTimeout:     10 * time.Millisecond,
		reconnectWakeCh:  make(chan struct{}, 1),
		reconnectResetCh: make(chan struct{}, 1),
	}

	ctx, cancel := context.WithCancel(context.Background())
	err := p.run(ctx)
	cancel()
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}

	line := strings.TrimSpace(out.String())
	if strings.Contains(line, `"error"`) {
		t.Fatalf("expected fallback success response, got error: %s", line)
	}
	if !strings.Contains(line, `"protocolVersion":"2024-11-05"`) {
		t.Fatalf("expected initialize fallback protocolVersion, got %s", line)
	}
}

func TestProxy_DisconnectedToolCallNotificationWritesNoResponse(t *testing.T) {
	var out bytes.Buffer
	p := &mcpProxy{
		serverURL:        "http://127.0.0.1:1/api/v1/mcp/message",
		httpClient:       &http.Client{Timeout: 50 * time.Millisecond},
		stdin:            strings.NewReader(`{"jsonrpc":"2.0","method":"tools/call","params":{"name":"execute_graphql"}}` + "\n"),
		stdout:           &out,
		initialBackoff:   10 * time.Millisecond,
		maxBackoff:       20 * time.Millisecond,
		probeTimeout:     10 * time.Millisecond,
		reconnectWakeCh:  make(chan struct{}, 1),
		reconnectResetCh: make(chan struct{}, 1),
	}

	ctx, cancel := context.WithCancel(context.Background())
	err := p.run(ctx)
	cancel()
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if strings.TrimSpace(out.String()) != "" {
		t.Fatalf("expected no response for tools/call notification, got: %q", out.String())
	}
}
