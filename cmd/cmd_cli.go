package main

import (
	"github.com/spf13/cobra"
)

func cliCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "cli",
		Short: "HTTP client commands against a running GraphJin server",
		Long: `Send commands to a running GraphJin server via the MCP HTTP/JSON-RPC API.

Configuration is read from ~/.config/graphjin/client.json — run
` + "`deeporg cli setup <server-url>`" + ` once to point this CLI at a server and
sign in. After that, every subcommand below uses the saved server + token.

Examples:
  deeporg cli setup http://localhost:8080
  deeporg cli query list
  deeporg cli fragment list
  deeporg cli workflow list
  deeporg cli schema tables
  deeporg cli health
  deeporg cli config show`,
	}

	// Shared client flags: --header, --timeout, --format. There is intentionally
	// no --server / --token — those live in client.json (see cli setup).
	addMCPClientFlags(c)

	c.AddCommand(setupCmd())
	c.AddCommand(mcpQueryCmd())
	c.AddCommand(mcpFragmentCmd())
	c.AddCommand(mcpWorkflowCmd())
	c.AddCommand(mcpArtifactsCmd())
	c.AddCommand(mcpSchemaCmd())
	c.AddCommand(mcpExplainCmd())
	c.AddCommand(mcpAuditCmd())
	c.AddCommand(mcpHealthCmd())
	c.AddCommand(mcpConfigCmd())

	// Tool-name parity: every MCP tool is a top-level subcommand.
	for _, cmd := range mcpParityToolCmds() {
		c.AddCommand(cmd)
	}
	for _, cmd := range mcpParityResourceCmds() {
		c.AddCommand(cmd)
	}
	c.AddCommand(mcpResourcesCmd())

	return c
}
