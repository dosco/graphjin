package serv

import (
	"context"
	"sort"
	"strings"

	gjagent "github.com/dosco/graphjin/agent/v3"
	gjopenapi "github.com/dosco/graphjin/core/v3/openapi"
	"github.com/dosco/graphjin/core/v3/sourcecap"
)

// callerAllowedActions is the bounded mutating half of the caller capability
// profile. It is advisory/preflight truth for the agent; core role/RLS checks
// remain the execution-time authority for every concrete table and row.
func (ms *mcpServer) callerAllowedActions(ctx context.Context) []string {
	conf := ms.config()
	if conf == nil || conf.Agent.ReadOnly || !conf.MCP.AllowMutations || !ms.toolAvailableForContext(ctx, "execute_graphql") {
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
			case sourcecap.KindAPI:
				// Concrete API operations are evaluated from the loaded registry below;
				// source config alone does not reveal the operation's HTTP method.
			}
		}
		ms.addCallerAllowedAPIActions(ctx, conf, role, actions)
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

func (ms *mcpServer) addCallerAllowedAPIActions(ctx context.Context, conf *Config, role string, actions map[string]bool) {
	if ms == nil || ms.service == nil || ms.service.gj == nil || conf == nil {
		return
	}
	md, err := ms.service.gj.MetadataSnapshot(ms.service.metadataSnapshotExcludes()...)
	if err != nil || md == nil {
		return
	}
	for _, operation := range md.APIOperations {
		if !operation.Active {
			continue
		}
		decision := conf.Core.AuthorizeOpenAPIOperation(ctx, &gjopenapi.OpDescriptor{
			SourceName: operation.SourceName, OperationID: operation.OperationID,
			Method: operation.Method, AllowedRoles: operation.AllowedRoles,
		}, role)
		if !decision.Allowed {
			continue
		}
		switch strings.ToUpper(operation.Method) {
		case "POST", "PUT", "PATCH":
			actions[gjagent.CapabilityActionAPIWrite] = true
		case "DELETE":
			actions[gjagent.CapabilityActionAPIDelete] = true
		}
	}
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
