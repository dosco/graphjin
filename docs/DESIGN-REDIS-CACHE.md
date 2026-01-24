# Redis Response Cache Design

## Overview

This document describes the design for optional Redis-based response caching in GraphJin. The cache stores query responses and automatically invalidates them when mutations modify related tables.

## Motivation

- **Shared cache across instances**: Multiple GraphJin instances behind a load balancer can share cached responses
- **Reduced database load**: Expensive queries can be served from cache
- **Automatic invalidation**: Since GraphJin controls mutations, we can invalidate cache entries when data changes

## Architecture

```
┌─────────────┐      ┌─────────────┐      ┌─────────────┐
│  GraphJin   │      │  GraphJin   │      │  GraphJin   │
│ Instance 1  │      │ Instance 2  │      │ Instance 3  │
└──────┬──────┘      └──────┬──────┘      └──────┬──────┘
       │                    │                    │
       └────────────────────┼────────────────────┘
                            │
                     ┌──────▼──────┐
                     │    Redis    │
                     │    Cache    │
                     └─────────────┘
```

## Redis Data Structures

### 1. Response Cache

Stores the actual cached responses (with injected PK fields already stripped).

```
Key:    gj:cache:resp:{query_hash}
Value:  JSON response bytes (cleaned)
TTL:    Configurable (default: 1 hour)
```

### 2. Row Index (Fine-Grained Reverse Lookup)

Maps (table, row_id) pairs to query hashes. Used for precise invalidation.

```
Key:    gj:cache:row:{table_name}:{row_id}
Type:   Redis SET
Value:  Set of query hashes that include this specific row
TTL:    Same as response TTL
```

### 3. Table Index (Fallback for Large Results)

For queries returning many rows, fall back to table-level tracking.

```
Key:    gj:cache:table:{table_name}
Type:   Redis SET
Value:  Set of query hashes (for large result sets)
TTL:    Same as response TTL
```

### Example

```
Query: { users { posts { comments } } }
Hash:  abc123
Response: user #5 → posts #10, #11 → comments #20, #21

Redis state after caching:
  gj:cache:resp:abc123 = {"data":{"users":[...]}}  (cleaned JSON)

  Row-level indices:
  gj:cache:row:users:5      = {abc123}
  gj:cache:row:posts:10     = {abc123}
  gj:cache:row:posts:11     = {abc123}
  gj:cache:row:comments:20  = {abc123}
  gj:cache:row:comments:21  = {abc123}
```

## Primary Key Injection Strategy

### The Problem

Users don't always request the primary key in their queries:

```graphql
query {
  orders {
    total           # No ID requested
    items {
      quantity      # No ID requested
      product {
        name        # No ID requested
        price
      }
    }
  }
}
```

Without PKs in the response, we can't build row-level cache indices.

### The Solution: Inject PKs with Reserved Alias

During query compilation, inject the PK field at every level using a reserved alias:

```graphql
# User's query (after internal transformation)
query {
  orders {
    __gj_id: id     # Injected
    total
    items {
      __gj_id: id   # Injected
      quantity
      product {
        __gj_id: id # Injected
        name
        price
      }
    }
  }
}
```

### Reserved Field Name

Use `__gj_id` as the alias:
- Double underscore prefix follows GraphQL convention for internal fields
- Unlikely to conflict with user field names
- Easy to identify and strip from response

### Implementation Flow

```
1. COMPILE QUERY
   - Walk the QCode tree
   - At each table node, inject: __gj_id: {pk_column}
   - GraphJin already knows PK columns from schema introspection

2. EXECUTE QUERY
   - Run modified query against database
   - Response now contains __gj_id at every level

3. EXTRACT ROW REFERENCES (before caching)
   - Walk JSON response tree
   - At each object, extract __gj_id value
   - Map to table name from QCode structure
   - Build list: [(orders, 1), (items, 10), (items, 11), (products, 100), ...]

4. STRIP INJECTED FIELDS
   - Remove all __gj_id fields from response JSON
   - This is the "cleaned" response to cache and return to user

5. CACHE
   - Store cleaned response
   - Create row-level indices for each (table, id) pair
```

### Code Location

PK injection should happen in QCode compilation (`core/internal/qcode/`):

```go
// During field selection building
func (co *Compiler) addCacheTrackingField(sel *Select) {
    if co.cacheEnabled && sel.Table.PrimaryKey != "" {
        sel.Fields = append(sel.Fields, Field{
            Name:  sel.Table.PrimaryKey,
            Alias: "__gj_id",
        })
    }
}
```

### Response Processing

```go
type RowRef struct {
    Table string
    ID    interface{}  // Could be int, string, uuid
}

// Extract refs and clean response in one pass
func processResponseForCache(qc *QCode, response []byte) (cleaned []byte, refs []RowRef) {
    // Walk JSON tree guided by QCode structure
    // Extract __gj_id values with table context
    // Remove __gj_id fields
    // Return cleaned JSON and row references
}
```

### Mutation Handling

For mutations, we need the affected row IDs. Two approaches:

**1. RETURNING clause (already used)**
```sql
INSERT INTO orders (...) RETURNING id
UPDATE orders SET ... WHERE id = 5 RETURNING id
DELETE FROM orders WHERE ... RETURNING id
```

**2. For bulk mutations with WHERE clauses**
```sql
UPDATE orders SET status = 'shipped' WHERE created_at < '2024-01-01' RETURNING id
-- Returns all affected IDs
```

GraphJin already uses RETURNING - we just need to extract the IDs for cache invalidation.

## Cache Key Generation

The cache key should include:

1. **Query hash**: SHA256 of normalized query string
2. **Variables hash**: SHA256 of JSON-serialized variables
3. **User role**: Different roles may see different data
4. **Tenant ID**: For multi-tenant deployments (if applicable)

```
cache_key = sha256(query + variables + role + tenant_id)
```

### Which Queries Are Cacheable?

Anonymous queries (no operation name, no APQ) are typically dynamic/ad-hoc and not good caching candidates:
- Often exploratory or debugging queries
- No consistent naming → poor cache hit rate
- Would require query normalization for any benefit

**Strategy: Only Cache Named Queries**

```go
func (c *RedisCache) ShouldCache(req *Request) bool {
    // APQ: Client provides consistent hash
    if req.APQKey != "" {
        return true
    }

    // Named operation: Consistent across formatting
    if req.OpName != "" {
        return true
    }

    // Anonymous query: Dynamic, skip caching
    return false
}

func (c *RedisCache) BuildKey(ctx context.Context, req *Request, vars json.RawMessage) string {
    h := sha256.New()

    if req.APQKey != "" {
        h.Write([]byte(req.APQKey))
    } else {
        // Must have OpName (checked by ShouldCache)
        h.Write([]byte(req.OpName))
    }

    h.Write(vars)
    // ... add user context
}
```

### Query Type Summary

| Query Type | Cached | Cache Key |
|------------|--------|-----------|
| APQ | Yes | APQ hash (client-provided) |
| Named operation | Yes | Operation name |
| Anonymous | No | - |

This avoids the need for query normalization entirely.

## Flows

### Query Execution (Cache Read)

```
1. Receive GraphQL query
2. Compute cache key from query, variables, role
3. Check Redis: GET gj:cache:resp:{key}
4. If HIT:
   - Return cached response (already cleaned)
   - Skip database query
5. If MISS:
   - Compile query WITH __gj_id injection at every level
   - Execute query against database
   - Process response:
     a. Extract all (table, row_id) pairs from __gj_id fields
     b. Strip __gj_id fields from response
   - Cache cleaned response with row-level indices
   - Return cleaned response to user
```

### Cache Write (Row-Level)

```
1. Query executed successfully
2. Compute cache key
3. Process response → get row refs: [(orders,1), (items,10), (items,11), (products,100)]
4. Decide: row-level or table-level based on count
   - If refs.count <= threshold (e.g., 500): use row-level
   - If refs.count > threshold: fall back to table-level

5. Redis pipeline (row-level):
   - SETEX gj:cache:resp:{key} {ttl} {cleaned_response}
   - SADD gj:cache:row:orders:1 {key}
   - SADD gj:cache:row:items:10 {key}
   - SADD gj:cache:row:items:11 {key}
   - SADD gj:cache:row:products:100 {key}
   - EXPIRE on each key
```

### Mutation Execution (Cache Invalidation)

```
1. Receive GraphQL mutation
2. Compile mutation, identify tables being modified
   - Includes nested inserts/updates/deletes
   - Example: insert_order with nested items → [orders, order_items]
3. Execute mutation against database
4. Extract affected row IDs from RETURNING clause
   - Example: orders.id=5, order_items.id=20,21
5. If SUCCESS:
   - For each (table, row_id) pair:
     - SMEMBERS gj:cache:row:{table}:{id} → [hash1, hash2, ...]
     - DEL gj:cache:resp:hash1 gj:cache:resp:hash2 ...
     - DEL gj:cache:row:{table}:{id}
```

### Invalidation with Redis Pipeline

```go
type RowRef struct {
    Table string
    ID    string  // Stringified for Redis key
}

func invalidateRowsCache(ctx context.Context, rdb *redis.Client, refs []RowRef) error {
    pipe := rdb.Pipeline()
    hashesToDelete := make(map[string]bool)

    // Collect all query hashes that need invalidation
    for _, ref := range refs {
        rowKey := fmt.Sprintf("gj:cache:row:%s:%s", ref.Table, ref.ID)

        hashes, err := rdb.SMembers(ctx, rowKey).Result()
        if err != nil {
            continue
        }

        for _, hash := range hashes {
            hashesToDelete[hash] = true
        }

        // Delete the row index
        pipe.Del(ctx, rowKey)
    }

    // Delete all affected response cache entries
    for hash := range hashesToDelete {
        pipe.Del(ctx, fmt.Sprintf("gj:cache:resp:%s", hash))
    }

    _, err := pipe.Exec(ctx)
    return err
}
```

## Configuration

```yaml
# config.yaml
cache:
  # Enable Redis response caching
  enable: true

  # Redis connection
  redis_url: "redis://localhost:6379/0"

  # Default TTL for cached responses (seconds)
  ttl: 3600

  # Key prefix (useful for shared Redis instances)
  prefix: "gj:cache"

  # Row-level vs table-level threshold
  # Queries returning more rows than this use table-level invalidation
  row_level_threshold: 500

  # Tables to exclude from caching (e.g., frequently changing data)
  exclude_tables:
    - audit_logs
    - sessions

  # Queries to exclude from caching (by operation name)
  exclude_operations:
    - GetCurrentUser
    - GetNotifications
```

### Per-Query Cache Control

Existing `@cacheControl` directive can be extended:

```graphql
# Cache this query for 5 minutes
query GetProducts @cacheControl(maxAge: 300) {
  products { id name price }
}

# Disable caching for this query
query GetUserBalance @cacheControl(maxAge: 0) {
  user { balance }
}
```

## Implementation Components

### 1. Redis Client (serv/cache_redis.go)

```go
type RedisCache struct {
    client *redis.Client
    prefix string
    ttl    time.Duration
}

func NewRedisCache(cfg CacheConfig) (*RedisCache, error)
func (c *RedisCache) Get(ctx context.Context, key string) ([]byte, bool)
func (c *RedisCache) Set(ctx context.Context, key string, val []byte, tables []string) error
func (c *RedisCache) InvalidateTables(ctx context.Context, tables []string) error
```

### 2. Cache Key Builder (core/cache_key.go)

```go
type CacheKeyBuilder struct {
    query     string
    variables json.RawMessage
    role      string
    tenantID  string
}

func (b *CacheKeyBuilder) Build() string
```

### 3. QCode Table Extraction

The QCode compiler already tracks tables. Expose this for cache:

```go
// In QCode or compiled query result
func (qc *QCode) TablesRead() []string   // For query caching
func (qc *QCode) TablesWritten() []string // For mutation invalidation
```

### 4. Integration Points

**Query path** (core/api.go):
```go
func (gj *graphjin) query(ctx context.Context, req Request) (*Result, error) {
    // Check cache first
    if cached := gj.redisCache.Get(ctx, cacheKey); cached != nil {
        return cached, nil
    }

    // Execute query
    result, err := gj.execQuery(ctx, req)

    // Cache result
    gj.redisCache.Set(ctx, cacheKey, result, qcode.TablesRead())

    return result, err
}
```

**Mutation path** (core/api.go):
```go
func (gj *graphjin) mutate(ctx context.Context, req Request) (*Result, error) {
    // Execute mutation
    result, err := gj.execMutation(ctx, req)

    // Invalidate cache for affected tables
    gj.redisCache.InvalidateTables(ctx, qcode.TablesWritten())

    return result, err
}
```

## Edge Cases

### 1. TTL Expiration vs Index Cleanup

When a response TTL expires, its hash remains in table indices (orphaned references).

**Solution**: Lazy cleanup is acceptable. When invalidating:
- DEL on non-existent keys is a no-op
- Table indices themselves have TTL, so they eventually clean up

### 2. Cache Stampede

When a popular cached query expires, many requests hit the database simultaneously.

**Solutions**:
- Probabilistic early expiration (jitter)
- Single-flight pattern (only one request fetches, others wait)
- Background refresh before expiration

### 3. Large Responses

Some queries return very large responses that shouldn't be cached.

**Solution**:
- Configure max response size for caching
- Skip caching if response exceeds threshold

```yaml
cache:
  max_response_size: 1048576  # 1MB
```

### 4. Transactions and Partial Failures

If mutation succeeds but cache invalidation fails:

**Solution**:
- Log the error, don't fail the mutation
- Cache will eventually expire via TTL
- Consider async invalidation for reliability

### 5. Role-Based Data

Same query returns different data for different roles.

**Solution**: Include role in cache key (already in design).

### 6. Race Condition: Mutation During Query Execution

**The Problem:**

```
T0: Query starts (reads user #5 with name="Alice")
T1: Mutation updates user #5 to name="Bob", invalidates cache
T2: Query finishes, caches result with name="Alice" ← STALE DATA CACHED
T3: Stale data served until TTL expires
```

**The Solution: Modification Timestamps**

Track when each row was last modified. Before caching a query result, verify no touched rows were modified after the query started.

**Redis Keys:**

```
gj:cache:modified:{table}:{id} = timestamp (Unix ms)
TTL: Same as cache TTL (or slightly longer)
```

**Flow:**

```
QUERY EXECUTION:
1. query_start_time = now()
2. Execute query against database
3. Extract row refs: [(users, 5), (posts, 10), ...]
4. For each ref, check: GET gj:cache:modified:{table}:{id}
5. If ANY ref has modified_time > query_start_time:
   - Skip caching (data may be stale)
   - Return result to user (fresh from DB anyway)
6. Otherwise: cache the result

MUTATION EXECUTION:
1. Execute mutation
2. Extract affected rows from RETURNING
3. For each (table, id):
   - SET gj:cache:modified:{table}:{id} = now()
   - Invalidate cached queries (existing logic)
```

**Implementation:**

```go
func (c *RedisCache) SafeSet(
    ctx context.Context,
    key string,
    value []byte,
    refs []RowRef,
    queryStartTime time.Time,
) error {
    // Check if any rows were modified during query execution
    pipe := c.client.Pipeline()
    cmds := make([]*redis.StringCmd, len(refs))

    for i, ref := range refs {
        modKey := fmt.Sprintf("gj:cache:modified:%s:%s", ref.Table, ref.ID)
        cmds[i] = pipe.Get(ctx, modKey)
    }

    pipe.Exec(ctx)

    // Check timestamps
    for _, cmd := range cmds {
        if ts, err := cmd.Int64(); err == nil {
            modTime := time.UnixMilli(ts)
            if modTime.After(queryStartTime) {
                // Row was modified during query - don't cache
                return nil
            }
        }
    }

    // Safe to cache
    return c.Set(ctx, key, value, refs)
}

func (c *RedisCache) RecordModification(ctx context.Context, refs []RowRef) error {
    pipe := c.client.Pipeline()
    now := time.Now().UnixMilli()

    for _, ref := range refs {
        modKey := fmt.Sprintf("gj:cache:modified:%s:%s", ref.Table, ref.ID)
        pipe.Set(ctx, modKey, now, c.ttl)
    }

    _, err := pipe.Exec(ctx)
    return err
}
```

**Trade-offs:**

- Adds Redis GETs before caching (one per row in result)
- Can batch with pipeline for efficiency
- Slightly increases mutation overhead (recording timestamps)
- Guarantees cache correctness

### 7. Pagination Handling

#### Why Offset Pagination Can't Be Cached

With offset pagination (`LIMIT/OFFSET`), any insert or delete shifts ALL subsequent pages:

```
Page 1 (offset=0):  [row1, row2, row3]
Page 2 (offset=10): [row4, row5, row6]

DELETE row2:
- All pages shift - Page 2 now has different rows
- Row-level invalidation misses this cascade
```

#### Strategy: Don't Cache Offset Queries

Offset-paginated queries skip caching entirely. Cursor queries get full caching.

```go
func (c *RedisCache) ShouldCache(qc *QCode) bool {
    // Skip caching for offset-based pagination
    if qc.HasOffsetPagination() {
        return false
    }
    return true
}
```

#### Cursor Pagination Works

Cursor pagination (`WHERE id > $cursor`) is cache-safe:

```
Page 1: cursor=0,   results=[id:1-10]
Page 2: cursor=10,  results=[id:11-20]

DELETE id:5:
- Page 1 invalidated (contains row 5)
- Page 2 still valid (cursor boundary unchanged)
```

Row-level invalidation works correctly for cursor pagination.

#### Summary

| Pagination Type | Cached | Why |
|-----------------|--------|-----|
| Offset (LIMIT/OFFSET) | No | Pages shift on any insert/delete |
| Cursor (WHERE id > X) | Yes | Pages are independent |
| None | Yes | Standard row-level invalidation |

### 8. Security: User Context in Cache Keys

#### The Problem

User ID and role are often passed via sidechannels (context, headers, JWT) rather than GraphQL variables:

```go
// User ID is in context, not in variables
ctx = context.WithValue(ctx, "user_id", 123)
ctx = context.WithValue(ctx, "role", "admin")

// Query and variables are the same for all users
gj.GraphQL(ctx, query, variables)
```

If cache key only includes `query + variables`, different users get the same cache entry:

```
User A (admin):  query GetOrders → sees all orders → cached as hash123
User B (viewer): query GetOrders → gets hash123 → SEES ADMIN DATA!
```

#### Solution: Include User Context in Cache Key

Always include user-identifying information in the cache key:

```go
func BuildCacheKey(ctx context.Context, query string, vars json.RawMessage) string {
    h := sha256.New()
    h.Write([]byte(query))
    h.Write(vars)

    // CRITICAL: Include user context from sidechannels
    if userID := ctx.Value("user_id"); userID != nil {
        fmt.Fprintf(h, ":user=%v", userID)
    }
    if role := ctx.Value("role"); role != nil {
        fmt.Fprintf(h, ":role=%v", role)
    }
    if tenantID := ctx.Value("tenant_id"); tenantID != nil {
        fmt.Fprintf(h, ":tenant=%v", tenantID)
    }

    return hex.EncodeToString(h.Sum(nil))
}
```

#### What to Include

| Context Value | Include? | Why |
|---------------|----------|-----|
| User ID | Yes | Different users may have different permissions |
| Role | Yes | Role-based access control affects results |
| Tenant ID | Yes | Multi-tenant isolation |
| Session ID | No | Too unique, kills cache hit rate |
| Request ID | No | Unique per request |

#### Configuration

```yaml
cache:
  key_context:
    - user_id      # Context key to include
    - role
    - tenant_id
```

#### Public vs Authenticated Queries

Some queries return the same data for everyone. Use directive to mark as public:

```graphql
# Shared cache entry - no user context needed
query GetPublicProducts @cacheControl(public: true) {
  products(where: { is_public: true }) { id name price }
}

# Per-user cache entry - must include user in key
query GetMyOrders {
  orders { id total }
}
```

#### Summary

| Scenario | Cache Key Includes | Result |
|----------|-------------------|--------|
| No user context | query + vars only | Data leakage risk |
| User ID in key | query + vars + user_id | Per-user caching (safe) |
| Public query | query + vars (no user) | Shared cache (safe) |

## Performance Considerations

### Redis Operations per Request

**Cache hit (query)**: 1 GET
**Cache miss (query)**: 1 GET + 1 SETEX + N SADD + N EXPIRE (pipelined)
**Mutation**: 1 SMEMBERS per table + DEL operations (pipelined)

### Memory Usage

Estimate per cached query:
- Response: Variable (avg 1-10KB)
- Table index entry: ~50 bytes per hash

For 10,000 cached queries across 50 tables:
- Responses: ~50-100MB
- Indices: ~2.5MB

## Graceful Degradation

### Redis Unavailable → Fallback to In-Memory LRU

When Redis is unavailable (connection error, timeout), fall back to the local LRU cache:

```
┌─────────────────────────────────────────────────────┐
│                   Cache Layer                        │
│                                                      │
│   ┌─────────┐    fail    ┌─────────────────────┐   │
│   │  Redis  │ ─────────► │  Local LRU Cache    │   │
│   │  (L1)   │            │  (Fallback)         │   │
│   └─────────┘            └─────────────────────┘   │
│        │                          │                 │
│        │ success                  │                 │
│        ▼                          ▼                 │
│   Row-level               Table-level               │
│   invalidation            invalidation only         │
└─────────────────────────────────────────────────────┘
```

**Behavior:**

1. On cache read/write, attempt Redis operation with timeout (e.g., 100ms)
2. If Redis fails:
   - Log warning (with rate limiting to avoid log spam)
   - Fall back to local LRU cache
   - Use table-level invalidation (row-level too complex for in-memory)
3. Periodically retry Redis connection (e.g., every 30s)
4. When Redis recovers, resume using it as primary

**Implementation:**

```go
type CacheLayer struct {
    redis       *RedisCache
    localLRU    *lru.TwoQueueCache[string, []byte]
    redisOK     atomic.Bool
    lastCheck   atomic.Int64
}

func (c *CacheLayer) Get(ctx context.Context, key string) ([]byte, bool) {
    if c.redisOK.Load() {
        val, ok, err := c.redis.Get(ctx, key)
        if err != nil {
            c.handleRedisFailure(err)
            return c.localLRU.Get(key)
        }
        return val, ok
    }

    // Redis down, use local cache
    c.maybeRetryRedis()
    return c.localLRU.Get(key)
}
```

**Configuration:**

```yaml
cache:
  redis_timeout: 100ms        # Timeout for Redis operations
  redis_retry_interval: 30s   # How often to retry when Redis is down
  fallback_to_lru: true       # Enable LRU fallback (default: true)
```

## Stale-While-Revalidate (SWR)

Optional optimization for better UX on high-traffic queries. Serve stale data instantly while refreshing in background.

### How It Works

```
                    Time
──────────────────────────────────────────────────────►
     │◄── fresh ──►│◄── stale ──►│◄── expired ──►
     │             │              │
   cache          soft           hard
   write          TTL            TTL

Fresh period:   Return cached data
Stale period:   Return cached data + trigger background refresh
Expired:        Cache miss, must wait for fresh data
```

### Cache Entry Structure

```go
type CacheEntry struct {
    Data       []byte
    CachedAt   time.Time
    FreshUntil time.Time  // soft TTL
    StaleUntil time.Time  // hard TTL
}
```

### Configuration

```yaml
cache:
  ttl: 3600              # Hard TTL (1 hour)
  fresh_ttl: 300         # Soft TTL (5 minutes) - serve without revalidation
  swr_enabled: true      # Enable stale-while-revalidate
  swr_workers: 10        # Worker pool size for background revalidation
```

### Flow

```
1. Cache HIT:
   - If now < fresh_until: return data (no revalidation)
   - If now < stale_until: return data + submit refresh to worker pool
   - If now >= stale_until: cache MISS

2. Async Refresh (worker pool):
   - Submit to bounded worker pool (prevents goroutine explosion)
   - Single-flight prevents duplicate refreshes for same key
   - If pool is full, skip refresh (serve stale, try next request)
```

### Worker Pool Implementation

Using a bounded worker pool prevents goroutine explosion under high load:

```go
type RefreshJob struct {
    Key       string
    RefreshFn func() ([]byte, []RowRef, error)
}

type SWRWorkerPool struct {
    jobs         chan RefreshJob
    singleFlight singleflight.Group
    cache        *RedisCache
    wg           sync.WaitGroup
}

func NewSWRWorkerPool(size int, cache *RedisCache) *SWRWorkerPool {
    pool := &SWRWorkerPool{
        jobs:  make(chan RefreshJob, size*2),  // Buffered channel
        cache: cache,
    }

    // Start fixed number of workers
    for i := 0; i < size; i++ {
        pool.wg.Add(1)
        go pool.worker()
    }

    return pool
}

func (p *SWRWorkerPool) worker() {
    defer p.wg.Done()
    for job := range p.jobs {
        // Single-flight: only one refresh per key at a time
        p.singleFlight.Do(job.Key, func() (interface{}, error) {
            data, refs, err := job.RefreshFn()
            if err == nil {
                p.cache.Set(context.Background(), job.Key, data, refs)
            }
            return nil, err
        })
    }
}

// TrySubmit attempts to submit a job, returns false if pool is busy
func (p *SWRWorkerPool) TrySubmit(job RefreshJob) bool {
    select {
    case p.jobs <- job:
        return true
    default:
        // Pool is full, skip this refresh
        return false
    }
}

func (p *SWRWorkerPool) Shutdown() {
    close(p.jobs)
    p.wg.Wait()
}
```

### GetSWR with Worker Pool

```go
func (c *RedisCache) GetSWR(
    ctx context.Context,
    key string,
    refreshFn func() ([]byte, []RowRef, error),
) ([]byte, error) {
    entry, err := c.getEntry(ctx, key)
    if err != nil {
        // Cache miss - execute synchronously
        data, refs, err := refreshFn()
        if err != nil {
            return nil, err
        }
        c.Set(ctx, key, data, refs)
        return data, nil
    }

    now := time.Now()

    if now.Before(entry.FreshUntil) {
        // Fresh - return immediately
        return entry.Data, nil
    }

    if now.Before(entry.StaleUntil) {
        // Stale - submit to worker pool, return cached data
        c.workerPool.TrySubmit(RefreshJob{
            Key:       key,
            RefreshFn: refreshFn,
        })
        return entry.Data, nil
    }

    // Expired - must refresh synchronously
    data, refs, err := refreshFn()
    if err != nil {
        return nil, err
    }
    c.Set(ctx, key, data, refs)
    return data, nil
}
```

### Benefits

- Instant responses for stale data (better p99 latency)
- **Bounded resource usage** - fixed worker pool, no goroutine explosion
- Single-flight prevents duplicate refreshes for same key
- Graceful degradation when pool is busy (serve stale, retry later)
- Clean shutdown support

### When to Use

- High-traffic queries where latency matters
- Data that can tolerate brief staleness (seconds to minutes)
- NOT for real-time critical data (use shorter TTL instead)

## Response Compression

Large responses waste Redis memory. Compress before caching to reduce storage and network overhead.

### Strategy: Store Compressed, Serve Smart

```
┌─────────────────────────────────────────────────────────────┐
│                    Compression Flow                          │
│                                                              │
│  Response (10KB) ──► gzip ──► Compressed (2KB) ──► Redis    │
│                                                              │
│  On cache hit:                                               │
│  ┌─────────────────────────────────────────────────────┐    │
│  │ Client accepts gzip?                                 │    │
│  │   YES → Serve compressed directly (fastest)          │    │
│  │   NO  → Decompress, then serve                       │    │
│  └─────────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────────┘
```

### Size Threshold

Compression has overhead. For small responses, the compressed size may be larger than original.

```yaml
cache:
  compression:
    enabled: true
    algorithm: gzip           # gzip (default), snappy, zstd
    threshold: 1024           # Only compress responses > 1KB
    min_ratio: 0.9            # Skip if doesn't save at least 10%
```

**Threshold guidelines:**
- < 512 bytes: Almost never worth compressing
- 512-1KB: Marginal benefit, skip to avoid CPU overhead
- > 1KB: Good compression ratio, worth the CPU cost

### Cache Entry Metadata

Store compression info alongside the data:

```go
type CacheEntry struct {
    Data         []byte
    Compressed   bool
    Algorithm    string    // "gzip", "snappy", "zstd", ""
    OriginalSize int       // For metrics/debugging
    FreshUntil   time.Time
    StaleUntil   time.Time
}
```

### Implementation

```go
type Compressor struct {
    algorithm string
    threshold int
}

func (c *Compressor) MaybeCompress(data []byte) ([]byte, bool) {
    if len(data) < c.threshold {
        return data, false // Too small, skip compression
    }

    var compressed bytes.Buffer
    switch c.algorithm {
    case "gzip":
        w := gzip.NewWriter(&compressed)
        w.Write(data)
        w.Close()
    case "snappy":
        compressed.Write(snappy.Encode(nil, data))
    case "zstd":
        enc, _ := zstd.NewWriter(&compressed)
        enc.Write(data)
        enc.Close()
    }

    // Check if compression actually helped
    if compressed.Len() >= len(data) {
        return data, false // Compression made it bigger, skip
    }

    return compressed.Bytes(), true
}

func (c *Compressor) Decompress(data []byte, algorithm string) ([]byte, error) {
    switch algorithm {
    case "gzip":
        r, _ := gzip.NewReader(bytes.NewReader(data))
        return io.ReadAll(r)
    case "snappy":
        return snappy.Decode(nil, data)
    case "zstd":
        dec, _ := zstd.NewReader(bytes.NewReader(data))
        return io.ReadAll(dec)
    default:
        return data, nil // Not compressed
    }
}
```

### HTTP Integration: Serve Compressed Directly

When the client's `Accept-Encoding` matches our compression algorithm, skip decompression:

```go
func (c *RedisCache) GetForHTTP(
    ctx context.Context,
    key string,
    acceptEncoding string,
) (data []byte, contentEncoding string, err error) {
    entry, err := c.getEntry(ctx, key)
    if err != nil {
        return nil, "", err
    }

    // Check if client accepts our compression format
    if entry.Compressed && strings.Contains(acceptEncoding, entry.Algorithm) {
        // Serve compressed directly - no decompression needed!
        return entry.Data, entry.Algorithm, nil
    }

    // Client doesn't accept our format, decompress
    if entry.Compressed {
        data, err = c.compressor.Decompress(entry.Data, entry.Algorithm)
        if err != nil {
            return nil, "", err
        }
        return data, "", nil  // No Content-Encoding, already decompressed
    }

    return entry.Data, "", nil
}
```

### HTTP Handler Integration

```go
func handleCachedResponse(w http.ResponseWriter, r *http.Request, entry *CacheEntry) {
    acceptEncoding := r.Header.Get("Accept-Encoding")

    data, contentEncoding, err := cache.GetForHTTP(ctx, key, acceptEncoding)
    if err != nil {
        // Handle error
        return
    }

    if contentEncoding != "" {
        w.Header().Set("Content-Encoding", contentEncoding)
    }
    w.Header().Set("Content-Type", "application/json")
    w.Write(data)
}
```

### Algorithm Comparison

| Algorithm | Compression Ratio | Speed | CPU Usage | Best For |
|-----------|------------------|-------|-----------|----------|
| gzip      | High (~70-80%)   | Medium| Medium    | Max savings, HTTP native |
| snappy    | Medium (~50-60%) | Fast  | Low       | Speed priority |
| zstd      | Very High (~80%) | Fast  | Medium    | Best overall |

**Recommendation:** Use `gzip` for HTTP compatibility (most clients support it natively).

### Compression Metrics

Track compression effectiveness:

```go
type CompressionMetrics struct {
    TotalCompressed     int64
    TotalSkipped        int64   // Below threshold or poor ratio
    BytesSaved          int64
    AvgCompressionRatio float64
    DirectServes        int64   // Served compressed without decompression
    DecompressedServes  int64   // Had to decompress before serving
}
```

## Future Enhancements

### 1. Subscription Integration

When a subscription detects changes, trigger cache invalidation for related tables.

### 2. Cache Warming

Pre-populate cache on startup with frequently-used queries from allow list.

### 3. Metrics

Expose cache metrics:
- Hit rate
- Miss rate
- Invalidation count
- Average response size
- Redis latency

```go
type CacheMetrics struct {
    Hits        int64
    Misses      int64
    Invalidations int64
    BytesCached int64
}
```

## Dependencies

```go
// go.mod addition
require github.com/redis/go-redis/v9 v9.x.x
```

## Testing Strategy

1. **Unit tests**: Cache key generation, table extraction
2. **Integration tests**: Redis operations, invalidation flows
3. **Load tests**: Cache stampede handling, concurrent access
4. **Failure tests**: Redis unavailable, network timeouts
