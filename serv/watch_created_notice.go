package serv

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	gjagent "github.com/dosco/graphjin/agent/v3"
)

// An agent run that creates a watch gets a watch_created notice naming what it
// set up, so a client can show the standing question as a card without parsing
// the agent's actions. The recorder rides on the run's context; gj_watch
// inserts made under it are collected.

type watchCreatedRecorderKey struct{}

type watchCreatedRecorder struct {
	mu  sync.Mutex
	ids []string
}

func withWatchCreatedRecorder(ctx context.Context) context.Context {
	if _, ok := ctx.Value(watchCreatedRecorderKey{}).(*watchCreatedRecorder); ok {
		return ctx
	}
	return context.WithValue(ctx, watchCreatedRecorderKey{}, &watchCreatedRecorder{})
}

func recordWatchCreated(ctx context.Context, id string) {
	if rec, ok := ctx.Value(watchCreatedRecorderKey{}).(*watchCreatedRecorder); ok {
		rec.mu.Lock()
		if !slices.Contains(rec.ids, id) {
			rec.ids = append(rec.ids, id)
		}
		rec.mu.Unlock()
	}
}

func watchesCreatedIn(ctx context.Context) []string {
	rec, ok := ctx.Value(watchCreatedRecorderKey{}).(*watchCreatedRecorder)
	if !ok {
		return nil
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return append([]string(nil), rec.ids...)
}

func (s *graphjinService) appendWatchCreatedNotices(ctx context.Context, resp *gjagent.Response) {
	for _, id := range watchesCreatedIn(ctx) {
		row, err := s.internalWatchStoreRow(ctx, id)
		if err != nil || row == nil {
			continue
		}
		resp.Notices = append(resp.Notices, gjagent.ResponseNotice{
			Kind:     "watch_created",
			Message:  watchCreatedMessage(row),
			WatchIDs: []string{id},
		})
	}
}

func watchCreatedMessage(row map[string]any) string {
	name := stringMapValue(row, "name")
	parts := []string{fmt.Sprintf("Watching %q", name)}
	if m, ok := parseWatchMetricJSON(jsonMapString(row, "metric_json")); ok {
		cond := m.Op
		if m.Threshold != nil {
			cond = fmt.Sprintf("%s %v", m.Op, m.Threshold)
		}
		parts = append(parts, fmt.Sprintf("%s %s", m.Path, cond))
		st := parseWatchMetricState(jsonMapString(row, "metric_state_json"))
		if st.HasValue {
			parts = append(parts, fmt.Sprintf("now %v", st.LastValue))
		}
	}
	if cron := stringMapValue(row, "schedule_cron"); cron != "" {
		parts = append(parts, "checked on cron "+cron)
	}
	if cfg, _, err := parseWatchDeliveryConfig(jsonMapString(row, "delivery_json")); err == nil && cfg.Kind != "" {
		parts = append(parts, "delivers to "+cfg.Kind)
	}
	return strings.Join(parts, " · ")
}
