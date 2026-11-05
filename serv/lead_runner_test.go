package serv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/dosco/graphjin/core/v3"
	"net/http"
	"net/http/httptest"
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
	for len(c.responses) != 0 {
		steps, ok := c.responses[0].(leadSteps)
		if !ok {
			break
		}
		c.responses = append(append([]ax.Value(nil), steps...), c.responses[1:]...)
	}
	if len(c.responses) == 0 {
		return nil, errors.New("lead model script exhausted")
	}
	out := c.responses[0]
	c.responses = c.responses[1:]
	return out, nil
}

// leadSteps is several scripted lead-model responses written as one entry.
type leadSteps []ax.Value

func leadCode(code string) ax.Value {
	content, _ := json.Marshal(map[string]string{"javascriptCode": code})
	return ax.Object("results", ax.Array(ax.Object("content", string(content), "function_calls", ax.Array())),
		"model_usage", ax.Object("tokens", ax.Object("promptTokens", 100, "completionTokens", 10)))
}

func (c *leadModelClient) Embed(context.Context, map[string]ax.Value, map[string]ax.Value) (ax.Value, error) {
	return nil, nil
}

func (c *leadModelClient) Stream(context.Context, map[string]ax.Value, map[string]ax.Value) ([]ax.Value, error) {
	return nil, nil
}

// leadAsks scripts a lead turn that asks the data agent questions: the
// context stage hands off, the executor asks and logs, then finishes.
func leadAsks(questions ...string) ax.Value {
	raw, _ := json.Marshal(questions)
	return leadSteps{
		leadCode(`final({ plan: "delegate" })`),
		leadCode(`const results = await ask_graphjin({ questions: ` + string(raw) + ` }); console.log(JSON.stringify(results))`),
		leadCode(`final("delegated")`),
	}
}

func leadAnswers(answer string) ax.Value {
	content, _ := json.Marshal(map[string]string{"status": "answered", "answer": answer})
	return ax.Object("results", ax.Array(ax.Object("content", string(content), "function_calls", ax.Array())),
		"model_usage", ax.Object("tokens", ax.Object("promptTokens", 100, "completionTokens", 10)))
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
	// Each lead turn is four model calls; the follow-up starts at the fifth.
	if !strings.Contains(model.requests[4], "2 open tickets and 5 roast batches") {
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

	// The run row counts every model call the turn made, lead and workers.
	runs, err := newThreadControlPlane(svc).queryRows(ctx, core.ManagedQueryRoot{
		Table: threadRunsRootTable, Where: map[string]any{"id": map[string]any{"eq": first.RunID}},
	})
	if err != nil || len(runs) != 1 || runs[0]["llm_calls"].(int64) < 4 {
		t.Fatalf("gj_thread_run for the first turn = %+v err=%v, want llm_calls counted", runs, err)
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

// With the lead enabled every agent request runs it, a plain question
// included, and its workers never see the conversation. With it off the
// worker is exposed directly and a conversation is refused.
func TestAgentRequestsRunTheLeadWhenEnabled(t *testing.T) {
	worker := &statelessWorker{answers: map[string]string{
		"How many orders does Harbor Cafe have?": "Harbor Cafe has 3 orders",
		"How many customers are there?":          "There are 4 customers",
		"how many customers?":                    "There are 4 customers",
	}}
	prev := newGraphJinAgentRunner
	newGraphJinAgentRunner = func(*graphjinService, gjagent.Config, ...gjagent.Option) (graphjinAgentRunner, error) {
		return worker, nil
	}
	t.Cleanup(func() { newGraphJinAgentRunner = prev })
	post := func(hs *HttpService, body string, want int) map[string]any {
		t.Helper()
		rec := httptest.NewRecorder()
		hs.Agent(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, routeAgent, strings.NewReader(body)))
		if rec.Code != want {
			t.Fatalf("status = %d, want %d: %s", rec.Code, want, rec.Body.String())
		}
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return out
	}

	model := &leadModelClient{responses: []ax.Value{
		leadAsks("How many customers are there?"),
		leadAnswers("There are 4 customers."),
		leadAsks("How many orders does Harbor Cafe have?"),
		leadAnswers("Harbor Cafe has 3 orders."),
	}}
	on := newAgentHTTPTestService(&Config{Serv: Serv{Agent: AgentConfig{Enabled: true, Lead: gjagent.LeadConfig{Enabled: true}}}})
	on.Load().(*graphjinService).agentClientFactory = func(gjagent.Config) (ax.AIClient, error) { return model, nil }

	plain := post(on, `{"instruction":"how many customers?"}`, http.StatusOK)
	if plain["answer"] != "There are 4 customers." || len(model.requests) == 0 {
		t.Fatalf("plain question = %v, want the lead's answer", plain)
	}
	if worker.requests[0].Instruction != "How many customers are there?" {
		t.Fatalf("worker got %q, want the lead's question", worker.requests[0].Instruction)
	}

	followUp := post(on, `{"instruction":"how many orders do they have?","history":[{"role":"user","content":"Tell me about Harbor Cafe"},{"role":"assistant","content":"Harbor Cafe is a customer since 2024."}]}`, http.StatusOK)
	if followUp["answer"] != "Harbor Cafe has 3 orders." {
		t.Fatalf("follow-up = %v, want the lead's answer", followUp)
	}
	if !strings.Contains(strings.Join(model.requests, "\n"), "Harbor Cafe is a customer since 2024") {
		t.Fatal("the caller's history never reached the lead")
	}
	last := worker.requests[len(worker.requests)-1]
	if last.Instruction != "How many orders does Harbor Cafe have?" || len(last.History) != 0 {
		t.Fatalf("worker request = %+v, want the lead's self-contained question and no history", last)
	}

	off := newAgentHTTPTestService(&Config{Serv: Serv{Agent: AgentConfig{Enabled: true}}})
	seen := len(worker.requests)
	direct := post(off, `{"instruction":"how many customers?"}`, http.StatusOK)
	if direct["answer"] != "There are 4 customers" || worker.requests[seen].Instruction != "how many customers?" {
		t.Fatalf("lead off = %v, want the worker answering directly", direct)
	}
	refused := post(off, `{"instruction":"and orders?","history":[{"role":"user","content":"hi"}]}`, http.StatusBadRequest)
	if !strings.Contains(fmt.Sprint(refused), "agent.lead.enabled") {
		t.Fatalf("history with the lead off = %v, want the lead-required error", refused)
	}
}
