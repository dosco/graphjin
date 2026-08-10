package serv

import (
	"context"

	gjagent "github.com/dosco/graphjin/agent/v3"
)

// agentCapabilityProfile builds the caller-scoped capability profile and maps it onto
// the agent package's opaque CapabilityProfile. It reuses the single MCP profile builder
// (callerCapabilityProfile) via the same lightweight mcpServer wrapper used by
// mcpStartupCapabilityProfile, so the REST and MCP agent paths derive identical
// capabilities from one source of truth.
//
// Invariant: only the fixed gj_* system roots are carried across (as names). Application/
// database roots are never enumerated here — they stay behind the catalog and progressive
// discovery, and their authorization remains core RLS per-table at execution.
func (s *graphjinService) agentCapabilityProfile(ctx context.Context) *gjagent.CapabilityProfile {
	if s == nil {
		return nil
	}
	ms := &mcpServer{service: s, ctx: ctx}
	mp := ms.callerCapabilityProfile(ctx, false)
	if mp == nil {
		return nil
	}
	return &gjagent.CapabilityProfile{
		RoleClass:             mp.RoleClass,
		Authenticated:         mp.Authenticated,
		Mode:                  mp.Mode,
		CatalogRevision:       mp.CatalogRevision,
		AvailableTools:        mp.AvailableTools,
		AllowedActions:        mp.AllowedActions,
		AvailableSystemRoots:  systemRootNames(mp.AvailableRoots),
		BlockedSystemRoots:    systemRootNames(mp.BlockedRoots),
		RecommendedEntrypoint: mp.RecommendedEntrypoint,
		SafetyNotes:           mp.SafetyNotes,
	}
}

// systemRootNames extracts root names from MCP root profiles. The MCP profile only ever
// lists the fixed gj_* system roots (see mcpGraphJinRoots / graphJinRootProfiles), so the
// result is bounded and never contains application roots.
func systemRootNames(roots []MCPRootProfile) []string {
	if len(roots) == 0 {
		return nil
	}
	out := make([]string, 0, len(roots))
	for _, r := range roots {
		if r.Root != "" {
			out = append(out, r.Root)
		}
	}
	return out
}
