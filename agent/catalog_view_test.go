package agent

import (
	"strings"
	"testing"

	core "github.com/dosco/graphjin/core/v3"
)

func savedQueryCatalogFixture() *core.CatalogSnapshot {
	return &core.CatalogSnapshot{
		Revision: "r1",
		Cards: []core.CatalogCard{
			{ID: "table:app:main.accounts", Kind: "table", Title: "accounts", TableName: "accounts", Summary: "accounts with mrr"},
			{ID: "saved_query:active_account_mrr", Kind: "saved_query", Title: "active_account_mrr", Summary: "account mrr total", GraphQLQuery: `query active_account_mrr { accounts { sum_mrr_cents } }`},
			{ID: "help:saved_queries", Kind: "help", Title: "Saved query help", Summary: "run execute_saved_query"},
			{ID: "capability.execute_saved_query", Kind: "capability", Title: "execute_saved_query", Summary: "Execute a saved allow-list query by name."},
		},
		Details: []core.CatalogCardDetail{
			{ID: "table:app:main.accounts:columns", CardID: "table:app:main.accounts", Section: "key_columns"},
			{ID: "saved_query:active_account_mrr:definition", CardID: "saved_query:active_account_mrr", Section: "saved_query_definition"},
		},
		Nodes: []core.CatalogNode{
			{ID: "node:table:app:main.accounts", CardID: "table:app:main.accounts"},
			{ID: "node:saved_query:active_account_mrr", CardID: "saved_query:active_account_mrr"},
		},
		Edges: []core.CatalogEdge{
			{ID: "e1", FromID: "node:saved_query:active_account_mrr", ToID: "node:table:app:main.accounts", Kind: "reads"},
		},
	}
}

// The data agent adopted a saved query matched by name whether or not it
// answered the question, so its catalog leaves saved queries out entirely:
// the cards, the help card and capability that point at them, their details,
// nodes and edges, and their search hits.
func TestCatalogViewLeavesOutSavedQueries(t *testing.T) {
	full := savedQueryCatalogFixture()
	view := CatalogView(full)
	if len(view.Cards) != 1 || view.Cards[0].ID != "table:app:main.accounts" {
		t.Fatalf("view cards = %+v", view.Cards)
	}
	if len(view.Details) != 1 || len(view.Nodes) != 1 || len(view.Edges) != 0 {
		t.Fatalf("view kept saved-query neighbours: details=%d nodes=%d edges=%d", len(view.Details), len(view.Nodes), len(view.Edges))
	}
	result, err := view.QueryResult(core.CatalogQuery{Search: "account mrr", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	for _, card := range result.Cards {
		if strings.Contains(card.ID, "saved_quer") {
			t.Fatalf("search returned %s", card.ID)
		}
	}
	if len(full.Cards) != 4 {
		t.Fatal("the view must not change the snapshot MCP and REST read")
	}
	plain := &core.CatalogSnapshot{Cards: full.Cards[:1]}
	if CatalogView(plain) != plain {
		t.Fatal("a snapshot with nothing to hide is returned as is")
	}
}

// A cached service snapshot keeps its identity between calls, so its view is
// built once.
func TestCatalogViewsBuildsEachViewOnce(t *testing.T) {
	var views CatalogViews
	snap := savedQueryCatalogFixture()
	first := views.Get(snap)
	if views.Get(snap) != first {
		t.Fatal("the view was rebuilt for the same snapshot")
	}
	if views.Get(savedQueryCatalogFixture()) == first {
		t.Fatal("a different snapshot shared a view")
	}
}

// A model that still asks for saved queries gets told where to go instead of
// an empty result it would retry.
func TestSavedQueryKindSearchNamesTheAuthoringRoute(t *testing.T) {
	next := catalogNextForQuery(core.CatalogQuery{Kind: "saved_query"}, nil)
	if next["reason"] != SavedQueriesAbsentReason || !strings.Contains(SavedQueriesAbsentReason, `query_catalog({kind:"table"})`) {
		t.Fatalf("next = %+v", next)
	}
}
