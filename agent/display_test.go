package agent

import (
	"context"
	"strings"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
	"github.com/dosco/graphjin/core/v3"
)

// The columns ride on the execution result for display hints but are never
// serialized, so the model's view of a tool result does not change.
func TestDisplayColumnsNeverReachTheModel(t *testing.T) {
	out := executeResult{
		Data:    map[string]any{"accounts": []any{map[string]any{"id": 1}}},
		Columns: []core.ResultColumn{{Path: "accounts.id", Root: "accounts", Name: "id", Role: "key"}},
	}
	if view := dumpAXValue(normalizeValue(out)); strings.Contains(view, "role") || strings.Contains(view, "columns") {
		t.Fatalf("the model would see display columns: %s", view)
	}
	if got := executionColumns(out); len(got) != 1 {
		t.Fatalf("columns lost: %v", got)
	}
}

func TestDisplayKindFollowsTheColumnRoles(t *testing.T) {
	col := func(name, role string) DisplayColumn { return DisplayColumn{Name: name, Role: role} }
	for _, test := range []struct {
		name    string
		columns []DisplayColumn
		rows    int
		want    string
	}{
		{"one total", []DisplayColumn{col("count_id", "measure"), col("sum_mrr_cents", "measure")}, 1, "value"},
		{"label against a measure", []DisplayColumn{col("id", "key"), col("name", "text"), col("mrr_cents", "measure")}, 3, "bar"},
		{"measure over time", []DisplayColumn{col("day", "time"), col("count_id", "measure")}, 7, "line"},
		{"two labels", []DisplayColumn{col("name", "text"), col("status", "text"), col("mrr_cents", "measure")}, 3, "table"},
		{"too many bars", []DisplayColumn{col("name", "text"), col("mrr_cents", "measure")}, 40, "table"},
	} {
		if got := displayKind(test.columns, test.rows); got != test.want {
			t.Errorf("%s: kind = %s, want %s", test.name, got, test.want)
		}
	}
}

func TestDisplayHintsCarryTheRowsTheyDescribe(t *testing.T) {
	columns := []core.ResultColumn{
		{Path: "accounts.name", Root: "accounts", Name: "name", Type: "text", Role: "text"},
		{Path: "accounts.mrr_cents", Root: "accounts", Name: "mrr_cents", Type: "integer", Role: "measure"},
		{Path: "accounts.plan.name", Root: "accounts", Name: "name", Type: "text", Role: "text"},
	}
	data := map[string]any{"accounts": []any{map[string]any{"name": "Meridian", "mrr_cents": 480000}, map[string]any{"name": "Solvex", "mrr_cents": 610000}}}
	hints := buildDisplayHints(columns, data)
	if len(hints) != 1 || hints[0].Root != "accounts" || hints[0].RowCount != 2 || len(hints[0].Rows) != 2 {
		t.Fatalf("hints = %+v", hints)
	}
	if names := []string{hints[0].Columns[0].Name, hints[0].Columns[2].Name}; names[0] != "name" || names[1] != "plan.name" {
		t.Fatalf("column names = %v, want paths inside a row", names)
	}
}

func TestLeadCarriesItsQuestionsDisplay(t *testing.T) {
	question := "How many open tickets are there?"
	client := &scriptedLeadClient{responses: []ax.Value{
		leadToolCall("c1", question),
		leadReply(StatusAnswered, "There are 2 open tickets."),
	}}
	hint := DisplayHint{Root: "tickets", Kind: "value", Columns: []DisplayColumn{{Name: "count_id", Role: "measure"}}, Rows: []any{map[string]any{"count_id": 2}}, RowCount: 1}
	lead := newTestLead(t, Config{}, client, func(context.Context, DelegateRequest) (Response, error) {
		resp := leadTestAnswers[question]
		resp.Display = []DisplayHint{hint}
		return resp, nil
	})
	resp, err := lead.Run(context.Background(), Request{Instruction: "open tickets?"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Display) != 1 || resp.Display[0].Question != question || resp.Display[0].Kind != "value" {
		t.Fatalf("display = %+v", resp.Display)
	}
}

// SQLite keeps dates in TEXT columns; the values, not the declared type,
// make them a time.
func TestDisplayHintsReadDatesStoredAsText(t *testing.T) {
	columns := []core.ResultColumn{
		{Path: "invoices.due_at", Root: "invoices", Name: "due_at", Type: "text", Role: "text"},
		{Path: "invoices.amount_cents", Root: "invoices", Name: "amount_cents", Type: "integer", Role: "measure"},
		{Path: "invoices.status", Root: "invoices", Name: "status", Type: "text", Role: "text"},
	}
	data := map[string]any{"invoices": []any{
		map[string]any{"due_at": "2026-11-04", "amount_cents": 30000, "status": "paid"},
		map[string]any{"due_at": "2026-11-03T09:30:00Z", "amount_cents": 140000, "status": "paid"},
		map[string]any{"due_at": nil, "amount_cents": 1, "status": "paid"},
	}}
	hints := buildDisplayHints(columns, data)
	if len(hints) != 1 || hints[0].Columns[0].Role != "time" || hints[0].Columns[2].Role != "text" {
		t.Fatalf("columns = %+v", hints[0].Columns)
	}
	if hints[0].Kind != "table" {
		t.Fatalf("a time, a measure and a label is a table, got %s", hints[0].Kind)
	}
	if hints := buildDisplayHints(columns[:2], data); hints[0].Kind != "line" {
		t.Fatalf("dates against a measure = %s, want line", hints[0].Kind)
	}
	data["invoices"] = append(data["invoices"].([]any), map[string]any{"due_at": "soon", "amount_cents": 2})
	if hints := buildDisplayHints(columns[:2], data); hints[0].Columns[0].Role != "text" {
		t.Fatalf("one non-date value keeps the column text: %+v", hints[0].Columns)
	}
}
