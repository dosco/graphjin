package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// CacheKeyBuilder builds cache keys from query context
type CacheKeyBuilder struct{}

// NewCacheKeyBuilder creates a new cache key builder
func NewCacheKeyBuilder() *CacheKeyBuilder {
	return &CacheKeyBuilder{}
}

// Build creates a cache key from query parameters and context.
// The key is a SHA256 hash of: query identifier + query text + variables +
// database scope + user_id + role.
func (b *CacheKeyBuilder) Build(
	ctx context.Context,
	opName string,
	apqKey string,
	query []byte,
	vars json.RawMessage,
	role string,
	databases ...string,
) string {
	h := sha256.New()

	// Use APQ key if available, otherwise operation name
	if apqKey != "" {
		h.Write([]byte("apq:"))
		h.Write([]byte(apqKey))
	} else if opName != "" {
		h.Write([]byte("op:"))
		h.Write([]byte(opName))
	} else {
		return "" // Anonymous queries not cached
	}

	// Include full query text
	if len(query) > 0 {
		h.Write([]byte(":query:"))
		h.Write(query)
	}

	// Include variables
	if len(vars) > 0 {
		h.Write([]byte(":vars:"))
		h.Write(vars)
	}

	// Include role for permission isolation
	h.Write([]byte(":role:"))
	h.Write([]byte(role))

	// Include database scope so identical named queries against different
	// databases never collide in the shared response cache.
	for _, database := range databases {
		if database == "" {
			continue
		}
		h.Write([]byte(":db:"))
		h.Write([]byte(database))
	}

	// Include user_id from context for user isolation
	if userID := ctx.Value(UserIDKey); userID != nil {
		fmt.Fprintf(h, ":uid:%v", userID) //nolint:errcheck
	}

	return hex.EncodeToString(h.Sum(nil))
}

// BuildFragment creates a cache key for a source-owned execution fragment.
// Parts should fully describe the fragment's source, selection, variables,
// and compiled statement shape; the builder adds role/user isolation.
func (b *CacheKeyBuilder) BuildFragment(
	ctx context.Context,
	kind string,
	role string,
	parts map[string]interface{},
) string {
	if kind == "" {
		return ""
	}

	h := sha256.New()
	h.Write([]byte("frag:"))
	h.Write([]byte(kind))
	h.Write([]byte(":role:"))
	h.Write([]byte(role))

	if userID := ctx.Value(UserIDKey); userID != nil {
		fmt.Fprintf(h, ":uid:%v", userID) //nolint:errcheck
	}

	keys := make([]string, 0, len(parts))
	for k := range parts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		h.Write([]byte(":"))
		h.Write([]byte(k))
		h.Write([]byte("="))
		switch v := parts[k].(type) {
		case []byte:
			h.Write(v)
		case json.RawMessage:
			h.Write(v)
		default:
			b, _ := json.Marshal(v)
			h.Write(b)
		}
	}

	return hex.EncodeToString(h.Sum(nil))
}

// ShouldCache determines if a query should be cached.
// Only named queries and APQ queries are cached (skip anonymous).
func (b *CacheKeyBuilder) ShouldCache(opName, apqKey string) bool {
	return apqKey != "" || opName != ""
}

// BuildCacheKey is a convenience function that builds a cache key
func BuildCacheKey(
	ctx context.Context,
	opName string,
	apqKey string,
	query []byte,
	vars json.RawMessage,
	role string,
	databases ...string,
) string {
	return NewCacheKeyBuilder().Build(ctx, opName, apqKey, query, vars, role, databases...)
}

// ShouldCacheQuery is a convenience function that checks if query should be cached
func ShouldCacheQuery(opName, apqKey string) bool {
	return NewCacheKeyBuilder().ShouldCache(opName, apqKey)
}
