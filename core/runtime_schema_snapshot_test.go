package core

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestRuntimeSchemaCacheFirstUsesFullSnapshot(t *testing.T) {
	base := t.TempDir()
	dbPath := filepath.Join(base, "app.sqlite3")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE customers (id INTEGER PRIMARY KEY, email TEXT DEFAULT 'unknown')`); err != nil {
		t.Fatal(err)
	}
	conf := &Config{DBType: "sqlite", DisableAllowList: true}
	fs := NewOsFS(base)
	options := []Option{
		OptionSetFS(fs),
		OptionSetDBSchemaWatcherDisabled(true),
		OptionSetRuntimeSchemaDDLDir("generation"),
	}
	live, err := NewGraphJin(conf, db, options...)
	if err != nil {
		t.Fatalf("live discovery: %v", err)
	}
	live.Close()
	if _, err := os.Stat(filepath.Join(base, "generation", filepath.Base(RuntimeSchemaSnapshotPath(DefaultDBName)))); err != nil {
		t.Fatalf("full schema snapshot was not written: %v", err)
	}

	if _, err := db.Exec(`CREATE TABLE audit_events (id INTEGER PRIMARY KEY, message TEXT)`); err != nil {
		t.Fatal(err)
	}
	cacheOptions := append(append([]Option{}, options...),
		OptionSetRuntimeSchemaCacheFirst(true),
		OptionSetRuntimeSchemaCacheRequired(true),
	)
	cached, err := NewGraphJin(conf, db, cacheOptions...)
	if err != nil {
		t.Fatalf("cache-first core: %v", err)
	}
	defer cached.Close()
	if tableNamed(cached.GetTables(), "audit_events") {
		t.Fatal("cache-first initialization unexpectedly ran live discovery")
	}
	customers := tableInfoNamed(cached.GetTables(), "customers")
	if customers == nil {
		t.Fatal("cached customers table is missing")
	}
	metadata, err := cached.MetadataSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	columnCount := 0
	for _, column := range metadata.Columns {
		if column.TableName == "customers" {
			columnCount++
		}
	}
	if columnCount != 2 {
		t.Fatalf("cached customers columns = %d, want 2", columnCount)
	}

	refreshed, err := NewGraphJin(conf, db, options...)
	if err != nil {
		t.Fatalf("refreshed live core: %v", err)
	}
	defer refreshed.Close()
	if !tableNamed(refreshed.GetTables(), "audit_events") {
		t.Fatal("live discovery did not observe newly added table")
	}
}

func TestReloadFromRuntimeSchemaCacheIsAtomic(t *testing.T) {
	base := t.TempDir()
	db, err := sql.Open("sqlite3", filepath.Join(base, "reload.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE customers (id INTEGER PRIMARY KEY, email TEXT)`); err != nil {
		t.Fatal(err)
	}
	conf := &Config{DBType: "sqlite", DisableAllowList: true}
	fs := NewOsFS("")
	generationOne := filepath.Join(base, "generation-one")
	generationTwo := filepath.Join(base, "generation-two")
	writeGeneration := func(dir string) {
		t.Helper()
		live, err := NewGraphJin(conf, db,
			OptionSetFS(fs),
			OptionSetDBSchemaWatcherDisabled(true),
			OptionSetRuntimeSchemaDDLDir(dir),
		)
		if err != nil {
			t.Fatalf("write %s: %v", dir, err)
		}
		live.Close()
	}
	writeGeneration(generationOne)
	stable, err := NewGraphJin(conf, db,
		OptionSetFS(fs),
		OptionSetDBSchemaWatcherDisabled(true),
		OptionSetRuntimeSchemaDDLDir(generationOne),
		OptionSetRuntimeSchemaCacheFirst(true),
		OptionSetRuntimeSchemaCacheRequired(true),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer stable.Close()
	if _, err := db.Exec(`CREATE TABLE orders (id INTEGER PRIMARY KEY, customer_id INTEGER REFERENCES customers(id))`); err != nil {
		t.Fatal(err)
	}
	writeGeneration(generationTwo)
	if err := stable.ReloadFromRuntimeSchemaCache(generationTwo); err != nil {
		t.Fatalf("reload activated generation: %v", err)
	}
	if !tableNamed(stable.GetTables(), "orders") {
		t.Fatal("atomic reload did not expose the activated generation")
	}

	corruptGeneration := filepath.Join(base, "corrupt-generation")
	if err := fs.Put(filepath.Join(corruptGeneration, filepath.Base(RuntimeSchemaSnapshotPath(DefaultDBName))), []byte("not-json")); err != nil {
		t.Fatal(err)
	}
	if err := stable.ReloadFromRuntimeSchemaCache(corruptGeneration); err == nil {
		t.Fatal("corrupt generation reload unexpectedly succeeded")
	}
	if !tableNamed(stable.GetTables(), "orders") {
		t.Fatal("failed reload replaced the previously active engine")
	}
}

func tableNamed(tables []TableInfo, name string) bool {
	return tableInfoNamed(tables, name) != nil
}

func tableInfoNamed(tables []TableInfo, name string) *TableInfo {
	for n := range tables {
		if tables[n].Name == name {
			return &tables[n]
		}
	}
	return nil
}
