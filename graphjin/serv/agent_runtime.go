package serv

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/dosco/graphjin/core/v3"
)

func (c *Config) agentEnabled() bool {
	return c != nil && c.Agent.Enabled && c.agenticSurfaceEnabled()
}

// agentOnlyMCP reports whether the MCP surface should expose only the
// ask_graphjin_agent tool. When the agent is enabled it is the single front door
// that orchestrates the low-level tools (catalog, execution, schema) internally,
// so those primitives are hidden to keep the surface minimal — unless
// mcp.include_tools_with_agent opts them back in for a client that must drive them
// directly.
func (c *Config) agentOnlyMCP() bool {
	return c != nil && c.agentEnabled() && !c.MCP.IncludeToolsWithAgent
}

func agentConfigFromService(conf *Config) gjagent.Config {
	if conf == nil {
		return gjagent.Config{}
	}
	return gjagent.Config{
		Enabled:             conf.Agent.Enabled,
		Provider:            conf.Agent.Provider,
		Model:               conf.Agent.Model,
		APIKeyEnv:           conf.Agent.APIKeyEnv,
		BaseURL:             conf.Agent.BaseURL,
		Sampling:            conf.Agent.Sampling,
		MaxSteps:            conf.Agent.MaxSteps,
		TimeoutSeconds:      gjagent.EffectiveTimeoutSeconds(conf.Agent.TimeoutSeconds),
		ReadOnly:            conf.Agent.ReadOnly,
		ReturnTrace:         conf.Agent.ReturnTrace,
		SeedLimit:           conf.Agent.SeedLimit,
		CatalogDefaultLimit: conf.Agent.CatalogDefaultLimit,
	}
}

type graphjinAgentRunner interface {
	Run(context.Context, gjagent.Request) (gjagent.Response, error)
}

var newGraphJinAgentRunner = func(s *graphjinService, conf gjagent.Config, opts ...gjagent.Option) (graphjinAgentRunner, error) {
	if s == nil {
		return nil, gjagent.ErrMissingGraphJin
	}
	rt, err := newServiceAgentRuntime(s, conf)
	if err != nil {
		return nil, err
	}
	agentOpts := []gjagent.Option{gjagent.WithRuntime(rt)}
	if s.agentClientFactory != nil {
		agentOpts = append(agentOpts, gjagent.WithClientFactory(s.agentClientFactory))
	}
	if s.semantic != nil {
		agentOpts = append(agentOpts, gjagent.WithCatalogSearchFeatures(gjagent.CatalogSearchFeatures{
			SemanticRecall: true,
			CoverageBatch:  true,
		}))
	}
	agentOpts = append(agentOpts, opts...)
	return gjagent.New(s.gj, conf, agentOpts...)
}

type serviceAgentRuntime struct {
	service *graphjinService
	base    gjagent.GraphRuntime
	conf    gjagent.Config
}

type agentCatalogResult struct {
	GeneratedAt string                       `json:"generated_at"`
	Revision    string                       `json:"revision,omitempty"`
	Count       int                          `json:"count"`
	Limit       int                          `json:"limit,omitempty"`
	Truncated   bool                         `json:"truncated,omitempty"`
	Cards       []core.CatalogCard           `json:"cards"`
	Details     []core.CatalogCardDetail     `json:"details,omitempty"`
	Edges       []core.CatalogEdge           `json:"edges,omitempty"`
	Matches     map[string]core.CatalogMatch `json:"matches,omitempty"`
	Facets      map[string]int               `json:"facets,omitempty"`
	Retrieval   *catalogRetrievalMetadata    `json:"retrieval,omitempty"`
	Coverage    []agentCatalogCoverageGroup  `json:"coverage,omitempty"`
	Next        any                          `json:"next,omitempty"`
}

func newServiceAgentRuntime(s *graphjinService, conf gjagent.Config) (gjagent.GraphRuntime, error) {
	if s == nil || s.gj == nil {
		return nil, gjagent.ErrMissingGraphJin
	}
	base, err := gjagent.NewCoreRuntime(s.gj, conf)
	if err != nil {
		return nil, err
	}
	return &serviceAgentRuntime{service: s, base: base, conf: conf}, nil
}

func (r *serviceAgentRuntime) GraphQLHelp(ctx context.Context, args map[string]any) (any, error) {
	if err := r.requireCatalogAccess(ctx); err != nil {
		return nil, err
	}
	topic := strings.ToLower(stringArg(args, "for"))
	if topic == "" {
		topic = "discovery"
	}
	snap, err := r.service.catalogSnapshotForContext(ctx)
	if err != nil {
		return nil, err
	}
	id := "help:" + topic
	if card, ok := snap.Card(id); ok {
		return agentCatalogResult{
			GeneratedAt: time.Now().UTC().Format(time.RFC3339),
			Revision:    snap.Revision,
			Count:       1,
			Cards:       []core.CatalogCard{card},
			Details:     snap.CardDetails(id),
			Edges:       snap.CardEdges(id),
			Next:        agentCatalogNext("query_catalog", "Continue with filtered catalog discovery or execute an approved saved query when this detail row supports it."),
		}, nil
	}
	q := core.CatalogQuery{Search: topic, Kind: "help", Limit: 5, Explain: true}
	result, err := r.service.queryCatalog(ctx, snap, q)
	if err != nil {
		return nil, err
	}
	return agentCatalogResult{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Revision:    snap.Revision,
		Count:       len(result.Cards),
		Limit:       q.Limit,
		Truncated:   len(result.Cards) >= q.Limit,
		Cards:       gjagent.SummarizeCatalogCards(result.Cards),
		Matches:     result.Matches,
		Facets:      result.Facets,
		Next:        agentCatalogNext("query_catalog", "Inspect a returned help row by id, or continue with filtered catalog discovery."),
	}, nil
}

func (r *serviceAgentRuntime) QueryCatalog(ctx context.Context, args map[string]any) (any, error) {
	if err := r.requireCatalogAccess(ctx); err != nil {
		return nil, err
	}
	snap, err := r.service.catalogSnapshotForContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.queryCatalogSnapshot(ctx, snap, args)
}

// queryCatalogSnapshot is the service-runtime catalog seam used after caller
// authorization and visibility have produced a scoped snapshot. Keeping the
// batch path here makes it testable without exposing `searches` through MCP.
func (r *serviceAgentRuntime) queryCatalogSnapshot(ctx context.Context, snap *core.CatalogSnapshot, args map[string]any) (any, error) {
	if ids := agentDetailIDs(args); len(ids) != 0 {
		return agentCatalogDetailResult(snap, ids), nil
	}
	if _, exists := args["ids"]; exists {
		return nil, fmt.Errorf("query_catalog ids requires at least one non-empty catalog id; use the prior result's next.args.ids exactly")
	}
	where, err := catalogObjectArg(args, "where")
	if err != nil {
		return nil, err
	}
	orderBy, err := catalogStringMapArg(args, "order_by")
	if err != nil {
		return nil, err
	}
	q := core.CatalogQuery{
		Search:   stringArg(args, "search"),
		Where:    where,
		OrderBy:  orderBy,
		Explain:  catalogBoolArg(args, "explain"),
		Kind:     stringArg(args, "kind"),
		Database: stringArg(args, "database"),
		Schema:   stringArg(args, "schema"),
		Table:    stringArg(args, "table"),
		Column:   stringArg(args, "column"),
		Limit:    catalogIntArg(args, "limit"),
	}
	if q.Limit <= 0 {
		q.Limit = r.conf.CatalogDefaultLimit
	}
	if q.Limit <= 0 {
		q.Limit = 20
	}
	if searches := agentCoverageSearches(args); len(searches) != 0 {
		result, groups, retrieval, err := r.service.queryCatalogCoverage(ctx, snap, q, searches)
		if err != nil {
			return nil, err
		}
		return agentCatalogResult{
			GeneratedAt: time.Now().UTC().Format(time.RFC3339),
			Revision:    snap.Revision,
			Count:       len(result.Cards),
			Limit:       q.Limit,
			Truncated:   len(result.Cards) >= q.Limit,
			Cards:       gjagent.SummarizeCatalogCards(result.Cards),
			Matches:     result.Matches,
			Facets:      result.Facets,
			Retrieval:   &retrieval,
			Coverage:    groups,
			Next:        agentCatalogCoverageNext(result),
		}, nil
	}
	result, retrieval, err := r.service.queryCatalogDetailed(ctx, snap, q)
	if err != nil {
		return nil, err
	}
	var retrievalPtr *catalogRetrievalMetadata
	if r.service.semantic != nil && strings.TrimSpace(q.Search) != "" {
		retrievalPtr = &retrieval
	}
	return agentCatalogResult{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Revision:    snap.Revision,
		Count:       len(result.Cards),
		Limit:       q.Limit,
		Truncated:   len(result.Cards) >= q.Limit,
		Cards:       gjagent.SummarizeCatalogCards(result.Cards),
		Matches:     result.Matches,
		Facets:      result.Facets,
		Retrieval:   retrievalPtr,
		Next:        agentCatalogNextForQuery(q, result.Cards),
	}, nil
}

// agentCatalogCoverageNext turns the visible, already-filtered coverage union
// into a bounded deterministic detail handoff. Models should not have to parse
// prose or reconstruct ids from several per-phrase groups. Endpoint tables are
// preferred, followed by cards that core marked as real relationship paths.
func agentCatalogCoverageNext(result core.CatalogQueryOutput) map[string]any {
	next := agentCatalogNext("query_catalog", "Inspect next.args.ids exactly, then answer from those details; adaptive coverage is already complete and must not be repeated.")
	ids := make([]string, 0, 8)
	appendID := func(id string) {
		if id == "" || len(ids) >= 8 {
			return
		}
		for _, existing := range ids {
			if existing == id {
				return
			}
		}
		ids = append(ids, id)
	}
	tableCount := 0
	for _, card := range result.Cards {
		if card.Kind == "table" && tableCount < 4 {
			appendID(card.ID)
			tableCount++
		}
	}
	for _, card := range result.Cards {
		if card.Kind == "relationship" && strings.Contains(strings.ToLower(result.Matches[card.ID].Why), "relationship path") {
			appendID(card.ID)
		}
	}
	if len(ids) == 0 {
		for _, card := range result.Cards {
			appendID(card.ID)
		}
	}
	if len(ids) != 0 {
		next["args"] = map[string]any{"ids": ids}
	}
	return next
}

func agentCoverageSearches(args map[string]any) []string {
	if args == nil {
		return nil
	}
	var values []any
	switch typed := args["searches"].(type) {
	case []any:
		values = typed
	case []string:
		values = make([]any, len(typed))
		for n := range typed {
			values[n] = typed[n]
		}
	default:
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		text, ok := value.(string)
		if !ok {
			return nil
		}
		text = strings.TrimSpace(text)
		if text == "" {
			return nil
		}
		out = append(out, text)
	}
	return out
}

// agentDetailIDs merges the single `id` and batched `ids` detail arguments.
func agentDetailIDs(args map[string]any) []string {
	var out []string
	appendID := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		for _, existing := range out {
			if existing == id {
				return
			}
		}
		out = append(out, id)
	}
	appendID(stringArg(args, "id"))
	switch v := args["ids"].(type) {
	case []string:
		for _, id := range v {
			appendID(id)
		}
	case []any:
		for _, item := range v {
			if id, ok := item.(string); ok {
				appendID(id)
			}
		}
	}
	return out
}

// agentCatalogDetailResult resolves one or more catalog ids to full detail rows
// (caller-scoped snapshot) in a single round-trip.
func agentCatalogDetailResult(snap *core.CatalogSnapshot, ids []string) agentCatalogResult {
	if len(ids) > gjagent.MaxCatalogBatchIDs {
		ids = ids[:gjagent.MaxCatalogBatchIDs]
	}
	out := agentCatalogResult{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Revision:    snap.Revision,
	}
	for _, id := range ids {
		card, ok := snap.Card(id)
		if !ok {
			continue
		}
		out.Cards = append(out.Cards, card)
		out.Details = append(out.Details, snap.CardDetails(id)...)
		out.Edges = append(out.Edges, snap.CardEdges(id)...)
	}
	out.Count = len(out.Cards)
	if out.Count == 1 {
		out.Next = agentCatalogNext("query_catalog", "Use this detail row's evidence, examples, safety notes, and edges before selecting an action.")
	} else if out.Count > 1 {
		out.Next = agentCatalogNext("query_catalog", "Use these detail rows' evidence, examples, safety notes, and edges before selecting an action.")
	}
	return out
}

func (r *serviceAgentRuntime) ValidateWhereClause(ctx context.Context, args map[string]any) (any, error) {
	return r.base.ValidateWhereClause(ctx, args)
}

// ExecuteSavedQuery and ExecuteGraphQL delegate straight to core. GraphJin core is the
// security boundary: it runs every query/mutation as the caller's role (resolved from the
// request context) and enforces access via per-role table rules + RLS — including the gj_*
// control-plane roots (e.g. gj_config is admin-only via modeBlocksRole in
// core/source_access.go). execute_graphql is intentionally available to all callers; there
// is no extra role gate at this layer.
func (r *serviceAgentRuntime) ExecuteSavedQuery(ctx context.Context, args map[string]any) (any, error) {
	if r == nil || r.service == nil {
		return r.base.ExecuteSavedQuery(ctx, args)
	}
	name := stringArg(args, "name")
	if name == "" {
		return nil, fmt.Errorf("query name is required")
	}
	varsJSON, err := agentVariablesJSON(args["variables"])
	if err != nil {
		return nil, err
	}
	var rc core.RequestConfig
	if namespace := stringArg(args, "namespace"); namespace != "" {
		rc.SetNamespace(namespace)
	}
	res, err := r.service.executeSavedQueryByName(ctx, name, varsJSON, &rc)
	return executeAgentResultFromCore(res, err), nil
}

func (r *serviceAgentRuntime) ExecuteGraphQL(ctx context.Context, args map[string]any) (any, error) {
	return r.base.ExecuteGraphQL(ctx, args)
}

func agentVariablesJSON(value any) (json.RawMessage, error) {
	if value == nil {
		return nil, nil
	}
	if raw, ok := value.(json.RawMessage); ok {
		return raw, nil
	}
	if text, ok := value.(string); ok {
		text = strings.TrimSpace(text)
		if text == "" {
			return nil, nil
		}
		if !json.Valid([]byte(text)) {
			return nil, fmt.Errorf("variables must be valid JSON")
		}
		return json.RawMessage(text), nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("invalid variables: %w", err)
	}
	return data, nil
}

func executeAgentResultFromCore(res *core.Result, err error) ExecuteResult {
	result := ExecuteResult{}
	if err != nil {
		result.Errors = []ErrorInfo{{Message: err.Error()}}
		return result
	}
	if res == nil {
		return result
	}
	result.Data = res.Data
	for _, e := range res.Errors {
		result.Errors = append(result.Errors, ErrorInfo{Message: e.Message, Extensions: e.Extensions})
	}
	return result
}

func (r *serviceAgentRuntime) requireCatalogAccess(ctx context.Context) error {
	if r == nil || r.service == nil || r.service.conf == nil || !r.service.conf.Core.IsSourcesUsed() {
		return nil
	}
	if r.service.sourceModeRootAccessAllowed("gj_catalog", runtimeRoleClass(ctx)) {
		return nil
	}
	return fmt.Errorf("gj_catalog is not available to this caller; request authenticated/admin access instead of inventing schema")
}

func agentCatalogNext(tool, reason string) map[string]any {
	return map[string]any{
		"recommended_tool": tool,
		"reason":           reason,
	}
}

func agentCatalogNextForQuery(q core.CatalogQuery, cards []core.CatalogCard) map[string]any {
	count := len(cards)
	kind := agentCatalogQueryKind(q)
	if count == 0 {
		next := agentCatalogNext("query_catalog", "No catalog rows matched. Broaden the search before acting.")
		args := map[string]any{}
		if kind != "" {
			args["kind"] = kind
		}
		if q.Database != "" {
			args["database"] = q.Database
		}
		if q.Table != "" {
			args["table"] = q.Table
		}
		if q.Limit > 0 {
			args["limit"] = q.Limit
		}
		if kind == "saved_query" && q.Search != "" {
			next["reason"] = "No saved_query rows matched the search text. Live row values are not catalog metadata; broaden to query_catalog({kind:\"saved_query\"}), then inspect a returned saved_query:<name> detail before execute_saved_query."
		}
		if len(args) != 0 {
			next["args"] = args
		}
		return next
	}
	if kind == "saved_query" {
		next := agentCatalogNext("query_catalog", "Inspect the most relevant saved_query:<name> row with query_catalog({id:\"saved_query:<name>\"}) before execute_saved_query.")
		if count == 1 && cards[0].ID != "" {
			next["args"] = map[string]any{"id": cards[0].ID}
			next["reason"] = "Inspect this saved query detail before execute_saved_query."
		} else {
			ids := make([]string, 0, len(cards))
			for _, card := range cards {
				if card.ID != "" {
					ids = append(ids, card.ID)
				}
			}
			if len(ids) != 0 {
				next["candidate_ids"] = ids
			}
		}
		return next
	}
	return agentCatalogNext("query_catalog", "Inspect the best returned catalog row with query_catalog({id:\"...\"}) before acting.")
}

func agentCatalogQueryKind(q core.CatalogQuery) string {
	if q.Kind != "" {
		return q.Kind
	}
	if q.Where == nil {
		return ""
	}
	kind, ok := q.Where["kind"].(map[string]any)
	if !ok {
		return ""
	}
	if eq, ok := kind["eq"].(string); ok {
		return eq
	}
	return ""
}
