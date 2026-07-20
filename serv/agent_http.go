package serv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/dosco/graphjin/auth/v3"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

const (
	defaultAgentProvider       = "openai"
	defaultAgentAPIKeyEnv      = "OPENAI_API_KEY"
	defaultAgentMaxSteps       = 8
	defaultAgentTimeoutSeconds = 50
)

type agentStatusResponse struct {
	Status                  string `json:"status"`
	Enabled                 bool   `json:"enabled"`
	Ready                   bool   `json:"ready"`
	RESTReady               bool   `json:"rest_ready"`
	ServerModelReady        bool   `json:"server_model_ready"`
	ClientSamplingAllowed   bool   `json:"client_sampling_allowed"`
	ClientSamplingAvailable bool   `json:"client_sampling_available"`
	Endpoint                string `json:"endpoint"`
	MCPTool                 string `json:"mcp_tool"`
	Provider                string `json:"provider"`
	Model                   string `json:"model,omitempty"`
	APIKeyEnv               string `json:"api_key_env"`
	APIKeyConfigured        bool   `json:"api_key_configured"`
	MaxSteps                int    `json:"max_steps"`
	TimeoutSeconds          int    `json:"timeout_seconds"`
	ReadOnly                bool   `json:"read_only"`
	ReturnTrace             bool   `json:"return_trace"`
	Namespace               string `json:"namespace,omitempty"`
	Message                 string `json:"message"`
}

// Agent is the HTTP handler for the server-side GraphJin agent endpoint.
func (s *HttpService) Agent(ah auth.HandlerFunc) http.Handler {
	h := s.apiV1Agent(nil)
	return apiV1Handler(s, nil, h, ah)
}

// AgentWithNS is the namespaced HTTP handler for the server-side GraphJin agent endpoint.
func (s *HttpService) AgentWithNS(ah auth.HandlerFunc, ns string) http.Handler {
	h := s.apiV1Agent(&ns)
	return apiV1Handler(s, &ns, h, ah)
}

// AgentStatus is the HTTP handler for the server-side GraphJin agent readiness endpoint.
func (s *HttpService) AgentStatus(ah auth.HandlerFunc) http.Handler {
	h := s.apiV1AgentStatus(nil)
	return apiV1Handler(s, nil, h, ah)
}

// AgentStatusWithNS is the namespaced HTTP handler for the server-side GraphJin agent readiness endpoint.
func (s *HttpService) AgentStatusWithNS(ah auth.HandlerFunc, ns string) http.Handler {
	h := s.apiV1AgentStatus(&ns)
	return apiV1Handler(s, &ns, h, ah)
}

func (s1 *HttpService) apiV1AgentStatus(ns *string) http.Handler {
	h := func(w http.ResponseWriter, r *http.Request) {
		s := s1.Load().(*graphjinService)
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			writeAgentError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		_ = json.NewEncoder(w).Encode(agentStatusFromConfig(agentConfigFromService(s.conf), ns, s.agentClientFactory != nil))
	}
	return http.HandlerFunc(h)
}

func (s1 *HttpService) apiV1Agent(ns *string) http.Handler {
	dtrace := otel.GetTextMapPropagator()

	h := func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		s := s1.Load().(*graphjinService)
		w.Header().Set("Content-Type", "application/json")
		extendDeadlineForAgent(w, s.conf)
		defer func() {
			if recovered := recover(); recovered != nil {
				if s != nil && s.log != nil {
					s.log.Errorf("agent handler panic: %v", recovered)
				}
				writeAgentError(w, http.StatusInternalServerError, fmt.Sprintf("agent handler panic: %v", recovered))
			}
		}()

		ctx, opts := newDTrace(dtrace, r)
		ctx, span := s.spanStart(ctx, "Agent Request", opts...)
		defer span.End()

		if !s.conf.agentEnabled() {
			writeAgentError(w, http.StatusNotFound, "GraphJin agent is disabled")
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeAgentError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		var req gjagent.Request
		body, err := io.ReadAll(io.LimitReader(r.Body, maxReadBytes))
		if err == nil {
			defer r.Body.Close() //nolint:errcheck
			err = json.Unmarshal(body, &req)
		}
		if err != nil {
			spanError(span, err)
			writeAgentError(w, http.StatusBadRequest, err.Error())
			return
		}
		// The route namespace is authoritative, matching the GraphQL endpoint:
		// a body namespace never overrides the /api/v1/ns/{ns}/ route.
		if ns != nil {
			req.Namespace = *ns
		}

		ctx = s.applyIdentityContext(ctx)
		// Capabilities is server-derived and json:"-", so it is never taken from the
		// request body; set it from the caller's identity context.
		req.Capabilities = s.agentCapabilityProfile(ctx)
		runner, err := newGraphJinAgentRunner(s, agentConfigFromService(s.conf))
		if err == nil && isSSERequest(r) {
			s.agentSSE(ctx, w, req, runner, start)
			if span.IsRecording() {
				span.SetAttributes(
					attribute.String("http.path", r.RequestURI),
					attribute.String("http.method", r.Method),
					attribute.Bool("agent.sse", true))
			}
			return
		}
		var resp gjagent.Response
		if err == nil {
			resp, err = runner.Run(ctx, req)
			if err == nil {
				s.appendWatchNotices(ctx, &resp)
			}
		}
		status := agentHTTPStatus(err)
		if err != nil {
			spanError(span, err)
			resp = gjagent.Response{
				Status: gjagent.StatusError,
				Errors: []gjagent.ErrorInfo{{
					Message: err.Error(),
				}},
			}
		}
		recordAgentRuntimeEvent(s, ctx, req, resp, time.Since(start), err)

		if span.IsRecording() {
			span.SetAttributes(
				attribute.String("http.path", r.RequestURI),
				attribute.String("http.method", r.Method),
				attribute.String("agent.status", resp.Status))
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(resp)
	}
	return http.HandlerFunc(h)
}

func agentStatusFromConfig(conf gjagent.Config, ns *string, injectedServerClient ...bool) agentStatusResponse {
	provider := strings.TrimSpace(conf.Provider)
	if provider == "" {
		provider = defaultAgentProvider
	}
	apiKeyEnv := strings.TrimSpace(conf.APIKeyEnv)
	if apiKeyEnv == "" {
		apiKeyEnv = defaultAgentAPIKeyEnv
	}
	maxSteps := conf.MaxSteps
	if maxSteps <= 0 {
		maxSteps = defaultAgentMaxSteps
	}
	timeoutSeconds := gjagent.EffectiveTimeoutSeconds(conf.TimeoutSeconds)

	apiKeyConfigured := strings.TrimSpace(os.Getenv(apiKeyEnv)) != ""
	serverModelReady := apiKeyConfigured || (len(injectedServerClient) != 0 && injectedServerClient[0])
	clientSamplingAllowed := normalizeAgentSamplingMode(conf.Sampling) != gjagent.SamplingOff
	status := "ready"
	message := "GraphJin agent is ready."
	ready := conf.Enabled && (serverModelReady || clientSamplingAllowed)
	switch {
	case !conf.Enabled:
		status = "disabled"
		message = "Enable agent.enabled; REST also needs " + apiKeyEnv + ", while MCP may use a sampling-capable client."
	case !serverModelReady && clientSamplingAllowed:
		status = "client_sampling_required"
		message = "REST requires " + apiKeyEnv + "; MCP can use a sampling-capable client."
	case !serverModelReady:
		status = "missing_key"
		message = "GraphJin agent is enabled but " + apiKeyEnv + " is not set and client sampling is disabled."
	}

	resp := agentStatusResponse{
		Status:                  status,
		Enabled:                 conf.Enabled,
		Ready:                   ready,
		RESTReady:               conf.Enabled && serverModelReady,
		ServerModelReady:        serverModelReady,
		ClientSamplingAllowed:   conf.Enabled && clientSamplingAllowed,
		ClientSamplingAvailable: false, // REST status has no MCP client session.
		Endpoint:                routeAgent,
		MCPTool:                 mcpToolAskGraphJinAgent,
		Provider:                provider,
		Model:                   strings.TrimSpace(conf.Model),
		APIKeyEnv:               apiKeyEnv,
		APIKeyConfigured:        apiKeyConfigured,
		MaxSteps:                maxSteps,
		TimeoutSeconds:          timeoutSeconds,
		ReadOnly:                conf.ReadOnly,
		ReturnTrace:             conf.ReturnTrace,
		Message:                 message,
	}
	if ns != nil {
		resp.Namespace = *ns
	}
	return resp
}

// agentSSE streams the agent run over Server-Sent Events: one `action` event
// per executed tool call, then a single `result` event carrying the final
// agent Response, then `complete`. Requested via Accept: text/event-stream;
// the default JSON contract is unchanged.
func (s *graphjinService) agentSSE(ctx context.Context, w http.ResponseWriter, req gjagent.Request, runner graphjinAgentRunner, start time.Time) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeAgentError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	clearStreamDeadline(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	writeEvent := func(event string, payload any) {
		data, err := json.Marshal(payload)
		if err != nil {
			return
		}
		_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
		flusher.Flush()
	}
	// Run() invokes the observer synchronously on this goroutine per tool call.
	req.Observer = func(event gjagent.ActionEvent) { writeEvent("action", event) }

	resp, err := runner.Run(ctx, req)
	if err != nil {
		resp = gjagent.Response{
			Status: gjagent.StatusError,
			Errors: []gjagent.ErrorInfo{{Message: err.Error()}},
		}
	} else {
		s.appendWatchNotices(ctx, &resp)
	}
	recordAgentRuntimeEvent(s, ctx, req, resp, time.Since(start), err)
	writeEvent("result", resp)
	writeEvent("complete", map[string]any{"status": resp.Status})
}

// extendDeadlineForAgent lifts the per-request read/write deadlines so the
// server-owned Ax agent can run up to its configured timeout without the global
// http.Server WriteTimeout closing the connection before a JSON response is
// written.
func extendDeadlineForAgent(w http.ResponseWriter, conf *Config) {
	timeoutSecs := defaultAgentTimeoutSeconds
	if conf != nil {
		timeoutSecs = gjagent.EffectiveTimeoutSeconds(conf.Agent.TimeoutSeconds)
	}
	deadline := time.Now().Add(time.Duration(timeoutSecs+30) * time.Second)
	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(deadline); err != nil {
		return
	}
	_ = rc.SetReadDeadline(deadline)
}

func writeAgentError(w http.ResponseWriter, status int, message string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(gjagent.Response{
		Status: gjagent.StatusError,
		Errors: []gjagent.ErrorInfo{{
			Message: message,
		}},
	})
}

func agentHTTPStatus(err error) int {
	switch {
	case err == nil:
		return http.StatusOK
	case errors.Is(err, gjagent.ErrMissingInstruction),
		errors.Is(err, gjagent.ErrInstructionTooLong):
		return http.StatusBadRequest
	case errors.Is(err, gjagent.ErrMissingAPIKey):
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

func recordAgentRuntimeEvent(s *graphjinService, ctx context.Context, req gjagent.Request, resp gjagent.Response, duration time.Duration, err error) {
	if s == nil {
		return
	}
	event := runtimeEvent{
		Kind:       runtimeKindEvent,
		Phase:      "agent",
		Status:     runtimeStatusReady,
		Severity:   "info",
		Summary:    "GraphJin agent request completed",
		DurationMS: duration.Milliseconds(),
		Details: map[string]any{
			"agent_status":  resp.Status,
			"namespace":     req.Namespace,
			"has_context":   len(req.Context) != 0,
			"history_turns": len(req.History),
			"return_trace":  req.ReturnTrace != nil && *req.ReturnTrace,
		},
	}
	if samplingPath := agentSamplingPathFromContext(ctx); samplingPath != "" {
		event.Details["sampling_path"] = samplingPath
	}
	// Audit trail: what the agent actually did, independent of return_trace.
	if resp.Skill != "" {
		event.Details["skill"] = resp.Skill
	}
	if resp.Refusal != nil && strings.TrimSpace(resp.Refusal.Code) != "" {
		event.Details["refusal_code"] = resp.Refusal.Code
	}
	if actions := agentAuditActions(resp.Actions); actions != nil {
		event.Details["actions"] = actions
	}
	if steps := agentActionCount(resp.Actions); steps != 0 {
		event.Details["steps"] = steps
	}
	if resp.Usage != nil {
		event.Details["usage"] = resp.Usage
	}
	if codes := agentViolationCodes(resp); len(codes) != 0 {
		event.Details["violations"] = codes
	}
	if err != nil {
		event.Status = runtimeStatusFailed
		event.Severity = "error"
		event.Summary = "GraphJin agent request failed"
		event.ErrorCode = "agent_error"
		event.Details["error"] = err.Error()
	} else if len(resp.Errors) != 0 {
		event.Status = runtimeStatusDegraded
		event.Severity = "warn"
		event.Summary = "GraphJin agent returned errors"
	}
	s.recordRuntimeEvent(ctx, event)
}

const (
	// maxAuditActions bounds the per-event action audit (most recent kept).
	maxAuditActions = 24
	// maxAuditActionBytes bounds the serialized audit payload; over budget the
	// actions fall back to a minimal step/tool/status form.
	maxAuditActionBytes = 8 * 1024
)

// agentAuditActions returns a bounded, JSON-plain copy of the protocol actions
// for the runtime-event audit trail. Args are already redacted by the agent
// protocol layer.
func agentAuditActions(actions any) any {
	items := agentActionSlice(actions)
	if len(items) == 0 {
		return nil
	}
	if len(items) > maxAuditActions {
		items = items[len(items)-maxAuditActions:]
	}
	if data, err := json.Marshal(items); err == nil && len(data) <= maxAuditActionBytes {
		return items
	}
	minimal := make([]any, 0, len(items))
	for _, item := range items {
		action, ok := item.(map[string]any)
		if !ok {
			continue
		}
		entry := map[string]any{
			"step":   action["step"],
			"source": action["source"],
			"tool":   action["tool"],
			"status": action["status"],
		}
		if errText, ok := action["error"].(string); ok && errText != "" {
			entry["error"] = errText
		}
		minimal = append(minimal, entry)
	}
	return minimal
}

func agentActionCount(actions any) int {
	return len(agentActionSlice(actions))
}

// agentActionSlice normalizes the response actions (typed protocol structs or
// already-decoded JSON) into a []any of plain maps.
func agentActionSlice(actions any) []any {
	if actions == nil {
		return nil
	}
	if items, ok := actions.([]any); ok {
		allMaps := true
		for _, item := range items {
			if _, ok := item.(map[string]any); !ok {
				allMaps = false
				break
			}
		}
		if allMaps {
			return items
		}
	}
	data, err := json.Marshal(actions)
	if err != nil {
		return nil
	}
	var items []any
	if err := json.Unmarshal(data, &items); err != nil {
		return nil
	}
	return items
}

// agentViolationCodes extracts protocol violation codes from the response
// evidence for the audit event.
func agentViolationCodes(resp gjagent.Response) []string {
	data, err := json.Marshal(resp.Evidence)
	if err != nil {
		return nil
	}
	var evidence map[string]any
	if err := json.Unmarshal(data, &evidence); err != nil {
		return nil
	}
	if protocol, ok := evidence["protocol"].(map[string]any); ok {
		evidence = protocol
	}
	violations, _ := evidence["violations"].([]any)
	var codes []string
	for _, item := range violations {
		violation, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if code, ok := violation["code"].(string); ok && code != "" {
			codes = append(codes, code)
		}
	}
	return codes
}
