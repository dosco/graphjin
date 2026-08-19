package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
)

// The Vertex Gemma pairing is the case the deleted workaround existed to serve.
// Ax's vertex-ai profile carries an exact model rule for it that prefers
// json_object and excludes native JSON Schema, so mode "auto" must resolve to a
// json_object request with no help from GraphJin. This runs over a scripted
// transport, so it makes no network call. It is one case, not a restatement of
// Ax's conformance suite.
func TestVertexGemmaAutoModeSelectsJSONObject(t *testing.T) {
	transport := ax.NewScriptedTransport([]ax.Value{
		ax.Object("status", float64(200), "json", ax.Object(
			"choices", ax.Array(ax.Object("message", ax.Object("content", `{"answer":"ok"}`))),
		)),
	})
	client := ax.NewAI("vertex-ai", map[string]ax.Value{
		"apiKey":    "test-token",
		"api_key":   "test-token",
		"model":     "google/gemma-4-26b-a4b-it-maas",
		"baseUrl":   "https://example.invalid/v1beta1/projects/p/locations/l/endpoints/openapi",
		"base_url":  "https://example.invalid/v1beta1/projects/p/locations/l/endpoints/openapi",
		"transport": transport,
	})

	program := ax.NewAx("question:string -> answer:string", nil)
	_, _ = program.Forward(context.Background(), client,
		map[string]ax.Value{"question": "how many customers?"},
		map[string]ax.Value{"structured_output_mode": StructuredOutputAuto},
	)

	if len(transport.Requests) == 0 {
		t.Fatal("scripted transport captured no request")
	}
	raw, err := json.Marshal(transport.Requests[0])
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if !strings.Contains(body, `"response_format"`) || !strings.Contains(body, `"json_object"`) {
		t.Fatalf("auto mode did not resolve to a json_object request for the Gemma rule:\n%s", truncateForLog(body))
	}
	if strings.Contains(body, `"json_schema"`) {
		t.Fatalf("native JSON Schema is excluded for this model but was requested:\n%s", truncateForLog(body))
	}
}

func truncateForLog(s string) string {
	if len(s) > 1200 {
		return s[:1200] + "…"
	}
	return s
}

// Every published DeepSeek run sets GJ_AGENT_REASONING, which wraps the client
// in reasoningClient. Ax resolves the structured-output mode from the client's
// GetFeatures (axllm.go:956), so a wrapper that hid it would silently degrade
// auto-mode selection exactly when reasoning is on. Prove the wrapped client
// still resolves the Gemma rule to json_object and still requests thinking.
func TestVertexGemmaAutoModeSurvivesReasoningWrapper(t *testing.T) {
	transport := ax.NewScriptedTransport([]ax.Value{
		ax.Object("status", float64(200), "json", ax.Object(
			"choices", ax.Array(ax.Object("message", ax.Object("content", `{"answer":"ok"}`))),
		)),
	})
	inner := ax.NewAI("vertex-ai", map[string]ax.Value{
		"apiKey":    "test-token",
		"api_key":   "test-token",
		"model":     "google/gemma-4-26b-a4b-it-maas",
		"baseUrl":   "https://example.invalid/v1beta1/projects/p/locations/l/endpoints/openapi",
		"base_url":  "https://example.invalid/v1beta1/projects/p/locations/l/endpoints/openapi",
		"transport": transport,
	})
	client := &reasoningClient{inner: inner, budget: "high"}

	program := ax.NewAx("question:string -> answer:string", nil)
	_, _ = program.Forward(context.Background(), client,
		map[string]ax.Value{"question": "how many customers?"},
		map[string]ax.Value{"structured_output_mode": StructuredOutputAuto},
	)

	if len(transport.Requests) == 0 {
		t.Fatal("scripted transport captured no request")
	}
	raw, err := json.Marshal(transport.Requests[0])
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if !strings.Contains(body, `"response_format"`) || !strings.Contains(body, `"json_object"`) {
		t.Fatalf("wrapped client lost the Gemma json_object rule:\n%s", truncateForLog(body))
	}
	if strings.Contains(body, `"json_schema"`) {
		t.Fatalf("wrapped client requested excluded native JSON Schema:\n%s", truncateForLog(body))
	}
	if !strings.Contains(body, "enable_thinking") {
		t.Fatalf("Gemma thinking request transform missing under the wrapper:\n%s", truncateForLog(body))
	}
}
