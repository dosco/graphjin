package eval

import (
	"strings"
	"testing"
)

func TestSummarizeCompletedReportInPlainLanguage(t *testing.T) {
	report := Report{
		Metrics: Metrics{
			TaskCount: 24, EpisodeCount: 72,
			Recall: 18.0 / 24.0, PassAtK: 22.0 / 24.0, PassPowerK: 15.0 / 24.0,
		},
		Progress: RunProgress{CompletedInitialSlots: 72, PlannedInitialSlots: 72},
	}
	summary := SummarizeReport(report)
	if summary.QuestionsPassedReliably != 18 || summary.QuestionsSolvedAtLeastOnce != 22 || summary.QuestionsSolvedEveryTime != 15 || summary.InconsistentQuestions != 4 || summary.NeverSolvedQuestions != 2 {
		t.Fatalf("summary counts = %+v", summary)
	}
	want := "The agent reliably passed 18 of 24 questions. It solved another 4 at least once but was inconsistent. Two questions were never solved."
	if summary.Message != want {
		t.Fatalf("message = %q, want %q", summary.Message, want)
	}
}

func TestSummarizePartialGoogleQuotaWithoutInventingScore(t *testing.T) {
	report := PartialReport{
		RunStatus:       RunStatusEnvironmentFailed,
		Provenance:      RunProvenance{Provider: "google-gemini", Repeats: 3},
		Progress:        RunProgress{CompletedInitialSlots: 36, PlannedInitialSlots: 72},
		EnvironmentCode: "provider_quota",
	}
	summary := SummarizePartialReport(report)
	want := "GraphJin completed 36 of 72 test attempts before Google stopped accepting requests because of quota limits. Your completed work is saved. No overall performance score is available yet."
	if summary.Title != "This evaluation did not finish" || summary.Message != want || summary.QuestionCount != 24 {
		t.Fatalf("summary = %+v", summary)
	}
	markdown := RenderFriendlyPartialReportMarkdown(report)
	for _, forbidden := range []string{"Recall", "Pass@", "pass^", "provider attempts", "fingerprint"} {
		if strings.Contains(strings.ToLower(markdown), strings.ToLower(forbidden)) {
			t.Fatalf("friendly partial report contains %q: %s", forbidden, markdown)
		}
	}
}

func TestFriendlyStopReasonsRemainSpecific(t *testing.T) {
	tests := map[string]string{
		"provider_rate_limit":        "request limit was reached",
		"provider_timeout":           "configured timeout",
		"provider_auth":              "rejected the configured credentials",
		"provider_model_unavailable": "configured model was unavailable",
		"provider_5xx":               "temporary service problem",
		"provider_transport":         "lost its connection",
		"interrupted":                "evaluation was stopped",
	}
	for code, want := range tests {
		summary := SummarizePartialReport(PartialReport{
			Provenance:      RunProvenance{Provider: "openai", Repeats: 3},
			Progress:        RunProgress{CompletedInitialSlots: 1, PlannedInitialSlots: 3},
			EnvironmentCode: code,
		})
		if !strings.Contains(summary.Message, want) {
			t.Errorf("code %s message = %q, want %q", code, summary.Message, want)
		}
	}
}
