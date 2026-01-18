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
