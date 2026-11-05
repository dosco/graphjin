package serv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/dosco/graphjin/auth/v3"
)

const runEventPollInterval = 500 * time.Millisecond

// threadAgentResponse is the agent response of a threaded run, tagged with the
// thread and run it belongs to.
type threadAgentResponse struct {
	gjagent.Response
	ThreadID string `json:"thread_id"`
	RunID    string `json:"run_id"`
}

func (s1 *HttpService) AgentRuns(ah auth.HandlerFunc) http.Handler {
	return apiV1Handler(s1, nil, s1.apiV1AgentRuns(), ah)
}

func (s1 *HttpService) AgentRunsWithNS(ah auth.HandlerFunc, ns string) http.Handler {
	return apiV1Handler(s1, &ns, s1.apiV1AgentRuns(), ah)
}

// apiV1AgentRuns serves a running turn's live actions:
//
//	GET  /api/v1/agent/runs/{id}/events  stream a run's events (SSE), resumable with ?after=
//	POST /api/v1/agent/runs/{id}/cancel  cancel a run on any replica
//
// Threads, their messages and runs are the gj_thread, gj_thread_message and
// gj_thread_run GraphQL roots; a turn starts at POST /api/v1/agent.
func (s1 *HttpService) apiV1AgentRuns() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := s1.Load().(*graphjinService)
		w.Header().Set("Content-Type", "application/json")
		if !s.threadsEnabled() {
			writeJSONStatus(w, http.StatusNotFound, map[string]any{"error": "agent threads are disabled"})
			return
		}
		if err := s.checkGraphJinInitialized(); err != nil {
			renderErr(w, err)
			return
		}
		ctx := s.applyIdentityContext(r.Context())
		if _, ok := artifactUserID(ctx); !ok {
			writeJSONStatus(w, http.StatusUnauthorized, map[string]any{"error": "agent runs require a signed-in user"})
			return
		}
		path, _ := strings.CutPrefix(r.URL.Path, routeAgentRuns)
		s.handleAgentRunsREST(ctx, w, r, strings.Trim(path, "/"))
	})
}

func (s *graphjinService) handleAgentRunsREST(ctx context.Context, w http.ResponseWriter, r *http.Request, path string) {
	runID, action, _ := strings.Cut(path, "/")
	switch {
	case runID != "" && action == "events" && r.Method == http.MethodGet:
		s.streamRunEvents(ctx, w, r, runID)
	case runID != "" && action == "cancel" && r.Method == http.MethodPost:
		row, err := s.requestRunCancel(ctx, runID)
		writeThreadResult(w, http.StatusAccepted, row, err)
	default:
		writeJSONStatus(w, http.StatusNotFound, map[string]any{"error": "not found"})
	}
}

func writeThreadResult(w http.ResponseWriter, status int, data any, err error) {
	if err != nil {
		writeJSONStatus(w, threadErrorStatus(err), map[string]any{"error": err.Error()})
		return
	}
	writeJSONStatus(w, status, map[string]any{"data": data})
}

func threadErrorStatus(err error) int {
	switch {
	case errors.Is(err, errThreadNotFound), errors.Is(err, errRunNotFound):
		return http.StatusNotFound
	case errors.Is(err, errThreadBusy):
		return http.StatusConflict
	case errors.Is(err, errThreadLimit):
		return http.StatusTooManyRequests
	default:
		return http.StatusBadRequest
	}
}

// streamRunEvents replays a run's stored events after the Last-Event-ID (or
// ?after=N) and follows new ones until the run finishes. Any replica can serve
// it, because the events live in the store.
func (s *graphjinService) streamRunEvents(ctx context.Context, w http.ResponseWriter, r *http.Request, runID string) {
	if _, err := s.runForCaller(ctx, runID); err != nil {
		writeThreadResult(w, http.StatusOK, nil, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": "streaming unsupported"})
		return
	}
	after := int64(0)
	if v := r.Header.Get("Last-Event-ID"); v != "" {
		after, _ = strconv.ParseInt(v, 10, 64)
	} else if v := r.URL.Query().Get("after"); v != "" {
		after, _ = strconv.ParseInt(v, 10, 64)
	}
	startSSE(w)
	ticker := time.NewTicker(runEventPollInterval)
	defer ticker.Stop()
	for {
		events, err := s.runEventsAfter(ctx, runID, after)
		if err != nil {
			writeSSE(w, flusher, 0, "error", map[string]any{"error": err.Error()})
			return
		}
		for _, event := range events {
			writeSSERaw(w, flusher, event.Seq, event.Kind, event.Payload)
			after = event.Seq
			if event.Kind == "complete" {
				return
			}
		}
		if len(events) == 0 {
			row, err := s.runStoreRow(ctx, runID)
			if err != nil || row == nil {
				return
			}
			if status := stringMapValue(row, "status"); runIsTerminal(status) {
				// A lost run has no complete event: the sweep only fails it.
				writeSSE(w, flusher, 0, "complete", map[string]any{"status": status})
				return
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func startSSE(w http.ResponseWriter) {
	clearStreamDeadline(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, seq int64, event string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	writeSSERaw(w, flusher, seq, event, data)
}

func writeSSERaw(w http.ResponseWriter, flusher http.Flusher, seq int64, event string, data []byte) {
	if seq > 0 {
		_, _ = fmt.Fprintf(w, "id: %d\n", seq)
	}
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
	flusher.Flush()
}

// agentThreadTurn serves POST /api/v1/agent with a thread_id: the turn is
// recorded in the thread and streamed (SSE) or returned (JSON) as usual.
func (s *graphjinService) agentThreadTurn(ctx context.Context, w http.ResponseWriter, r *http.Request,
	threadID string, req gjagent.Request, taskWarm taskWarmStart, runner graphjinAgentRunner, start time.Time,
) {
	run, err := s.prepareThreadRun(ctx, threadID, &req)
	if err != nil {
		writeThreadResult(w, http.StatusOK, nil, err)
		return
	}
	settle := func(ctx context.Context, resp gjagent.Response, err error) (gjagent.Response, error) {
		return s.settleAgentResponse(ctx, req, taskWarm, resp, err)
	}
	var emit threadRunEmit
	sse := isSSERequest(r)
	if sse {
		flusher, ok := w.(http.Flusher)
		if !ok {
			writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": "streaming unsupported"})
			return
		}
		startSSE(w)
		writeSSE(w, flusher, 0, "run", map[string]any{"thread_id": threadID, "run_id": run.runID})
		// Writes stop once the client goes away; the run itself carries on.
		emit = func(seq int64, kind string, payload any) {
			if r.Context().Err() == nil {
				writeSSE(w, flusher, seq, kind, payload)
			}
		}
	}
	resp, runErr := run.execute(ctx, runner, req, settle, emit)
	s.appendTaskTrailEntry(ctx, req, resp, time.Since(start), runErr)
	recordAgentRuntimeEvent(s, ctx, req, resp, taskWarm, time.Since(start), runErr)
	if sse {
		return
	}
	status := http.StatusOK
	if runErr != nil {
		status = agentHTTPStatus(runErr)
	}
	writeJSONStatus(w, status, threadAgentResponse{Response: resp, ThreadID: threadID, RunID: run.runID})
}

// settleAgentResponse turns a runner result into the response a caller sees:
// notices on success, a sanitized public error on failure.
func (s *graphjinService) settleAgentResponse(ctx context.Context, req gjagent.Request, taskWarm taskWarmStart, resp gjagent.Response, err error) (gjagent.Response, error) {
	agentConf := agentConfigFromService(s.conf)
	if err != nil {
		info, publicErr := agentPublicFailure(agentConf, err)
		return gjagent.Response{Status: gjagent.StatusError, Errors: []gjagent.ErrorInfo{info}}, publicErr
	}
	s.appendWatchNotices(ctx, &resp)
	s.appendTaskNotices(ctx, req, &resp)
	s.appendAnnotationNotices(ctx, &resp)
	appendTaskContextNotice(taskWarm, &resp)
	return sanitizeAgentResponse(agentConf, resp), nil
}
