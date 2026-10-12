package methodevidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"testing"
)

func TestAggregateRequirementsUseOperationAndExactColumn(t *testing.T) {
	for _, tc := range []struct {
		requirement, query string
		want               bool
	}{
		{"sum_mrr_cents", `{ accounts { sum_mrr_cents } }`, true},
		{"sum_mrr_cents", `{ accounts_aggregate { aggregate { sum { mrr_cents } } } }`, true},
		{"sum_quantity", `{ usage_events_aggregate { sum { quantity } } }`, true},
		{"sum_quantity", `{ usage_events { sum(column: quantity) } }`, true},
		{"count_", `{ usage_events_aggregate { aggregate { count } } }`, true},
		{"avg_price|sum_price", `{ products_aggregate { aggregate { avg { price } } } }`, true},
		{"sum_price", `{ products { sum_price_other } }`, false},
		{"sum_price", `{ products_aggregate { aggregate { sum { quantity } } } }`, false},
		{"sum_price", `{ products_aggregate { aggregate { avg { price } } } }`, false},
		{"sum_price", `{ products { sum(expr: {mul: [{col: price}, {col: quantity}]}) } }`, false},
		{"sum_price", `query sum_price { products(where:{name:{eq:"sum_price"}}) { sum_price: price } } # sum_price`, false},
		{"sum_price", `{ products { sum_price`, false},
		{"count_id", `{ products_aggregate { aggregate { count } } }`, false}, // implicit column is unknown
	} {
		p := regexp.MustCompile("(?i)" + tc.requirement)
		if got := MatchRequired(p, tc.requirement, tc.query); got != tc.want {
			t.Fatalf("%s on %s = %v, want %v", tc.requirement, tc.query, got, tc.want)
		}
	}
	if HasAggregate(`{ products { count_id: id } } # count_id`) {
		t.Fatal("row aliases/comments are not aggregates")
	}
}

func savedReceipt(name, namespace, query string) map[string]any {
	data, _ := json.Marshal(query)
	hash := sha256.Sum256(data)
	qualified := name
	if namespace != "" {
		qualified = namespace + "." + name
	}
	meta, _ := json.Marshal(map[string]any{"name": name, "namespace": namespace, "qualified_name": qualified, "source_hash": hex.EncodeToString(hash[:])})
	return map[string]any{"tool": "query_catalog", "status": "ok", "args": map[string]any{"id": "saved_query:" + qualified}, "evidence": map[string]any{"saved_query:" + qualified: map[string]any{"evidence_json": string(meta), "graphql_query": query}}}
}

func TestSavedQueryRequiresPriorVerifiedDefinitionAndNamespace(t *testing.T) {
	q := `query totals { invoices { sum_amount_cents } }`
	detail := savedReceipt("totals", "billing", q)
	action := map[string]any{"tool": "execute_saved_query", "status": "ok", "args": map[string]any{"name": "totals", "namespace": "billing", "query": `{ invoices { sum_id } }`}}
	r := NewResolver(nil)
	if r.ExecutionQuery(action) != "" {
		t.Fatal("query argument/name alone gave saved-query credit")
	}
	r.ObserveCatalog(detail)
	if got := r.ExecutionQuery(action); got != q {
		t.Fatalf("resolved %q, want %q", got, q)
	}
	wrong := map[string]any{"tool": "execute_saved_query", "args": map[string]any{"name": "totals", "namespace": "other"}}
	if r.ExecutionQuery(wrong) != "" {
		t.Fatal("cross-namespace saved query resolved")
	}
	fields := object(object(detail["evidence"])["saved_query:billing.totals"])
	fields["graphql_query"] = q + " # changed"
	r.ObserveCatalog(detail)
	if r.ExecutionQuery(action) != "" {
		t.Fatal("changed definition retained stale credit")
	}
}

func TestPreparedDefinitionStillRequiresDetailReceipt(t *testing.T) {
	q := `{ invoices { sum_amount_cents } }`
	detail := savedReceipt("totals", "", q)
	fields := object(object(detail["evidence"])["saved_query:totals"])
	card := map[string]any{"kind": "saved_query", "graphql_query": q, "evidence_json": fields["evidence_json"]}
	delete(fields, "graphql_query")
	trace := map[string]any{"input": map[string]any{"context": map[string]any{"_graphjin_prepared": map[string]any{"catalog": []any{map[string]any{"cards": []any{card}}}}}}}
	r := NewResolver(trace)
	action := map[string]any{"tool": "execute_saved_query", "args": map[string]any{"name": "totals"}}
	if r.ExecutionQuery(action) != "" {
		t.Fatal("prefetch alone gave execution credit")
	}
	detail["status"] = "error"
	r.ObserveCatalog(detail)
	if r.ExecutionQuery(action) != "" {
		t.Fatal("failed catalog lookup gave credit")
	}
	detail["status"] = "ok"
	r.ObserveCatalog(detail)
	if r.ExecutionQuery(action) != q {
		t.Fatal("matching prepared source did not resolve")
	}
	if Successful(map[string]any{"status": "ok", "summary": map[string]any{"error_count": float64(1)}}) {
		t.Fatal("structured error treated as successful execution")
	}
}

func TestSavedDefinitionHashFormats(t *testing.T) {
	q := `{ invoices { sum_amount_cents } }`
	for _, original := range []string{q, q + "\n", q + "\r\n"} {
		sum := sha256.Sum256([]byte(original))
		if !verified(q, hex.EncodeToString(sum[:])) {
			t.Fatal("published source hash did not verify")
		}
		if verified(q+" # changed", hex.EncodeToString(sum[:])) {
			t.Fatal("changed source verified")
		}
	}
	encoded, _ := json.Marshal(q)
	sum := sha256.Sum256(encoded)
	meta := map[string]any{"query_hash": hex.EncodeToString(sum[:]), "source_hash": "original-file-revision"}
	if !verifiedDefinition(q, meta) {
		t.Fatal("normalized query hash failed")
	}
	meta["query_hash"] = "invalid"
	if verifiedDefinition(q, meta) {
		t.Fatal("invalid declared query hash fell back to a different format")
	}
}
