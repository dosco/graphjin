package agent

import (
	"strings"
	"testing"
)

func TestAggregateRewriteOf(t *testing.T) {
	numeric := map[string]map[string]bool{
		"accounts": {"id": true, "mrr_cents": true},
		"invoices": {"id": true, "account_id": true, "amount_cents": true},
	}
	for _, tc := range []struct {
		name, query, op, want string
	}{
		{"filtered sum keeps the filter", `query { accounts(where: {name: {in: ["A", "B"]}}) { id name mrr_cents } }`, "sum",
			`query { accounts(where: {name: {in: ["A", "B"]}}) { sum_mrr_cents } }`},
		{"average", `{ accounts(where: {plan: {eq: "enterprise"}}) { name mrr_cents } }`, "avg",
			`query { accounts(where: {plan: {eq: "enterprise"}}) { avg_mrr_cents } }`},
		{"count needs no numeric column", `{ invoices(where: {status: {eq: "failed"}}) { id status } }`, "count",
			`query { invoices(where: {status: {eq: "failed"}}) { count_id } }`},
		{"alias dropped, foreign keys skipped", `{ late: invoices(where: {status: {eq: "failed"}}) { id account_id amount_cents } }`, "sum",
			`query { invoices(where: {status: {eq: "failed"}}) { sum_amount_cents } }`},
		{"no arguments", `{ accounts { mrr_cents } }`, "sum", `query { accounts { sum_mrr_cents } }`},
		{"nested selection is ignored", `{ accounts { mrr_cents invoices { amount_cents } } }`, "sum", `query { accounts { sum_mrr_cents } }`},
		// Nothing safe to propose: text columns only, an unknown table, a root
		// that cannot be rewritten, or a query the scanner cannot read.
		{"text only", `{ accounts { id name } }`, "sum", ""},
		{"unknown table", `{ tickets { severity_score } }`, "sum", ""},
		{"one root not rewritable", `{ accounts { mrr_cents } tickets { subject } }`, "sum", ""},
		{"mutation", `mutation { accounts(insert: {id: 1}) { mrr_cents } }`, "sum", ""},
		{"limited read is not a plain filter", `{ accounts(order_by: {mrr_cents: desc}, limit: 3) { name mrr_cents } }`, "sum", ""},
	} {
		if got := aggregateRewriteOf(tc.query, tc.op, numeric); got != tc.want {
			t.Errorf("%s:\n got %q\nwant %q", tc.name, got, tc.want)
		}
		if tc.want != "" {
			if _, ok := scanRootFields(tc.want); !ok {
				t.Errorf("%s: the rewrite must itself be a query the scanner reads", tc.name)
			}
		}
	}
}

func TestAggregateOperation(t *testing.T) {
	for instruction, want := range map[string]string{
		"What is the combined monthly recurring revenue of A and B?": "sum",
		"How much do these add up to?":                               "sum",
		"What is the average invoice amount?":                        "avg",
		"How many failed invoices are there?":                        "count",
		"Which account has the most revenue?":                        "",
		"Show me the churn-risk accounts.":                           "",
	} {
		if got := aggregateOperation(instruction); got != want {
			t.Errorf("%q: op = %q, want %q", instruction, got, want)
		}
	}
	if aggregate, _ := databaseComputationIntent("What is their combined MRR?"); !aggregate {
		t.Fatal("a combined total must require a database aggregate")
	}
	// A total over a set the lead already ranked and listed is a sum, not a
	// fresh ranking: the explicit set satisfies it without order_by.
	listed := "What is the combined MRR for the top three accounts: Solvex Dynamics (ID 2), Harborlight Systems (ID 3), and Meridian Robotics (ID 1)?"
	if databaseOrderedRowIntent(listed) {
		t.Fatal("an explicit total is not a request for ranked rows")
	}
	if !aggregatesExplicitSet(`query { accounts(where: { id: { in: [1, 2, 3] } }) { sum_mrr_cents } }`) ||
		aggregatesExplicitSet(`query { accounts { sum_mrr_cents } }`) ||
		aggregatesExplicitSet(`query { accounts(where: { id: { in: [1, 2] } }) { id mrr_cents } }`) {
		t.Fatal("only an aggregate over a listed set counts")
	}
}

// Running the rewrite the refusal quotes must clear the refusal, or the
// worker is handed a query that only earns it another refusal.
func TestQuotedRewriteClearsTheComputationGuard(t *testing.T) {
	for instruction, rows := range map[string]string{
		"What is the combined MRR for the top three accounts: Solvex Dynamics (ID 2), Harborlight Systems (ID 3), and Meridian Robotics (ID 1)?": `query { accounts(where: { id: { in: [1, 2, 3] } }) { id name mrr_cents } }`,
		"What is the combined monthly recurring revenue of our churn-risk accounts?":                                                             `query { accounts(where: {status: {eq: "churn_risk"}}) { id name mrr_cents } }`,
	} {
		state := newDiscoveryState(instruction)
		state.seedResult = map[string]any{"cards": []any{
			map[string]any{"id": "table:app:main.accounts", "kind": "table", "table_name": "accounts"},
			map[string]any{"id": "column:app:main.accounts.mrr_cents", "kind": "column", "table_name": "accounts", "column_name": "mrr_cents", "summary": "integer, not null"},
			map[string]any{"id": "column:app:main.accounts.name", "kind": "column", "table_name": "accounts", "column_name": "name", "summary": "text"},
		}}
		state.executions = append(state.executions, map[string]any{"tool": toolExecuteGraphQL, "query": rows, "has_data": true, "error_count": 0})
		refusal := state.pendingDatabaseComputation()
		_, rewrite, ok := strings.Cut(refusal, "run exactly: ")
		if !ok {
			t.Fatalf("%q: refusal carries no rewrite: %q", instruction, refusal)
		}
		state.executions = append(state.executions, map[string]any{"tool": toolExecuteGraphQL, "query": rewrite, "has_data": true, "error_count": 0})
		if still := state.pendingDatabaseComputation(); still != "" {
			t.Fatalf("%q: running the quoted %q left %q", instruction, rewrite, still)
		}
	}
}
