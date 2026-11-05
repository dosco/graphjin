package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ax "github.com/ax-llm/ax/packages/go"
)

// scriptedLeadClient replays provider responses in order and records every
// request it was sent, so a test drives the real Ax tool loop.
type scriptedLeadClient struct {
	mu        sync.Mutex
	responses []ax.Value
	requests  []string
}

func (c *scriptedLeadClient) Chat(_ context.Context, req map[string]ax.Value, _ map[string]ax.Value) (ax.Value, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	raw, _ := json.Marshal(normalizeValue(req))
	c.requests = append(c.requests, string(raw))
	for len(c.responses) != 0 {
		steps, ok := c.responses[0].(leadSteps)
		if !ok {
			break
		}
		c.responses = append(append([]ax.Value(nil), steps...), c.responses[1:]...)
	}
	if len(c.responses) == 0 {
		return nil, errors.New("scripted client exhausted")
	}
	out := c.responses[0]
	c.responses = c.responses[1:]
	return out, nil
}

// leadSteps is several scripted model responses written as one entry.
type leadSteps []ax.Value

// leadCode is one actor turn of the lead's code runtime.
func leadCode(code string) ax.Value {
	content, _ := json.Marshal(map[string]string{"javascriptCode": code})
	return ax.Object("results", ax.Array(ax.Object("content", string(content), "function_calls", ax.Array())),
		"model_usage", ax.Object("tokens", ax.Object("promptTokens", 100, "completionTokens", 10)))
}

func (c *scriptedLeadClient) Embed(context.Context, map[string]ax.Value, map[string]ax.Value) (ax.Value, error) {
	return nil, nil
}

func (c *scriptedLeadClient) Stream(context.Context, map[string]ax.Value, map[string]ax.Value) ([]ax.Value, error) {
	return nil, nil
}

// leadToolCall scripts a lead turn that asks the data agent questions: the
// context stage hands off, the executor asks and logs, then finishes.
func leadToolCall(_ string, questions ...string) ax.Value {
	raw, _ := json.Marshal(questions)
	return leadSteps{
		leadCode(`final({ plan: "delegate" })`),
		leadCode(`const results = await ask_graphjin({ questions: ` + string(raw) + ` }); console.log(JSON.stringify(results))`),
		leadCode(`final("delegated")`),
	}
}

func leadReply(status, answer string) ax.Value {
	content, _ := json.Marshal(map[string]string{"status": status, "answer": answer})
	return ax.Object("results", ax.Array(ax.Object("content", string(content), "function_calls", ax.Array())),
		"model_usage", ax.Object("tokens", ax.Object("promptTokens", 120, "completionTokens", 30)))
}

func newTestLead(t *testing.T, cfg Config, client ax.AIClient, delegate DelegateFunc) *Lead {
	t.Helper()
	lead, err := NewLead(cfg, delegate, WithClientFactory(func(Config) (ax.AIClient, error) { return client, nil }))
	if err != nil {
		t.Fatal(err)
	}
	return lead
}

var leadTestAnswers = map[string]Response{
	"How many open tickets are there?":        {Status: StatusAnswered, Answer: "2 open tickets", Data: map[string]any{"customer_tickets": []any{map[string]any{"count_id": 2}}}},
	"How many roast batches ran this week?":   {Status: StatusAnswered, Answer: "5 batches", Data: map[string]any{"roast_batches": []any{map[string]any{"count_id": 5}}}},
	"Which customer ordered most this month?": {Status: StatusAnswered, Answer: "Northstar Grocers", Data: map[string]any{"orders": []any{map[string]any{"customer_name": "Northstar Grocers", "sum_total": 912}}}},
}

// Independent questions run at once, bounded by max_parallel, and come back
// in the order they were asked.
func TestLeadFansOutQuestionsInParallel(t *testing.T) {
	questions := []string{"How many open tickets are there?", "How many roast batches ran this week?", "Which customer ordered most this month?"}
	client := &scriptedLeadClient{responses: []ax.Value{
		leadToolCall("c1", questions...),
		leadReply(StatusAnswered, "There are 2 open tickets, 5 roast batches this week, and Northstar Grocers ordered most (sum_total 912)."),
	}}
	var inFlight, peak int32
	var got []DelegateRequest
	var mu sync.Mutex
	delegate := func(ctx context.Context, req DelegateRequest) (Response, error) {
		n := atomic.AddInt32(&inFlight, 1)
		for {
			p := atomic.LoadInt32(&peak)
			if n <= p || atomic.CompareAndSwapInt32(&peak, p, n) {
				break
			}
		}
		mu.Lock()
		got = append(got, req)
		mu.Unlock()
		req.Observer(ActionEvent{Index: 1, Source: "model", Tool: "execute_graphql", Status: "ok"})
		time.Sleep(40 * time.Millisecond)
		atomic.AddInt32(&inFlight, -1)
		resp := leadTestAnswers[req.Question]
		resp.Usage = map[string]any{"llm_calls": 2, "prompt_tokens": 50, "completion_tokens": 5, "total_tokens": 55}
		return resp, nil
	}
	var events []ActionEvent
	var evMu sync.Mutex
	cfg := Config{Lead: LeadConfig{MaxParallel: 2}}
	lead := newTestLead(t, cfg, client, delegate)

	resp, err := lead.Run(context.Background(), Request{
		Instruction: "Give me tickets, batches and the top customer",
		Observer:    func(e ActionEvent) { evMu.Lock(); events = append(events, e); evMu.Unlock() },
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != StatusAnswered || !strings.Contains(resp.Answer, "Northstar Grocers") {
		t.Fatalf("response = %+v", resp)
	}
	if peak != 2 {
		t.Fatalf("peak concurrent questions = %d, want max_parallel 2", peak)
	}
	delegations := mapValue(resp.Data)["delegations"].([]any)
	for i, q := range questions {
		if mapValue(delegations[i])["question"] != q {
			t.Fatalf("delegation %d = %v, want %q in asked order", i, delegations[i], q)
		}
	}
	// Every worker event is tagged with its question; the lead reports each
	// question starting and finishing.
	leadEvents := map[int][]string{}
	for _, e := range events {
		if e.Delegate == 0 {
			t.Fatalf("event %+v has no delegate", e)
		}
		if e.Source == "lead" {
			leadEvents[e.Delegate] = append(leadEvents[e.Delegate], e.Status)
		}
	}
	for i := 1; i <= 3; i++ {
		if strings.Join(leadEvents[i], ",") != "running,answered" {
			t.Fatalf("lead events for question %d = %v", i, leadEvents[i])
		}
	}
	usage := SummarizeUsage(resp.Usage)
	// The lead's four calls: hand-off, ask, finish, reply.
	if usage.LLMCalls != 4+3*2 || usage.PromptTokens != 420+3*50 {
		t.Fatalf("usage = %+v, want the lead's four calls plus every worker's", usage)
	}
	for _, req := range got {
		if req.ReadOnly {
			t.Fatal("a writable lead must not make its workers read-only")
		}
	}
}

// The conversation reaches the lead's prompt and never a worker.
func TestLeadKeepsConversationStateOutOfWorkers(t *testing.T) {
	client := &scriptedLeadClient{responses: []ax.Value{
		leadToolCall("c1", "How many open tickets are there?"),
		leadReply(StatusAnswered, "2 open tickets."),
	}}
	var asked []DelegateRequest
	delegate := func(_ context.Context, req DelegateRequest) (Response, error) {
		asked = append(asked, req)
		return leadTestAnswers[req.Question], nil
	}
	lead := newTestLead(t, Config{ReadOnly: true}, client, delegate)
	_, err := lead.Run(context.Background(), Request{
		Instruction: "and how many are open?",
		History:     []Turn{{Role: "user", Content: "Tell me about tickets at Harbor Cafe"}, {Role: "assistant", Content: "Harbor Cafe has 3 tickets."}},
		Memories:    []string{"Always answer in metric units"},
		Profile:     &ProfileInstructions{Name: "Support desk", Instructions: "Focus on customer support"},
	})
	if err != nil {
		t.Fatal(err)
	}
	prompt := client.requests[0]
	for _, want := range []string{"Harbor Cafe has 3 tickets", "Always answer in metric units", "Focus on customer support"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("lead prompt is missing %q", want)
		}
	}
	if len(asked) != 1 || !asked[0].ReadOnly {
		t.Fatalf("delegations = %+v, want one read-only question", asked)
	}
	raw, _ := json.Marshal(asked[0])
	for _, leaked := range []string{"Harbor Cafe", "metric units", "customer support"} {
		if strings.Contains(string(raw), leaked) {
			t.Fatalf("worker request carries conversation state %q: %s", leaked, raw)
		}
	}
}

// One failed question does not sink the turn.
func TestLeadAnswersAroundAFailedQuestion(t *testing.T) {
	client := &scriptedLeadClient{responses: []ax.Value{
		leadToolCall("c1", "How many open tickets are there?", "How many roast batches ran this week?"),
		leadReply(StatusAnswered, "There are 2 open tickets; the roast batch count could not be retrieved."),
	}}
	delegate := func(_ context.Context, req DelegateRequest) (Response, error) {
		if strings.Contains(req.Question, "roast") {
			return Response{}, errors.New("worker timed out")
		}
		return leadTestAnswers[req.Question], nil
	}
	lead := newTestLead(t, Config{}, client, delegate)
	resp, err := lead.Run(context.Background(), Request{Instruction: "tickets and batches?"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != StatusAnswered || !strings.Contains(resp.Answer, "2 open tickets") {
		t.Fatalf("response = %+v", resp)
	}
	// The model read the failure back as data.
	if !strings.Contains(strings.Join(client.requests, "\n"), "worker timed out") {
		t.Fatal("the failed question's error never reached the lead model")
	}
	delegations := mapValue(resp.Data)["delegations"].([]any)
	if mapValue(delegations[1])["status"] != StatusError {
		t.Fatalf("delegations = %v, want the second marked error", delegations)
	}
}

// An answer naming a field no worker returned gets one restatement, and is
// refused if the restatement still invents one.
func TestLeadGroundsItsAnswerInWorkerResults(t *testing.T) {
	ground := func(rewrite string) Response {
		client := &scriptedLeadClient{responses: []ax.Value{
			leadToolCall("c1", "How many open tickets are there?"),
			leadReply(StatusAnswered, "There are 2 open tickets and the escalation_rate is 40%."),
			ax.Object("results", ax.Array(ax.Object("content", fmt.Sprintf(`{"answer": %q}`, rewrite)))),
		}}
		lead := newTestLead(t, Config{}, client, func(_ context.Context, req DelegateRequest) (Response, error) {
			return leadTestAnswers[req.Question], nil
		})
		resp, err := lead.Run(context.Background(), Request{Instruction: "tickets?"})
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	if resp := ground("There are 2 open tickets."); resp.Status != StatusAnswered || resp.Answer != "There are 2 open tickets." {
		t.Fatalf("grounded rewrite = %+v, want the restated answer", resp)
	}
	resp := ground("The escalation_rate is still 40%.")
	if resp.Status != StatusBlocked || resp.Refusal == nil || resp.Refusal.Code != "ungrounded_answer_fields" {
		t.Fatalf("ungrounded rewrite = %+v, want an ungrounded_answer_fields refusal", resp)
	}
}

// The lead runs on the worker's model unless configured otherwise, and a
// different provider must bring its own key.
func TestLeadModelFallsBackToTheWorker(t *testing.T) {
	worker := Config{Provider: "google-gemini", Model: "gemini-3.5-flash", APIKeyEnv: "GOOGLE_API_KEY"}
	m := worker.LeadModelConfig()
	if m.Provider != "google-gemini" || m.Model != "gemini-3.5-flash" || m.APIKeyEnv != "GOOGLE_API_KEY" {
		t.Fatalf("unset lead = %+v, want the worker's model", m)
	}

	worker.Lead = LeadConfig{Model: "gemini-3.5-pro"}
	if m := worker.LeadModelConfig(); m.Model != "gemini-3.5-pro" || m.APIKeyEnv != "GOOGLE_API_KEY" {
		t.Fatalf("same-provider lead = %+v", m)
	}

	worker.Lead = LeadConfig{Provider: "anthropic", Model: "claude-opus-5-5"}
	if err := worker.ValidateLead(); err == nil || !strings.Contains(err.Error(), "api_key_env") {
		t.Fatalf("cross-provider lead without a key env: err = %v", err)
	}
	worker.Lead.APIKeyEnv = "ANTHROPIC_API_KEY"
	if err := worker.ValidateLead(); err != nil {
		t.Fatal(err)
	}
	if m := worker.LeadModelConfig(); m.Provider != "anthropic" || m.APIKeyEnv != "ANTHROPIC_API_KEY" || m.Model != "claude-opus-5-5" {
		t.Fatalf("cross-provider lead = %+v", m)
	}
}

// Questions past the turn's budget are not asked, and the model is told.
func TestLeadCapsQuestionsPerTurn(t *testing.T) {
	client := &scriptedLeadClient{responses: []ax.Value{
		leadToolCall("c1", "q1", "q2", "q3"),
		leadReply(StatusAnswered, "Done."),
	}}
	var asked int32
	lead := newTestLead(t, Config{Lead: LeadConfig{MaxQuestions: 2}}, client, func(context.Context, DelegateRequest) (Response, error) {
		atomic.AddInt32(&asked, 1)
		return Response{Status: StatusAnswered, Answer: "ok"}, nil
	})
	if _, err := lead.Run(context.Background(), Request{Instruction: "go"}); err != nil {
		t.Fatal(err)
	}
	if asked != 2 || !strings.Contains(strings.Join(client.requests, "\n"), "were not asked") {
		t.Fatalf("asked %d questions; the model never read the cap note", asked)
	}
}

// fakeLeadStore is the caller's skills and memories as the service would
// serve them through the graph.
type fakeLeadStore struct {
	mu         sync.Mutex
	skills     []SkillDoc
	memories   []MemoryDoc
	queries    []string
	remembered []string
}

func (s *fakeLeadStore) Skills(context.Context) ([]SkillDoc, error) { return s.skills, nil }

func (s *fakeLeadStore) RecallMemories(_ context.Context, query string, limit int) ([]MemoryDoc, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queries = append(s.queries, query)
	if len(s.memories) > limit {
		return s.memories[:limit], nil
	}
	return s.memories, nil
}

func (s *fakeLeadStore) Remember(_ context.Context, content string) (MemoryDoc, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.remembered = append(s.remembered, content)
	return MemoryDoc{ID: fmt.Sprintf("memory-%d", len(s.remembered)), Content: content}, nil
}

// The lead sees its caller's skill catalog, loads a skill by name, and can
// never load anything that is not in that catalog.
func TestLeadLoadsOnlyCatalogSkills(t *testing.T) {
	store := &fakeLeadStore{skills: []SkillDoc{{ID: "skill-churn", Name: "churn review", Description: "How we report churn", Content: "Report churn as a count and the MRR lost, last 30 days."}}}
	client := &scriptedLeadClient{responses: []ax.Value{
		leadCode(`final({ plan: "use the churn skill" })`),
		leadCode(`await discover({ skills: ["churn review", "someone else's skill"] }); console.log("loaded")`),
		leadToolCall("c1", "How many open tickets are there?"),
		leadReply(StatusAnswered, "2 open tickets."),
	}}
	lead := newTestLead(t, Config{}, client, func(_ context.Context, req DelegateRequest) (Response, error) {
		return leadTestAnswers[req.Question], nil
	})
	lead.Store = store
	resp, err := lead.Run(context.Background(), Request{Instruction: "do a churn review"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Skills) != 1 || resp.Skills[0].ID != "skill-churn" {
		t.Fatalf("response skills = %+v, want the loaded churn skill", resp.Skills)
	}
	all := strings.Join(client.requests, "\n")
	if !strings.Contains(client.requests[0], "How we report churn") {
		t.Fatal("the skill catalog never reached the lead")
	}
	if strings.Contains(client.requests[0], "MRR lost") {
		t.Fatal("skill content was in the prompt before the lead loaded it")
	}
	if !strings.Contains(all, "MRR lost, last 30 days") {
		t.Fatal("the loaded skill's content never reached the lead")
	}
}

// A turn starts with the memories recalled for the message; recall() asks the
// store again and never returns a memory already loaded.
func TestLeadRecallsMemoriesThroughTheStore(t *testing.T) {
	store := &fakeLeadStore{memories: []MemoryDoc{{ID: "mem-eur", Content: "Report amounts in EUR."}}}
	client := &scriptedLeadClient{responses: []ax.Value{
		leadCode(`await recall(["currency"]); console.log(JSON.stringify(inputs.memories))`),
		leadCode(`final({ plan: "delegate" })`),
		leadCode(`final("nothing to ask")`),
		leadReply(StatusAnswered, "Noted."),
	}}
	lead := newTestLead(t, Config{}, client, func(context.Context, DelegateRequest) (Response, error) { return Response{}, nil })
	lead.Store = store
	if _, err := lead.Run(context.Background(), Request{Instruction: "what is our MRR?"}); err != nil {
		t.Fatal(err)
	}
	// The turn starts from memories relevant to the message, topped up with
	// standing ones; recall() then searches for the model.
	if len(store.queries) < 3 || store.queries[0] != "what is our MRR?" || store.queries[1] != "" || store.queries[2] != "currency" {
		t.Fatalf("recall queries = %q, want the message, the standing memories, then the model's search", store.queries)
	}
	if !strings.Contains(client.requests[0], "Report amounts in EUR.") {
		t.Fatal("the seeded memory never reached the lead")
	}
	if got := strings.Count(client.requests[1], "Report amounts in EUR."); got > 2 {
		t.Fatalf("an already-loaded memory came back from recall: %d copies", got)
	}
}

// remember saves through the store on a writable turn and is not offered on a
// read-only one.
func TestLeadRemembersOnlyOnWritableTurns(t *testing.T) {
	run := func(readOnly bool) (*fakeLeadStore, *scriptedLeadClient) {
		store := &fakeLeadStore{}
		client := &scriptedLeadClient{responses: []ax.Value{
			leadCode(`final({ plan: "save the preference" })`),
			leadCode(`if (typeof remember === "function") { console.log(JSON.stringify(await remember({ content: "Report amounts in EUR." }))) } else { console.log("no remember") }`),
			leadCode(`if (typeof remember === "function") { console.log(JSON.stringify(await remember({ content: "Report amounts in EUR." }))) }`),
			leadCode(`final("done")`),
			leadReply(StatusAnswered, "Noted: amounts in EUR."),
		}}
		lead := newTestLead(t, Config{ReadOnly: readOnly}, client, func(context.Context, DelegateRequest) (Response, error) { return Response{}, nil })
		lead.Store = store
		if _, err := lead.Run(context.Background(), Request{Instruction: "remember I want amounts in EUR"}); err != nil {
			t.Fatal(err)
		}
		return store, client
	}
	store, _ := run(false)
	if len(store.remembered) != 1 || store.remembered[0] != "Report amounts in EUR." {
		t.Fatalf("remembered = %q", store.remembered)
	}
	store, client := run(true)
	if len(store.remembered) != 0 {
		t.Fatalf("a read-only turn saved %q", store.remembered)
	}
	if strings.Contains(client.requests[0], "Save one of the user's durable preferences") {
		t.Fatal("remember was offered on a read-only turn")
	}
}

// A question asked again in the same turn — the lead's code can run again in
// a later stage — is answered from the first result, not by a second worker.
func TestLeadAnswersARepeatedQuestionOnce(t *testing.T) {
	client := &scriptedLeadClient{responses: []ax.Value{
		leadCode(`final({ plan: "delegate" })`),
		leadCode(`const first = await ask_graphjin({ questions: ["How many open tickets are there?"] }); console.log(JSON.stringify(first))`),
		leadCode(`const again = await ask_graphjin({ questions: ["how many open  tickets are there?", "How many roast batches ran this week?"] }); console.log(JSON.stringify(again))`),
		leadCode(`final("done")`),
		leadReply(StatusAnswered, "There are 2 open tickets and 5 roast batches this week."),
	}}
	var asked []string
	var mu sync.Mutex
	lead := newTestLead(t, Config{}, client, func(_ context.Context, req DelegateRequest) (Response, error) {
		mu.Lock()
		asked = append(asked, req.Question)
		mu.Unlock()
		return leadTestAnswers[req.Question], nil
	})
	resp, err := lead.Run(context.Background(), Request{Instruction: "tickets and batches?"})
	if err != nil {
		t.Fatal(err)
	}
	if len(asked) != 2 {
		t.Fatalf("worker ran %d questions %q, want each question once", len(asked), asked)
	}
	if !strings.Contains(strings.Join(client.requests, "\n"), "already_asked") {
		t.Fatal("the model was not told the repeated question was already answered")
	}
	if resp.Status != StatusAnswered {
		t.Fatalf("response = %+v", resp)
	}
}

// The reply is written by Ax's responder stage, so the turn's saved memories
// must reach it too: a preference such as a currency format applies to the
// reply itself, not only to the code that gathers data.
func TestLeadReplyStageSeesSavedMemories(t *testing.T) {
	store := &fakeLeadStore{memories: []MemoryDoc{{ID: "mem-usd", Content: "State money in whole US dollars with no cents."}}}
	client := &scriptedLeadClient{responses: []ax.Value{
		leadToolCall("c1", "How many open tickets are there?"),
		leadReply(StatusAnswered, "2 open tickets."),
	}}
	lead := newTestLead(t, Config{}, client, func(_ context.Context, req DelegateRequest) (Response, error) {
		return leadTestAnswers[req.Question], nil
	})
	lead.Store = store
	if _, err := lead.Run(context.Background(), Request{Instruction: "tickets?"}); err != nil {
		t.Fatal(err)
	}
	reply := client.requests[len(client.requests)-1]
	if !strings.Contains(reply, "whole US dollars") {
		t.Fatal("the reply stage never saw the saved memories")
	}
}
