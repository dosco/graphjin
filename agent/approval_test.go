package agent

import (
	"context"
	"strings"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
)

// proposingRuntime holds writes: it records every proposal and every real
// execution separately, so a test can prove a held write never ran.
type proposingRuntime struct {
	fakeRuntime
	executed   []string
	proposed   []string
	failDryRun bool
}

func (r *proposingRuntime) ExecuteGraphQL(_ context.Context, args map[string]any) (any, error) {
	r.executed = append(r.executed, stringArg(args, "query"))
	return map[string]any{"data": map[string]any{"support_tickets": []any{map[string]any{"id": 7}}}}, nil
}

func (r *proposingRuntime) ProposeWrite(_ context.Context, args map[string]any) (any, *Approval, error) {
	r.proposed = append(r.proposed, stringArg(args, "query"))
	if r.failDryRun {
		return map[string]any{"errors": []any{map[string]any{"message": "NOT NULL constraint failed: support_tickets.account_id"}}}, nil, nil
	}
	return map[string]any{"data": map[string]any{"support_tickets": []any{map[string]any{"id": 7}}}},
		&Approval{ID: "apr_1", Hash: "h1", Summary: "update 1 row in support_tickets", Preview: map[string]any{"available": true, "rows": 1}}, nil
}

const closeTicket = `mutation { support_tickets(where: { id: { eq: 7 } }, update: { status: "closed" }) { id } }`

// writeReadyRuntime is a protocol runtime that has already discovered what a
// write to support_tickets needs, so a test reaches the execution point.
func writeReadyRuntime(base GraphRuntime, approvals bool) *protocolRuntime {
	profile := &CapabilityProfile{RoleClass: "user", AllowedActions: []string{CapabilityActionDataUpdate, CapabilityActionDataInsert}}
	rt := newProtocolRuntime(base, "Close support ticket 7.", "", 8, profile, nil, CatalogSearchFeatures{})
	rt.writeApproval = approvals
	rt.state.seedOK = true
	rt.state.modelDiscoveryAction = true
	rt.state.securityRuntimeEvidence = true
	rt.state.mutationEvidenceSupplied = true
	rt.state.tablesDetailed["support_tickets"] = true
	rt.state.catalogIDs["table:app:main.support_tickets"] = true
	rt.state.catalogDetails = []string{"table:app:main.support_tickets"}
	return rt
}

func TestHeldWriteIsDataWritesOnly(t *testing.T) {
	for query, want := range map[string]bool{
		closeTicket: true,
		`mutation { insert_payments(objects: [{ amount_cents: 5 }]) { id } }`:                     true,
		`mutation { gj_watch(insert: { name: "w", query: "subscription { a { id } }" }) { id } }`: false,
		`query { support_tickets { id } }`:                                                        false,
	} {
		if got := heldWrite(query); got != want {
			t.Errorf("heldWrite(%q) = %v, want %v", query, got, want)
		}
	}
}

func TestApprovalHoldsTheWriteAndTheEngineWritesTheAnswer(t *testing.T) {
	base := &proposingRuntime{}
	rt := writeReadyRuntime(base, true)
	out, err := rt.ExecuteGraphQL(context.Background(), map[string]any{"query": closeTicket})
	if err != nil {
		t.Fatal(err)
	}
	if len(base.executed) != 0 || len(base.proposed) != 1 {
		t.Fatalf("executed=%v proposed=%v: a held write must only be proposed", base.executed, base.proposed)
	}
	if !strings.Contains(dumpAXValue(out), "held_for_approval") || rt.state.pendingApproval == nil {
		t.Fatalf("held result = %v", out)
	}
	// A model that claims the change was made does not get to say so.
	resp := rt.state.finalize(Response{Status: StatusAnswered, Answer: "Done, ticket 7 is closed."})
	assertReplyContract(t, "held write", resp)
	if resp.Status != StatusNeedsApproval || resp.Approval.ID != "apr_1" || strings.Contains(resp.Answer, "Done") ||
		!strings.Contains(resp.Answer, "Nothing has been changed yet") {
		t.Fatalf("resp = %+v", resp)
	}

	// One change per reply: a second held write in the same run is refused.
	_, err = rt.ExecuteGraphQL(context.Background(), map[string]any{"query": strings.Replace(closeTicket, "7", "8", 1)})
	if err == nil || !strings.Contains(err.Error(), "apr_1") || len(base.proposed) != 1 {
		t.Fatalf("second held write: err=%v proposed=%v", err, base.proposed)
	}
}

func TestApprovalOffOrControlPlaneWritesRun(t *testing.T) {
	base := &proposingRuntime{}
	if _, err := writeReadyRuntime(base, false).ExecuteGraphQL(context.Background(), map[string]any{"query": closeTicket}); err != nil {
		t.Fatal(err)
	}
	if len(base.executed) != 1 || len(base.proposed) != 0 {
		t.Fatalf("approvals off: executed=%v proposed=%v", base.executed, base.proposed)
	}
}

// A dry run that fails is the write failing: the normal failure handling
// applies and nothing is held.
func TestFailedDryRunIsAFailedWrite(t *testing.T) {
	base := &proposingRuntime{failDryRun: true}
	rt := writeReadyRuntime(base, true)
	_, err := rt.ExecuteGraphQL(context.Background(), map[string]any{"query": closeTicket})
	if err == nil || !strings.Contains(err.Error(), "NOT NULL") {
		t.Fatalf("failed dry run err = %v", err)
	}
	if rt.state.pendingApproval != nil || !rt.state.mutationAttempted || len(base.executed) != 0 {
		t.Fatalf("pending=%v attempted=%v executed=%v", rt.state.pendingApproval, rt.state.mutationAttempted, base.executed)
	}
}

func TestAgentNeverWritesApprovals(t *testing.T) {
	base := &proposingRuntime{}
	rt := writeReadyRuntime(base, true)
	out, err := rt.ExecuteGraphQL(context.Background(), map[string]any{
		"query": `mutation { gj_approval(where: { id: { eq: "apr_1" } }, update: { decision: "approve", expected_hash: "h1" }) { id } }`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(base.executed) != 0 || !strings.Contains(dumpAXValue(out), "approval_decision_reserved") {
		t.Fatalf("out=%v executed=%v", out, base.executed)
	}
	resp := rt.state.finalize(Response{Status: StatusAnswered, Answer: "Approved."})
	if resp.Status != StatusBlocked || resp.Refusal == nil || resp.Refusal.Code != "approval_decision_reserved" || resp.Refusal.Retryable {
		t.Fatalf("resp = %+v", resp)
	}
}

// Required approvals on a runtime that cannot hold writes refuse the write;
// running it would skip the user.
func TestRequiredApprovalWithoutAProposerRefuses(t *testing.T) {
	base := &successfulExecutionRuntime{}
	rt := writeReadyRuntime(base, true)
	if _, err := rt.ExecuteGraphQL(context.Background(), map[string]any{"query": closeTicket}); err != nil {
		t.Fatal(err)
	}
	if base.graphqlCalls != 0 {
		t.Fatal("the write ran without approval")
	}
	if resp := rt.state.finalize(Response{Status: StatusAnswered, Answer: "ok"}); resp.Refusal == nil || resp.Refusal.Code != "write_approval_unavailable" {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestLeadCarriesADelegatedApproval(t *testing.T) {
	question := "Close support ticket 7."
	approval := &Approval{ID: "apr_9", Hash: "h9", Summary: "update 1 row in support_tickets"}
	client := &scriptedLeadClient{responses: []ax.Value{
		leadToolCall("c1", question),
		leadReply(StatusAnswered, "Ticket 7 will be closed once you approve."),
	}}
	lead := newTestLead(t, Config{}, client, func(context.Context, DelegateRequest) (Response, error) {
		return Response{Status: StatusNeedsApproval, Answer: approvalAnswer(approval), Approval: approval}, nil
	})
	resp, err := lead.Run(context.Background(), Request{Instruction: "close ticket 7"})
	if err != nil {
		t.Fatal(err)
	}
	assertReplyContract(t, "lead approval", resp)
	if resp.Status != StatusNeedsApproval || resp.Approval.ID != "apr_9" ||
		!strings.Contains(resp.Answer, "once you approve") || !strings.Contains(resp.Answer, approvalAnswer(approval)) {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestSettleReplyApprovalRules(t *testing.T) {
	approval := &Approval{ID: "apr_2", Summary: "insert 1 row"}
	if got := settleReply(Response{Status: StatusNeedsApproval}, replyFacts{}); got.Status != StatusError {
		t.Fatalf("needs_approval without a proposal = %+v", got)
	}
	if got := settleReply(Response{Status: StatusBlocked, Answer: "no"}, replyFacts{approval: approval}); got.Status != StatusBlocked || got.Approval != nil {
		t.Fatalf("a block wins over a held write: %+v", got)
	}
	if got := settleReply(Response{Status: StatusAnswered, Approval: approval}, replyFacts{}); got.Approval != nil {
		t.Fatalf("only needs_approval carries an approval: %+v", got)
	}
	if err := ValidateWriteApproval("sometimes"); err == nil {
		t.Fatal("an unknown write_approval value must be rejected")
	}
}
