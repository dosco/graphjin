package serv

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dosco/graphjin/auth/v3"
	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/serv/v3/internal/util"
	"github.com/spf13/afero"
	"github.com/spf13/viper"
)

type (
	Core      = core.Config
	Auth      = auth.Auth
	JWTConfig = auth.JWTConfig
)

// Configuration for the GraphJin service
type Config struct {
	// Configuration for the GraphJin compiler core
	Core `mapstructure:",squash" jsonschema:"title=Compiler Configuration"`

	// Configuration for the GraphJin Service
	Serv `mapstructure:",squash" jsonschema:"title=Service Configuration"`

	// Configuration for admin service
	// Admin `mapstructure:",squash" jsonschema:"title=Admin Configuration"`

	hostPort string
	hash     string
	name     string
	dirty    bool
	viper    *viper.Viper
}

// Configuration for admin service
// type Admin struct {
// 	// Enables the ability to hot-deploy a new configuration
// 	HotDeploy bool `mapstructure:"hot_deploy" jsonschema:"title=Enable Hot Deploy"`
//
// 	// Secret key used to control access to the admin api
// 	AdminSecretKey string `mapstructure:"admin_secret_key" jsonschema:"title=Admin API Secret Key"`
// }

// Configuration for the GraphJin Service
type Serv struct {
	// Application name is used in log and debug messages
	AppName string `mapstructure:"app_name" jsonschema:"title=Application Name"`

	// When enabled runs the service with production level security defaults.
	// For example allow lists are enforced.
	Production bool `jsonschema:"title=Production Mode,default=false"`

	// The default path to find all configuration files and scripts
	ConfigPath string `mapstructure:"config_path" jsonschema:"title=Config Path"`


	// Logging level must be one of debug, error, warn, info
	LogLevel string `mapstructure:"log_level" jsonschema:"title=Log Level,enum=debug,enum=error,enum=warn,enum=info"`

	// Logging Format: "auto" (default, colored console in dev, JSON in production),
	// "json" (always JSON), or "simple" (always colored console)
	LogFormat string `mapstructure:"log_format" jsonschema:"title=Logging Format,enum=auto,enum=json,enum=simple"`

	// The host and port the service runs on. Example localhost:8080
	HostPort string `mapstructure:"host_port" jsonschema:"title=Host and Port"`

	// Host to run the service on
	Host string `jsonschema:"title=Host"`

	// Port to run the service on
	Port string `jsonschema:"title=Port"`

	// Enables HTTP compression
	HTTPGZip bool `mapstructure:"http_compress" jsonschema:"title=Enable Compression,default=true"`

	// Sets the API rate limits
	RateLimiter RateLimiter `mapstructure:"rate_limiter" jsonschema:"title=Set API Rate Limiting"`

	// Enables the Server-Timing HTTP header
	ServerTiming bool `mapstructure:"server_timing" jsonschema:"title=Server Timing HTTP Header,default=true"`

	// Enable the web UI. Disabled in production
	WebUI bool `mapstructure:"web_ui" jsonschema:"title=Enable Web UI,default=false"`

	// Enable OpenTrace request tracing
	EnableTracing bool `mapstructure:"enable_tracing" jsonschema:"title=Enable Tracing,default=true"`

	// Enables reloading the service on config changes. Disabled in production
	WatchAndReload bool `mapstructure:"reload_on_config_change" jsonschema:"title=Reload Config"`

	// Enable blocking requests with a HTTP 401 on auth failure
	AuthFailBlock bool `mapstructure:"auth_fail_block" jsonschema:"title=Block Request on Authorization Failure"`

	// Sets the HTTP CORS Access-Control-Allow-Origin header
	AllowedOrigins []string `mapstructure:"cors_allowed_origins" jsonschema:"title=HTTP CORS Allowed Origins"`

	// Sets the HTTP CORS Access-Control-Allow-Headers header
	AllowedHeaders []string `mapstructure:"cors_allowed_headers" jsonschema:"title=HTTP CORS Allowed Headers"`

	// Enables debug logs for CORS
	DebugCORS bool `mapstructure:"cors_debug" jsonschema:"title=Log CORS"`

	// Sets the HTTP Cache-Control header
	CacheControl string `mapstructure:"cache_control" jsonschema:"title=Enable Cache-Control"`

	// Telemetry struct contains OpenCensus metrics and tracing related config
	// Telemetry Telemetry

	// Sets the default authentication used by the service
	Auth Auth `jsonschema:"title=Authentication"`

	// Database configuration
	DB Database `mapstructure:"database" jsonschema:"title=Database"`

	// MCP (Model Context Protocol) server configuration
	MCP MCPConfig `mapstructure:"mcp" jsonschema:"title=MCP Configuration"`

	// Redis configuration
	Redis RedisConfig `mapstructure:"redis" jsonschema:"title=Redis Configuration"`

	// Response caching configuration
	Caching CachingConfig `mapstructure:"caching" jsonschema:"title=Caching Configuration"`
}

// Database configuration
type Database struct {
	ConnString string `mapstructure:"connection_string" jsonschema:"title=Connection String"`
	Type       string `jsonschema:"title=Type,enum=postgres,enum=mysql,enum=mariadb,enum=mssql,enum=sqlite,enum=oracle,enum=mongodb"`
	Host       string `jsonschema:"title=Host"`
	Port       uint16 `jsonschema:"title=Port"`
	DBName     string `jsonschema:"title=Database Name"`
	User       string `jsonschema:"title=User"`
	Password   string `jsonschema:"title=Password"`
	Schema     string `jsonschema:"title=Postgres Schema"`

	// File path for SQLite databases
	Path       string `jsonschema:"title=File Path (SQLite)"`

	// Size of database connection pool
	PoolSize int `mapstructure:"pool_size" jsonschema:"title=Connection Pool Size"`

	// Max number of active database connections allowed
	MaxConnections int `mapstructure:"max_connections" jsonschema:"title=Maximum Connections"`

	// Max time after which idle database connections are closed
	MaxConnIdleTime time.Duration `mapstructure:"max_connection_idle_time" jsonschema:"title=Connection Idel Time"`

	// Max time after which database connections are not reused
	MaxConnLifeTime time.Duration `mapstructure:"max_connection_life_time" jsonschema:"title=Connection Life Time"`

	// Database ping timeout is used for db health checking
	PingTimeout time.Duration `mapstructure:"ping_timeout" jsonschema:"title=Healthcheck Ping Timeout"`

	// Set up an secure TLS encrypted database connection
	EnableTLS bool `mapstructure:"enable_tls" jsonschema:"title=Enable TLS"`

	// Required for TLS. For example with Google Cloud SQL it's
	// <gcp-project-id>:<cloud-sql-instance>
	ServerName string `mapstructure:"server_name" jsonschema:"title=TLS Server Name"`

	// Required for TLS. Can be a file path or the contents of the PEM file
	ServerCert string `mapstructure:"server_cert" jsonschema:"title=Server Certificate"`

	// Required for TLS. Can be a file path or the contents of the PEM file
	ClientCert string `mapstructure:"client_cert" jsonschema:"title=Client Certificate"`

	// Required for TLS. Can be a file path or the contents of the pem file
	ClientKey string `mapstructure:"client_key" jsonschema:"title=Client Key"`

	// MSSQL: disable TLS encryption (default: true in go-mssqldb)
	Encrypt *bool `mapstructure:"encrypt" jsonschema:"title=MSSQL Encrypt"`

	// MSSQL: trust server certificate without validation
	TrustServerCertificate *bool `mapstructure:"trust_server_certificate" jsonschema:"title=MSSQL Trust Server Certificate"`
}

// RateLimiter sets the API rate limits
type RateLimiter struct {
	// The number of events per second
	Rate float64 `jsonschema:"title=Connection Rate"`

	// Bucket a burst of at most 'bucket' number of events
	Bucket int `jsonschema:"title=Bucket Size"`

	// The header that contains the client ip
	IPHeader string `mapstructure:"ip_header" jsonschema:"title=IP From HTTP Header,example=X-Forwarded-For"`
}

// MCPConfig configures the Model Context Protocol (MCP) server
// MCP enables AI assistants to interact with GraphJin via function calling
//
// Transport is implicit based on context:
// - CLI (`graphjin mcp`) uses stdio transport for Claude Desktop and CLI clients
// - HTTP service uses SSE/HTTP transport at /api/v1/mcp endpoint
//
// Authentication:
// - HTTP transport uses the same auth as GraphQL/REST (JWT, headers, etc.)
// - Stdio transport uses env vars (GRAPHJIN_USER_ID, GRAPHJIN_USER_ROLE) or config values
type MCPConfig struct {
	// Disable the MCP server (MCP is enabled by default)
	Disable bool `jsonschema:"title=Disable MCP Server,default=false"`

	// Enable search functionality for saved queries and fragments
	EnableSearch bool `mapstructure:"enable_search" jsonschema:"title=Enable Query Search,default=true"`

	// Allow mutation operations via MCP
	AllowMutations bool `mapstructure:"allow_mutations" jsonschema:"title=Allow Mutations,default=true"`

	// Allow arbitrary GraphQL queries (vs only saved queries from allow-list)
	AllowRawQueries bool `mapstructure:"allow_raw_queries" jsonschema:"title=Allow Raw Queries,default=true"`

	// Default user ID for stdio transport (CLI). Can be overridden by GRAPHJIN_USER_ID env var.
	StdioUserID string `mapstructure:"stdio_user_id" jsonschema:"title=Stdio User ID"`

	// Default user role for stdio transport (CLI). Can be overridden by GRAPHJIN_USER_ROLE env var.
	StdioUserRole string `mapstructure:"stdio_user_role" jsonschema:"title=Stdio User Role"`

	// Run in MCP-only mode - disables GraphQL, REST, WebUI, and OpenAPI endpoints
	// Only health check and MCP endpoints will be available
	Only bool `mapstructure:"only" jsonschema:"title=MCP Only Mode,default=false"`

	// CursorCacheTTL in seconds for cursor ID cache (default: 1800 = 30 min)
	// Cursors are cached to allow short numeric IDs for LLM-friendly pagination
	CursorCacheTTL int `mapstructure:"cursor_cache_ttl" jsonschema:"title=Cursor Cache TTL,default=1800"`

	// CursorCacheSize max entries for in-memory cursor cache fallback (default: 10000)
	CursorCacheSize int `mapstructure:"cursor_cache_size" jsonschema:"title=Cursor Cache Size,default=10000"`

	// AllowConfigUpdates enables MCP tools that can modify GraphJin configuration
	// WARNING: Allows LLMs to change database connections, table configs, and roles
	// Only enable in trusted environments. Default: false
	AllowConfigUpdates bool `mapstructure:"allow_config_updates" jsonschema:"title=Allow Config Updates,default=false"`

	// AllowSchemaReload enables the MCP reload_schema tool for manual schema refresh
	// Useful when user adds tables and wants immediate discovery without waiting for poll
	// Default: false (auto-enabled in dev mode if not explicitly set)
	AllowSchemaReload bool `mapstructure:"allow_schema_reload" jsonschema:"title=Allow Schema Reload,default=false"`

	// AllowSchemaUpdates enables MCP tools to preview and apply database schema changes (DDL)
	// Uses db.graphql format. Auto-enabled in dev mode. Default: false
	AllowSchemaUpdates bool `mapstructure:"allow_schema_updates" jsonschema:"title=Allow Schema Updates,default=false"`

	// AllowDevTools enables advanced introspection MCP tools (explain_query, explore_relationships, audit_role_permissions).
	// These expose SQL, relationship graphs, and role permissions — useful for development/debugging.
	// Auto-enabled in dev mode. Default: false
	AllowDevTools bool `mapstructure:"allow_dev_tools" jsonschema:"title=Allow Dev Tools,default=false"`
}

// RedisConfig configures Redis connection
type RedisConfig struct {
	// Redis connection URL (e.g., redis://localhost:6379/0)
	URL string `mapstructure:"url" jsonschema:"title=Redis URL"`
}

// CachingConfig configures response caching
// Caching is enabled by default using in-memory LRU cache
// Configure redis.url to use Redis for distributed caching
// Responses are cached with automatic invalidation when mutations modify related tables
type CachingConfig struct {
	// Disable response caching (caching is enabled by default)
	Disable bool `mapstructure:"disable" jsonschema:"title=Disable Caching,default=false"`

	// Default TTL for cached responses in seconds (hard TTL)
	TTL int `mapstructure:"ttl" jsonschema:"title=Cache TTL,default=3600"`

	// Soft TTL for stale-while-revalidate in seconds (0 = disabled)
	FreshTTL int `mapstructure:"fresh_ttl" jsonschema:"title=Fresh TTL for SWR,default=300"`

	// Tables to exclude from caching
	ExcludeTables []string `mapstructure:"exclude_tables" jsonschema:"title=Exclude Tables"`
}

// Telemetry struct contains OpenCensus metrics and tracing related config
/*
type Telemetry struct {
	// Debug enables debug logging for metrics and tracing data.
	Debug bool

	// Interval to send out metrics and tracing data
	Interval *time.Duration

	Metrics struct {
		// Exporter is the name of the metrics exporter to use. Example: prometheus
		Exporter string

		// Endpoint to send the data to.
		Endpoint string

		// Namespace is set based on your exporter configration
		Namespace string

		// Key is set based on your exporter configuration
		Key string
	}

	Tracing struct {
		// Exporter is the name of the tracing exporter to use. Example: zipkin
		Exporter string

		// Endpoint to send the data to. Example: http://zipkin:9411/api/v3/spans
		Endpoint string

		// Sample sets how many requests to sample for tracing: Example: 0.6
		Sample string

		// IncludeQuery when set the GraphQL query is included in the tracing data
		IncludeQuery bool `mapstructure:"include_query"`

		// IncludeParams when set variables used with the query are included in the
		// tracing data
		IncludeParams bool `mapstructure:"include_params"`

		// ExcludeHealthCheck when set health check tracing is excluded from the
		// tracing data
		ExcludeHealthCheck bool `mapstructure:"exclude_health_check"`
	}
}
*/

// ReadInConfig function reads in the config file for the environment specified in the GO_ENV
// environment variable. This is the best way to create a new GraphJin config.
func ReadInConfig(configFile string) (*Config, error) {
	return readInConfig(configFile, nil)
}

// ReadInConfigFS is the same as ReadInConfig but it also takes a filesytem as an argument
func ReadInConfigFS(configFile string, fs afero.Fs) (*Config, error) {
	return readInConfig(configFile, fs)
}


// readInConfig function reads in the config file for the environment specified in the GO_ENV
func readInConfig(configFile string, fs afero.Fs) (*Config, error) {
	cp := filepath.Dir(configFile)
	viper := newViper(cp, filepath.Base(configFile))

	if fs != nil {
		viper.SetFs(fs)
	}

	if err := viper.ReadInConfig(); err != nil {
		return nil, err
	}

	if pcf := viper.GetString("inherits"); pcf != "" {
		cf := viper.ConfigFileUsed()
		viper = newViper(cp, pcf)
		if fs != nil {
			viper.SetFs(fs)
		}

		if err := viper.ReadInConfig(); err != nil {
			return nil, err
		}

		if value := viper.GetString("inherits"); value != "" {
			return nil, fmt.Errorf("inherited config '%s' cannot itself inherit '%s'", pcf, value)
		}

		viper.SetConfigFile(cf)

		if err := viper.MergeInConfig(); err != nil {
			return nil, err
		}
	}

	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "GJ_") || strings.HasPrefix(e, "SJ_") {
			kv := strings.SplitN(e, "=", 2)
			util.SetKeyValue(viper, kv[0], kv[1])
		}
	}

	config := &Config{viper: viper}
	config.ConfigPath = cp

	if err := viper.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to decode config, %v", err)
	}

	return config, nil
}

// NewConfig function creates a new GraphJin configuration from the provided config string
func NewConfig(config, format string) (*Config, error) {
	if format == "" {
		format = "yaml"
	}

	// migrate old sg var prefixes to new gj prefixes
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "SG_") {
			continue
		}
		v := strings.SplitN(e, "=", 2)
		if err := os.Setenv(("GJ_" + v[0][3:]), v[1]); err != nil {
			return nil, err
		}
	}

	viper := newViperWithDefaults()
	viper.SetConfigType(format)

	if err := viper.ReadConfig(strings.NewReader(config)); err != nil {
		return nil, err
	}

	c := &Config{viper: viper}

	if err := viper.Unmarshal(&c); err != nil {
		return nil, fmt.Errorf("failed to decode config, %v", err)
	}

	return c, nil
}

// newViperWithDefaults returns a new viper instance with the default settings
func newViperWithDefaults() *viper.Viper {
	vi := viper.New()

	vi.SetDefault("host_port", "0.0.0.0:8080")
	vi.SetDefault("web_ui", false)
	vi.SetDefault("enable_tracing", false)
	vi.SetDefault("auth_fail_block", false)
	vi.SetDefault("seed_file", "seed.js")

	vi.SetDefault("log_level", "info")
	vi.SetDefault("log_format", "auto")

	vi.SetDefault("default_block", true)

	vi.SetDefault("database.type", "postgres")
	vi.SetDefault("database.host", "localhost")
	vi.SetDefault("database.port", 5432)
	vi.SetDefault("database.user", "postgres")
	vi.SetDefault("database.password", "")
	vi.SetDefault("database.schema", "public")
	vi.SetDefault("database.pool_size", 10)

	vi.SetDefault("env", "development")

	vi.BindEnv("env", "GO_ENV") //nolint:errcheck
	vi.BindEnv("host", "HOST")  //nolint:errcheck
	vi.BindEnv("port", "PORT")  //nolint:errcheck

	vi.SetDefault("auth.subs_creds_in_vars", false)

	// MCP defaults (MCP enabled by default, use mcp.disable: true to turn off)
	vi.SetDefault("mcp.disable", false)
	vi.SetDefault("mcp.enable_search", true)
	vi.SetDefault("mcp.allow_mutations", true)
	vi.SetDefault("mcp.allow_raw_queries", true)
	vi.SetDefault("mcp.only", false)
	vi.SetDefault("mcp.cursor_cache_ttl", 1800)       // 30 minutes
	vi.SetDefault("mcp.cursor_cache_size", 10000)    // max in-memory entries
	vi.SetDefault("mcp.allow_config_updates", false) // disabled by default for security
	vi.SetDefault("mcp.allow_schema_reload", false)   // disabled by default, auto-enabled in dev mode
	vi.SetDefault("mcp.allow_schema_updates", false)  // disabled by default, auto-enabled in dev mode
	vi.SetDefault("mcp.allow_dev_tools", false)        // disabled by default, auto-enabled in dev mode

	// Caching defaults
	vi.SetDefault("caching.enable", false)
	vi.SetDefault("caching.ttl", 3600)
	vi.SetDefault("caching.fresh_ttl", 300)

	return vi
}

// newViper returns a new viper instance with the default settings
func newViper(configPath, configFile string) *viper.Viper {
	vi := newViperWithDefaults()
	vi.SetConfigName(strings.TrimSuffix(configFile, filepath.Ext(configFile)))

	if configPath == "" {
		vi.AddConfigPath("./config")
	} else {
		vi.AddConfigPath(configPath)
	}

	return vi
}


// func (c *Config) telemetryEnabled() bool {
// 	return c.Telemetry.Debug || c.Telemetry.Metrics.Exporter != "" || c.Telemetry.Tracing.Exporter != ""
// }

// AbsolutePath returns the absolute path of the file
func (c *Config) AbsolutePath(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(c.ConfigPath, p)
}

// SetHash sets the hash value of the configuration
func (c *Config) SetHash(hash string) {
	c.hash = hash
}

// SetName sets the name of the configuration
func (c *Config) SetName(name string) {
	c.name = name
}

// rateLimiterEnable returns true if the rate limiter is enabled
func (c *Config) rateLimiterEnable() bool {
	return c.RateLimiter.Rate > 0 && c.RateLimiter.Bucket > 0
}

// ShouldUseJSONLogs returns true if logs should be in JSON format.
// Returns true if log_format is "json" OR if log_format is "auto" and production mode is enabled.
// Returns false otherwise (colored console output for dev mode).
func (c *Config) ShouldUseJSONLogs() bool {
	if c.LogFormat == "json" {
		return true
	}
	if c.LogFormat == "auto" && c.Serv.Production {
		return true
	}
	return false
}

// GetConfigName returns the name of the configuration
func GetConfigName() string {
	goEnv := strings.TrimSpace(strings.ToLower(os.Getenv("GO_ENV")))

	switch goEnv {
	case "production", "prod":
		return "prod"

	case "staging", "stage":
		return "stage"

	case "testing", "test":
		return "test"

	case "development", "dev", "":
		return "dev"

	default:
		return goEnv
	}
}
