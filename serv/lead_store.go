package serv

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/dosco/graphjin/core/v3"
)

// graphLeadStore is the lead's view of the caller's skills and memories. It
// reads and writes them through the graph, as the caller: the same
// gj_artifacts queries and mutations a GUI would send, under the same
// identity, tiers, caps and policy.
type graphLeadStore struct {
	s *graphjinService
}

// leadStore returns the store for a lead run, or nil when the deployment
// keeps neither skills nor memories.
func (s *graphjinService) leadStore() gjagent.LeadStore {
	if !s.skillsEnabled() && !s.memoryEnabled() {
		return nil
	}
	return graphLeadStore{s: s}
}

// maxLeadArtifactRows bounds one listing; owners are capped well below it.
const maxLeadArtifactRows = 500

func (st graphLeadStore) Skills(ctx context.Context) ([]gjagent.SkillDoc, error) {
	if !st.s.skillsEnabled() {
		return nil, nil
	}
	rows, err := st.artifacts(ctx, artifactKindSkill)
	if err != nil {
		return nil, err
	}
	out := make([]gjagent.SkillDoc, 0, len(rows))
	for _, row := range rows {
		out = append(out, gjagent.SkillDoc{
			ID:          stringMapValue(row, "id"),
			Name:        stringMapValue(row, "name"),
			Description: stringMapValue(row, "description"),
			Content:     stringMapValue(row, "content"),
		})
	}
	return out, nil
}

func (st graphLeadStore) RecallMemories(ctx context.Context, query string, limit int) ([]gjagent.MemoryDoc, error) {
	if !st.s.memoryEnabled() {
		return nil, nil
	}
	rows, err := st.artifacts(ctx, artifactKindMemory)
	if err != nil {
		return nil, err
	}
	var ranked []map[string]any
	if strings.TrimSpace(query) == "" {
		ranked = standingMemoryRows(rows, limit)
	} else if ranked, err = st.s.rankMemoryRows(ctx, rows, query, limit); err != nil {
		return nil, err
	}
	out := make([]gjagent.MemoryDoc, 0, len(ranked))
	for _, row := range ranked {
		out = append(out, gjagent.MemoryDoc{ID: stringMapValue(row, "id"), Content: stringMapValue(row, "content")})
	}
	return out, nil
}

func (st graphLeadStore) Remember(ctx context.Context, content string) (gjagent.MemoryDoc, error) {
	if !st.s.memoryEnabled() {
		return gjagent.MemoryDoc{}, fmt.Errorf("agent memory is disabled: set memory.enabled")
	}
	var out struct {
		Row json.RawMessage `json:"gj_artifacts"`
	}
	err := st.graphql(ctx, `mutation lead_remember($content: String!) {
		gj_artifacts(insert: { kind: "memory", content: $content }) { id content }
	}`, map[string]any{"content": content}, &out)
	if err != nil {
		return gjagent.MemoryDoc{}, err
	}
	// A single-row insert returns an object; tolerate a one-row list too.
	var row map[string]any
	if json.Unmarshal(out.Row, &row) != nil {
		var rows []map[string]any
		if json.Unmarshal(out.Row, &rows) == nil && len(rows) != 0 {
			row = rows[0]
		}
	}
	if stringMapValue(row, "id") == "" {
		return gjagent.MemoryDoc{}, fmt.Errorf("memory was not saved")
	}
	return gjagent.MemoryDoc{ID: stringMapValue(row, "id"), Content: stringMapValue(row, "content")}, nil
}

// standingMemoryRows is the caller's most recently updated memories within
// the recall byte budget.
func standingMemoryRows(rows []map[string]any, limit int) []map[string]any {
	sorted := append([]map[string]any(nil), rows...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return stringMapValue(sorted[i], "updated_at") > stringMapValue(sorted[j], "updated_at")
	})
	out := make([]map[string]any, 0, limit)
	used := 0
	for _, row := range sorted {
		if len(out) == limit {
			break
		}
		content := stringMapValue(row, "content")
		if used+len(content) > maxRecalledMemoryBytes {
			continue
		}
		used += len(content)
		out = append(out, row)
	}
	return out
}

// artifacts lists the caller's visible rows of one kind.
func (st graphLeadStore) artifacts(ctx context.Context, kind string) ([]map[string]any, error) {
	var out struct {
		Rows []map[string]any `json:"gj_artifacts"`
	}
	err := st.graphql(ctx, fmt.Sprintf(`query lead_artifacts($kind: String!) {
		gj_artifacts(where: { kind: { eq: $kind } }, order_by: { name: asc }, limit: %d) {
			id name kind description content tier updated_at
		}
	}`, maxLeadArtifactRows), map[string]any{"kind": kind}, &out)
	return out.Rows, err
}

func (st graphLeadStore) graphql(ctx context.Context, query string, vars map[string]any, out any) error {
	if _, ok := artifactUserID(ctx); !ok {
		// Skills and memories are a signed-in caller's own; an anonymous
		// caller has none, which is not an error.
		return nil
	}
	ctx = st.s.applyIdentityContext(ctx)
	raw, err := json.Marshal(vars)
	if err != nil {
		return err
	}
	res, err := st.s.gj.GraphQL(ctx, query, raw, &core.RequestConfig{})
	if err != nil {
		return err
	}
	if len(res.Errors) != 0 {
		return fmt.Errorf("%s", catalogGraphQLErrors(res.Errors))
	}
	return json.Unmarshal(res.Data, out)
}
