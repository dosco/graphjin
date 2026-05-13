package qcode_test

import (
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
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

func TestWindow_PartitionAndOrderParsed(t *testing.T) {
	qc := newWindowCompiler(t)

	result, err := qc.Compile([]byte(`
		query {
			products {
				id
				price
				running: sum_price @window(partition: ["user_id"], order: ["created_at desc"], frame: "rows unbounded preceding")
			}
		}`), nil, "user", "")
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	sel := result.Selects[0]

	// Window functions don't trigger GROUP BY — one row per input.
	if sel.GroupCols {
		t.Errorf("expected GroupCols=false for windowed aggregate, got true")
	}
	if sel.GlobalAgg {
		t.Errorf("expected GlobalAgg=false for windowed aggregate, got true")
	}

	var found bool
	for _, f := range sel.Fields {
		if f.FieldName != "running" {
			continue
		}
		found = true
		if f.Window == nil {
			t.Fatalf("expected non-nil Window on field %q", f.FieldName)
		}
		if got := f.Window.Partition; len(got) != 1 || got[0] != "user_id" {
			t.Errorf("expected Partition=[user_id], got %v", got)
		}
		if len(f.Window.OrderBy) != 1 ||
			f.Window.OrderBy[0].Col != "created_at" ||
			!f.Window.OrderBy[0].Desc {
			t.Errorf("expected OrderBy=[{created_at desc}], got %+v", f.Window.OrderBy)
		}
		if !strings.Contains(f.Window.Frame, "ROWS UNBOUNDED PRECEDING") {
			t.Errorf("expected canonical frame, got %q", f.Window.Frame)
		}
	}
	if !found {
		t.Fatalf("did not find field 'running' in compiled select")
	}
}

func TestWindow_RejectsBogusFrame(t *testing.T) {
	cases := []struct {
		frame    string
		wantSubs string
	}{
		// Wrong leading keyword.
		{"between unbounded preceding and current row", "ROWS or RANGE"},
		// Negative offset.
		{"rows between -3 preceding and current row", "non-negative integer"},
		// Junk where a bound should be.
		{"rows between abc and current row", "unrecognised bound"},
		// BETWEEN missing AND.
		{"rows between 5 preceding current row", "BETWEEN"},
	}
	for _, c := range cases {
		qc := newWindowCompiler(t)
		_, err := qc.Compile([]byte(`
			query {
				products {
					running: sum_price @window(partition: ["user_id"], frame: "`+c.frame+`")
				}
			}`), nil, "user", "")
		if err == nil || !strings.Contains(err.Error(), c.wantSubs) {
			t.Errorf("frame=%q: want error containing %q, got: %v", c.frame, c.wantSubs, err)
		}
	}
}

func TestWindow_RejectsUnknownPartitionColumn(t *testing.T) {
	qc := newWindowCompiler(t)

	_, err := qc.Compile([]byte(`
		query {
			products {
				running: sum_price @window(partition: ["bogus_col"])
			}
		}`), nil, "user", "")
	if err == nil || !strings.Contains(err.Error(), "bogus_col") {
		t.Fatalf("expected unknown column error, got: %v", err)
	}
}

func TestWindow_RequiresOnFunctionField(t *testing.T) {
	qc := newWindowCompiler(t)

	_, err := qc.Compile([]byte(`
		query {
			products {
				name @window(partition: ["user_id"])
			}
		}`), nil, "user", "")
	if err == nil || !strings.Contains(err.Error(), "@window") {
		t.Fatalf("expected @window-on-non-function error, got: %v", err)
	}
}

// TestWindow_EmptyDirectiveAllowed: an empty @window emits a bare
// OVER() — valid for ranking functions and others that don't need a
// partition or order.
func TestWindow_EmptyDirectiveAllowed(t *testing.T) {
	qc := newWindowCompiler(t)
	result, err := qc.Compile([]byte(`
		query {
			products {
				total: sum_price @window
			}
		}`), nil, "user", "")
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	for _, f := range result.Selects[0].Fields {
		if f.FieldName == "total" {
			if f.Window == nil {
				t.Fatalf("expected non-nil Window from empty @window")
			}
			if len(f.Window.Partition) != 0 || len(f.Window.OrderBy) != 0 || f.Window.Frame != "" {
				t.Errorf("expected zero-valued WindowSpec, got %+v", f.Window)
			}
			return
		}
	}
	t.Fatalf("did not find field 'total' in compiled select")
}

// TestWindow_NullsHandling drives the order-clause parser through the
// existing Compile() entry point so column validation runs, and asserts
// the parsed WindowSpec carries the right Nulls enum.
func TestWindow_NullsHandling(t *testing.T) {
	cases := []struct {
		dir      string
		wantDesc bool
		wantNull qcode.NullsHandling
	}{
		{`["created_at"]`, false, qcode.NullsDefault},
		{`["created_at desc"]`, true, qcode.NullsDefault},
		{`["created_at nulls first"]`, false, qcode.NullsFirst},
		{`["created_at nulls last"]`, false, qcode.NullsLast},
		{`["created_at desc nulls first"]`, true, qcode.NullsFirst},
		{`["created_at asc nulls last"]`, false, qcode.NullsLast},
	}
	for _, c := range cases {
		qc := newWindowCompiler(t)
		gql := `query {
				products {
					running: sum_price @window(partition: ["user_id"], order: ` + c.dir + `)
				}
			}`
		result, err := qc.Compile([]byte(gql), nil, "user", "")
		if err != nil {
			t.Errorf("compile %s: %v", c.dir, err)
			continue
		}
		var got *qcode.WindowSpec
		for _, f := range result.Selects[0].Fields {
			if f.FieldName == "running" {
				got = f.Window
				break
			}
		}
		if got == nil || len(got.OrderBy) != 1 {
			t.Errorf("%s: window or order missing on field", c.dir)
			continue
		}
		o := got.OrderBy[0]
		if o.Desc != c.wantDesc || o.Nulls != c.wantNull {
			t.Errorf("%s: got desc=%v nulls=%v, want desc=%v nulls=%v",
				c.dir, o.Desc, o.Nulls, c.wantDesc, c.wantNull)
		}
	}
}

// TestWindow_BadNullsHandling rejects malformed order entries early
// (before they reach the SQL renderer).
func TestWindow_BadNullsHandling(t *testing.T) {
	bads := []string{
		`["created_at nulls"]`,       // no FIRST/LAST
		`["created_at nulls maybe"]`, // bogus side
		`["created_at desc nulls"]`,  // trailing
		`["created_at asc desc"]`,    // two directions
	}
	for _, in := range bads {
		qc := newWindowCompiler(t)
		gql := `query {
				products {
					running: sum_price @window(partition: ["user_id"], order: ` + in + `)
				}
			}`
		_, err := qc.Compile([]byte(gql), nil, "user", "")
		if err == nil {
			t.Errorf("compile %s: expected error, got nil", in)
		}
	}
}

func TestWindow_MixedAggregatesAndWindows(t *testing.T) {
	qc := newWindowCompiler(t)

	// A pure aggregate alongside a windowed aggregate: GROUP BY must be
	// triggered by the pure aggregate, while the windowed one rides along.
	result, err := qc.Compile([]byte(`
		query {
			products {
				user_id
				total: sum_price
				running: sum_price @window(partition: ["user_id"], order: ["created_at"])
			}
		}`), nil, "user", "")
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	sel := result.Selects[0]
	if !sel.GroupCols {
		t.Errorf("expected GroupCols=true (pure aggregate forces GROUP BY)")
	}
}

func TestWindow_RankingFunctionsParsed(t *testing.T) {
	cases := []struct {
		field string
		want  qcode.WindowFunc
	}{
		{"row_number", qcode.WindowFuncRowNumber},
		{"rank", qcode.WindowFuncRank},
		{"dense_rank", qcode.WindowFuncDenseRank},
	}
	for _, c := range cases {
		qc := newWindowCompiler(t)
		result, err := qc.Compile([]byte(`
			query {
				products {
					metric: `+c.field+` @window(partition: ["user_id"], order: ["price desc"])
				}
			}`), nil, "user", "")
		if err != nil {
			t.Fatalf("%s: compile failed: %v", c.field, err)
		}
		f := result.Selects[0].Fields[0]
		if f.WindowFunc != c.want {
			t.Errorf("%s: WindowFunc = %v, want %v", c.field, f.WindowFunc, c.want)
		}
		if f.Window == nil {
			t.Fatalf("%s: expected @window spec", c.field)
		}
	}
}

func TestWindow_ValueFunctionsParsed(t *testing.T) {
	cases := []struct {
		field string
		want  qcode.WindowFunc
	}{
		{"lag_price", qcode.WindowFuncLag},
		{"lead_price", qcode.WindowFuncLead},
		{"first_value_price", qcode.WindowFuncFirstValue},
		{"last_value_price", qcode.WindowFuncLastValue},
	}
	for _, c := range cases {
		qc := newWindowCompiler(t)
		result, err := qc.Compile([]byte(`
			query {
				products {
					metric: `+c.field+` @window(partition: ["user_id"], order: ["created_at"])
				}
			}`), nil, "user", "")
		if err != nil {
			t.Fatalf("%s: compile failed: %v", c.field, err)
		}
		f := result.Selects[0].Fields[0]
		if f.WindowFunc != c.want {
			t.Errorf("%s: WindowFunc = %v, want %v", c.field, f.WindowFunc, c.want)
		}
		if len(f.Args) != 1 || f.Args[0].Type != qcode.ArgTypeCol || f.Args[0].Col.Name != "price" {
			t.Errorf("%s: expected price column arg, got %+v", c.field, f.Args)
		}
	}
}

func TestWindow_FunctionRequiresDirective(t *testing.T) {
	qc := newWindowCompiler(t)
	_, err := qc.Compile([]byte(`
		query {
			products {
				row_number
			}
		}`), nil, "user", "")
	if err == nil || !strings.Contains(err.Error(), "requires @window") {
		t.Fatalf("expected requires @window error, got: %v", err)
	}
}

func TestWindow_ValueFunctionRejectsUnknownColumn(t *testing.T) {
	qc := newWindowCompiler(t)
	_, err := qc.Compile([]byte(`
		query {
			products {
				metric: lag_bogus @window(partition: ["user_id"], order: ["created_at"])
			}
		}`), nil, "user", "")
	if err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("expected unknown suffix column error, got: %v", err)
	}
}
