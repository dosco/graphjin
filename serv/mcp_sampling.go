package serv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	ax "github.com/ax-llm/ax/packages/go"
	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const (
	agentSamplingPathServer      = "server"
	agentSamplingPathClient      = "mcp_client"
	agentSamplingPathUnavailable = "unavailable"

	defaultSamplingMaxTokens = 4096
)

var errMCPSamplingUnavailable = errors.New("MCP client sampling is required but unavailable")

type agentSamplingPathContextKey struct{}

type samplingAIClient struct {
	srv *server.MCPServer
}

func (c samplingAIClient) Chat(ctx context.Context, values map[string]ax.Value, _ map[string]ax.Value) (ax.Value, error) {
	if c.srv == nil {
		return nil, errors.New("MCP server is required for client sampling")
	}
	req := c.createMessageRequest(values)
	result, err := c.srv.RequestSampling(ctx, req)
	if err != nil {
		return nil, err
	}
	text := samplingContentText(result.Content)
	return map[string]ax.Value{
		"results": []ax.Value{
			map[string]ax.Value{
				"content":       text,
				"role":          string(mcp.RoleAssistant),
				"model":         result.Model,
				"finish_reason": result.StopReason,
				"stop_reason":   result.StopReason,
			},
		},
	}, nil
}

func (c samplingAIClient) Embed(context.Context, map[string]ax.Value, map[string]ax.Value) (ax.Value, error) {
	return nil, errors.New("MCP client sampling does not support embeddings")
}

func (c samplingAIClient) Stream(ctx context.Context, values map[string]ax.Value, options map[string]ax.Value) ([]ax.Value, error) {
	result, err := c.Chat(ctx, values, options)
	if err != nil {
		return nil, err
	}
	return []ax.Value{result}, nil
}

func (c samplingAIClient) createMessageRequest(values map[string]ax.Value) mcp.CreateMessageRequest {
	var systemParts []string
	messages := make([]mcp.SamplingMessage, 0)
	for _, raw := range samplingSlice(values["chat_prompt"]) {
		msg := samplingMap(raw)
		role := strings.ToLower(strings.TrimSpace(fmt.Sprint(msg["role"])))
		content := samplingText(msg["content"])
		if strings.TrimSpace(content) == "" {
			continue
		}
		switch role {
		case "system":
			systemParts = append(systemParts, content)
		case string(mcp.RoleAssistant):
			messages = append(messages, mcp.SamplingMessage{Role: mcp.RoleAssistant, Content: mcp.NewTextContent(content)})
		default:
			messages = append(messages, mcp.SamplingMessage{Role: mcp.RoleUser, Content: mcp.NewTextContent(content)})
		}
	}

	return mcp.CreateMessageRequest{
		CreateMessageParams: mcp.CreateMessageParams{
			Messages:      messages,
			SystemPrompt:  strings.Join(systemParts, "\n\n"),
			Temperature:   samplingFloat(values, "temperature"),
			MaxTokens:     samplingIntDefault(values, defaultSamplingMaxTokens, "max_tokens", "maxTokens"),
			StopSequences: samplingStringSliceFromMaps(values, "stop_sequences", "stopSequences", "stop"),
			Metadata:      samplingMetadata(values),
		},
	}
}

func normalizeAgentSamplingMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case gjagent.SamplingAuto:
		return gjagent.SamplingAuto
	case gjagent.SamplingRequire:
		return gjagent.SamplingRequire
	default:
		return gjagent.SamplingOff
	}
}

func withAgentSamplingPath(ctx context.Context, path string) context.Context {
	if strings.TrimSpace(path) == "" {
		path = agentSamplingPathServer
	}
	return context.WithValue(ctx, agentSamplingPathContextKey{}, path)
}

func agentSamplingPathFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	path, _ := ctx.Value(agentSamplingPathContextKey{}).(string)
	return path
}

func (ms *mcpServer) agentSamplingOptions(ctx context.Context, conf gjagent.Config) (string, []gjagent.Option, error) {
	mode := normalizeAgentSamplingMode(conf.Sampling)
	if mode == gjagent.SamplingOff {
		return agentSamplingPathServer, nil, nil
	}
	if !mcpSamplingAvailable(ctx) {
		if mode == gjagent.SamplingRequire {
			return agentSamplingPathUnavailable, nil, errMCPSamplingUnavailable
		}
		return agentSamplingPathServer, nil, nil
	}
	return agentSamplingPathClient, []gjagent.Option{
		gjagent.WithClientFactory(func(gjagent.Config) (ax.AIClient, error) {
			return samplingAIClient{srv: ms.srv}, nil
		}),
	}, nil
}

func mcpSamplingAvailable(ctx context.Context) bool {
	session := server.ClientSessionFromContext(ctx)
	if session == nil {
		return false
	}
	if _, ok := session.(server.SessionWithSampling); !ok {
		return false
	}
	if info, ok := session.(server.SessionWithClientInfo); ok {
		return info.GetClientCapabilities().Sampling != nil
	}
	return true
}

func samplingEnabledForConfig(conf *Config) bool {
	return conf != nil && normalizeAgentSamplingMode(conf.Agent.Sampling) != gjagent.SamplingOff
}

func samplingSlice(v any) []any {
	switch t := v.(type) {
	case []any:
		return t
	default:
		var out []any
		if samplingRoundTrip(t, &out) == nil {
			return out
		}
		return nil
	}
}

func samplingMap(v any) map[string]any {
	switch t := v.(type) {
	case map[string]any:
		return t
	default:
		var out map[string]any
		if samplingRoundTrip(t, &out) == nil {
			return out
		}
		return nil
	}
}

func samplingRoundTrip(v any, out any) error {
	if v == nil {
		return errors.New("nil value")
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func samplingText(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case mcp.TextContent:
		return t.Text
	case *mcp.TextContent:
		if t == nil {
			return ""
		}
		return t.Text
	case fmt.Stringer:
		return t.String()
	default:
		if m := samplingMap(t); len(m) != 0 {
			if text, ok := m["text"]; ok {
				return samplingText(text)
			}
		}
		data, err := json.Marshal(t)
		if err != nil {
			return fmt.Sprint(t)
		}
		return string(data)
	}
}

func samplingContentText(v any) string {
	text := samplingText(v)
	if text != "" {
		return text
	}
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(data)
}

func samplingMetadata(values map[string]ax.Value) map[string]any {
	metadata := map[string]any{}
	for _, key := range []string{"model", "model_config", "response_format", "functions", "function_call"} {
		if value, ok := values[key]; ok && value != nil {
			metadata[key] = value
		}
	}
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}

func samplingFloat(values map[string]ax.Value, keys ...string) float64 {
	for _, value := range samplingValuesFromMaps(values, keys...) {
		switch t := value.(type) {
		case float64:
			return t
		case float32:
			return float64(t)
		case int:
			return float64(t)
		case int64:
			return float64(t)
		case json.Number:
			if n, err := t.Float64(); err == nil {
				return n
			}
		}
	}
	return 0
}

func samplingIntDefault(values map[string]ax.Value, fallback int, keys ...string) int {
	for _, value := range samplingValuesFromMaps(values, keys...) {
		switch t := value.(type) {
		case int:
			if t > 0 {
				return t
			}
		case int64:
			if t > 0 {
				return int(t)
			}
		case float64:
			if t > 0 {
				return int(t)
			}
		case json.Number:
			if n, err := t.Int64(); err == nil && n > 0 {
				return int(n)
			}
		}
	}
	return fallback
}

func samplingStringSliceFromMaps(values map[string]ax.Value, keys ...string) []string {
	for _, value := range samplingValuesFromMaps(values, keys...) {
		items := samplingSlice(value)
		if len(items) == 0 {
			if text := strings.TrimSpace(samplingText(value)); text != "" {
				return []string{text}
			}
			continue
		}
		out := make([]string, 0, len(items))
		for _, item := range items {
			if text := strings.TrimSpace(samplingText(item)); text != "" {
				out = append(out, text)
			}
		}
		if len(out) != 0 {
			return out
		}
	}
	return nil
}

func samplingValuesFromMaps(values map[string]ax.Value, keys ...string) []any {
	out := make([]any, 0, len(keys)*2)
	for _, key := range keys {
		if value, ok := values[key]; ok && value != nil {
			out = append(out, value)
		}
	}
	for _, mapKey := range []string{"model_config", "options"} {
		nested := samplingMap(values[mapKey])
		for _, key := range keys {
			if value, ok := nested[key]; ok && value != nil {
				out = append(out, value)
			}
		}
	}
	return out
}
