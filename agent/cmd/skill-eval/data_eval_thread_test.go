package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// threadServer mimics serv's shapes: threads are created and deleted through
// the gj_thread GraphQL root (an insert answers one object, a delete answers
// {id, deleted}), and a turn returns the agent response with thread_id and
// run_id beside it. The removed thread REST paths 404, as they do in serv. It
// refuses a turn without a thread, so a case that silently fell back to the
// direct route fails here as it would against the lead.
type threadServer struct {
	mu      sync.Mutex
	turns   []string
	deleted []string
	answer  string
}

func (f *threadServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	body, _ := io.ReadAll(r.Body)
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/graphql":
		var req struct {
			Query     string
			Variables map[string]any
		}
		_ = json.Unmarshal(body, &req)
		switch {
		case strings.Contains(req.Query, "gj_thread(insert:"):
			_, _ = io.WriteString(w, `{"data":{"gj_thread":{"id":"thread-1"}}}`)
		case strings.Contains(req.Query, "gj_thread(delete:"):
			id, _ := req.Variables["id"].(string)
			f.deleted = append(f.deleted, id)
			_, _ = io.WriteString(w, `{"data":{"gj_thread":{"id":"`+id+`","deleted":true}}}`)
		case strings.Contains(req.Query, "sum_mrr_cents"):
			_, _ = io.WriteString(w, `{"data":{"accounts":[{"sum_mrr_cents":1980000}]}}`)
		default:
			_, _ = io.WriteString(w, `{"data":{"support_tickets":[{"count_id":4}]}}`)
		}
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/agent":
		var req struct {
			Instruction string `json:"instruction"`
			ThreadID    string `json:"thread_id"`
		}
		_ = json.Unmarshal(body, &req)
		if req.ThreadID != "thread-1" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":"no thread"}`)
			return
		}
		f.turns = append(f.turns, req.Instruction)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "answered", "answer": f.answer, "thread_id": req.ThreadID, "run_id": "run-1",
			"data": map[string]any{"delegations": []any{
				map[string]any{"question": "total MRR?"}, map[string]any{"question": "open tickets?"},
			}},
			"actions": []any{
				map[string]any{"tool": "execute_graphql", "status": "ok", "delegate": 0, "args": map[string]any{"query": "{ accounts { sum_mrr_cents } }"}},
				map[string]any{"tool": "execute_graphql", "status": "ok", "delegate": 1, "args": map[string]any{"query": "{ support_tickets { count_id } }"}},
			},
		})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func threadCase() dataEvalCase {
	return dataEvalCase{
		ID: "lead-two-parts", Group: "multi", Route: routeThread, ExpectedStatus: "answered",
		Turns:  []string{"Hi, I run the ops team."},
		Prompt: "What's our total MRR and how many tickets are open?",
		Parts: []dataPart{
			{Label: "mrr", Oracle: dataOracle{Query: "query { accounts { sum_mrr_cents } }", Extract: "accounts.0.sum_mrr_cents"},
				Answer: answerRule{Kind: "number", AcceptScales: []float64{1, 0.01}}, Method: methodRule{RequireQueryMatch: []string{"sum_mrr_cents"}}},
			{Label: "open", Oracle: dataOracle{Query: "query { support_tickets { count_id } }", Extract: "support_tickets.0.count_id"},
				Answer: answerRule{Kind: "number"}, Method: methodRule{RequireQueryMatch: []string{"count_"}}},
		},
	}
}

func TestThreadCaseRunsEveryTurnInOneThreadAndScoresEachPart(t *testing.T) {
	fake := &threadServer{answer: "Total MRR is $19,800 and 4 tickets are open."}
	server := httptest.NewServer(fake)
	defer server.Close()
	profile := endpointProfile{Name: "user", URL: server.URL + "/api/v1/agent"}

	result := runDataOne(server.Client(), dataTask{testCase: threadCase(), repeat: 1, profile: profile}, 1)

	if result.Error != "" {
		t.Fatalf("run error: %s", result.Error)
	}
	if strings.Join(fake.turns, "|") != "Hi, I run the ops team.|What's our total MRR and how many tickets are open?" {
		t.Fatalf("turns = %q, want the earlier turn then the scored prompt in one thread", fake.turns)
	}
	if len(fake.deleted) != 1 || fake.deleted[0] != "thread-1" {
		t.Fatalf("the eval thread must be deleted, got %v", fake.deleted)
	}
	if result.GroundTruthPass == nil || !*result.GroundTruthPass || result.PartsPassed != 2 || result.PartsTotal != 2 {
		t.Fatalf("ground truth = %v (%d/%d): %s", result.GroundTruthPass, result.PartsPassed, result.PartsTotal, result.GroundTruthDetail)
	}
	if result.MethodPass == nil || !*result.MethodPass || result.Delegations != 2 || len(result.Delegated) != 2 || result.Route != routeThread {
		t.Fatalf("method = %v, delegations = %d, route = %q", result.MethodPass, result.Delegations, result.Route)
	}
	if result.OracleValue != "mrr: 1980000; open: 4" {
		t.Fatalf("oracle value = %q", result.OracleValue)
	}
}

// A multi-part answer that gets one part wrong fails the case, and the report
// says which part.
func TestThreadCaseFailsOnAnyWrongPart(t *testing.T) {
	fake := &threadServer{answer: "Total MRR is $19,800 and 7 tickets are open."}
	server := httptest.NewServer(fake)
	defer server.Close()
	profile := endpointProfile{Name: "user", URL: server.URL + "/api/v1/agent"}

	result := runDataOne(server.Client(), dataTask{testCase: threadCase(), repeat: 1, profile: profile}, 1)

	if result.GroundTruthPass == nil || *result.GroundTruthPass || result.PartsPassed != 1 {
		t.Fatalf("one wrong part must fail the case: %v (%d passed)", result.GroundTruthPass, result.PartsPassed)
	}
	if !strings.HasPrefix(result.GroundTruthDetail, "open: ") {
		t.Fatalf("the detail must name the failing part: %q", result.GroundTruthDetail)
	}
	metrics := calculateDataMetrics(aggregateDataVerdicts([]dataEvalCase{threadCase()}, []runResult{result}), []runResult{result})
	if metrics.PartRecall != 0.5 || metrics.MedianDelegations != 2 {
		t.Fatalf("part recall = %v, delegations = %v", metrics.PartRecall, metrics.MedianDelegations)
	}
}

func TestCollidingPartsAreFlagged(t *testing.T) {
	parts := []dataPart{
		{Label: "a", Answer: answerRule{Kind: "number", AcceptScales: []float64{1, 0.01}}},
		{Label: "b", Answer: answerRule{Kind: "number"}},
	}
	if collidingParts(parts, []string{"400", "4"}) == "" {
		t.Fatal("400 cents shown as dollars is 4: the parts collide")
	}
	if collidingParts(parts, []string{"1980000", "4"}) != "" {
		t.Fatal("distinct values must not be flagged")
	}
}
