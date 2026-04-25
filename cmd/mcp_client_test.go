package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestMain ensures the test process has an isolated config dir so tests
// can read/write client.json without clobbering the developer's real one.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "graphjin-cmd-test-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	initIsolatedConfigDir(dir)
	os.Exit(m.Run())
}

// newTestMCPServer returns an httptest.Server that mimics GraphJin's MCP
// HTTP transport enough to satisfy callTool. The handler inspects the
// request, applies the provided modifier, and sends a JSON-RPC response
// whose result is either the given CallToolResult (if non-nil) or a default
// echo of the tool arguments.
func newTestMCPServer(t *testing.T, handler func(req *jsonRPCRequest, httpReq *http.Request) (any, *jsonRPCError, int)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var req jsonRPCRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		result, rpcErr, status := handler(&req, r)
		if status == 0 {
			status = http.StatusOK
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if status >= 400 {
			_, _ = w.Write([]byte(fmt.Sprintf("HTTP %d", status)))
			return
		}
		envelope := map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
		}
		if rpcErr != nil {
			envelope["error"] = rpcErr
		} else {
			envelope["result"] = result
		}
		_ = json.NewEncoder(w).Encode(envelope)
	}))
}

// resetMCPClientFlags clears the per-process client flags and seeds an
// isolated client.json with the given server (token = "" — set with
// seedTestClientToken if needed). Pass serverURL = "" to leave the saved
// config absent (simulating "user hasn't run setup yet").
func resetMCPClientFlags(serverURL string) {
	mcpClientHeaders = nil
	mcpClientTimeout = 0
	mcpClientFormat = "json"
	if testConfigDir == "" {
		// initIsolatedConfigDir must be called from TestMain.
		panic("test setup error: testConfigDir not initialized")
	}
	if serverURL == "" {
		_ = DeleteClientConfig()
		return
	}
	if err := SaveClientConfig(&ClientConfig{Server: serverURL}); err != nil {
		panic(err)
	}
}

// seedTestClientToken updates the saved client.json with a token (server
// must already be set via resetMCPClientFlags).
func seedTestClientToken(token string) {
	cc, err := LoadClientConfig()
	if err != nil || cc == nil {
		panic("seedTestClientToken: no client.json — call resetMCPClientFlags first")
	}
	cc.Token = token
	if err := SaveClientConfig(cc); err != nil {
		panic(err)
	}
}

var testConfigDir string

// initIsolatedConfigDir redirects os.UserConfigDir() to a per-test-process
// scratch directory so the real `~/.config/graphjin/client.json` is never
// touched. Called from TestMain.
func initIsolatedConfigDir(dir string) {
	testConfigDir = dir
	_ = os.Setenv("XDG_CONFIG_HOME", dir)
	_ = os.Setenv("HOME", dir)
	if runtime.GOOS == "windows" {
		_ = os.Setenv("AppData", dir)
	}
}

// newEmptyCobraCmd returns a fresh cobra command suitable for passing to
// callTool/resolveMCPServerURL without triggering os.Exit paths.
func newEmptyCobraCmd() *cobra.Command {
	return &cobra.Command{Use: "test"}
}

func TestCallTool_ReturnsStructuredContent(t *testing.T) {
	srv := newTestMCPServer(t, func(req *jsonRPCRequest, _ *http.Request) (any, *jsonRPCError, int) {
		if req.Method != "tools/call" {
			t.Fatalf("method = %q, want tools/call", req.Method)
		}
		if req.Params["name"] != "list_tables" {
			t.Fatalf("tool name = %v, want list_tables", req.Params["name"])
		}
		return mcpToolResult{
			StructuredContent: json.RawMessage(`[{"name":"users"},{"name":"orders"}]`),
			Content: []mcpContent{
				{Type: "text", Text: `[{"name":"users"},{"name":"orders"}]`},
			},
		}, nil, http.StatusOK
	})
	defer srv.Close()
	resetMCPClientFlags(srv.URL)

	payload, err := callTool(context.Background(), newEmptyCobraCmd(), "list_tables", nil)
	if err != nil {
		t.Fatalf("callTool: %v", err)
	}
	var tables []map[string]string
	if err := json.Unmarshal(payload, &tables); err != nil {
		t.Fatalf("unmarshal payload: %v (payload=%s)", err, string(payload))
	}
	if len(tables) != 2 || tables[0]["name"] != "users" {
		t.Fatalf("tables = %v", tables)
	}
}

func TestCallTool_FallsBackToTextContent(t *testing.T) {
	srv := newTestMCPServer(t, func(*jsonRPCRequest, *http.Request) (any, *jsonRPCError, int) {
		return mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: `{"ok":true}`}},
		}, nil, http.StatusOK
	})
	defer srv.Close()
	resetMCPClientFlags(srv.URL)

	payload, err := callTool(context.Background(), newEmptyCobraCmd(), "any_tool", nil)
	if err != nil {
		t.Fatalf("callTool: %v", err)
	}
	if string(payload) != `{"ok":true}` {
		t.Fatalf("payload = %s", string(payload))
	}
}

func TestCallTool_SurfacesToolError(t *testing.T) {
	srv := newTestMCPServer(t, func(*jsonRPCRequest, *http.Request) (any, *jsonRPCError, int) {
		return mcpToolResult{
			IsError: true,
			Content: []mcpContent{{Type: "text", Text: "raw queries are not allowed"}},
		}, nil, http.StatusOK
	})
	defer srv.Close()
	resetMCPClientFlags(srv.URL)

	_, err := callTool(context.Background(), newEmptyCobraCmd(), "execute_graphql", map[string]any{"query": "x"})
	if err == nil || !strings.Contains(err.Error(), "raw queries are not allowed") {
		t.Fatalf("expected gated tool error, got %v", err)
	}
}

func TestCallTool_SurfacesJSONRPCError(t *testing.T) {
	srv := newTestMCPServer(t, func(*jsonRPCRequest, *http.Request) (any, *jsonRPCError, int) {
		return nil, &jsonRPCError{Code: -32601, Message: "method not found"}, http.StatusOK
	})
	defer srv.Close()
	resetMCPClientFlags(srv.URL)

	_, err := callTool(context.Background(), newEmptyCobraCmd(), "missing_tool", nil)
	if err == nil || !strings.Contains(err.Error(), "method not found") {
		t.Fatalf("expected RPC error, got %v", err)
	}
}

func TestCallTool_SurfacesHTTP401WithHint(t *testing.T) {
	srv := newTestMCPServer(t, func(*jsonRPCRequest, *http.Request) (any, *jsonRPCError, int) {
		return nil, nil, http.StatusUnauthorized
	})
	defer srv.Close()
	resetMCPClientFlags(srv.URL)

	_, err := callTool(context.Background(), newEmptyCobraCmd(), "list_tables", nil)
	if err == nil {
		t.Fatal("expected error on 401")
	}
	if !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "graphjin cli setup") {
		t.Fatalf("expected 401 + setup hint, got %v", err)
	}
}

func TestCallTool_ForwardsAuthAndHeaders(t *testing.T) {
	var seen http.Header
	srv := newTestMCPServer(t, func(_ *jsonRPCRequest, r *http.Request) (any, *jsonRPCError, int) {
		seen = r.Header.Clone()
		return mcpToolResult{StructuredContent: json.RawMessage(`{}`)}, nil, http.StatusOK
	})
	defer srv.Close()
	resetMCPClientFlags(srv.URL)
	seedTestClientToken("shh")

	mcpClientHeaders = []string{"X-Trace: abc123", "X-Env:prod"}

	_, err := callTool(context.Background(), newEmptyCobraCmd(), "list_tables", nil)
	if err != nil {
		t.Fatalf("callTool: %v", err)
	}
	if got := seen.Get("Authorization"); got != "Bearer shh" {
		t.Fatalf("Authorization = %q", got)
	}
	if got := seen.Get("X-Trace"); got != "abc123" {
		t.Fatalf("X-Trace = %q", got)
	}
	if got := seen.Get("X-Env"); got != "prod" {
		t.Fatalf("X-Env = %q", got)
	}
}

func TestCallTool_RejectsMalformedHeader(t *testing.T) {
	resetMCPClientFlags("http://127.0.0.1:0/")
	mcpClientHeaders = []string{"no-colon-here"}

	_, err := callTool(context.Background(), newEmptyCobraCmd(), "list_tables", nil)
	if err == nil || !strings.Contains(err.Error(), "invalid --header") {
		t.Fatalf("expected invalid-header error, got %v", err)
	}
}

func TestCallTool_HandlesSSEResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req jsonRPCRequest
		_ = json.Unmarshal(body, &req)
		envelope := map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result": mcpToolResult{
				StructuredContent: json.RawMessage(`{"sse":true}`),
			},
		}
		b, _ := json.Marshal(envelope)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "event: message\ndata: %s\n\n", string(b))
	}))
	defer srv.Close()
	resetMCPClientFlags(srv.URL)

	payload, err := callTool(context.Background(), newEmptyCobraCmd(), "list_tables", nil)
	if err != nil {
		t.Fatalf("callTool: %v", err)
	}
	if string(payload) != `{"sse":true}` {
		t.Fatalf("payload = %s", string(payload))
	}
}

func TestResolveMCPServerURL_FromClientJSON(t *testing.T) {
	resetMCPClientFlags("http://saved.example:9000/")
	got := resolveMCPServerURL(newEmptyCobraCmd())
	if !strings.Contains(got, "saved.example") {
		t.Fatalf("client.json URL not honored: %s", got)
	}

	// No client.json → empty (forces callers to surface "run setup" hint).
	resetMCPClientFlags("")
	if got := resolveMCPServerURL(newEmptyCobraCmd()); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestResolveMCPAuth_FromClientJSON(t *testing.T) {
	// Token from client.json is the only source.
	resetMCPClientFlags("http://saved.example/")
	seedTestClientToken("saved-token")
	if got := resolveMCPAuth(); got != "Bearer saved-token" {
		t.Fatalf("client.json token: %q", got)
	}

	// No token → empty (server may not need auth).
	resetMCPClientFlags("http://saved.example/")
	if got := resolveMCPAuth(); got != "" {
		t.Fatalf("expected empty when no token, got %q", got)
	}

	// No client.json at all → empty.
	resetMCPClientFlags("")
	if got := resolveMCPAuth(); got != "" {
		t.Fatalf("expected empty without client.json, got %q", got)
	}
}

func TestCallTool_NoServerConfigured(t *testing.T) {
	resetMCPClientFlags("")
	_, err := callTool(context.Background(), newEmptyCobraCmd(), "list_tables", nil)
	if err == nil || !strings.Contains(err.Error(), "graphjin cli setup") {
		t.Fatalf("expected setup hint, got %v", err)
	}
}

func TestEmitResult_TableForFlatArray(t *testing.T) {
	var buf bytes.Buffer
	ok, err := writeTable(&buf, json.RawMessage(`[{"name":"users","rows":10},{"name":"orders","rows":125}]`))
	if err != nil {
		t.Fatalf("writeTable err: %v", err)
	}
	if !ok {
		t.Fatal("expected writeTable to handle flat array")
	}
	out := buf.String()
	if !strings.Contains(out, "name") || !strings.Contains(out, "rows") {
		t.Fatalf("missing header: %s", out)
	}
	if !strings.Contains(out, "users") || !strings.Contains(out, "10") {
		t.Fatalf("missing data: %s", out)
	}
}

func TestEmitResult_TableRejectsNested(t *testing.T) {
	var buf bytes.Buffer
	ok, _ := writeTable(&buf, json.RawMessage(`[{"name":"users","cols":[{"id":1}]}]`))
	if ok {
		t.Fatalf("expected writeTable to reject nested value; got %s", buf.String())
	}
}

func TestLoadVars_InlineAndFile(t *testing.T) {
	got, err := loadVars(`{"id":1}`, "")
	if err != nil {
		t.Fatalf("inline: %v", err)
	}
	if fmt.Sprint(got["id"]) != "1" {
		t.Fatalf("inline parse: %v", got)
	}

	tmp, err := os.CreateTemp(t.TempDir(), "vars.json")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = tmp.WriteString(`{"role":"admin"}`)
	_ = tmp.Close()
	got, err = loadVars("", tmp.Name())
	if err != nil {
		t.Fatalf("file: %v", err)
	}
	if got["role"] != "admin" {
		t.Fatalf("file parse: %v", got)
	}

	if got, err := loadVars("", ""); err != nil || got != nil {
		t.Fatalf("empty: got=%v err=%v", got, err)
	}
}
