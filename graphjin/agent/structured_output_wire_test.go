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

// A run that enables reasoning pays for the thinking and then could not see it:
// across 80 recorded benchmark episodes every one carried a chat_log and not one
// carried a thought, so "why did the model do that?" was answerable only by
// inference from the code the model emitted. Providers gate the reasoning text
// behind a flag separate from the budget, and only the budget was being sent.
//
// This pins graphjin's half — that both spellings ride the same model_config ax
// merges. Whether it reaches the wire is ax's half: the ported Gemini path
// currently builds no thinkingConfig at all, so neither the budget nor the
// thoughts are forwarded (ax-llm/ax, mirrored from api.ts:1148).
func TestReasoningWrapperAsksForTheThinkingItPaysFor(t *testing.T) {
	client := &reasoningClient{budget: "high", showThoughts: true}
	req := client.withBudget(map[string]ax.Value{})

	config, ok := req["model_config"].(map[string]ax.Value)
	if !ok {
		t.Fatalf("model_config missing: %#v", req)
	}
	for _, key := range []string{"thinkingTokenBudget", "thinking_token_budget"} {
		if config[key] != "high" {
			t.Fatalf("%s = %#v, want the configured budget", key, config[key])
		}
	}
	// The budget alone makes the model think in private. These return the text.
	for _, key := range []string{"showThoughts", "show_thoughts"} {
		if config[key] != true {
			t.Fatalf("%s = %#v, want the thoughts requested back", key, config[key])
		}
	}
}

// The two halves joined: graphjin puts showThoughts and the budget on the
// model_config, and ax now turns both into Gemini's generationConfig.thinkingConfig
// (ax-llm/ax#620). Pinning it here because each half is inert alone, and a future
// ax bump that dropped the mapping would otherwise cost another silent run.
func TestReasoningReachesTheGeminiWire(t *testing.T) {
	transport := ax.NewScriptedTransport([]ax.Value{
		ax.Object("status", float64(200), "json", ax.Object(
			"candidates", ax.Array(ax.Object(
				"content", ax.Object("parts", ax.Array(ax.Object("text", `{"answer":"ok"}`))),
				"finishReason", "STOP",
			)),
		)),
	})
	inner := ax.NewAI("google-gemini", map[string]ax.Value{
		"apiKey": "test-token", "api_key": "test-token",
		"model": "gemini-3.5-flash", "transport": transport,
	})
	program := ax.NewAx("question:string -> answer:string", nil)
	_, _ = program.Forward(context.Background(), &reasoningClient{inner: inner, budget: "high", showThoughts: true},
		map[string]ax.Value{"question": "how many customers?"}, nil)

	if len(transport.Requests) == 0 {
		t.Fatal("scripted transport captured no request")
	}
	raw, err := json.Marshal(transport.Requests[0])
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if !strings.Contains(body, "thinkingConfig") {
		t.Fatalf("no thinkingConfig reached the wire:\n%s", truncateForLog(body))
	}
	if !strings.Contains(body, "includeThoughts") {
		t.Fatalf("thinking was requested but the thoughts were not:\n%s", truncateForLog(body))
	}
}

// These models think by default, so returning the reasoning text changes what
// is observable, not what the model does. Seeing it therefore must not require
// setting a thinking budget, which does change behaviour: the first version
// shipped showThoughts only inside the reasoning wrapper, so the only way to
// read the thinking was to alter the run producing it.
func TestShowThoughtsIsIndependentOfTheThinkingBudget(t *testing.T) {
	observeOnly := (&reasoningClient{showThoughts: true}).withBudget(map[string]ax.Value{})
	config, _ := observeOnly["model_config"].(map[string]ax.Value)
	if config["showThoughts"] != true {
		t.Fatalf("observation-only client did not ask for the thoughts: %#v", config)
	}
	for _, key := range []string{"thinkingTokenBudget", "thinking_token_budget"} {
		if _, set := config[key]; set {
			t.Fatalf("observing the thinking also set %s, which changes the run: %#v", key, config)
		}
	}

	// And a budget without the switch stays silent, as before.
	budgetOnly := (&reasoningClient{budget: "high"}).withBudget(map[string]ax.Value{})
	bc, _ := budgetOnly["model_config"].(map[string]ax.Value)
	if bc["thinkingTokenBudget"] != "high" {
		t.Fatalf("budget was not applied: %#v", bc)
	}
	if _, set := bc["showThoughts"]; set {
		t.Fatalf("a budget alone must not start returning reasoning text: %#v", bc)
	}
}
