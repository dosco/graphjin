package tests_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/mongodriver"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"github.com/testcontainers/testcontainers-go/modules/mysql"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	"github.com/mattn/go-sqlite3"
	_ "github.com/microsoft/go-mssqldb"
	_ "github.com/sijms/go-ora/v2"
)

func init() {
	sql.Register("sqlite3_regexp", &sqlite3.SQLiteDriver{
		ConnectHook: func(conn *sqlite3.SQLiteConn) error {
			if err := conn.RegisterFunc("REGEXP", func(re, s string) (bool, error) {
				return regexp.MatchString(re, s)
			}, true); err != nil {
				return err
			}
			return conn.RegisterFunc("regexp", func(re, s string) (bool, error) {
				return regexp.MatchString(re, s)
			}, true)
		},
	})
}

type dbinfo struct {
	name      string
	driver    string
	disable   bool
	startFunc func(context.Context) (func(context.Context) error, string, error)
	// dbFunc is an optional function that returns *sql.DB directly (for drivers like MongoDB)
	dbFunc func(context.Context) (func(context.Context) error, *sql.DB, error)
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
			startFunc: func(ctx context.Context) (func(context.Context) error, string, error) {
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
					return nil, "", err
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

				return container.Terminate, connStr, err
			},
		},
		{
			name:    "mysql",
			driver:  "mysql",
			startFunc: func(ctx context.Context) (func(context.Context) error, string, error) {
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
					return nil, "", err
				}
				if strings.Contains(connStr, "?") {
					connStr += "&multiStatements=true&parseTime=true&interpolateParams=true"
				} else {
					connStr += "?multiStatements=true&parseTime=true&interpolateParams=true"
				}
				// fmt.Printf("DEBUG MySQL DSN: %s\n", connStr)

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

				return container.Terminate, connStr, err
			},
		},
		{
			name:   "mariadb",
			driver: "mysql", // MariaDB uses MySQL wire protocol
			startFunc: func(ctx context.Context) (func(context.Context) error, string, error) {
				// Use GenericContainer instead of mysql.Run because the MySQL helper
				// has a wait strategy that doesn't recognize MariaDB's log format
				req := testcontainers.GenericContainerRequest{
					ContainerRequest: testcontainers.ContainerRequest{
						Image:        "mariadb:10.11",
						ExposedPorts: []string{"3306/tcp"},
						Env: map[string]string{
							"MYSQL_ROOT_PASSWORD": "root",
							"MYSQL_DATABASE":      "db",
							"MYSQL_USER":          "user",
							"MYSQL_PASSWORD":      "user",
						},
						WaitingFor: wait.ForLog("ready for connections").WithStartupTimeout(120 * time.Second),
					},
					Started: true,
				}
				container, err := testcontainers.GenericContainer(ctx, req)
				if err != nil {
					return nil, "", err
				}

				host, _ := container.Host(ctx)
				port, _ := container.MappedPort(ctx, "3306")

				connStr := fmt.Sprintf("user:user@tcp(%s:%s)/db?multiStatements=true&parseTime=true&interpolateParams=true",
					host, port.Port())

				// Wait for database to be fully ready and initialize with mariadb.sql script
				var initDB *sql.DB
				for i := 0; i < 30; i++ {
					initDB, err = sql.Open("mysql", connStr)
					if err == nil {
						if err = initDB.Ping(); err == nil {
							break
						}
						initDB.Close()
					}
					time.Sleep(500 * time.Millisecond)
				}
				if err != nil {
					return nil, "", fmt.Errorf("failed to connect to mariadb: %w", err)
				}
				defer initDB.Close()

				script, err := os.ReadFile("./mariadb.sql")
				if err != nil {
					return nil, "", err
				}
				if _, err := initDB.Exec(string(script)); err != nil {
					return nil, "", fmt.Errorf("failed to init mariadb: %w", err)
				}

				return container.Terminate, connStr, nil
			},
		},
		{
			name:   "sqlite",
			driver: "sqlite3_regexp",
			startFunc: func(ctx context.Context) (func(context.Context) error, string, error) {
				// Use shared in-memory DB
				connStr := "file:memdb1?mode=memory&cache=shared&_busy_timeout=5000"
				
				
				// Initialize DB
				db, err := sql.Open("sqlite3_regexp", connStr)
				if err != nil {
					return nil, "", err
				}
				// defer db.Close() // Keep open for shared memory DB
				
				script, err := os.ReadFile("./sqlite.sql")
				if err != nil {
					db.Close()
					return nil, "", err
				}
				
				_, err = db.Exec(string(script))
				if err != nil {
					db.Close() // Cleanup on error
					return nil, "", fmt.Errorf("failed to init sqlite: %w", err)
				}

				cleanup := func(ctx context.Context) error {
					return db.Close()
				}
				return cleanup, connStr, nil
			},
		},
		{
			name:   "oracle",
			driver: "oracle",
			startFunc: func(ctx context.Context) (func(context.Context) error, string, error) {
				req := testcontainers.GenericContainerRequest{
					ContainerRequest: testcontainers.ContainerRequest{
						Image:        "gvenzl/oracle-free:23-slim",
						ExposedPorts: []string{"1521/tcp"},
						Env: map[string]string{
							"ORACLE_PASSWORD":    "tester_password",
							"APP_USER":           "tester",
							"APP_USER_PASSWORD":  "tester_password",
						},
						WaitingFor: wait.ForLog("DATABASE IS READY TO USE!"),
					},
					Started: true,
				}
				container, err := testcontainers.GenericContainer(ctx, req)
				if err != nil {
					return nil, "", err
				}

				host, _ := container.Host(ctx)
				port, _ := container.MappedPort(ctx, "1521")

				// Connection string for go-ora
				// oracle://user:password@host:port/service_name
				connStr := fmt.Sprintf("oracle://tester:tester_password@%s:%s/FREEPDB1", host, port.Port())

				// Initialize DB
				db, err := sql.Open("oracle", connStr)
				if err != nil {
					return nil, "", err
				}
				defer db.Close()

				script, err := os.ReadFile("./oracle.sql")
				if err != nil {
					return nil, "", err
				}

				// Oracle SQL files can contain PL/SQL blocks terminated by / on its own line
				// Use regexp to split on / at end of line (with optional following whitespace/newlines)
				plsqlRe := regexp.MustCompile(`(?m)^/\s*$`)
				blocks := plsqlRe.Split(string(script), -1)

				for _, block := range blocks {
					block = strings.TrimSpace(block)
					if block == "" {
						continue
					}
					// Check if this is a PL/SQL block
					// Look for patterns like CREATE FUNCTION, CREATE PROCEDURE, CREATE TYPE, or BEGIN
					// Use word boundaries to avoid matching column names like "subject_type"
					plsqlPatterns := regexp.MustCompile(`(?i)\bCREATE\s+(OR\s+REPLACE\s+)?(FUNCTION|PROCEDURE|TYPE)\b|\bBEGIN\b`)
					isPLSQL := plsqlPatterns.MatchString(block)
					if isPLSQL {
						// Execute the entire block as one statement
						if _, err := db.Exec(block); err != nil {
							return nil, "", fmt.Errorf("failed to init oracle: %w\nSQL: %s", err, block)
						}
					} else {
						// Split by ; for regular statements
						for _, sqlLine := range strings.Split(block, ";") {
							sqlLine = strings.TrimSpace(sqlLine)
							if sqlLine == "" {
								continue
							}
							if _, err := db.Exec(sqlLine); err != nil {
								return nil, "", fmt.Errorf("failed to init oracle: %w\nSQL: %s", err, sqlLine)
							}
						}
					}
				}

				return container.Terminate, connStr, nil
			},
		},
		{
			name:   "mssql",
			driver: "sqlserver",
			startFunc: func(ctx context.Context) (func(context.Context) error, string, error) {
				req := testcontainers.GenericContainerRequest{
					ContainerRequest: testcontainers.ContainerRequest{
						Image:        "mcr.microsoft.com/mssql/server:2022-latest",
						ExposedPorts: []string{"1433/tcp"},
						Env: map[string]string{
							"ACCEPT_EULA":       "Y",
							"MSSQL_SA_PASSWORD": "YourStrong!Passw0rd",
						},
						WaitingFor: wait.ForLog("SQL Server is now ready for client connections").WithStartupTimeout(120 * time.Second),
					},
					Started: true,
				}
				container, err := testcontainers.GenericContainer(ctx, req)
				if err != nil {
					return nil, "", err
				}

				host, _ := container.Host(ctx)
				port, _ := container.MappedPort(ctx, "1433")

				// Connection string for go-mssqldb
				connStr := fmt.Sprintf("sqlserver://sa:YourStrong!Passw0rd@%s:%s?database=master", host, port.Port())

				// Wait for SQL Server to be ready and create database
				var initDB *sql.DB
				for i := 0; i < 60; i++ {
					initDB, err = sql.Open("sqlserver", connStr)
					if err == nil {
						if err = initDB.Ping(); err == nil {
							break
						}
						initDB.Close()
					}
					time.Sleep(1 * time.Second)
				}
				if err != nil {
					return nil, "", fmt.Errorf("failed to connect to mssql: %w", err)
				}

				// Create the test database
				if _, err := initDB.Exec("IF NOT EXISTS (SELECT * FROM sys.databases WHERE name = 'db') CREATE DATABASE db"); err != nil {
					initDB.Close()
					return nil, "", fmt.Errorf("failed to create mssql database: %w", err)
				}
				initDB.Close()

				// Connect to the test database and run init script
				connStr = fmt.Sprintf("sqlserver://sa:YourStrong!Passw0rd@%s:%s?database=db", host, port.Port())
				initDB, err = sql.Open("sqlserver", connStr)
				if err != nil {
					return nil, "", err
				}
				defer initDB.Close()

				script, err := os.ReadFile("./mssql.sql")
				if err != nil {
					return nil, "", err
				}

				// Split by GO statements (MSSQL batch separator)
				goRe := regexp.MustCompile(`(?im)^\s*GO\s*$`)
				blocks := goRe.Split(string(script), -1)

				for _, block := range blocks {
					block = strings.TrimSpace(block)
					if block == "" {
						continue
					}
					if _, err := initDB.Exec(block); err != nil {
						return nil, "", fmt.Errorf("failed to init mssql: %w\nSQL: %s", err, block)
					}
				}

				return container.Terminate, connStr, nil
			},
		},
		{
			name:   "mongodb",
			driver: "mongodb", // Not used since we use dbFunc
			dbFunc: func(ctx context.Context) (func(context.Context) error, *sql.DB, error) {
				container, err := mongodb.Run(ctx, "mongo:7")
				if err != nil {
					return nil, nil, err
				}

				connStr, err := container.ConnectionString(ctx)
				if err != nil {
					return nil, nil, err
				}

				// Connect to MongoDB using the official driver
				client, err := mongo.Connect(options.Client().ApplyURI(connStr))
				if err != nil {
					container.Terminate(ctx)
					return nil, nil, err
				}

				// Wait for MongoDB to be ready
				for i := 0; i < 30; i++ {
					if err := client.Ping(ctx, nil); err == nil {
						break
					}
					time.Sleep(500 * time.Millisecond)
				}

				// Initialize test data
				testDB := client.Database("graphjin_test")

				// Create users collection
				usersCol := testDB.Collection("users")
				var userDocs []interface{}
				for i := 1; i <= 100; i++ {
					disabled := false
					if i == 50 {
						disabled = true
					}
					userDocs = append(userDocs, bson.M{
						"_id":             int64(i),
						"full_name":       fmt.Sprintf("User %d", i),
						"email":           fmt.Sprintf("user%d@test.com", i),
						"phone":           nil,
						"stripe_id":       fmt.Sprintf("payment_id_%d", i+1000),
						"category_counts": []bson.M{{"category_id": 1, "count": 400}, {"category_id": 2, "count": 600}},
						"disabled":        disabled,
						"created_at":      time.Date(2021, 1, 9, 16, 37, 1, 0, time.UTC),
					})
				}
				usersCol.InsertMany(ctx, userDocs)

				// Create categories collection
				categoriesCol := testDB.Collection("categories")
				var categoryDocs []interface{}
				for i := 1; i <= 5; i++ {
					categoryDocs = append(categoryDocs, bson.M{
						"_id":         int64(i),
						"name":        fmt.Sprintf("Category %d", i),
						"description": fmt.Sprintf("Description for category %d", i),
						"created_at":  time.Date(2021, 1, 9, 16, 37, 1, 0, time.UTC),
					})
				}
				categoriesCol.InsertMany(ctx, categoryDocs)

				// Create products collection
				productsCol := testDB.Collection("products")
				var productDocs []interface{}
				tags := []string{"Tag 1", "Tag 2", "Tag 3", "Tag 4", "Tag 5"}
				categoryIDs := []int64{1, 2, 3, 4, 5}
				for i := 1; i <= 100; i++ {
					metadata := bson.M{"bar": true}
					if i%2 == 0 {
						metadata = bson.M{"foo": true}
					}
					productDocs = append(productDocs, bson.M{
						"_id":          int64(i),
						"name":         fmt.Sprintf("Product %d", i),
						"description":  fmt.Sprintf("Description for product %d", i),
						"tags":         tags,
						"metadata":     metadata,
						"country_code": "US",
						"category_ids": categoryIDs,
						"price":        float64(i) + 10.5,
						"owner_id":     int64(i),
						"likes":        []int64{}, // Empty likes array for count_likes aggregation
						"created_at":   time.Date(2021, 1, 9, 16, 37, 1, 0, time.UTC),
					})
				}
				productsCol.InsertMany(ctx, productDocs)

				// Create text index for full-text search on products collection
				_, err = productsCol.Indexes().CreateOne(ctx, mongo.IndexModel{
					Keys: bson.D{{Key: "name", Value: "text"}},
				})
				if err != nil {
					return nil, nil, fmt.Errorf("failed to create text index: %w", err)
				}

				// Create purchases collection
				purchasesCol := testDB.Collection("purchases")
				var purchaseDocs []interface{}
				for i := 1; i <= 100; i++ {
					customerID := int64(i + 1)
					if i >= 100 {
						customerID = 1
					}
					purchaseDocs = append(purchaseDocs, bson.M{
						"_id":         int64(i),
						"customer_id": customerID,
						"product_id":  int64(i),
						"quantity":    i * 10,
						"created_at":  time.Date(2021, 1, 9, 16, 37, 1, 0, time.UTC),
					})
				}
				purchasesCol.InsertMany(ctx, purchaseDocs)

				// Create comments collection
				commentsCol := testDB.Collection("comments")
				var commentDocs []interface{}
				for i := 1; i <= 100; i++ {
					doc := bson.M{
						"_id":          int64(i),
						"body":         fmt.Sprintf("This is comment number %d", i),
						"product_id":   int64(i),
						"commenter_id": int64(i),
						"created_at":   time.Date(2021, 1, 9, 16, 37, 1, 0, time.UTC),
					}
					if i >= 2 {
						doc["reply_to_id"] = int64(i - 1)
					}
					commentDocs = append(commentDocs, doc)
				}
				commentsCol.InsertMany(ctx, commentDocs)

				// Create quotations collection for JSON path tests
				quotationsCol := testDB.Collection("quotations")
				quotationDocs := []interface{}{
					bson.M{
						"_id": int64(1),
						"validity_period": bson.M{
							"issue_date":  "2024-09-15T03:03:16+0000",
							"expiry_date": "2024-10-15T03:03:16+0000",
							"status":      "active",
						},
					},
					bson.M{
						"_id": int64(2),
						"validity_period": bson.M{
							"issue_date":  "2024-09-20T03:03:16+0000",
							"expiry_date": "2024-10-20T03:03:16+0000",
							"status":      "pending",
						},
					},
					bson.M{
						"_id": int64(3),
						"validity_period": bson.M{
							"issue_date":  "2024-09-10T03:03:16+0000",
							"expiry_date": "2024-10-10T03:03:16+0000",
							"status":      "expired",
						},
					},
				}
				quotationsCol.InsertMany(ctx, quotationDocs)

				// Create graph_node collection for self-referencing M2M tests
				graphNodeCol := testDB.Collection("graph_node")
				graphNodeDocs := []interface{}{
					bson.M{"_id": "a", "label": "node a"},
					bson.M{"_id": "b", "label": "node b"},
					bson.M{"_id": "c", "label": "node c"},
				}
				graphNodeCol.InsertMany(ctx, graphNodeDocs)

				// Create graph_edge collection (join table for graph_node M2M)
				graphEdgeCol := testDB.Collection("graph_edge")
				graphEdgeDocs := []interface{}{
					bson.M{"_id": int64(1), "src_node": "a", "dst_node": "b"},
					bson.M{"_id": int64(2), "src_node": "a", "dst_node": "c"},
				}
				graphEdgeCol.InsertMany(ctx, graphEdgeDocs)

				// Create notifications collection for polymorphic relationship tests
				notificationsCol := testDB.Collection("notifications")
				notificationDocs := []interface{}{
					bson.M{
						"_id":          int64(1),
						"verb":         "Joined",
						"subject_type": "users",
						"subject_id":   int64(1),
						"user_id":      int64(1),
						"created_at":   time.Date(2021, 1, 9, 16, 37, 1, 0, time.UTC),
					},
					bson.M{
						"_id":          int64(2),
						"verb":         "Bought",
						"subject_type": "products",
						"subject_id":   int64(2),
						"user_id":      int64(2),
						"created_at":   time.Date(2021, 1, 9, 16, 37, 1, 0, time.UTC),
					},
				}
				notificationsCol.InsertMany(ctx, notificationDocs)

				// Create chats collection for subscription cursor tests
				chatsCol := testDB.Collection("chats")
				var chatDocs []interface{}
				for i := 1; i <= 5; i++ {
					chatDocs = append(chatDocs, bson.M{
						"_id":        int64(i),
						"body":       fmt.Sprintf("This is chat message number %d", i),
						"created_at": time.Date(2021, 1, 9, 16, 37, 1, 0, time.UTC),
					})
				}
				chatsCol.InsertMany(ctx, chatDocs)

				// Create sql.DB using mongodriver
				connector := mongodriver.NewConnector(client, "graphjin_test")
				sqlDB := sql.OpenDB(connector)

				cleanup := func(ctx context.Context) error {
					sqlDB.Close()
					client.Disconnect(ctx)
					return container.Terminate(ctx)
				}

				return cleanup, sqlDB, nil
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

		var cleanup func(context.Context) error
		var err error

		// Use dbFunc if provided (for MongoDB), otherwise use standard startFunc + sql.Open
		if v.dbFunc != nil {
			cleanup, db, err = v.dbFunc(ctx)
			if err != nil {
				panic(err)
			}
		} else {
			var connStr string
			cleanup, connStr, err = v.startFunc(ctx)
			if err != nil {
				panic(err)
			}

			db, err = sql.Open(v.driver, connStr)
			if err != nil {
				_ = cleanup(ctx)
				panic(err)
			}
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
		_ = cleanup(ctx)
		if res != 0 {
			os.Exit(res)
		}
	}
	os.Exit(0)
}

func newConfig(c *core.Config) *core.Config {
	c.DBSchemaPollDuration = -1

	// MongoDB needs explicit relationship configuration since it has no foreign keys
	if c.DBType == "mongodb" {
		mongoTables := []core.Table{
			{
				Name: "products",
				Columns: []core.Column{
					{Name: "owner_id", ForeignKey: "users.id"},
					{Name: "name", FullText: true}, // Enable full-text search on name
				},
			},
			{
				Name: "comments",
				Columns: []core.Column{
					{Name: "product_id", ForeignKey: "products.id"},
					{Name: "commenter_id", ForeignKey: "users.id"},
					{Name: "reply_to_id", ForeignKey: "comments.id"},
				},
			},
			{
				Name: "purchases",
				Columns: []core.Column{
					{Name: "customer_id", ForeignKey: "users.id"},
					{Name: "product_id", ForeignKey: "products.id"},
				},
			},
			{
				Name: "graph_edge",
				Columns: []core.Column{
					{Name: "src_node", ForeignKey: "graph_node.id"},
					{Name: "dst_node", ForeignKey: "graph_node.id"},
				},
			},
			{
				Name: "chats",
			},
		}

		// Merge MongoDB tables with existing tables (avoid duplicates)
		for _, mt := range mongoTables {
			found := false
			for i, t := range c.Tables {
				if t.Name == mt.Name && t.Schema == mt.Schema {
					// Merge columns into existing table
					c.Tables[i].Columns = append(c.Tables[i].Columns, mt.Columns...)
					found = true
					break
				}
			}
			if !found {
				c.Tables = append(c.Tables, mt)
			}
		}
	}

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
