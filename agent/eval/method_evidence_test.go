package eval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
)

func TestMethodEvidenceScoresHasuraAndVerifiedSavedQueries(t *testing.T) {
	query := `query totals { invoices { sum_amount_cents } }`
	bytes, _ := json.Marshal(query)
	hash := sha256.Sum256(bytes)
	meta, _ := json.Marshal(map[string]any{"name": "totals", "qualified_name": "totals", "source_hash": hex.EncodeToString(hash[:])})
	detail := map[string]any{"tool": "query_catalog", "status": "ok", "args": map[string]any{"id": "saved_query:totals"}, "evidence": map[string]any{"saved_query:totals": map[string]any{"graphql_query": query, "evidence_json": string(meta)}}}
	execution := map[string]any{"tool": "execute_saved_query", "status": "ok", "args": map[string]any{"name": "totals"}}
	_, queries, _, tools, _, _ := actionInventory([]any{detail, execution})
	rule := MethodRule{RequireQueryMatch: []string{"sum_amount_cents"}, ForbidFinalizeFromListOnly: true}
	answer := AnswerRule{Kind: "number"}
	if len(queries) != 1 || !evaluateMethod(rule, answer, queries, tools, nil) {
		t.Fatal("verified saved execution failed method check")
	}
	compat := []string{`{ invoices_aggregate { aggregate { sum { amount_cents } } } }`}
	if !evaluateMethod(rule, answer, compat, []string{"execute_graphql"}, nil) {
		t.Fatal("Hasura operation/column equivalent failed")
	}
	for _, bad := range []string{`{ invoices { sum_amount_cents_other } }`, `{ invoices { sum_amount_cents: amount_cents } }`, `{ invoices_aggregate { aggregate { avg { amount_cents } } } }`} {
		if evaluateMethod(rule, answer, []string{bad}, []string{"execute_graphql"}, nil) {
			t.Fatalf("wrong method passed: %s", bad)
		}
	}
	_, before, _, _, _, _ := actionInventory([]any{execution, detail})
	if len(before) != 0 {
		t.Fatal("future detail lookup gave earlier execution credit")
	}
	execution["summary"] = map[string]any{"error_count": 1}
	_, failed, _, _, _, _ := actionInventory([]any{detail, execution})
	if len(failed) != 0 {
		t.Fatal("failed saved execution gave method credit")
	}
	execution["summary"] = map[string]any{"empty_roots": []any{"invoices"}}
	_, queries, _, tools, _, _ = actionInventory([]any{detail, execution})
	rule.RequireQueryMatch = []string{"invoices"}
	if evaluateMethod(rule, answer, queries, tools, emptyRootsByQuery([]any{detail, execution})) {
		t.Fatal("empty saved-query root credited a required read")
	}
}
