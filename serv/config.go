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
	secrets map[string]string

	// Configuration for the GraphJin compiler core
	Core `mapstructure:",squash" jsonschema:"title=Compiler Configuration"`

	// Configuration for the GraphJin Service
	Serv `mapstructure:",squash" jsonschema:"title=Service Configuration"`

	// Configuration for admin service
	Admin `mapstructure:",squash" jsonschema:"title=Admin Configuration"`

	hostPort string
	hash     string
	name     string
	dirty    bool
	vi       *viper.Viper
}

// Configuration for admin service
type Admin struct {
	// Enables the ability to hot-deploy a new configuration
	HotDeploy bool `mapstructure:"hot_deploy" jsonschema:"title=Enable Hot Deploy"`

	// Secret key used to control access to the admin api
	AdminSecretKey string `mapstructure:"admin_secret_key" jsonschema:"title=Admin API Secret Key"`
}

// Configuration for the GraphJin Service
type Serv struct {
	// Application name is used in log and debug messages
	AppName string `mapstructure:"app_name" jsonschema:"title=Application Name"`

	// When enabled runs the service with production level security defaults.
	// For example allow lists are enforced.
	Production bool `jsonschema:"title=Production Mode,default=false"`

	// The default path to find all configuration files and scripts
	ConfigPath string `mapstructure:"config_path" jsonschema:"title=Config Path"`

	// The file for the secret key store. This must be a Mozilla SOPS file
	SecretsFile string `mapstructure:"secrets_file" jsonschema:"title=Secrets File"`

	// Logging level must be one of debug, error, warn, info
	LogLevel string `mapstructure:"log_level" jsonschema:"title=Log Level,enum=debug,enum=error,enum=warn,enum=info"`

	// Logging Format must me either json or simple
	LogFormat string `mapstructure:"log_format" jsonschema:"title=Logging Level,enum=json,enum=simple"`

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

	// This is the path to the database migration files
	MigrationsPath string `mapstructure:"migrations_path" jsonschema:"title=Migrations Path"`

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
}

// Database configuration
type Database struct {
	ConnString string `mapstructure:"connection_string" jsonschema:"title=Connection String"`
	Type       string `jsonschema:"title=Type,enum=postgres,enum=mysql"`
	Host       string `jsonschema:"title=Host"`
	Port       uint16 `jsonschema:"title=Port"`
	DBName     string `jsonschema:"title=Database Name"`
	User       string `jsonschema:"title=User"`
	Password   string `jsonschema:"title=Password"`
	Schema     string `jsonschema:"title=Postgres Schema"`

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
	c, err := readInConfig(configFile, nil)
	if err != nil {
		return nil, err
	}
	return setupSecrets(c, nil)
}

// ReadInConfigFS is the same as ReadInConfig but it also takes a filesytem as an argument
func ReadInConfigFS(configFile string, fs afero.Fs) (*Config, error) {
	c, err := readInConfig(configFile, fs)
	if err != nil {
		return nil, err
	}
	c1, err := setupSecrets(c, fs)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, c.SecretsFile)
	}
	return c1, err
}

func setupSecrets(conf *Config, fs afero.Fs) (*Config, error) {
	if conf.SecretsFile == "" {
		return conf, nil
	}

	secFile, err := filepath.Abs(conf.RelPath(conf.SecretsFile))
	if err != nil {
		return nil, err
	}

	var newConf Config

	newConf.secrets, err = initSecrets(secFile, fs)
	if err != nil {
		return nil, err
	}

	for k, v := range newConf.secrets {
		util.SetKeyValue(conf.vi, k, v)
	}

	if len(newConf.secrets) == 0 {
		return conf, nil
	}

	if err := conf.vi.Unmarshal(&newConf); err != nil {
		return nil, fmt.Errorf("failed to decode config, %v", err)
	}

	// c := conf.vi.AllSettings()
	// bs, err := yaml.Marshal(c)
	// if err != nil {
	// 	return nil, err
	// }
	// fmt.Println(">", string(bs))

	return &newConf, nil
}

func readInConfig(configFile string, fs afero.Fs) (*Config, error) {
	cp := filepath.Dir(configFile)
	vi := newViper(cp, filepath.Base(configFile))

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
			return nil, fmt.Errorf("inherited config '%s' cannot itself inherit '%s'", pcf, v)
		}

		vi.SetConfigFile(cf)

		if err := vi.MergeInConfig(); err != nil {
			return nil, err
		}
	}

	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "GJ_") || strings.HasPrefix(e, "SJ_") {
			kv := strings.SplitN(e, "=", 2)
			util.SetKeyValue(vi, kv[0], kv[1])
		}
	}

	c := &Config{vi: vi}
	c.Serv.ConfigPath = cp

	if err := vi.Unmarshal(&c); err != nil {
		return nil, fmt.Errorf("failed to decode config, %v", err)
	}

	return c, nil
}

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

	vi := newViperWithDefaults()
	vi.SetConfigType(format)

	if err := vi.ReadConfig(strings.NewReader(config)); err != nil {
		return nil, err
	}

	c := &Config{vi: vi}

	if err := vi.Unmarshal(&c); err != nil {
		return nil, fmt.Errorf("failed to decode config, %v", err)
	}

	return c, nil
}

func newViperWithDefaults() *viper.Viper {
	vi := viper.New()

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
	vi.SetDefault("database.pool_size", 10)

	vi.SetDefault("env", "development")

	vi.BindEnv("env", "GO_ENV") //nolint:errcheck
	vi.BindEnv("host", "HOST")  //nolint:errcheck
	vi.BindEnv("port", "PORT")  //nolint:errcheck

	vi.SetDefault("auth.rails.max_idle", 80)
	vi.SetDefault("auth.rails.max_active", 12000)
	vi.SetDefault("auth.subs_creds_in_vars", false)

	return vi
}

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

func (c *Config) GetSecret(k string) (string, bool) {
	v, ok := c.secrets[k]
	return v, ok
}

func (c *Config) GetSecretOrEnv(k string) string {
	if v, ok := c.GetSecret(k); ok {
		return v
	}
	return os.Getenv(k)
}

// func (c *Config) telemetryEnabled() bool {
// 	return c.Telemetry.Debug || c.Telemetry.Metrics.Exporter != "" || c.Telemetry.Tracing.Exporter != ""
// }

func (c *Config) RelPath(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(c.Serv.ConfigPath, p)
}

func (c *Config) SetHash(hash string) {
	c.hash = hash
}

func (c *Config) SetName(name string) {
	c.name = name
}

func (c *Config) rateLimiterEnable() bool {
	return c.RateLimiter.Rate > 0 && c.RateLimiter.Bucket > 0
}

func GetConfigName() string {
	ge := strings.TrimSpace(strings.ToLower(os.Getenv("GO_ENV")))

	switch ge {
	case "production", "prod":
		return "prod"

	case "staging", "stage":
		return "stage"

	case "testing", "test":
		return "test"

	case "development", "dev", "":
		return "dev"

	default:
		return ge
	}
}
