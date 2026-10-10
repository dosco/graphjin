package agent

import (
	"slices"
	"testing"

	"github.com/dosco/graphjin/core/v3"
)

// GitHub issue #656: the validator listed only equality operators for
// "timestamp without time zone" columns, so it rejected date-range filters the
// compiler runs fine, and the agent gave up on filtering by date.
func TestValidateWhereClauseAcceptsTimestampRanges(t *testing.T) {
	columns := map[string]core.ColumnInfo{
		"orderdate":   {Name: "orderdate", Type: "timestamp without time zone"},
		"shipdate":    {Name: "shipdate", Type: "timestamp with time zone"},
		"duedate":     {Name: "duedate", Type: "date"},
		"totaldue":    {Name: "totaldue", Type: "numeric(19,4)"},
		"freight":     {Name: "freight", Type: "double precision"},
		"comment":     {Name: "comment", Type: "character varying(128)"},
		"onlineorder": {Name: "onlineorder", Type: "boolean"},
		"tags":        {Name: "tags", Type: "text[]", Array: true},
	}

	valid := []map[string]any{
		{"orderdate": map[string]any{"gte": "2025-10-09"}},
		{"orderdate": map[string]any{"gte": "2025-10-09", "lt": "2026-10-09"}},
		{"shipdate": map[string]any{"greater_than": "2025-01-01"}},
		{"duedate": map[string]any{"lte": "2025-12-31"}},
		{"freight": map[string]any{"gt": 10}},
		{"comment": map[string]any{"ilike": "%rush%"}},
		{"comment": map[string]any{"not_ilike": "%rush%"}},
		{"tags": map[string]any{"in": []any{"a"}}},
		{"onlineorder": map[string]any{"eq": true}},
		{"and": []any{
			map[string]any{"orderdate": map[string]any{"gte": "2025-01-01"}},
			map[string]any{"totaldue": map[string]any{"gt": 100}},
		}},
		// A filter on a related table is left to the compiler.
		{"customer": map[string]any{"territoryid": map[string]any{"eq": 1}}},
	}
	for _, where := range valid {
		if errs := validateWhereClause(where, columns, ""); len(errs) != 0 {
			t.Errorf("validateWhereClause(%v) = %+v, want no errors", where, errs)
		}
	}

	errs := validateWhereClause(map[string]any{"orderdate": map[string]any{"between": []any{"a", "b"}}}, columns, "")
	if len(errs) != 1 || errs[0].Error != "unknown_operator" {
		t.Fatalf("between: got %+v, want one unknown_operator error", errs)
	}
	errs = validateWhereClause(map[string]any{"orderdate": map[string]any{"like": "2025%"}}, columns, "")
	if len(errs) != 1 || errs[0].Error != "invalid_operator" {
		t.Fatalf("like on timestamp: got %+v, want one invalid_operator error", errs)
	}
	errs = validateWhereClause(map[string]any{"missing": map[string]any{"eq": 1}}, columns, "")
	if len(errs) != 1 || errs[0].Error != "unknown_column" {
		t.Fatalf("missing column: got %+v, want one unknown_column error", errs)
	}

	for _, op := range []string{"gt", "gte", "lt", "lte"} {
		if !slices.Contains(validOperators("timestamp without time zone", false), op) {
			t.Errorf("validOperators(timestamp without time zone) is missing %q", op)
		}
	}
}
