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
	anchoredContent := false
	for _, raw := range messages {
		message, ok := raw.(map[string]ax.Value)
		if !ok {
			continue
		}
		if content, ok := message["content"].(string); ok && containsAll(content, c.request) {
			anchoredContent = true
			break
		}
	}
	_, hasRoute := request["model"]
	if anchoredContent && (hasRoute || c.contextRoute == "") {
		return request
	}
	anchored := make(map[string]ax.Value, len(request))
	for k, value := range request {
		anchored[k] = value
	}
	if !anchoredContent {
		copyMessages := make([]ax.Value, 0, len(messages)+1)
		copyMessages = append(copyMessages, ax.Object("role", "system", "content", "Accepted user request and constraints (authoritative): "+strings.Join(c.request, "\n\n")))
		copyMessages = append(copyMessages, messages...)
		anchored[key] = copyMessages
	}
	if !hasRoute && c.contextRoute != "" {
		anchored["model"] = c.contextRoute
	}
	return anchored
}

func containsAll(content string, parts []string) bool {
	for _, part := range parts {
		if !strings.Contains(content, part) {
			return false
		}
	}
	return true
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
