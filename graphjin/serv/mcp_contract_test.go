package serv

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3"
	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/afero"
	"go.opentelemetry.io/otel"
	"go.uber.org/zap/zaptest"
	_ "modernc.org/sqlite"
)

func TestMCPToolSchemasMatchHandlerContracts(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{AllowWorkflowUpdates: true})
	ms.service.conf.Serv.Production = false
	ms.srv = server.NewMCPServer("test", "0.0.0")
	ms.registerTools()

	validateWhere := ms.srv.ListTools()["validate_where_clause"].Tool
	whereSchema, ok := validateWhere.InputSchema.Properties["where"].(map[string]any)
	if !ok {
		t.Fatalf("expected validate_where_clause.where schema to be an object map, got %T", validateWhere.InputSchema.Properties["where"])
	}
	if whereSchema["type"] != "object" {
		t.Fatalf("expected validate_where_clause.where type=object, got %v", whereSchema["type"])
	}

	saveWorkflow := ms.srv.ListTools()["save_workflow"].Tool
	tagsSchema, ok := saveWorkflow.InputSchema.Properties["tags"].(map[string]any)
	if !ok {
		t.Fatalf("expected save_workflow.tags schema to be an object map, got %T", saveWorkflow.InputSchema.Properties["tags"])
	}
	if tagsSchema["type"] != "array" {
		t.Fatalf("expected save_workflow.tags type=array, got %v", tagsSchema["type"])
	}
}

func TestHandleValidateWhereClause_AcceptsObjectAndLegacyJSONString(t *testing.T) {
	ms := newSQLiteReadyMCPServer(t, nil, nil)

	t.Run("object input", func(t *testing.T) {
		res, err := ms.handleValidateWhereClause(context.Background(), newToolRequest(map[string]any{
			"table": "users",
			"where": map[string]any{
				"price":  map[string]any{"gt": 50.0},
				"active": map[string]any{"eq": true},
			},
		}))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var out WhereValidationResult
		if err := json.Unmarshal([]byte(assertToolSuccess(t, res)), &out); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if !out.Valid {
			t.Fatalf("expected where clause to be valid, got errors: %+v", out.Errors)
		}
	})

	t.Run("legacy json string input", func(t *testing.T) {
		res, err := ms.handleValidateWhereClause(context.Background(), newToolRequest(map[string]any{
			"table": "users",
			"where": `{"active":{"eq":true}}`,
		}))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var out WhereValidationResult
		if err := json.Unmarshal([]byte(assertToolSuccess(t, res)), &out); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if !out.Valid {
			t.Fatalf("expected JSON string where clause to be valid, got errors: %+v", out.Errors)
		}
	})

	t.Run("invalid operator and malformed legacy json", func(t *testing.T) {
		res, err := ms.handleValidateWhereClause(context.Background(), newToolRequest(map[string]any{
			"table": "users",
			"where": map[string]any{
				"active": map[string]any{"gt": true},
			},
		}))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var out WhereValidationResult
		if err := json.Unmarshal([]byte(assertToolSuccess(t, res)), &out); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if out.Valid || len(out.Errors) == 0 || out.Errors[0].Error != "invalid_operator" {
			t.Fatalf("expected invalid_operator error, got %+v", out.Errors)
		}

		res, err = ms.handleValidateWhereClause(context.Background(), newToolRequest(map[string]any{
			"table": "users",
			"where": `{"active":`,
		}))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if err := json.Unmarshal([]byte(assertToolSuccess(t, res)), &out); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if out.Valid || len(out.Errors) == 0 || out.Errors[0].Error != "parse_error" {
			t.Fatalf("expected parse_error, got %+v", out.Errors)
		}
	})
}

func TestHandleGetSavedQuery_RespectsNamespace(t *testing.T) {
	ms := newSQLiteReadyMCPServer(t, map[string]string{
		"users_by_id":      "query GetUsersByID { users { id name } }",
		"shop.users_by_id": "query GetShopUsersByID { users { id name price } }",
	}, map[string]string{
		"users_by_id":      `{"id": 1}`,
		"shop.users_by_id": `{"id": 2}`,
	})

	t.Run("plain name", func(t *testing.T) {
		res, err := ms.handleGetSavedQuery(context.Background(), newToolRequest(map[string]any{
			"name": "users_by_id",
		}))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var out core.SavedQueryDetails
		if err := json.Unmarshal([]byte(assertToolSuccess(t, res)), &out); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if out.Namespace != "" {
			t.Fatalf("expected empty namespace, got %q", out.Namespace)
		}
		if !strings.Contains(out.Query, "GetUsersByID") {
			t.Fatalf("expected unqualified query, got %s", out.Query)
		}
	})

	t.Run("namespace + name", func(t *testing.T) {
		res, err := ms.handleGetSavedQuery(context.Background(), newToolRequest(map[string]any{
			"name":      "users_by_id",
			"namespace": "shop",
		}))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var out core.SavedQueryDetails
		if err := json.Unmarshal([]byte(assertToolSuccess(t, res)), &out); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if out.Namespace != "shop" {
			t.Fatalf("expected namespace shop, got %q", out.Namespace)
		}
		if !strings.Contains(out.Query, "GetShopUsersByID") {
			t.Fatalf("expected namespaced query, got %s", out.Query)
		}
	})

	t.Run("missing namespace entry", func(t *testing.T) {
		res, err := ms.handleGetSavedQuery(context.Background(), newToolRequest(map[string]any{
			"name":      "users_by_id",
			"namespace": "missing",
		}))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertToolError(t, res, "failed to get query")
	})
}

func TestHandleGetFragment_RespectsNamespace(t *testing.T) {
	ms := newSQLiteReadyMCPServer(t, nil, nil, map[string]string{
		"user_fields":      "fragment UserFields on users { id name }",
		"shop.user_fields": "fragment ShopUserFields on users { id name price }",
	})

	t.Run("plain name", func(t *testing.T) {
		res, err := ms.handleGetFragment(context.Background(), newToolRequest(map[string]any{
			"name": "user_fields",
		}))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var out struct {
			core.FragmentDetails
			ImportDirective string `json:"import_directive"`
		}
		if err := json.Unmarshal([]byte(assertToolSuccess(t, res)), &out); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if out.Namespace != "" {
			t.Fatalf("expected empty namespace, got %q", out.Namespace)
		}
		if out.ImportDirective != `#import "./fragments/user_fields"` {
			t.Fatalf("unexpected import directive: %s", out.ImportDirective)
		}
	})

	t.Run("namespace + name", func(t *testing.T) {
		res, err := ms.handleGetFragment(context.Background(), newToolRequest(map[string]any{
			"name":      "user_fields",
			"namespace": "shop",
		}))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var out struct {
			core.FragmentDetails
			ImportDirective string `json:"import_directive"`
		}
		if err := json.Unmarshal([]byte(assertToolSuccess(t, res)), &out); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if out.Namespace != "shop" {
			t.Fatalf("expected namespace shop, got %q", out.Namespace)
		}
		if out.ImportDirective != `#import "./fragments/shop.user_fields"` {
			t.Fatalf("unexpected import directive: %s", out.ImportDirective)
		}
	})

	t.Run("missing namespace entry", func(t *testing.T) {
		res, err := ms.handleGetFragment(context.Background(), newToolRequest(map[string]any{
			"name":      "user_fields",
			"namespace": "missing",
		}))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertToolError(t, res, "failed to get fragment")
	})
}

func TestNormalizeColumnType_DialectAwareBooleans(t *testing.T) {
	tests := []struct {
		dbType string
		want   string
	}{
		{dbType: "bool", want: "boolean"},
		{dbType: "boolean", want: "boolean"},
		{dbType: "tinyint(1)", want: "boolean"},
		{dbType: "BIT", want: "boolean"},
		{dbType: "number(1)", want: "boolean"},
		{dbType: "number(1,0)", want: "boolean"},
		{dbType: "numeric(7,2)", want: "numeric"},
		{dbType: "tinyint(4)", want: "numeric"},
	}

	for _, tt := range tests {
		if got := normalizeColumnType(tt.dbType); got != tt.want {
			t.Fatalf("normalizeColumnType(%q) = %q, want %q", tt.dbType, got, tt.want)
		}
	}
}

func TestHandleGetWorkflowGuide_UsesRegisteredToolSurface(t *testing.T) {
	t.Run("minimal surface omits gated flows", func(t *testing.T) {
		ms := mockMcpServerWithConfig(MCPConfig{
			AllowRawQueries: false,
			AllowMutations:  true,
		})
		ms.service.conf.Serv.Production = true
		ms.srv = server.NewMCPServer("test", "0.0.0")
		ms.registerTools()

		res, err := ms.handleGetWorkflowGuide(context.Background(), newToolRequest(map[string]any{}))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var out WorkflowGuide
		if err := json.Unmarshal([]byte(assertToolSuccess(t, res)), &out); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if containsAny(out.QueryWorkflow, "execute_graphql") {
			t.Fatalf("did not expect execute_graphql in minimal query workflow: %+v", out.QueryWorkflow)
		}
		if containsAny(mapValues(out.ToolSequences), "execute_graphql", "save_workflow") {
			t.Fatalf("did not expect gated tools in tool sequences: %+v", out.ToolSequences)
		}
		if _, ok := out.ToolSequences["js_workflow_authoring"]; ok {
			t.Fatal("did not expect js_workflow_authoring without save_workflow")
		}
		if _, ok := out.ToolSequences["saved_query_only"]; !ok {
			t.Fatal("expected saved_query_only sequence when raw queries are disabled")
		}
	})

	t.Run("full surface includes authoring flows", func(t *testing.T) {
		ms := mockMcpServerWithConfig(MCPConfig{
			AllowRawQueries:      true,
			AllowConfigUpdates:   true,
			AllowSchemaReload:    true,
			AllowWorkflowUpdates: true,
			AllowDevTools:        true,
		})
		ms.service.conf.Serv.Production = false
		ms.srv = server.NewMCPServer("test", "0.0.0")
		ms.registerTools()

		res, err := ms.handleGetWorkflowGuide(context.Background(), newToolRequest(map[string]any{}))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var out WorkflowGuide
		if err := json.Unmarshal([]byte(assertToolSuccess(t, res)), &out); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if _, ok := out.ToolSequences["js_workflow_authoring"]; !ok {
			t.Fatal("expected js_workflow_authoring when save_workflow is enabled")
		}
		if _, ok := out.ToolSequences["configure_resolver"]; !ok {
			t.Fatal("expected configure_resolver when config updates and reloads are enabled")
		}
		if !containsAny(out.Tips, "save_workflow") {
			t.Fatalf("expected save_workflow guidance in tips, got %+v", out.Tips)
		}
	})
}

func newSQLiteReadyMCPServer(t *testing.T, queries map[string]string, queryVars map[string]string, fragments ...map[string]string) *mcpServer {
	t.Helper()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.sqlite3")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
	})

	for _, stmt := range []string{
		`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, price NUMERIC, active BOOLEAN, created_at TEXT)`,
		`INSERT INTO users (id, name, price, active, created_at) VALUES (1, 'Ada', 75.5, 1, '2026-03-09T00:00:00Z')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("exec %q: %v", stmt, err)
		}
	}

	mem := afero.NewMemMapFs()
	fs := newAferoFS(mem, "/")

	for name, query := range queries {
		if err := fs.Put("/queries/"+name+".gql", []byte(query)); err != nil {
			t.Fatalf("write query %s: %v", name, err)
		}
	}
	for name, vars := range queryVars {
		if err := fs.Put("/queries/"+name+".json", []byte(vars)); err != nil {
			t.Fatalf("write query vars %s: %v", name, err)
		}
	}
	if len(fragments) > 0 {
		for name, fragment := range fragments[0] {
			if err := fs.Put("/queries/fragments/"+name+".gql", []byte(fragment)); err != nil {
				t.Fatalf("write fragment %s: %v", name, err)
			}
		}
	}

	conf := &Config{
		Core: core.Config{
			DBType:     "sqlite",
			Production: false,
		},
		Serv: Serv{Production: false},
	}

	svc := &graphjinService{
		conf:   conf,
		dbs:    map[string]*sql.DB{core.DefaultDBName: db},
		fs:     fs,
		log:    zaptest.NewLogger(t).Sugar(),
		tracer: otel.Tracer("graphjin-serv-test"),
	}

	gj, err := core.NewGraphJin(&conf.Core, db, core.OptionSetFS(fs), core.OptionSetDatabases(svc.dbs))
	if err != nil {
		t.Fatalf("init graphjin: %v", err)
	}
	t.Cleanup(func() {
		gj.Close()
	})

	svc.gj = gj
	return &mcpServer{service: svc, ctx: context.Background()}
}

func containsAny(items []string, fragments ...string) bool {
	for _, item := range items {
		for _, fragment := range fragments {
			if strings.Contains(item, fragment) {
				return true
			}
		}
	}
	return false
}

func mapValues(m map[string]string) []string {
	values := make([]string, 0, len(m))
	for _, value := range m {
		values = append(values, value)
	}
	return values
}
