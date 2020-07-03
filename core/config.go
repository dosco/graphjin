package core

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Core struct contains core specific config value
type Config struct {
	// SecretKey is used to encrypt opaque values such as
	// the cursor. Auto-generated if not set
	SecretKey string `mapstructure:"secret_key"`

	// UseAllowList (aka production mode) when set to true ensures
	// only queries lists in the allow.list file can be used. All
	// queries are pre-prepared so no compiling happens and things are
	// very fast.
	UseAllowList bool `mapstructure:"use_allow_list"`

	// AllowListFile if the path to allow list file if not set the
	// path is assumed to be the same as the config path (allow.list)
	AllowListFile string `mapstructure:"allow_list_file"`

	// SetUserID forces the database session variable `user.id` to
	// be set to the user id. This variables can be used by triggers
	// or other database functions
	SetUserID bool `mapstructure:"set_user_id"`

	// DefaultBlock ensures that in anonymous mode (role 'anon') all tables
	// are blocked from queries and mutations. To open access to tables in
	// anonymous mode they have to be added to the 'anon' role config.
	DefaultBlock bool `mapstructure:"default_block"`

	// Vars is a map of hardcoded variables that can be leveraged in your
	// queries (eg. variable admin_id will be $admin_id in the query)
	Vars map[string]string `mapstructure:"variables"`

	// Blocklist is a list of tables and columns that should be filtered
	// out from any and all queries
	Blocklist []string

	// Tables contains all table specific configuration such as aliased tables
	// creating relationships between tables, etc
	Tables []Table

	// RolesQuery if set enabled attributed based access control. This query
	// is used to fetch the user attributes that then dynamically define the users
	// role.
	RolesQuery string `mapstructure:"roles_query"`

	// Roles contains all the configuration for all the roles you want to support
	// `user` and `anon` are two default roles. User role is for when a user ID is
	// available and Anon when it's not.
	//
	// If you're using the RolesQuery config to enable atribute based acess control then
	// you can add more custom roles.
	Roles []Role

	// Inflections is to add additionally singular to plural mappings
	// to the engine (eg. sheep: sheep)
	Inflections map[string]string `mapstructure:"inflections"`

	// Database schema name. Defaults to 'public'
	DBSchema string `mapstructure:"db_schema"`

	// Log warnings and other debug information
	Debug bool

	// Useful for quickly debugging. Please set to false in production
	CredsInVars bool `mapstructure:"creds_in_vars"`

	// Subscriptions poll the database to query for updates
	// this sets the duration (in seconds) between requests.
	// Defaults to 5 seconds
	PollDuration time.Duration `mapstructure:"poll_every_seconds"`
}

// Table struct defines a database table
type Table struct {
	Name      string
	Table     string
	Type      string
	Blocklist []string
	Remotes   []Remote
	Columns   []Column
}

// Column struct defines a database column
type Column struct {
	Name       string
	Type       string
	ForeignKey string `mapstructure:"related_to"`
}

// Remote struct defines a remote API endpoint
type Remote struct {
	Name        string
	ID          string
	Path        string
	URL         string
	Debug       bool
	PassHeaders []string `mapstructure:"pass_headers"`
	SetHeaders  []struct {
		Name  string
		Value string
	} `mapstructure:"set_headers"`
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
	ReadOnly bool `mapstructure:"read_only"`

	Query  *Query
	Insert *Insert
	Update *Update
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

// Delete struct contains access control values for delete operations
type Delete struct {
	Filters []string
	Columns []string
	Block   bool
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
		r = &nr
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
		t = &nt
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

// ReadInConfig function reads in the config file for the environment specified in the GO_ENV
// environment variable. This is the best way to create a new Super Graph config.
func ReadInConfig(configFile string) (*Config, error) {
	cp := path.Dir(configFile)
	vi := newViper(cp, path.Base(configFile))

	if err := vi.ReadInConfig(); err != nil {
		return nil, err
	}

	if pcf := vi.GetString("inherits"); pcf != "" {
		cf := vi.ConfigFileUsed()
		vi = newViper(cp, pcf)

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

	c := &Config{}

	if err := vi.Unmarshal(&c); err != nil {
		return nil, fmt.Errorf("failed to decode config, %v", err)
	}

	if c.AllowListFile == "" {
		c.AllowListFile = path.Join(cp, "allow.list")
	}

	return c, nil
}

func newViper(configPath, configFile string) *viper.Viper {
	vi := viper.New()

	vi.SetEnvPrefix("SG")
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
