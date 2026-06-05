package serv

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/core/v3/sourcecap"
	"github.com/spf13/afero"
	"github.com/spf13/viper"
	"go.opentelemetry.io/otel"
	"go.uber.org/zap/zaptest"
	_ "modernc.org/sqlite"
)

func TestApplySourceConfigPatchesMergePatchSemantics(t *testing.T) {
	existing := []core.SourceConfig{{
		Name:         "main",
		Kind:         sourcecap.KindDatabase,
		Type:         "sqlite",
		Path:         "old.sqlite3",
		Default:      true,
		ReadOnly:     true,
		Capabilities: map[string]bool{sourcecap.KeyDataRead: true, sourcecap.KeySchemaRead: true},
		Access: core.SourceAccessConfig{
			PublicTables:  []string{"users"},
			BlockedTables: []string{"secrets"},
		},
	}}

	updated, changes, err := applySourceConfigMergePatches(existing, []any{map[string]any{
		"name":      "main",
		"path":      "new.sqlite3",
		"read_only": nil,
		"capabilities": map[string]any{
			sourcecap.KeySchemaRead: false,
			sourcecap.KeyDataWrite:  true,
		},
		"access": map[string]any{
			"public_tables":  []any{"accounts"},
			"blocked_tables": nil,
		},
	}})
	if err != nil {
		t.Fatalf("apply source patch: %v", err)
	}
	if len(changes) != 1 || changes[0] != "updated source: main" {
		t.Fatalf("changes = %v", changes)
	}
	if len(updated) != 1 {
		t.Fatalf("updated len = %d", len(updated))
	}
	got := updated[0]
	if got.Kind != sourcecap.KindDatabase || got.Type != "sqlite" || !got.Default {
		t.Fatalf("omitted fields were not preserved: %+v", got)
	}
	if got.Path != "new.sqlite3" {
		t.Fatalf("path = %q, want new.sqlite3", got.Path)
	}
	if got.ReadOnly {
		t.Fatalf("read_only should be cleared by null: %+v", got)
	}
	if !got.Capabilities[sourcecap.KeyDataRead] || got.Capabilities[sourcecap.KeySchemaRead] || !got.Capabilities[sourcecap.KeyDataWrite] {
		t.Fatalf("capabilities merge failed: %+v", got.Capabilities)
	}
	if len(got.Access.PublicTables) != 1 || got.Access.PublicTables[0] != "accounts" || len(got.Access.BlockedTables) != 0 {
		t.Fatalf("nested access merge failed: %+v", got.Access)
	}

	updated, changes, err = applySourceConfigMergePatches(updated, []any{map[string]any{
		"name": "logs",
		"kind": sourcecap.KindDatabase,
		"type": "sqlite",
		"path": "logs.sqlite3",
	}})
	if err != nil {
		t.Fatalf("apply new source patch: %v", err)
	}
	if len(updated) != 2 || changes[0] != "added source: logs" {
		t.Fatalf("new source update = %+v changes=%v", updated, changes)
	}

	if _, _, err := applySourceConfigMergePatches(existing, []any{map[string]any{"name": "missing_kind"}}); err == nil ||
		!strings.Contains(err.Error(), "requires kind") {
		t.Fatalf("expected new source kind error, got %v", err)
	}
}

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

func TestHandleUpdateCurrentConfig_RejectsPlaintextSecretWithoutKeystoreKey(t *testing.T) {
	livePath := createSQLiteDBFile(t, "live.sqlite3", true)
	replacementPath := createSQLiteDBFile(t, "replacement.sqlite3", true)
	ms := newTransactionalConfigMCPServer(t, livePath)

	out := applyConfigUpdate(t, ms, map[string]any{
		"databases": map[string]any{
			"main": map[string]any{
				"type":              "sqlite",
				"connection_string": replacementPath,
			},
		},
	})
	if out.Success {
		t.Fatalf("expected missing keystore key rejection, got %+v", out)
	}
	if len(out.Errors) == 0 || !strings.Contains(out.Errors[0], "secrets.keystore.key") {
		t.Fatalf("expected secrets.keystore.key error, got %+v", out.Errors)
	}
	if got := ms.service.conf.Core.Databases["main"].ConnString; got != "" {
		t.Fatalf("expected persisted config to remain unchanged, got connection_string %q", got)
	}
	if got := ms.service.conf.Core.Databases["main"].Path; got != livePath {
		t.Fatalf("expected live path %q to remain unchanged, got %q", livePath, got)
	}
}

func TestHandleUpdateCurrentConfig_RejectsPlaintextSecretWithInvalidKeystoreKey(t *testing.T) {
	livePath := createSQLiteDBFile(t, "live.sqlite3", true)
	replacementPath := createSQLiteDBFile(t, "replacement.sqlite3", true)
	ms := newTransactionalConfigMCPServer(t, livePath)
	ms.service.conf.Secrets.Keystore.Key = "not-base64"
	ms.service.conf.Secrets.Keystore.Path = filepath.Join(t.TempDir(), "secrets.enc.yml")

	out := applyConfigUpdate(t, ms, map[string]any{
		"databases": map[string]any{
			"main": map[string]any{
				"type":              "sqlite",
				"connection_string": replacementPath,
			},
		},
	})
	if out.Success {
		t.Fatalf("expected invalid keystore key rejection, got %+v", out)
	}
	if len(out.Errors) == 0 || !strings.Contains(out.Errors[0], "32 bytes") {
		t.Fatalf("expected 32-byte key error, got %+v", out.Errors)
	}
	if got := ms.service.conf.Core.Databases["main"].Path; got != livePath {
		t.Fatalf("expected live path %q to remain unchanged, got %q", livePath, got)
	}
}

func TestHandleUpdateCurrentConfig_SealsPlaintextSecretHydratesRuntimeAndSavesRefs(t *testing.T) {
	livePath := createSQLiteDBFile(t, "live.sqlite3", true)
	replacementPath := createSQLiteDBFile(t, "replacement.sqlite3", true)

	confPath := filepath.Join(t.TempDir(), "dev.yml")
	if err := os.WriteFile(confPath, []byte("app_name: test\n"), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}
	v := viper.New()
	v.SetConfigFile(confPath)
	v.SetConfigType("yaml")
	if err := v.ReadInConfig(); err != nil {
		t.Fatalf("read config file: %v", err)
	}
	ms := newTransactionalConfigMCPServerWithOptions(t, livePath, false, v)
	keystorePath := filepath.Join(t.TempDir(), "secrets.enc.yml")
	ms.service.conf.Secrets.Keystore.Key = testKeystoreKey(4)
	ms.service.conf.Secrets.Keystore.Path = keystorePath

	out := applyConfigUpdate(t, ms, map[string]any{
		"databases": map[string]any{
			"main": map[string]any{
				"type":              "sqlite",
				"connection_string": replacementPath,
			},
		},
	})
	if !out.Success {
		t.Fatalf("expected config update to succeed, got %+v", out)
	}
	ref := "gjsecret://databases/main/connection_string"
	if got := ms.service.conf.Core.Databases["main"].ConnString; got != ref {
		t.Fatalf("persisted connection string = %q, want %q", got, ref)
	}
	if got := ms.service.runtimeCore.Databases["main"].ConnString; got != replacementPath {
		t.Fatalf("runtime connection string = %q, want %q", got, replacementPath)
	}

	savedConfig, err := os.ReadFile(confPath)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	if strings.Contains(string(savedConfig), replacementPath) {
		t.Fatalf("saved config leaked plaintext path:\n%s", string(savedConfig))
	}
	if !strings.Contains(string(savedConfig), ref) {
		t.Fatalf("saved config missing secret ref %q:\n%s", ref, string(savedConfig))
	}
	savedKeystore, err := os.ReadFile(keystorePath)
	if err != nil {
		t.Fatalf("read keystore: %v", err)
	}
	if strings.Contains(string(savedKeystore), replacementPath) {
		t.Fatalf("keystore leaked plaintext path:\n%s", string(savedKeystore))
	}

	res, err := ms.handleGetCurrentConfig(context.Background(), newToolRequest(map[string]any{"section": "databases"}))
	if err != nil {
		t.Fatalf("get_current_config error: %v", err)
	}
	payload := assertToolSuccess(t, res)
	if strings.Contains(payload, replacementPath) {
		t.Fatalf("get_current_config leaked plaintext: %s", payload)
	}
	if strings.Contains(payload, ref) {
		t.Fatalf("get_current_config exposed stable secret ref instead of redacting: %s", payload)
	}

	restartedConf := &Config{
		Core: core.Config{
			Databases: map[string]core.DatabaseConfig{
				"main": {Type: "sqlite", ConnString: ref},
			},
		},
		Serv: Serv{
			Production: false,
			Secrets: SecretsConfig{Keystore: KeystoreConfig{
				Key:  testKeystoreKey(4),
				Path: keystorePath,
			}},
		},
	}
	restarted, err := newGraphJinService(restartedConf, nil)
	if err != nil {
		t.Fatalf("restart with encrypted ref: %v", err)
	}
	t.Cleanup(func() { closeTestService(restarted) })
	if got := restarted.conf.Core.Databases["main"].ConnString; got != ref {
		t.Fatalf("restart persisted config = %q, want %q", got, ref)
	}
	if got := restarted.runtimeCore.Databases["main"].ConnString; got != replacementPath {
		t.Fatalf("restart runtime connection string = %q, want %q", got, replacementPath)
	}
}

func TestHandleUpdateCurrentConfig_ConfigSaveFailureKeepsRemovedKeystoreEntry(t *testing.T) {
	livePath := createSQLiteDBFile(t, "live.sqlite3", true)
	oldPath := createSQLiteDBFile(t, "old.sqlite3", true)
	confPath := filepath.Join(t.TempDir(), "dev.yml")
	oldRef := "gjsecret://databases/old/connection_string"
	before := []byte("databases:\n  main:\n    type: sqlite\n    path: " + livePath + "\n  old:\n    type: sqlite\n    connection_string: " + oldRef + "\n")
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
	keystorePath := filepath.Join(t.TempDir(), "secrets.enc.yml")
	seedKeystoreRef(t, ms, keystorePath, testKeystoreKey(8), oldRef, oldPath)
	ms.service.conf.Core.Databases["old"] = core.DatabaseConfig{Type: "sqlite", ConnString: oldRef}

	if err := os.Chmod(confPath, 0o400); err != nil {
		t.Fatalf("make config read-only: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(confPath, 0o600)
	})

	out := applyConfigUpdate(t, ms, map[string]any{
		"remove_databases": []any{"old"},
	})
	if !out.Success {
		t.Fatalf("expected runtime update to succeed with config save warning, got %+v", out)
	}
	if !configUpdateChangesContain(out.Changes, "config save warning") {
		t.Fatalf("expected config save warning, got changes %+v", out.Changes)
	}

	after, err := os.ReadFile(confPath)
	if err != nil {
		t.Fatalf("read config file: %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("expected failed config save to leave on-disk config unchanged\nbefore:\n%s\nafter:\n%s", before, after)
	}

	reopened, err := newLocalKeystore(&Config{Serv: Serv{Secrets: SecretsConfig{Keystore: KeystoreConfig{
		Key:  testKeystoreKey(8),
		Path: keystorePath,
	}}}})
	if err != nil {
		t.Fatalf("reopen keystore: %v", err)
	}
	got, err := reopened.Open(oldRef)
	if err != nil {
		t.Fatalf("expected removed ref to remain after failed config save: %v", err)
	}
	if got != oldPath {
		t.Fatalf("old ref decrypted to %q, want %q", got, oldPath)
	}
}

func TestHandleUpdateCurrentConfig_ConfigSaveSuccessPrunesRemovedKeystoreEntry(t *testing.T) {
	livePath := createSQLiteDBFile(t, "live.sqlite3", true)
	oldPath := createSQLiteDBFile(t, "old.sqlite3", true)
	confPath := filepath.Join(t.TempDir(), "dev.yml")
	oldRef := "gjsecret://databases/old/connection_string"
	if err := os.WriteFile(confPath, []byte("app_name: test\n"), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}
	v := viper.New()
	v.SetConfigFile(confPath)
	v.SetConfigType("yaml")
	if err := v.ReadInConfig(); err != nil {
		t.Fatalf("read config file: %v", err)
	}

	ms := newTransactionalConfigMCPServerWithOptions(t, livePath, false, v)
	keystorePath := filepath.Join(t.TempDir(), "secrets.enc.yml")
	seedKeystoreRef(t, ms, keystorePath, testKeystoreKey(9), oldRef, oldPath)
	ms.service.conf.Core.Databases["old"] = core.DatabaseConfig{Type: "sqlite", ConnString: oldRef}

	out := applyConfigUpdate(t, ms, map[string]any{
		"remove_databases": []any{"old"},
	})
	if !out.Success {
		t.Fatalf("expected config update to succeed, got %+v", out)
	}

	savedConfig, err := os.ReadFile(confPath)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	if strings.Contains(string(savedConfig), oldRef) {
		t.Fatalf("saved config still references removed secret ref:\n%s", string(savedConfig))
	}

	reopened, err := newLocalKeystore(&Config{Serv: Serv{Secrets: SecretsConfig{Keystore: KeystoreConfig{
		Key:  testKeystoreKey(9),
		Path: keystorePath,
	}}}})
	if err != nil {
		t.Fatalf("reopen keystore: %v", err)
	}
	if _, err := reopened.Open(oldRef); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected removed ref to be pruned after successful config save, got %v", err)
	}
}

func TestNewGraphJinServiceLoadsPlaintextSecretConfigWithoutKeystoreKey(t *testing.T) {
	dbPath := createSQLiteDBFile(t, "plaintext.sqlite3", true)
	conf := &Config{
		Core: core.Config{
			Databases: map[string]core.DatabaseConfig{
				"main": {Type: "sqlite", ConnString: dbPath},
			},
		},
		Serv: Serv{Production: false},
	}
	svc, err := newGraphJinService(conf, nil)
	if err != nil {
		t.Fatalf("expected plaintext bootstrap config to load without keystore key: %v", err)
	}
	t.Cleanup(func() { closeTestService(svc) })
	if got := svc.runtimeCore.Databases["main"].ConnString; got != dbPath {
		t.Fatalf("runtime connection string = %q, want %q", got, dbPath)
	}
}

func TestNewGraphJinServiceRequiresKeystoreForEncryptedRefs(t *testing.T) {
	ref := "gjsecret://databases/main/connection_string"
	baseCore := core.Config{Databases: map[string]core.DatabaseConfig{
		"main": {Type: "sqlite", ConnString: ref},
	}}

	_, err := newGraphJinService(&Config{Core: baseCore, Serv: Serv{Production: false}}, nil)
	if err == nil || !strings.Contains(err.Error(), "secrets.keystore.key") {
		t.Fatalf("expected missing key startup error, got %v", err)
	}

	missingPath := filepath.Join(t.TempDir(), "missing.yml")
	_, err = newGraphJinService(&Config{
		Core: baseCore,
		Serv: Serv{Production: false, Secrets: SecretsConfig{Keystore: KeystoreConfig{
			Key:  testKeystoreKey(5),
			Path: missingPath,
		}}},
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected missing keystore entry startup error, got %v", err)
	}

	corruptPath := filepath.Join(t.TempDir(), "corrupt.yml")
	if err := os.WriteFile(corruptPath, []byte("secrets: ["), 0o600); err != nil {
		t.Fatalf("write corrupt keystore: %v", err)
	}
	_, err = newGraphJinService(&Config{
		Core: baseCore,
		Serv: Serv{Production: false, Secrets: SecretsConfig{Keystore: KeystoreConfig{
			Key:  testKeystoreKey(5),
			Path: corruptPath,
		}}},
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "parse secrets keystore") {
		t.Fatalf("expected corrupt keystore startup error, got %v", err)
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

func TestHandleUpdateCurrentConfig_RuntimeReadDisabledStopsRuntimeStore(t *testing.T) {
	livePath := createSQLiteDBFile(t, "live.sqlite3", true)
	conf := &Config{
		Core: core.Config{
			Mode: modeAgentic,
			Sources: []core.SourceConfig{
				{Name: "main", Kind: "database", Type: "sqlite", Path: livePath},
				{Name: "graphjin", Kind: "graphjin"},
			},
		},
		Serv: Serv{Production: false},
	}
	svc, err := newGraphJinService(conf, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeTestService(svc) })

	oldStore := &trackingRuntimeStore{name: "tracking"}
	svc.runtimeEvents = oldStore
	ms := &mcpServer{
		service:     svc,
		ctx:         context.Background(),
		readOnlyDBs: map[string]bool{},
	}

	out := applySourceModeConfigUpdate(t, ms, map[string]any{
		"sources": []any{
			map[string]any{
				"name": "main",
				"kind": "database",
				"type": "sqlite",
				"path": livePath,
			},
			map[string]any{
				"name":         "graphjin",
				"kind":         "graphjin",
				"capabilities": map[string]any{"runtime.read": false},
			},
		},
	})
	if !out.Success {
		t.Fatalf("expected runtime.read disable update to succeed, got %+v", out)
	}
	if !oldStore.closed {
		t.Fatal("expected old runtime event store to be closed")
	}
	if svc.runtimeEvents != nil {
		t.Fatalf("expected runtime event store to be disabled, got %#v", svc.runtimeEvents)
	}
}

func TestHandleUpdateCurrentConfig_SourcePatchUsesSourceScopedReload(t *testing.T) {
	mainPath := createSQLiteDBFile(t, "main.sqlite3", true)
	analyticsPath := createSQLiteDBFile(t, "analytics.sqlite3", true)
	replacementPath := createSQLiteDBFile(t, "replacement.sqlite3", true)
	ms := newSourceModeConfigMCPServer(t, map[string]string{
		"main":      mainPath,
		"analytics": analyticsPath,
	})

	oldGJ := ms.service.gj
	oldMain := ms.service.dbs["main"]
	oldAnalytics := ms.service.dbs["analytics"]

	out := applySourceModeConfigUpdate(t, ms, map[string]any{
		"update_sources": []any{map[string]any{
			"name": "main",
			"path": replacementPath,
		}},
	})
	assertSourceScopedConfigResult(t, out, "main")
	if out.CatalogRevision == "" {
		t.Fatalf("expected catalog revision in source-scoped result: %+v", out)
	}
	if ms.service.gj != oldGJ {
		t.Fatal("expected source-scoped config reload to preserve the GraphJin wrapper")
	}
	if ms.service.dbs["main"] == oldMain {
		t.Fatal("expected changed source database handle to be replaced")
	}
	if ms.service.dbs["analytics"] != oldAnalytics {
		t.Fatal("expected untouched source database handle to be reused")
	}
	if err := oldMain.Ping(); err == nil {
		t.Fatal("expected old changed-source database handle to be closed")
	}
	if err := oldAnalytics.Ping(); err != nil {
		t.Fatalf("expected untouched source database handle to remain open: %v", err)
	}
	details := latestRuntimeEventDetails(t, ms.service, "config", "reload_mode", "source_scoped")
	changed, _ := details["changed_sources"].([]any)
	if len(changed) != 1 || changed[0] != "main" {
		t.Fatalf("config event changed_sources = %+v", details["changed_sources"])
	}
}

func TestHandleUpdateCurrentConfig_AddSourceUsesSourceScopedReload(t *testing.T) {
	mainPath := createSQLiteDBFile(t, "main.sqlite3", true)
	logsPath := createSQLiteDBFile(t, "logs.sqlite3", true)
	ms := newSourceModeConfigMCPServer(t, map[string]string{"main": mainPath})

	oldGJ := ms.service.gj
	oldMain := ms.service.dbs["main"]

	out := applySourceModeConfigUpdate(t, ms, map[string]any{
		"update_sources": []any{map[string]any{
			"name": "logs",
			"kind": sourcecap.KindDatabase,
			"type": "sqlite",
			"path": logsPath,
		}},
	})
	assertSourceScopedConfigResult(t, out, "logs")
	if ms.service.gj != oldGJ {
		t.Fatal("expected source add to preserve the GraphJin wrapper")
	}
	if ms.service.dbs["main"] != oldMain {
		t.Fatal("expected existing source database handle to be reused")
	}
	if ms.service.dbs["logs"] == nil {
		t.Fatal("expected added source database handle")
	}
	if err := oldMain.Ping(); err != nil {
		t.Fatalf("expected existing source database handle to remain open: %v", err)
	}
}

func TestHandleUpdateCurrentConfig_RemoveSourceUsesSourceScopedReload(t *testing.T) {
	mainPath := createSQLiteDBFile(t, "main.sqlite3", true)
	analyticsPath := createSQLiteDBFile(t, "analytics.sqlite3", true)
	ms := newSourceModeConfigMCPServer(t, map[string]string{
		"main":      mainPath,
		"analytics": analyticsPath,
	})

	oldGJ := ms.service.gj
	oldMain := ms.service.dbs["main"]
	oldAnalytics := ms.service.dbs["analytics"]

	out := applySourceModeConfigUpdate(t, ms, map[string]any{
		"remove_sources": []any{"analytics"},
	})
	assertSourceScopedConfigResult(t, out, "analytics")
	if ms.service.gj != oldGJ {
		t.Fatal("expected source removal to preserve the GraphJin wrapper")
	}
	if ms.service.dbs["main"] != oldMain {
		t.Fatal("expected untouched source database handle to be reused")
	}
	if _, ok := ms.service.dbs["analytics"]; ok {
		t.Fatal("expected removed source database handle to be omitted")
	}
	if err := oldMain.Ping(); err != nil {
		t.Fatalf("expected untouched source database handle to remain open: %v", err)
	}
	if err := oldAnalytics.Ping(); err == nil {
		t.Fatal("expected removed source database handle to be closed")
	}
}

func TestHandleUpdateCurrentConfig_MultiSourcePatchUsesSourceScopedReload(t *testing.T) {
	mainPath := createSQLiteDBFile(t, "main.sqlite3", true)
	analyticsPath := createSQLiteDBFile(t, "analytics.sqlite3", true)
	mainReplacement := createSQLiteDBFile(t, "main-replacement.sqlite3", true)
	analyticsReplacement := createSQLiteDBFile(t, "analytics-replacement.sqlite3", true)
	ms := newSourceModeConfigMCPServer(t, map[string]string{
		"main":      mainPath,
		"analytics": analyticsPath,
	})

	oldGJ := ms.service.gj
	oldMain := ms.service.dbs["main"]
	oldAnalytics := ms.service.dbs["analytics"]

	out := applySourceModeConfigUpdate(t, ms, map[string]any{
		"update_sources": []any{
			map[string]any{"name": "main", "path": mainReplacement},
			map[string]any{"name": "analytics", "path": analyticsReplacement},
		},
	})
	assertSourceScopedConfigResult(t, out, "analytics", "main")
	if ms.service.gj != oldGJ {
		t.Fatal("expected multi-source patch to preserve the GraphJin wrapper")
	}
	if ms.service.dbs["main"] == oldMain || ms.service.dbs["analytics"] == oldAnalytics {
		t.Fatal("expected both changed source database handles to be replaced")
	}
	if err := oldMain.Ping(); err == nil {
		t.Fatal("expected old main database handle to be closed")
	}
	if err := oldAnalytics.Ping(); err == nil {
		t.Fatal("expected old analytics database handle to be closed")
	}
}

func TestHandleUpdateCurrentConfig_SourcePatchWithGlobalEditFallsBackToFullReload(t *testing.T) {
	mainPath := createSQLiteDBFile(t, "main.sqlite3", true)
	analyticsPath := createSQLiteDBFile(t, "analytics.sqlite3", true)
	replacementPath := createSQLiteDBFile(t, "replacement.sqlite3", true)
	ms := newSourceModeConfigMCPServer(t, map[string]string{
		"main":      mainPath,
		"analytics": analyticsPath,
	})

	oldGJ := ms.service.gj

	out := applySourceModeConfigUpdate(t, ms, map[string]any{
		"update_sources": []any{map[string]any{
			"name": "main",
			"path": replacementPath,
		}},
		"blocklist": []any{"users.name"},
	})
	if !out.Success {
		t.Fatalf("expected mixed config update to succeed via full fallback, got %+v", out)
	}
	if out.ReloadMode != "full" || !out.ReloadFallback {
		t.Fatalf("reload result = mode %q fallback %v, want full fallback", out.ReloadMode, out.ReloadFallback)
	}
	if len(out.ChangedSources) != 1 || out.ChangedSources[0] != "main" {
		t.Fatalf("changed sources = %v, want [main]", out.ChangedSources)
	}
	if ms.service.gj == oldGJ {
		t.Fatal("expected full fallback to replace the GraphJin wrapper")
	}
	details := latestRuntimeEventDetails(t, ms.service, "config.apply", "reload_mode", "full")
	if details["reload_fallback"] != true {
		t.Fatalf("expected config event reload_fallback=true, details=%+v", details)
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

func assertSourceScopedConfigResult(t *testing.T, out ConfigUpdateResult, expectedSources ...string) {
	t.Helper()
	if !out.Success {
		t.Fatalf("expected source-scoped update to succeed, got %+v", out)
	}
	if out.ReloadMode != "source_scoped" || out.ReloadFallback {
		t.Fatalf("reload result = mode %q fallback %v, want source_scoped without fallback", out.ReloadMode, out.ReloadFallback)
	}
	if len(out.ChangedSources) != len(expectedSources) {
		t.Fatalf("changed sources = %v, want %v", out.ChangedSources, expectedSources)
	}
	for i, want := range expectedSources {
		if out.ChangedSources[i] != want {
			t.Fatalf("changed sources = %v, want %v", out.ChangedSources, expectedSources)
		}
	}
}

func newSourceModeConfigMCPServer(t *testing.T, dbPaths map[string]string) *mcpServer {
	t.Helper()

	names := make([]string, 0, len(dbPaths))
	for name := range dbPaths {
		names = append(names, name)
	}
	sort.Strings(names)
	sources := make([]core.SourceConfig, 0, len(names)+1)
	for _, name := range names {
		sources = append(sources, core.SourceConfig{
			Name:    name,
			Kind:    sourcecap.KindDatabase,
			Type:    "sqlite",
			Path:    dbPaths[name],
			Default: name == "main",
		})
	}
	sources = append(sources, core.SourceConfig{Name: "graphjin", Kind: sourcecap.KindGraphJin})

	conf := &Config{
		Core: core.Config{
			Mode:             modeAgentic,
			Production:       false,
			DisableAllowList: true,
			Sources:          sources,
		},
		Serv: Serv{
			Production: false,
			MCP:        MCPConfig{AllowConfigUpdates: true},
		},
	}
	svc, err := newGraphJinService(conf, nil)
	if err != nil {
		t.Fatalf("init source-mode service: %v", err)
	}
	t.Cleanup(func() { closeTestService(svc) })

	return &mcpServer{
		service:     svc,
		ctx:         context.Background(),
		readOnlyDBs: map[string]bool{},
	}
}

func seedKeystoreRef(t *testing.T, ms *mcpServer, path, key, ref, plaintext string) {
	t.Helper()

	ms.service.conf.Secrets.Keystore.Key = key
	ms.service.conf.Secrets.Keystore.Path = path
	ks, err := ms.service.localKeystore()
	if err != nil {
		t.Fatalf("new keystore: %v", err)
	}
	if err := ks.Seal(ref, plaintext); err != nil {
		t.Fatalf("seal ref: %v", err)
	}
	if err := ks.Save(map[string]struct{}{ref: {}}); err != nil {
		t.Fatalf("save keystore: %v", err)
	}
}

func configUpdateChangesContain(changes []string, needle string) bool {
	for _, change := range changes {
		if strings.Contains(change, needle) {
			return true
		}
	}
	return false
}

func applySourceModeConfigUpdate(t *testing.T, ms *mcpServer, args map[string]any) ConfigUpdateResult {
	t.Helper()

	ctx := context.Background()
	revision := ms.currentConfigCatalogRevision(ctx)
	if revision == "" {
		t.Fatal("expected source-mode catalog revision")
	}
	previewArgs := cloneConfigUpdateArgs(t, args)
	previewArgs["mode"] = "preview"
	previewArgs["expected_catalog_revision"] = revision
	preview := applyConfigUpdate(t, ms, previewArgs)
	if !preview.Success || !preview.Valid || preview.PreviewID == "" {
		t.Fatalf("expected valid preview, got %+v", preview)
	}
	applyArgs := cloneConfigUpdateArgs(t, args)
	applyArgs["mode"] = "apply"
	applyArgs["preview_id"] = preview.PreviewID
	applyArgs["expected_catalog_revision"] = revision
	return applyConfigUpdate(t, ms, applyArgs)
}

func cloneConfigUpdateArgs(t *testing.T, args map[string]any) map[string]any {
	t.Helper()

	data, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal config args: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal config args: %v", err)
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

type trackingRuntimeStore struct {
	name   string
	closed bool
}

func (s *trackingRuntimeStore) Name() string {
	return s.name
}

func (s *trackingRuntimeStore) NodeID() string {
	return "tracking-node"
}

func (s *trackingRuntimeStore) Record(context.Context, runtimeEvent) {
}

func (s *trackingRuntimeStore) Rows(context.Context, runtimeStatus) []map[string]any {
	return nil
}

func (s *trackingRuntimeStore) Close() error {
	s.closed = true
	return nil
}
