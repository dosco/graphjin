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
		Short:       "Deprecated alias for `deeporg mcp add claude`",
		Deprecated:  "use `deeporg mcp add claude` instead",
		ForceClient: "claude",
		HideClient:  true,
		Long: `Backward-compatible alias for Claude plugin installation.

Equivalent to:
  deeporg mcp add claude`,
	})
	return c
}
