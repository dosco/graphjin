package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	ax "github.com/ax-llm/ax/packages/go"
)

// scripted answers each request with the next reply and records the bodies.
type scripted struct {
	mu      sync.Mutex
	replies []string
	bodies  []string
	finish  string
	model   string
}

func (s *scripted) server(t *testing.T) *httptest.Server {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		index := len(s.bodies)
		s.bodies = append(s.bodies, string(body))
		s.mu.Unlock()
		if index >= len(s.replies) {
			http.Error(w, "unexpected model call", http.StatusBadRequest)
			return
		}
		finish := "stop"
		if s.finish != "" && index == len(s.replies)-1 {
			finish = s.finish
		}
		response := ax.Object("choices", ax.Array(ax.Object("message", ax.Object("role", "assistant", "content", s.replies[index]), "finish_reason", finish)))
		if s.model != "" {
			response["model"] = s.model
			response["usage"] = ax.Object("prompt_tokens", 3, "completion_tokens", 2, "total_tokens", 5)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	t.Cleanup(server.Close)
	return server
}

func (s *scripted) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.bodies)
}

func (s *scripted) body(i int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bodies[i]
}

func openAIClient(server *httptest.Server) ax.AIClient {
	return ax.NewOpenAICompatibleClient(ax.Object("base_url", server.URL, "api_key", "synthetic", "model", "fixture", "retry", ax.Object("max_retries", 0)))
}

func TestSpecLimitsMatchHermes(t *testing.T) {
	base := Spec{Version: 1, RunID: "r", InputID: "i", Prompt: "p"}
	if !base.valid() || base.ActorSteps() != 25 || base.ChildSteps() != 50 || base.Timeout() != 9*time.Minute ||
		base.OperationLimit() != 0 || base.ModelCallLimit() != 0 {
		t.Fatalf("defaults: %+v", base)
	}
	invalid := []func(*Spec){
		func(s *Spec) { s.MaxActorSteps = 501 },
		func(s *Spec) { s.MaxChildSteps = 501 },
		func(s *Spec) { s.MaxOperations = 4001 },
		func(s *Spec) { s.MaxModelCalls = 2001 },
		func(s *Spec) { s.TimeoutMS = 999 },
		func(s *Spec) { s.TimeoutMS = 1_800_001 },
		func(s *Spec) { s.ContextWindowTokens, s.MaxOutputTokens = 1000, 1000 },
		func(s *Spec) { s.ReasoningEffort = "max" },
	}
	for i, change := range invalid {
		spec := base
		change(&spec)
		if spec.valid() {
			t.Fatalf("case %d accepted: %+v", i, spec)
		}
	}
	large := base
	large.Prompt, large.MaxActorSteps, large.MaxOperations, large.MaxModelCalls = strings.Repeat("x", 3<<20), 500, 4000, 2000
	if !large.valid() {
		t.Fatal("a 3 MiB prompt and the upper limits were rejected")
	}
}

func TestContextWindowStopsAnOversizedPrompt(t *testing.T) {
	model := &scripted{}
	server := model.server(t)
	result, err := RunWithTools(context.Background(), Spec{Version: 1, RunID: "window", InputID: "i", Prompt: strings.Repeat("x", 8000), ContextWindowTokens: 1000},
		openAIClient(server), Tools{}, func(Event) error { return nil })
	if err != nil || result.Status != "failed" || result.Code != "model_context_overflow" || model.calls() != 0 {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, model.calls())
	}
}

func TestMaxOutputTokensAndReasoningReachTheRequest(t *testing.T) {
	model := &scripted{replies: []string{`{"javascriptCode":"final('Answer',{})"}`, `{"javascriptCode":"final('Answer',{})"}`, `Answer: done`}}
	server := model.server(t)
	result, err := RunWithTools(context.Background(), Spec{Version: 1, RunID: "config", InputID: "i", Prompt: "p", MaxOutputTokens: 777, ReasoningEffort: "high"},
		openAIClient(server), Tools{}, func(Event) error { return nil })
	if err != nil || result.Status != "completed" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	for i := 0; i < model.calls(); i++ {
		if !strings.Contains(model.body(i), `"max_completion_tokens":777`) || !strings.Contains(model.body(i), `"reasoning_effort":"high"`) {
			t.Fatalf("request %d lacks the output cap or reasoning effort: %s", i, model.body(i))
		}
	}
}

func TestRunTimeoutComesFromSpec(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer server.Close()
	start := time.Now()
	result, err := RunWithTools(context.Background(), Spec{Version: 1, RunID: "timeout", InputID: "i", Prompt: "p", TimeoutMS: 1000},
		openAIClient(server), Tools{}, func(Event) error { return nil })
	if err != nil || result.Code != "deadline_exceeded" || time.Since(start) > 4*time.Second {
		t.Fatalf("result=%+v err=%v elapsed=%s", result, err, time.Since(start))
	}
}

func echoTool(calls *int, mu *sync.Mutex, result string) Capability {
	return Capability{Name: "lookup_data", Version: "1", Origin: "fixture", Effect: "read", Description: "Read data.",
		InputSchema: json.RawMessage(`{"type":"object"}`), Call: func(context.Context, json.RawMessage) (json.RawMessage, error) {
			mu.Lock()
			*calls++
			mu.Unlock()
			return json.Marshal(map[string]string{"data": result})
		}}
}

func TestBudgetExitWritesOneSummary(t *testing.T) {
	cases := []struct {
		name, code string
		spec       Spec
		replies    []string
	}{
		{"actor steps", "actor_steps_exhausted", Spec{MaxActorSteps: 1}, []string{
			`{"javascriptCode":"final('Look',{})"}`, `{"javascriptCode":"console.log('still working')"}`, `Answer: Partial answer.`}},
		{"operations", "operations_exhausted", Spec{MaxActorSteps: 2, MaxOperations: 1}, []string{
			`{"javascriptCode":"final('Look',{})"}`, `{"javascriptCode":"const a=lookup_data({}); const b=lookup_data({}); console.log(b.error)"}`,
			`{"javascriptCode":"console.log('no tools left')"}`, `Answer: Partial answer.`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			model := &scripted{replies: tc.replies}
			server := model.server(t)
			var mu sync.Mutex
			calls := 0
			spec := tc.spec
			spec.Version, spec.RunID, spec.InputID, spec.Prompt = 1, "budget", "i", "Find the data"
			result, err := RunWithTools(context.Background(), spec, openAIClient(server), Tools{Capabilities: []Capability{echoTool(&calls, &mu, "row-1")}}, func(Event) error { return nil })
			if err != nil || result.Status != "completed" || result.Kind != "summary" || result.Code != tc.code || result.Answer != "Partial answer." || model.calls() != len(tc.replies) {
				t.Fatalf("result=%+v err=%v calls=%d", result, err, model.calls())
			}
			last := model.body(model.calls() - 1)
			if strings.Contains(last, "lookup_data(input)") || !strings.Contains(last, "budget before it finished") {
				t.Fatal("the summary call offered tools or lost its instruction")
			}
		})
	}
}

func TestLargeToolResultsFollowHermesSizes(t *testing.T) {
	model := &scripted{replies: []string{
		`{"javascriptCode":"final('Read',{})"}`,
		`{"javascriptCode":"const big=lookup_data({}); if (!big.reference || big.result_preview.length !== 1500) throw new Error('large result was inline'); final('Done',{ok:true});"}`,
		`Answer: done`,
	}}
	server := model.server(t)
	var mu sync.Mutex
	calls := 0
	result, err := RunWithTools(context.Background(), Spec{Version: 1, RunID: "sizes", InputID: "i", Prompt: "p"}, openAIClient(server),
		Tools{Capabilities: []Capability{echoTool(&calls, &mu, strings.Repeat("y", 120_000))}}, func(Event) error { return nil })
	if err != nil || result.Status != "completed" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestOneStepReceivesManyFullResults(t *testing.T) {
	model := &scripted{replies: []string{
		`{"javascriptCode":"final('Read pages',{})"}`,
		`{"javascriptCode":"for (let i = 0; i < 12; i++) { if (lookup_data({}).reference) throw new Error('page ' + i + ' was saved'); } final('Done',{ok:true});"}`,
		`Answer: done`,
	}}
	server := model.server(t)
	var mu sync.Mutex
	calls := 0
	result, err := RunWithTools(context.Background(), Spec{Version: 1, RunID: "pages", InputID: "i", Prompt: "p"}, openAIClient(server),
		Tools{Capabilities: []Capability{echoTool(&calls, &mu, strings.Repeat("y", 30_000))}}, func(Event) error { return nil })
	if err != nil || result.Status != "completed" || calls != 12 {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, calls)
	}
}

func TestModelEventsCarryObservedModelAndProvider(t *testing.T) {
	model := &scripted{model: "served-model-7", replies: []string{`{"javascriptCode":"final('Answer',{})"}`, `{"javascriptCode":"final('Answer',{})"}`, `Answer: done`}}
	server := model.server(t)
	var finished []Event
	result, err := RunWithTools(context.Background(), Spec{Version: 1, RunID: "identity", InputID: "i", Prompt: "p"}, openAIClient(server), Tools{}, func(e Event) error {
		if e.Type == "model.request.finished" {
			finished = append(finished, e)
		}
		return nil
	})
	if err != nil || result.Status != "completed" || len(finished) != 3 {
		t.Fatalf("result=%+v err=%v finished=%d", result, err, len(finished))
	}
	for _, e := range finished {
		if e.ObservedModel != "served-model-7" || e.Provider != "openai-compatible" {
			t.Fatalf("model receipt lacks identity: %+v", e)
		}
	}
}

func TestOverflowAndTruncationHaveTheirOwnCodes(t *testing.T) {
	overflow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"message":"This model's maximum context length is 8192 tokens.","type":"invalid_request_error","code":"context_length_exceeded"}}`)
	}))
	defer overflow.Close()
	result, err := RunWithTools(context.Background(), Spec{Version: 1, RunID: "overflow", InputID: "i", Prompt: "p"}, openAIClient(overflow), Tools{}, func(Event) error { return nil })
	if err != nil || result.Code != "model_context_overflow" {
		t.Fatalf("overflow result=%+v err=%v", result, err)
	}
	model := &scripted{finish: "length", replies: []string{`{"javascriptCode":"final('Answer',{})"}`, `{"javascriptCode":"final('Answer',{})"}`, `Answer: partial tex`}}
	server := model.server(t)
	result, err = RunWithTools(context.Background(), Spec{Version: 1, RunID: "length", InputID: "i", Prompt: "p"}, openAIClient(server), Tools{}, func(Event) error { return nil })
	if err != nil || result.Code != "model_output_truncated" {
		t.Fatalf("truncation result=%+v err=%v", result, err)
	}
}
