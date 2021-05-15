package serv

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/dosco/graphjin/core"
	"github.com/dosco/graphjin/serv/internal/auth"
	"github.com/spf13/viper"
)

type Core = core.Config
type Auth = auth.Auth

// Config struct holds the GraphJin service config values
type Config struct {

	// Core holds config values for the GraphJin compiler
	Core `mapstructure:",squash"`

	// Serv holds config values for the GraphJin Service
	Serv `mapstructure:",squash"`

	closeFn  func()
	hostPort string
	vi       *viper.Viper
}

// Serv struct contains config values used by the GraphJin service
type Serv struct {
	// AppName is the name of your application used in log and debug messages
	AppName string `mapstructure:"app_name"`

	// Production when set to true runs the service with production level
	// security and other defaults. For example allow lists are enforced.
	Production bool

	// ConfigPath is the default path to find all configuration
	// files and scripts under
	ConfigPath string `mapstructure:"config_path"`

	// LogLevel can be debug, error, warn, info
	LogLevel string `mapstructure:"log_level"`

	// LogFormat can be json or simple
	LogFormat string `mapstructure:"log_format"`

	// HostPost to run the service on. Example localhost:8080
	HostPort string `mapstructure:"host_port"`

	// Host to run the service on
	Host string

	// Port to run the service on
	Port string

	// HTTPGZip enables HTTP compresssion
	HTTPGZip bool `mapstructure:"http_compress"`

	// Enable the web UI. Disabled in production
	WebUI bool `mapstructure:"web_ui"`

	// EnableTracing enables OpenTrace request tracing
	EnableTracing bool `mapstructure:"enable_tracing"`

	// WatchAndReload enables reloading the service on config changes
	WatchAndReload bool `mapstructure:"reload_on_config_change"`

	// AuthFailBlock when enabled blocks requests with a 401 on auth failure
	AuthFailBlock bool `mapstructure:"auth_fail_block"`

	// SeedFile is the path to the database seeding script
	SeedFile string `mapstructure:"seed_file"`

	// MigrationsPath is the path to the database migration files
	MigrationsPath string `mapstructure:"migrations_path"`

	// AllowedOrigins sets the HTTP CORS Access-Control-Allow-Origin header
	AllowedOrigins []string `mapstructure:"cors_allowed_origins"`

	// AllowedHeaders sets the HTTP CORS Access-Control-Allow-Headers header
	AllowedHeaders []string `mapstructure:"cors_allowed_headers"`

	// DebugCORS enables debug logs for cors
	DebugCORS bool `mapstructure:"cors_debug"`

	// APIPath change the suffix of the api path. Defaults to /v1/graphql
	APIPath string `mapstructure:"api_path"`

	// CacheControl sets the HTTP Cache-Control header
	CacheControl string `mapstructure:"cache_control"`

	// Telemetry struct contains OpenCensus metrics and tracing related config
	Telemetry struct {
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

			// Endpoint to send the data to. Example: http://zipkin:9411/api/v2/spans
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

	// Auth set the default auth used by the service
	Auth Auth

	// Auths sets multiple auths to be used by actions
	Auths []Auth

	// DB struct contains db config
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
		EnableTLS   bool          `mapstructure:"enable_tls"`
		ServerName  string        `mapstructure:"server_name"`
		ServerCert  string        `mapstructure:"server_cert"`
		ClientCert  string        `mapstructure:"client_cert"`
		ClientKey   string        `mapstructure:"client_key"`
	} `mapstructure:"database"`

	Actions []Action

	RateLimiter struct {
		Rate     float64
		Bucket   int
		IPHeader string `mapstructure:"ip_header"`
	} `mapstructure:"rate_limiter"`
}

// Action struct contains config values for a GraphJin service action
type Action struct {
	Name     string
	SQL      string
	AuthName string `mapstructure:"auth_name"`
}

// ReadInConfig function reads in the config file for the environment specified in the GO_ENV
// environment variable. This is the best way to create a new GraphJin config.
func ReadInConfig(configFile string) (*Config, error) {
	// migrate old sg var prefixes to new gj prefixes
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "SG_") {
			continue
		}
		v := strings.SplitN(e, "=", 2)
		os.Setenv(("GJ_" + v[0][3:]), v[1])
	}

	cpath := path.Dir(configFile)
	cfile := path.Base(configFile)
	vi := newViper(cpath, cfile)

	if err := vi.ReadInConfig(); err != nil {
		return nil, err
	}

	inherits := vi.GetString("inherits")

	if inherits != "" {
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

	c := &Config{vi: vi}
	c.Serv.ConfigPath = cpath
	c.Core.ConfigPath = cpath

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

	vi.AddConfigPath(configPath)
	vi.SetConfigName(configFile)
	vi.AddConfigPath("./config")

	vi.SetDefault("host_port", "0.0.0.0:8080")
	vi.SetDefault("web_ui", false)
	vi.SetDefault("enable_tracing", false)
	vi.SetDefault("auth_fail_block", false)
	vi.SetDefault("seed_file", "seed.js")

	vi.SetDefault("log_level", "info")
	vi.SetDefault("log_format", "json")

	vi.SetDefault("default_block", true)

	vi.SetDefault("database.type", "postgres")
	vi.SetDefault("database.host", "localhost")
	vi.SetDefault("database.port", 5432)
	vi.SetDefault("database.user", "postgres")
	vi.SetDefault("database.password", "")
	vi.SetDefault("database.schema", "public")

	vi.SetDefault("env", "development")

	vi.BindEnv("env", "GO_ENV") //nolint: errcheck
	vi.BindEnv("host", "HOST")  //nolint: errcheck
	vi.BindEnv("port", "PORT")  //nolint: errcheck

	vi.SetDefault("auth.rails.max_idle", 80)
	vi.SetDefault("auth.rails.max_active", 12000)
	vi.SetDefault("auth.creds_in_header", false)
	vi.SetDefault("auth.subs_creds_in_vars", false)

	return vi
}

func (c *Config) telemetryEnabled() bool {
	return c.Telemetry.Debug || c.Telemetry.Metrics.Exporter != "" || c.Telemetry.Tracing.Exporter != ""
}

func (c *Config) RelPath(p string) string {
	if filepath.IsAbs(p) {
		return p
	}

	return path.Join(c.Serv.ConfigPath, p)
}

func (c *Config) rateLimiterEnable() bool {
	return c.RateLimiter.Rate > 0 && c.RateLimiter.Bucket > 0
}
