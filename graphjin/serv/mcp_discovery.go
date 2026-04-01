package serv

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
)

// registerDiscoveryResources registers MCP resources for the schema Bible.
// The full document is split into focused sections so agents can load only
// what they need without exceeding context limits.
func (ms *mcpServer) registerDiscoveryResources() {
	type section struct {
		uri  string
		name string
		desc string
		key  string // "" = full markdown
	}

	sections := []section{
		{
			uri:  "graphjin://discovery",
			name: "Schema Bible — Overview",
			desc: "Compact overview with header, stats, and table of contents linking to sub-resources. Read this first.",
			key:  "overview",
		},
		{
			uri:  "graphjin://discovery/syntax",
			name: "Schema Bible — Query Syntax",
			desc: "GraphJin DSL cheat sheet: filter operators, aggregation functions (count_, sum_, avg_), GROUP BY via distinct, pagination, ordering. Essential before writing any query.",
			key:  "syntax",
		},
		{
			uri:  "graphjin://discovery/tables",
			name: "Schema Bible — Table Index",
			desc: "Compact index of all tables: name, schema, row count, foreign keys, key column names, and join targets. Enough to find relevant tables without loading full column details.",
			key:  "tables",
		},
		{
			uri:  "graphjin://discovery/tables/full",
			name: "Schema Bible — Full Table Details",
			desc: "Full table definitions with column types, nullability, defaults, indexes, aggregation fields, live data profiles, and sample rows. Warning: very large. Prefer describe_table for specific tables.",
			key:  "full_tables",
		},
		{
			uri:  "graphjin://discovery/insights",
			name: "Schema Bible — Insights",
			desc: "Relationship paths between tables, auto-generated query templates, data quality flags, and database functions.",
			key:  "insights",
		},
		{
			uri:  "graphjin://discovery/full",
			name: "Schema Bible — Full",
			desc: "Complete schema Bible in a single document. Warning: may be very large for databases with many tables.",
			key:  "",
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
				var md string
				if s.key == "" {
					md = ms.service.gj.GetCombinedDiscovery()
				} else {
					md = ms.service.gj.GetCombinedDiscoverySection(s.key)
				}

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
