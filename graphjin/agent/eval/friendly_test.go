package eval

import (
	"strings"
	"testing"
)

func TestSummarizeCompletedReportInPlainLanguage(t *testing.T) {
	tasks := make([]TaskVerdict, 24)
	for i := range tasks {
		tasks[i].SafetyPass = true
		if i < 22 {
			tasks[i].GroundTruthPass = boolPointer(i < 20)
			tasks[i].MethodPass = boolPointer(i < 18)
		}
	}
	report := Report{
		Metrics: Metrics{
			TaskCount: 24, EpisodeCount: 72,
			Recall: 18.0 / 24.0, GroundTruthRecall: 20.0 / 22.0, MethodRecall: 18.0 / 22.0,
			SafetyPrecision: 1, PassAtK: 22.0 / 24.0, PassPowerK: 15.0 / 24.0,
		},
		Progress: RunProgress{CompletedInitialSlots: 72, PlannedInitialSlots: 72},
		Tasks:    tasks,
	}
	summary := SummarizeReport(report)
	if summary.DataQuestionCount != 22 || summary.MethodQuestionCount != 22 || summary.CorrectAnswerQuestions != 20 || summary.RequiredMethodQuestions != 18 || summary.FullPassQuestions != 18 || summary.SafetyRulesFollowed != 24 || summary.PassedEveryAttempt != 15 {
		t.Fatalf("summary counts = %+v", summary)
	}
	want := "The agent returned a correct answer on 20 of 22 data questions and used the required database method on 18 of 22. It fully passed 18 of 24 tasks."
	if summary.Message != want {
		t.Fatalf("message = %q, want %q", summary.Message, want)
	}
	markdown := RenderFriendlyReportMarkdown(report)
	for _, want := range []string{"Correct answer", "Required database method", "Full pass (both)", "Safety rules followed", "Passed on every attempt"} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("friendly report omitted %q: %s", want, markdown)
		}
	}
	if strings.Contains(markdown, "Never solved") {
		t.Fatalf("friendly report still conflates answer and method failures: %s", markdown)
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
