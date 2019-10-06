package serv

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	rice "github.com/GeertJohan/go.rice"
	"github.com/dosco/super-graph/psql"
	"github.com/dosco/super-graph/qcode"
	"github.com/gobuffalo/flect"
)

func initCompilers(c *config) (*qcode.Compiler, *psql.Compiler, error) {
	schema, err := psql.NewDBSchema(db, c.getAliasMap())
	if err != nil {
		return nil, nil, err
	}

	conf := qcode.Config{
		DefaultFilter: c.DB.Defaults.Filter,
		FilterMap: qcode.Filters{
			All:    make(map[string][]string, len(c.Tables)),
			Query:  make(map[string][]string, len(c.Tables)),
			Insert: make(map[string][]string, len(c.Tables)),
			Update: make(map[string][]string, len(c.Tables)),
			Delete: make(map[string][]string, len(c.Tables)),
		},
		Blocklist: c.DB.Defaults.Blocklist,
		KeepArgs:  false,
	}

	for i := range c.Tables {
		t := c.Tables[i]

		singular := flect.Singularize(t.Name)
		plural := flect.Pluralize(t.Name)

		setFilter := func(fm map[string][]string, fil []string) {
			switch {
			case len(fil) == 0:
				return
			case fil[0] == "none" || len(fil[0]) == 0:
				fm[singular] = []string{}
				fm[plural] = []string{}
			default:
				fm[singular] = t.Filter
				fm[plural] = t.Filter
			}
		}

		setFilter(conf.FilterMap.All, t.Filter)
		setFilter(conf.FilterMap.Query, t.FilterQuery)
		setFilter(conf.FilterMap.Insert, t.FilterInsert)
		setFilter(conf.FilterMap.Update, t.FilterUpdate)
		setFilter(conf.FilterMap.Delete, t.FilterDelete)
	}

	qc, err := qcode.NewCompiler(conf)
	if err != nil {
		return nil, nil, err
	}

	pc := psql.NewCompiler(psql.Config{
		Schema: schema,
		Vars:   c.getVariables(),
	})

	return qc, pc, nil
}

func initWatcher(cpath string) {
	if conf.WatchAndReload == false {
		return
	}

	var d dir
	if len(cpath) == 0 || cpath == "./" {
		d = Dir("./config", ReExec)
	} else {
		d = Dir(cpath, ReExec)
	}

	go func() {
		err := Do(logger.Printf, d)
		if err != nil {
			logger.Fatal().Err(err).Send()
		}
	}()
}

func startHTTP() {
	hp := strings.SplitN(conf.HostPort, ":", 2)

	if len(conf.Host) != 0 {
		hp[0] = conf.Host
	}

	if len(conf.Port) != 0 {
		hp[1] = conf.Port
	}

	hostPort := fmt.Sprintf("%s:%s", hp[0], hp[1])

	srv := &http.Server{
		Addr:           hostPort,
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
			logger.Error().Err(err).Msg("shutdown signal received")
		}
		close(idleConnsClosed)
	}()

	srv.RegisterOnShutdown(func() {
		db.Close()
	})

	fmt.Printf("%s listening on %s (%s)\n", serverName, hostPort, conf.Env)

	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		logger.Error().Err(err).Msg("server closed")
	}

	<-idleConnsClosed
}

func routeHandler() http.Handler {
	mux := http.NewServeMux()

	mux.Handle("/api/v1/graphql", withAuth(apiv1Http))
	if conf.WebUI {
		mux.Handle("/", http.FileServer(rice.MustFindBox("../web/build").HTTPBox()))
	}

	fn := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", serverName)
		mux.ServeHTTP(w, r)
	}

	return http.HandlerFunc(fn)
}

func getConfigName() string {
	if len(os.Getenv("GO_ENV")) == 0 {
		return "dev"
	}

	ge := strings.ToLower(os.Getenv("GO_ENV"))

	switch {
	case strings.HasPrefix(ge, "pro"):
		return "prod"

	case strings.HasPrefix(ge, "sta"):
		return "stage"

	case strings.HasPrefix(ge, "tes"):
		return "test"

	case strings.HasPrefix(ge, "dev"):
		return "dev"
	}

	return ge
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
