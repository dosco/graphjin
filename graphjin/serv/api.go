package serv

import (
	"database/sql"
	"fmt"
	"net/http"
	"time"

	"github.com/dosco/graphjin/core"
	"github.com/dosco/graphjin/internal/util"
	"github.com/dosco/graphjin/serv/internal/auth"
	"go.uber.org/zap"

	"github.com/spf13/viper"
)

type Core = core.Config

// Config struct holds the GraphJin config values
type Config struct {
	Core `mapstructure:",squash"`
	Serv `mapstructure:",squash"`

	closeFn  func()
	hostPort string
	cpath    string
	vi       *viper.Viper
}

// Serv struct contains config values used by the GraphJin service
type Serv struct {
	AppName        string `mapstructure:"app_name"`
	Production     bool
	LogLevel       string `mapstructure:"log_level"`
	LogFormat      string `mapstructure:"log_format"`
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
	AllowedHeaders []string `mapstructure:"cors_allowed_headers"`
	DebugCORS      bool     `mapstructure:"cors_debug"`
	APIPath        string   `mapstructure:"api_path"`
	CacheControl   string   `mapstructure:"cache_control"`

	// Telemetry struct contains OpenCensus metrics and tracing related config
	Telemetry struct {
		Debug    bool
		Interval *time.Duration
		Metrics  struct {
			Exporter  string
			Endpoint  string
			Namespace string
			Key       string
		}

		Tracing struct {
			Exporter      string
			Endpoint      string
			Sample        string
			IncludeQuery  bool `mapstructure:"include_query"`
			IncludeParams bool `mapstructure:"include_params"`
		}
	}

	Auth  auth.Auth
	Auths []auth.Auth

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

type Service struct {
	log      *zap.SugaredLogger // logger
	zlog     *zap.Logger        // faster logger
	logLevel int                // log level
	conf     *Config            // parsed config
	db       *sql.DB            // database connection pool
	gj       *core.GraphJin
}

func NewService(conf *Config) (*Service, error) {
	zlog := util.NewLogger(conf.LogFormat == "json")
	log := zlog.Sugar()

	if err := initConfig(conf, log); err != nil {
		return nil, err
	}

	s := &Service{conf: conf, log: log, zlog: zlog}
	initLogLevel(s)
	validateConf(s)

	if s.conf != nil && s.conf.WatchAndReload {
		initWatcher(s)
	}

	return s, nil
}

func (s *Service) init() error {
	var db *sql.DB
	var err error

	if db, err = s.newDB(true, true); err != nil {
		return fmt.Errorf("Failed to connect to database: %w", err)
	}

	s.gj, err = core.NewGraphJin(&s.conf.Core, db)
	if err != nil {
		return fmt.Errorf("Failed to initialize: %w", err)
	}

	return nil
}

func (s *Service) Start() error {
	if err := s.init(); err != nil {
		return err
	}
	startHTTP(s)
	return nil
}

func (s *Service) Attach(mux *http.ServeMux) error {
	if err := s.init(); err != nil {
		return err
	}
	_, err := routeHandler(s, mux)
	return err
}
