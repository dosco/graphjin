package serv

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	watchRedisMinLeaseTTL = 30 * time.Second
	watchRedisMinEventTTL = time.Hour
)

type watchCoordinator interface {
	NodeID() string
	Acquire(context.Context, string, string, time.Duration) (watchLease, bool, error)
	Renew(context.Context, watchLease, time.Duration) (bool, error)
	Release(context.Context, watchLease) error
	Current(context.Context, watchLease) (bool, error)
	ClaimEvent(context.Context, string, time.Duration) (bool, error)
	PublishRunnerChanged(context.Context) error
	SubscribeRunnerChanges(context.Context) <-chan struct{}
	PublishUnseen(context.Context, watchEventScope) error
	SubscribeUnseen(context.Context) <-chan watchEventScope
	Close() error
}

type watchLease struct {
	WatchID    string
	RuntimeKey string
	NodeID     string
	Fence      int64
	TTL        time.Duration
}

func (l watchLease) valid() bool {
	return strings.TrimSpace(l.WatchID) != "" && strings.TrimSpace(l.NodeID) != "" && l.Fence > 0
}

func (l watchLease) value() string {
	return strings.Join([]string{l.NodeID, l.RuntimeKey, strconv.FormatInt(l.Fence, 10)}, "|")
}

type redisWatchCoordinator struct {
	client *redis.Client
	prefix string
	nodeID string
}

func newRedisWatchCoordinator(redisURL string, conf *Config, metadataDB string, namespace *string) (*redisWatchCoordinator, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("invalid redis URL: %w", err)
	}
	client := redis.NewClient(opts)
	ctx, cancel := context.WithTimeout(context.Background(), runtimeRedisTimeout)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("redis connection failed: %w", err)
	}
	scope := runtimeScope(conf, metadataDB, namespace)
	return &redisWatchCoordinator{
		client: client,
		prefix: "gj:watch:" + cleanRuntimeScope(scope),
		nodeID: defaultRuntimeNodeID(),
	}, nil
}

func (r *redisWatchCoordinator) NodeID() string {
	if r == nil {
		return ""
	}
	return r.nodeID
}

func (r *redisWatchCoordinator) Acquire(ctx context.Context, watchID, runtimeKey string, ttl time.Duration) (watchLease, bool, error) {
	ttl = normalizeWatchLeaseTTL(ttl)
	lease := watchLease{WatchID: watchID, RuntimeKey: runtimeKey, NodeID: r.NodeID(), TTL: ttl}
	if r == nil || r.client == nil || strings.TrimSpace(watchID) == "" {
		return lease, false, nil
	}
	keys := []string{r.leaseKey(watchID), r.fenceKey(watchID)}
	args := []any{lease.NodeID, runtimeKey, int64(ttl / time.Millisecond)}
	qctx, cancel := watchRedisContext(ctx)
	defer cancel()
	val, err := r.client.Eval(qctx, redisWatchAcquireScript, keys, args...).Int64()
	if err != nil {
		return lease, false, err
	}
	if val <= 0 {
		return lease, false, nil
	}
	lease.Fence = val
	return lease, true, nil
}

func (r *redisWatchCoordinator) Renew(ctx context.Context, lease watchLease, ttl time.Duration) (bool, error) {
	if r == nil || r.client == nil || !lease.valid() {
		return false, nil
	}
	ttl = normalizeWatchLeaseTTL(ttl)
	qctx, cancel := watchRedisContext(ctx)
	defer cancel()
	val, err := r.client.Eval(qctx, redisWatchRenewScript, []string{r.leaseKey(lease.WatchID)}, lease.value(), int64(ttl/time.Millisecond)).Int64()
	return val == 1, err
}

func (r *redisWatchCoordinator) Release(ctx context.Context, lease watchLease) error {
	if r == nil || r.client == nil || !lease.valid() {
		return nil
	}
	qctx, cancel := watchRedisContext(ctx)
	defer cancel()
	_, err := r.client.Eval(qctx, redisWatchReleaseScript, []string{r.leaseKey(lease.WatchID)}, lease.value()).Result()
	return err
}

func (r *redisWatchCoordinator) Current(ctx context.Context, lease watchLease) (bool, error) {
	if r == nil || r.client == nil || !lease.valid() {
		return true, nil
	}
	qctx, cancel := watchRedisContext(ctx)
	defer cancel()
	value, err := r.client.Get(qctx, r.leaseKey(lease.WatchID)).Result()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return value == lease.value(), nil
}

func (r *redisWatchCoordinator) ClaimEvent(ctx context.Context, eventID string, ttl time.Duration) (bool, error) {
	if r == nil || r.client == nil || strings.TrimSpace(eventID) == "" {
		return true, nil
	}
	ttl = normalizeWatchEventTTL(ttl)
	qctx, cancel := watchRedisContext(ctx)
	defer cancel()
	ok, err := r.client.SetNX(qctx, r.eventKey(eventID), "1", ttl).Result()
	return ok, err
}

func (r *redisWatchCoordinator) PublishRunnerChanged(ctx context.Context) error {
	if r == nil || r.client == nil {
		return nil
	}
	qctx, cancel := watchRedisContext(ctx)
	defer cancel()
	return r.client.Publish(qctx, r.runnerChannel(), r.nodeID).Err()
}

func (r *redisWatchCoordinator) SubscribeRunnerChanges(ctx context.Context) <-chan struct{} {
	out := make(chan struct{}, 1)
	if r == nil || r.client == nil {
		close(out)
		return out
	}
	pubsub := r.client.Subscribe(ctx, r.runnerChannel())
	go func() {
		defer close(out)
		defer pubsub.Close() //nolint:errcheck
		for {
			msg, err := pubsub.ReceiveMessage(ctx)
			if err != nil {
				return
			}
			if msg.Payload == r.nodeID {
				continue
			}
			select {
			case out <- struct{}{}:
			default:
			}
		}
	}()
	return out
}

func (r *redisWatchCoordinator) PublishUnseen(ctx context.Context, scope watchEventScope) error {
	if r == nil || r.client == nil || strings.TrimSpace(scope.OwnerID) == "" {
		return nil
	}
	scope.SourceNodeID = r.nodeID
	data, err := json.Marshal(scope)
	if err != nil {
		return err
	}
	qctx, cancel := watchRedisContext(ctx)
	defer cancel()
	return r.client.Publish(qctx, r.unseenChannel(), string(data)).Err()
}

func (r *redisWatchCoordinator) SubscribeUnseen(ctx context.Context) <-chan watchEventScope {
	out := make(chan watchEventScope, 8)
	if r == nil || r.client == nil {
		close(out)
		return out
	}
	pubsub := r.client.Subscribe(ctx, r.unseenChannel())
	go func() {
		defer close(out)
		defer pubsub.Close() //nolint:errcheck
		for {
			msg, err := pubsub.ReceiveMessage(ctx)
			if err != nil {
				return
			}
			var scope watchEventScope
			if err := json.Unmarshal([]byte(msg.Payload), &scope); err != nil {
				continue
			}
			if scope.SourceNodeID == r.nodeID || strings.TrimSpace(scope.OwnerID) == "" {
				continue
			}
			select {
			case out <- scope:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}

func (r *redisWatchCoordinator) Close() error {
	if r == nil || r.client == nil {
		return nil
	}
	return r.client.Close()
}

func (r *redisWatchCoordinator) leaseKey(watchID string) string {
	return r.prefix + ":lease:" + cleanRuntimeKeyPart(watchID)
}

func (r *redisWatchCoordinator) fenceKey(watchID string) string {
	return r.prefix + ":fence:" + cleanRuntimeKeyPart(watchID)
}

func (r *redisWatchCoordinator) eventKey(eventID string) string {
	return r.prefix + ":event:" + cleanRuntimeKeyPart(eventID)
}

func (r *redisWatchCoordinator) runnerChannel() string {
	return r.prefix + ":changed"
}

func (r *redisWatchCoordinator) unseenChannel() string {
	return r.prefix + ":unseen"
}

func watchRedisContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(ctx, runtimeRedisTimeout)
}

func normalizeWatchLeaseTTL(ttl time.Duration) time.Duration {
	if ttl < watchRedisMinLeaseTTL {
		return watchRedisMinLeaseTTL
	}
	return ttl
}

func normalizeWatchEventTTL(ttl time.Duration) time.Duration {
	if ttl < watchRedisMinEventTTL {
		return watchRedisMinEventTTL
	}
	return ttl
}

func (s *graphjinService) ensureWatchCoordinator(ctx context.Context) watchCoordinator {
	if s == nil || s.conf == nil || strings.TrimSpace(s.conf.Redis.URL) == "" {
		return nil
	}
	s.watchCoordMu.Lock()
	if s.watchCoord != nil {
		coord := s.watchCoord
		s.watchCoordMu.Unlock()
		return coord
	}
	coord, err := newRedisWatchCoordinator(s.conf.Redis.URL, s.conf, s.metadataDB, s.namespace)
	if err != nil {
		s.watchCoordMu.Unlock()
		s.recordRuntimeEvent(context.Background(), runtimeEvent{
			Phase:      "watch",
			Status:     runtimeStatusDegraded,
			Severity:   "warn",
			Summary:    "Redis watch coordination is unavailable; watch runners use process-local behavior.",
			NextAction: "Check redis.url connectivity before relying on horizontally scaled watch efficiency.",
			ErrorCode:  "watch_redis_unavailable",
			Details:    map[string]any{"error": err.Error()},
		})
		return nil
	}
	s.watchCoord = coord
	s.addCloseFn(func() {
		_ = coord.Close()
	})
	s.watchCoordMu.Unlock()
	go s.watchUnseenFanoutLoop(ctx, coord)
	s.recordRuntimeEvent(context.Background(), runtimeEvent{
		Phase:      "watch",
		Status:     runtimeStatusReady,
		Severity:   "info",
		Summary:    "Redis watch coordination is enabled.",
		NextAction: "Use cursor-backed watches normally; Redis coordinates runners and MCP wakeups across replicas.",
		Details:    map[string]any{"node_id": coord.NodeID()},
	})
	return coord
}

func (s *graphjinService) startWatchCoordinator(parent context.Context) {
	if s == nil || s.conf == nil || !s.watchesEnabled() {
		return
	}
	ctx := parent
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)
	if coord := s.ensureWatchCoordinator(ctx); coord == nil {
		cancel()
		return
	}
	s.addCloseFn(cancel)
}

func (s *graphjinService) currentWatchCoordinator() watchCoordinator {
	if s == nil {
		return nil
	}
	s.watchCoordMu.Lock()
	defer s.watchCoordMu.Unlock()
	return s.watchCoord
}

func (s *graphjinService) watchLeaseTTL() time.Duration {
	seconds := 0
	if s != nil && s.conf != nil {
		seconds = s.conf.Core.EffectiveArtifactsConfig().PollSeconds
	}
	ttl := time.Duration(seconds*3) * time.Second
	return normalizeWatchLeaseTTL(ttl)
}

func (s *graphjinService) watchEventDedupeTTL() time.Duration {
	hours := 0
	if s != nil && s.conf != nil {
		hours = s.conf.Core.EffectiveWatchesConfig().EventRetentionHours
	}
	ttl := time.Duration(hours+1) * time.Hour
	return normalizeWatchEventTTL(ttl)
}

func (s *graphjinService) publishWatchRunnerChanged(ctx context.Context) {
	if coord := s.currentWatchCoordinator(); coord != nil {
		if err := coord.PublishRunnerChanged(ctx); err != nil {
			s.recordWatchRunnerError("publish watch runner change", err, nil)
		}
	}
}

func (s *graphjinService) watchUnseenFanoutLoop(ctx context.Context, coord watchCoordinator) {
	if s == nil || coord == nil {
		return
	}
	for scope := range coord.SubscribeUnseen(ctx) {
		if scope.SourceNodeID == coord.NodeID() {
			continue
		}
		s.notifyWatchEventsResourceScope(scope, false)
	}
}

const redisWatchAcquireScript = `
local ok = redis.call("set", KEYS[1], ARGV[1] .. "|" .. ARGV[2] .. "|0", "NX", "PX", ARGV[3])
if not ok then
	return 0
end
local fence = redis.call("incr", KEYS[2])
redis.call("set", KEYS[1], ARGV[1] .. "|" .. ARGV[2] .. "|" .. fence, "XX", "PX", ARGV[3])
return fence
`

const redisWatchRenewScript = `
if redis.call("get", KEYS[1]) == ARGV[1] then
	redis.call("pexpire", KEYS[1], ARGV[2])
	return 1
end
return 0
`

const redisWatchReleaseScript = `
if redis.call("get", KEYS[1]) == ARGV[1] then
	return redis.call("del", KEYS[1])
end
return 0
`
