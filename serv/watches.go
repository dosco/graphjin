package serv

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dosco/graphjin/core/v3"
)

const (
	watchesRootTable            = "gj_watch"
	watchEventsRootTable        = "gj_watch_event"
	watchValidationProbeTimeout = 10 * time.Second
)

const watchStoreFields = `id name description query saved_query_name variables_json condition_js delivery_json enrich_json evidence_json lifecycle lease_expires_at lease_owner_id status approval enabled account_id owner_id owner_role last_data_hash last_cursor_json last_fired_at last_error failure_count created_at updated_at`

const watchEventStoreFields = `id watch_id data_hash data_json data_truncated evidence_json delivery_status delivery_attempts delivery_json receipt_json enrichment_json seen seen_at account_id owner_id created_at updated_at`

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
		managedTable(watchFlowPreviewRootTable, watchFlowPreviewColumns()),
	}
}

func (h watchControlPlane) ManagedMutationTables() []string {
	if h.service == nil || !h.service.watchesEnabled() {
		return nil
	}
	return []string{watchesRootTable, watchEventsRootTable, watchFlowPreviewRootTable}
}

func watchColumns() []core.ManagedColumn {
	return []core.ManagedColumn{
		cpCol("id", "text", true),
		cpCol("name", "text", false),
		cpCol("description", "text", false),
		cpCol("query", "text", false),
		cpCol("saved_query_name", "text", false),
		cpCol("variables_json", "json", false),
		cpCol("condition_js", "text", false),
		cpCol("delivery_json", "json", false),
		cpCol("enrich_json", "json", false),
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
	case watchFlowPreviewRootTable:
		return nil, fmt.Errorf("gj_watch_flow_preview is mutation-only")
	default:
		return nil, fmt.Errorf("unsupported GraphJin watch root: %s", root.Table)
	}
	if err != nil {
		return nil, err
	}
	return applyManagedQuery(rows, root), nil
}

func (h watchControlPlane) ExecuteManagedMutation(ctx context.Context, req core.ManagedMutationRequest) (json.RawMessage, error) {
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
	case watchFlowPreviewRootTable:
		if root.Operation != "insert" {
			return nil, fmt.Errorf("gj_watch_flow_preview supports insert mutations")
		}
		return h.previewWatchFlow(ctx, root)
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
	rows, err := s.internalStoreRows(ctx, "watches", args, watchStoreFields, vars)
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	for _, row := range rows {
		if !allOwners && !admin && stringMapValue(row, "owner_id") != ownerID {
			continue
		}
		out = append(out, watchStoreRow(row, admin || allOwners))
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
	rows, err := s.internalStoreRows(ctx, "watch_events", args, watchEventStoreFields, vars)
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
	name := strings.TrimSpace(stringInput(root.Input, "name", ""))
	if name == "" {
		return nil, fmt.Errorf("gj_watch mutation requires name")
	}
	query := strings.TrimSpace(stringInput(root.Input, "query", ""))
	savedQuery := strings.TrimSpace(stringInput(root.Input, "saved_query_name", ""))
	variablesJSON := jsonStringInput(root.Input, "variables_json")
	evidence, err := h.validateWatchDefinition(ctx, query, savedQuery, variablesJSON)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	admin := s.identityRoleIsAdmin(ctx)
	id := watchID(ownerID, name)
	if admin {
		id = stringInput(root.Input, "id", id)
	}
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
	accountID, _ := identityVarString(ctx, "account_id")
	ownerRole := runtimeRoleClass(ctx)
	description := stringInput(root.Input, "description", "")
	conditionJS := stringInput(root.Input, "condition_js", "")
	deliveryJSON := jsonStringInput(root.Input, "delivery_json")
	enrichJSON := jsonStringInput(root.Input, "enrich_json")
	var enrichCfg watchEnrichmentConfig
	enrichJSON, enrichCfg, _, err = normalizeWatchEnrichmentJSON(enrichJSON)
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
	statusDefault, approvalDefault, enabledDefault := "active", "approved", true
	if existing != nil {
		statusDefault = stringMapValue(existing, "status")
		approvalDefault = stringMapValue(existing, "approval")
		enabledDefault = boolMapValue(existing, "enabled")
	}
	status := watchStatus(stringInput(root.Input, "status", statusDefault))
	approval := watchApproval(stringInput(root.Input, "approval", approvalDefault))
	enabled := boolInput(root.Input, "enabled", enabledDefault)
	createdAt := now
	lastDataHash := ""
	lastCursorJSON := ""
	lastFiredAt := ""
	lastError := ""
	failureCount := int64(0)
	definitionChanged := existing == nil
	flowChanged := enrichCfg.Kind == "flow" && existing == nil
	if existing != nil {
		createdAt = stringMapValue(existing, "created_at")
		if createdAt == "" {
			createdAt = now
		}
		definitionChanged = query != stringMapValue(existing, "query") ||
			savedQuery != stringMapValue(existing, "saved_query_name") ||
			variablesJSON != jsonMapString(existing, "variables_json")
		_, existingEnrichCfg, _, existingEnrichErr := normalizeWatchEnrichmentJSON(jsonMapString(existing, "enrich_json"))
		flowChanged = (enrichCfg.Kind == "flow") != (existingEnrichCfg.Kind == "flow") ||
			(enrichCfg.Kind == "flow" && (existingEnrichErr != nil || enrichCfg.FlowHash != existingEnrichCfg.FlowHash))
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
	if flowChanged {
		delete(evidenceMap, "flow_preview")
		approval = "pending"
		status = "paused"
		enabled = false
	}
	if enrichCfg.Kind == "flow" && approval == "approved" && !watchFlowPreviewEvidenceMatches(evidenceMap, enrichCfg.FlowHash) {
		return nil, fmt.Errorf("gj_watch flow approval requires a successful preview for flow_hash %s", enrichCfg.FlowHash)
	}
	evidenceJSON := mustMarshalString(evidenceMap)
	input := map[string]any{
		"id": id, "name": name, "description": description, "query": query, "saved_query_name": savedQuery,
		"variables_json": nullableJSONString(variablesJSON), "condition_js": conditionJS,
		"delivery_json": nullableJSONString(deliveryJSON), "enrich_json": nullableJSONString(enrichJSON), "evidence_json": nullableJSONString(evidenceJSON),
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
	if len(rows) != 0 {
		return watchStoreRow(rows[0], admin), nil
	}
	return watchStoreRow(input, admin), nil
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
	seen := boolInput(root.Input, "seen", true)
	now := time.Now().UTC().Format(time.RFC3339)
	admin := s.identityRoleIsAdmin(ctx)
	existing, err := s.internalWatchEventStoreRow(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing == nil || (!admin && stringMapValue(existing, "owner_id") != ownerID) {
		return nil, fmt.Errorf("gj_watch_event update denied")
	}
	update := map[string]any{"seen": seen, "seen_at": nullableSeenAt(seen, now), "updated_at": now}
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
	return map[string]any{"id": id, "seen": seen, "seen_at": nullableSeenAt(seen, now), "updated_at": now}, nil
}

func (h watchControlPlane) validateWatchDefinition(ctx context.Context, query, savedQuery, variablesJSON string) (map[string]any, error) {
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
			evidence["probe"] = "ok"
			evidence["cursor_variables"] = cursorVars
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
	evidence["operation"] = "subscription"
	evidence["operation_name"] = header.Name
	evidence["saved_query_name"] = savedQuery
	evidence["probe"] = "ok"
	evidence["cursor_variables"] = cursorVars
	return evidence, nil
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
	rows, err := s.internalStoreRows(ctx, "watch_events",
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
	rows, err := h.service.internalStoreRows(ctx, "watches", `where: { owner_id: { eq: $owner_id } }`, `id`, map[string]any{"owner_id": ownerID})
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
description TEXT NOT NULL DEFAULT '',
query TEXT NOT NULL DEFAULT '',
saved_query_name TEXT NOT NULL DEFAULT '',
variables_json JSONB,
condition_js TEXT NOT NULL DEFAULT '',
delivery_json JSONB,
enrich_json JSONB,
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
description LONGTEXT NOT NULL,
query LONGTEXT NOT NULL,
saved_query_name VARCHAR(255) NOT NULL DEFAULT '',
variables_json LONGTEXT,
condition_js LONGTEXT NOT NULL,
delivery_json LONGTEXT,
enrich_json LONGTEXT,
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
description TEXT NOT NULL DEFAULT '',
query TEXT NOT NULL DEFAULT '',
saved_query_name TEXT NOT NULL DEFAULT '',
variables_json TEXT,
condition_js TEXT NOT NULL DEFAULT '',
delivery_json TEXT,
enrich_json TEXT,
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
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS last_cursor_json JSONB`, watches),
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS lifecycle TEXT NOT NULL DEFAULT 'durable'`, watches),
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS lease_expires_at TIMESTAMPTZ`, watches),
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS lease_owner_id TEXT NOT NULL DEFAULT ''`, watches),
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS data_truncated BOOLEAN NOT NULL DEFAULT FALSE`, events),
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
	events := artifactTableName(dbType, schema, "watch_events")
	dataTruncatedDef := "INTEGER NOT NULL DEFAULT 0"
	if dbType == "mysql" || dbType == "mariadb" {
		dataTruncatedDef = "BOOLEAN NOT NULL DEFAULT FALSE"
	}
	return ensureSQLColumn(ctx, db, dbType, events, "data_truncated", dataTruncatedDef)
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
	return map[string]any{
		"id":               stringMapValue(row, "id"),
		"name":             stringMapValue(row, "name"),
		"description":      stringMapValue(row, "description"),
		"query":            stringMapValue(row, "query"),
		"saved_query_name": stringMapValue(row, "saved_query_name"),
		"variables_json":   row["variables_json"],
		"condition_js":     stringMapValue(row, "condition_js"),
		"delivery_json":    row["delivery_json"],
		"enrich_json":      row["enrich_json"],
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
