package serv

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/dosco/graphjin/core/v3"
)

const watchRunnerMinInterval = time.Second

type watchRuntimeDefinition struct {
	ID             string
	Name           string
	Query          string
	SavedQueryName string
	VariablesJSON  string
	DeliveryJSON   string
	EnrichJSON     string
	AccountID      string
	OwnerID        string
	OwnerRole      string
	LastDataHash   string
	key            string
}

type activeWatchRuntime struct {
	def    watchRuntimeDefinition
	cancel context.CancelFunc
}

func (s *graphjinService) startWatchRunner(parent context.Context) {
	if s == nil || s.conf == nil || !s.watchesEnabled() || s.gj == nil {
		return
	}
	cfg := s.conf.Core.EffectiveWatchesConfig()
	if !strings.EqualFold(strings.TrimSpace(cfg.Runner), "all") {
		return
	}
	if _, _, _, _, ok := s.watchDB(); !ok {
		return
	}
	ctx := parent
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)
	s.addCloseFn(cancel)
	go s.watchRunnerLoop(ctx)
	go s.watchDeliveryLoop(ctx)
}

func (s *graphjinService) watchRunnerLoop(ctx context.Context) {
	interval := time.Duration(s.conf.Core.EffectiveArtifactsConfig().PollSeconds) * time.Second
	if interval < watchRunnerMinInterval {
		interval = watchRunnerMinInterval
	}
	active := make(map[string]activeWatchRuntime)
	defer func() {
		for _, item := range active {
			item.cancel()
		}
	}()
	reconcile := func() {
		if err := s.reconcileWatchRunner(ctx, active); err != nil {
			s.recordWatchRunnerError("reconcile watches", err, nil)
		}
	}
	reconcile()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var lastRevision int64
	if rev, err := s.artifactRevision(ctx, "watches"); err == nil {
		lastRevision = rev
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rev, err := s.artifactRevision(ctx, "watches")
			if err != nil {
				s.recordWatchRunnerError("read watch revision", err, nil)
				continue
			}
			if rev == lastRevision {
				continue
			}
			lastRevision = rev
			reconcile()
		}
	}
}

func (s *graphjinService) reconcileWatchRunner(ctx context.Context, active map[string]activeWatchRuntime) error {
	defs, err := s.loadRunnableWatches(ctx)
	if err != nil {
		return err
	}
	desired := make(map[string]watchRuntimeDefinition, len(defs))
	for _, def := range defs {
		desired[def.ID] = def
		if current, ok := active[def.ID]; ok && current.def.key == def.key {
			continue
		}
		if current, ok := active[def.ID]; ok {
			current.cancel()
			delete(active, def.ID)
		}
		watchCtx, cancel := context.WithCancel(ctx)
		active[def.ID] = activeWatchRuntime{def: def, cancel: cancel}
		go s.runWatchSubscription(watchCtx, def)
	}
	for id, current := range active {
		if _, ok := desired[id]; !ok {
			current.cancel()
			delete(active, id)
		}
	}
	return nil
}

func (s *graphjinService) loadRunnableWatches(ctx context.Context) ([]watchRuntimeDefinition, error) {
	if _, _, _, _, ok := s.watchDB(); !ok {
		return nil, nil
	}
	rows, err := s.internalStoreRows(ctx, "watches", "", watchStoreFields, nil)
	if err != nil {
		return nil, err
	}
	var defs []watchRuntimeDefinition
	for _, row := range rows {
		if !boolMapValue(row, "enabled") || watchStatus(stringMapValue(row, "status")) != "active" || watchApproval(stringMapValue(row, "approval")) != "approved" {
			continue
		}
		def := watchRuntimeDefinition{
			ID:             stringMapValue(row, "id"),
			Name:           stringMapValue(row, "name"),
			Query:          stringMapValue(row, "query"),
			SavedQueryName: stringMapValue(row, "saved_query_name"),
			VariablesJSON:  jsonMapString(row, "variables_json"),
			DeliveryJSON:   jsonMapString(row, "delivery_json"),
			EnrichJSON:     jsonMapString(row, "enrich_json"),
			AccountID:      stringMapValue(row, "account_id"),
			OwnerID:        stringMapValue(row, "owner_id"),
			OwnerRole:      stringMapValue(row, "owner_role"),
			LastDataHash:   stringMapValue(row, "last_data_hash"),
		}
		def.OwnerRole = s.trustedWatchRunnerRole(def.OwnerRole)
		def.key = def.runtimeKey()
		defs = append(defs, def)
	}
	return defs, nil
}

func (d watchRuntimeDefinition) runtimeKey() string {
	return hashString(strings.Join([]string{
		d.ID,
		d.Query,
		d.SavedQueryName,
		d.VariablesJSON,
		d.DeliveryJSON,
		d.EnrichJSON,
		d.AccountID,
		d.OwnerID,
		d.OwnerRole,
	}, "\x00"))
}

func (s *graphjinService) runWatchSubscription(ctx context.Context, def watchRuntimeDefinition) {
	if strings.TrimSpace(def.OwnerID) == "" {
		s.recordWatchRunnerError("subscribe watch", fmt.Errorf("watch owner_id is missing"), map[string]any{"watch_id": def.ID})
		return
	}
	ownerCtx := s.watchOwnerContext(ctx, def)
	var vars json.RawMessage
	if strings.TrimSpace(def.VariablesJSON) != "" {
		if !json.Valid([]byte(def.VariablesJSON)) {
			s.updateWatchRunnerError(ctx, def.ID, fmt.Errorf("watch variables_json is invalid"))
			return
		}
		vars = json.RawMessage(def.VariablesJSON)
	}
	var member *core.Member
	var err error
	if strings.TrimSpace(def.SavedQueryName) != "" {
		member, err = s.gj.SubscribeByName(ownerCtx, def.SavedQueryName, vars, nil)
	} else {
		member, err = s.gj.Subscribe(ownerCtx, def.Query, vars, nil)
	}
	if err != nil {
		s.updateWatchRunnerError(ctx, def.ID, err)
		return
	}
	defer member.Unsubscribe()
	current := def
	for {
		select {
		case <-ctx.Done():
			return
		case res, ok := <-member.Result:
			if !ok {
				return
			}
			if res == nil {
				continue
			}
			dataHash, inserted, err := s.persistWatchResult(ctx, &current, res)
			if err != nil {
				s.updateWatchRunnerError(ctx, current.ID, err)
				continue
			}
			if dataHash != "" {
				current.LastDataHash = dataHash
			}
			if inserted {
				s.recordRuntimeEvent(ctx, runtimeEvent{
					Phase:      "watch",
					Status:     runtimeStatusReady,
					Severity:   "info",
					Summary:    "Watch produced a new inbox event.",
					NextAction: "Query gj_watch_event for unseen events and mark them seen after review.",
					ErrorCode:  "watch_event_created",
					Details:    map[string]any{"watch_id": current.ID, "name": current.Name},
				})
			}
		}
	}
}

func (s *graphjinService) watchOwnerContext(parent context.Context, def watchRuntimeDefinition) context.Context {
	ctx := parent
	if ctx == nil {
		ctx = context.Background()
	}
	role := s.trustedWatchRunnerRole(def.OwnerRole)
	ctx = context.WithValue(ctx, core.UserIDKey, def.OwnerID)
	ctx = context.WithValue(ctx, core.UserRoleKey, role)
	ctx = context.WithValue(ctx, core.IdentityRolesKey, []string{role})
	vars := map[string]interface{}{
		"user_id":  def.OwnerID,
		"user_ref": safeArtifactIdentity(def.OwnerID, false),
	}
	if strings.TrimSpace(def.AccountID) != "" {
		vars["account_id"] = def.AccountID
		vars["account_ref"] = safeArtifactIdentity(def.AccountID, false)
	}
	return context.WithValue(ctx, core.IdentityVarsKey, vars)
}

func (s *graphjinService) persistWatchResult(ctx context.Context, def *watchRuntimeDefinition, res *core.Result) (string, bool, error) {
	if def == nil || res == nil || len(res.Data) == 0 {
		return "", false, nil
	}
	data := string(res.Data)
	dataHash := hashString(data)
	if dataHash == def.LastDataHash {
		return dataHash, false, nil
	}
	dataJSON, dataTruncated := s.watchSnapshotJSON(data)
	if _, _, _, _, ok := s.watchDB(); !ok {
		return "", false, fmt.Errorf("watch store database is not configured")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	eventID := watchEventID(def.ID, dataHash)
	evidenceJSON := mustMarshalString(map[string]any{
		"watch_id":    def.ID,
		"watch_name":  def.Name,
		"query_name":  res.QueryName(),
		"role":        res.Role(),
		"observed_at": now,
	})
	existing, err := s.internalWatchEventStoreRow(ctx, eventID)
	if err != nil {
		return "", false, err
	}
	inserted := existing == nil
	enrichCfg, enrichEnabled := parseWatchEnrichmentConfig(def.EnrichJSON)
	deliveryStatus := "pending"
	if enrichEnabled {
		deliveryStatus = "enriching"
	}
	if inserted {
		input := map[string]any{
			"id": eventID, "watch_id": def.ID, "data_hash": dataHash, "data_json": nullableJSONString(dataJSON),
			"data_truncated": dataTruncated, "evidence_json": nullableJSONString(evidenceJSON),
			"delivery_status": deliveryStatus, "delivery_attempts": 0, "delivery_json": nullableJSONString(def.DeliveryJSON),
			"receipt_json": nil, "enrichment_json": nil, "seen": false, "account_id": def.AccountID, "owner_id": def.OwnerID,
			"created_at": now, "updated_at": now,
		}
		if _, err := s.internalStoreMutationRows(ctx, "watch_events", `insert: $input`, watchEventStoreFields, map[string]any{"input": input}); err != nil {
			return "", false, err
		}
		if enrichEnabled {
			if err := s.enrichWatchEvent(ctx, def, eventID, dataJSON, evidenceJSON, enrichCfg); err != nil {
				s.recordWatchRunnerError("enrich watch event", err, map[string]any{"watch_id": def.ID, "event_id": eventID})
			}
		}
	}
	if _, err := s.internalStoreMutationRows(ctx, "watches", `where: { id: { eq: $id } }, update: $input`, watchStoreFields, map[string]any{
		"id":    def.ID,
		"input": map[string]any{"last_data_hash": dataHash, "last_fired_at": now, "last_error": "", "failure_count": 0, "updated_at": now},
	}); err != nil {
		return "", inserted, err
	}
	if err := s.pruneWatchEvents(ctx, def.ID); err != nil {
		return "", inserted, err
	}
	if inserted {
		if err := s.bumpArtifactRevision(ctx, "watch_events"); err != nil {
			return "", inserted, err
		}
	}
	if err := s.bumpArtifactRevision(ctx, "watches"); err != nil {
		return "", inserted, err
	}
	s.markWatchChanged("watch runner event")
	return dataHash, inserted, nil
}

func (s *graphjinService) watchSnapshotJSON(data string) (string, bool) {
	max := 0
	if s != nil && s.conf != nil {
		max = s.conf.Core.EffectiveWatchesConfig().SnapshotMaxBytes
	}
	return capWatchSnapshotJSON(data, max)
}

func capWatchSnapshotJSON(data string, maxBytes int) (string, bool) {
	if maxBytes <= 0 || len(data) <= maxBytes {
		return data, false
	}
	if maxBytes <= 0 {
		return data, false
	}
	if maxBytes == 1 {
		return "0", true
	}
	if maxBytes < len("null") {
		return "{}", true
	}
	payload := func(prefix string) string {
		out, err := json.Marshal(map[string]any{
			"truncated": true,
			"bytes":     len(data),
			"prefix":    prefix,
		})
		if err != nil {
			return "null"
		}
		return string(out)
	}
	lo, hi := 0, len(data)
	best := "null"
	for lo <= hi {
		mid := (lo + hi) / 2
		candidate := payload(data[:mid])
		if len(candidate) <= maxBytes {
			best = candidate
			lo = mid + 1
			continue
		}
		hi = mid - 1
	}
	if len(best) <= maxBytes {
		return best, true
	}
	if maxBytes >= len("null") {
		return "null", true
	}
	return "{}", true
}

func (s *graphjinService) updateWatchRunnerError(ctx context.Context, id string, err error) {
	if err == nil {
		return
	}
	if _, _, _, _, ok := s.watchDB(); !ok {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	row, loadErr := s.internalWatchStoreRow(ctx, id)
	if loadErr != nil {
		s.recordWatchRunnerError("update watch error", loadErr, map[string]any{"watch_id": id, "source_error": err.Error()})
		return
	}
	failureCount := int64(1)
	if row != nil {
		failureCount = int64MapValue(row, "failure_count") + 1
	}
	if _, execErr := s.internalStoreMutationRows(ctx, "watches", `where: { id: { eq: $id } }, update: $input`, watchStoreFields, map[string]any{
		"id":    id,
		"input": map[string]any{"status": "error", "last_error": err.Error(), "failure_count": failureCount, "updated_at": now},
	}); execErr != nil {
		s.recordWatchRunnerError("update watch error", execErr, map[string]any{"watch_id": id, "source_error": err.Error()})
		return
	}
	_ = s.bumpArtifactRevision(ctx, "watches")
	s.markWatchChanged("watch runner error")
	s.recordWatchRunnerError("subscribe or process watch", err, map[string]any{"watch_id": id})
}

func (s *graphjinService) pruneWatchEvents(ctx context.Context, watchID string) error {
	cfg := s.conf.Core.EffectiveWatchesConfig()
	rows, err := s.internalStoreRows(ctx, "watch_events", `where: { watch_id: { eq: $watch_id } }`, `id watch_id created_at`, map[string]any{"watch_id": watchID})
	if err != nil {
		return err
	}
	deleteIDs := map[string]struct{}{}
	if cfg.EventRetentionHours > 0 {
		cutoff := time.Now().UTC().Add(-time.Duration(cfg.EventRetentionHours) * time.Hour)
		for _, row := range rows {
			if ts, ok := parseWatchTime(stringMapValue(row, "created_at")); ok && ts.Before(cutoff) {
				deleteIDs[stringMapValue(row, "id")] = struct{}{}
			}
		}
	}
	if cfg.MaxEventsPerWatch > 0 {
		sort.SliceStable(rows, func(i, j int) bool {
			return stringMapValue(rows[i], "created_at") > stringMapValue(rows[j], "created_at")
		})
		kept := 0
		for _, row := range rows {
			id := stringMapValue(row, "id")
			if _, deleting := deleteIDs[id]; deleting {
				continue
			}
			if kept >= cfg.MaxEventsPerWatch {
				deleteIDs[id] = struct{}{}
				continue
			}
			kept++
		}
	}
	for id := range deleteIDs {
		if id == "" {
			continue
		}
		if _, err := s.internalStoreMutationRows(ctx, "watch_events", `where: { id: { eq: $id } }, delete: true`, `id`, map[string]any{"id": id}); err != nil {
			return err
		}
	}
	return nil
}

func nullableJSONString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func (s *graphjinService) recordWatchRunnerError(action string, err error, details map[string]any) {
	if err == nil {
		return
	}
	if s.log != nil {
		s.log.Warnf("watch runner: %s failed: %v", action, err)
	}
	if details == nil {
		details = map[string]any{}
	}
	details["action"] = action
	details["error"] = err.Error()
	s.recordRuntimeEvent(context.Background(), runtimeEvent{
		Phase:      "watch",
		Status:     runtimeStatusDegraded,
		Severity:   "warn",
		Summary:    "Watch runner failed.",
		NextAction: "Inspect gj_watch last_error, verify stored identity/role still has access, then update or pause the watch.",
		ErrorCode:  "watch_runner_failed",
		Details:    details,
	})
}

func (s *graphjinService) appendWatchNotices(ctx context.Context, resp *gjagent.Response) {
	if resp == nil || s == nil || !s.watchesEnabled() {
		return
	}
	count, since, err := s.unseenWatchEventSummary(ctx)
	if err != nil || count == 0 {
		return
	}
	resp.Notices = append(resp.Notices, gjagent.ResponseNotice{
		Kind:    "watch_events_unseen",
		Message: "You have unseen watch events. Query gj_watch_event and mark reviewed events seen with gj_watch_event(update).",
		Count:   count,
		Since:   since,
	})
}

func (s *graphjinService) unseenWatchEventSummary(ctx context.Context) (int, string, error) {
	if _, _, _, _, ok := s.watchDB(); !ok {
		return 0, "", nil
	}
	ownerID, ok := artifactUserID(ctx)
	if !ok {
		return 0, "", nil
	}
	rows, err := s.internalStoreRows(ctx, "watch_events", `where: { owner_id: { eq: $owner_id } }`, `id seen created_at owner_id`, map[string]any{"owner_id": ownerID})
	if err != nil {
		return 0, "", err
	}
	count := 0
	since := ""
	for _, row := range rows {
		if boolMapValue(row, "seen") {
			continue
		}
		count++
		createdAt := stringMapValue(row, "created_at")
		if since == "" || (createdAt != "" && createdAt < since) {
			since = createdAt
		}
	}
	return count, since, nil
}

func trimWatchEventProjectionRows(rows []map[string]any, cfg core.WatchesConfig) []map[string]any {
	if len(rows) == 0 {
		return rows
	}
	cutoff := time.Time{}
	if cfg.EventRetentionHours > 0 {
		cutoff = time.Now().UTC().Add(-time.Duration(cfg.EventRetentionHours) * time.Hour)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return fmt.Sprint(rows[i]["created_at"]) > fmt.Sprint(rows[j]["created_at"])
	})
	perWatch := map[string]int{}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if !cutoff.IsZero() {
			if ts, ok := parseWatchTime(fmt.Sprint(row["created_at"])); ok && ts.Before(cutoff) {
				continue
			}
		}
		watchID := fmt.Sprint(row["watch_id"])
		if cfg.MaxEventsPerWatch > 0 && perWatch[watchID] >= cfg.MaxEventsPerWatch {
			continue
		}
		perWatch[watchID]++
		out = append(out, row)
	}
	return out
}

func parseWatchTime(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, value); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
