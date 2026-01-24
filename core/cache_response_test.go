package core

import (
	"encoding/json"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func TestStringifyID(t *testing.T) {
	tests := []struct {
		name  string
		input interface{}
		want  string
	}{
		{"string", "abc123", "abc123"},
		{"int", 42, "42"},
		{"int64", int64(9999999999), "9999999999"},
		{"float64 whole", float64(123), "123"},
		{"float64 decimal", float64(123.456), "123.456"},
		{"json.Number", json.Number("789"), "789"},
		{"nil", nil, "<nil>"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stringifyID(tt.input)
			if got != tt.want {
				t.Errorf("stringifyID(%v) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestResponseProcessor_EmptyData(t *testing.T) {
	// Create a minimal qcode for testing
	qc := &qcode.QCode{
		Selects: []qcode.Select{},
	}
	rp := NewResponseProcessor(qc)

	// Empty data should return empty
	cleaned, refs, err := rp.ProcessForCache(nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(cleaned) != 0 {
		t.Errorf("expected empty cleaned data, got %s", cleaned)
	}
	if len(refs) != 0 {
		t.Errorf("expected no refs, got %d", len(refs))
	}

	// Empty byte slice
	cleaned, refs, err = rp.ProcessForCache([]byte{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(cleaned) != 0 {
		t.Errorf("expected empty cleaned data")
	}
	if len(refs) != 0 {
		t.Errorf("expected no refs")
	}
}

func TestResponseProcessor_NoDataField(t *testing.T) {
	qc := &qcode.QCode{
		Selects: []qcode.Select{},
	}
	rp := NewResponseProcessor(qc)

	// Response without "data" field
	input := []byte(`{"errors": [{"message": "something wrong"}]}`)
	cleaned, refs, err := rp.ProcessForCache(input)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(refs) != 0 {
		t.Errorf("expected no refs for error response, got %d", len(refs))
	}
	// Should return original input
	if string(cleaned) != string(input) {
		t.Errorf("expected original input returned")
	}
}

func TestResponseProcessor_SingleObject(t *testing.T) {
	// Create qcode with one selection for "users" table
	qc := &qcode.QCode{
		Selects: []qcode.Select{
			{
				Field: qcode.Field{
					ID:        0,
					ParentID:  -1,
					FieldName: "users",
				},
				Table: "users",
				Ti:    sdata.DBTable{Name: "users"},
			},
		},
	}
	rp := NewResponseProcessor(qc)

	input := []byte(`{"data": {"users": {"id": 1, "name": "John", "__gj_id": 1}}}`)
	cleaned, refs, err := rp.ProcessForCache(input)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Should have extracted one ref
	if len(refs) != 1 {
		t.Errorf("expected 1 ref, got %d", len(refs))
	} else {
		if refs[0].Table != "users" {
			t.Errorf("expected table 'users', got %q", refs[0].Table)
		}
		if refs[0].ID != "1" {
			t.Errorf("expected ID '1', got %q", refs[0].ID)
		}
	}

	// Should have removed __gj_id from output
	var result map[string]interface{}
	if err := json.Unmarshal(cleaned, &result); err != nil {
		t.Errorf("failed to parse cleaned response: %v", err)
	}
	dataMap := result["data"].(map[string]interface{})
	usersMap := dataMap["users"].(map[string]interface{})
	if _, hasGjId := usersMap["__gj_id"]; hasGjId {
		t.Errorf("__gj_id should have been removed from response")
	}
	if usersMap["id"] != float64(1) {
		t.Errorf("id field should be preserved")
	}
}

func TestResponseProcessor_Array(t *testing.T) {
	qc := &qcode.QCode{
		Selects: []qcode.Select{
			{
				Field: qcode.Field{
					ID:        0,
					ParentID:  -1,
					FieldName: "products",
				},
				Table: "products",
				Ti:    sdata.DBTable{Name: "products"},
			},
		},
	}
	rp := NewResponseProcessor(qc)

	input := []byte(`{"data": {"products": [
		{"id": 1, "name": "Widget", "__gj_id": 1},
		{"id": 2, "name": "Gadget", "__gj_id": 2},
		{"id": 3, "name": "Gizmo", "__gj_id": 3}
	]}}`)

	cleaned, refs, err := rp.ProcessForCache(input)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Should have extracted three refs
	if len(refs) != 3 {
		t.Errorf("expected 3 refs, got %d", len(refs))
	}

	// Verify all refs are for products table
	for i, ref := range refs {
		if ref.Table != "products" {
			t.Errorf("ref[%d] expected table 'products', got %q", i, ref.Table)
		}
	}

	// Verify __gj_id removed from all items
	var result map[string]interface{}
	if err := json.Unmarshal(cleaned, &result); err != nil {
		t.Errorf("failed to parse cleaned response: %v", err)
	}
	dataMap := result["data"].(map[string]interface{})
	products := dataMap["products"].([]interface{})
	for i, p := range products {
		prod := p.(map[string]interface{})
		if _, hasGjId := prod["__gj_id"]; hasGjId {
			t.Errorf("products[%d] should not have __gj_id", i)
		}
	}
}

func TestResponseProcessor_NestedObjects(t *testing.T) {
	// Create qcode with nested selection: users -> posts
	qc := &qcode.QCode{
		Selects: []qcode.Select{
			{
				Field: qcode.Field{
					ID:        0,
					ParentID:  -1,
					FieldName: "users",
				},
				Table:    "users",
				Ti:       sdata.DBTable{Name: "users"},
				Children: []int32{1},
			},
			{
				Field: qcode.Field{
					ID:        1,
					ParentID:  0,
					FieldName: "posts",
				},
				Table: "posts",
				Ti:    sdata.DBTable{Name: "posts"},
			},
		},
	}
	rp := NewResponseProcessor(qc)

	input := []byte(`{"data": {"users": {
		"id": 1,
		"name": "John",
		"__gj_id": 1,
		"posts": [
			{"id": 10, "title": "First Post", "__gj_id": 10},
			{"id": 11, "title": "Second Post", "__gj_id": 11}
		]
	}}}`)

	cleaned, refs, err := rp.ProcessForCache(input)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Should have extracted 3 refs (1 user + 2 posts)
	if len(refs) != 3 {
		t.Errorf("expected 3 refs, got %d", len(refs))
	}

	// Count by table
	tableCount := make(map[string]int)
	for _, ref := range refs {
		tableCount[ref.Table]++
	}
	if tableCount["users"] != 1 {
		t.Errorf("expected 1 users ref, got %d", tableCount["users"])
	}
	if tableCount["posts"] != 2 {
		t.Errorf("expected 2 posts refs, got %d", tableCount["posts"])
	}

	// Verify all __gj_id fields removed
	var result map[string]interface{}
	if err := json.Unmarshal(cleaned, &result); err != nil {
		t.Errorf("failed to parse cleaned response: %v", err)
	}
	dataMap := result["data"].(map[string]interface{})
	user := dataMap["users"].(map[string]interface{})
	if _, hasGjId := user["__gj_id"]; hasGjId {
		t.Errorf("user should not have __gj_id")
	}
	posts := user["posts"].([]interface{})
	for i, p := range posts {
		post := p.(map[string]interface{})
		if _, hasGjId := post["__gj_id"]; hasGjId {
			t.Errorf("posts[%d] should not have __gj_id", i)
		}
	}
}

func TestExtractMutationRefs_Empty(t *testing.T) {
	// Nil qcode
	refs := ExtractMutationRefs(nil, []byte(`{"data": {"users": {"id": 1}}}`))
	if len(refs) != 0 {
		t.Errorf("expected no refs for nil qcode, got %d", len(refs))
	}

	// Empty data
	qc := &qcode.QCode{}
	refs = ExtractMutationRefs(qc, nil)
	if len(refs) != 0 {
		t.Errorf("expected no refs for empty data, got %d", len(refs))
	}

	refs = ExtractMutationRefs(qc, []byte{})
	if len(refs) != 0 {
		t.Errorf("expected no refs for empty bytes, got %d", len(refs))
	}
}

func TestExtractMutationRefs_Insert(t *testing.T) {
	// Simulate INSERT mutation qcode
	qc := &qcode.QCode{
		Mutates: []qcode.Mutate{
			{
				Type: qcode.MTInsert,
				Key:  "users",
				Ti: sdata.DBTable{
					Name:       "users",
					PrimaryCol: sdata.DBColumn{Name: "id"},
				},
			},
		},
	}

	data := []byte(`{"data": {"users": {"id": 42, "name": "New User"}}}`)
	refs := ExtractMutationRefs(qc, data)

	if len(refs) != 1 {
		t.Errorf("expected 1 ref, got %d", len(refs))
	} else {
		if refs[0].Table != "users" {
			t.Errorf("expected table 'users', got %q", refs[0].Table)
		}
		if refs[0].ID != "42" {
			t.Errorf("expected ID '42', got %q", refs[0].ID)
		}
	}
}

func TestExtractMutationRefs_BulkInsert(t *testing.T) {
	qc := &qcode.QCode{
		Mutates: []qcode.Mutate{
			{
				Type: qcode.MTInsert,
				Key:  "products",
				Ti: sdata.DBTable{
					Name:       "products",
					PrimaryCol: sdata.DBColumn{Name: "id"},
				},
			},
		},
	}

	data := []byte(`{"data": {"products": [
		{"id": 1, "name": "Product 1"},
		{"id": 2, "name": "Product 2"},
		{"id": 3, "name": "Product 3"}
	]}}`)

	refs := ExtractMutationRefs(qc, data)

	if len(refs) != 3 {
		t.Errorf("expected 3 refs for bulk insert, got %d", len(refs))
	}

	for i, ref := range refs {
		if ref.Table != "products" {
			t.Errorf("refs[%d] expected table 'products', got %q", i, ref.Table)
		}
	}
}

func TestResponseProcessor_MalformedJSON(t *testing.T) {
	qc := &qcode.QCode{
		Selects: []qcode.Select{},
	}
	rp := NewResponseProcessor(qc)

	// Invalid JSON should return error
	input := []byte(`{"data": invalid}`)
	_, _, err := rp.ProcessForCache(input)
	if err == nil {
		t.Errorf("expected error for malformed JSON")
	}
}

func TestResponseProcessor_MissingGjId(t *testing.T) {
	// Test that objects without __gj_id are handled gracefully
	qc := &qcode.QCode{
		Selects: []qcode.Select{
			{
				Field: qcode.Field{
					ID:        0,
					ParentID:  -1,
					FieldName: "users",
				},
				Table: "users",
				Ti:    sdata.DBTable{Name: "users"},
			},
		},
	}
	rp := NewResponseProcessor(qc)

	// Object without __gj_id
	input := []byte(`{"data": {"users": {"id": 1, "name": "John"}}}`)
	cleaned, refs, err := rp.ProcessForCache(input)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Should have no refs extracted
	if len(refs) != 0 {
		t.Errorf("expected 0 refs for object without __gj_id, got %d", len(refs))
	}

	// Response should still be valid
	if len(cleaned) == 0 {
		t.Errorf("expected non-empty cleaned response")
	}
}

func TestResponseProcessor_NullValues(t *testing.T) {
	qc := &qcode.QCode{
		Selects: []qcode.Select{
			{
				Field: qcode.Field{
					ID:        0,
					ParentID:  -1,
					FieldName: "users",
				},
				Table: "users",
				Ti:    sdata.DBTable{Name: "users"},
			},
		},
	}
	rp := NewResponseProcessor(qc)

	// Array with null elements
	input := []byte(`{"data": {"users": [{"id": 1, "__gj_id": 1}, null, {"id": 2, "__gj_id": 2}]}}`)
	cleaned, refs, err := rp.ProcessForCache(input)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Should extract refs from non-null objects
	if len(refs) != 2 {
		t.Errorf("expected 2 refs (skipping null), got %d", len(refs))
	}

	// Verify output is still valid JSON
	var result map[string]interface{}
	if err := json.Unmarshal(cleaned, &result); err != nil {
		t.Errorf("failed to parse cleaned response: %v", err)
	}
}

func TestResponseProcessor_LargeArray(t *testing.T) {
	qc := &qcode.QCode{
		Selects: []qcode.Select{
			{
				Field: qcode.Field{
					ID:        0,
					ParentID:  -1,
					FieldName: "items",
				},
				Table: "items",
				Ti:    sdata.DBTable{Name: "items"},
			},
		},
	}
	rp := NewResponseProcessor(qc)

	// Generate array with 1000 items
	var items []map[string]interface{}
	for i := 1; i <= 1000; i++ {
		items = append(items, map[string]interface{}{
			"id":      i,
			"__gj_id": i,
			"name":    "Item",
		})
	}
	data := map[string]interface{}{"data": map[string]interface{}{"items": items}}
	input, _ := json.Marshal(data)

	cleaned, refs, err := rp.ProcessForCache(input)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Should extract all refs
	if len(refs) != 1000 {
		t.Errorf("expected 1000 refs, got %d", len(refs))
	}

	// Verify __gj_id removed
	var result map[string]interface{}
	if err := json.Unmarshal(cleaned, &result); err != nil {
		t.Errorf("failed to parse cleaned response: %v", err)
	}
	dataMap := result["data"].(map[string]interface{})
	itemsResult := dataMap["items"].([]interface{})
	if len(itemsResult) != 1000 {
		t.Errorf("expected 1000 items in cleaned output, got %d", len(itemsResult))
	}
	// Check first item doesn't have __gj_id
	firstItem := itemsResult[0].(map[string]interface{})
	if _, hasGjId := firstItem["__gj_id"]; hasGjId {
		t.Errorf("first item should not have __gj_id")
	}
}

func TestExtractMutationRefs_Update(t *testing.T) {
	qc := &qcode.QCode{
		Mutates: []qcode.Mutate{
			{
				Type: qcode.MTUpdate,
				Key:  "users",
				Ti: sdata.DBTable{
					Name:       "users",
					PrimaryCol: sdata.DBColumn{Name: "id"},
				},
			},
		},
	}

	data := []byte(`{"data": {"users": {"id": 42, "name": "Updated User"}}}`)
	refs := ExtractMutationRefs(qc, data)

	if len(refs) != 1 {
		t.Errorf("expected 1 ref for update, got %d", len(refs))
	} else {
		if refs[0].Table != "users" {
			t.Errorf("expected table 'users', got %q", refs[0].Table)
		}
		if refs[0].ID != "42" {
			t.Errorf("expected ID '42', got %q", refs[0].ID)
		}
	}
}

func TestExtractMutationRefs_NoPrimaryKey(t *testing.T) {
	// Table without primary key
	qc := &qcode.QCode{
		Mutates: []qcode.Mutate{
			{
				Type: qcode.MTInsert,
				Key:  "audit_logs",
				Ti: sdata.DBTable{
					Name:       "audit_logs",
					PrimaryCol: sdata.DBColumn{}, // No primary key
				},
			},
		},
	}

	data := []byte(`{"data": {"audit_logs": {"message": "test log"}}}`)
	refs := ExtractMutationRefs(qc, data)

	// Should return no refs when there's no primary key
	if len(refs) != 0 {
		t.Errorf("expected 0 refs for table without PK, got %d", len(refs))
	}
}

func TestStringifyID_EdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		input interface{}
		want  string
	}{
		{"empty string", "", ""},
		{"zero int", 0, "0"},
		{"negative int", -42, "-42"},
		{"large int64", int64(9223372036854775807), "9223372036854775807"},
		{"small float", float64(0.001), "0.001"},
		{"scientific notation", float64(1e10), "10000000000"},
		{"bool true", true, "true"},
		{"bool false", false, "false"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stringifyID(tt.input)
			if got != tt.want {
				t.Errorf("stringifyID(%v) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
