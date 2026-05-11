package serv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3"
)

func TestCodeSQLMultiDBInitializesManagedSQLiteRuntime(t *testing.T) {
	source := t.TempDir()
	writeTestFile(t, filepath.Join(source, "main.go"), `package main

func Handler() {}
`)

	conf := &Config{
		Core: Core{
			Databases: map[string]core.DatabaseConfig{
				"code": {Type: "codesql", Path: source},
			},
		},
		Serv: Serv{
			ConfigPath: filepath.Join(t.TempDir(), "config"),
			MCP:        MCPConfig{Disable: true},
		},
	}

	s, err := newGraphJinService(conf, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestService(s)

	if got := s.conf.Core.Databases["code"].Type; got != "codesql" {
		t.Fatalf("logical config type = %q, want codesql", got)
	}
	runtime := s.runtimeCore.Databases["code"]
	if runtime.Type != "sqlite" {
		t.Fatalf("runtime type = %q, want sqlite", runtime.Type)
	}
	if !runtime.ReadOnly {
		t.Fatalf("runtime read_only = false, want true")
	}
	if runtime.AnalyticsMode == nil || !*runtime.AnalyticsMode {
		t.Fatalf("runtime analytics_mode = %v, want true", runtime.AnalyticsMode)
	}
	if !strings.HasPrefix(filepath.Base(runtime.Path), "code-") {
		t.Fatalf("cache filename = %q, want database-name prefix", filepath.Base(runtime.Path))
	}
	assertGraphJinTable(t, s, "code", "code_symbols")
	assertServiceCount(t, s, "code", `SELECT count(*) FROM code_symbols WHERE name = 'Handler'`, 1)
}

func TestCodeSQLLegacyUsesDefaultCachePrefix(t *testing.T) {
	source := t.TempDir()
	writeTestFile(t, filepath.Join(source, "main.go"), `package main

func Legacy() {}
`)

	conf := &Config{
		Serv: Serv{
			ConfigPath: filepath.Join(t.TempDir(), "config"),
			DB:         Database{Type: "codesql", Path: source},
			MCP:        MCPConfig{Disable: true},
		},
	}

	s, err := newGraphJinService(conf, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestService(s)

	runtime := s.runtimeCore.Databases[core.DefaultDBName]
	if runtime.Type != "sqlite" {
		t.Fatalf("runtime type = %q, want sqlite", runtime.Type)
	}
	if !strings.HasPrefix(filepath.Base(runtime.Path), "default-") {
		t.Fatalf("cache filename = %q, want default prefix", filepath.Base(runtime.Path))
	}
	assertGraphJinTable(t, s, core.DefaultDBName, "code_symbols")
	assertServiceCount(t, s, core.DefaultDBName, `SELECT count(*) FROM code_symbols WHERE name = 'Legacy'`, 1)
}

func writeTestFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertServiceCount(t *testing.T, s *graphjinService, dbName, query string, want int) {
	t.Helper()
	db := s.dbs[dbName]
	if db == nil {
		t.Fatalf("database %q not connected", dbName)
	}
	var got int
	if err := db.QueryRow(query).Scan(&got); err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if got != want {
		t.Fatalf("count = %d, want %d", got, want)
	}
}

func assertGraphJinTable(t *testing.T, s *graphjinService, dbName, tableName string) {
	t.Helper()
	if s.gj == nil || !s.gj.SchemaReady() {
		t.Fatalf("GraphJin schema is not ready")
	}
	for _, table := range s.gj.GetTablesForDatabase(dbName) {
		if table.Name == tableName {
			return
		}
	}
	t.Fatalf("GraphJin did not discover table %q", tableName)
}

func closeTestService(s *graphjinService) {
	if s.gj != nil {
		s.gj.Close()
	}
	closed := s.closeManagedDBs(nil)
	for name, db := range s.dbs {
		if _, ok := closed[name]; ok {
			continue
		}
		db.Close() //nolint:errcheck
	}
}
