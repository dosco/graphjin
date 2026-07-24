package serv

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/core/v3/sourcecap"
)

func (s *graphjinService) initSystemNanoDBBeforeCore() error {
	if s == nil || s.conf == nil {
		return nil
	}
	if s.runtimeCore == nil {
		runtimeCore := cloneCoreConfig(s.conf.Core)
		s.runtimeCore = &runtimeCore
	}
	s.ensureRuntimeDatabaseEntries()
	if len(registeredSystemRoots(s.conf)) == 0 {
		s.metadataDB = ""
		return nil
	}
	name := allocateRuntimeDatabaseName(internalSystemDatabaseBase, &s.conf.Core, s.runtimeCore, s.dbs)
	s.metadataDB = name
	if s.systemNanoDB == nil {
		db, err := core.NewNanoDB(systemNanoSnapshotForRoots("", nil, registeredSystemRoots(s.conf)))
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
	if err := assertArtifactNanoRoleDefaults(s.conf, s.runtimeCore, name); err != nil {
		return err
	}
	if err := assertWatchNanoRoleDefaults(s.conf, s.runtimeCore, name); err != nil {
		return err
	}
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
		if err := s.systemNanoDB.Refresh(systemNanoSnapshotForRoots(codeDB, nil, registeredSystemRoots(s.conf))); err != nil {
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

func (s *graphjinService) refreshSystemNanoDBForSources(sourceNames []string) error {
	if s == nil || s.systemNanoDB == nil || s.metadataDB == "" {
		return nil
	}
	sourceSet := catalogSourceSet(sourceNames)
	if len(sourceSet) == 0 || s.gj == nil || s.conf == nil {
		return s.refreshSystemNanoDB()
	}
	snapshot, err := s.gj.MetadataSnapshot(s.metadataSnapshotExcludesFor(s.metadataDB, &s.conf.Core, s.managedDBs)...)
	if err != nil {
		return err
	}
	if err := s.refreshCodeSQLTargetsForMetadata(context.Background(), snapshot); err != nil {
		return err
	}

	opts := s.catalogBuildOptions()
	fullCatalog := core.BuildCatalogSnapshotWithOptions(snapshot, &s.conf.Core, opts)
	scopedSnapshot := filterMetadataSnapshotForCatalogSources(snapshot, sourceSet)
	scopedOpts := filterCatalogBuildOptionsForSources(opts, sourceSet)
	scopedCatalog := core.BuildCatalogSnapshotWithOptions(scopedSnapshot, &s.conf.Core, scopedOpts)
	cp := newControlPlaneGraphQL(s)

	var catalogRows []core.NanoRow
	for _, row := range cp.allCatalogRows(scopedCatalog, core.CatalogQueryOutput{Cards: scopedCatalog.Cards}) {
		if catalogRowOwnedBySources(row, sourceSet) {
			catalogRows = append(catalogRows, normalizeCatalogNanoRow(row))
		}
	}
	var configRows []core.NanoRow
	if s.conf.systemControlPlaneEnabled() {
		row := copyNanoRow(cp.configRow())
		row["catalog_revision"] = fullCatalog.Revision
		configRows = []core.NanoRow{row}
	}

	if err := core.UpdateNanoDB(s.systemNanoDB, func(tx *core.NanoUpdate) error {
		if systemRootRegistered(s.conf, "gj_catalog") {
			if err := tx.ReplaceRows("main", "gj_catalog", func(row core.NanoRow) bool {
				return catalogNanoRowOwnedBySources(row, sourceSet)
			}, catalogRows); err != nil {
				return err
			}
		}
		if systemRootRegistered(s.conf, "gj_security") {
			if err := tx.ReplaceTable("main", "gj_security", securityNanoRows(s)); err != nil {
				return err
			}
		}
		if systemRootRegistered(s.conf, "gj_config") {
			return tx.ReplaceTable("main", "gj_config", configRows)
		}
		return nil
	}); err != nil {
		return err
	}
	s.catalogMu.Lock()
	s.catalogCache = &catalogCacheEntry{revision: fullCatalog.Revision, snapshot: fullCatalog}
	s.catalogMu.Unlock()
	return nil
}

func (s *graphjinService) refreshCatalogAfterCoreConfigChange(oldCore, newCore core.Config, reason string) error {
	if s == nil {
		return nil
	}
	s.invalidateCatalogCache()
	mode := "full"
	var changed []string
	var err error
	if s.systemNanoDB == nil {
		s.markCatalogChanged(reason)
		s.recordCatalogRefreshRuntimeEvent(context.Background(), mode, nil, reason, nil)
		return nil
	}
	changedSources := changedCatalogSources(oldCore, newCore)
	if len(changedSources) != 0 &&
		sourceScopedCatalogPatchAllowed(oldCore, newCore, changedSources) &&
		coreConfigChangeScopedToSources(oldCore, newCore, changedSources) {
		mode = "source_scoped"
		changed = sortedSourceSet(changedSources)
		err = s.refreshSystemNanoDBForSources(changed)
	} else {
		changed = sortedSourceSet(changedSources)
		err = s.refreshSystemNanoDB()
	}
	s.recordCatalogRefreshRuntimeEvent(context.Background(), mode, changed, reason, err)
	return err
}

func (s *graphjinService) recordCatalogRefreshRuntimeEvent(ctx context.Context, mode string, changedSources []string, reason string, err error) {
	if s == nil {
		return
	}
	status := runtimeStatusReady
	severity := "info"
	summary := "Catalog refresh completed after a runtime config change."
	nextAction := "Use gj_catalog/query_catalog for planning with the refreshed catalog."
	errorCode := ""
	details := map[string]any{
		"refresh_mode": mode,
		"reason":       reason,
	}
	if len(changedSources) != 0 {
		details["changed_sources"] = changedSources
	}
	if err != nil {
		status = runtimeStatusDegraded
		severity = "warn"
		summary = "Catalog refresh failed after a runtime config change."
		nextAction = "Check recent config/schema events, then retry catalog discovery or reload schema."
		errorCode = "catalog_refresh_failed"
		details["error"] = redactRuntimeError(err)
	}
	event := runtimeEvent{
		Phase:      "catalog",
		Status:     status,
		Severity:   severity,
		Summary:    summary,
		NextAction: nextAction,
		ErrorCode:  errorCode,
		Details:    details,
	}
	if snap, snapErr := s.catalogSnapshot(); snapErr == nil {
		event.CatalogRevision = snap.Revision
		details["catalog_revision"] = snap.Revision
	}
	s.recordRuntimeEvent(ctx, event)
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
	return systemNanoSnapshotForRoots(codeDB, systemNanoRows(s, catalogSnapshot, conf), registeredSystemRoots(s.conf))
}

func systemNanoSnapshot(_ string, codeDB string, rows map[string][]core.NanoRow) core.NanoSnapshot {
	return systemNanoSnapshotForRoots(codeDB, rows, []string{
		"gj_catalog", "gj_security", "gj_artifacts", "gj_watch", "gj_watch_event", "gj_watch_flow_preview",
		"gj_workflow", "gj_workflow_execution", "gj_config",
	})
}

func systemNanoSnapshotForRoots(codeDB string, rows map[string][]core.NanoRow, roots []string) core.NanoSnapshot {
	rootSet := make(map[string]bool, len(roots))
	for _, root := range roots {
		rootSet[root] = true
	}
	tables := make([]core.NanoTable, 0, len(roots))
	for _, root := range roots {
		var columns []core.NanoColumn
		switch root {
		case "gj_catalog":
			columns = catalogNanoColumns(codeDB)
		case "gj_security":
			columns = securityNanoColumns()
		case "gj_artifacts":
			columns = artifactNanoColumns()
		case "gj_watch":
			columns = watchNanoColumns()
		case "gj_watch_event":
			columns = watchEventNanoColumns()
		case "gj_watch_flow_preview":
			columns = watchFlowPreviewNanoColumns()
		case "gj_workflow":
			columns = workflowNanoColumns()
		case "gj_workflow_execution":
			columns = workflowExecutionNanoColumns()
		case "gj_config":
			columns = configNanoColumns()
		case "gj_runtime":
			columns = runtimeNanoColumns()
		default:
			continue
		}
		for i := range columns {
			if columns[i].FKeyTable != "" && !rootSet[columns[i].FKeyTable] {
				columns[i].FKeyTable = ""
				columns[i].FKeyColumn = ""
				columns[i].FKeyUnique = false
			}
		}
		tables = append(tables, core.NanoTable{Name: root, Columns: columns, Rows: rowsFor(rows, root)})
	}
	return core.NanoSnapshot{Schema: "main", Tables: tables}
}

func runtimeNanoColumns() []core.NanoColumn {
	managed := runtimeManagedTable()
	columns := make([]core.NanoColumn, 0, len(managed.Columns))
	for _, column := range managed.Columns {
		columns = append(columns, core.NanoColumn{
			Name:       column.Name,
			Type:       column.Type,
			PrimaryKey: column.PrimaryKey,
			NotNull:    column.PrimaryKey,
			FullText:   column.FullText,
		})
	}
	return columns
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
		{Name: "owner_source", Type: "text", Index: true},
		{Name: "owner_sources_json", Type: "json"},
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
		{Name: "update_sources", Type: "json"},
		{Name: "remove_sources", Type: "json"},
		{Name: "system", Type: "json"},
		{Name: "workflows", Type: "json"},
		{Name: "databases", Type: "json"},
		{Name: "relationships", Type: "json"},
		{Name: "tables", Type: "json"},
		{Name: "roles", Type: "json"},
		{Name: "blocklist", Type: "json"},
		{Name: "functions", Type: "json"},
		{Name: "resolvers", Type: "json"},
		{Name: "mcp", Type: "json"},
		{Name: "serv", Type: "json"},
		{Name: "config_json", Type: "json"},
		{Name: "redacted_paths", Type: "json"},
		{Name: "updated_at", Type: "text"},
		{Name: "catalog_revision", Type: "text"},
		{Name: "mode", Type: "text"},
		{Name: "preview_id", Type: "text"},
		{Name: "expected_catalog_revision", Type: "text"},
		{Name: "scope", Type: "text"},
		{Name: "reload_mode", Type: "text"},
		{Name: "reload_strategy", Type: "text"},
		{Name: "source_patches", Type: "json"},
		{Name: "valid", Type: "boolean"},
		{Name: "applied", Type: "boolean"},
		{Name: "expires_at", Type: "text"},
		{Name: "change_summary_json", Type: "json"},
		{Name: "findings_json", Type: "json"},
		{Name: "errors_json", Type: "json"},
	}
}

const artifactProjectionContentMaxBytes = 32 * 1024

func artifactNanoColumns() []core.NanoColumn {
	return []core.NanoColumn{
		{Name: "id", Type: "text", PrimaryKey: true, NotNull: true},
		{Name: "name", Type: "text", Index: true},
		{Name: "kind", Type: "text", Index: true},
		{Name: "path", Type: "text"},
		{Name: "source", Type: "text", Index: true},
		{Name: "visibility", Type: "text", Index: true},
		{Name: "read_only", Type: "boolean", Index: true},
		{Name: "account_id", Type: "text", Index: true},
		{Name: "owner_id", Type: "text", Index: true},
		{Name: "account_ref", Type: "text", Index: true},
		{Name: "owner_ref", Type: "text", Index: true},
		{Name: "content", Type: "text"},
		{Name: "content_truncated", Type: "boolean"},
		{Name: "content_json", Type: "json"},
		{Name: "metadata_json", Type: "json"},
		{Name: "content_hash", Type: "text", Index: true},
		{Name: "status", Type: "text", Index: true},
		{Name: "revision", Type: "integer"},
		{Name: "created_at", Type: "text"},
		{Name: "updated_at", Type: "text"},
		// Delete responses select `deleted` (the managed handler returns
		// {id, deleted: true}); expose it like gj_workflow does.
		{Name: "deleted", Type: "boolean"},
		{Name: "search_vector", Type: "text", FullText: true},
	}
}

func watchNanoColumns() []core.NanoColumn {
	return []core.NanoColumn{
		{Name: "id", Type: "text", PrimaryKey: true, NotNull: true},
		{Name: "name", Type: "text", Index: true},
		{Name: "description", Type: "text"},
		{Name: "query", Type: "text"},
		{Name: "saved_query_name", Type: "text", Index: true},
		{Name: "variables_json", Type: "json"},
		{Name: "condition_js", Type: "text"},
		{Name: "delivery_json", Type: "json"},
		{Name: "enrich_json", Type: "json"},
		{Name: "evidence_json", Type: "json"},
		{Name: "lifecycle", Type: "text", Index: true},
		{Name: "lease_expires_at", Type: "text", Index: true},
		{Name: "lease_owner_id", Type: "text", Index: true},
		{Name: "status", Type: "text", Index: true},
		{Name: "approval", Type: "text", Index: true},
		{Name: "enabled", Type: "boolean", Index: true},
		{Name: "account_id", Type: "text", Index: true},
		{Name: "owner_id", Type: "text", Index: true},
		{Name: "owner_role", Type: "text", Index: true},
		{Name: "account_ref", Type: "text", Index: true},
		{Name: "owner_ref", Type: "text", Index: true},
		{Name: "last_data_hash", Type: "text", Index: true},
		{Name: "last_fired_at", Type: "text"},
		{Name: "last_error", Type: "text"},
		{Name: "failure_count", Type: "integer"},
		{Name: "created_at", Type: "text"},
		{Name: "updated_at", Type: "text"},
		// Delete responses select `deleted` (the managed handler returns
		// {id, deleted: true}); expose it like gj_workflow does.
		{Name: "deleted", Type: "boolean"},
		{Name: "search_vector", Type: "text", FullText: true},
	}
}

func watchEventNanoColumns() []core.NanoColumn {
	return []core.NanoColumn{
		{Name: "id", Type: "text", PrimaryKey: true, NotNull: true},
		{Name: "watch_id", Type: "text", Index: true, FKeyTable: "gj_watch", FKeyColumn: "id", FKeyUnique: true},
		{Name: "data_hash", Type: "text", Index: true},
		{Name: "data_json", Type: "json"},
		{Name: "data_truncated", Type: "boolean"},
		{Name: "evidence_json", Type: "json"},
		{Name: "delivery_status", Type: "text", Index: true},
		{Name: "delivery_attempts", Type: "integer"},
		{Name: "delivery_json", Type: "json"},
		{Name: "receipt_json", Type: "json"},
		{Name: "enrichment_json", Type: "json"},
		{Name: "seen", Type: "boolean", Index: true},
		{Name: "seen_at", Type: "text"},
		{Name: "account_id", Type: "text", Index: true},
		{Name: "owner_id", Type: "text", Index: true},
		{Name: "account_ref", Type: "text", Index: true},
		{Name: "owner_ref", Type: "text", Index: true},
		{Name: "created_at", Type: "text", Index: true},
		{Name: "updated_at", Type: "text"},
		{Name: "search_vector", Type: "text", FullText: true},
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
	if workflowRows, err := cp.workflowRows(context.Background(), true, core.ManagedQueryRoot{}); err == nil {
		for _, row := range workflowRows {
			rows["gj_workflow"] = append(rows["gj_workflow"], normalizeWorkflowNanoRow(row))
		}
	}
	artifactMaxBytes := conf.EffectiveArtifactsConfig().ProjectionContentMaxBytes
	if artifactRows, err := newArtifactControlPlane(s).allArtifactRowsForProjection(context.Background()); err == nil {
		for _, row := range artifactRows {
			rows["gj_artifacts"] = append(rows["gj_artifacts"], normalizeArtifactNanoRow(row, artifactMaxBytes))
		}
	}
	if watchRows, err := newWatchControlPlane(s).allWatchRowsForProjection(context.Background()); err == nil {
		for _, row := range watchRows {
			rows["gj_watch"] = append(rows["gj_watch"], normalizeWatchNanoRow(row))
		}
	}
	if watchEventRows, err := newWatchControlPlane(s).allWatchEventRowsForProjection(context.Background()); err == nil {
		for _, row := range watchEventRows {
			rows["gj_watch_event"] = append(rows["gj_watch_event"], normalizeWatchEventNanoRow(row))
		}
	}
	if s != nil && s.conf != nil && s.conf.systemControlPlaneEnabled() {
		rows["gj_config"] = []core.NanoRow{copyNanoRow(cp.configRow())}
	}
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
		fmt.Sprint(row["owner_source"]),
		fmt.Sprint(row["owner_sources_json"]),
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

func normalizeArtifactNanoRow(in map[string]any, maxBytes int) core.NanoRow {
	if maxBytes <= 0 {
		maxBytes = artifactProjectionContentMaxBytes
	}
	row := copyNanoRow(in)
	content := fmt.Sprint(row["content"])
	truncated := false
	if len(content) > maxBytes {
		content = content[:maxBytes]
		truncated = true
	}
	row["content"] = content
	// Oversized JSON cannot be truncated in place without becoming invalid, so
	// the projection drops it; the store row keeps the full value and the
	// gj_artifacts read-through path returns it.
	metadataText := artifactProjectionText(row["metadata_json"], maxBytes)
	for _, key := range []string{"content_json", "metadata_json"} {
		if artifactProjectionJSONLen(row[key]) > maxBytes {
			row[key] = nil
			truncated = true
		}
	}
	row["content_truncated"] = truncated
	row["search_vector"] = strings.Join([]string{
		fmt.Sprint(row["kind"]),
		fmt.Sprint(row["name"]),
		fmt.Sprint(row["path"]),
		fmt.Sprint(row["source"]),
		fmt.Sprint(row["visibility"]),
		content,
		metadataText,
	}, " ")
	return row
}

// artifactProjectionJSONLen approximates the serialized size of a projected
// JSON value so oversized payloads stay out of the in-memory table.
func artifactProjectionJSONLen(v any) int {
	switch t := v.(type) {
	case nil:
		return 0
	case string:
		return len(t)
	case []byte:
		return len(t)
	case json.RawMessage:
		return len(t)
	default:
		if b, err := json.Marshal(v); err == nil {
			return len(b)
		}
		return len(fmt.Sprint(v))
	}
}

// artifactProjectionText renders a value for the full-text search vector,
// byte-capped so oversized metadata stays findable by its leading tokens
// without unbounding the index.
func artifactProjectionText(v any, maxBytes int) string {
	if v == nil {
		return ""
	}
	text := fmt.Sprint(v)
	if maxBytes > 0 && len(text) > maxBytes {
		text = text[:maxBytes]
	}
	return text
}

func normalizeWatchNanoRow(in map[string]any) core.NanoRow {
	row := copyNanoRow(in)
	row["search_vector"] = strings.Join([]string{
		fmt.Sprint(row["name"]),
		fmt.Sprint(row["description"]),
		fmt.Sprint(row["query"]),
		fmt.Sprint(row["saved_query_name"]),
		fmt.Sprint(row["lifecycle"]),
		fmt.Sprint(row["lease_expires_at"]),
		fmt.Sprint(row["status"]),
		fmt.Sprint(row["approval"]),
		fmt.Sprint(row["evidence_json"]),
	}, " ")
	return row
}

func normalizeWatchEventNanoRow(in map[string]any) core.NanoRow {
	row := copyNanoRow(in)
	row["search_vector"] = strings.Join([]string{
		fmt.Sprint(row["watch_id"]),
		fmt.Sprint(row["data_hash"]),
		fmt.Sprint(row["data_json"]),
		fmt.Sprint(row["evidence_json"]),
		fmt.Sprint(row["delivery_status"]),
		fmt.Sprint(row["enrichment_json"]),
	}, " ")
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
	injectSystemNanoTablesInto(s.conf, s.runtimeCore, database)
}

func injectSystemNanoTablesInto(conf *Config, runtimeCore *core.Config, database string) {
	if runtimeCore == nil {
		return
	}
	addTableConfig := func(name string, aliases ...string) {
		for _, table := range runtimeCore.Tables {
			if table.Database == database && table.Name == name && table.Table == "" {
				return
			}
		}
		readOnly := controlPlaneTableReadOnly(conf, database, name)
		runtimeCore.Tables = append(runtimeCore.Tables, core.Table{Database: database, Schema: "main", Name: name, ReadOnly: readOnly, Generated: true})
		for _, alias := range aliases {
			runtimeCore.Tables = append(runtimeCore.Tables, core.Table{Database: database, Schema: "main", Name: alias, Table: name, ReadOnly: readOnly, Generated: true})
		}
	}
	for _, root := range registeredSystemRoots(conf) {
		if root == "gj_catalog" {
			addTableConfig(root, "parent", "children", "columns", "relationships")
			continue
		}
		addTableConfig(root)
	}
}

func (s *graphjinService) refreshWatchProjection() error {
	if s == nil || s.systemNanoDB == nil || s.metadataDB == "" || !s.watchesEnabled() {
		return nil
	}
	cp := newWatchControlPlane(s)
	watchRows, err := cp.allWatchRowsForProjection(context.Background())
	if err != nil {
		return err
	}
	eventRows, err := cp.allWatchEventRowsForProjection(context.Background())
	if err != nil {
		return err
	}
	nanoWatchRows := make([]core.NanoRow, 0, len(watchRows))
	for _, row := range watchRows {
		nanoWatchRows = append(nanoWatchRows, normalizeWatchNanoRow(row))
	}
	nanoEventRows := make([]core.NanoRow, 0, len(eventRows))
	for _, row := range eventRows {
		nanoEventRows = append(nanoEventRows, normalizeWatchEventNanoRow(row))
	}
	return core.UpdateNanoDB(s.systemNanoDB, func(tx *core.NanoUpdate) error {
		if err := tx.ReplaceTable("main", "gj_watch", nanoWatchRows); err != nil {
			return err
		}
		return tx.ReplaceRows("main", "gj_watch_event", func(row core.NanoRow) bool {
			return true
		}, nanoEventRows)
	})
}

func (s *graphjinService) markWatchChanged(reason string) {
	if s == nil {
		return
	}
	s.invalidateCatalogCache()
	if s.systemNanoDB == nil {
		s.markCatalogChanged(reason)
		return
	}
	if err := s.refreshWatchProjection(); err != nil && s.log != nil {
		s.log.Warnf("watch projection refresh failed: %s", err)
	}
}

func (s *graphjinService) refreshArtifactProjection() error {
	if s == nil || s.systemNanoDB == nil || s.metadataDB == "" || s.conf == nil || !s.conf.Core.Artifacts.Enabled {
		return nil
	}
	rows, err := newArtifactControlPlane(s).allArtifactRowsForProjection(context.Background())
	if err != nil {
		return err
	}
	maxBytes := s.conf.Core.EffectiveArtifactsConfig().ProjectionContentMaxBytes
	nanoRows := make([]core.NanoRow, 0, len(rows))
	for _, row := range rows {
		nanoRows = append(nanoRows, normalizeArtifactNanoRow(row, maxBytes))
	}
	return core.UpdateNanoDB(s.systemNanoDB, func(tx *core.NanoUpdate) error {
		return tx.ReplaceTable("main", "gj_artifacts", nanoRows)
	})
}

func (s *graphjinService) markArtifactChanged(reason string) {
	if s == nil {
		return
	}
	s.invalidateCatalogCache()
	if s.systemNanoDB == nil {
		s.markCatalogChanged(reason)
		return
	}
	if err := s.refreshArtifactProjection(); err != nil && s.log != nil {
		if strings.TrimSpace(reason) == "" {
			reason = "artifact change"
		}
		s.log.Warnf("artifact projection refresh after %s failed: %v", reason, err)
		s.recordRuntimeEvent(context.Background(), runtimeEvent{
			Phase:      "catalog",
			Status:     runtimeStatusDegraded,
			Severity:   "warn",
			Summary:    "Artifact NanoDB projection refresh failed after a runtime change.",
			NextAction: "Check artifact storage connectivity, then retry gj_artifacts or reload schema.",
			ErrorCode:  "artifact_projection_refresh_failed",
			Details:    map[string]any{"reason": reason, "error": err.Error()},
		})
	}
}

func (s *graphjinService) markSystemNanoChanged(reason string) {
	if err := s.refreshSystemNanoDB(); err != nil && s.log != nil {
		if strings.TrimSpace(reason) == "" {
			reason = "system graph change"
		}
		s.log.Warnf("nanodb refresh after %s failed: %v", reason, err)
		s.recordRuntimeEvent(context.Background(), runtimeEvent{
			Phase:      "catalog",
			Status:     runtimeStatusDegraded,
			Severity:   "warn",
			Summary:    "System NanoDB catalog refresh failed after a runtime change.",
			NextAction: "Check recent config/schema events, then retry catalog discovery or reload schema.",
			ErrorCode:  "catalog_refresh_failed",
			Details:    map[string]any{"reason": reason, "error": err.Error()},
		})
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
	if err := s.refreshCodeSQLTargetsForMetadata(ctx, snapshot); err != nil {
		return err
	}
	catalogSnapshot := core.BuildCatalogSnapshotWithOptions(snapshot, conf, s.catalogBuildOptions())
	return s.systemNanoDB.Refresh(s.systemNanoSnapshotFromCatalog(catalogSnapshot))
}

func (s *graphjinService) refreshCodeSQLTargetsForMetadata(ctx context.Context, snapshot *core.MetadataSnapshot) error {
	if s == nil || s.conf == nil {
		return nil
	}
	codeDBs := s.selectedCodeSQLDatabasesFor(&s.conf.Core, s.managedDBs)
	if len(codeDBs) != 1 {
		return nil
	}
	if managed, ok := s.managedDBs[codeDBs[0]]; ok && managed.handle != nil {
		targets := codeSQLTargetsFromMetadata(snapshot, nil)
		if err := managed.handle.SetDBRefTargets(ctx, targets); err != nil {
			return err
		}
		if err := managed.handle.RefreshPublicGraph(ctx); err != nil {
			return err
		}
	}
	return nil
}

func catalogSourceSet(sourceNames []string) map[string]struct{} {
	out := make(map[string]struct{}, len(sourceNames))
	for _, name := range sourceNames {
		name = strings.TrimSpace(name)
		if name != "" {
			out[name] = struct{}{}
		}
	}
	return out
}

func filterMetadataSnapshotForCatalogSources(snapshot *core.MetadataSnapshot, sources map[string]struct{}) *core.MetadataSnapshot {
	if snapshot == nil {
		return &core.MetadataSnapshot{}
	}
	owns := func(name string) bool {
		_, ok := sources[strings.TrimSpace(name)]
		return ok
	}
	out := &core.MetadataSnapshot{}
	for _, db := range snapshot.Databases {
		if owns(db.Name) {
			out.Databases = append(out.Databases, db)
		}
	}
	for _, table := range snapshot.Tables {
		if owns(table.DatabaseName) {
			out.Tables = append(out.Tables, table)
		}
	}
	for _, column := range snapshot.Columns {
		if owns(column.DatabaseName) {
			out.Columns = append(out.Columns, column)
		}
	}
	for _, rel := range snapshot.Relationships {
		if owns(rel.FromDatabaseName) || owns(rel.ToDatabaseName) {
			out.Relationships = append(out.Relationships, rel)
		}
	}
	for _, fn := range snapshot.Functions {
		if owns(fn.DatabaseName) {
			out.Functions = append(out.Functions, fn)
		}
	}
	for _, idx := range snapshot.Indexes {
		if owns(idx.DatabaseName) {
			out.Indexes = append(out.Indexes, idx)
		}
	}
	return out
}

func filterCatalogBuildOptionsForSources(opts core.CatalogBuildOptions, sources map[string]struct{}) core.CatalogBuildOptions {
	filtered := opts
	filtered.Sources = nil
	for _, source := range opts.Sources {
		if _, ok := sources[strings.TrimSpace(source.Name)]; ok {
			filtered.Sources = append(filtered.Sources, source)
		}
	}
	filtered.Workflows = nil
	filtered.Fragments = nil
	filtered.SavedQueries = nil
	return filtered
}

func catalogRowOwnedBySources(row map[string]any, sources map[string]struct{}) bool {
	if len(sources) == 0 {
		return false
	}
	if sourceInSet(fmt.Sprint(row["owner_source"]), sources) {
		return true
	}
	if catalogOwnerJSONContainsSource(row["owner_sources_json"], sources) {
		return true
	}
	if sourceInSet(fmt.Sprint(row["database_name"]), sources) {
		return true
	}
	id := fmt.Sprint(row["id"])
	for source := range sources {
		if id == "source:"+source || id == "database:"+source {
			return true
		}
	}
	evidence := fmt.Sprint(row["evidence_json"])
	for source := range sources {
		if strings.Contains(evidence, `"DatabaseName":"`+source+`"`) ||
			strings.Contains(evidence, `"FromDatabaseName":"`+source+`"`) ||
			strings.Contains(evidence, `"ToDatabaseName":"`+source+`"`) {
			return true
		}
	}
	return false
}

func catalogNanoRowOwnedBySources(row core.NanoRow, sources map[string]struct{}) bool {
	return catalogRowOwnedBySources(map[string]any(row), sources)
}

func sourceInSet(source string, sources map[string]struct{}) bool {
	_, ok := sources[strings.TrimSpace(source)]
	return ok
}

func catalogOwnerJSONContainsSource(raw any, sources map[string]struct{}) bool {
	switch value := raw.(type) {
	case []string:
		for _, source := range value {
			if sourceInSet(source, sources) {
				return true
			}
		}
	case []any:
		for _, source := range value {
			if sourceInSet(fmt.Sprint(source), sources) {
				return true
			}
		}
	case string:
		var list []string
		if err := json.Unmarshal([]byte(value), &list); err == nil {
			for _, source := range list {
				if sourceInSet(source, sources) {
					return true
				}
			}
		}
	}
	return false
}

func sortedSourceSet(sources map[string]struct{}) []string {
	out := make([]string, 0, len(sources))
	for source := range sources {
		out = append(out, source)
	}
	sort.Strings(out)
	return out
}

func changedCatalogSources(oldCore, newCore core.Config) map[string]struct{} {
	changed := make(map[string]struct{})
	oldSources := sourceConfigByCatalogName(oldCore.Sources)
	newSources := sourceConfigByCatalogName(newCore.Sources)
	sourceNames := make(map[string]struct{}, len(oldSources)+len(newSources))
	for name, oldSource := range oldSources {
		sourceNames[name] = struct{}{}
		if newSource, ok := newSources[name]; !ok || !reflect.DeepEqual(oldSource, newSource) {
			changed[name] = struct{}{}
		}
	}
	for name, newSource := range newSources {
		sourceNames[name] = struct{}{}
		if oldSource, ok := oldSources[name]; !ok || !reflect.DeepEqual(oldSource, newSource) {
			changed[name] = struct{}{}
		}
	}
	for name, oldDB := range oldCore.Databases {
		if _, ok := sourceNames[name]; !ok {
			continue
		}
		if newDB, ok := newCore.Databases[name]; !ok || !reflect.DeepEqual(oldDB, newDB) {
			changed[name] = struct{}{}
		}
	}
	for name, newDB := range newCore.Databases {
		if _, ok := sourceNames[name]; !ok {
			continue
		}
		if oldDB, ok := oldCore.Databases[name]; !ok || !reflect.DeepEqual(oldDB, newDB) {
			changed[name] = struct{}{}
		}
	}
	return changed
}

func sourceConfigByCatalogName(sources []core.SourceConfig) map[string]core.SourceConfig {
	out := make(map[string]core.SourceConfig, len(sources))
	for _, source := range sources {
		name := catalogSourceConfigName(source)
		if name != "" {
			out[name] = source
		}
	}
	return out
}

func catalogSourceConfigName(source core.SourceConfig) string {
	name := strings.TrimSpace(source.Name)
	if name != "" {
		return name
	}
	return strings.TrimSpace(source.CanonicalKind())
}

func coreConfigChangeScopedToSources(oldCore, newCore core.Config, sources map[string]struct{}) bool {
	oldCopy := cloneCoreConfig(oldCore)
	newCopy := cloneCoreConfig(newCore)
	oldCopy.Sources = filterSourcesOutsideSet(oldCopy.Sources, sources)
	newCopy.Sources = filterSourcesOutsideSet(newCopy.Sources, sources)
	for source := range sources {
		delete(oldCopy.Databases, source)
		delete(newCopy.Databases, source)
	}
	return reflect.DeepEqual(oldCopy, newCopy)
}

func sourceScopedCatalogPatchAllowed(oldCore, newCore core.Config, sources map[string]struct{}) bool {
	oldSources := sourceConfigByCatalogName(oldCore.Sources)
	newSources := sourceConfigByCatalogName(newCore.Sources)
	for name := range sources {
		oldKind := ""
		if oldSource, ok := oldSources[name]; ok {
			oldKind = oldSource.CanonicalKind()
		}
		newKind := ""
		if newSource, ok := newSources[name]; ok {
			newKind = newSource.CanonicalKind()
		}
		if oldKind != "" && oldKind != sourcecap.KindDatabase {
			return false
		}
		if newKind != "" && newKind != sourcecap.KindDatabase {
			return false
		}
		if oldKind == "" && newKind == "" {
			return false
		}
	}
	return true
}

func filterSourcesOutsideSet(sources []core.SourceConfig, remove map[string]struct{}) []core.SourceConfig {
	out := make([]core.SourceConfig, 0, len(sources))
	for _, source := range sources {
		if _, ok := remove[catalogSourceConfigName(source)]; ok {
			continue
		}
		out = append(out, source)
	}
	return out
}

func nowNanoTimestamp() string {
	return time.Now().UTC().Format(workflowTimestampFormat)
}
