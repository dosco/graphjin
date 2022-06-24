package serv

import (
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/dosco/graphjin/internal/common"
	"github.com/dosco/graphjin/serv/auth"
	"github.com/klauspost/compress/gzhttp"
	"go.uber.org/zap"
)

const (
	routeGraphQL = "/api/v1/graphql"
	routeREST    = "/api/v1/rest/{queryName}"
	actionRoute  = "/api/v1/actions"
	healthRoute  = "/health"
	// metricsRoute = "/metrics"
)

// func (s *service) isHealthEndpoint(r *http.Request) bool {
// 	p := r.URL.Path
// 	return p == healthRoute || p == metricsRoute ||
// 		(s.conf.Telemetry.Metrics.Endpoint != "" && p == s.conf.Telemetry.Metrics.Endpoint)
// }

type Mux interface {
	Handle(string, http.Handler)
	ServeHTTP(http.ResponseWriter, *http.Request)
}

func routesHandler(s1 *Service, mux Mux, ns nspace) (http.Handler, error) {
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
			mux.Handle("/*", http.FileServer(http.FS(webRoot)))
		}
	}

	// GraphQL / REST API
	h1 := apiV1Handler(s1, ns, s1.apiV1GraphQL(ns))
	h2 := apiV1Handler(s1, ns, s1.apiV1Rest(ns))

	if s.conf.rateLimiterEnable() {
		h1 = rateLimiter(s1, h1)
		h2 = rateLimiter(s1, h2)
	}

	if s.conf.HTTPGZip {
		if gz, err := gzhttp.NewWrapper(gzhttp.CompressionLevel(6)); err != nil {
			return nil, err
		} else {
			h1 = gz(h1)
			h2 = gz(h2)
		}
	}

	mux.Handle(routeGraphQL, h1)
	mux.Handle(routeREST, h2)

	// if s.conf.telemetryEnabled() {
	// 	if s.closeFn, err = enableObservability(s, mux); err != nil {
	// 		return nil, err
	// 	}
	// }

	return setServerHeader(mux), nil
}

func setActionRoutes(s1 *Service, mux Mux) error {
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

		if ac, ok := findAuth(s, a.AuthName); ok {
			authOpt := auth.Options{AuthFailBlock: s.conf.Serv.AuthFailBlock}
			useAuth, err := auth.NewAuth(ac, zlog, authOpt)
			if err != nil {
				s.log.Fatalf("actions: error initializing auth: %s", err)
			}
			h = useAuth(h)
		}

		mux.Handle(p, h)
	}
	return nil
}
