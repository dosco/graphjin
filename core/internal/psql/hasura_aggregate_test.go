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
