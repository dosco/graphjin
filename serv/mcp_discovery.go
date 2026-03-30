package serv

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
)

// registerDiscoveryResources registers the MCP resource for the combined schema Bible.
// Called from registerResources() in mcp_syntax.go.
func (ms *mcpServer) registerDiscoveryResources() {
	ms.srv.AddResource(
		mcp.NewResource(
			"graphjin://discovery",
			"Schema Bible",
			mcp.WithResourceDescription("Complete schema Bible — all databases, tables, columns, relationships, live data profiles, query templates. Read this first to avoid describe_table/list_tables calls."),
			mcp.WithMIMEType("text/markdown"),
		),
		func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			md := ms.service.gj.GetCombinedDiscovery()
			if md == "" {
				return []mcp.ResourceContents{
					mcp.TextResourceContents{
						URI:      req.Params.URI,
						MIMEType: "text/plain",
						Text:     "Discovery not available. Schema may not be ready yet.",
					},
				}, nil
			}

			return []mcp.ResourceContents{
				mcp.TextResourceContents{
					URI:      req.Params.URI,
					MIMEType: "text/markdown",
					Text:     md,
				},
			}, nil
		},
	)
}
