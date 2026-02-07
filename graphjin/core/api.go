// Package core provides an API to include and use the GraphJin compiler with your own code.
// For detailed documentation visit https://graphjin.com
package core

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	_log "log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dosco/graphjin/core/v3/internal/allow"
	"github.com/dosco/graphjin/core/v3/internal/graph"
	"github.com/dosco/graphjin/core/v3/internal/psql"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
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
)

const (
	APQ_PX = "_apq"
)

// dbContext holds per-database state for multi-database support.
// Each database gets its own connection pool, schema discovery, and SQL compiler.
type dbContext struct {
	name          string           // Database name (key in Config.Databases)
	db            *sql.DB          // Connection pool for this database
	dbtype        string           // Database type (postgres, mysql, sqlite, etc.)
	dbinfo        *sdata.DBInfo    // Raw schema metadata
	schema        *sdata.DBSchema  // Processed schema with relationships
	qcodeCompiler *qcode.Compiler  // GraphQL to QCode compiler (validates against this DB's schema)
	psqlCompiler  *psql.Compiler   // QCode to SQL compiler (generates this DB's dialect)
}

// GraphJin struct is an instance of the GraphJin engine it holds all the required information like
// datase schemas, relationships, etc that the GraphQL to SQL compiler would need to do it's job.
type graphjinEngine struct {
	conf                  *Config
	db                    *sql.DB
	log                   *_log.Logger
	fs                    FS
	trace                 Tracer
	dbtype                string
	dbinfo                *sdata.DBInfo
	schema                *sdata.DBSchema
	allowList             *allow.List
	encryptionKey         [32]byte
	encryptionKeySet      bool
	cache                 Cache
	queries               sync.Map
	roles                 map[string]*Role
	roleStatement         string
	roleStatementMetadata psql.Metadata
	tmap                  map[string]qcode.TConfig
	rtmap                 map[string]ResolverFn
	rmap                  map[string]resItem
	abacEnabled           bool
	qcodeCompiler         *qcode.Compiler
	psqlCompiler          *psql.Compiler
	subs                  sync.Map
	prod                  bool
	prodSec               bool
	namespace             string
	printFormat           []byte
	opts                  []Option
	done                  chan bool

	// Multi-database support: map of database name to context
	// When nil or empty, single-database mode is used (backward compatible)
	databases map[string]*dbContext
	// Name of the default database (used when table has no explicit database)
	defaultDB string

	// Response cache provider (optional, set via OptionSetResponseCache)
	responseCache ResponseCacheProvider
	// Cache key builder
	cacheKeyBuilder *CacheKeyBuilder
}

type GraphJin struct {
	atomic.Value
	done     chan bool
	reloadMu sync.Mutex // serializes reload operations
}

type Option func(*graphjinEngine) error

// NewGraphJin creates the GraphJin struct, this involves querying the database to learn its
// schemas and relationships
func NewGraphJin(conf *Config, db *sql.DB, options ...Option) (g *GraphJin, err error) {
	fs, err := getFS(conf)
	if err != nil {
		return
	}

	g = &GraphJin{done: make(chan bool)}
	if err = g.newGraphJin(conf, db, nil, fs, options...); err != nil {
		return
	}

	if err = g.initDBWatcher(); err != nil {
		return
	}
	return
}

// NewGraphJinWithFS creates the GraphJin struct, this involves querying the database to learn its
func NewGraphJinWithFS(conf *Config, db *sql.DB, fs FS, options ...Option) (g *GraphJin, err error) {
	g = &GraphJin{done: make(chan bool)}
	if err = g.newGraphJin(conf, db, nil, fs, options...); err != nil {
		return
	}

	if err = g.initDBWatcher(); err != nil {
		return
	}
	return
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

	t := time.Now()

	gj := &graphjinEngine{
		conf:        conf,
		db:          db,
		dbinfo:      dbinfo,
		log:         _log.New(os.Stdout, "", 0),
		prod:        conf.Production,
		prodSec:     conf.Production,
		printFormat: []byte(fmt.Sprintf("gj-%x:", t.Unix())),
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

	for _, op := range options {
		if err = op(gj); err != nil {
			return
		}
	}

	if err = gj.initDiscover(); err != nil {
		return
	}

	if err = gj.initResolvers(); err != nil {
		return
	}

	if err = gj.initSchema(); err != nil {
		return
	}

	// Only initialize compilers and dependent features if schema exists (tables found)
	if gj.schema != nil {
		if err = gj.initAllowList(); err != nil {
			return
		}

		if err = gj.initCompilers(); err != nil {
			return
		}

		// Initialize multi-database support if configured
		if err = gj.initMultiDB(); err != nil {
			return
		}

		// Set database names on tables for multi-DB routing
		gj.setTableDatabases()

		// Initialize compilers for additional databases
		if err = gj.initMultiDBCompilers(); err != nil {
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

// OptionSetResponseCache sets the response cache provider for caching query results.
// The cache provider is typically the Redis cache from the serv package.
func OptionSetResponseCache(cache ResponseCacheProvider) Option {
	return func(s *graphjinEngine) error {
		s.responseCache = cache
		s.cacheKeyBuilder = NewCacheKeyBuilder()
		return nil
	}
}

type Error struct {
	Message string `json:"message"`
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
	gj := g.Load().(*graphjinEngine)

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

	// if not production then save to allow list
	if !gj.prod && r.name != "IntrospectionQuery" {
		if err = gj.saveToAllowList(resp.qc, resp.res.namespace); err != nil {
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
	gj := g.Load().(*graphjinEngine)

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
		resp.res.Data, err = gj.getIntroResult()
		return
	}

	if r.operation == qcode.QTSubscription {
		err = errors.New("use 'core.Subscribe' for subscriptions")
		return
	}

	if gj.schema == nil {
		err = fmt.Errorf("no tables found in database '%s'; schema not initialized", gj.dbinfo.Name)
		return
	}

	s, err := newGState(c, gj, r)
	if err != nil {
		return
	}
	err = s.compileAndExecuteWrapper(c)

	resp.qc = s.qcode()
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
	resp.res.cacheHit = s.cacheHit

	if err != nil {
		resp.res.Errors = newError(err)
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
	gj := g.Load().(*graphjinEngine)
	return g.newGraphJin(gj.conf, gj.db, nil, gj.fs, gj.opts...)
}

// IsProd return true for production mode or false for development mode
func (g *GraphJin) IsProd() bool {
	gj := g.Load().(*graphjinEngine)
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
func newError(err error) (errList []Error) {
	errList = []Error{{Message: err.Error()}}
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

	var obj interface{}
	if err := json.Unmarshal(data, &obj); err != nil {
		return data // Return original on parse error
	}

	removeGjIdKey(obj)

	result, err := json.Marshal(obj)
	if err != nil {
		return data // Return original on marshal error
	}
	return result
}

// removeGjIdKey recursively removes "__gj_id" from all objects in the JSON structure.
func removeGjIdKey(v interface{}) {
	switch val := v.(type) {
	case map[string]interface{}:
		delete(val, "__gj_id")
		for _, child := range val {
			removeGjIdKey(child)
		}
	case []interface{}:
		for _, item := range val {
			removeGjIdKey(item)
		}
	}
}

// TableInfo represents basic table information for MCP/API consumers
type TableInfo struct {
	Name        string `json:"name"`
	Schema      string `json:"schema,omitempty"`
	Type        string `json:"type"` // table, view, etc.
	Comment     string `json:"comment,omitempty"`
	ColumnCount int    `json:"column_count"`
}

// ColumnInfo represents column information for MCP/API consumers
type ColumnInfo struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Nullable   bool   `json:"nullable"`
	PrimaryKey bool   `json:"primary_key"`
	ForeignKey string `json:"foreign_key,omitempty"` // "schema.table.column" if FK
	Array      bool   `json:"array,omitempty"`
}

// RelationInfo represents a relationship between tables
type RelationInfo struct {
	Name       string `json:"name"`         // Field name to use in queries
	Table      string `json:"table"`        // Related table name
	Type       string `json:"type"`         // one_to_one, one_to_many, many_to_many
	ForeignKey string `json:"foreign_key"`  // The FK column
	Through    string `json:"through,omitempty"` // Join table for many-to-many
}

// TableSchema represents full table schema with relationships
type TableSchema struct {
	Name          string         `json:"name"`
	Schema        string         `json:"schema,omitempty"`
	Type          string         `json:"type"`
	Comment       string         `json:"comment,omitempty"`
	PrimaryKey    string         `json:"primary_key,omitempty"`
	Columns       []ColumnInfo   `json:"columns"`
	Relationships struct {
		Outgoing []RelationInfo `json:"outgoing"` // Tables this table references
		Incoming []RelationInfo `json:"incoming"` // Tables that reference this table
	} `json:"relationships"`
}

// PathStep represents a step in a relationship path
type PathStep struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Via      string `json:"via,omitempty"` // Column or join table
	Relation string `json:"relation"`      // Relationship type
}

// GetTables returns a list of all tables in the database schema
func (g *GraphJin) GetTables() []TableInfo {
	gj := g.Load().(*graphjinEngine)
	tables := gj.schema.GetTables()

	result := make([]TableInfo, 0, len(tables))
	for _, t := range tables {
		// Skip virtual tables and blocked tables
		if t.Type == "virtual" || t.Blocked {
			continue
		}
		result = append(result, TableInfo{
			Name:        t.Name,
			Schema:      t.Schema,
			Type:        t.Type,
			Comment:     t.Comment,
			ColumnCount: len(t.Columns),
		})
	}
	return result
}

// GetTableSchema returns detailed schema for a specific table including relationships
func (g *GraphJin) GetTableSchema(tableName string) (*TableSchema, error) {
	gj := g.Load().(*graphjinEngine)

	// Find the table
	t, err := gj.schema.Find("", tableName)
	if err != nil {
		return nil, fmt.Errorf("table not found: %s", tableName)
	}

	schema := &TableSchema{
		Name:    t.Name,
		Schema:  t.Schema,
		Type:    t.Type,
		Comment: t.Comment,
	}

	if t.PrimaryCol.Name != "" {
		schema.PrimaryKey = t.PrimaryCol.Name
	}

	// Add columns
	for _, col := range t.Columns {
		ci := ColumnInfo{
			Name:       col.Name,
			Type:       col.Type,
			Nullable:   !col.NotNull,
			PrimaryKey: col.PrimaryKey,
			Array:      col.Array,
		}
		if col.FKeyTable != "" {
			ci.ForeignKey = fmt.Sprintf("%s.%s", col.FKeyTable, col.FKeyCol)
		}
		schema.Columns = append(schema.Columns, ci)
	}

	// Get relationships
	firstDegree, err := gj.schema.GetFirstDegree(t)
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
			// This table is referenced by another (incoming)
			schema.Relationships.Incoming = append(schema.Relationships.Incoming, ri)
		} else {
			// This table references another (outgoing)
			schema.Relationships.Outgoing = append(schema.Relationships.Outgoing, ri)
		}
	}

	return schema, nil
}

// FindRelationshipPath finds the path between two tables
func (g *GraphJin) FindRelationshipPath(fromTable, toTable string) ([]PathStep, error) {
	gj := g.Load().(*graphjinEngine)

	paths, err := gj.schema.FindPath(fromTable, toTable, "")
	if err != nil {
		return nil, err
	}

	if len(paths) == 0 {
		return nil, fmt.Errorf("no path found between %s and %s", fromTable, toTable)
	}

	// Each TPath is a step in the path
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

// ListSavedQueries returns all saved queries from the allow list
func (g *GraphJin) ListSavedQueries() ([]SavedQueryInfo, error) {
	gj := g.Load().(*graphjinEngine)

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
	return result, nil
}

// GetSavedQuery returns details of a specific saved query
func (g *GraphJin) GetSavedQuery(name string) (*SavedQueryDetails, error) {
	gj := g.Load().(*graphjinEngine)

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
	gj := g.Load().(*graphjinEngine)

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
	return result, nil
}

// GetFragment returns details of a specific fragment
func (g *GraphJin) GetFragment(name string) (*FragmentDetails, error) {
	gj := g.Load().(*graphjinEngine)

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
// In single-database mode, returns a single entry for the main connection.
// In multi-database mode, returns stats for each configured database.
func (g *GraphJin) GetAllDatabaseStats() []DatabaseStats {
	gj := g.Load().(*graphjinEngine)

	// Multi-database mode
	if len(gj.databases) > 0 {
		stats := make([]DatabaseStats, 0, len(gj.databases))
		for name, ctx := range gj.databases {
			ds := DatabaseStats{
				Name:      name,
				Type:      ctx.dbtype,
				IsDefault: name == gj.defaultDB,
			}

			// Get table count from schema
			if ctx.schema != nil {
				tables := ctx.schema.GetTables()
				// Count non-virtual, non-blocked tables
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
		return stats
	}

	// Single-database mode (backward compatible)
	ds := DatabaseStats{
		Name:      "default",
		Type:      gj.dbtype,
		IsDefault: true,
	}

	// Get table count from schema
	if gj.schema != nil {
		tables := gj.schema.GetTables()
		count := 0
		for _, t := range tables {
			if t.Type != "virtual" && !t.Blocked {
				count++
			}
		}
		ds.TableCount = count
	}

	// Get pool stats
	if gj.db != nil {
		dbStats := gj.db.Stats()
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

	return []DatabaseStats{ds}
}
