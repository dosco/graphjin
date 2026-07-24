// Package featurecap defines capabilities for GraphJin-owned features.
//
// These are deliberately separate from sourcecap: sources are external graph
// providers, while system roots and workflows are built into GraphJin.
package featurecap

import (
	"sort"
	"strings"
)

const (
	ModeDev     = "dev"
	ModeProd    = "prod"
	ModeAgentic = "agentic"
)

const (
	KindSystem    = "system"
	KindWorkflows = "workflows"
)

const (
	ActionRead    = "read"
	ActionWrite   = "write"
	ActionExecute = "execute"
	ActionReload  = "reload"
	ActionQuery   = "query"
	ActionMutate  = "mutate"
)

const EnforcementRuntime = "runtime"

const (
	MCPAllowConfigUpdates   = "allow_config_updates"
	MCPAllowSchemaReload    = "allow_schema_reload"
	MCPAllowSchemaUpdates   = "allow_schema_updates"
	MCPAllowRawQueries      = "allow_raw_queries"
	MCPAllowMutations       = "allow_mutations"
	MCPAllowDevTools        = "allow_dev_tools"
	MCPLegacyDiscovery      = "legacy_discovery"
	MCPAllowWorkflowUpdates = "allow_workflow_updates"
)

const (
	KeyCatalogRead         = "catalog.read"
	KeySecurityRead        = "security.read"
	KeyConfigRead          = "config.read"
	KeyConfigWrite         = "config.write"
	KeyRuntimeRead         = "runtime.read"
	KeyRawGraphQLQuery     = "raw_graphql.query"
	KeyRawGraphQLMutate    = "raw_graphql.mutate"
	KeySchemaReload        = "schema.reload"
	KeySchemaWrite         = "schema.write"
	KeyDevToolsRead        = "dev_tools.read"
	KeyLegacyDiscoveryRead = "legacy_discovery.read"

	KeyWorkflowExecute = "execute"
	KeyWorkflowRead    = "read"
	KeyWorkflowWrite   = "write"
)

// Definition is the source of truth for a public feature capability.
type Definition struct {
	Kind           string
	Key            string
	Action         string
	Summary        string
	Reason         string
	Recommendation string
	Severity       string
	Enforcement    string
	ReadOnlyBlocks bool
	DefaultDev     bool
	DefaultProd    bool
	DefaultAgentic bool
	MCPFlag        string
	ExampleValue   string
}

func (d Definition) Default(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case ModeDev:
		return d.DefaultDev
	case ModeAgentic:
		return d.DefaultAgentic
	default:
		return d.DefaultProd
	}
}

var kindOrder = []string{KindSystem, KindWorkflows}

var systemRoots = []string{
	"gj_catalog", "gj_security", "gj_runtime", "gj_config", "gj_artifacts",
	"gj_watch", "gj_watch_event", "gj_watch_flow_preview", "gj_workflow", "gj_workflow_execution",
}

var definitions = []Definition{
	def(KindSystem, KeyCatalogRead, ActionRead, true, true, true, "medium", false, "Read the GraphJin catalog."),
	def(KindSystem, KeySecurityRead, ActionRead, true, false, false, "high", false, "Read detailed GraphJin security audit rows."),
	def(KindSystem, KeyConfigRead, ActionRead, true, false, false, "high", false, "Read GraphJin configuration rows."),
	def(KindSystem, KeyConfigWrite, ActionWrite, true, false, false, "critical", true, "Write GraphJin configuration.", mcp(MCPAllowConfigUpdates)),
	def(KindSystem, KeyRuntimeRead, ActionRead, true, false, true, "medium", false, "Read compact GraphJin runtime status and recent redacted events."),
	def(KindSystem, KeyRawGraphQLQuery, ActionQuery, true, false, false, "high", false, "Execute raw GraphQL queries through MCP.", mcp(MCPAllowRawQueries)),
	def(KindSystem, KeyRawGraphQLMutate, ActionMutate, true, false, false, "critical", true, "Execute raw GraphQL mutations through MCP.", mcp(MCPAllowMutations)),
	def(KindSystem, KeySchemaReload, ActionReload, true, false, false, "high", true, "Reload GraphJin schema metadata.", mcp(MCPAllowSchemaReload)),
	def(KindSystem, KeySchemaWrite, ActionWrite, true, false, false, "critical", true, "Write schema changes through GraphJin tools.", mcp(MCPAllowSchemaUpdates)),
	def(KindSystem, KeyDevToolsRead, ActionRead, true, false, false, "medium", false, "Read development and diagnostic tool output.", mcp(MCPAllowDevTools)),
	def(KindSystem, KeyLegacyDiscoveryRead, ActionRead, true, false, false, "medium", false, "Read legacy discovery surfaces.", mcp(MCPLegacyDiscovery)),

	def(KindWorkflows, KeyWorkflowExecute, ActionExecute, true, false, true, "critical", true, "Execute approved workflows."),
	def(KindWorkflows, KeyWorkflowRead, ActionRead, true, false, false, "high", false, "Read workflow definitions and code."),
	def(KindWorkflows, KeyWorkflowWrite, ActionWrite, true, false, false, "high", true, "Write workflow definitions and code.", mcp(MCPAllowWorkflowUpdates)),
}

var byKind map[string][]Definition
var byKindKey map[string]Definition

func init() {
	byKind = make(map[string][]Definition)
	byKindKey = make(map[string]Definition)
	for _, definition := range definitions {
		byKind[definition.Kind] = append(byKind[definition.Kind], definition)
		byKindKey[definition.Kind+"\x00"+definition.Key] = definition
	}
}

func def(kind, key, action string, dev, prod, agentic bool, severity string, readOnlyBlocks bool, summary string, opts ...func(*Definition)) Definition {
	d := Definition{
		Kind: kind, Key: key, Action: action, Summary: summary,
		Reason:         "Built-in feature capabilities expose GraphJin-owned behavior.",
		Recommendation: "Keep this capability disabled unless the deployment needs it.",
		Severity:       severity, Enforcement: EnforcementRuntime, ReadOnlyBlocks: readOnlyBlocks,
		DefaultDev: dev, DefaultProd: prod, DefaultAgentic: agentic,
	}
	for _, opt := range opts {
		opt(&d)
	}
	if dev || agentic {
		d.ExampleValue = "true"
	} else {
		d.ExampleValue = "false"
	}
	return d
}

func mcp(flag string) func(*Definition) {
	return func(d *Definition) { d.MCPFlag = flag }
}

func Kinds() []string { return append([]string(nil), kindOrder...) }

func SystemRoots() []string { return append([]string(nil), systemRoots...) }

func ValidSystemRoot(root string) bool {
	root = strings.ToLower(strings.TrimSpace(root))
	for _, candidate := range systemRoots {
		if root == candidate {
			return true
		}
	}
	return false
}

func Definitions(kind string) []Definition {
	return append([]Definition(nil), byKind[strings.ToLower(strings.TrimSpace(kind))]...)
}

func Lookup(kind, key string) (Definition, bool) {
	d, ok := byKindKey[strings.ToLower(strings.TrimSpace(kind))+"\x00"+strings.TrimSpace(key)]
	return d, ok
}

func ValidKeys(kind string) []string {
	defs := Definitions(kind)
	keys := make([]string, 0, len(defs))
	for _, d := range defs {
		keys = append(keys, d.Key)
	}
	return keys
}

func ValidKeyList(kind string) string { return strings.Join(ValidKeys(kind), ", ") }

func CapabilityMap() map[string][]string {
	out := make(map[string][]string, len(kindOrder))
	for _, kind := range kindOrder {
		out[kind] = ValidKeys(kind)
	}
	return out
}

func SortedDefinitions() []Definition {
	out := append([]Definition(nil), definitions...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Key < out[j].Key
	})
	return out
}
