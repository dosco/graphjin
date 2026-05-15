package serv

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dosco/graphjin/core/v3"
)

func TestMetadataGraphPopulatesAndLinksCodeSQLRefs(t *testing.T) {
	appPath := createMetadataAppDB(t)
	source := t.TempDir()
	writeTestFile(t, filepath.Join(source, "main.go"), `package main

func LookupUser() {
	query := "SELECT users.email FROM users WHERE users.id = ?"
	_ = query
}
`)

	conf := &Config{
		Core: Core{
			DisableAllowList: true,
			Sources: []core.SourceConfig{
				{Name: "app", Kind: "sql", Type: "sqlite", Path: appPath, Default: true},
				{Name: "code", Kind: "codesql", Path: source},
				{Name: defaultMetadataDBName, Kind: "graphjin"},
			},
		},
		Serv: Serv{
			ConfigPath: filepath.Join(t.TempDir(), "config"),
			MCP:        MCPConfig{Disable: true},
		},
	}

	s, err := newGraphJinService(conf, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestService(s)

	if s.metadataDB != defaultMetadataDBName {
		t.Fatalf("metadata database = %q, want %q", s.metadataDB, defaultMetadataDBName)
	}
	runtime := s.runtimeCore.Databases[defaultMetadataDBName]
	if runtime.Type != "nanodb" || !runtime.ReadOnly {
		t.Fatalf("metadata runtime = %+v, want read-only nanodb", runtime)
	}
	assertGraphJinTable(t, s, defaultMetadataDBName, "gj_catalog")
	assertGraphJinTable(t, s, defaultMetadataDBName, "gj_workflow")
	assertServiceCount(t, s, "code", `SELECT count(*) FROM gj_code WHERE kind = 'db_reference' AND db_object_id = 'app:main.users.email'`, 1)
	res, err := s.gj.GraphQL(context.Background(), `query {
		gj_catalog(where: { kind: { eq: "column" }, database_name: { eq: "app" }, table_name: { eq: "users" }, column_name: { eq: "email" } }, limit: 1) {
			id
			kind
			database_name
			table_name
			column_name
			code_refs_id
			gj_code {
				kind
				path
				db_object_id
			}
		}
		directives: gj_catalog(where: { kind: { eq: "directive" }, title: { eq: "@running" } }, limit: 1) {
			id
			title
		}
		internal: gj_catalog(where: { database_name: { in: ["code", "graphjin"] } }) {
			id
		}
	}`, nil, nil)
	if err != nil {
		t.Fatalf("query gj_catalog: %v", err)
	}
	var out struct {
		GJCatalog []struct {
			ID           string `json:"id"`
			Kind         string `json:"kind"`
			DatabaseName string `json:"database_name"`
			TableName    string `json:"table_name"`
			ColumnName   string `json:"column_name"`
			CodeRefsID   string `json:"code_refs_id"`
			GJCode       []struct {
				Kind       string `json:"kind"`
				Path       string `json:"path"`
				DBObjectID string `json:"db_object_id"`
			} `json:"gj_code"`
		} `json:"gj_catalog"`
		Directives []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"directives"`
		Internal []struct {
			ID string `json:"id"`
		} `json:"internal"`
	}
	if err := json.Unmarshal(res.Data, &out); err != nil {
		t.Fatalf("decode gj_catalog response: %v\n%s", err, res.Data)
	}
	if len(out.GJCatalog) != 1 || out.GJCatalog[0].Kind != "column" || out.GJCatalog[0].CodeRefsID != "app:main.users.email" {
		t.Fatalf("column catalog row = %+v data=%s", out.GJCatalog, res.Data)
	}
	if len(out.GJCatalog[0].GJCode) != 1 || out.GJCatalog[0].GJCode[0].Kind != "db_reference" || out.GJCatalog[0].GJCode[0].Path != "main.go" {
		t.Fatalf("catalog-to-code join = %+v data=%s", out.GJCatalog[0].GJCode, res.Data)
	}
	if len(out.Directives) != 1 || out.Directives[0].Title != "@running" {
		t.Fatalf("directive catalog row = %+v data=%s", out.Directives, res.Data)
	}
	if len(out.Internal) != 0 {
		t.Fatalf("internal metadata leaked into gj_catalog: %+v", out.Internal)
	}
	assertServiceCount(t, s, "code", `SELECT count(*) FROM code_db_refs WHERE column_key = 'app:main.users.email' AND resolved = 1`, 1)

	if !hasRuntimeRelationship(s, defaultMetadataDBName, "gj_catalog", "code_refs_id", "code:gj_code.db_object_id") {
		t.Fatalf("missing automatic gj_catalog -> gj_code relationship")
	}
}

func TestPopulateMetadataDBDedupesSnapshotRows(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "metadata.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.Exec(metadataSchemaSQL); err != nil {
		t.Fatal(err)
	}

	snapshot := &core.MetadataSnapshot{
		Databases: []core.MetadataDatabase{
			{ID: "app", Name: "app", Type: "sqlite", IsDefault: true},
			{ID: "app", Name: "app", Type: "sqlite", IsDefault: true},
		},
		Tables: []core.MetadataTable{
			{ID: "app:main.users", DatabaseName: "app", SchemaName: "main", TableName: "users", Type: "table", PrimaryKey: "id", ColumnCount: 2, TableKey: "app:main.users"},
			{ID: "app:main.users", DatabaseName: "app", SchemaName: "main", TableName: "users", Type: "table", PrimaryKey: "id", ColumnCount: 2, TableKey: "app:main.users"},
		},
		Columns: []core.MetadataColumn{
			{ID: "app:main.users.id", TableID: "app:main.users", DatabaseName: "app", SchemaName: "main", TableName: "users", ColumnName: "id", Type: "integer", PrimaryKey: true, TableKey: "app:main.users", ColumnKey: "app:main.users.id"},
			{ID: "app:main.users.id", TableID: "app:main.users", DatabaseName: "app", SchemaName: "main", TableName: "users", ColumnName: "id", Type: "integer", PrimaryKey: true, TableKey: "app:main.users", ColumnKey: "app:main.users.id"},
		},
		Relationships: []core.MetadataRelationship{
			{ID: "app:main.users.team_id->app:main.teams.id", FromDatabaseName: "app", FromSchemaName: "main", FromTableName: "users", FromColumnName: "team_id", FromColumnID: "app:main.users.team_id", ToDatabaseName: "app", ToSchemaName: "main", ToTableName: "teams", ToColumnName: "id", ToColumnID: "app:main.teams.id", RelType: "one_to_many", Source: "foreign_key"},
			{ID: "app:main.users.team_id->app:main.teams.id", FromDatabaseName: "app", FromSchemaName: "main", FromTableName: "users", FromColumnName: "team_id", FromColumnID: "app:main.users.team_id", ToDatabaseName: "app", ToSchemaName: "main", ToTableName: "teams", ToColumnName: "id", ToColumnID: "app:main.teams.id", RelType: "one_to_many", Source: "foreign_key"},
		},
		Functions: []core.MetadataFunction{
			{ID: "app:main.lookup_user", DatabaseName: "app", SchemaName: "main", Name: "lookup_user", ReturnType: "integer"},
			{ID: "app:main.lookup_user", DatabaseName: "app", SchemaName: "main", Name: "lookup_user", ReturnType: "integer"},
		},
		Indexes: []core.MetadataIndex{
			{ID: "app:main.users.id:users_pkey", DatabaseName: "app", SchemaName: "main", TableName: "users", ColumnName: "id", Name: "users_pkey", Unique: true},
			{ID: "app:main.users.id:users_pkey", DatabaseName: "app", SchemaName: "main", TableName: "users", ColumnName: "id", Name: "users_pkey", Unique: true},
		},
	}

	if err := populateMetadataDB(context.Background(), db, snapshot); err != nil {
		t.Fatalf("populate metadata: %v", err)
	}

	for _, tc := range []struct {
		table string
		want  int
	}{
		{table: "gj_databases", want: 1},
		{table: "gj_tables", want: 1},
		{table: "gj_columns", want: 1},
		{table: "gj_relationships", want: 1},
		{table: "gj_functions", want: 1},
		{table: "gj_indexes", want: 1},
	} {
		var got int
		if err := db.QueryRow("SELECT count(*) FROM " + tc.table).Scan(&got); err != nil {
			t.Fatalf("count %s: %v", tc.table, err)
		}
		if got != tc.want {
			t.Fatalf("%s count = %d, want %d", tc.table, got, tc.want)
		}
	}
	var catalogCards int
	if err := db.QueryRow("SELECT count(*) FROM gj_catalog_cards").Scan(&catalogCards); err != nil {
		t.Fatalf("count gj_catalog_cards: %v", err)
	}
	if catalogCards < 10 {
		t.Fatalf("gj_catalog_cards count = %d, want at least language/schema catalog cards", catalogCards)
	}
}

func TestMetadataCatalogCardsLifecycleColumns(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "metadata.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.Exec(metadataSchemaSQL); err != nil {
		t.Fatal(err)
	}
	for _, col := range []string{"created_at", "updated_at"} {
		var found int
		if err := db.QueryRow(`SELECT count(*) FROM pragma_table_info('gj_catalog_cards') WHERE name = ?`, col).Scan(&found); err != nil {
			t.Fatalf("check column %s: %v", col, err)
		}
		if found != 1 {
			t.Fatalf("expected gj_catalog_cards.%s column", col)
		}
	}

	snap := core.BuildCatalogSnapshotWithOptions(&core.MetadataSnapshot{}, nil, core.CatalogBuildOptions{
		Workflows: []core.CatalogWorkflow{{
			Name:       "daily_report",
			SourceHash: "abc123",
			CreatedAt:  "2026-01-01T00:00:00Z",
			UpdatedAt:  "2026-01-02T00:00:00Z",
		}},
	})
	if err := populateMetadataDB(context.Background(), db, &core.MetadataSnapshot{}, snap); err != nil {
		t.Fatalf("populate metadata: %v", err)
	}
	var createdAt, updatedAt string
	if err := db.QueryRow(`SELECT created_at, updated_at FROM gj_catalog_cards WHERE id = 'workflow:daily_report'`).Scan(&createdAt, &updatedAt); err != nil {
		t.Fatalf("query workflow catalog card timestamps: %v", err)
	}
	if createdAt != "2026-01-01T00:00:00Z" || updatedAt != "2026-01-02T00:00:00Z" {
		t.Fatalf("unexpected workflow timestamps in metadata db: created=%q updated=%q", createdAt, updatedAt)
	}
}

func TestMetadataCatalogMirrorRefreshesWorkflowRowsAfterMutation(t *testing.T) {
	appPath := createMetadataAppDB(t)
	conf := &Config{
		Core: Core{
			DisableAllowList: true,
			Sources: []core.SourceConfig{
				{Name: "app", Kind: "sql", Type: "sqlite", Path: appPath, Default: true},
				{Name: defaultMetadataDBName, Kind: "graphjin"},
				{Name: "workflows", Kind: "workflows"},
			},
		},
		Serv: Serv{
			ConfigPath: filepath.Join(t.TempDir(), "config"),
			MCP:        MCPConfig{AllowWorkflowUpdates: true},
		},
	}

	s, err := newGraphJinService(conf, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestService(s)

	assertWorkflowCatalogRows(t, s, "daily_report", "", 0)
	res, err := s.gj.GraphQL(context.Background(), `mutation {
		gj_workflow(insert: {
			name: "daily_report"
			description: "Daily report"
			code: "function main(input) { return { ok: true }; }"
		}) {
			name
			workflow_revision
			catalog_revision
		}
	}`, nil, &core.RequestConfig{})
	if err != nil {
		t.Fatalf("insert workflow: %v", err)
	}
	if len(res.Errors) != 0 {
		t.Fatalf("insert workflow errors: %+v", res.Errors)
	}
	assertWorkflowCatalogRows(t, s, "daily_report", "Daily report", 1)

	time.Sleep(2 * time.Millisecond)
	res, err = s.gj.GraphQL(context.Background(), `mutation {
		gj_workflow(where: { name: { eq: "daily_report" } }, update: {
			description: "Updated daily report"
			code: "function main(input) { return { updated: true }; }"
		}) {
			name
			workflow_revision
			catalog_revision
		}
	}`, nil, &core.RequestConfig{})
	if err != nil {
		t.Fatalf("update workflow: %v", err)
	}
	if len(res.Errors) != 0 {
		t.Fatalf("update workflow errors: %+v", res.Errors)
	}
	assertWorkflowCatalogRows(t, s, "daily_report", "Updated daily report", 1)

	res, err = s.gj.GraphQL(context.Background(), `mutation {
		gj_workflow(where: { name: { eq: "daily_report" } }, delete: true) {
			name
			deleted
		}
	}`, nil, &core.RequestConfig{})
	if err != nil {
		t.Fatalf("delete workflow: %v", err)
	}
	if len(res.Errors) != 0 {
		t.Fatalf("delete workflow errors: %+v", res.Errors)
	}
	assertWorkflowCatalogRows(t, s, "daily_report", "", 0)
}

func TestEnsureMetadataCatalogCardColumnsMigratesExistingTable(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "metadata.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE gj_catalog_cards (
		id TEXT PRIMARY KEY,
		kind TEXT NOT NULL,
		title TEXT NOT NULL,
		summary TEXT NOT NULL DEFAULT '',
		database_name TEXT NOT NULL DEFAULT '',
		schema_name TEXT NOT NULL DEFAULT '',
		table_name TEXT NOT NULL DEFAULT '',
		column_name TEXT NOT NULL DEFAULT '',
		source TEXT NOT NULL DEFAULT '',
		risk_level TEXT NOT NULL DEFAULT '',
		confidence TEXT NOT NULL DEFAULT '',
		sensitive BOOLEAN NOT NULL DEFAULT 0,
		sensitivity TEXT NOT NULL DEFAULT '',
		evidence_json TEXT NOT NULL DEFAULT '',
		examples_json TEXT NOT NULL DEFAULT '',
		suggested_next_json TEXT NOT NULL DEFAULT '',
		detail_ref TEXT NOT NULL DEFAULT ''
	)`); err != nil {
		t.Fatal(err)
	}
	if err := ensureMetadataCatalogCardColumns(context.Background(), db); err != nil {
		t.Fatalf("migrate catalog card columns: %v", err)
	}
	for _, col := range []string{"created_at", "updated_at"} {
		var found int
		if err := db.QueryRow(`SELECT count(*) FROM pragma_table_info('gj_catalog_cards') WHERE name = ?`, col).Scan(&found); err != nil {
			t.Fatalf("check migrated column %s: %v", col, err)
		}
		if found != 1 {
			t.Fatalf("expected migrated gj_catalog_cards.%s column", col)
		}
	}
}

func TestLegacyMetadataConfigRejected(t *testing.T) {
	enabled := true
	conf := &Config{
		Core: Core{
			DisableAllowList: true,
			Metadata: core.MetadataConfig{
				Enabled: &enabled,
			},
		},
		Serv: Serv{
			ConfigPath: filepath.Join(t.TempDir(), "config"),
			MCP:        MCPConfig{Disable: true},
		},
	}

	s, err := newGraphJinService(conf, nil)
	if err == nil {
		closeTestService(s)
		t.Fatal("expected legacy metadata config to be rejected")
	}
	if !strings.Contains(err.Error(), "kind: graphjin") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMetadataGraphDisabledInProductionByDefault(t *testing.T) {
	appPath := createMetadataAppDB(t)
	conf := &Config{
		Core: Core{
			DisableAllowList: true,
			Databases: map[string]core.DatabaseConfig{
				"app": {Type: "sqlite", Path: appPath},
			},
		},
		Serv: Serv{
			Production: true,
			ConfigPath: filepath.Join(t.TempDir(), "config"),
			MCP:        MCPConfig{Disable: true},
		},
	}

	s, err := newGraphJinService(conf, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestService(s)

	if s.metadataDB != "" {
		t.Fatalf("metadata database = %q, want disabled", s.metadataDB)
	}
	if _, ok := s.runtimeCore.Databases[defaultMetadataDBName]; ok {
		t.Fatalf("metadata runtime database should not be created in production by default")
	}
}

func TestMetadataDatabaseNameCollisionFailsStartup(t *testing.T) {
	appPath := createMetadataAppDB(t)
	conf := &Config{
		Core: Core{
			DisableAllowList: true,
			Sources: []core.SourceConfig{
				{Name: defaultMetadataDBName, Kind: "sql", Type: "sqlite", Path: appPath, Default: true},
				{Name: defaultMetadataDBName, Kind: "graphjin"},
			},
		},
		Serv: Serv{
			ConfigPath: filepath.Join(t.TempDir(), "config"),
			MCP:        MCPConfig{Disable: true},
		},
	}

	_, err := newGraphJinService(conf, nil)
	if err == nil || !strings.Contains(err.Error(), `duplicate source name "graphjin"`) {
		t.Fatalf("startup error = %v, want duplicate source collision", err)
	}
}

func createMetadataAppDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "app.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE users (
		id integer primary key,
		email text not null unique,
		team_id integer
	);
	CREATE INDEX idx_users_team_id ON users(team_id);`); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertWorkflowCatalogRows(t *testing.T, s *graphjinService, name, summary string, want int) {
	t.Helper()
	where := `kind: { eq: "workflow" }, id: { eq: ` + strconv.Quote("workflow:"+name) + ` }`
	if summary != "" {
		where += `, summary: { eq: ` + strconv.Quote(summary) + ` }`
	}
	query := `query { gj_catalog(where: { ` + where + ` }) { id summary } }`
	res, err := s.gj.GraphQL(context.Background(), query, nil, nil)
	if err != nil {
		t.Fatalf("query workflow catalog row: %v", err)
	}
	var out struct {
		GJCatalog []struct {
			ID      string `json:"id"`
			Summary string `json:"summary"`
		} `json:"gj_catalog"`
	}
	if err := json.Unmarshal(res.Data, &out); err != nil {
		t.Fatalf("decode workflow catalog response: %v\n%s", err, res.Data)
	}
	if len(out.GJCatalog) != want {
		t.Fatalf("workflow catalog rows = %d, want %d: %+v data=%s", len(out.GJCatalog), want, out.GJCatalog, res.Data)
	}
}

func hasRuntimeRelationship(s *graphjinService, database, table, column, target string) bool {
	if s.runtimeCore == nil {
		return false
	}
	for _, t := range s.runtimeCore.Tables {
		if t.Database != database || t.Name != table {
			continue
		}
		for _, c := range t.Columns {
			if c.Name == column && c.ForeignKey == target {
				return true
			}
		}
	}
	return false
}
