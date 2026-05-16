package serv

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestHandleUpdateCurrentConfig_SerializesConcurrentUpdates(t *testing.T) {
	livePath := createSQLiteDBFile(t, "live.sqlite3", true)
	ms := newTransactionalConfigMCPServer(t, livePath)

	ms.service.configMu.Lock()
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(started)
		res, err := ms.handleUpdateCurrentConfig(context.Background(), newToolRequest(map[string]any{
			"mcp": map[string]any{"allow_raw_queries": true},
		}))
		if err != nil {
			done <- err
			return
		}
		var out ConfigUpdateResult
		if err := json.Unmarshal([]byte(assertToolSuccess(t, res)), &out); err != nil {
			done <- err
			return
		}
		if !out.Success {
			done <- fmt.Errorf("expected successful update, got %+v", out)
			return
		}
		done <- nil
	}()

	<-started
	select {
	case err := <-done:
		ms.service.configMu.Unlock()
		t.Fatalf("config update completed while configMu was held: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	ms.service.configMu.Unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("config update after unlock: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("config update did not complete after configMu was released")
	}
	if !ms.service.conf.MCP.AllowRawQueries {
		t.Fatal("expected serialized update to apply after configMu was released")
	}
}

func TestHandleUpdateCurrentConfig_MetadataReloadTogglesAutoCodeRelations(t *testing.T) {
	metadataEnabled := true
	conf := &Config{Core: core.Config{Metadata: core.MetadataConfig{Enabled: &metadataEnabled}}}
	_, err := newGraphJinService(conf, nil)
	if err == nil || !strings.Contains(err.Error(), "kind: graphjin") {
		t.Fatalf("legacy metadata config error = %v, want migration guidance", err)
	}
}

func TestHandleUpdateCurrentConfig_MetadataReloadSwitchesCodeDatabasesAndInferDBRefs(t *testing.T) {
	conf := &Config{Core: core.Config{Databases: map[string]core.DatabaseConfig{
		"code": {Type: "codesql", Path: t.TempDir()},
	}}}
	_, err := newGraphJinService(conf, nil)
	if err == nil || !strings.Contains(err.Error(), "kind: code") {
		t.Fatalf("legacy codesql config error = %v, want migration guidance", err)
	}
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
		gj_catalog(where: { kind: { eq: "column" }, table_name: { eq: "users" }, column_name: { eq: "email" } }, limit: 1) {
			gj_code {
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
		GJCatalog []struct {
			GJCode []struct {
				File struct {
					Path string `json:"path"`
				} `json:"file"`
			} `json:"gj_code"`
		} `json:"gj_catalog"`
	}
	if err := json.Unmarshal(res.Data, &out); err != nil {
		t.Fatalf("decode metadata GraphQL data: %v\n%s", err, string(res.Data))
	}
	if len(out.GJCatalog) != 1 {
		t.Fatalf("gj_catalog len = %d, want 1: %s", len(out.GJCatalog), string(res.Data))
	}
	got := make([]string, 0, len(out.GJCatalog[0].GJCode))
	for _, ref := range out.GJCatalog[0].GJCode {
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
