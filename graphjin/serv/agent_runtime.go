package serv

import (
	"context"
	"fmt"
	"strings"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/dosco/graphjin/core/v3"
)

func (c *Config) agentEnabled() bool {
	return c != nil && c.Agent.Enabled
}

func agentConfigFromService(conf *Config) gjagent.Config {
	if conf == nil {
		return gjagent.Config{}
	}
	return gjagent.Config{
		Enabled:         conf.Agent.Enabled,
		Provider:        conf.Agent.Provider,
		Model:           conf.Agent.Model,
		APIKeyEnv:       conf.Agent.APIKeyEnv,
		BaseURL:         conf.Agent.BaseURL,
		MaxSteps:        conf.Agent.MaxSteps,
		TimeoutSeconds:  gjagent.EffectiveTimeoutSeconds(conf.Agent.TimeoutSeconds),
		AllowRawGraphQL: conf.Agent.AllowRawGraphQL && conf.MCP.AllowRawQueries,
		AllowMutations:  conf.MCP.AllowMutations,
		ReturnTrace:     conf.Agent.ReturnTrace,
	}
}

type graphjinAgentRunner interface {
	Run(context.Context, gjagent.Request) (gjagent.Response, error)
}

var newGraphJinAgentRunner = func(s *graphjinService, conf gjagent.Config) (graphjinAgentRunner, error) {
	if s == nil {
		return nil, gjagent.ErrMissingGraphJin
	}
	rt, err := newServiceAgentRuntime(s, conf)
	if err != nil {
		return nil, err
	}
	return gjagent.New(s.gj, conf, gjagent.WithRuntime(rt))
}

type serviceAgentRuntime struct {
	service *graphjinService
	base    gjagent.GraphRuntime
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
	return &serviceAgentRuntime{service: s, base: base}, nil
}

func (r *serviceAgentRuntime) GraphQLHelp(ctx context.Context, args map[string]any) (any, error) {
	if err := r.requireCatalogAccess(ctx); err != nil {
		return nil, err
	}
	topic := strings.ToLower(stringArg(args, "for"))
	if topic == "" {
		topic = "discovery"
	}
	snap, err := r.service.catalogSnapshot()
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
	result, err := snap.QueryResult(q)
	if err != nil {
		return nil, err
	}
	return agentCatalogResult{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Revision:    snap.Revision,
		Count:       len(result.Cards),
		Limit:       q.Limit,
		Truncated:   len(result.Cards) >= q.Limit,
		Cards:       result.Cards,
		Matches:     result.Matches,
		Next:        agentCatalogNext("query_catalog", "Inspect a returned help row by id, or continue with filtered catalog discovery."),
	}, nil
}

func (r *serviceAgentRuntime) QueryCatalog(ctx context.Context, args map[string]any) (any, error) {
	if err := r.requireCatalogAccess(ctx); err != nil {
		return nil, err
	}
	snap, err := r.service.catalogSnapshot()
	if err != nil {
		return nil, err
	}
	if id := stringArg(args, "id"); id != "" {
		card, ok := snap.Card(id)
		if !ok {
			return agentCatalogResult{
				GeneratedAt: time.Now().UTC().Format(time.RFC3339),
				Revision:    snap.Revision,
			}, nil
		}
		return agentCatalogResult{
			GeneratedAt: time.Now().UTC().Format(time.RFC3339),
			Revision:    snap.Revision,
			Count:       1,
			Cards:       []core.CatalogCard{card},
			Details:     snap.CardDetails(id),
			Edges:       snap.CardEdges(id),
			Next:        agentCatalogNext("query_catalog", "Use this detail row's evidence, examples, safety notes, and edges before selecting an action."),
		}, nil
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
		q.Limit = 20
	}
	result, err := snap.QueryResult(q)
	if err != nil {
		return nil, err
	}
	return agentCatalogResult{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Revision:    snap.Revision,
		Count:       len(result.Cards),
		Limit:       q.Limit,
		Truncated:   len(result.Cards) >= q.Limit,
		Cards:       result.Cards,
		Matches:     result.Matches,
		Next:        agentCatalogNextForQuery(q, result.Cards),
	}, nil
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
	return r.base.ExecuteSavedQuery(ctx, args)
}

func (r *serviceAgentRuntime) ExecuteGraphQL(ctx context.Context, args map[string]any) (any, error) {
	return r.base.ExecuteGraphQL(ctx, args)
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
