package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	ax "github.com/ax-llm/ax/packages/go"
)

type recordingFollowUps struct {
	at   []time.Time
	note []string
}

func (r *recordingFollowUps) ScheduleFollowUp(_ context.Context, at time.Time, note string) (FollowUp, error) {
	r.at = append(r.at, at)
	r.note = append(r.note, note)
	return FollowUp{ID: "watch:f1", At: at.Format(time.RFC3339)}, nil
}

func TestFollowUpToolSchedulesOncePerNote(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	scheduler := &recordingFollowUps{}
	lead := &Lead{FollowUps: scheduler, now: func() time.Time { return now }}
	run := &leadRun{lead: lead, followUps: map[string]FollowUp{}}
	tool := run.followUpTool(context.Background())

	out, err := tool.Handler(map[string]ax.Value{"in_hours": float64(24), "note": "Check whether invoice 42 was paid."})
	if err != nil {
		t.Fatal(err)
	}
	if got := normalizeValue(out).(map[string]any); got["at"] != "2026-11-10T12:00:00Z" || len(scheduler.at) != 1 {
		t.Fatalf("follow_up = %v scheduled %v", got, scheduler.at)
	}
	// The lead's code can run again in a later stage: the same note is one
	// follow-up.
	if again, _ := tool.Handler(map[string]ax.Value{"in_hours": float64(24), "note": "Check whether invoice 42 was paid."}); normalizeValue(again).(map[string]any)["already_scheduled"] != true || len(scheduler.at) != 1 {
		t.Fatalf("repeat scheduled again: %v", scheduler.at)
	}
	for _, args := range []map[string]ax.Value{
		{"in_hours": float64(24)},
		{"in_hours": float64(800), "note": "too far"},
		{"in_hours": "soon", "note": "not a number"},
	} {
		if _, err := tool.Handler(args); err == nil || !(strings.Contains(err.Error(), "note") || strings.Contains(err.Error(), "in_hours")) {
			t.Fatalf("args %v err = %v", args, err)
		}
	}
}
