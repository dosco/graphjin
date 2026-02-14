package serv

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	osuser "os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/mark3labs/mcp-go/mcp"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// registerDiscoverTools registers the discover_databases tool
func (ms *mcpServer) registerDiscoverTools() {
	if !ms.service.conf.MCP.AllowDevTools {
		return
	}
	ms.srv.AddTool(mcp.NewTool(
		"discover_databases",
		mcp.WithDescription("Scan the local system for running databases. "+
			"Probes well-known TCP ports on localhost for PostgreSQL, MySQL, MariaDB, MSSQL, Oracle, and MongoDB. "+
			"Checks Unix domain sockets for PostgreSQL and MySQL. "+
			"Searches for SQLite database files. Detects database Docker containers. "+
			"Then attempts to connect using default credentials and lists database names inside each instance. "+
			"If defaults fail, reports auth_failed so you can re-call with user/password. "+
			"Use this before configuring GraphJin to find which databases are available. "+
			"Does NOT require an existing database connection. "+
			"Note: system databases (postgres, mysql, information_schema, master, etc.) "+
			"are filtered from the databases list by default. "+
			"If no user databases exist, use update_current_config with create_if_not_exists: true to create one."),
		mcp.WithString("scan_dir",
			mcp.Description("Directory to scan for SQLite files (default: current working directory)")),
		mcp.WithBoolean("skip_docker",
			mcp.Description("Skip Docker container detection (default: false)")),
		mcp.WithBoolean("skip_probe",
			mcp.Description("Skip connection probing and database listing (default: false)")),
		mcp.WithString("user",
			mcp.Description("Username to try when probing (tried before defaults)")),
		mcp.WithString("password",
			mcp.Description("Password to try when probing")),
	), ms.handleDiscoverDatabases)

	// list_databases - List databases on all connected servers
	ms.srv.AddTool(mcp.NewTool(
		"list_databases",
		mcp.WithDescription("List databases on all configured database servers. "+
			"Unlike discover_databases (which scans for new servers), this queries EXISTING configured connections "+
			"for their database lists. Use after initial setup to see what databases are available on each server."),
	), ms.handleListDatabases)
}

// DatabaseConnection represents a single database server connection for list_databases
type DatabaseConnection struct {
	Name      string   `json:"name"`
	Type      string   `json:"type"`
	Host      string   `json:"host"`
	Databases []string `json:"databases"`
	Active    bool     `json:"active"`
	Error     string   `json:"error,omitempty"`
}

// ListDatabasesResult is the response from list_databases
type ListDatabasesResult struct {
	Connections    []DatabaseConnection `json:"connections"`
	TotalDatabases int                  `json:"total_databases"`
}

// DiscoveredDatabase represents a database found during discovery
type DiscoveredDatabase struct {
	Type          string         `json:"type"`
	Host          string         `json:"host,omitempty"`
	Port          int            `json:"port,omitempty"`
	FilePath      string         `json:"file_path,omitempty"`
	Source        string         `json:"source"`
	Status        string         `json:"status"`
	Databases     []string       `json:"databases,omitempty"`
	AuthStatus    string         `json:"auth_status,omitempty"`
	AuthUser      string         `json:"auth_user,omitempty"`
	AuthError     string         `json:"auth_error,omitempty"`
	DockerInfo    *DockerDBInfo  `json:"docker_info,omitempty"`
	ConfigSnippet map[string]any `json:"config_snippet"`
}

// DockerDBInfo holds Docker container details for a discovered database
type DockerDBInfo struct {
	ContainerID   string `json:"container_id"`
	ContainerName string `json:"container_name"`
	Image         string `json:"image"`
	Ports         string `json:"ports"`
}

// DiscoverResult is the top-level response structure
type DiscoverResult struct {
	Databases    []DiscoveredDatabase `json:"databases"`
	Summary      DiscoverSummary      `json:"summary"`
	DockerStatus string               `json:"docker_status"`
}

// DiscoverSummary summarizes the discovery scan
type DiscoverSummary struct {
	TotalFound     int      `json:"total_found"`
	DatabaseTypes  []string `json:"database_types"`
	ScanDurationMs int64    `json:"scan_duration_ms"`
}

// dbProbe defines a port to probe for a specific database type
type dbProbe struct {
	dbType string
	port   int
}

// socketProbe defines a Unix socket path to check for a specific database type
type socketProbe struct {
	dbType string
	path   string
}

// handleListDatabases queries all configured database connections for their database lists
func (ms *mcpServer) handleListDatabases(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	conf := &ms.service.conf.Core
	activeDB := ms.getActiveDatabase()
	var connections []DatabaseConnection
	totalDBs := 0

	// Query the primary connection
	if ms.service.db != nil {
		primaryName := "default"
		if activeDB != "" {
			primaryName = activeDB
		}
		names, err := listDatabaseNames(ms.service.db, ms.service.conf.DBType)
		if !ms.service.conf.MCP.DefaultDBAllowed {
			names = filterSystemDatabases(ms.service.conf.DBType, names)
		}
		conn := DatabaseConnection{
			Name:   primaryName,
			Type:   ms.service.conf.DBType,
			Host:   fmt.Sprintf("%s:%d", ms.service.conf.DB.Host, ms.service.conf.DB.Port),
			Active: true,
		}
		if err != nil {
			conn.Error = err.Error()
		} else {
			conn.Databases = names
			totalDBs += len(names)
		}
		connections = append(connections, conn)
	}

	// Query each additional configured database
	queried := make(map[string]bool) // track host:port to avoid duplicates
	if ms.service.db != nil {
		queried[fmt.Sprintf("%s:%d", ms.service.conf.DB.Host, ms.service.conf.DB.Port)] = true
	}

	for name, dbConf := range conf.Databases {
		hostPort := fmt.Sprintf("%s:%d", dbConf.Host, dbConf.Port)
		if queried[hostPort] {
			continue
		}
		queried[hostPort] = true

		dbType := strings.ToLower(dbConf.Type)
		names, err := testDatabaseConnection(dbType, dbConf.Host, dbConf.Port, dbConf.User, dbConf.Password, dbConf.DBName)
		if !ms.service.conf.MCP.DefaultDBAllowed {
			names = filterSystemDatabases(dbType, names)
		}

		conn := DatabaseConnection{
			Name:   name,
			Type:   dbConf.Type,
			Host:   hostPort,
			Active: name == activeDB,
		}
		if err != nil {
			conn.Error = err.Error()
		} else {
			conn.Databases = names
			totalDBs += len(names)
		}
		connections = append(connections, conn)
	}

	result := ListDatabasesResult{
		Connections:    connections,
		TotalDatabases: totalDBs,
	}

	data, err := mcpMarshalJSON(result, true)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal result: %v", err)), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

// handleDiscoverDatabases scans the local system for running databases
func (ms *mcpServer) handleDiscoverDatabases(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	start := time.Now()
	args := req.GetArguments()

	scanDir, _ := args["scan_dir"].(string)
	if scanDir == "" {
		scanDir, _ = os.Getwd()
	}

	skipDocker := false
	if sd, ok := args["skip_docker"].(bool); ok {
		skipDocker = sd
	}

	skipProbe := false
	if sp, ok := args["skip_probe"].(bool); ok {
		skipProbe = sp
	}

	userParam, _ := args["user"].(string)
	passwordParam, _ := args["password"].(string)

	timeout := 500 * time.Millisecond

	// TCP port probes for all supported database types
	tcpProbes := []dbProbe{
		{"postgres", 5432},
		{"postgres", 5433},
		{"postgres", 5434},
		{"mysql", 3306},
		{"mysql", 3307},
		{"mssql", 1433},
		{"mssql", 1434},
		{"oracle", 1521},
		{"oracle", 1522},
		{"mongodb", 27017},
		{"mongodb", 27018},
	}

	// Unix socket probes
	unixProbes := []socketProbe{
		// PostgreSQL sockets
		{"postgres", "/tmp/.s.PGSQL.5432"},
		{"postgres", "/var/run/postgresql/.s.PGSQL.5432"},
		{"postgres", "/var/pgsql_socket/.s.PGSQL.5432"},
		// MySQL sockets
		{"mysql", "/tmp/mysql.sock"},
		{"mysql", "/var/run/mysqld/mysqld.sock"},
		{"mysql", "/var/lib/mysql/mysql.sock"},
		// MongoDB sockets
		{"mongodb", "/tmp/mongodb-27017.sock"},
	}

	var databases []DiscoveredDatabase
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Phase 1: TCP port probes (concurrent)
	for _, probe := range tcpProbes {
		wg.Add(1)
		go func(p dbProbe) {
			defer wg.Done()
			if checkTCPPort("127.0.0.1", p.port, timeout) {
				db := DiscoveredDatabase{
					Type:          p.dbType,
					Host:          "localhost",
					Port:          p.port,
					Source:        "tcp",
					Status:        "listening",
					ConfigSnippet: buildConfigSnippet(p.dbType, "localhost", p.port, ""),
				}
				mu.Lock()
				databases = append(databases, db)
				mu.Unlock()
			}
		}(probe)
	}

	// Phase 2: Unix socket checks (concurrent)
	for _, probe := range unixProbes {
		wg.Add(1)
		go func(p socketProbe) {
			defer wg.Done()
			if checkUnixSocket(p.path, timeout) {
				db := DiscoveredDatabase{
					Type:          p.dbType,
					Host:          p.path,
					Port:          defaultPortForType(p.dbType),
					Source:        "unix_socket",
					Status:        "listening",
					ConfigSnippet: buildConfigSnippet(p.dbType, p.path, 0, ""),
				}
				mu.Lock()
				databases = append(databases, db)
				mu.Unlock()
			}
		}(probe)
	}

	// Phase 3: SQLite file scan (synchronous, fast)
	sqliteFiles := findSQLiteFiles(scanDir)
	for _, f := range sqliteFiles {
		databases = append(databases, DiscoveredDatabase{
			Type:          "sqlite",
			FilePath:      f,
			Source:        "file",
			Status:        "found",
			ConfigSnippet: buildConfigSnippet("sqlite", "", 0, f),
		})
	}

	// Wait for TCP and socket probes to finish
	wg.Wait()

	// Phase 4: Docker detection
	dockerStatus := "skipped"
	if !skipDocker {
		dockerDBs, status := discoverDockerDatabases()
		dockerStatus = status
		if len(dockerDBs) > 0 {
			databases = append(databases, dockerDBs...)
		}
	}

	// Deduplicate: if Docker found a specific type (e.g. mariadb) on a port
	// that was also found via TCP as "mysql", prefer the Docker label
	databases = deduplicateDatabases(databases)

	// Phase 5: Connection probing (concurrent)
	if !skipProbe && len(databases) > 0 {
		var probeWg sync.WaitGroup
		for i := range databases {
			probeWg.Add(1)
			go func(db *DiscoveredDatabase) {
				defer probeWg.Done()
				probeDatabase(db, userParam, passwordParam)
			}(&databases[i])
		}
		probeWg.Wait()
	}

	// Filter system databases from results (unless allowed by config)
	if !ms.service.conf.MCP.DefaultDBAllowed {
		for i := range databases {
			if len(databases[i].Databases) > 0 {
				origLen := len(databases[i].Databases)
				databases[i].Databases = filterSystemDatabases(
					databases[i].Type, databases[i].Databases)
				// If all databases were filtered, add a hint
				if len(databases[i].Databases) == 0 && origLen > 0 {
					databases[i].AuthError = fmt.Sprintf(
						"all %d databases are system databases (filtered). "+
							"Use update_current_config with create_if_not_exists: true to create a new database.",
						origLen)
				}
			}
		}
	}

	// Build summary
	typeSet := make(map[string]bool)
	for _, db := range databases {
		typeSet[db.Type] = true
	}
	var types []string
	for t := range typeSet {
		types = append(types, t)
	}

	result := DiscoverResult{
		Databases: databases,
		Summary: DiscoverSummary{
			TotalFound:     len(databases),
			DatabaseTypes:  types,
			ScanDurationMs: time.Since(start).Milliseconds(),
		},
		DockerStatus: dockerStatus,
	}

	data, err := mcpMarshalJSON(result, true)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal result: %v", err)), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

// checkTCPPort attempts a TCP connection to host:port with the given timeout
func checkTCPPort(host string, port int, timeout time.Duration) bool {
	addr := fmt.Sprintf("%s:%d", host, port)
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// checkUnixSocket attempts a connection to a Unix domain socket
func checkUnixSocket(path string, timeout time.Duration) bool {
	conn, err := net.DialTimeout("unix", path, timeout)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// findSQLiteFiles searches for SQLite database files in the given directory
func findSQLiteFiles(dir string) []string {
	var files []string
	patterns := []string{"*.db", "*.sqlite", "*.sqlite3"}
	for _, pattern := range patterns {
		matches, err := filepath.Glob(filepath.Join(dir, pattern))
		if err == nil {
			files = append(files, matches...)
		}
	}
	return files
}

// discoverDockerDatabases runs docker ps to find running database containers
func discoverDockerDatabases() ([]DiscoveredDatabase, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "ps",
		"--format", "{{.ID}}\t{{.Names}}\t{{.Image}}\t{{.Ports}}")
	out, err := cmd.Output()
	if err != nil {
		return nil, "unavailable"
	}

	// Docker image prefix to DB type mapping
	imageMap := map[string]string{
		"postgres":                   "postgres",
		"mysql":                      "mysql",
		"mariadb":                    "mariadb",
		"mcr.microsoft.com/mssql":   "mssql",
		"mongo":                      "mongodb",
		"oracle":                     "oracle",
		"gvenzl/oracle":             "oracle",
	}

	var databases []DiscoveredDatabase
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 4)
		if len(parts) < 4 {
			continue
		}
		containerID := parts[0]
		containerName := parts[1]
		image := parts[2]
		ports := parts[3]

		// Match image to DB type
		var dbType string
		for prefix, t := range imageMap {
			if strings.HasPrefix(image, prefix) {
				dbType = t
				break
			}
		}
		if dbType == "" {
			continue
		}

		hostPort := parseDockerHostPort(ports, defaultPortForType(dbType))

		db := DiscoveredDatabase{
			Type:   dbType,
			Host:   "localhost",
			Port:   hostPort,
			Source: "docker",
			Status: "running",
			DockerInfo: &DockerDBInfo{
				ContainerID:   containerID,
				ContainerName: containerName,
				Image:         image,
				Ports:         ports,
			},
			ConfigSnippet: buildConfigSnippet(dbType, "localhost", hostPort, ""),
		}
		databases = append(databases, db)
	}

	return databases, "available"
}

// parseDockerHostPort extracts the host port from a Docker ports string
// e.g., "0.0.0.0:5432->5432/tcp" → 5432, "0.0.0.0:15432->5432/tcp" → 15432
func parseDockerHostPort(portsStr string, defaultPort int) int {
	if portsStr == "" {
		return defaultPort
	}
	// Handle multiple port mappings (take the first relevant one)
	for _, mapping := range strings.Split(portsStr, ", ") {
		// Look for "host:port->container" pattern
		arrowIdx := strings.Index(mapping, "->")
		if arrowIdx == -1 {
			continue
		}
		hostPart := mapping[:arrowIdx]
		// Extract port after last ":"
		colonIdx := strings.LastIndex(hostPart, ":")
		if colonIdx == -1 {
			continue
		}
		portStr := hostPart[colonIdx+1:]
		if port, err := strconv.Atoi(portStr); err == nil {
			return port
		}
	}
	return defaultPort
}

// defaultPortForType returns the default TCP port for a database type
func defaultPortForType(dbType string) int {
	switch dbType {
	case "postgres":
		return 5432
	case "mysql", "mariadb":
		return 3306
	case "mssql":
		return 1433
	case "oracle":
		return 1521
	case "mongodb":
		return 27017
	default:
		return 0
	}
}

// buildConfigSnippet creates a config snippet with default credentials for a DB type
func buildConfigSnippet(dbType, host string, port int, filePath string) map[string]any {
	snippet := map[string]any{
		"type": dbType,
	}

	if dbType == "sqlite" {
		snippet["path"] = filePath
		return snippet
	}

	if host != "" {
		snippet["host"] = host
	}
	if port > 0 {
		snippet["port"] = port
	}

	// Default credentials per type
	switch dbType {
	case "postgres":
		snippet["user"] = "postgres"
		snippet["password"] = ""
		snippet["dbname"] = ""
	case "mysql", "mariadb":
		snippet["user"] = "root"
		snippet["password"] = ""
		snippet["dbname"] = ""
	case "mssql":
		snippet["user"] = "sa"
		snippet["password"] = ""
		snippet["dbname"] = ""
	case "oracle":
		snippet["user"] = "system"
		snippet["password"] = ""
		snippet["dbname"] = ""
	case "mongodb":
		snippet["dbname"] = ""
	}

	return snippet
}

// deduplicateDatabases removes TCP entries when Docker provides a more specific type
// (e.g., Docker says "mariadb" on port 3306, TCP says "mysql" on port 3306 — keep Docker)
func deduplicateDatabases(dbs []DiscoveredDatabase) []DiscoveredDatabase {
	// Build a set of docker-discovered host:port pairs with their specific types
	dockerPorts := make(map[string]string) // "host:port" -> dbType
	for _, db := range dbs {
		if db.Source == "docker" && db.Port > 0 {
			key := fmt.Sprintf("%s:%d", db.Host, db.Port)
			dockerPorts[key] = db.Type
		}
	}

	var result []DiscoveredDatabase
	for _, db := range dbs {
		if db.Source == "tcp" && db.Port > 0 {
			key := fmt.Sprintf("%s:%d", db.Host, db.Port)
			if _, dockerHasIt := dockerPorts[key]; dockerHasIt {
				// Docker provides a more specific label; skip the generic TCP entry
				continue
			}
		}
		result = append(result, db)
	}
	return result
}

// =============================================================================
// Connection Probing
// =============================================================================

// dbCredential holds a username/password pair for probing
type dbCredential struct {
	user     string
	password string
}

// probeDatabase attempts to connect to a discovered database and list its databases
func probeDatabase(db *DiscoveredDatabase, userParam, passwordParam string) {
	dbType := db.Type

	// Unknown types get skipped
	switch dbType {
	case "postgres", "mysql", "mariadb", "mssql", "oracle", "sqlite", "mongodb":
	default:
		db.AuthStatus = "skipped"
		return
	}

	// SQLite: no auth needed, just open and list tables
	if dbType == "sqlite" {
		probeSQLite(db)
		return
	}

	// MongoDB: use native driver
	if dbType == "mongodb" {
		probeMongoDBEntry(db, userParam, passwordParam)
		return
	}

	// SQL databases: build credential list and try each
	creds := defaultCredentials(dbType)
	if userParam != "" {
		creds = append([]dbCredential{{user: userParam, password: passwordParam}}, creds...)
	}

	host := db.Host
	port := db.Port
	filePath := db.FilePath

	var triedUsers []string
	seen := make(map[string]bool)

	for _, cred := range creds {
		driverName, connString := buildProbeConnString(dbType, host, port, filePath, cred.user, cred.password, db.Source, "")
		if connString == "" {
			continue
		}

		sqlDB, err := tryConnect(driverName, connString)
		if err != nil {
			if isAuthError(err) {
				if !seen[cred.user] {
					triedUsers = append(triedUsers, cred.user)
					seen[cred.user] = true
				}
				continue
			}
			// Non-auth error
			db.AuthStatus = "error"
			db.AuthError = err.Error()
			return
		}

		// Success — list databases
		names, err := listDatabaseNames(sqlDB, dbType)
		sqlDB.Close()

		db.AuthStatus = "ok"
		db.AuthUser = cred.user
		db.Databases = names
		if err != nil {
			db.AuthError = fmt.Sprintf("connected but failed to list databases: %v", err)
		}

		// Update config snippet with working credentials
		db.ConfigSnippet["user"] = cred.user
		db.ConfigSnippet["password"] = cred.password

		// Set dbname to first non-system database if available
		if db.ConfigSnippet["dbname"] == "" || db.ConfigSnippet["dbname"] == nil {
			filtered := filterSystemDatabases(dbType, names)
			if len(filtered) > 0 {
				db.ConfigSnippet["dbname"] = filtered[0]
			}
		}
		return
	}

	// All credentials failed
	db.AuthStatus = "auth_failed"
	db.AuthError = fmt.Sprintf("default credentials failed — tried users: %s — provide username and password",
		strings.Join(triedUsers, ", "))
}

// probeSQLite opens a SQLite file and lists its tables
func probeSQLite(db *DiscoveredDatabase) {
	filePath := db.FilePath
	if filePath == "" {
		db.AuthStatus = "error"
		db.AuthError = "no file path"
		return
	}

	sqlDB, err := tryConnect("sqlite", filePath)
	if err != nil {
		db.AuthStatus = "error"
		db.AuthError = err.Error()
		return
	}
	defer sqlDB.Close()

	names, err := listDatabaseNames(sqlDB, "sqlite")
	db.AuthStatus = "ok"
	db.Databases = names
	if err != nil {
		db.AuthError = fmt.Sprintf("opened but failed to list tables: %v", err)
	}
}

// probeMongoDBEntry probes a MongoDB instance using the native driver
func probeMongoDBEntry(db *DiscoveredDatabase, userParam, passwordParam string) {
	host := db.Host
	port := db.Port
	if port == 0 {
		port = 27017
	}

	type mongoCred struct {
		user     string
		password string
		noAuth   bool
	}

	creds := []mongoCred{{noAuth: true}}
	if userParam != "" {
		creds = append([]mongoCred{{user: userParam, password: passwordParam}}, creds...)
	}

	for _, cred := range creds {
		var connString string
		if cred.noAuth {
			connString = fmt.Sprintf("mongodb://%s:%d/?timeoutMS=2000", host, port)
		} else {
			connString = fmt.Sprintf("mongodb://%s:%s@%s:%d/?timeoutMS=2000",
				url.PathEscape(cred.user), url.PathEscape(cred.password), host, port)
		}

		names, err := probeMongoDB(connString)
		if err != nil {
			if isAuthError(err) {
				continue
			}
			db.AuthStatus = "error"
			db.AuthError = err.Error()
			return
		}

		db.AuthStatus = "ok"
		db.Databases = names
		if !cred.noAuth {
			db.AuthUser = cred.user
			db.ConfigSnippet["user"] = cred.user
			db.ConfigSnippet["password"] = cred.password
		}

		// Set dbname to first non-system database if available
		if db.ConfigSnippet["dbname"] == "" || db.ConfigSnippet["dbname"] == nil {
			filtered := filterSystemDatabases("mongodb", names)
			if len(filtered) > 0 {
				db.ConfigSnippet["dbname"] = filtered[0]
			}
		}
		return
	}

	db.AuthStatus = "auth_failed"
	db.AuthError = "authentication failed — provide username and password"
}

// probeMongoDB connects to MongoDB and lists database names
func probeMongoDB(connString string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	client, err := mongo.Connect(options.Client().ApplyURI(connString))
	if err != nil {
		return nil, err
	}
	defer client.Disconnect(ctx) //nolint:errcheck

	if err := client.Ping(ctx, nil); err != nil {
		return nil, err
	}

	names, err := client.ListDatabaseNames(ctx, bson.D{})
	if err != nil {
		return nil, err
	}
	return names, nil
}

// defaultCredentials returns ordered credential sets for a database type
func defaultCredentials(dbType string) []dbCredential {
	switch dbType {
	case "postgres":
		osUser := currentOSUser()
		creds := []dbCredential{
			{user: "postgres", password: ""},
		}
		if osUser != "" && osUser != "postgres" {
			creds = append(creds, dbCredential{user: osUser, password: ""})
		}
		creds = append(creds,
			dbCredential{user: "postgres", password: "postgres"},
			dbCredential{user: "root", password: ""},
		)
		return creds
	case "mysql", "mariadb":
		return []dbCredential{
			{user: "root", password: ""},
			{user: "root", password: "root"},
		}
	case "mssql":
		return []dbCredential{
			{user: "sa", password: ""},
		}
	case "oracle":
		return []dbCredential{
			{user: "system", password: ""},
			{user: "system", password: "oracle"},
		}
	default:
		return nil
	}
}

// buildProbeConnString builds a driver name and connection string for probing
func buildProbeConnString(dbType, host string, port int, filePath, user, password, source, dbName string) (string, string) {
	switch dbType {
	case "postgres":
		return buildPostgresProbeConn(host, port, user, password, source, dbName)
	case "mysql", "mariadb":
		return buildMySQLProbeConn(host, port, user, password, source, dbName)
	case "mssql":
		if port == 0 {
			port = 1433
		}
		connString := fmt.Sprintf("sqlserver://%s:%s@%s:%d?encrypt=disable",
			url.PathEscape(user), url.PathEscape(password), host, port)
		if dbName != "" {
			connString += "&database=" + url.QueryEscape(dbName)
		}
		return "sqlserver", connString
	case "oracle":
		if port == 0 {
			port = 1521
		}
		dbPath := "/"
		if dbName != "" {
			dbPath = "/" + dbName
		}
		connString := fmt.Sprintf("oracle://%s:%s@%s:%d%s",
			user, password, host, port, dbPath)
		return "oracle", connString
	case "sqlite":
		return "sqlite", filePath
	default:
		return "", ""
	}
}

// buildPostgresProbeConn builds a pgx connection for probing
func buildPostgresProbeConn(host string, port int, user, password, source, dbName string) (string, string) {
	if port == 0 {
		port = 5432
	}

	dbPath := "/"
	if dbName != "" {
		dbPath = "/" + url.PathEscape(dbName)
	}

	var connStr string
	if source == "unix_socket" {
		// host is the socket path; extract directory
		socketDir := filepath.Dir(host)
		connStr = fmt.Sprintf("postgres://%s:%s@%s?host=%s&port=%d&sslmode=disable",
			url.PathEscape(user), url.PathEscape(password), dbPath, url.PathEscape(socketDir), port)
	} else {
		connStr = fmt.Sprintf("postgres://%s:%s@%s:%d%s?sslmode=disable",
			url.PathEscape(user), url.PathEscape(password), host, port, dbPath)
	}

	config, err := pgx.ParseConfig(connStr)
	if err != nil {
		return "", ""
	}

	return "pgx", stdlib.RegisterConnConfig(config)
}

// buildMySQLProbeConn builds a MySQL connection string for probing
func buildMySQLProbeConn(host string, port int, user, password, source, dbName string) (string, string) {
	if port == 0 {
		port = 3306
	}

	var connString string
	if source == "unix_socket" {
		connString = fmt.Sprintf("%s:%s@unix(%s)/%s", user, password, host, dbName)
	} else {
		connString = fmt.Sprintf("%s:%s@tcp(%s:%d)/%s", user, password, host, port, dbName)
	}
	return "mysql", connString
}

// tryConnect opens a database connection and pings it with a 2s timeout
func tryConnect(driverName, connString string) (*sql.DB, error) {
	db, err := sql.Open(driverName, connString)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// listDatabaseNames runs the appropriate query to list databases/schemas/tables
func listDatabaseNames(db *sql.DB, dbType string) ([]string, error) {
	var query string
	switch dbType {
	case "postgres":
		query = "SELECT datname FROM pg_database WHERE datistemplate = false"
	case "mysql", "mariadb":
		query = "SELECT schema_name FROM information_schema.schemata"
	case "mssql":
		query = "SELECT name FROM sys.databases WHERE database_id > 4"
	case "oracle":
		query = "SELECT username FROM all_users WHERE oracle_maintained = 'N'"
	case "sqlite":
		query = "SELECT name FROM sqlite_master WHERE type='table'"
	default:
		return nil, fmt.Errorf("unsupported database type: %s", dbType)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		// For Oracle, fall back to alternate query
		if dbType == "oracle" {
			return listOracleFallback(db)
		}
		return nil, err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// listOracleFallback tries an alternate query for Oracle
func listOracleFallback(db *sql.DB) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	rows, err := db.QueryContext(ctx, "SELECT DISTINCT owner FROM all_tables")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// isAuthError checks if an error is an authentication failure
func isAuthError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	authPatterns := []string{
		"password authentication failed", // postgres
		"access denied",                   // mysql/mariadb
		"login failed",                    // mssql
		"ora-01017",                       // oracle
		"authentication failed",           // mongodb
	}
	for _, pattern := range authPatterns {
		if strings.Contains(msg, pattern) {
			return true
		}
	}
	// PostgreSQL: FATAL: role "X" does not exist (SQLSTATE 28000) — e.g., Homebrew installs
	// Require "fatal" prefix to avoid false positives from MSSQL/Oracle DDL errors
	if strings.Contains(msg, "fatal") && strings.Contains(msg, "role") && strings.Contains(msg, "does not exist") {
		return true
	}
	return false
}

// currentOSUser returns the current OS username, with fallback to $USER
func currentOSUser() string {
	if u, err := osuser.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return os.Getenv("USER")
}
