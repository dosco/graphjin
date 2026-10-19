package qcode_test

import (
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
)

// Regression tests for the aggregate order_by bug: ordering by an
// aggregate-prefixed field (sum_price) used to add the aggregate's source
// column to BCols. BCols drives both the base SELECT list and GROUP BY, so
// the grouping degenerated to per-row groups and the "top group" answer
// was silently a single row's value.

func newAggOrderByCompiler(t *testing.T) *qcode.Compiler {
	t.Helper()
	qc, err := qcode.NewCompiler(dbs, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{Columns: []string{"id", "name", "price"}},
	}); err != nil {
		t.Fatal(err)
	}
	return qc
}

// TestOrderByAggregateKeepsGrouping: a grouped selection (dimension +
// aggregate) ordered by the aggregate must stay grouped on the dimension
// only — the aggregate's source column must not leak into BCols.
func TestOrderByAggregateKeepsGrouping(t *testing.T) {
	qc := newAggOrderByCompiler(t)

	q, err := qc.Compile([]byte(`
		query { products(order_by: { sum_price: desc }, limit: 1) {
			name
			sum_price
		} }`), nil, "user", "")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	sel := q.Selects[0]
	if !sel.GroupCols {
		t.Error("GroupCols = false, want true (aggregate selection must group)")
	}
	if sel.GlobalAgg {
		t.Error("GlobalAgg = true, want false (dimension column present)")
	}

	if len(sel.OrderBy) != 1 {
		t.Fatalf("len(OrderBy) = %d, want 1", len(sel.OrderBy))
	}
	ob := sel.OrderBy[0]
	if !ob.IsFunc || ob.Func.Name != "sum" || ob.Col.Name != "price" {
		t.Errorf("OrderBy[0] = {IsFunc:%v Func:%q Col:%q}, want aggregate sum(price)",
			ob.IsFunc, ob.Func.Name, ob.Col.Name)
	}

	var hasName bool
	for _, bc := range sel.BCols {
		switch bc.Col.Name {
		case "name":
			hasName = true
		case "price":
			t.Error("BCols contains 'price' — the aggregate's source column would land in GROUP BY and collapse each group to a single row")
		}
	}
	if !hasName {
		t.Error("BCols missing 'name' (the grouping dimension)")
	}

	if sel.Paging.Limit != 1 || sel.Paging.NoLimit {
		t.Errorf("Paging = {Limit:%d NoLimit:%v}, want Limit:1 (grouped top-1 must keep its limit)",
			sel.Paging.Limit, sel.Paging.NoLimit)
	}
}

// TestOrderByAggregatePromotesGrouping: ordering by an aggregate that is
// not part of the selection still forces a grouped compilation — ORDER BY
// SUM(x) is only valid alongside a GROUP BY over the selected columns.
func TestOrderByAggregatePromotesGrouping(t *testing.T) {
	qc := newAggOrderByCompiler(t)

	q, err := qc.Compile([]byte(`
		query { products(order_by: { sum_price: desc }, limit: 5) {
			name
		} }`), nil, "user", "")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	sel := q.Selects[0]
	if !sel.GroupCols {
		t.Error("GroupCols = false, want true (aggregate order_by must promote grouping)")
	}
	for _, bc := range sel.BCols {
		if bc.Col.Name == "price" {
			t.Error("BCols contains 'price' — aggregate order_by column must not be projected/grouped")
		}
	}
}

// TestOrderByPlainColumnStillProjected: control case — a plain-column
// order_by keeps its column in BCols (the base select must project it)
// and does not group.
func TestOrderByPlainColumnStillProjected(t *testing.T) {
	qc := newAggOrderByCompiler(t)

	q, err := qc.Compile([]byte(`
		query { products(order_by: { price: desc }, limit: 3) {
			name
		} }`), nil, "user", "")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	sel := q.Selects[0]
	if sel.GroupCols {
		t.Error("GroupCols = true, want false (no aggregates anywhere)")
	}
	var hasPrice bool
	for _, bc := range sel.BCols {
		if bc.Col.Name == "price" {
			hasPrice = true
		}
	}
	if !hasPrice {
		t.Error("BCols missing 'price' — plain order_by columns must stay projected")
	}
}
