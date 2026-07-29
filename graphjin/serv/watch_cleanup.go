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

	"github.com/dosco/graphjin/core/v3"
)

const (
	watchCleanupReasonExpiredEphemeral     = "expired_ephemeral"
	watchCleanupReasonDisabledStale        = "disabled_stale"
	watchCleanupReasonErroredStale         = "errored_stale"
	watchCleanupReasonOrphanedEvents       = "orphaned_events"
	watchCleanupReasonRetentionEvents      = "retention_events"
	watchCleanupReasonOrphanedSavedQueries = "orphaned_saved_queries"
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
	Token       string   `json:"token"`
	StaleHours  int      `json:"stale_hours,omitempty"`
	Reasons     []string `json:"reasons,omitempty"`
	WatchIDs    []string `json:"watch_ids,omitempty"`
	EventIDs    []string `json:"event_ids,omitempty"`
	ArtifactIDs []string `json:"artifact_ids,omitempty"`
}

type watchCleanupApplyResult struct {
	ExpiredWatchIDs    []string `json:"expired_watch_ids,omitempty"`
	DeletedWatchIDs    []string `json:"deleted_watch_ids,omitempty"`
	DeletedEventIDs    []string `json:"deleted_event_ids,omitempty"`
	DeletedArtifactIDs []string `json:"deleted_artifact_ids,omitempty"`
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

	orphans, err := s.orphanedSavedQueryArtifacts(ctx, watchRows, ownerID, admin)
	if err != nil {
		return out, err
	}
	for _, c := range orphans {
		out.Candidates[c.Reason] = append(out.Candidates[c.Reason], c)
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
	artifactIDs := stringSet(req.ArtifactIDs)
	if len(reasons) == 0 && len(watchIDs) == 0 && len(eventIDs) == 0 && len(artifactIDs) == 0 {
		return result, fmt.Errorf("watch cleanup apply requires reasons, watch_ids, event_ids, or artifact_ids")
	}
	expiredDone := map[string]bool{}
	deletedWatchDone := map[string]bool{}
	deletedEventDone := map[string]bool{}
	deletedArtifactDone := map[string]bool{}
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
			case "saved_query":
				if deletedArtifactDone[c.ID] {
					continue
				}
				if !artifactIDs[c.ID] && !reasons[reason] {
					continue
				}
				if err := s.deleteSavedQueryArtifactRow(ctx, c.ID); err != nil {
					return result, err
				}
				deletedArtifactDone[c.ID] = true
				result.DeletedArtifactIDs = append(result.DeletedArtifactIDs, c.ID)
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
	if len(result.DeletedArtifactIDs) != 0 {
		if err := s.bumpArtifactRevision(ctx, "artifacts"); err != nil {
			return result, err
		}
		s.markArtifactChanged("watch cleanup apply")
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
	row, err := s.internalWatchStoreRow(ctx, id)
	if err != nil {
		return err
	}
	if _, err := s.internalStoreMutationRows(ctx, "watches", `where: { id: { eq: $id } }, delete: true`, `id`, map[string]any{"id": id}); err != nil {
		return err
	}
	if _, err := s.deleteWatchEvents(ctx, id); err != nil {
		return err
	}
	return s.cleanupWatchSavedQueryArtifacts(ctx, row)
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

// watchSavedQueryRefs returns the saved-query names a watch references: the
// explicit saved_query_name and the operation name of its inline subscription
// query, both of which dev-mode allow-list saves register as saved_query
// artifacts during watch validation.
func watchSavedQueryRefs(row map[string]any) []string {
	var refs []string
	if name := strings.TrimSpace(stringMapValue(row, "saved_query_name")); name != "" {
		refs = append(refs, name)
	}
	if query := strings.TrimSpace(stringMapValue(row, "query")); query != "" {
		if header, err := core.Operation(query); err == nil {
			if name := strings.TrimSpace(header.Name); name != "" {
				refs = append(refs, name)
			}
		}
	}
	return refs
}

// watchRegisteredSavedQueryRefs returns only names registered by an inline
// watch query. saved_query_name references pre-existing user artifacts and must
// never become cascade-delete targets.
func watchRegisteredSavedQueryRefs(row map[string]any) []string {
	query := strings.TrimSpace(stringMapValue(row, "query"))
	if query == "" {
		return nil
	}
	header, err := core.Operation(query)
	if err != nil || strings.TrimSpace(header.Name) == "" {
		return nil
	}
	return []string{strings.TrimSpace(header.Name)}
}

// savedQueryNamesMatch tolerates a namespace qualifier on either side:
// "ns.orders" matches "orders", but "ns1.orders" does not match "ns2.orders".
func savedQueryNamesMatch(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	if strings.EqualFold(a, b) {
		return true
	}
	ans, abase := splitQualifiedArtifactName(a)
	bns, bbase := splitQualifiedArtifactName(b)
	return strings.EqualFold(abase, bbase) && (ans == "" || bns == "")
}

func savedQuerySubscriptionArtifact(row map[string]any) bool {
	return artifactKindMatches(row["kind"], artifactKindSavedQuery) &&
		savedQueryOperation(rowContent(row), metadataMap(row)) == "subscription"
}

func savedQueryWatchRegisteredArtifact(row map[string]any) bool {
	if !savedQuerySubscriptionArtifact(row) {
		return false
	}
	registered, _ := metadataMap(row)["watch_registered"].(bool)
	return registered
}

// orphanedSavedQueryArtifacts lists db-backed subscription saved-query
// artifacts whose name no existing watch references (by saved_query_name or
// inline query operation name). References are checked against every watch
// regardless of owner so a shared name is never cleaned up early; candidates
// are limited to rows the caller could delete.
func (s *graphjinService) orphanedSavedQueryArtifacts(
	ctx context.Context,
	watchRows []map[string]any,
	ownerID string,
	admin bool,
) ([]watchCleanupCandidate, error) {
	if _, _, _, ok := s.artifactDB(); !ok {
		return nil, nil
	}
	if err := s.checkArtifactKindWritable(artifactKindSavedQuery); err != nil {
		return nil, nil
	}
	refs := make([]string, 0, len(watchRows))
	for _, row := range watchRows {
		refs = append(refs, watchSavedQueryRefs(row)...)
	}
	artifactRows, err := s.internalStoreAllRows(ctx, "artifacts", "", artifactStoreFields, nil)
	if err != nil {
		return nil, err
	}
	var out []watchCleanupCandidate
	for _, row := range artifactRows {
		if !admin && stringMapValue(row, "owner_id") != ownerID {
			continue
		}
		if !savedQueryWatchRegisteredArtifact(row) {
			continue
		}
		name := stringMapValue(row, "name")
		referenced := false
		for _, ref := range refs {
			if savedQueryNamesMatch(name, ref) {
				referenced = true
				break
			}
		}
		if referenced {
			continue
		}
		out = append(out, watchCleanupCandidate{
			Kind:      "saved_query",
			ID:        stringMapValue(row, "id"),
			Name:      name,
			Reason:    watchCleanupReasonOrphanedSavedQueries,
			Action:    "delete_artifact",
			CreatedAt: stringMapValue(row, "created_at"),
			UpdatedAt: stringMapValue(row, "updated_at"),
			OwnerID:   safeArtifactIdentity(stringMapValue(row, "owner_id"), false),
		})
	}
	return out, nil
}

func (s *graphjinService) deleteSavedQueryArtifactRow(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return nil
	}
	_, err := s.internalStoreMutationRows(ctx, "artifacts", `where: { id: { eq: $id } }, delete: true`, `id`, map[string]any{"id": id})
	return err
}

// cleanupWatchSavedQueryArtifacts removes the saved-query artifacts that watch
// creation registered for the deleted watch's subscription, unless another
// watch still references the same query name. Non-subscription artifacts are
// never touched, and locked kinds are left for policy review.
func (s *graphjinService) cleanupWatchSavedQueryArtifacts(ctx context.Context, watchRow map[string]any) error {
	if watchRow == nil {
		return nil
	}
	refs := watchRegisteredSavedQueryRefs(watchRow)
	if len(refs) == 0 {
		return nil
	}
	if _, _, _, ok := s.artifactDB(); !ok {
		return nil
	}
	if err := s.checkArtifactKindWritable(artifactKindSavedQuery); err != nil {
		return nil
	}
	watchRows, err := s.internalStoreAllRows(ctx, "watches", "", watchStoreFields, nil)
	if err != nil {
		return err
	}
	deletedID := stringMapValue(watchRow, "id")
	var otherRefs []string
	for _, row := range watchRows {
		if stringMapValue(row, "id") == deletedID {
			continue
		}
		otherRefs = append(otherRefs, watchSavedQueryRefs(row)...)
	}
	var orphaned []string
	for _, ref := range refs {
		referenced := false
		for _, other := range otherRefs {
			if savedQueryNamesMatch(other, ref) {
				referenced = true
				break
			}
		}
		if !referenced {
			orphaned = append(orphaned, ref)
		}
	}
	if len(orphaned) == 0 {
		return nil
	}
	ownerID := stringMapValue(watchRow, "owner_id")
	if ownerID == "" {
		return nil
	}
	artifactRows, err := s.internalStoreAllRows(ctx, "artifacts", `where: { owner_id: { eq: $owner_id } }`, artifactStoreFields, map[string]any{"owner_id": ownerID})
	if err != nil {
		return err
	}
	deleted := false
	for _, row := range artifactRows {
		if !savedQuerySubscriptionArtifact(row) {
			continue
		}
		name := stringMapValue(row, "name")
		match := false
		for _, ref := range orphaned {
			if savedQueryNamesMatch(name, ref) {
				match = true
				break
			}
		}
		if !match {
			continue
		}
		if err := s.deleteSavedQueryArtifactRow(ctx, stringMapValue(row, "id")); err != nil {
			return err
		}
		deleted = true
	}
	if deleted {
		if err := s.bumpArtifactRevision(ctx, "artifacts"); err != nil {
			return err
		}
		s.markArtifactChanged("watch saved query cleanup")
	}
	return nil
}
