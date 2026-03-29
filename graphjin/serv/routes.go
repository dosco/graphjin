package serv

import (
	"net/http"

	"github.com/dosco/graphjin/auth/v3"
)

const (
	routeGraphQL   = "/api/v1/graphql"
	routeREST      = "/api/v1/rest/*"
	routeWorkflows = "/api/v1/workflows/*"
	routeOpenAPI   = "/api/v1/openapi.json"
	routeMCP       = "/api/v1/mcp"
	routeMCPMsg    = "/api/v1/mcp/message"
	healthRoute    = "/health"
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
	// if s.conf.HotDeploy {
	// 	mux.Handle(RollbackRoute, adminRollbackHandler(s1))
	// 	mux.Handle(DeployRoute, adminDeployHandler(s1))
	// }

	// Skip GraphQL/REST/OpenAPI/WebUI in MCP-only mode.
	if !s.conf.MCP.Only {
		ah, err := auth.NewAuthHandlerFunc(s.conf.Auth)
		if err != nil {
			s.log.Fatalf("api: error initializing auth handler: %s", err)
		}

		if s.conf.Auth.Development {
			s.log.Warn("api: auth.development=true this allows clients to bypass authentication")
		}

		if s.conf.WebUI {
			mux.Handle("/*", s1.WebUI("/", routeGraphQL))

			// Admin API routes for Web UI
			mux.Handle("/api/v1/admin/tables", apiV1Handler(s1, ns, adminTablesHandler(s1), ah))
			mux.Handle("/api/v1/admin/tables/*", apiV1Handler(s1, ns, adminTableSchemaHandler(s1), ah))
			mux.Handle("/api/v1/admin/queries", apiV1Handler(s1, ns, adminQueriesHandler(s1), ah))
			mux.Handle("/api/v1/admin/queries/*", apiV1Handler(s1, ns, adminQueryDetailHandler(s1), ah))
			mux.Handle("/api/v1/admin/fragments", apiV1Handler(s1, ns, adminFragmentsHandler(s1), ah))
			mux.Handle("/api/v1/admin/config", apiV1Handler(s1, ns, adminConfigHandler(s1), ah))
			mux.Handle("/api/v1/admin/database", apiV1Handler(s1, ns, adminDatabaseHandler(s1), ah))
			mux.Handle("/api/v1/admin/databases", apiV1Handler(s1, ns, adminDatabasesHandler(s1), ah))
		}

		// GraphQL / REST API
		if ns == nil {
			mux.Handle(routeGraphQL, s1.GraphQL(ah))
			mux.Handle(routeREST, s1.REST(ah))
			mux.Handle(routeWorkflows, s1.Workflows(ah))
			mux.Handle(routeOpenAPI, apiV1Handler(s1, nil, s1.OpenAPI(), ah))
		} else {
			mux.Handle(routeGraphQL, s1.GraphQLWithNS(ah, *ns))
			mux.Handle(routeREST, s1.RESTWithNS(ah, *ns))
			mux.Handle(routeWorkflows, s1.WorkflowsWithNS(ah, *ns))
			mux.Handle(routeOpenAPI, apiV1Handler(s1, ns, s1.OpenAPIWithNS(*ns), ah))
		}
	}

	// Keep workflow endpoint available in MCP-only mode for JS orchestration.
	if s.conf.MCP.Only {
		ah, err := auth.NewAuthHandlerFunc(s.conf.Auth)
		if err != nil {
			s.log.Fatalf("api: error initializing auth handler: %s", err)
		}

		if s.conf.Auth.Development {
			s.log.Warn("api: auth.development=true this allows clients to bypass authentication")
		}

		if ns == nil {
			mux.Handle(routeWorkflows, s1.Workflows(ah))
		} else {
			mux.Handle(routeWorkflows, s1.WorkflowsWithNS(ah, *ns))
		}
	}

	// MCP (Model Context Protocol) API
	// Transport is implicit: HTTP service uses SSE/HTTP, CLI uses stdio via RunMCPStdio()
	// Auth: Uses same auth middleware as GraphQL/REST endpoints
	if !s.conf.MCP.Disable {
		mcpAuth, err := auth.NewAuthHandlerFunc(s.conf.Auth)
		if err != nil {
			s.log.Fatalf("api: error initializing MCP auth handler: %s", err)
		}
		// SSE transport for web-based integrations (with auth)
		mux.Handle(routeMCP, s1.MCPHandlerWithAuth(mcpAuth))
		// HTTP transport for stateless API integrations (with auth)
		mux.Handle(routeMCPMsg, s1.MCPMessageHandlerWithAuth(mcpAuth))
	}

	return setServerHeader(mux), nil
}
