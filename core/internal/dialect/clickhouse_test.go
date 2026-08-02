package dialect

import (
	"reflect"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func TestClickHouseCollectFieldsGroupsCompilerBaseColumns(t *testing.T) {
	nameCol := sdata.DBColumn{Name: "name"}
	sel := &qcode.Select{
		GroupCols: true,
		Fields: []qcode.Field{
			{Type: qcode.FieldTypeCol, Col: nameCol, FieldName: "name"},
			{Type: qcode.FieldTypeFunc, Func: sdata.DBFunction{Name: "count", Agg: true}, FieldName: "count_id"},
		},
		BCols: []qcode.Column{{Col: nameCol}},
	}

	node := &chNode{}
	if err := (&chBuilder{}).collectFields(sel, node); err != nil {
		t.Fatal(err)
	}
	if want := []string{"name"}; !reflect.DeepEqual(node.GroupBy, want) {
		t.Fatalf("group_by = %v, want %v", node.GroupBy, want)
	}
	if len(node.Aggregates) != 1 || node.Aggregates[0].Alias != "count_id" {
		t.Fatalf("aggregates = %+v", node.Aggregates)
	}
}

func TestClickHouseCollectFieldsKeepsGlobalAggregateUngrouped(t *testing.T) {
	sel := &qcode.Select{
		GroupCols: true,
		GlobalAgg: true,
		Fields: []qcode.Field{
			{Type: qcode.FieldTypeFunc, Func: sdata.DBFunction{Name: "count", Agg: true}, FieldName: "count_id"},
		},
	}

	node := &chNode{}
	if err := (&chBuilder{}).collectFields(sel, node); err != nil {
		t.Fatal(err)
	}
	if len(node.GroupBy) != 0 {
		t.Fatalf("global aggregate group_by = %v, want none", node.GroupBy)
	}
}
