// Package core provides an API to include and use the GraphJin compiler with your own code.
// For detailed documentation visit https://graphjin.com
package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	_log "log"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dosco/graphjin/core/v3/fstable"
	"github.com/dosco/graphjin/core/v3/internal/allow"
	"github.com/dosco/graphjin/core/v3/internal/graph"
	"github.com/dosco/graphjin/core/v3/internal/psql"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
	"github.com/dosco/graphjin/core/v3/openapi"
)

type contextkey int

// Constants to set values on the context passed to the NewGraphJin function
const (
	// Name of the authentication provider. Eg. google, github, etc
	UserIDProviderKey contextkey = iota

	// The raw user id (jwt sub) value
	UserIDRawKey

	// User ID value for authenticated users
	UserIDKey

	// User role if pre-defined
	UserRoleKey

	// IdentityVarsKey carries trusted request-wide identity variables such as
	// account_id that may be referenced by generated source-mode filters.
	IdentityVarsKey

	// IdentityRolesKey carries candidate roles extracted from the verified
	// request identity before roles_query / match fallback.
	IdentityRolesKey
)

const (
	APQ_PX = "_apq"
)

// dbContext holds per-database state for multi-database support.
// Each database gets its own connection pool, schema discovery, and SQL compiler.
type dbContext struct {
	name          string          // Database name (key in Config.Databases)
	db            *sql.DB         // Connection pool for this database
	dbtype        string          // Database type (postgres, mysql, sqlite, etc.)
	nano          *NanoDB         // Pure-Go compact built-in system/catalog/workflow tables
	dbinfo        *sdata.DBInfo   // Raw schema metadata
	schema        *sdata.DBSchema // Processed schema with relationships
	qcodeCompiler *qcode.Compiler // GraphQL to QCode compiler (validates against this DB's schema)
	psqlCompiler  *psql.Compiler  // QCode to SQL compiler (generates this DB's dialect)
}

// GraphJin struct is an instance of the GraphJin engine it holds all the required information like
// datase schemas, relationships, etc that the GraphQL to SQL compiler would need to do it's job.
type graphjinEngine struct {
	conf                       *Config
	catalogConf                *Config
	log                        *_log.Logger
	fs                         FS
	trace                      Tracer
	allowList                  *allow.List
	savedQuerySaveHook         SavedQuerySaveHook
	encryptionKey              [32]byte
	encryptionKeySet           bool
	cache                      Cache
	queries                    sync.Map
	sqliteConflictGetMu        sync.Mutex
	roles                      map[string]*Role
	roleStatement              string
	roleStatementMetadata      psql.Metadata
	roleQueryMode              roleQueryMode
	roleGraphQLStmt            stmt
	roleGraphQLMatches         []compiledRoleMatch
	tmap                       map[string]qcode.TConfig
	rtmap                      map[string]ResolverFn
	rmap                       map[string]resItem
	openapiRuntime             *openapi.Runtime
	abacEnabled                bool
	subs                       sync.Map
	prod                       bool
	prodSec                    bool
	learn                      bool
	namespace                  string
	printFormat                []byte
	opts                       []Option
	runtimeSchemaDDLDir        string
	runtimeSchemaCacheFirst    bool
	runtimeSchemaCacheRequired bool
	disableDBSchemaWatcher     bool
	done                       chan bool

	// All databases (including the primary/default) live here.
	databases map[string]*dbContext
	// Name of the default database (used as the map key for the primary DB)
	defaultDB string

	// Response cache provider (optional, set via OptionSetResponseCache)
	responseCache ResponseCacheProvider
	// Cache key builder
	cacheKeyBuilder *CacheKeyBuilder

	// Federation SDL is built lazily on first request and cached. The
	// schema only changes via Reload, which constructs a fresh
	// graphjinEngine, so cache invalidation is implicit.
	federationSDLOnce sync.Once
	federationSDL     string
	federationSDLErr  error

	// Filesystem virtual tables (local / S3 / GCS). Backend factories
	// are registered by name via OptionSetFilesystemBackend; the engine
	// instantiates one Backend per FilesystemConfig entry during init.
	fsFactories map[string]FilesystemBackendFactory
	fsBackends  map[string]fstable.Backend

	// Managed mutation handlers intercept selected mutation tables before
	// normal SQL execution. The runtime database can remain read-only while
	// a service-owned handler applies guarded side effects.
	managedMutationHandlers map[string]ManagedMutationHandler

	// Managed query handlers expose service-owned, synthetic read roots through
	// GraphJin's normal GraphQL query surface without hitting user tables.
	managedQueryHandlers map[string]ManagedQueryHandler

	// reservedRoleAuthorizer can allow package-internal service contexts to use
	// reserved roles. Request-derived roles are denied by default.
	reservedRoleAuthorizer ReservedRoleAuthorizer
}

// primaryDB returns the default database context.
func (gj *graphjinEngine) primaryDB() *dbContext {
	if ctx, ok := gj.databases[gj.defaultDB]; ok {
		return ctx
	}
	return nil
}

// anyDatabaseReady returns true if at least one database has an initialized schema.
func (gj *graphjinEngine) anyDatabaseReady() bool {
	for _, ctx := range gj.databases {
		if ctx.schema != nil {
			return true
		}
	}
	return false
}

type GraphJin struct {
	atomic.Value
	done     chan bool
	stopOnce sync.Once
	reloadMu sync.Mutex // serializes reload operations

	// Schema change callbacks
	schemaCallbacks []func(dbName string, hash string)
	callbackMu      sync.RWMutex
}

type Option func(*graphjinEngine) error

// ReservedRoleAuthorizer authorizes a reserved role name, such as a GraphJin
// internal role, for a specific request context.
type ReservedRoleAuthorizer func(context.Context, string) bool

// SavedQueryFragment is a fragment captured while saving a named query.
type SavedQueryFragment struct {
	Name  string
	Value []byte
}

// SavedQuerySaveRequest is passed to SavedQuerySaveHook before dev-mode named
// query auto-save writes to the configured filesystem allow-list.
type SavedQuerySaveRequest struct {
	Namespace  string
	Name       string
	Operation  string
	Query      []byte
	Fragments  []SavedQueryFragment
	ActionJSON map[string]json.RawMessage
}

// SavedQuerySaveHook lets an embedding service redirect dev-mode named-query
// saves. Returning handled=false preserves the default filesystem allow-list
// behavior.
type SavedQuerySaveHook func(context.Context, SavedQuerySaveRequest) (handled bool, err error)

// OnSchemaChange registers a callback that fires when the database schema changes.
// The callback receives the database name and a hex-encoded hash of the schema.
// Callbacks also fire once at startup after initial schema discovery.
func (g *GraphJin) OnSchemaChange(fn func(dbName string, hash string)) {
	g.callbackMu.Lock()
	defer g.callbackMu.Unlock()
	g.schemaCallbacks = append(g.schemaCallbacks, fn)
}

// fireSchemaCallbacks invokes all registered schema change callbacks.
// Runs each callback in a goroutine to avoid blocking the caller (which may hold reloadMu).
func (g *GraphJin) fireSchemaCallbacks(dbName string, hash string) {
	g.callbackMu.RLock()
	callbacks := make([]func(string, string), len(g.schemaCallbacks))
	copy(callbacks, g.schemaCallbacks)
	g.callbackMu.RUnlock()

	for _, fn := range callbacks {
		fn := fn
		go fn(dbName, hash)
	}
}

// DefaultDatabase returns the name of the default (primary) database.
func (g *GraphJin) DefaultDatabase() string {
	gj, err := g.getEngine()
	if err != nil {
		return ""
	}
	return gj.defaultDB
}

// DatabaseNames returns the names of all configured databases.
func (g *GraphJin) DatabaseNames() []string {
	gj, err := g.getEngine()
	if err != nil {
		return nil
	}
	return gj.sortedDatabaseNames()
}

// fireAllSchemaCallbacks fires schema change callbacks for all databases with initialized schemas.
func (g *GraphJin) fireAllSchemaCallbacks() {
	gj, err := g.getEngine()
	if err != nil {
		return
	}
	for name, ctx := range gj.databases {
		if ctx.dbinfo != nil {
			g.fireSchemaCallbacks(name, fmt.Sprintf("%x", ctx.dbinfo.Hash()))
		}
	}
}

// NewGraphJin creates the GraphJin struct, this involves querying the database to learn its
// schemas and relationships
func NewGraphJin(conf *Config, db *sql.DB, options ...Option) (g *GraphJin, err error) {
	fs, err := getFS(conf)
	if err != nil {
		return
	}

	g = &GraphJin{done: make(chan bool)}
	if err = g.newGraphJin(conf, db, nil, fs, options...); err != nil {
		g = nil
		return
	}

	if err = g.initDBWatcher(); err != nil {
		g = nil
		return
	}

	g.fireAllSchemaCallbacks()
	return
}

// NewGraphJinWithFS creates the GraphJin struct, this involves querying the database to learn its
func NewGraphJinWithFS(conf *Config, db *sql.DB, fs FS, options ...Option) (g *GraphJin, err error) {
	g = &GraphJin{done: make(chan bool)}
	if err = g.newGraphJin(conf, db, nil, fs, options...); err != nil {
		g = nil
		return
	}

	if err = g.initDBWatcher(); err != nil {
		g = nil
		return
	}

	g.fireAllSchemaCallbacks()
	return
}

var errEngineNotInitialized = errors.New("graphjin: engine not initialized")

func (g *GraphJin) getEngine() (*graphjinEngine, error) {
	v := g.Load()
	if v == nil {
		return nil, errEngineNotInitialized
	}
	gj, ok := v.(*graphjinEngine)
	if !ok || gj == nil {
		return nil, errEngineNotInitialized
	}
	return gj, nil
}

// FilesystemBackend returns the backend instance configured under the
// given filesystems[].name, or false if no such filesystem exists.
// Useful for callers that want to bypass the GraphQL surface — e.g.
// the multipart upload parser streaming directly to object storage.
func (g *GraphJin) FilesystemBackend(name string) (fstable.Backend, bool) {
	gj, err := g.getEngine()
	if err != nil {
		return nil, false
	}
	b, ok := gj.fsBackends[name]
	return b, ok
}

// InvalidateCacheRefs invalidates response-cache entries associated with the
// supplied dependency refs. It is a no-op when response caching is disabled.
func (g *GraphJin) InvalidateCacheRefs(ctx context.Context, refs []RowRef) error {
	gj, err := g.getEngine()
	if err != nil {
		return err
	}
	if gj.responseCache == nil || len(refs) == 0 {
		return nil
	}
	return gj.responseCache.InvalidateRows(ctx, refs)
}

// Close stops GraphJin background tasks. It is safe to call multiple times.
func (g *GraphJin) Close() {
	if g == nil || g.done == nil {
		return
	}

	g.stopOnce.Do(func() {
		close(g.done)
	})
}

// newGraphJinWithDBInfo creates the GraphJin struct, this involves querying the database to learn its
// it all starts here
func (g *GraphJin) newGraphJin(conf *Config,
	db *sql.DB,
	dbinfo *sdata.DBInfo,
	fs FS,
	options ...Option,
) (err error) {
	if conf == nil {
		conf = &Config{Debug: true}
	}

	// Deep-copy mutable slices/maps so that init never mutates the caller's
	// Config. Without this, NormalizeDatabases, finalizeDatabaseSchema, and
	// ensureDiscoveredTablesInConfig would accumulate side-effects across
	// repeated NewGraphJin calls that share the same *Config.
	conf = conf.clone()
	if err := conf.NormalizeMode(); err != nil {
		return err
	}

	t := time.Now()

	gj := &graphjinEngine{
		conf:        conf,
		log:         _log.New(os.Stderr, "", 0),
		prod:        conf.Production,
		prodSec:     conf.Production,
		learn:       !conf.Production && !isAgenticMode(conf),
		printFormat: []byte(fmt.Sprintf("gj-%x:", t.UnixNano())),
		opts:        options,
		fs:          fs,
		trace:       &tracer{},
		done:        g.done,
	}

	if gj.conf.DisableProdSecurity {
		gj.prodSec = false
	}

	// ordering of these initializer matter, do not re-order!

	if err = gj.initCache(); err != nil {
		return
	}

	if err = gj.initConfig(); err != nil {
		return
	}

	// Set defaultDB from the normalized config, preferring application sources
	// over service-owned runtime databases.
	if gj.defaultDB == "" {
		gj.defaultDB = gj.conf.defaultDatabaseName()
	}

	// Determine dbtype for the primary database
	dbtype := conf.DBType
	if dbtype == "" {
		dbtype = "postgres"
	}

	// Store the primary DB as a bare context in gj.databases.
	// Always create the entry even when db is nil (e.g. MockDB mode).
	gj.databases = make(map[string]*dbContext)
	gj.databases[gj.defaultDB] = &dbContext{
		name:   gj.defaultDB,
		db:     db, // may be nil for MockDB
		dbtype: dbtype,
		dbinfo: dbinfo, // may be preset from watcher/tests
	}

	for _, op := range options {
		if err = op(gj); err != nil {
			return
		}
	}
	// Catalog configuration revisions describe the normalized caller config,
	// not derived table entries added during schema finalization. Keeping this
	// snapshot stable also makes live- and cache-discovered engines comparable.
	gj.catalogConf = gj.conf.clone()

	// Phase 1: Discover all databases (get raw schema metadata)
	if err = gj.discoverAllDatabases(); err != nil {
		return
	}

	// Phase 2: Resolvers (adds remote tables to primary DB's dbinfo)
	if err = gj.initResolvers(); err != nil {
		return
	}

	// Phase 3: Managed service-owned tables (adds synthetic gj_* roots)
	if err = gj.initManagedQueryTables(); err != nil {
		return
	}

	// Phase 4: Finalize schemas and compilers for all databases
	if err = gj.finalizeAllDatabases(); err != nil {
		return
	}

	// Only initialize dependent features if at least one database has a schema
	if gj.anyDatabaseReady() {
		if err = gj.initAllowList(); err != nil {
			return
		}

		if err = gj.prepareRoleStmt(); err != nil {
			return
		}

		if err = gj.initIntro(); err != nil {
			return
		}
	}

	if conf.SecretKey != "" {
		sk := sha256.Sum256([]byte(conf.SecretKey))
		gj.encryptionKey = sk
		gj.encryptionKeySet = true
	}

	g.Store(gj)
	return
}

func OptionSetNamespace(namespace string) Option {
	return func(s *graphjinEngine) error {
		s.namespace = namespace
		return nil
	}
}

// OptionSetFS sets the file system to be used by GraphJin
func OptionSetFS(fs FS) Option {
	return func(s *graphjinEngine) error {
		s.fs = fs
		return nil
	}
}

// OptionSetSavedQuerySaveHook installs a service hook for dev-mode named-query
// auto-save. If the hook returns handled=false, GraphJin writes to the
// configured filesystem allow-list as before.
func OptionSetSavedQuerySaveHook(h SavedQuerySaveHook) Option {
	return func(s *graphjinEngine) error {
		s.savedQuerySaveHook = h
		return nil
	}
}

// OptionSetReservedRoleAuthorizer installs an internal authorization hook for
// reserved role names. Reserved roles are never accepted from request context
// unless this hook explicitly allows the role for that context.
func OptionSetReservedRoleAuthorizer(h ReservedRoleAuthorizer) Option {
	return func(s *graphjinEngine) error {
		s.reservedRoleAuthorizer = h
		return nil
	}
}

// OptionSetRuntimeSchemaDDLDir overrides where generated schema restart
// snapshots are read from and written to. Relative and absolute paths are
// passed through to the configured filesystem implementation unchanged.
func OptionSetRuntimeSchemaDDLDir(dir string) Option {
	return func(s *graphjinEngine) error {
		s.runtimeSchemaDDLDir = strings.TrimSpace(dir)
		return nil
	}
}

// OptionSetRuntimeSchemaCacheFirst makes runtime discovery snapshots the first
// source consulted during initialization. Direct core users retain live-first
// discovery unless they opt in.
func OptionSetRuntimeSchemaCacheFirst(enabled bool) Option {
	return func(s *graphjinEngine) error {
		s.runtimeSchemaCacheFirst = enabled
		return nil
	}
}

// OptionSetRuntimeSchemaCacheRequired prevents a cache-initialized engine from
// falling through to live discovery when an activated generation is incomplete.
func OptionSetRuntimeSchemaCacheRequired(required bool) Option {
	return func(s *graphjinEngine) error {
		s.runtimeSchemaCacheRequired = required
		return nil
	}
}

// OptionSetDBSchemaWatcherDisabled lets a service-level coordinator own schema
// polling so horizontally scaled replicas do not all introspect independently.
func OptionSetDBSchemaWatcherDisabled(disabled bool) Option {
	return func(s *graphjinEngine) error {
		s.disableDBSchemaWatcher = disabled
		return nil
	}
}

// OptionSetTrace sets the tracer to be used by GraphJin
func OptionSetTrace(trace Tracer) Option {
	return func(s *graphjinEngine) error {
		s.trace = trace
		return nil
	}
}

// OptionSetResolver sets the resolver function to be used by GraphJin
func OptionSetResolver(name string, fn ResolverFn) Option {
	return func(s *graphjinEngine) error {
		if s.rtmap == nil {
			s.rtmap = s.newRTMap()
		}
		if _, ok := s.rtmap[name]; ok {
			return fmt.Errorf("duplicate resolver: %s", name)
		}
		s.rtmap[name] = fn
		return nil
	}
}

// OptionSetFilesystemBackend registers a filesystem backend factory by
// name. The engine ships with the "local" factory built in; "s3" and
// "gcs" are registered by the serv package (where their SDK
// dependencies live). Custom backends can be plugged in here too —
// e.g. an Azure Blob factory in user code:
//
//	gj, _ := core.NewGraphJin(conf, db,
//	    core.OptionSetFilesystemBackend("azureblob", func(c core.FilesystemConfig) (fstable.Backend, error) {
//	        return myazure.New(c.Bucket, c.Region)
//	    }),
//	)
func OptionSetFilesystemBackend(name string, factory FilesystemBackendFactory) Option {
	return func(s *graphjinEngine) error {
		if name == "" {
			return errors.New("filesystem backend: name is required")
		}
		if factory == nil {
			return errors.New("filesystem backend: factory is nil")
		}
		if s.fsFactories == nil {
			s.fsFactories = make(map[string]FilesystemBackendFactory)
		}
		if _, ok := s.fsFactories[name]; ok {
			return fmt.Errorf("filesystem backend: duplicate name %q", name)
		}
		s.fsFactories[name] = factory
		return nil
	}
}

// OptionSetResponseCache sets the response cache provider for caching query results.
// The cache provider is typically the Redis cache from the serv package.
func OptionSetResponseCache(cache ResponseCacheProvider) Option {
	return func(s *graphjinEngine) error {
		s.responseCache = cache
		s.cacheKeyBuilder = NewCacheKeyBuilder()
		return nil
	}
}

// ManagedMutationField describes one selected return field on a managed
// mutation root.
type ManagedMutationField struct {
	Name   string
	Column string
}

// ManagedColumn describes a synthetic service-owned table column.
type ManagedColumn struct {
	Name       string
	Type       string
	PrimaryKey bool
	FullText   bool
}

// ManagedTable describes a synthetic service-owned GraphQL table.
type ManagedTable struct {
	Name    string
	Columns []ManagedColumn
}

// ManagedOrderBy describes one order_by entry on a managed query root.
type ManagedOrderBy struct {
	Column string
	Order  string
}

// ManagedQueryRoot describes one root query routed to a managed handler.
type ManagedQueryRoot struct {
	FieldName string
	Table     string
	Fields    []ManagedMutationField
	Where     map[string]interface{}
	OrderBy   []ManagedOrderBy
	Limit     int
	Offset    int
}

// ManagedQueryRequest is passed to service-owned query handlers.
type ManagedQueryRequest struct {
	Database string
	Roots    []ManagedQueryRoot
}

// ManagedQueryHandler handles GraphQL queries for service-managed tables.
type ManagedQueryHandler interface {
	ManagedQueryTables() []ManagedTable
	ExecuteManagedQuery(context.Context, ManagedQueryRequest) (json.RawMessage, error)
}

// ManagedMutationRoot describes one root mutation routed to a managed handler.
type ManagedMutationRoot struct {
	FieldName string
	Table     string
	Operation string
	Input     map[string]interface{}
	Where     map[string]interface{}
	Fields    []ManagedMutationField
}

// ManagedMutationRequest is passed to service-owned mutation handlers.
type ManagedMutationRequest struct {
	Database  string
	Operation string
	Roots     []ManagedMutationRoot
}

// ManagedMutationHandler handles GraphQL mutations for service-managed tables.
type ManagedMutationHandler interface {
	ManagedMutationTables() []string
	ExecuteManagedMutation(context.Context, ManagedMutationRequest) (json.RawMessage, error)
}

// OptionSetManagedMutationHandler registers a handler for service-managed
// mutation tables in a database.
func OptionSetManagedMutationHandler(database string, handler ManagedMutationHandler) Option {
	return func(s *graphjinEngine) error {
		if database == "" {
			database = s.defaultDB
		}
		if handler == nil {
			return errors.New("managed mutation handler: nil handler")
		}
		if s.managedMutationHandlers == nil {
			s.managedMutationHandlers = make(map[string]ManagedMutationHandler)
		}
		combined, err := combineManagedMutationHandlers(s.managedMutationHandlers[database], handler)
		if err != nil {
			return err
		}
		s.managedMutationHandlers[database] = combined
		return nil
	}
}

// OptionSetManagedQueryHandler registers a handler for service-managed query
// tables in a database.
func OptionSetManagedQueryHandler(database string, handler ManagedQueryHandler) Option {
	return func(s *graphjinEngine) error {
		if database == "" {
			database = s.defaultDB
		}
		if handler == nil {
			return errors.New("managed query handler: nil handler")
		}
		if s.managedQueryHandlers == nil {
			s.managedQueryHandlers = make(map[string]ManagedQueryHandler)
		}
		combined, err := combineManagedQueryHandlers(s.managedQueryHandlers[database], handler)
		if err != nil {
			return err
		}
		s.managedQueryHandlers[database] = combined
		return nil
	}
}

type Error struct {
	Message    string         `json:"message"`
	Extensions map[string]any `json:"extensions,omitempty"`
}

// Result struct contains the output of the GraphQL function this includes resulting json from the
// database query and any error information
type Result struct {
	namespace    string
	operation    qcode.QType
	name         string
	sql          string
	role         string
	cacheControl string
	cacheHit     bool
	subCursors   map[string]string
	rootLimits   []RootLimitInfo
	Vars         json.RawMessage   `json:"-"`
	Data         json.RawMessage   `json:"data,omitempty"`
	Hash         [sha256.Size]byte `json:"-"`
	Errors       []Error           `json:"errors,omitempty"`
	Validation   []qcode.ValidErr  `json:"validation,omitempty"`
	// Extensions   *extensions     `json:"extensions,omitempty"`
}

// RequestConfig is used to pass request specific config values to the GraphQL and Subscribe functions. Dynamic variables can be set here.
type RequestConfig struct {
	ns *string

	// APQKey is set when using GraphJin with automatic persisted queries
	APQKey string

	// Pass additional variables complex variables such as functions that return string values.
	Vars map[string]interface{}

	// Execute this query as part of a transaction
	Tx *sql.Tx
}

// SubscriptionRootInfo describes a root selected by a subscription member.
// Filter contains the resolved user filter and excludes GraphJin's synthetic
// cursor seek predicate.
type SubscriptionRootInfo struct {
	FieldName string
	Table     string
	Database  string
	CursorVar string
	Filter    map[string]any
}

// SetNamespace is used to set namespace requests within a single instance of GraphJin. For example queries with the same name
func (rc *RequestConfig) SetNamespace(ns string) {
	rc.ns = &ns
}

// GetNamespace is used to get the namespace requests within a single instance of GraphJin
func (rc *RequestConfig) GetNamespace() (string, bool) {
	if rc.ns != nil {
		return *rc.ns, true
	}
	return "", false
}

// GraphQL function is our main function it takes a GraphQL query compiles it
// to SQL and executes returning the resulting JSON.
//
// In production mode the compiling happens only once and from there on the compiled queries
// are directly executed.
//
// In developer mode all named queries are saved into the queries folder and in production mode only
// queries from these saved queries can be used.
func (g *GraphJin) GraphQL(c context.Context,
	query string,
	vars json.RawMessage,
	rc *RequestConfig,
) (res *Result, err error) {
	gj, err := g.getEngine()
	if err != nil {
		return
	}

	c1, span := gj.spanStart(c, "GraphJin Query")
	defer span.End()

	var queryBytes []byte
	var inCache bool

	// get query from apq cache if apq key exists
	if rc != nil && rc.APQKey != "" {
		queryBytes, inCache = gj.cache.Get(APQ_PX + rc.APQKey)
	}

	// query not found in apq cache so use original query
	if len(queryBytes) == 0 {
		queryBytes = []byte(query)
	}

	// fast extract name and query type from query
	h, err := graph.FastParseBytes(queryBytes)
	if err != nil {
		return
	}
	r := gj.newGraphqlReq(rc, h.Operation, h.Name, queryBytes, vars)

	// if production security enabled then get query and metadata
	// from allow list
	if gj.prodSec {
		var item allow.Item
		item, err = gj.allowList.GetByName(h.Name, true)
		if err != nil {
			err = fmt.Errorf("%w: %s", err, h.Name)
			return
		}
		r.Set(item)
	}

	// do the query
	resp, err := gj.query(c1, r)
	res = &resp.res
	if err != nil {
		return
	}

	// save to apq cache is apq key exists and not already in cache
	if !inCache && rc != nil && rc.APQKey != "" {
		gj.cache.Set((APQ_PX + rc.APQKey), r.query)
	}

	// Development learning saves named queries to the allow list. Agentic mode
	// keeps dynamic authoring enabled but does not mint permanent query entries.
	if gj.learn && r.name != "" && r.name != "IntrospectionQuery" {
		if err = gj.saveToAllowList(c, resp.qc, resp.res.namespace); err != nil {
			return
		}
	}
	return
}

// GraphQLTx is similiar to the GraphQL function except that it can be used
// within a database transactions.
func (g *GraphJin) GraphQLTx(c context.Context,
	tx *sql.Tx,
	query string,
	vars json.RawMessage,
	rc *RequestConfig,
) (res *Result, err error) {
	if rc == nil {
		rc = &RequestConfig{Tx: tx}
	} else {
		rc.Tx = tx
	}
	return g.GraphQL(c, query, vars, rc)
}

// GraphQLByName is similar to the GraphQL function except that queries saved
// in the queries folder can directly be used just by their name (filename).
func (g *GraphJin) GraphQLByName(c context.Context,
	name string,
	vars json.RawMessage,
	rc *RequestConfig,
) (res *Result, err error) {
	gj, err := g.getEngine()
	if err != nil {
		return
	}

	c1, span := gj.spanStart(c, "GraphJin Query")
	defer span.End()

	item, err := gj.allowList.GetByName(name, gj.prod)
	if err != nil {
		err = fmt.Errorf("%w: %s", err, name)
		return
	}

	r := gj.newGraphqlReq(rc, "", name, nil, vars)
	r.Set(item)

	res, err = gj.queryWithResult(c1, r)
	return
}

// GraphQLBySavedQuery executes a saved query definition supplied by the
// embedding service. It is intended for trusted stores that have already
// resolved the saved query and its imports.
func (g *GraphJin) GraphQLBySavedQuery(c context.Context,
	details *SavedQueryDetails,
	vars json.RawMessage,
	rc *RequestConfig,
) (res *Result, err error) {
	if details == nil {
		return nil, errors.New("saved query details are required")
	}
	if strings.TrimSpace(details.Name) == "" {
		return nil, errors.New("saved query name is required")
	}
	if strings.TrimSpace(details.Query) == "" {
		return nil, errors.New("saved query query is required")
	}
	gj, err := g.getEngine()
	if err != nil {
		return
	}

	c1, span := gj.spanStart(c, "GraphJin Query")
	defer span.End()

	operation := strings.TrimSpace(details.Operation)
	if operation == "" {
		h, parseErr := graph.FastParseBytes([]byte(details.Query))
		if parseErr != nil {
			return nil, parseErr
		}
		operation = h.Operation
	}

	item := allow.Item{
		Namespace:  details.Namespace,
		Name:       details.Name,
		Operation:  operation,
		Query:      []byte(details.Query),
		ActionJSON: savedQueryVariablesToRaw(details.Variables),
	}
	r := gj.newGraphqlReq(rc, "", details.Name, nil, vars)
	r.Set(item)

	res, err = gj.queryWithResult(c1, r)
	return
}

// GraphQLByNameTx is similiar to the GraphQLByName function except
// that it can be used within a database transactions.
func (g *GraphJin) GraphQLByNameTx(c context.Context,
	tx *sql.Tx,
	name string,
	vars json.RawMessage,
	rc *RequestConfig,
) (res *Result, err error) {
	if rc == nil {
		rc = &RequestConfig{Tx: tx}
	} else {
		rc.Tx = tx
	}
	return g.GraphQLByName(c, name, vars, rc)
}

type GraphqlReq struct {
	namespace     string
	operation     qcode.QType
	name          string
	query         []byte
	vars          json.RawMessage
	aschema       map[string]json.RawMessage
	requestconfig *RequestConfig
}

type GraphqlResponse struct {
	res Result
	qc  *qcode.QCode
}

// newGraphqlReq creates a new GraphQL request
func (gj *graphjinEngine) newGraphqlReq(rc *RequestConfig,
	op string,
	name string,
	query []byte,
	vars json.RawMessage,
) (r GraphqlReq) {
	r = GraphqlReq{
		operation: qcode.GetQTypeByName(op),
		name:      name,
		query:     query,
		vars:      vars,
	}

	if rc != nil {
		r.requestconfig = rc
	}
	if rc != nil && rc.ns != nil {
		r.namespace = *rc.ns
	} else {
		r.namespace = gj.namespace
	}
	return
}

// Set is used to set the namespace, operation type, name and query for the GraphQL request
func (r *GraphqlReq) Set(item allow.Item) {
	r.namespace = item.Namespace
	r.operation = qcode.GetQTypeByName(item.Operation)
	r.name = item.Name
	r.query = item.Query
	r.aschema = item.ActionJSON
}

// GraphQL function is our main function it takes a GraphQL query compiles it
func (gj *graphjinEngine) queryWithResult(c context.Context, r GraphqlReq) (res *Result, err error) {
	resp, err := gj.query(c, r)
	return &resp.res, err
}

// GraphQL function is our main function it takes a GraphQL query compiles it
func (gj *graphjinEngine) query(c context.Context, r GraphqlReq) (
	resp GraphqlResponse, err error,
) {
	resp.res = Result{
		namespace: r.namespace,
		operation: r.operation,
		name:      r.name,
	}

	if !gj.prodSec && r.name == "IntrospectionQuery" {
		resp.res.Data, err = gj.introQueryForContext(c)
		return
	}

	// Federation v2: intercept `_service { sdl }` and `_entities` queries
	// before the normal compile pipeline. The compiler doesn't know about
	// these fields and would reject them as unknown root selections.
	if gj.conf.Federation.Enabled && r.operation == qcode.QTQuery {
		handled, data, ferr := gj.handleFederationQuery(r)
		if ferr != nil {
			resp.res.Errors = newError(string(r.query), ferr)
			err = ferr
			return
		}
		if handled {
			resp.res.Data = data
			return
		}
	}

	if r.operation == qcode.QTSubscription {
		err = errors.New("use 'core.Subscribe' for subscriptions")
		return
	}

	if !gj.anyDatabaseReady() {
		err = fmt.Errorf("no tables found in any database; schema not initialized")
		return
	}

	s, err := newGState(c, gj, r)
	if err != nil {
		return
	}
	err = s.compileAndExecuteWrapper(c)

	resp.qc = s.qcode()
	resp.res.rootLimits = rootLimitInfoFromQCode(resp.qc)
	resp.res.sql = s.sql()
	resp.res.cacheControl = s.cacheHeader()
	resp.res.Vars = r.vars
	// Strip internal __gj_id fields unconditionally when cache tracking is enabled.
	// This handles all code paths: cache hits, multi-DB queries, and regular queries.
	if gj.conf.CacheTrackingEnabled {
		s.data = stripGjIdFields(s.data)
	}
	resp.res.Data = json.RawMessage(s.data)
	resp.res.Hash = s.dhash
	resp.res.role = s.role
	resp.res.cacheHit = s.cacheHit || (s.fragmentHits.Load() > 0 && s.fragmentMisses.Load() == 0)

	if err != nil {
		resp.res.Errors = newError(string(r.query), err)
	}

	if len(s.verrs) != 0 {
		resp.res.Validation = s.verrs
	}
	return
}

// Reload redoes database discover and reinitializes GraphJin.
func (g *GraphJin) Reload() error {
	g.reloadMu.Lock()
	defer g.reloadMu.Unlock()
	gj, err := g.getEngine()
	if err != nil {
		return err
	}
	var db *sql.DB
	if pdb := gj.primaryDB(); pdb != nil {
		db = pdb.db
	}
	reloadConf := gj.conf
	if gj.catalogConf != nil {
		reloadConf = gj.catalogConf
	}
	if err := g.newGraphJin(reloadConf, db, nil, gj.fs, gj.opts...); err != nil {
		return err
	}
	g.fireAllSchemaCallbacks()
	return nil
}

// ReloadDatabase rediscovers and rebuilds one configured database context.
// It is intended for source-local schema changes. Call Reload for global
// config, auth, relationship, resolver, API, filesystem, or workflow changes.
func (g *GraphJin) ReloadDatabase(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("database name is required")
	}
	g.reloadMu.Lock()
	defer g.reloadMu.Unlock()

	gj, err := g.getEngine()
	if err != nil {
		return err
	}
	if _, ok := gj.databases[name]; !ok {
		return fmt.Errorf("database %q not configured", name)
	}
	if err := g.newGraphJinReloadingDatabase(gj, name); err != nil {
		return err
	}
	next, err := g.getEngine()
	if err != nil {
		return err
	}
	if ctx, ok := next.databases[name]; ok && ctx.dbinfo != nil {
		g.fireSchemaCallbacks(name, fmt.Sprintf("%x", ctx.dbinfo.Hash()))
	}
	return nil
}

// ReloadConfigDatabases swaps to conf while rediscoving and rebuilding only the
// named database contexts. It is intended for service-owned config commits that
// have already proved the change is database-source scoped. Use Reload for
// global config, auth, relationship, resolver, API, filesystem, or workflow
// changes.
func (g *GraphJin) ReloadConfigDatabases(conf *Config, databases []string, opts ...Option) error {
	if conf == nil {
		return fmt.Errorf("config is required")
	}
	reloadSet := make(map[string]struct{}, len(databases))
	for _, name := range databases {
		name = strings.TrimSpace(name)
		if name != "" {
			reloadSet[name] = struct{}{}
		}
	}
	if len(reloadSet) == 0 {
		return fmt.Errorf("at least one database name is required")
	}

	g.reloadMu.Lock()
	defer g.reloadMu.Unlock()

	base, err := g.getEngine()
	if err != nil {
		return err
	}
	if len(opts) == 0 {
		opts = base.opts
	}
	if err := g.newGraphJinReloadingConfigDatabases(base, conf, reloadSet, opts...); err != nil {
		return err
	}
	next, err := g.getEngine()
	if err != nil {
		return err
	}
	names := make([]string, 0, len(reloadSet))
	for name := range reloadSet {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if ctx, ok := next.databases[name]; ok && ctx.dbinfo != nil {
			g.fireSchemaCallbacks(name, fmt.Sprintf("%x", ctx.dbinfo.Hash()))
		}
	}
	return nil
}

// ReloadWithDB redoes database discover with a new primary DB connection.
func (g *GraphJin) ReloadWithDB(db *sql.DB) error {
	g.reloadMu.Lock()
	defer g.reloadMu.Unlock()
	gj, err := g.getEngine()
	if err != nil {
		return err
	}
	reloadConf := gj.conf
	if gj.catalogConf != nil {
		reloadConf = gj.catalogConf
	}
	return g.newGraphJin(reloadConf, db, nil, gj.fs, gj.opts...)
}

// ReloadFromRuntimeSchemaCache atomically rebuilds the engine from a complete,
// activated runtime schema generation. It never falls through to live database
// discovery, which keeps horizontally scaled replicas on the same generation.
func (g *GraphJin) ReloadFromRuntimeSchemaCache(dir string) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return fmt.Errorf("runtime schema cache directory is required")
	}
	g.reloadMu.Lock()
	defer g.reloadMu.Unlock()
	base, err := g.getEngine()
	if err != nil {
		return err
	}
	var db *sql.DB
	if primary := base.primaryDB(); primary != nil {
		db = primary.db
	}
	reloadConf := base.conf
	if base.catalogConf != nil {
		reloadConf = base.catalogConf
	}
	opts := append([]Option(nil), base.opts...)
	opts = append(opts,
		OptionSetRuntimeSchemaDDLDir(dir),
		OptionSetRuntimeSchemaCacheFirst(true),
		OptionSetRuntimeSchemaCacheRequired(true),
	)
	if err := g.newGraphJin(reloadConf, db, nil, base.fs, opts...); err != nil {
		return err
	}
	g.fireAllSchemaCallbacks()
	return nil
}

func (g *GraphJin) newGraphJinReloadingConfigDatabases(base *graphjinEngine, nextConf *Config, reloadSet map[string]struct{}, opts ...Option) error {
	if base == nil {
		return errors.New("graphjin engine is not initialized")
	}
	conf := nextConf.clone()
	if conf.IsSourcesUsed() {
		for i := range conf.Tables {
			if conf.Tables[i].Source == "" && conf.Tables[i].Database != "" {
				conf.Tables[i].Source = conf.Tables[i].Database
			}
		}
	}
	if err := conf.NormalizeMode(); err != nil {
		return err
	}

	log := base.log
	if log == nil {
		log = _log.New(os.Stderr, "", 0)
	}
	trace := base.trace
	if trace == nil {
		trace = &tracer{}
	}
	printFormat := append([]byte(nil), base.printFormat...)
	if len(printFormat) == 0 {
		printFormat = []byte(fmt.Sprintf("gj-%x:", time.Now().UnixNano()))
	}

	gj := &graphjinEngine{
		conf:        conf,
		log:         log,
		prod:        conf.Production,
		prodSec:     conf.Production,
		learn:       !conf.Production && !isAgenticMode(conf),
		printFormat: printFormat,
		opts:        opts,
		fs:          base.fs,
		trace:       trace,
		namespace:   base.namespace,
		done:        g.done,
	}
	if gj.conf.DisableProdSecurity {
		gj.prodSec = false
	}
	if err := gj.initCache(); err != nil {
		return err
	}
	if err := gj.initConfig(); err != nil {
		return err
	}
	gj.defaultDB = gj.conf.defaultDatabaseName()

	for _, op := range opts {
		if err := op(gj); err != nil {
			return err
		}
	}
	gj.catalogConf = gj.conf.clone()
	if gj.databases == nil {
		gj.databases = make(map[string]*dbContext)
	}

	for name, ctx := range gj.databases {
		if _, reload := reloadSet[name]; reload {
			continue
		}
		baseCtx := base.databases[name]
		if baseCtx == nil {
			reloadSet[name] = struct{}{}
			continue
		}
		if !reflect.DeepEqual(base.conf.Databases[name], conf.Databases[name]) || baseCtx.db != ctx.db || baseCtx.nano != ctx.nano {
			reloadSet[name] = struct{}{}
			continue
		}
		gj.databases[name] = cloneDBContextForReload(baseCtx)
	}

	reloadDefault := base.defaultDB != gj.defaultDB
	for name := range reloadSet {
		if name == gj.defaultDB {
			reloadDefault = true
			break
		}
	}
	if reloadDefault {
		if err := gj.initResolvers(); err != nil {
			return err
		}
	} else {
		gj.rtmap = base.rtmap
		gj.rmap = base.rmap
		gj.openapiRuntime = base.openapiRuntime
		gj.fsBackends = base.fsBackends
	}

	reloadNames := make([]string, 0, len(reloadSet))
	for name := range reloadSet {
		reloadNames = append(reloadNames, name)
	}
	sort.Strings(reloadNames)
	for _, name := range reloadNames {
		target := gj.databases[name]
		if target == nil {
			continue
		}
		target.dbinfo = nil
		target.schema = nil
		target.qcodeCompiler = nil
		target.psqlCompiler = nil
		if target.nano != nil {
			snap := target.nano.Snapshot()
			if snap == nil {
				return fmt.Errorf("database %q has no nanodb snapshot", name)
			}
			target.dbtype = "nanodb"
			target.dbinfo = snap.DBInfo(name)
		} else if err := gj.discoverDatabase(target); err != nil {
			return err
		}
		if handler := gj.managedQueryHandlers[name]; handler != nil {
			if err := gj.initManagedQueryTablesForDatabase(name, handler); err != nil {
				return err
			}
		}
		if err := gj.finalizeDatabaseSchema(target); err != nil {
			return err
		}
		if target.nano == nil && !schemaHasApplicationTables(target.schema) {
			return fmt.Errorf("database connected but schema discovery found no tables")
		}
	}
	if gj.anyDatabaseReady() {
		if err := gj.initAllowList(); err != nil {
			return err
		}
		if err := gj.prepareRoleStmt(); err != nil {
			return err
		}
		if err := gj.initIntro(); err != nil {
			return err
		}
	}
	if conf.SecretKey != "" {
		sk := sha256.Sum256([]byte(conf.SecretKey))
		gj.encryptionKey = sk
		gj.encryptionKeySet = true
	}
	g.Store(gj)
	return nil
}

func (g *GraphJin) newGraphJinReloadingDatabase(base *graphjinEngine, database string) error {
	if base == nil {
		return errors.New("graphjin engine is not initialized")
	}
	conf := base.conf.clone()
	if conf.IsSourcesUsed() {
		for i := range conf.Tables {
			if conf.Tables[i].Source == "" && conf.Tables[i].Database != "" {
				conf.Tables[i].Source = conf.Tables[i].Database
			}
		}
	}
	if err := conf.NormalizeMode(); err != nil {
		return err
	}

	log := base.log
	if log == nil {
		log = _log.New(os.Stderr, "", 0)
	}
	trace := base.trace
	if trace == nil {
		trace = &tracer{}
	}
	printFormat := append([]byte(nil), base.printFormat...)
	if len(printFormat) == 0 {
		printFormat = []byte(fmt.Sprintf("gj-%x:", time.Now().UnixNano()))
	}

	gj := &graphjinEngine{
		conf:        conf,
		log:         log,
		prod:        conf.Production,
		prodSec:     conf.Production,
		learn:       !conf.Production && !isAgenticMode(conf),
		printFormat: printFormat,
		opts:        base.opts,
		fs:          base.fs,
		trace:       trace,
		namespace:   base.namespace,
		done:        g.done,
	}
	if gj.conf.DisableProdSecurity {
		gj.prodSec = false
	}
	if err := gj.initCache(); err != nil {
		return err
	}
	if err := gj.initConfig(); err != nil {
		return err
	}
	gj.defaultDB = base.defaultDB
	if gj.defaultDB == "" {
		gj.defaultDB = gj.conf.defaultDatabaseName()
	}

	for _, op := range base.opts {
		if err := op(gj); err != nil {
			return err
		}
	}
	if base.catalogConf != nil {
		gj.catalogConf = base.catalogConf.clone()
	} else {
		gj.catalogConf = gj.conf.clone()
	}

	gj.databases = make(map[string]*dbContext, len(base.databases))
	for name, ctx := range base.databases {
		gj.databases[name] = cloneDBContextForReload(ctx)
	}
	target := gj.databases[database]
	if target == nil {
		return fmt.Errorf("database %q not configured", database)
	}
	target.dbinfo = nil
	target.schema = nil
	target.qcodeCompiler = nil
	target.psqlCompiler = nil

	if target.nano != nil {
		snap := target.nano.Snapshot()
		if snap == nil {
			return fmt.Errorf("database %q has no nanodb snapshot", database)
		}
		target.dbtype = "nanodb"
		target.dbinfo = snap.DBInfo(database)
	} else if err := gj.discoverDatabase(target); err != nil {
		return err
	}

	if database == gj.defaultDB {
		if err := gj.initResolvers(); err != nil {
			return err
		}
	} else {
		gj.rtmap = base.rtmap
		gj.rmap = base.rmap
		gj.openapiRuntime = base.openapiRuntime
		gj.fsBackends = base.fsBackends
	}
	if handler := gj.managedQueryHandlers[database]; handler != nil {
		if err := gj.initManagedQueryTablesForDatabase(database, handler); err != nil {
			return err
		}
	}
	if err := gj.finalizeDatabaseSchema(target); err != nil {
		return err
	}
	if gj.anyDatabaseReady() {
		if err := gj.initAllowList(); err != nil {
			return err
		}
		if err := gj.prepareRoleStmt(); err != nil {
			return err
		}
		if err := gj.initIntro(); err != nil {
			return err
		}
	}
	if conf.SecretKey != "" {
		sk := sha256.Sum256([]byte(conf.SecretKey))
		gj.encryptionKey = sk
		gj.encryptionKeySet = true
	}
	g.Store(gj)
	return nil
}

func cloneDBContextForReload(ctx *dbContext) *dbContext {
	if ctx == nil {
		return nil
	}
	return &dbContext{
		name:          ctx.name,
		db:            ctx.db,
		dbtype:        ctx.dbtype,
		nano:          ctx.nano,
		dbinfo:        ctx.dbinfo,
		schema:        ctx.schema,
		qcodeCompiler: ctx.qcodeCompiler,
		psqlCompiler:  ctx.psqlCompiler,
	}
}

// SetOptions replaces the options slice so the next Reload picks them up.
func (g *GraphJin) SetOptions(opts ...Option) {
	g.reloadMu.Lock()
	defer g.reloadMu.Unlock()
	gj, err := g.getEngine()
	if err != nil {
		return
	}
	gj.opts = opts
}

// IsProd return true for production mode or false for development mode
func (g *GraphJin) IsProd() bool {
	gj, err := g.getEngine()
	if err != nil {
		return false
	}
	return gj.prod
}

type Header struct {
	Type OpType
	Name string
}

// Operation function return the operation type and name from the query.
// It uses a very fast algorithm to extract the operation without having to parse the query.
func Operation(query string) (h Header, err error) {
	if v, err := graph.FastParse(query); err == nil {
		h.Type = OpType(qcode.GetQTypeByName(v.Operation))
		h.Name = v.Name
	}
	return
}

// getFS returns the file system to be used by GraphJin
func getFS(conf *Config) (fs FS, err error) {
	if v, ok := conf.FS.(FS); ok {
		fs = v
		return
	}

	v, err := os.Getwd()
	if err != nil {
		return
	}

	fs = NewOsFS(filepath.Join(v, "config"))
	return
}

// newError creates a new error list
func newError(query string, err error) (errList []Error) {
	e := Error{Message: err.Error()}
	if repair := BuildGraphJinErrorRepair(query, err.Error()); repair.Known() {
		e.Extensions = map[string]any{"graphjin_repair": repair}
	}
	errList = []Error{e}
	return
}

// stripGjIdFields removes all "__gj_id" fields from JSON response.
// Uses JSON parse/delete/marshal for correctness - doesn't depend on QCode.
// This is used to unconditionally strip internal tracking fields from all responses,
// including cache hits where s.cs is nil.
func stripGjIdFields(data []byte) []byte {
	if len(data) == 0 {
		return data
	}

	result, err := stripCacheTrackingFields(data)
	if err != nil {
		return data // Return original on strip error
	}
	return result
}

// TableInfo represents basic table information for MCP/API consumers
type TableInfo struct {
	Name        string `json:"name"`
	Schema      string `json:"schema,omitempty"`
	Database    string `json:"database,omitempty"`
	Type        string `json:"type"` // table, view, etc.
	Comment     string `json:"comment,omitempty"`
	ColumnCount int    `json:"column_count"`
}

// ColumnInfo represents column information for MCP/API consumers
type ColumnInfo struct {
	Name                string `json:"name"`
	Type                string `json:"type"`
	Nullable            bool   `json:"nullable"`
	PrimaryKey          bool   `json:"primary_key"`
	ForeignKey          string `json:"foreign_key,omitempty"` // "schema.table.column" if FK
	Array               bool   `json:"array,omitempty"`
	Default             string `json:"default,omitempty"`
	UniqueKey           bool   `json:"unique_key,omitempty"`
	Index               bool   `json:"index,omitempty"`
	IndexName           string `json:"index_name,omitempty"`
	FullText            bool   `json:"full_text,omitempty"`
	ForeignKeyDatabase  string `json:"foreign_key_database,omitempty"`
	ForeignKeyRecursive bool   `json:"foreign_key_recursive,omitempty"`
}

// RelationInfo represents a relationship between tables
type RelationInfo struct {
	Name       string `json:"name"`              // Field name to use in queries
	Table      string `json:"table"`             // Related table name
	Type       string `json:"type"`              // one_to_one, one_to_many, many_to_many
	ForeignKey string `json:"foreign_key"`       // The FK column
	Through    string `json:"through,omitempty"` // Join table for many-to-many
}

// TableSchema represents full table schema with relationships
type TableSchema struct {
	Name                 string       `json:"name"`
	Schema               string       `json:"schema,omitempty"`
	Database             string       `json:"database,omitempty"`
	Type                 string       `json:"type"`
	Comment              string       `json:"comment,omitempty"`
	Blocked              bool         `json:"blocked,omitempty"`
	PrimaryKey           string       `json:"primary_key,omitempty"`
	PrimaryKeys          []string     `json:"primary_keys,omitempty"`
	FullTextColumns      []string     `json:"full_text_columns,omitempty"`
	PartitionKey         string       `json:"partition_key,omitempty"`
	ImplicitPartitionKey string       `json:"implicit_partition_key,omitempty"`
	Columns              []ColumnInfo `json:"columns"`
	Relationships        struct {
		Outgoing []RelationInfo `json:"outgoing"` // Tables this table references
		Incoming []RelationInfo `json:"incoming"` // Tables that reference this table
	} `json:"relationships"`
}

// FunctionParam represents a function input or output parameter
type FunctionParam struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Array bool   `json:"array,omitempty"`
}

// FunctionInfo represents a database function
type FunctionInfo struct {
	Name      string          `json:"name"`
	Schema    string          `json:"schema,omitempty"`
	Type      string          `json:"type"`
	Comment   string          `json:"comment,omitempty"`
	Aggregate bool            `json:"aggregate,omitempty"`
	Inputs    []FunctionParam `json:"inputs,omitempty"`
	Outputs   []FunctionParam `json:"outputs,omitempty"`
}

// PathStep represents a step in a relationship path
type PathStep struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Via      string `json:"via,omitempty"` // Column or join table
	Relation string `json:"relation"`      // Relationship type
}

// GetTables returns a list of all tables across all databases (in multi-DB mode)
// or from the default database (in single-DB mode)
func (g *GraphJin) GetTables() []TableInfo {
	gj, err := g.getEngine()
	if err != nil {
		return nil
	}
	return gj.getTables("")
}

// SchemaReady returns true if the engine has a usable schema.
// Use this to check before calling methods that access the schema
// to avoid nil pointer dereferences during partial initialization.
func (g *GraphJin) SchemaReady() bool {
	gj, ok := g.Load().(*graphjinEngine)
	if !ok || gj == nil {
		return false
	}
	for _, ctx := range gj.databases {
		if schemaHasApplicationTables(ctx.schema) {
			return true
		}
	}
	return false
}

func schemaHasApplicationTables(schema *sdata.DBSchema) bool {
	if schema == nil {
		return false
	}
	for _, table := range schema.GetTables() {
		name := strings.ToLower(table.Name)
		if table.Blocked || table.Type == "managed" {
			continue
		}
		switch name {
		case "gj_catalog", "gj_security", "gj_workflow", "gj_config", "gj_code":
			return true
		}
		if strings.HasPrefix(name, "gj_") {
			continue
		}
		return true
	}
	return false
}

// GetTablesForDatabase returns tables from a specific database.
// If database is empty, returns tables from all databases.
func (g *GraphJin) GetTablesForDatabase(database string) []TableInfo {
	gj, err := g.getEngine()
	if err != nil {
		return nil
	}
	return gj.getTables(database)
}

func (g *GraphJin) EffectiveAnalyticsMode(database string) bool {
	gj, err := g.getEngine()
	if err != nil {
		return false
	}
	return gj.conf.EffectiveAnalyticsMode(database)
}

// DBForDatabase returns the read-only connection pool and database type for
// `database`. Empty name resolves to the default database. Do not close the pool.
func (g *GraphJin) DBForDatabase(database string) (*sql.DB, string, error) {
	gj, err := g.getEngine()
	if err != nil {
		return nil, "", err
	}
	ctx, ok := gj.GetDatabase(database)
	if !ok || ctx == nil {
		if database == "" {
			return nil, "", fmt.Errorf("no default database configured")
		}
		return nil, "", fmt.Errorf("database %q not configured", database)
	}
	if ctx.db == nil {
		return nil, ctx.dbtype, fmt.Errorf("database %q has no active connection", database)
	}
	return ctx.db, ctx.dbtype, nil
}

// getTables returns tables, optionally filtered by database name.
// With empty database, returns tables from all databases.
func (gj *graphjinEngine) getTables(database string) []TableInfo {
	var result []TableInfo
	for _, dbName := range gj.sortedDatabaseNames() {
		if database != "" && dbName != database {
			continue
		}
		ctx := gj.databases[dbName]
		if ctx.schema == nil {
			continue
		}
		tables := ctx.schema.GetTables()
		start := len(result)
		for _, t := range tables {
			if t.Type == "virtual" || t.Blocked {
				continue
			}
			result = append(result, TableInfo{
				Name:        t.Name,
				Schema:      t.Schema,
				Database:    dbName,
				Type:        t.Type,
				Comment:     t.Comment,
				ColumnCount: len(t.Columns),
			})
		}
		// Sort this database's slice by (schema, name) so output is deterministic
		// regardless of the order returned by the underlying schema walker.
		// Outer iteration is already in sorted database order.
		sub := result[start:]
		sort.SliceStable(sub, func(i, j int) bool {
			if sub[i].Schema != sub[j].Schema {
				return sub[i].Schema < sub[j].Schema
			}
			return sub[i].Name < sub[j].Name
		})
	}
	return result
}

// GetFunctions returns database functions across all databases.
func (g *GraphJin) GetFunctions() []FunctionInfo {
	gj, err := g.getEngine()
	if err != nil {
		return nil
	}
	return gj.getFunctions("")
}

// GetFunctionsForDatabase returns database functions for a specific database.
func (g *GraphJin) GetFunctionsForDatabase(database string) []FunctionInfo {
	gj, err := g.getEngine()
	if err != nil {
		return nil
	}
	return gj.getFunctions(database)
}

func (gj *graphjinEngine) getFunctions(database string) []FunctionInfo {
	var result []FunctionInfo
	for _, dbName := range gj.sortedDatabaseNames() {
		if database != "" && dbName != database {
			continue
		}
		ctx := gj.databases[dbName]
		if ctx.schema == nil {
			continue
		}
		for _, fn := range ctx.schema.GetFunctions() {
			fi := FunctionInfo{
				Name:      fn.Name,
				Schema:    fn.Schema,
				Type:      fn.Type,
				Comment:   fn.Comment,
				Aggregate: fn.Agg,
			}
			for _, p := range fn.Inputs {
				fi.Inputs = append(fi.Inputs, FunctionParam{
					Name: p.Name, Type: p.Type, Array: p.Array,
				})
			}
			for _, p := range fn.Outputs {
				fi.Outputs = append(fi.Outputs, FunctionParam{
					Name: p.Name, Type: p.Type, Array: p.Array,
				})
			}
			result = append(result, fi)
		}
	}
	return result
}

// GetTableSchema returns detailed schema for a specific table including relationships.
// In multi-DB mode, searches across all databases.
func (g *GraphJin) GetTableSchema(tableName string) (*TableSchema, error) {
	gj, err := g.getEngine()
	if err != nil {
		return nil, err
	}
	return gj.getTableSchema("", tableName)
}

// GetTableSchemaForDatabase returns detailed schema for a table in a specific database.
// If database is empty, searches across all databases.
func (g *GraphJin) GetTableSchemaForDatabase(database, tableName string) (*TableSchema, error) {
	gj, err := g.getEngine()
	if err != nil {
		return nil, err
	}
	return gj.getTableSchema(database, tableName)
}

// GetTableSchemaForDatabaseSchema returns detailed schema for a table in a
// specific configured database and database schema.
func (g *GraphJin) GetTableSchemaForDatabaseSchema(database, schemaName, tableName string) (*TableSchema, error) {
	gj, err := g.getEngine()
	if err != nil {
		return nil, err
	}
	return gj.getTableSchemaWithSchema(database, schemaName, tableName)
}

// getTableSchema finds and returns the schema for a table, optionally in a specific database.
func (gj *graphjinEngine) getTableSchema(database, tableName string) (*TableSchema, error) {
	if database != "" {
		ctx, ok := gj.GetDatabase(database)
		if !ok {
			return nil, fmt.Errorf("database not found: %s", database)
		}
		return gj.buildTableSchema(ctx.schema, database, tableName)
	}

	dbName, ctx, err := gj.resolveUniqueTableDatabase(tableName)
	if err != nil {
		return nil, err
	}
	return gj.buildTableSchema(ctx.schema, dbName, tableName)
}

func (gj *graphjinEngine) getTableSchemaWithSchema(database, schemaName, tableName string) (*TableSchema, error) {
	if schemaName == "" {
		return gj.getTableSchema(database, tableName)
	}

	if database != "" {
		ctx, ok := gj.GetDatabase(database)
		if !ok {
			return nil, fmt.Errorf("database not found: %s", database)
		}
		return gj.buildTableSchemaWithSchema(ctx.schema, database, schemaName, tableName)
	}

	var matches []string
	var matchedCtx *dbContext
	var matchedDB string
	for _, dbName := range gj.sortedDatabaseNames() {
		ctx := gj.databases[dbName]
		if ctx.schema == nil {
			continue
		}
		if _, err := ctx.schema.Find(schemaName, tableName); err == nil {
			matches = append(matches, dbName)
			matchedCtx = ctx
			matchedDB = dbName
		}
	}

	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("table not found: %s.%s (searched all databases)", schemaName, tableName)
	case 1:
		return gj.buildTableSchemaWithSchema(matchedCtx.schema, matchedDB, schemaName, tableName)
	default:
		return nil, fmt.Errorf("table %q in schema %q exists in multiple databases: %s; pass database to disambiguate",
			tableName, schemaName, strings.Join(matches, ", "))
	}
}

// buildTableSchema builds a TableSchema from a specific database schema.
func (gj *graphjinEngine) buildTableSchema(dbSchema *sdata.DBSchema, dbName, tableName string) (*TableSchema, error) {
	return gj.buildTableSchemaWithSchema(dbSchema, dbName, "", tableName)
}

func (gj *graphjinEngine) buildTableSchemaWithSchema(dbSchema *sdata.DBSchema, dbName, schemaName, tableName string) (*TableSchema, error) {
	t, err := dbSchema.Find(schemaName, tableName)
	if err != nil {
		return nil, fmt.Errorf("table not found: %s", tableName)
	}

	schema := &TableSchema{
		Name:     t.Name,
		Schema:   t.Schema,
		Database: dbName,
		Type:     t.Type,
		Comment:  t.Comment,
		Blocked:  t.Blocked,
	}

	if t.PrimaryCol.Name != "" {
		schema.PrimaryKey = t.PrimaryCol.Name
	}
	if len(t.PrimaryCols) > 1 {
		schema.PrimaryKeys = t.PKColNames()
	}

	schema.PartitionKey = t.PartitionKey
	schema.ImplicitPartitionKey = t.ImplicitPartitionKey

	// FullText columns
	for _, ft := range t.FullText {
		schema.FullTextColumns = append(schema.FullTextColumns, ft.Name)
	}

	// Add columns
	for _, col := range t.Columns {
		ci := ColumnInfo{
			Name:                col.Name,
			Type:                col.Type,
			Nullable:            !col.NotNull,
			PrimaryKey:          col.PrimaryKey,
			Array:               col.Array,
			UniqueKey:           col.UniqueKey,
			FullText:            col.FullText,
			ForeignKeyDatabase:  col.FKeyDatabase,
			ForeignKeyRecursive: col.FKRecursive,
		}
		if col.FKeyTable != "" {
			ci.ForeignKey = fmt.Sprintf("%s.%s", col.FKeyTable, col.FKeyCol)
		}
		schema.Columns = append(schema.Columns, ci)
	}

	// Get relationships
	firstDegree, err := dbSchema.GetFirstDegree(t)
	if err != nil {
		return schema, nil // Return schema without relationships
	}

	for _, rel := range firstDegree {
		ri := RelationInfo{
			Name:  rel.Name,
			Table: rel.Table.Name,
			Type:  relTypeToString(rel.Type),
		}

		// Determine if outgoing or incoming
		if rel.Type == sdata.RelOneToMany {
			schema.Relationships.Incoming = append(schema.Relationships.Incoming, ri)
		} else {
			schema.Relationships.Outgoing = append(schema.Relationships.Outgoing, ri)
		}
	}

	return schema, nil
}

// FindRelationshipPath finds the path between two tables.
// In multi-DB mode, searches across all databases.
func (g *GraphJin) FindRelationshipPath(fromTable, toTable string) ([]PathStep, error) {
	gj, err := g.getEngine()
	if err != nil {
		return nil, err
	}
	return gj.findRelationshipPath("", fromTable, toTable)
}

// FindRelationshipPathForDatabase finds the path between two tables in a specific database.
// If database is empty, searches across all databases.
func (g *GraphJin) FindRelationshipPathForDatabase(database, fromTable, toTable string) ([]PathStep, error) {
	gj, err := g.getEngine()
	if err != nil {
		return nil, err
	}
	return gj.findRelationshipPath(database, fromTable, toTable)
}

// findRelationshipPath finds the path between two tables, optionally in a specific database.
func (gj *graphjinEngine) findRelationshipPath(database, fromTable, toTable string) ([]PathStep, error) {
	if database != "" {
		ctx, ok := gj.GetDatabase(database)
		if !ok {
			return nil, fmt.Errorf("database not found: %s", database)
		}
		return gj.buildPath(ctx.schema, fromTable, toTable)
	}

	_, ctx, err := gj.resolveUniquePathDatabase(fromTable, toTable)
	if err != nil {
		return nil, err
	}
	return gj.buildPath(ctx.schema, fromTable, toTable)
}

// buildPath builds the relationship path between two tables using a specific schema.
func (gj *graphjinEngine) buildPath(dbSchema *sdata.DBSchema, fromTable, toTable string) ([]PathStep, error) {
	paths, err := dbSchema.FindPath(fromTable, toTable, "")
	if err != nil {
		return nil, err
	}

	if len(paths) == 0 {
		return nil, fmt.Errorf("no path found between %s and %s", fromTable, toTable)
	}

	result := make([]PathStep, 0, len(paths))
	for _, p := range paths {
		step := PathStep{
			From:     p.LT.Name,
			To:       p.RT.Name,
			Relation: relTypeToString(p.Rel),
		}
		if p.LC.Name != "" {
			step.Via = p.LC.Name
		}
		result = append(result, step)
	}

	return result, nil
}

// relTypeToString converts RelType to a human-readable string
func relTypeToString(rt sdata.RelType) string {
	switch rt {
	case sdata.RelOneToOne:
		return "one_to_one"
	case sdata.RelOneToMany:
		return "one_to_many"
	case sdata.RelPolymorphic:
		return "polymorphic"
	case sdata.RelRecursive:
		return "recursive"
	case sdata.RelEmbedded:
		return "embedded"
	case sdata.RelRemote:
		return "remote"
	default:
		return "unknown"
	}
}

// --- explain_query structs ---

// ParamInfo represents a query parameter
type ParamInfo struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	IsArray bool   `json:"is_array,omitempty"`
}

// SelectInfo represents a table selection in a compiled query
type SelectInfo struct {
	Table    string `json:"table"`
	Schema   string `json:"schema,omitempty"`
	Database string `json:"database,omitempty"`
	Singular bool   `json:"singular,omitempty"`
	Children int    `json:"children,omitempty"`
}

// QueryExplanation represents the compiled form of a GraphQL query
type QueryExplanation struct {
	CompiledQuery string             `json:"compiled_query"`
	Params        []ParamInfo        `json:"params"`
	Operation     string             `json:"operation"`
	Name          string             `json:"name,omitempty"`
	Role          string             `json:"role"`
	Database      string             `json:"database,omitempty"`
	Tables        []SelectInfo       `json:"tables"`
	JoinDepth     int                `json:"join_depth"`
	CacheHeader   string             `json:"cache_header,omitempty"`
	Errors        []string           `json:"errors,omitempty"`
	MultiDatabase bool               `json:"multi_database,omitempty"`
	Queries       []QueryExplanation `json:"queries,omitempty"`
}

// --- explore_relationships structs ---

// GraphNode represents a table node in the relationship graph
type GraphNode struct {
	Name        string `json:"name"`
	Schema      string `json:"schema,omitempty"`
	Database    string `json:"database,omitempty"`
	Type        string `json:"type"`
	ColumnCount int    `json:"column_count"`
}

// GraphEdge represents a relationship edge in the graph
type GraphEdge struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Type      string `json:"type"`
	Weight    int    `json:"weight"`
	ViaColumn string `json:"via_column,omitempty"`
}

// RelationshipGraph represents the data model neighborhood around a table
type RelationshipGraph struct {
	CenterTable string      `json:"center_table"`
	Depth       int         `json:"depth"`
	Nodes       []GraphNode `json:"nodes"`
	Edges       []GraphEdge `json:"edges"`
}

// --- audit_role_permissions structs ---

// OperationPermission represents the permission details for a single operation
type OperationPermission struct {
	Allowed          bool              `json:"allowed"`
	Blocked          bool              `json:"blocked,omitempty"`
	Limit            int               `json:"limit,omitempty"`
	Filters          []string          `json:"filters,omitempty"`
	Columns          []string          `json:"columns,omitempty"`
	Presets          map[string]string `json:"presets,omitempty"`
	DisableFunctions bool              `json:"disable_functions,omitempty"`
}

// TablePermissions represents per-table permission details for a role
type TablePermissions struct {
	TableName string               `json:"table_name"`
	Schema    string               `json:"schema,omitempty"`
	ReadOnly  bool                 `json:"read_only,omitempty"`
	Query     *OperationPermission `json:"query"`
	Insert    *OperationPermission `json:"insert"`
	Update    *OperationPermission `json:"update"`
	Upsert    *OperationPermission `json:"upsert"`
	Delete    *OperationPermission `json:"delete"`
}

// RoleAudit represents the complete permission audit for a role
type RoleAudit struct {
	Name     string             `json:"name"`
	Match    string             `json:"match,omitempty"`
	Tables   []TablePermissions `json:"tables"`
	FixGuide string             `json:"fix_guide"`
}

// ExplainQuery compiles a GraphQL query without executing it.
// Returns the compiled query, parameters, tables touched, join depth, and cache info.
func (g *GraphJin) ExplainQuery(query string, vars json.RawMessage, role string) (*QueryExplanation, error) {
	gj, err := g.getEngine()
	if err != nil {
		return nil, err
	}
	return gj.explainQuery(query, vars, role)
}

// ExplainQueryForDatabase compiles a GraphQL query against a specific configured database without executing it.
func (g *GraphJin) ExplainQueryForDatabase(database, query string, vars json.RawMessage, role string) (*QueryExplanation, error) {
	gj, err := g.getEngine()
	if err != nil {
		return nil, err
	}
	return gj.explainQueryForDatabase(database, query, vars, role)
}

// ExploreRelationships returns a graph of all reachable tables from the given table up to the specified depth.
func (g *GraphJin) ExploreRelationships(table string, depth int) (*RelationshipGraph, error) {
	gj, err := g.getEngine()
	if err != nil {
		return nil, err
	}
	return gj.exploreRelationships("", table, depth)
}

// ExploreRelationshipsForDatabase returns a relationship graph for a table in a specific database.
func (g *GraphJin) ExploreRelationshipsForDatabase(database, table string, depth int) (*RelationshipGraph, error) {
	gj, err := g.getEngine()
	if err != nil {
		return nil, err
	}
	return gj.exploreRelationships(database, table, depth)
}

// AuditRolePermissions returns a complete permission matrix for a single role.
func (g *GraphJin) AuditRolePermissions(role string) (*RoleAudit, error) {
	gj, err := g.getEngine()
	if err != nil {
		return nil, err
	}
	return gj.auditRolePermissions(role)
}

// AuditAllRoles returns permission matrices for all configured roles.
func (g *GraphJin) AuditAllRoles() ([]RoleAudit, error) {
	gj, err := g.getEngine()
	if err != nil {
		return nil, err
	}
	var audits []RoleAudit
	for _, r := range gj.conf.Roles {
		audit, err := gj.auditRolePermissions(r.Name)
		if err != nil {
			return nil, err
		}
		audits = append(audits, *audit)
	}
	return audits, nil
}

// explainQuery compiles a query through the full pipeline without executing it.
func (gj *graphjinEngine) explainQuery(query string, vars json.RawMessage, role string) (*QueryExplanation, error) {
	if !gj.anyDatabaseReady() {
		return nil, fmt.Errorf("schema not initialized")
	}

	queryBytes := []byte(query)

	h, err := graph.FastParseBytes(queryBytes)
	if err != nil {
		return &QueryExplanation{
			Errors: []string{fmt.Sprintf("parse error: %s", err.Error())},
		}, nil
	}

	r := gj.newGraphqlReq(nil, h.Operation, h.Name, queryBytes, vars)

	s, err := newGState(context.Background(), gj, r)
	if err != nil {
		return &QueryExplanation{
			Errors: []string{fmt.Sprintf("state error: %s", err.Error())},
		}, nil
	}

	if role != "" {
		s.role = role
	}

	err = s.compileQueryForRole()
	if err != nil {
		return &QueryExplanation{
			Operation: h.Operation,
			Name:      h.Name,
			Role:      s.role,
			Errors:    []string{err.Error()},
		}, nil
	}

	// Handle multi-DB queries by compiling each per-database sub-query
	if s.multiDB && len(s.dbGroups) > 0 {
		exp := &QueryExplanation{
			Operation:     h.Operation,
			Name:          h.Name,
			Role:          s.role,
			MultiDatabase: true,
		}
		for dbName, rootFields := range s.dbGroups {
			subExp := gj.explainForDatabase(&s, dbName, rootFields)
			exp.Queries = append(exp.Queries, *subExp)
		}
		return exp, nil
	}

	exp := &QueryExplanation{
		CompiledQuery: s.cs.st.sql,
		Operation:     s.cs.st.qc.Type.String(),
		Name:          s.cs.st.qc.Name,
		Role:          s.cs.st.role,
		Database:      s.database,
	}

	// Extract params
	params := s.cs.st.md.Params()
	for _, p := range params {
		exp.Params = append(exp.Params, ParamInfo{
			Name:    p.Name,
			Type:    p.Type,
			IsArray: p.IsArray,
		})
	}

	// Extract tables and compute join depth
	maxDepth := 0
	for i := range s.cs.st.qc.Selects {
		sel := &s.cs.st.qc.Selects[i]
		if sel.SkipRender != 0 {
			continue
		}
		exp.Tables = append(exp.Tables, SelectInfo{
			Table:    sel.Table,
			Schema:   sel.Schema,
			Database: sel.Database,
			Singular: sel.Singular,
			Children: len(sel.Children),
		})

		// Compute depth by walking ParentID chain
		depth := 0
		pid := sel.ParentID
		for pid != -1 {
			depth++
			if int(pid) < len(s.cs.st.qc.Selects) {
				pid = s.cs.st.qc.Selects[pid].ParentID
			} else {
				break
			}
		}
		if depth > maxDepth {
			maxDepth = depth
		}
	}
	exp.JoinDepth = maxDepth

	// Cache header
	if s.cs.st.qc.Cache.Header != "" {
		exp.CacheHeader = s.cs.st.qc.Cache.Header
	}

	return exp, nil
}

func (gj *graphjinEngine) explainQueryForDatabase(database, query string, vars json.RawMessage, role string) (*QueryExplanation, error) {
	if !gj.anyDatabaseReady() {
		return nil, fmt.Errorf("schema not initialized")
	}

	dbCtx, ok := gj.GetDatabase(database)
	if !ok {
		return nil, fmt.Errorf("database not found: %s", database)
	}

	queryBytes := []byte(query)
	h, err := graph.FastParseBytes(queryBytes)
	if err != nil {
		return &QueryExplanation{
			Database: database,
			Errors:   []string{fmt.Sprintf("parse error: %s", err.Error())},
		}, nil
	}

	r := gj.newGraphqlReq(nil, h.Operation, h.Name, queryBytes, vars)
	s, err := newGState(context.Background(), gj, r)
	if err != nil {
		return &QueryExplanation{
			Database: database,
			Errors:   []string{fmt.Sprintf("state error: %s", err.Error())},
		}, nil
	}
	if role != "" {
		s.role = role
	}

	st := stmt{role: s.role}
	var found bool
	if st.roc, found = gj.roles[s.role]; !found {
		return &QueryExplanation{
			Operation: h.Operation,
			Name:      h.Name,
			Role:      s.role,
			Database:  database,
			Errors:    []string{fmt.Sprintf(`roles '%s' not defined in c.gj.config`, s.role)},
		}, nil
	}

	var compileVars map[string]json.RawMessage
	if len(s.r.aschema) != 0 {
		compileVars = s.r.aschema
	} else {
		compileVars = s.vmap
	}

	if err := s.compileForDatabase(st, compileVars, dbCtx); err != nil {
		return &QueryExplanation{
			Operation: h.Operation,
			Name:      h.Name,
			Role:      s.role,
			Database:  database,
			Errors:    []string{err.Error()},
		}, nil
	}

	return queryExplanationFromState(&s), nil
}

func queryExplanationFromState(s *gstate) *QueryExplanation {
	exp := &QueryExplanation{
		CompiledQuery: s.cs.st.sql,
		Operation:     s.cs.st.qc.Type.String(),
		Name:          s.cs.st.qc.Name,
		Role:          s.cs.st.role,
		Database:      s.database,
	}

	params := s.cs.st.md.Params()
	for _, p := range params {
		exp.Params = append(exp.Params, ParamInfo{
			Name:    p.Name,
			Type:    p.Type,
			IsArray: p.IsArray,
		})
	}

	maxDepth := 0
	for i := range s.cs.st.qc.Selects {
		sel := &s.cs.st.qc.Selects[i]
		if sel.SkipRender != 0 {
			continue
		}
		exp.Tables = append(exp.Tables, SelectInfo{
			Table:    sel.Table,
			Schema:   sel.Schema,
			Database: sel.Database,
			Singular: sel.Singular,
			Children: len(sel.Children),
		})

		depth := 0
		pid := sel.ParentID
		for pid != -1 {
			depth++
			if int(pid) < len(s.cs.st.qc.Selects) {
				pid = s.cs.st.qc.Selects[pid].ParentID
			} else {
				break
			}
		}
		if depth > maxDepth {
			maxDepth = depth
		}
	}
	exp.JoinDepth = maxDepth

	if s.cs.st.qc.Cache.Header != "" {
		exp.CacheHeader = s.cs.st.qc.Cache.Header
	}
	return exp
}

// explainForDatabase compiles a sub-query for a single database and returns its explanation.
func (gj *graphjinEngine) explainForDatabase(s *gstate, dbName string, rootFields []string) *QueryExplanation {
	// Get compilers for the target database
	dbCtx, ok := gj.GetDatabase(dbName)
	if !ok {
		return &QueryExplanation{
			Database: dbName,
			Errors:   []string{fmt.Sprintf("database not found: %s", dbName)},
		}
	}
	qcodeCompiler := dbCtx.qcodeCompiler
	psqlCompiler := dbCtx.psqlCompiler

	// Build a sub-query with only this database's root fields
	subQuery, err := s.buildDatabaseQuery(rootFields)
	if err != nil {
		return &QueryExplanation{
			Database: dbName,
			Errors:   []string{fmt.Sprintf("failed to build sub-query: %s", err.Error())},
		}
	}

	// Get vars
	var vars map[string]json.RawMessage
	if len(s.r.aschema) != 0 {
		vars = s.r.aschema
	} else {
		vars = s.vmap
	}

	// Compile QCode
	qc, err := qcodeCompiler.Compile(subQuery, vars, s.role, s.r.namespace)
	if err != nil {
		return &QueryExplanation{
			Database: dbName,
			Errors:   []string{fmt.Sprintf("qcode compile failed: %s", err.Error())},
		}
	}

	// Compile query (SQL or MongoDB pipeline depending on dialect)
	var sqlBuf bytes.Buffer
	md, err := psqlCompiler.Compile(&sqlBuf, qc)
	if err != nil {
		return &QueryExplanation{
			Database: dbName,
			Errors:   []string{fmt.Sprintf("query compile failed: %s", err.Error())},
		}
	}

	exp := &QueryExplanation{
		CompiledQuery: sqlBuf.String(),
		Operation:     qc.Type.String(),
		Name:          qc.Name,
		Role:          s.role,
		Database:      dbName,
	}

	// Extract params
	params := md.Params()
	for _, p := range params {
		exp.Params = append(exp.Params, ParamInfo{
			Name:    p.Name,
			Type:    p.Type,
			IsArray: p.IsArray,
		})
	}

	// Extract tables and compute join depth
	maxDepth := 0
	for i := range qc.Selects {
		sel := &qc.Selects[i]
		if sel.SkipRender != 0 {
			continue
		}
		exp.Tables = append(exp.Tables, SelectInfo{
			Table:    sel.Table,
			Schema:   sel.Schema,
			Database: sel.Database,
			Singular: sel.Singular,
			Children: len(sel.Children),
		})

		depth := 0
		pid := sel.ParentID
		for pid != -1 {
			depth++
			if int(pid) < len(qc.Selects) {
				pid = qc.Selects[pid].ParentID
			} else {
				break
			}
		}
		if depth > maxDepth {
			maxDepth = depth
		}
	}
	exp.JoinDepth = maxDepth

	// Cache header
	if qc.Cache.Header != "" {
		exp.CacheHeader = qc.Cache.Header
	}

	return exp
}

// exploreRelationships performs BFS over the relationship graph.
func (gj *graphjinEngine) exploreRelationships(database, tableName string, depth int) (*RelationshipGraph, error) {
	if depth < 1 {
		depth = 1
	}
	if depth > 3 {
		depth = 3
	}

	var dbSchema *sdata.DBSchema
	if database != "" {
		ctx, ok := gj.GetDatabase(database)
		if !ok {
			return nil, fmt.Errorf("database not found: %s", database)
		}
		dbSchema = ctx.schema
	} else {
		dbName, ctx, err := gj.resolveUniqueTableDatabase(tableName)
		if err != nil {
			return nil, err
		}
		dbSchema = ctx.schema
		database = dbName
	}

	if dbSchema == nil {
		return nil, fmt.Errorf("schema not initialized")
	}

	centerTable, err := dbSchema.Find("", tableName)
	if err != nil {
		return nil, fmt.Errorf("table not found: %s", tableName)
	}

	result := &RelationshipGraph{
		CenterTable: centerTable.Name,
		Depth:       depth,
	}

	nodeSet := make(map[string]bool)
	edgeSet := make(map[string]bool)

	// Add center node
	nodeKey := centerTable.Schema + ":" + centerTable.Name
	nodeSet[nodeKey] = true
	result.Nodes = append(result.Nodes, GraphNode{
		Name:        centerTable.Name,
		Schema:      centerTable.Schema,
		Database:    database,
		Type:        centerTable.Type,
		ColumnCount: len(centerTable.Columns),
	})

	// BFS expansion
	type bfsItem struct {
		table sdata.DBTable
		level int
	}
	queue := []bfsItem{{table: centerTable, level: 0}}

	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]

		if item.level >= depth {
			continue
		}

		rels, err := dbSchema.GetFirstDegree(item.table)
		if err != nil {
			continue
		}

		for _, rel := range rels {
			relNodeKey := rel.Table.Schema + ":" + rel.Table.Name

			// Add node if not seen
			if !nodeSet[relNodeKey] {
				nodeSet[relNodeKey] = true
				result.Nodes = append(result.Nodes, GraphNode{
					Name:        rel.Table.Name,
					Schema:      rel.Table.Schema,
					Database:    database,
					Type:        rel.Table.Type,
					ColumnCount: len(rel.Table.Columns),
				})
				queue = append(queue, bfsItem{table: rel.Table, level: item.level + 1})
			}

			// Add edge if not seen
			edgeKey := item.table.Name + "->" + rel.Table.Name + ":" + rel.Name
			if !edgeSet[edgeKey] {
				edgeSet[edgeKey] = true
				result.Edges = append(result.Edges, GraphEdge{
					From:      item.table.Name,
					To:        rel.Table.Name,
					Type:      relTypeToString(rel.Type),
					Weight:    relTypeWeight(rel.Type),
					ViaColumn: rel.Name,
				})
			}
		}
	}

	return result, nil
}

func (gj *graphjinEngine) resolveUniqueTableDatabase(tableName string) (string, *dbContext, error) {
	matches := gj.findDatabasesWithTable(tableName)
	switch len(matches) {
	case 0:
		return "", nil, fmt.Errorf("table not found: %s (searched all databases)", tableName)
	case 1:
		dbName := matches[0]
		ctx := gj.databases[dbName]
		return dbName, ctx, nil
	default:
		return "", nil, fmt.Errorf("table %q exists in multiple databases: %s; pass database to disambiguate",
			tableName, strings.Join(matches, ", "))
	}
}

func (gj *graphjinEngine) resolveUniquePathDatabase(fromTable, toTable string) (string, *dbContext, error) {
	fromMatches := gj.findDatabasesWithTable(fromTable)
	toMatches := gj.findDatabasesWithTable(toTable)

	if len(fromMatches) == 0 || len(toMatches) == 0 {
		return "", nil, fmt.Errorf("no path found between %s and %s (searched all databases)", fromTable, toTable)
	}

	toSet := make(map[string]struct{}, len(toMatches))
	for _, dbName := range toMatches {
		toSet[dbName] = struct{}{}
	}

	var common []string
	for _, dbName := range fromMatches {
		if _, ok := toSet[dbName]; ok {
			common = append(common, dbName)
		}
	}

	switch len(common) {
	case 0:
		return "", nil, fmt.Errorf("no path found between %s and %s (searched all databases)", fromTable, toTable)
	case 1:
		dbName := common[0]
		ctx := gj.databases[dbName]
		return dbName, ctx, nil
	default:
		return "", nil, fmt.Errorf("tables %q and %q exist in multiple databases: %s; pass database to disambiguate",
			fromTable, toTable, strings.Join(common, ", "))
	}
}

func (gj *graphjinEngine) findDatabasesWithTable(tableName string) []string {
	var matches []string
	for _, dbName := range gj.sortedDatabaseNames() {
		ctx := gj.databases[dbName]
		if ctx == nil || ctx.schema == nil {
			continue
		}
		if _, err := ctx.schema.Find("", tableName); err == nil {
			matches = append(matches, dbName)
		}
	}
	return matches
}

// relTypeWeight returns a weight for a relationship type
func relTypeWeight(rt sdata.RelType) int {
	switch rt {
	case sdata.RelOneToOne, sdata.RelOneToMany:
		return 1
	case sdata.RelEmbedded:
		return 5
	case sdata.RelRemote:
		return 8
	case sdata.RelRecursive:
		return 10
	case sdata.RelPolymorphic:
		return 15
	default:
		return 1
	}
}

// auditRolePermissions returns a permission audit for a single role.
func (gj *graphjinEngine) auditRolePermissions(roleName string) (*RoleAudit, error) {
	var role *Role
	for i := range gj.conf.Roles {
		if strings.EqualFold(gj.conf.Roles[i].Name, roleName) {
			role = &gj.conf.Roles[i]
			break
		}
	}
	if role == nil {
		return nil, fmt.Errorf("role not found: %s", roleName)
	}

	audit := &RoleAudit{
		Name:  role.Name,
		Match: role.Match,
	}

	for _, rt := range role.Tables {
		tp := TablePermissions{
			TableName: rt.Name,
			Schema:    rt.Schema,
			ReadOnly:  rt.ReadOnly,
			Query:     buildQueryPermission(rt.Query),
			Insert:    buildInsertPermission(rt.Insert, rt.ReadOnly),
			Update:    buildUpdatePermission(rt.Update, rt.ReadOnly),
			Upsert:    buildUpsertPermission(rt.Upsert, rt.ReadOnly),
			Delete:    buildDeletePermission(rt.Delete, rt.ReadOnly),
		}
		audit.Tables = append(audit.Tables, tp)
	}

	audit.FixGuide = fmt.Sprintf(
		"To modify permissions for role '%s', use the update_current_config tool with the roles parameter. "+
			"Example: update_current_config(roles: [{name: \"%s\", tables: [{name: \"<table>\", query: {block: true}}]}])",
		role.Name, role.Name)

	return audit, nil
}

func buildQueryPermission(q *Query) *OperationPermission {
	if q == nil {
		return &OperationPermission{Allowed: true}
	}
	return &OperationPermission{
		Allowed:          !q.Block,
		Blocked:          q.Block,
		Limit:            q.Limit,
		Filters:          q.Filters,
		Columns:          q.Columns,
		DisableFunctions: q.DisableFunctions,
	}
}

func buildInsertPermission(ins *Insert, readOnly bool) *OperationPermission {
	if readOnly {
		return &OperationPermission{Allowed: false, Blocked: true}
	}
	if ins == nil {
		return &OperationPermission{Allowed: true}
	}
	return &OperationPermission{
		Allowed: !ins.Block,
		Blocked: ins.Block,
		Filters: ins.Filters,
		Columns: ins.Columns,
		Presets: ins.Presets,
	}
}

func buildUpdatePermission(upd *Update, readOnly bool) *OperationPermission {
	if readOnly {
		return &OperationPermission{Allowed: false, Blocked: true}
	}
	if upd == nil {
		return &OperationPermission{Allowed: true}
	}
	return &OperationPermission{
		Allowed: !upd.Block,
		Blocked: upd.Block,
		Filters: upd.Filters,
		Columns: upd.Columns,
		Presets: upd.Presets,
	}
}

func buildUpsertPermission(ups *Upsert, readOnly bool) *OperationPermission {
	if readOnly {
		return &OperationPermission{Allowed: false, Blocked: true}
	}
	if ups == nil {
		return &OperationPermission{Allowed: true}
	}
	return &OperationPermission{
		Allowed: !ups.Block,
		Blocked: ups.Block,
		Filters: ups.Filters,
		Columns: ups.Columns,
		Presets: ups.Presets,
	}
}

func buildDeletePermission(del *Delete, readOnly bool) *OperationPermission {
	if readOnly {
		return &OperationPermission{Allowed: false, Blocked: true}
	}
	if del == nil {
		return &OperationPermission{Allowed: true}
	}
	return &OperationPermission{
		Allowed: !del.Block,
		Blocked: del.Block,
		Filters: del.Filters,
		Columns: del.Columns,
	}
}

// SavedQueryInfo represents a saved query from the allow list
type SavedQueryInfo struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Operation string `json:"operation"` // query or mutation
}

// SavedQueryDetails represents full details of a saved query
type SavedQueryDetails struct {
	Name      string                 `json:"name"`
	Namespace string                 `json:"namespace,omitempty"`
	Operation string                 `json:"operation"`
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables,omitempty"`
}

func savedQueryVariablesToRaw(vars map[string]interface{}) map[string]json.RawMessage {
	if len(vars) == 0 {
		return nil
	}
	out := make(map[string]json.RawMessage, len(vars))
	for k, v := range vars {
		b, err := json.Marshal(v)
		if err != nil {
			continue
		}
		out[k] = json.RawMessage(b)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ResolveGraphQLFile reads a GraphQL file from fs and expands #import
// directives using the same allow-list path validation as saved queries.
func ResolveGraphQLFile(fs FS, fname string) ([]byte, error) {
	return allow.ReadGQL(fs, fname)
}

// ListSavedQueries returns all saved queries from the allow list
func (g *GraphJin) ListSavedQueries() ([]SavedQueryInfo, error) {
	gj, err := g.getEngine()
	if err != nil {
		return nil, err
	}
	if gj.allowList == nil {
		return []SavedQueryInfo{}, nil
	}

	items, err := gj.allowList.ListAll()
	if err != nil {
		return nil, err
	}

	result := make([]SavedQueryInfo, 0, len(items))
	for _, item := range items {
		result = append(result, SavedQueryInfo{
			Name:      item.Name,
			Namespace: item.Namespace,
			Operation: item.Operation,
		})
	}
	// Sort by (namespace, name) so output is deterministic across runs
	// regardless of the order returned by the underlying allow-list walk.
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Namespace != result[j].Namespace {
			return result[i].Namespace < result[j].Namespace
		}
		return result[i].Name < result[j].Name
	})
	return result, nil
}

// GetSavedQuery returns details of a specific saved query
func (g *GraphJin) GetSavedQuery(name string) (*SavedQueryDetails, error) {
	gj, err := g.getEngine()
	if err != nil {
		return nil, err
	}

	item, err := gj.allowList.GetByName(name, false)
	if err != nil {
		return nil, err
	}

	details := &SavedQueryDetails{
		Name:      item.Name,
		Namespace: item.Namespace,
		Operation: item.Operation,
		Query:     string(item.Query),
	}

	// Parse action JSON if present
	if len(item.ActionJSON) > 0 {
		details.Variables = make(map[string]interface{})
		for k, v := range item.ActionJSON {
			var val interface{}
			if err := json.Unmarshal(v, &val); err == nil {
				details.Variables[k] = val
			}
		}
	}

	return details, nil
}

// FragmentInfo represents a fragment from the allow list
type FragmentInfo struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
}

// FragmentDetails represents full details of a fragment
type FragmentDetails struct {
	Name       string `json:"name"`
	Namespace  string `json:"namespace,omitempty"`
	Definition string `json:"definition"`
	On         string `json:"on,omitempty"` // The type the fragment is defined on
}

// ListFragments returns all fragments from the allow list
func (g *GraphJin) ListFragments() ([]FragmentInfo, error) {
	gj, err := g.getEngine()
	if err != nil {
		return nil, err
	}
	if gj.allowList == nil {
		return []FragmentInfo{}, nil
	}

	fragments, err := gj.allowList.ListFragments()
	if err != nil {
		return nil, err
	}

	result := make([]FragmentInfo, 0, len(fragments))
	for _, f := range fragments {
		ns, name := splitFragmentName(f.Name)
		result = append(result, FragmentInfo{
			Name:      name,
			Namespace: ns,
		})
	}
	// Sort by (namespace, name) so output is deterministic across runs
	// regardless of the order returned by the underlying allow-list walk.
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Namespace != result[j].Namespace {
			return result[i].Namespace < result[j].Namespace
		}
		return result[i].Name < result[j].Name
	})
	return result, nil
}

// GetFragment returns details of a specific fragment
func (g *GraphJin) GetFragment(name string) (*FragmentDetails, error) {
	gj, err := g.getEngine()
	if err != nil {
		return nil, err
	}

	fragment, err := gj.allowList.GetFragment(name)
	if err != nil {
		return nil, err
	}

	ns, fragName := splitFragmentName(fragment.Name)

	details := &FragmentDetails{
		Name:       fragName,
		Namespace:  ns,
		Definition: string(fragment.Value),
	}

	// Try to extract the "on" type from the fragment definition
	// Format: fragment FragmentName on TypeName { ... }
	def := string(fragment.Value)
	if idx := strings.Index(def, " on "); idx != -1 {
		rest := def[idx+4:]
		if endIdx := strings.IndexAny(rest, " {"); endIdx != -1 {
			details.On = strings.TrimSpace(rest[:endIdx])
		}
	}

	return details, nil
}

// splitFragmentName splits a fragment name into namespace and name
func splitFragmentName(name string) (string, string) {
	i := strings.LastIndex(name, ".")
	if i == -1 {
		return "", name
	} else if i < len(name)-1 {
		return name[:i], name[(i + 1):]
	}
	return "", ""
}

// DatabaseStats represents statistics and info for a database connection
type DatabaseStats struct {
	Name       string     `json:"name"`
	Type       string     `json:"type"`
	IsDefault  bool       `json:"isDefault"`
	ReadOnly   bool       `json:"readOnly"`
	TableCount int        `json:"tableCount"`
	Pool       *PoolStats `json:"pool,omitempty"`
}

// PoolStats represents connection pool statistics
type PoolStats struct {
	MaxOpen           int    `json:"maxOpen"`
	Open              int    `json:"open"`
	InUse             int    `json:"inUse"`
	Idle              int    `json:"idle"`
	WaitCount         int64  `json:"waitCount"`
	WaitDuration      string `json:"waitDuration"`
	MaxIdleClosed     int64  `json:"maxIdleClosed"`
	MaxLifetimeClosed int64  `json:"maxLifetimeClosed"`
}

// GetAllDatabaseStats returns statistics for all configured databases.
func (g *GraphJin) GetAllDatabaseStats() []DatabaseStats {
	gj, err := g.getEngine()
	if err != nil {
		return nil
	}

	stats := make([]DatabaseStats, 0, len(gj.databases))
	for _, name := range gj.sortedDatabaseNames() {
		ctx := gj.databases[name]
		// Check if database is read-only from config
		readOnly := false
		if dbConf, ok := gj.conf.Databases[name]; ok {
			readOnly = dbConf.ReadOnly
		}
		ds := DatabaseStats{
			Name:      name,
			Type:      ctx.dbtype,
			IsDefault: name == gj.defaultDB,
			ReadOnly:  readOnly,
		}

		// Get table count from schema
		if ctx.schema != nil {
			tables := ctx.schema.GetTables()
			count := 0
			for _, t := range tables {
				if t.Type != "virtual" && !t.Blocked {
					count++
				}
			}
			ds.TableCount = count
		}

		// Get pool stats if DB connection exists
		if ctx.db != nil {
			dbStats := ctx.db.Stats()
			ds.Pool = &PoolStats{
				MaxOpen:           dbStats.MaxOpenConnections,
				Open:              dbStats.OpenConnections,
				InUse:             dbStats.InUse,
				Idle:              dbStats.Idle,
				WaitCount:         dbStats.WaitCount,
				WaitDuration:      dbStats.WaitDuration.String(),
				MaxIdleClosed:     dbStats.MaxIdleClosed,
				MaxLifetimeClosed: dbStats.MaxLifetimeClosed,
			}
		}

		stats = append(stats, ds)
	}

	if len(stats) == 0 {
		return []DatabaseStats{{Name: "default", IsDefault: true}}
	}
	return stats
}
