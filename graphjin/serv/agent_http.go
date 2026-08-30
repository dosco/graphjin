package serv

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/dosco/graphjin/auth/v3"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	defaultAgentProvider       = "openai"
	defaultAgentAPIKeyEnv      = "OPENAI_API_KEY"
	defaultAgentMaxSteps       = 8
	defaultAgentTimeoutSeconds = 50
)

type agentStatusResponse struct {
	Status               string                  `json:"status"`
	Enabled              bool                    `json:"enabled"`
	Ready                bool                    `json:"ready"`
	RESTReady            bool                    `json:"rest_ready"`
	ServerModelReady     bool                    `json:"server_model_ready"`
	Endpoint             string                  `json:"endpoint"`
	MCPTool              string                  `json:"mcp_tool"`
	Provider             string                  `json:"provider"`
	Model                string                  `json:"model,omitempty"`
	APIKeyEnv            string                  `json:"api_key_env"`
	APIKeyConfigured     bool                    `json:"api_key_configured"`
	ResponseFormat       string                  `json:"response_format"`
	StructuredOutputMode string                  `json:"structured_output_mode"`
	ServiceTier          string                  `json:"service_tier"`
	MaxSteps             int                     `json:"max_steps"`
	Reasoning            string                  `json:"reasoning,omitempty"`
	ShowThoughts         bool                    `json:"show_thoughts"`
	RateLimit            gjagent.RateLimitConfig `json:"rate_limit"`
	TimeoutSeconds       int                     `json:"timeout_seconds"`
	ReadOnly             bool                    `json:"read_only"`
	ReturnTrace          bool                    `json:"return_trace"`
	Namespace            string                  `json:"namespace,omitempty"`
	RoleClass            string                  `json:"role_class,omitempty"`
	AllowedActions       []string                `json:"allowed_actions,omitempty"`
	AvailableSystemRoots []string                `json:"available_system_roots,omitempty"`
	BlockedSystemRoots   []string                `json:"blocked_system_roots,omitempty"`
	EvalFingerprint      string                  `json:"eval_fingerprint,omitempty"`
	Message              string                  `json:"message"`
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

		status := agentStatusFromConfig(agentConfigFromService(s.conf), ns, s.agentClientFactory != nil)
		if profile := s.agentCapabilityProfile(r.Context()); profile != nil {
			status.RoleClass = profile.RoleClass
			status.AllowedActions = append([]string(nil), profile.AllowedActions...)
			status.AvailableSystemRoots = append([]string(nil), profile.AvailableSystemRoots...)
			status.BlockedSystemRoots = append([]string(nil), profile.BlockedSystemRoots...)
		}
		status.EvalFingerprint = agentEvalFingerprint(status)
		_ = json.NewEncoder(w).Encode(status)
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
				secret := ""
				if s != nil {
					secret = configuredAgentSecret(agentConfigFromService(s.conf))
				}
				message := gjagent.SanitizeText(fmt.Sprintf("agent handler panic: %v", recovered), secret)
				if s != nil && s.log != nil {
					s.log.Errorf("%s", message)
				}
				writeAgentError(w, http.StatusInternalServerError, message)
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
		taskWarm, resolveErr := s.resolveAgentTaskContext(ctx, &req)
		err = resolveErr
		taskResolved := err == nil
		var runner graphjinAgentRunner
		if err == nil {
			runner, err = newGraphJinAgentRunner(s, agentConfigFromService(s.conf))
		}
		if err == nil && isSSERequest(r) {
			s.agentSSE(ctx, w, req, taskWarm, runner, start)
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
				s.appendTaskNotices(ctx, req, &resp)
				s.appendAnnotationNotices(ctx, &resp)
				appendTaskContextNotice(taskWarm, &resp)
			}
		}
		status := agentHTTPStatus(err)
		agentConf := agentConfigFromService(s.conf)
		if err != nil {
			info, publicErr := agentPublicFailure(agentConf, err)
			spanError(span, publicErr)
			err = publicErr
			resp = gjagent.Response{
				Status: gjagent.StatusError,
				Errors: []gjagent.ErrorInfo{info},
			}
		} else {
			resp = sanitizeAgentResponse(agentConf, resp)
		}
		if taskResolved {
			s.appendTaskTrailEntry(ctx, req, resp, time.Since(start), err)
		}
		recordAgentRuntimeEvent(s, ctx, req, resp, taskWarm, time.Since(start), err)
		recordAgentUsageObservability(s, span, "rest", agentConf, resp, time.Since(start))

		if span.IsRecording() {
			attrs := []attribute.KeyValue{
				attribute.String("http.path", r.RequestURI),
				attribute.String("http.method", r.Method),
				attribute.String("agent.status", resp.Status),
			}
			if code := agentResponseErrorCode(resp); code != "" {
				attrs = append(attrs, attribute.String("agent.error_code", code))
			}
			span.SetAttributes(attrs...)
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
	status := "ready"
	message := "GraphJin agent is ready."
	ready := conf.Enabled && serverModelReady
	switch {
	case !conf.Enabled:
		status = "disabled"
		message = "Enable agent.enabled and configure GraphJin-owned model credentials in " + apiKeyEnv + "."
	case !serverModelReady:
		status = "missing_key"
		message = "GraphJin agent is enabled but " + apiKeyEnv + " is not set."
	}

	resp := agentStatusResponse{
		Status:               status,
		Enabled:              conf.Enabled,
		Ready:                ready,
		RESTReady:            conf.Enabled && serverModelReady,
		ServerModelReady:     serverModelReady,
		Endpoint:             routeAgent,
		MCPTool:              mcpToolAskGraphJinAgent,
		Provider:             provider,
		Model:                strings.TrimSpace(conf.Model),
		APIKeyEnv:            apiKeyEnv,
		APIKeyConfigured:     apiKeyConfigured,
		ResponseFormat:       strings.TrimSpace(conf.ResponseFormat),
		StructuredOutputMode: gjagent.EffectiveStructuredOutputMode(conf.StructuredOutputMode, conf.ResponseFormat),
		ServiceTier:          gjagent.EffectiveServiceTier(conf.ServiceTier),
		MaxSteps:             maxSteps,
		Reasoning:            strings.TrimSpace(conf.Reasoning),
		ShowThoughts:         conf.ShowThoughts,
		RateLimit:            conf.RateLimit,
		TimeoutSeconds:       timeoutSeconds,
		ReadOnly:             conf.ReadOnly,
		ReturnTrace:          conf.ReturnTrace,
		Message:              message,
	}
	if ns != nil {
		resp.Namespace = *ns
	}
	resp.EvalFingerprint = agentEvalFingerprint(resp)
	return resp
}

// agentSSE streams the agent run over Server-Sent Events: one `action` event
// per executed tool call, then a single `result` event carrying the final
// agent Response, then `complete`. Requested via Accept: text/event-stream;
// the default JSON contract is unchanged.
func (s *graphjinService) agentSSE(ctx context.Context, w http.ResponseWriter, req gjagent.Request, taskWarm taskWarmStart, runner graphjinAgentRunner, start time.Time) {
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

	agentConf := agentConfigFromService(s.conf)
	secret := configuredAgentSecret(agentConf)
	writeEvent := func(event string, payload any) {
		payload = gjagent.SanitizeValue(payload, secret)
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
		info, publicErr := agentPublicFailure(agentConf, err)
		err = publicErr
		resp = gjagent.Response{
			Status: gjagent.StatusError,
			Errors: []gjagent.ErrorInfo{info},
		}
	} else {
		s.appendWatchNotices(ctx, &resp)
		s.appendTaskNotices(ctx, req, &resp)
		s.appendAnnotationNotices(ctx, &resp)
		appendTaskContextNotice(taskWarm, &resp)
		resp = sanitizeAgentResponse(agentConf, resp)
	}
	s.appendTaskTrailEntry(ctx, req, resp, time.Since(start), err)
	recordAgentRuntimeEvent(s, ctx, req, resp, taskWarm, time.Since(start), err)
	span := trace.SpanFromContext(ctx)
	recordAgentUsageObservability(s, span, "sse", agentConf, resp, time.Since(start))
	if span.IsRecording() {
		attrs := []attribute.KeyValue{attribute.String("agent.status", resp.Status), attribute.Bool("agent.sse", true)}
		if code := agentResponseErrorCode(resp); code != "" {
			attrs = append(attrs, attribute.String("agent.error_code", code))
		}
		span.SetAttributes(attrs...)
		if err != nil {
			spanError(span, err)
		}
	}
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
			Message: gjagent.SanitizeText(message),
		}},
	})
}

func configuredAgentSecret(conf gjagent.Config) string {
	apiKeyEnv := strings.TrimSpace(conf.APIKeyEnv)
	if apiKeyEnv == "" {
		apiKeyEnv = defaultAgentAPIKeyEnv
	}
	return os.Getenv(apiKeyEnv)
}

func agentPublicFailure(conf gjagent.Config, err error) (gjagent.ErrorInfo, error) {
	secret := configuredAgentSecret(conf)
	info := gjagent.PublicErrorInfo(err, conf.Provider, conf.Model, secret)
	return info, errors.New(info.Message)
}

func sanitizeAgentResponse(conf gjagent.Config, resp gjagent.Response) gjagent.Response {
	return gjagent.SanitizeResponse(resp, configuredAgentSecret(conf))
}

func recordAgentUsageObservability(s *graphjinService, span trace.Span, surface string, conf gjagent.Config, resp gjagent.Response, duration time.Duration) {
	if resp.Usage == nil {
		return
	}
	usage := gjagent.SummarizeUsage(resp.Usage)
	provider := strings.TrimSpace(conf.Provider)
	if provider == "" {
		provider = defaultAgentProvider
	}
	if s != nil && s.log != nil {
		s.log.Infow("GraphJin agent usage",
			"surface", surface,
			"status", resp.Status,
			"provider", provider,
			"model", conf.Model,
			"prompt_tokens", usage.PromptTokens,
			"completion_tokens", usage.CompletionTokens,
			"total_tokens", usage.TotalTokens,
			"llm_calls", usage.LLMCalls,
			"duration_ms", duration.Milliseconds(),
		)
	}
	if span != nil && span.IsRecording() {
		span.SetAttributes(
			attribute.Int64("agent.prompt_tokens", usage.PromptTokens),
			attribute.Int64("agent.completion_tokens", usage.CompletionTokens),
			attribute.Int64("agent.total_tokens", usage.TotalTokens),
			attribute.Int64("agent.llm_calls", usage.LLMCalls),
		)
	}
}

func agentEvalFingerprint(status agentStatusResponse) string {
	available := append([]string(nil), status.AvailableSystemRoots...)
	blocked := append([]string(nil), status.BlockedSystemRoots...)
	allowedActions := append([]string(nil), status.AllowedActions...)
	sort.Strings(available)
	sort.Strings(blocked)
	sort.Strings(allowedActions)
	payload := struct {
		Version              string                  `json:"version"`
		Build                string                  `json:"build"`
		PromptRegistryHash   string                  `json:"prompt_registry_hash"`
		Provider             string                  `json:"provider"`
		Model                string                  `json:"model"`
		APIKeyEnv            string                  `json:"api_key_env"`
		ResponseFormat       string                  `json:"response_format,omitempty"`
		StructuredOutputMode string                  `json:"structured_output_mode"`
		ServiceTier          string                  `json:"service_tier"`
		MaxSteps             int                     `json:"max_steps"`
		Reasoning            string                  `json:"reasoning,omitempty"`
		ShowThoughts         bool                    `json:"show_thoughts"`
		RateLimit            gjagent.RateLimitConfig `json:"rate_limit"`
		TimeoutSeconds       int                     `json:"timeout_seconds"`
		ReadOnly             bool                    `json:"read_only"`
		ReturnTrace          bool                    `json:"return_trace"`
		Namespace            string                  `json:"namespace"`
		RoleClass            string                  `json:"role_class"`
		AllowedActions       []string                `json:"allowed_actions"`
		AvailableSystemRoots []string                `json:"available_system_roots"`
		BlockedSystemRoots   []string                `json:"blocked_system_roots"`
	}{
		Version: version, Build: agentBuildIdentity(), PromptRegistryHash: gjagent.PromptRegistryHash(),
		Provider: status.Provider, Model: status.Model, APIKeyEnv: status.APIKeyEnv,
		ResponseFormat: status.ResponseFormat, StructuredOutputMode: status.StructuredOutputMode, ServiceTier: status.ServiceTier,
		MaxSteps: status.MaxSteps, Reasoning: status.Reasoning, ShowThoughts: status.ShowThoughts,
		RateLimit: status.RateLimit, TimeoutSeconds: status.TimeoutSeconds,
		ReadOnly: status.ReadOnly, ReturnTrace: status.ReturnTrace,
		Namespace: status.Namespace, RoleClass: status.RoleClass,
		AllowedActions:       allowedActions,
		AvailableSystemRoots: available, BlockedSystemRoots: blocked,
	}
	data, _ := json.Marshal(payload)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func agentBuildIdentity() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	parts := []string{info.Main.Path + "@" + info.Main.Version}
	for _, dep := range info.Deps {
		switch dep.Path {
		case "github.com/ax-llm/ax/packages/go", "github.com/dosco/graphjin/agent/v3", "github.com/dosco/graphjin/serv/v3":
			parts = append(parts, dep.Path+"@"+dep.Version)
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, ";")
}

func agentHTTPStatus(err error) int {
	switch {
	case err == nil:
		return http.StatusOK
	case errors.Is(err, gjagent.ErrMissingInstruction),
		errors.Is(err, gjagent.ErrInstructionTooLong),
		errors.Is(err, errTaskNotFoundOrClosed):
		return http.StatusBadRequest
	case errors.Is(err, gjagent.ErrMissingAPIKey):
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

func recordAgentRuntimeEvent(s *graphjinService, ctx context.Context, req gjagent.Request, resp gjagent.Response, taskWarm taskWarmStart, duration time.Duration, err error) {
	if s == nil {
		return
	}
	conf := agentConfigFromService(s.conf)
	resp = sanitizeAgentResponse(conf, resp)
	if err != nil {
		_, err = agentPublicFailure(conf, err)
	}
	event := runtimeEvent{
		Kind:       runtimeKindEvent,
		Phase:      "agent",
		Status:     runtimeStatusReady,
		Severity:   "info",
		Summary:    "GraphJin agent request completed",
		DurationMS: duration.Milliseconds(),
		Details: map[string]any{
			"agent_status":        resp.Status,
			"task_id":             req.TaskID,
			"task_entries_loaded": taskWarm.EntriesLoaded,
			"namespace":           req.Namespace,
			"has_context":         len(req.Context) != 0,
			"history_turns":       len(req.History),
			"return_trace":        req.ReturnTrace != nil && *req.ReturnTrace,
		},
	}
	// Audit trail: what the agent actually did, independent of return_trace.
	if len(resp.Skills) != 0 {
		event.Details["skills"] = resp.Skills
	}
	if resp.Skill != "" {
		// Deprecated v3 alias retained for existing audit consumers.
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
		event.ErrorCode = agentResponseErrorCode(resp)
		if event.ErrorCode == "" {
			event.ErrorCode = gjagent.ErrorCodeAgentError
		}
		event.Details["error"] = err.Error()
	} else if len(resp.Errors) != 0 {
		event.Status = runtimeStatusDegraded
		event.Severity = "warn"
		event.Summary = "GraphJin agent returned errors"
	}
	s.recordRuntimeEvent(ctx, event)
}

func agentResponseErrorCode(resp gjagent.Response) string {
	for _, responseError := range resp.Errors {
		if code, ok := responseError.Extensions["code"].(string); ok && strings.TrimSpace(code) != "" {
			return strings.TrimSpace(code)
		}
	}
	return ""
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
	return gjagent.ProtocolViolationCodes(resp)
}
