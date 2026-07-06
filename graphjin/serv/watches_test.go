package serv

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/dosco/graphjin/core/v3"
	"github.com/spf13/afero"
	_ "modernc.org/sqlite"
)

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

	cp := newWatchControlPlane(svc)
	ctx := contextWithUserRole(artifactUserCtx("user_1"), "analyst")
	row, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table:     watchesRootTable,
		Operation: "insert",
		Input: map[string]interface{}{
			"name":       "new_orders",
			"owner_role": "admin",
			"query":      `subscription new_orders_watch { orders { id status } }`,
		},
	})
	if err != nil {
		t.Fatalf("insert watch: %v", err)
	}
	watchID, _ := row["id"].(string)
	if watchID == "" {
		t.Fatalf("watch insert returned no id: %+v", row)
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
			"name":  "second_watch",
			"query": `subscription second_watch { orders { id } }`,
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
			"query":       `subscription updated_orders_watch { orders { id status updated_at } }`,
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
			"query":         `subscription new_orders_watch { orders { id status } }`,
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
		Query:        `subscription new_orders_watch { orders { id status } }`,
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
		Query:     `subscription large_orders_watch { orders { id status } }`,
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
			"query": `subscription webhook_orders { orders { id status } }`,
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
	def := watchRuntimeDefinition{
		ID:           row["id"].(string),
		Name:         "webhook_orders",
		Query:        `subscription webhook_orders { orders { id status } }`,
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
			"query":         `subscription workflow_orders { orders { id status } }`,
			"delivery_json": map[string]any{"kind": "workflow", "name": "notify"},
		},
	})
	if err != nil {
		t.Fatalf("insert watch: %v", err)
	}
	def := watchRuntimeDefinition{
		ID:           row["id"].(string),
		Name:         "workflow_orders",
		Query:        `subscription workflow_orders { orders { id status } }`,
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
			"query":       `subscription enriched_orders { orders { id status } }`,
			"enrich_json": map[string]any{"enabled": true, "instruction": "summarize", "max_steps": 99},
		},
	})
	if err != nil {
		t.Fatalf("insert watch: %v", err)
	}
	def := watchRuntimeDefinition{
		ID:         row["id"].(string),
		Name:       "enriched_orders",
		Query:      `subscription enriched_orders { orders { id status } }`,
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
			"query":         `subscription cap_orders { orders { id status } }`,
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
		Query:        `subscription cap_orders { orders { id status } }`,
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
			"query":         `subscription big_delivery { orders { id status } }`,
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
			"query": `subscription small_watch { orders { id status } }`,
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
			{Name: "app", Kind: "database", Type: "sqlite", Path: dsn, Default: true},
			{Name: "graphjin", Kind: "graphjin"},
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
