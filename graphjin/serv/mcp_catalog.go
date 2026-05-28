package serv

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/dosco/graphjin/core/v3"
	"github.com/mark3labs/mcp-go/mcp"
)

type CatalogItem struct {
	ID               string  `json:"id"`
	Kind             string  `json:"kind"`
	Name             string  `json:"name,omitempty"`
	Title            string  `json:"title,omitempty"`
	Summary          string  `json:"summary,omitempty"`
	DatabaseName     string  `json:"database_name,omitempty"`
	SchemaName       string  `json:"schema_name,omitempty"`
	TableName        string  `json:"table_name,omitempty"`
	ColumnName       string  `json:"column_name,omitempty"`
	Source           string  `json:"source,omitempty"`
	RiskLevel        string  `json:"risk_level,omitempty"`
	Confidence       string  `json:"confidence,omitempty"`
	Sensitive        bool    `json:"sensitive,omitempty"`
	Sensitivity      string  `json:"sensitivity,omitempty"`
	EvidenceJSON     string  `json:"evidence_json,omitempty"`
	ExamplesJSON     string  `json:"examples_json,omitempty"`
	SuggestedNext    string  `json:"suggested_next_json,omitempty"`
	DetailRef        string  `json:"detail_ref,omitempty"`
	DetailsJSON      string  `json:"details_json,omitempty"`
	EdgesJSON        string  `json:"edges_json,omitempty"`
	QueryJSON        string  `json:"query_json,omitempty"`
	InputSchemaJSON  string  `json:"input_schema_json,omitempty"`
	OutputSchemaJSON string  `json:"output_schema_json,omitempty"`
	SafetyJSON       string  `json:"safety_json,omitempty"`
	Enabled          bool    `json:"enabled,omitempty"`
	CapabilityKind   string  `json:"capability_kind,omitempty"`
	GraphQLQuery     string  `json:"graphql_query,omitempty"`
	GraphQLMutation  string  `json:"graphql_mutation,omitempty"`
	CreatedAt        string  `json:"created_at,omitempty"`
	UpdatedAt        string  `json:"updated_at,omitempty"`
	Score            float64 `json:"score,omitempty"`
	SearchRank       float64 `json:"search_rank,omitempty"`
}

type CatalogQueryResult struct {
	GeneratedAt     string                       `json:"generated_at"`
	Revision        string                       `json:"revision,omitempty"`
	SourceRevisions map[string]string            `json:"source_revisions,omitempty"`
	Count           int                          `json:"count"`
	Cards           []CatalogItem                `json:"cards"`
	Matches         map[string]core.CatalogMatch `json:"matches,omitempty"`
	Next            *NextGuidance                `json:"next,omitempty"`
}

type GraphQLHelpResult struct {
	For                   string         `json:"for"`
	Summary               string         `json:"summary"`
	Bootstrap             []string       `json:"bootstrap,omitempty"`
	TopicRoutes           []HelpRoute    `json:"topic_routes,omitempty"`
	ReplacesTools         []ToolReplaces `json:"replaces_tools,omitempty"`
	RecommendedFirstQuery string         `json:"recommended_first_query"`
	GraphQLQuery          string         `json:"graphql_query"`
	GraphQLVariables      map[string]any `json:"graphql_variables"`
	CatalogRows           []CatalogItem  `json:"catalog_rows"`
	Examples              []string       `json:"examples,omitempty"`
	Safety                map[string]any `json:"safety,omitempty"`
	Next                  *NextGuidance  `json:"next,omitempty"`
}

type HelpRoute struct {
	Need        string `json:"need"`
	For         string `json:"for"`
	FirstCall   string `json:"first_call"`
	DetailQuery string `json:"detail_query"`
}

type ToolReplaces struct {
	Tool        string `json:"tool"`
	Replacement string `json:"replacement"`
	For         string `json:"for,omitempty"`
}

type CatalogCardResult struct {
	Card    CatalogItem              `json:"card"`
	Details []core.CatalogCardDetail `json:"details,omitempty"`
	Edges   []core.CatalogEdge       `json:"edges,omitempty"`
	Next    *NextGuidance            `json:"next,omitempty"`
}

type CatalogEntrypointsResult struct {
	Entrypoints []CatalogItem `json:"entrypoints"`
}

type CatalogCapabilitiesResult struct {
	Capabilities []CatalogItem `json:"capabilities"`
}

type CatalogOverviewResource struct {
	GeneratedAt     string                   `json:"generated_at"`
	Revision        string                   `json:"revision,omitempty"`
	SourceRevisions map[string]string        `json:"source_revisions,omitempty"`
	Guidance        []string                 `json:"guidance"`
	Entrypoints     []core.CatalogEntryPoint `json:"entrypoints"`
	Capabilities    []core.CatalogCapability `json:"capabilities"`
	Cards           []core.CatalogCard       `json:"cards"`
	CardLimitHint   string                   `json:"card_limit_hint"`
}

func (ms *mcpServer) registerCatalogTools() {
	if ms.service != nil && ms.service.conf != nil && ms.service.conf.Core.IsSourcesUsed() {
		ms.srv.AddTool(mcp.NewTool(
			"graphql_help",
			mcp.WithDescription(graphQLHelpToolDescription()),
			mcp.WithString("for",
				mcp.Required(),
				mcp.Description("Help topic. Start with discovery when unsure."),
				mcp.Enum(graphQLHelpTopics()...),
			),
			mcp.WithOutputSchema[GraphQLHelpResult](),
		), ms.handleGraphQLHelp)
	}

	ms.srv.AddTool(mcp.NewTool(
		"query_catalog",
		mcp.WithDescription(queryCatalogToolDescription()),
		mcp.WithString("id",
			mcp.Description("Optional catalog item id. When set, returns one detailed row with details_json, evidence_json, examples_json, safety_json, and edges_json."),
		),
		mcp.WithString("search",
			mcp.Description("Optional full-text search over catalog identifiers, @directives, relationship intent, analytics intent, summaries, and evidence."),
		),
		mcp.WithObject("where",
			mcp.Description("Optional GraphJin-style filter object. Supported operators: eq, neq, in, nin, like, ilike, regex, iregex, contains, is_null, and, or, not. Fields include kind, title, database_name, table_name, created_at, and updated_at; created_on/updated_on are accepted aliases. Example: {kind: {in: ['table', 'relationship']}}."),
		),
		mcp.WithObject("order_by",
			mcp.Description("Optional sort object. Use {score: 'desc'} with search, or fields like {title: 'asc'}, {updated_at: 'desc'}, or {created_on: 'asc'}."),
		),
		mcp.WithBoolean("explain",
			mcp.Description("When true and search is present, include score, matched fields, matched terms, and short non-sensitive match reasons."),
		),
		mcp.WithString("kind",
			mcp.Description("Compatibility shorthand for where.kind.eq. Prefer where: {kind: {eq: 'table'}} or where.kind.in for new callers."),
		),
		mcp.WithString("database",
			mcp.Description("Compatibility shorthand for where.database_name.eq."),
		),
		mcp.WithString("schema",
			mcp.Description("Compatibility shorthand for where.schema_name.eq."),
		),
		mcp.WithString("table",
			mcp.Description("Compatibility shorthand for where.table_name.eq."),
		),
		mcp.WithString("column",
			mcp.Description("Compatibility shorthand for where.column_name.eq."),
		),
		mcp.WithNumber("limit",
			mcp.Description("Maximum catalog items to return. Defaults to 100, max 500."),
			mcp.Min(1),
			mcp.Max(500),
		),
		mcp.WithOutputSchema[CatalogQueryResult](),
	), ms.handleQueryCatalog)

	if ms.service != nil && ms.service.conf != nil && ms.service.conf.Core.IsSourcesUsed() {
		return
	}

	ms.srv.AddTool(mcp.NewTool(
		"get_catalog_card",
		mcp.WithDescription("Fetch one catalog item with details and nearby graph edges. Use after query_catalog returns an interesting item id."),
		mcp.WithString("id",
			mcp.Required(),
			mcp.Description("Catalog item id, for example language:directive.running or table:<database:schema:table>."),
		),
		mcp.WithOutputSchema[CatalogCardResult](),
	), ms.handleGetCatalogCard)

	ms.srv.AddTool(mcp.NewTool(
		"get_catalog_entrypoints",
		mcp.WithDescription("List recommended catalog entrypoints for discovering schema, language features, config, and capabilities."),
		mcp.WithOutputSchema[CatalogEntrypointsResult](),
	), ms.handleGetCatalogEntrypoints)

	ms.srv.AddTool(mcp.NewTool(
		"get_catalog_capabilities",
		mcp.WithDescription("List catalog-described GraphJin capabilities and safety notes."),
		mcp.WithOutputSchema[CatalogCapabilitiesResult](),
	), ms.handleGetCatalogCapabilities)
}

func graphQLHelpToolDescription() string {
	return "Read-only bootstrap helper for sources-mode GraphJin. Start with graphql_help(for: \"discovery\") when unsure. Valid for values: " +
		strings.Join(graphQLHelpTopics(), ", ") +
		". Replaces legacy MCP discovery prompts/tools such as get_query_syntax, get_mutation_syntax, get_catalog_card, get_config_docs, get_js_runtime_api, fix_query_error, saved-query discovery, fragment discovery, table/schema discovery, and relationship exploration by querying gj_catalog help rows. Returns bootstrap steps, topic_routes, replaces_tools, catalog rows, examples, safety notes, next guidance, and the exact internal gj_catalog GraphQL query."
}

func queryCatalogToolDescription() string {
	return "Search GraphJin's AI-first catalog for schema, relationships, workflows, saved queries, fragments, language features, directives, operators, config, security, and capabilities. In sources mode, call graphql_help(for: \"discovery\") when unsure. Use query_catalog(id: \"...\") for one full-detail row with details_json, evidence_json, examples_json, safety_json, and edges_json. Examples: query_catalog(id: \"help:query\"), query_catalog(id: \"help:schema\"), query_catalog(where: { kind: { eq: \"table\" } }), query_catalog(where: { kind: { eq: \"saved_query\" } }). Use validate_where_clause for filters, execute_saved_query for approved saved queries, and execute_graphql only when raw execution is enabled."
}

func (ms *mcpServer) registerCatalogResources() {
	ms.srv.AddResource(
		mcp.NewResource(
			CatalogOverviewResourceURI,
			"GraphJin Catalog Overview",
			mcp.WithResourceDescription("AI-first catalog overview with schema, language, config, capability, and discovery entrypoints"),
			mcp.WithMIMEType("application/json"),
		),
		func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			snapshot, err := ms.catalogSnapshot()
			if err != nil {
				return nil, err
			}
			payload := CatalogOverviewResource{
				GeneratedAt:     snapshot.GeneratedAt.Format("2006-01-02T15:04:05Z07:00"),
				Revision:        snapshot.Revision,
				SourceRevisions: snapshot.SourceRevisions,
				Guidance: []string{
					"Use query_catalog as the primary discovery surface: search for ranked text discovery, where for exact filters.",
					"Canonical shape: {search: 'join orders customers', where: {kind: {eq: 'relationship'}}, order_by: {score: 'desc'}}.",
					"Find reusable workflows with query_catalog(where: {kind: {eq: 'workflow'}}), then inspect metadata with query_catalog(id: 'workflow:<name>').",
					"Use query_catalog(id: '...') to inspect evidence, examples, and graph edges for a returned item id.",
					"Use validate_where_clause before executing filters against discovered tables and columns.",
				},
				Entrypoints:   snapshot.EntryPoints,
				Capabilities:  snapshot.Capabilities,
				Cards:         snapshot.Query(core.CatalogQuery{Limit: 100}),
				CardLimitHint: "Overview includes the first 100 base catalog items. Use query_catalog for filtered discovery.",
			}
			data, err := mcpMarshalJSON(payload, true)
			if err != nil {
				return nil, err
			}
			return []mcp.ResourceContents{
				mcp.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(data)},
			}, nil
		},
	)

	ms.srv.AddResource(
		mcp.NewResource(
			CatalogEntrypointsResourceURI,
			"GraphJin Catalog Entrypoints",
			mcp.WithResourceDescription("Recommended entrypoints for navigating GraphJin catalog discovery"),
			mcp.WithMIMEType("application/json"),
		),
		func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			snapshot, err := ms.catalogSnapshot()
			if err != nil {
				return nil, err
			}
			data, err := mcpMarshalJSON(map[string]any{"entrypoints": snapshot.EntryPoints}, true)
			if err != nil {
				return nil, err
			}
			return []mcp.ResourceContents{
				mcp.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(data)},
			}, nil
		},
	)

	ms.srv.AddResource(
		mcp.NewResource(
			CatalogCapabilitiesResourceURI,
			"GraphJin Catalog Capabilities",
			mcp.WithResourceDescription("Catalog-described GraphJin capabilities, safety notes, and recommended tools"),
			mcp.WithMIMEType("application/json"),
		),
		func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			snapshot, err := ms.catalogSnapshot()
			if err != nil {
				return nil, err
			}
			data, err := mcpMarshalJSON(map[string]any{"capabilities": snapshot.Capabilities}, true)
			if err != nil {
				return nil, err
			}
			return []mcp.ResourceContents{
				mcp.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(data)},
			}, nil
		},
	)
}

func (ms *mcpServer) catalogSnapshot() (*core.CatalogSnapshot, error) {
	if ms.service == nil {
		return core.BuildCatalogSnapshot(&core.MetadataSnapshot{}, nil), nil
	}
	return ms.service.catalogSnapshot()
}

func (ms *mcpServer) handleQueryCatalog(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	where, err := catalogObjectArg(args, "where")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	orderBy, err := catalogStringMapArg(args, "order_by")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	q := catalogGraphQLQuery{
		ID:       stringArg(args, "id"),
		Kind:     stringArg(args, "kind"),
		Search:   stringArg(args, "search"),
		Where:    where,
		OrderBy:  orderBy,
		Explain:  catalogBoolArg(args, "explain"),
		Database: stringArg(args, "database"),
		Schema:   stringArg(args, "schema"),
		Table:    stringArg(args, "table"),
		Column:   stringArg(args, "column"),
		Limit:    catalogIntArg(args, "limit"),
	}
	rows, err := ms.queryCatalogGraphQL(ctx, q)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	result := CatalogQueryResult{
		GeneratedAt: time.Now().UTC().Format("2006-01-02T15:04:05Z07:00"),
		Revision:    ms.catalogRevisionGraphQL(ctx),
		Count:       len(rows),
		Cards:       rows,
		Next: ms.newNextGuidance("catalog_results", []NextOption{
			nextOption("query_catalog", 1, "Inspect a returned catalog item in detail.", "Call query_catalog with the id of the most relevant item.", []string{"id"}, nil),
			nextOption("validate_where_clause", 2, "Validate filters after choosing a table/column.", "Use for where clauses against discovered schema.", []string{"table", "where"}, []string{"database"}),
		}),
	}
	if q.Explain && q.Search != "" {
		result.Matches = catalogMatchesFromRows(rows)
	}
	return ms.toolResultJSON("query_catalog", args, result)
}

func (ms *mcpServer) handleGraphQLHelp(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	helpFor := strings.ToLower(strings.TrimSpace(stringArg(args, "for")))
	spec, ok := graphQLHelpSpecFor(helpFor)
	if !ok {
		return mcp.NewToolResultError(fmt.Sprintf("unsupported graphql_help topic %q; valid values: %s", helpFor, strings.Join(graphQLHelpTopics(), ", "))), nil
	}
	q := spec.catalogQuery()
	query, err := buildCatalogGraphQLQuery(q)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	rows, err := ms.queryCatalogGraphQL(ctx, q)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	result := GraphQLHelpResult{
		For:                   spec.For,
		Summary:               spec.Summary,
		Bootstrap:             graphQLHelpBootstrap(),
		TopicRoutes:           graphQLHelpTopicRoutesFor(spec.For),
		ReplacesTools:         graphQLHelpReplacementsFor(spec.For),
		RecommendedFirstQuery: spec.RecommendedFirstQuery,
		GraphQLQuery:          query,
		GraphQLVariables:      map[string]any{},
		CatalogRows:           rows,
		Examples:              spec.Examples,
		Safety:                spec.Safety,
		Next:                  ms.graphQLHelpNext(spec),
	}
	return ms.toolResultJSON("graphql_help", args, result)
}

type graphQLHelpSpec struct {
	For                   string
	Summary               string
	Search                string
	Kinds                 []string
	RecommendedFirstQuery string
	Examples              []string
	Safety                map[string]any
	Limit                 int
}

func graphQLHelpBootstrap() []string {
	return []string{
		`Call graphql_help(for: "discovery") when unsure; it is the sources-mode prompt replacement.`,
		`Use topic_routes to choose a narrower graphql_help(for: "...") topic.`,
		`Use query_catalog(id: "help:<topic>") for full guidance with details_json, evidence_json, examples_json, safety_json, and edges_json.`,
		`Use query_catalog(search/where/order_by/limit) or direct gj_catalog queries for evidence-backed discovery.`,
		`Use validate_where_clause before non-trivial filters, execute_saved_query after inspecting a saved_query row, and execute_graphql only when raw execution is enabled.`,
	}
}

func graphQLHelpTopicRoutes() []HelpRoute {
	return []HelpRoute{
		{Need: "unknown starting point, old MCP tool mapping, available discovery surfaces", For: "discovery", FirstCall: `graphql_help(for: "discovery")`, DetailQuery: `query_catalog(id: "help:discovery")`},
		{Need: "tiny sources-mode MCP tool surface and removed legacy tool replacements", For: "mcp_tools", FirstCall: `graphql_help(for: "mcp_tools")`, DetailQuery: `query_catalog(id: "help:mcp_tools")`},
		{Need: "catalog row shape, details, evidence, examples, safety, edges, capabilities", For: "catalog", FirstCall: `graphql_help(for: "catalog")`, DetailQuery: `query_catalog(id: "help:catalog")`},
		{Need: "databases, tables, columns, relationships, functions, indexes", For: "schema", FirstCall: `graphql_help(for: "schema")`, DetailQuery: `query_catalog(id: "help:schema")`},
		{Need: "table names, primary keys, row shape, sample/profile guidance", For: "tables", FirstCall: `graphql_help(for: "tables")`, DetailQuery: `query_catalog(id: "help:tables")`},
		{Need: "column names, types, sensitivity, indexes, filter hints", For: "columns", FirstCall: `graphql_help(for: "columns")`, DetailQuery: `query_catalog(id: "help:columns")`},
		{Need: "join paths, nested selectors, @through hints", For: "relationships", FirstCall: `graphql_help(for: "relationships")`, DetailQuery: `query_catalog(id: "help:relationships")`},
		{Need: "GraphJin query DSL, directives, aggregates, analytics, pagination", For: "query", FirstCall: `graphql_help(for: "query")`, DetailQuery: `query_catalog(id: "help:query")`},
		{Need: "where operators and filter validation", For: "filters", FirstCall: `graphql_help(for: "filters")`, DetailQuery: `query_catalog(id: "help:filters")`},
		{Need: "insert, update, upsert, delete, nested writes, preview/apply patterns", For: "mutations", FirstCall: `graphql_help(for: "mutations")`, DetailQuery: `query_catalog(id: "help:mutations")`},
		{Need: "approved allow-list queries and variable contracts", For: "saved_queries", FirstCall: `graphql_help(for: "saved_queries")`, DetailQuery: `query_catalog(id: "help:saved_queries")`},
		{Need: "reusable GraphQL fragments", For: "fragments", FirstCall: `graphql_help(for: "fragments")`, DetailQuery: `query_catalog(id: "help:fragments")`},
		{Need: "workflow discovery, variables, execution policy", For: "workflows", FirstCall: `graphql_help(for: "workflows")`, DetailQuery: `query_catalog(id: "help:workflows")`},
		{Need: "JavaScript workflow runtime and callable GraphJin tool guidance", For: "workflow_runtime", FirstCall: `graphql_help(for: "workflow_runtime")`, DetailQuery: `query_catalog(id: "help:workflow_runtime")`},
		{Need: "redacted config docs, roles, permissions, safe config changes", For: "config", FirstCall: `graphql_help(for: "config")`, DetailQuery: `query_catalog(id: "help:config")`},
		{Need: "gj_security posture, policy rows, findings, severity filters", For: "security", FirstCall: `graphql_help(for: "security")`, DetailQuery: `query_catalog(id: "help:security")`},
		{Need: "code/source intelligence and safe preview/apply edit flows", For: "code", FirstCall: `graphql_help(for: "code")`, DetailQuery: `query_catalog(id: "help:code")`},
		{Need: "GraphJin error repair hints", For: "errors", FirstCall: `graphql_help(for: "errors")`, DetailQuery: `query_catalog(id: "help:errors")`},
	}
}

func graphQLHelpTopicRoutesFor(topic string) []HelpRoute {
	if topic == "discovery" || topic == "mcp_tools" {
		return graphQLHelpTopicRoutes()
	}
	for _, route := range graphQLHelpTopicRoutes() {
		if route.For == topic {
			return []HelpRoute{route}
		}
	}
	return nil
}

func graphQLHelpToolReplacements() []ToolReplaces {
	return []ToolReplaces{
		{Tool: "get_catalog_entrypoints", Replacement: `graphql_help(for: "discovery") or gj_catalog(where: { kind: { eq: "entrypoint" } })`, For: "discovery"},
		{Tool: "get_catalog_card", Replacement: `query_catalog(id: "...")`, For: "catalog"},
		{Tool: "get_catalog_capabilities", Replacement: `query_catalog(where: { kind: { in: ["capability", "system_capability"] } })`, For: "catalog"},
		{Tool: "get_query_syntax", Replacement: `graphql_help(for: "query") and query_catalog(id: "help:query")`, For: "query"},
		{Tool: "get_mutation_syntax", Replacement: `graphql_help(for: "mutations") and query_catalog(id: "help:mutations")`, For: "mutations"},
		{Tool: "get_discovery_schema", Replacement: `graphql_help(for: "catalog") and query_catalog(id: "help:catalog")`, For: "catalog"},
		{Tool: "get_table_sample", Replacement: `graphql_help(for: "tables"), graphql_help(for: "columns"), sample/profile catalog guidance, then permitted app-data queries or workflows`, For: "tables"},
		{Tool: "get_workflow_guide", Replacement: `graphql_help(for: "workflows") and workflow catalog rows`, For: "workflows"},
		{Tool: "get_schema_insights", Replacement: `graphql_help(for: "schema")`, For: "schema"},
		{Tool: "explore_relationships", Replacement: `graphql_help(for: "relationships"), relationship rows, and edges_json`, For: "relationships"},
		{Tool: "find_path", Replacement: `graphql_help(for: "relationships"), relationship rows, and edges_json`, For: "relationships"},
		{Tool: "list_saved_queries", Replacement: `graphql_help(for: "saved_queries") and saved_query rows`, For: "saved_queries"},
		{Tool: "search_saved_queries", Replacement: `graphql_help(for: "saved_queries") and saved_query rows`, For: "saved_queries"},
		{Tool: "get_saved_query", Replacement: `query_catalog(id: "saved_query:<name>") or query_catalog(where: { kind: { eq: "saved_query" } })`, For: "saved_queries"},
		{Tool: "list_fragments", Replacement: `graphql_help(for: "fragments") and fragment rows`, For: "fragments"},
		{Tool: "search_fragments", Replacement: `graphql_help(for: "fragments") and fragment rows`, For: "fragments"},
		{Tool: "get_fragment", Replacement: `query_catalog(id: "fragment:<name>") or query_catalog(where: { kind: { eq: "fragment" } })`, For: "fragments"},
		{Tool: "get_config_docs", Replacement: `graphql_help(for: "config") and config catalog rows`, For: "config"},
		{Tool: "get_js_runtime_api", Replacement: `graphql_help(for: "workflow_runtime") and workflow runtime catalog rows`, For: "workflow_runtime"},
		{Tool: "write_query", Replacement: `graphql_help(for: "query"), catalog examples, then direct GraphQL or saved query/workflow`, For: "query"},
		{Tool: "write_mutation", Replacement: `graphql_help(for: "mutations"), catalog examples, then governed GraphQL mutation or workflow`, For: "mutations"},
		{Tool: "fix_query_error", Replacement: `errors[].extensions.graphjin_repair and graphql_help(for: "errors")`, For: "errors"},
		{Tool: "execute_workflow", Replacement: `gj_workflow_execution(insert) in GraphQL`, For: "workflows"},
	}
}

func graphQLHelpReplacementsFor(topic string) []ToolReplaces {
	all := graphQLHelpToolReplacements()
	if topic == "discovery" || topic == "mcp_tools" {
		return all
	}
	out := make([]ToolReplaces, 0, len(all))
	for _, repl := range all {
		if repl.For == topic {
			out = append(out, repl)
		}
	}
	return out
}

func (s graphQLHelpSpec) catalogQuery() catalogGraphQLQuery {
	limit := s.Limit
	if limit <= 0 {
		limit = 25
	}
	return catalogGraphQLQuery{
		Search: s.Search,
		Where: map[string]any{
			"or": []any{
				map[string]any{"id": map[string]any{"eq": "help:" + s.For}},
				map[string]any{"kind": map[string]any{"in": s.Kinds}},
			},
		},
		OrderBy: map[string]string{"search_rank": "desc"},
		Limit:   limit,
	}
}

func (ms *mcpServer) graphQLHelpNext(spec graphQLHelpSpec) *NextGuidance {
	options := []NextOption{
		optionWithTemplate(
			nextOption("query_catalog", 1, "Inspect the canonical help row in detail.", "Use this before acting on a new GraphJin surface.", []string{"id"}, nil),
			map[string]any{"id": "help:" + spec.For},
		),
		optionWithTemplate(
			nextOption("query_catalog", 2, "Continue with filtered catalog discovery for this topic.", "Use the same kind filters returned by graphql_help.", nil, []string{"search", "where", "order_by", "limit"}),
			map[string]any{
				"search": spec.Search,
				"where":  map[string]any{"kind": map[string]any{"in": spec.Kinds}},
				"limit":  spec.Limit,
			},
		),
	}
	switch spec.For {
	case "filters", "query", "columns", "tables", "schema":
		options = append(options, nextOption("validate_where_clause", 3, "Validate filters against discovered table and column metadata.", "Use before executing a query with non-trivial where clauses.", []string{"table", "where"}, []string{"database"}))
	case "saved_queries":
		options = append(options, nextOption("execute_saved_query", 3, "Run an approved saved query after inspecting its catalog row.", "Use when a saved_query row matches the task.", []string{"name"}, []string{"variables", "namespace"}))
	}
	return ms.newNextGuidance("graphql_help_"+spec.For, options)
}

var graphQLHelpTopicOrder = []string{
	"discovery",
	"mcp_tools",
	"catalog",
	"schema",
	"tables",
	"columns",
	"relationships",
	"query",
	"filters",
	"mutations",
	"saved_queries",
	"fragments",
	"workflows",
	"workflow_runtime",
	"config",
	"security",
	"code",
	"errors",
}

func graphQLHelpTopics() []string {
	return append([]string(nil), graphQLHelpTopicOrder...)
}

func graphQLHelpSpecFor(topic string) (graphQLHelpSpec, bool) {
	specs := map[string]graphQLHelpSpec{
		"discovery":        helpSpec("discovery", "Start here when unsure. Routes to catalog entrypoints, capabilities, high-level help rows, and the legacy-tool replacement map.", "catalog discovery schema workflow query security mcp tools legacy", []string{"help", "entrypoint", "capability", "system_capability"}, `graphql_help(for: "discovery")`, []string{`query_catalog(id: "help:mcp_tools")`, `query_catalog(where: { kind: { eq: "table" } })`}),
		"mcp_tools":        helpSpec("mcp_tools", "Learn the tiny sources-mode MCP surface and how removed legacy discovery tools map into catalog/help rows.", "mcp tools legacy discovery get_query_syntax get_catalog_card get_js_runtime_api fix_query_error", []string{"help", "capability", "system_capability", "entrypoint"}, `query_catalog(id: "help:mcp_tools")`, []string{`graphql_help(for: "query")`, `query_catalog(id: "help:query")`, `query_catalog(where: { kind: { in: ["capability", "system_capability"] } })`}),
		"catalog":          helpSpec("catalog", "Use gj_catalog/query_catalog for discovery and query_catalog(id) for full item evidence.", "catalog detail evidence examples edges safety", []string{"help", "entrypoint", "capability", "system_capability"}, `query_catalog(id: "help:catalog")`, []string{`query_catalog(search: "join orders customers", where: { kind: { eq: "relationship" } })`}),
		"schema":           helpSpec("schema", "Discover databases, tables, columns, relationships, functions, indexes, and row-shape hints.", "schema table column relationship function index sample profile", []string{"help", "database", "table", "column", "relationship", "function"}, `query_catalog(id: "help:schema")`, []string{`query_catalog(where: { kind: { in: ["table", "column", "relationship"] } })`}),
		"tables":           helpSpec("tables", "Find table names, primary keys, row-shape hints, sample/profile availability, and graph edges.", "tables primary key sample profile row count", []string{"help", "table", "relationship", "column"}, `query_catalog(where: { kind: { eq: "table" } })`, []string{`query_catalog(id: "table:<database.schema.table>")`}),
		"columns":          helpSpec("columns", "Find column names, types, sensitivity notes, filter hints, indexes, and sample/profile availability.", "columns fields types filters sensitive sample profile", []string{"help", "column", "table", "operator_set"}, `query_catalog(where: { kind: { eq: "column" }, table_name: { eq: "<table>" } })`, []string{`validate_where_clause(table: "<table>", where: { id: { eq: 1 } })`}),
		"relationships":    helpSpec("relationships", "Find safe join paths and @through hints before nesting related selectors.", "relationship join path foreign key through nested selector", []string{"help", "relationship", "directive", "table"}, `query_catalog(search: "join <source> <target>", where: { kind: { eq: "relationship" } })`, []string{`query_catalog(id: "<relationship_id>")`}),
		"query":            helpSpec("query", "Learn GraphJin query DSL syntax, query patterns, aggregations, analytics directives, and common mistakes.", "query syntax dsl aggregate analytics directive pattern distinct limit", []string{"help", "directive", "operator_set", "query_pattern", "deprecated_feature"}, `query_catalog(id: "help:query")`, []string{`query_catalog(where: { kind: { in: ["directive", "operator_set", "query_pattern"] } })`}),
		"filters":          helpSpec("filters", "Learn typed where operators and validate filters against table and column metadata.", "where filter operators eq in ilike is_null validate", []string{"help", "operator_set", "column", "query_pattern"}, `query_catalog(id: "help:filters")`, []string{`validate_where_clause(table: "<table>", where: { id: { in: [1, 2, 3] } })`}),
		"mutations":        helpSpec("mutations", "Learn insert, update, upsert, delete, nested mutation, and code-source preview/apply patterns.", "mutation insert update upsert delete code source preview apply", []string{"help", "mutation_pattern", "operator_set", "system_capability"}, `query_catalog(where: { kind: { eq: "mutation_pattern" } })`, []string{`query_catalog(id: "help:mutations")`}),
		"saved_queries":    helpSpec("saved_queries", "Find allow-listed saved queries, inspect variable contracts, then run execute_saved_query.", "saved query allow list variables execute_saved_query", []string{"help", "saved_query", "capability"}, `query_catalog(where: { kind: { eq: "saved_query" } })`, []string{`execute_saved_query(name: "<saved_query_name>", variables: {})`}),
		"fragments":        helpSpec("fragments", "Discover reusable GraphQL fragments and import guidance before repeating field selections.", "fragments graphql reusable field selection import", []string{"help", "fragment", "table"}, `query_catalog(where: { kind: { eq: "fragment" } })`, []string{`query_catalog(id: "help:fragments")`}),
		"workflows":        helpSpec("workflows", "Discover reusable workflows, variable schemas, execution policy, and workflow control-plane guidance.", "workflow reusable variables execution gj_workflow_execution", []string{"help", "workflow", "system_capability", "capability"}, `query_catalog(where: { kind: { eq: "workflow" } })`, []string{`mutation { gj_workflow_execution(insert: { workflow_name: "...", variables: {} }) { status result_json error duration_ms } }`}),
		"workflow_runtime": helpSpec("workflow_runtime", "Learn JavaScript workflow runtime concepts, callable tool guidance, and safety constraints.", "javascript workflow runtime goja gj tools queryCatalog executeSavedQuery", []string{"help", "workflow", "capability", "system_capability"}, `query_catalog(id: "help:workflow_runtime")`, []string{`query_catalog(search: "workflow runtime goja tools")`}),
		"config":           helpSpec("config", "Discover redacted configuration documentation, roles, permissions, sources, and safe config update guidance.", "config docs sources roles permissions redacted update gj_config", []string{"help", "config", "system_capability", "capability"}, `query_catalog(id: "help:config")`, []string{`query_catalog(search: "config docs", where: { kind: { in: ["help", "config", "system_capability"] } })`}),
		"security":         helpSpec("security", "Discover gj_security guidance, policy rows, findings, severity filters, and agentic safety expectations.", "security findings policy posture gj_security agentic production", []string{"help", "system_capability", "config"}, `query_catalog(id: "help:security")`, []string{`query_catalog(where: { kind: { eq: "system_capability" }, name: { eq: "gj_security.query" } })`}),
		"code":             helpSpec("code", "Discover code-source catalog rows and safe source-edit preview/apply guidance when code sources are configured.", "code source file symbol preview apply lock", []string{"help", "mutation_pattern", "system_capability", "table", "column"}, `query_catalog(id: "help:code")`, []string{`query_catalog(search: "code source preview apply source edit")`}),
		"errors":           helpSpec("errors", "Use errors[].extensions.graphjin_repair, then inspect relevant schema or language rows before retrying.", "error repair graphjin_repair syntax table column relationship", []string{"help", "deprecated_feature", "query_pattern", "operator_set", "system_capability"}, `query_catalog(id: "help:errors")`, []string{`query_catalog(search: "error repair syntax relationship")`}),
	}
	spec, ok := specs[topic]
	return spec, ok
}

func helpSpec(forValue, summary, search string, kinds []string, recommended string, examples []string) graphQLHelpSpec {
	return graphQLHelpSpec{
		For:                   forValue,
		Summary:               summary,
		Search:                search,
		Kinds:                 kinds,
		RecommendedFirstQuery: recommended,
		Examples:              examples,
		Safety:                map[string]any{"read_only": true, "catalog_backed": true, "uses_caller_permissions": true},
		Limit:                 25,
	}
}

func (ms *mcpServer) handleGetCatalogCard(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	id := stringArg(args, "id")
	if id == "" {
		return mcp.NewToolResultError("id is required"), nil
	}

	rows, err := ms.queryCatalogGraphQL(ctx, catalogGraphQLQuery{
		Where: map[string]any{"id": map[string]any{"eq": id}},
		Limit: 1,
	})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if len(rows) == 0 {
		return mcp.NewToolResultError("catalog item not found"), nil
	}
	card := rows[0]
	details, err := catalogCardDetails(card)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	edges, err := catalogCardEdges(card)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	result := CatalogCardResult{
		Card:    card,
		Details: details,
		Edges:   edges,
		Next: ms.newNextGuidance("catalog_card", []NextOption{
			nextOption("query_catalog", 1, "Search related catalog items.", "Use search plus where filters to continue discovery.", nil, []string{"search", "where", "order_by", "limit"}),
			nextOption("validate_where_clause", 2, "Validate filters after reviewing schema/operator items.", "Use for table and filter/operator guidance.", []string{"table", "where"}, []string{"database"}),
		}),
	}
	return ms.toolResultJSON("get_catalog_card", args, result)
}

func (ms *mcpServer) handleGetCatalogEntrypoints(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	rows, err := ms.queryCatalogGraphQL(ctx, catalogGraphQLQuery{
		Where:   map[string]any{"kind": map[string]any{"eq": "entrypoint"}},
		OrderBy: map[string]string{"name": "asc"},
		Limit:   100,
	})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return ms.toolResultJSON("get_catalog_entrypoints", req.GetArguments(), CatalogEntrypointsResult{Entrypoints: rows})
}

func (ms *mcpServer) handleGetCatalogCapabilities(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	rows, err := ms.queryCatalogGraphQL(ctx, catalogGraphQLQuery{
		Where:   map[string]any{"kind": map[string]any{"eq": "capability"}},
		OrderBy: map[string]string{"name": "asc"},
		Limit:   200,
	})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return ms.toolResultJSON("get_catalog_capabilities", req.GetArguments(), CatalogCapabilitiesResult{Capabilities: rows})
}

type catalogGraphQLQuery struct {
	ID      string
	Search  string
	Where   map[string]any
	OrderBy map[string]string
	Limit   int
	Explain bool

	Kind     string
	Database string
	Schema   string
	Table    string
	Column   string
}

func (ms *mcpServer) queryCatalogGraphQL(ctx context.Context, q catalogGraphQLQuery) ([]CatalogItem, error) {
	if ms == nil || ms.service == nil || ms.service.gj == nil {
		return nil, fmt.Errorf("GraphJin catalog GraphQL is not ready")
	}
	query, err := buildCatalogGraphQLQuery(q)
	if err != nil {
		return nil, err
	}
	var rc core.RequestConfig
	if namespace := ms.getNamespace(); namespace != "" {
		rc.SetNamespace(namespace)
	}
	res, err := ms.service.gj.GraphQL(ctx, query, nil, &rc)
	if err != nil {
		return nil, err
	}
	if len(res.Errors) != 0 {
		return nil, fmt.Errorf("%s", catalogGraphQLErrors(res.Errors))
	}
	var out struct {
		Items []CatalogItem `json:"gj_catalog"`
	}
	if err := json.Unmarshal(res.Data, &out); err != nil {
		return nil, fmt.Errorf("decode gj_catalog response: %w", err)
	}
	return out.Items, nil
}

func (ms *mcpServer) catalogRevisionGraphQL(ctx context.Context) string {
	if ms == nil || ms.service == nil || ms.service.gj == nil || !ms.service.conf.graphjinControlPlaneEnabled() {
		return ""
	}
	var rc core.RequestConfig
	if namespace := ms.getNamespace(); namespace != "" {
		rc.SetNamespace(namespace)
	}
	res, err := ms.service.gj.GraphQL(ctx, `query { gj_config(id: "current") { catalog_revision } }`, nil, &rc)
	if err != nil || len(res.Errors) != 0 {
		return ""
	}
	var out struct {
		Config struct {
			CatalogRevision string `json:"catalog_revision"`
		} `json:"gj_config"`
	}
	if err := json.Unmarshal(res.Data, &out); err != nil {
		return ""
	}
	return out.Config.CatalogRevision
}

func buildCatalogGraphQLQuery(q catalogGraphQLQuery) (string, error) {
	args, err := catalogGraphQLArgs(q)
	if err != nil {
		return "", err
	}
	if args != "" {
		args = "(" + args + ")"
	}
	return "query { gj_catalog" + args + " { " + catalogGraphQLFields() + " } }", nil
}

func catalogGraphQLArgs(q catalogGraphQLQuery) (string, error) {
	var args []string
	if q.Search != "" {
		args = append(args, "search: "+catalogQuote(q.Search))
	}
	where := catalogCombinedWhere(q)
	if len(where) != 0 {
		value, err := catalogGraphQLValue(catalogNormalizeWhere(where))
		if err != nil {
			return "", err
		}
		args = append(args, "where: "+value)
	}
	if len(q.OrderBy) != 0 {
		value, err := catalogGraphQLOrderBy(q.OrderBy)
		if err != nil {
			return "", err
		}
		args = append(args, "order_by: "+value)
	}
	args = append(args, fmt.Sprintf("limit: %d", catalogEffectiveLimit(q)))
	return strings.Join(args, ", "), nil
}

func catalogGraphQLLimit(limit int) int {
	if limit <= 0 {
		return 100
	}
	if limit > 500 {
		return 500
	}
	return limit
}

func catalogCombinedWhere(q catalogGraphQLQuery) map[string]any {
	var clauses []any
	if len(q.Where) != 0 {
		clauses = append(clauses, q.Where)
	}
	shorthand := make(map[string]any)
	addCatalogShorthand(shorthand, "id", q.ID)
	addCatalogShorthand(shorthand, "kind", q.Kind)
	addCatalogShorthand(shorthand, "database_name", q.Database)
	addCatalogShorthand(shorthand, "schema_name", q.Schema)
	addCatalogShorthand(shorthand, "table_name", q.Table)
	addCatalogShorthand(shorthand, "column_name", q.Column)
	if len(shorthand) != 0 {
		clauses = append(clauses, shorthand)
	}
	switch len(clauses) {
	case 0:
		return nil
	case 1:
		if where, ok := catalogAnyMap(clauses[0]); ok {
			return where
		}
	}
	return map[string]any{"and": clauses}
}

func catalogEffectiveLimit(q catalogGraphQLQuery) int {
	if q.ID != "" && q.Limit <= 0 {
		return 1
	}
	return catalogGraphQLLimit(q.Limit)
}

func addCatalogShorthand(where map[string]any, field, value string) {
	value = strings.TrimSpace(value)
	if value != "" {
		where[field] = map[string]any{"eq": value}
	}
}

func catalogGraphQLOrderBy(orderBy map[string]string) (string, error) {
	keys := make([]string, 0, len(orderBy))
	for key := range orderBy {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var parts []string
	for _, key := range keys {
		field := catalogNormalizeFieldName(key)
		if !catalogValidName(field) {
			return "", fmt.Errorf("unsupported catalog order_by field %q", key)
		}
		order := strings.ToLower(strings.TrimSpace(orderBy[key]))
		switch order {
		case "asc", "desc":
		default:
			return "", fmt.Errorf("unsupported catalog order %q for %s", orderBy[key], key)
		}
		parts = append(parts, field+": "+order)
	}
	return "{ " + strings.Join(parts, ", ") + " }", nil
}

func catalogGraphQLValue(v any) (string, error) {
	v = catalogJSONAny(v)
	switch x := v.(type) {
	case nil:
		return "null", nil
	case string:
		return catalogQuote(x), nil
	case bool:
		if x {
			return "true", nil
		}
		return "false", nil
	case float64:
		return fmt.Sprintf("%v", x), nil
	case []any:
		items := make([]string, 0, len(x))
		for _, item := range x {
			value, err := catalogGraphQLValue(item)
			if err != nil {
				return "", err
			}
			items = append(items, value)
		}
		return "[" + strings.Join(items, ", ") + "]", nil
	case map[string]any:
		keys := make([]string, 0, len(x))
		for key := range x {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			field := catalogNormalizeFieldName(key)
			if !catalogValidName(field) {
				return "", fmt.Errorf("unsupported catalog field %q", key)
			}
			value, err := catalogGraphQLValue(x[key])
			if err != nil {
				return "", err
			}
			parts = append(parts, field+": "+value)
		}
		return "{ " + strings.Join(parts, ", ") + " }", nil
	default:
		return "", fmt.Errorf("unsupported catalog value %T", v)
	}
}

func catalogNormalizeWhere(where map[string]any) map[string]any {
	out := make(map[string]any, len(where))
	for key, value := range where {
		out[catalogNormalizeFieldName(key)] = catalogNormalizeWhereValue(value)
	}
	return out
}

func catalogNormalizeWhereValue(value any) any {
	value = catalogJSONAny(value)
	switch x := value.(type) {
	case map[string]any:
		return catalogNormalizeWhere(x)
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = catalogNormalizeWhereValue(item)
		}
		return out
	default:
		return value
	}
}

func catalogNormalizeFieldName(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "created_on":
		return "created_at"
	case "updated_on":
		return "updated_at"
	default:
		return strings.TrimSpace(name)
	}
}

func catalogJSONAny(v any) any {
	switch v.(type) {
	case nil, string, bool, float64, []any, map[string]any:
		return v
	}
	data, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		return v
	}
	return out
}

func catalogAnyMap(v any) (map[string]any, bool) {
	v = catalogJSONAny(v)
	m, ok := v.(map[string]any)
	return m, ok
}

func catalogQuote(value string) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func catalogValidName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if i == 0 {
			if r != '_' && !unicode.IsLetter(r) {
				return false
			}
			continue
		}
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func catalogGraphQLFields() string {
	return strings.Join([]string{
		"id", "kind", "name", "title", "summary",
		"database_name", "schema_name", "table_name", "column_name",
		"source", "risk_level", "confidence", "sensitive", "sensitivity",
		"evidence_json", "examples_json", "suggested_next_json", "detail_ref",
		"details_json", "edges_json", "query_json", "input_schema_json",
		"output_schema_json", "safety_json", "enabled", "capability_kind",
		"graphql_query", "graphql_mutation", "created_at", "updated_at",
		"score", "search_rank",
	}, " ")
}

func catalogMatchesFromRows(rows []CatalogItem) map[string]core.CatalogMatch {
	matches := make(map[string]core.CatalogMatch)
	for _, row := range rows {
		score := row.SearchRank
		if score == 0 {
			score = row.Score
		}
		if score <= 0 {
			continue
		}
		matches[row.ID] = core.CatalogMatch{Score: score}
	}
	if len(matches) == 0 {
		return nil
	}
	return matches
}

func catalogCardDetails(card CatalogItem) ([]core.CatalogCardDetail, error) {
	if strings.TrimSpace(card.DetailsJSON) == "" {
		return nil, nil
	}
	var details []core.CatalogCardDetail
	if err := json.Unmarshal([]byte(card.DetailsJSON), &details); err != nil {
		return nil, fmt.Errorf("decode catalog details for %s: %w", card.ID, err)
	}
	return details, nil
}

func catalogCardEdges(card CatalogItem) ([]core.CatalogEdge, error) {
	if strings.TrimSpace(card.EdgesJSON) == "" {
		return nil, nil
	}
	var edges []core.CatalogEdge
	if err := json.Unmarshal([]byte(card.EdgesJSON), &edges); err != nil {
		return nil, fmt.Errorf("decode catalog edges for %s: %w", card.ID, err)
	}
	return edges, nil
}

func catalogGraphQLErrors(errs []core.Error) string {
	messages := make([]string, 0, len(errs))
	for _, err := range errs {
		if strings.TrimSpace(err.Message) != "" {
			messages = append(messages, err.Message)
		}
	}
	if len(messages) == 0 {
		return "catalog GraphQL query failed"
	}
	return strings.Join(messages, "; ")
}

func catalogIntArg(args map[string]any, key string) int {
	if args == nil {
		return 0
	}
	switch v := args[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	default:
		return 0
	}
}

func catalogObjectArg(args map[string]any, key string) (map[string]any, error) {
	if args == nil {
		return nil, nil
	}
	value, ok := args[key]
	if !ok || value == nil {
		return nil, nil
	}
	switch v := value.(type) {
	case map[string]any:
		return v, nil
	case string:
		if strings.TrimSpace(v) == "" {
			return nil, nil
		}
		var out map[string]any
		if err := json.Unmarshal([]byte(v), &out); err == nil {
			return out, nil
		} else {
			return nil, fmt.Errorf("%s must be a JSON object: %w", key, err)
		}
	}
	return nil, fmt.Errorf("%s must be an object, got %T", key, value)
}

func catalogStringMapArg(args map[string]any, key string) (map[string]string, error) {
	if args == nil {
		return nil, nil
	}
	value, ok := args[key]
	if !ok || value == nil {
		return nil, nil
	}
	switch v := value.(type) {
	case map[string]string:
		return v, nil
	case map[string]any:
		out := make(map[string]string, len(v))
		for k, val := range v {
			out[k] = fmt.Sprint(val)
		}
		return out, nil
	case string:
		if strings.TrimSpace(v) == "" {
			return nil, nil
		}
		var raw map[string]any
		if err := json.Unmarshal([]byte(v), &raw); err == nil {
			out := make(map[string]string, len(raw))
			for k, val := range raw {
				out[k] = fmt.Sprint(val)
			}
			return out, nil
		} else {
			return nil, fmt.Errorf("%s must be a JSON object: %w", key, err)
		}
	}
	return nil, fmt.Errorf("%s must be an object, got %T", key, value)
}

func catalogBoolArg(args map[string]any, key string) bool {
	if args == nil {
		return false
	}
	switch v := args[key].(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true")
	default:
		return false
	}
}
