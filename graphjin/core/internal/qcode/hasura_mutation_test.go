package qcode_test

import (
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
)

// Models write Hasura mutations constantly — across 2043 stored agent episodes
// insert_X appeared 28 times, update_X 20, _set 18, pk_columns 7 — and every
// one of them used to die at the root. These pin the lowering that makes them
// run, and the refusals that keep an unsupported shape from being guessed at.

func compileMutation(t *testing.T, query string) (*qcode.QCode, error) {
	t.Helper()
	compiler, err := qcode.NewCompiler(dbs, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}
	return compiler.Compile([]byte(query), nil, "user", "")
}

func TestHasuraUpdateRewrite(t *testing.T) {
	compiled, err := compileMutation(t, `
		mutation {
			update_products(where: { id: { _eq: 1 } }, _set: { name: "a" }) {
				returning { id name }
				affected_rows
			}
		}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.HasuraMutations) != 1 {
		t.Fatalf("Hasura mutation plans = %d, want 1", len(compiled.HasuraMutations))
	}
	plan := compiled.HasuraMutations[0]
	if plan.ResponseKey != "update_products" || !plan.Returning || !plan.AffectedRows || plan.Single {
		t.Fatalf("plan = %#v", plan)
	}
	if len(compiled.Selects) != 1 || compiled.Selects[0].Table != "products" {
		t.Fatalf("lowered select = %#v", compiled.Selects)
	}
	// The returning wrapper is hoisted: its columns become the root selection.
	if len(compiled.Selects[0].Fields) != 2 {
		t.Fatalf("lowered fields = %#v, want id and name", compiled.Selects[0].Fields)
	}
}

func TestHasuraInsertRewriteBothSpellings(t *testing.T) {
	for _, arg := range []string{"objects", "object"} {
		compiled, err := compileMutation(t, `
			mutation {
				insert_products(`+arg+`: { id: 1, name: "a" }) {
					returning { id }
				}
			}`)
		if err != nil {
			t.Fatalf("%s: %v", arg, err)
		}
		if len(compiled.HasuraMutations) != 1 || !compiled.HasuraMutations[0].Returning {
			t.Fatalf("%s: plans = %#v", arg, compiled.HasuraMutations)
		}
		if len(compiled.Selects) != 1 || compiled.Selects[0].Table != "products" {
			t.Fatalf("%s: lowered select = %#v", arg, compiled.Selects)
		}
	}
}

func TestHasuraByPKRewriteBecomesAPrimaryKeyFilter(t *testing.T) {
	compiled, err := compileMutation(t, `
		mutation {
			update_products_by_pk(pk_columns: { id: 3 }, _set: { name: "a" }) { id name }
		}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.HasuraMutations) != 1 || !compiled.HasuraMutations[0].Single {
		t.Fatalf("plans = %#v", compiled.HasuraMutations)
	}
	if len(compiled.Selects) != 1 || compiled.Selects[0].Table != "products" {
		t.Fatalf("lowered select = %#v", compiled.Selects)
	}
	// pk_columns became a real where clause, so the write touches one row.
	if compiled.Selects[0].Where.Exp == nil {
		t.Fatalf("pk_columns must lower to a where filter: %#v", compiled.Selects[0].Where)
	}
}

func TestHasuraDeleteRewrite(t *testing.T) {
	compiled, err := compileMutation(t, `
		mutation { delete_products(where: { id: { _eq: 1 } }) { returning { id } } }`)
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.HasuraMutations) != 1 {
		t.Fatalf("plans = %#v", compiled.HasuraMutations)
	}
	if compiled.SType != qcode.QTDelete {
		t.Fatalf("lowered mutation type = %v, want delete", compiled.SType)
	}
}

// A write that asks only for a count still has to return something to count.
func TestHasuraAffectedRowsOnlySelectionCompiles(t *testing.T) {
	compiled, err := compileMutation(t, `
		mutation { update_products(where: { id: { _eq: 1 } }, _set: { name: "a" }) { affected_rows } }`)
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.HasuraMutations) != 1 || !compiled.HasuraMutations[0].AffectedRows {
		t.Fatalf("plans = %#v", compiled.HasuraMutations)
	}
	if len(compiled.Selects) != 1 || len(compiled.Selects[0].Fields) == 0 {
		t.Fatalf("a synthesised selection is required to count rows: %#v", compiled.Selects)
	}
}

// Native mutations are untouched by any of this.
func TestNativeMutationIsNotRewritten(t *testing.T) {
	compiled, err := compileMutation(t, `
		mutation { products(where: { id: { eq: 1 } }, update: { name: "a" }) { id } }`)
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.HasuraMutations) != 0 {
		t.Fatalf("a native mutation must record no compatibility plan: %#v", compiled.HasuraMutations)
	}
}

// Every shape the lowering does not implement is refused by name. For writes
// this matters more than for reads: a mis-lowered where or _set writes the
// wrong columns to the wrong rows.
func TestHasuraMutationRefusesUnsupportedShapes(t *testing.T) {
	for name, tc := range map[string]struct{ query, want string }{
		"on_conflict": {
			`mutation { insert_products(objects: { id: 1 }, on_conflict: { constraint: products_pkey }) { returning { id } } }`,
			"on_conflict",
		},
		"unknown underscore argument": {
			`mutation { update_products(where: { id: { _eq: 1 } }, _inc: { price: 1 }) { returning { id } } }`,
			"_inc",
		},
		"pk_columns on a non-pk root": {
			`mutation { update_products(pk_columns: { id: 1 }, _set: { name: "a" }) { id } }`,
			"pk_columns",
		},
		"where and pk_columns together": {
			`mutation { update_products_by_pk(where: { id: { _eq: 1 } }, pk_columns: { id: 1 }, _set: { name: "a" }) { id } }`,
			"pk_columns",
		},
		"pk_columns naming an unknown column": {
			`mutation { update_products_by_pk(pk_columns: { nope: 1 }, _set: { name: "a" }) { id } }`,
			"nope",
		},
		"update input on an insert root": {
			`mutation { insert_products(_set: { name: "a" }) { returning { id } } }`,
			"_set",
		},
		"missing input": {
			`mutation { update_products(where: { id: { _eq: 1 } }) { returning { id } } }`,
			"_set",
		},
		"unknown table": {
			`mutation { update_widgets(where: { id: { _eq: 1 } }, _set: { name: "a" }) { returning { id } } }`,
			"widgets",
		},
		// A recorded episode wrote update_tickets_by_pk against a schema whose
		// table is support_tickets; a near miss must name the real root.
		"near-miss table suggests the real root": {
			`mutation { update_purchase_by_pk(pk_columns: { id: 3 }, _set: { quantity: 1 }) { id } }`,
			"did you mean",
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := compileMutation(t, tc.query)
			if err == nil {
				t.Fatalf("unsupported shape must fail loudly, not be guessed at")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("the error must name %q: %v", tc.want, err)
			}
		})
	}
}
