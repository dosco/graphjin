package serv

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3"
	_ "modernc.org/sqlite"
)

func TestManagedArtifactStorePersistsAndIsolatesApplicationDatabase(t *testing.T) {
	root := t.TempDir()
	appPath := filepath.Join(root, "app.sqlite3")
	app, err := sql.Open("sqlite", appPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.Exec(`CREATE TABLE orders (id INTEGER PRIMARY KEY, status TEXT)`); err != nil {
		t.Fatal(err)
	}
	app.Close() //nolint:errcheck

	start := func() (*HttpService, *graphjinService) {
		conf, err := NewConfig(fmt.Sprintf(`
mode: dev
disable_allow_list: true
discovery_cache:
  enabled: false
database:
  type: sqlite
  path: %q
`, appPath), "yaml")
		if err != nil {
			t.Fatalf("NewConfig: %v", err)
		}
		conf.ConfigPath = root
		httpService, err := NewGraphJinService(conf)
		if err != nil {
			t.Fatalf("NewGraphJinService: %v", err)
		}
		return httpService, httpService.Load().(*graphjinService)
	}

	httpService, svc := start()
	managedPath := filepath.Join(root, filepath.FromSlash(managedArtifactRelativePath))
	info, err := os.Stat(managedPath)
	if err != nil {
		t.Fatalf("managed SQLite not created: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("managed SQLite mode = %#o, want 0600", got)
	}
	if _, public := svc.conf.Core.Databases[managedArtifactDatabaseName]; public {
		t.Fatal("managed store leaked into public database configuration")
	}
	if _, runtime := svc.runtimeCore.Databases[managedArtifactDatabaseName]; !runtime {
		t.Fatal("managed store missing from runtime compiler graph")
	}
	managedDB, _, _, ok := svc.artifactDB()
	if !ok || managedDB == nil {
		t.Fatal("managed artifact database is unavailable")
	}
	if svc.anyDB() == managedDB {
		t.Fatal("managed artifact database was selected as the application database")
	}
	if got := managedDB.Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("managed SQLite max connections = %d, want 1", got)
	}
	var journalMode string
	if err := managedDB.QueryRow(`PRAGMA journal_mode`).Scan(&journalMode); err != nil || !strings.EqualFold(journalMode, "wal") {
		t.Fatalf("managed SQLite journal mode = %q err=%v", journalMode, err)
	}
	var busyTimeout int
	if err := managedDB.QueryRow(`PRAGMA busy_timeout`).Scan(&busyTimeout); err != nil || busyTimeout != 5000 {
		t.Fatalf("managed SQLite busy timeout = %d err=%v", busyTimeout, err)
	}

	appDB := svc.anyDB()
	if sqliteTableExists(t, appDB, "_graphjin_artifacts") || sqliteTableExists(t, appDB, "_graphjin_watches") {
		t.Fatal("managed artifact/watch tables leaked into the application database")
	}
	if !sqliteTableExists(t, managedDB, "_graphjin_artifacts") || !sqliteTableExists(t, managedDB, "_graphjin_watches") {
		t.Fatal("managed artifact/watch tables were not initialized")
	}

	ctx := contextWithUserRole(artifactUserCtx("user_1"), "user")
	artifact, err := newArtifactControlPlane(svc).mutateRow(ctx, core.ManagedMutationRoot{
		Table: artifactsRootTable, Operation: "insert",
		Input: map[string]interface{}{"name": "persistent", "kind": "query", "content": "query { orders { id status } }"},
	})
	if err != nil {
		t.Fatalf("insert managed artifact: %v", err)
	}
	watch, err := newWatchControlPlane(svc).mutateRow(ctx, core.ManagedMutationRoot{
		Table: watchesRootTable, Operation: "insert",
		Input: map[string]interface{}{
			"name": "persistent_watch", "query": cursorOrdersWatchQuery("persistent_watch"),
			"status": "paused", "enabled": false,
		},
	})
	if err != nil {
		t.Fatalf("insert managed watch: %v", err)
	}
	watchID := fmt.Sprint(watch["id"])
	if err := svc.updateWatchCursorCheckpoint(ctx, watchID, `{"orders_cursor":"cursor-1"}`); err != nil {
		t.Fatalf("persist watch cursor: %v", err)
	}
	if err := httpService.Close(); err != nil {
		t.Fatalf("close first service: %v", err)
	}

	httpService, svc = start()
	t.Cleanup(func() { _ = httpService.Close() })
	artifactRows, err := newArtifactControlPlane(svc).artifactRows(ctx)
	if err != nil {
		t.Fatalf("read artifacts after restart: %v", err)
	}
	if len(artifactRows) != 1 || artifactRows[0]["id"] != artifact["id"] {
		t.Fatalf("artifact did not persist across restart: %+v", artifactRows)
	}
	watchRows, err := newWatchControlPlane(svc).watchRows(ctx)
	if err != nil {
		t.Fatalf("read watches after restart: %v", err)
	}
	if len(watchRows) != 1 || watchRows[0]["id"] != watchID {
		t.Fatalf("watch did not persist across restart: %+v", watchRows)
	}
	managedDB, _, _, ok = svc.artifactDB()
	if !ok {
		t.Fatal("managed artifact database unavailable after restart")
	}
	var cursor sql.NullString
	if err := managedDB.QueryRow(`SELECT last_cursor_json FROM "_graphjin_watches" WHERE id = ?`, watchID).Scan(&cursor); err != nil {
		t.Fatalf("read cursor after restart: %v", err)
	}
	if !cursor.Valid || cursor.String != `{"orders_cursor":"cursor-1"}` {
		t.Fatalf("watch cursor after restart = %+v", cursor)
	}
}

func TestManagedArtifactStoreCreationFailureIsFatal(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".graphjin"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	appPath := filepath.Join(root, "app.sqlite3")
	conf, err := NewConfig(fmt.Sprintf(`
mode: dev
discovery_cache:
  enabled: false
database:
  type: sqlite
  path: %q
`, appPath), "yaml")
	if err != nil {
		t.Fatal(err)
	}
	conf.ConfigPath = root
	_, err = NewGraphJinService(conf)
	if err == nil || !strings.Contains(err.Error(), "managed artifact store") {
		t.Fatalf("startup error = %v, want fatal managed artifact store error", err)
	}
}

func TestDemoArtifactBootstrapDefersManagedStoreToService(t *testing.T) {
	conf, err := NewConfig("mode: agentic\n", "yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureArtifactStore(conf, map[string]*sql.DB{}); err != nil {
		t.Fatalf("managed demo bootstrap should defer to service startup: %v", err)
	}
}
