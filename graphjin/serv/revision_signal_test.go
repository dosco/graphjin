package serv

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/core/v3/featurecap"
)

func TestRevisionSignalIsPrivate(t *testing.T) {
	svc := newArtifactOverlayTestService(t, nil)
	query := `query { gj_internal_revision { domain revision updated_at } }`

	res, err := svc.gj.GraphQL(svc.internalStoreContext(context.Background()), query, nil, nil)
	if err != nil || len(res.Errors) != 0 {
		t.Fatalf("internal revision query failed: err=%v errors=%+v", err, res.Errors)
	}

	contexts := map[string]context.Context{
		"anon":  context.Background(),
		"user":  artifactUserCtx("user_1"),
		"admin": context.WithValue(context.Background(), core.UserRoleKey, "admin"),
	}
	for name, ctx := range contexts {
		res, err = svc.gj.GraphQL(ctx, query, nil, nil)
		if err == nil && len(res.Errors) == 0 {
			t.Fatalf("%s queried private revision signal: %s", name, res.Data)
		}
	}

	forged := context.WithValue(context.Background(), core.UserRoleKey, graphjinInternalStoreRole)
	forged = context.WithValue(forged, core.IdentityRolesKey, []string{graphjinInternalStoreRole})
	res, err = svc.gj.GraphQL(forged, query, nil, nil)
	if err == nil && len(res.Errors) == 0 {
		t.Fatalf("forged internal role queried private revision signal: %s", res.Data)
	}

	if featurecap.ValidSystemRoot(revisionSignalRootTable) {
		t.Fatalf("%s must not be a public system root", revisionSignalRootTable)
	}
	if systemRootRegistered(svc.conf, revisionSignalRootTable) {
		t.Fatalf("%s must not be registered in the public NanoDB projection", revisionSignalRootTable)
	}
	catalog, err := svc.gj.GraphQL(artifactUserCtx("user_1"), `query {
		gj_catalog(where: { table_name: { eq: "gj_internal_revision" } }) {
			table_name
		}
	}`, nil, nil)
	if err != nil {
		t.Fatalf("query catalog privacy: %v", err)
	}
	if strings.Contains(string(catalog.Data), revisionSignalRootTable) {
		t.Fatalf("private revision signal leaked into catalog: %s", catalog.Data)
	}
}

func TestRevisionSignalSubscriptionMembersAreIsolated(t *testing.T) {
	svc := newRevisionSignalTestService(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	artifacts, err := svc.subscribeRevisionSignal(ctx, "artifacts")
	if err != nil {
		t.Fatalf("subscribe artifacts revision: %v", err)
	}
	defer artifacts.Unsubscribe()
	if roots := artifacts.SubscriptionRoots(); len(roots) != 1 || roots[0].Table != revisionSignalRootTable {
		t.Fatalf("artifacts revision roots = %+v", roots)
	}
	watches, err := svc.subscribeRevisionSignal(ctx, "watches")
	if err != nil {
		t.Fatalf("subscribe watches revision: %v", err)
	}
	defer watches.Unsubscribe()
	if _, err := svc.gj.GetSavedQuery("graphjin_revision_signal"); err == nil {
		t.Fatal("private revision subscription was persisted as a saved query")
	}

	assertRevisionResult(t, artifacts.Result, "artifacts", 0)
	assertRevisionResult(t, watches.Result, "watches", 0)

	if err := svc.bumpArtifactRevision(ctx, "artifacts"); err != nil {
		t.Fatalf("bump artifacts revision: %v", err)
	}
	assertRevisionResult(t, artifacts.Result, "artifacts", 1)
	assertNoRevisionResult(t, watches.Result, 350*time.Millisecond)

	if err := svc.bumpArtifactRevision(ctx, "watches"); err != nil {
		t.Fatalf("bump watches revision: %v", err)
	}
	assertRevisionResult(t, watches.Result, "watches", 1)
	assertNoRevisionResult(t, artifacts.Result, 350*time.Millisecond)
}

func TestPublishRevisionSignalInitialAndCoalescing(t *testing.T) {
	out := make(chan revisionSignal, 1)
	var last int64
	initialized := publishRevisionSignal(out, revisionSignal{Domain: "artifacts", Revision: 4}, false, false, &last)
	if !initialized || last != 4 || len(out) != 0 {
		t.Fatalf("suppressed initial signal = initialized:%v last:%d queued:%d", initialized, last, len(out))
	}
	initialized = publishRevisionSignal(out, revisionSignal{Domain: "artifacts", Revision: 4}, false, initialized, &last)
	if len(out) != 0 {
		t.Fatal("unchanged revision should be suppressed")
	}
	publishRevisionSignal(out, revisionSignal{Domain: "artifacts", Revision: 5}, false, initialized, &last)
	publishRevisionSignal(out, revisionSignal{Domain: "artifacts", Revision: 6}, false, initialized, &last)
	if len(out) != 1 || last != 6 {
		t.Fatalf("revision signals should coalesce, queued:%d last:%d", len(out), last)
	}
}

func TestRevisionSignalsInitialPolicyAndChangeDelivery(t *testing.T) {
	svc := newRevisionSignalTestService(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	watches := svc.revisionSignals(ctx, "watches", true, 100*time.Millisecond)
	select {
	case signal := <-watches:
		if signal.Domain != "watches" || signal.Revision != 0 {
			t.Fatalf("initial watch signal = %+v", signal)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for initial watch signal")
	}

	artifacts := svc.revisionSignals(ctx, "artifacts", false, 100*time.Millisecond)
	select {
	case signal := <-artifacts:
		t.Fatalf("artifact initial signal should be suppressed: %+v", signal)
	case <-time.After(250 * time.Millisecond):
	}
	if err := svc.bumpArtifactRevision(ctx, "artifacts"); err != nil {
		t.Fatalf("bump artifact revision: %v", err)
	}
	select {
	case signal := <-artifacts:
		if signal.Domain != "artifacts" || signal.Revision != 1 {
			t.Fatalf("artifact change signal = %+v", signal)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for artifact change signal")
	}
}

func TestWatchDeliverySweepRecoversEventWithoutRevisionBump(t *testing.T) {
	svc := newRevisionSignalTestService(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go svc.watchDeliveryLoopWithSweepInterval(ctx, 150*time.Millisecond)

	// Let the initial revision result and empty pending scan settle before
	// inserting a row without the required revision bump.
	time.Sleep(350 * time.Millisecond)
	now := time.Now().UTC().Format(time.RFC3339)
	db := svc.dbs["app"]
	if _, err := db.Exec(`INSERT INTO "_graphjin_watches"
		(id, name, query, lifecycle, status, approval, enabled, owner_id, owner_role, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"watch:recovery", "recovery", cursorOrdersWatchQuery("recovery"),
		"durable", "active", "approved", true, "user_1", "analyst", now, now,
	); err != nil {
		t.Fatalf("insert recovery watch: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO "_graphjin_watch_events"
		(id, watch_id, data_hash, data_json, delivery_status, owner_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"event:recovery", "watch:recovery", "hash:recovery", `{"ok":true}`,
		"pending", "user_1", now, now,
	); err != nil {
		t.Fatalf("insert recovery event: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var status string
		if err := db.QueryRow(
			`SELECT delivery_status FROM "_graphjin_watch_events" WHERE id = ?`,
			"event:recovery",
		).Scan(&status); err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "locked") ||
				strings.Contains(strings.ToLower(err.Error()), "busy") {
				time.Sleep(25 * time.Millisecond)
				continue
			}
			t.Fatalf("read recovery event: %v", err)
		}
		if status == "delivered" {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("hourly recovery path did not process pending event without a revision bump")
}

func newRevisionSignalTestService(t *testing.T) *graphjinService {
	t.Helper()
	db, svc := newSQLiteWatchService(t, 20)
	svc.conf.SubsPollDuration = 100 * time.Millisecond
	svc.conf.Artifacts.PollSeconds = 1
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("initialize revision signal store: %v", err)
	}
	startSQLiteWatchCore(t, svc, db)
	t.Cleanup(func() { svc.gj.Close() })
	return svc
}

func assertRevisionResult(
	t *testing.T,
	results <-chan *core.Result,
	domain string,
	revision int64,
) {
	t.Helper()
	select {
	case result := <-results:
		signal, ok := decodeRevisionSignal(result, domain)
		if !ok {
			t.Fatalf("decode %s revision result: %+v", domain, result)
		}
		if signal.Revision != revision {
			t.Fatalf("%s revision = %d, want %d", domain, signal.Revision, revision)
		}
	case <-time.After(7 * time.Second):
		t.Fatalf("timed out waiting for %s revision %d", domain, revision)
	}
}

func assertNoRevisionResult(t *testing.T, results <-chan *core.Result, duration time.Duration) {
	t.Helper()
	select {
	case result := <-results:
		var data any
		_ = json.Unmarshal(result.Data, &data)
		t.Fatalf("unexpected revision result: %+v", data)
	case <-time.After(duration):
	}
}
