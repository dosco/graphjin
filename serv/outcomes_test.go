package serv

import (
	"strings"
	"testing"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/core/v3/sourcecap"
)

func TestParseApprovalExpectation(t *testing.T) {
	ok := map[string]any{"query": "query { orders { count_id } }", "path": "orders.0.count_id", "op": "increase", "value": 2, "within_hours": 24}
	if exp, err := parseApprovalExpectation(ok); err != nil || exp == nil || exp.Op != "increase" || exp.WithinHours != 24 {
		t.Fatalf("valid expectation = %+v, %v", exp, err)
	}
	if exp, err := parseApprovalExpectation(nil); exp != nil || err != nil {
		t.Fatal("no expectation is not an error")
	}
	for name, bad := range map[string]map[string]any{
		"mutation":   {"query": "mutation { orders(delete: true) { id } }", "path": "a", "op": "gte", "value": 1, "within_hours": 1},
		"no path":    {"query": "query { a { id } }", "op": "gte", "value": 1, "within_hours": 1},
		"bad op":     {"query": "query { a { id } }", "path": "a", "op": "more", "value": 1, "within_hours": 1},
		"too long":   {"query": "query { a { id } }", "path": "a", "op": "gte", "value": 1, "within_hours": 10000},
		"no value":   {"query": "query { a { id } }", "path": "a", "op": "gte", "within_hours": 1},
		"subscribes": {"query": "subscription { a { id } }", "path": "a", "op": "gte", "value": 1, "within_hours": 1},
	} {
		if _, err := parseApprovalExpectation(bad); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestExpectationHolds(t *testing.T) {
	base := 10.0
	for _, test := range []struct {
		op       string
		value    float64
		observed float64
		want     bool
	}{
		{"gte", 5, 5, true}, {"gte", 5, 4, false},
		{"lte", 5, 5, true}, {"lte", 5, 6, false},
		{"eq", 3, 3, true}, {"eq", 3, 3.5, false},
		{"increase", 2, 12, true}, {"increase", 2, 11, false},
		{"decrease", 2, 8, true}, {"decrease", 2, 9, false},
	} {
		exp := &approvalExpectation{Op: test.op, Value: test.value, Baseline: &base}
		if got := exp.holds(test.observed); got != test.want {
			t.Errorf("%s %v from %v observed %v = %v", test.op, test.value, base, test.observed, got)
		}
	}
}

// closedOrders measures the change approvalTestWrite makes.
var closedOrdersExpect = map[string]any{
	"query": `query { orders(where: { status: { eq: "closed" } }) { count_id } }`, "path": "orders.0.count_id",
	"op": "increase", "value": 1, "within_hours": 2,
}

// A change proposed with an expectation is measured before it runs, checked
// once its window passes, and lands on the owner's ledger as a hit.
func TestApprovedChangeIsCheckedAgainstItsExpectation(t *testing.T) {
	_, svc, rt := newApprovalService(t)
	ctx := approvalUserCtx("user_1")
	_, approval, err := rt.ProposeWrite(ctx, map[string]any{"query": approvalTestWrite, "expect": closedOrdersExpect})
	if err != nil {
		t.Fatal(err)
	}
	if approval.Expect != "orders.0.count_id up by at least 1 within 2h" {
		t.Fatalf("expect = %q", approval.Expect)
	}
	row, _ := svc.approvalRow(ctx, approval.ID)
	if exp := expectationFromRow(row); exp == nil || exp.Baseline == nil || *exp.Baseline != 0 {
		t.Fatalf("baseline = %+v", exp)
	}
	if _, err := svc.decideApproval(ctx, approval.ID, "approve", approval.Hash); err != nil {
		t.Fatal(err)
	}
	row, _ = svc.approvalRow(ctx, approval.ID)
	if stringMapValue(row, "outcome_status") != outcomePending {
		t.Fatalf("outcome not scheduled: %v", row)
	}
	// Not due yet.
	if n, err := svc.sweepOutcomeChecks(ctx, time.Now()); err != nil || n != 0 {
		t.Fatalf("early sweep checked %d (%v)", n, err)
	}
	if n, err := svc.sweepOutcomeChecks(ctx, time.Now().Add(3*time.Hour)); err != nil || n != 1 {
		t.Fatalf("due sweep checked %d (%v)", n, err)
	}
	row, _ = svc.approvalRow(ctx, approval.ID)
	if stringMapValue(row, "outcome_status") != outcomeHit || stringMapValue(row, "outcome_value") != "1" {
		t.Fatalf("outcome = %v / %v", row["outcome_status"], row["outcome_value"])
	}
	notes, _ := svc.listNotifications(ctx, true, 0, "")
	found := false
	for _, n := range notes {
		if n["kind"] == notificationOutcomeHit && strings.Contains(stringFromAny(n["body"]), "went from 0 to 1") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no hit notification: %v", notes)
	}
	// Checked once: a second sweep finds nothing.
	if n, _ := svc.sweepOutcomeChecks(ctx, time.Now().Add(4*time.Hour)); n != 0 {
		t.Fatalf("a checked change was checked again")
	}
	ledger, err := svc.reliabilityLedger(ctx)
	if err != nil {
		t.Fatal(err)
	}
	overall := ledger["overall"].(*reliabilityTally)
	if overall.Checked != 1 || overall.Hits != 1 || *overall.HitRate != 1 {
		t.Fatalf("ledger = %+v", overall)
	}
	byChange := ledger["by_change"].([]*reliabilityTally)
	if len(byChange) != 1 || byChange[0].Key != "data.update orders" {
		t.Fatalf("by change = %+v", byChange[0])
	}
}

// A miss is the agent's to look into: it starts a turn in the change's
// thread, and the ledger counts it.
func TestMissedExpectationStartsAFollowUpTurn(t *testing.T) {
	db, svc := newSQLiteWatchServiceWithOptions(t, 20, func(c *Config) {
		c.Agent.Enabled = true
		c.Agent.Lead.Enabled = true
		c.Agent.WriteApproval = gjagent.WriteApprovalRequired
		c.MCP.AllowMutations = true
		c.MCP.IncludeToolsWithAgent = true
		c.MCP.AllowRawQueries = true
		c.Core.Threads.Enabled = true
		c.Core.Sources[0].Access.Write = core.AccessModeAuthenticated
		c.Core.Sources[0].Capabilities = map[string]bool{sourcecap.KeyDataWrite: true}
	})
	if _, err := db.Exec(`INSERT INTO orders (id, status) VALUES (1, 'open')`); err != nil {
		t.Fatal(err)
	}
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatal(err)
	}
	startSQLiteWatchCore(t, svc, db)
	base, err := newServiceAgentRuntime(svc, agentConfigFromService(svc.conf))
	if err != nil {
		t.Fatal(err)
	}
	rt := base.(*serviceAgentRuntime)
	ctx := approvalUserCtx("user_1")
	thread, err := svc.createThread(ctx, threadSpec{Title: "orders"})
	if err != nil {
		t.Fatal(err)
	}
	threadID := stringFromAny(thread["id"])
	runner := &scriptedAgentRunner{resp: gjagent.Response{Status: gjagent.StatusAnswered, Answer: "Only one order was open, so the count could not rise by five."}}
	withScriptedAgentRunner(t, runner)
	expect := map[string]any{}
	for k, v := range closedOrdersExpect {
		expect[k] = v
	}
	expect["value"] = 5
	approvalContinuationsInline = true
	t.Cleanup(func() { approvalContinuationsInline = false })
	_, approval, err := rt.ProposeWrite(withRunThread(ctx, threadID, "run_1"), map[string]any{"query": approvalTestWrite, "expect": expect})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.decideApproval(ctx, approval.ID, "approve", approval.Hash); err != nil {
		t.Fatal(err)
	}
	runner.req = gjagent.Request{}
	if n, err := svc.sweepOutcomeChecks(ctx, time.Now().Add(3*time.Hour)); err != nil || n != 1 {
		t.Fatalf("sweep checked %d (%v)", n, err)
	}
	if !strings.Contains(runner.req.Instruction, "so it missed") || !strings.Contains(runner.req.Instruction, "went from 0 to 1") {
		t.Fatalf("miss turn instruction = %q", runner.req.Instruction)
	}
	if run, _ := svc.runStoreRow(ctx, "run_o_"+approval.ID); stringMapValue(run, "status") != runStatusDone {
		t.Fatalf("miss turn run = %v", run)
	}
	ledger, _ := svc.reliabilityLedger(ctx)
	if overall := ledger["overall"].(*reliabilityTally); overall.Misses != 1 || *overall.HitRate != 0 {
		t.Fatalf("ledger = %+v", overall)
	}
}

// An expectation that cannot be measured now could never be checked, so the
// write is refused with what to fix.
func TestUnmeasurableExpectationIsRefused(t *testing.T) {
	db, _, rt := newApprovalService(t)
	bad := map[string]any{"query": `query { orders { count_id } }`, "path": "orders.0.nope", "op": "gte", "value": 1, "within_hours": 1}
	if _, _, err := rt.ProposeWrite(approvalUserCtx("user_1"), map[string]any{"query": approvalTestWrite, "expect": bad}); err == nil || !strings.Contains(err.Error(), "available keys") {
		t.Fatalf("err = %v", err)
	}
	if orderStatus(t, db, 1) != "open" {
		t.Fatal("a refused proposal changed the database")
	}
}

// A change an autonomy rule ran starts its outcome clock at once.
func TestAutonomousChangeSchedulesItsCheck(t *testing.T) {
	_, svc, _ := newApprovalService(t)
	svc.conf.Agent.Autonomy = []gjagent.AutonomyRule{{Action: "data.update", Table: "orders", Level: gjagent.AutonomyAuto}}
	rt, err := newServiceAgentRuntime(svc, agentConfigFromService(svc.conf))
	if err != nil {
		t.Fatal(err)
	}
	ctx := approvalUserCtx("user_1")
	_, approval, err := rt.(*serviceAgentRuntime).ProposeWrite(ctx, map[string]any{"query": approvalTestWrite, "expect": closedOrdersExpect})
	if err != nil || !approval.Decided() {
		t.Fatalf("approval = %+v err %v", approval, err)
	}
	row, _ := svc.approvalRow(ctx, approval.ID)
	if stringMapValue(row, "outcome_status") != outcomePending || stringMapValue(row, "outcome_due_at") == "" {
		t.Fatalf("autonomous change not scheduled: %v", row)
	}
}

// With nobody watching, a change must say what it should achieve; the
// refusal shows how, for the table the change touches.
func TestUnattendedChangeMustCarryAnExpectation(t *testing.T) {
	db, _, rt := newApprovalService(t)
	ctx := withRunOrigin(approvalUserCtx("user_1"), runOrigin{Origin: approvalOriginRoutine, WatchID: "w_1"})
	_, _, err := rt.ProposeWrite(ctx, map[string]any{"query": approvalTestWrite})
	if err == nil || !strings.Contains(err.Error(), "nobody watching") || !strings.Contains(err.Error(), `path: "orders.0.count_id"`) {
		t.Fatalf("err = %v", err)
	}
	if orderStatus(t, db, 1) != "open" {
		t.Fatal("a refused proposal changed the database")
	}
	// A person in the conversation may leave it out.
	if _, approval, err := rt.ProposeWrite(approvalUserCtx("user_1"), map[string]any{"query": approvalTestWrite}); err != nil || approval == nil {
		t.Fatalf("a chat proposal without expect = %v", err)
	}
}
