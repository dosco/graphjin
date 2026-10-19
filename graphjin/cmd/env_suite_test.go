package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	gjeval "github.com/dosco/graphjin/agent/v3/eval"
)

// The binary already carries the frozen public suite. Reaching it is what lets
// an image boot ready with nothing mounted.
func TestResolveEnvSuiteReadsTheEmbeddedSuite(t *testing.T) {
	suite, source, err := resolveEnvSuite("public")
	if err != nil {
		t.Fatal(err)
	}
	if source != "public" || len(suite.Tasks) == 0 {
		t.Fatalf("source %q with %d tasks", source, len(suite.Tasks))
	}
	if !gjeval.IsSupportedGeneratorVersion(suite.Generator.Version) {
		t.Fatalf("the embedded suite is %s, which this binary cannot run", suite.Generator.Version)
	}
	// Case is not a selector. A file genuinely named "public" stays reachable
	// as ./public, which is the escape hatch for the reserved word.
	if _, source, err := resolveEnvSuite("  PUBLIC "); err != nil || source != "public" {
		t.Fatalf("the reserved word must normalize: %q %v", source, err)
	}
	if _, _, err := resolveEnvSuite("./public"); err == nil {
		t.Fatal("./public must be read as a path, not the reserved word")
	}
}

func TestResolveEnvSuiteRefusesWhatItCannotGrade(t *testing.T) {
	if _, _, err := resolveEnvSuite("no/such/suite.yml"); err == nil {
		t.Fatal("a missing suite must be refused")
	}
	// A suite from a generator this binary does not know would be scored under
	// rules never written for it.
	forged := gjeval.Suite{
		SchemaVersion: gjeval.SuiteSchemaVersion, Name: "forged",
		Generator: gjeval.GeneratorMeta{Version: "graphjin.eval.generator/v99", Seed: 1, Scale: 1},
		Tasks:     []gjeval.Task{{Slug: "x"}},
	}
	if err := assertServableSuite(forged); err == nil {
		t.Fatal("an unknown generator version must be refused")
	} else if !strings.Contains(err.Error(), "v99") {
		t.Fatalf("the refusal must name the version: %v", err)
	}
	if err := assertServableSuite(gjeval.Suite{
		Generator: gjeval.GeneratorMeta{Version: gjeval.GeneratorVersion},
	}); err == nil {
		t.Fatal("an empty suite must be refused")
	}
}

// A holdout that needs no file is what lets one image tag serve a train
// container and an eval container that agree on the division.
func TestResolveEnvSplitDerivesAHoldoutWithoutAFile(t *testing.T) {
	suite := envTestSuite(t)

	none, label, err := resolveEnvSplit("", suite)
	if err != nil || none != nil || label != "none" {
		t.Fatalf("no split means no holdout, said plainly: %v %q", err, label)
	}

	auto, label, err := resolveEnvSplit("auto", suite)
	if err != nil {
		t.Fatal(err)
	}
	if label != "auto:0.80" {
		t.Fatalf("label = %q", label)
	}
	// Disjoint, complete, and identical on every call — two containers derive
	// the same division with no coordination.
	again, _, err := resolveEnvSplit("auto", suite)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(auto.Train, ",") != strings.Join(again.Train, ",") ||
		strings.Join(auto.Eval, ",") != strings.Join(again.Eval, ",") {
		t.Fatal("two derivations of the same split disagree")
	}
	seen := map[string]int{}
	for _, id := range append(append([]string{}, auto.Train...), auto.Eval...) {
		seen[id]++
	}
	if len(seen) != len(suite.Tasks) {
		t.Fatalf("the split covers %d of %d tasks", len(seen), len(suite.Tasks))
	}
	for id, count := range seen {
		if count != 1 {
			t.Fatalf("task %s is on both sides", id)
		}
	}
	if _, label, err := resolveEnvSplit("auto:0.5", suite); err != nil || label != "auto:0.50" {
		t.Fatalf("a ratio must be honoured: %q %v", label, err)
	}
	for _, bad := range []string{"auto:1.5", "auto:0", "auto:x", "auto:-1"} {
		if _, _, err := resolveEnvSplit(bad, suite); err == nil {
			t.Fatalf("%s must be refused", bad)
		}
	}
}

// A split cut from another suite names task ids that do not exist here, so it
// would hold out nothing at all — the guard `eval sample` and `eval export`
// already apply and this path was missing.
func TestResolveEnvSplitRefusesASplitFromAnotherSuite(t *testing.T) {
	suite := envTestSuite(t)
	path := filepath.Join(t.TempDir(), "split.json")
	if err := gjeval.SaveSplit(path, gjeval.SuiteSplit{
		SchemaVersion: gjeval.SplitSchemaVersion, SuiteFingerprint: "a-different-suite",
		Train: []string{suite.Tasks[0].ID},
	}); err != nil {
		t.Fatal(err)
	}
	_, _, err := resolveEnvSplit(path, suite)
	if err == nil {
		t.Fatal("a split from another suite must be refused")
	}
	if !strings.Contains(err.Error(), "different suite") {
		t.Fatalf("the refusal must say why: %v", err)
	}
	// A matching one loads and is labelled by file.
	good := filepath.Join(t.TempDir(), "ok.json")
	if err := gjeval.SaveSplit(good, gjeval.SuiteSplit{
		SchemaVersion: gjeval.SplitSchemaVersion, SuiteFingerprint: gjeval.SuiteFingerprint(suite),
		Train: []string{suite.Tasks[0].ID}, Eval: []string{suite.Tasks[1].ID},
	}); err != nil {
		t.Fatal(err)
	}
	split, label, err := resolveEnvSplit(good, suite)
	if err != nil || split == nil || label != "file:ok.json" {
		t.Fatalf("a matching split must load: %v %q", err, label)
	}
	_ = os.Remove(good)
}

// A suite whose oracles were verified elsewhere still runs and still scores —
// against the wrong answers. That is the failure worth refusing.
func TestSuiteMustDescribeTheWorldItIsServedOn(t *testing.T) {
	suite := gjeval.Suite{CatalogFingerprint: "catalog-a"}
	world := gjeval.DatasetFingerprint{CatalogHash: "catalog-b"}

	err := assertSuiteMatchesWorld(suite, world, false)
	if err == nil {
		t.Fatal("a suite verified against another catalog must be refused")
	}
	for _, want := range []string{"catalog-a", "catalog-b"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal must name both fingerprints: %v", err)
		}
	}
	if err := assertSuiteMatchesWorld(suite, world, true); err != nil {
		t.Fatalf("--allow-catalog-drift must permit it: %v", err)
	}
	if err := assertSuiteMatchesWorld(suite, gjeval.DatasetFingerprint{CatalogHash: "catalog-a"}, false); err != nil {
		t.Fatalf("a matching catalog must be accepted: %v", err)
	}
	// Nothing to compare is not a mismatch — and must not be reported as one.
	if err := assertSuiteMatchesWorld(gjeval.Suite{}, world, false); err != nil {
		t.Fatalf("a suite with no recorded catalog must not be refused: %v", err)
	}
	if got := catalogAgreement(gjeval.Suite{}, world); got != nil {
		t.Fatalf("nothing to compare must read as unknown, not %v", *got)
	}
	if got := catalogAgreement(suite, world); got == nil || *got {
		t.Fatal("a real mismatch must be reported as one")
	}
	if got := catalogAgreement(suite, gjeval.DatasetFingerprint{CatalogHash: "catalog-a"}); got == nil || !*got {
		t.Fatal("a match must be reported as one")
	}
}
