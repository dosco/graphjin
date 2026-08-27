package serv

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/dosco/graphjin/core/v3"
	"github.com/spf13/afero"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"go.uber.org/zap/zaptest/observer"
	_ "modernc.org/sqlite"
)

// newSemanticEnableFixture builds the smallest service a semantic index needs,
// so the tests below exercise build and query paths rather than construction.
func newSemanticEnableFixture(t *testing.T, client SemanticEmbeddingClient, preset string, log *zap.SugaredLogger) (*semanticCatalogIndex, *core.GraphJin, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	// Every pooled connection to ":memory:" is its own database, so a second
	// connection would introspect an empty schema and DDL would land nowhere.
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE metrics (id INTEGER PRIMARY KEY, revenue NUMERIC, recorded_at TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	fs := newAferoFS(afero.NewMemMapFs(), "/")
	coreConf := &core.Config{DBType: "sqlite", DisableAllowList: true}
	gj, err := core.NewGraphJin(coreConf, db, core.OptionSetFS(fs), core.OptionSetDBSchemaWatcherDisabled(true))
	if err != nil {
		t.Fatalf("new core: %v", err)
	}
	t.Cleanup(func() { gj.Close() })
	if log == nil {
		log = zaptest.NewLogger(t).Sugar()
	}
	conf := &Config{Core: *coreConf, Serv: Serv{
		DiscoveryCache: DiscoveryCacheConfig{Path: ".graphjin/discovery"},
		CatalogSearch: CatalogSearchConfig{Semantic: SemanticCatalogSearchConfig{
			Enabled: true, Provider: "openai", EmbeddingModel: "fake", Dimensions: preset,
		}},
	}}
	service := &graphjinService{
		conf: conf, gj: gj, dbs: map[string]*sql.DB{core.DefaultDBName: db},
		fs: fs, log: log, semanticEmbedder: client,
	}
	index, err := newSemanticCatalogIndex(service)
	if err != nil {
		t.Fatal(err)
	}
	return index, gj, db
}

// A provider that ignores the requested size must not take the feature down. The
// documented tiny/small/medium presets are unsupported by several embedding
// models, and Gemini's non-Vertex endpoint dropped the parameter outright, so a
// configuration reading `enabled: true` silently served lexical search forever.
func TestSemanticIndexBuildsWhenProviderIgnoresRequestedDimension(t *testing.T) {
	logCore, observed := observer.New(zap.InfoLevel)
	client := &deterministicEmbeddingClient{dimension: 3072}
	index, gj, _ := newSemanticEnableFixture(t, client, "tiny", zap.New(logCore).Sugar())

	snapshot, _ := gj.CatalogSnapshot()
	built, err := index.build(context.Background(), snapshot)
	if err != nil {
		t.Fatalf("build refused a provider-chosen dimension: %v", err)
	}
	if built.manifest.ActualDimension != 3072 {
		t.Fatalf("manifest recorded dimension %d, want the 3072 the provider returned", built.manifest.ActualDimension)
	}
	for n, document := range built.docs {
		start := document.VectorOffset
		if start < 0 || start+3072 > len(built.vectors) {
			t.Fatalf("document %d (%s) has no full 3072-wide vector", n, document.Hash)
		}
	}

	var warned string
	for _, entry := range observed.All() {
		if strings.Contains(entry.Message, "dimensions for the configured") {
			warned = entry.Message
		}
	}
	if warned == "" {
		t.Fatal("ignoring the requested dimension was not reported at all")
	}
	if !strings.Contains(warned, "3072") || !strings.Contains(warned, "tiny") || !strings.Contains(warned, "128") {
		t.Fatalf("warning does not name what was asked for and what came back: %q", warned)
	}
}

// Vectors carried over from an earlier generation are stale once the provider
// changes width, which is what an SDK upgrade that starts honouring the size
// request looks like. The build must give up its reuse rather than fail for the
// life of the configuration.
func TestSemanticIndexRecoversWhenCarriedVectorsChangeWidth(t *testing.T) {
	client := &deterministicEmbeddingClient{dimension: 256}
	index, gj, db := newSemanticEnableFixture(t, client, "default", nil)

	snapshot, _ := gj.CatalogSnapshot()
	first, err := index.build(context.Background(), snapshot)
	if err != nil {
		t.Fatalf("first build: %v", err)
	}
	index.setActive(first)

	// A wider catalog leaves the untouched documents reusable and the new ones
	// to embed, which is the only shape that can mix widths.
	if _, err := db.Exec(`ALTER TABLE metrics ADD COLUMN region TEXT`); err != nil {
		t.Fatal(err)
	}
	if err := gj.Reload(); err != nil {
		t.Fatalf("reload core: %v", err)
	}
	index.embedder = &deterministicEmbeddingClient{dimension: 64}
	wider, _ := gj.CatalogSnapshot()
	if _, err := index.build(context.Background(), wider); err == nil {
		t.Fatal("mixing carried 256-wide vectors with fresh 64-wide vectors was accepted")
	}
	index.mu.RLock()
	forced, active := index.forceFullRebuild, index.active
	index.mu.RUnlock()
	if !forced || active != nil {
		t.Fatalf("stale reuse was not cleared: forceFullRebuild=%v active=%v", forced, active != nil)
	}

	// With reuse dropped the next build succeeds at the provider's new width.
	recovered, err := index.build(context.Background(), wider)
	if err != nil {
		t.Fatalf("rebuild after dropping stale vectors: %v", err)
	}
	if recovered.manifest.ActualDimension != 64 {
		t.Fatalf("recovered at %d dimensions, want 64", recovered.manifest.ActualDimension)
	}
}

// partialFailureEmbedder fails one batch with a message an operator can act on
// while every other batch reports only that it ran out of time — the shape rate
// limiting actually produces.
type partialFailureEmbedder struct {
	mu     sync.Mutex
	calls  int
	real   error
	first  chan struct{}
	closed sync.Once
}

func (e *partialFailureEmbedder) Embed(ctx context.Context, texts []string, _ *int) ([][]float32, error) {
	e.mu.Lock()
	e.calls++
	call := e.calls
	e.mu.Unlock()
	if call == 1 {
		// Let the timeouts land in the collector first.
		<-e.first
		return nil, e.real
	}
	e.closed.Do(func() { close(e.first) })
	return nil, context.DeadlineExceeded
}

func TestEmbedMissingReportsTheActionableErrorNotTheTimeout(t *testing.T) {
	quota := errors.New("429 insufficient_quota: embedding tokens exhausted for this project")
	client := &partialFailureEmbedder{real: quota, first: make(chan struct{})}
	index, _, _ := newSemanticEnableFixture(t, client, "default", nil)

	// Two batches, so one call can fail for a real reason while the other only
	// reports that it timed out.
	total := semanticEmbeddingBatchSize + 1
	documents := make([]semanticDocument, total)
	missing := make([]int, total)
	for n := range documents {
		documents[n] = semanticDocument{Hash: fmt.Sprint(n), Text: fmt.Sprintf("document %d", n)}
		missing[n] = n
	}

	_, err := index.embedMissing(context.Background(), documents, missing, nil)
	if err == nil {
		t.Fatal("embedding failure was not reported")
	}
	if !errors.Is(err, quota) {
		t.Fatalf("reported %q, which tells an operator nothing; want the quota rejection", err)
	}
}

// Close cancels the build, and reporting that cancellation as degradation made
// three healthy boots of the eval demo look like the feature had failed.
func TestWarnFallbackSeparatesShutdownFromDegradation(t *testing.T) {
	logCore, observed := observer.New(zap.InfoLevel)
	client := &deterministicEmbeddingClient{dimension: 128}
	index, _, _ := newSemanticEnableFixture(t, client, "default", zap.New(logCore).Sugar())

	index.Start()
	index.warnFallback(errors.New("embedding provider rejected the API key"))
	if got := len(observed.FilterMessageSnippet("rejected the API key").All()); got != 1 {
		t.Fatalf("a real failure logged %d times, want 1", got)
	}

	index.Close()
	index.warnFallback(context.Canceled)
	index.warnFallback(fmt.Errorf("build stopped: %w", context.Canceled))
	if got := len(observed.FilterMessageSnippet("using lexical search").All()); got != 1 {
		t.Fatalf("shutdown cancellation was reported as degradation: %d warnings, want the 1 real failure",
			got)
	}
}
