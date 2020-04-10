// Package config provides the config values needed for Super Graph
// For detailed documentation visit https://supergraph.dev
package config

import (
	"fmt"
	"log"
	"os"
	"path"
	"strings"
	"time"

	"github.com/gobuffalo/flect"
	"github.com/spf13/viper"
)

const (
	LogLevelNone int = iota
	LogLevelInfo
	LogLevelWarn
	LogLevelError
	LogLevelDebug
)

// Config struct holds the Super Graph config values
type Config struct {
	Core `mapstructure:",squash"`
	Serv `mapstructure:",squash"`

	vi          *viper.Viper
	log         *log.Logger
	logLevel    int
	roles       map[string]*Role
	abacEnabled bool
	valid       bool
}

// Core struct contains core specific config value
type Core struct {
	Env        string
	Production bool
	LogLevel   string            `mapstructure:"log_level"`
	SecretKey  string            `mapstructure:"secret_key"`
	SetUserID  bool              `mapstructure:"set_user_id"`
	Vars       map[string]string `mapstructure:"variables"`
	Blocklist  []string
	Tables     []Table
	RolesQuery string `mapstructure:"roles_query"`
	Roles      []Role
}

// Serv struct contains config values used by the Super Graph service
type Serv struct {
	AppName        string `mapstructure:"app_name"`
	HostPort       string `mapstructure:"host_port"`
	Host           string
	Port           string
	HTTPGZip       bool     `mapstructure:"http_compress"`
	WebUI          bool     `mapstructure:"web_ui"`
	EnableTracing  bool     `mapstructure:"enable_tracing"`
	UseAllowList   bool     `mapstructure:"use_allow_list"`
	WatchAndReload bool     `mapstructure:"reload_on_config_change"`
	AuthFailBlock  bool     `mapstructure:"auth_fail_block"`
	SeedFile       string   `mapstructure:"seed_file"`
	MigrationsPath string   `mapstructure:"migrations_path"`
	AllowedOrigins []string `mapstructure:"cors_allowed_origins"`
	DebugCORS      bool     `mapstructure:"cors_debug"`

	Inflections map[string]string

	Auth  Auth
	Auths []Auth

	DB struct {
		Type        string
		Host        string
		Port        uint16
		DBName      string
		User        string
		Password    string
		Schema      string
		PoolSize    int32         `mapstructure:"pool_size"`
		MaxRetries  int           `mapstructure:"max_retries"`
		PingTimeout time.Duration `mapstructure:"ping_timeout"`
	} `mapstructure:"database"`

	Actions []Action
}

// Auth struct contains authentication related config values used by the Super Graph service
type Auth struct {
	Name          string
	Type          string
	Cookie        string
	CredsInHeader bool `mapstructure:"creds_in_header"`

	Rails struct {
		Version       string
		SecretKeyBase string `mapstructure:"secret_key_base"`
		URL           string
		Password      string
		MaxIdle       int `mapstructure:"max_idle"`
		MaxActive     int `mapstructure:"max_active"`
		Salt          string
		SignSalt      string `mapstructure:"sign_salt"`
		AuthSalt      string `mapstructure:"auth_salt"`
	}

	JWT struct {
		Provider   string
		Secret     string
		PubKeyFile string `mapstructure:"public_key_file"`
		PubKeyType string `mapstructure:"public_key_type"`
	}

	Header struct {
		Name   string
		Value  string
		Exists bool
	}
}

// Column struct defines a database column
type Column struct {
	Name       string
	Type       string
	ForeignKey string `mapstructure:"related_to"`
}

// Table struct defines a database table
type Table struct {
	Name      string
	Table     string
	Blocklist []string
	Remotes   []Remote
	Columns   []Column
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

// RoleTable struct contains role specific access control values for a database table
type RoleTable struct {
	Name string

	Query  Query
	Insert Insert
	Update Update
	Delete Delete
}

// Role struct contains role specific access control values for for all database tables
type Role struct {
	Name      string
	Match     string
	Tables    []RoleTable
	tablesMap map[string]*RoleTable
}

// Action struct contains config values for a Super Graph service action
type Action struct {
	Name     string
	SQL      string
	AuthName string `mapstructure:"auth_name"`
}

// NewConfig function reads in the config file for the environment specified in the GO_ENV
// environment variable. This is the best way to create a new Super Graph config.
func NewConfig(path string) (*Config, error) {
	return NewConfigWithLogger(path, log.New(os.Stdout, "", 0))
}

// NewConfigWithLogger function reads in the config file for the environment specified in the GO_ENV
// environment variable. This is the best way to create a new Super Graph config.
func NewConfigWithLogger(path string, logger *log.Logger) (*Config, error) {
	vi := newViper(path, GetConfigName())

	if err := vi.ReadInConfig(); err != nil {
		return nil, err
	}

	inherits := vi.GetString("inherits")

	if len(inherits) != 0 {
		vi = newViper(path, inherits)

		if err := vi.ReadInConfig(); err != nil {
			return nil, err
		}

		if vi.IsSet("inherits") {
			return nil, fmt.Errorf("inherited config (%s) cannot itself inherit (%s)",
				inherits,
				vi.GetString("inherits"))
		}

		vi.SetConfigName(GetConfigName())

		if err := vi.MergeInConfig(); err != nil {
			return nil, err
		}
	}

	c := &Config{log: logger, vi: vi}

	if err := vi.Unmarshal(&c); err != nil {
		return nil, fmt.Errorf("failed to decode config, %v", err)
	}

	if err := c.init(); err != nil {
		return nil, fmt.Errorf("failed to initialize config: %w", err)
	}

	return c, nil
}

// NewConfigFrom function initializes a Config struct that you manually created
// so it can be used by Super Graph
func NewConfigFrom(c *Config, configPath string, logger *log.Logger) (*Config, error) {
	c.vi = newViper(configPath, GetConfigName())
	c.log = logger
	if err := c.init(); err != nil {
		return nil, err
	}
	return c, nil
}

func newViper(configPath, filename string) *viper.Viper {
	vi := viper.New()

	vi.SetEnvPrefix("SG")
	vi.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	vi.AutomaticEnv()

	vi.SetConfigName(filename)
	vi.AddConfigPath(configPath)
	vi.AddConfigPath("./config")

	vi.SetDefault("host_port", "0.0.0.0:8080")
	vi.SetDefault("web_ui", false)
	vi.SetDefault("enable_tracing", false)
	vi.SetDefault("auth_fail_block", "always")
	vi.SetDefault("seed_file", "seed.js")

	vi.SetDefault("database.type", "postgres")
	vi.SetDefault("database.host", "localhost")
	vi.SetDefault("database.port", 5432)
	vi.SetDefault("database.user", "postgres")
	vi.SetDefault("database.schema", "public")

	vi.SetDefault("env", "development")

	vi.BindEnv("env", "GO_ENV") //nolint: errcheck
	vi.BindEnv("host", "HOST")  //nolint: errcheck
	vi.BindEnv("port", "PORT")  //nolint: errcheck

	vi.SetDefault("auth.rails.max_idle", 80)
	vi.SetDefault("auth.rails.max_active", 12000)

	return vi
}

func (c *Config) init() error {
	switch c.Core.LogLevel {
	case "debug":
		c.logLevel = LogLevelDebug
	case "error":
		c.logLevel = LogLevelError
	case "warn":
		c.logLevel = LogLevelWarn
	case "info":
		c.logLevel = LogLevelInfo
	default:
		c.logLevel = LogLevelNone
	}

	if c.UseAllowList {
		c.Production = true
	}

	for k, v := range c.Inflections {
		flect.AddPlural(k, v)
	}

	// Tables: Validate and sanitize
	tm := make(map[string]struct{})

	for i := 0; i < len(c.Tables); i++ {
		t := &c.Tables[i]
		t.Name = flect.Pluralize(strings.ToLower(t.Name))

		if _, ok := tm[t.Name]; ok {
			c.Tables = append(c.Tables[:i], c.Tables[i+1:]...)
			c.log.Printf("WRN duplicate table found: %s", t.Name)
		}
		tm[t.Name] = struct{}{}

		t.Table = flect.Pluralize(strings.ToLower(t.Table))
	}

	// Variables: Validate and sanitize
	for k, v := range c.Vars {
		c.Vars[k] = sanitize(v)
	}

	// Roles: validate and sanitize
	c.RolesQuery = sanitize(c.RolesQuery)
	c.roles = make(map[string]*Role)

	for i := 0; i < len(c.Roles); i++ {
		r := &c.Roles[i]
		r.Name = strings.ToLower(r.Name)

		if _, ok := c.roles[r.Name]; ok {
			c.Roles = append(c.Roles[:i], c.Roles[i+1:]...)
			c.log.Printf("WRN duplicate role found: %s", r.Name)
		}

		r.Match = sanitize(r.Match)
		r.tablesMap = make(map[string]*RoleTable)

		for n, table := range r.Tables {
			r.tablesMap[table.Name] = &r.Tables[n]
		}

		c.roles[r.Name] = r
	}

	if _, ok := c.roles["user"]; !ok {
		u := Role{Name: "user"}
		c.Roles = append(c.Roles, u)
		c.roles["user"] = &u
	}

	if _, ok := c.roles["anon"]; !ok {
		c.log.Printf("WRN unauthenticated requests will be blocked. no role 'anon' defined")
		c.AuthFailBlock = true
	}

	if len(c.RolesQuery) == 0 {
		c.log.Printf("WRN roles_query not defined: attribute based access control disabled")
	}

	if len(c.RolesQuery) == 0 {
		c.abacEnabled = false
	} else {
		switch len(c.Roles) {
		case 0, 1:
			c.abacEnabled = false
		case 2:
			_, ok1 := c.roles["anon"]
			_, ok2 := c.roles["user"]
			c.abacEnabled = !(ok1 && ok2)
		default:
			c.abacEnabled = true
		}
	}

	// Auths: validate and sanitize
	am := make(map[string]struct{})

	for i := 0; i < len(c.Auths); i++ {
		a := &c.Auths[i]
		a.Name = strings.ToLower(a.Name)

		if _, ok := am[a.Name]; ok {
			c.Auths = append(c.Auths[:i], c.Auths[i+1:]...)
			c.log.Printf("WRN duplicate auth found: %s", a.Name)
		}
		am[a.Name] = struct{}{}
	}

	// Actions: validate and sanitize
	axm := make(map[string]struct{})

	for i := 0; i < len(c.Actions); i++ {
		a := &c.Actions[i]
		a.Name = strings.ToLower(a.Name)
		a.AuthName = strings.ToLower(a.AuthName)

		if _, ok := axm[a.Name]; ok {
			c.Actions = append(c.Actions[:i], c.Actions[i+1:]...)
			c.log.Printf("WRN duplicate action found: %s", a.Name)
		}

		if _, ok := am[a.AuthName]; !ok {
			c.Actions = append(c.Actions[:i], c.Actions[i+1:]...)
			c.log.Printf("WRN invalid auth_name '%s' for auth: %s", a.AuthName, a.Name)
		}
		axm[a.Name] = struct{}{}
	}

	c.valid = true

	return nil
}

// GetDBTableAliases function returns a map with database tables as keys
// and a list of aliases as values
func (c *Config) GetDBTableAliases() map[string][]string {
	m := make(map[string][]string, len(c.Tables))

	for i := range c.Tables {
		t := c.Tables[i]

		if len(t.Table) == 0 || len(t.Columns) != 0 {
			continue
		}

		m[t.Table] = append(m[t.Table], t.Name)
	}
	return m
}

// IsABACEnabled function returns true if attribute based access control is enabled
func (c *Config) IsABACEnabled() bool {
	return c.abacEnabled
}

// IsAnonRoleDefined function returns true if the config has configuration for the `anon` role
func (c *Config) IsAnonRoleDefined() bool {
	_, ok := c.roles["anon"]
	return ok
}

// GetRole function returns returns the Role struct by name
func (c *Config) GetRole(name string) *Role {
	role := c.roles[name]
	return role
}

// ConfigPathUsed function returns the path to the current config file (excluding filename)
func (c *Config) ConfigPathUsed() string {
	return path.Dir(c.vi.ConfigFileUsed())
}

// WriteConfigAs function writes the config to a file
// Format defined by extension (eg: .yml, .json)
func (c *Config) WriteConfigAs(fname string) error {
	return c.vi.WriteConfigAs(fname)
}

// Log function returns the logger
func (c *Config) Log() *log.Logger {
	return c.log
}

// LogLevel function returns the log level
func (c *Config) LogLevel() int {
	return c.logLevel
}

// IsValid function returns true if the Config struct is initialized and valid
func (c *Config) IsValid() bool {
	return c.valid
}

// GetTable function returns the RoleTable struct for a Role by table name
func (r *Role) GetTable(name string) *RoleTable {
	table := r.tablesMap[name]
	return table
}
