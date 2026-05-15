package serv

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/dosco/graphjin/core/v3"
	"github.com/mark3labs/mcp-go/mcp"
)

// Workflow metadata is stored as a JSON header comment in the JS file:
//
//	// @graphjin-workflow {"description":"...","tags":["a","b"],"variables":[{"name":"customer_id","type":"number","required":true}]}
//
// followed by the JS code.
const workflowMetaPrefix = "// @graphjin-workflow "

// WorkflowMeta holds discoverable metadata for a saved workflow.
type WorkflowMeta struct {
	Description string             `json:"description"`
	Tags        []string           `json:"tags,omitempty"`
	Variables   []WorkflowVariable `json:"variables,omitempty"`
	CreatedAt   string             `json:"created_at,omitempty"`
	UpdatedAt   string             `json:"updated_at,omitempty"`
}

// WorkflowVariable describes one input variable expected by a saved workflow.
type WorkflowVariable struct {
	Name        string `json:"name"`
	Type        string `json:"type,omitempty"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// WorkflowInfo is returned by list_workflows.
type WorkflowInfo struct {
	Name             string             `json:"name"`
	Description      string             `json:"description"`
	Tags             []string           `json:"tags,omitempty"`
	Variables        []WorkflowVariable `json:"variables,omitempty"`
	CreatedAt        string             `json:"created_at,omitempty"`
	UpdatedAt        string             `json:"updated_at,omitempty"`
	WorkflowRevision string             `json:"workflow_revision,omitempty"`
}

var workflowNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)
var workflowVariableNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// registerWorkflowMgmtTools registers save_workflow and list_workflows.
func (ms *mcpServer) registerWorkflowMgmtTools() {
	if !ms.service.conf.legacyMCPToolsEnabled() {
		return
	}
	if ms.service.conf.legacyMCPToolsEnabled() {
		ms.srv.AddTool(mcp.NewTool(
			"list_workflows",
			mcp.WithDescription("Legacy workflow discovery tool. Prefer query_catalog(where: {kind: {eq: 'workflow'}}). List saved JavaScript workflows in ./workflows/."),
		), ms.handleListWorkflows)
	}

	// save_workflow — gated by AllowWorkflowUpdates
	if ms.service.conf.MCP.AllowWorkflowUpdates {
		ms.srv.AddTool(mcp.NewTool(
			"save_workflow",
			mcp.WithDescription("Compatibility tool for the GraphQL control-plane mutation gj_workflow(insert/update/delete). Save a JavaScript workflow to ./workflows/<name>.js. "+
				"The workflow can then be executed with gj_workflow_execution(insert), or execute_workflow when the legacy MCP tool is enabled. "+
				"Call get_js_runtime_api FIRST to learn the available gj.tools.* functions. "+
				"The code MUST define a `function main(input) { ... }` entry point. "+
				"Declare reusable workflow variables in metadata so callers know what to pass. "+
				"Use gj.tools.* to call MCP tools (e.g., gj.tools.queryCatalog({where:{kind:{eq:'table'}}}), "+
				"gj.tools.getCatalogCard({id:'table:...'}), gj.tools.executeSavedQuery({name:'...'}))."),
			mcp.WithString("name",
				mcp.Required(),
				mcp.Description("Workflow name (alphanumeric, hyphens, underscores; max 64 chars). "+
					"Use descriptive snake_case names like 'order_pnl' or 'customer_lifetime_value'."),
			),
			mcp.WithString("description",
				mcp.Required(),
				mcp.Description("Human-readable description of what this workflow does. "+
					"Future queries will match against this to find reusable workflows."),
			),
			mcp.WithString("code",
				mcp.Required(),
				mcp.Description("JavaScript source code. Must define function main(input) { ... }. "+
					"Use gj.tools.* for database access. Return the result object from main()."),
			),
			mcp.WithArray("tags",
				mcp.Description("Optional list of tags for discoverability (e.g., [\"orders\", \"finance\", \"pnl\"])"),
				mcp.WithStringItems(),
			),
			mcp.WithArray("variables",
				mcp.Description("Optional workflow input variable metadata. Declare variables here so callers know what execute_workflow must provide."),
				mcp.Items(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name": map[string]any{
							"type":        "string",
							"description": "Variable name read from input.<name>",
						},
						"type": map[string]any{
							"type":        "string",
							"description": "Type hint such as string, number, boolean, object, or array",
						},
						"description": map[string]any{
							"type":        "string",
							"description": "Human-readable description of the variable",
						},
						"required": map[string]any{
							"type":        "boolean",
							"description": "Whether execute_workflow requires this variable",
						},
					},
					"required":             []string{"name"},
					"additionalProperties": false,
				}),
			),
		), ms.handleSaveWorkflow)
	}
}

// handleListWorkflows returns all workflows with their metadata.
func (ms *mcpServer) handleListWorkflows(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	snap := ms.service.workflowSnapshot(ms.service.workflowTimeoutSeconds())
	workflows := make([]WorkflowInfo, 0, len(snap.workflows))
	for _, wf := range snap.workflows {
		info := WorkflowInfo{
			Name:             wf.Name,
			Description:      wf.Description,
			Tags:             append([]string(nil), wf.Tags...),
			Variables:        workflowVariablesFromCatalog(wf.Variables),
			CreatedAt:        wf.CreatedAt,
			UpdatedAt:        wf.UpdatedAt,
			WorkflowRevision: snap.revision,
		}
		workflows = append(workflows, info)
	}

	result := map[string]any{
		"workflows": workflows,
		"count":     len(workflows),
	}
	return ms.toolResultJSON("list_workflows", args, result)
}

// handleSaveWorkflow saves LLM-authored JS code as a workflow file.
func (ms *mcpServer) handleSaveWorkflow(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if !ms.service.conf.MCP.AllowWorkflowUpdates {
		return mcp.NewToolResultError("workflow updates are not allowed. Enable allow_workflow_updates in MCP config."), nil
	}

	args := req.GetArguments()
	row, err := newControlPlaneGraphQL(ms.service).mutateWorkflow(core.ManagedMutationRoot{
		Operation: "insert",
		Input:     args,
	})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	result := map[string]any{
		"saved":       true,
		"name":        row["name"],
		"path":        row["path"],
		"description": row["description"],
		"tags":        row["tags"],
		"variables":   row["variables"],
		"source_hash": row["source_hash"],
		"created_at":  row["created_at"],
		"updated_at":  row["updated_at"],
		"hint":        "Now run gj_workflow_execution(insert), or call execute_workflow when the legacy MCP tool is enabled, with name: " + fmt.Sprint(row["name"]),
	}
	return ms.toolResultJSON("save_workflow", args, result)
}

// parseWorkflowMeta extracts metadata from the first line of a workflow file.
func parseWorkflowMeta(src string) (WorkflowMeta, bool) {
	firstLine := src
	if idx := strings.IndexByte(src, '\n'); idx >= 0 {
		firstLine = src[:idx]
	}

	if !strings.HasPrefix(firstLine, workflowMetaPrefix) {
		return WorkflowMeta{}, false
	}

	jsonStr := strings.TrimPrefix(firstLine, workflowMetaPrefix)
	var meta WorkflowMeta
	if err := json.Unmarshal([]byte(jsonStr), &meta); err != nil {
		return WorkflowMeta{}, false
	}
	return meta, true
}

func parseWorkflowVariables(raw any) ([]WorkflowVariable, error) {
	if raw == nil {
		return nil, nil
	}

	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("variables must be an array of objects")
	}

	vars := make([]WorkflowVariable, 0, len(items))
	seen := make(map[string]struct{}, len(items))

	for i, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("variables[%d] must be an object", i)
		}

		v, err := parseWorkflowVariable(m)
		if err != nil {
			return nil, fmt.Errorf("variables[%d]: %w", i, err)
		}
		if _, exists := seen[v.Name]; exists {
			return nil, fmt.Errorf("variables[%d]: duplicate variable name %q", i, v.Name)
		}
		seen[v.Name] = struct{}{}
		vars = append(vars, v)
	}

	return vars, nil
}

func parseWorkflowVariable(m map[string]any) (WorkflowVariable, error) {
	var v WorkflowVariable

	name, _ := m["name"].(string)
	name = strings.TrimSpace(name)
	if name == "" {
		return v, fmt.Errorf("name is required")
	}
	if !workflowVariableNameRe.MatchString(name) {
		return v, fmt.Errorf("invalid variable name %q", name)
	}
	v.Name = name

	if kind, ok := m["type"].(string); ok {
		v.Type = strings.TrimSpace(kind)
	}
	if description, ok := m["description"].(string); ok {
		v.Description = strings.TrimSpace(description)
	}
	if required, ok := m["required"].(bool); ok {
		v.Required = required
	}

	return v, nil
}

func workflowVariablesFromCatalog(vars []core.CatalogWorkflowVariable) []WorkflowVariable {
	out := make([]WorkflowVariable, len(vars))
	for i, v := range vars {
		out[i] = WorkflowVariable{
			Name:        v.Name,
			Type:        v.Type,
			Description: v.Description,
			Required:    v.Required,
		}
	}
	return out
}
