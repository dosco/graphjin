package serv

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/dosco/graphjin/core/v3"
)

type semanticScoredDocument struct {
	doc   semanticDocumentMap
	score float64
}

type catalogRetrievalMetadata struct {
	Mode                   string   `json:"mode"`
	ExactMatch             bool     `json:"exact_match"`
	SemanticCandidateCount int      `json:"semantic_candidate_count"`
	VisibleTableEndpoints  []string `json:"visible_table_endpoints,omitempty"`
	RelationshipPathCount  int      `json:"relationship_path_count"`
	LexicalFallback        bool     `json:"lexical_fallback"`
}

type semanticHintSet struct {
	hints             []core.CatalogCandidateHint
	relationshipPaths [][]string
	indexReady        bool
}

func (s *graphjinService) queryCatalog(ctx context.Context, snapshot *core.CatalogSnapshot, query core.CatalogQuery) (core.CatalogQueryOutput, error) {
	result, _, err := s.queryCatalogDetailed(ctx, snapshot, query)
	return result, err
}

func (s *graphjinService) queryCatalogDetailed(ctx context.Context, snapshot *core.CatalogSnapshot, query core.CatalogQuery) (core.CatalogQueryOutput, catalogRetrievalMetadata, error) {
	metadata := catalogRetrievalMetadata{Mode: "lexical"}
	if snapshot == nil {
		return core.CatalogQueryOutput{}, metadata, nil
	}
	lexical, err := snapshot.QueryResult(query)
	if err != nil || s == nil || s.semantic == nil || strings.TrimSpace(query.Search) == "" {
		return lexical, metadata, err
	}
	if semanticExactIdentifier(query.Search, lexical) {
		metadata.Mode = "lexical_exact"
		metadata.ExactMatch = true
		metadata.VisibleTableEndpoints = semanticVisibleTableEndpoints(lexical.Cards, 4)
		return lexical, metadata, nil
	}
	hintSet, err := s.semantic.hintsDetailed(ctx, snapshot, query)
	if err != nil || !hintSet.indexReady {
		if err != nil {
			s.semantic.warnFallback(err)
		}
		metadata.Mode = "lexical_fallback"
		metadata.LexicalFallback = true
		metadata.VisibleTableEndpoints = semanticVisibleTableEndpoints(lexical.Cards, 4)
		return lexical, metadata, nil
	}
	metadata.Mode = "hybrid"
	if len(hintSet.hints) == 0 {
		metadata.VisibleTableEndpoints = semanticVisibleTableEndpoints(lexical.Cards, 4)
		return lexical, metadata, nil
	}
	result, err := snapshot.QueryResultWithHints(query, hintSet.hints)
	metadata.SemanticCandidateCount = semanticFilteredCandidateCount(result, hintSet.hints)
	metadata.RelationshipPathCount = semanticFilteredRelationshipPathCount(result, hintSet.relationshipPaths)
	metadata.VisibleTableEndpoints = semanticVisibleTableEndpoints(result.Cards, 4)
	return result, metadata, err
}

func (i *semanticCatalogIndex) hints(ctx context.Context, snapshot *core.CatalogSnapshot, query core.CatalogQuery) ([]core.CatalogCandidateHint, error) {
	result, err := i.hintsDetailed(ctx, snapshot, query)
	return result.hints, err
}

func (i *semanticCatalogIndex) hintsDetailed(ctx context.Context, snapshot *core.CatalogSnapshot, query core.CatalogQuery) (semanticHintSet, error) {
	index := i.current()
	if index == nil || index.manifest.ActualDimension <= 0 {
		// The index builds in the background, so the first searches after a cold
		// boot run lexical. That is expected and brief, but it used to be
		// entirely silent while the agent was already being told semantic recall
		// was available.
		i.logBuilding.Do(func() {
			if i.service != nil && i.service.log != nil {
				i.service.log.Infof("semantic catalog index is still building; catalog search is lexical until it is ready")
			}
		})
		return semanticHintSet{}, nil
	}
	result := semanticHintSet{indexReady: true}
	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	vector, err := i.queryVector(queryCtx, query.Search, index.manifest.ActualDimension)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "dimension") {
			i.invalidateDimension()
		}
		return result, err
	}
	if len(vector) != index.manifest.ActualDimension {
		i.invalidateDimension()
		return result, fmt.Errorf("query embedding dimension %d does not match semantic index dimension %d", len(vector), index.manifest.ActualDimension)
	}
	normalizeSemanticVector(vector)
	return i.hintsForVector(ctx, snapshot, query, index, vector), nil
}

func (i *semanticCatalogIndex) hintsForVector(ctx context.Context, snapshot *core.CatalogSnapshot, query core.CatalogQuery, index *semanticPersistedIndex, vector []float32) semanticHintSet {
	result := semanticHintSet{indexReady: index != nil}
	if snapshot == nil || index == nil || len(vector) != index.manifest.ActualDimension {
		return result
	}

	scored := make([]semanticScoredDocument, 0, len(index.docs))
	for _, document := range index.docs {
		if document.Kind == artifactKindAnnotation && !semanticAnnotationVisible(ctx, document.AccountRef) {
			continue
		}
		start := document.VectorOffset
		end := start + index.manifest.ActualDimension
		if start < 0 || end > len(index.vectors) {
			continue
		}
		score := semanticDot(vector, index.vectors[start:end])
		scored = append(scored, semanticScoredDocument{doc: document, score: score})
	}
	if len(scored) == 0 {
		return result
	}
	sort.Slice(scored, func(a, b int) bool {
		if scored[a].score != scored[b].score {
			return scored[a].score > scored[b].score
		}
		return scored[a].doc.Hash < scored[b].doc.Hash
	})
	best := scored[0].score
	background := make([]float64, 0, len(scored)-minSemanticInt(8, len(scored)))
	for _, item := range scored[minSemanticInt(8, len(scored)):] {
		background = append(background, item.score)
	}
	sort.Float64s(background)
	p99 := 0.0
	if len(background) != 0 {
		position := int(math.Ceil(float64(len(background))*0.99)) - 1
		if position < 0 {
			position = 0
		}
		p99 = background[position]
	}
	threshold := math.Max(0.25, p99+0.02)
	qualified := make([]semanticScoredDocument, 0, 64)
	for _, item := range scored {
		if len(qualified) == 64 || item.score < threshold || best-item.score > 0.10 {
			break
		}
		qualified = append(qualified, item)
	}
	if len(qualified) == 0 {
		return result
	}

	cardByID := make(map[string]core.CatalogCard, len(snapshot.Cards))
	for _, card := range snapshot.Cards {
		cardByID[card.ID] = card
	}
	type rankedHint struct {
		id     string
		score  float64
		source string
		why    string
		order  int
	}
	byCard := make(map[string]rankedHint)
	order := 0
	for _, item := range qualified {
		targets := item.doc.TargetCardIDs
		source := "semantic table recall"
		if item.doc.Kind == "column_facet" {
			source = "semantic facet recall"
			if strings.EqualFold(strings.TrimSpace(query.Kind), "column") || semanticWhereKind(query.Where, "column") {
				targets = semanticRankColumns(query.Search, item.doc.MemberColumns, cardByID)
			}
		} else if item.doc.Kind == "concept" {
			source = "semantic concept recall"
		} else if item.doc.Kind == artifactKindAnnotation {
			source = "approved organizational annotation recall"
		}
		for _, cardID := range targets {
			if _, visible := cardByID[cardID]; !visible {
				continue
			}
			order++
			candidate := rankedHint{
				id:     cardID,
				score:  item.score,
				source: source,
				why:    fmt.Sprintf("%s (cosine %.3f)", source, item.score),
				order:  order,
			}
			if existing, ok := byCard[cardID]; !ok || candidate.score > existing.score || (candidate.score == existing.score && candidate.order < existing.order) {
				byCard[cardID] = candidate
			}
		}
	}
	ranked := make([]rankedHint, 0, len(byCard))
	for _, item := range byCard {
		ranked = append(ranked, item)
	}
	sort.Slice(ranked, func(a, b int) bool {
		if ranked[a].score != ranked[b].score {
			return ranked[a].score > ranked[b].score
		}
		return ranked[a].id < ranked[b].id
	})
	hints := make([]core.CatalogCandidateHint, 0, len(ranked)+12)
	for n, item := range ranked {
		hints = append(hints, core.CatalogCandidateHint{
			CardID: item.id,
			Rank:   n + 1,
			Weight: 0.7,
			Score:  item.score,
			Source: item.source,
			Why:    item.why,
		})
	}

	endpoints := make([]string, 0, 4)
	for _, item := range ranked {
		if card := cardByID[item.id]; card.Kind == "table" {
			endpoints = appendUniqueSemantic(endpoints, item.id)
			if len(endpoints) == 4 {
				break
			}
		}
	}
	paths := semanticRelationshipPaths(snapshot, endpoints, 2, 3)
	result.relationshipPaths = paths
	pathRank := 0
	for _, path := range paths {
		for _, cardID := range path {
			if _, exists := byCard[cardID]; exists {
				continue
			}
			pathRank++
			hints = append(hints, core.CatalogCandidateHint{
				CardID: cardID,
				Rank:   pathRank,
				Weight: 0.5,
				Source: "relationship path",
				Why:    "deterministic relationship path through catalog foreign-key relationships",
			})
		}
	}
	result.hints = hints
	return result
}

func (i *semanticCatalogIndex) queryVector(ctx context.Context, query string, expected int) ([]float32, error) {
	vectors, err := i.queryVectors(ctx, []string{query}, expected)
	if err != nil {
		return nil, err
	}
	return vectors[0], nil
}

// queryVectors resolves cache hits locally and sends every miss through one Ax
// embedding call. A malformed response invalidates the whole batch so callers
// can consistently fall back to lexical retrieval for every phrase.
func (i *semanticCatalogIndex) queryVectors(ctx context.Context, queries []string, expected int) ([][]float32, error) {
	if len(queries) == 0 {
		return nil, nil
	}
	vectors := make([][]float32, len(queries))
	keys := make([]string, len(queries))
	misses := make([]string, 0, len(queries))
	missIndexes := make([]int, 0, len(queries))
	for n, query := range queries {
		sum := sha256.Sum256([]byte(strings.TrimSpace(query)))
		keys[n] = hex.EncodeToString(sum[:])
		if vector, ok := i.cache.Get(keys[n]); ok {
			if expected != 0 && len(vector) != expected {
				return nil, fmt.Errorf("cached query embedding dimension %d does not match index dimension %d", len(vector), expected)
			}
			vectors[n] = vector
			continue
		}
		misses = append(misses, query)
		missIndexes = append(missIndexes, n)
	}
	if len(misses) == 0 {
		return vectors, nil
	}
	requested, named, err := i.conf.dimensionCount()
	if err != nil {
		return nil, err
	}
	var dimensions *int
	if named {
		dimensions = &requested
	}
	embedded, err := i.embedder.Embed(ctx, misses, dimensions)
	if err != nil {
		return nil, err
	}
	if len(embedded) != len(misses) {
		return nil, fmt.Errorf("query embedding returned %d vectors, want %d", len(embedded), len(misses))
	}
	for n, vector := range embedded {
		if named && len(vector) != requested {
			return nil, fmt.Errorf("query embedding %d dimension %d does not match configured dimension %d", n, len(vector), requested)
		}
		if expected != 0 && len(vector) != expected {
			return nil, fmt.Errorf("query embedding %d dimension %d does not match index dimension %d", n, len(vector), expected)
		}
	}
	for n, vector := range embedded {
		index := missIndexes[n]
		vectors[index] = vector
		i.cache.Put(keys[index], vector)
	}
	return vectors, nil
}

func (i *semanticCatalogIndex) invalidateDimension() {
	if i == nil {
		return
	}
	i.mu.Lock()
	i.active = nil
	i.forceFullRebuild = true
	i.mu.Unlock()
	i.cache.Reset()
	i.CatalogChanged()
}

func semanticExactIdentifier(search string, result core.CatalogQueryOutput) bool {
	if len(result.Cards) == 0 {
		return false
	}
	needle := semanticNormalizeIdentifier(search)
	if needle == "" {
		return false
	}
	card := result.Cards[0]
	for _, value := range []string{card.ID, card.Title, card.TableName, card.ColumnName, card.SchemaName + "." + card.TableName} {
		if semanticNormalizeIdentifier(value) == needle {
			return true
		}
	}
	return false
}

func semanticNormalizeIdentifier(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "`", "")
	value = strings.ReplaceAll(value, `"`, "")
	return value
}

func semanticVisibleTableEndpoints(cards []core.CatalogCard, limit int) []string {
	if limit <= 0 {
		return nil
	}
	out := make([]string, 0, limit)
	for _, card := range cards {
		if card.Kind != "table" {
			continue
		}
		out = appendUniqueSemantic(out, card.ID)
		if len(out) == limit {
			break
		}
	}
	return out
}

func semanticFilteredCandidateCount(result core.CatalogQueryOutput, hints []core.CatalogCandidateHint) int {
	visible := make(map[string]bool, len(result.Cards))
	for _, card := range result.Cards {
		visible[card.ID] = true
	}
	seen := make(map[string]bool)
	for _, hint := range hints {
		if visible[hint.CardID] && hint.Source != "relationship path" {
			seen[hint.CardID] = true
		}
	}
	return len(seen)
}

func semanticFilteredRelationshipPathCount(result core.CatalogQueryOutput, paths [][]string) int {
	visible := make(map[string]bool, len(result.Cards))
	for _, card := range result.Cards {
		visible[card.ID] = true
	}
	count := 0
	for _, path := range paths {
		complete := len(path) != 0
		for _, id := range path {
			if !visible[id] {
				complete = false
				break
			}
		}
		if complete {
			count++
		}
	}
	return count
}

func semanticWhereKind(where map[string]any, kind string) bool {
	return semanticWhereContainsKind(where, strings.ToLower(strings.TrimSpace(kind)))
}

func semanticWhereContainsKind(value any, kind string) bool {
	switch clause := value.(type) {
	case map[string]any:
		for key, item := range clause {
			switch strings.ToLower(strings.TrimSpace(key)) {
			case "kind":
				if semanticKindPredicate(item, kind) {
					return true
				}
			case "and", "or":
				if semanticWhereContainsKind(item, kind) {
					return true
				}
			}
		}
	case []any:
		for _, item := range clause {
			if semanticWhereContainsKind(item, kind) {
				return true
			}
		}
	}
	return false
}

func semanticKindPredicate(value any, kind string) bool {
	switch predicate := value.(type) {
	case string:
		return strings.EqualFold(strings.TrimSpace(predicate), kind)
	case map[string]any:
		for operator, item := range predicate {
			switch strings.ToLower(strings.TrimSpace(operator)) {
			case "eq", "ieq":
				if strings.EqualFold(strings.TrimSpace(fmt.Sprint(item)), kind) {
					return true
				}
			case "in":
				if semanticKindPredicate(item, kind) {
					return true
				}
			}
		}
	case []any:
		for _, item := range predicate {
			if strings.EqualFold(strings.TrimSpace(fmt.Sprint(item)), kind) {
				return true
			}
		}
	}
	return false
}

func semanticRankColumns(query string, ids []string, cards map[string]core.CatalogCard) []string {
	terms := strings.Fields(strings.ToLower(query))
	out := append([]string(nil), ids...)
	sort.Slice(out, func(a, b int) bool {
		left, right := cards[out[a]], cards[out[b]]
		leftScore := semanticColumnQueryScore(left, terms)
		rightScore := semanticColumnQueryScore(right, terms)
		if leftScore != rightScore {
			return leftScore > rightScore
		}
		return left.ID < right.ID
	})
	return out
}

func semanticColumnQueryScore(card core.CatalogCard, terms []string) int {
	name := strings.ToLower(card.ColumnName)
	score := 0
	for _, term := range terms {
		if term == name {
			score += 10
		} else if strings.Contains(name, term) {
			score += 3
		}
	}
	if strings.Contains(strings.ToLower(card.Summary), "primary") {
		score++
	}
	return score
}

type semanticGraphEdge struct {
	to           string
	relationship string
}

func semanticRelationshipPaths(snapshot *core.CatalogSnapshot, endpoints []string, maxPaths, maxEdges int) [][]string {
	if snapshot == nil || len(endpoints) < 2 || maxPaths <= 0 || maxEdges <= 0 {
		return nil
	}
	tableByKey := make(map[string]string)
	visible := make(map[string]core.CatalogCard, len(snapshot.Cards))
	for _, card := range snapshot.Cards {
		visible[card.ID] = card
		if card.Kind == "table" {
			tableByKey[semanticTableKey(card.DatabaseName, card.SchemaName, card.TableName)] = card.ID
		}
	}
	graph := make(map[string][]semanticGraphEdge)
	for _, card := range snapshot.Cards {
		if card.Kind != "relationship" {
			continue
		}
		var relationship core.MetadataRelationship
		if json.Unmarshal([]byte(card.EvidenceJSON), &relationship) != nil {
			continue
		}
		from := tableByKey[semanticTableKey(relationship.FromDatabaseName, relationship.FromSchemaName, relationship.FromTableName)]
		to := tableByKey[semanticTableKey(relationship.ToDatabaseName, relationship.ToSchemaName, relationship.ToTableName)]
		if from == "" || to == "" {
			continue
		}
		graph[from] = append(graph[from], semanticGraphEdge{to: to, relationship: card.ID})
		graph[to] = append(graph[to], semanticGraphEdge{to: from, relationship: card.ID})
	}
	for node := range graph {
		sort.Slice(graph[node], func(a, b int) bool {
			if graph[node][a].to != graph[node][b].to {
				return graph[node][a].to < graph[node][b].to
			}
			return graph[node][a].relationship < graph[node][b].relationship
		})
	}
	type foundPath struct {
		key   string
		cards []string
		edges int
	}
	var found []foundPath
	for a := 0; a < len(endpoints); a++ {
		for b := a + 1; b < len(endpoints); b++ {
			cards, edges := semanticBFSPath(graph, endpoints[a], endpoints[b], maxEdges)
			if len(cards) == 0 {
				continue
			}
			filtered := make([]string, 0, len(cards))
			for _, cardID := range cards {
				if _, ok := visible[cardID]; ok && cardID != endpoints[a] && cardID != endpoints[b] {
					filtered = append(filtered, cardID)
				}
			}
			if len(filtered) != 0 {
				found = append(found, foundPath{key: strings.Join(filtered, "|"), cards: filtered, edges: edges})
			}
		}
	}
	sort.Slice(found, func(a, b int) bool {
		if found[a].edges != found[b].edges {
			return found[a].edges < found[b].edges
		}
		return found[a].key < found[b].key
	})
	seen := make(map[string]struct{})
	out := make([][]string, 0, maxPaths)
	for _, item := range found {
		if _, ok := seen[item.key]; ok {
			continue
		}
		seen[item.key] = struct{}{}
		out = append(out, item.cards)
		if len(out) == maxPaths {
			break
		}
	}
	return out
}

func semanticBFSPath(graph map[string][]semanticGraphEdge, start, goal string, maxEdges int) ([]string, int) {
	type state struct {
		table string
		cards []string
		edges int
	}
	queue := []state{{table: start}}
	visited := map[string]int{start: 0}
	for len(queue) != 0 {
		current := queue[0]
		queue = queue[1:]
		if current.table == goal {
			return current.cards, current.edges
		}
		if current.edges == maxEdges {
			continue
		}
		for _, edge := range graph[current.table] {
			nextEdges := current.edges + 1
			if previous, ok := visited[edge.to]; ok && previous <= nextEdges {
				continue
			}
			visited[edge.to] = nextEdges
			cards := append(append([]string(nil), current.cards...), edge.relationship, edge.to)
			queue = append(queue, state{table: edge.to, cards: cards, edges: nextEdges})
		}
	}
	return nil, 0
}

func semanticDot(left, right []float32) float64 {
	var total float64
	for n := range left {
		total += float64(left[n]) * float64(right[n])
	}
	return total
}

func minSemanticInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
