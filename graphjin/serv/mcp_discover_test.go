package serv

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/server"

	_ "modernc.org/sqlite"
)

// =============================================================================
// Registration Gating Tests
// =============================================================================

func TestRegisterDiscoverTools_Gating(t *testing.T) {
	t.Run("not registered when AllowDevTools is false", func(t *testing.T) {
		ms := mockMcpServerWithConfig(MCPConfig{
			AllowDevTools: false,
		})
		ms.srv = server.NewMCPServer("test", "0.0.0")
		ms.registerDiscoverTools()

		tools := ms.srv.ListTools()
		if _, exists := tools["discover_databases"]; exists {
			t.Error("discover_databases should not be registered when AllowDevTools is false")
		}
	})

	t.Run("registered when AllowDevTools is true", func(t *testing.T) {
		ms := mockMcpServerWithConfig(MCPConfig{
			AllowDevTools: true,
		})
		ms.srv = server.NewMCPServer("test", "0.0.0")
		ms.registerDiscoverTools()

		tools := ms.srv.ListTools()
		if _, exists := tools["discover_databases"]; !exists {
			t.Error("discover_databases should be registered when AllowDevTools is true")
		}
	})
}

// =============================================================================
// Handler Tests
// =============================================================================

func TestHandleDiscoverDatabases_NoGJRequired(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{
		AllowDevTools: true,
	})
	// gj is nil — should still work
	ctx := context.Background()

	req := newToolRequest(map[string]any{
		"skip_docker": true,
		"scan_dir":    "/nonexistent/path/that/does/not/exist",
	})

	result, err := ms.handleDiscoverDatabases(ctx, req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("Expected result, got nil")
	}
	if result.IsError {
		t.Fatal("Expected success, got error")
	}

	// Parse the response
	text := assertToolSuccess(t, result)
	var resp DiscoverResult
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Should have a valid summary
	if resp.Summary.ScanDurationMs < 0 {
		t.Error("Expected non-negative scan duration")
	}
	if resp.DockerStatus != "skipped" {
		t.Errorf("Expected docker_status 'skipped', got %q", resp.DockerStatus)
	}
}

// =============================================================================
// Helper Function Tests
// =============================================================================

func TestCheckTCPPort_ClosedPort(t *testing.T) {
	// Port 1 on localhost should not be listening (requires root to bind)
	if checkTCPPort("127.0.0.1", 1, 100*1e6) {
		t.Skip("Port 1 is unexpectedly open on this system")
	}
}

func TestCheckUnixSocket_NonexistentPath(t *testing.T) {
	if checkUnixSocket("/tmp/nonexistent_socket_path_12345.sock", 100*1e6) {
		t.Error("Expected false for nonexistent socket path")
	}
}

func TestFindSQLiteFiles_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	files := findSQLiteFiles(dir)
	if len(files) != 0 {
		t.Errorf("Expected 0 files in empty dir, got %d", len(files))
	}
}

func TestFindSQLiteFiles_WithDBFiles(t *testing.T) {
	dir := t.TempDir()

	// Create test files
	testFiles := []string{"test.db", "data.sqlite", "app.sqlite3"}
	for _, f := range testFiles {
		path := filepath.Join(dir, f)
		if err := os.WriteFile(path, []byte(""), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", f, err)
		}
	}

	// Also create a non-matching file
	nonMatch := filepath.Join(dir, "readme.txt")
	if err := os.WriteFile(nonMatch, []byte(""), 0644); err != nil {
		t.Fatalf("Failed to create non-matching file: %v", err)
	}

	files := findSQLiteFiles(dir)
	if len(files) != 3 {
		t.Errorf("Expected 3 SQLite files, got %d: %v", len(files), files)
	}

	// Verify all expected files are found
	found := make(map[string]bool)
	for _, f := range files {
		found[filepath.Base(f)] = true
	}
	for _, expected := range testFiles {
		if !found[expected] {
			t.Errorf("Expected to find %s in results", expected)
		}
	}
}

func TestParseDockerHostPort(t *testing.T) {
	tests := []struct {
		name        string
		portsStr    string
		defaultPort int
		expected    int
	}{
		{
			name:        "standard port mapping",
			portsStr:    "0.0.0.0:5432->5432/tcp",
			defaultPort: 5432,
			expected:    5432,
		},
		{
			name:        "custom host port",
			portsStr:    "0.0.0.0:15432->5432/tcp",
			defaultPort: 5432,
			expected:    15432,
		},
		{
			name:        "empty string returns default",
			portsStr:    "",
			defaultPort: 3306,
			expected:    3306,
		},
		{
			name:        "multiple port mappings takes first",
			portsStr:    "0.0.0.0:5432->5432/tcp, 0.0.0.0:5433->5433/tcp",
			defaultPort: 5432,
			expected:    5432,
		},
		{
			name:        "no arrow returns default",
			portsStr:    "5432/tcp",
			defaultPort: 5432,
			expected:    5432,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseDockerHostPort(tt.portsStr, tt.defaultPort)
			if result != tt.expected {
				t.Errorf("parseDockerHostPort(%q, %d) = %d, expected %d",
					tt.portsStr, tt.defaultPort, result, tt.expected)
			}
		})
	}
}

func TestBuildConfigSnippet_AllTypes(t *testing.T) {
	tests := []struct {
		dbType     string
		host       string
		port       int
		filePath   string
		expectUser string
		expectPath bool
	}{
		{"postgres", "localhost", 5432, "", "postgres", false},
		{"mysql", "localhost", 3306, "", "root", false},
		{"mariadb", "localhost", 3306, "", "root", false},
		{"mssql", "localhost", 1433, "", "sa", false},
		{"oracle", "localhost", 1521, "", "system", false},
		{"mongodb", "localhost", 27017, "", "", false},
		{"sqlite", "", 0, "/data/app.db", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.dbType, func(t *testing.T) {
			snippet := buildConfigSnippet(tt.dbType, tt.host, tt.port, tt.filePath)

			if snippet["type"] != tt.dbType {
				t.Errorf("Expected type %q, got %q", tt.dbType, snippet["type"])
			}

			if tt.expectPath {
				if snippet["path"] != tt.filePath {
					t.Errorf("Expected path %q, got %v", tt.filePath, snippet["path"])
				}
				// SQLite should not have host/port/user
				if _, ok := snippet["host"]; ok {
					t.Error("SQLite snippet should not have host")
				}
				return
			}

			if tt.host != "" {
				if snippet["host"] != tt.host {
					t.Errorf("Expected host %q, got %v", tt.host, snippet["host"])
				}
			}
			if tt.port > 0 {
				if snippet["port"] != tt.port {
					t.Errorf("Expected port %d, got %v", tt.port, snippet["port"])
				}
			}
			if tt.expectUser != "" {
				if snippet["user"] != tt.expectUser {
					t.Errorf("Expected user %q, got %v", tt.expectUser, snippet["user"])
				}
			}
			if tt.dbType != "sqlite" && tt.dbType != "mongodb" {
				if _, ok := snippet["dbname"]; !ok {
					t.Error("Expected dbname in snippet")
				}
			}
		})
	}
}

func TestDefaultPortForType(t *testing.T) {
	tests := []struct {
		dbType   string
		expected int
	}{
		{"postgres", 5432},
		{"mysql", 3306},
		{"mariadb", 3306},
		{"mssql", 1433},
		{"oracle", 1521},
		{"mongodb", 27017},
		{"sqlite", 0},
		{"unknown", 0},
	}

	for _, tt := range tests {
		t.Run(tt.dbType, func(t *testing.T) {
			result := defaultPortForType(tt.dbType)
			if result != tt.expected {
				t.Errorf("defaultPortForType(%q) = %d, expected %d", tt.dbType, result, tt.expected)
			}
		})
	}
}

// =============================================================================
// Deduplication Tests
// =============================================================================

func TestDeduplicateDatabases(t *testing.T) {
	t.Run("docker overrides tcp for same port", func(t *testing.T) {
		dbs := []DiscoveredDatabase{
			{Type: "mysql", Host: "localhost", Port: 3306, Source: "tcp"},
			{Type: "mariadb", Host: "localhost", Port: 3306, Source: "docker"},
		}
		result := deduplicateDatabases(dbs)
		if len(result) != 1 {
			t.Fatalf("Expected 1 database after dedup, got %d", len(result))
		}
		if result[0].Type != "mariadb" {
			t.Errorf("Expected mariadb (from docker), got %s", result[0].Type)
		}
	})

	t.Run("different ports kept", func(t *testing.T) {
		dbs := []DiscoveredDatabase{
			{Type: "postgres", Host: "localhost", Port: 5432, Source: "tcp"},
			{Type: "mysql", Host: "localhost", Port: 3306, Source: "tcp"},
		}
		result := deduplicateDatabases(dbs)
		if len(result) != 2 {
			t.Fatalf("Expected 2 databases, got %d", len(result))
		}
	})

	t.Run("unix sockets not deduped by docker", func(t *testing.T) {
		dbs := []DiscoveredDatabase{
			{Type: "postgres", Host: "/tmp/.s.PGSQL.5432", Port: 5432, Source: "unix_socket"},
			{Type: "postgres", Host: "localhost", Port: 5432, Source: "docker"},
		}
		result := deduplicateDatabases(dbs)
		if len(result) != 2 {
			t.Fatalf("Expected 2 databases (unix_socket not deduped), got %d", len(result))
		}
	})
}

// =============================================================================
// Response Structure Tests
// =============================================================================

func TestDiscoverResult_JSONStructure(t *testing.T) {
	result := DiscoverResult{
		Databases: []DiscoveredDatabase{
			{
				Type:   "postgres",
				Host:   "localhost",
				Port:   5432,
				Source: "tcp",
				Status: "listening",
				ConfigSnippet: map[string]any{
					"type":     "postgres",
					"host":     "localhost",
					"port":     5432,
					"user":     "postgres",
					"password": "",
					"dbname":   "",
				},
			},
		},
		Summary: DiscoverSummary{
			TotalFound:     1,
			DatabaseTypes:  []string{"postgres"},
			ScanDurationMs: 512,
		},
		DockerStatus: "available",
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	// Verify it contains expected fields
	text := string(data)
	for _, expected := range []string{
		`"type":"postgres"`,
		`"host":"localhost"`,
		`"port":5432`,
		`"source":"tcp"`,
		`"status":"listening"`,
		`"total_found":1`,
		`"docker_status":"available"`,
	} {
		if !strings.Contains(text, expected) {
			t.Errorf("Expected JSON to contain %s, got: %s", expected, text)
		}
	}
}

// =============================================================================
// Connection Probing Tests
// =============================================================================

func TestDefaultCredentials(t *testing.T) {
	tests := []struct {
		dbType        string
		expectMinLen  int
		expectFirst   string // expected first username
	}{
		{"postgres", 3, "postgres"},
		{"mysql", 2, "root"},
		{"mariadb", 2, "root"},
		{"mssql", 1, "sa"},
		{"oracle", 2, "system"},
		{"unknown", 0, ""},
	}

	for _, tt := range tests {
		t.Run(tt.dbType, func(t *testing.T) {
			creds := defaultCredentials(tt.dbType)
			if len(creds) < tt.expectMinLen {
				t.Errorf("Expected at least %d credentials for %s, got %d", tt.expectMinLen, tt.dbType, len(creds))
			}
			if tt.expectFirst != "" && len(creds) > 0 {
				if creds[0].user != tt.expectFirst {
					t.Errorf("Expected first user %q, got %q", tt.expectFirst, creds[0].user)
				}
			}
		})
	}
}

func TestDefaultCredentials_PostgresOrder(t *testing.T) {
	creds := defaultCredentials("postgres")

	// First should be postgres with empty password
	if creds[0].user != "postgres" || creds[0].password != "" {
		t.Errorf("Expected first cred postgres/'', got %s/%s", creds[0].user, creds[0].password)
	}

	// Last should be root with empty password
	last := creds[len(creds)-1]
	if last.user != "root" || last.password != "" {
		t.Errorf("Expected last cred root/'', got %s/%s", last.user, last.password)
	}
}

func TestDefaultCredentials_MySQLOrder(t *testing.T) {
	creds := defaultCredentials("mysql")
	if len(creds) != 2 {
		t.Fatalf("Expected 2 creds for mysql, got %d", len(creds))
	}
	if creds[0].user != "root" || creds[0].password != "" {
		t.Errorf("Expected first cred root/'', got %s/%s", creds[0].user, creds[0].password)
	}
	if creds[1].user != "root" || creds[1].password != "root" {
		t.Errorf("Expected second cred root/root, got %s/%s", creds[1].user, creds[1].password)
	}
}

func TestBuildProbeConnString_AllTypes(t *testing.T) {
	tests := []struct {
		dbType       string
		host         string
		port         int
		filePath     string
		user         string
		password     string
		source       string
		expectDriver string
		expectNonEmpty bool
	}{
		{"postgres", "localhost", 5432, "", "postgres", "", "tcp", "pgx", true},
		{"mysql", "localhost", 3306, "", "root", "", "tcp", "mysql", true},
		{"mariadb", "localhost", 3306, "", "root", "", "tcp", "mysql", true},
		{"mssql", "localhost", 1433, "", "sa", "", "tcp", "sqlserver", true},
		{"oracle", "localhost", 1521, "", "system", "", "tcp", "oracle", true},
		{"sqlite", "", 0, "/tmp/test.db", "", "", "file", "sqlite", true},
		{"unknown", "localhost", 1234, "", "user", "", "tcp", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.dbType, func(t *testing.T) {
			driver, connStr := buildProbeConnString(tt.dbType, tt.host, tt.port, tt.filePath, tt.user, tt.password, tt.source, "")
			if tt.expectNonEmpty {
				if driver != tt.expectDriver {
					t.Errorf("Expected driver %q, got %q", tt.expectDriver, driver)
				}
				if connStr == "" {
					t.Error("Expected non-empty connection string")
				}
			} else {
				if driver != "" || connStr != "" {
					t.Errorf("Expected empty driver/connStr for %s, got %q/%q", tt.dbType, driver, connStr)
				}
			}
		})
	}
}

func TestBuildProbeConnString_UnixSocket(t *testing.T) {
	t.Run("postgres unix socket", func(t *testing.T) {
		driver, connStr := buildProbeConnString("postgres", "/tmp/.s.PGSQL.5432", 5432, "", "postgres", "", "unix_socket", "")
		if driver != "pgx" {
			t.Errorf("Expected driver pgx, got %q", driver)
		}
		if connStr == "" {
			t.Error("Expected non-empty connection string for postgres unix socket")
		}
	})

	t.Run("mysql unix socket", func(t *testing.T) {
		driver, connStr := buildProbeConnString("mysql", "/tmp/mysql.sock", 3306, "", "root", "", "unix_socket", "")
		if driver != "mysql" {
			t.Errorf("Expected driver mysql, got %q", driver)
		}
		if !strings.Contains(connStr, "unix(/tmp/mysql.sock)") {
			t.Errorf("Expected unix socket path in conn string, got %q", connStr)
		}
	})
}

func TestBuildProbeConnString_MSSQLSpecialChars(t *testing.T) {
	driver, connStr := buildProbeConnString("mssql", "localhost", 1433, "", "sa", "P@ss!word", "tcp", "")
	if driver != "sqlserver" {
		t.Errorf("Expected driver sqlserver, got %q", driver)
	}
	// url.PathEscape encodes ! but not @
	if !strings.Contains(connStr, "P@ss%21word") {
		t.Errorf("Expected URL-encoded password in conn string, got %q", connStr)
	}
}

func TestIsAuthError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"postgres password fail", fmt.Errorf("password authentication failed for user \"postgres\""), true},
		{"mysql access denied", fmt.Errorf("Error 1045 (28000): Access denied for user 'root'@'localhost'"), true},
		{"mssql login failed", fmt.Errorf("Login failed for user 'sa'"), true},
		{"oracle auth", fmt.Errorf("ORA-01017: invalid username/password"), true},
		{"mongodb auth", fmt.Errorf("authentication failed"), true},
		{"connection refused", fmt.Errorf("dial tcp 127.0.0.1:5432: connect: connection refused"), false},
		{"timeout", fmt.Errorf("i/o timeout"), false},
		{"postgres role missing", fmt.Errorf(`FATAL: role "postgres" does not exist (SQLSTATE 28000)`), true},
		{"mssql role ddl error", fmt.Errorf(`Cannot alter the role 'db_datareader', because it does not exist`), false},
		{"generic error", fmt.Errorf("something went wrong"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isAuthError(tt.err)
			if result != tt.expected {
				t.Errorf("isAuthError(%v) = %v, expected %v", tt.err, result, tt.expected)
			}
		})
	}
}

func TestCurrentOSUser(t *testing.T) {
	user := currentOSUser()
	if user == "" {
		t.Error("Expected non-empty OS username")
	}
}

func TestListDatabaseNames_SQLite(t *testing.T) {
	// Create a temp SQLite database with tables
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.sqlite3")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("Failed to open SQLite: %v", err)
	}

	_, err = db.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)")
	if err != nil {
		db.Close()
		t.Fatalf("Failed to create table: %v", err)
	}
	_, err = db.Exec("CREATE TABLE orders (id INTEGER PRIMARY KEY, total REAL)")
	if err != nil {
		db.Close()
		t.Fatalf("Failed to create table: %v", err)
	}

	names, err := listDatabaseNames(db, "sqlite")
	db.Close()

	if err != nil {
		t.Fatalf("listDatabaseNames failed: %v", err)
	}

	if len(names) < 2 {
		t.Fatalf("Expected at least 2 tables, got %d: %v", len(names), names)
	}

	found := make(map[string]bool)
	for _, n := range names {
		found[n] = true
	}
	if !found["users"] {
		t.Error("Expected 'users' table in results")
	}
	if !found["orders"] {
		t.Error("Expected 'orders' table in results")
	}
}

func TestProbeDatabase_SQLite(t *testing.T) {
	// Create a temp SQLite database with a table
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.sqlite3")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("Failed to open SQLite: %v", err)
	}
	_, err = db.Exec("CREATE TABLE products (id INTEGER PRIMARY KEY, name TEXT)")
	if err != nil {
		db.Close()
		t.Fatalf("Failed to create table: %v", err)
	}
	db.Close()

	// Run probeDatabase
	discovered := &DiscoveredDatabase{
		Type:          "sqlite",
		FilePath:      dbPath,
		Source:        "file",
		Status:        "found",
		ConfigSnippet: map[string]any{"type": "sqlite", "path": dbPath},
	}

	probeDatabase(discovered, "", "")

	if discovered.AuthStatus != "ok" {
		t.Errorf("Expected auth_status 'ok', got %q (error: %s)", discovered.AuthStatus, discovered.AuthError)
	}

	if len(discovered.Databases) == 0 {
		t.Fatal("Expected at least 1 table in Databases")
	}

	found := false
	for _, name := range discovered.Databases {
		if name == "products" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected 'products' in databases, got %v", discovered.Databases)
	}
}

func TestProbeDatabase_SkippedForUnknownType(t *testing.T) {
	discovered := &DiscoveredDatabase{
		Type:          "cockroachdb",
		Host:          "localhost",
		Port:          26257,
		Source:        "tcp",
		Status:        "listening",
		ConfigSnippet: map[string]any{"type": "cockroachdb"},
	}

	probeDatabase(discovered, "", "")

	if discovered.AuthStatus != "skipped" {
		t.Errorf("Expected auth_status 'skipped', got %q", discovered.AuthStatus)
	}
}

func TestProbeDatabase_SQLiteNoFilePath(t *testing.T) {
	discovered := &DiscoveredDatabase{
		Type:          "sqlite",
		Source:        "file",
		Status:        "found",
		ConfigSnippet: map[string]any{"type": "sqlite"},
	}

	probeDatabase(discovered, "", "")

	if discovered.AuthStatus != "error" {
		t.Errorf("Expected auth_status 'error' for missing file path, got %q", discovered.AuthStatus)
	}
}

func TestDiscoverResult_JSONWithProbeFields(t *testing.T) {
	result := DiscoverResult{
		Databases: []DiscoveredDatabase{
			{
				Type:       "postgres",
				Host:       "localhost",
				Port:       5432,
				Source:     "tcp",
				Status:     "listening",
				AuthStatus: "ok",
				AuthUser:   "postgres",
				Databases:  []string{"myapp", "myapp_test"},
				ConfigSnippet: map[string]any{
					"type": "postgres",
					"host": "localhost",
					"port": 5432,
					"user": "postgres",
				},
			},
		},
		Summary: DiscoverSummary{
			TotalFound:     1,
			DatabaseTypes:  []string{"postgres"},
			ScanDurationMs: 100,
		},
		DockerStatus: "available",
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	text := string(data)
	for _, expected := range []string{
		`"auth_status":"ok"`,
		`"auth_user":"postgres"`,
		`"databases":["myapp","myapp_test"]`,
	} {
		if !strings.Contains(text, expected) {
			t.Errorf("Expected JSON to contain %s, got: %s", expected, text)
		}
	}

	// Verify omitempty works - auth_error should not be present
	if strings.Contains(text, "auth_error") {
		t.Error("Expected auth_error to be omitted when empty")
	}
}
