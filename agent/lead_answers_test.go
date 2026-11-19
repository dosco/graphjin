package agent

import (
	"context"
	"strings"
	"sync"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
)

// feedbackLead is a lead with a delegate that notes how many model calls had
// been made when each question reached the data agent.
type feedbackLead struct {
	mu      sync.Mutex
	client  *scriptedLeadClient
	asked   []string
	askedAt map[string]int
	answers map[string]Response
}

func (f *feedbackLead) delegate(_ context.Context, d DelegateRequest) (Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, d.Question)
	if f.askedAt == nil {
		f.askedAt = map[string]int{}
	}
	f.client.mu.Lock()
	f.askedAt[d.Question] = len(f.client.requests)
	f.client.mu.Unlock()
	if resp, ok := f.answers[d.Question]; ok {
		return resp, nil
	}
	return Response{Status: StatusAnswered, Answer: "Answer to " + d.Question}, nil
}

func (f *feedbackLead) run(t *testing.T, cfg Config, req Request, responses ...ax.Value) Response {
	t.Helper()
	f.client = &scriptedLeadClient{responses: responses}
	lead, err := NewLead(cfg, f.delegate,
		WithClientFactory(func(Config) (ax.AIClient, error) { return f.client, nil }))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := lead.Run(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

const (
	whichAccount = "Which account does support ticket 31 belong to?"
	atChurnRisk  = "Is the account Zeta Corp at churn risk?"
	// The program a model writes: ask, ask again, finish, all in one block.
	// The second question is written before the first has an answer.
	askAskFinish = `const a = await ask_graphjin({ questions: ["` + whichAccount + `"] }); const b = await ask_graphjin({ questions: ["` + atChurnRisk + `"] }); await final("done", {})`
	askFinish    = `const b = await ask_graphjin({ questions: ["` + atChurnRisk + `"] }); await final("done", {})`
	finish       = `await final("done", {})`
)

// ask_graphjin ends the block it is called in: a second question written in
// the same block is asked only by a model call that was shown the first
// answer, and the turn finishes only after the second was shown too.
func TestLeadIsShownAnAnswerBeforeItsNextQuestion(t *testing.T) {
	answers := map[string]Response{whichAccount: {Status: StatusAnswered, Answer: "Ticket 31 belongs to Zeta Corp."}}
	req := Request{Instruction: "Is ticket 31's account at churn risk?"}

	on := &feedbackLead{answers: answers}
	resp := on.run(t, Config{}, req, leadCode(askAskFinish), leadCode(askFinish), leadCode(finish), leadReply(StatusAnswered, "Yes."))
	if on.askedAt[whichAccount] != 1 || on.askedAt[atChurnRisk] != 2 {
		t.Fatalf("questions asked after %v model calls; want the second held until the second call", on.askedAt)
	}
	if len(on.client.requests) != 4 || len(on.client.responses) != 0 || resp.Answer != "Yes." {
		t.Fatalf("%d model calls, %d scripted replies unused, answer %q", len(on.client.requests), len(on.client.responses), resp.Answer)
	}
	// Each answer is in front of the next model call without the block having
	// printed it.
	second, third := on.client.requests[1], on.client.requests[2]
	for _, want := range []string{"Ticket 31 belongs to Zeta Corp.", "Execution paused before ask_graphjin"} {
		if !strings.Contains(second, want) {
			t.Errorf("the model call after the first question does not carry %q", want)
		}
	}
	for _, want := range []string{"Answer to " + atChurnRisk, "Execution paused before final"} {
		if !strings.Contains(third, want) {
			t.Errorf("the model call after the second question does not carry %q", want)
		}
	}
	if delegations, _ := mapValue(resp.Data)["delegations"].([]any); len(delegations) != 2 {
		t.Fatalf("the reply records %d questions, want 2", len(delegations))
	}
}

// A turn with data attached has a first stage, and it reads an answer the
// same way: the hand-off written in the block that asked does not happen
// until a model call has been shown the answer.
func TestLeadHoldsAFirstStageHandOffUntilItsAnswerIsRead(t *testing.T) {
	f := &feedbackLead{answers: map[string]Response{whichAccount: {Status: StatusAnswered, Answer: "Ticket 31 belongs to Zeta Corp."}}}
	f.run(t, Config{},
		Request{Instruction: "Check this ticket.", Context: map[string]any{"_watch_events": []any{map[string]any{"id": "we1", "data_json": map[string]any{"id": 31}}}}},
		leadSteps{
			// First stage: asks and hands on in one block.
			leadCode(`const a = await ask_graphjin({ questions: ["` + whichAccount + `"] }); await final("check the account", { account: a[0].answer })`),
			leadCode(`await final("check the account", { account: a[0].answer })`),
			// Second stage.
			leadCode(`console.log("handed=" + inputs.distilledContext.account); const b = await ask_graphjin({ questions: ["` + atChurnRisk + `"] });`),
			leadCode(finish),
		},
		leadReply(StatusAnswered, "Yes."))
	if len(f.client.requests) != 5 || len(f.client.responses) != 0 {
		t.Fatalf("%d model calls, %d scripted replies unused; want hand-off held for one call", len(f.client.requests), len(f.client.responses))
	}
	if held := f.client.requests[1]; !strings.Contains(held, "Execution paused before final") || !strings.Contains(held, "Ticket 31 belongs to Zeta Corp.") {
		t.Fatal("the first stage was not shown its answer before it handed on")
	}
	if !strings.Contains(f.client.requests[3], "handed=Ticket 31 belongs to Zeta Corp.") {
		t.Fatal("the second stage never read what the first handed on")
	}
	if f.askedAt[atChurnRisk] != 3 {
		t.Fatalf("the second question was asked after %d model calls, want 3", f.askedAt[atChurnRisk])
	}
}

// A function's error is a message for the model. Ax's registration of a typed
// function ends the turn on one; handed over as a plain record, the error is
// shown on the next model call and the turn goes on, whichever name the code
// called the function by.
func TestLeadFunctionErrorIsReturnedToTheModel(t *testing.T) {
	for _, call := range []string{toolAskGraphJin, axFlatFunctionNamespace + "." + toolAskGraphJin} {
		f := &feedbackLead{}
		resp := f.run(t, Config{}, Request{Instruction: "How many orders are open?"},
			// No questions: the function returns an error.
			leadCode(`await `+call+`({ questions: [] })`),
			leadCode(finish),
			leadReply(StatusAnswered, "ok"))
		if resp.Status != StatusAnswered {
			t.Fatalf("%s: a function error ended the turn: %s %+v", call, resp.Status, resp.Errors)
		}
		if len(f.client.requests) != 3 || !strings.Contains(f.client.requests[1], "questions must list at least one question") {
			t.Fatalf("%s: the model never saw the function's error (%d model calls)", call, len(f.client.requests))
		}
	}
}

// remember returns a confirmation, not something to read: the code that
// follows it in the block runs, and a block that printed nothing still shows
// what it returned.
func TestLeadCodeGoesOnAfterAFunctionThatOnlyConfirms(t *testing.T) {
	store := &fakeLeadStore{}
	client := &scriptedLeadClient{responses: []ax.Value{
		leadCode(`await remember({ content: "Report amounts in EUR." }); await final("saved", {})`),
		leadReply(StatusAnswered, "Noted: amounts in EUR."),
	}}
	lead, err := NewLead(Config{}, func(context.Context, DelegateRequest) (Response, error) { return Response{}, nil },
		WithClientFactory(func(Config) (ax.AIClient, error) { return client, nil }))
	if err != nil {
		t.Fatal(err)
	}
	lead.Store = store
	if _, err := lead.Run(context.Background(), Request{Instruction: "remember I want amounts in EUR"}); err != nil {
		t.Fatal(err)
	}
	if len(store.remembered) != 1 || len(client.requests) != 2 || len(client.responses) != 0 {
		t.Fatalf("remembered %q in %d model calls, %d scripted replies unused; want one save and no pause", store.remembered, len(client.requests), len(client.responses))
	}

	client = &scriptedLeadClient{responses: []ax.Value{
		leadCode(`await remember({ content: "Report amounts in EUR." });`),
		leadCode(finish),
		leadReply(StatusAnswered, "Noted: amounts in EUR."),
	}}
	lead, _ = NewLead(Config{}, func(context.Context, DelegateRequest) (Response, error) { return Response{}, nil },
		WithClientFactory(func(Config) (ax.AIClient, error) { return client, nil }))
	lead.Store = &fakeLeadStore{}
	if _, err := lead.Run(context.Background(), Request{Instruction: "remember I want amounts in EUR"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(client.requests[1], toolRemember+" returned (this step printed nothing)") {
		t.Fatal("a block that saved a memory and printed nothing does not show what the save returned")
	}
}

func TestLeadResultPolicies(t *testing.T) {
	for function, want := range map[string]string{toolAskGraphJin: "inspect", toolRemember: "data", toolFollowUp: "data"} {
		if got := leadResultPolicy(function); got != want {
			t.Errorf("%s: policy %q, want %q", function, got, want)
		}
	}
}
