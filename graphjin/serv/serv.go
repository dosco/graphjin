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
)

func initCompilers(c *config) (*qcode.Compiler, *psql.Compiler, error) {
	var err error

	schema, err = psql.NewDBSchema(db, c.getAliasMap())
	if err != nil {
		return nil, nil, err
	}

	conf := qcode.Config{
		Blocklist: c.DB.Blocklist,
		KeepArgs:  false,
	}

	qc, err := qcode.NewCompiler(conf)
	if err != nil {
		return nil, nil, err
	}

	blockFilter := []string{"false"}

	for _, r := range c.Roles {
		for _, t := range r.Tables {
			query := qcode.QueryConfig{
				Limit:            t.Query.Limit,
				Filters:          t.Query.Filters,
				Columns:          t.Query.Columns,
				DisableFunctions: t.Query.DisableFunctions,
			}

			if t.Query.Block {
				query.Filters = blockFilter
			}

			insert := qcode.InsertConfig{
				Filters: t.Insert.Filters,
				Columns: t.Insert.Columns,
				Presets: t.Insert.Presets,
			}

			if t.Query.Block {
				insert.Filters = blockFilter
			}

			update := qcode.UpdateConfig{
				Filters: t.Insert.Filters,
				Columns: t.Insert.Columns,
				Presets: t.Insert.Presets,
			}

			if t.Query.Block {
				update.Filters = blockFilter
			}

			delete := qcode.DeleteConfig{
				Filters: t.Insert.Filters,
				Columns: t.Insert.Columns,
			}

			if t.Query.Block {
				delete.Filters = blockFilter
			}

			err := qc.AddRole(r.Name, t.Name, qcode.TRConfig{
				Query:  query,
				Insert: insert,
				Update: update,
				Delete: delete,
			})

			if err != nil {
				return nil, nil, err
			}
		}
	}

	pc := psql.NewCompiler(psql.Config{
		Schema: schema,
		Vars:   c.DB.Vars,
	})

	return qc, pc, nil
}

func initWatcher(cpath string) {
	if !conf.WatchAndReload {
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
		Str("app_name", conf.AppName).
		Str("env", conf.Env).
		Msgf("%s listening", serverName)

	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		errlog.Error().Err(err).Msg("server closed")
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
