package serv

import (
	"context"
	"strings"

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
	roots := systemRootNames(mp.AvailableRoots)
	actions := mp.AllowedActions
	return &gjagent.CapabilityProfile{
		RoleClass:             mp.RoleClass,
		Authenticated:         mp.Authenticated,
		Mode:                  mp.Mode,
		CatalogRevision:       mp.CatalogRevision,
		AvailableTools:        mp.AvailableTools,
		AllowedActions:        actions,
		AvailableSystemRoots:  roots,
		BlockedSystemRoots:    systemRootNames(mp.BlockedRoots),
		RecommendedEntrypoint: mp.RecommendedEntrypoint,
		SafetyNotes:           mp.SafetyNotes,
		APIRoots:              s.apiRootActions(),
	}
}

// apiRootActions maps each active API operation's mutation root to the action
// it needs: api.delete for DELETE, api.write for other writing methods. The
// agent classifies a write by its roots, and only the service knows which
// roots are API calls.
func (s *graphjinService) apiRootActions() map[string]string {
	if s == nil || s.gj == nil {
		return nil
	}
	md, err := s.gj.MetadataSnapshot(s.metadataSnapshotExcludes()...)
	if err != nil || md == nil || len(md.APIOperations) == 0 {
		return nil
	}
	out := map[string]string{}
	for _, op := range md.APIOperations {
		if !op.Active || op.RootName == "" {
			continue
		}
		switch strings.ToUpper(op.Method) {
		case "DELETE":
			out[strings.ToLower(op.RootName)] = gjagent.CapabilityActionAPIDelete
		case "POST", "PUT", "PATCH":
			out[strings.ToLower(op.RootName)] = gjagent.CapabilityActionAPIWrite
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
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
