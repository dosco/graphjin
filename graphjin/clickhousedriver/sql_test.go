package clickhousedriver

import (
	"reflect"
	"testing"
)

func TestBuildSelectBasic(t *testing.T) {
	n := &Node{Table: "users", Columns: []string{"id", "email"}}
	got, args, err := BuildSelect(n, "shop", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "SELECT `id`, `email` FROM `shop`.`users`"
	if got != want {
		t.Errorf("sql = %q, want %q", got, want)
	}
	if len(args) != 0 {
		t.Errorf("args = %v, want none", args)
	}
}

func TestBuildSelectFiltersAndOrder(t *testing.T) {
	n := &Node{
		Table:   "products",
		Columns: []string{"id", "price"},
		Filters: []Filter{{Col: "price", Op: OpGte, Value: 10}},
		OrderBy: []OrderBy{{Col: "price", Order: "desc"}, {Col: "id", Order: "asc"}},
		Limit:   5,
		Offset:  10,
	}
	got, args, err := BuildSelect(n, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "SELECT `id`, `price` FROM `products` WHERE `price` >= ? ORDER BY `price` DESC, `id` ASC LIMIT 5 OFFSET 10"
	if got != want {
		t.Errorf("sql = %q, want %q", got, want)
	}
	if !reflect.DeepEqual(args, []any{10}) {
		t.Errorf("args = %v, want [10]", args)
	}
}

func TestBuildSelectOrNotLikeIsNull(t *testing.T) {
	n := &Node{
		Table:   "t",
		Columns: []string{"a"},
		Filters: []Filter{{Or: []Filter{
			{Col: "a", Op: OpLike, Value: "x%"},
			{Not: []Filter{{Col: "b", Op: OpIsNull}}},
		}}},
	}
	got, args, err := BuildSelect(n, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "SELECT `a` FROM `t` WHERE (`a` LIKE ? OR (NOT `b` IS NULL))"
	if got != want {
		t.Errorf("sql = %q, want %q", got, want)
	}
	if !reflect.DeepEqual(args, []any{"x%"}) {
		t.Errorf("args = %v, want [x%%]", args)
	}
}

func TestBuildSelectInExpansion(t *testing.T) {
	n := &Node{Table: "t", Columns: []string{"id"}}
	extra := []Filter{{Col: "id", Op: OpIn, Value: []any{1, 2, 3}}}
	got, args, err := BuildSelect(n, "", extra)
	if err != nil {
		t.Fatal(err)
	}
	want := "SELECT `id` FROM `t` WHERE `id` IN (?, ?, ?)"
	if got != want {
		t.Errorf("sql = %q, want %q", got, want)
	}
	if !reflect.DeepEqual(args, []any{1, 2, 3}) {
		t.Errorf("args = %v", args)
	}
}

func TestBuildSelectAggregateGroupBy(t *testing.T) {
	n := &Node{
		Table:      "purchases",
		GroupBy:    []string{"product_id"},
		Aggregates: []Aggregate{{Fn: "count", Col: "*", Alias: "count"}, {Fn: "sum", Col: "quantity", Alias: "sum_quantity"}},
	}
	got, _, err := BuildSelect(n, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "SELECT `product_id`, count(*) AS `count`, sum(`quantity`) AS `sum_quantity` FROM `purchases` GROUP BY `product_id`"
	if got != want {
		t.Errorf("sql = %q, want %q", got, want)
	}
}

func TestBuildSelectUnsupportedOp(t *testing.T) {
	n := &Node{Table: "t", Columns: []string{"a"}, Filters: []Filter{{Col: "a", Op: "regex", Value: "x"}}}
	if _, _, err := BuildSelect(n, "", nil); err == nil {
		t.Fatal("expected error for unsupported operator")
	}
}

func TestChTypeToSQL(t *testing.T) {
	cases := []struct {
		in       string
		sqlType  string
		array    bool
		nullable bool
	}{
		{"Int32", "integer", false, false},
		{"UInt64", "bigint", false, false},
		{"Float64", "double precision", false, false},
		{"String", "text", false, false},
		{"Nullable(String)", "text", false, true},
		{"LowCardinality(String)", "text", false, false},
		{"Array(Int32)", "integer", true, false},
		{"Array(Nullable(String))", "text", true, true},
		{"DateTime64(3, 'UTC')", "timestamptz", false, false},
		{"Decimal(10, 2)", "numeric", false, false},
		{"Map(String, UInt64)", "json", false, false},
		{"UUID", "uuid", false, false},
		{"Bool", "boolean", false, false},
	}
	for _, c := range cases {
		st, arr, null := chTypeToSQL(c.in)
		if st != c.sqlType || arr != c.array || null != c.nullable {
			t.Errorf("chTypeToSQL(%q) = (%q,%v,%v), want (%q,%v,%v)",
				c.in, st, arr, null, c.sqlType, c.array, c.nullable)
		}
	}
}
