package main

import (
	"strings"
	"testing"

	gjeval "github.com/dosco/graphjin/agent/v3/eval"
	"github.com/spf13/cobra"
)

func sampleTestSuite(t *testing.T) gjeval.Suite {
	t.Helper()
	suite := envTestSuite(t)
	return suite
}

// A split is only meaningful against the suite it was cut from. Filtering by
// one generated against a different suite names task ids that no longer exist,
// which would silently produce an arbitrary set rather than the holdout
// somebody intended.
func TestSampleRefusesASplitFromAnotherSuite(t *testing.T) {
	suite := sampleTestSuite(t)
	split := gjeval.SuiteSplit{
		SchemaVersion: gjeval.SplitSchemaVersion, SuiteFingerprint: "a-different-suite",
		Train: []string{suite.Tasks[0].ID},
	}
	path := t.TempDir() + "/split.json"
	if err := gjeval.SaveSplit(path, split); err != nil {
		t.Fatal(err)
	}
	_, _, _, err := sampleSuiteSide(suite, path, "train")
	if err == nil {
		t.Fatal("a split cut from another suite must be refused")
	}
	if !strings.Contains(err.Error(), "different suite") {
		t.Fatalf("the refusal must say why: %v", err)
	}
}

func TestSampleSideSelection(t *testing.T) {
	suite := sampleTestSuite(t)
	split := gjeval.SuiteSplit{
		SchemaVersion: gjeval.SplitSchemaVersion, SuiteFingerprint: gjeval.SuiteFingerprint(suite),
		Train: []string{suite.Tasks[0].ID}, Eval: []string{suite.Tasks[1].ID},
	}
	path := t.TempDir() + "/split.json"
	if err := gjeval.SaveSplit(path, split); err != nil {
		t.Fatal(err)
	}

	train, side, fingerprint, err := sampleSuiteSide(suite, path, "train")
	if err != nil {
		t.Fatal(err)
	}
	if len(train.Tasks) != 1 || train.Tasks[0].ID != suite.Tasks[0].ID {
		t.Fatalf("the train side was not selected: %+v", train.Tasks)
	}
	if side != "train" || fingerprint == "" {
		t.Fatalf("the run must record which side and which split: %q %q", side, fingerprint)
	}
	// The recorded fingerprint has to identify this split specifically, or a
	// corpus cannot be checked against the holdout it claims.
	other := split
	other.TrainRatio = 0.5
	if other.Fingerprint() == split.Fingerprint() {
		t.Fatal("two different splits must not share a fingerprint")
	}

	evalSide, side, _, err := sampleSuiteSide(suite, path, "eval")
	if err != nil {
		t.Fatal(err)
	}
	if len(evalSide.Tasks) != 1 || evalSide.Tasks[0].ID != suite.Tasks[1].ID || side != "eval" {
		t.Fatalf("the eval side was not selected: %+v", evalSide.Tasks)
	}

	// A typo must not silently serve the other side.
	if _, _, _, err := sampleSuiteSide(suite, path, "trian"); err == nil {
		t.Fatal("an unrecognised side must be refused rather than guessed")
	}
	// And no split at all leaves the suite alone.
	whole, side, fingerprint, err := sampleSuiteSide(suite, "", "train")
	if err != nil || len(whole.Tasks) != len(suite.Tasks) || side != "" || fingerprint != "" {
		t.Fatalf("without a split the suite is untouched: %v %d %q", err, len(whole.Tasks), side)
	}
}

// An empty side is a mistake worth naming: it usually means the split and the
// suite disagree about which tasks exist.
func TestSampleRefusesAnEmptySide(t *testing.T) {
	suite := sampleTestSuite(t)
	split := gjeval.SuiteSplit{
		SchemaVersion: gjeval.SplitSchemaVersion, SuiteFingerprint: gjeval.SuiteFingerprint(suite),
		Train: []string{suite.Tasks[0].ID, suite.Tasks[1].ID},
	}
	path := t.TempDir() + "/split.json"
	if err := gjeval.SaveSplit(path, split); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := sampleSuiteSide(suite, path, "eval"); err == nil {
		t.Fatal("a side with no tasks must be refused")
	}
}

func TestSampleRepeatsAreBounded(t *testing.T) {
	opts := &evalCLIOptions{}
	for _, repeats := range []string{"0", "101", "-1"} {
		command := evalSampleCmd(opts)
		command.SetArgs([]string{"--repeats", repeats})
		command.SetOut(&strings.Builder{})
		command.SetErr(&strings.Builder{})
		if err := command.Execute(); err == nil {
			t.Fatalf("--repeats %s must be refused", repeats)
		}
	}
}

// A held-out episode must never end up in a training corpus by accident. The
// run records which side it drew from, so omitting the flags cannot hide it.
func TestExportRefusesHeldOutEpisodes(t *testing.T) {
	held := []gjeval.Episode{
		{TaskID: "t1", Provenance: gjeval.RunProvenance{SplitSide: "eval", SplitFingerprint: "fp"}},
		{TaskID: "t2", Provenance: gjeval.RunProvenance{SplitSide: "eval", SplitFingerprint: "fp"}},
	}
	_, err := selectExportableEpisodes(&cobra.Command{}, held, "", "train", false)
	if err == nil {
		t.Fatal("a run that drew from the eval side must not export into a training corpus")
	}
	if !strings.Contains(err.Error(), "held out") {
		t.Fatalf("the refusal must say what is wrong: %v", err)
	}
	// Explicitly asked for, it goes through — the guard is against accidents,
	// not against a deliberate choice.
	out, err := selectExportableEpisodes(&cobra.Command{}, held, "", "train", true)
	if err != nil || len(out) != 2 {
		t.Fatalf("--allow-eval-side must permit it: %v %d", err, len(out))
	}
	// A train-side run exports unchanged.
	train := []gjeval.Episode{{TaskID: "t1", Provenance: gjeval.RunProvenance{SplitSide: "train"}}}
	if out, err := selectExportableEpisodes(&cobra.Command{}, train, "", "train", false); err != nil || len(out) != 1 {
		t.Fatalf("a train-side run must export: %v %d", err, len(out))
	}
	// And a run that recorded nothing behaves exactly as it always did.
	plain := []gjeval.Episode{{TaskID: "t1"}, {TaskID: "t2"}}
	if out, err := selectExportableEpisodes(&cobra.Command{}, plain, "", "train", false); err != nil || len(out) != 2 {
		t.Fatalf("an unstamped run must export unchanged: %v %d", err, len(out))
	}
}

func TestExportSplitFiltering(t *testing.T) {
	split := gjeval.SuiteSplit{
		SchemaVersion: gjeval.SplitSchemaVersion, SuiteFingerprint: "suite",
		Train: []string{"t1"}, Eval: []string{"t2"},
	}
	path := t.TempDir() + "/split.json"
	if err := gjeval.SaveSplit(path, split); err != nil {
		t.Fatal(err)
	}
	mixed := []gjeval.Episode{{TaskID: "t1"}, {TaskID: "t2"}, {TaskID: "t3"}}

	if _, err := selectExportableEpisodes(&cobra.Command{}, mixed, path, "train", false); err == nil {
		t.Fatal("a mixed run must be refused for a training corpus")
	}
	out, err := selectExportableEpisodes(&cobra.Command{}, mixed, path, "train", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("--allow-eval-side keeps both sides, dropping only what the split never mentions: %d", len(out))
	}
	evalOnly, err := selectExportableEpisodes(&cobra.Command{}, mixed, path, "eval", false)
	if err != nil || len(evalOnly) != 1 || evalOnly[0].TaskID != "t2" {
		t.Fatalf("the eval side must export on request: %v %+v", err, evalOnly)
	}
	if _, err := selectExportableEpisodes(&cobra.Command{}, mixed, path, "sideways", false); err == nil {
		t.Fatal("an unrecognised side must be refused")
	}
	// A split the run did not come from cannot be used to filter it.
	stamped := []gjeval.Episode{{TaskID: "t1", Provenance: gjeval.RunProvenance{SplitFingerprint: "another-split"}}}
	if _, err := selectExportableEpisodes(&cobra.Command{}, stamped, path, "train", false); err == nil {
		t.Fatal("filtering by a split the run did not use must be refused")
	}
}
