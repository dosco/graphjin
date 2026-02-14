package serv

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"
)

// systemDatabases returns the set of default/system database names for the given DB type.
// Names are stored in lowercase (except Oracle which uses uppercase).
func systemDatabases(dbType string) map[string]bool {
	switch strings.ToLower(dbType) {
	case "postgres":
		return map[string]bool{
			"postgres": true,
		}
	case "mysql", "mariadb":
		return map[string]bool{
			"information_schema": true,
			"mysql":              true,
			"performance_schema": true,
			"sys":                true,
		}
	case "mssql":
		return map[string]bool{
			"master": true,
			"tempdb": true,
			"model":  true,
			"msdb":   true,
		}
	case "oracle":
		return map[string]bool{
			"SYS":    true,
			"SYSTEM": true,
			"DBSNMP": true,
			"OUTLN":  true,
			"XDB":    true,
		}
	case "mongodb":
		return map[string]bool{
			"admin":  true,
			"config": true,
			"local":  true,
		}
	default:
		return nil
	}
}

// isSystemDatabase checks whether dbName is a system/default database for the given type.
// The comparison is case-insensitive (Oracle names are compared uppercase).
func isSystemDatabase(dbType, dbName string) bool {
	sysDBs := systemDatabases(dbType)
	if len(sysDBs) == 0 {
		return false
	}
	normalized := dbName
	if strings.ToLower(dbType) == "oracle" {
		normalized = strings.ToUpper(dbName)
	} else {
		normalized = strings.ToLower(dbName)
	}
	return sysDBs[normalized]
}

// filterSystemDatabases removes system databases from a list.
func filterSystemDatabases(dbType string, names []string) []string {
	sysDBs := systemDatabases(dbType)
	if len(sysDBs) == 0 {
		return names
	}
	isOracle := strings.ToLower(dbType) == "oracle"
	var filtered []string
	for _, n := range names {
		key := n
		if isOracle {
			key = strings.ToUpper(n)
		} else {
			key = strings.ToLower(n)
		}
		if !sysDBs[key] {
			filtered = append(filtered, n)
		}
	}
	return filtered
}

// systemDatabaseError returns a descriptive error message when a system database name is rejected.
func systemDatabaseError(dbType, dbName string) string {
	sysDBs := systemDatabases(dbType)
	var names []string
	for n := range sysDBs {
		names = append(names, n)
	}
	sort.Strings(names)
	return fmt.Sprintf(
		"'%s' is a system database for %s (system databases: %s). "+
			"Choose a different database name, or set mcp.default_db_allowed: true in config to allow system databases.",
		dbName, dbType, strings.Join(names, ", "))
}

// createDatabaseIfNotExists creates a database on the server if it doesn't already exist.
// It delegates to createDatabaseOnServer which uses buildProbeConnString to explicitly
// connect to the correct admin database (e.g. "postgres" for PostgreSQL).
func createDatabaseIfNotExists(conf *Config, log *zap.SugaredLogger) error {
	dbName := conf.DB.DBName
	if dbName == "" {
		return fmt.Errorf("no database name configured")
	}
	return createDatabaseOnServer(
		conf.DBType,
		conf.DB.Host,
		int(conf.DB.Port),
		conf.DB.User,
		conf.DB.Password,
		dbName,
		log,
	)
}

func createPostgresDB(ctx context.Context, db *sql.DB, dbName string) error {
	var exists bool
	if err := db.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)", dbName,
	).Scan(&exists); err != nil {
		return fmt.Errorf("check database existence: %w", err)
	}
	if exists {
		return nil
	}
	// Identifier-quote the name to prevent SQL injection
	quoted := fmt.Sprintf(`"%s"`, strings.ReplaceAll(dbName, `"`, `""`))
	if _, err := db.ExecContext(ctx, "CREATE DATABASE "+quoted); err != nil {
		return fmt.Errorf("CREATE DATABASE: %w", err)
	}
	return nil
}

func createMysqlDB(ctx context.Context, db *sql.DB, dbName string) error {
	quoted := "`" + strings.ReplaceAll(dbName, "`", "``") + "`"
	if _, err := db.ExecContext(ctx, "CREATE DATABASE IF NOT EXISTS "+quoted); err != nil {
		return fmt.Errorf("CREATE DATABASE: %w", err)
	}
	return nil
}

func createMssqlDB(ctx context.Context, db *sql.DB, dbName string) error {
	quoted := "[" + strings.ReplaceAll(dbName, "]", "]]") + "]"
	stmt := fmt.Sprintf(
		"IF NOT EXISTS (SELECT * FROM sys.databases WHERE name = '%s') CREATE DATABASE %s",
		strings.ReplaceAll(dbName, "'", "''"), quoted)
	if _, err := db.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("CREATE DATABASE: %w", err)
	}
	return nil
}

// createDatabaseOnServer creates a database on a specific server using explicit connection params.
// It opens its own admin connection, creates the DB, and closes the connection.
// This allows creating databases on any configured server independently of conf.DB.
func createDatabaseOnServer(dbType, host string, port int, user, password, dbName string, log *zap.SugaredLogger) error {
	if dbName == "" {
		return fmt.Errorf("no database name provided")
	}

	dbType = strings.ToLower(dbType)

	// SQLite and MongoDB create databases automatically
	switch dbType {
	case "sqlite", "mongodb":
		return nil
	}

	// Build a probe connection to the "postgres" admin database (for PostgreSQL)
	// or without selecting a specific database (for other types)
	adminDBName := ""
	if dbType == "postgres" || dbType == "" {
		adminDBName = "postgres"
	}
	driverName, connString := buildProbeConnString(dbType, host, port, "", user, password, "tcp", adminDBName)
	if connString == "" {
		return fmt.Errorf("unsupported database type for create: %s", dbType)
	}

	adminDB, err := tryConnect(driverName, connString)
	if err != nil {
		return fmt.Errorf("admin connection failed: %w", err)
	}
	defer adminDB.Close() //nolint:errcheck

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	switch dbType {
	case "postgres", "":
		return createPostgresDB(ctx, adminDB, dbName)
	case "mysql", "mariadb":
		return createMysqlDB(ctx, adminDB, dbName)
	case "mssql":
		return createMssqlDB(ctx, adminDB, dbName)
	case "oracle":
		return createOracleDB(ctx, adminDB, dbName)
	default:
		return fmt.Errorf("create_if_not_exists not supported for %s", dbType)
	}
}

// testDatabaseConnection tests connectivity to a database server using explicit params.
// Returns the list of databases found on the server (if any) and any error.
func testDatabaseConnection(dbType, host string, port int, user, password, dbName string) ([]string, error) {
	dbType = strings.ToLower(dbType)

	// SQLite: just check the file exists (or will be created)
	if dbType == "sqlite" {
		return nil, nil
	}

	// MongoDB: use native driver
	if dbType == "mongodb" {
		connString := fmt.Sprintf("mongodb://%s:%d/?timeoutMS=3000", host, port)
		if user != "" {
			connString = fmt.Sprintf("mongodb://%s:%s@%s:%d/?timeoutMS=3000",
				user, password, host, port)
		}
		names, err := probeMongoDB(connString)
		return names, err
	}

	// SQL databases
	driverName, connString := buildProbeConnString(dbType, host, port, "", user, password, "tcp", dbName)
	if connString == "" {
		return nil, fmt.Errorf("unsupported database type: %s", dbType)
	}

	sqlDB, err := tryConnect(driverName, connString)
	if err != nil {
		return nil, err
	}
	defer sqlDB.Close()

	names, _ := listDatabaseNames(sqlDB, dbType)
	return names, nil
}

func createOracleDB(ctx context.Context, db *sql.DB, dbName string) error {
	upperName := strings.ToUpper(dbName)
	quoted := `"` + strings.ReplaceAll(upperName, `"`, `""`) + `"`

	// Check if user/schema already exists
	var count int
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM all_users WHERE username = :1", upperName,
	).Scan(&count); err != nil {
		return fmt.Errorf("check user existence: %w", err)
	}
	if count > 0 {
		return nil
	}
	if _, err := db.ExecContext(ctx,
		fmt.Sprintf("CREATE USER %s IDENTIFIED BY %s", quoted, quoted)); err != nil {
		return fmt.Errorf("CREATE USER: %w", err)
	}
	if _, err := db.ExecContext(ctx,
		fmt.Sprintf("GRANT CONNECT, RESOURCE TO %s", quoted)); err != nil {
		return fmt.Errorf("GRANT: %w", err)
	}
	return nil
}
