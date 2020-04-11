package core

import (
	"fmt"
	"path"
	"strings"

	"github.com/spf13/viper"
)

// Core struct contains core specific config value
type Config struct {
	SecretKey     string            `mapstructure:"secret_key"`
	UseAllowList  bool              `mapstructure:"use_allow_list"`
	AllowListFile string            `mapstructure:"allow_list_file"`
	SetUserID     bool              `mapstructure:"set_user_id"`
	Vars          map[string]string `mapstructure:"variables"`
	Blocklist     []string
	Tables        []Table
	RolesQuery    string `mapstructure:"roles_query"`
	Roles         []Role
	Inflections   map[string]string
}

// Table struct defines a database table
type Table struct {
	Name      string
	Table     string
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
	Name string

	Query  Query
	Insert Insert
	Update Update
	Delete Delete
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

// ReadInConfig function reads in the config file for the environment specified in the GO_ENV
// environment variable. This is the best way to create a new Super Graph config.
func ReadInConfig(configFile string) (*Config, error) {
	cpath := path.Dir(configFile)
	cfile := path.Base(configFile)
	vi := newViper(cpath, cfile)

	if err := vi.ReadInConfig(); err != nil {
		return nil, err
	}

	inherits := vi.GetString("inherits")

	if len(inherits) != 0 {
		vi = newViper(cpath, inherits)

		if err := vi.ReadInConfig(); err != nil {
			return nil, err
		}

		if vi.IsSet("inherits") {
			return nil, fmt.Errorf("inherited config (%s) cannot itself inherit (%s)",
				inherits,
				vi.GetString("inherits"))
		}

		vi.SetConfigName(cfile)

		if err := vi.MergeInConfig(); err != nil {
			return nil, err
		}
	}

	c := &Config{}

	if err := vi.Unmarshal(&c); err != nil {
		return nil, fmt.Errorf("failed to decode config, %v", err)
	}

	if len(c.AllowListFile) == 0 {
		c.AllowListFile = path.Join(cpath, "allow.list")
	}

	return c, nil
}

func newViper(configPath, configFile string) *viper.Viper {
	vi := viper.New()

	vi.SetEnvPrefix("SG")
	vi.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	vi.AutomaticEnv()

	vi.SetConfigName(configFile)
	vi.AddConfigPath(configPath)
	vi.AddConfigPath("./config")

	return vi
}
