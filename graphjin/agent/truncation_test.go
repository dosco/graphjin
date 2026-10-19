package agent

import (
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3"
)

func TestAttachTruncationDecoratesResult(t *testing.T) {
	roots := []core.TruncatedRootInfo{
		{Path: "usage_events", Rows: 10, Limit: 10},
		{Path: "accounts.invoices", Rows: 20, Limit: 20},
	}
	result := attachTruncation(executeResult{Data: map[string]any{"usage_events": []any{}}}, roots)
	if result.Truncation == nil {
		t.Fatal("truncation not attached")
	}
	if len(result.Truncation.Roots) != 2 {
		t.Fatalf("roots = %+v", result.Truncation.Roots)
	}
	message := result.Truncation.Message
	for _, want := range []string{
		"usage_events returned 10 rows and reached its row limit (10)",
		"accounts.invoices returned 20 rows and reached its row limit (20)",
		"more rows may exist",
		"pages, not the population",
		"count_/sum_/avg_/min_/max_<column>",
		"order_by on the aggregate",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("truncation message missing %q:\n%s", want, message)
		}
	}
}

func TestAttachTruncationNoOpWithoutRoots(t *testing.T) {
	result := attachTruncation(executeResult{Data: "x"}, nil)
	if result.Truncation != nil {
		t.Fatalf("unexpected truncation: %+v", result.Truncation)
	}
}

func TestExecuteResultSummaryCarriesTruncation(t *testing.T) {
	out := map[string]any{
		"data": map[string]any{"usage_events": []any{map[string]any{"id": 1}}},
		"truncation": map[string]any{
			"roots":   []any{map[string]any{"path": "usage_events", "rows": 10, "limit": 10}},
			"message": "GraphJin truncation: ...",
		},
	}
	summary := resultSummary("execute_graphql", map[string]any{"query": "query { usage_events { id } }"}, out)
	if summary["truncated"] == nil {
		t.Fatalf("summary missing truncated marker: %+v", summary)
	}
	if summary["has_data"] != true {
		t.Fatalf("summary lost has_data: %+v", summary)
	}

	clean := resultSummary("execute_graphql", nil, map[string]any{"data": map[string]any{}})
	if _, ok := clean["truncated"]; ok {
		t.Fatalf("clean result gained truncated marker: %+v", clean)
	}
}

func TestRedactArgsPreservesOrdinaryGraphQLForMethodScoring(t *testing.T) {
	query := "mutation { gj_watch(insert: {query: \"" + strings.Repeat("x", 600) + "\", delivery_json: {kind: \"inbox\"}}) { id } }"
	args := redactArgs(map[string]any{
		"query":     query,
		"variables": map[string]any{"token": "secret"},
	})
	if got := args["query"]; got != query {
		t.Fatalf("ordinary GraphQL action query was truncated: got %d bytes, want %d", len(got.(string)), len(query))
	}
	if args["variables"] != "[redacted]" {
		t.Fatalf("variables were not redacted: %+v", args)
	}
}
