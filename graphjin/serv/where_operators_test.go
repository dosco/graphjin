package serv

import (
	"slices"
	"testing"

	"github.com/dosco/graphjin/core/v3"
)

// GitHub issue #656 and the validator audit that followed it: the advisory
// validator must not reject filters the compiler runs.
func TestValidateWhereClauseOperatorFamilies(t *testing.T) {
	columns := map[string]core.ColumnInfo{
		"orderdate": {Name: "orderdate", Type: "timestamp without time zone"},
		"shipdate":  {Name: "shipdate", Type: "timestamptz"},
		"duedate":   {Name: "duedate", Type: "datetime2"},
		"location":  {Name: "location", Type: "geometry(Point,4326)"},
		"payload":   {Name: "payload", Type: "jsonb"},
		"tags":      {Name: "tags", Type: "text[]", Array: true},
		"active":    {Name: "active", Type: "tinyint(1)"},
		"name":      {Name: "name", Type: "character varying(64)"},
		"total":     {Name: "total", Type: "numeric(19,4)"},
	}

	valid := []map[string]any{
		{"orderdate": map[string]any{"gte": "2025-10-09"}},
		{"orderdate": map[string]any{"gte": "2025-10-09", "lt": "2026-10-09"}},
		{"shipdate": map[string]any{"greater_than": "2025-01-01"}},
		{"duedate": map[string]any{"nin": []any{"2025-01-01"}}},
		{"location": map[string]any{"st_dwithin": map[string]any{"point": []any{1, 2}, "distance": 10}}},
		{"tags": map[string]any{"in": []any{"a"}}},
		{"active": map[string]any{"in": []any{true}}},
		{"name": map[string]any{"gt": "m"}},
		{"name": map[string]any{"distinct": "x"}},
		{"name": map[string]any{"not_ilike": "%x%"}},
		{"total": map[string]any{"gt": "100.50"}},
	}
	for _, where := range valid {
		if errs := validateWhereClause(where, columns, ""); len(errs) != 0 {
			t.Errorf("validateWhereClause(%v) = %+v, want no errors", where, errs)
		}
	}

	invalid := []struct {
		where map[string]any
		code  string
	}{
		{map[string]any{"orderdate": map[string]any{"between": []any{"a", "b"}}}, "unknown_operator"},
		{map[string]any{"orderdate": map[string]any{"like": "2025%"}}, "invalid_operator"},
		{map[string]any{"active": map[string]any{"gt": true}}, "invalid_operator"},
		{map[string]any{"total": map[string]any{"gt": "lots"}}, "type_mismatch"},
		{map[string]any{"name": map[string]any{"nlike": 5}}, "type_mismatch"},
	}
	for _, c := range invalid {
		errs := validateWhereClause(c.where, columns, "")
		if len(errs) != 1 || errs[0].Error != c.code {
			t.Errorf("validateWhereClause(%v) = %+v, want one %s error", c.where, errs, c.code)
		}
	}
}

func TestGenerateAggregationsByType(t *testing.T) {
	agg := generateAggregations(&core.TableSchema{
		Name: "orders",
		Columns: []core.ColumnInfo{
			{Name: "id", Type: "bigint", PrimaryKey: true},
			{Name: "customer_id", Type: "integer", ForeignKey: "public.customers.id"},
			{Name: "total", Type: "numeric(19,4)"},
			{Name: "orderdate", Type: "timestamp without time zone"},
			{Name: "location", Type: "geometry(Point,4326)"},
			{Name: "flag", Type: "tinyint(1)"},
		},
	})
	for _, want := range []string{"sum_total", "avg_total", "min_total", "max_total", "min_orderdate", "max_orderdate", "count_location"} {
		if !slices.Contains(agg.Available, want) {
			t.Errorf("Available is missing %q: %v", want, agg.Available)
		}
	}
	for _, unwanted := range []string{"sum_id", "avg_customer_id", "sum_location", "sum_flag", "sum_orderdate"} {
		if slices.Contains(agg.Available, unwanted) {
			t.Errorf("Available should not list %q: %v", unwanted, agg.Available)
		}
	}
}
