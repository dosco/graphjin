package main

import (
	"github.com/spf13/cobra"
)

func cliCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "cli",
		Short: "HTTP client commands against a running GraphJin server",
		Long: `Send commands to a running GraphJin server via the MCP HTTP/JSON-RPC API.

Configuration is read from ~/.config/graphjin/client.json. Run
` + "`graphjin cli setup <server-url>`" + ` once to point this CLI at a server and
sign in. After that, every subcommand below uses the saved server + token.

Examples:
  graphjin cli setup http://localhost:8080
  graphjin cli list_tables
  graphjin cli describe_table --args '{"table":"users"}'
  graphjin cli query_syntax
  graphjin cli resources list
  graphjin cli resources read graphjin://syntax/query
  graphjin cli execute_graphql --args '{"query":"query { users { id } }"}'
  graphjin cli check_health`,
	}

	// Shared client flags: --header, --timeout, --format.
	addMCPClientFlags(c)

	c.AddCommand(setupCmd())

	for _, cmd := range mcpParityToolCmds() {
		c.AddCommand(cmd)
	}
	for _, cmd := range mcpParityResourceCmds() {
		c.AddCommand(cmd)
	}
	c.AddCommand(mcpResourcesCmd())

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
