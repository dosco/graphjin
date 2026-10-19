package eval

import (
	"context"
	"sync"
	"testing"
	"time"
)

// staticPool leases a fixed set of instances and records what was handed out.
type staticPool struct {
	mu     sync.Mutex
	free   chan Instance
	leased map[string]int
	open   int
	peak   int
}

func newStaticPool(instances ...Instance) *staticPool {
	pool := &staticPool{free: make(chan Instance, len(instances)), leased: map[string]int{}}
	for _, instance := range instances {
		pool.free <- instance
	}
	return pool
}

func (p *staticPool) Acquire(ctx context.Context) (Instance, error) {
	select {
	case instance := <-p.free:
		p.mu.Lock()
		p.leased[instance.BaseURL()]++
		p.open++
		if p.open > p.peak {
			p.peak = p.open
		}
		p.mu.Unlock()
		return instance, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (p *staticPool) Release(instance Instance) error {
	p.mu.Lock()
	p.open--
	p.mu.Unlock()
	p.free <- instance
	return nil
}

func (p *staticPool) Close() error { return nil }

func (p *staticPool) stats() (workers, peak int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.leased), p.peak
}

func poolWorker(url string, resets *int64, mu *sync.Mutex) Instance {
	return &ResettableStaticInstance{
		StaticInstance: &StaticInstance{URL: url, TargetLabel: "demo"},
		ResetFunc: func(context.Context) error {
			mu.Lock()
			*resets++
			mu.Unlock()
			return nil
		},
	}
}

// Episodes must run against the instance they leased. A pool that booted
// several worlds while every episode still went to the one the run was
// prepared with would look like it was working and buy nothing.
func TestPooledRunSpreadsEpisodesAcrossWorkers(t *testing.T) {
	probe := &concurrencyProbe{}
	suite := concurrencySuite(t, 8, 0)
	var resets int64
	var resetMu sync.Mutex
	pool := newStaticPool(
		poolWorker("http://worker-one", &resets, &resetMu),
		poolWorker("http://worker-two", &resets, &resetMu),
		poolWorker("http://worker-three", &resets, &resetMu),
	)
	prepared := &StaticInstance{URL: "http://worker-one", TargetLabel: "demo"}

	report, err := (Runner{Client: concurrencyDoer(t, probe, 20*time.Millisecond)}).Run(
		context.Background(), suite, prepared,
		RunOptions{Repeats: 2, Seed: 23, Store: NewStore(t.TempDir()), Concurrency: 3, Pool: pool},
	)
	if err != nil {
		t.Fatal(err)
	}
	if report.RunStatus != RunStatusComplete {
		t.Fatalf("run status = %s", report.RunStatus)
	}
	workers, peak := pool.stats()
	if workers != 3 {
		t.Fatalf("expected all three workers to serve episodes, got %d", workers)
	}
	if peak < 2 {
		t.Fatalf("expected episodes to hold workers concurrently, peak was %d", peak)
	}
	if probe.violations.Load() != 0 {
		t.Fatalf("recorded %d isolation violations", probe.violations.Load())
	}
}

// With a world each, a write no longer has to stop the run: it resets only the
// instance it leased, so reads keep flowing on the others. Against a single
// shared instance the same suite must still serialize, because a reset there
// would corrupt a concurrent reader's world.
func TestPooledWritesDoNotBlockConcurrentReads(t *testing.T) {
	probe := &concurrencyProbe{}
	suite := concurrencySuite(t, 6, 2)
	var resets int64
	var resetMu sync.Mutex
	pool := newStaticPool(
		poolWorker("http://worker-one", &resets, &resetMu),
		poolWorker("http://worker-two", &resets, &resetMu),
		poolWorker("http://worker-three", &resets, &resetMu),
	)
	prepared := &ResettableStaticInstance{
		StaticInstance: &StaticInstance{URL: "http://worker-one", TargetLabel: "demo"},
		ResetFunc:      func(context.Context) error { return nil },
	}
	report, err := (Runner{Client: concurrencyDoer(t, probe, 20*time.Millisecond)}).Run(
		context.Background(), suite, prepared,
		RunOptions{Repeats: 2, Seed: 23, Store: NewStore(t.TempDir()), Concurrency: 3, Pool: pool},
	)
	if err != nil {
		t.Fatal(err)
	}
	if report.RunStatus != RunStatusComplete {
		t.Fatalf("run status = %s", report.RunStatus)
	}
	// Mutation episodes reset the world they leased, before and after.
	resetMu.Lock()
	observed := resets
	resetMu.Unlock()
	if observed == 0 {
		t.Fatal("mutation episodes never reset the world they leased")
	}
	if _, peak := pool.stats(); peak < 2 {
		t.Fatalf("a pooled write still serialized the run, peak lease was %d", peak)
	}
}

// Without a pool the runner keeps its historical behavior exactly, including
// the exclusive gate that stops a reset from corrupting a concurrent reader.
func TestUnpooledRunStillSerializesWrites(t *testing.T) {
	probe := &concurrencyProbe{}
	suite := concurrencySuite(t, 6, 2)
	instance := &ResettableStaticInstance{
		StaticInstance: &StaticInstance{URL: "http://graphjin.test", TargetLabel: "test"},
		ResetFunc: func(context.Context) error {
			if probe.readers.Load() > 0 {
				probe.violations.Add(1)
			}
			return nil
		},
	}
	report, err := (Runner{Client: concurrencyDoer(t, probe, 20*time.Millisecond)}).Run(
		context.Background(), suite, instance,
		RunOptions{Repeats: 2, Seed: 23, Store: NewStore(t.TempDir()), Concurrency: 4},
	)
	if err != nil {
		t.Fatal(err)
	}
	if report.RunStatus != RunStatusComplete {
		t.Fatalf("run status = %s", report.RunStatus)
	}
	if got := probe.violations.Load(); got != 0 {
		t.Fatalf("unpooled run overlapped a write with reads %d times", got)
	}
}

// An exhausted pool must make the episode wait, never fall back to a world
// another episode is writing to.
func TestPooledEpisodeFailsWhenNoWorldIsAvailable(t *testing.T) {
	pool := newStaticPool()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := pool.Acquire(ctx); err == nil {
		t.Fatal("expected an exhausted pool to refuse rather than share a world")
	}
}
