package eval

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// countingVerifier answers every oracle successfully and records how many
// resolutions were in flight at once.
func countingVerifier(t *testing.T, delay time.Duration) (*Verifier, *int64, *int64) {
	t.Helper()
	var inFlight, peak, total int64
	verifier := &Verifier{Client: doerFunc(func(request *http.Request) (*http.Response, error) {
		current := atomic.AddInt64(&inFlight, 1)
		for {
			seen := atomic.LoadInt64(&peak)
			if current <= seen || atomic.CompareAndSwapInt64(&peak, seen, current) {
				break
			}
		}
		atomic.AddInt64(&total, 1)
		if delay > 0 {
			time.Sleep(delay)
		}
		atomic.AddInt64(&inFlight, -1)
		_ = request
		return jsonResponse(200, `{"data":{"orders":[{"count_id":7}]}}`), nil
	}), BaseURL: "http://graphjin.test"}
	return verifier, &peak, &total
}

func volumeCandidates(n int) []Task {
	tasks := make([]Task, 0, n)
	for i := 0; i < n; i++ {
		task := curatedTask("volume-"+string(rune('a'+i%26))+string(rune('a'+i/26)), DifficultyT1)
		task.Oracle = &OracleSpec{Query: "query { orders { count_id } }", Extract: "orders.0.count_id"}
		task.Answer = AnswerRule{Kind: "number"}
		tasks = append(tasks, task)
	}
	return tasks
}

// Concurrency must change only how long verification takes. A suite that
// depended on it would stop being reproducible from its recorded seed.
func TestVerifyConcurrencyDoesNotChangeTheSuite(t *testing.T) {
	source := staticCatalogSource{snapshot: CatalogSnapshot{Fingerprint: "catalog"}}
	candidates := volumeCandidates(40)
	var reference []byte
	for _, concurrency := range []int{0, 1, 4, 16} {
		verifier, _, _ := countingVerifier(t, 0)
		suite, err := (Generator{Source: source, Verifier: verifier, Now: func() time.Time { return time.Unix(1, 0) }}).
			Generate(context.Background(), GeneratorOptions{
				Seed: 23, Scale: 25, Curated: candidates, VerifyConcurrency: concurrency,
			})
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(suite)
		if err != nil {
			t.Fatal(err)
		}
		if reference == nil {
			reference = encoded
			continue
		}
		if string(reference) != string(encoded) {
			t.Fatalf("concurrency %d produced a different suite", concurrency)
		}
	}
}

func TestVerifyConcurrencyActuallyOverlapsResolutions(t *testing.T) {
	source := staticCatalogSource{snapshot: CatalogSnapshot{Fingerprint: "catalog"}}
	candidates := volumeCandidates(24)

	serialVerifier, serialPeak, serialTotal := countingVerifier(t, 2*time.Millisecond)
	if _, err := (Generator{Source: source, Verifier: serialVerifier, Now: func() time.Time { return time.Unix(1, 0) }}).
		Generate(context.Background(), GeneratorOptions{Seed: 23, Scale: 24, Curated: candidates, VerifyConcurrency: 1}); err != nil {
		t.Fatal(err)
	}
	if peak := atomic.LoadInt64(serialPeak); peak != 1 {
		t.Fatalf("serial verification overlapped: peak %d", peak)
	}

	parallelVerifier, parallelPeak, parallelTotal := countingVerifier(t, 2*time.Millisecond)
	if _, err := (Generator{Source: source, Verifier: parallelVerifier, Now: func() time.Time { return time.Unix(1, 0) }}).
		Generate(context.Background(), GeneratorOptions{Seed: 23, Scale: 24, Curated: candidates, VerifyConcurrency: 8}); err != nil {
		t.Fatal(err)
	}
	if peak := atomic.LoadInt64(parallelPeak); peak < 2 {
		t.Fatalf("concurrent verification never overlapped: peak %d", peak)
	}
	// Both passes must do the same amount of work; concurrency is not a way to
	// verify fewer candidates.
	if atomic.LoadInt64(serialTotal) != atomic.LoadInt64(parallelTotal) {
		t.Fatalf("resolution counts differ: serial %d, concurrent %d",
			atomic.LoadInt64(serialTotal), atomic.LoadInt64(parallelTotal))
	}
}

// A cancelled context must not look like a catalog whose oracles all stopped
// working, which would save a silently truncated suite.
func TestGenerateFailsOnCancelledContextInsteadOfTruncating(t *testing.T) {
	source := staticCatalogSource{snapshot: CatalogSnapshot{Fingerprint: "catalog"}}
	ctx, cancel := context.WithCancel(context.Background())
	var once sync.Once
	verifier := &Verifier{Client: doerFunc(func(*http.Request) (*http.Response, error) {
		once.Do(cancel)
		return jsonResponse(200, `{"data":{"orders":[{"count_id":7}]}}`), nil
	}), BaseURL: "http://graphjin.test"}
	_, err := (Generator{Source: source, Verifier: verifier, Now: func() time.Time { return time.Unix(1, 0) }}).
		Generate(ctx, GeneratorOptions{Seed: 23, Scale: 10, Curated: volumeCandidates(20), VerifyConcurrency: 4})
	if err == nil {
		t.Fatal("expected generation to fail on a cancelled context")
	}
}

func TestUnknownCompositionIsRejected(t *testing.T) {
	source := staticCatalogSource{snapshot: CatalogSnapshot{Fingerprint: "catalog"}}
	verifier, _, _ := countingVerifier(t, 0)
	_, err := (Generator{Source: source, Verifier: verifier}).Generate(context.Background(), GeneratorOptions{
		Seed: 23, Scale: 5, Curated: volumeCandidates(10), Composition: "widest",
	})
	if err == nil || !strings.Contains(err.Error(), "widest") {
		t.Fatalf("expected the unknown composition to be named, got %v", err)
	}
}

func categoryTask(slug string, category Category) Task {
	task := curatedTask(slug, DifficultyT2)
	task.Category = category
	task.Provenance.Source = "catalog-entity"
	task.Oracle = &OracleSpec{Query: "query { orders { count_id } }", Extract: "orders.0.count_id"}
	task.Answer = AnswerRule{Kind: "number"}
	return task
}

// The published sampler holds traversal to its explicit quota and never lets it
// take unused slots, because it had no objective oracle. Coverage exists to
// spread wide, so it must include the category the join-count family feeds.
func TestCoverageSampleIncludesTraversalWhileBenchmarkStillDoesNot(t *testing.T) {
	var tasks []Task
	for i := 0; i < 40; i++ {
		tasks = append(tasks, categoryTask("agg-"+strings.Repeat("x", i+1), CategoryAggregate))
	}
	for i := 0; i < 40; i++ {
		tasks = append(tasks, categoryTask("trav-"+strings.Repeat("y", i+1), CategoryTraversal))
	}
	countTraversal := func(selected []Task) int {
		total := 0
		for _, task := range selected {
			if task.Category == CategoryTraversal {
				total++
			}
		}
		return total
	}
	coverage := countTraversal(coverageSample(tasks, 30, 23))
	if coverage < 10 {
		t.Fatalf("coverage should spread into traversal, got %d of 30", coverage)
	}
	benchmark := countTraversal(stratifiedSample(tasks, 30, 23))
	if benchmark > 2 {
		t.Fatalf("benchmark composition must hold traversal to its quota, got %d of 30", benchmark)
	}
}

// The hand-authored reference set is a fixed few dozen tasks. Kept whole in a
// large suite it becomes a rounding error; uncapped in a small one it crowds
// out everything the catalog could contribute.
func TestCoverageSampleCapsTheCuratedReferenceSet(t *testing.T) {
	var tasks []Task
	for i := 0; i < 60; i++ {
		task := categoryTask("ref-"+strings.Repeat("z", i+1), CategoryAction)
		task.Provenance.Source = "deeporg-reference"
		tasks = append(tasks, task)
	}
	for i := 0; i < 60; i++ {
		tasks = append(tasks, categoryTask("gen-"+strings.Repeat("w", i+1), CategoryAggregate))
	}
	selected := coverageSample(tasks, 40, 23)
	curated := 0
	for _, task := range selected {
		if strings.HasPrefix(task.Provenance.Source, "deeporg-reference") {
			curated++
		}
	}
	if curated == 0 {
		t.Fatal("the reference set must still be represented")
	}
	if curated > 10 {
		t.Fatalf("reference set should be capped at a quarter of the suite, got %d of 40", curated)
	}
}

func TestCoverageSampleIsDeterministic(t *testing.T) {
	var tasks []Task
	for i := 0; i < 50; i++ {
		tasks = append(tasks, categoryTask("agg-"+strings.Repeat("x", i+1), CategoryAggregate))
		tasks = append(tasks, categoryTask("win-"+strings.Repeat("y", i+1), CategoryWindow))
	}
	first := coverageSample(tasks, 33, 23)
	for i := 0; i < 5; i++ {
		again := coverageSample(tasks, 33, 23)
		if len(first) != len(again) {
			t.Fatalf("sample size changed: %d then %d", len(first), len(again))
		}
		for j := range first {
			if first[j].Slug != again[j].Slug {
				t.Fatalf("sample order changed at %d", j)
			}
		}
	}
}
