package serv

import (
	"context"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

const jsRuntimeResourceURI = "graphjin://syntax/workflow-js"

// JSRuntimeAPI describes the functions exposed in the GraphJin JS runtime.
type JSRuntimeAPI struct {
	Runtime       string              `json:"runtime"`
	RuntimeStatus string              `json:"runtime_status"`
	EntryPoint    string              `json:"entry_point"`
	Globals       []JSRuntimeGlobal   `json:"globals"`
	Functions     []JSRuntimeFunction `json:"functions"`
	Notes         []string            `json:"notes,omitempty"`
}

// JSRuntimeGlobal describes one global in the JS runtime.
type JSRuntimeGlobal struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
}

// JSRuntimeFunction describes one callable function in the JS runtime.
type JSRuntimeFunction struct {
	Name        string         `json:"name"`
	Tool        string         `json:"tool,omitempty"`
	Description string         `json:"description,omitempty"`
	Arguments   map[string]any `json:"arguments,omitempty"`
	Required    []string       `json:"required,omitempty"`
}

// registerJSRuntimeTools registers tooling for JS runtime API discoverability.
func (ms *mcpServer) registerJSRuntimeTools() {
	ms.srv.AddTool(mcp.NewTool(
		"get_js_runtime_api",
		mcp.WithDescription("Get the machine-readable API for GraphJin's JS workflow runtime. "+
			"Use this FIRST before generating JavaScript workflows so the LLM can see exactly "+
			"which `gj.*` globals and functions are available."),
	), ms.handleGetJSRuntimeAPI)
}

// registerJSRuntimeResources registers static resources for JS workflow runtime docs.
func (ms *mcpServer) registerJSRuntimeResources() {
	ms.srv.AddResource(
		mcp.NewResource(
			jsRuntimeResourceURI,
			"GraphJin JS Runtime API",
			mcp.WithResourceDescription("Machine-readable API for GraphJin JS workflow runtime globals and callable functions."),
			mcp.WithMIMEType("application/json"),
		),
		func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			data, err := mcpMarshalJSON(ms.buildJSRuntimeAPI(), true)
			if err != nil {
				return nil, err
			}
			return []mcp.ResourceContents{
				mcp.TextResourceContents{
					URI:      req.Params.URI,
					MIMEType: "application/json",
					Text:     string(data),
				},
			}, nil
		},
	)
}

func (ms *mcpServer) handleGetJSRuntimeAPI(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	data, err := mcpMarshalJSON(ms.buildJSRuntimeAPI(), true)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

func (ms *mcpServer) buildJSRuntimeAPI() JSRuntimeAPI {
	api := JSRuntimeAPI{
		Runtime:       "goja",
		RuntimeStatus: "available",
		EntryPoint:    "function main(input) { ... } // globals: gj, ctx, input",
		Globals: []JSRuntimeGlobal{
			{
				Name:        "gj",
				Type:        "object",
				Description: "GraphJin host API namespace for tool calls and runtime metadata.",
			},
			{
				Name:        "ctx",
				Type:        "object",
				Description: "Read-only auth context (user, role, namespace and request metadata).",
			},
			{
				Name:        "input",
				Type:        "object",
				Description: "Caller-provided workflow input payload.",
			},
			{
				Name:        "console",
				Type:        "object",
				Description: "Structured logging helpers for workflow traces.",
			},
		},
		Functions: []JSRuntimeFunction{
			{
				Name:        "gj.tools.call",
				Description: "Call a workflow-callable MCP tool by name. Equivalent to calling gj.tools.<camelCaseTool>(args) for allowed tools.",
				Arguments: map[string]any{
					"tool": "string (workflow-callable MCP tool name, example: list_tables)",
					"args": "object (tool arguments)",
				},
				Required: []string{"tool"},
			},
			{
				Name:        "gj.meta.listFunctions",
				Description: "Return all exposed JS runtime functions and source MCP tool names.",
			},
		},
		Notes: []string{
			"IMPORTANT: All gj.tools.* functions return DECODED native JavaScript objects — ready to use directly.",
			"Example: var result = gj.tools.executeGraphql({query: 'query GetOrders { orders { id total } }'}); var orders = result.data.orders;",
			"Example: var tables = gj.tools.listTables().tables;",
			"Example: var schema = gj.tools.describeTable({table: 'orders'});",
			"Only workflow-callable tools are available inside scripts; config mutation, schema mutation, and workflow management tools are blocked.",
			"Tool errors throw JavaScript exceptions — use try/catch to handle them.",
			"Tool-level auth and policy checks are enforced exactly as in direct MCP calls.",
			"Function names are generated from MCP tool names by converting snake_case to camelCase.",
			"Example: list_tables -> gj.tools.listTables",
			"`execute_graphql` is available inside workflow scripts when allow_raw_queries is enabled.",
			"`execute_workflow`, `save_workflow`, `list_workflows`, config updates, and schema update tools are intentionally blocked inside workflow scripts.",
			"Named workflow execution endpoint: /api/v1/workflows/<name> (loads ./workflows/<name>.js).",
			"Workflow variables are supported: POST JSON body is passed to global `input` and `main(input)`.",
			"GET variables are supported via query param: /api/v1/workflows/<name>?variables={...json...}.",
			"Inside scripts you can read variables from either `input` global or the `main(input)` argument.",
			"When saving workflows, declare expected variables in workflow metadata so callers know which execute_workflow variables are required.",
		},
	}

	api.Functions = append(api.Functions, ms.jsToolFunctions()...)
	return api
}

func (ms *mcpServer) jsToolFunctions() []JSRuntimeFunction {
	tools := ms.srv.ListTools()
	out := make([]JSRuntimeFunction, 0, len(tools))

	for _, toolName := range ms.workflowCallableToolNames() {
		tool := tools[toolName].Tool
		fn := JSRuntimeFunction{
			Name:        "gj.tools." + snakeToCamel(toolName),
			Tool:        toolName,
			Description: tool.Description,
		}
		if len(tool.InputSchema.Properties) > 0 {
			fn.Arguments = copyStringAnyMap(tool.InputSchema.Properties)
		}
		if len(tool.InputSchema.Required) > 0 {
			fn.Required = append([]string(nil), tool.InputSchema.Required...)
		}
		out = append(out, fn)
	}
	return out
}

func snakeToCamel(s string) string {
	parts := strings.Split(s, "_")
	if len(parts) == 0 {
		return s
	}

	var b strings.Builder
	b.WriteString(parts[0])

	for i := 1; i < len(parts); i++ {
		p := parts[i]
		if p == "" {
			continue
		}
		b.WriteString(strings.ToUpper(p[:1]))
		if len(p) > 1 {
			b.WriteString(p[1:])
		}
	}
	return b.String()
}

func copyStringAnyMap(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
