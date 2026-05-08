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

func TestWindow_RejectsUnknownFrame(t *testing.T) {
	qc := newWindowCompiler(t)

	_, err := qc.Compile([]byte(`
		query {
			products {
				running: sum_price @window(partition: ["user_id"], frame: "rows between 0 preceding and current row")
			}
		}`), nil, "user", "")
	if err == nil || !strings.Contains(err.Error(), "frame") {
		t.Fatalf("expected frame allowlist error, got: %v", err)
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

func TestWindow_RequiresAtLeastOneArg(t *testing.T) {
	qc := newWindowCompiler(t)

	_, err := qc.Compile([]byte(`
		query {
			products {
				running: sum_price @window
			}
		}`), nil, "user", "")
	if err == nil || !strings.Contains(err.Error(), "partition") {
		t.Fatalf("expected partition-or-order requirement error, got: %v", err)
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
