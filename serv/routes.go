package serv

import (
	"io/fs"
	"net/http"

	"github.com/dosco/graphjin/auth/v3"
	"github.com/klauspost/compress/gzhttp"
)

const (
	routeGraphQL = "/api/v1/graphql"
	routeREST    = "/api/v1/rest/*"
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

func routesHandler(s1 *Service, mux Mux, ns *string) (http.Handler, error) {
	s := s1.Load().(*service)

	// Healthcheck API
	mux.Handle(healthRoute, healthV1Handler(s1))

	if s.conf.HotDeploy {
		// Rollback Config API
		mux.Handle(RollbackRoute, adminRollbackHandler(s1))
		// Deploy Config API
		mux.Handle(DeployRoute, adminDeployHandler(s1))
	}

	if s.conf.WebUI {
		if webRoot, err := fs.Sub(webBuild, "web/build"); err != nil {
			return nil, err
		} else {
			mux.Handle("/*", http.FileServer(http.FS(webRoot)))
		}
	}

	ah, err := auth.NewAuthHandlerFunc(s.conf.Auth)
	if err != nil {
		s.log.Fatalf("api: error initializing auth handler: %s", err)
	}

	if s.conf.Auth.Development {
		s.log.Warn("api: auth.development=true this allows clients to bypass authentication")
	}

	// GraphQL / REST API
	h1 := apiV1Handler(s1, ns, s1.apiV1GraphQL(ns, ah), ah)
	h2 := apiV1Handler(s1, ns, s1.apiV1Rest(ns, ah), ah)

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
