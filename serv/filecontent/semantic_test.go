package filecontent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// conceptEmbedder is a deterministic stand-in for an embedding model: words
// of one concept share a dimension, so "automobile" is close to "car" while
// sharing no word with it, which full-text search cannot see.
type conceptEmbedder struct {
	mu      sync.Mutex
	calls   int
	batches []int
	texts   []string
	before  func(texts []string) // runs before each answer
}

var concepts = map[string]int{
	"car": 0, "automobile": 0, "vehicle": 0, "sedan": 0,
	"holiday": 1, "vacation": 1, "leave": 1, "pto": 1,
	"salary": 2, "pay": 2, "compensation": 2, "wage": 2,
	"laminate": 3, "rogers": 3, "substrate": 3,
}

const conceptDims = 8

func (e *conceptEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	e.mu.Lock()
	e.calls++
	e.batches = append(e.batches, len(texts))
	e.texts = append(e.texts, texts...)
	before := e.before
	e.mu.Unlock()
	if before != nil {
		before(texts)
	}
	out := make([][]float32, len(texts))
	for i, text := range texts {
		v := make([]float32, conceptDims)
		// Only the passage counts, not the title prefix.
		if _, body, ok := strings.Cut(text, "| text: "); ok {
			text = body
		}
		for _, w := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return r < 'a' || r > 'z' }) {
			if d, ok := concepts[strings.TrimSuffix(w, "s")]; ok {
				v[d]++
			}
		}
		v[conceptDims-1] += 0.01 // never the zero vector
		out[i] = v
	}
	return out, nil
}

func (e *conceptEmbedder) stats() (calls int, batches []int, texts []string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls, append([]int(nil), e.batches...), append([]string(nil), e.texts...)
}

func embedAll(t *testing.T, ix *Index, space Space) int {
	t.Helper()
	total := 0
	for i := 0; i < 100; i++ {
		n, err := ix.EmbedPending(context.Background(), space, MaxEmbedBatch)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			return total
		}
		total += n
	}
	t.Fatal("embedding never finished")
	return total
}

func TestEmbedPendingBatchesResumesAndDropsVectorsWithFiles(t *testing.T) {
	ctx := context.Background()
	var long strings.Builder
	for i := 0; i < 70; i++ {
		fmt.Fprintf(&long, "# Section %d\nThe sedan number %d.\n", i, i)
	}
	root, b := localSource(t, map[string][]byte{
		"fleet/cars.md":     []byte(long.String()),
		"hr/leave.md":       []byte("# Leave\nEmployees get 25 vacation days.\n"),
		"pricing/rogers.md": []byte("# Laminates\nRogers 4350B substrate.\n"),
	})
	dir := t.TempDir()
	ix, err := Open(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ix.Sync(ctx, b, 0); err != nil {
		t.Fatal(err)
	}
	emb := &conceptEmbedder{}
	space := Space{Fingerprint: "space-a", DocumentPrefix: "title: none | text: ", Embedder: emb}

	if got := embedAll(t, ix, space); got != 72 {
		t.Fatalf("embedded %d passages, want 72 (70 sections + 2 files; file names are not embedded)", got)
	}
	_, batches, texts := emb.stats()
	for _, n := range batches {
		if n > MaxEmbedBatch {
			t.Fatalf("batch of %d passages exceeds %d", n, MaxEmbedBatch)
		}
	}
	if len(batches) != 2 {
		t.Fatalf("batches = %v, want two", batches)
	}
	var sawTitle bool
	for _, text := range texts {
		if strings.HasPrefix(text, "title: leave.md | text: ") && strings.Contains(text, "vacation") {
			sawTitle = true
		}
		if strings.Contains(text, "title: none") {
			t.Fatalf("file passage kept the placeholder title: %q", text)
		}
	}
	if !sawTitle {
		t.Fatalf("passages were not embedded as title: <file name> | text: <passage>: %q", texts[:3])
	}

	// Vectors survive a restart, so nothing is embedded again.
	ix.Close()
	if ix, err = Open(dir, 0); err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	if n, err := ix.EmbedPending(ctx, space, MaxEmbedBatch); err != nil || n != 0 {
		t.Fatalf("after reopen embedded %d (%v), want 0", n, err)
	}
	if done, total, _ := ix.EmbeddingProgress(ctx, "space-a"); done != total || total != 72 {
		t.Fatalf("progress = %d/%d", done, total)
	}

	// A changed file loses its vectors and only it is embedded again; a
	// removed file's vectors go with it.
	if err := os.WriteFile(filepath.Join(root, "hr", "leave.md"), []byte("# Leave\nEmployees get 30 holiday days.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "pricing", "rogers.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := ix.Sync(ctx, b, 0); err != nil {
		t.Fatal(err)
	}
	var orphans int
	if err := ix.db.QueryRow(`select count(*) from unit_vectors where key = 'pricing/rogers.md'`).Scan(&orphans); err != nil || orphans != 0 {
		t.Fatalf("removed file kept %d vectors (%v)", orphans, err)
	}
	before, _, _ := emb.stats()
	if got := embedAll(t, ix, space); got != 1 {
		t.Fatalf("re-embedded %d passages after one file changed, want 1", got)
	}
	if after, _, texts := emb.stats(); after != before+1 || !strings.Contains(texts[len(texts)-1], "30 holiday days") {
		t.Fatalf("re-embedding did not send just the changed passage: %q", texts[len(texts)-1])
	}

	// Another space replaces this one's vectors.
	if got := embedAll(t, ix, Space{Fingerprint: "space-b", Embedder: emb}); got != 71 {
		t.Fatalf("new space embedded %d passages, want 71", got)
	}
	var stale int
	if err := ix.db.QueryRow(`select count(*) from unit_vectors where fingerprint <> 'space-b'`).Scan(&stale); err != nil || stale != 0 {
		t.Fatalf("%d vectors from the old space remain (%v)", stale, err)
	}
}

func TestEmbedPendingSkipsPassagesReplacedMidRequest(t *testing.T) {
	ctx := context.Background()
	root, b := localSource(t, map[string][]byte{"hr/leave.md": []byte("# Leave\nEmployees get 25 vacation days.\n")})
	ix, err := Open(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	if _, err := ix.Sync(ctx, b, 0); err != nil {
		t.Fatal(err)
	}
	emb := &conceptEmbedder{}
	emb.before = func([]string) {
		emb.before = nil
		if err := os.WriteFile(filepath.Join(root, "hr", "leave.md"), []byte("# Leave\nEmployees get 30 pto days.\n"), 0o644); err != nil {
			t.Error(err)
		}
		if _, err := ix.Sync(ctx, b, 0); err != nil {
			t.Error(err)
		}
	}
	space := Space{Fingerprint: "s", Embedder: emb}
	if _, err := ix.EmbedPending(ctx, space, MaxEmbedBatch); err != nil {
		t.Fatal(err)
	}
	var stored int
	if err := ix.db.QueryRow(`select count(*) from unit_vectors`).Scan(&stored); err != nil || stored != 0 {
		t.Fatalf("stored %d vectors for a passage that changed mid-request", stored)
	}
	embedAll(t, ix, space)
	if done, total, _ := ix.EmbeddingProgress(ctx, "s"); done != 1 || total != 1 {
		t.Fatalf("progress = %d/%d after retry", done, total)
	}
}

func TestSearchSemanticRanksByEmbeddingsAndFillsInUnembeddedFiles(t *testing.T) {
	ctx := context.Background()
	root, b := localSource(t, map[string][]byte{
		"fleet/cars.md":     []byte("# Fleet\nEvery sedan is serviced each spring.\n"),
		"hr/leave.md":       []byte("# Leave\nEmployees get 25 vacation days.\n"),
		"pricing/rogers.md": []byte("# Laminates\nRogers 4350B substrate.\n"),
	})
	ix, err := Open(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	if _, err := ix.Sync(ctx, b, 0); err != nil {
		t.Fatal(err)
	}
	emb := &conceptEmbedder{}
	space := Space{Fingerprint: "s", DocumentPrefix: "title: none | text: ", Embedder: emb}
	query := "automobile maintenance"
	qv, _ := emb.Embed(ctx, []string{query})

	// Nothing embedded: exactly the full-text result.
	lexical, err := ix.Search(ctx, query, 5)
	if err != nil {
		t.Fatal(err)
	}
	got, semantic, err := ix.SearchSemantic(ctx, query, qv[0], "s", 5)
	if err != nil || semantic || len(got) != len(lexical) {
		t.Fatalf("unembedded index: semantic=%v %d matches (%v), want the %d full-text ones", semantic, len(got), err, len(lexical))
	}

	embedAll(t, ix, space)
	got, semantic, err = ix.SearchSemantic(ctx, query, qv[0], "s", 2)
	if err != nil || !semantic {
		t.Fatalf("semantic=%v err=%v", semantic, err)
	}
	if len(lexical) != 0 {
		t.Fatalf("fixture broke: full-text search found %v for a query sharing no word", lexical)
	}
	if len(got) != 2 || got[0].Key != "fleet/cars.md" {
		t.Fatalf("semantic ranking = %+v, want fleet/cars.md first", got)
	}
	if len(got[0].Passages) == 0 || !strings.Contains(got[0].Passages[0].Snippet, "sedan") {
		t.Fatalf("passages = %+v", got[0].Passages)
	}

	// A new file is findable by full text before the embedder reaches it.
	if err := os.WriteFile(filepath.Join(root, "fleet", "maintenance.md"), []byte("# Automobile maintenance\nOil every 10000 km.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ix.Sync(ctx, b, 0); err != nil {
		t.Fatal(err)
	}
	got, _, err = ix.SearchSemantic(ctx, query, qv[0], "s", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 || got[0].Key != "fleet/maintenance.md" {
		t.Fatalf("unembedded file missing from its full-text place: %+v", got)
	}
	if len(got) < 2 || got[1].Key != "fleet/cars.md" {
		t.Fatalf("embedded files no longer follow: %+v", got)
	}
	keys := map[string]bool{}
	for _, m := range got {
		if keys[m.Key] {
			t.Fatalf("file %s returned twice: %+v", m.Key, got)
		}
		keys[m.Key] = true
	}
}

func TestSpaceDocumentUsesFileNameAsTitle(t *testing.T) {
	for _, tc := range []struct{ prefix, want string }{
		{"", "body"},
		{"title: none | text: ", "title: Q4 prices.pdf | text: body"},
		{"passage: ", "passage: body"},
	} {
		if got := (Space{DocumentPrefix: tc.prefix}).Document("pricing/Q4 prices.pdf", "body"); got != tc.want {
			t.Fatalf("prefix %q: %q, want %q", tc.prefix, got, tc.want)
		}
	}
	if got := (Space{}).Document("a.md", strings.Repeat("x", maxEmbedChars+10)); len(got) != maxEmbedChars {
		t.Fatalf("document of %d chars, want capped at %d", len(got), maxEmbedChars)
	}
}

func TestSnippetCentresOnQueryWords(t *testing.T) {
	body := strings.Repeat("filler ", 50) + "the sedan fleet is serviced " + strings.Repeat("tail ", 50)
	got := snippet(body, "sedan service")
	if !strings.Contains(got, "«sedan»") || !strings.HasPrefix(got, "…") || !strings.HasSuffix(got, "…") {
		t.Fatalf("snippet = %q", got)
	}
}

func TestEmbedPendingSizesRequestsByLength(t *testing.T) {
	ctx := context.Background()
	var doc strings.Builder
	for i := 0; i < 6; i++ {
		fmt.Fprintf(&doc, "# Part %d\n%s\n", i, strings.Repeat("sedan ", 15))
	}
	_, b := localSource(t, map[string][]byte{"fleet/cars.md": []byte(doc.String())})
	ix, err := Open(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	if _, err := ix.Sync(ctx, b, 0); err != nil {
		t.Fatal(err)
	}
	emb := &conceptEmbedder{}
	// Each passage document is about 120 characters.
	space := Space{Fingerprint: "s", Embedder: emb, MaxRequestChars: 250}
	if got := embedAll(t, ix, space); got != 6 {
		t.Fatalf("embedded %d passages, want 6", got)
	}
	if _, batches, _ := emb.stats(); len(batches) != 3 {
		t.Fatalf("batches = %v, want three of two passages", batches)
	}
	// A single passage longer than the cap is still sent alone.
	emb2 := &conceptEmbedder{}
	if got := embedAll(t, ix, Space{Fingerprint: "t", Embedder: emb2, MaxRequestChars: 10}); got != 6 {
		t.Fatalf("embedded %d passages under a tiny cap, want 6", got)
	}
	if _, batches, _ := emb2.stats(); len(batches) != 6 {
		t.Fatalf("batches = %v, want one passage each", batches)
	}
}
