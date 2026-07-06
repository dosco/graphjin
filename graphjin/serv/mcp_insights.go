package serv

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
)

// registerInsightsTools is retained as an inert compatibility hook.
func (ms *mcpServer) registerInsightsTools() {
	return

	ms.srv.AddTool(mcp.NewTool(
		"get_schema_insights",
		mcp.WithDescription("Get database insights for schema understanding: hub tables (ranked by FK count), relationship paths between hubs, auto-generated query templates (time-series / breakdown / join), duplicate-table warnings, and data-quality flags. Use after list_tables to orient on the data model before writing queries."),
		mcp.WithString("database",
			mcp.Description("Optional database name. Omit to use the default database."),
		),
		mcp.WithOutputSchema[SchemaInsights](),
	), ms.handleGetSchemaInsights)
}

func (ms *mcpServer) handleGetSchemaInsights(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := ms.requireDB(); err != nil {
		return err, nil
	}
	args := req.GetArguments()
	database, _ := args["database"].(string)
	if database == "" {
		database = ms.service.gj.DefaultDatabase()
	}
	if ms.service.disc == nil {
		return mcp.NewToolResultError("Discovery not available yet."), nil
	}
	insights := ms.service.disc.Insights(database)
	return ms.toolResultJSON("get_schema_insights", args, insights)
}
