package core

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func newNanoDBSearchTestGraphJin(t *testing.T) *GraphJin {
	t.Helper()
	rows := []NanoRow{
		{"id": "hi", "title": "symbols symbols symbols", "kind": "column", "search_vector": "symbols symbols symbols title"},
		{"id": "mid1", "title": "one", "kind": "column", "search_vector": "symbols one"},
		{"id": "mid2", "title": "two", "kind": "column", "search_vector": "symbols two"},
		{"id": "mid3", "title": "three", "kind": "column", "search_vector": "symbols three"},
		{"id": "lo", "title": "entry", "kind": "entrypoint", "search_vector": "symbols entry"},
		{"id": "none", "title": "unrelated", "kind": "column", "search_vector": "billing invoices"},
	}
	db, err := NewNanoDB(NanoSnapshot{Schema: "main", Tables: []NanoTable{{
		Name: "gj_catalog",
		Columns: []NanoColumn{
			{Name: "id", Type: "text", PrimaryKey: true},
			{Name: "title", Type: "text"},
			{Name: "kind", Type: "text", Index: true},
			{Name: "search_rank", Type: "float"},
			{Name: "score", Type: "float"},
			{Name: "search_vector", Type: "text", FullText: true},
		},
		Rows: rows,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	conf := &Config{
		DBType:           "nanodb",
		DisableAllowList: true,
		Databases:        map[string]DatabaseConfig{DefaultDBName: {Type: "nanodb"}},
	}
	gj, err := NewGraphJin(conf, nil, OptionSetNanoDatabases(map[string]*NanoDB{DefaultDBName: db}))
	if err != nil {
		t.Fatal(err)
	}
	return gj
}

func nanoDBSearchDocs(t *testing.T, gj *GraphJin, query string) []string {
	t.Helper()
	res, err := gj.GraphQL(context.Background(), query, nil, nil)
	if err != nil || len(res.Errors) != 0 {
		t.Fatalf("GraphQL: err=%v errors=%+v", err, res.Errors)
	}
	var out struct {
		Rows []struct {
			ID string `json:"id"`
		} `json:"gj_catalog"`
	}
	if err := json.Unmarshal(res.Data, &out); err != nil {
		t.Fatalf("decode response: %v\n%s", err, res.Data)
	}
	ids := make([]string, 0, len(out.Rows))
	for _, row := range out.Rows {
		ids = append(ids, row.ID)
	}
	return ids
}

// MCP query_catalog issues search queries ordered by search_rank; the nano
// executor must return the same match set as an unordered search.
func TestNanoDBSearchWithSearchRankOrderBy(t *testing.T) {
	gj := newNanoDBSearchTestGraphJin(t)
	defer gj.Close()

	plain := nanoDBSearchDocs(t, gj, `query { gj_catalog(search: "symbols", limit: 50) { id } }`)
	if len(plain) != 5 {
		t.Fatalf("plain search returned %d rows, want 5: %v", len(plain), plain)
	}

	ordered := nanoDBSearchDocs(t, gj, `query { gj_catalog(search: "symbols", order_by: { search_rank: desc }, limit: 50) { id } }`)
	if len(ordered) != len(plain) {
		t.Fatalf("search with search_rank order returned %d rows, want %d: %v", len(ordered), len(plain), ordered)
	}
	if ordered[0] != "hi" {
		t.Fatalf("highest-rank row should sort first, got %v", ordered)
	}
}

func TestNanoDBCursorPaginationAndVariableLimit(t *testing.T) {
	gj := newNanoDBSearchTestGraphJin(t)
	defer gj.Close()

	query := `query page($first: Int!, $gj_catalog_cursor: Cursor) {
		gj_catalog(first: $first, after: $gj_catalog_cursor, order_by: { id: asc }) {
			id
		}
		gj_catalog_cursor
	}`
	first, err := gj.GraphQL(context.Background(), query,
		json.RawMessage(`{"first":2,"gj_catalog_cursor":null}`), nil)
	if err != nil || len(first.Errors) != 0 {
		t.Fatalf("first page: err=%v errors=%+v", err, first.Errors)
	}
	var page1 struct {
		Rows []struct {
			ID string `json:"id"`
		} `json:"gj_catalog"`
		Cursor string `json:"gj_catalog_cursor"`
	}
	if err := json.Unmarshal(first.Data, &page1); err != nil {
		t.Fatalf("decode first page: %v\n%s", err, first.Data)
	}
	if got, want := len(page1.Rows), 2; got != want {
		t.Fatalf("first page row count = %d, want %d: %s", got, want, first.Data)
	}
	if page1.Cursor == "" {
		t.Fatalf("first page cursor missing: %s", first.Data)
	}

	vars, err := json.Marshal(map[string]any{
		"first":             2,
		"gj_catalog_cursor": page1.Cursor,
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := gj.GraphQL(context.Background(), query, vars, nil)
	if err != nil || len(second.Errors) != 0 {
		t.Fatalf("second page: err=%v errors=%+v", err, second.Errors)
	}
	var page2 struct {
		Rows []struct {
			ID string `json:"id"`
		} `json:"gj_catalog"`
		Cursor string `json:"gj_catalog_cursor"`
	}
	if err := json.Unmarshal(second.Data, &page2); err != nil {
		t.Fatalf("decode second page: %v\n%s", err, second.Data)
	}
	if got, want := len(page2.Rows), 2; got != want {
		t.Fatalf("second page row count = %d, want %d: %s", got, want, second.Data)
	}
	if page2.Rows[0].ID == page1.Rows[0].ID || page2.Rows[0].ID == page1.Rows[1].ID {
		t.Fatalf("second page repeated first-page rows: first=%+v second=%+v", page1.Rows, page2.Rows)
	}
}

func TestNanoDBVariableLimitAndOffset(t *testing.T) {
	gj := newNanoDBSearchTestGraphJin(t)
	defer gj.Close()

	result, err := gj.GraphQL(context.Background(), `query page($limit: Int!, $offset: Int!) {
		gj_catalog(limit: $limit, offset: $offset, order_by: { id: asc }) {
			id
		}
	}`, json.RawMessage(`{"limit":2,"offset":1}`), nil)
	if err != nil || len(result.Errors) != 0 {
		t.Fatalf("variable limit/offset: err=%v errors=%+v", err, result.Errors)
	}
	var page struct {
		Rows []struct {
			ID string `json:"id"`
		} `json:"gj_catalog"`
	}
	if err := json.Unmarshal(result.Data, &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Rows) != 2 || page.Rows[0].ID != "lo" || page.Rows[1].ID != "mid1" {
		t.Fatalf("variable limit/offset page = %s", result.Data)
	}
}

func TestNanoDBCursorPaginationRejectsBackwardPaging(t *testing.T) {
	gj := newNanoDBSearchTestGraphJin(t)
	defer gj.Close()

	for _, query := range []string{
		`query { gj_catalog(last: 2, order_by: { id: asc }) { id } gj_catalog_cursor }`,
		`query page($gj_catalog_cursor: Cursor) {
			gj_catalog(last: 2, before: $gj_catalog_cursor, order_by: { id: asc }) { id }
			gj_catalog_cursor
		}`,
	} {
		_, err := gj.GraphQL(context.Background(), query,
			json.RawMessage(`{"gj_catalog_cursor":null}`), nil)
		if err == nil || !strings.Contains(err.Error(), "does not support last/before") {
			t.Fatalf("backward paging error = %v, want explicit unsupported error", err)
		}
	}
}
