package core

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
)

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

	// An SQL query if set enables attribute based access control. This query is
	// used to fetch the user attribute that then dynamically define the users role
	RolesQuery string `mapstructure:"roles_query" json:"roles_query" yaml:"roles_query" jsonschema:"title=Roles Query"`

	// Roles contains the configuration for all the roles you want to support 'user' and
	// 'anon' are two default roles. The 'user' role is used when a user ID is available
	// and 'anon' when it's not. Use the 'Roles Query' config to add more custom roles
	Roles []Role

	// Database type name Defaults to 'postgres' (options: mysql, postgres)
	DBType string `mapstructure:"db_type" json:"db_type" yaml:"db_type" jsonschema:"title=Database Type,enum=postgres,enum=mysql"`

	// Log warnings and other debug information
	Debug bool `jsonschema:"title=Debug,default=false"`

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
}

// Configuration for a database table
type Table struct {
	Name      string
	Schema    string
	Table     string // Inherits Table
	Type      string
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
	ForeignKey string `mapstructure:"related_to" json:"related_to" yaml:"related_to" jsonschema:"title=Related To,example=other_table.id_column,example=users.id"`
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
	*ReqConfig
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
