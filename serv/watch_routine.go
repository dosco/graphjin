package serv

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
)

// A routine is a watch whose delivery is an agent turn: when the watch fires,
// GraphJin asks the agent the stored prompt as the watch owner, read-only, in
// a thread, and the answer lands there like any other turn. It runs through
// the same thread run path as chat, so it gets the run lease, cancel, resume
// and the lost-run sweep.

const (
	watchRoutinePromptMaxBytes = 2048
	watchRoutineBusyWait       = 2 * time.Minute
	watchRoutineDefaultPrompt  = "This watch just fired. Explain what changed, why it matters, and what to look at next, quoting the numbers you find."
)

type watchAgentConfig struct {
	Prompt   string
	ThreadID string
}

func parseWatchAgentConfig(m map[string]any) (watchAgentConfig, error) {
	src := m
	if nested := mapFromAny(m["agent"]); nested != nil {
		src = nested
	}
	cfg := watchAgentConfig{
		Prompt:   strings.TrimSpace(stringFromAny(src["prompt"])),
		ThreadID: strings.TrimSpace(stringFromAny(src["thread_id"])),
	}
	if cfg.Prompt == "" {
		cfg.Prompt = watchRoutineDefaultPrompt
	}
	if len(cfg.Prompt) > watchRoutinePromptMaxBytes {
		return cfg, fmt.Errorf("agent delivery prompt is longer than %d bytes", watchRoutinePromptMaxBytes)
	}
	return cfg, nil
}

// watchRoutineThreadID is the thread a watch's routine answers in when none is
// named. It is fixed per owner and watch, so concurrent deliveries share it.
func watchRoutineThreadID(ownerID, watchID string) string {
	return "th_w" + hashString(ownerID + ":routine:" + watchID)[:24]
}

// watchRoutineRunID is fixed per event, so a delivery that is attempted again
// cannot start a second run.
func watchRoutineRunID(eventID string) string {
	return "run_w" + hashString("routine:" + eventID)[:24]
}

func (s *graphjinService) watchRoutineThread(ownerCtx context.Context, def watchRuntimeDefinition, cfg watchAgentConfig) (string, error) {
	if cfg.ThreadID != "" {
		if _, err := s.ownedThreadStoreRow(ownerCtx, cfg.ThreadID); err != nil {
			return "", fmt.Errorf("agent delivery thread %s: %w", cfg.ThreadID, err)
		}
		return cfg.ThreadID, nil
	}
	id := watchRoutineThreadID(def.OwnerID, def.ID)
	if row, err := s.threadStoreRow(ownerCtx, id); err != nil {
		return "", err
	} else if row != nil {
		return id, nil
	}
	_, err := s.createThread(ownerCtx, threadSpec{ID: id, Title: "Watch: " + def.Name, Channel: "watch", ChannelRef: def.ID})
	if err != nil {
		// Another delivery may have created it first.
		if row, _ := s.threadStoreRow(ownerCtx, id); row != nil {
			return id, nil
		}
		return "", err
	}
	return id, nil
}

func (s *graphjinService) deliverWatchAgent(ctx context.Context, def watchRuntimeDefinition, event watchDeliveryEvent, cfg watchAgentConfig) (map[string]any, error) {
	receipt := map[string]any{"kind": "agent", "status": "failed"}
	if !s.threadsEnabled() {
		return receipt, errors.New("agent delivery needs agent threads; enable threads in the agent config")
	}
	ownerCtx := s.watchOwnerContext(ctx, def)
	threadID, err := s.watchRoutineThread(ownerCtx, def, cfg)
	if err != nil {
		return receipt, err
	}
	runID := watchRoutineRunID(event.ID)
	receipt["thread_id"], receipt["run_id"] = threadID, runID

	data := parseJSONValue(event.DataJSON)
	req := gjagent.Request{
		Instruction: watchRoutineInstruction(def, cfg.Prompt, mapFromAny(data)),
		Context: map[string]any{
			"_watch_events": []any{map[string]any{"id": event.ID, "watch_id": def.ID, "data_json": data}},
			"watch":         map[string]any{"id": def.ID, "name": def.Name},
		},
		Capabilities: s.agentCapabilityProfile(ownerCtx),
	}
	opts := threadRunOptions{RunID: runID, Display: watchRoutineDisplay(def, mapFromAny(data)) + "\n" + cfg.Prompt}
	run, err := s.prepareWatchRoutineRun(ownerCtx, threadID, &req, opts)
	if err != nil {
		return receipt, err
	}

	agentConf := agentConfigFromService(s.conf)
	agentConf.ReadOnly = true
	runner, err := newGraphJinAgentRunner(s, agentConf)
	if err != nil {
		// The run row exists; record the failure in the thread rather than
		// leaving it for the lease sweep.
		run.execute(ownerCtx, failingAgentRunner{err: err}, req, s.watchRoutineSettle(req), nil) //nolint:errcheck
		return receipt, err
	}
	resp, err := run.execute(ownerCtx, s.withMemoryRecall(runner), req, s.watchRoutineSettle(req), nil)
	receipt["answer_status"] = resp.Status
	if err != nil {
		return receipt, err
	}
	receipt["status"] = "delivered"
	receipt["delivered_at"] = time.Now().UTC().Format(time.RFC3339)
	return receipt, nil
}

// prepareWatchRoutineRun waits for a thread that is busy with another turn,
// such as the owner asking a follow-up, instead of dropping the routine.
func (s *graphjinService) prepareWatchRoutineRun(ctx context.Context, threadID string, req *gjagent.Request, opts threadRunOptions) (*threadRun, error) {
	deadline := time.Now().Add(watchRoutineBusyWait)
	wait := time.Second
	for {
		run, err := s.prepareThreadRunWith(ctx, threadID, req, opts)
		if !errors.Is(err, errThreadBusy) || time.Now().After(deadline) {
			return run, err
		}
		if err := sleepContext(ctx, wait); err != nil {
			return nil, err
		}
		if wait < 8*time.Second {
			wait *= 2
		}
	}
}

func (s *graphjinService) watchRoutineSettle(req gjagent.Request) func(context.Context, gjagent.Response, error) (gjagent.Response, error) {
	return func(ctx context.Context, resp gjagent.Response, err error) (gjagent.Response, error) {
		return s.settleAgentResponse(ctx, req, taskWarmStart{}, resp, err)
	}
}

type failingAgentRunner struct{ err error }

func (r failingAgentRunner) Run(context.Context, gjagent.Request) (gjagent.Response, error) {
	return gjagent.Response{}, r.err
}

// watchRoutineInstruction is the prompt plus a plain-language brief of the
// event. It names no field paths or watch ids: the agent repeats what it is
// given, and an answer citing a field the run never observed is rejected, so a
// brief like "orders.0.count_id is 3" made routines exhaust their steps. The
// full event rides in the run context (_watch_events).
func watchRoutineInstruction(def watchRuntimeDefinition, prompt string, data map[string]any) string {
	return prompt + "\n\n" + watchRoutineBrief(data)
}

var watchRoutineOpWords = map[string]string{
	"gt": "above", "gte": "at or above", "lt": "below", "lte": "at or below", "eq": "at", "ne": "other than",
}

func watchRoutineBrief(data map[string]any) string {
	switch stringFromAny(data["kind"]) {
	case "metric":
		m := mapFromAny(data["metric"])
		op := stringFromAny(m["op"])
		rule := ""
		switch op {
		case "changed":
			rule = "it fires when the number changes"
		case "change_pct":
			rule = fmt.Sprintf("it fires when the number moves by %v%% or more", m["threshold"])
		default:
			rule = fmt.Sprintf("it fires when the number is %s %v", watchRoutineOpWords[op], m["threshold"])
		}
		if crossings, ok := data["crossings"].([]any); ok {
			parts := make([]string, 0, len(crossings))
			for _, c := range crossings {
				cm := mapFromAny(c)
				parts = append(parts, fmt.Sprintf("%v (%v)", cm["key"], cm["value"]))
			}
			return fmt.Sprintf("This was triggered by a watch firing for %s; %s for a group.", strings.Join(parts, ", "), rule)
		}
		line := fmt.Sprintf("This was triggered by a watch firing: the number it watches is now %v", data["value"])
		if prev, ok := data["previous"]; ok {
			line += fmt.Sprintf(", it was %v", prev)
		}
		return line + "; " + rule + "."
	case "schedule":
		return "This was triggered by a scheduled check."
	case "absence":
		return "This was triggered because a watch saw no new data in its window."
	}
	return "This was triggered by a watch firing."
}

// watchRoutineDisplay is the one line the thread shows for the event.
func watchRoutineDisplay(def watchRuntimeDefinition, data map[string]any) string {
	switch stringFromAny(data["kind"]) {
	case "metric":
		m := mapFromAny(data["metric"])
		if crossings, ok := data["crossings"].([]any); ok {
			keys := make([]string, 0, len(crossings))
			for _, c := range crossings {
				keys = append(keys, fmt.Sprint(mapFromAny(c)["key"]))
			}
			return fmt.Sprintf("Watch %q fired for %s (%s %v).", def.Name, strings.Join(keys, ", "), stringFromAny(m["op"]), m["threshold"])
		}
		line := fmt.Sprintf("Watch %q fired: %s is %v", def.Name, stringFromAny(m["path"]), data["value"])
		if prev, ok := data["previous"]; ok {
			line += fmt.Sprintf(" (was %v)", prev)
		}
		if t, ok := m["threshold"]; ok {
			line += fmt.Sprintf(", %s %v", stringFromAny(m["op"]), t)
		}
		return line + "."
	case "schedule":
		return fmt.Sprintf("Scheduled check %q (%s).", def.Name, stringFromAny(data["scheduled_for_local"]))
	case "absence":
		return fmt.Sprintf("Watch %q saw no data in its window.", def.Name)
	}
	return fmt.Sprintf("Watch %q fired.", def.Name)
}

// watchRoutineThreadProjection names the thread an agent-delivery watch
// answers in, so clients can link to it before the first firing.
func watchRoutineThreadProjection(row map[string]any) string {
	cfg, _, err := parseWatchDeliveryConfig(jsonMapString(row, "delivery_json"))
	if err != nil || cfg.Kind != "agent" {
		return ""
	}
	if cfg.Agent.ThreadID != "" {
		return cfg.Agent.ThreadID
	}
	return watchRoutineThreadID(stringMapValue(row, "owner_id"), stringMapValue(row, "id"))
}
