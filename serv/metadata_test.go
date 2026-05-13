package serv

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3"
)

func TestMetadataGraphPopulatesAndLinksCodeSQLRefs(t *testing.T) {
	appPath := createMetadataAppDB(t)
	source := t.TempDir()
	writeTestFile(t, filepath.Join(source, "main.go"), `package main

func LookupUser() {
	query := "SELECT users.email FROM users WHERE users.id = ?"
	_ = query
}
`)

	conf := &Config{
		Core: Core{
			DisableAllowList: true,
			Databases: map[string]core.DatabaseConfig{
				"app":  {Type: "sqlite", Path: appPath},
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

	if s.metadataDB != defaultMetadataDBName {
		t.Fatalf("metadata database = %q, want %q", s.metadataDB, defaultMetadataDBName)
	}
	runtime := s.runtimeCore.Databases[defaultMetadataDBName]
	if runtime.Type != "sqlite" || !runtime.ReadOnly || runtime.AnalyticsMode == nil || !*runtime.AnalyticsMode {
		t.Fatalf("metadata runtime = %+v, want read-only analytics sqlite", runtime)
	}
	assertGraphJinTable(t, s, defaultMetadataDBName, "gj_columns")
	assertServiceCount(t, s, defaultMetadataDBName, `SELECT count(*) FROM gj_columns WHERE database_name = 'app' AND schema_name = 'main' AND table_name = 'users' AND column_name = 'email'`, 1)
	assertServiceCount(t, s, defaultMetadataDBName, `SELECT count(*) FROM gj_databases WHERE name IN ('code', 'graphjin')`, 0)
	assertServiceCount(t, s, defaultMetadataDBName, `SELECT count(*) FROM gj_tables WHERE database_name IN ('code', 'graphjin') OR table_name LIKE 'code_%' OR table_name LIKE 'gj_%'`, 0)
	assertServiceCount(t, s, defaultMetadataDBName, `SELECT count(*) FROM gj_relationships WHERE from_database_name IN ('code', 'graphjin') OR to_database_name IN ('code', 'graphjin')`, 0)
	assertServiceCount(t, s, "code", `SELECT count(*) FROM code_db_refs WHERE column_key = 'app:main.users.email' AND resolved = 1`, 1)

	if !hasRuntimeRelationship(s, defaultMetadataDBName, "gj_columns", "code_db_refs_id", "code:code_db_refs.column_key") {
		t.Fatalf("missing automatic gj_columns -> code_db_refs relationship")
	}
}

func TestMetadataAutoCodeRelationsCanBeDisabled(t *testing.T) {
	appPath := createMetadataAppDB(t)
	source := t.TempDir()
	writeTestFile(t, filepath.Join(source, "main.go"), `package main

func LookupUser() {
	query := "SELECT users.email FROM users"
	_ = query
}
`)
	enabled := true
	autoCodeRelations := false
	conf := &Config{
		Core: Core{
			DisableAllowList: true,
			Metadata: core.MetadataConfig{
				Enabled:           &enabled,
				AutoCodeRelations: &autoCodeRelations,
			},
			Databases: map[string]core.DatabaseConfig{
				"app":  {Type: "sqlite", Path: appPath},
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

	assertGraphJinTable(t, s, defaultMetadataDBName, "gj_columns")
	assertServiceCount(t, s, "code", `SELECT count(*) FROM code_db_refs WHERE column_key = 'app:main.users.email' AND resolved = 1`, 1)
	if hasRuntimeRelationship(s, defaultMetadataDBName, "gj_columns", "code_db_refs_id", "code:code_db_refs.column_key") {
		t.Fatalf("automatic gj_columns -> code_db_refs relationship was injected while disabled")
	}
}

func TestMetadataGraphDisabledInProductionByDefault(t *testing.T) {
	appPath := createMetadataAppDB(t)
	conf := &Config{
		Core: Core{
			DisableAllowList: true,
			Databases: map[string]core.DatabaseConfig{
				"app": {Type: "sqlite", Path: appPath},
			},
		},
		Serv: Serv{
			Production: true,
			ConfigPath: filepath.Join(t.TempDir(), "config"),
			MCP:        MCPConfig{Disable: true},
		},
	}

	s, err := newGraphJinService(conf, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestService(s)

	if s.metadataDB != "" {
		t.Fatalf("metadata database = %q, want disabled", s.metadataDB)
	}
	if _, ok := s.runtimeCore.Databases[defaultMetadataDBName]; ok {
		t.Fatalf("metadata runtime database should not be created in production by default")
	}
}

func TestMetadataDatabaseNameCollisionFailsStartup(t *testing.T) {
	appPath := createMetadataAppDB(t)
	conf := &Config{
		Core: Core{
			DisableAllowList: true,
			Databases: map[string]core.DatabaseConfig{
				defaultMetadataDBName: {Type: "sqlite", Path: appPath},
			},
		},
		Serv: Serv{
			ConfigPath: filepath.Join(t.TempDir(), "config"),
			MCP:        MCPConfig{Disable: true},
		},
	}

	_, err := newGraphJinService(conf, nil)
	if err == nil || !strings.Contains(err.Error(), `metadata database "graphjin" collides`) {
		t.Fatalf("startup error = %v, want metadata collision", err)
	}
}

func createMetadataAppDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "app.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE users (
		id integer primary key,
		email text not null unique,
		team_id integer
	);
	CREATE INDEX idx_users_team_id ON users(team_id);`); err != nil {
		t.Fatal(err)
	}
	return path
}

func hasRuntimeRelationship(s *graphjinService, database, table, column, target string) bool {
	if s.runtimeCore == nil {
		return false
	}
	for _, t := range s.runtimeCore.Tables {
		if t.Database != database || t.Name != table {
			continue
		}
		for _, c := range t.Columns {
			if c.Name == column && c.ForeignKey == target {
				return true
			}
		}
	}
	return false
}
