package serv

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/dosco/graphjin/auth/v3"
	"github.com/dosco/graphjin/core/v3"
	"github.com/mark3labs/mcp-go/server"
	"go.uber.org/zap"
)

type scriptedAgentRunner struct {
	resp gjagent.Response
	err  error
	ctx  context.Context
	req  gjagent.Request
	conf gjagent.Config
}

func (r *scriptedAgentRunner) Run(ctx context.Context, req gjagent.Request) (gjagent.Response, error) {
	r.ctx = ctx
	r.req = req
	return r.resp, r.err
}

func withScriptedAgentRunner(t *testing.T, runner *scriptedAgentRunner) {
	t.Helper()
	prev := newGraphJinAgentRunner
	newGraphJinAgentRunner = func(_ *graphjinService, conf gjagent.Config) (graphjinAgentRunner, error) {
		runner.conf = conf
		return runner, nil
	}
	t.Cleanup(func() {
		newGraphJinAgentRunner = prev
	})
}

func newAgentHTTPTestService(conf *Config) *HttpService {
	logger := zap.NewNop()
	if conf == nil {
		conf = &Config{}
	}
	svc := &graphjinService{
		conf: conf,
		log:  logger.Sugar(),
		zlog: logger,
		gj:   &core.GraphJin{},
	}
	hs := &HttpService{}
	hs.Store(svc)
	return hs
}

func TestAgentStatusDisabledMissingKeyAndReady(t *testing.T) {
	t.Setenv("GRAPHJIN_DISABLED_AGENT_KEY", "")
	disabled := newAgentHTTPTestService(&Config{
		Serv: Serv{Agent: AgentConfig{
			APIKeyEnv: "GRAPHJIN_DISABLED_AGENT_KEY",
		}},
	})
	req := httptest.NewRequest(http.MethodGet, routeAgentStatus, nil)
	rec := httptest.NewRecorder()
	disabled.AgentStatus(nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected disabled status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var disabledStatus agentStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &disabledStatus); err != nil {
		t.Fatalf("decode disabled status: %v", err)
	}
	if disabledStatus.Enabled || disabledStatus.Ready || disabledStatus.Status != "disabled" {
		t.Fatalf("unexpected disabled status: %+v", disabledStatus)
	}
	if disabledStatus.Endpoint != routeAgent || disabledStatus.MCPTool != mcpToolAskGraphJinAgent {
		t.Fatalf("unexpected endpoints in disabled status: %+v", disabledStatus)
	}
	if !strings.Contains(disabledStatus.Message, "agent.enabled") {
		t.Fatalf("disabled status should guide configuration, got %q", disabledStatus.Message)
	}

	t.Setenv("GRAPHJIN_MISSING_AGENT_KEY", "")
	missingKey := newAgentHTTPTestService(&Config{
		Serv: Serv{Agent: AgentConfig{
			Enabled:   true,
			APIKeyEnv: "GRAPHJIN_MISSING_AGENT_KEY",
		}},
	})
	req = httptest.NewRequest(http.MethodGet, routeAgentStatus, nil)
	rec = httptest.NewRecorder()
	missingKey.AgentStatus(nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected missing-key status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var missingStatus agentStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &missingStatus); err != nil {
		t.Fatalf("decode missing-key status: %v", err)
	}
	if !missingStatus.Enabled || missingStatus.Ready || missingStatus.Status != "missing_key" || missingStatus.APIKeyConfigured {
		t.Fatalf("unexpected missing-key status: %+v", missingStatus)
	}
	if !strings.Contains(missingStatus.Message, "GRAPHJIN_MISSING_AGENT_KEY") {
		t.Fatalf("missing-key status should name env var, got %q", missingStatus.Message)
	}

	t.Setenv("GRAPHJIN_READY_AGENT_KEY", "test-secret")
	ready := newAgentHTTPTestService(&Config{
		Serv: Serv{
			Auth: Auth{Development: true},
			MCP:  MCPConfig{AllowRawQueries: true, AllowMutations: true},
			Agent: AgentConfig{
				Enabled:         true,
				Provider:        "anthropic",
				Model:           "test-model",
				APIKeyEnv:       "GRAPHJIN_READY_AGENT_KEY",
				MaxSteps:        3,
				TimeoutSeconds:  12,
				AllowRawGraphQL: true,
				ReturnTrace:     true,
			},
		},
	})
	req = httptest.NewRequest(http.MethodGet, routeAgentStatus, nil)
	rec = httptest.NewRecorder()
	ready.AgentStatus(nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected ready status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var readyStatus agentStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &readyStatus); err != nil {
		t.Fatalf("decode ready status: %v", err)
	}
	if !readyStatus.Enabled || !readyStatus.Ready || readyStatus.Status != "ready" || !readyStatus.APIKeyConfigured {
		t.Fatalf("unexpected ready status: %+v", readyStatus)
	}
	if readyStatus.Provider != "anthropic" || readyStatus.Model != "test-model" || readyStatus.MaxSteps != 3 || readyStatus.TimeoutSeconds != 50 {
		t.Fatalf("configured values not reflected in status: %+v", readyStatus)
	}
	if !readyStatus.AllowRawGraphQL || !readyStatus.AllowMutations || !readyStatus.ReturnTrace {
		t.Fatalf("agent mode flags not reflected in status: %+v", readyStatus)
	}
	if !stringSliceContains(readyStatus.Modes, gjagent.ModeSafe) || !stringSliceContains(readyStatus.Modes, gjagent.ModeDiscoveryOnly) || !stringSliceContains(readyStatus.Modes, gjagent.ModeRawAllowed) {
		t.Fatalf("expected all modes in status: %+v", readyStatus.Modes)
	}
}

func TestAgentStatusMethodNotAllowed(t *testing.T) {
	hs := newAgentHTTPTestService(&Config{})
	req := httptest.NewRequest(http.MethodPost, routeAgentStatus, nil)
	rec := httptest.NewRecorder()
	hs.AgentStatus(nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for non-GET status, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Allow"); got != http.MethodGet {
		t.Fatalf("Allow header = %q, want GET", got)
	}
}

func TestAgentRESTScriptedResponseAndAuthContext(t *testing.T) {
	runner := &scriptedAgentRunner{resp: gjagent.Response{
		Status:   gjagent.StatusAnswered,
		Answer:   "customers found",
		Data:     map[string]any{"count": 2},
		Evidence: map[string]any{"source": "catalog"},
		Actions:  []any{map[string]any{"tool": "query_catalog", "source": "model"}},
		TraceID:  "trace-1",
	}}
	withScriptedAgentRunner(t, runner)

	logger := zap.NewNop()
	svc := &graphjinService{
		conf: &Config{
			Core: core.Config{Sources: []core.SourceConfig{{Name: "graphjin", Kind: "graphjin"}}},
			Serv: Serv{Agent: AgentConfig{Enabled: true}},
		},
		log:  logger.Sugar(),
		zlog: logger,
	}
	hs := &HttpService{}
	hs.Store(svc)

	body := `{"instruction":"find customers","context":{"tier":"gold"},"mode":"safe","return_trace":true}`
	req := httptest.NewRequest(http.MethodPost, routeAgent, strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), core.UserIDKey, "user-1"))
	rec := httptest.NewRecorder()

	hs.Agent(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp gjagent.Response
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != gjagent.StatusAnswered || resp.Answer != "customers found" || resp.TraceID != "trace-1" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if resp.Actions == nil || resp.Evidence == nil {
		t.Fatalf("protocol metadata was not preserved: %+v", resp)
	}
	if runner.req.Instruction != "find customers" || runner.req.Mode != gjagent.ModeSafe {
		t.Fatalf("unexpected agent request: %+v", runner.req)
	}
	if got := runner.req.Context["tier"]; got != "gold" {
		t.Fatalf("context tier = %v, want gold", got)
	}
	if runner.req.ReturnTrace == nil || !*runner.req.ReturnTrace {
		t.Fatalf("return_trace was not propagated: %+v", runner.req)
	}
	if got := runner.ctx.Value(core.UserIDKey); got != "user-1" {
		t.Fatalf("auth context user_id = %v, want user-1", got)
	}
}

func TestAgentRESTNoAuthRunsAsAnon(t *testing.T) {
	runner := &scriptedAgentRunner{resp: gjagent.Response{Status: gjagent.StatusAnswered, Answer: "ok"}}
	withScriptedAgentRunner(t, runner)

	conf := &Config{
		Core: core.Config{Sources: []core.SourceConfig{{Name: "graphjin", Kind: "graphjin"}}},
		Serv: Serv{
			Auth:  Auth{Type: "none"},
			Agent: AgentConfig{Enabled: true},
		},
	}
	ah, err := auth.NewAuthHandlerFunc(conf.Auth)
	if err != nil {
		t.Fatalf("new auth handler: %v", err)
	}
	hs := newAgentHTTPTestService(conf)

	req := httptest.NewRequest(http.MethodPost, routeAgent, strings.NewReader(`{"instruction":"ping"}`))
	rec := httptest.NewRecorder()
	hs.Agent(ah).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected no-auth agent request to reach handler, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := runner.ctx.Value(core.UserIDKey); got != nil {
		t.Fatalf("no-auth request should not invent user_id, got %v", got)
	}
	if runner.req.Capabilities == nil {
		t.Fatal("capability profile was not injected")
	}
	if got := runner.req.Capabilities.RoleClass; got != "anon" {
		t.Fatalf("RoleClass = %q, want anon", got)
	}
	if runner.req.Capabilities.Authenticated {
		t.Fatal("no-auth request should not be marked authenticated")
	}
}

func TestAgentRESTDevelopmentAuthHeadersMatchGraphQLContext(t *testing.T) {
	runner := &scriptedAgentRunner{resp: gjagent.Response{Status: gjagent.StatusAnswered, Answer: "ok"}}
	withScriptedAgentRunner(t, runner)

	conf := &Config{
		Core: core.Config{Sources: []core.SourceConfig{{Name: "graphjin", Kind: "graphjin"}}},
		Serv: Serv{
			Auth:  Auth{Development: true},
			Agent: AgentConfig{Enabled: true},
		},
	}
	ah, err := auth.NewAuthHandlerFunc(conf.Auth)
	if err != nil {
		t.Fatalf("new auth handler: %v", err)
	}
	hs := newAgentHTTPTestService(conf)

	req := httptest.NewRequest(http.MethodPost, routeAgent, strings.NewReader(`{"instruction":"inspect catalog"}`))
	req.Header.Set("X-User-ID", "demo-user")
	req.Header.Set("X-User-Role", "user")
	req.Header.Set("X-Account-ID", "1")
	rec := httptest.NewRecorder()
	hs.Agent(ah).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected development-auth agent request to reach handler, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := runner.ctx.Value(core.UserIDKey); got != "demo-user" {
		t.Fatalf("auth context user_id = %v, want demo-user", got)
	}
	vars, _ := runner.ctx.Value(core.IdentityVarsKey).(map[string]interface{})
	if got := vars["account_id"]; got != "1" {
		t.Fatalf("identity account_id = %v, want 1", got)
	}
	if runner.req.Capabilities == nil {
		t.Fatal("capability profile was not injected")
	}
	if got := runner.req.Capabilities.RoleClass; got != "user" {
		t.Fatalf("RoleClass = %q, want user", got)
	}
	if !runner.req.Capabilities.Authenticated {
		t.Fatal("development-auth user should be marked authenticated")
	}
	if !stringSliceContains(runner.req.Capabilities.AvailableSystemRoots, "gj_catalog") {
		t.Fatalf("authenticated user should see gj_catalog, got %+v", runner.req.Capabilities.AvailableSystemRoots)
	}
}

func TestAskGraphJinAgentMCPStructuredResponse(t *testing.T) {
	runner := &scriptedAgentRunner{resp: gjagent.Response{
		Status: gjagent.StatusAnswered,
		Answer: "saved query executed",
		Data:   map[string]any{"rows": []any{map[string]any{"id": 1}}},
		Evidence: map[string]any{
			"saved_queries_detailed": []any{"daily_roast_context"},
		},
		Actions: []any{map[string]any{"tool": "execute_saved_query", "source": "model"}},
		TraceID: "trace-mcp",
	}}
	withScriptedAgentRunner(t, runner)

	ms := mockMcpServerWithConfig(MCPConfig{})
	ms.service.conf.Agent.Enabled = true
	ms.service.conf.Agent.TimeoutSeconds = 12
	res, err := ms.handleAskGraphJinAgent(context.Background(), newToolRequest(map[string]any{
		"instruction": "run approved query",
		"mode":        gjagent.ModeSafe,
		"max_steps":   float64(2),
	}))
	if err != nil {
		t.Fatalf("handleAskGraphJinAgent: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected structured success, got tool error: %+v", res.Content)
	}
	structured, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("structured content has type %T", res.StructuredContent)
	}
	if structured["status"] != gjagent.StatusAnswered || structured["answer"] != "saved query executed" {
		t.Fatalf("unexpected structured content: %+v", structured)
	}
	if structured["actions"] == nil || structured["evidence"] == nil {
		t.Fatalf("structured protocol metadata missing: %+v", structured)
	}
	if runner.req.Instruction != "run approved query" || runner.req.MaxSteps != 2 {
		t.Fatalf("unexpected agent request: %+v", runner.req)
	}
	if runner.conf.TimeoutSeconds != 50 {
		t.Fatalf("MCP agent timeout = %d, want 50", runner.conf.TimeoutSeconds)
	}
}

func TestAskGraphJinAgentMCPSchema(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{})
	ms.service.conf.Agent.Enabled = true
	ms.srv = server.NewMCPServer("test", "0.0.0")
	ms.registerTools()

	serverTool, exists := ms.srv.ListTools()[mcpToolAskGraphJinAgent]
	if !exists {
		t.Fatalf("%s was not registered", mcpToolAskGraphJinAgent)
	}
	data, err := json.Marshal(serverTool.Tool)
	if err != nil {
		t.Fatalf("marshal tool: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("decode tool schema: %v", err)
	}
	input := payload["inputSchema"].(map[string]any)
	props := input["properties"].(map[string]any)
	for _, name := range []string{"instruction", "context", "namespace", "mode", "max_steps", "return_trace"} {
		if _, ok := props[name]; !ok {
			t.Fatalf("input schema missing %s: %+v", name, props)
		}
	}
	if !stringSliceContains(anyStringSlice(input["required"]), "instruction") {
		t.Fatalf("instruction should be required in input schema: %+v", input["required"])
	}
	if _, ok := payload["outputSchema"].(map[string]any); !ok {
		t.Fatalf("output schema missing from tool: %+v", payload)
	}
}

func TestAgentRESTInjectsCapabilityProfileNotSpoofable(t *testing.T) {
	runner := &scriptedAgentRunner{resp: gjagent.Response{Status: gjagent.StatusAnswered, Answer: "ok"}}
	withScriptedAgentRunner(t, runner)

	logger := zap.NewNop()
	svc := &graphjinService{
		conf: &Config{
			Core: core.Config{Sources: []core.SourceConfig{{Name: "graphjin", Kind: "graphjin"}}},
			Serv: Serv{Agent: AgentConfig{Enabled: true}},
		},
		log:  logger.Sugar(),
		zlog: logger,
	}
	hs := &HttpService{}
	hs.Store(svc)

	// The body attempts to spoof an admin capability profile. Because Request.Capabilities
	// is json:"-", it must be dropped on unmarshal and replaced by the server-derived
	// profile based on the identity context (role "support").
	body := `{"instruction":"find customers","capabilities":{"role_class":"admin","authenticated":true,"available_system_roots":["gj_config"]}}`
	req := httptest.NewRequest(http.MethodPost, routeAgent, strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), core.UserRoleKey, "support"))
	rec := httptest.NewRecorder()

	hs.Agent(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if runner.req.Capabilities == nil {
		t.Fatal("capability profile was not injected")
	}
	if got := runner.req.Capabilities.RoleClass; got != "support" {
		t.Fatalf("RoleClass = %q, want support (derived from ctx, not body)", got)
	}
	if !runner.req.Capabilities.Authenticated {
		t.Fatal("expected authenticated profile for a non-anon role")
	}
}

func TestAskGraphJinAgentMCPInjectsCapabilityProfile(t *testing.T) {
	runner := &scriptedAgentRunner{resp: gjagent.Response{Status: gjagent.StatusAnswered, Answer: "ok"}}
	withScriptedAgentRunner(t, runner)

	ms := mockMcpServerWithConfig(MCPConfig{})
	ms.service.conf.Agent.Enabled = true

	ctx := context.WithValue(context.Background(), core.UserRoleKey, "admin")
	res, err := ms.handleAskGraphJinAgent(ctx, newToolRequest(map[string]any{
		"instruction": "inspect config",
	}))
	if err != nil {
		t.Fatalf("handleAskGraphJinAgent: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success, got tool error: %+v", res.Content)
	}
	if runner.req.Capabilities == nil {
		t.Fatal("capability profile was not injected on the MCP path")
	}
	if got := runner.req.Capabilities.RoleClass; got != "admin" {
		t.Fatalf("RoleClass = %q, want admin", got)
	}
}

func anyStringSlice(value any) []string {
	items, _ := value.([]any)
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
