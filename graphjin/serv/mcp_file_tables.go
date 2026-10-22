package serv

import (
	"context"
	"fmt"

	gjagent "github.com/dosco/graphjin/agent/v3"

	"github.com/dosco/graphjin/serv/v3/internal/mcpcompat/mcp"
)

func (ms *mcpServer) registerFileTableTools() {
	if ms.service.fileContentReader() == nil {
		return
	}
	ms.srv.AddTool(mcp.NewTool(
		"query_file_tables",
		mcp.WithDescription(gjagent.QueryFileTablesDescription),
		mcp.WithString("source", mcp.Required(), mcp.Description("File source name, the same as its table name")),
		mcp.WithString("sql", mcp.Required(), mcp.Description("One SELECT or WITH query naming the file tables it reads")),
	), ms.handleQueryFileTables)
}

func (ms *mcpServer) handleQueryFileTables(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx = ms.effectiveContext(ctx)
	args := req.GetArguments()
	source, _ := args["source"].(string)
	query, _ := args["sql"].(string)
	if source == "" || query == "" {
		return mcp.NewToolResultError("source and sql are required"), nil
	}
	res, err := ms.service.queryFileTables(ctx, source, query)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("query_file_tables: %v", err)), nil
	}
	return ms.toolResultJSON("query_file_tables", args, res)
}
