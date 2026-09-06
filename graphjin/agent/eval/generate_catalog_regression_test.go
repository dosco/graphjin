package eval

import (
	"strings"
	"testing"
)

func TestCatalogCandidatesPreserveIntegerAggregatesAndCommonNames(t *testing.T) {
	rows := []CatalogRow{{ID: "table:app:main.orders", Kind: "table", TableName: "orders", DetailsJSON: `[{"ColumnName":"id","Type":"integer","PrimaryKey":true},{"ColumnName":"name","Type":"text"},{"ColumnName":"amount_cents","Type":"integer"}]`}}
	tasks := generateCatalogCandidates(CatalogSnapshot{Rows: rows}, 23)
	for _, field := range []string{"sum_amount_cents", "avg_amount_cents", "name: {is_null: true}"} {
		found := false
		for _, task := range tasks {
			if task.Oracle != nil && strings.Contains(task.Oracle.Query, field) {
				found = true
			}
		}
		if !found {
			t.Errorf("lost valid catalog task containing %q", field)
		}
	}
}
func TestCatalogCandidatesSkipCompositeKeyRankings(t *testing.T) {
	rows := []CatalogRow{{ID: "table:app:main.orders", Kind: "table", TableName: "orders", DetailsJSON: `[{"ColumnName":"order_id","Type":"integer","PrimaryKey":true},{"ColumnName":"line_id","Type":"integer","PrimaryKey":true},{"ColumnName":"name","Type":"text"},{"ColumnName":"amount","Type":"decimal"}]`}}
	tables := catalogTables(rows)
	if len(tables) != 1 || !tables[0].CompositeKey {
		t.Fatalf("composite key metadata missing: %+v", tables)
	}
	for _, task := range generateCatalogCandidates(CatalogSnapshot{Rows: rows}, 23) {
		if task.Category == CategoryRanking {
			t.Errorf("composite key still produces ranking: %s", task.Oracle.Query)
		}
	}
}

func TestCatalogTablesPrimaryKeyRepresentations(t *testing.T) {
	for _, tc := range []struct {
		name, details string
		composite     bool
	}{
		{"sectioned composite", `[{"section":"key_columns","data_json":"{\"columns\":[{\"ColumnName\":\"order_id\",\"Type\":\"integer\",\"PrimaryKey\":true},{\"ColumnName\":\"line_id\",\"Type\":\"integer\",\"PrimaryKey\":true}]}"}]`, true},
		{"explicit composite", `{"primary_keys":["order_id","line_id"]}`, true},
		{"repeated single key", `[{"primary_key":"id"},{"primary_keys":["id"]},{"ColumnName":"id","PrimaryKey":true},{"ColumnName":"id","PrimaryKey":true}]`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tables := catalogTables([]CatalogRow{{ID: "table:orders", Kind: "table", TableName: "orders", DetailsJSON: tc.details}})
			if len(tables) != 1 || tables[0].CompositeKey != tc.composite {
				t.Fatalf("key metadata: %+v", tables)
			}
		})
	}
}

func TestCatalogCandidatesPreserveQuotedTableAndParameterizedTypes(t *testing.T) {
	for _, typ := range []string{"integer", "numeric(18,2)", "decimal(18,2)", "number"} {
		t.Run(typ, func(t *testing.T) {
			rows := []CatalogRow{{ID: "table:order", Kind: "table", TableName: "order", DetailsJSON: []any{
				map[string]any{"ColumnName": "key", "Type": "integer", "PrimaryKey": true},
				map[string]any{"ColumnName": "value", "Type": typ},
			}}}
			tasks := generateCatalogCandidates(CatalogSnapshot{Rows: rows}, 23)
			for _, field := range []string{"count_key", "sum_value", "avg_value"} {
				found := false
				for _, task := range tasks {
					if task.Oracle != nil && strings.Contains(task.Oracle.Query, field) {
						found = true
					}
				}
				if !found {
					t.Errorf("missing %s for %s", field, typ)
				}
			}
		})
	}
}
