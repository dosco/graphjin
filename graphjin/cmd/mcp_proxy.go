package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
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
}

func (p *mcpProxy) run(ctx context.Context) error {
	reader := bufio.NewReader(os.Stdin)
	consecutiveErrors := 0
	maxConsecutiveErrors := 10

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

		// Forward to remote server
		resp, err := p.forwardRequest(ctx, line)
		if err != nil {
			consecutiveErrors++
			log.Errorf("proxy error (%d/%d): %s", consecutiveErrors, maxConsecutiveErrors, err)

			// Send JSON-RPC error response to client
			p.writeErrorResponse(line, err)

			if consecutiveErrors >= maxConsecutiveErrors {
				return fmt.Errorf("too many consecutive errors, giving up")
			}
			continue
		}

		// Reset error counter on success
		consecutiveErrors = 0

		// Write response to stdout
		os.Stdout.Write(resp)
		if len(resp) > 0 && resp[len(resp)-1] != '\n' {
			os.Stdout.Write([]byte("\n"))
		}
	}
}

func (p *mcpProxy) writeErrorResponse(request []byte, err error) {
	// Extract request ID if possible
	var req struct {
		ID interface{} `json:"id"`
	}
	json.Unmarshal(request, &req) //nolint:errcheck

	errResp := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      req.ID,
		"error": map[string]interface{}{
			"code":    -32603,
			"message": fmt.Sprintf("proxy error: %s", err),
		},
	}

	respBytes, _ := json.Marshal(errResp)
	os.Stdout.Write(respBytes)
	os.Stdout.Write([]byte("\n"))
}

func (p *mcpProxy) forwardRequest(ctx context.Context, body []byte) ([]byte, error) {
	var lastErr error
	maxRetries := 5
	baseDelay := 500 * time.Millisecond

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 500ms, 1s, 2s, 4s, 8s
			delay := baseDelay * time.Duration(1<<(attempt-1))
			if delay > 8*time.Second {
				delay = 8 * time.Second
			}
			log.Infof("Retrying request (attempt %d/%d) after %v", attempt+1, maxRetries, delay)

			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.serverURL, bytes.NewReader(body))
		if err != nil {
			return nil, err // Don't retry request creation errors
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")

		// Support auth via environment variable
		if auth := os.Getenv("GRAPHJIN_MCP_AUTH"); auth != "" {
			req.Header.Set("Authorization", auth)
		}

		resp, err := p.httpClient.Do(req)
		if err != nil {
			lastErr = err
			log.Errorf("Request failed: %s", err)
			continue // Retry on network errors
		}

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			log.Errorf("Failed to read response: %s", err)
			continue
		}

		// Retry on server errors (5xx)
		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("server error: %d", resp.StatusCode)
			log.Errorf("Server error: %d", resp.StatusCode)
			continue
		}

		// Return error for client errors (4xx) without retry
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("client error: %d - %s", resp.StatusCode, string(respBody))
		}

		return respBody, nil
	}

	return nil, fmt.Errorf("max retries exceeded: %w", lastErr)
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
			"graphjin-proxy": map[string]interface{}{
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
