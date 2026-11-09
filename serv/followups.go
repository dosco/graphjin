package serv

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/dosco/graphjin/core/v3"
)

// Follow-ups are check-backs the lead schedules in the conversation it is in
// (the follow_up tool). Each is a one-time schedule watch whose agent delivery
// answers in that thread with the lead's note as the instruction; it is
// deleted once it has delivered. They count against watches.max_per_owner.

const (
	followUpWatchPrefix   = "followup_"
	followUpMaxPerThread  = 3
	followUpNoteMaxLength = watchRoutinePromptMaxBytes
)

var errFollowUpNoThread = errors.New("follow-ups need a conversation: this run has no thread")

// followUpScheduler schedules follow-ups as the caller.
type followUpScheduler struct {
	service *graphjinService
}

func (s *graphjinService) followUpsEnabled() bool {
	return s != nil && s.threadsEnabled() && s.watchesEnabled()
}

func followUpWatchName(threadID string, at time.Time, note string) string {
	return followUpWatchPrefix + threadID + "_" + hashString(at.Format(time.RFC3339)+"\x00"+note)[:10]
}

// followUpCron fires once a year at the minute of at, in UTC; the watch is
// deleted after its first delivery, so it fires once.
func followUpCron(at time.Time) string {
	at = at.UTC()
	return fmt.Sprintf("%d %d %d %d *", at.Minute(), at.Hour(), at.Day(), int(at.Month()))
}

func (f followUpScheduler) ScheduleFollowUp(ctx context.Context, at time.Time, note string) (gjagent.FollowUp, error) {
	s := f.service
	threadID := runThreadFrom(ctx).threadID
	if threadID == "" {
		return gjagent.FollowUp{}, errFollowUpNoThread
	}
	ownerID, ok := artifactUserID(ctx)
	if !ok {
		return gjagent.FollowUp{}, fmt.Errorf("follow-ups need a signed-in user")
	}
	note = strings.TrimSpace(note)
	if note == "" || len(note) > followUpNoteMaxLength {
		return gjagent.FollowUp{}, fmt.Errorf("a follow-up note must be 1 to %d bytes", followUpNoteMaxLength)
	}
	now := time.Now().UTC()
	// Fire on a whole minute, never before the time asked for.
	at = at.UTC().Truncate(time.Minute)
	if !at.After(now) {
		at = now.Truncate(time.Minute).Add(time.Minute)
	}
	if at.After(now.Add(time.Duration(gjagent.FollowUpMaxHours) * time.Hour)) {
		return gjagent.FollowUp{}, fmt.Errorf("a follow-up can be at most %d days ahead", int(gjagent.FollowUpMaxHours/24))
	}
	pending, err := s.internalStoreAllRows(ctx, "watches", `where: { owner_id: { eq: $owner_id }, name: { like: $prefix } }`, "id",
		map[string]any{"owner_id": ownerID, "prefix": followUpWatchPrefix + threadID + "_%"})
	if err != nil {
		return gjagent.FollowUp{}, err
	}
	if len(pending) >= followUpMaxPerThread {
		return gjagent.FollowUp{}, fmt.Errorf("this conversation already has %d follow-ups pending; that is the limit", followUpMaxPerThread)
	}
	row, err := newWatchControlPlane(s).mutateRow(ctx, core.ManagedMutationRoot{
		Table: watchesRootTable, Operation: "insert",
		Input: map[string]any{
			"name":        followUpWatchName(threadID, at, note),
			"description": "Follow-up the agent scheduled: " + truncateUTF8(note, 200),
			"schedule":    map[string]any{"cron": followUpCron(at), "timezone": "UTC"},
			"delivery_json": map[string]any{
				"kind": "agent", "prompt": note, "thread_id": threadID, "once": true,
			},
		},
	})
	if err != nil {
		return gjagent.FollowUp{}, err
	}
	return gjagent.FollowUp{ID: stringFromAny(row["id"]), At: at.Format(time.RFC3339)}, nil
}

// retireFollowUpWatch deletes a one-time follow-up watch once it has
// delivered. Its event stays, delivered and seen.
func (s *graphjinService) retireFollowUpWatch(ctx context.Context, watchID string) {
	ctx = context.WithoutCancel(ctx)
	if _, err := s.internalStoreMutationRows(ctx, "watches", `where: { id: { eq: $id } }, delete: true`, "id", map[string]any{"id": watchID}); err != nil {
		if s.log != nil {
			s.log.Warnf("follow-up watch %s: delete after delivery: %v", watchID, err)
		}
		return
	}
	_ = s.bumpArtifactRevision(ctx, "watches")
	s.markWatchChanged("follow-up delivered")
}
