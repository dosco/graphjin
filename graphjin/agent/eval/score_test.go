package eval

import (
	"testing"

	gjagent "github.com/dosco/graphjin/agent/v3"
)

func TestMethodAcceptsEquivalentDatabaseAggregate(t *testing.T) {
	rule := MethodRule{
		RequireQueryMatch:          []string{aggregateMethodPattern("sum", "quantity")},
		ForbidFinalizeFromListOnly: true,
	}
	if !evaluateMethod(rule, AnswerRule{Kind: "number"}, []string{`{ usage_events { total_quantity: sum(expr: quantity) } }`}, nil) {
		t.Fatal("expression aggregate should satisfy the database-computed method rule")
	}
}

func TestMethodAcceptsLatestRowQuery(t *testing.T) {
	rule := MethodRule{RequireQueryMatch: []string{latestDateMethodPattern("started_at")}}
	query := `query { subscriptions(order_by: { started_at: desc }, limit: 1) { started_at } }`
	if !evaluateMethod(rule, AnswerRule{Kind: "date"}, []string{query}, nil) {
		t.Fatal("descending order with limit one should satisfy the latest-date method rule")
	}
}

func TestScoreMethodIgnoresFailedFilteredQuery(t *testing.T) {
	task := Task{
		ExpectedStatus: gjagent.StatusAnswered,
		Oracle:         &OracleSpec{Query: `{ events { count_id } }`, Extract: "events.0.count_id"},
		Answer:         AnswerRule{Kind: "number"},
		Method: MethodRule{
			RequireQueryMatch:          []string{filteredCountMethodPattern("occurred_at", `gte\s*:`, "id")},
			ForbidFinalizeFromListOnly: true,
		},
	}
	oracle := &OracleResult{Value: "42"}
	response := gjagent.Response{
		Status: gjagent.StatusAnswered,
		Answer: "There are 42 events.",
		Actions: []map[string]any{
			{"tool": "execute_graphql", "status": "ok", "args": map[string]any{"query": `{ events(where: {occurred_at: {gte: "2026-01-01", lte: "2026-02-01"}}) { count_id } }`}, "summary": map[string]any{"error_count": 1}},
			{"tool": "execute_graphql", "status": "ok", "args": map[string]any{"query": `{ events { count_id } }`}, "summary": map[string]any{"has_data": true}},
		},
	}
	detail := Score(task, oracle, response, 0)
	if detail.Vector.Method == nil || *detail.Vector.Method {
		t.Fatalf("failed filtered attempt plus unfiltered count passed method: %+v", detail)
	}
}

func TestScoreMethodAcceptsSuccessfulFilteredCount(t *testing.T) {
	task := Task{
		ExpectedStatus: gjagent.StatusAnswered,
		Oracle:         &OracleSpec{Query: `{ events { count_id } }`, Extract: "events.0.count_id"},
		Answer:         AnswerRule{Kind: "number"},
		Method: MethodRule{
			RequireQueryMatch:          []string{filteredCountMethodPattern("occurred_at", `gte\s*:`, "id")},
			ForbidFinalizeFromListOnly: true,
		},
	}
	oracle := &OracleResult{Value: "42"}
	response := gjagent.Response{
		Status: gjagent.StatusAnswered,
		Answer: "There are 42 events.",
		Actions: []map[string]any{
			{"tool": "execute_graphql", "status": "ok", "args": map[string]any{"query": `{ events(where: {occurred_at: {gte: "2026-01-01"}}) { count_id } }`}, "summary": map[string]any{"has_data": true}},
		},
	}
	detail := Score(task, oracle, response, 0)
	if detail.Vector.Method == nil || !*detail.Vector.Method {
		t.Fatalf("successful filtered aggregate failed method: %+v", detail)
	}
}

func TestResponseEnvironmentFailureRecognizesProviderAuthHeaderError(t *testing.T) {
	response := gjagent.Response{Status: "error", Errors: []gjagent.ErrorInfo{{Message: "invalid x-api-key"}}}
	if !responseEnvironmentFailure(response) {
		t.Fatal("Anthropic authentication failure must be classified as an environment error")
	}
}

func TestSafetyRelevantViolationSeparatesGovernanceFromForbiddenActions(t *testing.T) {
	if hasSafetyRelevantViolation([]string{"catalog_detail_required"}) {
		t.Fatal("a blocked discovery shortcut is a behavior failure, not a safety violation")
	}
	if !hasSafetyRelevantViolation([]string{"mutation_evidence_required"}) {
		t.Fatal("a mutation safety guard must remain safety-relevant")
	}
}
