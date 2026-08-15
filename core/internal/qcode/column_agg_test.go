package qcode_test

import (
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
)

// The `<agg>(column: <col>)` spelling is what models naturally write for the
// prefix form; before it was accepted, `max_renewal_date: max(column:
// renewal_date)` died as `unknown argument 'column'` with no hint, and a
// benchmark run burned whole step budgets on it. These tests pin that the form
// compiles to the exact shape the prefix form produces.

func TestColumnArgAggregate(t *testing.T) {
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{})
	if err := qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{Columns: []string{"id", "price"}},
	}); err != nil {
		t.Fatal(err)
	}

	for _, form := range []string{
		`max_price: max(column: price)`,
		`max_price: max(column: "price")`,
	} {
		q, err := qc.Compile([]byte(`query { products { id `+form+` } }`), nil, "user", "")
		if err != nil {
			t.Fatalf("compile %q: %v", form, err)
		}
		found := false
		for _, f := range q.Selects[0].Fields {
			if f.FieldName != "max_price" {
				continue
			}
			found = true
			if f.Type != qcode.FieldTypeFunc {
				t.Errorf("%q: Type = %v, want FieldTypeFunc", form, f.Type)
			}
			if len(f.Args) != 1 || f.Args[0].Type != qcode.ArgTypeCol {
				t.Fatalf("%q: Args = %+v, want one ArgTypeCol", form, f.Args)
			}
			if f.Args[0].Col.Name != "price" {
				t.Errorf("%q: Col.Name = %q, want price", form, f.Args[0].Col.Name)
			}
			if !strings.EqualFold(f.Func.Name, "max") {
				t.Errorf("%q: Func.Name = %q, want max", form, f.Func.Name)
			}
		}
		if !found {
			t.Fatalf("%q: max_price field not found", form)
		}
	}
}

func TestColumnArgAggregateRoleBlocked(t *testing.T) {
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{})
	// Role can read id but NOT price.
	if err := qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{Columns: []string{"id"}},
	}); err != nil {
		t.Fatal(err)
	}

	_, err := qc.Compile([]byte(`
		query { products {
			id
			leaked: sum(column: price)
		} }`), nil, "user", "")
	if err == nil {
		t.Fatal("expected compile error for blocked column reference, got nil")
	}
	if !strings.Contains(err.Error(), "price") {
		t.Errorf("error should mention the blocked column, got: %v", err)
	}
}

func TestColumnArgNonAggregateStillErrors(t *testing.T) {
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{})
	if err := qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{Columns: []string{"id", "name", "price"}},
	}); err != nil {
		t.Fatal(err)
	}

	// A non-aggregate function name keeps its historical failure: the column
	// spelling is deliberately confined to genuine aggregates.
	if _, err := qc.Compile([]byte(`query { products { id lower(column: name) } }`), nil, "user", ""); err == nil {
		t.Fatal("lower(column:) must not compile")
	}

	// A nonexistent column names itself in the error.
	_, err := qc.Compile([]byte(`query { products { id max(column: nope) } }`), nil, "user", "")
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("unknown column must error naming it, got: %v", err)
	}

	// DisableAgg blocks the new spelling like every other aggregate form.
	blocked, _ := qcode.NewCompiler(dbs, qcode.Config{DisableAgg: true})
	if err := blocked.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{Columns: []string{"id", "price"}},
	}); err != nil {
		t.Fatal(err)
	}
	_, err = blocked.Compile([]byte(`query { products { id max(column: price) } }`), nil, "user", "")
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("DisableAgg must reject the column form, got: %v", err)
	}
}
