package serv

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/dosco/super-graph/psql"
	"github.com/dosco/super-graph/qcode"
	"github.com/go-pg/pg"
	"github.com/gobuffalo/flect"
	"github.com/rs/zerolog"
	"github.com/spf13/viper"
)

//go:generate esc -o static.go -ignore \\.DS_Store -prefix ../web/build -private -pkg serv ../web/build

const (
	serverName = "Super Graph"

	authFailBlockAlways = iota + 1
	authFailBlockPerQuery
	authFailBlockNever
)

var (
	logger        *zerolog.Logger
	conf          *config
	db            *pg.DB
	qcompile      *qcode.Compiler
	pcompile      *psql.Compiler
	authFailBlock int
)

func initLog() *zerolog.Logger {
	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).
		With().
		Timestamp().
		Caller().
		Logger()

	return &logger
	/*
		log := logrus.New()
		logger.Formatter = new(logrus.TextFormatter)
		logger.Formatter.(*logrus.TextFormatter).DisableColors = false
		logger.Formatter.(*logrus.TextFormatter).DisableTimestamp = true
		logger.Level = logrus.TraceLevel
		logger.Out = os.Stdout
	*/
}

func initConf(path string) (*config, error) {
	vi := viper.New()

	vi.SetEnvPrefix("SG")
	vi.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	vi.AutomaticEnv()

	vi.AddConfigPath(path)
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

	vi.SetDefault("env", "development")
	vi.BindEnv("env", "GO_ENV")
	vi.BindEnv("HOST", "HOST")
	vi.BindEnv("PORT", "PORT")

	vi.SetDefault("auth.rails.max_idle", 80)
	vi.SetDefault("auth.rails.max_active", 12000)

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

	for i := range c.DB.Tables {
		t := c.DB.Tables[i]
		t.Name = flect.Pluralize(strings.ToLower(t.Name))
	}

	authFailBlock = getAuthFailBlock(c)

	//fmt.Printf("%#v", c)

	return c, nil
}

func initDB(c *config) (*pg.DB, error) {
	opt := &pg.Options{
		Addr:            strings.Join([]string{c.DB.Host, c.DB.Port}, ":"),
		User:            c.DB.User,
		Password:        c.DB.Password,
		Database:        c.DB.DBName,
		ApplicationName: c.AppName,
	}

	if c.DB.PoolSize != 0 {
		opt.PoolSize = conf.DB.PoolSize
	}

	if c.DB.MaxRetries != 0 {
		opt.MaxRetries = c.DB.MaxRetries
	}

	if len(c.DB.Schema) != 0 {
		opt.OnConnect = func(conn *pg.Conn) error {
			_, err := conn.Exec("set search_path=?", c.DB.Schema)
			if err != nil {
				return err
			}
			return nil
		}
	}

	db := pg.Connect(opt)
	if db == nil {
		return nil, errors.New("failed to connect to postgres db")
	}

	return db, nil
}

func Init() {
	var err error

	path := flag.String("path", "./config", "Path to config files")
	flag.Parse()

	logger = initLog()

	conf, err = initConf(*path)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to read config")
	}

	logLevel, err := zerolog.ParseLevel(conf.LogLevel)
	if err != nil {
		logger.Error().Err(err).Msg("error setting log_level")
	}
	zerolog.SetGlobalLevel(logLevel)

	db, err = initDB(conf)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to connect to database")
	}

	qcompile, pcompile, err = initCompilers(conf)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to connect to database")
	}

	if err := initResolvers(); err != nil {
		logger.Fatal().Err(err).Msg("failed to initialized resolvers")
	}

	args := flag.Args()

	if len(args) == 0 {
		cmdServ(*path)
	}

	switch args[0] {
	case "seed":
		cmdSeed(*path)

	case "serv":
		fallthrough

	default:
		logger.Fatal().Msg("options: [serve|seed]")
	}

}

func cmdServ(path string) {
	initAllowList(path)
	initPreparedList()
	initWatcher(path)

	startHTTP()
}
