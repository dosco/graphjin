package serv

// MCPCLIResource describes a first-class CLI shortcut for a static MCP resource.
// The server owns this catalog so GraphJin's client and server surfaces stay aligned.
type MCPCLIResource struct {
	Command string
	URI     string
	Short   string
}

const (
	CatalogOverviewResourceURI     = "graphjin://catalog/overview"
	CatalogEntrypointsResourceURI  = "graphjin://catalog/entrypoints"
	CatalogCapabilitiesResourceURI = "graphjin://catalog/capabilities"
	QuerySyntaxResourceURI         = "graphjin://syntax/query"
	MutationSyntaxResourceURI      = "graphjin://syntax/mutation"
	WorkflowGuideResourceURI       = "graphjin://guides/workflow"
)

var mcpCLIResources = []MCPCLIResource{
	{
		Command: "catalog_overview",
		URI:     CatalogOverviewResourceURI,
		Short:   "Read the GraphJin catalog overview resource",
	},
	{
		Command: "catalog_entrypoints",
		URI:     CatalogEntrypointsResourceURI,
		Short:   "Read the GraphJin catalog entrypoints resource",
	},
	{
		Command: "catalog_capabilities",
		URI:     CatalogCapabilitiesResourceURI,
		Short:   "Read the GraphJin catalog capabilities resource",
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
	conf.MCP.LegacyDiscovery = true

	out := mcpToolList(conf)
	// graphql_help and query_catalog are the catalog-first discovery tools the
	// server exposes in sources mode; make sure the CLI always publishes them so
	// `graphjin cli query_catalog` works against a sources-mode server.
	for _, required := range []string{"graphql_help", "query_catalog"} {
		present := false
		for _, name := range out {
			if name == required {
				present = true
				break
			}
		}
		if !present {
			out = append(out, required)
		}
	}
	return append([]string(nil), out...)
}

// MCPCLIResources returns the static resource shortcuts GraphJin CLI should expose.
func MCPCLIResources() []MCPCLIResource {
	out := make([]MCPCLIResource, len(mcpCLIResources))
	copy(out, mcpCLIResources)
	return out
}
