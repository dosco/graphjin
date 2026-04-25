package serv

// MCPCLIResource describes a first-class CLI shortcut for a static MCP resource.
// The server owns this catalog so GraphJin's client and server surfaces stay aligned.
type MCPCLIResource struct {
	Command string
	URI     string
	Short   string
}

const (
	QuerySyntaxResourceURI    = "graphjin://syntax/query"
	MutationSyntaxResourceURI = "graphjin://syntax/mutation"
	WorkflowGuideResourceURI  = "graphjin://guides/workflow"
)

var mcpCLIResources = []MCPCLIResource{
	{
		Command: "query_syntax",
		URI:     QuerySyntaxResourceURI,
		Short:   "Read the GraphJin query syntax resource",
	},
	{
		Command: "mutation_syntax",
		URI:     MutationSyntaxResourceURI,
		Short:   "Read the GraphJin mutation syntax resource",
	},
	{
		Command: "workflow_guide",
		URI:     WorkflowGuideResourceURI,
		Short:   "Read the GraphJin workflow guide resource",
	},
	{
		Command: "js_runtime_api",
		URI:     JSRuntimeResourceURI,
		Short:   "Read the GraphJin JS runtime API resource",
	},
}

// MCPAllToolNames returns the full superset of MCP tools GraphJin may expose.
// This is used by the CLI to publish first-class commands for every server tool.
func MCPAllToolNames() []string {
	conf := &Config{}
	conf.Serv.Production = false
	conf.MCP.AllowWorkflowUpdates = true
	conf.MCP.AllowConfigUpdates = true
	conf.MCP.AllowSchemaReload = true
	conf.MCP.AllowSchemaUpdates = true
	conf.MCP.AllowDevTools = true
	conf.MCP.AllowRawQueries = true

	out := mcpToolList(conf)
	return append([]string(nil), out...)
}

// MCPCLIResources returns the static resource shortcuts GraphJin CLI should expose.
func MCPCLIResources() []MCPCLIResource {
	out := make([]MCPCLIResource, len(mcpCLIResources))
	copy(out, mcpCLIResources)
	return out
}
