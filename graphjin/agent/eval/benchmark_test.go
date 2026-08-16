package eval

import (
	"reflect"
	"testing"
	"time"
)

func TestSuiteIdentityFieldBoundary(t *testing.T) {
	base := Report{
		Mode: RunModeBenchmark, SuiteFingerprint: "suite", RewardVersion: "reward",
		DatasetFingerprint: DatasetFingerprint{CatalogHash: "catalog", SeedManifestHash: "seed-manifest", DataAnchor: "anchor"},
		OracleValueHash:    "oracle", GeneratedAt: time.Unix(1, 0),
		Provenance: RunProvenance{Provider: "provider", Model: "model", GraphJinCommit: "commit", BinaryFingerprint: "binary", Seed: 23, Repeats: 3, MaxSteps: 8, Temperature: 0},
		Metrics:    Metrics{Recall: 0.5},
	}
	identity := SuiteIdentity(base)

	sensitive := []struct {
		name   string
		change func(*Report)
	}{
		{"mode", func(r *Report) { r.Mode = RunModeRun }},
		{"suite_fingerprint", func(r *Report) { r.SuiteFingerprint += "x" }},
		{"catalog_hash", func(r *Report) { r.DatasetFingerprint.CatalogHash += "x" }},
		{"seed_manifest_hash", func(r *Report) { r.DatasetFingerprint.SeedManifestHash += "x" }},
		{"seed", func(r *Report) { r.Provenance.Seed++ }},
		{"repeats", func(r *Report) { r.Provenance.Repeats++ }},
		{"max_steps", func(r *Report) { r.Provenance.MaxSteps++ }},
		{"temperature", func(r *Report) { r.Provenance.Temperature = 0.1 }},
		{"reward_version", func(r *Report) { r.RewardVersion += "x" }},
	}
	for _, tc := range sensitive {
		t.Run("sensitive_"+tc.name, func(t *testing.T) {
			changed := base
			tc.change(&changed)
			if SuiteIdentity(changed) == identity {
				t.Fatalf("identity ignored %s", tc.name)
			}
			if got := SuiteIdentityMismatches(changed, base); !reflect.DeepEqual(got, []string{identityMismatchName(tc.name)}) {
				t.Fatalf("mismatches = %v", got)
			}
		})
	}

	ignored := []struct {
		name   string
		change func(*Report)
	}{
		{"model", func(r *Report) { r.Provenance.Model += "x" }},
		{"provider", func(r *Report) { r.Provenance.Provider += "x" }},
		{"commit", func(r *Report) { r.Provenance.GraphJinCommit += "x" }},
		{"binary", func(r *Report) { r.Provenance.BinaryFingerprint += "x" }},
		{"oracle", func(r *Report) { r.OracleValueHash += "x" }},
		{"anchor", func(r *Report) { r.DatasetFingerprint.DataAnchor += "x" }},
		{"generated", func(r *Report) { r.GeneratedAt = r.GeneratedAt.Add(time.Hour) }},
		{"metrics", func(r *Report) { r.Metrics.Recall = 1 }},
	}
	for _, tc := range ignored {
		t.Run("ignored_"+tc.name, func(t *testing.T) {
			changed := base
			tc.change(&changed)
			if got := SuiteIdentity(changed); got != identity {
				t.Fatalf("identity changed for %s: %s != %s", tc.name, got, identity)
			}
			if got := SuiteIdentityMismatches(changed, base); len(got) != 0 {
				t.Fatalf("mismatches for %s = %v", tc.name, got)
			}
		})
	}
}

func identityMismatchName(short string) string {
	switch short {
	case "catalog_hash":
		return "dataset_fingerprint.catalog_hash"
	case "seed_manifest_hash":
		return "dataset_fingerprint.seed_manifest_hash"
	case "seed", "repeats", "max_steps", "temperature":
		return "provenance." + short
	default:
		return short
	}
}

// TestBenchmarkRollupMapCoversEveryCategory pins the frozen v1 mapping: every
// valid category maps to exactly one rollup, so a future twelfth category
// cannot silently vanish from every rollup, and no category can count twice.
func TestBenchmarkRollupMapCoversEveryCategory(t *testing.T) {
	all := []Category{
		CategoryDiscovery, CategoryAggregate, CategoryRanking, CategoryWindow,
		CategoryTraversal, CategorySavedMetric, CategoryRefusal, CategoryAction,
		CategoryReactive, CategoryMultiTurn, CategoryCrossSource,
	}
	seen := map[Category]string{}
	for _, rollup := range benchmarkRollups {
		for _, member := range rollup.Categories {
			if prior, dup := seen[member]; dup {
				t.Fatalf("category %s mapped twice: %s and %s", member, prior, rollup.Name)
			}
			seen[member] = rollup.Name
		}
	}
	for _, category := range all {
		if _, ok := seen[category]; !ok {
			t.Fatalf("category %s is missing from the rollup mapping", category)
		}
		if !validCategory(category) {
			t.Fatalf("test list carries an invalid category %s", category)
		}
	}
	if len(seen) != len(all) {
		t.Fatalf("mapping covers %d categories, expected %d", len(seen), len(all))
	}
	// The projection the publisher embeds must mirror the slice exactly.
	projected := BenchmarkRollupMap()
	for category, name := range seen {
		if projected[string(category)] != name {
			t.Fatalf("projection disagrees for %s: %s != %s", category, projected[string(category)], name)
		}
	}
	if got := BenchmarkRollupNames(); len(got) != 3 || got[0] != "questions" || got[1] != "operations" || got[2] != "governance" {
		t.Fatalf("frozen display order changed: %v", got)
	}
}
