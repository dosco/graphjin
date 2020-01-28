package psql

import (
	"log"
	"os"
	"strings"
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

	tables := []DBTable{
		DBTable{Name: "customers", Type: "table"},
		DBTable{Name: "users", Type: "table"},
		DBTable{Name: "products", Type: "table"},
		DBTable{Name: "purchases", Type: "table"},
		DBTable{Name: "tags", Type: "table"},
		DBTable{Name: "tag_count", Type: "json"},
	}

	columns := [][]DBColumn{
		[]DBColumn{
			DBColumn{ID: 1, Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true, UniqueKey: true},
			DBColumn{ID: 2, Name: "full_name", Type: "character varying", NotNull: true, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 3, Name: "phone", Type: "character varying", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 4, Name: "email", Type: "character varying", NotNull: true, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 5, Name: "encrypted_password", Type: "character varying", NotNull: true, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 6, Name: "reset_password_token", Type: "character varying", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 7, Name: "reset_password_sent_at", Type: "timestamp without time zone", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 8, Name: "remember_created_at", Type: "timestamp without time zone", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 9, Name: "created_at", Type: "timestamp without time zone", NotNull: true, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 10, Name: "updated_at", Type: "timestamp without time zone", NotNull: true, PrimaryKey: false, UniqueKey: false}},
		[]DBColumn{
			DBColumn{ID: 1, Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true, UniqueKey: true},
			DBColumn{ID: 2, Name: "full_name", Type: "character varying", NotNull: true, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 3, Name: "phone", Type: "character varying", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 4, Name: "avatar", Type: "character varying", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 5, Name: "email", Type: "character varying", NotNull: true, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 6, Name: "encrypted_password", Type: "character varying", NotNull: true, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 7, Name: "reset_password_token", Type: "character varying", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 8, Name: "reset_password_sent_at", Type: "timestamp without time zone", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 9, Name: "remember_created_at", Type: "timestamp without time zone", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 10, Name: "created_at", Type: "timestamp without time zone", NotNull: true, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 11, Name: "updated_at", Type: "timestamp without time zone", NotNull: true, PrimaryKey: false, UniqueKey: false}},
		[]DBColumn{
			DBColumn{ID: 1, Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true, UniqueKey: true},
			DBColumn{ID: 2, Name: "name", Type: "character varying", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 3, Name: "description", Type: "text", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 4, Name: "price", Type: "numeric(7,2)", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 5, Name: "user_id", Type: "bigint", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeyTable: "users", FKeyColID: []int16{1}},
			DBColumn{ID: 6, Name: "created_at", Type: "timestamp without time zone", NotNull: true, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 7, Name: "updated_at", Type: "timestamp without time zone", NotNull: true, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 8, Name: "tsv", Type: "tsvector", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 9, Name: "tags", Type: "text[]", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeyTable: "tags", FKeyColID: []int16{3}, Array: true},
			DBColumn{ID: 9, Name: "tag_count", Type: "json", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeyTable: "tag_count", FKeyColID: []int16{}}},
		[]DBColumn{
			DBColumn{ID: 1, Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true, UniqueKey: true},
			DBColumn{ID: 2, Name: "customer_id", Type: "bigint", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeyTable: "customers", FKeyColID: []int16{1}},
			DBColumn{ID: 3, Name: "product_id", Type: "bigint", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeyTable: "products", FKeyColID: []int16{1}},
			DBColumn{ID: 4, Name: "sale_type", Type: "character varying", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 5, Name: "quantity", Type: "integer", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 6, Name: "due_date", Type: "timestamp without time zone", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 7, Name: "returned", Type: "timestamp without time zone", NotNull: false, PrimaryKey: false, UniqueKey: false}},
		[]DBColumn{
			DBColumn{ID: 1, Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true, UniqueKey: true},
			DBColumn{ID: 2, Name: "name", Type: "text", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 3, Name: "slug", Type: "text", NotNull: false, PrimaryKey: false, UniqueKey: false}},
		[]DBColumn{
			DBColumn{ID: 1, Name: "tag_id", Type: "bigint", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeyTable: "tags", FKeyColID: []int16{1}},
			DBColumn{ID: 2, Name: "count", Type: "int", NotNull: false, PrimaryKey: false, UniqueKey: false}},
	}

	for i := range tables {
		tables[i].Key = strings.ToLower(tables[i].Name)
		for n := range columns[i] {
			columns[i][n].Key = strings.ToLower(columns[i][n].Name)
		}
	}

	schema := &DBSchema{
		ver: 110000,
		t:   make(map[string]*DBTableInfo),
		rm:  make(map[string]map[string]*DBRel),
	}

	aliases := map[string][]string{
		"users": []string{"mes"},
	}

	for i, t := range tables {
		err := schema.addTable(t, columns[i], aliases)
		if err != nil {
			log.Fatal(err)
		}
	}

	for i, t := range tables {
		err := schema.updateRelationships(t, columns[i])
		if err != nil {
			log.Fatal(err)
		}
	}

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
