package core

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
)

// DefaultDBName is the canonical name used for the primary/default database
// after config normalization. It replaces the empty-string and "_default" sentinels.
const DefaultDBName = "default"

// SupportedDBTypes lists the database types supported for single-database mode
var SupportedDBTypes = []string{"postgres", "mysql", "mariadb", "sqlite", "oracle", "mssql", "mongodb"}

// SupportedMultiDBTypes lists the database types supported for multi-database mode
var SupportedMultiDBTypes = []string{"postgres", "mysql", "mariadb", "sqlite", "oracle", "mongodb", "mssql"}

// ValidateDBType checks if the given database type is supported
func ValidateDBType(dbType string) error {
	if dbType == "" {
		return nil // Empty defaults to postgres, which is valid
	}
	for _, t := range SupportedDBTypes {
		if strings.EqualFold(dbType, t) {
			return nil
		}
	}
	return fmt.Errorf("unsupported database type %q: supported types are %s",
		dbType, strings.Join(SupportedDBTypes, ", "))
}

// ValidateMultiDBType checks if the given database type is supported for multi-database mode
func ValidateMultiDBType(dbType string) error {
	if dbType == "" {
		return nil // Empty defaults to postgres, which is valid
	}
	for _, t := range SupportedMultiDBTypes {
		if strings.EqualFold(dbType, t) {
			return nil
		}
	}
	return fmt.Errorf("unsupported database type %q: supported types are %s",
		dbType, strings.Join(SupportedMultiDBTypes, ", "))
}

// Validate checks the configuration for errors
func (c *Config) Validate() error {
	// Validate primary database type
	if err := ValidateDBType(c.DBType); err != nil {
		return err
	}

	// Validate multi-database types
	for name, dbConf := range c.Databases {
		if err := ValidateMultiDBType(dbConf.Type); err != nil {
			return fmt.Errorf("database %q: %w", name, err)
		}
	}

	return nil
}

// NormalizeDatabases ensures the primary database is represented as an entry
// in the Databases map, eliminating special-casing of empty targetDB strings.
// It is idempotent and should be called during core initialization.
func (c *Config) NormalizeDatabases() {
	// If no databases configured, create a default entry
	if len(c.Databases) == 0 {
		dbType := c.DBType
		if dbType == "" {
			dbType = "postgres"
		}
		c.Databases = map[string]DatabaseConfig{
			DefaultDBName: {
				Type: dbType,
			},
		}
	}

	// Pick the first entry (sorted) as the representative default
	// Sorting ensures deterministic behavior across Go map iterations.
	names := make([]string, 0, len(c.Databases))
	for name := range c.Databases {
		names = append(names, name)
	}
	sort.Strings(names)
	defaultName := names[0]

	// Sync DBType with the default entry's Type
	defConf := c.Databases[defaultName]
	if defConf.Type == "" && c.DBType != "" {
		defConf.Type = c.DBType
		c.Databases[defaultName] = defConf
	} else if defConf.Type != "" && c.DBType == "" {
		c.DBType = defConf.Type
	}

	// Tag tables that have empty Database with the default name
	for i := range c.Tables {
		if c.Tables[i].Database == "" {
			c.Tables[i].Database = defaultName
		}
	}

	// Propagate database-level ReadOnly to all roles' table entries
	// belonging to that database. This leverages the existing table-level
	// read_only enforcement in the core engine.
	for dbName, dbConf := range c.Databases {
		if !dbConf.ReadOnly {
			continue
		}
		for ri := range c.Roles {
			for ti := range c.Roles[ri].Tables {
				rt := &c.Roles[ri].Tables[ti]
				// Match tables that belong to this database
				for _, tbl := range c.Tables {
					if tbl.Database == dbName && strings.EqualFold(tbl.Name, rt.Name) {
						rt.ReadOnly = true
					}
				}
			}
		}
	}

	c.Databases[defaultName] = defConf
}

// Configuration for the GraphJin compiler core
type Config struct {
	// Is used to encrypt opaque values such as the cursor. Auto-generated when not set
	SecretKey string `mapstructure:"secret_key" json:"secret_key" yaml:"secret_key"  jsonschema:"title=Secret Key"`

	// When set to true it disables the allow list workflow
	DisableAllowList bool `mapstructure:"disable_allow_list" json:"disable_allow_list" yaml:"disable_allow_list" jsonschema:"title=Disable Allow List,default=false"`

	// When set to true a database schema file will be generated in dev mode and
	// used in production mode. Auto database discovery will be disabled
	// in production mode.
	EnableSchema bool `mapstructure:"enable_schema" json:"enable_schema" yaml:"enable_schema" jsonschema:"title=Enable Schema,default=false"`

	// When set to true an introspection json file will be generated in dev mode.
	// This file can be used with other GraphQL tooling to generate clients, enable
	// autocomplete, etc
	EnableIntrospection bool `mapstructure:"enable_introspection" json:"enable_introspection" yaml:"enable_introspection" jsonschema:"title=Generate introspection JSON,default=false"`

	// Forces the database session variable 'user.id' to be set to the user id
	SetUserID bool `mapstructure:"set_user_id" json:"set_user_id" yaml:"set_user_id" jsonschema:"title=Set User ID,default=false"`

	// This ensures that for anonymous users (role 'anon') all tables are blocked
	// from queries and mutations. To open access to tables for anonymous users
	// they have to be added to the 'anon' role config
	DefaultBlock bool `mapstructure:"default_block" json:"default_block" yaml:"default_block" jsonschema:"title=Block tables for anonymous users,default=true"`

	// This is a list of variables that can be leveraged in your queries.
	// (eg. variable admin_id will be $admin_id in the query)
	Vars map[string]string `mapstructure:"variables" json:"variables" yaml:"variables" jsonschema:"title=Variables"`

	// This is a list of variables that map to http header values
	HeaderVars map[string]string `mapstructure:"header_variables" json:"header_variables" yaml:"header_variables" jsonschema:"title=Header Variables"`

	// A list of tables and columns that should disallowed in any and all queries
	Blocklist []string `jsonschema:"title=Block List"`

	// The configs for custom resolvers. For example the `remote_api`
	// resolver would join json from a remote API into your query response
	Resolvers []ResolverConfig `jsonschema:"-"`

	// All table specific configuration such as aliased tables and relationships
	// between tables
	Tables []Table `jsonschema:"title=Tables"`

	// All function specific configuration such as return types
	Functions []Function `jsonschema:"title=Functions"`

	// An SQL query if set enables attribute based access control. This query is
	// used to fetch the user attribute that then dynamically define the users role
	RolesQuery string `mapstructure:"roles_query" json:"roles_query" yaml:"roles_query" jsonschema:"title=Roles Query"`

	// Roles contains the configuration for all the roles you want to support 'user' and
	// 'anon' are two default roles. The 'user' role is used when a user ID is available
	// and 'anon' when it's not. Use the 'Roles Query' config to add more custom roles
	Roles []Role

	// Database type name Defaults to 'postgres' (options: postgres, mysql, mariadb, sqlite, oracle, mssql)
	DBType string `mapstructure:"db_type" json:"db_type" yaml:"db_type" jsonschema:"title=Database Type,enum=postgres,enum=mysql,enum=mariadb,enum=sqlite,enum=oracle,enum=mssql"`

	// Log warnings and other debug information
	Debug bool `jsonschema:"title=Debug,default=false"`

	// Log SQL Query variable values
	LogVars bool `mapstructure:"log_vars" json:"log_vars" yaml:"log_vars" jsonschema:"title=Log Variables,default=false"`

	// Database polling duration (in seconds) used by subscriptions to
	// query for updates.
	SubsPollDuration time.Duration `mapstructure:"subs_poll_duration" json:"subs_poll_duration" yaml:"subs_poll_duration" jsonschema:"title=Subscription Polling Duration,default=5s"`

	// The default max limit (number of rows) when a limit is not defined in
	// the query or the table role config.
	DefaultLimit int `mapstructure:"default_limit" json:"default_limit" yaml:"default_limit" jsonschema:"title=Default Row Limit,default=20"`

	// Disable all aggregation functions like count, sum, etc
	DisableAgg bool `mapstructure:"disable_agg_functions" json:"disable_agg_functions" yaml:"disable_agg_functions" jsonschema:"title=Disable Aggregations,default=false"`

	// Disable all functions like count, length,  etc
	DisableFuncs bool `mapstructure:"disable_functions" json:"disable_functions" yaml:"disable_functions" jsonschema:"title=Disable Functions,default=false"`

	// When set to true, GraphJin will not connect to a database and instead
	// return mock data based on the query structure.
	MockDB bool `mapstructure:"mock_db" json:"mock_db" yaml:"mock_db" jsonschema:"title=Mock DB,default=false"`

	// Enable automatic coversion of camel case in GraphQL to snake case in SQL
	EnableCamelcase bool `mapstructure:"enable_camelcase" json:"enable_camelcase" yaml:"enable_camelcase" jsonschema:"title=Enable Camel Case,default=false"`

	// When enabled GraphJin runs with production level security defaults.
	// For example allow lists are enforced.
	Production bool `jsonschema:"title=Production Mode,default=false"`

	// Duration for polling the database to detect schema changes
	DBSchemaPollDuration time.Duration `mapstructure:"db_schema_poll_duration" json:"db_schema_poll_duration" yaml:"db_schema_poll_duration" jsonschema:"title=Schema Change Detection Polling Duration,default=10s"`

	// When set to true it disables production security features like enforcing the allow list
	DisableProdSecurity bool `mapstructure:"disable_production_security" json:"disable_production_security" yaml:"disable_production_security" jsonschema:"title=Disable Production Security"`

	// The filesystem to use for this instance of GraphJin
	FS interface{} `mapstructure:"-" jsonschema:"-" json:"-"`

	// Multiple database configurations for multi-database support.
	// When set, allows querying across multiple databases in a single GraphQL request.
	// Each database gets its own connection pool, schema, and SQL compiler.
	Databases map[string]DatabaseConfig `mapstructure:"databases" json:"databases" yaml:"databases" jsonschema:"title=Databases"`

	// CacheTrackingEnabled enables injection of __gj_id fields for cache row tracking.
	// This is set by the service layer when Redis caching is enabled.
	CacheTrackingEnabled bool `mapstructure:"-" json:"-" yaml:"-" jsonschema:"-"`
}

// DatabaseConfig defines configuration for a single database in multi-database mode
type DatabaseConfig struct {
	// Database type (postgres, mysql, mariadb, sqlite, oracle, mongodb)
	Type string `mapstructure:"type" json:"type" yaml:"type" jsonschema:"title=Database Type,enum=postgres,enum=mysql,enum=mariadb,enum=sqlite,enum=oracle,enum=mongodb"`

	// Connection string for the database (alternative to individual params)
	ConnString string `mapstructure:"connection_string" json:"connection_string" yaml:"connection_string" jsonschema:"title=Connection String"`

	// Database host
	Host string `mapstructure:"host" json:"host" yaml:"host" jsonschema:"title=Host"`

	// Database port
	Port int `mapstructure:"port" json:"port" yaml:"port" jsonschema:"title=Port"`

	// Database name
	DBName string `mapstructure:"dbname" json:"dbname" yaml:"dbname" jsonschema:"title=Database Name"`

	// Database user
	User string `mapstructure:"user" json:"user" yaml:"user" jsonschema:"title=User"`

	// Database password
	Password string `mapstructure:"password" json:"password" yaml:"password" jsonschema:"title=Password"`

	// File path for SQLite databases
	Path string `mapstructure:"path" json:"path" yaml:"path" jsonschema:"title=File Path (SQLite)"`

	// Maximum number of open connections
	MaxOpenConns int `mapstructure:"max_open_conns" json:"max_open_conns" yaml:"max_open_conns" jsonschema:"title=Max Open Connections"`

	// Maximum number of idle connections
	MaxIdleConns int `mapstructure:"max_idle_conns" json:"max_idle_conns" yaml:"max_idle_conns" jsonschema:"title=Max Idle Connections"`

	// Schema name to use (for databases that support schemas)
	Schema string `mapstructure:"schema" json:"schema" yaml:"schema" jsonschema:"title=Schema"`

	// Connection pool settings
	PoolSize        int           `mapstructure:"pool_size" json:"pool_size" yaml:"pool_size" jsonschema:"title=Connection Pool Size"`
	MaxConnections  int           `mapstructure:"max_connections" json:"max_connections" yaml:"max_connections" jsonschema:"title=Maximum Connections"`
	MaxConnIdleTime time.Duration `mapstructure:"max_connection_idle_time" json:"max_connection_idle_time" yaml:"max_connection_idle_time" jsonschema:"title=Connection Idle Time"`
	MaxConnLifeTime time.Duration `mapstructure:"max_connection_life_time" json:"max_connection_life_time" yaml:"max_connection_life_time" jsonschema:"title=Connection Life Time"`

	// Health check
	PingTimeout time.Duration `mapstructure:"ping_timeout" json:"ping_timeout" yaml:"ping_timeout" jsonschema:"title=Healthcheck Ping Timeout"`

	// TLS settings
	EnableTLS  bool   `mapstructure:"enable_tls" json:"enable_tls" yaml:"enable_tls" jsonschema:"title=Enable TLS"`
	ServerName string `mapstructure:"server_name" json:"server_name" yaml:"server_name" jsonschema:"title=TLS Server Name"`
	ServerCert string `mapstructure:"server_cert" json:"server_cert" yaml:"server_cert" jsonschema:"title=Server Certificate"`
	ClientCert string `mapstructure:"client_cert" json:"client_cert" yaml:"client_cert" jsonschema:"title=Client Certificate"`
	ClientKey  string `mapstructure:"client_key" json:"client_key" yaml:"client_key" jsonschema:"title=Client Key"`

	// MSSQL-specific: disable TLS encryption (go-mssqldb defaults to encrypt=true)
	Encrypt *bool `mapstructure:"encrypt" json:"encrypt,omitempty" yaml:"encrypt,omitempty" jsonschema:"title=MSSQL Encrypt"`

	// MSSQL-specific: trust server certificate without validation
	TrustServerCertificate *bool `mapstructure:"trust_server_certificate" json:"trust_server_certificate,omitempty" yaml:"trust_server_certificate,omitempty" jsonschema:"title=MSSQL Trust Server Certificate"`

	// Read-only mode — blocks all mutations and DDL against this database.
	// Once set in config, cannot be changed at runtime via MCP tools.
	ReadOnly bool `mapstructure:"read_only" json:"read_only" yaml:"read_only" jsonschema:"title=Read Only"`
}

// Configuration for a database table
type Table struct {
	Name      string
	Schema    string
	Table     string // Inherits Table
	Type      string
	// Database name for multi-database support. References a key in Config.Databases.
	// If empty, uses the default database.
	Database  string `mapstructure:"database" json:"database" yaml:"database" jsonschema:"title=Database"`
	Blocklist []string
	Columns   []Column
	// Permitted order by options
	OrderBy map[string][]string `mapstructure:"order_by" json:"order_by" yaml:"order_by" jsonschema:"title=Order By Options,example=created_at desc"`
}

// Configuration for a database table column
type Column struct {
	Name       string
	Type       string `jsonschema:"example=integer,example=text"`
	Primary    bool
	Array      bool
	FullText   bool   `mapstructure:"full_text" json:"full_text" yaml:"full_text" jsonschema:"title=Full Text Search"`
	ForeignKey string `mapstructure:"related_to" json:"related_to" yaml:"related_to" jsonschema:"title=Related To,example=other_table.id_column,example=users.id"`
}

// Configuration for a database function
type Function struct {
	Name       string
	Schema     string
	ReturnType string `mapstructure:"return_type" json:"return_type" yaml:"return_type" jsonschema:"title=Return Type,example=boolean,example=record"`
}

// Configuration for user role
type Role struct {
	Name    string
	Comment string
	Match   string      `jsonschema:"title=Related To,example=other_table.id_column,example=users.id"`
	Tables  []RoleTable `jsonschema:"title=Table Configuration for Role"`
	tm      map[string]*RoleTable
}

// Table configuration for a specific role (user role)
type RoleTable struct {
	Name     string
	Schema   string
	ReadOnly bool `mapstructure:"read_only" json:"read_only" yaml:"read_only" jsonschema:"title=Read Only"`

	Query  *Query
	Insert *Insert
	Update *Update
	Upsert *Upsert
	Delete *Delete
}

// Table configuration for querying a table with a role
type Query struct {
	Limit int
	// Use filters to enforce table wide things like { disabled: false } where you never want disabled users to be shown.
	Filters          []string
	Columns          []string
	DisableFunctions bool `mapstructure:"disable_functions" json:"disable_functions" yaml:"disable_functions"`
	Block            bool
}

// Table configuration for inserting into a table with a role
type Insert struct {
	Filters []string
	Columns []string
	Presets map[string]string
	Block   bool
}

// Table configuration for updating a table with a role
type Update struct {
	Filters []string
	Columns []string
	Presets map[string]string
	Block   bool
}

// Table configuration for creating/updating (upsert) a table with a role
type Upsert struct {
	Filters []string
	Columns []string
	Presets map[string]string
	Block   bool
}

// Table configuration for deleting from a table with a role
type Delete struct {
	Filters []string
	Columns []string
	Block   bool
}

// Resolver interface is used to create custom resolvers
// Custom resolvers must return a JSON value to be merged into
// the response JSON.
//
// Example Redis Resolver:
/*
	type Redis struct {
		Addr string
		client redis.Client
	}

	func newRedis(v map[string]interface{}) (*Redis, error) {
		re := &Redis{}
		if err := mapstructure.Decode(v, re); err != nil {
			return nil, err
		}
		re.client := redis.NewClient(&redis.Options{
			Addr:     re.Addr,
			Password: "", // no password set
			DB:       0,  // use default DB
		})
		return re, nil
	}

	func (r *remoteAPI) Resolve(req ResolverReq) ([]byte, error) {
		val, err := rdb.Get(ctx, req.ID).Result()
		if err != nil {
				return err
		}

		return val, nil
	}

	func main() {
		conf := core.Config{
			Resolvers: []Resolver{
				Name: "cached_profile",
				Type: "redis",
				Table: "users",
				Column: "id",
				Props: []ResolverProps{
					"addr": "localhost:6379",
				},
			},
		}

		redisRe := func(v ResolverProps) (Resolver, error) {
			return newRedis(v)
		}

		gj, err := core.NewGraphJin(conf, db,
			core.OptionSetResolver("redis" redisRe))
		if err != nil {
			log.Fatal(err)
		}
	}
*/
type Resolver interface {
	Resolve(context.Context, ResolverReq) ([]byte, error)
}

// ResolverProps is a map of properties from the resolver config to be passed
// to the customer resolver's builder (new) function
type ResolverProps map[string]interface{}

// ResolverConfig struct defines a custom resolver
type ResolverConfig struct {
	Name      string
	Type      string
	Schema    string
	Table     string
	Column    string
	StripPath string        `mapstructure:"strip_path" json:"strip_path" yaml:"strip_path"`
	Props     ResolverProps `mapstructure:",remain"`
}

type ResolverReq struct {
	ID  string
	Sel *qcode.Select
	Log *log.Logger
	*RequestConfig
}

// AddRoleTable function is a helper function to make it easy to add per-table
// row-level config
func (c *Config) AddRoleTable(role, table string, conf interface{}) error {
	var r *Role

	for i := range c.Roles {
		if strings.EqualFold(c.Roles[i].Name, role) {
			r = &c.Roles[i]
			break
		}
	}
	if r == nil {
		nr := Role{Name: role}
		c.Roles = append(c.Roles, nr)
		r = &c.Roles[len(c.Roles)-1]
	}

	var schema string

	if v := strings.SplitN(table, ".", 2); len(v) == 2 {
		schema = v[0]
		table = v[1]
	}

	var t *RoleTable
	for i := range r.Tables {
		if strings.EqualFold(r.Tables[i].Name, table) &&
			strings.EqualFold(r.Tables[i].Schema, schema) {
			t = &r.Tables[i]
			break
		}
	}
	if t == nil {
		nt := RoleTable{Name: table, Schema: schema}
		r.Tables = append(r.Tables, nt)
		t = &r.Tables[len(r.Tables)-1]
	}

	switch v := conf.(type) {
	case Query:
		t.Query = &v
	case Insert:
		t.Insert = &v
	case Update:
		t.Update = &v
	case Upsert:
		t.Upsert = &v
	case Delete:
		t.Delete = &v
	default:
		return fmt.Errorf("unsupported object type: %t", v)
	}
	return nil
}

func (c *Config) RemoveRoleTable(role, table string) error {
	ri := -1

	for i := range c.Roles {
		if strings.EqualFold(c.Roles[i].Name, role) {
			ri = i
			break
		}
	}
	if ri == -1 {
		return fmt.Errorf("role not found: %s", role)
	}

	tables := c.Roles[ri].Tables
	ti := -1

	var schema string

	if v := strings.SplitN(table, ".", 2); len(v) == 2 {
		schema = v[0]
		table = v[1]
	}

	for i, t := range tables {
		if strings.EqualFold(t.Name, table) &&
			strings.EqualFold(t.Schema, schema) {
			ti = i
			break
		}
	}
	if ti == -1 {
		return fmt.Errorf("table not found: %s", table)
	}

	c.Roles[ri].Tables = append(tables[:ti], tables[ti+1:]...)
	if len(c.Roles[ri].Tables) == 0 {
		c.Roles = append(c.Roles[:ri], c.Roles[ri+1:]...)
	}
	return nil
}
