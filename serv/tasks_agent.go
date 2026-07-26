package serv

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
)

const maxTaskWarmEntryTurns = 5

type taskWarmStart struct {
	TaskID        string
	Goal          string
	EntriesLoaded int
	CatalogHints  int
}

func (s *graphjinService) resolveAgentTaskContext(ctx context.Context, req *gjagent.Request) (taskWarmStart, error) {
	if s == nil || req == nil || strings.TrimSpace(req.TaskID) == "" {
		return taskWarmStart{}, nil
	}
	req.TaskID = strings.TrimSpace(req.TaskID)
	if !s.tasksEnabled() {
		return taskWarmStart{}, errTaskNotFoundOrClosed
	}
	ownerID, ok := artifactUserID(ctx)
	if !ok {
		return taskWarmStart{}, errTaskNotFoundOrClosed
	}
	task, err := s.internalTaskStoreRow(ctx, req.TaskID)
	if err != nil {
		return taskWarmStart{}, err
	}
	admin := s.identityRoleIsAdmin(ctx)
	if task == nil || taskStatus(stringMapValue(task, "status")) != "open" || (!admin && stringMapValue(task, "owner_id") != ownerID) {
		return taskWarmStart{}, errTaskNotFoundOrClosed
	}
	rows, err := s.internalStoreAllRows(ctx, "task_entries", `where: { task_id: { eq: $task_id } }`, taskEntryStoreFields, map[string]any{"task_id": req.TaskID})
	if err != nil {
		return taskWarmStart{}, err
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return stringMapValue(rows[i], "created_at") > stringMapValue(rows[j], "created_at")
	})
	if len(rows) > maxTaskWarmEntryTurns {
		rows = rows[:maxTaskWarmEntryTurns]
	}
	warmStart := taskWarmStart{
		TaskID:        req.TaskID,
		Goal:          stringMapValue(task, "goal"),
		EntriesLoaded: len(rows),
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return stringMapValue(rows[i], "created_at") < stringMapValue(rows[j], "created_at")
	})
	warm := make([]gjagent.Turn, 0, 1+len(rows)+len(req.History))
	warm = append(warm, gjagent.Turn{Role: "user", Content: "Declared task goal: " + warmStart.Goal})
	for _, row := range rows {
		origin := stringMapValue(row, "origin")
		role := "assistant"
		if origin == "caller" {
			role = "user"
		}
		content := strings.TrimSpace(stringMapValue(row, "body"))
		if content == "" {
			content = fmt.Sprintf("Task trail entry recorded from %s.", origin)
		}
		turn := gjagent.Turn{Role: role, Content: "[task " + origin + "] " + content}
		if origin == "agent_run" {
			turn.Status = stringMapValue(row, "status")
			turn.CatalogIDs = taskEntryCatalogDetailIDs(jsonMapString(row, "detail_json"))
			warmStart.CatalogHints += len(turn.CatalogIDs)
		}
		warm = append(warm, turn)
	}
	req.History = append(warm, req.History...)
	return warmStart, nil
}

// appendTaskNotices is deliberately best-effort. It adds one bounded,
// owner-scoped read only for successful agent runs that did not retain a task.
func (s *graphjinService) appendTaskNotices(ctx context.Context, req gjagent.Request, resp *gjagent.Response) {
	if s == nil || resp == nil || !s.tasksEnabled() || strings.TrimSpace(req.TaskID) != "" {
		return
	}
	ownerID, ok := artifactUserID(ctx)
	if !ok {
		return
	}
	rows, err := s.internalStoreAllRows(ctx, "tasks", `where: { owner_id: { eq: $owner_id }, status: { eq: "open" } }`, taskStoreFields, map[string]any{"owner_id": ownerID})
	if err != nil || len(rows) == 0 {
		return
	}
	// Keep defense-in-depth owner/status checks even though the store query is
	// already scoped. A future internal-store implementation must not leak ids.
	open := rows[:0]
	for _, row := range rows {
		if stringMapValue(row, "owner_id") == ownerID && taskStatus(stringMapValue(row, "status")) == "open" {
			open = append(open, row)
		}
	}
	if len(open) == 0 {
		return
	}
	sort.SliceStable(open, func(i, j int) bool {
		iTime, iOK := taskNoticeTime(open[i])
		jTime, jOK := taskNoticeTime(open[j])
		if iOK && jOK && !iTime.Equal(jTime) {
			return iTime.After(jTime)
		}
		if iOK != jOK {
			return iOK
		}
		return stringMapValue(open[i], "id") < stringMapValue(open[j], "id")
	})
	taskIDs := make([]string, 0, min(5, len(open)))
	for _, row := range open {
		if id := strings.TrimSpace(stringMapValue(row, "id")); id != "" {
			taskIDs = append(taskIDs, id)
			if len(taskIDs) == 5 {
				break
			}
		}
	}
	resp.Notices = append(resp.Notices, gjagent.ResponseNotice{
		Kind:    "task_open_unlinked",
		Message: "You have open declared tasks not linked to this run. Pass task_id to continue one of the listed tasks, or omit it deliberately for unrelated work.",
		Count:   len(open),
		TaskIDs: taskIDs,
	})
}

func taskNoticeTime(row map[string]any) (time.Time, bool) {
	if value := stringMapValue(row, "last_entry_at"); strings.TrimSpace(value) != "" {
		if parsed, ok := parseWatchTime(value); ok {
			return parsed, true
		}
	}
	return parseWatchTime(stringMapValue(row, "updated_at"))
}

func appendTaskContextNotice(warm taskWarmStart, resp *gjagent.Response) {
	if resp == nil || strings.TrimSpace(warm.TaskID) == "" {
		return
	}
	resp.Notices = append(resp.Notices, gjagent.ResponseNotice{
		Kind:    "task_context_loaded",
		Message: fmt.Sprintf("Loaded declared task goal %q with %d recent trail entries and %d catalog hints.", warm.Goal, warm.EntriesLoaded, warm.CatalogHints),
		Count:   warm.EntriesLoaded,
		TaskIDs: []string{warm.TaskID},
	})
}

func taskEntryCatalogDetailIDs(detailJSON string) []string {
	if strings.TrimSpace(detailJSON) == "" {
		return nil
	}
	var detail struct {
		CatalogDetailIDs []string `json:"catalog_detail_ids"`
	}
	if err := json.Unmarshal([]byte(detailJSON), &detail); err != nil {
		return nil
	}
	seen := make(map[string]bool, len(detail.CatalogDetailIDs))
	out := make([]string, 0, len(detail.CatalogDetailIDs))
	for _, id := range detail.CatalogDetailIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func (s *graphjinService) appendTaskTrailEntry(ctx context.Context, req gjagent.Request, resp gjagent.Response, duration time.Duration, runErr error) {
	if s == nil || strings.TrimSpace(req.TaskID) == "" || !s.tasksEnabled() {
		return
	}
	if s.conf != nil && s.conf.Core.Artifacts.Source != "" && s.conf.artifactSourceReadOnly() {
		return
	}
	detail := map[string]any{
		"skills":             resp.Skills,
		"catalog_detail_ids": gjagent.CatalogDetailIDs(resp),
		"duration_ms":        duration.Milliseconds(),
	}
	if actions := agentAuditActions(resp.Actions); actions != nil {
		detail["actions"] = actions
	}
	if resp.Refusal != nil && strings.TrimSpace(resp.Refusal.Code) != "" {
		detail["refusal_code"] = resp.Refusal.Code
	}
	if resp.Usage != nil {
		detail["usage"] = resp.Usage
	}
	if runErr != nil {
		detail["error"] = runErr.Error()
	}
	body := strings.TrimSpace(resp.Answer)
	if body == "" && runErr != nil {
		body = runErr.Error()
	}
	if body == "" && len(resp.Errors) != 0 {
		body = resp.Errors[0].Message
	}
	if body == "" {
		body = "Agent run completed."
	}
	if len(body) > maxTaskEntryBodyBytes {
		body = body[:maxTaskEntryBodyBytes]
	}
	status := strings.TrimSpace(resp.Status)
	if status == "" && runErr != nil {
		status = gjagent.StatusError
	}
	_, _, err := newTaskControlPlane(s).appendTaskEntry(ctx, taskEntrySpec{
		TaskID: req.TaskID, Origin: "agent_run", Body: body,
		DetailJSON: mustMarshalString(detail), Status: status, TraceID: resp.TraceID,
	})
	if err != nil && s.log != nil {
		s.log.Warnf("task trail append failed: %v", err)
	}
}
