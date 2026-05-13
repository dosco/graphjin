package serv

import (
	"context"
	"database/sql"
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
`

func (s *graphjinService) initMetadataGraphBeforeCore() error {
	if !s.conf.Core.MetadataEnabled() {
		s.metadataDB = ""
		return nil
	}
	if s.runtimeCore == nil {
		runtimeCore := cloneCoreConfig(s.conf.Core)
		s.runtimeCore = &runtimeCore
	}
	s.ensureRuntimeDatabaseEntries()
	name, err := s.initMetadataGraphForRuntime(&s.conf.Core, s.runtimeCore, s.dbs, s.managedDBs)
	if err != nil {
		return err
	}
	s.metadataDB = name
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
	if gj == nil || conf == nil || !conf.MetadataEnabled() || metadataDB == "" {
		return nil
	}
	db := dbs[metadataDB]
	if db == nil {
		return nil
	}
	snapshot, err := gj.MetadataSnapshot(s.metadataSnapshotExcludesFor(metadataDB, conf, managedDBs)...)
	if err != nil {
		return err
	}
	if err := populateMetadataDB(context.Background(), db, snapshot); err != nil {
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

func populateMetadataDB(ctx context.Context, db *sql.DB, snapshot *core.MetadataSnapshot) error {
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
	return tx.Commit()
}

func (s *graphjinService) injectMetadataCodeRelationships(metadataDB, codeDB string) {
	injectMetadataCodeRelationships(s.runtimeCore, metadataDB, codeDB)
}

func injectMetadataCodeRelationships(runtimeCore *core.Config, metadataDB, codeDB string) {
	if runtimeCore == nil {
		return
	}
	add := func(table, column, target string) {
		for i := range runtimeCore.Tables {
			t := &runtimeCore.Tables[i]
			if t.Database == metadataDB && t.Schema == "main" && t.Name == table {
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
			Database: metadataDB,
			Schema:   "main",
			Name:     table,
			Columns:  []core.Column{{Name: column, ForeignKey: target}},
		})
	}
	add("gj_tables", "code_db_refs_id", codeDB+":code_db_refs.table_key")
	add("gj_columns", "code_db_refs_id", codeDB+":code_db_refs.column_key")
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
	if len(conf.Metadata.CodeDatabases) > 0 {
		out := make([]string, 0, len(conf.Metadata.CodeDatabases))
		for _, name := range conf.Metadata.CodeDatabases {
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
