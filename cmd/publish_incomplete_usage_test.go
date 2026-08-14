package main

import (
	"testing"

	gjeval "github.com/dosco/graphjin/agent/v3/eval"
)

// A run interrupted by provider failures keeps every score — each episode still
// ran and was graded — but loses the token counts for attempts in flight. The
// publisher used to block such a run outright, discarding a valid result to
// protect a secondary number, with no way to say "score good, cost unknown".
// Flash's completed 339-episode run hit exactly that after six provider
// interruptions.
func TestIncompleteUsageWithholdsCostRatherThanUnderstatingIt(t *testing.T) {
	report := gjeval.Report{
		RunID: "run", Metrics: gjeval.Metrics{TaskCount: 100, Recall: 0.56, TotalTokens: 500},
		ProviderUsage: gjeval.ProviderUsage{
			PromptTokens: 111, CompletionTokens: 222, TotalTokens: 333,
			Complete: false, UnknownAttempts: 4,
		},
	}
	opts := &evalPublishOptions{PromptPricePerMillion: 10, CompletionPricePerMillion: 30, PricingSource: "list"}

	entry := benchmarkEntryFromReport(report, "slug", "label", "rel", "notes", true, "", opts)

	if !entry.UsageIncomplete {
		t.Fatal("an interrupted run must be marked usage_incomplete")
	}
	// Partial totals must not be printed: understating cost silently is worse than
	// omitting it.
	for name, got := range map[string]int64{
		"prompt": entry.PromptTokens, "completion": entry.CompletionTokens,
		"provider total": entry.ProviderTotalTokens, "measured total": entry.TotalTokens,
	} {
		if got != 0 {
			t.Errorf("%s tokens should be withheld, got %d", name, got)
		}
	}
	if entry.EstimatedListCostUSD != 0 || entry.EstimatedListCostPerTaskUSD != 0 || entry.PricingSource != "" {
		t.Errorf("cost must be withheld, got %v / %v / %q",
			entry.EstimatedListCostUSD, entry.EstimatedListCostPerTaskUSD, entry.PricingSource)
	}
	// The score is untouched — that is the whole point of the hatch.
	if entry.Recall != 0.56 || entry.TaskCount != 100 {
		t.Errorf("scores must survive: recall=%v tasks=%d", entry.Recall, entry.TaskCount)
	}

	// A clean run keeps its full receipt.
	report.ProviderUsage.Complete, report.ProviderUsage.UnknownAttempts = true, 0
	clean := benchmarkEntryFromReport(report, "slug", "label", "rel", "notes", true, "", opts)
	if clean.UsageIncomplete || clean.PromptTokens != 111 || clean.EstimatedListCostUSD == 0 || clean.PricingSource != "list" {
		t.Fatalf("a complete run must publish its usage and cost: %+v", clean)
	}
}
