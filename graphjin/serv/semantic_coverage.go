package serv

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/dosco/graphjin/core/v3"
)

type agentCatalogCoverageGroup struct {
	Search    string                       `json:"search"`
	CardIDs   []string                     `json:"card_ids"`
	Matches   map[string]core.CatalogMatch `json:"matches,omitempty"`
	Retrieval catalogRetrievalMetadata     `json:"retrieval"`
}

type coveragePhraseResult struct {
	search   string
	result   core.CatalogQueryOutput
	metadata catalogRetrievalMetadata
}

// queryCatalogCoverage embeds all uncached, non-exact phrases in one request,
// then runs the existing hybrid retrieval pipeline independently for each
// phrase. Cross-phrase fusion uses ranks only; cosine scores never cross phrase
// boundaries.
func (s *graphjinService) queryCatalogCoverage(ctx context.Context, snapshot *core.CatalogSnapshot, base core.CatalogQuery, searches []string) (core.CatalogQueryOutput, []agentCatalogCoverageGroup, catalogRetrievalMetadata, error) {
	aggregate := catalogRetrievalMetadata{Mode: "coverage_lexical"}
	groups := make([]coveragePhraseResult, len(searches))
	if snapshot == nil {
		return core.CatalogQueryOutput{}, nil, aggregate, nil
	}

	missing := make([]string, 0, len(searches))
	missingGroups := make([]int, 0, len(searches))
	for n, search := range searches {
		query := base
		query.Search = search
		query.Explain = true
		lexical, err := snapshot.QueryResult(query)
		if err != nil {
			return core.CatalogQueryOutput{}, nil, aggregate, err
		}
		groups[n] = coveragePhraseResult{
			search: search,
			result: lexical,
			metadata: catalogRetrievalMetadata{
				Mode:                  "lexical",
				VisibleTableEndpoints: semanticVisibleTableEndpoints(lexical.Cards, 4),
			},
		}
		if semanticExactIdentifier(search, lexical) {
			groups[n].metadata.Mode = "lexical_exact"
			groups[n].metadata.ExactMatch = true
			continue
		}
		missing = append(missing, search)
		missingGroups = append(missingGroups, n)
	}

	var index *semanticPersistedIndex
	if s != nil && s.semantic != nil {
		index = s.semantic.current()
	}
	indexReady := index != nil && index.manifest.ActualDimension > 0
	var vectors [][]float32
	var vectorErr error
	if len(missing) != 0 && indexReady {
		queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		vectors, vectorErr = s.semantic.queryVectors(queryCtx, missing, index.manifest.ActualDimension)
		cancel()
		if vectorErr != nil && strings.Contains(strings.ToLower(vectorErr.Error()), "dimension") {
			s.semantic.invalidateDimension()
		}
	}

	if len(missing) != 0 && (!indexReady || vectorErr != nil) {
		if vectorErr != nil {
			s.semantic.warnFallback(vectorErr)
		}
		aggregate.Mode = "coverage_lexical_fallback"
		aggregate.LexicalFallback = true
		for _, n := range missingGroups {
			groups[n].metadata.Mode = "lexical_fallback"
			groups[n].metadata.LexicalFallback = true
		}
	} else if len(missing) != 0 {
		for vectorIndex, groupIndex := range missingGroups {
			vector := append([]float32(nil), vectors[vectorIndex]...)
			normalizeSemanticVector(vector)
			query := base
			query.Search = groups[groupIndex].search
			query.Explain = true
			hints := s.semantic.hintsForVector(ctx, snapshot, query, index, vector)
			result := groups[groupIndex].result
			var err error
			if len(hints.hints) != 0 {
				result, err = snapshot.QueryResultWithHints(query, hints.hints)
				if err != nil {
					return core.CatalogQueryOutput{}, nil, aggregate, err
				}
			}
			groups[groupIndex].result = result
			groups[groupIndex].metadata = catalogRetrievalMetadata{
				Mode:                   "hybrid",
				SemanticCandidateCount: semanticFilteredCandidateCount(result, hints.hints),
				VisibleTableEndpoints:  semanticVisibleTableEndpoints(result.Cards, 4),
				RelationshipPathCount:  semanticFilteredRelationshipPathCount(result, hints.relationshipPaths),
			}
		}
		aggregate.Mode = "coverage_hybrid"
	}

	merged, paths := fuseCatalogCoverage(snapshot, base, groups)
	aggregate.ExactMatch = coverageHasExact(groups)
	aggregate.SemanticCandidateCount = coverageSemanticCandidateCount(groups)
	aggregate.VisibleTableEndpoints = semanticVisibleTableEndpoints(merged.Cards, 4)
	aggregate.RelationshipPathCount = paths
	publicGroups := make([]agentCatalogCoverageGroup, 0, len(groups))
	for _, group := range groups {
		ids := make([]string, 0, len(group.result.Cards))
		for _, card := range group.result.Cards {
			ids = append(ids, card.ID)
		}
		publicGroups = append(publicGroups, agentCatalogCoverageGroup{
			Search:    group.search,
			CardIDs:   ids,
			Matches:   group.result.Matches,
			Retrieval: group.metadata,
		})
	}
	return merged, publicGroups, aggregate, nil
}

func fuseCatalogCoverage(snapshot *core.CatalogSnapshot, base core.CatalogQuery, groups []coveragePhraseResult) (core.CatalogQueryOutput, int) {
	limit := base.Limit
	if limit <= 0 {
		limit = 20
	}
	type fusedItem struct {
		card  core.CatalogCard
		score float64
		match core.CatalogMatch
	}
	byID := make(map[string]*fusedItem)
	for _, group := range groups {
		for rank, card := range group.result.Cards {
			item := byID[card.ID]
			if item == nil {
				item = &fusedItem{card: card, match: group.result.Matches[card.ID]}
				byID[card.ID] = item
			}
			item.score += 1 / float64(60+rank+1)
		}
	}

	ordered := make([]core.CatalogCard, 0, len(byID))
	selected := make(map[string]bool)
	exact := make(map[string]bool)
	appendCard := func(card core.CatalogCard) {
		if card.ID == "" || selected[card.ID] || len(ordered) >= limit {
			return
		}
		selected[card.ID] = true
		ordered = append(ordered, card)
	}
	// Exact identifiers remain pinned above all fused candidates.
	for _, group := range groups {
		if group.metadata.ExactMatch && len(group.result.Cards) != 0 {
			exact[group.result.Cards[0].ID] = true
			appendCard(group.result.Cards[0])
		}
	}
	// Reserve one distinct coverage anchor per phrase.
	for _, group := range groups {
		if group.metadata.ExactMatch {
			continue
		}
		for _, card := range group.result.Cards {
			if !selected[card.ID] {
				appendCard(card)
				break
			}
		}
	}

	endpoints := make([]string, 0, 4)
	for _, group := range groups {
		for _, id := range group.metadata.VisibleTableEndpoints {
			endpoints = appendUniqueSemantic(endpoints, id)
			if len(endpoints) == 4 {
				break
			}
		}
		if len(endpoints) == 4 {
			break
		}
	}
	pathCards, pathMatches, pathCount := filteredCoveragePaths(snapshot, base, endpoints)
	for rank, card := range pathCards {
		if item := byID[card.ID]; item == nil {
			byID[card.ID] = &fusedItem{card: card, score: 0.5 / float64(60+rank+1), match: pathMatches[card.ID]}
		} else if item.match.Why == "" {
			item.match = pathMatches[card.ID]
			item.score += 0.5 / float64(60+rank+1)
		} else {
			item.score += 0.5 / float64(60+rank+1)
		}
	}

	remaining := make([]*fusedItem, 0, len(byID))
	for id, item := range byID {
		if !selected[id] {
			remaining = append(remaining, item)
		}
	}
	sort.Slice(remaining, func(a, b int) bool {
		if remaining[a].score != remaining[b].score {
			return remaining[a].score > remaining[b].score
		}
		return remaining[a].card.ID < remaining[b].card.ID
	})
	for _, item := range remaining {
		appendCard(item.card)
	}
	for _, card := range pathCards {
		if !selected[card.ID] {
			pathCount = 0
			break
		}
	}

	result := core.CatalogQueryOutput{
		Cards:   ordered,
		Matches: make(map[string]core.CatalogMatch, len(ordered)),
		Facets:  make(map[string]int),
	}
	for _, item := range byID {
		result.Facets[item.card.Kind]++
	}
	for _, card := range ordered {
		match := byID[card.ID].match
		match.Score = byID[card.ID].score
		if exact[card.ID] {
			match.Score += 1_000_000
		}
		match.Score = math.Round(match.Score*1_000_000) / 1_000_000
		if pathMatch, ok := pathMatches[card.ID]; ok {
			match.Why = appendCoverageWhy(match.Why, pathMatch.Why)
		}
		result.Matches[card.ID] = match
	}
	return result, pathCount
}

func filteredCoveragePaths(snapshot *core.CatalogSnapshot, base core.CatalogQuery, endpoints []string) ([]core.CatalogCard, map[string]core.CatalogMatch, int) {
	paths := semanticRelationshipPaths(snapshot, endpoints, 2, 3)
	if len(paths) == 0 {
		return nil, nil, 0
	}
	var cards []core.CatalogCard
	matches := make(map[string]core.CatalogMatch)
	count := 0
	for _, path := range paths {
		query := base
		query.Search = "__graphjin_internal_relationship_path__"
		query.OrderBy = nil
		query.Offset = 0
		query.Limit = len(path)
		query.Explain = true
		hints := make([]core.CatalogCandidateHint, 0, len(path))
		for rank, id := range path {
			hints = append(hints, core.CatalogCandidateHint{CardID: id, Rank: rank + 1, Weight: 100, Source: "relationship path", Why: "deterministic relationship path through catalog foreign-key relationships"})
		}
		filtered, err := snapshot.QueryResultWithHints(query, hints)
		if err != nil || len(filtered.Cards) != len(path) {
			continue
		}
		eligible := make(map[string]bool, len(filtered.Cards))
		for _, card := range filtered.Cards {
			eligible[card.ID] = true
		}
		complete := true
		for _, id := range path {
			if !eligible[id] {
				complete = false
				break
			}
		}
		if !complete {
			continue
		}
		count++
		for _, id := range path {
			card, _ := snapshot.Card(id)
			if !catalogCardIncluded(cards, id) {
				cards = append(cards, card)
				matches[id] = filtered.Matches[id]
			}
		}
	}
	return cards, matches, count
}

func catalogCardIncluded(cards []core.CatalogCard, id string) bool {
	for _, card := range cards {
		if card.ID == id {
			return true
		}
	}
	return false
}

func coverageHasExact(groups []coveragePhraseResult) bool {
	for _, group := range groups {
		if group.metadata.ExactMatch {
			return true
		}
	}
	return false
}

func coverageSemanticCandidateCount(groups []coveragePhraseResult) int {
	total := 0
	for _, group := range groups {
		total += group.metadata.SemanticCandidateCount
	}
	return total
}

func appendCoverageWhy(existing, extra string) string {
	if strings.TrimSpace(existing) == "" {
		return extra
	}
	if strings.TrimSpace(extra) == "" || strings.Contains(existing, extra) {
		return existing
	}
	return fmt.Sprintf("%s; %s", existing, extra)
}
