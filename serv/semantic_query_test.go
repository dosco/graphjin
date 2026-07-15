package serv

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"sync"
	"testing"

	"github.com/dosco/graphjin/core/v3"
)

type retrievalFixtureEmbedder struct {
	mu    sync.Mutex
	calls int
}

func (e *retrievalFixtureEmbedder) Embed(_ context.Context, texts []string, _ *int) ([][]float32, error) {
	e.mu.Lock()
	e.calls += len(texts)
	e.mu.Unlock()
	out := make([][]float32, len(texts))
	for n, text := range texts {
		vector := make([]float32, 6)
		switch strings.ToLower(strings.TrimSpace(text)) {
		case "clients":
			vector[0] = 1
		case "purchases":
			vector[1] = 1
		case "customers and products they bought":
			vector[0], vector[2] = 1, 1
		case "revenue by month":
			vector[3] = 1
		case "prevent users changing records":
			vector[4] = 1
		default:
			vector[5] = 1
		}
		out[n] = vector
	}
	return out, nil
}

func (e *retrievalFixtureEmbedder) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls
}

func TestHybridCatalogRetrievalFixtureAndLexicalFallback(t *testing.T) {
	table := func(id, name string) core.CatalogCard {
		return core.CatalogCard{ID: id, Kind: "table", DatabaseName: "app", SchemaName: "public", TableName: name, Title: name, Summary: name + " records"}
	}
	snapshot := &core.CatalogSnapshot{Revision: "fixture-v1", Cards: []core.CatalogCard{
		table("table:customers", "customers"),
		table("table:orders", "orders"),
		table("table:order_items", "order_items"),
		table("table:products", "products"),
		table("table:monthly_metrics", "monthly_metrics"),
		{ID: "config_recipe:record_permissions", Kind: "config_recipe", Title: "Role mutation permissions", Summary: "Configure role write and delete access"},
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
			ID: "relationship:" + string(rune('a'+n)), Kind: "relationship",
			Title: "foreign key", Summary: "database foreign-key relationship", EvidenceJSON: string(evidence),
		})
	}

	vectors := [][]float32{
		{1, 0, 0, 0, 0, 0},
		{0, 1, 0, 0, 0, 0},
		{0, 0, 1, 0, 0, 0},
		{0, 0, 0, 1, 0, 0},
		{0, 0, 0, 0, 1, 0},
	}
	flat := make([]float32, 0, len(vectors)*6)
	docs := []semanticDocumentMap{
		{Hash: "customers", Kind: "table_identity", TargetCardIDs: []string{"table:customers", "table:hidden"}},
		{Hash: "purchases", Kind: "column_facet", TargetCardIDs: []string{"table:orders", "table:order_items"}},
		{Hash: "products", Kind: "table_identity", TargetCardIDs: []string{"table:products"}},
		{Hash: "metrics", Kind: "column_facet", TargetCardIDs: []string{"table:monthly_metrics"}},
		{Hash: "permissions", Kind: "concept", TargetCardIDs: []string{"config_recipe:record_permissions"}},
	}
	for n := range docs {
		docs[n].VectorOffset = len(flat)
		flat = append(flat, vectors[n]...)
	}
	embedder := &retrievalFixtureEmbedder{}
	semantic := &semanticCatalogIndex{
		conf:     SemanticCatalogSearchConfig{Dimensions: "default"},
		embedder: embedder,
		cache:    newSemanticQueryLRU(),
		active: &semanticPersistedIndex{
			manifest: semanticIndexManifest{ActualDimension: 6, CatalogRevision: snapshot.Revision, DocumentCount: len(docs)},
			docs:     docs, vectors: flat,
		},
	}
	service := &graphjinService{semantic: semantic}

	assertContains := func(search string, required ...string) core.CatalogQueryOutput {
		t.Helper()
		result, err := service.queryCatalog(context.Background(), snapshot, core.CatalogQuery{Search: search, Explain: true, Limit: 50})
		if err != nil {
			t.Fatalf("query %q: %v", search, err)
		}
		ids := make(map[string]bool, len(result.Cards))
		for _, card := range result.Cards {
			ids[card.ID] = true
		}
		for _, id := range required {
			if !ids[id] {
				t.Fatalf("query %q missing %q: %+v", search, id, result.Cards)
			}
		}
		if ids["table:hidden"] {
			t.Fatalf("query %q leaked a caller-invisible card", search)
		}
		return result
	}

	clients := assertContains("clients", "table:customers")
	if why := clients.Matches["table:customers"].Why; !strings.Contains(why, "semantic") {
		t.Fatalf("semantic explanation missing: %q", why)
	}
	assertContains("purchases", "table:orders", "table:order_items")
	assertContains("revenue by month", "table:monthly_metrics")
	assertContains("prevent users changing records", "config_recipe:record_permissions")
	assertContains("customers and products they bought",
		"table:customers", "table:orders", "table:order_items", "table:products",
		"relationship:a", "relationship:b", "relationship:c",
	)

	unrelated, err := service.queryCatalog(context.Background(), snapshot, core.CatalogQuery{Search: "unrelated aardvarks", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(unrelated.Cards) != 0 {
		t.Fatalf("unrelated query injected results: %+v", unrelated.Cards)
	}

	filtered, err := service.queryCatalog(context.Background(), snapshot, core.CatalogQuery{Search: "clients", Kind: "column", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered.Cards) != 0 {
		t.Fatalf("kind filter leaked semantic tables: %+v", filtered.Cards)
	}

	beforeExact := embedder.count()
	exact, err := service.queryCatalog(context.Background(), snapshot, core.CatalogQuery{Search: "customers", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(exact.Cards) == 0 || exact.Cards[0].ID != "table:customers" {
		t.Fatalf("exact lexical identifier lost top-one: %+v", exact.Cards)
	}
	if embedder.count() != beforeExact {
		t.Fatal("exact identifier unnecessarily called the embedding model")
	}

	if math.Abs(float64(vectors[0][0])-1) > 1e-6 {
		t.Fatal("fixture vectors were unexpectedly mutated")
	}
}

func TestCatalogMCPExplainPreservesHybridReasons(t *testing.T) {
	rows := []map[string]any{
		{
			"id":              "table:ops:public.customers",
			"kind":            "table",
			"table_name":      "customers",
			"search_rank":     0.011475,
			"_match_why":      "semantic table recall (cosine 0.912); deterministic shortest path through catalog foreign-key relationships",
			"_matched_fields": []string{"title"},
			"_matched_terms":  []string{"clients"},
		},
	}
	items, err := catalogItemsFromRows(rows)
	if err != nil {
		t.Fatal(err)
	}
	matches := catalogMatchesFromRows(items)
	match, ok := matches["table:ops:public.customers"]
	if !ok {
		t.Fatalf("missing explain match: %+v", matches)
	}
	if !strings.Contains(match.Why, "semantic table recall") || !strings.Contains(match.Why, "relationship") {
		t.Fatalf("hybrid explanation was lost: %+v", match)
	}
	if len(match.MatchedFields) != 1 || match.MatchedFields[0] != "title" || len(match.MatchedTerms) != 1 || match.MatchedTerms[0] != "clients" {
		t.Fatalf("lexical explanation fields were lost: %+v", match)
	}
	encoded, err := json.Marshal(items[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "_match_why") || strings.Contains(string(encoded), "semantic table recall") {
		t.Fatalf("internal match state leaked into the card payload: %s", encoded)
	}
}
