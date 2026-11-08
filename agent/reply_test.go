package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
)

// assertReplyContract fails when resp breaks the contract in reply.go.
func assertReplyContract(t *testing.T, label string, resp Response) {
	t.Helper()
	switch resp.Status {
	case StatusAnswered, StatusNeedsClarification, StatusBlocked, StatusError:
	default:
		t.Fatalf("%s: status %q is outside the contract", label, resp.Status)
	}
	if (resp.Status == StatusBlocked) != (resp.Refusal != nil) {
		t.Fatalf("%s: status %s with refusal %+v; only a blocked reply carries one, and it always does", label, resp.Status, resp.Refusal)
	}
	if resp.Refusal != nil && strings.TrimSpace(resp.Refusal.Code) == "" {
		t.Fatalf("%s: refusal without a code: %+v", label, resp.Refusal)
	}
	if resp.Status == StatusError && len(resp.Errors) == 0 {
		t.Fatalf("%s: error reply without errors", label)
	}
	if resp.Status == StatusNeedsClarification && len(resp.Errors) != 0 {
		t.Fatalf("%s: clarification carrying errors: %+v", label, resp.Errors)
	}
}

func TestSettleReplyEnforcesTheContract(t *testing.T) {
	guard := &Refusal{Code: "mutation_evidence_required", Retryable: true}
	for _, test := range []struct {
		name  string
		resp  Response
		facts replyFacts
		check func(Response) bool
	}{
		{"empty status is an answer", Response{Answer: "42"}, replyFacts{},
			func(r Response) bool { return r.Status == StatusAnswered }},
		{"invented status is an error", Response{Status: "done", Answer: "42"}, replyFacts{},
			func(r Response) bool {
				return r.Status == StatusError && r.Errors[0].Extensions["code"] == "agent_status_invalid"
			}},
		{"a block takes the refusal behind it", Response{Status: StatusBlocked}, replyFacts{refusal: func(Response) *Refusal { return guard }},
			func(r Response) bool { return r.Refusal == guard }},
		{"a block with only failures is an error", Response{Status: StatusBlocked, Answer: "could not"},
			replyFacts{failures: []ErrorInfo{{Message: "worker timed out"}}},
			func(r Response) bool { return r.Status == StatusError && r.Errors[0].Message == "worker timed out" }},
		{"an unexplained block names itself", Response{Status: StatusBlocked, Answer: "I won't."}, replyFacts{},
			func(r Response) bool {
				return r.Refusal.Code == "agent_blocked" && r.Refusal.Retryable && r.Refusal.Because[0] == "I won't."
			}},
		{"an answer sheds a stale refusal", Response{Status: StatusAnswered, Answer: "ok", Refusal: guard}, replyFacts{},
			func(r Response) bool { return r.Refusal == nil }},
		{"an error always says why", Response{Status: StatusError}, replyFacts{},
			func(r Response) bool { return len(r.Errors) == 1 && r.Errors[0].Message != "" }},
		{"a clarification carries no errors", Response{Status: StatusNeedsClarification, Answer: "Which one?", Errors: []ErrorInfo{{Message: "x"}}}, replyFacts{},
			func(r Response) bool { return len(r.Errors) == 0 && r.Answer == "Which one?" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := settleReply(test.resp, test.facts)
			assertReplyContract(t, test.name, got)
			if !test.check(got) {
				t.Fatalf("settled = %+v", got)
			}
		})
	}
}

func TestForwardErrorReplyMapsAxEndings(t *testing.T) {
	clarify := forwardErrorReply(ax.AxError{Category: "clarification", Payload: map[string]any{"question": "Which plan?"}})
	if clarify.Status != StatusNeedsClarification || !strings.Contains(clarify.Answer, "Which plan?") {
		t.Fatalf("clarification = %+v", clarify)
	}
	failed := forwardErrorReply(errors.New("provider exploded"))
	if failed.Status != StatusError || len(failed.Errors) != 1 {
		t.Fatalf("plain error = %+v", failed)
	}
	for _, resp := range []Response{clarify, failed} {
		assertReplyContract(t, "forward error", settleReply(resp, replyFacts{}))
	}
}

// The worker blocked with no violation and no coded error used to come back
// with refusal null; its own guards are now only the first source of a
// refusal, not the only one.
func TestWorkerFinalizeBlockAlwaysCarriesARefusal(t *testing.T) {
	state := newDiscoveryState("Insert support ticket 999999.")
	resp := state.finalize(Response{Status: StatusBlocked, Answer: "I will not insert that."})
	assertReplyContract(t, "worker block", resp)
	if resp.Refusal.Code != "agent_blocked" {
		t.Fatalf("refusal = %+v", resp.Refusal)
	}
	state = newDiscoveryState("Show invoices.")
	resp = state.finalize(Response{Status: "complete", Answer: "Here are the invoices."})
	assertReplyContract(t, "worker invented status", resp)
	if resp.Status != StatusError {
		t.Fatalf("an invented status must not pass as an answer: %+v", resp)
	}
}

// The lead's model picks its own status class; whatever it picks, the reply
// that leaves Lead.Run keeps the contract.
func TestLeadRepliesKeepTheContract(t *testing.T) {
	question := "How many open tickets are there?"
	for _, test := range []struct {
		name   string
		status string
		worker Response
	}{
		{"answered", StatusAnswered, leadTestAnswers[question]},
		{"blocked over a guard refusal", StatusBlocked, Response{Status: StatusBlocked, Refusal: &Refusal{Code: "access_blocked"}}},
		{"blocked over a failure", StatusBlocked, Response{Status: StatusError, Errors: []ErrorInfo{{Message: "boom"}}}},
		{"blocked on its own", StatusBlocked, leadTestAnswers[question]},
		{"clarifying", StatusNeedsClarification, leadTestAnswers[question]},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &scriptedLeadClient{responses: []ax.Value{
				leadToolCall("c1", question),
				leadReply(test.status, "There are 2 open tickets."),
			}}
			lead := newTestLead(t, Config{}, client, func(context.Context, DelegateRequest) (Response, error) {
				return test.worker, nil
			})
			resp, err := lead.Run(context.Background(), Request{Instruction: "open tickets?"})
			if err != nil {
				t.Fatal(err)
			}
			assertReplyContract(t, test.name, resp)
		})
	}
}

// A lead block its own run explains keeps its own code; it does not borrow an
// earlier delegated question's refusal.
func TestLeadOwnBlockKeepsItsCode(t *testing.T) {
	run := &leadRun{delegations: []leadDelegation{{question: "q", resp: Response{Status: StatusBlocked, Refusal: &Refusal{Code: "access_blocked"}}}}}
	facts := replyFacts{refusal: run.delegatedRefusal, failures: run.failures()}
	coded := settleReply(Response{Status: StatusBlocked, Errors: []ErrorInfo{{Message: "no runnable action", Extensions: map[string]any{"code": "agent_no_runtime_code"}}}}, facts)
	assertReplyContract(t, "own block", coded)
	if coded.Refusal.Code != "agent_no_runtime_code" {
		t.Fatalf("own coded block borrowed %q", coded.Refusal.Code)
	}
	borrowed := settleReply(Response{Status: StatusBlocked, Answer: "That was refused."}, facts)
	if borrowed.Refusal.Code != "access_blocked" {
		t.Fatalf("an unexplained lead block must inherit the delegated refusal, got %q", borrowed.Refusal.Code)
	}
}
