package serv

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dosco/graphjin/core/v3"
)

func (s *graphjinService) initSystemNanoDBBeforeCore() error {
	if s == nil || s.conf == nil {
		return nil
	}
	if !(s.conf.catalogToolsEnabled() || s.conf.graphjinControlPlaneEnabled() || s.conf.workflowsSourceEnabled()) {
		s.metadataDB = ""
		return nil
	}
	if s.runtimeCore == nil {
		runtimeCore := cloneCoreConfig(s.conf.Core)
		s.runtimeCore = &runtimeCore
	}
	s.ensureRuntimeDatabaseEntries()
	name := s.conf.Core.CatalogDatabaseName()
	if name == "" {
		name = defaultMetadataDBName
	}
	s.metadataDB = name
	if s.systemNanoDB == nil {
		db, err := core.NewNanoDB(systemNanoSnapshot(name, "", nil))
		if err != nil {
			return err
		}
		s.systemNanoDB = db
	}
	if s.runtimeCore.Databases == nil {
		s.runtimeCore.Databases = make(map[string]core.DatabaseConfig)
	}
	s.runtimeCore.Databases[name] = core.DatabaseConfig{Type: "nanodb", ReadOnly: true}
	s.injectSystemNanoTables(name)
	applySystemRoleQueryDefaults(s.conf, s.runtimeCore, name)
	codeDB := ""
	if s.conf.Core.MetadataAutoCodeRelationsEnabled() {
		codeDBs := s.selectedCodeSQLDatabasesFor(&s.conf.Core, s.managedDBs)
		if len(codeDBs) == 1 {
			codeDB = codeDBs[0]
			injectMetadataCodeRelationships(s.runtimeCore, name, codeDB)
		} else if len(codeDBs) > 1 {
			s.log.Warnf("metadata auto_code_relations skipped: multiple CodeSQL databases selected: %s", strings.Join(codeDBs, ", "))
		}
	}
	if codeDB != "" {
		if err := s.systemNanoDB.Refresh(systemNanoSnapshot(name, codeDB, nil)); err != nil {
			return err
		}
	}
	return nil
}

func (s *graphjinService) refreshSystemNanoDB() error {
	if s == nil || s.systemNanoDB == nil || s.metadataDB == "" {
		return nil
	}
	var snapshot *core.MetadataSnapshot
	if s.gj != nil {
		md, err := s.gj.MetadataSnapshot(s.metadataSnapshotExcludesFor(s.metadataDB, &s.conf.Core, s.managedDBs)...)
		if err != nil {
			return err
		}
		snapshot = md
	} else {
		snapshot = &core.MetadataSnapshot{}
	}
	catalogSnapshot := core.BuildCatalogSnapshotWithOptions(snapshot, &s.conf.Core, s.catalogBuildOptions())
	return s.systemNanoDB.Refresh(s.systemNanoSnapshotFromCatalog(catalogSnapshot))
}

func (s *graphjinService) systemNanoSnapshotFromCatalog(catalogSnapshot *core.CatalogSnapshot) core.NanoSnapshot {
	codeDB := ""
	codeDBs := s.selectedCodeSQLDatabasesFor(&s.conf.Core, s.managedDBs)
	if len(codeDBs) == 1 {
		codeDB = codeDBs[0]
	}
	var conf *core.Config
	if s.conf != nil {
		conf = &s.conf.Core
	}
	return systemNanoSnapshot(s.metadataDB, codeDB, systemNanoRows(s, catalogSnapshot, conf))
}

func systemNanoSnapshot(_ string, codeDB string, rows map[string][]core.NanoRow) core.NanoSnapshot {
	tables := []core.NanoTable{
		{Name: "gj_catalog", Columns: catalogNanoColumns(codeDB), Rows: rowsFor(rows, "gj_catalog")},
		{Name: "gj_security", Columns: securityNanoColumns(), Rows: rowsFor(rows, "gj_security")},
		{Name: "gj_workflow", Columns: workflowNanoColumns(), Rows: rowsFor(rows, "gj_workflow")},
		{Name: "gj_workflow_execution", Columns: workflowExecutionNanoColumns(), Rows: rowsFor(rows, "gj_workflow_execution")},
		{Name: "gj_config", Columns: configNanoColumns(), Rows: rowsFor(rows, "gj_config")},
	}
	return core.NanoSnapshot{Schema: "main", Tables: tables}
}

func rowsFor(rows map[string][]core.NanoRow, table string) []core.NanoRow {
	if rows == nil {
		return nil
	}
	return rows[table]
}

func catalogNanoColumns(codeDB string) []core.NanoColumn {
	cols := []core.NanoColumn{
		{Name: "id", Type: "text", PrimaryKey: true, NotNull: true},
		{Name: "parent_id", Type: "text", Index: true, FKeyTable: "gj_catalog", FKeyColumn: "id", FKeyUnique: true},
		{Name: "object_key", Type: "text", Index: true},
		{Name: "table_key", Type: "text", Index: true},
		{Name: "column_key", Type: "text", Index: true},
		{Name: "code_refs_id", Type: "text", Index: true},
		{Name: "workflow_id", Type: "text", Index: true, FKeyTable: "gj_workflow", FKeyColumn: "name", FKeyUnique: true},
		{Name: "kind", Type: "text", Index: true},
		{Name: "name", Type: "text"},
		{Name: "title", Type: "text"},
		{Name: "type", Type: "text"},
		{Name: "summary", Type: "text"},
		{Name: "database_name", Type: "text", Index: true},
		{Name: "schema_name", Type: "text", Index: true},
		{Name: "table_name", Type: "text", Index: true},
		{Name: "column_name", Type: "text", Index: true},
		{Name: "source", Type: "text"},
		{Name: "source_kind", Type: "text", Index: true},
		{Name: "risk_level", Type: "text"},
		{Name: "confidence", Type: "text"},
		{Name: "sensitive", Type: "boolean"},
		{Name: "sensitivity", Type: "text"},
		{Name: "query_json", Type: "json"},
		{Name: "input_schema_json", Type: "json"},
		{Name: "output_schema_json", Type: "json"},
		{Name: "safety_json", Type: "json"},
		{Name: "enabled", Type: "boolean"},
		{Name: "capability_kind", Type: "text"},
		{Name: "evidence_json", Type: "json"},
		{Name: "examples_json", Type: "json"},
		{Name: "suggested_next_json", Type: "json"},
		{Name: "detail_ref", Type: "text"},
		{Name: "details_json", Type: "json"},
		{Name: "edges_json", Type: "json"},
		{Name: "graphql_query", Type: "text"},
		{Name: "graphql_mutation", Type: "text"},
		{Name: "created_at", Type: "text"},
		{Name: "updated_at", Type: "text"},
		{Name: "score", Type: "float"},
		{Name: "search_rank", Type: "float"},
		{Name: "search_vector", Type: "text", FullText: true},
	}
	if codeDB != "" {
		for i := range cols {
			if cols[i].Name == "code_refs_id" {
				cols[i].FKeyDatabase = codeDB
				cols[i].FKeySchema = "main"
				cols[i].FKeyTable = "gj_code"
				cols[i].FKeyColumn = "db_object_id"
			}
		}
	}
	return cols
}

func securityNanoColumns() []core.NanoColumn {
	return []core.NanoColumn{
		{Name: "id", Type: "text", PrimaryKey: true, NotNull: true},
		{Name: "kind", Type: "text", Index: true},
		{Name: "report", Type: "text", Index: true},
		{Name: "scope", Type: "text", Index: true},
		{Name: "config_id", Type: "text", Index: true},
		{Name: "config_name", Type: "text", Index: true},
		{Name: "config_file", Type: "text", Index: true},
		{Name: "config_path", Type: "text"},
		{Name: "config_inherits", Type: "text", Index: true},
		{Name: "config_active", Type: "boolean", Index: true},
		{Name: "mode", Type: "text", Index: true},
		{Name: "audience", Type: "text", Index: true},
		{Name: "layer", Type: "text", Index: true},
		{Name: "surface", Type: "text", Index: true},
		{Name: "transport", Type: "text", Index: true},
		{Name: "database_name", Type: "text", Index: true},
		{Name: "source", Type: "text", Index: true},
		{Name: "source_kind", Type: "text", Index: true},
		{Name: "table_name", Type: "text", Index: true},
		{Name: "column_name", Type: "text", Index: true},
		{Name: "role", Type: "text", Index: true},
		{Name: "capability", Type: "text", Index: true},
		{Name: "action", Type: "text", Index: true},
		{Name: "title", Type: "text"},
		{Name: "summary", Type: "text"},
		{Name: "effective", Type: "text", Index: true},
		{Name: "default_effective", Type: "text", Index: true},
		{Name: "effective_allowed", Type: "boolean", Index: true},
		{Name: "default_allowed", Type: "boolean", Index: true},
		{Name: "override_key", Type: "text", Index: true},
		{Name: "override_value", Type: "text"},
		{Name: "override_explicit", Type: "boolean", Index: true},
		{Name: "override_source", Type: "text", Index: true},
		{Name: "weakens_default", Type: "boolean", Index: true},
		{Name: "read_only", Type: "boolean", Index: true},
		{Name: "severity", Type: "text", Index: true},
		{Name: "severity_rank", Type: "integer", Index: true},
		{Name: "confidence", Type: "text", Index: true},
		{Name: "status", Type: "text", Index: true},
		{Name: "reason", Type: "text"},
		{Name: "recommendation", Type: "text"},
		{Name: "summary_json", Type: "json"},
		{Name: "evidence_json", Type: "json"},
		{Name: "examples_json", Type: "json"},
		{Name: "details_json", Type: "json"},
		{Name: "safety_json", Type: "json"},
		{Name: "created_at", Type: "text"},
		{Name: "updated_at", Type: "text"},
		{Name: "search_rank", Type: "float"},
		{Name: "search_vector", Type: "text", FullText: true},
	}
}

func workflowNanoColumns() []core.NanoColumn {
	return []core.NanoColumn{
		{Name: "name", Type: "text", PrimaryKey: true, NotNull: true},
		{Name: "catalog_item_id", Type: "text", Index: true, FKeyTable: "gj_catalog", FKeyColumn: "id", FKeyUnique: true},
		{Name: "description", Type: "text"},
		{Name: "tags", Type: "json"},
		{Name: "tags_json", Type: "json"},
		{Name: "variables", Type: "json"},
		{Name: "variables_json", Type: "json"},
		{Name: "code", Type: "text"},
		{Name: "path", Type: "text"},
		{Name: "source_hash", Type: "text"},
		{Name: "runtime", Type: "text"},
		{Name: "timeout_seconds", Type: "integer"},
		{Name: "created_at", Type: "text"},
		{Name: "updated_at", Type: "text"},
		{Name: "workflow_revision", Type: "text"},
		{Name: "catalog_revision", Type: "text"},
		{Name: "deleted", Type: "boolean"},
		{Name: "search_vector", Type: "text", FullText: true},
	}
}

func workflowExecutionNanoColumns() []core.NanoColumn {
	return []core.NanoColumn{
		{Name: "id", Type: "text", PrimaryKey: true, NotNull: true},
		{Name: "workflow_name", Type: "text", Index: true, FKeyTable: "gj_workflow", FKeyColumn: "name", FKeyUnique: true},
		{Name: "namespace", Type: "text"},
		{Name: "variables", Type: "json"},
		{Name: "status", Type: "text"},
		{Name: "result_json", Type: "json"},
		{Name: "error", Type: "text"},
		{Name: "duration_ms", Type: "integer"},
	}
}

func configNanoColumns() []core.NanoColumn {
	return []core.NanoColumn{
		{Name: "id", Type: "text", PrimaryKey: true, NotNull: true},
		{Name: "sources_used", Type: "boolean"},
		{Name: "config_path", Type: "text"},
		{Name: "active_database", Type: "text"},
		{Name: "sources", Type: "json"},
		{Name: "databases", Type: "json"},
		{Name: "relationships", Type: "json"},
		{Name: "tables", Type: "json"},
		{Name: "roles", Type: "json"},
		{Name: "blocklist", Type: "json"},
		{Name: "functions", Type: "json"},
		{Name: "resolvers", Type: "json"},
		{Name: "mcp", Type: "json"},
		{Name: "config_json", Type: "json"},
		{Name: "redacted_paths", Type: "json"},
		{Name: "updated_at", Type: "text"},
		{Name: "catalog_revision", Type: "text"},
	}
}

func systemNanoRows(s *graphjinService, catalogSnapshot *core.CatalogSnapshot, conf *core.Config) map[string][]core.NanoRow {
	rows := map[string][]core.NanoRow{}
	cp := newControlPlaneGraphQL(s)
	if catalogSnapshot != nil {
		for _, row := range cp.allCatalogRows(catalogSnapshot, core.CatalogQueryOutput{Cards: catalogSnapshot.Cards}) {
			rows["gj_catalog"] = append(rows["gj_catalog"], normalizeCatalogNanoRow(row))
		}
	}
	rows["gj_security"] = append(rows["gj_security"], securityNanoRows(s)...)
	for _, row := range cp.workflowRows(true) {
		rows["gj_workflow"] = append(rows["gj_workflow"], normalizeWorkflowNanoRow(row))
	}
	if s != nil && s.conf != nil && s.conf.graphjinControlPlaneEnabled() {
		rows["gj_config"] = []core.NanoRow{copyNanoRow(cp.configRow())}
	}
	_ = conf
	return rows
}

func normalizeCatalogNanoRow(in map[string]any) core.NanoRow {
	row := copyNanoRow(in)
	id := fmt.Sprint(row["id"])
	kind := fmt.Sprint(row["kind"])
	row["object_key"] = id
	row["table_key"] = tableKeyFromRow(row)
	row["column_key"] = columnKeyFromRow(row)
	row["code_refs_id"] = codeRefsIDFromRow(row)
	if kind == "column" {
		row["parent_id"] = "table:" + tableKeyFromRow(row)
	}
	if kind == "table" {
		row["parent_id"] = "database:" + fmt.Sprint(row["database_name"])
	}
	if kind == "workflow" {
		name := strings.TrimPrefix(id, "workflow:")
		row["workflow_id"] = name
	}
	if _, ok := row["search_rank"]; !ok {
		row["search_rank"] = row["score"]
	}
	row["search_vector"] = strings.Join([]string{
		fmt.Sprint(row["kind"]),
		fmt.Sprint(row["name"]),
		fmt.Sprint(row["title"]),
		fmt.Sprint(row["summary"]),
		fmt.Sprint(row["database_name"]),
		fmt.Sprint(row["table_name"]),
		fmt.Sprint(row["column_name"]),
	}, " ")
	return row
}

func normalizeWorkflowNanoRow(in map[string]any) core.NanoRow {
	row := copyNanoRow(in)
	name := fmt.Sprint(row["name"])
	row["catalog_item_id"] = "workflow:" + name
	row["search_vector"] = strings.Join([]string{name, fmt.Sprint(row["description"]), fmt.Sprint(row["tags_json"])}, " ")
	return row
}

func copyNanoRow(in map[string]any) core.NanoRow {
	out := make(core.NanoRow, len(in)+4)
	for k, v := range in {
		out[k] = v
	}
	return out
}

func tableKeyFromRow(row map[string]any) string {
	database := fmt.Sprint(row["database_name"])
	parts := nonEmptyStrings(row["schema_name"], row["table_name"])
	if database == "" {
		return strings.Join(parts, ".")
	}
	if len(parts) == 0 {
		return database
	}
	return database + ":" + strings.Join(parts, ".")
}

func columnKeyFromRow(row map[string]any) string {
	tableKey := tableKeyFromRow(row)
	column := fmt.Sprint(row["column_name"])
	if tableKey == "" {
		return column
	}
	if column == "" {
		return tableKey
	}
	return tableKey + "." + column
}

func codeRefsIDFromRow(row map[string]any) string {
	if fmt.Sprint(row["column_name"]) != "" {
		return columnKeyFromRow(row)
	}
	return tableKeyFromRow(row)
}

func nonEmptyStrings(values ...any) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		s := strings.TrimSpace(fmt.Sprint(v))
		if s != "" && s != "<nil>" {
			out = append(out, s)
		}
	}
	return out
}

func (s *graphjinService) injectSystemNanoTables(database string) {
	if s.runtimeCore == nil {
		return
	}
	addTableConfig := func(name string, aliases ...string) {
		for _, table := range s.runtimeCore.Tables {
			if table.Database == database && table.Name == name && table.Table == "" {
				return
			}
		}
		readOnly := controlPlaneTableReadOnly(s.conf, database, name)
		s.runtimeCore.Tables = append(s.runtimeCore.Tables, core.Table{Database: database, Source: database, Schema: "main", Name: name, ReadOnly: readOnly})
		for _, alias := range aliases {
			s.runtimeCore.Tables = append(s.runtimeCore.Tables, core.Table{Database: database, Source: database, Schema: "main", Name: alias, Table: name, ReadOnly: readOnly})
		}
	}
	addTableConfig("gj_catalog", "parent", "children", "columns", "relationships")
	addTableConfig("gj_security")
	addTableConfig("gj_workflow")
	addTableConfig("gj_workflow_execution")
	addTableConfig("gj_config")
}

func (s *graphjinService) markSystemNanoChanged(reason string) {
	if err := s.refreshSystemNanoDB(); err != nil && s.log != nil {
		if strings.TrimSpace(reason) == "" {
			reason = "system graph change"
		}
		s.log.Warnf("nanodb refresh after %s failed: %v", reason, err)
	}
}

func (s *graphjinService) refreshMetadataGraphForRuntimeNano(ctx context.Context, gj *core.GraphJin, conf *core.Config, metadataDB string) error {
	if s == nil || s.systemNanoDB == nil || metadataDB == "" {
		return nil
	}
	if gj == nil || conf == nil {
		return s.refreshSystemNanoDB()
	}
	excludes := s.metadataSnapshotExcludesFor(metadataDB, conf, s.managedDBs)
	snapshot, err := gj.MetadataSnapshot(excludes...)
	if err != nil {
		return err
	}
	codeDBs := s.selectedCodeSQLDatabasesFor(conf, s.managedDBs)
	if len(codeDBs) == 1 {
		if managed, ok := s.managedDBs[codeDBs[0]]; ok && managed.handle != nil {
			targets := codeSQLTargetsFromMetadata(snapshot, nil)
			if err := managed.handle.SetDBRefTargets(ctx, targets); err != nil {
				return err
			}
			if err := managed.handle.RefreshPublicGraph(ctx); err != nil {
				return err
			}
		}
	}
	catalogSnapshot := core.BuildCatalogSnapshotWithOptions(snapshot, conf, s.catalogBuildOptions())
	return s.systemNanoDB.Refresh(s.systemNanoSnapshotFromCatalog(catalogSnapshot))
}

func nowNanoTimestamp() string {
	return time.Now().UTC().Format(workflowTimestampFormat)
}
