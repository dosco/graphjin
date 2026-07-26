// Package serv provides an API to include and use the GraphJin service with your own code.
// For detailed documentation visit https://graphjin.com
//
// Example usage:
/*
	package main

	import (
		"database/sql"
		"fmt"
		"time"
		"github.com/dosco/graphjin/core/v3"
		_ "github.com/jackc/pgx/v5/stdlib"
	)

	func main() {
		conf := serv.Config{ AppName: "Test App" }
		conf.DB.Host := "127.0.0.1"
		conf.DB.Port := 5432
		conf.DB.DBName := "test_db"
		conf.DB.User := "postgres"
		conf.DB.Password := "postgres"

		gjs, err := serv.NewGraphJinService(conf)
		if err != nil {
			log.Fatal(err)
		}

	 	if err := gjs.Start(); err != nil {
			log.Fatal(err)
		}
	}
*/
package serv

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	// "path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/dosco/graphjin/auth/v3"
	"github.com/dosco/graphjin/core/v3"
	otelPlugin "github.com/dosco/graphjin/plugin/otel/v3"
	"github.com/dosco/graphjin/serv/v3/internal/util"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type HttpService struct {
	atomic.Value
	opt   []Option
	cpath string
}

type servState int

const (
	servStarted servState = iota + 1
	servListening
)

type HookFn func(*core.Result)

type graphjinService struct {
	artifactProjectionRefreshes atomic.Int64

	log                   *zap.SugaredLogger // logger
	zlog                  *zap.Logger        // faster logger
	logLevel              int                // log level
	conf                  *Config            // parsed config
	dbs                   map[string]*sql.DB // named database connections (all equal)
	managedDBs            map[string]managedDB
	runtimeCore           *core.Config
	secretStore           *localKeystore
	metadataDB            string
	managedArtifactDB     string
	systemNanoDB          *core.NanoDB
	gj                    *core.GraphJin
	disc                  *DiscoveryManager
	discovery             *discoveryGenerationManager
	semantic              *semanticCatalogIndex
	semanticEmbedder      SemanticEmbeddingClient
	agentClientFactory    gjagent.ClientFactory
	watchSubscribeForTest func(context.Context, watchRuntimeDefinition, json.RawMessage) (*core.Member, error)
	srv                   *http.Server
	srvMu                 sync.Mutex // guards srv: written by startHTTP, read by Shutdown
	fs                    core.FS
	coreOptions           []core.Option
	// asec         [32]byte
	closeFn func()
	chash   string
	state   servState
	hook    HookFn
	prod    bool
	// deployActive bool
	// adminCount   int32
	namespace            *string
	tracer               trace.Tracer
	cache                ResponseCache // Response cache (Redis or in-memory)
	cursorCache          CursorCache   // MCP cursor cache for short numeric IDs
	runtimeEvents        runtimeEventStore
	runtimeEventsMu      sync.RWMutex
	configPreviews       *configPreviewStore
	configMu             sync.Mutex
	workflowMu           sync.Mutex
	workflowCache        *workflowRegistrySnapshot
	mcpHTTPMu            sync.Mutex
	mcpHTTP              *mcpHTTPTransportCache
	mcpWatchSubs         watchMCPSubscriptionRegistry
	watchCoordMu         sync.Mutex
	watchCoord           watchCoordinator
	watchSnoozeMu        sync.Mutex
	watchSnoozeLastSweep time.Time
	revisionSignalWG     sync.WaitGroup
	revisionConsumerWG   sync.WaitGroup
	catalogMu            sync.Mutex
	catalogCache         *catalogCacheEntry
	onboardingMu         sync.RWMutex
	onboardingCandidates map[string]cachedDiscoveredCandidate
	authLogin            *authLoginService // built-in OIDC login (optional)
}

// anyDB returns any single connection from the dbs map (for callers
// that just need a live connection, e.g. health checks, DDL, listing).
func (s *graphjinService) anyDB() *sql.DB {
	names := make([]string, 0, len(s.dbs))
	for name := range s.dbs {
		if name != s.managedArtifactDB {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if db := s.dbs[name]; db != nil {
			return db
		}
	}
	return nil
}

// buildCoreOptions returns core.Option slice including OptionSetDatabases.
func (s *graphjinService) buildCoreOptions() []core.Option {
	return s.buildCoreOptionsWithDBs(s.dbs)
}

func (s *graphjinService) buildCoreOptionsWithDBs(dbs map[string]*sql.DB) []core.Option {
	return s.buildCoreOptionsFor(dbs, s.managedDBs)
}

func (s *graphjinService) buildCoreOptionsFor(dbs map[string]*sql.DB, managedDBs map[string]managedDB) []core.Option {
	controlPlane := newControlPlaneGraphQL(s)
	artifacts := newArtifactControlPlane(s)
	watches := newWatchControlPlane(s)
	tasks := newTaskControlPlane(s)
	revisions := revisionSignalHandler{service: s}
	opts := []core.Option{
		core.OptionSetFS(s.fs),
		core.OptionSetTrace(otelPlugin.NewTracerFrom(s.tracer)),
		core.OptionSetSavedQuerySaveHook(s.saveSavedQueryArtifactOrFallback),
		core.OptionSetReservedRoleAuthorizer(s.authorizeReservedRole),
	}
	opts = append(opts, s.coreOptions...)
	if s.conf != nil && (s.conf.systemControlPlaneEnabled() || s.conf.workflowsEnabled()) {
		targetDB := s.metadataDB
		if targetDB == "" {
			targetDB = core.DefaultDBName
		}
		for _, dbName := range s.managedSystemRootDatabases(targetDB) {
			opts = append(opts, core.OptionSetManagedMutationHandler(dbName, controlPlane))
		}
		if s.conf.Core.Artifacts.Enabled {
			for _, dbName := range s.managedSystemRootDatabases(targetDB) {
				opts = append(opts, core.OptionSetManagedQueryHandler(dbName, controlPlane))
			}
		}
	}
	if s.conf != nil && s.conf.runtimeRootRegistered() {
		targetDB := s.metadataDB
		if targetDB == "" {
			targetDB = core.DefaultDBName
		}
		opts = append(opts, core.OptionSetManagedQueryHandler(targetDB, runtimeQueryHandler{service: s}))
	}
	if s.conf != nil && s.conf.Core.Artifacts.Enabled {
		targetDB := s.metadataDB
		if targetDB == "" {
			targetDB = core.DefaultDBName
		}
		opts = append(opts, core.OptionSetManagedQueryHandler(targetDB, revisions))
		for _, dbName := range s.managedSystemRootDatabases(targetDB) {
			if s.systemNanoDB == nil {
				opts = append(opts, core.OptionSetManagedQueryHandler(dbName, artifacts))
			}
			opts = append(opts, core.OptionSetManagedMutationHandler(dbName, artifacts))
		}
	}
	if s.conf != nil && s.watchesEnabled() {
		targetDB := s.metadataDB
		if targetDB == "" {
			targetDB = core.DefaultDBName
		}
		for _, dbName := range s.managedSystemRootDatabases(targetDB) {
			if s.systemNanoDB == nil {
				opts = append(opts, core.OptionSetManagedQueryHandler(dbName, watches))
			}
			opts = append(opts, core.OptionSetManagedMutationHandler(dbName, watches))
		}
	}
	if s.conf != nil && s.tasksEnabled() {
		targetDB := s.metadataDB
		if targetDB == "" {
			targetDB = core.DefaultDBName
		}
		for _, dbName := range s.managedSystemRootDatabases(targetDB) {
			if s.systemNanoDB == nil {
				opts = append(opts, core.OptionSetManagedQueryHandler(dbName, tasks))
			}
			opts = append(opts, core.OptionSetManagedMutationHandler(dbName, tasks))
		}
	}
	if s.namespace != nil {
		opts = append(opts, core.OptionSetNamespace(*s.namespace))
	}
	if s.cache != nil {
		opts = append(opts, core.OptionSetResponseCache(s.cache))
	}
	if len(dbs) > 0 {
		opts = append(opts, core.OptionSetDatabases(dbs))
	}
	if s.systemNanoDB != nil && s.metadataDB != "" {
		opts = append(opts, core.OptionSetNanoDatabases(map[string]*core.NanoDB{s.metadataDB: s.systemNanoDB}))
	}
	for name, managed := range managedDBs {
		if managed.handle != nil {
			opts = append(opts, core.OptionSetManagedMutationHandler(name, codeSQLMutationAdapter{
				managed:  managed.handle,
				readOnly: managed.readOnly,
			}))
		}
	}
	// Register filesystem backends contributed by this package's
	// init() blocks (s3, gcs) — gated by build tags. Local lives in
	// core itself and is always available.
	opts = append(opts, filesystemBackendOptions()...)
	return opts
}

func (s *graphjinService) buildCoreOptionsForState(
	dbs map[string]*sql.DB,
	managedDBs map[string]managedDB,
	conf *Config,
	metadataDB string,
	managedArtifactDB string,
	systemNanoDB *core.NanoDB,
) []core.Option {
	if s == nil {
		return nil
	}
	scoped := *s
	scoped.conf = conf
	scoped.metadataDB = metadataDB
	scoped.managedArtifactDB = managedArtifactDB
	scoped.systemNanoDB = systemNanoDB
	return scoped.buildCoreOptionsFor(dbs, managedDBs)
}

func (s *graphjinService) managedSystemRootDatabases(primary string) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	add(primary)
	if len(out) == 0 {
		out = append(out, core.DefaultDBName)
	}
	return out
}

type Option func(*graphjinService) error

// NewGraphJinService a new service
func NewGraphJinService(conf *Config, options ...Option) (*HttpService, error) {
	if conf.dirty {
		return nil, errors.New("do not re-use config object")
	}

	s, err := newGraphJinService(conf, nil, options...)
	if err != nil {
		return nil, err
	}

	s1 := &HttpService{opt: options, cpath: conf.ConfigPath}
	s1.Store(s)

	if s.conf.WatchAndReload {
		initConfigWatcher(s1)
	}

	// if s.conf.HotDeploy {
	// 	initHotDeployWatcher(s1)
	// }

	return s1, nil
}

// Close shuts down the in-process service resources owned by HttpService.
// It is useful for embedded use and tests that do not call Start().
func (s *HttpService) Close() error {
	if s == nil {
		return nil
	}
	gs, ok := s.Load().(*graphjinService)
	if !ok || gs == nil {
		return nil
	}
	gs.closeMCPHTTPTransport()
	if gs.semantic != nil {
		gs.semantic.Close()
	}
	if gs.discovery != nil {
		gs.discovery.Close()
	}
	if gs.gj != nil {
		gs.gj.Close()
	}
	if gs.closeFn != nil {
		gs.closeFn()
	}
	if gs.cache != nil {
		gs.cache.Close() //nolint:errcheck
	}
	gs.closeRuntimeEvents()
	closedManaged := gs.closeManagedDBs(nil)
	for name, db := range gs.dbs {
		if _, ok := closedManaged[name]; ok {
			continue
		}
		if db != nil {
			db.Close() //nolint:errcheck
		}
	}
	return nil
}

// Shutdown gracefully stops the running HTTP server so that a blocking Start
// returns. In-flight requests are drained until the context deadline, after
// which the server is force-closed. It is safe to call more than once and
// before Start (a no-op when the server was never started). Callers that embed
// the service and run their own signal handling use this to unblock Start;
// the service resources (databases, cache, etc.) are then released as Start
// returns, or explicitly via Close.
func (s *HttpService) Shutdown(ctx context.Context) error {
	if s == nil {
		return nil
	}
	gs, ok := s.Load().(*graphjinService)
	if !ok || gs == nil {
		return nil
	}
	gs.srvMu.Lock()
	srv := gs.srv
	gs.srvMu.Unlock()
	if srv == nil {
		return nil
	}
	if err := srv.Shutdown(ctx); err != nil {
		gs.log.Warnf("graceful shutdown timed out, forcing close: %s", err)
		return srv.Close()
	}
	return nil
}

// closeServResources releases the resources owned by a running service once
// its HTTP listener has stopped: the MCP transport, the user close hook, the
// response cache, runtime event streams and all database connections.
func (s *graphjinService) closeServResources() {
	s.closeMCPHTTPTransport()
	if s.semantic != nil {
		s.semantic.Close()
	}
	if s.discovery != nil {
		s.discovery.Close()
	}
	if s.closeFn != nil {
		s.closeFn()
	}
	s.revisionConsumerWG.Wait()
	s.revisionSignalWG.Wait()
	if s.gj != nil {
		s.gj.Close()
	}
	if s.cache != nil {
		s.cache.Close() //nolint:errcheck
	}
	s.closeRuntimeEvents()
	closedManaged := s.closeManagedDBs(nil)
	for name, db := range s.dbs {
		if _, ok := closedManaged[name]; ok {
			continue
		}
		if db != nil {
			db.Close() //nolint:errcheck
			s.log.Infof("closed database connection: %s", name)
		}
	}
}

// OptionSetDB sets a new db client. The connection is stored under the
// DefaultDBName key in the dbs map for backward compatibility.
func OptionSetDB(db *sql.DB) Option {
	return func(s *graphjinService) error {
		if s.dbs == nil {
			s.dbs = make(map[string]*sql.DB)
		}
		s.dbs[core.DefaultDBName] = db
		return nil
	}
}

// OptionSetDatabases sets named database clients for multi-database embeddings.
func OptionSetDatabases(dbs map[string]*sql.DB) Option {
	return func(s *graphjinService) error {
		if len(dbs) == 0 {
			return nil
		}
		if s.dbs == nil {
			s.dbs = make(map[string]*sql.DB, len(dbs))
		}
		for name, db := range dbs {
			if db != nil {
				s.dbs[name] = db
			}
		}
		return nil
	}
}

// OptionSetHookFunc sets a function to be called on every request
func OptionSetHookFunc(fn HookFn) Option {
	return func(s *graphjinService) error {
		s.hook = fn
		return nil
	}
}

// OptionSetNamespace sets service namespace
func OptionSetNamespace(namespace string) Option {
	return func(s *graphjinService) error {
		s.namespace = &namespace
		return nil
	}
}

// OptionSetFS sets service filesystem
func OptionSetFS(fs core.FS) Option {
	return func(s *graphjinService) error {
		s.fs = fs
		return nil
	}
}

// OptionSetSemanticEmbeddingClient replaces the Ax embedding adapter. It is
// primarily intended for deterministic tests and private provider adapters.
func OptionSetSemanticEmbeddingClient(client SemanticEmbeddingClient) Option {
	return func(s *graphjinService) error {
		s.semanticEmbedder = client
		return nil
	}
}

// OptionSetAgentClientFactory injects a server-side agent model client. An
// injected client is treated exactly like configured provider credentials:
// MCP never falls back to client sampling while it is present.
func OptionSetAgentClientFactory(factory gjagent.ClientFactory) Option {
	return func(s *graphjinService) error {
		s.agentClientFactory = factory
		return nil
	}
}

// OptionSetRuntimeSchemaDDLDir sets where the core stores generated
// schema-DDL restart snapshots, relative to the service filesystem root.
func OptionSetRuntimeSchemaDDLDir(dir string) Option {
	return func(s *graphjinService) error {
		s.coreOptions = append(s.coreOptions, core.OptionSetRuntimeSchemaDDLDir(dir))
		return nil
	}
}

// OptionSetZapLogger sets service structured logger
func OptionSetZapLogger(zlog *zap.Logger) Option {
	return func(s *graphjinService) error {
		s.zlog = zlog
		s.log = zlog.Sugar()
		return nil
	}
}

// OptionSetLogOutput sets the log output writer (e.g., os.Stderr for MCP stdio mode)
func OptionSetLogOutput(output zapcore.WriteSyncer) Option {
	return func(s *graphjinService) error {
		zlog := util.NewLoggerWithOutput(s.conf.ShouldUseJSONLogs(), output)
		s.zlog = zlog
		s.log = zlog.Sugar()
		return nil
	}
}

// OptionDeployActive caused the active config to be deployed on
// func OptionDeployActive() Option {
// 	return func(s *graphjinService) error {
// 		s.deployActive = true
// 		return nil
// 	}
// }

// newGraphJinService creates a new service
func newGraphJinService(conf *Config, dbs map[string]*sql.DB, options ...Option) (*graphjinService, error) {
	var err error
	if conf == nil {
		conf = &Config{Core: Core{Debug: true}}
	}
	if err := normalizeConfigMode(conf); err != nil {
		return nil, err
	}

	zlog := util.NewLogger(conf.ShouldUseJSONLogs())
	prod := conf.Serv.Production

	s := &graphjinService{
		conf:           conf,
		zlog:           zlog,
		log:            zlog.Sugar(),
		dbs:            dbs,
		managedDBs:     make(map[string]managedDB),
		configPreviews: newConfigPreviewStore(),
		chash:          conf.hash,
		prod:           prod,
		tracer:         otel.Tracer("graphjin.com/serv"),
	}
	if s.dbs == nil {
		s.dbs = make(map[string]*sql.DB)
	}

	if err := s.initConfig(); err != nil {
		return nil, err
	}

	// Default raw MCP execution to true in dev mode when MCP is enabled.
	if !s.conf.Serv.Production && !s.conf.mcpDisabled() {
		if s.conf.viper != nil && !s.conf.viper.IsSet("mcp.allow_raw_queries") {
			s.conf.MCP.AllowRawQueries = true
			s.log.Info("MCP raw GraphQL execution enabled by default (dev mode)")
		}
		if s.conf.viper != nil && !s.conf.viper.IsSet("mcp.allow_mutations") {
			s.conf.MCP.AllowMutations = true
			s.log.Info("MCP raw GraphQL mutations enabled by default (dev mode)")
		}
	}

	// Default AllowConfigUpdates to true in dev mode when MCP is enabled
	if !s.conf.Serv.Production && !s.conf.mcpDisabled() {
		// Only set default if not explicitly configured by user
		if s.conf.viper != nil && !s.conf.viper.IsSet("mcp.allow_config_updates") {
			s.conf.MCP.AllowConfigUpdates = true
			s.log.Info("MCP config updates enabled by default (dev mode)")
		}
	}

	// Default AllowSchemaReload to true in dev mode when MCP is enabled
	if !s.conf.Serv.Production && !s.conf.mcpDisabled() {
		if s.conf.viper != nil && !s.conf.viper.IsSet("mcp.allow_schema_reload") {
			s.conf.MCP.AllowSchemaReload = true
			s.log.Info("MCP schema reload enabled by default (dev mode)")
		}
	}

	// Default AllowSchemaUpdates to true in dev mode when MCP is enabled
	if !s.conf.Serv.Production && !s.conf.mcpDisabled() {
		if s.conf.viper != nil && !s.conf.viper.IsSet("mcp.allow_schema_updates") {
			s.conf.MCP.AllowSchemaUpdates = true
			s.log.Info("MCP schema updates enabled by default (dev mode)")
		}
	}

	// Default AllowWorkflowUpdates to true in dev mode when MCP is enabled
	if !s.conf.Serv.Production && !s.conf.mcpDisabled() {
		if s.conf.viper != nil && !s.conf.viper.IsSet("mcp.allow_workflow_updates") {
			s.conf.MCP.AllowWorkflowUpdates = true
			s.log.Info("MCP workflow updates enabled by default (dev mode)")
		}
	}

	// Default legacy MCP workflow execution to true in dev mode when MCP is enabled.
	if !s.conf.Serv.Production && !s.conf.mcpDisabled() {
		if s.conf.viper != nil && !s.conf.viper.IsSet("mcp.allow_workflow_execution") {
			s.conf.MCP.AllowWorkflowExecution = true
			s.log.Info("MCP workflow execution enabled by default (dev mode)")
		}
	}

	// Default AllowDevTools to true in dev mode when MCP is enabled
	if !s.conf.Serv.Production && !s.conf.mcpDisabled() {
		if s.conf.viper != nil && !s.conf.viper.IsSet("mcp.allow_dev_tools") {
			s.conf.MCP.AllowDevTools = true
			s.log.Info("MCP dev tools enabled by default (dev mode)")
		}
	}
	applySourceCapabilityMCPDefaults(s.conf)

	if err := s.initFS(); err != nil {
		return nil, err
	}

	for _, op := range options {
		if err := op(s); err != nil {
			return nil, err
		}
	}

	initLogLevel(s)
	if err := validateConf(s); err != nil {
		return nil, err
	}

	if err := s.initRuntimeObservability(); err != nil {
		s.log.Warnf("runtime observability init error: %s", err)
	}

	if err := s.initDB(); err != nil {
		s.recordRuntimeEvent(context.Background(), runtimeEvent{
			Phase:      "database",
			Status:     runtimeStatusFailed,
			Severity:   "error",
			Summary:    "Database initialization failed.",
			NextAction: "Inspect database configuration and retry GraphJin initialization.",
			ErrorCode:  "database_init_failed",
			Details:    map[string]any{"error": err.Error()},
		})
		return nil, err
	}
	if err := s.initManagedArtifactStore(); err != nil {
		return nil, err
	}

	// Initialize Redis cache (non-fatal if unavailable)
	if err := s.initResponseCache(); err != nil {
		s.log.Warnf("response cache init error: %s", err)
	}
	s.bindCodeSQLCacheHooks()

	// Initialize MCP cursor cache (non-fatal if unavailable)
	if err := s.initCursorCache(); err != nil {
		s.log.Warnf("cursor cache init error: %s", err)
	}

	// Initialize built-in OIDC login (non-fatal if disabled)
	if s.conf.AuthLogin.Enabled {
		als, err := newAuthLoginService(context.Background(), s.conf)
		if err != nil {
			return nil, fmt.Errorf("auth_login: %w", err)
		}
		s.authLogin = als
		s.log.Infof("auth_login: enabled (oidc issuer: %s)", s.conf.AuthLogin.OIDC.IssuerURL)
	}

	// if s.deployActive {
	// 	err = s.hotStart()
	// } else {
	err = s.normalStart()
	// }

	if err != nil {
		s.recordRuntimeEvent(context.Background(), runtimeEvent{
			Phase:      "graphjin_init",
			Status:     runtimeStatusFailed,
			Severity:   "error",
			Summary:    "GraphJin core initialization failed.",
			NextAction: "Inspect gj_runtime events and repair configuration or database connectivity before retrying.",
			ErrorCode:  "graphjin_init_failed",
			Details:    map[string]any{"error": err.Error()},
		})
		if isNonRecoverableStartupError(err) {
			return nil, err
		}
		if !s.conf.Serv.Production {
			s.gj = nil // Ensure gj is nil so checkGraphJinInitialized() works
			s.log.Warnf("GraphJin core initialization failed: %s", err)
			s.log.Warn("Server starting without query engine — use MCP to fix the configuration")
			// Continue with gj = nil, MCP tools still work
		} else {
			return nil, err
		}
	}
	if err == nil {
		s.recordRuntimeEvent(context.Background(), runtimeEvent{
			Phase:       "graphjin_init",
			Status:      runtimeStatusReady,
			Severity:    "info",
			Summary:     "GraphJin core initialized successfully.",
			NextAction:  "Use gj_runtime after errors or before guarded workflow, config, or schema actions.",
			SchemaReady: s.gj != nil && s.gj.SchemaReady(),
		})
		s.registerRuntimeSchemaCallbacks()
		if werr := s.startLocalFilesystemCacheWatchers(); werr != nil {
			s.log.Warnf("filesystem cache watcher init error: %s", werr)
		}
		s.startProjectionPoller(context.Background())
		s.startWatchCoordinator(context.Background())
		s.startWatchRunner(context.Background())
		s.startTaskVerifier(context.Background())
	}

	s.state = servStarted
	return s, nil
}

// normalStart starts the service in normal mode
func (s *graphjinService) normalStart() error {
	if err := s.initArtifactsBeforeCore(); err != nil {
		return err
	}
	if err := s.initMetadataGraphBeforeCore(); err != nil {
		return err
	}
	if s.systemNanoDB == nil {
		if err := s.ensureSystemHostDBBeforeCore(); err != nil {
			return err
		}
	}
	// Skip GraphJin core initialization if no database is configured (dev mode only)
	if len(s.dbs) == 0 && (s.systemNanoDB == nil || !s.conf.Core.IsSourcesUsed()) {
		if !s.conf.Serv.Production {
			s.log.Info("GraphJin core not initialized - waiting for database configuration via MCP")
			return nil
		}
		return fmt.Errorf("no database source configured")
	}

	coreConf := &s.conf.Core
	if s.runtimeCore != nil {
		coreConf = s.runtimeCore
	}
	s.injectInternalStoreRole()
	opts := s.buildCoreOptions()
	if s.conf.DiscoveryCache.enabled() {
		manager, err := newDiscoveryGenerationManager(s)
		if err != nil {
			return err
		}
		dir, err := manager.InitialGeneration(context.Background(), coreConf, opts)
		if err != nil {
			manager.Close()
			return err
		}
		s.discovery = manager
		opts = append(opts,
			core.OptionSetDBSchemaWatcherDisabled(true),
			core.OptionSetRuntimeSchemaDDLDir(dir),
			core.OptionSetRuntimeSchemaCacheFirst(true),
			core.OptionSetRuntimeSchemaCacheRequired(true),
		)
	}

	var err error
	s.gj, err = core.NewGraphJin(coreConf, s.anyDB(), opts...)
	if err != nil {
		return err
	}
	if err := s.refreshMetadataGraph(); err != nil {
		return err
	}
	s.disc = NewDiscoveryManager(s.gj)
	if s.conf.CatalogSearch.Semantic.Enabled {
		semantic, err := newSemanticCatalogIndex(s)
		if err != nil {
			s.log.Warnf("semantic catalog initialization failed; using lexical search: %s", redactRuntimeError(err))
		} else {
			s.semantic = semantic
			semantic.Start()
		}
	}
	if s.discovery != nil {
		s.discovery.Start()
	}
	return nil
}

// hotStart starts the service in hot-deploy mode
// func (s *graphjinService) hotStart() error {
// 	ab, err := fetchActiveBundle(s.db)
// 	if err != nil {
// 		if strings.Contains(err.Error(), "_graphjin.") {
// 			return fmt.Errorf("please run 'graphjin init' to setup database for hot-deploy")
// 		}
// 		return err
// 	}
//
// 	if ab == nil {
// 		return s.normalStart()
// 	}
//
// 	cf := s.conf.viper.ConfigFileUsed()
// 	cf = filepath.Base(strings.TrimSuffix(cf, filepath.Ext(cf)))
// 	cf = filepath.Join("/", cf)
//
// 	bfs, err := bundle2Fs(ab.name, ab.hash, cf, ab.bundle)
// 	if err != nil {
// 		return err
// 	}
// 	s.conf = bfs.conf
// 	s.chash = bfs.conf.hash
//
// 	if err := s.initConfig(); err != nil {
// 		return err
// 	}
//
// 	opts := []core.Option{
// 		core.OptionSetFS(newAferoFS(bfs.fs, "/")),
// 		core.OptionSetTrace(otelPlugin.NewTracerFrom(s.tracer)),
// 	}
//
// 	if s.namespace != nil {
// 		opts = append(opts,
// 			core.OptionSetNamespace(*s.namespace))
// 	}
// 	// Add response cache if enabled
// 	if s.cache != nil {
// 		opts = append(opts, core.OptionSetResponseCache(s.cache))
// 	}
//
// 	s.gj, err = core.NewGraphJin(&s.conf.Core, s.db, opts...)
// 	return err
// }

// Deploy a new configuration
func (s *HttpService) Deploy(conf *Config, options ...Option) error {
	var err error
	os := s.Load().(*graphjinService)

	if conf == nil {
		return nil
	}

	s1, err := newGraphJinService(conf, os.dbs, options...)
	if err != nil {
		return err
	}
	s1.srv = os.srv
	s1.namespace = os.namespace
	os.closeMCPHTTPTransport()
	if os.closeFn != nil {
		os.closeFn()
	}

	s.Store(s1)
	return nil
}

// Start the service listening on the configured port
func (s *HttpService) Start() error {
	startHTTP(s)
	return nil
}

// Attach route to the internal http service
func (s *HttpService) Attach(mux Mux) error {
	return s.attach(mux, nil)
}

// AttachWithNS a namespaced route to the internal http service
func (s *HttpService) AttachWithNS(mux Mux, namespace string) error {
	return s.attach(mux, &namespace)
}

// attach attaches the service to the router
func (s *HttpService) attach(mux Mux, ns *string) error {
	if _, err := routesHandler(s, mux, ns); err != nil {
		return err
	}

	s1 := s.Load().(*graphjinService)

	ver := version
	dep := s1.conf.name

	if ver == "" {
		ver = "not-set"
	}

	fields := []zapcore.Field{
		zap.String("version", ver),
		zap.String("app-name", s1.conf.AppName),
		zap.String("deployment-name", dep),
		zap.String("env", os.Getenv("GO_ENV")),
		// zap.Bool("hot-deploy", s1.conf.HotDeploy),
		zap.Bool("production", s1.conf.Core.Production),
	}

	if s1.namespace != nil {
		fields = append(fields, zap.String("namespace", *s1.namespace))
	}

	// if s1.conf.HotDeploy {
	// 	fields = append(fields, zap.String("deployment-name", dep))
	// }

	s1.zlog.Info("GraphJin attached to router", fields...)
	return nil
}

// GraphQLis the http handler the GraphQL endpoint
func (s *HttpService) GraphQL(ah auth.HandlerFunc) http.Handler {
	return s.apiHandler(nil, ah, false)
}

// GraphQLWithNS is the http handler the namespaced GraphQL endpoint
func (s *HttpService) GraphQLWithNS(ah auth.HandlerFunc, ns string) http.Handler {
	return s.apiHandler(&ns, ah, false)
}

// REST is the http handler the REST endpoint
func (s *HttpService) REST(ah auth.HandlerFunc) http.Handler {
	return s.apiHandler(nil, ah, true)
}

// RESTWithNS is the http handler the namespaced REST endpoint
func (s *HttpService) RESTWithNS(ah auth.HandlerFunc, ns string) http.Handler {
	return s.apiHandler(&ns, ah, true)
}

// Workflows is the http handler for named JS workflows.
func (s *HttpService) Workflows(ah auth.HandlerFunc) http.Handler {
	h := s.apiV1Workflows(nil)
	return apiV1Handler(s, nil, h, ah)
}

// WorkflowsWithNS is the namespaced http handler for named JS workflows.
func (s *HttpService) WorkflowsWithNS(ah auth.HandlerFunc, ns string) http.Handler {
	h := s.apiV1Workflows(&ns)
	return apiV1Handler(s, &ns, h, ah)
}

// OpenAPI is the http handler for the OpenAPI specification endpoint
func (s *HttpService) OpenAPI() http.Handler {
	return s.openAPIHandler(nil)
}

// OpenAPIWithNS is the http handler for the namespaced OpenAPI specification endpoint
func (s *HttpService) OpenAPIWithNS(ns string) http.Handler {
	return s.openAPIHandler(&ns)
}

func (s *HttpService) apiHandler(ns *string, ah auth.HandlerFunc, rest bool) http.Handler {
	var h http.Handler
	if rest {
		h = s.apiV1Rest(ns, ah)
	} else {
		h = s.apiV1GraphQL(ns, ah)
	}
	return apiV1Handler(s, ns, h, ah)
}

// WebUI is the http handler the web ui endpoint
func (s *HttpService) WebUI(routePrefix, gqlEndpoint string) http.Handler {
	return webuiHandler(routePrefix, gqlEndpoint)
}

// GetGraphJin fetching internal GraphJin core
func (s *HttpService) GetGraphJin() *core.GraphJin {
	s1 := s.Load().(*graphjinService)
	return s1.gj
}

// GetDB fetching internal db client (returns any connection from the pool)
func (s *HttpService) GetDB() *sql.DB {
	s1 := s.Load().(*graphjinService)
	return s1.anyDB()
}

// Reload re-runs database discover and reinitializes service.
func (s *HttpService) Reload() error {
	s1 := s.Load().(*graphjinService)
	if s1.gj == nil {
		return errors.New("graphjin: engine not initialized")
	}
	if s1.discovery != nil {
		return s1.discovery.RefreshNow(context.Background())
	}
	return s1.gj.Reload()
}

// spanStart starts the tracer
func (s *graphjinService) spanStart(c context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	if s.tracer == nil {
		return otel.Tracer("graphjin").Start(c, name, opts...)
	}
	return s.tracer.Start(c, name, opts...)
}

// spanError records an error in the span
func spanError(span trace.Span, err error) {
	if span.IsRecording() {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
}
