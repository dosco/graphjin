package core

import (
	"context"
	"encoding/json"
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
