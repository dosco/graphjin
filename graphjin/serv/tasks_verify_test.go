package serv

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/dosco/graphjin/core/v3"
)

func saveTaskVerificationQuery(t *testing.T, svc *graphjinService, ctx context.Context, name string) {
	t.Helper()
	query := fmt.Sprintf(`query %s { orders(where: { status: { eq: "late" } }, order_by: { id: asc }) { id status } }`, name)
	res, err := svc.gj.GraphQL(ctx, query, nil, &core.RequestConfig{})
	if err != nil || len(res.Errors) != 0 {
		t.Fatalf("save verification query %s: err=%v errors=%+v", name, err, res.Errors)
	}
	if _, ok, err := svc.userArtifactRow(ctx, artifactKindSavedQuery, name); err != nil || !ok {
		t.Fatalf("saved verification query %s missing: ok=%v err=%v", name, ok, err)
	}
}

func TestTaskVerifySpecValidationAndPredicates(t *testing.T) {
	for _, raw := range []string{
		`{"saved_query_name":"q","expect":{"path":"orders.*","op":"empty"}}`,
		`{"saved_query_name":"q","expect":{"path":"orders","op":"script"}}`,
		`{"saved_query_name":"q","expect":{"path":"orders","op":"count_le","value":1.5}}`,
		`{"saved_query_name":"q","expect":{"path":"orders","op":"empty"},"condition_js":"true"}`,
	} {
		if _, err := parseTaskVerifySpec(raw); err == nil {
			t.Fatalf("invalid verify spec accepted: %s", raw)
		}
	}
	spec, err := parseTaskVerifySpec(`{"saved_query_name":"q","expect":{"path":"orders.0.count","op":"le","value":5},"recheck":"1ms"}`)
	if err != nil {
		t.Fatalf("parse valid verify spec: %v", err)
	}
	if spec.Recheck != "1m0s" || spec.RecheckWindow != time.Minute || len(spec.Hash) != 64 {
		t.Fatalf("normalized verify spec = %+v", spec)
	}
	data := map[string]any{"orders": []any{map[string]any{"count": 5.0}}}
	observed, err := taskVerifyPathValue(data, "orders.0.count")
	if err != nil {
		t.Fatalf("resolve path: %v", err)
	}
	passed, err := evaluateTaskVerifyExpectation(observed, spec)
	if err != nil || !passed {
		t.Fatalf("typed numeric comparison passed=%v err=%v", passed, err)
	}
}

func TestTaskImmediateVerificationPassAndFail(t *testing.T) {
	svc, cp := newSQLiteTaskService(t, 5, 20)
	ctx := contextWithUserRole(artifactUserCtx("verify_owner"), "analyst")
	saveTaskVerificationQuery(t, svc, ctx, "late_orders_check")

	passedTask := insertTaskForTest(t, cp, ctx, "Close only when late orders are empty")
	passedID := fmt.Sprint(passedTask["id"])
	passedRevision := sqliteRevision(t, svc.dbs["app"], "tasks")
	passed, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table: tasksRootTable, Operation: "update", Where: map[string]any{"id": passedID},
		Input: map[string]any{
			"status": "closed", "outcome": "No late orders remain.",
			"verify_json": map[string]any{"saved_query_name": "late_orders_check", "expect": map[string]any{"path": "orders", "op": "empty"}},
		},
	})
	if err != nil || passed["status"] != "closed" || passed["verify_status"] != "verified" || passed["closed_at"] == "" || passed["verify_attempts"] != int64(1) {
		t.Fatalf("verified close = %+v err=%v", passed, err)
	}
	if got := sqliteRevision(t, svc.dbs["app"], "tasks"); got != passedRevision+1 {
		t.Fatalf("immediate verification revision = %d, want %d", got, passedRevision+1)
	}
	assertTaskVerificationEntries(t, svc, ctx, passedID, 1, "passed")

	failedTask := insertTaskForTest(t, cp, ctx, "Remain open when verification fails")
	failedID := fmt.Sprint(failedTask["id"])
	failed, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table: tasksRootTable, Operation: "update", Where: map[string]any{"id": failedID},
		Input: map[string]any{
			"status": "closed", "outcome": "Claimed complete.",
			"verify_json": map[string]any{"saved_query_name": "late_orders_check", "expect": map[string]any{"path": "orders", "op": "not_empty"}},
		},
	})
	if err != nil || failed["status"] != "open" || failed["verify_status"] != "failed" || failed["closed_at"] != "" || failed["outcome"] != "" {
		t.Fatalf("failed verified close = %+v err=%v", failed, err)
	}
	assertTaskVerificationEntries(t, svc, ctx, failedID, 1, "failed")
	failedStored, err := svc.internalTaskStoreRow(ctx, failedID)
	if err != nil {
		t.Fatalf("read failed task: %v", err)
	}
	failedSpec, err := parseTaskVerifySpec(jsonMapString(failedStored, "verify_json"))
	if err != nil {
		t.Fatalf("parse stored failed spec: %v", err)
	}
	inserted, err := cp.insertTaskVerificationEntry(ctx, failedStored, failedSpec, taskVerificationResult{Observed: []any{}}, int64MapValue(failedStored, "verify_attempts"), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil || inserted {
		t.Fatalf("duplicate verification entry inserted=%v err=%v", inserted, err)
	}
	assertTaskVerificationEntries(t, svc, ctx, failedID, 1, "failed")
	var resp gjagent.Response
	svc.appendTaskNotices(ctx, gjagent.Request{TaskID: failedID}, &resp)
	notice, ok := taskNoticeForKind(resp, "task_verify_failed")
	if !ok || notice.Count != 1 || len(notice.TaskIDs) != 1 || notice.TaskIDs[0] != failedID {
		t.Fatalf("failed verification notice = %+v", resp.Notices)
	}

	cleared, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table: tasksRootTable, Operation: "update", Where: map[string]any{"id": failedID},
		Input: map[string]any{"verify_json": map[string]any{"saved_query_name": "late_orders_check", "expect": map[string]any{"path": "orders", "op": "empty"}}},
	})
	if err != nil || cleared["verify_status"] != "" {
		t.Fatalf("verify spec change did not clear failure: %+v err=%v", cleared, err)
	}
	closedWithoutVerify, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table: tasksRootTable, Operation: "update", Where: map[string]any{"id": failedID},
		Input: map[string]any{"status": "closed", "outcome": "Closed without a declared check.", "verify_json": nil},
	})
	if err != nil || closedWithoutVerify["status"] != "closed" || closedWithoutVerify["verify_status"] != "" || closedWithoutVerify["closed_at"] == "" {
		t.Fatalf("unverified close did not clear failure state: %+v err=%v", closedWithoutVerify, err)
	}
	resp = gjagent.Response{}
	svc.appendTaskNotices(ctx, gjagent.Request{TaskID: failedID}, &resp)
	if _, ok := taskNoticeForKind(resp, "task_verify_failed"); ok {
		t.Fatalf("closed task retained failed verification notice: %+v", resp.Notices)
	}
}

func TestTaskDelayedVerificationSweepAndReopenCancellation(t *testing.T) {
	svc, cp := newSQLiteTaskService(t, 5, 20)
	ctx := contextWithUserRole(artifactUserCtx("delayed_owner"), "analyst")
	saveTaskVerificationQuery(t, svc, ctx, "delayed_late_orders")
	task := insertTaskForTest(t, cp, ctx, "Recheck delayed orders")
	taskID := fmt.Sprint(task["id"])
	scheduled, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table: tasksRootTable, Operation: "update", Where: map[string]any{"id": taskID},
		Input: map[string]any{
			"status": "closed", "outcome": "No late orders remain.",
			"verify_json": map[string]any{"saved_query_name": "delayed_late_orders", "expect": map[string]any{"path": "orders", "op": "empty"}, "recheck": "1ms"},
		},
	})
	if err != nil || scheduled["status"] != "verifying" || scheduled["verify_status"] != "pending" || scheduled["closed_at"] != "" {
		t.Fatalf("scheduled verification = %+v err=%v", scheduled, err)
	}
	if _, err := svc.resolveAgentTaskContext(ctx, &gjagent.Request{TaskID: taskID}); err != nil {
		t.Fatalf("warm start while verifying: %v", err)
	}
	watch, err := newWatchControlPlane(svc).mutateRow(ctx, core.ManagedMutationRoot{
		Table: watchesRootTable, Operation: "insert",
		Input: map[string]any{"name": "verifying_task_watch", "task_id": taskID, "query": cursorOrdersWatchQuery("verifying_task_watch")},
	})
	if err != nil || stringMapValue(watch, "task_id") != taskID {
		t.Fatalf("watch link while verifying = %+v err=%v", watch, err)
	}
	if _, _, err := cp.appendTaskEntry(ctx, taskEntrySpec{TaskID: taskID, Origin: "caller", Body: "Still monitoring."}); err != nil {
		t.Fatalf("append while verifying: %v", err)
	}
	if _, err := svc.internalStoreMutationRows(ctx, "tasks", `where: { id: { eq: $id } }, update: $input`, taskStoreFields, map[string]any{
		"id": taskID, "input": map[string]any{"verify_after": time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)},
	}); err != nil {
		t.Fatalf("force verification due: %v", err)
	}
	completed, err := svc.sweepTaskVerifications(context.Background(), time.Now().UTC())
	if err != nil || completed != 1 {
		t.Fatalf("verification sweep completed=%d err=%v", completed, err)
	}
	stored, err := svc.internalTaskStoreRow(ctx, taskID)
	if err != nil || taskStatus(stringMapValue(stored, "status")) != "closed" || stringMapValue(stored, "verify_status") != "verified" || stringMapValue(stored, "closed_at") == "" {
		t.Fatalf("swept task = %+v err=%v", stored, err)
	}
	assertTaskVerificationEntries(t, svc, ctx, taskID, 1, "passed")
	if completed, err := svc.sweepTaskVerifications(context.Background(), time.Now().UTC().Add(time.Hour)); err != nil || completed != 0 {
		t.Fatalf("repeat sweep completed=%d err=%v", completed, err)
	}
	assertTaskVerificationEntries(t, svc, ctx, taskID, 1, "passed")

	second := insertTaskForTest(t, cp, ctx, "Cancel a scheduled verification")
	secondID := fmt.Sprint(second["id"])
	if _, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table: tasksRootTable, Operation: "update", Where: map[string]any{"id": secondID},
		Input: map[string]any{
			"status": "closed", "outcome": "Wait before closing.",
			"verify_json": map[string]any{"saved_query_name": "delayed_late_orders", "expect": map[string]any{"path": "orders", "op": "empty"}, "recheck": "2h"},
		},
	}); err != nil {
		t.Fatalf("schedule second verification: %v", err)
	}
	reopened, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table: tasksRootTable, Operation: "update", Where: map[string]any{"id": secondID}, Input: map[string]any{"status": "open"},
	})
	if err != nil || reopened["status"] != "open" || reopened["verify_status"] != "cancelled" || reopened["verify_after"] != "" {
		t.Fatalf("cancelled verification = %+v err=%v", reopened, err)
	}

	failedTask := insertTaskForTest(t, cp, ctx, "Reopen after a delayed failed check")
	failedID := fmt.Sprint(failedTask["id"])
	if _, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table: tasksRootTable, Operation: "update", Where: map[string]any{"id": failedID},
		Input: map[string]any{
			"status": "closed", "outcome": "Late orders still exist.",
			"verify_json": map[string]any{"saved_query_name": "delayed_late_orders", "expect": map[string]any{"path": "orders", "op": "not_empty"}, "recheck": "1m"},
		},
	}); err != nil {
		t.Fatalf("schedule failing verification: %v", err)
	}
	if _, err := svc.internalStoreMutationRows(ctx, "tasks", `where: { id: { eq: $id } }, update: $input`, taskStoreFields, map[string]any{
		"id": failedID, "input": map[string]any{"verify_after": time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)},
	}); err != nil {
		t.Fatalf("force failing verification due: %v", err)
	}
	if completed, err := svc.sweepTaskVerifications(context.Background(), time.Now().UTC()); err != nil || completed != 1 {
		t.Fatalf("failing verification sweep completed=%d err=%v", completed, err)
	}
	failedStored, err := svc.internalTaskStoreRow(ctx, failedID)
	if err != nil || taskStatus(stringMapValue(failedStored, "status")) != "open" || stringMapValue(failedStored, "verify_status") != "failed" || stringMapValue(failedStored, "outcome") != "" {
		t.Fatalf("failed swept task = %+v err=%v", failedStored, err)
	}
	var resp gjagent.Response
	svc.appendTaskNotices(ctx, gjagent.Request{}, &resp)
	if notice, ok := taskNoticeForKind(resp, "task_verify_failed"); !ok || notice.Count != 1 || notice.TaskIDs[0] != failedID {
		t.Fatalf("delayed failed verification notice = %+v", resp.Notices)
	}
}

func TestTaskVerificationClaimIsSingleWinnerAcrossReplicas(t *testing.T) {
	dsn := "file:" + t.TempDir() + "/task-verify-replicas.db"
	db1, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db2, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	for _, db := range []*sql.DB{db1, db2} {
		db.SetMaxOpenConns(1)
		if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
			t.Fatal(err)
		}
	}
	configure := func(conf *Config) {
		conf.Core.Tasks = core.TasksConfig{Enabled: true, MaxPerOwner: 5, MaxEntriesPerTask: 20, EntryRetentionHours: 168, SnapshotMaxBytes: 32768}
	}
	_, first := newSQLiteWatchServiceWithDBAndOptions(t, db1, dsn, 10, configure)
	if err := first.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("init first replica: %v", err)
	}
	startSQLiteWatchCore(t, first, db1)
	_, second := newSQLiteWatchServiceWithDBAndOptions(t, db2, dsn, 10, configure)
	if err := second.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("init second replica: %v", err)
	}
	startSQLiteWatchCore(t, second, db2)

	ctx := contextWithUserRole(artifactUserCtx("replica_owner"), "analyst")
	saveTaskVerificationQuery(t, first, ctx, "replica_late_orders")
	cp := newTaskControlPlane(first)
	task := insertTaskForTest(t, cp, ctx, "Only one replica records verification")
	taskID := fmt.Sprint(task["id"])
	if _, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table: tasksRootTable, Operation: "update", Where: map[string]any{"id": taskID},
		Input: map[string]any{
			"status": "closed", "outcome": "No late orders remain.",
			"verify_json": map[string]any{"saved_query_name": "replica_late_orders", "expect": map[string]any{"path": "orders", "op": "empty"}, "recheck": "1m"},
		},
	}); err != nil {
		t.Fatalf("schedule replica verification: %v", err)
	}
	if _, err := first.internalStoreMutationRows(ctx, "tasks", `where: { id: { eq: $id } }, update: $input`, taskStoreFields, map[string]any{
		"id": taskID, "input": map[string]any{"verify_after": time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)},
	}); err != nil {
		t.Fatalf("force replica verification due: %v", err)
	}

	type sweepResult struct {
		completed int
		err       error
	}
	results := make(chan sweepResult, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, svc := range []*graphjinService{first, second} {
		wg.Add(1)
		go func(svc *graphjinService) {
			defer wg.Done()
			<-start
			completed, err := svc.sweepTaskVerifications(context.Background(), time.Now().UTC())
			results <- sweepResult{completed: completed, err: err}
		}(svc)
	}
	close(start)
	wg.Wait()
	close(results)
	total := 0
	for result := range results {
		if result.err != nil {
			t.Fatalf("replica sweep: %v", result.err)
		}
		total += result.completed
	}
	if total != 1 {
		t.Fatalf("replica verification completions = %d, want 1", total)
	}
	assertTaskVerificationEntries(t, first, ctx, taskID, 1, "passed")
}

func TestTaskVerificationReclaimsStuckClaim(t *testing.T) {
	svc, cp := newSQLiteTaskService(t, 5, 20)
	ctx := artifactUserCtx("stuck_claim_owner")
	saveTaskVerificationQuery(t, svc, ctx, "stuck_claim_orders")
	task := insertTaskForTest(t, cp, ctx, "Recover a crashed verifier")
	taskID := fmt.Sprint(task["id"])
	if _, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table: tasksRootTable, Operation: "update", Where: map[string]any{"id": taskID},
		Input: map[string]any{
			"status": "closed", "outcome": "No late orders remain.",
			"verify_json": map[string]any{"saved_query_name": "stuck_claim_orders", "expect": map[string]any{"path": "orders", "op": "empty"}, "recheck": "1m"},
		},
	}); err != nil {
		t.Fatalf("schedule verification: %v", err)
	}
	stale := time.Now().UTC().Add(-taskVerifyClaimTimeout - time.Minute)
	if _, err := svc.internalStoreMutationRows(ctx, "tasks", `where: { id: { eq: $id } }, update: $input`, taskStoreFields, map[string]any{
		"id": taskID,
		"input": map[string]any{
			"verify_status": "claimed:crashed-replica", "verify_after": stale.Format(time.RFC3339Nano), "updated_at": stale.Format(time.RFC3339Nano),
		},
	}); err != nil {
		t.Fatalf("seed stuck claim: %v", err)
	}
	completed, err := svc.sweepTaskVerifications(context.Background(), time.Now().UTC())
	if err != nil || completed != 1 {
		t.Fatalf("stuck claim sweep completed=%d err=%v", completed, err)
	}
	stored, err := svc.internalTaskStoreRow(ctx, taskID)
	if err != nil || taskStatus(stringMapValue(stored, "status")) != "closed" || stringMapValue(stored, "verify_status") != "verified" {
		t.Fatalf("reclaimed task = %+v err=%v", stored, err)
	}
	assertTaskVerificationEntries(t, svc, ctx, taskID, 1, "passed")
}

func TestTaskVerifyJSONSemanticRejects(t *testing.T) {
	svc, cp := newSQLiteTaskService(t, 5, 20)
	ctx := artifactUserCtx("semantic_owner")
	task := insertTaskForTest(t, cp, ctx, "Reject unsafe verification specs")
	taskID := fmt.Sprint(task["id"])
	for _, verify := range []any{
		map[string]any{"saved_query_name": "missing", "expect": map[string]any{"path": "orders", "op": "empty"}},
		map[string]any{"saved_query_name": "missing", "expect": map[string]any{"path": "orders.*", "op": "empty"}},
		map[string]any{"saved_query_name": "missing", "expect": map[string]any{"path": "orders", "op": "empty"}, "condition_js": "true"},
		map[string]any{"saved_query_name": "missing", "query": "query { orders { id } }", "expect": map[string]any{"path": "orders", "op": "empty"}},
	} {
		_, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
			Table: tasksRootTable, Operation: "update", Where: map[string]any{"id": taskID}, Input: map[string]any{"verify_json": verify},
		})
		if err == nil || !strings.Contains(err.Error(), "verify_json") {
			t.Fatalf("unsafe verify_json accepted: %#v err=%v", verify, err)
		}
	}
	if _, err := svc.saveUserArtifact(ctx, artifactKindSavedQuery, "unsafe_mutation", `mutation unsafe_mutation { orders(delete: true, where: { id: { eq: -1 } }) { id } }`, map[string]any{"operation": "mutation"}); err != nil {
		t.Fatalf("save mutation artifact: %v", err)
	}
	if _, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table: tasksRootTable, Operation: "update", Where: map[string]any{"id": taskID},
		Input: map[string]any{"verify_json": map[string]any{"saved_query_name": "unsafe_mutation", "expect": map[string]any{"path": "orders", "op": "empty"}}},
	}); err == nil || !strings.Contains(err.Error(), "must resolve to a query") {
		t.Fatalf("mutation verification query error = %v", err)
	}
	if _, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table: tasksRootTable, Operation: "update", Where: map[string]any{"id": taskID}, Input: map[string]any{"status": "verifying"},
	}); err == nil || !strings.Contains(err.Error(), "server-managed") {
		t.Fatalf("caller-selected verifying status error = %v", err)
	}
	degraded := &graphjinService{conf: svc.conf}
	if _, _, err := degraded.normalizeTaskVerifyJSON(ctx, `{"saved_query_name":"missing","expect":{"path":"orders","op":"empty"}}`, 32768); err == nil || !strings.Contains(err.Error(), "engine is not initialized") {
		t.Fatalf("degraded verification validation error = %v", err)
	}
}

func TestTaskVerificationCapabilityRows(t *testing.T) {
	svc, _ := newSQLiteTaskService(t, 5, 20)
	rows := newControlPlaneGraphQL(svc).systemCapabilityRows()
	var taskRow, entryRow map[string]any
	for _, row := range rows {
		switch stringFromAny(row["name"]) {
		case "gj_task.insert_update_delete":
			taskRow = row
		case "gj_task_entry.insert":
			entryRow = row
		}
	}
	taskText := mustMarshalString(taskRow)
	for _, want := range []string{"verifying", "verify_json", "saved_query_name", "verification_is_server_run"} {
		if !strings.Contains(taskText, want) {
			t.Fatalf("task capability row missing %q: %s", want, taskText)
		}
	}
	entryText := mustMarshalString(entryRow)
	for _, want := range []string{"open or verifying", "server_managed"} {
		if !strings.Contains(entryText, want) {
			t.Fatalf("task entry capability row missing %q: %s", want, entryText)
		}
	}
}

func assertTaskVerificationEntries(t *testing.T, svc *graphjinService, ctx context.Context, taskID string, want int, status string) {
	t.Helper()
	rows, err := svc.internalStoreAllRows(ctx, "task_entries", `where: { task_id: { eq: $task_id }, origin: { eq: "verification" } }`, taskEntryStoreFields, map[string]any{"task_id": taskID})
	if err != nil || len(rows) != want {
		t.Fatalf("verification entries for %s = %+v err=%v", taskID, rows, err)
	}
	if want != 0 && stringMapValue(rows[0], "status") != status {
		t.Fatalf("verification entry status = %+v, want %s", rows[0], status)
	}
}
