package serv

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/core/v3/sourcecap"
	"github.com/redis/go-redis/v9"
)

const (
	runtimeRootTable = "gj_runtime"

	runtimeKindStatus = "status"
	runtimeKindEvent  = "event"

	runtimeStatusReady    = "ready"
	runtimeStatusDegraded = "degraded"
	runtimeStatusFailed   = "failed"

	runtimeDefaultMaxEvents = 1000
	runtimeDefaultTTL       = 24 * time.Hour
	runtimeRedisTimeout     = 100 * time.Millisecond
)

type runtimeEventStore interface {
	Name() string
	NodeID() string
	Record(context.Context, runtimeEvent)
	Rows(context.Context, runtimeStatus) []map[string]any
	Close() error
}

type runtimeEvent struct {
	ID                string
	Kind              string
	CreatedAt         time.Time
	NodeID            string
	Mode              string
	Store             string
	Phase             string
	Status            string
	Severity          string
	Summary           string
	NextAction        string
	Source            string
	SourceKind        string
	DatabaseName      string
	ActiveDatabase    string
	SchemaReady       bool
	TableCount        int
	CatalogRevision   string
	DurationMS        int64
	ErrorCode         string
	Details           map[string]any
	DetailsJSON       string
	SuggestedNext     []string
	SuggestedNextJSON string
}

type runtimeStatus struct {
	Mode            string
	Phase           string
	Status          string
	Severity        string
	Summary         string
	NextAction      string
	Source          string
	SourceKind      string
	DatabaseName    string
	ActiveDatabase  string
	SchemaReady     bool
	TableCount      int
	CatalogRevision string
	Details         map[string]any
	SuggestedNext   []string
}

type runtimeEventOptions struct {
	MaxEvents int
	TTL       time.Duration
	NodeID    string
	Scope     string
	Now       func() time.Time
}

type memoryRuntimeEventStore struct {
	mu              sync.Mutex
	maxEvents       int
	ttl             time.Duration
	nodeID          string
	storeName       string
	now             func() time.Time
	seq             uint64
	events          []runtimeEvent
	degraded        bool
	degradedSummary string
	degradedNext    string
}

type redisRuntimeClient interface {
	Ping(context.Context) *redis.StatusCmd
	Set(context.Context, string, any, time.Duration) *redis.StatusCmd
	Get(context.Context, string) *redis.StringCmd
	Scan(context.Context, uint64, string, int64) *redis.ScanCmd
	ZAdd(context.Context, string, ...redis.Z) *redis.IntCmd
	ZRevRange(context.Context, string, int64, int64) *redis.StringSliceCmd
	ZRemRangeByScore(context.Context, string, string, string) *redis.IntCmd
	ZRemRangeByRank(context.Context, string, int64, int64) *redis.IntCmd
	Expire(context.Context, string, time.Duration) *redis.BoolCmd
	Close() error
}

type redisRuntimeEventStore struct {
	client   redisRuntimeClient
	fallback *memoryRuntimeEventStore
	keys     runtimeRedisKeys
	max      int64
	ttl      time.Duration
	now      func() time.Time
}

type runtimeRedisKeys struct {
	Prefix   string
	Events   string
	Statuses string
}

var runtimeSecretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b[^\s"'@:;=/]+:[^\s"'@;]+@(?:tcp|unix)\([^)]+\)/[^\s"']*`),
	regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.-]*://[^\s"'/@:]+:[^\s"'@/]*@[^\s"']+`),
	regexp.MustCompile(`(?i)\b(postgres|postgresql|mysql|mariadb|sqlserver|redis|mongodb|snowflake)://[^\s"']+`),
	regexp.MustCompile(`(?i)\b(password|pwd|token|secret|private_key|connection_string|key_passphrase|client_key|api_key|apikey)=([^\s;&,]+)`),
}

type runtimeQueryHandler struct {
	service *graphjinService
}

func (s *graphjinService) runtimeObservabilityEnabled() bool {
	if s == nil || s.conf == nil {
		return false
	}
	return s.conf.runtimeRootSourceEnabled()
}

func (s *graphjinService) initRuntimeObservability() error {
	if !s.runtimeObservabilityEnabled() {
		s.runtimeEvents = nil
		return nil
	}
	opts := runtimeEventOptionsFromConfig(s.conf, s.metadataDB, s.namespace)
	if opts.NodeID == "" {
		opts.NodeID = defaultRuntimeNodeID()
	}
	if s.conf.Redis.URL != "" {
		store, err := newRedisRuntimeEventStore(s.conf.Redis.URL, opts)
		if err == nil {
			s.runtimeEvents = store
			s.recordRuntimeEvent(context.Background(), runtimeEvent{
				Phase:      "runtime_store",
				Status:     runtimeStatusReady,
				Severity:   "info",
				Summary:    "Runtime observability is using Redis for shared agentic status and events.",
				NextAction: "Query gj_runtime before workflow, config, or schema actions when system state matters.",
				Details:    map[string]any{"store": "redis", "scope": opts.Scope},
				SuggestedNext: []string{
					`query { gj_runtime(where: { kind: { in: ["status", "event"] } }, order_by: { created_at: desc }, limit: 20) { kind status severity summary next_action details_json } }`,
				},
			})
			return nil
		}
		memStore := newMemoryRuntimeEventStore(opts)
		memStore.setDegraded("Redis runtime store is unavailable; using process-local memory events.", "Continue with gj_runtime, but treat event rows as local to this GraphJin node.")
		s.runtimeEvents = memStore
		s.recordRuntimeEvent(context.Background(), runtimeEvent{
			Phase:      "runtime_store",
			Status:     runtimeStatusDegraded,
			Severity:   "warn",
			Summary:    "Redis runtime store fallback: using memory for agentic runtime events.",
			NextAction: "Check redis.url connectivity if multi-node agentic status should be shared.",
			ErrorCode:  "redis_unavailable",
			Details:    map[string]any{"store": "memory", "redis_configured": true, "error": err.Error(), "scope": opts.Scope},
			SuggestedNext: []string{
				"Use gj_runtime for current-node decision support; do not treat memory events as cross-node history.",
				"Fix Redis connectivity for horizontally scaled deployments.",
			},
		})
		return nil
	}
	s.runtimeEvents = newMemoryRuntimeEventStore(opts)
	s.recordRuntimeEvent(context.Background(), runtimeEvent{
		Phase:      "runtime_store",
		Status:     runtimeStatusReady,
		Severity:   "info",
		Summary:    "Runtime observability is using bounded in-memory events.",
		NextAction: "Use gj_runtime for decision support on this GraphJin node.",
		Details:    map[string]any{"store": "memory", "max_events": opts.MaxEvents, "ttl_seconds": int(opts.TTL.Seconds()), "scope": opts.Scope},
	})
	return nil
}

func (s *graphjinService) reinitRuntimeObservability() {
	if s == nil {
		return
	}
	if s.runtimeEvents != nil {
		if err := s.runtimeEvents.Close(); err != nil && s.log != nil {
			s.log.Warnf("runtime observability close error: %s", err)
		}
		s.runtimeEvents = nil
	}
	if err := s.initRuntimeObservability(); err != nil && s.log != nil {
		s.log.Warnf("runtime observability init error: %s", err)
	}
}

func (s *graphjinService) recordRuntimeEvent(ctx context.Context, event runtimeEvent) {
	if s == nil || s.runtimeEvents == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if event.Mode == "" && s.conf != nil {
		event.Mode = effectiveMode(s.conf)
	}
	if event.Source == "" {
		event.Source = "graphjin"
	}
	if event.SourceKind == "" {
		event.SourceKind = sourcecap.KindGraphJin
	}
	if event.ActiveDatabase == "" {
		event.ActiveDatabase = s.activeRuntimeDatabase()
	}
	if event.DatabaseName == "" {
		event.DatabaseName = event.ActiveDatabase
	}
	if s.gj != nil && !event.SchemaReady {
		event.SchemaReady = s.gj.SchemaReady()
	}
	s.runtimeEvents.Record(ctx, event)
}

func (s *graphjinService) runtimeCurrentStatus() runtimeStatus {
	activeDatabase := ""
	if s != nil {
		activeDatabase = s.activeRuntimeDatabase()
	}
	status := runtimeStatus{
		Mode:           modeDev,
		Phase:          "runtime",
		Status:         runtimeStatusDegraded,
		Severity:       "warn",
		Summary:        "GraphJin runtime status is not initialized.",
		NextAction:     "Use configuration or database setup tools before querying application data.",
		Source:         "graphjin",
		SourceKind:     sourcecap.KindGraphJin,
		ActiveDatabase: activeDatabase,
		SuggestedNext: []string{
			`query { gj_runtime(where: { kind: { eq: "event" } }, order_by: { created_at: desc }, limit: 20) { created_at phase status severity summary next_action details_json } }`,
		},
	}
	if s == nil || s.conf == nil {
		return status
	}
	status.Mode = effectiveMode(s.conf)
	status.DatabaseName = status.ActiveDatabase
	status.Details = map[string]any{
		"redis_configured": s.conf.Redis.URL != "",
		"runtime_read":     s.runtimeObservabilityEnabled(),
	}
	if s.gj == nil {
		status.Summary = "GraphJin core is not initialized."
		status.NextAction = "Inspect recent gj_runtime events and update the database or configuration before application queries."
		return status
	}
	status.SchemaReady = s.gj.SchemaReady()
	if md, err := s.gj.MetadataSnapshot(s.metadataSnapshotExcludesFor(s.metadataDB, &s.conf.Core, s.managedDBs)...); err == nil {
		status.TableCount = len(md.Tables)
		status.Details["database_count"] = len(md.Databases)
	}
	if snap, err := s.catalogSnapshot(); err == nil {
		status.CatalogRevision = snap.Revision
	}
	if !status.SchemaReady {
		status.Summary = "GraphJin is running but application schema readiness is degraded."
		status.NextAction = "Check recent schema/database events, then reload or fix the database configuration before application queries."
		return status
	}
	status.Status = runtimeStatusReady
	status.Severity = "info"
	status.Summary = "GraphJin runtime is ready."
	status.NextAction = "Proceed with the requested workflow; re-check gj_runtime after errors or before guarded system changes."
	return status
}

func (s *graphjinService) registerRuntimeSchemaCallbacks() {
	if s == nil || s.gj == nil || s.runtimeEvents == nil {
		return
	}
	s.gj.OnSchemaChange(func(dbName string, hash string) {
		s.recordRuntimeEvent(context.Background(), runtimeEvent{
			Phase:        "schema",
			Status:       runtimeStatusReady,
			Severity:     "info",
			Summary:      "GraphJin schema metadata changed or finished initial discovery.",
			NextAction:   "Refresh catalog-guided planning if your next action depends on table, column, or relationship shape.",
			DatabaseName: dbName,
			ErrorCode:    "",
			Details:      map[string]any{"schema_hash": hash},
			SuggestedNext: []string{
				`query { gj_catalog(where: { kind: { in: ["table", "relationship"] } }, limit: 20) { id kind title summary database_name table_name details_json } }`,
			},
		})
	})
}

func (s *graphjinService) activeRuntimeDatabase() string {
	if s == nil {
		return ""
	}
	return (&mcpServer{service: s}).getActiveDatabase()
}

func (h runtimeQueryHandler) ManagedQueryTables() []core.ManagedTable {
	return []core.ManagedTable{runtimeManagedTable()}
}

func (h runtimeQueryHandler) ExecuteManagedQuery(ctx context.Context, req core.ManagedQueryRequest) (json.RawMessage, error) {
	out := make(map[string]any, len(req.Roots))
	if h.service == nil {
		for _, root := range req.Roots {
			out[root.FieldName] = nil
		}
		return json.Marshal(out)
	}
	authorized := h.service.runtimeRootQueryAuthorized(ctx)
	current := h.service.runtimeCurrentStatus()
	for _, root := range req.Roots {
		if !strings.EqualFold(root.Table, runtimeRootTable) {
			return nil, fmt.Errorf("unsupported GraphJin runtime query root: %s", root.Table)
		}
		if !authorized {
			out[root.FieldName] = nil
			continue
		}
		if h.service.runtimeEvents == nil {
			out[root.FieldName] = nil
			continue
		}
		rows := h.service.runtimeEvents.Rows(ctx, current)
		rows = applyManagedQuery(rows, root)
		out[root.FieldName] = filterRows(rows, root.Fields)
	}
	return json.Marshal(out)
}

func (s *graphjinService) runtimeRootQueryAuthorized(ctx context.Context) bool {
	if s == nil || !s.runtimeObservabilityEnabled() {
		return false
	}
	if s.conf != nil && s.conf.Core.IsSourcesUsed() {
		return s.sourceModeRootAccessAllowed(runtimeRootTable, runtimeRoleClass(ctx))
	}
	return runtimeContextRole(ctx) == "user"
}

func runtimeManagedTable() core.ManagedTable {
	return managedTable(runtimeRootTable, []core.ManagedColumn{
		cpCol("id", "text", true),
		cpCol("kind", "text", false),
		cpCol("created_at", "text", false),
		cpCol("node_id", "text", false),
		cpCol("mode", "text", false),
		cpCol("store", "text", false),
		cpCol("phase", "text", false),
		cpCol("status", "text", false),
		cpCol("severity", "text", false),
		cpCol("summary", "text", false),
		cpCol("next_action", "text", false),
		cpCol("source", "text", false),
		cpCol("source_kind", "text", false),
		cpCol("database_name", "text", false),
		cpCol("active_database", "text", false),
		cpCol("schema_ready", "boolean", false),
		cpCol("table_count", "integer", false),
		cpCol("catalog_revision", "text", false),
		cpCol("duration_ms", "integer", false),
		cpCol("error_code", "text", false),
		cpCol("details_json", "json", false),
		cpCol("suggested_next_json", "json", false),
		cpFullTextCol("search_vector"),
	})
}

func runtimeContextRole(ctx context.Context) string {
	if ctx == nil {
		return "anon"
	}
	if role, ok := ctx.Value(core.UserRoleKey).(string); ok && strings.TrimSpace(role) != "" {
		return role
	}
	switch ctx.Value(core.UserIDKey).(type) {
	case string, int:
		return "user"
	default:
		return "anon"
	}
}

func runtimeEventOptionsFromConfig(conf *Config, metadataDB string, namespace *string) runtimeEventOptions {
	opts := runtimeEventOptions{
		MaxEvents: runtimeDefaultMaxEvents,
		TTL:       runtimeDefaultTTL,
		NodeID:    defaultRuntimeNodeID(),
		Now:       time.Now,
	}
	if conf != nil {
		if conf.RuntimeEvents.MaxEvents > 0 {
			opts.MaxEvents = conf.RuntimeEvents.MaxEvents
		}
		if conf.RuntimeEvents.TTLSeconds > 0 {
			opts.TTL = time.Duration(conf.RuntimeEvents.TTLSeconds) * time.Second
		}
	}
	opts.Scope = runtimeScope(conf, metadataDB, namespace)
	return opts
}

func defaultRuntimeNodeID() string {
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		host = "node"
	}
	return cleanRuntimeKeyPart(host) + "-" + strconv.Itoa(os.Getpid())
}

func runtimeScope(conf *Config, metadataDB string, namespace *string) string {
	app := "graphjin"
	if conf != nil && strings.TrimSpace(conf.AppName) != "" {
		app = conf.AppName
	}
	ns := "default"
	if namespace != nil && strings.TrimSpace(*namespace) != "" {
		ns = *namespace
	}
	db := metadataDB
	if db == "" && conf != nil {
		db = conf.Core.CatalogDatabaseName()
	}
	if db == "" {
		db = defaultMetadataDBName
	}
	return strings.Join([]string{cleanRuntimeKeyPart(app), cleanRuntimeKeyPart(ns), cleanRuntimeKeyPart(db)}, ":")
}

func cleanRuntimeKeyPart(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "default"
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "_.-")
	if out == "" {
		out = "default"
	}
	if len(out) <= 80 {
		return out
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(out))
	return out[:64] + "_" + strconv.FormatUint(uint64(h.Sum32()), 16)
}

func runtimeKeys(scope string) runtimeRedisKeys {
	prefix := "gj:runtime:" + cleanRuntimeScope(scope)
	return runtimeRedisKeys{
		Prefix:   prefix,
		Events:   prefix + ":events",
		Statuses: prefix + ":status:",
	}
}

func cleanRuntimeScope(scope string) string {
	parts := strings.Split(scope, ":")
	for i := range parts {
		parts[i] = cleanRuntimeKeyPart(parts[i])
	}
	return strings.Join(parts, ":")
}

func newMemoryRuntimeEventStore(opts runtimeEventOptions) *memoryRuntimeEventStore {
	if opts.MaxEvents <= 0 {
		opts.MaxEvents = runtimeDefaultMaxEvents
	}
	if opts.TTL <= 0 {
		opts.TTL = runtimeDefaultTTL
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.NodeID == "" {
		opts.NodeID = defaultRuntimeNodeID()
	}
	return &memoryRuntimeEventStore{
		maxEvents: opts.MaxEvents,
		ttl:       opts.TTL,
		nodeID:    opts.NodeID,
		storeName: "memory",
		now:       opts.Now,
	}
}

func (m *memoryRuntimeEventStore) Name() string {
	if m == nil {
		return "memory"
	}
	return m.storeName
}

func (m *memoryRuntimeEventStore) NodeID() string {
	if m == nil {
		return ""
	}
	return m.nodeID
}

func (m *memoryRuntimeEventStore) Record(_ context.Context, event runtimeEvent) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	event = m.prepareEventLocked(event, m.storeName)
	m.events = append(m.events, event)
	m.trimLocked(m.now())
}

func (m *memoryRuntimeEventStore) Rows(_ context.Context, current runtimeStatus) []map[string]any {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	m.trimLocked(now)
	if m.degraded {
		current.Status = runtimeStatusDegraded
		current.Severity = "warn"
		if m.degradedSummary != "" {
			current.Summary = m.degradedSummary
		}
		if m.degradedNext != "" {
			current.NextAction = m.degradedNext
		}
	}
	rows := []map[string]any{runtimeStatusToEvent(current, m.nodeID, m.storeName, now).row()}
	for i := len(m.events) - 1; i >= 0; i-- {
		rows = append(rows, m.events[i].row())
	}
	return rows
}

func (m *memoryRuntimeEventStore) Close() error {
	return nil
}

func (m *memoryRuntimeEventStore) setDegraded(summary, next string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.degraded = true
	m.degradedSummary = summary
	m.degradedNext = next
}

func (m *memoryRuntimeEventStore) prepareEventLocked(event runtimeEvent, store string) runtimeEvent {
	if event.CreatedAt.IsZero() {
		event.CreatedAt = m.now()
	}
	if event.Kind == "" {
		event.Kind = runtimeKindEvent
	}
	if event.NodeID == "" {
		event.NodeID = m.nodeID
	}
	if event.Store == "" {
		event.Store = store
	}
	if event.ID == "" {
		m.seq++
		event.ID = fmt.Sprintf("%s:%d:%d", event.NodeID, event.CreatedAt.UnixNano(), m.seq)
	}
	return sanitizeRuntimeEvent(event)
}

func (m *memoryRuntimeEventStore) trimLocked(now time.Time) {
	if m.ttl > 0 {
		cutoff := now.Add(-m.ttl)
		start := 0
		for start < len(m.events) && m.events[start].CreatedAt.Before(cutoff) {
			start++
		}
		if start > 0 {
			copy(m.events, m.events[start:])
			m.events = m.events[:len(m.events)-start]
		}
	}
	if m.maxEvents > 0 && len(m.events) > m.maxEvents {
		copy(m.events, m.events[len(m.events)-m.maxEvents:])
		m.events = m.events[:m.maxEvents]
	}
}

func newRedisRuntimeEventStore(redisURL string, opts runtimeEventOptions) (*redisRuntimeEventStore, error) {
	redisOpts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("invalid redis URL: %w", err)
	}
	client := redis.NewClient(redisOpts)
	return newRedisRuntimeEventStoreWithClient(client, opts)
}

func newRedisRuntimeEventStoreWithClient(client redisRuntimeClient, opts runtimeEventOptions) (*redisRuntimeEventStore, error) {
	if client == nil {
		return nil, fmt.Errorf("nil redis client")
	}
	if opts.MaxEvents <= 0 {
		opts.MaxEvents = runtimeDefaultMaxEvents
	}
	if opts.TTL <= 0 {
		opts.TTL = runtimeDefaultTTL
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.NodeID == "" {
		opts.NodeID = defaultRuntimeNodeID()
	}
	ctx, cancel := context.WithTimeout(context.Background(), runtimeRedisTimeout)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("redis connection failed: %w", err)
	}
	fallback := newMemoryRuntimeEventStore(opts)
	fallback.storeName = "memory"
	return &redisRuntimeEventStore{
		client:   client,
		fallback: fallback,
		keys:     runtimeKeys(opts.Scope),
		max:      int64(opts.MaxEvents),
		ttl:      opts.TTL,
		now:      opts.Now,
	}, nil
}

func (r *redisRuntimeEventStore) Name() string {
	return "redis"
}

func (r *redisRuntimeEventStore) NodeID() string {
	if r == nil || r.fallback == nil {
		return ""
	}
	return r.fallback.NodeID()
}

func (r *redisRuntimeEventStore) Record(ctx context.Context, event runtimeEvent) {
	if r == nil || r.fallback == nil {
		return
	}
	r.fallback.Record(ctx, event)
	event = r.prepareEvent(event)
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	if err := r.withTimeout(ctx, func(qctx context.Context) error {
		if err := r.client.ZAdd(qctx, r.keys.Events, redis.Z{Score: float64(event.CreatedAt.UnixNano()), Member: string(data)}).Err(); err != nil {
			return err
		}
		cutoff := event.CreatedAt.Add(-r.ttl).UnixNano()
		_ = r.client.ZRemRangeByScore(qctx, r.keys.Events, "-inf", strconv.FormatInt(cutoff, 10)).Err()
		if r.max > 0 {
			_ = r.client.ZRemRangeByRank(qctx, r.keys.Events, 0, -r.max-1).Err()
		}
		_ = r.client.Expire(qctx, r.keys.Events, r.ttl*2).Err()
		return nil
	}); err != nil {
		r.fallback.setDegraded("Redis runtime store is unavailable; using process-local memory events.", "Check Redis connectivity before relying on cross-node runtime status.")
	}
}

func (r *redisRuntimeEventStore) Rows(ctx context.Context, current runtimeStatus) []map[string]any {
	if r == nil || r.fallback == nil {
		return nil
	}
	if err := r.writeStatus(ctx, current); err != nil {
		r.fallback.setDegraded("Redis runtime store is unavailable; using process-local memory events.", "Check Redis connectivity before relying on cross-node runtime status.")
		return r.fallback.Rows(ctx, current)
	}
	var rows []map[string]any
	statuses, err := r.readStatusRows(ctx)
	if err != nil {
		r.fallback.setDegraded("Redis runtime status rows are unavailable; using process-local memory events.", "Check Redis connectivity before relying on cross-node runtime status.")
		return r.fallback.Rows(ctx, current)
	}
	rows = append(rows, statuses...)
	events, err := r.readEventRows(ctx)
	if err != nil {
		r.fallback.setDegraded("Redis runtime event rows are unavailable; using process-local memory events.", "Check Redis connectivity before relying on cross-node runtime status.")
		return r.fallback.Rows(ctx, current)
	}
	rows = append(rows, events...)
	return rows
}

func (r *redisRuntimeEventStore) Close() error {
	if r == nil || r.client == nil {
		return nil
	}
	return r.client.Close()
}

func (r *redisRuntimeEventStore) prepareEvent(event runtimeEvent) runtimeEvent {
	if event.CreatedAt.IsZero() {
		event.CreatedAt = r.now()
	}
	if event.Kind == "" {
		event.Kind = runtimeKindEvent
	}
	if event.NodeID == "" {
		event.NodeID = r.NodeID()
	}
	event.Store = "redis"
	if event.ID == "" {
		event.ID = fmt.Sprintf("%s:%d", event.NodeID, event.CreatedAt.UnixNano())
	}
	return sanitizeRuntimeEvent(event)
}

func (r *redisRuntimeEventStore) writeStatus(ctx context.Context, current runtimeStatus) error {
	event := runtimeStatusToEvent(current, r.NodeID(), "redis", r.now())
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	key := r.keys.Statuses + cleanRuntimeKeyPart(r.NodeID())
	return r.withTimeout(ctx, func(qctx context.Context) error {
		return r.client.Set(qctx, key, string(data), r.ttl*2).Err()
	})
}

func (r *redisRuntimeEventStore) readStatusRows(ctx context.Context) ([]map[string]any, error) {
	var keys []string
	err := r.withTimeout(ctx, func(qctx context.Context) error {
		var cursor uint64
		for {
			batch, next, err := r.client.Scan(qctx, cursor, r.keys.Statuses+"*", 100).Result()
			if err != nil {
				return err
			}
			keys = append(keys, batch...)
			cursor = next
			if cursor == 0 {
				return nil
			}
		}
	})
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		var text string
		if err := r.withTimeout(ctx, func(qctx context.Context) error {
			var err error
			text, err = r.client.Get(qctx, key).Result()
			return err
		}); err != nil {
			continue
		}
		var event runtimeEvent
		if err := json.Unmarshal([]byte(text), &event); err != nil {
			continue
		}
		rows = append(rows, sanitizeRuntimeEvent(event).row())
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return fmt.Sprint(rows[i]["node_id"]) < fmt.Sprint(rows[j]["node_id"])
	})
	return rows, nil
}

func (r *redisRuntimeEventStore) readEventRows(ctx context.Context) ([]map[string]any, error) {
	limit := r.max
	if limit <= 0 {
		limit = runtimeDefaultMaxEvents
	}
	var values []string
	if err := r.withTimeout(ctx, func(qctx context.Context) error {
		var err error
		values, err = r.client.ZRevRange(qctx, r.keys.Events, 0, limit-1).Result()
		return err
	}); err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(values))
	for _, value := range values {
		var event runtimeEvent
		if err := json.Unmarshal([]byte(value), &event); err != nil {
			continue
		}
		rows = append(rows, sanitizeRuntimeEvent(event).row())
	}
	return rows, nil
}

func (r *redisRuntimeEventStore) withTimeout(ctx context.Context, fn func(context.Context) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	qctx, cancel := context.WithTimeout(ctx, runtimeRedisTimeout)
	defer cancel()
	return fn(qctx)
}

func runtimeStatusToEvent(status runtimeStatus, nodeID, store string, now time.Time) runtimeEvent {
	event := runtimeEvent{
		ID:                "status:" + nodeID,
		Kind:              runtimeKindStatus,
		CreatedAt:         now,
		NodeID:            nodeID,
		Mode:              status.Mode,
		Store:             store,
		Phase:             status.Phase,
		Status:            status.Status,
		Severity:          status.Severity,
		Summary:           status.Summary,
		NextAction:        status.NextAction,
		Source:            status.Source,
		SourceKind:        status.SourceKind,
		DatabaseName:      status.DatabaseName,
		ActiveDatabase:    status.ActiveDatabase,
		SchemaReady:       status.SchemaReady,
		TableCount:        status.TableCount,
		CatalogRevision:   status.CatalogRevision,
		Details:           status.Details,
		SuggestedNext:     status.SuggestedNext,
		SuggestedNextJSON: mustMarshalString(status.SuggestedNext),
	}
	return sanitizeRuntimeEvent(event)
}

func sanitizeRuntimeEvent(event runtimeEvent) runtimeEvent {
	event.Summary = trimRuntimeString(redactRuntimeStringValue(event.Summary), 512)
	event.NextAction = trimRuntimeString(redactRuntimeStringValue(event.NextAction), 512)
	event.ErrorCode = trimRuntimeString(event.ErrorCode, 128)
	event.Phase = trimRuntimeString(event.Phase, 128)
	event.Status = trimRuntimeString(event.Status, 64)
	event.Severity = trimRuntimeString(event.Severity, 32)
	if event.Mode == "" {
		event.Mode = modeDev
	}
	if event.Status == "" {
		event.Status = "info"
	}
	if event.Severity == "" {
		event.Severity = "info"
	}
	if event.Source == "" {
		event.Source = "graphjin"
	}
	if event.SourceKind == "" {
		event.SourceKind = sourcecap.KindGraphJin
	}
	if event.Details != nil {
		if redacted, ok := redactRuntimeValue(event.Details).(map[string]any); ok {
			event.Details = redacted
			event.DetailsJSON = mustMarshalString(redacted)
		}
	} else if event.DetailsJSON != "" {
		event.DetailsJSON = sanitizeRuntimeJSONString(event.DetailsJSON)
	}
	if event.SuggestedNext != nil {
		event.SuggestedNext = redactRuntimeStringSlice(event.SuggestedNext)
		event.SuggestedNextJSON = mustMarshalString(event.SuggestedNext)
	} else if event.SuggestedNextJSON != "" {
		event.SuggestedNextJSON = sanitizeRuntimeJSONString(event.SuggestedNextJSON)
	}
	event.DetailsJSON = trimRuntimeString(event.DetailsJSON, 8192)
	event.SuggestedNextJSON = trimRuntimeString(event.SuggestedNextJSON, 4096)
	return event
}

func (event runtimeEvent) row() map[string]any {
	event = sanitizeRuntimeEvent(event)
	created := ""
	if !event.CreatedAt.IsZero() {
		created = event.CreatedAt.UTC().Format(time.RFC3339Nano)
	}
	row := map[string]any{
		"id":                  event.ID,
		"kind":                event.Kind,
		"created_at":          created,
		"node_id":             event.NodeID,
		"mode":                event.Mode,
		"store":               event.Store,
		"phase":               event.Phase,
		"status":              event.Status,
		"severity":            event.Severity,
		"summary":             event.Summary,
		"next_action":         event.NextAction,
		"source":              event.Source,
		"source_kind":         event.SourceKind,
		"database_name":       event.DatabaseName,
		"active_database":     event.ActiveDatabase,
		"schema_ready":        event.SchemaReady,
		"table_count":         event.TableCount,
		"catalog_revision":    event.CatalogRevision,
		"duration_ms":         event.DurationMS,
		"error_code":          event.ErrorCode,
		"details_json":        event.DetailsJSON,
		"suggested_next_json": event.SuggestedNextJSON,
	}
	row["search_vector"] = runtimeSearchVector(row)
	return row
}

func runtimeSearchVector(row map[string]any) string {
	keys := []string{"kind", "mode", "store", "phase", "status", "severity", "summary", "next_action", "source", "source_kind", "database_name", "active_database", "error_code"}
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		if value := strings.TrimSpace(fmt.Sprint(row[key])); value != "" {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, " ")
}

func sanitizeRuntimeJSONString(text string) string {
	var value any
	if err := json.Unmarshal([]byte(text), &value); err != nil {
		return redactRuntimeStringValue(text)
	}
	return mustMarshalString(redactRuntimeValue(value))
}

func redactRuntimeValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			if isSensitiveRuntimeKey(key) {
				out[key] = "[REDACTED]"
				continue
			}
			out[key] = redactRuntimeValue(item)
		}
		return out
	case map[string]string:
		out := make(map[string]any, len(v))
		for key, item := range v {
			if isSensitiveRuntimeKey(key) {
				out[key] = "[REDACTED]"
				continue
			}
			out[key] = redactRuntimeStringValue(item)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i := range v {
			out[i] = redactRuntimeValue(v[i])
		}
		return out
	case []string:
		out := make([]any, len(v))
		for i := range v {
			out[i] = redactRuntimeStringValue(v[i])
		}
		return out
	case string:
		return redactRuntimeStringValue(v)
	default:
		return v
	}
}

func redactRuntimeStringValue(value string) string {
	out := trimRuntimeString(value, 2048)
	for _, pattern := range runtimeSecretPatterns {
		out = pattern.ReplaceAllString(out, "[REDACTED]")
	}
	return out
}

func redactRuntimeError(err error) string {
	if err == nil {
		return ""
	}
	return redactRuntimeStringValue(err.Error())
}

func redactRuntimeStringSlice(values []string) []string {
	out := make([]string, len(values))
	for i, value := range values {
		out[i] = redactRuntimeStringValue(value)
	}
	return out
}

func isSensitiveRuntimeKey(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	return strings.Contains(k, "password") ||
		strings.Contains(k, "secret") ||
		strings.Contains(k, "token") ||
		strings.Contains(k, "authorization") ||
		strings.Contains(k, "cookie") ||
		strings.Contains(k, "private_key") ||
		strings.Contains(k, "client_key") ||
		strings.Contains(k, "connection_string") ||
		strings.Contains(k, "conn_string") ||
		k == "sql" ||
		k == "query" ||
		k == "variables" ||
		k == "headers" ||
		k == "request_body" ||
		k == "response_body" ||
		k == "stack" ||
		k == "stack_trace"
}

func trimRuntimeString(value string, max int) string {
	value = strings.TrimSpace(value)
	if max <= 0 || len(value) <= max {
		return value
	}
	return value[:max]
}
