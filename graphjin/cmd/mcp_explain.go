package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	mcpExplainRole     string
	mcpExplainVars     string
	mcpExplainVarsFile string
)

// mcpExplainCmd wires `graphjin mcp explain <graphql>` which compiles a query
// via the explain_query MCP tool without executing it.
// Server-gated: requires mcp.allow_dev_tools.
func mcpExplainCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "explain <graphql|->",
		Short: "Compile a query into SQL without executing (MCP: explain_query)",
		Long: `Compile a GraphQL query into SQL and return the plan without running it.

Server-gated: requires mcp.allow_dev_tools. Pass "-" to read GraphQL from
stdin, e.g.:

  graphjin mcp explain 'query { users { id } }' --role user
  cat query.graphql | graphjin mcp explain - --role anon`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			query, err := readGraphQLArg(args[0])
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %s\n", err)
				os.Exit(1)
			}
			vars, err := loadVars(mcpExplainVars, mcpExplainVarsFile)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %s\n", err)
				os.Exit(1)
			}
			runToolCmd(cmd, "explain_query", map[string]any{
				"query":     query,
				"role":      mcpExplainRole,
				"variables": vars,
			})
		},
	}
	c.Flags().StringVar(&mcpExplainRole, "role", "", "Role to compile the query as (e.g. anon, user)")
	c.Flags().StringVar(&mcpExplainVars, "vars", "", "Variables as a JSON object string")
	c.Flags().StringVar(&mcpExplainVarsFile, "vars-file", "", "Variables from a JSON file (use '-' for stdin)")
	return c
}
