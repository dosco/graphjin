package eval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

const PublicBenchmarkGeneration = "2028.4"

type PublicBenchmarkSpec struct {
	Generation       string  `json:"generation"`
	Command          string  `json:"command"`
	Target           Target  `json:"target"`
	Mode             RunMode `json:"mode"`
	Scale            int     `json:"scale"`
	Seed             int64   `json:"seed"`
	Repeats          int     `json:"repeats"`
	SuiteFingerprint string  `json:"suite_fingerprint"`
}

func PublicBenchmark() PublicBenchmarkSpec {
	return PublicBenchmarkSpec{
		Generation:       PublicBenchmarkGeneration,
		Command:          "graphjin eval bench --public --demo --yes",
		Target:           TargetDemo,
		Mode:             RunModeBenchmark,
		Scale:            100,
		Seed:             23,
		Repeats:          DefaultRepeats,
		SuiteFingerprint: publicBenchmarkSuiteFingerprint,
	}
}

// Generation 2027.2 rewrote reactive watch creation from an exact-string
// expectation on the stored subscription to a semantic post-state, and widened
// the cross-source file rule to accept either valid way of reading a file
// source. Both were unpassable-or-over-strict task defects rather than agent
// failures; see agent/eval/generate.go for the reasoning.
//
// Generation 2028.2 restores that widening: the v10 pattern regressed it by
// anchoring on "sla_policies(", so the argument-free read every model writes
// scored method false while producing correct answers (12 of 12 episodes of the
// strongest model's run, 7 with ground truth true). v11 also accepts the new
// decoded text column and splits method_pattern_unmatched out of the
// client_side_aggregation label so the failure taxonomy stays causal.
//
// Generation 2028.4 fixes two tasks that graded something they never asked for.
// The reactive execution twins said "over support_tickets" while requiring the
// subscription to filter on urgent, so both models built exactly what was asked
// and failed; a twin exists to hand over the finished operation, so it now names
// the filter. The ranking twins projected one identifier, so an answer naming
// the right row by its primary key scored wrong with the right value; the oracle
// now selects the key as an alternate the scorer already knew how to accept
// (reward/v5). The SLA policy the agent can read also states that closing a
// ticket records the time it was resolved, which the close-ticket post-state has
// always required and no readable evidence mentioned.
const publicBenchmarkSuiteFingerprint = "8f31247903e5b5cbc49501e3632968d6"

// PublicBenchmarkRollupVersion freezes the capability-rollup mapping below.
// The board's single recall conflates two different deployment questions —
// "can it answer questions about my org?" and "can it operate my org?" — and
// one measured run answered them 0.9 and 0.2 with the same headline number.
// The mapping is data, versioned like every other frozen ruler piece: a row
// publishes the rollup its generation's mapping produced, and changing the
// mapping is a visible version bump, never a silent regrouping.
const PublicBenchmarkRollupVersion = "v1"

// benchmarkRollup names one capability group and the task categories it
// covers. Ordered: display order is part of the frozen contract.
type benchmarkRollup struct {
	Name       string
	Categories []Category
	Reason     string
}

// benchmarkRollups is the frozen v1 capability mapping.
//
// Judgment calls, recorded: multi-turn reads as follow-up questions but what
// it demands is carrying state and often executing (confirming a proposed
// watch), so it counts as operations. Refusal is a governance property, not a
// capability — folding it into either capability group would let a safety
// score pad a capability score. Traversal is mapped for completeness; the
// current public suite generates none.
var benchmarkRollups = []benchmarkRollup{
	{Name: "questions", Categories: []Category{CategoryAggregate, CategoryWindow, CategoryRanking, CategoryDiscovery, CategorySavedMetric, CategoryTraversal},
		Reason: "stateless answers computed from live data"},
	{Name: "operations", Categories: []Category{CategoryAction, CategoryReactive, CategoryMultiTurn, CategoryCrossSource},
		Reason: "writes, watches, follow-ups, and multi-source work that carries state"},
	{Name: "governance", Categories: []Category{CategoryRefusal},
		Reason: "declining what policy forbids, reliably and legibly"},
}

// RollupForCategory returns the frozen rollup name for a category, or "".
func RollupForCategory(category Category) string {
	for _, rollup := range benchmarkRollups {
		for _, member := range rollup.Categories {
			if member == category {
				return rollup.Name
			}
		}
	}
	return ""
}

// BenchmarkRollupMap projects the frozen mapping as category → rollup name,
// the shape the publisher embeds so the site checker can validate rollup
// numbers against category numbers without a second copy of this table.
func BenchmarkRollupMap() map[string]string {
	out := make(map[string]string)
	for _, rollup := range benchmarkRollups {
		for _, member := range rollup.Categories {
			out[string(member)] = rollup.Name
		}
	}
	return out
}

// BenchmarkRollupNames returns the frozen display order.
func BenchmarkRollupNames() []string {
	names := make([]string, 0, len(benchmarkRollups))
	for _, rollup := range benchmarkRollups {
		names = append(names, rollup.Name)
	}
	return names
}

type suiteIdentityProjection struct {
	Mode             RunMode `json:"mode"`
	SuiteFingerprint string  `json:"suite_fingerprint"`
	CatalogHash      string  `json:"catalog_hash"`
	SeedManifestHash string  `json:"seed_manifest_hash"`
	Seed             int64   `json:"seed"`
	Repeats          int     `json:"repeats"`
	MaxSteps         int     `json:"max_steps"`
	Temperature      float64 `json:"temperature"`
	TopP             float64 `json:"top_p,omitempty"`
	RewardVersion    string  `json:"reward_version"`
}

func SuiteIdentity(r Report) string {
	data, _ := json.Marshal(reportSuiteIdentity(r))
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:16])
}

func SuiteIdentityMismatches(have, want Report) []string {
	h, w := reportSuiteIdentity(have), reportSuiteIdentity(want)
	var out []string
	if h.Mode != w.Mode {
		out = append(out, "mode")
	}
	if h.SuiteFingerprint != w.SuiteFingerprint {
		out = append(out, "suite_fingerprint")
	}
	if h.CatalogHash != w.CatalogHash {
		out = append(out, "dataset_fingerprint.catalog_hash")
	}
	if h.SeedManifestHash != w.SeedManifestHash {
		out = append(out, "dataset_fingerprint.seed_manifest_hash")
	}
	if h.Seed != w.Seed {
		out = append(out, "provenance.seed")
	}
	if h.Repeats != w.Repeats {
		out = append(out, "provenance.repeats")
	}
	if h.MaxSteps != w.MaxSteps {
		out = append(out, "provenance.max_steps")
	}
	if h.Temperature != w.Temperature {
		out = append(out, "provenance.temperature")
	}
	if h.TopP != w.TopP {
		out = append(out, "provenance.top_p")
	}
	if h.RewardVersion != w.RewardVersion {
		out = append(out, "reward_version")
	}
	return out
}

func reportSuiteIdentity(r Report) suiteIdentityProjection {
	return suiteIdentityProjection{
		Mode: r.Mode, SuiteFingerprint: r.SuiteFingerprint,
		CatalogHash:      r.DatasetFingerprint.CatalogHash,
		SeedManifestHash: r.DatasetFingerprint.SeedManifestHash,
		Seed:             r.Provenance.Seed, Repeats: r.Provenance.Repeats,
		MaxSteps: r.Provenance.MaxSteps, Temperature: r.Provenance.Temperature,
		TopP:          r.Provenance.TopP,
		RewardVersion: r.RewardVersion,
	}
}
