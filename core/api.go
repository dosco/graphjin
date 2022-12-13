// Package core provides an API to include and use the GraphJin compiler with your own code.
// For detailed documentation visit https://graphjin.com
//
// Example usage:
/*
	package main

	import (
		"database/sql"
		"fmt"
		"time"
		"github.com/dosco/graphjin/core"
		_ "github.com/jackc/pgx/v5/stdlib"
	)

	func main() {
		db, err := sql.Open("pgx", "postgres://postgrs:@localhost:5432/example_db")
		if err != nil {
			log.Fatal(err)
		}

		gj, err := core.NewGraphJin(nil, db)
		if err != nil {
			log.Fatal(err)
		}

		query := `
			query {
				posts {
				id
				title
			}
		}`

		ctx = context.WithValue(ctx, core.UserIDKey, 1)

		res, err := gj.GraphQL(ctx, query, nil)
		if err != nil {
			log.Fatal(err)
		}

	}
*/
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
	"sync"
	"sync/atomic"
	"time"

	"github.com/chirino/graphql"
	"github.com/dosco/graphjin/core/internal/allow"
	"github.com/dosco/graphjin/core/internal/graph"
	"github.com/dosco/graphjin/core/internal/psql"
	"github.com/dosco/graphjin/core/internal/qcode"
	"github.com/dosco/graphjin/core/internal/sdata"
	"github.com/dosco/graphjin/plugin"
	"github.com/dosco/graphjin/plugin/fs"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
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

// GraphJin struct is an instance of the GraphJin engine it holds all the required information like
// datase schemas, relationships, etc that the GraphQL to SQL compiler would need to do it's job.
type graphjin struct {
	conf         *Config
	db           *sql.DB
	log          *_log.Logger
	fs           plugin.FS
	dbtype       string
	dbinfo       *sdata.DBInfo
	schema       *sdata.DBSchema
	allowList    *allow.List
	encKey       [32]byte
	encKeySet    bool
	apq          apqCache
	queries      sync.Map
	roles        map[string]*Role
	roleStmt     string
	roleStmtMD   psql.Metadata
	rmap         map[string]resItem
	abacEnabled  bool
	qc           *qcode.Compiler
	pc           *psql.Compiler
	ge           *graphql.Engine
	subs         sync.Map
	scriptMap    map[string]plugin.ScriptCompiler
	validatorMap map[string]plugin.ValidationCompiler
	prod         bool
	namespace    string
	tracer       trace.Tracer
	pf           []byte
	opts         []Option
}

type GraphJin struct {
	atomic.Value
}

type Option func(*graphjin) error

var (
	errPersistedQueryNotFound = errors.New("persisted query not found")
)

// NewGraphJin creates the GraphJin struct, this involves querying the database to learn its
// schemas and relationships
func NewGraphJin(conf *Config, db *sql.DB, options ...Option) (*GraphJin, error) {
	gj, err := newGraphJin(conf, db, nil, options...)
	if err != nil {
		return nil, err
	}

	g := &GraphJin{}
	g.Store(gj)

	if err := g.initDBWatcher(); err != nil {
		return nil, err
	}
	return g, nil
}

// newGraphJin helps with writing tests and benchmarks
func newGraphJin(conf *Config, db *sql.DB, dbinfo *sdata.DBInfo, options ...Option) (*graphjin, error) {
	if conf == nil {
		conf = &Config{Debug: true, DisableAllowList: true}
	}

	t := time.Now()

	gj := &graphjin{
		conf:      conf,
		db:        db,
		dbinfo:    dbinfo,
		log:       _log.New(os.Stdout, "", 0),
		prod:      conf.Production,
		tracer:    otel.Tracer("graphjin.com/core"),
		pf:        []byte(fmt.Sprintf("gj/%x:", t.Unix())),
		opts:      options,
		scriptMap: make(map[string]plugin.ScriptCompiler),
	}

	// ordering of these initializer matter, do not re-order!

	if err := gj.initScript(); err != nil {
		return nil, err
	}

	if err := gj.initAPQCache(); err != nil {
		return nil, err
	}

	if err := gj.initConfig(); err != nil {
		return nil, err
	}

	for _, op := range options {
		if err := op(gj); err != nil {
			return nil, err
		}
	}

	if err := gj.initFS(); err != nil {
		return nil, err
	}

	if err := gj.initDiscover(); err != nil {
		return nil, err
	}

	if err := gj.initResolvers(); err != nil {
		return nil, err
	}

	if err := gj.initSchema(); err != nil {
		return nil, err
	}

	if err := gj.initAllowList(); err != nil {
		return nil, err
	}

	if err := gj.initCompilers(); err != nil {
		return nil, err
	}

	if err := gj.initGraphQLEgine(); err != nil {
		return nil, err
	}

	if err := gj.prepareRoleStmt(); err != nil {
		return nil, err
	}

	if conf.SecretKey != "" {
		sk := sha256.Sum256([]byte(conf.SecretKey))
		gj.encKey = sk
		gj.encKeySet = true
	}

	return gj, nil
}

func OptionSetNamespace(namespace string) Option {
	return func(s *graphjin) error {
		s.namespace = namespace
		return nil
	}
}

func OptionSetScriptCompiler(ext []string, se plugin.ScriptCompiler) Option {
	return func(s *graphjin) error {
		if s.scriptMap == nil {
			s.scriptMap = make(map[string]plugin.ScriptCompiler)
		}
		for _, v := range ext {
			s.scriptMap[v] = se
		}
		return nil
	}
}

func OptionSetValidator(name string, v plugin.ValidationCompiler) Option {
	return func(s *graphjin) error {
		if s.validatorMap == nil {
			s.validatorMap = make(map[string]plugin.ValidationCompiler)
		}
		s.validatorMap[name] = v
		return nil
	}
}

func OptionSetFS(fs plugin.FS) Option {
	return func(s *graphjin) error {
		s.fs = fs
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
	actionJSON   json.RawMessage
	Errors       []Error           `json:"errors,omitempty"`
	Vars         json.RawMessage   `json:"-"`
	Data         json.RawMessage   `json:"data,omitempty"`
	Hash         [sha256.Size]byte `json:"-"`
	// Extensions   *extensions     `json:"extensions,omitempty"`
}

// ReqConfig is used to pass request specific config values to the GraphQLEx and SubscribeEx functions. Dynamic variables can be set here.
type ReqConfig struct {
	ns *string

	// APQKey is set when using GraphJin with automatic persisted queries
	APQKey string

	// Pass additional variables complex variables such as functions that return string values.
	Vars map[string]interface{}
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

// GraphQL function is called on the GraphJin struct to convert the provided GraphQL query into an
// SQL query and execute it on the database. In production mode prepared statements are directly used
// and no query compiling takes places.
//
// In developer mode all names queries are saved into a file `allow.list` and in production mode only
// queries from this file can be run.
func (g *GraphJin) GraphQL(
	c context.Context,
	query string,
	vars json.RawMessage,
	rc *ReqConfig) (*Result, error) {

	gj := g.Load().(*graphjin)
	ns := gj.namespace

	c1, span := gj.spanStart(c, "GraphJin Query")
	defer span.End()

	if rc != nil {
		if rc.ns != nil {
			ns = *rc.ns
		}
		if rc.APQKey != "" && query == "" {
			if v, ok := gj.apq.Get(ns, rc.APQKey); ok {
				query = v.query
			} else {
				return nil, errPersistedQueryNotFound
			}
		}
	}

	res, err := gj.graphQL(c1, query, vars, rc)
	if err != nil {
		return res, err
	}

	if rc != nil && rc.APQKey != "" {
		gj.apq.Set(ns, rc.APQKey, apqInfo{query: query})
	}

	if !gj.prod {
		err := gj.saveToAllowList(res.actionJSON, query, res.ns)
		if err != nil {
			return res, err
		}
	}

	return res, err
}

func (g *GraphJin) GraphQLByName(
	c context.Context,
	name string,
	vars json.RawMessage,
	rc *ReqConfig) (*Result, error) {

	gj := g.Load().(*graphjin)

	c1, span := gj.spanStart(c, "GraphJin Query")
	defer span.End()

	item, err := gj.allowList.GetByName(name, gj.prod)
	if err != nil {
		return nil, err
	}
	op := qcode.GetQTypeByName(item.Operation)
	query := item.Query

	return gj.graphQLWithOpName(c1, op, name, query, vars, rc)
}

func (gj *graphjin) graphQL(
	c context.Context,
	query string,
	vars json.RawMessage,
	rc *ReqConfig) (*Result, error) {

	var op qcode.QType
	var name string

	if h, err := graph.FastParse(query); err == nil {
		name = h.Name
		op = qcode.GetQTypeByName(h.Operation)
	} else {
		return nil, err
	}

	if gj.prod && !gj.conf.DisableAllowList {
		item, err := gj.allowList.GetByName(name, gj.prod)
		if err != nil {
			return nil, err
		}
		op = qcode.GetQTypeByName(item.Operation)
		query = item.Query
	}
	return gj.graphQLWithOpName(c, op, name, query, vars, rc)
}

func (gj *graphjin) graphQLWithOpName(
	c context.Context,
	op qcode.QType,
	name string,
	query string,
	vars json.RawMessage,
	rc *ReqConfig) (*Result, error) {

	ns := gj.namespace
	if rc != nil && rc.ns != nil {
		ns = *rc.ns
	}

	// use the chirino/graphql library for introspection queries
	// disabled when allow list is enforced
	if !gj.prod && name == "IntrospectionQuery" {
		res, err := gj.introspection(query)
		return res, err
	}

	ct := &gcontext{
		gj:   gj,
		rc:   rc,
		ns:   ns,
		op:   op,
		name: name,
	}

	res := &Result{
		ns:   ns,
		op:   op,
		name: name,
	}

	if ct.op == qcode.QTSubscription {
		return res, errors.New("use 'core.Subscribe' for subscriptions")
	}

	if ct.op == qcode.QTMutation && gj.schema.DBType() == "mysql" {
		return res, errors.New("mysql: mutations not supported")
	}

	var role string

	if v, ok := c.Value(UserRoleKey).(string); ok {
		role = v
	} else {
		switch c.Value(UserIDKey).(type) {
		case string, int:
			role = "user"
		default:
			role = "anon"
		}
	}

	qr := queryReq{
		ns:    ct.ns,
		op:    ct.op,
		name:  ct.name,
		query: []byte(query),
		vars:  vars,
	}

	qres, err := ct.execQuery(c, qr, role)
	if err != nil {
		res.Errors = []Error{{Message: err.Error()}}
	}

	res.actionJSON = qres.actionVar()
	res.sql = qres.sql()
	res.cacheControl = qres.cacheHeader()

	res.Data = json.RawMessage(qres.data)
	res.Hash = qres.dhash
	res.role = qres.role
	res.Vars = vars

	return res, err
}

func (g *GraphJin) Introspection(query string) (*Result, error) {
	gj := g.Load().(*graphjin)
	return gj.introspection(query)
}

func (gj *graphjin) introspection(query string) (*Result, error) {
	r := gj.ge.ServeGraphQL(&graphql.Request{Query: query})
	if err := r.Error(); err != nil {
		return errResult("", err), err
	}
	return &Result{Data: r.Data}, nil
}

// Reload redoes database discover and reinitializes GraphJin.
func (g *GraphJin) Reload() error {
	gj := g.Load().(*graphjin)
	gjNew, err := newGraphJin(gj.conf, gj.db, nil, gj.opts...)
	if err == nil {
		g.Store(gjNew)
	}
	return err
}

// IsProd return true for production mode or false for development mode
func (g *GraphJin) IsProd() bool {
	gj := g.Load().(*graphjin)
	return gj.prod
}

func Upgrade(configPath string) error {
	fs := fs.NewOsFSWithBase(configPath)
	al, err := allow.New(nil, fs, false)
	if err != nil {
		return fmt.Errorf("failed to initialize allow list: %w", err)
	}
	return al.Upgrade()
}

type Header struct {
	Type OpType
	Name string
}

// Operation function return the operation type and name from the query.
// It uses a very fast algorithm to extract the operation without having to parse the query.
func Operation(query string) (Header, error) {
	if h, err := graph.FastParse(query); err == nil {
		t := OpType(qcode.GetQTypeByName(h.Operation))
		return Header{t, h.Name}, nil
	} else {
		return Header{}, err
	}
}

func errResult(name string, err error) *Result {
	return &Result{
		name:   name,
		Errors: []Error{{Message: err.Error()}},
	}

}
