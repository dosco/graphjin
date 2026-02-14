package serv

import (
	"sort"
	"strings"
	"testing"
)

// =============================================================================
// systemDatabases tests
// =============================================================================

func TestSystemDatabases(t *testing.T) {
	tests := []struct {
		dbType   string
		expected []string
	}{
		{"postgres", []string{"postgres"}},
		{"mysql", []string{"information_schema", "mysql", "performance_schema", "sys"}},
		{"mariadb", []string{"information_schema", "mysql", "performance_schema", "sys"}},
		{"mssql", []string{"master", "model", "msdb", "tempdb"}},
		{"oracle", []string{"DBSNMP", "OUTLN", "SYS", "SYSTEM", "XDB"}},
		{"mongodb", []string{"admin", "config", "local"}},
	}

	for _, tc := range tests {
		t.Run(tc.dbType, func(t *testing.T) {
			sysDBs := systemDatabases(tc.dbType)
			if len(sysDBs) != len(tc.expected) {
				t.Fatalf("expected %d system databases for %s, got %d", len(tc.expected), tc.dbType, len(sysDBs))
			}
			for _, name := range tc.expected {
				if !sysDBs[name] {
					t.Errorf("expected %q to be a system database for %s", name, tc.dbType)
				}
			}
		})
	}

	t.Run("unknown type returns nil", func(t *testing.T) {
		if got := systemDatabases("cockroachdb"); got != nil {
			t.Errorf("expected nil for unknown type, got %v", got)
		}
	})

	t.Run("sqlite returns nil", func(t *testing.T) {
		if got := systemDatabases("sqlite"); got != nil {
			t.Errorf("expected nil for sqlite, got %v", got)
		}
	})
}

// =============================================================================
// isSystemDatabase tests
// =============================================================================

func TestIsSystemDatabase(t *testing.T) {
	tests := []struct {
		name     string
		dbType   string
		dbName   string
		expected bool
	}{
		// postgres
		{"postgres system", "postgres", "postgres", true},
		{"postgres user db", "postgres", "myapp", false},
		{"postgres case insensitive", "postgres", "POSTGRES", true},

		// mysql
		{"mysql information_schema", "mysql", "information_schema", true},
		{"mysql mysql", "mysql", "mysql", true},
		{"mysql performance_schema", "mysql", "performance_schema", true},
		{"mysql sys", "mysql", "sys", true},
		{"mysql user db", "mysql", "myapp", false},

		// mssql
		{"mssql master", "mssql", "master", true},
		{"mssql tempdb", "mssql", "tempdb", true},
		{"mssql model", "mssql", "model", true},
		{"mssql msdb", "mssql", "msdb", true},
		{"mssql user db", "mssql", "myapp", false},

		// oracle (case insensitive — uppercased for comparison)
		{"oracle SYS", "oracle", "SYS", true},
		{"oracle sys lowercase", "oracle", "sys", true},
		{"oracle user db", "oracle", "myapp", false},

		// mongodb
		{"mongodb admin", "mongodb", "admin", true},
		{"mongodb config", "mongodb", "config", true},
		{"mongodb local", "mongodb", "local", true},
		{"mongodb user db", "mongodb", "myapp", false},

		// edge cases
		{"unknown type always false", "cockroachdb", "anything", false},
		{"empty name", "postgres", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isSystemDatabase(tc.dbType, tc.dbName)
			if got != tc.expected {
				t.Errorf("isSystemDatabase(%q, %q) = %v, want %v", tc.dbType, tc.dbName, got, tc.expected)
			}
		})
	}
}

// =============================================================================
// filterSystemDatabases tests
// =============================================================================

func TestFilterSystemDatabases(t *testing.T) {
	t.Run("postgres filters system db", func(t *testing.T) {
		input := []string{"postgres", "myapp", "myapp_test"}
		got := filterSystemDatabases("postgres", input)
		expected := []string{"myapp", "myapp_test"}
		if len(got) != len(expected) {
			t.Fatalf("expected %v, got %v", expected, got)
		}
		for i := range expected {
			if got[i] != expected[i] {
				t.Errorf("index %d: expected %q, got %q", i, expected[i], got[i])
			}
		}
	})

	t.Run("mysql filters all system dbs", func(t *testing.T) {
		input := []string{"information_schema", "mysql", "performance_schema", "sys", "myapp"}
		got := filterSystemDatabases("mysql", input)
		if len(got) != 1 || got[0] != "myapp" {
			t.Errorf("expected [myapp], got %v", got)
		}
	})

	t.Run("all filtered returns empty", func(t *testing.T) {
		input := []string{"postgres"}
		got := filterSystemDatabases("postgres", input)
		if len(got) != 0 {
			t.Errorf("expected empty slice, got %v", got)
		}
	})

	t.Run("none filtered returns same", func(t *testing.T) {
		input := []string{"myapp", "other"}
		got := filterSystemDatabases("postgres", input)
		if len(got) != 2 || got[0] != "myapp" || got[1] != "other" {
			t.Errorf("expected [myapp other], got %v", got)
		}
	})

	t.Run("empty input returns empty", func(t *testing.T) {
		got := filterSystemDatabases("postgres", []string{})
		if len(got) != 0 {
			t.Errorf("expected empty, got %v", got)
		}
	})

	t.Run("nil input returns nil", func(t *testing.T) {
		got := filterSystemDatabases("postgres", nil)
		if got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("unknown type returns input unchanged", func(t *testing.T) {
		input := []string{"anything", "else"}
		got := filterSystemDatabases("cockroachdb", input)
		if len(got) != len(input) {
			t.Errorf("expected input unchanged, got %v", got)
		}
	})
}

// =============================================================================
// systemDatabaseError tests
// =============================================================================

func TestSystemDatabaseError(t *testing.T) {
	t.Run("contains rejected db name", func(t *testing.T) {
		msg := systemDatabaseError("postgres", "postgres")
		if !strings.Contains(msg, "'postgres'") {
			t.Errorf("error should contain rejected db name, got: %s", msg)
		}
	})

	t.Run("contains db type", func(t *testing.T) {
		msg := systemDatabaseError("mysql", "mysql")
		if !strings.Contains(msg, "mysql") {
			t.Errorf("error should contain db type, got: %s", msg)
		}
	})

	t.Run("contains config hint", func(t *testing.T) {
		msg := systemDatabaseError("postgres", "postgres")
		if !strings.Contains(msg, "mcp.default_db_allowed") {
			t.Errorf("error should contain config hint, got: %s", msg)
		}
	})

	t.Run("lists system databases for type", func(t *testing.T) {
		msg := systemDatabaseError("mssql", "master")
		for _, name := range []string{"master", "model", "msdb", "tempdb"} {
			if !strings.Contains(msg, name) {
				t.Errorf("error should contain system db %q, got: %s", name, msg)
			}
		}
	})

	t.Run("deterministic order", func(t *testing.T) {
		// Call multiple times and verify consistent output
		first := systemDatabaseError("mysql", "mysql")
		for i := 0; i < 10; i++ {
			got := systemDatabaseError("mysql", "mysql")
			if got != first {
				t.Errorf("non-deterministic output:\nfirst: %s\ngot:   %s", first, got)
			}
		}

		// Verify the system DB names appear in sorted order in the message
		sysDBs := systemDatabases("mysql")
		var names []string
		for n := range sysDBs {
			names = append(names, n)
		}
		sort.Strings(names)
		sorted := strings.Join(names, ", ")
		if !strings.Contains(first, sorted) {
			t.Errorf("system databases should be sorted in error message.\nExpected to contain: %s\nGot: %s", sorted, first)
		}
	})
}
