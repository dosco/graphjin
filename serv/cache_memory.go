package serv

import (
	"context"
	"strings"
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
	indexRefs  []core.RowRef
	storedAt   time.Time
	queryStart time.Time
}

// MemoryCache provides in-memory LRU response caching with row-level invalidation
type MemoryCache struct {
	cache        *lru.Cache[string, *memoryCacheEntry]
	conf         CachingConfig
	metrics      *CacheMetrics
	excludeTable map[string]bool
	workerPool   *SWRWorkerPool

	// Dependency index: dependency key -> set of fragment keys
	rowIndex   map[string]map[string]struct{}
	tableIndex map[string]map[string]struct{}
	modTimes   map[string]int64 // dependency key -> modification timestamp (ms)
	cacheMu    sync.Mutex
	mu         sync.RWMutex

	// OpenTelemetry metric instruments
	otelHitCounter          metric.Int64Counter
	otelMissCounter         metric.Int64Counter
	otelInvalidationCounter metric.Int64Counter
	otelErrorCounter        metric.Int64Counter
	otelSWRRefreshCounter   metric.Int64Counter
	otelBytesCachedGauge    metric.Int64UpDownCounter
	otelBytesSavedGauge     metric.Int64UpDownCounter
}

// NewMemoryCache creates a new in-memory LRU cache
func NewMemoryCache(conf CachingConfig, maxEntries int) (*MemoryCache, error) {
	if maxEntries <= 0 {
		maxEntries = defaultMemoryCacheSize
	}

	mc := &MemoryCache{
		conf:         conf,
		metrics:      &CacheMetrics{},
		excludeTable: make(map[string]bool),
		rowIndex:     make(map[string]map[string]struct{}),
		tableIndex:   make(map[string]map[string]struct{}),
		modTimes:     make(map[string]int64),
	}
	cache, err := lru.NewWithEvict[string, *memoryCacheEntry](maxEntries, mc.removeEntryIndexes)
	if err != nil {
		return nil, err
	}
	mc.cache = cache

	// Build exclude table lookup
	for _, t := range conf.ExcludeTables {
		t = strings.TrimSpace(t)
		if t != "" {
			mc.excludeTable[t] = true
		}
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
	mc.otelSWRRefreshCounter, _ = meter.Int64Counter("graphjin.cache.swr_refreshes",
		metric.WithDescription("Number of SWR background refreshes"))
	mc.otelBytesCachedGauge, _ = meter.Int64UpDownCounter("graphjin.cache.bytes_cached",
		metric.WithDescription("Total bytes stored in cache"))
	mc.otelBytesSavedGauge, _ = meter.Int64UpDownCounter("graphjin.cache.bytes_saved",
		metric.WithDescription("Bytes saved via compression"))

	// Spin up SWR worker pool when stale-while-revalidate is enabled
	// (FreshTTL must be strictly less than TTL, otherwise entries can never go stale).
	if conf.FreshTTL > 0 && conf.TTL > conf.FreshTTL {
		mc.workerPool = NewSWRWorkerPool(swrWorkers, mc)
	}

	return mc, nil
}

// SubmitRefresh enqueues a stale-while-revalidate refresh on the worker pool.
// Returns false if SWR is disabled or the pool is full.
func (mc *MemoryCache) SubmitRefresh(key string, fn core.RefreshFn) bool {
	if mc.workerPool == nil {
		return false
	}
	return mc.workerPool.TrySubmit(key, fn)
}

// SubmitRefreshWithOptions enqueues an option-aware stale-while-revalidate refresh.
func (mc *MemoryCache) SubmitRefreshWithOptions(key string, fn core.RefreshFnWithOptions) bool {
	if mc.workerPool == nil {
		return false
	}
	return mc.workerPool.TrySubmitWithOptions(key, fn)
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
		mc.cacheMu.Lock()
		if current, ok := mc.cache.Peek(key); ok && current == entry {
			mc.cache.Remove(key)
		}
		mc.cacheMu.Unlock()
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
	return mc.SetWithOptions(ctx, key, data, refs, queryStartTime, core.CacheEntryOptions{})
}

// SetWithOptions stores a response with dependency indices and per-entry TTL caps.
func (mc *MemoryCache) SetWithOptions(
	ctx context.Context,
	key string,
	data []byte,
	refs []core.RowRef,
	queryStartTime time.Time,
	opts core.CacheEntryOptions,
) error {
	// Filter out excluded tables
	filteredRefs := mc.filterExcludedTables(refs)

	now := time.Now()
	ttl, freshTTL, ok := cacheEntryTTLs(mc.conf, opts)
	if !ok {
		return nil
	}

	// Compress if beneficial
	compressed := false
	originalSize := len(data)
	savedBytes := int64(0)

	if len(data) > compressionThreshold {
		compData, err := compress(data)
		if err == nil && len(compData) < len(data) {
			savedBytes = int64(len(data) - len(compData))
			data = compData
			compressed = true
		}
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
		indexRefs:  cacheIndexRefs(filteredRefs, len(filteredRefs) <= rowLevelThreshold),
		storedAt:   now,
		queryStart: queryStartTime,
	}

	mc.cacheMu.Lock()
	defer mc.cacheMu.Unlock()

	// Check for race condition - verify no rows were modified during query.
	if len(filteredRefs) > 0 && !mc.checkModificationSafety(filteredRefs, queryStartTime) {
		return nil
	}

	if previous, exists := mc.cache.Peek(key); exists {
		mc.removeEntryIndexes(key, previous)
	}
	mc.cache.Add(key, entry)

	mc.mu.Lock()
	mc.addEntryIndexesLocked(key, entry)
	mc.mu.Unlock()

	if savedBytes > 0 {
		mc.metrics.BytesSaved.Add(savedBytes)
		if mc.otelBytesSavedGauge != nil {
			mc.otelBytesSavedGauge.Add(ctx, savedBytes)
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

	mc.cacheMu.Lock()
	defer mc.cacheMu.Unlock()

	mc.mu.Lock()

	invalidateRefs := cacheIndexRefs(filteredRefs, true)
	keysToDelete := make(map[string]struct{})

	for _, ref := range invalidateRefs {
		depKey := ref.DependencyKey()
		mc.modTimes[depKey] = now
		for respKey := range mc.rowIndex[depKey] {
			keysToDelete[respKey] = struct{}{}
		}
		delete(mc.rowIndex, depKey)
		for respKey := range mc.tableIndex[depKey] {
			keysToDelete[respKey] = struct{}{}
		}
		delete(mc.tableIndex, depKey)
	}
	mc.mu.Unlock()

	for key := range keysToDelete {
		mc.cache.Remove(key)
	}

	mc.recordInvalidation(ctx, int64(len(keysToDelete)))
	return nil
}

// InvalidateTables invalidates cache entries that reference any of the given tables.
func (mc *MemoryCache) InvalidateTables(ctx context.Context, tables []string) error {
	if len(tables) == 0 {
		return nil
	}
	refs := make([]core.RowRef, 0, len(tables))
	for _, table := range tables {
		table = strings.TrimSpace(table)
		ref := core.RowRef{Kind: core.CacheKindTable, Table: table}
		if table == "" || refExcludedByConfig(mc.excludeTable, ref) {
			continue
		}
		refs = append(refs, ref)
	}
	return mc.InvalidateRows(ctx, refs)
}

// checkModificationSafety verifies no rows were modified during query execution
func (mc *MemoryCache) checkModificationSafety(refs []core.RowRef, queryStartTime time.Time) bool {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	queryStartMs := queryStartTime.UnixMilli()
	for _, ref := range cacheIndexRefs(refs, true) {
		if ts, ok := mc.modTimes[ref.DependencyKey()]; ok && ts > queryStartMs {
			return false
		}
	}
	return true
}

func (mc *MemoryCache) addEntryIndexesLocked(key string, entry *memoryCacheEntry) {
	for _, ref := range entry.indexRefs {
		ref = ref.Normalize()
		depKey := ref.DependencyKey()
		if mc.rowIndex[depKey] == nil {
			mc.rowIndex[depKey] = make(map[string]struct{})
		}
		mc.rowIndex[depKey][key] = struct{}{}
		if ref.Kind == core.CacheKindTable {
			if mc.tableIndex[depKey] == nil {
				mc.tableIndex[depKey] = make(map[string]struct{})
			}
			mc.tableIndex[depKey][key] = struct{}{}
		}
	}
}

func (mc *MemoryCache) removeEntryIndexes(key string, entry *memoryCacheEntry) {
	if entry == nil || len(entry.indexRefs) == 0 {
		return
	}
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.removeEntryIndexesLocked(key, entry)
}

func (mc *MemoryCache) removeEntryIndexesLocked(key string, entry *memoryCacheEntry) {
	for _, ref := range entry.indexRefs {
		ref = ref.Normalize()
		depKey := ref.DependencyKey()
		if keys := mc.rowIndex[depKey]; keys != nil {
			delete(keys, key)
			if len(keys) == 0 {
				delete(mc.rowIndex, depKey)
			}
		}
		if ref.Kind == core.CacheKindTable {
			if keys := mc.tableIndex[depKey]; keys != nil {
				delete(keys, key)
				if len(keys) == 0 {
					delete(mc.tableIndex, depKey)
				}
			}
		}
	}
}

// filterExcludedTables removes refs excluded by table or source-aware keys.
func (mc *MemoryCache) filterExcludedTables(refs []core.RowRef) []core.RowRef {
	if len(mc.excludeTable) == 0 {
		return refs
	}

	filtered := make([]core.RowRef, 0, len(refs))
	for _, ref := range refs {
		if !refExcludedByConfig(mc.excludeTable, ref) {
			filtered = append(filtered, ref)
		}
	}
	return filtered
}

// Key helpers
func (mc *MemoryCache) rowKey(table, id string) string {
	return core.RowRef{Table: table, ID: id}.DependencyKey()
}

func (mc *MemoryCache) modKey(table, id string) string {
	return core.RowRef{Table: table, ID: id}.DependencyKey()
}

func (mc *MemoryCache) tableModKey(table string) string {
	return core.RowRef{Kind: core.CacheKindTable, Table: table}.DependencyKey()
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

func (mc *MemoryCache) recordSWRRefresh(ctx context.Context) {
	mc.metrics.SWRRefreshes.Add(1)
	if mc.otelSWRRefreshCounter != nil {
		mc.otelSWRRefreshCounter.Add(ctx, 1)
	}
}

// Metrics returns the cache metrics
func (mc *MemoryCache) Metrics() *CacheMetrics {
	return mc.metrics
}

// Close stops the SWR worker pool and purges the cache.
func (mc *MemoryCache) Close() error {
	if mc.workerPool != nil {
		mc.workerPool.Shutdown()
	}
	mc.cacheMu.Lock()
	defer mc.cacheMu.Unlock()
	mc.cache.Purge()
	mc.mu.Lock()
	mc.rowIndex = make(map[string]map[string]struct{})
	mc.tableIndex = make(map[string]map[string]struct{})
	mc.mu.Unlock()
	return nil
}
