package serv

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
)

// registerDiscoveryResources registers MCP resources for schema discovery.
// Each section is a focused resource so agents load only what they need.
func (ms *mcpServer) registerDiscoveryResources() {
	type section struct {
		uri  string
		name string
		desc string
		key  string
	}

	sections := []section{
		{
			uri:  "graphjin://discovery/syntax",
			name: "Query Syntax",
			desc: "GraphJin DSL cheat sheet: filter operators, aggregation functions (count_, sum_, avg_), GROUP BY via distinct, pagination, ordering. Essential before writing any query.",
			key:  "syntax",
		},
		{
			uri:  "graphjin://discovery/tables",
			name: "Table Index",
			desc: "Compact index of all tables: name, schema, row count, foreign keys, key column names, and join targets. Enough to find relevant tables without loading full column details.",
			key:  "tables",
		},
		{
			uri:  "graphjin://discovery/tables/full",
			name: "Full Table Details",
			desc: "Full table definitions with column types, nullability, defaults, indexes, aggregation fields, live data profiles, and sample rows. Warning: very large. Prefer describe_table for specific tables.",
			key:  "full_tables",
		},
		{
			uri:  "graphjin://discovery/insights",
			name: "Insights",
			desc: "Relationship paths between tables, auto-generated query templates, data quality flags, and database functions.",
			key:  "insights",
		},
	}

	for _, s := range sections {
		s := s // capture
		ms.srv.AddResource(
			mcp.NewResource(
				s.uri,
				s.name,
				mcp.WithResourceDescription(s.desc),
				mcp.WithMIMEType("text/markdown"),
			),
			func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
				md := ms.service.disc.CombinedSection(s.key)

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
}
