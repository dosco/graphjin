package serv

import "strings"

// Single source of truth for the rules that apply when a database is in
// analytics mode. Restated across every discovery surface so an agent that
// reads `analytics_mode: true` early doesn't lose the implications by the
// time they're building a query (they don't re-read the overview).

func analyticsModeRules() []string {
	return []string{
		"No defensive limit:. Implicit row-limit defaults are off — explicit limit: only when you want a top-N.",
		"Aggregations: root the query at the dimension table (small side) and nest down to the fact table. Never root at the fact table and try to bucket with distinct.",
		"distinct: dedupes rows; it does NOT bucket. There is no GROUP BY at non-root levels — only the root selection produces aggregate output.",
		"order_by does not work on aggregation aliases (sum_*, count_*). Sort aggregated results in workflow JavaScript.",
		"Temporal columns require either an explicit filter or @unrestricted. Unfiltered scans of large fact tables are rejected.",
		"Partition-keyed tables require the partition column in the where clause (per dialect partition rules).",
		"Use sum(expr: { mul: [a, b] }) and similar expression aggregates to compute composite measures server-side instead of paginating raw rows.",
	}
}

func analyticsModeBlockMarkdown() string {
	var sb strings.Builder
	sb.WriteString("## Analytics Mode Rules\n\n")
	sb.WriteString("This database is in analytics mode. The following rules govern how queries must be shaped:\n\n")
	for _, r := range analyticsModeRules() {
		sb.WriteString("- ")
		sb.WriteString(r)
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	return sb.String()
}

// analyticsModeOn reports whether the default database is in analytics mode.
// Returns false if the engine is not yet wired up (during onboarding).
func (ms *mcpServer) analyticsModeOn() bool {
	if ms == nil || ms.service == nil || ms.service.gj == nil {
		return false
	}
	return ms.service.gj.EffectiveAnalyticsMode(ms.service.gj.DefaultDatabase())
}
