package core

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
)

// RowRef represents a (table, row_id) pair for cache indexing
type RowRef struct {
	Table string
	ID    string
}

// ResponseProcessor handles extraction and stripping of __gj_id fields for caching
type ResponseProcessor struct {
	qc *qcode.QCode
}

// NewResponseProcessor creates a new response processor
func NewResponseProcessor(qc *qcode.QCode) *ResponseProcessor {
	return &ResponseProcessor{qc: qc}
}

// ProcessForCache extracts row references and strips __gj_id from response.
// Returns the cleaned response and list of (table, row_id) pairs.
func (rp *ResponseProcessor) ProcessForCache(data []byte) (cleaned []byte, refs []RowRef, err error) {
	if len(data) == 0 {
		return data, nil, nil
	}

	// Parse JSON response
	var result map[string]interface{}
	if err = json.Unmarshal(data, &result); err != nil {
		return data, nil, err
	}

	// Get the "data" field
	dataField, ok := result["data"]
	if !ok {
		return data, nil, nil
	}

	dataMap, ok := dataField.(map[string]interface{})
	if !ok {
		return data, nil, nil
	}

	refs = make([]RowRef, 0, 100)

	// Process each root selection
	for i := range rp.qc.Selects {
		sel := &rp.qc.Selects[i]
		if sel.ParentID != -1 {
			continue // Skip non-root selections
		}

		fieldName := sel.FieldName
		if fieldName == "" {
			fieldName = sel.Table
		}

		if fieldData, ok := dataMap[fieldName]; ok {
			rp.processNode(sel.Table, fieldData, &refs, sel)
		}
	}

	// Re-serialize cleaned response
	cleaned, err = json.Marshal(result)
	return
}

func (rp *ResponseProcessor) processNode(
	tableName string,
	data interface{},
	refs *[]RowRef,
	sel *qcode.Select,
) {
	switch v := data.(type) {
	case map[string]interface{}:
		rp.processObject(tableName, v, refs, sel)
	case []interface{}:
		for _, item := range v {
			if obj, ok := item.(map[string]interface{}); ok {
				rp.processObject(tableName, obj, refs, sel)
			}
		}
	}
}

func (rp *ResponseProcessor) processObject(
	tableName string,
	obj map[string]interface{},
	refs *[]RowRef,
	sel *qcode.Select,
) {
	// Extract and remove __gj_id
	if id, ok := obj["__gj_id"]; ok {
		*refs = append(*refs, RowRef{
			Table: tableName,
			ID:    stringifyID(id),
		})
		delete(obj, "__gj_id")
	}

	// Process child selections
	if sel != nil {
		for _, childID := range sel.Children {
			if childID < 0 || int(childID) >= len(rp.qc.Selects) {
				continue
			}
			childSel := &rp.qc.Selects[childID]

			fieldName := childSel.FieldName
			if fieldName == "" {
				fieldName = childSel.Table
			}

			if childData, ok := obj[fieldName]; ok {
				rp.processNode(childSel.Table, childData, refs, childSel)
			}
		}
	}
}

// stringifyID converts various ID types to string
func stringifyID(id interface{}) string {
	switch v := id.(type) {
	case string:
		return v
	case float64:
		// Check if it's a whole number
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case json.Number:
		return v.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}

// ExtractMutationRefs extracts affected row IDs from a mutation response.
// Used for cache invalidation after INSERT/UPDATE/DELETE.
func ExtractMutationRefs(qc *qcode.QCode, data []byte) []RowRef {
	if len(data) == 0 || qc == nil {
		return nil
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil
	}

	dataField, ok := result["data"].(map[string]interface{})
	if !ok {
		return nil
	}

	refs := make([]RowRef, 0)

	// Extract IDs from each mutated table using the Mutates list
	for _, m := range qc.Mutates {
		if m.Type == qcode.MTNone {
			continue
		}

		tableName := m.Ti.Name
		pkName := m.Ti.PrimaryCol.Name
		if pkName == "" {
			continue
		}

		// Find the table data in response using the mutation key
		if tableData, ok := dataField[m.Key]; ok {
			refs = append(refs, extractIDsFromData(tableName, pkName, tableData)...)
		}
	}

	return refs
}

func extractIDsFromData(tableName, pkName string, data interface{}) []RowRef {
	refs := make([]RowRef, 0)

	switch v := data.(type) {
	case map[string]interface{}:
		if id, ok := v[pkName]; ok {
			refs = append(refs, RowRef{Table: tableName, ID: stringifyID(id)})
		}
		// Also check for __gj_id in case it was added
		if id, ok := v["__gj_id"]; ok {
			refs = append(refs, RowRef{Table: tableName, ID: stringifyID(id)})
		}
	case []interface{}:
		for _, item := range v {
			if obj, ok := item.(map[string]interface{}); ok {
				if id, ok := obj[pkName]; ok {
					refs = append(refs, RowRef{Table: tableName, ID: stringifyID(id)})
				}
				if id, ok := obj["__gj_id"]; ok {
					refs = append(refs, RowRef{Table: tableName, ID: stringifyID(id)})
				}
			}
		}
	}

	return refs
}
