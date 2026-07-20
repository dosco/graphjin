package serv

import (
	"fmt"
	"os"
	"strings"

	"github.com/dosco/graphjin/core/v3"
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
	return c.sourceCapability(sourcecap.KindGraphJin, sourcecap.KeyLegacyDiscoveryRead, c.MCP.LegacyDiscovery)
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

func (c *Config) graphjinControlPlaneEnabled() bool {
	if c == nil {
		return false
	}
	source, ok := c.Core.GraphJinSource()
	return ok && sourceBool(source.ControlPlane, true) && c.agenticSurfaceEnabled()
}

func (c *Config) workflowsSourceEnabled() bool {
	if c == nil {
		return false
	}
	_, ok := c.Core.WorkflowsSource()
	return ok && c.agenticSurfaceEnabled()
}

func (c *Config) runtimeRootSourceEnabled() bool {
	if !c.runtimeRootRegistered() {
		return false
	}
	source, ok := c.sourceByCanonicalKind(sourcecap.KindGraphJin)
	if !ok {
		return false
	}
	allowed, _ := c.sourceCapabilityForSource(source, sourcecap.KeyRuntimeRead)
	return allowed
}

func (c *Config) runtimeRootRegistered() bool {
	if c == nil || !c.Core.IsSourcesUsed() {
		return false
	}
	switch effectiveMode(c) {
	case modeDev, modeAgentic:
	default:
		return false
	}
	_, ok := c.sourceByCanonicalKind(sourcecap.KindGraphJin)
	return ok
}

func (c *Config) sourceCapability(kind, key string, fallback bool) bool {
	if c == nil {
		return fallback
	}
	if source, ok := c.sourceByCanonicalKind(kind); ok {
		if source.Capabilities != nil {
			if value, ok := source.Capabilities[key]; ok {
				return value
			}
		}
	}
	return fallback
}

func (c *Config) sourceCapabilityConfigured(kind, key string) (bool, bool) {
	if c == nil {
		return false, false
	}
	if source, ok := c.sourceByCanonicalKind(kind); ok && source.Capabilities != nil {
		value, exists := source.Capabilities[key]
		return value, exists
	}
	return false, false
}

func (c *Config) sourceByCanonicalKind(kind string) (core.SourceConfig, bool) {
	if c == nil {
		return core.SourceConfig{}, false
	}
	for _, source := range c.Core.Sources {
		if source.CanonicalKind() == kind {
			return source, true
		}
	}
	return core.SourceConfig{}, false
}

func (c *Config) graphjinSourceReadOnly() bool {
	if c == nil {
		return false
	}
	source, ok := c.Core.GraphJinSource()
	return ok && source.ReadOnly
}

func (c *Config) workflowsSourceReadOnly() bool {
	if c == nil {
		return false
	}
	source, ok := c.Core.WorkflowsSource()
	return ok && source.ReadOnly
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
	for _, source := range c.Core.Sources {
		switch source.CanonicalKind() {
		case sourcecap.KindFile, sourcecap.KindAPI, sourcecap.KindGraphJin, sourcecap.KindWorkflow:
			return true
		}
	}
	return false
}

func validateServiceIsSourcesUsedConfig(conf *Config) error {
	if conf == nil {
		return nil
	}
	for _, source := range conf.Core.Sources {
		if isManagedArtifactDatabase(source.Name) {
			return fmt.Errorf("source name %q is reserved for GraphJin's managed artifact store", source.Name)
		}
	}
	if conf.Core.Watches.Enabled && !conf.Core.Artifacts.Enabled {
		return fmt.Errorf("watches require artifacts.enabled")
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

func validateCoreSourcesWithManagedArtifacts(conf *Config) error {
	if conf == nil || !conf.managedArtifactStore {
		return conf.Core.ValidateIsSourcesUsed()
	}
	artifacts, watches := conf.Core.Artifacts, conf.Core.Watches
	conf.Core.Artifacts.Enabled = false
	conf.Core.Artifacts.Source = ""
	conf.Core.Watches.Enabled = false
	err := conf.Core.ValidateIsSourcesUsed()
	conf.Core.Artifacts, conf.Core.Watches = artifacts, watches
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
	artifacts, watches := conf.Core.Artifacts, conf.Core.Watches
	conf.Core.Artifacts.Enabled = false
	conf.Core.Artifacts.Source = ""
	conf.Core.Watches.Enabled = false
	err := conf.Core.NormalizeSources()
	conf.Core.Artifacts, conf.Core.Watches = artifacts, watches
	return err
}

func sourceBool(v *bool, def bool) bool {
	if v == nil {
		return def
	}
	return *v
}
