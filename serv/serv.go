package serv

import (
	"context"
	"embed"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/dosco/graphjin/serv/internal/auth"
	"go.opencensus.io/plugin/ochttp"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	serverName = "GraphJin"
	defaultHP  = "0.0.0.0:8080"
)

var (
	// These variables are set using -ldflags
	version string
	commit  string
	date    string

	//go:embed web/build
	webBuild embed.FS
)

func initConfigWatcher(s1 *Service) {
	s := s1.Load().(*service)
	if s.conf.Serv.Production {
		return
	}

	go func() {
		err := startConfigWatcher(s1)
		if err != nil {
			s.log.Fatalf("error in config file watcher: %s", err)
		}
	}()
}

func initHotDeployWatcher(s1 *Service) {
	s := s1.Load().(*service)
	go func() {
		err := startHotDeployWatcher(s1)
		if err != nil {
			s.log.Fatalf("error in hot deploy watcher: %s", err)
		}
	}()
}

func startHTTP(s1 *Service) {
	s := s1.Load().(*service)

	routes, err := routeHandler(s1, http.NewServeMux())
	if err != nil {
		s.log.Fatalf("error setting up routes: %s", err)
	}

	s.srv = &http.Server{
		Addr:           s.conf.hostPort,
		Handler:        routes,
		ReadTimeout:    5 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	if s.conf.telemetryEnabled() {
		if s.conf.Serv.Telemetry.Tracing.ExcludeHealthCheck {
			s.srv.Handler = &ochttp.Handler{
				Handler:          routes,
				IsHealthEndpoint: s.isHealthEndpoint,
			}
		} else {
			s.srv.Handler = &ochttp.Handler{Handler: routes}
		}
	}

	idleConnsClosed := make(chan struct{})
	go func() {
		sigint := make(chan os.Signal, 1)
		signal.Notify(sigint, os.Interrupt)
		<-sigint

		if err := s.srv.Shutdown(context.Background()); err != nil {
			s.log.Warn("shutdown signal received")
		}
		close(idleConnsClosed)
	}()

	s.srv.RegisterOnShutdown(func() {
		if s.closeFn != nil {
			s.closeFn()
		}
		if s.db != nil {
			s.db.Close()
		}
		s.log.Info("shutdown complete")
	})

	ver := version
	dep := s.conf.name

	if version == "" {
		ver = "not-set"
	}

	/*
		s.log.Infof("GraphJin started, version: %s, host-port: %s, app-name: %s, deployment: %s, env: %s",
		 	ver, s.conf.hostPort, s.conf.AppName, dep, os.Getenv("GO_ENV"))
	*/

	fields := []zapcore.Field{
		zap.String("version", ver),
		zap.String("host-port", s.conf.hostPort),
		zap.String("app-name", s.conf.AppName),
		zap.String("deployment-name", dep),
		zap.String("env", os.Getenv("GO_ENV")),
		zap.Bool("production", s.conf.Core.Production),
		zap.Bool("secrets-used", (s.conf.Serv.SecretsFile != "")),
	}

	s.zlog.Info("GraphJin started", fields...)

	l, err := net.Listen("tcp", s.conf.hostPort)
	if err != nil {
		s.log.Fatalf("c: %s", err)
	}

	// signal we are open for business.
	s.state = servListening

	if err := s.srv.Serve(l); err != http.ErrServerClosed {
		s.log.Fatalf("failed to start: %s", err)
	}
	<-idleConnsClosed
}

func setServerHeader(h http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", serverName)
		h.ServeHTTP(w, r)
	}
	return http.HandlerFunc(fn)
}

func findAuth(s *service, name string) *auth.Auth {
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
