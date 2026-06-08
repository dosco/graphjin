package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/hostedemu"
	hostedbigquery "github.com/dosco/graphjin/hostedemu/bigquery"
	"github.com/mattn/go-sqlite3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"github.com/testcontainers/testcontainers-go/modules/mysql"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

var (
	multiDBConns map[string]*sql.DB // Store multi-DB connections for migrations
	currentDemo  *demoState
)

const demoManifestVersion = 1

type DemoRuntime struct {
	Cleanups  []func(context.Context) error
	Databases map[string]*sql.DB
	Status    demoStatus
}

type demoStatus struct {
	out io.Writer
}

func (s demoStatus) Emit(source, status, msg string) {
	if s.out == nil {
		return
	}
	if strings.TrimSpace(source) == "" {
		source = "demo"
	}
	msg = cleanDemoStatusMessage(msg)
	fmt.Fprintf(s.out, "demo %-18s %-9s %s\n", source, status, msg)
}

func cleanDemoStatusMessage(msg string) string {
	msg = strings.ReplaceAll(msg, "\r\n", "; ")
	msg = strings.ReplaceAll(msg, "\n", "; ")
	return msg
}

func demoRuntimeSchemaDDLDir() string {
	return filepath.ToSlash(filepath.Join("demo", core.SourceSchemaDDLDir))
}

type demoState struct {
	Dir      string
	FirstRun bool
	Manifest demoManifest
	Status   demoStatus
}

type demoManifest struct {
	Version    int                         `json:"version"`
	ConfigHash string                      `json:"config_hash"`
	CreatedAt  string                      `json:"created_at"`
	UpdatedAt  string                      `json:"updated_at"`
	Sources    map[string]demoManifestItem `json:"sources"`
}

type demoManifestItem struct {
	Type   string `json:"type"`
	Status string `json:"status"`
}

// StartDemo starts demo backing services, syncs schema, and seeds first-run data.
func StartDemo(ctx context.Context, dbFlags []string, statusOut io.Writer) (*DemoRuntime, error) {
	status := demoStatus{out: statusOut}
	if err := conf.Core.NormalizeSources(); err != nil {
		return nil, err
	}

	state, err := initDemoState(status)
	if err != nil {
		return nil, err
	}
	currentDemo = state

	primaryType, dbOverrides := parseDBFlags(dbFlags)

	runtime := &DemoRuntime{
		Databases: make(map[string]*sql.DB),
		Status:    status,
	}

	// Check if multi-database mode (conf.Core.Databases is populated)
	if len(conf.Databases) > 0 {
		// Multi-database mode
		cleanups, dbs, err := startMultiDBDemo(ctx, primaryType, dbOverrides, state)
		if err != nil {
			return nil, fmt.Errorf("failed to start demo sources: %w", err)
		}
		runtime.Cleanups = append(runtime.Cleanups, cleanups...)
		for name, db := range dbs {
			runtime.Databases[name] = db
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

		status.Emit("database", "starting", fmt.Sprintf("starting %s", dbType))
		cleanup, connInfo, err := startDemoContainer(ctx, core.DefaultDBName, core.DatabaseConfig{Type: dbType}, state)
		if err != nil {
			return nil, fmt.Errorf("failed to start demo database: %w", err)
		}

		runtime.Cleanups = append(runtime.Cleanups, cleanup)

		// Override config with container connection
		applyContainerConfig(connInfo)

		conn, err := openDemoConnection(connInfo)
		if err != nil {
			cleanupAll(ctx, runtime.Cleanups)
			return nil, err
		}
		db = conn
		dbOpened = true
		runtime.Databases[core.DefaultDBName] = conn
	}

	status.Emit("schema", "syncing", "syncing demo schema")
	runDemoMigrations()

	status.Emit("seed", "seeding", "loading first-run data")
	runDemoSeed()

	status.Emit("schema-cache", "writing", "writing discovered DDL cache")
	writeDemoSchemaCache(runtime.Databases, state, status)

	if err := state.writeManifest(runtime.Databases); err != nil {
		return nil, err
	}

	status.Emit("workflows", "loading", "loading workflow scripts")
	if err := verifyDemoWorkflows(status); err != nil {
		return nil, err
	}
	status.Emit("GraphJin", "starting", "initializing service and managed indexes")

	return runtime, nil
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
	DB       *sql.DB
}

func initDemoState(status demoStatus) (*demoState, error) {
	stateDir := filepath.Join(cpath, "demo")
	if abs, err := filepath.Abs(stateDir); err == nil {
		stateDir = abs
	}
	status.Emit("state", "locating", stateDir)

	manifestPath := filepath.Join(stateDir, "manifest.json")
	st, err := os.Stat(stateDir)
	if os.IsNotExist(err) {
		status.Emit("state", "created", "creating local demo state")
		if err := os.MkdirAll(stateDir, 0o755); err != nil {
			status.Emit("state", "failed", err.Error())
			return nil, err
		}
		return &demoState{
			Dir:      stateDir,
			FirstRun: true,
			Manifest: demoManifest{
				Version:    demoManifestVersion,
				ConfigHash: demoConfigHash(),
				CreatedAt:  time.Now().UTC().Format(time.RFC3339),
				Sources:    make(map[string]demoManifestItem),
			},
			Status: status,
		}, nil
	}
	if err != nil {
		status.Emit("state", "failed", err.Error())
		return nil, err
	}
	if !st.IsDir() {
		err := fmt.Errorf("demo state path is not a directory; delete %s to recreate from scratch", stateDir)
		status.Emit("state", "failed", err.Error())
		return nil, err
	}

	var manifest demoManifest
	data, err := os.ReadFile(manifestPath)
	if os.IsNotExist(err) {
		err := fmt.Errorf("demo state is invalid; delete %s to recreate from scratch", stateDir)
		status.Emit("state", "failed", err.Error())
		return nil, err
	}
	if err != nil {
		status.Emit("state", "failed", err.Error())
		return nil, err
	}
	if err := json.Unmarshal(data, &manifest); err != nil || manifest.Version != demoManifestVersion {
		if err == nil {
			err = fmt.Errorf("unsupported manifest version %d", manifest.Version)
		}
		err = fmt.Errorf("demo state is invalid (%s); delete %s to recreate from scratch", err, stateDir)
		status.Emit("state", "failed", err.Error())
		return nil, err
	}
	if manifest.Sources == nil {
		manifest.Sources = make(map[string]demoManifestItem)
	}
	status.Emit("state", "reused", "manifest verified")
	return &demoState{Dir: stateDir, Manifest: manifest, Status: status}, nil
}

func demoConfigHash() string {
	h := sha256.New()
	for _, name := range []string{"agentic.yml", "dev.yml", "prod.yml"} {
		if data, err := os.ReadFile(filepath.Join(cpath, name)); err == nil {
			h.Write([]byte(name))
			h.Write(data)
		}
	}
	sum := h.Sum(nil)
	return hex.EncodeToString(sum[:12])
}

func (s *demoState) writeManifest(dbs map[string]*sql.DB) error {
	if s == nil {
		return nil
	}
	if s.Manifest.Version == 0 {
		s.Manifest.Version = demoManifestVersion
	}
	if s.Manifest.CreatedAt == "" {
		s.Manifest.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	s.Manifest.ConfigHash = demoConfigHash()
	s.Manifest.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if s.Manifest.Sources == nil {
		s.Manifest.Sources = make(map[string]demoManifestItem)
	}
	for name, dbConf := range conf.Databases {
		status := "verified"
		if _, ok := dbs[name]; !ok {
			status = "skipped"
		}
		s.Manifest.Sources[name] = demoManifestItem{Type: dbConf.Type, Status: status}
	}
	if len(conf.Databases) == 0 && conf.DB.Type != "" {
		s.Manifest.Sources[core.DefaultDBName] = demoManifestItem{Type: conf.DB.Type, Status: "verified"}
	}
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.Manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.Dir, "manifest.json"), append(data, '\n'), 0o644)
}

func verifyDemoWorkflows(status demoStatus) error {
	source, ok := conf.Core.WorkflowsSource()
	if !ok {
		status.Emit("workflows", "skipped", "no workflow source configured")
		return nil
	}
	workflowPath := strings.TrimSpace(source.Path)
	if workflowPath == "" {
		workflowPath = "workflows"
	}
	if !filepath.IsAbs(workflowPath) {
		workflowPath = filepath.Join(cpath, workflowPath)
	}
	entries, err := os.ReadDir(workflowPath)
	if err != nil {
		status.Emit("workflows", "failed", err.Error())
		return err
	}
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(entry.Name()), ".js") {
			count++
		}
	}
	status.Emit("workflows", "verified", fmt.Sprintf("%d workflow scripts found", count))
	return nil
}

func writeDemoSchemaCache(dbs map[string]*sql.DB, state *demoState, status demoStatus) {
	if state == nil || len(dbs) == 0 {
		status.Emit("schema-cache", "skipped", "no database schemas to cache")
		return
	}
	dir := filepath.Join(state.Dir, core.SourceSchemaDDLDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		status.Emit("schema-cache", "failed", err.Error())
		return
	}
	names := make([]string, 0, len(dbs))
	for name := range dbs {
		names = append(names, name)
	}
	sort.Strings(names)
	wrote := 0
	for _, name := range names {
		dbConf := conf.Databases[name]
		dbType := dbConf.Type
		if dbType == "" && len(conf.Databases) == 0 {
			dbType = conf.DB.Type
		}
		if dbType == "" {
			dbType = "postgres"
		}
		data, err := core.GenerateSchema(dbs[name], dbType, conf.Blocklist)
		if err != nil {
			status.Emit(name, "skipped", fmt.Sprintf("schema cache unavailable: %s", err))
			continue
		}
		path := filepath.Join(dir, name+".ddl")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			status.Emit(name, "failed", err.Error())
			continue
		}
		wrote++
		status.Emit(name, "verified", "schema cache written")
	}
	if wrote == 0 {
		status.Emit("schema-cache", "skipped", "no schema cache files written")
		return
	}
	status.Emit("schema-cache", "verified", fmt.Sprintf("%d DDL cache files written", wrote))
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

// startDemoContainer starts or opens the appropriate local demo database.
func startDemoContainer(ctx context.Context, name string, dbConf core.DatabaseConfig, state *demoState) (
	cleanup func(context.Context) error,
	connInfo *DemoConnInfo,
	err error,
) {
	defer func() {
		if r := recover(); r != nil {
			msg := cleanDemoStatusMessage(fmt.Sprint(r))
			err = fmt.Errorf("demo source %s failed: %s", name, msg)
			if state != nil {
				state.Status.Emit(name, "failed", msg)
			}
			cleanup = nil
			connInfo = nil
		}
	}()

	dbType := dbConf.Type
	if dbType == "" {
		dbType = "postgres"
	}
	dataDir := filepath.Join(state.Dir, "databases", name)
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, nil, err
	}
	if state.FirstRun {
		state.Status.Emit(name, "created", "state directory ready")
	} else {
		state.Status.Emit(name, "reused", "state directory verified")
	}

	switch strings.ToLower(dbType) {
	case "postgres", "postgresql":
		return startPostgresDemo(ctx, name, dataDir, state.Status)
	case "mysql":
		return startMySQLDemo(ctx, name, dataDir, state.Status)
	case "mariadb":
		return startMariaDBDemo(ctx, name, dataDir, state.Status)
	case "sqlite", "sqlite3":
		return startSQLiteDemo(ctx, name, dataDir, state.Status)
	case "oracle":
		return startOracleDemo(ctx, name, dataDir, state.Status)
	case "mssql", "sqlserver":
		return startMSSQLDemo(ctx, name, dataDir, state.Status)
	case "mongodb", "mongo":
		return startMongoDBDemo(ctx, name, dataDir, state.Status)
	case "bigquery":
		return startBigQueryDemo(ctx, name, dbConf, dataDir, state.Status)
	case "codesql":
		codePath := strings.TrimSpace(dbConf.Path)
		if codePath != "" && !filepath.IsAbs(codePath) {
			codePath = filepath.Join(cpath, codePath)
		}
		if codePath == "" {
			err := fmt.Errorf("CodeSQL source %q requires path", name)
			state.Status.Emit(name, "failed", err.Error())
			return nil, nil, err
		}
		if _, err := os.Stat(codePath); err != nil {
			state.Status.Emit(name, "failed", err.Error())
			return nil, nil, err
		}
		state.Status.Emit(name, "verified", "CodeSQL source path verified; indexing during GraphJin initialization")
		return func(context.Context) error { return nil }, &DemoConnInfo{Type: "codesql"}, nil
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

// terminateFn adapts Terminate to the cleanup signature (testcontainers v0.40 made it variadic).
func terminateFn(c testcontainers.Container) func(context.Context) error {
	return func(ctx context.Context) error { return c.Terminate(ctx) }
}

// startPostgresDemo starts a PostgreSQL container
func startPostgresDemo(ctx context.Context, name, dataDir string, status demoStatus) (func(context.Context) error, *DemoConnInfo, error) {
	status.Emit(name, "starting", "starting Postgres container")
	opts := []testcontainers.ContainerCustomizer{
		postgres.WithUsername("graphjin"),
		postgres.WithPassword("graphjin"),
		postgres.WithDatabase("graphjin_demo"),
		withVolumeMounts(testcontainers.ContainerMounts{
			testcontainers.BindMount(dataDir, testcontainers.ContainerMountTarget("/var/lib/postgresql/data")),
		}),
	}

	container, err := postgres.Run(ctx, "postgres:15", opts...)
	if err != nil {
		status.Emit(name, "failed", err.Error())
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
		testDB, err := sql.Open("pgx", connStr)
		if err == nil {
			if err = testDB.Ping(); err == nil {
				testDB.Close() //nolint:errcheck
				status.Emit(name, "verified", "Postgres is accepting connections")
				break
			}
			testDB.Close() //nolint:errcheck
		}
		time.Sleep(500 * time.Millisecond)
	}

	return terminateFn(container), &DemoConnInfo{
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
func startMySQLDemo(ctx context.Context, name, dataDir string, status demoStatus) (func(context.Context) error, *DemoConnInfo, error) {
	status.Emit(name, "starting", "starting MySQL container")
	opts := []testcontainers.ContainerCustomizer{
		mysql.WithUsername("graphjin"),
		mysql.WithPassword("graphjin"),
		mysql.WithDatabase("graphjin_demo"),
		withVolumeMounts(testcontainers.ContainerMounts{
			testcontainers.BindMount(dataDir, testcontainers.ContainerMountTarget("/var/lib/mysql")),
		}),
	}

	container, err := mysql.Run(ctx, "mysql:8.0", opts...)
	if err != nil {
		status.Emit(name, "failed", err.Error())
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
	status.Emit(name, "verified", "MySQL is accepting connections")

	return terminateFn(container), &DemoConnInfo{
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
func startMariaDBDemo(ctx context.Context, name, dataDir string, status demoStatus) (func(context.Context) error, *DemoConnInfo, error) {
	status.Emit(name, "starting", "starting MariaDB container")
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
			Mounts: testcontainers.ContainerMounts{
				testcontainers.BindMount(dataDir, testcontainers.ContainerMountTarget("/var/lib/mysql")),
			},
		},
		Started: true,
	}

	container, err := testcontainers.GenericContainer(ctx, req)
	if err != nil {
		status.Emit(name, "failed", err.Error())
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
	status.Emit(name, "verified", "MariaDB is accepting connections")

	return terminateFn(container), &DemoConnInfo{
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
func startSQLiteDemo(ctx context.Context, name, dataDir string, status demoStatus) (func(context.Context) error, *DemoConnInfo, error) {
	_ = ctx
	dbPath := filepath.Join(dataDir, "graphjin_demo.db")
	connStr := fmt.Sprintf("file:%s?cache=shared&_busy_timeout=5000", dbPath)
	status.Emit(name, "starting", "opening SQLite database")

	// Register the sqlite3_regexp driver if not already registered
	registerSQLite3Regexp()

	// Open database to verify it works
	testDB, err := sql.Open("sqlite3_regexp", connStr)
	if err != nil {
		status.Emit(name, "failed", err.Error())
		return nil, nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}
	status.Emit(name, "verified", "SQLite file opened")

	cleanup := func(ctx context.Context) error {
		_ = ctx
		return testDB.Close()
	}

	return cleanup, &DemoConnInfo{
		ConnStr: connStr,
		Type:    "sqlite",
		DB:      testDB,
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
func startOracleDemo(ctx context.Context, name, dataDir string, status demoStatus) (func(context.Context) error, *DemoConnInfo, error) {
	status.Emit(name, "starting", "starting Oracle container")
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
			Mounts: testcontainers.ContainerMounts{
				testcontainers.BindMount(dataDir, testcontainers.ContainerMountTarget("/opt/oracle/oradata")),
			},
		},
		Started: true,
	}

	container, err := testcontainers.GenericContainer(ctx, req)
	if err != nil {
		status.Emit(name, "failed", err.Error())
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
	status.Emit(name, "verified", "Oracle container is ready")

	return terminateFn(container), &DemoConnInfo{
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
func startMSSQLDemo(ctx context.Context, name, dataDir string, status demoStatus) (func(context.Context) error, *DemoConnInfo, error) {
	status.Emit(name, "starting", "starting SQL Server container")
	req := testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "mcr.microsoft.com/mssql/server:2022-latest",
			ExposedPorts: []string{"1433/tcp"},
			Env: map[string]string{
				"ACCEPT_EULA":       "Y",
				"MSSQL_SA_PASSWORD": "GraphJin!Passw0rd",
			},
			WaitingFor: wait.ForLog("SQL Server is now ready for client connections").WithStartupTimeout(120 * time.Second),
			Mounts: testcontainers.ContainerMounts{
				testcontainers.BindMount(dataDir, testcontainers.ContainerMountTarget("/var/opt/mssql")),
			},
		},
		Started: true,
	}

	container, err := testcontainers.GenericContainer(ctx, req)
	if err != nil {
		status.Emit(name, "failed", err.Error())
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
		initDB.Close()           //nolint:errcheck
		container.Terminate(ctx) //nolint:errcheck
		return nil, nil, fmt.Errorf("failed to create mssql database: %w", err)
	}
	initDB.Close() //nolint:errcheck

	// Connection string for the demo database
	connStr := fmt.Sprintf("sqlserver://sa:GraphJin!Passw0rd@%s:%s?database=graphjin_demo", host, port.Port())

	log.Infof("SQL Server running on %s:%s", host, port.Port())
	status.Emit(name, "verified", "SQL Server is accepting connections")

	return terminateFn(container), &DemoConnInfo{
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
func startMongoDBDemo(ctx context.Context, name, dataDir string, status demoStatus) (func(context.Context) error, *DemoConnInfo, error) {
	status.Emit(name, "starting", "starting MongoDB container")
	opts := []testcontainers.ContainerCustomizer{
		withVolumeMounts(testcontainers.ContainerMounts{
			testcontainers.BindMount(dataDir, testcontainers.ContainerMountTarget("/data/db")),
		}),
	}

	container, err := mongodb.Run(ctx, "mongo:7", opts...)
	if err != nil {
		status.Emit(name, "failed", err.Error())
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
	status.Emit(name, "verified", "MongoDB is accepting connections")

	return terminateFn(container), &DemoConnInfo{
		Host:    host,
		Port:    uint16(port.Int()),
		DBName:  "graphjin_demo",
		ConnStr: connStr,
		Type:    "mongodb",
	}, nil
}

func startBigQueryDemo(ctx context.Context, name string, dbConf core.DatabaseConfig, dataDir string, status demoStatus) (func(context.Context) error, *DemoConnInfo, error) {
	status.Emit(name, "opening", "opening BigQuery simulator")
	setupSQL, setupSource, setupIsDDL, err := bigQuerySimulatorSetupSQL(name, dbConf)
	if err != nil {
		status.Emit(name, "failed", err.Error())
		return nil, nil, err
	}

	dbPath := filepath.Join(dataDir, "warehouse.duckdb")
	initialized := bigQuerySimulatorInitialized(dbPath)
	connector := hostedemu.NewConnector(hostedemu.Config{
		SeedSQL:  setupSQL,
		DBPath:   dbPath,
		Backend:  hostedemu.BackendDuckDB,
		Fallback: hostedemu.FallbackStrict,
		TestName: name,
	}, hostedbigquery.NewAdapter())

	demoDB := sql.OpenDB(connector)
	if err := demoDB.PingContext(ctx); err != nil {
		demoDB.Close() //nolint:errcheck
		status.Emit(name, "failed", err.Error())
		return nil, nil, fmt.Errorf("failed to open BigQuery simulator: %w", err)
	}
	if initialized {
		status.Emit(name, "verified", "BigQuery simulator state reused")
	} else if setupIsDDL {
		status.Emit(name, "applied", fmt.Sprintf("%s applied via simulator", filepath.Base(setupSource)))
	} else {
		status.Emit(name, "applied", fmt.Sprintf("%s applied via legacy simulator SQL", filepath.Base(setupSource)))
	}
	status.Emit(name, "verified", "BigQuery simulator ready")

	cleanup := func(ctx context.Context) error {
		_ = ctx
		return demoDB.Close()
	}
	return cleanup, &DemoConnInfo{
		ConnStr: dbPath,
		Type:    "bigquery",
		DB:      demoDB,
	}, nil
}

func bigQuerySimulatorSetupSQL(name string, dbConf core.DatabaseConfig) (setupSQL, sourcePath string, ddl bool, err error) {
	ddlPath := filepath.Join(cpath, filepath.FromSlash(core.SourceSchemaDDLPath(name)))
	if data, readErr := os.ReadFile(ddlPath); readErr == nil {
		sqls, genErr := core.GenerateSchemaSQL("bigquery", data, conf.Blocklist)
		if genErr != nil {
			return "", ddlPath, true, genErr
		}
		return strings.Join(sqls, "\n\n"), ddlPath, true, nil
	} else if !os.IsNotExist(readErr) {
		return "", ddlPath, true, readErr
	}

	seedPath := strings.TrimSpace(dbConf.Path)
	if seedPath == "" {
		seedPath = filepath.Join("schema", name+".sql")
	}
	if !filepath.IsAbs(seedPath) {
		seedPath = filepath.Join(cpath, seedPath)
	}
	data, err := os.ReadFile(seedPath)
	if err != nil {
		return "", seedPath, false, err
	}
	return string(data), seedPath, false, nil
}

func bigQuerySimulatorInitialized(dbPath string) bool {
	st, err := os.Stat(dbPath)
	return err == nil && st.Size() > 0
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

// startMultiDBDemo starts or opens all databases in the config.
func startMultiDBDemo(ctx context.Context, primaryType string, overrides map[string]string, state *demoState) (
	cleanups []func(context.Context) error,
	dbs map[string]*sql.DB,
	err error,
) {
	// Initialize connections map
	multiDBConns = make(map[string]*sql.DB)
	dbs = make(map[string]*sql.DB)

	names := make([]string, 0, len(conf.Databases))
	for name := range conf.Databases {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		pi := demoSourcePriority(conf.Databases[names[i]].Type)
		pj := demoSourcePriority(conf.Databases[names[j]].Type)
		if pi != pj {
			return pi < pj
		}
		return names[i] < names[j]
	})

	for _, name := range names {
		dbConf := conf.Databases[name]
		dbType := dbConf.Type

		// Check for override
		if override, ok := overrides[name]; ok {
			dbType = override
		}

		if dbType == "" {
			dbType = "postgres" // Default
		}
		dbConf.Type = dbType

		cleanup, connInfo, err := startDemoContainer(ctx, name, dbConf, state)
		if err != nil {
			// Cleanup already started containers
			cleanupAll(ctx, cleanups)
			return nil, nil, fmt.Errorf("failed to start %s: %w", name, err)
		}
		cleanups = append(cleanups, cleanup)

		// Update the config with container connection info
		applyMultiDBContainerConfig(name, connInfo)

		// Open database connection for migrations
		conn, err := openDemoConnection(connInfo)
		if err != nil {
			if strings.EqualFold(connInfo.Type, "codesql") {
				continue
			}
			cleanupAll(ctx, cleanups)
			return nil, nil, fmt.Errorf("failed to open connection for %s: %w", name, err)
		} else {
			multiDBConns[name] = conn
			dbs[name] = conn
		}
	}
	return cleanups, dbs, nil
}

func demoSourcePriority(dbType string) int {
	switch strings.ToLower(strings.TrimSpace(dbType)) {
	case "bigquery":
		return 1
	case "codesql":
		return 2
	default:
		return 0
	}
}

// openDemoConnection opens a database connection based on DemoConnInfo
func openDemoConnection(connInfo *DemoConnInfo) (*sql.DB, error) {
	if connInfo.DB != nil {
		return connInfo.DB, nil
	}
	var driverName string
	switch strings.ToLower(connInfo.Type) {
	case "postgres", "postgresql":
		driverName = "pgx"
	case "mysql", "mariadb":
		driverName = "mysql"
	case "sqlite", "sqlite3":
		driverName = "sqlite3_regexp"
	case "mssql", "sqlserver":
		driverName = "sqlserver"
	case "oracle":
		driverName = "oracle"
	case "bigquery":
		return nil, fmt.Errorf("bigquery demo requires a pre-opened simulator database")
	case "codesql":
		return nil, fmt.Errorf("codesql is initialized by the GraphJin service")
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

func runDemoSQLFiles(dirName, phase string) bool {
	dir := filepath.Join(cpath, dirName)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return false
	}
	if err != nil {
		log.Warnf("Error reading %s directory: %s", dirName, err)
		if currentDemo != nil {
			currentDemo.Status.Emit(phase, "failed", err.Error())
		}
		return false
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".sql") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	if len(names) == 0 {
		return false
	}

	var ran bool
	for _, fileName := range names {
		dbName := strings.TrimSuffix(fileName, filepath.Ext(fileName))
		conn := demoConnectionForSQLFile(dbName, len(names) == 1)
		if conn == nil {
			if currentDemo != nil {
				currentDemo.Status.Emit(dbName, "skipped", fmt.Sprintf("%s SQL has no writable database", phase))
			}
			continue
		}
		path := filepath.Join(dir, fileName)
		if err := execDemoSQLFile(conn, path); err != nil {
			log.Warnf("Error applying %s to %s: %s", path, dbName, err)
			if currentDemo != nil {
				currentDemo.Status.Emit(dbName, "failed", err.Error())
			}
			continue
		}
		ran = true
		if currentDemo != nil {
			currentDemo.Status.Emit(dbName, "verified", fmt.Sprintf("%s SQL applied", phase))
		}
	}
	return ran
}

func demoConnectionForSQLFile(dbName string, singleFile bool) *sql.DB {
	if len(multiDBConns) != 0 {
		if !demoDBWritable(dbName) {
			return nil
		}
		return multiDBConns[dbName]
	}
	if db == nil {
		return nil
	}
	if singleFile || dbName == core.DefaultDBName || dbName == "default" || dbName == "db" || dbName == "graphjin_demo" {
		if conf.DB.Type == "mongodb" || conf.DB.Type == "bigquery" {
			return nil
		}
		return db
	}
	return nil
}

func demoDBWritable(dbName string) bool {
	dbConf, ok := conf.Databases[dbName]
	if !ok {
		return true
	}
	dbType := strings.ToLower(dbConf.Type)
	return !dbConf.ReadOnly && dbType != "codesql" && dbType != "mongodb"
}

func execDemoSQLFile(conn *sql.DB, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(data)) == "" {
		return nil
	}
	_, err = conn.ExecContext(context.Background(), string(data))
	return err
}

func runDemoSourceSeedJS() bool {
	dir := filepath.Join(cpath, "seed")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return false
	}
	if err != nil {
		log.Warnf("Error reading seed directory: %s", err)
		if currentDemo != nil {
			currentDemo.Status.Emit("seed", "failed", err.Error())
		}
		return false
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".js") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	if len(names) == 0 {
		return false
	}

	var ran bool
	for _, fileName := range names {
		source := strings.TrimSuffix(fileName, filepath.Ext(fileName))
		conn, simulatorSeed := demoConnectionForSeedJS(source, len(names) == 1)
		if conn == nil {
			if currentDemo != nil {
				currentDemo.Status.Emit(source, "skipped", "seed JS has no writable database")
			}
			continue
		}
		if len(conf.Databases) != 0 && !demoDBWritable(source) && !simulatorSeed {
			if currentDemo != nil {
				currentDemo.Status.Emit(source, "skipped", "seed JS has no writable database")
			}
			continue
		}

		path := filepath.Join(dir, fileName)
		log.Infof("Running seed script from %s", path)
		prepareSeedConfig()
		seedCtx := seedJSContext{
			DB:            conn,
			Databases:     multiDBConns,
			ConfigPath:    cpath,
			DefaultSource: source,
		}
		if len(seedCtx.Databases) == 0 {
			seedCtx.Databases = nil
		}
		if err := compileAndRunJSWithContext(path, seedCtx); err != nil {
			log.Warnf("Failed to execute seed file %s: %s", path, err)
			if currentDemo != nil {
				currentDemo.Status.Emit(source, "failed", err.Error())
			}
			continue
		}
		ran = true
		if currentDemo != nil {
			if simulatorSeed {
				currentDemo.Status.Emit(source, "verified", fmt.Sprintf("%s seeded via simulator", fileName))
			} else {
				currentDemo.Status.Emit(source, "verified", "seed JS completed")
			}
		}
	}
	return ran
}

func demoConnectionForSeedJS(source string, singleFile bool) (*sql.DB, bool) {
	if len(multiDBConns) != 0 {
		if demoDBWritable(source) {
			return multiDBConns[source], false
		}
		if demoDBSimulatorSeed(source) {
			return multiDBConns[source], true
		}
		return nil, false
	}
	return demoConnectionForSQLFile(source, singleFile), false
}

func demoDBSimulatorSeed(source string) bool {
	dbConf, ok := conf.Databases[source]
	if !ok || !dbConf.ReadOnly || multiDBConns[source] == nil {
		return false
	}
	return strings.EqualFold(dbConf.Type, "bigquery")
}

func prepareSeedConfig() {
	conf.Serv.Production = false
	conf.DefaultBlock = false
	conf.DisableAllowList = true
	conf.DBSchemaPollDuration = -1
	conf.Blocklist = nil
}

// runDemoMigrations syncs the database schema from GraphJin DDL.
func runDemoMigrations() {
	if currentDemo != nil && !currentDemo.FirstRun {
		currentDemo.Status.Emit("schema", "skipped", "existing demo state verified")
		return
	}
	if len(conf.Databases) > 0 && len(multiDBConns) > 0 {
		if runDemoMigrationsMultiDB() {
			return
		}
	} else if runDemoProjectDDL() {
		return
	}

	if runDemoSQLFiles("schema", "schema") {
		return
	}

	if conf.DB.Type == "mongodb" {
		log.Info("Schema sync not applicable for MongoDB, skipping")
		if currentDemo != nil {
			currentDemo.Status.Emit("schema", "skipped", "schema sync not applicable for MongoDB")
		}
		return
	}

	log.Info("No GraphJin DDL found, skipping schema sync")
	if currentDemo != nil {
		currentDemo.Status.Emit("schema", "skipped", "no db.ddl, schema-ddl/*.ddl, legacy db.graphql, or legacy schema/*.sql found")
	}
}

func runDemoProjectDDL() bool {
	schemaBytes, schemaPath, err := readProjectSchemaDDL()
	if err != nil {
		return false
	}
	log.Infof("Syncing schema from %s", schemaPath)

	// Compute schema diff
	opts := core.DiffOptions{Destructive: false}
	ops, err := core.SchemaDiff(db, conf.DB.Type, schemaBytes, conf.Blocklist, opts)
	if err != nil {
		log.Warnf("Error computing schema diff: %s", err)
		if currentDemo != nil {
			currentDemo.Status.Emit("schema", "failed", err.Error())
		}
		return true
	}

	if len(ops) == 0 {
		log.Info("Schema is already in sync")
		if currentDemo != nil {
			currentDemo.Status.Emit("schema", "verified", "DDL already in sync")
		}
		return true
	}

	sqls := core.GenerateDiffSQL(ops)
	if err := applySQLChanges(context.Background(), db, conf.DB.Type, "", sqls); err != nil {
		log.Warnf("Error applying schema: %s", err)
		if currentDemo != nil {
			currentDemo.Status.Emit("schema", "failed", err.Error())
		}
		return true
	}

	log.Infof("Schema sync completed (%d changes applied)", len(ops))
	if currentDemo != nil {
		currentDemo.Status.Emit("schema", "verified", fmt.Sprintf("%d DDL changes applied", len(ops)))
	}
	return true
}

// runDemoMigrationsMultiDB syncs schemas for all databases in multi-DB mode
func runDemoMigrationsMultiDB() bool {
	files, err := sourceSchemaDDLFiles()
	if err != nil {
		log.Warnf("Error reading source DDL files: %s", err)
		if currentDemo != nil {
			currentDemo.Status.Emit("schema", "failed", err.Error())
		}
		return true
	}
	if err := ensureNoSchemaDDLAmbiguity(files); err != nil {
		log.Warnf("Error resolving schema DDL: %s", err)
		if currentDemo != nil {
			currentDemo.Status.Emit("schema", "failed", err.Error())
		}
		return true
	}

	// Build dbTypes map from config
	dbTypes := make(map[string]string)
	for name, dbConf := range conf.Databases {
		dbTypes[name] = dbConf.Type
	}

	// Compute schema diff for all databases
	opts := core.DiffOptions{Destructive: false}
	results := make(map[string][]core.SchemaOperation)
	if len(files) != 0 {
		for _, file := range files {
			conn, ok := multiDBConns[file.Source]
			if !ok {
				err := fmt.Errorf("%s has no writable database source %q", file.Path, file.Source)
				log.Warn(err)
				if currentDemo != nil {
					currentDemo.Status.Emit(file.Source, "failed", err.Error())
				}
				return true
			}
			dbType := dbTypes[file.Source]
			if demoDBSimulatorDDL(file.Source, dbType) {
				if currentDemo != nil {
					currentDemo.Status.Emit(file.Source, "skipped", "DDL handled by simulator startup")
				}
				continue
			}
			if !demoDBWritable(file.Source) || !core.SupportsSchemaDDL(dbType) {
				if currentDemo != nil {
					currentDemo.Status.Emit(file.Source, "skipped", "DDL has no supported writable database")
				}
				continue
			}
			ops, err := core.SchemaDiff(conn, dbType, file.Data, conf.Blocklist, opts)
			if err != nil {
				log.Warnf("Error computing schema diff for %s: %s", file.Source, err)
				if currentDemo != nil {
					currentDemo.Status.Emit(file.Source, "failed", err.Error())
				}
				return true
			}
			if len(ops) == 0 && currentDemo != nil {
				currentDemo.Status.Emit(file.Source, "verified", "DDL already in sync")
			}
			results[file.Source] = ops
		}
	} else {
		schemaBytes, schemaPath, err := readProjectSchemaDDL()
		if err != nil {
			return false
		}
		log.Infof("Syncing schemas from %s (multi-database mode)", schemaPath)
		results, err = core.SchemaDiffMultiDB(multiDBConns, dbTypes, schemaBytes, conf.Blocklist, opts)
		if err != nil {
			log.Warnf("Error computing schema diff: %s", err)
			if currentDemo != nil {
				currentDemo.Status.Emit("schema", "failed", err.Error())
			}
			return true
		}
	}

	// Apply changes per database
	totalChanges := 0
	for dbName, ops := range results {
		if len(ops) == 0 {
			continue
		}

		sqls := core.GenerateDiffSQL(ops)
		conn := multiDBConns[dbName]

		if err := applySQLChanges(context.Background(), conn, dbTypes[dbName], dbName, sqls); err != nil {
			log.Warnf("Error applying schema to %s: %s", dbName, err)
			continue
		}
		log.Infof("[%s] Schema sync completed (%d changes)", dbName, len(ops))
		if currentDemo != nil {
			currentDemo.Status.Emit(dbName, "verified", fmt.Sprintf("%d DDL changes applied", len(ops)))
		}
		totalChanges += len(ops)
	}

	if totalChanges == 0 {
		log.Info("All schemas are already in sync")
		if currentDemo != nil {
			currentDemo.Status.Emit("schema", "verified", "all DDL already in sync")
		}
	} else {
		log.Infof("Multi-DB schema sync completed (%d total changes)", totalChanges)
		if currentDemo != nil {
			currentDemo.Status.Emit("schema", "verified", fmt.Sprintf("%d total DDL changes applied", totalChanges))
		}
	}
	return true
}

func demoDBSimulatorDDL(source, dbType string) bool {
	if !strings.EqualFold(dbType, "bigquery") {
		return false
	}
	dbConf, ok := conf.Databases[source]
	return ok && dbConf.ReadOnly && multiDBConns[source] != nil
}

// runDemoSeed runs the seed script if available
func runDemoSeed() {
	if currentDemo != nil && !currentDemo.FirstRun {
		currentDemo.Status.Emit("seed", "skipped", "existing demo state verified")
		return
	}
	if runDemoSourceSeedJS() {
		return
	}
	if runDemoSQLFiles("seed", "seed") {
		return
	}

	if conf.DB.Type == "mysql" {
		log.Warn("Seed scripts not supported with MySQL, skipping")
		if currentDemo != nil {
			currentDemo.Status.Emit("seed", "skipped", "seed.js is not supported with MySQL")
		}
		return
	}

	if conf.DB.Type == "mongodb" {
		log.Info("Seed scripts not applicable for MongoDB, skipping")
		if currentDemo != nil {
			currentDemo.Status.Emit("seed", "skipped", "seed.js is not applicable for MongoDB")
		}
		return
	}

	seedPath := filepath.Join(cpath, "seed.js")
	if _, err := os.Stat(seedPath); os.IsNotExist(err) {
		log.Info("No seed.js found, skipping")
		if currentDemo != nil {
			currentDemo.Status.Emit("seed", "skipped", "no seed.js, seed/*.js, or seed/*.sql found")
		}
		return
	}

	log.Infof("Running seed script from %s", seedPath)

	prepareSeedConfig()

	seedCtx := seedJSContext{
		DB:            db,
		Databases:     multiDBConns,
		ConfigPath:    cpath,
		DefaultSource: seedDefaultSource(),
	}
	if len(seedCtx.Databases) == 0 {
		seedCtx.Databases = nil
	}
	if err := compileAndRunJSWithContext(seedPath, seedCtx); err != nil {
		log.Warnf("Failed to execute seed file: %s", err)
		if currentDemo != nil {
			currentDemo.Status.Emit("seed", "failed", err.Error())
		}
		return
	}

	log.Info("Seed script completed")
	if currentDemo != nil {
		currentDemo.Status.Emit("seed", "verified", "seed script completed")
	}
}

func seedDefaultSource() string {
	for _, source := range conf.Core.Sources {
		if !source.Default {
			continue
		}
		kind := strings.ToLower(strings.TrimSpace(source.Kind))
		if kind == "database" || kind == "code" {
			return source.Name
		}
	}
	if len(multiDBConns) != 0 {
		names := make([]string, 0, len(multiDBConns))
		for name := range multiDBConns {
			if demoDBWritable(name) {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		if len(names) != 0 {
			return names[0]
		}
	}
	return core.DefaultDBName
}
