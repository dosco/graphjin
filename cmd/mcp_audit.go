package main

import "github.com/spf13/cobra"

// mcpAuditCmd wires `graphjin mcp audit [role]` which calls the
// audit_role_permissions MCP tool (server-gated: AllowDevTools).
func mcpAuditCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "audit [role]",
		Short: "Show permission matrix for a role (or all) (MCP: audit_role_permissions)",
		Long: `Return a complete permission matrix for one role or every role.

With no argument, audits every role. Pass a role name (e.g. anon, user) to
limit the output.

Server-gated: requires mcp.allow_dev_tools.`,
		Args: cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			role := ""
			if len(args) == 1 {
				role = args[0]
			}
			runToolCmd(cmd, "audit_role_permissions", map[string]any{
				"role": role,
			})
		},
	}
}
