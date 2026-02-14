package serv

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
)

// registerAuditTools registers the audit_role_permissions tool
func (ms *mcpServer) registerAuditTools() {
	if !ms.service.conf.MCP.AllowDevTools {
		return
	}

	ms.srv.AddTool(mcp.NewTool(
		"audit_role_permissions",
		mcp.WithDescription("Get a complete permission matrix for a role (or all roles). "+
			"Returns per-table operation status (query/insert/update/delete), column restrictions, "+
			"filters, presets, and row limits. Includes a fix_guide showing how to modify permissions "+
			"using update_current_config."),
		mcp.WithString("role",
			mcp.Description("Role name to audit (e.g., 'anon', 'user'). Omit or pass 'all' to audit every role."),
		),
	), ms.handleAuditRolePermissions)
}

// handleAuditRolePermissions returns the permission audit for one or all roles
func (ms *mcpServer) handleAuditRolePermissions(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := ms.requireDB(); err != nil {
		return err, nil
	}

	args := req.GetArguments()
	role, _ := args["role"].(string)

	var result interface{}
	var err error

	if role == "" || role == "all" {
		result, err = ms.service.gj.AuditAllRoles()
	} else {
		result, err = ms.service.gj.AuditRolePermissions(role)
	}
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	data, err := mcpMarshalJSON(result, true)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}
