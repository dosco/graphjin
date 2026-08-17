package agent

import (
	"context"
	"reflect"
	"strings"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
)

type responseFormatCaptureClient struct {
	chatRequest   map[string]ax.Value
	streamRequest map[string]ax.Value
	embedRequest  map[string]ax.Value
	features      map[string]ax.Value
}

func (c *responseFormatCaptureClient) Chat(_ context.Context, request, _ map[string]ax.Value) (ax.Value, error) {
	c.chatRequest = request
	return nil, nil
}

func (c *responseFormatCaptureClient) Embed(_ context.Context, request, _ map[string]ax.Value) (ax.Value, error) {
	c.embedRequest = request
	return nil, nil
}

func (c *responseFormatCaptureClient) Stream(_ context.Context, request, _ map[string]ax.Value) ([]ax.Value, error) {
	c.streamRequest = request
	return nil, nil
}

func (c *responseFormatCaptureClient) GetFeatures(string) map[string]ax.Value {
	return c.features
}

func TestResponseFormatJSONObjectOverridesAxSchemaWithoutMutatingRequest(t *testing.T) {
	strict := map[string]ax.Value{
		"type": "json_schema",
		"schema": map[string]ax.Value{
			"name": "output",
			"schema": map[string]ax.Value{
				"type":     "object",
				"required": []ax.Value{"status", "answer"},
				"properties": map[string]ax.Value{
					"status": map[string]ax.Value{"type": "string"},
					"answer": map[string]ax.Value{"type": "string"},
				},
			},
		},
	}
	originalPrompt := []ax.Value{map[string]ax.Value{"role": "system", "content": "Base instructions."}}
	request := map[string]ax.Value{
		"model":           "gemma",
		"response_format": strict,
		"chat_prompt":     originalPrompt,
	}
	capture := &responseFormatCaptureClient{features: map[string]ax.Value{"structuredOutputs": true}}
	client := withResponseFormat(capture, ResponseFormatJSONObject)

	if _, err := client.Chat(context.Background(), request, nil); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if _, err := client.Stream(context.Background(), request, nil); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if _, err := client.Embed(context.Background(), request, nil); err != nil {
		t.Fatalf("Embed: %v", err)
	}

	want := map[string]ax.Value{"type": ResponseFormatJSONObject}
	if got := capture.chatRequest["response_format"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("chat response_format = %#v, want %#v", got, want)
	}
	if got := capture.streamRequest["response_format"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("stream response_format = %#v, want %#v", got, want)
	}
	if got := request["response_format"]; !reflect.DeepEqual(got, strict) {
		t.Fatalf("original request was mutated: %#v", got)
	}
	chatPrompt, ok := capture.chatRequest["chat_prompt"].([]ax.Value)
	if !ok || len(chatPrompt) != 1 {
		t.Fatalf("chat_prompt = %#v", capture.chatRequest["chat_prompt"])
	}
	system, ok := chatPrompt[0].(map[string]ax.Value)
	if !ok || !strings.Contains(system["content"].(string), `"required":["status","answer"]`) {
		t.Fatalf("schema hint missing from system prompt: %#v", chatPrompt[0])
	}
	if got := originalPrompt[0].(map[string]ax.Value)["content"]; got != "Base instructions." {
		t.Fatalf("original chat prompt was mutated: %q", got)
	}
	if !reflect.DeepEqual(capture.embedRequest, request) {
		t.Fatalf("Embed request = %#v, want %#v", capture.embedRequest, request)
	}
	featureClient, ok := client.(interface {
		GetFeatures(string) map[string]ax.Value
	})
	if !ok || !reflect.DeepEqual(featureClient.GetFeatures("gemma"), capture.features) {
		t.Fatal("response format wrapper did not forward provider features")
	}
}

func TestJSONObjectSchemaHintSupportsOpenAIResponseFormatShape(t *testing.T) {
	format := map[string]ax.Value{
		"type": "json_schema",
		"json_schema": map[string]ax.Value{
			"schema": map[string]ax.Value{"type": "object", "required": []ax.Value{"answer"}},
		},
	}
	if hint := jsonObjectSchemaHint(format); !strings.Contains(hint, `"required":["answer"]`) {
		t.Fatalf("schema hint = %q", hint)
	}
}

func TestResponseFormatDefaultsAndValidation(t *testing.T) {
	if got := EffectiveResponseFormat(""); got != ResponseFormatJSONSchema {
		t.Fatalf("default response format = %q, want %q", got, ResponseFormatJSONSchema)
	}
	capture := &responseFormatCaptureClient{}
	if got := withResponseFormat(capture, " json_schema "); got != capture {
		t.Fatal("json_schema should preserve the original client")
	}
	if err := ValidateResponseFormat(" JSON_OBJECT "); err != nil {
		t.Fatalf("ValidateResponseFormat(json_object): %v", err)
	}
	if err := ValidateResponseFormat("xml"); err == nil {
		t.Fatal("expected unsupported response format to fail validation")
	}
}
