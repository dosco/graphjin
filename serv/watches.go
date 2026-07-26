package serv

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dosco/graphjin/core/v3"
)

const (
	watchesRootTable            = "gj_watch"
	watchEventsRootTable        = "gj_watch_event"
	watchValidationProbeTimeout = 10 * time.Second
)

const watchStoreFields = `id name task_id description query saved_query_name variables_json condition_js delivery_json enrich_json absence_json evidence_json lifecycle lease_expires_at lease_owner_id status approval enabled account_id owner_id owner_role last_data_hash last_cursor_json last_fired_at last_error failure_count created_at updated_at`

const watchEventStoreFields = `id watch_id data_hash data_json data_truncated evidence_json delivery_status delivery_attempts delivery_json receipt_json enrichment_json seen seen_at snoozed_until account_id owner_id created_at updated_at`

type watchControlPlane struct {
	service *graphjinService
}

func newWatchControlPlane(s *graphjinService) watchControlPlane {
	return watchControlPlane{service: s}
}

func (h watchControlPlane) ManagedQueryTables() []core.ManagedTable {
	if h.service == nil || !h.service.watchesEnabled() {
		return nil
	}
	return []core.ManagedTable{
		managedTable(watchesRootTable, watchColumns()),
		managedTable(watchEventsRootTable, watchEventColumns()),
	}
}

func (h watchControlPlane) ManagedMutationTables() []string {
	if h.service == nil || !h.service.watchesEnabled() {
		return nil
	}
	return []string{watchesRootTable, watchEventsRootTable}
}

func watchColumns() []core.ManagedColumn {
	return []core.ManagedColumn{
		cpCol("id", "text", true),
		cpCol("name", "text", false),
		cpCol("task_id", "text", false),
		cpCol("description", "text", false),
		cpCol("query", "text", false),
		cpCol("saved_query_name", "text", false),
		cpCol("variables_json", "json", false),
		cpCol("condition_js", "text", false),
		cpCol("delivery_json", "json", false),
		cpCol("enrich_json", "json", false),
		cpCol("absence_json", "json", false),
		cpCol("flow_review_json", "json", false),
		cpCol("action_review_json", "json", false),
		cpCol("flow_hash", "text", false),
		cpCol("flow_approval", "text", false),
		cpCol("flow_preview_json", "json", false),
		cpCol("action_hash", "text", false),
		cpCol("action_approval", "text", false),
		cpCol("evidence_json", "json", false),
		cpCol("lifecycle", "text", false),
		cpCol("lease_expires_at", "text", false),
		cpCol("lease_owner_id", "text", false),
		cpCol("status", "text", false),
		cpCol("approval", "text", false),
		cpCol("enabled", "boolean", false),
		cpCol("account_id", "text", false),
		cpCol("owner_id", "text", false),
		cpCol("owner_role", "text", false),
		cpCol("account_ref", "text", false),
		cpCol("owner_ref", "text", false),
		cpCol("last_data_hash", "text", false),
		cpCol("last_fired_at", "text", false),
		cpCol("last_error", "text", false),
		cpCol("failure_count", "integer", false),
		cpCol("created_at", "text", false),
		cpCol("updated_at", "text", false),
	}
}

func watchEventColumns() []core.ManagedColumn {
	return []core.ManagedColumn{
		cpCol("id", "text", true),
		cpCol("watch_id", "text", false),
		cpCol("data_hash", "text", false),
		cpCol("data_json", "json", false),
		cpCol("data_truncated", "boolean", false),
		cpCol("evidence_json", "json", false),
		cpCol("delivery_status", "text", false),
		cpCol("delivery_attempts", "integer", false),
		cpCol("delivery_json", "json", false),
		cpCol("receipt_json", "json", false),
		cpCol("enrichment_json", "json", false),
		cpCol("seen", "boolean", false),
		cpCol("seen_at", "text", false),
		cpCol("snoozed_until", "text", false),
		cpCol("account_id", "text", false),
		cpCol("owner_id", "text", false),
		cpCol("account_ref", "text", false),
		cpCol("owner_ref", "text", false),
		cpCol("created_at", "text", false),
		cpCol("updated_at", "text", false),
	}
}

func (h watchControlPlane) ExecuteManagedQuery(ctx context.Context, req core.ManagedQueryRequest) (json.RawMessage, error) {
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

func (h watchControlPlane) queryRows(ctx context.Context, root core.ManagedQueryRoot) ([]map[string]any, error) {
	var rows []map[string]any
	var err error
	switch root.Table {
	case watchesRootTable:
		rows, err = h.watchRows(ctx)
	case watchEventsRootTable:
		rows, err = h.watchEventRows(ctx)
	default:
		return nil, fmt.Errorf("unsupported GraphJin watch root: %s", root.Table)
	}
	if err != nil {
		return nil, err
	}
	return applyManagedQuery(rows, root), nil
}

func (h watchControlPlane) ExecuteManagedMutation(ctx context.Context, req core.ManagedMutationRequest) (json.RawMessage, error) {
	for _, root := range req.Roots {
		if root.Table == watchesRootTable && hasWatchActionReviewInput(root.Input) && len(req.Roots) != 1 {
			return nil, fmt.Errorf("gj_watch action review must be submitted as a separate mutation")
		}
	}
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

func (h watchControlPlane) mutateRow(ctx context.Context, root core.ManagedMutationRoot) (map[string]any, error) {
	switch root.Table {
	case watchesRootTable:
		switch root.Operation {
		case "insert", "upsert", "update":
			if hasWatchReviewInput(root.Input) {
				return h.reviewWatch(ctx, root)
			}
			return h.upsertWatch(ctx, root)
		case "delete":
			return h.deleteWatch(ctx, root)
		default:
			return nil, fmt.Errorf("gj_watch supports insert, update, upsert, and delete mutations")
		}
	case watchEventsRootTable:
		if root.Operation != "update" {
			return nil, fmt.Errorf("gj_watch_event supports update mutations")
		}
		return h.updateWatchEvent(ctx, root)
	default:
		return nil, fmt.Errorf("unsupported GraphJin watch mutation root: %s", root.Table)
	}
}

func (h watchControlPlane) watchRows(ctx context.Context) ([]map[string]any, error) {
	return h.watchRowsWithScope(ctx, false)
}

func (h watchControlPlane) allWatchRowsForProjection(ctx context.Context) ([]map[string]any, error) {
	return h.watchRowsWithScope(ctx, true)
}

func (h watchControlPlane) watchRowsWithScope(ctx context.Context, allOwners bool) ([]map[string]any, error) {
	s := h.service
	if _, _, _, _, ok := s.watchDB(); !ok {
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
	var rows []map[string]any
	var err error
	rows, err = s.internalStoreAllRows(ctx, "watches", args, watchStoreFields, vars)
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	for _, row := range rows {
		if !allOwners && !admin && stringMapValue(row, "owner_id") != ownerID {
			continue
		}
		out = append(out, h.projectWatchRow(ctx, row, admin || allOwners))
	}
	return out, nil
}

func (h watchControlPlane) watchEventRows(ctx context.Context) ([]map[string]any, error) {
	return h.watchEventRowsWithScope(ctx, false)
}

func (h watchControlPlane) allWatchEventRowsForProjection(ctx context.Context) ([]map[string]any, error) {
	rows, err := h.watchEventRowsWithScope(ctx, true)
	if err != nil {
		return nil, err
	}
	if h.service != nil && h.service.conf != nil {
		rows = trimWatchEventProjectionRows(rows, h.service.conf.Core.EffectiveWatchesConfig())
	}
	return rows, nil
}

func (h watchControlPlane) watchEventRowsWithScope(ctx context.Context, allOwners bool) ([]map[string]any, error) {
	s := h.service
	if _, _, _, _, ok := s.watchDB(); !ok {
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
	var rows []map[string]any
	var err error
	rows, err = s.internalStoreAllRows(ctx, "watch_events", args, watchEventStoreFields, vars)
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	for _, row := range rows {
		if !allOwners && !admin && stringMapValue(row, "owner_id") != ownerID {
			continue
		}
		out = append(out, watchEventStoreRow(row, admin || allOwners))
	}
	return out, nil
}

func (h watchControlPlane) upsertWatch(ctx context.Context, root core.ManagedMutationRoot) (map[string]any, error) {
	s := h.service
	if _, _, _, _, ok := s.watchDB(); !ok {
		return nil, fmt.Errorf("watch store database is not configured")
	}
	ownerID, ok := artifactUserID(ctx)
	if !ok {
		return nil, fmt.Errorf("gj_watch write requires user identity")
	}
	for _, field := range []string{
		"approval",
		"flow_hash",
		"flow_approval",
		"flow_preview_json",
		"action_hash",
		"action_approval",
	} {
		if _, exists := root.Input[field]; exists {
			return nil, fmt.Errorf("gj_watch %s is server-managed; use flow_review_json or action_review_json", field)
		}
	}
	name := strings.TrimSpace(stringInput(root.Input, "name", ""))
	if name == "" {
		return nil, fmt.Errorf("gj_watch mutation requires name")
	}
	admin := s.identityRoleIsAdmin(ctx)
	id := watchID(ownerID, name)
	if admin {
		id = strings.TrimSpace(stringInput(root.Input, "id", id))
		if id == "" {
			return nil, fmt.Errorf("gj_watch id cannot be empty")
		}
	}
	query := strings.TrimSpace(stringInput(root.Input, "query", ""))
	savedQuery := strings.TrimSpace(stringInput(root.Input, "saved_query_name", ""))
	variablesJSON := jsonStringInput(root.Input, "variables_json")
	evidence, err := h.validateWatchDefinition(ctx, id, query, savedQuery, variablesJSON)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := h.enforceWatchLimit(ctx, ownerID, id); err != nil {
		return nil, err
	}
	existing, err := s.internalWatchStoreRow(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing != nil && !admin && stringMapValue(existing, "owner_id") != ownerID {
		return nil, fmt.Errorf("gj_watch write denied")
	}
	taskID := strings.TrimSpace(stringInput(root.Input, "task_id", ""))
	_, taskIDSupplied := root.Input["task_id"]
	if !taskIDSupplied && existing != nil {
		taskID = stringMapValue(existing, "task_id")
	}
	if taskIDSupplied && taskID != "" {
		task, taskErr := s.internalTaskStoreRow(ctx, taskID)
		if taskErr != nil {
			return nil, taskErr
		}
		if task == nil || taskStatus(stringMapValue(task, "status")) != "open" || (!admin && stringMapValue(task, "owner_id") != ownerID) {
			return nil, errTaskNotFoundOrClosed
		}
	}
	accountID, _ := identityVarString(ctx, "account_id")
	ownerRole := runtimeRoleClass(ctx)
	description := stringInput(root.Input, "description", "")
	conditionJS := stringInput(root.Input, "condition_js", "")
	deliveryJSON := jsonStringInput(root.Input, "delivery_json")
	enrichJSON := jsonStringInput(root.Input, "enrich_json")
	absenceJSON := jsonStringInput(root.Input, "absence_json")
	if maxBytes := s.conf.Core.EffectiveWatchesConfig().SnapshotMaxBytes; maxBytes > 0 {
		for _, field := range []struct{ name, value string }{
			{"variables_json", variablesJSON},
			{"delivery_json", deliveryJSON},
			{"enrich_json", enrichJSON},
			{"absence_json", absenceJSON},
		} {
			if len(field.value) > maxBytes {
				return nil, fmt.Errorf("gj_watch %s exceeds watches.snapshot_max_bytes (%d)", field.name, maxBytes)
			}
		}
	}
	deliveryJSON, _, _, err = normalizeWatchDeliveryJSON(deliveryJSON)
	if err != nil {
		return nil, fmt.Errorf("gj_watch %w", err)
	}
	absenceJSON, _, _, err = normalizeWatchAbsenceJSON(absenceJSON)
	if err != nil {
		return nil, fmt.Errorf("gj_watch %w", err)
	}
	var enrichCfg watchEnrichmentConfig
	var enrichEnabled bool
	enrichJSON, enrichCfg, enrichEnabled, err = normalizeWatchEnrichmentJSON(enrichJSON)
	if err != nil {
		return nil, fmt.Errorf("gj_watch %w", err)
	}
	actionProposal, err := s.watchActionProposal(ctx, query, savedQuery, variablesJSON, enrichJSON, deliveryJSON, absenceJSON)
	if err != nil {
		return nil, fmt.Errorf("gj_watch %w", err)
	}
	lifecycle := watchLifecycle(stringInput(root.Input, "lifecycle", "durable"))
	leaseExpiresAt := strings.TrimSpace(stringInput(root.Input, "lease_expires_at", ""))
	leaseOwnerID := ""
	if lifecycle == "ephemeral" {
		if leaseExpiresAt == "" {
			return nil, fmt.Errorf("gj_watch lease_expires_at is required for ephemeral watches")
		}
		expiresAt, ok := parseWatchTime(leaseExpiresAt)
		if !ok {
			return nil, fmt.Errorf("gj_watch lease_expires_at must be an RFC3339 timestamp")
		}
		if !expiresAt.After(time.Now().UTC()) {
			return nil, fmt.Errorf("gj_watch lease_expires_at must be in the future")
		}
		leaseExpiresAt = expiresAt.UTC().Format(time.RFC3339)
		leaseOwnerID = ownerID
	}
	if maxBytes := s.conf.Core.EffectiveWatchesConfig().SnapshotMaxBytes; maxBytes > 0 {
		for _, f := range []struct{ name, value string }{
			{"variables_json", variablesJSON},
			{"delivery_json", deliveryJSON},
			{"enrich_json", enrichJSON},
			{"absence_json", absenceJSON},
		} {
			if len(f.value) > maxBytes {
				return nil, fmt.Errorf("gj_watch %s exceeds watches.snapshot_max_bytes (%d)", f.name, maxBytes)
			}
		}
	}
	evidenceMap := map[string]any{}
	if existing != nil {
		evidenceMap = mapFromAny(parseJSONValue(jsonMapString(existing, "evidence_json")))
		if evidenceMap == nil {
			evidenceMap = map[string]any{}
		}
	}
	for key, value := range evidence {
		evidenceMap[key] = value
	}
	statusDefault, enabledDefault := "active", true
	if existing != nil {
		statusDefault = stringMapValue(existing, "status")
		enabledDefault = boolMapValue(existing, "enabled")
	}
	status := watchStatus(stringInput(root.Input, "status", statusDefault))
	enabled := boolInput(root.Input, "enabled", enabledDefault)
	resumeIntent := status == "active" && enabled
	_, statusSpecified := root.Input["status"]
	_, enabledSpecified := root.Input["enabled"]
	if existing != nil && !statusSpecified && !enabledSpecified {
		if storedResume, ok := evidenceMap[watchReviewResumeKey]; ok {
			resumeIntent = boolValue(storedResume)
		}
	}
	createdAt := now
	lastDataHash := ""
	lastCursorJSON := ""
	lastFiredAt := ""
	lastError := ""
	failureCount := int64(0)
	definitionChanged := existing == nil
	flowRequired := enrichEnabled && enrichCfg.Kind == "flow"
	flowChanged := flowRequired && existing == nil
	if existing != nil {
		createdAt = stringMapValue(existing, "created_at")
		if createdAt == "" {
			createdAt = now
		}
		definitionChanged = query != stringMapValue(existing, "query") ||
			savedQuery != stringMapValue(existing, "saved_query_name") ||
			variablesJSON != jsonMapString(existing, "variables_json") ||
			absenceJSON != jsonMapString(existing, "absence_json")
		_, existingEnrichCfg, existingEnrichEnabled, existingEnrichErr := normalizeWatchEnrichmentJSON(jsonMapString(existing, "enrich_json"))
		existingFlowRequired := existingEnrichErr == nil && existingEnrichEnabled && existingEnrichCfg.Kind == "flow"
		flowChanged = flowRequired != existingFlowRequired ||
			(flowRequired && (existingEnrichErr != nil || enrichCfg.FlowHash != existingEnrichCfg.FlowHash))
		definitionChanged = definitionChanged || flowChanged
		lastDataHash = stringMapValue(existing, "last_data_hash")
		lastCursorJSON = jsonMapString(existing, "last_cursor_json")
		lastFiredAt = stringMapValue(existing, "last_fired_at")
		lastError = stringMapValue(existing, "last_error")
		failureCount = int64MapValue(existing, "failure_count")
		if definitionChanged {
			lastDataHash = ""
			lastCursorJSON = ""
			lastFiredAt = ""
			lastError = ""
			failureCount = 0
		}
	}
	ensureWatchReviewEvidence(evidenceMap, enrichCfg, flowRequired, actionProposal, now)
	flowApproval := watchFlowApproval(evidenceMap, enrichCfg, flowRequired)
	actionApproval := watchActionApproval(evidenceMap, actionProposal)
	approval := combinedWatchApproval(flowApproval, actionApproval)
	if approval != watchReviewApproved {
		evidenceMap[watchReviewResumeKey] = resumeIntent
		status = "paused"
		enabled = false
	} else {
		if _, hadReviewResume := evidenceMap[watchReviewResumeKey]; hadReviewResume && resumeIntent {
			status = "active"
			enabled = true
		}
		delete(evidenceMap, watchReviewResumeKey)
	}
	if err := h.validateWatchCycle(ctx, id, lifecycle, status, enabled,
		watchedWatchIDsFromEvidence(evidenceMap)); err != nil {
		return nil, err
	}
	evidenceJSON := mustMarshalString(evidenceMap)
	input := map[string]any{
		"id": id, "name": name, "task_id": taskID, "description": description, "query": query, "saved_query_name": savedQuery,
		"variables_json": nullableJSONString(variablesJSON), "condition_js": conditionJS,
		"delivery_json": nullableJSONString(deliveryJSON), "enrich_json": nullableJSONString(enrichJSON), "absence_json": nullableJSONString(absenceJSON), "evidence_json": nullableJSONString(evidenceJSON),
		"lifecycle": lifecycle, "lease_expires_at": nullableJSONString(leaseExpiresAt), "lease_owner_id": leaseOwnerID,
		"status": status, "approval": approval, "enabled": enabled, "account_id": accountID, "owner_id": ownerID, "owner_role": ownerRole,
		"last_data_hash": lastDataHash, "last_cursor_json": nullableJSONString(lastCursorJSON), "last_fired_at": nullableJSONString(lastFiredAt), "last_error": lastError, "failure_count": failureCount,
		"created_at": createdAt, "updated_at": now,
	}
	var rows []map[string]any
	if existing == nil {
		rows, err = s.internalStoreMutationRows(ctx, "watches", `insert: $input`, watchStoreFields, map[string]any{"input": input})
	} else {
		update := cloneMap(input)
		delete(update, "id")
		delete(update, "account_id")
		delete(update, "owner_id")
		delete(update, "owner_role")
		if !definitionChanged {
			delete(update, "last_data_hash")
			delete(update, "last_cursor_json")
			delete(update, "last_fired_at")
			delete(update, "last_error")
			delete(update, "failure_count")
		}
		delete(update, "created_at")
		rows, err = s.internalStoreMutationRows(ctx, "watches", `where: { id: { eq: $id } }, update: $input`, watchStoreFields, map[string]any{"id": id, "input": update})
	}
	if err != nil {
		return nil, err
	}
	if err := s.bumpArtifactRevision(ctx, "watches"); err != nil {
		return nil, err
	}
	s.markWatchChanged("watch mutation")
	s.publishWatchRunnerChanged(ctx)
	if taskIDSupplied && taskID != "" {
		if _, _, err := newTaskControlPlane(s).appendTaskEntry(ctx, taskEntrySpec{
			TaskID: taskID, Origin: "watch_created", WatchID: id,
			Body:       fmt.Sprintf("Created watch %s (%s) under this task.", name, id),
			DetailJSON: mustMarshalString(map[string]any{"watch_id": id, "watch_name": name}),
		}); err != nil {
			return nil, err
		}
	}
	if len(rows) != 0 {
		return h.projectWatchRow(ctx, rows[0], admin), nil
	}
	return h.projectWatchRow(ctx, input, admin), nil
}

func (h watchControlPlane) deleteWatch(ctx context.Context, root core.ManagedMutationRoot) (map[string]any, error) {
	s := h.service
	if _, _, _, _, ok := s.watchDB(); !ok {
		return nil, fmt.Errorf("watch store database is not configured")
	}
	ownerID, ok := artifactUserID(ctx)
	if !ok {
		return nil, fmt.Errorf("gj_watch delete requires user identity")
	}
	id := stringInput(root.Input, "id", "")
	if id == "" {
		id = stringWhere(root.Where, "id")
	}
	if id == "" {
		return nil, fmt.Errorf("gj_watch delete requires id")
	}
	admin := s.identityRoleIsAdmin(ctx)
	existing, err := s.internalWatchStoreRow(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing == nil || (!admin && stringMapValue(existing, "owner_id") != ownerID) {
		return map[string]any{"id": id, "deleted": true}, nil
	}
	if _, err := s.internalStoreMutationRows(ctx, "watches", `where: { id: { eq: $id } }, delete: true`, `id`, map[string]any{"id": id}); err != nil {
		return nil, err
	}
	if err := s.bumpArtifactRevision(ctx, "watches"); err != nil {
		return nil, err
	}
	deletedEvents, err := s.deleteWatchEvents(ctx, id)
	if err != nil {
		return nil, err
	}
	if deletedEvents != 0 {
		if err := s.bumpArtifactRevision(ctx, "watch_events"); err != nil {
			return nil, err
		}
	}
	s.markWatchChanged("watch delete")
	s.publishWatchRunnerChanged(ctx)
	return map[string]any{"id": id, "deleted": true}, nil
}

func (h watchControlPlane) updateWatchEvent(ctx context.Context, root core.ManagedMutationRoot) (map[string]any, error) {
	s := h.service
	if _, _, _, _, ok := s.watchDB(); !ok {
		return nil, fmt.Errorf("watch event store database is not configured")
	}
	ownerID, ok := artifactUserID(ctx)
	if !ok {
		return nil, fmt.Errorf("gj_watch_event update requires user identity")
	}
	id := stringInput(root.Input, "id", "")
	if id == "" {
		id = stringWhere(root.Where, "id")
	}
	if id == "" {
		return nil, fmt.Errorf("gj_watch_event update requires id")
	}
	_, seenSpecified := root.Input["seen"]
	_, snoozeSpecified := root.Input["snoozed_until"]
	seen := boolInput(root.Input, "seen", true)
	nowTime := time.Now().UTC()
	now := nowTime.Format(time.RFC3339Nano)
	admin := s.identityRoleIsAdmin(ctx)
	existing, err := s.internalWatchEventStoreRow(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing == nil || (!admin && stringMapValue(existing, "owner_id") != ownerID) {
		return nil, fmt.Errorf("gj_watch_event update denied")
	}
	update := map[string]any{"updated_at": now}
	if seenSpecified || !snoozeSpecified {
		update["seen"] = seen
		update["seen_at"] = nullableSeenAt(seen, now)
	}
	snoozedUntil := stringMapValue(existing, "snoozed_until")
	if snoozeSpecified {
		raw := root.Input["snoozed_until"]
		snoozedUntil = ""
		if raw != nil && !strings.EqualFold(strings.TrimSpace(fmt.Sprint(raw)), "null") {
			requested, ok := parseWatchTime(fmt.Sprint(raw))
			if !ok {
				return nil, fmt.Errorf("gj_watch_event snoozed_until must be an RFC3339 timestamp or null")
			}
			if !requested.After(nowTime) {
				return nil, fmt.Errorf("gj_watch_event snoozed_until must be in the future")
			}
			if retention := s.conf.Core.EffectiveWatchesConfig().EventRetentionHours; retention > 0 {
				maxSnooze := nowTime.Add(time.Duration(retention) * time.Hour)
				if requested.After(maxSnooze) {
					requested = maxSnooze
				}
			}
			snoozedUntil = requested.UTC().Format(time.RFC3339Nano)
		}
		update["snoozed_until"] = nullableJSONString(snoozedUntil)
	}
	if _, err := s.internalStoreMutationRows(ctx, "watch_events", `where: { id: { eq: $id } }, update: $input`, watchEventStoreFields, map[string]any{"id": id, "input": update}); err != nil {
		return nil, err
	}
	if err := s.bumpArtifactRevision(ctx, "watch_events"); err != nil {
		return nil, err
	}
	s.markWatchChanged("watch event update")
	s.notifyWatchEventsResource(
		stringMapValue(existing, "owner_id"),
		stringMapValue(existing, "account_id"),
		stringMapValue(existing, "watch_id"),
	)
	result := map[string]any{
		"id": id, "seen": boolMapValue(existing, "seen"), "seen_at": stringMapValue(existing, "seen_at"),
		"snoozed_until": snoozedUntil, "updated_at": now,
	}
	if _, ok := update["seen"]; ok {
		result["seen"] = seen
		result["seen_at"] = nullableSeenAt(seen, now)
	}
	return result, nil
}

func (h watchControlPlane) validateWatchDefinition(
	ctx context.Context,
	effectiveWatchID, query, savedQuery, variablesJSON string,
) (map[string]any, error) {
	query = strings.TrimSpace(query)
	savedQuery = strings.TrimSpace(savedQuery)
	if query == "" && savedQuery == "" {
		return nil, fmt.Errorf("gj_watch requires query or saved_query_name")
	}
	vars, err := watchVariablesWithCursor(variablesJSON, "")
	if err != nil {
		if strings.Contains(err.Error(), "variables_json") {
			return nil, fmt.Errorf("gj_watch %w", err)
		}
		return nil, err
	}
	evidence := map[string]any{
		"validated_at": time.Now().UTC().Format(time.RFC3339),
	}
	if query != "" {
		header, err := core.Operation(query)
		if err != nil {
			return nil, err
		}
		if header.Type != core.OpSubscription {
			return nil, fmt.Errorf("gj_watch query must be a subscription")
		}
		evidence["operation"] = "subscription"
		evidence["operation_name"] = header.Name
		if h.service != nil && h.service.gj != nil {
			probeCtx, cancel := context.WithTimeout(ctx, watchValidationProbeTimeout)
			member, err := h.service.gj.Subscribe(probeCtx, query, vars, nil)
			cursorVars := memberCursorVariableNames(member)
			if member != nil {
				member.Unsubscribe()
			}
			cancel()
			if err != nil {
				return nil, fmt.Errorf("gj_watch subscription probe failed: %w", err)
			}
			if len(cursorVars) == 0 {
				return nil, fmt.Errorf("gj_watch subscription must use cursor pagination")
			}
			watchedIDs, err := watchedWatchIDsForMember(member, effectiveWatchID)
			if err != nil {
				return nil, err
			}
			evidence["probe"] = "ok"
			evidence["cursor_variables"] = cursorVars
			evidence["watched_watch_ids"] = watchedIDs
		}
		return evidence, nil
	}
	if h.service == nil || h.service.gj == nil {
		return nil, fmt.Errorf("gj_watch saved_query_name validation requires initialized GraphJin core")
	}
	details, err := h.service.gj.GetSavedQuery(savedQuery)
	if err != nil {
		return nil, err
	}
	header, err := core.Operation(details.Query)
	if err != nil {
		return nil, err
	}
	if header.Type != core.OpSubscription {
		return nil, fmt.Errorf("gj_watch saved_query_name must reference a subscription")
	}
	probeCtx, cancel := context.WithTimeout(ctx, watchValidationProbeTimeout)
	member, err := h.service.gj.SubscribeByName(probeCtx, savedQuery, vars, nil)
	cursorVars := memberCursorVariableNames(member)
	if member != nil {
		member.Unsubscribe()
	}
	cancel()
	if err != nil {
		return nil, fmt.Errorf("gj_watch saved-query subscription probe failed: %w", err)
	}
	if len(cursorVars) == 0 {
		return nil, fmt.Errorf("gj_watch saved_query_name subscription must use cursor pagination")
	}
	watchedIDs, err := watchedWatchIDsForMember(member, effectiveWatchID)
	if err != nil {
		return nil, err
	}
	evidence["operation"] = "subscription"
	evidence["operation_name"] = header.Name
	evidence["saved_query_name"] = savedQuery
	evidence["probe"] = "ok"
	evidence["cursor_variables"] = cursorVars
	evidence["watched_watch_ids"] = watchedIDs
	return evidence, nil
}

func watchedWatchIDsForMember(member *core.Member, effectiveWatchID string) ([]string, error) {
	if member == nil {
		return nil, nil
	}
	return watchedWatchIDsForRoots(member.SubscriptionRoots(), effectiveWatchID)
}

func watchedWatchIDsForRoots(roots []core.SubscriptionRootInfo, effectiveWatchID string) ([]string, error) {
	ids := make(map[string]struct{})
	for _, root := range roots {
		if strings.EqualFold(strings.TrimSpace(root.Table), tasksRootTable) || strings.EqualFold(strings.TrimSpace(root.Table), taskEntriesRootTable) {
			return nil, fmt.Errorf("gj_watch subscriptions over gj_task or gj_task_entry are not supported")
		}
		if !strings.EqualFold(strings.TrimSpace(root.Table), watchEventsRootTable) {
			continue
		}
		rootIDs, unsafe, found := conjunctiveWatchIDs(root.Filter)
		if unsafe {
			return nil, fmt.Errorf("gj_watch gj_watch_event watch_id filter must be conjunctive and cannot appear under or/not")
		}
		if !found || len(rootIDs) == 0 {
			return nil, fmt.Errorf("gj_watch gj_watch_event subscription requires a non-empty conjunctive watch_id eq/in filter")
		}
		for _, id := range rootIDs {
			if id == effectiveWatchID {
				return nil, fmt.Errorf("gj_watch gj_watch_event subscription cannot include its own watch_id %q", effectiveWatchID)
			}
			ids[id] = struct{}{}
		}
	}
	out := make([]string, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	sort.Strings(out)
	return out, nil
}

func conjunctiveWatchIDs(filter map[string]any) (ids []string, unsafe, found bool) {
	for key, value := range filter {
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "and":
			for _, item := range watchAnySlice(value) {
				child, _ := item.(map[string]any)
				childIDs, childUnsafe, childFound := conjunctiveWatchIDs(child)
				ids = append(ids, childIDs...)
				unsafe = unsafe || childUnsafe
				found = found || childFound
			}
		case "or", "not":
			if filterContainsWatchID(value) {
				unsafe = true
			}
		case "watch_id":
			found = true
			condition, ok := value.(map[string]any)
			if !ok {
				continue
			}
			for operator, operand := range condition {
				switch strings.ToLower(strings.TrimSpace(operator)) {
				case "eq":
					if id := strings.TrimSpace(stringFromAny(operand)); id != "" {
						ids = append(ids, id)
					}
				case "in":
					for _, item := range watchAnySlice(operand) {
						if id := strings.TrimSpace(stringFromAny(item)); id != "" {
							ids = append(ids, id)
						}
					}
				}
			}
		}
	}
	return ids, unsafe, found
}

func filterContainsWatchID(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if strings.EqualFold(strings.TrimSpace(key), "watch_id") || filterContainsWatchID(item) {
				return true
			}
		}
	case []any:
		for _, item := range typed {
			if filterContainsWatchID(item) {
				return true
			}
		}
	}
	return false
}

func watchAnySlice(value any) []any {
	switch typed := value.(type) {
	case []any:
		return typed
	case []string:
		out := make([]any, len(typed))
		for index := range typed {
			out[index] = typed[index]
		}
		return out
	default:
		return nil
	}
}

func watchedWatchIDsFromEvidence(evidence map[string]any) []string {
	var ids []string
	for _, value := range watchAnySlice(evidence["watched_watch_ids"]) {
		if id := strings.TrimSpace(stringFromAny(value)); id != "" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

func sameWatchIDSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	counts := make(map[string]int, len(left))
	for _, id := range left {
		counts[id]++
	}
	for _, id := range right {
		if counts[id] == 0 {
			return false
		}
		counts[id]--
	}
	return true
}

func (h watchControlPlane) validateWatchCycle(
	ctx context.Context,
	candidateID, lifecycle, status string,
	enabled bool,
	candidateEdges []string,
) error {
	rows, err := h.service.internalStoreAllRows(ctx, "watches", "", watchStoreFields, nil)
	if err != nil {
		return err
	}
	graph := make(map[string][]string, len(rows)+1)
	for _, row := range rows {
		id := stringMapValue(row, "id")
		if id == candidateID ||
			!boolMapValue(row, "enabled") ||
			watchLifecycle(stringMapValue(row, "lifecycle")) != "durable" ||
			watchStatus(stringMapValue(row, "status")) != "active" {
			continue
		}
		evidence := mapFromAny(parseJSONValue(jsonMapString(row, "evidence_json")))
		graph[id] = watchedWatchIDsFromEvidence(evidence)
	}
	if enabled && watchLifecycle(lifecycle) == "durable" && watchStatus(status) == "active" {
		graph[candidateID] = append([]string(nil), candidateEdges...)
	}
	if cycleID, cyclic := watchGraphCycle(graph); cyclic {
		return fmt.Errorf("gj_watch dependency cycle detected involving %q", cycleID)
	}
	return nil
}

func watchGraphCycle(graph map[string][]string) (string, bool) {
	nodes := make(map[string]struct{}, len(graph))
	for node, edges := range graph {
		nodes[node] = struct{}{}
		for _, edge := range edges {
			nodes[edge] = struct{}{}
		}
	}
	ordered := make([]string, 0, len(nodes))
	for node := range nodes {
		ordered = append(ordered, node)
	}
	sort.Strings(ordered)

	state := make(map[string]uint8, len(nodes))
	var visit func(string) (string, bool)
	visit = func(node string) (string, bool) {
		switch state[node] {
		case 1:
			return node, true
		case 2:
			return "", false
		}
		state[node] = 1
		for _, next := range graph[node] {
			if cycleID, cyclic := visit(next); cyclic {
				return cycleID, true
			}
		}
		state[node] = 2
		return "", false
	}
	for _, node := range ordered {
		if cycleID, cyclic := visit(node); cyclic {
			return cycleID, true
		}
	}
	return "", false
}

func watchGraphReaches(
	graph map[string][]string,
	current, target string,
	visiting map[string]bool,
	skipInitial bool,
) bool {
	if !skipInitial && current == target {
		return true
	}
	if visiting[current] {
		return false
	}
	visiting[current] = true
	defer delete(visiting, current)
	for _, next := range graph[current] {
		if watchGraphReaches(graph, next, target, visiting, false) {
			return true
		}
	}
	return false
}

func memberCursorVariableNames(member *core.Member) []string {
	if member == nil {
		return nil
	}
	return member.CursorVariableNames()
}

func (s *graphjinService) internalWatchStoreRow(ctx context.Context, id string) (map[string]any, error) {
	rows, err := s.internalStoreRows(ctx, "watches", `where: { id: { eq: $id } }`, watchStoreFields, map[string]any{"id": id})
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

func (s *graphjinService) internalWatchEventStoreRow(ctx context.Context, id string) (map[string]any, error) {
	rows, err := s.internalStoreRows(ctx, "watch_events", `where: { id: { eq: $id } }`, watchEventStoreFields, map[string]any{"id": id})
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

func (s *graphjinService) deleteWatchEvents(ctx context.Context, watchID string) (int, error) {
	if strings.TrimSpace(watchID) == "" {
		return 0, nil
	}
	rows, err := s.internalStoreAllRows(ctx, "watch_events",
		`where: { watch_id: { eq: $watch_id } }`,
		`id watch_id owner_id account_id`,
		map[string]any{"watch_id": watchID})
	if err != nil || len(rows) == 0 {
		return 0, err
	}
	scopes := watchEventScopesFromRows(rows)
	if _, err := s.internalStoreMutationRows(ctx, "watch_events",
		`where: { watch_id: { eq: $watch_id } }, delete: true`,
		`id`,
		map[string]any{"watch_id": watchID}); err != nil {
		return 0, err
	}
	s.notifyWatchEventScopes(scopes)
	return len(rows), nil
}

func (s *graphjinService) deleteWatchEventByID(ctx context.Context, id string) (int, error) {
	if strings.TrimSpace(id) == "" {
		return 0, nil
	}
	rows, err := s.internalStoreRows(ctx, "watch_events",
		`where: { id: { eq: $id } }`,
		`id watch_id owner_id account_id`,
		map[string]any{"id": id})
	if err != nil || len(rows) == 0 {
		return 0, err
	}
	scopes := watchEventScopesFromRows(rows)
	if _, err := s.internalStoreMutationRows(ctx, "watch_events",
		`where: { id: { eq: $id } }, delete: true`,
		`id`,
		map[string]any{"id": id}); err != nil {
		return 0, err
	}
	s.notifyWatchEventScopes(scopes)
	return len(rows), nil
}

func (h watchControlPlane) enforceWatchLimit(ctx context.Context, ownerID, id string) error {
	if h.service == nil || h.service.conf == nil {
		return nil
	}
	max := h.service.conf.Core.EffectiveWatchesConfig().MaxPerOwner
	if max <= 0 {
		return nil
	}
	rows, err := h.service.internalStoreAllRows(ctx, "watches", `where: { owner_id: { eq: $owner_id } }`, `id`, map[string]any{"owner_id": ownerID})
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
		return fmt.Errorf("gj_watch max_per_owner exceeded")
	}
	return nil
}

func (s *graphjinService) watchesEnabled() bool {
	if s == nil {
		return false
	}
	return configWatchesEnabled(s.conf)
}

func configWatchesEnabled(conf *Config) bool {
	return conf != nil && conf.Core.Artifacts.Enabled && conf.Core.Watches.Enabled
}

func (s *graphjinService) watchDB() (*sql.DB, string, string, string, bool) {
	if !s.watchesEnabled() {
		return nil, "", "", "", false
	}
	db, dbType, _, ok := s.artifactDB()
	if !ok {
		return nil, "", "", "", false
	}
	schema := s.conf.Core.EffectiveArtifactsConfig().Schema
	return db, dbType, artifactTableName(dbType, schema, "watches"), artifactTableName(dbType, schema, "watch_events"), true
}

func watchDDL(dbType, schema string) []string {
	watches := artifactTableName(dbType, schema, "watches")
	events := artifactTableName(dbType, schema, "watch_events")
	switch dbType {
	case "postgres", "":
		return []string{
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
id TEXT PRIMARY KEY,
name TEXT NOT NULL,
task_id TEXT NOT NULL DEFAULT '',
description TEXT NOT NULL DEFAULT '',
query TEXT NOT NULL DEFAULT '',
saved_query_name TEXT NOT NULL DEFAULT '',
variables_json JSONB,
condition_js TEXT NOT NULL DEFAULT '',
delivery_json JSONB,
enrich_json JSONB,
absence_json JSONB,
evidence_json JSONB,
lifecycle TEXT NOT NULL DEFAULT 'durable',
lease_expires_at TIMESTAMPTZ,
lease_owner_id TEXT NOT NULL DEFAULT '',
status TEXT NOT NULL DEFAULT 'active',
approval TEXT NOT NULL DEFAULT 'approved',
enabled BOOLEAN NOT NULL DEFAULT TRUE,
account_id TEXT NOT NULL DEFAULT '',
owner_id TEXT NOT NULL DEFAULT '',
owner_role TEXT NOT NULL DEFAULT 'user',
last_data_hash TEXT NOT NULL DEFAULT '',
last_cursor_json JSONB,
last_fired_at TIMESTAMPTZ,
last_error TEXT NOT NULL DEFAULT '',
failure_count BIGINT NOT NULL DEFAULT 0,
created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`, watches),
			fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (owner_id)`, quotePGIdent("idx_graphjin_watches_owner"), watches),
			fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (enabled, status)`, quotePGIdent("idx_graphjin_watches_enabled"), watches),
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
id TEXT PRIMARY KEY,
watch_id TEXT NOT NULL,
data_hash TEXT NOT NULL DEFAULT '',
data_json JSONB,
data_truncated BOOLEAN NOT NULL DEFAULT FALSE,
evidence_json JSONB,
delivery_status TEXT NOT NULL DEFAULT 'pending',
delivery_attempts BIGINT NOT NULL DEFAULT 0,
delivery_json JSONB,
receipt_json JSONB,
enrichment_json JSONB,
seen BOOLEAN NOT NULL DEFAULT FALSE,
seen_at TIMESTAMPTZ,
snoozed_until TIMESTAMPTZ,
account_id TEXT NOT NULL DEFAULT '',
owner_id TEXT NOT NULL DEFAULT '',
created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`, events),
			fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (watch_id, data_hash)`, quotePGIdent("idx_graphjin_watch_events_dedup"), events),
			fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (owner_id, seen, created_at)`, quotePGIdent("idx_graphjin_watch_events_owner_seen"), events),
			fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (delivery_status, created_at)`, quotePGIdent("idx_graphjin_watch_events_delivery"), events),
		}
	case "mysql", "mariadb":
		return []string{
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
id VARCHAR(191) PRIMARY KEY,
name VARCHAR(255) NOT NULL,
task_id VARCHAR(191) NOT NULL DEFAULT '',
description LONGTEXT NOT NULL,
query LONGTEXT NOT NULL,
saved_query_name VARCHAR(255) NOT NULL DEFAULT '',
variables_json LONGTEXT,
condition_js LONGTEXT NOT NULL,
delivery_json LONGTEXT,
enrich_json LONGTEXT,
absence_json LONGTEXT,
evidence_json LONGTEXT,
lifecycle VARCHAR(32) NOT NULL DEFAULT 'durable',
lease_expires_at VARCHAR(64),
lease_owner_id VARCHAR(191) NOT NULL DEFAULT '',
status VARCHAR(32) NOT NULL DEFAULT 'active',
approval VARCHAR(32) NOT NULL DEFAULT 'approved',
enabled BOOLEAN NOT NULL DEFAULT TRUE,
account_id VARCHAR(191) NOT NULL DEFAULT '',
owner_id VARCHAR(191) NOT NULL DEFAULT '',
owner_role VARCHAR(64) NOT NULL DEFAULT 'user',
last_data_hash VARCHAR(128) NOT NULL DEFAULT '',
last_cursor_json LONGTEXT,
last_fired_at VARCHAR(64),
last_error LONGTEXT NOT NULL,
failure_count BIGINT NOT NULL DEFAULT 0,
created_at VARCHAR(64) NOT NULL DEFAULT '',
updated_at VARCHAR(64) NOT NULL DEFAULT ''
) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`, watches),
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
id VARCHAR(191) PRIMARY KEY,
watch_id VARCHAR(191) NOT NULL,
data_hash VARCHAR(128) NOT NULL DEFAULT '',
data_json LONGTEXT,
data_truncated BOOLEAN NOT NULL DEFAULT FALSE,
evidence_json LONGTEXT,
delivery_status VARCHAR(32) NOT NULL DEFAULT 'pending',
delivery_attempts BIGINT NOT NULL DEFAULT 0,
delivery_json LONGTEXT,
receipt_json LONGTEXT,
enrichment_json LONGTEXT,
seen BOOLEAN NOT NULL DEFAULT FALSE,
seen_at VARCHAR(64),
snoozed_until VARCHAR(64),
account_id VARCHAR(191) NOT NULL DEFAULT '',
owner_id VARCHAR(191) NOT NULL DEFAULT '',
created_at VARCHAR(64) NOT NULL DEFAULT '',
updated_at VARCHAR(64) NOT NULL DEFAULT ''
) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`, events),
		}
	default:
		return []string{
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
id TEXT PRIMARY KEY,
name TEXT NOT NULL,
task_id TEXT NOT NULL DEFAULT '',
description TEXT NOT NULL DEFAULT '',
query TEXT NOT NULL DEFAULT '',
saved_query_name TEXT NOT NULL DEFAULT '',
variables_json TEXT,
condition_js TEXT NOT NULL DEFAULT '',
delivery_json TEXT,
enrich_json TEXT,
absence_json TEXT,
evidence_json TEXT,
lifecycle TEXT NOT NULL DEFAULT 'durable',
lease_expires_at TEXT,
lease_owner_id TEXT NOT NULL DEFAULT '',
status TEXT NOT NULL DEFAULT 'active',
approval TEXT NOT NULL DEFAULT 'approved',
enabled INTEGER NOT NULL DEFAULT 1,
account_id TEXT NOT NULL DEFAULT '',
owner_id TEXT NOT NULL DEFAULT '',
owner_role TEXT NOT NULL DEFAULT 'user',
last_data_hash TEXT NOT NULL DEFAULT '',
last_cursor_json TEXT,
last_fired_at TEXT,
last_error TEXT NOT NULL DEFAULT '',
failure_count INTEGER NOT NULL DEFAULT 0,
created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
)`, watches),
			fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (owner_id)`, quoteSQLIdent("idx_graphjin_watches_owner"), watches),
			fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (enabled, status)`, quoteSQLIdent("idx_graphjin_watches_enabled"), watches),
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
id TEXT PRIMARY KEY,
watch_id TEXT NOT NULL,
data_hash TEXT NOT NULL DEFAULT '',
data_json TEXT,
data_truncated INTEGER NOT NULL DEFAULT 0,
evidence_json TEXT,
delivery_status TEXT NOT NULL DEFAULT 'pending',
delivery_attempts INTEGER NOT NULL DEFAULT 0,
delivery_json TEXT,
receipt_json TEXT,
enrichment_json TEXT,
seen INTEGER NOT NULL DEFAULT 0,
seen_at TEXT,
snoozed_until TEXT,
account_id TEXT NOT NULL DEFAULT '',
owner_id TEXT NOT NULL DEFAULT '',
created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
)`, events),
			fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (watch_id, data_hash)`, quoteSQLIdent("idx_graphjin_watch_events_dedup"), events),
			fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (owner_id, seen, created_at)`, quoteSQLIdent("idx_graphjin_watch_events_owner_seen"), events),
			fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (delivery_status, created_at)`, quoteSQLIdent("idx_graphjin_watch_events_delivery"), events),
		}
	}
}

func watchMigrationDDL(dbType, schema string) []string {
	if dbType != "postgres" && dbType != "" {
		return nil
	}
	watches := artifactTableName(dbType, schema, "watches")
	events := artifactTableName(dbType, schema, "watch_events")
	return []string{
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS owner_role TEXT NOT NULL DEFAULT 'user'`, watches),
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS task_id TEXT NOT NULL DEFAULT ''`, watches),
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS last_cursor_json JSONB`, watches),
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS lifecycle TEXT NOT NULL DEFAULT 'durable'`, watches),
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS lease_expires_at TIMESTAMPTZ`, watches),
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS lease_owner_id TEXT NOT NULL DEFAULT ''`, watches),
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS absence_json JSONB`, watches),
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS data_truncated BOOLEAN NOT NULL DEFAULT FALSE`, events),
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS snoozed_until TIMESTAMPTZ`, events),
	}
}

func (s *graphjinService) migrateWatchSchema(ctx context.Context, db *sql.DB, dbType, schema string) error {
	for _, stmt := range watchMigrationDDL(dbType, schema) {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if dbType == "postgres" || dbType == "" {
		return nil
	}
	watches := artifactTableName(dbType, schema, "watches")
	ownerRoleDef := "TEXT NOT NULL DEFAULT 'user'"
	if dbType == "mysql" || dbType == "mariadb" {
		ownerRoleDef = "VARCHAR(64) NOT NULL DEFAULT 'user'"
	}
	if err := ensureSQLColumn(ctx, db, dbType, watches, "owner_role", ownerRoleDef); err != nil {
		return err
	}
	taskIDDef := "TEXT NOT NULL DEFAULT ''"
	if dbType == "mysql" || dbType == "mariadb" {
		taskIDDef = "VARCHAR(191) NOT NULL DEFAULT ''"
	}
	if err := ensureSQLColumn(ctx, db, dbType, watches, "task_id", taskIDDef); err != nil {
		return err
	}
	lastCursorDef := "TEXT"
	if dbType == "mysql" || dbType == "mariadb" {
		lastCursorDef = "LONGTEXT"
	}
	if err := ensureSQLColumn(ctx, db, dbType, watches, "last_cursor_json", lastCursorDef); err != nil {
		return err
	}
	lifecycleDef := "TEXT NOT NULL DEFAULT 'durable'"
	leaseExpiresDef := "TEXT"
	leaseOwnerDef := "TEXT NOT NULL DEFAULT ''"
	if dbType == "mysql" || dbType == "mariadb" {
		lifecycleDef = "VARCHAR(32) NOT NULL DEFAULT 'durable'"
		leaseExpiresDef = "VARCHAR(64)"
		leaseOwnerDef = "VARCHAR(191) NOT NULL DEFAULT ''"
	}
	if err := ensureSQLColumn(ctx, db, dbType, watches, "lifecycle", lifecycleDef); err != nil {
		return err
	}
	if err := ensureSQLColumn(ctx, db, dbType, watches, "lease_expires_at", leaseExpiresDef); err != nil {
		return err
	}
	if err := ensureSQLColumn(ctx, db, dbType, watches, "lease_owner_id", leaseOwnerDef); err != nil {
		return err
	}
	absenceDef := "TEXT"
	if dbType == "mysql" || dbType == "mariadb" {
		absenceDef = "LONGTEXT"
	}
	if err := ensureSQLColumn(ctx, db, dbType, watches, "absence_json", absenceDef); err != nil {
		return err
	}
	events := artifactTableName(dbType, schema, "watch_events")
	dataTruncatedDef := "INTEGER NOT NULL DEFAULT 0"
	if dbType == "mysql" || dbType == "mariadb" {
		dataTruncatedDef = "BOOLEAN NOT NULL DEFAULT FALSE"
	}
	if err := ensureSQLColumn(ctx, db, dbType, events, "data_truncated", dataTruncatedDef); err != nil {
		return err
	}
	snoozedUntilDef := "TEXT"
	if dbType == "mysql" || dbType == "mariadb" {
		snoozedUntilDef = "VARCHAR(64)"
	}
	return ensureSQLColumn(ctx, db, dbType, events, "snoozed_until", snoozedUntilDef)
}

func watchID(ownerID, name string) string {
	sum := sha256.Sum256([]byte(ownerID + ":watch:" + name))
	return "watch:" + hex.EncodeToString(sum[:16])
}

func watchEventID(watchID, dataHash string) string {
	sum := sha256.Sum256([]byte(watchID + ":" + dataHash))
	return "we:" + hex.EncodeToString(sum[:16])
}

func watchStatus(status string) string {
	status = strings.ToLower(strings.TrimSpace(status))
	switch status {
	case "", "active", "paused", "error", "expired":
		if status == "" {
			return "active"
		}
		return status
	default:
		return "active"
	}
}

func watchLifecycle(lifecycle string) string {
	lifecycle = strings.ToLower(strings.TrimSpace(lifecycle))
	switch lifecycle {
	case "", "durable":
		return "durable"
	case "ephemeral":
		return "ephemeral"
	default:
		return "durable"
	}
}

func watchApproval(approval string) string {
	approval = strings.ToLower(strings.TrimSpace(approval))
	if approval == "" {
		return "approved"
	}
	return approval
}

func watchDeliveryStatus(status string) string {
	status = strings.ToLower(strings.TrimSpace(status))
	if status == "" {
		return "pending"
	}
	return status
}

func watchStoreRow(row map[string]any, rawIDs bool) map[string]any {
	accountID := stringMapValue(row, "account_id")
	ownerID := stringMapValue(row, "owner_id")
	out := map[string]any{
		"id":               stringMapValue(row, "id"),
		"name":             stringMapValue(row, "name"),
		"task_id":          stringMapValue(row, "task_id"),
		"description":      stringMapValue(row, "description"),
		"query":            stringMapValue(row, "query"),
		"saved_query_name": stringMapValue(row, "saved_query_name"),
		"variables_json":   row["variables_json"],
		"condition_js":     stringMapValue(row, "condition_js"),
		"delivery_json":    row["delivery_json"],
		"enrich_json":      row["enrich_json"],
		"absence_json":     row["absence_json"],
		"evidence_json":    row["evidence_json"],
		"lifecycle":        watchLifecycle(stringMapValue(row, "lifecycle")),
		"lease_expires_at": stringMapValue(row, "lease_expires_at"),
		"lease_owner_id":   safeArtifactIdentity(stringMapValue(row, "lease_owner_id"), rawIDs),
		"status":           watchStatus(stringMapValue(row, "status")),
		"approval":         watchApproval(stringMapValue(row, "approval")),
		"enabled":          boolMapValue(row, "enabled"),
		"account_id":       safeArtifactIdentity(accountID, rawIDs),
		"owner_id":         safeArtifactIdentity(ownerID, rawIDs),
		"owner_role":       trustedWatchOwnerRole(stringMapValue(row, "owner_role"), rawIDs),
		"account_ref":      safeArtifactIdentity(accountID, false),
		"owner_ref":        safeArtifactIdentity(ownerID, false),
		"last_data_hash":   stringMapValue(row, "last_data_hash"),
		"last_fired_at":    stringMapValue(row, "last_fired_at"),
		"last_error":       stringMapValue(row, "last_error"),
		"failure_count":    int64MapValue(row, "failure_count"),
		"created_at":       stringMapValue(row, "created_at"),
		"updated_at":       stringMapValue(row, "updated_at"),
	}
	evidence := mapFromAny(parseJSONValue(jsonMapString(row, "evidence_json")))
	if evidence == nil {
		evidence = map[string]any{}
	}
	_, enrichCfg, enrichEnabled, _ := normalizeWatchEnrichmentJSON(jsonMapString(row, "enrich_json"))
	flowRequired := enrichEnabled && enrichCfg.Kind == "flow"
	flowReview := mapFromAny(evidence[watchFlowReviewKey])
	actionReview := mapFromAny(evidence[watchActionReviewKey])
	out["flow_hash"] = enrichCfg.FlowHash
	out["flow_approval"] = watchFlowApproval(evidence, enrichCfg, flowRequired)
	out["flow_preview_json"] = flowReview["preview"]
	out["action_hash"] = stringFromAny(actionReview["action_hash"])
	out["action_approval"] = normalizedWatchReviewDecision(stringFromAny(actionReview["approval"]))
	if strings.TrimSpace(stringFromAny(actionReview["action_hash"])) == "" {
		out["action_approval"] = watchReviewNotRequired
	}
	return out
}

func watchEventStoreRow(row map[string]any, rawIDs bool) map[string]any {
	accountID := stringMapValue(row, "account_id")
	ownerID := stringMapValue(row, "owner_id")
	return map[string]any{
		"id":                stringMapValue(row, "id"),
		"watch_id":          stringMapValue(row, "watch_id"),
		"data_hash":         stringMapValue(row, "data_hash"),
		"data_json":         row["data_json"],
		"data_truncated":    boolMapValue(row, "data_truncated"),
		"evidence_json":     row["evidence_json"],
		"delivery_status":   watchDeliveryStatus(stringMapValue(row, "delivery_status")),
		"delivery_attempts": int64MapValue(row, "delivery_attempts"),
		"delivery_json":     row["delivery_json"],
		"receipt_json":      row["receipt_json"],
		"enrichment_json":   row["enrichment_json"],
		"seen":              boolMapValue(row, "seen"),
		"seen_at":           stringMapValue(row, "seen_at"),
		"snoozed_until":     stringMapValue(row, "snoozed_until"),
		"account_id":        safeArtifactIdentity(accountID, rawIDs),
		"owner_id":          safeArtifactIdentity(ownerID, rawIDs),
		"account_ref":       safeArtifactIdentity(accountID, false),
		"owner_ref":         safeArtifactIdentity(ownerID, false),
		"created_at":        stringMapValue(row, "created_at"),
		"updated_at":        stringMapValue(row, "updated_at"),
	}
}

func boolInput(input map[string]interface{}, key string, def bool) bool {
	if input == nil {
		return def
	}
	value, ok := input[key]
	if !ok || value == nil {
		return def
	}
	switch v := value.(type) {
	case bool:
		return v
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "1", "yes", "on":
			return true
		case "false", "0", "no", "off":
			return false
		}
	case int:
		return v != 0
	case int64:
		return v != 0
	case float64:
		return v != 0
	}
	return def
}

func nullableSeenAt(seen bool, now string) any {
	if seen {
		return now
	}
	return nil
}

func trustedWatchOwnerRole(role string, trusted bool) string {
	if !trusted {
		return ""
	}
	role = strings.TrimSpace(role)
	if role == "" || role == "unknown" || core.IsReservedRoleName(role) {
		return "user"
	}
	return role
}

func (s *graphjinService) trustedWatchRunnerRole(role string) string {
	role = trustedWatchOwnerRole(role, true)
	if s == nil || s.conf == nil || s.watchRoleAllowed(role) {
		return role
	}
	if s.log != nil {
		s.log.Warnf("watch runner: stored owner_role %q is not configured; running watch as user", role)
	}
	return "user"
}

func (s *graphjinService) watchRoleAllowed(role string) bool {
	role = strings.TrimSpace(role)
	if role == "" || role == "unknown" {
		return false
	}
	if role == "user" || role == "anon" {
		return true
	}
	if s == nil || s.conf == nil {
		return true
	}
	for _, configured := range s.conf.Core.Roles {
		if strings.EqualFold(strings.TrimSpace(configured.Name), role) {
			return true
		}
	}
	for _, admin := range s.conf.Core.EffectiveIdentityConfig().AdminRoles {
		if strings.EqualFold(strings.TrimSpace(admin), role) {
			return true
		}
	}
	return false
}
