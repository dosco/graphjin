package agent

import (
	"context"
	"strings"

	ax "github.com/ax-llm/ax/packages/go"
)

// contextAnchorClient restores the accepted request when Ax builds a model
// request from a compacted trajectory. In particular, Ax's internal
// summarizer receives only working code and may otherwise omit user limits.
// It copies the request before changing it; Ax retains ownership of its maps.
type contextAnchorClient struct {
	ax.AIClient
	request      []string
	contextRoute string
}

func (c *contextAnchorClient) anchored(request map[string]ax.Value) map[string]ax.Value {
	if len(c.request) == 0 || strings.TrimSpace(strings.Join(c.request, "")) == "" {
		return request
	}
	key := "chat_prompt"
	if _, ok := request[key]; !ok {
		key = "chatPrompt"
		if _, ok := request[key]; !ok {
			key = "messages"
			if _, ok := request[key]; !ok {
				return request
			}
		}
	}
	var messages []ax.Value
	switch value := request[key].(type) {
	case []ax.Value:
		messages = value
	case *ax.AxArray:
		if value == nil {
			return request
		}
		messages = value.Items
	default:
		return request
	}
	// Restore only the parts Ax cut to a preview, so the anchor stays cacheable across turns.
	var missing []string
	for _, part := range c.request {
		if !anyContains(messages, part) {
			missing = append(missing, part)
		}
	}
	anchoredContent := len(missing) == 0
	_, hasRoute := request["model"]
	if anchoredContent && (hasRoute || c.contextRoute == "") {
		return request
	}
	anchored := make(map[string]ax.Value, len(request))
	for k, value := range request {
		anchored[k] = value
	}
	if !anchoredContent {
		anchored[key] = withSystemAnchor(messages, "Accepted user request and constraints (authoritative): "+strings.Join(missing, "\n\n"))
	}
	if !hasRoute && c.contextRoute != "" {
		anchored["model"] = c.contextRoute
	}
	return anchored
}

// withSystemAnchor puts the anchor at the start of the leading system message.
// Some chat templates (Qwen, Llama) accept only one system message.
func withSystemAnchor(messages []ax.Value, anchor string) []ax.Value {
	if len(messages) > 0 {
		if first, ok := messages[0].(map[string]ax.Value); ok && first["role"] == "system" {
			if content, ok := first["content"].(string); ok {
				merged := make(map[string]ax.Value, len(first))
				for k, value := range first {
					merged[k] = value
				}
				merged["content"] = anchor + "\n\n" + content
				out := make([]ax.Value, 0, len(messages))
				out = append(out, merged)
				return append(out, messages[1:]...)
			}
		}
	}
	out := make([]ax.Value, 0, len(messages)+1)
	out = append(out, ax.Object("role", "system", "content", anchor))
	return append(out, messages...)
}

func anyContains(messages []ax.Value, part string) bool {
	for _, raw := range messages {
		if message, ok := raw.(map[string]ax.Value); ok {
			if content, ok := message["content"].(string); ok && strings.Contains(content, part) {
				return true
			}
		}
	}
	return false
}

func (c *contextAnchorClient) Chat(ctx context.Context, request, options map[string]ax.Value) (ax.Value, error) {
	return c.AIClient.Chat(ctx, c.anchored(request), options)
}

func (c *contextAnchorClient) Stream(ctx context.Context, request, options map[string]ax.Value) ([]ax.Value, error) {
	return c.AIClient.Stream(ctx, c.anchored(request), options)
}

func (c *contextAnchorClient) StreamEvents(ctx context.Context, request, options map[string]ax.Value) (ax.AxChatStream, error) {
	return streamEvents(ctx, c.AIClient, c.anchored(request), options)
}

func (c *contextAnchorClient) GetFeatures(model string) map[string]ax.Value {
	if features, ok := c.AIClient.(interface {
		GetFeatures(string) map[string]ax.Value
	}); ok {
		return features.GetFeatures(model)
	}
	return nil
}
