// mcp-sampling-client is a minimal MCP client used by the demo smoke suites to
// exercise GraphJin's agent sampling end to end: it connects to a GraphJin MCP
// endpoint over streamable HTTP, advertises the MCP sampling capability, calls
// ask_graphjin_agent, and answers the server's sampling/createMessage requests
// by forwarding them to an OpenAI-compatible chat-completions endpoint.
//
// Output is a single JSON object on stdout:
//
//	{"sampling_calls": N, "is_error": bool, "response": <structuredContent>}
//
// so shell smoke suites can assert on it with jq.
package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

// mintHS256 builds a signed HS256 JWT for the demo servers' static-secret
// JWT auth (agentic mode refuses header-trust identity).
func mintHS256(secret, sub, role, accountID string) string {
	b64 := func(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
	header := b64([]byte(`{"alg":"HS256","typ":"JWT"}`))
	claims, _ := json.Marshal(map[string]any{
		"sub":        sub,
		"roles":      []string{role},
		"account_id": accountID,
		"iat":        time.Now().Unix(),
		"exp":        time.Now().Add(time.Hour).Unix(),
	})
	payload := b64(claims)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(header + "." + payload))
	return header + "." + payload + "." + b64(mac.Sum(nil))
}

type providerSampler struct {
	baseURL string
	apiKey  string
	model   string
	verbose bool
	calls   atomic.Int64
	httpc   *http.Client
}

type chatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func samplingText(content any) string {
	switch c := content.(type) {
	case mcp.TextContent:
		return c.Text
	case map[string]any:
		if t, ok := c["text"].(string); ok {
			return t
		}
	}
	b, _ := json.Marshal(content)
	return string(b)
}

func (p *providerSampler) CreateMessage(ctx context.Context, req mcp.CreateMessageRequest) (*mcp.CreateMessageResult, error) {
	p.calls.Add(1)

	msgs := make([]map[string]any, 0, len(req.Messages)+1)
	if req.SystemPrompt != "" {
		msgs = append(msgs, map[string]any{"role": "system", "content": req.SystemPrompt})
	}
	for _, m := range req.Messages {
		role := string(m.Role)
		if role == "" {
			role = "user"
		}
		msgs = append(msgs, map[string]any{"role": role, "content": samplingText(m.Content)})
	}

	// The ax RLM protocol wraps prompts and replies in a strict JSON envelope
	// ({"javascriptCode": "..."}); enforce it with structured outputs so any
	// schema-capable model complies exactly.
	body := map[string]any{
		"model":    p.model,
		"messages": msgs,
		"response_format": map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "rlm_javascript_code",
				"strict": true,
				"schema": map[string]any{
					"type":                 "object",
					"properties":           map[string]any{"javascriptCode": map[string]any{"type": "string"}},
					"required":             []string{"javascriptCode"},
					"additionalProperties": false,
				},
			},
		},
	}
	if req.MaxTokens > 0 {
		body["max_completion_tokens"] = req.MaxTokens
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}
	if len(req.StopSequences) > 0 {
		body["stop"] = req.StopSequences
	}

	call := func(b map[string]any) (*chatCompletionResponse, int, error) {
		payload, err := json.Marshal(b)
		if err != nil {
			return nil, 0, err
		}
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(payload))
		if err != nil {
			return nil, 0, err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
		resp, err := p.httpc.Do(httpReq)
		if err != nil {
			return nil, 0, fmt.Errorf("provider request: %w", err)
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		if err != nil {
			return nil, resp.StatusCode, err
		}
		var out chatCompletionResponse
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, resp.StatusCode, fmt.Errorf("provider response decode (HTTP %d): %w", resp.StatusCode, err)
		}
		return &out, resp.StatusCode, nil
	}

	out, code, err := call(body)
	if err != nil {
		return nil, err
	}
	// Older OpenAI-compatible endpoints only accept the legacy parameter name.
	if out.Error != nil && strings.Contains(out.Error.Message, "max_completion_tokens") {
		delete(body, "max_completion_tokens")
		if req.MaxTokens > 0 {
			body["max_tokens"] = req.MaxTokens
		}
		out, code, err = call(body)
		if err != nil {
			return nil, err
		}
	}
	// Some model families reject sampling temperature; retry without it.
	if out.Error != nil && strings.Contains(out.Error.Message, "temperature") {
		delete(body, "temperature")
		out, code, err = call(body)
		if err != nil {
			return nil, err
		}
	}
	if out.Error != nil {
		return nil, fmt.Errorf("provider error (HTTP %d): %s", code, out.Error.Message)
	}
	if code < 200 || code >= 300 || len(out.Choices) == 0 {
		return nil, fmt.Errorf("provider returned HTTP %d with no choices", code)
	}

	stop := out.Choices[0].FinishReason
	if stop == "" || stop == "stop" {
		stop = "endTurn"
	}
	if p.verbose {
		fmt.Fprintf(os.Stderr, "--- sampling request (system %d bytes, %d msgs, maxTokens %d) ---\n%s\n--- sampled reply ---\n%s\n---\n",
			len(req.SystemPrompt), len(req.Messages), req.MaxTokens, samplingText(func() any {
				if len(req.Messages) > 0 {
					return req.Messages[len(req.Messages)-1].Content
				}
				return ""
			}()), out.Choices[0].Message.Content)
	}
	return &mcp.CreateMessageResult{
		SamplingMessage: mcp.SamplingMessage{
			Role:    mcp.RoleAssistant,
			Content: mcp.TextContent{Type: "text", Text: out.Choices[0].Message.Content},
		},
		Model:      p.model,
		StopReason: stop,
	}, nil
}

type output struct {
	SamplingCalls int64  `json:"sampling_calls"`
	IsError       bool   `json:"is_error"`
	Error         string `json:"error,omitempty"`
	Response      any    `json:"response,omitempty"`
	ContentText   string `json:"content_text,omitempty"`
}

func emit(o output, code int) {
	enc := json.NewEncoder(os.Stdout)
	_ = enc.Encode(o)
	os.Exit(code)
}

func main() {
	var (
		url             = flag.String("url", "http://localhost:8080/api/v1/mcp", "GraphJin MCP endpoint")
		instruction     = flag.String("instruction", "List the approved saved queries. Discovery only.", "instruction for ask_graphjin_agent")
		userID          = flag.String("user-id", "demo-user", "dev identity user id")
		role            = flag.String("role", "user", "dev identity role")
		accountID       = flag.String("account-id", "1", "dev identity account id")
		bearer          = flag.String("bearer", "", "Authorization bearer token (overrides --jwt-secret)")
		jwtSecret       = flag.String("jwt-secret", "", "mint an HS256 bearer JWT from this secret using the identity flags")
		providerBaseURL = flag.String("provider-base-url", "https://api.openai.com/v1", "OpenAI-compatible base URL for sampling")
		providerKeyEnv  = flag.String("provider-key-env", "OPENAI_API_KEY", "env var holding the provider API key")
		model           = flag.String("model", "gpt-5-mini", "model used to answer sampling requests")
		maxSteps        = flag.Int("max-steps", 10, "agent step cap")
		timeoutSec      = flag.Int("timeout", 240, "overall timeout in seconds")
		noSampling      = flag.Bool("no-sampling", false, "connect WITHOUT the sampling capability (for require-mode checks)")
		verbose         = flag.Bool("verbose", false, "dump sampling requests/replies to stderr")
	)
	flag.Parse()

	// Dev identity headers are always sent (dev-mode servers trust them,
	// JWT-mode servers ignore them); a bearer token is added when available.
	headers := map[string]string{
		"X-User-ID":    *userID,
		"X-User-Role":  *role,
		"X-Account-ID": *accountID,
	}
	switch {
	case *bearer != "":
		headers["Authorization"] = "Bearer " + *bearer
	case *jwtSecret != "":
		headers["Authorization"] = "Bearer " + mintHS256(*jwtSecret, *userID, *role, *accountID)
	}

	sampler := &providerSampler{
		baseURL: *providerBaseURL,
		apiKey:  os.Getenv(*providerKeyEnv),
		model:   *model,
		verbose: *verbose,
		httpc:   &http.Client{Timeout: 120 * time.Second},
	}
	if !*noSampling && sampler.apiKey == "" {
		emit(output{IsError: true, Error: fmt.Sprintf("missing provider key: env %s is empty", *providerKeyEnv)}, 2)
	}

	trans, err := transport.NewStreamableHTTP(*url,
		transport.WithHTTPHeaders(headers),
		transport.WithContinuousListening(),
	)
	if err != nil {
		emit(output{IsError: true, Error: "transport: " + err.Error()}, 2)
	}

	opts := []client.ClientOption{}
	if !*noSampling {
		opts = append(opts, client.WithSamplingHandler(sampler))
	}
	c := client.NewClient(trans, opts...)

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*timeoutSec)*time.Second)
	defer cancel()

	if err := c.Start(ctx); err != nil {
		emit(output{IsError: true, Error: "start: " + err.Error()}, 2)
	}
	defer c.Close()

	initReq := mcp.InitializeRequest{}
	initReq.Params = mcp.InitializeParams{
		ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
		ClientInfo:      mcp.Implementation{Name: "gj-sampling-smoke", Version: "1.0.0"},
	}
	if _, err := c.Initialize(ctx, initReq); err != nil {
		emit(output{IsError: true, Error: "initialize: " + err.Error()}, 2)
	}

	callReq := mcp.CallToolRequest{}
	callReq.Params.Name = "ask_graphjin_agent"
	callReq.Params.Arguments = map[string]any{
		"instruction": *instruction,
		"max_steps":   *maxSteps,
	}
	result, err := c.CallTool(ctx, callReq)
	if err != nil {
		emit(output{SamplingCalls: sampler.calls.Load(), IsError: true, Error: "tools/call: " + err.Error()}, 2)
	}

	var contentText string
	for _, item := range result.Content {
		if tc, ok := item.(mcp.TextContent); ok {
			contentText += tc.Text
		}
	}
	emit(output{
		SamplingCalls: sampler.calls.Load(),
		IsError:       result.IsError,
		Response:      result.StructuredContent,
		ContentText:   contentText,
	}, 0)
}
