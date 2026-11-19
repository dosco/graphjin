package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
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

// leadToolCall scripts a conversation turn that asks the data agent questions:
// the lead asks and logs, then finishes. A turn with nothing attached has no
// first stage of its own (newLeadProgram), so no step is scripted for one.
func leadToolCall(_ string, questions ...string) ax.Value {
	raw, _ := json.Marshal(questions)
	return leadSteps{
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
	// The lead's three calls: ask, finish, reply.
	if usage.LLMCalls != 3+3*2 || usage.PromptTokens != 320+3*50 {
		t.Fatalf("usage = %+v, want the lead's three calls plus every worker's", usage)
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

// A blocked reply always carries a structured refusal. The lead holds no
// guards, so it reports "blocked" after a delegated question was refused or
// failed; the REST smoke saw status blocked with refusal null either way.
func TestLeadBlockedReplyCarriesARefusal(t *testing.T) {
	question := "Insert support ticket 999999."
	run := func(t *testing.T, worker Response, workerErr error) Response {
		t.Helper()
		client := &scriptedLeadClient{responses: []ax.Value{
			leadToolCall("c1", question),
			leadReply(StatusBlocked, "The insert could not be done."),
		}}
		lead := newTestLead(t, Config{}, client, func(context.Context, DelegateRequest) (Response, error) {
			return worker, workerErr
		})
		resp, err := lead.Run(context.Background(), Request{Instruction: "insert ticket 999999"})
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	t.Run("a guard's refusal travels up", func(t *testing.T) {
		guard := &Refusal{Code: "mutation_evidence_required", Retryable: true,
			Unblock: []UnblockStep{{Tool: toolQueryCatalog, Args: map[string]any{"id": "table:app:main.support_tickets"}}}}
		resp := run(t, Response{Status: StatusBlocked, Answer: "blocked", Refusal: guard}, nil)
		if resp.Status != StatusBlocked || resp.Refusal == nil || resp.Refusal.Code != "mutation_evidence_required" || len(resp.Refusal.Unblock) != 1 {
			t.Fatalf("resp = %+v, want the worker's refusal", resp)
		}
	})
	t.Run("a failure is an error, not a refusal", func(t *testing.T) {
		resp := run(t, Response{Status: StatusError, Errors: []ErrorInfo{{Message: "agent actor loop exceeded max steps"}}}, nil)
		if resp.Status != StatusError || resp.Refusal != nil || len(resp.Errors) != 1 || !strings.Contains(resp.Errors[0].Message, "max steps") {
			t.Fatalf("resp = %+v, want the worker's error", resp)
		}
		resp = run(t, Response{}, errors.New("worker timed out"))
		if resp.Status != StatusError || len(resp.Errors) != 1 || resp.Errors[0].Message != "worker timed out" {
			t.Fatalf("resp = %+v, want the delegate error", resp)
		}
	})
	t.Run("an unexplained block still names itself", func(t *testing.T) {
		resp := run(t, Response{Status: StatusAnswered, Answer: "done"}, nil)
		if resp.Status != StatusBlocked || resp.Refusal == nil || resp.Refusal.Code != "agent_blocked" || !resp.Refusal.Retryable ||
			len(resp.Refusal.Because) != 1 || resp.Refusal.Because[0] != "The insert could not be done." {
			t.Fatalf("resp = %+v, want an agent_blocked refusal", resp)
		}
	})
}

// A lead actor that asks the user a question ends its run with Ax's
// clarification error. That is a needs_clarification reply carrying the
// question; the lead used to report it as status error with the question as
// the error message.
func TestLeadClarificationIsAReplyNotAnError(t *testing.T) {
	client := &scriptedLeadClient{responses: []ax.Value{
		leadCode(`askClarification("Which name should the new admin role have?")`),
	}}
	lead := newTestLead(t, Config{}, client, func(context.Context, DelegateRequest) (Response, error) {
		t.Fatal("a clarification must not delegate")
		return Response{}, nil
	})
	resp, err := lead.Run(context.Background(), Request{Instruction: "Add a new admin role to the GraphJin config."})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != StatusNeedsClarification || len(resp.Errors) != 0 || !strings.Contains(resp.Answer, "Which name should the new admin role have?") {
		t.Fatalf("resp = %+v, want needs_clarification carrying the question", resp)
	}
}

// The lead learns what this caller may change from the same check that gates
// the worker's guides, so a non-admin asking for a config change is refused
// up front instead of being asked which role to add.
func TestLeadPermissionsFollowTheCallersAccess(t *testing.T) {
	admin := &CapabilityProfile{RoleClass: "admin", Authenticated: true,
		AvailableSystemRoots: []string{systemRootConfig, systemRootWatch},
		AllowedActions:       []string{systemRootConfig + ".update", systemRootWatch + ".insert", CapabilityActionDataInsert}}
	user := &CapabilityProfile{RoleClass: "user", Authenticated: true,
		AvailableSystemRoots: []string{systemRootWatch},
		AllowedActions:       []string{systemRootWatch + ".insert", CapabilityActionDataInsert}}

	got := leadPermissions(false, admin)
	if !strings.Contains(got, "May ask for:") || strings.Contains(got, "May not ask for: changing GraphJin configuration") ||
		!strings.Contains(strings.SplitN(got, "May not", 2)[0], "changing GraphJin configuration") {
		t.Fatalf("admin permissions = %q", got)
	}
	got = leadPermissions(false, user)
	if !strings.Contains(got, "May not ask for:") || !strings.Contains(strings.SplitN(got, "May not", 2)[1], "changing GraphJin configuration") ||
		!strings.Contains(strings.SplitN(got, "May not", 2)[0], "creating or changing watches") {
		t.Fatalf("user permissions = %q", got)
	}
	if got := leadPermissions(true, user); strings.Contains(got, "May ask for: changing application data") || strings.Contains(strings.SplitN(got, "May not", 2)[0], "creating or changing watches") {
		t.Fatalf("read-only permissions granted a write: %q", got)
	}
	if leadPermissions(false, nil) != "" {
		t.Fatal("an unknown caller must not be described")
	}

	client := &scriptedLeadClient{responses: []ax.Value{
		leadCode(`final("refuse")`),
		leadReply(StatusBlocked, "Only an administrator can change the GraphJin configuration."),
	}}
	lead := newTestLead(t, Config{}, client, func(context.Context, DelegateRequest) (Response, error) {
		t.Fatal("a refused change must not delegate")
		return Response{}, nil
	})
	resp, err := lead.Run(context.Background(), Request{Instruction: "Add a new admin role to the GraphJin config.", Capabilities: user})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(client.requests, "\n"), "May not ask for:") {
		t.Fatal("the permissions summary never reached the lead model")
	}
	if resp.Status != StatusBlocked || resp.Refusal == nil {
		t.Fatalf("resp = %+v, want a blocked reply with a refusal", resp)
	}
}

// With OnAnswer set the lead streams its reply stage; the streamed text adds
// up to the reply and the response is the same as without streaming.
func TestLeadStreamsItsAnswer(t *testing.T) {
	run := func(stream bool) (Response, string) {
		client := &scriptedLeadClient{responses: []ax.Value{
			leadToolCall("c1", "How many open tickets are there?"),
			leadReply(StatusAnswered, "There are 2 open tickets."),
		}}
		lead := newTestLead(t, Config{}, client, func(_ context.Context, req DelegateRequest) (Response, error) {
			return leadTestAnswers[req.Question], nil
		})
		var streamed strings.Builder
		req := Request{Instruction: "open tickets?"}
		if stream {
			req.OnAnswer = func(text string, reset bool) {
				if reset {
					streamed.Reset()
				}
				streamed.WriteString(text)
			}
		}
		resp, err := lead.Run(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		return resp, streamed.String()
	}
	plain, _ := run(false)
	streamedResp, streamed := run(true)
	if streamed != "There are 2 open tickets." {
		t.Fatalf("streamed %q", streamed)
	}
	if streamedResp.Status != plain.Status || streamedResp.Answer != plain.Answer {
		t.Fatalf("streamed response %+v differs from plain %+v", streamedResp, plain)
	}
}

// A conversation turn gives a first stage nothing to read but the message, so
// the lead goes straight to the stage that asks: one model call fewer. A turn
// with data attached, such as a routine's watch event, keeps the first stage,
// which reads what is attached and hands on what it found.
func TestLeadSkipsTheFirstStageOnlyWhenNothingIsAttached(t *testing.T) {
	question := "How many open tickets are there?"
	delegate := func(_ context.Context, req DelegateRequest) (Response, error) {
		return leadTestAnswers[req.Question], nil
	}
	asked := func(resp Response) int {
		delegations, _ := mapValue(resp.Data)["delegations"].([]any)
		return len(delegations)
	}

	client := &scriptedLeadClient{responses: []ax.Value{
		leadToolCall("", question),
		leadReply(StatusAnswered, "2 open tickets."),
	}}
	resp, err := newTestLead(t, Config{}, client, delegate).Run(context.Background(), Request{
		Instruction: "and how many are open?",
		History:     []Turn{{Role: "user", Content: "Tell me about tickets"}, {Role: "assistant", Content: "There are 3 tickets."}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Ask, finish, reply.
	if len(client.requests) != 3 || len(client.responses) != 0 || asked(resp) != 1 || resp.Answer != "2 open tickets." {
		t.Fatalf("conversation turn: %d model calls, %d scripted replies unused, %d questions, answer %q", len(client.requests), len(client.responses), asked(resp), resp.Answer)
	}
	if usage := SummarizeUsage(resp.Usage); usage.LLMCalls != 3 {
		t.Fatalf("conversation turn reports %d model calls, want the 3 that were made", usage.LLMCalls)
	}

	client = &scriptedLeadClient{responses: []ax.Value{
		leadSteps{
			// First stage: reads the attached event and hands on what it found.
			leadCode(`final("check the ticket", { ticketId: inputs.attached._watch_events[0].data_json.id })`),
			// Second stage: what was handed on is there to read.
			leadCode(`console.log("handed=" + inputs.distilledContext.ticketId); const r = await ask_graphjin({ questions: ["` + question + `"] }); console.log(JSON.stringify(r))`),
			leadCode(`final("done")`),
		},
		leadReply(StatusAnswered, "2 open tickets."),
	}}
	resp, err = newTestLead(t, Config{}, client, delegate).Run(context.Background(), Request{
		Instruction: "Check this ticket.",
		Context:     map[string]any{"_watch_events": []any{map[string]any{"id": "we1", "data_json": map[string]any{"id": 13}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Hand on, ask, finish, reply.
	if len(client.requests) != 4 || len(client.responses) != 0 || asked(resp) != 1 {
		t.Fatalf("attached turn: %d model calls, %d scripted replies unused, %d questions", len(client.requests), len(client.responses), asked(resp))
	}
	if !strings.Contains(client.requests[2], "handed=13") {
		t.Fatal("the second stage never read what the first handed on")
	}
}

// A step that calls a function for its effect and prints nothing used to come
// back as "(no output)", and the model, unable to tell the call had run, ran
// the same step again until it was out of steps. What ask_graphjin returned is
// shown on the next model call whether or not the step printed anything
// (lead_answers.go); a function that only confirms is noted by GraphJin
// (TestLeadCodeGoesOnAfterAFunctionThatOnlyConfirms). A step that called
// nothing still reads "(no output)".
func TestLeadStepThatPrintsNothingShowsWhatItsCallReturned(t *testing.T) {
	change := "Set support ticket 21's status to resolved."
	lookup := "How many open tickets are there?"
	client := &scriptedLeadClient{responses: []ax.Value{
		leadSteps{
			// First stage: called for its effect, nothing printed.
			leadCode(`await ask_graphjin({ questions: ["` + change + `"] });`),
			leadCode(`final("report the change", {})`),
			// Second stage: a step that prints after it asks.
			leadCode(`const r = await ask_graphjin({ questions: ["` + lookup + `"] }); console.log("printed=" + r[0].status)`),
			// No call and nothing printed.
			leadCode(`let quiet = 1;`),
			leadCode(`final("done")`),
		},
		leadReply(StatusAnswered, "The change waits for approval."),
	}}
	lead := newTestLead(t, Config{}, client, func(_ context.Context, d DelegateRequest) (Response, error) {
		if d.Question == change {
			return Response{Status: StatusNeedsApproval, Answer: "Ticket change held for the owner.", Approval: &Approval{ID: "apr_1", Summary: "update 1 row in support_tickets"}}, nil
		}
		return Response{Status: StatusAnswered, Answer: "Zeta answer."}, nil
	})
	_, err := lead.Run(context.Background(), Request{
		Instruction: "A support ticket changed.",
		Context:     map[string]any{"_watch_events": []any{map[string]any{"id": "we1"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 6 || len(client.responses) != 0 {
		t.Fatalf("%d model calls, %d scripted replies unused", len(client.requests), len(client.responses))
	}
	after := client.requests[1]
	for _, want := range []string{StatusNeedsApproval, "Ticket change held for the owner."} {
		if !strings.Contains(after, want) {
			t.Fatalf("the step after an unprinted call does not show %q", want)
		}
	}
	if strings.Contains(after, "(no output)") {
		t.Fatal("a step that asked and printed nothing reads (no output)")
	}
	// Plain code after the question still runs; another function call or
	// final() would not.
	if printed := client.requests[3]; !strings.Contains(printed, "printed="+StatusAnswered) || !strings.Contains(printed, "Zeta answer.") {
		t.Fatal("a step that asked and then printed was not shown both what it printed and the answer")
	}
	if !strings.Contains(client.requests[4], "(no output)") {
		t.Fatal("a step that called nothing and printed nothing no longer reads (no output)")
	}
}

// A returned value too long for a step result is shown without its row data,
// which the code can still read from the call's result.
func TestReturnedTextLeavesOutRowDataWhenLong(t *testing.T) {
	rows := make([]any, 0, 400)
	for i := 0; i < 400; i++ {
		rows = append(rows, map[string]any{"id": i, "subject": "a ticket subject"})
	}
	value := []any{map[string]any{"question": "q", "status": StatusAnswered, "answer": "400 tickets.", "data": map[string]any{"support_tickets": rows}}}
	text := returnedText(value)
	if len(text) > returnedTextLimit || !strings.Contains(text, "400 tickets.") || !strings.Contains(text, StatusAnswered) || strings.Contains(text, "a ticket subject") {
		t.Fatalf("long value shown as %d chars: %.200s", len(text), text)
	}
	if short := returnedText([]any{map[string]any{"status": StatusAnswered, "data": map[string]any{"n": 4}}}); !strings.Contains(short, `"n":4`) {
		t.Fatalf("a short value lost its data: %s", short)
	}
	var calls unprintedCalls
	calls.record("ask_graphjin", value)
	failed := map[string]ax.Value{"kind": "error", "is_error": true, "error": "boom"}
	showReturned(failed, calls.take())
	if _, shown := failed["logs"]; shown {
		t.Fatal("a failed step was given the returned value as output")
	}
	if len(calls.take()) != 0 {
		t.Fatal("take left calls behind for the next step")
	}
}

// The lead's prompt shows each input under its label and lists no context
// fields; it must also say where an input lives in code, or a model reading
// the attached watch event guesses a path and fails every step.
func TestLeadPromptNamesWhereInputsLiveInCode(t *testing.T) {
	client := &scriptedLeadClient{responses: []ax.Value{
		leadSteps{
			leadCode(`console.log("subject=" + inputs.attached._watch_events[0].data_json.support_tickets[0].subject); console.log("turns=" + inputs.history.length); console.log("llmQuery=" + typeof llmQuery)`),
			leadCode(`respond("say what the subject is", { subject: inputs.attached._watch_events[0].data_json.support_tickets[0].subject })`),
		},
		leadReply(StatusAnswered, "The subject is DUPLICATE: x."),
	}}
	lead := newTestLead(t, Config{}, client, func(context.Context, DelegateRequest) (Response, error) {
		return Response{Status: StatusAnswered}, nil
	})
	_, err := lead.Run(context.Background(), Request{
		Instruction: "A support ticket changed.",
		History:     []Turn{{Role: "user", Content: "earlier"}, {Role: "assistant", Content: "reply"}},
		Context: map[string]any{"_watch_events": []any{map[string]any{"data_json": map[string]any{
			"support_tickets": []any{map[string]any{"id": 13, "subject": "DUPLICATE: x"}},
		}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	first := client.requests[0]
	for _, want := range []string{"inputs.attached", "inputs.history"} {
		if !strings.Contains(first, want) {
			t.Fatalf("the lead's prompt never names %s", want)
		}
	}
	second := client.requests[1]
	for _, want := range []string{"subject=DUPLICATE: x", "turns=2"} {
		if !strings.Contains(second, want) {
			t.Fatalf("code reading the inputs the way the prompt says got no %q:\n%s", want, second[max(0, len(second)-600):])
		}
	}
	// Ax registers llmQuery through a hook the runtime wrappers must keep.
	if !strings.Contains(second, "llmQuery=function") {
		t.Fatal("the prompt advertises llmQuery and the lead's runtime does not define it")
	}
}

// Lead code in the second stage that reads a value under inputs when it is
// not an input gets the value it meant: a variable the first stage created,
// or evidence it handed on. Behind optional chaining the wrong name used to
// read as undefined without an error, and the turn decided on an empty
// subject that a duplicate ticket needed no change.
func TestLeadCodeReadsAMisnamedInputAsTheValueItMeant(t *testing.T) {
	client := &scriptedLeadClient{responses: []ax.Value{
		leadSteps{
			// First stage. A key of the attached input read as an input: the
			// host cannot set inputs here, so the failed step is told where
			// the value is.
			leadCode(`console.log(inputs._watch_events[0].id)`),
			leadCode(`const ticket = inputs.attached._watch_events[0].data_json.support_tickets[0]; final("say whether it is a duplicate", { ticketId: ticket.id })`),
			// Second stage. The first stage's variable and its evidence, both
			// read as inputs, the variable behind optional chaining.
			leadCode(`const subject = inputs.ticket?.subject ?? ""; console.log("subject=" + subject + " id=" + inputs.ticketId)`),
			// A variable of this stage, read as an input in later steps, is
			// read where it lives each time: it follows a change. The real
			// inputs are still there beside it.
			leadCode(`note = "first"; console.log("set")`),
			leadCode(`console.log("a=" + inputs.note + " kept=" + inputs.distilledContext.ticketId + " real=" + inputs.attached._watch_events[0].id); note = "second"`),
			leadCode(`console.log("b=" + inputs.note)`),
			leadCode(`final("say it is a duplicate", { subject: ticket.subject })`),
		},
		leadReply(StatusAnswered, "Ticket 13 is a duplicate."),
	}}
	lead := newTestLead(t, Config{}, client, func(context.Context, DelegateRequest) (Response, error) {
		return Response{Status: StatusAnswered}, nil
	})
	if _, err := lead.Run(context.Background(), Request{
		Instruction: "A support ticket changed.",
		Context: map[string]any{"_watch_events": []any{map[string]any{"id": "we1", "data_json": map[string]any{
			"support_tickets": []any{map[string]any{"id": 13, "subject": "DUPLICATE: x"}},
		}}}},
	}); err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 8 {
		t.Fatalf("model calls = %d, want the seven steps and the reply", len(client.requests))
	}
	// Each step's result is what the next model call reads.
	for call, want := range map[int][]string{
		1: {"inputs._watch_events does not exist", "read inputs.attached._watch_events"},
		3: {"subject=DUPLICATE: x id=13"},
		5: {"a=first kept=13 real=we1"},
		6: {"b=second"},
	} {
		for _, text := range want {
			if !strings.Contains(client.requests[call], text) {
				t.Errorf("model call %d did not read %q", call+1, text)
			}
		}
	}
	if strings.Contains(client.requests[3], "does not exist") {
		t.Error("a name that resolved still drew a hint")
	}
}

// What the runtime places, and what it leaves alone: a name nothing holds is
// never guessed, and a declared input this turn does not carry stays absent
// even when a variable shares its name.
func TestLeadPlacesOnlyNamesItCanFind(t *testing.T) {
	session := &leadNamesSession{
		CodeSession: variablesSession{vars: map[string]any{"ticket": map[string]any{"id": 7}, "history": []any{"mine"}, "ticketId": 99}},
		inputs: map[string]any{
			"instruction":      "x",
			"attached":         map[string]any{"k": 1},
			"distilledContext": map[string]any{"ticketId": 13},
		},
	}
	placed := session.place(`inputs.nowhere.x; inputs.attached.k; inputs.history; inputs?.ticket?.id; inputs.ticketId; inputs.k; inputs.instruction`)
	got := map[string]string{}
	for _, name := range placed {
		got[name.name] = name.where
	}
	want := map[string]string{
		"ticket":   "it as ticket",
		"ticketId": "inputs.distilledContext.ticketId", // handed-on evidence before a variable of the same name
		"k":        "inputs.attached.k",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("placed = %v, want %v", got, want)
	}
	for _, declared := range []string{"instruction", "history", "saved_memories", "saved_skills", "profile", "attached", "current_date", "read_only", "permissions"} {
		if !leadDeclaredInputs[declared] {
			t.Errorf("%s is a declared lead input and is not treated as one", declared)
		}
	}
}

// variablesSession is a code session that holds only live variables.
type variablesSession struct {
	ax.CodeSession
	vars map[string]any
}

func (s variablesSession) Inspect(map[string]ax.Value) ax.Value { return s.vars }

// unattendedReply is the reply of a turn nobody is watching: it says whether
// the turn calls for a change, and what the change is.
func unattendedReply(answer string, proposes bool, change string) ax.Value {
	fields := map[string]any{"status": StatusAnswered, "answer": answer, "proposes_change": proposes}
	if change != "" {
		fields["change_request"] = change
	}
	content, _ := json.Marshal(fields)
	return ax.Object("results", ax.Array(ax.Object("content", string(content), "function_calls", ax.Array())),
		"model_usage", ax.Object("tokens", ax.Object("promptTokens", 120, "completionTokens", 30)))
}

// routineLookup scripts the code of a routine turn the way a model writes it:
// the first stage hands the event on, and the second asks the data agent and
// ends in the same step, on a conclusion it reached without reading the
// answer. Asking ends that step, so the ending is held and written again by
// the model call that was shown the answer.
func routineLookup(conclusion string, questions ...string) ax.Value {
	raw, _ := json.Marshal(questions)
	said, _ := json.Marshal(conclusion)
	return leadSteps{
		leadCode(`final("check the ticket", {})`),
		leadCode(`const r = await ask_graphjin({ questions: ` + string(raw) + ` }); await final(` + string(said) + `, { conditionMet: false })`),
		leadCode(`await final(` + string(said) + `, { conditionMet: false })`),
	}
}

var routineRequest = Request{
	Instruction: "Flag ticket 25 if its account is at churn risk and has a failed invoice.",
	Context:     map[string]any{"_watch_events": []any{map[string]any{"id": "we1", "data_json": map[string]any{"id": 25}}}},
}

var unattendedConfig = Config{HeldWritesOnly: true, WriteApproval: WriteApprovalRequired}

// A routine's lead looks something up and ends its code on a wrong
// conclusion, as a model that decides in code before reading the answer does.
// The reply stage reads what the data agent answered, states the change, and
// GraphJin sends it: the change is held although no code asked for it.
func TestUnattendedReplyThatStatesAChangeHasItSent(t *testing.T) {
	lookup := "Is the account of support ticket 25 at churn risk with a failed invoice?"
	change := "Set support ticket 25's resolution_note to priority-review"
	var mu sync.Mutex
	var asked []string
	readOnly := map[string]bool{}
	delegate := func(_ context.Context, d DelegateRequest) (Response, error) {
		mu.Lock()
		asked = append(asked, d.Question)
		readOnly[d.Question] = d.ReadOnly
		mu.Unlock()
		if d.Question == change {
			if d.ReadOnly {
				return Response{Status: StatusBlocked, Answer: "read-only"}, nil
			}
			return Response{Status: StatusNeedsApproval, Answer: "held", Approval: &Approval{ID: "apr_1", Summary: "update 1 row in support_tickets"}}, nil
		}
		return Response{Status: StatusAnswered, Answer: "Yes: churn risk, 2 failed invoices."}, nil
	}

	client := &scriptedLeadClient{responses: []ax.Value{
		routineLookup("No change is needed.", lookup),
		unattendedReply("Ticket 25 is proposed for priority review: its account is at churn risk and has 2 failed invoices.", true, change),
	}}
	resp, err := newTestLead(t, unattendedConfig, client, delegate).Run(context.Background(), routineRequest)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(asked, "|") != lookup+"|"+change {
		t.Fatalf("data agent was asked %q, want the look-up and then the stated change", asked)
	}
	if resp.Status != StatusNeedsApproval || resp.Approval == nil || resp.Approval.ID != "apr_1" {
		t.Fatalf("reply = %s %+v, want the stated change held", resp.Status, resp.Approval)
	}
	if readOnly[lookup] || readOnly[change] {
		t.Fatalf("read-only: look-up %v, stated change %v; want neither", readOnly[lookup], readOnly[change])
	}
	for _, notice := range resp.Notices {
		if notice.Kind == noticeStatedChangeNotHeld {
			t.Fatalf("a held change was reported as not held: %+v", notice)
		}
	}
	// Hand on, ask, end, reply: no second attempt.
	if len(client.requests) != 4 || len(client.responses) != 0 {
		t.Fatalf("%d model calls, %d scripted replies unused", len(client.requests), len(client.responses))
	}
	// The code ended on its own conclusion. The reply stage was given the task
	// of deciding and the data agent's answer, and none of what the code
	// concluded.
	reply := client.requests[len(client.requests)-1]
	if !strings.Contains(reply, replyAnswersKey) || !strings.Contains(reply, "Yes: churn risk, 2 failed invoices.") {
		t.Fatal("the reply stage was not handed what the data agent answered")
	}
	if !strings.Contains(reply, "Decide from them whether the instruction calls for a change") {
		t.Fatal("the reply stage was not given the task of deciding")
	}
	if strings.Contains(reply, "conditionMet") || strings.Contains(reply, "No change is needed.") {
		t.Fatal("the reply stage was handed the conclusion the code reached without reading")
	}

	made := routineRequest
	made.ChangeAlreadyMade = true
	for name, tc := range map[string]struct {
		cfg   Config
		req   Request
		reply ax.Value
	}{
		"no change called for":       {unattendedConfig, routineRequest, unattendedReply("No change is needed.", false, "")},
		"a change named but not due": {unattendedConfig, routineRequest, unattendedReply("No change is needed.", false, change)},
		"due with nothing to send":   {unattendedConfig, routineRequest, unattendedReply("A change is needed.", true, "")},
		"a watched turn":             {Config{}, routineRequest, unattendedReply("Ticket 25 is proposed for priority review.", true, change)},
		"a change already made":      {unattendedConfig, made, unattendedReply("Ticket 25 was updated as approved.", true, change)},
	} {
		asked = nil
		client := &scriptedLeadClient{responses: []ax.Value{routineLookup("No change is needed.", lookup), tc.reply}}
		resp, err := newTestLead(t, tc.cfg, client, delegate).Run(context.Background(), tc.req)
		if err != nil {
			t.Fatal(err)
		}
		if len(asked) != 1 || resp.Approval != nil {
			t.Fatalf("%s: asked %q, approval %+v; want only the look-up", name, asked, resp.Approval)
		}
		if strings.Contains(client.requests[len(client.requests)-1], replyAnswersKey) != (name != "a watched turn" && name != "a change already made") {
			t.Fatalf("%s: the reply stage is handed the answers only on a turn whose reply decides a change", name)
		}
	}
}

// Every stage of a lead turn is told the lead's instructions: the stages that
// write its code, which decide what the data agent is asked and in what
// words, and the stage that writes the reply. Ax hands a signature's
// instructions to the reply stage only, so the code stages get them with the
// runtime's (leadCodeInstructions).
func TestEveryLeadStageIsToldTheLeadsInstructions(t *testing.T) {
	delegate := func(_ context.Context, d DelegateRequest) (Response, error) {
		return Response{Status: StatusAnswered, Answer: "2 open tickets."}, nil
	}
	const forEveryTurn = "The data agent sees no conversation, so every question must stand alone"
	const forAConversation = "only after the user asked for or confirmed that change"
	const forAnUnattendedTurn = "Only code can ask the data agent"
	for name, tc := range map[string]struct {
		cfg      Config
		req      Request
		script   ax.Value
		reply    ax.Value
		has, not string
	}{
		"a conversation": {Config{}, Request{Instruction: "How many tickets are open?"}, leadToolCall("", "How many support tickets are open?"),
			leadReply(StatusAnswered, "2 open tickets."), forAConversation, forAnUnattendedTurn},
		"a routine": {unattendedConfig, routineRequest, routineLookup("No change is needed.", "Is the account of support ticket 25 at churn risk?"),
			unattendedReply("No change is needed.", false, ""), forAnUnattendedTurn, forAConversation},
	} {
		client := &scriptedLeadClient{responses: []ax.Value{tc.script, tc.reply}}
		if _, err := newTestLead(t, tc.cfg, client, delegate).Run(context.Background(), tc.req); err != nil {
			t.Fatal(err)
		}
		if len(client.requests) < 3 {
			t.Fatalf("%s: %d model calls, want code stages and a reply", name, len(client.requests))
		}
		if len(client.responses) != 0 {
			t.Fatalf("%s: %d scripted replies unused", name, len(client.responses))
		}
		for i, request := range client.requests {
			if !strings.Contains(request, forEveryTurn) || !strings.Contains(request, tc.has) || strings.Contains(request, tc.not) {
				t.Fatalf("%s: model call %d of %d was not told the lead's instructions for this kind of turn", name, i+1, len(client.requests))
			}
			// How an answer is read is for the stages that write code, and only
			// for them.
			code := i < len(client.requests)-1
			if code != strings.Contains(request, "ask_graphjin ends the code block it is called in") {
				t.Fatalf("%s: model call %d: how an answer is read must be told to the stages that write code, and only to them", name, i+1)
			}
		}
	}
}

// The two kinds of turn differ in how a change comes about, and in nothing
// else of what the lead is told.
func TestUnattendedSignatureCarriesItsOwnChangeRules(t *testing.T) {
	quoted := func(text string) string { return strings.ReplaceAll(text, `"`, `'`) }
	if !strings.Contains(leadSignature, quoted(leadChangeRules)) || strings.Contains(leadSignature, quoted(leadUnattendedChangeRules)) {
		t.Fatal("a conversation's signature must carry the conversation's rules for changes")
	}
	if !strings.Contains(leadUnattendedSignature, quoted(leadUnattendedChangeRules)) || strings.Contains(leadUnattendedSignature, quoted(leadChangeRules)) {
		t.Fatal("an unattended signature must carry the unattended rules for changes")
	}
	for _, field := range []string{"proposes_change:boolean", "change_request?:string"} {
		if !strings.Contains(leadUnattendedSignature, field) || strings.Contains(leadSignature, field) {
			t.Fatalf("%s belongs to the unattended reply only", field)
		}
	}
	rest := func(signature, rules string) string { return strings.Replace(signature, quoted(rules), "", 1) }
	unattended, _, _ := strings.Cut(rest(leadUnattendedSignature, leadUnattendedChangeRules), ", proposes_change:boolean")
	if unattended != rest(leadSignature, leadChangeRules) {
		t.Fatal("the two signatures differ outside their rules for changes and the unattended reply's fields")
	}
}

// A stated change the data agent does not hold is said so in the reply, in
// the data agent's words: the reply was written before the change was sent.
func TestUnattendedReplyWhoseStatedChangeIsNotHeldSaysSo(t *testing.T) {
	lookup := "Is the account of support ticket 25 at churn risk with a failed invoice?"
	change := "Set support ticket 25's resolution_notes to priority-review"
	delegate := func(_ context.Context, d DelegateRequest) (Response, error) {
		if d.Question == change {
			return Response{Status: StatusAnswered, Answer: "support_tickets has no such field; nothing was changed."}, nil
		}
		return Response{Status: StatusAnswered, Answer: "Yes: churn risk, 2 failed invoices."}, nil
	}
	client := &scriptedLeadClient{responses: []ax.Value{
		routineLookup("No change is needed.", lookup),
		unattendedReply("Ticket 25 is proposed for priority review.", true, change),
	}}
	resp, err := newTestLead(t, unattendedConfig, client, delegate).Run(context.Background(), routineRequest)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Approval != nil || resp.Status == StatusNeedsApproval {
		t.Fatalf("reply = %s %+v, want nothing held", resp.Status, resp.Approval)
	}
	if !strings.Contains(resp.Answer, "is not held for approval") || !strings.Contains(resp.Answer, "support_tickets has no such field") {
		t.Fatalf("answer = %q, want it to say the change is not held and why", resp.Answer)
	}
	var noted bool
	for _, notice := range resp.Notices {
		noted = noted || notice.Kind == noticeStatedChangeNotHeld
	}
	if !noted {
		t.Fatalf("notices = %+v, want the unheld change recorded", resp.Notices)
	}
}

// A reply carries one change. When the turn's code asked for one and it is
// held, a change the reply states is not sent as well.
func TestUnattendedReplyDoesNotSendASecondChange(t *testing.T) {
	change := "Set support ticket 25's resolution_note to priority-review"
	var asked []string
	delegate := func(_ context.Context, d DelegateRequest) (Response, error) {
		asked = append(asked, d.Question)
		return Response{Status: StatusNeedsApproval, Answer: "held", Approval: &Approval{ID: "apr_1", Summary: "update 1 row in support_tickets"}}, nil
	}
	for name, stated := range map[string]string{"the same change": change, "another change": "Set support ticket 26's status to resolved"} {
		asked = nil
		client := &scriptedLeadClient{responses: []ax.Value{
			routineLookup("Proposed.", change),
			unattendedReply("Ticket 25 is proposed for priority review.", true, stated),
		}}
		resp, err := newTestLead(t, unattendedConfig, client, delegate).Run(context.Background(), routineRequest)
		if err != nil {
			t.Fatal(err)
		}
		if len(asked) != 1 || resp.Status != StatusNeedsApproval {
			t.Fatalf("%s: asked %q, status %s; want the one change the code asked for", name, asked, resp.Status)
		}
	}
}

// A change the turn's code already asked for, in the same words, that the
// data agent did not hold is not asked for a second time: the reply says it
// is not held, with what the data agent said.
func TestUnattendedReplyDoesNotRepeatARequestTheCodeMade(t *testing.T) {
	change := "Set support ticket 25's resolution_note to priority-review"
	var asked []string
	delegate := func(_ context.Context, d DelegateRequest) (Response, error) {
		asked = append(asked, d.Question)
		return Response{Status: StatusAnswered, Answer: "Ticket 25 is already flagged; nothing was changed."}, nil
	}
	client := &scriptedLeadClient{responses: []ax.Value{
		routineLookup("Proposed.", change),
		unattendedReply("Ticket 25 is proposed for priority review.", true, change),
	}}
	resp, err := newTestLead(t, unattendedConfig, client, delegate).Run(context.Background(), routineRequest)
	if err != nil {
		t.Fatal(err)
	}
	if len(asked) != 1 || resp.Approval != nil {
		t.Fatalf("asked %q, approval %+v; want the request made once and nothing held", asked, resp.Approval)
	}
	if !strings.Contains(resp.Answer, "is not held for approval") || !strings.Contains(resp.Answer, "already flagged") {
		t.Fatalf("answer = %q, want it to say the change is not held and why", resp.Answer)
	}
}

// A routine whose change is worded by one of the owner's skills loads the
// skill in its code. Ax gives loaded skill text to the code stages only, so
// the reply that states the change gets it from the turn's record: without it
// the change read "close it per the playbook" and the note was the model's.
func TestUnattendedReplyReadsTheSkillsTheCodeLoaded(t *testing.T) {
	store := &fakeLeadStore{skills: []SkillDoc{{ID: "skill-dup", Name: "duplicate playbook", Description: "How we close duplicate tickets", Content: "Close a duplicate with status resolved and the note merged-into-original."}}}
	for name, code := range map[string]ax.Value{
		// The skill is loaded in the step written to end the first stage, the
		// way a model writes it: loading ends that step, so the ending is
		// written again by the model call that was shown the skill. And in a
		// step of its own.
		"loaded in the step written to end the stage": leadSteps{
			leadCode(`await discover({ skills: ["duplicate playbook"] }); await final("close the duplicate ticket per the playbook", {})`),
			leadCode(`await final("close the duplicate ticket per the playbook", {})`),
			leadCode(`await final("Propose closing ticket 25 using the duplicate playbook.", {})`),
		},
		"loaded in a step of its own": leadSteps{
			leadCode(`await discover({ skills: ["duplicate playbook"] }); console.log("loaded")`),
			leadCode(`await final("close the duplicate ticket per the playbook", {})`),
			leadCode(`await final("Propose closing ticket 25 using the duplicate playbook.", {})`),
		},
	} {
		client := &scriptedLeadClient{responses: []ax.Value{code, unattendedReply("No change is needed.", false, "")}}
		lead := newTestLead(t, unattendedConfig, client, func(context.Context, DelegateRequest) (Response, error) {
			return Response{Status: StatusAnswered, Answer: "ok"}, nil
		})
		lead.Store = store
		if _, err := lead.Run(context.Background(), routineRequest); err != nil {
			t.Fatal(err)
		}
		if len(client.responses) != 0 {
			t.Fatalf("%s: %d scripted replies unused", name, len(client.responses))
		}
		reply := client.requests[len(client.requests)-1]
		if !strings.Contains(reply, replySkillsKey) || !strings.Contains(reply, "the note merged-into-original") {
			t.Errorf("%s: the reply stage was not handed the skill the code loaded", name)
		}
	}
}

func TestDiscoveredSkillsReadsTheNamesAStepAsksFor(t *testing.T) {
	for code, want := range map[string][]ax.Value{
		`await discover({ skills: ["duplicate playbook"] }); await final("x", {})`:                  {"duplicate playbook"},
		"await discover({\n  skills: ['a b', `c`],\n});":                                            {"a b", "c"},
		`const s = await discover({ modules: ["m"], skills: ["one"] }); discover({skills:["two"]})`: {"one", "two"},
		"await discover({ skills: [`skill ${n}`, name] });":                                         nil,
		`await ask_graphjin({ questions: ["which skills: [\"x\"] exist?"] })`:                       nil,
	} {
		if got := discoveredSkills(code); !reflect.DeepEqual(got, want) {
			t.Errorf("discoveredSkills(%q) = %v, want %v", code, got, want)
		}
	}
}

// The hand-off to the reply stage of a turn whose reply decides is the task
// of deciding and the turn's own record. Nothing the code ended on is in it.
func TestHandDecisionReplacesWhatTheCodeEndedOn(t *testing.T) {
	record := map[string]any{replyAnswersKey: []any{map[string]any{"question": "q", "status": StatusAnswered, "answer": "a"}}}
	for name, args := range map[string][]any{
		"a task alone":            {"No change is needed."},
		"a task and its evidence": {"Propose the change.", map[string]any{"proposes_change": true, "change_request": "Set ticket 26's note"}},
		"nothing passed on":       nil,
	} {
		for _, kind := range []string{"final", "respond"} {
			step := map[string]any{"type": kind, "args": args}
			handDecision(step, record)
			got := anySlice(step["args"])
			if len(got) != 2 || got[0] != leadDecisionTask || !reflect.DeepEqual(got[1], record) {
				t.Errorf("%s (%s): hand-off = %v, want the task of deciding and the turn's record", name, kind, got)
			}
		}
	}
	// A step that does not end the stage is left as it is.
	step := map[string]any{"kind": "result", "result": nil}
	handDecision(step, record)
	if _, added := step["args"]; added {
		t.Fatal("a step that did not end the stage was given a hand-off")
	}
}

// The turn's record is what its functions returned: every question with how
// it ended and what came back, the skills it loaded, the check-backs it
// scheduled. A turn that asked nothing says so, so the reply cannot take the
// instruction's facts for checked.
func TestTurnRecordIsWhatTheTurnsFunctionsReturned(t *testing.T) {
	run := &leadRun{followUps: map[string]FollowUp{}}
	if got := run.turnRecord(); len(got) != 1 || got[replyAnswersKey] != replyNothingAsked {
		t.Fatalf("record of a turn that did nothing = %v", got)
	}
	run.delegations = []leadDelegation{
		{question: "Is account 1 at churn risk?", resp: Response{Status: StatusAnswered, Answer: "Yes.", Data: map[string]any{"status": "churn_risk"}}},
		{question: "Does account 1 have a failed invoice?", err: errors.New("agent actor loop exceeded max steps")},
	}
	run.loadedSkillText = []any{map[string]any{"name": "duplicates", "content": "Close duplicates with the note Duplicate per playbook."}}
	run.followUps["check ticket 25 again"] = FollowUp{ID: "f1", At: "2026-11-12T20:00:00Z"}
	got := run.turnRecord()
	answers := anySlice(got[replyAnswersKey])
	if len(answers) != 2 {
		t.Fatalf("answers = %v, want both questions", got[replyAnswersKey])
	}
	first, second := mapValue(answers[0]), mapValue(answers[1])
	if first["answer"] != "Yes." || first["status"] != StatusAnswered || mapValue(first["data"])["status"] != "churn_risk" {
		t.Fatalf("an answered question = %v, want its status, answer and data", first)
	}
	if second["status"] != StatusError || !strings.Contains(fmt.Sprint(second["errors"]), "exceeded max steps") {
		t.Fatalf("a failed question = %v, want its status and error", second)
	}
	if skills := anySlice(got[replySkillsKey]); len(skills) != 1 || !strings.Contains(fmt.Sprint(skills[0]), "Duplicate per playbook") {
		t.Fatalf("loaded skills = %v", got[replySkillsKey])
	}
	if want := []any{map[string]any{"at": "2026-11-12T20:00:00Z", "note": "check ticket 25 again"}}; !reflect.DeepEqual(got[replyCheckBacksKey], want) {
		t.Fatalf("check-backs = %v, want %v", got[replyCheckBacksKey], want)
	}
}

// A lead function's error is the model's to read and act on, whichever name
// the code called it by. Ax's own registration of the qualified name fails
// the turn instead, which is why the lead keeps its own.
func TestLeadFunctionErrorReachesTheModelUnderEitherName(t *testing.T) {
	for _, call := range []string{"ask_graphjin", axFlatFunctionNamespace + ".ask_graphjin"} {
		client := &scriptedLeadClient{responses: []ax.Value{
			leadSteps{
				// No questions: the function returns an error.
				leadCode(`await ` + call + `({ questions: [] })`),
				leadCode(`final("done", {})`),
			},
			leadReply(StatusAnswered, "ok"),
		}}
		lead := newTestLead(t, Config{}, client, func(context.Context, DelegateRequest) (Response, error) {
			return Response{Status: StatusAnswered}, nil
		})
		resp, err := lead.Run(context.Background(), Request{Instruction: "How many orders are open?"})
		if err != nil || resp.Status != StatusAnswered {
			t.Fatalf("%s: a function error ended the turn: %s %+v %v", call, resp.Status, resp.Errors, err)
		}
		if len(client.requests) != 3 || !strings.Contains(client.requests[1], "questions must list at least one question") {
			t.Fatalf("%s: the model never saw the function's error (%d model calls)", call, len(client.requests))
		}
	}
}
