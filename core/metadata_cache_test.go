package core

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestMetadataSnapshotIsReusedUntilReload(t *testing.T) {
	base := t.TempDir()
	db, err := sql.Open("sqlite3", filepath.Join(base, "app.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE customers (id INTEGER PRIMARY KEY, email TEXT)`); err != nil {
		t.Fatal(err)
	}
	gj, err := NewGraphJin(&Config{DBType: "sqlite", DisableAllowList: true}, db,
		OptionSetFS(NewOsFS(base)), OptionSetDBSchemaWatcherDisabled(true))
	if err != nil {
		t.Fatal(err)
	}
	defer gj.Close()

	first, err := gj.MetadataSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	second, err := gj.MetadataSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("expected the same snapshot until the schema reloads")
	}
	excluded, err := gj.MetadataSnapshot(DefaultDBName)
	if err != nil {
		t.Fatal(err)
	}
	if excluded == first || len(excluded.Tables) != 0 {
		t.Fatalf("expected a separate snapshot without the excluded database, got %d tables", len(excluded.Tables))
	}

	if _, err := db.Exec(`CREATE TABLE orders (id INTEGER PRIMARY KEY, total REAL)`); err != nil {
		t.Fatal(err)
	}
	if err := gj.Reload(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := gj.MetadataSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded == first {
		t.Fatal("expected a new snapshot after the schema reload")
	}
	found := false
	for _, table := range reloaded.Tables {
		if table.TableName == "orders" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected the reloaded snapshot to include the new table")
	}
}
