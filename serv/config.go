package serv

import (
	"strings"

	"github.com/gobuffalo/flect"
)

type config struct {
	AppName       string `mapstructure:"app_name"`
	Env           string
	HostPort      string `mapstructure:"host_port"`
	WebUI         bool   `mapstructure:"web_ui"`
	DebugLevel    int    `mapstructure:"debug_level"`
	EnableTracing bool   `mapstructure:"enable_tracing"`
	AuthFailBlock string `mapstructure:"auth_fail_block"`
	Inflections   map[string]string

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
		Port       string
		DBName     string
		User       string
		Password   string
		Schema     string
		PoolSize   int    `mapstructure:"pool_size"`
		MaxRetries int    `mapstructure:"max_retries"`
		LogLevel   string `mapstructure:"log_level"`

		Variables map[string]string

		Defaults struct {
			Filter    []string
			Blacklist []string
		}

		Fields []configTable
		Tables []configTable
	} `mapstructure:"database"`
}

type configTable struct {
	Name      string
	Filter    []string
	Table     string
	Blacklist []string
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

func (c *config) getAliasMap() map[string][]string {
	m := make(map[string][]string, len(c.DB.Tables))

	for i := range c.DB.Tables {
		t := c.DB.Tables[i]

		if len(t.Table) == 0 {
			continue
		}

		k := strings.ToLower(t.Table)
		m[k] = append(m[k], strings.ToLower(t.Name))
	}
	return m
}

func (c *config) getFilterMap() map[string][]string {
	m := make(map[string][]string, len(c.DB.Tables))

	for i := range c.DB.Tables {
		t := c.DB.Tables[i]

		if len(t.Filter) == 0 {
			continue
		}
		singular := flect.Singularize(t.Name)
		plural := flect.Pluralize(t.Name)

		if t.Filter[0] == "none" {
			m[singular] = []string{}
			m[plural] = []string{}

		} else {
			m[singular] = t.Filter
			m[plural] = t.Filter
		}
	}

	return m
}
