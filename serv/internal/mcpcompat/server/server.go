// Package server adapts GraphJin's stable internal MCP registration surface to
// the official modelcontextprotocol/go-sdk server and transports.
package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"

	compat "github.com/dosco/graphjin/serv/v3/internal/mcpcompat/mcp"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type ToolHandler func(context.Context, compat.CallToolRequest) (*compat.CallToolResult, error)
type ResourceHandler func(context.Context, compat.ReadResourceRequest) ([]compat.ResourceContents, error)
type PromptHandler func(context.Context, compat.GetPromptRequest) (*compat.GetPromptResult, error)
type ToolFilter func(context.Context, []compat.Tool) []compat.Tool
type SubscribeHandler func(context.Context, string) error

type ServerTool struct {
	Tool    compat.Tool
	Handler ToolHandler
}

type serverOptions struct {
	instructions       string
	toolFilter         ToolFilter
	subscribeHandler   SubscribeHandler
	unsubscribeHandler SubscribeHandler
}

type ServerOption func(*serverOptions)

// These compatibility options are retained for GraphJin's focused registration
// tests. The official SDK infers the capabilities from registered features.
func WithPromptCapabilities(bool) ServerOption { return func(*serverOptions) {} }
func WithResourceCapabilities(bool, bool) ServerOption {
	return func(*serverOptions) {}
}

func WithInstructions(value string) ServerOption {
	return func(o *serverOptions) { o.instructions = value }
}

func WithToolFilter(filter ToolFilter) ServerOption {
	return func(o *serverOptions) { o.toolFilter = filter }
}

func WithSubscribeHandler(subscribe, unsubscribe SubscribeHandler) ServerOption {
	return func(o *serverOptions) {
		o.subscribeHandler = subscribe
		o.unsubscribeHandler = unsubscribe
	}
}

type MCPServer struct {
	sdk        *sdk.Server
	toolFilter ToolFilter
	mu         sync.RWMutex
	tools      map[string]*ServerTool
}

type sessionContextKey struct{}

func NewMCPServer(name, version string, opts ...ServerOption) *MCPServer {
	var cfg serverOptions
	for _, opt := range opts {
		opt(&cfg)
	}
	sdkOpts := &sdk.ServerOptions{
		Instructions: cfg.instructions,
		// Supplying an explicit capability set prevents the deprecated logging
		// capability from being advertised. Tools/resources/prompts are inferred.
		Capabilities: &sdk.ServerCapabilities{},
	}
	if cfg.subscribeHandler != nil && cfg.unsubscribeHandler != nil {
		sdkOpts.SubscribeHandler = func(ctx context.Context, req *sdk.SubscribeRequest) error {
			return cfg.subscribeHandler(ctx, req.Params.URI)
		}
		sdkOpts.UnsubscribeHandler = func(ctx context.Context, req *sdk.UnsubscribeRequest) error {
			return cfg.unsubscribeHandler(ctx, req.Params.URI)
		}
	}
	s := &MCPServer{
		sdk:        sdk.NewServer(&sdk.Implementation{Name: name, Version: version}, sdkOpts),
		toolFilter: cfg.toolFilter,
		tools:      map[string]*ServerTool{},
	}
	s.sdk.AddReceivingMiddleware(s.receivingMiddleware)
	return s
}

func (s *MCPServer) receivingMiddleware(next sdk.MethodHandler) sdk.MethodHandler {
	return func(ctx context.Context, method string, req sdk.Request) (sdk.Result, error) {
		if method == "tools/call" {
			if call, ok := req.(*sdk.CallToolRequest); ok {
				if idx := strings.LastIndex(call.Params.Name, ":"); idx >= 0 {
					call.Params.Name = call.Params.Name[idx+1:]
				}
			}
		}
		result, err := next(ctx, method, req)
		if err != nil || result == nil {
			return result, err
		}
		switch r := result.(type) {
		case *sdk.ListToolsResult:
			if s.toolFilter != nil {
				tools := make([]compat.Tool, 0, len(r.Tools))
				for _, tool := range r.Tools {
					tools = append(tools, toolFromSDK(tool))
				}
				filtered := s.toolFilter(ctx, tools)
				r.Tools = make([]*sdk.Tool, 0, len(filtered))
				for i := range filtered {
					r.Tools = append(r.Tools, toolToSDK(filtered[i]))
				}
			}
			privateCache(&r.Cacheable)
		case *sdk.ListPromptsResult:
			privateCache(&r.Cacheable)
		case *sdk.ListResourcesResult:
			privateCache(&r.Cacheable)
		case *sdk.ListResourceTemplatesResult:
			privateCache(&r.Cacheable)
		case *sdk.ReadResourceResult:
			privateCache(&r.Cacheable)
		case *sdk.DiscoverResult:
			privateCache(&r.Cacheable)
		}
		return result, nil
	}
}

func privateCache(cache *sdk.Cacheable) {
	cache.TTLMs = 0
	cache.CacheScope = "private"
}

func (s *MCPServer) AddTool(tool compat.Tool, handler ToolHandler) {
	s.mu.Lock()
	s.tools[tool.Name] = &ServerTool{Tool: tool, Handler: handler}
	s.mu.Unlock()

	official := toolToSDK(tool)
	sdk.AddTool[map[string]any, any](s.sdk, official,
		func(ctx context.Context, req *sdk.CallToolRequest, args map[string]any) (*sdk.CallToolResult, any, error) {
			ctx = context.WithValue(ctx, sessionContextKey{}, req.Session)
			result, err := handler(ctx, compat.CallToolRequest{Params: compat.CallToolParams{
				Name: req.Params.Name, Arguments: args, Meta: compat.NewMetaFromMap(req.Params.Meta),
			}})
			if err != nil {
				return nil, nil, err
			}
			officialResult := resultToSDK(result)
			structured := officialResult.StructuredContent
			// Return structured data through the typed handler's Out value so the
			// official SDK validates it against the registered output schema.
			officialResult.StructuredContent = nil
			return officialResult, structured, nil
		})
}

func (s *MCPServer) ListTools() map[string]*ServerTool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]*ServerTool, len(s.tools))
	for name, tool := range s.tools {
		copy := *tool
		out[name] = &copy
	}
	return out
}

func (s *MCPServer) AddResource(resource compat.Resource, handler ResourceHandler) {
	s.sdk.AddResource(&sdk.Resource{
		URI: resource.URI, Name: resource.Name, Description: resource.Description, MIMEType: resource.MIMEType,
	}, func(ctx context.Context, req *sdk.ReadResourceRequest) (*sdk.ReadResourceResult, error) {
		contents, err := handler(ctx, compat.ReadResourceRequest{Params: compat.ReadResourceParams{URI: req.Params.URI}})
		if err != nil {
			return nil, err
		}
		return &sdk.ReadResourceResult{Cacheable: sdk.Cacheable{TTLMs: 0, CacheScope: "private"}, Contents: resourceContentsToSDK(contents)}, nil
	})
}

func (s *MCPServer) AddResourceTemplate(resource compat.ResourceTemplate, handler ResourceHandler) {
	s.sdk.AddResourceTemplate(&sdk.ResourceTemplate{
		URITemplate: resource.URITemplate, Name: resource.Name, Description: resource.Description, MIMEType: resource.MIMEType,
	}, func(ctx context.Context, req *sdk.ReadResourceRequest) (*sdk.ReadResourceResult, error) {
		contents, err := handler(ctx, compat.ReadResourceRequest{Params: compat.ReadResourceParams{URI: req.Params.URI}})
		if err != nil {
			return nil, err
		}
		return &sdk.ReadResourceResult{Cacheable: sdk.Cacheable{TTLMs: 0, CacheScope: "private"}, Contents: resourceContentsToSDK(contents)}, nil
	})
}

func (s *MCPServer) AddPrompt(prompt *sdk.Prompt, handler PromptHandler) {
	s.sdk.AddPrompt(prompt, func(ctx context.Context, req *sdk.GetPromptRequest) (*sdk.GetPromptResult, error) {
		result, err := handler(ctx, compat.GetPromptRequest{Params: compat.GetPromptParams{Name: req.Params.Name, Arguments: req.Params.Arguments}})
		if err != nil {
			return nil, err
		}
		messages := make([]*sdk.PromptMessage, 0, len(result.Messages))
		for _, message := range result.Messages {
			var content sdk.Content
			if text, ok := message.Content.(compat.TextContent); ok {
				content = &sdk.TextContent{Text: text.Text}
			}
			messages = append(messages, &sdk.PromptMessage{Role: sdk.Role(message.Role), Content: content})
		}
		return &sdk.GetPromptResult{Description: result.Description, Messages: messages}, nil
	})
}

func (s *MCPServer) SendNotificationToClient(ctx context.Context, method string, params map[string]any) error {
	if method != "notifications/progress" {
		return errors.New("unsupported request-scoped notification")
	}
	session, _ := ctx.Value(sessionContextKey{}).(*sdk.ServerSession)
	if session == nil {
		return errors.New("MCP session is unavailable")
	}
	progress, _ := number(params["progress"])
	total, _ := number(params["total"])
	message, _ := params["message"].(string)
	return session.NotifyProgress(ctx, &sdk.ProgressNotificationParams{
		ProgressToken: params["progressToken"], Progress: progress, Total: total, Message: message,
	})
}

func (s *MCPServer) ResourceUpdated(ctx context.Context, uri string) error {
	return s.sdk.ResourceUpdated(ctx, &sdk.ResourceUpdatedNotificationParams{URI: uri})
}

func (s *MCPServer) SDK() *sdk.Server { return s.sdk }

// ToolFromSDK converts an official SDK tool into GraphJin's stable internal
// representation. It is exported for contract tests that exercise the wire
// catalog through an official client.
func ToolFromSDK(tool *sdk.Tool) compat.Tool { return toolFromSDK(tool) }

func toolToSDK(tool compat.Tool) *sdk.Tool {
	annotations := &sdk.ToolAnnotations{Title: tool.Annotations.Title}
	if tool.Annotations.ReadOnlyHint != nil {
		annotations.ReadOnlyHint = *tool.Annotations.ReadOnlyHint
	}
	if tool.Annotations.IdempotentHint != nil {
		annotations.IdempotentHint = *tool.Annotations.IdempotentHint
	}
	annotations.DestructiveHint = tool.Annotations.DestructiveHint
	annotations.OpenWorldHint = tool.Annotations.OpenWorldHint
	var output any
	if tool.OutputSchema.Type != "" {
		output = tool.OutputSchema
	}
	return &sdk.Tool{
		Name: tool.Name, Description: tool.Description, InputSchema: tool.InputSchema,
		OutputSchema: output, Annotations: annotations, Meta: sdk.Meta(tool.Meta.Map()),
	}
}

func toolFromSDK(tool *sdk.Tool) compat.Tool {
	var input compat.ToolInputSchema
	data, _ := json.Marshal(tool.InputSchema)
	_ = json.Unmarshal(data, &input)
	var output compat.ToolOutputSchema
	data, _ = json.Marshal(tool.OutputSchema)
	_ = json.Unmarshal(data, &output)
	out := compat.Tool{
		Name: tool.Name, Description: tool.Description, InputSchema: input,
		OutputSchema: output, Meta: compat.NewMetaFromMap(tool.Meta),
	}
	if tool.Annotations != nil {
		out.Annotations = compat.ToolAnnotation{
			Title: tool.Annotations.Title, ReadOnlyHint: boolPointer(tool.Annotations.ReadOnlyHint),
			IdempotentHint:  boolPointer(tool.Annotations.IdempotentHint),
			DestructiveHint: tool.Annotations.DestructiveHint, OpenWorldHint: tool.Annotations.OpenWorldHint,
		}
	}
	return out
}

func boolPointer(value bool) *bool { return &value }

func resultToSDK(result *compat.CallToolResult) *sdk.CallToolResult {
	if result == nil {
		return &sdk.CallToolResult{}
	}
	contents := make([]sdk.Content, 0, len(result.Content))
	for _, content := range result.Content {
		if text, ok := content.(compat.TextContent); ok {
			contents = append(contents, &sdk.TextContent{Text: text.Text})
		}
	}
	return &sdk.CallToolResult{Content: contents, StructuredContent: result.StructuredContent, IsError: result.IsError}
}

func resourceContentsToSDK(contents []compat.ResourceContents) []*sdk.ResourceContents {
	out := make([]*sdk.ResourceContents, 0, len(contents))
	for _, content := range contents {
		if text, ok := content.(compat.TextResourceContents); ok {
			out = append(out, &sdk.ResourceContents{URI: text.URI, MIMEType: text.MIMEType, Text: text.Text})
		}
	}
	return out
}

func number(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	default:
		return 0, false
	}
}

type StreamableHTTPServer struct {
	modern http.Handler
	legacy http.Handler
}

func NewDualStreamableHTTPServer(server *MCPServer) *StreamableHTTPServer {
	getServer := func(*http.Request) *sdk.Server { return server.sdk }
	return &StreamableHTTPServer{
		modern: sdk.NewStreamableHTTPHandler(getServer, &sdk.StreamableHTTPOptions{
			Stateless: true, PropagateRequestCancellation: true,
		}),
		legacy: sdk.NewStreamableHTTPHandler(getServer, &sdk.StreamableHTTPOptions{Stateless: false}),
	}
}

func (s *StreamableHTTPServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	modern := isModernRequest(r)
	if !modern {
		if id, removed := methodRequest(r, "sampling/createMessage"); removed {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"error": map[string]any{
					"code": -32601, "message": "Method not found",
				},
			})
			return
		}
	}
	// AxMCPEventSource correctly uses an SSE response stream for
	// subscriptions/listen, but its 23.0.9 transport sends the narrower
	// `Accept: text/event-stream`. The official SDK's shared transport checker
	// requires both media types, so widen this one streaming request before it
	// reaches the SDK.
	if requestHasMethod(r, "subscriptions/listen") && !strings.Contains(r.Header.Get("Accept"), "application/json") {
		r.Header.Set("Accept", "application/json, text/event-stream")
	}
	if modern {
		s.modern.ServeHTTP(w, r)
		return
	}
	s.legacy.ServeHTTP(w, r)
}

func requestHasMethod(r *http.Request, method string) bool {
	_, ok := methodRequest(r, method)
	return ok
}

func methodRequest(r *http.Request, method string) (any, bool) {
	if r == nil || r.Method != http.MethodPost || r.Body == nil {
		return nil, false
	}
	body, err := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(body))
	if err != nil {
		return nil, false
	}
	var message struct {
		ID     any    `json:"id"`
		Method string `json:"method"`
	}
	if json.Unmarshal(body, &message) != nil || message.Method != method {
		return nil, false
	}
	return message.ID, true
}

func (s *StreamableHTTPServer) Shutdown(context.Context) error { return nil }

func isModernRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	if r.Method == http.MethodGet || r.Method == http.MethodDelete || r.Header.Get("Mcp-Session-Id") != "" {
		return false
	}
	if r.Header.Get("Mcp-Protocol-Version") == "2026-07-28" {
		return true
	}
	if r.Method != http.MethodPost || r.Body == nil {
		return false
	}
	body, err := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(body))
	if err != nil {
		return false
	}
	var message struct {
		Method string `json:"method"`
		Params struct {
			Meta map[string]any `json:"_meta"`
		} `json:"params"`
	}
	if json.Unmarshal(body, &message) != nil {
		return false
	}
	if message.Method == "server/discover" {
		return true
	}
	version, _ := message.Params.Meta["io.modelcontextprotocol/protocolVersion"].(string)
	return version == "2026-07-28"
}

func ServeStdio(ctx context.Context, server *MCPServer) error {
	return server.sdk.Run(ctx, &sdk.StdioTransport{})
}
