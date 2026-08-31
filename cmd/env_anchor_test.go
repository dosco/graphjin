package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
	gjagent "github.com/dosco/graphjin/agent/v3"
	gjeval "github.com/dosco/graphjin/agent/v3/eval"
)

// A pinned world must be dated where it was pinned, on any calendar day.
//
// `--freeze-time` froze the agent's clock and the oracle's, but not the data:
// the pooled worker always provisions fresh (its skeleton copy excludes demo/
// on purpose) and the first-run branch ignored the pin, so the seed ran against
// the wall clock. Run the same image a week after it was built and every
// relative-window task asks about a window seven days off its rows, while
// /health quietly reports a different anchor each day.
//
// This fails on master, where the anchor is whatever today happens to be.
func TestPinnedAnchorSurvivesAFreshProvision(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded service integration")
	}
	const anchor = "2026-08-01"
	pool, stop := bootAnchoredPool(t, gjeval.EnvSpec{
		FreezeTime: anchor + "T12:00:00Z",
	}, 2)
	defer stop()

	for index, instance := range pool.instances {
		if got := instance.Fingerprint().DataAnchor; got != anchor {
			t.Fatalf("worker %d seeded for %s, want the pinned %s", index, got, anchor)
		}
	}
	// And the manifest on disk agrees — /health reads its anchor from there,
	// so a manifest stamped with today would report a different world daily.
	for index, dir := range pool.dirs {
		var manifest struct {
			DataAnchor string `json:"data_anchor"`
		}
		body, err := os.ReadFile(filepath.Join(dir, "demo", "manifest.json"))
		if err != nil {
			t.Fatalf("worker %d: %v", index, err)
		}
		if err := json.Unmarshal(body, &manifest); err != nil {
			t.Fatal(err)
		}
		if manifest.DataAnchor != anchor {
			t.Fatalf("worker %d manifest anchor = %s, want %s", index, manifest.DataAnchor, anchor)
		}
	}
}

// The second half of the same bug: a resettable boot calls StartDemo twice, and
// the second call takes the reuse branch, which used to drop the pin whenever
// the delta was zero and restamp the manifest with today. Every writable suite
// hits this — including the frozen public one.
func TestPinnedAnchorSurvivesAResettableBoot(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded service integration")
	}
	const anchor = "2026-08-01"
	pool, stop := bootAnchoredPool(t, gjeval.EnvSpec{
		FreezeTime: anchor + "T12:00:00Z",
		Writable:   true, Resettable: true,
	}, 1)
	defer stop()

	if got := pool.instances[0].Fingerprint().DataAnchor; got != anchor {
		t.Fatalf("a resettable world seeded for %s, want the pinned %s", got, anchor)
	}
}

// An explicit --data-anchor pins the data without freezing the clock.
func TestDataAnchorFlagPinsTheWorld(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded service integration")
	}
	const anchor = "2026-07-15"
	pool, stop := bootAnchoredPool(t, gjeval.EnvSpec{PinDataAnchor: anchor}, 1)
	defer stop()
	if got := pool.instances[0].Fingerprint().DataAnchor; got != anchor {
		t.Fatalf("seeded for %s, want the pinned %s", got, anchor)
	}
}

// Naming one day for the questions and another for the rows is the exact drift
// both settings exist to remove.
func TestEnvAnchorRefusesAContradiction(t *testing.T) {
	if err := validateEnvAnchor("2026-08-01T12:00:00Z", "2026-07-15"); err == nil {
		t.Fatal("a freeze time and a data anchor on different days must be refused")
	}
	if err := validateEnvAnchor("2026-08-01T12:00:00Z", "2026-08-01"); err != nil {
		t.Fatalf("the same day stated twice is not a contradiction: %v", err)
	}
	if err := validateEnvAnchor("", "2026-08-01"); err != nil {
		t.Fatalf("an anchor alone is fine: %v", err)
	}
	if err := validateEnvAnchor("2026-08-01T12:00:00Z", ""); err != nil {
		t.Fatalf("a freeze time alone is fine: %v", err)
	}
	if err := validateEnvAnchor("", "yesterday"); err == nil {
		t.Fatal("an unparseable anchor must be refused rather than silently ignored")
	}
}

// bootAnchoredPool returns the pool itself, because each worker's state has to
// be read from the directory that worker was given. Finding them by globbing
// TMPDIR picked up whichever pool another test happened to be running.
func bootAnchoredPool(t *testing.T, spec gjeval.EnvSpec, size int) (*evalInstancePool, func()) {
	t.Helper()
	project := t.TempDir()
	if err := extractDefaultDemo(project); err != nil {
		t.Fatal(err)
	}
	originalPath, originalConf, originalDB, originalOpened := cpath, conf, db, dbOpened
	t.Setenv("GO_ENV", "dev")

	client := &evalScriptClient{code: `await final({status:"blocked",answer:"not configured"});`}
	environment := evalEnvironment{
		ClientFactory: func(gjagent.Config) (ax.AIClient, error) { return client, nil },
	}
	spec.Target = gjeval.TargetDemo
	spec.ConfigPath = project
	spec.Seed = 23
	pool, err := newEvalInstancePool(context.Background(),
		func(int) evalEnvironment { return environment }, spec, size)
	if err != nil {
		cpath, conf, db, dbOpened = originalPath, originalConf, originalDB, originalOpened
		t.Fatal(err)
	}
	return pool, func() {
		_ = pool.Close()
		cpath, conf, db, dbOpened = originalPath, originalConf, originalDB, originalOpened
	}
}
