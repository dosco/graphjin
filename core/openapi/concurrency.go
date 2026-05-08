package openapi

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/time/rate"
)

// Default per-spec concurrency limits when SpecConfig.Concurrency is
// unset. They're conservative because the alternative — unbounded
// goroutine fan-out hammering an upstream — produces real outages.
// Users with friendlier upstream APIs override these in config.
const (
	defaultMaxConcurrent   = 8
	defaultRateLimitPerSec = 50
)

// limiter combines a semaphore (for in-flight concurrency) with a
// token-bucket rate limiter (for sustained request rate). One instance
// per spec; shared across every operation against that spec, so the
// per-vendor budget is enforced collectively rather than per-operation.
type limiter struct {
	sem  chan struct{}
	rate *rate.Limiter
}

func newLimiter(cfg ConcurrencyConfig) *limiter {
	maxC := cfg.MaxConcurrent
	if maxC <= 0 {
		maxC = defaultMaxConcurrent
	}
	rps := cfg.RateLimitPerSec
	if rps <= 0 {
		rps = defaultRateLimitPerSec
	}
	return &limiter{
		sem: make(chan struct{}, maxC),
		// Burst = rate so a momentary spike doesn't blow the budget;
		// over a one-second window the limiter keeps things at rps.
		rate: rate.NewLimiter(rate.Limit(rps), rps),
	}
}

// acquire blocks until both a concurrency slot and a rate-limit token
// are available, or until ctx fires. The caller must invoke release()
// (typically via defer) once it's done with the upstream request.
//
// The acquisition order — concurrency slot first, then rate — matters:
// if we waited on the rate limiter while holding zero slots, a sudden
// burst could exhaust the rate budget without ever bounding concurrency,
// and we'd see vendor errors that masquerade as application bugs.
func (l *limiter) acquire(ctx context.Context) error {
	select {
	case l.sem <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := l.rate.Wait(ctx); err != nil {
		// Hand back the concurrency slot we just took — leaking it
		// would deadlock subsequent callers when ctx is canceled.
		<-l.sem
		return fmt.Errorf("rate limiter: %w", err)
	}
	return nil
}

func (l *limiter) release() {
	<-l.sem
}

// reservation is exposed for tests that want to assert timing behaviour
// without sleeping in a real-time test.
//
//nolint:unused // kept as a documented hook for future test helpers
func (l *limiter) reservationDelay() time.Duration {
	return l.rate.Reserve().Delay()
}
