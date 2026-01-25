package serv

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// mockMcpServer creates a mock mcpServer with a memory cursor cache for testing
func mockMcpServer() *mcpServer {
	svc := &graphjinService{
		cursorCache: NewMemoryCursorCache(100, time.Hour),
	}
	return &mcpServer{
		service: svc,
	}
}

func TestProcessCursorsForMCP(t *testing.T) {
	ms := mockMcpServer()
	ctx := context.Background()

	// Test data with encrypted cursor
	input := json.RawMessage(`{
		"products": [{"id": 1}, {"id": 2}],
		"products_cursor": "__gj-enc:abc123xyz"
	}`)

	result := ms.processCursorsForMCP(ctx, input)

	var parsed map[string]any
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("Failed to parse result: %v", err)
	}

	cursor, ok := parsed["products_cursor"].(string)
	if !ok {
		t.Fatal("products_cursor not found or not a string")
	}

	// Should be a numeric ID, not the encrypted cursor
	if cursor == "__gj-enc:abc123xyz" {
		t.Error("Cursor should have been replaced with numeric ID")
	}

	// Should be a short numeric string
	if cursor != "1" {
		t.Errorf("Expected cursor ID '1', got %q", cursor)
	}
}

func TestProcessCursorsForMCP_NestedObjects(t *testing.T) {
	ms := mockMcpServer()
	ctx := context.Background()

	// Test data with nested objects
	input := json.RawMessage(`{
		"data": {
			"users": [{"id": 1}],
			"users_cursor": "__gj-enc:users123"
		},
		"products_cursor": "__gj-enc:products456"
	}`)

	result := ms.processCursorsForMCP(ctx, input)

	var parsed map[string]any
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("Failed to parse result: %v", err)
	}

	// Check top-level cursor
	productsCursor, ok := parsed["products_cursor"].(string)
	if !ok {
		t.Fatal("products_cursor not found")
	}
	if productsCursor == "__gj-enc:products456" {
		t.Error("products_cursor should have been replaced")
	}

	// Check nested cursor
	data, ok := parsed["data"].(map[string]any)
	if !ok {
		t.Fatal("data not found")
	}
	usersCursor, ok := data["users_cursor"].(string)
	if !ok {
		t.Fatal("users_cursor not found")
	}
	if usersCursor == "__gj-enc:users123" {
		t.Error("users_cursor should have been replaced")
	}
}

func TestProcessCursorsForMCP_NoCursor(t *testing.T) {
	ms := mockMcpServer()
	ctx := context.Background()

	// Test data without cursors
	input := json.RawMessage(`{"products": [{"id": 1}, {"id": 2}]}`)

	result := ms.processCursorsForMCP(ctx, input)

	// Compare parsed JSON (formatting may differ)
	var inputParsed, resultParsed any
	if err := json.Unmarshal(input, &inputParsed); err != nil {
		t.Fatalf("Failed to parse input: %v", err)
	}
	if err := json.Unmarshal(result, &resultParsed); err != nil {
		t.Fatalf("Failed to parse result: %v", err)
	}

	inputJSON, _ := json.Marshal(inputParsed)
	resultJSON, _ := json.Marshal(resultParsed)

	if string(inputJSON) != string(resultJSON) {
		t.Errorf("Data without cursors should be unchanged.\nInput: %s\nResult: %s", inputJSON, resultJSON)
	}
}

func TestExpandCursorIDs(t *testing.T) {
	ms := mockMcpServer()
	ctx := context.Background()

	// First, store a cursor
	encryptedCursor := "__gj-enc:abc123xyz"
	id, err := ms.service.cursorCache.Set(ctx, encryptedCursor)
	if err != nil {
		t.Fatalf("Failed to set cursor: %v", err)
	}

	// Now test expansion
	vars := map[string]any{
		"cursor": "1", // numeric ID as string
		"limit":  10,
	}

	expanded, err := ms.expandCursorIDs(ctx, vars)
	if err != nil {
		t.Fatalf("expandCursorIDs failed: %v", err)
	}

	cursor, ok := expanded["cursor"].(string)
	if !ok {
		t.Fatal("cursor not found or not a string")
	}

	if cursor != encryptedCursor {
		t.Errorf("Expected %q, got %q", encryptedCursor, cursor)
	}

	// Non-cursor variables should be unchanged
	if expanded["limit"] != 10 {
		t.Error("Non-cursor variable should be unchanged")
	}

	_ = id // use the id
}

func TestExpandCursorIDs_AlreadyEncrypted(t *testing.T) {
	ms := mockMcpServer()
	ctx := context.Background()

	// Variables with already-encrypted cursor
	vars := map[string]any{
		"cursor": "__gj-enc:already-encrypted",
	}

	expanded, err := ms.expandCursorIDs(ctx, vars)
	if err != nil {
		t.Fatalf("expandCursorIDs failed: %v", err)
	}

	// Should be unchanged
	cursor, ok := expanded["cursor"].(string)
	if !ok {
		t.Fatal("cursor not found or not a string")
	}

	if cursor != "__gj-enc:already-encrypted" {
		t.Error("Already-encrypted cursor should be unchanged")
	}
}

func TestExpandCursorIDs_InvalidID(t *testing.T) {
	ms := mockMcpServer()
	ctx := context.Background()

	// Variables with non-existent cursor ID
	vars := map[string]any{
		"cursor": "999",
	}

	_, err := ms.expandCursorIDs(ctx, vars)
	if err == nil {
		t.Error("Expected error for non-existent cursor ID")
	}
}

func TestExpandCursorIDs_VariousKeyNames(t *testing.T) {
	ms := mockMcpServer()
	ctx := context.Background()

	// Store cursors
	cursor1 := "__gj-enc:cursor1"
	id1, _ := ms.service.cursorCache.Set(ctx, cursor1)
	cursor2 := "__gj-enc:cursor2"
	id2, _ := ms.service.cursorCache.Set(ctx, cursor2)
	cursor3 := "__gj-enc:cursor3"
	id3, _ := ms.service.cursorCache.Set(ctx, cursor3)

	// Test various key names that should be recognized as cursors
	testCases := []struct {
		key    string
		idStr  string
		expect string
	}{
		{"cursor", "1", cursor1},
		{"after", "2", cursor2},
		{"before", "3", cursor3},
		{"users_cursor", "1", cursor1},
		{"productsCursor", "2", cursor2},
	}

	for _, tc := range testCases {
		vars := map[string]any{tc.key: tc.idStr}
		expanded, err := ms.expandCursorIDs(ctx, vars)
		if err != nil {
			t.Errorf("Key %q: expandCursorIDs failed: %v", tc.key, err)
			continue
		}

		val, ok := expanded[tc.key].(string)
		if !ok {
			t.Errorf("Key %q: value not found or not a string", tc.key)
			continue
		}

		if val != tc.expect {
			t.Errorf("Key %q: expected %q, got %q", tc.key, tc.expect, val)
		}
	}

	_ = id1
	_ = id2
	_ = id3
}

func TestIsCursorKey(t *testing.T) {
	testCases := []struct {
		key      string
		expected bool
	}{
		{"cursor", true},
		{"Cursor", true},
		{"CURSOR", true},
		{"after", true},
		{"After", true},
		{"before", true},
		{"Before", true},
		{"users_cursor", true},
		{"productsCursor", true},
		{"someCursor", true},
		{"name", false},
		{"limit", false},
		{"offset", false},
		{"id", false},
	}

	for _, tc := range testCases {
		result := isCursorKey(tc.key)
		if result != tc.expected {
			t.Errorf("isCursorKey(%q) = %v, expected %v", tc.key, result, tc.expected)
		}
	}
}

func TestProcessCursorsForMCP_Roundtrip(t *testing.T) {
	ms := mockMcpServer()
	ctx := context.Background()

	// Simulate a query response with cursor
	responseData := json.RawMessage(`{
		"users": [{"id": 1, "name": "Alice"}, {"id": 2, "name": "Bob"}],
		"users_cursor": "__gj-enc:encrypted-cursor-value-12345"
	}`)

	// Process response (replace encrypted cursor with ID)
	processed := ms.processCursorsForMCP(ctx, responseData)

	var parsedResponse map[string]any
	if err := json.Unmarshal(processed, &parsedResponse); err != nil {
		t.Fatalf("Failed to parse processed response: %v", err)
	}

	cursorID := parsedResponse["users_cursor"].(string)

	// Now simulate using that cursor ID in a subsequent request
	vars := map[string]any{
		"users_cursor": cursorID,
	}

	expanded, err := ms.expandCursorIDs(ctx, vars)
	if err != nil {
		t.Fatalf("Failed to expand cursor IDs: %v", err)
	}

	// Should get back the original encrypted cursor
	expandedCursor := expanded["users_cursor"].(string)
	if expandedCursor != "__gj-enc:encrypted-cursor-value-12345" {
		t.Errorf("Roundtrip failed: expected original encrypted cursor, got %q", expandedCursor)
	}
}
