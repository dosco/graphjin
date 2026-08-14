package eval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

const PublicBenchmarkGeneration = "2028.2"

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
const publicBenchmarkSuiteFingerprint = "b77fb918d9dd40dc7204cc4241d6c763"

type suiteIdentityProjection struct {
	Mode             RunMode `json:"mode"`
	SuiteFingerprint string  `json:"suite_fingerprint"`
	CatalogHash      string  `json:"catalog_hash"`
	SeedManifestHash string  `json:"seed_manifest_hash"`
	Seed             int64   `json:"seed"`
	Repeats          int     `json:"repeats"`
	MaxSteps         int     `json:"max_steps"`
	Temperature      float64 `json:"temperature"`
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
		RewardVersion: r.RewardVersion,
	}
}
