package serv

import (
	"errors"
	"flag"
	"fmt"
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

const (
	authFailBlockAlways = iota + 1
	authFailBlockPerQuery
	authFailBlockNever
)

var (
	logger        *logrus.Logger
	debug         int
	conf          *viper.Viper
	db            *pg.DB
	pcompile      *psql.Compiler
	qcompile      *qcode.Compiler
	authFailBlock int
)

func initLog() {
	logger = logrus.New()
	logger.Formatter = new(logrus.TextFormatter)
	logger.Formatter.(*logrus.TextFormatter).DisableColors = false
	logger.Formatter.(*logrus.TextFormatter).DisableTimestamp = true
	logger.Level = logrus.TraceLevel
	logger.Out = os.Stdout
}

func initConf() {
	conf = viper.New()

	cPath := flag.String("path", ".", "Path to folder that contains config files")
	flag.Parse()

	conf.AddConfigPath(*cPath)

	switch os.Getenv("GO_ENV") {
	case "production", "prod":
		conf.SetConfigName("prod")
	case "staging", "stage":
		conf.SetConfigName("stage")
	default:
		conf.SetConfigName("dev")
	}

	err := conf.ReadInConfig()
	if err != nil {
		logger.Fatal(err)
	}

	debug = conf.GetInt("debug_level")

	for k, v := range conf.GetStringMapString("inflections") {
		flect.AddPlural(k, v)
	}

	conf.SetDefault("host_port", "0.0.0.0:8080")
	conf.SetDefault("web_ui", false)
	conf.SetDefault("debug_level", 0)

	conf.SetDefault("database.type", "postgres")
	conf.SetDefault("database.host", "localhost")
	conf.SetDefault("database.port", 5432)
	conf.SetDefault("database.user", "postgres")
	conf.SetDefault("database.password", "")

	conf.SetDefault("env", "development")
	conf.BindEnv("env", "GO_ENV")

	switch conf.GetString("auth_fail_block") {
	case "always":
		authFailBlock = authFailBlockAlways
	case "per_query", "perquery", "query":
		authFailBlock = authFailBlockPerQuery
	case "never", "false":
		authFailBlock = authFailBlockNever
	default:
		authFailBlock = authFailBlockAlways
	}
}

func initDB() {
	conf.BindEnv("database.host", "SG_DATABASE_HOST")
	conf.BindEnv("database.port", "SG_DATABASE_PORT")
	conf.BindEnv("database.user", "SG_DATABASE_USER")
	conf.BindEnv("database.password", "SG_DATABASE_PASSWORD")

	hostport := strings.Join([]string{
		conf.GetString("database.host"), conf.GetString("database.port")}, ":")

	opt := &pg.Options{
		Addr:     hostport,
		User:     conf.GetString("database.user"),
		Password: conf.GetString("database.password"),
		Database: conf.GetString("database.dbname"),
	}

	if conf.IsSet("database.pool_size") {
		opt.PoolSize = conf.GetInt("database.pool_size")
	}

	if conf.IsSet("database.max_retries") {
		opt.MaxRetries = conf.GetInt("database.max_retries")
	}

	if db = pg.Connect(opt); db == nil {
		logger.Fatal(errors.New("failed to connect to postgres db"))
	}
}

func initCompilers() {
	filters := conf.GetStringMapString("database.filters")
	blacklist := conf.GetStringSlice("database.blacklist")

	fm := qcode.NewFilterMap(filters)
	bl := qcode.NewBlacklist(blacklist)
	qcompile = qcode.NewCompiler(fm, bl)

	schema, err := psql.NewDBSchema(db)
	if err != nil {
		logger.Fatal(err)
	}

	varlist := conf.GetStringMapString("database.variables")
	vars := psql.NewVariables(varlist)

	pcompile = psql.NewCompiler(schema, vars)
}

func InitAndListen() {
	initLog()
	initConf()
	initDB()
	initCompilers()

	http.HandleFunc("/api/v1/graphql", withAuth(apiv1Http))

	if conf.GetBool("web_ui") {
		fs := http.FileServer(http.Dir("web/build"))
		http.Handle("/", fs)
	}

	hp := conf.GetString("host_port")
	fmt.Printf("Super-Graph listening on %s (%s)\n", hp, conf.GetString("env"))

	logger.Fatal(http.ListenAndServe(hp, nil))
}
