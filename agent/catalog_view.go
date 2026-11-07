package agent

import (
	"sync"

	core "github.com/dosco/graphjin/core/v3"
)

// The data agent authors every query itself, so it reads a catalog without
// saved queries. A saved-query card offered a finished query matched by its
// name, and the agent adopted it whether or not it answered the question:
// asked which account brings in the most MRR, it ran active_account_mrr and
// reported that one total. Table cards teach the read, the name join and the
// database total those definitions stood in for. MCP clients, REST and the
// production allow-list still use saved queries; only the agent's view
// leaves them out.

// SavedQueriesAbsentReason answers a data-agent catalog query for saved
// queries, which its catalog does not list.
const SavedQueriesAbsentReason = "The data agent's catalog lists no saved queries: author the query yourself from table detail, query_catalog({kind:\"table\"}) then query_catalog({id:\"table:...\"}), and run it with execute_graphql."

// agentHiddenCardIDs are the non-saved_query cards that exist only to point
// at saved queries.
var agentHiddenCardIDs = map[string]bool{
	"help:saved_queries":             true,
	"capability.execute_saved_query": true,
}

func agentHidesCard(card core.CatalogCard) bool {
	return card.Kind == "saved_query" || agentHiddenCardIDs[card.ID]
}

// CatalogView returns the catalog the data agent reads: snap without saved
// queries. It builds a new search index, so callers holding a long-lived
// snapshot should go through a CatalogViews cache.
func CatalogView(snap *core.CatalogSnapshot) *core.CatalogSnapshot {
	return snap.Without(agentHidesCard)
}

// maxCatalogViews bounds a CatalogViews cache. A service keeps a handful of
// caller-scoped snapshots alive at once; past this the cache starts over.
const maxCatalogViews = 32

// CatalogViews caches CatalogView per snapshot, for runtimes whose snapshots
// are themselves cached and so keep their identity between calls.
type CatalogViews struct {
	mu    sync.Mutex
	views map[*core.CatalogSnapshot]*core.CatalogSnapshot
}

// Get returns the agent's view of snap, building it once per snapshot.
func (c *CatalogViews) Get(snap *core.CatalogSnapshot) *core.CatalogSnapshot {
	if snap == nil {
		return nil
	}
	c.mu.Lock()
	if view, ok := c.views[snap]; ok {
		c.mu.Unlock()
		return view
	}
	c.mu.Unlock()

	view := CatalogView(snap)

	c.mu.Lock()
	if c.views == nil || len(c.views) >= maxCatalogViews {
		c.views = make(map[*core.CatalogSnapshot]*core.CatalogSnapshot)
	}
	c.views[snap] = view
	c.mu.Unlock()
	return view
}
