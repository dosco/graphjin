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
		_ "github.com/jackc/pgx/v4/stdlib"
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

	"github.com/chirino/graphql"
	"github.com/dop251/goja"
	"github.com/dosco/graphjin/core/internal/allow"
	"github.com/dosco/graphjin/core/internal/crypto"
	"github.com/dosco/graphjin/core/internal/graph"
	"github.com/dosco/graphjin/core/internal/psql"
	"github.com/dosco/graphjin/core/internal/qcode"
	"github.com/dosco/graphjin/core/internal/sdata"
	"github.com/dosco/graphjin/core/internal/util"
	"github.com/spf13/afero"
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
	conf        *Config
	db          *sql.DB
	log         *_log.Logger
	fs          afero.Fs
	dbtype      string
	dbinfo      *sdata.DBInfo
	schema      *sdata.DBSchema
	allowList   *allow.List
	encKey      [32]byte
	apq         apqCache
	queries     map[string]*queryComp
	roles       map[string]*Role
	roleStmt    string
	roleStmtMD  psql.Metadata
	rmap        map[string]resItem
	abacEnabled bool
	qc          *qcode.Compiler
	pc          *psql.Compiler
	ge          *graphql.Engine
	subs        sync.Map
	scripts     sync.Map
	prod        bool
	namespace   string
	tracer      trace.Tracer
	babelInit   bool
}

type GraphJin struct {
	atomic.Value
}

type script struct {
	util.Once

	ReqFunc  reqFunc
	RespFunc respFunc
	vm       *goja.Runtime
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

	gj := &graphjin{
		conf:   conf,
		db:     db,
		dbinfo: dbinfo,
		log:    _log.New(os.Stdout, "", 0),
		prod:   conf.Production || os.Getenv("GO_ENV") == "production",
		tracer: otel.Tracer("graphjin.com/core"),
	}

	if err := gj.initAPQCache(); err != nil {
		return nil, err
	}

	//order matters, do not re-order the initializers
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

	if err := gj.initScripting(); err != nil {
		return nil, err
	}

	if conf.SecretKey != "" {
		sk := sha256.Sum256([]byte(conf.SecretKey))
		conf.SecretKey = ""
		gj.encKey = sk
	} else {
		gj.encKey = crypto.NewEncryptionKey()
	}

	return gj, nil
}

func OptionSetNamespace(namespace string) Option {
	return func(s *graphjin) error {
		s.namespace = namespace
		return nil
	}
}

func OptionSetFS(fs afero.Fs) Option {
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
	Errors       []Error         `json:"errors,omitempty"`
	Vars         json.RawMessage `json:"-"`
	Data         json.RawMessage `json:"data,omitempty"`
	// Extensions   *extensions     `json:"extensions,omitempty"`
}

type Namespace struct {
	Name string
	Set  bool
}

// ReqConfig is used to pass request specific config values to the GraphQLEx and SubscribeEx functions. Dynamic variables can be set here.
type ReqConfig struct {
	// Namespace is used to namespace requests within a single instance of GraphJin. For example queries with the same name
	// can exist in allow list in seperate namespaces.
	Namespace Namespace

	// APQKey is set when using GraphJin with automatic persisted queries
	APQKey string

	// Pass additional variables complex variables such as functions that return string values.
	Vars map[string]interface{}
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

	var err error
	var op qcode.QType
	var name string

	gj := g.Load().(*graphjin)
	ns := gj.namespace

	if rc != nil && rc.Namespace.Set {
		ns = rc.Namespace.Name
	}

	if rc != nil && rc.APQKey != "" && query == "" {
		if v, ok := gj.apq.Get(ns, rc.APQKey); ok {
			query = v.query
			op = v.op
			name = v.name
		} else {
			return nil, errPersistedQueryNotFound
		}

	} else {
		if h, err := graph.FastParse(query); err == nil {
			op = qcode.GetQType(h.Type)
			name = h.Name
		} else {
			return nil, err
		}
	}

	// use the chirino/graphql library for introspection queries
	// disabled when allow list is enforced
	if !gj.prod && name == "IntrospectionQuery" {
		r := gj.ge.ServeGraphQL(&graphql.Request{Query: query})

		if err := r.Error(); err != nil {
			return errResult(ns, op, name, err), err
		}
		res := &Result{
			ns:   ns,
			op:   op,
			name: name,
			Data: r.Data,
		}
		return res, nil
	}

	if err != nil {
		return errResult(ns, op, name, err), err
	}

	var varMap map[string]interface{}

	if len(vars) != 0 {
		if err := json.Unmarshal(vars, &varMap); err != nil {
			return errResult(ns, op, name, err), err
		}
	}

	qc, res, err := gj.graphQL(c, op, ns, name, query, vars, rc)
	if err != nil {
		return res, err
	}

	if rc != nil && rc.APQKey != "" {
		gj.apq.Set(ns, rc.APQKey, apqInfo{
			op:    op,
			name:  name,
			query: query,
		})
	}

	if !gj.prod {
		if err := gj.saveToAllowList(
			qc,
			query,
			ns); err != nil {
			return res, err
		}
	}

	return res, err
}

func (g *GraphJin) GraphQLByName(
	c context.Context,
	operation OpType,
	name string,
	vars json.RawMessage,
	rc *ReqConfig) (*Result, error) {

	gj := g.Load().(*graphjin)
	ns := gj.namespace

	if rc != nil && rc.Namespace.Set {
		ns = rc.Namespace.Name
	}

	var op qcode.QType

	switch operation {
	case OpQuery:
		op = qcode.QTQuery
	case OpMutation:
		op = qcode.QTMutation
	default:
		err := fmt.Errorf("invalid operation: %d", operation)
		res := &Result{
			ns:     ns,
			op:     op,
			name:   name,
			Errors: []Error{{Message: err.Error()}},
		}
		return res, err
	}

	_, res, err := gj.graphQL(c, op, ns, name, "", vars, rc)
	return res, err
}

func (gj *graphjin) graphQL(
	ctx context.Context,
	op qcode.QType,
	ns string,
	name string,
	query string,
	vars json.RawMessage,
	rc *ReqConfig) (*qcode.QCode, *Result, error) {

	var err error

	ctx1, span := gj.spanStart(ctx, "GraphJin Query")
	defer span.End()

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

	if err != nil {
		res.Errors = []Error{{Message: err.Error()}}
		return nil, res, err
	}

	if ct.op == qcode.QTSubscription {
		return nil, res, errors.New("use 'core.Subscribe' for subscriptions")
	}

	if ct.op == qcode.QTMutation && gj.schema.DBType() == "mysql" {
		return nil, res, errors.New("mysql: mutations not supported")
	}

	var role string

	if v, ok := ctx1.Value(UserRoleKey).(string); ok {
		role = v
	} else if ctx1.Value(UserIDKey) != nil {
		role = "user"
	} else {
		role = "anon"
	}

	qr := queryReq{
		ns:    ct.ns,
		op:    ct.op,
		name:  ct.name,
		query: []byte(query),
		vars:  vars,
	}
	qres, err := ct.execQuery(ctx1, qr, role)

	if err != nil {
		res.Errors = []Error{{Message: err.Error()}}
	}

	var qc *qcode.QCode

	if qres.qc != nil {
		qc = qres.qc.st.qc
		res.sql = qres.qc.st.sql

		if qc != nil {
			res.cacheControl = qc.Cache.Header
		}
	}

	res.Data = json.RawMessage(qres.data)
	res.role = qres.role
	res.Vars = vars

	return qc, res, err
}

// Reload redoes database discover and reinitializes GraphJin.
func (g *GraphJin) Reload() error {
	gj := g.Load().(*graphjin)
	gjNew, err := newGraphJin(gj.conf, gj.db, nil)
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

type Header struct {
	Type OpType
	Name string
}

// Operation function return the operation type and name from the query.
// It uses a very fast algorithm to extract the operation without having to parse the query.
func Operation(query string) (Header, error) {
	if h, err := graph.FastParse(query); err == nil {
		t := OpType(qcode.GetQType(h.Type))
		return Header{t, h.Name}, nil
	} else {
		return Header{}, err
	}
}

func errResult(ns string, op qcode.QType, name string, err error) *Result {
	return &Result{
		ns:     ns,
		op:     op,
		name:   name,
		Errors: []Error{{Message: err.Error()}},
	}

}
