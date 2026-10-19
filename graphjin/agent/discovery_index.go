package agent

import (
	"context"
	"sort"
	"strings"

	"github.com/dosco/graphjin/core/v3"
)

const selectionShardSize = 24

func discoveryRootKind(kind string) bool {
	switch kind {
	case "table", "saved_query", "workflow", "entrypoint", "api_operation":
		return true
	}
	return false
}

// CatalogDiscoveryProvider is an optional runtime extension. The index must
// have the same caller visibility as QueryCatalog, never an unfiltered global
// inventory. Custom runtimes without it still receive seed-based preparation.
type CatalogDiscoveryProvider interface {
	CatalogDiscovery(context.Context, map[string]any) (*CatalogDiscoveryIndex, error)
}

// CatalogDiscoveryIndex is immutable metadata, reusable for a catalog revision.
// Its summaries contain no query bodies, raw data, or policy grants.
type CatalogDiscoveryIndex struct {
	Revision  string
	Cards     []core.CatalogCard
	Neighbors map[string][]string
	snapshot  *core.CatalogSnapshot
}

// NewCatalogDiscoveryIndex constructs summaries and resolves graph node IDs to
// card IDs once, instead of scanning the graph during each expansion.
func NewCatalogDiscoveryIndex(snap *core.CatalogSnapshot) *CatalogDiscoveryIndex {
	index := &CatalogDiscoveryIndex{Neighbors: map[string][]string{}, snapshot: snap}
	if snap == nil {
		return index
	}
	index.Revision = snap.Revision
	index.Cards = SummarizeCatalogCards(snap.Cards)
	columns := map[string][]string{}
	for _, card := range index.Cards {
		if card.Kind == "column" {
			key := card.DatabaseName + "/" + card.SchemaName + "/" + card.TableName
			columns[key] = append(columns[key], card.ColumnName)
		}
	}
	for i := range index.Cards {
		card := &index.Cards[i]
		if card.Kind == "table" {
			key := card.DatabaseName + "/" + card.SchemaName + "/" + card.TableName
			if names := columns[key]; len(names) > 0 {
				sort.Strings(names)
				card.Summary += " Columns: " + truncateSelectionText(strings.Join(names, ", "), 1024)
			}
		}
	}
	sort.Slice(index.Cards, func(i, j int) bool { return index.Cards[i].ID < index.Cards[j].ID })
	nodes := map[string]string{}
	visible := map[string]bool{}
	for _, card := range index.Cards {
		visible[card.ID] = true
	}
	for _, node := range snap.Nodes {
		if visible[node.CardID] {
			nodes[node.ID] = node.CardID
		}
	}
	for _, edge := range snap.Edges {
		from, to := nodes[edge.FromID], nodes[edge.ToID]
		if from != "" && to != "" && from != to {
			index.Neighbors[from] = appendUniqueString(index.Neighbors[from], to)
			index.Neighbors[to] = appendUniqueString(index.Neighbors[to], from)
		}
	}
	for id := range index.Neighbors {
		sort.Strings(index.Neighbors[id])
	}
	return index
}

// Restrict retains only cards and edges present in the caller's snapshot. This
// also admits caller-owned overlays absent from the shared base revision.
func (index *CatalogDiscoveryIndex) Restrict(snap *core.CatalogSnapshot) *CatalogDiscoveryIndex {
	// Revision equality alone is insufficient: a caller projection can hide a
	// column or an edge without changing its source revision. Reuse only the
	// exact immutable snapshot; rebuild overlays before composing summaries.
	if index != nil && snap != nil && index.snapshot == snap {
		return index
	}
	return NewCatalogDiscoveryIndex(snap)
}

type discoveryGroup struct {
	ID      string             `json:"id"`
	Summary string             `json:"summary"`
	Cards   []core.CatalogCard `json:"-"`
}

func discoveryGroups(cards []core.CatalogCard) []discoveryGroup {
	var groups []discoveryGroup
	// Keep catalog kind/schema boundaries, then shard deterministically. Every
	// card appears in a group; keyword search never defines the whole universe.
	var current discoveryGroup
	lastKey := ""
	ordered := append([]core.CatalogCard(nil), cards...)
	sort.SliceStable(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if discoveryRootKind(a.Kind) != discoveryRootKind(b.Kind) {
			return discoveryRootKind(a.Kind)
		}
		return a.Kind+"/"+a.DatabaseName+"/"+a.SchemaName+"/"+a.ID < b.Kind+"/"+b.DatabaseName+"/"+b.SchemaName+"/"+b.ID
	})
	flush := func() {
		if len(current.Cards) > 0 {
			groups = append(groups, current)
			current = discoveryGroup{}
		}
	}
	for _, card := range ordered {
		key := card.Kind + "/" + card.DatabaseName + "/" + card.SchemaName
		if key != lastKey || len(current.Cards) >= selectionShardSize {
			flush()
			lastKey = key
			current.ID = card.ID
			current.Summary = key
		}
		current.Cards = append(current.Cards, card)
		current.Summary += "\n" + card.Title + ": " + truncateSelectionText(card.Summary, 160)
	}
	flush()
	return groups
}

func truncateSelectionText(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	for limit > 0 && (value[limit]&0xc0) == 0x80 {
		limit--
	}
	return strings.TrimSpace(value[:limit])
}
