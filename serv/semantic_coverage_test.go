package serv

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/dosco/graphjin/core/v3"
)

type coverageBatchEmbedder struct {
	mu        sync.Mutex
	batches   [][]string
	dimension int
	err       error
	block     bool
}

func (e *coverageBatchEmbedder) Embed(ctx context.Context, texts []string, _ *int) ([][]float32, error) {
	e.mu.Lock()
	e.batches = append(e.batches, append([]string(nil), texts...))
	err, block, dimension := e.err, e.block, e.dimension
	e.mu.Unlock()
	if block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, err
	}
	if dimension == 0 {
		dimension = 4
	}
	out := make([][]float32, len(texts))
	for n, text := range texts {
		vector := make([]float32, dimension)
		lower := strings.ToLower(text)
		if strings.Contains(lower, "customer") || strings.Contains(lower, "client") || strings.Contains(lower, "buyer") {
			vector[0] = 1
		}
		if strings.Contains(lower, "product") || strings.Contains(lower, "purchase") || strings.Contains(lower, "bought") {
			if dimension > 1 {
				vector[1] = 1
			}
		}
		if semanticVectorIsZero(vector) {
			vector[dimension-1] = 1
		}
		out[n] = vector
	}
	return out, nil
}

func (e *coverageBatchEmbedder) calls() [][]string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([][]string, len(e.batches))
	for n := range e.batches {
		out[n] = append([]string(nil), e.batches[n]...)
	}
	return out
}

func semanticCoverageFixture(t *testing.T, embedder SemanticEmbeddingClient) (*graphjinService, *core.CatalogSnapshot) {
	t.Helper()
	table := func(id, name string) core.CatalogCard {
		return core.CatalogCard{ID: id, Kind: "table", DatabaseName: "app", SchemaName: "public", TableName: name, Title: name, Summary: name + " records"}
	}
	snapshot := &core.CatalogSnapshot{Revision: "coverage-v1", Cards: []core.CatalogCard{
		table("table:customers", "customers"),
		table("table:orders", "orders"),
		table("table:order_items", "order_items"),
		table("table:products", "products"),
	}}
	for n, relationship := range []core.MetadataRelationship{
		{FromDatabaseName: "app", FromSchemaName: "public", FromTableName: "orders", FromColumnName: "customer_id", ToDatabaseName: "app", ToSchemaName: "public", ToTableName: "customers", ToColumnName: "id"},
		{FromDatabaseName: "app", FromSchemaName: "public", FromTableName: "order_items", FromColumnName: "order_id", ToDatabaseName: "app", ToSchemaName: "public", ToTableName: "orders", ToColumnName: "id"},
		{FromDatabaseName: "app", FromSchemaName: "public", FromTableName: "order_items", FromColumnName: "product_id", ToDatabaseName: "app", ToSchemaName: "public", ToTableName: "products", ToColumnName: "id"},
	} {
		evidence, err := json.Marshal(relationship)
		if err != nil {
			t.Fatal(err)
		}
		snapshot.Cards = append(snapshot.Cards, core.CatalogCard{
			ID: "relationship:" + string(rune('a'+n)), Kind: "relationship", Title: "foreign key", Summary: "database foreign-key relationship", EvidenceJSON: string(evidence),
		})
	}
	docs := []semanticDocumentMap{
		{Hash: "customers", Kind: "table_identity", TargetCardIDs: []string{"table:customers", "table:hidden"}, VectorOffset: 0},
		{Hash: "products", Kind: "table_identity", TargetCardIDs: []string{"table:products"}, VectorOffset: 4},
	}
	semantic := &semanticCatalogIndex{
		conf:     SemanticCatalogSearchConfig{Dimensions: "default"},
		embedder: embedder,
		cache:    newSemanticQueryLRU(),
		active: &semanticPersistedIndex{
			manifest: semanticIndexManifest{ActualDimension: 4, CatalogRevision: snapshot.Revision, DocumentCount: len(docs)},
			docs:     docs,
			vectors:  []float32{1, 0, 0, 0, 0, 1, 0, 0},
		},
	}
	return &graphjinService{semantic: semantic}, snapshot
}

func TestSemanticCoverageUsesOneBatchCachesMissesAndReturnsRealPath(t *testing.T) {
	embedder := &coverageBatchEmbedder{}
	service, snapshot := semanticCoverageFixture(t, embedder)
	searches := []string{"customers and products bought", "customer buyers", "products purchased"}
	result, groups, retrieval, err := service.queryCatalogCoverage(context.Background(), snapshot, core.CatalogQuery{Limit: 20, Explain: true}, searches)
	if err != nil {
		t.Fatal(err)
	}
	if calls := embedder.calls(); len(calls) != 1 || !reflect.DeepEqual(calls[0], searches) {
		t.Fatalf("embedding calls = %#v, want one three-phrase batch", calls)
	}
	if len(groups) != 3 || retrieval.Mode != "coverage_hybrid" || retrieval.LexicalFallback {
		t.Fatalf("coverage metadata = %+v, groups=%d", retrieval, len(groups))
	}
	ids := make(map[string]bool)
	for _, card := range result.Cards {
		ids[card.ID] = true
	}
	for _, id := range []string{"table:customers", "table:orders", "table:order_items", "table:products", "relationship:a", "relationship:b", "relationship:c"} {
		if !ids[id] {
			t.Fatalf("coverage missing %s: %+v", id, result.Cards)
		}
	}
	if ids["table:hidden"] || containsSemanticString(retrieval.VisibleTableEndpoints, "table:hidden") {
		t.Fatalf("invisible semantic target leaked: cards=%+v metadata=%+v", result.Cards, retrieval)
	}
	if retrieval.RelationshipPathCount == 0 || !strings.Contains(result.Matches["relationship:a"].Why, "relationship path") {
		t.Fatalf("real relationship path was not explained: metadata=%+v matches=%+v", retrieval, result.Matches)
	}

	second := []string{"customer buyers", "products purchased", "buyer accounts"}
	if _, _, _, err := service.queryCatalogCoverage(context.Background(), snapshot, core.CatalogQuery{Limit: 20}, second); err != nil {
		t.Fatal(err)
	}
	calls := embedder.calls()
	if len(calls) != 2 || !reflect.DeepEqual(calls[1], []string{"buyer accounts"}) {
		t.Fatalf("cache misses were not batched selectively: %#v", calls)
	}

	filtered, filteredGroups, filteredMeta, err := service.queryCatalogCoverage(context.Background(), snapshot, core.CatalogQuery{Kind: "table", Limit: 20}, searches)
	if err != nil {
		t.Fatal(err)
	}
	for _, card := range filtered.Cards {
		if card.Kind != "table" {
			t.Fatalf("kind filter leaked %s card %s", card.Kind, card.ID)
		}
	}
	if filteredMeta.RelationshipPathCount != 0 {
		t.Fatalf("filtered relationship path leaked through metadata: %+v", filteredMeta)
	}
	for _, group := range filteredGroups {
		if group.Retrieval.RelationshipPathCount != 0 {
			t.Fatalf("filtered group path leaked through metadata: %+v", group)
		}
	}
}

func TestCoffeeRoasteryServiceRuntimeCoverageBatch(t *testing.T) {
	embedder := &coverageBatchEmbedder{}
	service, snapshot := semanticCoverageFixture(t, embedder)
	runtime := &serviceAgentRuntime{service: service, conf: gjagent.Config{CatalogDefaultLimit: 20}}
	value, err := runtime.queryCatalogSnapshot(context.Background(), snapshot, map[string]any{
		"searches": []any{"customers and products bought", "customer buyers", "products purchased"},
		"where":    map[string]any{"kind": map[string]any{"in": []any{"table", "relationship"}}},
		"limit":    20,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, ok := value.(agentCatalogResult)
	if !ok {
		t.Fatalf("service runtime returned %T", value)
	}
	if len(embedder.calls()) != 1 || len(embedder.calls()[0]) != 3 {
		t.Fatalf("service runtime embedding calls = %#v, want one three-input batch", embedder.calls())
	}
	if result.Retrieval == nil || result.Retrieval.Mode != "coverage_hybrid" || len(result.Coverage) != 3 {
		t.Fatalf("service runtime omitted coverage metadata: %+v", result)
	}
	if result.Facets["table"] != 4 || result.Facets["relationship"] != 3 {
		t.Fatalf("service runtime facets = %+v, want the full coverage union", result.Facets)
	}
	next, ok := result.Next.(map[string]any)
	if !ok {
		t.Fatalf("coverage next = %T, want map", result.Next)
	}
	nextArgs, _ := next["args"].(map[string]any)
	nextIDs, _ := nextArgs["ids"].([]string)
	for _, id := range []string{"table:customers", "table:products", "relationship:a"} {
		if !containsSemanticString(nextIDs, id) {
			t.Fatalf("coverage next ids missing %s: %#v", id, nextIDs)
		}
	}
	if containsSemanticString(nextIDs, "table:hidden") {
		t.Fatalf("coverage next ids leaked invisible target: %#v", nextIDs)
	}
	ids := make(map[string]bool, len(result.Cards))
	for _, card := range result.Cards {
		ids[card.ID] = true
	}
	for _, id := range []string{"table:customers", "table:products", "table:orders", "table:order_items"} {
		if !ids[id] {
			t.Fatalf("service runtime coverage missing %s: %+v", id, result.Cards)
		}
	}
	if _, err := runtime.queryCatalogSnapshot(context.Background(), snapshot, map[string]any{"ids": []any{}}); err == nil || !strings.Contains(err.Error(), "at least one non-empty") {
		t.Fatalf("empty detail ids error = %v", err)
	}
}

func TestSemanticCoverageExactIdentifiersSkipEmbeddingAndStayPinned(t *testing.T) {
	embedder := &coverageBatchEmbedder{}
	service, snapshot := semanticCoverageFixture(t, embedder)
	result, _, retrieval, err := service.queryCatalogCoverage(context.Background(), snapshot, core.CatalogQuery{Limit: 10}, []string{"customers", "products"})
	if err != nil {
		t.Fatal(err)
	}
	if len(embedder.calls()) != 0 {
		t.Fatalf("exact identifiers called the embedding model: %#v", embedder.calls())
	}
	if len(result.Cards) < 2 || result.Cards[0].ID != "table:customers" || result.Cards[1].ID != "table:products" || !retrieval.ExactMatch {
		t.Fatalf("exact identifiers were not pinned: cards=%+v metadata=%+v", result.Cards, retrieval)
	}
}

func TestSemanticCoverageProviderFailuresDimensionsAndCancellationFallBackLexically(t *testing.T) {
	for _, test := range []struct {
		name     string
		embedder *coverageBatchEmbedder
		context  func() (context.Context, context.CancelFunc)
	}{
		{name: "provider", embedder: &coverageBatchEmbedder{err: errors.New("provider unavailable")}},
		{name: "dimension", embedder: &coverageBatchEmbedder{dimension: 3}},
		{name: "cancel", embedder: &coverageBatchEmbedder{block: true}, context: func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), 20*time.Millisecond)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, snapshot := semanticCoverageFixture(t, test.embedder)
			ctx, cancel := context.WithCancel(context.Background())
			if test.context != nil {
				cancel()
				ctx, cancel = test.context()
			}
			defer cancel()
			started := time.Now()
			result, groups, retrieval, err := service.queryCatalogCoverage(ctx, snapshot, core.CatalogQuery{Limit: 10}, []string{"customer records", "product records"})
			if err != nil {
				t.Fatal(err)
			}
			if time.Since(started) > time.Second {
				t.Fatalf("fallback took too long: %s", time.Since(started))
			}
			if !retrieval.LexicalFallback || retrieval.Mode != "coverage_lexical_fallback" || len(groups) != 2 {
				t.Fatalf("fallback metadata = %+v groups=%+v", retrieval, groups)
			}
			if len(result.Cards) == 0 {
				t.Fatal("provider failure discarded lexical groups")
			}
			for _, group := range groups {
				if !group.Retrieval.LexicalFallback {
					t.Fatalf("group did not report lexical fallback: %+v", group)
				}
			}
		})
	}
}

func containsSemanticString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
