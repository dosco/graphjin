//go:build cgo

package codesql

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ts "github.com/tree-sitter/go-tree-sitter"
)

func TestCachePathUsesDatabaseNamePrefix(t *testing.T) {
	cacheDir := t.TempDir()
	root := t.TempDir()

	path, err := CachePath("code-db", root, cacheDir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(filepath.Base(path), "code-db-") {
		t.Fatalf("cache filename = %q, want database-name prefix", filepath.Base(path))
	}
	if filepath.Ext(path) != ".sqlite" {
		t.Fatalf("cache extension = %q, want .sqlite", filepath.Ext(path))
	}

	path, err = CachePath("", root, cacheDir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(filepath.Base(path), "default-") {
		t.Fatalf("legacy cache filename = %q, want default prefix", filepath.Base(path))
	}
}

func TestBundledQueryPacksCompile(t *testing.T) {
	for _, spec := range commonLanguages() {
		for _, pack := range spec.QueryPacks {
			q, err := ts.NewQuery(spec.Language, pack.Source)
			if err != nil {
				t.Fatalf("%s %s query did not compile: %v", spec.Name, pack.Kind, err)
			}
			q.Close()
		}
	}
}

func TestReconcileAddsUpdatesDeletesAndKeepsSQLiteQueryable(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(t.TempDir(), "codesql")

	writeFile(t, filepath.Join(root, "main.go"), `package main

import "fmt"

// Greet writes a greeting.
func Greet(name string) {
	fmt.Println(name)
}
`)

	managed, stats, err := OpenManaged(context.Background(), Options{
		Name:     "code",
		Root:     root,
		CacheDir: cacheDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer managed.Close()

	if stats.FilesAdded != 1 {
		t.Fatalf("FilesAdded = %d, want 1", stats.FilesAdded)
	}
	assertCount(t, managed.DB, `SELECT count(*) FROM code_files WHERE path = 'main.go'`, 1)
	assertCount(t, managed.DB, `SELECT count(*) FROM code_symbols WHERE name = 'Greet' AND kind = 'function'`, 1)
	assertCount(t, managed.DB, `SELECT count(*) FROM code_imports WHERE path = 'fmt'`, 1)
	assertCount(t, managed.DB, `SELECT count(*) FROM code_nodes WHERE node_type = 'function_declaration'`, 1)
	assertCount(t, managed.DB, `SELECT count(*) FROM code_captures WHERE query_kind = 'codesql' AND capture_name = 'symbol.name'`, 1)
	assertCount(t, managed.DB, `SELECT count(*) FROM code_symbols_fts WHERE code_symbols_fts MATCH 'Greet'`, 1)

	idx := &indexer{db: managed.DB, root: managed.Root, cachePath: managed.CachePath, languages: commonLanguages()}
	stats, err = idx.Reconcile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.FilesSkipped != 1 {
		t.Fatalf("FilesSkipped = %d, want 1", stats.FilesSkipped)
	}

	writeFile(t, filepath.Join(root, "other.js"), `export function handler() { return 1 }`)
	stats, err = idx.Reconcile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.FilesAdded != 1 {
		t.Fatalf("FilesAdded after adding JS = %d, want 1", stats.FilesAdded)
	}
	assertCount(t, managed.DB, `SELECT count(*) FROM code_symbols WHERE name = 'handler'`, 1)

	writeFile(t, filepath.Join(root, "main.go"), `package main
func Welcome() {}
`)
	stats, err = idx.Reconcile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.FilesChanged != 1 {
		t.Fatalf("FilesChanged = %d, want 1", stats.FilesChanged)
	}
	assertCount(t, managed.DB, `SELECT count(*) FROM code_symbols WHERE name = 'Greet'`, 0)
	assertCount(t, managed.DB, `SELECT count(*) FROM code_symbols WHERE name = 'Welcome'`, 1)

	if err := os.Remove(filepath.Join(root, "other.js")); err != nil {
		t.Fatal(err)
	}
	stats, err = idx.Reconcile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.FilesDeleted != 1 {
		t.Fatalf("FilesDeleted = %d, want 1", stats.FilesDeleted)
	}
	assertCount(t, managed.DB, `SELECT count(*) FROM code_files WHERE path = 'other.js'`, 0)
}

func TestWatcherIndexesNewFiles(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(t.TempDir(), "codesql")

	managed, _, err := OpenManaged(context.Background(), Options{
		Name:     "watch",
		Root:     root,
		CacheDir: cacheDir,
		Watch:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer managed.Close()

	writeFile(t, filepath.Join(root, "watch.go"), `package main

func Watched() {}
`)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var got int
		err := managed.DB.QueryRowContext(context.Background(), `SELECT count(*) FROM code_symbols WHERE name = 'Watched'`).Scan(&got)
		if err != nil {
			t.Fatal(err)
		}
		if got == 1 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("watcher did not index new file")
}

func TestMarkdownFencesBecomeVirtualIndexedFiles(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(t.TempDir(), "codesql")
	writeFile(t, filepath.Join(root, "README.md"), "# Example\n\n```go\npackage main\nfunc FromFence() {}\n```\n")

	managed, _, err := OpenManaged(context.Background(), Options{
		Name:     "docs",
		Root:     root,
		CacheDir: cacheDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer managed.Close()

	assertCount(t, managed.DB, `SELECT count(*) FROM code_files WHERE path = 'README.md' AND language = 'markdown'`, 1)
	assertCount(t, managed.DB, `SELECT count(*) FROM code_files WHERE path LIKE 'README.md#fence-%' AND is_virtual = 1 AND language = 'go'`, 1)
	assertCount(t, managed.DB, `SELECT count(*) FROM code_injections WHERE language = 'go'`, 1)
	assertCount(t, managed.DB, `SELECT count(*) FROM code_symbols WHERE name = 'FromFence'`, 1)
}

func TestDBRefsInferAndResolveAgainstTargets(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(t.TempDir(), "codesql")
	writeFile(t, filepath.Join(root, "main.go"), `package main

type User struct {
	Email string `+"`db:\"email\" json:\"email\"`"+`
	Name  string `+"`gorm:\"column:name\"`"+`
}

func LoadUser() {
	query := "SELECT users.email FROM users JOIN teams ON teams.id = users.team_id WHERE users.id = ?"
	_ = query
}
`)
	writeFile(t, filepath.Join(root, "queries", "users.graphql"), `query {
	users {
		id
		email
		name
	}
}
`)
	writeFile(t, filepath.Join(root, "migrations", "001_users.sql"), `CREATE TABLE users (
	id integer primary key,
	email text not null,
	team_id integer REFERENCES teams(id)
);

INSERT INTO users (email) VALUES ('a@example.com');
`)
	writeFile(t, filepath.Join(root, "config", "dev.yml"), `tables:
  - name: users
    columns:
      - name: team_id
        related_to: teams.id
`)

	managed, _, err := OpenManaged(context.Background(), Options{
		Name:        "code",
		Root:        root,
		CacheDir:    cacheDir,
		InferDBRefs: true,
		RefTargets: []DBRefTarget{
			{DatabaseName: "app", SchemaName: "main", TableName: "users", Columns: []string{"id", "email", "name", "team_id"}},
			{DatabaseName: "app", SchemaName: "main", TableName: "teams", Columns: []string{"id"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer managed.Close()

	assertMinCount(t, managed.DB, `SELECT count(*) FROM code_db_refs WHERE table_key = 'app:main.users' AND resolved = 1`, 10)
	assertCount(t, managed.DB, `SELECT count(*) FROM code_db_refs WHERE column_key = 'app:main.users.email' AND ref_kind = 'graphql'`, 1)
	assertCount(t, managed.DB, `SELECT count(*) FROM code_db_refs WHERE column_key = 'app:main.users.email' AND ref_kind = 'struct_tag'`, 1)
	assertCount(t, managed.DB, `SELECT count(*) FROM code_db_refs WHERE column_key = 'app:main.teams.id' AND evidence = 'sql_references'`, 1)
	assertCount(t, managed.DB, `SELECT count(*) FROM code_db_refs WHERE ref_kind = 'config' AND table_key = 'app:main.users'`, 1)
	assertMinCount(t, managed.DB, `SELECT count(*) FROM code_db_refs r JOIN code_symbols s ON s.id = r.symbol_id WHERE r.ref_kind = 'sql_string' AND s.name = 'LoadUser'`, 4)
}

func TestDBRefsLeaveDuplicateTablesAmbiguous(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(t.TempDir(), "codesql")
	writeFile(t, filepath.Join(root, "query.sql"), `SELECT * FROM users`)

	managed, _, err := OpenManaged(context.Background(), Options{
		Name:        "code",
		Root:        root,
		CacheDir:    cacheDir,
		InferDBRefs: true,
		RefTargets: []DBRefTarget{
			{DatabaseName: "app", SchemaName: "main", TableName: "users", Columns: []string{"id"}},
			{DatabaseName: "analytics", SchemaName: "main", TableName: "users", Columns: []string{"id"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer managed.Close()

	assertCount(t, managed.DB, `SELECT count(*) FROM code_db_refs WHERE table_name = 'users' AND resolved = 0 AND ambiguous = 1 AND table_key = ''`, 1)
}

func TestInferDBRefsReloadBackfillsUnchangedCache(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(t.TempDir(), "codesql")
	writeFile(t, filepath.Join(root, "main.go"), `package main

func LookupUser() {
	query := "SELECT users.email FROM users WHERE users.id = ?"
	_ = query
}
`)

	managed, _, err := OpenManaged(context.Background(), Options{
		Name:        "code",
		Root:        root,
		CacheDir:    cacheDir,
		InferDBRefs: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertCount(t, managed.DB, `SELECT count(*) FROM code_db_refs`, 0)
	if err := managed.Close(); err != nil {
		t.Fatal(err)
	}

	managed, _, err = OpenManaged(context.Background(), Options{
		Name:        "code",
		Root:        root,
		CacheDir:    cacheDir,
		InferDBRefs: true,
		RefTargets: []DBRefTarget{
			{DatabaseName: "app", SchemaName: "main", TableName: "users", Columns: []string{"id", "email"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer managed.Close()

	assertCount(t, managed.DB, `SELECT count(*) FROM code_db_refs WHERE column_key = 'app:main.users.email' AND resolved = 1`, 1)
}

func TestInferDBRefsReloadDisabledClearsStaleRefs(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(t.TempDir(), "codesql")
	writeFile(t, filepath.Join(root, "query.sql"), `SELECT users.email FROM users WHERE users.id = ?`)

	managed, _, err := OpenManaged(context.Background(), Options{
		Name:        "code",
		Root:        root,
		CacheDir:    cacheDir,
		InferDBRefs: true,
		RefTargets: []DBRefTarget{
			{DatabaseName: "app", SchemaName: "main", TableName: "users", Columns: []string{"id", "email"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertCount(t, managed.DB, `SELECT count(*) FROM code_db_refs WHERE column_key = 'app:main.users.email' AND resolved = 1`, 1)
	if err := managed.Close(); err != nil {
		t.Fatal(err)
	}

	managed, _, err = OpenManaged(context.Background(), Options{
		Name:        "code",
		Root:        root,
		CacheDir:    cacheDir,
		InferDBRefs: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer managed.Close()

	assertCount(t, managed.DB, `SELECT count(*) FROM code_db_refs`, 0)
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertCount(t *testing.T, db *sql.DB, query string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRowContext(context.Background(), query).Scan(&got); err != nil {
		t.Fatalf("query %q failed: %v", query, err)
	}
	if got != want {
		t.Fatalf("query %q count = %d, want %d", query, got, want)
	}
}

func assertMinCount(t *testing.T, db *sql.DB, query string, wantMin int) {
	t.Helper()
	var got int
	if err := db.QueryRowContext(context.Background(), query).Scan(&got); err != nil {
		t.Fatalf("query %q failed: %v", query, err)
	}
	if got < wantMin {
		t.Fatalf("query %q count = %d, want at least %d", query, got, wantMin)
	}
}
