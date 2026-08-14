package core

import (
	"context"
	"encoding/json"
	"testing"
)

// managedAggregateTestHandler records the request core sends and serves fixed
// rows projected through root.Fields, the way real control-plane handlers do.
type managedAggregateTestHandler struct {
	rows     []map[string]any
	captured *ManagedQueryRequest
}

func (h managedAggregateTestHandler) ManagedQueryTables() []ManagedTable {
	return []ManagedTable{{
		Name: "gj_managed_item",
		Columns: []ManagedColumn{
			{Name: "id", Type: "text", PrimaryKey: true},
			{Name: "rank", Type: "integer"},
			{Name: "title", Type: "text"},
		},
	}}
}

func (h managedAggregateTestHandler) ExecuteManagedQuery(
	_ context.Context,
	req ManagedQueryRequest,
) (json.RawMessage, error) {
	if h.captured != nil {
		*h.captured = req
	}
	out := make(map[string]any, len(req.Roots))
	for _, root := range req.Roots {
		rows := make([]map[string]any, 0, len(h.rows))
		for _, source := range h.rows {
			row := make(map[string]any, len(root.Fields))
			for _, field := range root.Fields {
				row[field.Name] = source[field.Column]
			}
			rows = append(rows, row)
		}
		out[root.FieldName] = rows
	}
	return json.Marshal(out)
}

// TestManagedRootCountAggregateFoldsRows pins the two managed-root behaviors
// the benchmark watch oracles depend on: a count_<col> selection folds the
// filtered rows into one aggregate row (instead of projecting empty objects),
// and the `is_null: false` literal survives translation (instead of inverting
// into `is_null: true`).
func TestManagedRootCountAggregateFoldsRows(t *testing.T) {
	captured := &ManagedQueryRequest{}
	handler := managedAggregateTestHandler{rows: []map[string]any{
		{"id": "a", "rank": 1, "title": "one"},
		{"id": "b", "rank": 2, "title": nil},
		{"id": "c", "rank": 3, "title": ""},
	}, captured: captured}
	gj := newManagedCursorTestGraphJin(t, handler)
	defer gj.Close()

	res, err := gj.GraphQL(context.Background(),
		`query { gj_managed_item(where: { title: { is_null: false } }) { count_id } }`, nil, nil)
	if err != nil || len(res.Errors) != 0 {
		t.Fatalf("aggregate query: err=%v errors=%+v", err, res.Errors)
	}

	if len(captured.Roots) != 1 {
		t.Fatalf("expected one root, got %+v", captured.Roots)
	}
	root := captured.Roots[0]
	condition, _ := root.Where["title"].(map[string]any)
	if wantNull, ok := condition["is_null"].(bool); !ok || wantNull {
		t.Fatalf("is_null: false must survive translation, got where=%v", root.Where)
	}
	if root.Limit != 0 {
		t.Fatalf("aggregate root must not inherit the row limit, got %d", root.Limit)
	}
	if len(root.Fields) != 1 || root.Fields[0].Column != "id" {
		t.Fatalf("aggregate root should fetch the counted column hidden, got %+v", root.Fields)
	}

	var out struct {
		Rows []map[string]any `json:"gj_managed_item"`
	}
	if err := json.Unmarshal(res.Data, &out); err != nil {
		t.Fatal(err)
	}
	// The test handler ignores Where (filtering is the serv layer's job), so
	// all three rows count: every id is populated.
	if len(out.Rows) != 1 {
		t.Fatalf("count selection must fold to one row, got %s", res.Data)
	}
	if count, ok := out.Rows[0]["count_id"].(float64); !ok || count != 3 {
		t.Fatalf("count_id = %v, want 3", out.Rows[0]["count_id"])
	}

	// A count over a nullable column skips null and empty values.
	res, err = gj.GraphQL(context.Background(),
		`query { gj_managed_item { count_title } }`, nil, nil)
	if err != nil || len(res.Errors) != 0 {
		t.Fatalf("nullable count: err=%v errors=%+v", err, res.Errors)
	}
	if err := json.Unmarshal(res.Data, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Rows) != 1 || out.Rows[0]["count_title"] != float64(1) {
		t.Fatalf("count_title should skip null/empty, got %s", res.Data)
	}

	// Plain selections keep their historical row shape.
	res, err = gj.GraphQL(context.Background(),
		`query { gj_managed_item { id title } }`, nil, nil)
	if err != nil || len(res.Errors) != 0 {
		t.Fatalf("plain query: err=%v errors=%+v", err, res.Errors)
	}
	if err := json.Unmarshal(res.Data, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Rows) != 3 {
		t.Fatalf("plain selection regressed: %s", res.Data)
	}
}
