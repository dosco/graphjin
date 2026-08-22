package main

import (
	"context"
	"testing"

	gjeval "github.com/dosco/graphjin/agent/v3/eval"
)

// The frozen public suite records which catalog its 113 questions were written
// against, and nothing at run time enforces that stamp: a catalog change that
// forgets to update it would let every future benchmark run silently claim a
// catalog it did not use, surfacing only as ranking confusion at publish time.
// This test is the enforcement — it boots the demo the way a public bench run
// does and fails the build if the served catalog no longer matches the file.
//
// When it fails after a deliberate catalog change: re-run
// `graphjin eval freeze-suite` against the demo, but keep the committed tasks
// and copy only the new target_catalog_fingerprint into the suite file — a
// same-day regeneration was measured to swap a third of the questions purely
// because the demo's date-anchored data moves what the generator picks. Then
// update publicBenchmarkSuiteFingerprint and advance PublicBenchmarkGeneration
// in agent/eval/benchmark.go: a catalog change is a cohort boundary.
func TestFrozenPublicSuiteMatchesLiveCatalogFingerprint(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded demo integration")
	}
	suite, err := loadPublicEvalSuite()
	if err != nil {
		t.Fatal(err)
	}

	project := t.TempDir()
	if err := extractDefaultDemo(project); err != nil {
		t.Fatal(err)
	}
	originalPath, originalConf, originalDB, originalOpened := cpath, conf, db, dbOpened
	defer func() {
		cpath, conf, db, dbOpened = originalPath, originalConf, originalDB, originalOpened
	}()
	t.Setenv("GO_ENV", "dev")

	spec := gjeval.PublicBenchmark()
	instance, err := (evalEnvironment{}).Start(context.Background(), gjeval.EnvSpec{
		Target: gjeval.TargetDemo, ConfigPath: project, Seed: spec.Seed,
		Writable: true, Reactive: true, Resettable: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close() //nolint:errcheck

	live := instance.Fingerprint().CatalogHash
	if live == "" {
		t.Fatal("demo instance reported no catalog fingerprint")
	}
	if live != suite.CatalogFingerprint {
		t.Fatalf("the demo catalog (%s) no longer matches the frozen suite (%s); the catalog changed without a refreeze — see this test's comment for the procedure",
			live, suite.CatalogFingerprint)
	}
}
