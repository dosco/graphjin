package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"
)

// MCP HTTP-client persistent flags. Attached to the `cli` and `mcp` parents
// so every client-mode subcommand inherits them.
//
// Important: there is intentionally no --server or --token flag. Both come
// from `~/.config/graphjin/client.json`, written by `graphjin cli setup` /
// `graphjin mcp setup`. Having a single source of truth avoids the "which
// server am I actually talking to?" confusion and means MCP integrations
// (Claude Desktop, Codex) don't need to embed credentials in their configs.
var (
	mcpClientHeaders []string
	mcpClientTimeout time.Duration
	mcpClientFormat  string
)

// addMCPClientFlags attaches the shared persistent flags for all client-mode
// subcommands to the given parent command. Call once on the `cli` parent.
func addMCPClientFlags(c *cobra.Command) {
	c.PersistentFlags().StringArrayVar(&mcpClientHeaders, "header", nil,
		"Extra HTTP header 'Key: Value' (repeatable)")
	c.PersistentFlags().DurationVar(&mcpClientTimeout, "timeout", 60*time.Second,
		"Request timeout for MCP HTTP calls")
	c.PersistentFlags().StringVar(&mcpClientFormat, "format", "json",
		"Output format for client-mode commands: json|table (default json)")
}

// mcpClientRedirectLog sends the zap logger to stderr so stdout stays clean
// JSON for pipe/file workflows. Must be called at the top of every
// client-mode PreRun/Run.
func mcpClientRedirectLog() {
	log = newLoggerWithOutput(false, os.Stderr).Sugar()
}

// resolveMCPServerURL returns the saved server URL, normalized to point at
// the MCP HTTP endpoint. Returns "" if `graphjin cli setup` has not been run
// yet — callers must check and surface the "run setup" hint.
func resolveMCPServerURL(_ *cobra.Command) string {
	cc, _ := LoadClientConfig()
	if cc == nil || cc.Server == "" {
		return ""
	}
	return normalizeMCPServerURL(cc.Server)
}

// resolveMCPAuth returns the Authorization header value (Bearer <token>) to
// send on every MCP HTTP request, or "" when the saved config has no token
// (e.g. server has auth_login disabled).
func resolveMCPAuth() string {
	cc, _ := LoadClientConfig()
	if cc == nil || cc.Token == "" {
		return ""
	}
	return "Bearer " + cc.Token
}

// mcpClientReqID supplies monotonically increasing JSON-RPC request IDs.
var mcpClientReqID atomic.Int64

// jsonRPCRequest is the envelope sent to the MCP HTTP endpoint.
type jsonRPCRequest struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      int64          `json:"id"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params,omitempty"`
}

// jsonRPCResponse is the envelope returned by the MCP HTTP endpoint.
type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// mcpToolResult mirrors mcp.CallToolResult — we only need the fields the
// server produces via mcpToolResultJSONBytes in serv/mcp.go.
type mcpToolResult struct {
	Content           []mcpContent    `json:"content"`
	StructuredContent json.RawMessage `json:"structuredContent,omitempty"`
	IsError           bool            `json:"isError,omitempty"`
}

type mcpContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// callTool POSTs a tools/call JSON-RPC request to the MCP server and returns
// the tool's payload as raw JSON (structured if present, otherwise the text
// content parsed as JSON, otherwise the raw text wrapped in a JSON string).
func callTool(ctx context.Context, cmd *cobra.Command, toolName string, args map[string]any) (json.RawMessage, error) {
	serverURL := resolveMCPServerURL(cmd)
	if serverURL == "" {
		return nil, errors.New("no GraphJin server configured — run `graphjin cli setup <server-url>` first")
	}

	reqID := mcpClientReqID.Add(1)
	params := map[string]any{"name": toolName}
	if args != nil {
		params["arguments"] = args
	}
	rpc := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      reqID,
		Method:  "tools/call",
		Params:  params,
	}
	body, err := json.Marshal(rpc)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	timeout := mcpClientTimeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	httpClient := &http.Client{Timeout: timeout}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, serverURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	// StreamableHTTPServer may choose text/event-stream when it wants to
	// stream; accepting both lets the server pick. The mcp-go server returns
	// application/json for short responses in stateless mode.
	req.Header.Set("Accept", "application/json, text/event-stream")

	if auth := resolveMCPAuth(); auth != "" {
		req.Header.Set("Authorization", auth)
	}
	for _, h := range mcpClientHeaders {
		idx := strings.Index(h, ":")
		if idx <= 0 {
			return nil, fmt.Errorf("invalid --header %q (want 'Key: Value')", h)
		}
		key := strings.TrimSpace(h[:idx])
		val := strings.TrimSpace(h[idx+1:])
		if key == "" {
			return nil, fmt.Errorf("invalid --header %q (empty key)", h)
		}
		req.Header.Set(key, val)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		hint := ""
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			hint = " (token expired or invalid — re-run `graphjin cli setup`)"
		}
		return nil, fmt.Errorf("server returned HTTP %d%s: %s", resp.StatusCode, hint, strings.TrimSpace(string(respBody)))
	}

	// The streamable HTTP transport may respond with an SSE stream or a plain
	// JSON envelope. Detect and extract the data payload for both.
	payload, err := extractJSONRPCPayload(respBody, resp.Header.Get("Content-Type"))
	if err != nil {
		return nil, err
	}

	var envelope jsonRPCResponse
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return nil, fmt.Errorf("decode JSON-RPC response: %w (body: %s)", err, string(payload))
	}
	if envelope.Error != nil {
		return nil, fmt.Errorf("MCP error %d: %s", envelope.Error.Code, envelope.Error.Message)
	}
	if len(envelope.Result) == 0 {
		return nil, errors.New("empty MCP response")
	}

	var result mcpToolResult
	if err := json.Unmarshal(envelope.Result, &result); err != nil {
		// Not a CallToolResult envelope — return raw result.
		return envelope.Result, nil
	}

	if result.IsError {
		msg := ""
		if len(result.Content) > 0 {
			msg = result.Content[0].Text
		}
		return nil, fmt.Errorf("tool error: %s", msg)
	}

	if len(result.StructuredContent) > 0 {
		return result.StructuredContent, nil
	}
	if len(result.Content) > 0 && result.Content[0].Text != "" {
		// Try parsing the text as JSON; fall back to a JSON string.
		text := result.Content[0].Text
		var probe any
		if err := json.Unmarshal([]byte(text), &probe); err == nil {
			return json.RawMessage(text), nil
		}
		b, _ := json.Marshal(text)
		return b, nil
	}
	return json.RawMessage("null"), nil
}

// extractJSONRPCPayload returns the JSON-RPC envelope bytes from either an
// application/json body or a text/event-stream body (picks the first `data:`
// frame that parses as a JSON-RPC response).
func extractJSONRPCPayload(body []byte, contentType string) ([]byte, error) {
	ct := strings.ToLower(contentType)
	if strings.Contains(ct, "text/event-stream") {
		lines := strings.Split(string(body), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "" {
				continue
			}
			// Return the first data frame that parses as JSON-RPC.
			var probe jsonRPCResponse
			if err := json.Unmarshal([]byte(data), &probe); err == nil {
				return []byte(data), nil
			}
		}
		return nil, fmt.Errorf("no JSON-RPC frame in SSE response: %s", string(body))
	}
	return body, nil
}

// emitResult prints the tool's JSON payload according to --format. For
// --format=table it tries to render a simple table for arrays of flat
// objects; otherwise it falls back to pretty JSON with a stderr notice.
func emitResult(payload json.RawMessage) error {
	format := strings.ToLower(strings.TrimSpace(mcpClientFormat))
	switch format {
	case "", "json":
		// Pretty-print for readability; single-line JSON still parses by jq.
		return writePrettyJSON(os.Stdout, payload)
	case "table":
		if ok, err := writeTable(os.Stdout, payload); ok {
			return err
		}
		fmt.Fprintln(os.Stderr, "notice: --format=table not applicable to this output; falling back to JSON")
		return writePrettyJSON(os.Stdout, payload)
	default:
		return fmt.Errorf("unknown --format %q (want json|table)", mcpClientFormat)
	}
}

func writePrettyJSON(w io.Writer, payload json.RawMessage) error {
	var v any
	if err := json.Unmarshal(payload, &v); err != nil {
		// Not valid JSON — emit as-is.
		_, werr := w.Write(payload)
		if werr == nil {
			_, _ = w.Write([]byte("\n"))
		}
		return werr
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// writeTable renders an array of flat objects as a simple aligned table.
// Returns (true, err) if it produced output (success or write-failure) and
// (false, nil) if the input shape is not table-compatible.
func writeTable(w io.Writer, payload json.RawMessage) (bool, error) {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return false, nil
	}
	var rows []map[string]any
	if err := json.Unmarshal(payload, &rows); err != nil {
		return false, nil
	}
	if len(rows) == 0 {
		fmt.Fprintln(w, "(empty)")
		return true, nil
	}

	// Collect column order from first row keys, then append any extra keys.
	var cols []string
	seen := map[string]bool{}
	for _, r := range rows {
		for k := range r {
			if !seen[k] {
				seen[k] = true
				cols = append(cols, k)
			}
		}
	}

	// Every value must be a scalar — otherwise bail.
	for _, r := range rows {
		for _, v := range r {
			switch v.(type) {
			case nil, bool, float64, string:
				// ok
			default:
				return false, nil
			}
		}
	}

	widths := make([]int, len(cols))
	for i, c := range cols {
		widths[i] = len(c)
	}
	cells := make([][]string, len(rows))
	for ri, r := range rows {
		row := make([]string, len(cols))
		for ci, c := range cols {
			row[ci] = scalarToString(r[c])
			if l := len(row[ci]); l > widths[ci] {
				widths[ci] = l
			}
		}
		cells[ri] = row
	}

	// Header
	for i, c := range cols {
		if i > 0 {
			fmt.Fprint(w, "  ")
		}
		fmt.Fprintf(w, "%-*s", widths[i], c)
	}
	fmt.Fprintln(w)
	for i := range cols {
		if i > 0 {
			fmt.Fprint(w, "  ")
		}
		fmt.Fprint(w, strings.Repeat("-", widths[i]))
	}
	fmt.Fprintln(w)
	for _, row := range cells {
		for i, v := range row {
			if i > 0 {
				fmt.Fprint(w, "  ")
			}
			fmt.Fprintf(w, "%-*s", widths[i], v)
		}
		fmt.Fprintln(w)
	}
	return true, nil
}

func scalarToString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case float64:
		// Print integers without decimals.
		if x == float64(int64(x)) {
			return fmt.Sprintf("%d", int64(x))
		}
		return fmt.Sprintf("%g", x)
	default:
		return fmt.Sprint(v)
	}
}

// loadVars resolves --vars / --vars-file into a map for the MCP tool call.
// --vars takes precedence over --vars-file; --vars-file "-" reads stdin.
func loadVars(varsJSON, varsFile string) (map[string]any, error) {
	var raw []byte
	switch {
	case varsJSON != "":
		raw = []byte(varsJSON)
	case varsFile == "-":
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("read --vars-file stdin: %w", err)
		}
		raw = b
	case varsFile != "":
		b, err := os.ReadFile(varsFile)
		if err != nil {
			return nil, fmt.Errorf("read --vars-file: %w", err)
		}
		raw = b
	default:
		return nil, nil
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, nil
	}
	var v map[string]any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("parse variables JSON: %w", err)
	}
	return v, nil
}

// readGraphQLArg resolves a positional GraphQL argument. If the value is "-",
// reads from stdin. Otherwise returns the value unchanged.
func readGraphQLArg(arg string) (string, error) {
	if arg != "-" {
		return arg, nil
	}
	b, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", fmt.Errorf("read stdin: %w", err)
	}
	return string(bytes.TrimSpace(b)), nil
}

// runToolCmd is a shared Run function: marshals args, calls the tool, emits
// the result. Any error exits the process with status 1.
// cleanToolArgs strips nil / empty values so server-side defaults apply.
func cleanToolArgs(args map[string]any) map[string]any {
	clean := map[string]any{}
	for k, v := range args {
		switch vv := v.(type) {
		case nil:
			continue
		case string:
			if vv == "" {
				continue
			}
			clean[k] = vv
		case map[string]any:
			if len(vv) == 0 {
				continue
			}
			clean[k] = vv
		default:
			clean[k] = v
		}
	}
	return clean
}

// isToolNotFound reports whether err is a JSON-RPC "tool not found" (-32602),
// which a catalog-first (sources mode) server returns for legacy discovery tools.
func isToolNotFound(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "tool not found") ||
		(strings.Contains(s, "-32602") && strings.Contains(s, "not found"))
}

func runToolCmd(cmd *cobra.Command, toolName string, args map[string]any) {
	runToolCmdWithFallback(cmd, toolName, args, "", nil)
}

// runToolCmdWithFallback calls toolName; if the server does not expose it (a
// sources-mode server hides legacy discovery tools behind the catalog), it
// retries fallbackTool. This lets schema commands work against both legacy and
// catalog-first servers without the caller knowing which mode is in use.
func runToolCmdWithFallback(cmd *cobra.Command, toolName string, args map[string]any, fallbackTool string, fallbackArgs map[string]any) {
	mcpClientRedirectLog()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	payload, err := callTool(ctx, cmd, toolName, cleanToolArgs(args))
	if err != nil && fallbackTool != "" && isToolNotFound(err) {
		payload, err = callTool(ctx, cmd, fallbackTool, cleanToolArgs(fallbackArgs))
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
	if err := emitResult(payload); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}

// emitSchemaTables lists tables: it tries the legacy list_tables tool first and,
// when a sources-mode server hides it, pages query_catalog to completion so the
// full table set is returned in one command rather than a truncated first page.
func emitSchemaTables(cmd *cobra.Command, database string) {
	mcpClientRedirectLog()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if payload, err := callTool(ctx, cmd, "list_tables", cleanToolArgs(map[string]any{"database": database})); err == nil {
		emitOrExit(payload)
		return
	} else if !isToolNotFound(err) {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}

	where := map[string]any{"kind": map[string]any{"eq": "table"}}
	if database != "" {
		where["database_name"] = map[string]any{"eq": database}
	}
	merged, err := callCatalogPaginated(ctx, cmd, where, 500)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
	emitOrExit(merged)
}

func emitOrExit(payload json.RawMessage) {
	if err := emitResult(payload); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}

// callCatalogPaginated pages query_catalog (offset += limit) until the server
// stops reporting truncated, then returns one merged result holding every card.
// Servers predating the truncated/offset fields simply return one page and stop.
func callCatalogPaginated(ctx context.Context, cmd *cobra.Command, where map[string]any, pageLimit int) (json.RawMessage, error) {
	const maxPages = 100 // safety bound (maxPages*pageLimit items) against a stuck truncated flag
	var merged map[string]any
	var allCards []any
	offset := 0

	for page := 0; page < maxPages; page++ {
		args := map[string]any{"where": where, "limit": pageLimit}
		if offset > 0 {
			args["offset"] = offset
		}
		raw, err := callTool(ctx, cmd, "query_catalog", args)
		if err != nil {
			return nil, err
		}
		var obj map[string]any
		if err := json.Unmarshal(raw, &obj); err != nil {
			return nil, fmt.Errorf("decode query_catalog page: %w", err)
		}
		if merged == nil {
			merged = obj
		}
		cards, _ := obj["cards"].([]any)
		allCards = append(allCards, cards...)
		if truncated, _ := obj["truncated"].(bool); !truncated || len(cards) == 0 {
			break
		}
		offset += pageLimit
	}
	if merged == nil {
		merged = map[string]any{}
	}
	merged["cards"] = allCards
	merged["count"] = len(allCards)
	merged["truncated"] = false
	delete(merged, "offset")
	delete(merged, "next")
	return json.Marshal(merged)
}
