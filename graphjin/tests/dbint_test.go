package tests_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/dosco/graphjin/core/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mysql"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
)

type dbinfo struct {
	name      string
	driver    string
	disable   bool
	startFunc func(context.Context) (testcontainers.Container, string, error)
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

	ctx := context.Background()

	dbinfoList := []dbinfo{
		{
			name:   "postgres",
			driver: "postgres",
			startFunc: func(ctx context.Context) (testcontainers.Container, string, error) {
				container, err := postgres.Run(ctx,
					"postgres:12.5",
					postgres.WithUsername("tester"),
					postgres.WithPassword("tester"),
					postgres.WithDatabase("db"),
					postgres.WithInitScripts("./postgres.sql"),
				)
				if err != nil {
					return nil, "", err
				}

				connStr, err := container.ConnectionString(ctx, "sslmode=disable")
				if err != nil {
					return container, "", err
				}

				// Test connection and wait for database to be fully ready
				for i := 0; i < 30; i++ {
					testDB, err := sql.Open("postgres", connStr)
					if err == nil {
						if err = testDB.Ping(); err == nil {
							// Test that our schema is loaded by checking for a table
							var count int
							err = testDB.QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_name = 'users'").Scan(&count)
							testDB.Close()
							if err == nil && count > 0 {
								break
							}
						}
						testDB.Close()
					}
					time.Sleep(500 * time.Millisecond)
				}

				return container, connStr, err
			},
		},
		{
			disable: true,
			name:    "mysql",
			driver:  "mysql",
			startFunc: func(ctx context.Context) (testcontainers.Container, string, error) {
				container, err := mysql.Run(ctx,
					"mysql:8.0",
					mysql.WithUsername("user"),
					mysql.WithPassword("user"),
					mysql.WithDatabase("db"),
					mysql.WithScripts("./mysql.sql"),
				)
				if err != nil {
					return nil, "", err
				}

				connStr, err := container.ConnectionString(ctx)
				if err != nil {
					return container, "", err
				}

				// Test connection and wait for database to be fully ready
				for i := 0; i < 30; i++ {
					testDB, err := sql.Open("mysql", connStr)
					if err == nil {
						if err = testDB.Ping(); err == nil {
							// Test that our schema is loaded by checking for a table
							var count int
							err = testDB.QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'db' AND table_name = 'users'").Scan(&count)
							testDB.Close()
							if err == nil && count > 0 {
								break
							}
						}
						testDB.Close()
					}
					time.Sleep(500 * time.Millisecond)
				}

				return container, connStr, err
			},
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

		container, connStr, err := v.startFunc(ctx)
		if err != nil {
			panic(err)
		}

		db, err = sql.Open(v.driver, connStr)
		if err != nil {
			_ = container.Terminate(ctx)
			panic(err)
		}
		// Configure connection pool settings to prevent "closing bad idle connection" errors
		// Use reasonable limits for test scenarios and ensure connections are recycled
		// before MySQL's default wait_timeout (8 hours)
		db.SetMaxIdleConns(20)                      // Reduced from 300
		db.SetMaxOpenConns(100)                     // Reduced from 600
		db.SetConnMaxLifetime(5 * time.Minute)      // Recycle connections after 5 minutes
		db.SetConnMaxIdleTime(2 * time.Minute)      // Close idle connections after 2 minutes
		dbType = v.name

		res := m.Run()
		_ = container.Terminate(ctx)
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

var re = regexp.MustCompile(`([:,])\s|`)

func printJSONString(val string) {
	v := re.ReplaceAllString(val, `$1`)
	fmt.Println(v)
}
