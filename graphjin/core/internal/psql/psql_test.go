package psql_test

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/psql"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

var (
	qcompile *qcode.Compiler
	pcompile *psql.Compiler
)

func TestMain(m *testing.M) {
	var err error

	schema, err := sdata.GetTestSchema()
	if err != nil {
		log.Fatal(err)
	}

	qcompile, err = qcode.NewCompiler(schema, qcode.Config{DBSchema: schema.DBSchema()})
	if err != nil {
		log.Fatal(err)
	}

	err = qcompile.AddRole("user", "public", "products", qcode.TRConfig{
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

	err = qcompile.AddRole("anon", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "name"},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	err = qcompile.AddRole("anon1", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns:          []string{"id", "name", "price"},
			DisableFunctions: true,
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	err = qcompile.AddRole("user", "public", "users", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "full_name", "avatar", "email", "products"},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	err = qcompile.AddRole("bad_dude", "public", "users", qcode.TRConfig{
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

	// err = qcompile.AddRole("user", "", "mes", qcode.TRConfig{
	// 	Query: qcode.QueryConfig{
	// 		Columns: []string{"id", "full_name", "avatar", "email"},
	// 		Filters: []string{
	// 			"{ id: { eq: $user_id } }",
	// 		},
	// 	},
	// })
	// if err != nil {
	// 	log.Fatal(err)
	// }

	err = qcompile.AddRole("user", "public", "customers", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "vip"},
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

	os.Exit(m.Run())
}

func compileGQLToPSQL(t *testing.T, gql string,
	vars map[string]json.RawMessage,
	role string,
) {
	var v json.RawMessage
	var err error

	if v, err = json.Marshal(vars); err != nil {
		t.Error(err)
	}

	if err := _compileGQLToPSQL(t, gql, v, role); err != nil {
		t.Error(err)
	}
}

func compileGQLToPSQLExpectErr(t *testing.T, gql string,
	vars map[string]json.RawMessage,
	role string,
) {
	var v json.RawMessage
	var err error

	if v, err = json.Marshal(v); err != nil {
		t.Error(err)
	}

	if err := _compileGQLToPSQL(t, gql, v, role); err == nil {
		t.Error(errors.New("we were expecting an error"))
	}
}

func _compileGQLToPSQL(t *testing.T, gql string, vars json.RawMessage, role string) error {
	v := make(map[string]json.RawMessage)
	if err := json.Unmarshal(vars, &v); err != nil {
		t.Error(err)
	}

	for i := 0; i < 1000; i++ {
		qc, err := qcompile.Compile([]byte(gql), v, role, "")
		if err != nil {
			return err
		}

		_, _, err = pcompile.CompileEx(qc)
		if err != nil {
			return err
		}
	}

	return nil
}
