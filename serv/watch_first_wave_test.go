package serv

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dosco/graphjin/core/v3"
)

func TestWatchRetryBackoffSchedule(t *testing.T) {
	t.Parallel()
	cases := []struct {
		count int64
		want  time.Duration
	}{
		{count: 0, want: time.Second},
		{count: 1, want: time.Second},
		{count: 2, want: 2 * time.Second},
		{count: 3, want: 4 * time.Second},
		{count: 20, want: 5 * time.Minute},
	}
	for _, tc := range cases {
		if got := watchRetryBackoff(time.Second, tc.count); got != tc.want {
			t.Fatalf("watchRetryBackoff(1s, %d) = %s, want %s", tc.count, got, tc.want)
		}
	}
}

func TestWatchRetryMaxFailuresDefault(t *testing.T) {
	t.Parallel()
	cfg := (&core.Config{Watches: core.WatchesConfig{Enabled: true}}).EffectiveWatchesConfig()
	if cfg.RetryMaxFailures != 5 {
		t.Fatalf("RetryMaxFailures = %d, want 5", cfg.RetryMaxFailures)
	}
	var nilConfig *core.Config
	if cfg = nilConfig.EffectiveWatchesConfig(); cfg.RetryMaxFailures != 5 {
		t.Fatalf("nil config RetryMaxFailures = %d, want 5", cfg.RetryMaxFailures)
	}
}

func TestWatchRunnerTransientErrorKeepsStatusActiveThenTerminates(t *testing.T) {
	db, svc := newSQLiteWatchService(t, 20)
	svc.conf.Core.Watches.RetryMaxFailures = 2
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("initArtifactsBeforeCore: %v", err)
	}
	startSQLiteWatchCore(t, svc, db)
	cp := newWatchControlPlane(svc)
	ctx := contextWithUserRole(artifactUserCtx("user_1"), "analyst")
	row, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table: watchesRootTable, Operation: "insert",
		Input: map[string]any{"name": "retry_watch", "query": cursorOrdersWatchQuery("retry_watch")},
	})
	if err != nil {
		t.Fatalf("insert watch: %v", err)
	}
	id := stringFromAny(row["id"])
	def := watchRuntimeDefinition{ID: id, OwnerID: "user_1"}

	if _, terminal := svc.updateWatchRunnerTransientError(ctx, def, errors.New("temporary outage")); terminal {
		t.Fatal("first transient failure unexpectedly terminal")
	}
	var status, evidenceJSON string
	var failures int64
	if err := db.QueryRow(
		`SELECT status, failure_count, evidence_json FROM "_graphjin_watches" WHERE id = ?`, id,
	).Scan(&status, &failures, &evidenceJSON); err != nil {
		t.Fatalf("read retry state: %v", err)
	}
	if status != "active" || failures != 1 {
		t.Fatalf("retry state status=%q failures=%d, want active/1", status, failures)
	}
	retry := mapFromAny(mapFromAny(parseJSONValue(evidenceJSON))["retry"])
	if intFromAny(retry["count"]) != 1 || intFromAny(retry["backoff_ms"]) != 1000 ||
		stringFromAny(retry["next_retry_at"]) == "" {
		t.Fatalf("retry evidence = %+v", retry)
	}

	if _, terminal := svc.updateWatchRunnerTransientError(ctx, def, errors.New("still unavailable")); !terminal {
		t.Fatal("second consecutive failure should be terminal")
	}
	if err := db.QueryRow(
		`SELECT status, failure_count FROM "_graphjin_watches" WHERE id = ?`, id,
	).Scan(&status, &failures); err != nil {
		t.Fatalf("read terminal state: %v", err)
	}
	if status != "error" || failures != 2 {
		t.Fatalf("terminal state status=%q failures=%d, want error/2", status, failures)
	}
}

func TestWatchSubscriptionSessionExitsOnMemberDone(t *testing.T) {
	db, svc := newSQLiteWatchService(t, 20)
	svc.conf.SubsPollDuration = 100 * time.Millisecond
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("initArtifactsBeforeCore: %v", err)
	}
	startSQLiteWatchCore(t, svc, db)
	cp := newWatchControlPlane(svc)
	ctx := contextWithUserRole(artifactUserCtx("user_1"), "analyst")
	row, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table: watchesRootTable, Operation: "insert",
		Input: map[string]any{"name": "done_watch", "query": cursorOrdersWatchQuery("done_watch")},
	})
	if err != nil {
		t.Fatalf("insert watch: %v", err)
	}
	def := watchRuntimeDefinition{
		ID: stringFromAny(row["id"]), Name: "done_watch", Query: cursorOrdersWatchQuery("done_watch"),
		OwnerID: "user_1", OwnerRole: "analyst", Lifecycle: "durable",
	}
	member, err := svc.subscribeWatchMember(ctx, def)
	if err != nil {
		t.Fatalf("subscribe watch member: %v", err)
	}
	result := make(chan watchSessionExit, 1)
	go func() {
		exit, _ := svc.watchSubscriptionSession(ctx, &def, member)
		result <- exit
	}()
	svc.gj.Close()
	select {
	case exit := <-result:
		if exit != watchSessionReconnect {
			t.Fatalf("session exit = %v, want reconnect", exit)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("watch session did not exit after Member.Done")
	}
}

func TestWatchRunnerResubscribesAfterMemberDone(t *testing.T) {
	db, svc := newSQLiteWatchService(t, 20)
	svc.conf.SubsPollDuration = 100 * time.Millisecond
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("initArtifactsBeforeCore: %v", err)
	}
	startSQLiteWatchCore(t, svc, db)
	firstCore := svc.gj
	startSQLiteWatchCore(t, svc, db)
	cp := newWatchControlPlane(svc)
	ownerCtx := contextWithUserRole(artifactUserCtx("user_1"), "analyst")
	row, err := cp.mutateRow(ownerCtx, core.ManagedMutationRoot{
		Table: watchesRootTable, Operation: "insert",
		Input: map[string]any{"name": "reconnect_watch", "query": cursorOrdersWatchQuery("reconnect_watch")},
	})
	if err != nil {
		t.Fatalf("insert watch: %v", err)
	}
	def := watchRuntimeDefinition{
		ID: stringFromAny(row["id"]), Name: "reconnect_watch", Query: cursorOrdersWatchQuery("reconnect_watch"),
		OwnerID: "user_1", OwnerRole: "analyst", Lifecycle: "durable",
	}
	ctx, cancel := context.WithCancel(ownerCtx)
	defer cancel()
	var calls atomic.Int32
	svc.watchSubscribeForTest = func(ctx context.Context, def watchRuntimeDefinition, vars json.RawMessage) (*core.Member, error) {
		if calls.Load() == 0 {
			member, err := firstCore.Subscribe(ctx, def.Query, vars, nil)
			calls.Store(1)
			return member, err
		}
		calls.Add(1)
		cancel()
		return nil, context.Canceled
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.runWatchSubscription(ctx, def)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if calls.Load() == 0 {
		t.Fatal("initial watch subscription did not start")
	}
	firstCore.Close()
	select {
	case <-done:
	case <-time.After(4 * time.Second):
		t.Fatal("watch runner did not reconnect after Member.Done")
	}
	if calls.Load() < 2 {
		t.Fatalf("subscribe calls = %d, want at least 2", calls.Load())
	}
}

func TestNormalizeWatchAbsenceJSON(t *testing.T) {
	t.Parallel()
	got, cfg, enabled, err := normalizeWatchAbsenceJSON(`{"window":"10s","enabled":true,"repeat":true}`)
	if err != nil {
		t.Fatalf("normalize absence: %v", err)
	}
	if !enabled || cfg.Window != time.Minute || !cfg.Repeat ||
		got != `{"enabled":true,"repeat":true,"window":"1m0s"}` {
		t.Fatalf("normalized absence = %q %+v enabled=%v", got, cfg, enabled)
	}
	got, cfg, enabled, err = normalizeWatchAbsenceJSON(`{"enabled":true,"window":"2160h"}`)
	if err != nil || !enabled || cfg.Window != 30*24*time.Hour ||
		got != `{"enabled":true,"repeat":false,"window":"720h0m0s"}` {
		t.Fatalf("capped absence = %q %+v enabled=%v err=%v", got, cfg, enabled, err)
	}
	if got, _, enabled, err = normalizeWatchAbsenceJSON(`{"enabled":false,"window":"1h"}`); err != nil || enabled || got != "" {
		t.Fatalf("disabled absence = %q enabled=%v err=%v", got, enabled, err)
	}
}

func TestWatchAbsenceFiresOnceRearmsAndHonorsDowntimeGrace(t *testing.T) {
	db, svc := newSQLiteWatchService(t, 20)
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("initArtifactsBeforeCore: %v", err)
	}
	startSQLiteWatchCore(t, svc, db)
	cp := newWatchControlPlane(svc)
	ctx := contextWithUserRole(artifactUserCtx("user_1"), "analyst")
	row, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table: watchesRootTable, Operation: "insert",
		Input: map[string]any{
			"name": "absence_watch", "query": cursorOrdersWatchQuery("absence_watch"),
			"absence_json": map[string]any{"enabled": true, "window": "1m", "repeat": false},
		},
	})
	if err != nil {
		t.Fatalf("insert absence watch: %v", err)
	}
	def := watchRuntimeDefinition{
		ID: stringFromAny(row["id"]), Name: "absence_watch", Query: cursorOrdersWatchQuery("absence_watch"),
		AbsenceJSON: watchTestJSONString(row["absence_json"]), OwnerID: "user_1", OwnerRole: "analyst",
	}
	now := time.Now().UTC()
	if fired, err := svc.sweepWatchAbsence(ctx, &def, now.Add(-2*time.Minute), now); err != nil || !fired {
		t.Fatalf("first absence sweep fired=%v err=%v", fired, err)
	}
	if fired, err := svc.sweepWatchAbsence(ctx, &def, now.Add(-2*time.Minute), now.Add(2*time.Minute)); err != nil || fired {
		t.Fatalf("fire-once sweep fired=%v err=%v", fired, err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM "_graphjin_watch_events" WHERE watch_id = ?`, def.ID).Scan(&count); err != nil {
		t.Fatalf("count absence events: %v", err)
	}
	if count != 1 {
		t.Fatalf("absence event count = %d, want 1", count)
	}
	realDataAt := now.Add(3 * time.Minute)
	if _, err := db.Exec(
		`UPDATE "_graphjin_watches" SET last_fired_at = ?, updated_at = ? WHERE id = ?`,
		realDataAt.Format(time.RFC3339Nano), realDataAt.Format(time.RFC3339Nano), def.ID,
	); err != nil {
		t.Fatalf("re-arm absence watch: %v", err)
	}
	if fired, err := svc.sweepWatchAbsence(ctx, &def, now.Add(-2*time.Minute), realDataAt.Add(2*time.Minute)); err != nil || !fired {
		t.Fatalf("re-armed absence sweep fired=%v err=%v", fired, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM "_graphjin_watch_events" WHERE watch_id = ?`, def.ID).Scan(&count); err != nil {
		t.Fatalf("count re-armed absence events: %v", err)
	}
	if count != 2 {
		t.Fatalf("re-armed absence event count = %d, want 2", count)
	}

	graceRow, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table: watchesRootTable, Operation: "insert",
		Input: map[string]any{
			"name": "grace_watch", "query": cursorOrdersWatchQuery("grace_watch"),
			"absence_json": map[string]any{"enabled": true, "window": "1m"},
		},
	})
	if err != nil {
		t.Fatalf("insert grace watch: %v", err)
	}
	graceDef := watchRuntimeDefinition{
		ID: stringFromAny(graceRow["id"]), Name: "grace_watch", Query: cursorOrdersWatchQuery("grace_watch"),
		AbsenceJSON: watchTestJSONString(graceRow["absence_json"]), OwnerID: "user_1", OwnerRole: "analyst",
	}
	if fired, err := svc.sweepWatchAbsence(ctx, &graceDef, now, now.Add(30*time.Second)); err != nil || fired {
		t.Fatalf("downtime grace sweep fired=%v err=%v", fired, err)
	}
}

func TestWatchAbsenceChangesActionHashOnlyWhenConfigured(t *testing.T) {
	svc := &graphjinService{}
	query := cursorOrdersWatchQuery("absence_hash")
	delivery := `{"kind":"webhook","url":"https://example.com/hook"}`
	base, err := svc.watchActionProposal(context.Background(), query, "", "", "", delivery, "")
	if err != nil {
		t.Fatalf("base action proposal: %v", err)
	}
	same, err := svc.watchActionProposal(context.Background(), query, "", "", "", delivery, `{"enabled":false,"window":"1h"}`)
	if err != nil {
		t.Fatalf("disabled absence proposal: %v", err)
	}
	changed, err := svc.watchActionProposal(context.Background(), query, "", "", "", delivery, `{"enabled":true,"window":"1h"}`)
	if err != nil {
		t.Fatalf("enabled absence proposal: %v", err)
	}
	if same.Hash != base.Hash || changed.Hash == base.Hash {
		t.Fatalf("action hashes base=%s disabled=%s enabled=%s", base.Hash, same.Hash, changed.Hash)
	}
}

func TestNormalizeWatchDeliveryPreservesDigest(t *testing.T) {
	t.Parallel()
	got, cfg, action, err := normalizeWatchDeliveryJSON(`{"kind":"inbox","digest":{"window":"10s"}}`)
	if err != nil {
		t.Fatalf("normalize inbox digest: %v", err)
	}
	if action || !cfg.Digest.Enabled || cfg.Digest.Window != time.Minute ||
		got != `{"digest":{"window":"1m0s"},"kind":"inbox"}` {
		t.Fatalf("inbox digest = %q %+v action=%v", got, cfg, action)
	}
	got, cfg, action, err = normalizeWatchDeliveryJSON(`{"kind":"webhook","url":"https://example.com/hook","digest":{"window":"720h"}}`)
	if err != nil {
		t.Fatalf("normalize action digest: %v", err)
	}
	if !action || cfg.Digest.Window != 7*24*time.Hour || !strings.Contains(got, `"digest":{"window":"168h0m0s"}`) {
		t.Fatalf("action digest = %q %+v action=%v", got, cfg, action)
	}
	got, cfg, action, err = normalizeWatchDeliveryJSON(`{"digest":{"window":"1h"}}`)
	if err != nil {
		t.Fatalf("normalize digest with default inbox kind: %v", err)
	}
	if action || cfg.Kind != "inbox" || cfg.Digest.Window != time.Hour ||
		got != `{"digest":{"window":"1h0m0s"},"kind":"inbox"}` {
		t.Fatalf("default inbox digest = %q %+v action=%v", got, cfg, action)
	}
}

func watchTestJSONString(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	data, _ := json.Marshal(value)
	return string(data)
}

func TestWatchDigestFlushCoalescesAndIsIdempotent(t *testing.T) {
	db, svc := newSQLiteWatchService(t, 20)
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("initArtifactsBeforeCore: %v", err)
	}
	startSQLiteWatchCore(t, svc, db)
	cp := newWatchControlPlane(svc)
	ctx := contextWithUserRole(artifactUserCtx("user_1"), "analyst")
	row, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table: watchesRootTable, Operation: "insert",
		Input: map[string]any{
			"name": "digest_watch", "query": cursorOrdersWatchQuery("digest_watch"),
			"delivery_json": map[string]any{"kind": "inbox", "digest": map[string]any{"window": "1m"}},
		},
	})
	if err != nil {
		t.Fatalf("insert digest watch: %v", err)
	}
	watchID := stringFromAny(row["id"])
	createdAt := time.Now().UTC().Add(-2 * time.Minute).Format(time.RFC3339Nano)
	for index, severity := range []string{"info", "critical", "warn"} {
		id := "digest_member_" + severity
		insertWatchEventFixture(t, svc, ctx, id, watchID, createdAt)
		if _, err := db.Exec(
			`UPDATE "_graphjin_watch_events" SET delivery_status = 'digest_queued', seen = 1, enrichment_json = ? WHERE id = ?`,
			mustMarshalString(map[string]any{"kind": "flow", "verdict": "digest", "severity": severity, "summary": "member summary"}),
			id,
		); err != nil {
			t.Fatalf("prepare digest member %d: %v", index, err)
		}
	}
	if flushed, err := svc.sweepWatchDigests(ctx, time.Now().UTC()); err != nil || flushed != 1 {
		t.Fatalf("sweep digest flushed=%d err=%v", flushed, err)
	}
	var digestCount, memberCount int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM "_graphjin_watch_events" WHERE watch_id = ? AND delivery_status = 'pending'`, watchID,
	).Scan(&digestCount); err != nil {
		t.Fatalf("count digest events: %v", err)
	}
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM "_graphjin_watch_events" WHERE watch_id = ? AND delivery_status = 'digested'`, watchID,
	).Scan(&memberCount); err != nil {
		t.Fatalf("count digested members: %v", err)
	}
	if digestCount != 1 || memberCount != 3 {
		t.Fatalf("digest count=%d member count=%d, want 1/3", digestCount, memberCount)
	}
	var dataJSON, enrichmentJSON string
	if err := db.QueryRow(
		`SELECT data_json, enrichment_json FROM "_graphjin_watch_events" WHERE watch_id = ? AND delivery_status = 'pending'`,
		watchID,
	).Scan(&dataJSON, &enrichmentJSON); err != nil {
		t.Fatalf("read digest event: %v", err)
	}
	data := mapFromAny(parseJSONValue(dataJSON))
	enrichment := mapFromAny(parseJSONValue(enrichmentJSON))
	if stringFromAny(data["kind"]) != "digest" || intFromAny(data["count"]) != 3 ||
		stringFromAny(data["max_severity"]) != "critical" ||
		stringFromAny(enrichment["verdict"]) != "notify" {
		t.Fatalf("digest data=%+v enrichment=%+v", data, enrichment)
	}
	if flushed, err := svc.sweepWatchDigests(ctx, time.Now().UTC().Add(time.Minute)); err != nil || flushed != 0 {
		t.Fatalf("idempotent digest sweep flushed=%d err=%v", flushed, err)
	}
}

func TestWatchEventSnoozeSetClearAndDoesNotMarkSeen(t *testing.T) {
	db, svc := newSQLiteWatchService(t, 20)
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("initArtifactsBeforeCore: %v", err)
	}
	startSQLiteWatchCore(t, svc, db)
	cp := newWatchControlPlane(svc)
	ctx := contextWithUserRole(artifactUserCtx("user_1"), "analyst")
	watch, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table: watchesRootTable, Operation: "insert",
		Input: map[string]any{"name": "snooze_watch", "query": cursorOrdersWatchQuery("snooze_watch")},
	})
	if err != nil {
		t.Fatalf("insert watch: %v", err)
	}
	eventID := "snooze_event"
	insertWatchEventFixture(t, svc, ctx, eventID, stringFromAny(watch["id"]), time.Now().UTC().Format(time.RFC3339Nano))
	requested := time.Now().UTC().Add(2 * time.Hour)
	updated, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table: watchEventsRootTable, Operation: "update",
		Where: map[string]any{"id": map[string]any{"eq": eventID}},
		Input: map[string]any{"snoozed_until": requested.Format(time.RFC3339Nano)},
	})
	if err != nil {
		t.Fatalf("snooze event: %v", err)
	}
	if boolValue(updated["seen"]) {
		t.Fatalf("snooze-only update marked event seen: %+v", updated)
	}
	var seen bool
	var snoozedUntil string
	if err := db.QueryRow(
		`SELECT seen, snoozed_until FROM "_graphjin_watch_events" WHERE id = ?`, eventID,
	).Scan(&seen, &snoozedUntil); err != nil {
		t.Fatalf("read snoozed event: %v", err)
	}
	if seen || snoozedUntil == "" {
		t.Fatalf("stored snooze seen=%v snoozed_until=%q", seen, snoozedUntil)
	}
	payload, err := svc.unseenWatchEventsPayload(ctx)
	if err != nil || payload.Count != 0 {
		t.Fatalf("snoozed unseen payload=%+v err=%v", payload, err)
	}
	if _, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table: watchEventsRootTable, Operation: "update",
		Where: map[string]any{"id": map[string]any{"eq": eventID}},
		Input: map[string]any{"snoozed_until": nil},
	}); err != nil {
		t.Fatalf("clear snooze: %v", err)
	}
	payload, err = svc.unseenWatchEventsPayload(ctx)
	if err != nil || payload.Count != 1 {
		t.Fatalf("cleared snooze unseen payload=%+v err=%v", payload, err)
	}
	if _, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table: watchEventsRootTable, Operation: "update",
		Where: map[string]any{"id": map[string]any{"eq": eventID}},
		Input: map[string]any{"snoozed_until": time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)},
	}); err == nil || !strings.Contains(err.Error(), "future") {
		t.Fatalf("past snooze error = %v", err)
	}
}

func TestWatchFirstWaveCapabilityRows(t *testing.T) {
	_, svc := newSQLiteWatchService(t, 20)
	rows := newControlPlaneGraphQL(svc).systemCapabilityRows()
	var watchRow, eventRow map[string]any
	for _, row := range rows {
		switch stringFromAny(row["name"]) {
		case "gj_watch.insert_update_delete":
			watchRow = row
		case "gj_watch_event.update":
			eventRow = row
		}
	}
	watchText := mustMarshalString(watchRow)
	for _, want := range []string{"absence_json", "digest", "rollup", "watch_ids"} {
		if !strings.Contains(watchText, want) {
			t.Fatalf("watch capability row missing %q: %s", want, watchText)
		}
	}
	eventText := mustMarshalString(eventRow)
	if !strings.Contains(eventText, "snoozed_until") {
		t.Fatalf("watch event capability row missing snoozed_until: %s", eventText)
	}
}

// Models copy a card's first example verbatim, and episode evidence truncates
// deep examples — so the LEAD example is the one teaching position that
// reliably reaches the model. These pins hold it to the filtered shape: a
// benchmark run of unfiltered lead examples produced watch-everything watches
// across the whole reactive-create family.
func TestWatchTeachingLeadsWithFilteredShape(t *testing.T) {
	_, svc := newSQLiteWatchService(t, 20)
	rows := newControlPlaneGraphQL(svc).systemCapabilityRows()
	var watchRow map[string]any
	for _, row := range rows {
		if stringFromAny(row["name"]) == "gj_watch.insert_update_delete" {
			watchRow = row
		}
	}
	if watchRow == nil {
		t.Fatal("gj_watch capability row missing")
	}
	var examples []map[string]string
	if err := json.Unmarshal([]byte(stringFromAny(watchRow["examples_json"])), &examples); err != nil || len(examples) == 0 {
		t.Fatalf("examples_json undecodable: %v", err)
	}
	lead := examples[0]["query"]
	for _, want := range []string{"where:", `\"failed\"`, "after: $cursor", "orders_cursor"} {
		if !strings.Contains(lead, want) {
			t.Fatalf("lead capability example must teach the filtered escaped-inline watch, missing %q: %s", want, lead)
		}
	}

	spec, ok := graphQLHelpSpecFor("watches")
	if !ok || len(spec.Examples) == 0 {
		t.Fatal("watches help spec missing")
	}
	for _, want := range []string{"where:", `\"failed\"`, "orders_cursor"} {
		if !strings.Contains(spec.Examples[0], want) {
			t.Fatalf("graphql_help watches lead example must be filtered, missing %q: %s", want, spec.Examples[0])
		}
	}
}
