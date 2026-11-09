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

// A write held during a thread turn reports its outcome in that thread, with
// no model call.
func TestApprovalOutcomeLandsInItsThread(t *testing.T) {
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
	messages, err := svc.internalStoreRows(ctx, "messages", `where: { thread_id: { eq: $thread_id } }`, messageStoreFields, map[string]any{"thread_id": threadID})
	if err != nil || len(messages) != 1 || !strings.Contains(stringMapValue(messages[0], "content"), "Approved and applied: update 1 row in orders") {
		t.Fatalf("thread messages = %v err = %v", messages, err)
	}
	if row, _ := svc.threadStoreRow(ctx, threadID); stringMapValue(row, "active_run_id") != "" {
		t.Fatalf("the outcome left the thread claimed: %v", row)
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
