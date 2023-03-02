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

// GraphJin struct is an instance of the GraphJin engine it holds all the required information like
// datase schemas, relationships, etc that the GraphQL to SQL compiler would need to do it's job.
type graphjin struct {
	conf        *Config
	db          *sql.DB
	log         *_log.Logger
	fs          FS
	trace       Tracer
	dbtype      string
	dbinfo      *sdata.DBInfo
	schema      *sdata.DBSchema
	allowList   *allow.List
	encKey      [32]byte
	encKeySet   bool
	cache       Cache
	queries     sync.Map
	roles       map[string]*Role
	roleStmt    string
	roleStmtMD  psql.Metadata
	tmap        map[string]qcode.TConfig
	rtmap       map[string]ResolverFn
	rmap        map[string]resItem
	abacEnabled bool
	qc          *qcode.Compiler
	pc          *psql.Compiler
	subs        sync.Map
	prod        bool
	prodSec     bool
	namespace   string
	pf          []byte
	opts        []Option
	done        chan bool
}

type GraphJin struct {
	atomic.Value
	done chan bool
}

type Option func(*graphjin) error

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

	gj := &graphjin{
		conf:    conf,
		db:      db,
		dbinfo:  dbinfo,
		log:     _log.New(os.Stdout, "", 0),
		prod:    conf.Production,
		prodSec: conf.Production,
		pf:      []byte(fmt.Sprintf("gj/%x:", t.Unix())),
		opts:    options,
		fs:      fs,
		trace:   &tracer{},
		done:    g.done,
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

	if err = gj.initAllowList(); err != nil {
		return
	}

	if err = gj.initCompilers(); err != nil {
		return
	}

	if err = gj.prepareRoleStmt(); err != nil {
		return
	}

	if conf.SecretKey != "" {
		sk := sha256.Sum256([]byte(conf.SecretKey))
		gj.encKey = sk
		gj.encKeySet = true
	}

	g.Store(gj)
	return
}

func OptionSetNamespace(namespace string) Option {
	return func(s *graphjin) error {
		s.namespace = namespace
		return nil
	}
}

func OptionSetFS(fs FS) Option {
	return func(s *graphjin) error {
		s.fs = fs
		return nil
	}
}

func OptionSetTrace(trace Tracer) Option {
	return func(s *graphjin) error {
		s.trace = trace
		return nil
	}
}

func OptionSetResolver(name string, fn ResolverFn) Option {
	return func(s *graphjin) error {
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

type Error struct {
	Message string `json:"message"`
}

// Result struct contains the output of the GraphQL function this includes resulting json from the
// database query and any error information
type Result struct {
	ns           string
	op           qcode.QType
	name         string
	sql          string
	role         string
	cacheControl string
	Vars         json.RawMessage   `json:"-"`
	Data         json.RawMessage   `json:"data,omitempty"`
	Hash         [sha256.Size]byte `json:"-"`
	Errors       []Error           `json:"errors,omitempty"`
	Validation   []qcode.ValidErr  `json:"validation,omitempty"`
	// Extensions   *extensions     `json:"extensions,omitempty"`
}

// ReqConfig is used to pass request specific config values to the GraphQL and Subscribe functions. Dynamic variables can be set here.
type ReqConfig struct {
	ns *string

	// APQKey is set when using GraphJin with automatic persisted queries
	APQKey string

	// Pass additional variables complex variables such as functions that return string values.
	Vars map[string]interface{}

	// Execute this query as part of a transaction
	Tx *sql.Tx
}

// SetNamespace is used to set namespace requests within a single instance of GraphJin. For example queries with the same name
func (rc *ReqConfig) SetNamespace(ns string) {
	rc.ns = &ns
}

func (rc *ReqConfig) GetNamespace() (string, bool) {
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
	rc *ReqConfig,
) (res *Result, err error) {
	gj := g.Load().(*graphjin)

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
		if err = gj.saveToAllowList(resp.qc, resp.res.ns); err != nil {
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
	rc *ReqConfig,
) (res *Result, err error) {
	if rc == nil {
		rc = &ReqConfig{Tx: tx}
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
	rc *ReqConfig,
) (res *Result, err error) {
	gj := g.Load().(*graphjin)

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
	rc *ReqConfig,
) (res *Result, err error) {
	if rc == nil {
		rc = &ReqConfig{Tx: tx}
	} else {
		rc.Tx = tx
	}
	return g.GraphQLByName(c, name, vars, rc)
}

type graphqlReq struct {
	ns      string
	op      qcode.QType
	name    string
	query   []byte
	vars    json.RawMessage
	aschema map[string]json.RawMessage
	rc      *ReqConfig
}

type graphqlResp struct {
	res Result
	qc  *qcode.QCode
}

func (gj *graphjin) newGraphqlReq(rc *ReqConfig,
	op string,
	name string,
	query []byte,
	vars json.RawMessage,
) (r graphqlReq) {
	r = graphqlReq{
		op:    qcode.GetQTypeByName(op),
		name:  name,
		query: query,
		vars:  vars,
	}

	if rc != nil {
		r.rc = rc
	}
	if rc != nil && rc.ns != nil {
		r.ns = *rc.ns
	} else {
		r.ns = gj.namespace
	}
	return
}

func (r *graphqlReq) Set(item allow.Item) {
	r.ns = item.Namespace
	r.op = qcode.GetQTypeByName(item.Operation)
	r.name = item.Name
	r.query = item.Query
	r.aschema = item.ActionJSON
}

func (gj *graphjin) queryWithResult(c context.Context, r graphqlReq) (res *Result, err error) {
	resp, err := gj.query(c, r)
	return &resp.res, err
}

func (gj *graphjin) query(c context.Context, r graphqlReq) (
	resp graphqlResp, err error,
) {
	resp.res = Result{
		ns:   r.ns,
		op:   r.op,
		name: r.name,
	}

	if !gj.prodSec && r.name == "IntrospectionQuery" {
		resp.res.Data, err = gj.getIntroResult()
		return
	}

	if r.op == qcode.QTSubscription {
		err = errors.New("use 'core.Subscribe' for subscriptions")
		return
	}

	if r.op == qcode.QTMutation && gj.schema.DBType() == "mysql" {
		err = errors.New("mysql: mutations not supported")
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
	resp.res.Data = json.RawMessage(s.data)
	resp.res.Hash = s.dhash
	resp.res.role = s.role

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
	return g.reload(nil)
}

func (g *GraphJin) reload(di *sdata.DBInfo) (err error) {
	gj := g.Load().(*graphjin)
	err = g.newGraphJin(gj.conf, gj.db, di, gj.fs, gj.opts...)
	return
}

// IsProd return true for production mode or false for development mode
func (g *GraphJin) IsProd() bool {
	gj := g.Load().(*graphjin)
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

func newError(err error) (errList []Error) {
	errList = []Error{{Message: err.Error()}}
	return
}
