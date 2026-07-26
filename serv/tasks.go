package serv

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dosco/graphjin/core/v3"
)

const (
	tasksRootTable       = "gj_task"
	taskEntriesRootTable = "gj_task_entry"

	maxTaskGoalBytes      = 2048
	maxTaskOutcomeBytes   = 4096
	maxTaskEntryBodyBytes = 4096
)

var errTaskNotFoundOrClosed = errors.New("task_not_found_or_closed")

const taskStoreFields = `id goal status outcome snapshot_json verify_json verify_status verify_after verify_attempts account_id owner_id owner_role last_entry_at created_at updated_at closed_at`
const taskEntryStoreFields = `id task_id origin body detail_json status trace_id watch_id account_id owner_id created_at updated_at`

type taskControlPlane struct {
	service *graphjinService
}

type taskEntrySpec struct {
	TaskID              string
	Origin              string
	Body                string
	DetailJSON          string
	Status              string
	TraceID             string
	WatchID             string
	VerificationHash    string
	VerificationAttempt int64
}

func newTaskControlPlane(s *graphjinService) taskControlPlane {
	return taskControlPlane{service: s}
}

func (h taskControlPlane) ManagedQueryTables() []core.ManagedTable {
	if h.service == nil || !h.service.tasksEnabled() {
		return nil
	}
	return []core.ManagedTable{
		managedTable(tasksRootTable, taskColumns()),
		managedTable(taskEntriesRootTable, taskEntryColumns()),
	}
}

func (h taskControlPlane) ManagedMutationTables() []string {
	if h.service == nil || !h.service.tasksEnabled() {
		return nil
	}
	return []string{tasksRootTable, taskEntriesRootTable}
}

func taskColumns() []core.ManagedColumn {
	return []core.ManagedColumn{
		cpCol("id", "text", true),
		cpCol("goal", "text", false),
		cpCol("status", "text", false),
		cpCol("outcome", "text", false),
		cpCol("snapshot_json", "json", false),
		cpCol("verify_json", "json", false),
		cpCol("verify_status", "text", false),
		cpCol("verify_after", "text", false),
		cpCol("verify_attempts", "integer", false),
		cpCol("account_id", "text", false),
		cpCol("owner_id", "text", false),
		cpCol("owner_role", "text", false),
		cpCol("account_ref", "text", false),
		cpCol("owner_ref", "text", false),
		cpCol("last_entry_at", "text", false),
		cpCol("created_at", "text", false),
		cpCol("updated_at", "text", false),
		cpCol("closed_at", "text", false),
	}
}

func taskEntryColumns() []core.ManagedColumn {
	return []core.ManagedColumn{
		cpCol("id", "text", true),
		cpCol("task_id", "text", false),
		cpCol("origin", "text", false),
		cpCol("body", "text", false),
		cpCol("detail_json", "json", false),
		cpCol("status", "text", false),
		cpCol("trace_id", "text", false),
		cpCol("watch_id", "text", false),
		cpCol("account_id", "text", false),
		cpCol("owner_id", "text", false),
		cpCol("account_ref", "text", false),
		cpCol("owner_ref", "text", false),
		cpCol("created_at", "text", false),
		cpCol("updated_at", "text", false),
	}
}

func (h taskControlPlane) ExecuteManagedQuery(ctx context.Context, req core.ManagedQueryRequest) (json.RawMessage, error) {
	out := make(map[string]any, len(req.Roots))
	for _, root := range req.Roots {
		rows, err := h.queryRows(ctx, root)
		if err != nil {
			return nil, err
		}
		out[root.FieldName] = filterRows(rows, root.Fields)
	}
	return json.Marshal(out)
}

func (h taskControlPlane) queryRows(ctx context.Context, root core.ManagedQueryRoot) ([]map[string]any, error) {
	var (
		rows []map[string]any
		err  error
	)
	switch root.Table {
	case tasksRootTable:
		rows, err = h.taskRowsWithScope(ctx, false)
	case taskEntriesRootTable:
		rows, err = h.taskEntryRowsWithScope(ctx, false)
	default:
		return nil, fmt.Errorf("unsupported GraphJin task root: %s", root.Table)
	}
	if err != nil {
		return nil, err
	}
	return applyManagedQuery(rows, root), nil
}

func (h taskControlPlane) ExecuteManagedMutation(ctx context.Context, req core.ManagedMutationRequest) (json.RawMessage, error) {
	out := make(map[string]any, len(req.Roots))
	for _, root := range req.Roots {
		row, err := h.mutateRow(ctx, root)
		if err != nil {
			return nil, err
		}
		out[root.FieldName] = filterRow(row, root.Fields)
	}
	return json.Marshal(out)
}

func (h taskControlPlane) mutateRow(ctx context.Context, root core.ManagedMutationRoot) (map[string]any, error) {
	switch root.Table {
	case tasksRootTable:
		switch root.Operation {
		case "insert", "upsert", "update":
			return h.upsertTask(ctx, root)
		case "delete":
			return h.deleteTask(ctx, root)
		default:
			return nil, fmt.Errorf("gj_task supports insert, update, upsert, and delete mutations")
		}
	case taskEntriesRootTable:
		if root.Operation != "insert" {
			return nil, fmt.Errorf("gj_task_entry is immutable and supports insert mutations only")
		}
		return h.insertTaskEntry(ctx, root)
	default:
		return nil, fmt.Errorf("unsupported GraphJin task mutation root: %s", root.Table)
	}
}

func (h taskControlPlane) taskRowsWithScope(ctx context.Context, allOwners bool) ([]map[string]any, error) {
	s := h.service
	if _, _, _, _, ok := s.taskDB(); !ok {
		return nil, nil
	}
	ownerID, hasUser := artifactUserID(ctx)
	admin := s.identityRoleIsAdmin(ctx)
	if !allOwners && !hasUser {
		return nil, nil
	}
	args := ""
	var vars any
	if !allOwners && !admin {
		args = `where: { owner_id: { eq: $owner_id } }`
		vars = map[string]any{"owner_id": ownerID}
	}
	rows, err := s.internalStoreAllRows(ctx, "tasks", args, taskStoreFields, vars)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if !allOwners && !admin && stringMapValue(row, "owner_id") != ownerID {
			continue
		}
		out = append(out, taskStoreRow(row, admin || allOwners))
	}
	return out, nil
}

func (h taskControlPlane) taskEntryRowsWithScope(ctx context.Context, allOwners bool) ([]map[string]any, error) {
	s := h.service
	if _, _, _, _, ok := s.taskDB(); !ok {
		return nil, nil
	}
	ownerID, hasUser := artifactUserID(ctx)
	admin := s.identityRoleIsAdmin(ctx)
	if !allOwners && !hasUser {
		return nil, nil
	}
	args := ""
	var vars any
	if !allOwners && !admin {
		args = `where: { owner_id: { eq: $owner_id } }`
		vars = map[string]any{"owner_id": ownerID}
	}
	rows, err := s.internalStoreAllRows(ctx, "task_entries", args, taskEntryStoreFields, vars)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if !allOwners && !admin && stringMapValue(row, "owner_id") != ownerID {
			continue
		}
		out = append(out, taskEntryStoreRow(row, admin || allOwners))
	}
	return out, nil
}

func (h taskControlPlane) allTaskRowsForProjection(ctx context.Context) ([]map[string]any, error) {
	return h.taskRowsWithScope(ctx, true)
}

func (h taskControlPlane) allTaskEntryRowsForProjection(ctx context.Context) ([]map[string]any, error) {
	rows, err := h.taskEntryRowsWithScope(ctx, true)
	if err != nil {
		return nil, err
	}
	if h.service != nil && h.service.conf != nil {
		rows = trimTaskEntryProjectionRows(rows, h.service.conf.Core.EffectiveTasksConfig())
	}
	return rows, nil
}

func (h taskControlPlane) upsertTask(ctx context.Context, root core.ManagedMutationRoot) (map[string]any, error) {
	s := h.service
	if _, _, _, _, ok := s.taskDB(); !ok {
		return nil, fmt.Errorf("task store database is not configured")
	}
	ownerID, ok := artifactUserID(ctx)
	if !ok {
		return nil, fmt.Errorf("gj_task write requires user identity")
	}
	admin := s.identityRoleIsAdmin(ctx)
	for _, field := range []string{"id", "verify_status", "verify_after", "verify_attempts", "account_id", "owner_id", "owner_role", "account_ref", "owner_ref", "last_entry_at", "created_at", "updated_at", "closed_at"} {
		if _, exists := root.Input[field]; exists {
			return nil, fmt.Errorf("gj_task %s is server-managed", field)
		}
	}
	id := strings.TrimSpace(stringInput(root.Input, "id", ""))
	if id == "" {
		id = strings.TrimSpace(stringWhere(root.Where, "id"))
	}
	var existing map[string]any
	var err error
	if id != "" {
		existing, err = s.internalTaskStoreRow(ctx, id)
		if err != nil {
			return nil, err
		}
	}
	if existing != nil && !admin && stringMapValue(existing, "owner_id") != ownerID {
		return nil, errTaskNotFoundOrClosed
	}
	if existing == nil && root.Operation == "update" {
		return nil, errTaskNotFoundOrClosed
	}

	goal := strings.TrimSpace(stringInput(root.Input, "goal", ""))
	if goal == "" && existing != nil {
		goal = stringMapValue(existing, "goal")
	}
	if goal == "" {
		return nil, fmt.Errorf("gj_task mutation requires goal")
	}
	if len(goal) > maxTaskGoalBytes {
		return nil, fmt.Errorf("gj_task goal exceeds %d bytes", maxTaskGoalBytes)
	}
	if existing == nil {
		if id == "" {
			id = taskID(ownerID, goal)
		} else {
			return nil, fmt.Errorf("gj_task id is server-managed")
		}
	}

	statusDefault := "open"
	if existing != nil {
		statusDefault = stringMapValue(existing, "status")
	}
	_, statusSupplied := root.Input["status"]
	status, err := normalizeTaskStatus(stringInput(root.Input, "status", statusDefault))
	if err != nil {
		return nil, err
	}
	if statusSupplied && status == "verifying" {
		return nil, fmt.Errorf("gj_task verifying status is server-managed")
	}
	if existing == nil && status != "open" {
		return nil, fmt.Errorf("gj_task must be created open")
	}
	outcome := stringInput(root.Input, "outcome", "")
	if _, supplied := root.Input["outcome"]; !supplied && existing != nil {
		outcome = stringMapValue(existing, "outcome")
	}
	if len(outcome) > maxTaskOutcomeBytes {
		return nil, fmt.Errorf("gj_task outcome exceeds %d bytes", maxTaskOutcomeBytes)
	}
	if status == "open" {
		outcome = ""
	}

	snapshotJSON := ""
	if _, supplied := root.Input["snapshot_json"]; supplied {
		snapshotJSON, err = normalizeTaskJSON(jsonStringInput(root.Input, "snapshot_json"), s.conf.Core.EffectiveTasksConfig().SnapshotMaxBytes, "snapshot_json")
		if err != nil {
			return nil, err
		}
	} else if existing != nil {
		snapshotJSON = jsonMapString(existing, "snapshot_json")
	}
	verifyJSON := ""
	verifySpec := taskVerifySpec{}
	_, verifySupplied := root.Input["verify_json"]
	if verifySupplied {
		verifyJSON, verifySpec, err = s.normalizeTaskVerifyJSON(ctx, jsonStringInput(root.Input, "verify_json"), s.conf.Core.EffectiveTasksConfig().SnapshotMaxBytes)
		if err != nil {
			return nil, err
		}
	} else if existing != nil {
		verifyJSON = jsonMapString(existing, "verify_json")
		if strings.TrimSpace(verifyJSON) != "" {
			verifySpec, err = parseTaskVerifySpec(verifyJSON)
			if err != nil {
				return nil, fmt.Errorf("gj_task stored verify_json is invalid: %w", err)
			}
		}
	}

	previousStatus := ""
	if existing != nil {
		previousStatus = taskStatus(stringMapValue(existing, "status"))
	}
	if existing == nil || (!taskStatusActive(previousStatus) && taskStatusActive(status)) {
		if err := h.enforceTaskLimit(ctx, ownerID, id); err != nil {
			return nil, err
		}
	}
	nowTime := time.Now().UTC()
	now := nowTime.Format(time.RFC3339Nano)
	accountID, _ := identityVarString(ctx, "account_id")
	createdAt := now
	lastEntryAt := ""
	closedAt := ""
	verifyStatus := ""
	verifyAfter := ""
	verifyAttempts := int64(0)
	if existing != nil {
		createdAt = stringMapValue(existing, "created_at")
		if createdAt == "" {
			createdAt = now
		}
		lastEntryAt = stringMapValue(existing, "last_entry_at")
		closedAt = stringMapValue(existing, "closed_at")
		accountID = stringMapValue(existing, "account_id")
		verifyStatus = stringMapValue(existing, "verify_status")
		verifyAfter = stringMapValue(existing, "verify_after")
		verifyAttempts = int64MapValue(existing, "verify_attempts")
	}
	verifyChanged := verifySupplied && verifyJSON != jsonMapString(existing, "verify_json")
	if verifyChanged {
		verifyStatus = ""
		verifyAfter = ""
		if previousStatus == "verifying" && !(statusSupplied && status == "closed") {
			status = "open"
			outcome = ""
			closedAt = ""
		}
	}
	if statusSupplied && status == "open" && previousStatus == "verifying" {
		verifyStatus = "cancelled"
		verifyAfter = ""
	}
	closingWithVerification := existing != nil && statusSupplied && status == "closed" && strings.TrimSpace(verifyJSON) != "" &&
		(previousStatus != "closed" || verifyChanged || verifyStatus != "verified")
	var immediateResult *taskVerificationResult
	if closingWithVerification {
		verifyAttempts++
		closedAt = ""
		if verifySpec.RecheckWindow > 0 {
			status = "verifying"
			verifyStatus = "pending"
			verifyAfter = nowTime.Add(verifySpec.RecheckWindow).Format(time.RFC3339Nano)
		} else {
			ownerCtx := s.ownerContext(ctx, stringMapValue(existing, "owner_id"), stringMapValue(existing, "owner_role"), stringMapValue(existing, "account_id"))
			result, runErr := s.runTaskVerification(ownerCtx, verifySpec)
			if runErr != nil {
				return nil, fmt.Errorf("gj_task verification: %w", runErr)
			}
			immediateResult = &result
			verifyAfter = ""
			if result.Passed {
				status = "closed"
				verifyStatus = "verified"
				closedAt = now
			} else {
				status = "open"
				verifyStatus = "failed"
				outcome = ""
				closedAt = ""
			}
		}
	} else if statusSupplied && status == "closed" && strings.TrimSpace(verifyJSON) == "" {
		verifyStatus = ""
		verifyAfter = ""
	}
	if status == "closed" && !closingWithVerification && (existing == nil || previousStatus != "closed") {
		closedAt = now
	}
	if status == "open" {
		outcome = ""
		closedAt = ""
	}
	input := map[string]any{
		"id": id, "goal": goal, "status": status, "outcome": outcome,
		"snapshot_json": nullableJSONString(snapshotJSON), "account_id": accountID,
		"verify_json": nullableJSONString(verifyJSON), "verify_status": verifyStatus,
		"verify_after": nullableJSONString(verifyAfter), "verify_attempts": verifyAttempts,
		"owner_id": ownerID, "owner_role": runtimeRoleClass(ctx), "last_entry_at": nullableJSONString(lastEntryAt),
		"created_at": createdAt, "updated_at": now, "closed_at": nullableJSONString(closedAt),
	}
	if immediateResult != nil {
		if _, err := h.insertTaskVerificationEntry(ctx, existing, verifySpec, *immediateResult, verifyAttempts, now); err != nil {
			return nil, err
		}
		input["last_entry_at"] = now
	}
	var rows []map[string]any
	if existing == nil {
		rows, err = s.internalStoreMutationRows(ctx, "tasks", `insert: $input`, taskStoreFields, map[string]any{"input": input})
	} else {
		update := cloneMap(input)
		for _, field := range []string{"id", "account_id", "owner_id", "owner_role", "created_at"} {
			delete(update, field)
		}
		if immediateResult == nil {
			delete(update, "last_entry_at")
		}
		rows, err = s.internalStoreMutationRows(ctx, "tasks", `where: { id: { eq: $id } }, update: $input`, taskStoreFields, map[string]any{"id": id, "input": update})
	}
	if err != nil {
		return nil, err
	}
	if immediateResult != nil {
		if err := h.pruneTaskEntries(ctx, id); err != nil {
			return nil, err
		}
	}
	if err := s.bumpArtifactRevision(ctx, "tasks"); err != nil {
		return nil, err
	}
	s.markTaskChanged("task mutation")
	if len(rows) != 0 {
		return taskStoreRow(rows[0], admin), nil
	}
	return taskStoreRow(input, admin), nil
}

func (h taskControlPlane) deleteTask(ctx context.Context, root core.ManagedMutationRoot) (map[string]any, error) {
	s := h.service
	if _, _, _, _, ok := s.taskDB(); !ok {
		return nil, fmt.Errorf("task store database is not configured")
	}
	ownerID, ok := artifactUserID(ctx)
	if !ok {
		return nil, fmt.Errorf("gj_task delete requires user identity")
	}
	id := strings.TrimSpace(stringInput(root.Input, "id", ""))
	if id == "" {
		id = strings.TrimSpace(stringWhere(root.Where, "id"))
	}
	if id == "" {
		return nil, fmt.Errorf("gj_task delete requires id")
	}
	admin := s.identityRoleIsAdmin(ctx)
	existing, err := s.internalTaskStoreRow(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing == nil || (!admin && stringMapValue(existing, "owner_id") != ownerID) {
		return map[string]any{"id": id, "deleted": true}, nil
	}
	if _, err := s.internalStoreMutationRows(ctx, "task_entries", `where: { task_id: { eq: $task_id } }, delete: true`, `id`, map[string]any{"task_id": id}); err != nil {
		return nil, err
	}
	if s.watchesEnabled() {
		rows, unlinkErr := s.internalStoreMutationRows(ctx, "watches", `where: { task_id: { eq: $task_id } }, update: $input`, watchStoreFields, map[string]any{"task_id": id, "input": map[string]any{"task_id": "", "updated_at": time.Now().UTC().Format(time.RFC3339Nano)}})
		if unlinkErr != nil {
			return nil, unlinkErr
		}
		if len(rows) != 0 {
			if err := s.bumpArtifactRevision(ctx, "watches"); err != nil {
				return nil, err
			}
			s.markWatchChanged("task delete unlinked watches")
			s.publishWatchRunnerChanged(ctx)
		}
	}
	if _, err := s.internalStoreMutationRows(ctx, "tasks", `where: { id: { eq: $id } }, delete: true`, `id`, map[string]any{"id": id}); err != nil {
		return nil, err
	}
	if err := s.bumpArtifactRevision(ctx, "tasks"); err != nil {
		return nil, err
	}
	s.markTaskChanged("task delete")
	return map[string]any{"id": id, "deleted": true}, nil
}

func (h taskControlPlane) insertTaskEntry(ctx context.Context, root core.ManagedMutationRoot) (map[string]any, error) {
	for _, field := range []string{"id", "origin", "status", "trace_id", "watch_id", "account_id", "owner_id", "account_ref", "owner_ref", "created_at", "updated_at"} {
		if _, exists := root.Input[field]; exists {
			return nil, fmt.Errorf("gj_task_entry %s is server-managed", field)
		}
	}
	entry, _, err := h.appendTaskEntry(ctx, taskEntrySpec{
		TaskID:     strings.TrimSpace(stringInput(root.Input, "task_id", "")),
		Origin:     "caller",
		Body:       strings.TrimSpace(stringInput(root.Input, "body", "")),
		DetailJSON: jsonStringInput(root.Input, "detail_json"),
	})
	return entry, err
}

func (h taskControlPlane) appendTaskEntry(ctx context.Context, spec taskEntrySpec) (map[string]any, bool, error) {
	s := h.service
	if _, _, _, _, ok := s.taskDB(); !ok {
		return nil, false, fmt.Errorf("task store database is not configured")
	}
	ownerID, ok := artifactUserID(ctx)
	if !ok {
		return nil, false, fmt.Errorf("gj_task_entry write requires user identity")
	}
	spec.TaskID = strings.TrimSpace(spec.TaskID)
	if spec.TaskID == "" {
		return nil, false, fmt.Errorf("gj_task_entry insert requires task_id")
	}
	task, err := s.internalTaskStoreRow(ctx, spec.TaskID)
	admin := s.identityRoleIsAdmin(ctx)
	if err != nil {
		return nil, false, err
	}
	if task == nil || !taskStatusActive(stringMapValue(task, "status")) || (!admin && stringMapValue(task, "owner_id") != ownerID) {
		return nil, false, errTaskNotFoundOrClosed
	}
	spec.Origin = strings.ToLower(strings.TrimSpace(spec.Origin))
	switch spec.Origin {
	case "caller", "agent_run", "watch_created", "verification":
	default:
		return nil, false, fmt.Errorf("gj_task_entry origin is invalid")
	}
	if spec.Origin == "caller" && strings.TrimSpace(spec.Body) == "" {
		return nil, false, fmt.Errorf("gj_task_entry insert requires body")
	}
	if len(spec.Body) > maxTaskEntryBodyBytes {
		return nil, false, fmt.Errorf("gj_task_entry body exceeds %d bytes", maxTaskEntryBodyBytes)
	}
	detailJSON, err := normalizeTaskJSON(spec.DetailJSON, s.conf.Core.EffectiveTasksConfig().SnapshotMaxBytes, "detail_json")
	if err != nil {
		return nil, false, err
	}
	id := taskEntryID(spec.TaskID, spec.Origin, spec.TraceID, spec.WatchID, spec.VerificationHash, spec.VerificationAttempt)
	if existing, err := s.internalTaskEntryStoreRow(ctx, id); err != nil {
		return nil, false, err
	} else if existing != nil {
		return taskEntryStoreRow(existing, admin), false, nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	input := map[string]any{
		"id": id, "task_id": spec.TaskID, "origin": spec.Origin, "body": spec.Body,
		"detail_json": nullableJSONString(detailJSON), "status": strings.TrimSpace(spec.Status),
		"trace_id": strings.TrimSpace(spec.TraceID), "watch_id": strings.TrimSpace(spec.WatchID),
		"account_id": stringMapValue(task, "account_id"), "owner_id": stringMapValue(task, "owner_id"),
		"created_at": now, "updated_at": now,
	}
	rows, err := s.internalStoreMutationRows(ctx, "task_entries", `insert: $input`, taskEntryStoreFields, map[string]any{"input": input})
	if err != nil {
		// Deterministic server-written entry IDs make cross-replica duplicate
		// attempts benign. Re-read before surfacing the insert race.
		if existing, readErr := s.internalTaskEntryStoreRow(ctx, id); readErr == nil && existing != nil {
			return taskEntryStoreRow(existing, admin), false, nil
		}
		return nil, false, err
	}
	if _, err := s.internalStoreMutationRows(ctx, "tasks", `where: { id: { eq: $id } }, update: $input`, taskStoreFields, map[string]any{
		"id":    spec.TaskID,
		"input": map[string]any{"last_entry_at": now, "updated_at": now},
	}); err != nil {
		return nil, false, err
	}
	if err := h.pruneTaskEntries(ctx, spec.TaskID); err != nil {
		return nil, false, err
	}
	if err := s.bumpArtifactRevision(ctx, "tasks"); err != nil {
		return nil, false, err
	}
	s.markTaskChanged("task entry append")
	if len(rows) != 0 {
		return taskEntryStoreRow(rows[0], admin), true, nil
	}
	return taskEntryStoreRow(input, admin), true, nil
}

func (h taskControlPlane) pruneTaskEntries(ctx context.Context, taskID string) error {
	rows, err := h.service.internalStoreAllRows(ctx, "task_entries", `where: { task_id: { eq: $task_id } }`, taskEntryStoreFields, map[string]any{"task_id": taskID})
	if err != nil || len(rows) == 0 {
		return err
	}
	cfg := h.service.conf.Core.EffectiveTasksConfig()
	sort.SliceStable(rows, func(i, j int) bool {
		return stringMapValue(rows[i], "created_at") > stringMapValue(rows[j], "created_at")
	})
	cutoff := time.Time{}
	if cfg.EntryRetentionHours > 0 {
		cutoff = time.Now().UTC().Add(-time.Duration(cfg.EntryRetentionHours) * time.Hour)
	}
	for index, row := range rows {
		remove := cfg.MaxEntriesPerTask > 0 && index >= cfg.MaxEntriesPerTask
		if !cutoff.IsZero() {
			if createdAt, ok := parseWatchTime(stringMapValue(row, "created_at")); ok && createdAt.Before(cutoff) {
				remove = true
			}
		}
		if !remove {
			continue
		}
		if _, err := h.service.internalStoreMutationRows(ctx, "task_entries", `where: { id: { eq: $id } }, delete: true`, `id`, map[string]any{"id": stringMapValue(row, "id")}); err != nil {
			return err
		}
	}
	return nil
}

func (h taskControlPlane) enforceTaskLimit(ctx context.Context, ownerID, id string) error {
	if h.service == nil || h.service.conf == nil {
		return nil
	}
	max := h.service.conf.Core.EffectiveTasksConfig().MaxPerOwner
	if max <= 0 {
		return nil
	}
	// This deliberately mirrors the watch cap's read-then-write behavior. A
	// shared store can briefly exceed the cap under a cross-replica race.
	rows, err := h.service.internalStoreAllRows(ctx, "tasks", `where: { owner_id: { eq: $owner_id }, status: { in: ["open", "verifying"] } }`, `id status`, map[string]any{"owner_id": ownerID})
	if err != nil {
		return err
	}
	count := 0
	for _, row := range rows {
		if stringMapValue(row, "id") != id {
			count++
		}
	}
	if count >= max {
		return fmt.Errorf("gj_task max_per_owner exceeded")
	}
	return nil
}

func (s *graphjinService) internalTaskStoreRow(ctx context.Context, id string) (map[string]any, error) {
	rows, err := s.internalStoreRows(ctx, "tasks", `where: { id: { eq: $id } }`, taskStoreFields, map[string]any{"id": id})
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

func (s *graphjinService) internalTaskEntryStoreRow(ctx context.Context, id string) (map[string]any, error) {
	rows, err := s.internalStoreRows(ctx, "task_entries", `where: { id: { eq: $id } }`, taskEntryStoreFields, map[string]any{"id": id})
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

func taskStoreRow(row map[string]any, rawIDs bool) map[string]any {
	accountID := stringMapValue(row, "account_id")
	ownerID := stringMapValue(row, "owner_id")
	out := map[string]any{
		"id": stringMapValue(row, "id"), "goal": stringMapValue(row, "goal"),
		"status": taskStatus(stringMapValue(row, "status")), "outcome": stringMapValue(row, "outcome"),
		"snapshot_json": parseJSONValue(jsonMapString(row, "snapshot_json")),
		"verify_json":   parseJSONValue(jsonMapString(row, "verify_json")), "verify_status": stringMapValue(row, "verify_status"),
		"verify_after": stringMapValue(row, "verify_after"), "verify_attempts": int64MapValue(row, "verify_attempts"),
		"owner_role": trustedWatchOwnerRole(stringMapValue(row, "owner_role"), rawIDs), "last_entry_at": stringMapValue(row, "last_entry_at"),
		"created_at": stringMapValue(row, "created_at"), "updated_at": stringMapValue(row, "updated_at"),
		"closed_at":   stringMapValue(row, "closed_at"),
		"account_ref": safeArtifactIdentity(accountID, false), "owner_ref": safeArtifactIdentity(ownerID, false),
	}
	if rawIDs {
		out["account_id"] = accountID
		out["owner_id"] = ownerID
	}
	return out
}

func taskEntryStoreRow(row map[string]any, rawIDs bool) map[string]any {
	accountID := stringMapValue(row, "account_id")
	ownerID := stringMapValue(row, "owner_id")
	out := map[string]any{
		"id": stringMapValue(row, "id"), "task_id": stringMapValue(row, "task_id"),
		"origin": stringMapValue(row, "origin"), "body": stringMapValue(row, "body"),
		"detail_json": parseJSONValue(jsonMapString(row, "detail_json")), "status": stringMapValue(row, "status"),
		"trace_id": stringMapValue(row, "trace_id"), "watch_id": stringMapValue(row, "watch_id"),
		"created_at": stringMapValue(row, "created_at"), "updated_at": stringMapValue(row, "updated_at"),
		"account_ref": safeArtifactIdentity(accountID, false), "owner_ref": safeArtifactIdentity(ownerID, false),
	}
	if rawIDs {
		out["account_id"] = accountID
		out["owner_id"] = ownerID
	}
	return out
}

func normalizeTaskJSON(value string, maxBytes int, field string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if maxBytes > 0 && len(value) > maxBytes {
		return "", fmt.Errorf("gj_task %s exceeds tasks.snapshot_max_bytes (%d)", field, maxBytes)
	}
	var decoded any
	if err := json.Unmarshal([]byte(value), &decoded); err != nil {
		return "", fmt.Errorf("gj_task %s is invalid JSON", field)
	}
	normalized, err := json.Marshal(decoded)
	if err != nil {
		return "", fmt.Errorf("gj_task %s is invalid JSON", field)
	}
	if maxBytes > 0 && len(normalized) > maxBytes {
		return "", fmt.Errorf("gj_task %s exceeds tasks.snapshot_max_bytes (%d)", field, maxBytes)
	}
	return string(normalized), nil
}

func normalizeTaskStatus(status string) (string, error) {
	status = strings.ToLower(strings.TrimSpace(status))
	if status == "" {
		return "open", nil
	}
	if status != "open" && status != "verifying" && status != "closed" {
		return "", fmt.Errorf("gj_task status must be open, verifying, or closed")
	}
	return status, nil
}

func taskStatusActive(status string) bool {
	status = taskStatus(status)
	return status == "open" || status == "verifying"
}

func taskStatus(status string) string {
	status, err := normalizeTaskStatus(status)
	if err != nil {
		return "open"
	}
	return status
}

func taskID(ownerID, goal string) string {
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	sum := sha256.Sum256([]byte(ownerID + ":task:" + goal + ":" + stamp + ":" + taskRandomHex()))
	return "task:" + hex.EncodeToString(sum[:16])
}

func taskEntryID(taskID, origin, traceID, watchID, verificationHash string, verificationAttempt int64) string {
	var key string
	switch origin {
	case "agent_run":
		if strings.TrimSpace(traceID) != "" {
			key = taskID + ":agent_run:" + strings.TrimSpace(traceID)
		}
	case "watch_created":
		if strings.TrimSpace(watchID) != "" {
			key = taskID + ":watch_created:" + strings.TrimSpace(watchID)
		}
	case "verification":
		if strings.TrimSpace(verificationHash) != "" && verificationAttempt > 0 {
			key = fmt.Sprintf("%s:verification:%s:%d", taskID, strings.TrimSpace(verificationHash), verificationAttempt)
		}
	}
	if key == "" {
		key = taskID + ":caller:" + time.Now().UTC().Format(time.RFC3339Nano) + ":" + taskRandomHex()
	}
	sum := sha256.Sum256([]byte(key))
	return "te:" + hex.EncodeToString(sum[:16])
}

func taskRandomHex() string {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err == nil {
		return hex.EncodeToString(nonce[:])
	}
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func trimTaskEntryProjectionRows(rows []map[string]any, cfg core.TasksConfig) []map[string]any {
	if len(rows) == 0 {
		return rows
	}
	cutoff := time.Time{}
	if cfg.EntryRetentionHours > 0 {
		cutoff = time.Now().UTC().Add(-time.Duration(cfg.EntryRetentionHours) * time.Hour)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return stringMapValue(rows[i], "created_at") > stringMapValue(rows[j], "created_at")
	})
	perTask := make(map[string]int)
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if !cutoff.IsZero() {
			if createdAt, ok := parseWatchTime(stringMapValue(row, "created_at")); ok && createdAt.Before(cutoff) {
				continue
			}
		}
		taskID := stringMapValue(row, "task_id")
		if cfg.MaxEntriesPerTask > 0 && perTask[taskID] >= cfg.MaxEntriesPerTask {
			continue
		}
		perTask[taskID]++
		out = append(out, row)
	}
	return out
}

func (s *graphjinService) tasksEnabled() bool {
	if s == nil {
		return false
	}
	return configTasksEnabled(s.conf)
}

func configTasksEnabled(conf *Config) bool {
	return conf != nil && conf.Core.Artifacts.Enabled && conf.Core.Tasks.Enabled
}

func (s *graphjinService) taskDB() (*sql.DB, string, string, string, bool) {
	if !s.tasksEnabled() {
		return nil, "", "", "", false
	}
	db, dbType, _, ok := s.artifactDB()
	if !ok {
		return nil, "", "", "", false
	}
	schema := s.conf.Core.EffectiveArtifactsConfig().Schema
	return db, dbType, artifactTableName(dbType, schema, "tasks"), artifactTableName(dbType, schema, "task_entries"), true
}

func taskDDL(dbType, schema string) []string {
	tasks := artifactTableName(dbType, schema, "tasks")
	entries := artifactTableName(dbType, schema, "task_entries")
	switch dbType {
	case "postgres", "":
		return []string{
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
id TEXT PRIMARY KEY,
goal TEXT NOT NULL,
status TEXT NOT NULL DEFAULT 'open',
outcome TEXT NOT NULL DEFAULT '',
snapshot_json JSONB,
verify_json JSONB,
verify_status TEXT NOT NULL DEFAULT '',
verify_after TIMESTAMPTZ,
verify_attempts BIGINT NOT NULL DEFAULT 0,
account_id TEXT NOT NULL DEFAULT '',
owner_id TEXT NOT NULL DEFAULT '',
owner_role TEXT NOT NULL DEFAULT 'user',
last_entry_at TIMESTAMPTZ,
created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
closed_at TIMESTAMPTZ
)`, tasks),
			fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (owner_id)`, quotePGIdent("idx_graphjin_tasks_owner"), tasks),
			fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (status, updated_at)`, quotePGIdent("idx_graphjin_tasks_status_updated"), tasks),
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
id TEXT PRIMARY KEY,
task_id TEXT NOT NULL,
origin TEXT NOT NULL DEFAULT 'caller',
body TEXT NOT NULL DEFAULT '',
detail_json JSONB,
status TEXT NOT NULL DEFAULT '',
trace_id TEXT NOT NULL DEFAULT '',
watch_id TEXT NOT NULL DEFAULT '',
account_id TEXT NOT NULL DEFAULT '',
owner_id TEXT NOT NULL DEFAULT '',
created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`, entries),
			fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (task_id, created_at)`, quotePGIdent("idx_graphjin_task_entries_task_created"), entries),
			fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (owner_id)`, quotePGIdent("idx_graphjin_task_entries_owner"), entries),
			fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (task_id, trace_id)`, quotePGIdent("idx_graphjin_task_entries_trace"), entries),
		}
	case "mysql", "mariadb":
		return []string{
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
id VARCHAR(191) PRIMARY KEY,
goal LONGTEXT NOT NULL,
status VARCHAR(32) NOT NULL DEFAULT 'open',
outcome LONGTEXT NOT NULL,
snapshot_json LONGTEXT,
verify_json LONGTEXT,
verify_status VARCHAR(191) NOT NULL DEFAULT '',
verify_after VARCHAR(64),
verify_attempts BIGINT NOT NULL DEFAULT 0,
account_id VARCHAR(191) NOT NULL DEFAULT '',
owner_id VARCHAR(191) NOT NULL DEFAULT '',
owner_role VARCHAR(64) NOT NULL DEFAULT 'user',
last_entry_at VARCHAR(64),
created_at VARCHAR(64) NOT NULL DEFAULT '',
updated_at VARCHAR(64) NOT NULL DEFAULT '',
closed_at VARCHAR(64),
KEY idx_graphjin_tasks_owner (owner_id),
KEY idx_graphjin_tasks_status_updated (status, updated_at)
) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`, tasks),
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
id VARCHAR(191) PRIMARY KEY,
task_id VARCHAR(191) NOT NULL,
origin VARCHAR(32) NOT NULL DEFAULT 'caller',
body LONGTEXT NOT NULL,
detail_json LONGTEXT,
status VARCHAR(32) NOT NULL DEFAULT '',
trace_id VARCHAR(191) NOT NULL DEFAULT '',
watch_id VARCHAR(191) NOT NULL DEFAULT '',
account_id VARCHAR(191) NOT NULL DEFAULT '',
owner_id VARCHAR(191) NOT NULL DEFAULT '',
created_at VARCHAR(64) NOT NULL DEFAULT '',
updated_at VARCHAR(64) NOT NULL DEFAULT '',
KEY idx_graphjin_task_entries_task_created (task_id, created_at),
KEY idx_graphjin_task_entries_owner (owner_id),
KEY idx_graphjin_task_entries_trace (task_id, trace_id)
) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`, entries),
		}
	default:
		return []string{
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
id TEXT PRIMARY KEY,
goal TEXT NOT NULL,
status TEXT NOT NULL DEFAULT 'open',
outcome TEXT NOT NULL DEFAULT '',
snapshot_json TEXT,
verify_json TEXT,
verify_status TEXT NOT NULL DEFAULT '',
verify_after TEXT,
verify_attempts INTEGER NOT NULL DEFAULT 0,
account_id TEXT NOT NULL DEFAULT '',
owner_id TEXT NOT NULL DEFAULT '',
owner_role TEXT NOT NULL DEFAULT 'user',
last_entry_at TEXT,
created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
closed_at TEXT
)`, tasks),
			fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (owner_id)`, quoteSQLIdent("idx_graphjin_tasks_owner"), tasks),
			fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (status, updated_at)`, quoteSQLIdent("idx_graphjin_tasks_status_updated"), tasks),
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
id TEXT PRIMARY KEY,
task_id TEXT NOT NULL,
origin TEXT NOT NULL DEFAULT 'caller',
body TEXT NOT NULL DEFAULT '',
detail_json TEXT,
status TEXT NOT NULL DEFAULT '',
trace_id TEXT NOT NULL DEFAULT '',
watch_id TEXT NOT NULL DEFAULT '',
account_id TEXT NOT NULL DEFAULT '',
owner_id TEXT NOT NULL DEFAULT '',
created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
)`, entries),
			fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (task_id, created_at)`, quoteSQLIdent("idx_graphjin_task_entries_task_created"), entries),
			fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (owner_id)`, quoteSQLIdent("idx_graphjin_task_entries_owner"), entries),
			fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (task_id, trace_id)`, quoteSQLIdent("idx_graphjin_task_entries_trace"), entries),
		}
	}
}

func (s *graphjinService) migrateTaskSchema(ctx context.Context, db *sql.DB, dbType, schema string) error {
	tasks := artifactTableName(dbType, schema, "tasks")
	entries := artifactTableName(dbType, schema, "task_entries")
	taskColumns := []struct{ name, definition string }{
		{"outcome", "TEXT NOT NULL DEFAULT ''"},
		{"owner_role", "TEXT NOT NULL DEFAULT 'user'"},
		{"last_entry_at", "TEXT"},
		{"closed_at", "TEXT"},
		{"verify_json", "TEXT"},
		{"verify_status", "TEXT NOT NULL DEFAULT ''"},
		{"verify_after", "TEXT"},
		{"verify_attempts", "INTEGER NOT NULL DEFAULT 0"},
	}
	entryColumns := []struct{ name, definition string }{
		{"detail_json", "TEXT"},
		{"status", "TEXT NOT NULL DEFAULT ''"},
		{"trace_id", "TEXT NOT NULL DEFAULT ''"},
		{"watch_id", "TEXT NOT NULL DEFAULT ''"},
	}
	switch dbType {
	case "postgres", "":
		taskColumns[2].definition = "TIMESTAMPTZ"
		taskColumns[3].definition = "TIMESTAMPTZ"
		taskColumns[4].definition = "JSONB"
		taskColumns[6].definition = "TIMESTAMPTZ"
		taskColumns[7].definition = "BIGINT NOT NULL DEFAULT 0"
		entryColumns[0].definition = "JSONB"
	case "mysql", "mariadb":
		taskColumns[0].definition = "LONGTEXT NOT NULL"
		taskColumns[1].definition = "VARCHAR(64) NOT NULL DEFAULT 'user'"
		taskColumns[2].definition = "VARCHAR(64)"
		taskColumns[3].definition = "VARCHAR(64)"
		taskColumns[4].definition = "LONGTEXT"
		taskColumns[5].definition = "VARCHAR(191) NOT NULL DEFAULT ''"
		taskColumns[6].definition = "VARCHAR(64)"
		taskColumns[7].definition = "BIGINT NOT NULL DEFAULT 0"
		entryColumns[0].definition = "LONGTEXT"
		entryColumns[1].definition = "VARCHAR(32) NOT NULL DEFAULT ''"
		entryColumns[2].definition = "VARCHAR(191) NOT NULL DEFAULT ''"
		entryColumns[3].definition = "VARCHAR(191) NOT NULL DEFAULT ''"
	}
	for _, column := range taskColumns {
		if err := ensureSQLColumn(ctx, db, dbType, tasks, column.name, column.definition); err != nil {
			return err
		}
	}
	for _, column := range entryColumns {
		if err := ensureSQLColumn(ctx, db, dbType, entries, column.name, column.definition); err != nil {
			return err
		}
	}
	indexName := "idx_graphjin_tasks_verify_due"
	if dbType == "mysql" || dbType == "mariadb" {
		rows, err := db.QueryContext(ctx, fmt.Sprintf("SHOW INDEX FROM %s WHERE Key_name = '%s'", tasks, indexName))
		if err == nil {
			exists := rows.Next()
			_ = rows.Close()
			if exists {
				return nil
			}
		}
		_, err = db.ExecContext(ctx, fmt.Sprintf("CREATE INDEX %s ON %s (verify_status, verify_after)", quoteStoreIdent(dbType, indexName), tasks))
		return err
	}
	_, err := db.ExecContext(ctx, fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON %s (verify_status, verify_after)", quoteStoreIdent(dbType, indexName), tasks))
	return err
}
