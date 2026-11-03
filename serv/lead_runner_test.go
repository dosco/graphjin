package serv

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	ax "github.com/ax-llm/ax/packages/go"
	gjagent "github.com/dosco/graphjin/agent/v3"
)

// leadModelClient replays the lead model's responses and records each request.
type leadModelClient struct {
	mu        sync.Mutex
	responses []ax.Value
	requests  []string
}

func (c *leadModelClient) Chat(_ context.Context, req map[string]ax.Value, _ map[string]ax.Value) (ax.Value, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	raw, _ := json.Marshal(req)
	c.requests = append(c.requests, string(raw))
	if len(c.responses) == 0 {
		return nil, errors.New("lead model script exhausted")
	}
	out := c.responses[0]
	c.responses = c.responses[1:]
	return out, nil
}

func (c *leadModelClient) Embed(context.Context, map[string]ax.Value, map[string]ax.Value) (ax.Value, error) {
	return nil, nil
}

func (c *leadModelClient) Stream(context.Context, map[string]ax.Value, map[string]ax.Value) ([]ax.Value, error) {
	return nil, nil
}

func leadAsks(questions ...string) ax.Value {
	qs := make([]ax.Value, 0, len(questions))
	for _, q := range questions {
		qs = append(qs, q)
	}
	return ax.Object("results", ax.Array(ax.Object(
		"content", "",
		"function_calls", ax.Array(ax.Object("id", "call_1", "type", "function",
			"function", ax.Object("name", "ask_graphjin", "params", ax.Object("questions", ax.Array(qs...))))),
	)))
}

func leadAnswers(answer string) ax.Value {
	content, _ := json.Marshal(map[string]string{"status": "answered", "answer": answer})
	return ax.Object("results", ax.Array(ax.Object("content", string(content), "function_calls", ax.Array())))
}

// statelessWorker answers each question and records the requests it saw.
type statelessWorker struct {
	mu       sync.Mutex
	requests []gjagent.Request
	answers  map[string]string
}

func (w *statelessWorker) Run(_ context.Context, req gjagent.Request) (gjagent.Response, error) {
	w.mu.Lock()
	w.requests = append(w.requests, req)
	w.mu.Unlock()
	if req.Observer != nil {
		req.Observer(gjagent.ActionEvent{Index: 1, Source: "model", Tool: "execute_graphql", Status: "ok"})
	}
	time.Sleep(20 * time.Millisecond)
	return gjagent.Response{Status: "answered", Answer: w.answers[req.Instruction]}, nil
}

// A thread turn runs the lead: the conversation reaches the lead's model,
// each worker gets only its own question, and every delegated step lands in
// the run's event log in order.
func TestThreadTurnRunsTheLeadOverStatelessWorkers(t *testing.T) {
	svc := newSQLiteThreadService(t)
	hs := &HttpService{}
	hs.Store(svc)
	worker := &statelessWorker{answers: map[string]string{
		"How many open tickets are there?":         "2 open tickets",
		"How many roast batches ran this week?":    "5 batches",
		"Which of Harbor Cafe's tickets are open?": "Ticket 7 is open",
	}}
	prev := newGraphJinAgentRunner
	newGraphJinAgentRunner = func(*graphjinService, gjagent.Config, ...gjagent.Option) (graphjinAgentRunner, error) {
		return worker, nil
	}
	t.Cleanup(func() { newGraphJinAgentRunner = prev })
	model := &leadModelClient{responses: []ax.Value{
		leadAsks("How many open tickets are there?", "How many roast batches ran this week?"),
		leadAnswers("There are 2 open tickets and 5 roast batches this week."),
		leadAsks("Which of Harbor Cafe's tickets are open?"),
		leadAnswers("Ticket 7 is open."),
	}}
	svc.agentClientFactory = func(gjagent.Config) (ax.AIClient, error) { return model, nil }

	ctx := threadUserCtx("user_1")
	thread, err := svc.createThread(ctx, threadSpec{})
	if err != nil {
		t.Fatal(err)
	}
	threadID := thread["id"].(string)

	rec := postThreadTurn(t, hs, ctx, `{"instruction":"Tickets and batches?","thread_id":"`+threadID+`"}`, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("first turn = %d: %s", rec.Code, rec.Body.String())
	}
	var first threadAgentResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	if first.Answer != "There are 2 open tickets and 5 roast batches this week." {
		t.Fatalf("first answer = %+v", first.Response)
	}

	rec = postThreadTurn(t, hs, ctx, `{"instruction":"Which of Harbor Cafe's are open?","thread_id":"`+threadID+`"}`, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("second turn = %d: %s", rec.Code, rec.Body.String())
	}
	// The follow-up's lead call saw the first turn; no worker ever did.
	if !strings.Contains(model.requests[2], "2 open tickets and 5 roast batches") {
		t.Fatal("the lead's follow-up call is missing the thread history")
	}
	if len(worker.requests) != 3 {
		t.Fatalf("worker ran %d questions, want 3", len(worker.requests))
	}
	for _, req := range worker.requests {
		if len(req.History) != 0 || len(req.Memories) != 0 || req.Profile != nil {
			t.Fatalf("worker request carries conversation state: %+v", req)
		}
		if req.Capabilities == nil {
			t.Fatal("worker request lost the caller's capabilities")
		}
	}

	// Both questions' steps are in the run log, tagged, with unique ordered seq.
	events, err := svc.runEventsAfter(ctx, first.RunID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var lastSeq int64
	delegates := map[float64]int{}
	for _, e := range events {
		if e.Seq <= lastSeq {
			t.Fatalf("run event seq %d after %d", e.Seq, lastSeq)
		}
		lastSeq = e.Seq
		if e.Kind != "action" {
			continue
		}
		var payload map[string]any
		_ = json.Unmarshal(e.Payload, &payload)
		d, _ := payload["delegate"].(float64)
		if d == 0 {
			t.Fatalf("action event without a delegate: %s", e.Payload)
		}
		delegates[d]++
	}
	// Each question: lead start, worker step, lead finish.
	if len(delegates) != 2 || delegates[1] != 3 || delegates[2] != 3 {
		t.Fatalf("action events per question = %v", delegates)
	}
}
