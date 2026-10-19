package clickhousedriver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// fakeExec routes a Statement to canned rows by table name, returning copies so
// the resolver's in-place child attachment doesn't corrupt the source.
type fakeExec struct {
	tables  map[string][]map[string]any
	queries []string
}

func (f *fakeExec) Query(_ context.Context, stmt Statement) (ResultSet, error) {
	f.queries = append(f.queries, stmt.SQL)
	rows := f.tables[tableOf(stmt.SQL)]
	out := make([]map[string]any, len(rows))
	for i, r := range rows {
		cp := make(map[string]any, len(r))
		for k, v := range r {
			cp[k] = v
		}
		out[i] = cp
	}
	return ResultSet{Rows: out}, nil
}

func (f *fakeExec) Exec(_ context.Context, _ Statement) error { return nil }

func tableOf(sqlStr string) string {
	i := strings.Index(sqlStr, " FROM ")
	if i < 0 {
		return ""
	}
	rest := sqlStr[i+6:]
	if end := strings.IndexByte(rest, ' '); end >= 0 {
		rest = rest[:end]
	}
	rest = strings.ReplaceAll(rest, "`", "")
	if dot := strings.LastIndex(rest, "."); dot >= 0 {
		rest = rest[dot+1:]
	}
	return rest
}

func TestResolveNested(t *testing.T) {
	exec := &fakeExec{tables: map[string][]map[string]any{
		"products": {{"id": 1, "owner_id": 10}, {"id": 2, "owner_id": 20}},
		"users":    {{"id": 10}, {"id": 20}},
		"comments": {{"id": 100, "product_id": 1}, {"id": 101, "product_id": 1}, {"id": 102, "product_id": 2}},
	}}
	root := &Node{
		Table: "products", Columns: []string{"id", "owner_id"}, FieldName: "products",
		Children: []*Node{
			{Table: "users", Columns: []string{"id"}, FieldName: "owner", Singular: true,
				Rel: &Rel{ParentCol: "owner_id", ChildCol: "id", Kind: RelOneToOne}},
			{Table: "comments", Columns: []string{"id", "product_id"}, FieldName: "comments",
				Rel: &Rel{ParentCol: "id", ChildCol: "product_id", Kind: RelOneToMany}},
		},
	}
	r := &Resolver{Exec: exec, DefaultDatabase: "shop"}
	data, err := r.ResolveQuery(context.Background(), &QueryDSL{Operation: OpQuery, Root: root})
	if err != nil {
		t.Fatal(err)
	}

	var got struct {
		Products []struct {
			ID       int            `json:"id"`
			Owner    map[string]any `json:"owner"`
			Comments []any          `json:"comments"`
		} `json:"products"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v (%s)", err, data)
	}
	if len(got.Products) != 2 {
		t.Fatalf("products = %d, want 2", len(got.Products))
	}
	p1 := got.Products[0]
	if p1.ID != 1 {
		t.Errorf("p1 id = %d, want 1", p1.ID)
	}
	if p1.Owner == nil || p1.Owner["id"].(float64) != 10 {
		t.Errorf("p1 owner = %v, want id 10", p1.Owner)
	}
	if len(p1.Comments) != 2 {
		t.Errorf("p1 comments = %d, want 2", len(p1.Comments))
	}
	if len(got.Products[1].Comments) != 1 {
		t.Errorf("p2 comments = %d, want 1", len(got.Products[1].Comments))
	}
}

func TestResolveSingularEmpty(t *testing.T) {
	exec := &fakeExec{tables: map[string][]map[string]any{"users": {}}}
	root := &Node{Table: "users", Columns: []string{"id"}, FieldName: "users", Singular: true}
	r := &Resolver{Exec: exec}
	data, err := r.ResolveQuery(context.Background(), &QueryDSL{Operation: OpQuery, Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"users":null}` {
		t.Errorf("got %s, want {\"users\":null}", data)
	}
}

func TestParseAndSubstitute(t *testing.T) {
	dsl := `{"operation":"query","root":{"table":"users","columns":["id"],` +
		`"filters":[{"col":"id","op":"eq","param":"$1"}],"field_name":"users","singular":true}}`
	q, err := ParseQuery("/* meta */ " + dsl)
	if err != nil {
		t.Fatal(err)
	}
	if err := q.SubstituteParams([]any{int64(42)}); err != nil {
		t.Fatal(err)
	}
	got := q.Root.Filters[0].Value
	if got != int64(42) {
		t.Errorf("filter value = %v (%T), want 42", got, got)
	}
}
