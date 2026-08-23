package core

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func TestApplyOpenAPIQueryUsesPagingVariables(t *testing.T) {
	body, err := applyOpenAPIQuery([]byte(`[{"id":1},{"id":2},{"id":3}]`), ResolverReq{
		Sel: &qcode.Select{Paging: qcode.Paging{LimitVar: "take", OffsetVar: "skip"}},
		Vars: map[string]json.RawMessage{
			"take": json.RawMessage(`1`),
			"skip": json.RawMessage(`1`),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(body), `[{"id":2}]`; got != want {
		t.Fatalf("paged body = %s, want %s", got, want)
	}
}

func TestApplyOpenAPIQueryRejectsUnsupportedArguments(t *testing.T) {
	column := sdata.DBColumn{Name: "id"}
	tests := []struct {
		name string
		sel  *qcode.Select
		want string
	}{
		{
			name: "distinct",
			sel:  &qcode.Select{DistinctOn: []sdata.DBColumn{column}},
			want: "distinct is not supported",
		},
		{
			name: "cursor pagination",
			sel:  &qcode.Select{Paging: qcode.Paging{Cursor: true}},
			want: "cursor pagination is not supported",
		},
		{
			name: "configured order variant",
			sel: &qcode.Select{OrderBy: []qcode.OrderBy{{
				Col: column, Order: qcode.OrderAsc, KeyVar: "sort",
			}}},
			want: "order_by supports response columns only",
		},
		{
			name: "unsupported where operator",
			sel: &qcode.Select{Where: qcode.Filter{Exp: &qcode.Exp{
				Op: qcode.OpTsQuery,
			}}},
			want: "where does not support operator",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := applyOpenAPIQuery([]byte(`[]`), ResolverReq{Sel: tt.sel})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestApplyOpenAPIQueryRejectsColumnReferenceFilter(t *testing.T) {
	ex := &qcode.Exp{Op: qcode.OpEquals}
	ex.Left.Col = sdata.DBColumn{Name: "id"}
	ex.Right.Col = sdata.DBColumn{Name: "other_id"}

	_, err := applyOpenAPIQuery([]byte(`[{}]`), ResolverReq{
		Sel: &qcode.Select{Where: qcode.Filter{Exp: ex}},
	})
	if err == nil || !strings.Contains(err.Error(), "column-reference operands") {
		t.Fatalf("error = %v, want column-reference rejection", err)
	}
}
