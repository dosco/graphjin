package serv

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/server"
)

func TestHandleGetJSRuntimeAPI_IncludesMappedTools(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{
		AllowRawQueries:    true,
		AllowMutations:     true,
		EnableSearch:       true,
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
	if !hasJSFunction(api.Functions, "gj.tools.listTables") {
		t.Fatal("expected gj.tools.listTables to be exposed")
	}
	if !hasJSFunction(api.Functions, "gj.tools.executeGraphql") {
		t.Fatal("expected gj.tools.executeGraphql to be exposed when raw queries are enabled")
	}
	if !hasJSFunction(api.Functions, "gj.tools.getCurrentConfig") {
		t.Fatal("expected gj.tools.getCurrentConfig in development mode")
	}
	if hasJSFunction(api.Functions, "gj.tools.getJsRuntimeApi") {
		t.Fatal("did not expect get_js_runtime_api to be exposed as a runtime tool function")
	}
	if hasJSFunction(api.Functions, "gj.tools.executeWorkflow") {
		t.Fatal("did not expect execute_workflow to be exposed as a runtime tool function")
	}
}

func TestHandleGetJSRuntimeAPI_RespectsToolGates(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{
		AllowRawQueries: false,
		AllowMutations:  true,
		EnableSearch:    false,
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
		t.Fatal("get_current_config should not be exposed in production mode")
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
