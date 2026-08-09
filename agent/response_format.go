package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	ax "github.com/ax-llm/ax/packages/go"
)

const (
	// ResponseFormatJSONSchema preserves Ax's default strict structured-output
	// request, including the generated response schema.
	ResponseFormatJSONSchema = "json_schema"
	// ResponseFormatJSONObject asks compatible providers for JSON without strict
	// schema decoding. It is useful for endpoints that accept json_schema but do
	// not reliably complete GraphJin's richer final-response schema.
	ResponseFormatJSONObject = "json_object"
)

// EffectiveResponseFormat returns the normalized configured response format.
func EffectiveResponseFormat(responseFormat string) string {
	responseFormat = strings.ToLower(strings.TrimSpace(responseFormat))
	if responseFormat == "" {
		return defaultResponseFormat
	}
	return responseFormat
}

// ValidateResponseFormat rejects response formats that GraphJin cannot apply
// consistently across provider clients.
func ValidateResponseFormat(responseFormat string) error {
	switch EffectiveResponseFormat(responseFormat) {
	case ResponseFormatJSONSchema, ResponseFormatJSONObject:
		return nil
	default:
		return fmt.Errorf("agent.response_format must be one of %s, %s", ResponseFormatJSONSchema, ResponseFormatJSONObject)
	}
}

func withResponseFormat(client ax.AIClient, responseFormat string) ax.AIClient {
	if client == nil || EffectiveResponseFormat(responseFormat) == ResponseFormatJSONSchema {
		return client
	}
	return responseFormatClient{inner: client, responseFormat: EffectiveResponseFormat(responseFormat)}
}

type responseFormatClient struct {
	inner          ax.AIClient
	responseFormat string
}

func (c responseFormatClient) Chat(ctx context.Context, request, options map[string]ax.Value) (ax.Value, error) {
	return c.inner.Chat(ctx, requestWithResponseFormat(request, c.responseFormat), options)
}

func (c responseFormatClient) Embed(ctx context.Context, request, options map[string]ax.Value) (ax.Value, error) {
	return c.inner.Embed(ctx, request, options)
}

func (c responseFormatClient) Stream(ctx context.Context, request, options map[string]ax.Value) ([]ax.Value, error) {
	return c.inner.Stream(ctx, requestWithResponseFormat(request, c.responseFormat), options)
}

func requestWithResponseFormat(request map[string]ax.Value, responseFormat string) map[string]ax.Value {
	updated := make(map[string]ax.Value, len(request)+1)
	for key, value := range request {
		updated[key] = value
	}
	if responseFormat == ResponseFormatJSONObject {
		if hint := jsonObjectSchemaHint(request["response_format"]); hint != "" {
			updated["chat_prompt"] = chatPromptWithSchemaHint(request["chat_prompt"], hint)
		}
	}
	updated["response_format"] = map[string]ax.Value{"type": responseFormat}
	return updated
}

func jsonObjectSchemaHint(responseFormat ax.Value) string {
	format, ok := responseFormat.(map[string]ax.Value)
	if !ok {
		return ""
	}
	container, ok := format["schema"].(map[string]ax.Value)
	if !ok {
		container, _ = format["json_schema"].(map[string]ax.Value)
	}
	if container == nil {
		return ""
	}
	schema := container["schema"]
	if schema == nil {
		return ""
	}
	data, err := json.Marshal(schema)
	if err != nil {
		return ""
	}
	return "JSON object compatibility mode is active. Return exactly one JSON object that conforms to the schema below. Include every key listed in required, using null for a nullable field with no value. Preserve property names and casing exactly. Do not wrap the object or add prose.\n\nJSON schema:\n" + string(data)
}

func chatPromptWithSchemaHint(chatPrompt ax.Value, hint string) ax.Value {
	messages, ok := chatPrompt.([]ax.Value)
	if !ok || len(messages) == 0 {
		return chatPrompt
	}
	updated := append([]ax.Value(nil), messages...)
	for index, value := range messages {
		message, ok := value.(map[string]ax.Value)
		if !ok || message["role"] != "system" {
			continue
		}
		cloned := make(map[string]ax.Value, len(message))
		for key, field := range message {
			cloned[key] = field
		}
		content, _ := message["content"].(string)
		cloned["content"] = strings.TrimSpace(content) + "\n\n" + hint
		updated[index] = cloned
		return updated
	}
	return append([]ax.Value{map[string]ax.Value{"role": "system", "content": hint}}, updated...)
}
