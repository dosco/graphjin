package serv

import (
	"fmt"
	"os"
	"strings"

	"github.com/dosco/graphjin/core/v3/featurecap"
	"github.com/dosco/graphjin/core/v3/sourcecap"
)

// agenticSurfaceEnabled reports whether the agentic surface — catalog, artifacts
// persistence, workflows, code, the gj_* control-plane roots, the MCP server, and
// the agent — may mount. prod is the only pre-agentic compatibility mode and never
// mounts the agentic surface, regardless of individual enabled flags. This is the
// fail-closed feature gate from the security model (see SECURITY.md); prod stays a
// classic public REST/GraphQL API over compiled queries + roles/RLS.
func (c *Config) agenticSurfaceEnabled() bool {
	return c != nil && effectiveMode(c) != modeProd
}

func (c *Config) legacyMCPToolsEnabled() bool {
	return c.legacyDiscoveryEnabled()
}

func (c *Config) legacyDiscoveryEnabled() bool {
	if c == nil {
		return false
	}
	if !c.Core.IsSourcesUsed() {
		return true
	}
	return c.effectiveFeatureCapability(featurecap.KindSystem, featurecap.KeyLegacyDiscoveryRead)
}

func (c *Config) catalogToolsEnabled() bool {
	if c == nil {
		return false
	}
	return c.Core.CatalogEnabled() && c.agenticSurfaceEnabled()
}

func (c *Config) mcpDisabled() bool {
	if c == nil || c.MCP.Disable {
		return true
	}
	if effectiveMode(c) == modeProd {
		// Source mode: prod hard-gates the agentic surface (security model); the
		// MCP server never mounts there, even if explicitly enabled.
		if c.Core.IsSourcesUsed() {
			return true
		}
		// Legacy (non-sources) prod keeps the long-standing escape hatch: MCP is
		// off by default but an explicit mcp.disable=false re-enables it.
		if c.MCP.disableExplicit {
			return false
		}
		return true
	}
	return false
}

func (c *Config) systemControlPlaneEnabled() bool {
	return c != nil && c.agenticSurfaceEnabled() && (c.effectiveFeatureCapability(featurecap.KindSystem, featurecap.KeySecurityRead) ||
		c.effectiveFeatureCapability(featurecap.KindSystem, featurecap.KeyConfigRead) ||
		c.effectiveFeatureCapability(featurecap.KindSystem, featurecap.KeyConfigWrite))
}

func (c *Config) workflowsEnabled() bool {
	return c != nil && c.agenticSurfaceEnabled() && (c.effectiveFeatureCapability(featurecap.KindWorkflows, featurecap.KeyWorkflowExecute) ||
		c.effectiveFeatureCapability(featurecap.KindWorkflows, featurecap.KeyWorkflowRead) ||
		c.effectiveFeatureCapability(featurecap.KindWorkflows, featurecap.KeyWorkflowWrite))
}

func (c *Config) runtimeRootEnabled() bool {
	if !c.runtimeRootRegistered() {
		return false
	}
	return c.effectiveFeatureCapability(featurecap.KindSystem, featurecap.KeyRuntimeRead)
}

func (c *Config) runtimeRootRegistered() bool {
	if c == nil {
		return false
	}
	switch effectiveMode(c) {
	case modeDev, modeAgentic:
	default:
		return false
	}
	return c.effectiveFeatureCapability(featurecap.KindSystem, featurecap.KeyRuntimeRead)
}

func (c *Config) artifactSourceReadOnly() bool {
	if c == nil || !c.Core.Artifacts.Enabled {
		return false
	}
	source, ok := c.Core.SourceByName(c.Core.EffectiveArtifactsConfig().Source)
	return ok && source.ReadOnly
}

func (c *Config) needsSystemHostDB() bool {
	if c == nil || !c.Core.IsSourcesUsed() {
		return false
	}
	if c.Core.Artifacts.Enabled {
		return true
	}
	if c.catalogToolsEnabled() || c.systemControlPlaneEnabled() || c.runtimeRootRegistered() || c.workflowsEnabled() {
		return true
	}
	for _, source := range c.Core.Sources {
		switch source.CanonicalKind() {
		case sourcecap.KindFile, sourcecap.KindAPI:
			return true
		}
	}
	return false
}

func validateServiceIsSourcesUsedConfig(conf *Config) error {
	if conf == nil {
		return nil
	}
	if conf.Core.Watches.Enabled && !conf.Core.Artifacts.Enabled {
		return fmt.Errorf("watches require artifacts.enabled")
	}
	if err := validateServiceTasksConfig(conf); err != nil {
		return err
	}
	if !conf.Core.IsSourcesUsed() {
		if isCodeSQLType(conf.DB.Type) || isCodeSQLType(conf.DBType) {
			return fmt.Errorf("database.type codesql is legacy config; move CodeSQL providers to sources with kind: code")
		}
		return validateCoreSourcesWithManagedArtifacts(conf)
	}
	if hasUserSuppliedLegacyDatabase(conf) {
		return fmt.Errorf("database is legacy database-only config; move SQL providers to sources")
	}
	return validateCoreSourcesWithManagedArtifacts(conf)
}

// Managed artifact stores are injected after public source validation, so the
// source validator temporarily disables their dependent roots. Keep task
// scalar validation here so that temporary disablement cannot hide bad input.
func validateServiceTasksConfig(conf *Config) error {
	if conf == nil || !conf.Core.Tasks.Enabled {
		return nil
	}
	if !conf.Core.Artifacts.Enabled {
		return fmt.Errorf("tasks require artifacts.enabled")
	}
	checks := []struct {
		name  string
		value int
	}{
		{"max_per_owner", conf.Core.Tasks.MaxPerOwner},
		{"max_entries_per_task", conf.Core.Tasks.MaxEntriesPerTask},
		{"entry_retention_hours", conf.Core.Tasks.EntryRetentionHours},
		{"snapshot_max_bytes", conf.Core.Tasks.SnapshotMaxBytes},
	}
	for _, check := range checks {
		if check.value < 0 {
			return fmt.Errorf("tasks.%s must be greater than or equal to 0", check.name)
		}
	}
	return nil
}

func validateCoreSourcesWithManagedArtifacts(conf *Config) error {
	if conf == nil || !conf.managedArtifactStore {
		return conf.Core.ValidateIsSourcesUsed()
	}
	artifacts, watches, tasks := conf.Core.Artifacts, conf.Core.Watches, conf.Core.Tasks
	conf.Core.Artifacts.Enabled = false
	conf.Core.Artifacts.Source = ""
	conf.Core.Watches.Enabled = false
	conf.Core.Tasks.Enabled = false
	err := conf.Core.ValidateIsSourcesUsed()
	conf.Core.Artifacts, conf.Core.Watches, conf.Core.Tasks = artifacts, watches, tasks
	return err
}

// hasUserSuppliedLegacyDatabase reports whether the user has explicitly
// configured a top-level legacy `database:` provider — either via the
// YAML config file or via GJ_DATABASE_* / SJ_DATABASE_* / SG_DATABASE_*
// environment variables. Defaults baked into the viper instance by
// newViperWithDefaults are intentionally ignored: they are not user
// intent and would otherwise cause every sources-used config to be
// wrongly rejected.
func hasUserSuppliedLegacyDatabase(conf *Config) bool {
	if conf == nil {
		return false
	}
	if conf.viper != nil && conf.viper.InConfig("database") {
		return true
	}
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "GJ_DATABASE_") ||
			strings.HasPrefix(e, "SJ_DATABASE_") ||
			strings.HasPrefix(e, "SG_DATABASE_") {
			return true
		}
	}
	return false
}

func normalizeServiceSources(conf *Config) error {
	if conf == nil {
		return nil
	}
	if !conf.managedArtifactStore {
		return conf.Core.NormalizeSources()
	}
	artifacts, watches, tasks := conf.Core.Artifacts, conf.Core.Watches, conf.Core.Tasks
	conf.Core.Artifacts.Enabled = false
	conf.Core.Artifacts.Source = ""
	conf.Core.Watches.Enabled = false
	conf.Core.Tasks.Enabled = false
	err := conf.Core.NormalizeSources()
	conf.Core.Artifacts, conf.Core.Watches, conf.Core.Tasks = artifacts, watches, tasks
	return err
}
