package psql

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func TestOracleRepro(t *testing.T) {
	t1 := sdata.DBTable{
		Name:   "users",
		Schema: "public",
		Type:   "table",
		Columns: []sdata.DBColumn{
			{Name: "id", Type: "integer"},
			{Name: "full_name", Type: "text"},
		},
	}
	
	qc := &qcode.QCode{
		Type: qcode.QTQuery,
		Selects: []qcode.Select{
			{
				Field: qcode.Field{
					ID: 0,
				},
				Table: "users",
				Ti:    t1,
				Fields: []qcode.Field{
					{
						ID: 0, 
						Type: qcode.FieldTypeCol,
						Col: t1.Columns[0],
						FieldName: "id",
					},
					{
						ID: 1,
						Type: qcode.FieldTypeCol,
						Col: t1.Columns[1],
						FieldName: "full_name",
					},
				},

				Where: qcode.Filter{
					Exp: &qcode.Exp{
						Op: qcode.OpEquals,
						Left: struct{ID int32; Table string; Col sdata.DBColumn; ColName string; Path []string}{
							Col: t1.Columns[0],
							Table: "users",
							ColName: "id",
						},
						Right: struct{ValType qcode.ValType; Val string; ID int32; Table string; Col sdata.DBColumn; ColName string; ListType qcode.ValType; ListVal []string; Path []string}{
							ValType: qcode.ValNum,
							Val: "2",
						},
					},
				},
			},
		},
		Roots: []int32{0},
	}
	qc.Selects[0].Fields[0].ParentID = -1
	qc.Selects[0].Fields[1].ParentID = -1

	conf := Config{
		DBType: "oracle",
	}
	co := NewCompiler(conf)
	
	var w bytes.Buffer
	md, err := co.Compile(&w, qc)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
	
	sql := w.String()
	fmt.Printf("Generated SQL:\n%s\n", sql)
	_ = md
}
