package serv

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

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

// mcpToolResultJSONBytes returns a tool result with both JSON text fallback and
// StructuredContent so MCP clients can consume typed data without reparsing text.
func mcpToolResultJSONBytes(data []byte) *mcp.CallToolResult {
	var structured any
	if err := json.Unmarshal(data, &structured); err != nil {
		return mcp.NewToolResultText(string(data))
	}
	return mcp.NewToolResultStructured(structured, string(data))
}

func mcpInjectNextJSON(data []byte, next *NextGuidance) ([]byte, error) {
	if next == nil {
		return data, nil
	}

	var payload any
	if err := json.Unmarshal(data, &payload); err != nil {
		return data, nil
	}

	body, ok := payload.(map[string]any)
	if !ok {
		return data, nil
	}
	if _, exists := body["next"]; exists {
		return data, nil
	}

	body["next"] = next
	return mcpMarshalJSON(body, true)
}

func (ms *mcpServer) toolResultJSON(tool string, args map[string]any, payload any) (*mcp.CallToolResult, error) {
	data, err := mcpMarshalJSON(payload, true)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if next := ms.nextForToolCall(tool, args, payload); next != nil {
		data, err = mcpInjectNextJSON(data, next)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
	}

	return mcpToolResultJSONBytes(data), nil
}

// mcpToolList returns the names of MCP tools that will be registered
// based on the current configuration flags.
func mcpToolList(conf *Config) []string {
	if conf.mcpDisabled() {
		return nil
	}

	tools := []string{}
	// When the agent tool is the front door it orchestrates the primitives
	// internally, so they are hidden unless the operator opts in.
	if !conf.agentOnlyMCP() {
		tools = append(tools,
			"graphql_help",
			"query_catalog",
			"execute_saved_query",
			"validate_where_clause",
		)
		if conf.MCP.AllowRawQueries {
			tools = append(tools, "execute_graphql")
		}
	}
	if conf.agentEnabled() {
		tools = append(tools, "ask_graphjin_agent")
	}

	return tools
}

// mcpServer wraps the MCP server instance
type mcpServer struct {
	srv         *server.MCPServer
	service     *graphjinService
	ctx         context.Context // Auth context (user_id, user_role)
	readOnlyDBs map[string]bool // snapshot from config at startup, immutable at runtime
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

	var ms *mcpServer
	startupProfile := s.mcpStartupCapabilityProfile(ctx)
	mcpSrv := server.NewMCPServer(
		"graphjin",
		version,
		server.WithToolCapabilities(true),
		server.WithPromptCapabilities(true),
		server.WithResourceCapabilities(true, false),
		server.WithHooks(hooks),
		server.WithToolFilter(func(callCtx context.Context, tools []mcp.Tool) []mcp.Tool {
			if ms == nil {
				return tools
			}
			return ms.applyCallerToolMetadata(callCtx, tools)
		}),
		server.WithInstructions(mcpServerInstructions(s.conf, startupProfile)),
	)
	if samplingEnabledForConfig(s.conf) {
		mcpSrv.EnableSampling()
	}

	// Snapshot which databases are read-only from the config file.
	// This snapshot is immutable — MCP config updates cannot change it.
	readOnlyDBs := make(map[string]bool)
	for name, dbConf := range s.conf.Core.Databases {
		if dbConf.ReadOnly {
			readOnlyDBs[name] = true
		}
	}

	ms = &mcpServer{
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

type mcpHTTPTransportCache struct {
	handler *server.StreamableHTTPServer
}

func (s *graphjinService) closeMCPHTTPTransport() {
	if s == nil {
		return
	}
	s.mcpHTTPMu.Lock()
	cached := s.mcpHTTP
	s.mcpHTTP = nil
	s.mcpHTTPMu.Unlock()

	if cached == nil || cached.handler == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = cached.handler.Shutdown(ctx)
}

func (s *graphjinService) mcpHTTPTransport(ctx context.Context) *server.StreamableHTTPServer {
	if s == nil || s.conf == nil || !s.conf.MCP.HTTPStateful {
		mcpSrv := s.newMCPServerWithContext(ctx)
		return server.NewStreamableHTTPServer(mcpSrv.srv, server.WithStateLess(true))
	}

	s.mcpHTTPMu.Lock()
	defer s.mcpHTTPMu.Unlock()

	if s.mcpHTTP == nil || s.mcpHTTP.handler == nil {
		mcpSrv := s.newMCPServerWithContext(context.Background())
		s.mcpHTTP = &mcpHTTPTransportCache{
			handler: server.NewStreamableHTTPServer(mcpSrv.srv, server.WithStateful(true)),
		}
	}
	return s.mcpHTTP.handler
}

// registerTools registers all MCP tools with the server
func (ms *mcpServer) registerTools() {
	if ms.service.conf.mcpDisabled() {
		return
	}

	// When the agent tool is the front door, the primitives it orchestrates are
	// hidden by default; mcp.include_tools_with_agent opts them back in.
	if !ms.service.conf.agentOnlyMCP() {
		ms.registerCatalogTools()
		ms.registerExecutionTools()
		ms.registerSchemaTools()
	}
	ms.registerAgentTools()
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

	if s1.conf.mcpDisabled() {
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
	if userRole != "" && !core.IsReservedRoleName(userRole) {
		authCtx = context.WithValue(authCtx, core.UserRoleKey, userRole)
	}

	mcpSrv := s1.newMCPServerWithContext(authCtx)
	return server.ServeStdio(mcpSrv.srv)
}

// MCPHandler returns an HTTP handler for MCP HTTP transport
// This uses StreamableHTTPServer which handles POST requests directly
func (s *HttpService) MCPHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s1 := s.Load().(*graphjinService)

		if s1.conf.mcpDisabled() {
			http.Error(w, "MCP is disabled", http.StatusNotFound)
			return
		}

		extendDeadlineForMCPRequest(w, r, s1.conf)

		s1.mcpHTTPTransport(r.Context()).ServeHTTP(w, r)
	})
}

// MCPHandlerWithAuth returns an HTTP handler for MCP HTTP transport with authentication
// This wraps the MCP handler with the same auth middleware as GraphQL/REST endpoints
func (s *HttpService) MCPHandlerWithAuth(ah auth.HandlerFunc) http.Handler {
	return apiV1Handler(s, nil, s.MCPHandler(), ah)
}

// MCPMessageHandler returns an HTTP handler for MCP HTTP transport
// This uses StreamableHTTPServer which handles POST requests directly without SSE
func (s *HttpService) MCPMessageHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s1 := s.Load().(*graphjinService)

		if s1.conf.mcpDisabled() {
			http.Error(w, "MCP is disabled", http.StatusNotFound)
			return
		}

		// See MCPHandler — same WriteTimeout extension applies here.
		extendDeadlineForMCPRequest(w, r, s1.conf)

		s1.mcpHTTPTransport(r.Context()).ServeHTTP(w, r)
	})
}

// MCPMessageHandlerWithAuth returns an HTTP handler for MCP HTTP transport with authentication
func (s *HttpService) MCPMessageHandlerWithAuth(ah auth.HandlerFunc) http.Handler {
	return apiV1Handler(s, nil, s.MCPMessageHandler(), ah)
}

// extendDeadlineForMCPRequest lifts per-request deadlines for long-running MCP
// tool calls. Workflow calls use mcp.workflow_timeout; ask_graphjin_agent can
// run up to agent.timeout_seconds while waiting on an LLM provider.
func extendDeadlineForMCPRequest(w http.ResponseWriter, r *http.Request, conf *Config) {
	extendDeadlineForWorkflow(w, conf)
	if conf != nil && conf.agentEnabled() && mcpRequestCallsTool(r, mcpToolAskGraphJinAgent) {
		extendDeadlineForAgent(w, conf)
	}
}

func mcpRequestCallsTool(r *http.Request, tool string) bool {
	if r == nil || r.Body == nil || r.Method != http.MethodPost || strings.TrimSpace(tool) == "" {
		return false
	}
	body, err := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(body))
	if err != nil {
		return false
	}

	var req struct {
		Method string `json:"method"`
		Params struct {
			Name string `json:"name"`
		} `json:"params"`
	}
	if err := json.Unmarshal(body, &req); err != nil || req.Method != "tools/call" {
		return false
	}
	name := strings.TrimSpace(req.Params.Name)
	if idx := strings.LastIndex(name, ":"); idx != -1 {
		name = name[idx+1:]
	}
	return name == tool
}
