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

	qcompile.AddRole("user", "product", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "name", "price", "users", "customers"},
			Filters: []string{
				"{ price: { gt: 0 } }",
				"{ price: { lt: 8 } }",
			},
		},
		Update: qcode.UpdateConfig{
			Filters: []string{"{ user_id: { eq: $user_id } }"},
		},
		Delete: qcode.DeleteConfig{
			Filters: []string{
				"{ price: { gt: 0 } }",
				"{ price: { lt: 8 } }",
			},
		},
	})

	qcompile.AddRole("anon", "product", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "name"},
		},
	})

	qcompile.AddRole("anon1", "product", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns:          []string{"id", "name", "price"},
			DisableFunctions: true,
		},
	})

	qcompile.AddRole("user", "users", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "full_name", "avatar", "email", "products"},
		},
	})

	qcompile.AddRole("bad_dude", "users", qcode.TRConfig{
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

	qcompile.AddRole("user", "mes", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "full_name", "avatar"},
			Filters: []string{
				"{ id: { eq: $user_id } }",
			},
		},
	})

	qcompile.AddRole("user", "customers", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "email", "full_name", "products"},
		},
	})

	if err != nil {
		log.Fatal(err)
	}

	tables := []*DBTable{
		&DBTable{Name: "customers", Type: "table"},
		&DBTable{Name: "users", Type: "table"},
		&DBTable{Name: "products", Type: "table"},
		&DBTable{Name: "purchases", Type: "table"},
	}

	columns := [][]*DBColumn{
		[]*DBColumn{
			&DBColumn{ID: 1, Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true, UniqueKey: false, FKeyTable: "", FKeyColID: []int16(nil)},
			&DBColumn{ID: 2, Name: "full_name", Type: "character varying", NotNull: true, PrimaryKey: false, UniqueKey: false, FKeyTable: "", FKeyColID: []int16(nil)},
			&DBColumn{ID: 3, Name: "phone", Type: "character varying", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeyTable: "", FKeyColID: []int16(nil)},
			&DBColumn{ID: 4, Name: "email", Type: "character varying", NotNull: true, PrimaryKey: false, UniqueKey: false, FKeyTable: "", FKeyColID: []int16(nil)},
			&DBColumn{ID: 5, Name: "encrypted_password", Type: "character varying", NotNull: true, PrimaryKey: false, UniqueKey: false, FKeyTable: "", FKeyColID: []int16(nil)},
			&DBColumn{ID: 6, Name: "reset_password_token", Type: "character varying", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeyTable: "", FKeyColID: []int16(nil)},
			&DBColumn{ID: 7, Name: "reset_password_sent_at", Type: "timestamp without time zone", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeyTable: "", FKeyColID: []int16(nil)},
			&DBColumn{ID: 8, Name: "remember_created_at", Type: "timestamp without time zone", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeyTable: "", FKeyColID: []int16(nil)},
			&DBColumn{ID: 9, Name: "created_at", Type: "timestamp without time zone", NotNull: true, PrimaryKey: false, UniqueKey: false, FKeyTable: "", FKeyColID: []int16(nil)},
			&DBColumn{ID: 10, Name: "updated_at", Type: "timestamp without time zone", NotNull: true, PrimaryKey: false, UniqueKey: false, FKeyTable: "", FKeyColID: []int16(nil)}},
		[]*DBColumn{
			&DBColumn{ID: 1, Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true, UniqueKey: false, FKeyTable: "", FKeyColID: []int16(nil)},
			&DBColumn{ID: 2, Name: "full_name", Type: "character varying", NotNull: true, PrimaryKey: false, UniqueKey: false, FKeyTable: "", FKeyColID: []int16(nil)},
			&DBColumn{ID: 3, Name: "phone", Type: "character varying", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeyTable: "", FKeyColID: []int16(nil)},
			&DBColumn{ID: 4, Name: "avatar", Type: "character varying", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeyTable: "", FKeyColID: []int16(nil)},
			&DBColumn{ID: 5, Name: "email", Type: "character varying", NotNull: true, PrimaryKey: false, UniqueKey: false, FKeyTable: "", FKeyColID: []int16(nil)},
			&DBColumn{ID: 6, Name: "encrypted_password", Type: "character varying", NotNull: true, PrimaryKey: false, UniqueKey: false, FKeyTable: "", FKeyColID: []int16(nil)},
			&DBColumn{ID: 7, Name: "reset_password_token", Type: "character varying", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeyTable: "", FKeyColID: []int16(nil)},
			&DBColumn{ID: 8, Name: "reset_password_sent_at", Type: "timestamp without time zone", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeyTable: "", FKeyColID: []int16(nil)},
			&DBColumn{ID: 9, Name: "remember_created_at", Type: "timestamp without time zone", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeyTable: "", FKeyColID: []int16(nil)},
			&DBColumn{ID: 10, Name: "created_at", Type: "timestamp without time zone", NotNull: true, PrimaryKey: false, UniqueKey: false, FKeyTable: "", FKeyColID: []int16(nil)},
			&DBColumn{ID: 11, Name: "updated_at", Type: "timestamp without time zone", NotNull: true, PrimaryKey: false, UniqueKey: false, FKeyTable: "", FKeyColID: []int16(nil)}},
		[]*DBColumn{
			&DBColumn{ID: 1, Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true, UniqueKey: false, FKeyTable: "", FKeyColID: []int16(nil)},
			&DBColumn{ID: 2, Name: "name", Type: "character varying", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeyTable: "", FKeyColID: []int16(nil)},
			&DBColumn{ID: 3, Name: "description", Type: "text", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeyTable: "", FKeyColID: []int16(nil)},
			&DBColumn{ID: 4, Name: "price", Type: "numeric(7,2)", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeyTable: "", FKeyColID: []int16(nil)},
			&DBColumn{ID: 5, Name: "user_id", Type: "bigint", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeyTable: "users", FKeyColID: []int16{1}},
			&DBColumn{ID: 6, Name: "created_at", Type: "timestamp without time zone", NotNull: true, PrimaryKey: false, UniqueKey: false, FKeyTable: "", FKeyColID: []int16(nil)},
			&DBColumn{ID: 7, Name: "updated_at", Type: "timestamp without time zone", NotNull: true, PrimaryKey: false, UniqueKey: false, FKeyTable: "", FKeyColID: []int16(nil)},
			&DBColumn{ID: 8, Name: "tsv", Type: "tsvector", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeyTable: "", FKeyColID: []int16(nil)}},
		[]*DBColumn{
			&DBColumn{ID: 1, Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true, UniqueKey: false, FKeyTable: "", FKeyColID: []int16(nil)},
			&DBColumn{ID: 2, Name: "customer_id", Type: "bigint", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeyTable: "customers", FKeyColID: []int16{1}},
			&DBColumn{ID: 3, Name: "product_id", Type: "bigint", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeyTable: "products", FKeyColID: []int16{1}},
			&DBColumn{ID: 4, Name: "sale_type", Type: "character varying", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeyTable: "", FKeyColID: []int16(nil)},
			&DBColumn{ID: 5, Name: "quantity", Type: "integer", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeyTable: "", FKeyColID: []int16(nil)},
			&DBColumn{ID: 6, Name: "due_date", Type: "timestamp without time zone", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeyTable: "", FKeyColID: []int16(nil)},
			&DBColumn{ID: 7, Name: "returned", Type: "timestamp without time zone", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeyTable: "", FKeyColID: []int16(nil)}},
	}

	schema := &DBSchema{
		t:  make(map[string]*DBTableInfo),
		rm: make(map[string]map[string]*DBRel),
		al: make(map[string]struct{}),
	}

	aliases := map[string][]string{
		"users": []string{"mes"},
	}

	for i, t := range tables {
		schema.updateSchema(t, columns[i], aliases)
	}

	vars := NewVariables(map[string]string{
		"account_id": "select account_id from users where id = $user_id",
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
