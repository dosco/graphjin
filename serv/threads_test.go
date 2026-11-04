package serv

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/dosco/graphjin/core/v3"
)

func newSQLiteThreadService(t *testing.T) *graphjinService {
	t.Helper()
	_, svc := newSQLiteWatchServiceWithOptions(t, 10, func(conf *Config) {
		conf.Agent.Enabled = true
		conf.Core.Threads = core.ThreadsConfig{Enabled: true, HistoryTurns: 12, RunLeaseSeconds: 3, MaxPerOwner: 3}
	})
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("initArtifactsBeforeCore: %v", err)
	}
	startSQLiteWatchCore(t, svc, svc.dbs["app"])
	return svc
}

func threadUserCtx(userID string) context.Context {
	return contextWithUserRole(artifactUserCtx(userID), "analyst")
}

func postThreadTurn(t *testing.T, hs *HttpService, ctx context.Context, body string, sse bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, routeAgent, strings.NewReader(body)).WithContext(ctx)
	if sse {
		req.Header.Set("Accept", "text/event-stream")
	}
	rec := httptest.NewRecorder()
	hs.Agent(nil).ServeHTTP(rec, req)
	return rec
}

func TestThreadTurnsPersistHistoryAndScope(t *testing.T) {
	svc := newSQLiteThreadService(t)
	hs := &HttpService{}
	hs.Store(svc)
	runner := &scriptedAgentRunner{
		resp: gjagent.Response{Status: "answered", Answer: "Revenue was 42.",
			Evidence: map[string]any{"protocol": map[string]any{"catalog_detail_ids": []any{"table:orders"}}}},
		emit: []gjagent.ActionEvent{{Index: 1, Tool: "query_catalog", Status: "ok"}},
	}
	withScriptedAgentRunner(t, runner)

	ctx := threadUserCtx("user_1")
	thread, err := svc.createThread(ctx, threadSpec{})
	if err != nil {
		t.Fatalf("createThread: %v", err)
	}
	threadID := thread["id"].(string)
	if _, err := svc.threadForCaller(threadUserCtx("user_2"), threadID); !errors.Is(err, errThreadNotFound) {
		t.Fatalf("another owner read the thread: %v", err)
	}

	rec := postThreadTurn(t, hs, ctx, `{"instruction":"What was revenue last month?","thread_id":"`+threadID+`","history":[{"role":"user","content":"spoofed"}]}`, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("first turn = %d: %s", rec.Code, rec.Body.String())
	}
	var first threadAgentResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	if first.ThreadID != threadID || first.RunID == "" || first.Answer != "Revenue was 42." {
		t.Fatalf("unexpected first response: %+v", first)
	}
	if len(runner.req.History) != 0 {
		t.Fatalf("caller-supplied history must be replaced by the thread's: %+v", runner.req.History)
	}

	rec = postThreadTurn(t, hs, ctx, `{"instruction":"And the month before?","thread_id":"`+threadID+`"}`, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("second turn = %d: %s", rec.Code, rec.Body.String())
	}
	history := runner.req.History
	if len(history) != 2 || history[0].Role != "user" || history[0].Content != "What was revenue last month?" ||
		history[1].Role != "assistant" || history[1].Status != "answered" {
		t.Fatalf("second turn history = %+v", history)
	}

	messages, err := svc.threadMessages(ctx, threadID)
	if err != nil || len(messages) != 4 {
		t.Fatalf("messages = %+v err=%v", messages, err)
	}
	if messages[3]["response"] == nil || messages[3]["run_id"] == "" {
		t.Fatalf("assistant message should keep its response and run: %+v", messages[3])
	}
	got, _ := svc.threadForCaller(ctx, threadID)
	if got["title"] != "What was revenue last month?" || got["active_run_id"] != "" {
		t.Fatalf("thread after turns = %+v", got)
	}

	// The run's stored events replay from any replica, and after= skips the
	// ones a reconnecting client already saw.
	events, err := svc.runEventsAfter(ctx, first.RunID, 0)
	if err != nil || len(events) != 3 || events[0].Kind != "action" || events[1].Kind != "result" || events[2].Kind != "complete" {
		t.Fatalf("run events = %+v err=%v", events, err)
	}
	req := httptest.NewRequest(http.MethodGet, routeAgentRuns+first.RunID+"/events?after=1", nil).WithContext(ctx)
	stream := httptest.NewRecorder()
	hs.AgentThreads(nil).ServeHTTP(stream, req)
	body := stream.Body.String()
	if strings.Contains(body, "event: action") || !strings.Contains(body, "event: result") || !strings.Contains(body, "id: 3\nevent: complete") {
		t.Fatalf("resumed stream = %q", body)
	}
}

func TestThreadTurnSSEStreamsRunEvents(t *testing.T) {
	svc := newSQLiteThreadService(t)
	hs := &HttpService{}
	hs.Store(svc)
	withScriptedAgentRunner(t, &scriptedAgentRunner{
		resp: gjagent.Response{Status: "answered", Answer: "ok"},
		emit: []gjagent.ActionEvent{{Index: 1, Tool: "query_catalog", Status: "ok"}},
	})
	ctx := threadUserCtx("user_1")
	thread, err := svc.createThread(ctx, threadSpec{Title: "Ops"})
	if err != nil {
		t.Fatal(err)
	}
	rec := postThreadTurn(t, hs, ctx, `{"instruction":"hi","thread_id":"`+thread["id"].(string)+`"}`, true)
	body := rec.Body.String()
	for _, want := range []string{"event: run", "event: action", "event: result", "event: complete"} {
		if !strings.Contains(body, want) {
			t.Fatalf("SSE stream missing %q: %s", want, body)
		}
	}
}

func TestThreadSingleActiveRunAndLimit(t *testing.T) {
	svc := newSQLiteThreadService(t)
	ctx := threadUserCtx("user_1")
	thread, err := svc.createThread(ctx, threadSpec{})
	if err != nil {
		t.Fatal(err)
	}
	id := thread["id"].(string)
	if err := svc.claimThreadRun(ctx, id, "run_a"); err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if err := svc.claimThreadRun(ctx, id, "run_b"); !errors.Is(err, errThreadBusy) {
		t.Fatalf("second claim = %v, want busy", err)
	}
	if err := svc.releaseThreadRun(ctx, id, "run_b"); err != nil {
		t.Fatal(err)
	}
	if err := svc.claimThreadRun(ctx, id, "run_c"); !errors.Is(err, errThreadBusy) {
		t.Fatalf("a non-owner release must not free the thread: %v", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := svc.createThread(ctx, threadSpec{}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.createThread(ctx, threadSpec{}); !errors.Is(err, errThreadLimit) {
		t.Fatalf("fourth thread = %v, want limit", err)
	}
}

// blockingAgentRunner runs until its context is cancelled.
type blockingAgentRunner struct{ started chan struct{} }

func (r *blockingAgentRunner) Run(ctx context.Context, req gjagent.Request) (gjagent.Response, error) {
	close(r.started)
	<-ctx.Done()
	return gjagent.Response{}, ctx.Err()
}

func TestThreadRunCancelFromAnotherReplica(t *testing.T) {
	svc := newSQLiteThreadService(t)
	ctx := threadUserCtx("user_1")
	thread, err := svc.createThread(ctx, threadSpec{})
	if err != nil {
		t.Fatal(err)
	}
	req := gjagent.Request{Instruction: "long question"}
	run, err := svc.prepareThreadRun(ctx, thread["id"].(string), &req)
	if err != nil {
		t.Fatal(err)
	}
	runner := &blockingAgentRunner{started: make(chan struct{})}
	done := make(chan gjagent.Response, 1)
	go func() {
		resp, _ := run.execute(ctx, runner, req, func(_ context.Context, r gjagent.Response, err error) (gjagent.Response, error) {
			return r, err
		}, nil)
		done <- resp
	}()
	<-runner.started

	// Another replica only has the store: it flips the status, and the
	// executing replica's monitor must notice without a local cancel.
	if _, err := svc.internalStoreMutationRows(ctx, "runs", `where: { id: { eq: $id } }, update: $input`, "id",
		map[string]any{"id": run.runID, "input": map[string]any{"status": runStatusCancelRequested}}); err != nil {
		t.Fatal(err)
	}
	select {
	case resp := <-done:
		if resp.Status != runStatusCancelled {
			t.Fatalf("cancelled run response = %+v", resp)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run did not stop after a cancel requested through the store")
	}
	row, _ := svc.runStoreRow(ctx, run.runID)
	if stringMapValue(row, "status") != runStatusCancelled {
		t.Fatalf("run row = %+v", row)
	}
	got, _ := svc.threadForCaller(ctx, thread["id"].(string))
	if got["active_run_id"] != "" {
		t.Fatalf("cancelled run must release the thread: %+v", got)
	}
}

func TestThreadSweepFailsRunsWithExpiredLeaseOnce(t *testing.T) {
	svc := newSQLiteThreadService(t)
	ctx := threadUserCtx("user_1")
	thread, err := svc.createThread(ctx, threadSpec{})
	if err != nil {
		t.Fatal(err)
	}
	id := thread["id"].(string)
	req := gjagent.Request{Instruction: "question on a replica that dies"}
	run, err := svc.prepareThreadRun(ctx, id, &req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.internalStoreMutationRows(ctx, "runs", `where: { id: { eq: $id } }, update: $input`, "id",
		map[string]any{"id": run.runID, "input": map[string]any{"lease_until_ms": int64(1)}}); err != nil {
		t.Fatal(err)
	}
	failed, err := svc.sweepExpiredRuns(ctx, time.Now())
	if err != nil || failed != 1 {
		t.Fatalf("first sweep failed=%d err=%v", failed, err)
	}
	if failed, err := svc.sweepExpiredRuns(ctx, time.Now()); err != nil || failed != 0 {
		t.Fatalf("second sweep failed=%d err=%v", failed, err)
	}
	messages, _ := svc.threadMessages(ctx, id)
	if len(messages) != 2 || messages[1]["status"] != runStatusFailed {
		t.Fatalf("thread should record the lost run: %+v", messages)
	}
	if err := svc.claimThreadRun(ctx, id, "run_next"); err != nil {
		t.Fatalf("the sweep must release the thread: %v", err)
	}
	if err := svc.deleteThread(ctx, id); !errors.Is(err, errThreadBusy) {
		t.Fatalf("deleting a thread with an active run = %v, want busy", err)
	}
	if err := svc.releaseThreadRun(ctx, id, "run_next"); err != nil {
		t.Fatal(err)
	}
	if err := svc.deleteThread(threadUserCtx("user_2"), id); !errors.Is(err, errThreadNotFound) {
		t.Fatalf("another owner deleted the thread: %v", err)
	}
	if err := svc.deleteThread(ctx, id); err != nil {
		t.Fatalf("delete thread: %v", err)
	}
	if rows, _ := svc.internalStoreAllRows(ctx, "messages", `where: { thread_id: { eq: $thread_id } }`, "id", map[string]any{"thread_id": id}); len(rows) != 0 {
		t.Fatalf("deleted thread left %d messages", len(rows))
	}
	if events, _ := svc.runEventsAfter(ctx, run.runID, 0); len(events) != 0 {
		t.Fatalf("deleted thread left %d run events", len(events))
	}
}
