package psql_test

import (
	"encoding/json"
	"testing"
)

func TestHasuraAggregateSQLMatchesNativeGraphJin(t *testing.T) {
	variables := map[string]json.RawMessage{"minimum": json.RawMessage(`10`)}
	compat := compileGQLToPSQLString(t, `
		query Totals($minimum: Int!) {
			products_aggregate(where: {price: {gte: $minimum}}, limit: 1) {
				aggregate { count sum { price } max { created_at } }
			}
		}`, variables, "user")
	native := compileGQLToPSQLString(t, `
		query Totals($minimum: Int!) {
			products_aggregate: products(where: {price: {gte: $minimum}}, limit: 1) {
				count_id sum_price max_created_at
			}
		}`, variables, "user")
	if compat != native {
		t.Fatalf("compatibility SQL differs from native GraphJin SQL\ncompat:\n%s\nnative:\n%s", compat, native)
	}
}

func TestShallowHasuraAggregateSQLMatchesWrappedAndNative(t *testing.T) {
	variables := map[string]json.RawMessage{"minimum": json.RawMessage(`10`)}
	shallow := compileGQLToPSQLString(t, `
		query Totals($minimum: Int!) {
			products_aggregate(where: {price: {gte: $minimum}}, limit: 1) {
				count sum { price } max { created_at }
			}
		}`, variables, "user")
	wrapped := compileGQLToPSQLString(t, `
		query Totals($minimum: Int!) {
			products_aggregate(where: {price: {gte: $minimum}}, limit: 1) {
				aggregate { count sum { price } max { created_at } }
			}
		}`, variables, "user")
	native := compileGQLToPSQLString(t, `
		query Totals($minimum: Int!) {
			products_aggregate: products(where: {price: {gte: $minimum}}, limit: 1) {
				count_id sum_price max_created_at
			}
		}`, variables, "user")
	if shallow != wrapped || shallow != native {
		t.Fatalf("aggregate dialect SQL differs\nshallow:\n%s\nwrapped:\n%s\nnative:\n%s", shallow, wrapped, native)
	}
}

// TestColumnArgAggregateSQLMatchesNative pins the `<agg>(column: <col>)`
// spelling to byte-identical SQL with the prefix form it aliases — the
// compiler builds the same Function shape, so the renderer must not be able
// to tell the two apart.
func TestColumnArgAggregateSQLMatchesNative(t *testing.T) {
	columnForm := compileGQLToPSQLString(t, `
		query {
			products(limit: 1) {
				count_id: count(column: id)
				max_price: max(column: price)
			}
		}`, nil, "user")
	native := compileGQLToPSQLString(t, `
		query {
			products(limit: 1) {
				count_id
				max_price
			}
		}`, nil, "user")
	if columnForm != native {
		t.Fatalf("column-arg SQL differs from native prefix SQL\ncolumn form:\n%s\nnative:\n%s", columnForm, native)
	}
}
