package serv

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dosco/graphjin/core/v3"
)

const testQueryPrefix = "task: search result | query: "

// conceptEmbeddingClient embeds words of one concept on one dimension, so a
// query can match a passage it shares no word with. It can be told to fail
// the way a busy embedding service does.
type conceptEmbeddingClient struct {
	mu            sync.Mutex
	documents     []string
	queries       []string
	batches       []int
	active        int
	maxConcurrent int
	failDocuments atomic.Int32 // the next n document batches fail
	failQueries   atomic.Bool
	reject        string // a batch holding a text with this word always fails
	attempts      []int  // texts per file passage request, failed ones included
	requestChars  []int  // characters per successful file passage request
}

var testConcepts = map[string]int{
	"car": 1, "automobile": 1, "vehicle": 1, "sedan": 1,
	"holiday": 2, "vacation": 2, "pto": 2,
	"salary": 3, "pay": 3, "compensation": 3,
}

func (c *conceptEmbeddingClient) Embed(ctx context.Context, texts []string, _ *int) ([][]float32, error) {
	query := len(texts) == 1 && strings.HasPrefix(texts[0], testQueryPrefix)
	if query && c.failQueries.Load() {
		return nil, errEmbeddingServiceBusy
	}
	passages := !query && strings.Contains(texts[0], ".md | text: ")
	if passages {
		c.mu.Lock()
		c.attempts = append(c.attempts, len(texts))
		c.mu.Unlock()
	}
	if !query && c.failDocuments.Add(-1) >= 0 {
		return nil, errEmbeddingServiceBusy
	}
	c.mu.Lock()
	reject := c.reject
	c.mu.Unlock()
	for _, text := range texts {
		if reject != "" && strings.Contains(text, reject) {
			return nil, errors.New("embedding service rejected the input")
		}
	}
	c.mu.Lock()
	if passages {
		chars := 0
		for _, text := range texts {
			chars += len(text)
		}
		c.requestChars = append(c.requestChars, chars)
	}
	c.active++
	c.maxConcurrent = max(c.maxConcurrent, c.active)
	if query {
		c.queries = append(c.queries, texts...)
	} else {
		c.documents = append(c.documents, texts...)
		c.batches = append(c.batches, len(texts))
	}
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.active--
		c.mu.Unlock()
	}()
	out := make([][]float32, len(texts))
	for i, text := range texts {
		v := make([]float32, 8)
		if _, body, ok := strings.Cut(text, "| text: "); ok {
			text = body
		}
		text = strings.TrimPrefix(text, testQueryPrefix)
		for _, w := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return r < 'a' || r > 'z' }) {
			if d, ok := testConcepts[strings.TrimSuffix(w, "s")]; ok {
				v[d]++
			}
		}
		v[0] = 0.01
		out[i] = v
	}
	return out, ctx.Err()
}

// documentTexts returns the file passages embedded so far; the same client
// also embeds catalog documents, which are left out.
func (c *conceptEmbeddingClient) documentTexts() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	for _, text := range c.documents {
		if strings.Contains(text, ".md | text: ") || strings.HasPrefix(text, "title: none | text: #") {
			out = append(out, text)
		}
	}
	return out
}

func writeNestedTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, path, body)
}

func semanticFileService(t *testing.T, embedder SemanticEmbeddingClient, enabled bool) (*graphjinService, string) {
	t.Helper()
	configRoot := t.TempDir()
	files := filepath.Join(configRoot, "files")
	writeNestedTestFile(t, filepath.Join(files, "fleet", "cars.md"), "# Fleet\nEvery sedan is serviced each spring.\n")
	writeNestedTestFile(t, filepath.Join(files, "hr", "leave.md"), "# Leave\nEmployees get 25 vacation days.\n")
	writeNestedTestFile(t, filepath.Join(files, "users", "alice", "notes.md"), "# Notes\nMy sedan needs new tyres.\n")
	conf := &Config{
		Core: Core{
			Mode: "dev",
			Sources: []core.SourceConfig{
				{Name: "docs", Kind: "file", Backend: "local", Root: "files", ReadOnly: true, UserPrefix: "users/"},
			},
		},
		Serv: Serv{
			ConfigPath:  configRoot,
			MCP:         MCPConfig{Disable: true, AllowConfigUpdates: true},
			FileContent: FileContentConfig{Enabled: true},
			CatalogSearch: CatalogSearchConfig{Semantic: SemanticCatalogSearchConfig{
				Enabled: enabled, Provider: "openai", EmbeddingModel: "embeddinggemma-2", Dimensions: "tiny",
				APIKeyEnv: "OPENNEKO_EMBEDDING_API_KEY", BaseURL: "http://embedding:5003/v1",
				QueryPrefix: testQueryPrefix, DocumentPrefix: "title: none | text: ",
			}},
		},
	}
	s, err := newGraphJinService(conf, nil, OptionSetSemanticEmbeddingClient(embedder))
	if err != nil {
		t.Fatalf("service start: %v", err)
	}
	t.Cleanup(func() { closeTestService(s) })
	return s, files
}

// forceSync indexes the source now, past the refresh interval.
func forceSync(t *testing.T, fc *fileContent) {
	t.Helper()
	b, ok := fc.backend("docs")
	if !ok {
		t.Fatal("no docs backend")
	}
	ix, err := fc.index("docs")
	if err != nil {
		t.Fatal(err)
	}
	st, err := ix.Sync(context.Background(), b, 0)
	if err != nil {
		t.Fatal(err)
	}
	if st.Indexed > 0 {
		fc.wakeEmbedder()
	}
}

// waitForEmbedded syncs the source and waits until the embedder has a vector
// for every passage.
func waitForEmbedded(t *testing.T, s *graphjinService) {
	t.Helper()
	fc := s.fileContentReader()
	sem := fc.sem.Load()
	if sem == nil {
		t.Fatal("semantic file search is off")
	}
	forceSync(t, fc)
	waitFor(t, "passages to be embedded", func() bool {
		ix, err := fc.index("docs")
		if err != nil {
			t.Fatal(err)
		}
		done, total, err := ix.EmbeddingProgress(context.Background(), sem.space.fingerprint)
		return err == nil && total > 0 && done == total
	})
}

func searchDocs(t *testing.T, s *graphjinService, user, search string) []string {
	t.Helper()
	ctx := context.Background()
	if user != "" {
		ctx = context.WithValue(ctx, core.UserIDKey, user)
	}
	literal, _ := json.Marshal(search)
	res, err := s.gj.GraphQL(ctx, `query { docs(search: `+string(literal)+`, limit: 5) { key passages } }`, nil, nil)
	if err != nil {
		t.Fatalf("search %q: %v", search, err)
	}
	var out struct {
		Docs []struct {
			Key      string             `json:"key"`
			Passages []core.FilePassage `json:"passages"`
		} `json:"docs"`
	}
	if err := json.Unmarshal(res.Data, &out); err != nil {
		t.Fatalf("%v\n%s", err, res.Data)
	}
	keys := make([]string, 0, len(out.Docs))
	for _, d := range out.Docs {
		if len(d.Passages) == 0 {
			t.Fatalf("%s returned without passages: %s", d.Key, res.Data)
		}
		keys = append(keys, d.Key)
	}
	return keys
}

func TestFileSearchRanksByEmbeddingsWithinAccessRules(t *testing.T) {
	embedder := &conceptEmbeddingClient{}
	s, _ := semanticFileService(t, embedder, true)
	waitForEmbedded(t, s)

	texts := embedder.documentTexts()
	if len(texts) != 3 {
		t.Fatalf("embedded file passages = %q, want one per file", texts)
	}
	for _, text := range texts {
		if strings.HasPrefix(text, "title: none") {
			t.Fatalf("passage embedded without its file name as title: %q", text)
		}
	}
	if !strings.Contains(strings.Join(texts, "\n"), "title: cars.md | text: # Fleet") {
		t.Fatalf("passages = %q", texts)
	}
	// "automobile" shares no word with any file; only embeddings find them.
	bob := searchDocs(t, s, "bob", "automobile")
	if len(bob) == 0 || bob[0] != "fleet/cars.md" {
		t.Fatalf("bob's semantic search = %v, want fleet/cars.md first", bob)
	}
	for _, key := range bob {
		if strings.HasPrefix(key, "users/") {
			t.Fatalf("bob's semantic search returned another user's file: %v", bob)
		}
	}
	alice := searchDocs(t, s, "alice", "automobile")
	if !strings.Contains(strings.Join(alice, ","), "users/alice/notes.md") {
		t.Fatalf("alice's semantic search = %v, want her own notes", alice)
	}
	anonymous := searchDocs(t, s, "", "automobile")
	for _, key := range anonymous {
		if strings.HasPrefix(key, "users/") {
			t.Fatalf("anonymous semantic search returned a private file: %v", anonymous)
		}
	}

	// The embedding service down: full-text search, as without semantic.
	embedder.failQueries.Store(true)
	if got := searchDocs(t, s, "bob", "vacation days"); len(got) != 1 || got[0] != "hr/leave.md" {
		t.Fatalf("fallback search = %v, want the full-text match", got)
	}
	if got := searchDocs(t, s, "bob", "automobile insurance quote"); len(got) != 0 {
		t.Fatalf("fallback search found %v for words no file holds", got)
	}
}

func TestFileSearchTogglesLiveAndKeepsVectors(t *testing.T) {
	embedder := &conceptEmbeddingClient{}
	s, files := semanticFileService(t, embedder, false)
	ms := &mcpServer{service: s, ctx: context.Background(), readOnlyDBs: map[string]bool{}}
	fc := s.fileContentReader()
	if fc.sem.Load() != nil {
		t.Fatal("semantic file search running while disabled")
	}
	if got := searchDocs(t, s, "bob", "automobile"); len(got) != 0 {
		t.Fatalf("disabled search = %v, want full-text (no match)", got)
	}

	updateSemanticConfig(t, ms, map[string]any{"enabled": true})
	if s.semanticCatalog() == nil {
		t.Fatal("enabling did not start semantic catalog search")
	}
	waitForEmbedded(t, s)
	if got := searchDocs(t, s, "bob", "automobile"); len(got) == 0 || got[0] != "fleet/cars.md" {
		t.Fatalf("enabled search = %v", got)
	}
	embedded := len(embedder.documentTexts())

	updateSemanticConfig(t, ms, map[string]any{"enabled": false})
	if fc.sem.Load() != nil || s.semanticCatalog() != nil {
		t.Fatal("disabling left semantic search running")
	}
	if got := searchDocs(t, s, "bob", "automobile"); len(got) != 0 {
		t.Fatalf("search after disabling = %v, want full-text (no match)", got)
	}
	// A file added while off is indexed for full text only.
	writeNestedTestFile(t, filepath.Join(files, "fleet", "vans.md"), "# Vans\nThe vehicle pool has two vans.\n")
	forceSync(t, fc)
	if got := searchDocs(t, s, "bob", "vans"); len(got) != 1 || got[0] != "fleet/vans.md" {
		t.Fatalf("file added while off is not searchable by full text: %v", got)
	}
	time.Sleep(50 * time.Millisecond)
	if got := len(embedder.documentTexts()); got != embedded {
		t.Fatalf("embedding continued while disabled: %d then %d passages", embedded, got)
	}

	// Back on: stored vectors are reused, only the new file is embedded.
	updateSemanticConfig(t, ms, map[string]any{"enabled": true})
	waitForEmbedded(t, s)
	texts := embedder.documentTexts()
	if len(texts) != embedded+1 || !strings.Contains(texts[len(texts)-1], "vans.md") {
		t.Fatalf("re-enabling embedded %d new passages (%q), want only vans.md", len(texts)-embedded, texts[embedded:])
	}
}

func TestFileEmbedderBacksOffAndRecovers(t *testing.T) {
	embedder := &conceptEmbeddingClient{}
	embedder.failDocuments.Store(1 << 20) // busy until told otherwise
	s, files := semanticFileService(t, embedder, true)
	fc := s.fileContentReader()
	// Restart the embedder with short delays, once every file is indexed so
	// its first request holds all three passages.
	fc.setSemantic(nil)
	forceSync(t, fc)
	embedder.mu.Lock()
	embedder.attempts = nil
	embedder.mu.Unlock()
	fc.backoffs = []time.Duration{20 * time.Millisecond}
	fc.setSemantic(s.fileSemanticSpace())

	ix, err := fc.index("docs")
	if err != nil {
		t.Fatal(err)
	}
	// While busy, search still answers by full text.
	if got := searchDocs(t, s, "bob", "vacation"); len(got) != 1 || got[0] != "hr/leave.md" {
		t.Fatalf("search while embedder is busy = %v", got)
	}
	// Each failure halves the request size, down to a single passage.
	waitFor(t, "requests to shrink to one passage", func() bool {
		embedder.mu.Lock()
		defer embedder.mu.Unlock()
		n := len(embedder.attempts)
		return n > 3 && embedder.attempts[0] == 3 && embedder.attempts[n-1] == 1
	})
	if done, _, _ := ix.EmbeddingProgress(context.Background(), fc.sem.Load().space.fingerprint); done != 0 {
		t.Fatalf("%d passages embedded while every request failed", done)
	}
	embedder.failDocuments.Store(0)
	waitForEmbedded(t, s)

	// Many passages go in batches bounded by count and by length, one
	// request at a time.
	var long strings.Builder
	for i := 0; i < 150; i++ {
		long.WriteString("# Part\nThe sedan list. " + strings.Repeat("Spare tyres and service history. ", 20) + "\n")
	}
	writeNestedTestFile(t, filepath.Join(files, "fleet", "list.md"), long.String())
	waitForEmbedded(t, s)
	embedder.mu.Lock()
	batches, maxConcurrent := append([]int(nil), embedder.batches...), embedder.maxConcurrent
	requestChars := append([]int(nil), embedder.requestChars...)
	embedder.mu.Unlock()
	for _, n := range requestChars {
		if n > fileEmbedRequestChars {
			t.Fatalf("request of %d characters exceeds %d", n, fileEmbedRequestChars)
		}
	}
	if len(requestChars) < 5 {
		t.Fatalf("150 long passages went in %d requests", len(requestChars))
	}
	for _, n := range batches {
		if n > 64 {
			t.Fatalf("batch of %d passages", n)
		}
	}
	if maxConcurrent > 1 {
		t.Fatalf("%d concurrent embedding requests", maxConcurrent)
	}
	if err := os.Remove(filepath.Join(files, "fleet", "list.md")); err != nil {
		t.Fatal(err)
	}
}

func TestFileEmbedderSkipsAPassageTheServiceRejects(t *testing.T) {
	embedder := &conceptEmbeddingClient{reject: "unembeddable"}
	s, files := semanticFileService(t, embedder, true)
	fc := s.fileContentReader()
	fc.setSemantic(nil)
	writeNestedTestFile(t, filepath.Join(files, "a", "broken.md"), "# Broken\nThis unembeddable passage lists every vacation rule.\n")
	forceSync(t, fc)
	fc.backoffs = []time.Duration{10 * time.Millisecond}
	fc.setSemantic(s.fileSemanticSpace())

	ix, err := fc.index("docs")
	if err != nil {
		t.Fatal(err)
	}
	fingerprint := fc.sem.Load().space.fingerprint
	// The rejected passage sorts first, so without skipping it would hold up
	// every passage after it.
	waitFor(t, "the other passages to be embedded", func() bool {
		done, total, err := ix.EmbeddingProgress(context.Background(), fingerprint)
		return err == nil && total > 0 && done == total-1
	})
	if got := searchDocs(t, s, "bob", "unembeddable"); len(got) != 1 || got[0] != "a/broken.md" {
		t.Fatalf("full-text search for the skipped passage = %v", got)
	}
	// A change to the file clears the mark and embeds it again.
	embedder.mu.Lock()
	embedder.reject = ""
	embedder.mu.Unlock()
	writeNestedTestFile(t, filepath.Join(files, "a", "broken.md"), "# Fixed\nThis passage lists every vacation rule.\n")
	waitForEmbedded(t, s)
}
