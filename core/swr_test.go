package core

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// fakeSWRCache implements ResponseCacheProvider + SWRRefresher and records
// SubmitRefresh calls so a unit test can verify the dispatch path.
type fakeSWRCache struct {
	staleData     []byte
	stale         bool
	found         bool
	submitCount   atomic.Int64
	lastKey       atomic.Value // string
	lastFn        atomic.Value // RefreshFn
	submitAccepts bool
}

func (c *fakeSWRCache) Get(ctx context.Context, key string) ([]byte, bool, bool) {
	return c.staleData, c.stale, c.found
}

func (c *fakeSWRCache) Set(
	ctx context.Context,
	key string,
	data []byte,
	refs []RowRef,
	queryStartTime time.Time,
) error {
	return nil
}

func (c *fakeSWRCache) InvalidateRows(ctx context.Context, refs []RowRef) error {
	return nil
}

func (c *fakeSWRCache) SubmitRefresh(key string, fn RefreshFn) bool {
	c.submitCount.Add(1)
	c.lastKey.Store(key)
	if fn != nil {
		c.lastFn.Store(fn)
	}
	return c.submitAccepts
}

func newFakeStaleCache() *fakeSWRCache {
	return &fakeSWRCache{
		staleData:     []byte(`{"data":{"users":[]}}`),
		stale:         true,
		found:         true,
		submitAccepts: true,
	}
}

// minimalEngine builds a graphjinEngine with just enough wiring for cache
// path tests. Using an unexported constructor would be cleaner, but the
// engine is large; only the responseCache, cacheKeyBuilder, and namespace
// fields matter for the SWR dispatch.
func minimalEngine(provider ResponseCacheProvider) *graphjinEngine {
	return &graphjinEngine{
		responseCache:   provider,
		cacheKeyBuilder: NewCacheKeyBuilder(),
	}
}

func newReq(name, query string) GraphqlReq {
	return GraphqlReq{
		name:  name,
		query: []byte(query),
	}
}

// TestTryCacheGet_StaleHitDispatchesRefresh verifies that a stale cache hit
// returns the stale data immediately AND submits a background refresh job.
func TestTryCacheGet_StaleHitDispatchesRefresh(t *testing.T) {
	fake := newFakeStaleCache()
	gj := minimalEngine(fake)

	s := &gstate{
		gj:   gj,
		r:    newReq("getUsers", "query getUsers { users { id } }"),
		role: "anon",
	}

	hit := s.tryCacheGet(context.Background())
	if !hit {
		t.Fatalf("expected cache hit, got miss")
	}
	if !s.cacheHit {
		t.Errorf("expected s.cacheHit=true after stale hit")
	}
	if string(s.data) != `{"data":{"users":[]}}` {
		t.Errorf("expected stale data populated on hit, got %q", string(s.data))
	}
	if got := fake.submitCount.Load(); got != 1 {
		t.Errorf("expected 1 SubmitRefresh call, got %d", got)
	}
	if k, _ := fake.lastKey.Load().(string); k != s.cacheKey {
		t.Errorf("expected SubmitRefresh key=%q, got %q", s.cacheKey, k)
	}
}

// TestTryCacheGet_FreshHitSkipsRefresh verifies a fresh hit does not
// dispatch a refresh.
func TestTryCacheGet_FreshHitSkipsRefresh(t *testing.T) {
	fake := newFakeStaleCache()
	fake.stale = false
	gj := minimalEngine(fake)

	s := &gstate{
		gj:   gj,
		r:    newReq("getUsers", "query getUsers { users { id } }"),
		role: "anon",
	}

	if !s.tryCacheGet(context.Background()) {
		t.Fatalf("expected fresh cache hit")
	}
	if got := fake.submitCount.Load(); got != 0 {
		t.Errorf("expected no SubmitRefresh on fresh hit, got %d", got)
	}
}

// TestTryCacheGet_AnonymousQuerySkipsCache verifies queries without an
// operation name or APQ key are not cached and don't dispatch refresh.
func TestTryCacheGet_AnonymousQuerySkipsCache(t *testing.T) {
	fake := newFakeStaleCache()
	gj := minimalEngine(fake)

	s := &gstate{
		gj:   gj,
		r:    newReq("", "{ users { id } }"),
		role: "anon",
	}

	if s.tryCacheGet(context.Background()) {
		t.Errorf("expected anonymous query to skip cache")
	}
	if !s.skipCache {
		t.Errorf("expected s.skipCache=true for anonymous query")
	}
	if got := fake.submitCount.Load(); got != 0 {
		t.Errorf("expected no SubmitRefresh for anonymous query, got %d", got)
	}
}

// TestSubmitSWRRefresh_NonRefresherIsNoOp verifies that a cache provider
// without SWR support is silently ignored.
func TestSubmitSWRRefresh_NonRefresherIsNoOp(t *testing.T) {
	noopCache := &fakeNoSWRCache{}
	gj := minimalEngine(noopCache)
	s := &gstate{
		gj:       gj,
		r:        newReq("getUsers", "query getUsers { users { id } }"),
		role:     "anon",
		cacheKey: "abc123",
	}
	// Should not panic and should not error.
	s.submitSWRRefresh(context.Background())
}

type fakeNoSWRCache struct{}

func (c *fakeNoSWRCache) Get(ctx context.Context, key string) ([]byte, bool, bool) {
	return nil, false, false
}
func (c *fakeNoSWRCache) Set(
	ctx context.Context,
	key string,
	data []byte,
	refs []RowRef,
	queryStartTime time.Time,
) error {
	return nil
}
func (c *fakeNoSWRCache) InvalidateRows(ctx context.Context, refs []RowRef) error {
	return nil
}
