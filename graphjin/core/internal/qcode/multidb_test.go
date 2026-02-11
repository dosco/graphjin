package qcode

import (
	"fmt"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

// TestSelectDatabaseField verifies that Select has a Database field.
func TestSelectDatabaseField(t *testing.T) {
	sel := Select{
		Table:    "users",
		Database: "analytics",
	}

	if sel.Database != "analytics" {
		t.Errorf("Database = %q, want %q", sel.Database, "analytics")
	}
}

// TestSkipTypeDatabaseJoin verifies SkipTypeDatabaseJoin is defined correctly.
func TestSkipTypeDatabaseJoin(t *testing.T) {
	// Verify SkipTypeDatabaseJoin has a reasonable value (not 0 which is SkipTypeNone)
	if SkipTypeDatabaseJoin == SkipTypeNone {
		t.Error("SkipTypeDatabaseJoin should not equal SkipTypeNone")
	}

	// Verify it's distinct from other skip types
	types := []SkipType{
		SkipTypeNone,
		SkipTypeUserNeeded,
		SkipTypeBlocked,
		SkipTypeDrop,
		SkipTypeRemote,
	}

	for _, st := range types {
		if st == SkipTypeDatabaseJoin {
			t.Errorf("SkipTypeDatabaseJoin should be distinct from %v", st)
		}
	}
}

// TestSkipTypeDatabaseJoinString verifies String() for SkipTypeDatabaseJoin.
func TestSkipTypeDatabaseJoinString(t *testing.T) {
	s := SkipTypeDatabaseJoin.String()
	if s != "SkipTypeDatabaseJoin" {
		t.Errorf("SkipTypeDatabaseJoin.String() = %q, want %q", s, "SkipTypeDatabaseJoin")
	}
}

// TestQCodeSelectsWithDatabaseField verifies QCode Selects can use Database field.
func TestQCodeSelectsWithDatabaseField(t *testing.T) {
	qc := &QCode{
		Selects: []Select{
			{Table: "users", Database: "main"},
			{Table: "orders", Database: "analytics"},
			{Table: "products", Database: ""}, // default
		},
	}

	if qc.Selects[0].Database != "main" {
		t.Errorf("Selects[0].Database = %q, want %q", qc.Selects[0].Database, "main")
	}
	if qc.Selects[1].Database != "analytics" {
		t.Errorf("Selects[1].Database = %q, want %q", qc.Selects[1].Database, "analytics")
	}
	if qc.Selects[2].Database != "" {
		t.Errorf("Selects[2].Database = %q, want empty", qc.Selects[2].Database)
	}
}

// TestSkipRenderWithDatabaseJoin verifies SkipRender can be set to SkipTypeDatabaseJoin.
func TestSkipRenderWithDatabaseJoin(t *testing.T) {
	sel := Select{
		Field: Field{
			SkipRender: SkipTypeDatabaseJoin,
		},
		Table:    "orders",
		Database: "analytics",
	}

	if sel.SkipRender != SkipTypeDatabaseJoin {
		t.Errorf("SkipRender = %v, want %v", sel.SkipRender, SkipTypeDatabaseJoin)
	}
}

// TestMixedSkipTypes verifies different skip types can coexist.
func TestMixedSkipTypes(t *testing.T) {
	selects := []Select{
		{Field: Field{SkipRender: SkipTypeNone}},
		{Field: Field{SkipRender: SkipTypeRemote}},
		{Field: Field{SkipRender: SkipTypeDatabaseJoin}},
		{Field: Field{SkipRender: SkipTypeUserNeeded}},
	}

	// Count each type
	counts := make(map[SkipType]int)
	for _, sel := range selects {
		counts[sel.SkipRender]++
	}

	if counts[SkipTypeNone] != 1 {
		t.Errorf("SkipTypeNone count = %d, want 1", counts[SkipTypeNone])
	}
	if counts[SkipTypeRemote] != 1 {
		t.Errorf("SkipTypeRemote count = %d, want 1", counts[SkipTypeRemote])
	}
	if counts[SkipTypeDatabaseJoin] != 1 {
		t.Errorf("SkipTypeDatabaseJoin count = %d, want 1", counts[SkipTypeDatabaseJoin])
	}
}

// TestSelectFieldsForDatabaseJoin verifies a Select configured for DB join.
func TestSelectFieldsForDatabaseJoin(t *testing.T) {
	// A typical cross-database child select
	sel := Select{
		Field: Field{
			ID:         1,
			ParentID:   0,
			FieldName:  "orders",
			SkipRender: SkipTypeDatabaseJoin,
		},
		Table:    "orders",
		Database: "analytics",
	}

	if sel.ParentID != 0 {
		t.Errorf("ParentID = %d, want 0", sel.ParentID)
	}
	if sel.Database != "analytics" {
		t.Errorf("Database = %q, want %q", sel.Database, "analytics")
	}
	if sel.SkipRender != SkipTypeDatabaseJoin {
		t.Errorf("SkipRender = %v, want %v", sel.SkipRender, SkipTypeDatabaseJoin)
	}
}

// TestSelectTiDatabaseField verifies Ti.Database is accessible.
func TestSelectTiDatabaseField(t *testing.T) {
	sel := Select{
		Table:    "orders",
		Database: "analytics",
		Ti: sdata.DBTable{
			Name:     "orders",
			Database: "analytics",
		},
	}

	if sel.Ti.Database != "analytics" {
		t.Errorf("Ti.Database = %q, want %q", sel.Ti.Database, "analytics")
	}
}

// TestAddRelColumnsForDatabaseJoin verifies that addRelColumns handles
// RelDatabaseJoin correctly: adds a placeholder field to the parent select,
// sets SkipRender to SkipTypeDatabaseJoin, and sets the Database field.
func TestAddRelColumnsForDatabaseJoin(t *testing.T) {
	// Create a minimal compiler (addRelColumns doesn't use the compiler's fields)
	co := &Compiler{}

	// Set up parent and child selects
	parentSel := Select{
		Field: Field{ID: 0, FieldName: "users"},
		Table: "users",
		Fields: []Field{},
		BCols:  []Column{},
	}
	childSel := Select{
		Field: Field{ID: 1, ParentID: 0, FieldName: "orders"},
		Table: "orders",
		Ti:    sdata.DBTable{Name: "orders", Database: "analytics"},
		Fields: []Field{},
		BCols:  []Column{},
		Rel: sdata.DBRel{
			Type: sdata.RelDatabaseJoin,
			Right: sdata.DBRelRight{
				Col: sdata.DBColumn{Name: "user_id", Table: "users", Schema: "public"},
			},
		},
	}

	qc := &QCode{
		Selects: []Select{parentSel, childSel},
	}

	err := co.addRelColumns(qc, &qc.Selects[1], qc.Selects[1].Rel)
	if err != nil {
		t.Fatalf("addRelColumns() error: %v", err)
	}

	// Verify parent select got a placeholder field with the right name
	expectedPlaceholder := fmt.Sprintf("__%s_db_join", "orders")
	foundPlaceholder := false
	for _, f := range qc.Selects[0].Fields {
		if f.FieldName == expectedPlaceholder {
			foundPlaceholder = true
			if f.Col.Name != "user_id" {
				t.Errorf("placeholder field Col.Name = %q, want %q", f.Col.Name, "user_id")
			}
			break
		}
	}
	if !foundPlaceholder {
		t.Errorf("parent select missing placeholder field %q; fields: %v",
			expectedPlaceholder, qc.Selects[0].Fields)
	}

	// Verify child select has SkipRender set to SkipTypeDatabaseJoin
	if qc.Selects[1].SkipRender != SkipTypeDatabaseJoin {
		t.Errorf("child SkipRender = %v, want %v", qc.Selects[1].SkipRender, SkipTypeDatabaseJoin)
	}

	// Verify child select has Database set
	if qc.Selects[1].Database != "analytics" {
		t.Errorf("child Database = %q, want %q", qc.Selects[1].Database, "analytics")
	}
}
