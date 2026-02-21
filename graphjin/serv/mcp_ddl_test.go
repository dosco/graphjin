package serv

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3"
	"github.com/mark3labs/mcp-go/mcp"
)

// mockDDLServer creates an mcpServer with DDL-relevant config.
// databases maps database names to their DatabaseConfig entries;
// dbs maps database names to *sql.DB connections.
func mockDDLServer(databases map[string]core.DatabaseConfig, dbs map[string]*sql.DB) *mcpServer {
	conf := &Config{
		Core: core.Config{
			Databases: databases,
		},
		Serv: Serv{
			MCP: MCPConfig{AllowSchemaUpdates: true},
		},
	}
	svc := &graphjinService{
		conf: conf,
		dbs:  dbs,
	}
	// Snapshot read-only databases (mirrors production init logic)
	readOnlyDBs := make(map[string]bool)
	for name, dbConf := range databases {
		if dbConf.ReadOnly {
			readOnlyDBs[name] = true
		}
	}
	return &mcpServer{service: svc, ctx: context.Background(), readOnlyDBs: readOnlyDBs}
}

// =============================================================================
// Handler validation tests
// =============================================================================

func TestHandlePreviewSchemaChanges_MissingDatabase(t *testing.T) {
	ms := mockDDLServer(nil, nil)
	ctx := context.Background()

	req := newToolRequest(map[string]any{
		"schema": "type products { id: BigInt! @id\n name: Text! }",
		// database omitted
	})

	result, err := ms.handlePreviewSchemaChanges(ctx, req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	assertToolError(t, result, "database is required")
}

func TestHandlePreviewSchemaChanges_MissingSchema(t *testing.T) {
	ms := mockDDLServer(nil, nil)
	ctx := context.Background()

	req := newToolRequest(map[string]any{
		"database": "mydb",
		// schema omitted
	})

	result, err := ms.handlePreviewSchemaChanges(ctx, req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	assertToolError(t, result, "schema is required")
}

func TestHandlePreviewSchemaChanges_UnknownDatabase(t *testing.T) {
	ms := mockDDLServer(nil, nil) // no dbs configured
	ctx := context.Background()

	req := newToolRequest(map[string]any{
		"schema":   "type products { id: BigInt! @id }",
		"database": "nonexistent",
	})

	result, err := ms.handlePreviewSchemaChanges(ctx, req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	assertToolError(t, result, "not found")
}

func TestHandleApplySchemaChanges_MissingDatabase(t *testing.T) {
	ms := mockDDLServer(nil, nil)
	ctx := context.Background()

	req := newToolRequest(map[string]any{
		"schema": "type products { id: BigInt! @id }",
	})

	result, err := ms.handleApplySchemaChanges(ctx, req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	assertToolError(t, result, "database is required")
}

func TestHandleApplySchemaChanges_MissingSchema(t *testing.T) {
	ms := mockDDLServer(nil, nil)
	ctx := context.Background()

	req := newToolRequest(map[string]any{
		"database": "mydb",
	})

	result, err := ms.handleApplySchemaChanges(ctx, req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	assertToolError(t, result, "schema is required")
}

func TestHandleApplySchemaChanges_UnknownDatabase(t *testing.T) {
	ms := mockDDLServer(nil, nil)
	ctx := context.Background()

	req := newToolRequest(map[string]any{
		"schema":   "type products { id: BigInt! @id }",
		"database": "nonexistent",
	})

	result, err := ms.handleApplySchemaChanges(ctx, req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	assertToolError(t, result, "not found")
}

// =============================================================================
// getDBByName tests
// =============================================================================

func TestGetDBByName_EmptyFallsBack(t *testing.T) {
	// When database is "", getDBByName should return anyDB()
	ms := mockDDLServer(nil, nil)
	db := ms.getDBByName("")
	if db != nil {
		t.Error("Expected nil when no dbs configured and database is empty")
	}
}

func TestGetDBByName_NotFoundFallsBack(t *testing.T) {
	ms := mockDDLServer(nil, nil)
	db := ms.getDBByName("missing")
	// Falls back to anyDB() which is nil when dbs map is empty
	if db != nil {
		t.Error("Expected nil when database name not found and no dbs configured")
	}
}

// =============================================================================
// prepareSchema tests
// =============================================================================

func TestPrepareSchema_UsesNamedDatabase(t *testing.T) {
	databases := map[string]core.DatabaseConfig{
		"orders": {Type: "mysql", Schema: "orders_schema"},
	}
	ms := mockDDLServer(databases, nil)

	schema := "type products { id: BigInt! @id }"
	result := string(ms.prepareSchema(schema, "orders"))

	if !strings.Contains(result, "# dbinfo:mysql,,orders_schema") {
		t.Errorf("Expected header with mysql/orders_schema, got:\n%s", result)
	}
}

func TestPrepareSchema_FallsBackToConfDB(t *testing.T) {
	ms := mockDDLServer(nil, nil)
	ms.service.conf.DB.Type = "postgres"
	ms.service.conf.DB.Schema = "public"

	schema := "type products { id: BigInt! @id }"
	result := string(ms.prepareSchema(schema, "unknown"))

	if !strings.Contains(result, "# dbinfo:postgres,,public") {
		t.Errorf("Expected fallback to conf.DB, got:\n%s", result)
	}
}

func TestPrepareSchema_PreservesExistingHeader(t *testing.T) {
	ms := mockDDLServer(nil, nil)

	schema := "# dbinfo:sqlite,,main\n\ntype products { id: BigInt! @id }"
	result := string(ms.prepareSchema(schema, "anything"))

	if result != schema {
		t.Errorf("Expected schema unchanged when header present, got:\n%s", result)
	}
}

func TestPrepareSchema_DefaultSchemaForType(t *testing.T) {
	tests := []struct {
		dbType         string
		expectedSchema string
	}{
		{"postgres", "public"},
		{"postgresql", "public"},
		{"mysql", "db"},
		{"mariadb", "db"},
		{"sqlite", "main"},
		{"mssql", "dbo"},
		{"unknown", "public"},
	}

	for _, tt := range tests {
		t.Run(tt.dbType, func(t *testing.T) {
			databases := map[string]core.DatabaseConfig{
				"testdb": {Type: tt.dbType},
			}
			ms := mockDDLServer(databases, nil)

			result := string(ms.prepareSchema("type t { id: BigInt! @id }", "testdb"))
			expected := "# dbinfo:" + tt.dbType + ",," + tt.expectedSchema
			if !strings.Contains(result, expected) {
				t.Errorf("Expected %q in header, got:\n%s", expected, result)
			}
		})
	}
}

// =============================================================================
// Read-only database tests
// =============================================================================

func TestHandleApplySchemaChanges_ReadOnlyDB(t *testing.T) {
	databases := map[string]core.DatabaseConfig{
		"prod_replica": {Type: "postgres", ReadOnly: true},
	}
	ms := mockDDLServer(databases, nil)
	ctx := context.Background()

	req := newToolRequest(map[string]any{
		"schema":   "type products { id: BigInt! @id\n name: Text! }",
		"database": "prod_replica",
	})

	result, err := ms.handleApplySchemaChanges(ctx, req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	assertToolError(t, result, "read-only")
}

func TestHandleApplySchemaChanges_NonReadOnlyDB(t *testing.T) {
	// Non-read-only DB should NOT be blocked by read-only check
	// (it will fail at a later stage since there's no real DB connection,
	// but should NOT return "read-only" error)
	databases := map[string]core.DatabaseConfig{
		"writable": {Type: "postgres", ReadOnly: false},
	}
	ms := mockDDLServer(databases, nil)
	ctx := context.Background()

	req := newToolRequest(map[string]any{
		"schema":   "type products { id: BigInt! @id\n name: Text! }",
		"database": "writable",
	})

	result, err := ms.handleApplySchemaChanges(ctx, req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	// Should NOT contain read-only error (may contain other errors like "not found")
	if result.IsError {
		if len(result.Content) > 0 {
			if tc, ok := result.Content[0].(mcp.TextContent); ok {
				if strings.Contains(tc.Text, "read-only") {
					t.Errorf("Non-read-only DB should not be blocked: %s", tc.Text)
				}
			}
		}
	}
}

func TestIsDBReadOnly(t *testing.T) {
	databases := map[string]core.DatabaseConfig{
		"readonly_db": {Type: "postgres", ReadOnly: true},
		"normal_db":   {Type: "postgres", ReadOnly: false},
	}
	ms := mockDDLServer(databases, nil)

	if !ms.isDBReadOnly("readonly_db") {
		t.Error("Expected readonly_db to be read-only")
	}
	if ms.isDBReadOnly("normal_db") {
		t.Error("Expected normal_db to NOT be read-only")
	}
	if ms.isDBReadOnly("nonexistent") {
		t.Error("Expected nonexistent DB to NOT be read-only")
	}
}
