package serv

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strings"
	"time"

	"github.com/dosco/graphjin/serv/internal/auth"
	"github.com/klauspost/compress/gzhttp"
	"go.opencensus.io/plugin/ochttp"
	"go.uber.org/zap"
)

const (
	serverName = "GraphJin"
)

var (
	// These variables are set using -ldflags
	version string
	commit  string
	date    string
)

var (
	//go:embed web/build
	webBuild embed.FS

	apiRoute string = "/api/v1/graphql"
)

func initWatcher(s *Service) {
	cpath := s.conf.Serv.ConfigPath

	var d dir
	if cpath == "" || cpath == "./" {
		d = watchDir("./config", reExec(s))
	} else {
		d = watchDir(cpath, reExec(s))
	}

	go func() {
		err := do(s, s.log.Infof, d)
		if err != nil {
			s.log.Fatalf("Error in config file wacher: %s", err)
		}
	}()
}

func isHealthEndpoint(r *http.Request) bool {
	healthEndPointPaths := []string{"/health", "/metrics"}
	for _, healthEndPointPath := range healthEndPointPaths {
		if r.URL.Path == healthEndPointPath {
			return true
		}
	}
	return false
}

func startHTTP(s *Service) {
	var appName string

	defaultHP := "0.0.0.0:8080"
	env := os.Getenv("GO_ENV")

	if s.conf != nil {
		appName = s.conf.AppName
		hp := strings.SplitN(s.conf.HostPort, ":", 2)

		if len(hp) == 2 {
			if s.conf.Host != "" {
				hp[0] = s.conf.Host
			}

			if s.conf.Port != "" {
				hp[1] = s.conf.Port
			}

			s.conf.hostPort = fmt.Sprintf("%s:%s", hp[0], hp[1])
		}
	}

	if s.conf.hostPort == "" {
		s.conf.hostPort = defaultHP
	}

	routes, err := routeHandler(s, http.NewServeMux())
	if err != nil {
		s.log.Fatalf("Error setting up API routes: %s", err)
	}

	srv := &http.Server{
		Addr:           s.conf.hostPort,
		Handler:        routes,
		ReadTimeout:    5 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	if s.conf.telemetryEnabled() {
		if s.conf.Serv.Telemetry.Tracing.ExcludeHealthCheck {
			srv.Handler = &ochttp.Handler{
				Handler:          routes,
				IsHealthEndpoint: isHealthEndpoint,
			}
		} else {
			srv.Handler = &ochttp.Handler{Handler: routes}
		}
	}

	idleConnsClosed := make(chan struct{})
	go func() {
		sigint := make(chan os.Signal, 1)
		signal.Notify(sigint, os.Interrupt)
		<-sigint

		if err := srv.Shutdown(context.Background()); err != nil {
			s.log.Warn("Shutdown signal received")
		}
		close(idleConnsClosed)
	}()

	srv.RegisterOnShutdown(func() {
		if s.conf.closeFn != nil {
			s.conf.closeFn()
		}
		if s.db != nil {
			s.db.Close()
		}
		s.log.Info("Shutdown complete")
	})

	s.log.Infof("GraphJin started, version: %s, host-port: %s, app-name: %s, env: %s\n",
		version, s.conf.hostPort, appName, env)

	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		s.log.Fatal("Server stopped")
	}

	<-idleConnsClosed
}

func routeHandler(s *Service, mux *http.ServeMux) (http.Handler, error) {
	var err error

	if s.conf == nil {
		return mux, nil
	}

	if s.conf.APIPath != "" {
		apiRoute = path.Join("/", s.conf.APIPath, "/v1/graphql")
	}

	// Main GraphQL API handler
	apiHandler := apiV1Handler(s)

	// API rate limiter
	if s.conf.rateLimiterEnable() {
		apiHandler = rateLimiter(s, apiHandler)
	}

	routes := map[string]http.Handler{
		"/health": http.HandlerFunc(health(s)),
		apiRoute:  apiHandler,
	}

	if err := setActionRoutes(s, routes); err != nil {
		return nil, err
	}

	if s.conf.WebUI {
		webRoot, err := fs.Sub(webBuild, "web/build")
		if err != nil {
			return nil, err
		}
		routes["/"] = http.FileServer(http.FS(webRoot))
	}

	if s.conf.HTTPGZip {
		gz, err := gzhttp.NewWrapper(gzhttp.MinSize(2000), gzhttp.CompressionLevel(6))
		if err != nil {
			return nil, err
		}

		for k, v := range routes {
			routes[k] = gz(v)
		}
	}

	for k, v := range routes {
		mux.Handle(k, v)
	}

	if s.conf.telemetryEnabled() {
		s.conf.closeFn, err = enableObservability(s, mux)
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

func setActionRoutes(s *Service, routes map[string]http.Handler) error {
	var zlog *zap.Logger
	var err error

	if s.conf.Debug {
		zlog = s.zlog
	}

	for _, a := range s.conf.Actions {
		var fn http.Handler

		fn, err = newAction(s, &a)
		if err != nil {
			break
		}

		p := fmt.Sprintf("/api/v1/actions/%s", strings.ToLower(a.Name))

		if ac := findAuth(s, a.AuthName); ac != nil {
			routes[p], err = auth.WithAuth(fn, ac, zlog)
		} else {
			routes[p] = fn
		}

		if s.conf.telemetryEnabled() {
			routes[p] = ochttp.WithRouteTag(routes[p], p)
		}

		if err != nil {
			return err
		}
	}
	return nil
}

func findAuth(s *Service, name string) *auth.Auth {
	for _, a := range s.conf.Auths {
		if strings.EqualFold(a.Name, name) {
			return &a
		}
	}
	return nil
}

type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

func GetBuildInfo() BuildInfo {
	return BuildInfo{
		Version: version,
		Commit:  commit,
		Date:    date,
	}
}
