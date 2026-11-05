package serv

import (
	"context"
	"sort"

	"github.com/dosco/graphjin/core/v3"
)

// Test helpers that read conversations, memories and profiles the way a GUI
// does: through the gj_thread and gj_artifacts roots, as the caller.

func (s *graphjinService) threadMessages(ctx context.Context, threadID string) ([]map[string]any, error) {
	return newThreadControlPlane(s).queryRows(ctx, core.ManagedQueryRoot{
		Table:   threadMessagesRootTable,
		Where:   map[string]any{"thread_id": map[string]any{"eq": threadID}},
		OrderBy: []core.ManagedOrderBy{{Column: "seq"}},
	})
}

func (s *graphjinService) listThreads(ctx context.Context, includeArchived bool) ([]map[string]any, error) {
	rows, err := newThreadControlPlane(s).queryRows(ctx, core.ManagedQueryRoot{Table: threadsRootTable})
	if err != nil {
		return nil, err
	}
	out := rows[:0]
	for _, row := range rows {
		if includeArchived || stringMapValue(row, "status") == "open" {
			out = append(out, row)
		}
	}
	key := func(row map[string]any) string {
		if v := stringMapValue(row, "last_message_at"); v != "" {
			return v
		}
		return stringMapValue(row, "created_at")
	}
	sort.SliceStable(out, func(i, j int) bool { return key(out[i]) > key(out[j]) })
	return out, nil
}

func (s *graphjinService) visibleMemoryRows(ctx context.Context) ([]map[string]any, error) {
	return graphLeadStore{s: s}.artifacts(ctx, artifactKindMemory)
}

func (s *graphjinService) visibleProfileRows(ctx context.Context) ([]map[string]any, error) {
	return graphLeadStore{s: s}.artifacts(ctx, artifactKindAgentProfile)
}

func (s *graphjinService) recallMemories(ctx context.Context, query string) ([]map[string]any, error) {
	rows, err := s.visibleMemoryRows(ctx)
	if err != nil {
		return nil, err
	}
	return s.rankMemoryRows(ctx, rows, query, s.conf.Memory.effectiveRecallLimit())
}
