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
