package serv

import (
	"net/http"

	"github.com/dosco/graphjin/auth/v3"
)

const (
	routeGraphQL   = "/api/v1/graphql"
	routeREST      = "/api/v1/rest/"
	routeWorkflows = "/api/v1/workflows/"
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
			mux.Handle("/", s1.WebUI("/", routeGraphQL))
		}

		// GraphQL / REST API
		if ns == nil {
			mux.Handle(routeGraphQL, s1.GraphQL(ah))
			mux.Handle(routeREST, s1.REST(ah))
			if s.conf.legacyDiscoveryEnabled() {
				mux.Handle(routeWorkflows, s1.Workflows(ah))
			}
			mux.Handle(routeOpenAPI, apiV1Handler(s1, nil, s1.OpenAPI(), ah))
		} else {
			mux.Handle(routeGraphQL, s1.GraphQLWithNS(ah, *ns))
			mux.Handle(routeREST, s1.RESTWithNS(ah, *ns))
			if s.conf.legacyDiscoveryEnabled() {
				mux.Handle(routeWorkflows, s1.WorkflowsWithNS(ah, *ns))
			}
			mux.Handle(routeOpenAPI, apiV1Handler(s1, ns, s1.OpenAPIWithNS(*ns), ah))
		}
	}

	// Keep the legacy workflow endpoint available in MCP-only mode only when
	// legacy discovery/action surfaces are explicitly enabled.
	if s.conf.MCP.Only && s.conf.legacyDiscoveryEnabled() {
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

	// Legacy Discovery API. In sources used this is disabled unless
	// mcp.legacy_discovery is enabled; use catalog/control-plane GraphQL instead.
	if s.conf.legacyDiscoveryEnabled() {
		discAuth, err := auth.NewAuthHandlerFunc(s.conf.Auth)
		if err != nil {
			s.log.Fatalf("api: error initializing discovery auth handler: %s", err)
		}
		mux.Handle(routeDiscovery, apiV1Handler(s1, ns, discoveryHandler(s1), discAuth))
		mux.Handle(routeDiscovery+"/", apiV1Handler(s1, ns, discoveryWildcardHandler(s1), discAuth))
	}

	// MCP (Model Context Protocol) API
	// Transport is implicit: HTTP service uses SSE/HTTP, CLI uses stdio via RunMCPStdio()
	// Auth: Uses same auth middleware as GraphQL/REST endpoints
	if !s.conf.mcpDisabled() {
		s.registerMCPOAuthMetadataRoutes(mux)
		mcpAuth, err := s.newMCPAuthHandler()
		if err != nil {
			s.log.Fatalf("api: error initializing MCP auth handler: %s", err)
		}
		// SSE transport for web-based integrations (with auth)
		mux.Handle(routeMCP, s1.MCPHandlerWithAuth(mcpAuth))
		// HTTP transport for stateless API integrations (with auth)
		mux.Handle(routeMCPMsg, s1.MCPMessageHandlerWithAuth(mcpAuth))
	}

	// Built-in OIDC login endpoints (only if auth_login.enabled)
	if s.authLogin != nil {
		s.authLogin.routes(mux)
	}

	return setServerHeader(mux), nil
}
