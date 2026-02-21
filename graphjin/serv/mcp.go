package serv

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"github.com/dosco/graphjin/auth/v3"
	"github.com/dosco/graphjin/core/v3"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// mcpMarshalJSON marshals data to JSON without HTML escaping.
// This ensures characters like <, >, and & are not converted to Unicode escapes
// (e.g., \u003c, \u003e, \u0026) making output more readable for LLM clients.
func mcpMarshalJSON(v any, indent bool) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if indent {
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	// Encode adds a trailing newline; trim it to match MarshalIndent behavior
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// mcpToolList returns the names of MCP tools that will be registered
// based on the current configuration flags.
func mcpToolList(conf *Config) []string {
	if conf.MCP.Disable {
		return nil
	}

	// Always-on tools
	tools := []string{
		"get_query_syntax",
		"get_mutation_syntax",
		"list_tables",
		"describe_table",
		"find_path",
		"validate_where_clause",
		"get_workflow_guide",
		"explore_relationships",
		"execute_saved_query",
	}

	// Conditionally registered
	if !conf.Serv.Production {
		tools = append(tools, "get_current_config")
	}
	if conf.MCP.AllowRawQueries {
		tools = append(tools, "execute_graphql")
	}
	if conf.MCP.EnableSearch {
		tools = append(tools,
			"list_saved_queries", "search_saved_queries", "get_saved_query",
			"list_fragments", "get_fragment", "search_fragments",
		)
	}
	if conf.MCP.AllowConfigUpdates {
		tools = append(tools, "update_current_config")
	}
	if conf.MCP.AllowSchemaReload {
		tools = append(tools, "reload_schema")
	}
	if conf.MCP.AllowSchemaUpdates {
		tools = append(tools, "preview_schema_changes", "apply_schema_changes")
	}
	if conf.MCP.AllowDevTools {
		tools = append(tools, "explain_query", "audit_role_permissions", "discover_databases",
			"list_databases", "check_health", "plan_database_setup",
			"test_database_connection", "get_onboarding_status")
	}
	if conf.MCP.AllowDevTools && conf.MCP.AllowConfigUpdates {
		tools = append(tools, "apply_database_setup")
	}

	return tools
}

// mcpServer wraps the MCP server instance
type mcpServer struct {
	srv         *server.MCPServer
	service     *graphjinService
	ctx         context.Context    // Auth context (user_id, user_role)
	readOnlyDBs map[string]bool    // snapshot from config at startup, immutable at runtime
}

// newMCPServerWithContext creates a new MCP server with an auth context
func (s *graphjinService) newMCPServerWithContext(ctx context.Context) *mcpServer {
	// Create hooks to handle prefixed tool names from Claude Desktop
	// Claude Desktop may prefix tool names with "server_name:" when calling tools
	hooks := &server.Hooks{}
	hooks.AddBeforeCallTool(func(ctx context.Context, id any, req *mcp.CallToolRequest) {
		// Strip any "server_name:" prefix from tool name
		// e.g., "webshop-development:list_tables" -> "list_tables"
		if idx := strings.LastIndex(req.Params.Name, ":"); idx != -1 {
			req.Params.Name = req.Params.Name[idx+1:]
		}
	})

	mcpSrv := server.NewMCPServer(
		"graphjin",
		version,
		server.WithToolCapabilities(true),
		server.WithPromptCapabilities(true),
		server.WithResourceCapabilities(false, false),
		server.WithHooks(hooks),
	)

	// Snapshot which databases are read-only from the config file.
	// This snapshot is immutable — MCP config updates cannot change it.
	readOnlyDBs := make(map[string]bool)
	for name, dbConf := range s.conf.Core.Databases {
		if dbConf.ReadOnly {
			readOnlyDBs[name] = true
		}
	}

	ms := &mcpServer{
		srv:         mcpSrv,
		service:     s,
		ctx:         ctx,
		readOnlyDBs: readOnlyDBs,
	}

	// Register all MCP tools
	ms.registerTools()

	// Register MCP prompts
	ms.registerPrompts()

	// Register MCP resources
	ms.registerResources()

	return ms
}

// registerTools registers all MCP tools with the server
func (ms *mcpServer) registerTools() {
	// Syntax Reference Tools (call these first!)
	ms.registerSyntaxTools()

	// Schema Discovery Tools
	ms.registerSchemaTools()
	ms.registerExploreTools()

	// Query Execution Tools
	ms.registerExecutionTools()

	// Saved Query Discovery Tools
	ms.registerQueryDiscoveryTools()

	// Fragment Discovery Tools
	ms.registerFragmentTools()

	// Configuration Update Tools (conditionally registered)
	ms.registerConfigTools()

	// DDL Tools - schema modifications (conditionally registered)
	ms.registerDDLTools()

	// Dev Tools - advanced introspection (conditionally registered)
	ms.registerExplainTools()
	ms.registerAuditTools()
	ms.registerDiscoverTools()
	ms.registerHealthTools()
	ms.registerOnboardingTools()
}

// isDBReadOnly checks the startup snapshot to determine if a database is read-only.
// This checks the immutable snapshot, not the current config, so runtime config
// changes by MCP tools cannot bypass the read-only flag.
func (ms *mcpServer) isDBReadOnly(database string) bool {
	return ms.readOnlyDBs[database]
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

// MCPHandler returns an HTTP handler for MCP HTTP transport (stateless)
// This uses StreamableHTTPServer which handles POST requests directly
func (s *HttpService) MCPHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s1 := s.Load().(*graphjinService)

		if s1.conf.MCP.Disable {
			http.Error(w, "MCP is disabled", http.StatusNotFound)
			return
		}

		// Use request context (may contain auth info from middleware)
		mcpSrv := s1.newMCPServerWithContext(r.Context())
		// Use StreamableHTTPServer with stateless mode
		httpServer := server.NewStreamableHTTPServer(mcpSrv.srv, server.WithStateLess(true))
		httpServer.ServeHTTP(w, r)
	})
}

// MCPHandlerWithAuth returns an HTTP handler for MCP HTTP transport with authentication
// This wraps the MCP handler with the same auth middleware as GraphQL/REST endpoints
func (s *HttpService) MCPHandlerWithAuth(ah auth.HandlerFunc) http.Handler {
	return apiV1Handler(s, nil, s.MCPHandler(), ah)
}

// MCPMessageHandler returns an HTTP handler for MCP HTTP transport (stateless)
// This uses StreamableHTTPServer which handles POST requests directly without SSE
func (s *HttpService) MCPMessageHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s1 := s.Load().(*graphjinService)

		if s1.conf.MCP.Disable {
			http.Error(w, "MCP is disabled", http.StatusNotFound)
			return
		}

		// Use request context (may contain auth info from middleware)
		mcpSrv := s1.newMCPServerWithContext(r.Context())
		// Use StreamableHTTPServer with stateless mode for the HTTP transport
		// This handles POST requests directly without requiring an SSE session
		httpServer := server.NewStreamableHTTPServer(mcpSrv.srv, server.WithStateLess(true))
		httpServer.ServeHTTP(w, r)
	})
}

// MCPMessageHandlerWithAuth returns an HTTP handler for MCP HTTP transport with authentication
func (s *HttpService) MCPMessageHandlerWithAuth(ah auth.HandlerFunc) http.Handler {
	return apiV1Handler(s, nil, s.MCPMessageHandler(), ah)
}
