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
	"github.com/NYTimes/gziphandler"
	"github.com/dosco/super-graph/psql"
	"github.com/dosco/super-graph/qcode"
)

func initCompilers(c *config) (*qcode.Compiler, *psql.Compiler, error) {
	di, err := psql.GetDBInfo(db)
	if err != nil {
		return nil, nil, err
	}

	if err = addTables(c, di); err != nil {
		return nil, nil, err
	}

	if err = addForeignKeys(c, di); err != nil {
		return nil, nil, err
	}

	schema, err = psql.NewDBSchema(di, c.getAliasMap())
	if err != nil {
		return nil, nil, err
	}

	qc, err := qcode.NewCompiler(qcode.Config{
		Blocklist: c.DB.Blocklist,
	})
	if err != nil {
		return nil, nil, err
	}

	if err := addRoles(c, qc); err != nil {
		return nil, nil, err
	}

	pc := psql.NewCompiler(psql.Config{
		Schema: schema,
		Vars:   c.DB.Vars,
	})

	return qc, pc, nil
}

func initWatcher(cpath string) {
	if conf != nil && !conf.WatchAndReload {
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
			errlog.Fatal().Err(err).Send()
		}
	}()
}

func startHTTP() {
	var hostPort string
	var appName string

	defaultHP := "0.0.0.0:8080"
	env := os.Getenv("GO_ENV")

	if conf != nil {
		appName = conf.AppName
		hp := strings.SplitN(conf.HostPort, ":", 2)

		if len(hp) == 2 {
			if len(conf.Host) != 0 {
				hp[0] = conf.Host
			}

			if len(conf.Port) != 0 {
				hp[1] = conf.Port
			}

			hostPort = fmt.Sprintf("%s:%s", hp[0], hp[1])
		}
	}

	if len(hostPort) == 0 {
		hostPort = defaultHP
	}

	routes, err := routeHandler()
	if err != nil {
		errlog.Fatal().Err(err).Send()
	}

	srv := &http.Server{
		Addr:           hostPort,
		Handler:        routes,
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
			errlog.Error().Err(err).Msg("shutdown signal received")
		}
		close(idleConnsClosed)
	}()

	srv.RegisterOnShutdown(func() {
		db.Close()
	})

	logger.Info().
		Str("version", version).
		Str("git_branch", gitBranch).
		Str("host_post", hostPort).
		Str("app_name", appName).
		Str("env", env).
		Msgf("%s listening", serverName)

	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		errlog.Error().Err(err).Msg("server closed")
	}

	<-idleConnsClosed
}

func routeHandler() (http.Handler, error) {
	mux := http.NewServeMux()

	if conf == nil {
		return mux, nil
	}

	routes := map[string]http.Handler{
		"/health":         http.HandlerFunc(health),
		"/api/v1/graphql": withAuth(http.HandlerFunc(apiV1), conf.Auth),
	}

	if err := setActionRoutes(routes); err != nil {
		return nil, err
	}

	if conf.WebUI {
		routes["/"] = http.FileServer(rice.MustFindBox("../web/build").HTTPBox())
	}

	if conf.HTTPGZip {
		gz := gziphandler.MustNewGzipLevelHandler(6)
		for k, v := range routes {
			routes[k] = gz(v)
		}
	}

	for k, v := range routes {
		mux.Handle(k, v)
	}

	fn := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", serverName)
		mux.ServeHTTP(w, r)
	}

	return http.HandlerFunc(fn), nil
}

func setActionRoutes(routes map[string]http.Handler) error {
	var err error

	for _, a := range conf.Actions {
		var fn http.Handler

		fn, err = newAction(a)
		if err != nil {
			break
		}

		p := fmt.Sprintf("/api/v1/actions/%s", strings.ToLower(a.Name))

		if authc, ok := findAuth(a.AuthName); ok {
			routes[p] = withAuth(fn, authc)
		} else {
			routes[p] = fn
		}
	}
	return nil
}

func findAuth(name string) (configAuth, bool) {
	var authc configAuth

	for _, a := range conf.Auths {
		if strings.EqualFold(a.Name, name) {
			return a, true
		}
	}
	return authc, false
}
