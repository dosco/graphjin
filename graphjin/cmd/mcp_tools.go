package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/dosco/graphjin/serv/v3"
	"github.com/spf13/cobra"
)

// mcpExactToolCmd exposes one MCP tool directly on the cli surface using the
// exact tool name. Arguments are passed as a JSON object via --args or
// --args-file so the command stays name-stable and low-hallucination even when
// the tool's schema is more complex.
func mcpExactToolCmd(toolName string) *cobra.Command {
	var toolArgs string
	var toolArgsFile string

	short := "Call the MCP tool " + mcpToolDisplayName(toolName) + " directly"
	long := fmt.Sprintf(`Direct MCP tool access for %s.

Pass arguments as a JSON object with --args or --args-file:

  graphjin cli %s
  graphjin cli %s --args '{"example":"value"}'

This command is the exact tool name exposed by the server.`, toolName, toolName, toolName)

	c := &cobra.Command{
		Use:   toolName,
		Short: short,
		Long:  long,
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			args, err := loadVars(toolArgs, toolArgsFile)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %s\n", err)
				os.Exit(1)
			}
			runToolCmd(cmd, toolName, args)
		},
	}

	c.Flags().StringVar(&toolArgs, "args", "", "Tool arguments as a JSON object string")
	c.Flags().StringVar(&toolArgsFile, "args-file", "", "Tool arguments from a JSON file (use '-' for stdin)")
	return c
}

// mcpParityToolCmds returns direct CLI commands for every MCP tool the server
// may expose. The command names match MCP tool names exactly.
func mcpParityToolCmds() []*cobra.Command {
	toolNames := serv.MCPAllToolNames()
	cmds := make([]*cobra.Command, 0, len(toolNames))
	for _, name := range toolNames {
		cmds = append(cmds, mcpExactToolCmd(name))
	}
	return cmds
}

// mcpExactToolNames is handy for tests that want to assert the visible CLI
// surface without having to inspect every command tree branch.
func mcpExactToolNames() []string {
	cmds := mcpParityToolCmds()
	names := make([]string, 0, len(cmds))
	for _, cmd := range cmds {
		names = append(names, cmd.Use)
	}
	return names
}

func mcpToolDisplayName(toolName string) string {
	return strings.ReplaceAll(toolName, "_", " ")
}
