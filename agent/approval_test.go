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
	if got := settleReply(Response{Status: StatusBlocked, Answer: "no"}, replyFacts{approval: approval, guardBlocked: true}); got.Status != StatusBlocked || got.Approval != nil {
		t.Fatalf("a guard's block wins over a held write: %+v", got)
	}
	// Seen live: the model held a write, tried it again, was told it already
	// waits, and finished "blocked" on its own. The change still waits.
	if got := settleReply(Response{Status: StatusBlocked, Answer: "it needs approval"}, replyFacts{approval: approval}); got.Status != StatusNeedsApproval || got.Approval != approval {
		t.Fatalf("a model's own block hid a held write: %+v", got)
	}
	if got := settleReply(Response{Status: StatusAnswered, Approval: approval}, replyFacts{}); got.Approval != nil {
		t.Fatalf("only needs_approval carries an approval: %+v", got)
	}
	if err := ValidateWriteApproval("sometimes"); err == nil {
		t.Fatal("an unknown write_approval value must be rejected")
	}
}

// A run nobody is watching (a routine) proposes data writes and runs no
// write itself: GraphJin's own roots are refused, not held.
func TestHeldWritesOnlyProposesDataWritesAndRefusesTheRest(t *testing.T) {
	base := &proposingRuntime{}
	rt := writeReadyRuntime(base, true)
	rt.heldWritesOnly = true
	rt.state.capabilities.AllowedActions = append(rt.state.capabilities.AllowedActions, "gj_watch.insert")
	if _, err := rt.ExecuteGraphQL(context.Background(), map[string]any{"query": closeTicket}); err != nil {
		t.Fatal(err)
	}
	if len(base.proposed) != 1 || len(base.executed) != 0 || rt.state.pendingApproval == nil {
		t.Fatalf("data write: proposed=%v executed=%v pending=%v", base.proposed, base.executed, rt.state.pendingApproval)
	}

	other := writeReadyRuntime(base, true)
	other.heldWritesOnly = true
	other.state.capabilities.AllowedActions = append(other.state.capabilities.AllowedActions, "gj_watch.insert")
	out, err := other.ExecuteGraphQL(context.Background(), map[string]any{"query": `mutation { gj_watch(insert: { name: "w", query: "subscription { a { id } }" }) { id } }`})
	if err != nil {
		t.Fatal(err)
	}
	if !executionFailed(out) || len(base.executed) != 0 || len(base.proposed) != 1 {
		t.Fatalf("control-plane write: out=%v executed=%v proposed=%v", out, base.executed, base.proposed)
	}
	if !other.state.hasPolicyFinalBlockingViolation() {
		t.Fatal("refusing a control-plane write must be final")
	}
}

// A run that may only propose changes is told how a change is proposed, in
// the instruction its model reads. The catalog seed keeps the caller's own
// words, and a run that cannot write, or one somebody is watching, is told
// nothing.
func TestHeldWritesRunIsToldHowAChangeIsProposed(t *testing.T) {
	const question = "Propose setting support ticket 7 to resolved."
	run := func(cfg Config) (shown, seeded string) {
		t.Helper()
		rt := &fakeRuntime{}
		rt.catalogOverride = func(args map[string]any) any {
			if seeded == "" {
				seeded = stringArg(args, "search")
			}
			return fakeCatalogResult(args)
		}
		program := &fakeProgram{output: map[string]ax.Value{"status": StatusAnswered, "answer": "done"}}
		cfg.TimeoutSeconds = 5
		runner := newAgent(cfg, rt,
			WithClientFactory(func(Config) (ax.AIClient, error) { return fakeClient{}, nil }),
			WithProgramFactory(func(string, map[string]ax.Value) Program { return program }),
		)
		if _, err := runner.Run(context.Background(), Request{Instruction: question, Capabilities: profileWithRoleAndRoots("user")}); err != nil {
			t.Fatal(err)
		}
		return stringValue(program.forwardValues["instruction"]), seeded
	}

	shown, seeded := run(Config{HeldWritesOnly: true, WriteApproval: WriteApprovalRequired})
	if !strings.HasPrefix(shown, question) || !strings.Contains(shown, heldWritesNote) {
		t.Fatalf("a held-writes run was not told how a change is proposed: %q", shown)
	}
	if seeded != question {
		t.Fatalf("the catalog seed searched %q, want the caller's own words", seeded)
	}
	for name, cfg := range map[string]Config{
		"watched":   {WriteApproval: WriteApprovalRequired},
		"read-only": {HeldWritesOnly: true, WriteApproval: WriteApprovalRequired, ReadOnly: true},
	} {
		if shown, _ := run(cfg); shown != question {
			t.Fatalf("%s run: instruction = %q, want it unchanged", name, shown)
		}
	}
}

// The core runtime of a held-writes-only run executes a mutation only as a
// dry run whose transaction the caller rolls back.
func TestHeldWritesOnlyCoreRuntimeRunsOnlyDryRuns(t *testing.T) {
	rt := &coreRuntime{config: Config{HeldWritesOnly: true}}
	_, err := rt.ExecuteGraphQL(context.Background(), map[string]any{"query": closeTicket})
	if err == nil || !strings.Contains(err.Error(), "only propose") {
		t.Fatalf("direct mutation err = %v", err)
	}
	// A dry-run marker without a transaction is not a dry run.
	ctx := context.WithValue(context.Background(), dryRunKey{}, true)
	if _, err := rt.ExecuteGraphQL(ctx, map[string]any{"query": closeTicket}); err == nil {
		t.Fatal("a dry-run flag without a transaction must not unlock writes")
	}
}

// A reply carries one change: after a question held a write, the lead's
// later questions in the turn run read-only.
func TestLeadQuestionsAfterAHeldWriteOnlyRead(t *testing.T) {
	var readOnly []bool
	lead := &Lead{delegate: func(_ context.Context, d DelegateRequest) (Response, error) {
		readOnly = append(readOnly, d.ReadOnly)
		return Response{Status: StatusAnswered, Answer: "ok"}, nil
	}}
	run := &leadRun{lead: lead, answered: map[string]leadDelegation{}}
	run.runOne(context.Background(), 1, "How many tickets are open?", run.codeOnlyReads())
	run.delegations = append(run.delegations, leadDelegation{question: "Close ticket 7", resp: Response{Status: StatusNeedsApproval, Approval: &Approval{ID: "apr_1"}}})
	run.runOne(context.Background(), 2, "And close ticket 8 too.", run.codeOnlyReads())
	if len(readOnly) != 2 || readOnly[0] || !readOnly[1] {
		t.Fatalf("read-only flags = %v, want [false true]", readOnly)
	}
}
