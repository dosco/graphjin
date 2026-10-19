package main

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/hostedemu"
	hostedbigquery "github.com/dosco/graphjin/hostedemu/bigquery"
	hostedsnowflake "github.com/dosco/graphjin/hostedemu/snowflake"
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
	rootAccess := cfg.Core.EffectiveSystemRootAccess()
	for _, root := range []string{"gj_catalog", "gj_artifacts", "gj_workflow", "gj_workflow_execution", "gj_runtime", "gj_security"} {
		if got := rootAccess[root]; got != core.AccessModePublic {
			t.Fatalf("system %s root access = %q, want public in dev demo", root, got)
		}
	}
	// gj_config is deliberately admin-gated in the demo so the smoke suite can
	// assert role-based control-plane access end to end.
	if got := rootAccess["gj_config"]; got != core.AccessModeAdmin {
		t.Fatalf("system gj_config root access = %q, want admin", got)
	}
}

func TestCoffeeRoasteryMinimalAgenticConfigResolvesRuntimeDefaults(t *testing.T) {
	cfg, err := serv.ReadInConfig("../examples/coffee-roastery/agentic")
	if err != nil {
		t.Fatalf("read coffee demo agentic config: %v", err)
	}
	if err := cfg.Core.NormalizeSources(); err != nil {
		t.Fatalf("normalize coffee demo sources: %v", err)
	}

	if !cfg.Agent.Enabled {
		t.Fatal("demo agentic config should enable the agent")
	}
	if got := cfg.Agent.Provider; got != "openai" {
		t.Fatalf("agent provider = %q, want openai", got)
	}
	if got := cfg.Agent.APIKeyEnv; got != "OPENAI_API_KEY" {
		t.Fatalf("agent api key env = %q, want OPENAI_API_KEY", got)
	}
	if got := cfg.Agent.Model; got != "" {
		t.Fatalf("agent model = %q, want provider default", got)
	}
	if got := cfg.Agent.MaxSteps; got != 8 {
		t.Fatalf("agent max steps = %d, want default 8", got)
	}
	if got := cfg.Agent.TimeoutSeconds; got != 50 {
		t.Fatalf("agent timeout = %d, want default 50", got)
	}
	if !cfg.MCP.IncludeToolsWithAgent {
		t.Fatalf("MCP runtime defaults missing: %+v", cfg.MCP)
	}
	if !cfg.Core.Artifacts.Enabled || cfg.Core.Artifacts.Source == "__graphjin_artifacts" || !cfg.Core.Watches.Enabled || cfg.Core.Watches.Runner != "all" {
		t.Fatalf("artifact/watch runtime defaults missing: artifacts=%+v watches=%+v", cfg.Core.Artifacts, cfg.Core.Watches)
	}
	data, err := os.ReadFile("../examples/coffee-roastery/agentic.yml")
	if err != nil {
		t.Fatal(err)
	}
	for _, redundant := range []string{"agent:", "sampling:", "http_stateful:", "include_tools_with_agent:", "watches:"} {
		if strings.Contains(string(data), redundant) {
			t.Fatalf("minimal agentic config contains redundant %q:\n%s", redundant, data)
		}
	}
}

func TestDemoConfigsKeepZeroConfigurationDefaultsMinimal(t *testing.T) {
	for _, demo := range []string{"coffee-roastery", "saas-ops", "corrugated-plant", "pcb-fab"} {
		t.Run(demo, func(t *testing.T) {
			agenticPath := filepath.Join("..", "examples", demo, "agentic.yml")
			agentic, err := os.ReadFile(agenticPath)
			if err != nil {
				t.Fatal(err)
			}
			for _, redundant := range []string{"\nagent:", "sampling:", "http_stateful:", "include_tools_with_agent:", "\nwatches:", "\nartifacts:"} {
				if strings.Contains(string(agentic), redundant) {
					t.Fatalf("agentic.yml contains redundant %q:\n%s", redundant, agentic)
				}
			}

			dev, err := os.ReadFile(filepath.Join("..", "examples", demo, "dev.yml"))
			if err != nil {
				t.Fatal(err)
			}
			for _, redundant := range []string{"artifacts:\n  enabled:", "artifacts:\n  source:", "\nwatches:"} {
				if strings.Contains(string(dev), redundant) {
					t.Fatalf("dev.yml contains redundant artifact/watch config %q:\n%s", redundant, dev)
				}
			}

			cfg, err := serv.ReadInConfig(filepath.Join("..", "examples", demo, "agentic"))
			if err != nil {
				t.Fatalf("ReadInConfig: %v", err)
			}
			if !cfg.Agent.Enabled || !cfg.MCP.IncludeToolsWithAgent || !cfg.Core.Artifacts.Enabled || !cfg.Core.Watches.Enabled || cfg.Core.Watches.Runner != "all" {
				t.Fatalf("minimal config did not resolve complete runtime: agent=%+v mcp=%+v artifacts=%+v watches=%+v", cfg.Agent, cfg.MCP, cfg.Core.Artifacts, cfg.Core.Watches)
			}
		})
	}
}

func TestCoffeeRoasterySmokeScriptCoversConnectedAgenticSurfaces(t *testing.T) {
	path := "../examples/coffee-roastery/scripts/smoke.sh"
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat smoke script: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("smoke script should be executable, mode=%s", info.Mode())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read smoke script: %v", err)
	}
	text := string(data)
	for _, want := range []string{
		"customers",
		"roast_batches",
		"gj_code",
		"business_code",
		"gj_catalog",
		"daily_roast_context",
		"batch_quality_snapshot",
		"customer_issue_context",
		"gj_workflow_execution",
		"daily_roast_plan",
		"batch_quality_review",
		"customer_issue_triage",
		"query_catalog",
		"ask_graphjin_agent",
		"/api/v1/agent",
		"--agent-eval",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("smoke script should cover %q", want)
		}
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

// runWarehouseSeedScript boots a warehouse simulator from an example's DDL and
// runs its JS seed against it, returning the opened database for assertions.
func runWarehouseSeedScript(t *testing.T, examplePath, source, dialect string, adapter hostedemu.Adapter) *sql.DB {
	t.Helper()
	schemaData, err := os.ReadFile(filepath.Join("..", examplePath, "schema-ddl", source+".ddl"))
	if err != nil {
		t.Fatalf("read %s DDL: %v", source, err)
	}
	sqls, err := core.GenerateSchemaSQL(dialect, schemaData, nil)
	if err != nil {
		t.Fatalf("GenerateSchemaSQL: %v", err)
	}

	db := sql.OpenDB(hostedemu.NewConnector(hostedemu.Config{
		SeedSQL:  strings.Join(sqls, "\n\n"),
		DBPath:   filepath.Join(t.TempDir(), "warehouse.duckdb"),
		Backend:  hostedemu.BackendDuckDB,
		Fallback: hostedemu.FallbackStrict,
		TestName: source,
	}, adapter))
	t.Cleanup(func() { db.Close() }) //nolint:errcheck
	if err := db.Ping(); err != nil {
		t.Fatalf("ping simulator: %v", err)
	}

	oldCpath, oldConf := cpath, conf
	t.Cleanup(func() {
		cpath = oldCpath
		conf = oldConf
	})
	cpath = filepath.Clean(filepath.Join("..", examplePath))
	conf = &serv.Config{Core: core.Config{DisableAllowList: true}}

	seedPath := filepath.Join(cpath, "seed", source+".js")
	if err := compileAndRunJSWithContext(seedPath, seedJSContext{
		DB:            db,
		ConfigPath:    cpath,
		DefaultSource: source,
	}); err != nil {
		t.Fatalf("run %s seed: %v", source, err)
	}
	return db
}

// assertRecentMondays checks every value in the column is a Monday in the
// recent past — the shape demoWeekStart seeds must produce regardless of when
// the seed runs.
func assertRecentMondays(t *testing.T, db *sql.DB, table, column string, wantRows int) {
	t.Helper()
	rows, err := db.Query("SELECT CAST(" + column + " AS VARCHAR) FROM " + table)
	if err != nil {
		t.Fatalf("query %s: %v", table, err)
	}
	defer rows.Close() //nolint:errcheck
	count := 0
	today := time.Now().UTC().Truncate(24 * time.Hour)
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			t.Fatalf("scan %s.%s: %v", table, column, err)
		}
		day, err := time.Parse("2006-01-02", strings.TrimSpace(raw))
		if err != nil {
			t.Fatalf("%s.%s = %q is not a date: %v", table, column, raw, err)
		}
		if day.Weekday() != time.Monday {
			t.Errorf("%s.%s = %s falls on %s, want Monday", table, column, raw, day.Weekday())
		}
		if day.After(today) {
			t.Errorf("%s.%s = %s is in the future", table, column, raw)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if count != wantRows {
		t.Fatalf("%s rows = %d, want %d", table, count, wantRows)
	}
}

// The corrugated and pcb-fab warehouse seeds compute week_start rows with the
// demoWeekStart helper; these tests run the real seeds through goja against
// the simulators and check the rows land on Mondays in the recent past.
func TestCorrugatedPlantBigQuerySeedWeekStarts(t *testing.T) {
	db := runWarehouseSeedScript(t, "examples/corrugated-plant", "demand_warehouse", "bigquery", hostedbigquery.NewAdapter())
	assertRecentMondays(t, db, "demand_history", "week_start", 8)
	assertRecentMondays(t, db, "material_price_index", "week_start", 5)
}

func TestPCBFabSnowflakeSeedWeekStarts(t *testing.T) {
	db := runWarehouseSeedScript(t, "examples/pcb-fab", "yield_warehouse", "snowflake", hostedsnowflake.NewAdapter())
	assertRecentMondays(t, db, "yield_by_layer_count", "week_start", 6)
	assertRecentMondays(t, db, "defect_pareto", "week_start", 3)
}
