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

// Withholding every number for any gap at all was too blunt. The Muse-Glimmer
// run measured 22.47M tokens across 1,880 calls and had six attempts of 348 with
// unknown counts — 1.7% — and published a blank cost column for it. A benchmark
// whose cost axis is empty on more than half its rows is not serving anyone, so
// a small gap now publishes the measured figure as a disclosed lower bound.
func TestSmallUsageGapPublishesADisclosedLowerBound(t *testing.T) {
	report := gjeval.Report{
		RunID:    "run",
		Metrics:  gjeval.Metrics{TaskCount: 113, Recall: 0.65, TotalTokens: 21_973_891},
		Progress: gjeval.RunProgress{ProviderAttempts: 348},
		ProviderUsage: gjeval.ProviderUsage{
			PromptTokens: 12_000_461, CompletionTokens: 5_053_281, TotalTokens: 22_471_310,
			Complete: false, UnknownAttempts: 6,
		},
	}
	opts := &evalPublishOptions{PromptPricePerMillion: 0.35, CompletionPricePerMillion: 1.50}

	entry := benchmarkEntryFromReport(report, "slug", "label", "rel", "notes", true, "", opts)

	if !entry.UsageIncomplete {
		t.Fatal("the gap must still be disclosed on the row")
	}
	if !entry.CostIsLowerBound {
		t.Fatal("a disclosed partial cost must be marked as a lower bound")
	}
	if entry.UsageUnknownAttempts != 6 {
		t.Errorf("the reader needs the unknown-attempt count, got %d", entry.UsageUnknownAttempts)
	}
	if entry.PromptTokens != 12_000_461 || entry.CompletionTokens != 5_053_281 {
		t.Errorf("measured tokens must be published, got %d/%d", entry.PromptTokens, entry.CompletionTokens)
	}
	// 12.000461 * 0.35 + 5.053281 * 1.50
	if want := 11.780082; entry.EstimatedListCostUSD < want-0.001 || entry.EstimatedListCostUSD > want+0.001 {
		t.Errorf("cost = %v, want ~%v", entry.EstimatedListCostUSD, want)
	}
	if entry.EstimatedListCostPerTaskUSD <= 0 {
		t.Error("per-task cost should be derived too")
	}
}

// Above the threshold the understatement could be large enough to mislead, so
// the original all-or-nothing behaviour still applies.
func TestLargeUsageGapStillWithholdsCost(t *testing.T) {
	report := gjeval.Report{
		RunID:    "run",
		Metrics:  gjeval.Metrics{TaskCount: 113, Recall: 0.5},
		Progress: gjeval.RunProgress{ProviderAttempts: 100},
		ProviderUsage: gjeval.ProviderUsage{
			PromptTokens: 1_000_000, CompletionTokens: 500_000, TotalTokens: 1_500_000,
			Complete: false, UnknownAttempts: 20, // 20% unknown
		},
	}
	opts := &evalPublishOptions{PromptPricePerMillion: 1, CompletionPricePerMillion: 2}

	entry := benchmarkEntryFromReport(report, "slug", "label", "rel", "notes", true, "", opts)

	if entry.EstimatedListCostUSD != 0 || entry.PromptTokens != 0 {
		t.Errorf("a material gap must withhold usage, got cost=%v prompt=%d",
			entry.EstimatedListCostUSD, entry.PromptTokens)
	}
	if entry.CostIsLowerBound {
		t.Error("nothing was published, so nothing can be a lower bound")
	}
}

// The override should only be demanded when it buys something. A gap small
// enough to publish honestly needs no operator acknowledgement — that friction is
// what left fifteen rows uncosted.
func TestOnlyMaterialGapsDemandTheOverrideFlag(t *testing.T) {
	small := gjeval.Report{
		Progress:      gjeval.RunProgress{ProviderAttempts: 348},
		ProviderUsage: gjeval.ProviderUsage{Complete: false, UnknownAttempts: 6},
	}
	if usageGapNeedsAcknowledgement(small) {
		t.Error("a 1.7% gap should publish without --allow-incomplete-usage")
	}
	large := gjeval.Report{
		Progress:      gjeval.RunProgress{ProviderAttempts: 100},
		ProviderUsage: gjeval.ProviderUsage{Complete: false, UnknownAttempts: 20},
	}
	if !usageGapNeedsAcknowledgement(large) {
		t.Error("a 20% gap must still require the override")
	}
	clean := gjeval.Report{
		Progress:      gjeval.RunProgress{ProviderAttempts: 339},
		ProviderUsage: gjeval.ProviderUsage{Complete: true},
	}
	if usageGapNeedsAcknowledgement(clean) {
		t.Error("a clean run needs no override")
	}
	// No attempt count means the share is uncomputable; withhold rather than guess.
	unknown := gjeval.Report{ProviderUsage: gjeval.ProviderUsage{Complete: false, UnknownAttempts: 4}}
	if !usageGapNeedsAcknowledgement(unknown) {
		t.Error("an uncomputable share must fall back to requiring the override")
	}
}
