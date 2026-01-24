package serv

import (
	"context"
	"testing"
	"time"

	"github.com/dosco/graphjin/core/v3"
)

func TestMemoryCache_BasicOperations(t *testing.T) {
	conf := CachingConfig{
		TTL:      3600,
		FreshTTL: 300,
	}

	mc, err := NewMemoryCache(conf, 100)
	if err != nil {
		t.Fatalf("failed to create memory cache: %v", err)
	}
	defer mc.Close()

	ctx := context.Background()
	key := "test-key"
	data := []byte(`{"data": {"users": [{"id": 1}]}}`)
	refs := []core.RowRef{{Table: "users", ID: "1"}}

	// Test Set
	err = mc.Set(ctx, key, data, refs, time.Now())
	if err != nil {
		t.Fatalf("failed to set cache: %v", err)
	}

	// Test Get
	result, isStale, found := mc.Get(ctx, key)
	if !found {
		t.Errorf("expected to find cached entry")
	}
	if isStale {
		t.Errorf("expected entry to be fresh")
	}
	if string(result) != string(data) {
		t.Errorf("expected %s, got %s", data, result)
	}

	// Verify metrics
	snapshot := mc.Metrics().Snapshot()
	if snapshot["hits"] != 1 {
		t.Errorf("expected 1 hit, got %d", snapshot["hits"])
	}
}

func TestMemoryCache_Miss(t *testing.T) {
	conf := CachingConfig{TTL: 3600}
	mc, err := NewMemoryCache(conf, 100)
	if err != nil {
		t.Fatalf("failed to create memory cache: %v", err)
	}
	defer mc.Close()

	ctx := context.Background()
	_, _, found := mc.Get(ctx, "nonexistent-key")
	if found {
		t.Errorf("expected cache miss")
	}

	snapshot := mc.Metrics().Snapshot()
	if snapshot["misses"] != 1 {
		t.Errorf("expected 1 miss, got %d", snapshot["misses"])
	}
}

func TestMemoryCache_InvalidateRows(t *testing.T) {
	conf := CachingConfig{TTL: 3600}
	mc, err := NewMemoryCache(conf, 100)
	if err != nil {
		t.Fatalf("failed to create memory cache: %v", err)
	}
	defer mc.Close()

	ctx := context.Background()
	key := "test-key"
	data := []byte(`{"data": {}}`)
	refs := []core.RowRef{{Table: "users", ID: "1"}}

	// Set cache
	err = mc.Set(ctx, key, data, refs, time.Now())
	if err != nil {
		t.Fatalf("failed to set cache: %v", err)
	}

	// Verify it's cached
	_, _, found := mc.Get(ctx, key)
	if !found {
		t.Errorf("expected to find cached entry before invalidation")
	}

	// Invalidate
	err = mc.InvalidateRows(ctx, refs)
	if err != nil {
		t.Fatalf("failed to invalidate: %v", err)
	}

	// Verify it's gone
	_, _, found = mc.Get(ctx, key)
	if found {
		t.Errorf("expected cache miss after invalidation")
	}

	snapshot := mc.Metrics().Snapshot()
	if snapshot["invalidations"] != 1 {
		t.Errorf("expected 1 invalidation, got %d", snapshot["invalidations"])
	}
}

func TestMemoryCache_ExcludeTables(t *testing.T) {
	conf := CachingConfig{
		TTL:           3600,
		ExcludeTables: []string{"audit_logs"},
	}
	mc, err := NewMemoryCache(conf, 100)
	if err != nil {
		t.Fatalf("failed to create memory cache: %v", err)
	}
	defer mc.Close()

	refs := []core.RowRef{
		{Table: "users", ID: "1"},
		{Table: "audit_logs", ID: "100"},
	}

	filtered := mc.filterExcludedTables(refs)
	if len(filtered) != 1 {
		t.Errorf("expected 1 ref after filtering, got %d", len(filtered))
	}
	if filtered[0].Table != "users" {
		t.Errorf("expected users table, got %s", filtered[0].Table)
	}
}

func TestMemoryCache_Compression(t *testing.T) {
	conf := CachingConfig{TTL: 3600}
	mc, err := NewMemoryCache(conf, 100)
	if err != nil {
		t.Fatalf("failed to create memory cache: %v", err)
	}
	defer mc.Close()

	ctx := context.Background()
	key := "large-key"
	// Create data larger than compression threshold (1024 bytes)
	largeData := make([]byte, 2000)
	for i := range largeData {
		largeData[i] = 'x'
	}

	err = mc.Set(ctx, key, largeData, nil, time.Now())
	if err != nil {
		t.Fatalf("failed to set cache: %v", err)
	}

	result, _, found := mc.Get(ctx, key)
	if !found {
		t.Errorf("expected to find cached entry")
	}
	if len(result) != len(largeData) {
		t.Errorf("expected %d bytes, got %d bytes", len(largeData), len(result))
	}

	// Verify compression savings were recorded
	snapshot := mc.Metrics().Snapshot()
	if snapshot["bytes_saved"] == 0 {
		t.Errorf("expected compression savings")
	}
}

func TestMemoryCache_LRUEviction(t *testing.T) {
	conf := CachingConfig{TTL: 3600}
	mc, err := NewMemoryCache(conf, 3) // Very small cache
	if err != nil {
		t.Fatalf("failed to create memory cache: %v", err)
	}
	defer mc.Close()

	ctx := context.Background()
	data := []byte(`{}`)

	// Add 4 entries to a cache with size 3
	for i := 0; i < 4; i++ {
		key := string(rune('a' + i))
		err = mc.Set(ctx, key, data, nil, time.Now())
		if err != nil {
			t.Fatalf("failed to set cache: %v", err)
		}
	}

	// First entry should be evicted
	_, _, found := mc.Get(ctx, "a")
	if found {
		t.Errorf("expected first entry to be evicted")
	}

	// Later entries should exist
	_, _, found = mc.Get(ctx, "d")
	if !found {
		t.Errorf("expected last entry to exist")
	}
}
