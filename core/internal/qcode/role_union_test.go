package qcode_test

import (
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
)

func TestCompileUnionRoleAppliesMergedRules(t *testing.T) {
	qc, err := qcode.NewCompiler(dbs, qcode.Config{UnionRoles: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := qc.AddRole("finance", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{Filters: []string{`{ price: { gt: 50 } }`}, Columns: []string{"id", "name", "price"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := qc.AddRole("sales", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{Filters: []string{`{ name: { like: "a%" } }`}, Columns: []string{"id", "name"}},
	}); err != nil {
		t.Fatal(err)
	}

	q, err := qc.Compile([]byte(`query { products { id name } }`), nil, "finance+sales", "")
	if err != nil {
		t.Fatalf("shared columns should compile for the union role: %v", err)
	}
	where := q.Selects[0].Where.Exp
	if where == nil || !containsOp(where, qcode.OpOr) {
		t.Fatalf("union role should filter rows with the or of both role filters, got %+v", where)
	}

	_, err = qc.Compile([]byte(`query { products { id price } }`), nil, "finance+sales", "")
	if err == nil || !strings.Contains(err.Error(), "price") {
		t.Fatalf("price is visible to finance rows only and must be blocked for the union, got %v", err)
	}

	if _, err := qc.Compile([]byte(`query { products { id price } }`), nil, "finance", ""); err != nil {
		t.Fatalf("a single role keeps its own columns: %v", err)
	}
}

func containsOp(ex *qcode.Exp, op qcode.ExpOp) bool {
	if ex == nil {
		return false
	}
	if ex.Op == op {
		return true
	}
	for _, child := range ex.Children {
		if containsOp(child, op) {
			return true
		}
	}
	return false
}
