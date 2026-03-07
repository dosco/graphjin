package serv

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

// Workflow metadata is stored as a JSON header comment in the JS file:
//   // @graphjin-workflow {"description":"...","tags":["a","b"]}
// followed by the JS code.
const workflowMetaPrefix = "// @graphjin-workflow "

// WorkflowMeta holds discoverable metadata for a saved workflow.
type WorkflowMeta struct {
	Description string   `json:"description"`
	Tags        []string `json:"tags,omitempty"`
}

// WorkflowInfo is returned by list_workflows.
type WorkflowInfo struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags,omitempty"`
}

var workflowNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

// registerWorkflowMgmtTools registers save_workflow and list_workflows.
func (ms *mcpServer) registerWorkflowMgmtTools() {
	// list_workflows — always available (read-only)
	ms.srv.AddTool(mcp.NewTool(
		"list_workflows",
		mcp.WithDescription("List all saved JavaScript workflows in ./workflows/. "+
			"Returns name, description, and tags for each workflow. "+
			"Check here FIRST before authoring a new workflow — a reusable one may already exist."),
	), ms.handleListWorkflows)

	// save_workflow — gated by AllowWorkflowUpdates
	if ms.service.conf.MCP.AllowWorkflowUpdates {
		ms.srv.AddTool(mcp.NewTool(
			"save_workflow",
			mcp.WithDescription("Save a JavaScript workflow to ./workflows/<name>.js. "+
				"The workflow can then be executed with execute_workflow. "+
				"Call get_js_runtime_api FIRST to learn the available gj.tools.* functions. "+
				"The code MUST define a `function main(input) { ... }` entry point. "+
				"Use gj.tools.* to call MCP tools (e.g., gj.tools.listTables(), "+
				"gj.tools.describeTable({name:'orders'}), gj.tools.executeGraphql({query:'...'}))."),
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
			mcp.WithObject("tags",
				mcp.Description("Optional list of tags for discoverability (e.g., [\"orders\", \"finance\", \"pnl\"])"),
			),
		), ms.handleSaveWorkflow)
	}
}

// handleListWorkflows returns all workflows with their metadata.
func (ms *mcpServer) handleListWorkflows(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	files, err := ms.service.fs.List(workflowsPath)
	if err != nil {
		// No workflows directory yet — return empty list
		data, _ := mcpMarshalJSON(map[string]any{"workflows": []any{}}, true)
		return mcp.NewToolResultText(string(data)), nil
	}

	workflows := make([]WorkflowInfo, 0, len(files))
	for _, name := range files {
		if !strings.HasSuffix(name, workflowExt) {
			continue
		}

		baseName := strings.TrimSuffix(name, workflowExt)
		info := WorkflowInfo{Name: baseName}

		// Try to read metadata from file header
		src, err := ms.service.fs.Get(filepath.Join(workflowsPath, name))
		if err == nil {
			if meta, ok := parseWorkflowMeta(string(src)); ok {
				info.Description = meta.Description
				info.Tags = meta.Tags
			}
		}

		workflows = append(workflows, info)
	}

	data, err := mcpMarshalJSON(map[string]any{
		"workflows": workflows,
		"count":     len(workflows),
	}, true)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

// handleSaveWorkflow saves LLM-authored JS code as a workflow file.
func (ms *mcpServer) handleSaveWorkflow(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if !ms.service.conf.MCP.AllowWorkflowUpdates {
		return mcp.NewToolResultError("workflow updates are not allowed. Enable allow_workflow_updates in MCP config."), nil
	}

	args := req.GetArguments()
	name, _ := args["name"].(string)
	description, _ := args["description"].(string)
	code, _ := args["code"].(string)

	if name == "" {
		return mcp.NewToolResultError("name is required"), nil
	}
	if description == "" {
		return mcp.NewToolResultError("description is required"), nil
	}
	if code == "" {
		return mcp.NewToolResultError("code is required"), nil
	}

	// Validate name
	if !workflowNameRe.MatchString(name) {
		return mcp.NewToolResultError(
			"invalid workflow name: must be alphanumeric with hyphens/underscores, 1-64 chars"), nil
	}

	// Validate code contains main function
	if !strings.Contains(code, "function main") {
		return mcp.NewToolResultError(
			"code must define a `function main(input) { ... }` entry point"), nil
	}

	// Build tags
	var tags []string
	if rawTags, ok := args["tags"]; ok {
		switch v := rawTags.(type) {
		case []any:
			for _, t := range v {
				if s, ok := t.(string); ok {
					tags = append(tags, s)
				}
			}
		case map[string]any:
			// MCP object type — extract values if they're strings
			for _, val := range v {
				if s, ok := val.(string); ok {
					tags = append(tags, s)
				}
			}
		}
	}

	// Build metadata header
	meta := WorkflowMeta{
		Description: description,
		Tags:        tags,
	}
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to encode metadata: %v", err)), nil
	}

	// Compose file: metadata header + code
	var sb strings.Builder
	sb.WriteString(workflowMetaPrefix)
	sb.Write(metaJSON)
	sb.WriteString("\n")
	sb.WriteString(code)
	if !strings.HasSuffix(code, "\n") {
		sb.WriteString("\n")
	}

	// Write to filesystem
	filePath := filepath.Join(workflowsPath, name+workflowExt)
	if err := ms.service.fs.Put(filePath, []byte(sb.String())); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to save workflow: %v", err)), nil
	}

	data, err := mcpMarshalJSON(map[string]any{
		"saved":       true,
		"name":        name,
		"path":        filePath,
		"description": description,
		"tags":        tags,
		"hint":        "Now call execute_workflow with name: " + name,
	}, true)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(data)), nil
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
