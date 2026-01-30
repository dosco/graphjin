package serv

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
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
