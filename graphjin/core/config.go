package core

import (
	"fmt"
	"log"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/dosco/graphjin/core/internal/qcode"
	"github.com/spf13/afero"
	"github.com/spf13/viper"
)

// Core struct contains core specific config value
type Config struct {
	// SecretKey is used to encrypt opaque values such as
	// the cursor. Auto-generated if not set
	SecretKey string `mapstructure:"secret_key"`

	// DisableAllowList when set to true entirely disables the
	// allow list workflow and all queries are always compiled
	// even in production (Warning possible security concern)
	DisableAllowList bool `mapstructure:"disable_allow_list"`

	// ConfigPath is the default path to find all configuration
	// files and scripts under
	ConfigPath string `mapstructure:"config_path"`

	// ScriptPath if the path to the script files if not set the
	// path is assumed to be the same as the config path
	ScriptPath string `mapstructure:"script_path"`

	// SetUserID forces the database session variable `user.id` to
	// be set to the user id
	SetUserID bool `mapstructure:"set_user_id"`

	// DefaultBlock ensures that in anonymous mode (role 'anon') all tables
	// are blocked from queries and mutations. To open access to tables in
	// anonymous mode they have to be added to the 'anon' role config
	DefaultBlock bool `mapstructure:"default_block"`

	// Vars is a map of hardcoded variables that can be leveraged in your
	// queries. (eg. variable admin_id will be $admin_id in the query)
	Vars map[string]string `mapstructure:"variables"`

	// HeaderVars is a map of dynamic variables that map to http header values
	HeaderVars map[string]string `mapstructure:"header_variables"`

	// Blocklist is a list of tables and columns that should be filtered
	// out from any and all queries
	Blocklist []string

	// Resolvers contain the configs for custom resolvers. For example the `remote_api`
	// resolver would join json from a remote API into your query response
	Resolvers []ResolverConfig

	// Tables contains all table specific configuration such as aliased tables
	// creating relationships between tables, etc
	Tables []Table

	// RolesQuery if set enabled attribute based access control. This query
	// is used to fetch the user attribute that then dynamically define the users
	// role
	RolesQuery string `mapstructure:"roles_query"`

	// Roles contains all the configuration for all the roles you want to support
	// `user` and `anon` are two default roles. User role is for when a user ID is
	// available and Anon when it's not
	//
	// If you're using the RolesQuery config to enable atribute based acess control then
	// you can add more custom roles
	Roles []Role

	// Inflections is to add additionally singular to plural mappings
	// to the engine (eg. sheep: sheep)
	Inflections []string `mapstructure:"inflections"`

	// Disable inflections. Inflections are deprecated and will be
	// removed in next major version
	EnableInflection bool `mapstructure:"enable_inflection"`

	// Customize singular suffix
	// By default is set to "ByID"
	SingularSuffix string `mapstructure:"singular_suffix"`

	// Database type name Defaults to 'postgres' (options: mysql, postgres)
	DBType string `mapstructure:"db_type"`

	// Log warnings and other debug information
	Debug bool

	// SubsPollDuration is the database polling duration (in seconds)
	// used by subscriptions to query for updates.
	// Default set to 5 seconds
	SubsPollDuration time.Duration `mapstructure:"subs_poll_every_seconds"`

	// DefaultLimit sets the default max limit (number of rows) when a
	// limit is not defined in the query or the table role config
	// Default set to 20
	DefaultLimit int `mapstructure:"default_limit"`

	// DisableAgg disables all aggregation functions like count, sum, etc
	DisableAgg bool `mapstructure:"disable_agg_functions"`

	// DisableFuncs disables all functions like count, length,  etc
	DisableFuncs bool `mapstructure:"disable_functions"`

	// EnableCamelcase enables autp camel case terms in GraphQL to snake case in SQL
	EnableCamelcase bool `mapstructure:"enable_camelcase"`

	// Enable production mode. This defaults to true if GO_ENV is set to
	// "production". When true the allow list is enforced
	Production bool

	// DBSchemaPollDuration sets the duration for polling the database
	// schema to detect changes to it. GraphJin is reinitialized when a
	// change is detected
	DBSchemaPollDuration time.Duration `mapstructure:"db_schema_poll_every_seconds"`

	rtmap map[string]refunc
	tmap  map[string]qcode.TConfig
}

// Table struct defines a database table
type Table struct {
	Name      string
	Schema    string
	Table     string
	Type      string
	Blocklist []string
	Columns   []Column
	OrderBy   map[string][]string `mapstructure:"order_by"`
}

// Column struct defines a database column
type Column struct {
	ID         int32
	Name       string
	Type       string
	Primary    bool
	Array      bool
	ForeignKey string `mapstructure:"related_to"`
}

// Role struct contains role specific access control values for for all database tables
type Role struct {
	Name   string
	Match  string
	Tables []RoleTable
	tm     map[string]*RoleTable
}

// RoleTable struct contains role specific access control values for a database table
type RoleTable struct {
	Name     string
	Schema   string
	ReadOnly bool `mapstructure:"read_only"`

	Query  *Query
	Insert *Insert
	Update *Update
	Upsert *Upsert
	Delete *Delete
}

// Query struct contains access control values for query operations
type Query struct {
	Limit            int
	Filters          []string
	Columns          []string
	DisableFunctions bool `mapstructure:"disable_functions"`
	Block            bool
}

// Insert struct contains access control values for insert operations
type Insert struct {
	Filters []string
	Columns []string
	Presets map[string]string
	Block   bool
}

// Insert struct contains access control values for update operations
type Update struct {
	Filters []string
	Columns []string
	Presets map[string]string
	Block   bool
}

type Upsert struct {
	Filters []string
	Columns []string
	Presets map[string]string
	Block   bool
}

// Delete struct contains access control values for delete operations
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

		gj.conf.SetResolver("redis", func(v ResolverProps) (Resolver, error) {
			return newRedis(v)
		})

		gj, err := core.NewGraphJin(conf, db)
		if err != nil {
			log.Fatal(err)
		}
	}
*/
type Resolver interface {
	Resolve(ResolverReq) ([]byte, error)
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
	StripPath string        `mapstructure:"strip_path"`
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

	var t *RoleTable
	for i := range r.Tables {
		if strings.EqualFold(r.Tables[i].Name, table) {
			t = &r.Tables[i]
			break
		}
	}
	if t == nil {
		nt := RoleTable{Name: table}
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

	for i, t := range tables {
		if strings.EqualFold(t.Name, table) {
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

func (c *Config) SetResolver(name string, fn refunc) error {
	if c.rtmap == nil {
		c.rtmap = make(map[string]refunc)
	}
	if _, ok := c.rtmap[name]; ok {
		return fmt.Errorf("resolver defined: %s", name)
	}
	c.rtmap[name] = fn
	return nil
}

// ReadInConfig reads in the config file for the environment specified in the GO_ENV
// environment variable. This is the best way to create a new GraphJin config.
func ReadInConfig(configFile string) (*Config, error) {
	return readInConfig(configFile, nil)
}

// ReadInConfigFS is the same as ReadInConfig but it also takes a filesytem as an argument
func ReadInConfigFS(configFile string, fs afero.Fs) (*Config, error) {
	return readInConfig(configFile, fs)
}

func readInConfig(configFile string, fs afero.Fs) (*Config, error) {
	cp := path.Dir(configFile)
	vi := newViper(cp, path.Base(configFile))

	if fs != nil {
		vi.SetFs(fs)
	}

	if err := vi.ReadInConfig(); err != nil {
		return nil, err
	}

	if pcf := vi.GetString("inherits"); pcf != "" {
		cf := vi.ConfigFileUsed()
		vi = newViper(cp, pcf)
		if fs != nil {
			vi.SetFs(fs)
		}

		if err := vi.ReadInConfig(); err != nil {
			return nil, err
		}

		if v := vi.GetString("inherits"); v != "" {
			return nil, fmt.Errorf("inherited config (%s) cannot itself inherit (%s)", pcf, v)
		}

		vi.SetConfigFile(cf)

		if err := vi.MergeInConfig(); err != nil {
			return nil, err
		}
	}

	c := &Config{
		ConfigPath: path.Dir(vi.ConfigFileUsed()),
	}

	if err := vi.Unmarshal(&c); err != nil {
		return nil, fmt.Errorf("failed to decode config, %v", err)
	}

	return c, nil
}

func newViper(configPath, configFile string) *viper.Viper {
	vi := viper.New()

	vi.SetEnvPrefix("GJ")
	vi.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	vi.AutomaticEnv()

	if filepath.Ext(configFile) != "" {
		vi.SetConfigFile(path.Join(configPath, configFile))
	} else {
		vi.SetConfigName(configFile)
		vi.AddConfigPath(configPath)
		vi.AddConfigPath("./config")
	}

	return vi
}
