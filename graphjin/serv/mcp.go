package serv

import (
	"context"
	"net/http"
	"os"

	"github.com/dosco/graphjin/auth/v3"
	"github.com/dosco/graphjin/core/v3"
	"github.com/mark3labs/mcp-go/server"
)

// mcpServer wraps the MCP server instance
type mcpServer struct {
	srv     *server.MCPServer
	service *graphjinService
	ctx     context.Context // Auth context (user_id, user_role)
}

// newMCPServer creates a new MCP server for the graphjin service
func (s *graphjinService) newMCPServer() *mcpServer {
	return s.newMCPServerWithContext(context.Background())
}

// newMCPServerWithContext creates a new MCP server with an auth context
func (s *graphjinService) newMCPServerWithContext(ctx context.Context) *mcpServer {
	mcpSrv := server.NewMCPServer(
		"graphjin",
		version,
		server.WithToolCapabilities(true),
		server.WithPromptCapabilities(true),
	)

	ms := &mcpServer{
		srv:     mcpSrv,
		service: s,
		ctx:     ctx,
	}

	// Register all MCP tools
	ms.registerTools()

	// Register MCP prompts
	ms.registerPrompts()

	return ms
}

// registerTools registers all MCP tools with the server
func (ms *mcpServer) registerTools() {
	// Syntax Reference Tools (call these first!)
	ms.registerSyntaxTools()

	// Schema Discovery Tools
	ms.registerSchemaTools()

	// Query Execution Tools
	ms.registerExecutionTools()

	// Saved Query Discovery Tools
	ms.registerQueryDiscoveryTools()

	// Fragment Discovery Tools
	ms.registerFragmentTools()
}

// RunMCPStdio runs the MCP server using stdio transport (for CLI/Claude Desktop)
// Auth credentials can be provided via environment variables:
// - GRAPHJIN_USER_ID: User ID for the session
// - GRAPHJIN_USER_ROLE: User role for the session
func (s *HttpService) RunMCPStdio(ctx context.Context) error {
	s1 := s.Load().(*graphjinService)

	if s1.conf.MCP.Disable {
		s1.log.Warn("MCP is disabled in configuration")
	}

	// Build auth context from environment variables or config
	authCtx := ctx

	// Try environment variables first
	userID := os.Getenv("GRAPHJIN_USER_ID")
	userRole := os.Getenv("GRAPHJIN_USER_ROLE")

	// Fall back to config values if env vars not set
	if userID == "" && s1.conf.MCP.StdioUserID != "" {
		userID = s1.conf.MCP.StdioUserID
	}
	if userRole == "" && s1.conf.MCP.StdioUserRole != "" {
		userRole = s1.conf.MCP.StdioUserRole
	}

	// Set context values if provided
	if userID != "" {
		authCtx = context.WithValue(authCtx, core.UserIDKey, userID)
	}
	if userRole != "" {
		authCtx = context.WithValue(authCtx, core.UserRoleKey, userRole)
	}

	mcpSrv := s1.newMCPServerWithContext(authCtx)
	return server.ServeStdio(mcpSrv.srv)
}

// MCPHandler returns an HTTP handler for MCP SSE transport (without auth)
// For authenticated MCP, use MCPHandlerWithAuth instead
func (s *HttpService) MCPHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s1 := s.Load().(*graphjinService)

		if s1.conf.MCP.Disable {
			http.Error(w, "MCP is disabled", http.StatusNotFound)
			return
		}

		// Use request context (may contain auth info from middleware)
		mcpSrv := s1.newMCPServerWithContext(r.Context())
		sseServer := server.NewSSEServer(mcpSrv.srv)
		sseServer.ServeHTTP(w, r)
	})
}

// MCPHandlerWithAuth returns an HTTP handler for MCP SSE transport with authentication
// This wraps the MCP handler with the same auth middleware as GraphQL/REST endpoints
func (s *HttpService) MCPHandlerWithAuth(ah auth.HandlerFunc) http.Handler {
	return apiV1Handler(s, nil, s.MCPHandler(), ah)
}

// MCPMessageHandler returns an HTTP handler for MCP HTTP transport (streamable)
// Note: This uses the SSE server which handles both SSE and regular HTTP requests
func (s *HttpService) MCPMessageHandler() http.Handler {
	return s.MCPHandler()
}

// MCPMessageHandlerWithAuth returns an HTTP handler for MCP HTTP transport with authentication
func (s *HttpService) MCPMessageHandlerWithAuth(ah auth.HandlerFunc) http.Handler {
	return apiV1Handler(s, nil, s.MCPMessageHandler(), ah)
}

// mcpEnabled returns true if MCP is enabled (enabled by default)
func (s *graphjinService) mcpEnabled() bool {
	return !s.conf.MCP.Disable
}
