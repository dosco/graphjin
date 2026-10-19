package serv

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// A watch with poll_json runs its query on a fixed interval instead of
// subscribing. It suits aggregates and API sources, which have no cursor to
// follow. The runner records an event each time the result changes.
const (
	watchPollMinInterval = 10 * time.Second
	watchPollMaxInterval = 24 * time.Hour
)

type watchPollConfig struct {
	Interval  time.Duration
	Variables map[string]watchPollVariable
}

// watchPollVariable is a variable that the runner computes on each poll, so a
// query over "the last hour" keeps moving.
type watchPollVariable struct {
	Kind    string `json:"kind"`
	Seconds int64  `json:"seconds,omitempty"`
}

func normalizeWatchPollJSON(raw string) (string, watchPollConfig, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" || raw == "{}" {
		return "", watchPollConfig{}, false, nil
	}
	var in struct {
		IntervalSeconds int64                        `json:"interval_seconds"`
		Variables       map[string]watchPollVariable `json:"variables"`
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return "", watchPollConfig{}, false, fmt.Errorf("poll_json is invalid: %w", err)
	}
	interval := time.Duration(in.IntervalSeconds) * time.Second
	if interval < watchPollMinInterval || interval > watchPollMaxInterval {
		return "", watchPollConfig{}, false, fmt.Errorf("poll_json interval_seconds must be between %d and %d",
			int64(watchPollMinInterval/time.Second), int64(watchPollMaxInterval/time.Second))
	}
	for name, v := range in.Variables {
		if strings.TrimSpace(name) == "" {
			return "", watchPollConfig{}, false, fmt.Errorf("poll_json variable names must not be empty")
		}
		switch v.Kind {
		case "now":
		case "seconds_ago":
			if v.Seconds <= 0 {
				return "", watchPollConfig{}, false, fmt.Errorf("poll_json variable %s needs seconds > 0", name)
			}
		default:
			return "", watchPollConfig{}, false, fmt.Errorf("poll_json variable %s has unsupported kind %q", name, v.Kind)
		}
	}
	cfg := watchPollConfig{Interval: interval, Variables: in.Variables}
	out := map[string]any{"interval_seconds": in.IntervalSeconds}
	if len(in.Variables) > 0 {
		out["variables"] = in.Variables
	}
	return mustMarshalString(out), cfg, true, nil
}

// pollVariables merges the computed variables into the watch's variables_json.
func (c watchPollConfig) pollVariables(variablesJSON string, now time.Time) (json.RawMessage, error) {
	vars := map[string]any{}
	if raw := strings.TrimSpace(variablesJSON); raw != "" && raw != "null" {
		if err := json.Unmarshal([]byte(raw), &vars); err != nil {
			return nil, fmt.Errorf("watch variables_json must be a JSON object: %w", err)
		}
	}
	for name, v := range c.Variables {
		switch v.Kind {
		case "now":
			vars[name] = now.UTC().Format(time.RFC3339)
		case "seconds_ago":
			vars[name] = now.Add(-time.Duration(v.Seconds) * time.Second).UTC().Format(time.RFC3339)
		}
	}
	data, err := json.Marshal(vars)
	return data, err
}

// watchPollTick numbers the poll interval that holds now. It goes into the
// event id, so a value that returns to an earlier result is a new event, while
// a retry of the same poll is not.
func watchPollTick(now time.Time, interval time.Duration) int64 {
	if interval <= 0 {
		return 0
	}
	return now.UnixNano() / int64(interval)
}

func (s *graphjinService) runWatchPoll(ctx context.Context, def watchRuntimeDefinition, cfg watchPollConfig) {
	current := def
	for {
		now := time.Now()
		err := s.pollWatchOnce(ctx, &current, cfg, now)
		if ctx.Err() != nil {
			return
		}
		wait := cfg.Interval - time.Since(now)
		if err != nil {
			backoff, terminal := s.updateWatchRunnerTransientError(ctx, current, err)
			if terminal {
				return
			}
			if backoff < wait {
				wait = backoff
			}
		}
		if !waitWatchRetry(ctx, wait) {
			return
		}
	}
}

func (s *graphjinService) pollWatchOnce(ctx context.Context, current *watchRuntimeDefinition, cfg watchPollConfig, now time.Time) error {
	if strings.TrimSpace(current.OwnerID) == "" {
		return fmt.Errorf("watch owner_id is missing")
	}
	vars, err := cfg.pollVariables(current.VariablesJSON, now)
	if err != nil {
		return err
	}
	ownerCtx := s.watchOwnerContext(ctx, *current)
	gj := s.gj
	if gj == nil {
		return fmt.Errorf("graphjin core is not initialized")
	}
	res, err := gj.GraphQL(ownerCtx, current.Query, vars, nil)
	if err != nil {
		return err
	}
	current.pollTick = watchPollTick(now, cfg.Interval)
	dataHash, _, err := s.persistWatchResult(ctx, current, res)
	if err != nil {
		return err
	}
	if dataHash != "" {
		current.LastDataHash = dataHash
	}
	return nil
}
