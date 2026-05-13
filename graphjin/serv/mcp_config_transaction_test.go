package serv

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/dosco/graphjin/core/v3"
	"github.com/spf13/afero"
	"github.com/spf13/viper"
	"go.opentelemetry.io/otel"
	"go.uber.org/zap/zaptest"
	_ "modernc.org/sqlite"
)

func TestHandleUpdateCurrentConfig_TransactionalFailureLeavesLiveStateUntouched(t *testing.T) {
	livePath := createSQLiteDBFile(t, "live.sqlite3", true)
	emptyPath := createSQLiteDBFile(t, "empty.sqlite3", false)
	ms := newTransactionalConfigMCPServer(t, livePath)

	oldGJ := ms.service.gj
	oldDB := ms.service.dbs["main"]
	oldPath := ms.service.conf.Core.Databases["main"].Path

	res, err := ms.handleUpdateCurrentConfig(context.Background(), newToolRequest(map[string]any{
		"databases": map[string]any{
			"main": map[string]any{
				"type": "sqlite",
				"path": emptyPath,
			},
		},
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out ConfigUpdateResult
	if err := json.Unmarshal([]byte(assertToolSuccess(t, res)), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Success {
		t.Fatalf("expected staged update to fail, got %+v", out)
	}
	if ms.service.gj != oldGJ {
		t.Fatal("expected live GraphJin instance to remain unchanged on staged failure")
	}
	if ms.service.dbs["main"] != oldDB {
		t.Fatal("expected live database handle to remain unchanged on staged failure")
	}
	if got := ms.service.conf.Core.Databases["main"].Path; got != oldPath {
		t.Fatalf("expected live config path %q to remain unchanged, got %q", oldPath, got)
	}
	if err := oldDB.Ping(); err != nil {
		t.Fatalf("expected original database handle to remain open, ping failed: %v", err)
	}
}

func TestHandleUpdateCurrentConfig_StagedFailureDoesNotSaveConfigFile(t *testing.T) {
	livePath := createSQLiteDBFile(t, "live.sqlite3", true)
	emptyPath := createSQLiteDBFile(t, "empty.sqlite3", false)

	confPath := filepath.Join(t.TempDir(), "dev.yml")
	before := []byte("app_name: test\n")
	if err := os.WriteFile(confPath, before, 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	v := viper.New()
	v.SetConfigFile(confPath)
	v.SetConfigType("yaml")
	if err := v.ReadInConfig(); err != nil {
		t.Fatalf("read config file: %v", err)
	}

	ms := newTransactionalConfigMCPServerWithOptions(t, livePath, false, v)
	res, err := ms.handleUpdateCurrentConfig(context.Background(), newToolRequest(map[string]any{
		"databases": map[string]any{
			"main": map[string]any{
				"type": "sqlite",
				"path": emptyPath,
			},
		},
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out ConfigUpdateResult
	if err := json.Unmarshal([]byte(assertToolSuccess(t, res)), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Success {
		t.Fatalf("expected staged update to fail, got %+v", out)
	}

	after, err := os.ReadFile(confPath)
	if err != nil {
		t.Fatalf("read config file: %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("expected failed staged update to leave config file untouched\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestHandleUpdateCurrentConfig_TransactionalSuccessSwapsRuntime(t *testing.T) {
	livePath := createSQLiteDBFile(t, "live.sqlite3", true)
	replacementPath := createSQLiteDBFile(t, "replacement.sqlite3", true)
	ms := newTransactionalConfigMCPServer(t, livePath)

	oldGJ := ms.service.gj
	oldDB := ms.service.dbs["main"]

	res, err := ms.handleUpdateCurrentConfig(context.Background(), newToolRequest(map[string]any{
		"databases": map[string]any{
			"main": map[string]any{
				"type": "sqlite",
				"path": replacementPath,
			},
		},
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out ConfigUpdateResult
	if err := json.Unmarshal([]byte(assertToolSuccess(t, res)), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !out.Success {
		t.Fatalf("expected staged update to succeed, got %+v", out)
	}
	if ms.service.gj == oldGJ {
		t.Fatal("expected transactional update to replace the GraphJin instance")
	}
	if ms.service.dbs["main"] == oldDB {
		t.Fatal("expected transactional update to replace the database handle")
	}
	if got := ms.service.conf.Core.Databases["main"].Path; got != replacementPath {
		t.Fatalf("expected live config path %q, got %q", replacementPath, got)
	}
	if ms.service.gj == nil || !ms.service.gj.SchemaReady() {
		t.Fatal("expected replacement GraphJin instance to be schema-ready")
	}
	if err := oldDB.Ping(); err == nil {
		t.Fatal("expected superseded database handle to be closed")
	}
}

func TestHandleUpdateCurrentConfig_MetadataReloadTogglesAutoCodeRelations(t *testing.T) {
	ms := newMetadataReloadMCPServer(t, false, []string{"code_a"}, true, false)
	s := ms.service

	assertGraphJinTable(t, s, defaultMetadataDBName, "gj_columns")
	assertServiceCount(t, s, defaultMetadataDBName, `SELECT count(*) FROM gj_columns WHERE database_name = 'app' AND table_name = 'users' AND column_name = 'email'`, 1)
	if hasRuntimeRelationship(s, defaultMetadataDBName, "gj_columns", "code_db_refs_id", "code_a:code_db_refs.column_key") {
		t.Fatal("automatic metadata relationship was present before enabling auto_code_relations")
	}

	out := applyConfigUpdate(t, ms, map[string]any{
		"metadata": map[string]any{
			"auto_code_relations": true,
			"code_databases":      []any{"code_a"},
		},
	})
	if !out.Success {
		t.Fatalf("metadata reload failed: %+v", out)
	}
	if !hasRuntimeRelationship(s, defaultMetadataDBName, "gj_columns", "code_db_refs_id", "code_a:code_db_refs.column_key") {
		t.Fatal("missing automatic gj_columns -> code_a.code_db_refs relationship after reload")
	}
	assertServiceCount(t, s, defaultMetadataDBName, `SELECT count(*) FROM gj_columns WHERE database_name = 'app' AND table_name = 'users' AND column_name = 'email'`, 1)
	assertServiceCount(t, s, "code_a", `SELECT count(*) FROM code_db_refs WHERE column_key = 'app:main.users.email' AND resolved = 1`, 1)
	assertMetadataCodeRefPaths(t, s, []string{"a.go"})

	out = applyConfigUpdate(t, ms, map[string]any{
		"metadata": map[string]any{
			"auto_code_relations": false,
			"code_databases":      []any{"code_a"},
		},
	})
	if !out.Success {
		t.Fatalf("metadata reload failed: %+v", out)
	}
	if hasRuntimeRelationship(s, defaultMetadataDBName, "gj_columns", "code_db_refs_id", "code_a:code_db_refs.column_key") {
		t.Fatal("automatic metadata relationship stayed present after disabling auto_code_relations")
	}
	assertServiceCount(t, s, defaultMetadataDBName, `SELECT count(*) FROM gj_columns WHERE database_name = 'app' AND table_name = 'users' AND column_name = 'email'`, 1)
}

func TestHandleUpdateCurrentConfig_MetadataReloadSwitchesCodeDatabasesAndInferDBRefs(t *testing.T) {
	ms := newMetadataReloadMCPServer(t, true, []string{"code_a"}, true, false)
	s := ms.service

	if !hasRuntimeRelationship(s, defaultMetadataDBName, "gj_columns", "code_db_refs_id", "code_a:code_db_refs.column_key") {
		t.Fatal("missing initial code_a metadata relationship")
	}
	assertServiceCount(t, s, "code_a", `SELECT count(*) FROM code_db_refs WHERE column_key = 'app:main.users.email' AND resolved = 1`, 1)
	assertServiceCount(t, s, "code_b", `SELECT count(*) FROM code_db_refs`, 0)

	out := applyConfigUpdate(t, ms, map[string]any{
		"metadata": map[string]any{
			"auto_code_relations": true,
			"code_databases":      []any{"code_b"},
		},
		"databases": map[string]any{
			"code_b": map[string]any{
				"type":          "codesql",
				"path":          s.conf.Core.Databases["code_b"].Path,
				"infer_db_refs": true,
			},
		},
	})
	if !out.Success {
		t.Fatalf("metadata/code reload failed: %+v", out)
	}
	if hasRuntimeRelationship(s, defaultMetadataDBName, "gj_columns", "code_db_refs_id", "code_a:code_db_refs.column_key") {
		t.Fatal("old code_a metadata relationship stayed present after switching code_databases")
	}
	if !hasRuntimeRelationship(s, defaultMetadataDBName, "gj_columns", "code_db_refs_id", "code_b:code_db_refs.column_key") {
		t.Fatal("missing code_b metadata relationship after switching code_databases")
	}
	assertServiceCount(t, s, "code_b", `SELECT count(*) FROM code_db_refs WHERE column_key = 'app:main.users.email' AND resolved = 1`, 1)
	assertMetadataCodeRefPaths(t, s, []string{"b.go"})

	out = applyConfigUpdate(t, ms, map[string]any{
		"databases": map[string]any{
			"code_b": map[string]any{
				"type":          "codesql",
				"path":          s.conf.Core.Databases["code_b"].Path,
				"infer_db_refs": false,
			},
		},
	})
	if !out.Success {
		t.Fatalf("disable infer_db_refs reload failed: %+v", out)
	}
	assertServiceCount(t, s, "code_b", `SELECT count(*) FROM code_db_refs`, 0)
	assertMetadataCodeRefPaths(t, s, nil)
}

func createSQLiteDBFile(t *testing.T, name string, withSchema bool) string {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), name)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	if withSchema {
		for _, stmt := range []string{
			`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)`,
			`INSERT INTO users (id, name) VALUES (1, 'Ada')`,
		} {
			if _, err := db.Exec(stmt); err != nil {
				t.Fatalf("exec %q: %v", stmt, err)
			}
		}
	}

	return dbPath
}

func newMetadataReloadMCPServer(t *testing.T, autoCodeRelations bool, codeDatabases []string, inferA, inferB bool) *mcpServer {
	t.Helper()

	appPath := createMetadataAppDB(t)
	codeA := t.TempDir()
	codeB := t.TempDir()
	writeTestFile(t, filepath.Join(codeA, "a.go"), `package main

func LookupA() {
	query := "SELECT users.email FROM users WHERE users.id = ?"
	_ = query
}
`)
	writeTestFile(t, filepath.Join(codeB, "b.go"), `package main

func LookupB() {
	query := "SELECT users.email FROM users WHERE users.id = ?"
	_ = query
}
`)
	confPath := filepath.Join(t.TempDir(), "dev.yml")
	if err := os.WriteFile(confPath, []byte("app_name: metadata_reload_test\n"), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}
	v := viper.New()
	v.SetConfigFile(confPath)
	v.SetConfigType("yaml")
	if err := v.ReadInConfig(); err != nil {
		t.Fatalf("read config file: %v", err)
	}

	metadataEnabled := true
	conf := &Config{
		Core: core.Config{
			DisableAllowList: true,
			Metadata: core.MetadataConfig{
				Enabled:           &metadataEnabled,
				AutoCodeRelations: &autoCodeRelations,
				CodeDatabases:     codeDatabases,
			},
			Databases: map[string]core.DatabaseConfig{
				"app":    {Type: "sqlite", Path: appPath},
				"code_a": {Type: "codesql", Path: codeA, InferDBRefs: &inferA},
				"code_b": {Type: "codesql", Path: codeB, InferDBRefs: &inferB},
			},
		},
		Serv: Serv{
			Production: false,
			ConfigPath: filepath.Join(t.TempDir(), "config"),
			MCP:        MCPConfig{Disable: true},
		},
		viper: v,
	}

	s, err := newGraphJinService(conf, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeTestService(s) })

	return &mcpServer{
		service:     s,
		ctx:         context.Background(),
		readOnlyDBs: map[string]bool{},
	}
}

func applyConfigUpdate(t *testing.T, ms *mcpServer, args map[string]any) ConfigUpdateResult {
	t.Helper()

	res, err := ms.handleUpdateCurrentConfig(context.Background(), newToolRequest(args))
	if err != nil {
		t.Fatalf("update_current_config error: %v", err)
	}
	var out ConfigUpdateResult
	if err := json.Unmarshal([]byte(assertToolSuccess(t, res)), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return out
}

func assertMetadataCodeRefPaths(t *testing.T, s *graphjinService, want []string) {
	t.Helper()

	res, err := s.gj.GraphQL(context.Background(), `query MetadataCodeRefPaths {
		gj_columns(where: { table_name: { eq: "users" }, column_name: { eq: "email" } }, limit: 1) {
			code_db_refs {
				file { path }
			}
		}
	}`, nil, &core.RequestConfig{})
	if err != nil {
		t.Fatalf("metadata GraphQL query failed: %v", err)
	}
	if len(res.Errors) != 0 {
		t.Fatalf("metadata GraphQL errors: %+v", res.Errors)
	}
	var out struct {
		GJColumns []struct {
			CodeDBRefs []struct {
				File struct {
					Path string `json:"path"`
				} `json:"file"`
			} `json:"code_db_refs"`
		} `json:"gj_columns"`
	}
	if err := json.Unmarshal(res.Data, &out); err != nil {
		t.Fatalf("decode metadata GraphQL data: %v\n%s", err, string(res.Data))
	}
	if len(out.GJColumns) != 1 {
		t.Fatalf("gj_columns len = %d, want 1: %s", len(out.GJColumns), string(res.Data))
	}
	got := make([]string, 0, len(out.GJColumns[0].CodeDBRefs))
	for _, ref := range out.GJColumns[0].CodeDBRefs {
		got = append(got, ref.File.Path)
	}
	if len(got) != len(want) {
		t.Fatalf("code ref paths = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("code ref paths = %v, want %v", got, want)
		}
	}
}

func newTransactionalConfigMCPServer(t *testing.T, dbPath string) *mcpServer {
	return newTransactionalConfigMCPServerWithOptions(t, dbPath, true, nil)
}

func newTransactionalConfigMCPServerWithOptions(t *testing.T, dbPath string, production bool, v *viper.Viper) *mcpServer {
	t.Helper()

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
	})

	conf := &Config{
		Core: core.Config{
			DBType:     "sqlite",
			Production: production,
			Databases: map[string]core.DatabaseConfig{
				"main": {
					Type: "sqlite",
					Path: dbPath,
				},
			},
		},
		Serv:  Serv{Production: production},
		viper: v,
	}
	syncDBFromDatabases(conf)

	fs := newAferoFS(afero.NewMemMapFs(), "/")
	svc := &graphjinService{
		conf:   conf,
		dbs:    map[string]*sql.DB{"main": db},
		fs:     fs,
		log:    zaptest.NewLogger(t).Sugar(),
		tracer: otel.Tracer("graphjin-transaction-test"),
	}

	gj, err := core.NewGraphJin(&conf.Core, db, core.OptionSetFS(fs), core.OptionSetDatabases(svc.dbs))
	if err != nil {
		t.Fatalf("init graphjin: %v", err)
	}
	t.Cleanup(func() {
		gj.Close()
	})
	svc.gj = gj

	return &mcpServer{
		service:     svc,
		ctx:         context.Background(),
		readOnlyDBs: map[string]bool{},
	}
}
