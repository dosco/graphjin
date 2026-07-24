package serv

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/core/v3/featurecap"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type deadlineCaptureWriter struct {
	header             http.Header
	readDeadlineCalls  int
	writeDeadlineCalls int
	readDeadline       time.Time
	writeDeadline      time.Time
}

func (w *deadlineCaptureWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (*deadlineCaptureWriter) Write(p []byte) (int, error) { return len(p), nil }
func (*deadlineCaptureWriter) WriteHeader(int)             {}

func (w *deadlineCaptureWriter) SetReadDeadline(deadline time.Time) error {
	w.readDeadlineCalls++
	w.readDeadline = deadline
	return nil
}

func (w *deadlineCaptureWriter) SetWriteDeadline(deadline time.Time) error {
	w.writeDeadlineCalls++
	w.writeDeadline = deadline
	return nil
}

func toolNamesFromServer(tools map[string]*server.ServerTool) []string {
	names := make([]string, 0, len(tools))
	for name := range tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func TestMCPRequestCallsToolRestoresBodyAndStripsPrefix(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"GraphJin:ask_graphjin_agent","arguments":{"instruction":"x"}}}`
	req := &http.Request{
		Method: http.MethodPost,
		Body:   io.NopCloser(strings.NewReader(body)),
	}
	if !mcpRequestCallsTool(req, mcpToolAskGraphJinAgent) {
		t.Fatal("expected request to call ask_graphjin_agent")
	}
	restored, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("read restored body: %v", err)
	}
	if string(restored) != body {
		t.Fatalf("body was not restored: %s", restored)
	}
}

func TestExtendDeadlineForMCPRequestClearsNotificationStreamDeadline(t *testing.T) {
	w := &deadlineCaptureWriter{}
	req := &http.Request{
		Method: http.MethodGet,
		Header: http.Header{"Accept": []string{"text/event-stream"}},
	}

	extendDeadlineForMCPRequest(w, req, &Config{})

	if w.readDeadlineCalls != 1 || !w.readDeadline.IsZero() {
		t.Fatalf("read deadline calls=%d deadline=%v, want one cleared deadline", w.readDeadlineCalls, w.readDeadline)
	}
	if w.writeDeadlineCalls != 1 || !w.writeDeadline.IsZero() {
		t.Fatalf("write deadline calls=%d deadline=%v, want one cleared deadline", w.writeDeadlineCalls, w.writeDeadline)
	}
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

func toolsFromServer(t *testing.T, srv *server.MCPServer, ctx context.Context) map[string]mcp.Tool {
	t.Helper()

	c, err := client.NewInProcessClient(srv)
	if err != nil {
		t.Fatalf("new in-process client: %v", err)
	}
	defer c.Close()

	if ctx == nil {
		ctx = context.Background()
	}
	if err := c.Start(ctx); err != nil {
		t.Fatalf("start in-process client: %v", err)
	}

	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{Name: "test-client", Version: "1.0.0"}
	if _, err := c.Initialize(ctx, initReq); err != nil {
		t.Fatalf("initialize in-process client: %v", err)
	}

	result, err := c.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}

	tools := make(map[string]mcp.Tool, len(result.Tools))
	for _, tool := range result.Tools {
		tools[tool.Name] = tool
	}
	return tools
}

func TestRegisterConfigTools_DoesNotExposeLegacyConfigTools(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{AllowConfigUpdates: true})
	ms.service.conf.Serv.Production = false
	ms.srv = server.NewMCPServer("test", "0.0.0")
	ms.registerConfigTools()

	for _, name := range []string{"get_current_config", "update_current_config"} {
		if _, exists := ms.srv.ListTools()[name]; exists {
			t.Fatalf("%s should not be registered by MCP", name)
		}
	}
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
	if _, exists := tools["apply_database_setup"]; exists {
		t.Fatal("apply_database_setup should not be registered by the catalog-first MCP surface")
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

func TestSourcesUsedRegistersNoDefaultPromptsAndOnlyWatchResource(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{})
	ms.srv = server.NewMCPServer("test", "0.0.0", server.WithPromptCapabilities(true), server.WithResourceCapabilities(true, false))
	ms.registerPrompts()
	ms.registerResources()

	if prompts := promptNamesFromServer(t, ms.srv); len(prompts) != 0 {
		t.Fatalf("expected no prompts in sources-used mode, got %v", prompts)
	}
	resources := resourceURIsFromServer(t, ms.srv)
	if len(resources) != 1 || !resources[WatchEventsUnseenResourceURI] {
		t.Fatalf("expected only the watch-events resource in sources-used mode, got %v", resources)
	}
}

func TestLegacyRegistrationHooksAreInert(t *testing.T) {
	ms := mockLegacyMcpServerWithConfig(MCPConfig{
		LegacyDiscovery:        true,
		AllowRawQueries:        true,
		AllowWorkflowExecution: true,
		AllowWorkflowUpdates:   true,
		AllowConfigUpdates:     true,
		AllowSchemaReload:      true,
		AllowSchemaUpdates:     true,
		AllowDevTools:          true,
	})
	ms.service.conf.Serv.Production = false
	ms.srv = server.NewMCPServer("test", "0.0.0", server.WithPromptCapabilities(true), server.WithResourceCapabilities(true, false))

	legacyHooks := []func(){
		ms.registerAuditTools,
		ms.registerCatalogResources,
		ms.registerConfigDocsTool,
		// registerConfigTools is no longer inert — it registers the dev-mode
		// config read/validate/update tools (see TestRegisterConfigTools_DevOnly).
		ms.registerDDLTools,
		ms.registerDiscoverTools,
		ms.registerExploreTools,
		ms.registerExplainTools,
		ms.registerFragmentTools,
		ms.registerGuidanceTools,
		ms.registerHealthTools,
		ms.registerInsightsTools,
		ms.registerJSRuntimeResources,
		ms.registerJSRuntimeTools,
		ms.registerOnboardingTools,
		ms.registerPrompts,
		ms.registerQueryDiscoveryTools,
		ms.registerResources,
		ms.registerSyntaxTools,
		ms.registerWorkflowMgmtTools,
	}
	for _, hook := range legacyHooks {
		hook()
	}

	if tools := ms.srv.ListTools(); len(tools) != 0 {
		t.Fatalf("legacy registration hooks should not register tools, got %v", toolNamesFromServer(tools))
	}
	if prompts := promptNamesFromServer(t, ms.srv); len(prompts) != 0 {
		t.Fatalf("legacy registration hooks should not register prompts, got %v", prompts)
	}
	if resources := resourceURIsFromServer(t, ms.srv); len(resources) != 0 {
		t.Fatalf("legacy registration hooks should not register resources, got %v", resources)
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
		t.Fatalf("unexpected mcpToolList: %v", listed)
	}
	for _, name := range []string{"graphql_help", "query_catalog", "execute_saved_query", "validate_where_clause"} {
		if _, exists := tools[name]; !exists {
			t.Fatalf("%s should be registered by default", name)
		}
	}
	if len(tools) != 4 {
		t.Fatalf("default tools should be exactly 4, got %v", toolNamesFromServer(tools))
	}
	for _, name := range []string{"get_catalog_card", "get_catalog_entrypoints", "get_catalog_capabilities", "list_tables", "describe_table", "find_path", "get_table_sample", "get_query_syntax", "get_mutation_syntax", "get_workflow_guide", "list_workflows", "write_query", "write_mutation", "fix_query_error", "get_config_docs"} {
		if _, exists := tools[name]; exists {
			t.Fatalf("%s should be hidden in default MCP registration", name)
		}
	}
}

func TestRegisterTools_AgentToolOptIn(t *testing.T) {
	// Default: when the agent tool is enabled it is the single front door, and the
	// low-level primitives it orchestrates are hidden to keep the surface minimal.
	ms := mockMcpServerWithConfig(MCPConfig{})
	ms.service.conf.Agent.Enabled = true
	ms.service.conf.Agent.Provider = "openai"
	ms.srv = server.NewMCPServer("test", "0.0.0")
	ms.registerTools()

	tools := ms.srv.ListTools()
	if _, exists := tools[mcpToolAskGraphJinAgent]; !exists {
		t.Fatalf("%s should be registered when agent.enabled=true", mcpToolAskGraphJinAgent)
	}
	if listed := mcpToolList(ms.service.conf); strings.Join(listed, ",") != "ask_graphjin_agent" {
		t.Fatalf("with the agent enabled, only the agent tool should be exposed by default, got: %v", listed)
	}
	for _, name := range []string{"graphql_help", "query_catalog", "execute_saved_query", "validate_where_clause"} {
		if _, exists := tools[name]; exists {
			t.Fatalf("%s should be hidden when the agent is the front door (mcp.include_tools_with_agent=false)", name)
		}
	}

	// Opt-in: mcp.include_tools_with_agent re-exposes the primitives alongside the agent.
	ms2 := mockMcpServerWithConfig(MCPConfig{IncludeToolsWithAgent: true})
	ms2.service.conf.Agent.Enabled = true
	ms2.service.conf.Agent.Provider = "openai"
	ms2.srv = server.NewMCPServer("test", "0.0.0")
	ms2.registerTools()
	if listed := mcpToolList(ms2.service.conf); strings.Join(listed, ",") != "graphql_help,query_catalog,execute_saved_query,validate_where_clause,ask_graphjin_agent" {
		t.Fatalf("with include_tools_with_agent=true, primitives + agent should be exposed, got: %v", listed)
	}
	if _, exists := ms2.srv.ListTools()["query_catalog"]; !exists {
		t.Fatal("query_catalog should be registered when include_tools_with_agent=true")
	}
}

func TestAgentConfigFromServiceMapsReadOnly(t *testing.T) {
	// Raw GraphQL is always available to the agent now; the only agent-level write
	// gate is read_only, which must propagate from serv config to the agent config.
	conf := &Config{Serv: Serv{Agent: AgentConfig{Enabled: true}}}
	if got := agentConfigFromService(conf); got.ReadOnly {
		t.Fatal("agent read_only should default to false")
	}
	conf.Agent.ReadOnly = true
	if got := agentConfigFromService(conf); !got.ReadOnly {
		t.Fatal("agent read_only should propagate to the agent runtime config")
	}
}

func TestHandleAskGraphJinAgentRequiresInstruction(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{})
	ms.service.conf.Agent.Enabled = true
	ms.service.gj = &core.GraphJin{}
	res, err := ms.handleAskGraphJinAgent(context.Background(), newToolRequest(map[string]any{}))
	if err != nil {
		t.Fatalf("handleAskGraphJinAgent: %v", err)
	}
	assertToolError(t, res, "agent instruction is required")
}

func TestRegisterTools_LegacyDiscoveryDoesNotExpandMCPTools(t *testing.T) {
	for _, tc := range []struct {
		name     string
		ms       *mcpServer
		wantList string
		wantLen  int
	}{
		// Sources mock runs in agentic mode, so the dev-only config tools stay
		// hidden; the legacy (non-sources) mock defaults to dev mode, where
		// get_current_config/validate_config are exposed.
		{name: "sources", ms: mockMcpServerWithConfig(MCPConfig{LegacyDiscovery: true, AllowRawQueries: true, AllowWorkflowExecution: true}), wantList: "graphql_help,query_catalog,execute_saved_query,validate_where_clause,execute_graphql", wantLen: 5},
		{name: "non_sources", ms: mockLegacyMcpServerWithConfig(MCPConfig{LegacyDiscovery: true, AllowRawQueries: true, AllowWorkflowExecution: true}), wantList: "graphql_help,query_catalog,execute_saved_query,validate_where_clause,execute_graphql,get_current_config,validate_config", wantLen: 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ms := tc.ms
			ms.srv = server.NewMCPServer("test", "0.0.0")
			ms.registerTools()

			tools := ms.srv.ListTools()
			if listed := mcpToolList(ms.service.conf); strings.Join(listed, ",") != tc.wantList {
				t.Fatalf("unexpected mcpToolList with legacy discovery: %v", listed)
			}
			if len(tools) != tc.wantLen {
				t.Fatalf("legacy discovery should not expand MCP tools, got %v", toolNamesFromServer(tools))
			}
			if _, exists := tools["execute_graphql"]; !exists {
				t.Fatal("execute_graphql should be registered when raw queries are enabled")
			}
			for _, name := range []string{"get_query_syntax", "list_tables", "describe_table", "get_catalog_card", "execute_workflow"} {
				if _, exists := tools[name]; exists {
					t.Fatalf("%s should remain hidden", name)
				}
			}
		})
	}
}

func TestRegisterTools_SourcesUsedRawGraphQLCapabilityControlsTool(t *testing.T) {
	testCases := []struct {
		name         string
		mcp          MCPConfig
		capability   bool
		wantFlag     bool
		wantTool     bool
		wantToolList string
	}{
		{
			name:         "capability enables raw execution",
			mcp:          MCPConfig{},
			capability:   true,
			wantFlag:     true,
			wantTool:     true,
			wantToolList: "graphql_help,query_catalog,execute_saved_query,validate_where_clause,execute_graphql",
		},
		{
			name:         "capability suppresses mcp flag",
			mcp:          MCPConfig{AllowRawQueries: true},
			capability:   false,
			wantFlag:     false,
			wantTool:     false,
			wantToolList: "graphql_help,query_catalog,execute_saved_query,validate_where_clause",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ms := mockMcpServerWithConfig(tc.mcp)
			ms.service.conf.Core.System.Capabilities[featurecap.KeyRawGraphQLQuery] = tc.capability
			applySourceCapabilityMCPDefaults(ms.service.conf)
			if ms.service.conf.MCP.AllowRawQueries != tc.wantFlag {
				t.Fatalf("AllowRawQueries = %v, want %v", ms.service.conf.MCP.AllowRawQueries, tc.wantFlag)
			}

			ms.srv = server.NewMCPServer("test", "0.0.0")
			ms.registerTools()

			tools := ms.srv.ListTools()
			if listed := strings.Join(mcpToolList(ms.service.conf), ","); listed != tc.wantToolList {
				t.Fatalf("unexpected sources-used mcpToolList: %s", listed)
			}
			_, exists := tools["execute_graphql"]
			if exists != tc.wantTool {
				t.Fatalf("execute_graphql registered = %v, want %v; tools=%v", exists, tc.wantTool, toolNamesFromServer(tools))
			}
		})
	}
}

func TestRegisterTools_CatalogToolsAreBuiltIn(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{})
	ms.service.conf.Core.Sources = []core.SourceConfig{{Name: "app", Kind: "database"}}
	ms.srv = server.NewMCPServer("test", "0.0.0")
	ms.registerTools()

	tools := ms.srv.ListTools()
	if listed := strings.Join(mcpToolList(ms.service.conf), ","); listed != "graphql_help,query_catalog,execute_saved_query,validate_where_clause" {
		t.Fatalf("unexpected built-in catalog tool list: %s", listed)
	}
	if _, exists := tools["query_catalog"]; !exists {
		t.Fatal("query_catalog should register without a fake catalog source")
	}
	if _, exists := tools["graphql_help"]; !exists {
		t.Fatal("graphql_help should register without a fake catalog source")
	}
	for _, name := range []string{"execute_saved_query", "validate_where_clause"} {
		if _, exists := tools[name]; !exists {
			t.Fatalf("%s should still register with the built-in catalog", name)
		}
	}
}

func TestRegisterTools_CatalogCapabilityDisabledKeepsBootstrapAndValidation(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{})
	ms.service.conf.Core.Mode = "dev"
	ms.service.conf.Core.System.Capabilities[featurecap.KeyCatalogRead] = false
	ms.srv = server.NewMCPServer("test", "0.0.0")
	ms.registerTools()

	tools := ms.srv.ListTools()
	if _, exists := tools["query_catalog"]; exists {
		t.Fatalf("query_catalog must not register when catalog.read is disabled: %v", toolNamesFromServer(tools))
	}
	for _, name := range []string{"graphql_help", "get_current_config", "validate_config"} {
		if _, exists := tools[name]; !exists {
			t.Fatalf("%s must remain available when catalog.read is disabled: %v", name, toolNamesFromServer(tools))
		}
	}
}

func TestRegisterTools_NonSourcesUsesCatalogFirstSurface(t *testing.T) {
	ms := mockLegacyMcpServerWithConfig(MCPConfig{LegacyDiscovery: true})
	ms.service.conf.Serv.Production = false
	ms.srv = server.NewMCPServer("test", "0.0.0")
	ms.registerTools()

	tools := ms.srv.ListTools()
	if listed := strings.Join(mcpToolList(ms.service.conf), ","); listed != "graphql_help,query_catalog,execute_saved_query,validate_where_clause,get_current_config,validate_config" {
		t.Fatalf("unexpected non-sources tool list: %s", listed)
	}
	for _, name := range []string{"graphql_help", "query_catalog", "execute_saved_query", "validate_where_clause", "get_current_config", "validate_config"} {
		if _, exists := tools[name]; !exists {
			t.Fatalf("%s should be registered in non-sources MCP", name)
		}
	}
	for _, name := range []string{"list_tables", "describe_table", "find_path", "get_query_syntax", "get_workflow_guide", "list_workflows"} {
		if _, exists := tools[name]; exists {
			t.Fatalf("%s should not be registered in non-sources MCP", name)
		}
	}
}

func TestRegisterTools_LegacyExecuteWorkflowIsRemoved(t *testing.T) {
	t.Run("hidden without execution gate", func(t *testing.T) {
		ms := mockLegacyMcpServerWithConfig(MCPConfig{LegacyDiscovery: true})
		ms.srv = server.NewMCPServer("test", "0.0.0")
		ms.registerTools()

		if _, exists := ms.srv.ListTools()["execute_workflow"]; exists {
			t.Fatal("execute_workflow should require mcp.allow_workflow_execution")
		}
	})

	t.Run("hidden with legacy discovery and execution gate", func(t *testing.T) {
		ms := mockLegacyMcpServerWithConfig(MCPConfig{LegacyDiscovery: true, AllowWorkflowExecution: true})
		ms.srv = server.NewMCPServer("test", "0.0.0")
		ms.registerTools()

		if _, exists := ms.srv.ListTools()["execute_workflow"]; exists {
			t.Fatal("execute_workflow should not be registered by MCP")
		}
	})
}

func TestHandleQueryCatalog_SearchWhereOrderExplain(t *testing.T) {
	ms := workflowCatalogTestServer(t, MCPConfig{}, nil)

	res, err := ms.handleQueryCatalog(sourceModeUserTestContext(), newToolRequest(map[string]any{
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

func TestHandleQueryCatalog_NonSourcesUsesSnapshotFallback(t *testing.T) {
	ms := workflowCatalogTestServer(t, MCPConfig{}, nil)
	ms.service.conf.Core.Sources = nil

	res, err := ms.handleQueryCatalog(context.Background(), newToolRequest(map[string]any{
		"where": map[string]any{"kind": map[string]any{"eq": "help"}},
		"limit": 1,
	}))
	if err != nil {
		t.Fatalf("handle query_catalog: %v", err)
	}
	var out CatalogQueryResult
	if err := json.Unmarshal([]byte(assertToolSuccess(t, res)), &out); err != nil {
		t.Fatalf("decode query_catalog response: %v", err)
	}
	if len(out.Cards) != 1 || out.Cards[0].Kind != "help" {
		t.Fatalf("expected one help row from snapshot fallback, got %+v", out.Cards)
	}
	if out.Revision == "" {
		t.Fatal("expected snapshot fallback to include a catalog revision")
	}
}

func TestHandleQueryCatalog_ConfigRecipeNextGuidance(t *testing.T) {
	ms := workflowCatalogTestServer(t, MCPConfig{}, nil)

	res, err := ms.handleQueryCatalog(sourceModeUserTestContext(), newToolRequest(map[string]any{
		"search":  "add role from jwt",
		"limit":   5,
		"explain": true,
	}))
	if err != nil {
		t.Fatalf("handle query_catalog config recipe search: %v", err)
	}
	var out CatalogQueryResult
	if err := json.Unmarshal([]byte(assertToolSuccess(t, res)), &out); err != nil {
		t.Fatalf("decode query_catalog response: %v", err)
	}
	if len(out.Cards) == 0 || out.Cards[0].ID != "recipe.config.add_role" || out.Cards[0].Kind != "config_recipe" {
		t.Fatalf("expected add-role recipe first, got %+v", out.Cards)
	}
	if out.CapabilityProfile == nil {
		t.Fatal("expected config recipe search to include caller capability_profile")
	}
	if !stringSliceContains(out.CapabilityProfile.AvailableTools, "query_catalog") {
		t.Fatalf("expected query_catalog in available tools: %+v", out.CapabilityProfile.AvailableTools)
	}
	if !rootProfilesContain(out.CapabilityProfile.BlockedRoots, "gj_config") {
		t.Fatalf("expected non-admin config recipe search to mark gj_config unavailable: %+v", out.CapabilityProfile.BlockedRoots)
	}
	if out.Next == nil || out.Next.RecommendedTool != "query_catalog" || len(out.Next.Options) == 0 {
		t.Fatalf("expected query_catalog next guidance, got %+v", out.Next)
	}
	if got, _ := out.Next.Options[0].ArgsTemplate["id"].(string); got != "recipe.config.add_role" {
		t.Fatalf("expected next guidance to inspect recipe id, got %+v", out.Next.Options[0].ArgsTemplate)
	}
	for _, opt := range out.Next.Options {
		if opt.Tool == "validate_where_clause" {
			t.Fatalf("config recipe search should not recommend validate_where_clause: %+v", out.Next.Options)
		}
	}

	detailRes, err := ms.handleQueryCatalog(sourceModeUserTestContext(), newToolRequest(map[string]any{
		"id": "recipe.config.add_role",
	}))
	if err != nil {
		t.Fatalf("handle query_catalog config recipe detail: %v", err)
	}
	var detailOut CatalogQueryResult
	if err := json.Unmarshal([]byte(assertToolSuccess(t, detailRes)), &detailOut); err != nil {
		t.Fatalf("decode recipe detail response: %v", err)
	}
	if detailOut.Next == nil || detailOut.Next.StateCode != "config_recipe_detail" {
		t.Fatalf("expected recipe detail next guidance, got %+v", detailOut.Next)
	}
	for _, opt := range detailOut.Next.Options {
		if opt.Tool == "validate_where_clause" {
			t.Fatalf("config recipe detail should not recommend validate_where_clause: %+v", detailOut.Next.Options)
		}
	}
}

func TestMCPEffectiveContextMergesStoredAndPerCallIdentity(t *testing.T) {
	stored := context.WithValue(context.Background(), core.UserIDKey, "stdio-user")
	stored = context.WithValue(stored, core.UserRoleKey, "stdio-role")
	stored = context.WithValue(stored, core.IdentityVarsKey, map[string]interface{}{"account_id": "stdio-account"})
	ms := &mcpServer{ctx: stored}

	merged := ms.effectiveContext(context.Background())
	if got := merged.Value(core.UserIDKey); got != "stdio-user" {
		t.Fatalf("expected stored user_id, got %v", got)
	}
	if got := merged.Value(core.UserRoleKey); got != "stdio-role" {
		t.Fatalf("expected stored role, got %v", got)
	}

	perCall := context.WithValue(context.Background(), core.UserIDKey, "http-user")
	perCall = context.WithValue(perCall, core.IdentityVarsKey, map[string]interface{}{"account_id": "http-account"})
	merged = ms.effectiveContext(perCall)
	if got := merged.Value(core.UserIDKey); got != "http-user" {
		t.Fatalf("per-call user_id should win, got %v", got)
	}
	vars, _ := merged.Value(core.IdentityVarsKey).(map[string]interface{})
	if got := vars["account_id"]; got != "http-account" {
		t.Fatalf("per-call identity vars should win, got %v", vars)
	}
	if got := merged.Value(core.UserRoleKey); got != "stdio-role" {
		t.Fatalf("missing per-call role should be filled from stored context, got %v", got)
	}
}

func TestMCPEffectiveIdentityContextAppliesJWTAccountClaim(t *testing.T) {
	svc := &graphjinService{conf: &Config{
		Core: core.Config{Sources: []core.SourceConfig{{Name: "graphjin", Kind: "database", Type: "sqlite"}}},
		Serv: Serv{Auth: Auth{
			Type: "jwt",
			JWT:  JWTConfig{Secret: sourceModeHTTPJWTSecret},
		}},
	}}
	authHandler, err := svc.newMCPAuthHandler()
	if err != nil {
		t.Fatalf("new MCP auth handler: %v", err)
	}
	token := signSourceModeJWT(t, map[string]any{
		"sub":        "mcp-user",
		"account_id": "mcp-account",
	})
	req, err := http.NewRequest(http.MethodPost, routeMCP, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	ctx, err := authHandler(&deadlineCaptureWriter{}, req)
	if err != nil {
		t.Fatalf("authenticate MCP request: %v", err)
	}

	ms := &mcpServer{service: svc}
	ctx = ms.effectiveIdentityContext(ctx)
	if got, ok := identityVarString(ctx, "account_id"); !ok || got != "mcp-account" {
		t.Fatalf("effective MCP account identity = %q, %v; want mcp-account", got, ok)
	}
}

func TestMCPCallerCapabilityProfileReflectsSourceRootAccess(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{AllowRawQueries: true})

	anonProfile := ms.callerCapabilityProfile(context.Background(), false)
	if !rootProfilesContain(anonProfile.AvailableRoots, "gj_catalog") {
		t.Fatalf("expected anonymous catalog discovery by default, got %+v", anonProfile.AvailableRoots)
	}
	for _, root := range []string{"gj_security", "gj_runtime", "gj_config", "gj_workflow"} {
		if !rootProfilesContain(anonProfile.BlockedRoots, root) {
			t.Fatalf("expected %s blocked for anonymous caller, got %+v", root, anonProfile.BlockedRoots)
		}
	}

	userProfile := ms.callerCapabilityProfile(sourceModeUserTestContext(), false)
	if !userProfile.Authenticated || userProfile.RoleClass != "user" {
		t.Fatalf("expected authenticated user profile, got %+v", userProfile)
	}
	if !stringSliceContains(userProfile.AvailableTools, "query_catalog") || !stringSliceContains(userProfile.AvailableTools, "execute_graphql") {
		t.Fatalf("expected user-visible catalog/raw tools, got %+v", userProfile.AvailableTools)
	}
	// Agentic mode exposes catalog, owner-scoped state, and execution by default.
	for _, root := range []string{"gj_catalog", "gj_artifacts", "gj_watch", "gj_watch_event", "gj_workflow_execution"} {
		if !rootProfilesContain(userProfile.AvailableRoots, root) {
			t.Fatalf("expected %s available to user, got %+v", root, userProfile.AvailableRoots)
		}
	}
	// Detailed workflow/config/security features are disabled; runtime is admin-only.
	for _, root := range []string{"gj_workflow", "gj_security", "gj_runtime", "gj_config"} {
		if !rootProfilesContain(userProfile.BlockedRoots, root) {
			t.Fatalf("expected %s blocked for user, got %+v", root, userProfile.BlockedRoots)
		}
	}
	data, err := json.Marshal(userProfile)
	if err != nil {
		t.Fatalf("marshal profile: %v", err)
	}
	for _, forbidden := range []string{"app-user", "app-account", "Bearer"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("capability profile leaked %q: %s", forbidden, data)
		}
	}

	adminProfile := ms.callerCapabilityProfile(sourceModeAdminTestContext(), false)
	for _, root := range []string{"gj_runtime"} {
		if !rootProfilesContain(adminProfile.AvailableRoots, root) {
			t.Fatalf("expected %s available to admin, got %+v", root, adminProfile.AvailableRoots)
		}
	}
	for _, root := range []string{"gj_security", "gj_config", "gj_workflow"} {
		if !rootProfilesContain(adminProfile.BlockedRoots, root) {
			t.Fatalf("expected disabled %s to remain unavailable to admin, got %+v", root, adminProfile.BlockedRoots)
		}
	}

	// Watches disabled by config: the roots must not be advertised, and the
	// blocked reason must say so (not a source-mode access denial).
	disabled := mockMcpServerWithConfig(MCPConfig{AllowRawQueries: true})
	disabled.service.conf.Core.Watches.Enabled = false
	disabledProfile := disabled.callerCapabilityProfile(sourceModeUserTestContext(), false)
	for _, root := range []string{"gj_watch", "gj_watch_event"} {
		if rootProfilesContain(disabledProfile.AvailableRoots, root) {
			t.Fatalf("expected %s hidden when watches are disabled, got %+v", root, disabledProfile.AvailableRoots)
		}
		if !rootProfilesContain(disabledProfile.BlockedRoots, root) {
			t.Fatalf("expected %s blocked when watches are disabled, got %+v", root, disabledProfile.BlockedRoots)
		}
	}
	for _, rp := range disabledProfile.BlockedRoots {
		if (rp.Root == "gj_watch" || rp.Root == "gj_watch_event") && rp.Reason != "disabled by configuration" {
			t.Fatalf("expected config-disabled reason for %s, got %+v", rp.Root, rp)
		}
	}
}

func TestMCPToolFilterAndMetadataAreCallerAware(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{AllowRawQueries: true, AllowConfigUpdates: true})
	queryCatalogTool := mcp.NewTool("query_catalog")
	queryCatalogTool.Meta = mcp.NewMetaFromMap(map[string]any{"cached": true, "progressToken": "cached-token"})
	tools := []mcp.Tool{
		mcp.NewTool("graphql_help"),
		queryCatalogTool,
		mcp.NewTool("execute_saved_query"),
		mcp.NewTool("update_current_config"),
	}

	anon := ms.applyCallerToolMetadata(context.Background(), tools)
	if !toolListContains(anon, "query_catalog") || !toolListContains(anon, "graphql_help") {
		t.Fatalf("anonymous caller should see public catalog tools: %+v", toolNamesFromToolList(anon))
	}
	if !toolListContains(anon, "execute_saved_query") {
		t.Fatalf("anonymous caller should still see non-root execution tools: %+v", toolNamesFromToolList(anon))
	}

	user := ms.applyCallerToolMetadata(sourceModeUserTestContext(), tools)
	queryTool, ok := findMCPTool(user, "query_catalog")
	if !ok {
		t.Fatalf("authenticated user should see query_catalog: %+v", toolNamesFromToolList(user))
	}
	if toolListContains(user, "update_current_config") {
		t.Fatalf("non-admin user should not see gj_config update tool: %+v", toolNamesFromToolList(user))
	}
	graphjinMeta, ok := queryTool.Meta.AdditionalFields["graphjin"].(map[string]any)
	if !ok || graphjinMeta["category"] != "discovery" {
		t.Fatalf("query_catalog should include graphjin discovery meta, got %+v", queryTool.Meta)
	}
	if queryTool.Meta.AdditionalFields["cached"] != true || queryTool.Meta.ProgressToken != "cached-token" {
		t.Fatalf("tool filter should preserve existing _meta fields, got %+v", queryTool.Meta)
	}
	if queryTool.Annotations.ReadOnlyHint == nil || !*queryTool.Annotations.ReadOnlyHint ||
		queryTool.Annotations.DestructiveHint == nil || *queryTool.Annotations.DestructiveHint {
		t.Fatalf("query_catalog should be annotated read-only/non-destructive, got %+v", queryTool.Annotations)
	}

	admin := ms.applyCallerToolMetadata(sourceModeAdminTestContext(), tools)
	configTool, ok := findMCPTool(admin, "update_current_config")
	if !ok {
		t.Fatalf("admin should see update_current_config: %+v", toolNamesFromToolList(admin))
	}
	configMeta, ok := configTool.Meta.AdditionalFields["graphjin"].(map[string]any)
	if !ok || configMeta["config_preview_required"] != true {
		t.Fatalf("update_current_config should advertise preview requirement, got %+v", configTool.Meta)
	}
	if configTool.Annotations.DestructiveHint == nil || !*configTool.Annotations.DestructiveHint {
		t.Fatalf("update_current_config should be annotated destructive, got %+v", configTool.Annotations)
	}
}

func TestMCPToolsListUsesCallerFilterAndMetadata(t *testing.T) {
	anonSvc := mockMcpServerWithConfig(MCPConfig{AllowRawQueries: true}).service
	anonMS := anonSvc.newMCPServerWithContext(context.Background())
	anonTools := toolsFromServer(t, anonMS.srv, context.Background())
	if _, ok := anonTools["query_catalog"]; !ok {
		t.Fatalf("anonymous tools/list should include query_catalog: %+v", toolNamesFromToolMap(anonTools))
	}
	if _, ok := anonTools["graphql_help"]; !ok {
		t.Fatalf("anonymous tools/list should include graphql_help: %+v", toolNamesFromToolMap(anonTools))
	}
	if _, ok := anonTools["execute_saved_query"]; !ok {
		t.Fatalf("anonymous tools/list should keep non-root execution tools: %+v", toolNamesFromToolMap(anonTools))
	}

	userCtx := sourceModeUserTestContext()
	userSvc := mockMcpServerWithConfig(MCPConfig{AllowRawQueries: true}).service
	userMS := userSvc.newMCPServerWithContext(userCtx)
	userTools := toolsFromServer(t, userMS.srv, userCtx)
	queryTool, ok := userTools["query_catalog"]
	if !ok {
		t.Fatalf("authenticated tools/list should include query_catalog: %+v", toolNamesFromToolMap(userTools))
	}
	if queryTool.Meta == nil {
		t.Fatalf("query_catalog should include _meta.graphjin")
	}
	graphjinMeta, ok := queryTool.Meta.AdditionalFields["graphjin"].(map[string]any)
	if !ok || graphjinMeta["category"] != "discovery" {
		t.Fatalf("query_catalog should include graphjin discovery meta, got %+v", queryTool.Meta)
	}
	if queryTool.Annotations.ReadOnlyHint == nil || !*queryTool.Annotations.ReadOnlyHint {
		t.Fatalf("query_catalog should be read-only in tools/list, got %+v", queryTool.Annotations)
	}
}

// TestHandleQueryCatalog_TruncationAndOffset guards against silent truncation:
// a full page must report truncated + echo limit/offset, and offset must page to
// the next item so callers can reach every table rather than miss the tail.
func TestHandleQueryCatalog_TruncationAndOffset(t *testing.T) {
	ms := workflowCatalogTestServer(t, MCPConfig{}, nil)

	res, err := ms.handleQueryCatalog(sourceModeUserTestContext(), newToolRequest(map[string]any{"limit": 1}))
	if err != nil {
		t.Fatalf("handle query_catalog: %v", err)
	}
	var page1 CatalogQueryResult
	if err := json.Unmarshal([]byte(assertToolSuccess(t, res)), &page1); err != nil {
		t.Fatalf("decode page1: %v", err)
	}
	if page1.Count != 1 || page1.Limit != 1 {
		t.Fatalf("page1 count/limit = %d/%d, want 1/1", page1.Count, page1.Limit)
	}
	if !page1.Truncated {
		t.Fatal("page1 must be truncated when more catalog items exist beyond the limit")
	}

	res2, err := ms.handleQueryCatalog(sourceModeUserTestContext(), newToolRequest(map[string]any{"limit": 1, "offset": 1}))
	if err != nil {
		t.Fatalf("handle query_catalog offset: %v", err)
	}
	var page2 CatalogQueryResult
	if err := json.Unmarshal([]byte(assertToolSuccess(t, res2)), &page2); err != nil {
		t.Fatalf("decode page2: %v", err)
	}
	if page2.Offset != 1 {
		t.Fatalf("page2 offset = %d, want 1", page2.Offset)
	}
	if len(page1.Cards) == 1 && len(page2.Cards) == 1 && page1.Cards[0].ID == page2.Cards[0].ID {
		t.Fatalf("offset did not advance: both pages returned %s", page1.Cards[0].ID)
	}
}

func TestHandleCatalogEntrypointsAndCapabilitiesUseGraphQL(t *testing.T) {
	ms := workflowCatalogTestServer(t, MCPConfig{}, nil)

	ctx := sourceModeUserTestContext()
	entryRes, err := ms.handleGetCatalogEntrypoints(ctx, newToolRequest(nil))
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

	capRes, err := ms.handleGetCatalogCapabilities(ctx, newToolRequest(nil))
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
			t.Fatalf("%s should be hidden in MCP resource registration", uri)
		}
	}
}

func TestRegisterResources_LegacyDiscoveryDoesNotExposeResources(t *testing.T) {
	ms := mockLegacyMcpServerWithConfig(MCPConfig{LegacyDiscovery: true})
	ms.srv = server.NewMCPServer("test", "0.0.0", server.WithResourceCapabilities(true, false))
	ms.registerResources()

	uris := resourceURIsFromServer(t, ms.srv)
	for _, uri := range []string{QuerySyntaxResourceURI, MutationSyntaxResourceURI, WorkflowGuideResourceURI} {
		if uris[uri] {
			t.Fatalf("%s should not be registered when legacy discovery is enabled", uri)
		}
	}
}

func TestMCPServerInstructions_CatalogDefaultDoesNotRecommendLegacyTools(t *testing.T) {
	text := mcpServerInstructions(&Config{Core: core.Config{Mode: "dev", Sources: []core.SourceConfig{{Name: "graphjin", Kind: "database", Type: "sqlite"}}}})
	for _, required := range []string{
		`graphql_help(for: "discovery")`,
		`graphql_help(for: "mcp_tools")`,
		`query_catalog(search: "<user instruction>") -> query_catalog(id) -> validate_where_clause -> execute_saved_query`,
		"Topic routing",
		"graphql_query",
		"query_catalog",
		"query_catalog(id)",
		`query_catalog(id: "help:query")`,
		"validate_where_clause",
		"Legacy discovery MCP tools are gone from MCP",
		"Discovery means selecting evidence-backed catalog items before acting",
		"details, evidence, examples, safety notes, and nearby graph edges",
		"Resolve ambiguity by inspecting candidate items",
		"sample/profile availability",
		"Catalog tools own nouns, facts, context, and evidence",
		"gj_catalog",
		"gj_workflow_execution(insert)",
		"gj_workflow(insert/update/delete)",
		`gj_config(id: "current", update: ...)`,
		`graphql_help(for: "runtime")`,
		"gj_runtime",
		"decision support, not audit history",
		`query_catalog(search: "workflow", where: { kind: { eq: "workflow" } })`,
		"Parsed dev/agentic configs use runner all automatically",
		"no application subscription starts until an enabled, active, approved watch exists",
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

func TestSourcesUsedBootstrapToolDescriptions(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{})
	ms.srv = server.NewMCPServer("test", "0.0.0")
	ms.registerTools()

	tools := ms.srv.ListTools()
	helpDesc := tools["graphql_help"].Tool.Description
	for _, topic := range graphQLHelpTopics() {
		if !strings.Contains(helpDesc, topic) {
			t.Fatalf("graphql_help description should mention topic %q:\n%s", topic, helpDesc)
		}
	}
	for _, required := range []string{
		`query_catalog(search: "<user instruction>")`,
		"Replaces legacy MCP discovery",
		"get_query_syntax",
		"get_catalog_card",
		"fix_query_error",
		"replaces_tools",
	} {
		if !strings.Contains(helpDesc, required) {
			t.Fatalf("graphql_help description should include %q:\n%s", required, helpDesc)
		}
	}

	catalogDesc := tools["query_catalog"].Tool.Description
	for _, required := range []string{
		`query_catalog(search: "<user instruction>")`,
		"config_recipe",
		`query_catalog(search: "add role from jwt")`,
		`query_catalog(id: "help:query")`,
		`query_catalog(id: "help:schema")`,
		`query_catalog(where: { kind: { eq: "table" } })`,
		`query_catalog(where: { kind: { eq: "saved_query" } })`,
		"details_json",
		"evidence_json",
		"examples_json",
		"safety_json",
		"edges_json",
	} {
		if !strings.Contains(catalogDesc, required) {
			t.Fatalf("query_catalog description should include %q:\n%s", required, catalogDesc)
		}
	}
}

func TestMCPServerInstructions_SourcesUsedIgnoresLegacyDiscoveryPrompt(t *testing.T) {
	text := mcpServerInstructions(&Config{
		Core: core.Config{Mode: "dev", Sources: []core.SourceConfig{{Name: "graphjin", Kind: "database", Type: "sqlite"}}},
		Serv: Serv{MCP: MCPConfig{LegacyDiscovery: true}},
	})
	for _, required := range []string{
		`query_catalog(search: "<user instruction>")`,
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

func TestMCPServerInstructions_LegacyDiscoveryModeUsesCatalogInstructions(t *testing.T) {
	text := mcpServerInstructions(&Config{Serv: Serv{MCP: MCPConfig{LegacyDiscovery: true}}})
	for _, required := range []string{
		`query_catalog(search: "<user instruction>")`,
		`graphql_help(for: "discovery")`,
		"Legacy discovery MCP tools are gone from MCP",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("legacy-discovery instructions should include catalog guidance %q:\n%s", required, text)
		}
	}
	for _, forbidden := range []string{
		"Call tool get_query_syntax",
		"Call tool list_tables",
		"Use find_path or explore_relationships",
		"Use describe_table",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("legacy-discovery instructions should not include legacy prompt %q:\n%s", forbidden, text)
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
			// Mode selects the surface: dev/agentic mount MCP, prod hard-gates it.
			mode := "dev"
			if tc.production {
				mode = "prod"
			}
			conf := &Config{
				Core: core.Config{Mode: mode, Sources: []core.SourceConfig{{Name: "graphjin", Kind: "database", Type: "sqlite"}}},
				Serv: Serv{Production: tc.production, MCP: tc.cfg},
			}
			expected := mcpToolList(conf)
			if expected == nil {
				// prod hard-gates MCP: mcpToolList returns nil, the server yields an
				// empty (non-nil) slice; normalize so DeepEqual compares contents.
				expected = []string{}
			}
			sort.Strings(expected)

			ms := mockMcpServerWithConfig(tc.cfg)
			// Keep the registered surface consistent with the config used for
			// expected: prod hard-gates MCP even with every feature flag on.
			ms.service.conf.Core.Mode = mode
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

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func rootProfilesContain(roots []MCPRootProfile, want string) bool {
	for _, root := range roots {
		if root.Root == want {
			return true
		}
	}
	return false
}

func toolListContains(tools []mcp.Tool, want string) bool {
	_, ok := findMCPTool(tools, want)
	return ok
}

func findMCPTool(tools []mcp.Tool, want string) (mcp.Tool, bool) {
	for _, tool := range tools {
		if tool.Name == want {
			return tool, true
		}
	}
	return mcp.Tool{}, false
}

func toolNamesFromToolList(tools []mcp.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	return names
}

func toolNamesFromToolMap(tools map[string]mcp.Tool) []string {
	names := make([]string, 0, len(tools))
	for name := range tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
