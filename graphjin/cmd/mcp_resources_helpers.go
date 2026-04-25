package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func callMCPMethod(ctx context.Context, cmd *cobra.Command, method string, params map[string]any) (json.RawMessage, error) {
	serverURL := resolveMCPServerURL(cmd)
	if serverURL == "" {
		return nil, errors.New("no GraphJin server configured — run `graphjin cli setup <server-url>` first")
	}

	reqID := mcpClientReqID.Add(1)
	rpc := jsonRPCRequest{JSONRPC: "2.0", ID: reqID, Method: method, Params: params}
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
	return envelope.Result, nil
}

func readMCPResource(ctx context.Context, cmd *cobra.Command, uri string) (json.RawMessage, error) {
	return callMCPMethod(ctx, cmd, "resources/read", map[string]any{"uri": uri})
}
