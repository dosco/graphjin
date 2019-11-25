package serv

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"unicode"

	"github.com/gobuffalo/flect"
	"github.com/spf13/viper"
)

type config struct {
	*viper.Viper

	AppName        string `mapstructure:"app_name"`
	Env            string
	HostPort       string `mapstructure:"host_port"`
	Host           string
	Port           string
	WebUI          bool   `mapstructure:"web_ui"`
	LogLevel       string `mapstructure:"log_level"`
	EnableTracing  bool   `mapstructure:"enable_tracing"`
	UseAllowList   bool   `mapstructure:"use_allow_list"`
	Production     bool
	WatchAndReload bool   `mapstructure:"reload_on_config_change"`
	AuthFailBlock  bool   `mapstructure:"auth_fail_block"`
	SeedFile       string `mapstructure:"seed_file"`
	MigrationsPath string `mapstructure:"migrations_path"`

	Inflections map[string]string

	Auth struct {
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
	}

	DB struct {
		Type       string
		Host       string
		Port       uint16
		DBName     string
		User       string
		Password   string
		Schema     string
		PoolSize   int32 `mapstructure:"pool_size"`
		MaxRetries int   `mapstructure:"max_retries"`
		SetUserID  bool  `mapstructure:"set_user_id"`

		Vars      map[string]string `mapstructure:"variables"`
		Blocklist []string

		Tables []configTable
	} `mapstructure:"database"`

	Tables []configTable

	RolesQuery string `mapstructure:"roles_query"`
	Roles      []configRole
	roles      map[string]*configRole
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

type configQuery struct {
	Limit            int
	Filters          []string
	Columns          []string
	DisableFunctions bool `mapstructure:"disable_functions"`
	Block            bool
}

type configInsert struct {
	Filters []string
	Columns []string
	Presets map[string]string
	Block   bool
}

type configUpdate struct {
	Filters []string
	Columns []string
	Presets map[string]string
	Block   bool
}

type configDelete struct {
	Filters []string
	Columns []string
	Block   bool
}

type configRoleTable struct {
	Name string

	Query  configQuery
	Insert configInsert
	Update configUpdate
	Delete configDelete
}

type configRole struct {
	Name      string
	Match     string
	Tables    []configRoleTable
	tablesMap map[string]*configRoleTable
}

func newConfig(name string) *viper.Viper {
	vi := viper.New()

	vi.SetEnvPrefix("SG")
	vi.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	vi.AutomaticEnv()

	vi.SetConfigName(name)
	vi.AddConfigPath(confPath)
	vi.AddConfigPath("./config")

	if dir, _ := os.Getwd(); strings.HasSuffix(dir, "/serv") {
		vi.AddConfigPath("../config")
	}

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

func (c *config) Init(vi *viper.Viper) error {
	if err := vi.Unmarshal(c); err != nil {
		return fmt.Errorf("unable to decode config, %v", err)
	}
	c.Viper = vi

	if len(c.Tables) == 0 {
		c.Tables = c.DB.Tables
	}

	if c.UseAllowList {
		c.Production = true
	}

	for k, v := range c.Inflections {
		flect.AddPlural(k, v)
	}

	for i := range c.Tables {
		t := c.Tables[i]
		t.Name = flect.Pluralize(strings.ToLower(t.Name))
		t.Table = flect.Pluralize(strings.ToLower(t.Table))
	}

	for i := range c.Roles {
		r := c.Roles[i]
		r.Name = strings.ToLower(r.Name)
	}

	for k, v := range c.DB.Vars {
		c.DB.Vars[k] = sanitize(v)
	}

	c.RolesQuery = sanitize(c.RolesQuery)
	c.roles = make(map[string]*configRole)

	for i := range c.Roles {
		role := &c.Roles[i]

		if _, ok := c.roles[role.Name]; ok {
			errlog.Fatal().Msgf("duplicate role '%s' found", role.Name)
		}
		role.Name = strings.ToLower(role.Name)
		role.Match = sanitize(role.Match)
		role.tablesMap = make(map[string]*configRoleTable)

		for n, table := range role.Tables {
			role.tablesMap[table.Name] = &role.Tables[n]
		}

		c.roles[role.Name] = role
	}

	if _, ok := c.roles["user"]; !ok {
		u := configRole{Name: "user"}
		c.Roles = append(c.Roles, u)
		c.roles["user"] = &u
	}

	if _, ok := c.roles["anon"]; !ok {
		logger.Warn().Msg("unauthenticated requests will be blocked. no role 'anon' defined")
		c.AuthFailBlock = true
	}

	c.validate()

	return nil
}

func (c *config) validate() {
	rm := make(map[string]struct{})

	for i := range c.Roles {
		name := c.Roles[i].Name

		if _, ok := rm[name]; ok {
			errlog.Fatal().Msgf("duplicate config for role '%s'", c.Roles[i].Name)
		}
		rm[name] = struct{}{}
	}

	tm := make(map[string]struct{})

	for i := range c.Tables {
		name := c.Tables[i].Name

		if _, ok := tm[name]; ok {
			errlog.Fatal().Msgf("duplicate config for table '%s'", c.Tables[i].Name)
		}
		tm[name] = struct{}{}
	}

	if len(c.RolesQuery) == 0 {
		logger.Warn().Msgf("no 'roles_query' defined.")
	}
}

func (c *config) getAliasMap() map[string][]string {
	m := make(map[string][]string, len(c.Tables))

	for i := range c.Tables {
		t := c.Tables[i]

		if len(t.Table) == 0 {
			continue
		}

		m[t.Table] = append(m[t.Table], t.Name)
	}
	return m
}

var varRe1 = regexp.MustCompile(`(?mi)\$([a-zA-Z0-9_.]+)`)
var varRe2 = regexp.MustCompile(`\{\{([a-zA-Z0-9_.]+)\}\}`)

func sanitize(s string) string {
	s0 := varRe1.ReplaceAllString(s, `{{$1}}`)

	s1 := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return ' '
		}
		return r
	}, s0)

	return varRe2.ReplaceAllStringFunc(s1, func(m string) string {
		return strings.ToLower(m)
	})
}
