package main

import (
	"bytes"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gjeval "github.com/dosco/graphjin/agent/v3/eval"
	"github.com/spf13/cobra"
)

func TestEvalPublishRefusalsAndLowScoreBoundary(t *testing.T) {
	tests := []struct {
		name string
		edit func(*gjeval.Report)
		code int
	}{
		{"incomplete", func(r *gjeval.Report) { r.RunStatus = gjeval.RunStatusInterrupted }, 1},
		{"invalid_suite", func(r *gjeval.Report) { r.Acceptance.SuiteValid = false }, 2},
		{"environment_failed", func(r *gjeval.Report) { r.Acceptance.EnvironmentFailure = true }, 3},
		{"empty", func(r *gjeval.Report) { r.Metrics.TaskCount = 0 }, 1},
		{"empty_commit", func(r *gjeval.Report) { r.Provenance.GraphJinCommit = "" }, 2},
		{"wrong_binary", func(r *gjeval.Report) { r.Provenance.BinaryFingerprint = "different-binary" }, 2},
		{"incomplete_usage", func(r *gjeval.Report) { r.ProviderUsage.Complete = false }, 2},
		{"suspect_scoring", func(r *gjeval.Report) {
			r.Metrics.GroundTruthRecall = .9
			r.Metrics.MethodRecall = .2
		}, 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			project, site := t.TempDir(), t.TempDir()
			report := publishTestReport("20260803T101112.000000000Z-" + tc.name)
			tc.edit(&report)
			writePublishTestReport(t, project, report)
			err := publishTestRun(t, project, site, report.RunID, &evalPublishOptions{Site: site})
			var exitErr *evalExitError
			if !errors.As(err, &exitErr) || exitErr.Code != tc.code {
				t.Fatalf("error = %v, want exit %d", err, tc.code)
			}
		})
	}

	project, site := t.TempDir(), t.TempDir()
	report := publishTestReport("20260803T101112.000000000Z-low-score")
	report.Acceptance.HardPass = false
	report.Metrics.Recall = .31
	writePublishTestReport(t, project, report)
	if err := publishTestRun(t, project, site, report.RunID, &evalPublishOptions{Site: site}); err != nil {
		t.Fatalf("low score was refused: %v", err)
	}
	data, err := loadBenchmarkData(filepath.Join(site, "data", "benchmarks.yaml"))
	if err != nil || len(data.Runs) != 1 || data.Runs[0].Accepted {
		t.Fatalf("published low score = %+v err=%v", data.Runs, err)
	}
}

func TestEvalPublishSuspectScoringRequiresExplicitOverride(t *testing.T) {
	project, site := t.TempDir(), t.TempDir()
	report := publishTestReport("20260803T101112.000000000Z-suspect-override")
	report.Metrics.GroundTruthRecall = .9
	report.Metrics.MethodRecall = .2
	writePublishTestReport(t, project, report)
	if err := publishTestRun(t, project, site, report.RunID, &evalPublishOptions{Site: site}); err == nil || !strings.Contains(err.Error(), "--allow-suspect-scoring") {
		t.Fatalf("suspect publish error = %v", err)
	}
	if err := publishTestRun(t, project, site, report.RunID, &evalPublishOptions{Site: site, AllowSuspectScoring: true}); err != nil {
		t.Fatalf("explicit suspect override failed: %v", err)
	}
	data, err := loadBenchmarkData(filepath.Join(site, "data", "benchmarks.yaml"))
	if err != nil || len(data.Runs) != 1 || !data.Runs[0].ScoringSuspect {
		t.Fatalf("suspect benchmark data = %+v err=%v", data.Runs, err)
	}
}

func TestEvalPublishWritesOneSafeRowAndPage(t *testing.T) {
	project, site := t.TempDir(), t.TempDir()
	report := publishTestReport("20260803T101112.000000000Z-ab12cd34")
	report.Tasks = []gjeval.TaskVerdict{{TaskID: "task", Slug: "private-task-slug"}}
	report.InvalidOracleDetails = map[string]string{"task": "private oracle prose"}
	report.EpisodePaths = []string{"/private/episode.json"}
	writePublishTestReport(t, project, report)
	if err := publishTestRun(t, project, site, report.RunID, &evalPublishOptions{Site: site}); err != nil {
		t.Fatal(err)
	}
	dataPath := filepath.Join(site, "data", "benchmarks.yaml")
	pagePath := filepath.Join(site, "content", "benchmark", "runs", "20260803t101112-ab12cd34.md")
	for _, path := range []string{dataPath, pagePath} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o644 {
			t.Fatalf("mode for %s: info=%v err=%v", path, info, err)
		}
		for _, private := range []string{"private-task-slug", "private oracle prose", "/private/episode.json"} {
			if strings.Contains(string(raw), private) {
				t.Fatalf("%s leaked %q", path, private)
			}
		}
	}
	data, err := loadBenchmarkData(dataPath)
	if err != nil || len(data.Runs) != 1 || !data.Runs[0].Ranked || data.Runs[0].UnrankedReason != "" {
		t.Fatalf("data=%+v err=%v", data, err)
	}
	page, err := os.ReadFile(pagePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, phrase := range []string{"## Evaluation complete", "## Results at a glance", "## Technical benchmark report", "## Headline", "Pass@k"} {
		if !strings.Contains(string(page), phrase) {
			t.Fatalf("published page missing %q: %s", phrase, page)
		}
	}
	if err := publishTestRun(t, project, site, report.RunID, &evalPublishOptions{Site: site}); err == nil {
		t.Fatal("idempotent publish succeeded without --force")
	}
	if err := publishTestRun(t, project, site, report.RunID, &evalPublishOptions{Site: site, Force: true}); err != nil {
		t.Fatalf("forced replacement failed: %v", err)
	}
	data, err = loadBenchmarkData(dataPath)
	if err != nil || len(data.Runs) != 1 {
		t.Fatalf("forced data=%+v err=%v", data.Runs, err)
	}
}

func TestEvalPublishUpgradesLegacySingleTechnicalMarkdown(t *testing.T) {
	project, site := t.TempDir(), t.TempDir()
	report := publishTestReport("20260803T101112.000000000Z-legacy")
	writePublishTestReport(t, project, report)
	store := gjeval.NewStore(filepath.Join(project, gjeval.DefaultStateDir))
	technical, err := store.LoadReportTechnicalMarkdown(report.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.ReportMarkdownPath(report.RunID), technical, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(store.ReportTechnicalMarkdownPath(report.RunID)); err != nil {
		t.Fatal(err)
	}
	if err := publishTestRun(t, project, site, report.RunID, &evalPublishOptions{Site: site}); err != nil {
		t.Fatal(err)
	}
	page, err := os.ReadFile(filepath.Join(site, "content", "benchmark", "runs", "20260803t101112-legacy.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, phrase := range []string{gjeval.FriendlyReportMarkdownVersion, gjeval.TechnicalReportMarkdownVersion} {
		if !strings.Contains(string(page), phrase) {
			t.Fatalf("published legacy page missing %q: %s", phrase, page)
		}
	}
}

func TestEvalPublishOffSuiteIsSeparated(t *testing.T) {
	project, site := t.TempDir(), t.TempDir()
	first := publishTestReport("20260803T101112.000000000Z-first")
	writePublishTestReport(t, project, first)
	if err := publishTestRun(t, project, site, first.RunID, &evalPublishOptions{Site: site}); err != nil {
		t.Fatal(err)
	}
	second := publishTestReport("20260803T101113.000000000Z-second")
	second.DatasetFingerprint.CatalogHash = "other-catalog"
	writePublishTestReport(t, project, second)
	if err := publishTestRun(t, project, site, second.RunID, &evalPublishOptions{Site: site}); err == nil || !strings.Contains(err.Error(), "catalog_hash") {
		t.Fatalf("off-suite publish error = %v", err)
	}
	if err := publishTestRun(t, project, site, second.RunID, &evalPublishOptions{Site: site, AllowOffSuite: true}); err != nil {
		t.Fatal(err)
	}
	data, err := loadBenchmarkData(filepath.Join(site, "data", "benchmarks.yaml"))
	if err != nil || len(data.Runs) != 2 || data.Runs[1].Ranked || !strings.Contains(data.Runs[1].UnrankedReason, "catalog_hash") {
		t.Fatalf("data=%+v err=%v", data.Runs, err)
	}
}

func TestEvalPublishReplacesEmptyPriorCohortMetadata(t *testing.T) {
	project, site := t.TempDir(), t.TempDir()
	report := publishTestReport("20260803T101112.000000000Z-regenerated-cohort")
	writePublishTestReport(t, project, report)
	dataPath := filepath.Join(site, "data", "benchmarks.yaml")
	if err := os.MkdirAll(filepath.Dir(dataPath), 0o755); err != nil {
		t.Fatal(err)
	}
	old := benchmarkData{
		SchemaVersion: benchmarkDataVersion,
		Suite:         benchmarkSuite{Identity: "old-suite", SuiteFingerprint: "old-fingerprint"},
		Runs:          []benchmarkEntry{},
	}
	raw, err := marshalBenchmarkData(old)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dataPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := publishTestRun(t, project, site, report.RunID, &evalPublishOptions{Site: site}); err != nil {
		t.Fatalf("regenerated empty cohort was refused: %v", err)
	}
	data, err := loadBenchmarkData(dataPath)
	if err != nil || len(data.Runs) != 1 || !data.Runs[0].Ranked || data.Suite.SuiteFingerprint != report.SuiteFingerprint {
		t.Fatalf("regenerated cohort data = %+v err=%v", data, err)
	}
}

func TestEvalPublishAdvancesOfficialCohortAndKeepsHistory(t *testing.T) {
	project, site := t.TempDir(), t.TempDir()
	report := publishTestReport("20260803T101112.000000000Z-current-cohort")
	report.Tasks = []gjeval.TaskVerdict{
		{TaskID: "aggregate", Category: gjeval.CategoryAggregate},
		{TaskID: "refusal", Category: gjeval.CategoryRefusal},
	}
	writePublishTestReport(t, project, report)
	dataPath := filepath.Join(site, "data", "benchmarks.yaml")
	if err := os.MkdirAll(filepath.Dir(dataPath), 0o755); err != nil {
		t.Fatal(err)
	}
	old := benchmarkData{
		SchemaVersion: benchmarkDataVersion,
		Suite:         benchmarkSuite{Generation: "2026.1", Identity: "old-suite", SuiteFingerprint: "old-fingerprint"},
		Runs: []benchmarkEntry{{RunID: "old-run", Slug: "old-run", Label: "Historical", Ranked: true,
			Generation: "2026.1", SuiteFingerprint: "old-fingerprint"}},
	}
	raw, err := marshalBenchmarkData(old)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dataPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := publishTestRun(t, project, site, report.RunID, &evalPublishOptions{Site: site}); err != nil {
		t.Fatalf("official cohort advance was refused: %v", err)
	}
	data, err := loadBenchmarkData(dataPath)
	if err != nil {
		t.Fatal(err)
	}
	if data.Suite.SuiteFingerprint != report.SuiteFingerprint || data.Suite.GeneratorVersion != gjeval.GeneratorVersion {
		t.Fatalf("suite did not advance: %+v", data.Suite)
	}
	if data.Suite.CategoryCounts[string(gjeval.CategoryAggregate)] != 1 || data.Suite.CategoryCounts[string(gjeval.CategoryRefusal)] != 1 {
		t.Fatalf("category composition was not recorded: %+v", data.Suite.CategoryCounts)
	}
	for _, entry := range data.Runs {
		switch entry.RunID {
		case "old-run":
			if entry.Ranked || !strings.Contains(entry.UnrankedReason, "2026.1") {
				t.Fatalf("historical row was not demoted clearly: %+v", entry)
			}
		case report.RunID:
			if !entry.Ranked {
				t.Fatalf("current official row is unranked: %+v", entry)
			}
		}
	}
}

func TestEvalPublishRecordsAuditableListPrice(t *testing.T) {
	project, site := t.TempDir(), t.TempDir()
	report := publishTestReport("20260803T101112.000000000Z-priced")
	report.Metrics.LatencyP50MS = 1250
	report.Metrics.LatencyP95MS = 3400
	writePublishTestReport(t, project, report)
	opts := &evalPublishOptions{
		Site: site, PromptPricePerMillion: 2, CompletionPricePerMillion: 8,
		PricingSource: "provider price card, 2026-08-06",
	}
	if err := publishTestRun(t, project, site, report.RunID, opts); err != nil {
		t.Fatal(err)
	}
	data, err := loadBenchmarkData(filepath.Join(site, "data", "benchmarks.yaml"))
	if err != nil || len(data.Runs) != 1 {
		t.Fatalf("priced data = %+v err=%v", data, err)
	}
	entry := data.Runs[0]
	if math.Abs(entry.EstimatedListCostUSD-0.00056) > 1e-12 || math.Abs(entry.EstimatedListCostPerTaskUSD-0.00028) > 1e-12 {
		t.Fatalf("wrong list price calculation: %+v", entry)
	}
	if entry.PromptTokens != 40 || entry.CompletionTokens != 60 || entry.LatencyP50MS != 1250 || entry.PricingSource != opts.PricingSource {
		t.Fatalf("pricing provenance was not preserved: %+v", entry)
	}
}

func TestBenchmarkRunSlug(t *testing.T) {
	if got := benchmarkRunSlug("20260803T101112.000000000Z-ab12cd34"); got != "20260803t101112-ab12cd34" {
		t.Fatalf("slug = %q", got)
	}
}

func publishTestReport(runID string) gjeval.Report {
	return gjeval.Report{
		SchemaVersion: gjeval.ReportSchemaVersion, UsageAccountingVersion: gjeval.UsageAccountingVersion, RewardVersion: gjeval.RewardVersion,
		RunID: runID, RunStatus: gjeval.RunStatusComplete, Mode: gjeval.RunModeBenchmark, GeneratedAt: time.Date(2026, 8, 3, 10, 11, 12, 0, time.UTC),
		SuiteFingerprint:   gjeval.PublicBenchmark().SuiteFingerprint,
		DatasetFingerprint: gjeval.DatasetFingerprint{CatalogHash: "catalog", SeedManifestHash: "manifest", DataAnchor: "anchor"}, OracleValueHash: "oracle",
		Provenance:    gjeval.RunProvenance{Provider: "openai", Model: "gpt-test", GraphJinCommit: "abcdef123456", BinaryFingerprint: evalBinaryFingerprint(), Seed: 23, Repeats: 3, MaxSteps: 8},
		Metrics:       gjeval.Metrics{TaskCount: 2, EpisodeCount: 6, Recall: .5, GroundTruthRecall: .5, MethodRecall: .5, SafetyPrecision: 1, BehaviorRecall: 1, PassAtK: .75, PassPowerK: .25},
		ProviderUsage: gjeval.ProviderUsage{PromptTokens: 40, CompletionTokens: 60, TotalTokens: 100, Complete: true},
		Acceptance:    gjeval.Acceptance{SuiteValid: true, SafetyPass: true, HardPass: true},
	}
}

func writePublishTestReport(t *testing.T, project string, report gjeval.Report) {
	t.Helper()
	store := gjeval.NewStore(filepath.Join(project, gjeval.DefaultStateDir))
	if _, err := store.WriteReport(report); err != nil {
		t.Fatal(err)
	}
}

func publishTestRun(t *testing.T, project, site, runID string, opts *evalPublishOptions) error {
	t.Helper()
	original := cpath
	cpath = project
	defer func() { cpath = original }()
	if opts.Site == "" {
		opts.Site = site
	}
	command := &cobra.Command{}
	command.SetOut(new(bytes.Buffer))
	command.SetErr(new(bytes.Buffer))
	return runEvalPublish(command, &evalCLIOptions{Yes: true}, opts, runID)
}
