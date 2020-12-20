package psql_test

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/internal/psql"
	"github.com/dosco/graphjin/core/internal/qcode"
	"github.com/dosco/graphjin/core/internal/sdata"
)

const (
	errNotExpected = "Generated SQL did not match what was expected"
	headerMarker   = "=== RUN"
	commentMarker  = "---"
)

var (
	qcompile *qcode.Compiler
	pcompile *psql.Compiler
	expected map[string][]string
)

func TestMain(m *testing.M) {
	var err error

	schema, err := sdata.GetTestSchema()
	if err != nil {
		log.Fatal(err)
	}

	t, err := schema.GetTableInfo("customers", "")
	if err != nil {
		log.Fatal(err)
	}

	remoteVal := sdata.DBRel{Type: sdata.RelRemote}
	remoteVal.Left.Col = t.PrimaryCol
	remoteVal.Right.VTable = fmt.Sprintf("__%s_%s", t.Name, t.PrimaryCol.Name)

	if err := schema.SetRel("payments", "customers", remoteVal); err != nil {
		log.Fatal(err)
	}

	qcompile, err = qcode.NewCompiler(schema, qcode.Config{})
	if err != nil {
		log.Fatal(err)
	}

	err = qcompile.AddRole("user", "product", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "name", "price", "users", "customers"},
			Filters: []string{
				"{ price: { gt: 0 } }",
				"{ price: { lt: 8 } }",
			},
		},
		Insert: qcode.InsertConfig{
			Presets: map[string]string{
				"price":      "$get_price",
				"user_id":    "$user_id",
				"created_at": "now",
				"updated_at": "now",
			},
		},
		Update: qcode.UpdateConfig{
			Filters: []string{"{ user_id: { eq: $user_id } }"},
			Presets: map[string]string{"updated_at": "now"},
		},
		Delete: qcode.DeleteConfig{
			Filters: []string{
				"{ price: { gt: 0 } }",
				"{ price: { lt: 8 } }",
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	err = qcompile.AddRole("anon", "product", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "name"},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	err = qcompile.AddRole("anon1", "product", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns:          []string{"id", "name", "price"},
			DisableFunctions: true,
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	err = qcompile.AddRole("user", "users", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "full_name", "avatar", "email", "products"},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	err = qcompile.AddRole("bad_dude", "users", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Filters:          []string{"false"},
			DisableFunctions: true,
		},
		Insert: qcode.InsertConfig{},
		Update: qcode.UpdateConfig{
			Filters: []string{"false"},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	err = qcompile.AddRole("user", "mes", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "full_name", "avatar", "email"},
			Filters: []string{
				"{ id: { eq: $user_id } }",
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	err = qcompile.AddRole("user", "customers", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "email", "full_name", "products"},
		},
	})

	if err != nil {
		log.Fatal(err)
	}

	vars := map[string]string{
		"admin_account_id": "5",
		"get_price":        "sql:select price from prices where id = $product_id",
	}

	pcompile = psql.NewCompiler(psql.Config{
		Vars: vars,
	})

	expected = make(map[string][]string)

	b, err := ioutil.ReadFile("tests.sql")
	if err != nil {
		log.Fatal(err)
	}
	text := string(b)
	lines := strings.Split(text, "\n")

	var h string

	for _, v := range lines {
		switch {
		case strings.HasPrefix(v, headerMarker):
			h = strings.TrimSpace(v[len(headerMarker):])

		case strings.HasPrefix(v, commentMarker):
			break

		default:
			v := strings.TrimSpace(v)
			if v != "" {
				expected[h] = append(expected[h], v)
			}
		}
	}
	os.Exit(m.Run())
}

func compileGQLToPSQL(t *testing.T, gql string, vars qcode.Variables, role string) {
	if err := _compileGQLToPSQL(t, gql, vars, role); err != nil {
		t.Fatal(err)
	}
}

func compileGQLToPSQLExpectErr(t *testing.T, gql string, vars qcode.Variables, role string) {
	if err := _compileGQLToPSQL(t, gql, vars, role); err == nil {
		t.Fatal(errors.New("we were expecting an error"))
	}
}

func _compileGQLToPSQL(t *testing.T, gql string, vars qcode.Variables, role string) error {
	generateTestFile := false

	if generateTestFile {
		var sqlStmts []string

		for i := 0; i < 100; i++ {
			qc, err := qcompile.Compile([]byte(gql), vars, role)
			if err != nil {
				return err
			}

			_, sqlB, err := pcompile.CompileEx(qc)
			if err != nil {
				return err
			}

			sql := string(sqlB)

			match := false
			for _, s := range sqlStmts {
				if sql == s {
					match = true
					break
				}
			}

			if !match {
				s := string(sql)
				sqlStmts = append(sqlStmts, s)
				fmt.Println(s)
			}
		}

		return nil
	}

	for i := 0; i < 200; i++ {
		qc, err := qcompile.Compile([]byte(gql), vars, role)
		if err != nil {
			return err
		}

		_, sqlStmt, err := pcompile.CompileEx(qc)
		if err != nil {
			return err
		}

		failed := true

		for _, sql := range expected[t.Name()] {
			if string(sqlStmt) == sql {
				failed = false
			}
		}

		if failed {
			fmt.Println(string(sqlStmt))
			t.Fatal(errNotExpected)
		}
	}

	return nil
}
