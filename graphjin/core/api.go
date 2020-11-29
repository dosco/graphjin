// Package core provides the primary API to include and use Super Graph with your own code.
// For detailed documentation visit https://supergraph.dev
//
// Example usage:
/*
	package main

	import (
		"database/sql"
		"fmt"
		"time"
		"github.com/dosco/super-graph/core"
		_ "github.com/jackc/pgx/v4/stdlib"
	)

	func main() {
		db, err := sql.Open("pgx", "postgres://postgrs:@localhost:5432/example_db")
		if err != nil {
			log.Fatal(err)
		}

		sg, err := core.NewSuperGraph(nil, db)
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

		res, err := sg.GraphQL(ctx, query, nil)
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
	"hash/maphash"
	_log "log"
	"os"
	"sync"

	"github.com/chirino/graphql"
	"github.com/dosco/super-graph/core/internal/allow"
	"github.com/dosco/super-graph/core/internal/crypto"
	"github.com/dosco/super-graph/core/internal/psql"
	"github.com/dosco/super-graph/core/internal/qcode"
	"github.com/dosco/super-graph/core/internal/sdata"
)

type contextkey int

// Constants to set values on the context passed to the NewSuperGraph function
const (
	// Name of the authentication provider. Eg. google, github, etc
	UserIDProviderKey contextkey = iota

	// User ID value for authenticated users
	UserIDKey

	// User role if pre-defined
	UserRoleKey
)

// SuperGraph struct is an instance of the Super Graph engine it holds all the required information like
// datase schemas, relationships, etc that the GraphQL to SQL compiler would need to do it's job.
type SuperGraph struct {
	conf        *Config
	db          *sql.DB
	log         *_log.Logger
	dbinfo      *sdata.DBInfo
	schema      *sdata.DBSchema
	allowList   *allow.List
	encKey      [32]byte
	hashSeed    maphash.Seed
	queries     map[string]*cquery
	roles       map[string]*Role
	roleStmt    string
	roleStmtMD  psql.Metadata
	rmap        map[uint64]resolvFn
	abacEnabled bool
	qc          *qcode.Compiler
	pc          *psql.Compiler
	ge          *graphql.Engine
	subs        sync.Map
}

// NewSuperGraph creates the SuperGraph struct, this involves querying the database to learn its
// schemas and relationships
func NewSuperGraph(conf *Config, db *sql.DB) (*SuperGraph, error) {
	return newSuperGraph(conf, db, nil)
}

// newSuperGraph helps with writing tests and benchmarks
func newSuperGraph(conf *Config, db *sql.DB, dbinfo *sdata.DBInfo) (*SuperGraph, error) {
	if conf == nil {
		conf = &Config{Debug: true}
	}

	sg := &SuperGraph{
		conf:     conf,
		db:       db,
		dbinfo:   dbinfo,
		log:      _log.New(os.Stdout, "", 0),
		hashSeed: maphash.MakeSeed(),
	}

	if err := sg.initConfig(); err != nil {
		return nil, err
	}

	if err := sg.initSchema(); err != nil {
		return nil, err
	}

	if err := sg.initResolvers(); err != nil {
		return nil, err
	}

	if err := sg.initAllowList(); err != nil {
		return nil, err
	}

	if err := sg.initCompilers(); err != nil {
		return nil, err
	}

	if err := sg.initGraphQLEgine(); err != nil {
		return nil, err
	}

	if err := sg.prepareRoleStmt(); err != nil {
		return nil, err
	}

	if conf.SecretKey != "" {
		sk := sha256.Sum256([]byte(conf.SecretKey))
		conf.SecretKey = ""
		sg.encKey = sk
	} else {
		sg.encKey = crypto.NewEncryptionKey()
	}

	return sg, nil
}

// Result struct contains the output of the GraphQL function this includes resulting json from the
// database query and any error information
type Result struct {
	op   qcode.QType
	name string
	sql  string
	role string

	Error      string          `json:"message,omitempty"`
	Data       json.RawMessage `json:"data,omitempty"`
	Extensions *extensions     `json:"extensions,omitempty"`
}

// ReqConfig is used to pass request specific config values to the GraphQLEx and SubscribeEx functions. Dynamic variables can be set here.
type ReqConfig struct {
	Vars map[string]interface{}
}

// GraphQL function is called on the SuperGraph struct to convert the provided GraphQL query into an
// SQL query and execute it on the database. In production mode prepared statements are directly used
// and no query compiling takes places.
//
// In developer mode all names queries are saved into a file `allow.list` and in production mode only
// queries from this file can be run.
func (sg *SuperGraph) GraphQL(c context.Context, query string, vars json.RawMessage) (*Result, error) {
	return sg.GraphQLEx(c, query, vars, nil)
}

// GraphQLEx is the extended version of the GraphQL function allowing for request specific config.
func (sg *SuperGraph) GraphQLEx(
	c context.Context,
	query string,
	vars json.RawMessage,
	rc *ReqConfig) (*Result, error) {

	ct := scontext{
		Context: c,
		sg:      sg,
		op:      qcode.GetQType(query),
		rc:      rc,
		name:    Name(query),
	}

	res := &Result{
		op:   ct.op,
		name: ct.name,
	}

	if ct.op == qcode.QTSubscription {
		return res, errors.New("use 'core.Subscribe' for subscriptions")
	}

	// use the chirino/graphql library for introspection queries
	// disabled when allow list is enforced
	if !sg.conf.EnforceAllowList && ct.name == "IntrospectionQuery" {
		r := sg.ge.ServeGraphQL(&graphql.Request{Query: query})
		res.Data = r.Data

		if r.Error() != nil {
			res.Error = r.Error().Error()
		}
		return res, r.Error()
	}

	var role string

	if keyExists(c, UserIDKey) {
		role = "user"
	} else {
		role = "anon"
	}

	qr, err := ct.execQuery(query, vars, role)

	if err != nil {
		res.Error = err.Error()
	}

	if qr.q != nil {
		res.sql = qr.q.st.sql
	}

	res.Data = json.RawMessage(qr.data)
	res.role = qr.role

	return res, err
}

// GraphQLSchema function return the GraphQL schema for the underlying database connected
// to this instance of Super Graph
func (sg *SuperGraph) GraphQLSchema() (string, error) {
	return sg.ge.Schema.String(), nil
}

// Operation function return the operation type from the query. It uses a very fast algorithm to
// extract the operation without having to parse the query.
func Operation(query string) OpType {
	return OpType(qcode.GetQType(query))
}

// Name function return the operation name from the query. It uses a very fast algorithm to
// extract the operation name without having to parse the query.
func Name(query string) string {
	return allow.QueryName(query)
}
