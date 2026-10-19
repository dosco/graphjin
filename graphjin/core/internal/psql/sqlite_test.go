package psql

import (
	"bytes"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func TestSQLiteGeneration(t *testing.T) {
	schema, err := sdata.GetTestSchema()
	if err != nil {
		t.Fatal(err)
	}

	qcCompiler, err := qcode.NewCompiler(schema, qcode.Config{DBSchema: schema.DBSchema()})
	if err != nil {
		t.Fatal(err)
	}

	err = qcCompiler.AddRole("user", "public", "users", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "full_name", "email", "products"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	gql := `query {
		users {
			id
			products {
				name
			}
		}
	}`

	qc, err := qcCompiler.Compile([]byte(gql), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}

	conf := Config{
		DBType: "sqlite",
	}
	co := NewCompiler(conf)
	
	var w bytes.Buffer
	_, err = co.Compile(&w, qc)
	if err != nil {
		t.Fatal(err)
	}
	
	sql := w.String()
	t.Log(sql)
	
	if strings.Contains(sql, "LATERAL") {
		t.Error("Generated SQL contains LATERAL join, expected inline rendering for SQLite")
	}
	

	if !strings.Contains(sql, "json_group_array") {
		t.Error("Generated SQL missing json_group_array")
	}
}

func TestSQLiteEmptySelection(t *testing.T) {
	schema, err := sdata.GetTestSchema()
	if err != nil {
		t.Fatal(err)
	}

	qcCompiler, err := qcode.NewCompiler(schema, qcode.Config{DBSchema: schema.DBSchema()})
	if err != nil {
		t.Fatal(err)
	}

	err = qcCompiler.AddRole("user", "public", "users", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "full_name", "email", "products"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	gql := `query {
		users {
			id @remove(ifRole: "user")
		}
	}`

	qc, err := qcCompiler.Compile([]byte(gql), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}

	conf := Config{
		DBType: "sqlite",
	}
	co := NewCompiler(conf)

	var w bytes.Buffer
	_, err = co.Compile(&w, qc)
	if err != nil {
		t.Fatal(err)
	}

	sql := w.String()
	// t.Log(sql)

	if strings.Contains(sql, "SELECT FROM") {
		t.Error("Generated SQL contains invalid 'SELECT FROM' syntax for SQLite")
	}
}
