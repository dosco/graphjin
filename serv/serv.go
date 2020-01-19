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

func routeHandler() http.Handler {
	var apiH http.Handler

	if conf != nil && conf.HTTPGZip {
		gzipH := gziphandler.MustNewGzipLevelHandler(6)
		apiH = gzipH(http.HandlerFunc(apiV1))
	} else {
		apiH = http.HandlerFunc(apiV1)
	}

	mux := http.NewServeMux()

	if conf != nil {
		mux.HandleFunc("/health", health)
		mux.Handle("/api/v1/graphql", withAuth(apiH))

		if conf.WebUI {
			mux.Handle("/", http.FileServer(rice.MustFindBox("../web/build").HTTPBox()))
		}
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

func isDev() bool {
	return strings.HasPrefix(os.Getenv("GO_ENV"), "dev")
}
