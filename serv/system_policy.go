package serv

import (
	"strings"

	"github.com/dosco/graphjin/core/v3/featurecap"
)

const (
	systemActionRead   = "read"
	systemActionInsert = "insert"
	systemActionUpdate = "update"
	systemActionDelete = "delete"
)

// systemRootActionEnabled is the single feature gate for every built-in root.
// A root cannot be resurrected by role or admin access when its capability is
// disabled.
func systemRootActionEnabled(conf *Config, root, action string) bool {
	if conf == nil || !conf.agenticSurfaceEnabled() {
		return false
	}
	root = strings.ToLower(strings.TrimSpace(root))
	action = strings.ToLower(strings.TrimSpace(action))
	system := func(key string) bool {
		return conf.effectiveFeatureCapability(featurecap.KindSystem, key)
	}
	workflows := func(key string) bool {
		return conf.effectiveFeatureCapability(featurecap.KindWorkflows, key)
	}

	switch root {
	case "gj_catalog":
		enabled := system(featurecap.KeyCatalogRead)
		if !conf.Core.IsSourcesUsed() {
			enabled = enabled && conf.Core.CatalogEnabled()
		}
		return action == systemActionRead && enabled
	case "gj_security":
		return action == systemActionRead && system(featurecap.KeySecurityRead)
	case "gj_runtime":
		return action == systemActionRead && system(featurecap.KeyRuntimeRead)
	case "gj_config":
		if action == systemActionRead {
			return system(featurecap.KeyConfigRead)
		}
		return action != systemActionDelete && system(featurecap.KeyConfigWrite)
	case "gj_workflow":
		if action == systemActionRead {
			return workflows(featurecap.KeyWorkflowRead)
		}
		return workflows(featurecap.KeyWorkflowWrite)
	case "gj_workflow_execution":
		if action == systemActionRead {
			return workflows(featurecap.KeyWorkflowExecute)
		}
		return action == systemActionInsert && workflows(featurecap.KeyWorkflowExecute)
	case "gj_artifacts":
		if !conf.Core.Artifacts.Enabled {
			return false
		}
		return action == systemActionRead || !conf.artifactSourceReadOnly()
	case "gj_watch", "gj_watch_event":
		if !configWatchesEnabled(conf) {
			return false
		}
		return action == systemActionRead || !conf.artifactSourceReadOnly()
	case "gj_task", "gj_task_entry":
		if !configTasksEnabled(conf) {
			return false
		}
		return action == systemActionRead || !conf.artifactSourceReadOnly()
	default:
		return false
	}
}

func systemRootRegistered(conf *Config, root string) bool {
	for _, action := range []string{systemActionRead, systemActionInsert, systemActionUpdate, systemActionDelete} {
		if systemRootActionEnabled(conf, root, action) {
			return true
		}
	}
	return false
}

func systemRootAllowed(conf *Config, root, action, role string) bool {
	if !systemRootActionEnabled(conf, root, action) {
		return false
	}
	access := conf.Core.EffectiveSystemRootAccess()
	accessMode := access[strings.ToLower(strings.TrimSpace(root))]
	return systemRootAccessAllowed(accessMode, role, conf.Core.EffectiveIdentityConfig().AdminRoles, effectiveMode(conf), conf.DefaultBlock)
}

func registeredSystemRoots(conf *Config) []string {
	all := featurecap.SystemRoots()
	out := make([]string, 0, len(all))
	for _, root := range all {
		if systemRootRegistered(conf, root) {
			out = append(out, root)
		}
	}
	return out
}
