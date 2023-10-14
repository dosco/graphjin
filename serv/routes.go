package serv

import (
	"net/http"

	"github.com/dosco/graphjin/auth/v3"
)

const (
	routeGraphQL = "/api/v1/graphql"
	routeREST    = "/api/v1/rest/*"
	healthRoute  = "/health"
)

type Mux interface {
	Handle(string, http.Handler)
	ServeHTTP(http.ResponseWriter, *http.Request)
}

func routesHandler(s1 *Service, mux Mux, ns *string) (http.Handler, error) {
	s := s1.Load().(*service)

	// Healthcheck API
	mux.Handle(healthRoute, healthCheckHandler(s1))

	// Hot deploy API
	if s.conf.HotDeploy {
		mux.Handle(RollbackRoute, adminRollbackHandler(s1))
		mux.Handle(DeployRoute, adminDeployHandler(s1))
	}

	if s.conf.WebUI {
		mux.Handle("/*", s1.WebUI("/", routeGraphQL))
	}

	ah, err := auth.NewAuthHandlerFunc(s.conf.Auth)
	if err != nil {
		s.log.Fatalf("api: error initializing auth handler: %s", err)
	}

	if s.conf.Auth.Development {
		s.log.Warn("api: auth.development=true this allows clients to bypass authentication")
	}

	// GraphQL / REST API
	if ns == nil {
		mux.Handle(routeGraphQL, s1.GraphQL(ah))
		mux.Handle(routeREST, s1.REST(ah))
	} else {
		mux.Handle(routeGraphQL, s1.GraphQLWithNS(ah, *ns))
		mux.Handle(routeREST, s1.RESTWithNS(ah, *ns))
	}

	return setServerHeader(mux), nil
}
