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

// The provider fixture records what the model actually receives during an
// ordinary multi-step run. Recovery projections are covered separately.
func TestLongRunKeepsUserConstraintWithoutReplayingAllObservations(t *testing.T) {
	const constraint = "Only report a verified REF-42 receipt"
	var mu sync.Mutex
	var requests []string
	var actionCalls int
	var summarizerCalls int
	var reads atomic.Int32
	answers := []string{
		`{"javascriptCode":"final('Read and verify the receipt',{});"}`,
		`{"javascriptCode":"const row1=read({step:1}); console.log('irrelevant-observation-'.repeat(600));"}`,
		`{"javascriptCode":"const row2=read({step:2}); console.log('irrelevant-observation-'.repeat(600));"}`,
		`{"javascriptCode":"const row3=read({step:3}); console.log('irrelevant-observation-'.repeat(600));"}`,
		`{"javascriptCode":"const row4=read({step:4}); const saved4=harnessSavedOperation(4); final('Report verified receipt',{receipt:saved4.result.receipt});"}`,
		`{"answer":"REF-42"}`,
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		requests = append(requests, string(body))
		isSummary := strings.Contains(string(body), "Working Code State (verbatim)")
		index := actionCalls
		if isSummary {
			summarizerCalls++
		} else {
			actionCalls++
		}
		mu.Unlock()
		if !isSummary && index >= len(answers) {
			http.Error(w, "unexpected model call", http.StatusBadRequest)
			return
		}
		content := "Objective: verify the receipt. User constraints and preferences: Only report a verified REF-42 receipt. Evidence: REF-42 from read."
		if !isSummary {
			content = answers[index]
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ax.Object("choices", ax.Array(ax.Object("message", ax.Object("role", "assistant", "content", content), "finish_reason", "stop"))))
	}))
	defer server.Close()
	client := ax.NewOpenAICompatibleClient(ax.Object("base_url", server.URL, "api_key", "synthetic", "model", "fixture"))
	read := Capability{Name: "read", Version: "1", Origin: "fixture", Effect: "read", Description: "Read a receipt and a large irrelevant observation.",
		InputSchema: json.RawMessage(`{"type":"object","required":["step"],"properties":{"step":{"type":"integer"}}}`),
		Call: func(context.Context, json.RawMessage) (json.RawMessage, error) {
			reads.Add(1)
			return json.Marshal(ax.Object("receipt", "REF-42", "noise", strings.Repeat("irrelevant-observation-", 600)))
		}}
	result, err := RunWithTools(context.Background(), Spec{Version: 1, RunID: "long-context", InputID: "input", Prompt: constraint, MaxOperations: 8}, client, Tools{Capabilities: []Capability{read}}, func(Event) error { return nil })
	mu.Lock()
	seen := append([]string(nil), requests...)
	steps, summaries := actionCalls, summarizerCalls
	mu.Unlock()
	if err != nil || result.Status != "completed" || result.Answer != "REF-42" || reads.Load() != 4 || steps != len(answers) || len(seen) != steps+summaries {
		t.Fatalf("result=%+v err=%v reads=%d requests=%d actions=%d summaries=%d", result, err, reads.Load(), len(seen), steps, summaries)
	}
	var largest int
	for i, request := range seen {
		if strings.Contains(request, "Working Code State (verbatim)") {
			continue
		}
		if !strings.Contains(request, constraint) {
			t.Fatalf("request %d lost original constraint", i+1)
		}
		if len(request) > largest {
			largest = len(request)
		}
	}
	if largest > 45_000 {
		t.Fatalf("executor request replayed excessive observations: %d bytes", largest)
	}
	if !strings.Contains(strings.Join(seen, ""), "truncated from") {
		t.Fatal("large diagnostics were not visibly truncated")
	}
}
