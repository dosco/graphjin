package serv

import (
	"fmt"
	"os"
	"strings"
)

func (c *Config) legacyMCPToolsEnabled() bool {
	return c.legacyDiscoveryEnabled()
}

func (c *Config) legacyDiscoveryEnabled() bool {
	if c == nil {
		return false
	}
	return !c.Core.SourceMode() || c.MCP.LegacyDiscovery
}

func (c *Config) catalogToolsEnabled() bool {
	if c == nil {
		return false
	}
	return c.Core.CatalogEnabled()
}

func (c *Config) graphjinControlPlaneEnabled() bool {
	if c == nil {
		return false
	}
	source, ok := c.Core.GraphJinSource()
	return ok && sourceBool(source.ControlPlane, true)
}

func (c *Config) workflowsSourceEnabled() bool {
	if c == nil {
		return false
	}
	_, ok := c.Core.WorkflowsSource()
	return ok
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

func (c *Config) needsSystemHostDB() bool {
	if c == nil || !c.Core.SourceMode() {
		return false
	}
	for _, source := range c.Core.Sources {
		switch strings.ToLower(strings.TrimSpace(source.Kind)) {
		case "filesystem", "openapi", "graphjin", "workflows":
			return true
		}
	}
	return false
}

func validateServiceSourceModeConfig(conf *Config) error {
	if conf == nil {
		return nil
	}
	if !conf.Core.SourceMode() {
		if isCodeSQLType(conf.DB.Type) || isCodeSQLType(conf.DBType) {
			return fmt.Errorf("database.type codesql is legacy config; move CodeSQL providers to sources with kind: codesql")
		}
		return conf.Core.ValidateSourceMode()
	}
	if hasUserSuppliedLegacyDatabase(conf) {
		return fmt.Errorf("database is legacy database-only config; move SQL providers to sources")
	}
	return conf.Core.ValidateSourceMode()
}

// hasUserSuppliedLegacyDatabase reports whether the user has explicitly
// configured a top-level legacy `database:` provider — either via the
// YAML config file or via GJ_DATABASE_* / SJ_DATABASE_* / SG_DATABASE_*
// environment variables. Defaults baked into the viper instance by
// newViperWithDefaults are intentionally ignored: they are not user
// intent and would otherwise cause every source-mode config to be
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
	return conf.Core.NormalizeSources()
}

func sourceBool(v *bool, def bool) bool {
	if v == nil {
		return def
	}
	return *v
}
