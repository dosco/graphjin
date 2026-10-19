package serv

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"time"

	"github.com/dosco/graphjin/core/v3"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

// Hardcoded constants for cache behavior
const (
	cachePrefix          = "gj:cache"             // Redis key prefix
	swrWorkers           = 10                     // SWR worker pool size
	compressionThreshold = 1024                   // Only compress > 1KB
	rowLevelThreshold    = 500                    // Switch to table-level above this
	maxResponseSize      = 1 << 20                // 1MB max cacheable response
	redisTimeout         = 100 * time.Millisecond // Redis operation timeout
	redisRetryInterval   = 30 * time.Second       // Retry interval when Redis unavailable
)

// Redis key prefixes
const (
	respKeyPrefix  = "resp:"
	rowKeyPrefix   = "row:"
	tableKeyPrefix = "table:"
	modKeyPrefix   = "mod:"
	tableModPrefix = "tablemod:"
)

// CacheEntry represents a cached response with metadata
type CacheEntry struct {
	Data         []byte `json:"d"`
	Compressed   bool   `json:"c,omitempty"`
	OriginalSize int    `json:"o,omitempty"`
	FreshUntil   int64  `json:"f"`
	StaleUntil   int64  `json:"s"`
}

// RedisCache provides Redis-based response caching with row-level invalidation
type RedisCache struct {
	client       *redis.Client
	conf         CachingConfig
	workerPool   *SWRWorkerPool
	metrics      *CacheMetrics
	available    atomic.Bool
	lastCheck    atomic.Int64
	excludeTable map[string]bool

	// OpenTelemetry metric instruments
	otelHitCounter          metric.Int64Counter
	otelMissCounter         metric.Int64Counter
	otelInvalidationCounter metric.Int64Counter
	otelErrorCounter        metric.Int64Counter
	otelSWRRefreshCounter   metric.Int64Counter
	otelBytesCachedGauge    metric.Int64UpDownCounter
	otelBytesSavedGauge     metric.Int64UpDownCounter
}

// NewRedisCache creates a new Redis cache instance
func NewRedisCache(redisURL string, conf CachingConfig) (*RedisCache, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("invalid redis URL: %w", err)
	}

	client := redis.NewClient(opts)

	ctx, cancel := context.WithTimeout(context.Background(), redisTimeout)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis connection failed: %w", err)
	}

	rc := &RedisCache{
		client:       client,
		conf:         conf,
		metrics:      &CacheMetrics{},
		excludeTable: make(map[string]bool),
	}
	rc.available.Store(true)

	// Build exclude table lookup
	for _, t := range conf.ExcludeTables {
		t = strings.TrimSpace(t)
		if t != "" {
			rc.excludeTable[t] = true
		}
	}

	// Initialize OpenTelemetry metrics
	meter := otel.Meter("graphjin.com/cache")

	rc.otelHitCounter, _ = meter.Int64Counter("graphjin.cache.hits",
		metric.WithDescription("Number of cache hits"))
	rc.otelMissCounter, _ = meter.Int64Counter("graphjin.cache.misses",
		metric.WithDescription("Number of cache misses"))
	rc.otelInvalidationCounter, _ = meter.Int64Counter("graphjin.cache.invalidations",
		metric.WithDescription("Number of cache invalidations"))
	rc.otelErrorCounter, _ = meter.Int64Counter("graphjin.cache.errors",
		metric.WithDescription("Number of cache errors"))
	rc.otelSWRRefreshCounter, _ = meter.Int64Counter("graphjin.cache.swr_refreshes",
		metric.WithDescription("Number of SWR background refreshes"))
	rc.otelBytesCachedGauge, _ = meter.Int64UpDownCounter("graphjin.cache.bytes_cached",
		metric.WithDescription("Total bytes stored in cache"))
	rc.otelBytesSavedGauge, _ = meter.Int64UpDownCounter("graphjin.cache.bytes_saved",
		metric.WithDescription("Bytes saved via compression"))

	// Initialize SWR worker pool when fresh TTL is set and shorter than hard TTL.
	if conf.FreshTTL > 0 && conf.TTL > conf.FreshTTL {
		rc.workerPool = NewSWRWorkerPool(swrWorkers, rc)
	}

	return rc, nil
}

// Key building methods
func (c *RedisCache) respKey(hash string) string {
	return cachePrefix + ":" + respKeyPrefix + hash
}

func (c *RedisCache) rowKey(table, id string) string {
	return cachePrefix + ":" + rowKeyPrefix + table + ":" + id
}

func (c *RedisCache) tableKey(table string) string {
	return cachePrefix + ":" + tableKeyPrefix + table
}

func (c *RedisCache) modKey(table, id string) string {
	return cachePrefix + ":" + modKeyPrefix + table + ":" + id
}

func (c *RedisCache) tableModKey(table string) string {
	return cachePrefix + ":" + tableModPrefix + table
}

func (c *RedisCache) depKey(ref core.RowRef) string {
	return cachePrefix + ":" + rowKeyPrefix + ref.DependencyKey()
}

func (c *RedisCache) depModKey(ref core.RowRef) string {
	return cachePrefix + ":" + modKeyPrefix + ref.DependencyKey()
}

// Get retrieves a cached response
// Returns (data, isStale, found)
func (c *RedisCache) Get(ctx context.Context, key string) ([]byte, bool, bool) {
	if !c.isAvailable() {
		c.maybeRetryConnection()
		return nil, false, false
	}

	ctx, cancel := context.WithTimeout(ctx, redisTimeout)
	defer cancel()

	data, err := c.client.Get(ctx, c.respKey(key)).Bytes()
	if err == redis.Nil {
		c.recordMiss(ctx)
		return nil, false, false
	}
	if err != nil {
		c.handleError(err)
		c.recordMiss(ctx)
		return nil, false, false
	}

	var entry CacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		c.recordMiss(ctx)
		return nil, false, false
	}

	now := time.Now().Unix()

	// Expired (past hard TTL)
	if now >= entry.StaleUntil {
		c.recordMiss(ctx)
		return nil, false, false
	}

	// Decompress if needed
	respData := entry.Data
	if entry.Compressed {
		var err error
		respData, err = decompress(entry.Data)
		if err != nil {
			c.recordError(ctx)
			return nil, false, false
		}
	}

	c.recordHit(ctx)

	// Check if stale (past soft TTL but before hard TTL)
	isStale := now >= entry.FreshUntil
	return respData, isStale, true
}

// Set stores a response with row-level indices
func (c *RedisCache) Set(
	ctx context.Context,
	key string,
	data []byte,
	refs []core.RowRef,
	queryStartTime time.Time,
) error {
	return c.SetWithOptions(ctx, key, data, refs, queryStartTime, core.CacheEntryOptions{})
}

// SetWithOptions stores a response with dependency indices and per-entry TTL caps.
func (c *RedisCache) SetWithOptions(
	ctx context.Context,
	key string,
	data []byte,
	refs []core.RowRef,
	queryStartTime time.Time,
	opts core.CacheEntryOptions,
) error {
	if !c.isAvailable() {
		return nil
	}

	// Filter out excluded tables
	filteredRefs := c.filterExcludedTables(refs)

	// Check for race condition - verify no rows were modified during query
	if len(filteredRefs) > 0 {
		safe, err := c.checkModificationSafety(ctx, filteredRefs, queryStartTime)
		if err != nil || !safe {
			return err
		}
	}

	now := time.Now()
	ttl, freshTTL, ok := cacheEntryTTLs(c.conf, opts)
	if !ok {
		return nil
	}

	// Compress if beneficial
	compressed := false
	originalSize := len(data)

	if len(data) > compressionThreshold {
		compData, err := compress(data)
		if err == nil && len(compData) < len(data) {
			saved := int64(len(data) - len(compData))
			c.metrics.BytesSaved.Add(saved)
			if c.otelBytesSavedGauge != nil {
				c.otelBytesSavedGauge.Add(ctx, saved)
			}
			data = compData
			compressed = true
		}
	}

	entry := CacheEntry{
		Data:         data,
		Compressed:   compressed,
		OriginalSize: originalSize,
		FreshUntil:   now.Add(freshTTL).Unix(),
		StaleUntil:   now.Add(ttl).Unix(),
	}

	entryJSON, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, redisTimeout)
	defer cancel()

	pipe := c.client.Pipeline()

	// Store response
	pipe.Set(ctx, c.respKey(key), entryJSON, ttl)

	for _, ref := range cacheIndexRefs(filteredRefs, len(filteredRefs) <= rowLevelThreshold) {
		depKey := c.depKey(ref)
		pipe.SAdd(ctx, depKey, key)
		pipe.Expire(ctx, depKey, ttl)
	}

	_, err = pipe.Exec(ctx)
	if err != nil {
		c.handleError(err)
		c.recordError(ctx)
		return err
	}

	cached := int64(len(entryJSON))
	c.metrics.BytesCached.Add(cached)
	if c.otelBytesCachedGauge != nil {
		c.otelBytesCachedGauge.Add(ctx, cached)
	}
	return nil
}

// InvalidateRows invalidates cache entries for specific rows (called after mutations)
func (c *RedisCache) InvalidateRows(ctx context.Context, refs []core.RowRef) error {
	if !c.isAvailable() || len(refs) == 0 {
		return nil
	}

	// Filter out excluded tables
	filteredRefs := c.filterExcludedTables(refs)
	if len(filteredRefs) == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, redisTimeout*2) // Allow more time for invalidation
	defer cancel()

	now := time.Now().UnixMilli()
	ttl := time.Duration(c.conf.TTL) * time.Second
	invalidateRefs := cacheIndexRefs(filteredRefs, true)

	// Record modification timestamps first
	pipe := c.client.Pipeline()
	for _, ref := range invalidateRefs {
		pipe.Set(ctx, c.depModKey(ref), now, ttl)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		c.handleError(err)
		return err
	}

	hashesToDelete := make(map[string]bool)
	for _, ref := range invalidateRefs {
		hashes, err := c.client.SMembers(ctx, c.depKey(ref)).Result()
		if err != nil && err != redis.Nil {
			continue
		}
		for _, hash := range hashes {
			hashesToDelete[hash] = true
		}
	}

	if len(hashesToDelete) == 0 {
		return nil
	}

	// Delete response caches and row indices
	pipe = c.client.Pipeline()
	for hash := range hashesToDelete {
		pipe.Del(ctx, c.respKey(hash))
	}
	for _, ref := range invalidateRefs {
		pipe.Del(ctx, c.depKey(ref))
	}

	_, err := pipe.Exec(ctx)
	if err != nil {
		c.handleError(err)
		c.recordError(ctx)
		return err
	}

	c.recordInvalidation(ctx, int64(len(hashesToDelete)))
	return nil
}

// InvalidateTables invalidates cache entries associated with whole tables.
func (c *RedisCache) InvalidateTables(ctx context.Context, tables []string) error {
	if !c.isAvailable() || len(tables) == 0 {
		return nil
	}
	refs := make([]core.RowRef, 0, len(tables))
	for _, table := range tables {
		table = strings.TrimSpace(table)
		ref := core.RowRef{Kind: core.CacheKindTable, Table: table}
		if table == "" || refExcludedByConfig(c.excludeTable, ref) {
			continue
		}
		refs = append(refs, ref)
	}
	return c.InvalidateRows(ctx, refs)
}

// checkModificationSafety verifies no rows were modified during query execution
func (c *RedisCache) checkModificationSafety(
	ctx context.Context,
	refs []core.RowRef,
	queryStartTime time.Time,
) (bool, error) {
	if len(refs) == 0 {
		return true, nil
	}

	ctx, cancel := context.WithTimeout(ctx, redisTimeout)
	defer cancel()

	pipe := c.client.Pipeline()
	indexRefs := cacheIndexRefs(refs, true)
	cmds := make([]*redis.StringCmd, 0, len(indexRefs))

	for _, ref := range indexRefs {
		cmds = append(cmds, pipe.Get(ctx, c.depModKey(ref)))
	}

	_, _ = pipe.Exec(ctx)

	queryStartMs := queryStartTime.UnixMilli()
	for _, cmd := range cmds {
		if ts, err := cmd.Int64(); err == nil {
			if ts > queryStartMs {
				// Row was modified during query - unsafe to cache
				return false, nil
			}
		}
	}

	return true, nil
}

// filterExcludedTables removes refs excluded by table or source-aware keys.
func (c *RedisCache) filterExcludedTables(refs []core.RowRef) []core.RowRef {
	if len(c.excludeTable) == 0 {
		return refs
	}

	filtered := make([]core.RowRef, 0, len(refs))
	for _, ref := range refs {
		if !refExcludedByConfig(c.excludeTable, ref) {
			filtered = append(filtered, ref)
		}
	}
	return filtered
}

// Availability management
func (c *RedisCache) isAvailable() bool {
	return c.available.Load()
}

func (c *RedisCache) handleError(err error) {
	if err != nil {
		c.available.Store(false)
		c.lastCheck.Store(time.Now().Unix())
	}
}

func (c *RedisCache) maybeRetryConnection() {
	if c.isAvailable() {
		return
	}

	lastCheck := c.lastCheck.Load()
	if time.Now().Unix()-lastCheck < int64(redisRetryInterval.Seconds()) {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), redisTimeout)
	defer cancel()

	if err := c.client.Ping(ctx).Err(); err == nil {
		c.available.Store(true)
	}
	c.lastCheck.Store(time.Now().Unix())
}

// Metric recording helpers (record both internal metrics and OTel metrics)
func (c *RedisCache) recordHit(ctx context.Context) {
	c.metrics.Hits.Add(1)
	if c.otelHitCounter != nil {
		c.otelHitCounter.Add(ctx, 1)
	}
}

func (c *RedisCache) recordMiss(ctx context.Context) {
	c.metrics.Misses.Add(1)
	if c.otelMissCounter != nil {
		c.otelMissCounter.Add(ctx, 1)
	}
}

func (c *RedisCache) recordError(ctx context.Context) {
	c.metrics.Errors.Add(1)
	if c.otelErrorCounter != nil {
		c.otelErrorCounter.Add(ctx, 1)
	}
}

func (c *RedisCache) recordInvalidation(ctx context.Context, count int64) {
	c.metrics.Invalidations.Add(count)
	if c.otelInvalidationCounter != nil {
		c.otelInvalidationCounter.Add(ctx, count)
	}
}

func (c *RedisCache) recordSWRRefresh(ctx context.Context) {
	c.metrics.SWRRefreshes.Add(1)
	if c.otelSWRRefreshCounter != nil {
		c.otelSWRRefreshCounter.Add(ctx, 1)
	}
}

// Metrics returns the cache metrics
func (c *RedisCache) Metrics() *CacheMetrics {
	return c.metrics
}

// Close closes the Redis connection and worker pool
func (c *RedisCache) Close() error {
	if c.workerPool != nil {
		c.workerPool.Shutdown()
	}
	return c.client.Close()
}

// Compression helpers using gzip
func compress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func decompress(data []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Close() //nolint:errcheck
	return io.ReadAll(r)
}

// SubmitRefresh enqueues a stale-while-revalidate refresh on the worker pool.
// Returns false if SWR is disabled, the pool is full, or the cache is unavailable.
func (c *RedisCache) SubmitRefresh(key string, fn core.RefreshFn) bool {
	if c.workerPool == nil || !c.isAvailable() {
		return false
	}
	return c.workerPool.TrySubmit(key, fn)
}

// SubmitRefreshWithOptions enqueues an option-aware stale-while-revalidate refresh.
func (c *RedisCache) SubmitRefreshWithOptions(key string, fn core.RefreshFnWithOptions) bool {
	if c.workerPool == nil || !c.isAvailable() {
		return false
	}
	return c.workerPool.TrySubmitWithOptions(key, fn)
}

// CacheMetrics tracks cache performance
type CacheMetrics struct {
	Hits          atomic.Int64
	Misses        atomic.Int64
	Invalidations atomic.Int64
	BytesCached   atomic.Int64
	BytesSaved    atomic.Int64 // Compression savings
	Errors        atomic.Int64
	SWRRefreshes  atomic.Int64
}

// Snapshot returns a point-in-time snapshot of metrics
func (m *CacheMetrics) Snapshot() map[string]int64 {
	return map[string]int64{
		"hits":          m.Hits.Load(),
		"misses":        m.Misses.Load(),
		"invalidations": m.Invalidations.Load(),
		"bytes_cached":  m.BytesCached.Load(),
		"bytes_saved":   m.BytesSaved.Load(),
		"errors":        m.Errors.Load(),
		"swr_refreshes": m.SWRRefreshes.Load(),
	}
}

// HitRate returns the cache hit rate (0.0 to 1.0)
func (m *CacheMetrics) HitRate() float64 {
	hits := m.Hits.Load()
	total := hits + m.Misses.Load()
	if total == 0 {
		return 0
	}
	return float64(hits) / float64(total)
}
