package main

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/hostedemu"
	hostedbigquery "github.com/dosco/graphjin/hostedemu/bigquery"
	"github.com/dosco/graphjin/serv/v3"
	"github.com/spf13/cobra"
)

func TestParseDBFlags_Empty(t *testing.T) {
	primary, overrides := parseDBFlags([]string{})
	if primary != "" {
		t.Errorf("expected empty primary, got %q", primary)
	}
	if len(overrides) != 0 {
		t.Errorf("expected empty overrides, got %v", overrides)
	}
}

func TestParseDBFlags_SingleType(t *testing.T) {
	primary, overrides := parseDBFlags([]string{"postgres"})
	if primary != "postgres" {
		t.Errorf("expected 'postgres', got %q", primary)
	}
	if len(overrides) != 0 {
		t.Errorf("expected empty overrides, got %v", overrides)
	}
}

func TestParseDBFlags_NamedOverride(t *testing.T) {
	primary, overrides := parseDBFlags([]string{"primary=mysql", "secondary=postgres"})
	if primary != "" {
		t.Errorf("expected empty primary, got %q", primary)
	}
	if overrides["primary"] != "mysql" {
		t.Errorf("expected 'mysql' for primary, got %q", overrides["primary"])
	}
	if overrides["secondary"] != "postgres" {
		t.Errorf("expected 'postgres' for secondary, got %q", overrides["secondary"])
	}
}

func TestParseDBFlags_Mixed(t *testing.T) {
	primary, overrides := parseDBFlags([]string{"postgres", "analytics=mysql"})
	if primary != "postgres" {
		t.Errorf("expected 'postgres', got %q", primary)
	}
	if overrides["analytics"] != "mysql" {
		t.Errorf("expected 'mysql' for analytics, got %q", overrides["analytics"])
	}
}

func TestParseDBFlags_TableDriven(t *testing.T) {
	tests := []struct {
		name              string
		flags             []string
		expectedPrimary   string
		expectedOverrides map[string]string
	}{
		{
			name:              "empty",
			flags:             []string{},
			expectedPrimary:   "",
			expectedOverrides: map[string]string{},
		},
		{
			name:              "single type",
			flags:             []string{"postgres"},
			expectedPrimary:   "postgres",
			expectedOverrides: map[string]string{},
		},
		{
			name:              "named override",
			flags:             []string{"primary=mysql"},
			expectedPrimary:   "",
			expectedOverrides: map[string]string{"primary": "mysql"},
		},
		{
			name:              "mixed",
			flags:             []string{"postgres", "analytics=mysql"},
			expectedPrimary:   "postgres",
			expectedOverrides: map[string]string{"analytics": "mysql"},
		},
		{
			name:            "multiple overrides",
			flags:           []string{"db1=postgres", "db2=mysql"},
			expectedPrimary: "",
			expectedOverrides: map[string]string{
				"db1": "postgres",
				"db2": "mysql",
			},
		},
		{
			name:            "override with equals in value",
			flags:           []string{"db=type=foo"},
			expectedPrimary: "",
			expectedOverrides: map[string]string{
				"db": "type=foo",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			primary, overrides := parseDBFlags(tc.flags)
			if primary != tc.expectedPrimary {
				t.Errorf("primary = %q, want %q", primary, tc.expectedPrimary)
			}
			if len(overrides) != len(tc.expectedOverrides) {
				t.Errorf("overrides len = %d, want %d", len(overrides), len(tc.expectedOverrides))
			}
			for k, v := range tc.expectedOverrides {
				if overrides[k] != v {
					t.Errorf("overrides[%s] = %q, want %q", k, overrides[k], v)
				}
			}
		})
	}
}

func TestDemoStateCreatesAndReusesManifest(t *testing.T) {
	oldCpath, oldConf := cpath, conf
	defer func() {
		cpath = oldCpath
		conf = oldConf
	}()

	cpath = t.TempDir()
	conf = &serv.Config{Core: core.Config{Databases: map[string]core.DatabaseConfig{
		"ops": {Type: "postgres"},
	}}}

	var out bytes.Buffer
	state, err := initDemoState(demoStatus{out: &out})
	if err != nil {
		t.Fatalf("initDemoState first run: %v", err)
	}
	if !state.FirstRun {
		t.Fatal("expected first run state")
	}
	if got := filepath.Base(state.Dir); got != "demo" {
		t.Fatalf("state dir base = %q, want demo", got)
	}
	if err := state.writeManifest(nil); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	out.Reset()
	state, err = initDemoState(demoStatus{out: &out})
	if err != nil {
		t.Fatalf("initDemoState reuse: %v", err)
	}
	if state.FirstRun {
		t.Fatal("expected reused state")
	}
	if !strings.Contains(out.String(), "reused") {
		t.Fatalf("expected reused status, got %q", out.String())
	}
}

func TestDemoStateInvalidManifestFailsWithResetPath(t *testing.T) {
	oldCpath := cpath
	defer func() { cpath = oldCpath }()

	cpath = t.TempDir()
	if err := os.Mkdir(filepath.Join(cpath, "demo"), 0o755); err != nil {
		t.Fatalf("mkdir demo: %v", err)
	}

	_, err := initDemoState(demoStatus{})
	if err == nil {
		t.Fatal("expected invalid state error")
	}
	if !strings.Contains(err.Error(), "delete") || !strings.Contains(err.Error(), "demo") {
		t.Fatalf("error should explain reset path, got %v", err)
	}
}

func TestDemoStatusWritesToProvidedWriter(t *testing.T) {
	var out bytes.Buffer
	demoStatus{out: &out}.Emit("ops", "verified", "Postgres is accepting connections")
	got := out.String()
	if !strings.Contains(got, "demo ops") || !strings.Contains(got, "verified") {
		t.Fatalf("unexpected status output: %q", got)
	}
}

func TestDemoHelpDoesNotExposePersistFlag(t *testing.T) {
	for _, cmd := range []*cobra.Command{servCmd(), mcpCmd()} {
		if flag := cmd.Flags().Lookup("persist"); flag != nil {
			t.Fatalf("%s still exposes --persist", cmd.Name())
		}
		help := cmd.UsageString()
		if strings.Contains(help, "--persist") {
			t.Fatalf("%s help still mentions --persist:\n%s", cmd.Name(), help)
		}
	}
}

func TestCoffeeRoasteryDemoConfigNormalizes(t *testing.T) {
	cfg, err := serv.ReadInConfig("../examples/coffee-roastery/dev")
	if err != nil {
		t.Fatalf("read coffee demo config: %v", err)
	}
	if err := cfg.Core.NormalizeSources(); err != nil {
		t.Fatalf("normalize coffee demo sources: %v", err)
	}

	if got := cfg.Databases["ops"].Type; got != "postgres" {
		t.Fatalf("ops type = %q, want postgres", got)
	}
	if got := cfg.Databases["roast_warehouse"].Type; got != "bigquery" {
		t.Fatalf("roast_warehouse type = %q, want bigquery", got)
	}
	if got := cfg.Databases["business_code"].Type; got != "codesql" {
		t.Fatalf("business_code type = %q, want codesql", got)
	}
	if !cfg.Databases["roast_warehouse"].ReadOnly {
		t.Fatal("roast_warehouse should be read-only")
	}
	if got := cfg.Databases["roast_warehouse"].Path; got != "" {
		t.Fatalf("roast_warehouse path = %q, want empty canonical DDL discovery", got)
	}
}

func TestCoffeeRoasteryOpsDDLBootsMockDB(t *testing.T) {
	data, err := os.ReadFile("../examples/coffee-roastery/schema-ddl/ops.ddl")
	if err != nil {
		t.Fatalf("read ops DDL: %v", err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, core.SchemaDDLFile), data, 0o644); err != nil {
		t.Fatalf("write temp DDL: %v", err)
	}

	gj, err := core.NewGraphJinWithFS(&core.Config{
		MockDB:           true,
		EnableSchema:     true,
		DisableAllowList: true,
		DBType:           "postgres",
	}, nil, core.NewOsFS(dir))
	if err != nil {
		t.Fatalf("NewGraphJinWithFS: %v", err)
	}
	defer gj.Close()

	if _, err := gj.GraphQL(context.Background(), `query { customers { id name } }`, nil, nil); err != nil {
		t.Fatalf("mock query against ops DDL: %v", err)
	}
}

func TestComputeSchemaDiffMultiBigQueryUnsupportedLiveDDL(t *testing.T) {
	oldCpath, oldConf := cpath, conf
	defer func() {
		cpath = oldCpath
		conf = oldConf
	}()

	cpath = t.TempDir()
	conf = &serv.Config{Core: core.Config{Databases: map[string]core.DatabaseConfig{
		"roast_warehouse": {Type: "bigquery"},
	}}}
	if err := os.MkdirAll(filepath.Join(cpath, core.SourceSchemaDDLDir), 0o755); err != nil {
		t.Fatalf("mkdir schema-ddl: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cpath, core.SourceSchemaDDLDir, "roast_warehouse.ddl"), []byte(`
type roast_batches {
  id: Bigint! @id
}
`), 0o644); err != nil {
		t.Fatalf("write DDL: %v", err)
	}

	_, err := computeSchemaDiffMulti(false)
	if err == nil {
		t.Fatal("expected BigQuery live DDL to be unsupported")
	}
	if !strings.Contains(err.Error(), "not supported") || !strings.Contains(err.Error(), "bigquery") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCoffeeRoasteryBigQueryDDLAndSeedScript(t *testing.T) {
	schemaData, err := os.ReadFile("../examples/coffee-roastery/schema-ddl/roast_warehouse.ddl")
	if err != nil {
		t.Fatalf("read roast warehouse DDL: %v", err)
	}
	sqls, err := core.GenerateSchemaSQL("bigquery", schemaData, nil)
	if err != nil {
		t.Fatalf("GenerateSchemaSQL: %v", err)
	}

	db := sql.OpenDB(hostedemu.NewConnector(hostedemu.Config{
		SeedSQL:  strings.Join(sqls, "\n\n"),
		DBPath:   filepath.Join(t.TempDir(), "warehouse.duckdb"),
		Backend:  hostedemu.BackendDuckDB,
		Fallback: hostedemu.FallbackStrict,
		TestName: "coffee-roastery-roast-warehouse",
	}, hostedbigquery.NewAdapter()))
	defer db.Close() //nolint:errcheck
	if err := db.Ping(); err != nil {
		t.Fatalf("ping simulator: %v", err)
	}

	oldCpath, oldConf := cpath, conf
	defer func() {
		cpath = oldCpath
		conf = oldConf
	}()
	cpath = filepath.Clean("../examples/coffee-roastery")
	conf = &serv.Config{Core: core.Config{DisableAllowList: true}}

	seedPath := filepath.Join(cpath, "seed", "roast_warehouse.js")
	if err := compileAndRunJSWithContext(seedPath, seedJSContext{
		DB:            db,
		ConfigPath:    cpath,
		DefaultSource: "roast_warehouse",
	}); err != nil {
		t.Fatalf("run roast warehouse seed: %v", err)
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM roast_batches").Scan(&count); err != nil {
		t.Fatalf("query seeded batches: %v", err)
	}
	if count != 3 {
		t.Fatalf("roast_batches count = %d, want 3", count)
	}
}

func TestExamplesUseCanonicalDemoSchemaFiles(t *testing.T) {
	var bad []string
	root := "../examples"
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == "demo" {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.EqualFold(filepath.Ext(path), ".sql") {
			return nil
		}
		clean := filepath.ToSlash(path)
		if strings.Contains(clean, "/schema/") || strings.Contains(clean, "/seed/") {
			bad = append(bad, clean)
		}
		return nil
	}); err != nil {
		t.Fatalf("walk examples: %v", err)
	}
	if len(bad) != 0 {
		t.Fatalf("curated examples should use schema-ddl/*.ddl plus JS seeds, found SQL fixtures: %s", strings.Join(bad, ", "))
	}
}
