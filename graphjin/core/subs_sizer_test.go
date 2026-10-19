package core

import (
	"testing"
	"time"
)

func newTestSizer(min, max int, target time.Duration, initial int) *chunkSizer {
	return newChunkSizer(sizerCfg{
		min:     min,
		max:     max,
		target:  target,
		initial: initial,
	})
}

func TestSizerInitialDefaults(t *testing.T) {
	cfg := resolveSubsSizerConfig(&Config{SubsPollDuration: 5 * time.Second})
	if cfg.min != defaultSubsMinChunk {
		t.Fatalf("min: got %d want %d", cfg.min, defaultSubsMinChunk)
	}
	if cfg.max != defaultSubsMaxChunk {
		t.Fatalf("max: got %d want %d", cfg.max, defaultSubsMaxChunk)
	}
	if cfg.target != (5*time.Second)/4 {
		t.Fatalf("target: got %s want %s", cfg.target, (5*time.Second)/4)
	}
	if cfg.initial != defaultSubsInitialChunk {
		t.Fatalf("initial: got %d want %d", cfg.initial, defaultSubsInitialChunk)
	}
}

func TestSizerInitialMatchesLegacyConstant(t *testing.T) {
	z := newChunkSizer(resolveSubsSizerConfig(&Config{SubsPollDuration: 5 * time.Second}))
	if got := z.current(); got != 2000 {
		t.Fatalf("first-cycle chunk size must match the old hardcoded value: got %d want 2000", got)
	}
}

func TestSizerCustomBoundsClampInitial(t *testing.T) {
	cfg := resolveSubsSizerConfig(&Config{
		SubsPollDuration:        5 * time.Second,
		SubsMaxMembersPerWorker: 500,
	})
	if cfg.initial != 500 {
		t.Fatalf("initial should clamp to max: got %d want 500", cfg.initial)
	}

	cfg = resolveSubsSizerConfig(&Config{
		SubsPollDuration:        5 * time.Second,
		SubsMinMembersPerWorker: 3000,
	})
	if cfg.initial != 3000 {
		t.Fatalf("initial should clamp to min: got %d want 3000", cfg.initial)
	}
}

func TestSizerMinClampedAgainstMax(t *testing.T) {
	cfg := resolveSubsSizerConfig(&Config{
		SubsPollDuration:        5 * time.Second,
		SubsMinMembersPerWorker: 9000,
		SubsMaxMembersPerWorker: 1000,
	})
	if cfg.min != cfg.max {
		t.Fatalf("min must be clamped down to max: got min=%d max=%d", cfg.min, cfg.max)
	}
}

func TestSizerTargetOverride(t *testing.T) {
	cfg := resolveSubsSizerConfig(&Config{
		SubsPollDuration:       5 * time.Second,
		SubsTargetQueryLatency: 100 * time.Millisecond,
	})
	if cfg.target != 100*time.Millisecond {
		t.Fatalf("target override ignored: got %s", cfg.target)
	}
}

func TestSizerGrowsWhenFast(t *testing.T) {
	target := 1 * time.Second
	z := newTestSizer(50, 5000, target, 1000)

	// Many fast observations should walk cur up toward max.
	fast := target / 10
	for i := 0; i < 200; i++ {
		z.observe(fast)
	}
	if got := z.current(); got != 5000 {
		t.Fatalf("expected sizer to saturate at max under sustained fast latency: got %d", got)
	}
}

func TestSizerShrinksWhenSlow(t *testing.T) {
	target := 1 * time.Second
	z := newTestSizer(50, 5000, target, 4000)

	slow := target * 3
	prev := z.current()
	for i := 0; i < 200; i++ {
		z.observe(slow)
		cur := z.current()
		if cur > prev {
			t.Fatalf("sizer grew under slow latency: was %d now %d", prev, cur)
		}
		prev = cur
	}
	if got := z.current(); got != 50 {
		t.Fatalf("expected sizer to converge to min under sustained slow latency: got %d", got)
	}
}

func TestSizerDeadband(t *testing.T) {
	target := 1 * time.Second
	z := newTestSizer(50, 5000, target, 1000)

	// Observation right at target should land in the [0.8x, 1.2x] deadband.
	for i := 0; i < 50; i++ {
		z.observe(target)
	}
	if got := z.current(); got != 1000 {
		t.Fatalf("expected no change inside deadband: got %d want 1000", got)
	}
}

func TestSizerErrorOnlyShrinks(t *testing.T) {
	target := 1 * time.Second
	z := newTestSizer(50, 5000, target, 1000)

	z.observeError()
	if got := z.current(); got != 500 {
		t.Fatalf("first error should halve cur: got %d want 500", got)
	}

	for i := 0; i < 100; i++ {
		z.observeError()
	}
	if got := z.current(); got != 50 {
		t.Fatalf("repeated errors must respect floor: got %d want 50", got)
	}
}

func TestSizerEMAAbsorbsModerateSpike(t *testing.T) {
	target := 1 * time.Second
	z := newTestSizer(50, 5000, target, 1000)

	// Seed at exactly target so EMA = target.
	for i := 0; i < 5; i++ {
		z.observe(target)
	}
	before := z.current()

	// 1.5x spike: new EMA = 0.7*target + 0.3*1.5*target = 1.15*target,
	// which is INSIDE the 1.2x deadband — so cur must not move.
	z.observe(time.Duration(float64(target) * 1.5))

	if z.current() != before {
		t.Fatalf("moderate spike (1.5x) must be absorbed by EMA into the deadband: before=%d after=%d",
			before, z.current())
	}
}

func TestSizerLargeSpikeShrinkIsBounded(t *testing.T) {
	target := 1 * time.Second
	z := newTestSizer(50, 5000, target, 1000)

	for i := 0; i < 5; i++ {
		z.observe(target)
	}
	before := z.current()

	// 2x spike: EMA = 1.3*target — outside deadband — shrink expected,
	// but multiplicative (0.7x), not collapse.
	z.observe(target * 2)

	shrunk := z.current()
	if shrunk >= before {
		t.Fatalf("large spike should shrink cur: before=%d after=%d", before, shrunk)
	}
	if shrunk < int(float64(before)*0.5) {
		t.Fatalf("single large spike caused excessive shrink: before=%d after=%d", before, shrunk)
	}
}

func TestSizerBoundsClamp(t *testing.T) {
	target := 1 * time.Second
	z := newTestSizer(100, 1500, target, 1500)

	for i := 0; i < 1000; i++ {
		z.observe(target / 100) // very fast
	}
	if got := z.current(); got != 1500 {
		t.Fatalf("must clamp to max: got %d", got)
	}

	for i := 0; i < 1000; i++ {
		z.observe(target * 100) // very slow
	}
	if got := z.current(); got != 100 {
		t.Fatalf("must clamp to min: got %d", got)
	}
}

func TestSizerNoExtremeOscillation(t *testing.T) {
	target := 1 * time.Second
	z := newTestSizer(50, 5000, target, 1000)

	// Alternating in-band observations should leave cur unchanged.
	below := (target * 9) / 10  // inside deadband (≥ 0.8x)
	above := (target * 11) / 10 // inside deadband (≤ 1.2x)
	for i := 0; i < 100; i++ {
		if i%2 == 0 {
			z.observe(below)
		} else {
			z.observe(above)
		}
	}
	if got := z.current(); got != 1000 {
		t.Fatalf("oscillation inside deadband should not move cur: got %d", got)
	}
}

func TestSizerCurrentReadOncePerChunkConsistency(t *testing.T) {
	// The contract is that current() reads atomically under the mutex.
	// This test just exercises concurrent observation + reads to flush
	// out any race the mutex would catch under -race.
	target := 1 * time.Second
	z := newTestSizer(50, 5000, target, 1000)

	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			z.observe(target / 4)
		}
		close(done)
	}()
	for i := 0; i < 1000; i++ {
		_ = z.current()
	}
	<-done
}
