package serv

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dosco/graphjin/core/v3"
	"github.com/mark3labs/mcp-go/mcp"
)

// registerExecutionTools registers the query execution tools
func (ms *mcpServer) registerExecutionTools() {
	sourcesUsed := ms.service.conf.Core.IsSourcesUsed()
	// execute_graphql - Only registered when AllowRawQueries is true
	if !sourcesUsed && ms.service.conf.MCP.AllowRawQueries {
		ms.srv.AddTool(mcp.NewTool(
			"execute_graphql",
			mcp.WithDescription("Execute a GraphJin GraphQL query or mutation against the database. "+
				"Use query_catalog and query_catalog(id: ...) to learn the DSL syntax first."),
			mcp.WithString("query",
				mcp.Required(),
				mcp.Description("The GraphQL query or mutation to execute. Use GraphJin DSL syntax."),
			),
			mcp.WithObject("variables",
				mcp.Description("Variables to pass to the query"),
			),
			mcp.WithString("namespace",
				mcp.Description("Optional namespace for multi-tenant deployments"),
			),
		), ms.handleExecuteGraphQL)
	}

	// execute_saved_query - Execute a pre-defined saved query
	ms.srv.AddTool(mcp.NewTool(
		"execute_saved_query",
		mcp.WithDescription("Execute a pre-defined saved query from the allow-list by name. "+
			"PREFER this over execute_graphql when a matching saved query exists - "+
			"saved queries are pre-validated and safer. Use query_catalog to find available saved queries."),
		mcp.WithString("name",
			mcp.Required(),
			mcp.Description("Name of the saved query to execute"),
		),
		mcp.WithObject("variables",
			mcp.Description("Variables to pass to the query"),
		),
		mcp.WithString("namespace",
			mcp.Description("Optional namespace for multi-tenant deployments"),
		),
	), ms.handleExecuteSavedQuery)

	if !sourcesUsed && ms.service.conf.legacyMCPToolsEnabled() && ms.service.conf.MCP.AllowWorkflowExecution {
		// execute_workflow - Execute a named JS workflow from ./workflows
		ms.srv.AddTool(mcp.NewTool(
			"execute_workflow",
			mcp.WithDescription("Compatibility tool for the GraphQL control-plane mutation gj_workflow_execution(insert). Execute a named JavaScript workflow from ./workflows/<name>.js. "+
				"Discover reusable workflows first with query_catalog(where: {kind: {eq: 'workflow'}}). "+
				"Use catalog workflow/runtime rows to inspect runtime globals and callable gj.tools.* functions. "+
				"If the workflow declares variables in metadata, provide them here."),
			mcp.WithString("name",
				mcp.Required(),
				mcp.Description("Workflow name, with or without .js extension"),
			),
			mcp.WithObject("variables",
				mcp.Description("Workflow input payload passed to global `input` and `main(input)`"),
			),
			mcp.WithString("namespace",
				mcp.Description("Optional namespace for multi-tenant deployments"),
			),
		), ms.handleExecuteWorkflow)
	}
}

// ExecuteResult represents the result of a query execution
type ExecuteResult struct {
	Data   json.RawMessage `json:"data"`
	Errors []ErrorInfo     `json:"errors,omitempty"`
}

// ErrorInfo represents an error from query execution
type ErrorInfo struct {
	Message    string         `json:"message"`
	Extensions map[string]any `json:"extensions,omitempty"`
}

// handleExecuteGraphQL executes a GraphQL query or mutation
func (ms *mcpServer) handleExecuteGraphQL(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Check if raw queries are allowed
	if !ms.service.conf.MCP.AllowRawQueries {
		return mcp.NewToolResultError("raw queries are not allowed. Use execute_saved_query instead or enable allow_raw_queries in config."), nil
	}

	args := req.GetArguments()
	query, _ := args["query"].(string)
	namespace, _ := args["namespace"].(string)

	if query == "" {
		return mcp.NewToolResultError("query is required"), nil
	}

	// Check if this is a mutation and if mutations are allowed
	if isMutation(query) && !ms.service.conf.MCP.AllowMutations {
		return mcp.NewToolResultError("mutations are not allowed. Enable allow_mutations in config."), nil
	}

	// Convert variables map to JSON
	var varsJSON json.RawMessage
	if vars, ok := args["variables"].(map[string]any); ok && len(vars) > 0 {
		// Expand cursor IDs to full encrypted cursors
		expandedVars, err := ms.expandCursorIDs(ctx, vars)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("cursor lookup failed: %v", err)), nil
		}
		varsJSON, err = json.Marshal(expandedVars)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid variables: %v", err)), nil
		}
	}

	var rc core.RequestConfig
	if namespace != "" {
		rc.SetNamespace(namespace)
	} else {
		rc.SetNamespace(ms.getNamespace())
	}

	if err := ms.service.checkGraphJinInitialized(); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	res, err := ms.service.gj.GraphQL(ctx, query, varsJSON, &rc)

	result := ExecuteResult{}
	if err != nil {
		result.Errors = []ErrorInfo{ms.errorInfo(query, err.Error(), "execute_graphql")}
	} else {
		// Replace encrypted cursors with short numeric IDs for LLM-friendly responses
		result.Data = ms.processCursorsForMCP(ctx, res.Data)
		for _, e := range res.Errors {
			result.Errors = append(result.Errors, ms.errorInfoFromCoreError(query, e, "execute_graphql"))
		}
	}
	return ms.toolResultJSON("execute_graphql", args, result)
}

// handleExecuteSavedQuery executes a saved query by name
func (ms *mcpServer) handleExecuteSavedQuery(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	name, _ := args["name"].(string)
	namespace, _ := args["namespace"].(string)

	if name == "" {
		return mcp.NewToolResultError("query name is required"), nil
	}

	// Convert variables map to JSON
	var varsJSON json.RawMessage
	if vars, ok := args["variables"].(map[string]any); ok && len(vars) > 0 {
		// Expand cursor IDs to full encrypted cursors
		expandedVars, err := ms.expandCursorIDs(ctx, vars)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("cursor lookup failed: %v", err)), nil
		}
		varsJSON, err = json.Marshal(expandedVars)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid variables: %v", err)), nil
		}
	}

	var rc core.RequestConfig
	if namespace != "" {
		rc.SetNamespace(namespace)
	} else {
		rc.SetNamespace(ms.getNamespace())
	}

	if err := ms.service.checkGraphJinInitialized(); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	res, err := ms.service.gj.GraphQLByName(ctx, name, varsJSON, &rc)

	result := ExecuteResult{}
	if err != nil {
		result.Errors = []ErrorInfo{ms.errorInfo("", err.Error(), "execute_saved_query")}
	} else {
		// Replace encrypted cursors with short numeric IDs for LLM-friendly responses
		result.Data = ms.processCursorsForMCP(ctx, res.Data)
		for _, e := range res.Errors {
			result.Errors = append(result.Errors, ms.errorInfoFromCoreError("", e, "execute_saved_query"))
		}
	}
	return ms.toolResultJSON("execute_saved_query", args, result)
}

// handleExecuteWorkflow executes a named JS workflow from ./workflows.
func (ms *mcpServer) handleExecuteWorkflow(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if !ms.service.conf.MCP.AllowWorkflowExecution {
		return mcp.NewToolResultError("workflow execution is not allowed. Enable allow_workflow_execution in MCP config."), nil
	}

	args := req.GetArguments()
	name, _ := args["name"].(string)
	namespace, _ := args["namespace"].(string)

	if name == "" {
		return mcp.NewToolResultError("workflow name is required"), nil
	}

	input := map[string]any{}
	if vars, ok := args["variables"].(map[string]any); ok {
		input = vars
	}

	ns := namespace
	if ns == "" {
		ns = ms.getNamespace()
	}

	row, err := newControlPlaneGraphQL(ms.service).runWorkflow(ctx, core.ManagedMutationRoot{
		Input: map[string]interface{}{
			"workflow_name": name,
			"variables":     input,
			"namespace":     ns,
		},
	})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if status, _ := row["status"].(string); status == "error" {
		return mcp.NewToolResultError(fmt.Sprint(row["error"])), nil
	}
	var out any
	if raw, _ := row["result_json"].(string); raw != "" {
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
	}

	return ms.toolResultJSON("execute_workflow", args, map[string]any{"data": out})
}

// isMutation checks if a query is a mutation (simple heuristic)
func isMutation(query string) bool {
	// Quick check - look for mutation keyword at the start, skipping whitespace and comments
	i := 0
	for i < len(query) {
		c := query[i]
		// Skip whitespace
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			i++
			continue
		}
		// Skip # comment lines
		if c == '#' {
			for i < len(query) && query[i] != '\n' {
				i++
			}
			continue
		}
		// Check if starts with "mutation" (case-insensitive)
		if len(query) >= i+8 {
			word := query[i : i+8]
			return strings.EqualFold(word, "mutation")
		}
		// If it starts with anything else (or too short), it's a query
		return false
	}
	return false
}
