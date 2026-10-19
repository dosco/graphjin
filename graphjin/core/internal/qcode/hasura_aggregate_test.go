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

func TestHasuraAggregateShallowRewrite(t *testing.T) {
	compiler, err := qcode.NewCompiler(dbs, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := compiler.Compile([]byte(`
		query {
			products_aggregate {
				count
				min { price }
			}
		}`), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.HasuraAggregates) != 1 {
		t.Fatalf("Hasura aggregate plans = %d, want 1", len(compiled.HasuraAggregates))
	}
	wantPaths := [][]string{{"count"}, {"min", "price"}}
	for i, field := range compiled.HasuraAggregates[0].Fields {
		if strings.Join(field.Path, ".") != strings.Join(wantPaths[i], ".") {
			t.Fatalf("field %d response path = %#v, want %#v", i, field.Path, wantPaths[i])
		}
	}
	if len(compiled.Selects) != 1 || compiled.Selects[0].Table != "products" || !compiled.Selects[0].GlobalAgg {
		t.Fatalf("lowered aggregate select = %#v", compiled.Selects)
	}
}

func TestHasuraAggregateQualifiedRootHint(t *testing.T) {
	compiler, err := qcode.NewCompiler(dbs, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = compiler.Compile([]byte(`query { app.main.products_aggregate { count } }`), nil, "user", "")
	if err == nil {
		t.Fatal("expected qualified-root parse error")
	}
	want := "roots are unqualified table names: write `products_aggregate`, not `app.main.products_aggregate`"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want substring %q", err, want)
	}
}

func TestHasuraAggregateUnknownRootSuggestsSchemaTable(t *testing.T) {
	schema := hasuraAggregateSchema(t, []sdata.DBColumn{
		{Schema: "public", Table: "support_tickets", Name: "id", Type: "bigint", PrimaryKey: true, NotNull: true},
	})
	compiler, err := qcode.NewCompiler(schema, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = compiler.Compile([]byte(`query { tickets_aggregate { count } }`), nil, "user", "")
	if err == nil || !strings.Contains(err.Error(), `did you mean "support_tickets_aggregate"`) {
		t.Fatalf("error = %v, want support_tickets_aggregate suggestion", err)
	}
}

func TestNativeAggregateWrapperHint(t *testing.T) {
	compiler, err := qcode.NewCompiler(dbs, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = compiler.Compile([]byte(`query { products { aggregate { count } } }`), nil, "user", "")
	if err == nil || !strings.Contains(err.Error(), `aggregates are fields such as "count_id", or use the "products_aggregate" root`) {
		t.Fatalf("error = %v, want native aggregate syntax hint", err)
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
		{"mixed wrapper and shallow", `query { products_aggregate { count aggregate { max { price } } } }`, "cannot be mixed"},
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
