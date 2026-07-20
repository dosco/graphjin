package serv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	ax "github.com/ax-llm/ax/packages/go"
	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/dosco/graphjin/core/v3"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type captureSamplingHandler struct {
	req      mcp.CreateMessageRequest
	response string
	calls    int
}

func (h *captureSamplingHandler) CreateMessage(_ context.Context, req mcp.CreateMessageRequest) (*mcp.CreateMessageResult, error) {
	h.calls++
	h.req = req
	response := h.response
	if response == "" {
		response = `{"javascriptCode":"await final('done', {})"}`
	}
	return &mcp.CreateMessageResult{
		SamplingMessage: mcp.SamplingMessage{
			Role:    mcp.RoleAssistant,
			Content: mcp.NewTextContent(response),
		},
		Model:      "client-model",
		StopReason: "endTurn",
	}, nil
}

func TestSamplingAIClientMapsAxChatToMCPSampling(t *testing.T) {
	mcpSrv := server.NewMCPServer("test", "0.0.0")
	mcpSrv.EnableSampling()
	handler := &captureSamplingHandler{}
	session := server.NewInProcessSession("session-1", handler)
	session.Initialize()
	session.SetClientCapabilities(mcp.ClientCapabilities{Sampling: &mcp.SamplingCapability{}})
	ctx := mcpSrv.WithContext(context.Background(), session)

	client := samplingAIClient{srv: mcpSrv}
	result, err := client.Chat(ctx, map[string]ax.Value{
		"chat_prompt": []ax.Value{
			map[string]ax.Value{"role": "system", "content": "system instructions"},
			map[string]ax.Value{"role": "user", "content": "question"},
			map[string]ax.Value{"role": "assistant", "content": "prior answer"},
		},
		"model_config": map[string]ax.Value{
			"temperature": 0.25,
			"max_tokens":  77,
		},
		"stop_sequences": []ax.Value{"END"},
		"response_format": map[string]ax.Value{
			"type": "json_schema",
		},
	}, nil)
	if err != nil {
		t.Fatalf("Chat returned error: %v", err)
	}

	if handler.req.SystemPrompt != "system instructions" {
		t.Fatalf("system prompt = %q", handler.req.SystemPrompt)
	}
	if len(handler.req.Messages) != 2 {
		t.Fatalf("expected 2 non-system messages, got %d", len(handler.req.Messages))
	}
	if handler.req.Messages[0].Role != mcp.RoleUser || samplingContentText(handler.req.Messages[0].Content) != "question" {
		t.Fatalf("first message mismatch: %+v", handler.req.Messages[0])
	}
	if handler.req.Messages[1].Role != mcp.RoleAssistant || samplingContentText(handler.req.Messages[1].Content) != "prior answer" {
		t.Fatalf("second message mismatch: %+v", handler.req.Messages[1])
	}
	if handler.req.MaxTokens != 77 {
		t.Fatalf("max tokens = %d", handler.req.MaxTokens)
	}
	if math.Abs(handler.req.Temperature-0.25) > 0.0001 {
		t.Fatalf("temperature = %v", handler.req.Temperature)
	}
	if len(handler.req.StopSequences) != 1 || handler.req.StopSequences[0] != "END" {
		t.Fatalf("stop sequences = %+v", handler.req.StopSequences)
	}
	if handler.req.Metadata == nil {
		t.Fatal("expected response metadata to be forwarded")
	}

	payload, ok := result.(map[string]ax.Value)
	if !ok {
		t.Fatalf("result type = %T", result)
	}
	results, ok := payload["results"].([]ax.Value)
	if !ok || len(results) != 1 {
		t.Fatalf("results payload = %#v", payload["results"])
	}
	first, ok := results[0].(map[string]ax.Value)
	if !ok {
		t.Fatalf("first result type = %T", results[0])
	}
	if first["content"] != `{"javascriptCode":"await final('done', {})"}` {
		t.Fatalf("content = %#v", first["content"])
	}
	if first["model"] != "client-model" || first["stop_reason"] != "endTurn" {
		t.Fatalf("sampling metadata not returned: %+v", first)
	}
}

func TestMCPHTTPTransportCachesOnlyStatefulHandler(t *testing.T) {
	statefulSvc := &graphjinService{conf: &Config{Serv: Serv{MCP: MCPConfig{HTTPStateful: true}}}}
	first := statefulSvc.mcpHTTPTransport(context.Background())
	second := statefulSvc.mcpHTTPTransport(context.Background())
	if first == nil || second == nil {
		t.Fatal("expected stateful transport handlers")
	}
	if first != second {
		t.Fatal("stateful MCP HTTP transport should reuse one handler per service")
	}

	statelessSvc := &graphjinService{conf: &Config{}}
	first = statelessSvc.mcpHTTPTransport(context.Background())
	second = statelessSvc.mcpHTTPTransport(context.Background())
	if first == nil || second == nil {
		t.Fatal("expected stateless transport handlers")
	}
	if first == second {
		t.Fatal("stateless MCP HTTP transport should remain per request")
	}
}

func TestMCPHTTPTransportCloseClearsStatefulHandler(t *testing.T) {
	svc := &graphjinService{conf: &Config{Serv: Serv{MCP: MCPConfig{HTTPStateful: true}}}}
	first := svc.mcpHTTPTransport(context.Background())
	if first == nil || svc.mcpHTTP == nil {
		t.Fatal("expected cached stateful MCP HTTP transport")
	}

	svc.closeMCPHTTPTransport()
	if svc.mcpHTTP != nil {
		t.Fatal("stateful MCP HTTP transport cache should be cleared on close")
	}

	second := svc.mcpHTTPTransport(context.Background())
	if second == nil {
		t.Fatal("expected stateful MCP HTTP transport to be recreated after close")
	}
	if first == second {
		t.Fatal("stateful MCP HTTP transport should be rebuilt after close")
	}
}

func TestMCPSamplingAvailableRequiresAdvertisedCapability(t *testing.T) {
	mcpSrv := server.NewMCPServer("test", "0.0.0")
	session := server.NewInProcessSession("session-1", &captureSamplingHandler{})
	ctx := mcpSrv.WithContext(context.Background(), session)

	if mcpSamplingAvailable(ctx) {
		t.Fatal("sampling should be unavailable until the client advertises the capability")
	}
	session.SetClientCapabilities(mcp.ClientCapabilities{Sampling: &mcp.SamplingCapability{}})
	if !mcpSamplingAvailable(ctx) {
		t.Fatal("sampling should be available with a sampling-capable session and advertised capability")
	}
}

func TestAgentSamplingOptionsMatrix(t *testing.T) {
	mcpSrv := server.NewMCPServer("test", "0.0.0")
	ms := &mcpServer{srv: mcpSrv}
	t.Setenv("GRAPHJIN_SAMPLING_MATRIX_KEY", "")
	conf := gjagent.Config{APIKeyEnv: "GRAPHJIN_SAMPLING_MATRIX_KEY"}

	path, opts, err := ms.agentSamplingOptions(context.Background(), conf)
	if !errors.Is(err, errMCPSamplingUnavailable) {
		t.Fatalf("automatic mode without session error = %v", err)
	}
	if path != agentSamplingPathUnavailable || len(opts) != 0 {
		t.Fatalf("automatic mode without session path=%s opts=%d", path, len(opts))
	}

	conf.Sampling = gjagent.SamplingOff
	path, opts, err = ms.agentSamplingOptions(context.Background(), conf)
	if err != nil || path != agentSamplingPathServer || len(opts) != 0 {
		t.Fatalf("sampling off path=%s opts=%d err=%v", path, len(opts), err)
	}
	session := server.NewInProcessSession("session-1", &captureSamplingHandler{})
	session.SetClientCapabilities(mcp.ClientCapabilities{Sampling: &mcp.SamplingCapability{}})
	ctx := mcpSrv.WithContext(context.Background(), session)
	conf.Sampling = ""
	path, opts, err = ms.agentSamplingOptions(ctx, conf)
	if err != nil {
		t.Fatalf("automatic mode with sampling session returned error: %v", err)
	}
	if path != agentSamplingPathClient || len(opts) != 1 {
		t.Fatalf("automatic mode with sampling session path=%s opts=%d", path, len(opts))
	}

	t.Setenv("GRAPHJIN_SAMPLING_MATRIX_KEY", "server-secret")
	path, opts, err = ms.agentSamplingOptions(ctx, conf)
	if err != nil || path != agentSamplingPathServer || len(opts) != 0 {
		t.Fatalf("server credentials must win: path=%s opts=%d err=%v", path, len(opts), err)
	}

	t.Setenv("GRAPHJIN_SAMPLING_MATRIX_KEY", "")
	ms.service = &graphjinService{agentClientFactory: func(gjagent.Config) (ax.AIClient, error) { return nil, nil }}
	path, opts, err = ms.agentSamplingOptions(ctx, conf)
	if err != nil || path != agentSamplingPathServer || len(opts) != 0 {
		t.Fatalf("injected server client must win: path=%s opts=%d err=%v", path, len(opts), err)
	}
}

func TestServerProviderFailureNeverFallsBackToSampling(t *testing.T) {
	t.Setenv("GRAPHJIN_SERVER_FIRST_KEY", "server-secret")
	mcpSrv := server.NewMCPServer("test", "0.0.0")
	mcpSrv.EnableSampling()
	handler := &captureSamplingHandler{}
	session := server.NewInProcessSession("session-1", handler)
	session.Initialize()
	session.SetClientCapabilities(mcp.ClientCapabilities{Sampling: &mcp.SamplingCapability{}})
	ctx := mcpSrv.WithContext(context.Background(), session)

	ms := mockMcpServerWithConfig(MCPConfig{})
	ms.srv = mcpSrv
	ms.service.conf.Agent.Enabled = true
	ms.service.conf.Agent.APIKeyEnv = "GRAPHJIN_SERVER_FIRST_KEY"
	ms.service.gj = &core.GraphJin{}

	previous := newGraphJinAgentRunner
	newGraphJinAgentRunner = func(_ *graphjinService, _ gjagent.Config, _ ...gjagent.Option) (graphjinAgentRunner, error) {
		return nil, errors.New("server provider failed")
	}
	t.Cleanup(func() { newGraphJinAgentRunner = previous })

	result, err := ms.handleAskGraphJinAgent(ctx, newToolRequest(map[string]any{"instruction": "server first"}))
	if err != nil {
		t.Fatalf("handleAskGraphJinAgent: %v", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("provider failure result = %+v", result)
	}
	if handler.calls != 0 {
		t.Fatalf("sampling calls = %d, want zero after server provider failure", handler.calls)
	}
}

func TestMissingServerCredentialsAndSamplingReturnsStructuredError(t *testing.T) {
	t.Setenv("GRAPHJIN_NO_MODEL_KEY", "")
	ms := mockMcpServerWithConfig(MCPConfig{})
	ms.service.conf.Agent.Enabled = true
	ms.service.conf.Agent.APIKeyEnv = "GRAPHJIN_NO_MODEL_KEY"
	ms.service.gj = &core.GraphJin{}

	result, err := ms.handleAskGraphJinAgent(context.Background(), newToolRequest(map[string]any{"instruction": "needs a model"}))
	if err != nil {
		t.Fatalf("handleAskGraphJinAgent: %v", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("missing model result = %+v", result)
	}
	structured, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("structured error type = %T", result.StructuredContent)
	}
	if structured["code"] != "model_sampling_unavailable" {
		t.Fatalf("structured error = %+v", structured)
	}
}

type samplingGraphRuntime struct{}

func (samplingGraphRuntime) GraphQLHelp(context.Context, map[string]any) (any, error) {
	return samplingCatalogSeed(), nil
}

func (samplingGraphRuntime) QueryCatalog(context.Context, map[string]any) (any, error) {
	return samplingCatalogSeed(), nil
}

func (samplingGraphRuntime) ValidateWhereClause(context.Context, map[string]any) (any, error) {
	return map[string]any{"valid": true}, nil
}

func (samplingGraphRuntime) ExecuteSavedQuery(context.Context, map[string]any) (any, error) {
	return map[string]any{"data": map[string]any{}}, nil
}

func (samplingGraphRuntime) ExecuteGraphQL(context.Context, map[string]any) (any, error) {
	return map[string]any{"data": map[string]any{}}, nil
}

func samplingCatalogSeed() map[string]any {
	return map[string]any{
		"cards": []any{
			map[string]any{
				"id":      "help:discovery",
				"kind":    "help",
				"name":    "discovery",
				"title":   "Discovery",
				"summary": "Use catalog discovery before execution.",
			},
		},
	}
}

type samplingProgram struct {
	options map[string]ax.Value
}

func (p *samplingProgram) Forward(ctx context.Context, client ax.AIClient, values map[string]ax.Value, _ map[string]ax.Value) (ax.Value, error) {
	if _, err := p.callTool("query_catalog", map[string]ax.Value{"id": "help:discovery"}); err != nil {
		return nil, err
	}
	result, err := client.Chat(ctx, map[string]ax.Value{
		"chat_prompt": []ax.Value{
			map[string]ax.Value{"role": "system", "content": "sampling integration system"},
			map[string]ax.Value{"role": "user", "content": fmt.Sprint(values["instruction"])},
		},
		"model_config": map[string]ax.Value{"max_tokens": 32},
	}, nil)
	if err != nil {
		return nil, err
	}

	payload, _ := result.(map[string]ax.Value)
	results, _ := payload["results"].([]ax.Value)
	first, _ := results[0].(map[string]ax.Value)
	return map[string]ax.Value{
		"status": gjagent.StatusAnswered,
		"answer": fmt.Sprint(first["content"]),
	}, nil
}

func (p *samplingProgram) callTool(name string, args map[string]ax.Value) (ax.Value, error) {
	tools := map[string]ax.Tool{}
	var items []ax.Value
	switch arr := p.options["functions"].(type) {
	case []ax.Value:
		items = arr
	case *ax.AxArray:
		items = arr.Items
	}
	for _, item := range items {
		if tool, ok := item.(ax.Tool); ok {
			tools[tool.Name] = tool
		}
	}
	tool, ok := tools[name]
	if !ok {
		return nil, fmt.Errorf("missing tool: %s", name)
	}
	return tool.Handler(args)
}

func (p *samplingProgram) GetActionLog() ax.Value {
	return []ax.Value{map[string]ax.Value{"step": 1, "tool": "sampling", "status": "ok"}}
}

func (p *samplingProgram) GetUsage() ax.Value {
	return nil
}

func (p *samplingProgram) GetChatLog() ax.Value {
	return nil
}

func (p *samplingProgram) ExportTrace() ax.Value {
	return nil
}

func TestAskGraphJinAgentMCPAutomaticallyUsesClientSamplingSession(t *testing.T) {
	mcpSrv := server.NewMCPServer("test", "0.0.0")
	mcpSrv.EnableSampling()
	handler := &captureSamplingHandler{response: "sampled answer"}
	session := server.NewInProcessSession("session-1", handler)
	session.Initialize()
	session.SetClientCapabilities(mcp.ClientCapabilities{Sampling: &mcp.SamplingCapability{}})
	ctx := mcpSrv.WithContext(context.Background(), session)

	ms := mockMcpServerWithConfig(MCPConfig{})
	ms.srv = mcpSrv
	ms.service.conf.Agent.Enabled = true
	ms.service.conf.Agent.Sampling = ""
	ms.service.gj = &core.GraphJin{}
	ms.service.runtimeEvents = newMemoryRuntimeEventStore(runtimeEventOptions{
		MaxEvents: 4,
		Now:       func() time.Time { return time.Unix(20, 0) },
	})

	prev := newGraphJinAgentRunner
	newGraphJinAgentRunner = func(_ *graphjinService, conf gjagent.Config, opts ...gjagent.Option) (graphjinAgentRunner, error) {
		agentOpts := []gjagent.Option{
			gjagent.WithRuntime(samplingGraphRuntime{}),
			gjagent.WithProgramFactory(func(_ string, options map[string]ax.Value) gjagent.Program {
				return &samplingProgram{options: options}
			}),
		}
		agentOpts = append(agentOpts, opts...)
		return gjagent.New(&core.GraphJin{}, conf, agentOpts...)
	}
	t.Cleanup(func() {
		newGraphJinAgentRunner = prev
	})

	res, err := ms.handleAskGraphJinAgent(ctx, newToolRequest(map[string]any{
		"instruction": "use client sampling",
	}))
	if err != nil {
		t.Fatalf("handleAskGraphJinAgent: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %+v", res.Content)
	}

	structured := assertToolStructuredMap(t, res)
	if structured["status"] != gjagent.StatusAnswered || structured["answer"] != "sampled answer" {
		t.Fatalf("unexpected structured response: %+v", structured)
	}
	if handler.req.SystemPrompt != "sampling integration system" {
		t.Fatalf("system prompt = %q", handler.req.SystemPrompt)
	}
	if len(handler.req.Messages) != 1 {
		t.Fatalf("sampling messages = %d, want 1", len(handler.req.Messages))
	}
	if handler.req.Messages[0].Role != mcp.RoleUser || samplingContentText(handler.req.Messages[0].Content) != "use client sampling" {
		t.Fatalf("sampling user message mismatch: %+v", handler.req.Messages[0])
	}
	if handler.req.MaxTokens != 32 {
		t.Fatalf("max tokens = %d", handler.req.MaxTokens)
	}

	rows := ms.service.runtimeEvents.Rows(context.Background(), runtimeStatus{})
	if len(rows) < 2 {
		t.Fatalf("runtime rows = %d, want status + event", len(rows))
	}
	var details map[string]any
	if err := json.Unmarshal([]byte(fmt.Sprint(rows[1]["details_json"])), &details); err != nil {
		t.Fatalf("decode details_json: %v", err)
	}
	if details["sampling_path"] != agentSamplingPathClient {
		t.Fatalf("sampling_path = %v, want %s; details=%+v", details["sampling_path"], agentSamplingPathClient, details)
	}
}
