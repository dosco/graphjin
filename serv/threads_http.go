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

func (s1 *HttpService) AgentThreads(ah auth.HandlerFunc) http.Handler {
	return apiV1Handler(s1, nil, s1.apiV1AgentThreads(), ah)
}

func (s1 *HttpService) AgentThreadsWithNS(ah auth.HandlerFunc, ns string) http.Handler {
	return apiV1Handler(s1, &ns, s1.apiV1AgentThreads(), ah)
}

// apiV1AgentThreads serves thread and run reads, run streams and cancels:
//
//	GET    /api/v1/agent/threads              list the caller's threads
//	POST   /api/v1/agent/threads              create a thread
//	GET    /api/v1/agent/threads/{id}         a thread with its messages
//	PATCH  /api/v1/agent/threads/{id}         rename, archive or change profile
//	DELETE /api/v1/agent/threads/{id}         delete with its messages and runs
//	GET    /api/v1/agent/runs/{id}            a run's status
//	GET    /api/v1/agent/runs/{id}/events     replay and follow a run (SSE)
//	POST   /api/v1/agent/runs/{id}/cancel     cancel a run on any replica
func (s1 *HttpService) apiV1AgentThreads() http.Handler {
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
			writeJSONStatus(w, http.StatusUnauthorized, map[string]any{"error": "agent threads require a signed-in user"})
			return
		}
		if path, ok := strings.CutPrefix(r.URL.Path, routeAgentRuns); ok {
			s.handleAgentRunsREST(ctx, w, r, strings.Trim(path, "/"))
			return
		}
		path := strings.Trim(strings.TrimPrefix(r.URL.Path, routeAgentThreads), "/")
		switch {
		case path == "" && r.Method == http.MethodGet:
			rows, err := s.listThreads(ctx, r.URL.Query().Get("archived") == "true")
			writeThreadResult(w, http.StatusOK, rows, err)
		case path == "" && r.Method == http.MethodPost:
			var input struct {
				Title   string `json:"title"`
				Profile string `json:"profile"`
			}
			if err := decodeJSON(r, &input); err != nil {
				writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
				return
			}
			row, err := s.createThread(ctx, threadSpec{Title: input.Title, Profile: input.Profile})
			writeThreadResult(w, http.StatusCreated, row, err)
		case path != "" && !strings.Contains(path, "/") && r.Method == http.MethodGet:
			thread, err := s.threadForCaller(ctx, path)
			if err == nil {
				var messages []map[string]any
				messages, err = s.threadMessages(ctx, path)
				thread["messages"] = messages
			}
			writeThreadResult(w, http.StatusOK, thread, err)
		case path != "" && !strings.Contains(path, "/") && r.Method == http.MethodPatch:
			input, err := readJSONMap(r)
			if err != nil {
				writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
				return
			}
			row, err := s.updateThread(ctx, path, input)
			writeThreadResult(w, http.StatusOK, row, err)
		case path != "" && !strings.Contains(path, "/") && r.Method == http.MethodDelete:
			err := s.deleteThread(ctx, path)
			writeThreadResult(w, http.StatusOK, map[string]any{"id": path, "deleted": err == nil}, err)
		default:
			writeJSONStatus(w, http.StatusNotFound, map[string]any{"error": "not found"})
		}
	})
}

func (s *graphjinService) handleAgentRunsREST(ctx context.Context, w http.ResponseWriter, r *http.Request, path string) {
	runID, action, _ := strings.Cut(path, "/")
	switch {
	case runID != "" && action == "" && r.Method == http.MethodGet:
		row, err := s.runForCaller(ctx, runID)
		if err == nil {
			writeThreadResult(w, http.StatusOK, runAPIRow(row), nil)
			return
		}
		writeThreadResult(w, http.StatusOK, nil, err)
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
