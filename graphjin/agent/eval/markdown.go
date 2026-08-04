package eval

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

const ReportMarkdownVersion = "graphjin.eval.report.md/v1"

// RenderReportMarkdown renders a finalized report as shareable GFM. Task slugs,
// invalid-oracle prose, and local episode paths are cleared before rendering so
// Markdown can never be more revealing than reports/<run-id>.json.
func RenderReportMarkdown(report Report) string {
	report.InvalidOracleDetails = nil
	report.EpisodePaths = nil
	report.Tasks = publicTaskVerdicts(report.Tasks)

	var b strings.Builder
	writeReportTitle(&b, report.RunID, report.RunStatus)
	writeIdentity(&b, report)
	writeComparability(&b, report)
	writeHeadline(&b, report.Metrics, report.Provenance.Repeats)
	writeTiers(&b, report.Metrics)
	writeFailures(&b, report.Metrics.FailureCategories)
	writeEfficiency(&b, report.Metrics, report.ProviderUsage)
	if report.UsageComparison != nil {
		writeBaselineComparison(&b, *report.UsageComparison)
	}
	writeAcceptance(&b, report.Acceptance)
	writeTaskVerdicts(&b, report.Tasks)
	writeInvalidOracles(&b, report.InvalidOracles)
	writePrivacyFooter(&b)
	return b.String()
}

func RenderPartialReportMarkdown(report PartialReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# GraphJin evaluation report `%s`\n\n", markdownCell(report.RunID))
	fmt.Fprintf(&b, "> Status: **%s**. Finalized quality metrics are unavailable.\n\n", markdownCell(string(report.RunStatus)))
	writePartialIdentity(&b, report)
	if report.EnvironmentCode != "" || report.Notice != "" {
		b.WriteString("## Run notice\n\n")
		if report.EnvironmentCode != "" {
			fmt.Fprintf(&b, "- Environment code: `%s`\n", markdownCell(report.EnvironmentCode))
		}
		if report.Notice != "" {
			fmt.Fprintf(&b, "- %s\n", markdownText(report.Notice))
		}
		b.WriteByte('\n')
	}
	b.WriteString("## Progress\n\n")
	b.WriteString("| Initial slots | Confirmation slots | Provider attempts | Retries |\n")
	b.WriteString("| ---: | ---: | ---: | ---: |\n")
	fmt.Fprintf(&b, "| %d/%d | %d/%d | %d | %d |\n\n",
		report.Progress.CompletedInitialSlots, report.Progress.PlannedInitialSlots,
		report.Progress.CompletedConfirmation, report.Progress.PlannedConfirmationSlots,
		report.Progress.ProviderAttempts, report.Progress.RetryCount)
	writeProviderUsage(&b, report.ProviderUsage)
	writePrivacyFooter(&b)
	return b.String()
}

func publicTaskVerdicts(tasks []TaskVerdict) []TaskVerdict {
	out := append([]TaskVerdict(nil), tasks...)
	for i := range out {
		out[i].Slug = ""
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TaskID < out[j].TaskID })
	return out
}

func writeReportTitle(b *strings.Builder, runID string, status RunStatus) {
	fmt.Fprintf(b, "# GraphJin evaluation report `%s`\n\n", markdownCell(runID))
	fmt.Fprintf(b, "> Status: **%s** · Markdown schema: `%s`\n\n", markdownCell(string(status)), ReportMarkdownVersion)
}

func writeIdentity(b *strings.Builder, r Report) {
	b.WriteString("## Identity and provenance\n\n")
	b.WriteString("| Field | Value |\n| --- | --- |\n")
	writeRow(b, "Generated", formatTime(r.GeneratedAt))
	writeRow(b, "Mode", string(r.Mode))
	writeRow(b, "Provider", displayValue(r.Provenance.Provider))
	writeRow(b, "Model", displayValue(r.Provenance.Model))
	writeRow(b, "Target", displayValue(r.Provenance.Target))
	writeRow(b, "GraphJin commit", displayValue(r.Provenance.GraphJinCommit))
	writeRow(b, "Binary fingerprint", displayValue(r.Provenance.BinaryFingerprint))
	writeRow(b, "Ax version", displayValue(r.Provenance.AxVersion))
	writeRow(b, "Prompt registry hash", displayValue(r.Provenance.PromptRegistryHash))
	writeRow(b, "Seed", fmt.Sprint(r.Provenance.Seed))
	writeRow(b, "Repeats", fmt.Sprint(r.Provenance.Repeats))
	writeRow(b, "Max steps", formatPositive(r.Provenance.MaxSteps))
	writeRow(b, "Temperature", fmt.Sprintf("%.2f", r.Provenance.Temperature))
	b.WriteByte('\n')
}

func writePartialIdentity(b *strings.Builder, r PartialReport) {
	b.WriteString("## Identity and provenance\n\n")
	b.WriteString("| Field | Value |\n| --- | --- |\n")
	writeRow(b, "Generated", formatTime(r.GeneratedAt))
	writeRow(b, "Mode", string(r.Mode))
	writeRow(b, "Provider", displayValue(r.Provenance.Provider))
	writeRow(b, "Model", displayValue(r.Provenance.Model))
	writeRow(b, "Target", displayValue(r.Provenance.Target))
	writeRow(b, "Suite fingerprint", displayValue(r.SuiteFingerprint))
	writeRow(b, "Catalog hash", displayValue(r.DatasetFingerprint.CatalogHash))
	b.WriteByte('\n')
}

func writeComparability(b *strings.Builder, r Report) {
	b.WriteString("## Comparability\n\n")
	b.WriteString("The ranked cohort identity is computed from the fields below. Oracle value hash and data anchor are retained as audit fields but excluded because calendar-relative demo data can change them without changing the suite.\n\n")
	b.WriteString("| Cohort field | Value |\n| --- | --- |\n")
	writeRow(b, "Suite identity", SuiteIdentity(r))
	writeRow(b, "Suite fingerprint", displayValue(r.SuiteFingerprint))
	writeRow(b, "Catalog hash", displayValue(r.DatasetFingerprint.CatalogHash))
	writeRow(b, "Seed manifest hash", displayValue(r.DatasetFingerprint.SeedManifestHash))
	writeRow(b, "Reward version", displayValue(r.RewardVersion))
	writeRow(b, "Oracle value hash (audit only)", displayValue(r.OracleValueHash))
	writeRow(b, "Data anchor (audit only)", displayValue(r.DatasetFingerprint.DataAnchor))
	b.WriteByte('\n')
}

func writeHeadline(b *strings.Builder, m Metrics, repeats int) {
	if repeats <= 0 {
		repeats = DefaultRepeats
	}
	fmt.Fprintf(b, "## Headline\n\n")
	b.WriteString("| Recall | 95% CI | Pass@k | Pass^k | Answer recall | Method recall | Safety | Behavior recall | Mean reward |\n")
	b.WriteString("| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	fmt.Fprintf(b, "| %s | %s–%s | %s | %s | %s | %s | %s | %s | %.3f |\n\n",
		percent(m.Recall), percent(m.RecallCI.Low), percent(m.RecallCI.High), percent(m.PassAtK), percent(m.PassPowerK),
		percent(m.GroundTruthRecall), percent(m.MethodRecall), percent(m.SafetyPrecision), percent(m.BehaviorRecall), m.MeanReward)
	fmt.Fprintf(b, "Pass@k and Pass^k use k=%d rollout%s per task.\n\n", repeats, plural(repeats))
}

func writeTiers(b *strings.Builder, m Metrics) {
	b.WriteString("## By difficulty tier\n\n")
	b.WriteString("| Tier | Tasks | Recall | 95% CI | Pass@k | Pass^k |\n| --- | ---: | ---: | ---: | ---: | ---: |\n")
	for _, tier := range []Difficulty{DifficultyT1, DifficultyT2, DifficultyT3, DifficultyT4} {
		v, ok := m.ByTier[tier]
		if !ok {
			fmt.Fprintf(b, "| %s | 0 | n/a | n/a | n/a | n/a |\n", tier)
			continue
		}
		fmt.Fprintf(b, "| %s | %d | %s | %s–%s | %s | %s |\n", tier, v.TaskCount, percent(v.Recall), percent(v.RecallCI.Low), percent(v.RecallCI.High), percent(v.PassAtK), percent(v.PassPowerK))
	}
	b.WriteByte('\n')
}

func writeFailures(b *strings.Builder, failures map[string]int) {
	b.WriteString("## Failure categories\n\n")
	if len(failures) == 0 {
		b.WriteString("No task failures were categorized.\n\n")
		return
	}
	keys := make([]string, 0, len(failures))
	for key := range failures {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	b.WriteString("| Category | Tasks |\n| --- | ---: |\n")
	for _, key := range keys {
		fmt.Fprintf(b, "| %s | %d |\n", markdownCell(key), failures[key])
	}
	b.WriteByte('\n')
}

func writeEfficiency(b *strings.Builder, m Metrics, usage ProviderUsage) {
	b.WriteString("## Efficiency\n\n")
	b.WriteString("Finalized usage covers scored episodes. Provider usage also includes failed attempts and retries.\n\n")
	b.WriteString("| Usage | Episodes/attempts | Prompt tokens | Completion tokens | Total tokens | LLM calls | Latency | Tokens/episode |\n")
	b.WriteString("| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	perEpisode := "n/a"
	if m.EpisodeCount > 0 {
		perEpisode = fmt.Sprintf("%.1f", float64(m.TotalTokens)/float64(m.EpisodeCount))
	}
	fmt.Fprintf(b, "| Finalized | %d | %d | %d | %d | %d | p50 %.0f ms / p95 %.0f ms | %s |\n",
		m.EpisodeCount, m.PromptTokens, m.CompletionTokens, m.TotalTokens, m.LLMCalls, m.LatencyP50MS, m.LatencyP95MS, perEpisode)
	fmt.Fprintf(b, "| Provider | %s | %d | %d | %d | %d | %d ms | n/a |\n\n",
		providerAttemptLabel(usage), usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens, usage.LLMCalls, usage.LatencyMS)
	writeProviderCompleteness(b, usage)
}

func writeProviderUsage(b *strings.Builder, usage ProviderUsage) {
	b.WriteString("## Provider usage\n\n")
	b.WriteString("| Prompt tokens | Completion tokens | Total tokens | LLM calls | Latency |\n| ---: | ---: | ---: | ---: | ---: |\n")
	fmt.Fprintf(b, "| %d | %d | %d | %d | %d ms |\n\n", usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens, usage.LLMCalls, usage.LatencyMS)
	writeProviderCompleteness(b, usage)
}

func writeProviderCompleteness(b *strings.Builder, usage ProviderUsage) {
	if usage.Complete {
		b.WriteString("Provider usage accounting is complete for every attempt.\n\n")
		return
	}
	fmt.Fprintf(b, "Provider usage accounting is incomplete: %d attempt%s returned no usage. Recorded tokens are a lower bound.\n\n", usage.UnknownAttempts, plural(usage.UnknownAttempts))
}

func writeBaselineComparison(b *strings.Builder, c UsageComparison) {
	b.WriteString("## Baseline comparison\n\n")
	if !c.Comparable {
		fmt.Fprintf(b, "Token usage is advisory and not directly comparable: %s.\n\n", markdownText(displayValue(c.Reason)))
		return
	}
	b.WriteString("| Baseline | Finalized token change | Provider token change | Tokens/episode change |\n| --- | ---: | ---: | ---: |\n")
	fmt.Fprintf(b, "| %s | %s | %s | %s |\n\n", markdownCell(c.BaselineRunID), formatDelta(c.FinalizedTokensDelta, c.FinalizedTokensChangePercent), formatDelta(c.ProviderTokensDelta, c.ProviderTokensChangePercent), formatFloatDelta(c.TokensPerEpisodeDelta, c.TokensPerEpisodeChangePercent))
}

func writeAcceptance(b *strings.Builder, a Acceptance) {
	b.WriteString("## Acceptance\n\n")
	b.WriteString("| Gate | Result |\n| --- | --- |\n")
	writeRow(b, "Suite valid", yesNo(a.SuiteValid))
	writeRow(b, "Safety", yesNo(a.SafetyPass))
	writeRow(b, "No regression", yesNo(a.NoRegression))
	writeRow(b, "Hard pass", yesNo(a.HardPass))
	writeRow(b, "Environment healthy", yesNo(!a.EnvironmentFailure))
	writeRow(b, "Baseline compared", yesNo(a.BaselineCompared))
	writeRow(b, "Value comparison enabled", yesNo(a.ValueComparisonEnabled))
	b.WriteByte('\n')
	if len(a.Notices) != 0 {
		b.WriteString("Notices:\n\n")
		for _, notice := range a.Notices {
			fmt.Fprintf(b, "- %s\n", markdownText(notice))
		}
		b.WriteByte('\n')
	}
}

func writeTaskVerdicts(b *strings.Builder, tasks []TaskVerdict) {
	b.WriteString("## Task verdicts\n\n")
	if len(tasks) == 0 {
		b.WriteString("No finalized task verdicts.\n\n")
		return
	}
	b.WriteString("| Task ID | Category | Tier | Pass | Answer | Method | Safety | Behavior | Consistency | Mean reward | Failure |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- | ---: | ---: | --- |\n")
	for _, task := range tasks {
		fmt.Fprintf(b, "| %s | %s | %s | %s | %s | %s | %s | %s | %.3f | %.3f | %s |\n",
			markdownCell(task.TaskID), markdownCell(string(task.Category)), markdownCell(string(task.Difficulty)), yesNo(task.Pass), optionalYesNo(task.GroundTruthPass), optionalYesNo(task.MethodPass), yesNo(task.SafetyPass), yesNo(task.BehaviorPass), task.Consistency, task.MeanReward, markdownCell(displayValue(task.FailureCategory)))
	}
	b.WriteByte('\n')
}

func writeInvalidOracles(b *strings.Builder, invalid map[string]string) {
	b.WriteString("## Invalid oracles\n\n")
	if len(invalid) == 0 {
		b.WriteString("None.\n\n")
		return
	}
	ids := make([]string, 0, len(invalid))
	for id := range invalid {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	b.WriteString("| Task ID | Failure category |\n| --- | --- |\n")
	for _, id := range ids {
		fmt.Fprintf(b, "| %s | %s |\n", markdownCell(id), markdownCell(invalid[id]))
	}
	b.WriteByte('\n')
}

func writePrivacyFooter(b *strings.Builder) {
	b.WriteString("---\n\nThis shareable report contains aggregate metrics, public task identifiers, fingerprints, and acceptance state. It excludes prompts, answers, database rows, executed queries, request headers, credentials, task slugs, raw oracle errors, and local episode paths.\n")
}

func writeRow(b *strings.Builder, field, value string) {
	fmt.Fprintf(b, "| %s | %s |\n", markdownCell(field), markdownCell(value))
}

func percent(v float64) string { return fmt.Sprintf("%.1f%%", v*100) }

func optionalYesNo(v *bool) string {
	if v == nil {
		return "n/a"
	}
	return yesNo(*v)
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func formatTime(v time.Time) string {
	if v.IsZero() {
		return "n/a"
	}
	return v.UTC().Format(time.RFC3339)
}

func formatPositive(v int) string {
	if v <= 0 {
		return "n/a"
	}
	return fmt.Sprint(v)
}

func displayValue(v string) string {
	if strings.TrimSpace(v) == "" {
		return "n/a"
	}
	return v
}

func providerAttemptLabel(usage ProviderUsage) string {
	if usage.UnknownAttempts > 0 {
		return fmt.Sprintf("%d+", usage.UnknownAttempts)
	}
	return "n/a"
}

func formatDelta(delta int64, pct *float64) string {
	if pct == nil {
		return fmt.Sprintf("%+d", delta)
	}
	return fmt.Sprintf("%+d (%+.1f%%)", delta, *pct)
}

func formatFloatDelta(delta float64, pct *float64) string {
	if pct == nil {
		return fmt.Sprintf("%+.1f", delta)
	}
	return fmt.Sprintf("%+.1f (%+.1f%%)", delta, *pct)
}

func markdownCell(v string) string {
	v = strings.ReplaceAll(v, "|", "\\|")
	v = strings.ReplaceAll(v, "\n", " ")
	v = strings.ReplaceAll(v, "\r", " ")
	return v
}

func markdownText(v string) string {
	v = strings.ReplaceAll(v, "\r", " ")
	v = strings.ReplaceAll(v, "\n", " ")
	return v
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
