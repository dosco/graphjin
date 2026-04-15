package main

import "github.com/spf13/cobra"

// mcpFragmentCmd wires `graphjin mcp fragment ...` subcommands that call the
// list_fragments / search_fragments / get_fragment MCP tools.
func mcpFragmentCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "fragment",
		Short: "Inspect GraphQL fragments defined on a running GraphJin server",
	}
	c.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all GraphQL fragments (MCP: list_fragments)",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			runToolCmd(cmd, "list_fragments", nil)
		},
	})
	c.AddCommand(&cobra.Command{
		Use:   "search <term>",
		Short: "Fuzzy-search fragments by name (MCP: search_fragments)",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			runToolCmd(cmd, "search_fragments", map[string]any{"query": args[0]})
		},
	})
	c.AddCommand(&cobra.Command{
		Use:   "get <name>",
		Short: "Show a fragment's definition and usage (MCP: get_fragment)",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			runToolCmd(cmd, "get_fragment", map[string]any{"name": args[0]})
		},
	})
	return c
}
