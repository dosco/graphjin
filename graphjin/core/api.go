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
			log.Fatalf(err)
		}

		conf, err := core.ReadInConfig("./config/dev.yml")
		if err != nil {
			log.Fatalf(err)
		}

		sg, err = core.NewSuperGraph(conf, db)
		if err != nil {
			log.Fatalf(err)
		}

		query := `
			query {
				posts {
				id
				title
			}
		}`

		res, err := sg.GraphQL(context.Background(), query, nil)
		if err != nil {
			log.Fatalf(err)
		}

		fmt.Println(string(res.Data))
	}
*/
package core

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	_log "log"
	"os"

	"github.com/dosco/super-graph/core/internal/allow"
	"github.com/dosco/super-graph/core/internal/crypto"
	"github.com/dosco/super-graph/core/internal/psql"
	"github.com/dosco/super-graph/core/internal/qcode"
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
	schema      *psql.DBSchema
	allowList   *allow.List
	encKey      [32]byte
	prepared    map[string]*preparedItem
	roles       map[string]*Role
	getRole     *sql.Stmt
	rmap        map[uint64]*resolvFn
	abacEnabled bool
	anonExists  bool
	qc          *qcode.Compiler
	pc          *psql.Compiler
}

// NewSuperGraph creates the SuperGraph struct, this involves querying the database to learn its
// schemas and relationships
func NewSuperGraph(conf *Config, db *sql.DB) (*SuperGraph, error) {
	sg := &SuperGraph{
		conf: conf,
		db:   db,
		log:  _log.New(os.Stdout, "", 0),
	}

	if err := sg.initConfig(); err != nil {
		return nil, err
	}

	if err := sg.initCompilers(); err != nil {
		return nil, err
	}

	if err := sg.initAllowList(); err != nil {
		return nil, err
	}

	if err := sg.initPrepared(); err != nil {
		return nil, err
	}

	if err := sg.initResolvers(); err != nil {
		return nil, err
	}

	if len(conf.SecretKey) != 0 {
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

// GraphQL function is called on the SuperGraph struct to convert the provided GraphQL query into an
// SQL query and execute it on the database. In production mode prepared statements are directly used
// and no query compiling takes places.
//
// In developer mode all names queries are saved into a file `allow.list` and in production mode only
// queries from this file can be run.
func (sg *SuperGraph) GraphQL(c context.Context, query string, vars json.RawMessage) (*Result, error) {
	ct := scontext{Context: c, sg: sg, query: query, vars: vars}

	if len(vars) <= 2 {
		ct.vars = nil
	}

	if keyExists(c, UserIDKey) {
		ct.role = "user"
	} else {
		ct.role = "anon"
	}

	ct.res.op = qcode.GetQType(query)
	ct.res.name = allow.QueryName(query)

	data, err := ct.execQuery()
	if err != nil {
		return &ct.res, err
	}

	ct.res.Data = json.RawMessage(data)

	return &ct.res, nil
}
