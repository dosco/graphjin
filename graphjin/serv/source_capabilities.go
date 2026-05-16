package serv

import (
	"github.com/dosco/graphjin/core/v3"
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

func (c *Config) effectiveSourceCapability(kind, capability string) bool {
	if c == nil {
		return false
	}
	source, ok := c.sourceByCanonicalKind(kind)
	if !ok {
		return false
	}
	value, _ := c.sourceCapabilityForSource(source, capability)
	return value
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
	for _, kind := range sourcecap.Kinds() {
		for _, def := range sourcecap.Definitions(kind) {
			if def.MCPFlag == "" {
				continue
			}
			value, explicit := conf.sourceCapabilityConfigured(kind, def.Key)
			if explicit {
				conf.setMCPFlag(def.MCPFlag, value)
			}
		}
	}
	if conf.graphjinSourceReadOnly() {
		conf.MCP.AllowConfigUpdates = false
		conf.MCP.AllowSchemaReload = false
		conf.MCP.AllowSchemaUpdates = false
		conf.MCP.AllowMutations = false
	}
	if conf.workflowsSourceReadOnly() {
		conf.MCP.AllowWorkflowUpdates = false
	}
}

func (c *Config) setMCPFlag(flag string, value bool) {
	switch flag {
	case sourcecap.MCPAllowConfigUpdates:
		c.MCP.AllowConfigUpdates = value
	case sourcecap.MCPAllowSchemaReload:
		c.MCP.AllowSchemaReload = value
	case sourcecap.MCPAllowSchemaUpdates:
		c.MCP.AllowSchemaUpdates = value
	case sourcecap.MCPAllowRawQueries:
		c.MCP.AllowRawQueries = value
	case sourcecap.MCPAllowMutations:
		c.MCP.AllowMutations = value
	case sourcecap.MCPAllowDevTools:
		c.MCP.AllowDevTools = value
	case sourcecap.MCPLegacyDiscovery:
		c.MCP.LegacyDiscovery = value
	case sourcecap.MCPAllowWorkflowUpdates:
		c.MCP.AllowWorkflowUpdates = value
	}
}
