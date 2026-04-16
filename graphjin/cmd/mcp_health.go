package main

import "github.com/spf13/cobra"

// mcpHealthCmd wires `graphjin cli health` which calls the check_health MCP
// tool (server-gated: AllowDevTools). Exits non-zero on tool error.
func mcpHealthCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "health",
		Short: "Check database connectivity and pool stats (MCP: check_health)",
		Long: `Report DB connection health and pool statistics for the running server.

Exits non-zero when the server returns an error (useful for CI gating).
Server-gated: requires mcp.allow_dev_tools.`,
		Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			runToolCmd(cmd, "check_health", nil)
		},
	}
}
