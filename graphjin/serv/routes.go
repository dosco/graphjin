package serv

import (
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/dosco/graphjin/serv/internal/auth"
	"github.com/klauspost/compress/gzhttp"
	"go.opencensus.io/plugin/ochttp"
	"go.uber.org/zap"
)

const (
	deployRoute  = "/api/v1/deploy"
	apiRoute     = "/api/v1/graphql"
	actionRoute  = "/api/v1/actions"
	healthRoute  = "/health"
	metricsRoute = "/metrics"
)

func (s *service) isHealthEndpoint(r *http.Request) bool {
	p := r.URL.Path
	return p == healthRoute || p == metricsRoute ||
		(s.conf.Telemetry.Metrics.Endpoint != "" && p == s.conf.Telemetry.Metrics.Endpoint)
}

func routeHandler(s1 *Service, mux *http.ServeMux) (http.Handler, error) {
	var err error
	s := s1.Load().(*service)

	routes := map[string]http.Handler{
		healthRoute: healthV1Handler(s1), // Healthcheck API
		apiRoute:    apiV1Handler(s1),    // Main GraphQL API
	}

	if s.conf.HotDeploy {
		routes[deployRoute] = adminDeployHandler(s1) // Deploy Config API

	}

	if s.conf.rateLimiterEnable() {
		routes[apiRoute] = rateLimiter(s1, routes[apiRoute]) // API rate limiter
	}

	if err := setActionRoutes(s1, routes); err != nil {
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
		s.closeFn, err = enableObservability(s, mux)
		if err != nil {
			return nil, err
		}
	}

	return setServerHeader(mux), nil
}

func setActionRoutes(s1 *Service, routes map[string]http.Handler) error {
	var zlog *zap.Logger
	var err error
	s := s1.Load().(*service)

	if s.conf.Debug {
		zlog = s.zlog
	}

	for _, a := range s.conf.Actions {
		var fn http.Handler

		fn, err = newAction(s1, &a)
		if err != nil {
			break
		}

		p := path.Join(actionRoute, strings.ToLower(a.Name))

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
