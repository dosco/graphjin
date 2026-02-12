package serv

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
)

// registerExploreTools registers the explore_relationships tool
func (ms *mcpServer) registerExploreTools() {
	ms.srv.AddTool(mcp.NewTool(
		"explore_relationships",
		mcp.WithDescription("Map out the data model neighborhood around a table. "+
			"Given a table and depth (1-3), returns ALL reachable tables as a graph with nodes and typed edges. "+
			"Use this to understand how tables connect before writing complex nested queries."),
		mcp.WithString("table",
			mcp.Required(),
			mcp.Description("Center table name to explore from"),
		),
		mcp.WithNumber("depth",
			mcp.Description("How many hops to explore (1-3, default 2). Higher depth = more tables but slower."),
		),
		mcp.WithString("database",
			mcp.Description("Optional database name for multi-database deployments"),
		),
	), ms.handleExploreRelationships)
}

// handleExploreRelationships returns the relationship graph around a table
func (ms *mcpServer) handleExploreRelationships(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if ms.service.gj == nil {
		return mcp.NewToolResultError("GraphJin not initialized - no database connection configured"), nil
	}

	args := req.GetArguments()
	table, _ := args["table"].(string)
	database, _ := args["database"].(string)

	if table == "" {
		return mcp.NewToolResultError("table name is required"), nil
	}

	// JSON numbers come as float64
	depth := 2
	if d, ok := args["depth"].(float64); ok {
		depth = int(d)
	}

	var graph interface{}
	var err error
	if database != "" {
		graph, err = ms.service.gj.ExploreRelationshipsForDatabase(database, table, depth)
	} else {
		graph, err = ms.service.gj.ExploreRelationships(table, depth)
	}
	if err != nil {
		return mcp.NewToolResultError(enhanceError(err.Error(), "explore_relationships")), nil
	}

	data, err := mcpMarshalJSON(graph, true)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}
