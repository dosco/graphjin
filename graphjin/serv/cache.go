package serv

import (
	"context"
	"time"

	"github.com/dosco/graphjin/core/v3"
)

// ResponseCache defines the interface for response caching backends
// Both RedisCache and MemoryCache implement this interface
type ResponseCache interface {
	// Get retrieves a cached response
	// Returns (data, isStale, found)
	Get(ctx context.Context, key string) ([]byte, bool, bool)

	// Set stores a response with row-level indices for invalidation
	Set(ctx context.Context, key string, data []byte, refs []core.RowRef, queryStartTime time.Time) error

	// InvalidateRows invalidates cache entries for specific rows (called after mutations)
	InvalidateRows(ctx context.Context, refs []core.RowRef) error

	// Metrics returns the cache metrics
	Metrics() *CacheMetrics

	// Close releases resources
	Close() error
}
