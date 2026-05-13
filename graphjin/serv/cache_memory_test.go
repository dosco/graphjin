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
	defer mc.Close() //nolint:errcheck

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
	defer mc.Close() //nolint:errcheck

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
	defer mc.Close() //nolint:errcheck

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

func TestMemoryCache_SetWithOptionsCapsHardTTL(t *testing.T) {
	conf := CachingConfig{TTL: 3600, FreshTTL: 300}
	mc, err := NewMemoryCache(conf, 100)
	if err != nil {
		t.Fatalf("failed to create memory cache: %v", err)
	}
	defer mc.Close() //nolint:errcheck

	ctx := context.Background()
	if err := mc.SetWithOptions(ctx, "signed-url", []byte(`{"url":"signed"}`), nil, time.Now(), core.CacheEntryOptions{
		HardTTL: time.Second,
	}); err != nil {
		t.Fatalf("set with options: %v", err)
	}
	if _, _, found := mc.Get(ctx, "signed-url"); !found {
		t.Fatal("expected entry immediately after set")
	}
	time.Sleep(1200 * time.Millisecond)
	if _, _, found := mc.Get(ctx, "signed-url"); found {
		t.Fatal("expected per-entry hard ttl to expire before global ttl")
	}
}

func TestMemoryCache_SetWithOptionsCapsFreshTTL(t *testing.T) {
	conf := CachingConfig{TTL: 3600, FreshTTL: 300}
	mc, err := NewMemoryCache(conf, 100)
	if err != nil {
		t.Fatalf("failed to create memory cache: %v", err)
	}
	defer mc.Close() //nolint:errcheck

	ctx := context.Background()
	if err := mc.SetWithOptions(ctx, "swr", []byte(`{"ok":true}`), nil, time.Now(), core.CacheEntryOptions{
		HardTTL:  5 * time.Second,
		FreshTTL: time.Second,
	}); err != nil {
		t.Fatalf("set with options: %v", err)
	}
	time.Sleep(1200 * time.Millisecond)
	if _, stale, found := mc.Get(ctx, "swr"); !found || !stale {
		t.Fatalf("expected stale-but-present entry, found=%v stale=%v", found, stale)
	}
}

func TestMemoryCache_SetWithOptionsNoStore(t *testing.T) {
	conf := CachingConfig{TTL: 3600}
	mc, err := NewMemoryCache(conf, 100)
	if err != nil {
		t.Fatalf("failed to create memory cache: %v", err)
	}
	defer mc.Close() //nolint:errcheck

	ctx := context.Background()
	if err := mc.SetWithOptions(ctx, "nostore", []byte(`{"ok":true}`), nil, time.Now(), core.CacheEntryOptions{
		NoStore: true,
	}); err != nil {
		t.Fatalf("set with options: %v", err)
	}
	if _, _, found := mc.Get(ctx, "nostore"); found {
		t.Fatal("expected no-store entry to be skipped")
	}
}

func TestMemoryCache_SourceOwnedInvalidation(t *testing.T) {
	conf := CachingConfig{TTL: 3600}
	mc, err := NewMemoryCache(conf, 100)
	if err != nil {
		t.Fatalf("failed to create memory cache: %v", err)
	}
	defer mc.Close() //nolint:errcheck

	ctx := context.Background()
	dbKey := "db-fragment"
	fsKey := "fs-fragment"

	if err := mc.Set(ctx, dbKey, []byte(`{"users":[{"id":1}]}`),
		[]core.RowRef{core.DBRowRef("default", "users", "1")}, time.Now()); err != nil {
		t.Fatalf("set db fragment: %v", err)
	}
	if err := mc.Set(ctx, fsKey, []byte(`[{"key":"users/1/avatar.png"}]`),
		[]core.RowRef{{Source: core.CacheSourceFS, Scope: "avatars", Kind: core.CacheKindPrefix, ID: "users/1/"}}, time.Now()); err != nil {
		t.Fatalf("set fs fragment: %v", err)
	}

	if err := mc.InvalidateRows(ctx, core.FilesystemKeyRefs("avatars", "users/1/avatar.png")); err != nil {
		t.Fatalf("invalidate fs key: %v", err)
	}

	if _, _, found := mc.Get(ctx, fsKey); found {
		t.Fatal("expected filesystem fragment to be invalidated")
	}
	if _, _, found := mc.Get(ctx, dbKey); !found {
		t.Fatal("expected db parent fragment to remain cached")
	}
}

func TestMemoryCache_CodeSQLInvalidationDoesNotEvictDBTable(t *testing.T) {
	conf := CachingConfig{TTL: 3600}
	mc, err := NewMemoryCache(conf, 100)
	if err != nil {
		t.Fatalf("failed to create memory cache: %v", err)
	}
	defer mc.Close() //nolint:errcheck

	ctx := context.Background()
	dbKey := "db-users"
	codeKey := "codesql-users"

	if err := mc.Set(ctx, dbKey, []byte(`{"users":[]}`),
		[]core.RowRef{core.DBRowRef("default", "users", "1")}, time.Now()); err != nil {
		t.Fatalf("set db fragment: %v", err)
	}
	if err := mc.Set(ctx, codeKey, []byte(`{"users":[]}`),
		core.CodeSQLTableRefs("repo", []string{"users"}), time.Now()); err != nil {
		t.Fatalf("set codesql fragment: %v", err)
	}

	if err := mc.InvalidateRows(ctx, core.CodeSQLTableRefs("repo", []string{"users"})); err != nil {
		t.Fatalf("invalidate codesql: %v", err)
	}

	if _, _, found := mc.Get(ctx, codeKey); found {
		t.Fatal("expected codesql fragment to be invalidated")
	}
	if _, _, found := mc.Get(ctx, dbKey); !found {
		t.Fatal("expected db fragment with same table name to remain cached")
	}
}

func TestMemoryCache_InvalidateTables(t *testing.T) {
	conf := CachingConfig{TTL: 3600}
	mc, err := NewMemoryCache(conf, 100)
	if err != nil {
		t.Fatalf("failed to create memory cache: %v", err)
	}
	defer mc.Close() //nolint:errcheck

	ctx := context.Background()
	key := "table-key"
	data := []byte(`{"data": {}}`)
	refs := []core.RowRef{
		{Table: "users", ID: "1"},
		{Table: "users", ID: "2"},
	}

	if err := mc.Set(ctx, key, data, refs, time.Now()); err != nil {
		t.Fatalf("failed to set cache: %v", err)
	}
	if _, _, found := mc.Get(ctx, key); !found {
		t.Fatal("expected cached entry before table invalidation")
	}
	if err := mc.InvalidateTables(ctx, []string{"users"}); err != nil {
		t.Fatalf("failed to invalidate tables: %v", err)
	}
	if _, _, found := mc.Get(ctx, key); found {
		t.Fatal("expected cache miss after table invalidation")
	}
}

func TestMemoryCache_TableModificationSafety(t *testing.T) {
	conf := CachingConfig{TTL: 3600}
	mc, err := NewMemoryCache(conf, 100)
	if err != nil {
		t.Fatalf("failed to create memory cache: %v", err)
	}
	defer mc.Close() //nolint:errcheck

	ctx := context.Background()
	refs := []core.RowRef{{Table: "users", ID: "1"}}
	queryStarted := time.Now().Add(-time.Second)

	if err := mc.InvalidateTables(ctx, []string{"users"}); err != nil {
		t.Fatalf("failed to invalidate tables: %v", err)
	}
	if err := mc.Set(ctx, "stale-key", []byte(`{}`), refs, queryStarted); err != nil {
		t.Fatalf("unexpected set error: %v", err)
	}
	if _, _, found := mc.Get(ctx, "stale-key"); found {
		t.Fatal("expected stale query result to be rejected from cache after table invalidation")
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
	defer mc.Close() //nolint:errcheck

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

func TestMemoryCache_ExcludeSourceScopedRefs(t *testing.T) {
	conf := CachingConfig{
		TTL:           3600,
		ExcludeTables: []string{"fs:avatars", "codesql:repo:gj_files"},
	}
	mc, err := NewMemoryCache(conf, 100)
	if err != nil {
		t.Fatalf("failed to create memory cache: %v", err)
	}
	defer mc.Close() //nolint:errcheck

	refs := []core.RowRef{
		core.FilesystemPrefixRef("avatars", ""),
		core.CodeSQLTableRefs("repo", []string{"gj_files"})[0],
		core.DBTableRef("default", "gj_files"),
	}

	filtered := mc.filterExcludedTables(refs)
	if len(filtered) != 1 {
		t.Fatalf("expected 1 ref after scoped filtering, got %d: %+v", len(filtered), filtered)
	}
	if filtered[0].Source != core.CacheSourceDB {
		t.Fatalf("expected db ref to remain, got %+v", filtered[0])
	}
}

func TestMemoryCache_Compression(t *testing.T) {
	conf := CachingConfig{TTL: 3600}
	mc, err := NewMemoryCache(conf, 100)
	if err != nil {
		t.Fatalf("failed to create memory cache: %v", err)
	}
	defer mc.Close() //nolint:errcheck

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
	defer mc.Close() //nolint:errcheck

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
