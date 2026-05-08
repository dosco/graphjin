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
	"syscall"
	"time"
)

// resolveGraphQLURL turns the saved MCP URL (.../api/v1/mcp/message) into the
// GraphQL endpoint (.../api/v1/graphql) that serves SSE subscriptions. Both
// hang off the same `serv` HTTP service.
func resolveGraphQLURL(mcpURL string) (string, error) {
	if mcpURL == "" {
		return "", errors.New("no GraphJin server configured — run `graphjin cli setup <server-url>` first")
	}
	u, err := url.Parse(mcpURL)
	if err != nil {
		return "", fmt.Errorf("parse server URL: %w", err)
	}
	u.Path = "/api/v1/graphql"
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

// ssePayload mirrors serv.Payload — the JSON written into each `event: next`
// data frame on the server side ([serv/sse.go]).
type ssePayload struct {
	Data   json.RawMessage `json:"data,omitempty"`
	Errors json.RawMessage `json:"errors,omitempty"`
}

// runSubscribeStream POSTs an SSE subscription against the GraphJin GraphQL
// endpoint and feeds each `next` frame into emit. It returns when the server
// sends `event: complete`, when the context is cancelled, or on a transport
// error. Cancellation is the normal exit path for an interactive CLI run.
//
// Output goes through the emit callback so tests can inject a buffer-writer
// in place of the production emitResult (which targets stdout and honors
// --format=table).
func runSubscribeStream(
	ctx context.Context,
	gqlURL string,
	auth string,
	extraHeaders []string,
	query string,
	vars map[string]any,
	namespace string,
	emit func(json.RawMessage) error,
	stderr io.Writer,
) error {
	body := map[string]any{"query": query}
	if len(vars) > 0 {
		body["variables"] = vars
	}
	if namespace != "" {
		body["namespace"] = namespace
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, gqlURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	for _, h := range extraHeaders {
		idx := strings.Index(h, ":")
		if idx <= 0 {
			return fmt.Errorf("invalid --header %q (want 'Key: Value')", h)
		}
		key := strings.TrimSpace(h[:idx])
		val := strings.TrimSpace(h[idx+1:])
		if key == "" {
			return fmt.Errorf("invalid --header %q (empty key)", h)
		}
		req.Header.Set(key, val)
	}

	client := &http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			ResponseHeaderTimeout: 30 * time.Second,
			DisableCompression:    true,
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		hint := ""
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			hint = " (token expired or invalid — re-run `graphjin cli setup`)"
		}
		return fmt.Errorf("server returned HTTP %d%s: %s", resp.StatusCode, hint, strings.TrimSpace(string(respBody)))
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)

	var event string
	var dataBuf bytes.Buffer

	dispatch := func() error {
		defer func() {
			event = ""
			dataBuf.Reset()
		}()
		raw := bytes.TrimRight(dataBuf.Bytes(), "\n")
		switch event {
		case "next":
			if len(raw) == 0 {
				return nil
			}
			payload := append([]byte(nil), raw...)
			var probe ssePayload
			if err := json.Unmarshal(payload, &probe); err == nil && len(probe.Errors) > 0 {
				fmt.Fprintf(stderr, "subscription errors: %s\n", string(probe.Errors))
			}
			return emit(json.RawMessage(payload))
		case "complete":
			return io.EOF
		}
		return nil
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := dispatch(); err != nil {
				if errors.Is(err, io.EOF) {
					return nil
				}
				return err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "event:") {
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if strings.HasPrefix(line, "data:") {
			data := strings.TrimPrefix(line, "data:")
			data = strings.TrimPrefix(data, " ")
			dataBuf.WriteString(data)
			dataBuf.WriteByte('\n')
			continue
		}
	}

	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("read SSE stream: %w", err)
	}
	return nil
}

// runSubscribe is the production entry point invoked by the cobra Run handler
// for `graphjin cli query subscribe`. It wires signal handling, the saved
// client config, and the production emitResult writer.
func runSubscribe(parent context.Context, query string, vars map[string]any, namespace string) {
	mcpClientRedirectLog()

	gqlURL, err := resolveGraphQLURL(resolveMCPServerURL(nil))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()

	err = runSubscribeStream(
		ctx,
		gqlURL,
		resolveMCPAuth(),
		mcpClientHeaders,
		query,
		vars,
		namespace,
		emitResult,
		os.Stderr,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}
