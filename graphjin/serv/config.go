package serv

import (
	"strings"

	"github.com/spf13/viper"
)

type config struct {
	AppName        string `mapstructure:"app_name"`
	Env            string
	HostPort       string `mapstructure:"host_port"`
	Host           string
	Port           string
	WebUI          bool   `mapstructure:"web_ui"`
	LogLevel       string `mapstructure:"log_level"`
	EnableTracing  bool   `mapstructure:"enable_tracing"`
	UseAllowList   bool   `mapstructure:"use_allow_list"`
	WatchAndReload bool   `mapstructure:"reload_on_config_change"`
	AuthFailBlock  string `mapstructure:"auth_fail_block"`
	SeedFile       string `mapstructure:"seed_file"`
	MigrationsPath string `mapstructure:"migrations_path"`

	Inflections map[string]string

	Auth struct {
		Type   string
		Cookie string
		Header string

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
	}

	DB struct {
		Type       string
		Host       string
		Port       uint16
		DBName     string
		User       string
		Password   string
		Schema     string
		PoolSize   int32  `mapstructure:"pool_size"`
		MaxRetries int    `mapstructure:"max_retries"`
		LogLevel   string `mapstructure:"log_level"`

		vars map[string][]byte `mapstructure:"variables"`

		Defaults struct {
			Filter    []string
			Blocklist []string
		}

		Tables []configTable
	} `mapstructure:"database"`

	Tables []configTable
	Roles  []configRoles
}

type configTable struct {
	Name      string
	Table     string
	Blocklist []string
	Remotes   []configRemote
}

type configRemote struct {
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

type configRoles struct {
	Name   string
	Tables []struct {
		Name string

		Query struct {
			Limit              int
			Filter             []string
			Columns            []string
			DisableAggregation bool `mapstructure:"disable_aggregation"`
			Deny               bool
		}

		Insert struct {
			Filter  []string
			Columns []string
			Set     map[string]string
			Deny    bool
		}

		Update struct {
			Filter  []string
			Columns []string
			Set     map[string]string
			Deny    bool
		}

		Delete struct {
			Filter  []string
			Columns []string
			Deny    bool
		}
	}
}

func newConfig() *viper.Viper {
	vi := viper.New()

	vi.SetEnvPrefix("SG")
	vi.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	vi.AutomaticEnv()

	vi.AddConfigPath(confPath)
	vi.AddConfigPath("./config")
	vi.SetConfigName(getConfigName())

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
	vi.BindEnv("env", "GO_ENV")
	vi.BindEnv("HOST", "HOST")
	vi.BindEnv("PORT", "PORT")

	vi.SetDefault("auth.rails.max_idle", 80)
	vi.SetDefault("auth.rails.max_active", 12000)

	return vi
}

func (c *config) getVariables() map[string]string {
	vars := make(map[string]string, len(c.DB.vars))

	for k, v := range c.DB.vars {
		isVar := false

		for i := range v {
			if v[i] == '$' {
				isVar = true
			} else if v[i] == ' ' {
				isVar = false
			} else if isVar && v[i] >= 'a' && v[i] <= 'z' {
				v[i] = 'A' + (v[i] - 'a')
			}
		}
		vars[k] = string(v)
	}
	return vars
}

func (c *config) getAliasMap() map[string][]string {
	m := make(map[string][]string, len(c.Tables))

	for i := range c.Tables {
		t := c.Tables[i]

		if len(t.Table) == 0 {
			continue
		}

		k := strings.ToLower(t.Table)
		m[k] = append(m[k], strings.ToLower(t.Name))
	}
	return m
}
