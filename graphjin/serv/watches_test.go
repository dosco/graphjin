package serv

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/dosco/graphjin/core/v3"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/spf13/afero"
	_ "modernc.org/sqlite"
)

func cursorOrdersWatchQuery(name string) string {
	if name == "" {
		name = "orders_watch"
	}
	return `subscription ` + name + ` { orders(first: 25, after: $cursor) { id status } orders_cursor }`
}

func TestWatchControlPlaneInitializesScopesAndUpdatesEvents(t *testing.T) {
	db, svc := newSQLiteWatchService(t, 1)
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("initArtifactsBeforeCore: %v", err)
	}
	startSQLiteWatchCore(t, svc, db)
	if !sqliteTableExists(t, db, "_graphjin_watches") {
		t.Fatal("expected watch table to be created")
	}
	if !sqliteTableExists(t, db, "_graphjin_watch_events") {
		t.Fatal("expected watch event table to be created")
	}
	if ok, err := sqliteColumnExists(artifactUserCtx("user_1"), db, quoteSQLIdent("_graphjin_watches"), "owner_role"); err != nil {
		t.Fatalf("check watch owner_role column: %v", err)
	} else if !ok {
		t.Fatal("expected watch owner_role column to be created")
	}
	if ok, err := sqliteColumnExists(artifactUserCtx("user_1"), db, quoteSQLIdent("_graphjin_watches"), "last_cursor_json"); err != nil {
		t.Fatalf("check watch last_cursor_json column: %v", err)
	} else if !ok {
		t.Fatal("expected watch last_cursor_json column to be created")
	}

	cp := newWatchControlPlane(svc)
	ctx := contextWithUserRole(artifactUserCtx("user_1"), "analyst")
	row, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table:     watchesRootTable,
		Operation: "insert",
		Input: map[string]interface{}{
			"name":       "new_orders",
			"owner_role": "admin",
			"query":      cursorOrdersWatchQuery("new_orders_watch"),
		},
	})
	if err != nil {
		t.Fatalf("insert watch: %v", err)
	}
	watchID, _ := row["id"].(string)
	if watchID == "" {
		t.Fatalf("watch insert returned no id: %+v", row)
	}
	if row["status"] != "active" || row["enabled"] != true || row["approval"] != watchReviewApproved ||
		row["flow_approval"] != watchReviewNotRequired || row["action_approval"] != watchReviewNotRequired {
		t.Fatalf("plain notification watch should be immediately active: %+v", row)
	}
	if row["owner_id"] == "user_1" || !strings.HasPrefix(row["owner_ref"].(string), "sha256:") {
		t.Fatalf("watch row leaked owner identity: %+v", row)
	}
	var storedRole string
	if err := db.QueryRow(`SELECT owner_role FROM "_graphjin_watches" WHERE id = ?`, watchID).Scan(&storedRole); err != nil {
		t.Fatalf("query stored owner role: %v", err)
	}
	if storedRole != "analyst" {
		t.Fatalf("stored owner_role = %q, want trusted context role analyst", storedRole)
	}
	projectionRows, err := cp.allWatchRowsForProjection(contextWithUserRole(artifactUserCtx("admin_1"), "admin"))
	if err != nil {
		t.Fatalf("allWatchRowsForProjection: %v", err)
	}
	if len(projectionRows) != 1 || projectionRows[0]["owner_role"] != "analyst" {
		t.Fatalf("projection should carry trusted owner role, got %+v", projectionRows)
	}
	if got := sqliteRevision(t, db, "watches"); got != 1 {
		t.Fatalf("watch revision after insert = %d, want 1", got)
	}

	rows, err := cp.watchRows(ctx)
	if err != nil {
		t.Fatalf("watchRows: %v", err)
	}
	if len(rows) != 1 || rows[0]["id"] != watchID {
		t.Fatalf("owner should see own watch, got %+v", rows)
	}
	otherCtx := artifactUserCtx("user_2")
	rows, err = cp.watchRows(otherCtx)
	if err != nil {
		t.Fatalf("watchRows other user: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("other user should not see watch rows, got %+v", rows)
	}

	if _, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table:     watchesRootTable,
		Operation: "insert",
		Input: map[string]interface{}{
			"name":  "not_a_subscription",
			"query": `query { orders { id } }`,
		},
	}); err == nil || !strings.Contains(err.Error(), "must be a subscription") {
		t.Fatalf("non-subscription watch error = %v", err)
	}
	if _, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table:     watchesRootTable,
		Operation: "insert",
		Input: map[string]interface{}{
			"name":  "not_cursor_backed",
			"query": `subscription not_cursor_backed { orders { id } }`,
		},
	}); err == nil || !strings.Contains(err.Error(), "cursor pagination") {
		t.Fatalf("non-cursor subscription watch error = %v", err)
	}
	if _, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table:     watchesRootTable,
		Operation: "insert",
		Input: map[string]interface{}{
			"name":  "second_watch",
			"query": cursorOrdersWatchQuery("second_watch"),
		},
	}); err == nil || !strings.Contains(err.Error(), "max_per_owner") {
		t.Fatalf("second watch should exceed max_per_owner, got %v", err)
	}
	if _, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table:     watchesRootTable,
		Operation: "update",
		Input: map[string]interface{}{
			"name":        "new_orders",
			"description": "same watch, new description",
			"query":       cursorOrdersWatchQuery("updated_orders_watch"),
		},
	}); err != nil {
		t.Fatalf("update existing watch should not trip max_per_owner: %v", err)
	}

	_, _, _, eventTable, ok := svc.watchDB()
	if !ok {
		t.Fatal("watch DB not configured")
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO `+eventTable+` (id, watch_id, data_hash, data_json, evidence_json, delivery_json, receipt_json, enrichment_json, account_id, owner_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"evt_1", watchID, "hash_1", `{"order_id":1}`, `{}`, `{}`, `{}`, `{}`, "acct_1", "user_1"); err != nil {
		t.Fatalf("insert watch event fixture: %v", err)
	}
	eventRows, err := cp.watchEventRows(ctx)
	if err != nil {
		t.Fatalf("watchEventRows: %v", err)
	}
	if len(eventRows) != 1 || eventRows[0]["id"] != "evt_1" {
		t.Fatalf("owner should see own event, got %+v", eventRows)
	}
	eventRows, err = cp.watchEventRows(otherCtx)
	if err != nil {
		t.Fatalf("watchEventRows other user: %v", err)
	}
	if len(eventRows) != 0 {
		t.Fatalf("other user should not see event rows, got %+v", eventRows)
	}
	if _, err := cp.mutateRow(otherCtx, core.ManagedMutationRoot{
		Table:     watchEventsRootTable,
		Operation: "update",
		Where:     map[string]interface{}{"id": "evt_1"},
		Input:     map[string]interface{}{"seen": true},
	}); err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("cross-user event update should be denied, got %v", err)
	}
	updated, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table:     watchEventsRootTable,
		Operation: "update",
		Where:     map[string]interface{}{"id": "evt_1"},
		Input:     map[string]interface{}{"seen": true},
	})
	if err != nil {
		t.Fatalf("owner event update: %v", err)
	}
	if updated["seen"] != true || updated["seen_at"] == nil {
		t.Fatalf("unexpected event update row: %+v", updated)
	}
	if got := sqliteRevision(t, db, "watch_events"); got != 1 {
		t.Fatalf("watch event revision after update = %d, want 1", got)
	}
}

func TestAssertWatchNanoRoleDefaultsFailsClosed(t *testing.T) {
	conf := &Config{Core: core.Config{
		Mode:      "agentic",
		Artifacts: core.ArtifactsConfig{Enabled: true},
		Watches:   core.WatchesConfig{Enabled: true},
	}}
	filters := []string{`{ owner_ref: { eq: $user_ref } }`}
	watchCols := watchPublicProjectionColumns()
	eventCols := watchEventPublicProjectionColumns()
	runtimeCore := func(watchColumns, eventColumns []string, watchFilters, eventFilters []string) *core.Config {
		return &core.Config{Roles: []core.Role{{
			Name: "user",
			Tables: []core.RoleTable{
				{
					Database: "graphjin",
					Name:     watchesRootTable,
					Query:    &core.Query{Filters: watchFilters, Columns: watchColumns},
				},
				{
					Database: "graphjin",
					Name:     watchEventsRootTable,
					Query:    &core.Query{Filters: eventFilters, Columns: eventColumns},
				},
			},
		}}}
	}

	if err := assertWatchNanoRoleDefaults(conf, runtimeCore(watchCols, eventCols, filters, filters), "graphjin"); err != nil {
		t.Fatalf("valid watch projection defaults rejected: %v", err)
	}
	if err := assertWatchNanoRoleDefaults(conf, runtimeCore(watchCols, eventCols, nil, filters), "graphjin"); err == nil {
		t.Fatal("missing watch owner filter should fail closed")
	}
	if err := assertWatchNanoRoleDefaults(conf, runtimeCore(watchCols, eventCols, filters, nil), "graphjin"); err == nil {
		t.Fatal("missing watch event owner filter should fail closed")
	}
	leakyWatchCols := append(append([]string{}, watchCols...), "owner_ref")
	if err := assertWatchNanoRoleDefaults(conf, runtimeCore(leakyWatchCols, eventCols, filters, filters), "graphjin"); err == nil {
		t.Fatal("owner refs should not be selectable for non-admin watch projection")
	}
	leakyWatchRoleCols := append(append([]string{}, watchCols...), "owner_role")
	if err := assertWatchNanoRoleDefaults(conf, runtimeCore(leakyWatchRoleCols, eventCols, filters, filters), "graphjin"); err == nil {
		t.Fatal("owner roles should not be selectable for non-admin watch projection")
	}
	leakyEventCols := append(append([]string{}, eventCols...), "owner_id")
	if err := assertWatchNanoRoleDefaults(conf, runtimeCore(watchCols, leakyEventCols, filters, filters), "graphjin"); err == nil {
		t.Fatal("raw owner ids should not be selectable for non-admin watch event projection")
	}
}

func TestWatchRunnerPersistsEventsIdempotentlyAndNotices(t *testing.T) {
	db, svc := newSQLiteWatchService(t, 20)
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("initArtifactsBeforeCore: %v", err)
	}
	startSQLiteWatchCore(t, svc, db)
	cp := newWatchControlPlane(svc)
	ctx := contextWithUserRole(artifactUserCtx("user_1"), "analyst")
	row, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table:     watchesRootTable,
		Operation: "insert",
		Input: map[string]interface{}{
			"name":          "new_orders",
			"query":         cursorOrdersWatchQuery("new_orders_watch"),
			"delivery_json": map[string]interface{}{"kind": "inbox"},
		},
	})
	if err != nil {
		t.Fatalf("insert watch: %v", err)
	}
	watchID := row["id"].(string)
	def := watchRuntimeDefinition{
		ID:           watchID,
		Name:         "new_orders",
		Query:        cursorOrdersWatchQuery("new_orders_watch"),
		DeliveryJSON: `{"kind":"inbox"}`,
		AccountID:    "acct_1",
		OwnerID:      "user_1",
		OwnerRole:    "analyst",
	}
	dataHash, inserted, err := svc.persistWatchResult(ctx, &def, &core.Result{
		Data: json.RawMessage(`{"data":{"orders":[{"id":1,"status":"new"}]}}`),
	})
	if err != nil {
		t.Fatalf("persistWatchResult: %v", err)
	}
	if !inserted || dataHash == "" {
		t.Fatalf("first watch result inserted=%v hash=%q", inserted, dataHash)
	}
	if got := sqliteRevision(t, db, "watch_events"); got != 1 {
		t.Fatalf("watch event revision after runner insert = %d, want 1", got)
	}
	var storedHash, lastError string
	if err := db.QueryRow(`SELECT last_data_hash, last_error FROM "_graphjin_watches" WHERE id = ?`, watchID).Scan(&storedHash, &lastError); err != nil {
		t.Fatalf("query updated watch state: %v", err)
	}
	if storedHash != dataHash || lastError != "" {
		t.Fatalf("watch state hash=%q last_error=%q, want hash %q and empty error", storedHash, lastError, dataHash)
	}

	eventRows, err := cp.watchEventRows(ctx)
	if err != nil {
		t.Fatalf("watchEventRows: %v", err)
	}
	if len(eventRows) != 1 {
		t.Fatalf("watch event rows = %+v, want one", eventRows)
	}
	if eventRows[0]["data_hash"] != dataHash || eventRows[0]["delivery_status"] != "pending" {
		t.Fatalf("unexpected watch event row: %+v", eventRows[0])
	}
	if eventRows[0]["data_truncated"] != false {
		t.Fatalf("small watch event should not be marked truncated: %+v", eventRows[0])
	}
	if eventRows[0]["owner_id"] == "user_1" {
		t.Fatalf("watch event projection leaked raw owner id: %+v", eventRows[0])
	}

	var resp gjagent.Response
	svc.appendWatchNotices(ctx, &resp)
	if len(resp.Notices) != 1 || resp.Notices[0].Kind != "watch_events_unseen" || resp.Notices[0].Count != 1 {
		t.Fatalf("unexpected watch notices: %+v", resp.Notices)
	}
	resp = gjagent.Response{}
	svc.appendWatchNotices(artifactUserCtx("user_2"), &resp)
	if len(resp.Notices) != 0 {
		t.Fatalf("other user should not get watch notices: %+v", resp.Notices)
	}

	def.LastDataHash = dataHash
	_, inserted, err = svc.persistWatchResult(ctx, &def, &core.Result{
		Data: json.RawMessage(`{"data":{"orders":[{"id":1,"status":"new"}]}}`),
	})
	if err != nil {
		t.Fatalf("persist duplicate watch result: %v", err)
	}
	if inserted {
		t.Fatal("duplicate watch result should not insert a second event")
	}
	eventRows, err = cp.watchEventRows(ctx)
	if err != nil {
		t.Fatalf("watchEventRows after duplicate: %v", err)
	}
	if len(eventRows) != 1 {
		t.Fatalf("duplicate result inserted extra rows: %+v", eventRows)
	}
	if got := sqliteRevision(t, db, "watch_events"); got != 1 {
		t.Fatalf("watch event revision after duplicate = %d, want 1", got)
	}

	if _, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table:     watchEventsRootTable,
		Operation: "update",
		Where:     map[string]interface{}{"id": eventRows[0]["id"]},
		Input:     map[string]interface{}{"seen": true},
	}); err != nil {
		t.Fatalf("mark event seen: %v", err)
	}
	resp = gjagent.Response{}
	svc.appendWatchNotices(ctx, &resp)
	if len(resp.Notices) != 0 {
		t.Fatalf("seen watch events should not produce notices: %+v", resp.Notices)
	}
}

func TestWatchRunnerPersistsCursorCheckpointAndResumeVars(t *testing.T) {
	db, svc := newSQLiteWatchService(t, 20)
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("initArtifactsBeforeCore: %v", err)
	}
	startSQLiteWatchCore(t, svc, db)
	if _, err := db.Exec(`INSERT INTO orders (id, status, updated_at) VALUES (1, 'new', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("insert order: %v", err)
	}

	cp := newWatchControlPlane(svc)
	ctx := contextWithUserRole(artifactUserCtx("user_1"), "analyst")
	query := cursorOrdersWatchQuery("cursor_orders")
	row, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table:     watchesRootTable,
		Operation: "insert",
		Input: map[string]interface{}{
			"name":          "cursor_orders",
			"query":         query,
			"delivery_json": map[string]interface{}{"kind": "inbox"},
		},
	})
	if err != nil {
		t.Fatalf("insert watch: %v", err)
	}
	watchID := row["id"].(string)

	subCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	vars, err := watchVariablesWithCursor("", "")
	if err != nil {
		t.Fatalf("seed cursor vars: %v", err)
	}
	member, err := svc.gj.Subscribe(subCtx, query, vars, nil)
	if err != nil {
		t.Fatalf("subscribe watch query: %v", err)
	}
	defer member.Unsubscribe()
	if got := member.CursorVariableNames(); len(got) != 1 || got[0] != "cursor" {
		t.Fatalf("cursor variables = %v, want [cursor]", got)
	}
	var res *core.Result
	select {
	case res = <-member.Result:
	case <-subCtx.Done():
		t.Fatal("timed out waiting for subscription result")
	}
	if cursors := res.SubscriptionCursors(); cursors["cursor"] == "" {
		t.Fatalf("subscription cursors = %v, data = %s, want cursor checkpoint", cursors, string(res.Data))
	}

	def := watchRuntimeDefinition{
		ID:           watchID,
		Name:         "cursor_orders",
		Query:        query,
		DeliveryJSON: `{"kind":"inbox"}`,
		AccountID:    "acct_1",
		OwnerID:      "user_1",
		OwnerRole:    "analyst",
	}
	dataHash, inserted, err := svc.persistWatchResult(ctx, &def, res)
	if err != nil {
		t.Fatalf("persist watch result: %v", err)
	}
	if !inserted || dataHash == "" {
		t.Fatalf("persist inserted=%v hash=%q, want first event", inserted, dataHash)
	}
	var storedCursor string
	if err := db.QueryRow(`SELECT last_cursor_json FROM "_graphjin_watches" WHERE id = ?`, watchID).Scan(&storedCursor); err != nil {
		t.Fatalf("query stored cursor: %v", err)
	}
	if storedCursor == "" {
		t.Fatal("stored cursor checkpoint is empty")
	}
	var cursorVars map[string]string
	if err := json.Unmarshal([]byte(storedCursor), &cursorVars); err != nil {
		t.Fatalf("stored cursor json invalid: %v", err)
	}
	if cursorVars["cursor"] == "" {
		t.Fatalf("stored cursor vars = %v, want cursor", cursorVars)
	}
	resumeVars, err := watchVariablesWithCursor("", storedCursor)
	if err != nil {
		t.Fatalf("merge cursor vars: %v", err)
	}
	var resume map[string]string
	if err := json.Unmarshal(resumeVars, &resume); err != nil {
		t.Fatalf("resume vars invalid: %v", err)
	}
	if resume["cursor"] != cursorVars["cursor"] {
		t.Fatalf("resume cursor = %q, want %q", resume["cursor"], cursorVars["cursor"])
	}
	def.LastDataHash = dataHash
	_, inserted, err = svc.persistWatchResult(ctx, &def, res)
	if err != nil {
		t.Fatalf("persist duplicate watch result: %v", err)
	}
	if inserted {
		t.Fatal("duplicate cursor-backed result should not insert a second event")
	}
	eventRows, err := cp.watchEventRows(ctx)
	if err != nil {
		t.Fatalf("watchEventRows: %v", err)
	}
	if len(eventRows) != 1 {
		t.Fatalf("duplicate result inserted extra rows: %+v", eventRows)
	}
}

func TestWatchRunnerEmptyInitialResultDoesNotRefireAfterRestart(t *testing.T) {
	_, svc := newSQLiteWatchService(t, 20)
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("initArtifactsBeforeCore: %v", err)
	}
	startSQLiteWatchCore(t, svc, svc.dbs["app"])

	cp := newWatchControlPlane(svc)
	ctx := contextWithUserRole(artifactUserCtx("user_1"), "analyst")
	query := cursorOrdersWatchQuery("empty_orders")
	row, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table:     watchesRootTable,
		Operation: "insert",
		Input: map[string]interface{}{
			"name":          "empty_orders",
			"query":         query,
			"delivery_json": map[string]interface{}{"kind": "inbox"},
		},
	})
	if err != nil {
		t.Fatalf("insert watch: %v", err)
	}
	watchID := row["id"].(string)
	def := watchRuntimeDefinition{
		ID:           watchID,
		Name:         "empty_orders",
		Query:        query,
		DeliveryJSON: `{"kind":"inbox"}`,
		AccountID:    "acct_1",
		OwnerID:      "user_1",
		OwnerRole:    "analyst",
	}
	emptyData := json.RawMessage(`{"orders":[],"orders_cursor":null}`)
	dataHash, inserted, err := svc.persistWatchResult(ctx, &def, &core.Result{Data: emptyData})
	if err != nil {
		t.Fatalf("persist empty result: %v", err)
	}
	if !inserted || dataHash == "" {
		t.Fatalf("empty first result inserted=%v hash=%q, want first event", inserted, dataHash)
	}

	restarted := def
	restarted.LastDataHash = dataHash
	_, inserted, err = svc.persistWatchResult(ctx, &restarted, &core.Result{Data: emptyData})
	if err != nil {
		t.Fatalf("persist restarted empty result: %v", err)
	}
	if inserted {
		t.Fatal("empty result should not refire after restart with stored hash")
	}
	eventRows, err := cp.watchEventRows(ctx)
	if err != nil {
		t.Fatalf("watchEventRows: %v", err)
	}
	if len(eventRows) != 1 {
		t.Fatalf("empty restart inserted extra rows: %+v", eventRows)
	}
}

func TestDeleteWatchCascadesEvents(t *testing.T) {
	db, svc := newSQLiteWatchService(t, 20)
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("initArtifactsBeforeCore: %v", err)
	}
	startSQLiteWatchCore(t, svc, db)
	cp := newWatchControlPlane(svc)
	ctx := contextWithUserRole(artifactUserCtx("user_1"), "analyst")
	row, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table:     watchesRootTable,
		Operation: "insert",
		Input: map[string]interface{}{
			"name":  "delete_me",
			"query": cursorOrdersWatchQuery("delete_me"),
		},
	})
	if err != nil {
		t.Fatalf("insert watch: %v", err)
	}
	watchID := row["id"].(string)
	insertWatchEventFixture(t, svc, ctx, "evt_delete_me", watchID, time.Now().UTC().Format(time.RFC3339))

	if _, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table:     watchesRootTable,
		Operation: "delete",
		Where:     map[string]interface{}{"id": map[string]interface{}{"eq": watchID}},
	}); err != nil {
		t.Fatalf("delete watch: %v", err)
	}
	if got := watchEventIDs(t, db); len(got) != 0 {
		t.Fatalf("watch events after delete = %+v, want none", got)
	}
	if got := sqliteRevision(t, db, "watches"); got != 2 {
		t.Fatalf("watch revision after delete = %d, want 2", got)
	}
	if got := sqliteRevision(t, db, "watch_events"); got != 1 {
		t.Fatalf("watch event revision after delete = %d, want 1", got)
	}
}

func TestSweepWatchEventsPrunesExpiredAndOrphanedRows(t *testing.T) {
	db, svc := newSQLiteWatchService(t, 20)
	svc.conf.Core.Watches.EventRetentionHours = 1
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("initArtifactsBeforeCore: %v", err)
	}
	startSQLiteWatchCore(t, svc, db)
	ctx := artifactUserCtx("user_1")
	if _, err := db.Exec(`INSERT INTO "_graphjin_watches" (id, name, query, account_id, owner_id, owner_role) VALUES (?, ?, ?, ?, ?, ?)`,
		"watch_live", "live", cursorOrdersWatchQuery("live"), "acct_1", "user_1", "analyst"); err != nil {
		t.Fatalf("insert watch fixture: %v", err)
	}
	now := time.Now().UTC()
	insertWatchEventFixture(t, svc, ctx, "evt_keep", "watch_live", now.Format(time.RFC3339))
	insertWatchEventFixture(t, svc, ctx, "evt_old", "watch_live", now.Add(-2*time.Hour).Format(time.RFC3339))
	insertWatchEventFixture(t, svc, ctx, "evt_orphan", "watch_deleted", now.Format(time.RFC3339))

	deleted, err := svc.sweepWatchEvents(ctx)
	if err != nil {
		t.Fatalf("sweepWatchEvents: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("sweep deleted %d rows, want 2", deleted)
	}
	if got := watchEventIDs(t, db); len(got) != 1 || got[0] != "evt_keep" {
		t.Fatalf("watch events after sweep = %+v, want [evt_keep]", got)
	}
	if got := sqliteRevision(t, db, "watch_events"); got != 1 {
		t.Fatalf("watch event revision after sweep = %d, want 1", got)
	}
}

func TestWatchRunnerSkipsPersistForDeletedWatch(t *testing.T) {
	db, svc := newSQLiteWatchService(t, 20)
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("initArtifactsBeforeCore: %v", err)
	}
	startSQLiteWatchCore(t, svc, db)
	dataHash, inserted, err := svc.persistWatchResult(artifactUserCtx("user_1"), &watchRuntimeDefinition{
		ID:        "watch_deleted",
		Name:      "deleted",
		Query:     cursorOrdersWatchQuery("deleted"),
		AccountID: "acct_1",
		OwnerID:   "user_1",
		OwnerRole: "analyst",
	}, &core.Result{Data: json.RawMessage(`{"data":{"orders":[{"id":1}]}}`)})
	if err != nil {
		t.Fatalf("persist deleted watch result: %v", err)
	}
	if dataHash == "" || inserted {
		t.Fatalf("deleted watch persist hash=%q inserted=%v, want hash and no insert", dataHash, inserted)
	}
	if got := watchEventIDs(t, db); len(got) != 0 {
		t.Fatalf("deleted watch should not insert events, got %+v", got)
	}
}

func TestWatchRunnerCapsStoredSnapshotButHashesFullData(t *testing.T) {
	db, svc := newSQLiteWatchService(t, 20)
	svc.conf.Core.Watches.SnapshotMaxBytes = 96
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("initArtifactsBeforeCore: %v", err)
	}
	startSQLiteWatchCore(t, svc, db)
	ctx := artifactUserCtx("user_1")
	def := watchRuntimeDefinition{
		ID:        "watch_1",
		Name:      "large_orders",
		Query:     cursorOrdersWatchQuery("large_orders_watch"),
		AccountID: "acct_1",
		OwnerID:   "user_1",
		OwnerRole: "user",
	}
	if _, _, _, eventTable, ok := svc.watchDB(); !ok {
		t.Fatal("watch DB not configured")
	} else if _, err := db.Exec(`INSERT INTO "_graphjin_watches" (id, name, query, account_id, owner_id, owner_role) VALUES (?, ?, ?, ?, ?, ?)`,
		def.ID, def.Name, def.Query, def.AccountID, def.OwnerID, def.OwnerRole); err != nil {
		t.Fatalf("insert watch fixture: %v", err)
	} else if eventTable == "" {
		t.Fatal("empty event table")
	}

	fullData := `{"data":{"orders":[{"id":1,"notes":"` + strings.Repeat("x", 300) + `"}]}}`
	dataHash, inserted, err := svc.persistWatchResult(ctx, &def, &core.Result{Data: json.RawMessage(fullData)})
	if err != nil {
		t.Fatalf("persistWatchResult: %v", err)
	}
	if !inserted {
		t.Fatal("large watch result should insert an event")
	}
	if dataHash != hashString(fullData) {
		t.Fatalf("data hash = %q, want full-data hash %q", dataHash, hashString(fullData))
	}
	var storedJSON string
	var truncated bool
	if err := db.QueryRow(`SELECT data_json, data_truncated FROM "_graphjin_watch_events" WHERE watch_id = ?`, def.ID).Scan(&storedJSON, &truncated); err != nil {
		t.Fatalf("query stored event snapshot: %v", err)
	}
	if !truncated {
		t.Fatal("large watch event should be marked truncated")
	}
	if len(storedJSON) > svc.conf.Core.Watches.SnapshotMaxBytes {
		t.Fatalf("stored snapshot length = %d, want <= %d: %s", len(storedJSON), svc.conf.Core.Watches.SnapshotMaxBytes, storedJSON)
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(storedJSON), &envelope); err != nil {
		t.Fatalf("stored truncated snapshot should remain valid JSON: %v; %s", err, storedJSON)
	}
	if envelope["truncated"] != true || envelope["prefix"] == "" {
		t.Fatalf("unexpected truncated snapshot envelope: %+v", envelope)
	}
}

func TestWatchDeliveryWebhookAllowlistSignatureAndStatus(t *testing.T) {
	db, svc := newSQLiteWatchService(t, 20)
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("initArtifactsBeforeCore: %v", err)
	}
	startSQLiteWatchCore(t, svc, db)

	var gotBody []byte
	var gotSignature, gotIDKey, gotCustom string
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotIDKey = r.Header.Get("Idempotency-Key")
		gotSignature = r.Header.Get("X-GraphJin-Signature")
		gotCustom = r.Header.Get("X-Watch-Test")
		body, _ := io.ReadAll(r.Body)
		gotBody = body
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`accepted`))
	}))
	defer hook.Close()
	svc.conf.Core.Watches.WebhookAllow = []string{hook.URL}
	t.Setenv("WATCH_WEBHOOK_SECRET", "top-secret")

	cp := newWatchControlPlane(svc)
	ctx := contextWithUserRole(artifactUserCtx("user_1"), "analyst")
	row, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table:     watchesRootTable,
		Operation: "insert",
		Input: map[string]interface{}{
			"name":  "webhook_orders",
			"query": cursorOrdersWatchQuery("webhook_orders"),
			"delivery_json": map[string]any{
				"kind":       "webhook",
				"url":        hook.URL,
				"secret_env": "WATCH_WEBHOOK_SECRET",
				"headers":    map[string]any{"X-Watch-Test": "ok"},
			},
		},
	})
	if err != nil {
		t.Fatalf("insert watch: %v", err)
	}
	row = approveWatchActionForTest(t, cp, ctx, row)
	def := watchRuntimeDefinition{
		ID:           row["id"].(string),
		Name:         "webhook_orders",
		Query:        cursorOrdersWatchQuery("webhook_orders"),
		DeliveryJSON: `{"kind":"webhook","url":` + strconvQuote(hook.URL) + `,"secret_env":"WATCH_WEBHOOK_SECRET","headers":{"X-Watch-Test":"ok"}}`,
		AccountID:    "acct_1",
		OwnerID:      "user_1",
		OwnerRole:    "analyst",
	}
	_, inserted, err := svc.persistWatchResult(ctx, &def, &core.Result{Data: json.RawMessage(`{"data":{"orders":[{"id":1}]}}`)})
	if err != nil {
		t.Fatalf("persist watch result: %v", err)
	}
	if !inserted {
		t.Fatal("expected event insert")
	}
	if err := svc.processPendingWatchDeliveries(context.Background()); err != nil {
		t.Fatalf("processPendingWatchDeliveries: %v", err)
	}
	if gotBody == nil {
		t.Fatal("webhook was not called")
	}
	if gotCustom != "ok" {
		t.Fatalf("custom header = %q", gotCustom)
	}
	mac := hmac.New(sha256.New, []byte("top-secret"))
	_, _ = mac.Write(gotBody)
	wantSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if gotSignature != wantSig {
		t.Fatalf("signature = %q, want %q", gotSignature, wantSig)
	}
	if gotIDKey == "" {
		t.Fatal("missing idempotency key")
	}
	var status string
	var attempts int
	var receipt string
	if err := db.QueryRow(`SELECT delivery_status, delivery_attempts, receipt_json FROM "_graphjin_watch_events"`).Scan(&status, &attempts, &receipt); err != nil {
		t.Fatalf("query delivery status: %v", err)
	}
	if status != "delivered" || attempts != 1 || !strings.Contains(receipt, `"kind":"webhook"`) {
		t.Fatalf("delivery status=%q attempts=%d receipt=%s", status, attempts, receipt)
	}
}

func TestWatchWebhookDenyByDefault(t *testing.T) {
	_, svc := newSQLiteWatchService(t, 20)
	if _, err := svc.validateWatchWebhookURL(context.Background(), "https://hooks.example.com/watch"); err == nil || !strings.Contains(err.Error(), "webhook_allow") {
		t.Fatalf("expected deny-by-default webhook error, got %v", err)
	}
	svc.conf.Core.Watches.WebhookAllow = []string{"hooks.example.com"}
	if _, err := svc.validateWatchWebhookURL(context.Background(), "https://hooks.example.com/watch"); err == nil || !strings.Contains(err.Error(), "webhook_allow") {
		t.Fatalf("bare host allowlist should not match; got %v", err)
	}
	tr, ok := svc.watchWebhookTransport().(*http.Transport)
	if !ok || tr.Proxy != nil {
		t.Fatalf("watch webhook transport must not use environment proxies: %#v", svc.watchWebhookTransport())
	}
	if !isBlockedWebhookIP(net.ParseIP("100.64.0.1")) || !isBlockedWebhookIP(net.ParseIP("100.127.255.255")) {
		t.Fatal("CGNAT webhook targets should be blocked")
	}
	if isBlockedWebhookIP(net.ParseIP("100.128.0.1")) {
		t.Fatal("IP outside CGNAT range should not be blocked by the CGNAT check")
	}
	if _, err := resolveWebhookDialIP(context.Background(), "100.64.0.1", nil); err == nil || !strings.Contains(err.Error(), "private or local") {
		t.Fatalf("CGNAT literal without exact allowlist should be blocked, got %v", err)
	}
	if ip, err := resolveWebhookDialIP(context.Background(), "100.64.0.1", []string{"http://100.64.0.1"}); err != nil || !ip.Equal(net.ParseIP("100.64.0.1")) {
		t.Fatalf("exact literal allowlist should permit CGNAT admin escape hatch, ip=%v err=%v", ip, err)
	}
}

func TestWatchWebhookSecurityPolicyFlagsUnsafeAllowlist(t *testing.T) {
	conf := &Config{Core: core.Config{Watches: core.WatchesConfig{Enabled: true}}}
	row := watchWebhookSecurityPolicyForTest(t, conf)
	if row.Status != securityStatusFinding || row.EffectiveAllowed {
		t.Fatalf("empty watch webhook allowlist should be a finding, got status=%q effective=%v", row.Status, row.EffectiveAllowed)
	}
	conf.Core.Watches.WebhookAllow = []string{"hooks.example.com"}
	row = watchWebhookSecurityPolicyForTest(t, conf)
	if row.Status != securityStatusFinding || row.EffectiveAllowed {
		t.Fatalf("bare host watch webhook allowlist should be a finding, got status=%q effective=%v", row.Status, row.EffectiveAllowed)
	}
	conf.Core.Watches.WebhookAllow = []string{"https://hooks.example.com"}
	row = watchWebhookSecurityPolicyForTest(t, conf)
	if row.Status != securityStatusPass || !row.EffectiveAllowed {
		t.Fatalf("exact origin watch webhook allowlist should pass, got status=%q effective=%v", row.Status, row.EffectiveAllowed)
	}
}

func TestWatchDeliveryWorkflowRunsUnderOwnerContext(t *testing.T) {
	db, svc := newSQLiteWatchService(t, 20)
	svc.conf.MCP.AllowWorkflowExecution = true
	if err := svc.fs.Put("/workflows/notify.js", []byte(`function main(input) {
  return { user: ctx.user_id, role: ctx.user_role, event: input.event.id };
}`)); err != nil {
		t.Fatalf("write workflow: %v", err)
	}
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("initArtifactsBeforeCore: %v", err)
	}
	startSQLiteWatchCore(t, svc, db)

	ctx := contextWithUserRole(artifactUserCtx("user_1"), "analyst")
	cp := newWatchControlPlane(svc)
	row, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table:     watchesRootTable,
		Operation: "insert",
		Input: map[string]interface{}{
			"name":          "workflow_orders",
			"query":         cursorOrdersWatchQuery("workflow_orders"),
			"delivery_json": map[string]any{"kind": "workflow", "name": "notify"},
		},
	})
	if err != nil {
		t.Fatalf("insert watch: %v", err)
	}
	row = approveWatchActionForTest(t, cp, ctx, row)
	def := watchRuntimeDefinition{
		ID:           row["id"].(string),
		Name:         "workflow_orders",
		Query:        cursorOrdersWatchQuery("workflow_orders"),
		DeliveryJSON: `{"kind":"workflow","name":"notify"}`,
		AccountID:    "acct_1",
		OwnerID:      "user_1",
		OwnerRole:    "analyst",
	}
	if _, _, err := svc.persistWatchResult(ctx, &def, &core.Result{Data: json.RawMessage(`{"data":{"orders":[{"id":2}]}}`)}); err != nil {
		t.Fatalf("persist watch result: %v", err)
	}
	if err := svc.processPendingWatchDeliveries(context.Background()); err != nil {
		t.Fatalf("process delivery: %v", err)
	}
	var receipt string
	if err := db.QueryRow(`SELECT receipt_json FROM "_graphjin_watch_events"`).Scan(&receipt); err != nil {
		t.Fatalf("query receipt: %v", err)
	}
	if !strings.Contains(receipt, `"user":"user_1"`) || !strings.Contains(receipt, `"role":"analyst"`) {
		t.Fatalf("workflow receipt did not use owner context: %s", receipt)
	}
}

func TestWatchEnrichmentUsesReadOnlyOwnerEnvelope(t *testing.T) {
	db, svc := newSQLiteWatchService(t, 20)
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("initArtifactsBeforeCore: %v", err)
	}
	startSQLiteWatchCore(t, svc, db)
	runner := &scriptedAgentRunner{resp: gjagent.Response{Status: gjagent.StatusAnswered, Answer: "summary"}}
	withScriptedAgentRunner(t, runner)

	ctx := contextWithUserRole(artifactUserCtx("user_1"), "analyst")
	cp := newWatchControlPlane(svc)
	row, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table:     watchesRootTable,
		Operation: "insert",
		Input: map[string]interface{}{
			"name":        "enriched_orders",
			"query":       cursorOrdersWatchQuery("enriched_orders"),
			"enrich_json": map[string]any{"enabled": true, "instruction": "summarize", "max_steps": 99},
		},
	})
	if err != nil {
		t.Fatalf("insert watch: %v", err)
	}
	def := watchRuntimeDefinition{
		ID:         row["id"].(string),
		Name:       "enriched_orders",
		Query:      cursorOrdersWatchQuery("enriched_orders"),
		EnrichJSON: `{"enabled":true,"instruction":"summarize","max_steps":99}`,
		AccountID:  "acct_1",
		OwnerID:    "user_1",
		OwnerRole:  "analyst",
	}
	if _, _, err := svc.persistWatchResult(ctx, &def, &core.Result{Data: json.RawMessage(`{"data":{"orders":[{"id":3}]}}`)}); err != nil {
		t.Fatalf("persist watch result: %v", err)
	}
	if !runner.conf.ReadOnly || runner.conf.Sampling != gjagent.SamplingOff || runner.conf.MaxSteps != 4 {
		t.Fatalf("enrichment config = %+v, want read-only sampling-off max_steps=4", runner.conf)
	}
	if got, _ := runner.ctx.Value(core.UserRoleKey).(string); got != "analyst" {
		t.Fatalf("runner role = %q, want analyst", got)
	}
	events, ok := runner.req.Context["_watch_events"].([]any)
	if !ok || len(events) != 1 {
		t.Fatalf("missing _watch_events context: %+v", runner.req.Context)
	}
	var status, enrichment string
	if err := db.QueryRow(`SELECT delivery_status, enrichment_json FROM "_graphjin_watch_events"`).Scan(&status, &enrichment); err != nil {
		t.Fatalf("query enrichment: %v", err)
	}
	if status != "pending" || !strings.Contains(enrichment, `"status":"ok"`) || !strings.Contains(enrichment, "summary") {
		t.Fatalf("status=%q enrichment=%s", status, enrichment)
	}
}

func TestWatchEnrichmentDailyCapSkipStillDelivers(t *testing.T) {
	db, svc := newSQLiteWatchService(t, 20)
	svc.conf.Core.Watches.EnrichmentDailyCap = 1
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("initArtifactsBeforeCore: %v", err)
	}
	startSQLiteWatchCore(t, svc, db)
	runner := &scriptedAgentRunner{resp: gjagent.Response{Status: gjagent.StatusAnswered, Answer: "should not run"}}
	withScriptedAgentRunner(t, runner)

	ctx := contextWithUserRole(artifactUserCtx("user_1"), "analyst")
	cp := newWatchControlPlane(svc)
	row, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table:     watchesRootTable,
		Operation: "insert",
		Input: map[string]interface{}{
			"name":          "cap_orders",
			"query":         cursorOrdersWatchQuery("cap_orders"),
			"delivery_json": map[string]any{"kind": "inbox"},
			"enrich_json":   map[string]any{"enabled": true, "instruction": "summarize"},
		},
	})
	if err != nil {
		t.Fatalf("insert watch: %v", err)
	}
	watchID := row["id"].(string)
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := svc.internalStoreMutationRows(ctx, "watch_events", `insert: $input`, watchEventStoreFields, map[string]any{"input": map[string]any{
		"id":                "seed_enriched_event",
		"watch_id":          watchID,
		"data_hash":         "seed_hash",
		"data_json":         nullableJSONString(`{"seed":true}`),
		"data_truncated":    false,
		"evidence_json":     nullableJSONString(`{}`),
		"delivery_status":   "delivered",
		"delivery_attempts": 0,
		"delivery_json":     nullableJSONString(`{"kind":"inbox"}`),
		"receipt_json":      nullableJSONString(`{"status":"delivered"}`),
		"enrichment_json":   nullableJSONString(`{"status":"ok"}`),
		"seen":              false,
		"account_id":        "acct_1",
		"owner_id":          "user_1",
		"created_at":        now,
		"updated_at":        now,
	}}); err != nil {
		t.Fatalf("seed enriched event: %v", err)
	}
	data := `{"data":{"orders":[{"id":4}]}}`
	def := watchRuntimeDefinition{
		ID:           watchID,
		Name:         "cap_orders",
		Query:        cursorOrdersWatchQuery("cap_orders"),
		DeliveryJSON: `{"kind":"inbox"}`,
		EnrichJSON:   `{"enabled":true,"instruction":"summarize"}`,
		AccountID:    "acct_1",
		OwnerID:      "user_1",
		OwnerRole:    "analyst",
	}
	dataHash, inserted, err := svc.persistWatchResult(ctx, &def, &core.Result{Data: json.RawMessage(data)})
	if err != nil {
		t.Fatalf("persist watch result: %v", err)
	}
	if !inserted {
		t.Fatal("expected capped event insert")
	}
	if runner.ctx != nil {
		t.Fatalf("daily cap should skip agent runner, got request %+v", runner.req)
	}
	eventID := watchEventID(watchID, dataHash)
	var status, enrichment string
	if err := db.QueryRow(`SELECT delivery_status, enrichment_json FROM "_graphjin_watch_events" WHERE id = ?`, eventID).Scan(&status, &enrichment); err != nil {
		t.Fatalf("query capped enrichment: %v", err)
	}
	if status != "pending" || !strings.Contains(enrichment, `"reason":"daily_cap"`) {
		t.Fatalf("daily-cap event status=%q enrichment=%s", status, enrichment)
	}
	if err := svc.processPendingWatchDeliveries(context.Background()); err != nil {
		t.Fatalf("process capped delivery: %v", err)
	}
	if err := db.QueryRow(`SELECT delivery_status, enrichment_json FROM "_graphjin_watch_events" WHERE id = ?`, eventID).Scan(&status, &enrichment); err != nil {
		t.Fatalf("query capped delivery: %v", err)
	}
	if status != "delivered" || !strings.Contains(enrichment, `"reason":"daily_cap"`) {
		t.Fatalf("daily-cap delivery status=%q enrichment=%s", status, enrichment)
	}
}

func TestWatchRunnerOwnerContextCarriesTrustedIdentity(t *testing.T) {
	ctx := (&graphjinService{}).watchOwnerContext(context.Background(), watchRuntimeDefinition{
		AccountID: "acct_1",
		OwnerID:   "user_1",
		OwnerRole: "analyst",
	})
	if got, _ := ctx.Value(core.UserRoleKey).(string); got != "analyst" {
		t.Fatalf("role = %q, want analyst", got)
	}
	roles, _ := ctx.Value(core.IdentityRolesKey).([]string)
	if len(roles) != 1 || roles[0] != "analyst" {
		t.Fatalf("identity roles = %+v, want analyst", roles)
	}
	vars, _ := ctx.Value(core.IdentityVarsKey).(map[string]interface{})
	if vars["user_id"] != "user_1" || vars["user_ref"] != safeArtifactIdentity("user_1", false) {
		t.Fatalf("trusted user vars = %+v", vars)
	}
	if vars["account_id"] != "acct_1" || vars["account_ref"] != safeArtifactIdentity("acct_1", false) {
		t.Fatalf("trusted account vars = %+v", vars)
	}
}

func TestWatchRunnerDowngradesUnknownStoredOwnerRole(t *testing.T) {
	_, svc := newSQLiteWatchService(t, 20)
	svc.conf.Core.Roles = []core.Role{{Name: "analyst"}}
	if got := svc.trustedWatchRunnerRole("analyst"); got != "analyst" {
		t.Fatalf("configured watch runner role = %q, want analyst", got)
	}
	if got := svc.trustedWatchRunnerRole("admin"); got != "admin" {
		t.Fatalf("configured admin identity role = %q, want admin", got)
	}
	if got := svc.trustedWatchRunnerRole("former_admin"); got != "user" {
		t.Fatalf("unknown stored watch runner role = %q, want user", got)
	}
}

func TestTrimWatchEventProjectionRows(t *testing.T) {
	now := time.Now().UTC()
	rows := []map[string]any{
		{"id": "old", "watch_id": "watch_1", "created_at": now.Add(-2 * time.Hour).Format(time.RFC3339)},
		{"id": "older_kept_without_retention", "watch_id": "watch_1", "created_at": now.Add(-30 * time.Minute).Format(time.RFC3339)},
		{"id": "newest", "watch_id": "watch_1", "created_at": now.Format(time.RFC3339)},
		{"id": "other_watch", "watch_id": "watch_2", "created_at": now.Add(-10 * time.Minute).Format(time.RFC3339)},
	}
	got := trimWatchEventProjectionRows(rows, core.WatchesConfig{
		EventRetentionHours: 1,
		MaxEventsPerWatch:   1,
	})
	if len(got) != 2 {
		t.Fatalf("trimmed rows = %+v, want two recent capped rows", got)
	}
	if got[0]["id"] != "newest" || got[1]["id"] != "other_watch" {
		t.Fatalf("trimmed row order/ids = %+v", got)
	}
}

func TestEphemeralWatchLeaseLifecycleAndExpiry(t *testing.T) {
	db, svc := newSQLiteWatchService(t, 20)
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("initArtifactsBeforeCore: %v", err)
	}
	startSQLiteWatchCore(t, svc, db)
	cp := newWatchControlPlane(svc)
	ctx := artifactUserCtx("user_1")

	durable, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table:     watchesRootTable,
		Operation: "insert",
		Input: map[string]interface{}{
			"name":  "durable_watch",
			"query": cursorOrdersWatchQuery("durable_watch"),
		},
	})
	if err != nil {
		t.Fatalf("insert durable watch: %v", err)
	}
	if durable["lifecycle"] != "durable" || durable["lease_expires_at"] != "" {
		t.Fatalf("durable watch lifecycle projection = %+v", durable)
	}

	if _, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table:     watchesRootTable,
		Operation: "insert",
		Input: map[string]interface{}{
			"name":      "bad_ephemeral",
			"query":     cursorOrdersWatchQuery("bad_ephemeral"),
			"lifecycle": "ephemeral",
		},
	}); err == nil || !strings.Contains(err.Error(), "lease_expires_at is required") {
		t.Fatalf("ephemeral watch without lease error = %v", err)
	}

	expiresAt := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	ephemeral, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table:     watchesRootTable,
		Operation: "insert",
		Input: map[string]interface{}{
			"name":             "ephemeral_watch",
			"query":            cursorOrdersWatchQuery("ephemeral_watch"),
			"lifecycle":        "ephemeral",
			"lease_expires_at": expiresAt,
		},
	})
	if err != nil {
		t.Fatalf("insert ephemeral watch: %v", err)
	}
	watchID := ephemeral["id"].(string)
	if ephemeral["lifecycle"] != "ephemeral" || ephemeral["lease_expires_at"] == "" {
		t.Fatalf("ephemeral watch lifecycle projection = %+v", ephemeral)
	}
	if _, err := db.Exec(`INSERT INTO "_graphjin_watch_events" (id, watch_id, data_hash, owner_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		"evt_ephemeral", watchID, "hash_1", "user_1", time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatalf("insert event: %v", err)
	}
	if _, err := db.Exec(`UPDATE "_graphjin_watches" SET lease_expires_at = ? WHERE id = ?`, time.Now().UTC().Add(-time.Minute).Format(time.RFC3339), watchID); err != nil {
		t.Fatalf("backdate lease: %v", err)
	}
	if err := svc.expireEphemeralWatches(ctx); err != nil {
		t.Fatalf("expireEphemeralWatches: %v", err)
	}
	var status string
	var enabled bool
	if err := db.QueryRow(`SELECT status, enabled FROM "_graphjin_watches" WHERE id = ?`, watchID).Scan(&status, &enabled); err != nil {
		t.Fatalf("read expired watch: %v", err)
	}
	if status != "expired" || enabled {
		t.Fatalf("expired watch status/enabled = %s/%v", status, enabled)
	}
	if got := watchEventIDs(t, db); len(got) != 1 || got[0] != "evt_ephemeral" {
		t.Fatalf("expiry should keep watch events, got %v", got)
	}
}

func TestWatchCleanupPreviewAndApplyScopesDurableDeletion(t *testing.T) {
	db, svc := newSQLiteWatchService(t, 20)
	svc.conf.Core.Watches.EventRetentionHours = 1
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("initArtifactsBeforeCore: %v", err)
	}
	startSQLiteWatchCore(t, svc, db)
	now := time.Now().UTC()
	old := now.Add(-2 * time.Hour).Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO "_graphjin_watches" (id, name, query, lifecycle, lease_expires_at, status, enabled, owner_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"watch_expired", "expired", cursorOrdersWatchQuery("expired"), "ephemeral", old, "active", true, "user_1", old, old); err != nil {
		t.Fatalf("insert expired watch: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO "_graphjin_watches" (id, name, query, lifecycle, status, enabled, owner_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"watch_stale", "stale", cursorOrdersWatchQuery("stale"), "durable", "paused", false, "user_1", old, old); err != nil {
		t.Fatalf("insert stale watch: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO "_graphjin_watch_events" (id, watch_id, data_hash, owner_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		"evt_orphan", "missing_watch", "hash_orphan", "user_1", old, old); err != nil {
		t.Fatalf("insert orphan event: %v", err)
	}

	ctx := artifactUserCtx("user_1")
	preview, err := svc.previewWatchCleanup(ctx, watchCleanupOptions{StaleHours: 1})
	if err != nil {
		t.Fatalf("previewWatchCleanup: %v", err)
	}
	if preview.Counts[watchCleanupReasonExpiredEphemeral] != 1 ||
		preview.Counts[watchCleanupReasonDisabledStale] != 1 ||
		preview.Counts[watchCleanupReasonOrphanedEvents] != 1 {
		t.Fatalf("cleanup counts = %+v candidates=%+v", preview.Counts, preview.Candidates)
	}
	applied, err := svc.applyWatchCleanup(ctx, watchCleanupApplyRequest{
		Token:   preview.Token,
		Reasons: []string{watchCleanupReasonExpiredEphemeral, watchCleanupReasonDisabledStale, watchCleanupReasonOrphanedEvents},
	})
	if err != nil {
		t.Fatalf("applyWatchCleanup: %v", err)
	}
	if len(applied.ExpiredWatchIDs) != 1 || applied.ExpiredWatchIDs[0] != "watch_expired" {
		t.Fatalf("expired watches = %+v", applied)
	}
	if len(applied.DeletedWatchIDs) != 0 {
		t.Fatalf("durable stale watch should not be broad-deleted by reason: %+v", applied)
	}
	if got := watchEventIDs(t, db); len(got) != 0 {
		t.Fatalf("orphan event should be deleted, got %v", got)
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM "_graphjin_watches" WHERE id = ?`, "watch_expired").Scan(&status); err != nil {
		t.Fatalf("read expired watch status: %v", err)
	}
	if status != "expired" {
		t.Fatalf("expired watch status = %q", status)
	}
	preview, err = svc.previewWatchCleanup(ctx, watchCleanupOptions{StaleHours: 1})
	if err != nil {
		t.Fatalf("preview after apply: %v", err)
	}
	applied, err = svc.applyWatchCleanup(ctx, watchCleanupApplyRequest{Token: preview.Token, WatchIDs: []string{"watch_stale"}})
	if err != nil {
		t.Fatalf("apply explicit durable cleanup: %v", err)
	}
	if len(applied.DeletedWatchIDs) != 1 || applied.DeletedWatchIDs[0] != "watch_stale" {
		t.Fatalf("explicit durable cleanup result = %+v", applied)
	}
}

func TestWatchRESTWrappersCRUDAndUnseenEvents(t *testing.T) {
	db, svc := newSQLiteWatchService(t, 20)
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("initArtifactsBeforeCore: %v", err)
	}
	startSQLiteWatchCore(t, svc, db)
	hs := &HttpService{}
	hs.Store(svc)
	h := hs.apiV1Watches(nil)
	ctx := artifactUserCtx("user_1")
	body := `{"name":"rest_watch","query":"` + cursorOrdersWatchQuery("rest_watch") + `"}`
	req := httptest.NewRequest(http.MethodPost, routeWatches, bytes.NewBufferString(body)).WithContext(ctx)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var createResp struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	watchID, _ := createResp.Data["id"].(string)
	if watchID == "" {
		t.Fatalf("create response missing id: %+v", createResp)
	}
	if _, err := db.Exec(`INSERT INTO "_graphjin_watch_events" (id, watch_id, data_hash, account_id, owner_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"evt_rest", watchID, "hash_rest", "acct_1", "user_1", time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatalf("insert rest event: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO "_graphjin_watch_events" (id, watch_id, data_hash, account_id, owner_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"evt_other_owner", watchID, "hash_other", "acct_1", "user_2", time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatalf("insert other owner event: %v", err)
	}
	req = httptest.NewRequest(http.MethodGet, routeWatchEvents+"/unseen", nil).WithContext(ctx)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unseen status=%d body=%s", rec.Code, rec.Body.String())
	}
	var unseen watchEventsUnseenResource
	if err := json.Unmarshal(rec.Body.Bytes(), &unseen); err != nil {
		t.Fatalf("decode unseen: %v", err)
	}
	if unseen.Count != 1 || unseen.EventIDs[0] != "evt_rest" {
		t.Fatalf("unseen payload = %+v", unseen)
	}
	req = httptest.NewRequest(http.MethodPost, routeWatchEvents+"/evt_rest/seen", nil).WithContext(ctx)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("seen status=%d body=%s", rec.Code, rec.Body.String())
	}
	old := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO "_graphjin_watches" (id, name, query, lifecycle, lease_expires_at, status, enabled, owner_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"rest_expired_1", "rest_expired_1", cursorOrdersWatchQuery("rest_expired_1"), "ephemeral", old, "active", true, "user_1", old, old); err != nil {
		t.Fatalf("insert rest expired watch: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO "_graphjin_watches" (id, name, query, lifecycle, lease_expires_at, status, enabled, owner_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"rest_expired_2", "rest_expired_2", cursorOrdersWatchQuery("rest_expired_2"), "ephemeral", old, "active", true, "user_2", old, old); err != nil {
		t.Fatalf("insert other owner expired watch: %v", err)
	}
	req = httptest.NewRequest(http.MethodPost, routeWatches+"/cleanup-preview", bytes.NewBufferString(`{"stale_hours":1}`)).WithContext(ctx)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cleanup preview status=%d body=%s", rec.Code, rec.Body.String())
	}
	var preview watchCleanupPreview
	if err := json.Unmarshal(rec.Body.Bytes(), &preview); err != nil {
		t.Fatalf("decode cleanup preview: %v", err)
	}
	if preview.Counts[watchCleanupReasonExpiredEphemeral] != 1 ||
		len(preview.Candidates[watchCleanupReasonExpiredEphemeral]) != 1 ||
		preview.Candidates[watchCleanupReasonExpiredEphemeral][0].ID != "rest_expired_1" {
		t.Fatalf("cleanup preview should be owner-scoped, got %+v", preview)
	}
	applyBody := fmt.Sprintf(`{"token":%q,"stale_hours":1,"reasons":[%q]}`, preview.Token, watchCleanupReasonExpiredEphemeral)
	req = httptest.NewRequest(http.MethodPost, routeWatches+"/cleanup-apply", bytes.NewBufferString(applyBody)).WithContext(ctx)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cleanup apply status=%d body=%s", rec.Code, rec.Body.String())
	}
	var applyResp struct {
		Data watchCleanupApplyResult `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &applyResp); err != nil {
		t.Fatalf("decode cleanup apply: %v", err)
	}
	if len(applyResp.Data.ExpiredWatchIDs) != 1 || applyResp.Data.ExpiredWatchIDs[0] != "rest_expired_1" {
		t.Fatalf("cleanup apply result = %+v", applyResp)
	}
	var restExpiredStatus, otherExpiredStatus string
	if err := db.QueryRow(`SELECT status FROM "_graphjin_watches" WHERE id = ?`, "rest_expired_1").Scan(&restExpiredStatus); err != nil {
		t.Fatalf("read rest expired status: %v", err)
	}
	if err := db.QueryRow(`SELECT status FROM "_graphjin_watches" WHERE id = ?`, "rest_expired_2").Scan(&otherExpiredStatus); err != nil {
		t.Fatalf("read other expired status: %v", err)
	}
	if restExpiredStatus != "expired" || otherExpiredStatus != "active" {
		t.Fatalf("cleanup status user/other = %q/%q", restExpiredStatus, otherExpiredStatus)
	}
	req = httptest.NewRequest(http.MethodDelete, routeWatches+"/"+watchID, nil).WithContext(ctx)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}
}

type watchTestSession struct {
	id     string
	notify chan mcp.JSONRPCNotification
	init   bool
}

func (s *watchTestSession) Initialize() { s.init = true }
func (s *watchTestSession) Initialized() bool {
	return s.init
}
func (s *watchTestSession) NotificationChannel() chan<- mcp.JSONRPCNotification {
	return s.notify
}
func (s *watchTestSession) SessionID() string { return s.id }

func assertWatchResourceNotification(t *testing.T, ch <-chan mcp.JSONRPCNotification) {
	assertWatchResourceNotificationURI(t, ch, WatchEventsUnseenResourceURI)
}

func assertWatchResourceNotificationURI(t *testing.T, ch <-chan mcp.JSONRPCNotification, wantURI string) {
	t.Helper()
	select {
	case n := <-ch:
		if n.Method != mcp.MethodNotificationResourceUpdated {
			t.Fatalf("notification method = %s", n.Method)
		}
		fields := n.Params.AdditionalFields
		if len(fields) != 1 || fields["uri"] != wantURI {
			t.Fatalf("notification params = %+v, want uri %q", fields, wantURI)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for resource update notification")
	}
}

func assertNoWatchResourceNotification(t *testing.T, ch <-chan mcp.JSONRPCNotification) {
	t.Helper()
	select {
	case n := <-ch:
		t.Fatalf("unexpected watch resource notification: %+v", n)
	case <-time.After(50 * time.Millisecond):
	}
}

type fakeWatchCoordinator struct {
	mu              sync.Mutex
	nodeID          string
	current         bool
	claimEventOK    bool
	acquireOK       bool
	nextFence       int64
	leases          map[string]watchLease
	runnerCh        chan struct{}
	unseenCh        chan watchEventScope
	publishedUnseen []watchEventScope
}

func newFakeWatchCoordinator() *fakeWatchCoordinator {
	return &fakeWatchCoordinator{
		nodeID:       "node-a",
		current:      true,
		claimEventOK: true,
		acquireOK:    true,
		leases:       map[string]watchLease{},
		runnerCh:     make(chan struct{}, 8),
		unseenCh:     make(chan watchEventScope, 8),
	}
}

func (f *fakeWatchCoordinator) NodeID() string { return f.nodeID }

func (f *fakeWatchCoordinator) Acquire(_ context.Context, watchID, runtimeKey string, ttl time.Duration) (watchLease, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.acquireOK {
		return watchLease{}, false, nil
	}
	if _, exists := f.leases[watchID]; exists {
		return watchLease{}, false, nil
	}
	f.nextFence++
	lease := watchLease{WatchID: watchID, RuntimeKey: runtimeKey, NodeID: f.nodeID, Fence: f.nextFence, TTL: ttl}
	f.leases[watchID] = lease
	return lease, true, nil
}

func (f *fakeWatchCoordinator) Renew(context.Context, watchLease, time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.current, nil
}

func (f *fakeWatchCoordinator) Release(_ context.Context, lease watchLease) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.leases, lease.WatchID)
	return nil
}

func (f *fakeWatchCoordinator) Current(context.Context, watchLease) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.current, nil
}

func (f *fakeWatchCoordinator) ClaimEvent(context.Context, string, time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.claimEventOK, nil
}

func (f *fakeWatchCoordinator) PublishRunnerChanged(context.Context) error {
	select {
	case f.runnerCh <- struct{}{}:
	default:
	}
	return nil
}

func (f *fakeWatchCoordinator) SubscribeRunnerChanges(context.Context) <-chan struct{} {
	return f.runnerCh
}

func (f *fakeWatchCoordinator) PublishUnseen(_ context.Context, scope watchEventScope) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.publishedUnseen = append(f.publishedUnseen, scope)
	return nil
}

func (f *fakeWatchCoordinator) SubscribeUnseen(context.Context) <-chan watchEventScope {
	return f.unseenCh
}

func (f *fakeWatchCoordinator) Close() error { return nil }

func TestWatchMCPUnseenResourceAndSubscriptionNotification(t *testing.T) {
	db, svc := newSQLiteWatchService(t, 20)
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("initArtifactsBeforeCore: %v", err)
	}
	startSQLiteWatchCore(t, svc, db)
	cp := newWatchControlPlane(svc)
	ctx := artifactUserCtx("user_1")
	row, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table:     watchesRootTable,
		Operation: "insert",
		Input: map[string]interface{}{
			"name":  "mcp_watch",
			"query": cursorOrdersWatchQuery("mcp_watch"),
		},
	})
	if err != nil {
		t.Fatalf("insert watch: %v", err)
	}
	watchID := row["id"].(string)
	if _, err := db.Exec(`INSERT INTO "_graphjin_watch_events" (id, watch_id, data_hash, account_id, owner_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"evt_mcp", watchID, "hash_mcp", "acct_1", "user_1", time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatalf("insert mcp event: %v", err)
	}

	ms := svc.newMCPServerWithContext(ctx)
	readResp := ms.srv.HandleMessage(ctx, []byte(`{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"`+WatchEventsUnseenResourceURI+`"}}`))
	readSuccess, ok := readResp.(mcp.JSONRPCResponse)
	if !ok {
		t.Fatalf("read response = %#v", readResp)
	}
	readResult, ok := readSuccess.Result.(mcp.ReadResourceResult)
	if !ok {
		t.Fatalf("read result = %T %#v", readSuccess.Result, readSuccess.Result)
	}
	payload, err := watchEventsResourceText(readResult.Contents)
	if err != nil {
		t.Fatalf("decode watch resource: %v", err)
	}
	if payload.Count != 1 || payload.EventIDs[0] != "evt_mcp" {
		t.Fatalf("watch resource payload = %+v", payload)
	}
	otherPayload, err := svc.unseenWatchEventsPayload(artifactUserCtx("user_2"))
	if err != nil {
		t.Fatalf("other user unseen payload: %v", err)
	}
	if otherPayload.Count != 0 {
		t.Fatalf("other user should not see watch event: %+v", otherPayload)
	}

	session := &watchTestSession{id: "sess-1", notify: make(chan mcp.JSONRPCNotification, 2)}
	session.Initialize()
	if err := ms.srv.RegisterSession(ctx, session); err != nil {
		t.Fatalf("register session: %v", err)
	}
	subCtx := ms.srv.WithContext(ctx, session)
	resp := ms.srv.HandleMessage(subCtx, []byte(`{"jsonrpc":"2.0","id":1,"method":"resources/subscribe","params":{"uri":"`+WatchEventsUnseenResourceURI+`"}}`))
	if _, ok := resp.(mcp.JSONRPCResponse); !ok {
		t.Fatalf("subscribe response = %#v", resp)
	}
	if got := len(svc.mcpWatchSubs.matching("user_1")); got != 1 {
		t.Fatalf("matching subscriptions = %d, want 1", got)
	}
	assertWatchResourceNotification(t, session.notify)
	otherSession := &watchTestSession{id: "sess-2", notify: make(chan mcp.JSONRPCNotification, 2)}
	otherSession.Initialize()
	otherCtx := artifactUserCtx("user_2")
	if err := ms.srv.RegisterSession(otherCtx, otherSession); err != nil {
		t.Fatalf("register other session: %v", err)
	}
	otherSubCtx := ms.srv.WithContext(otherCtx, otherSession)
	resp = ms.srv.HandleMessage(otherSubCtx, []byte(`{"jsonrpc":"2.0","id":3,"method":"resources/subscribe","params":{"uri":"`+WatchEventsUnseenResourceURI+`"}}`))
	if _, ok := resp.(mcp.JSONRPCResponse); !ok {
		t.Fatalf("other subscribe response = %#v", resp)
	}
	if got := len(svc.mcpWatchSubs.matching("user_2")); got != 1 {
		t.Fatalf("other matching subscriptions = %d, want 1", got)
	}
	svc.notifyWatchEventsResource("user_1", "", watchID)
	assertWatchResourceNotification(t, session.notify)
	select {
	case n := <-otherSession.notify:
		t.Fatalf("other owner should not receive notification: %+v", n)
	default:
	}
	resp = ms.srv.HandleMessage(subCtx, []byte(`{"jsonrpc":"2.0","id":2,"method":"resources/unsubscribe","params":{"uri":"`+WatchEventsUnseenResourceURI+`"}}`))
	if _, ok := resp.(mcp.JSONRPCResponse); !ok {
		t.Fatalf("unsubscribe response = %#v", resp)
	}
	if got := len(svc.mcpWatchSubs.matching("user_1")); got != 0 {
		t.Fatalf("matching subscriptions after unsubscribe = %d, want 0", got)
	}
	var exists int
	if err := db.QueryRow(`SELECT COUNT(*) FROM "_graphjin_watches" WHERE id = ?`, watchID).Scan(&exists); err != nil {
		t.Fatalf("count watch after unsubscribe: %v", err)
	}
	if exists != 1 {
		t.Fatal("unsubscribe should not delete the watch")
	}
}

func TestWatchMCPPerWatchRoutingSameOwnerSessions(t *testing.T) {
	db, svc := newSQLiteWatchService(t, 20)
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("initArtifactsBeforeCore: %v", err)
	}
	startSQLiteWatchCore(t, svc, db)
	ctx := artifactUserCtx("user_1")
	coffeeWatchID := watchID("user_1", "coffee_roast_session_a")
	orderWatchID := watchID("user_1", "purchase_order_session_b")
	coffeeURI := watchEventsUnseenResourceURI(coffeeWatchID)
	orderURI := watchEventsUnseenResourceURI(orderWatchID)
	now := time.Now().UTC().Format(time.RFC3339)
	for _, event := range []struct {
		id      string
		watchID string
		hash    string
	}{
		{id: "evt_coffee", watchID: coffeeWatchID, hash: "hash_coffee"},
		{id: "evt_order", watchID: orderWatchID, hash: "hash_order"},
	} {
		if _, err := db.Exec(`INSERT INTO "_graphjin_watch_events" (id, watch_id, data_hash, account_id, owner_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			event.id, event.watchID, event.hash, "acct_1", "user_1", now, now); err != nil {
			t.Fatalf("insert %s: %v", event.id, err)
		}
	}

	ms := svc.newMCPServerWithContext(ctx)
	templateResp := ms.srv.HandleMessage(ctx, []byte(`{"jsonrpc":"2.0","id":1,"method":"resources/templates/list"}`))
	templateSuccess, ok := templateResp.(mcp.JSONRPCResponse)
	if !ok {
		t.Fatalf("resource templates response = %#v", templateResp)
	}
	templateResult, ok := templateSuccess.Result.(mcp.ListResourceTemplatesResult)
	if !ok {
		t.Fatalf("resource templates result = %T %#v", templateSuccess.Result, templateSuccess.Result)
	}
	var foundTemplate bool
	for _, template := range templateResult.ResourceTemplates {
		if template.URITemplate != nil && template.URITemplate.Raw() == WatchEventsUnseenResourceTemplateURI {
			foundTemplate = true
			break
		}
	}
	if !foundTemplate {
		t.Fatalf("resource templates = %+v, missing %q", templateResult.ResourceTemplates, WatchEventsUnseenResourceTemplateURI)
	}

	coffeeSession := &watchTestSession{id: "coffee-session", notify: make(chan mcp.JSONRPCNotification, 8)}
	orderSession := &watchTestSession{id: "order-session", notify: make(chan mcp.JSONRPCNotification, 8)}
	coffeeSession.Initialize()
	orderSession.Initialize()
	if err := ms.srv.RegisterSession(ctx, coffeeSession); err != nil {
		t.Fatalf("register coffee session: %v", err)
	}
	if err := ms.srv.RegisterSession(ctx, orderSession); err != nil {
		t.Fatalf("register order session: %v", err)
	}
	coffeeCtx := ms.srv.WithContext(ctx, coffeeSession)
	orderCtx := ms.srv.WithContext(ctx, orderSession)
	subscribe := func(subCtx context.Context, id int, uri string) {
		t.Helper()
		resp := ms.srv.HandleMessage(subCtx, []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"resources/subscribe","params":{"uri":%q}}`, id, uri)))
		if _, ok := resp.(mcp.JSONRPCResponse); !ok {
			t.Fatalf("subscribe %q response = %#v", uri, resp)
		}
	}
	subscribe(coffeeCtx, 2, coffeeURI)
	assertWatchResourceNotificationURI(t, coffeeSession.notify, coffeeURI)
	subscribe(coffeeCtx, 3, WatchEventsUnseenResourceURI)
	assertWatchResourceNotification(t, coffeeSession.notify)
	subscribe(orderCtx, 4, orderURI)
	assertWatchResourceNotificationURI(t, orderSession.notify, orderURI)

	readResp := ms.srv.HandleMessage(coffeeCtx, []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":5,"method":"resources/read","params":{"uri":%q}}`, coffeeURI)))
	readSuccess, ok := readResp.(mcp.JSONRPCResponse)
	if !ok {
		t.Fatalf("per-watch read response = %#v", readResp)
	}
	readResult, ok := readSuccess.Result.(mcp.ReadResourceResult)
	if !ok {
		t.Fatalf("per-watch read result = %T %#v", readSuccess.Result, readSuccess.Result)
	}
	payload, err := watchEventsResourceText(readResult.Contents)
	if err != nil {
		t.Fatalf("decode per-watch resource: %v", err)
	}
	if payload.Count != 1 || len(payload.Events) != 1 || payload.Events[0].ID != "evt_coffee" || payload.Events[0].WatchID != coffeeWatchID {
		t.Fatalf("coffee resource payload = %+v", payload)
	}
	foreignRead := ms.srv.HandleMessage(ms.effectiveIdentityContext(artifactUserCtx("user_2")), []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":6,"method":"resources/read","params":{"uri":%q}}`, coffeeURI)))
	foreignSuccess, ok := foreignRead.(mcp.JSONRPCResponse)
	if !ok {
		t.Fatalf("foreign read response = %#v", foreignRead)
	}
	foreignResult, ok := foreignSuccess.Result.(mcp.ReadResourceResult)
	if !ok {
		t.Fatalf("foreign read result = %T %#v", foreignSuccess.Result, foreignSuccess.Result)
	}
	foreignPayload, err := watchEventsResourceText(foreignResult.Contents)
	if err != nil || foreignPayload.Count != 0 {
		t.Fatalf("foreign resource payload = %+v err=%v", foreignPayload, err)
	}
	otherAccountCtx := context.WithValue(artifactUserCtx("user_1"), core.IdentityVarsKey, map[string]interface{}{
		"user_id":     "user_1",
		"user_ref":    safeArtifactIdentity("user_1", false),
		"account_id":  "acct_2",
		"account_ref": safeArtifactIdentity("acct_2", false),
	})
	otherAccountRead := ms.srv.HandleMessage(ms.effectiveIdentityContext(otherAccountCtx), []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":7,"method":"resources/read","params":{"uri":%q}}`, coffeeURI)))
	otherAccountSuccess, ok := otherAccountRead.(mcp.JSONRPCResponse)
	if !ok {
		t.Fatalf("other-account read response = %#v", otherAccountRead)
	}
	otherAccountResult, ok := otherAccountSuccess.Result.(mcp.ReadResourceResult)
	if !ok {
		t.Fatalf("other-account read result = %T %#v", otherAccountSuccess.Result, otherAccountSuccess.Result)
	}
	otherAccountPayload, err := watchEventsResourceText(otherAccountResult.Contents)
	if err != nil || otherAccountPayload.Count != 0 {
		t.Fatalf("other-account resource payload = %+v err=%v", otherAccountPayload, err)
	}

	svc.notifyWatchEventsResource("user_1", "acct_1", orderWatchID)
	assertWatchResourceNotificationURI(t, orderSession.notify, orderURI)
	assertNoWatchResourceNotification(t, coffeeSession.notify)
	svc.notifyWatchEventsResource("user_1", "acct_1", coffeeWatchID)
	assertWatchResourceNotificationURI(t, coffeeSession.notify, coffeeURI)
	assertNoWatchResourceNotification(t, orderSession.notify)

	var coffeeNotice gjagent.Response
	svc.appendWatchNotices(coffeeCtx, &coffeeNotice)
	if len(coffeeNotice.Notices) != 1 || coffeeNotice.Notices[0].Count != 1 ||
		len(coffeeNotice.Notices[0].WatchIDs) != 1 || coffeeNotice.Notices[0].WatchIDs[0] != coffeeWatchID {
		t.Fatalf("coffee session notices = %+v", coffeeNotice.Notices)
	}
	var aggregateNotice gjagent.Response
	svc.appendWatchNotices(ctx, &aggregateNotice)
	if len(aggregateNotice.Notices) != 1 || aggregateNotice.Notices[0].Count != 2 || len(aggregateNotice.Notices[0].WatchIDs) != 2 {
		t.Fatalf("aggregate notices = %+v", aggregateNotice.Notices)
	}

	unsubscribeResp := ms.srv.HandleMessage(coffeeCtx, []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":8,"method":"resources/unsubscribe","params":{"uri":%q}}`, coffeeURI)))
	if _, ok := unsubscribeResp.(mcp.JSONRPCResponse); !ok {
		t.Fatalf("unsubscribe coffee response = %#v", unsubscribeResp)
	}
	if got := len(svc.mcpWatchSubs.matching("user_1", "acct_1")); got != 2 {
		t.Fatalf("subscriptions after exact unsubscribe = %d, want aggregate coffee plus exact order", got)
	}
	svc.notifyWatchEventsResource("user_1", "acct_1", orderWatchID)
	assertWatchResourceNotification(t, coffeeSession.notify)
	assertWatchResourceNotificationURI(t, orderSession.notify, orderURI)
}

func TestWatchEventsUnseenResourceURIRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		watchID string
		suffix  string
	}{
		{watchID: "watch:0123456789abcdef", suffix: "watch%3A0123456789abcdef"},
		{watchID: "custom/watch id", suffix: "custom%2Fwatch%20id"},
	} {
		uri := watchEventsUnseenResourceURI(tc.watchID)
		if !strings.HasSuffix(uri, "/"+tc.suffix) {
			t.Fatalf("watch URI %q does not have encoded suffix %q", uri, tc.suffix)
		}
		got, ok := watchIDFromUnseenResourceURI(uri)
		if !ok || got != tc.watchID {
			t.Fatalf("watch URI round trip %q -> %q ok=%v", uri, got, ok)
		}
	}
	if _, ok := watchIDFromUnseenResourceURI("graphjin://watch-events/other"); ok {
		t.Fatal("unrelated resource URI should not parse as a watch-events resource")
	}
}

func TestWatchMCPRedisFanoutWakesMatchingLocalSession(t *testing.T) {
	db, svc := newSQLiteWatchService(t, 20)
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("initArtifactsBeforeCore: %v", err)
	}
	startSQLiteWatchCore(t, svc, db)
	fakeCoord := newFakeWatchCoordinator()
	svc.watchCoord = fakeCoord
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go svc.watchUnseenFanoutLoop(ctx, fakeCoord)

	ms := svc.newMCPServerWithContext(artifactUserCtx("user_1"))
	session := &watchTestSession{id: "fanout-1", notify: make(chan mcp.JSONRPCNotification, 2)}
	session.Initialize()
	if err := ms.srv.RegisterSession(artifactUserCtx("user_1"), session); err != nil {
		t.Fatalf("register session: %v", err)
	}
	subCtx := ms.srv.WithContext(artifactUserCtx("user_1"), session)
	watchURI := watchEventsUnseenResourceURI("watch_remote")
	resp := ms.srv.HandleMessage(subCtx, []byte(`{"jsonrpc":"2.0","id":1,"method":"resources/subscribe","params":{"uri":"`+WatchEventsUnseenResourceURI+`"}}`))
	if _, ok := resp.(mcp.JSONRPCResponse); !ok {
		t.Fatalf("subscribe response = %#v", resp)
	}
	resp = ms.srv.HandleMessage(subCtx, []byte(`{"jsonrpc":"2.0","id":2,"method":"resources/subscribe","params":{"uri":"`+watchURI+`"}}`))
	if _, ok := resp.(mcp.JSONRPCResponse); !ok {
		t.Fatalf("exact subscribe response = %#v", resp)
	}

	fakeCoord.unseenCh <- watchEventScope{OwnerID: "user_1", AccountID: "acct_1", WatchID: "watch_remote", SourceNodeID: "node-b"}
	assertWatchResourceNotificationURI(t, session.notify, watchURI)

	// A legacy publisher has no watch_id, so rolling upgrades retain the
	// aggregate wakeup instead of guessing an exact watch resource.
	fakeCoord.unseenCh <- watchEventScope{OwnerID: "user_1", AccountID: "acct_1", SourceNodeID: "node-b"}
	assertWatchResourceNotification(t, session.notify)

	fakeCoord.unseenCh <- watchEventScope{OwnerID: "user_2", AccountID: "acct_1", SourceNodeID: "node-b"}
	select {
	case n := <-session.notify:
		t.Fatalf("unrelated owner should not receive fanout notification: %+v", n)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestWatchMCPSubscriptionRegistryScopesSessionByIdentity(t *testing.T) {
	db, svc := newSQLiteWatchService(t, 20)
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("initArtifactsBeforeCore: %v", err)
	}
	startSQLiteWatchCore(t, svc, db)
	user1 := artifactUserCtx("user_1")
	user2 := artifactUserCtx("user_2")
	ms := svc.newMCPServerWithContext(user1)
	session := &watchTestSession{id: "shared-session", notify: make(chan mcp.JSONRPCNotification, 2)}
	session.Initialize()
	if err := ms.srv.RegisterSession(user1, session); err != nil {
		t.Fatalf("register session: %v", err)
	}

	resp := ms.srv.HandleMessage(ms.srv.WithContext(user1, session), []byte(`{"jsonrpc":"2.0","id":1,"method":"resources/subscribe","params":{"uri":"`+WatchEventsUnseenResourceURI+`"}}`))
	if _, ok := resp.(mcp.JSONRPCResponse); !ok {
		t.Fatalf("user1 subscribe response = %#v", resp)
	}
	resp = ms.srv.HandleMessage(ms.srv.WithContext(user1, session), []byte(`{"jsonrpc":"2.0","id":2,"method":"resources/subscribe","params":{"uri":"`+WatchEventsUnseenResourceURI+`/watch:unencoded"}}`))
	if _, ok := resp.(mcp.JSONRPCResponse); !ok {
		t.Fatalf("non-canonical subscribe response = %#v", resp)
	}
	if got := len(svc.mcpWatchSubs.matching("user_1")); got != 1 {
		t.Fatalf("user1 subscriptions after non-canonical URI = %d, want aggregate only", got)
	}
	resp = ms.srv.HandleMessage(ms.srv.WithContext(user2, session), []byte(`{"jsonrpc":"2.0","id":3,"method":"resources/subscribe","params":{"uri":"`+WatchEventsUnseenResourceURI+`"}}`))
	if _, ok := resp.(mcp.JSONRPCResponse); !ok {
		t.Fatalf("user2 subscribe response = %#v", resp)
	}
	for id, watchID := range []string{"watch:session-a", "watch:session-b"} {
		uri := watchEventsUnseenResourceURI(watchID)
		resp = ms.srv.HandleMessage(ms.srv.WithContext(user2, session), []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"resources/subscribe","params":{"uri":%q}}`, id+4, uri)))
		if _, ok := resp.(mcp.JSONRPCResponse); !ok {
			t.Fatalf("user2 exact subscribe %q response = %#v", uri, resp)
		}
	}
	if got := len(svc.mcpWatchSubs.matching("user_1")); got != 1 {
		t.Fatalf("user1 subscriptions = %d, want 1", got)
	}
	if got := len(svc.mcpWatchSubs.matching("user_2")); got != 3 {
		t.Fatalf("user2 subscriptions = %d, want aggregate plus two exact subscriptions", got)
	}

	resp = ms.srv.HandleMessage(ms.srv.WithContext(user1, session), []byte(`{"jsonrpc":"2.0","id":6,"method":"resources/unsubscribe","params":{"uri":"`+WatchEventsUnseenResourceURI+`"}}`))
	if _, ok := resp.(mcp.JSONRPCResponse); !ok {
		t.Fatalf("user1 unsubscribe response = %#v", resp)
	}
	if got := len(svc.mcpWatchSubs.matching("user_1")); got != 0 {
		t.Fatalf("user1 subscriptions after unsubscribe = %d, want 0", got)
	}
	if got := len(svc.mcpWatchSubs.matching("user_2")); got != 3 {
		t.Fatalf("user2 subscriptions after user1 unsubscribe = %d, want 3", got)
	}
	ms.srv.UnregisterSession(user2, session.SessionID())
	if got := len(svc.mcpWatchSubs.matching("user_2")); got != 0 {
		t.Fatalf("user2 subscriptions after session unregister = %d, want 0", got)
	}
}

func TestWatchPersistSkipsWritesWhenLeaseIsStale(t *testing.T) {
	db, svc := newSQLiteWatchService(t, 20)
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("initArtifactsBeforeCore: %v", err)
	}
	startSQLiteWatchCore(t, svc, db)
	cp := newWatchControlPlane(svc)
	ctx := artifactUserCtx("user_1")
	row, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table:     watchesRootTable,
		Operation: "insert",
		Input: map[string]interface{}{
			"name":  "stale_lease",
			"query": cursorOrdersWatchQuery("stale_lease"),
		},
	})
	if err != nil {
		t.Fatalf("insert watch: %v", err)
	}
	fakeCoord := newFakeWatchCoordinator()
	fakeCoord.current = false
	svc.watchCoord = fakeCoord
	def := watchRuntimeDefinition{
		ID:        row["id"].(string),
		Name:      "stale_lease",
		AccountID: "acct_1",
		OwnerID:   "user_1",
		OwnerRole: "analyst",
		lease:     watchLease{WatchID: row["id"].(string), RuntimeKey: "stale", NodeID: fakeCoord.NodeID(), Fence: 1, TTL: time.Minute},
	}
	_, inserted, err := svc.persistWatchResult(ctx, &def, &core.Result{Data: json.RawMessage(`{"data":{"orders":[{"id":1}]}}`)})
	if err != nil {
		t.Fatalf("persist stale lease result: %v", err)
	}
	if inserted {
		t.Fatal("stale lease should not insert watch event")
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM "_graphjin_watch_events"`).Scan(&count); err != nil {
		t.Fatalf("count watch events: %v", err)
	}
	if count != 0 {
		t.Fatalf("watch events = %d, want 0", count)
	}
}

func TestWatchRedisEventDedupeHintDoesNotDropMissingEvent(t *testing.T) {
	db, svc := newSQLiteWatchService(t, 20)
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("initArtifactsBeforeCore: %v", err)
	}
	startSQLiteWatchCore(t, svc, db)
	cp := newWatchControlPlane(svc)
	ctx := artifactUserCtx("user_1")
	row, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table:     watchesRootTable,
		Operation: "insert",
		Input: map[string]interface{}{
			"name":  "dedupe_hint",
			"query": cursorOrdersWatchQuery("dedupe_hint"),
		},
	})
	if err != nil {
		t.Fatalf("insert watch: %v", err)
	}
	fakeCoord := newFakeWatchCoordinator()
	fakeCoord.claimEventOK = false
	svc.watchCoord = fakeCoord
	def := watchRuntimeDefinition{
		ID:        row["id"].(string),
		Name:      "dedupe_hint",
		AccountID: "acct_1",
		OwnerID:   "user_1",
		OwnerRole: "analyst",
	}
	_, inserted, err := svc.persistWatchResult(ctx, &def, &core.Result{Data: json.RawMessage(`{"data":{"orders":[{"id":2}]}}`)})
	if err != nil {
		t.Fatalf("persist dedupe result: %v", err)
	}
	if !inserted {
		t.Fatal("Redis dedupe hint should not drop event missing from DB")
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM "_graphjin_watch_events"`).Scan(&count); err != nil {
		t.Fatalf("count watch events: %v", err)
	}
	if count != 1 {
		t.Fatalf("watch events = %d, want 1", count)
	}
}

func TestWatchNotifyPublishesRedisFanoutScope(t *testing.T) {
	_, svc := newSQLiteWatchService(t, 20)
	fakeCoord := newFakeWatchCoordinator()
	svc.watchCoord = fakeCoord
	svc.notifyWatchEventsResource("user_1", "acct_1", "watch_1")
	fakeCoord.mu.Lock()
	defer fakeCoord.mu.Unlock()
	if len(fakeCoord.publishedUnseen) != 1 {
		t.Fatalf("published unseen = %+v, want one scope", fakeCoord.publishedUnseen)
	}
	scope := fakeCoord.publishedUnseen[0]
	if scope.OwnerID != "user_1" || scope.AccountID != "acct_1" || scope.WatchID != "watch_1" || scope.SourceNodeID != "" {
		t.Fatalf("published scope = %+v", scope)
	}
}

func TestWatchMigrationAddsOwnerRole(t *testing.T) {
	dsn := "file:" + t.TempDir() + "/old-watches.db"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE "_graphjin_watches" (
id TEXT PRIMARY KEY,
name TEXT NOT NULL,
description TEXT NOT NULL DEFAULT '',
query TEXT NOT NULL DEFAULT '',
saved_query_name TEXT NOT NULL DEFAULT '',
variables_json TEXT,
condition_js TEXT NOT NULL DEFAULT '',
delivery_json TEXT,
enrich_json TEXT,
evidence_json TEXT,
status TEXT NOT NULL DEFAULT 'active',
approval TEXT NOT NULL DEFAULT 'approved',
enabled INTEGER NOT NULL DEFAULT 1,
account_id TEXT NOT NULL DEFAULT '',
owner_id TEXT NOT NULL DEFAULT '',
last_data_hash TEXT NOT NULL DEFAULT '',
last_fired_at TEXT,
last_error TEXT NOT NULL DEFAULT '',
failure_count INTEGER NOT NULL DEFAULT 0,
created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
)`); err != nil {
		t.Fatalf("create old watches table: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE "_graphjin_watch_events" (
id TEXT PRIMARY KEY,
watch_id TEXT NOT NULL,
data_hash TEXT NOT NULL DEFAULT '',
data_json TEXT,
evidence_json TEXT,
delivery_status TEXT NOT NULL DEFAULT 'pending',
delivery_attempts INTEGER NOT NULL DEFAULT 0,
delivery_json TEXT,
receipt_json TEXT,
enrichment_json TEXT,
seen INTEGER NOT NULL DEFAULT 0,
seen_at TEXT,
account_id TEXT NOT NULL DEFAULT '',
owner_id TEXT NOT NULL DEFAULT '',
created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
)`); err != nil {
		t.Fatalf("create old watch events table: %v", err)
	}
	_, svc := newSQLiteWatchServiceWithDB(t, db, dsn, 20)
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("initArtifactsBeforeCore: %v", err)
	}
	if ok, err := sqliteColumnExists(artifactUserCtx("user_1"), db, quoteSQLIdent("_graphjin_watches"), "owner_role"); err != nil {
		t.Fatalf("check migrated owner_role column: %v", err)
	} else if !ok {
		t.Fatal("expected owner_role column to be added")
	}
	if ok, err := sqliteColumnExists(artifactUserCtx("user_1"), db, quoteSQLIdent("_graphjin_watch_events"), "data_truncated"); err != nil {
		t.Fatalf("check migrated data_truncated column: %v", err)
	} else if !ok {
		t.Fatal("expected data_truncated column to be added")
	}
	if ok, err := sqliteColumnExists(artifactUserCtx("user_1"), db, quoteSQLIdent("_graphjin_watches"), "last_cursor_json"); err != nil {
		t.Fatalf("check migrated last_cursor_json column: %v", err)
	} else if !ok {
		t.Fatal("expected last_cursor_json column to be added")
	}
}

func TestUpsertWatchRejectsOversizedDefinitionJSON(t *testing.T) {
	db, svc := newSQLiteWatchService(t, 5)
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("initArtifactsBeforeCore: %v", err)
	}
	startSQLiteWatchCore(t, svc, db)

	cp := newWatchControlPlane(svc)
	ctx := artifactUserCtx("user_1")
	maxBytes := svc.conf.Core.EffectiveWatchesConfig().SnapshotMaxBytes
	oversized := `{"pad":"` + strings.Repeat("x", maxBytes) + `"}`
	if _, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table:     watchesRootTable,
		Operation: "insert",
		Input: map[string]interface{}{
			"name":          "big_delivery",
			"query":         cursorOrdersWatchQuery("big_delivery"),
			"delivery_json": oversized,
		},
	}); err == nil || !strings.Contains(err.Error(), "snapshot_max_bytes") {
		t.Fatalf("oversized delivery_json error = %v", err)
	}

	if _, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table:     watchesRootTable,
		Operation: "insert",
		Input: map[string]interface{}{
			"name":  "small_watch",
			"query": cursorOrdersWatchQuery("small_watch"),
		},
	}); err != nil {
		t.Fatalf("normal watch insert: %v", err)
	}
}

func newSQLiteWatchService(t *testing.T, maxPerOwner int) (*sql.DB, *graphjinService) {
	t.Helper()
	dsn := "file:" + t.TempDir() + "/watches.db"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return newSQLiteWatchServiceWithDB(t, db, dsn, maxPerOwner)
}

func newSQLiteWatchServiceWithDB(t *testing.T, db *sql.DB, dsn string, maxPerOwner int) (*sql.DB, *graphjinService) {
	t.Helper()
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS orders (id INTEGER PRIMARY KEY, status TEXT, updated_at TEXT)`); err != nil {
		t.Fatalf("create orders table: %v", err)
	}
	autoInit := true
	conf := &Config{Core: core.Config{
		Sources: []core.SourceConfig{
			{Name: "app", Kind: "database", Type: "sqlite", Path: dsn, Default: true, Access: core.SourceAccessConfig{Read: core.AccessModeAuthenticated}},
		},
		Artifacts: core.ArtifactsConfig{Enabled: true, Source: "app", AutoInit: &autoInit, GlobalsPath: "."},
		Watches:   core.WatchesConfig{Enabled: true, MaxPerOwner: maxPerOwner},
		Roles:     []core.Role{{Name: "analyst"}},
	}}
	conf.ConfigPath = t.TempDir()
	if err := conf.Core.NormalizeSources(); err != nil {
		t.Fatalf("NormalizeSources: %v", err)
	}
	return db, &graphjinService{
		conf: conf,
		dbs:  map[string]*sql.DB{"app": db},
		fs:   newAferoFS(afero.NewMemMapFs(), "/"),
	}
}

func startSQLiteWatchCore(t *testing.T, svc *graphjinService, db *sql.DB) {
	t.Helper()
	svc.metadataDB = "app"
	svc.conf.Core.FS = svc.fs
	svc.injectInternalStoreRole()
	artifacts := newArtifactControlPlane(svc)
	watches := newWatchControlPlane(svc)
	opts := []core.Option{
		core.OptionSetFS(svc.fs),
		core.OptionSetDatabases(svc.dbs),
		core.OptionSetSavedQuerySaveHook(svc.saveSavedQueryArtifactOrFallback),
		core.OptionSetReservedRoleAuthorizer(svc.authorizeReservedRole),
		core.OptionSetManagedQueryHandler("app", artifacts),
		core.OptionSetManagedMutationHandler("app", artifacts),
		core.OptionSetManagedQueryHandler("app", watches),
		core.OptionSetManagedMutationHandler("app", watches),
	}
	gj, err := core.NewGraphJin(&svc.conf.Core, db, opts...)
	if err != nil {
		t.Fatalf("start GraphJin core: %v", err)
	}
	svc.gj = gj
}

func approveWatchActionForTest(
	t *testing.T,
	cp watchControlPlane,
	ctx context.Context,
	watch map[string]any,
) map[string]any {
	t.Helper()
	watchID := stringFromAny(watch["id"])
	actionHash := stringFromAny(watch["action_hash"])
	if watchID == "" || actionHash == "" || watch["action_approval"] != watchReviewPending {
		t.Fatalf("watch is missing a pending action proposal: %+v", watch)
	}
	reviewed, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table:     watchesRootTable,
		Operation: "update",
		Where:     map[string]interface{}{"id": map[string]interface{}{"eq": watchID}},
		Input: map[string]interface{}{
			"action_review_json": map[string]any{
				"decision":             "approve",
				"expected_action_hash": actionHash,
			},
		},
	})
	if err != nil {
		t.Fatalf("approve watch action: %v", err)
	}
	if reviewed["action_approval"] != watchReviewApproved || reviewed["approval"] != watchReviewApproved {
		t.Fatalf("watch action was not approved: %+v", reviewed)
	}
	return reviewed
}

func insertWatchEventFixture(t *testing.T, svc *graphjinService, ctx context.Context, id, watchID, createdAt string) {
	t.Helper()
	if _, err := svc.internalStoreMutationRows(ctx, "watch_events", `insert: $input`, watchEventStoreFields, map[string]any{"input": map[string]any{
		"id":                id,
		"watch_id":          watchID,
		"data_hash":         id + "_hash",
		"data_json":         nullableJSONString(`{"fixture":true}`),
		"data_truncated":    false,
		"evidence_json":     nullableJSONString(`{}`),
		"delivery_status":   "pending",
		"delivery_attempts": 0,
		"delivery_json":     nullableJSONString(`{"kind":"inbox"}`),
		"receipt_json":      nil,
		"enrichment_json":   nil,
		"seen":              false,
		"account_id":        "acct_1",
		"owner_id":          "user_1",
		"created_at":        createdAt,
		"updated_at":        createdAt,
	}}); err != nil {
		t.Fatalf("insert watch event fixture %s: %v", id, err)
	}
}

func watchEventIDs(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`SELECT id FROM "_graphjin_watch_events" ORDER BY id`)
	if err != nil {
		t.Fatalf("query watch event ids: %v", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan watch event id: %v", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate watch event ids: %v", err)
	}
	return ids
}

func contextWithUserRole(ctx context.Context, role string) context.Context {
	ctx = context.WithValue(ctx, core.UserRoleKey, role)
	return context.WithValue(ctx, core.IdentityRolesKey, []string{role})
}

func watchWebhookSecurityPolicyForTest(t *testing.T, conf *Config) securityPolicyEval {
	t.Helper()
	for _, row := range securityPolicyEvaluations(conf, "agentic") {
		if row.ID == "policy:serve.watch_webhook_egress" {
			return row
		}
	}
	t.Fatal("missing watch webhook security policy")
	return securityPolicyEval{}
}

func TestStringWhereUnwrapsOperatorMap(t *testing.T) {
	where := map[string]interface{}{"id": map[string]interface{}{"eq": "we:abc"}}
	if got := stringWhere(where, "id"); got != "we:abc" {
		t.Fatalf("stringWhere eq-map = %q, want we:abc", got)
	}
	if got := stringWhere(map[string]interface{}{"id": "plain"}, "id"); got != "plain" {
		t.Fatalf("stringWhere scalar = %q, want plain", got)
	}
	if got := stringWhere(map[string]interface{}{"id": map[string]interface{}{"in": []string{"a"}}}, "id"); got != "" {
		t.Fatalf("stringWhere non-eq operator = %q, want empty", got)
	}
}
