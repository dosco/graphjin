package serv

import (
	"context"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3"
)

type seedReport struct {
	name    string
	status  string
	message string
}

func newSeededWatchService(t *testing.T, seed *OperatorSeed) (*graphjinService, *[]seedReport) {
	t.Helper()
	db, svc := newSQLiteWatchService(t, 10)
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("initArtifactsBeforeCore: %v", err)
	}
	startSQLiteWatchCore(t, svc, db)

	reports := &[]seedReport{}
	if seed != nil {
		seed.Report = func(name, status, message string) {
			*reports = append(*reports, seedReport{name: name, status: status, message: message})
		}
		svc.operatorSeed = seed
	}
	return svc, reports
}

func seedStatuses(reports []seedReport) map[string]string {
	out := map[string]string{}
	for _, r := range reports {
		out[r.name] = r.status
	}
	return out
}

func TestSeedOperatorWatchesCreatesRunnableWatches(t *testing.T) {
	seed := &OperatorSeed{
		UserID: "demo-operator",
		Watches: []WatchSeed{
			{Name: "seeded_one", Description: "first", Query: cursorOrdersWatchQuery("seeded_one")},
			{Name: "seeded_two", Description: "second", Query: cursorOrdersWatchQuery("seeded_two")},
		},
	}
	svc, reports := newSeededWatchService(t, seed)
	svc.seedOperatorWatches(context.Background())

	if len(*reports) != 2 {
		t.Fatalf("expected one report per watch, got %d: %+v", len(*reports), *reports)
	}
	for name, status := range seedStatuses(*reports) {
		if status != "created" {
			t.Fatalf("watch %q: expected created, got %q", name, status)
		}
	}

	ctx := svc.ownerContext(context.Background(), "demo-operator", "user", "")
	for _, name := range []string{"seeded_one", "seeded_two"} {
		row, err := svc.internalWatchStoreRow(ctx, watchID("demo-operator", name))
		if err != nil {
			t.Fatalf("read %q: %v", name, err)
		}
		if row == nil {
			t.Fatalf("watch %q was not stored", name)
		}
		// The runner only picks up watches matching all three, so this is the
		// assertion that a seeded watch actually runs rather than merely exists.
		if got := watchStatus(stringMapValue(row, "status")); got != "active" {
			t.Fatalf("watch %q status = %q, want active", name, got)
		}
		if got := watchApproval(stringMapValue(row, "approval")); got != "approved" {
			t.Fatalf("watch %q approval = %q, want approved", name, got)
		}
		if !boolMapValue(row, "enabled") {
			t.Fatalf("watch %q is not enabled", name)
		}
		if got := stringMapValue(row, "owner_id"); got != "demo-operator" {
			t.Fatalf("watch %q owner_id = %q, want demo-operator", name, got)
		}
		if got := stringMapValue(row, "owner_role"); got != "user" {
			t.Fatalf("watch %q owner_role = %q, want user", name, got)
		}
	}
}

func TestSeedOperatorWatchesIsIdempotent(t *testing.T) {
	seed := &OperatorSeed{
		UserID:  "demo-operator",
		Watches: []WatchSeed{{Name: "seeded_one", Query: cursorOrdersWatchQuery("seeded_one")}},
	}
	svc, reports := newSeededWatchService(t, seed)

	svc.seedOperatorWatches(context.Background())
	*reports = nil
	svc.seedOperatorWatches(context.Background())

	if len(*reports) != 1 || (*reports)[0].status != "present" {
		t.Fatalf("re-seeding should report present, got %+v", *reports)
	}
	ctx := svc.ownerContext(context.Background(), "demo-operator", "user", "")
	cp := newWatchControlPlane(svc)
	rows, err := cp.watchRows(ctx)
	if err != nil {
		t.Fatalf("watchRows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected exactly one watch after re-seeding, got %d", len(rows))
	}
}

// Seeding preserves an operator's edits to a watch it created, because it never
// touches a row that already exists. An upsert would instead clear the cursor
// checkpoint and re-fire the watch on every boot.
//
// Note what this does NOT promise: the seeder cannot tell a deleted watch from
// an unseeded one, so calling it again does recreate a deleted watch. Not
// resurrecting one is the caller's job — the demo only passes the seed option on
// a first run (see demoWatchSeedOption).
func TestSeedOperatorWatchesLeavesExistingRowsAlone(t *testing.T) {
	seed := &OperatorSeed{
		UserID:  "demo-operator",
		Watches: []WatchSeed{{Name: "seeded_one", Description: "original", Query: cursorOrdersWatchQuery("seeded_one")}},
	}
	svc, _ := newSeededWatchService(t, seed)
	svc.seedOperatorWatches(context.Background())

	ctx := svc.ownerContext(context.Background(), "demo-operator", "user", "")
	cp := newWatchControlPlane(svc)
	id := watchID("demo-operator", "seeded_one")
	if _, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table:     watchesRootTable,
		Operation: "update",
		Input: map[string]any{
			"id":          id,
			"name":        "seeded_one",
			"query":       cursorOrdersWatchQuery("seeded_one"),
			"enabled":     false,
			"description": "operator edit",
		},
	}); err != nil {
		t.Fatalf("update watch: %v", err)
	}

	svc.seedOperatorWatches(context.Background())

	row, err := svc.internalWatchStoreRow(ctx, id)
	if err != nil || row == nil {
		t.Fatalf("read after re-seed: row=%v err=%v", row, err)
	}
	if boolMapValue(row, "enabled") {
		t.Fatal("re-seeding re-enabled a watch the operator had paused")
	}
	if got := stringMapValue(row, "description"); got != "operator edit" {
		t.Fatalf("description = %q, want the operator's edit to survive", got)
	}
}

func TestSeedOperatorWatchesSkipsBadDefinitionsOnly(t *testing.T) {
	seed := &OperatorSeed{
		UserID: "demo-operator",
		Watches: []WatchSeed{
			// Not a subscription, and no cursor pagination: rejected by
			// validateWatchDefinition.
			{Name: "bad_watch", Query: `query bad_watch { orders { id } }`},
			{Name: "good_watch", Query: cursorOrdersWatchQuery("good_watch")},
		},
	}
	svc, reports := newSeededWatchService(t, seed)
	svc.seedOperatorWatches(context.Background())

	statuses := seedStatuses(*reports)
	if statuses["bad_watch"] != "failed" {
		t.Fatalf("bad_watch status = %q, want failed", statuses["bad_watch"])
	}
	if statuses["good_watch"] != "created" {
		t.Fatalf("good_watch status = %q, want created", statuses["good_watch"])
	}
	ctx := svc.ownerContext(context.Background(), "demo-operator", "user", "")
	row, err := svc.internalWatchStoreRow(ctx, watchID("demo-operator", "good_watch"))
	if err != nil || row == nil {
		t.Fatalf("good watch should still be seeded: row=%v err=%v", row, err)
	}
}

func TestSeedOperatorWatchesSkipsWhenUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name    string
		disable func(*graphjinService)
		want    string
	}{
		{"no query engine", func(s *graphjinService) { s.gj = nil }, "query engine unavailable"},
		{"watches disabled", func(s *graphjinService) { s.conf.Core.Watches.Enabled = false }, "watches are disabled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			seed := &OperatorSeed{
				UserID:  "demo-operator",
				Watches: []WatchSeed{{Name: "seeded_one", Query: cursorOrdersWatchQuery("seeded_one")}},
			}
			svc, reports := newSeededWatchService(t, seed)
			tc.disable(svc)

			svc.seedOperatorWatches(context.Background())

			if len(*reports) != 1 {
				t.Fatalf("expected a single skip report, got %+v", *reports)
			}
			if (*reports)[0].status != "skipped" || !strings.Contains((*reports)[0].message, tc.want) {
				t.Fatalf("report = %+v, want skipped %q", (*reports)[0], tc.want)
			}
		})
	}
}

func TestOptionSetOperatorSeedRequiresUserID(t *testing.T) {
	svc := &graphjinService{}
	if err := OptionSetOperatorSeed(OperatorSeed{})(svc); err == nil {
		t.Fatal("expected an error when the seed has no user id")
	}
	if err := OptionSetOperatorSeed(OperatorSeed{UserID: "demo-operator"})(svc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc.operatorSeed == nil || svc.operatorSeed.operatorSeedRole() != "user" {
		t.Fatalf("seed role should default to user, got %+v", svc.operatorSeed)
	}
}

// Running a watch and driving the console need different access, so the two
// roles are allowed to diverge; the console role falls back to the watch role.
func TestOperatorSeedConsoleRoleFallsBackToWatchRole(t *testing.T) {
	for name, tc := range map[string]struct {
		seed        OperatorSeed
		wantWatch   string
		wantConsole string
	}{
		"defaults":      {OperatorSeed{UserID: "op"}, "user", "user"},
		"watch only":    {OperatorSeed{UserID: "op", Role: "analyst"}, "analyst", "analyst"},
		"both declared": {OperatorSeed{UserID: "op", Role: "user", ConsoleRole: "admin"}, "user", "admin"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := tc.seed.operatorSeedRole(); got != tc.wantWatch {
				t.Fatalf("watch role = %q, want %q", got, tc.wantWatch)
			}
			if got := tc.seed.consoleSeedRole(); got != tc.wantConsole {
				t.Fatalf("console role = %q, want %q", got, tc.wantConsole)
			}
		})
	}
}
