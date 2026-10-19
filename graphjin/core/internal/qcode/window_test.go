package qcode_test

import (
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func newWindowCompiler(t *testing.T) *qcode.Compiler {
	t.Helper()
	qc, err := qcode.NewCompiler(dbs, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "name", "price", "user_id", "created_at"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	return qc
}

func TestAnalytics_RunningDirectiveParsed(t *testing.T) {
	qc := newWindowCompiler(t)

	result, err := qc.Compile([]byte(`
		query {
			products {
				id
				price
				running: price @running(aggregate: sum, by: "user_id", orderBy: { created_at: desc })
			}
		}`), nil, "user", "")
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	sel := result.Selects[0]
	if sel.GroupCols {
		t.Errorf("expected GroupCols=false for analytic aggregate, got true")
	}
	if sel.GlobalAgg {
		t.Errorf("expected GlobalAgg=false for analytic aggregate, got true")
	}

	f := findWindowField(t, sel.Fields, "running")
	if f.Type != qcode.FieldTypeFunc {
		t.Fatalf("running.Type = %v, want FieldTypeFunc", f.Type)
	}
	if f.Func.Name != "sum" {
		t.Errorf("running.Func.Name = %q, want sum", f.Func.Name)
	}
	if len(f.Args) != 1 || f.Args[0].Type != qcode.ArgTypeCol || f.Args[0].Col.Name != "price" {
		t.Errorf("expected price column arg, got %+v", f.Args)
	}
	if got := f.Window.Partition; len(got) != 1 || got[0] != "user_id" {
		t.Errorf("expected Partition=[user_id], got %v", got)
	}
	if len(f.Window.OrderBy) != 1 ||
		f.Window.OrderBy[0].Col != "created_at" ||
		!f.Window.OrderBy[0].Desc {
		t.Errorf("expected OrderBy=[{created_at desc}], got %+v", f.Window.OrderBy)
	}
	if f.Window.Frame != "ROWS UNBOUNDED PRECEDING" {
		t.Errorf("Frame = %q, want ROWS UNBOUNDED PRECEDING", f.Window.Frame)
	}
}

func TestAnalytics_MovingDirectiveParsed(t *testing.T) {
	qc := newWindowCompiler(t)

	result, err := qc.Compile([]byte(`
		query {
			products {
				moving_avg: price @moving(aggregate: avg, rows: 6, by: ["user_id"], orderBy: { created_at: asc })
			}
		}`), nil, "user", "")
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	f := findWindowField(t, result.Selects[0].Fields, "moving_avg")
	if f.Func.Name != "avg" {
		t.Errorf("Func.Name = %q, want avg", f.Func.Name)
	}
	if f.Window.Frame != "ROWS BETWEEN 5 PRECEDING AND CURRENT ROW" {
		t.Errorf("Frame = %q", f.Window.Frame)
	}
}

func TestAnalytics_AggregateChoicesAndOptionalBy(t *testing.T) {
	for _, agg := range []string{"sum", "avg", "count", "min", "max"} {
		qc := newWindowCompiler(t)
		result, err := qc.Compile([]byte(`
			query {
				products {
					metric: price @running(aggregate: `+agg+`, order: asc)
				}
			}`), nil, "user", "")
		if err != nil {
			t.Fatalf("@running aggregate %s: compile failed: %v", agg, err)
		}
		f := findWindowField(t, result.Selects[0].Fields, "metric")
		if f.Func.Name != agg {
			t.Errorf("@running aggregate %s Func.Name = %q", agg, f.Func.Name)
		}
		if len(f.Window.Partition) != 0 {
			t.Errorf("@running aggregate %s Partition = %v, want none", agg, f.Window.Partition)
		}
		if len(f.Window.OrderBy) != 1 ||
			f.Window.OrderBy[0].Col != "price" ||
			f.Window.OrderBy[0].Desc {
			t.Errorf("@running aggregate %s OrderBy = %+v, want price asc", agg, f.Window.OrderBy)
		}
	}
}

func TestAnalytics_ValueDirectivesParsed(t *testing.T) {
	cases := []struct {
		dir       string
		want      qcode.WindowFunc
		wantFrame string
	}{
		{"previous", qcode.WindowFuncLag, ""},
		{"next", qcode.WindowFuncLead, ""},
		{"first", qcode.WindowFuncFirstValue, "ROWS BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING"},
		{"last", qcode.WindowFuncLastValue, "ROWS BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING"},
	}
	for _, c := range cases {
		qc := newWindowCompiler(t)
		result, err := qc.Compile([]byte(`
			query {
				products {
					metric: price @`+c.dir+`(by: "user_id", orderBy: { created_at: asc })
				}
			}`), nil, "user", "")
		if err != nil {
			t.Fatalf("@%s: compile failed: %v", c.dir, err)
		}
		f := findWindowField(t, result.Selects[0].Fields, "metric")
		if f.WindowFunc != c.want {
			t.Errorf("@%s WindowFunc = %v, want %v", c.dir, f.WindowFunc, c.want)
		}
		if len(f.Args) != 1 || f.Args[0].Col.Name != "price" {
			t.Errorf("@%s expected price column arg, got %+v", c.dir, f.Args)
		}
		if f.Window.Frame != c.wantFrame {
			t.Errorf("@%s Frame = %q, want %q", c.dir, f.Window.Frame, c.wantFrame)
		}
	}
}

func TestAnalytics_RankingDirectivesParsed(t *testing.T) {
	cases := []struct {
		dir  string
		arg  string
		want qcode.WindowFunc
	}{
		{"rank", `order: desc`, qcode.WindowFuncRank},
		{"denseRank", `order: desc`, qcode.WindowFuncDenseRank},
		{"rowNumber", `orderBy: { created_at: asc }`, qcode.WindowFuncRowNumber},
	}
	for _, c := range cases {
		qc := newWindowCompiler(t)
		result, err := qc.Compile([]byte(`
			query {
				products {
					metric: price @`+c.dir+`(by: "user_id", `+c.arg+`)
				}
			}`), nil, "user", "")
		if err != nil {
			t.Fatalf("@%s: compile failed: %v", c.dir, err)
		}
		f := findWindowField(t, result.Selects[0].Fields, "metric")
		if f.WindowFunc != c.want {
			t.Errorf("@%s WindowFunc = %v, want %v", c.dir, f.WindowFunc, c.want)
		}
		if f.Window == nil || len(f.Window.OrderBy) != 1 {
			t.Fatalf("@%s expected window order, got %+v", c.dir, f.Window)
		}
	}
}

func TestAnalytics_MixedAggregatesAndAnalytics(t *testing.T) {
	qc := newWindowCompiler(t)

	result, err := qc.Compile([]byte(`
		query {
			products {
				user_id
				total: sum_price
				running: price @running(aggregate: sum, by: "user_id", orderBy: { created_at: asc })
			}
		}`), nil, "user", "")
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if !result.Selects[0].GroupCols {
		t.Errorf("expected GroupCols=true because pure aggregate still forces grouping")
	}
}

func TestAnalytics_RejectsOldWindowDirective(t *testing.T) {
	qc := newWindowCompiler(t)
	_, err := qc.Compile([]byte(`
		query {
			products {
				running: sum_price @window(partition: ["user_id"], order: ["created_at"], frame: "rows unbounded preceding")
			}
		}`), nil, "user", "")
	if err == nil || !strings.Contains(err.Error(), "@window has been replaced") {
		t.Fatalf("expected @window replacement error, got: %v", err)
	}
}

func TestAnalytics_RejectsInternalWindowFunctionNames(t *testing.T) {
	cases := []string{
		"row_number",
		"rank",
		"dense_rank",
		"lag_price",
		"lead_price",
		"first_value_price",
		"last_value_price",
	}
	for _, field := range cases {
		qc := newWindowCompiler(t)
		_, err := qc.Compile([]byte(`
			query {
				products {
					`+field+`
				}
			}`), nil, "user", "")
		if err == nil || !strings.Contains(err.Error(), "analytics") {
			t.Errorf("%s: expected analytics directive error, got %v", field, err)
		}
	}
}

func TestAnalytics_InternalNamesDoNotShadowColumns(t *testing.T) {
	cols := []sdata.DBColumn{
		{Schema: "public", Table: "metrics", Name: "id", Type: "bigint", PrimaryKey: true},
		{Schema: "public", Table: "metrics", Name: "rank", Type: "integer"},
		{Schema: "public", Table: "metrics", Name: "row_number", Type: "integer"},
		{Schema: "public", Table: "metrics", Name: "dense_rank", Type: "integer"},
		{Schema: "public", Table: "metrics", Name: "lag_price", Type: "numeric"},
		{Schema: "public", Table: "metrics", Name: "lead_price", Type: "numeric"},
		{Schema: "public", Table: "metrics", Name: "first_value_price", Type: "numeric"},
		{Schema: "public", Table: "metrics", Name: "last_value_price", Type: "numeric"},
	}
	schema, err := sdata.NewDBSchema(sdata.NewDBInfo("postgres", 140000, "public", "db", cols, nil, nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	qc, err := qcode.NewCompiler(schema, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := qc.AddRole("user", "public", "metrics", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "rank", "row_number", "dense_rank", "lag_price", "lead_price", "first_value_price", "last_value_price"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	result, err := qc.Compile([]byte(`
		query {
			metrics {
				id
				rank
				row_number
				dense_rank
				lag_price
				lead_price
				first_value_price
				last_value_price
			}
		}`), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range result.Selects[0].Fields {
		if f.Type != qcode.FieldTypeCol {
			t.Fatalf("field %q compiled as %v, want column", f.FieldName, f.Type)
		}
	}
}

func TestAnalytics_RejectsSQLShapedArgs(t *testing.T) {
	cases := []struct {
		name string
		gql  string
		want string
	}{
		{
			name: "string order entry",
			gql:  `{ products { running: price @running(aggregate: sum, by: "user_id", orderBy: ["created_at desc"]) } }`,
			want: "orderBy must be an object",
		},
		{
			name: "sql frame string",
			gql:  `{ products { running: price @running(aggregate: sum, by: "user_id", orderBy: { created_at: asc }, frame: "rows unbounded preceding") } }`,
			want: "unknown arg",
		},
		{
			name: "nulls in shorthand order",
			gql:  `{ products { running: price @running(aggregate: sum, by: "user_id", order: "created_at desc nulls last") } }`,
			want: "asc or desc",
		},
	}
	for _, c := range cases {
		qc := newWindowCompiler(t)
		_, err := qc.Compile([]byte(`query `+c.gql), nil, "user", "")
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want error containing %q, got %v", c.name, c.want, err)
		}
	}
}

func TestAnalytics_ValidationErrors(t *testing.T) {
	cases := []struct {
		name string
		gql  string
		want string
	}{
		{
			name: "unknown by column",
			gql:  `{ products { running: price @running(aggregate: sum, by: "bogus", orderBy: { created_at: asc }) } }`,
			want: "bogus",
		},
		{
			name: "unknown order column",
			gql:  `{ products { running: price @running(aggregate: sum, by: "user_id", orderBy: { bogus: asc }) } }`,
			want: "bogus",
		},
		{
			name: "bad aggregate",
			gql:  `{ products { running: price @running(aggregate: median, by: "user_id", orderBy: { created_at: asc }) } }`,
			want: "aggregate",
		},
		{
			name: "missing rows",
			gql:  `{ products { moving: price @moving(aggregate: avg, by: "user_id", orderBy: { created_at: asc }) } }`,
			want: "rows",
		},
		{
			name: "nonpositive rows",
			gql:  `{ products { moving: price @moving(aggregate: avg, rows: 0, by: "user_id", orderBy: { created_at: asc }) } }`,
			want: "positive integer",
		},
		{
			name: "missing order",
			gql:  `{ products { running: price @running(aggregate: sum, by: "user_id") } }`,
			want: "requires orderBy",
		},
		{
			name: "old window function without directive",
			gql:  `{ products { row_number } }`,
			want: "analytics directives",
		},
	}
	for _, c := range cases {
		qc := newWindowCompiler(t)
		_, err := qc.Compile([]byte(`query `+c.gql), nil, "user", "")
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want error containing %q, got %v", c.name, c.want, err)
		}
	}
}

func findWindowField(t *testing.T, fields []qcode.Field, name string) qcode.Field {
	t.Helper()
	for _, f := range fields {
		if f.FieldName == name {
			if f.Window == nil {
				t.Fatalf("field %q has nil Window", name)
			}
			return f
		}
	}
	t.Fatalf("field %q not found in %+v", name, fields)
	return qcode.Field{}
}
