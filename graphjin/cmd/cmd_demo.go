package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/dosco/graphjin/core/v3"
	"github.com/mattn/go-sqlite3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"github.com/testcontainers/testcontainers-go/modules/mysql"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

var (
	demoPersist  bool               // --persist: Use Docker volumes for data persistence
	demoDBFlags  []string           // --db: Can be used multiple times
	multiDBConns map[string]*sql.DB // Store multi-DB connections for migrations
)

// StartDemo starts demo containers, runs migrations and seeds.
// Returns cleanup functions for graceful shutdown.
func StartDemo(ctx context.Context, persist bool, dbFlags []string) ([]func(context.Context) error, error) {
	primaryType, dbOverrides := parseDBFlags(dbFlags)

	var cleanups []func(context.Context) error

	// Check if multi-database mode (conf.Core.Databases is populated)
	if len(conf.Databases) > 0 {
		// Multi-database mode
		var err error
		cleanups, err = startMultiDBDemo(ctx, primaryType, dbOverrides, persist)
		if err != nil {
			return nil, fmt.Errorf("failed to start containers: %w", err)
		}
	} else {
		// Single-database mode
		dbType := conf.DB.Type
		if primaryType != "" {
			dbType = primaryType
		}
		if dbType == "" {
			dbType = "postgres" // Default
		}

		log.Infof("Starting %s container...", dbType)
		cleanup, connInfo, err := startDemoContainer(ctx, dbType, persist)
		if err != nil {
			return nil, fmt.Errorf("failed to start container: %w", err)
		}

		log.Infof("Container started successfully")
		cleanups = append(cleanups, cleanup)

		// Override config with container connection
		applyContainerConfig(connInfo)
	}

	// Initialize database connection
	initDB(true)

	// Run migrations if available
	runDemoMigrations()

	// Run seed script if available
	runDemoSeed()

	return cleanups, nil
}

// DemoConnInfo holds database connection information
type DemoConnInfo struct {
	Host     string
	Port     uint16
	User     string
	Password string
	DBName   string
	ConnStr  string // Full connection string for databases that need it
	Type     string // Database type
}

// parseDBFlags parses --db flags into a map of database name -> type
// Returns: primary type (if any), per-name overrides map
func parseDBFlags(flags []string) (primaryType string, overrides map[string]string) {
	overrides = make(map[string]string)
	for _, flag := range flags {
		if strings.Contains(flag, "=") {
			parts := strings.SplitN(flag, "=", 2)
			overrides[parts[0]] = parts[1] // name=type
		} else {
			primaryType = flag // Just a type, e.g., "mysql"
		}
	}
	return
}


// startDemoContainer starts the appropriate database container based on type
func startDemoContainer(ctx context.Context, dbType string, persist bool) (
	cleanup func(context.Context) error,
	connInfo *DemoConnInfo,
	err error,
) {
	switch strings.ToLower(dbType) {
	case "postgres", "postgresql":
		return startPostgresDemo(ctx, persist)
	case "mysql":
		return startMySQLDemo(ctx, persist)
	case "mariadb":
		return startMariaDBDemo(ctx, persist)
	case "sqlite", "sqlite3":
		return startSQLiteDemo(ctx, persist)
	case "oracle":
		return startOracleDemo(ctx, persist)
	case "mssql", "sqlserver":
		return startMSSQLDemo(ctx, persist)
	case "mongodb", "mongo":
		return startMongoDBDemo(ctx, persist)
	default:
		return nil, nil, fmt.Errorf("unsupported database type: %s", dbType)
	}
}

// withVolumeMounts creates a customizer that adds volume mounts to the container
func withVolumeMounts(mounts testcontainers.ContainerMounts) testcontainers.CustomizeRequestOption {
	return func(req *testcontainers.GenericContainerRequest) error {
		req.Mounts = append(req.Mounts, mounts...)
		return nil
	}
}

// startPostgresDemo starts a PostgreSQL container
func startPostgresDemo(ctx context.Context, persist bool) (func(context.Context) error, *DemoConnInfo, error) {
	opts := []testcontainers.ContainerCustomizer{
		postgres.WithUsername("graphjin"),
		postgres.WithPassword("graphjin"),
		postgres.WithDatabase("graphjin_demo"),
	}

	if persist {
		opts = append(opts, withVolumeMounts(testcontainers.ContainerMounts{
			{
				Source: testcontainers.DockerVolumeMountSource{Name: "graphjin-demo-postgres"},
				Target: "/var/lib/postgresql/data",
			},
		}))
	}

	container, err := postgres.Run(ctx, "postgres:15", opts...)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to start postgres container: %w", err)
	}

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		container.Terminate(ctx) //nolint:errcheck
		return nil, nil, fmt.Errorf("failed to get connection string: %w", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		container.Terminate(ctx) //nolint:errcheck
		return nil, nil, err
	}

	port, err := container.MappedPort(ctx, "5432")
	if err != nil {
		container.Terminate(ctx) //nolint:errcheck
		return nil, nil, err
	}

	log.Infof("PostgreSQL running on %s:%s", host, port.Port())

	// Wait for database to be fully ready
	for i := 0; i < 30; i++ {
		testDB, err := sql.Open("postgres", connStr)
		if err == nil {
			if err = testDB.Ping(); err == nil {
				testDB.Close() //nolint:errcheck
				break
			}
			testDB.Close() //nolint:errcheck
		}
		time.Sleep(500 * time.Millisecond)
	}

	return container.Terminate, &DemoConnInfo{
		Host:     host,
		Port:     uint16(port.Int()),
		User:     "graphjin",
		Password: "graphjin",
		DBName:   "graphjin_demo",
		ConnStr:  connStr,
		Type:     "postgres",
	}, nil
}

// startMySQLDemo starts a MySQL container
func startMySQLDemo(ctx context.Context, persist bool) (func(context.Context) error, *DemoConnInfo, error) {
	opts := []testcontainers.ContainerCustomizer{
		mysql.WithUsername("graphjin"),
		mysql.WithPassword("graphjin"),
		mysql.WithDatabase("graphjin_demo"),
	}

	if persist {
		opts = append(opts, withVolumeMounts(testcontainers.ContainerMounts{
			{
				Source: testcontainers.DockerVolumeMountSource{Name: "graphjin-demo-mysql"},
				Target: "/var/lib/mysql",
			},
		}))
	}

	container, err := mysql.Run(ctx, "mysql:8.0", opts...)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to start mysql container: %w", err)
	}

	connStr, err := container.ConnectionString(ctx)
	if err != nil {
		container.Terminate(ctx) //nolint:errcheck
		return nil, nil, fmt.Errorf("failed to get connection string: %w", err)
	}

	// Add required parameters
	if strings.Contains(connStr, "?") {
		connStr += "&multiStatements=true&parseTime=true&interpolateParams=true"
	} else {
		connStr += "?multiStatements=true&parseTime=true&interpolateParams=true"
	}

	host, err := container.Host(ctx)
	if err != nil {
		container.Terminate(ctx) //nolint:errcheck
		return nil, nil, err
	}

	port, err := container.MappedPort(ctx, "3306")
	if err != nil {
		container.Terminate(ctx) //nolint:errcheck
		return nil, nil, err
	}

	log.Infof("MySQL running on %s:%s", host, port.Port())

	return container.Terminate, &DemoConnInfo{
		Host:     host,
		Port:     uint16(port.Int()),
		User:     "graphjin",
		Password: "graphjin",
		DBName:   "graphjin_demo",
		ConnStr:  connStr,
		Type:     "mysql",
	}, nil
}

// startMariaDBDemo starts a MariaDB container
func startMariaDBDemo(ctx context.Context, persist bool) (func(context.Context) error, *DemoConnInfo, error) {
	// Use GenericContainer instead of mysql.Run because the MySQL helper
	// has a wait strategy that doesn't recognize MariaDB's log format
	req := testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "mariadb:10.11",
			ExposedPorts: []string{"3306/tcp"},
			Env: map[string]string{
				"MYSQL_ROOT_PASSWORD": "root",
				"MYSQL_DATABASE":      "graphjin_demo",
				"MYSQL_USER":          "graphjin",
				"MYSQL_PASSWORD":      "graphjin",
			},
			WaitingFor: wait.ForLog("ready for connections").WithStartupTimeout(120 * time.Second),
		},
		Started: true,
	}

	if persist {
		req.Mounts = testcontainers.ContainerMounts{
			{
				Source: testcontainers.DockerVolumeMountSource{Name: "graphjin-demo-mariadb"},
				Target: "/var/lib/mysql",
			},
		}
	}

	container, err := testcontainers.GenericContainer(ctx, req)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to start mariadb container: %w", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		container.Terminate(ctx) //nolint:errcheck
		return nil, nil, err
	}

	port, err := container.MappedPort(ctx, "3306")
	if err != nil {
		container.Terminate(ctx) //nolint:errcheck
		return nil, nil, err
	}

	connStr := fmt.Sprintf("graphjin:graphjin@tcp(%s:%s)/graphjin_demo?multiStatements=true&parseTime=true&interpolateParams=true",
		host, port.Port())

	// Wait for database to be fully ready
	for i := 0; i < 30; i++ {
		testDB, err := sql.Open("mysql", connStr)
		if err == nil {
			if err = testDB.Ping(); err == nil {
				testDB.Close() //nolint:errcheck
				break
			}
			testDB.Close() //nolint:errcheck
		}
		time.Sleep(500 * time.Millisecond)
	}

	log.Infof("MariaDB running on %s:%s", host, port.Port())

	return container.Terminate, &DemoConnInfo{
		Host:     host,
		Port:     uint16(port.Int()),
		User:     "graphjin",
		Password: "graphjin",
		DBName:   "graphjin_demo",
		ConnStr:  connStr,
		Type:     "mysql", // MariaDB uses MySQL wire protocol
	}, nil
}

// startSQLiteDemo sets up an SQLite database (no container needed)
func startSQLiteDemo(ctx context.Context, persist bool) (func(context.Context) error, *DemoConnInfo, error) {
	var connStr string

	if persist {
		// Use file-based database for persistence
		dbPath := filepath.Join(cpath, "graphjin_demo.db")
		connStr = fmt.Sprintf("file:%s?cache=shared&_busy_timeout=5000", dbPath)
		log.Infof("SQLite using file: %s", dbPath)
	} else {
		// Use shared in-memory database
		connStr = "file:memdb1?mode=memory&cache=shared&_busy_timeout=5000"
		log.Info("SQLite using in-memory database")
	}

	// Register the sqlite3_regexp driver if not already registered
	registerSQLite3Regexp()

	// Open database to verify it works
	testDB, err := sql.Open("sqlite3_regexp", connStr)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	cleanup := func(ctx context.Context) error {
		return testDB.Close()
	}

	return cleanup, &DemoConnInfo{
		ConnStr: connStr,
		Type:    "sqlite",
	}, nil
}

// sqlite3RegexpRegistered tracks if we've registered the custom driver
var sqlite3RegexpRegistered bool

// registerSQLite3Regexp registers the sqlite3_regexp driver with REGEXP support
func registerSQLite3Regexp() {
	if sqlite3RegexpRegistered {
		return
	}

	sql.Register("sqlite3_regexp", &sqlite3.SQLiteDriver{
		ConnectHook: func(conn *sqlite3.SQLiteConn) error {
			// Register REGEXP function
			if err := conn.RegisterFunc("REGEXP", func(re, s string) (bool, error) {
				return regexp.MatchString(re, s)
			}, true); err != nil {
				return err
			}
			if err := conn.RegisterFunc("regexp", func(re, s string) (bool, error) {
				return regexp.MatchString(re, s)
			}, true); err != nil {
				return err
			}
			return nil
		},
	})
	sqlite3RegexpRegistered = true
}

// startOracleDemo starts an Oracle container
func startOracleDemo(ctx context.Context, persist bool) (func(context.Context) error, *DemoConnInfo, error) {
	req := testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "gvenzl/oracle-free:23-slim",
			ExposedPorts: []string{"1521/tcp"},
			Env: map[string]string{
				"ORACLE_PASSWORD":   "graphjin_password",
				"APP_USER":          "graphjin",
				"APP_USER_PASSWORD": "graphjin_password",
			},
			WaitingFor: wait.ForLog("DATABASE IS READY TO USE!").WithStartupTimeout(300 * time.Second),
		},
		Started: true,
	}

	if persist {
		req.Mounts = testcontainers.ContainerMounts{
			{
				Source: testcontainers.DockerVolumeMountSource{Name: "graphjin-demo-oracle"},
				Target: "/opt/oracle/oradata",
			},
		}
	}

	container, err := testcontainers.GenericContainer(ctx, req)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to start oracle container: %w", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		container.Terminate(ctx) //nolint:errcheck
		return nil, nil, err
	}

	port, err := container.MappedPort(ctx, "1521")
	if err != nil {
		container.Terminate(ctx) //nolint:errcheck
		return nil, nil, err
	}

	// Connection string for go-ora
	connStr := fmt.Sprintf("oracle://graphjin:graphjin_password@%s:%s/FREEPDB1", host, port.Port())

	log.Infof("Oracle running on %s:%s", host, port.Port())

	return container.Terminate, &DemoConnInfo{
		Host:     host,
		Port:     uint16(port.Int()),
		User:     "graphjin",
		Password: "graphjin_password",
		DBName:   "FREEPDB1",
		ConnStr:  connStr,
		Type:     "oracle",
	}, nil
}

// startMSSQLDemo starts a Microsoft SQL Server container
func startMSSQLDemo(ctx context.Context, persist bool) (func(context.Context) error, *DemoConnInfo, error) {
	req := testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "mcr.microsoft.com/mssql/server:2022-latest",
			ExposedPorts: []string{"1433/tcp"},
			Env: map[string]string{
				"ACCEPT_EULA":       "Y",
				"MSSQL_SA_PASSWORD": "GraphJin!Passw0rd",
			},
			WaitingFor: wait.ForLog("SQL Server is now ready for client connections").WithStartupTimeout(120 * time.Second),
		},
		Started: true,
	}

	if persist {
		req.Mounts = testcontainers.ContainerMounts{
			{
				Source: testcontainers.DockerVolumeMountSource{Name: "graphjin-demo-mssql"},
				Target: "/var/opt/mssql",
			},
		}
	}

	container, err := testcontainers.GenericContainer(ctx, req)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to start mssql container: %w", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		container.Terminate(ctx) //nolint:errcheck
		return nil, nil, err
	}

	port, err := container.MappedPort(ctx, "1433")
	if err != nil {
		container.Terminate(ctx) //nolint:errcheck
		return nil, nil, err
	}

	// Connect to master and create the database
	masterConnStr := fmt.Sprintf("sqlserver://sa:GraphJin!Passw0rd@%s:%s?database=master", host, port.Port())

	// Wait for SQL Server to be fully ready
	var initDB *sql.DB
	for i := 0; i < 60; i++ {
		initDB, err = sql.Open("sqlserver", masterConnStr)
		if err == nil {
			if err = initDB.Ping(); err == nil {
				break
			}
			initDB.Close() //nolint:errcheck
		}
		time.Sleep(1 * time.Second)
	}
	if err != nil {
		container.Terminate(ctx) //nolint:errcheck
		return nil, nil, fmt.Errorf("failed to connect to mssql: %w", err)
	}

	// Create the demo database
	_, err = initDB.Exec("IF NOT EXISTS (SELECT * FROM sys.databases WHERE name = 'graphjin_demo') CREATE DATABASE graphjin_demo")
	if err != nil {
		initDB.Close() //nolint:errcheck
		container.Terminate(ctx) //nolint:errcheck
		return nil, nil, fmt.Errorf("failed to create mssql database: %w", err)
	}
	initDB.Close() //nolint:errcheck

	// Connection string for the demo database
	connStr := fmt.Sprintf("sqlserver://sa:GraphJin!Passw0rd@%s:%s?database=graphjin_demo", host, port.Port())

	log.Infof("SQL Server running on %s:%s", host, port.Port())

	return container.Terminate, &DemoConnInfo{
		Host:     host,
		Port:     uint16(port.Int()),
		User:     "sa",
		Password: "GraphJin!Passw0rd",
		DBName:   "graphjin_demo",
		ConnStr:  connStr,
		Type:     "mssql",
	}, nil
}

// startMongoDBDemo starts a MongoDB container
func startMongoDBDemo(ctx context.Context, persist bool) (func(context.Context) error, *DemoConnInfo, error) {
	opts := []testcontainers.ContainerCustomizer{}

	if persist {
		opts = append(opts, withVolumeMounts(testcontainers.ContainerMounts{
			{
				Source: testcontainers.DockerVolumeMountSource{Name: "graphjin-demo-mongodb"},
				Target: "/data/db",
			},
		}))
	}

	container, err := mongodb.Run(ctx, "mongo:7", opts...)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to start mongodb container: %w", err)
	}

	connStr, err := container.ConnectionString(ctx)
	if err != nil {
		container.Terminate(ctx) //nolint:errcheck
		return nil, nil, fmt.Errorf("failed to get connection string: %w", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		container.Terminate(ctx) //nolint:errcheck
		return nil, nil, err
	}

	port, err := container.MappedPort(ctx, "27017")
	if err != nil {
		container.Terminate(ctx) //nolint:errcheck
		return nil, nil, err
	}

	log.Infof("MongoDB running on %s:%s", host, port.Port())

	return container.Terminate, &DemoConnInfo{
		Host:    host,
		Port:    uint16(port.Int()),
		DBName:  "graphjin_demo",
		ConnStr: connStr,
		Type:    "mongodb",
	}, nil
}

// applyContainerConfig updates the configuration with container connection info
func applyContainerConfig(connInfo *DemoConnInfo) {
	conf.DB.Type = connInfo.Type
	conf.DB.Host = connInfo.Host
	conf.DB.Port = connInfo.Port
	conf.DB.User = connInfo.User
	conf.DB.Password = connInfo.Password
	conf.DB.DBName = connInfo.DBName
	conf.DB.ConnString = connInfo.ConnStr
}

// startMultiDBDemo starts containers for all databases in the config
func startMultiDBDemo(ctx context.Context, primaryType string, overrides map[string]string, persist bool) (
	cleanups []func(context.Context) error, err error,
) {
	// Initialize connections map
	multiDBConns = make(map[string]*sql.DB)

	// Iterate over conf.Core.Databases
	for name, dbConf := range conf.Databases {
		dbType := dbConf.Type

		// Check for override
		if override, ok := overrides[name]; ok {
			dbType = override
		}

		if dbType == "" {
			dbType = "postgres" // Default
		}

		log.Infof("Starting %s container for database '%s'...", dbType, name)

		cleanup, connInfo, err := startDemoContainer(ctx, dbType, persist)
		if err != nil {
			// Cleanup already started containers
			cleanupAll(ctx, cleanups)
			return nil, fmt.Errorf("failed to start %s: %w", name, err)
		}
		cleanups = append(cleanups, cleanup)

		// Update the config with container connection info
		applyMultiDBContainerConfig(name, connInfo)

		// Open database connection for migrations
		conn, err := openDemoConnection(connInfo)
		if err != nil {
			log.Warnf("Failed to open connection for '%s': %s", name, err)
		} else {
			multiDBConns[name] = conn
		}

		log.Infof("Container for '%s' started successfully", name)
	}
	return cleanups, nil
}

// openDemoConnection opens a database connection based on DemoConnInfo
func openDemoConnection(connInfo *DemoConnInfo) (*sql.DB, error) {
	var driverName string
	switch strings.ToLower(connInfo.Type) {
	case "postgres", "postgresql":
		driverName = "postgres"
	case "mysql", "mariadb":
		driverName = "mysql"
	case "sqlite", "sqlite3":
		driverName = "sqlite3_regexp"
	case "mssql", "sqlserver":
		driverName = "sqlserver"
	case "oracle":
		driverName = "oracle"
	default:
		return nil, fmt.Errorf("unsupported database type: %s", connInfo.Type)
	}

	conn, err := sql.Open(driverName, connInfo.ConnStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open connection: %w", err)
	}

	if err := conn.Ping(); err != nil {
		conn.Close() //nolint:errcheck
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return conn, nil
}

// applyMultiDBContainerConfig updates a specific database in the Databases map
func applyMultiDBContainerConfig(name string, connInfo *DemoConnInfo) {
	if conf.Databases == nil {
		conf.Databases = make(map[string]core.DatabaseConfig)
	}

	existing := conf.Databases[name]
	existing.Type = connInfo.Type
	existing.Host = connInfo.Host
	existing.Port = int(connInfo.Port)
	existing.User = connInfo.User
	existing.Password = connInfo.Password
	existing.DBName = connInfo.DBName
	existing.ConnString = connInfo.ConnStr
	conf.Databases[name] = existing
}

// cleanupAll terminates all containers
func cleanupAll(ctx context.Context, cleanups []func(context.Context) error) {
	for _, cleanup := range cleanups {
		if cleanup != nil {
			cleanup(ctx) //nolint:errcheck // Best effort, ignore errors
		}
	}
}

// runDemoMigrations syncs the database schema from db.graphql
func runDemoMigrations() {
	// Multi-DB mode
	if len(conf.Databases) > 0 && len(multiDBConns) > 0 {
		runDemoMigrationsMultiDB()
		return
	}

	if conf.DB.Type == "mongodb" {
		log.Info("Schema sync not applicable for MongoDB, skipping")
		return
	}

	// Check if db.graphql exists
	schemaPath := filepath.Join(cpath, "db.graphql")
	schemaBytes, err := os.ReadFile(schemaPath)
	if os.IsNotExist(err) {
		log.Info("No db.graphql found, skipping schema sync")
		return
	}
	if err != nil {
		log.Warnf("Error reading db.graphql: %s", err)
		return
	}

	log.Infof("Syncing schema from %s", schemaPath)

	// Compute schema diff
	opts := core.DiffOptions{Destructive: false}
	ops, err := core.SchemaDiff(db, conf.DB.Type, schemaBytes, conf.Blocklist, opts)
	if err != nil {
		log.Warnf("Error computing schema diff: %s", err)
		return
	}

	if len(ops) == 0 {
		log.Info("Schema is already in sync")
		return
	}

	// Apply changes
	sqls := core.GenerateDiffSQL(ops)
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		log.Warnf("Error starting transaction: %s", err)
		return
	}

	for _, sqlStmt := range sqls {
		if _, err := tx.ExecContext(ctx, sqlStmt); err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Warnf("Rollback failed: %s", rbErr)
			}
			log.Warnf("Error applying schema: %s", err)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		log.Warnf("Error committing schema changes: %s", err)
		return
	}

	log.Infof("Schema sync completed (%d changes applied)", len(ops))
}

// runDemoMigrationsMultiDB syncs schemas for all databases in multi-DB mode
func runDemoMigrationsMultiDB() {
	schemaPath := filepath.Join(cpath, "db.graphql")
	schemaBytes, err := os.ReadFile(schemaPath)
	if os.IsNotExist(err) {
		log.Info("No db.graphql found, skipping schema sync")
		return
	}
	if err != nil {
		log.Warnf("Error reading db.graphql: %s", err)
		return
	}

	log.Infof("Syncing schemas from %s (multi-database mode)", schemaPath)

	// Build dbTypes map from config
	dbTypes := make(map[string]string)
	for name, dbConf := range conf.Databases {
		dbTypes[name] = dbConf.Type
	}

	// Compute schema diff for all databases
	opts := core.DiffOptions{Destructive: false}
	results, err := core.SchemaDiffMultiDB(multiDBConns, dbTypes, schemaBytes, conf.Blocklist, opts)
	if err != nil {
		log.Warnf("Error computing schema diff: %s", err)
		return
	}

	// Apply changes per database
	totalChanges := 0
	for dbName, ops := range results {
		if len(ops) == 0 {
			continue
		}

		sqls := core.GenerateDiffSQL(ops)
		conn := multiDBConns[dbName]

		ctx := context.Background()
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			log.Warnf("Error starting transaction for %s: %s", dbName, err)
			continue
		}

		var failed bool
		for _, sqlStmt := range sqls {
			if _, err := tx.ExecContext(ctx, sqlStmt); err != nil {
				if rbErr := tx.Rollback(); rbErr != nil {
					log.Warnf("Rollback failed for %s: %s", dbName, rbErr)
				}
				log.Warnf("Error applying schema to %s: %s", dbName, err)
				failed = true
				break
			}
		}

		if !failed {
			if err := tx.Commit(); err != nil {
				log.Warnf("Error committing schema changes to %s: %s", dbName, err)
			} else {
				log.Infof("[%s] Schema sync completed (%d changes)", dbName, len(ops))
				totalChanges += len(ops)
			}
		}
	}

	if totalChanges == 0 {
		log.Info("All schemas are already in sync")
	} else {
		log.Infof("Multi-DB schema sync completed (%d total changes)", totalChanges)
	}
}

// runDemoSeed runs the seed script if available
func runDemoSeed() {
	if conf.DB.Type == "mysql" {
		log.Warn("Seed scripts not supported with MySQL, skipping")
		return
	}

	if conf.DB.Type == "mongodb" {
		log.Info("Seed scripts not applicable for MongoDB, skipping")
		return
	}

	seedPath := filepath.Join(cpath, "seed.js")
	if _, err := os.Stat(seedPath); os.IsNotExist(err) {
		log.Info("No seed.js found, skipping")
		return
	}

	log.Infof("Running seed script from %s", seedPath)

	// Disable production mode and blocklist for seeding
	conf.Serv.Production = false
	conf.DefaultBlock = false
	conf.DisableAllowList = true
	conf.DBSchemaPollDuration = -1
	conf.Blocklist = nil

	if err := compileAndRunJS(seedPath, db, cpath); err != nil {
		log.Warnf("Failed to execute seed file: %s", err)
		return
	}

	log.Info("Seed script completed")
}
