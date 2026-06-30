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
	Status           string   `json:"status"`
	Enabled          bool     `json:"enabled"`
	Ready            bool     `json:"ready"`
	Endpoint         string   `json:"endpoint"`
	MCPTool          string   `json:"mcp_tool"`
	Provider         string   `json:"provider"`
	Model            string   `json:"model,omitempty"`
	APIKeyEnv        string   `json:"api_key_env"`
	APIKeyConfigured bool     `json:"api_key_configured"`
	MaxSteps         int      `json:"max_steps"`
	TimeoutSeconds   int      `json:"timeout_seconds"`
	Modes            []string `json:"modes"`
	AllowRawGraphQL  bool     `json:"allow_raw_graphql"`
	AllowMutations   bool     `json:"allow_mutations"`
	ReturnTrace      bool     `json:"return_trace"`
	Namespace        string   `json:"namespace,omitempty"`
	Message          string   `json:"message"`
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

		_ = json.NewEncoder(w).Encode(agentStatusFromConfig(agentConfigFromService(s.conf), ns))
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
		if ns != nil && req.Namespace == "" {
			req.Namespace = *ns
		}

		ctx = s.applyIdentityContext(ctx)
		// Capabilities is server-derived and json:"-", so it is never taken from the
		// request body; set it from the caller's identity context.
		req.Capabilities = s.agentCapabilityProfile(ctx)
		runner, err := newGraphJinAgentRunner(s, agentConfigFromService(s.conf))
		var resp gjagent.Response
		if err == nil {
			resp, err = runner.Run(ctx, req)
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
				attribute.String("agent.status", resp.Status),
				attribute.String("agent.mode", req.Mode))
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(resp)
	}
	return http.HandlerFunc(h)
}

func agentStatusFromConfig(conf gjagent.Config, ns *string) agentStatusResponse {
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
	status := "ready"
	message := "GraphJin agent is ready."
	ready := conf.Enabled && apiKeyConfigured
	switch {
	case !conf.Enabled:
		status = "disabled"
		message = "Enable agent.enabled and configure " + apiKeyEnv + " to chat with GraphJin."
	case !apiKeyConfigured:
		status = "missing_key"
		message = "GraphJin agent is enabled but " + apiKeyEnv + " is not set."
	}

	resp := agentStatusResponse{
		Status:           status,
		Enabled:          conf.Enabled,
		Ready:            ready,
		Endpoint:         routeAgent,
		MCPTool:          mcpToolAskGraphJinAgent,
		Provider:         provider,
		Model:            strings.TrimSpace(conf.Model),
		APIKeyEnv:        apiKeyEnv,
		APIKeyConfigured: apiKeyConfigured,
		MaxSteps:         maxSteps,
		TimeoutSeconds:   timeoutSeconds,
		Modes:            []string{gjagent.ModeSafe, gjagent.ModeDiscoveryOnly, gjagent.ModeRawAllowed},
		AllowRawGraphQL:  conf.AllowRawGraphQL,
		AllowMutations:   conf.AllowMutations,
		ReturnTrace:      conf.ReturnTrace,
		Message:          message,
	}
	if ns != nil {
		resp.Namespace = *ns
	}
	return resp
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
	case errors.Is(err, gjagent.ErrMissingInstruction), errors.Is(err, gjagent.ErrInvalidMode):
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
			"agent_status": resp.Status,
			"mode":         req.Mode,
			"namespace":    req.Namespace,
			"has_context":  len(req.Context) != 0,
			"return_trace": req.ReturnTrace != nil && *req.ReturnTrace,
		},
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
