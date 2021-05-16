package core_test

import (
	"database/sql"
	"flag"
	"fmt"
	"testing"

	"github.com/orlangure/gnomock"
	"github.com/orlangure/gnomock/preset/cockroachdb"
	"github.com/orlangure/gnomock/preset/mssql"
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
	dbParam string
	dbType  string
	db      *sql.DB
)

func init() {
	flag.StringVar(&dbParam, "db", "", "database type")
}

func TestMain(m *testing.M) {
	flag.Parse()

	dbinfoList := []dbinfo{
		{
			name:    "postgres",
			driver:  "postgres",
			connstr: "postgres://tester:tester@%s/db?sslmode=disable",
			preset: postgres.Preset(
				postgres.WithUser("tester", "tester"),
				postgres.WithDatabase("db"),
				postgres.WithQueriesFile("./postgres.sql"),
			),
		},
		{
			disable: true,
			name:    "cockroach",
			driver:  "postgres",
			connstr: "postgres://root:@%s/db?sslmode=disable",
			preset: cockroachdb.Preset(
				cockroachdb.WithDatabase("db"),
				cockroachdb.WithQueriesFile("./cockroach.sql"),
			),
		},
		{
			disable: true,
			name:    "mysql",
			driver:  "mysql",
			connstr: "user:user@tcp(%s)/db",
			preset: mysql.Preset(
				mysql.WithUser("user", "user"),
				mysql.WithDatabase("db"),
				mysql.WithQueriesFile("./mysql.sql"),
			),
		},
		{
			disable: true,
			name:    "mssql",
			driver:  "sqlserver",
			connstr: "sqlserver://sa:password@%s?database=db",
			preset: mssql.Preset(
				mssql.WithLicense(true),
				mssql.WithVersion("2019-latest"),
				mssql.WithAdminPassword("YourStrong!Passw0rd"),
				mssql.WithDatabase("db"),
				mssql.WithQueriesFile("./mssql.sql"),
			),
		},
	}

	for _, v := range dbinfoList {
		disable := v.disable

		if dbParam != "" {
			if dbParam != v.name {
				continue
			} else {
				disable = false
			}
		}

		if disable {
			continue
		}

		con, err := gnomock.Start(v.preset)
		if err != nil {
			panic(err)
		}
		defer func() {
			if err := gnomock.Stop(con); err != nil {
				panic(err)
			}
		}()

		db, err = sql.Open(v.driver, fmt.Sprintf(v.connstr, con.DefaultAddress()))
		if err != nil {
			panic(err)
		}
		db.SetMaxIdleConns(100)
		dbType = v.name

		// if res := m.Run(); res != 0 {
		// 	os.Exit(res)
		// }
	}
}
