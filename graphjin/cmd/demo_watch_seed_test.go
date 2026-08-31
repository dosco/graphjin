package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3"
)

const saasOpsDir = "../examples/saas-ops"

// The seeded watches must be definitions GraphJin will actually accept:
// upsertWatch rejects anything that is not a cursor-paginated subscription, and
// a rejected seed shows up as a silently empty demo inbox.
func TestSaaSOpsWatchSeedFileIsCoherent(t *testing.T) {
	seed, ok, err := loadDemoWatchSeeds(saasOpsDir)
	if err != nil {
		t.Fatalf("load watch seeds: %v", err)
	}
	if !ok {
		t.Fatalf("%s should declare standing watches", saasOpsDir)
	}
	if strings.TrimSpace(seed.UserID) == "" {
		t.Fatal("watch seeds must name the operator that owns them")
	}
	if len(seed.Watches) == 0 {
		t.Fatal("watch seed file declared no watches")
	}

	for _, watch := range seed.Watches {
		if strings.TrimSpace(watch.Name) == "" {
			t.Fatal("every seeded watch needs a name")
		}
		header, err := core.Operation(watch.Query)
		if err != nil {
			t.Fatalf("watch %q: parse query: %v", watch.Name, err)
		}
		if header.Type != core.OpSubscription {
			t.Fatalf("watch %q must be a subscription", watch.Name)
		}
		if !strings.Contains(watch.Query, "after: $cursor") {
			t.Fatalf("watch %q must paginate with after: $cursor", watch.Name)
		}
		if !strings.Contains(watch.Query, "_cursor") {
			t.Fatalf("watch %q must select its <table>_cursor field", watch.Name)
		}
	}
}

// The absence watch only demonstrates absence while its filter matches nothing.
// Pin that to the seed data rather than to a comment: if someone later seeds a
// critical ticket, this fails instead of the demo quietly losing the story.
func TestSaaSOpsAbsenceWatchMatchesNoSeededRows(t *testing.T) {
	seed, _, err := loadDemoWatchSeeds(saasOpsDir)
	if err != nil {
		t.Fatalf("load watch seeds: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(saasOpsDir, "seed", "app.js"))
	if err != nil {
		t.Fatalf("read seed data: %v", err)
	}

	var absence int
	for _, watch := range seed.Watches {
		if watch.AbsenceJSON == "" {
			continue
		}
		absence++
		if !strings.Contains(watch.Query, `severity: {eq: "critical"}`) {
			t.Fatalf("watch %q: absence filter changed; re-derive the zero-match invariant", watch.Name)
		}
		if strings.Contains(string(data), `severity: "critical"`) {
			t.Fatalf("watch %q can no longer demonstrate absence: the seed data now contains a critical ticket", watch.Name)
		}
	}
	if absence != 1 {
		t.Fatalf("expected exactly one absence watch, found %d", absence)
	}
}

// The operator identity and the watches have different lifetimes: the identity
// is offered on every demo boot so the console keeps landing in an owner scope
// with content, while watches are only created on a fresh provision so a reused
// demo keeps the operator's edits — including watches they deleted.
func TestDemoOperatorSeedCarriesWatchesOnFirstRunOnly(t *testing.T) {
	for name, tc := range map[string]struct {
		demo      *DemoRuntime
		wantSeed  bool
		wantWatch bool
	}{
		"no demo":  {demo: nil, wantSeed: false},
		"reused":   {demo: &DemoRuntime{FirstRun: false}, wantSeed: true, wantWatch: false},
		"firstRun": {demo: &DemoRuntime{FirstRun: true}, wantSeed: true, wantWatch: true},
	} {
		t.Run(name, func(t *testing.T) {
			seed, ok, err := demoOperatorSeed(tc.demo, saasOpsDir)
			if err != nil {
				t.Fatalf("demoOperatorSeed: %v", err)
			}
			if ok != tc.wantSeed {
				t.Fatalf("seed present = %v, want %v", ok, tc.wantSeed)
			}
			if !ok {
				return
			}
			if seed.UserID == "" {
				t.Fatal("every demo boot must know which operator it runs as")
			}
			if got := len(seed.Watches) > 0; got != tc.wantWatch {
				t.Fatalf("carries watches = %v, want %v", got, tc.wantWatch)
			}
		})
	}
}

// A project without a watch seed file simply seeds nothing.
func TestDemoWatchSeedsAbsentFileIsNotAnError(t *testing.T) {
	_, ok, err := loadDemoWatchSeeds(t.TempDir())
	if err != nil {
		t.Fatalf("missing watch seed file should not error: %v", err)
	}
	if ok {
		t.Fatal("a project without seed/watches.yml should declare no watches")
	}
}
