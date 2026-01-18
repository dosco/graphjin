package serv

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dosco/graphjin/core/v3"
	"github.com/mark3labs/mcp-go/mcp"
)

// registerSchemaTools registers the schema discovery tools
func (ms *mcpServer) registerSchemaTools() {
	// list_tables - List all database tables
	ms.srv.AddTool(mcp.NewTool(
		"list_tables",
		mcp.WithDescription("List all database tables available for querying. Returns table names, types, and column counts."),
		mcp.WithString("namespace",
			mcp.Description("Optional namespace for multi-tenant deployments"),
		),
	), ms.handleListTables)

	// describe_table - Get detailed table schema with relationships
	ms.srv.AddTool(mcp.NewTool(
		"describe_table",
		mcp.WithDescription("Get detailed schema information for a table, including columns, types, and relationships (both incoming and outgoing)."),
		mcp.WithString("table",
			mcp.Required(),
			mcp.Description("Name of the table to describe"),
		),
		mcp.WithString("namespace",
			mcp.Description("Optional namespace for multi-tenant deployments"),
		),
	), ms.handleDescribeTable)

	// find_path - Find relationship path between tables
	ms.srv.AddTool(mcp.NewTool(
		"find_path",
		mcp.WithDescription("Find the relationship path between two tables. Useful for understanding how to join tables in queries."),
		mcp.WithString("from_table",
			mcp.Required(),
			mcp.Description("Starting table name"),
		),
		mcp.WithString("to_table",
			mcp.Required(),
			mcp.Description("Target table name"),
		),
	), ms.handleFindPath)

	// validate_graphql - Validate a query without executing
	ms.srv.AddTool(mcp.NewTool(
		"validate_graphql",
		mcp.WithDescription("Validate a GraphJin GraphQL query without executing it. Returns validation errors or the generated SQL if valid."),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("The GraphQL query to validate"),
		),
	), ms.handleValidateGraphQL)

	// explain_graphql - Show generated SQL for a query
	ms.srv.AddTool(mcp.NewTool(
		"explain_graphql",
		mcp.WithDescription("Show the SQL that would be generated for a GraphQL query. Useful for understanding query execution."),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("The GraphQL query to explain"),
		),
		mcp.WithObject("variables",
			mcp.Description("Optional query variables"),
		),
	), ms.handleExplainGraphQL)
}

// handleListTables returns all available tables
func (ms *mcpServer) handleListTables(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	tables := ms.service.gj.GetTables()

	result := struct {
		Tables []core.TableInfo `json:"tables"`
		Count  int              `json:"count"`
	}{
		Tables: tables,
		Count:  len(tables),
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

// handleDescribeTable returns detailed schema for a table
func (ms *mcpServer) handleDescribeTable(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	table, _ := args["table"].(string)

	if table == "" {
		return mcp.NewToolResultError("table name is required"), nil
	}

	schema, err := ms.service.gj.GetTableSchema(table)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	data, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

// handleFindPath finds the relationship path between two tables
func (ms *mcpServer) handleFindPath(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	fromTable, _ := args["from_table"].(string)
	toTable, _ := args["to_table"].(string)

	if fromTable == "" || toTable == "" {
		return mcp.NewToolResultError("both from_table and to_table are required"), nil
	}

	path, err := ms.service.gj.FindRelationshipPath(fromTable, toTable)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	// Generate an example query
	exampleQuery := generatePathExampleQuery(fromTable, toTable, path)

	result := struct {
		Path         []core.PathStep `json:"path"`
		ExampleQuery string          `json:"example_query"`
	}{
		Path:         path,
		ExampleQuery: exampleQuery,
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

// generatePathExampleQuery generates an example GraphQL query based on the path
func generatePathExampleQuery(from, to string, path []core.PathStep) string {
	if len(path) == 0 {
		return ""
	}

	// Simple nested query structure
	query := "{ " + from + " { id "
	for _, step := range path {
		query += step.To + " { id "
	}

	// Close all the braces
	for range path {
		query += "} "
	}
	query += "} }"

	return query
}

// handleValidateGraphQL validates a query without executing
func (ms *mcpServer) handleValidateGraphQL(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	query, _ := args["query"].(string)

	if query == "" {
		return mcp.NewToolResultError("query is required"), nil
	}

	// Execute with empty variables to validate
	var rc core.RequestConfig
	rc.SetNamespace(ms.getNamespace())

	res, err := ms.service.gj.GraphQL(ctx, query, nil, &rc)

	result := struct {
		Valid  bool     `json:"valid"`
		Errors []string `json:"errors,omitempty"`
		SQL    string   `json:"sql,omitempty"`
	}{}

	if err != nil {
		result.Valid = false
		result.Errors = []string{err.Error()}
	} else if len(res.Errors) > 0 {
		result.Valid = false
		for _, e := range res.Errors {
			result.Errors = append(result.Errors, e.Message)
		}
	} else {
		result.Valid = true
		result.SQL = res.SQL()
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

// handleExplainGraphQL shows the SQL generated for a query
func (ms *mcpServer) handleExplainGraphQL(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	query, _ := args["query"].(string)

	if query == "" {
		return mcp.NewToolResultError("query is required"), nil
	}

	// Convert variables map to JSON
	var varsJSON json.RawMessage
	if vars, ok := args["variables"].(map[string]any); ok && len(vars) > 0 {
		var err error
		varsJSON, err = json.Marshal(vars)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid variables: %v", err)), nil
		}
	}

	var rc core.RequestConfig
	rc.SetNamespace(ms.getNamespace())

	res, err := ms.service.gj.GraphQL(ctx, query, varsJSON, &rc)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("query error: %v", err)), nil
	}

	if len(res.Errors) > 0 {
		errMsgs := make([]string, len(res.Errors))
		for i, e := range res.Errors {
			errMsgs[i] = e.Message
		}
		return mcp.NewToolResultError(fmt.Sprintf("query errors: %v", errMsgs)), nil
	}

	// Convert operation type to string
	opStr := "unknown"
	switch res.Operation() {
	case core.OpQuery:
		opStr = "query"
	case core.OpMutation:
		opStr = "mutation"
	}

	result := struct {
		SQL          string `json:"sql"`
		Operation    string `json:"operation"`
		CacheControl string `json:"cache_control,omitempty"`
	}{
		SQL:          res.SQL(),
		Operation:    opStr,
		CacheControl: res.CacheControl(),
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

// getNamespace returns the configured namespace
func (ms *mcpServer) getNamespace() string {
	if ms.service.namespace != nil {
		return *ms.service.namespace
	}
	return ""
}
