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
	"github.com/dosco/super-graph/internal/serv/internal/auth"
	"go.opencensus.io/plugin/ochttp"
)

var (
	apiRoute string = "/api/v1/graphql"
)

func initWatcher(servConf *ServConfig) {
	cpath := servConf.conf.cpath
	if servConf.conf != nil && !servConf.conf.WatchAndReload {
		return
	}

	var d dir
	if cpath == "" || cpath == "./" {
		d = Dir("./config", ReExec(servConf))
	} else {
		d = Dir(cpath, ReExec(servConf))
	}

	go func() {
		err := Do(servConf, servConf.log.Printf, d)
		if err != nil {
			servConf.log.Fatalf("ERR %s", err)
		}
	}()
}

func startHTTP(servConf *ServConfig) {
	var appName string

	defaultHP := "0.0.0.0:8080"
	env := os.Getenv("GO_ENV")

	if servConf.conf != nil {
		appName = servConf.conf.AppName
		hp := strings.SplitN(servConf.conf.HostPort, ":", 2)

		if len(hp) == 2 {
			if servConf.conf.Host != "" {
				hp[0] = servConf.conf.Host
			}

			if servConf.conf.Port != "" {
				hp[1] = servConf.conf.Port
			}

			servConf.conf.hostPort = fmt.Sprintf("%s:%s", hp[0], hp[1])
		}
	}

	if servConf.conf.hostPort == "" {
		servConf.conf.hostPort = defaultHP
	}

	routes, err := routeHandler(servConf)
	if err != nil {
		servConf.log.Fatalf("ERR %s", err)
	}

	srv := &http.Server{
		Addr:           servConf.conf.hostPort,
		Handler:        routes,
		ReadTimeout:    5 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	if servConf.conf.telemetryEnabled() {
		srv.Handler = &ochttp.Handler{Handler: routes}
	}

	idleConnsClosed := make(chan struct{})
	go func() {
		sigint := make(chan os.Signal, 1)
		signal.Notify(sigint, os.Interrupt)
		<-sigint

		if err := srv.Shutdown(context.Background()); err != nil {
			servConf.log.Fatalln("INF shutdown signal received")
		}
		close(idleConnsClosed)
	}()

	srv.RegisterOnShutdown(func() {
		if servConf.conf.closeFn != nil {
			servConf.conf.closeFn()
		}
		servConf.db.Close()
		servConf.log.Fatalln("INF shutdown complete")
	})

	servConf.log.Printf("INF Super Graph started, version: %s, git-branch: %s, host-port: %s, app-name: %s, env: %s\n",
		version, gitBranch, servConf.conf.hostPort, appName, env)

	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		servConf.log.Fatalln("INF server closed")
	}

	<-idleConnsClosed
}

func routeHandler(servConf *ServConfig) (http.Handler, error) {
	var err error
	mux := http.NewServeMux()

	if servConf.conf == nil {
		return mux, nil
	}

	if servConf.conf.APIPath != "" {
		apiRoute = path.Join("/", servConf.conf.APIPath, "/v1/graphql")
	}

	routes := map[string]http.Handler{
		"/health": http.HandlerFunc(health(servConf)),
		apiRoute:  apiV1Handler(servConf),
	}

	if err := setActionRoutes(servConf, routes); err != nil {
		return nil, err
	}

	if servConf.conf.WebUI {
		routes["/"] = http.FileServer(rice.MustFindBox("./web/build").HTTPBox())
	}

	if servConf.conf.HTTPGZip {
		gz := gziphandler.MustNewGzipLevelHandler(6)
		for k, v := range routes {
			routes[k] = gz(v)
		}
	}

	for k, v := range routes {
		mux.Handle(k, v)
	}

	if servConf.conf.telemetryEnabled() {
		servConf.conf.closeFn, err = enableObservability(servConf, mux)
		if err != nil {
			return nil, err
		}
	}

	fn := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", serverName)
		// rate limiter only apply if it's enable from configuration and for API route
		// WebUI and health are excluded from rate limiter
		if servConf.conf.rateLimiterEnable() && strings.Contains(r.URL.Path, apiRoute) {
			err := limit(servConf, w, r)
			if err != nil {
				servConf.log.Println(err.Error())
				return
			}
		}
		mux.ServeHTTP(w, r)
	}

	return http.HandlerFunc(fn), nil
}

func setActionRoutes(servConf *ServConfig, routes map[string]http.Handler) error {
	var err error

	for _, a := range servConf.conf.Actions {
		var fn http.Handler

		fn, err = newAction(servConf, &a)
		if err != nil {
			break
		}

		p := fmt.Sprintf("/api/v1/actions/%s", strings.ToLower(a.Name))

		if ac := findAuth(servConf, a.AuthName); ac != nil {
			routes[p], err = auth.WithAuth(fn, ac)
		} else {
			routes[p] = fn
		}

		if servConf.conf.telemetryEnabled() {
			routes[p] = ochttp.WithRouteTag(routes[p], p)
		}

		if err != nil {
			return err
		}
	}
	return nil
}

func findAuth(servConf *ServConfig, name string) *auth.Auth {
	for _, a := range servConf.conf.Auths {
		if strings.EqualFold(a.Name, name) {
			return &a
		}
	}
	return nil
}
