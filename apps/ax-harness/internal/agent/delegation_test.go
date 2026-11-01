package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
)

func TestOwnedChildSharesRunBudgetAndReadScope(t *testing.T) {
	answers := []string{
		`{"javascriptCode":"final('Find two references',{})"}`,
		`{"javascriptCode":"const one=team.worker({task:'Find first reference'}); const two=team.worker({task:'Find second reference'}); final('Two references',{one,two});"}`,
		`{"javascriptCode":"final('Find first reference',{})"}`,
		`{"javascriptCode":"const found=catalog({key:'first'}); final('Found',{found});"}`,
		`{"answer":"REF-1"}`,
		`{"javascriptCode":"final('Find second reference',{})"}`,
		`{"javascriptCode":"const found=catalog({key:'second'}); final('Found',{found});"}`,
		`{"answer":"REF-2"}`,
		`{"answer":"REF-1 and REF-2"}`,
	}
	var modelCalls, reads, writes atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		index := int(modelCalls.Add(1)) - 1
		if index >= len(answers) {
			http.Error(w, "unexpected model call", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ax.Object("choices", ax.Array(ax.Object("message", ax.Object("role", "assistant", "content", answers[index]), "finish_reason", "stop"))))
	}))
	defer model.Close()
	client := ax.NewOpenAICompatibleClient(ax.Object("base_url", model.URL, "api_key", "synthetic", "model", "fixture"))
	read := Capability{Name: "catalog", Version: "1", Origin: "fixture", Effect: "read", Description: "Read a fixture.", InputSchema: json.RawMessage(`{"type":"object","required":["key"],"properties":{"key":{"type":"string"}},"additionalProperties":false}`), Call: func(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
		reads.Add(1)
		return json.Marshal(map[string]json.RawMessage{"reference": raw})
	}}
	write := Capability{Name: "write", Version: "1", Origin: "fixture", Effect: "durable", Description: "Write a fixture.", InputSchema: json.RawMessage(`{"type":"object"}`), Call: func(context.Context, json.RawMessage) (json.RawMessage, error) {
		writes.Add(1)
		return json.RawMessage(`{}`), nil
	}}
	var events []Event
	result, err := RunWithTools(context.Background(), Spec{Version: 1, RunID: "child", InputID: "input", Prompt: "Find two references", MaxOperations: 4, MaxModelCalls: 16}, client, Tools{Capabilities: []Capability{read, write}, ChildTools: []string{"catalog"}}, func(e Event) error { events = append(events, e); return nil })
	if err != nil || result.Status != "completed" || result.Answer != "REF-1 and REF-2" || reads.Load() != 2 || writes.Load() != 0 || result.Usage == nil || result.Usage.Requests != int(modelCalls.Load()) {
		t.Fatalf("result=%+v err=%v calls=%d reads=%d writes=%d events=%d", result, err, modelCalls.Load(), reads.Load(), writes.Load(), len(events))
	}
	started, finished := 0, 0
	for _, event := range events {
		if event.Type == "child.started" {
			started++
		}
		if event.Type == "child.finished" {
			finished++
		}
	}
	if started != 2 || finished != 2 {
		t.Fatalf("child lifecycle events started=%d finished=%d", started, finished)
	}
	modelCalls.Store(0)
	reads.Store(0)
	events = nil
	result, err = RunWithTools(context.Background(), Spec{Version: 1, RunID: "child-budget", InputID: "input", Prompt: "Find two references", MaxOperations: 4, MaxModelCalls: 4}, client, Tools{Capabilities: []Capability{read, write}, ChildTools: []string{"catalog"}}, func(e Event) error { events = append(events, e); return nil })
	// Three agent calls, then the reserved summary call.
	if err != nil || result.Status != "completed" || result.Kind != "summary" || result.Code != "model_calls_exhausted" || modelCalls.Load() != 4 || writes.Load() != 0 {
		t.Fatalf("child escaped shared budget: result=%+v err=%v calls=%d writes=%d", result, err, modelCalls.Load(), writes.Load())
	}
	modelCalls.Store(0)
	reads.Store(0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result, err = RunWithTools(ctx, Spec{Version: 1, RunID: "child-cancel", InputID: "input", Prompt: "Find two references", MaxOperations: 4, MaxModelCalls: 16}, client, Tools{Capabilities: []Capability{read, write}, ChildTools: []string{"catalog"}}, func(e Event) error {
		if e.Type == "model.request.started" && e.CallID == 3 {
			cancel()
		}
		return nil
	})
	if err != nil || result.Status != "cancelled" || reads.Load() != 0 || writes.Load() != 0 {
		t.Fatalf("child survived parent cancellation: result=%+v err=%v reads=%d writes=%d", result, err, reads.Load(), writes.Load())
	}
	modelCalls.Store(0)
	result, err = RunWithTools(context.Background(), Spec{Version: 1, RunID: "child-disabled", InputID: "input", Prompt: "Find two references", MaxOperations: 4, MaxModelCalls: 16}, client, Tools{Capabilities: []Capability{read, write}}, func(Event) error { return nil })
	if err != nil || result.Status != "failed" || reads.Load() != 0 || writes.Load() != 0 {
		t.Fatalf("disabled child was callable: result=%+v err=%v reads=%d writes=%d", result, err, reads.Load(), writes.Load())
	}
}

func TestChildToolErrorReachesTheParent(t *testing.T) {
	answers := []string{
		`{"javascriptCode":"final('Delegate verification',{})"}`,
		`{"javascriptCode":"const child=team.worker({task:'Verify reference'}); final('Use child result',{child});"}`,
		`{"javascriptCode":"final('Read the reference',{})"}`,
		`{"javascriptCode":"const found=catalog({key:'reference'}); final('Report evidence',{found});"}`,
		`Answer: The reference is unavailable.`,
		`Answer: The reference could not be verified.`,
	}
	var calls, reads atomic.Int32
	var mu sync.Mutex
	var bodies []string
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(body))
		mu.Unlock()
		index := int(calls.Add(1)) - 1
		if index >= len(answers) {
			http.Error(w, "unexpected model call", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ax.Object("choices", ax.Array(ax.Object("message", ax.Object("role", "assistant", "content", answers[index]), "finish_reason", "stop"))))
	}))
	defer model.Close()
	client := ax.NewOpenAICompatibleClient(ax.Object("base_url", model.URL, "api_key", "synthetic", "model", "fixture"))
	read := Capability{Name: "catalog", Version: "1", Origin: "fixture", Effect: "read", Description: "Read a reference.",
		InputSchema: json.RawMessage(`{"type":"object","required":["key"],"properties":{"key":{"type":"string"}},"additionalProperties":false}`),
		Call: func(context.Context, json.RawMessage) (json.RawMessage, error) {
			reads.Add(1)
			return json.RawMessage(`{"is_error":true,"content":["reference unavailable"]}`), nil
		}}
	result, err := RunWithTools(context.Background(), Spec{Version: 1, RunID: "child-failed-read", InputID: "input", Prompt: "Verify the reference", MaxOperations: 4, MaxModelCalls: 12}, client,
		Tools{Capabilities: []Capability{read}, ChildTools: []string{"catalog"}}, func(Event) error { return nil })
	if err != nil || result.Status != "completed" || reads.Load() != 1 {
		t.Fatalf("result=%+v err=%v reads=%d models=%d", result, err, reads.Load(), calls.Load())
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(bodies[4], "reference unavailable") {
		t.Fatal("the child responder did not see the tool error")
	}
}

func TestChildLargeResultReferenceStaysInChildRuntime(t *testing.T) {
	answers := []string{
		`{"javascriptCode":"final('Delegate read',{});"}`,
		`{"javascriptCode":"const child=team.worker({task:'Verify receipt'}); let escaped=false; try { harnessSavedOperation(1); } catch (err) { escaped=true; } if (!escaped) throw new Error('child receipt escaped'); final('Answer',{child});"}`,
		`{"javascriptCode":"final('Read receipt',{});"}`,
		`{"javascriptCode":"const ref=catalog({}); if (!ref.reference) throw new Error('missing reference'); const saved=harnessSavedOperation(1); final('Verified',{label:saved.result.label});"}`,
		`{"answer":"REF-42"}`,
		`{"answer":"REF-42"}`,
	}
	var calls, reads atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		index := int(calls.Add(1)) - 1
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), strings.Repeat("X", 1000)) {
			t.Error("child's large observation leaked into model context")
		}
		if index >= len(answers) {
			http.Error(w, "unexpected model call", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ax.Object("choices", ax.Array(ax.Object("message", ax.Object("role", "assistant", "content", answers[index]), "finish_reason", "stop"))))
	}))
	defer model.Close()
	client := &RoutedClient{AIClient: ax.NewOpenAICompatibleClient(ax.Object("base_url", model.URL, "api_key", "synthetic", "model", "fixture")),
		Stages: StageModels{Context: "fixture", Executor: "fixture", Responder: "fixture"}}
	read := Capability{Name: "catalog", Version: "1", Origin: "fixture", Effect: "read", Description: "Read a large receipt.",
		InputSchema: json.RawMessage(`{"type":"object"}`), Call: func(context.Context, json.RawMessage) (json.RawMessage, error) {
			reads.Add(1)
			return json.Marshal(ax.Object("label", "REF-42", "noise", strings.Repeat("X", 200_000)))
		}}
	modelRequests := 0
	result, err := RunWithTools(context.Background(), Spec{Version: 1, RunID: "child-reference", InputID: "input", Prompt: "Verify receipt", MaxOperations: 4, MaxModelCalls: 8}, client,
		Tools{Capabilities: []Capability{read}, ChildTools: []string{"catalog"}}, func(event Event) error {
			if event.Type == "model.request.started" {
				modelRequests++
			}
			return nil
		})
	if err != nil || result.Status != "completed" || reads.Load() != 1 || calls.Load() != int32(len(answers)) {
		t.Fatalf("result=%+v err=%v reads=%d calls=%d", result, err, reads.Load(), calls.Load())
	}
	if modelRequests != 6 {
		t.Fatalf("model requests = %d, want 6", modelRequests)
	}
}
