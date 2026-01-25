package serv

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

const (
	// cursorFieldSuffix is the suffix for cursor fields in GraphJin responses
	cursorFieldSuffix = "_cursor"

	// encryptedCursorPrefix is the prefix for encrypted cursors from GraphJin
	encryptedCursorPrefix = "__gj-enc:"
)

// processCursorsForMCP replaces encrypted cursors with numeric IDs in response data
// This makes cursors LLM-friendly by using short numeric IDs instead of long encrypted strings
func (ms *mcpServer) processCursorsForMCP(ctx context.Context, data json.RawMessage) json.RawMessage {
	if ms.service.cursorCache == nil || len(data) == 0 {
		return data
	}

	// Parse the JSON
	var parsed any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return data
	}

	// Process the parsed data
	processed := ms.processValue(ctx, parsed)

	// Re-marshal
	result, err := json.Marshal(processed)
	if err != nil {
		return data
	}

	return result
}

// processValue recursively processes JSON values, replacing encrypted cursors with IDs
func (ms *mcpServer) processValue(ctx context.Context, v any) any {
	switch val := v.(type) {
	case map[string]any:
		return ms.processObject(ctx, val)
	case []any:
		return ms.processArray(ctx, val)
	default:
		return v
	}
}

// processObject processes a JSON object, replacing cursor fields
func (ms *mcpServer) processObject(ctx context.Context, obj map[string]any) map[string]any {
	result := make(map[string]any, len(obj))

	for key, value := range obj {
		// Check if this is a cursor field with an encrypted value
		if strings.HasSuffix(key, cursorFieldSuffix) {
			if strVal, ok := value.(string); ok && strings.HasPrefix(strVal, encryptedCursorPrefix) {
				// Replace with numeric ID
				id, err := ms.service.cursorCache.Set(ctx, strVal)
				if err == nil {
					result[key] = strconv.FormatUint(id, 10)
					continue
				}
				// On error, keep original value
			}
		}

		// Recursively process nested values
		result[key] = ms.processValue(ctx, value)
	}

	return result
}

// processArray processes a JSON array
func (ms *mcpServer) processArray(ctx context.Context, arr []any) []any {
	result := make([]any, len(arr))
	for i, v := range arr {
		result[i] = ms.processValue(ctx, v)
	}
	return result
}

// expandCursorIDs replaces numeric cursor IDs with cached encrypted cursors in variables
// This is called before query execution to expand short IDs back to full cursors
func (ms *mcpServer) expandCursorIDs(ctx context.Context, vars map[string]any) (map[string]any, error) {
	if ms.service.cursorCache == nil || len(vars) == 0 {
		return vars, nil
	}

	result := make(map[string]any, len(vars))

	for key, value := range vars {
		// Check if this is a cursor variable (key contains "cursor")
		if isCursorKey(key) {
			if strVal, ok := value.(string); ok {
				// Check if it's a numeric ID (not already an encrypted cursor)
				if !strings.HasPrefix(strVal, encryptedCursorPrefix) {
					// Try to parse as uint64
					if id, err := strconv.ParseUint(strVal, 10, 64); err == nil {
						// Look up the cached cursor
						cursor, err := ms.service.cursorCache.Get(ctx, id)
						if err != nil {
							return nil, fmt.Errorf("invalid cursor ID %q: %w", strVal, err)
						}
						result[key] = cursor
						continue
					}
				}
			}
		}

		// Keep original value
		result[key] = value
	}

	return result, nil
}

// isCursorKey checks if a variable key is a cursor variable
// Matches: "cursor", "after", "before", "*_cursor"
func isCursorKey(key string) bool {
	lower := strings.ToLower(key)
	return lower == "cursor" ||
		lower == "after" ||
		lower == "before" ||
		strings.HasSuffix(lower, "_cursor") ||
		strings.HasSuffix(lower, "cursor")
}
