package serv

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

// CursorCache is the interface for MCP cursor caching
// It maps short numeric IDs to encrypted cursor strings for LLM-friendly pagination
type CursorCache interface {
	// Set stores a cursor and returns a short numeric ID
	Set(ctx context.Context, cursor string) (uint64, error)

	// Get retrieves a cursor by its numeric ID
	Get(ctx context.Context, id uint64) (string, error)

	// Close releases resources
	Close() error
}

// Redis key prefixes for cursor cache
const (
	cursorPrefix     = "gj:cursor:"
	cursorIDKey      = cursorPrefix + "id:"      // id:<id> -> cursor string
	cursorRevKey     = cursorPrefix + "rev:"     // rev:<hash> -> id (for deduplication)
	cursorNextIDKey  = cursorPrefix + "next"     // atomic counter for ID generation
	cursorRedisTimeout = 100 * time.Millisecond
)

// RedisCursorCache uses Redis for distributed cursor caching
type RedisCursorCache struct {
	client *redis.Client
	ttl    time.Duration
}

// NewRedisCursorCache creates a new Redis cursor cache
func NewRedisCursorCache(redisURL string, ttl time.Duration) (*RedisCursorCache, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("invalid redis URL: %w", err)
	}

	client := redis.NewClient(opts)

	ctx, cancel := context.WithTimeout(context.Background(), cursorRedisTimeout)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis connection failed: %w", err)
	}

	return &RedisCursorCache{
		client: client,
		ttl:    ttl,
	}, nil
}

// Set stores a cursor and returns a short numeric ID
func (c *RedisCursorCache) Set(ctx context.Context, cursor string) (uint64, error) {
	ctx, cancel := context.WithTimeout(ctx, cursorRedisTimeout)
	defer cancel()

	// Check if cursor already exists (deduplication)
	hash := hashCursor(cursor)
	revKey := cursorRevKey + hash

	// Try to get existing ID
	existingID, err := c.client.Get(ctx, revKey).Uint64()
	if err == nil {
		// Refresh TTL on existing entries
		pipe := c.client.Pipeline()
		pipe.Expire(ctx, revKey, c.ttl)
		pipe.Expire(ctx, cursorIDKey+fmt.Sprintf("%d", existingID), c.ttl)
		pipe.Exec(ctx)
		return existingID, nil
	}

	// Generate new ID atomically
	id, err := c.client.Incr(ctx, cursorNextIDKey).Uint64()
	if err != nil {
		return 0, fmt.Errorf("failed to generate cursor ID: %w", err)
	}

	idKey := cursorIDKey + fmt.Sprintf("%d", id)

	// Store both mappings
	pipe := c.client.Pipeline()
	pipe.Set(ctx, idKey, cursor, c.ttl)
	pipe.Set(ctx, revKey, id, c.ttl)
	_, err = pipe.Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to store cursor: %w", err)
	}

	return id, nil
}

// Get retrieves a cursor by its numeric ID
func (c *RedisCursorCache) Get(ctx context.Context, id uint64) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, cursorRedisTimeout)
	defer cancel()

	idKey := cursorIDKey + fmt.Sprintf("%d", id)
	cursor, err := c.client.Get(ctx, idKey).Result()
	if err == redis.Nil {
		return "", fmt.Errorf("cursor not found (ID: %d may have expired)", id)
	}
	if err != nil {
		return "", fmt.Errorf("failed to get cursor: %w", err)
	}

	return cursor, nil
}

// Close closes the Redis connection
func (c *RedisCursorCache) Close() error {
	return c.client.Close()
}

// MemoryCursorCache uses in-memory LRU as fallback
type MemoryCursorCache struct {
	mu         sync.RWMutex
	idToCursor map[uint64]*cursorEntry
	cursorToID map[string]uint64
	nextID     atomic.Uint64
	maxEntries int
	ttl        time.Duration

	// LRU order tracking (doubly-linked list)
	head *cursorNode // oldest
	tail *cursorNode // newest
}

type cursorEntry struct {
	cursor    string
	createdAt time.Time
	node      *cursorNode
}

type cursorNode struct {
	id   uint64
	prev *cursorNode
	next *cursorNode
}

// NewMemoryCursorCache creates a new in-memory cursor cache
func NewMemoryCursorCache(maxEntries int, ttl time.Duration) *MemoryCursorCache {
	if maxEntries <= 0 {
		maxEntries = 10000
	}

	mc := &MemoryCursorCache{
		idToCursor: make(map[uint64]*cursorEntry),
		cursorToID: make(map[string]uint64),
		maxEntries: maxEntries,
		ttl:        ttl,
	}

	// Start background cleanup goroutine
	go mc.cleanupLoop()

	return mc
}

// Set stores a cursor and returns a short numeric ID
func (c *MemoryCursorCache) Set(ctx context.Context, cursor string) (uint64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if cursor already exists (deduplication)
	if existingID, ok := c.cursorToID[cursor]; ok {
		// Move to end of LRU and refresh timestamp
		entry := c.idToCursor[existingID]
		if entry != nil {
			entry.createdAt = time.Now()
			c.moveToTail(entry.node)
		}
		return existingID, nil
	}

	// Generate new ID
	id := c.nextID.Add(1)

	// Create entry
	node := &cursorNode{id: id}
	entry := &cursorEntry{
		cursor:    cursor,
		createdAt: time.Now(),
		node:      node,
	}

	// Add to maps
	c.idToCursor[id] = entry
	c.cursorToID[cursor] = id

	// Add to LRU tail
	c.addToTail(node)

	// Evict if over capacity
	for len(c.idToCursor) > c.maxEntries {
		c.evictOldest()
	}

	return id, nil
}

// Get retrieves a cursor by its numeric ID
func (c *MemoryCursorCache) Get(ctx context.Context, id uint64) (string, error) {
	c.mu.RLock()
	entry, ok := c.idToCursor[id]
	c.mu.RUnlock()

	if !ok {
		return "", fmt.Errorf("cursor not found (ID: %d may have expired)", id)
	}

	// Check if expired
	if time.Since(entry.createdAt) > c.ttl {
		// Remove expired entry
		c.mu.Lock()
		c.removeEntry(id)
		c.mu.Unlock()
		return "", fmt.Errorf("cursor not found (ID: %d may have expired)", id)
	}

	return entry.cursor, nil
}

// Close stops the cleanup goroutine
func (c *MemoryCursorCache) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Clear all data
	c.idToCursor = make(map[uint64]*cursorEntry)
	c.cursorToID = make(map[string]uint64)
	c.head = nil
	c.tail = nil

	return nil
}

// LRU list operations (must be called with lock held)

func (c *MemoryCursorCache) addToTail(node *cursorNode) {
	if c.tail == nil {
		c.head = node
		c.tail = node
		return
	}
	node.prev = c.tail
	c.tail.next = node
	c.tail = node
}

func (c *MemoryCursorCache) moveToTail(node *cursorNode) {
	if node == c.tail {
		return
	}

	// Remove from current position
	if node.prev != nil {
		node.prev.next = node.next
	} else {
		c.head = node.next
	}
	if node.next != nil {
		node.next.prev = node.prev
	}

	// Add to tail
	node.prev = c.tail
	node.next = nil
	if c.tail != nil {
		c.tail.next = node
	}
	c.tail = node
}

func (c *MemoryCursorCache) evictOldest() {
	if c.head == nil {
		return
	}

	id := c.head.id
	c.removeEntry(id)
}

func (c *MemoryCursorCache) removeEntry(id uint64) {
	entry, ok := c.idToCursor[id]
	if !ok {
		return
	}

	// Remove from maps
	delete(c.idToCursor, id)
	delete(c.cursorToID, entry.cursor)

	// Remove from linked list
	node := entry.node
	if node.prev != nil {
		node.prev.next = node.next
	} else {
		c.head = node.next
	}
	if node.next != nil {
		node.next.prev = node.prev
	} else {
		c.tail = node.prev
	}
}

// cleanupLoop periodically removes expired entries
func (c *MemoryCursorCache) cleanupLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		c.cleanupExpired()
	}
}

func (c *MemoryCursorCache) cleanupExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	var toRemove []uint64

	for id, entry := range c.idToCursor {
		if now.Sub(entry.createdAt) > c.ttl {
			toRemove = append(toRemove, id)
		}
	}

	for _, id := range toRemove {
		c.removeEntry(id)
	}
}

// hashCursor creates a short hash of a cursor for deduplication
func hashCursor(cursor string) string {
	h := sha256.Sum256([]byte(cursor))
	return hex.EncodeToString(h[:8]) // Use first 8 bytes (16 hex chars)
}

// NewCursorCache creates a cursor cache using Redis if available, otherwise in-memory
func NewCursorCache(redisURL string, ttl time.Duration, maxEntries int) (CursorCache, error) {
	if redisURL != "" {
		cache, err := NewRedisCursorCache(redisURL, ttl)
		if err != nil {
			// Fall back to memory cache
			return NewMemoryCursorCache(maxEntries, ttl), nil
		}
		return cache, nil
	}
	return NewMemoryCursorCache(maxEntries, ttl), nil
}
