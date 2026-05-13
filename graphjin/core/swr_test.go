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

func TestFragmentCacheGet_StaleHitDispatchesFragmentRefresh(t *testing.T) {
	fake := newFakeStaleCache()
	gj := minimalEngine(fake)
	s := &gstate{
		gj:   gj,
		r:    newReq("getUsers", "query getUsers { users { id } }"),
		role: "anon",
	}

	refresh := func() ([]byte, []RowRef, CacheEntryOptions, error) {
		return []byte(`{"users":[]}`), []RowRef{DBTableRef("default", "users")}, CacheEntryOptions{}, nil
	}
	data, hit := s.fragmentCacheGet(context.Background(), "fragment-key", refresh)
	if !hit {
		t.Fatalf("expected fragment cache hit")
	}
	if string(data) != string(fake.staleData) {
		t.Fatalf("unexpected cached data: %s", data)
	}
	if got := fake.submitCount.Load(); got != 1 {
		t.Fatalf("expected 1 fragment refresh submission, got %d", got)
	}
	if s.fragmentHits.Load() != 1 || s.fragmentMisses.Load() != 0 {
		t.Fatalf("unexpected fragment hit/miss counters: hits=%d misses=%d",
			s.fragmentHits.Load(), s.fragmentMisses.Load())
	}
}

func TestFragmentCacheGet_UsesOptionAwareRefresh(t *testing.T) {
	fake := &fakeOptionSWRCache{
		staleData: []byte(`{"items":[]}`),
		stale:     true,
		found:     true,
	}
	gj := minimalEngine(fake)
	s := &gstate{
		gj:   gj,
		r:    newReq("getFiles", "query getFiles { files { key url } }"),
		role: "anon",
	}

	opts := CacheEntryOptions{HardTTL: time.Minute}
	refresh := func() ([]byte, []RowRef, CacheEntryOptions, error) {
		return []byte(`{"items":[]}`), []RowRef{FilesystemPrefixRef("files", "")}, opts, nil
	}
	if _, hit := s.fragmentCacheGet(context.Background(), "fragment-key", refresh); !hit {
		t.Fatalf("expected fragment cache hit")
	}
	if got := fake.submitOptionsCount.Load(); got != 1 {
		t.Fatalf("expected option-aware refresh submission, got %d", got)
	}
	fn := fake.lastOptionsFn()
	if fn == nil {
		t.Fatal("expected option-aware refresh fn to be stored")
	}
	data, refs, gotOpts, err := fn()
	if err != nil {
		t.Fatalf("refresh fn: %v", err)
	}
	if string(data) != `{"items":[]}` || len(refs) != 1 || gotOpts != opts {
		t.Fatalf("unexpected refresh output data=%s refs=%+v opts=%+v", data, refs, gotOpts)
	}
}

func TestFragmentCacheSet_UsesSetWithOptions(t *testing.T) {
	fake := &fakeOptionSWRCache{}
	gj := minimalEngine(fake)
	s := &gstate{
		gj:   gj,
		r:    newReq("getFiles", "query getFiles { files { key url } }"),
		role: "anon",
	}

	opts := CacheEntryOptions{HardTTL: time.Minute}
	s.fragmentCacheSet(context.Background(), "fragment-key", []byte(`[]`), []RowRef{FilesystemPrefixRef("files", "")}, time.Now(), opts)
	if fake.setOptions != opts {
		t.Fatalf("expected SetWithOptions opts %+v, got %+v", opts, fake.setOptions)
	}
}

type fakeOptionSWRCache struct {
	staleData          []byte
	stale              bool
	found              bool
	setOptions         CacheEntryOptions
	submitOptionsCount atomic.Int64
	lastFn             atomic.Value // RefreshFnWithOptions
}

func (c *fakeOptionSWRCache) Get(ctx context.Context, key string) ([]byte, bool, bool) {
	return c.staleData, c.stale, c.found
}

func (c *fakeOptionSWRCache) Set(
	ctx context.Context,
	key string,
	data []byte,
	refs []RowRef,
	queryStartTime time.Time,
) error {
	return c.SetWithOptions(ctx, key, data, refs, queryStartTime, CacheEntryOptions{})
}

func (c *fakeOptionSWRCache) SetWithOptions(
	ctx context.Context,
	key string,
	data []byte,
	refs []RowRef,
	queryStartTime time.Time,
	opts CacheEntryOptions,
) error {
	c.setOptions = opts
	return nil
}

func (c *fakeOptionSWRCache) InvalidateRows(ctx context.Context, refs []RowRef) error {
	return nil
}

func (c *fakeOptionSWRCache) SubmitRefresh(key string, fn RefreshFn) bool {
	return false
}

func (c *fakeOptionSWRCache) SubmitRefreshWithOptions(key string, fn RefreshFnWithOptions) bool {
	c.submitOptionsCount.Add(1)
	c.lastFn.Store(fn)
	return true
}

func (c *fakeOptionSWRCache) lastOptionsFn() RefreshFnWithOptions {
	fn, _ := c.lastFn.Load().(RefreshFnWithOptions)
	return fn
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
