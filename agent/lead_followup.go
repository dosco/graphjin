package agent

import (
	"context"
	"errors"
	"strings"
	"time"

	ax "github.com/ax-llm/ax/packages/go"
)

// Follow-ups let work outlive one turn. The lead schedules a check-back in the
// conversation it is in; when the time comes, GraphJin starts a turn there
// with the lead's note as its instruction and the conversation as history.
// The service decides where and how (FollowUpScheduler); the lead only names
// when and why.

const (
	toolFollowUp = "follow_up"

	// FollowUpMinHours and FollowUpMaxHours bound how far ahead a follow-up
	// may be scheduled.
	FollowUpMinHours = 1.0 / 60
	FollowUpMaxHours = 30 * 24
)

// FollowUp is a scheduled check-back.
type FollowUp struct {
	ID string `json:"id"`
	At string `json:"at"`
}

// FollowUpScheduler schedules a follow-up turn in the conversation the
// context belongs to. It returns an error when there is no conversation or
// the caller has too many pending.
type FollowUpScheduler interface {
	ScheduleFollowUp(ctx context.Context, at time.Time, note string) (FollowUp, error)
}

func (r *leadRun) followUpTool(ctx context.Context) ax.Tool {
	tool := ax.Fn(toolFollowUp).WithHandler(func(args map[string]ax.Value) (ax.Value, error) {
		note := strings.TrimSpace(stringValue(normalizeValue(args["note"])))
		if note == "" {
			return nil, errors.New("note must say what to check when the follow-up runs")
		}
		hours, ok := numberValue(normalizeValue(args["in_hours"]))
		if !ok || hours < FollowUpMinHours || hours > FollowUpMaxHours {
			return nil, errors.New("in_hours must be a number from 0.02 (about a minute) to 720 (30 days)")
		}
		key := leadQuestionKey(note)
		r.mu.Lock()
		prior, scheduled := r.followUps[key]
		r.mu.Unlock()
		if scheduled {
			return map[string]any{"scheduled": true, "id": prior.ID, "at": prior.At, "already_scheduled": true}, nil
		}
		start := r.emit(ActionEvent{Source: "lead", Tool: toolFollowUp, Status: "running", Args: map[string]any{"in_hours": hours, "note": note}})
		at := r.lead.now().UTC().Add(time.Duration(hours * float64(time.Hour)))
		followUp, err := r.lead.FollowUps.ScheduleFollowUp(ctx, at, note)
		finish := ActionEvent{Source: "lead", Tool: toolFollowUp, Status: StatusAnswered, ElapsedMS: time.Since(start).Milliseconds()}
		if err != nil {
			finish.Status, finish.Error = StatusError, err.Error()
			r.emit(finish)
			return nil, err
		}
		r.emit(finish)
		r.mu.Lock()
		r.followUps[key] = followUp
		r.mu.Unlock()
		return map[string]any{"scheduled": true, "id": followUp.ID, "at": followUp.At}, nil
	})
	tool.Description = "Come back to this conversation later: after a change has had time to take effect, before a deadline, or to re-check a number. At that time a new turn starts here with your note as its instruction. Use it only when checking later serves the user; say in your reply when you will check back."
	tool.Args = map[string]ax.Field{
		"in_hours": {Name: "in_hours", Type: ax.FieldType{Name: "number"}, Description: "How many hours from now, up to 720 (30 days)."},
		"note":     {Name: "note", Type: ax.FieldType{Name: "string"}, Description: "What to check then, as a self-contained instruction naming the accounts, ids and figures involved."},
	}
	return tool
}

func numberValue(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}
