package eval

import (
	"context"
	"strings"
	"testing"
)

// A sampling run collects attempts; it does not judge them. Running it through
// the baseline comparison would answer a question nobody asked — and answer it
// red, since a temperature raised on purpose loses against a greedy baseline
// every time.
func TestSampleRunCollectsWithoutJudging(t *testing.T) {
	task := scoredTask(t)
	suite := Suite{
		Name: "sample", CatalogFingerprint: "catalog",
		Generator: GeneratorMeta{Version: GeneratorVersion, Seed: 23, Scale: 1},
		Tasks:     []Task{task},
	}
	if err := suite.Normalize(); err != nil {
		t.Fatal(err)
	}
	store := NewStore(t.TempDir())
	instance := &StaticInstance{URL: "http://graphjin.test", TargetLabel: "test"}

	prepared, err := (Runner{Client: &scriptedEvalDoer{alwaysPass: true}}).Prepare(
		context.Background(), suite, instance, RunOptions{
			Mode: RunModeSample, Repeats: 4, Seed: 23, Store: store,
		})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close() //nolint:errcheck
	report, err := prepared.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if report.Mode != RunModeSample {
		t.Fatalf("mode = %q", report.Mode)
	}
	// Every attempt is collected; nothing is spent on a confirmation phase that
	// only exists to double-check a regression against a baseline.
	if report.Progress.PlannedConfirmationSlots != 0 {
		t.Fatalf("a sampling run has no baseline to confirm against: %d slots",
			report.Progress.PlannedConfirmationSlots)
	}
	if report.Tasks[0].EpisodeCount != 4 {
		t.Fatalf("expected 4 attempts, got %d", report.Tasks[0].EpisodeCount)
	}
	// It reaches no verdict, and says so rather than leaving a blank.
	if report.Acceptance.NoRegression || report.Acceptance.HardPass {
		t.Fatalf("a sampling run must not claim a verdict: %+v", report.Acceptance)
	}
	if !strings.Contains(strings.Join(report.Acceptance.Notices, " "), "acceptance gating does not apply") {
		t.Fatalf("the report must say why there is no verdict: %v", report.Acceptance.Notices)
	}
	// What it does produce is the collection summary.
	if report.Metrics.PassAtK == 0 {
		t.Fatal("pass-at-least-once is the number a sampling run exists to produce")
	}
	episodes, err := store.LoadEpisodes(report.RunID)
	if err != nil || len(episodes) != 4 {
		t.Fatalf("every attempt must be stored for export: %v %d", err, len(episodes))
	}
}

// Asking for a baseline alongside sampling is a contradiction, and a caller who
// did it wanted something this cannot give them. Refusing beats ignoring.
func TestSampleRunRefusesBaselineWork(t *testing.T) {
	task := scoredTask(t)
	suite := Suite{
		Name: "sample", CatalogFingerprint: "catalog",
		Generator: GeneratorMeta{Version: GeneratorVersion, Seed: 23, Scale: 1},
		Tasks:     []Task{task},
	}
	if err := suite.Normalize(); err != nil {
		t.Fatal(err)
	}
	instance := &StaticInstance{URL: "http://graphjin.test", TargetLabel: "test"}
	cases := map[string]RunOptions{
		"with a baseline":  {Mode: RunModeSample, Baseline: &Report{}},
		"auto-promoting":   {Mode: RunModeSample, AutoBaseline: true},
		"promoting on ask": {Mode: RunModeSample, DeliberatePromotion: true},
	}
	for name, opts := range cases {
		if _, err := (Runner{Client: &scriptedEvalDoer{alwaysPass: true}}).Prepare(
			context.Background(), suite, instance, opts); err == nil {
			t.Fatalf("%s: expected a refusal", name)
		}
	}
}
