package serv

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dosco/graphjin/core/v3"
	"golang.org/x/sync/singleflight"
)

// swrTarget is the minimal cache surface the SWR worker pool needs to
// store a refreshed entry and bump metrics.
type swrTarget interface {
	Set(ctx context.Context, key string, data []byte, refs []core.RowRef, queryStartTime time.Time) error
	SetWithOptions(ctx context.Context, key string, data []byte, refs []core.RowRef, queryStartTime time.Time, opts core.CacheEntryOptions) error
	recordSWRRefresh(ctx context.Context)
}

// SWRWorkerPool runs background stale-while-revalidate refreshes.
// Concurrent submissions for the same key are deduplicated via singleflight,
// and the job channel is bounded so a thundering-herd of stale hits cannot
// spawn unbounded goroutines.
type SWRWorkerPool struct {
	jobs         chan swrJob
	target       swrTarget
	wg           sync.WaitGroup
	singleFlight singleflight.Group
	shutdown     atomic.Bool
}

type swrJob struct {
	key string
	fn  core.RefreshFnWithOptions
}

// NewSWRWorkerPool starts a fixed-size pool of refresh workers.
func NewSWRWorkerPool(size int, target swrTarget) *SWRWorkerPool {
	if size <= 0 {
		size = 1
	}
	p := &SWRWorkerPool{
		jobs:   make(chan swrJob, size*2),
		target: target,
	}
	for i := 0; i < size; i++ {
		p.wg.Add(1)
		go p.worker()
	}
	return p
}

func (p *SWRWorkerPool) worker() {
	defer p.wg.Done()
	for job := range p.jobs {
		if p.shutdown.Load() {
			return
		}
		// Single-flight: only one refresh per key at a time across the pool.
		_, _, _ = p.singleFlight.Do(job.key, func() (interface{}, error) {
			ctx := context.Background()
			data, refs, opts, err := job.fn()
			if err == nil && len(data) > 0 {
				_ = p.target.SetWithOptions(ctx, job.key, data, refs, time.Now(), opts)
				p.target.recordSWRRefresh(ctx)
			}
			return nil, err
		})
	}
}

// TrySubmit enqueues a refresh job. Returns false if the pool is full or
// shutting down — the caller should treat that as "skip this refresh."
func (p *SWRWorkerPool) TrySubmit(key string, fn core.RefreshFn) bool {
	if fn == nil {
		return false
	}
	return p.TrySubmitWithOptions(key, func() ([]byte, []core.RowRef, core.CacheEntryOptions, error) {
		data, refs, err := fn()
		return data, refs, core.CacheEntryOptions{}, err
	})
}

// TrySubmitWithOptions enqueues a refresh job with per-entry cache options.
func (p *SWRWorkerPool) TrySubmitWithOptions(key string, fn core.RefreshFnWithOptions) bool {
	if p.shutdown.Load() || fn == nil || key == "" {
		return false
	}
	select {
	case p.jobs <- swrJob{key: key, fn: fn}:
		return true
	default:
		return false
	}
}

// Shutdown drains and stops workers. Safe to call once.
func (p *SWRWorkerPool) Shutdown() {
	if p.shutdown.Swap(true) {
		return
	}
	close(p.jobs)
	p.wg.Wait()
}
