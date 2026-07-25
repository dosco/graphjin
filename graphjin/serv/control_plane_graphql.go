package serv

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/core/v3/sourcecap"
	"github.com/mark3labs/mcp-go/mcp"
)

type controlPlaneGraphQL struct {
	service *graphjinService
}

type deleteFS interface {
	Delete(path string) error
}

func newControlPlaneGraphQL(s *graphjinService) controlPlaneGraphQL {
	return controlPlaneGraphQL{service: s}
}

func (h controlPlaneGraphQL) ManagedQueryTables() []core.ManagedTable {
	var out []core.ManagedTable
	if h.service != nil && h.service.conf != nil && (h.service.conf.catalogToolsEnabled() || h.service.conf.systemControlPlaneEnabled()) {
		out = append(out, graphjinControlPlaneTables()...)
	}
	if h.service != nil && h.service.conf != nil && h.service.conf.workflowsEnabled() {
		out = append(out, workflowControlPlaneTables()...)
	}
	return out
}

func (h controlPlaneGraphQL) ManagedMutationTables() []string {
	var tables []string
	if h.service != nil && h.service.conf != nil && h.service.conf.workflowsEnabled() {
		tables = append(tables, "gj_workflow", "gj_workflow_execution")
	}
	if h.service != nil && h.service.conf != nil && h.service.conf.systemControlPlaneEnabled() {
		tables = append(tables, "gj_config")
	}
	return tables
}

func graphjinControlPlaneTables() []core.ManagedTable {
	return []core.ManagedTable{
		managedTable("gj_catalog", []core.ManagedColumn{
			cpCol("id", "text", true), cpCol("kind", "text", false), cpCol("title", "text", false), cpCol("summary", "text", false),
			cpCol("name", "text", false),
			cpCol("database_name", "text", false), cpCol("schema_name", "text", false), cpCol("table_name", "text", false), cpCol("column_name", "text", false),
			cpCol("source", "text", false), cpCol("source_kind", "text", false), cpCol("owner_source", "text", false), cpCol("owner_sources_json", "json", false),
			cpCol("risk_level", "text", false), cpCol("confidence", "text", false), cpCol("sensitive", "boolean", false),
			cpCol("sensitivity", "text", false), cpCol("evidence_json", "json", false), cpCol("examples_json", "json", false), cpCol("suggested_next_json", "json", false),
			cpCol("detail_ref", "text", false), cpCol("details_json", "json", false), cpCol("edges_json", "json", false),
			cpCol("query_json", "json", false), cpCol("input_schema_json", "json", false), cpCol("output_schema_json", "json", false),
			cpCol("safety_json", "json", false), cpCol("enabled", "boolean", false), cpCol("capability_kind", "text", false),
			cpCol("graphql_query", "text", false), cpCol("graphql_mutation", "text", false),
			cpCol("created_at", "text", false), cpCol("updated_at", "text", false), cpCol("score", "float", false), cpCol("search_rank", "float", false), cpFullTextCol("search_vector"),
		}),
		managedTable("gj_config", []core.ManagedColumn{
			cpCol("id", "text", true), cpCol("sources_used", "boolean", false), cpCol("config_path", "text", false), cpCol("active_database", "text", false),
			cpCol("sources", "json", false), cpCol("update_sources", "json", false), cpCol("remove_sources", "json", false),
			cpCol("databases", "json", false), cpCol("relationships", "json", false), cpCol("tables", "json", false),
			cpCol("roles", "json", false), cpCol("blocklist", "json", false), cpCol("functions", "json", false), cpCol("resolvers", "json", false),
			cpCol("mcp", "json", false), cpCol("serv", "json", false), cpCol("config_json", "json", false), cpCol("redacted_paths", "json", false), cpCol("updated_at", "text", false), cpCol("catalog_revision", "text", false),
			cpCol("mode", "text", false), cpCol("preview_id", "text", false), cpCol("expected_catalog_revision", "text", false), cpCol("source_patches", "json", false),
			cpCol("valid", "boolean", false), cpCol("applied", "boolean", false), cpCol("expires_at", "text", false), cpCol("change_summary_json", "json", false), cpCol("findings_json", "json", false), cpCol("errors_json", "json", false),
			cpCol("scope", "text", false), cpCol("reload_mode", "text", false), cpCol("reload_strategy", "text", false),
		}),
	}
}

func workflowControlPlaneTables() []core.ManagedTable {
	return []core.ManagedTable{
		managedTable("gj_workflow", []core.ManagedColumn{
			cpCol("name", "text", true), cpCol("description", "text", false), cpCol("tags", "json", false), cpCol("tags_json", "json", false),
			cpCol("variables", "json", false), cpCol("variables_json", "json", false), cpCol("code", "text", false), cpCol("path", "text", false),
			cpCol("source_hash", "text", false), cpCol("runtime", "text", false), cpCol("timeout_seconds", "integer", false),
			cpCol("created_at", "text", false), cpCol("updated_at", "text", false), cpCol("workflow_revision", "text", false),
			cpCol("catalog_item_id", "text", false), cpCol("catalog_revision", "text", false), cpCol("deleted", "boolean", false),
		}),
		managedTable("gj_workflow_execution", []core.ManagedColumn{cpCol("id", "text", true), cpCol("workflow_name", "text", false), cpCol("namespace", "text", false), cpCol("variables", "json", false), cpCol("status", "text", false), cpCol("result_json", "json", false), cpCol("error", "text", false), cpCol("duration_ms", "integer", false)}),
	}
}

func managedTable(name string, cols []core.ManagedColumn) core.ManagedTable {
	return core.ManagedTable{Name: name, Columns: cols}
}

func cpCol(name, typ string, pk bool) core.ManagedColumn {
	return core.ManagedColumn{Name: name, Type: typ, PrimaryKey: pk}
}

func cpFullTextCol(name string) core.ManagedColumn {
	return core.ManagedColumn{Name: name, Type: "text", FullText: true}
}

func (h controlPlaneGraphQL) ExecuteManagedQuery(ctx context.Context, req core.ManagedQueryRequest) (json.RawMessage, error) {
	out := make(map[string]any, len(req.Roots))
	for _, root := range req.Roots {
		rows, err := h.queryRows(ctx, root)
		if err != nil {
			return nil, err
		}
		out[root.FieldName] = filterRows(rows, root.Fields)
	}
	return json.Marshal(out)
}

func (h controlPlaneGraphQL) queryRows(ctx context.Context, root core.ManagedQueryRoot) ([]map[string]any, error) {
	switch root.Table {
	case "gj_catalog":
		return h.queryCatalog(ctx, root)
	case "gj_workflow":
		return h.workflowRows(ctx, true, root)
	case "gj_workflow_execution":
		return nil, fmt.Errorf("gj_workflow_execution is mutation-only and does not store run history")
	case "gj_config":
		return applyManagedQuery([]map[string]any{h.configRow()}, root), nil
	default:
		return nil, fmt.Errorf("unsupported GraphJin system query root: %s", root.Table)
	}
}

func (h controlPlaneGraphQL) queryCatalog(ctx context.Context, root core.ManagedQueryRoot) ([]map[string]any, error) {
	snap, err := h.service.catalogSnapshotForContext(ctx)
	if err != nil {
		return nil, err
	}
	return h.queryCatalogRowsFromSnapshotContext(ctx, snap, root), nil
}

func (h controlPlaneGraphQL) queryCatalogRowsFromSnapshot(snap *core.CatalogSnapshot, root core.ManagedQueryRoot) []map[string]any {
	return h.queryCatalogRowsFromSnapshotContext(context.Background(), snap, root)
}

func (h controlPlaneGraphQL) queryCatalogRowsFromSnapshotContext(ctx context.Context, snap *core.CatalogSnapshot, root core.ManagedQueryRoot) []map[string]any {
	search, where := splitSearchWhere(root.Where)
	if search == "" {
		return applyManagedQuery(h.allCatalogRows(snap, core.CatalogQueryOutput{Cards: snap.Cards}), root)
	}
	orderBy := make(map[string]string, len(root.OrderBy))
	for _, ob := range root.OrderBy {
		orderBy[ob.Column] = strings.ToLower(ob.Order)
	}
	result, err := h.service.queryCatalog(ctx, snap, core.CatalogQuery{
		Search:  search,
		Where:   where,
		OrderBy: orderBy,
		Limit:   root.Limit,
		Offset:  root.Offset,
		Explain: true,
	})
	if err != nil {
		return applyManagedQuery(h.allCatalogRows(snap, core.CatalogQueryOutput{Cards: snap.Cards}), root)
	}
	rows := h.catalogRowsFromCards(snap, result)
	rows = append(rows, applyManagedQuery(h.catalogEntrypointRows(snap), root)...)
	rows = append(rows, applyManagedQuery(h.catalogCapabilityRows(snap), root)...)
	rows = append(rows, applyManagedQuery(h.catalogSystemCapabilityRows(), root)...)
	if len(root.OrderBy) != 0 {
		sortRows(rows, root.OrderBy)
	} else {
		sortCatalogSearchRows(rows)
	}
	if root.Limit > 0 && root.Limit < len(rows) {
		rows = rows[:root.Limit]
	}
	return rows
}

func (h controlPlaneGraphQL) allCatalogRows(snap *core.CatalogSnapshot, result core.CatalogQueryOutput) []map[string]any {
	rows := h.catalogRowsFromCards(snap, result)
	rows = append(rows, h.catalogEntrypointRows(snap)...)
	rows = append(rows, h.catalogCapabilityRows(snap)...)
	rows = append(rows, h.catalogSystemCapabilityRows()...)
	return rows
}

func (h controlPlaneGraphQL) catalogRowsFromCards(snap *core.CatalogSnapshot, result core.CatalogQueryOutput) []map[string]any {
	rawRows := structRows(result.Cards)
	rows := rawRows[:0]
	for _, row := range rawRows {
		if fmt.Sprint(row["kind"]) == "capability" {
			continue
		}
		id, _ := row["id"].(string)
		if fmt.Sprint(row["kind"]) == "fragment" {
			row["name"] = row["title"]
		} else {
			row["name"] = catalogItemName(row)
		}
		row["details_json"] = mustMarshalString(snap.CardDetails(id))
		row["edges_json"] = mustMarshalString(snap.CardEdges(id))
		if match, ok := result.Matches[id]; ok {
			row["score"] = match.Score
			row["search_rank"] = match.Score
			row["_match_why"] = match.Why
			row["_matched_fields"] = match.MatchedFields
			row["_matched_terms"] = match.MatchedTerms
		}
		rows = append(rows, row)
	}
	return rows
}

func (h controlPlaneGraphQL) catalogEntrypointRows(snap *core.CatalogSnapshot) []map[string]any {
	rows := structRows(snap.EntryPoints)
	for _, row := range rows {
		row["kind"] = "entrypoint"
		row["title"] = row["name"]
		row["score"] = 0
		row["search_rank"] = 0
	}
	return rows
}

func (h controlPlaneGraphQL) catalogCapabilityRows(snap *core.CatalogSnapshot) []map[string]any {
	rows := structRows(snap.Capabilities)
	for _, row := range rows {
		row["capability_kind"] = row["kind"]
		row["kind"] = "capability"
		row["title"] = row["name"]
		row["score"] = 0
		row["search_rank"] = 0
	}
	return rows
}

func (h controlPlaneGraphQL) catalogSystemCapabilityRows() []map[string]any {
	rows := h.systemCapabilityRows()
	for _, row := range rows {
		row["capability_kind"] = row["kind"]
		row["kind"] = "system_capability"
		row["title"] = row["name"]
		row["score"] = 0
		row["search_rank"] = 0
	}
	return rows
}

func catalogItemName(row map[string]any) string {
	for _, key := range []string{"name", "table_name", "column_name", "title", "id"} {
		raw := row[key]
		if raw == nil {
			continue
		}
		if value := strings.TrimSpace(fmt.Sprint(raw)); value != "" {
			return value
		}
	}
	return ""
}

func (h controlPlaneGraphQL) workflowRows(ctx context.Context, includeSource bool, root core.ManagedQueryRoot) ([]map[string]any, error) {
	workflowSnap, err := h.service.workflowSnapshotForContext(ctx, h.service.workflowTimeoutSeconds())
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(workflowSnap.workflows))
	revision := ""
	if snap, err := h.service.catalogSnapshotForContext(ctx); err == nil {
		revision = snap.Revision
	}
	for _, wf := range workflowSnap.workflows {
		row := map[string]any{
			"name":              wf.Name,
			"description":       wf.Description,
			"tags":              wf.Tags,
			"tags_json":         mustMarshalString(wf.Tags),
			"variables":         wf.Variables,
			"variables_json":    mustMarshalString(wf.Variables),
			"path":              wf.Path,
			"source_hash":       wf.SourceHash,
			"runtime":           wf.Runtime,
			"timeout_seconds":   wf.TimeoutSeconds,
			"created_at":        wf.CreatedAt,
			"updated_at":        wf.UpdatedAt,
			"workflow_revision": workflowSnap.revision,
			"catalog_item_id":   "workflow:" + wf.Name,
			"catalog_revision":  revision,
		}
		if includeSource {
			if strings.HasPrefix(wf.Path, "artifact:") {
				if _, src, _, err := h.service.resolveWorkflowForContext(ctx, wf.Name); err == nil {
					row["code"] = workflowCodeWithoutMeta(src)
				}
			} else if src, err := h.service.fs.Get(wf.Path); err == nil {
				row["code"] = workflowCodeWithoutMeta(string(src))
			}
		}
		rows = append(rows, row)
	}
	return applyManagedQuery(rows, root), nil
}

func workflowCodeWithoutMeta(src string) string {
	if !strings.HasPrefix(src, workflowMetaPrefix) {
		return src
	}
	if idx := strings.IndexByte(src, '\n'); idx != -1 {
		return src[idx+1:]
	}
	return ""
}

func (h controlPlaneGraphQL) configRow() map[string]any {
	conf := h.service.conf
	coreConf := &conf.Core
	sources := redactedConfigValue(coreConf.Sources)
	databases := redactedConfigValue(coreConf.Databases)
	mcpConfig := mcpConfigMap(conf)
	row := map[string]any{
		"id":              "current",
		"sources_used":    coreConf.IsSourcesUsed(),
		"config_path":     conf.ConfigPath,
		"active_database": (&mcpServer{service: h.service}).getActiveDatabase(),
		"sources":         sources,
		"system":          redactedConfigValue(coreConf.System),
		"workflows":       redactedConfigValue(coreConf.Workflows),
		"databases":       databases,
		"relationships":   redactedConfigValue(coreConf.Relationships),
		"tables":          redactedConfigValue(coreConf.Tables),
		"roles":           redactedConfigValue(convertRolesToInfo(coreConf.Roles)),
		"blocklist":       coreConf.Blocklist,
		"functions":       redactedConfigValue(coreConf.Functions),
		"resolvers":       redactedConfigValue(coreConf.Resolvers),
		"mcp":             mcpConfig,
		"serv":            servConfigMap(conf),
		"redacted_paths":  []string{},
		"updated_at":      time.Now().UTC().Format(time.RFC3339Nano),
	}
	row["config_json"] = map[string]any{
		"active_database": row["active_database"],
		"sources":         row["sources"],
		"system":          row["system"],
		"workflows":       row["workflows"],
		"databases":       row["databases"],
		"relationships":   row["relationships"],
		"tables":          row["tables"],
		"roles":           row["roles"],
		"blocklist":       row["blocklist"],
		"functions":       row["functions"],
		"resolvers":       row["resolvers"],
		"mcp":             row["mcp"],
		"serv":            row["serv"],
	}
	if snap, err := h.service.catalogSnapshot(); err == nil {
		row["catalog_revision"] = snap.Revision
	}
	return row
}

// servConfigMap is the redacted view of the server-side (serv.Config) settings
// surfaced on the gj_config row. It mirrors mcpConfigMap: an explicit,
// snake_case projection so the agent and MCP clients can see server settings
// (auth, rate_limiter, redis, caching, uploads, agent, ...) alongside the core
// sections. Nested structs pass through redactedConfigValue so secrets never
// leave the process; the writable subset is a smaller allowlist enforced in
// applyServConfigPatch.
func servConfigMap(conf *Config) map[string]any {
	if conf == nil {
		return map[string]any{}
	}
	s := &conf.Serv
	return map[string]any{
		"app_name":                s.AppName,
		"host_port":               s.HostPort,
		"host":                    s.Host,
		"port":                    s.Port,
		"production":              s.Production,
		"log_level":               s.LogLevel,
		"log_format":              s.LogFormat,
		"http_compress":           s.HTTPGZip,
		"server_timing":           s.ServerTiming,
		"web_ui":                  s.WebUI,
		"reload_on_config_change": s.WatchAndReload,
		"cors_allowed_origins":    s.AllowedOrigins,
		"cors_allowed_headers":    s.AllowedHeaders,
		"cors_debug":              s.DebugCORS,
		"rate_limiter":            redactedConfigValue(s.RateLimiter),
		"auth":                    redactedConfigValue(s.Auth),
		"agent":                   redactedConfigValue(s.Agent),
		"redis":                   redactedConfigValue(s.Redis),
		"caching":                 redactedConfigValue(s.Caching),
		"uploads":                 redactedConfigValue(s.Uploads),
		"runtime_events":          redactedConfigValue(s.RuntimeEvents),
	}
}

func mcpConfigMap(conf *Config) map[string]any {
	mcpConf := MCPConfig{}
	if conf != nil {
		mcpConf = conf.MCP
	}
	return map[string]any{
		"disable":                  mcpConf.Disable,
		"allow_mutations":          mcpConf.AllowMutations,
		"allow_raw_queries":        mcpConf.AllowRawQueries,
		"allow_workflow_updates":   mcpConf.AllowWorkflowUpdates,
		"allow_workflow_execution": mcpConf.AllowWorkflowExecution,
		"allow_config_updates":     mcpConf.AllowConfigUpdates,
		"allow_schema_reload":      mcpConf.AllowSchemaReload,
		"allow_schema_updates":     mcpConf.AllowSchemaUpdates,
		"allow_dev_tools":          mcpConf.AllowDevTools,
		"legacy_discovery":         mcpConf.LegacyDiscovery,
		"stdio_user_id":            mcpConf.StdioUserID,
		"stdio_user_role":          mcpConf.StdioUserRole,
		"only":                     mcpConf.Only,
		"cursor_cache_ttl":         mcpConf.CursorCacheTTL,
		"cursor_cache_size":        mcpConf.CursorCacheSize,
	}
}

func redactedConfigValue(v any) any {
	data, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		return v
	}
	return redactConfigJSON(out)
}

func redactConfigJSON(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			if isSensitiveConfigKey(k) {
				x[k] = "[REDACTED]"
				continue
			}
			x[k] = redactConfigJSON(val)
		}
		return x
	case []any:
		for i := range x {
			x[i] = redactConfigJSON(x[i])
		}
		return x
	default:
		return v
	}
}

func isSensitiveConfigKey(key string) bool {
	k := strings.ToLower(key)
	k = strings.ReplaceAll(k, "-", "_")
	if strings.Contains(k, "password") ||
		strings.Contains(k, "secret") ||
		strings.Contains(k, "token") ||
		strings.Contains(k, "passphrase") ||
		strings.Contains(k, "private_key") ||
		strings.Contains(k, "client_key") ||
		strings.Contains(k, "api_key") ||
		strings.Contains(k, "apikey") ||
		strings.Contains(k, "authorization") ||
		strings.Contains(k, "cookie") ||
		k == "connection_string" ||
		k == "key_value" {
		return true
	}
	return false
}

func (h controlPlaneGraphQL) systemCapabilityRows() []map[string]any {
	conf := h.service.conf.MCP
	workflowWriteEnabled := conf.AllowWorkflowUpdates && !h.controlPlaneRootReadOnly("gj_workflow")
	workflowExecutionEnabled := !h.controlPlaneRootReadOnly("gj_workflow_execution")
	configWriteEnabled := conf.AllowConfigUpdates && !h.controlPlaneRootReadOnly("gj_config")
	watchEnabled := h.service != nil && h.service.watchesEnabled() && !h.controlPlaneRootReadOnly("gj_watch")
	watchEventEnabled := h.service != nil && h.service.watchesEnabled() && !h.controlPlaneRootReadOnly("gj_watch_event")
	caps := []map[string]any{
		{
			"name": "gj_workflow.insert_update_delete", "kind": "mutation", "enabled": workflowWriteEnabled,
			"summary":          "Create, update, and delete JavaScript workflow definition files.",
			"graphql_mutation": "gj_workflow(insert/update/delete)",
			"safety_json":      mustMarshalString(map[string]any{"writes_files": true, "requires_config": "mcp.allow_workflow_updates", "blocked_by": "read_only"}),
		},
		{
			"name": "gj_workflow_execution.insert", "kind": "execution", "enabled": workflowExecutionEnabled,
			"summary":          "Execute a saved JavaScript workflow and return an ephemeral result row. This is mutation-only and does not store run history.",
			"graphql_mutation": "gj_workflow_execution(insert)",
			"details_json": mustMarshalString(map[string]any{
				"root":          "gj_workflow_execution",
				"mutation_only": true,
				"ephemeral":     true,
				"stores_runs":   false,
				"input_shape":   `gj_workflow_execution(insert: { workflow_name: "...", variables: {...} })`,
				"return_fields": []string{"id", "workflow_name", "namespace", "status", "result_json", "error", "duration_ms"},
			}),
			"safety_json": mustMarshalString(map[string]any{"preferred_for_data_questions": true, "mutation_only": true, "ephemeral": true, "blocked_by": "read_only"}),
		},
		{
			"name": "gj_watch.insert_update_delete", "kind": "watch", "enabled": watchEnabled,
			"summary":          "Create, update, review, pause, and delete user-owned cursor-backed standing subscription watches, including absence detection, digests, and rollup watches. Watches are durable by default; ephemeral watches require an explicit lease_expires_at.",
			"graphql_mutation": `gj_watch(insert: { name: "...", lifecycle: "durable", query: "subscription { orders(first: 25, after: $cursor) { id status } orders_cursor }", delivery_json: {...} })`,
			"details_json": mustMarshalString(map[string]any{
				"root":            "gj_watch",
				"event_root":      "gj_watch_event",
				"required_fields": []string{"name", "query or saved_query_name"},
				"owner_scoped":    true,
				"query_type":      "subscription",
				"lifecycle":       map[string]string{"durable": "default; never deleted because an MCP client unsubscribes", "ephemeral": "requires future lease_expires_at and expires to status expired/enabled false"},
				"input_shape":     `gj_watch(insert: { name: "...", query: "subscription { <root>(first: 25, after: $cursor) { ... } <root>_cursor }", variables_json: {...}, enrich_json: {...}, delivery_json: { kind: "inbox", digest: { window: "1h" } }, absence_json: { enabled: true, window: "4h", repeat: false } }); gj_watch(where: { id: { eq: "..." } }, update: { flow_review_json: {...} | action_review_json: {...} })`,
				"return_fields":   []string{"id", "name", "lifecycle", "lease_expires_at", "status", "approval", "enabled", "absence_json", "flow_hash", "flow_approval", "flow_preview_json", "action_hash", "action_approval", "evidence_json", "created_at", "updated_at"},
				"rest":            []string{"GET /api/v1/watches", "POST /api/v1/watches", "PATCH /api/v1/watches/{id}", "DELETE /api/v1/watches/{id}", "POST /api/v1/watches/cleanup-preview", "POST /api/v1/watches/cleanup-apply"},
				"mcp_resource":    WatchEventsUnseenResourceURI,
			}),
			"examples_json": mustMarshalString([]map[string]string{
				{"name": "create durable cursor-backed subscription watch", "query": `mutation { gj_watch(insert: { name: "important_orders", query: "subscription { orders(first: 25, after: $cursor) { id status updated_at } orders_cursor }" }) { id name lifecycle status enabled evidence_json } }`},
				{"name": "create explicit ephemeral watch", "query": `mutation { gj_watch(insert: { name: "next_30m_orders", lifecycle: "ephemeral", lease_expires_at: "<future RFC3339 timestamp>", query: "subscription { orders(first: 25, after: $cursor) { id status updated_at } orders_cursor }" }) { id lifecycle lease_expires_at status } }`},
				{"name": "create absence watch", "query": `mutation { gj_watch(insert: { name: "shipment_scan_silence", query: "subscription { shipment_scans(first: 25, after: $cursor) { id scanned_at } shipment_scans_cursor }", absence_json: { enabled: true, window: "4h", repeat: false } }) { id absence_json status enabled action_hash action_approval } }`},
				{"name": "queue same-watch flow noise into hourly digests", "query": `mutation { gj_watch(insert: { name: "roast_digest", query: "subscription { roast_batches(first: 25, after: $cursor) { id phase temperature } roast_batches_cursor }", enrich_json: { enabled: true, kind: "flow", flow: "default_watch_triage" }, delivery_json: { kind: "inbox", digest: { window: "1h" } } }) { id flow_hash flow_approval delivery_json } }`},
				{"name": "create cross-watch rollup watch", "query": `mutation { gj_watch(insert: { name: "ops_rollup", query: "subscription($watch_ids: [String!], $gj_watch_event_cursor: Cursor) { gj_watch_event(where: { watch_id: { in: $watch_ids } }, first: 25, after: $gj_watch_event_cursor) { id watch_id data_json created_at } gj_watch_event_cursor }", variables_json: { watch_ids: ["watch:a", "watch:b"] } }) { id status enabled evidence_json } }`},
				{"name": "preview and approve an inline flow", "query": `mutation { gj_watch(where: { id: { eq: "watch:..." } }, update: { flow_review_json: { decision: "approve", expected_flow_hash: "...", samples_json: [{ status: "late" }] } }) { id flow_hash flow_approval flow_preview_json status enabled } }`},
				{"name": "approve an exact workflow or webhook action", "query": `mutation { gj_watch(where: { id: { eq: "watch:..." } }, update: { action_review_json: { decision: "approve", expected_action_hash: "..." } }) { id action_hash action_approval status enabled } }`},
				{"name": "list my watch inbox", "query": `query { gj_watch_event(order_by: { created_at: desc }, limit: 20) { id watch_id delivery_status seen created_at data_json } }`},
			}),
			"safety_json": mustMarshalString(map[string]any{
				"owner_scoped": true, "requires_user_identity": true, "direct_queries_must_be_subscriptions": true,
				"review_controls_separate_from_definition_changes": true, "flow_hash_pinned": true, "action_hash_pinned": true,
				"autonomous_actions_require_confirmation": true, "cleanup_preview_required": true,
				"do_not_broad_delete_durable_watches": true, "blocked_by": "read_only",
			}),
		},
		{
			"name": "gj_watch_event.update", "kind": "watch", "enabled": watchEventEnabled,
			"summary":          "Mark user-owned watch inbox events seen or unseen, or snooze them until a future time.",
			"graphql_mutation": `gj_watch_event(where: { id: { eq: "..." } }, update: { seen: true | false, snoozed_until: "<future RFC3339>" | null })`,
			"details_json": mustMarshalString(map[string]any{
				"root":          "gj_watch_event",
				"owner_scoped":  true,
				"mutation_only": false,
				"input_shape":   `gj_watch_event(where: { id: { eq: "..." } }, update: { seen: true | false, snoozed_until: "<future RFC3339>" | null })`,
				"return_fields": []string{"id", "seen", "seen_at", "snoozed_until", "updated_at"},
			}),
			"examples_json": mustMarshalString([]map[string]string{
				{"name": "snooze an event", "query": `mutation { gj_watch_event(where: { id: { eq: "we:..." } }, update: { snoozed_until: "<future RFC3339>" }) { id seen snoozed_until } }`},
			}),
			"safety_json": mustMarshalString(map[string]any{"owner_scoped": true, "requires_user_identity": true, "allowed_updates": []string{"seen", "snoozed_until"}, "blocked_by": "read_only"}),
		},
		{"name": "gj_config.update", "kind": "mutation", "enabled": configWriteEnabled, "summary": "Update GraphJin configuration.", "graphql_mutation": `gj_config(id: "current", update: ...)`},
		{"name": "reload_schema", "kind": "mutation", "enabled": conf.AllowSchemaReload, "summary": "Reload database schema metadata through the MCP tool surface."},
		{"name": "preview_schema_changes", "kind": "mutation", "enabled": conf.AllowSchemaUpdates, "summary": "Preview GraphJin DDL schema changes through the MCP tool surface."},
		{"name": "apply_schema_changes", "kind": "mutation", "enabled": conf.AllowSchemaUpdates, "summary": "Apply GraphJin DDL schema changes through the MCP tool surface."},
		{"name": "validate_where_clause", "kind": "validation", "enabled": true, "summary": "Validate where clauses with schema/operator guidance and compile-only GraphJin verification through the MCP tool surface."},
		{
			"name": "graphjin_error_repair", "kind": "repair", "enabled": true,
			"summary":      "Normal GraphJin errors include errors[].extensions.graphjin_repair with structured repair guidance for known query mistakes.",
			"details_json": mustMarshalString(map[string]any{"graphql_error_extension": "errors[].extensions.graphjin_repair", "fields": []string{"kind", "diagnosis", "repaired_query", "next"}}),
		},
		{
			"name": "graphjin_config_docs", "kind": "documentation", "enabled": true,
			"summary":       "Static GraphJin configuration documentation and examples are discoverable through catalog rows instead of the get_config_docs MCP tool in sources-used mode.",
			"details_json":  mustMarshalString(map[string]any{"recommended_filters": []string{`kind = "system_capability"`, `name = "graphjin_config_docs"`}, "runtime_config": "Use gj_config only when role permissions explicitly allow it."}),
			"examples_json": mustMarshalString([]string{`query_catalog(search: "config docs", where: { kind: { eq: "system_capability" } })`}),
		},
		{
			"name": "gj_security.query", "kind": "security", "enabled": true,
			"summary":       "Read GraphJin security posture, effective policy rows, config audits, runtime evidence, and findings from the read-only gj_security NanoDB table.",
			"graphql_query": `gj_security(where: { kind: { eq: "finding" }, severity: { in: ["high", "critical"] } }) { id scope config_id mode severity title recommendation evidence_json }`,
			"details_json": mustMarshalString(map[string]any{
				"root":                "gj_security",
				"kinds":               []string{"summary", "policy", "finding"},
				"scopes":              []string{"runtime", "config"},
				"modes":               []string{"dev", "prod", "agentic"},
				"source_kinds":        sourcecap.Kinds(),
				"severity_levels":     []string{"critical", "high", "medium", "low"},
				"agentic_audience":    "ordinary company end users using an approved agentic deployment",
				"filter_by":           []string{"kind", "report", "scope", "config_id", "config_active", "mode", "status", "severity", "severity_rank", "surface", "transport", "database_name", "source", "source_kind", "table_name", "column_name", "role", "audience", "capability", "action", "default_effective", "effective", "weakens_default", "read_only", "override_key", "override_explicit", "override_source"},
				"json_fields":         []string{"summary_json", "evidence_json", "details_json", "examples_json", "safety_json"},
				"read_shape":          `gj_security(where: { kind: { eq: "policy" } }) { id scope config_id mode surface role capability action default_effective effective weakens_default evidence_json }`,
				"singleton_shape":     `gj_security(id: "summary") { id kind scope mode summary_json }`,
				"per_config_ids":      []string{"config:prod:summary", "config:prod:policy:<surface>", "config:prod:finding:<severity>:<surface>"},
				"source_capabilities": sourcecap.CapabilityMap(),
				"mode_definitions": map[string]string{
					"dev":     "developer audit mode with detailed security/config/workflow visibility",
					"prod":    "strict production, system discovery/audit/control-plane reads blocked unless explicitly granted",
					"agentic": "agentic deployment for authenticated company end users: gj_catalog and approved workflow execution are available; detailed audit/config/workflow-code surfaces require explicit authenticated grants",
				},
			}),
			"examples_json": mustMarshalString([]map[string]string{
				{"name": "active summary", "query": `query { gj_security(id: "summary") { id kind scope mode summary_json safety_json } }`},
				{"name": "high critical findings across all configs", "query": `query { gj_security(where: { kind: { eq: "finding" }, severity: { in: ["high", "critical"] } }, order_by: { severity_rank: desc }) { id scope config_id mode severity title recommendation evidence_json } }`},
				{"name": "prod config findings", "query": `query { gj_security(where: { scope: { eq: "config" }, mode: { eq: "prod" }, kind: { eq: "finding" } }) { id config_id config_file severity title recommendation } }`},
				{"name": "agentic effective policy", "query": `query { gj_security(where: { kind: { eq: "policy" }, mode: { eq: "agentic" } }) { id scope config_id surface role capability action default_effective effective weakens_default } }`},
				{"name": "system capability policy", "query": `query { gj_security(where: { kind: { eq: "policy" }, source_kind: { eq: "system" }, capability: { eq: "security.read" } }) { id source source_kind capability override_explicit default_effective effective evidence_json } }`},
				{"name": "explicit override review", "query": `query { gj_security(where: { override_explicit: { eq: true } }) { id scope config_id mode override_key override_source default_effective effective weakens_default } }`},
			}),
			"safety_json": mustMarshalString(map[string]any{
				"read_only": true,
				"guidance":  "Check gj_catalog first to discover the gj_security API, then query gj_security before config, workflow, schema, file-source, or code-source changes. Use findings as evidence; apply changes through normal guarded config/control-plane APIs.",
				"agentic":   "Normal agentic users should discover through gj_catalog and execute approved workflows. Detailed gj_security, gj_config, and gj_workflow.code require an explicit authenticated grant.",
			}),
		},
		{
			"name": "gj_runtime.query", "kind": "runtime", "enabled": h.service != nil && h.service.conf != nil && h.service.conf.runtimeRootEnabled(),
			"summary":       "Read compact agentic GraphJin runtime health, source health, recent structured events, and suggested next actions from gj_runtime.",
			"graphql_query": `gj_runtime(where: { kind: { in: ["status", "source", "event"] } }, order_by: { created_at: desc }, limit: 20) { kind source source_kind status severity summary next_action details_json }`,
			"details_json": mustMarshalString(map[string]any{
				"root":               "gj_runtime",
				"kinds":              []string{"status", "source", "event"},
				"agentic_only":       true,
				"source_capability":  "runtime.read",
				"role_default":       "authenticated user allowed in agentic mode; anon blocked",
				"decision_support":   true,
				"audit_history":      false,
				"memory_defaults":    map[string]any{"max_events": runtimeDefaultMaxEvents, "ttl_seconds": int(runtimeDefaultTTL.Seconds())},
				"redis_preferred":    "When redis.url is configured and reachable, Redis stores shared events and per-node statuses for horizontally scaled GraphJin.",
				"filter_by":          []string{"kind", "created_at", "node_id", "mode", "store", "phase", "status", "severity", "source", "source_kind", "database_name", "active_database", "schema_ready", "catalog_revision", "error_code"},
				"json_fields":        []string{"details_json", "suggested_next_json"},
				"when_to_use":        []string{"before workflow/config/schema actions", "after GraphJin errors", "when stale schema is suspected", "when source health matters", "when a database appears disconnected", "when Redis is degraded", "when reload/discovery/catalog refresh problems are suspected"},
				"degraded_guidance":  "When a status row is degraded, follow next_action before continuing.",
				"example_query":      `query { gj_runtime(where: { kind: { in: ["status", "source", "event"] } }, order_by: { created_at: desc }, limit: 20) { kind source source_kind status severity summary next_action details_json } }`,
				"excluded_from_rows": []string{"raw_sql", "variables", "headers", "result_bodies", "connection_strings", "secrets", "stack_traces", "full_request_payloads"},
			}),
			"examples_json": mustMarshalString([]map[string]string{
				{"name": "latest runtime decision context", "query": `query { gj_runtime(where: { kind: { in: ["status", "source", "event"] } }, order_by: { created_at: desc }, limit: 20) { kind source source_kind status severity summary next_action details_json } }`},
				{"name": "source health", "query": `query { gj_runtime(where: { kind: { eq: "source" } }, order_by: { source: asc }) { source source_kind database_name status severity schema_ready table_count duration_ms summary next_action details_json } }`},
				{"name": "degraded status rows", "query": `query { gj_runtime(where: { kind: { eq: "status" }, status: { neq: "ready" } }) { node_id store status severity summary next_action suggested_next_json } }`},
				{"name": "recent schema and database events", "query": `query { gj_runtime(where: { phase: { in: ["schema", "database"] } }, order_by: { created_at: desc }, limit: 10) { created_at phase status severity database_name summary next_action } }`},
			}),
			"safety_json": mustMarshalString(map[string]any{
				"read_only":         true,
				"agentic_only":      true,
				"not_audit_history": true,
				"bounded":           true,
				"redacted":          true,
				"guidance":          "Use gj_runtime for current decision support, not forensic history. Follow next_action when status is degraded.",
			}),
		},
	}
	for _, cap := range caps {
		if _, ok := cap["safety_json"]; !ok {
			cap["safety_json"] = mustMarshalString(map[string]any{"enabled": cap["enabled"]})
		}
	}
	return caps
}

func (h controlPlaneGraphQL) ExecuteManagedMutation(ctx context.Context, req core.ManagedMutationRequest) (json.RawMessage, error) {
	out := make(map[string]any, len(req.Roots))
	for _, root := range req.Roots {
		row, err := h.mutateRow(ctx, root)
		if err != nil {
			return nil, err
		}
		out[root.FieldName] = filterRow(row, root.Fields)
	}
	return json.Marshal(out)
}

func (h controlPlaneGraphQL) mutateRow(ctx context.Context, root core.ManagedMutationRoot) (map[string]any, error) {
	if h.controlPlaneRootReadOnly(root.Table) {
		return nil, fmt.Errorf("mutations blocked: table %s is read-only", root.Table)
	}
	switch root.Table {
	case "gj_workflow":
		return h.mutateWorkflow(ctx, root)
	case "gj_workflow_execution":
		if root.Operation != "insert" {
			return nil, fmt.Errorf("workflow execution only supports insert mutations")
		}
		return h.runWorkflow(ctx, root)
	case "gj_config":
		return h.mutateConfig(ctx, root)
	default:
		return nil, fmt.Errorf("unsupported GraphJin system mutation root: %s", root.Table)
	}
}

func (h controlPlaneGraphQL) controlPlaneRootReadOnly(table string) bool {
	if h.service == nil || h.service.conf == nil {
		return true
	}
	return controlPlaneTableReadOnly(h.service.conf, h.service.metadataDB, table)
}

func (h controlPlaneGraphQL) mutateWorkflow(ctx context.Context, root core.ManagedMutationRoot) (map[string]any, error) {
	if !h.service.conf.MCP.AllowWorkflowUpdates {
		return nil, fmt.Errorf("workflow updates are not allowed; enable mcp.allow_workflow_updates")
	}
	switch root.Operation {
	case "insert", "upsert", "update":
		input := root.Input
		if input == nil {
			input = map[string]interface{}{}
		}
		name := stringFrom(input, "name")
		if name == "" {
			name = stringFromWhere(root.Where, "name")
		}
		description := stringFrom(input, "description")
		code := stringFrom(input, "code")
		if name == "" {
			return nil, fmt.Errorf("workflow name is required")
		}
		if description == "" {
			return nil, fmt.Errorf("workflow description is required")
		}
		if code == "" {
			return nil, fmt.Errorf("workflow code is required")
		}
		if !workflowNameRe.MatchString(name) {
			return nil, fmt.Errorf("invalid workflow name: must be alphanumeric with hyphens/underscores, 1-64 chars")
		}
		if !strings.Contains(code, "function main") {
			return nil, fmt.Errorf("code must define a function main(input) entry point")
		}
		tags := stringSliceFrom(input["tags"])
		vars, err := parseWorkflowVariables(input["variables"])
		if err != nil {
			return nil, err
		}
		now := time.Now().UTC()
		existingMeta := WorkflowMeta{}
		if h.service.artifactStoreForUser(ctx) {
			if row, ok, err := h.service.userArtifactRow(ctx, artifactKindWorkflow, name); err != nil {
				return nil, err
			} else if ok {
				existingMeta = workflowMetaFromArtifact(row)
			}
		} else {
			path := filepath.Join(h.service.workflowBasePath(), name+workflowExt)
			if src, err := h.service.fs.Get(path); err == nil {
				existingMeta, _ = parseWorkflowMeta(string(src))
				existingMeta = catalogWorkflowMeta(existingMeta)
			}
		}
		createdAt := existingMeta.CreatedAt
		if createdAt == "" {
			createdAt = formatWorkflowTime(now)
		}
		meta := WorkflowMeta{
			Description: description,
			Tags:        tags,
			Variables:   vars,
			CreatedAt:   createdAt,
			UpdatedAt:   formatWorkflowTime(now),
		}
		if h.service.artifactStoreForUser(ctx) {
			if _, err := h.service.saveUserArtifact(ctx, artifactKindWorkflow, name, code, workflowMetaMap(meta)); err != nil {
				return nil, err
			}
			h.service.markWorkflowChanged("workflow artifact mutation")
			h.service.recordRuntimeEvent(ctx, runtimeEvent{
				Phase:      "workflow",
				Status:     runtimeStatusReady,
				Severity:   "info",
				Summary:    "User workflow artifact was saved through a guarded mutation.",
				NextAction: "Query gj_catalog or gj_workflow before executing the updated workflow.",
				Details:    map[string]any{"workflow_name": name, "operation": root.Operation, "source": "database"},
			})
			return h.workflowMutationRow(ctx, name, false), nil
		}
		if h.service.prod {
			return nil, fmt.Errorf("workflow file writes are only allowed in dev fallback mode")
		}
		path := filepath.Join(h.service.workflowBasePath(), name+workflowExt)
		createdAt, _ = h.service.workflowTimestamps(path, existingMeta, now)
		meta.CreatedAt = createdAt
		metaJSON, err := json.Marshal(meta)
		if err != nil {
			return nil, err
		}
		body := workflowMetaPrefix + string(metaJSON) + "\n" + code
		if err := h.service.fs.Put(path, []byte(body)); err != nil {
			return nil, err
		}
		h.service.markWorkflowChanged("workflow mutation")
		h.service.recordRuntimeEvent(context.Background(), runtimeEvent{
			Phase:      "workflow",
			Status:     runtimeStatusReady,
			Severity:   "info",
			Summary:    "Workflow definition was saved through a guarded mutation.",
			NextAction: "Query gj_catalog or gj_workflow before executing the updated workflow.",
			Details:    map[string]any{"workflow_name": name, "operation": root.Operation},
		})
		return h.workflowMutationRow(ctx, name, false), nil
	case "delete":
		name := stringFromWhere(root.Where, "name")
		if name == "" {
			name = stringFrom(root.Input, "name")
		}
		if name == "" {
			return nil, fmt.Errorf("workflow delete requires where: { name: { eq: ... } } or name input")
		}
		if !workflowNameRe.MatchString(name) {
			return nil, fmt.Errorf("invalid workflow name: %s", name)
		}
		if h.service.artifactStoreForUser(ctx) {
			if err := h.service.deleteUserArtifact(ctx, artifactKindWorkflow, name); err != nil {
				return nil, err
			}
			h.service.markWorkflowChanged("workflow artifact mutation")
			h.service.recordRuntimeEvent(ctx, runtimeEvent{
				Phase:      "workflow",
				Status:     runtimeStatusReady,
				Severity:   "info",
				Summary:    "User workflow artifact was deleted through a guarded mutation.",
				NextAction: "Refresh catalog-guided planning before referencing the deleted workflow.",
				Details:    map[string]any{"workflow_name": name, "operation": root.Operation, "source": "database"},
			})
			return h.workflowMutationRow(ctx, name, true), nil
		}
		if h.service.prod {
			return nil, fmt.Errorf("workflow file deletes are only allowed in dev fallback mode")
		}
		fs, ok := h.service.fs.(deleteFS)
		if !ok {
			return nil, fmt.Errorf("workflow delete is not supported by this filesystem")
		}
		if err := fs.Delete(filepath.Join(h.service.workflowBasePath(), name+workflowExt)); err != nil {
			return nil, err
		}
		h.service.markWorkflowChanged("workflow mutation")
		h.service.recordRuntimeEvent(context.Background(), runtimeEvent{
			Phase:      "workflow",
			Status:     runtimeStatusReady,
			Severity:   "info",
			Summary:    "Workflow definition was deleted through a guarded mutation.",
			NextAction: "Refresh catalog-guided planning before referencing the deleted workflow.",
			Details:    map[string]any{"workflow_name": name, "operation": root.Operation},
		})
		return h.workflowMutationRow(ctx, name, true), nil
	default:
		return nil, fmt.Errorf("unsupported gj_workflow operation: %s", root.Operation)
	}
}

func (h controlPlaneGraphQL) workflowMutationRow(ctx context.Context, name string, deleted bool) map[string]any {
	revision := ""
	if snap, err := h.service.catalogSnapshotForContext(ctx); err == nil {
		revision = snap.Revision
	}
	workflowSnap, _ := h.service.workflowSnapshotForContext(ctx, h.service.workflowTimeoutSeconds())
	row := map[string]any{
		"name":              name,
		"catalog_item_id":   "workflow:" + name,
		"catalog_revision":  revision,
		"workflow_revision": workflowSnap.revision,
		"deleted":           deleted,
	}
	for _, wf := range workflowSnap.workflows {
		if wf.Name == name {
			row["description"] = wf.Description
			row["tags"] = wf.Tags
			row["tags_json"] = mustMarshalString(wf.Tags)
			row["variables"] = wf.Variables
			row["variables_json"] = mustMarshalString(wf.Variables)
			row["path"] = wf.Path
			row["source_hash"] = wf.SourceHash
			row["runtime"] = wf.Runtime
			row["timeout_seconds"] = wf.TimeoutSeconds
			row["created_at"] = wf.CreatedAt
			row["updated_at"] = wf.UpdatedAt
			break
		}
	}
	return row
}

func (h controlPlaneGraphQL) runWorkflow(ctx context.Context, root core.ManagedMutationRoot) (map[string]any, error) {
	input := root.Input
	name := stringFrom(input, "workflow_name")
	if name == "" {
		name = stringFrom(input, "name")
	}
	if name == "" {
		return nil, fmt.Errorf("workflow_name is required")
	}
	namespace := stringFrom(input, "namespace")
	var nsPtr *string
	if namespace != "" {
		nsPtr = &namespace
	}
	vars, _ := input["variables"].(map[string]interface{})
	if vars == nil {
		vars = map[string]interface{}{}
	}
	start := time.Now()
	out, err := h.service.runNamedWorkflow(ctx, name, vars, nsPtr)
	duration := time.Since(start).Milliseconds()
	row := map[string]any{
		"id":            fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%s:%d", name, start.UnixNano()))))[:16],
		"workflow_name": name,
		"namespace":     namespace,
		"variables":     vars,
		"duration_ms":   duration,
	}
	if err != nil {
		row["status"] = "error"
		row["error"] = err.Error()
		h.service.recordRuntimeEvent(ctx, runtimeEvent{
			Phase:      "workflow",
			Status:     runtimeStatusFailed,
			Severity:   "warn",
			Summary:    "Workflow execution failed.",
			NextAction: "Inspect the workflow definition and runtime error before retrying.",
			DurationMS: duration,
			ErrorCode:  "workflow_execution_failed",
			Details:    map[string]any{"workflow_name": name, "namespace": namespace, "error": err.Error()},
		})
		return row, nil
	}
	row["status"] = "ok"
	row["result_json"] = mustMarshalString(out)
	h.service.recordRuntimeEvent(ctx, runtimeEvent{
		Phase:      "workflow",
		Status:     runtimeStatusReady,
		Severity:   "info",
		Summary:    "Workflow execution completed.",
		NextAction: "Continue with the workflow result; query gj_runtime again if follow-up state is uncertain.",
		DurationMS: duration,
		Details:    map[string]any{"workflow_name": name, "namespace": namespace},
	})
	return row, nil
}

func (h controlPlaneGraphQL) mutateConfig(ctx context.Context, root core.ManagedMutationRoot) (map[string]any, error) {
	if !h.service.conf.MCP.AllowConfigUpdates {
		return nil, fmt.Errorf("config updates are not allowed; enable mcp.allow_config_updates; next_action: query_catalog(search: \"enable config updates gj_config.update\")")
	}
	switch root.Operation {
	case "update", "upsert":
	default:
		return nil, fmt.Errorf("gj_config supports update and upsert mutations only")
	}
	ms := &mcpServer{service: h.service, ctx: ctx}
	res, err := ms.handleUpdateCurrentConfig(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: root.Input}})
	payload := mcpResultPayload(res, err)
	if payload["success"] == false {
		return nil, fmt.Errorf("%s", firstPayloadError(payload))
	}
	mode, _ := payload["mode"].(string)
	row := h.configRow()
	overlayKeys := []string{"valid", "applied", "mode", "preview_id", "expires_at", "change_summary_json", "findings_json", "errors_json", "scope", "reload_mode", "reload_strategy"}
	if mode == "preview" {
		overlayKeys = append(overlayKeys, "catalog_revision")
	}
	for _, key := range overlayKeys {
		if value, ok := payload[key]; ok {
			row[key] = value
		}
	}
	return row, nil
}

func firstPayloadError(payload map[string]any) string {
	for _, item := range anySlice(payload["errors"]) {
		if s := strings.TrimSpace(fmt.Sprint(item)); s != "" {
			return s
		}
	}
	if s := strings.TrimSpace(fmt.Sprint(payload["message"])); s != "" {
		return s
	}
	return "config update failed"
}

func (h controlPlaneGraphQL) reloadSchema(root core.ManagedMutationRoot) (map[string]any, error) {
	if !h.service.conf.MCP.AllowSchemaReload {
		return nil, fmt.Errorf("schema reload is not allowed; enable mcp.allow_schema_reload")
	}
	row := map[string]any{"id": stableID("schema_reload", time.Now().String())}
	if h.service.gj == nil {
		row["reloaded"] = false
		row["error"] = "GraphJin engine is not initialized"
		h.service.recordRuntimeEvent(context.Background(), runtimeEvent{
			Phase:      "schema",
			Status:     runtimeStatusFailed,
			Severity:   "warn",
			Summary:    "GraphQL schema reload was blocked because GraphJin is not initialized.",
			NextAction: "Fix configuration or database connectivity before retrying schema reload.",
			ErrorCode:  "schema_reload_blocked",
		})
		return row, nil
	}
	database, _ := root.Input["database"].(string)
	reload, err := h.service.reloadSchema(context.Background(), database)
	if err != nil {
		errText := redactRuntimeError(err)
		row["reloaded"] = false
		row["error"] = errText
		h.service.recordRuntimeEvent(context.Background(), runtimeEvent{
			Phase:      "schema",
			Status:     runtimeStatusFailed,
			Severity:   "error",
			Summary:    "GraphQL schema reload failed.",
			NextAction: "Inspect database connectivity and schema discovery errors before retrying.",
			ErrorCode:  "schema_reload_failed",
			Details:    map[string]any{"error": errText, "database": strings.TrimSpace(database)},
		})
		return row, nil
	}
	row["reloaded"] = true
	row["reload_mode"] = reload.Mode
	if reload.Database != "" {
		row["database"] = reload.Database
	}
	if reload.CatalogRevision != "" {
		row["catalog_revision"] = reload.CatalogRevision
	}
	h.service.recordRuntimeEvent(context.Background(), runtimeEvent{
		Phase:           "schema",
		Status:          runtimeStatusReady,
		Severity:        "info",
		Summary:         "GraphQL schema reload completed.",
		NextAction:      "Refresh catalog-guided planning before using newly discovered tables or relationships.",
		DatabaseName:    reload.Database,
		CatalogRevision: reload.CatalogRevision,
		Details: map[string]any{
			"catalog_revision": reload.CatalogRevision,
			"database":         reload.Database,
			"reload_mode":      reload.Mode,
			"table_count":      len(reload.Tables),
		},
	})
	return row, nil
}

func (h controlPlaneGraphQL) schemaChangeSet(ctx context.Context, root core.ManagedMutationRoot) (map[string]any, error) {
	if !h.service.conf.MCP.AllowSchemaUpdates {
		return nil, fmt.Errorf("schema changes are not allowed; enable mcp.allow_schema_updates")
	}
	input := root.Input
	action := strings.ToLower(stringFrom(input, "action"))
	if action == "" {
		action = "preview"
	}
	args := map[string]any{
		"schema":      stringFrom(input, "schema"),
		"database":    stringFrom(input, "database"),
		"destructive": boolFrom(input, "destructive"),
	}
	ms := &mcpServer{service: h.service, ctx: ctx}
	var res *mcp.CallToolResult
	var err error
	if action == "apply" {
		res, err = ms.handleApplySchemaChanges(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: args}})
	} else {
		res, err = ms.handlePreviewSchemaChanges(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: args}})
	}
	payload := mcpResultPayload(res, err)
	row := map[string]any{
		"id":           stableID("schema_change", action, args["database"], args["schema"]),
		"action":       action,
		"schema":       args["schema"],
		"database":     args["database"],
		"destructive":  args["destructive"],
		"preview_json": mustMarshalString(payload),
		"applied":      action == "apply" && payload["success"] != false,
		"errors_json":  mustMarshalString(payload["errors"]),
	}
	if row["applied"] == true {
		h.service.markCatalogChanged("schema change set")
	}
	if snap, err := h.service.catalogSnapshot(); err == nil {
		row["catalog_revision"] = snap.Revision
	}
	return row, nil
}

func (h controlPlaneGraphQL) validateQueryWhere(root core.ManagedMutationRoot) (map[string]any, error) {
	input := root.Input
	table := stringFrom(input, "table")
	database := stringFrom(input, "database")
	rawWhere := input["where"]
	if table == "" {
		return nil, fmt.Errorf("table is required")
	}
	var schema *core.TableSchema
	var err error
	if database != "" {
		schema, err = h.service.gj.GetTableSchemaForDatabase(database, table)
	} else {
		schema, err = h.service.gj.GetTableSchema(table)
	}
	if err != nil {
		return nil, err
	}
	ms := &mcpServer{service: h.service}
	compileResult := ms.validateWhereClauseByCompilation(table, database, rawWhere, schema)
	if compileResult.ParseErr != nil {
		return map[string]any{"valid": false, "errors_json": mustMarshalString([]string{compileResult.ParseErr.Error()})}, nil
	}
	columnTypes := make(map[string]core.ColumnInfo)
	for _, col := range schema.Columns {
		columnTypes[col.Name] = col
	}
	var errors []WhereValidationError
	if compileResult.WhereData != nil {
		errors = validateWhereClause(compileResult.WhereData, columnTypes, "")
	}
	for _, compilerErr := range compileResult.CompilerErrors {
		errors = append(errors, WhereValidationError{
			Path:    "",
			Error:   "compiler_error",
			Message: compilerErr,
		})
	}
	if errors == nil {
		errors = []WhereValidationError{}
	}
	return map[string]any{
		"id":            stableID("validate", table, compileResult.WhereLiteral),
		"table":         table,
		"database":      database,
		"where":         compileResult.WhereData,
		"valid":         len(errors) == 0,
		"errors_json":   mustMarshalString(errors),
		"warnings_json": mustMarshalString(compileResult.Warnings),
	}, nil
}

func (h controlPlaneGraphQL) repairQuery(root core.ManagedMutationRoot) map[string]any {
	input := root.Input
	query := stringFrom(input, "query")
	errText := stringFrom(input, "error")
	ms := &mcpServer{service: h.service}
	repair := buildFixQueryErrorRepair(query, errText, ms.analyticsModeOn())
	return map[string]any{
		"id":                   stableID("repair", query, errText),
		"query":                query,
		"error":                errText,
		"kind":                 repair.Kind,
		"diagnosis":            repair.Diagnosis,
		"fixed_query":          repair.RepairedQuery,
		"explanation_json":     mustMarshalString(repair),
		"follow_up_tools_json": mustMarshalString(repair.FollowUpTools),
	}
}

func structRows[T any](items []T) []map[string]any {
	rows := make([]map[string]any, 0, len(items))
	for _, item := range items {
		var row map[string]any
		data, err := json.Marshal(item)
		if err != nil {
			continue
		}
		if err := json.Unmarshal(data, &row); err != nil {
			continue
		}
		rows = append(rows, row)
	}
	return rows
}

func applyManagedQuery(rows []map[string]any, root core.ManagedQueryRoot) []map[string]any {
	out := rows[:0]
	for _, row := range rows {
		if rowMatchesWhere(row, root.Where) {
			out = append(out, row)
		}
	}
	sortRows(out, root.OrderBy)
	start := root.Offset
	if start > len(out) {
		return nil
	}
	out = out[start:]
	if root.Limit > 0 && root.Limit < len(out) {
		out = out[:root.Limit]
	}
	return out
}

func rowMatchesWhere(row map[string]any, where map[string]interface{}) bool {
	if len(where) == 0 {
		return true
	}
	for key, value := range where {
		switch key {
		case "and":
			for _, item := range anySlice(value) {
				m, _ := item.(map[string]interface{})
				if !rowMatchesWhere(row, m) {
					return false
				}
			}
		case "or":
			ok := false
			for _, item := range anySlice(value) {
				m, _ := item.(map[string]interface{})
				if rowMatchesWhere(row, m) {
					ok = true
					break
				}
			}
			if !ok {
				return false
			}
		case "not":
			m, _ := value.(map[string]interface{})
			if rowMatchesWhere(row, m) {
				return false
			}
		case "search":
			needle := strings.ToLower(fmt.Sprint(value))
			if needle != "" && !strings.Contains(strings.ToLower(mustMarshalString(row)), needle) {
				return false
			}
		default:
			if !matchColumn(row[key], value) {
				return false
			}
		}
	}
	return true
}

func matchColumn(got any, cond any) bool {
	ops, ok := cond.(map[string]interface{})
	if !ok {
		return fmt.Sprint(got) == fmt.Sprint(cond)
	}
	for op, want := range ops {
		switch op {
		case "eq":
			if fmt.Sprint(got) != fmt.Sprint(want) {
				return false
			}
		case "neq":
			if fmt.Sprint(got) == fmt.Sprint(want) {
				return false
			}
		case "in":
			if !containsString(anySlice(want), fmt.Sprint(got)) {
				return false
			}
		case "nin":
			if containsString(anySlice(want), fmt.Sprint(got)) {
				return false
			}
		case "like", "ilike":
			if !likeMatch(fmt.Sprint(got), fmt.Sprint(want), op == "ilike") {
				return false
			}
		case "is_null":
			isNull := got == nil || fmt.Sprint(got) == ""
			if isNull != boolValue(want) {
				return false
			}
		}
	}
	return true
}

func splitSearchWhere(where map[string]interface{}) (string, map[string]any) {
	if len(where) == 0 {
		return "", nil
	}
	if v, ok := where["search"]; ok {
		out := cloneWhere(where)
		delete(out, "search")
		return fmt.Sprint(v), out
	}
	if items, ok := where["and"]; ok {
		var search string
		var rest []any
		for _, item := range anySlice(items) {
			m, _ := item.(map[string]interface{})
			s, w := splitSearchWhere(m)
			if s != "" && search == "" {
				search = s
			}
			if len(w) != 0 {
				rest = append(rest, w)
			}
		}
		if len(rest) == 0 {
			return search, nil
		}
		return search, map[string]any{"and": rest}
	}
	return "", where
}

func cloneWhere(in map[string]interface{}) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func sortRows(rows []map[string]any, orderBy []core.ManagedOrderBy) {
	if len(orderBy) == 0 {
		return
	}
	sort.SliceStable(rows, func(i, j int) bool {
		for _, ob := range orderBy {
			if iv, iok := numericRowScore(rows[i][ob.Column]); iok {
				if jv, jok := numericRowScore(rows[j][ob.Column]); jok && iv != jv {
					if strings.Contains(strings.ToLower(ob.Order), "desc") {
						return iv > jv
					}
					return iv < jv
				}
			}
			iv := fmt.Sprint(rows[i][ob.Column])
			jv := fmt.Sprint(rows[j][ob.Column])
			if iv == jv {
				continue
			}
			if strings.Contains(strings.ToLower(ob.Order), "desc") {
				return iv > jv
			}
			return iv < jv
		}
		return false
	})
}

func sortCatalogSearchRows(rows []map[string]any) {
	sort.SliceStable(rows, func(i, j int) bool {
		left := rowSearchScore(rows[i])
		right := rowSearchScore(rows[j])
		if left != right {
			return left > right
		}
		return fmt.Sprint(rows[i]["id"]) < fmt.Sprint(rows[j]["id"])
	})
}

func rowSearchScore(row map[string]any) float64 {
	if v, ok := row["search_rank"]; ok {
		if score, ok := numericRowScore(v); ok {
			return score
		}
	}
	if v, ok := row["score"]; ok {
		if score, ok := numericRowScore(v); ok {
			return score
		}
	}
	return 0
}

func numericRowScore(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

func filterRows(rows []map[string]any, fields []core.ManagedMutationField) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, filterRow(row, fields))
	}
	return out
}

func filterRow(row map[string]any, fields []core.ManagedMutationField) map[string]any {
	if len(fields) == 0 {
		return row
	}
	out := make(map[string]any, len(fields))
	for _, field := range fields {
		if field.Name == "__typename" {
			continue
		}
		if value, ok := row[field.Column]; ok {
			out[field.Name] = value
		} else {
			out[field.Name] = nil
		}
	}
	return out
}

func anySlice(v any) []any {
	switch x := v.(type) {
	case []any:
		return x
	default:
		return nil
	}
}

func containsString(items []any, value string) bool {
	for _, item := range items {
		if fmt.Sprint(item) == value {
			return true
		}
	}
	return false
}

func likeMatch(got, pattern string, insensitive bool) bool {
	if insensitive {
		got = strings.ToLower(got)
		pattern = strings.ToLower(pattern)
	}
	pattern = strings.Trim(pattern, "%")
	return strings.Contains(got, pattern)
}

func boolValue(v any) bool {
	b, _ := v.(bool)
	return b
}

func stringFrom(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	s, _ := m[key].(string)
	return strings.TrimSpace(s)
}

func boolFrom(m map[string]interface{}, key string) bool {
	if m == nil {
		return false
	}
	b, _ := m[key].(bool)
	return b
}

func stringFromWhere(where map[string]interface{}, key string) string {
	if where == nil {
		return ""
	}
	if cond, ok := where[key].(map[string]interface{}); ok {
		if v, ok := cond["eq"]; ok {
			return strings.TrimSpace(fmt.Sprint(v))
		}
	}
	if items, ok := where["and"]; ok {
		for _, item := range anySlice(items) {
			if m, ok := item.(map[string]interface{}); ok {
				if v := stringFromWhere(m, key); v != "" {
					return v
				}
			}
		}
	}
	return ""
}

func stringSliceFrom(v any) []string {
	var out []string
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for key := range x {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if s, ok := x[key].(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
	default:
		for _, item := range anySlice(v) {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
	}
	return out
}

func stableID(parts ...any) string {
	h := sha256.New()
	for _, part := range parts {
		_, _ = h.Write([]byte(fmt.Sprint(part)))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func mcpResultPayload(res *mcp.CallToolResult, err error) map[string]any {
	if err != nil {
		return map[string]any{"success": false, "errors": []string{err.Error()}}
	}
	if res == nil {
		return map[string]any{"success": false, "errors": []string{"empty MCP result"}}
	}
	if res.StructuredContent != nil {
		if m, ok := res.StructuredContent.(map[string]any); ok {
			return m
		}
	}
	return map[string]any{"success": !res.IsError, "errors": []string{}}
}
