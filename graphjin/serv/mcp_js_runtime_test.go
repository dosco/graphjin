package serv

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/server"
)

func TestHandleGetJSRuntimeAPI_IncludesMappedTools(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{
		AllowRawQueries:    true,
		AllowMutations:     true,
		AllowConfigUpdates: true,
		AllowSchemaReload:  true,
		AllowSchemaUpdates: true,
		AllowDevTools:      true,
	})
	ms.service.conf.Serv.Production = false
	ms.srv = server.NewMCPServer("test", "0.0.0")
	ms.registerTools()

	res, err := ms.handleGetJSRuntimeAPI(context.Background(), newToolRequest(map[string]any{}))
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	text := assertToolSuccess(t, res)
	var api JSRuntimeAPI
	if err := json.Unmarshal([]byte(text), &api); err != nil {
		t.Fatalf("failed to decode API response: %v", err)
	}

	if api.Runtime != "goja" {
		t.Fatalf("expected runtime goja, got %q", api.Runtime)
	}
	if !hasJSFunction(api.Functions, "gj.tools.queryCatalog") {
		t.Fatal("expected gj.tools.queryCatalog to be exposed")
	}
	if !hasJSFunction(api.Functions, "gj.tools.executeGraphql") {
		t.Fatal("expected gj.tools.executeGraphql to be exposed when raw queries are enabled")
	}
	if hasJSFunction(api.Functions, "gj.tools.getCurrentConfig") {
		t.Fatal("did not expect get_current_config to be exposed inside workflow runtime")
	}
	if hasJSFunction(api.Functions, "gj.tools.getJsRuntimeApi") {
		t.Fatal("did not expect get_js_runtime_api to be exposed as a runtime tool function")
	}
	if hasJSFunction(api.Functions, "gj.tools.executeWorkflow") {
		t.Fatal("did not expect execute_workflow to be exposed as a runtime tool function")
	}
	if hasJSFunction(api.Functions, "gj.tools.saveWorkflow") {
		t.Fatal("did not expect save_workflow to be exposed as a runtime tool function")
	}
	if !hasNote(api.Notes, "queryCatalog({where: {kind: {eq: 'table'}, table_name: {eq: 'orders'}}})") {
		t.Fatal("expected queryCatalog example to use GraphJin-style where")
	}
	if !hasNote(api.Notes, "Only workflow-callable tools are available inside scripts") {
		t.Fatal("expected runtime notes to describe workflow tool allowlist")
	}
	if hasNote(api.Notes, ".table;") {
		t.Fatal("did not expect catalog docs to mention a .table suffix")
	}
	if hasNote(api.Notes, "GraphQL queries MUST be named") {
		t.Fatal("did not expect unsupported named-query guidance in JS runtime notes")
	}

	queryCatalog := findJSFunction(api.Functions, "gj.tools.queryCatalog")
	if queryCatalog == nil {
		t.Fatal("expected gj.tools.queryCatalog to be exposed")
	}
	if _, ok := queryCatalog.Arguments["where"]; !ok {
		t.Fatal("expected gj.tools.queryCatalog arguments to expose where")
	}
}

func TestHandleGetJSRuntimeAPI_RespectsToolGates(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{
		AllowRawQueries: false,
		AllowMutations:  true,
	})
	ms.service.conf.Serv.Production = true
	ms.srv = server.NewMCPServer("test", "0.0.0")
	ms.registerTools()

	res, err := ms.handleGetJSRuntimeAPI(context.Background(), newToolRequest(map[string]any{}))
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	text := assertToolSuccess(t, res)
	var api JSRuntimeAPI
	if err := json.Unmarshal([]byte(text), &api); err != nil {
		t.Fatalf("failed to decode API response: %v", err)
	}

	if hasJSFunction(api.Functions, "gj.tools.executeGraphql") {
		t.Fatal("execute_graphql should not be exposed when raw queries are disabled")
	}
	if hasJSFunction(api.Functions, "gj.tools.getCurrentConfig") {
		t.Fatal("get_current_config should not be exposed in workflow runtime")
	}
	if hasJSFunction(api.Functions, "gj.tools.listTables") {
		t.Fatal("legacy list_tables should not be exposed without legacy_discovery")
	}
}

func TestHandleGetJSRuntimeAPI_LegacyDiscoveryExposesLegacyListTables(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{LegacyDiscovery: true})
	ms.srv = server.NewMCPServer("test", "0.0.0")
	ms.registerTools()

	res, err := ms.handleGetJSRuntimeAPI(context.Background(), newToolRequest(map[string]any{}))
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	text := assertToolSuccess(t, res)
	var api JSRuntimeAPI
	if err := json.Unmarshal([]byte(text), &api); err != nil {
		t.Fatalf("failed to decode API response: %v", err)
	}
	if !hasJSFunction(api.Functions, "gj.tools.listTables") {
		t.Fatal("expected legacy list_tables to be exposed when legacy_discovery is enabled")
	}
	if hasJSFunction(api.Functions, "gj.tools.listWorkflows") {
		t.Fatal("list_workflows should remain blocked inside workflow runtime")
	}
}

func TestHandleGetJSRuntimeAPI_ExposesWorkflowTimeout(t *testing.T) {
	// Configured timeout should be surfaced
	ms := mockMcpServerWithConfig(MCPConfig{WorkflowTimeout: 120})
	ms.srv = server.NewMCPServer("test", "0.0.0")
	ms.registerTools()

	res, err := ms.handleGetJSRuntimeAPI(context.Background(), newToolRequest(map[string]any{}))
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	text := assertToolSuccess(t, res)
	var api JSRuntimeAPI
	if err := json.Unmarshal([]byte(text), &api); err != nil {
		t.Fatalf("failed to decode API response: %v", err)
	}

	if api.WorkflowTimeout != 120 {
		t.Fatalf("expected workflow_timeout_seconds=120, got %d", api.WorkflowTimeout)
	}
}

func TestHandleGetJSRuntimeAPI_DefaultWorkflowTimeout(t *testing.T) {
	// When not configured, should show the default (5s)
	ms := mockMcpServerWithConfig(MCPConfig{})
	ms.srv = server.NewMCPServer("test", "0.0.0")
	ms.registerTools()

	res, err := ms.handleGetJSRuntimeAPI(context.Background(), newToolRequest(map[string]any{}))
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	text := assertToolSuccess(t, res)
	var api JSRuntimeAPI
	if err := json.Unmarshal([]byte(text), &api); err != nil {
		t.Fatalf("failed to decode API response: %v", err)
	}

	if api.WorkflowTimeout != defaultWorkflowScriptTimeout {
		t.Fatalf("expected default workflow_timeout_seconds=%d, got %d", defaultWorkflowScriptTimeout, api.WorkflowTimeout)
	}
}

func hasJSFunction(functions []JSRuntimeFunction, name string) bool {
	for _, f := range functions {
		if f.Name == name {
			return true
		}
	}
	return false
}

func findJSFunction(functions []JSRuntimeFunction, name string) *JSRuntimeFunction {
	for i := range functions {
		if functions[i].Name == name {
			return &functions[i]
		}
	}
	return nil
}

func hasNote(notes []string, fragment string) bool {
	for _, note := range notes {
		if strings.Contains(note, fragment) {
			return true
		}
	}
	return false
}
