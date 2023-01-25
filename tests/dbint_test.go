package tests_test

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"testing"

	"github.com/dosco/graphjin/core/v3"
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

	if dbParam == "none" {
		res := m.Run()
		os.Exit(res)
	}

	dbinfoList := []dbinfo{
		{
			name:    "postgres",
			driver:  "postgres",
			connstr: "postgres://tester:tester@%s/db?sslmode=disable",
			preset: postgres.Preset(
				postgres.WithUser("tester", "tester"),
				postgres.WithDatabase("db"),
				postgres.WithQueriesFile("./postgres.sql"),
				postgres.WithVersion("12.5"),
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
				cockroachdb.WithVersion("v20.1.10"),
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
				mysql.WithVersion("8.0.22"),
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

		con, err := gnomock.Start(
			v.preset,
			gnomock.WithLogWriter(os.Stdout))
		if err != nil {
			panic(err)
		}

		db, err = sql.Open(v.driver, fmt.Sprintf(v.connstr, con.DefaultAddress()))

		if err != nil {
			_ = gnomock.Stop(con)
			panic(err)
		}
		db.SetMaxIdleConns(300)
		db.SetMaxOpenConns(600)
		dbType = v.name

		res := m.Run()
		_ = gnomock.Stop(con)
		os.Exit(res)
	}
}

func newConfig(c *core.Config) *core.Config {
	c.DBSchemaPollDuration = -1
	return c
}

func stdJSON(val []byte) string {
	var m map[string]interface{}

	if err := json.Unmarshal(val, &m); err != nil {
		panic(err)
	}

	if v, err := json.Marshal(m); err == nil {
		return string(v)
	} else {
		panic(err)
	}
}

func printJSON(val []byte) {
	fmt.Println(stdJSON(val))
}
