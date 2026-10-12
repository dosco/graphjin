package agent

import (
	"strings"
	"testing"
)

func TestCatalogMethodEvidenceIncludesOnlyCompleteSavedDefinitions(t *testing.T) {
	for _, query := range []string{`{ invoices { sum_amount_cents } }`, strings.Repeat("x", catalogEvidenceFieldLimit+1)} {
		out := map[string]any{"cards": []any{map[string]any{
			"id": "saved_query:totals", "kind": "saved_query", "graphql_query": query,
			"evidence_json": `{"name":"totals","source_hash":"fixture"}`,
		}}}
		evidence := catalogCardEvidence("query_catalog", map[string]any{"id": "saved_query:totals"}, out)
		got := mapValue(evidence["saved_query:totals"])["graphql_query"]
		if len(query) <= catalogEvidenceFieldLimit && got != query {
			t.Fatalf("definition missing: %v", got)
		}
		if len(query) > catalogEvidenceFieldLimit && got != nil {
			t.Fatal("truncated definition must not be evidence")
		}
		if catalogCardEvidence("query_catalog", map[string]any{"search": "totals"}, out) != nil {
			t.Fatal("search is not detail evidence")
		}
	}
}
