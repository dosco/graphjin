package psql

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func TestOracleSchemaRepro(t *testing.T) {
	t1 := sdata.DBTable{
		Name:   "products",
		Schema: "public",
		Type:   "table",
		Columns: []sdata.DBColumn{
			{Name: "id", Type: "integer"},
			{Name: "name", Type: "text"},
		},
	}
	
	qc := &qcode.QCode{
		Type: qcode.QTQuery,
		Selects: []qcode.Select{
			{
				Field: qcode.Field{
					ID: 0,
				},
				Table: "products",
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
						Col: t1.Columns[1], // name
						FieldName: "name",
					},
				},
			},
		},
		Roots: []int32{0},
	}
    // Set parent IDs
	qc.Selects[0].Fields[0].ParentID = -1
	qc.Selects[0].Fields[1].ParentID = -1

	// EnableSchema doesn't change the compiler itself much effectively, 
	// typically it's the GraphJin wrapper that does checking.
	// But let's check if the Compiler relies on config properties that might change SQL.
	conf := Config{
		DBType: "oracle",
		// EnableSchema isn't a field in psql.Config directly, it's used higher up.
		// However, TestEnableSchema generates a query.
		// Let's assume the query structure is standard.
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
