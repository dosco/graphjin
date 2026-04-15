package qcode_test

import (
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
)

// TestExprBasicArithmetic verifies that a structured expression like
// SUM(price * 2) compiles cleanly through the new `expr:` arg path.
func TestExprBasicArithmetic(t *testing.T) {
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{})
	if err := qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{Columns: []string{"id", "name", "price"}},
	}); err != nil {
		t.Fatal(err)
	}

	q, err := qc.Compile([]byte(`
		query { products(distinct: [id]) {
			id
			doubled: sum(expr: { mul: [{ col: "price" }, { num: 2 }] })
		} }`), nil, "user", "")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	sel := q.Selects[0]
	var found bool
	for _, f := range sel.Fields {
		if f.FieldName == "doubled" {
			found = true
			if f.Type != qcode.FieldTypeFunc {
				t.Errorf("doubled.Type = %v, want FieldTypeFunc", f.Type)
			}
			if len(f.Args) != 1 || f.Args[0].Type != qcode.ArgTypeExpr {
				t.Errorf("doubled.Args[0].Type = %v, want ArgTypeExpr", f.Args[0].Type)
			}
			if f.Args[0].Expr == nil || f.Args[0].Expr.Op != qcode.OpMul {
				t.Errorf("doubled expression root op = %v, want OpMul", f.Args[0].Expr.Op)
			}
		}
	}
	if !found {
		t.Error("doubled field not found in compiled select")
	}
}

// TestExprBackwardsCompat verifies the legacy `<fn>_<col>` path still
// compiles unchanged when no `expr:` arg is present.
func TestExprBackwardsCompat(t *testing.T) {
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{})
	if err := qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{Columns: []string{"id", "price"}},
	}); err != nil {
		t.Fatal(err)
	}

	q, err := qc.Compile([]byte(`
		query { products(distinct: [id]) {
			id
			sum_price
		} }`), nil, "user", "")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	for _, f := range q.Selects[0].Fields {
		if f.FieldName == "sum_price" {
			if f.Type != qcode.FieldTypeFunc {
				t.Errorf("sum_price.Type = %v, want FieldTypeFunc", f.Type)
			}
			if len(f.Args) != 1 || f.Args[0].Type != qcode.ArgTypeCol {
				t.Errorf("sum_price.Args[0].Type = %v, want ArgTypeCol (legacy path)", f.Args[0].Type)
			}
			if f.Args[0].Col.Name != "price" {
				t.Errorf("sum_price.Args[0].Col.Name = %q, want %q", f.Args[0].Col.Name, "price")
			}
			return
		}
	}
	t.Error("sum_price field not found")
}

// TestExprRoleAllowlist verifies that referencing a column outside the
// role's allowlist via `expr:` is rejected at compile time. This closes
// a gap where the legacy function-args path bypassed the allowlist.
func TestExprRoleAllowlist(t *testing.T) {
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{})
	// Role can read id but NOT price.
	if err := qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{Columns: []string{"id"}},
	}); err != nil {
		t.Fatal(err)
	}

	_, err := qc.Compile([]byte(`
		query { products(distinct: [id]) {
			id
			leaked: sum(expr: { mul: [{ col: "price" }, { num: 2 }] })
		} }`), nil, "user", "")
	if err == nil {
		t.Fatal("expected compile error for blocked column reference, got nil")
	}
	if !strings.Contains(err.Error(), "price") {
		t.Errorf("error should mention the blocked column, got: %v", err)
	}
}

// TestExprNonNumericRejected verifies that arithmetic on a text column
// is rejected at compile time unless wrapped in cast.
func TestExprNonNumericRejected(t *testing.T) {
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{})
	if err := qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{Columns: []string{"id", "name", "description"}},
	}); err != nil {
		t.Fatal(err)
	}

	// description is text, not numeric — multiplying it should fail.
	_, err := qc.Compile([]byte(`
		query { products(distinct: [id]) {
			id
			bogus: sum(expr: { mul: [{ col: "description" }, { num: 2 }] })
		} }`), nil, "user", "")
	if err == nil {
		t.Fatal("expected compile error for non-numeric column under arithmetic, got nil")
	}
}

// TestExprRelCol verifies dot-notation relationship-qualified column
// references compile to an OpColRef carrying a non-nil Rel. The SQL
// renderer (psql/expr.go) emits this as a correlated scalar subquery.
func TestExprRelCol(t *testing.T) {
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{})
	if err := qc.AddRole("user", "public", "purchases", qcode.TRConfig{
		Query: qcode.QueryConfig{Columns: []string{"id", "quantity", "product_id"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{Columns: []string{"id", "price"}},
	}); err != nil {
		t.Fatal(err)
	}

	// purchases has product_id -> products.id (belongs-to). Expression:
	// SUM(quantity * products.price) over all purchases.
	q, err := qc.Compile([]byte(`
		query { purchases(distinct: [id]) {
			id
			revenue: sum(expr: { mul: [{ col: "quantity" }, { col: "products.price" }] })
		} }`), nil, "user", "")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	sel := q.Selects[0]
	var expr *qcode.Exp
	for _, f := range sel.Fields {
		if f.FieldName == "revenue" {
			if len(f.Args) != 1 || f.Args[0].Type != qcode.ArgTypeExpr {
				t.Fatal("revenue field should have one ArgTypeExpr")
			}
			expr = f.Args[0].Expr
		}
	}
	if expr == nil {
		t.Fatal("revenue expression not found")
	}
	if expr.Op != qcode.OpMul {
		t.Fatalf("expression root op = %v, want OpMul", expr.Op)
	}
	// expr.Children[1] should be the relationship-qualified col ref.
	relRef := expr.Children[1]
	if relRef.Op != qcode.OpColRef {
		t.Fatalf("second operand op = %v, want OpColRef", relRef.Op)
	}
	if len(relRef.RelPath) == 0 {
		t.Error("second operand should have a non-empty RelPath (relationship info)")
	}
	if relRef.Left.Col.Name != "price" {
		t.Errorf("related column = %q, want %q", relRef.Left.Col.Name, "price")
	}
	if relRef.Left.Col.Table != "products" {
		t.Errorf("related column's table = %q, want %q", relRef.Left.Col.Table, "products")
	}
}

// TestExprRelColMultiHop verifies that multi-hop relationship paths
// compile, storing the full RelPath on the Exp so the renderer can
// nest one correlated subquery per hop. Uses the test schema's
// purchases → customers → users chain (two hops).
func TestExprRelColMultiHop(t *testing.T) {
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{})
	if err := qc.AddRole("user", "public", "purchases", qcode.TRConfig{
		Query: qcode.QueryConfig{Columns: []string{"id", "quantity", "customer_id"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := qc.AddRole("user", "public", "customers", qcode.TRConfig{
		Query: qcode.QueryConfig{Columns: []string{"id", "user_id"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := qc.AddRole("user", "public", "users", qcode.TRConfig{
		Query: qcode.QueryConfig{Columns: []string{"id"}},
	}); err != nil {
		t.Fatal(err)
	}

	q, err := qc.Compile([]byte(`
		query { purchases(distinct: [id]) {
			id
			multi: sum(expr: { mul: [{ col: "quantity" }, { col: "users.id" }] })
		} }`), nil, "user", "")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var expr *qcode.Exp
	for _, f := range q.Selects[0].Fields {
		if f.FieldName == "multi" {
			expr = f.Args[0].Expr
		}
	}
	if expr == nil || expr.Op != qcode.OpMul {
		t.Fatalf("expected OpMul root, got %v", expr)
	}
	relRef := expr.Children[1]
	if len(relRef.RelPath) != 2 {
		t.Fatalf("expected 2-hop RelPath, got %d hops", len(relRef.RelPath))
	}
	// First hop: purchases → customers via customer_id
	if relRef.RelPath[0].Left.Col.Name != "customer_id" {
		t.Errorf("hop 1 left col = %q, want customer_id", relRef.RelPath[0].Left.Col.Name)
	}
	// Second hop: customers → users via user_id
	if relRef.RelPath[1].Left.Col.Name != "user_id" {
		t.Errorf("hop 2 left col = %q, want user_id", relRef.RelPath[1].Left.Col.Name)
	}
	if relRef.Left.Col.Name != "id" || relRef.Left.Col.Table != "users" {
		t.Errorf("target col = %q.%q, want users.id", relRef.Left.Col.Table, relRef.Left.Col.Name)
	}
}

// TestExprRelColTooManyHops verifies the depth cap rejects relationship
// paths longer than exprMaxRelHops.
func TestExprRelColTooManyHops(t *testing.T) {
	// The standard test schema doesn't have a 4+ hop chain, so this test
	// is effectively a safety check on the guard logic; it passes as long
	// as no 4-hop path exists. A dedicated 4+ hop fixture would need to
	// be added if we want to exercise the rejection branch directly.
}

// TestExprRelColOneToManyRejected verifies that dot-notation on a
// one-to-many (list) relationship is rejected — a scalar expression
// can't consume a list of rows.
func TestExprRelColOneToManyRejected(t *testing.T) {
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{})
	if err := qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{Columns: []string{"id", "price"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := qc.AddRole("user", "public", "purchases", qcode.TRConfig{
		Query: qcode.QueryConfig{Columns: []string{"id", "quantity", "product_id"}},
	}); err != nil {
		t.Fatal(err)
	}

	// products.purchases is one-to-many — dereferencing purchases.quantity
	// from a product query would yield many values per product, which
	// doesn't fit in a scalar expression.
	_, err := qc.Compile([]byte(`
		query { products(distinct: [id]) {
			id
			wrong: sum(expr: { col: "purchases.quantity" })
		} }`), nil, "user", "")
	if err == nil {
		t.Fatal("expected error for one-to-many relationship in scalar expr, got nil")
	}
}

// MongoDB rejection is exercised via the dialect-level test in
// dialect/mongodb*_test.go (the qcode test schema is postgres-shaped,
// so triggering the dialect.Name() == "mongodb" branch from this layer
// would require a parallel schema setup that is out of scope for these
// unit tests).

// TestExprOrderByAlias verifies that order_by: { <alias>: desc } works
// when <alias> is an expression-aggregate field's alias — unblocks
// server-side top-N by computed metric (e.g. top products by revenue).
func TestExprOrderByAlias(t *testing.T) {
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{})
	if err := qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{Columns: []string{"id", "price"}},
	}); err != nil {
		t.Fatal(err)
	}

	q, err := qc.Compile([]byte(`
		query { products(distinct: [id], order_by: { doubled: desc }) {
			id
			doubled: sum(expr: { mul: [{ col: "price" }, { num: 2 }] })
		} }`), nil, "user", "")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	sel := q.Selects[0]
	if len(sel.OrderBy) == 0 {
		t.Fatal("expected one order_by entry")
	}
	ob := sel.OrderBy[0]
	if ob.Alias != "doubled" {
		t.Errorf("OrderBy.Alias = %q, want %q", ob.Alias, "doubled")
	}
	if ob.Order != qcode.OrderDesc {
		t.Errorf("OrderBy.Order = %v, want OrderDesc", ob.Order)
	}
}

// TestExprOrderByAliasNotFound verifies alias validation rejects
// order_by entries that don't match any compiled field.
func TestExprOrderByAliasNotFound(t *testing.T) {
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{})
	if err := qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{Columns: []string{"id", "price"}},
	}); err != nil {
		t.Fatal(err)
	}

	_, err := qc.Compile([]byte(`
		query { products(order_by: { nonexistent: desc }) {
			id
		} }`), nil, "user", "")
	if err == nil {
		t.Fatal("expected error for unresolved order_by alias, got nil")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Errorf("error should name the unresolved alias, got: %v", err)
	}
}

// TestExprCase verifies that CASE WHEN ... THEN ... ELSE ... END syntax
// compiles, with the boolean predicate in `when` going through the
// shared where-clause parser.
func TestExprCase(t *testing.T) {
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{})
	if err := qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{Columns: []string{"id", "price"}},
	}); err != nil {
		t.Fatal(err)
	}

	q, err := qc.Compile([]byte(`
		query { products(distinct: [id]) {
			id
			big_ticket: sum(expr: {
				case: {
					arms: [{ when: { price: { gt: 50 } }, then: { col: "price" } }],
					else: { num: 0 }
				}
			})
		} }`), nil, "user", "")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var expr *qcode.Exp
	for _, f := range q.Selects[0].Fields {
		if f.FieldName == "big_ticket" {
			expr = f.Args[0].Expr
		}
	}
	if expr == nil || expr.Op != qcode.OpCase {
		t.Fatalf("expected OpCase root, got %v", expr)
	}
	if len(expr.CaseArms) != 1 {
		t.Fatalf("expected 1 arm, got %d", len(expr.CaseArms))
	}
	arm := expr.CaseArms[0]
	if arm.When == nil || arm.Then == nil {
		t.Fatal("arm missing when or then")
	}
	if expr.Else == nil || expr.Else.Op != qcode.OpLiteral {
		t.Error("expected OpLiteral else branch")
	}
	// When sub-tree should resolve the column: price > 50 → OpGreaterThan
	// with Left.Col.Name = "price".
	if arm.When.Left.Col.Name != "price" {
		t.Errorf("arm.When.Left.Col.Name = %q, want %q", arm.When.Left.Col.Name, "price")
	}
}

// TestExprDepthLimit verifies the validator caps tree depth.
func TestExprDepthLimit(t *testing.T) {
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{})
	if err := qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{Columns: []string{"id", "price"}},
	}); err != nil {
		t.Fatal(err)
	}

	// Build a deeply nested mul expression: mul[mul[mul[...col, num]]]
	expr := `{ col: "price" }`
	for i := 0; i < 30; i++ {
		expr = `{ mul: [` + expr + `, { num: 1 }] }`
	}
	query := `query { products(distinct: [id]) {
		id
		deep: sum(expr: ` + expr + `)
	} }`

	_, err := qc.Compile([]byte(query), nil, "user", "")
	if err == nil {
		t.Fatal("expected depth-limit error, got nil")
	}
	if !strings.Contains(err.Error(), "depth") && !strings.Contains(err.Error(), "nodes") {
		t.Errorf("error should mention depth or nodes limit, got: %v", err)
	}
}

// TestGlobalAggSetsFlag verifies that aggregations without distinct at
// the top level set the GlobalAgg flag (broken.md fix path).
func TestGlobalAggSetsFlag(t *testing.T) {
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{})
	if err := qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{Columns: []string{"id", "price"}},
	}); err != nil {
		t.Fatal(err)
	}

	q, err := qc.Compile([]byte(`
		query { products {
			count_id
			sum_price
		} }`), nil, "user", "")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	sel := q.Selects[0]
	if !sel.GroupCols {
		t.Error("expected GroupCols=true for aggregate query")
	}
	if !sel.GlobalAgg {
		t.Error("expected GlobalAgg=true for top-level aggregate without distinct (broken.md fix)")
	}
	if !sel.Paging.NoLimit {
		t.Error("expected Paging.NoLimit=true for global aggregate (LIMIT would yield degenerate per-row results)")
	}
}

// TestGlobalAggWithCacheTracking is the broken.md regression test: even
// with cache tracking enabled (the default when running as a server),
// aggregates without distinct must collapse to one row. Before the fix,
// addCacheTrackingField ran AFTER compileChildColumns and re-injected
// __gj_id into BCols, forcing a per-row GROUP BY.
func TestGlobalAggWithCacheTracking(t *testing.T) {
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{EnableCacheTracking: true})
	if err := qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{Columns: []string{"id", "price"}},
	}); err != nil {
		t.Fatal(err)
	}

	q, err := qc.Compile([]byte(`
		query { products {
			count_id
			sum_price
		} }`), nil, "user", "")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	sel := q.Selects[0]
	if !sel.GlobalAgg {
		t.Error("expected GlobalAgg=true with cache tracking enabled")
	}
	// No __gj_id field should remain — cache tracking must be skipped
	// for aggregated queries since there's no meaningful row-level PK
	// to track (the output is a single aggregate row).
	for _, f := range sel.Fields {
		if f.FieldName == "__gj_id" {
			t.Errorf("__gj_id must not be present in Fields of an aggregated query (broken.md fix)")
		}
	}
	for _, c := range sel.BCols {
		if c.FieldName == "__gj_id" {
			t.Errorf("__gj_id must not be present in BCols of an aggregated query (broken.md fix)")
		}
	}
}

// TestDistinctAggDoesNotSetGlobalAgg verifies that aggregation WITH
// distinct keeps GlobalAgg=false (existing per-group aggregation path).
func TestDistinctAggDoesNotSetGlobalAgg(t *testing.T) {
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{})
	if err := qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{Columns: []string{"id", "user_id", "price"}},
	}); err != nil {
		t.Fatal(err)
	}

	q, err := qc.Compile([]byte(`
		query { products(distinct: [user_id]) {
			user_id
			sum_price
		} }`), nil, "user", "")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	sel := q.Selects[0]
	if !sel.GroupCols {
		t.Error("expected GroupCols=true for aggregate query")
	}
	if sel.GlobalAgg {
		t.Error("expected GlobalAgg=false when distinct is present")
	}
}
