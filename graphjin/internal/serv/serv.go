package serv

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strings"
	"time"

	rice "github.com/GeertJohan/go.rice"
	"github.com/NYTimes/gziphandler"
	"github.com/dosco/graphjin/internal/serv/internal/auth"
	"go.opencensus.io/plugin/ochttp"
)

var (
	apiRoute string = "/api/v1/graphql"
)

func initWatcher(sc *ServConfig) {
	cpath := sc.conf.cpath
	if sc.conf != nil && !sc.conf.WatchAndReload {
		return
	}

	var d dir
	if cpath == "" || cpath == "./" {
		d = Dir("./config", ReExec(sc))
	} else {
		d = Dir(cpath, ReExec(sc))
	}

	go func() {
		err := Do(sc, sc.log.Infof, d)
		if err != nil {
			sc.log.Fatalf("Error in config file wacher: %s", err)
		}
	}()
}

func startHTTP(sc *ServConfig) {
	var appName string

	defaultHP := "0.0.0.0:8080"
	env := os.Getenv("GO_ENV")

	if sc.conf != nil {
		appName = sc.conf.AppName
		hp := strings.SplitN(sc.conf.HostPort, ":", 2)

		if len(hp) == 2 {
			if sc.conf.Host != "" {
				hp[0] = sc.conf.Host
			}

			if sc.conf.Port != "" {
				hp[1] = sc.conf.Port
			}

			sc.conf.hostPort = fmt.Sprintf("%s:%s", hp[0], hp[1])
		}
	}

	if sc.conf.hostPort == "" {
		sc.conf.hostPort = defaultHP
	}

	routes, err := routeHandler(sc)
	if err != nil {
		sc.log.Fatalf("Error setting up API routes: %s", err)
	}

	srv := &http.Server{
		Addr:           sc.conf.hostPort,
		Handler:        routes,
		ReadTimeout:    5 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	if sc.conf.telemetryEnabled() {
		srv.Handler = &ochttp.Handler{Handler: routes}
	}

	idleConnsClosed := make(chan struct{})
	go func() {
		sigint := make(chan os.Signal, 1)
		signal.Notify(sigint, os.Interrupt)
		<-sigint

		if err := srv.Shutdown(context.Background()); err != nil {
			sc.log.Warn("Shutdown signal received")
		}
		close(idleConnsClosed)
	}()

	srv.RegisterOnShutdown(func() {
		if sc.conf.closeFn != nil {
			sc.conf.closeFn()
		}
		sc.db.Close()
		sc.log.Info("Shutdown complete")
	})

	sc.log.Infof("GraphJin started, version: %s, host-port: %s, app-name: %s, env: %s\n",
		buildInfo.Version, sc.conf.hostPort, appName, env)

	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		sc.log.Fatal("Server stopped")
	}

	<-idleConnsClosed
}

func routeHandler(sc *ServConfig) (http.Handler, error) {
	var err error
	mux := http.NewServeMux()

	if sc.conf == nil {
		return mux, nil
	}

	if sc.conf.APIPath != "" {
		apiRoute = path.Join("/", sc.conf.APIPath, "/v1/graphql")
	}

	// Main GraphQL API handler
	apiHandler := apiV1Handler(sc)

	// API rate limiter
	if sc.conf.rateLimiterEnable() {
		apiHandler = rateLimiter(sc, apiHandler)
	}

	routes := map[string]http.Handler{
		"/health": http.HandlerFunc(health(sc)),
		apiRoute:  apiHandler,
	}

	if err := setActionRoutes(sc, routes); err != nil {
		return nil, err
	}

	if sc.conf.WebUI {
		routes["/"] = http.FileServer(rice.MustFindBox("./web/build").HTTPBox())
	}

	if sc.conf.HTTPGZip {
		gz := gziphandler.MustNewGzipLevelHandler(6)
		for k, v := range routes {
			routes[k] = gz(v)
		}
	}

	for k, v := range routes {
		mux.Handle(k, v)
	}

	if sc.conf.telemetryEnabled() {
		sc.conf.closeFn, err = enableObservability(sc, mux)
		if err != nil {
			return nil, err
		}
	}

	return setServerHeader(mux), nil
}

func setServerHeader(h http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", serverName)
		h.ServeHTTP(w, r)
	}
	return http.HandlerFunc(fn)
}

func setActionRoutes(sc *ServConfig, routes map[string]http.Handler) error {
	var err error

	for _, a := range sc.conf.Actions {
		var fn http.Handler

		fn, err = newAction(sc, &a)
		if err != nil {
			break
		}

		p := fmt.Sprintf("/api/v1/actions/%s", strings.ToLower(a.Name))

		if ac := findAuth(sc, a.AuthName); ac != nil {
			routes[p], err = auth.WithAuth(fn, ac)
		} else {
			routes[p] = fn
		}

		if sc.conf.telemetryEnabled() {
			routes[p] = ochttp.WithRouteTag(routes[p], p)
		}

		if err != nil {
			return err
		}
	}
	return nil
}

func findAuth(sc *ServConfig, name string) *auth.Auth {
	for _, a := range sc.conf.Auths {
		if strings.EqualFold(a.Name, name) {
			return &a
		}
	}
	return nil
}
