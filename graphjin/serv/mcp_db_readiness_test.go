package serv

import (
	"context"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestSearchAndFragmentHandlers_RequireDB(t *testing.T) {
	type testCase struct {
		name string
		call func(ms *mcpServer) (*mcp.CallToolResult, error)
	}

	testCases := []testCase{
		{
			name: "list_saved_queries",
			call: func(ms *mcpServer) (*mcp.CallToolResult, error) {
				return ms.handleListSavedQueries(context.Background(), newToolRequest(map[string]any{}))
			},
		},
		{
			name: "search_saved_queries",
			call: func(ms *mcpServer) (*mcp.CallToolResult, error) {
				return ms.handleSearchSavedQueries(context.Background(), newToolRequest(map[string]any{"query": "users"}))
			},
		},
		{
			name: "get_saved_query",
			call: func(ms *mcpServer) (*mcp.CallToolResult, error) {
				return ms.handleGetSavedQuery(context.Background(), newToolRequest(map[string]any{"name": "users_by_id"}))
			},
		},
		{
			name: "list_fragments",
			call: func(ms *mcpServer) (*mcp.CallToolResult, error) {
				return ms.handleListFragments(context.Background(), newToolRequest(map[string]any{}))
			},
		},
		{
			name: "search_fragments",
			call: func(ms *mcpServer) (*mcp.CallToolResult, error) {
				return ms.handleSearchFragments(context.Background(), newToolRequest(map[string]any{"query": "user"}))
			},
		},
		{
			name: "get_fragment",
			call: func(ms *mcpServer) (*mcp.CallToolResult, error) {
				return ms.handleGetFragment(context.Background(), newToolRequest(map[string]any{"name": "user_fields"}))
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ms := mockMcpServerWithConfig(MCPConfig{EnableSearch: true})
			result, err := tc.call(ms)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			assertToolError(t, result, "No databases have been configured yet")
		})
	}
}

func TestPromptHandlers_RequireDB(t *testing.T) {
	testCases := []struct {
		name string
		call func(ms *mcpServer) error
	}{
		{
			name: "write_where_clause",
			call: func(ms *mcpServer) error {
				_, err := ms.handleWriteWhereClause(context.Background(), mcp.GetPromptRequest{
					Params: mcp.GetPromptParams{
						Arguments: map[string]string{
							"table":  "users",
							"intent": "active users",
						},
					},
				})
				return err
			},
		},
		{
			name: "write_query",
			call: func(ms *mcpServer) error {
				_, err := ms.handleWriteQuery(context.Background(), mcp.GetPromptRequest{
					Params: mcp.GetPromptParams{
						Arguments: map[string]string{
							"table": "users",
						},
					},
				})
				return err
			},
		},
		{
			name: "write_mutation",
			call: func(ms *mcpServer) error {
				_, err := ms.handleWriteMutation(context.Background(), mcp.GetPromptRequest{
					Params: mcp.GetPromptParams{
						Arguments: map[string]string{
							"operation": "insert",
							"table":     "users",
						},
					},
				})
				return err
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ms := mockMcpServerWithConfig(MCPConfig{})
			err := tc.call(ms)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), "No databases have been configured yet") {
				t.Fatalf("expected no-db error, got: %v", err)
			}
		})
	}
}
