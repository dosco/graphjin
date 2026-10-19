package serv

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/dosco/graphjin/core/v3"
)

func newSQLiteTaskService(t *testing.T, maxPerOwner, maxEntries int) (*graphjinService, taskControlPlane) {
	t.Helper()
	_, svc := newSQLiteWatchServiceWithOptions(t, 10, func(conf *Config) {
		conf.Core.Tasks = core.TasksConfig{
			Enabled:             true,
			MaxPerOwner:         maxPerOwner,
			MaxEntriesPerTask:   maxEntries,
			EntryRetentionHours: 168,
			SnapshotMaxBytes:    32768,
		}
	})
	db := svc.dbs["app"]
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("initArtifactsBeforeCore: %v", err)
	}
	startSQLiteWatchCore(t, svc, db)
	return svc, newTaskControlPlane(svc)
}

func insertTaskForTest(t *testing.T, cp taskControlPlane, ctx context.Context, goal string) map[string]any {
	t.Helper()
	row, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table: tasksRootTable, Operation: "insert", Input: map[string]any{"goal": goal},
	})
	if err != nil {
		t.Fatalf("insert task: %v", err)
	}
	return row
}

func TestTaskControlPlaneLifecycleScopeAndTrail(t *testing.T) {
	svc, cp := newSQLiteTaskService(t, 1, 20)
	db := svc.dbs["app"]
	if !sqliteTableExists(t, db, "_graphjin_tasks") || !sqliteTableExists(t, db, "_graphjin_task_entries") {
		t.Fatal("task store tables were not initialized")
	}
	ctx := contextWithUserRole(artifactUserCtx("user_1"), "analyst")
	other := contextWithUserRole(artifactUserCtx("user_2"), "analyst")
	row := insertTaskForTest(t, cp, ctx, "Investigate late purchase orders")
	taskID := fmt.Sprint(row["id"])
	if !strings.HasPrefix(taskID, "task:") || row["status"] != "open" || row["last_entry_at"] != "" {
		t.Fatalf("unexpected task create row: %+v", row)
	}
	if _, leaked := row["owner_id"]; leaked || !strings.HasPrefix(fmt.Sprint(row["owner_ref"]), "sha256:") {
		t.Fatalf("task row leaked raw owner identity: %+v", row)
	}
	if _, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table: tasksRootTable, Operation: "insert", Input: map[string]any{"goal": "Second open task"},
	}); err == nil || !strings.Contains(err.Error(), "max_per_owner") {
		t.Fatalf("second open task should hit max_per_owner, got %v", err)
	}
	if rows, err := cp.taskRowsWithScope(other, false); err != nil || len(rows) != 0 {
		t.Fatalf("foreign task read = %+v, err=%v", rows, err)
	}

	entry, inserted, err := cp.appendTaskEntry(ctx, taskEntrySpec{TaskID: taskID, Origin: "caller", Body: "Confirmed the supplier feed is current."})
	if err != nil || !inserted || !strings.HasPrefix(fmt.Sprint(entry["id"]), "te:") || entry["origin"] != "caller" {
		t.Fatalf("caller entry = %+v inserted=%v err=%v", entry, inserted, err)
	}
	if _, _, err := cp.appendTaskEntry(other, taskEntrySpec{TaskID: taskID, Origin: "caller", Body: "foreign"}); !errors.Is(err, errTaskNotFoundOrClosed) {
		t.Fatalf("foreign append error = %v", err)
	}

	closed, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table: tasksRootTable, Operation: "update", Where: map[string]any{"id": taskID},
		Input: map[string]any{"status": "closed", "outcome": "Supplier delay identified."},
	})
	if err != nil || closed["status"] != "closed" || closed["closed_at"] == "" {
		t.Fatalf("close task = %+v err=%v", closed, err)
	}
	if _, _, err := cp.appendTaskEntry(ctx, taskEntrySpec{TaskID: taskID, Origin: "caller", Body: "late note"}); !errors.Is(err, errTaskNotFoundOrClosed) {
		t.Fatalf("closed append error = %v", err)
	}
	reopened, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table: tasksRootTable, Operation: "update", Where: map[string]any{"id": taskID},
		Input: map[string]any{"status": "open"},
	})
	if err != nil || reopened["status"] != "open" || reopened["outcome"] != "" || reopened["closed_at"] != "" {
		t.Fatalf("reopen task = %+v err=%v", reopened, err)
	}

	if _, err := cp.mutateRow(other, core.ManagedMutationRoot{
		Table: tasksRootTable, Operation: "delete", Where: map[string]any{"id": taskID},
	}); err != nil {
		t.Fatalf("foreign benign delete: %v", err)
	}
	if stored, err := svc.internalTaskStoreRow(ctx, taskID); err != nil || stored == nil {
		t.Fatalf("foreign delete removed task: row=%+v err=%v", stored, err)
	}
	if _, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table: tasksRootTable, Operation: "delete", Where: map[string]any{"id": taskID},
	}); err != nil {
		t.Fatalf("owner delete: %v", err)
	}
	if stored, err := svc.internalTaskStoreRow(ctx, taskID); err != nil || stored != nil {
		t.Fatalf("task remained after delete: row=%+v err=%v", stored, err)
	}
	entries, err := svc.internalStoreAllRows(ctx, "task_entries", `where: { task_id: { eq: $task_id } }`, taskEntryStoreFields, map[string]any{"task_id": taskID})
	if err != nil || len(entries) != 0 {
		t.Fatalf("task entries remained after cascade: %+v err=%v", entries, err)
	}
}

func TestTaskAgentTrailDedupesAndWarmStarts(t *testing.T) {
	svc, cp := newSQLiteTaskService(t, 5, 20)
	const secretEnv = "GRAPHJIN_TASK_TRAIL_PROVIDER_SECRET"
	const secret = "AIza-task-canary-secret-1234567890"
	t.Setenv(secretEnv, secret)
	svc.conf.Agent.Provider = "google-gemini"
	svc.conf.Agent.APIKeyEnv = secretEnv
	ctx := artifactUserCtx("user_1")
	task := insertTaskForTest(t, cp, ctx, "Explain the weekly order backlog")
	taskID := fmt.Sprint(task["id"])
	for i := 0; i < 6; i++ {
		if _, _, err := cp.appendTaskEntry(ctx, taskEntrySpec{
			TaskID: taskID, Origin: "caller", Body: fmt.Sprintf("journal %d", i),
		}); err != nil {
			t.Fatalf("append journal %d: %v", i, err)
		}
	}
	resp := gjagent.Response{
		Status: gjagent.StatusAnswered, Answer: "There are three delayed orders.",
		TraceID: "trace-task-1", Skills: []gjagent.SkillUsage{{ID: "data_discovery"}},
		Evidence: map[string]any{"catalog_detail_ids": []any{"table:app:main.orders", "table:app:main.orders"}},
	}
	svc.appendTaskTrailEntry(ctx, gjagent.Request{TaskID: taskID}, resp, 125*time.Millisecond, nil)
	svc.appendTaskTrailEntry(ctx, gjagent.Request{TaskID: taskID}, resp, 125*time.Millisecond, nil)
	rows, err := svc.internalStoreAllRows(ctx, "task_entries", `where: { task_id: { eq: $task_id }, trace_id: { eq: $trace_id } }`, taskEntryStoreFields, map[string]any{"task_id": taskID, "trace_id": resp.TraceID})
	if err != nil || len(rows) != 1 {
		t.Fatalf("agent trail trace dedupe rows=%d err=%v: %+v", len(rows), err, rows)
	}
	leaky := gjagent.Response{Status: gjagent.StatusError, TraceID: "trace-secret", Errors: []gjagent.ErrorInfo{{Message: "Authorization: Bearer " + secret, Extensions: map[string]any{"code": gjagent.ErrorCodeProviderTimeout, "retryable": true}}}, Actions: []any{map[string]any{"error": "https://provider.test/generate?key=" + secret}}}
	svc.appendTaskTrailEntry(ctx, gjagent.Request{TaskID: taskID}, leaky, time.Millisecond, fmt.Errorf("provider request failed with %s", secret))
	secretRows, err := svc.internalStoreAllRows(ctx, "task_entries", `where: { task_id: { eq: $task_id }, trace_id: { eq: $trace_id } }`, taskEntryStoreFields, map[string]any{"task_id": taskID, "trace_id": leaky.TraceID})
	if err != nil || len(secretRows) != 1 {
		t.Fatalf("secret trail rows=%d err=%v", len(secretRows), err)
	}
	if strings.Contains(fmt.Sprint(secretRows[0]), secret) {
		t.Fatalf("task trail leaked provider credential: %+v", secretRows[0])
	}
	if !strings.Contains(fmt.Sprint(secretRows[0]), gjagent.ErrorCodeProviderTimeout) {
		t.Fatalf("task trail lost stable provider code: %+v", secretRows[0])
	}

	req := gjagent.Request{
		TaskID:  taskID,
		History: []gjagent.Turn{{Role: "user", Content: "What changed since then?"}},
	}
	warmStart, err := svc.resolveAgentTaskContext(ctx, &req)
	if err != nil {
		t.Fatalf("resolve task context: %v", err)
	}
	if warmStart.TaskID != taskID || warmStart.EntriesLoaded != 5 || warmStart.CatalogHints != 1 {
		t.Fatalf("warm-start metadata = %+v", warmStart)
	}
	if len(req.History) != 7 || req.History[0].Content != "Declared task goal: Explain the weekly order backlog" || req.History[len(req.History)-1].Content != "What changed since then?" {
		t.Fatalf("unexpected synthesized history: %+v", req.History)
	}
	var agentTurn *gjagent.Turn
	for i := range req.History {
		if strings.Contains(req.History[i].Content, "three delayed orders") {
			agentTurn = &req.History[i]
			break
		}
	}
	if agentTurn == nil || !reflect.DeepEqual(agentTurn.CatalogIDs, []string{"table:app:main.orders"}) {
		t.Fatalf("agent trail catalog hints were not restored: %+v", agentTurn)
	}
	if _, err := svc.resolveAgentTaskContext(artifactUserCtx("user_2"), &gjagent.Request{TaskID: taskID}); !errors.Is(err, errTaskNotFoundOrClosed) {
		t.Fatalf("foreign warm start error = %v", err)
	}
}

func TestTaskWatchLinkJournalsAndDeleteUnlinks(t *testing.T) {
	svc, taskCP := newSQLiteTaskService(t, 5, 20)
	ctx := contextWithUserRole(artifactUserCtx("user_1"), "analyst")
	task := insertTaskForTest(t, taskCP, ctx, "Monitor late purchase orders")
	taskID := fmt.Sprint(task["id"])
	watchCP := newWatchControlPlane(svc)
	watch, err := watchCP.mutateRow(ctx, core.ManagedMutationRoot{
		Table: watchesRootTable, Operation: "insert",
		Input: map[string]any{
			"name": "late_orders_for_task", "task_id": taskID,
			"query": cursorOrdersWatchQuery("late_orders_for_task"),
		},
	})
	if err != nil {
		t.Fatalf("insert task-linked watch: %v", err)
	}
	watchID := fmt.Sprint(watch["id"])
	if watch["task_id"] != taskID {
		t.Fatalf("watch task_id = %v, want %s: %+v", watch["task_id"], taskID, watch)
	}
	entries, err := svc.internalStoreAllRows(ctx, "task_entries", `where: { task_id: { eq: $task_id }, watch_id: { eq: $watch_id } }`, taskEntryStoreFields, map[string]any{"task_id": taskID, "watch_id": watchID})
	if err != nil || len(entries) != 1 || stringMapValue(entries[0], "origin") != "watch_created" {
		t.Fatalf("watch-created trail = %+v err=%v", entries, err)
	}
	if _, err := watchCP.mutateRow(contextWithUserRole(artifactUserCtx("user_2"), "analyst"), core.ManagedMutationRoot{
		Table: watchesRootTable, Operation: "insert",
		Input: map[string]any{"name": "foreign_task_watch", "task_id": taskID, "query": cursorOrdersWatchQuery("foreign_task_watch")},
	}); !errors.Is(err, errTaskNotFoundOrClosed) {
		t.Fatalf("foreign task link error = %v", err)
	}
	if _, err := taskCP.mutateRow(ctx, core.ManagedMutationRoot{
		Table: tasksRootTable, Operation: "update", Where: map[string]any{"id": taskID},
		Input: map[string]any{"status": "closed", "outcome": "Monitoring configured."},
	}); err != nil {
		t.Fatalf("close linked task: %v", err)
	}
	if _, err := watchCP.mutateRow(ctx, core.ManagedMutationRoot{
		Table: watchesRootTable, Operation: "update", Where: map[string]any{"id": watchID},
		Input: map[string]any{
			"name": "late_orders_for_task", "query": cursorOrdersWatchQuery("late_orders_for_task"),
			"description": "Keep the historical task link.",
		},
	}); err != nil {
		t.Fatalf("update watch with retained closed-task link: %v", err)
	}
	if _, err := taskCP.mutateRow(ctx, core.ManagedMutationRoot{
		Table: tasksRootTable, Operation: "delete", Where: map[string]any{"id": taskID},
	}); err != nil {
		t.Fatalf("delete linked task: %v", err)
	}
	stored, err := svc.internalWatchStoreRow(ctx, watchID)
	if err != nil || stored == nil || stringMapValue(stored, "task_id") != "" {
		t.Fatalf("task delete did not unlink watch: %+v err=%v", stored, err)
	}
}

func TestWatchRejectsTaskRootSubscriptions(t *testing.T) {
	for _, table := range []string{tasksRootTable, taskEntriesRootTable} {
		if _, err := watchedWatchIDsForRoots([]core.SubscriptionRootInfo{{Table: table}}, "watch:self"); err == nil || !strings.Contains(err.Error(), "not supported") {
			t.Fatalf("task-root watch %s error = %v", table, err)
		}
	}
}

func TestTaskEntryProjectionTrimAndColumnDrift(t *testing.T) {
	now := time.Now().UTC()
	rows := []map[string]any{
		{"id": "new-a", "task_id": "a", "created_at": now.Format(time.RFC3339Nano)},
		{"id": "old-a", "task_id": "a", "created_at": now.Add(-2 * time.Hour).Format(time.RFC3339Nano)},
		{"id": "new-b", "task_id": "b", "created_at": now.Format(time.RFC3339Nano)},
	}
	trimmed := trimTaskEntryProjectionRows(rows, core.TasksConfig{MaxEntriesPerTask: 1, EntryRetentionHours: 1})
	if len(trimmed) != 2 {
		t.Fatalf("trimmed task entries = %+v", trimmed)
	}

	managedNames := make([]string, 0, len(taskColumns()))
	for _, column := range taskColumns() {
		managedNames = append(managedNames, column.Name)
	}
	nanoNames := make([]string, 0, len(taskNanoColumns()))
	for _, column := range taskNanoColumns() {
		if column.Name != "deleted" && column.Name != "search_vector" {
			nanoNames = append(nanoNames, column.Name)
		}
	}
	sort.Strings(managedNames)
	sort.Strings(nanoNames)
	if !reflect.DeepEqual(managedNames, nanoNames) {
		t.Fatalf("gj_task managed/nano column drift: managed=%v nano=%v", managedNames, nanoNames)
	}
}

func TestTaskSchemaMigrationAddsTrailColumns(t *testing.T) {
	dsn := fmt.Sprintf("file:task-migration-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE "_graphjin_tasks" (id TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE "_graphjin_task_entries" (id TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if err := (&graphjinService{}).migrateTaskSchema(context.Background(), db, "sqlite", "_graphjin"); err != nil {
		t.Fatalf("migrate task schema: %v", err)
	}
	for table, columns := range map[string][]string{
		"_graphjin_tasks":        {"outcome", "owner_role", "last_entry_at", "closed_at", "verify_json", "verify_status", "verify_after", "verify_attempts"},
		"_graphjin_task_entries": {"detail_json", "status", "trace_id", "watch_id"},
	} {
		for _, column := range columns {
			ok, err := sqliteColumnExists(context.Background(), db, quoteSQLIdent(table), column)
			if err != nil {
				t.Fatalf("inspect %s.%s: %v", table, column, err)
			}
			if !ok {
				t.Fatalf("expected migration to add %s.%s", table, column)
			}
		}
	}
	var verifyIndexCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_graphjin_tasks_verify_due'`).Scan(&verifyIndexCount); err != nil {
		t.Fatalf("inspect verification index: %v", err)
	}
	if verifyIndexCount != 1 {
		t.Fatalf("verification due index count = %d, want 1", verifyIndexCount)
	}
}

func TestTaskRejectsServerManagedInputs(t *testing.T) {
	_, cp := newSQLiteTaskService(t, 5, 20)
	ctx := artifactUserCtx("user_1")
	for _, input := range []map[string]any{
		{"id": "task:caller-selected", "goal": "Caller-selected id"},
		{"goal": "Spoof owner ref", "owner_ref": "sha256:spoofed"},
		{"goal": "Spoof verify state", "verify_status": "verified"},
		{"goal": "Spoof verify schedule", "verify_after": time.Now().UTC().Format(time.RFC3339)},
		{"goal": "Spoof verify attempt", "verify_attempts": 9},
	} {
		if _, err := cp.mutateRow(ctx, core.ManagedMutationRoot{Table: tasksRootTable, Operation: "insert", Input: input}); err == nil || !strings.Contains(err.Error(), "server-managed") {
			t.Fatalf("task server-managed input %+v error = %v", input, err)
		}
	}
	task := insertTaskForTest(t, cp, ctx, "Journal safely")
	if _, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table: taskEntriesRootTable, Operation: "insert",
		Input: map[string]any{"task_id": task["id"], "body": "note", "origin": "agent_run"},
	}); err == nil || !strings.Contains(err.Error(), "server-managed") {
		t.Fatalf("task-entry provenance spoof error = %v", err)
	}
}
