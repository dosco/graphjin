package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// mcpWorkflowCmd wires `graphjin mcp workflow ...` subcommands.
func mcpWorkflowCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "workflow",
		Short: "List and run JavaScript workflows on a running GraphJin server",
	}
	c.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List saved workflows (MCP: list_workflows)",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			runToolCmd(cmd, "list_workflows", nil)
		},
	})
	c.AddCommand(mcpWorkflowRunCmd())
	return c
}

var (
	mcpWorkflowRunVars      string
	mcpWorkflowRunVarsFile  string
	mcpWorkflowRunNamespace string
)

func mcpWorkflowRunCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "run <name>",
		Short: "Execute a named workflow (MCP: execute_workflow)",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			vars, err := loadVars(mcpWorkflowRunVars, mcpWorkflowRunVarsFile)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %s\n", err)
				os.Exit(1)
			}
			runToolCmd(cmd, "execute_workflow", map[string]any{
				"name":      args[0],
				"variables": vars,
				"namespace": mcpWorkflowRunNamespace,
			})
		},
	}
	c.Flags().StringVar(&mcpWorkflowRunVars, "vars", "", "Workflow input as a JSON object string")
	c.Flags().StringVar(&mcpWorkflowRunVarsFile, "vars-file", "", "Workflow input from a JSON file (use '-' for stdin)")
	c.Flags().StringVar(&mcpWorkflowRunNamespace, "namespace", "", "Namespace for multi-tenant deployments")
	return c
}
