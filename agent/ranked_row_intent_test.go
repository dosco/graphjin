package agent

import "testing"

// The frozen suite's own method gate accepts "order_by: {col: desc} ... limit: 1"
// as a correct way to read an extreme. The guard has to agree, or it refuses to
// let a correct run finalize: 12 of 15 recorded runaways carried
// database_computation_required after exactly this read.
func TestQueryUsesDatabaseRankingMatchesSnakeCaseColumns(t *testing.T) {
	for _, tc := range []struct {
		name        string
		query       string
		instruction string
		want        bool
	}{
		{
			name:        "snake_case column named with a dot in the question",
			query:       `{ invoices(order_by: { due_at: desc }, limit: 1) { due_at } }`,
			instruction: "What is the latest date recorded in invoices.due_at?",
			want:        true,
		},
		{
			name:        "snake_case column named in prose",
			query:       `{ accounts(order_by: { last_active_at: desc }, limit: 1) { last_active_at } }`,
			instruction: "What is the latest date recorded in accounts.last_active_at?",
			want:        true,
		},
		{
			name:        "single word column still matches",
			query:       `{ products(order_by: { price: desc }, limit: 1) { price } }`,
			instruction: "Which product has the highest price?",
			want:        true,
		},
		{
			name:        "ranking a column the question never mentions does not count",
			query:       `{ invoices(order_by: { created_at: desc }, limit: 1) { id } }`,
			instruction: "What is the latest date recorded in invoices.due_at?",
			want:        false,
		},
		{
			name:        "no limit is not a ranked read",
			query:       `{ invoices(order_by: { due_at: desc }) { due_at } }`,
			instruction: "What is the latest date recorded in invoices.due_at?",
			want:        false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := queryUsesDatabaseRanking(tc.query, tc.instruction); got != tc.want {
				t.Fatalf("queryUsesDatabaseRanking = %v, want %v", got, tc.want)
			}
		})
	}
}

// The guard must stop demanding an aggregate once that read has happened,
// which is what lets the run finalize instead of re-querying to the step cap.
func TestPendingDatabaseComputationClearedByRankedRead(t *testing.T) {
	s := newDiscoveryState("What is the latest date recorded in invoices.due_at?")
	s.executions = []map[string]any{{
		"tool":     toolExecuteGraphQL,
		"query":    `{ invoices(order_by: { due_at: desc }, limit: 1) { due_at } }`,
		"has_data": true,
	}}
	if got := s.pendingDatabaseComputation(); got != "" {
		t.Fatalf("a correct ranked read must satisfy the guard, got: %s", got)
	}
}
