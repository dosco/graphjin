package serv

import (
	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/core/v3/featurecap"
	"github.com/dosco/graphjin/core/v3/sourcecap"
)

func sourceCapabilityDefault(mode, kind, capability string) bool {
	if def, ok := sourcecap.Lookup(kind, capability); ok {
		return def.Default(mode)
	}
	return false
}

func sourceCapabilityKeys(kind string) []string {
	return sourcecap.ValidKeys(kind)
}

func featureCapabilityDefault(mode, kind, capability string) bool {
	if def, ok := featurecap.Lookup(kind, capability); ok {
		return def.Default(mode)
	}
	return false
}

func featureCapabilityKeys(kind string) []string {
	return featurecap.ValidKeys(kind)
}

func (c *Config) featureCapabilityConfigured(kind, capability string) (bool, bool) {
	if c == nil {
		return false, false
	}
	return c.Core.FeatureCapabilityConfigured(kind, capability)
}

func (c *Config) effectiveFeatureCapability(kind, capability string) bool {
	if c == nil || !c.agenticSurfaceEnabled() {
		return false
	}
	return c.Core.FeatureCapability(kind, capability)
}

func (c *Config) sourceCapabilityForSource(source core.SourceConfig, capability string) (value bool, explicit bool) {
	kind := source.CanonicalKind()
	def := sourceCapabilityDefault(effectiveMode(c), kind, capability)
	if source.Capabilities != nil {
		if value, ok := source.Capabilities[capability]; ok {
			return value, true
		}
	}
	return def, false
}

func applySourceCapabilitySourceDefaults(conf *Config) {
	if conf == nil || !conf.Core.IsSourcesUsed() {
		return
	}
	for i := range conf.Core.Sources {
		source := &conf.Core.Sources[i]
		switch source.CanonicalKind() {
		case sourcecap.KindCode:
			if infer, explicit := source.Capability(sourcecap.KeyCodeInferDBRefs); explicit {
				source.InferDBRefs = &infer
			}
		case sourcecap.KindFile:
			write, _ := conf.sourceCapabilityForSource(*source, sourcecap.KeyFilesWrite)
			deleteAllowed, _ := conf.sourceCapabilityForSource(*source, sourcecap.KeyFilesDelete)
			if !write || !deleteAllowed {
				source.ReadOnly = true
			}
		}
	}
}

func applySourceCapabilityMCPDefaults(conf *Config) {
	if conf == nil || !conf.Core.IsSourcesUsed() {
		return
	}
	for _, kind := range featurecap.Kinds() {
		for _, def := range featurecap.Definitions(kind) {
			if def.MCPFlag == "" {
				continue
			}
			value, explicit := conf.featureCapabilityConfigured(kind, def.Key)
			if explicit {
				conf.setMCPFlag(def.MCPFlag, value)
			}
		}
	}
}

func (c *Config) setMCPFlag(flag string, value bool) {
	switch flag {
	case featurecap.MCPAllowConfigUpdates:
		c.MCP.AllowConfigUpdates = value
	case featurecap.MCPAllowSchemaReload:
		c.MCP.AllowSchemaReload = value
	case featurecap.MCPAllowSchemaUpdates:
		c.MCP.AllowSchemaUpdates = value
	case featurecap.MCPAllowRawQueries:
		c.MCP.AllowRawQueries = value
	case featurecap.MCPAllowMutations:
		c.MCP.AllowMutations = value
	case featurecap.MCPAllowDevTools:
		c.MCP.AllowDevTools = value
	case featurecap.MCPLegacyDiscovery:
		c.MCP.LegacyDiscovery = value
	case featurecap.MCPAllowWorkflowUpdates:
		c.MCP.AllowWorkflowUpdates = value
	}
}
