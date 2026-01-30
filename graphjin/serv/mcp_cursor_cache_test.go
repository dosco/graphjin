package serv

import (
	"context"
	"testing"
	"time"
)

func TestMemoryCursorCache_SetGet(t *testing.T) {
	cache := NewMemoryCursorCache(100, time.Hour)
	defer cache.Close() //nolint:errcheck

	ctx := context.Background()
	cursor := "__gj-enc:abc123xyz789"

	// Set cursor
	id, err := cache.Set(ctx, cursor)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	if id == 0 {
		t.Fatal("Expected non-zero ID")
	}

	// Get cursor
	retrieved, err := cache.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if retrieved != cursor {
		t.Errorf("Expected %q, got %q", cursor, retrieved)
	}
}

func TestMemoryCursorCache_Deduplication(t *testing.T) {
	cache := NewMemoryCursorCache(100, time.Hour)
	defer cache.Close() //nolint:errcheck

	ctx := context.Background()
	cursor := "__gj-enc:abc123xyz789"

	// Set same cursor twice
	id1, err := cache.Set(ctx, cursor)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	id2, err := cache.Set(ctx, cursor)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Should return same ID (deduplication)
	if id1 != id2 {
		t.Errorf("Expected same ID for same cursor, got %d and %d", id1, id2)
	}
}

func TestMemoryCursorCache_DifferentCursors(t *testing.T) {
	cache := NewMemoryCursorCache(100, time.Hour)
	defer cache.Close() //nolint:errcheck

	ctx := context.Background()
	cursor1 := "__gj-enc:abc123"
	cursor2 := "__gj-enc:xyz789"

	id1, err := cache.Set(ctx, cursor1)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	id2, err := cache.Set(ctx, cursor2)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Should return different IDs
	if id1 == id2 {
		t.Errorf("Expected different IDs for different cursors, got same ID %d", id1)
	}

	// Should retrieve correct cursors
	retrieved1, err := cache.Get(ctx, id1)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if retrieved1 != cursor1 {
		t.Errorf("Expected %q, got %q", cursor1, retrieved1)
	}

	retrieved2, err := cache.Get(ctx, id2)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if retrieved2 != cursor2 {
		t.Errorf("Expected %q, got %q", cursor2, retrieved2)
	}
}

func TestMemoryCursorCache_NotFound(t *testing.T) {
	cache := NewMemoryCursorCache(100, time.Hour)
	defer cache.Close() //nolint:errcheck

	ctx := context.Background()

	// Get non-existent ID
	_, err := cache.Get(ctx, 999)
	if err == nil {
		t.Error("Expected error for non-existent ID")
	}
}

func TestMemoryCursorCache_Expiration(t *testing.T) {
	// Very short TTL
	cache := NewMemoryCursorCache(100, 10*time.Millisecond)
	defer cache.Close() //nolint:errcheck

	ctx := context.Background()
	cursor := "__gj-enc:abc123"

	id, err := cache.Set(ctx, cursor)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Should work immediately
	_, err = cache.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get failed immediately after set: %v", err)
	}

	// Wait for expiration
	time.Sleep(50 * time.Millisecond)

	// Should fail after expiration
	_, err = cache.Get(ctx, id)
	if err == nil {
		t.Error("Expected error for expired cursor")
	}
}

func TestMemoryCursorCache_Eviction(t *testing.T) {
	// Small cache
	cache := NewMemoryCursorCache(3, time.Hour)
	defer cache.Close() //nolint:errcheck

	ctx := context.Background()

	// Add 5 cursors (exceeds max of 3)
	var ids []uint64
	for i := 0; i < 5; i++ {
		cursor := "__gj-enc:cursor" + string(rune('a'+i))
		id, err := cache.Set(ctx, cursor)
		if err != nil {
			t.Fatalf("Set failed: %v", err)
		}
		ids = append(ids, id)
	}

	// First two should be evicted
	_, err := cache.Get(ctx, ids[0])
	if err == nil {
		t.Error("Expected first entry to be evicted")
	}

	_, err = cache.Get(ctx, ids[1])
	if err == nil {
		t.Error("Expected second entry to be evicted")
	}

	// Last three should still exist
	for i := 2; i < 5; i++ {
		_, err := cache.Get(ctx, ids[i])
		if err != nil {
			t.Errorf("Expected entry %d to exist, got error: %v", i, err)
		}
	}
}

func TestHashCursor(t *testing.T) {
	// Same input should produce same hash
	hash1 := hashCursor("__gj-enc:abc123")
	hash2 := hashCursor("__gj-enc:abc123")
	if hash1 != hash2 {
		t.Error("Same input should produce same hash")
	}

	// Different input should produce different hash
	hash3 := hashCursor("__gj-enc:xyz789")
	if hash1 == hash3 {
		t.Error("Different input should produce different hash")
	}

	// Hash should be 16 characters (8 bytes hex encoded)
	if len(hash1) != 16 {
		t.Errorf("Expected hash length 16, got %d", len(hash1))
	}
}
