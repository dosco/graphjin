package serv

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/dosco/super-graph/psql"
	"github.com/dosco/super-graph/qcode"
	"github.com/go-pg/pg"
	"github.com/gobuffalo/flect"
	"github.com/sirupsen/logrus"
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
	logger        *logrus.Logger
	conf          *config
	db            *pg.DB
	qcompile      *qcode.Compiler
	pcompile      *psql.Compiler
	authFailBlock int
)

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
	vi.SetDefault("auth_fail_block", "always")

	vi.SetDefault("database.type", "postgres")
	vi.SetDefault("database.host", "localhost")
	vi.SetDefault("database.port", 5432)
	vi.SetDefault("database.user", "postgres")

	vi.SetDefault("env", "development")
	vi.BindEnv("env", "GO_ENV")

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

	if len(c.DB.Tables) == 0 {
		c.DB.Tables = c.DB.Fields
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

func initCompilers(c *config) (*qcode.Compiler, *psql.Compiler, error) {
	schema, err := psql.NewDBSchema(db)
	if err != nil {
		return nil, nil, err
	}

	qc, err := qcode.NewCompiler(qcode.Config{
		DefaultFilter: c.DB.Defaults.Filter,
		FilterMap:     c.getFilterMap(),
		Blacklist:     c.DB.Defaults.Blacklist,
		KeepArgs:      false,
	})

	if err != nil {
		return nil, nil, err
	}

	pc := psql.NewCompiler(psql.Config{
		Schema:   schema,
		Vars:     c.DB.Variables,
		TableMap: c.getAliasMap(),
	})

	return qc, pc, nil
}

func Init() {
	var err error

	logger = initLog()

	conf, err = initConf()
	if err != nil {
		logger.Fatal(err)
	}

	db, err = initDB(conf)
	if err != nil {
		logger.Fatal(err)
	}

	qcompile, pcompile, err = initCompilers(conf)
	if err != nil {
		logger.Fatal(err)
	}

	initResolvers()

	startHTTP()
}

func startHTTP() {
	srv := &http.Server{
		Addr:           conf.HostPort,
		Handler:        routeHandler(),
		ReadTimeout:    5 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	idleConnsClosed := make(chan struct{})
	go func() {
		sigint := make(chan os.Signal, 1)
		signal.Notify(sigint, os.Interrupt)
		<-sigint

		if err := srv.Shutdown(context.Background()); err != nil {
			logger.Printf("http: %v", err)
		}
		close(idleConnsClosed)
	}()

	srv.RegisterOnShutdown(func() {
		if err := db.Close(); err != nil {
			logger.Println(err)
		}
	})

	fmt.Printf("%s listening on %s (%s)\n", serverName, conf.HostPort, conf.Env)

	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		fmt.Println(err)
	}

	<-idleConnsClosed
}

func routeHandler() http.Handler {
	mux := http.NewServeMux()

	mux.Handle("/api/v1/graphql", withAuth(apiv1Http))
	if conf.WebUI {
		mux.Handle("/", http.FileServer(_escFS(false)))
	}

	fn := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", serverName)
		mux.ServeHTTP(w, r)
	}

	return http.HandlerFunc(fn)
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
