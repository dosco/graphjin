package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

func runMCPProxy(cmd *cobra.Command, args []string) {
	serverURL := normalizeMCPServerURL(mcpServerURL)
	log.Infof("Proxying MCP requests to: %s", serverURL)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		cancel()
	}()

	proxy := &mcpProxy{
		serverURL:  serverURL,
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}

	if err := proxy.run(ctx); err != nil && ctx.Err() == nil {
		log.Fatalf("MCP proxy error: %s", err)
	}
}

type mcpProxy struct {
	serverURL  string
	httpClient *http.Client

	stdin  io.Reader
	stdout io.Writer

	initialBackoff time.Duration
	maxBackoff     time.Duration
	probeTimeout   time.Duration

	forwardFn func(ctx context.Context, body []byte) ([]byte, error)
	probeFn   func(ctx context.Context) error

	mu             sync.RWMutex
	connected      bool
	lastConnErr    error
	disconnectedAt time.Time
	currentBackoff time.Duration

	reconnectWakeCh  chan struct{}
	reconnectResetCh chan struct{}

	cachedToolsListResult json.RawMessage
}

func (p *mcpProxy) run(ctx context.Context) error {
	p.ensureDefaults()
	reader := bufio.NewReader(p.stdin)
	go p.reconnectLoop(ctx)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Read JSON-RPC message from stdin (newline delimited)
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("reading stdin: %w", err)
		}

		// Skip empty lines
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		meta := parseJSONRPCRequestMeta(line)

		if p.isDisconnected() {
			// A new incoming request should interrupt reconnect backoff and retry now.
			p.signalReconnectReset()
		}

		// Forward to remote server
		resp, err := p.forwardRequest(ctx, line)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}

			if p.isConnectivityError(err) {
				p.markDisconnected(err)
				p.signalReconnectWake()
				log.Warnf("proxy connectivity error: %s", err)

				if meta.Method == "tools/call" {
					p.writeDisconnectedToolCallError(meta)
					continue
				}

				if p.writeFallbackResponse(meta) {
					continue
				}

				p.writeErrorResponse(line, err)
			} else {
				log.Errorf("proxy error: %s", err)
				p.writeErrorResponse(line, err)
			}
			continue
		}

		p.markConnected()
		p.captureResponseCache(meta, resp)

		// Write response to stdout
		p.stdout.Write(resp)
		if len(resp) > 0 && resp[len(resp)-1] != '\n' {
			p.stdout.Write([]byte("\n"))
		}
	}
}

func (p *mcpProxy) writeErrorResponse(request []byte, err error) {
	reqID, ok := extractJSONRPCID(request)
	if !ok {
		return
	}

	errResp := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      reqID,
		"error": map[string]interface{}{
			"code":    -32603,
			"message": fmt.Sprintf("proxy error: %s", err),
		},
	}

	respBytes, _ := json.Marshal(errResp)
	p.stdout.Write(respBytes)
	p.stdout.Write([]byte("\n"))
}

func (p *mcpProxy) forwardRequest(ctx context.Context, body []byte) ([]byte, error) {
	if p.forwardFn != nil {
		return p.forwardFn(ctx, body)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.serverURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	if auth := os.Getenv("GRAPHJIN_MCP_AUTH"); auth != "" {
		req.Header.Set("Authorization", auth)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, &mcpHTTPError{
			StatusCode: resp.StatusCode,
			Body:       string(respBody),
		}
	}

	return respBody, nil
}

func (p *mcpProxy) reconnectLoop(ctx context.Context) {
	delay := p.initialBackoff
	for {
		select {
		case <-ctx.Done():
			return
		case <-p.reconnectWakeCh:
		case <-p.reconnectResetCh:
			delay = p.initialBackoff
			p.setCurrentBackoff(delay)
		}

		for p.isDisconnected() {
			err := p.probeConnectivity(ctx)
			if err == nil {
				p.markConnected()
				delay = p.initialBackoff
				p.setCurrentBackoff(delay)
				break
			}

			p.markDisconnected(err)
			log.Warnf("MCP proxy reconnect failed: %s", err)

			wait := delay
			if wait <= 0 {
				wait = p.initialBackoff
			}
			p.setCurrentBackoff(wait)

			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
				delay = nextBackoff(delay, p.initialBackoff, p.maxBackoff)
				p.setCurrentBackoff(delay)
			case <-p.reconnectResetCh:
				delay = p.initialBackoff
				p.setCurrentBackoff(delay)
			case <-p.reconnectWakeCh:
			}
		}
	}
}

func (p *mcpProxy) probeConnectivity(ctx context.Context) error {
	if p.probeFn != nil {
		return p.probeFn(ctx)
	}

	probeCtx, cancel := context.WithTimeout(ctx, p.probeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(probeCtx, http.MethodHead, p.serverURL, nil)
	if err != nil {
		return err
	}

	if auth := os.Getenv("GRAPHJIN_MCP_AUTH"); auth != "" {
		req.Header.Set("Authorization", auth)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		return &mcpHTTPError{StatusCode: resp.StatusCode}
	}
	return nil
}

func (p *mcpProxy) writeFallbackResponse(meta rpcRequestMeta) bool {
	if meta.IsNotification || !meta.HasID {
		return true
	}

	var result any
	switch meta.Method {
	case "initialize":
		result = map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{
				"tools":     map[string]any{},
				"prompts":   map[string]any{},
				"resources": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "graphjin-proxy",
				"version": version,
			},
		}
	case "ping":
		result = map[string]any{}
	case "tools/list":
		result = map[string]any{"tools": []any{}}
		if cached := p.getCachedToolsListResult(); len(cached) != 0 {
			var decoded map[string]any
			if err := json.Unmarshal(cached, &decoded); err == nil {
				result = decoded
			}
		}
	case "prompts/list":
		result = map[string]any{"prompts": []any{}}
	case "resources/list":
		result = map[string]any{"resources": []any{}}
	case "resources/read":
		result = map[string]any{"contents": []any{}}
	default:
		return false
	}

	resp := map[string]any{
		"jsonrpc": "2.0",
		"id":      meta.ID,
		"result":  result,
	}
	respBytes, _ := json.Marshal(resp)
	p.stdout.Write(respBytes)
	p.stdout.Write([]byte("\n"))
	return true
}

func (p *mcpProxy) ensureDefaults() {
	if p.stdin == nil {
		p.stdin = os.Stdin
	}
	if p.stdout == nil {
		p.stdout = os.Stdout
	}
	if p.initialBackoff <= 0 {
		p.initialBackoff = time.Second
	}
	if p.maxBackoff <= 0 {
		p.maxBackoff = 30 * time.Second
	}
	if p.probeTimeout <= 0 {
		p.probeTimeout = 5 * time.Second
	}
	if p.connected == false && p.disconnectedAt.IsZero() && p.lastConnErr == nil {
		p.connected = true
	}
	if p.reconnectWakeCh == nil {
		p.reconnectWakeCh = make(chan struct{}, 1)
	}
	if p.reconnectResetCh == nil {
		p.reconnectResetCh = make(chan struct{}, 1)
	}
	if p.currentBackoff <= 0 {
		p.currentBackoff = p.initialBackoff
	}
}

func (p *mcpProxy) markDisconnected(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.connected = false
	p.lastConnErr = err
	if p.disconnectedAt.IsZero() {
		p.disconnectedAt = time.Now()
	}
}

func (p *mcpProxy) markConnected() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.connected = true
	p.lastConnErr = nil
	p.disconnectedAt = time.Time{}
	p.currentBackoff = p.initialBackoff
}

func (p *mcpProxy) isDisconnected() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return !p.connected
}

func (p *mcpProxy) signalReconnectWake() {
	select {
	case p.reconnectWakeCh <- struct{}{}:
	default:
	}
}

func (p *mcpProxy) signalReconnectReset() {
	select {
	case p.reconnectResetCh <- struct{}{}:
	default:
	}
}

func (p *mcpProxy) setCurrentBackoff(delay time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.currentBackoff = delay
}

func (p *mcpProxy) getDisconnectedSnapshot() (time.Time, string, time.Duration) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var lastErr string
	if p.lastConnErr != nil {
		lastErr = p.lastConnErr.Error()
	}
	return p.disconnectedAt, lastErr, p.currentBackoff
}

func (p *mcpProxy) setCachedToolsListResult(result json.RawMessage) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cachedToolsListResult = append([]byte(nil), result...)
}

func (p *mcpProxy) getCachedToolsListResult() json.RawMessage {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if len(p.cachedToolsListResult) == 0 {
		return nil
	}
	return append([]byte(nil), p.cachedToolsListResult...)
}

func (p *mcpProxy) isConnectivityError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var httpErr *mcpHTTPError
	if errors.As(err, &httpErr) {
		return httpErr.StatusCode >= 500
	}
	return true
}

type mcpHTTPError struct {
	StatusCode int
	Body       string
}

func (e *mcpHTTPError) Error() string {
	if e.Body != "" {
		return fmt.Sprintf("http error: %d - %s", e.StatusCode, e.Body)
	}
	return fmt.Sprintf("http error: %d", e.StatusCode)
}

func extractJSONRPCID(request []byte) (interface{}, bool) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(request, &envelope); err != nil {
		return nil, false
	}

	rawID, ok := envelope["id"]
	if !ok || len(rawID) == 0 {
		return nil, false
	}

	var id interface{}
	if err := json.Unmarshal(rawID, &id); err != nil {
		return nil, false
	}

	return id, true
}

type rpcRequestMeta struct {
	ID             interface{}
	HasID          bool
	Method         string
	IsNotification bool
	ToolName       string
}

func parseJSONRPCRequestMeta(request []byte) rpcRequestMeta {
	meta := rpcRequestMeta{}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(request, &envelope); err != nil {
		return meta
	}

	if rawMethod, ok := envelope["method"]; ok {
		json.Unmarshal(rawMethod, &meta.Method) //nolint:errcheck
	}
	if rawID, ok := envelope["id"]; ok {
		meta.HasID = true
		json.Unmarshal(rawID, &meta.ID) //nolint:errcheck
	}
	meta.IsNotification = !meta.HasID

	if meta.Method == "tools/call" {
		var req struct {
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		if err := json.Unmarshal(request, &req); err == nil {
			meta.ToolName = req.Params.Name
		}
	}
	return meta
}

func (p *mcpProxy) captureResponseCache(meta rpcRequestMeta, resp []byte) {
	if meta.Method != "tools/list" || len(resp) == 0 {
		return
	}
	var payload struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(resp, &payload); err != nil {
		return
	}
	if len(payload.Result) == 0 {
		return
	}
	p.setCachedToolsListResult(payload.Result)
}

func (p *mcpProxy) writeDisconnectedToolCallError(meta rpcRequestMeta) {
	if meta.IsNotification || !meta.HasID {
		return
	}
	const msg = "GraphJin MCP proxy is disconnected from the upstream server. This tool call was not executed. Background reconnect is active; please retry this same tool call shortly."
	downSince, lastErr, backoff := p.getDisconnectedSnapshot()
	if backoff <= 0 {
		backoff = p.initialBackoff
	}
	data := map[string]any{
		"type":                     "UPSTREAM_DISCONNECTED",
		"retryable":                true,
		"background_reconnect":     true,
		"upstream":                 p.serverURL,
		"suggested_retry_delay_ms": int(backoff / time.Millisecond),
	}
	if !downSince.IsZero() {
		data["disconnected_since"] = downSince.UTC().Format(time.RFC3339)
	}
	if lastErr != "" {
		data["last_error"] = lastErr
	}
	if meta.ToolName != "" {
		data["tool_name"] = meta.ToolName
	}

	resp := map[string]any{
		"jsonrpc": "2.0",
		"id":      meta.ID,
		"error": map[string]any{
			"code":    -32001,
			"message": msg,
			"data":    data,
		},
	}
	respBytes, _ := json.Marshal(resp)
	p.stdout.Write(respBytes)
	p.stdout.Write([]byte("\n"))
}

func nextBackoff(current, initial, max time.Duration) time.Duration {
	if current <= 0 {
		return initial
	}
	n := current * 2
	if n > max {
		return max
	}
	return n
}

func normalizeMCPServerURL(input string) string {
	// Add scheme if missing
	if !strings.HasPrefix(input, "http://") && !strings.HasPrefix(input, "https://") {
		input = "http://" + input
	}

	u, err := url.Parse(input)
	if err != nil {
		return input
	}

	// Append MCP endpoint if path is empty
	if u.Path == "" || u.Path == "/" {
		u.Path = "/api/v1/mcp/message"
	}

	return u.String()
}

// printMCPProxyConfig outputs the Claude Desktop configuration JSON for proxy mode
func printMCPProxyConfig(serverURL string) {
	execPath, err := os.Executable()
	if err != nil {
		log.Fatalf("Failed to get executable path: %s", err)
	}

	mcpConfig := map[string]interface{}{
		"mcpServers": map[string]interface{}{
			"GraphJin": map[string]interface{}{
				"command": execPath,
				"args":    []string{"mcp", "--server", serverURL},
			},
		},
	}

	output, err := json.MarshalIndent(mcpConfig, "", "  ")
	if err != nil {
		log.Fatalf("Failed to marshal config: %s", err)
	}

	fmt.Println(string(output))
}
