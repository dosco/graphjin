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
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

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

// resetMCPClientFlags clears global flag state between tests.
func resetMCPClientFlags(serverURL string) {
	mcpServerURL = serverURL
	mcpClientToken = ""
	mcpClientHeaders = nil
	mcpClientTimeout = 0
	mcpClientFormat = "json"
	_ = os.Unsetenv("GRAPHJIN_TOKEN")
	_ = os.Unsetenv("GRAPHJIN_MCP_AUTH")
	_ = os.Unsetenv("GRAPHJIN_SERVER")
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
	if !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "--token") {
		t.Fatalf("expected 401 + token hint, got %v", err)
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

	mcpClientToken = "shh"
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

func TestResolveMCPServerURL_Precedence(t *testing.T) {
	// Explicit flag wins.
	resetMCPClientFlags("http://explicit.example:9000/")
	got := resolveMCPServerURL(newEmptyCobraCmd())
	if !strings.Contains(got, "explicit.example") {
		t.Fatalf("explicit URL not honored: %s", got)
	}

	// Env wins when flag empty.
	resetMCPClientFlags("")
	t.Setenv("GRAPHJIN_SERVER", "http://env.example/")
	got = resolveMCPServerURL(newEmptyCobraCmd())
	if !strings.Contains(got, "env.example") {
		t.Fatalf("env URL not honored: %s", got)
	}

	// Default otherwise.
	resetMCPClientFlags("")
	got = resolveMCPServerURL(newEmptyCobraCmd())
	if !strings.Contains(got, "localhost:8080") {
		t.Fatalf("default URL not used: %s", got)
	}
}

func TestResolveMCPAuth_Precedence(t *testing.T) {
	// --token beats env.
	resetMCPClientFlags("")
	mcpClientToken = "flag-token"
	t.Setenv("GRAPHJIN_TOKEN", "env-token")
	if got := resolveMCPAuth(); got != "Bearer flag-token" {
		t.Fatalf("flag precedence: %q", got)
	}

	// GRAPHJIN_TOKEN beats raw GRAPHJIN_MCP_AUTH when flag empty.
	resetMCPClientFlags("")
	t.Setenv("GRAPHJIN_TOKEN", "env-token")
	t.Setenv("GRAPHJIN_MCP_AUTH", "Basic xyz")
	if got := resolveMCPAuth(); got != "Bearer env-token" {
		t.Fatalf("env token: %q", got)
	}

	// Raw MCP_AUTH used when nothing else.
	resetMCPClientFlags("")
	t.Setenv("GRAPHJIN_MCP_AUTH", "Basic xyz")
	if got := resolveMCPAuth(); got != "Basic xyz" {
		t.Fatalf("raw mcp auth: %q", got)
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
