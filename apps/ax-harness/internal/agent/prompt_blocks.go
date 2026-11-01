package agent

import (
	"context"
	"strings"

	ax "github.com/ax-llm/ax/packages/go"
)

// instructionsLabel is how Ax renders the instructions input field.
const instructionsLabel = "Instructions: "

// breakpointCache is true for providers that reuse a cached prefix only up to
// a marked message: Anthropic cache_control and OpenAI explicit breakpoints.
func breakpointCache(provider string) bool {
	return provider == "anthropic" || provider == "openai" || provider == "openai-responses"
}

// instructionsBlockClient moves the rendered instructions field into its own
// cached user message. The breakpoint then covers the org prompt alone, so a
// new turn with another user message still reuses it.
type instructionsBlockClient struct {
	ax.AIClient
	head   string
	routed *RoutedClient
}

func requestMessages(request map[string]ax.Value) (string, []ax.Value, bool) {
	for _, key := range []string{"chat_prompt", "chatPrompt", "messages"} {
		switch value := request[key].(type) {
		case []ax.Value:
			return key, value, true
		case *ax.AxArray:
			if value != nil {
				return key, value.Items, true
			}
		}
	}
	return "", nil, false
}

func (c *instructionsBlockClient) split(request map[string]ax.Value) map[string]ax.Value {
	model, _ := request["model"].(string)
	if c.routed == nil || !breakpointCache(c.routed.providerFor(model)) {
		return request
	}
	key, messages, ok := requestMessages(request)
	if !ok {
		return request
	}
	for i, raw := range messages {
		message, ok := raw.(map[string]ax.Value)
		if !ok || message["role"] != "user" {
			continue
		}
		content, _ := message["content"].(string)
		if !strings.HasPrefix(content, c.head) {
			continue
		}
		rest := strings.TrimLeft(content[len(c.head):], "\n")
		if rest == "" {
			return request
		}
		tail := make(map[string]ax.Value, len(message))
		for k, value := range message {
			tail[k] = value
		}
		tail["content"] = rest
		out := make([]ax.Value, 0, len(messages)+1)
		out = append(out, messages[:i]...)
		out = append(out, ax.Object("role", "user", "content", c.head, "cache", true), tail)
		out = append(out, messages[i+1:]...)
		copy := make(map[string]ax.Value, len(request))
		for k, value := range request {
			copy[k] = value
		}
		copy[key] = out
		return copy
	}
	return request
}

func (c *instructionsBlockClient) Chat(ctx context.Context, request, options map[string]ax.Value) (ax.Value, error) {
	return c.AIClient.Chat(ctx, c.split(request), options)
}

func (c *instructionsBlockClient) Stream(ctx context.Context, request, options map[string]ax.Value) ([]ax.Value, error) {
	return c.AIClient.Stream(ctx, c.split(request), options)
}

func (c *instructionsBlockClient) StreamEvents(ctx context.Context, request, options map[string]ax.Value) (ax.AxChatStream, error) {
	return streamEvents(ctx, c.AIClient, c.split(request), options)
}

func (c *instructionsBlockClient) GetFeatures(model string) map[string]ax.Value {
	if features, ok := c.AIClient.(interface {
		GetFeatures(string) map[string]ax.Value
	}); ok {
		return features.GetFeatures(model)
	}
	return nil
}
