package serv

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/serv/v3/internal/mcpcompat/server"
	"github.com/spf13/afero"
	"go.opentelemetry.io/otel"
	"go.uber.org/zap/zaptest"
	_ "modernc.org/sqlite"
)

func TestMCPToolSchemasMatchHandlerContracts(t *testing.T) {
	ms := mockLegacyMcpServerWithConfig(MCPConfig{AllowWorkflowUpdates: true, LegacyDiscovery: true})
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

	if _, exists := ms.srv.ListTools()["save_workflow"]; exists {
		t.Fatal("save_workflow should not be registered by MCP")
	}
}

func TestJSONToolResultsIncludeStructuredContent(t *testing.T) {
	ms := newSQLiteReadyMCPServer(t, map[string]string{
		"users_by_id": "query GetUsersByID { users { id name } }",
	}, map[string]string{
		"users_by_id": `{"id": 1}`,
	})

	res, err := ms.handleListSavedQueries(context.Background(), newToolRequest(map[string]any{}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	structured := assertToolStructuredMap(t, res)
	if got, ok := structured["count"].(float64); !ok || got != 1 {
		t.Fatalf("expected structured count=1, got %#v", structured["count"])
	}
	queries, ok := structured["queries"].([]any)
	if !ok {
		t.Fatalf("expected structured queries slice, got %T", structured["queries"])
	}
	if len(queries) != 1 {
		t.Fatalf("expected 1 structured query, got %d", len(queries))
	}
	next, ok := structured["next"].(map[string]any)
	if !ok {
		t.Fatalf("expected next guidance in structured response, got %T", structured["next"])
	}
	if next["recommended_tool"] != "execute_saved_query" {
		t.Fatalf("expected recommended_tool=execute_saved_query, got %#v", next["recommended_tool"])
	}
	options, ok := next["options"].([]any)
	if !ok || len(options) == 0 {
		t.Fatalf("expected next options, got %#v", next["options"])
	}
	firstOpt, ok := options[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first option to be an object, got %T", options[0])
	}
	argsTemplate, ok := firstOpt["args_template"].(map[string]any)
	if !ok {
		t.Fatalf("expected args_template on first next option, got %T", firstOpt["args_template"])
	}
	if argsTemplate["name"] != "<saved_query_name>" {
		t.Fatalf("expected next args_template to include name placeholder, got %#v", argsTemplate["name"])
	}

	var textBody map[string]any
	if err := json.Unmarshal([]byte(assertToolSuccess(t, res)), &textBody); err != nil {
		t.Fatalf("decode fallback text: %v", err)
	}
	if !reflect.DeepEqual(textBody, structured) {
		t.Fatalf("expected text fallback to match structured content\ntext: %#v\nstructured: %#v", textBody, structured)
	}
}

func TestGuidanceToolsReturnGuideAndNext(t *testing.T) {
	ms := newSQLiteReadyMCPServer(t, nil, nil)

	res, err := ms.handleWriteQueryTool(context.Background(), newToolRequest(map[string]any{
		"table":      "users",
		"fields":     "id, name",
		"pagination": "limit",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out struct {
		Title         string        `json:"title"`
		GuideMarkdown string        `json:"guide_markdown"`
		Next          *NextGuidance `json:"next"`
	}
	if err := json.Unmarshal([]byte(assertToolSuccess(t, res)), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Title == "" {
		t.Fatal("expected non-empty title")
	}
	if !strings.Contains(out.GuideMarkdown, "Query Guide for Table: users") {
		t.Fatalf("expected users query guide markdown, got %q", out.GuideMarkdown)
	}
	for _, want := range []string{"Analytics Directives", "@running", "@previous", "@rank"} {
		if !strings.Contains(out.GuideMarkdown, want) {
			t.Fatalf("expected write_query guide to include %q, got %q", want, out.GuideMarkdown)
		}
	}
	for _, stale := range []string{"@window", "lag_", "lead_", "partition:", "frame:"} {
		if strings.Contains(out.GuideMarkdown, stale) {
			t.Fatalf("write_query guide should not include stale analytics syntax %q: %q", stale, out.GuideMarkdown)
		}
	}
	if out.Next == nil {
		t.Fatal("expected next guidance")
	}
	if out.Next.RecommendedTool != "validate_where_clause" {
		t.Fatalf("expected recommended tool validate_where_clause, got %q", out.Next.RecommendedTool)
	}
	if len(out.Next.Options) == 0 || out.Next.Options[0].ArgsTemplate["table"] != "users" {
		t.Fatalf("expected carried table in args_template, got %+v", out.Next.Options)
	}

	res, err = ms.handleFixQueryErrorTool(context.Background(), newToolRequest(map[string]any{
		"query": "{ users { missing } }",
		"error": "column missing not found",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(assertToolSuccess(t, res), "Query Error Analysis") {
		t.Fatal("expected fix_query_error tool to return analysis markdown")
	}
}

func TestGetMutationSyntax_IncludesCodeSQLWorkflow(t *testing.T) {
	ms := newSQLiteReadyMCPServer(t, nil, nil)

	res, err := ms.handleGetMutationSyntax(context.Background(), newToolRequest(map[string]any{}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := assertToolStructuredMap(t, res)
	codesqlSection, ok := out["codesql"].(map[string]any)
	if !ok {
		t.Fatalf("expected codesql mutation guidance, got %#v", out["codesql"])
	}
	if !strings.Contains(codesqlSection["preview"].(string), "gj_code") {
		t.Fatalf("expected preview example in codesql section, got %#v", codesqlSection["preview"])
	}
	rules, ok := codesqlSection["rules"].([]any)
	if !ok || len(rules) == 0 {
		t.Fatalf("expected codesql rules, got %#v", codesqlSection["rules"])
	}
}

func TestWriteMutation_CodeSQLGuide(t *testing.T) {
	ms := newSQLiteReadyMCPServer(t, nil, nil)

	res, err := ms.handleWriteMutationTool(context.Background(), newToolRequest(map[string]any{
		"operation": "preview",
		"table":     "gj_code",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out struct {
		GuideMarkdown string `json:"guide_markdown"`
	}
	if err := json.Unmarshal([]byte(assertToolSuccess(t, res)), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	for _, want := range []string{"gj_code", "hash", `action: "preview"`, "old_text", "code_context"} {
		if !strings.Contains(out.GuideMarkdown, want) {
			t.Fatalf("expected CodeSQL guide to include %q, got %q", want, out.GuideMarkdown)
		}
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

func TestHandleValidateWhereClause_CompilerBackedValidation(t *testing.T) {
	ms := newSQLiteReadyMCPServer(t, nil, nil)

	t.Run("graphql literal string input", func(t *testing.T) {
		res, err := ms.handleValidateWhereClause(context.Background(), newToolRequest(map[string]any{
			"table":    "users",
			"database": core.DefaultDBName,
			"where":    `{ name: { eq: "Ada" } }`,
		}))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var out WhereValidationResult
		if err := json.Unmarshal([]byte(assertToolSuccess(t, res)), &out); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if !out.Valid {
			t.Fatalf("expected GraphQL literal where clause to be valid, got errors: %+v", out.Errors)
		}
		if out.ValidatedBy != "compiler" || out.ExampleQuery == "" {
			t.Fatalf("expected compiler validation metadata, got validated_by=%q example=%q", out.ValidatedBy, out.ExampleQuery)
		}
		if !strings.Contains(out.ExampleQuery, `@database(name: "default")`) {
			t.Fatalf("expected database directive in validation query, got %q", out.ExampleQuery)
		}
	})

	t.Run("nested relationship typo rejected by compiler", func(t *testing.T) {
		res, err := ms.handleValidateWhereClause(context.Background(), newToolRequest(map[string]any{
			"table": "users",
			"where": map[string]any{
				"profile": map[string]any{
					"emial": map[string]any{"eq": "admin@example.com"},
				},
			},
		}))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var out WhereValidationResult
		if err := json.Unmarshal([]byte(assertToolSuccess(t, res)), &out); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if out.Valid || len(out.CompilerErrors) == 0 {
			t.Fatalf("expected compiler rejection for nested typo, got valid=%v compiler=%v errors=%+v", out.Valid, out.CompilerErrors, out.Errors)
		}
	})

	t.Run("invalid logical shape rejected by compiler", func(t *testing.T) {
		res, err := ms.handleValidateWhereClause(context.Background(), newToolRequest(map[string]any{
			"table": "users",
			"where": map[string]any{
				"and": map[string]any{"id": map[string]any{"eq": 1.0}},
			},
		}))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var out WhereValidationResult
		if err := json.Unmarshal([]byte(assertToolSuccess(t, res)), &out); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if out.Valid || len(out.CompilerErrors) == 0 {
			t.Fatalf("expected compiler rejection for invalid logical shape, got valid=%v compiler=%v errors=%+v", out.Valid, out.CompilerErrors, out.Errors)
		}
	})

	t.Run("compiler supported operator alias", func(t *testing.T) {
		res, err := ms.handleValidateWhereClause(context.Background(), newToolRequest(map[string]any{
			"table": "users",
			"where": map[string]any{
				"id": map[string]any{"equals": 1.0},
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
			t.Fatalf("expected compiler-supported operator alias to validate, got compiler=%v errors=%+v", out.CompilerErrors, out.Errors)
		}
	})
}

func TestValidateQueryWhere_UsesCompilerBackedValidation(t *testing.T) {
	ms := newSQLiteReadyMCPServer(t, nil, nil)
	h := newControlPlaneGraphQL(ms.service)

	row, err := h.validateQueryWhere(core.ManagedMutationRoot{
		Input: map[string]interface{}{
			"table": "users",
			"where": map[string]any{"id": map[string]any{"equals": 1.0}},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if row["valid"] != true {
		t.Fatalf("expected alias where validation to be valid, got %#v", row)
	}

	row, err = h.validateQueryWhere(core.ManagedMutationRoot{
		Input: map[string]interface{}{
			"table": "users",
			"where": map[string]any{"profile": map[string]any{"emial": map[string]any{"eq": "x"}}},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if row["valid"] != false || !strings.Contains(row["errors_json"].(string), "compiler_error") {
		t.Fatalf("expected compiler-backed invalid control-plane result, got %#v", row)
	}
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
		ms := mockLegacyMcpServerWithConfig(MCPConfig{
			AllowRawQueries: false,
			AllowMutations:  true,
			disableExplicit: true,
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

	t.Run("expanded flags still omit removed authoring flows", func(t *testing.T) {
		ms := mockLegacyMcpServerWithConfig(MCPConfig{
			AllowRawQueries:        true,
			AllowConfigUpdates:     true,
			AllowSchemaReload:      true,
			AllowWorkflowUpdates:   true,
			AllowWorkflowExecution: true,
			AllowDevTools:          true,
			LegacyDiscovery:        true,
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
		if !containsAny(out.QueryWorkflow, "execute_graphql") {
			t.Fatalf("expected execute_graphql in query workflow when raw queries are enabled: %+v", out.QueryWorkflow)
		}
		for _, key := range []string{"configure_resolver", "js_workflow_authoring"} {
			if _, ok := out.ToolSequences[key]; ok {
				t.Fatalf("did not expect %s after MCP surface collapse: %+v", key, out.ToolSequences[key])
			}
		}
		if containsAny(mapValues(out.ToolSequences), "save_workflow", "execute_workflow") || containsAny(out.Tips, "save_workflow", "execute_workflow") {
			t.Fatalf("removed workflow MCP tools should not appear in guidance: sequences=%+v tips=%+v", out.ToolSequences, out.Tips)
		}
	})
}

// newSQLiteMCPServerWithSchema is a sibling of newSQLiteReadyMCPServer that
// accepts a custom schema/seed SQL slice. Used for multi-FK tests where the
// default users-only schema can't reproduce ambiguity.
func newSQLiteMCPServerWithSchema(t *testing.T, ddl []string) *mcpServer {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.sqlite3")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	for _, stmt := range ddl {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("exec %q: %v", stmt, err)
		}
	}

	mem := afero.NewMemMapFs()
	fs := newAferoFS(mem, "/")

	conf := &Config{
		Core: core.Config{DBType: "sqlite", Production: false},
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
	t.Cleanup(func() { gj.Close() })
	// Force synchronous schema discovery so subsequent ExplainQuery calls
	// see the tables immediately. NewGraphJin only kicks off async polling.
	if err := gj.Reload(); err != nil {
		t.Fatalf("reload schema: %v", err)
	}

	svc.gj = gj
	return &mcpServer{service: svc, ctx: context.Background()}
}

func TestGeneratePathExampleQuery_UsesActualPK(t *testing.T) {
	resolver := func(table string) string {
		switch table {
		case "salesorderdetail":
			return "salesorderdetailid"
		case "salesorderheader":
			return "salesorderid"
		case "customer":
			return "customerid"
		}
		return ""
	}

	got := generatePathExampleQuery("salesorderdetail",
		[]core.PathStep{{To: "salesorderheader"}, {To: "customer"}}, resolver)

	for _, want := range []string{"salesorderdetailid", "salesorderid", "customerid"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected real PK %q in example, got:\n%s", want, got)
		}
	}
	if strings.Contains(got, "<pk_column>") {
		t.Errorf("did not expect placeholder when resolver returns real PKs:\n%s", got)
	}
	if regexp.MustCompile(`(?:^|[\s{(\[,])(id|name)(?:[\s})\],]|$)`).MatchString(got) {
		t.Errorf("example must not contain literal 'id' or 'name' tokens:\n%s", got)
	}
}

func TestGeneratePathExampleQuery_FallsBackToPlaceholder(t *testing.T) {
	got := generatePathExampleQuery("foo",
		[]core.PathStep{{To: "bar"}}, func(string) string { return "" })
	if !strings.Contains(got, "<pk_column>") {
		t.Errorf("expected <pk_column> placeholder when resolver returns empty:\n%s", got)
	}
}

func TestValidateExampleQuery_CleanPath(t *testing.T) {
	ms := newSQLiteMCPServerWithSchema(t, []string{
		`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)`,
		`CREATE TABLE orders (id INTEGER PRIMARY KEY, customer_id INTEGER REFERENCES users(id), amount NUMERIC)`,
	})

	compiles, warning := ms.validateExampleQuery("{ orders { id users { id } } }")
	if !compiles {
		t.Fatalf("expected clean path to compile; got warning: %+v", warning)
	}
	if warning != nil {
		t.Fatalf("expected no warning on clean compile; got: %+v", warning)
	}
}

func TestValidateExampleQuery_AmbiguousFK(t *testing.T) {
	ms := newSQLiteMCPServerWithSchema(t, []string{
		`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)`,
		`CREATE TABLE orders (
			id INTEGER PRIMARY KEY,
			customer_id INTEGER REFERENCES users(id),
			salesperson_id INTEGER REFERENCES users(id),
			amount NUMERIC
		)`,
	})

	// Two FKs from orders to users — nesting users without @through(column:)
	// must produce an *AmbiguousPathError at compile time.
	compiles, warning := ms.validateExampleQuery("{ orders { id users { id } } }")
	if compiles {
		t.Fatal("expected ambiguous-FK example to fail compilation")
	}
	if warning == nil {
		t.Fatal("expected structured warning on failed compile")
	}
	if warning.Kind != fixKindMultiFKAmbiguity {
		t.Errorf("warning kind = %q; want %q (full warning: %+v)", warning.Kind, fixKindMultiFKAmbiguity, warning)
	}
	if !strings.Contains(warning.Diagnosis, "foreign keys") {
		t.Errorf("diagnosis missing 'foreign keys' marker: %q", warning.Diagnosis)
	}
	// Repaired query should suggest @through(column: ...) with one of the
	// candidate columns from the actual schema (customer_id or salesperson_id).
	if !strings.Contains(warning.RepairedQuery, "customer_id") &&
		!strings.Contains(warning.RepairedQuery, "salesperson_id") {
		t.Errorf("repaired query should name a candidate column; got: %s", warning.RepairedQuery)
	}
}

func TestHandleFindPath_CollapsedExample(t *testing.T) {
	ms := newSQLiteMCPServerWithSchema(t, []string{
		`CREATE TABLE category (catid INTEGER PRIMARY KEY, label TEXT)`,
		`CREATE TABLE product (pid INTEGER PRIMARY KEY, catid INTEGER REFERENCES category(catid), title TEXT)`,
		`CREATE TABLE orderitem (oiid INTEGER PRIMARY KEY, pid INTEGER REFERENCES product(pid), amt NUMERIC)`,
	})

	req := newToolRequest(map[string]any{
		"from_table": "category",
		"to_table":   "orderitem",
	})
	result, err := ms.handleFindPath(context.Background(), req)
	if err != nil {
		t.Fatalf("handleFindPath: %v", err)
	}
	out := assertToolStructuredMap(t, result)

	pathRaw, _ := out["path"].([]any)
	if len(pathRaw) < 2 {
		t.Fatalf("expected a 2+ hop path between category and orderitem, got %d hops: %+v", len(pathRaw), out)
	}

	collapsed, _ := out["collapsed_example_query"].(string)
	if collapsed == "" {
		t.Fatalf("expected collapsed_example_query when path has intermediates; got: %+v", out)
	}
	if !strings.Contains(collapsed, "category") || !strings.Contains(collapsed, "orderitem") {
		t.Errorf("collapsed query must nest category and orderitem directly; got: %s", collapsed)
	}
	if strings.Contains(collapsed, "product") {
		t.Errorf("collapsed query must NOT name the intermediate `product`; got: %s", collapsed)
	}

	compiles, _ := out["collapsed_example_query_compiles"].(bool)
	if !compiles {
		t.Errorf("collapsed example must compile via auto-traversal; got warning: %v", out["collapsed_example_query_warning"])
	}

	note, _ := out["collapsed_note"].(string)
	if note == "" {
		t.Errorf("collapsed_note must be populated when collapsed query is emitted")
	} else if !strings.Contains(stringToLower(note), "auto-travers") {
		t.Errorf("collapsed_note should mention auto-traversal; got: %q", note)
	}
}

func TestHandleFindPath_DirectRelationship(t *testing.T) {
	ms := newSQLiteMCPServerWithSchema(t, []string{
		`CREATE TABLE users (uid INTEGER PRIMARY KEY, label TEXT)`,
		`CREATE TABLE orders (oid INTEGER PRIMARY KEY, uid INTEGER REFERENCES users(uid), amt NUMERIC)`,
	})

	req := newToolRequest(map[string]any{
		"from_table": "users",
		"to_table":   "orders",
	})
	result, err := ms.handleFindPath(context.Background(), req)
	if err != nil {
		t.Fatalf("handleFindPath: %v", err)
	}
	out := assertToolStructuredMap(t, result)

	if collapsed, ok := out["collapsed_example_query"]; ok && collapsed != "" {
		t.Errorf("collapsed_example_query should be omitted for single-hop paths; got: %v", collapsed)
	}
	if note, ok := out["collapsed_note"]; ok && note != "" {
		t.Errorf("collapsed_note should be omitted for single-hop paths; got: %v", note)
	}
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

func TestBuildFixQueryErrorRepair_Arms(t *testing.T) {
	cases := []struct {
		name         string
		errorMsg     string
		query        string // optional; defaults to "query { foo { bar } }"
		wantKind     string
		wantInRepair []string
		wantTools    []string
	}{
		{
			name:         "multi_fk_ambiguity",
			errorMsg:     `ambiguous relationship orders -> users: multiple foreign keys (customer_id, salesperson_id). Disambiguate by adding @through(column: "customer_id") or @through(column: "salesperson_id") on the nested selection`,
			wantKind:     fixKindMultiFKAmbiguity,
			wantInRepair: []string{`@through(column: "customer_id")`, "candidates: customer_id, salesperson_id"},
			wantTools:    []string{"query_catalog", "get_catalog_card"},
		},
		{
			name:         "distinct_join_shape",
			errorMsg:     `nested selection 'salesorderheader' joins through parent column 'salesorderdetail.salesorderid', which is not in distinct: [productid]. The GROUP BY collapses 'salesorderid' away.`,
			wantKind:     fixKindDistinctJoinShape,
			wantInRepair: []string{"<dimension_table>", "salesorderdetail", "salesorderheader", "productid", "metric_by_dimension"},
			wantTools:    []string{"query_catalog", "get_catalog_card"},
		},
		{
			name:         "partition_filter_required",
			errorMsg:     `table "salesorderdetail" requires a filter on temporal column "modifieddate" (e.g., { modifieddate: { gt: "2026-01-01" } }); pass unrestricted: true to override`,
			wantKind:     fixKindPartitionFilter,
			wantInRepair: []string{"salesorderdetail", "modifieddate", "unrestricted: true"},
			wantTools:    []string{"query_catalog", "validate_where_clause"},
		},
		{
			name:      "unknown_relationship",
			errorMsg:  `relationship not found: foo -> bar`,
			wantKind:  fixKindUnknownRelationship,
			wantTools: []string{"query_catalog"},
		},
		{
			name:      "table_not_found",
			errorMsg:  `table not found: usrs`,
			wantKind:  fixKindTableNotFound,
			wantTools: []string{"query_catalog"},
		},
		{
			name:      "column_not_found_postgres",
			errorMsg:  `pq: column purchases_0.customer_id does not exist`,
			wantKind:  fixKindColumnNotFound,
			wantTools: []string{"query_catalog"},
		},
		{
			name:         "field_not_on_table",
			errorMsg:     `field 'id' is not a column or a function`,
			wantKind:     fixKindFieldNotOnTable,
			wantInRepair: []string{"<actual_pk_column>", "<actual_name_column>"},
			wantTools:    []string{"query_catalog", "get_catalog_card"},
		},
		{
			name:         "wrong_dialect_argument",
			errorMsg:     `unknown argument 'aggregation' on field 'orders'`,
			wantKind:     fixKindWrongDialect,
			wantInRepair: []string{"sum(expr:", "sum_<numeric_col>", "count_<pk_column>"},
			wantTools:    []string{"query_catalog", "get_catalog_card"},
		},
		{
			name:         "wrong_dialect_aggregate_suffix",
			errorMsg:     `table not found: orders_aggregate`,
			query:        `query { orders_aggregate { aggregate { count } } }`,
			wantKind:     fixKindWrongDialect,
			wantInRepair: []string{"orders", "sum(expr:", "_aggregate"},
			wantTools:    []string{"query_catalog"},
		},
		{
			name:     "generic_fallback",
			errorMsg: `something completely unexpected happened`,
			wantKind: fixKindGeneric,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			query := tc.query
			if query == "" {
				query = "query { foo { bar } }"
			}
			res := buildFixQueryErrorRepair(query, tc.errorMsg, false)
			if res.Kind != tc.wantKind {
				t.Fatalf("kind: got %q want %q", res.Kind, tc.wantKind)
			}
			for _, s := range tc.wantInRepair {
				if !strings.Contains(res.RepairedQuery, s) {
					t.Errorf("RepairedQuery missing %q. Got:\n%s", s, res.RepairedQuery)
				}
			}
			for _, s := range tc.wantTools {
				found := false
				for _, tool := range res.FollowUpTools {
					if strings.Contains(tool, s) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("FollowUpTools missing %q. Got: %v", s, res.FollowUpTools)
				}
			}
			if !strings.Contains(res.GuideMarkdown, "Query Error Analysis") {
				t.Errorf("GuideMarkdown missing header")
			}
			if !strings.Contains(res.GuideMarkdown, res.Diagnosis) {
				t.Errorf("GuideMarkdown missing diagnosis text")
			}
		})
	}
}

func TestClassifyExecError_AllDialects(t *testing.T) {
	cases := []struct {
		name     string
		dbType   string
		errMsg   string
		wantKind string
		wantTbl  string
		wantCol  string
	}{
		// postgres
		{"pg_column", "postgres", `pq: column salesorderdetail_0.salesorderid does not exist`, ExecKindColumnNotFound, "salesorderdetail", "salesorderid"},
		{"pg_table", "postgres", `pq: relation "users" does not exist`, ExecKindTableNotFound, "users", ""},
		{"pg_type", "postgres", `pq: invalid input syntax for type integer: "abc"`, ExecKindTypeMismatch, "", "integer"},
		{"pg_perm", "postgres", `pq: permission denied for relation "secrets"`, ExecKindPermission, "", "secrets"},
		// mysql
		{"my_column", "mysql", `Error 1054: Unknown column 'foo' in 'field list'`, ExecKindColumnNotFound, "", "foo"},
		{"my_table", "mysql", `Error 1146: Table 'shop.orders' doesn't exist`, ExecKindTableNotFound, "orders", ""},
		{"my_type", "mysql", `Error 1366: Incorrect integer value: 'abc' for column 'age' at row 1`, ExecKindTypeMismatch, "", "abc"},
		// mariadb (same patterns as mysql)
		{"maria_col", "mariadb", `Error 1054: Unknown column 'bar' in 'where clause'`, ExecKindColumnNotFound, "", "bar"},
		// sqlite
		{"sl_column", "sqlite", `no such column: orders.total`, ExecKindColumnNotFound, "orders", "total"},
		{"sl_table", "sqlite", `no such table: orders`, ExecKindTableNotFound, "orders", ""},
		{"sl_type", "sqlite", `datatype mismatch`, ExecKindTypeMismatch, "", ""},
		// oracle
		{"or_column", "oracle", `ORA-00904: "FOO": invalid identifier`, ExecKindColumnNotFound, "", "FOO"},
		{"or_table", "oracle", `ORA-00942: table or view does not exist`, ExecKindTableNotFound, "", ""},
		{"or_type", "oracle", `ORA-01722: invalid number`, ExecKindTypeMismatch, "", ""},
		// mssql
		{"ms_column", "mssql", `mssql: Invalid column name 'salesorderid'.`, ExecKindColumnNotFound, "", "salesorderid"},
		{"ms_table", "mssql", `mssql: Invalid object name 'orders'.`, ExecKindTableNotFound, "orders", ""},
		// snowflake
		{"sf_column", "snowflake", `000904 (42000): SQL compilation error: error line 1 at position 7\ninvalid identifier 'NOT_A_COL'`, ExecKindColumnNotFound, "", "NOT_A_COL"},
		{"sf_table", "snowflake", `Object 'PUBLIC.NOPE' does not exist or not authorized.`, ExecKindTableNotFound, "PUBLIC.NOPE", ""},
		// mongodb
		{"mg_field", "mongodb", `field path 'foo.bar' is invalid`, ExecKindColumnNotFound, "foo", "bar"},
		{"mg_ns", "mongodb", `ns not found`, ExecKindTableNotFound, "", ""},
		// unknown / aliases
		{"empty_dbtype_pg_alias", "", `pq: column "x" does not exist`, ExecKindColumnNotFound, "", "x"},
		{"sqlserver_alias", "sqlserver", `mssql: Invalid column name 'y'.`, ExecKindColumnNotFound, "", "y"},
		{"unknown_dialect", "duckdb", `something went wrong`, ExecKindUnknown, "", ""},
		{"empty_msg", "postgres", ``, ExecKindUnknown, "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyExecError(tc.dbType, tc.errMsg)
			if got.Kind != tc.wantKind {
				t.Fatalf("kind: got %q want %q (msg=%q)", got.Kind, tc.wantKind, tc.errMsg)
			}
			if !strings.EqualFold(got.Table, tc.wantTbl) {
				t.Errorf("table: got %q want %q", got.Table, tc.wantTbl)
			}
			if !strings.EqualFold(got.Column, tc.wantCol) {
				t.Errorf("column: got %q want %q", got.Column, tc.wantCol)
			}
		})
	}
}

func TestAggregationLimitations(t *testing.T) {
	limits := aggregationLimitations()
	if len(limits) == 0 {
		t.Fatal("expected non-empty aggregation limitations")
	}
	// Each limitation must be a complete sentence pointing at a remedy
	// or a tool — agents read these and need actionable text.
	for i, l := range limits {
		if !strings.HasSuffix(l, ".") {
			t.Errorf("limitation[%d] should end with a period: %q", i, l)
		}
	}
	// The most load-bearing one (matches our Stage 3 compile error)
	// must explicitly reference the metric_by_dimension pattern, since
	// that's the canonical fix.
	joined := strings.Join(limits, "\n")
	for _, want := range []string{
		"order_by",
		"distinct",
		"metric_by_dimension",
		"MongoDB",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("limitations should mention %q; got:\n%s", want, joined)
		}
	}
}

func TestGenerateAggregations_UsageReferencesPatterns(t *testing.T) {
	// Minimal in-memory schema with a numeric column so the aggregation
	// generator emits sum_/avg_ entries.
	schema := &core.TableSchema{
		Name: "purchases",
		Columns: []core.ColumnInfo{
			{Name: "id", Type: "bigint", PrimaryKey: true},
			{Name: "amount", Type: "numeric"},
		},
	}
	agg := generateAggregations(schema)

	if !strings.Contains(agg.Usage, "query_pattern") {
		t.Errorf("Usage should reference query_pattern catalog rows; got: %q", agg.Usage)
	}
	if !strings.Contains(agg.Usage, "graphjin_repair") {
		t.Errorf("Usage should reference graphjin_repair; got: %q", agg.Usage)
	}
	if len(agg.Limitations) == 0 {
		t.Errorf("Limitations should be populated alongside Usage")
	}
}

func TestCanonicalQueryPatterns(t *testing.T) {
	patterns := canonicalQueryPatterns()
	if len(patterns) != 3 {
		t.Fatalf("expected 3 canonical patterns, got %d", len(patterns))
	}

	// Stable order: metric_by_dimension first (most common authoring
	// mistake; should lead).
	wantOrder := []string{"metric_by_dimension", "time_series", "top_n"}
	names := make([]string, len(patterns))
	for i, p := range patterns {
		names[i] = p.Name
	}
	for i, want := range wantOrder {
		if names[i] != want {
			t.Errorf("patterns[%d].Name = %q; want %q (full order: %v)", i, names[i], want, names)
		}
	}

	for _, p := range patterns {
		if p.Title == "" || p.Rule == "" || p.Why == "" || p.RightExample == "" {
			t.Errorf("pattern %q missing required field: %+v", p.Name, p)
		}
	}

	// metric_by_dimension MUST carry the wrong/right contrast — the
	// agent feedback (P3) flagged this as load-bearing for small models.
	mbd := patterns[0]
	if mbd.WrongExample == "" || mbd.WrongReason == "" {
		t.Errorf("metric_by_dimension must include WrongExample and WrongReason (load-bearing per P3)")
	}
	if mbd.AutoTraversalNote == "" {
		t.Errorf("metric_by_dimension must include AutoTraversalNote so agents learn the collapsed shape")
	} else {
		for _, marker := range []string{"auto-travers", "find_path"} {
			if !strings.Contains(stringToLower(mbd.AutoTraversalNote), stringToLower(marker)) {
				t.Errorf("metric_by_dimension.AutoTraversalNote should mention %q; got: %q", marker, mbd.AutoTraversalNote)
			}
		}
	}

	// Patterns must use placeholder column names like <pk_column>,
	// <name_column>, <date_column>. Literal 'id' / 'name' tokens
	// inside example queries would tempt small models to copy them
	// verbatim onto tables whose PKs are named differently
	// (e.g. productcategoryid in AdventureWorks). Allow 'id' / 'name'
	// as substrings of placeholders — only fail when they appear as
	// bare identifiers between whitespace/braces.
	bareIDName := regexp.MustCompile(`(?:^|[\s{(\[,])(id|name)(?:[\s})\],]|$)`)
	for _, p := range patterns {
		for label, ex := range map[string]string{"RightExample": p.RightExample, "WrongExample": p.WrongExample} {
			if ex == "" {
				continue
			}
			if bareIDName.MatchString(ex) {
				t.Errorf("pattern %q.%s contains a bare 'id' or 'name' literal — use a <pk_column> / <name_column> placeholder so small models don't copy it verbatim:\n%s", p.Name, label, ex)
			}
		}
	}
}

func TestStripAliasSuffix(t *testing.T) {
	cases := map[string]string{
		"":                    "",
		"orders":              "orders",
		"orders_0":            "orders",
		"salesorderdetail_42": "salesorderdetail",
		"order_items":         "order_items",      // _items isn't numeric — preserved
		"orders_":             "orders_",          // trailing underscore — preserved
		"my_table_5_extra":    "my_table_5_extra", // not a trailing numeric suffix
		"my_table_5":          "my_table",
	}
	for in, want := range cases {
		if got := stripAliasSuffix(in); got != want {
			t.Errorf("stripAliasSuffix(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestBuildFixQueryErrorRepair_AnalyticsModeBlock(t *testing.T) {
	res := buildFixQueryErrorRepair("query { x }", "table not found: x", true)
	if !strings.Contains(res.GuideMarkdown, "Analytics Mode Rules") {
		t.Fatalf("expected analytics-mode block when on; got:\n%s", res.GuideMarkdown)
	}
	res = buildFixQueryErrorRepair("query { x }", "table not found: x", false)
	if strings.Contains(res.GuideMarkdown, "Analytics Mode Rules") {
		t.Fatal("did not expect analytics-mode block when off")
	}
}

func TestDetectFKDisambiguation(t *testing.T) {
	out := detectFKDisambiguation([]RelationshipRef{
		{Table: "users", Column: "user_id"},
		{Table: "products", Column: "product_id"},
	})
	if len(out) != 0 {
		t.Fatalf("expected empty disambiguation, got %+v", out)
	}

	out = detectFKDisambiguation([]RelationshipRef{
		{Table: "users", Column: "customer_id"},
		{Table: "users", Column: "salesperson_id"},
		{Table: "products", Column: "product_id"},
	})
	if len(out) != 1 {
		t.Fatalf("expected 1 disambiguation entry, got %d", len(out))
	}
	entry := out[0]
	if entry.Target != "users" {
		t.Fatalf("expected target=users, got %s", entry.Target)
	}
	if !entry.Ambiguous {
		t.Fatal("expected Ambiguous=true")
	}
	if len(entry.Candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(entry.Candidates))
	}
	cols := entry.Candidates[0].Column + "," + entry.Candidates[1].Column
	if !strings.Contains(cols, "customer_id") || !strings.Contains(cols, "salesperson_id") {
		t.Fatalf("expected customer_id + salesperson_id, got %s", cols)
	}
	for _, c := range entry.Candidates {
		if !strings.Contains(c.Snippet, "@through(column:") {
			t.Fatalf("snippet missing @through(column:): %s", c.Snippet)
		}
	}
}
