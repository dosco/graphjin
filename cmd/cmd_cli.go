package main

import (
	"github.com/spf13/cobra"
)

func cliCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "cli",
		Short: "HTTP client commands against a running GraphJin server",
		Long: `Send commands to a running GraphJin server via the MCP HTTP/JSON-RPC API.

Examples:
  graphjin cli query list
  graphjin cli fragment list
  graphjin cli workflow list
  graphjin cli schema tables
  graphjin cli health
  graphjin cli config show

Use --server to target a specific server (default: http://localhost:8080/).`,
	}

	// --server for pointing at the running GraphJin instance. Reuses the same
	// mcpServerURL package variable as the mcp proxy mode.
	c.PersistentFlags().StringVar(&mcpServerURL, "server", "",
		"GraphJin server URL (env: GRAPHJIN_SERVER, default: http://localhost:8080/)")

	// Shared client flags: --token, --header, --timeout, --format.
	addMCPClientFlags(c)

	c.AddCommand(mcpQueryCmd())
	c.AddCommand(mcpFragmentCmd())
	c.AddCommand(mcpWorkflowCmd())
	c.AddCommand(mcpSchemaCmd())
	c.AddCommand(mcpExplainCmd())
	c.AddCommand(mcpAuditCmd())
	c.AddCommand(mcpHealthCmd())
	c.AddCommand(mcpConfigCmd())

	return c
}
