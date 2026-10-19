package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
)

func TestModelUsageFollowsEachAdmittedCall(t *testing.T) {
	answers := []string{
		`{"javascriptCode":"final('Finish',{})"}`,
		`{"javascriptCode":"final('Answer',{})"}`,
		`{"answer":"Done"}`,
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		index := int(calls.Add(1)) - 1
		if index >= len(answers) {
			http.Error(w, "unexpected call", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ax.Object("choices", ax.Array(ax.Object("message", ax.Object("role", "assistant", "content", answers[index]), "finish_reason", "stop")),
			"usage", ax.Object("prompt_tokens", (index+1)*10, "completion_tokens", index+3, "total_tokens", (index+1)*10+index+3)))
	}))
	defer server.Close()
	client := ax.NewOpenAICompatibleClient(ax.Object("base_url", server.URL, "api_key", "synthetic", "model", "fixture"))
	var events []Event
	result, err := RunWithTools(context.Background(), Spec{Version: 1, RunID: "usage", InputID: "input", Prompt: "Answer"},
		client, Tools{}, func(e Event) error { events = append(events, e); return nil })
	if err != nil || result.Status != "completed" || result.Usage == nil || result.Usage.Requests != 3 || result.Usage.Reported != 3 ||
		result.Usage.InputTokens != 60 || result.Usage.OutputTokens != 12 || result.Usage.TotalTokens != 72 || result.Usage.Coverage != "complete" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	var started, finished int
	for _, event := range events {
		switch event.Type {
		case "model.request.started":
			started++
			if event.CallID != uint64(started) {
				t.Fatalf("wrong call admission: %+v", event)
			}
		case "model.request.finished":
			finished++
			if event.CallID != uint64(finished) || event.Usage == nil || event.Usage.Reported != 1 {
				t.Fatalf("missing per-call usage: %+v", event)
			}
		}
	}
	if started != 3 || finished != 3 || calls.Load() != 3 {
		t.Fatalf("starts=%d finishes=%d model=%d", started, finished, calls.Load())
	}
}

func TestModelUsageMarksMissingProviderReport(t *testing.T) {
	answers := []string{
		`{"javascriptCode":"final('Finish',{})"}`,
		`{"javascriptCode":"final('Answer',{})"}`,
		`{"answer":"Done"}`,
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		index := int(calls.Add(1)) - 1
		if index >= len(answers) {
			http.Error(w, "unexpected call", http.StatusBadRequest)
			return
		}
		response := ax.Object("choices", ax.Array(ax.Object("message", ax.Object("role", "assistant", "content", answers[index]), "finish_reason", "stop")))
		if index != 1 {
			response["usage"] = ax.Object("prompt_tokens", 10, "completion_tokens", 2, "total_tokens", 12)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()
	client := ax.NewOpenAICompatibleClient(ax.Object("base_url", server.URL, "api_key", "synthetic", "model", "fixture"))
	result, err := RunWithTools(context.Background(), Spec{Version: 1, RunID: "partial-usage", InputID: "input", Prompt: "Answer"},
		client, Tools{}, func(Event) error { return nil })
	if err != nil || result.Status != "completed" || result.Usage == nil || result.Usage.Requests != 3 || result.Usage.Reported != 2 ||
		result.Usage.TotalTokens != 24 || result.Usage.Coverage != "partial" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestObservedTokenBudgetStopsBeforeNextModelRequest(t *testing.T) {
	answers := []string{`{"javascriptCode":"final('Do work',{})"}`, `{"javascriptCode":"final('Answer',{})"}`}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		index := int(calls.Add(1)) - 1
		if index >= len(answers) {
			http.Error(w, "unexpected call", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ax.Object("choices", ax.Array(ax.Object("message", ax.Object("role", "assistant", "content", answers[index]), "finish_reason", "stop")),
			"usage", ax.Object("prompt_tokens", 4000, "completion_tokens", 1000, "total_tokens", 5000)))
	}))
	defer server.Close()
	client := ax.NewOpenAICompatibleClient(ax.Object("base_url", server.URL, "api_key", "synthetic", "model", "fixture"))
	var events []Event
	result, err := RunWithTools(context.Background(), Spec{Version: 1, RunID: "token-stop", InputID: "input", Prompt: "Answer", MaxModelTokens: 11000},
		client, Tools{}, func(e Event) error { events = append(events, e); return nil })
	if err != nil || result.Status != "failed" || result.Code != "model_token_budget_exceeded" || calls.Load() != 2 {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, calls.Load())
	}
	if result.Usage == nil || result.Usage.TotalTokens != 10000 || result.Usage.Coverage != "complete" {
		t.Fatalf("usage=%+v", result.Usage)
	}
	var admissions int
	for _, event := range events {
		if event.Type == "model.request.started" {
			admissions++
		}
	}
	if admissions != 2 {
		t.Fatalf("admissions=%d", admissions)
	}
}

// TestDelegatedTokensCountAgainstTheBudget: a tool that reports tokens
// another agent spent for the run (GraphJin's agent answering a data
// question) uses up the run's token budget like the run's own calls.
func TestDelegatedTokensCountAgainstTheBudget(t *testing.T) {
	answers := []string{
		`{"javascriptCode":"final('Ask the data agent',{});"}`,
		`{"javascriptCode":"const answer=ask({instruction:'revenue'}); console.log(JSON.stringify(answer));"}`,
		`{"javascriptCode":"final('Report',{});"}`,
		`{"answer":"done"}`,
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		index := int(calls.Add(1)) - 1
		if index >= len(answers) {
			http.Error(w, "unexpected call", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ax.Object("choices", ax.Array(ax.Object("message", ax.Object("role", "assistant", "content", answers[index]), "finish_reason", "stop")),
			"usage", ax.Object("prompt_tokens", 80, "completion_tokens", 20, "total_tokens", 100)))
	}))
	defer server.Close()
	client := ax.NewOpenAICompatibleClient(ax.Object("base_url", server.URL, "api_key", "synthetic", "model", "fixture"))
	ask := Capability{Name: "ask", Version: "1", Origin: "fixture", Effect: "read", Description: "Ask the data agent.",
		InputSchema: json.RawMessage(`{"type":"object","required":["instruction"],"properties":{"instruction":{"type":"string"}}}`),
		Call: func(context.Context, json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"content":["Revenue was 1200."],"is_error":false,"delegated_usage":{"total_tokens":50000}}`), nil
		}}
	result, err := RunWithTools(context.Background(), Spec{Version: 1, RunID: "delegated", InputID: "input", Prompt: "Answer", MaxModelTokens: 20000, MaxOperations: 4},
		client, Tools{Capabilities: []Capability{ask}}, func(Event) error { return nil })
	if err != nil || result.Status != "failed" || result.Code != "model_token_budget_exceeded" {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, calls.Load())
	}
	if result.Usage == nil || result.Usage.DelegatedTokens != 50000 {
		t.Fatalf("usage=%+v", result.Usage)
	}
}
