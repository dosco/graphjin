package serv

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/dosco/graphjin/core/v3"
	"go.uber.org/zap"
)

func TestTaskProjectionGraphQLTraversal(t *testing.T) {
	autoInit := true
	svc := newArtifactOverlayTestServiceWithOptions(t, nil, core.ArtifactsConfig{
		Enabled: true, Source: "main", AutoInit: &autoInit, GlobalsPath: ".",
	}, func(conf *Config) {
		conf.Core.Tasks = core.TasksConfig{Enabled: true, MaxPerOwner: 20, MaxEntriesPerTask: 500, EntryRetentionHours: 168, SnapshotMaxBytes: 32768}
	})
	ctx := contextWithUserRole(artifactUserCtx("projection_owner"), "user")
	created, err := svc.gj.GraphQL(ctx, `mutation {
		gj_task(insert: { goal: "Trace the roast backlog" }) { id goal status }
	}`, nil, &core.RequestConfig{})
	if err != nil || len(created.Errors) != 0 {
		t.Fatalf("create task through GraphQL: err=%v errors=%+v data=%s", err, created.Errors, created.Data)
	}
	var taskResult struct {
		Task struct {
			ID string `json:"id"`
		} `json:"gj_task"`
	}
	if err := json.Unmarshal(created.Data, &taskResult); err != nil || taskResult.Task.ID == "" {
		t.Fatalf("decode task create: err=%v data=%s", err, created.Data)
	}
	vars := json.RawMessage(fmt.Sprintf(`{"task_id":%q}`, taskResult.Task.ID))
	journaled, err := svc.gj.GraphQL(ctx, `mutation($task_id: String!) {
		gj_task_entry(insert: { task_id: $task_id, body: "Check the current queue." }) { id task_id origin }
	}`, vars, &core.RequestConfig{})
	if err != nil || len(journaled.Errors) != 0 {
		t.Fatalf("journal task through GraphQL: err=%v errors=%+v data=%s", err, journaled.Errors, journaled.Data)
	}
	queried, err := svc.gj.GraphQL(ctx, `query($task_id: String!) {
		gj_task_entry(where: { task_id: { eq: $task_id } }) {
			id
			origin
			gj_task { id goal }
		}
	}`, vars, &core.RequestConfig{})
	if err != nil || len(queried.Errors) != 0 {
		t.Fatalf("traverse task projection: err=%v errors=%+v data=%s", err, queried.Errors, queried.Data)
	}
	if !strings.Contains(string(queried.Data), `"goal":"Trace the roast backlog"`) {
		t.Fatalf("task projection relationship missing: %s", queried.Data)
	}
	if rows := queryArtifactProjection(t, svc, contextWithUserRole(artifactUserCtx("projection_other"), "user"), `query { gj_task { id } }`); len(rows) != 0 {
		t.Fatalf("foreign task projection rows = %+v", rows)
	}
}

func TestTaskAgentHTTPFlowWarmStartsAndJournals(t *testing.T) {
	svc, cp := newSQLiteTaskService(t, 5, 20)
	logger := zap.NewNop()
	svc.log, svc.zlog = logger.Sugar(), logger
	svc.conf.Agent.Enabled = true
	ctx := contextWithUserRole(artifactUserCtx("flow_owner"), "analyst")
	task := insertTaskForTest(t, cp, ctx, "Summarize and monitor overdue orders")
	taskID := fmt.Sprint(task["id"])

	runner := &scriptedAgentRunner{resp: gjagent.Response{
		Status: gjagent.StatusAnswered, Answer: "Three overdue orders need review.",
		TraceID:  "trace-flow-1",
		Evidence: map[string]any{"catalog_detail_ids": []any{"table:app:main.orders"}},
	}}
	withScriptedAgentRunner(t, runner)
	hs := &HttpService{}
	hs.Store(svc)

	run := func(instruction, retainedTaskID string) *httptest.ResponseRecorder {
		payload := map[string]any{"instruction": instruction}
		if retainedTaskID != "" {
			payload["task_id"] = retainedTaskID
		}
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, routeAgent, strings.NewReader(string(body))).WithContext(ctx)
		rec := httptest.NewRecorder()
		hs.Agent(nil).ServeHTTP(rec, req)
		return rec
	}

	unlinkedRec := run("Check for declared work", "")
	if unlinkedRec.Code != http.StatusOK {
		t.Fatalf("unlinked task agent run = %d: %s", unlinkedRec.Code, unlinkedRec.Body.String())
	}
	var unlinkedResp gjagent.Response
	if err := json.Unmarshal(unlinkedRec.Body.Bytes(), &unlinkedResp); err != nil {
		t.Fatalf("decode unlinked response: %v", err)
	}
	if notice, ok := taskNoticeForKind(unlinkedResp, "task_open_unlinked"); !ok || len(notice.TaskIDs) != 1 || notice.TaskIDs[0] != taskID {
		t.Fatalf("HTTP unlinked task notice = %+v", unlinkedResp.Notices)
	}

	firstRec := run("Check overdue orders", taskID)
	if firstRec.Code != http.StatusOK {
		t.Fatalf("first task agent run = %d: %s", firstRec.Code, firstRec.Body.String())
	}
	var firstResp gjagent.Response
	if err := json.Unmarshal(firstRec.Body.Bytes(), &firstResp); err != nil {
		t.Fatalf("decode first task response: %v", err)
	}
	if notice, ok := taskNoticeForKind(firstResp, "task_context_loaded"); !ok || notice.Count != 0 || len(notice.TaskIDs) != 1 || notice.TaskIDs[0] != taskID {
		t.Fatalf("first HTTP task context notice = %+v", firstResp.Notices)
	}
	if runner.req.TaskID != taskID || len(runner.req.History) != 1 || !strings.Contains(runner.req.History[0].Content, "Summarize and monitor overdue orders") {
		t.Fatalf("first run did not receive declared goal: %+v", runner.req)
	}

	secondRec := run("What changed?", taskID)
	if secondRec.Code != http.StatusOK {
		t.Fatalf("second task agent run = %d: %s", secondRec.Code, secondRec.Body.String())
	}
	var secondResp gjagent.Response
	if err := json.Unmarshal(secondRec.Body.Bytes(), &secondResp); err != nil {
		t.Fatalf("decode second task response: %v", err)
	}
	if notice, ok := taskNoticeForKind(secondResp, "task_context_loaded"); !ok || notice.Count != 1 {
		t.Fatalf("second HTTP task context notice = %+v", secondResp.Notices)
	}
	if len(runner.req.History) < 2 || !strings.Contains(runner.req.History[len(runner.req.History)-1].Content, "Three overdue orders") {
		t.Fatalf("second run did not receive task trail: %+v", runner.req.History)
	}
	rows, err := svc.internalStoreAllRows(ctx, "task_entries", `where: { task_id: { eq: $task_id }, trace_id: { eq: $trace_id } }`, taskEntryStoreFields, map[string]any{"task_id": taskID, "trace_id": "trace-flow-1"})
	if err != nil || len(rows) != 1 {
		t.Fatalf("HTTP task trail dedupe rows=%d err=%v: %+v", len(rows), err, rows)
	}

	if _, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table: tasksRootTable, Operation: "update", Where: map[string]any{"id": taskID},
		Input: map[string]any{"status": "closed", "outcome": "Review complete"},
	}); err != nil {
		t.Fatalf("close flow task: %v", err)
	}
	if rec := run("One more check", taskID); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), errTaskNotFoundOrClosed.Error()) {
		t.Fatalf("closed task run = %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTaskAgentMCPFlowWarmStartsAndJournals(t *testing.T) {
	svc, cp := newSQLiteTaskService(t, 5, 20)
	logger := zap.NewNop()
	svc.log, svc.zlog = logger.Sugar(), logger
	svc.conf.Agent.Enabled = true
	ctx := contextWithUserRole(artifactUserCtx("mcp_task_owner"), "analyst")
	task := insertTaskForTest(t, cp, ctx, "Continue the MCP investigation")
	taskID := fmt.Sprint(task["id"])

	runner := &scriptedAgentRunner{resp: gjagent.Response{
		Status: gjagent.StatusAnswered, Answer: "The MCP investigation is current.",
		TraceID: "trace-mcp-task-1",
	}}
	withScriptedAgentRunner(t, runner)
	ms := &mcpServer{service: svc, ctx: ctx}
	unlinkedResult, err := ms.handleAskGraphJinAgent(ctx, newToolRequest(map[string]any{
		"instruction": "Check for declared work",
	}))
	if err != nil || unlinkedResult == nil || unlinkedResult.IsError {
		t.Fatalf("unlinked MCP run: result=%+v err=%v", unlinkedResult, err)
	}
	var unlinkedResp gjagent.Response
	if err := json.Unmarshal([]byte(assertToolSuccess(t, unlinkedResult)), &unlinkedResp); err != nil {
		t.Fatalf("decode unlinked MCP response: %v", err)
	}
	if notice, ok := taskNoticeForKind(unlinkedResp, "task_open_unlinked"); !ok || len(notice.TaskIDs) != 1 || notice.TaskIDs[0] != taskID {
		t.Fatalf("MCP unlinked task notice = %+v", unlinkedResp.Notices)
	}

	result, err := ms.handleAskGraphJinAgent(ctx, newToolRequest(map[string]any{
		"instruction": "Continue from the declared goal", "task_id": taskID,
	}))
	if err != nil || result == nil || result.IsError {
		t.Fatalf("task-bound MCP run: result=%+v err=%v", result, err)
	}
	var taskResp gjagent.Response
	if err := json.Unmarshal([]byte(assertToolSuccess(t, result)), &taskResp); err != nil {
		t.Fatalf("decode task-bound MCP response: %v", err)
	}
	if notice, ok := taskNoticeForKind(taskResp, "task_context_loaded"); !ok || notice.Count != 0 || len(notice.TaskIDs) != 1 || notice.TaskIDs[0] != taskID {
		t.Fatalf("MCP task context notice = %+v", taskResp.Notices)
	}
	if runner.req.TaskID != taskID || len(runner.req.History) != 1 || !strings.Contains(runner.req.History[0].Content, "Continue the MCP investigation") {
		t.Fatalf("MCP task warm start = %+v", runner.req)
	}
	rows, err := svc.internalStoreAllRows(ctx, "task_entries", `where: { task_id: { eq: $task_id }, trace_id: { eq: $trace_id } }`, taskEntryStoreFields, map[string]any{
		"task_id": taskID, "trace_id": "trace-mcp-task-1",
	})
	if err != nil || len(rows) != 1 || stringMapValue(rows[0], "origin") != "agent_run" {
		t.Fatalf("MCP task trail = %+v err=%v", rows, err)
	}

	foreign := contextWithUserRole(artifactUserCtx("mcp_task_other"), "analyst")
	result, err = ms.handleAskGraphJinAgent(foreign, newToolRequest(map[string]any{
		"instruction": "Try the foreign task", "task_id": taskID,
	}))
	if err != nil || result == nil || !result.IsError {
		t.Fatalf("foreign MCP task should be a tool error: result=%+v err=%v", result, err)
	}
}
