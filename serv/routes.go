package serv

import (
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/dosco/graphjin/internal/common"
	"github.com/dosco/graphjin/serv/internal/auth"
	"github.com/klauspost/compress/gzhttp"
	"go.opencensus.io/plugin/ochttp"
	"go.uber.org/zap"
)

const (
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

	// Healthcheck API
	mux.Handle(healthRoute, healthV1Handler(s1))

	if s.conf.HotDeploy {
		// Rollback Config API
		mux.Handle(common.RollbackRoute, adminRollbackHandler(s1))
		// Deploy Config API
		mux.Handle(common.DeployRoute, adminDeployHandler(s1))
	}

	if err := setActionRoutes(s1, mux); err != nil {
		return nil, err
	}

	if s.conf.WebUI {
		if webRoot, err := fs.Sub(webBuild, "web/build"); err != nil {
			return nil, err
		} else {
			mux.Handle("/", http.FileServer(http.FS(webRoot)))
		}
	}

	// Main GraphQL API
	h := apiV1Handler(s1)

	if s.conf.rateLimiterEnable() {
		h = rateLimiter(s1, h)
	}

	if s.conf.HTTPGZip {
		if gz, err := gzhttp.NewWrapper(gzhttp.CompressionLevel(6)); err != nil {
			return nil, err
		} else {
			h = gz(h)
		}
	}

	mux.Handle(apiRoute, h)

	if s.conf.telemetryEnabled() {
		if s.closeFn, err = enableObservability(s, mux); err != nil {
			return nil, err
		}
	}

	return setServerHeader(mux), nil
}

func setActionRoutes(s1 *Service, mux *http.ServeMux) error {
	var zlog *zap.Logger
	var err error
	s := s1.Load().(*service)

	if s.conf.Core.Debug {
		zlog = s.zlog
	}

	for i, a := range s.conf.Serv.Actions {
		var fn http.Handler

		fn, err = newAction(s1, &s.conf.Serv.Actions[i])
		if err != nil {
			break
		}

		p := path.Join(actionRoute, strings.ToLower(a.Name))
		h := fn

		if s.conf.telemetryEnabled() {
			h = ochttp.WithRouteTag(h, p)
		}

		if ac := findAuth(s, a.AuthName); ac != nil {
			h, err = auth.WithAuth(h, ac, zlog)
		}

		if err != nil {
			return err
		}

		mux.Handle(p, h)
	}
	return nil
}
