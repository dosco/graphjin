package serv

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/dosco/super-graph/psql"
	"github.com/dosco/super-graph/qcode"
	"github.com/go-pg/pg"
	"github.com/gobuffalo/flect"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

//go:generate esc -o static.go -ignore \\.DS_Store -prefix ../web/build -private -pkg serv ../web/build

const (
	authFailBlockAlways = iota + 1
	authFailBlockPerQuery
	authFailBlockNever
)

var (
	logger        *logrus.Logger
	conf          *config
	db            *pg.DB
	pcompile      *psql.Compiler
	qcompile      *qcode.Compiler
	authFailBlock int
)

type config struct {
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

		RailsCookie struct {
			SecretKeyBase string `mapstructure:"secret_key_base"`
		} `mapstructure:"rails_cookie"`

		RailsMemcache struct {
			Host string
		} `mapstructure:"rails_memcache"`

		RailsRedis struct {
			URL       string
			Password  string
			MaxIdle   int `mapstructure:"max_idle"`
			MaxActive int `mapstructure:"max_active"`
		} `mapstructure:"rails_redis"`

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
		PoolSize   int    `mapstructure:"pool_size"`
		MaxRetries int    `mapstructure:"max_retries"`
		LogLevel   string `mapstructure:"log_level"`

		Variables map[string]string

		Defaults struct {
			Filter    []string
			Blacklist []string
		}

		Fields []struct {
			Name      string
			Filter    []string
			Table     string
			Blacklist []string
		}
	} `mapstructure:"database"`
}

func initLog() *logrus.Logger {
	log := logrus.New()
	log.Formatter = new(logrus.TextFormatter)
	log.Formatter.(*logrus.TextFormatter).DisableColors = false
	log.Formatter.(*logrus.TextFormatter).DisableTimestamp = true
	log.Level = logrus.TraceLevel
	log.Out = os.Stdout

	return log
}

func initConf() (*config, error) {
	vi := viper.New()

	path := flag.String("path", "./", "Path to config files")
	flag.Parse()

	vi.SetEnvPrefix("SG")
	vi.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	vi.AutomaticEnv()

	vi.AddConfigPath(*path)
	vi.AddConfigPath("./config")
	vi.SetConfigName(getConfigName())

	vi.SetDefault("host_port", "0.0.0.0:8080")
	vi.SetDefault("web_ui", false)
	vi.SetDefault("debug_level", 0)
	vi.SetDefault("enable_tracing", false)

	vi.SetDefault("database.type", "postgres")
	vi.SetDefault("database.host", "localhost")
	vi.SetDefault("database.port", 5432)
	vi.SetDefault("database.user", "postgres")
	vi.SetDefault("database.password", "")

	vi.SetDefault("env", "development")
	vi.BindEnv("env", "GO_ENV")

	vi.SetDefault("auth.rails_redis.max_idle", 80)
	vi.SetDefault("auth.rails_redis.max_active", 12000)

	if err := vi.ReadInConfig(); err != nil {
		return nil, err
	}

	c := &config{}

	if err := vi.Unmarshal(c); err != nil {
		return nil, fmt.Errorf("unable to decode config, %v", err)
	}

	for k, v := range c.Inflections {
		flect.AddPlural(k, v)
	}

	authFailBlock = getAuthFailBlock(c)

	//fmt.Printf("%#v", c)

	return c, nil
}

func initDB(c *config) (*pg.DB, error) {
	opt := &pg.Options{
		Addr:     strings.Join([]string{c.DB.Host, c.DB.Port}, ":"),
		User:     c.DB.User,
		Password: c.DB.Password,
		Database: c.DB.DBName,
	}

	if c.DB.PoolSize != 0 {
		opt.PoolSize = conf.DB.PoolSize
	}

	if c.DB.MaxRetries != 0 {
		opt.MaxRetries = c.DB.MaxRetries
	}

	db := pg.Connect(opt)
	if db == nil {
		return nil, errors.New("failed to connect to postgres db")
	}

	return db, nil
}

func initCompilers(c *config) (*qcode.Compiler, *psql.Compiler, error) {
	cdb := c.DB

	fm := make(map[string][]string, len(cdb.Fields))
	tmap := make(map[string]string, len(cdb.Fields))

	for i := range cdb.Fields {
		f := cdb.Fields[i]
		name := flect.Pluralize(strings.ToLower(f.Name))
		if len(f.Filter) != 0 {
			if f.Filter[0] == "none" {
				fm[name] = []string{}
			} else {
				fm[name] = f.Filter
			}
		}
		if len(f.Table) != 0 {
			tmap[name] = f.Table
		}
	}

	qc, err := qcode.NewCompiler(qcode.Config{
		Filter:    cdb.Defaults.Filter,
		FilterMap: fm,
		Blacklist: cdb.Defaults.Blacklist,
	})
	if err != nil {
		return nil, nil, err
	}

	schema, err := psql.NewDBSchema(db)
	if err != nil {
		return nil, nil, err
	}

	pc := psql.NewCompiler(psql.Config{
		Schema:   schema,
		Vars:     cdb.Variables,
		TableMap: tmap,
	})

	return qc, pc, nil
}

func InitAndListen() {
	var err error

	logger = initLog()

	conf, err = initConf()
	if err != nil {
		log.Fatal(err)
	}

	db, err = initDB(conf)
	if err != nil {
		log.Fatal(err)
	}

	qcompile, pcompile, err = initCompilers(conf)
	if err != nil {
		log.Fatal(err)
	}

	http.HandleFunc("/api/v1/graphql", withAuth(apiv1Http))

	if conf.WebUI {
		http.Handle("/", http.FileServer(_escFS(false)))
	}

	fmt.Printf("Super-Graph listening on %s (%s)\n",
		conf.HostPort, conf.Env)

	logger.Fatal(http.ListenAndServe(conf.HostPort, nil))
}

func getConfigName() string {
	ge := strings.ToLower(os.Getenv("GO_ENV"))

	switch {
	case strings.HasPrefix(ge, "pro"):
		return "prod"

	case strings.HasPrefix(ge, "sta"):
		return "stage"

	case strings.HasPrefix(ge, "tes"):
		return "test"
	}

	return "dev"
}

func getAuthFailBlock(c *config) int {
	switch c.AuthFailBlock {
	case "always":
		return authFailBlockAlways
	case "per_query", "perquery", "query":
		return authFailBlockPerQuery
	case "never", "false":
		return authFailBlockNever
	}

	return authFailBlockAlways
}
