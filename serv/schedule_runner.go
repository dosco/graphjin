package serv

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	// Every replica must resolve a schedule's zone the same way, including
	// minimal container images that ship without a system zoneinfo database.
	_ "time/tzdata"
)

const (
	// watchScheduleMaxSleep bounds how long the schedule runner sleeps when it
	// knows of no sooner fire time, so a schedule written by another replica is
	// picked up even when no revision signal or Redis wake-up arrives.
	watchScheduleMaxSleep = 30 * time.Second
	watchScheduleMinSleep = 250 * time.Millisecond
	watchScheduleScanArgs = `where: { schedule_cron: { neq: "" } }`

	// cronStarBit is robfig/cron's (unexported) marker for a field written as
	// "*". The fall-back DST rule below keys off it for the hour field.
	cronStarBit = uint64(1) << 63
)

type watchScheduleSweep struct {
	Fired   int
	Soonest time.Time
}

// next returns the first fire time strictly after after, applying the two
// classic cron rules for daylight-saving transitions so a daily job runs
// exactly once per local day:
//
//   - spring forward: a wall-clock time the transition skips (02:30 on a day
//     that jumps 02:00 -> 03:00) fires at the transition instant instead of
//     silently skipping the day.
//   - fall back: a schedule with a fixed hour does not fire again during the
//     repeated hour. Schedules whose hour field is "*" fire in both passes,
//     because those are distinct real hours.
func (sch watchSchedule) next(after time.Time) time.Time {
	if sch.spec == nil {
		return time.Time{}
	}
	cursor := after
	// Each skipped candidate is a fall-back duplicate; a handful at most per
	// transition, so the bound only guards against a pathological zone table.
	for range 512 {
		candidate := sch.spec.Next(cursor)
		limit := candidate
		if limit.IsZero() {
			limit = after.AddDate(5, 0, 0)
		}
		if gapFire, ok := sch.springForwardFire(cursor, limit); ok {
			return gapFire
		}
		if candidate.IsZero() {
			return candidate
		}
		if sch.spec.Hour&cronStarBit == 0 && inRepeatedWallClock(candidate.In(sch.spec.Location)) {
			cursor = candidate
			continue
		}
		return candidate
	}
	return time.Time{}
}

// springForwardFire reports the first forward zone transition in
// (after, until] during which the schedule would have matched a wall-clock
// time that never happened.
func (sch watchSchedule) springForwardFire(after, until time.Time) (time.Time, bool) {
	loc := sch.spec.Location
	cursor := after.In(loc)
	for {
		_, transition := cursor.ZoneBounds()
		if transition.IsZero() || transition.After(until) {
			return time.Time{}, false
		}
		_, offsetBefore := transition.Add(-time.Second).Zone()
		_, offsetAfter := transition.Zone()
		if skipped := offsetAfter - offsetBefore; skipped > 0 {
			// Read in the pre-transition offset, the instants
			// [transition, transition+skipped) are exactly the skipped
			// wall-clock times.
			fixed := *sch.spec
			fixed.Location = time.FixedZone("", offsetBefore)
			matched := fixed.Next(transition.Add(-time.Second))
			if !matched.IsZero() && matched.Before(transition.Add(time.Duration(skipped)*time.Second)) {
				return transition, true
			}
		}
		cursor = transition.In(loc)
	}
}

// inRepeatedWallClock reports whether t falls in the second pass of a
// wall-clock interval that a backward zone transition repeated.
func inRepeatedWallClock(t time.Time) bool {
	start, _ := t.ZoneBounds()
	if start.IsZero() {
		return false
	}
	_, offset := t.Zone()
	_, offsetBefore := start.Add(-time.Second).Zone()
	repeated := offsetBefore - offset
	return repeated > 0 && t.Sub(start) < time.Duration(repeated)*time.Second
}

// formatWatchFireTime is the canonical stored form of next_fire_at. Text
// dialects compare it as a string, so it is always UTC with second precision.
func formatWatchFireTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func watchRowRunnable(row map[string]any) bool {
	return boolMapValue(row, "enabled") &&
		watchStatus(stringMapValue(row, "status")) == "active" &&
		watchApproval(stringMapValue(row, "approval")) == "approved"
}

// watchScheduleResumeNextFire restarts the clock of a schedule watch that is
// becoming runnable again. Ticks that passed while it was paused are skipped,
// not delivered late.
func watchScheduleResumeNextFire(row map[string]any, runnable bool) (string, bool) {
	if row == nil || !runnable || watchRowRunnable(row) {
		return "", false
	}
	sched, err := parseWatchSchedule(stringMapValue(row, "schedule_cron"), stringMapValue(row, "schedule_tz"))
	if stringMapValue(row, "schedule_cron") == "" || err != nil {
		return "", false
	}
	return formatWatchFireTime(sched.next(time.Now().UTC())), true
}

// watchScheduleEventID is deterministic in (watch, fire time): every replica
// that reaches the same tick derives the same event id, so the event row's
// primary key is what lets exactly one of them fire it.
func watchScheduleEventID(watchID string, due time.Time) (eventID, dataHash string) {
	dataHash = hashString("schedule:" + formatWatchFireTime(due))
	return watchEventID(watchID, dataHash), dataHash
}

// startWatchScheduleRunner starts the time-trigger sweep. It runs on every
// replica that runs watches; the event's primary key decides which replica
// fires each tick, so no leader, lock, or Redis is required.
func (s *graphjinService) startWatchScheduleRunner(parent context.Context) {
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
	s.ensureWatchCoordinator(ctx)
	s.revisionConsumerWG.Add(1)
	go func() {
		defer s.revisionConsumerWG.Done()
		s.watchScheduleLoop(ctx)
	}()
}

func (s *graphjinService) watchScheduleLoop(ctx context.Context) {
	interval := time.Duration(s.conf.Core.EffectiveArtifactsConfig().PollSeconds) * time.Second
	if interval < watchRunnerMinInterval {
		interval = watchRunnerMinInterval
	}
	// Both wake-ups only shorten the wait for a newly written schedule; the
	// bounded sleep below is what correctness relies on.
	revisionChanges := s.revisionSignals(ctx, "watches", false, interval)
	var runnerChanges <-chan struct{}
	if coord := s.currentWatchCoordinator(); coord != nil {
		runnerChanges = coord.SubscribeRunnerChanges(ctx)
	}
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		case _, ok := <-revisionChanges:
			if !ok {
				return
			}
		case _, ok := <-runnerChanges:
			if !ok {
				runnerChanges = nil
				continue
			}
		}
		sweep, err := s.sweepWatchSchedules(ctx, time.Now().UTC())
		if err != nil {
			s.recordWatchRunnerError("sweep watch schedules", err, nil)
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(watchScheduleSleep(sweep.Soonest, time.Now().UTC()))
	}
}

func watchScheduleSleep(soonest, now time.Time) time.Duration {
	wait := watchScheduleMaxSleep
	if !soonest.IsZero() {
		if until := soonest.Sub(now); until < wait {
			wait = until
		}
	}
	if wait < watchScheduleMinSleep {
		wait = watchScheduleMinSleep
	}
	return wait
}

// sweepWatchSchedules fires every runnable schedule watch whose next_fire_at
// is due at now. Ticks missed while no replica was running are coalesced into
// one fire for the due tick, and the schedule resumes from now.
func (s *graphjinService) sweepWatchSchedules(ctx context.Context, now time.Time) (watchScheduleSweep, error) {
	var out watchScheduleSweep
	if _, _, _, _, ok := s.watchDB(); !ok {
		return out, nil
	}
	rows, err := s.internalStoreAllRows(ctx, "watches", watchScheduleScanArgs, watchStoreFields, nil)
	if err != nil {
		return out, err
	}
	now = now.UTC()
	noteNext := func(t time.Time) {
		if !t.IsZero() && (out.Soonest.IsZero() || t.Before(out.Soonest)) {
			out.Soonest = t
		}
	}
	var firstErr error
	for _, row := range rows {
		id := stringMapValue(row, "id")
		if id == "" || stringMapValue(row, "schedule_cron") == "" || !watchRowRunnable(row) {
			continue
		}
		sched, err := parseWatchSchedule(stringMapValue(row, "schedule_cron"), stringMapValue(row, "schedule_tz"))
		if err != nil {
			// The zone database or parser changed under a stored row; stop
			// the watch with a visible error rather than retry forever.
			s.updateWatchRunnerError(ctx, id, fmt.Errorf("gj_watch %w", err))
			continue
		}
		raw := stringMapValue(row, "next_fire_at")
		due, hasDue := parseWatchTime(raw)
		if !hasDue {
			// Rows written before next_fire_at existed start their clock now.
			next := sched.next(now)
			if _, err := s.advanceWatchSchedule(ctx, id, raw, time.Time{}, next); err != nil && firstErr == nil {
				firstErr = err
			}
			noteNext(next)
			continue
		}
		if due.After(now) {
			noteNext(due)
			continue
		}
		fired, next, err := s.fireWatchSchedule(ctx, row, sched, raw, due, now)
		if err != nil {
			s.recordWatchRunnerError("fire watch schedule", err, map[string]any{"watch_id": id})
			if firstErr == nil {
				firstErr = err
			}
		}
		if fired {
			out.Fired++
		}
		noteNext(next)
	}
	return out, firstErr
}

// fireWatchSchedule records the due tick and then moves the schedule on.
//
// The event goes in first. Its id is deterministic in (watch, fire time) and
// is the event row's primary key, so of all the replicas that reach this tick
// exactly one insert succeeds and the rest find the row and write nothing.
// Only then does next_fire_at move. A replica that dies between the two steps
// leaves the tick due with its event already written; the next sweep, on any
// replica, finds the event and only moves the clock on. A tick is neither lost
// nor doubled, and no step holds a lock or needs Redis.
func (s *graphjinService) fireWatchSchedule(
	ctx context.Context,
	row map[string]any,
	sched watchSchedule,
	raw string,
	due, now time.Time,
) (bool, time.Time, error) {
	def := watchScheduleDefinition(s, row)
	ownerCtx := s.watchOwnerContext(ctx, def)
	proposal, err := s.watchActionProposal(ownerCtx, "", "", def.VariablesJSON, def.EnrichJSON, def.DeliveryJSON, "")
	if err != nil {
		// Same treatment as a subscription watch whose action no longer
		// resolves: park it for review instead of firing an unbound action.
		if pauseErr := s.pauseWatchForReview(ctx, row, watchReviewPending); pauseErr != nil {
			return false, time.Time{}, pauseErr
		}
		return false, time.Time{}, nil
	}
	evidence := mapFromAny(parseJSONValue(jsonMapString(row, "evidence_json")))
	def.ActionHash = proposal.Hash
	def.ActionRequired = proposal.Required
	def.ActionApproved = watchActionApproval(evidence, proposal) == watchReviewApproved
	def.WorkflowSourceHash = proposal.WorkflowSourceHash

	// The ordering already makes an interrupted fire safe; this only keeps a
	// shutdown from cutting the follow-up bookkeeping (enrichment, wake-ups)
	// short once the event exists.
	ctx = context.WithoutCancel(ctx)
	inserted, err := s.insertWatchScheduleEvent(ctx, &def, proposal, sched, due, now)
	if err != nil {
		// Nothing has moved: the tick stays due and the next sweep retries it.
		return false, time.Time{}, err
	}
	next := sched.next(now)
	advanced, err := s.advanceWatchSchedule(ctx, def.ID, raw, due, next)
	if err != nil {
		return inserted, time.Time{}, err
	}
	if !inserted && !advanced {
		// Another replica recorded this tick and moved the clock.
		return false, next, nil
	}
	if !inserted {
		// The event was written by a replica that stopped (or has not yet
		// got) past the insert, so its delivery wake-up may never have been
		// sent. A spare wake-up is harmless.
		if err := s.bumpArtifactRevision(ctx, "watch_events"); err != nil {
			return false, next, err
		}
	}
	nowText := now.Format(time.RFC3339Nano)
	if _, err := s.internalStoreMutationRows(ctx, "watches", `where: { id: { eq: $id } }, update: $input`, watchStoreFields, map[string]any{
		"id":    def.ID,
		"input": map[string]any{"last_fired_at": nowText, "last_error": "", "failure_count": 0, "updated_at": nowText},
	}); err != nil {
		return inserted, next, err
	}
	if err := s.bumpArtifactRevision(ctx, "watches"); err != nil {
		return inserted, next, err
	}
	s.markWatchChanged("watch schedule fired")
	return inserted, next, nil
}

// advanceWatchSchedule moves next_fire_at from prev (as read, raw) to next and
// reports whether this call made the move; of several replicas advancing the
// same tick, exactly one does.
func (s *graphjinService) advanceWatchSchedule(ctx context.Context, id, raw string, prev, next time.Time) (bool, error) {
	db, dbType, watches, _, ok := s.watchDB()
	if !ok {
		return false, nil
	}
	// Direct SQL for the same reason as claimTaskVerification: the internal
	// GraphQL mutation API cannot report the affected-row count, and a
	// read-back cannot tell two replicas apart when both would write the same
	// next fire time. A plain conditional UPDATE is enough: the database
	// re-checks the WHERE against the committed row, so a second writer
	// matches nothing once the first has moved the clock.
	nextFire := quoteStoreIdent(dbType, "next_fire_at")
	args := []any{watchScheduleSQLTime(dbType, next), id}
	match := nextFire + " IS NULL"
	if !prev.IsZero() || raw != "" {
		// Text dialects compare against the stored text exactly as read;
		// Postgres compares instants.
		var prevArg any = raw
		if dbType == "postgres" || dbType == "" {
			prevArg = prev.UTC()
		}
		args = append(args, prevArg)
		match = nextFire + " = " + taskVerifySQLPlaceholder(dbType, 3)
	}
	query := fmt.Sprintf("UPDATE %s SET %s = %s WHERE %s = %s AND %s",
		watches,
		nextFire, taskVerifySQLPlaceholder(dbType, 1),
		quoteStoreIdent(dbType, "id"), taskVerifySQLPlaceholder(dbType, 2),
		match)
	res, err := db.ExecContext(ctx, query, args...)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected == 1, nil
}

func watchScheduleSQLTime(dbType string, t time.Time) any {
	if t.IsZero() {
		return nil
	}
	if dbType == "postgres" || dbType == "" {
		return t.UTC()
	}
	return formatWatchFireTime(t)
}

func watchScheduleDefinition(s *graphjinService, row map[string]any) watchRuntimeDefinition {
	return watchRuntimeDefinition{
		ID:             stringMapValue(row, "id"),
		Name:           stringMapValue(row, "name"),
		VariablesJSON:  jsonMapString(row, "variables_json"),
		DeliveryJSON:   jsonMapString(row, "delivery_json"),
		EnrichJSON:     jsonMapString(row, "enrich_json"),
		Lifecycle:      watchLifecycle(stringMapValue(row, "lifecycle")),
		LeaseExpiresAt: stringMapValue(row, "lease_expires_at"),
		LeaseOwnerID:   stringMapValue(row, "lease_owner_id"),
		AccountID:      stringMapValue(row, "account_id"),
		OwnerID:        stringMapValue(row, "owner_id"),
		OwnerRole:      s.trustedWatchRunnerRole(stringMapValue(row, "owner_role")),
	}
}

// insertWatchScheduleEvent writes the event for one tick. It returns false
// without error when the event already exists, which is how every replica but
// one learns it lost the tick.
func (s *graphjinService) insertWatchScheduleEvent(
	ctx context.Context,
	def *watchRuntimeDefinition,
	proposal watchActionProposal,
	sched watchSchedule,
	due, now time.Time,
) (bool, error) {
	eventID, dataHash := watchScheduleEventID(def.ID, due)
	existing, err := s.internalWatchEventStoreRow(ctx, eventID)
	if err != nil {
		return false, err
	}
	if existing != nil {
		return false, nil
	}
	dueText := formatWatchFireTime(due)
	nowText := now.Format(time.RFC3339Nano)
	payload := map[string]any{
		"kind":                "schedule",
		"watch_id":            def.ID,
		"watch_name":          def.Name,
		"cron":                sched.Cron,
		"timezone":            sched.Timezone,
		"scheduled_for":       dueText,
		"scheduled_for_local": due.In(sched.spec.Location).Format(time.RFC3339),
		"fired_at":            nowText,
	}
	dataBytes, err := json.Marshal(payload)
	if err != nil {
		return false, err
	}
	dataJSON, dataTruncated := s.watchSnapshotJSON(string(dataBytes))
	eventEvidence := map[string]any{
		"watch_id":    def.ID,
		"watch_name":  def.Name,
		"observed_at": nowText,
		"schedule": map[string]any{
			"cron":          sched.Cron,
			"timezone":      sched.Timezone,
			"scheduled_for": dueText,
		},
	}
	if def.ActionRequired {
		eventEvidence["action_hash"] = def.ActionHash
		eventEvidence["delivery_kind"] = proposal.Kind
		if def.WorkflowSourceHash != "" {
			eventEvidence["workflow_source_hash"] = def.WorkflowSourceHash
		}
	}
	evidenceJSON := mustMarshalString(eventEvidence)
	enrichCfg, enrichEnabled := parseWatchEnrichmentConfig(def.EnrichJSON)
	deliveryStatus := "pending"
	if def.ActionRequired && !def.ActionApproved {
		deliveryStatus = "approval_required"
	}
	if enrichEnabled {
		deliveryStatus = "enriching"
	}
	input := map[string]any{
		"id": eventID, "watch_id": def.ID, "data_hash": dataHash,
		"data_json": nullableJSONString(dataJSON), "data_truncated": dataTruncated,
		"evidence_json":   nullableJSONString(evidenceJSON),
		"delivery_status": deliveryStatus, "delivery_attempts": 0,
		"delivery_json": nullableJSONString(def.DeliveryJSON), "receipt_json": nil,
		"enrichment_json": nil, "seen": false, "snoozed_until": nil,
		"account_id": def.AccountID, "owner_id": def.OwnerID,
		"created_at": nowText, "updated_at": nowText,
	}
	if _, err := s.internalStoreMutationRows(ctx, "watch_events", `insert: $input`, watchEventStoreFields, map[string]any{"input": input}); err != nil {
		existing, loadErr := s.internalWatchEventStoreRow(ctx, eventID)
		if loadErr != nil {
			return false, loadErr
		}
		if existing == nil {
			return false, err
		}
		return false, nil
	}
	notify := true
	if enrichEnabled {
		var enrichErr error
		notify, enrichErr = s.enrichWatchEvent(ctx, def, eventID, dataJSON, evidenceJSON, enrichCfg)
		if enrichErr != nil {
			s.recordWatchRunnerError("enrich watch schedule event", enrichErr, map[string]any{"watch_id": def.ID, "event_id": eventID})
		}
	}
	if err := s.pruneWatchEvents(ctx, def); err != nil {
		return true, err
	}
	// The watch_events revision is what wakes the delivery loop on every
	// replica; delivery itself is the same claim-and-deliver path data events
	// take.
	if err := s.bumpArtifactRevision(ctx, "watch_events"); err != nil {
		return true, err
	}
	if notify {
		s.notifyWatchEventsResourceScope(watchEventScope{OwnerID: def.OwnerID, AccountID: def.AccountID, WatchID: def.ID}, true)
	}
	return true, nil
}
