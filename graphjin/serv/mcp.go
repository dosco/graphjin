package serv

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/dosco/graphjin/auth/v3"
	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/serv/v3/internal/mcpcompat/mcp"
	"github.com/dosco/graphjin/serv/v3/internal/mcpcompat/server"
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
		tools = append(tools, "graphql_help")
		if conf.catalogToolsEnabled() {
			tools = append(tools, "query_catalog")
		}
		tools = append(tools, "execute_saved_query", "validate_where_clause")
		if conf.MCP.AllowRawQueries {
			tools = append(tools, "execute_graphql")
		}
	}
	// Mirror of registerConfigTools: dev-only, independent of agentOnlyMCP.
	if effectiveMode(conf) == modeDev {
		tools = append(tools, "get_current_config", "validate_config")
		if conf.MCP.AllowConfigUpdates {
			tools = append(tools, "update_current_config")
		}
	}
	if conf.agentEnabled() {
		tools = append(tools, "ask_graphjin_agent")
	}

	return tools
}

// mcpServer wraps the MCP server instance
type mcpServer struct {
	srv             *server.MCPServer
	service         *graphjinService
	ctx             context.Context // Auth context (user_id, user_role)
	readOnlyDBs     map[string]bool // snapshot from config at startup, immutable at runtime
	readOnlySources map[string]bool // file-configured source read_only veto, immutable at runtime
}

// newMCPServerWithContext creates a new MCP server with an auth context
func (s *graphjinService) newMCPServerWithContext(ctx context.Context) *mcpServer {
	var ms *mcpServer

	startupProfile := s.mcpStartupCapabilityProfile(ctx)
	mcpSrv := server.NewMCPServer(
		"graphjin",
		version,
		server.WithToolFilter(func(callCtx context.Context, tools []mcp.Tool) []mcp.Tool {
			if ms == nil {
				return tools
			}
			return ms.applyCallerToolMetadata(callCtx, tools)
		}),
		server.WithSubscribeHandler(
			func(callCtx context.Context, uri string) error {
				if ms == nil {
					return fmt.Errorf("MCP server is not ready")
				}
				return ms.subscribeWatchResource(callCtx, uri)
			},
			func(callCtx context.Context, uri string) error {
				if ms == nil {
					return fmt.Errorf("MCP server is not ready")
				}
				return ms.unsubscribeWatchResource(callCtx, uri)
			},
		),
		server.WithInstructions(mcpServerInstructions(s.conf, startupProfile)),
	)

	// Copy the startup policy captured before any MCP mutation. A new MCP
	// transport must not turn an earlier file-configured veto into writable.
	if s.startupReadOnlyDBs == nil || s.startupReadOnlySources == nil {
		s.captureStartupReadOnlyPolicy()
	}
	readOnlyDBs := cloneReadOnlyPolicy(s.startupReadOnlyDBs)
	readOnlySources := cloneReadOnlyPolicy(s.startupReadOnlySources)

	ms = &mcpServer{
		srv:             mcpSrv,
		service:         s,
		ctx:             ctx,
		readOnlyDBs:     readOnlyDBs,
		readOnlySources: readOnlySources,
	}

	// Register all MCP tools
	ms.registerTools()

	// Register MCP prompts
	ms.registerPrompts()

	// Register MCP resources
	ms.registerResources()

	return ms
}

func (s *graphjinService) captureStartupReadOnlyPolicy() {
	if s == nil || s.conf == nil {
		return
	}
	if s.startupReadOnlyDBs == nil {
		s.startupReadOnlyDBs = make(map[string]bool)
		for name, database := range s.conf.Core.Databases {
			if database.ReadOnly {
				s.startupReadOnlyDBs[name] = true
			}
		}
	}
	if s.startupReadOnlySources == nil {
		s.startupReadOnlySources = make(map[string]bool)
		for _, source := range s.conf.Core.Sources {
			if source.ReadOnly {
				s.startupReadOnlySources[canonicalSourcePolicyName(source.Name)] = true
			}
		}
	}
}

func cloneReadOnlyPolicy(in map[string]bool) map[string]bool {
	out := make(map[string]bool, len(in))
	for name, value := range in {
		out[name] = value
	}
	return out
}

type mcpHTTPTransportCache struct {
	handler *server.StreamableHTTPServer
	server  *mcpServer
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
	s.mcpHTTPMu.Lock()
	defer s.mcpHTTPMu.Unlock()

	if s.mcpHTTP == nil || s.mcpHTTP.handler == nil {
		mcpSrv := s.newMCPServerWithContext(context.Background())
		s.mcpHTTP = &mcpHTTPTransportCache{
			handler: server.NewDualStreamableHTTPServer(mcpSrv.srv),
			server:  mcpSrv,
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
	// Explicit mcp.include_tools_with_agent=false can hide primitives.
	if !ms.service.conf.agentOnlyMCP() {
		ms.registerCatalogTools()
		ms.registerExecutionTools()
		ms.registerSchemaTools()
	}
	// Config tools are dev-only but register independently of agentOnlyMCP:
	// external MCP clients keep config read/validate/update access alongside
	// the agent front door (the agent's own tool surface is unchanged).
	ms.registerConfigTools()
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
	return server.ServeStdio(ctx, mcpSrv.srv)
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
	// The Streamable HTTP GET channel remains open for server notifications.
	// It must not inherit the global 10-second server deadline while waiting
	// for a watch event or another asynchronous notification.
	if r != nil && ((r.Method == http.MethodGet && isSSERequest(r)) ||
		(r.Method == http.MethodPost && mcpRequestMethod(r) == "subscriptions/listen")) {
		clearStreamDeadline(w)
		return
	}

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

func mcpRequestMethod(r *http.Request) string {
	if r == nil || r.Body == nil {
		return ""
	}
	body, err := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(body))
	if err != nil {
		return ""
	}
	var req struct {
		Method string `json:"method"`
	}
	if json.Unmarshal(body, &req) != nil {
		return ""
	}
	return req.Method
}
