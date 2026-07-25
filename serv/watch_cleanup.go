package serv

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	watchCleanupReasonExpiredEphemeral = "expired_ephemeral"
	watchCleanupReasonDisabledStale    = "disabled_stale"
	watchCleanupReasonErroredStale     = "errored_stale"
	watchCleanupReasonOrphanedEvents   = "orphaned_events"
	watchCleanupReasonRetentionEvents  = "retention_events"
)

type watchCleanupOptions struct {
	StaleHours int `json:"stale_hours,omitempty"`
}

type watchCleanupPreview struct {
	GeneratedAt string                             `json:"generated_at"`
	Token       string                             `json:"token"`
	StaleHours  int                                `json:"stale_hours"`
	Counts      map[string]int                     `json:"counts"`
	Candidates  map[string][]watchCleanupCandidate `json:"candidates"`
}

type watchCleanupCandidate struct {
	Kind           string `json:"kind"`
	ID             string `json:"id"`
	WatchID        string `json:"watch_id,omitempty"`
	Name           string `json:"name,omitempty"`
	Reason         string `json:"reason"`
	Action         string `json:"action"`
	Status         string `json:"status,omitempty"`
	Lifecycle      string `json:"lifecycle,omitempty"`
	LeaseExpiresAt string `json:"lease_expires_at,omitempty"`
	CreatedAt      string `json:"created_at,omitempty"`
	UpdatedAt      string `json:"updated_at,omitempty"`
	OwnerID        string `json:"owner_id,omitempty"`
}

type watchCleanupApplyRequest struct {
	Token      string   `json:"token"`
	StaleHours int      `json:"stale_hours,omitempty"`
	Reasons    []string `json:"reasons,omitempty"`
	WatchIDs   []string `json:"watch_ids,omitempty"`
	EventIDs   []string `json:"event_ids,omitempty"`
}

type watchCleanupApplyResult struct {
	ExpiredWatchIDs []string `json:"expired_watch_ids,omitempty"`
	DeletedWatchIDs []string `json:"deleted_watch_ids,omitempty"`
	DeletedEventIDs []string `json:"deleted_event_ids,omitempty"`
}

func (s *graphjinService) previewWatchCleanup(ctx context.Context, opts watchCleanupOptions) (watchCleanupPreview, error) {
	out := watchCleanupPreview{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Candidates:  map[string][]watchCleanupCandidate{},
		Counts:      map[string]int{},
	}
	if _, _, _, _, ok := s.watchDB(); !ok {
		out.Token = s.watchCleanupToken(ctx, out)
		return out, nil
	}
	ownerID, hasUser := artifactUserID(ctx)
	if !hasUser {
		return out, fmt.Errorf("watch cleanup requires user identity")
	}
	admin := s.identityRoleIsAdmin(ctx)
	staleHours := opts.StaleHours
	if staleHours <= 0 {
		staleHours = s.conf.Core.EffectiveWatchesConfig().EventRetentionHours
	}
	if staleHours <= 0 {
		staleHours = 168
	}
	out.StaleHours = staleHours
	staleCutoff := time.Now().UTC().Add(-time.Duration(staleHours) * time.Hour)
	retentionCutoff := time.Time{}
	if hours := s.conf.Core.EffectiveWatchesConfig().EventRetentionHours; hours > 0 {
		retentionCutoff = time.Now().UTC().Add(-time.Duration(hours) * time.Hour)
	}

	watchRows, err := s.internalStoreAllRows(ctx, "watches", "", watchStoreFields, nil)
	if err != nil {
		return out, err
	}
	visibleWatchIDs := map[string]struct{}{}
	for _, row := range watchRows {
		if !admin && stringMapValue(row, "owner_id") != ownerID {
			continue
		}
		id := stringMapValue(row, "id")
		if id != "" {
			visibleWatchIDs[id] = struct{}{}
		}
		for _, candidate := range watchCleanupCandidatesForWatch(row, staleCutoff) {
			out.Candidates[candidate.Reason] = append(out.Candidates[candidate.Reason], candidate)
		}
	}

	eventRows, err := s.internalStoreAllRows(ctx, "watch_events", "", watchEventStoreFields, nil)
	if err != nil {
		return out, err
	}
	for _, row := range eventRows {
		if !admin && stringMapValue(row, "owner_id") != ownerID {
			continue
		}
		watchID := stringMapValue(row, "watch_id")
		if _, ok := visibleWatchIDs[watchID]; !ok {
			c := watchCleanupCandidateForEvent(row, watchCleanupReasonOrphanedEvents, "delete_event")
			out.Candidates[c.Reason] = append(out.Candidates[c.Reason], c)
			continue
		}
		if !retentionCutoff.IsZero() {
			if createdAt, ok := parseWatchTime(stringMapValue(row, "created_at")); ok && createdAt.Before(retentionCutoff) {
				c := watchCleanupCandidateForEvent(row, watchCleanupReasonRetentionEvents, "delete_event")
				out.Candidates[c.Reason] = append(out.Candidates[c.Reason], c)
			}
		}
	}

	for reason, rows := range out.Candidates {
		sort.SliceStable(rows, func(i, j int) bool {
			return rows[i].UpdatedAt > rows[j].UpdatedAt
		})
		out.Candidates[reason] = rows
		out.Counts[reason] = len(rows)
	}
	out.Token = s.watchCleanupToken(ctx, out)
	return out, nil
}

func watchCleanupCandidatesForWatch(row map[string]any, staleCutoff time.Time) []watchCleanupCandidate {
	lifecycle := watchLifecycle(stringMapValue(row, "lifecycle"))
	status := watchStatus(stringMapValue(row, "status"))
	c := watchCleanupCandidate{
		Kind:           "watch",
		ID:             stringMapValue(row, "id"),
		Name:           stringMapValue(row, "name"),
		Status:         status,
		Lifecycle:      lifecycle,
		LeaseExpiresAt: stringMapValue(row, "lease_expires_at"),
		CreatedAt:      stringMapValue(row, "created_at"),
		UpdatedAt:      stringMapValue(row, "updated_at"),
		OwnerID:        safeArtifactIdentity(stringMapValue(row, "owner_id"), false),
	}
	var out []watchCleanupCandidate
	if lifecycle == "ephemeral" && status != "expired" {
		if expiresAt, ok := parseWatchTime(c.LeaseExpiresAt); ok && !expiresAt.After(time.Now().UTC()) {
			c.Reason = watchCleanupReasonExpiredEphemeral
			c.Action = "expire_watch"
			out = append(out, c)
		}
	}
	if lifecycle == "durable" {
		rowTime, _ := parseWatchTime(c.UpdatedAt)
		if rowTime.IsZero() {
			rowTime, _ = parseWatchTime(c.CreatedAt)
		}
		stale := rowTime.IsZero() || rowTime.Before(staleCutoff)
		if stale && (!boolMapValue(row, "enabled") || status == "paused" || stringMapValue(row, "last_fired_at") == "") {
			c.Reason = watchCleanupReasonDisabledStale
			c.Action = "delete_watch"
			out = append(out, c)
		}
		if stale && (status == "error" || stringMapValue(row, "last_error") != "" || int64MapValue(row, "failure_count") > 0) {
			c.Reason = watchCleanupReasonErroredStale
			c.Action = "delete_watch"
			out = append(out, c)
		}
	}
	return out
}

func watchCleanupCandidateForEvent(row map[string]any, reason, action string) watchCleanupCandidate {
	return watchCleanupCandidate{
		Kind:      "event",
		ID:        stringMapValue(row, "id"),
		WatchID:   stringMapValue(row, "watch_id"),
		Reason:    reason,
		Action:    action,
		CreatedAt: stringMapValue(row, "created_at"),
		UpdatedAt: stringMapValue(row, "updated_at"),
		OwnerID:   safeArtifactIdentity(stringMapValue(row, "owner_id"), false),
	}
}

func (s *graphjinService) watchCleanupToken(ctx context.Context, preview watchCleanupPreview) string {
	ownerID, _ := artifactUserID(ctx)
	material := map[string]any{
		"owner_id":    ownerID,
		"admin":       s.identityRoleIsAdmin(ctx),
		"stale_hours": preview.StaleHours,
		"candidates":  preview.Candidates,
	}
	data, _ := json.Marshal(material)
	sum := sha256.Sum256(data)
	return "watch-cleanup:" + hex.EncodeToString(sum[:16])
}

func (s *graphjinService) applyWatchCleanup(ctx context.Context, req watchCleanupApplyRequest) (watchCleanupApplyResult, error) {
	var result watchCleanupApplyResult
	preview, err := s.previewWatchCleanup(ctx, watchCleanupOptions{StaleHours: req.StaleHours})
	if err != nil {
		return result, err
	}
	if strings.TrimSpace(req.Token) == "" || req.Token != preview.Token {
		return result, fmt.Errorf("watch cleanup token is invalid or stale; run cleanup-preview again")
	}
	reasons := stringSet(req.Reasons)
	watchIDs := stringSet(req.WatchIDs)
	eventIDs := stringSet(req.EventIDs)
	if len(reasons) == 0 && len(watchIDs) == 0 && len(eventIDs) == 0 {
		return result, fmt.Errorf("watch cleanup apply requires reasons, watch_ids, or event_ids")
	}
	expiredDone := map[string]bool{}
	deletedWatchDone := map[string]bool{}
	deletedEventDone := map[string]bool{}
	for reason, rows := range preview.Candidates {
		for _, c := range rows {
			switch c.Kind {
			case "watch":
				if expiredDone[c.ID] || deletedWatchDone[c.ID] {
					continue
				}
				if !watchIDs[c.ID] && !reasons[reason] {
					continue
				}
				if reason != watchCleanupReasonExpiredEphemeral && !watchIDs[c.ID] {
					continue
				}
				if c.Action == "expire_watch" {
					if err := s.expireWatchByID(ctx, c.ID); err != nil {
						return result, err
					}
					expiredDone[c.ID] = true
					result.ExpiredWatchIDs = append(result.ExpiredWatchIDs, c.ID)
					continue
				}
				if c.Action == "delete_watch" {
					if err := s.deleteWatchByID(ctx, c.ID); err != nil {
						return result, err
					}
					deletedWatchDone[c.ID] = true
					result.DeletedWatchIDs = append(result.DeletedWatchIDs, c.ID)
				}
			case "event":
				if deletedEventDone[c.ID] {
					continue
				}
				if !eventIDs[c.ID] && !reasons[reason] {
					continue
				}
				if _, err := s.deleteWatchEventByID(ctx, c.ID); err != nil {
					return result, err
				}
				deletedEventDone[c.ID] = true
				result.DeletedEventIDs = append(result.DeletedEventIDs, c.ID)
			}
		}
	}
	if len(result.ExpiredWatchIDs) != 0 || len(result.DeletedWatchIDs) != 0 {
		if err := s.bumpArtifactRevision(ctx, "watches"); err != nil {
			return result, err
		}
	}
	if len(result.DeletedWatchIDs) != 0 || len(result.DeletedEventIDs) != 0 {
		if err := s.bumpArtifactRevision(ctx, "watch_events"); err != nil {
			return result, err
		}
	}
	if len(result.ExpiredWatchIDs) != 0 || len(result.DeletedWatchIDs) != 0 || len(result.DeletedEventIDs) != 0 {
		s.markWatchChanged("watch cleanup apply")
	}
	if len(result.ExpiredWatchIDs) != 0 || len(result.DeletedWatchIDs) != 0 {
		s.publishWatchRunnerChanged(ctx)
	}
	return result, nil
}

func (s *graphjinService) expireWatchByID(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return nil
	}
	_, err := s.internalStoreMutationRows(ctx, "watches", `where: { id: { eq: $id } }, update: $input`, watchStoreFields, map[string]any{
		"id": id,
		"input": map[string]any{
			"status":     "expired",
			"enabled":    false,
			"updated_at": time.Now().UTC().Format(time.RFC3339),
		},
	})
	return err
}

func (s *graphjinService) deleteWatchByID(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return nil
	}
	if _, err := s.internalStoreMutationRows(ctx, "watches", `where: { id: { eq: $id } }, delete: true`, `id`, map[string]any{"id": id}); err != nil {
		return err
	}
	_, err := s.deleteWatchEvents(ctx, id)
	return err
}

func stringSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" {
			out[v] = true
		}
	}
	return out
}
