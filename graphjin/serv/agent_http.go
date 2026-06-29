package serv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/dosco/graphjin/auth/v3"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

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

// extendDeadlineForAgent lifts the per-request read/write deadlines so the
// server-owned Ax agent can run up to its configured timeout without the global
// http.Server WriteTimeout closing the connection before a JSON response is
// written.
func extendDeadlineForAgent(w http.ResponseWriter, conf *Config) {
	timeoutSecs := 30
	if conf != nil && conf.Agent.TimeoutSeconds > 0 {
		timeoutSecs = conf.Agent.TimeoutSeconds
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
