package main

import "github.com/spf13/cobra"

// mcpConfigCmd wires `graphjin cli config show [section]` which calls the
// get_current_config MCP tool. Server-gated: the server only exposes this
// when not running in Production mode.
func mcpConfigCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "config",
		Short: "Inspect server configuration",
	}
	c.AddCommand(&cobra.Command{
		Use:   "show [section]",
		Short: "Print current config (MCP: get_current_config)",
		Long: `Print the running server's current configuration.

Optional section: databases, tables, roles, blocklist, functions, resolvers,
or all (default).

Server-gated: get_current_config is disabled when the server runs in
production mode (conf.Serv.Production).`,
		Args: cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			section := ""
			if len(args) == 1 {
				section = args[0]
			}
			runToolCmd(cmd, "get_current_config", map[string]any{
				"section": section,
			})
		},
	})
	return c
}
