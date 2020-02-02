package psql

import (
	"log"
	"os"
	"testing"

	"github.com/dosco/super-graph/qcode"
)

const (
	errNotExpected = "Generated SQL did not match what was expected"
)

var (
	qcompile *qcode.Compiler
	pcompile *Compiler
)

func TestMain(m *testing.M) {
	var err error

	qcompile, err = qcode.NewCompiler(qcode.Config{
		Blocklist: []string{
			"secret",
			"password",
			"token",
		},
	})
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
		Insert: qcode.InsertConfig{
			Filters: []string{"false"},
		},
		Update: qcode.UpdateConfig{
			Filters: []string{"false"},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	err = qcompile.AddRole("user", "mes", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "full_name", "avatar"},
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

	schema := getTestSchema()

	vars := NewVariables(map[string]string{
		"admin_account_id": "5",
	})

	pcompile = NewCompiler(Config{
		Schema: schema,
		Vars:   vars,
	})

	os.Exit(m.Run())
}

func compileGQLToPSQL(gql string, vars Variables, role string) ([]byte, error) {
	qc, err := qcompile.Compile([]byte(gql), role)
	if err != nil {
		return nil, err
	}

	_, sqlStmt, err := pcompile.CompileEx(qc, vars)
	if err != nil {
		return nil, err
	}

	//fmt.Println(string(sqlStmt))

	return sqlStmt, nil
}
