// Package core provides the primary API to include and use GraphJin with your own code.
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
	_log "log"
	"os"
	"sync"

	"github.com/chirino/graphql"
	"github.com/dosco/graphjin/core/internal/allow"
	"github.com/dosco/graphjin/core/internal/crypto"
	"github.com/dosco/graphjin/core/internal/psql"
	"github.com/dosco/graphjin/core/internal/qcode"
	"github.com/dosco/graphjin/core/internal/sdata"
)

type contextkey int

// Constants to set values on the context passed to the NewGraphJin function
const (
	// Name of the authentication provider. Eg. google, github, etc
	UserIDProviderKey contextkey = iota

	// User ID value for authenticated users
	UserIDKey

	// User role if pre-defined
	UserRoleKey
)

// GraphJin struct is an instance of the GraphJin engine it holds all the required information like
// datase schemas, relationships, etc that the GraphQL to SQL compiler would need to do it's job.
type GraphJin struct {
	conf        *Config
	db          *sql.DB
	log         *_log.Logger
	dbinfo      *sdata.DBInfo
	schema      *sdata.DBSchema
	allowList   *allow.List
	encKey      [32]byte
	queries     map[string]*cquery
	roles       map[string]*Role
	roleStmt    string
	roleStmtMD  psql.Metadata
	rmap        map[string]resItem
	abacEnabled bool
	qc          *qcode.Compiler
	pc          *psql.Compiler
	ge          *graphql.Engine
	subs        sync.Map
	prod        bool
}

// NewGraphJin creates the GraphJin struct, this involves querying the database to learn its
// schemas and relationships
func NewGraphJin(conf *Config, db *sql.DB) (*GraphJin, error) {
	return newGraphJin(conf, db, nil)
}

// newGraphJin helps with writing tests and benchmarks
func newGraphJin(conf *Config, db *sql.DB, dbinfo *sdata.DBInfo) (*GraphJin, error) {
	if conf == nil {
		conf = &Config{Debug: true, DisableAllowList: true}
	}

	gj := &GraphJin{
		conf:   conf,
		db:     db,
		dbinfo: dbinfo,
		log:    _log.New(os.Stdout, "", 0),
		prod:   conf.Production || os.Getenv("GO_ENV") == "production",
	}

	if err := gj.initConfig(); err != nil {
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
		conf.SecretKey = ""
		gj.encKey = sk
	} else {
		gj.encKey = crypto.NewEncryptionKey()
	}

	return gj, nil
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

// GraphQL function is called on the GraphJin struct to convert the provided GraphQL query into an
// SQL query and execute it on the database. In production mode prepared statements are directly used
// and no query compiling takes places.
//
// In developer mode all names queries are saved into a file `allow.list` and in production mode only
// queries from this file can be run.
func (gj *GraphJin) GraphQL(
	c context.Context,
	query string,
	vars json.RawMessage,
	rc *ReqConfig) (*Result, error) {

	op, name := qcode.GetQType(query)

	ct := scontext{
		Context: c,
		gj:      gj,
		op:      op,
		rc:      rc,
		name:    name,
	}

	res := &Result{
		op:   ct.op,
		name: ct.name,
	}

	if ct.op == qcode.QTSubscription {
		return res, errors.New("use 'core.Subscribe' for subscriptions")
	}

	if ct.op == qcode.QTMutation && gj.schema.DBType() == "mysql" {
		return res, errors.New("mysql: mutations not supported")
	}

	// use the chirino/graphql library for introspection queries
	// disabled when allow list is enforced
	if !gj.prod && ct.name == "IntrospectionQuery" {
		r := gj.ge.ServeGraphQL(&graphql.Request{Query: query})
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

// Operation function return the operation type and name from the query.
// It uses a very fast algorithm to extract the operation without having to parse the query.
func Operation(query string) (OpType, string) {
	qt, name := qcode.GetQType(query)
	return OpType(qt), name
}
