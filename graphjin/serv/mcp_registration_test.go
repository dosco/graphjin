package serv

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func toolNamesFromServer(tools map[string]*server.ServerTool) []string {
	names := make([]string, 0, len(tools))
	for name := range tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func resourceURIsFromServer(t *testing.T, srv *server.MCPServer) map[string]bool {
	t.Helper()

	c, err := client.NewInProcessClient(srv)
	if err != nil {
		t.Fatalf("new in-process client: %v", err)
	}
	defer c.Close()

	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("start in-process client: %v", err)
	}

	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{Name: "test-client", Version: "1.0.0"}
	if _, err := c.Initialize(context.Background(), initReq); err != nil {
		t.Fatalf("initialize in-process client: %v", err)
	}

	result, err := c.ListResources(context.Background(), mcp.ListResourcesRequest{})
	if err != nil {
		t.Fatalf("list resources: %v", err)
	}

	uris := make(map[string]bool, len(result.Resources))
	for _, r := range result.Resources {
		uris[r.URI] = true
	}
	return uris
}

func promptNamesFromServer(t *testing.T, srv *server.MCPServer) map[string]bool {
	t.Helper()

	c, err := client.NewInProcessClient(srv)
	if err != nil {
		t.Fatalf("new in-process client: %v", err)
	}
	defer c.Close()

	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("start in-process client: %v", err)
	}

	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{Name: "test-client", Version: "1.0.0"}
	if _, err := c.Initialize(context.Background(), initReq); err != nil {
		t.Fatalf("initialize in-process client: %v", err)
	}

	result, err := c.ListPrompts(context.Background(), mcp.ListPromptsRequest{})
	if err != nil {
		t.Fatalf("list prompts: %v", err)
	}

	names := make(map[string]bool, len(result.Prompts))
	for _, prompt := range result.Prompts {
		names[prompt.Name] = true
	}
	return names
}

func TestRegisterConfigTools_GetCurrentConfigDevOnly(t *testing.T) {
	t.Run("registered in development mode", func(t *testing.T) {
		ms := mockMcpServerWithConfig(MCPConfig{})
		ms.service.conf.Serv.Production = false
		ms.srv = server.NewMCPServer("test", "0.0.0")
		ms.registerConfigTools()

		if _, exists := ms.srv.ListTools()["get_current_config"]; !exists {
			t.Fatal("get_current_config should be registered in development mode")
		}
	})

	t.Run("not registered in production mode", func(t *testing.T) {
		ms := mockMcpServerWithConfig(MCPConfig{})
		ms.service.conf.Serv.Production = true
		ms.srv = server.NewMCPServer("test", "0.0.0")
		ms.registerConfigTools()

		if _, exists := ms.srv.ListTools()["get_current_config"]; exists {
			t.Fatal("get_current_config should not be registered in production mode")
		}
	})
}

func TestRegisterTools_QuickSetupNotRegistered(t *testing.T) {
	ms := mockLegacyMcpServerWithConfig(MCPConfig{
		AllowRawQueries:    true,
		AllowConfigUpdates: true,
		AllowSchemaReload:  true,
		AllowSchemaUpdates: true,
		AllowDevTools:      true,
	})
	ms.service.conf.Serv.Production = false
	ms.srv = server.NewMCPServer("test", "0.0.0")
	ms.registerTools()

	tools := ms.srv.ListTools()
	if _, exists := tools["quick_setup"]; exists {
		t.Fatal("quick_setup should not be registered")
	}
	if _, exists := tools["apply_database_setup"]; !exists {
		t.Fatal("apply_database_setup should still be registered")
	}
}

func TestRegisterTools_MCPDisabledRegistersNoTools(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{Disable: true})
	ms.service.conf.Serv.Production = false
	ms.srv = server.NewMCPServer("test", "0.0.0")
	ms.registerTools()

	if tools := ms.srv.ListTools(); len(tools) != 0 {
		t.Fatalf("expected no tools when MCP is disabled, got %v", toolNamesFromServer(tools))
	}
	if listed := mcpToolList(ms.service.conf); len(listed) != 0 {
		t.Fatalf("expected mcpToolList to be empty when MCP is disabled, got %v", listed)
	}
}

func TestMCPDisabledRegistersNoPromptsOrResources(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{Disable: true})
	ms.srv = server.NewMCPServer("test", "0.0.0", server.WithPromptCapabilities(true), server.WithResourceCapabilities(true, false))
	ms.registerPrompts()
	ms.registerResources()

	if prompts := promptNamesFromServer(t, ms.srv); len(prompts) != 0 {
		t.Fatalf("expected no prompts when MCP is disabled, got %v", prompts)
	}
	if resources := resourceURIsFromServer(t, ms.srv); len(resources) != 0 {
		t.Fatalf("expected no resources when MCP is disabled, got %v", resources)
	}
}

func TestLegacyProductionMCPDisabledByDefault(t *testing.T) {
	conf := &Config{Serv: Serv{Production: true}}
	if listed := mcpToolList(conf); len(listed) != 0 {
		t.Fatalf("expected legacy production MCP tool list to be empty by default, got %v", listed)
	}

	ms := mockLegacyMcpServerWithConfig(MCPConfig{})
	ms.service.conf.Serv.Production = true
	ms.srv = server.NewMCPServer("test", "0.0.0")
	ms.registerTools()

	if tools := ms.srv.ListTools(); len(tools) != 0 {
		t.Fatalf("expected legacy production to register no tools by default, got %v", toolNamesFromServer(tools))
	}
}

func TestLegacyProductionMCPExplicitEnable(t *testing.T) {
	conf := &Config{
		Serv: Serv{
			Production: true,
			MCP:        MCPConfig{disableExplicit: true},
		},
	}
	if listed := mcpToolList(conf); len(listed) == 0 {
		t.Fatal("expected explicit mcp.disable=false to enable legacy production MCP tools")
	}
}

func TestLegacyAgenticMCPEnabledByDefault(t *testing.T) {
	conf := &Config{
		Core: core.Config{Mode: "agentic"},
		Serv: Serv{Production: true},
	}
	if listed := mcpToolList(conf); len(listed) == 0 {
		t.Fatal("expected legacy agentic MCP tools to be enabled by default")
	}
}

func TestSourcesUsedRegistersNoDefaultPromptsOrResources(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{})
	ms.srv = server.NewMCPServer("test", "0.0.0", server.WithPromptCapabilities(true), server.WithResourceCapabilities(true, false))
	ms.registerPrompts()
	ms.registerResources()

	if prompts := promptNamesFromServer(t, ms.srv); len(prompts) != 0 {
		t.Fatalf("expected no prompts in sources-used mode, got %v", prompts)
	}
	if resources := resourceURIsFromServer(t, ms.srv); len(resources) != 0 {
		t.Fatalf("expected no resources in sources-used mode, got %v", resources)
	}
}

func TestMCPServerInstructions_Disabled(t *testing.T) {
	text := mcpServerInstructions(&Config{Serv: Serv{MCP: MCPConfig{Disable: true}}})
	if !strings.Contains(text, "GraphJin MCP is disabled by configuration") {
		t.Fatalf("expected disabled instructions, got:\n%s", text)
	}
	for _, forbidden := range []string{
		"query_catalog",
		"list_tables",
		"execute_workflow",
		"get_catalog_card",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("disabled instructions should not recommend %q:\n%s", forbidden, text)
		}
	}
}

func TestRegisterTools_CatalogDefaultHidesLegacyDiscovery(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{})
	ms.service.conf.Serv.Production = false
	ms.srv = server.NewMCPServer("test", "0.0.0")
	ms.registerTools()

	tools := ms.srv.ListTools()
	if listed := mcpToolList(ms.service.conf); strings.Join(listed, ",") != "graphql_help,query_catalog,execute_saved_query,validate_where_clause" {
		t.Fatalf("unexpected sources-used mcpToolList: %v", listed)
	}
	for _, name := range []string{"graphql_help", "query_catalog", "execute_saved_query", "validate_where_clause"} {
		if _, exists := tools[name]; !exists {
			t.Fatalf("%s should be registered by default", name)
		}
	}
	if len(tools) != 4 {
		t.Fatalf("sources-used default tools should be exactly 4, got %v", toolNamesFromServer(tools))
	}
	for _, name := range []string{"get_catalog_card", "get_catalog_entrypoints", "get_catalog_capabilities", "list_tables", "describe_table", "find_path", "get_table_sample", "get_query_syntax", "get_mutation_syntax", "get_workflow_guide", "list_workflows", "write_query", "write_mutation", "fix_query_error", "get_config_docs"} {
		if _, exists := tools[name]; exists {
			t.Fatalf("%s should be hidden in sources-used default MCP registration", name)
		}
	}
}

func TestRegisterTools_SourcesUsedIgnoresLegacyDiscoveryFlag(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{LegacyDiscovery: true, AllowRawQueries: true, AllowWorkflowExecution: true})
	ms.service.conf.Serv.Production = false
	ms.srv = server.NewMCPServer("test", "0.0.0")
	ms.registerTools()

	tools := ms.srv.ListTools()
	if listed := mcpToolList(ms.service.conf); strings.Join(listed, ",") != "graphql_help,query_catalog,execute_saved_query,validate_where_clause" {
		t.Fatalf("unexpected sources-used mcpToolList with legacy discovery: %v", listed)
	}
	if len(tools) != 4 {
		t.Fatalf("sources-used tools should ignore legacy discovery and raw-query gates, got %v", toolNamesFromServer(tools))
	}
	for _, name := range []string{"get_query_syntax", "list_tables", "execute_workflow", "execute_graphql"} {
		if _, exists := tools[name]; exists {
			t.Fatalf("%s should remain hidden in sources-used mode", name)
		}
	}
}

func TestRegisterTools_SourcesUsedWithoutCatalogDoesNotAdvertiseQueryCatalog(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{})
	ms.service.conf.Core.Sources = []core.SourceConfig{{Name: "app", Kind: "database"}}
	ms.srv = server.NewMCPServer("test", "0.0.0")
	ms.registerTools()

	tools := ms.srv.ListTools()
	if listed := strings.Join(mcpToolList(ms.service.conf), ","); listed != "execute_saved_query,validate_where_clause" {
		t.Fatalf("unexpected sources-used tool list without catalog: %s", listed)
	}
	if _, exists := tools["query_catalog"]; exists {
		t.Fatal("query_catalog should not register without the graphjin catalog source")
	}
	if _, exists := tools["graphql_help"]; exists {
		t.Fatal("graphql_help should not register without the graphjin catalog source")
	}
	for _, name := range []string{"execute_saved_query", "validate_where_clause"} {
		if _, exists := tools[name]; !exists {
			t.Fatalf("%s should still register in sources-used mode without catalog", name)
		}
	}
}

func TestRegisterTools_LegacyDiscoveryOptIn(t *testing.T) {
	ms := mockLegacyMcpServerWithConfig(MCPConfig{LegacyDiscovery: true})
	ms.service.conf.Serv.Production = false
	ms.srv = server.NewMCPServer("test", "0.0.0")
	ms.registerTools()

	tools := ms.srv.ListTools()
	for _, name := range []string{"list_tables", "describe_table", "find_path", "get_query_syntax", "get_workflow_guide", "list_workflows"} {
		if _, exists := tools[name]; !exists {
			t.Fatalf("%s should be registered when legacy discovery is enabled", name)
		}
	}
}

func TestRegisterTools_LegacyExecuteWorkflowRequiresGate(t *testing.T) {
	t.Run("hidden without execution gate", func(t *testing.T) {
		ms := mockLegacyMcpServerWithConfig(MCPConfig{LegacyDiscovery: true})
		ms.srv = server.NewMCPServer("test", "0.0.0")
		ms.registerTools()

		if _, exists := ms.srv.ListTools()["execute_workflow"]; exists {
			t.Fatal("execute_workflow should require mcp.allow_workflow_execution")
		}
	})

	t.Run("registered when legacy discovery and execution gate are enabled", func(t *testing.T) {
		ms := mockLegacyMcpServerWithConfig(MCPConfig{LegacyDiscovery: true, AllowWorkflowExecution: true})
		ms.srv = server.NewMCPServer("test", "0.0.0")
		ms.registerTools()

		if _, exists := ms.srv.ListTools()["execute_workflow"]; !exists {
			t.Fatal("execute_workflow should be registered when both gates are enabled")
		}
	})
}

func TestHandleQueryCatalog_SearchWhereOrderExplain(t *testing.T) {
	ms := workflowCatalogTestServer(t, MCPConfig{}, nil)

	res, err := ms.handleQueryCatalog(context.Background(), newToolRequest(map[string]any{
		"search":   "running total",
		"where":    map[string]any{"kind": map[string]any{"in": []any{"directive", "query_pattern"}}},
		"order_by": map[string]any{"score": "desc"},
		"explain":  true,
		"limit":    3,
	}))
	if err != nil {
		t.Fatalf("handle query_catalog: %v", err)
	}

	text := assertToolSuccess(t, res)
	var out CatalogQueryResult
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("decode query_catalog response: %v", err)
	}
	if out.Count == 0 || len(out.Cards) == 0 {
		t.Fatal("expected catalog items")
	}
	if out.Cards[0].ID != "language:directive.running" {
		t.Fatalf("expected @running first, got %s", out.Cards[0].ID)
	}
	if len(out.Matches) == 0 || out.Matches[out.Cards[0].ID].Score <= 0 {
		t.Fatalf("expected match explanation, got %#v", out.Matches)
	}
}

func TestHandleCatalogEntrypointsAndCapabilitiesUseGraphQL(t *testing.T) {
	ms := workflowCatalogTestServer(t, MCPConfig{}, nil)

	entryRes, err := ms.handleGetCatalogEntrypoints(context.Background(), newToolRequest(nil))
	if err != nil {
		t.Fatalf("handle get_catalog_entrypoints: %v", err)
	}
	entryText := assertToolSuccess(t, entryRes)
	var entryOut CatalogEntrypointsResult
	if err := json.Unmarshal([]byte(entryText), &entryOut); err != nil {
		t.Fatalf("decode get_catalog_entrypoints response: %v", err)
	}
	if len(entryOut.Entrypoints) == 0 || entryOut.Entrypoints[0].Kind != "entrypoint" {
		t.Fatalf("expected GraphQL-backed catalog entrypoints, got %+v", entryOut.Entrypoints)
	}

	capRes, err := ms.handleGetCatalogCapabilities(context.Background(), newToolRequest(nil))
	if err != nil {
		t.Fatalf("handle get_catalog_capabilities: %v", err)
	}
	capText := assertToolSuccess(t, capRes)
	var capOut CatalogCapabilitiesResult
	if err := json.Unmarshal([]byte(capText), &capOut); err != nil {
		t.Fatalf("decode get_catalog_capabilities response: %v", err)
	}
	if len(capOut.Capabilities) == 0 || capOut.Capabilities[0].Kind != "capability" {
		t.Fatalf("expected GraphQL-backed catalog capabilities, got %+v", capOut.Capabilities)
	}
}

func TestRegisterResources_CatalogDefaultHidesLegacyResources(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{})
	ms.srv = server.NewMCPServer("test", "0.0.0", server.WithResourceCapabilities(true, false))
	ms.registerResources()

	uris := resourceURIsFromServer(t, ms.srv)
	for _, uri := range []string{CatalogOverviewResourceURI, CatalogEntrypointsResourceURI, CatalogCapabilitiesResourceURI, JSRuntimeResourceURI, QuerySyntaxResourceURI, MutationSyntaxResourceURI, WorkflowGuideResourceURI} {
		if uris[uri] {
			t.Fatalf("%s should be hidden in sources-used MCP resource registration", uri)
		}
	}
}

func TestRegisterResources_LegacyDiscoveryOptIn(t *testing.T) {
	ms := mockLegacyMcpServerWithConfig(MCPConfig{LegacyDiscovery: true})
	ms.srv = server.NewMCPServer("test", "0.0.0", server.WithResourceCapabilities(true, false))
	ms.registerResources()

	uris := resourceURIsFromServer(t, ms.srv)
	for _, uri := range []string{QuerySyntaxResourceURI, MutationSyntaxResourceURI, WorkflowGuideResourceURI} {
		if !uris[uri] {
			t.Fatalf("%s should be registered when legacy discovery is enabled", uri)
		}
	}
}

func TestMCPServerInstructions_CatalogDefaultDoesNotRecommendLegacyTools(t *testing.T) {
	text := mcpServerInstructions(&Config{Core: core.Config{Sources: []core.SourceConfig{{Name: "graphjin", Kind: "graphjin"}, {Name: "workflows", Kind: "workflow"}}}})
	for _, required := range []string{
		`graphql_help(for: "discovery")`,
		"graphql_query",
		"query_catalog",
		"query_catalog(id)",
		"validate_where_clause",
		"Discovery means selecting evidence-backed catalog items before acting",
		"details, evidence, examples, safety notes, and nearby graph edges",
		"Resolve ambiguity by inspecting candidate items",
		"sample/profile availability",
		"Catalog tools own nouns, facts, context, and evidence",
		"gj_catalog",
		"gj_workflow_execution(insert)",
		"gj_workflow(insert/update/delete)",
		`gj_config(id: "current", update: ...)`,
		`query_catalog(search: "workflow", where: { kind: { eq: "workflow" } })`,
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("catalog instructions should include %q:\n%s", required, text)
		}
	}
	for _, forbidden := range []string{
		"Call tool get_query_syntax",
		"Call tool list_tables",
		"Call get_query_syntax",
		"Use describe_table",
		"Use find_path",
		"Check list_workflows first",
		"gj_catalog_cards",
		"gj_schema_reloads(insert)",
		"gj_query_validations(insert)",
		"gj_query_repairs(insert)",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("catalog instructions should not recommend disabled legacy tool via %q:\n%s", forbidden, text)
		}
	}
}

func TestMCPServerInstructions_SourcesUsedIgnoresLegacyDiscoveryPrompt(t *testing.T) {
	text := mcpServerInstructions(&Config{
		Core: core.Config{Sources: []core.SourceConfig{{Name: "graphjin", Kind: "graphjin"}}},
		Serv: Serv{MCP: MCPConfig{LegacyDiscovery: true}},
	})
	for _, required := range []string{
		`graphql_help(for: "discovery")`,
		"query_catalog(id)",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("sources-used instructions should include %q:\n%s", required, text)
		}
	}
	for _, forbidden := range []string{
		"Call tool get_query_syntax",
		"Call tool list_tables",
		"Use describe_table",
		"Check list_workflows first",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("sources-used instructions should not use legacy prompt text via %q:\n%s", forbidden, text)
		}
	}
}

func TestMCPServerInstructions_LegacyDiscoveryMode(t *testing.T) {
	text := mcpServerInstructions(&Config{Serv: Serv{MCP: MCPConfig{LegacyDiscovery: true}}})
	for _, required := range []string{
		"Call tool get_query_syntax",
		"Call tool list_tables",
		"Use find_path or explore_relationships",
		"Use describe_table",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("legacy instructions should include %q:\n%s", required, text)
		}
	}
}

func TestMCPToolListMatchesRegisteredTools(t *testing.T) {
	testCases := []struct {
		name       string
		production bool
		cfg        MCPConfig
	}{
		{
			name:       "development all features enabled",
			production: false,
			cfg: MCPConfig{
				AllowRawQueries:    true,
				AllowConfigUpdates: true,
				AllowSchemaReload:  true,
				AllowSchemaUpdates: true,
				AllowDevTools:      true,
			},
		},
		{
			name:       "production all features enabled",
			production: true,
			cfg: MCPConfig{
				AllowRawQueries:    true,
				AllowConfigUpdates: true,
				AllowSchemaReload:  true,
				AllowSchemaUpdates: true,
				AllowDevTools:      true,
			},
		},
		{
			name:       "development minimal features",
			production: false,
			cfg:        MCPConfig{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			conf := &Config{
				Core: core.Config{Sources: []core.SourceConfig{
					{Name: "graphjin", Kind: "graphjin"},
					{Name: "workflows", Kind: "workflow"},
				}},
				Serv: Serv{Production: tc.production, MCP: tc.cfg},
			}
			expected := mcpToolList(conf)
			sort.Strings(expected)

			ms := mockMcpServerWithConfig(tc.cfg)
			ms.service.conf.Serv.Production = tc.production
			ms.srv = server.NewMCPServer("test", "0.0.0")
			ms.registerTools()

			actual := toolNamesFromServer(ms.srv.ListTools())
			if !reflect.DeepEqual(expected, actual) {
				t.Fatalf("mcpToolList mismatch\nexpected: %v\nactual:   %v", expected, actual)
			}
		})
	}
}
