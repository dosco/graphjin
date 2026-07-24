package core

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type managedCursorTestHandler struct {
	rows       []map[string]any
	omitHidden bool
}

func (h managedCursorTestHandler) ManagedQueryTables() []ManagedTable {
	return []ManagedTable{{
		Name: "gj_managed_item",
		Columns: []ManagedColumn{
			{Name: "id", Type: "text", PrimaryKey: true},
			{Name: "rank", Type: "integer"},
			{Name: "title", Type: "text"},
		},
	}}
}

func (h managedCursorTestHandler) ExecuteManagedQuery(
	_ context.Context,
	req ManagedQueryRequest,
) (json.RawMessage, error) {
	out := make(map[string]any, len(req.Roots))
	for _, root := range req.Roots {
		rows := make([]map[string]any, 0, len(h.rows))
		for _, source := range h.rows {
			row := make(map[string]any, len(root.Fields))
			for _, field := range root.Fields {
				if h.omitHidden && len(field.Name) >= len("__gj_cursor_") &&
					field.Name[:len("__gj_cursor_")] == "__gj_cursor_" {
					continue
				}
				row[field.Name] = source[field.Column]
			}
			rows = append(rows, row)
		}
		out[root.FieldName] = rows
	}
	return json.Marshal(out)
}

func newManagedCursorTestGraphJin(t *testing.T, handler ManagedQueryHandler) *GraphJin {
	t.Helper()
	db, err := NewNanoDB(NanoSnapshot{
		Schema: "main",
		Tables: []NanoTable{{
			Name: "gj_managed_item",
			Columns: []NanoColumn{
				{Name: "id", Type: "text", PrimaryKey: true},
				{Name: "rank", Type: "integer"},
				{Name: "title", Type: "text"},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	conf := &Config{
		DBType:           "nanodb",
		DisableAllowList: true,
		SubsPollDuration: minPollDuration,
		Databases:        map[string]DatabaseConfig{DefaultDBName: {Type: "nanodb"}},
	}
	gj, err := NewGraphJin(conf, nil,
		OptionSetNanoDatabases(map[string]*NanoDB{DefaultDBName: db}),
		OptionSetManagedQueryHandler(DefaultDBName, handler))
	if err != nil {
		t.Fatal(err)
	}
	return gj
}

func TestManagedQueryCursorPaginationIsCoreAuthoritative(t *testing.T) {
	handler := managedCursorTestHandler{rows: []map[string]any{
		{"id": "d", "rank": 4, "title": "four"},
		{"id": "b", "rank": 2, "title": "two"},
		{"id": "a", "rank": 1, "title": "one"},
		{"id": "c", "rank": 3, "title": "three"},
	}}
	gj := newManagedCursorTestGraphJin(t, handler)
	defer gj.Close()

	query := `query page($first: Int!, $gj_managed_item_cursor: Cursor) {
		gj_managed_item(first: $first, after: $gj_managed_item_cursor, order_by: { rank: asc }) {
			id
			rank
		}
		gj_managed_item_cursor
	}`
	first, err := gj.GraphQL(context.Background(), query,
		json.RawMessage(`{"first":2,"gj_managed_item_cursor":null}`), nil)
	if err != nil || len(first.Errors) != 0 {
		t.Fatalf("first page: err=%v errors=%+v", err, first.Errors)
	}
	var page1 struct {
		Rows []map[string]any `json:"gj_managed_item"`
		Cur  string           `json:"gj_managed_item_cursor"`
	}
	if err := json.Unmarshal(first.Data, &page1); err != nil {
		t.Fatal(err)
	}
	if len(page1.Rows) != 2 || page1.Rows[0]["id"] != "a" || page1.Rows[1]["id"] != "b" || page1.Cur == "" {
		t.Fatalf("unexpected first page: %s", first.Data)
	}

	vars, err := json.Marshal(map[string]any{
		"first":                  2,
		"gj_managed_item_cursor": page1.Cur,
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := gj.GraphQL(context.Background(), query, vars, nil)
	if err != nil || len(second.Errors) != 0 {
		t.Fatalf("second page: err=%v errors=%+v", err, second.Errors)
	}
	var page2 struct {
		Rows []map[string]any `json:"gj_managed_item"`
	}
	if err := json.Unmarshal(second.Data, &page2); err != nil {
		t.Fatal(err)
	}
	if len(page2.Rows) != 2 || page2.Rows[0]["id"] != "c" || page2.Rows[1]["id"] != "d" {
		t.Fatalf("unexpected second page: %s", second.Data)
	}
}

func TestManagedQueryCursorAppliesFilterAndVariableOffsetInCore(t *testing.T) {
	handler := managedCursorTestHandler{rows: []map[string]any{
		{"id": "d", "rank": 4, "title": "four"},
		{"id": "b", "rank": 2, "title": "two"},
		{"id": "a", "rank": 1, "title": "one"},
		{"id": "c", "rank": 3, "title": "three"},
	}}
	gj := newManagedCursorTestGraphJin(t, handler)
	defer gj.Close()

	result, err := gj.GraphQL(context.Background(), `query page(
		$minimum: Int!
		$first: Int!
		$offset: Int!
		$gj_managed_item_cursor: Cursor
	) {
		gj_managed_item(
			first: $first
			after: $gj_managed_item_cursor
			offset: $offset
			where: { rank: { gt: $minimum } }
			order_by: { id: asc }
		) {
			id
		}
		gj_managed_item_cursor
	}`, json.RawMessage(`{
		"minimum": 2,
		"first": 1,
		"offset": 1,
		"gj_managed_item_cursor": null
	}`), nil)
	if err != nil || len(result.Errors) != 0 {
		t.Fatalf("managed filtered page: err=%v errors=%+v", err, result.Errors)
	}
	var page struct {
		Rows []map[string]any `json:"gj_managed_item"`
		Cur  string           `json:"gj_managed_item_cursor"`
	}
	if err := json.Unmarshal(result.Data, &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Rows) != 1 || page.Rows[0]["id"] != "d" || page.Cur == "" {
		t.Fatalf("managed core-authoritative filter/offset page = %s", result.Data)
	}
}

func TestManagedQueryCursorRequiresHiddenOrderColumns(t *testing.T) {
	gj := newManagedCursorTestGraphJin(t, managedCursorTestHandler{
		rows:       []map[string]any{{"id": "a", "rank": 1, "title": "one"}},
		omitHidden: true,
	})
	defer gj.Close()

	_, err := gj.GraphQL(context.Background(), `query {
		gj_managed_item(first: 1, order_by: { rank: asc }) { id }
		gj_managed_item_cursor
	}`, nil, nil)
	if err == nil {
		t.Fatal("expected missing hidden cursor order column error")
	}
}

func TestManagedQueryCursorRejectsBackwardPaging(t *testing.T) {
	gj := newManagedCursorTestGraphJin(t, managedCursorTestHandler{
		rows: []map[string]any{{"id": "a", "rank": 1, "title": "one"}},
	})
	defer gj.Close()

	for _, query := range []string{
		`query { gj_managed_item(last: 1, order_by: { rank: asc }) { id } gj_managed_item_cursor }`,
		`query page($gj_managed_item_cursor: Cursor) {
			gj_managed_item(last: 1, before: $gj_managed_item_cursor, order_by: { rank: asc }) { id }
			gj_managed_item_cursor
		}`,
	} {
		_, err := gj.GraphQL(context.Background(), query,
			json.RawMessage(`{"gj_managed_item_cursor":null}`), nil)
		if err == nil || !strings.Contains(err.Error(), "does not support last/before") {
			t.Fatalf("backward paging error = %v, want explicit unsupported error", err)
		}
	}
}
