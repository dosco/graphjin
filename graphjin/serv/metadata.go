package serv

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dosco/graphjin/codesql"
	"github.com/dosco/graphjin/core/v3"
)

const defaultMetadataDBName = "graphjin"

const metadataSchemaSQL = `
PRAGMA foreign_keys=ON;
PRAGMA busy_timeout=5000;

CREATE TABLE IF NOT EXISTS gj_databases (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  type TEXT NOT NULL,
  is_default BOOLEAN NOT NULL DEFAULT 0,
  read_only BOOLEAN NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS gj_tables (
  id TEXT PRIMARY KEY,
  database_name TEXT NOT NULL,
  schema_name TEXT NOT NULL,
  table_name TEXT NOT NULL,
  type TEXT NOT NULL DEFAULT '',
  comment TEXT NOT NULL DEFAULT '',
  primary_key TEXT NOT NULL DEFAULT '',
  column_count INTEGER NOT NULL DEFAULT 0,
  table_key TEXT NOT NULL,
  code_db_refs_id TEXT NOT NULL,
  FOREIGN KEY(database_name) REFERENCES gj_databases(name) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_gj_tables_name ON gj_tables(database_name, schema_name, table_name);
CREATE INDEX IF NOT EXISTS idx_gj_tables_code_refs ON gj_tables(code_db_refs_id);

CREATE TABLE IF NOT EXISTS gj_columns (
  id TEXT PRIMARY KEY,
  table_id TEXT NOT NULL REFERENCES gj_tables(id) ON DELETE CASCADE,
  database_name TEXT NOT NULL,
  schema_name TEXT NOT NULL,
  table_name TEXT NOT NULL,
  column_name TEXT NOT NULL,
  type TEXT NOT NULL DEFAULT '',
  array BOOLEAN NOT NULL DEFAULT 0,
  not_null BOOLEAN NOT NULL DEFAULT 0,
  primary_key BOOLEAN NOT NULL DEFAULT 0,
  unique_key BOOLEAN NOT NULL DEFAULT 0,
  indexed BOOLEAN NOT NULL DEFAULT 0,
  index_name TEXT NOT NULL DEFAULT '',
  default_value TEXT NOT NULL DEFAULT '',
  comment TEXT NOT NULL DEFAULT '',
  ordinal INTEGER NOT NULL DEFAULT 0,
  table_key TEXT NOT NULL,
  column_key TEXT NOT NULL,
  code_db_refs_id TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_gj_columns_name ON gj_columns(database_name, schema_name, table_name, column_name);
CREATE INDEX IF NOT EXISTS idx_gj_columns_code_refs ON gj_columns(code_db_refs_id);

CREATE TABLE IF NOT EXISTS gj_relationships (
  id TEXT PRIMARY KEY,
  from_database_name TEXT NOT NULL,
  from_schema_name TEXT NOT NULL,
  from_table_name TEXT NOT NULL,
  from_column_name TEXT NOT NULL,
  from_column_id TEXT NOT NULL,
  to_database_name TEXT NOT NULL,
  to_schema_name TEXT NOT NULL,
  to_table_name TEXT NOT NULL,
  to_column_name TEXT NOT NULL,
  to_column_id TEXT NOT NULL,
  rel_type TEXT NOT NULL,
  is_cross_database BOOLEAN NOT NULL DEFAULT 0,
  source TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS gj_functions (
  id TEXT PRIMARY KEY,
  database_name TEXT NOT NULL,
  schema_name TEXT NOT NULL,
  name TEXT NOT NULL,
  return_type TEXT NOT NULL DEFAULT '',
  aggregate BOOLEAN NOT NULL DEFAULT 0,
  comment TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS gj_indexes (
  id TEXT PRIMARY KEY,
  database_name TEXT NOT NULL,
  schema_name TEXT NOT NULL,
  table_name TEXT NOT NULL,
  column_name TEXT NOT NULL,
  name TEXT NOT NULL,
  unique_index BOOLEAN NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS gj_catalog_cards (
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
  detail_ref TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_gj_catalog_cards_kind ON gj_catalog_cards(kind);
CREATE INDEX IF NOT EXISTS idx_gj_catalog_cards_target ON gj_catalog_cards(database_name, schema_name, table_name, column_name);

CREATE TABLE IF NOT EXISTS gj_catalog_meta (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS gj_catalog_card_details (
  id TEXT PRIMARY KEY,
  card_id TEXT NOT NULL REFERENCES gj_catalog_cards(id) ON DELETE CASCADE,
  section TEXT NOT NULL,
  content TEXT NOT NULL DEFAULT '',
  data_json TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS gj_nodes (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  name TEXT NOT NULL,
  summary TEXT NOT NULL DEFAULT '',
  card_id TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS gj_edges (
  id TEXT PRIMARY KEY,
  from_id TEXT NOT NULL,
  to_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  summary TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS gj_entrypoints (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  summary TEXT NOT NULL DEFAULT '',
  query_json TEXT NOT NULL DEFAULT '',
  suggested_next_json TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS gj_capabilities (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  kind TEXT NOT NULL,
  summary TEXT NOT NULL DEFAULT '',
  input_schema_json TEXT NOT NULL DEFAULT '',
  output_schema_json TEXT NOT NULL DEFAULT '',
  safety_json TEXT NOT NULL DEFAULT ''
);
`

func (s *graphjinService) initMetadataGraphBeforeCore() error {
	if !s.conf.Core.MetadataEnabled() && !s.conf.catalogToolsEnabled() && !s.conf.graphjinControlPlaneEnabled() && !s.conf.workflowsSourceEnabled() && !s.conf.runtimeRootRegistered() {
		s.metadataDB = ""
		return nil
	}
	return s.initSystemNanoDBBeforeCore()
}

func (s *graphjinService) ensureSystemHostDBBeforeCore() error {
	if s == nil || s.conf == nil || len(s.dbs) != 0 {
		return nil
	}
	if !s.conf.needsSystemHostDB() {
		return nil
	}
	if s.runtimeCore == nil {
		runtimeCore := cloneCoreConfig(s.conf.Core)
		s.runtimeCore = &runtimeCore
	}

	dsn := fmt.Sprintf("file:graphjin_system_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS graphjin_system_anchor (id TEXT PRIMARY KEY)`); err != nil {
		db.Close() //nolint:errcheck
		return err
	}
	if s.dbs == nil {
		s.dbs = make(map[string]*sql.DB)
	}
	s.dbs[core.DefaultDBName] = db
	if s.runtimeCore.Databases == nil {
		s.runtimeCore.Databases = make(map[string]core.DatabaseConfig)
	}
	analyticsMode := true
	s.runtimeCore.Databases[core.DefaultDBName] = core.DatabaseConfig{
		Type:          "sqlite",
		ConnString:    dsn,
		Path:          dsn,
		ReadOnly:      true,
		AnalyticsMode: &analyticsMode,
	}
	return nil
}

func (s *graphjinService) initMetadataGraphForRuntime(conf *core.Config, runtimeCore *core.Config, dbs map[string]*sql.DB, managedDBs map[string]managedDB) (string, error) {
	if conf == nil || !conf.MetadataEnabled() {
		return "", nil
	}
	if runtimeCore == nil {
		return "", fmt.Errorf("metadata runtime config is nil")
	}
	name := conf.MetadataDatabaseName()
	if name == "" {
		name = defaultMetadataDBName
	}
	if _, ok := conf.Databases[name]; ok {
		return "", fmt.Errorf("metadata database %q collides with configured database", name)
	}
	if _, ok := dbs[name]; ok {
		return "", fmt.Errorf("metadata database %q collides with active database", name)
	}

	dsn := fmt.Sprintf("file:graphjin_metadata_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return "", err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.Exec(metadataSchemaSQL); err != nil {
		db.Close() //nolint:errcheck
		return "", fmt.Errorf("metadata schema: %w", err)
	}
	if err := ensureMetadataCatalogCardColumns(context.Background(), db); err != nil {
		db.Close() //nolint:errcheck
		return "", fmt.Errorf("metadata schema migration: %w", err)
	}

	dbs[name] = db
	if runtimeCore.Databases == nil {
		runtimeCore.Databases = make(map[string]core.DatabaseConfig)
	}
	analyticsMode := true
	runtimeCore.Databases[name] = core.DatabaseConfig{
		Type:          "sqlite",
		ConnString:    dsn,
		Path:          dsn,
		ReadOnly:      true,
		AnalyticsMode: &analyticsMode,
	}

	if conf.MetadataAutoCodeRelationsEnabled() {
		codeDBs := s.selectedCodeSQLDatabasesFor(conf, managedDBs)
		if len(codeDBs) == 1 {
			injectMetadataCodeRelationships(runtimeCore, name, codeDBs[0])
		} else if len(codeDBs) > 1 {
			s.log.Warnf("metadata auto_code_relations skipped: multiple CodeSQL databases selected: %s", strings.Join(codeDBs, ", "))
		}
	}
	return name, nil
}

func isMetadataStartupError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "metadata database")
}

func isNonRecoverableStartupError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return isMetadataStartupError(err) ||
		strings.Contains(msg, "reserved GraphJin system table prefix gj_") ||
		strings.Contains(msg, "reserved GraphJin system table")
}

func ensureMetadataCatalogCardColumns(ctx context.Context, db *sql.DB) error {
	cols := make(map[string]struct{})
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(gj_catalog_cards)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		cols[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, col := range []string{"created_at", "updated_at"} {
		if _, ok := cols[col]; ok {
			continue
		}
		if _, err := db.ExecContext(ctx, "ALTER TABLE gj_catalog_cards ADD COLUMN "+col+" TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
	}
	return nil
}

func (s *graphjinService) ensureRuntimeDatabaseEntries() {
	if s.runtimeCore.Databases == nil {
		s.runtimeCore.Databases = make(map[string]core.DatabaseConfig)
	}
	for name := range s.dbs {
		if _, ok := s.runtimeCore.Databases[name]; ok {
			continue
		}
		if conf, ok := s.conf.Core.Databases[name]; ok {
			s.runtimeCore.Databases[name] = conf
			continue
		}
		if name == core.DefaultDBName {
			dbType := s.conf.DBType
			if dbType == "" {
				dbType = s.conf.DB.Type
			}
			if dbType == "" {
				dbType = "postgres"
			}
			s.runtimeCore.Databases[name] = core.DatabaseConfig{
				Type:            dbType,
				ConnString:      s.conf.DB.ConnString,
				Host:            s.conf.DB.Host,
				Port:            int(s.conf.DB.Port),
				DBName:          s.conf.DB.DBName,
				User:            s.conf.DB.User,
				Password:        s.conf.DB.Password,
				Schema:          s.conf.DB.Schema,
				Path:            s.conf.DB.Path,
				PingTimeout:     s.conf.DB.PingTimeout,
				PoolSize:        s.conf.DB.PoolSize,
				MaxConnections:  s.conf.DB.MaxConnections,
				MaxConnIdleTime: s.conf.DB.MaxConnIdleTime,
				MaxConnLifeTime: s.conf.DB.MaxConnLifeTime,
			}
		}
	}
}

func (s *graphjinService) refreshMetadataGraph() error {
	return s.refreshMetadataGraphForRuntime(s.gj, &s.conf.Core, s.metadataDB, s.dbs, s.managedDBs)
}

func (s *graphjinService) refreshMetadataGraphForRuntime(gj *core.GraphJin, conf *core.Config, metadataDB string, dbs map[string]*sql.DB, managedDBs map[string]managedDB) error {
	if s.systemNanoDB != nil {
		return s.refreshMetadataGraphForRuntimeNano(context.Background(), gj, conf, metadataDB)
	}
	if gj == nil || conf == nil || !conf.MetadataEnabled() || metadataDB == "" {
		return nil
	}
	db := dbs[metadataDB]
	if db == nil {
		return nil
	}
	excludes := s.metadataSnapshotExcludesFor(metadataDB, conf, managedDBs)
	snapshot, err := gj.MetadataSnapshot(excludes...)
	if err != nil {
		return err
	}
	catalogSnapshot := core.BuildCatalogSnapshotWithOptions(snapshot, conf, s.catalogBuildOptions())
	if err := populateMetadataDB(context.Background(), db, snapshot, catalogSnapshot); err != nil {
		return err
	}

	codeDBs := s.selectedCodeSQLDatabasesFor(conf, managedDBs)
	if len(codeDBs) != 1 {
		return nil
	}
	targets := codeSQLTargetsFromMetadata(snapshot, nil)
	if managed, ok := managedDBs[codeDBs[0]]; ok && managed.handle != nil {
		if err := managed.handle.SetDBRefTargets(context.Background(), targets); err != nil {
			return err
		}
	}
	return nil
}

type metadataExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

var metadataCatalogTables = []string{
	"gj_capabilities",
	"gj_entrypoints",
	"gj_edges",
	"gj_nodes",
	"gj_catalog_card_details",
	"gj_catalog_meta",
	"gj_catalog_cards",
}

func (s *graphjinService) refreshMetadataCatalogMirror() error {
	if s == nil || s.gj == nil || s.conf == nil || !s.conf.Core.MetadataEnabled() || s.metadataDB == "" {
		return nil
	}
	db := s.dbs[s.metadataDB]
	if db == nil {
		return nil
	}
	snapshot, err := s.gj.MetadataSnapshot(s.metadataSnapshotExcludesFor(s.metadataDB, &s.conf.Core, s.managedDBs)...)
	if err != nil {
		return err
	}
	catalogSnapshot := core.BuildCatalogSnapshotWithOptions(snapshot, &s.conf.Core, s.catalogBuildOptions())
	return populateMetadataCatalogDB(context.Background(), db, catalogSnapshot)
}

func populateMetadataDB(ctx context.Context, db *sql.DB, snapshot *core.MetadataSnapshot, catalogSnapshots ...*core.CatalogSnapshot) error {
	if snapshot == nil {
		snapshot = &core.MetadataSnapshot{}
	}
	if err := ensureMetadataCatalogCardColumns(ctx, db); err != nil {
		return err
	}
	dedupeMetadataSnapshotForInsert(snapshot)
	var catalogSnapshot *core.CatalogSnapshot
	if len(catalogSnapshots) != 0 {
		catalogSnapshot = catalogSnapshots[0]
	}
	if catalogSnapshot == nil {
		catalogSnapshot = core.BuildCatalogSnapshot(snapshot, nil)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for _, table := range []string{"gj_indexes", "gj_functions", "gj_relationships", "gj_columns", "gj_tables", "gj_databases"} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table); err != nil {
			tx.Rollback() //nolint:errcheck
			return err
		}
	}
	for _, d := range snapshot.Databases {
		if _, err := tx.ExecContext(ctx, `INSERT INTO gj_databases(id, name, type, is_default, read_only)
		  VALUES (?, ?, ?, ?, ?)`, d.ID, d.Name, d.Type, d.IsDefault, d.ReadOnly); err != nil {
			tx.Rollback() //nolint:errcheck
			return err
		}
	}
	for _, t := range snapshot.Tables {
		if _, err := tx.ExecContext(ctx, `INSERT INTO gj_tables(id, database_name, schema_name, table_name, type, comment, primary_key, column_count, table_key, code_db_refs_id)
		  VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			t.ID, t.DatabaseName, t.SchemaName, t.TableName, t.Type, t.Comment, t.PrimaryKey, t.ColumnCount, t.TableKey, t.TableKey); err != nil {
			tx.Rollback() //nolint:errcheck
			return err
		}
	}
	for _, c := range snapshot.Columns {
		if _, err := tx.ExecContext(ctx, `INSERT INTO gj_columns(id, table_id, database_name, schema_name, table_name, column_name, type, array, not_null,
		  primary_key, unique_key, indexed, index_name, default_value, comment, ordinal, table_key, column_key, code_db_refs_id)
		  VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			c.ID, c.TableID, c.DatabaseName, c.SchemaName, c.TableName, c.ColumnName, c.Type, c.Array, c.NotNull,
			c.PrimaryKey, c.UniqueKey, c.Indexed, c.IndexName, c.DefaultValue, c.Comment, c.Ordinal, c.TableKey, c.ColumnKey, c.ColumnKey); err != nil {
			tx.Rollback() //nolint:errcheck
			return err
		}
	}
	for _, r := range snapshot.Relationships {
		if _, err := tx.ExecContext(ctx, `INSERT INTO gj_relationships(id, from_database_name, from_schema_name, from_table_name, from_column_name,
		  from_column_id, to_database_name, to_schema_name, to_table_name, to_column_name, to_column_id, rel_type, is_cross_database, source)
		  VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			r.ID, r.FromDatabaseName, r.FromSchemaName, r.FromTableName, r.FromColumnName, r.FromColumnID,
			r.ToDatabaseName, r.ToSchemaName, r.ToTableName, r.ToColumnName, r.ToColumnID, r.RelType, r.IsCrossDatabase, r.Source); err != nil {
			tx.Rollback() //nolint:errcheck
			return err
		}
	}
	for _, f := range snapshot.Functions {
		if _, err := tx.ExecContext(ctx, `INSERT INTO gj_functions(id, database_name, schema_name, name, return_type, aggregate, comment)
		  VALUES (?, ?, ?, ?, ?, ?, ?)`, f.ID, f.DatabaseName, f.SchemaName, f.Name, f.ReturnType, f.Aggregate, f.Comment); err != nil {
			tx.Rollback() //nolint:errcheck
			return err
		}
	}
	for _, idx := range snapshot.Indexes {
		if _, err := tx.ExecContext(ctx, `INSERT INTO gj_indexes(id, database_name, schema_name, table_name, column_name, name, unique_index)
		  VALUES (?, ?, ?, ?, ?, ?, ?)`, idx.ID, idx.DatabaseName, idx.SchemaName, idx.TableName, idx.ColumnName, idx.Name, idx.Unique); err != nil {
			tx.Rollback() //nolint:errcheck
			return err
		}
	}
	if err := replaceMetadataCatalogTables(ctx, tx, catalogSnapshot); err != nil {
		tx.Rollback() //nolint:errcheck
		return err
	}
	return tx.Commit()
}

func populateMetadataCatalogDB(ctx context.Context, db *sql.DB, catalogSnapshot *core.CatalogSnapshot) error {
	if catalogSnapshot == nil {
		catalogSnapshot = core.BuildCatalogSnapshot(&core.MetadataSnapshot{}, nil)
	}
	if err := ensureMetadataCatalogCardColumns(ctx, db); err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := replaceMetadataCatalogTables(ctx, tx, catalogSnapshot); err != nil {
		tx.Rollback() //nolint:errcheck
		return err
	}
	return tx.Commit()
}

func replaceMetadataCatalogTables(ctx context.Context, exec metadataExecer, catalogSnapshot *core.CatalogSnapshot) error {
	for _, table := range metadataCatalogTables {
		if _, err := exec.ExecContext(ctx, "DELETE FROM "+table); err != nil {
			return err
		}
	}
	for _, card := range catalogSnapshot.Cards {
		if _, err := exec.ExecContext(ctx, `INSERT INTO gj_catalog_cards(id, kind, title, summary, database_name, schema_name, table_name, column_name,
		  source, risk_level, confidence, sensitive, sensitivity, evidence_json, examples_json, suggested_next_json, detail_ref, created_at, updated_at)
		  VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			card.ID, card.Kind, card.Title, card.Summary, card.DatabaseName, card.SchemaName, card.TableName, card.ColumnName,
			card.Source, card.RiskLevel, card.Confidence, card.Sensitive, card.Sensitivity, card.EvidenceJSON, card.ExamplesJSON, card.SuggestedNext, card.DetailRef,
			card.CreatedAt, card.UpdatedAt); err != nil {
			return err
		}
	}
	if _, err := exec.ExecContext(ctx, `INSERT INTO gj_catalog_meta(key, value) VALUES (?, ?)`, "revision", catalogSnapshot.Revision); err != nil {
		return err
	}
	if _, err := exec.ExecContext(ctx, `INSERT INTO gj_catalog_meta(key, value) VALUES (?, ?)`, "source_revisions", mustMarshalString(catalogSnapshot.SourceRevisions)); err != nil {
		return err
	}
	for _, detail := range catalogSnapshot.Details {
		if _, err := exec.ExecContext(ctx, `INSERT INTO gj_catalog_card_details(id, card_id, section, content, data_json)
		  VALUES (?, ?, ?, ?, ?)`, detail.ID, detail.CardID, detail.Section, detail.Content, detail.DataJSON); err != nil {
			return err
		}
	}
	for _, node := range catalogSnapshot.Nodes {
		if _, err := exec.ExecContext(ctx, `INSERT INTO gj_nodes(id, kind, name, summary, card_id)
		  VALUES (?, ?, ?, ?, ?)`, node.ID, node.Kind, node.Name, node.Summary, node.CardID); err != nil {
			return err
		}
	}
	for _, edge := range catalogSnapshot.Edges {
		if _, err := exec.ExecContext(ctx, `INSERT INTO gj_edges(id, from_id, to_id, kind, summary)
		  VALUES (?, ?, ?, ?, ?)`, edge.ID, edge.FromID, edge.ToID, edge.Kind, edge.Summary); err != nil {
			return err
		}
	}
	for _, ep := range catalogSnapshot.EntryPoints {
		if _, err := exec.ExecContext(ctx, `INSERT INTO gj_entrypoints(id, name, summary, query_json, suggested_next_json)
		  VALUES (?, ?, ?, ?, ?)`, ep.ID, ep.Name, ep.Summary, ep.QueryJSON, ep.SuggestedNext); err != nil {
			return err
		}
	}
	for _, cap := range catalogSnapshot.Capabilities {
		if _, err := exec.ExecContext(ctx, `INSERT INTO gj_capabilities(id, name, kind, summary, input_schema_json, output_schema_json, safety_json)
		  VALUES (?, ?, ?, ?, ?, ?, ?)`, cap.ID, cap.Name, cap.Kind, cap.Summary, cap.InputSchemaJSON, cap.OutputSchemaJSON, cap.SafetyJSON); err != nil {
			return err
		}
	}
	return nil
}

func dedupeMetadataSnapshotForInsert(snapshot *core.MetadataSnapshot) {
	if snapshot == nil {
		return
	}
	snapshot.Databases = uniqueMetadataRows(snapshot.Databases, func(v core.MetadataDatabase) string { return v.ID })
	snapshot.Tables = uniqueMetadataRows(snapshot.Tables, func(v core.MetadataTable) string { return v.ID })
	snapshot.Columns = uniqueMetadataRows(snapshot.Columns, func(v core.MetadataColumn) string { return v.ID })
	snapshot.Relationships = uniqueMetadataRows(snapshot.Relationships, func(v core.MetadataRelationship) string { return v.ID })
	snapshot.Functions = uniqueMetadataRows(snapshot.Functions, func(v core.MetadataFunction) string { return v.ID })
	snapshot.Indexes = uniqueMetadataRows(snapshot.Indexes, func(v core.MetadataIndex) string { return v.ID })
}

func uniqueMetadataRows[T any](items []T, id func(T) string) []T {
	if len(items) < 2 {
		return items
	}
	seen := make(map[string]struct{}, len(items))
	out := items[:0]
	for _, item := range items {
		key := id(item)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func mustMarshalString(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func (s *graphjinService) injectMetadataCodeRelationships(metadataDB, codeDB string) {
	injectMetadataCodeRelationships(s.runtimeCore, metadataDB, codeDB)
}

func injectMetadataCodeRelationships(runtimeCore *core.Config, metadataDB, codeDB string) {
	if runtimeCore == nil {
		return
	}
	add := func(database, table, column, target string) {
		for i := range runtimeCore.Tables {
			t := &runtimeCore.Tables[i]
			if t.Database == database && t.Schema == "main" && t.Name == table {
				if t.Source == "" {
					t.Source = database
				}
				for _, c := range t.Columns {
					if c.Name == column && c.ForeignKey == target {
						return
					}
				}
				t.Columns = append(t.Columns, core.Column{Name: column, ForeignKey: target})
				return
			}
		}
		runtimeCore.Tables = append(runtimeCore.Tables, core.Table{
			Database: database,
			Source:   database,
			Schema:   "main",
			Name:     table,
			Columns:  []core.Column{{Name: column, ForeignKey: target}},
		})
	}
	add(metadataDB, "gj_catalog", "code_refs_id", codeDB+":gj_code.db_object_id")
	add(codeDB, "gj_code", "catalog_item_id", metadataDB+":gj_catalog.id")
	add(codeDB, "gj_code", "table_catalog_item_id", metadataDB+":gj_catalog.id")
	add(codeDB, "gj_code", "column_catalog_item_id", metadataDB+":gj_catalog.id")
}

func (s *graphjinService) metadataSnapshotExcludes() []string {
	return s.metadataSnapshotExcludesFor(s.metadataDB, &s.conf.Core, s.managedDBs)
}

func (s *graphjinService) metadataSnapshotExcludesFor(metadataDB string, conf *core.Config, managedDBs map[string]managedDB) []string {
	seen := make(map[string]struct{})
	if metadataDB != "" {
		seen[metadataDB] = struct{}{}
	}
	for _, name := range s.allCodeSQLDatabasesFor(conf, managedDBs) {
		seen[name] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func (s *graphjinService) selectedCodeSQLDatabases() []string {
	return s.selectedCodeSQLDatabasesFor(&s.conf.Core, s.managedDBs)
}

func (s *graphjinService) selectedCodeSQLDatabasesFor(conf *core.Config, managedDBs map[string]managedDB) []string {
	if conf == nil {
		return nil
	}
	codeDatabases := conf.CatalogCodeDatabases()
	if len(codeDatabases) > 0 {
		out := make([]string, 0, len(codeDatabases))
		for _, name := range codeDatabases {
			if s.isConfiguredCodeSQLDatabaseFor(name, conf, managedDBs) {
				out = append(out, name)
				continue
			}
			s.log.Warnf("metadata code_databases ignored non-CodeSQL database: %s", name)
		}
		sort.Strings(out)
		return out
	}
	return s.allCodeSQLDatabasesFor(conf, managedDBs)
}

func (s *graphjinService) allCodeSQLDatabases() []string {
	return s.allCodeSQLDatabasesFor(&s.conf.Core, s.managedDBs)
}

func (s *graphjinService) allCodeSQLDatabasesFor(conf *core.Config, managedDBs map[string]managedDB) []string {
	seen := make(map[string]struct{})
	for name := range managedDBs {
		seen[name] = struct{}{}
	}
	if conf != nil {
		for name, dbConf := range conf.Databases {
			if isCodeSQLType(dbConf.Type) {
				seen[name] = struct{}{}
			}
		}
		if isCodeSQLType(conf.DBType) {
			seen[core.DefaultDBName] = struct{}{}
		}
	}
	if conf == nil {
		return nil
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func (s *graphjinService) isConfiguredCodeSQLDatabase(name string) bool {
	return s.isConfiguredCodeSQLDatabaseFor(name, &s.conf.Core, s.managedDBs)
}

func (s *graphjinService) isConfiguredCodeSQLDatabaseFor(name string, conf *core.Config, managedDBs map[string]managedDB) bool {
	if _, ok := managedDBs[name]; ok {
		return true
	}
	if conf == nil {
		return false
	}
	if dbConf, ok := conf.Databases[name]; ok {
		if isCodeSQLType(dbConf.Type) {
			return true
		}
	}
	if name == core.DefaultDBName {
		return isCodeSQLType(conf.DBType)
	}
	return false
}

func codeSQLTargetsFromMetadata(snapshot *core.MetadataSnapshot, exclude map[string]struct{}) []codesql.DBRefTarget {
	if snapshot == nil {
		return nil
	}
	tableColumns := make(map[string][]string)
	tableSeen := make(map[string]core.MetadataTable)
	for _, t := range snapshot.Tables {
		if _, ok := exclude[t.DatabaseName]; ok {
			continue
		}
		tableSeen[t.ID] = t
	}
	for _, c := range snapshot.Columns {
		if _, ok := tableSeen[c.TableID]; !ok {
			continue
		}
		tableColumns[c.TableID] = append(tableColumns[c.TableID], c.ColumnName)
	}
	targets := make([]codesql.DBRefTarget, 0, len(tableSeen))
	for id, t := range tableSeen {
		targets = append(targets, codesql.DBRefTarget{
			DatabaseName: t.DatabaseName,
			SchemaName:   t.SchemaName,
			TableName:    t.TableName,
			Columns:      tableColumns[id],
		})
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].DatabaseName != targets[j].DatabaseName {
			return targets[i].DatabaseName < targets[j].DatabaseName
		}
		if targets[i].SchemaName != targets[j].SchemaName {
			return targets[i].SchemaName < targets[j].SchemaName
		}
		return targets[i].TableName < targets[j].TableName
	})
	return targets
}
