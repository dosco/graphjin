package serv

import (
	"net/http"

	"github.com/dosco/graphjin/auth/v3"
)

const (
	routeGraphQL = "/api/v1/graphql"
	routeREST    = "/api/v1/rest/*"
	routeOpenAPI = "/api/v1/openapi.json"
	routeMCP     = "/api/v1/mcp"
	routeMCPMsg  = "/api/v1/mcp/message"
	healthRoute  = "/health"
)

type Mux interface {
	Handle(string, http.Handler)
	ServeHTTP(http.ResponseWriter, *http.Request)
}

// routesHandler is the main handler for all routes
func routesHandler(s1 *HttpService, mux Mux, ns *string) (http.Handler, error) {
	s := s1.Load().(*graphjinService)

	// Healthcheck API
	mux.Handle(healthRoute, healthCheckHandler(s1))

	// Hot deploy API
	if s.conf.HotDeploy {
		mux.Handle(RollbackRoute, adminRollbackHandler(s1))
		mux.Handle(DeployRoute, adminDeployHandler(s1))
	}

	if s.conf.WebUI {
		mux.Handle("/*", s1.WebUI("/", routeGraphQL))

		// Admin API routes for Web UI
		mux.Handle("/api/v1/admin/tables", adminTablesHandler(s1))
		mux.Handle("/api/v1/admin/tables/*", adminTableSchemaHandler(s1))
		mux.Handle("/api/v1/admin/queries", adminQueriesHandler(s1))
		mux.Handle("/api/v1/admin/queries/*", adminQueryDetailHandler(s1))
		mux.Handle("/api/v1/admin/fragments", adminFragmentsHandler(s1))
		mux.Handle("/api/v1/admin/config", adminConfigHandler(s1))
		mux.Handle("/api/v1/admin/database", adminDatabaseHandler(s1))
		mux.Handle("/api/v1/admin/databases", adminDatabasesHandler(s1))
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
		mux.Handle(routeOpenAPI, s1.OpenAPI())
	} else {
		mux.Handle(routeGraphQL, s1.GraphQLWithNS(ah, *ns))
		mux.Handle(routeREST, s1.RESTWithNS(ah, *ns))
		mux.Handle(routeOpenAPI, s1.OpenAPIWithNS(*ns))
	}

	// MCP (Model Context Protocol) API
	// Transport is implicit: HTTP service uses SSE/HTTP, CLI uses stdio via RunMCPStdio()
	// Auth: Uses same auth middleware as GraphQL/REST endpoints
	if !s.conf.MCP.Disable {
		// SSE transport for web-based integrations (with auth)
		mux.Handle(routeMCP, s1.MCPHandlerWithAuth(ah))
		// HTTP transport for stateless API integrations (with auth)
		mux.Handle(routeMCPMsg, s1.MCPMessageHandlerWithAuth(ah))
	}

	return setServerHeader(mux), nil
}
