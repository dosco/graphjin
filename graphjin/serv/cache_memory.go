package serv

import (
	"context"
	"sync"
	"time"

	"github.com/dosco/graphjin/core/v3"
	lru "github.com/hashicorp/golang-lru/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

// Default memory cache size (number of entries)
const defaultMemoryCacheSize = 10000

// memoryCacheEntry wraps a cache entry with expiration info
type memoryCacheEntry struct {
	entry      CacheEntry
	refs       []core.RowRef
	storedAt   time.Time
	queryStart time.Time
}

// MemoryCache provides in-memory LRU response caching with row-level invalidation
type MemoryCache struct {
	cache        *lru.Cache[string, *memoryCacheEntry]
	conf         CachingConfig
	metrics      *CacheMetrics
	excludeTable map[string]bool

	// Row index: rowKey -> set of response keys
	rowIndex   map[string]map[string]bool
	tableIndex map[string]map[string]bool
	modTimes   map[string]int64 // modKey -> modification timestamp (ms)
	mu         sync.RWMutex

	// OpenTelemetry metric instruments
	otelHitCounter          metric.Int64Counter
	otelMissCounter         metric.Int64Counter
	otelInvalidationCounter metric.Int64Counter
	otelErrorCounter        metric.Int64Counter
	otelBytesCachedGauge    metric.Int64UpDownCounter
	otelBytesSavedGauge     metric.Int64UpDownCounter
}

// NewMemoryCache creates a new in-memory LRU cache
func NewMemoryCache(conf CachingConfig, maxEntries int) (*MemoryCache, error) {
	if maxEntries <= 0 {
		maxEntries = defaultMemoryCacheSize
	}

	cache, err := lru.New[string, *memoryCacheEntry](maxEntries)
	if err != nil {
		return nil, err
	}

	mc := &MemoryCache{
		cache:        cache,
		conf:         conf,
		metrics:      &CacheMetrics{},
		excludeTable: make(map[string]bool),
		rowIndex:     make(map[string]map[string]bool),
		tableIndex:   make(map[string]map[string]bool),
		modTimes:     make(map[string]int64),
	}

	// Build exclude table lookup
	for _, t := range conf.ExcludeTables {
		mc.excludeTable[t] = true
	}

	// Initialize OpenTelemetry metrics
	meter := otel.Meter("graphjin.com/cache")

	mc.otelHitCounter, _ = meter.Int64Counter("graphjin.cache.hits",
		metric.WithDescription("Number of cache hits"))
	mc.otelMissCounter, _ = meter.Int64Counter("graphjin.cache.misses",
		metric.WithDescription("Number of cache misses"))
	mc.otelInvalidationCounter, _ = meter.Int64Counter("graphjin.cache.invalidations",
		metric.WithDescription("Number of cache invalidations"))
	mc.otelErrorCounter, _ = meter.Int64Counter("graphjin.cache.errors",
		metric.WithDescription("Number of cache errors"))
	mc.otelBytesCachedGauge, _ = meter.Int64UpDownCounter("graphjin.cache.bytes_cached",
		metric.WithDescription("Total bytes stored in cache"))
	mc.otelBytesSavedGauge, _ = meter.Int64UpDownCounter("graphjin.cache.bytes_saved",
		metric.WithDescription("Bytes saved via compression"))

	return mc, nil
}

// Get retrieves a cached response
// Returns (data, isStale, found)
func (mc *MemoryCache) Get(ctx context.Context, key string) ([]byte, bool, bool) {
	entry, ok := mc.cache.Get(key)
	if !ok {
		mc.recordMiss(ctx)
		return nil, false, false
	}

	now := time.Now().Unix()

	// Expired (past hard TTL)
	if now >= entry.entry.StaleUntil {
		mc.cache.Remove(key)
		mc.recordMiss(ctx)
		return nil, false, false
	}

	// Decompress if needed
	respData := entry.entry.Data
	if entry.entry.Compressed {
		var err error
		respData, err = decompress(entry.entry.Data)
		if err != nil {
			mc.recordError(ctx)
			return nil, false, false
		}
	}

	mc.recordHit(ctx)

	// Check if stale (past soft TTL but before hard TTL)
	isStale := now >= entry.entry.FreshUntil
	return respData, isStale, true
}

// Set stores a response with row-level indices
func (mc *MemoryCache) Set(
	ctx context.Context,
	key string,
	data []byte,
	refs []core.RowRef,
	queryStartTime time.Time,
) error {
	// Filter out excluded tables
	filteredRefs := mc.filterExcludedTables(refs)

	// Check for race condition - verify no rows were modified during query
	if len(filteredRefs) > 0 {
		safe := mc.checkModificationSafety(filteredRefs, queryStartTime)
		if !safe {
			return nil
		}
	}

	// Compress if beneficial
	compressed := false
	originalSize := len(data)

	if len(data) > compressionThreshold {
		compData, err := compress(data)
		if err == nil && len(compData) < len(data) {
			saved := int64(len(data) - len(compData))
			mc.metrics.BytesSaved.Add(saved)
			if mc.otelBytesSavedGauge != nil {
				mc.otelBytesSavedGauge.Add(ctx, saved)
			}
			data = compData
			compressed = true
		}
	}

	now := time.Now()
	ttl := time.Duration(mc.conf.TTL) * time.Second
	freshTTL := time.Duration(mc.conf.FreshTTL) * time.Second
	if freshTTL == 0 {
		freshTTL = ttl // No SWR - fresh until hard TTL
	}

	entry := &memoryCacheEntry{
		entry: CacheEntry{
			Data:         data,
			Compressed:   compressed,
			OriginalSize: originalSize,
			FreshUntil:   now.Add(freshTTL).Unix(),
			StaleUntil:   now.Add(ttl).Unix(),
		},
		refs:       filteredRefs,
		storedAt:   now,
		queryStart: queryStartTime,
	}

	// Store in cache
	mc.cache.Add(key, entry)

	// Update indices
	mc.mu.Lock()
	defer mc.mu.Unlock()

	if len(filteredRefs) <= rowLevelThreshold {
		// Row-level indexing
		for _, ref := range filteredRefs {
			rowKey := mc.rowKey(ref.Table, ref.ID)
			if mc.rowIndex[rowKey] == nil {
				mc.rowIndex[rowKey] = make(map[string]bool)
			}
			mc.rowIndex[rowKey][key] = true
		}
	} else {
		// Table-level indexing for large results
		tables := make(map[string]bool)
		for _, ref := range filteredRefs {
			tables[ref.Table] = true
		}
		for table := range tables {
			if mc.tableIndex[table] == nil {
				mc.tableIndex[table] = make(map[string]bool)
			}
			mc.tableIndex[table][key] = true
		}
	}

	cached := int64(len(data))
	mc.metrics.BytesCached.Add(cached)
	if mc.otelBytesCachedGauge != nil {
		mc.otelBytesCachedGauge.Add(ctx, cached)
	}
	return nil
}

// InvalidateRows invalidates cache entries for specific rows
func (mc *MemoryCache) InvalidateRows(ctx context.Context, refs []core.RowRef) error {
	if len(refs) == 0 {
		return nil
	}

	// Filter out excluded tables
	filteredRefs := mc.filterExcludedTables(refs)
	if len(filteredRefs) == 0 {
		return nil
	}

	now := time.Now().UnixMilli()

	mc.mu.Lock()
	defer mc.mu.Unlock()

	// Record modification timestamps
	for _, ref := range filteredRefs {
		modKey := mc.modKey(ref.Table, ref.ID)
		mc.modTimes[modKey] = now
	}

	// Collect all response keys to invalidate
	keysToDelete := make(map[string]bool)

	// From row-level indices
	for _, ref := range filteredRefs {
		rowKey := mc.rowKey(ref.Table, ref.ID)
		for respKey := range mc.rowIndex[rowKey] {
			keysToDelete[respKey] = true
		}
		delete(mc.rowIndex, rowKey)
	}

	// From table-level indices
	tables := make(map[string]bool)
	for _, ref := range filteredRefs {
		tables[ref.Table] = true
	}
	for table := range tables {
		for respKey := range mc.tableIndex[table] {
			keysToDelete[respKey] = true
		}
		delete(mc.tableIndex, table)
	}

	// Delete cached responses
	for key := range keysToDelete {
		mc.cache.Remove(key)
	}

	mc.recordInvalidation(ctx, int64(len(keysToDelete)))
	return nil
}

// checkModificationSafety verifies no rows were modified during query execution
func (mc *MemoryCache) checkModificationSafety(refs []core.RowRef, queryStartTime time.Time) bool {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	queryStartMs := queryStartTime.UnixMilli()
	for _, ref := range refs {
		modKey := mc.modKey(ref.Table, ref.ID)
		if ts, ok := mc.modTimes[modKey]; ok {
			if ts > queryStartMs {
				// Row was modified during query - unsafe to cache
				return false
			}
		}
	}
	return true
}

// filterExcludedTables removes refs for excluded tables
func (mc *MemoryCache) filterExcludedTables(refs []core.RowRef) []core.RowRef {
	if len(mc.excludeTable) == 0 {
		return refs
	}

	filtered := make([]core.RowRef, 0, len(refs))
	for _, ref := range refs {
		if !mc.excludeTable[ref.Table] {
			filtered = append(filtered, ref)
		}
	}
	return filtered
}

// Key helpers
func (mc *MemoryCache) rowKey(table, id string) string {
	return "row:" + table + ":" + id
}

func (mc *MemoryCache) modKey(table, id string) string {
	return "mod:" + table + ":" + id
}

// Metric recording helpers (record both internal metrics and OTel metrics)
func (mc *MemoryCache) recordHit(ctx context.Context) {
	mc.metrics.Hits.Add(1)
	if mc.otelHitCounter != nil {
		mc.otelHitCounter.Add(ctx, 1)
	}
}

func (mc *MemoryCache) recordMiss(ctx context.Context) {
	mc.metrics.Misses.Add(1)
	if mc.otelMissCounter != nil {
		mc.otelMissCounter.Add(ctx, 1)
	}
}

func (mc *MemoryCache) recordError(ctx context.Context) {
	mc.metrics.Errors.Add(1)
	if mc.otelErrorCounter != nil {
		mc.otelErrorCounter.Add(ctx, 1)
	}
}

func (mc *MemoryCache) recordInvalidation(ctx context.Context, count int64) {
	mc.metrics.Invalidations.Add(count)
	if mc.otelInvalidationCounter != nil {
		mc.otelInvalidationCounter.Add(ctx, count)
	}
}

// Metrics returns the cache metrics
func (mc *MemoryCache) Metrics() *CacheMetrics {
	return mc.metrics
}

// Close is a no-op for memory cache
func (mc *MemoryCache) Close() error {
	mc.cache.Purge()
	return nil
}
