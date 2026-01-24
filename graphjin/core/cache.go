package core

import (
	"context"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
)

// ResponseCacheProvider defines the interface for response caching.
// This is implemented by the service layer (serv package) to provide
// Redis-based caching with row-level invalidation.
type ResponseCacheProvider interface {
	// Get retrieves a cached response by key.
	// Returns (data, isStale, found). isStale is true if the entry is past soft TTL (SWR).
	Get(ctx context.Context, key string) (data []byte, isStale bool, found bool)

	// Set stores a response with row-level indices for invalidation.
	// refs contains (table, row_id) pairs for fine-grained cache invalidation.
	// queryStartTime is used for race condition detection.
	Set(ctx context.Context, key string, data []byte, refs []RowRef, queryStartTime time.Time) error

	// InvalidateRows invalidates cache entries for specific rows.
	// Called after mutations with the affected row IDs.
	InvalidateRows(ctx context.Context, refs []RowRef) error
}

// Cache provides local in-memory caching for APQ and introspection
type Cache struct {
	cache *lru.TwoQueueCache[string, []byte]
}

// initCache initializes the cache
func (gj *graphjinEngine) initCache() (err error) {
	gj.cache.cache, err = lru.New2Q[string, []byte](5000)
	return
}

// Get returns the value from the cache
func (c Cache) Get(key string) (val []byte, fromCache bool) {
	val, fromCache = c.cache.Get(key)
	return
}

// Set sets the value in the cache
func (c Cache) Set(key string, val []byte) {
	c.cache.Add(key, val)
}
