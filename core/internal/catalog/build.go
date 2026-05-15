package catalog

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
)

func Build(snapshot *MetadataSnapshot, conf any) *Snapshot {
	return BuildWithOptions(snapshot, conf, BuildOptions{})
}

func BuildWithOptions(snapshot *MetadataSnapshot, conf any, opts BuildOptions) *Snapshot {
	out := &Snapshot{GeneratedAt: time.Now().UTC()}
	if snapshot == nil {
		snapshot = &MetadataSnapshot{}
	}
	opts = normalizeBuildOptions(opts)

	addEntryPoints(out, opts)
	sampleMode := catalogSampleMode(conf)
	addCapabilities(out, sampleMode, opts)
	addSchema(out, snapshot, sampleMode, opts)
	addLanguage(out, opts)
	addConfig(out, conf)
	addWorkflows(out, opts)
	sortSnapshot(out)
	out.SourceRevisions = SourceRevisions(snapshot, conf, opts)
	out.Revision = RevisionFromSourceRevisions(out.SourceRevisions)
	out.search = newSearchIndex(out)
	return out
}

func addEntryPoints(out *Snapshot, opts BuildOptions) {
	out.EntryPoints = append(out.EntryPoints,
		EntryPoint{
			ID:      "entrypoint.catalog.overview",
			Name:    "catalog_overview",
			Summary: "Start here to discover available data, GraphJin language features, config, policies, and safe next actions.",
			QueryJSON: mustJSON(map[string]any{
				"where": map[string]any{"kind": map[string]any{"in": []string{"database", "table", "directive", "operator_set", "query_pattern", "mutation_pattern", "capability"}}},
				"limit": 50,
			}),
			SuggestedNext: suggestedNextJSON(opts, "query_catalog", "get_catalog_card"),
		},
		EntryPoint{
			ID:      "entrypoint.catalog.schema",
			Name:    "discover_schema",
			Summary: "Find tables, key columns, relationships, row-shape hints, and code references.",
			QueryJSON: mustJSON(map[string]any{
				"where": map[string]any{"kind": map[string]any{"in": []string{"table", "column", "relationship"}}},
			}),
			SuggestedNext: suggestedNextJSON(opts, "query_catalog", "get_catalog_card", "validate_where_clause"),
		},
		EntryPoint{
			ID:      "entrypoint.catalog.language",
			Name:    "learn_graphjin_dsl",
			Summary: "Discover GraphJin directives, filter operators, mutation patterns, analytics directives, and common mistakes.",
			QueryJSON: mustJSON(map[string]any{
				"where": map[string]any{"kind": map[string]any{"in": []string{"directive", "operator_set", "query_pattern", "mutation_pattern", "deprecated_feature"}}},
			}),
			SuggestedNext: suggestedNextJSON(opts, "query_catalog", "fix_query_error"),
		},
		EntryPoint{
			ID:      "entrypoint.catalog.samples_profiles",
			Name:    "discover_samples_profiles",
			Summary: "Find whether sample/profile data is available without inlining row values into base catalog items.",
			QueryJSON: mustJSON(map[string]any{
				"where":  map[string]any{"kind": map[string]any{"in": []string{"table", "column"}}},
				"search": "sample profile",
			}),
			SuggestedNext: suggestedNextJSON(opts, "get_catalog_card", "query_catalog"),
		},
		EntryPoint{
			ID:      "entrypoint.catalog.workflows",
			Name:    "discover_workflows",
			Summary: "Find reusable JavaScript workflows before authoring new orchestration or scanning broad data.",
			QueryJSON: mustJSON(map[string]any{
				"where": map[string]any{"kind": map[string]any{"eq": "workflow"}},
			}),
			SuggestedNext: suggestedNextJSON(opts, "query_catalog", "get_catalog_card"),
		},
	)
}

func addCapabilities(out *Snapshot, sampleMode string, opts BuildOptions) {
	templates := capabilityTemplates(sampleMode)
	enabledTools := opts.EnabledTools
	enabled := make(map[string]struct{}, len(enabledTools))
	for _, tool := range enabledTools {
		if tool = strings.TrimSpace(tool); tool != "" {
			enabled[tool] = struct{}{}
		}
	}
	if len(enabled) == 0 && !opts.EnabledToolsKnown {
		for _, cap := range templates {
			enabled[cap.Name] = struct{}{}
		}
		enabled["catalog_samples_profiles"] = struct{}{}
	}

	caps := make([]Capability, 0, len(enabled)+1)
	caps = append(caps, Capability{ID: "capability.catalog_samples_profiles", Name: "catalog_samples_profiles", Kind: "catalog_read", Summary: "Sample/profile availability is cataloged, but row values stay on-demand by default so base catalog items remain cheap and safe.", SafetyJSON: mustJSON(map[string]any{"sample_mode": sampleMode, "base_items_inline_rows": sampleMode == "inline"})})
	caps = append(caps, systemGraphQLCapabilities(enabled)...)
	for _, name := range sortedStringSet(enabled) {
		if name == "catalog_samples_profiles" {
			continue
		}
		cap, ok := templates[name]
		if !ok {
			cap = Capability{ID: "capability." + name, Name: name, Kind: "mcp_tool", Summary: "MCP tool is enabled on this GraphJin server.", SafetyJSON: mustJSON(map[string]any{"enabled": true})}
		}
		caps = append(caps, cap)
	}
	out.Capabilities = append(out.Capabilities, caps...)
	for _, cap := range caps {
		if !catalogCapabilityHasCard(cap) {
			continue
		}
		out.Cards = append(out.Cards, Card{
			ID:            cap.ID,
			Kind:          "capability",
			Title:         cap.Name,
			Summary:       cap.Summary,
			Source:        "core.catalog.capability",
			RiskLevel:     "low",
			Confidence:    "high",
			SuggestedNext: suggestedNextJSON(opts, "get_catalog_card"),
		})
		out.Nodes = append(out.Nodes, Node{ID: cap.ID, Kind: "capability", Name: cap.Name, Summary: cap.Summary, CardID: cap.ID})
	}
}

func catalogCapabilityHasCard(cap Capability) bool {
	switch cap.Kind {
	case "catalog_read", "validation", "repair":
		return true
	default:
		return false
	}
}

func systemGraphQLCapabilities(enabled map[string]struct{}) []Capability {
	has := func(name string) bool {
		_, ok := enabled[name]
		return ok
	}
	var caps []Capability
	if has("execute_workflow") {
		caps = append(caps, Capability{
			ID:      "capability.gj_workflow_execution.insert",
			Name:    "gj_workflow_execution.insert",
			Kind:    "graphql_execution",
			Summary: "Execute a saved workflow through the control-plane GraphQL API and return an ephemeral result row.",
			InputSchemaJSON: mustJSON(map[string]any{
				"graphql_mutation": `gj_workflow_execution(insert: { workflow_name: "...", variables: {...} })`,
				"required_fields":  []string{"workflow_name"},
				"optional_fields":  []string{"namespace", "variables"},
			}),
			OutputSchemaJSON: mustJSON(map[string]any{
				"fields":        []string{"id", "workflow_name", "namespace", "status", "result_json", "error", "duration_ms"},
				"mutation_only": true,
				"ephemeral":     true,
				"stores_runs":   false,
			}),
			SafetyJSON: mustJSON(map[string]any{"graphql_mutation": "gj_workflow_execution(insert)", "preferred_for_data_questions": true, "mutation_only": true, "ephemeral": true, "blocked_by": "read_only"}),
		})
	}
	if has("save_workflow") {
		caps = append(caps, Capability{ID: "capability.gj_workflow.write", Name: "gj_workflow.insert_update_delete", Kind: "graphql_mutation", Summary: "Create, update, or delete reusable workflow definition files through GraphJin GraphQL.", SafetyJSON: mustJSON(map[string]any{"graphql_mutation": "gj_workflow(insert/update/delete)", "writes_files": true, "requires_config": "mcp.allow_workflow_updates"})})
	}
	if has("update_current_config") {
		caps = append(caps, Capability{
			ID:      "capability.gj_config.update",
			Name:    "gj_config.update",
			Kind:    "graphql_mutation",
			Summary: "Update the active GraphJin configuration singleton through GraphQL.",
			InputSchemaJSON: mustJSON(map[string]any{
				"graphql_mutation": `gj_config(id: "current", update: { ... })`,
				"singleton_id":     "current",
				"update_fields": []string{
					"sources",
					"databases",
					"relationships",
					"tables",
					"roles",
					"blocklist",
					"functions",
					"resolvers",
					"mcp",
				},
				"mcp_fields": []string{
					"allow_workflow_updates",
					"allow_workflow_execution",
					"allow_config_updates",
					"allow_schema_reload",
					"allow_schema_updates",
					"allow_dev_tools",
					"allow_raw_queries",
					"legacy_discovery",
				},
				"errors": "Invalid updates return normal GraphQL errors; there is no dry_run, mode, patch, valid, or applied field.",
			}),
			OutputSchemaJSON: mustJSON(map[string]any{
				"root":   "gj_config",
				"fields": []string{"id", "source_mode", "config_path", "active_database", "sources", "databases", "relationships", "tables", "roles", "blocklist", "functions", "resolvers", "mcp", "config_json", "redacted_paths", "updated_at", "catalog_revision"},
			}),
			SafetyJSON: mustJSON(map[string]any{"graphql_mutation": `gj_config(id: "current", update: ...)`, "requires_config": "mcp.allow_config_updates", "serialized_by": "service config mutex"}),
		})
	}
	return caps
}

func capabilityTemplates(sampleMode string) map[string]Capability {
	return map[string]Capability{
		"query_catalog":            {ID: "capability.query_catalog", Name: "query_catalog", Kind: "catalog_read", Summary: "Search the AI-first GraphJin catalog for schema, language, config, workflow, and capability items.", SafetyJSON: mustJSON(map[string]any{"read_only": true})},
		"get_catalog_card":         {ID: "capability.get_catalog_card", Name: "get_catalog_card", Kind: "catalog_read", Summary: "Fetch a single catalog item with rich details and nearby edges.", SafetyJSON: mustJSON(map[string]any{"read_only": true})},
		"get_catalog_entrypoints":  {ID: "capability.get_catalog_entrypoints", Name: "get_catalog_entrypoints", Kind: "catalog_read", Summary: "List recommended catalog entrypoints for discovery.", SafetyJSON: mustJSON(map[string]any{"read_only": true})},
		"get_catalog_capabilities": {ID: "capability.get_catalog_capabilities", Name: "get_catalog_capabilities", Kind: "catalog_read", Summary: "List catalog-described GraphJin capabilities and safety notes.", SafetyJSON: mustJSON(map[string]any{"read_only": true})},
		"validate_where_clause":    {ID: "capability.validate_where_clause", Name: "validate_where_clause", Kind: "validation", Summary: "Validate a where clause against table columns and GraphJin operators.", SafetyJSON: mustJSON(map[string]any{"read_only": true})},
		"fix_query_error":          {ID: "capability.fix_query_error", Name: "fix_query_error", Kind: "repair", Summary: "Classify and repair GraphJin query errors using catalog language/schema context.", SafetyJSON: mustJSON(map[string]any{"read_only": true})},
		"execute_graphql":          {ID: "capability.execute_graphql", Name: "execute_graphql", Kind: "execution", Summary: "Execute raw GraphJin GraphQL when enabled by MCP config.", SafetyJSON: mustJSON(map[string]any{"requires_config": "mcp.allow_raw_queries"})},
		"execute_saved_query":      {ID: "capability.execute_saved_query", Name: "execute_saved_query", Kind: "execution", Summary: "Execute a saved allow-list query by name.", SafetyJSON: mustJSON(map[string]any{"prefers_saved_queries": true})},
		"execute_workflow":         {ID: "capability.execute_workflow", Name: "execute_workflow", Kind: "execution", Summary: "Execute a JavaScript workflow through the legacy MCP compatibility tool.", SafetyJSON: mustJSON(map[string]any{"preferred_for_data_questions": true, "requires_config": "mcp.legacy_discovery && mcp.allow_workflow_execution"})},
		"save_workflow":            {ID: "capability.save_workflow", Name: "save_workflow", Kind: "mutation", Summary: "Save a reusable JavaScript workflow when workflow updates are enabled.", SafetyJSON: mustJSON(map[string]any{"writes_files": true})},
		"get_js_runtime_api":       {ID: "capability.get_js_runtime_api", Name: "get_js_runtime_api", Kind: "catalog_read", Summary: "Describe the JavaScript workflow runtime API and callable tools.", SafetyJSON: mustJSON(map[string]any{"read_only": true, "workflow_runtime": "goja", "sample_mode": sampleMode})},
	}
}

func addSchema(out *Snapshot, snapshot *MetadataSnapshot, sampleMode string, opts BuildOptions) {
	for _, db := range snapshot.Databases {
		id := "database:" + db.Name
		summary := fmt.Sprintf("%s database", db.Type)
		if db.IsDefault {
			summary += " (default)"
		}
		if db.ReadOnly {
			summary += ", read-only"
		}
		out.Cards = append(out.Cards, Card{
			ID:           id,
			Kind:         "database",
			Title:        db.Name,
			Summary:      summary,
			DatabaseName: db.Name,
			Source:       "core.metadata",
			RiskLevel:    riskForReadOnly(db.ReadOnly),
			Confidence:   "high",
			EvidenceJSON: mustJSON(db),
		})
		out.Nodes = append(out.Nodes, Node{ID: id, Kind: "database", Name: db.Name, Summary: summary, CardID: id})
	}

	columnsByTable := make(map[string][]MetadataColumn)
	for _, c := range snapshot.Columns {
		columnsByTable[c.TableID] = append(columnsByTable[c.TableID], c)
	}
	for _, cols := range columnsByTable {
		sort.Slice(cols, func(i, j int) bool { return cols[i].Ordinal < cols[j].Ordinal })
	}

	for _, t := range snapshot.Tables {
		cardID := "table:" + t.ID
		nodeID := "node:" + cardID
		keyCols := keyColumns(columnsByTable[t.ID])
		summary := tableSummary(t, keyCols)
		out.Cards = append(out.Cards, Card{
			ID:            cardID,
			Kind:          "table",
			Title:         qualifiedName(t.DatabaseName, t.SchemaName, t.TableName),
			Summary:       summary,
			DatabaseName:  t.DatabaseName,
			SchemaName:    t.SchemaName,
			TableName:     t.TableName,
			Source:        "core.metadata",
			RiskLevel:     "low",
			Confidence:    "high",
			EvidenceJSON:  mustJSON(t),
			ExamplesJSON:  tableExamples(t, keyCols),
			SuggestedNext: suggestedNextJSON(opts, "get_catalog_card", "validate_where_clause"),
			DetailRef:     cardID,
		})
		out.Details = append(out.Details, CardDetail{
			ID:       cardID + ":columns",
			CardID:   cardID,
			Section:  "key_columns",
			Content:  "Primary keys, foreign keys, indexed columns, date/status/numeric columns, and likely sensitive columns are highlighted for model planning.",
			DataJSON: mustJSON(keyCols),
		})
		out.Details = append(out.Details, CardDetail{
			ID:      cardID + ":samples_profile",
			CardID:  cardID,
			Section: "samples_profile",
			Content: "Base catalog items do not inline live row values. Sample/profile data is tracked as availability and should be requested only when needed.",
			DataJSON: mustJSON(map[string]any{
				"mode":                          sampleMode,
				"base_card_contains_row_values": sampleMode == "inline",
				"suggested_next":                suggestedNext(opts, "get_catalog_card", "query_catalog"),
			}),
		})
		out.Nodes = append(out.Nodes, Node{ID: nodeID, Kind: "table", Name: t.TableName, Summary: summary, CardID: cardID})
		out.Edges = append(out.Edges, Edge{ID: "edge:database:" + t.DatabaseName + ":" + t.ID, FromID: "database:" + t.DatabaseName, ToID: nodeID, Kind: "contains", Summary: "Database contains table"})
	}

	for _, c := range snapshot.Columns {
		cardID := "column:" + c.ID
		nodeID := "node:" + cardID
		sensitive, sensitivity := columnSensitivity(c)
		summary := columnSummary(c, sensitive, sensitivity)
		out.Cards = append(out.Cards, Card{
			ID:            cardID,
			Kind:          "column",
			Title:         qualifiedName(c.DatabaseName, c.SchemaName, c.TableName) + "." + c.ColumnName,
			Summary:       summary,
			DatabaseName:  c.DatabaseName,
			SchemaName:    c.SchemaName,
			TableName:     c.TableName,
			ColumnName:    c.ColumnName,
			Source:        "core.metadata",
			RiskLevel:     riskForSensitive(sensitive),
			Confidence:    "medium",
			Sensitive:     sensitive,
			Sensitivity:   sensitivity,
			EvidenceJSON:  mustJSON(c),
			ExamplesJSON:  columnExamples(c),
			SuggestedNext: suggestedNextJSON(opts, columnSuggestedNext(c)...),
			DetailRef:     cardID,
		})
		out.Nodes = append(out.Nodes, Node{ID: nodeID, Kind: "column", Name: c.ColumnName, Summary: summary, CardID: cardID})
		out.Edges = append(out.Edges, Edge{ID: "edge:table-column:" + c.ID, FromID: "node:table:" + c.TableID, ToID: nodeID, Kind: "has_column", Summary: "Table has column"})
	}

	for _, r := range snapshot.Relationships {
		cardID := "relationship:" + r.ID
		summary := fmt.Sprintf("%s.%s -> %s.%s", r.FromTableName, r.FromColumnName, r.ToTableName, r.ToColumnName)
		out.Cards = append(out.Cards, Card{
			ID:           cardID,
			Kind:         "relationship",
			Title:        summary,
			Summary:      "Relationship discovered from database metadata. Use it to plan nested GraphJin queries instead of guessing join paths.",
			DatabaseName: r.FromDatabaseName,
			SchemaName:   r.FromSchemaName,
			TableName:    r.FromTableName,
			ColumnName:   r.FromColumnName,
			Source:       valueOrDefault(r.Source, "core.metadata"),
			RiskLevel:    "low",
			Confidence:   "high",
			EvidenceJSON: mustJSON(r),
			ExamplesJSON: mustJSON([]string{relationshipExample(r)}),
		})
		out.Edges = append(out.Edges, Edge{ID: "edge:" + cardID, FromID: "node:column:" + r.FromColumnID, ToID: "node:column:" + r.ToColumnID, Kind: "references", Summary: summary})
	}

	for _, fn := range snapshot.Functions {
		cardID := "function:" + fn.ID
		out.Cards = append(out.Cards, Card{
			ID:           cardID,
			Kind:         "function",
			Title:        fn.Name,
			Summary:      functionSummary(fn),
			DatabaseName: fn.DatabaseName,
			SchemaName:   fn.SchemaName,
			Source:       "core.metadata",
			RiskLevel:    "low",
			Confidence:   "high",
			EvidenceJSON: mustJSON(fn),
		})
	}
}

func addLanguage(out *Snapshot, opts BuildOptions) {
	for _, f := range languageFeatures {
		cardID := "language:" + f.ID
		out.Cards = append(out.Cards, Card{
			ID:            cardID,
			Kind:          f.Kind,
			Title:         f.Name,
			Summary:       f.Summary,
			Source:        "core.catalog.language_registry",
			RiskLevel:     riskForFeature(f),
			Confidence:    "high",
			EvidenceJSON:  mustJSON(f),
			ExamplesJSON:  mustJSON(f.Examples),
			SuggestedNext: suggestedNextJSON(opts, f.SuggestedNext...),
			DetailRef:     cardID,
		})
		out.Details = append(out.Details, CardDetail{
			ID:       cardID + ":spec",
			CardID:   cardID,
			Section:  "feature_spec",
			Content:  f.DialectSupport,
			DataJSON: mustJSON(f),
		})
		out.Nodes = append(out.Nodes, Node{ID: "node:" + cardID, Kind: f.Kind, Name: f.Name, Summary: f.Summary, CardID: cardID})
	}
}

func addConfig(out *Snapshot, conf any) {
	fields := ConfigFields(conf)
	if len(fields) == 0 {
		return
	}
	sensitiveCount := 0
	for _, f := range fields {
		if f.Sensitive {
			sensitiveCount++
		}
	}
	cardID := "config:core"
	out.Cards = append(out.Cards, Card{
		ID:           cardID,
		Kind:         "config",
		Title:        "core config",
		Summary:      fmt.Sprintf("GraphJin core configuration with %d fields (%d sensitive/redacted).", len(fields), sensitiveCount),
		Source:       "core.config",
		RiskLevel:    "medium",
		Confidence:   "high",
		Sensitive:    sensitiveCount != 0,
		Sensitivity:  "mixed",
		EvidenceJSON: mustJSON(map[string]any{"field_count": len(fields), "sensitive_field_count": sensitiveCount}),
		DetailRef:    cardID,
	})
	out.Details = append(out.Details, CardDetail{
		ID:       cardID + ":fields",
		CardID:   cardID,
		Section:  "redacted_fields",
		Content:  "Sensitive values are represented as has_value plus sensitivity class and never include raw secret material.",
		DataJSON: mustJSON(fields),
	})
}

func addWorkflows(out *Snapshot, opts BuildOptions) {
	for _, wf := range opts.Workflows {
		if strings.TrimSpace(wf.Name) == "" {
			continue
		}
		cardID := "workflow:" + wf.Name
		summary := strings.TrimSpace(wf.Description)
		if summary == "" {
			summary = "Reusable JavaScript workflow."
		}
		evidence := workflowEvidence(wf)
		out.Cards = append(out.Cards, Card{
			ID:            cardID,
			Kind:          "workflow",
			Title:         wf.Name,
			Summary:       summary,
			Source:        "serv.workflow",
			RiskLevel:     "medium",
			Confidence:    workflowConfidence(wf),
			EvidenceJSON:  mustJSON(evidence),
			SuggestedNext: suggestedNextJSON(opts, "get_catalog_card", "query_catalog"),
			DetailRef:     cardID,
			CreatedAt:     wf.CreatedAt,
			UpdatedAt:     wf.UpdatedAt,
		})
		out.Details = append(out.Details, CardDetail{
			ID:      cardID + ":metadata",
			CardID:  cardID,
			Section: "workflow_metadata",
			Content: workflowDetailContent(wf),
			DataJSON: mustJSON(map[string]any{
				"name":            wf.Name,
				"description":     wf.Description,
				"tags":            wf.Tags,
				"variables":       wf.Variables,
				"path":            wf.Path,
				"source_hash":     wf.SourceHash,
				"runtime":         wf.Runtime,
				"timeout_seconds": wf.TimeoutSeconds,
				"created_at":      wf.CreatedAt,
				"updated_at":      wf.UpdatedAt,
			}),
		})
		out.Nodes = append(out.Nodes, Node{ID: "node:" + cardID, Kind: "workflow", Name: wf.Name, Summary: summary, CardID: cardID})
	}
}

func workflowDetailContent(wf Workflow) string {
	var parts []string
	parts = append(parts, "Workflow discovery exposes metadata, variables, path, lifecycle timestamps, and source hash. Full JavaScript source and execution instructions are intentionally not in catalog items.")
	if len(wf.Tags) != 0 {
		parts = append(parts, "Tags: "+strings.Join(wf.Tags, ", ")+".")
	}
	if len(wf.Variables) != 0 {
		var vars []string
		for _, v := range wf.Variables {
			label := v.Name
			if v.Type != "" {
				label += ":" + v.Type
			}
			if v.Description != "" {
				label += " " + v.Description
			}
			vars = append(vars, label)
		}
		parts = append(parts, "Variables: "+strings.Join(vars, ", ")+".")
	}
	return strings.Join(parts, " ")
}

func workflowEvidence(wf Workflow) map[string]any {
	return map[string]any{
		"name":            wf.Name,
		"description":     wf.Description,
		"tags":            wf.Tags,
		"variables":       wf.Variables,
		"path":            wf.Path,
		"source_hash":     wf.SourceHash,
		"runtime":         wf.Runtime,
		"timeout_seconds": wf.TimeoutSeconds,
		"created_at":      wf.CreatedAt,
		"updated_at":      wf.UpdatedAt,
	}
}

func workflowConfidence(wf Workflow) string {
	if strings.TrimSpace(wf.Description) == "" {
		return "medium"
	}
	return "high"
}

func suggestedNextJSON(opts BuildOptions, names ...string) string {
	return mustJSON(suggestedNext(opts, names...))
}

func suggestedNext(opts BuildOptions, names ...string) []string {
	out := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		if !toolAvailable(opts, name) {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

func toolAvailable(opts BuildOptions, name string) bool {
	if !opts.EnabledToolsKnown {
		return true
	}
	for _, tool := range opts.EnabledTools {
		if tool == name {
			return true
		}
	}
	return false
}

func sortSnapshot(out *Snapshot) {
	sort.Slice(out.Cards, func(i, j int) bool { return out.Cards[i].ID < out.Cards[j].ID })
	sort.Slice(out.Details, func(i, j int) bool { return out.Details[i].ID < out.Details[j].ID })
	sort.Slice(out.Nodes, func(i, j int) bool { return out.Nodes[i].ID < out.Nodes[j].ID })
	sort.Slice(out.Edges, func(i, j int) bool { return out.Edges[i].ID < out.Edges[j].ID })
	sort.Slice(out.EntryPoints, func(i, j int) bool { return out.EntryPoints[i].ID < out.EntryPoints[j].ID })
	sort.Slice(out.Capabilities, func(i, j int) bool { return out.Capabilities[i].ID < out.Capabilities[j].ID })
}

func normalizeBuildOptions(opts BuildOptions) BuildOptions {
	opts.EnabledTools = sortedStrings(opts.EnabledTools)
	if opts.WorkflowRuntime == "" {
		opts.WorkflowRuntime = "goja"
	}
	for i := range opts.Workflows {
		opts.Workflows[i].Name = strings.TrimSpace(opts.Workflows[i].Name)
		opts.Workflows[i].Description = strings.TrimSpace(opts.Workflows[i].Description)
		opts.Workflows[i].Path = strings.TrimSpace(opts.Workflows[i].Path)
		opts.Workflows[i].SourceHash = strings.TrimSpace(opts.Workflows[i].SourceHash)
		opts.Workflows[i].CreatedAt = strings.TrimSpace(opts.Workflows[i].CreatedAt)
		opts.Workflows[i].UpdatedAt = strings.TrimSpace(opts.Workflows[i].UpdatedAt)
		if strings.TrimSpace(opts.Workflows[i].Runtime) == "" {
			opts.Workflows[i].Runtime = opts.WorkflowRuntime
		}
		if opts.Workflows[i].TimeoutSeconds <= 0 {
			opts.Workflows[i].TimeoutSeconds = opts.WorkflowTimeoutSeconds
		}
		opts.Workflows[i].Tags = sortedStrings(opts.Workflows[i].Tags)
		sort.Slice(opts.Workflows[i].Variables, func(a, b int) bool {
			return opts.Workflows[i].Variables[a].Name < opts.Workflows[i].Variables[b].Name
		})
	}
	sort.Slice(opts.Workflows, func(i, j int) bool { return opts.Workflows[i].Name < opts.Workflows[j].Name })
	return opts
}

func sortedStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func sortedStringSet(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func keyColumns(cols []MetadataColumn) []MetadataColumn {
	var out []MetadataColumn
	for _, c := range cols {
		if c.PrimaryKey || c.UniqueKey || c.Indexed || c.IndexName != "" || looksForeignKey(c) || looksDateColumn(c) || looksMetricColumn(c) || looksStatusColumn(c) {
			out = append(out, c)
		}
	}
	if len(out) > 12 {
		out = out[:12]
	}
	return out
}

func tableSummary(t MetadataTable, keyCols []MetadataColumn) string {
	parts := []string{fmt.Sprintf("%d columns", t.ColumnCount)}
	if t.PrimaryKey != "" {
		parts = append(parts, "primary key "+t.PrimaryKey)
	}
	if len(keyCols) != 0 {
		names := make([]string, 0, len(keyCols))
		for _, c := range keyCols {
			names = append(names, c.ColumnName)
		}
		parts = append(parts, "key columns: "+strings.Join(names, ", "))
	}
	if t.Comment != "" {
		parts = append(parts, t.Comment)
	}
	return strings.Join(parts, "; ")
}

func columnSummary(c MetadataColumn, sensitive bool, sensitivity string) string {
	parts := []string{c.Type}
	if c.Array {
		parts = append(parts, "array")
	}
	if c.PrimaryKey {
		parts = append(parts, "primary key")
	}
	if c.UniqueKey {
		parts = append(parts, "unique")
	}
	if c.Indexed || c.IndexName != "" {
		parts = append(parts, "indexed")
	}
	if c.NotNull {
		parts = append(parts, "not null")
	}
	if sensitive {
		parts = append(parts, "sensitive:"+sensitivity)
	}
	return strings.Join(parts, ", ")
}

func tableExamples(t MetadataTable, keyCols []MetadataColumn) string {
	fields := []string{"id"}
	for _, c := range keyCols {
		if len(fields) >= 4 {
			break
		}
		if c.ColumnName != "id" {
			fields = append(fields, c.ColumnName)
		}
	}
	return mustJSON([]string{fmt.Sprintf("{ %s(limit: 10) { %s } }", t.TableName, strings.Join(fields, " "))})
}

func columnExamples(c MetadataColumn) string {
	var examples []string
	switch {
	case looksMetricColumn(c):
		examples = append(examples, fmt.Sprintf("{ %s { sum_%s avg_%s } }", c.TableName, c.ColumnName, c.ColumnName))
	case looksDateColumn(c):
		examples = append(examples, fmt.Sprintf(`where: { %s: { gte: $from, lt: $to } }`, c.ColumnName))
	case looksStatusColumn(c):
		examples = append(examples, fmt.Sprintf(`where: { %s: { eq: "<status>" } }`, c.ColumnName))
	default:
		examples = append(examples, fmt.Sprintf("{ %s(limit: 10) { %s } }", c.TableName, c.ColumnName))
	}
	return mustJSON(examples)
}

func columnSuggestedNext(c MetadataColumn) []string {
	if looksMetricColumn(c) || looksDateColumn(c) || looksStatusColumn(c) {
		return []string{"get_catalog_card", "validate_where_clause"}
	}
	return []string{"get_catalog_card", "query_catalog"}
}

func relationshipExample(r MetadataRelationship) string {
	return fmt.Sprintf("{ %s { %s { %s } } }", r.FromTableName, r.ToTableName, r.ToColumnName)
}

func functionSummary(fn MetadataFunction) string {
	if fn.Aggregate {
		return "Aggregate function returning " + valueOrDefault(fn.ReturnType, "unknown")
	}
	return "Function returning " + valueOrDefault(fn.ReturnType, "unknown")
}

func columnSensitivity(c MetadataColumn) (bool, string) {
	name := strings.ToLower(c.ColumnName)
	switch {
	case strings.Contains(name, "password"), strings.Contains(name, "secret"):
		return true, "secret"
	case strings.Contains(name, "token"), strings.Contains(name, "api_key"):
		return true, "token"
	case strings.Contains(name, "email"), strings.Contains(name, "phone"):
		return true, "pii"
	default:
		return false, ""
	}
}

func looksForeignKey(c MetadataColumn) bool {
	name := strings.ToLower(c.ColumnName)
	return strings.HasSuffix(name, "_id") || strings.HasSuffix(name, "id") && name != "id"
}

func looksDateColumn(c MetadataColumn) bool {
	name := strings.ToLower(c.ColumnName + " " + c.Type)
	return strings.Contains(name, "date") || strings.Contains(name, "time") || strings.Contains(name, "created_at") || strings.Contains(name, "updated_at")
}

func looksMetricColumn(c MetadataColumn) bool {
	t := strings.ToLower(c.Type)
	if !(strings.Contains(t, "int") || strings.Contains(t, "numeric") || strings.Contains(t, "decimal") || strings.Contains(t, "float") || strings.Contains(t, "double") || strings.Contains(t, "money") || strings.Contains(t, "number")) {
		return false
	}
	name := strings.ToLower(c.ColumnName)
	return strings.Contains(name, "amount") || strings.Contains(name, "total") || strings.Contains(name, "price") || strings.Contains(name, "qty") || strings.Contains(name, "quantity") || strings.Contains(name, "count") || strings.Contains(name, "score")
}

func looksStatusColumn(c MetadataColumn) bool {
	name := strings.ToLower(c.ColumnName)
	return strings.Contains(name, "status") || strings.Contains(name, "state") || strings.Contains(name, "type") || strings.Contains(name, "category")
}

func riskForReadOnly(readOnly bool) string {
	if readOnly {
		return "low"
	}
	return "medium"
}

func riskForSensitive(sensitive bool) string {
	if sensitive {
		return "high"
	}
	return "low"
}

func riskForFeature(f Feature) string {
	if f.Kind == "deprecated_feature" {
		return "medium"
	}
	return "low"
}

func catalogSampleMode(conf any) string {
	const fallback = "on_demand"
	if conf == nil {
		return fallback
	}

	rv := reflect.ValueOf(conf)
	for rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return fallback
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return fallback
	}

	catalog := rv.FieldByName("Catalog")
	if !catalog.IsValid() {
		return fallback
	}
	for catalog.Kind() == reflect.Pointer || catalog.Kind() == reflect.Interface {
		if catalog.IsNil() {
			return fallback
		}
		catalog = catalog.Elem()
	}
	if catalog.Kind() != reflect.Struct {
		return fallback
	}
	samples := catalog.FieldByName("Samples")
	if !samples.IsValid() || samples.Kind() != reflect.String {
		return fallback
	}
	mode := strings.TrimSpace(samples.String())
	if mode == "" {
		return fallback
	}
	return mode
}

func qualifiedName(database, schema, table string) string {
	parts := make([]string, 0, 3)
	for _, part := range []string{database, schema, table} {
		if strings.TrimSpace(part) != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, ".")
}

func valueOrDefault(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func mustJSON(v any) string {
	if v == nil {
		return ""
	}
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(data)
}
