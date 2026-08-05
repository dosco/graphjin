package core

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
)

// reshapeHasuraAggregateData restores the nested response requested by the
// compatibility syntax after GraphJin has executed its native aggregate
// fields. Raw JSON leaves are preserved so numbers do not pass through a
// float64 conversion and encrypted values remain byte-for-byte intact.
func reshapeHasuraAggregateData(data []byte, plans []qcode.HasuraAggregateRoot) ([]byte, error) {
	if len(data) == 0 || len(plans) == 0 {
		return data, nil
	}

	var document map[string]json.RawMessage
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	for _, plan := range plans {
		raw, ok := document[plan.ResponseKey]
		if !ok {
			continue
		}
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			continue
		}

		var rows []map[string]json.RawMessage
		if err := json.Unmarshal(raw, &rows); err != nil {
			return nil, fmt.Errorf("root %q: expected native aggregate row: %w", plan.ResponseKey, err)
		}
		if len(rows) > 1 {
			return nil, fmt.Errorf("root %q: expected one native aggregate row, got %d", plan.ResponseKey, len(rows))
		}

		var row map[string]json.RawMessage
		if len(rows) == 1 {
			row = rows[0]
		} else {
			row = map[string]json.RawMessage{}
		}
		root := map[string]any{}
		for _, field := range plan.Fields {
			value, ok := row[field.NativeField]
			if !ok {
				value = json.RawMessage("null")
			}
			if err := setHasuraAggregatePath(root, field.Path, value); err != nil {
				return nil, fmt.Errorf("root %q: %w", plan.ResponseKey, err)
			}
		}
		reshaped, err := json.Marshal(root)
		if err != nil {
			return nil, err
		}
		document[plan.ResponseKey] = reshaped
	}
	return json.Marshal(document)
}

func setHasuraAggregatePath(root map[string]any, path []string, value json.RawMessage) error {
	if len(path) == 0 {
		return fmt.Errorf("empty aggregate response path")
	}
	current := root
	for _, part := range path[:len(path)-1] {
		next, ok := current[part]
		if !ok {
			child := map[string]any{}
			current[part] = child
			current = child
			continue
		}
		child, ok := next.(map[string]any)
		if !ok {
			return fmt.Errorf("aggregate response path %q collides with a scalar", part)
		}
		current = child
	}
	current[path[len(path)-1]] = value
	return nil
}
