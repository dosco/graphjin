package serv

import (
	"context"
	"encoding/json"

	"github.com/mark3labs/mcp-go/mcp"
)

// registerExplainTools registers the explain_query tool
func (ms *mcpServer) registerExplainTools() {
	if !ms.service.conf.MCP.AllowDevTools {
		return
	}

	ms.srv.AddTool(mcp.NewTool(
		"explain_query",
		mcp.WithDescription("Compile a GraphQL query to SQL WITHOUT executing it. "+
			"Returns the generated SQL, parameter bindings, tables touched, join depth, and cache info. "+
			"Use this to debug performance, verify correctness, and understand the SQL before running it."),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("The GraphQL query to explain"),
		),
		mcp.WithObject("variables",
			mcp.Description("Optional query variables as a JSON object"),
		),
		mcp.WithString("role",
			mcp.Description("Optional role to compile the query as (e.g., 'user', 'anon'). Defaults to the current session role."),
		),
	), ms.handleExplainQuery)
}

// handleExplainQuery compiles a query to SQL and returns the explanation
func (ms *mcpServer) handleExplainQuery(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if ms.service.gj == nil {
		return mcp.NewToolResultError("GraphJin not initialized - no database connection configured"), nil
	}

	args := req.GetArguments()
	query, _ := args["query"].(string)
	role, _ := args["role"].(string)

	if query == "" {
		return mcp.NewToolResultError("query is required"), nil
	}

	// Handle variables
	var vars json.RawMessage
	if v, ok := args["variables"]; ok && v != nil {
		varBytes, err := json.Marshal(v)
		if err != nil {
			return mcp.NewToolResultError("invalid variables: " + err.Error()), nil
		}
		vars = varBytes
	}

	explanation, err := ms.service.gj.ExplainQuery(query, vars, role)
	if err != nil {
		return mcp.NewToolResultError("explain failed: " + err.Error()), nil
	}

	data, err := mcpMarshalJSON(explanation, true)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}
