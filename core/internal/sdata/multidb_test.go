package sdata

import "testing"

// TestDBTableDatabaseField verifies that DBTable has a Database field.
func TestDBTableDatabaseField(t *testing.T) {
	table := DBTable{
		Name:     "users",
		Schema:   "public",
		Database: "main",
	}

	if table.Database != "main" {
		t.Errorf("Database = %q, want %q", table.Database, "main")
	}
}

// TestIsCrossDatabase verifies the IsCrossDatabase method on DBRel.
func TestIsCrossDatabase(t *testing.T) {
	tests := []struct {
		name     string
		leftDB   string
		rightDB  string
		wantCross bool
	}{
		{
			name:      "both empty (same default)",
			leftDB:   "",
			rightDB:  "",
			wantCross: false,
		},
		{
			name:      "left empty, right set",
			leftDB:   "",
			rightDB:  "analytics",
			wantCross: false, // Empty means default, not cross-DB
		},
		{
			name:      "left set, right empty",
			leftDB:   "main",
			rightDB:  "",
			wantCross: false, // Empty means default, not cross-DB
		},
		{
			name:      "same database",
			leftDB:   "main",
			rightDB:  "main",
			wantCross: false,
		},
		{
			name:      "different databases",
			leftDB:   "main",
			rightDB:  "analytics",
			wantCross: true,
		},
		{
			name:      "case sensitive",
			leftDB:   "Main",
			rightDB:  "main",
			wantCross: true, // Case matters
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rel := DBRel{
				Left: DBRelLeft{
					Ti: DBTable{Database: tt.leftDB},
				},
				Right: DBRelRight{
					Ti: DBTable{Database: tt.rightDB},
				},
			}

			got := rel.IsCrossDatabase()
			if got != tt.wantCross {
				t.Errorf("IsCrossDatabase() = %v, want %v", got, tt.wantCross)
			}
		})
	}
}

// TestRelDatabaseJoinType verifies RelDatabaseJoin is defined correctly.
func TestRelDatabaseJoinType(t *testing.T) {
	// Verify RelDatabaseJoin has a reasonable value (not 0 which is RelNone)
	if RelDatabaseJoin == RelNone {
		t.Error("RelDatabaseJoin should not equal RelNone")
	}

	// Verify it's distinct from other types
	types := []RelType{
		RelNone,
		RelOneToOne,
		RelOneToMany,
		RelPolymorphic,
		RelRecursive,
		RelEmbedded,
		RelRemote,
		RelSkip,
	}

	for _, rt := range types {
		if rt == RelDatabaseJoin {
			t.Errorf("RelDatabaseJoin should be distinct from %v", rt)
		}
	}
}

// TestRelTypeString verifies String() for RelDatabaseJoin.
func TestRelTypeString(t *testing.T) {
	s := RelDatabaseJoin.String()
	if s != "RelDatabaseJoin" {
		t.Errorf("RelDatabaseJoin.String() = %q, want %q", s, "RelDatabaseJoin")
	}
}

// TestDBRelCrossDatabaseWithRelType verifies IsCrossDatabase with different rel types.
func TestDBRelCrossDatabaseWithRelType(t *testing.T) {
	// Even with RelDatabaseJoin type, IsCrossDatabase should look at actual databases
	rel := DBRel{
		Type: RelDatabaseJoin,
		Left: DBRelLeft{
			Ti: DBTable{Database: "main"},
		},
		Right: DBRelRight{
			Ti: DBTable{Database: "analytics"},
		},
	}

	if !rel.IsCrossDatabase() {
		t.Error("IsCrossDatabase() should return true for different databases")
	}

	// Same database should return false even if type is RelDatabaseJoin
	rel2 := DBRel{
		Type: RelDatabaseJoin,
		Left: DBRelLeft{
			Ti: DBTable{Database: "main"},
		},
		Right: DBRelRight{
			Ti: DBTable{Database: "main"},
		},
	}

	if rel2.IsCrossDatabase() {
		t.Error("IsCrossDatabase() should return false for same database")
	}
}

// TestDBTableWithDatabaseInTestDBInfo checks test helpers include Database field.
func TestDBTableWithDatabaseInTestDBInfo(t *testing.T) {
	// GetTestDBInfo returns test schema
	dbinfo := GetTestDBInfo()

	// Tables should have Database field available (even if empty for default)
	for _, table := range dbinfo.Tables {
		// This test just verifies the field exists and doesn't panic
		_ = table.Database
	}
}

// TestCrossDBRelStoredAsMetadata verifies that cross-database FKs are stored
// as CrossDBRel metadata (not as shadow nodes in the graph), and that
// FindCrossDBPath resolves them correctly.
func TestCrossDBRelStoredAsMetadata(t *testing.T) {
	// Create a DBInfo with a table that has a cross-database FK
	cols := []DBColumn{
		{Schema: "public", Table: "job_crew", Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true, UniqueKey: true},
		{Schema: "public", Table: "job_crew", Name: "employee_id", Type: "integer",
			FKeyDatabase: "ats", FKeySchema: "public", FKeyTable: "employees", FKeyCol: "id",
			FKeyIsUnique: true},
	}

	di := NewDBInfo("postgres", 140000, "public", "ats_orders", cols, nil, nil)

	// Tag the table with its database
	for i := range di.Tables {
		di.Tables[i].Database = "ats_orders"
	}

	schema, err := NewDBSchema(di, nil)
	if err != nil {
		t.Fatalf("NewDBSchema() error: %v", err)
	}

	// The remote table should NOT exist as a node in the graph
	_, err = schema.Find("public", "employees")
	if err == nil {
		t.Fatal("expected 'employees' to NOT be a graph node (should be cross-DB metadata only)")
	}

	// Cross-DB relationship should be stored in metadata
	rels := schema.GetCrossDBRels()
	if len(rels) != 1 {
		t.Fatalf("expected 1 cross-DB rel, got %d", len(rels))
	}
	rel := rels[0]
	if rel.TargetDB != "ats" {
		t.Errorf("TargetDB = %q, want %q", rel.TargetDB, "ats")
	}
	if rel.TargetTable != "employees" {
		t.Errorf("TargetTable = %q, want %q", rel.TargetTable, "employees")
	}
	if !rel.IsOneToOne {
		t.Error("expected IsOneToOne = true for FK to PK column")
	}

	// FindCrossDBPath should resolve the relationship
	tp, ok := schema.FindCrossDBPath("employees", "job_crew")
	if !ok {
		t.Fatal("FindCrossDBPath() did not find cross-DB path")
	}

	// Verify the TPath has correct database info for IsCrossDatabase
	dbRel := PathToRel(tp)
	if !dbRel.IsCrossDatabase() {
		t.Error("expected IsCrossDatabase() = true for cross-database FK relationship")
	}
	if dbRel.Right.Ti.Database != "ats" {
		t.Errorf("Right.Ti.Database = %q, want %q", dbRel.Right.Ti.Database, "ats")
	}
}

// TestCrossDBRelNoGraphPollution verifies that cross-database FKs don't
// add phantom tables to GetTables() or GetFirstDegree().
func TestCrossDBRelNoGraphPollution(t *testing.T) {
	cols := []DBColumn{
		{Schema: "public", Table: "job_crew", Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true, UniqueKey: true},
		{Schema: "public", Table: "job_crew", Name: "employee_id", Type: "integer",
			FKeyDatabase: "ats", FKeySchema: "public", FKeyTable: "employees", FKeyCol: "id"},
	}

	di := NewDBInfo("postgres", 140000, "public", "ats_orders", cols, nil, nil)
	for i := range di.Tables {
		di.Tables[i].Database = "ats_orders"
	}

	schema, err := NewDBSchema(di, nil)
	if err != nil {
		t.Fatalf("NewDBSchema() error: %v", err)
	}

	// Only the local table should appear in GetTables
	tables := schema.GetTables()
	for _, tbl := range tables {
		if tbl.Name == "employees" {
			t.Error("remote table 'employees' should not appear in GetTables()")
		}
	}
}

// TestCrossDBRelWithLocalCollision verifies that when a local table has the
// same name as the cross-DB FK target, the cross-DB relationship still works
// and doesn't interfere with the local table.
func TestCrossDBRelWithLocalCollision(t *testing.T) {
	cols := []DBColumn{
		// Local employees table
		{Schema: "public", Table: "employees", Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true, UniqueKey: true},
		{Schema: "public", Table: "employees", Name: "name", Type: "text"},
		// job_crew table with cross-DB FK to ats:public.employees
		{Schema: "public", Table: "job_crew", Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true, UniqueKey: true},
		{Schema: "public", Table: "job_crew", Name: "employee_id", Type: "integer",
			FKeyDatabase: "ats", FKeySchema: "public", FKeyTable: "employees", FKeyCol: "id",
			FKeyIsUnique: true},
	}

	di := NewDBInfo("postgres", 140000, "public", "ats_orders", cols, nil, nil)
	for i := range di.Tables {
		di.Tables[i].Database = "ats_orders"
	}

	schema, err := NewDBSchema(di, nil)
	if err != nil {
		t.Fatalf("NewDBSchema() error: %v", err)
	}

	// Local employees table should still be findable
	localEmp, err := schema.Find("public", "employees")
	if err != nil {
		t.Fatalf("local employees table not found: %v", err)
	}
	if localEmp.Database != "ats_orders" {
		t.Errorf("local employees.Database = %q, want %q", localEmp.Database, "ats_orders")
	}

	// Cross-DB path should still resolve to the REMOTE employees
	tp, ok := schema.FindCrossDBPath("employees", "job_crew")
	if !ok {
		t.Fatal("FindCrossDBPath() should find cross-DB path even with local name collision")
	}
	if tp.RT.Database != "ats" {
		t.Errorf("cross-DB path target Database = %q, want %q", tp.RT.Database, "ats")
	}
}

// TestFKeyDatabaseFieldOnDBColumn verifies the FKeyDatabase field exists and works.
func TestFKeyDatabaseFieldOnDBColumn(t *testing.T) {
	col := DBColumn{
		Name:         "employee_id",
		FKeyDatabase: "ats",
		FKeySchema:   "public",
		FKeyTable:    "employees",
		FKeyCol:      "id",
	}

	if col.FKeyDatabase != "ats" {
		t.Errorf("FKeyDatabase = %q, want %q", col.FKeyDatabase, "ats")
	}
}
