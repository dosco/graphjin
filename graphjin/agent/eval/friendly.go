package eval

import (
	"fmt"
	"math"
	"strings"

	gjagent "github.com/dosco/graphjin/agent/v3"
)

// FriendlySummary is the plain-language projection of a canonical evaluation
// report. It deliberately contains counts and prose only; technical benchmark
// metrics remain available in the report and technical Markdown artifact.
type FriendlySummary struct {
	Complete                bool   `json:"complete"`
	Title                   string `json:"title"`
	Message                 string `json:"message"`
	QuestionCount           int    `json:"question_count"`
	DataQuestionCount       int    `json:"data_question_count,omitempty"`
	MethodQuestionCount     int    `json:"method_question_count,omitempty"`
	CompletedTestAttempts   int    `json:"completed_test_attempts"`
	PlannedTestAttempts     int    `json:"planned_test_attempts"`
	CorrectAnswerQuestions  int    `json:"correct_answer_questions,omitempty"`
	RequiredMethodQuestions int    `json:"required_method_questions,omitempty"`
	FullPassQuestions       int    `json:"full_pass_questions,omitempty"`
	SafetyRulesFollowed     int    `json:"safety_rules_followed,omitempty"`
	PassedEveryAttempt      int    `json:"passed_every_attempt,omitempty"`
	GuardInterventions      int    `json:"guard_interventions,omitempty"`
	ForbiddenAttempts       int    `json:"forbidden_attempts,omitempty"`
	UnsafeEffects           int    `json:"unsafe_effects"`
	// Compatibility aliases retained for API clients that consumed the first
	// friendly summary shape. New presentation uses the explicit dimensions.
	QuestionsPassedReliably    int `json:"questions_passed_reliably,omitempty"`
	QuestionsSolvedAtLeastOnce int `json:"questions_solved_at_least_once,omitempty"`
	QuestionsSolvedEveryTime   int `json:"questions_solved_every_time,omitempty"`
	InconsistentQuestions      int `json:"inconsistent_questions,omitempty"`
	NeverSolvedQuestions       int `json:"never_solved_questions,omitempty"`
}

func SummarizeReport(report Report) FriendlySummary {
	total := report.Metrics.TaskCount
	answerPasses, answerTotal := taskDimensionCounts(report.Tasks, func(task TaskVerdict) *bool { return task.GroundTruthPass })
	methodPasses, methodTotal := taskDimensionCounts(report.Tasks, func(task TaskVerdict) *bool { return task.MethodPass })
	fullPass := metricCount(report.Metrics.Recall, total)
	safetyPass := metricCount(report.Metrics.SafetyPrecision, total)
	any := metricCount(report.Metrics.PassAtK, total)
	all := metricCount(report.Metrics.PassPowerK, total)
	inconsistent := max(0, any-fullPass)
	never := max(0, total-any)
	message := fmt.Sprintf("The agent fully passed %d of %d %s.", fullPass, total, questionWord(total))
	if answerTotal > 0 {
		message = fmt.Sprintf("The agent returned a correct answer on %d of %d data questions and used the required database method on %d of %d. It fully passed %d of %d tasks.",
			answerPasses, answerTotal, methodPasses, methodTotal, fullPass, total)
	}
	return FriendlySummary{
		Complete: true, Title: "Evaluation complete", Message: message,
		QuestionCount: total, DataQuestionCount: answerTotal, MethodQuestionCount: methodTotal, CompletedTestAttempts: report.Metrics.EpisodeCount,
		PlannedTestAttempts:    report.Progress.PlannedInitialSlots,
		CorrectAnswerQuestions: answerPasses, RequiredMethodQuestions: methodPasses,
		FullPassQuestions: fullPass, SafetyRulesFollowed: safetyPass, PassedEveryAttempt: all,
		GuardInterventions: report.Metrics.GuardInterventions, ForbiddenAttempts: report.Metrics.ForbiddenAttempts, UnsafeEffects: report.Metrics.UnsafeEffects,
		QuestionsPassedReliably: fullPass, QuestionsSolvedAtLeastOnce: any,
		QuestionsSolvedEveryTime: all, InconsistentQuestions: inconsistent,
		NeverSolvedQuestions: never,
	}
}

func taskDimensionCounts(tasks []TaskVerdict, dimension func(TaskVerdict) *bool) (passes, total int) {
	for _, task := range tasks {
		value := dimension(task)
		if value == nil {
			continue
		}
		total++
		if *value {
			passes++
		}
	}
	return passes, total
}

func SummarizePartialReport(report PartialReport) FriendlySummary {
	planned := report.Progress.PlannedInitialSlots
	completed := report.Progress.CompletedInitialSlots
	questions := 0
	if report.Provenance.Repeats > 0 {
		questions = planned / report.Provenance.Repeats
	}
	reason := friendlyStopReason(report.EnvironmentCode, report.Provenance.Provider)
	message := fmt.Sprintf("GraphJin completed %d of %d test attempts before %s. Your completed work is saved. No overall performance score is available yet.", completed, planned, reason)
	return FriendlySummary{
		Complete: false, Title: "This evaluation did not finish", Message: message,
		QuestionCount: questions, CompletedTestAttempts: completed, PlannedTestAttempts: planned,
	}
}

func SummarizeStoredReport(report StoredReport) FriendlySummary {
	if report.RunStatus == "" || report.RunStatus == RunStatusComplete {
		return SummarizeReport(report.Report)
	}
	return SummarizePartialReport(PartialReport{
		RunID: report.RunID, RunStatus: report.RunStatus, Provenance: report.Provenance,
		Progress: report.Progress, ProviderUsage: report.ProviderUsage,
		EnvironmentCode: report.EnvironmentCode, Notice: report.Notice,
	})
}

func metricCount(ratio float64, total int) int {
	if total <= 0 || math.IsNaN(ratio) || math.IsInf(ratio, 0) {
		return 0
	}
	value := int(math.Round(ratio * float64(total)))
	return min(total, max(0, value))
}

func friendlyStopReason(code, provider string) string {
	name := friendlyProviderName(provider)
	switch strings.TrimSpace(code) {
	case gjagent.ErrorCodeProviderQuota:
		return name + " stopped accepting requests because of quota limits"
	case gjagent.ErrorCodeProviderRateLimit:
		return name + " temporarily stopped accepting requests because its request limit was reached"
	case gjagent.ErrorCodeProviderTimeout:
		return name + " did not respond before the configured timeout"
	case gjagent.ErrorCodeProviderAuth:
		return name + " rejected the configured credentials"
	case gjagent.ErrorCodeProviderModelUnavailable:
		return "the configured model was unavailable from " + name
	case gjagent.ErrorCodeProviderServer:
		return name + " reported a temporary service problem"
	case gjagent.ErrorCodeProviderTransport:
		return "GraphJin lost its connection to " + name
	case "interrupted", "":
		return "the evaluation was stopped"
	default:
		return "the evaluation environment stopped the run"
	}
}

func friendlyProviderName(provider string) string {
	trimmed := strings.TrimSpace(provider)
	lower := strings.ToLower(trimmed)
	switch {
	case strings.Contains(lower, "google"), strings.Contains(lower, "gemini"):
		return "Google"
	case strings.Contains(lower, "openai"), strings.Contains(lower, "gpt"):
		return "OpenAI"
	case strings.Contains(lower, "anthropic"), strings.Contains(lower, "claude"):
		return "Anthropic"
	case trimmed != "":
		return trimmed
	default:
		return "the model provider"
	}
}

func questionWord(n int) string {
	if n == 1 {
		return "question"
	}
	return "questions"
}

func questionWasWere(n int) string {
	if n == 1 {
		return "question was"
	}
	return "questions were"
}

func sentenceCount(n int) string {
	words := []string{"Zero", "One", "Two", "Three", "Four", "Five", "Six", "Seven", "Eight", "Nine", "Ten"}
	if n >= 0 && n < len(words) {
		return words[n]
	}
	return fmt.Sprint(n)
}
