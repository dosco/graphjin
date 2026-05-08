package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// mcpQueryCmd wires `graphjin cli query ...` subcommands that call the
// list_saved_queries / search_saved_queries / get_saved_query /
// execute_saved_query / execute_graphql MCP tools on a running GraphJin
// server. Output is JSON on stdout by default; server-side gates (e.g.
// AllowRawQueries for `exec`) are respected and their errors surface verbatim.
func mcpQueryCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "query",
		Short: "Inspect and execute saved GraphQL queries on a running GraphJin server",
		Long: `MCP-client commands for saved queries and raw GraphQL execution.

Uses the server saved in ~/.config/graphjin/client.json (set with
` + "`graphjin cli setup <server-url>`" + `) and calls the corresponding MCP
tool over HTTP.

Output is JSON on stdout so that results compose with | jq, | grep, > file.
Use --format=table for a human-readable flat-list view where applicable.`,
	}
	c.AddCommand(mcpQueryListCmd())
	c.AddCommand(mcpQuerySearchCmd())
	c.AddCommand(mcpQueryGetCmd())
	c.AddCommand(mcpQueryRunCmd())
	c.AddCommand(mcpQueryExecCmd())
	c.AddCommand(mcpQuerySubscribeCmd())
	return c
}

func mcpQueryListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all saved queries (MCP: list_saved_queries)",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			runToolCmd(cmd, "list_saved_queries", nil)
		},
	}
}

func mcpQuerySearchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "search <term>",
		Short: "Fuzzy-search saved queries by name or tags (MCP: search_saved_queries)",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			runToolCmd(cmd, "search_saved_queries", map[string]any{
				"query": args[0],
			})
		},
	}
}

func mcpQueryGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <name>",
		Short: "Show a saved query's source and metadata (MCP: get_saved_query)",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			runToolCmd(cmd, "get_saved_query", map[string]any{
				"name": args[0],
			})
		},
	}
}

var (
	mcpQueryRunVars      string
	mcpQueryRunVarsFile  string
	mcpQueryRunNamespace string
)

func mcpQueryRunCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "run <name>",
		Short: "Execute a saved query by name (MCP: execute_saved_query)",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			vars, err := loadVars(mcpQueryRunVars, mcpQueryRunVarsFile)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %s\n", err)
				os.Exit(1)
			}
			runToolCmd(cmd, "execute_saved_query", map[string]any{
				"name":      args[0],
				"variables": vars,
				"namespace": mcpQueryRunNamespace,
			})
		},
	}
	c.Flags().StringVar(&mcpQueryRunVars, "vars", "", "Variables as a JSON object string")
	c.Flags().StringVar(&mcpQueryRunVarsFile, "vars-file", "", "Variables from a JSON file (use '-' for stdin)")
	c.Flags().StringVar(&mcpQueryRunNamespace, "namespace", "", "Namespace for multi-tenant deployments")
	return c
}

var (
	mcpQueryExecVars      string
	mcpQueryExecVarsFile  string
	mcpQueryExecNamespace string
)

func mcpQueryExecCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "exec <graphql|->",
		Short: "Execute a raw GraphQL query (MCP: execute_graphql, server-gated)",
		Long: `Execute a raw GraphQL query or mutation against the server.

The server must have mcp.allow_raw_queries enabled, otherwise this command
returns the server's error and exits non-zero. Mutations additionally
require mcp.allow_mutations.

Pass the GraphQL body as a positional argument, or "-" to read from stdin:

  graphjin cli query exec 'query { users { id email } }'
  cat query.graphql | graphjin cli query exec -`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			query, err := readGraphQLArg(args[0])
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %s\n", err)
				os.Exit(1)
			}
			vars, err := loadVars(mcpQueryExecVars, mcpQueryExecVarsFile)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %s\n", err)
				os.Exit(1)
			}
			runToolCmd(cmd, "execute_graphql", map[string]any{
				"query":     query,
				"variables": vars,
				"namespace": mcpQueryExecNamespace,
			})
		},
	}
	c.Flags().StringVar(&mcpQueryExecVars, "vars", "", "Variables as a JSON object string")
	c.Flags().StringVar(&mcpQueryExecVarsFile, "vars-file", "", "Variables from a JSON file (use '-' for stdin)")
	c.Flags().StringVar(&mcpQueryExecNamespace, "namespace", "", "Namespace for multi-tenant deployments")
	return c
}

var (
	mcpQuerySubVars      string
	mcpQuerySubVarsFile  string
	mcpQuerySubNamespace string
)

func mcpQuerySubscribeCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "subscribe <graphql|->",
		Short: "Subscribe to a GraphQL subscription and stream results until interrupted",
		Long: `Open a GraphQL subscription against the configured server and print
each pushed result as JSON until the process is interrupted (Ctrl-C / SIGTERM)
or the server ends the subscription.

The transport is Server-Sent Events: POST /api/v1/graphql with
Accept: text/event-stream. The same exchange can be reproduced with curl:

  curl -N -H 'Accept: text/event-stream' -H 'Content-Type: application/json' \
       -d '{"query":"subscription { users { id email } }"}' \
       http://localhost:8080/api/v1/graphql

Pass the GraphQL body as a positional argument, or "-" to read from stdin:

  graphjin cli query subscribe 'subscription { users { id email } }'
  cat sub.graphql | graphjin cli query subscribe -

Note: the default /api/v1/graphql route resolves namespace from the route, not
from the request body — --namespace is forwarded but a non-default value only
takes effect if the server is configured with a namespace-prefixed route. For
header-keyed namespace setups, use --header "X-Namespace: <ns>" instead.`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			query, err := readGraphQLArg(args[0])
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %s\n", err)
				os.Exit(1)
			}
			vars, err := loadVars(mcpQuerySubVars, mcpQuerySubVarsFile)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %s\n", err)
				os.Exit(1)
			}
			runSubscribe(cmd.Context(), query, vars, mcpQuerySubNamespace)
		},
	}
	c.Flags().StringVar(&mcpQuerySubVars, "vars", "", "Variables as a JSON object string")
	c.Flags().StringVar(&mcpQuerySubVarsFile, "vars-file", "", "Variables from a JSON file (use '-' for stdin)")
	c.Flags().StringVar(&mcpQuerySubNamespace, "namespace", "", "Namespace for multi-tenant deployments (header-keyed: use --header instead)")
	return c
}
