package serv

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dosco/graphjin/core/v3"
	"github.com/spf13/afero"
	"go.uber.org/zap/zaptest"
	_ "modernc.org/sqlite"
)

type deterministicEmbeddingClient struct {
	mu            sync.Mutex
	dimension     int
	calls         int
	texts         int
	active        int
	maxConcurrent int
}

type blockingEmbeddingClient struct{}

func (blockingEmbeddingClient) Embed(ctx context.Context, _ []string, _ *int) ([][]float32, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (c *deterministicEmbeddingClient) Embed(ctx context.Context, texts []string, dimensions *int) ([][]float32, error) {
	c.mu.Lock()
	c.calls++
	c.texts += len(texts)
	c.active++
	if c.active > c.maxConcurrent {
		c.maxConcurrent = c.active
	}
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.active--
		c.mu.Unlock()
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	dimension := c.dimension
	if dimension == 0 && dimensions != nil {
		dimension = *dimensions
	}
	if dimension == 0 {
		dimension = 128
	}
	out := make([][]float32, len(texts))
	for n, text := range texts {
		vector := make([]float32, dimension)
		for _, token := range strings.Fields(strings.ToLower(text)) {
			index := 0
			for _, r := range token {
				index = (index*33 + int(r)) % dimension
			}
			vector[index]++
		}
		if len(vector) != 0 && semanticVectorIsZero(vector) {
			vector[0] = 1
		}
		out[n] = vector
	}
	return out, nil
}

func semanticVectorIsZero(vector []float32) bool {
	for _, value := range vector {
		if value != 0 {
			return false
		}
	}
	return true
}

func (c *deterministicEmbeddingClient) stats() (calls, texts, maxConcurrent int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls, c.texts, c.maxConcurrent
}

func (c *deterministicEmbeddingClient) reset() {
	c.mu.Lock()
	c.calls = 0
	c.texts = 0
	c.maxConcurrent = 0
	c.mu.Unlock()
}

func TestSemanticDimensionPresetsAndValidation(t *testing.T) {
	for _, test := range []struct {
		name  string
		count int
		named bool
	}{
		{name: "tiny", count: 128, named: true},
		{name: "small", count: 256, named: true},
		{name: "medium", count: 512, named: true},
		{name: "default", count: 0, named: false},
	} {
		count, named, err := (SemanticCatalogSearchConfig{Dimensions: test.name}).dimensionCount()
		if err != nil || count != test.count || named != test.named {
			t.Fatalf("preset %s = (%d,%v,%v), want (%d,%v,nil)", test.name, count, named, err, test.count, test.named)
		}
	}
	if _, _, err := (SemanticCatalogSearchConfig{Dimensions: "4096"}).dimensionCount(); err == nil {
		t.Fatal("numeric/unknown dimension should be rejected")
	}
	if err := normalizeDiscoveryAndSemanticConfig(&Config{Serv: Serv{CatalogSearch: CatalogSearchConfig{Semantic: SemanticCatalogSearchConfig{Enabled: true}}}}); err == nil {
		t.Fatal("enabled semantic search without embedding_model should fail")
	}
}

func TestAxSemanticEmbeddingClientAcceptsRuntimeArrays(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			http.Error(w, "unexpected path", http.StatusNotFound)
			return
		}
		var request struct {
			Model      string   `json:"model"`
			Input      []string `json:"input"`
			Dimensions int      `json:"dimensions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if request.Model != "fixture-embedding" || request.Dimensions != 4 || len(request.Input) != 2 {
			http.Error(w, fmt.Sprintf("unexpected request: %+v", request), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "fixture-embedding",
			"data": []any{
				map[string]any{"index": 0, "embedding": []float64{1, 0, 0, 0}},
				map[string]any{"index": 1, "embedding": []float64{0, 1, 0, 0}},
			},
			"usage": map[string]any{"prompt_tokens": 2, "total_tokens": 2},
		})
	}))
	defer server.Close()

	client := newAxSemanticEmbeddingClient(SemanticCatalogSearchConfig{
		Provider:       "openai",
		EmbeddingModel: "fixture-embedding",
		BaseURL:        server.URL + "/v1",
	})
	dimensions := 4
	vectors, err := client.Embed(context.Background(), []string{"clients", "purchases"}, &dimensions)
	if err != nil {
		t.Fatal(err)
	}
	if len(vectors) != 2 || len(vectors[0]) != dimensions || vectors[0][0] != 1 || vectors[1][1] != 1 {
		t.Fatalf("unexpected Ax vectors: %+v", vectors)
	}
}

func TestSemanticEmbeddingFingerprintIncludesSpaceAndDocumentFormat(t *testing.T) {
	base := SemanticCatalogSearchConfig{Provider: "openai", EmbeddingModel: "model-a", BaseURL: "https://embeddings.example/v1/", Dimensions: "tiny"}
	fingerprint, err := semanticEmbeddingFingerprint(base)
	if err != nil {
		t.Fatal(err)
	}
	for name, changed := range map[string]SemanticCatalogSearchConfig{
		"provider":   {Provider: "other", EmbeddingModel: "model-a", BaseURL: base.BaseURL, Dimensions: "tiny"},
		"model":      {Provider: "openai", EmbeddingModel: "model-b", BaseURL: base.BaseURL, Dimensions: "tiny"},
		"endpoint":   {Provider: "openai", EmbeddingModel: "model-a", BaseURL: "https://other.example/v1", Dimensions: "tiny"},
		"dimensions": {Provider: "openai", EmbeddingModel: "model-a", BaseURL: base.BaseURL, Dimensions: "small"},
	} {
		other, err := semanticEmbeddingFingerprint(changed)
		if err != nil {
			t.Fatal(err)
		}
		if other == fingerprint {
			t.Fatalf("%s change did not alter embedding-space fingerprint", name)
		}
	}
}

func TestBuildSemanticDocumentsBoundsWideTablesAndExcludesCallerArtifacts(t *testing.T) {
	metadata := &core.MetadataSnapshot{
		Tables: []core.MetadataTable{{ID: "app.public.metrics", DatabaseName: "app", SchemaName: "public", TableName: "metrics", Type: "table", Comment: "Monthly business metrics"}},
	}
	snapshot := &core.CatalogSnapshot{Cards: []core.CatalogCard{
		{ID: "table:app.public.metrics", Kind: "table", DatabaseName: "app", SchemaName: "public", TableName: "metrics", Title: "app.public.metrics", Summary: "metrics"},
		{ID: "saved_query:private", Kind: "saved_query", Title: "private query", Summary: "caller owned", OwnerSource: "user:123", Source: "artifact"},
	}}
	for n := 0; n < 300; n++ {
		column := core.MetadataColumn{
			ID: fmt.Sprintf("app.public.metrics.value_%03d", n), TableID: "app.public.metrics",
			DatabaseName: "app", SchemaName: "public", TableName: "metrics",
			ColumnName: fmt.Sprintf("revenue_value_%03d", n), Type: "numeric", Ordinal: n,
		}
		metadata.Columns = append(metadata.Columns, column)
		snapshot.Cards = append(snapshot.Cards, core.CatalogCard{
			ID: "column:" + column.ID, Kind: "column", DatabaseName: "app", SchemaName: "public", TableName: "metrics", ColumnName: column.ColumnName,
			Title: column.ColumnName, Summary: "numeric measure",
		})
	}
	documents := buildSemanticDocuments(metadata, snapshot)
	if len(documents) > 12 {
		t.Fatalf("one 300-column table produced %d semantic documents, want at most 12", len(documents))
	}
	for _, document := range documents {
		for _, target := range document.TargetCardIDs {
			if target == "saved_query:private" {
				t.Fatal("caller-owned artifact leaked into semantic documents")
			}
		}
	}
}

func TestSemanticDocumentsIgnoreUnstableColumnOrdinals(t *testing.T) {
	metadata := &core.MetadataSnapshot{
		Tables: []core.MetadataTable{{ID: "app.public.orders", DatabaseName: "app", SchemaName: "public", TableName: "orders"}},
		Columns: []core.MetadataColumn{
			{ID: "app.public.orders.status", TableID: "app.public.orders", DatabaseName: "app", SchemaName: "public", TableName: "orders", ColumnName: "status", Type: "text", Ordinal: 0},
			{ID: "app.public.orders.id", TableID: "app.public.orders", DatabaseName: "app", SchemaName: "public", TableName: "orders", ColumnName: "id", Type: "bigint", PrimaryKey: true, Ordinal: 1},
		},
	}
	snapshot := &core.CatalogSnapshot{Cards: []core.CatalogCard{
		{ID: "table:app.public.orders", Kind: "table", DatabaseName: "app", SchemaName: "public", TableName: "orders"},
		{ID: "column:app.public.orders.status", Kind: "column", DatabaseName: "app", SchemaName: "public", TableName: "orders", ColumnName: "status"},
		{ID: "column:app.public.orders.id", Kind: "column", DatabaseName: "app", SchemaName: "public", TableName: "orders", ColumnName: "id"},
	}}
	first := buildSemanticDocuments(metadata, snapshot)

	metadata.Columns[0], metadata.Columns[1] = metadata.Columns[1], metadata.Columns[0]
	metadata.Columns[0].Ordinal = 0
	metadata.Columns[1].Ordinal = 1
	second := buildSemanticDocuments(metadata, snapshot)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("equivalent columns produced different semantic documents\nfirst:  %+v\nsecond: %+v", first, second)
	}
}

func TestSemanticEmbeddingBatchesAreBounded(t *testing.T) {
	client := &deterministicEmbeddingClient{dimension: 128}
	index := &semanticCatalogIndex{embedder: client}
	documents := make([]semanticDocument, 130)
	missing := make([]int, 130)
	for n := range documents {
		documents[n] = semanticDocument{Text: fmt.Sprintf("document %d", n)}
		missing[n] = n
	}
	dimension := 128
	vectors, err := index.embedMissing(context.Background(), documents, missing, &dimension)
	if err != nil {
		t.Fatalf("embed missing: %v", err)
	}
	if len(vectors) != 130 {
		t.Fatalf("got %d vectors, want 130", len(vectors))
	}
	calls, texts, maxConcurrent := client.stats()
	if calls != 3 || texts != 130 || maxConcurrent > 2 {
		t.Fatalf("batch stats calls=%d texts=%d concurrent=%d, want 3,130,<=2", calls, texts, maxConcurrent)
	}
}

func TestSemanticEmbeddingBatchesHonorCancellation(t *testing.T) {
	index := &semanticCatalogIndex{embedder: blockingEmbeddingClient{}}
	documents := make([]semanticDocument, 130)
	missing := make([]int, len(documents))
	for n := range documents {
		documents[n].Text = fmt.Sprintf("document %d", n)
		missing[n] = n
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := index.embedMissing(ctx, documents, missing, nil); err == nil {
		t.Fatal("cancelled embedding build returned no error")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("cancelled embedding build took %s", elapsed)
	}
}

func TestSemanticWhereKindRecognizesStructuredFilters(t *testing.T) {
	for _, where := range []map[string]any{
		{"kind": "column"},
		{"kind": map[string]any{"eq": "column"}},
		{"and": []any{map[string]any{"database_name": map[string]any{"eq": "app"}}, map[string]any{"kind": map[string]any{"in": []any{"table", "column"}}}}},
	} {
		if !semanticWhereKind(where, "column") {
			t.Fatalf("column kind not recognized in %#v", where)
		}
	}
}

func TestSemanticIndexIncrementalReuseDimensionMismatchAndWarmLoad(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "catalog.sqlite3")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE metrics (id INTEGER PRIMARY KEY, revenue NUMERIC, recorded_at TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	fs := newAferoFS(afero.NewMemMapFs(), "/")
	coreConf := &core.Config{DBType: "sqlite", DisableAllowList: true}
	gj, err := core.NewGraphJin(coreConf, db, core.OptionSetFS(fs), core.OptionSetDBSchemaWatcherDisabled(true))
	if err != nil {
		t.Fatalf("new core: %v", err)
	}
	defer gj.Close()
	client := &deterministicEmbeddingClient{dimension: 128}
	conf := &Config{Core: *coreConf, Serv: Serv{
		DiscoveryCache: DiscoveryCacheConfig{Path: ".graphjin/discovery"},
		CatalogSearch: CatalogSearchConfig{Semantic: SemanticCatalogSearchConfig{
			Enabled: true, Provider: "openai", EmbeddingModel: "fake", Dimensions: "tiny",
		}},
	}}
	service := &graphjinService{conf: conf, gj: gj, dbs: map[string]*sql.DB{core.DefaultDBName: db}, fs: fs, log: zaptest.NewLogger(t).Sugar(), semanticEmbedder: client}
	index, err := newSemanticCatalogIndex(service)
	if err != nil {
		t.Fatal(err)
	}
	firstSnapshot, _ := gj.CatalogSnapshot()
	first, err := index.build(context.Background(), firstSnapshot)
	if err != nil {
		t.Fatalf("first build: %v", err)
	}
	_, firstTexts, _ := client.stats()
	if firstTexts == 0 {
		t.Fatal("initial build made no embedding calls")
	}
	index.setActive(first)
	if err := index.writeReceipt(first.manifest.GenerationID, 1); err != nil {
		t.Fatal(err)
	}

	client.reset()
	if _, err := db.Exec(`ALTER TABLE metrics ADD COLUMN region TEXT`); err != nil {
		t.Fatal(err)
	}
	if err := gj.Reload(); err != nil {
		t.Fatalf("reload core: %v", err)
	}
	secondSnapshot, _ := gj.CatalogSnapshot()
	second, err := index.build(context.Background(), secondSnapshot)
	if err != nil {
		t.Fatalf("incremental build: %v", err)
	}
	_, changedTexts, _ := client.stats()
	if changedTexts <= 0 || changedTexts >= second.manifest.DocumentCount {
		t.Fatalf("incremental build embedded %d of %d documents", changedTexts, second.manifest.DocumentCount)
	}
	if second.manifest.DocumentCount < first.manifest.DocumentCount {
		t.Fatalf("new column unexpectedly reduced document count: first=%d second=%d", first.manifest.DocumentCount, second.manifest.DocumentCount)
	}
	firstHashes := make(map[string]bool, len(first.docs))
	for _, document := range first.docs {
		firstHashes[document.Hash] = true
	}
	addedHashes := make(map[string]bool)
	for _, document := range second.docs {
		if !firstHashes[document.Hash] {
			addedHashes[document.Hash] = true
		}
	}
	if len(addedHashes) == 0 {
		t.Fatal("added column did not produce any changed semantic document hashes")
	}
	index.setActive(second)
	if _, err := db.Exec(`ALTER TABLE metrics DROP COLUMN region`); err != nil {
		t.Fatalf("drop incremental-test column: %v", err)
	}
	if err := gj.Reload(); err != nil {
		t.Fatalf("reload after removed column: %v", err)
	}
	removedSnapshot, _ := gj.CatalogSnapshot()
	removed, err := index.build(context.Background(), removedSnapshot)
	if err != nil {
		t.Fatalf("incremental removal build: %v", err)
	}
	for _, document := range removed.docs {
		if addedHashes[document.Hash] {
			t.Fatalf("stale semantic document %s survived column removal", document.Hash)
		}
	}

	mismatch := &deterministicEmbeddingClient{dimension: 64}
	index.embedder = mismatch
	index.mu.Lock()
	index.active = nil
	index.forceFullRebuild = true
	index.mu.Unlock()
	if _, err := index.build(context.Background(), secondSnapshot); err == nil || !strings.Contains(err.Error(), "dimension") {
		t.Fatalf("expected response dimension mismatch, got %v", err)
	}

	index.embedder = client
	index.setActive(removed)
	if err := index.writeReceipt(removed.manifest.GenerationID, 2); err != nil {
		t.Fatal(err)
	}
	client.reset()
	warm, err := newSemanticCatalogIndex(service)
	if err != nil {
		t.Fatal(err)
	}
	warm.Start()
	defer warm.Close()
	time.Sleep(100 * time.Millisecond)
	_, warmTexts, _ := client.stats()
	if warmTexts != 0 {
		t.Fatalf("warm semantic startup embedded %d documents, want zero", warmTexts)
	}
}

func TestSemanticQueryEmbeddingLRUReusesAndBoundsEntries(t *testing.T) {
	client := &deterministicEmbeddingClient{dimension: 16}
	index := &semanticCatalogIndex{
		conf: SemanticCatalogSearchConfig{Dimensions: "default"}, embedder: client, cache: newSemanticQueryLRU(),
	}
	first, err := index.queryVector(context.Background(), "clients", 16)
	if err != nil {
		t.Fatal(err)
	}
	second, err := index.queryVector(context.Background(), "clients", 16)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 16 || len(second) != 16 {
		t.Fatalf("cached vector dimensions = %d/%d", len(first), len(second))
	}
	calls, _, _ := client.stats()
	if calls != 1 {
		t.Fatalf("same query made %d embedding calls, want one", calls)
	}
	for n := 0; n < semanticQueryCacheSize+10; n++ {
		index.cache.Put(fmt.Sprintf("key-%d", n), []float32{1})
	}
	index.cache.mu.Lock()
	entries := len(index.cache.items)
	index.cache.mu.Unlock()
	if entries != semanticQueryCacheSize {
		t.Fatalf("query embedding cache retained %d entries, want %d", entries, semanticQueryCacheSize)
	}
}

func TestSemanticRelationshipPathsUseVisibleForeignKeysOnly(t *testing.T) {
	snapshot := &core.CatalogSnapshot{Cards: []core.CatalogCard{
		{ID: "table:customers", Kind: "table", DatabaseName: "app", SchemaName: "public", TableName: "customers"},
		{ID: "table:orders", Kind: "table", DatabaseName: "app", SchemaName: "public", TableName: "orders"},
		{ID: "table:order_items", Kind: "table", DatabaseName: "app", SchemaName: "public", TableName: "order_items"},
		{ID: "table:products", Kind: "table", DatabaseName: "app", SchemaName: "public", TableName: "products"},
	}}
	for n, relationship := range []core.MetadataRelationship{
		{FromDatabaseName: "app", FromSchemaName: "public", FromTableName: "orders", FromColumnName: "customer_id", ToDatabaseName: "app", ToSchemaName: "public", ToTableName: "customers", ToColumnName: "id"},
		{FromDatabaseName: "app", FromSchemaName: "public", FromTableName: "order_items", FromColumnName: "order_id", ToDatabaseName: "app", ToSchemaName: "public", ToTableName: "orders", ToColumnName: "id"},
		{FromDatabaseName: "app", FromSchemaName: "public", FromTableName: "order_items", FromColumnName: "product_id", ToDatabaseName: "app", ToSchemaName: "public", ToTableName: "products", ToColumnName: "id"},
	} {
		data, _ := jsonMarshalTest(relationship)
		snapshot.Cards = append(snapshot.Cards, core.CatalogCard{ID: fmt.Sprintf("relationship:%d", n), Kind: "relationship", EvidenceJSON: data})
	}
	paths := semanticRelationshipPaths(snapshot, []string{"table:customers", "table:products"}, 2, 3)
	if len(paths) != 1 {
		t.Fatalf("got %d paths, want one: %+v", len(paths), paths)
	}
	joined := strings.Join(paths[0], " ")
	for _, required := range []string{"table:orders", "table:order_items", "relationship:0", "relationship:1", "relationship:2"} {
		if !strings.Contains(joined, required) {
			t.Fatalf("path %q missing %q", joined, required)
		}
	}
}

func jsonMarshalTest(value any) (string, error) {
	data, err := json.Marshal(value)
	return string(data), err
}

func BenchmarkBuildSemanticDocumentsWideCatalog(b *testing.B) {
	metadata := &core.MetadataSnapshot{}
	snapshot := &core.CatalogSnapshot{}
	for tableIndex := 0; tableIndex < 1000; tableIndex++ {
		tableID := fmt.Sprintf("app.public.table_%04d", tableIndex)
		tableName := fmt.Sprintf("table_%04d", tableIndex)
		metadata.Tables = append(metadata.Tables, core.MetadataTable{ID: tableID, DatabaseName: "app", SchemaName: "public", TableName: tableName, Type: "table"})
		snapshot.Cards = append(snapshot.Cards, core.CatalogCard{ID: "table:" + tableID, Kind: "table", DatabaseName: "app", SchemaName: "public", TableName: tableName, Title: tableName, Summary: "wide table"})
		for columnIndex := 0; columnIndex < 300; columnIndex++ {
			columnName := fmt.Sprintf("value_%03d", columnIndex)
			columnID := tableID + "." + columnName
			metadata.Columns = append(metadata.Columns, core.MetadataColumn{ID: columnID, TableID: tableID, DatabaseName: "app", SchemaName: "public", TableName: tableName, ColumnName: columnName, Type: "numeric", Ordinal: columnIndex})
			snapshot.Cards = append(snapshot.Cards, core.CatalogCard{ID: "column:" + columnID, Kind: "column", DatabaseName: "app", SchemaName: "public", TableName: tableName, ColumnName: columnName, Title: columnName, Summary: "measure"})
		}
	}
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		documents := buildSemanticDocuments(metadata, snapshot)
		if len(documents) > 12000 {
			b.Fatalf("generated %d documents, want at most 12000", len(documents))
		}
		if rawBytes := len(documents) * 128 * 4; rawBytes > 6*1024*1024 {
			b.Fatalf("tiny vectors require %d bytes, want <= 6 MiB", rawBytes)
		}
	}
}
