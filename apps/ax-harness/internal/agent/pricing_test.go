package agent

import (
	"testing"

	"github.com/open-neko/openneko/apps/ax-harness/internal/catalog"
)

func TestRunUsageIsCatalogPricedOnlyWhenEveryCallIs(t *testing.T) {
	rates, ok := catalog.Lookup("google-gemini", "gemini-3.8-flash")
	if !ok {
		t.Fatal("gemini-3.8-flash has no catalog price")
	}
	call := func(priced bool) ModelUsage {
		u := ModelUsage{Reported: 1, InputTokens: 1_000, CacheReadTokens: 1_000, OutputTokens: 100, TotalTokens: 2_150}
		u.price(rates, priced, "v-test")
		return u
	}
	first := call(true)
	// $1.50 input, $0.15 cache read, $7.50 output; 50 reasoning tokens bill as output.
	if first.CostMicros != 1_500+150+1_125 || first.PricedBy != "catalog" || first.CatalogVersion != "v-test" {
		t.Fatalf("call usage=%+v", first)
	}
	run := ModelUsage{Requests: 2}
	run.AddReported(first)
	run.AddReported(first)
	run.setCoverage()
	if run.CostMicros != 2*first.CostMicros || run.PricedBy != "catalog" {
		t.Fatalf("run usage=%+v", run)
	}
	run.Requests++
	run.AddReported(call(false))
	run.setCoverage()
	if run.PricedBy != "unpriced" {
		t.Fatalf("a run with an unpriced call reported %q", run.PricedBy)
	}
}
