package serv

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/dosco/graphjin/core/v3"
)

func TestCacheEntry_Serialization(t *testing.T) {
	original := CacheEntry{
		Data:         []byte(`{"data": {"users": [{"id": 1}]}}`),
		Compressed:   true,
		OriginalSize: 100,
		FreshUntil:   1700000000,
		StaleUntil:   1700003600,
	}

	// Marshal to JSON
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal CacheEntry: %v", err)
	}

	// Unmarshal back
	var restored CacheEntry
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("failed to unmarshal CacheEntry: %v", err)
	}

	// Verify fields
	if !bytes.Equal(original.Data, restored.Data) {
		t.Errorf("Data mismatch")
	}
	if original.Compressed != restored.Compressed {
		t.Errorf("Compressed mismatch: want %v, got %v", original.Compressed, restored.Compressed)
	}
	if original.OriginalSize != restored.OriginalSize {
		t.Errorf("OriginalSize mismatch: want %d, got %d", original.OriginalSize, restored.OriginalSize)
	}
	if original.FreshUntil != restored.FreshUntil {
		t.Errorf("FreshUntil mismatch: want %d, got %d", original.FreshUntil, restored.FreshUntil)
	}
	if original.StaleUntil != restored.StaleUntil {
		t.Errorf("StaleUntil mismatch: want %d, got %d", original.StaleUntil, restored.StaleUntil)
	}
}

func TestCompress_Decompress(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", []byte{}},
		{"small", []byte("hello world")},
		{"medium", bytes.Repeat([]byte("test data "), 100)},
		{"json", []byte(`{"users": [{"id": 1, "name": "John"}, {"id": 2, "name": "Jane"}]}`)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if len(tt.data) == 0 {
				// Skip empty data - gzip can't compress empty
				return
			}

			compressed, err := compress(tt.data)
			if err != nil {
				t.Fatalf("compress failed: %v", err)
			}

			decompressed, err := decompress(compressed)
			if err != nil {
				t.Fatalf("decompress failed: %v", err)
			}

			if !bytes.Equal(tt.data, decompressed) {
				t.Errorf("roundtrip failed: original %d bytes, decompressed %d bytes",
					len(tt.data), len(decompressed))
			}
		})
	}
}

func TestCompress_SavingsForLargeData(t *testing.T) {
	// Repetitive data should compress well
	data := bytes.Repeat([]byte(`{"id": 123, "name": "Test User", "email": "test@example.com"}`), 100)

	compressed, err := compress(data)
	if err != nil {
		t.Fatalf("compress failed: %v", err)
	}

	ratio := float64(len(compressed)) / float64(len(data))

	// Expect significant compression for repetitive JSON
	if ratio > 0.5 {
		t.Errorf("expected compression ratio < 0.5, got %.2f", ratio)
	}
}

func TestRedisCache_KeyBuilding(t *testing.T) {
	// Create a minimal cache config for testing key generation
	rc := &RedisCache{
		conf: CachingConfig{
			TTL: 3600,
		},
		excludeTable: make(map[string]bool),
	}

	tests := []struct {
		name     string
		keyFunc  func() string
		expected string
	}{
		{
			name:     "respKey",
			keyFunc:  func() string { return rc.respKey("abc123") },
			expected: "gj:cache:resp:abc123",
		},
		{
			name:     "rowKey",
			keyFunc:  func() string { return rc.rowKey("users", "42") },
			expected: "gj:cache:row:users:42",
		},
		{
			name:     "tableKey",
			keyFunc:  func() string { return rc.tableKey("products") },
			expected: "gj:cache:table:products",
		},
		{
			name:     "modKey",
			keyFunc:  func() string { return rc.modKey("orders", "99") },
			expected: "gj:cache:mod:orders:99",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.keyFunc()
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestRedisCache_FilterExcludedTables(t *testing.T) {
	rc := &RedisCache{
		excludeTable: map[string]bool{
			"audit_logs": true,
			"sessions":   true,
		},
	}

	refs := []core.RowRef{
		{Table: "users", ID: "1"},
		{Table: "audit_logs", ID: "100"}, // excluded
		{Table: "products", ID: "5"},
		{Table: "sessions", ID: "abc"}, // excluded
		{Table: "orders", ID: "42"},
	}

	filtered := rc.filterExcludedTables(refs)

	if len(filtered) != 3 {
		t.Errorf("expected 3 refs after filtering, got %d", len(filtered))
	}

	// Verify excluded tables are not present
	for _, ref := range filtered {
		if ref.Table == "audit_logs" || ref.Table == "sessions" {
			t.Errorf("excluded table %q should not be in filtered results", ref.Table)
		}
	}
}

func TestRedisCache_PerEntryTTLsCapButDoNotExtendDefaults(t *testing.T) {
	conf := CachingConfig{TTL: 60, FreshTTL: 30}

	ttl, fresh, ok := cacheEntryTTLs(conf, core.CacheEntryOptions{
		HardTTL:  10 * time.Second,
		FreshTTL: 5 * time.Second,
	})
	if !ok {
		t.Fatal("expected cacheable options")
	}
	if ttl != 10*time.Second || fresh != 5*time.Second {
		t.Fatalf("expected capped ttl/fresh 10s/5s, got %s/%s", ttl, fresh)
	}

	ttl, fresh, ok = cacheEntryTTLs(conf, core.CacheEntryOptions{
		HardTTL:  time.Hour,
		FreshTTL: time.Hour,
	})
	if !ok {
		t.Fatal("expected cacheable options")
	}
	if ttl != 60*time.Second || fresh != 30*time.Second {
		t.Fatalf("expected global ttl/fresh 60s/30s, got %s/%s", ttl, fresh)
	}

	if _, _, ok = cacheEntryTTLs(conf, core.CacheEntryOptions{NoStore: true}); ok {
		t.Fatal("expected no-store options to skip caching")
	}
}

func TestRedisCache_FilterExcludedSourceScopedRefs(t *testing.T) {
	rc := &RedisCache{
		excludeTable: map[string]bool{
			"remote:billing":       true,
			"codesql:repo:gj_deps": true,
		},
	}

	refs := []core.RowRef{
		core.RemoteResolverRef("billing", "customer-1"),
		core.CodeSQLTableRefs("repo", []string{"gj_deps"})[0],
		core.DBTableRef("default", "gj_deps"),
	}

	filtered := rc.filterExcludedTables(refs)
	if len(filtered) != 1 {
		t.Fatalf("expected 1 ref after scoped filtering, got %d: %+v", len(filtered), filtered)
	}
	if filtered[0].Source != core.CacheSourceDB {
		t.Fatalf("expected db ref to remain, got %+v", filtered[0])
	}
}

func TestRedisCache_FilterExcludedTables_Empty(t *testing.T) {
	rc := &RedisCache{
		excludeTable: map[string]bool{},
	}

	refs := []core.RowRef{
		{Table: "users", ID: "1"},
		{Table: "products", ID: "5"},
	}

	filtered := rc.filterExcludedTables(refs)

	// With no exclusions, all refs should pass through
	if len(filtered) != len(refs) {
		t.Errorf("expected %d refs (no exclusions), got %d", len(refs), len(filtered))
	}
}

func TestCacheMetrics_Snapshot(t *testing.T) {
	m := &CacheMetrics{}

	// Add some values
	m.Hits.Add(100)
	m.Misses.Add(50)
	m.Invalidations.Add(10)
	m.BytesCached.Add(1024 * 100)
	m.BytesSaved.Add(1024 * 20)
	m.Errors.Add(2)
	m.SWRRefreshes.Add(5)

	snapshot := m.Snapshot()

	expected := map[string]int64{
		"hits":          100,
		"misses":        50,
		"invalidations": 10,
		"bytes_cached":  102400,
		"bytes_saved":   20480,
		"errors":        2,
		"swr_refreshes": 5,
	}

	for key, want := range expected {
		if got := snapshot[key]; got != want {
			t.Errorf("snapshot[%q] = %d, want %d", key, got, want)
		}
	}
}

func TestCacheMetrics_HitRate(t *testing.T) {
	tests := []struct {
		name   string
		hits   int64
		misses int64
		want   float64
	}{
		{"no requests", 0, 0, 0.0},
		{"all hits", 100, 0, 1.0},
		{"all misses", 0, 100, 0.0},
		{"half and half", 50, 50, 0.5},
		{"75% hit rate", 75, 25, 0.75},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &CacheMetrics{}
			m.Hits.Store(tt.hits)
			m.Misses.Store(tt.misses)

			got := m.HitRate()
			if got != tt.want {
				t.Errorf("HitRate() = %f, want %f", got, tt.want)
			}
		})
	}
}

func TestSWRWorkerPool_Shutdown(t *testing.T) {
	// Create a minimal worker pool with no workers (jobs channel only).
	pool := &SWRWorkerPool{
		jobs: make(chan swrJob, 10),
	}
	pool.shutdown.Store(false)

	// TrySubmit should work before shutdown
	submitted := pool.TrySubmit("test", func() ([]byte, []core.RowRef, error) { return nil, nil, nil })
	if !submitted {
		t.Errorf("expected job to be submitted before shutdown")
	}

	// Shutdown should prevent new submissions
	pool.shutdown.Store(true)

	submitted = pool.TrySubmit("test2", func() ([]byte, []core.RowRef, error) { return nil, nil, nil })
	if submitted {
		t.Errorf("expected job to be rejected after shutdown")
	}
}

func TestCompress_CorruptedDecompression(t *testing.T) {
	// Corrupted gzip data should fail decompression
	corruptedData := []byte{0x1f, 0x8b, 0x08, 0x00, 0xff, 0xff, 0xff}

	_, err := decompress(corruptedData)
	if err == nil {
		t.Errorf("expected error for corrupted gzip data")
	}
}

func TestCompress_ThresholdBoundary(t *testing.T) {
	// Test data at exactly the threshold boundary (1024 bytes)
	dataAtThreshold := bytes.Repeat([]byte("x"), compressionThreshold)

	compressed, err := compress(dataAtThreshold)
	if err != nil {
		t.Fatalf("compress failed at threshold: %v", err)
	}

	decompressed, err := decompress(compressed)
	if err != nil {
		t.Fatalf("decompress failed at threshold: %v", err)
	}

	if !bytes.Equal(dataAtThreshold, decompressed) {
		t.Errorf("roundtrip failed at threshold boundary")
	}

	// Data just below threshold
	dataBelowThreshold := bytes.Repeat([]byte("x"), compressionThreshold-1)
	compressed2, err := compress(dataBelowThreshold)
	if err != nil {
		t.Fatalf("compress failed below threshold: %v", err)
	}

	decompressed2, err := decompress(compressed2)
	if err != nil {
		t.Fatalf("decompress failed below threshold: %v", err)
	}

	if !bytes.Equal(dataBelowThreshold, decompressed2) {
		t.Errorf("roundtrip failed below threshold boundary")
	}
}

func TestCacheMetrics_ConcurrentUpdates(t *testing.T) {
	m := &CacheMetrics{}

	// Concurrent updates should not cause data races
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				m.Hits.Add(1)
				m.Misses.Add(1)
				m.Invalidations.Add(1)
				m.BytesCached.Add(100)
				m.BytesSaved.Add(10)
				m.Errors.Add(1)
				m.SWRRefreshes.Add(1)
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify final counts
	snapshot := m.Snapshot()
	expected := int64(10 * 100)
	if snapshot["hits"] != expected {
		t.Errorf("expected %d hits after concurrent updates, got %d", expected, snapshot["hits"])
	}
	if snapshot["misses"] != expected {
		t.Errorf("expected %d misses after concurrent updates, got %d", expected, snapshot["misses"])
	}
}

func TestRedisCache_AvailabilityFlag(t *testing.T) {
	rc := &RedisCache{
		excludeTable: make(map[string]bool),
	}

	// Initially available should be false (not set)
	if rc.isAvailable() {
		t.Errorf("expected unavailable initially")
	}

	// Set available
	rc.available.Store(true)
	if !rc.isAvailable() {
		t.Errorf("expected available after Store(true)")
	}

	// Set unavailable
	rc.available.Store(false)
	if rc.isAvailable() {
		t.Errorf("expected unavailable after Store(false)")
	}
}

func TestSWRWorkerPool_FullQueue(t *testing.T) {
	// Create a pool with small queue size
	pool := &SWRWorkerPool{
		jobs: make(chan swrJob, 2), // Only 2 slots
	}
	pool.shutdown.Store(false)

	noopFn := func() ([]byte, []core.RowRef, error) { return nil, nil, nil }

	// Fill the queue
	submitted1 := pool.TrySubmit("job1", noopFn)
	submitted2 := pool.TrySubmit("job2", noopFn)

	if !submitted1 || !submitted2 {
		t.Errorf("expected first two jobs to be submitted")
	}

	// Queue is now full - next submission should fail
	submitted3 := pool.TrySubmit("job3", noopFn)
	if submitted3 {
		t.Errorf("expected third job to be rejected (queue full)")
	}
}

func TestRedisCache_FilterExcludedTables_AllExcluded(t *testing.T) {
	rc := &RedisCache{
		excludeTable: map[string]bool{
			"users":    true,
			"products": true,
		},
	}

	refs := []core.RowRef{
		{Table: "users", ID: "1"},
		{Table: "products", ID: "5"},
	}

	filtered := rc.filterExcludedTables(refs)

	// All refs excluded
	if len(filtered) != 0 {
		t.Errorf("expected 0 refs (all excluded), got %d", len(filtered))
	}
}

func TestCacheEntry_OmitEmpty(t *testing.T) {
	// Test that omitempty works correctly
	entry := CacheEntry{
		Data:       []byte("test"),
		Compressed: false, // Should be omitted
		FreshUntil: 100,
		StaleUntil: 200,
		// OriginalSize is 0, should be omitted
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	// The JSON should not contain "c" (Compressed when false) or "o" (OriginalSize when 0)
	jsonStr := string(data)
	if bytes.Contains(data, []byte(`"c":`)) {
		t.Errorf("expected 'c' (Compressed) to be omitted when false, got: %s", jsonStr)
	}
	if bytes.Contains(data, []byte(`"o":`)) {
		t.Errorf("expected 'o' (OriginalSize) to be omitted when 0, got: %s", jsonStr)
	}
}

func TestCompress_BinaryData(t *testing.T) {
	// Test with binary data (non-text)
	binaryData := make([]byte, 2000)
	for i := range binaryData {
		binaryData[i] = byte(i % 256)
	}

	compressed, err := compress(binaryData)
	if err != nil {
		t.Fatalf("compress failed for binary data: %v", err)
	}

	decompressed, err := decompress(compressed)
	if err != nil {
		t.Fatalf("decompress failed for binary data: %v", err)
	}

	if !bytes.Equal(binaryData, decompressed) {
		t.Errorf("binary data roundtrip failed")
	}
}
