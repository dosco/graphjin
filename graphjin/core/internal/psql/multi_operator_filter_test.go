package psql_test

import "testing"

// A range on one column must keep both bounds, including inside a boolean
// expression, a relationship filter, or the Hasura aggregate compatibility form.
func TestMultiOperatorFilterMatchesExplicitAndAcrossDialects(t *testing.T) {
	cases := []struct{ name, compact, explicit string }{
		{"literals", `{ products(where: { price: { gte: 10, lte: 20 } }) { id } }`,
			`{ products(where: { and: [{ price: { gte: 10 } }, { price: { lte: 20 } }] }) { id } }`},
		{"variables", `query($from: String!, $to: String!) { products(where: { price: { _gte: $from, _lte: $to } }) { count_id } }`,
			`query($from: String!, $to: String!) { products(where: { and: [{ price: { _gte: $from } }, { price: { _lte: $to } }] }) { count_id } }`},
		{"aggregate", `{ products_aggregate(where: { price: { gte: 10, lte: 20 } }) { aggregate { count } } }`,
			`{ products_aggregate(where: { and: [{ price: { gte: 10 } }, { price: { lte: 20 } }] }) { aggregate { count } } }`},
		{"or", `{ products(where: { or: [{ price: { gt: 10, lt: 20 } }, { id: { eq: 1 } }] }) { id } }`,
			`{ products(where: { or: [{ and: [{ price: { gt: 10 } }, { price: { lt: 20 } }] }, { id: { eq: 1 } }] }) { id } }`},
		{"relationship", `{ users(where: { products: { price: { gte: 10, lte: 20 } } }) { id } }`,
			`{ users(where: { products: { and: [{ price: { gte: 10 } }, { price: { lte: 20 } }] } }) { id } }`},
	}
	for _, dialect := range []string{"postgres", "mysql", "sqlite", "mariadb", "oracle", "mssql", "mongodb"} {
		for _, tc := range cases {
			t.Run(dialect+"/"+tc.name, func(t *testing.T) {
				qc, pc := newDialectCompilers(t, dialect)
				want := compileWith(t, qc, pc, tc.explicit)
				got := compileWith(t, qc, pc, tc.compact)
				if got != want {
					t.Fatalf("range SQL differs from explicit AND\ncompact:\n%s\nexplicit:\n%s", got, want)
				}
			})
		}
	}
}
