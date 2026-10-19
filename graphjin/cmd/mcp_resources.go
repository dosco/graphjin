package main

import (
	"context"
	"fmt"
	"os"

	"github.com/dosco/graphjin/serv/v3"
	"github.com/spf13/cobra"
)

func mcpResourcesCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "resources",
		Short: "List and read MCP resources",
		Long: `Direct MCP resource access.

Use resources list to inspect the resource catalog, or resources read <uri>
to fetch any resource by URI.`,
	}
	c.AddCommand(mcpResourcesListCmd())
	c.AddCommand(mcpResourcesReadCmd())
	return c
}

func mcpResourcesListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List MCP resources exposed by the server",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			mcpClientRedirectLog()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			payload, err := callMCPMethod(ctx, cmd, "resources/list", nil)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %s\n", err)
				os.Exit(1)
			}
			if err := emitResult(payload); err != nil {
				fmt.Fprintf(os.Stderr, "error: %s\n", err)
				os.Exit(1)
			}
		},
	}
}

func mcpResourcesReadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "read <uri>",
		Short: "Read an MCP resource by URI",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			mcpClientRedirectLog()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			payload, err := readMCPResource(ctx, cmd, args[0])
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %s\n", err)
				os.Exit(1)
			}
			if err := emitResult(payload); err != nil {
				fmt.Fprintf(os.Stderr, "error: %s\n", err)
				os.Exit(1)
			}
		},
	}
}

func mcpParityResourceCmds() []*cobra.Command {
	specs := serv.MCPCLIResources()
	cmds := make([]*cobra.Command, 0, len(specs))
	for _, spec := range specs {
		cmds = append(cmds, mcpResourceShortcutCmd(spec.Command, spec.Short, spec.URI))
	}
	return cmds
}

func mcpResourceShortcutCmd(name, short, uri string) *cobra.Command {
	return &cobra.Command{
		Use:   name,
		Short: short,
		Long: fmt.Sprintf(`Read the MCP resource %s.

The resource URI is %s.`, name, uri),
		Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			mcpClientRedirectLog()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			payload, err := readMCPResource(ctx, cmd, uri)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %s\n", err)
				os.Exit(1)
			}
			if err := emitResult(payload); err != nil {
				fmt.Fprintf(os.Stderr, "error: %s\n", err)
				os.Exit(1)
			}
		},
	}
}
