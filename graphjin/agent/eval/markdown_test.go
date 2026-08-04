package eval

import (
	"strings"
	"testing"
	"time"
)

func TestRenderReportMarkdownOmitsPrivateFields(t *testing.T) {
	yes := true
	report := markdownTestReport()
	report.Tasks = []TaskVerdict{{TaskID: "task-1", Slug: "private-question-slug", Category: CategoryAggregate, Difficulty: DifficultyT1, Pass: true, GroundTruthPass: &yes, MethodPass: &yes, SafetyPass: true, BehaviorPass: true}}
	report.InvalidOracles = map[string]string{"task-2": "query_failed"}
	report.InvalidOracleDetails = map[string]string{"task-2": "private database error prose"}
	report.EpisodePaths = []string{"/private/local/episode.json"}
	markdown := RenderReportMarkdown(report)
	for _, private := range []string{"private-question-slug", "private database error prose", "/private/local/episode.json"} {
		if strings.Contains(markdown, private) {
			t.Fatalf("markdown leaked %q", private)
		}
	}
	for _, public := range []string{"task-1", "task-2", "query_failed", "## Headline", "## Comparability"} {
		if !strings.Contains(markdown, public) {
			t.Fatalf("markdown omitted %q", public)
		}
	}
}

func TestRenderReportMarkdownIsDeterministic(t *testing.T) {
	report := markdownTestReport()
	report.Metrics.ByTier = map[Difficulty]TierMetrics{
		DifficultyT3: {TaskCount: 1, Recall: .5}, DifficultyT1: {TaskCount: 2, Recall: 1},
	}
	report.Metrics.FailureCategories = map[string]int{"wrong_window": 2, "runaway": 1}
	report.Tasks = []TaskVerdict{{TaskID: "b"}, {TaskID: "a"}}
	report.InvalidOracles = map[string]string{"z": "last", "a": "first"}
	first := RenderReportMarkdown(report)
	second := RenderReportMarkdown(report)
	if first != second {
		t.Fatal("markdown render is not byte deterministic")
	}
	if strings.Index(first, "| a |") > strings.Index(first, "| b |") || strings.Index(first, "| runaway |") > strings.Index(first, "| wrong_window |") {
		t.Fatal("markdown collections are not sorted")
	}
}

func TestRenderPartialReportMarkdownHasNoFinalMetrics(t *testing.T) {
	markdown := RenderPartialReportMarkdown(PartialReport{RunID: "partial", RunStatus: RunStatusInterrupted, Notice: "stopped"})
	for _, section := range []string{"## Headline", "## By difficulty tier", "## Task verdicts", "## Acceptance"} {
		if strings.Contains(markdown, section) {
			t.Fatalf("partial report contains %s", section)
		}
	}
}

func TestRenderZeroReportNeverEmitsNaN(t *testing.T) {
	markdown := RenderReportMarkdown(Report{RunID: "zero", RunStatus: RunStatusComplete})
	if strings.Contains(markdown, "NaN") {
		t.Fatal("zero report rendered NaN")
	}
	if !strings.Contains(markdown, "| Finalized | 0") || !strings.Contains(markdown, "| n/a |") {
		t.Fatal("zero report did not render an honest n/a efficiency state")
	}
}

func markdownTestReport() Report {
	return Report{
		SchemaVersion: ReportSchemaVersion, RewardVersion: RewardVersion,
		RunID: "run-1", RunStatus: RunStatusComplete, Mode: RunModeBenchmark, GeneratedAt: time.Unix(1, 0).UTC(),
		SuiteFingerprint: "suite", DatasetFingerprint: DatasetFingerprint{CatalogHash: "catalog", SeedManifestHash: "manifest", DataAnchor: "anchor"},
		Provenance:    RunProvenance{Provider: "provider", Model: "model", Seed: 23, Repeats: 3, MaxSteps: 8},
		Metrics:       Metrics{TaskCount: 1, EpisodeCount: 3, Recall: 1, GroundTruthRecall: 1, MethodRecall: 1, SafetyPrecision: 1, BehaviorRecall: 1, MeanReward: 1, PassAtK: 1, PassPowerK: 1},
		ProviderUsage: ProviderUsage{Complete: true}, Acceptance: Acceptance{SuiteValid: true, SafetyPass: true, HardPass: true},
	}
}
