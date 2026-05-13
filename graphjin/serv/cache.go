package serv

import (
	"context"
	"time"

	"github.com/dosco/graphjin/core/v3"
)

// ResponseCache defines the interface for response caching backends.
// Both RedisCache and MemoryCache implement this interface.
type ResponseCache interface {
	// Get retrieves a cached response
	// Returns (data, isStale, found)
	Get(ctx context.Context, key string) ([]byte, bool, bool)

	// Set stores a response with dependency refs for invalidation.
	Set(ctx context.Context, key string, data []byte, refs []core.RowRef, queryStartTime time.Time) error

	// InvalidateRows invalidates cache entries for dependency refs.
	InvalidateRows(ctx context.Context, refs []core.RowRef) error

	// InvalidateTables invalidates cache entries for whole tables when row-level
	// tracking is insufficient, such as external source reindexing.
	InvalidateTables(ctx context.Context, tables []string) error

	// SubmitRefresh enqueues a stale-while-revalidate refresh on the worker pool.
	// Returns false if SWR is disabled or the pool is full/shutdown.
	SubmitRefresh(key string, fn core.RefreshFn) bool

	// Metrics returns the cache metrics
	Metrics() *CacheMetrics

	// Close releases resources
	Close() error
}

func cacheIndexRefs(refs []core.RowRef, includeExact bool) []core.RowRef {
	if len(refs) == 0 {
		return nil
	}
	out := make([]core.RowRef, 0, len(refs)*2)
	seen := make(map[string]struct{}, len(refs)*2)
	add := func(ref core.RowRef) {
		ref = ref.Normalize()
		key := ref.DependencyKey()
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, ref)
	}
	for _, ref := range refs {
		if includeExact {
			add(ref)
		}
		if tableRef, ok := ref.TableDependency(); ok {
			add(tableRef)
		} else if !includeExact {
			add(ref)
		}
	}
	return out
}

func cacheEntryTTLs(conf CachingConfig, opts core.CacheEntryOptions) (ttl time.Duration, freshTTL time.Duration, ok bool) {
	if opts.NoStore {
		return 0, 0, false
	}
	ttl = time.Duration(conf.TTL) * time.Second
	if ttl <= 0 {
		return 0, 0, false
	}
	freshTTL = time.Duration(conf.FreshTTL) * time.Second
	if freshTTL == 0 {
		freshTTL = ttl
	}
	if opts.HardTTL > 0 && opts.HardTTL < ttl {
		ttl = opts.HardTTL
	}
	if opts.FreshTTL > 0 && opts.FreshTTL < freshTTL {
		freshTTL = opts.FreshTTL
	}
	if freshTTL <= 0 || freshTTL > ttl {
		freshTTL = ttl
	}
	return ttl, freshTTL, true
}

func refExcludedByConfig(exclude map[string]bool, ref core.RowRef) bool {
	if len(exclude) == 0 {
		return false
	}
	ref = ref.Normalize()
	if ref.Table != "" && exclude[ref.Table] {
		return true
	}
	if ref.Scope != "" && exclude[ref.Source+":"+ref.Scope] {
		return true
	}
	if ref.Scope != "" && ref.Table != "" && exclude[ref.Source+":"+ref.Scope+":"+ref.Table] {
		return true
	}
	return false
}
