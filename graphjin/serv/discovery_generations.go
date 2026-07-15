package serv

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dosco/graphjin/core/v3"
	"github.com/redis/go-redis/v9"
)

const discoveryGenerationFormatVersion = 1

type discoveryGenerationFile struct {
	Name   string `json:"name"`
	Size   int    `json:"size"`
	SHA256 string `json:"sha256"`
}

type discoveryGenerationManifest struct {
	FormatVersion   int                       `json:"format_version"`
	GenerationID    string                    `json:"generation_id"`
	CreatedAt       time.Time                 `json:"created_at"`
	Fingerprint     string                    `json:"fingerprint"`
	CatalogRevision string                    `json:"catalog_revision"`
	SourceRevisions map[string]string         `json:"source_revisions"`
	Files           []discoveryGenerationFile `json:"files"`
	unchanged       bool                      `json:"-"`
}

type discoveryActivationReceipt struct {
	FormatVersion int       `json:"format_version"`
	GenerationID  string    `json:"generation_id"`
	Fingerprint   string    `json:"fingerprint"`
	Fence         int64     `json:"fence"`
	ActivatedAt   time.Time `json:"activated_at"`
}

type discoveryLease struct {
	fence int64
	value string
}

type discoveryGenerationManager struct {
	service     *graphjinService
	conf        DiscoveryCacheConfig
	base        string
	fingerprint string
	prefix      string
	nodeID      string
	redis       *redis.Client
	redisErr    error

	mu       sync.Mutex
	activeID string
	degraded bool
	cancel   context.CancelFunc
}

var localDiscoveryBuildMu sync.Mutex

func newDiscoveryGenerationManager(s *graphjinService) (*discoveryGenerationManager, error) {
	if s == nil || s.conf == nil || s.fs == nil {
		return nil, errors.New("discovery cache requires initialized service configuration and filesystem")
	}
	fingerprint, err := discoveryConfigFingerprint(s.conf)
	if err != nil {
		return nil, err
	}
	m := &discoveryGenerationManager{
		service:     s,
		conf:        s.conf.DiscoveryCache,
		base:        path.Clean(s.conf.DiscoveryCache.Path),
		fingerprint: fingerprint,
		prefix:      "gj:discovery:" + cleanRuntimeScope(runtimeScope(s.conf, s.metadataDB, s.namespace)) + ":" + fingerprint[:16],
		nodeID:      defaultRuntimeNodeID(),
	}
	if rawURL := strings.TrimSpace(s.conf.Redis.URL); rawURL != "" {
		opts, err := redis.ParseURL(rawURL)
		if err != nil {
			m.redisErr = fmt.Errorf("invalid redis URL: %w", err)
			return m, nil
		}
		m.redis = redis.NewClient(opts)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := m.redis.Ping(ctx).Err(); err != nil {
			m.redisErr = fmt.Errorf("redis connection failed: %w", err)
			_ = m.redis.Close()
			m.redis = nil
		}
	}
	return m, nil
}

// InitialGeneration returns a validated generation for cache-first startup.
// With Redis, only the fenced lease holder performs live discovery. Without
// Redis, the process mutex provides the same guarantee for embedded instances.
func (m *discoveryGenerationManager) InitialGeneration(ctx context.Context, coreConf *core.Config, opts []core.Option) (string, error) {
	if m == nil {
		return "", errors.New("nil discovery generation manager")
	}

	active, activeErr := m.redisActive(ctx)
	activeUnavailable := false
	if active != "" {
		if _, err := m.loadGeneration(active); err == nil {
			m.setDiscoveryState(active, false)
			return m.generationDir(active), nil
		} else {
			activeErr = fmt.Errorf("active discovery generation %q is unavailable: %w", active, err)
			activeUnavailable = true
			m.reportDiscoveryDegraded(activeErr)
		}
	}
	if receipt, err := m.newestReceipt(); err == nil && receipt.GenerationID != "" {
		m.setDiscoveryState(receipt.GenerationID, false)
		if m.redisErr != nil || activeErr != nil {
			m.reportDiscoveryDegraded(fmt.Errorf("starting from stale filesystem discovery generation: %w", firstNonNilError(m.redisErr, activeErr)))
		}
		return m.generationDir(receipt.GenerationID), nil
	}

	if m.redisErr != nil {
		wait := m.conf.StartupWait
		if wait > 0 {
			timer := time.NewTimer(wait)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-timer.C:
			}
		}
		return "", fmt.Errorf("discovery coordination unavailable and no valid filesystem generation exists: %w", m.redisErr)
	}
	if activeUnavailable {
		deadline := time.Now().Add(m.conf.StartupWait)
		for {
			candidate, err := m.redisActive(ctx)
			if err == nil && candidate != "" {
				if _, loadErr := m.loadGeneration(candidate); loadErr == nil {
					m.setDiscoveryState(candidate, false)
					return m.generationDir(candidate), nil
				}
			}
			if m.conf.StartupWait <= 0 || time.Now().After(deadline) {
				return "", fmt.Errorf("active discovery generation is not readable from this replica's filesystem after %s: %w", m.conf.StartupWait, activeErr)
			}
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(250 * time.Millisecond):
			}
		}
	}

	if m.redis == nil {
		localDiscoveryBuildMu.Lock()
		defer localDiscoveryBuildMu.Unlock()
		if receipt, err := m.newestReceipt(); err == nil && receipt.GenerationID != "" {
			m.setDiscoveryState(receipt.GenerationID, false)
			return m.generationDir(receipt.GenerationID), nil
		}
		manifest, err := m.buildGeneration(ctx, coreConf, opts)
		if err != nil {
			return "", err
		}
		fence := time.Now().UnixNano()
		if err := m.writeReceipt(manifest, fence); err != nil {
			return "", err
		}
		m.setDiscoveryState(manifest.GenerationID, false)
		m.cleanupOldGenerations()
		return m.generationDir(manifest.GenerationID), nil
	}

	deadline := time.Now().Add(m.conf.StartupWait)
	pubsub := m.redis.Subscribe(ctx, m.prefix+":events")
	defer pubsub.Close() //nolint:errcheck
	messages := pubsub.Channel()
	for {
		active, err := m.redisActive(ctx)
		if err == nil && active != "" {
			if _, loadErr := m.loadGeneration(active); loadErr == nil {
				m.setDiscoveryState(active, false)
				return m.generationDir(active), nil
			}
		}
		lease, won, err := m.acquire(ctx, 30*time.Second)
		if err != nil {
			return "", fmt.Errorf("acquire discovery lease: %w", err)
		}
		if won {
			m.setBuilderStatus(ctx, lease, "building", "cold_start")
			manifest, err := m.buildWithRenewal(ctx, coreConf, opts, lease)
			if err != nil {
				m.publishFailure(ctx, err)
				_ = m.release(context.Background(), lease)
				return "", err
			}
			if err := m.activate(ctx, lease, manifest); err != nil {
				m.publishFailure(ctx, err)
				_ = m.release(context.Background(), lease)
				return "", err
			}
			_ = m.release(context.Background(), lease)
			m.setDiscoveryState(manifest.GenerationID, false)
			m.cleanupOldGenerations()
			return m.generationDir(manifest.GenerationID), nil
		}

		if m.conf.StartupWait <= 0 || time.Now().After(deadline) {
			return "", fmt.Errorf("timed out after %s waiting for coordinated discovery activation", m.conf.StartupWait)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-messages:
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func (m *discoveryGenerationManager) Start() {
	if m == nil || m.redisErr != nil || m.conf.RefreshInterval <= 0 {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	go m.refreshLoop(ctx)
	go m.refreshOnce(ctx)
}

func (m *discoveryGenerationManager) Close() {
	if m == nil {
		return
	}
	if m.cancel != nil {
		m.cancel()
	}
	if m.redis != nil {
		_ = m.redis.Close()
	}
}

func (m *discoveryGenerationManager) refreshLoop(ctx context.Context) {
	ticker := time.NewTicker(m.conf.RefreshInterval)
	defer ticker.Stop()
	var pubsub *redis.PubSub
	var messages <-chan *redis.Message
	if m.redis != nil {
		pubsub = m.redis.Subscribe(ctx, m.prefix+":events")
		defer pubsub.Close() //nolint:errcheck
		messages = pubsub.Channel()
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.refreshOnce(ctx)
		case msg := <-messages:
			if msg != nil && msg.Payload != m.currentDiscoveryID() {
				m.loadActivated(msg.Payload)
			}
		}
	}
}

func (m *discoveryGenerationManager) refreshOnce(ctx context.Context) {
	if m == nil || m.service == nil || m.service.gj == nil {
		return
	}
	if current, err := m.loadGeneration(m.currentDiscoveryID()); err == nil &&
		m.conf.RefreshInterval > 0 && time.Since(current.CreatedAt) < m.conf.RefreshInterval {
		return
	}
	if active, err := m.redisActive(ctx); err == nil && active != "" && active != m.currentDiscoveryID() {
		m.loadActivated(active)
		return
	}
	lease, won, err := m.acquire(ctx, 30*time.Second)
	if err != nil || !won {
		if active, getErr := m.redisActive(ctx); getErr == nil && active != "" && active != m.currentDiscoveryID() {
			m.loadActivated(active)
		}
		return
	}
	defer m.release(context.Background(), lease) //nolint:errcheck
	m.setBuilderStatus(ctx, lease, "building", "background_refresh")
	coreConf := &m.service.conf.Core
	if m.service.runtimeCore != nil {
		coreConf = m.service.runtimeCore
	}
	manifest, err := m.buildWithRenewal(ctx, coreConf, m.service.buildCoreOptions(), lease)
	if err != nil {
		m.publishFailure(ctx, err)
		return
	}
	if current, err := m.loadGeneration(m.currentDiscoveryID()); err == nil && sameSourceRevisions(current.SourceRevisions, manifest.SourceRevisions) {
		return
	}
	if err := m.activate(ctx, lease, manifest); err != nil {
		m.publishFailure(ctx, err)
		return
	}
	m.loadActivated(manifest.GenerationID)
	m.cleanupOldGenerations()
}

// RefreshNow runs explicit schema reloads through the same fenced generation
// path used by startup and background refresh. Followers wait for the winning
// activation and then atomically reload their stable GraphJin pointer.
func (m *discoveryGenerationManager) RefreshNow(ctx context.Context) error {
	if m == nil || m.service == nil || m.service.gj == nil {
		return errors.New("discovery cache is not initialized")
	}
	if m.redisErr != nil {
		m.reportDiscoveryDegraded(m.redisErr)
		return fmt.Errorf("discovery refresh suspended while Redis coordination is unavailable: %w", m.redisErr)
	}
	if m.redis != nil {
		qctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		_ = m.redis.Incr(qctx, m.prefix+":dirty").Err()
		cancel()
	}
	coreConf := &m.service.conf.Core
	if m.service.runtimeCore != nil {
		coreConf = m.service.runtimeCore
	}
	if m.redis == nil {
		localDiscoveryBuildMu.Lock()
		defer localDiscoveryBuildMu.Unlock()
		manifest, err := m.buildGeneration(ctx, coreConf, m.service.buildCoreOptions())
		if err != nil {
			return err
		}
		if current, err := m.loadGeneration(m.currentDiscoveryID()); err == nil && sameSourceRevisions(current.SourceRevisions, manifest.SourceRevisions) {
			return nil
		}
		if err := m.writeReceipt(manifest, time.Now().UnixNano()); err != nil {
			return err
		}
		m.loadActivated(manifest.GenerationID)
		m.cleanupOldGenerations()
		return nil
	}

	previous := m.currentDiscoveryID()
	deadline := time.Now().Add(m.conf.StartupWait)
	for {
		active, err := m.redisActive(ctx)
		if err == nil && active != "" && active != previous {
			m.loadActivated(active)
			if m.currentDiscoveryID() == active {
				return nil
			}
			return fmt.Errorf("active discovery generation %q is not readable from this replica's shared filesystem", active)
		}
		lease, won, err := m.acquire(ctx, 30*time.Second)
		if err != nil {
			return err
		}
		if won {
			m.setBuilderStatus(ctx, lease, "building", "explicit_refresh")
			manifest, err := m.buildWithRenewal(ctx, coreConf, m.service.buildCoreOptions(), lease)
			if err == nil {
				err = m.activate(ctx, lease, manifest)
			}
			_ = m.release(context.Background(), lease)
			if err != nil {
				return err
			}
			m.loadActivated(manifest.GenerationID)
			m.cleanupOldGenerations()
			return nil
		}
		if m.conf.StartupWait <= 0 || time.Now().After(deadline) {
			return fmt.Errorf("timed out after %s waiting for discovery refresh", m.conf.StartupWait)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// reconfigureDiscoveryAfterConfigChange moves runtime config changes onto the
// fingerprint-scoped coordination namespace and waits for its activated cache
// generation. The staged runtime remains the validation boundary; replicas
// converge on the fenced filesystem generation before the update returns.
func (s *graphjinService) reconfigureDiscoveryAfterConfigChange(ctx context.Context) error {
	if s == nil || s.discovery == nil || s.conf == nil || !s.conf.DiscoveryCache.enabled() {
		return nil
	}
	next, err := newDiscoveryGenerationManager(s)
	if err != nil {
		return err
	}
	previous := s.discovery
	previous.Close()
	s.discovery = next
	if err := next.RefreshNow(ctx); err != nil {
		return err
	}
	next.Start()
	return nil
}

func (m *discoveryGenerationManager) loadActivated(id string) {
	if id == "" || id == m.currentDiscoveryID() || m.service == nil || m.service.gj == nil {
		return
	}
	if _, err := m.loadGeneration(id); err != nil {
		m.reportDiscoveryDegraded(fmt.Errorf("activated discovery generation %q is not readable from the shared filesystem: %w", id, err))
		return
	}
	if err := m.service.gj.ReloadFromRuntimeSchemaCache(m.generationDir(id)); err != nil {
		m.reportDiscoveryDegraded(fmt.Errorf("activated discovery generation %q could not be loaded: %w", id, err))
		return
	}
	m.setDiscoveryState(id, false)
	m.service.invalidateCatalogCache()
	if err := m.service.refreshMetadataGraph(); err != nil && m.service.log != nil {
		m.service.log.Warnf("metadata refresh after discovery activation failed: %s", redactRuntimeError(err))
	}
	if m.service.semantic != nil {
		m.service.semantic.CatalogChanged()
	}
}

func (m *discoveryGenerationManager) buildWithRenewal(ctx context.Context, coreConf *core.Config, opts []core.Option, lease discoveryLease) (discoveryGenerationManifest, error) {
	buildCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	lost := make(chan struct{}, 1)
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-buildCtx.Done():
				return
			case <-ticker.C:
				ok, err := m.renew(buildCtx, lease, 30*time.Second)
				if err != nil || !ok {
					select {
					case lost <- struct{}{}:
					default:
					}
					cancel()
					return
				}
			}
		}
	}()
	manifest, err := m.buildGeneration(buildCtx, coreConf, opts)
	select {
	case <-lost:
		return discoveryGenerationManifest{}, errors.New("discovery lease lost while building generation")
	default:
	}
	return manifest, err
}

func (m *discoveryGenerationManager) currentDiscoveryID() string {
	if m == nil {
		return ""
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.activeID
}

func (m *discoveryGenerationManager) setDiscoveryState(id string, degraded bool) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.activeID = id
	m.degraded = degraded
	m.mu.Unlock()
}

func (m *discoveryGenerationManager) setDiscoveryDegraded(degraded bool) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.degraded = degraded
	m.mu.Unlock()
}

func (m *discoveryGenerationManager) reportDiscoveryDegraded(cause error) {
	m.setDiscoveryDegraded(true)
	if m != nil && m.service != nil && m.service.log != nil && cause != nil {
		m.service.log.Warnf("discovery cache degraded: %s", redactRuntimeError(cause))
	}
}

func firstNonNilError(values ...error) error {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return errors.New("unknown discovery degradation")
}

func (m *discoveryGenerationManager) buildGeneration(ctx context.Context, coreConf *core.Config, opts []core.Option) (discoveryGenerationManifest, error) {
	if err := ctx.Err(); err != nil {
		return discoveryGenerationManifest{}, err
	}
	id, err := newDiscoveryGenerationID()
	if err != nil {
		return discoveryGenerationManifest{}, err
	}
	dir := m.generationDir(id)
	liveOpts := append([]core.Option{}, opts...)
	liveOpts = append(liveOpts,
		core.OptionSetDBSchemaWatcherDisabled(true),
		core.OptionSetRuntimeSchemaDDLDir(dir),
	)
	live, err := core.NewGraphJin(coreConf, m.service.anyDB(), liveOpts...)
	if err != nil {
		return discoveryGenerationManifest{}, fmt.Errorf("live discovery for generation %s: %w", id, err)
	}
	defer live.Close()
	liveCatalog, err := live.CatalogSnapshot()
	if err != nil {
		return discoveryGenerationManifest{}, fmt.Errorf("catalog revision for generation %s: %w", id, err)
	}

	manifest := discoveryGenerationManifest{
		FormatVersion:   discoveryGenerationFormatVersion,
		GenerationID:    id,
		CreatedAt:       time.Now().UTC(),
		Fingerprint:     m.fingerprint,
		CatalogRevision: liveCatalog.Revision,
		SourceRevisions: make(map[string]string),
	}
	for _, source := range live.DatabaseNames() {
		snapshotName := path.Base(core.RuntimeSchemaSnapshotPath(source))
		snapshotPath := path.Join(dir, snapshotName)
		if ok, _ := m.service.fs.Exists(snapshotPath); !ok {
			// Logical GraphJin, workflow, code, and managed system sources can
			// participate in the finalized engine without live DB introspection.
			// Only database sources produce full schema snapshot files.
			if configured, found := coreConf.SourceByName(source); found && configured.CanonicalKind() != "database" {
				continue
			}
			return discoveryGenerationManifest{}, fmt.Errorf("full schema snapshot for database source %q is missing", source)
		}
		file, err := m.generationFile(dir, snapshotName)
		if err != nil {
			return discoveryGenerationManifest{}, fmt.Errorf("full schema snapshot for source %q: %w", source, err)
		}
		manifest.Files = append(manifest.Files, file)
		manifest.SourceRevisions[source] = file.SHA256
		ddlName := path.Base(core.RuntimeSchemaDDLPath(source))
		if ok, _ := m.service.fs.Exists(path.Join(dir, ddlName)); ok {
			ddl, err := m.generationFile(dir, ddlName)
			if err != nil {
				return discoveryGenerationManifest{}, err
			}
			manifest.Files = append(manifest.Files, ddl)
		}
	}
	sort.Slice(manifest.Files, func(i, j int) bool { return manifest.Files[i].Name < manifest.Files[j].Name })
	for n := 1; n < len(manifest.Files); n++ {
		if manifest.Files[n-1].Name == manifest.Files[n].Name {
			return discoveryGenerationManifest{}, fmt.Errorf("database source names map to duplicate discovery filename %q", manifest.Files[n].Name)
		}
	}
	if currentID := m.currentDiscoveryID(); currentID != "" {
		if current, err := m.loadGeneration(currentID); err == nil &&
			current.CatalogRevision == manifest.CatalogRevision &&
			sameSourceRevisions(current.SourceRevisions, manifest.SourceRevisions) {
			m.discardUnactivatedFiles(manifest)
			current.unchanged = true
			return current, nil
		}
	}

	cachedOpts := append([]core.Option{}, opts...)
	cachedOpts = append(cachedOpts,
		core.OptionSetDBSchemaWatcherDisabled(true),
		core.OptionSetRuntimeSchemaDDLDir(dir),
		core.OptionSetRuntimeSchemaCacheFirst(true),
		core.OptionSetRuntimeSchemaCacheRequired(true),
	)
	cached, err := core.NewGraphJin(coreConf, m.service.anyDB(), cachedOpts...)
	if err != nil {
		return discoveryGenerationManifest{}, fmt.Errorf("validate cache-loaded generation %s: %w", id, err)
	}
	cachedCatalog, err := cached.CatalogSnapshot()
	cached.Close()
	if err != nil {
		return discoveryGenerationManifest{}, fmt.Errorf("validate catalog generation %s: %w", id, err)
	}
	if cachedCatalog.Revision != liveCatalog.Revision {
		return discoveryGenerationManifest{}, fmt.Errorf("generation %s revision mismatch: live %s cache %s; live sources=%v cache sources=%v", id, liveCatalog.Revision, cachedCatalog.Revision, liveCatalog.SourceRevisions, cachedCatalog.SourceRevisions)
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return discoveryGenerationManifest{}, err
	}
	// The manifest is deliberately written last. A partial generation cannot
	// pass loadGeneration even if some schema files are already visible.
	if err := m.service.fs.Put(m.manifestPath(id), data); err != nil {
		return discoveryGenerationManifest{}, fmt.Errorf("write discovery manifest: %w", err)
	}
	return manifest, nil
}

func (m *discoveryGenerationManager) discardUnactivatedFiles(manifest discoveryGenerationManifest) {
	deleter, ok := m.service.fs.(interface{ Delete(string) error })
	if !ok {
		return
	}
	for _, file := range manifest.Files {
		_ = deleter.Delete(path.Join(m.generationDir(manifest.GenerationID), file.Name))
	}
}

func (m *discoveryGenerationManager) generationFile(dir, name string) (discoveryGenerationFile, error) {
	data, err := m.service.fs.Get(path.Join(dir, name))
	if err != nil {
		return discoveryGenerationFile{}, err
	}
	sum := sha256.Sum256(data)
	return discoveryGenerationFile{Name: name, Size: len(data), SHA256: hex.EncodeToString(sum[:])}, nil
}

func (m *discoveryGenerationManager) loadGeneration(id string) (discoveryGenerationManifest, error) {
	var manifest discoveryGenerationManifest
	if strings.TrimSpace(id) == "" || strings.ContainsAny(id, `/\\`) {
		return manifest, errors.New("invalid discovery generation ID")
	}
	data, err := m.service.fs.Get(m.manifestPath(id))
	if err != nil {
		return manifest, err
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return manifest, err
	}
	if manifest.FormatVersion != discoveryGenerationFormatVersion || manifest.GenerationID != id || manifest.Fingerprint != m.fingerprint {
		return manifest, errors.New("incompatible discovery generation manifest")
	}
	schemaRevisions := make(map[string]int, len(manifest.SourceRevisions))
	seenFiles := make(map[string]struct{}, len(manifest.Files))
	for _, file := range manifest.Files {
		if file.Name == "" || path.Base(file.Name) != file.Name {
			return manifest, fmt.Errorf("invalid discovery filename %q", file.Name)
		}
		if _, exists := seenFiles[file.Name]; exists {
			return manifest, fmt.Errorf("duplicate discovery filename %q", file.Name)
		}
		seenFiles[file.Name] = struct{}{}
		data, err := m.service.fs.Get(path.Join(m.generationDir(id), file.Name))
		if err != nil {
			return manifest, err
		}
		if len(data) != file.Size {
			return manifest, fmt.Errorf("discovery file %s size mismatch", file.Name)
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != file.SHA256 {
			return manifest, fmt.Errorf("discovery file %s checksum mismatch", file.Name)
		}
		if strings.HasSuffix(file.Name, ".schema.json") {
			schemaRevisions[file.SHA256]++
		}
	}
	for source, revision := range manifest.SourceRevisions {
		if revision == "" || schemaRevisions[revision] == 0 {
			return manifest, fmt.Errorf("source %q has no matching full schema snapshot", source)
		}
		schemaRevisions[revision]--
	}
	for _, unmatched := range schemaRevisions {
		if unmatched != 0 {
			return manifest, errors.New("discovery manifest contains an unreferenced full schema snapshot")
		}
	}
	return manifest, nil
}

func (m *discoveryGenerationManager) newestReceipt() (discoveryActivationReceipt, error) {
	var out discoveryActivationReceipt
	entries, err := m.service.fs.List(path.Join(m.base, "activations"))
	if err != nil {
		return out, err
	}
	sort.Sort(sort.Reverse(sort.StringSlice(entries)))
	for _, name := range entries {
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		data, err := m.service.fs.Get(path.Join(m.base, "activations", name))
		if err != nil || json.Unmarshal(data, &out) != nil ||
			out.FormatVersion != discoveryGenerationFormatVersion || out.Fingerprint != m.fingerprint {
			continue
		}
		if _, err := m.loadGeneration(out.GenerationID); err == nil {
			return out, nil
		}
	}
	return discoveryActivationReceipt{}, errors.New("no valid discovery activation receipt")
}

func (m *discoveryGenerationManager) writeReceipt(manifest discoveryGenerationManifest, fence int64) error {
	receipt := discoveryActivationReceipt{
		FormatVersion: discoveryGenerationFormatVersion,
		GenerationID:  manifest.GenerationID,
		Fingerprint:   manifest.Fingerprint,
		Fence:         fence,
		ActivatedAt:   time.Now().UTC(),
	}
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return err
	}
	return m.service.fs.Put(m.receiptPath(manifest.GenerationID), data)
}

func (m *discoveryGenerationManager) generationDir(id string) string {
	return path.Join(m.base, "generations", id)
}

func (m *discoveryGenerationManager) manifestPath(id string) string {
	return path.Join(m.generationDir(id), "manifest.json")
}

func (m *discoveryGenerationManager) receiptPath(id string) string {
	return path.Join(m.base, "activations", id+".json")
}

func (m *discoveryGenerationManager) redisActive(ctx context.Context) (string, error) {
	if m.redisErr != nil {
		return "", m.redisErr
	}
	if m.redis == nil {
		return "", nil
	}
	qctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	value, err := m.redis.Get(qctx, m.prefix+":active").Result()
	if err == redis.Nil {
		return "", nil
	}
	return value, err
}

func (m *discoveryGenerationManager) acquire(ctx context.Context, ttl time.Duration) (discoveryLease, bool, error) {
	if m.redis == nil {
		return discoveryLease{fence: time.Now().UnixNano(), value: m.nodeID}, true, nil
	}
	qctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	value, err := m.redis.Eval(qctx, discoveryAcquireScript,
		[]string{m.prefix + ":lease", m.prefix + ":fence"},
		m.nodeID, int64(ttl/time.Millisecond)).Int64()
	if err != nil || value <= 0 {
		return discoveryLease{}, false, err
	}
	return discoveryLease{fence: value, value: m.nodeID + "|" + strconv.FormatInt(value, 10)}, true, nil
}

func (m *discoveryGenerationManager) renew(ctx context.Context, lease discoveryLease, ttl time.Duration) (bool, error) {
	if m.redis == nil {
		return true, nil
	}
	qctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	value, err := m.redis.Eval(qctx, discoveryRenewScript, []string{m.prefix + ":lease"}, lease.value, int64(ttl/time.Millisecond)).Int64()
	return value == 1, err
}

func (m *discoveryGenerationManager) release(ctx context.Context, lease discoveryLease) error {
	if m.redis == nil {
		return nil
	}
	qctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return m.redis.Eval(qctx, discoveryReleaseScript, []string{m.prefix + ":lease"}, lease.value).Err()
}

func (m *discoveryGenerationManager) activate(ctx context.Context, lease discoveryLease, manifest discoveryGenerationManifest) error {
	if m.redis != nil {
		status, _ := json.Marshal(map[string]any{
			"generation_id": manifest.GenerationID,
			"fence":         lease.fence,
			"status":        "active",
			"updated_at":    time.Now().UTC(),
		})
		qctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		value, err := m.redis.Eval(qctx, discoveryActivateScript,
			[]string{m.prefix + ":lease", m.prefix + ":active", m.prefix + ":status", m.prefix + ":events"},
			lease.value, manifest.GenerationID, string(status)).Int64()
		if err != nil {
			return err
		}
		if value != 1 {
			return errors.New("discovery activation rejected by fencing token")
		}
	}
	// The receipt follows successful fenced activation. It is the durable
	// filesystem fallback when Redis is unavailable on a later startup.
	return m.writeReceipt(manifest, lease.fence)
}

func (m *discoveryGenerationManager) publishFailure(ctx context.Context, cause error) {
	if m.redis == nil {
		return
	}
	status, _ := json.Marshal(map[string]any{
		"status":     "failed",
		"updated_at": time.Now().UTC(),
		"error":      redactRuntimeError(cause),
	})
	qctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_ = m.redis.Set(qctx, m.prefix+":status", status, 0).Err()
	_ = m.redis.Publish(qctx, m.prefix+":events", "failed").Err()
}

func (m *discoveryGenerationManager) setBuilderStatus(ctx context.Context, lease discoveryLease, status, phase string) {
	if m == nil || m.redis == nil {
		return
	}
	value, _ := json.Marshal(map[string]any{
		"status":     status,
		"phase":      phase,
		"fence":      lease.fence,
		"node_id":    m.nodeID,
		"updated_at": time.Now().UTC(),
	})
	qctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_ = m.redis.Set(qctx, m.prefix+":status", value, 0).Err()
}

func (m *discoveryGenerationManager) cleanupOldGenerations() {
	deleter, ok := m.service.fs.(interface{ Delete(string) error })
	if !ok || m.conf.RetainGenerations <= 0 {
		return
	}
	entries, err := m.service.fs.List(path.Join(m.base, "activations"))
	if err != nil {
		return
	}
	var receipts []string
	for _, name := range entries {
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		data, err := m.service.fs.Get(path.Join(m.base, "activations", name))
		if err != nil {
			continue
		}
		var receipt discoveryActivationReceipt
		if json.Unmarshal(data, &receipt) != nil ||
			receipt.FormatVersion != discoveryGenerationFormatVersion ||
			receipt.Fingerprint != m.fingerprint ||
			receipt.GenerationID != strings.TrimSuffix(name, ".json") {
			continue
		}
		if _, err := m.loadGeneration(receipt.GenerationID); err == nil {
			receipts = append(receipts, name)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(receipts)))
	for _, name := range receipts[m.minRetained(len(receipts)):] {
		id := strings.TrimSuffix(name, ".json")
		manifest, err := m.loadGeneration(id)
		if err == nil {
			for _, file := range manifest.Files {
				_ = deleter.Delete(path.Join(m.generationDir(id), file.Name))
			}
		}
		_ = deleter.Delete(m.manifestPath(id))
		_ = deleter.Delete(path.Join(m.base, "activations", name))
	}
}

func (m *discoveryGenerationManager) minRetained(count int) int {
	if count < m.conf.RetainGenerations {
		return count
	}
	return m.conf.RetainGenerations
}

func newDiscoveryGenerationID() (string, error) {
	var suffix [6]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", err
	}
	return time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + hex.EncodeToString(suffix[:]), nil
}

func sameSourceRevisions(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}

func discoveryConfigFingerprint(conf *Config) (string, error) {
	if conf == nil {
		return "", errors.New("nil discovery configuration")
	}
	raw, err := json.Marshal(map[string]any{
		"core":     conf.Core,
		"database": conf.DB,
	})
	if err != nil {
		return "", err
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", err
	}
	redactDiscoveryConfig(value)
	canonical, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

func redactDiscoveryConfig(value any) {
	object, ok := value.(map[string]any)
	if !ok {
		if list, ok := value.([]any); ok {
			for _, item := range list {
				redactDiscoveryConfig(item)
			}
		}
		return
	}
	for key, item := range object {
		lower := strings.ToLower(key)
		switch {
		case strings.Contains(lower, "password"), strings.Contains(lower, "secret"),
			strings.Contains(lower, "private_key"), strings.Contains(lower, "passphrase"),
			strings.Contains(lower, "client_key"), strings.Contains(lower, "api_key"),
			strings.Contains(lower, "token"), strings.Contains(lower, "authorization"),
			strings.Contains(lower, "credential"), strings.Contains(lower, "certificate"),
			strings.Contains(lower, "client_cert"), strings.Contains(lower, "server_cert"),
			strings.Contains(lower, "pem"), lower == "cookie", lower == "user", lower == "username":
			object[key] = "<redacted>"
		case lower == "connection_string" || lower == "connstring" || lower == "url" || lower == "dsn":
			object[key] = redactedConnectionIdentity(fmt.Sprint(item))
		default:
			redactDiscoveryConfig(item)
		}
	}
}

func redactedConnectionIdentity(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" {
		return "<redacted>"
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

const discoveryAcquireScript = `
local fence = redis.call("incr", KEYS[2])
local value = ARGV[1] .. "|" .. fence
local ok = redis.call("set", KEYS[1], value, "NX", "PX", ARGV[2])
if not ok then return 0 end
return fence
`

const discoveryRenewScript = `
if redis.call("get", KEYS[1]) == ARGV[1] then
	redis.call("pexpire", KEYS[1], ARGV[2])
	return 1
end
return 0
`

const discoveryReleaseScript = `
if redis.call("get", KEYS[1]) == ARGV[1] then
	return redis.call("del", KEYS[1])
end
return 0
`

const discoveryActivateScript = `
if redis.call("get", KEYS[1]) ~= ARGV[1] then return 0 end
redis.call("set", KEYS[2], ARGV[2])
redis.call("set", KEYS[3], ARGV[3])
redis.call("publish", KEYS[4], ARGV[2])
return 1
`
