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
	"github.com/dosco/super-graph/internal/serv/internal/auth"
)

func initWatcher() {
	cpath := conf.cpath
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
		err := Do(log.Printf, d)
		if err != nil {
			log.Fatalf("ERR %s", err)
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
		log.Fatalf("ERR %s", err)
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
			log.Fatalln("INF shutdown signal received")
		}
		close(idleConnsClosed)
	}()

	srv.RegisterOnShutdown(func() {
		db.Close()
	})

	log.Printf("INF version: %s, git-branch: %s, host-port: %s, app-name: %s, env: %s\n",
		version, gitBranch, hostPort, appName, env)

	log.Printf("INF %s started\n", serverName)

	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalln("INF server closed")
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
		"/api/v1/graphql": apiV1Handler(),
	}

	if err := setActionRoutes(routes); err != nil {
		return nil, err
	}

	if conf.WebUI {
		routes["/"] = http.FileServer(rice.MustFindBox("./web/build").HTTPBox())
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

		fn, err = newAction(&a)
		if err != nil {
			break
		}

		p := fmt.Sprintf("/api/v1/actions/%s", strings.ToLower(a.Name))

		if ac := findAuth(a.AuthName); ac != nil {
			routes[p], err = auth.WithAuth(fn, ac)
		} else {
			routes[p] = fn
		}

		if err != nil {
			return err
		}
	}
	return nil
}

func findAuth(name string) *auth.Auth {
	for _, a := range conf.Auths {
		if strings.EqualFold(a.Name, name) {
			return &a
		}
	}
	return nil
}
