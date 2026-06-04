package catalog

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/dosco/graphjin/core/v3/sourcecap"
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
	addHelp(out, opts)
	addSources(out, opts)
	sampleMode := catalogSampleMode(conf)
	addCapabilities(out, sampleMode, opts)
	addSchema(out, snapshot, sampleMode, opts)
	addLanguage(out, opts)
	addConfig(out, conf)
	addWorkflows(out, opts)
	addFragments(out, snapshot, opts)
	addSavedQueries(out, opts)
	sortSnapshot(out)
	out.SourceRevisions = SourceRevisions(snapshot, conf, opts)
	out.Revision = RevisionFromSourceRevisions(out.SourceRevisions)
	out.search = newSearchIndex(out)
	return out
}

func addSources(out *Snapshot, opts BuildOptions) {
	for _, source := range opts.Sources {
		name := strings.TrimSpace(source.Name)
		kind := strings.TrimSpace(source.Kind)
		if name == "" || kind == "" {
			continue
		}
		canonicalKind, err := sourcecap.CanonicalKind(kind)
		if err == nil {
			kind = canonicalKind
		}
		cardID := "source:" + name
		summary := fmt.Sprintf("%s source", kind)
		if source.Type != "" {
			summary = fmt.Sprintf("%s source (%s)", kind, source.Type)
		}
		if source.Default {
			summary += " default"
		}
		if source.ReadOnly {
			summary += " read-only"
		}
		details := map[string]any{
			"name":                   name,
			"source_kind":            kind,
			"type":                   source.Type,
			"default":                source.Default,
			"read_only":              source.ReadOnly,
			"capabilities":           source.Capabilities,
			"canonical_kinds":        sourcecap.Kinds(),
			"supported_capabilities": sourcecap.ValidKeys(kind),
			"capability_details":     sourceCapabilityDetails(kind),
		}
		out.Cards = append(out.Cards, Card{
			ID:               cardID,
			Kind:             "source",
			Title:            name,
			Summary:          summary,
			Source:           name,
			SourceKind:       kind,
			OwnerSource:      name,
			OwnerSourcesJSON: ownerSourcesJSON(name),
			RiskLevel:        riskForReadOnly(source.ReadOnly),
			Confidence:       "high",
			EvidenceJSON:     mustJSON(details),
			ExamplesJSON:     sourceExamples(source),
			SafetyJSON:       mustJSON(map[string]any{"capabilities": "Source capabilities grant authenticated user access only; anonymous access is controlled separately.", "read_only_blocks_mutation": source.ReadOnly}),
			SuggestedNext:    suggestedNextJSON(opts, "query_catalog"),
			DetailRef:        cardID,
		})
		out.Details = append(out.Details, CardDetail{
			ID:       cardID + ":capabilities",
			CardID:   cardID,
			Section:  "source_capabilities",
			Content:  "Source capabilities are the primary interface for enabling or blocking source, control-plane, and workflow surfaces.",
			DataJSON: mustJSON(details),
		})
		out.Nodes = append(out.Nodes, Node{ID: "node:" + cardID, Kind: "source", Name: name, Summary: summary, CardID: cardID})
	}
}

type helpTopic struct {
	Key      string
	Title    string
	Summary  string
	Guide    string
	Search   string
	Kinds    []string
	Examples []string
	Safety   map[string]any
	Next     []string
	Replaces []string
}

var mcpLegacyDiscoveryTools = []string{
	"get_catalog_entrypoints",
	"get_catalog_card",
	"get_catalog_capabilities",
	"get_query_syntax",
	"get_mutation_syntax",
	"get_discovery_schema",
	"get_table_sample",
	"get_workflow_guide",
	"get_schema_insights",
	"explore_relationships",
	"find_path",
	"list_saved_queries",
	"search_saved_queries",
	"get_saved_query",
	"list_fragments",
	"search_fragments",
	"get_fragment",
	"get_config_docs",
	"get_js_runtime_api",
	"write_query",
	"write_mutation",
	"fix_query_error",
	"execute_workflow",
}

var helpTopics = []helpTopic{
	{Key: "discovery", Title: "Discovery help", Summary: "Start here when you are unsure which catalog rows or GraphJin surface to inspect.", Guide: "Call graphql_help(for: \"discovery\") first, then use the returned topic routes. Use query_catalog(id: \"help:mcp_tools\") to see how removed legacy MCP discovery tools map into catalog rows.", Search: "catalog discovery schema workflow query security mcp tools legacy", Kinds: []string{"help", "entrypoint", "capability", "system_capability"}, Examples: []string{`graphql_help(for: "discovery")`, `query_catalog(id: "help:mcp_tools")`, `query_catalog(where: { kind: { eq: "table" } })`}, Next: []string{"query_catalog"}, Replaces: []string{"get_catalog_entrypoints", "get_workflow_guide", "get_discovery_schema"}},
	{Key: "mcp_tools", Title: "Sources-mode MCP tools help", Summary: "Learn the tiny sources-mode MCP surface and how old discovery tools moved into catalog/help rows.", Guide: "Sources-mode MCP keeps the prompt small: graphql_help routes the first call, query_catalog searches and fetches full detail by id, validate_where_clause checks filters, and execute_saved_query runs approved saved queries. When raw execution is explicitly enabled, execute_graphql is also available as an action tool. Removed discovery tools are represented by help, capability, saved_query, fragment, workflow, language, schema, and relationship catalog rows.", Search: "mcp tools legacy discovery get_query_syntax get_catalog_card get_js_runtime_api fix_query_error", Kinds: []string{"help", "capability", "system_capability", "entrypoint"}, Examples: []string{`graphql_help(for: "discovery")`, `query_catalog(id: "help:query")`, `query_catalog(where: { kind: { in: ["capability", "system_capability"] } })`}, Next: []string{"query_catalog"}, Replaces: mcpLegacyDiscoveryTools},
	{Key: "catalog", Title: "Catalog help", Summary: "Use gj_catalog/query_catalog for evidence-backed discovery and query_catalog(id) for one detailed item.", Guide: "Use query_catalog(search, where, order_by, limit) for discovery. Use query_catalog(id: \"...\") as the get_catalog_card replacement; it returns details_json, evidence_json, examples_json, safety_json, and edges_json.", Search: "catalog detail evidence examples edges safety get_catalog_card get_catalog_capabilities", Kinds: []string{"help", "entrypoint", "capability", "system_capability"}, Examples: []string{`query_catalog(search: "join orders customers", where: { kind: { eq: "relationship" } })`, `query_catalog(id: "help:catalog")`, `query_catalog(where: { kind: { in: ["capability", "system_capability"] } })`}, Next: []string{"query_catalog"}, Replaces: []string{"get_catalog_card", "get_catalog_capabilities"}},
	{Key: "schema", Title: "Schema help", Summary: "Discover tables, columns, relationships, functions, indexes, and row-shape hints from catalog rows.", Guide: "Use schema, table, column, relationship, and function catalog rows instead of broad schema-insight tools. Inspect details_json and evidence_json before choosing roots or joins.", Search: "schema table column relationship function index sample profile get_schema_insights get_discovery_schema", Kinds: []string{"help", "database", "table", "column", "relationship", "function"}, Examples: []string{`query_catalog(where: { kind: { in: ["table", "column", "relationship"] } })`, `query { gj_catalog(where: { kind: { eq: "table" } }) { id name summary details_json } }`}, Next: []string{"query_catalog", "validate_where_clause"}, Replaces: []string{"get_schema_insights", "get_discovery_schema"}},
	{Key: "tables", Title: "Table help", Summary: "Find table names, primary keys, row-shape hints, sample/profile availability, and related graph edges.", Guide: "Use table catalog rows instead of list_tables/describe_table. If values matter, inspect sample/profile guidance and then use permitted app-data queries or workflows.", Search: "tables primary key sample profile row count list_tables describe_table get_table_sample", Kinds: []string{"help", "table", "relationship", "column"}, Examples: []string{`query_catalog(where: { kind: { eq: "table" } })`, `query_catalog(id: "table:<database.schema.table>")`}, Next: []string{"query_catalog", "validate_where_clause"}, Replaces: []string{"list_tables", "describe_table", "get_table_sample"}},
	{Key: "columns", Title: "Column help", Summary: "Find column names, types, sensitivity notes, filter hints, indexes, and sample/profile availability.", Guide: "Use column catalog rows before selecting fields or writing filters. Validate type-sensitive where clauses with validate_where_clause.", Search: "columns fields types filters sensitive sample profile describe_table get_table_sample", Kinds: []string{"help", "column", "table", "operator_set"}, Examples: []string{`query_catalog(where: { kind: { eq: "column" }, table_name: { eq: "<table>" } })`, `validate_where_clause(table: "<table>", where: { id: { eq: 1 } })`}, Next: []string{"query_catalog", "validate_where_clause"}, Replaces: []string{"describe_table", "get_table_sample"}},
	{Key: "relationships", Title: "Relationship help", Summary: "Find safe join paths and @through hints before nesting related selectors.", Guide: "Use relationship catalog rows and edges_json instead of find_path/explore_relationships. Never infer joins from column names alone.", Search: "relationship join path foreign key through nested selector find_path explore_relationships", Kinds: []string{"help", "relationship", "directive", "table"}, Examples: []string{`query_catalog(search: "join orders customers", where: { kind: { eq: "relationship" } })`, `query_catalog(id: "<relationship_id>")`}, Next: []string{"query_catalog"}, Replaces: []string{"find_path", "explore_relationships"}},
	{Key: "query", Title: "Query help", Summary: "Learn GraphJin query DSL syntax, query patterns, aggregations, analytics directives, and common mistakes.", Guide: "Use query help and language catalog rows instead of get_query_syntax/write_query. Inspect examples_json before composing direct GraphQL or saved queries.", Search: "query syntax dsl aggregate analytics directive pattern distinct limit get_query_syntax write_query", Kinds: []string{"help", "directive", "operator_set", "query_pattern", "deprecated_feature"}, Examples: []string{`query_catalog(id: "help:query")`, `query_catalog(where: { kind: { in: ["directive", "operator_set", "query_pattern"] } })`}, Next: []string{"query_catalog", "validate_where_clause"}, Replaces: []string{"get_query_syntax", "write_query"}},
	{Key: "filters", Title: "Filter help", Summary: "Learn typed where operators and validate filters against table and column metadata.", Guide: "Use operator_set and column rows to choose operators. Use validate_where_clause before execution when filter values, arrays, nulls, or type coercion matter.", Search: "where filter operators eq in ilike is_null validate", Kinds: []string{"help", "operator_set", "column", "query_pattern"}, Examples: []string{`query_catalog(id: "help:filters")`, `validate_where_clause(table: "<table>", where: { id: { eq: 1 } })`}, Next: []string{"query_catalog", "validate_where_clause"}},
	{Key: "mutations", Title: "Mutation help", Summary: "Learn insert, update, upsert, delete, nested mutation, and code-source preview/apply patterns.", Guide: "Use mutation_pattern rows instead of get_mutation_syntax/write_mutation. Check gj_security before write-capable actions and prefer previews where available.", Search: "mutation insert update upsert delete code source preview apply get_mutation_syntax write_mutation", Kinds: []string{"help", "mutation_pattern", "operator_set", "system_capability"}, Examples: []string{`query_catalog(id: "help:mutations")`, `query_catalog(where: { kind: { eq: "mutation_pattern" } })`}, Next: []string{"query_catalog"}, Replaces: []string{"get_mutation_syntax", "write_mutation"}},
	{Key: "saved_queries", Title: "Saved query help", Summary: "Find allow-listed saved queries, inspect variable contracts, then run execute_saved_query.", Guide: "Use saved_query catalog rows instead of list/search/get saved-query tools. Inspect input_schema_json before execute_saved_query.", Search: "saved query allow list variables execute_saved_query list_saved_queries search_saved_queries get_saved_query", Kinds: []string{"help", "saved_query", "capability"}, Examples: []string{`query_catalog(where: { kind: { eq: "saved_query" } })`, `execute_saved_query(name: "<saved_query_name>", variables: {})`}, Next: []string{"query_catalog", "execute_saved_query"}, Replaces: []string{"list_saved_queries", "search_saved_queries", "get_saved_query"}},
	{Key: "fragments", Title: "Fragment help", Summary: "Discover reusable GraphQL fragments and import guidance before repeating field selections.", Guide: "Use fragment catalog rows instead of list/search/get fragment tools. Inspect examples_json and details_json for import guidance.", Search: "fragments graphql reusable field selection import list_fragments search_fragments get_fragment", Kinds: []string{"help", "fragment", "table"}, Examples: []string{`query_catalog(where: { kind: { eq: "fragment" } })`, `query_catalog(id: "help:fragments")`}, Next: []string{"query_catalog"}, Replaces: []string{"list_fragments", "search_fragments", "get_fragment"}},
	{Key: "workflows", Title: "Workflow help", Summary: "Discover reusable workflows, variable schemas, execution policy, and workflow control-plane guidance.", Guide: "Use workflow catalog rows instead of get_workflow_guide/execute_workflow. In sources mode, execute through gj_workflow_execution(insert) when policy allows it.", Search: "workflow reusable variables execution gj_workflow_execution get_workflow_guide execute_workflow", Kinds: []string{"help", "workflow", "system_capability", "capability"}, Examples: []string{`query_catalog(where: { kind: { eq: "workflow" } })`, `mutation { gj_workflow_execution(insert: { workflow_name: "...", variables: {} }) { status result_json error duration_ms } }`}, Next: []string{"query_catalog", "execute_saved_query"}, Replaces: []string{"get_workflow_guide", "execute_workflow"}},
	{Key: "workflow_runtime", Title: "Workflow runtime help", Summary: "Learn JavaScript workflow runtime concepts, callable tool guidance, and safety constraints.", Guide: "Use workflow runtime help rows instead of get_js_runtime_api. Runtime guidance is catalog-backed so it stays aligned with the active sources-mode tool surface.", Search: "javascript workflow runtime goja gj tools queryCatalog executeSavedQuery get_js_runtime_api", Kinds: []string{"help", "workflow", "capability", "system_capability"}, Examples: []string{`query_catalog(id: "help:workflow_runtime")`, `query_catalog(search: "workflow runtime goja tools")`}, Next: []string{"query_catalog"}, Replaces: []string{"get_js_runtime_api"}},
	{Key: "config", Title: "Config help", Summary: "Discover redacted configuration documentation, roles, permissions, sources, and safe config update guidance.", Guide: "Use config catalog rows instead of get_config_docs. Read gj_config only when permitted and check gj_security before config writes.", Search: "config docs sources roles permissions redacted update gj_config get_config_docs", Kinds: []string{"help", "config", "system_capability", "capability"}, Examples: []string{`query_catalog(id: "help:config")`, `query_catalog(search: "config docs", where: { kind: { in: ["help", "config", "system_capability"] } })`}, Next: []string{"query_catalog"}, Replaces: []string{"get_config_docs"}},
	{Key: "security", Title: "Security help", Summary: "Discover gj_security guidance, policy rows, findings, severity filters, and agentic safety expectations.", Guide: "Use security catalog rows to learn the gj_security query shapes, then query gj_security directly when the caller has permission. Normal agentic users may only see catalog-level safety guidance.", Search: "security findings policy posture gj_security agentic production", Kinds: []string{"help", "system_capability", "config"}, Examples: []string{`query_catalog(id: "help:security")`, `query_catalog(where: { kind: { eq: "system_capability" }, name: { eq: "gj_security.query" } })`}, Next: []string{"query_catalog"}},
	{Key: "runtime", Title: "Runtime help", Summary: "Use gj_runtime in agentic mode for compact current health, recent structured events, and suggested next actions.", Guide: "Query gj_runtime before workflow, config, or schema actions, after GraphJin errors, and when results suggest stale schema, disconnected databases, degraded Redis, reload, discovery, or catalog refresh problems. Treat gj_runtime as decision support, not audit history; when status is degraded, follow next_action before continuing.", Search: "runtime status health logs events system degraded redis schema reload discovery gj_runtime", Kinds: []string{"help", "system_capability"}, Examples: []string{`query_catalog(id: "help:runtime")`, `query_catalog(where: { kind: { eq: "system_capability" }, name: { eq: "gj_runtime.query" } })`, `query { gj_runtime(where: { kind: { in: ["status", "event"] } }, order_by: { created_at: desc }, limit: 20) { kind status severity summary next_action details_json } }`}, Next: []string{"query_catalog"}},
	{Key: "code", Title: "Code help", Summary: "Discover code-source catalog rows and safe source-edit preview/apply guidance when code sources are configured.", Guide: "Use code/source catalog rows before source intelligence or edit flows. Check gj_security and use preview/apply semantics for writes.", Search: "code source file symbol preview apply lock", Kinds: []string{"help", "mutation_pattern", "system_capability", "table", "column"}, Examples: []string{`query_catalog(id: "help:code")`, `query_catalog(search: "code source preview apply source edit")`}, Next: []string{"query_catalog"}},
	{Key: "errors", Title: "Error help", Summary: "Use errors[].extensions.graphjin_repair, then inspect relevant schema or language catalog rows before retrying.", Guide: "Use graphjin_repair in normal GraphJin errors instead of fix_query_error. Then inspect schema, relationship, operator, or query-pattern rows before retrying.", Search: "error repair graphjin_repair syntax table column relationship fix_query_error", Kinds: []string{"help", "deprecated_feature", "query_pattern", "operator_set", "system_capability"}, Examples: []string{`query_catalog(id: "help:errors")`, `query_catalog(search: "error repair syntax relationship")`}, Next: []string{"query_catalog", "validate_where_clause"}, Replaces: []string{"fix_query_error"}},
}

func addHelp(out *Snapshot, opts BuildOptions) {
	for _, topic := range helpTopics {
		cardID := "help:" + topic.Key
		queryJSON := helpQueryJSON(topic)
		guide := helpGuide(topic)
		safety := topic.Safety
		if safety == nil {
			safety = map[string]any{"read_only": true, "catalog_backed": true}
		}
		out.Cards = append(out.Cards, Card{
			ID:            cardID,
			Kind:          "help",
			Title:         topic.Title,
			Summary:       topic.Summary,
			Source:        "core.catalog.help",
			RiskLevel:     "low",
			Confidence:    "high",
			EvidenceJSON:  mustJSON(map[string]any{"for": topic.Key, "related_kinds": topic.Kinds, "replaces_legacy_tools": topic.Replaces}),
			ExamplesJSON:  mustJSON(topic.Examples),
			SuggestedNext: suggestedNextJSON(opts, topic.Next...),
			DetailRef:     cardID,
			QueryJSON:     queryJSON,
			SafetyJSON:    mustJSON(safety),
			GraphQLQuery:  helpGraphQLQuery(topic),
		})
		out.Details = append(out.Details, CardDetail{
			ID:       cardID + ":guide",
			CardID:   cardID,
			Section:  "graphql_help",
			Content:  guide,
			DataJSON: mustJSON(map[string]any{"for": topic.Key, "query": queryJSON, "examples": topic.Examples, "safety": safety, "replaces_legacy_tools": topic.Replaces, "recommended_detail_query": `query_catalog(id: "` + cardID + `")`}),
		})
		out.Nodes = append(out.Nodes, Node{ID: "node:" + cardID, Kind: "help", Name: topic.Key, Summary: topic.Summary, CardID: cardID})
	}
}

func helpGuide(topic helpTopic) string {
	parts := []string{topic.Summary}
	if topic.Guide != "" {
		parts = append(parts, topic.Guide)
	}
	parts = append(parts, `Detailed guidance is available with query_catalog(id: "help:`+topic.Key+`").`)
	if len(topic.Replaces) != 0 {
		parts = append(parts, "Replaces legacy MCP tools: "+strings.Join(topic.Replaces, ", ")+".")
	}
	return strings.Join(parts, " ")
}

func helpQueryJSON(topic helpTopic) string {
	return mustJSON(map[string]any{
		"search": topic.Search,
		"where":  map[string]any{"kind": map[string]any{"in": topic.Kinds}},
		"limit":  25,
	})
}

func helpGraphQLQuery(topic helpTopic) string {
	kinds := make([]string, 0, len(topic.Kinds))
	for _, kind := range topic.Kinds {
		kinds = append(kinds, fmt.Sprintf("%q", kind))
	}
	return fmt.Sprintf(`query { gj_catalog(search: %q, where: { kind: { in: [%s] } }, limit: 25) { id kind name summary details_json examples_json safety_json edges_json } }`, topic.Search, strings.Join(kinds, ", "))
}

func addEntryPoints(out *Snapshot, opts BuildOptions) {
	out.EntryPoints = append(out.EntryPoints,
		EntryPoint{
			ID:      "entrypoint.catalog.overview",
			Name:    "catalog_overview",
			Summary: "Start here to discover available data, GraphJin language features, config, policies, and safe next actions.",
			QueryJSON: mustJSON(map[string]any{
				"where": map[string]any{"kind": map[string]any{"in": []string{"database", "table", "fragment", "directive", "operator_set", "query_pattern", "mutation_pattern", "capability"}}},
				"limit": 50,
			}),
			SuggestedNext: suggestedNextJSON(opts, "query_catalog"),
		},
		EntryPoint{
			ID:      "entrypoint.catalog.schema",
			Name:    "discover_schema",
			Summary: "Find tables, key columns, relationships, row-shape hints, and code references.",
			QueryJSON: mustJSON(map[string]any{
				"where": map[string]any{"kind": map[string]any{"in": []string{"table", "column", "relationship"}}},
			}),
			SuggestedNext: suggestedNextJSON(opts, "query_catalog", "validate_where_clause"),
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
			SuggestedNext: suggestedNextJSON(opts, "query_catalog"),
		},
		EntryPoint{
			ID:      "entrypoint.catalog.fragments",
			Name:    "discover_fragments",
			Summary: "Find reusable GraphQL fragments before repeating field selections in new queries.",
			QueryJSON: mustJSON(map[string]any{
				"where": map[string]any{"kind": map[string]any{"eq": "fragment"}},
			}),
			SuggestedNext: suggestedNextJSON(opts, "query_catalog", "get_fragment"),
		},
		EntryPoint{
			ID:      "entrypoint.catalog.workflows",
			Name:    "discover_workflows",
			Summary: "Find reusable JavaScript workflows before authoring new orchestration or scanning broad data.",
			QueryJSON: mustJSON(map[string]any{
				"where": map[string]any{"kind": map[string]any{"eq": "workflow"}},
			}),
			SuggestedNext: suggestedNextJSON(opts, "query_catalog"),
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
			SuggestedNext: suggestedNextJSON(opts, "query_catalog"),
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
					"update_sources",
					"remove_sources",
					"databases",
					"relationships",
					"tables",
					"roles",
					"blocklist",
					"functions",
					"resolvers",
					"mcp",
				},
				"source_patch_semantics": "sources is replace-all. update_sources merge-patches by source name; existing source patches require name, new source patches require name and kind. Omitted fields are preserved, null clears fields, arrays replace, and nested objects merge. remove_sources deletes by source name.",
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
				"fields": []string{"id", "sources_used", "config_path", "active_database", "sources", "databases", "relationships", "tables", "roles", "blocklist", "functions", "resolvers", "mcp", "config_json", "redacted_paths", "updated_at", "catalog_revision"},
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
		"validate_where_clause":    {ID: "capability.validate_where_clause", Name: "validate_where_clause", Kind: "validation", Summary: "Validate a where clause with table/operator guidance and compile-only GraphJin verification.", SafetyJSON: mustJSON(map[string]any{"read_only": true})},
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
			ID:               id,
			Kind:             "database",
			Title:            db.Name,
			Summary:          summary,
			DatabaseName:     db.Name,
			Source:           "core.metadata",
			OwnerSource:      db.Name,
			OwnerSourcesJSON: ownerSourcesJSON(db.Name),
			RiskLevel:        riskForReadOnly(db.ReadOnly),
			Confidence:       "high",
			EvidenceJSON:     mustJSON(db),
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
			ID:               cardID,
			Kind:             "table",
			Title:            qualifiedName(t.DatabaseName, t.SchemaName, t.TableName),
			Summary:          summary,
			DatabaseName:     t.DatabaseName,
			SchemaName:       t.SchemaName,
			TableName:        t.TableName,
			Source:           "core.metadata",
			OwnerSource:      t.DatabaseName,
			OwnerSourcesJSON: ownerSourcesJSON(t.DatabaseName),
			RiskLevel:        "low",
			Confidence:       "high",
			EvidenceJSON:     mustJSON(t),
			ExamplesJSON:     tableExamples(t, keyCols),
			SuggestedNext:    suggestedNextJSON(opts, "query_catalog", "validate_where_clause"),
			DetailRef:        cardID,
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
				"suggested_next":                suggestedNext(opts, "query_catalog"),
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
			ID:               cardID,
			Kind:             "column",
			Title:            qualifiedName(c.DatabaseName, c.SchemaName, c.TableName) + "." + c.ColumnName,
			Summary:          summary,
			DatabaseName:     c.DatabaseName,
			SchemaName:       c.SchemaName,
			TableName:        c.TableName,
			ColumnName:       c.ColumnName,
			Source:           "core.metadata",
			OwnerSource:      c.DatabaseName,
			OwnerSourcesJSON: ownerSourcesJSON(c.DatabaseName),
			RiskLevel:        riskForSensitive(sensitive),
			Confidence:       "medium",
			Sensitive:        sensitive,
			Sensitivity:      sensitivity,
			EvidenceJSON:     mustJSON(c),
			ExamplesJSON:     columnExamples(c),
			SuggestedNext:    suggestedNextJSON(opts, columnSuggestedNext(c)...),
			DetailRef:        cardID,
		})
		out.Nodes = append(out.Nodes, Node{ID: nodeID, Kind: "column", Name: c.ColumnName, Summary: summary, CardID: cardID})
		out.Edges = append(out.Edges, Edge{ID: "edge:table-column:" + c.ID, FromID: "node:table:" + c.TableID, ToID: nodeID, Kind: "has_column", Summary: "Table has column"})
	}

	for _, r := range snapshot.Relationships {
		cardID := "relationship:" + r.ID
		summary := fmt.Sprintf("%s.%s -> %s.%s", r.FromTableName, r.FromColumnName, r.ToTableName, r.ToColumnName)
		owners := relationshipOwnerSources(r)
		ownerSource := ""
		if len(owners) != 0 {
			ownerSource = owners[0]
		}
		out.Cards = append(out.Cards, Card{
			ID:               cardID,
			Kind:             "relationship",
			Title:            summary,
			Summary:          "Relationship discovered from database metadata. Use it to plan nested GraphJin queries instead of guessing join paths.",
			DatabaseName:     r.FromDatabaseName,
			SchemaName:       r.FromSchemaName,
			TableName:        r.FromTableName,
			ColumnName:       r.FromColumnName,
			Source:           valueOrDefault(r.Source, "core.metadata"),
			OwnerSource:      ownerSource,
			OwnerSourcesJSON: mustJSON(owners),
			RiskLevel:        "low",
			Confidence:       "high",
			EvidenceJSON:     mustJSON(r),
			ExamplesJSON:     mustJSON([]string{relationshipExample(r)}),
		})
		out.Edges = append(out.Edges, Edge{ID: "edge:" + cardID, FromID: "node:column:" + r.FromColumnID, ToID: "node:column:" + r.ToColumnID, Kind: "references", Summary: summary})
	}

	for _, fn := range snapshot.Functions {
		cardID := "function:" + fn.ID
		out.Cards = append(out.Cards, Card{
			ID:               cardID,
			Kind:             "function",
			Title:            fn.Name,
			Summary:          functionSummary(fn),
			DatabaseName:     fn.DatabaseName,
			SchemaName:       fn.SchemaName,
			Source:           "core.metadata",
			OwnerSource:      fn.DatabaseName,
			OwnerSourcesJSON: ownerSourcesJSON(fn.DatabaseName),
			RiskLevel:        "low",
			Confidence:       "high",
			EvidenceJSON:     mustJSON(fn),
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
	out.Details = append(out.Details, CardDetail{
		ID:      cardID + ":source_capabilities",
		CardID:  cardID,
		Section: "source_capabilities",
		Content: "sources[].capabilities is a source-kind-specific boolean map. Valid keys come from the source capability registry and are also exposed on source catalog rows.",
		DataJSON: mustJSON(map[string]any{
			"source_kinds":        sourcecap.Kinds(),
			"source_capabilities": sourcecap.CapabilityMap(),
		}),
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
			SuggestedNext: suggestedNextJSON(opts, "query_catalog"),
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

func addFragments(out *Snapshot, snapshot *MetadataSnapshot, opts BuildOptions) {
	tableNodes := uniqueFragmentTableNodes(snapshot)
	for _, frag := range opts.Fragments {
		qualified := fragmentQualifiedName(frag)
		if qualified == "" {
			continue
		}
		cardID := "fragment:" + qualified
		summary := fragmentSummary(frag)
		importDirective := fmt.Sprintf(`#import "./fragments/%s"`, qualified)
		out.Cards = append(out.Cards, Card{
			ID:            cardID,
			Kind:          "fragment",
			Title:         qualified,
			Summary:       summary,
			TableName:     frag.On,
			Source:        "core.allow_list.fragments",
			RiskLevel:     "low",
			Confidence:    "high",
			EvidenceJSON:  mustJSON(fragmentEvidence(frag, qualified, importDirective)),
			ExamplesJSON:  mustJSON([]string{importDirective, fmt.Sprintf("{ %s { ...%s } }", valueOrDefault(frag.On, "<table>"), frag.Name)}),
			SuggestedNext: suggestedNextJSON(opts, "query_catalog", "get_fragment"),
			DetailRef:     cardID,
		})
		out.Details = append(out.Details, CardDetail{
			ID:      cardID + ":definition",
			CardID:  cardID,
			Section: "fragment_definition",
			Content: "Full GraphQL fragment definition.",
			DataJSON: mustJSON(map[string]any{
				"definition":       frag.Definition,
				"import_directive": importDirective,
			}),
		})
		nodeID := "node:" + cardID
		out.Nodes = append(out.Nodes, Node{ID: nodeID, Kind: "fragment", Name: qualified, Summary: summary, CardID: cardID})
		if targetNodeID := tableNodes[strings.ToLower(strings.TrimSpace(frag.On))]; targetNodeID != "" {
			out.Edges = append(out.Edges, Edge{ID: "edge:fragment-table:" + qualified, FromID: nodeID, ToID: targetNodeID, Kind: "applies_to", Summary: "Fragment type condition matches table"})
		}
	}
}

func addSavedQueries(out *Snapshot, opts BuildOptions) {
	for _, sq := range opts.SavedQueries {
		qualified := savedQueryQualifiedName(sq)
		if qualified == "" {
			continue
		}
		cardID := "saved_query:" + qualified
		operation := valueOrDefault(sq.Operation, "query")
		out.Cards = append(out.Cards, Card{
			ID:              cardID,
			Kind:            "saved_query",
			Title:           qualified,
			Summary:         fmt.Sprintf("Allow-listed %s ready for execute_saved_query.", operation),
			Source:          "core.allow_list",
			RiskLevel:       riskForSavedQuery(operation),
			Confidence:      "high",
			EvidenceJSON:    mustJSON(savedQueryEvidence(sq, qualified)),
			ExamplesJSON:    mustJSON([]string{fmt.Sprintf(`execute_saved_query(name: %q, variables: {...})`, qualified)}),
			SuggestedNext:   suggestedNextJSON(opts, "execute_saved_query", "query_catalog"),
			DetailRef:       cardID,
			InputSchemaJSON: mustJSON(map[string]any{"name": qualified, "namespace": sq.Namespace, "variables": sq.Variables}),
			SafetyJSON:      mustJSON(map[string]any{"allow_listed": true, "execute_with": "execute_saved_query", "operation": operation}),
			GraphQLQuery:    sq.Query,
		})
		out.Details = append(out.Details, CardDetail{
			ID:      cardID + ":definition",
			CardID:  cardID,
			Section: "saved_query_definition",
			Content: "Saved query definition and variable contract from the allow-list.",
			DataJSON: mustJSON(map[string]any{
				"name":      sq.Name,
				"namespace": sq.Namespace,
				"operation": operation,
				"query":     sq.Query,
				"variables": sq.Variables,
			}),
		})
		out.Nodes = append(out.Nodes, Node{ID: "node:" + cardID, Kind: "saved_query", Name: qualified, Summary: operation + " saved query", CardID: cardID})
	}
}

func savedQueryQualifiedName(sq SavedQuery) string {
	name := strings.TrimSpace(sq.Name)
	if name == "" {
		return ""
	}
	namespace := strings.TrimSpace(sq.Namespace)
	if namespace == "" {
		return name
	}
	return namespace + "." + name
}

func savedQueryEvidence(sq SavedQuery, qualified string) map[string]any {
	return map[string]any{
		"name":           sq.Name,
		"namespace":      sq.Namespace,
		"qualified_name": qualified,
		"operation":      valueOrDefault(sq.Operation, "query"),
		"source_hash":    sq.SourceHash,
	}
}

func riskForSavedQuery(operation string) string {
	switch strings.ToLower(strings.TrimSpace(operation)) {
	case "mutation":
		return "medium"
	default:
		return "low"
	}
}

func fragmentEvidence(frag Fragment, qualified, importDirective string) map[string]any {
	return map[string]any{
		"name":             frag.Name,
		"namespace":        frag.Namespace,
		"qualified_name":   qualified,
		"on":               frag.On,
		"import_directive": importDirective,
		"source_hash":      frag.SourceHash,
	}
}

func fragmentSummary(frag Fragment) string {
	if strings.TrimSpace(frag.On) == "" {
		return "Reusable GraphQL fragment field selection."
	}
	return "Reusable GraphQL fragment field selection on " + frag.On + "."
}

func fragmentQualifiedName(frag Fragment) string {
	name := strings.TrimSpace(frag.Name)
	if name == "" {
		return ""
	}
	namespace := strings.TrimSpace(frag.Namespace)
	if namespace == "" {
		return name
	}
	return namespace + "." + name
}

func uniqueFragmentTableNodes(snapshot *MetadataSnapshot) map[string]string {
	counts := make(map[string]int)
	nodes := make(map[string]string)
	add := func(key, nodeID string) {
		key = strings.ToLower(strings.TrimSpace(key))
		if key == "" {
			return
		}
		counts[key]++
		nodes[key] = nodeID
	}
	for _, table := range snapshot.Tables {
		nodeID := "node:table:" + table.ID
		keys := map[string]struct{}{
			table.TableName: {},
			qualifiedName("", table.SchemaName, table.TableName):                 {},
			qualifiedName(table.DatabaseName, table.SchemaName, table.TableName): {},
		}
		for key := range keys {
			add(key, nodeID)
		}
	}
	for key, count := range counts {
		if count != 1 {
			delete(nodes, key)
		}
	}
	return nodes
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

func sourceExamples(source Source) string {
	return mustJSON(sourcecap.Examples(source.Kind, valueOrDefault(source.Name, "<source>")))
}

func sourceCapabilityDetails(kind string) []map[string]any {
	defs := sourcecap.Definitions(kind)
	out := make([]map[string]any, 0, len(defs))
	for _, def := range defs {
		out = append(out, map[string]any{
			"key":              def.Key,
			"action":           def.Action,
			"summary":          def.Summary,
			"default_dev":      def.DefaultDev,
			"default_prod":     def.DefaultProd,
			"default_agentic":  def.DefaultAgentic,
			"severity":         def.Severity,
			"enforcement":      def.Enforcement,
			"read_only_blocks": def.ReadOnlyBlocks,
		})
	}
	return out
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
	for i := range opts.Sources {
		opts.Sources[i].Name = strings.TrimSpace(opts.Sources[i].Name)
		opts.Sources[i].Kind = strings.TrimSpace(opts.Sources[i].Kind)
		opts.Sources[i].Type = strings.TrimSpace(opts.Sources[i].Type)
	}
	sort.Slice(opts.Sources, func(i, j int) bool {
		if opts.Sources[i].Kind != opts.Sources[j].Kind {
			return opts.Sources[i].Kind < opts.Sources[j].Kind
		}
		return opts.Sources[i].Name < opts.Sources[j].Name
	})
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
	for i := range opts.Fragments {
		opts.Fragments[i].Name = strings.TrimSpace(opts.Fragments[i].Name)
		opts.Fragments[i].Namespace = strings.TrimSpace(opts.Fragments[i].Namespace)
		opts.Fragments[i].Definition = strings.TrimSpace(opts.Fragments[i].Definition)
		opts.Fragments[i].On = strings.TrimSpace(opts.Fragments[i].On)
		opts.Fragments[i].SourceHash = strings.TrimSpace(opts.Fragments[i].SourceHash)
		if opts.Fragments[i].SourceHash == "" && opts.Fragments[i].Definition != "" {
			opts.Fragments[i].SourceHash = hashJSON(opts.Fragments[i].Definition)
		}
	}
	sort.Slice(opts.Fragments, func(i, j int) bool {
		if opts.Fragments[i].Namespace != opts.Fragments[j].Namespace {
			return opts.Fragments[i].Namespace < opts.Fragments[j].Namespace
		}
		return opts.Fragments[i].Name < opts.Fragments[j].Name
	})
	for i := range opts.SavedQueries {
		opts.SavedQueries[i].Name = strings.TrimSpace(opts.SavedQueries[i].Name)
		opts.SavedQueries[i].Namespace = strings.TrimSpace(opts.SavedQueries[i].Namespace)
		opts.SavedQueries[i].Operation = strings.TrimSpace(opts.SavedQueries[i].Operation)
		opts.SavedQueries[i].Query = strings.TrimSpace(opts.SavedQueries[i].Query)
		opts.SavedQueries[i].SourceHash = strings.TrimSpace(opts.SavedQueries[i].SourceHash)
		if opts.SavedQueries[i].SourceHash == "" && opts.SavedQueries[i].Query != "" {
			opts.SavedQueries[i].SourceHash = hashJSON(opts.SavedQueries[i].Query)
		}
	}
	sort.Slice(opts.SavedQueries, func(i, j int) bool {
		if opts.SavedQueries[i].Namespace != opts.SavedQueries[j].Namespace {
			return opts.SavedQueries[i].Namespace < opts.SavedQueries[j].Namespace
		}
		return opts.SavedQueries[i].Name < opts.SavedQueries[j].Name
	})
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
		return []string{"query_catalog", "validate_where_clause"}
	}
	return []string{"query_catalog"}
}

func relationshipExample(r MetadataRelationship) string {
	return fmt.Sprintf("{ %s { %s { %s } } }", r.FromTableName, r.ToTableName, r.ToColumnName)
}

func ownerSourcesJSON(values ...string) string {
	return mustJSON(sortedStrings(values))
}

func relationshipOwnerSources(r MetadataRelationship) []string {
	return sortedStrings([]string{r.FromDatabaseName, r.ToDatabaseName})
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
