package serv

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dosco/graphjin/core/v3"
	"github.com/mark3labs/mcp-go/mcp"
)

type CatalogQueryResult struct {
	GeneratedAt     string                       `json:"generated_at"`
	Revision        string                       `json:"revision,omitempty"`
	SourceRevisions map[string]string            `json:"source_revisions,omitempty"`
	Count           int                          `json:"count"`
	Cards           []core.CatalogCard           `json:"cards"`
	Matches         map[string]core.CatalogMatch `json:"matches,omitempty"`
	Next            *NextGuidance                `json:"next,omitempty"`
}

type CatalogCardResult struct {
	Card    core.CatalogCard         `json:"card"`
	Details []core.CatalogCardDetail `json:"details,omitempty"`
	Edges   []core.CatalogEdge       `json:"edges,omitempty"`
	Next    *NextGuidance            `json:"next,omitempty"`
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
	ms.srv.AddTool(mcp.NewTool(
		"query_catalog",
		mcp.WithDescription("Search GraphJin's AI-first catalog for schema, relationships, workflows, language features, directives, operators, config, and capabilities. Use search for intelligent full-text ranking and where for precise GraphJin-style filters."),
		mcp.WithString("search",
			mcp.Description("Optional intelligent full-text search. Handles identifiers, @directives, relationship intent, analytics intent, and typo recovery automatically."),
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
		mcp.WithOutputSchema[struct {
			Entrypoints []core.CatalogEntryPoint `json:"entrypoints"`
		}](),
	), ms.handleGetCatalogEntrypoints)

	ms.srv.AddTool(mcp.NewTool(
		"get_catalog_capabilities",
		mcp.WithDescription("List catalog-described GraphJin capabilities and safety notes."),
		mcp.WithOutputSchema[struct {
			Capabilities []core.CatalogCapability `json:"capabilities"`
		}](),
	), ms.handleGetCatalogCapabilities)
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
					"Find reusable workflows with query_catalog(where: {kind: {eq: 'workflow'}}), then inspect metadata with get_catalog_card.",
					"Use get_catalog_card to inspect evidence, examples, and graph edges for a returned item id.",
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
	snapshot, err := ms.catalogSnapshot()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	where, err := catalogObjectArg(args, "where")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	orderBy, err := catalogStringMapArg(args, "order_by")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	q := core.CatalogQuery{
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
	queryResult, err := snapshot.QueryResult(q)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	result := CatalogQueryResult{
		GeneratedAt:     snapshot.GeneratedAt.Format("2006-01-02T15:04:05Z07:00"),
		Revision:        snapshot.Revision,
		SourceRevisions: snapshot.SourceRevisions,
		Count:           len(queryResult.Cards),
		Cards:           queryResult.Cards,
		Matches:         queryResult.Matches,
		Next: ms.newNextGuidance("catalog_results", []NextOption{
			nextOption("get_catalog_card", 1, "Inspect a returned catalog item in detail.", "Use the id of the most relevant catalog item.", []string{"id"}, nil),
			nextOption("validate_where_clause", 2, "Validate filters after choosing a table/column.", "Use for where clauses against discovered schema.", []string{"table", "where"}, []string{"database"}),
		}),
	}
	return ms.toolResultJSON("query_catalog", args, result)
}

func (ms *mcpServer) handleGetCatalogCard(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	id := stringArg(args, "id")
	if id == "" {
		return mcp.NewToolResultError("id is required"), nil
	}

	snapshot, err := ms.catalogSnapshot()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	card, ok := snapshot.Card(id)
	if !ok {
		return mcp.NewToolResultError("catalog item not found"), nil
	}

	result := CatalogCardResult{
		Card:    card,
		Details: snapshot.CardDetails(id),
		Edges:   snapshot.CardEdges(id),
		Next: ms.newNextGuidance("catalog_card", []NextOption{
			nextOption("query_catalog", 1, "Search related catalog items.", "Use search plus where filters to continue discovery.", nil, []string{"search", "where", "order_by", "limit"}),
			nextOption("validate_where_clause", 2, "Validate filters after reviewing schema/operator items.", "Use for table and filter/operator guidance.", []string{"table", "where"}, []string{"database"}),
		}),
	}
	return ms.toolResultJSON("get_catalog_card", args, result)
}

func (ms *mcpServer) handleGetCatalogEntrypoints(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	snapshot, err := ms.catalogSnapshot()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return ms.toolResultJSON("get_catalog_entrypoints", req.GetArguments(), map[string]any{"entrypoints": snapshot.EntryPoints})
}

func (ms *mcpServer) handleGetCatalogCapabilities(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	snapshot, err := ms.catalogSnapshot()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return ms.toolResultJSON("get_catalog_capabilities", req.GetArguments(), map[string]any{"capabilities": snapshot.Capabilities})
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
