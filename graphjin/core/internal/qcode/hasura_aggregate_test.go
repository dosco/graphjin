package qcode_test

import (
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func TestHasuraAggregateRewrite(t *testing.T) {
	compiler, err := qcode.NewCompiler(dbs, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := compiler.Compile([]byte(`
		query Totals($minimum: Int!) {
			stats: products_aggregate(where: {price: {gte: $minimum}}, limit: 1) {
				aggregate {
					count
					sum { price }
					max { price created_at }
				}
			}
		}`), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.HasuraAggregates) != 1 {
		t.Fatalf("Hasura aggregate plans = %d, want 1", len(compiled.HasuraAggregates))
	}
	plan := compiled.HasuraAggregates[0]
	if plan.ResponseKey != "stats" {
		t.Fatalf("response key = %q, want stats", plan.ResponseKey)
	}
	if len(plan.Fields) != 4 {
		t.Fatalf("aggregate fields = %#v, want 4", plan.Fields)
	}
	if len(compiled.Selects) != 1 || compiled.Selects[0].Table != "products" || compiled.Selects[0].FieldName != "stats" {
		t.Fatalf("lowered select = %#v", compiled.Selects)
	}
	if len(compiled.Selects[0].Fields) != 4 || !compiled.Selects[0].GlobalAgg {
		t.Fatalf("lowered aggregate select = %#v", compiled.Selects[0])
	}
}

func TestHasuraAggregateCountUsesNonNullFallback(t *testing.T) {
	schema := hasuraAggregateSchema(t, []sdata.DBColumn{
		{Schema: "public", Table: "events", Name: "nullable_value", Type: "text"},
		{Schema: "public", Table: "events", Name: "stable_value", Type: "text", NotNull: true},
	})
	compiler, err := qcode.NewCompiler(schema, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := compiler.Compile([]byte(`query { events_aggregate { aggregate { count } } }`), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := compiled.HasuraAggregates[0].Fields[0].NativeField; got != "count_stable_value" {
		t.Fatalf("native count field = %q, want count_stable_value", got)
	}
}

func TestHasuraAggregateCountRequiresCountableColumn(t *testing.T) {
	schema := hasuraAggregateSchema(t, []sdata.DBColumn{
		{Schema: "public", Table: "events", Name: "nullable_value", Type: "text"},
	})
	compiler, err := qcode.NewCompiler(schema, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = compiler.Compile([]byte(`query { events_aggregate { aggregate { count } } }`), nil, "user", "")
	if err == nil || !strings.Contains(err.Error(), "no primary key or non-null column") {
		t.Fatalf("error = %v, want countable-column error", err)
	}
}

func TestHasuraAggregateRealTableCollisionWins(t *testing.T) {
	columns := []sdata.DBColumn{
		{Schema: "public", Table: "events", Name: "id", Type: "bigint", PrimaryKey: true, NotNull: true},
		{Schema: "public", Table: "events_aggregate", Name: "id", Type: "bigint", PrimaryKey: true, NotNull: true},
	}
	schema := hasuraAggregateSchema(t, columns)
	compiler, err := qcode.NewCompiler(schema, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := compiler.Compile([]byte(`query { events_aggregate { id } }`), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.HasuraAggregates) != 0 || len(compiled.Selects) != 1 || compiled.Selects[0].Table != "events_aggregate" {
		t.Fatalf("real-table collision was rewritten: %#v", compiled)
	}
}

func TestHasuraAggregateUnsupportedShapes(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  string
	}{
		{"nodes", `query { products_aggregate { nodes { id } aggregate { count } } }`, "nodes is not supported"},
		{"count args", `query { products_aggregate { aggregate { count(columns: [id], distinct: true) } } }`, "count arguments"},
		{"aggregate alias", `query { products_aggregate { totals: aggregate { count } } }`, "alias on aggregate"},
		{"inner alias", `query { products_aggregate { aggregate { total: count } } }`, "is not supported"},
		{"column alias", `query { products_aggregate { aggregate { max { latest: created_at } } } }`, "is not supported"},
		{"unknown aggregate", `query { products_aggregate { aggregate { median { price } } } }`, "aggregate field \"median\" is unsupported"},
		{"subscription", `subscription { products_aggregate { aggregate { count } } }`, "subscription roots are not supported"},
	}
	compiler, err := qcode.NewCompiler(dbs, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := compiler.Compile([]byte(tt.query), nil, "user", "")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want substring %q", err, tt.want)
			}
			if !strings.Contains(err.Error(), "Supported form:") || !strings.Contains(err.Error(), "Native equivalent:") {
				t.Fatalf("error lacks actionable syntax guidance: %v", err)
			}
		})
	}
}

func hasuraAggregateSchema(t *testing.T, columns []sdata.DBColumn) *sdata.DBSchema {
	t.Helper()
	info := sdata.NewDBInfo("postgres", 160000, "public", "test", columns, nil, nil)
	schema, err := sdata.NewDBSchema(info, nil)
	if err != nil {
		t.Fatal(err)
	}
	return schema
}
