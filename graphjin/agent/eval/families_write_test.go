package eval

import (
	"strings"
	"testing"

	gjagent "github.com/dosco/graphjin/agent/v3"
)

func writeSnapshot(readOnly bool, actions ...string) CatalogSnapshot {
	if len(actions) == 0 {
		actions = []string{gjagent.CapabilityActionDataUpdate}
	}
	return CatalogSnapshot{
		Status: AgentStatus{ReadOnly: readOnly, AllowedActions: actions},
		Rows: []CatalogRow{
			{
				ID: "table:invoices", Kind: "table", TableName: "invoices",
				DetailsJSON: `[{"ColumnName":"id","Type":"integer","PrimaryKey":true},
					{"ColumnName":"amount_cents","Type":"integer"},
					{"ColumnName":"due_at","Type":"datetime"},
					{"ColumnName":"last_attempt_at","Type":"datetime"},
					{"ColumnName":"status","Type":"text"}]`,
			},
			columnRow("invoices", "status", []any{
				`where: { status: { eq: "failed" } }`,
				"status values: failed, paid",
			}),
		},
	}
}

func TestRowUpdateAsksForOneRowAndChecksTheRest(t *testing.T) {
	tasks := generateRowUpdateCandidates(writeSnapshot(false), 23)
	if len(tasks) == 0 {
		t.Fatal("expected write candidates")
	}
	task := tasks[0]
	if task.Category != CategoryAction || task.Difficulty != DifficultyT3 {
		t.Fatalf("unexpected classification %s/%s", task.Category, task.Difficulty)
	}
	if task.Mutation == nil {
		t.Fatal("a write task must carry a mutation spec")
	}
	if task.Mutation.ExpectedValue != "1" {
		t.Fatalf("post-state must expect exactly the one changed row, got %q", task.Mutation.ExpectedValue)
	}
	// The row is pinned by an anchor resolved at grading time rather than by a
	// literal id, which is what lets the same task shape work on any catalog.
	if !strings.Contains(task.Mutation.PostState.AnchorQuery, "order_by") {
		t.Fatalf("post-state must pin a row by ordering, got %q", task.Mutation.PostState.AnchorQuery)
	}
	if len(task.Mutation.Collateral) == 0 {
		t.Fatal("a write task must check that nothing else moved")
	}
	// The collateral read must exclude the row being changed and project every
	// column, or a neighbour's field could move unnoticed.
	collateral := task.Mutation.Collateral[0].Query
	if !strings.Contains(collateral, "neq") {
		t.Fatalf("collateral must exclude the graded row: %s", collateral)
	}
	for _, column := range []string{"id", "amount_cents", "due_at", "status"} {
		if !strings.Contains(collateral, column) {
			t.Fatalf("collateral does not project %s: %s", column, collateral)
		}
	}
	if !strings.Contains(task.Prompt, "Do not change any other record") {
		t.Fatalf("the prompt must state the constraint being graded: %q", task.Prompt)
	}
}

// The prompt has to name the ordering it means, or "the most recent record" has
// as many answers as the table has date columns and a correct agent can be
// graded wrong for choosing a different one.
func TestRowUpdatePromptNamesTheOrderingItMeans(t *testing.T) {
	task := generateRowUpdateCandidates(writeSnapshot(false), 23)[0]
	if !strings.Contains(task.Prompt, "due at") {
		t.Fatalf("prompt must name the ordering column: %q", task.Prompt)
	}
	if !strings.Contains(task.Mutation.PostState.AnchorQuery, "due_at") {
		t.Fatalf("the oracle must order by the column the prompt names: %q", task.Mutation.PostState.AnchorQuery)
	}
}

// A system-stamped column cannot pin a row: the graded write may move it, and
// the anchor would then select a different row afterwards than it did before,
// so the post-state would be read against the wrong row.
func TestRowUpdateNeverAnchorsOnAStampedColumn(t *testing.T) {
	for _, task := range generateRowUpdateCandidates(writeSnapshot(false), 23) {
		if strings.Contains(task.Mutation.PostState.AnchorQuery, "last_attempt_at") {
			t.Fatalf("anchored on a stamped column: %q", task.Mutation.PostState.AnchorQuery)
		}
	}
}

func TestRowUpdateDeclinesWithoutWritePermission(t *testing.T) {
	if tasks := generateRowUpdateCandidates(writeSnapshot(true), 23); len(tasks) != 0 {
		t.Fatalf("a read-only caller must generate no write tasks, got %d", len(tasks))
	}
	readOnlyActions := generateRowUpdateCandidates(writeSnapshot(false, "data.read"), 23)
	if len(readOnlyActions) != 0 {
		t.Fatalf("a caller without update permission must generate no write tasks, got %d", len(readOnlyActions))
	}
}

// Values must come from the column's published closed set. A filter on a state
// the business never uses would be satisfiable only by inventing one.
func TestRowUpdateOnlyRequestsObservedValues(t *testing.T) {
	allowed := map[string]bool{"failed": true, "paid": true}
	for _, task := range generateRowUpdateCandidates(writeSnapshot(false), 23) {
		query := task.Mutation.PostState.Query
		parts := strings.Split(query, `status: {eq: "`)
		if len(parts) < 2 {
			t.Fatalf("post-state does not constrain the changed column: %s", query)
		}
		value := parts[1][:strings.Index(parts[1], `"`)]
		if !allowed[value] {
			t.Fatalf("write task requests unobserved value %q", value)
		}
	}
}

func TestRowUpdateIsDeterministic(t *testing.T) {
	first := generateRowUpdateCandidates(writeSnapshot(false), 23)
	for i := 0; i < 5; i++ {
		again := generateRowUpdateCandidates(writeSnapshot(false), 23)
		if len(first) != len(again) {
			t.Fatalf("emitted %d then %d tasks", len(first), len(again))
		}
		for j := range first {
			if first[j].Prompt != again[j].Prompt {
				t.Fatalf("task %d differs between runs", j)
			}
		}
	}
}
