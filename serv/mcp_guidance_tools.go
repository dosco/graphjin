package serv

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

type guidanceToolResult struct {
	Title         string `json:"title"`
	GuideMarkdown string `json:"guide_markdown"`
}

// registerGuidanceTools registers tool twins for the highest-value prompt templates.
func (ms *mcpServer) registerGuidanceTools() {
	ms.srv.AddTool(mcp.NewTool(
		"write_query",
		mcp.WithDescription("Generate a complete GraphJin query guide as a tool result. "+
			"Use this when the MCP client does not support prompts well and you need a starter query, schema context, and follow-up steps."),
		mcp.WithString("table",
			mcp.Required(),
			mcp.Description("Primary table to query"),
		),
		mcp.WithString("fields",
			mcp.Description("Fields to select (for example: 'id, name, price' or 'all')"),
		),
		mcp.WithString("relationships",
			mcp.Description("Related tables to include (for example: 'owner, categories')"),
		),
		mcp.WithString("filter_intent",
			mcp.Description("What to filter (for example: 'active products over $50')"),
		),
		mcp.WithString("pagination",
			mcp.Description("Pagination style: 'limit' for limit/offset, 'cursor' for cursor-based"),
		),
		mcp.WithString("database",
			mcp.Description("Optional database name for multi-database deployments"),
		),
	), ms.handleWriteQueryTool)

	ms.srv.AddTool(mcp.NewTool(
		"write_mutation",
		mcp.WithDescription("Generate a GraphJin mutation guide as a tool result. "+
			"Use this when the MCP client does not support prompts well and you need an insert/update/upsert/delete starter."),
		mcp.WithString("operation",
			mcp.Required(),
			mcp.Description("Mutation type: insert, update, upsert, or delete"),
		),
		mcp.WithString("table",
			mcp.Required(),
			mcp.Description("Table to modify"),
		),
		mcp.WithString("data_intent",
			mcp.Description("What data to modify (for example: 'create user with email and name')"),
		),
		mcp.WithString("nested",
			mcp.Description("Related records to create/connect (for example: 'create order with products')"),
		),
		mcp.WithString("database",
			mcp.Description("Optional database name for multi-database deployments"),
		),
	), ms.handleWriteMutationTool)

	ms.srv.AddTool(mcp.NewTool(
		"fix_query_error",
		mcp.WithDescription("Analyze a GraphJin query error as a tool result. "+
			"Use this when the MCP client does not support prompts well and you need structured repair guidance."),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("The query that produced the error"),
		),
		mcp.WithString("error",
			mcp.Required(),
			mcp.Description("The error message received"),
		),
	), ms.handleFixQueryErrorTool)
}

func promptRequestFromToolArgs(args map[string]any) (mcp.GetPromptRequest, error) {
	out := make(map[string]string, len(args))
	for key, val := range args {
		if val == nil {
			continue
		}
		s, ok := val.(string)
		if !ok {
			return mcp.GetPromptRequest{}, fmt.Errorf("%s must be a string", key)
		}
		if strings.TrimSpace(s) == "" {
			continue
		}
		out[key] = s
	}

	return mcp.GetPromptRequest{
		Params: mcp.GetPromptParams{
			Arguments: out,
		},
	}, nil
}

func promptResultText(res *mcp.GetPromptResult) string {
	if res == nil {
		return ""
	}

	var sb strings.Builder
	for _, msg := range res.Messages {
		text, ok := msg.Content.(mcp.TextContent)
		if !ok {
			continue
		}
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(text.Text)
	}
	return sb.String()
}

func (ms *mcpServer) guidanceToolResult(tool string, args map[string]any, res *mcp.GetPromptResult) (*mcp.CallToolResult, error) {
	return ms.toolResultJSON(tool, args, guidanceToolResult{
		Title:         res.Description,
		GuideMarkdown: promptResultText(res),
	})
}

func (ms *mcpServer) handleWriteQueryTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	promptReq, err := promptRequestFromToolArgs(req.GetArguments())
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	res, err := ms.handleWriteQuery(ctx, promptReq)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return ms.guidanceToolResult("write_query", req.GetArguments(), res)
}

func (ms *mcpServer) handleWriteMutationTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	promptReq, err := promptRequestFromToolArgs(req.GetArguments())
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	res, err := ms.handleWriteMutation(ctx, promptReq)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return ms.guidanceToolResult("write_mutation", req.GetArguments(), res)
}

func (ms *mcpServer) handleFixQueryErrorTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	promptReq, err := promptRequestFromToolArgs(req.GetArguments())
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	res, err := ms.handleFixQueryError(ctx, promptReq)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return ms.guidanceToolResult("fix_query_error", req.GetArguments(), res)
}
