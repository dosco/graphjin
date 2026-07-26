package serv

import (
	"context"
	"fmt"
	"strings"
	"testing"

	gjagent "github.com/dosco/graphjin/agent/v3"
)

func TestTaskOpenUnlinkedNoticeIsBoundedAndOwnerScoped(t *testing.T) {
	svc, cp := newSQLiteTaskService(t, 10, 20)
	ctx := artifactUserCtx("notice_owner")
	other := artifactUserCtx("notice_other")

	ids := make([]string, 0, 7)
	for i := 0; i < 7; i++ {
		row := insertTaskForTest(t, cp, ctx, fmt.Sprintf("Open task %d", i))
		ids = append(ids, fmt.Sprint(row["id"]))
	}
	foreign := insertTaskForTest(t, cp, other, "Foreign open task")
	if _, err := svc.internalStoreMutationRows(ctx, "tasks", `where: { id: { eq: $id } }, update: $input`, taskStoreFields, map[string]any{
		"id": ids[6], "input": map[string]any{"status": "verifying", "verify_status": "pending", "verify_after": "2099-01-01T00:00:00Z"},
	}); err != nil {
		t.Fatalf("mark task verifying: %v", err)
	}
	if _, _, err := cp.appendTaskEntry(ctx, taskEntrySpec{
		TaskID: ids[0], Origin: "caller", Body: "Most recently active task.",
	}); err != nil {
		t.Fatalf("touch oldest task: %v", err)
	}

	var resp gjagent.Response
	svc.appendTaskNotices(ctx, gjagent.Request{}, &resp)
	if len(resp.Notices) != 1 {
		t.Fatalf("notices = %+v, want one", resp.Notices)
	}
	notice := resp.Notices[0]
	if notice.Kind != "task_open_unlinked" || notice.Count != 7 || len(notice.TaskIDs) != 5 {
		t.Fatalf("open-task notice = %+v", notice)
	}
	if !strings.Contains(notice.Message, "verifying") {
		t.Fatalf("active-task notice does not describe verifying tasks: %+v", notice)
	}
	if notice.TaskIDs[0] != ids[0] {
		t.Fatalf("most recently active task = %q, want %q; notice=%+v", notice.TaskIDs[0], ids[0], notice)
	}
	for _, id := range notice.TaskIDs {
		if id == fmt.Sprint(foreign["id"]) {
			t.Fatalf("foreign task leaked into notice: %+v", notice)
		}
	}

	resp = gjagent.Response{}
	svc.appendTaskNotices(ctx, gjagent.Request{TaskID: ids[1]}, &resp)
	if len(resp.Notices) != 0 {
		t.Fatalf("task-bound run should not get unlinked notice: %+v", resp.Notices)
	}

	resp = gjagent.Response{}
	svc.appendTaskNotices(context.Background(), gjagent.Request{}, &resp)
	if len(resp.Notices) != 0 {
		t.Fatalf("identity-free run should not get task notice: %+v", resp.Notices)
	}

	svc.conf.Core.Tasks.Enabled = false
	resp = gjagent.Response{}
	svc.appendTaskNotices(ctx, gjagent.Request{}, &resp)
	if len(resp.Notices) != 0 {
		t.Fatalf("disabled tasks should not produce notices: %+v", resp.Notices)
	}
}

func TestTaskOpenUnlinkedNoticeRequiresOpenTask(t *testing.T) {
	svc, _ := newSQLiteTaskService(t, 5, 20)
	var resp gjagent.Response
	svc.appendTaskNotices(artifactUserCtx("no_tasks"), gjagent.Request{}, &resp)
	if len(resp.Notices) != 0 {
		t.Fatalf("caller without open tasks should not get a notice: %+v", resp.Notices)
	}
}

func TestTaskContextLoadedNoticeReportsWarmStart(t *testing.T) {
	svc, cp := newSQLiteTaskService(t, 5, 20)
	ctx := artifactUserCtx("warm_notice_owner")
	goal := "Explain the declared production backlog"
	task := insertTaskForTest(t, cp, ctx, goal)
	taskID := fmt.Sprint(task["id"])
	entries := []taskEntrySpec{
		{TaskID: taskID, Origin: "caller", Body: "Use approved order evidence."},
		{TaskID: taskID, Origin: "agent_run", Body: "Inspected orders.", TraceID: "warm-notice-1", DetailJSON: `{"catalog_detail_ids":["table:app:main.orders","column:app:main.orders.status","table:app:main.orders"]}`},
		{TaskID: taskID, Origin: "agent_run", Body: "Inspected suppliers.", TraceID: "warm-notice-2", DetailJSON: `{"catalog_detail_ids":["table:app:main.suppliers"]}`},
	}
	for _, entry := range entries {
		if _, _, err := cp.appendTaskEntry(ctx, entry); err != nil {
			t.Fatalf("append warm entry: %v", err)
		}
	}

	req := gjagent.Request{TaskID: taskID}
	warm, err := svc.resolveAgentTaskContext(ctx, &req)
	if err != nil {
		t.Fatalf("resolve warm start: %v", err)
	}
	if warm.EntriesLoaded != 3 || warm.CatalogHints != 3 {
		t.Fatalf("warm-start counts = %+v", warm)
	}
	var resp gjagent.Response
	appendTaskContextNotice(warm, &resp)
	if len(resp.Notices) != 1 {
		t.Fatalf("context notices = %+v", resp.Notices)
	}
	notice := resp.Notices[0]
	if notice.Kind != "task_context_loaded" || notice.Count != 3 || len(notice.TaskIDs) != 1 || notice.TaskIDs[0] != taskID {
		t.Fatalf("context-loaded notice = %+v", notice)
	}
	if !strings.Contains(notice.Message, goal) || !strings.Contains(notice.Message, "3 catalog hints") {
		t.Fatalf("context-loaded message = %q", notice.Message)
	}
}

func TestTaskCreationNextGuidance(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{AllowRawQueries: true, IncludeToolsWithAgent: true})
	ms.service.conf.Agent.Enabled = true

	taskQuery := `mutation {
		gj_task (
			insert: { goal: "Keep insert: text in the declared goal" }
		) { id goal status }
	}`
	next := ms.nextForToolCall("execute_graphql", map[string]any{"query": taskQuery}, ExecuteResult{})
	if next == nil || next.StateCode != "task_created" || next.RecommendedTool != "ask_graphjin_agent" || len(next.Options) != 3 {
		t.Fatalf("task creation next guidance = %+v", next)
	}
	if got := fmt.Sprint(next.Options[0].ArgsTemplate["task_id"]); got != "<retained_task_id>" {
		t.Fatalf("task continuation template = %+v", next.Options[0].ArgsTemplate)
	}
	if query := fmt.Sprint(next.Options[1].ArgsTemplate["query"]); !strings.Contains(query, "gj_task_entry(insert") {
		t.Fatalf("task journal template = %+v", next.Options[1].ArgsTemplate)
	}
	if query := fmt.Sprint(next.Options[2].ArgsTemplate["query"]); !strings.Contains(query, "verify_json") || !strings.Contains(query, "saved_query_name") {
		t.Fatalf("task verification template = %+v", next.Options[2].ArgsTemplate)
	}

	ordinary := ms.nextForToolCall("execute_graphql", map[string]any{
		"query": `query { gj_task(where: { goal: { eq: "insert: is only string data" } }) { id } }`,
	}, ExecuteResult{})
	if ordinary == nil || ordinary.StateCode != "graphql_execution_success" {
		t.Fatalf("ordinary query should retain generic guidance without task-specific next: %+v", ordinary)
	}
	if hasTaskInsertArgument(`query { gj_task(where: { goal: { eq: "insert: data" } }) { id } }`) {
		t.Fatal("ordinary task read was misclassified as a task insert")
	}

	failed := ms.nextForToolCall("execute_graphql", map[string]any{"query": taskQuery}, ExecuteResult{
		Errors: []ErrorInfo{{Message: "creation failed"}},
	})
	if failed == nil || failed.StateCode != "graphql_execution_error" {
		t.Fatalf("failed task insert should retain execution error guidance: %+v", failed)
	}
}

func taskNoticeForKind(resp gjagent.Response, kind string) (gjagent.ResponseNotice, bool) {
	for _, notice := range resp.Notices {
		if notice.Kind == kind {
			return notice, true
		}
	}
	return gjagent.ResponseNotice{}, false
}
