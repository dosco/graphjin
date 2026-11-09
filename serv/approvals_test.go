package serv

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/core/v3/sourcecap"
)

const approvalTestWrite = `mutation { orders(where: { id: { eq: 1 } }, update: { status: "closed" }) { id status } }`

// newApprovalService is a SQLite service with write approvals required and
// one open order to change.
func newApprovalService(t *testing.T) (*sql.DB, *graphjinService, *serviceAgentRuntime) {
	t.Helper()
	db, svc := newSQLiteWatchServiceWithOptions(t, 20, func(c *Config) {
		c.Agent.Enabled = true
		c.Agent.WriteApproval = gjagent.WriteApprovalRequired
		c.MCP.AllowMutations = true
		// The agent's capability profile, which the decision re-checks, grants
		// writes only with the primitive tools enabled, as the demos run.
		c.MCP.IncludeToolsWithAgent = true
		c.MCP.AllowRawQueries = true
		c.Core.Sources[0].Access.Write = core.AccessModeAuthenticated
		c.Core.Sources[0].Capabilities = map[string]bool{sourcecap.KeyDataWrite: true}
	})
	if _, err := db.Exec(`INSERT INTO orders (id, status) VALUES (1, 'open'), (2, 'open')`); err != nil {
		t.Fatal(err)
	}
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("initArtifactsBeforeCore: %v", err)
	}
	startSQLiteWatchCore(t, svc, db)
	rt, err := newServiceAgentRuntime(svc, agentConfigFromService(svc.conf))
	if err != nil {
		t.Fatal(err)
	}
	return db, svc, rt.(*serviceAgentRuntime)
}

func orderStatus(t *testing.T, db *sql.DB, id int) string {
	t.Helper()
	var status string
	if err := db.QueryRow(`SELECT status FROM orders WHERE id = ?`, id).Scan(&status); err != nil {
		t.Fatal(err)
	}
	return status
}

func approvalUserCtx(user string) context.Context {
	return contextWithUserRole(artifactUserCtx(user), "user")
}

func TestProposeWriteDryRunsAndHoldsTheWrite(t *testing.T) {
	db, svc, rt := newApprovalService(t)
	if svc.conf.Agent.WriteApproval != gjagent.WriteApprovalRequired || agentConfigFromService(svc.conf).WriteApproval != gjagent.WriteApprovalRequired {
		t.Fatal("approvals must be required for this service")
	}
	ctx := approvalUserCtx("user_1")
	out, approval, err := rt.ProposeWrite(ctx, map[string]any{"query": approvalTestWrite})
	if err != nil {
		t.Fatal(err)
	}
	if approval == nil || approval.Hash == "" || approval.Query != approvalTestWrite {
		t.Fatalf("approval = %+v out = %v", approval, out)
	}
	if approval.Preview["available"] != true || intFromAny(approval.Preview["rows"]) != 1 || approval.Summary != "update 1 row in orders" {
		t.Fatalf("preview = %+v summary = %q", approval.Preview, approval.Summary)
	}
	if got := orderStatus(t, db, 1); got != "open" {
		t.Fatalf("the dry run changed the database: status = %q", got)
	}
	rows, err := svc.listApprovals(ctx)
	if err != nil || len(rows) != 1 || rows[0]["status"] != approvalStatusPending {
		t.Fatalf("stored approvals = %v err = %v", rows, err)
	}
	if other, _ := svc.listApprovals(approvalUserCtx("user_2")); len(other) != 0 {
		t.Fatalf("another user sees the approval: %v", other)
	}
}

// A dry run that fails is the write failing: nothing is stored and the
// failure comes back for the agent's normal recovery.
func TestProposeWriteReturnsAFailingDryRun(t *testing.T) {
	_, svc, rt := newApprovalService(t)
	ctx := approvalUserCtx("user_1")
	out, approval, err := rt.ProposeWrite(ctx, map[string]any{"query": `mutation { orders(where: { id: { eq: 1 } }, update: { no_such_column: "x" }) { id } }`})
	if err == nil && approval != nil {
		t.Fatalf("a failing write was held: %+v", approval)
	}
	if err == nil && !dryRunFailed(out) {
		t.Fatalf("out = %v, want the dry run's errors", out)
	}
	if rows, _ := svc.listApprovals(ctx); len(rows) != 0 {
		t.Fatalf("a failing write was stored: %v", rows)
	}
}

func TestDecideApprovalRunsTheExactWriteOnce(t *testing.T) {
	db, svc, rt := newApprovalService(t)
	ctx := approvalUserCtx("user_1")
	_, approval, err := rt.ProposeWrite(ctx, map[string]any{"query": approvalTestWrite})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.decideApproval(ctx, approval.ID, "approve", "wrong"); !errors.Is(err, errApprovalStale) {
		t.Fatalf("wrong hash err = %v", err)
	}
	if _, err := svc.decideApproval(approvalUserCtx("user_2"), approval.ID, "approve", approval.Hash); !errors.Is(err, errApprovalNotFound) {
		t.Fatalf("another user's decision err = %v", err)
	}
	if got := orderStatus(t, db, 1); got != "open" {
		t.Fatalf("a refused decision changed the database: %q", got)
	}
	row, err := svc.decideApproval(ctx, approval.ID, "approve", approval.Hash)
	if err != nil {
		t.Fatal(err)
	}
	if row["status"] != approvalStatusExecuted || orderStatus(t, db, 1) != "closed" {
		t.Fatalf("approved row = %v, order status = %q", row, orderStatus(t, db, 1))
	}
	if _, err := svc.decideApproval(ctx, approval.ID, "approve", approval.Hash); err == nil || !strings.Contains(err.Error(), "not pending") {
		t.Fatalf("a second approval err = %v", err)
	}
}

func TestRejectedOrExpiredApprovalChangesNothing(t *testing.T) {
	db, svc, rt := newApprovalService(t)
	ctx := approvalUserCtx("user_1")
	_, rejected, err := rt.ProposeWrite(ctx, map[string]any{"query": approvalTestWrite})
	if err != nil {
		t.Fatal(err)
	}
	row, err := svc.decideApproval(ctx, rejected.ID, "reject", rejected.Hash)
	if err != nil || row["status"] != approvalStatusRejected || orderStatus(t, db, 1) != "open" {
		t.Fatalf("reject: row = %v err = %v status = %q", row, err, orderStatus(t, db, 1))
	}

	_, expired, err := rt.ProposeWrite(ctx, map[string]any{"query": strings.Replace(approvalTestWrite, "eq: 1", "eq: 2", 1)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.internalStoreMutationRows(ctx, "approvals", `where: { id: { eq: $id } }, update: $input`, "id",
		map[string]any{"id": expired.ID, "input": map[string]any{"expires_at": "2000-01-01T00:00:00Z"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.decideApproval(ctx, expired.ID, "approve", expired.Hash); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expired approval err = %v", err)
	}
	if got := orderStatus(t, db, 2); got != "open" {
		t.Fatalf("an expired approval ran: %q", got)
	}
}

// Everything that allowed the write is checked again when the user decides.
func TestApprovalRechecksPermissionWhenDecided(t *testing.T) {
	db, svc, rt := newApprovalService(t)
	ctx := approvalUserCtx("user_1")
	_, approval, err := rt.ProposeWrite(ctx, map[string]any{"query": approvalTestWrite})
	if err != nil {
		t.Fatal(err)
	}
	svc.conf.MCP.AllowMutations = false
	if _, err := svc.decideApproval(ctx, approval.ID, "approve", approval.Hash); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("revoked writes err = %v", err)
	}
	if got := orderStatus(t, db, 1); got != "open" {
		t.Fatalf("a revoked approval ran: %q", got)
	}
}

// A person decides through the gj_approval root; the agent never can.
func TestApprovalRootDecidesThroughGraphQL(t *testing.T) {
	db, svc, rt := newApprovalService(t)
	ctx := approvalUserCtx("user_1")
	_, approval, err := rt.ProposeWrite(ctx, map[string]any{"query": approvalTestWrite})
	if err != nil {
		t.Fatal(err)
	}
	res, err := svc.gj.GraphQL(ctx, `query { gj_approval { id status hash summary preview } }`, nil, nil)
	if err != nil || !strings.Contains(string(res.Data), approval.ID) {
		t.Fatalf("list: %s err = %v", res.Data, err)
	}
	if _, err := svc.gj.GraphQL(ctx, `mutation { gj_approval(insert: { query: "x" }) { id } }`, nil, nil); err == nil {
		t.Fatal("approvals must not be insertable")
	}
	vars := []byte(`{"id":"` + approval.ID + `","hash":"` + approval.Hash + `"}`)
	res, err = svc.gj.GraphQL(ctx, `mutation decide($id: String!, $hash: String!) { gj_approval(where: { id: { eq: $id } }, update: { decision: "approve", expected_hash: $hash }) { id status } }`, vars, nil)
	if err != nil || !strings.Contains(string(res.Data), approvalStatusExecuted) {
		t.Fatalf("decide: data=%s errors=%v err = %v", res.Data, res.Errors, err)
	}
	if got := orderStatus(t, db, 1); got != "closed" {
		t.Fatalf("status = %q", got)
	}
}

func TestApprovalDryRunTargetRefusesSourcesWithSideEffects(t *testing.T) {
	_, svc, _ := newApprovalService(t)
	ctx := approvalUserCtx("user_1")
	if _, table, reason, _ := svc.approvalDryRunTarget(ctx, approvalTestWrite, "{}"); reason != "" || table != "orders" {
		t.Fatalf("sqlite target: table = %q reason = %q", table, reason)
	}
	dbConf := svc.conf.Core.Databases["app"]
	dbConf.Type = "snowflake"
	svc.conf.Core.Databases["app"] = dbConf
	if _, _, reason, _ := svc.approvalDryRunTarget(ctx, approvalTestWrite, "{}"); reason == "" {
		t.Fatal("a warehouse source must not be dry-run")
	}
	if got := approvalSummary([]string{gjagent.CapabilityActionDataUpdate}, "orders", map[string]any{"available": false, "reason": "no"}); !strings.Contains(got, "not previewed") {
		t.Fatalf("summary = %q", got)
	}
}

// A write held during a thread turn continues in that thread after the
// decision. With no model configured, the turn still records the outcome.
func TestApprovalOutcomeLandsInItsThread(t *testing.T) {
	approvalContinuationsInline = true
	t.Cleanup(func() { approvalContinuationsInline = false })
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
	rt, err := newServiceAgentRuntime(svc, agentConfigFromService(svc.conf))
	if err != nil {
		t.Fatal(err)
	}
	ctx := approvalUserCtx("user_1")
	thread, err := svc.createThread(ctx, threadSpec{Title: "orders"})
	if err != nil {
		t.Fatal(err)
	}
	threadID := stringFromAny(thread["id"])
	_, approval, err := rt.(*serviceAgentRuntime).ProposeWrite(withRunThread(ctx, threadID, "run_turn"), map[string]any{"query": approvalTestWrite})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.decideApproval(ctx, approval.ID, "approve", approval.Hash); err != nil {
		t.Fatal(err)
	}
	messages, err := svc.threadMessages(ctx, threadID)
	if err != nil || len(messages) != 2 ||
		stringFromAny(messages[0]["content"]) != "Approved: update 1 row in orders" ||
		!strings.Contains(stringFromAny(messages[1]["content"]), "Approved and applied: update 1 row in orders") {
		t.Fatalf("thread messages = %v err = %v", messages, err)
	}
	if row, _ := svc.threadStoreRow(ctx, threadID); stringMapValue(row, "active_run_id") != "" {
		t.Fatalf("the outcome left the thread claimed: %v", row)
	}
	// The continuation is a real run, so a lost one would be swept.
	if run, err := svc.runStoreRow(ctx, "run_c_"+approval.ID); err != nil || stringMapValue(run, "status") != runStatusDone {
		t.Fatalf("continuation run = %v err = %v", run, err)
	}
	// Continuing the same decision again starts nothing.
	svc.continueAfterDecision(ctx, approval.ID)
	if messages, _ := svc.threadMessages(ctx, threadID); len(messages) != 2 {
		t.Fatalf("a repeated continuation added messages: %d", len(messages))
	}
}

// Approvals are switched on by agent config, which the discovery fingerprint
// did not cover: a deployment upgrading with a reused discovery generation
// failed to start ("add roles failed: table not found: ..._graphjin_approvals").
func TestDiscoveryFingerprintCoversTheApprovalsTable(t *testing.T) {
	conf := &Config{Core: core.Config{Artifacts: core.ArtifactsConfig{Enabled: true}}}
	conf.Agent.Enabled = true
	before, err := discoveryConfigFingerprint(conf)
	if err != nil {
		t.Fatal(err)
	}
	conf.Agent.WriteApproval = gjagent.WriteApprovalRequired
	after, err := discoveryConfigFingerprint(conf)
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("turning approvals on must change the discovery fingerprint")
	}
}

// A JWT identity carries its roles in the identity roles and no user role.
// The dry-run target used to compile as anon for it, and returned anon's
// "update blocked" as the write's own failure.
func TestApprovalDryRunCompilesAsTheCaller(t *testing.T) {
	_, svc, rt := newApprovalService(t)
	ctx := context.WithValue(context.Background(), core.UserIDKey, "user_1")
	ctx = context.WithValue(ctx, core.IdentityRolesKey, []string{"user"})
	ctx = context.WithValue(ctx, core.IdentityVarsKey, map[string]interface{}{"user_id": "user_1", "user_ref": safeArtifactIdentity("user_1", false), "account_id": "acct_1"})
	if role := svc.gj.RequestRole(ctx); role != "user" {
		t.Fatalf("RequestRole = %q, want user", role)
	}
	if _, _, reason, errs := svc.approvalDryRunTarget(ctx, approvalTestWrite, "{}"); reason != "" || len(errs) != 0 {
		t.Fatalf("reason = %q errs = %v", reason, errs)
	}
	if _, approval, err := rt.ProposeWrite(ctx, map[string]any{"query": approvalTestWrite}); err != nil || approval == nil {
		t.Fatalf("approval = %+v err = %v", approval, err)
	}
}

// A routine's held write is dry-run and stored with its origin, while the
// same runtime refuses to run a mutation directly.
func TestRoutineHeldWriteIsFiledUnderItsWatch(t *testing.T) {
	db, svc, _ := newApprovalService(t)
	conf := agentConfigFromService(svc.conf)
	conf.HeldWritesOnly = true
	base, err := newServiceAgentRuntime(svc, conf)
	if err != nil {
		t.Fatal(err)
	}
	rt := base.(*serviceAgentRuntime)
	ctx := withRunOrigin(approvalUserCtx("user_1"), runOrigin{Origin: approvalOriginRoutine, WatchID: "w_1", EventID: "e_1"})
	if out, err := rt.ExecuteGraphQL(ctx, map[string]any{"query": approvalTestWrite}); err == nil && !strings.Contains(fmt.Sprint(out), "only propose") {
		t.Fatalf("a direct mutation ran: %v", out)
	}
	if orderStatus(t, db, 1) != "open" {
		t.Fatal("a direct mutation changed the database")
	}
	_, approval, err := rt.ProposeWrite(ctx, map[string]any{"query": approvalTestWrite})
	if err != nil {
		t.Fatal(err)
	}
	if orderStatus(t, db, 1) != "open" {
		t.Fatal("the dry run was not rolled back")
	}
	row, err := svc.approvalRow(ctx, approval.ID)
	if err != nil {
		t.Fatal(err)
	}
	api := approvalAPIRow(row, time.Now())
	if api["origin"] != approvalOriginRoutine || api["watch_id"] != "w_1" || api["event_id"] != "e_1" ||
		mapFromAny(api["preview"])["available"] != true {
		t.Fatalf("approval = %v", api)
	}
}

// A routine's held write, once approved, continues with the model: it sees the
// outcome and the original watch event, runs as unattended as the routine
// did, and the owner is told it followed up.
func TestApprovedRoutineWriteContinuesWithTheModel(t *testing.T) {
	approvalContinuationsInline = true
	t.Cleanup(func() { approvalContinuationsInline = false })
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
	runner := &scriptedAgentRunner{resp: gjagent.Response{Status: gjagent.StatusAnswered, Answer: "Order 1 is closed now."}}
	withScriptedAgentRunner(t, runner)
	rt, err := newServiceAgentRuntime(svc, agentConfigFromService(svc.conf))
	if err != nil {
		t.Fatal(err)
	}
	ctx := approvalUserCtx("user_1")
	thread, err := svc.createThread(ctx, threadSpec{Title: "routine"})
	if err != nil {
		t.Fatal(err)
	}
	threadID := stringFromAny(thread["id"])
	proposeCtx := withRunOrigin(withRunThread(ctx, threadID, "run_routine"), runOrigin{Origin: approvalOriginRoutine, WatchID: "w_1", EventID: "e_1"})
	_, approval, err := rt.(*serviceAgentRuntime).ProposeWrite(proposeCtx, map[string]any{"query": approvalTestWrite})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.decideApproval(ctx, approval.ID, "approve", approval.Hash); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(runner.req.Instruction, "was approved and has been applied") {
		t.Fatalf("continuation instruction = %q", runner.req.Instruction)
	}
	if got := mapFromAny(runner.req.Context["_approval"]); got["status"] != approvalStatusExecuted || got["summary"] != "update 1 row in orders" {
		t.Fatalf("continuation context = %v", runner.req.Context)
	}
	if origin := runOriginFrom(runner.ctx); origin.Origin != approvalOriginContinuation || origin.WatchID != "w_1" {
		t.Fatalf("continuation origin = %+v", origin)
	}
	if !runner.conf.HeldWritesOnly {
		t.Fatal("a routine's continuation must be as unattended as the routine")
	}
	messages, _ := svc.threadMessages(ctx, threadID)
	if len(messages) != 2 || messages[1]["content"] != "Order 1 is closed now.\n\nNo further change was proposed." {
		t.Fatalf("thread messages = %v", messages)
	}
	notes, _ := svc.listNotifications(ctx, true, 0, "")
	if len(notes) != 1 || notes[0]["title"] != "The agent followed up on your decision" {
		t.Fatalf("notifications = %v", notes)
	}
}

// An autonomy rule runs an allowed write at once, records it as decided by
// the rule, and with notify tells the owner. Over the row cap it waits.
func TestAutonomyRunsAllowedWritesAndRecordsThem(t *testing.T) {
	db, svc, _ := newApprovalService(t)
	svc.conf.Agent.Autonomy = []gjagent.AutonomyRule{{Action: "data.update", Table: "orders", Level: gjagent.AutonomyNotify, MaxRows: 1}}
	rt, err := newServiceAgentRuntime(svc, agentConfigFromService(svc.conf))
	if err != nil {
		t.Fatal(err)
	}
	ctx := approvalUserCtx("user_1")
	out, approval, err := rt.(*serviceAgentRuntime).ProposeWrite(ctx, map[string]any{"query": approvalTestWrite})
	if err != nil {
		t.Fatal(err)
	}
	if !approval.Decided() || approval.Status != approvalStatusExecuted || approval.DecidedBy != "autonomy:data.update:orders:notify" {
		t.Fatalf("approval = %+v", approval)
	}
	if orderStatus(t, db, 1) != "closed" || !strings.Contains(fmt.Sprint(out), "closed") {
		t.Fatalf("the allowed write did not run: out=%v status=%s", out, orderStatus(t, db, 1))
	}
	row, _ := svc.approvalRow(ctx, approval.ID)
	if stringMapValue(row, "status") != approvalStatusExecuted || stringMapValue(row, "decided_by") != approval.DecidedBy {
		t.Fatalf("record = %v", row)
	}
	notes, _ := svc.listNotifications(ctx, true, 0, "")
	if len(notes) != 1 || notes[0]["kind"] != notificationAutonomousWrite {
		t.Fatalf("notifications = %v", notes)
	}

	// Two rows is over the cap: the write waits for the user.
	if _, err := db.Exec(`UPDATE orders SET status = 'open'`); err != nil {
		t.Fatal(err)
	}
	_, held, err := rt.(*serviceAgentRuntime).ProposeWrite(ctx, map[string]any{"query": `mutation { orders(where: { status: { eq: "open" } }, update: { status: "closed" }) { id } }`})
	if err != nil {
		t.Fatal(err)
	}
	if held.Decided() || orderStatus(t, db, 1) != "open" || orderStatus(t, db, 2) != "open" {
		t.Fatalf("over the cap: approval=%+v statuses %s %s", held, orderStatus(t, db, 1), orderStatus(t, db, 2))
	}
	// An update filtered on the column it sets returns no rows afterwards;
	// the preview still counts the two it changes.
	if held.Preview["rows"] != int64(2) || held.Summary != "update 2 rows in orders" {
		t.Fatalf("preview = %v summary = %q", held.Preview, held.Summary)
	}
}

// A write a service-identity run proposes belongs to the person who delegated
// it: they decide it, and it runs as the service identity.
func TestServiceRunApprovalBelongsToTheDelegator(t *testing.T) {
	db, svc, rt := newApprovalService(t)
	svc.conf.Agent.ServiceIdentity = gjagent.ServiceIdentityConfig{UserID: "svc_ops", Role: "user"}
	origin := runOrigin{Origin: approvalOriginRoutine, WatchID: "w_1", DelegatedBy: "user_1", RunAsID: "svc_ops", RunAsRole: "user"}
	serviceCtx := withRunOrigin(svc.ownerContext(context.Background(), "svc_ops", "user", ""), origin)
	_, approval, err := rt.ProposeWrite(serviceCtx, map[string]any{"query": approvalTestWrite})
	if err != nil {
		t.Fatal(err)
	}
	row, _ := svc.approvalRow(serviceCtx, approval.ID)
	if stringMapValue(row, "owner_id") != "user_1" || stringMapValue(row, "run_as_id") != "svc_ops" || stringMapValue(row, "delegated_by") != "user_1" {
		t.Fatalf("approval row = %v", row)
	}
	if _, err := svc.decideApproval(approvalUserCtx("svc_ops"), approval.ID, "approve", approval.Hash); !errors.Is(err, errApprovalNotFound) {
		t.Fatalf("the service identity decided its own write: %v", err)
	}
	if _, err := svc.decideApproval(approvalUserCtx("user_1"), approval.ID, "approve", approval.Hash); err != nil {
		t.Fatal(err)
	}
	if orderStatus(t, db, 1) != "closed" {
		t.Fatal("the approved write did not run")
	}
}

// A question the lead marks read-only runs on a read-only worker.
func TestLeadReadOnlyQuestionsRunOnAReadOnlyWorker(t *testing.T) {
	_, svc, _ := newApprovalService(t)
	conf := agentConfigFromService(svc.conf)
	conf.Lead.Enabled = true
	realLead := newGraphJinLeadRunner
	runner := &scriptedAgentRunner{}
	withScriptedAgentRunner(t, runner)
	lead, err := realLead(svc, conf, runner)
	if err != nil {
		t.Skipf("lead runner needs model options here: %v", err)
	}
	lr := lead.(leadRunner)
	if worker, err := lr.workerFor(gjagent.DelegateRequest{}); err != nil || worker != graphjinAgentRunner(runner) {
		t.Fatalf("a normal question = %v, %v", worker, err)
	}
	if _, err := lr.workerFor(gjagent.DelegateRequest{ReadOnly: true}); err != nil || !runner.conf.ReadOnly {
		t.Fatalf("a read-only question built conf %+v, err %v", runner.conf.ReadOnly, err)
	}
}

// When an autonomy rule ran a write during an unattended run, GraphJin says
// so itself rather than leaving it to the model's wording.
func TestUnattendedOutcomeNamesAutonomousChanges(t *testing.T) {
	_, svc, _ := newApprovalService(t)
	svc.conf.Agent.Autonomy = []gjagent.AutonomyRule{{Action: "data.update", Table: "orders", Level: gjagent.AutonomyAuto}}
	rt, err := newServiceAgentRuntime(svc, agentConfigFromService(svc.conf))
	if err != nil {
		t.Fatal(err)
	}
	ctx := withRunOrigin(withRunThread(approvalUserCtx("user_1"), "th_1", "run_7"), runOrigin{Origin: approvalOriginRoutine, WatchID: "w_1"})
	if _, approval, err := rt.(*serviceAgentRuntime).ProposeWrite(ctx, map[string]any{"query": approvalTestWrite}); err != nil || !approval.Decided() {
		t.Fatalf("autonomous write = %+v err %v", approval, err)
	}
	got := svc.stateUnattendedOutcome(ctx, gjagent.Response{Status: gjagent.StatusAnswered, Answer: "Closed it."})
	if got.Answer != "Made a change (update 1 row in orders) on its own, as configured.\n\nClosed it." {
		t.Fatalf("answer = %q", got.Answer)
	}
}
