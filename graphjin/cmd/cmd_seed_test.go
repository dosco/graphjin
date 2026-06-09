package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/serv/v3"
)

func TestSourceSeedFilesSortedBySource(t *testing.T) {
	oldCpath := cpath
	defer func() { cpath = oldCpath }()
	cpath = t.TempDir()

	seedDir := filepath.Join(cpath, "seed")
	if err := os.MkdirAll(seedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"warehouse.sql", "ops.js", "alpha.js", "README.md"} {
		if err := os.WriteFile(filepath.Join(seedDir, name), []byte("// seed"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	files, err := sourceSeedFiles(".js")
	if err != nil {
		t.Fatalf("sourceSeedFiles: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("len(files) = %d, want 2", len(files))
	}
	if files[0].Source != "alpha" || files[1].Source != "ops" {
		t.Fatalf("sources = %q, %q; want alpha, ops", files[0].Source, files[1].Source)
	}
}

func TestSeedCoreConfigFiltersToOpenSQLDatabases(t *testing.T) {
	oldConf := conf
	defer func() { conf = oldConf }()

	conf = &serv.Config{Core: core.Config{Databases: map[string]core.DatabaseConfig{
		"ops":           {Type: "postgres"},
		"business_code": {Type: "codesql"},
		"missing":       {Type: "postgres"},
	}}}

	coreConf := seedCoreConfig(seedJSContext{Databases: map[string]*sql.DB{
		"ops":           new(sql.DB),
		"business_code": new(sql.DB),
	}})

	if _, ok := coreConf.Databases["ops"]; !ok {
		t.Fatal("ops source should remain available to seed GraphQL")
	}
	if _, ok := coreConf.Databases["business_code"]; ok {
		t.Fatal("codesql source should not be passed to seed GraphQL initialization")
	}
	if _, ok := coreConf.Databases["missing"]; ok {
		t.Fatal("sources without open seed connections should not be passed to seed GraphQL initialization")
	}
}
