package serv

import (
	"context"
	"sort"
	"strings"

	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/dosco/graphjin/core/v3/sourcecap"
)

// callerAllowedActions is the bounded mutating half of the caller capability
// profile. It is advisory/preflight truth for the agent; core role/RLS checks
// remain the execution-time authority for every concrete table and row.
func (ms *mcpServer) callerAllowedActions(ctx context.Context) []string {
	conf := ms.config()
	if conf == nil || conf.Agent.ReadOnly || !ms.toolAvailableForContext(ctx, "execute_graphql") {
		return nil
	}
	ctx = ms.effectiveIdentityContext(ctx)
	role := runtimeRoleClass(ctx)
	authenticated := mcpContextAuthenticated(ctx, role)
	actions := make(map[string]bool)

	if !conf.Core.IsSourcesUsed() {
		// Legacy mode has no bounded source access profile. Preserve its existing
		// coarse write posture and leave concrete authorization to core.
		actions[gjagent.CapabilityActionDataInsert] = true
		actions[gjagent.CapabilityActionDataUpdate] = true
		actions[gjagent.CapabilityActionDataDelete] = true
	} else {
		admins := conf.Core.EffectiveIdentityConfig().AdminRoles
		for _, source := range conf.Core.Sources {
			kind := source.CanonicalKind()
			switch kind {
			case sourcecap.KindDatabase:
				writeEnabled, _ := conf.sourceCapabilityForSource(source, sourcecap.KeyDataWrite)
				if !writeEnabled || source.ReadOnly {
					continue
				}
				access := conf.Core.EffectiveSourceAccess(source)
				if systemRootAccessAllowed(access.Write, role, admins, effectiveMode(conf), conf.DefaultBlock) {
					actions[gjagent.CapabilityActionDataInsert] = true
					actions[gjagent.CapabilityActionDataUpdate] = true
				}
				if systemRootAccessAllowed(access.Delete, role, admins, effectiveMode(conf), conf.DefaultBlock) {
					actions[gjagent.CapabilityActionDataDelete] = true
				}
			case sourcecap.KindCode:
				writeEnabled, _ := conf.sourceCapabilityForSource(source, sourcecap.KeyCodeWrite)
				if writeEnabled && !source.ReadOnly && authenticated {
					actions[gjagent.CapabilityActionCodeWrite] = true
				}
			}
		}
	}

	for _, root := range mcpGraphJinRoots {
		for _, action := range []string{systemActionInsert, systemActionUpdate, systemActionDelete} {
			if !ms.rootActionVisibleForContext(ctx, root, action) {
				continue
			}
			if callerActionNeedsIdentity(root, action) && !authenticated {
				continue
			}
			actions[root+"."+action] = true
		}
	}

	out := make([]string, 0, len(actions))
	for action := range actions {
		out = append(out, action)
	}
	sort.Strings(out)
	return out
}

func callerActionNeedsIdentity(root, action string) bool {
	if strings.EqualFold(action, systemActionRead) {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(root)) {
	case "gj_artifacts", "gj_watch", "gj_watch_event", "gj_task", "gj_task_entry":
		return true
	default:
		return false
	}
}
