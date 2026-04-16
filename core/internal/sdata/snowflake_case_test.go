package sdata

import (
	"strings"
	"testing"
)

// These are pure unit tests — they build DBInfo / DBTable structs in memory
// and never connect to a database. The identifiers simulate how Snowflake
// stores unquoted objects (UPPERCASE) so we can verify case-insensitive
// lookup works for GraphQL callers who write queries in lowercase. Table
// and column names mirror the standard test schema (users, products) used
// by the integration test harness in tests/snowflake.sql.

// TestCaseInsensitiveColumnLookup verifies DBTable.getColumn (used by
// ColumnExists and the query compiler) matches columns case-insensitively.
// This is what lets GraphQL queries use a single identifier style across
// case-preserving backends (Snowflake UPPERCASE, Oracle UPPERCASE) without
// the caller having to switch casing per backend.
func TestCaseInsensitiveColumnLookup(t *testing.T) {
	ti := NewDBTable("PUBLIC", "USERS", "", []DBColumn{
		{Schema: "PUBLIC", Table: "USERS", Name: "ID", Type: "bigint", PrimaryKey: true},
		{Schema: "PUBLIC", Table: "USERS", Name: "FULL_NAME", Type: "varchar"},
	})

	tests := []struct {
		name, input, wantName string
		wantFound             bool
	}{
		{"exact uppercase", "ID", "ID", true},
		{"lowercase fallback", "id", "ID", true},
		{"subject mixed case fallback", "Full_Name", "FULL_NAME", true},
		{"wacky case fallback", "fUlL_nAmE", "FULL_NAME", true},
		{"unknown column not found", "does_not_exist", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ti.ColumnExists(tt.input)
			if ok != tt.wantFound {
				t.Fatalf("ColumnExists(%q) found = %v, want %v", tt.input, ok, tt.wantFound)
			}
			if !tt.wantFound {
				return
			}
			if got.Name != tt.wantName {
				t.Errorf("ColumnExists(%q).Name = %q, want %q (storage case must be preserved)",
					tt.input, got.Name, tt.wantName)
			}
		})
	}
}

// TestCaseInsensitiveColumnCollisionPrefersExact documents collision
// resolution: when two columns differ only in case (`ID` vs `Id`), an
// exact-case lookup always returns its exact match — never falls back to
// the case-insensitive scan. For non-exact input, the current implementation
// returns an arbitrary case-insensitive match (map iteration order). This
// test documents today's behavior so a future ambiguity-error change has a
// clear regression boundary.
func TestCaseInsensitiveColumnCollisionPrefersExact(t *testing.T) {
	ti := NewDBTable("PUBLIC", "T", "", []DBColumn{
		{Schema: "PUBLIC", Table: "T", Name: "ID", Type: "bigint"},
		{Schema: "PUBLIC", Table: "T", Name: "Id", Type: "varchar"},
		{Schema: "PUBLIC", Table: "T", Name: "other", Type: "text"},
	})

	// Exact-case lookups MUST NOT fall back.
	if got, ok := ti.ColumnExists("ID"); !ok || got.Name != "ID" || got.Type != "bigint" {
		t.Errorf(`ColumnExists("ID") = (%+v, %v); want exact ID:bigint`, got, ok)
	}
	if got, ok := ti.ColumnExists("Id"); !ok || got.Name != "Id" || got.Type != "varchar" {
		t.Errorf(`ColumnExists("Id") = (%+v, %v); want exact Id:varchar`, got, ok)
	}

	// Case-folded input finds one of them.
	if got, ok := ti.ColumnExists("id"); !ok || !strings.EqualFold(got.Name, "id") {
		t.Errorf(`ColumnExists("id") = (%+v, %v); want any case-fold match of "id"`, got, ok)
	}
}

// TestCaseInsensitiveTableLookup asserts DBSchema.Find handles
// lowercase/uppercase table names. Snowflake stores `USERS` upstream but
// users write `{ users { ... } }`.
func TestCaseInsensitiveTableLookup(t *testing.T) {
	di := NewDBInfo("snowflake", 0, "PUBLIC", "db", []DBColumn{
		{Schema: "PUBLIC", Table: "USERS", Name: "ID", Type: "bigint", PrimaryKey: true},
	}, nil, nil)
	sch, err := NewDBSchema(di, nil)
	if err != nil {
		t.Fatalf("NewDBSchema: %v", err)
	}

	// Exact case
	if tbl, err := sch.Find("PUBLIC", "USERS"); err != nil {
		t.Errorf("Find(exact) err = %v", err)
	} else if tbl.Name != "USERS" {
		t.Errorf("Find(exact).Name = %q, want USERS", tbl.Name)
	}

	// Lowercase GraphQL-style input resolves via case-insensitive fallback.
	if tbl, err := sch.Find("", "users"); err != nil {
		t.Errorf("Find(lowercase) err = %v", err)
	} else if tbl.Name != "USERS" {
		t.Errorf("Find(lowercase).Name = %q, want USERS (storage case preserved)", tbl.Name)
	}
}

// TestDBInfoGetColumnCaseInsensitive asserts the DBInfo-level column lookup
// also falls back to case-insensitive — this is the backing store for
// GetColumnIndex and the outer schema's column accessors.
func TestDBInfoGetColumnCaseInsensitive(t *testing.T) {
	di := NewDBInfo("snowflake", 0, "PUBLIC", "db", []DBColumn{
		{Schema: "PUBLIC", Table: "USERS", Name: "ID", Type: "bigint", PrimaryKey: true},
		{Schema: "PUBLIC", Table: "USERS", Name: "FULL_NAME", Type: "varchar"},
	}, nil, nil)

	tests := []struct {
		name, input, wantName string
	}{
		{"exact", "ID", "ID"},
		{"lowercase", "id", "ID"},
		{"mixed", "Full_Name", "FULL_NAME"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := di.GetColumn("PUBLIC", "USERS", tt.input)
			if err != nil {
				t.Fatalf("GetColumn(%q) err = %v", tt.input, err)
			}
			if got.Name != tt.wantName {
				t.Errorf("GetColumn(%q).Name = %q, want %q", tt.input, got.Name, tt.wantName)
			}
		})
	}
}
