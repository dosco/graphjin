package serv

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net"
	"net/url"
	"os"
	"os/exec"
	osuser "os/user"
	"path/filepath"
	"sort"
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
		mcp.WithBoolean("scan_local",
			mcp.Description("Scan localhost ports/sockets/sqlite files (default: true)")),
		mcp.WithArray("targets",
			mcp.Description("Optional explicit targets to probe. Each item: {type?, host, port?, source_label?, user?, password?, dbname?}."),
		),
		mcp.WithArray("scan_ports",
			mcp.Description("Optional custom port list for localhost scanning (default uses known DB ports)."),
			mcp.WithNumberItems(),
		),
		mcp.WithNumber("probe_timeout_ms",
			mcp.Description("Probe timeout in milliseconds (default: 500)")),
		mcp.WithBoolean("include_system_databases",
			mcp.Description("Include system databases in discovered database lists (default from mcp.default_db_allowed)")),
		mcp.WithNumber("sqlite_max_depth",
			mcp.Description("SQLite file scan depth from scan_dir (default: 1, 0 = only scan_dir)")),
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
	CandidateID   string         `json:"candidate_id,omitempty"`
	Rank          int            `json:"rank,omitempty"`
	Confidence    string         `json:"confidence,omitempty"`
	Reasons       []string       `json:"reasons,omitempty"`
	NextActions   []string       `json:"next_actions,omitempty"`
	ProbeStatus   string         `json:"probe_status_code,omitempty"`
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

type discoverOptions struct {
	scanDir               string
	skipDocker            bool
	skipProbe             bool
	user                  string
	password              string
	scanLocal             bool
	scanPorts             []int
	probeTimeout          time.Duration
	includeSystemDatabase bool
	sqliteMaxDepth        int
	targets               []DiscoverTarget
}

// DiscoverTarget is an explicit host target to probe (local or remote).
type DiscoverTarget struct {
	Type        string `json:"type,omitempty"`
	Host        string `json:"host"`
	Port        int    `json:"port,omitempty"`
	SourceLabel string `json:"source_label,omitempty"`
	User        string `json:"user,omitempty"`
	Password    string `json:"password,omitempty"`
	DBName      string `json:"dbname,omitempty"`
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

	// Query all configured database connections
	queried := make(map[string]bool) // track host:port to avoid duplicates

	// Sort s.dbs keys for deterministic output order
	dbNames := make([]string, 0, len(ms.service.dbs))
	for name := range ms.service.dbs {
		dbNames = append(dbNames, name)
	}
	sort.Strings(dbNames)
	for _, name := range dbNames {
		db := ms.service.dbs[name]
		dbConf, ok := conf.Databases[name]
		if !ok {
			continue
		}
		hostPort := fmt.Sprintf("%s:%d", dbConf.Host, dbConf.Port)
		if queried[hostPort] {
			continue
		}
		queried[hostPort] = true

		dbType := strings.ToLower(dbConf.Type)
		if dbType == "" {
			dbType = ms.service.conf.DBType
		}
		names, err := listDatabaseNames(db, dbType)
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

	// Sort conf.Databases keys for deterministic output order
	confNames := make([]string, 0, len(conf.Databases))
	for name := range conf.Databases {
		confNames = append(confNames, name)
	}
	sort.Strings(confNames)
	for _, name := range confNames {
		dbConf := conf.Databases[name]
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
	args := req.GetArguments()
	result, err := ms.runDiscovery(args)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	ms.cacheCandidates(result.Databases)

	data, err := mcpMarshalJSON(result, true)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal result: %v", err)), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

func (ms *mcpServer) runDiscovery(args map[string]any) (DiscoverResult, error) {
	start := time.Now()

	opts, err := parseDiscoverOptions(ms, args)
	if err != nil {
		return DiscoverResult{}, err
	}

	timeout := opts.probeTimeout

	// TCP port probes for all supported database types
	defaultTCPProbes := []dbProbe{
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
	tcpProbes := defaultTCPProbes
	if len(opts.scanPorts) > 0 {
		tcpProbes = make([]dbProbe, 0, len(opts.scanPorts))
		for _, p := range opts.scanPorts {
			tcpProbes = append(tcpProbes, dbProbe{dbType: inferDBTypeFromPort(p), port: p})
		}
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

	if opts.scanLocal {
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

		// Phase 3: SQLite file scan
		sqliteFiles := findSQLiteFiles(opts.scanDir, opts.sqliteMaxDepth)
		for _, f := range sqliteFiles {
			databases = append(databases, DiscoveredDatabase{
				Type:          "sqlite",
				FilePath:      f,
				Source:        "file",
				Status:        "found",
				ConfigSnippet: buildConfigSnippet("sqlite", "", 0, f),
			})
		}
	}

	// Wait for TCP and socket probes to finish
	wg.Wait()

	// Phase 4: Explicit targets (local or remote)
	for _, target := range opts.targets {
		dbType := strings.ToLower(target.Type)
		if dbType == "" {
			dbType = inferDBTypeFromPort(target.Port)
			if dbType == "" {
				dbType = "unknown"
			}
		}
		port := target.Port
		if port == 0 {
			port = defaultPortForType(dbType)
		}
		source := "target"
		if target.SourceLabel != "" {
			source = "target:" + target.SourceLabel
		}
		status := "unreachable"
		if target.Host != "" && port > 0 && checkTCPPort(target.Host, port, timeout) {
			status = "listening"
		}
		databases = append(databases, DiscoveredDatabase{
			Type:          dbType,
			Host:          target.Host,
			Port:          port,
			Source:        source,
			Status:        status,
			ConfigSnippet: buildConfigSnippet(dbType, target.Host, port, ""),
		})
	}

	// Phase 5: Docker detection
	dockerStatus := "skipped"
	if !opts.skipDocker {
		dockerDBs, status := discoverDockerDatabases()
		dockerStatus = status
		if len(dockerDBs) > 0 {
			databases = append(databases, dockerDBs...)
		}
	}

	// Deduplicate merged candidates by endpoint identity.
	databases = deduplicateDatabases(databases)

	// Phase 6: Connection probing (concurrent)
	if !opts.skipProbe && len(databases) > 0 {
		var probeWg sync.WaitGroup
		for i := range databases {
			probeWg.Add(1)
			go func(db *DiscoveredDatabase) {
				defer probeWg.Done()
				credUser := opts.user
				credPassword := opts.password
				if db.Source == "target" || strings.HasPrefix(db.Source, "target:") {
					for _, t := range opts.targets {
						if t.Host == db.Host && t.Port == db.Port {
							if t.User != "" {
								credUser = t.User
								credPassword = t.Password
							}
							break
						}
					}
				}
				probeDatabase(db, credUser, credPassword)
			}(&databases[i])
		}
		probeWg.Wait()
	}

	// Filter system databases from results (unless allowed by option/config)
	if !opts.includeSystemDatabase {
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

	for i := range databases {
		enrichDiscoveredDatabase(&databases[i])
	}
	sortDiscoveredDatabases(databases)

	// Build summary
	typeSet := make(map[string]bool)
	for _, db := range databases {
		typeSet[db.Type] = true
	}
	var types []string
	for t := range typeSet {
		types = append(types, t)
	}
	sort.Strings(types)

	result := DiscoverResult{
		Databases: databases,
		Summary: DiscoverSummary{
			TotalFound:     len(databases),
			DatabaseTypes:  types,
			ScanDurationMs: time.Since(start).Milliseconds(),
		},
		DockerStatus: dockerStatus,
	}
	return result, nil
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
func findSQLiteFiles(dir string, maxDepth int) []string {
	if maxDepth < 0 {
		maxDepth = 0
	}
	var files []string
	patterns := map[string]bool{".db": true, ".sqlite": true, ".sqlite3": true}
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			rel, relErr := filepath.Rel(dir, path)
			if relErr == nil && rel != "." && strings.Count(rel, string(filepath.Separator)) >= maxDepth {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if patterns[ext] {
			files = append(files, path)
		}
		return nil
	})
	sort.Strings(files)
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

	// Docker image prefix to DB type mapping (ordered slice for deterministic matching)
	type imageMapping struct {
		prefix string
		dbType string
	}
	imageMappings := []imageMapping{
		{"postgres", "postgres"},
		{"mysql", "mysql"},
		{"mariadb", "mariadb"},
		{"mcr.microsoft.com/mssql", "mssql"},
		{"mongo", "mongodb"},
		{"oracle", "oracle"},
		{"gvenzl/oracle", "oracle"},
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
		for _, m := range imageMappings {
			if strings.HasPrefix(image, m.prefix) {
				dbType = m.dbType
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
	type bucket struct {
		db       DiscoveredDatabase
		priority int
	}
	sourcePriority := func(source string) int {
		switch {
		case source == "docker":
			return 5
		case strings.HasPrefix(source, "target"):
			return 4
		case source == "tcp":
			return 3
		case source == "unix_socket":
			return 2
		case source == "file":
			return 1
		default:
			return 0
		}
	}
	keyFor := func(db DiscoveredDatabase) string {
		switch db.Source {
		case "file":
			return "file:" + db.FilePath
		case "unix_socket":
			return "unix:" + db.Host
		default:
			return fmt.Sprintf("tcp:%s:%d", strings.ToLower(db.Host), db.Port)
		}
	}

	merged := make(map[string]bucket, len(dbs))
	for _, db := range dbs {
		key := keyFor(db)
		pr := sourcePriority(db.Source)
		existing, ok := merged[key]
		if !ok || pr > existing.priority {
			merged[key] = bucket{db: db, priority: pr}
			continue
		}
		// Preserve docker/container metadata and discovered names where useful.
		cur := existing.db
		if cur.DockerInfo == nil && db.DockerInfo != nil {
			cur.DockerInfo = db.DockerInfo
		}
		if len(cur.Databases) == 0 && len(db.Databases) > 0 {
			cur.Databases = db.Databases
		}
		merged[key] = bucket{db: cur, priority: existing.priority}
	}

	result := make([]DiscoveredDatabase, 0, len(merged))
	for _, v := range merged {
		result = append(result, v.db)
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
			db.ProbeStatus = classifyProbeError(err)
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
			db.ProbeStatus = classifyProbeError(err)
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
	db.ProbeStatus = "auth_failed"
}

// probeSQLite opens a SQLite file and lists its tables
func probeSQLite(db *DiscoveredDatabase) {
	filePath := db.FilePath
	if filePath == "" {
		db.AuthStatus = "error"
		db.AuthError = "no file path"
		db.ProbeStatus = "bad_input"
		return
	}

	sqlDB, err := tryConnect("sqlite", filePath)
	if err != nil {
		db.AuthStatus = "error"
		db.AuthError = err.Error()
		db.ProbeStatus = classifyProbeError(err)
		return
	}
	defer sqlDB.Close()

	names, err := listDatabaseNames(sqlDB, "sqlite")
	db.AuthStatus = "ok"
	db.Databases = names
	if err != nil {
		db.AuthError = fmt.Sprintf("opened but failed to list tables: %v", err)
		db.ProbeStatus = classifyProbeError(err)
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
			db.ProbeStatus = classifyProbeError(err)
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
	db.ProbeStatus = "auth_failed"
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
		"access denied",                  // mysql/mariadb
		"login failed",                   // mssql
		"ora-01017",                      // oracle
		"authentication failed",          // mongodb
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

func inferDBTypeFromPort(port int) string {
	switch port {
	case 5432, 5433, 5434:
		return "postgres"
	case 3306, 3307:
		return "mysql"
	case 1433, 1434:
		return "mssql"
	case 1521, 1522:
		return "oracle"
	case 27017, 27018:
		return "mongodb"
	default:
		return ""
	}
}

func classifyProbeError(err error) string {
	if err == nil {
		return "ok"
	}
	msg := strings.ToLower(err.Error())
	switch {
	case isAuthError(err):
		return "auth_failed"
	case strings.Contains(msg, "timeout") || strings.Contains(msg, "i/o timeout"):
		return "timeout"
	case strings.Contains(msg, "no such host"), strings.Contains(msg, "connection refused"),
		strings.Contains(msg, "network is unreachable"):
		return "network_unreachable"
	case strings.Contains(msg, "ssl"), strings.Contains(msg, "tls"), strings.Contains(msg, "certificate"):
		return "tls_required"
	default:
		return "error"
	}
}

func enrichDiscoveredDatabase(db *DiscoveredDatabase) {
	if db.CandidateID == "" {
		db.CandidateID = buildCandidateID(*db)
	}
	var reasons []string
	switch db.Source {
	case "docker":
		reasons = append(reasons, "detected via docker container")
	case "tcp":
		reasons = append(reasons, "detected open TCP port")
	case "unix_socket":
		reasons = append(reasons, "detected unix socket")
	case "file":
		reasons = append(reasons, "detected sqlite file")
	default:
		if strings.HasPrefix(db.Source, "target") {
			reasons = append(reasons, "explicitly targeted endpoint")
		}
	}

	rank := 10
	if db.AuthStatus == "ok" {
		rank += 60
		reasons = append(reasons, "authentication succeeded")
	}
	if len(db.Databases) > 0 {
		rank += 20
		reasons = append(reasons, "listed databases successfully")
	}
	if db.Status == "listening" || db.Status == "running" {
		rank += 10
	}
	if db.Source == "docker" {
		rank += 5
	}
	if strings.HasPrefix(db.Source, "target") {
		rank += 5
	}
	db.Rank = rank
	db.Reasons = reasons

	switch {
	case db.AuthStatus == "ok":
		db.Confidence = "high"
	case db.Status == "listening" || db.Status == "running":
		db.Confidence = "medium"
	default:
		db.Confidence = "low"
	}

	var actions []string
	switch db.AuthStatus {
	case "ok":
		actions = append(actions, "select_candidate", "apply_config")
	case "auth_failed":
		actions = append(actions, "provide_credentials", "retest_connection")
	default:
		actions = append(actions, "test_connection")
	}
	db.NextActions = actions

	if db.ProbeStatus == "" {
		if db.AuthStatus == "ok" {
			db.ProbeStatus = "ok"
		} else if db.AuthStatus == "auth_failed" {
			db.ProbeStatus = "auth_failed"
		}
	}
}

func sortDiscoveredDatabases(dbs []DiscoveredDatabase) {
	sort.SliceStable(dbs, func(i, j int) bool {
		if dbs[i].Rank != dbs[j].Rank {
			return dbs[i].Rank > dbs[j].Rank
		}
		if dbs[i].Type != dbs[j].Type {
			return dbs[i].Type < dbs[j].Type
		}
		if dbs[i].Host != dbs[j].Host {
			return dbs[i].Host < dbs[j].Host
		}
		return dbs[i].Port < dbs[j].Port
	})
}

func buildCandidateID(db DiscoveredDatabase) string {
	base := map[string]any{
		"type":      db.Type,
		"host":      strings.ToLower(db.Host),
		"port":      db.Port,
		"file_path": db.FilePath,
		"source":    db.Source,
	}
	b, _ := json.Marshal(base)
	h := fnv.New64a()
	_, _ = h.Write(b)
	return fmt.Sprintf("db-%x", h.Sum64())
}

func parseDiscoverOptions(ms *mcpServer, args map[string]any) (discoverOptions, error) {
	scanDir, _ := args["scan_dir"].(string)
	if scanDir == "" {
		scanDir, _ = os.Getwd()
	}

	opts := discoverOptions{
		scanDir:               scanDir,
		skipDocker:            false,
		skipProbe:             false,
		scanLocal:             true,
		probeTimeout:          500 * time.Millisecond,
		includeSystemDatabase: ms.service.conf.MCP.DefaultDBAllowed,
		sqliteMaxDepth:        1,
	}
	opts.user, _ = args["user"].(string)
	opts.password, _ = args["password"].(string)
	if v, ok := args["skip_docker"].(bool); ok {
		opts.skipDocker = v
	}
	if v, ok := args["skip_probe"].(bool); ok {
		opts.skipProbe = v
	}
	if v, ok := args["scan_local"].(bool); ok {
		opts.scanLocal = v
	}
	if v, ok := args["include_system_databases"].(bool); ok {
		opts.includeSystemDatabase = v
	}
	if v, ok := args["probe_timeout_ms"].(float64); ok && v > 0 {
		opts.probeTimeout = time.Duration(v) * time.Millisecond
	}
	if v, ok := args["sqlite_max_depth"].(float64); ok && v >= 0 {
		opts.sqliteMaxDepth = int(v)
	}
	if raw, ok := args["scan_ports"].([]any); ok {
		for _, p := range raw {
			switch val := p.(type) {
			case float64:
				if val > 0 {
					opts.scanPorts = append(opts.scanPorts, int(val))
				}
			case int:
				if val > 0 {
					opts.scanPorts = append(opts.scanPorts, val)
				}
			}
		}
	}
	if rawTargets, ok := args["targets"].([]any); ok && len(rawTargets) > 0 {
		for _, raw := range rawTargets {
			tm, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			var t DiscoverTarget
			t.Type, _ = tm["type"].(string)
			t.Host, _ = tm["host"].(string)
			t.SourceLabel, _ = tm["source_label"].(string)
			t.User, _ = tm["user"].(string)
			t.Password, _ = tm["password"].(string)
			t.DBName, _ = tm["dbname"].(string)
			if p, ok := tm["port"].(float64); ok {
				t.Port = int(p)
			}
			if t.Host == "" {
				continue
			}
			opts.targets = append(opts.targets, t)
		}
	}
	return opts, nil
}
