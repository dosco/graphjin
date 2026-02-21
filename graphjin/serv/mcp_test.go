package serv

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dosco/graphjin/core/v3"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/spf13/viper"
	"go.uber.org/zap/zaptest"
)

// mockMcpServerWithConfig creates a mock with custom MCP config
func mockMcpServerWithConfig(cfg MCPConfig) *mcpServer {
	svc := &graphjinService{
		cursorCache: NewMemoryCursorCache(100, time.Hour),
		conf: &Config{
			Serv: Serv{MCP: cfg},
		},
	}
	return &mcpServer{service: svc, ctx: context.Background()}
}

// newToolRequest builds a CallToolRequest with the given arguments
func newToolRequest(args map[string]any) mcp.CallToolRequest {
	return mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: args,
		},
	}
}

// assertToolError asserts that the result is an error containing the given substring
func assertToolError(t *testing.T, result *mcp.CallToolResult, contains string) {
	t.Helper()
	if result == nil {
		t.Fatal("Expected error result, got nil")
	}
	if !result.IsError {
		t.Fatalf("Expected error result, got success")
	}
	if len(result.Content) == 0 {
		t.Fatal("Expected error content, got empty")
	}
	textContent, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("Expected TextContent, got %T", result.Content[0])
	}
	if !strings.Contains(textContent.Text, contains) {
		t.Errorf("Expected error containing %q, got %q", contains, textContent.Text)
	}
}

// assertToolSuccess asserts that the result is a success and returns the text content
//
//nolint:unused
func assertToolSuccess(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if result == nil {
		t.Fatal("Expected success result, got nil")
	}
	if result.IsError {
		if len(result.Content) > 0 {
			if tc, ok := result.Content[0].(mcp.TextContent); ok {
				t.Fatalf("Expected success, got error: %s", tc.Text)
			}
		}
		t.Fatal("Expected success, got error")
	}
	if len(result.Content) == 0 {
		t.Fatal("Expected content, got empty")
	}
	textContent, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("Expected TextContent, got %T", result.Content[0])
	}
	return textContent.Text
}

// =============================================================================
// Execution Handler Tests
// =============================================================================

func TestHandleExecuteGraphQL_RawQueriesDisabled(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{
		AllowRawQueries: false,
		AllowMutations:  true,
	})
	ctx := context.Background()

	req := newToolRequest(map[string]any{
		"query": "{ users { id name } }",
	})

	result, err := ms.handleExecuteGraphQL(ctx, req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	assertToolError(t, result, "raw queries are not allowed")
}

func TestHandleExecuteGraphQL_MissingQuery(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{
		AllowRawQueries: true,
		AllowMutations:  true,
	})
	ctx := context.Background()

	req := newToolRequest(map[string]any{
		// No query provided
	})

	result, err := ms.handleExecuteGraphQL(ctx, req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	assertToolError(t, result, "query is required")
}

func TestHandleExecuteGraphQL_EmptyQuery(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{
		AllowRawQueries: true,
		AllowMutations:  true,
	})
	ctx := context.Background()

	req := newToolRequest(map[string]any{
		"query": "",
	})

	result, err := ms.handleExecuteGraphQL(ctx, req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	assertToolError(t, result, "query is required")
}

func TestHandleExecuteGraphQL_MutationBlocked(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{
		AllowRawQueries: true,
		AllowMutations:  false,
	})
	ctx := context.Background()

	req := newToolRequest(map[string]any{
		"query": "mutation { createUser(input: {name: \"test\"}) { id } }",
	})

	result, err := ms.handleExecuteGraphQL(ctx, req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	assertToolError(t, result, "mutations are not allowed")
}

func TestHandleExecuteGraphQL_InvalidCursorID(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{
		AllowRawQueries: true,
		AllowMutations:  true,
	})
	ctx := context.Background()

	req := newToolRequest(map[string]any{
		"query": "{ users { id } }",
		"variables": map[string]any{
			"cursor": "999", // Non-existent cursor ID
		},
	})

	result, err := ms.handleExecuteGraphQL(ctx, req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	assertToolError(t, result, "cursor lookup failed")
}

func TestHandleExecuteSavedQuery_MissingName(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{
		AllowRawQueries: true,
		AllowMutations:  true,
	})
	ctx := context.Background()

	req := newToolRequest(map[string]any{
		// No name provided
	})

	result, err := ms.handleExecuteSavedQuery(ctx, req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	assertToolError(t, result, "query name is required")
}

func TestHandleExecuteSavedQuery_EmptyName(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{
		AllowRawQueries: true,
		AllowMutations:  true,
	})
	ctx := context.Background()

	req := newToolRequest(map[string]any{
		"name": "",
	})

	result, err := ms.handleExecuteSavedQuery(ctx, req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	assertToolError(t, result, "query name is required")
}

func TestHandleExecuteSavedQuery_InvalidCursorID(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{
		AllowRawQueries: true,
		AllowMutations:  true,
	})
	ctx := context.Background()

	req := newToolRequest(map[string]any{
		"name": "getUsers",
		"variables": map[string]any{
			"after": "999", // Non-existent cursor ID
		},
	})

	result, err := ms.handleExecuteSavedQuery(ctx, req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	assertToolError(t, result, "cursor lookup failed")
}

// =============================================================================
// Cursor Integration Tests
// =============================================================================

func TestMCP_CursorRoundtripIntegration(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{
		AllowRawQueries: true,
		AllowMutations:  true,
	})
	ctx := context.Background()

	// Simulate response with encrypted cursor
	responseData := json.RawMessage(`{
		"users": [{"id": 1}],
		"users_cursor": "__gj-enc:encrypted-cursor-abc123"
	}`)

	// Process response - should replace cursor with numeric ID
	processed := ms.processCursorsForMCP(ctx, responseData)

	var resp map[string]any
	if err := json.Unmarshal(processed, &resp); err != nil {
		t.Fatalf("Failed to unmarshal processed response: %v", err)
	}
	cursorID := resp["users_cursor"].(string)

	// Verify it's a numeric ID
	if cursorID == "__gj-enc:encrypted-cursor-abc123" {
		t.Error("Expected numeric ID, got encrypted cursor")
	}

	// Now use that ID in a subsequent request
	vars := map[string]any{"users_cursor": cursorID}
	expanded, err := ms.expandCursorIDs(ctx, vars)
	if err != nil {
		t.Fatalf("Cursor expansion failed: %v", err)
	}

	// Should get back the original encrypted cursor
	if expanded["users_cursor"] != "__gj-enc:encrypted-cursor-abc123" {
		t.Errorf("Cursor roundtrip failed: expected encrypted cursor, got %v", expanded["users_cursor"])
	}
}

func TestMCP_CursorExpansionInVariables(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{
		AllowRawQueries: true,
		AllowMutations:  true,
	})
	ctx := context.Background()

	// Store a cursor first
	encryptedCursor := "__gj-enc:my-encrypted-cursor-xyz"
	id, err := ms.service.cursorCache.Set(ctx, encryptedCursor)
	if err != nil {
		t.Fatalf("Failed to set cursor: %v", err)
	}

	// Create variables with numeric cursor ID
	vars := map[string]any{
		"cursor": "1", // numeric ID as string
		"limit":  10,
		"name":   "test",
	}

	expanded, err := ms.expandCursorIDs(ctx, vars)
	if err != nil {
		t.Fatalf("expandCursorIDs failed: %v", err)
	}

	// Cursor should be expanded
	if expanded["cursor"] != encryptedCursor {
		t.Errorf("Expected cursor to be expanded to %q, got %q", encryptedCursor, expanded["cursor"])
	}

	// Non-cursor variables should be unchanged
	if expanded["limit"] != 10 {
		t.Error("Non-cursor variable 'limit' should be unchanged")
	}
	if expanded["name"] != "test" {
		t.Error("Non-cursor variable 'name' should be unchanged")
	}

	_ = id
}

func TestMCP_CursorProcessingInResponse(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{
		AllowRawQueries: true,
		AllowMutations:  true,
	})
	ctx := context.Background()

	// Test data with multiple cursors at different levels
	input := json.RawMessage(`{
		"products": [{"id": 1}],
		"products_cursor": "__gj-enc:products-cursor-value",
		"nested": {
			"orders": [{"id": 2}],
			"orders_cursor": "__gj-enc:orders-cursor-value"
		}
	}`)

	result := ms.processCursorsForMCP(ctx, input)

	var parsed map[string]any
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("Failed to parse result: %v", err)
	}

	// Check top-level cursor was replaced
	productsCursor, ok := parsed["products_cursor"].(string)
	if !ok {
		t.Fatal("products_cursor not found")
	}
	if productsCursor == "__gj-enc:products-cursor-value" {
		t.Error("products_cursor should have been replaced with numeric ID")
	}

	// Check nested cursor was replaced
	nested, ok := parsed["nested"].(map[string]any)
	if !ok {
		t.Fatal("nested not found")
	}
	ordersCursor, ok := nested["orders_cursor"].(string)
	if !ok {
		t.Fatal("orders_cursor not found")
	}
	if ordersCursor == "__gj-enc:orders-cursor-value" {
		t.Error("orders_cursor should have been replaced with numeric ID")
	}

	// Cursors should be different numeric IDs
	if productsCursor == ordersCursor {
		t.Error("Expected different cursor IDs for different cursors")
	}
}

func TestMCP_InvalidCursorReturnsError(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{
		AllowRawQueries: true,
		AllowMutations:  true,
	})
	ctx := context.Background()

	// Try to expand a non-existent cursor ID
	vars := map[string]any{
		"cursor": "99999", // Invalid/expired cursor ID
	}

	_, err := ms.expandCursorIDs(ctx, vars)
	if err == nil {
		t.Error("Expected error for non-existent cursor ID")
	}

	if !strings.Contains(err.Error(), "invalid cursor ID") {
		t.Errorf("Expected 'invalid cursor ID' error, got: %v", err)
	}
}

func TestMCP_CursorDeduplication(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{
		AllowRawQueries: true,
		AllowMutations:  true,
	})
	ctx := context.Background()

	// Process same cursor twice
	input1 := json.RawMessage(`{"users_cursor": "__gj-enc:same-cursor-value"}`)
	input2 := json.RawMessage(`{"users_cursor": "__gj-enc:same-cursor-value"}`)

	result1 := ms.processCursorsForMCP(ctx, input1)
	result2 := ms.processCursorsForMCP(ctx, input2)

	var parsed1, parsed2 map[string]any
	json.Unmarshal(result1, &parsed1) //nolint:errcheck
	json.Unmarshal(result2, &parsed2) //nolint:errcheck

	cursor1 := parsed1["users_cursor"].(string)
	cursor2 := parsed2["users_cursor"].(string)

	// Same encrypted cursor should map to same ID (deduplication)
	if cursor1 != cursor2 {
		t.Errorf("Expected same cursor ID for duplicate cursors, got %q and %q", cursor1, cursor2)
	}
}

// =============================================================================
// Configuration Tests
// =============================================================================

func TestMCP_DefaultConfigAllowsRawQueries(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{
		AllowRawQueries: true,
		AllowMutations:  true,
	})

	// Just verify the config is set correctly
	if !ms.service.conf.MCP.AllowRawQueries {
		t.Error("Expected AllowRawQueries to be true")
	}
	if !ms.service.conf.MCP.AllowMutations {
		t.Error("Expected AllowMutations to be true")
	}
}

func TestMCP_DisabledFeaturesReturnErrors(t *testing.T) {
	ctx := context.Background()

	testCases := []struct {
		name        string
		cfg         MCPConfig
		query       string
		expectError string
	}{
		{
			name:        "raw queries disabled",
			cfg:         MCPConfig{AllowRawQueries: false, AllowMutations: true},
			query:       "{ users { id } }",
			expectError: "raw queries are not allowed",
		},
		{
			name:        "mutations disabled",
			cfg:         MCPConfig{AllowRawQueries: true, AllowMutations: false},
			query:       "mutation { deleteUser(id: 1) { id } }",
			expectError: "mutations are not allowed",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ms := mockMcpServerWithConfig(tc.cfg)
			req := newToolRequest(map[string]any{"query": tc.query})

			result, err := ms.handleExecuteGraphQL(ctx, req)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			assertToolError(t, result, tc.expectError)
		})
	}
}

// =============================================================================
// isMutation Helper Function Tests
// =============================================================================

func TestIsMutation(t *testing.T) {
	testCases := []struct {
		query    string
		expected bool
	}{
		// Positive cases - mutations
		{"mutation { createUser { id } }", true},
		{"Mutation { createUser { id } }", true},
		{"MUTATION { createUser { id } }", true},
		{"mutation createUser { createUser { id } }", true},
		{"  mutation { createUser { id } }", true},
		{"\n\tmutation { createUser { id } }", true},
		{"\r\n  mutation { createUser { id } }", true},

		// Comment handling
		{"# comment\nmutation { createUser { id } }", true},
		{"# line1\n# line2\nmutation { createUser { id } }", true},
		{"# comment\n  mutation { createUser { id } }", true},
		{"# comment\nquery { users { id } }", false},

		// Case-insensitive
		{"mUtAtIoN { createUser { id } }", true},
		{"MuTaTiOn { createUser { id } }", true},

		// Negative cases - queries
		{"{ users { id } }", false},
		{"query { users { id } }", false},
		{"query GetUsers { users { id } }", false},
		{"{ mutation_field { id } }", false}, // field named mutation_field is a query
		{"", false},
		{"  ", false},
		{"q", false},
		{"que", false},
	}

	for _, tc := range testCases {
		t.Run(tc.query, func(t *testing.T) {
			result := isMutation(tc.query)
			if result != tc.expected {
				t.Errorf("isMutation(%q) = %v, expected %v", tc.query, result, tc.expected)
			}
		})
	}
}

// =============================================================================
// Edge Cases
// =============================================================================

func TestMCP_CursorCacheNotSet(t *testing.T) {
	// Create service without cursor cache
	svc := &graphjinService{
		cursorCache: nil,
		conf: &Config{
			Serv: Serv{MCP: MCPConfig{
				AllowRawQueries: true,
				AllowMutations:  true,
			}},
		},
	}
	ms := &mcpServer{service: svc, ctx: context.Background()}
	ctx := context.Background()

	// processCursorsForMCP should handle nil cache gracefully
	input := json.RawMessage(`{"users_cursor": "__gj-enc:test"}`)
	result := ms.processCursorsForMCP(ctx, input)

	// Should return input unchanged
	if string(result) != string(input) {
		t.Errorf("Expected unchanged input when cache is nil, got %s", result)
	}

	// expandCursorIDs should handle nil cache gracefully
	vars := map[string]any{"cursor": "1"}
	expanded, err := ms.expandCursorIDs(ctx, vars)
	if err != nil {
		t.Errorf("Expected no error when cache is nil, got %v", err)
	}
	if expanded["cursor"] != "1" {
		t.Error("Expected unchanged vars when cache is nil")
	}
}

func TestMCP_EmptyVariables(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{
		AllowRawQueries: true,
		AllowMutations:  true,
	})
	ctx := context.Background()

	// Empty variables should be handled gracefully
	vars := map[string]any{}
	expanded, err := ms.expandCursorIDs(ctx, vars)
	if err != nil {
		t.Errorf("Expected no error for empty vars, got %v", err)
	}
	if len(expanded) != 0 {
		t.Error("Expected empty result for empty vars")
	}

	// Nil variables
	nilVars := map[string]any(nil)
	expandedNil, err := ms.expandCursorIDs(ctx, nilVars)
	if err != nil {
		t.Errorf("Expected no error for nil vars, got %v", err)
	}
	if len(expandedNil) != 0 {
		t.Error("Expected empty result for nil vars")
	}
}

func TestMCP_ProcessInvalidJSON(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{
		AllowRawQueries: true,
		AllowMutations:  true,
	})
	ctx := context.Background()

	// Invalid JSON should be returned unchanged
	invalidJSON := json.RawMessage(`{invalid json}`)
	result := ms.processCursorsForMCP(ctx, invalidJSON)

	if string(result) != string(invalidJSON) {
		t.Errorf("Expected unchanged invalid JSON, got %s", result)
	}
}

func TestMCP_ProcessEmptyData(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{
		AllowRawQueries: true,
		AllowMutations:  true,
	})
	ctx := context.Background()

	// Empty data should be handled gracefully
	emptyData := json.RawMessage(``)
	result := ms.processCursorsForMCP(ctx, emptyData)

	if string(result) != string(emptyData) {
		t.Errorf("Expected unchanged empty data, got %s", result)
	}
}

func TestMCP_AlreadyEncryptedCursorUnchanged(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{
		AllowRawQueries: true,
		AllowMutations:  true,
	})
	ctx := context.Background()

	// Variables with already-encrypted cursor should be unchanged
	vars := map[string]any{
		"cursor": "__gj-enc:already-encrypted-value",
	}

	expanded, err := ms.expandCursorIDs(ctx, vars)
	if err != nil {
		t.Fatalf("expandCursorIDs failed: %v", err)
	}

	cursor, ok := expanded["cursor"].(string)
	if !ok {
		t.Fatal("cursor not found or not a string")
	}

	if cursor != "__gj-enc:already-encrypted-value" {
		t.Errorf("Expected unchanged encrypted cursor, got %q", cursor)
	}
}

func TestMCP_NonNumericCursorKeyUnchanged(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{
		AllowRawQueries: true,
		AllowMutations:  true,
	})
	ctx := context.Background()

	// Variables with non-numeric cursor value should be unchanged
	vars := map[string]any{
		"cursor": "not-a-number",
	}

	expanded, err := ms.expandCursorIDs(ctx, vars)
	if err != nil {
		t.Fatalf("expandCursorIDs failed: %v", err)
	}

	cursor, ok := expanded["cursor"].(string)
	if !ok {
		t.Fatal("cursor not found or not a string")
	}

	if cursor != "not-a-number" {
		t.Errorf("Expected unchanged non-numeric cursor, got %q", cursor)
	}
}

func TestMCP_ArrayInResponse(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{
		AllowRawQueries: true,
		AllowMutations:  true,
	})
	ctx := context.Background()

	// Test data with array containing objects with cursors
	input := json.RawMessage(`[
		{"users_cursor": "__gj-enc:cursor1"},
		{"users_cursor": "__gj-enc:cursor2"}
	]`)

	result := ms.processCursorsForMCP(ctx, input)

	var parsed []map[string]any
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("Failed to parse result: %v", err)
	}

	if len(parsed) != 2 {
		t.Fatalf("Expected 2 items, got %d", len(parsed))
	}

	cursor1 := parsed[0]["users_cursor"].(string)
	cursor2 := parsed[1]["users_cursor"].(string)

	// Both should be numeric IDs, not encrypted cursors
	if cursor1 == "__gj-enc:cursor1" {
		t.Error("First cursor should have been replaced")
	}
	if cursor2 == "__gj-enc:cursor2" {
		t.Error("Second cursor should have been replaced")
	}

	// They should be different IDs since cursors are different
	if cursor1 == cursor2 {
		t.Error("Expected different cursor IDs for different cursor values")
	}
}

func TestMCP_NonCursorKeyNotExpanded(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{
		AllowRawQueries: true,
		AllowMutations:  true,
	})
	ctx := context.Background()

	// Store a cursor
	_, err := ms.service.cursorCache.Set(ctx, "__gj-enc:test")
	if err != nil {
		t.Fatalf("Failed to set cursor: %v", err)
	}

	// Variables with numeric value but non-cursor key should be unchanged
	vars := map[string]any{
		"limit":  "1", // This is "1" but not a cursor key
		"offset": "2",
		"id":     "3",
	}

	expanded, err := ms.expandCursorIDs(ctx, vars)
	if err != nil {
		t.Fatalf("expandCursorIDs failed: %v", err)
	}

	// All should be unchanged
	if expanded["limit"] != "1" {
		t.Errorf("limit should be unchanged, got %v", expanded["limit"])
	}
	if expanded["offset"] != "2" {
		t.Errorf("offset should be unchanged, got %v", expanded["offset"])
	}
	if expanded["id"] != "3" {
		t.Errorf("id should be unchanged, got %v", expanded["id"])
	}
}

// =============================================================================
// Config Update Tests
// =============================================================================

func TestHandleUpdateCurrentConfig_NilGraphJin(t *testing.T) {
	// Test that update works even when gj is nil (no DB configured)
	logger := zaptest.NewLogger(t)
	ms := &mcpServer{
		service: &graphjinService{
			gj: nil, // Not initialized
			conf: &Config{
				Core: core.Config{},
				Serv: Serv{Production: false},
			},
			log: logger.Sugar(),
		},
		ctx: context.Background(),
	}

	req := newToolRequest(map[string]any{
		"tables": []any{
			map[string]any{"name": "users"},
		},
	})

	result, err := ms.handleUpdateCurrentConfig(context.Background(), req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Should succeed (with a warning about GraphJin not initialized)
	if result == nil {
		t.Fatal("Expected result, got nil")
	}

	// Parse result to verify
	if len(result.Content) > 0 {
		if tc, ok := result.Content[0].(mcp.TextContent); ok {
			// Should contain info about table being added
			if !strings.Contains(tc.Text, "users") {
				t.Errorf("Expected result to mention 'users', got: %s", tc.Text)
			}
		}
	}
}

func TestSyncConfigToViper(t *testing.T) {
	v := viper.New()
	logger := zaptest.NewLogger(t)
	ms := &mcpServer{
		service: &graphjinService{
			conf: &Config{
				Core: core.Config{
					Databases: map[string]core.DatabaseConfig{
						"main": {Type: "postgres", Host: "localhost"},
					},
					Tables: []core.Table{
						{Name: "users"},
					},
					Roles: []core.Role{
						{Name: "admin"},
					},
					Blocklist: []string{"secrets"},
					Functions: []core.Function{
						{Name: "my_func", ReturnType: "text"},
					},
					Resolvers: []core.ResolverConfig{
						{Name: "payments", Type: "remote_api", Table: "users"},
					},
				},
				viper: v,
			},
			log: logger.Sugar(),
		},
	}

	ms.syncConfigToViper(v)

	// Verify values were synced
	if v.Get("databases") == nil {
		t.Error("Expected databases to be set in viper")
	}
	if v.Get("tables") == nil {
		t.Error("Expected tables to be set in viper")
	}
	if v.Get("roles") == nil {
		t.Error("Expected roles to be set in viper")
	}
	if v.Get("blocklist") == nil {
		t.Error("Expected blocklist to be set in viper")
	}
	if v.Get("functions") == nil {
		t.Error("Expected functions to be set in viper")
	}
	if v.Get("resolvers") == nil {
		t.Error("Expected resolvers to be set in viper")
	}
}

func TestSyncConfigToViper_EmptyConfig(t *testing.T) {
	v := viper.New()
	logger := zaptest.NewLogger(t)
	ms := &mcpServer{
		service: &graphjinService{
			conf: &Config{
				Core:  core.Config{},
				viper: v,
			},
			log: logger.Sugar(),
		},
	}

	// Should not panic with empty config
	ms.syncConfigToViper(v)

	// Empty configs should not set values
	if v.Get("databases") != nil {
		t.Error("Expected databases to be nil for empty config")
	}
}

func TestSaveConfigToDisk_NoViper(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ms := &mcpServer{
		service: &graphjinService{
			conf: &Config{
				viper: nil, // No viper instance
			},
			log: logger.Sugar(),
		},
	}

	err := ms.saveConfigToDisk()
	if err == nil {
		t.Error("Expected error when viper is nil")
	}
	if !strings.Contains(err.Error(), "viper instance not available") {
		t.Errorf("Expected 'viper instance not available' error, got: %v", err)
	}
}

func TestAllowConfigUpdates_DefaultsToTrueInDevMode(t *testing.T) {
	tests := []struct {
		name          string
		production    bool
		mcpDisable    bool
		explicitlySet bool
		expectedValue bool
	}{
		{
			name:          "dev mode with MCP enabled - defaults to true",
			production:    false,
			mcpDisable:    false,
			explicitlySet: false,
			expectedValue: true,
		},
		{
			name:          "dev mode with MCP disabled - stays false",
			production:    false,
			mcpDisable:    true,
			explicitlySet: false,
			expectedValue: false,
		},
		{
			name:          "production mode - stays false",
			production:    true,
			mcpDisable:    false,
			explicitlySet: false,
			expectedValue: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			if tt.explicitlySet {
				v.Set("mcp.allow_config_updates", false)
			}

			conf := &Config{
				Serv: Serv{
					Production: tt.production,
					MCP: MCPConfig{
						Disable:            tt.mcpDisable,
						AllowConfigUpdates: false, // Start with false
					},
				},
				viper: v,
			}

			// Apply the logic from newGraphJinService
			if !conf.Serv.Production && !conf.MCP.Disable {
				if conf.viper != nil && !conf.viper.IsSet("mcp.allow_config_updates") {
					conf.MCP.AllowConfigUpdates = true
				}
			}

			if conf.MCP.AllowConfigUpdates != tt.expectedValue {
				t.Errorf("AllowConfigUpdates = %v, expected %v", conf.MCP.AllowConfigUpdates, tt.expectedValue)
			}
		})
	}
}

// =============================================================================
// Resolver Config Tests
// =============================================================================

func TestParseResolverConfig_FullConfig(t *testing.T) {
	input := map[string]any{
		"name":         "payments",
		"type":         "remote_api",
		"table":        "customers",
		"column":       "stripe_id",
		"schema":       "public",
		"strip_path":   "data",
		"url":          "http://payments-service/payments/$id",
		"debug":        true,
		"pass_headers": []any{"cookie", "authorization"},
		"set_headers": []any{
			map[string]any{"name": "Host", "value": "payments-service"},
			map[string]any{"name": "X-Api-Key", "value": "secret"},
		},
	}

	rc, err := parseResolverConfig(input)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if rc.Name != "payments" {
		t.Errorf("Expected name 'payments', got %q", rc.Name)
	}
	if rc.Type != "remote_api" {
		t.Errorf("Expected type 'remote_api', got %q", rc.Type)
	}
	if rc.Table != "customers" {
		t.Errorf("Expected table 'customers', got %q", rc.Table)
	}
	if rc.Column != "stripe_id" {
		t.Errorf("Expected column 'stripe_id', got %q", rc.Column)
	}
	if rc.Schema != "public" {
		t.Errorf("Expected schema 'public', got %q", rc.Schema)
	}
	if rc.StripPath != "data" {
		t.Errorf("Expected strip_path 'data', got %q", rc.StripPath)
	}

	// Verify Props
	if rc.Props == nil {
		t.Fatal("Expected Props to be set")
	}
	if rc.Props["url"] != "http://payments-service/payments/$id" {
		t.Errorf("Expected url in Props, got %v", rc.Props["url"])
	}
	if rc.Props["debug"] != true {
		t.Errorf("Expected debug=true in Props, got %v", rc.Props["debug"])
	}
	passHeaders, ok := rc.Props["pass_headers"].([]string)
	if !ok {
		t.Fatal("Expected pass_headers to be []string")
	}
	if len(passHeaders) != 2 || passHeaders[0] != "cookie" || passHeaders[1] != "authorization" {
		t.Errorf("Expected pass_headers [cookie, authorization], got %v", passHeaders)
	}
	setHeaders, ok := rc.Props["set_headers"].(map[string]string)
	if !ok {
		t.Fatal("Expected set_headers to be map[string]string")
	}
	if setHeaders["Host"] != "payments-service" {
		t.Errorf("Expected Host header 'payments-service', got %q", setHeaders["Host"])
	}
	if setHeaders["X-Api-Key"] != "secret" {
		t.Errorf("Expected X-Api-Key header 'secret', got %q", setHeaders["X-Api-Key"])
	}
}

func TestParseResolverConfig_MinimalConfig(t *testing.T) {
	input := map[string]any{
		"name":  "payments",
		"type":  "remote_api",
		"table": "customers",
	}

	rc, err := parseResolverConfig(input)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if rc.Name != "payments" {
		t.Errorf("Expected name 'payments', got %q", rc.Name)
	}
	if rc.Props != nil {
		t.Errorf("Expected nil Props for minimal config, got %v", rc.Props)
	}
}

func TestParseResolverConfig_MissingName(t *testing.T) {
	input := map[string]any{
		"type":  "remote_api",
		"table": "customers",
	}

	_, err := parseResolverConfig(input)
	if err == nil {
		t.Fatal("Expected error for missing name")
	}
	if !strings.Contains(err.Error(), "resolver name is required") {
		t.Errorf("Expected 'resolver name is required' error, got: %v", err)
	}
}

func TestParseResolverConfig_MissingType(t *testing.T) {
	input := map[string]any{
		"name":  "payments",
		"table": "customers",
	}

	_, err := parseResolverConfig(input)
	if err == nil {
		t.Fatal("Expected error for missing type")
	}
	if !strings.Contains(err.Error(), "resolver type is required") {
		t.Errorf("Expected 'resolver type is required' error, got: %v", err)
	}
}

func TestParseResolverConfig_InvalidType(t *testing.T) {
	input := map[string]any{
		"name":  "payments",
		"type":  "graphql",
		"table": "customers",
	}

	_, err := parseResolverConfig(input)
	if err == nil {
		t.Fatal("Expected error for invalid type")
	}
	if !strings.Contains(err.Error(), "invalid resolver type") {
		t.Errorf("Expected 'invalid resolver type' error, got: %v", err)
	}
}

func TestParseResolverConfig_MissingTable(t *testing.T) {
	input := map[string]any{
		"name": "payments",
		"type": "remote_api",
	}

	_, err := parseResolverConfig(input)
	if err == nil {
		t.Fatal("Expected error for missing table")
	}
	if !strings.Contains(err.Error(), "resolver table is required") {
		t.Errorf("Expected 'resolver table is required' error, got: %v", err)
	}
}

func TestParseResolverConfig_EmptySetHeaders(t *testing.T) {
	input := map[string]any{
		"name":        "payments",
		"type":        "remote_api",
		"table":       "customers",
		"set_headers": []any{},
	}

	rc, err := parseResolverConfig(input)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Empty set_headers should not be added to Props
	if rc.Props != nil {
		if _, exists := rc.Props["set_headers"]; exists {
			t.Error("Expected set_headers to be absent from Props when empty")
		}
	}
}

func TestHandleGetCurrentConfig_ResolversSection(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ms := &mcpServer{
		service: &graphjinService{
			conf: &Config{
				Core: core.Config{
					Resolvers: []core.ResolverConfig{
						{
							Name:  "payments",
							Type:  "remote_api",
							Table: "customers",
						},
					},
				},
			},
			log: logger.Sugar(),
		},
		ctx: context.Background(),
	}

	req := newToolRequest(map[string]any{
		"section": "resolvers",
	})

	result, err := ms.handleGetCurrentConfig(context.Background(), req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	text := assertToolSuccess(t, result)

	if !strings.Contains(text, "payments") {
		t.Errorf("Expected result to contain 'payments', got: %s", text)
	}
	if !strings.Contains(text, "remote_api") {
		t.Errorf("Expected result to contain 'remote_api', got: %s", text)
	}
}

func TestHandleGetCurrentConfig_AllIncludesResolvers(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ms := &mcpServer{
		service: &graphjinService{
			conf: &Config{
				Core: core.Config{
					Resolvers: []core.ResolverConfig{
						{
							Name:  "subscriptions",
							Type:  "remote_api",
							Table: "users",
						},
					},
				},
			},
			log: logger.Sugar(),
		},
		ctx: context.Background(),
	}

	req := newToolRequest(map[string]any{
		"section": "all",
	})

	result, err := ms.handleGetCurrentConfig(context.Background(), req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	text := assertToolSuccess(t, result)

	if !strings.Contains(text, "subscriptions") {
		t.Errorf("Expected 'all' section to include resolvers, got: %s", text)
	}
}

func TestHandleGetCurrentConfig_UnknownSectionListsResolvers(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ms := &mcpServer{
		service: &graphjinService{
			conf: &Config{
				Core: core.Config{},
			},
			log: logger.Sugar(),
		},
		ctx: context.Background(),
	}

	req := newToolRequest(map[string]any{
		"section": "invalid_section",
	})

	result, err := ms.handleGetCurrentConfig(context.Background(), req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	assertToolError(t, result, "resolvers")
}

func TestHandleUpdateCurrentConfig_AddResolver(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ms := &mcpServer{
		service: &graphjinService{
			gj: nil,
			conf: &Config{
				Core: core.Config{},
				Serv: Serv{Production: false},
			},
			log: logger.Sugar(),
		},
		ctx: context.Background(),
	}

	req := newToolRequest(map[string]any{
		"resolvers": []any{
			map[string]any{
				"name":  "payments",
				"type":  "remote_api",
				"table": "customers",
				"url":   "http://payments-api/payments/$id",
			},
		},
	})

	result, err := ms.handleUpdateCurrentConfig(context.Background(), req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	text := assertToolSuccess(t, result)
	if !strings.Contains(text, "added resolver: payments") {
		t.Errorf("Expected 'added resolver: payments' in result, got: %s", text)
	}

	// Verify resolver was actually added to config
	if len(ms.service.conf.Core.Resolvers) != 1 {
		t.Fatalf("Expected 1 resolver, got %d", len(ms.service.conf.Core.Resolvers))
	}
	if ms.service.conf.Core.Resolvers[0].Name != "payments" {
		t.Errorf("Expected resolver name 'payments', got %q", ms.service.conf.Core.Resolvers[0].Name)
	}
}

func TestHandleUpdateCurrentConfig_UpdateResolver(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ms := &mcpServer{
		service: &graphjinService{
			gj: nil,
			conf: &Config{
				Core: core.Config{
					Resolvers: []core.ResolverConfig{
						{
							Name:  "payments",
							Type:  "remote_api",
							Table: "customers",
						},
					},
				},
				Serv: Serv{Production: false},
			},
			log: logger.Sugar(),
		},
		ctx: context.Background(),
	}

	req := newToolRequest(map[string]any{
		"resolvers": []any{
			map[string]any{
				"name":  "payments",
				"type":  "remote_api",
				"table": "orders",
				"url":   "http://new-api/payments/$id",
			},
		},
	})

	result, err := ms.handleUpdateCurrentConfig(context.Background(), req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	text := assertToolSuccess(t, result)
	if !strings.Contains(text, "updated resolver: payments") {
		t.Errorf("Expected 'updated resolver: payments' in result, got: %s", text)
	}

	// Verify it was updated, not appended
	if len(ms.service.conf.Core.Resolvers) != 1 {
		t.Fatalf("Expected 1 resolver after update, got %d", len(ms.service.conf.Core.Resolvers))
	}
	if ms.service.conf.Core.Resolvers[0].Table != "orders" {
		t.Errorf("Expected table 'orders' after update, got %q", ms.service.conf.Core.Resolvers[0].Table)
	}
}

func TestHandleUpdateCurrentConfig_UpdateResolverCaseInsensitive(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ms := &mcpServer{
		service: &graphjinService{
			gj: nil,
			conf: &Config{
				Core: core.Config{
					Resolvers: []core.ResolverConfig{
						{Name: "Payments", Type: "remote_api", Table: "customers"},
					},
				},
				Serv: Serv{Production: false},
			},
			log: logger.Sugar(),
		},
		ctx: context.Background(),
	}

	req := newToolRequest(map[string]any{
		"resolvers": []any{
			map[string]any{
				"name":  "payments", // lowercase, should match "Payments"
				"type":  "remote_api",
				"table": "orders",
			},
		},
	})

	result, err := ms.handleUpdateCurrentConfig(context.Background(), req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	text := assertToolSuccess(t, result)
	if !strings.Contains(text, "updated resolver") {
		t.Errorf("Expected update (not add) for case-insensitive match, got: %s", text)
	}
	if len(ms.service.conf.Core.Resolvers) != 1 {
		t.Fatalf("Expected 1 resolver after case-insensitive update, got %d", len(ms.service.conf.Core.Resolvers))
	}
}

func TestHandleUpdateCurrentConfig_RemoveResolver(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ms := &mcpServer{
		service: &graphjinService{
			gj: nil,
			conf: &Config{
				Core: core.Config{
					Resolvers: []core.ResolverConfig{
						{Name: "payments", Type: "remote_api", Table: "customers"},
						{Name: "subscriptions", Type: "remote_api", Table: "users"},
					},
				},
				Serv: Serv{Production: false},
			},
			log: logger.Sugar(),
		},
		ctx: context.Background(),
	}

	req := newToolRequest(map[string]any{
		"remove_resolvers": []any{"payments"},
	})

	result, err := ms.handleUpdateCurrentConfig(context.Background(), req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	text := assertToolSuccess(t, result)
	if !strings.Contains(text, "removed resolver: payments") {
		t.Errorf("Expected 'removed resolver: payments' in result, got: %s", text)
	}

	// Verify only the correct resolver was removed
	if len(ms.service.conf.Core.Resolvers) != 1 {
		t.Fatalf("Expected 1 resolver after removal, got %d", len(ms.service.conf.Core.Resolvers))
	}
	if ms.service.conf.Core.Resolvers[0].Name != "subscriptions" {
		t.Errorf("Expected 'subscriptions' to remain, got %q", ms.service.conf.Core.Resolvers[0].Name)
	}
}

func TestHandleUpdateCurrentConfig_RemoveResolverCaseInsensitive(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ms := &mcpServer{
		service: &graphjinService{
			gj: nil,
			conf: &Config{
				Core: core.Config{
					Resolvers: []core.ResolverConfig{
						{Name: "Payments", Type: "remote_api", Table: "customers"},
					},
				},
				Serv: Serv{Production: false},
			},
			log: logger.Sugar(),
		},
		ctx: context.Background(),
	}

	req := newToolRequest(map[string]any{
		"remove_resolvers": []any{"payments"}, // lowercase
	})

	result, err := ms.handleUpdateCurrentConfig(context.Background(), req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	text := assertToolSuccess(t, result)
	if !strings.Contains(text, "removed resolver") {
		t.Errorf("Expected resolver removal with case-insensitive match, got: %s", text)
	}
	if len(ms.service.conf.Core.Resolvers) != 0 {
		t.Errorf("Expected 0 resolvers after removal, got %d", len(ms.service.conf.Core.Resolvers))
	}
}

func TestHandleUpdateCurrentConfig_InvalidResolverConfig(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ms := &mcpServer{
		service: &graphjinService{
			gj: nil,
			conf: &Config{
				Core: core.Config{},
				Serv: Serv{Production: false},
			},
			log: logger.Sugar(),
		},
		ctx: context.Background(),
	}

	req := newToolRequest(map[string]any{
		"resolvers": []any{
			map[string]any{
				"name": "payments",
				"type": "graphql", // invalid type
			},
		},
	})

	result, err := ms.handleUpdateCurrentConfig(context.Background(), req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	text := assertToolSuccess(t, result)
	if !strings.Contains(text, "invalid resolver type") {
		t.Errorf("Expected error about invalid resolver type, got: %s", text)
	}
}

func TestHandleUpdateCurrentConfig_InvalidResolverNotMap(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ms := &mcpServer{
		service: &graphjinService{
			gj: nil,
			conf: &Config{
				Core: core.Config{},
				Serv: Serv{Production: false},
			},
			log: logger.Sugar(),
		},
		ctx: context.Background(),
	}

	req := newToolRequest(map[string]any{
		"resolvers": []any{
			"not-a-map",
		},
	})

	result, err := ms.handleUpdateCurrentConfig(context.Background(), req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	text := assertToolSuccess(t, result)
	if !strings.Contains(text, "invalid resolver config") {
		t.Errorf("Expected error about invalid resolver config, got: %s", text)
	}
}

func TestSyncConfigToViper_WithResolvers(t *testing.T) {
	v := viper.New()
	logger := zaptest.NewLogger(t)
	ms := &mcpServer{
		service: &graphjinService{
			conf: &Config{
				Core: core.Config{
					Resolvers: []core.ResolverConfig{
						{Name: "payments", Type: "remote_api", Table: "customers"},
					},
				},
				viper: v,
			},
			log: logger.Sugar(),
		},
	}

	ms.syncConfigToViper(v)

	if v.Get("resolvers") == nil {
		t.Error("Expected resolvers to be set in viper")
	}
}

func TestSyncConfigToViper_EmptyResolvers(t *testing.T) {
	v := viper.New()
	logger := zaptest.NewLogger(t)
	ms := &mcpServer{
		service: &graphjinService{
			conf: &Config{
				Core:  core.Config{},
				viper: v,
			},
			log: logger.Sugar(),
		},
	}

	ms.syncConfigToViper(v)

	if v.Get("resolvers") != nil {
		t.Error("Expected resolvers to be nil for empty config")
	}
}

func TestQuerySyntaxReference_HasRemoteJoins(t *testing.T) {
	if len(querySyntaxReference.Examples.RemoteJoins) == 0 {
		t.Error("Expected RemoteJoins examples in query syntax reference")
	}
	for _, example := range querySyntaxReference.Examples.RemoteJoins {
		if example.Description == "" {
			t.Error("Expected description for remote join example")
		}
		if example.Query == "" {
			t.Error("Expected query for remote join example")
		}
	}
}

// =============================================================================
// enhanceError Tests
// =============================================================================

// =============================================================================
// Read-Only Database Tests
// =============================================================================

func TestHandleUpdateCurrentConfig_ReadOnlyTamperProtection(t *testing.T) {
	// Simulate a DB that was read_only: true in the config file.
	// Use SQLite to skip connection tests (file-based, no server needed).
	logger := zaptest.NewLogger(t)
	ms := &mcpServer{
		service: &graphjinService{
			gj: nil,
			conf: &Config{
				Core: core.Config{
					Databases: map[string]core.DatabaseConfig{
						"prod": {Type: "sqlite", Path: "/tmp/test_ro.db", ReadOnly: true},
					},
				},
				Serv: Serv{Production: false},
			},
			log: logger.Sugar(),
		},
		ctx:         context.Background(),
		readOnlyDBs: map[string]bool{"prod": true}, // startup snapshot
	}

	// LLM tries to update the database with read_only: false
	req := newToolRequest(map[string]any{
		"databases": map[string]any{
			"prod": map[string]any{
				"type":      "sqlite",
				"path":      "/tmp/test_ro.db",
				"read_only": false, // Trying to disable read-only
			},
		},
	})

	result, err := ms.handleUpdateCurrentConfig(context.Background(), req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Should succeed but preserve read_only: true
	text := assertToolSuccess(t, result)
	if !strings.Contains(text, "tamper-protected") {
		t.Errorf("Expected tamper-protection message, got: %s", text)
	}

	// Verify the config still has read_only: true
	dbConf := ms.service.conf.Core.Databases["prod"]
	if !dbConf.ReadOnly {
		t.Error("Expected read_only to remain true after tamper attempt")
	}
}

func TestHandleUpdateCurrentConfig_ReadOnlyNewDBNotProtected(t *testing.T) {
	// A new DB added via MCP should be able to set read_only: true
	// (only the startup snapshot is immutable)
	logger := zaptest.NewLogger(t)
	ms := &mcpServer{
		service: &graphjinService{
			gj: nil,
			conf: &Config{
				Core: core.Config{
					Databases: map[string]core.DatabaseConfig{},
				},
				Serv: Serv{Production: false},
			},
			log: logger.Sugar(),
		},
		ctx:         context.Background(),
		readOnlyDBs: map[string]bool{}, // empty snapshot (no DBs at startup)
	}

	// Adding a new DB with read_only: true should work
	req := newToolRequest(map[string]any{
		"databases": map[string]any{
			"newdb": map[string]any{
				"type":      "sqlite",
				"path":      "/tmp/test.db",
				"read_only": true,
			},
		},
	})

	result, err := ms.handleUpdateCurrentConfig(context.Background(), req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	text := assertToolSuccess(t, result)
	if !strings.Contains(text, "added/updated database: newdb") {
		t.Errorf("Expected database to be added, got: %s", text)
	}
	// Should NOT have tamper-protection warning since it wasn't in the snapshot
	if strings.Contains(text, "tamper-protected") {
		t.Errorf("New DB should not trigger tamper protection, got: %s", text)
	}
}

func TestNormalizeDatabases_PropagatesReadOnly(t *testing.T) {
	conf := &core.Config{
		Databases: map[string]core.DatabaseConfig{
			"readonly_db": {Type: "postgres", ReadOnly: true},
			"writable_db": {Type: "postgres", ReadOnly: false},
		},
		Tables: []core.Table{
			{Name: "orders", Database: "readonly_db"},
			{Name: "users", Database: "writable_db"},
		},
		Roles: []core.Role{
			{
				Name: "user",
				Tables: []core.RoleTable{
					{Name: "orders"},
					{Name: "users"},
				},
			},
		},
	}

	conf.NormalizeDatabases()

	// Find the role table entries
	for _, rt := range conf.Roles[0].Tables {
		if rt.Name == "orders" {
			if !rt.ReadOnly {
				t.Error("Expected 'orders' role table to be read-only (propagated from readonly_db)")
			}
		}
		if rt.Name == "users" {
			if rt.ReadOnly {
				t.Error("Expected 'users' role table to NOT be read-only")
			}
		}
	}
}

func TestParseDBConfig_ReadOnly(t *testing.T) {
	m := map[string]any{
		"type":      "postgres",
		"host":      "localhost",
		"read_only": true,
	}

	conf, err := parseDBConfig(m)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !conf.ReadOnly {
		t.Error("Expected ReadOnly to be true")
	}

	// Test with read_only: false
	m["read_only"] = false
	conf, err = parseDBConfig(m)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if conf.ReadOnly {
		t.Error("Expected ReadOnly to be false")
	}

	// Test without read_only (should default to false)
	delete(m, "read_only")
	conf, err = parseDBConfig(m)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if conf.ReadOnly {
		t.Error("Expected ReadOnly to default to false")
	}
}

func TestEnhanceError(t *testing.T) {
	testCases := []struct {
		name           string
		errMsg         string
		currentTool    string
		shouldEnhance  bool
		expectedTool   string
		notContains    string // substring that should NOT appear in enhanced output
	}{
		// Table-related errors
		{
			name:          "table not found",
			errMsg:        "table not found: users",
			currentTool:   "execute_graphql",
			shouldEnhance: true,
			expectedTool:  "list_tables",
		},
		{
			name:          "relation does not exist",
			errMsg:        "ERROR: relation \"orders\" does not exist",
			currentTool:   "execute_graphql",
			shouldEnhance: true,
			expectedTool:  "list_tables",
		},
		{
			name:          "no such table",
			errMsg:        "no such table: products",
			currentTool:   "execute_graphql",
			shouldEnhance: true,
			expectedTool:  "list_tables",
		},
		{
			name:          "unknown table",
			errMsg:        "unknown table 'items'",
			currentTool:   "execute_graphql",
			shouldEnhance: true,
			expectedTool:  "list_tables",
		},

		// Column-related errors
		{
			name:          "column not found",
			errMsg:        "column not found: email",
			currentTool:   "execute_graphql",
			shouldEnhance: true,
			expectedTool:  "describe_table",
		},
		{
			name:          "unknown column",
			errMsg:        "unknown column 'age' in field list",
			currentTool:   "execute_graphql",
			shouldEnhance: true,
			expectedTool:  "describe_table",
		},
		{
			name:          "no such column",
			errMsg:        "no such column: phone",
			currentTool:   "execute_graphql",
			shouldEnhance: true,
			expectedTool:  "describe_table",
		},

		// Operator-related errors
		{
			name:          "invalid operator",
			errMsg:        "invalid operator: LIKE",
			currentTool:   "execute_graphql",
			shouldEnhance: true,
			expectedTool:  "get_query_syntax",
		},
		{
			name:          "unsupported operator",
			errMsg:        "unsupported operator 'between'",
			currentTool:   "execute_graphql",
			shouldEnhance: true,
			expectedTool:  "get_query_syntax",
		},

		// Syntax errors
		{
			name:          "syntax error",
			errMsg:        "syntax error at or near '}'",
			currentTool:   "execute_graphql",
			shouldEnhance: true,
			expectedTool:  "get_query_syntax",
		},
		{
			name:          "parse error",
			errMsg:        "parse error: unexpected token",
			currentTool:   "execute_graphql",
			shouldEnhance: true,
			expectedTool:  "get_query_syntax",
		},

		// Permission errors
		{
			name:          "permission denied",
			errMsg:        "permission denied for table users",
			currentTool:   "execute_graphql",
			shouldEnhance: true,
			expectedTool:  "",
		},
		{
			name:          "access denied",
			errMsg:        "access denied for user 'guest'",
			currentTool:   "execute_graphql",
			shouldEnhance: true,
			expectedTool:  "",
		},

		// Should NOT enhance - avoid false positives from Bug 3 fix
		{
			name:          "generic error with table word in context",
			errMsg:        "pivot table calculation failed",
			currentTool:   "execute_graphql",
			shouldEnhance: false,
		},
		{
			name:          "generic error with column word in context",
			errMsg:        "column chart rendering error",
			currentTool:   "execute_graphql",
			shouldEnhance: false,
		},
		{
			name:          "generic error with operator word in context",
			errMsg:        "operator needs training",
			currentTool:   "execute_graphql",
			shouldEnhance: false,
		},
		{
			name:          "completely unrelated error",
			errMsg:        "connection timeout after 30s",
			currentTool:   "execute_graphql",
			shouldEnhance: false,
		},
		{
			name:          "empty error",
			errMsg:        "",
			currentTool:   "execute_graphql",
			shouldEnhance: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := enhanceError(tc.errMsg, tc.currentTool)

			if tc.shouldEnhance {
				// Should return JSON with suggestion
				if result == tc.errMsg {
					t.Errorf("Expected enhanced error for %q, but got original message back", tc.errMsg)
					return
				}

				// Parse the JSON to verify structure
				var enhanced EnhancedError
				if err := json.Unmarshal([]byte(result), &enhanced); err != nil {
					t.Errorf("Expected valid JSON for enhanced error, got: %s", result)
					return
				}

				if enhanced.Message != tc.errMsg {
					t.Errorf("Expected message %q, got %q", tc.errMsg, enhanced.Message)
				}
				if enhanced.Suggestion == "" {
					t.Error("Expected non-empty suggestion")
				}
				if tc.expectedTool != "" && enhanced.RelatedTool != tc.expectedTool {
					t.Errorf("Expected related tool %q, got %q", tc.expectedTool, enhanced.RelatedTool)
				}
			} else {
				// Should return original message unchanged
				if result != tc.errMsg {
					t.Errorf("Expected original error %q, but got enhanced: %s", tc.errMsg, result)
				}
			}
		})
	}
}
