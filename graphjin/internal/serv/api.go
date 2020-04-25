package serv

import (
	"time"

	"github.com/dosco/super-graph/core"
	"github.com/dosco/super-graph/internal/serv/internal/auth"

	"github.com/spf13/viper"
)

const (
	LogLevelNone int = iota
	LogLevelInfo
	LogLevelWarn
	LogLevelError
	LogLevelDebug
)

type Core = core.Config

// Config struct holds the Super Graph config values
type Config struct {
	Core `mapstructure:",squash"`
	Serv `mapstructure:",squash"`

	cpath string
	vi    *viper.Viper
}

// Serv struct contains config values used by the Super Graph service
type Serv struct {
	AppName        string `mapstructure:"app_name"`
	Production     bool
	LogLevel       string `mapstructure:"log_level"`
	HostPort       string `mapstructure:"host_port"`
	Host           string
	Port           string
	HTTPGZip       bool     `mapstructure:"http_compress"`
	WebUI          bool     `mapstructure:"web_ui"`
	EnableTracing  bool     `mapstructure:"enable_tracing"`
	WatchAndReload bool     `mapstructure:"reload_on_config_change"`
	AuthFailBlock  bool     `mapstructure:"auth_fail_block"`
	SeedFile       string   `mapstructure:"seed_file"`
	MigrationsPath string   `mapstructure:"migrations_path"`
	AllowedOrigins []string `mapstructure:"cors_allowed_origins"`
	DebugCORS      bool     `mapstructure:"cors_debug"`
	APIPath        string   `mapstructure:"api_path"`
	CacheControl   string   `mapstructure:"cache_control"`

	Auth  auth.Auth
	Auths []auth.Auth

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

// Action struct contains config values for a Super Graph service action
type Action struct {
	Name     string
	SQL      string
	AuthName string `mapstructure:"auth_name"`
}
