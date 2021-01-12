package core_test

import (
	"database/sql"
	"fmt"
	"os"
	"testing"

	"github.com/orlangure/gnomock"
	"github.com/orlangure/gnomock/preset/mysql"
	"github.com/orlangure/gnomock/preset/postgres"
)

type dbinfo struct {
	name    string
	driver  string
	connstr string
	disable bool
	preset  gnomock.Preset
}

var (
	dbType string
	db     *sql.DB
)

func TestMain(m *testing.M) {
	// var err error

	// schema, err := sdata.GetTestSchema()
	// if err != nil {
	// 	log.Fatal(err)
	// }

	// t, err := schema.GetTableInfo("customers", "")
	// if err != nil {
	// 	log.Fatal(err)
	// }

	// remoteVal := sdata.DBRel{Type: sdata.RelRemote}
	// remoteVal.Left.Col = t.PrimaryCol
	// remoteVal.Right.VTable = fmt.Sprintf("__%s_%s", t.Name, t.PrimaryCol.Name)

	// if err := schema.SetRel("payments", "customers", remoteVal); err != nil {
	// 	log.Fatal(err)
	// }

	// qcompile, err = qcode.NewCompiler(schema, qcode.Config{})
	// if err != nil {
	// 	log.Fatal(err)
	// }

	// err = qcompile.AddRole("user", "product", qcode.TRConfig{
	// 	Query: qcode.QueryConfig{
	// 		Columns: []string{"id", "name", "price", "users", "customers"},
	// 		Filters: []string{
	// 			"{ price: { gt: 0 } }",
	// 			"{ price: { lt: 8 } }",
	// 		},
	// 	},
	// 	Insert: qcode.InsertConfig{
	// 		Presets: map[string]string{
	// 			"price":      "$get_price",
	// 			"user_id":    "$user_id",
	// 			"created_at": "now",
	// 			"updated_at": "now",
	// 		},
	// 	},
	// 	Update: qcode.UpdateConfig{
	// 		Filters: []string{"{ user_id: { eq: $user_id } }"},
	// 		Presets: map[string]string{"updated_at": "now"},
	// 	},
	// 	Delete: qcode.DeleteConfig{
	// 		Filters: []string{
	// 			"{ price: { gt: 0 } }",
	// 			"{ price: { lt: 8 } }",
	// 		},
	// 	},
	// })
	// if err != nil {
	// 	log.Fatal(err)
	// }

	// err = qcompile.AddRole("anon", "product", qcode.TRConfig{
	// 	Query: qcode.QueryConfig{
	// 		Columns: []string{"id", "name"},
	// 	},
	// })
	// if err != nil {
	// 	log.Fatal(err)
	// }

	// err = qcompile.AddRole("anon1", "product", qcode.TRConfig{
	// 	Query: qcode.QueryConfig{
	// 		Columns:          []string{"id", "name", "price"},
	// 		DisableFunctions: true,
	// 	},
	// })
	// if err != nil {
	// 	log.Fatal(err)
	// }

	// err = qcompile.AddRole("user", "users", qcode.TRConfig{
	// 	Query: qcode.QueryConfig{
	// 		Columns: []string{"id", "full_name", "avatar", "email", "products"},
	// 	},
	// })
	// if err != nil {
	// 	log.Fatal(err)
	// }

	// err = qcompile.AddRole("bad_dude", "users", qcode.TRConfig{
	// 	Query: qcode.QueryConfig{
	// 		Filters:          []string{"false"},
	// 		DisableFunctions: true,
	// 	},
	// 	Insert: qcode.InsertConfig{},
	// 	Update: qcode.UpdateConfig{
	// 		Filters: []string{"false"},
	// 	},
	// })
	// if err != nil {
	// 	log.Fatal(err)
	// }

	// err = qcompile.AddRole("user", "mes", qcode.TRConfig{
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

	// err = qcompile.AddRole("user", "customers", qcode.TRConfig{
	// 	Query: qcode.QueryConfig{
	// 		Columns: []string{"id", "email", "full_name", "products"},
	// 	},
	// })

	// if err != nil {
	// 	log.Fatal(err)
	// }

	// vars := map[string]string{
	// 	"admin_account_id": "5",
	// 	"get_price":        "sql:select price from prices where id = $product_id",
	// }

	// pcompile = psql.NewCompiler(psql.Config{
	// 	Vars: vars,
	// })

	dbinfoList := []dbinfo{
		{
			name:    "postgres",
			driver:  "postgres",
			connstr: "postgres://tester:tester@%s/db?sslmode=disable",
			preset: postgres.Preset(
				postgres.WithUser("tester", "tester"),
				postgres.WithDatabase("db"),
				postgres.WithQueriesFile("./postgres.sql"),
			)},
		{
			disable: true,
			name:    "mysql",
			driver:  "mysql",
			connstr: "user:user@tcp(%s)/db",
			preset: mysql.Preset(
				mysql.WithUser("user", "user"),
				mysql.WithDatabase("db"),
				mysql.WithQueriesFile("./mysql.sql"),
			)},
	}

	for _, v := range dbinfoList {
		if v.disable {
			continue
		}
		con, err := gnomock.Start(v.preset)
		if err != nil {
			panic(err)
		}
		defer func() { _ = gnomock.Stop(con) }()

		db, err = sql.Open(v.driver, fmt.Sprintf(v.connstr, con.DefaultAddress()))
		if err != nil {
			panic(err)
		}
		dbType = v.name

		if res := m.Run(); res != 0 {
			os.Exit(res)
		}
	}
}
