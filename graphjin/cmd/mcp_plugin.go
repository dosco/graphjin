package main

import "github.com/spf13/cobra"

func mcpPluginCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "plugin",
		Short: "Compatibility aliases for Claude plugin commands",
	}

	c.AddCommand(mcpPluginInstallCmd())
	return c
}

func mcpPluginInstallCmd() *cobra.Command {
	c := newMCPInstallCommand(mcpInstallCommandConfig{
		Use:         "install",
		Short:       "Alias for `graphjin mcp install --client claude`",
		ForceClient: "claude",
		HideClient:  true,
		Long: `Backward-compatible alias for Claude plugin installation.

Equivalent to:
  graphjin mcp install --client claude`,
	})
	return c
}
