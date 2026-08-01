package serv

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	ax "github.com/ax-llm/ax/packages/go"
	gjagent "github.com/dosco/graphjin/agent/v3"
)

const (
	watchDeliveryBatchSize       = 20
	watchDeliveryMaxAttempts     = 3
	watchDeliveryHTTPTimeout     = 10 * time.Second
	watchDeliveryMaxResponseBody = 4 * 1024
	watchEventSweepInterval      = time.Hour
	watchDigestCheckInterval     = 30 * time.Second
	watchDigestDefaultWindow     = time.Hour
	watchDigestMinWindow         = time.Minute
	watchDigestMaxWindow         = 7 * 24 * time.Hour
)

type watchDeliveryConfig struct {
	Kind     string
	Webhook  watchWebhookConfig
	Workflow watchWorkflowConfig
	Digest   watchDigestConfig
}

type watchDigestConfig struct {
	Enabled    bool
	Window     time.Duration
	WindowText string
}

type watchWebhookConfig struct {
	URL       string
	SecretEnv string
	Headers   map[string]string
}

type watchWorkflowConfig struct {
	Name string
}

type watchEnrichmentConfig struct {
	Enabled       bool
	Kind          string
	Flow          string
	CanonicalFlow string
	FlowHash      string
	Instruction   string
	MaxSteps      int
}

type watchDeliveryEvent struct {
	ID               string
	WatchID          string
	DataHash         string
	DataJSON         string
	EvidenceJSON     string
	EnrichmentJSON   string
	DeliveryJSON     string
	DeliveryAttempts int64
	AccountID        string
	OwnerID          string
	CreatedAt        string
}

func (s *graphjinService) watchDeliveryLoop(ctx context.Context) {
	s.watchDeliveryLoopWithSweepInterval(ctx, watchEventSweepInterval)
}

func (s *graphjinService) watchDeliveryLoopWithSweepInterval(
	ctx context.Context,
	sweepInterval time.Duration,
) {
	s.watchDeliveryLoopWithIntervals(ctx, sweepInterval, watchDigestCheckInterval)
}

func (s *graphjinService) watchDeliveryLoopWithIntervals(
	ctx context.Context,
	sweepInterval time.Duration,
	digestInterval time.Duration,
) {
	interval := time.Duration(s.conf.Core.EffectiveArtifactsConfig().PollSeconds) * time.Second
	if interval < watchRunnerMinInterval {
		interval = watchRunnerMinInterval
	}
	if sweepInterval <= 0 {
		sweepInterval = watchEventSweepInterval
	}
	if digestInterval <= 0 {
		digestInterval = watchDigestCheckInterval
	}
	processPending := func() {
		if err := s.processPendingWatchDeliveries(ctx); err != nil {
			s.recordWatchRunnerError("deliver watch events", err, nil)
		}
	}
	sweepAndRecover := func() {
		if _, err := s.sweepWatchEvents(ctx); err != nil {
			s.recordWatchRunnerError("sweep watch events", err, nil)
		}
		if err := s.processPendingWatchDeliveries(ctx); err != nil {
			s.recordWatchRunnerError("recover pending watch deliveries", err, nil)
		}
	}
	if _, err := s.sweepWatchEvents(ctx); err != nil {
		s.recordWatchRunnerError("sweep watch events", err, nil)
	}
	revisionChanges := s.revisionSignals(ctx, "watch_events", true, interval)
	sweepTicker := time.NewTicker(sweepInterval)
	defer sweepTicker.Stop()
	digestTicker := time.NewTicker(digestInterval)
	defer digestTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-revisionChanges:
			if !ok {
				return
			}
			processPending()
		case <-sweepTicker.C:
			sweepAndRecover()
		case now := <-digestTicker.C:
			if _, err := s.sweepWatchDigests(ctx, now.UTC()); err != nil {
				s.recordWatchRunnerError("drain watch digests", err, nil)
			}
			s.notifyLapsedWatchSnoozes(ctx, now.UTC())
		}
	}
}

func (s *graphjinService) processPendingWatchDeliveries(ctx context.Context) error {
	args := fmt.Sprintf(
		`where: { delivery_status: { eq: "pending" } }, order_by: { created_at: asc }, limit: %d`,
		watchDeliveryBatchSize,
	)
	rows, err := s.internalStoreRows(ctx, "watch_events", args, watchEventStoreFields, nil)
	if err != nil {
		return err
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return stringMapValue(rows[i], "created_at") < stringMapValue(rows[j], "created_at")
	})
	var firstErr error
	processed := 0
	for _, row := range rows {
		if processed >= watchDeliveryBatchSize {
			break
		}
		processed++
		if err := s.processWatchDeliveryRow(ctx, row); err != nil {
			s.recordWatchRunnerError("deliver watch event", err, map[string]any{"event_id": stringMapValue(row, "id")})
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func (s *graphjinService) sweepWatchDigests(ctx context.Context, now time.Time) (int, error) {
	if _, _, _, _, ok := s.watchDB(); !ok {
		return 0, nil
	}
	rows, err := s.internalStoreAllRows(
		ctx,
		"watch_events",
		`where: { delivery_status: { eq: "digest_queued" } }`,
		watchEventStoreFields,
		nil,
	)
	if err != nil {
		return 0, err
	}
	groups := map[string][]map[string]any{}
	for _, row := range rows {
		watchID := stringMapValue(row, "watch_id")
		if watchID != "" {
			groups[watchID] = append(groups[watchID], row)
		}
	}
	watchIDs := make([]string, 0, len(groups))
	for watchID := range groups {
		watchIDs = append(watchIDs, watchID)
	}
	sort.Strings(watchIDs)
	flushed := 0
	for _, watchID := range watchIDs {
		group := groups[watchID]
		sort.SliceStable(group, func(i, j int) bool {
			return stringMapValue(group[i], "created_at") < stringMapValue(group[j], "created_at")
		})
		watchRow, err := s.internalWatchStoreRow(ctx, watchID)
		if err != nil {
			return flushed, err
		}
		if watchRow == nil {
			continue
		}
		deliveryJSON := jsonMapString(watchRow, "delivery_json")
		deliveryCfg, _, err := parseWatchDeliveryConfig(deliveryJSON)
		if err != nil {
			return flushed, err
		}
		window := watchDigestDefaultWindow
		windowText := window.String()
		if deliveryCfg.Digest.Enabled {
			window = deliveryCfg.Digest.Window
			windowText = deliveryCfg.Digest.WindowText
		}
		oldest, ok := parseWatchTime(stringMapValue(group[0], "created_at"))
		if !ok || oldest.Add(window).After(now.UTC()) {
			continue
		}
		if err := s.flushWatchDigest(ctx, watchRow, group, windowText, now.UTC()); err != nil {
			return flushed, err
		}
		flushed++
	}
	return flushed, nil
}

func (s *graphjinService) flushWatchDigest(
	ctx context.Context,
	watchRow map[string]any,
	members []map[string]any,
	windowText string,
	now time.Time,
) error {
	if len(members) == 0 || watchRow == nil {
		return nil
	}
	def := watchRuntimeDefinition{
		ID:             stringMapValue(watchRow, "id"),
		Name:           stringMapValue(watchRow, "name"),
		Query:          stringMapValue(watchRow, "query"),
		SavedQueryName: stringMapValue(watchRow, "saved_query_name"),
		VariablesJSON:  jsonMapString(watchRow, "variables_json"),
		DeliveryJSON:   jsonMapString(watchRow, "delivery_json"),
		EnrichJSON:     jsonMapString(watchRow, "enrich_json"),
		AbsenceJSON:    jsonMapString(watchRow, "absence_json"),
		Lifecycle:      watchLifecycle(stringMapValue(watchRow, "lifecycle")),
		AccountID:      stringMapValue(watchRow, "account_id"),
		OwnerID:        stringMapValue(watchRow, "owner_id"),
		OwnerRole:      s.trustedWatchRunnerRole(stringMapValue(watchRow, "owner_role")),
	}
	if def.ID == "" {
		return nil
	}
	type digestEntry struct {
		ID        string `json:"id"`
		CreatedAt string `json:"created_at"`
		Severity  string `json:"severity"`
		Summary   string `json:"summary"`
	}
	eventIDs := make([]string, 0, len(members))
	entries := make([]digestEntry, 0, min(len(members), 50))
	maxSeverity := "info"
	from, to := "", ""
	for _, member := range members {
		id := stringMapValue(member, "id")
		if id == "" {
			continue
		}
		eventIDs = append(eventIDs, id)
		createdAt := stringMapValue(member, "created_at")
		if from == "" || (createdAt != "" && createdAt < from) {
			from = createdAt
		}
		if to == "" || createdAt > to {
			to = createdAt
		}
		enrichment := mapFromAny(parseJSONValue(jsonMapString(member, "enrichment_json")))
		severity := normalizeWatchDigestSeverity(stringFromAny(enrichment["severity"]))
		if watchDigestSeverityRank(severity) > watchDigestSeverityRank(maxSeverity) {
			maxSeverity = severity
		}
		if len(entries) < 50 {
			entries = append(entries, digestEntry{
				ID: id, CreatedAt: createdAt, Severity: severity,
				Summary: strings.TrimSpace(stringFromAny(enrichment["summary"])),
			})
		}
	}
	if len(eventIDs) == 0 {
		return nil
	}
	sort.Strings(eventIDs)
	data := map[string]any{
		"kind":         "digest",
		"window":       windowText,
		"count":        len(eventIDs),
		"max_severity": maxSeverity,
		"from":         from,
		"to":           to,
		"events":       entries,
		"event_ids":    eventIDs,
	}
	dataBytes, err := json.Marshal(data)
	if err != nil {
		return err
	}
	dataJSON, dataTruncated := s.watchSnapshotJSON(string(dataBytes))
	cacheHash := hashString("digest:" + strings.Join(eventIDs, ","))
	eventID := watchEventID(def.ID, cacheHash)
	nowText := now.Format(time.RFC3339Nano)
	proposal, err := s.watchActionProposal(
		s.watchOwnerContext(ctx, def),
		def.Query,
		def.SavedQueryName,
		def.VariablesJSON,
		def.EnrichJSON,
		def.DeliveryJSON,
		def.AbsenceJSON,
	)
	if err != nil {
		return err
	}
	watchEvidence := mapFromAny(parseJSONValue(jsonMapString(watchRow, "evidence_json")))
	actionApproved := watchActionApproval(watchEvidence, proposal) == watchReviewApproved
	eventEvidence := map[string]any{
		"watch_id": def.ID, "watch_name": def.Name, "observed_at": nowText,
		"digest": map[string]any{"window": windowText, "event_ids": eventIDs},
	}
	if proposal.Required {
		eventEvidence["action_hash"] = proposal.Hash
		eventEvidence["delivery_kind"] = proposal.Kind
		if proposal.WorkflowSourceHash != "" {
			eventEvidence["workflow_source_hash"] = proposal.WorkflowSourceHash
		}
	}
	status := "pending"
	if proposal.Required && !actionApproved {
		status = "approval_required"
	}
	enrichment := map[string]any{
		"kind": "digest", "verdict": "notify", "severity": maxSeverity,
		"summary":      fmt.Sprintf("%d events digested over %s.", len(eventIDs), windowText),
		"generated_at": nowText,
	}
	inserted, err := s.insertSyntheticWatchEvent(ctx, &def, map[string]any{
		"id": eventID, "watch_id": def.ID, "data_hash": cacheHash,
		"data_json": nullableJSONString(dataJSON), "data_truncated": dataTruncated,
		"evidence_json":   nullableJSONString(mustMarshalString(eventEvidence)),
		"delivery_status": status, "delivery_attempts": 0,
		"delivery_json": nullableJSONString(def.DeliveryJSON),
		"receipt_json":  nil, "enrichment_json": nullableJSONString(mustMarshalString(enrichment)),
		"seen": false, "snoozed_until": nil, "account_id": def.AccountID, "owner_id": def.OwnerID,
		"created_at": nowText, "updated_at": nowText,
	})
	if err != nil {
		return err
	}
	updatedMembers := 0
	for _, member := range members {
		id := stringMapValue(member, "id")
		if id == "" {
			continue
		}
		rows, err := s.internalStoreMutationRows(ctx, "watch_events",
			`where: { id: { eq: $id }, delivery_status: { eq: "digest_queued" } }, update: $input`,
			watchEventStoreFields,
			map[string]any{
				"id": id,
				"input": map[string]any{
					"delivery_status": "digested",
					"receipt_json": nullableJSONString(mustMarshalString(map[string]any{
						"digest_event_id": eventID,
					})),
					"updated_at": nowText,
				},
			})
		if err != nil {
			return err
		}
		updatedMembers += len(rows)
	}
	if !inserted && updatedMembers == 0 {
		return nil
	}
	if inserted {
		if err := s.pruneWatchEvents(ctx, &def); err != nil {
			return err
		}
	}
	if err := s.bumpArtifactRevision(ctx, "watch_events"); err != nil {
		return err
	}
	s.markWatchChanged("watch digest")
	s.notifyWatchEventsResourceScope(watchEventScope{
		OwnerID: def.OwnerID, AccountID: def.AccountID, WatchID: def.ID,
	}, true)
	return nil
}

func normalizeWatchDigestSeverity(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "critical":
		return "critical"
	case "warn", "warning":
		return "warn"
	default:
		return "info"
	}
}

func watchDigestSeverityRank(value string) int {
	switch normalizeWatchDigestSeverity(value) {
	case "critical":
		return 3
	case "warn":
		return 2
	default:
		return 1
	}
}

func (s *graphjinService) notifyLapsedWatchSnoozes(ctx context.Context, now time.Time) {
	if s == nil {
		return
	}
	s.watchSnoozeMu.Lock()
	since := s.watchSnoozeLastSweep
	if since.IsZero() || since.After(now) {
		since = now.Add(-watchDigestCheckInterval)
	}
	s.watchSnoozeLastSweep = now
	s.watchSnoozeMu.Unlock()

	rows, err := s.internalStoreAllRows(
		ctx,
		"watch_events",
		"",
		`id watch_id seen snoozed_until owner_id account_id`,
		nil,
	)
	if err != nil {
		s.recordWatchRunnerError("scan lapsed watch snoozes", err, nil)
		return
	}
	scopes := map[string]watchEventScope{}
	for _, row := range rows {
		if boolMapValue(row, "seen") {
			continue
		}
		snoozedUntil, ok := parseWatchTime(stringMapValue(row, "snoozed_until"))
		if !ok || snoozedUntil.After(now) || !snoozedUntil.After(since) {
			continue
		}
		scope := watchEventScope{
			OwnerID: stringMapValue(row, "owner_id"), AccountID: stringMapValue(row, "account_id"),
			WatchID: stringMapValue(row, "watch_id"),
		}
		if scope.OwnerID == "" {
			continue
		}
		scopes[scope.OwnerID+"\x00"+scope.AccountID+"\x00"+scope.WatchID] = scope
	}
	for _, scope := range scopes {
		s.notifyWatchEventsResourceScope(scope, false)
	}
}

func (s *graphjinService) sweepWatchEvents(ctx context.Context) (int, error) {
	if _, _, _, _, ok := s.watchDB(); !ok {
		return 0, nil
	}
	cfg := s.conf.Core.EffectiveWatchesConfig()
	eventRows, err := s.internalStoreAllRows(ctx, "watch_events", "", `id watch_id created_at`, nil)
	if err != nil {
		return 0, err
	}
	if len(eventRows) == 0 {
		return 0, nil
	}
	watchRows, err := s.internalStoreAllRows(ctx, "watches", "", `id`, nil)
	if err != nil {
		return 0, err
	}
	watchIDs := make(map[string]struct{}, len(watchRows))
	for _, row := range watchRows {
		if id := stringMapValue(row, "id"); id != "" {
			watchIDs[id] = struct{}{}
		}
	}
	// An empty watch scan is not sufficient evidence that every event is
	// orphaned. Keep non-expired events in that case so a paging or projection
	// regression cannot turn the maintenance sweep into a mass deletion.
	orphanCleanupSafe := len(watchRows) != 0
	var cutoff time.Time
	if cfg.EventRetentionHours > 0 {
		cutoff = time.Now().UTC().Add(-time.Duration(cfg.EventRetentionHours) * time.Hour)
	}
	deleted := 0
	for _, row := range eventRows {
		id := stringMapValue(row, "id")
		if id == "" {
			continue
		}
		watchID := stringMapValue(row, "watch_id")
		_, watchExists := watchIDs[watchID]
		expired := false
		if !cutoff.IsZero() {
			if ts, ok := parseWatchTime(stringMapValue(row, "created_at")); ok && ts.Before(cutoff) {
				expired = true
			}
		}
		if !expired && watchExists {
			continue
		}
		if !expired {
			if !orphanCleanupSafe {
				continue
			}
			// Confirm absence with an exact lookup before destructive orphan
			// cleanup. This remains safe even if a future caller accidentally
			// replaces the complete watch scan with a bounded page.
			watchRow, err := s.internalWatchStoreRow(ctx, watchID)
			if err != nil {
				return deleted, err
			}
			if watchRow != nil {
				continue
			}
		}
		n, err := s.deleteWatchEventByID(ctx, id)
		if err != nil {
			return deleted, err
		}
		deleted += n
	}
	if deleted == 0 {
		return 0, nil
	}
	if err := s.bumpArtifactRevision(ctx, "watch_events"); err != nil {
		return deleted, err
	}
	s.markWatchChanged("watch event sweep")
	return deleted, nil
}

func (s *graphjinService) processWatchDeliveryRow(ctx context.Context, row map[string]any) error {
	event := watchDeliveryEventFromRow(row)
	if event.ID == "" {
		return nil
	}
	_, ok, err := s.claimWatchDelivery(ctx, event)
	if err != nil || !ok {
		return err
	}
	watchRow, err := s.internalWatchStoreRow(ctx, event.WatchID)
	if err != nil {
		return err
	}
	if watchRow == nil {
		return s.completeWatchDelivery(ctx, event, "failed", 0, map[string]any{
			"status": "failed",
			"error":  "watch definition no longer exists",
		})
	}
	def := watchRuntimeDefinition{
		ID:             stringMapValue(watchRow, "id"),
		Name:           stringMapValue(watchRow, "name"),
		Query:          stringMapValue(watchRow, "query"),
		SavedQueryName: stringMapValue(watchRow, "saved_query_name"),
		VariablesJSON:  jsonMapString(watchRow, "variables_json"),
		DeliveryJSON:   event.DeliveryJSON,
		EnrichJSON:     jsonMapString(watchRow, "enrich_json"),
		Lifecycle:      watchLifecycle(stringMapValue(watchRow, "lifecycle")),
		LeaseExpiresAt: stringMapValue(watchRow, "lease_expires_at"),
		LeaseOwnerID:   stringMapValue(watchRow, "lease_owner_id"),
		AccountID:      stringMapValue(watchRow, "account_id"),
		OwnerID:        stringMapValue(watchRow, "owner_id"),
		OwnerRole:      s.trustedWatchRunnerRole(stringMapValue(watchRow, "owner_role")),
		LastDataHash:   stringMapValue(watchRow, "last_data_hash"),
	}
	eventDelivery, eventAction, deliveryErr := parseWatchDeliveryConfig(event.DeliveryJSON)
	if deliveryErr != nil {
		return s.completeWatchDelivery(ctx, event, "failed", 0, map[string]any{
			"status": "failed",
			"error":  deliveryErr.Error(),
		})
	}
	if eventAction && (eventDelivery.Kind == "workflow" || eventDelivery.Kind == "webhook") {
		ownerCtx := s.watchOwnerContext(ctx, def)
		proposal, proposalErr := s.watchActionProposal(
			ownerCtx,
			def.Query,
			def.SavedQueryName,
			def.VariablesJSON,
			def.EnrichJSON,
			jsonMapString(watchRow, "delivery_json"),
			jsonMapString(watchRow, "absence_json"),
		)
		watchEvidence := mapFromAny(parseJSONValue(jsonMapString(watchRow, "evidence_json")))
		eventEvidence := mapFromAny(parseJSONValue(event.EvidenceJSON))
		eventActionHash := strings.TrimSpace(stringFromAny(eventEvidence["action_hash"]))
		actionApproved := proposalErr == nil &&
			proposal.Required &&
			watchActionApproval(watchEvidence, proposal) == watchReviewApproved &&
			eventActionHash != "" &&
			eventActionHash == proposal.Hash
		if !actionApproved {
			reason := "watch action approval is missing or no longer matches this event"
			if proposalErr != nil {
				reason = proposalErr.Error()
			}
			return s.completeWatchDelivery(ctx, event, "approval_required", 0, map[string]any{
				"status":              "approval_required",
				"error":               reason,
				"event_action_hash":   eventActionHash,
				"current_action_hash": proposal.Hash,
			})
		}
		def.ActionHash = proposal.Hash
		def.ActionRequired = true
		def.ActionApproved = true
		def.WorkflowSourceHash = proposal.WorkflowSourceHash
	}
	receipt, attempts, err := s.deliverWatchEvent(ctx, def, event)
	if err != nil {
		if receipt == nil {
			receipt = map[string]any{}
		}
		receipt["status"] = "failed"
		receipt["error"] = err.Error()
		return s.completeWatchDelivery(ctx, event, "failed", attempts, receipt)
	}
	return s.completeWatchDelivery(ctx, event, "delivered", attempts, receipt)
}

func (s *graphjinService) claimWatchDelivery(ctx context.Context, event watchDeliveryEvent) (map[string]any, bool, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	claimStatus := "claimed:" + hashString(fmt.Sprintf("%s:%d", event.ID, time.Now().UnixNano()))
	if _, err := s.internalStoreMutationRows(ctx, "watch_events",
		`where: { id: { eq: $id }, delivery_status: { eq: "pending" } }, update: $input`,
		watchEventStoreFields,
		map[string]any{
			"id":    event.ID,
			"input": map[string]any{"delivery_status": claimStatus, "updated_at": now},
		}); err != nil {
		return nil, false, err
	}
	row, err := s.internalWatchEventStoreRow(ctx, event.ID)
	if err != nil || row == nil {
		return nil, false, err
	}
	if stringMapValue(row, "delivery_status") != claimStatus {
		return nil, false, nil
	}
	return row, true, nil
}

func (s *graphjinService) deliverWatchEvent(ctx context.Context, def watchRuntimeDefinition, event watchDeliveryEvent) (map[string]any, int64, error) {
	cfg, enabled, err := parseWatchDeliveryConfig(event.DeliveryJSON)
	if err != nil {
		return nil, 0, err
	}
	if !enabled {
		return map[string]any{
			"status":       "delivered",
			"kind":         "inbox",
			"delivered_at": time.Now().UTC().Format(time.RFC3339),
		}, 0, nil
	}
	var attempts int64
	var receipt map[string]any
	for attempt := 1; attempt <= watchDeliveryMaxAttempts; attempt++ {
		attempts++
		switch cfg.Kind {
		case "webhook":
			receipt, err = s.deliverWatchWebhook(ctx, def, event, cfg.Webhook)
		case "workflow":
			receipt, err = s.deliverWatchWorkflow(ctx, def, event, cfg.Workflow)
		case "inbox":
			return map[string]any{
				"status":       "delivered",
				"kind":         "inbox",
				"delivered_at": time.Now().UTC().Format(time.RFC3339),
			}, 0, nil
		default:
			return nil, attempts, fmt.Errorf("unsupported watch delivery kind %q", cfg.Kind)
		}
		if err == nil {
			return receipt, attempts, nil
		}
		if attempt == watchDeliveryMaxAttempts {
			break
		}
		if err := sleepContext(ctx, time.Duration(attempt)*100*time.Millisecond); err != nil {
			return receipt, attempts, err
		}
	}
	return receipt, attempts, err
}

func (s *graphjinService) completeWatchDelivery(ctx context.Context, event watchDeliveryEvent, status string, attempts int64, receipt map[string]any) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if receipt == nil {
		receipt = map[string]any{}
	}
	receipt["completed_at"] = now
	rows, err := s.internalStoreMutationRows(ctx, "watch_events",
		`where: { id: { eq: $id } }, update: $input`,
		watchEventStoreFields,
		map[string]any{
			"id": event.ID,
			"input": map[string]any{
				"delivery_status":   watchDeliveryStatus(status),
				"delivery_attempts": event.DeliveryAttempts + attempts,
				"receipt_json":      nullableJSONString(mustMarshalString(receipt)),
				"updated_at":        now,
			},
		})
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	if err := s.bumpArtifactRevision(ctx, "watch_events"); err != nil {
		return err
	}
	s.markWatchChanged("watch delivery")
	s.notifyWatchEventsResource(event.OwnerID, event.AccountID, event.WatchID)
	return nil
}

func (s *graphjinService) deliverWatchWebhook(ctx context.Context, def watchRuntimeDefinition, event watchDeliveryEvent, cfg watchWebhookConfig) (map[string]any, error) {
	u, err := s.validateWatchWebhookURL(ctx, cfg.URL)
	if err != nil {
		return nil, err
	}
	payload := watchDeliveryPayload(def, event)
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "GraphJin-Watch-Delivery/1")
	req.Header.Set("Idempotency-Key", event.ID)
	for key, value := range cfg.Headers {
		headerName, ok := safeWebhookHeaderName(key)
		if !ok {
			return nil, fmt.Errorf("unsafe webhook header %q", key)
		}
		req.Header.Set(headerName, value)
	}
	if cfg.SecretEnv != "" {
		secret := os.Getenv(cfg.SecretEnv)
		if secret == "" {
			return nil, fmt.Errorf("webhook secret env %s is not set", cfg.SecretEnv)
		}
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = mac.Write(body)
		req.Header.Set("X-GraphJin-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	client := &http.Client{
		Timeout:       watchDeliveryHTTPTimeout,
		Transport:     s.watchWebhookTransport(),
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, watchDeliveryMaxResponseBody))
	receipt := map[string]any{
		"status":      "delivered",
		"kind":        "webhook",
		"url":         u.Redacted(),
		"status_code": resp.StatusCode,
		"body":        string(respBody),
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return receipt, fmt.Errorf("webhook returned HTTP %d", resp.StatusCode)
	}
	return receipt, nil
}

func (s *graphjinService) deliverWatchWorkflow(ctx context.Context, def watchRuntimeDefinition, event watchDeliveryEvent, cfg watchWorkflowConfig) (map[string]any, error) {
	if strings.TrimSpace(cfg.Name) == "" {
		return nil, errors.New("workflow delivery requires name")
	}
	ownerCtx := s.watchOwnerContext(ctx, def)
	out, err := s.runNamedWorkflowPinned(ownerCtx, cfg.Name, def.WorkflowSourceHash, watchDeliveryPayload(def, event), nil)
	receipt := map[string]any{
		"status": "delivered",
		"kind":   "workflow",
		"name":   cfg.Name,
		"result": out,
	}
	if err != nil {
		return receipt, err
	}
	return receipt, nil
}

func (s *graphjinService) enrichWatchEvent(ctx context.Context, def *watchRuntimeDefinition, eventID, dataJSON, evidenceJSON string, cfg watchEnrichmentConfig) (bool, error) {
	if def == nil {
		return true, nil
	}
	if cfg.Kind == "flow" {
		return s.triageWatchEvent(ctx, def, eventID, dataJSON, evidenceJSON, cfg)
	}
	status := "pending"
	enrichment := map[string]any{}
	if cfg.MaxSteps <= 0 || cfg.MaxSteps > 4 {
		cfg.MaxSteps = 4
	}
	if strings.TrimSpace(cfg.Instruction) == "" {
		cfg.Instruction = "Summarize this watch event, explain why it matters, and suggest the next safe action."
	}
	if s.watchEnrichmentDailyCapReached(ctx, def.ID) {
		enrichment = map[string]any{"status": "skipped", "reason": "daily_cap"}
		return true, s.storeWatchEnrichment(ctx, eventID, status, false, enrichment)
	}
	ownerCtx := s.watchOwnerContext(ctx, *def)
	agentConf := agentConfigFromService(s.conf)
	agentConf.ReadOnly = true
	agentConf.MaxSteps = cfg.MaxSteps
	runner, err := newGraphJinAgentRunner(s, agentConf)
	if err != nil {
		enrichment = map[string]any{"status": "error", "error": err.Error()}
		if errors.Is(err, gjagent.ErrMissingAPIKey) {
			enrichment["code"] = modelCredentialsRequiredCode
			err = fmt.Errorf("%s: GraphJin-owned model credentials are required: %w", modelCredentialsRequiredCode, err)
		}
		if storeErr := s.storeWatchEnrichment(ctx, eventID, status, false, enrichment); storeErr != nil {
			return true, storeErr
		}
		return true, err
	}
	req := gjagent.Request{
		Instruction: cfg.Instruction,
		Context: map[string]any{
			"_watch_events": []any{map[string]any{
				"id":            eventID,
				"watch_id":      def.ID,
				"data_json":     parseJSONValue(dataJSON),
				"evidence_json": parseJSONValue(evidenceJSON),
			}},
			"watch": map[string]any{"id": def.ID, "name": def.Name},
		},
		MaxSteps:     cfg.MaxSteps,
		Capabilities: s.agentCapabilityProfile(ownerCtx),
	}
	resp, err := runner.Run(ownerCtx, req)
	if err != nil {
		enrichment = map[string]any{"status": "error", "error": err.Error()}
		if errors.Is(err, gjagent.ErrMissingAPIKey) {
			enrichment["code"] = modelCredentialsRequiredCode
			err = fmt.Errorf("%s: GraphJin-owned model credentials are required: %w", modelCredentialsRequiredCode, err)
		}
		if storeErr := s.storeWatchEnrichment(ctx, eventID, status, false, enrichment); storeErr != nil {
			return true, storeErr
		}
		return true, err
	}
	enrichment = map[string]any{
		"status":       "ok",
		"generated_at": time.Now().UTC().Format(time.RFC3339),
		"response":     resp,
	}
	return true, s.storeWatchEnrichment(ctx, eventID, status, false, enrichment)
}

func (s *graphjinService) triageWatchEvent(ctx context.Context, def *watchRuntimeDefinition, eventID, dataJSON, evidenceJSON string, cfg watchEnrichmentConfig) (bool, error) {
	started := time.Now()
	failOpen := func(reason string, runErr error) (bool, error) {
		deliveryStatus := "pending"
		failOpenNotification := true
		if def.ActionRequired {
			deliveryStatus = "flow_failed"
			failOpenNotification = false
		}
		enrichment := map[string]any{
			"status": "error", "kind": "flow", "flow_hash": cfg.FlowHash,
			"fail_open_notification": true, "fail_open_action": failOpenNotification, "error": reason,
		}
		if errors.Is(runErr, gjagent.ErrMissingAPIKey) {
			enrichment["code"] = modelCredentialsRequiredCode
		}
		if err := s.storeWatchEnrichment(ctx, eventID, deliveryStatus, false, enrichment); err != nil {
			return true, err
		}
		s.recordWatchFlowRuntimeEvent(ctx, def.ID, cfg.FlowHash, "runtime", "failed", reason, time.Since(started), map[string]any{
			"event_id": eventID, "fail_open_notification": true, "fail_open_action": failOpenNotification,
		})
		return true, runErr
	}
	if s.watchEnrichmentDailyCapReached(ctx, def.ID) {
		return failOpen("daily_cap", nil)
	}
	run, err := s.runWatchFlow(s.watchOwnerContext(ctx, *def), cfg, map[string]ax.Value{
		"event":    parseJSONValue(dataJSON),
		"watch":    map[string]any{"id": def.ID, "name": def.Name},
		"evidence": parseJSONValue(evidenceJSON),
	})
	if err != nil {
		return failOpen(err.Error(), err)
	}
	status := "pending"
	seen := false
	notify := true
	switch run.Verdict {
	case "digest":
		status, seen, notify = "digest_queued", true, false
	case "discard":
		status, seen, notify = "suppressed", true, false
	default:
		if def.ActionRequired && !def.ActionApproved {
			status = "approval_required"
		}
	}
	enrichment := map[string]any{
		"status": "ok", "kind": "flow", "flow_hash": run.FlowHash,
		"verdict": run.Verdict, "severity": run.Severity, "summary": run.Summary,
		"usage": run.Usage, "model_calls": run.ModelCalls,
		"duration_ms": run.Duration.Milliseconds(), "generated_at": time.Now().UTC().Format(time.RFC3339),
	}
	if err := s.storeWatchEnrichment(ctx, eventID, status, seen, enrichment); err != nil {
		return true, err
	}
	s.recordWatchFlowRuntimeEvent(ctx, def.ID, run.FlowHash, "runtime", "ok", "", time.Since(started), map[string]any{
		"event_id": eventID, "verdict": run.Verdict, "severity": run.Severity, "model_calls": run.ModelCalls,
	})
	return notify, nil
}

func (s *graphjinService) storeWatchEnrichment(ctx context.Context, eventID, deliveryStatus string, seen bool, enrichment map[string]any) error {
	now := time.Now().UTC().Format(time.RFC3339)
	input := map[string]any{
		"delivery_status": deliveryStatus,
		"enrichment_json": nullableJSONString(mustMarshalString(enrichment)),
		"seen":            seen,
		"seen_at":         nullableSeenAt(seen, now),
		"updated_at":      now,
	}
	if _, err := s.internalStoreMutationRows(ctx, "watch_events",
		`where: { id: { eq: $id } }, update: $input`, watchEventStoreFields,
		map[string]any{"id": eventID, "input": input}); err != nil {
		return err
	}
	if err := s.bumpArtifactRevision(ctx, "watch_events"); err != nil {
		return err
	}
	s.markWatchChanged("watch enrichment")
	return nil
}

func (s *graphjinService) watchEnrichmentDailyCapReached(ctx context.Context, watchID string) bool {
	if s == nil || s.conf == nil {
		return false
	}
	cap := s.conf.Core.EffectiveWatchesConfig().EnrichmentDailyCap
	if cap <= 0 {
		return false
	}
	rows, err := s.internalStoreAllRows(ctx, "watch_events", `where: { watch_id: { eq: $watch_id } }`, `id created_at enrichment_json`, map[string]any{"watch_id": watchID})
	if err != nil {
		return false
	}
	cutoff := time.Now().UTC().Add(-24 * time.Hour)
	count := 0
	for _, row := range rows {
		if strings.TrimSpace(jsonMapString(row, "enrichment_json")) == "" {
			continue
		}
		if ts, ok := parseWatchTime(stringMapValue(row, "created_at")); ok && ts.Before(cutoff) {
			continue
		}
		count++
	}
	return count >= cap
}

func parseWatchDeliveryConfig(raw string) (watchDeliveryConfig, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" || raw == "{}" {
		return watchDeliveryConfig{Kind: "inbox"}, false, nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return watchDeliveryConfig{}, false, fmt.Errorf("delivery_json is invalid: %w", err)
	}
	cfg := watchDeliveryConfig{Kind: strings.ToLower(strings.TrimSpace(fmt.Sprint(m["kind"])))}
	if webhook, ok := m["webhook"]; ok {
		cfg.Kind = "webhook"
		cfg.Webhook = parseWatchWebhookConfig(webhook)
	}
	if workflow, ok := m["workflow"]; ok {
		cfg.Kind = "workflow"
		cfg.Workflow = parseWatchWorkflowConfig(workflow)
	}
	if digest, ok := m["digest"]; ok && digest != nil {
		digestMap := mapFromAny(digest)
		windowText := strings.TrimSpace(stringFromAny(digestMap["window"]))
		if windowText == "" {
			return cfg, cfg.Kind != "inbox", errors.New("digest delivery requires window")
		}
		window, err := parseClampedWindow(windowText, watchDigestMinWindow, watchDigestMaxWindow)
		if err != nil {
			return cfg, cfg.Kind != "inbox", fmt.Errorf("digest window is invalid: %w", err)
		}
		cfg.Digest = watchDigestConfig{
			Enabled: true, Window: window, WindowText: window.String(),
		}
	}
	switch cfg.Kind {
	case "", "inbox":
		cfg.Kind = "inbox"
	case "webhook":
		if cfg.Webhook.URL == "" {
			cfg.Webhook = parseWatchWebhookConfig(m)
		}
		if strings.TrimSpace(cfg.Webhook.URL) == "" {
			return cfg, true, errors.New("webhook delivery requires url")
		}
	case "workflow":
		if cfg.Workflow.Name == "" {
			cfg.Workflow = parseWatchWorkflowConfig(m)
		}
		if strings.TrimSpace(cfg.Workflow.Name) == "" {
			return cfg, true, errors.New("workflow delivery requires name")
		}
	default:
		return cfg, true, fmt.Errorf("unsupported watch delivery kind %q", cfg.Kind)
	}
	return cfg, cfg.Kind != "inbox", nil
}

func parseWatchWebhookConfig(raw any) watchWebhookConfig {
	m := mapFromAny(raw)
	cfg := watchWebhookConfig{
		URL:       stringFromAny(m["url"]),
		SecretEnv: stringFromAny(m["secret_env"]),
		Headers:   map[string]string{},
	}
	for key, value := range mapFromAny(m["headers"]) {
		cfg.Headers[key] = stringFromAny(value)
	}
	return cfg
}

func parseWatchWorkflowConfig(raw any) watchWorkflowConfig {
	m := mapFromAny(raw)
	return watchWorkflowConfig{Name: stringFromAny(m["name"])}
}

func parseWatchEnrichmentConfig(raw string) (watchEnrichmentConfig, bool) {
	_, cfg, enabled, err := normalizeWatchEnrichmentJSON(raw)
	if err != nil {
		return watchEnrichmentConfig{}, false
	}
	return cfg, enabled
}

func (s *graphjinService) validateWatchWebhookURL(ctx context.Context, raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, err
	}
	u.Fragment = ""
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("webhook URL scheme must be http or https")
	}
	if u.User != nil || strings.TrimSpace(u.Host) == "" {
		return nil, fmt.Errorf("webhook URL must not include userinfo and must include host")
	}
	if s == nil || s.conf == nil || !watchWebhookAllowMatch(u, s.conf.Core.EffectiveWatchesConfig().WebhookAllow) {
		return nil, fmt.Errorf("webhook URL is not in watches.webhook_allow")
	}
	if err := validateWebhookResolvedHost(ctx, u, s.conf.Core.EffectiveWatchesConfig().WebhookAllow); err != nil {
		return nil, err
	}
	return u, nil
}

func (s *graphjinService) watchWebhookTransport() http.RoundTripper {
	return &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			ip, err := resolveWebhookDialIP(ctx, host, s.conf.Core.EffectiveWatchesConfig().WebhookAllow)
			if err != nil {
				return nil, err
			}
			d := net.Dialer{Timeout: watchDeliveryHTTPTimeout}
			return d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		},
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
	}
}

func validateWebhookResolvedHost(ctx context.Context, u *url.URL, allow []string) error {
	_, err := resolveWebhookDialIP(ctx, u.Hostname(), allow)
	return err
}

func resolveWebhookDialIP(ctx context.Context, host string, allow []string) (net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		if isBlockedWebhookIP(ip) && !watchAllowContainsLiteralIP(host, allow) {
			return nil, fmt.Errorf("webhook IP %s is private or local", ip)
		}
		return ip, nil
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("webhook host %s has no addresses", host)
	}
	var first net.IP
	for _, addr := range addrs {
		ip := addr.IP
		if first == nil {
			first = ip
		}
		if isBlockedWebhookIP(ip) {
			return nil, fmt.Errorf("webhook host %s resolved to private or local IP %s", host, ip)
		}
	}
	return first, nil
}

func isBlockedWebhookIP(ip net.IP) bool {
	return ip == nil ||
		ip.IsUnspecified() ||
		ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() ||
		isCGNATIP(ip)
}

func isCGNATIP(ip net.IP) bool {
	ip4 := ip.To4()
	return ip4 != nil &&
		ip4[0] == 100 &&
		ip4[1] >= 64 &&
		ip4[1] <= 127
}

func watchWebhookAllowMatch(u *url.URL, allow []string) bool {
	targetOrigin := strings.ToLower(u.Scheme + "://" + u.Host)
	for _, entry := range allow {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		allowed, err := url.Parse(entry)
		if err != nil || (allowed.Scheme != "http" && allowed.Scheme != "https") || allowed.Host == "" || allowed.User != nil {
			continue
		}
		if strings.ToLower(allowed.Scheme+"://"+allowed.Host) != targetOrigin {
			continue
		}
		path := strings.TrimRight(allowed.Path, "/")
		if path == "" || strings.HasPrefix(u.EscapedPath(), path) {
			return true
		}
	}
	return false
}

func watchAllowContainsLiteralIP(host string, allow []string) bool {
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, entry := range allow {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		u, err := url.Parse(entry)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			continue
		}
		if parsed := net.ParseIP(u.Hostname()); parsed != nil && parsed.Equal(ip) {
			return true
		}
	}
	return false
}

func safeWebhookHeaderName(name string) (string, bool) {
	name = textproto.CanonicalMIMEHeaderKey(strings.TrimSpace(name))
	if name == "" || strings.EqualFold(name, "host") || strings.EqualFold(name, "content-length") {
		return "", false
	}
	for _, r := range name {
		if r <= 32 || r >= 127 || strings.ContainsRune("()<>@,;:\\\"/[]?={}", r) {
			return "", false
		}
	}
	return name, true
}

func watchDeliveryEventFromRow(row map[string]any) watchDeliveryEvent {
	return watchDeliveryEvent{
		ID:               stringMapValue(row, "id"),
		WatchID:          stringMapValue(row, "watch_id"),
		DataHash:         stringMapValue(row, "data_hash"),
		DataJSON:         jsonMapString(row, "data_json"),
		EvidenceJSON:     jsonMapString(row, "evidence_json"),
		EnrichmentJSON:   jsonMapString(row, "enrichment_json"),
		DeliveryJSON:     jsonMapString(row, "delivery_json"),
		DeliveryAttempts: int64MapValue(row, "delivery_attempts"),
		AccountID:        stringMapValue(row, "account_id"),
		OwnerID:          stringMapValue(row, "owner_id"),
		CreatedAt:        stringMapValue(row, "created_at"),
	}
}

func watchDeliveryPayload(def watchRuntimeDefinition, event watchDeliveryEvent) map[string]any {
	enrichment := mapFromAny(parseJSONValue(event.EnrichmentJSON))
	eventPayload := map[string]any{
		"id":              event.ID,
		"watch_id":        event.WatchID,
		"data_hash":       event.DataHash,
		"data_json":       parseJSONValue(event.DataJSON),
		"evidence_json":   parseJSONValue(event.EvidenceJSON),
		"enrichment_json": parseJSONValue(event.EnrichmentJSON),
		"created_at":      event.CreatedAt,
	}
	for _, key := range []string{"verdict", "severity", "summary"} {
		if value := strings.TrimSpace(stringFromAny(enrichment[key])); value != "" {
			eventPayload[key] = value
		}
	}
	return map[string]any{
		"watch": map[string]any{
			"id":               def.ID,
			"name":             def.Name,
			"query":            def.Query,
			"saved_query_name": def.SavedQueryName,
			"lifecycle":        def.Lifecycle,
			"lease_expires_at": def.LeaseExpiresAt,
		},
		"event": eventPayload,
	}
}

func parseJSONValue(raw string) any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var out any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return raw
	}
	return out
}

func mapFromAny(v any) map[string]any {
	switch t := v.(type) {
	case map[string]any:
		return t
	default:
		var out map[string]any
		data, err := json.Marshal(t)
		if err != nil {
			return nil
		}
		if err := json.Unmarshal(data, &out); err != nil {
			return nil
		}
		return out
	}
}

func stringFromAny(v any) string {
	if v == nil {
		return ""
	}
	s, ok := v.(string)
	if ok {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

func intFromAny(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case json.Number:
		n, _ := strconv.Atoi(t.String())
		return n
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(t))
		return n
	default:
		return 0
	}
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
