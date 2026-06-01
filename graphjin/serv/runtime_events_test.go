package serv

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/core/v3/sourcecap"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.uber.org/zap/zaptest"
)

func TestMemoryRuntimeEventStoreTrimTTLRedactionOrderingAndStatus(t *testing.T) {
	now := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	store := newMemoryRuntimeEventStore(runtimeEventOptions{
		MaxEvents: 2,
		TTL:       time.Hour,
		NodeID:    "node-a",
		Now: func() time.Time {
			return now
		},
	})

	store.Record(context.Background(), runtimeEvent{Summary: "old", Details: map[string]any{"safe": "old"}})
	now = now.Add(2 * time.Hour)
	store.Record(context.Background(), runtimeEvent{Summary: "first", Details: map[string]any{"safe": "first"}})
	now = now.Add(time.Second)
	store.Record(context.Background(), runtimeEvent{
		Summary:       "second redis://:secret@localhost:6379/0",
		NextAction:    "do not leak token=abc123",
		Details:       map[string]any{"password": "secret", "safe": "visible"},
		SuggestedNext: []string{"retry redis://:secret@localhost:6379/0", "token=abc123"},
	})
	now = now.Add(time.Second)
	store.Record(context.Background(), runtimeEvent{Summary: "third"})

	rows := store.Rows(context.Background(), runtimeStatus{
		Mode:        modeAgentic,
		Phase:       "runtime",
		Status:      runtimeStatusReady,
		Severity:    "info",
		Summary:     "ready",
		NextAction:  "continue",
		Source:      "graphjin",
		SourceKind:  sourcecap.KindGraphJin,
		SchemaReady: true,
	})
	if len(rows) != 3 {
		t.Fatalf("expected status plus two events, got %d rows: %+v", len(rows), rows)
	}
	if rows[0]["kind"] != runtimeKindStatus || rows[0]["store"] != "memory" || rows[0]["node_id"] != "node-a" {
		t.Fatalf("unexpected status row: %+v", rows[0])
	}
	if rows[1]["summary"] != "third" || rows[2]["summary"] != "second [REDACTED]" {
		t.Fatalf("expected newest event ordering and max trim, got %+v", rows)
	}
	if rows[2]["next_action"] != "do not leak [REDACTED]" {
		t.Fatalf("expected redacted next_action, got %+v", rows[2]["next_action"])
	}
	details, ok := rows[2]["details_json"].(string)
	if !ok || details == "" {
		t.Fatalf("expected details_json string, got %+v", rows[2]["details_json"])
	}
	if details != `{"password":"[REDACTED]","safe":"visible"}` {
		t.Fatalf("expected redacted details, got %s", details)
	}
	if got := fmt.Sprint(store.events[0].Details["password"]); got != "[REDACTED]" {
		t.Fatalf("expected stored event details to be redacted, got %s", got)
	}
	if strings.Contains(strings.Join(store.events[0].SuggestedNext, " "), "secret") {
		t.Fatalf("expected stored suggested next to be redacted, got %+v", store.events[0].SuggestedNext)
	}
	if suggested, _ := rows[2]["suggested_next_json"].(string); !strings.Contains(suggested, "[REDACTED]") {
		t.Fatalf("expected redacted suggested_next_json, got %s", suggested)
	}
}

func TestRuntimeRedisKeysAreScopedAndSanitized(t *testing.T) {
	keys := runtimeKeys(runtimeScope(&Config{
		Core: core.Config{},
		Serv: Serv{AppName: "My App/Prod"},
	}, "system db", ptrString("Team One")))

	if keys.Prefix != "gj:runtime:my_app_prod:team_one:system_db" {
		t.Fatalf("unexpected prefix: %s", keys.Prefix)
	}
	if keys.Events != keys.Prefix+":events" || keys.Statuses != keys.Prefix+":status:" {
		t.Fatalf("unexpected scoped keys: %+v", keys)
	}
}

func TestRuntimeRedisFallbackUsesMemoryAndDegradedStatus(t *testing.T) {
	conf := &Config{
		Core: core.Config{
			Mode: "agentic",
			Sources: []core.SourceConfig{{
				Name: "graphjin",
				Kind: "graphjin",
			}},
		},
		Serv: Serv{
			AppName: "Runtime Test",
			Redis:   RedisConfig{URL: "://not-a-redis-url"},
		},
	}
	svc := &graphjinService{
		conf:   conf,
		log:    zaptest.NewLogger(t).Sugar(),
		tracer: otel.Tracer("runtime-events-test"),
	}
	if err := normalizeServiceSources(conf); err != nil {
		t.Fatalf("normalize sources: %v", err)
	}
	if err := svc.initRuntimeObservability(); err != nil {
		t.Fatalf("init runtime observability: %v", err)
	}
	if svc.runtimeEvents == nil || svc.runtimeEvents.Name() != "memory" {
		t.Fatalf("expected memory fallback store, got %#v", svc.runtimeEvents)
	}
	rows := svc.runtimeEvents.Rows(context.Background(), svc.runtimeCurrentStatus())
	if len(rows) < 2 {
		t.Fatalf("expected degraded status and fallback event rows, got %+v", rows)
	}
	if rows[0]["kind"] != runtimeKindStatus || rows[0]["status"] != runtimeStatusDegraded || rows[0]["store"] != "memory" {
		t.Fatalf("expected degraded memory status row, got %+v", rows[0])
	}
	var sawFallback bool
	for _, row := range rows {
		if row["error_code"] == "redis_unavailable" {
			sawFallback = true
		}
	}
	if !sawFallback {
		t.Fatalf("expected redis fallback event, got %+v", rows)
	}
}

func TestObservedAuthHandlerRecordsJWTFailure(t *testing.T) {
	conf := &Config{
		Core: core.Config{
			Mode: "agentic",
			Sources: []core.SourceConfig{{
				Name: "graphjin",
				Kind: "graphjin",
			}},
		},
		Serv: Serv{Auth: Auth{Type: "jwt"}},
	}
	svc := &graphjinService{
		conf:   conf,
		log:    zaptest.NewLogger(t).Sugar(),
		tracer: otel.Tracer("runtime-events-test"),
	}
	if err := normalizeServiceSources(conf); err != nil {
		t.Fatalf("normalize sources: %v", err)
	}
	if err := svc.initRuntimeObservability(); err != nil {
		t.Fatalf("init runtime observability: %v", err)
	}

	ah := svc.observeAuthHandler(func(_ http.ResponseWriter, r *http.Request) (context.Context, error) {
		return r.Context(), fmt.Errorf("no jwt token found in cookie or authorization header")
	})
	_, err := ah(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/v1/graphql", nil))
	if err == nil {
		t.Fatal("expected auth error")
	}

	rows := svc.runtimeEvents.Rows(context.Background(), svc.runtimeCurrentStatus())
	var sawMissing bool
	for _, row := range rows {
		if row["error_code"] == "jwt_missing" {
			sawMissing = true
			if strings.Contains(fmt.Sprint(row["details_json"]), "authorization header") {
				t.Fatalf("runtime auth failure details should not store raw auth error: %+v", row)
			}
		}
	}
	if !sawMissing {
		t.Fatalf("expected jwt_missing runtime event, got %+v", rows)
	}
}

func TestRedisRuntimeEventStoreWithFakeRedis(t *testing.T) {
	now := time.Date(2026, 5, 31, 13, 0, 0, 0, time.UTC)
	client := newFakeRuntimeRedis()
	store, err := newRedisRuntimeEventStoreWithClient(client, runtimeEventOptions{
		MaxEvents: 2,
		TTL:       time.Hour,
		NodeID:    "node-r",
		Scope:     "app:team:system",
		Now: func() time.Time {
			return now
		},
	})
	if err != nil {
		t.Fatalf("new redis runtime store: %v", err)
	}

	for _, summary := range []string{"first", "second", "third"} {
		store.Record(context.Background(), runtimeEvent{Summary: summary, Status: runtimeStatusReady})
		now = now.Add(time.Second)
	}
	rows := store.Rows(context.Background(), runtimeStatus{
		Mode:        modeAgentic,
		Phase:       "runtime",
		Status:      runtimeStatusReady,
		Severity:    "info",
		Summary:     "ready",
		NextAction:  "continue",
		Source:      "graphjin",
		SourceKind:  sourcecap.KindGraphJin,
		SchemaReady: true,
	})
	if len(rows) != 3 {
		t.Fatalf("expected status plus two Redis event rows, got %d: %+v", len(rows), rows)
	}
	if rows[0]["kind"] != runtimeKindStatus || rows[0]["store"] != "redis" || rows[0]["node_id"] != "node-r" {
		t.Fatalf("unexpected Redis status row: %+v", rows[0])
	}
	if rows[1]["summary"] != "third" || rows[2]["summary"] != "second" {
		t.Fatalf("expected Redis newest ordering and trim, got %+v", rows)
	}
	if len(client.statusKeys("gj:runtime:app:team:system:status:")) != 1 {
		t.Fatalf("expected scoped status key to be written, keys=%+v", client.strings)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close redis store: %v", err)
	}
	if !client.closed {
		t.Fatal("expected fake Redis client to be closed")
	}
}

func ptrString(v string) *string {
	return &v
}

type fakeRuntimeRedis struct {
	mu      sync.Mutex
	strings map[string]string
	zsets   map[string][]redis.Z
	closed  bool
}

func newFakeRuntimeRedis() *fakeRuntimeRedis {
	return &fakeRuntimeRedis{
		strings: make(map[string]string),
		zsets:   make(map[string][]redis.Z),
	}
}

func (f *fakeRuntimeRedis) Ping(context.Context) *redis.StatusCmd {
	return redis.NewStatusResult("PONG", nil)
}

func (f *fakeRuntimeRedis) Set(_ context.Context, key string, value any, _ time.Duration) *redis.StatusCmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.strings[key] = fmt.Sprint(value)
	return redis.NewStatusResult("OK", nil)
}

func (f *fakeRuntimeRedis) Get(_ context.Context, key string) *redis.StringCmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	value, ok := f.strings[key]
	if !ok {
		return redis.NewStringResult("", redis.Nil)
	}
	return redis.NewStringResult(value, nil)
}

func (f *fakeRuntimeRedis) Scan(_ context.Context, _ uint64, match string, _ int64) *redis.ScanCmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	prefix := strings.TrimSuffix(match, "*")
	keys := f.statusKeys(prefix)
	return redis.NewScanCmdResult(keys, 0, nil)
}

func (f *fakeRuntimeRedis) ZAdd(_ context.Context, key string, values ...redis.Z) *redis.IntCmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.zsets[key] = append(f.zsets[key], values...)
	return redis.NewIntResult(int64(len(values)), nil)
}

func (f *fakeRuntimeRedis) ZRevRange(_ context.Context, key string, start, stop int64) *redis.StringSliceCmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	values := append([]redis.Z(nil), f.zsets[key]...)
	sort.SliceStable(values, func(i, j int) bool {
		return values[i].Score > values[j].Score
	})
	if start < 0 {
		start = int64(len(values)) + start
	}
	if stop < 0 {
		stop = int64(len(values)) + stop
	}
	if start < 0 {
		start = 0
	}
	if stop >= int64(len(values)) {
		stop = int64(len(values)) - 1
	}
	if len(values) == 0 || start > stop {
		return redis.NewStringSliceResult(nil, nil)
	}
	out := make([]string, 0, stop-start+1)
	for _, value := range values[start : stop+1] {
		out = append(out, fmt.Sprint(value.Member))
	}
	return redis.NewStringSliceResult(out, nil)
}

func (f *fakeRuntimeRedis) ZRemRangeByScore(_ context.Context, key, _, max string) *redis.IntCmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	maxScore, err := strconv.ParseFloat(max, 64)
	if err != nil {
		return redis.NewIntResult(0, nil)
	}
	values := f.zsets[key]
	kept := values[:0]
	var removed int64
	for _, value := range values {
		if value.Score <= maxScore {
			removed++
			continue
		}
		kept = append(kept, value)
	}
	f.zsets[key] = kept
	return redis.NewIntResult(removed, nil)
}

func (f *fakeRuntimeRedis) ZRemRangeByRank(_ context.Context, key string, start, stop int64) *redis.IntCmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	values := append([]redis.Z(nil), f.zsets[key]...)
	sort.SliceStable(values, func(i, j int) bool {
		return values[i].Score < values[j].Score
	})
	length := int64(len(values))
	if start < 0 {
		start = length + start
	}
	if stop < 0 {
		stop = length + stop
	}
	if start < 0 {
		start = 0
	}
	if stop >= length {
		stop = length - 1
	}
	if length == 0 || start > stop {
		return redis.NewIntResult(0, nil)
	}
	removed := make(map[any]struct{}, stop-start+1)
	for _, value := range values[start : stop+1] {
		removed[value.Member] = struct{}{}
	}
	kept := f.zsets[key][:0]
	for _, value := range f.zsets[key] {
		if _, ok := removed[value.Member]; ok {
			continue
		}
		kept = append(kept, value)
	}
	f.zsets[key] = kept
	return redis.NewIntResult(int64(len(removed)), nil)
}

func (f *fakeRuntimeRedis) Expire(context.Context, string, time.Duration) *redis.BoolCmd {
	return redis.NewBoolResult(true, nil)
}

func (f *fakeRuntimeRedis) Close() error {
	f.closed = true
	return nil
}

func (f *fakeRuntimeRedis) statusKeys(prefix string) []string {
	keys := make([]string, 0, len(f.strings))
	for key := range f.strings {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}
