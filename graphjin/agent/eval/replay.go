package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	gjagent "github.com/dosco/graphjin/agent/v3"
)

// ReplayFixture is a stored trajectory reduced to the parts needed to re-run it
// against current GraphJin code: the task it was attempting and the exact
// executor programs the model emitted.
//
// Replay is a mechanism check, not a score oracle. The canned programs cannot
// adapt to new guidance, so a replayed episode is not expected to pass; what it
// proves is which guards, recovery codes, and continuations the current code
// produces for a trajectory that previously failed.
//
// Fixtures deliberately carry programs only — no prompts beyond the task the
// suite already publishes, no returned rows, and no model answers — so they can
// be committed without widening what leaves the machine.
type ReplayFixture struct {
	RunID    string     `json:"run_id"`
	TaskID   string     `json:"task_id"`
	TaskSlug string     `json:"task_slug"`
	Repeat   int        `json:"repeat"`
	Task     Task       `json:"task"`
	Programs []string   `json:"programs"`
	Observed ReplaySeen `json:"observed"`
}

// ReplaySeen records what the original run produced, so a replay can be
// compared against the trajectory it came from.
type ReplaySeen struct {
	Status            string   `json:"status,omitempty"`
	FailureCategory   string   `json:"failure_category,omitempty"`
	ActorTurns        int64    `json:"actor_turns,omitempty"`
	ViolationCodes    []string `json:"violation_codes,omitempty"`
	ForbiddenAttempts []string `json:"forbidden_attempts,omitempty"`
	Pass              bool     `json:"pass"`
}

// LoadReplayFixture reduces one stored episode file to a replay fixture.
func LoadReplayFixture(path string) (ReplayFixture, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ReplayFixture{}, err
	}
	var episode Episode
	if err := json.Unmarshal(data, &episode); err != nil {
		return ReplayFixture{}, fmt.Errorf("decode episode %s: %w", path, err)
	}
	programs := executorPrograms(episode.Response)
	if len(programs) == 0 {
		return ReplayFixture{}, fmt.Errorf("episode %s has no executor programs to replay", path)
	}
	response := toMap(episode.Response)
	fixture := ReplayFixture{
		RunID:    episode.RunID,
		TaskID:   episode.TaskID,
		TaskSlug: episode.TaskSlug,
		Repeat:   episode.Repeat,
		Task:     episode.Task,
		Programs: programs,
		Observed: ReplaySeen{
			Status:            valueString(response["status"]),
			FailureCategory:   episode.Score.FailureCategory,
			ActorTurns:        episode.Score.ActorTurns,
			ViolationCodes:    append([]string(nil), episode.Score.ViolationCodes...),
			ForbiddenAttempts: append([]string(nil), episode.Score.ForbiddenAttempts...),
			Pass:              episode.Score.Pass,
		},
	}
	return fixture, nil
}

// executorPrograms pulls the ordered JavaScript the executor stage emitted from
// a stored trace. Distiller and responder stages are skipped: the executor
// programs are what exercise the protocol guards.
func executorPrograms(response any) []string {
	trace := toMap(toMap(response)["trace"])
	entries, _ := normalizeJSON(trace["chat_log"]).([]any)
	programs := make([]string, 0, len(entries))
	for _, entry := range entries {
		item := toMap(entry)
		if !strings.EqualFold(valueString(item["name"]), "executor") {
			continue
		}
		code := programFromCompletion(chatCompletionContent(item))
		if code == "" {
			continue
		}
		programs = append(programs, code)
	}
	return programs
}

// programFromCompletion extracts javascriptCode from an executor completion.
// Completions are JSON objects; a raw program is accepted as a fallback so a
// provider that returns bare code still replays.
func programFromCompletion(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	var decoded map[string]any
	if json.Unmarshal([]byte(content), &decoded) == nil {
		if code := strings.TrimSpace(valueString(decoded["javascriptCode"])); code != "" {
			return code
		}
		return ""
	}
	if strings.Contains(content, "await ") || strings.Contains(content, "function ") {
		return content
	}
	return ""
}

// ReplayObservation is what a replay run produced under current code.
type ReplayObservation struct {
	Status             string   `json:"status"`
	ActorTurns         int64    `json:"actor_turns"`
	ViolationCodes     []string `json:"violation_codes,omitempty"`
	RecoveryCodes      []string `json:"recovery_codes,omitempty"`
	ErrorCodes         []string `json:"error_codes,omitempty"`
	ForbiddenAttempts  []string `json:"forbidden_attempts,omitempty"`
	GuardInterventions int      `json:"guard_interventions"`
}

// ObserveReplay summarizes an agent response for mechanism assertions. It reads
// the same action summaries the scorer consumes, so a replay assertion and a
// benchmark verdict cannot drift apart.
func ObserveReplay(response any, score ScoreDetail) ReplayObservation {
	mapped := toMap(response)
	observation := ReplayObservation{
		Status:             valueString(mapped["status"]),
		ActorTurns:         score.ActorTurns,
		ViolationCodes:     append([]string(nil), score.ViolationCodes...),
		ForbiddenAttempts:  append([]string(nil), score.ForbiddenAttempts...),
		GuardInterventions: score.GuardInterventions,
	}
	recovery := map[string]struct{}{}
	errors := map[string]struct{}{}
	for _, action := range toSlice(mapped["actions"]) {
		summary := toMap(toMap(action)["summary"])
		for _, code := range toSlice(summary["recovery_codes"]) {
			if value := valueString(code); value != "" {
				recovery[value] = struct{}{}
			}
		}
		for _, code := range toSlice(summary["error_codes"]) {
			if value := valueString(code); value != "" {
				errors[value] = struct{}{}
			}
		}
	}
	observation.RecoveryCodes = sortedKeys(recovery)
	observation.ErrorCodes = sortedKeys(errors)
	return observation
}

// HasCode reports whether a replay observation surfaced the given violation,
// recovery, or error code.
func (o ReplayObservation) HasCode(code string) bool {
	for _, set := range [][]string{o.ViolationCodes, o.RecoveryCodes, o.ErrorCodes} {
		for _, value := range set {
			if strings.EqualFold(value, code) {
				return true
			}
		}
	}
	return false
}

func sortedKeys(values map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

// PostAgentForReplay exposes the benchmark's agent POST path so replay harnesses
// drive the exact same endpoint and payload shape as a scored run.
func PostAgentForReplay(ctx context.Context, client HTTPDoer, baseURL string, headers map[string]string, prompt string, turns []TurnSpec) (gjagent.Response, int, int64, error) {
	return postAgent(ctx, client, baseURL, headers, prompt, turns)
}

// ScoreForReplay scores a replayed response with the production scorer so replay
// assertions and benchmark verdicts cannot drift apart. Oracles are not resolved:
// replay checks mechanism, not ground truth.
func ScoreForReplay(task Task, response gjagent.Response) ScoreDetail {
	return Score(task, nil, response, 0)
}
