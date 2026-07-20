package serv

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/dosco/graphjin/auth/v3"
	"github.com/dosco/graphjin/core/v3"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"go.uber.org/zap"
)

type scriptedAgentRunner struct {
	resp gjagent.Response
	err  error
	ctx  context.Context
	req  gjagent.Request
	conf gjagent.Config
	opts int
	emit []gjagent.ActionEvent
}

func (r *scriptedAgentRunner) Run(ctx context.Context, req gjagent.Request) (gjagent.Response, error) {
	r.ctx = ctx
	r.req = req
	if req.Observer != nil {
		for _, event := range r.emit {
			req.Observer(event)
		}
	}
	return r.resp, r.err
}

func withScriptedAgentRunner(t *testing.T, runner *scriptedAgentRunner) {
	t.Helper()
	prev := newGraphJinAgentRunner
	newGraphJinAgentRunner = func(_ *graphjinService, conf gjagent.Config, opts ...gjagent.Option) (graphjinAgentRunner, error) {
		runner.conf = conf
		runner.opts = len(opts)
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

// TestValidateConfRefusesDevAuthInAgenticMode locks in audit finding F2: agentic
// mode must refuse to boot with auth.development=true, because the header-trust
// SimpleHandler lets any caller spoof X-User-Role/X-User-ID and role is the only
// authorization control in agentic mode.
func TestValidateConfRefusesDevAuthInAgenticMode(t *testing.T) {
	logger := zap.NewNop()
	newSvc := func(mode string, devAuth bool) *graphjinService {
		return &graphjinService{
			conf: &Config{
				Core: core.Config{Mode: mode},
				Serv: Serv{Auth: Auth{Development: devAuth}},
			},
			log:  logger.Sugar(),
			zlog: logger,
		}
	}

	if err := validateConf(newSvc("agentic", true)); err == nil || !strings.Contains(err.Error(), "auth.development") {
		t.Fatalf("agentic + auth.development must be refused at startup, got %v", err)
	}
	// Not agentic, or verified auth: allowed.
	if err := validateConf(newSvc("agentic", false)); err != nil {
		t.Fatalf("agentic with verified auth should boot, got %v", err)
	}
	if err := validateConf(newSvc("dev", true)); err != nil {
		t.Fatalf("dev mode may use auth.development, got %v", err)
	}
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
	if !missingStatus.Enabled || !missingStatus.Ready || missingStatus.RESTReady || missingStatus.Status != "client_sampling_required" || missingStatus.APIKeyConfigured || missingStatus.ServerModelReady || !missingStatus.ClientSamplingAllowed || missingStatus.ClientSamplingAvailable {
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
				Enabled:        true,
				Provider:       "anthropic",
				Model:          "test-model",
				APIKeyEnv:      "GRAPHJIN_READY_AGENT_KEY",
				MaxSteps:       3,
				TimeoutSeconds: 12,
				ReadOnly:       true,
				ReturnTrace:    true,
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
	if !readyStatus.Enabled || !readyStatus.Ready || !readyStatus.RESTReady || readyStatus.Status != "ready" || !readyStatus.APIKeyConfigured || !readyStatus.ServerModelReady {
		t.Fatalf("unexpected ready status: %+v", readyStatus)
	}
	if readyStatus.Provider != "anthropic" || readyStatus.Model != "test-model" || readyStatus.MaxSteps != 3 || readyStatus.TimeoutSeconds != 50 {
		t.Fatalf("configured values not reflected in status: %+v", readyStatus)
	}
	if !readyStatus.ReadOnly || !readyStatus.ReturnTrace {
		t.Fatalf("agent flags not reflected in status: %+v", readyStatus)
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
			Core: core.Config{Sources: []core.SourceConfig{{Name: "graphjin", Kind: "database", Type: "sqlite"}}},
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
	if runner.req.Instruction != "find customers" {
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
		Core: core.Config{Sources: []core.SourceConfig{{Name: "graphjin", Kind: "database", Type: "sqlite"}}},
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
		Core: core.Config{Sources: []core.SourceConfig{{Name: "graphjin", Kind: "database", Type: "sqlite"}}},
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
	ms.service.conf.Agent.Sampling = gjagent.SamplingOff
	ms.service.conf.Agent.TimeoutSeconds = 12
	res, err := ms.handleAskGraphJinAgent(context.Background(), newToolRequest(map[string]any{
		"instruction": "run approved query",
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

func TestAskGraphJinAgentMCPRefusalAndNotices(t *testing.T) {
	runner := &scriptedAgentRunner{resp: gjagent.Response{
		Status: gjagent.StatusBlocked,
		Answer: "saved query detail is required first",
		Refusal: &gjagent.Refusal{
			Code:          "saved_query_detail_required",
			BlockedAction: "execute_saved_query",
			Because:       []string{"inspect query_catalog before execute_saved_query"},
			Unblock: []gjagent.UnblockStep{{
				Tool:   "query_catalog",
				Args:   map[string]any{"id": "saved_query:daily_roast_context"},
				Reason: "Inspect the saved query detail before executing it.",
			}},
			Retryable: true,
		},
		Notices: []gjagent.ResponseNotice{{
			Kind:    "watch",
			Message: "1 unseen artifact update",
			Count:   1,
		}},
	}}
	withScriptedAgentRunner(t, runner)

	ms := mockMcpServerWithConfig(MCPConfig{})
	ms.service.conf.Agent.Enabled = true
	ms.service.conf.Agent.Sampling = gjagent.SamplingOff
	res, err := ms.handleAskGraphJinAgent(context.Background(), newToolRequest(map[string]any{
		"instruction": "run approved query",
	}))
	if err != nil {
		t.Fatalf("handleAskGraphJinAgent: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected structured blocked response, got tool error: %+v", res.Content)
	}
	structured, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("structured content has type %T", res.StructuredContent)
	}
	refusal, ok := structured["refusal"].(map[string]any)
	if !ok {
		t.Fatalf("structured refusal missing: %+v", structured)
	}
	if refusal["code"] != "saved_query_detail_required" || refusal["blocked_action"] != "execute_saved_query" {
		t.Fatalf("unexpected refusal: %+v", refusal)
	}
	notices := anySlice(structured["notices"])
	if len(notices) != 1 {
		t.Fatalf("notices = %+v, want one", structured["notices"])
	}
}

func TestAskGraphJinAgentMCPSchema(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{})
	ms.service.conf.Agent.Enabled = true
	ms.service.conf.Agent.Sampling = gjagent.SamplingOff
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
	for _, name := range []string{"instruction", "context", "namespace", "max_steps", "return_trace"} {
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
			Core: core.Config{Sources: []core.SourceConfig{{Name: "graphjin", Kind: "database", Type: "sqlite"}}},
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
	ms.service.conf.Agent.Sampling = gjagent.SamplingOff

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

func TestAgentRESTNamespaceRouteWins(t *testing.T) {
	runner := &scriptedAgentRunner{resp: gjagent.Response{Status: gjagent.StatusAnswered, Answer: "ok"}}
	withScriptedAgentRunner(t, runner)

	logger := zap.NewNop()
	svc := &graphjinService{
		conf: &Config{
			Core: core.Config{Sources: []core.SourceConfig{{Name: "graphjin", Kind: "database", Type: "sqlite"}}},
			Serv: Serv{Agent: AgentConfig{Enabled: true}},
		},
		log:  logger.Sugar(),
		zlog: logger,
	}
	hs := &HttpService{}
	hs.Store(svc)

	// The body tries to redirect the request to another namespace; the route
	// namespace must win, matching the GraphQL endpoint semantics.
	body := `{"instruction":"find customers","namespace":"tenant_b"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ns/tenant_a/agent", strings.NewReader(body))
	rec := httptest.NewRecorder()
	hs.AgentWithNS(nil, "tenant_a").ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if runner.req.Namespace != "tenant_a" {
		t.Fatalf("namespace = %q, want route namespace tenant_a", runner.req.Namespace)
	}
}

func TestAgentRESTSSEStreamsActionsAndResult(t *testing.T) {
	runner := &scriptedAgentRunner{
		resp: gjagent.Response{Status: gjagent.StatusAnswered, Answer: "streamed", Skill: "data_discovery"},
		emit: []gjagent.ActionEvent{
			{Index: 1, Source: "seed", Tool: "query_catalog", Status: "ok"},
			{Index: 2, Source: "model", Tool: "execute_saved_query", Status: "ok"},
		},
	}
	withScriptedAgentRunner(t, runner)

	logger := zap.NewNop()
	svc := &graphjinService{
		conf: &Config{
			Core: core.Config{Sources: []core.SourceConfig{{Name: "graphjin", Kind: "database", Type: "sqlite"}}},
			Serv: Serv{Agent: AgentConfig{Enabled: true}},
		},
		log:  logger.Sugar(),
		zlog: logger,
	}
	hs := &HttpService{}
	hs.Store(svc)

	req := httptest.NewRequest(http.MethodPost, routeAgent, strings.NewReader(`{"instruction":"stream this"}`))
	req.Header.Set("Accept", "text/event-stream")
	rec := httptest.NewRecorder()
	hs.Agent(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("content type = %q, want text/event-stream", got)
	}
	body := rec.Body.String()
	if strings.Count(body, "event: action") != 2 {
		t.Fatalf("expected 2 action events, body:\n%s", body)
	}
	if !strings.Contains(body, "event: result") || !strings.Contains(body, `"answer":"streamed"`) {
		t.Fatalf("missing result event, body:\n%s", body)
	}
	if !strings.Contains(body, "event: complete") {
		t.Fatalf("missing complete event, body:\n%s", body)
	}
	if !strings.Contains(body, `"tool":"execute_saved_query"`) {
		t.Fatalf("action payload missing, body:\n%s", body)
	}
}

func TestAgentAuditHelpers(t *testing.T) {
	actions := make([]any, 0, 30)
	for i := 0; i < 30; i++ {
		actions = append(actions, map[string]any{
			"step": float64(i + 1), "source": "model", "tool": "query_catalog", "status": "ok",
		})
	}
	if got := agentActionCount(actions); got != 30 {
		t.Fatalf("agentActionCount = %d, want 30", got)
	}
	audited, ok := agentAuditActions(actions).([]any)
	if !ok {
		t.Fatalf("agentAuditActions type %T", agentAuditActions(actions))
	}
	if len(audited) != maxAuditActions {
		t.Fatalf("audit actions = %d, want %d (most recent kept)", len(audited), maxAuditActions)
	}
	first, _ := audited[0].(map[string]any)
	if first["step"] != float64(30-maxAuditActions+1) {
		t.Fatalf("audit should keep most recent actions, first step = %v", first["step"])
	}

	resp := gjagent.Response{Evidence: map[string]any{
		"protocol": map[string]any{
			"violations": []any{
				map[string]any{"code": "mutation_evidence_required", "blocking": true},
			},
		},
	}}
	codes := agentViolationCodes(resp)
	if len(codes) != 1 || codes[0] != "mutation_evidence_required" {
		t.Fatalf("violation codes = %v", codes)
	}

	store := newMemoryRuntimeEventStore(runtimeEventOptions{
		MaxEvents: 4,
		Now:       func() time.Time { return time.Unix(10, 0) },
	})
	svc := &graphjinService{conf: &Config{}, runtimeEvents: store}
	recordAgentRuntimeEvent(svc, context.Background(), gjagent.Request{Instruction: "run saved query"}, gjagent.Response{
		Status:  gjagent.StatusBlocked,
		Refusal: &gjagent.Refusal{Code: "saved_query_detail_required"},
	}, time.Millisecond, nil)
	rows := store.Rows(context.Background(), runtimeStatus{})
	if len(rows) < 2 {
		t.Fatalf("runtime rows = %d, want status + event", len(rows))
	}
	var details map[string]any
	if err := json.Unmarshal([]byte(fmt.Sprint(rows[1]["details_json"])), &details); err != nil {
		t.Fatalf("decode details_json: %v", err)
	}
	if details["refusal_code"] != "saved_query_detail_required" {
		t.Fatalf("refusal_code missing from runtime event details: %+v", details)
	}
}

func TestAskGraphJinAgentMCPHistoryArg(t *testing.T) {
	runner := &scriptedAgentRunner{resp: gjagent.Response{Status: gjagent.StatusAnswered, Answer: "ok"}}
	withScriptedAgentRunner(t, runner)

	ms := mockMcpServerWithConfig(MCPConfig{})
	ms.service.conf.Agent.Enabled = true
	ms.service.conf.Agent.Sampling = gjagent.SamplingOff
	res, err := ms.handleAskGraphJinAgent(context.Background(), newToolRequest(map[string]any{
		"instruction": "and how many were sold?",
		"history": []any{
			map[string]any{"role": "user", "content": "show products"},
			map[string]any{"role": "assistant", "content": "3 products", "status": "answered", "catalog_ids": []any{"table:db:public.products"}},
		},
	}))
	if err != nil {
		t.Fatalf("handleAskGraphJinAgent: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %+v", res.Content)
	}
	if len(runner.req.History) != 2 {
		t.Fatalf("history turns = %d, want 2", len(runner.req.History))
	}
	if runner.req.History[1].Role != "assistant" || len(runner.req.History[1].CatalogIDs) != 1 {
		t.Fatalf("assistant turn malformed: %+v", runner.req.History[1])
	}
	if runner.req.Observer != nil {
		t.Fatal("observer must not be set without a progress token")
	}
}

func TestAskGraphJinAgentMCPProgressTokenSetsObserver(t *testing.T) {
	runner := &scriptedAgentRunner{resp: gjagent.Response{Status: gjagent.StatusAnswered, Answer: "ok"}}
	withScriptedAgentRunner(t, runner)

	ms := mockMcpServerWithConfig(MCPConfig{})
	ms.service.conf.Agent.Enabled = true
	ms.service.conf.Agent.Sampling = gjagent.SamplingOff
	ms.srv = server.NewMCPServer("test", "0.0.0")

	req := newToolRequest(map[string]any{"instruction": "with progress"})
	req.Params.Meta = &mcp.Meta{ProgressToken: "tok-1"}
	res, err := ms.handleAskGraphJinAgent(context.Background(), req)
	if err != nil {
		t.Fatalf("handleAskGraphJinAgent: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %+v", res.Content)
	}
	if runner.req.Observer == nil {
		t.Fatal("observer must be set when a progress token is present")
	}
	// Emitting without a client session must be a safe no-op.
	runner.req.Observer(gjagent.ActionEvent{Index: 1, Tool: "query_catalog", Status: "ok"})
}

func TestAgentProgressMessage(t *testing.T) {
	msg := agentProgressMessage(gjagent.ActionEvent{
		Index: 2, Tool: "query_catalog", Status: "ok",
		Args:    map[string]any{"id": "table:db:public.products"},
		Summary: map[string]any{"card_count": 1},
	})
	if msg != "query_catalog(id=table:db:public.products) ok — 1 cards" {
		t.Fatalf("progress message = %q", msg)
	}
}
