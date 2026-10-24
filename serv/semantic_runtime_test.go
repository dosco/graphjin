package serv

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/serv/v3/internal/mcpcompat/mcp"
	"github.com/spf13/viper"
)

// toolResultText returns a tool result's text, error or not.
func toolResultText(result *mcp.CallToolResult) string {
	if result == nil || len(result.Content) == 0 {
		return ""
	}
	if text, ok := result.Content[0].(mcp.TextContent); ok {
		return text.Text
	}
	return ""
}

// The local EmbeddingGemma service speaks the OpenAI embeddings shape and
// ignores the key; OpenNeko still names a placeholder key variable so the
// client has a credential to send.
func TestAxEmbeddingClientAgainstOpenAICompatibleService(t *testing.T) {
	var status atomic.Int32
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/v1/embeddings" {
			http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, http.StatusNotFound)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer local" {
			http.Error(w, "unexpected authorization "+got, http.StatusUnauthorized)
			return
		}
		if code := int(status.Load()); code != 0 {
			http.Error(w, `{"error":{"message":"busy"}}`, code)
			return
		}
		var request struct {
			Input      []string `json:"input"`
			Model      string   `json:"model"`
			Dimensions int      `json:"dimensions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if request.Model != "embeddinggemma-2" || request.Dimensions != 768 || len(request.Input) == 0 || len(request.Input) > 64 {
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		data := make([]any, len(request.Input))
		for i, text := range request.Input {
			vector := make([]float64, 768)
			vector[len(text)%768] = 1
			data[i] = map[string]any{"object": "embedding", "index": i, "embedding": vector}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list", "model": request.Model, "data": data,
			"usage": map[string]any{"prompt_tokens": len(request.Input), "total_tokens": len(request.Input)},
		})
	}))
	defer server.Close()

	t.Setenv("OPENNEKO_EMBEDDING_API_KEY", "local")
	conf := SemanticCatalogSearchConfig{
		Enabled: true, Provider: "openai", EmbeddingModel: "embeddinggemma-2",
		APIKeyEnv: "OPENNEKO_EMBEDDING_API_KEY", BaseURL: server.URL + "/v1", Dimensions: "tiny",
		QueryPrefix: "task: search result | query: ", DocumentPrefix: "title: none | text: ",
	}
	client := newAxSemanticEmbeddingClient(conf)
	dims, _, _ := conf.dimensionCount()
	vectors, err := client.Embed(context.Background(), []string{"a", "bb", "ccc"}, &dims)
	if err != nil {
		t.Fatal(err)
	}
	if len(vectors) != 3 || len(vectors[0]) != 768 || vectors[1][2] != 1 || vectors[2][3] != 1 {
		t.Fatalf("vectors not in input order at 768 dimensions: %d", len(vectors))
	}

	// File passages use the same client through the file space adapter.
	space := (&graphjinService{conf: &Config{Serv: Serv{CatalogSearch: CatalogSearchConfig{Semantic: conf}}}}).fileSemanticSpace()
	if space == nil {
		t.Fatal("no file space for an enabled configuration")
	}
	if vectors, err := space.Embed(context.Background(), []string{"title: a.md | text: x"}); err != nil || len(vectors) != 1 || len(vectors[0]) != 768 {
		t.Fatalf("file space embed: %d vectors, %v", len(vectors), err)
	}

	// A full queue or a failed model is an error the caller backs off from.
	for _, code := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		status.Store(int32(code))
		started := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		_, err := client.Embed(ctx, []string{"a"}, &dims)
		cancel()
		if err == nil {
			t.Fatalf("status %d was not reported as an error", code)
		}
		t.Logf("status %d: %v after %s", code, err, time.Since(started).Round(time.Millisecond))
	}
}

func TestSemanticPrefixesAreEmbeddedAndFingerprinted(t *testing.T) {
	base := SemanticCatalogSearchConfig{Provider: "openai", EmbeddingModel: "m", Dimensions: "tiny"}
	plain, err := semanticEmbeddingFingerprint(base)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{"none": plain}
	for name, conf := range map[string]SemanticCatalogSearchConfig{
		"query":    {Provider: "openai", EmbeddingModel: "m", Dimensions: "tiny", QueryPrefix: "task: search result | query: "},
		"document": {Provider: "openai", EmbeddingModel: "m", Dimensions: "tiny", DocumentPrefix: "title: none | text: "},
		"both":     {Provider: "openai", EmbeddingModel: "m", Dimensions: "tiny", QueryPrefix: "task: search result | query: ", DocumentPrefix: "title: none | text: "},
	} {
		fingerprint, err := semanticEmbeddingFingerprint(conf)
		if err != nil {
			t.Fatal(err)
		}
		for other, value := range seen {
			if value == fingerprint {
				t.Fatalf("%s prefixes share a fingerprint with %s", name, other)
			}
		}
		seen[name] = fingerprint
	}

	recorder := &recordingEmbeddingClient{}
	index, gj, _ := newSemanticEnableFixture(t, recorder, "default", nil)
	index.conf.QueryPrefix = "task: search result | query: "
	index.conf.DocumentPrefix = "title: none | text: "
	snapshot, _ := gj.CatalogSnapshot()
	built, err := index.build(context.Background(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	index.setActive(built)
	for _, text := range recorder.all() {
		if !strings.HasPrefix(text, "title: none | text: ") {
			t.Fatalf("document embedded without its prefix: %q", text)
		}
	}
	recorder.reset()
	if _, err := index.queryVector(context.Background(), "monthly revenue", built.manifest.ActualDimension); err != nil {
		t.Fatal(err)
	}
	if got := recorder.all(); len(got) != 1 || got[0] != "task: search result | query: monthly revenue" {
		t.Fatalf("query embedded as %q", got)
	}
}

// recordingEmbeddingClient records what it is asked to embed and answers
// like deterministicEmbeddingClient.
type recordingEmbeddingClient struct {
	deterministicEmbeddingClient
	textsMu sync.Mutex
	seen    []string
	fail    atomic.Bool
}

func (c *recordingEmbeddingClient) Embed(ctx context.Context, texts []string, dimensions *int) ([][]float32, error) {
	if c.fail.Load() {
		return nil, errEmbeddingServiceBusy
	}
	c.textsMu.Lock()
	c.seen = append(c.seen, texts...)
	c.textsMu.Unlock()
	return c.deterministicEmbeddingClient.Embed(ctx, texts, dimensions)
}

func (c *recordingEmbeddingClient) all() []string {
	c.textsMu.Lock()
	defer c.textsMu.Unlock()
	return append([]string(nil), c.seen...)
}

func (c *recordingEmbeddingClient) reset() {
	c.textsMu.Lock()
	c.seen = nil
	c.textsMu.Unlock()
}

type busyError struct{}

func (busyError) Error() string { return "embedding service answered 429 Too Many Requests" }

var errEmbeddingServiceBusy error = busyError{}

// newSemanticToggleServer is a config MCP server over a SQLite database whose
// semantic settings start off, with a deterministic embedder.
func newSemanticToggleServer(t *testing.T, embedder SemanticEmbeddingClient) (*mcpServer, *viper.Viper, string) {
	t.Helper()
	dbPath := createSQLiteDBFile(t, "semantic-toggle.sqlite3", true)
	configPath := filepath.Join(t.TempDir(), "dev.yml")
	if err := os.WriteFile(configPath, []byte("app_name: toggle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v := viper.New()
	v.SetConfigFile(configPath)
	if err := v.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	ms := newTransactionalConfigMCPServerWithOptions(t, dbPath, false, v)
	ms.service.conf.MCP.AllowConfigUpdates = true
	ms.service.conf.DiscoveryCache.Path = ".graphjin/discovery"
	ms.service.conf.CatalogSearch.Semantic = SemanticCatalogSearchConfig{Provider: "openai", APIKeyEnv: "OPENNEKO_EMBEDDING_API_KEY", Dimensions: "tiny"}
	ms.service.semanticEmbedder = embedder
	t.Cleanup(func() {
		if index := ms.service.semantic.Swap(nil); index != nil {
			index.Close()
		}
	})
	return ms, v, configPath
}

// updateSemanticConfig sends a catalog_search.semantic patch through
// gj_config's update path, with the preview/apply handshake a source-mode
// configuration requires.
func updateSemanticConfig(t *testing.T, ms *mcpServer, semantic map[string]any) ConfigUpdateResult {
	t.Helper()
	call := func(extra map[string]any) ConfigUpdateResult {
		args := map[string]any{"serv": map[string]any{"catalog_search": map[string]any{"semantic": semantic}}}
		for k, v := range extra {
			args[k] = v
		}
		res, err := ms.handleUpdateCurrentConfig(context.Background(), newToolRequest(args))
		if err != nil {
			t.Fatal(err)
		}
		var out ConfigUpdateResult
		if err := json.Unmarshal([]byte(assertToolSuccess(t, res)), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	out := call(nil)
	if !out.Success && strings.Contains(out.Message, `mode: "preview"`) {
		rev := out.CatalogRevision
		preview := call(map[string]any{"mode": "preview", "expected_catalog_revision": rev})
		if !preview.Valid || preview.PreviewID == "" {
			t.Fatalf("preview failed: %+v", preview)
		}
		out = call(map[string]any{"mode": "apply", "expected_catalog_revision": rev, "preview_id": preview.PreviewID})
		if !out.Applied {
			t.Fatalf("apply did not apply: %+v", out)
		}
	}
	if !out.Success {
		t.Fatalf("update failed: %+v", out)
	}
	if out.Scope != ConfigScopeServ || out.ReloadMode != servReloadHot {
		t.Fatalf("semantic change reported scope %q reload %q, want serv/hot", out.Scope, out.ReloadMode)
	}
	for _, change := range out.Changes {
		if strings.Contains(change, "restart") {
			t.Fatalf("semantic change reported a restart: %v", out.Changes)
		}
	}
	return out
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// gj_config turns semantic catalog search on, swaps it and turns it off in the
// running process while queries keep arriving.
func TestSemanticCatalogTogglesLiveThroughGJConfig(t *testing.T) {
	embedder := &recordingEmbeddingClient{}
	ms, v, configPath := newSemanticToggleServer(t, embedder)
	s := ms.service
	snapshot, err := s.gj.CatalogSnapshot()
	if err != nil {
		t.Fatal(err)
	}

	stop := make(chan struct{})
	var queries sync.WaitGroup
	var modes sync.Map
	for n := 0; n < 4; n++ {
		queries.Add(1)
		go func() {
			defer queries.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				_, metadata, err := s.queryCatalogDetailed(context.Background(), snapshot, core.CatalogQuery{Search: "customer purchases", Limit: 5})
				if err != nil {
					t.Error(err)
					return
				}
				modes.Store(metadata.Mode, true)
			}
		}()
	}

	if s.semanticCatalog() != nil {
		t.Fatal("semantic index running while disabled")
	}
	updateSemanticConfig(t, ms, map[string]any{
		"enabled": true, "embedding_model": "embeddinggemma-2", "base_url": "http://embedding:5003/v1",
		"query_prefix": "task: search result | query: ", "document_prefix": "title: none | text: ",
	})
	first := s.semanticCatalog()
	if first == nil {
		t.Fatal("enabling did not start a semantic index")
	}
	waitFor(t, "the first index", func() bool { return first.current() != nil })
	waitFor(t, "a hybrid query", func() bool { _, ok := modes.Load("hybrid"); return ok })

	// Persisted to the config file as gj_config persists other keys.
	saved := viper.New()
	saved.SetConfigFile(configPath)
	if err := saved.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	if !saved.GetBool("catalog_search.semantic.enabled") ||
		saved.GetString("catalog_search.semantic.query_prefix") != "task: search result | query: " ||
		saved.GetString("catalog_search.semantic.embedding_model") != "embeddinggemma-2" {
		t.Fatalf("config file holds %v", saved.Get("catalog_search"))
	}
	if staged, ok := v.Get("catalog_search").(map[string]any); !ok || staged["semantic"] == nil {
		t.Fatalf("catalog_search not staged in viper: %#v", v.Get("catalog_search"))
	}

	// A prefix change is a new embedding space: a new index, the old closed.
	updateSemanticConfig(t, ms, map[string]any{"query_prefix": "query: "})
	second := s.semanticCatalog()
	if second == nil || second == first || second.fingerprint == first.fingerprint {
		t.Fatal("prefix change did not swap the index")
	}
	if !first.closing() {
		t.Fatal("swapped-out index was not closed")
	}
	waitFor(t, "the swapped index", func() bool { return second.current() != nil })

	// Off: the index is closed and no embedding calls follow.
	updateSemanticConfig(t, ms, map[string]any{"enabled": false})
	if s.semanticCatalog() != nil || !second.closing() {
		t.Fatal("disabling left the index running")
	}
	modes.Delete("lexical")
	waitFor(t, "a lexical query", func() bool { _, ok := modes.Load("lexical"); return ok })
	calls, _, _ := embedder.stats()
	time.Sleep(100 * time.Millisecond)
	if after, _, _ := embedder.stats(); after != calls {
		t.Fatalf("embedding calls continued after disabling: %d then %d", calls, after)
	}
	close(stop)
	queries.Wait()
	if saved.ReadInConfig() != nil || saved.GetBool("catalog_search.semantic.enabled") {
		t.Fatal("disabling was not persisted")
	}
}

func TestSemanticConfigPatchValidation(t *testing.T) {
	embedder := &recordingEmbeddingClient{}
	ms, _, _ := newSemanticToggleServer(t, embedder)
	for name, semantic := range map[string]any{
		"model required":  map[string]any{"enabled": true},
		"bad dimensions":  map[string]any{"dimensions": "huge"},
		"unknown field":   map[string]any{"temperature": "hot"},
		"wrong type":      map[string]any{"enabled": "yes"},
		"not an object":   "on",
		"prefix not text": map[string]any{"query_prefix": 3},
	} {
		res, err := ms.handleUpdateCurrentConfig(context.Background(), newToolRequest(map[string]any{
			"serv": map[string]any{"catalog_search": map[string]any{"semantic": semantic}},
		}))
		if err != nil {
			t.Fatal(err)
		}
		var out ConfigUpdateResult
		_ = json.Unmarshal([]byte(toolResultText(res)), &out)
		if out.Success {
			t.Fatalf("%s: accepted %v", name, semantic)
		}
	}
	if ms.service.conf.CatalogSearch.Semantic.Enabled || ms.service.semanticCatalog() != nil {
		t.Fatal("a rejected patch changed the running settings")
	}
	if _, reload, err := validateServConfigPatch(map[string]any{"catalog_search": map[string]any{"semantic": map[string]any{"enabled": true}}}); err != nil || reload != servReloadHot {
		t.Fatalf("catalog_search classified %q (%v), want hot", reload, err)
	}
	if got := servConfigMap(ms.service.conf)["catalog_search"]; got == nil {
		t.Fatal("gj_config does not show catalog_search")
	}
}
