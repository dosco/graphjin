package serv

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/core/v3/openapi"
	"github.com/dosco/graphjin/core/v3/sourcecap"
	"github.com/dosco/graphjin/serv/v3/internal/mcpcompat/mcp"
	"github.com/spf13/viper"
)

// registerConfigTools registers the config read/validate/update MCP tools.
// Dev mode only: effectiveMode is the authoritative mode selector, so agentic
// and prod deployments register nothing here (agentic canonicalizes to
// production, and effectiveMode reports it as non-dev even before that
// normalization runs). registerTools calls this outside the agentOnlyMCP gate
// so external MCP clients keep first-class config access in dev even when the
// built-in agent is the MCP front door.
func (ms *mcpServer) registerConfigTools() {
	if effectiveMode(ms.service.conf) != modeDev {
		return
	}

	// get_current_config (read-only)
	ms.srv.AddTool(mcp.NewTool(
		"get_current_config",
		mcp.WithDescription("Get current GraphJin configuration. Returns external sources, system/workflow feature overrides, databases, relationships, tables, roles, blocklist, functions, resolvers, and MCP settings. "+
			"Use this to understand the current configuration before making changes."),
		mcp.WithString("section",
			mcp.Description("Optional section to retrieve: 'sources', 'system', 'workflows', 'databases', 'relationships', 'metadata', 'tables', 'roles', 'blocklist', 'functions', 'resolvers', 'mcp', or 'all' (default)"),
		),
	), ms.handleGetCurrentConfig)

	// The update tool definition is always built: validate_config shares its
	// payload schema even when config updates are disabled.
	updateTool := mcp.NewTool(
		"update_current_config",
		mcp.WithDescription("Compatibility tool for the GraphQL control-plane mutation gj_config(id: \"current\", update: ...). Update GraphJin configuration and automatically reload. "+
			"Legacy config changes are applied in-memory immediately. Source-mode config writes require mode: preview first, then mode: apply with the returned preview_id and the exact same payload. "+
			"Supports external sources, system/workflows feature settings, databases, relationships, MCP settings, metadata, tables, roles, blocklist, functions, and resolvers. "+
			"System database names (postgres, mysql, information_schema, master, etc.) "+
			"are rejected by default — use a user database name instead. "+
			"Use create_if_not_exists: true to create a new database on the server before connecting (dev mode only). "+
			"Response classification uses scope (core, serv, or mixed), reload_mode (hot or restart), and reload_strategy (full or source_scoped for core changes). "+
			"Response includes machine-readable next-step guidance in the `next` field. "+
			"WARNING: Changes are lost on restart unless persisted separately. "+
			"Use get_current_config first to understand the current state."),
		mcp.WithString("mode",
			mcp.Description("Source mode only. Use \"preview\" to validate and receive preview_id, then \"apply\" with the same payload plus preview_id. Legacy mode may omit this field."),
		),
		mcp.WithString("preview_id",
			mcp.Description("Source mode apply only. Returned by a successful preview; expires after 10 minutes and requires the exact same patch payload."),
		),
		mcp.WithString("expected_catalog_revision",
			mcp.Description("Source mode preview/apply guard. Read gj_config(id: \"current\") { catalog_revision } immediately before preview/apply and send that value."),
		),
		mcp.WithArray("sources",
			mcp.Description("Replace-all source list. Use update_sources/remove_sources for focused edits that preserve omitted sources."),
			mcp.Items(sourceConfigInputSchema([]string{"name", "kind"})),
		),
		mcp.WithArray("update_sources",
			mcp.Description("Merge-patch sources by name. Existing source patches require name. New source patches require name and kind. Omitted fields are preserved, null clears fields, arrays replace, nested objects merge."),
			mcp.Items(sourceConfigInputSchema([]string{"name"})),
		),
		mcp.WithArray("remove_sources",
			mcp.Description("Array of source names to remove from configuration."),
			mcp.WithStringItems(),
		),
		mcp.WithArray("source_patches",
			mcp.Description("Source mode patch-by-name updates for external sources. Preserves unmentioned source fields. Supports access read/write/delete, namespace_column, owner_column, missing_namespace_column, and public/admin/blocked table add/remove."),
			mcp.Items(map[string]any{
				"type":     "object",
				"required": []string{"name"},
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "Exact existing source name"},
					"access": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"read":                     map[string]any{"type": "string", "enum": []string{"blocked", "public", "authenticated", "account", "owner", "admin"}},
							"write":                    map[string]any{"type": "string", "enum": []string{"blocked", "authenticated", "account", "owner", "admin"}},
							"delete":                   map[string]any{"type": "string", "enum": []string{"blocked", "authenticated", "account", "owner", "admin"}},
							"namespace_column":         map[string]any{"type": "string"},
							"owner_column":             map[string]any{"type": "string"},
							"missing_namespace_column": map[string]any{"type": "string", "enum": []string{"block", "allow"}},
							"public_tables_add":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
							"public_tables_remove":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
							"admin_tables_add":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
							"admin_tables_remove":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
							"blocked_tables_add":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
							"blocked_tables_remove":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						},
					},
				},
			}),
		),
		mcp.WithObject("system",
			mcp.Description("Merge-patch built-in system capabilities and root_access. Omitted values are preserved; null removes an override and restores the mode default."),
		),
		mcp.WithObject("workflows",
			mcp.Description("Merge-patch built-in workflow path and execute/read/write capabilities. The JavaScript runtime is fixed to Goja."),
		),
		mcp.WithObject("databases",
			mcp.Description("Map of database configs to add/update. Key is database name, value is DatabaseConfig with type, host, port, dbname, user, password, read_only, infer_db_refs for CodeSQL, etc. NOTE: read_only cannot be changed from true to false at runtime if it was set in the config file."),
		),
		mcp.WithArray("tables",
			mcp.Description("Array of table configs to add/update. Each table has name, database (optional), blocklist (optional), columns (optional), order_by (optional)."),
			mcp.Items(map[string]any{
				"type":     "object",
				"required": []string{"name"},
				"properties": map[string]any{
					"name":      map[string]any{"type": "string", "description": "Table name (required)"},
					"database":  map[string]any{"type": "string", "description": "Database name"},
					"schema":    map[string]any{"type": "string", "description": "Schema name"},
					"type":      map[string]any{"type": "string", "description": "Table type"},
					"blocklist": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Columns to block"},
					"columns": map[string]any{
						"type":        "array",
						"description": "Column definitions",
						"items": map[string]any{
							"type":     "object",
							"required": []string{"name"},
							"properties": map[string]any{
								"name":       map[string]any{"type": "string", "description": "Column name"},
								"type":       map[string]any{"type": "string", "description": "Column type"},
								"primary":    map[string]any{"type": "boolean", "description": "Is primary key"},
								"array":      map[string]any{"type": "boolean", "description": "Is array type"},
								"full_text":  map[string]any{"type": "boolean", "description": "Full-text search enabled"},
								"related_to": map[string]any{"type": "string", "description": "Foreign key reference (e.g. 'users.id' or 'other_db:users.id' for cross-database)"},
							},
						},
					},
					"order_by": map[string]any{
						"type":        "object",
						"description": "Order-by configuration (keys are names, values are arrays of column strings)",
						"additionalProperties": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "string"},
						},
					},
				},
			}),
		),
		mcp.WithArray("roles",
			mcp.Description("Array of role configs to add/update. Each role has name, match (optional), and tables array with query/insert/update/delete permissions."),
			mcp.Items(map[string]any{
				"type":     "object",
				"required": []string{"name"},
				"properties": map[string]any{
					"name":    map[string]any{"type": "string", "description": "Role name (required)"},
					"comment": map[string]any{"type": "string", "description": "Role comment"},
					"match":   map[string]any{"type": "string", "description": "Match expression for role"},
					"tables": map[string]any{
						"type":        "array",
						"description": "Table permissions for this role",
						"items": map[string]any{
							"type":     "object",
							"required": []string{"name"},
							"properties": map[string]any{
								"name":      map[string]any{"type": "string", "description": "Table name"},
								"schema":    map[string]any{"type": "string", "description": "Schema name"},
								"database":  map[string]any{"type": "string", "description": "External database/source name for multi-database tables; built-in system roots do not use a public database name"},
								"read_only": map[string]any{"type": "boolean", "description": "Read-only access"},
								"query": map[string]any{
									"type":        "object",
									"description": "Query permissions",
									"properties": map[string]any{
										"limit":             map[string]any{"type": "number", "description": "Row limit"},
										"filters":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Query filters"},
										"columns":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Allowed columns"},
										"disable_functions": map[string]any{"type": "boolean", "description": "Disable functions"},
										"block":             map[string]any{"type": "boolean", "description": "Block this operation"},
									},
								},
								"insert": map[string]any{
									"type":        "object",
									"description": "Insert permissions",
									"properties": map[string]any{
										"filters": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Insert filters"},
										"columns": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Allowed columns"},
										"presets": map[string]any{"type": "object", "description": "Preset values", "additionalProperties": map[string]any{"type": "string"}},
										"block":   map[string]any{"type": "boolean", "description": "Block this operation"},
									},
								},
								"update": map[string]any{
									"type":        "object",
									"description": "Update permissions",
									"properties": map[string]any{
										"filters": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Update filters"},
										"columns": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Allowed columns"},
										"presets": map[string]any{"type": "object", "description": "Preset values", "additionalProperties": map[string]any{"type": "string"}},
										"block":   map[string]any{"type": "boolean", "description": "Block this operation"},
									},
								},
								"upsert": map[string]any{
									"type":        "object",
									"description": "Upsert permissions",
									"properties": map[string]any{
										"filters": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Upsert filters"},
										"columns": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Allowed columns"},
										"presets": map[string]any{"type": "object", "description": "Preset values", "additionalProperties": map[string]any{"type": "string"}},
										"block":   map[string]any{"type": "boolean", "description": "Block this operation"},
									},
								},
								"delete": map[string]any{
									"type":        "object",
									"description": "Delete permissions",
									"properties": map[string]any{
										"filters": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Delete filters"},
										"columns": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Allowed columns"},
										"block":   map[string]any{"type": "boolean", "description": "Block this operation"},
									},
								},
							},
						},
					},
				},
			}),
		),
		mcp.WithArray("blocklist",
			mcp.Description("Array of tables/columns to block globally. Use 'table_name' to block entire table or 'table_name.column_name' to block specific column."),
			mcp.WithStringItems(),
		),
		mcp.WithArray("functions",
			mcp.Description("Array of database function configs. Each function has name and return_type."),
			mcp.Items(map[string]any{
				"type":     "object",
				"required": []string{"name"},
				"properties": map[string]any{
					"name":        map[string]any{"type": "string", "description": "Function name (required)"},
					"schema":      map[string]any{"type": "string", "description": "Schema name"},
					"return_type": map[string]any{"type": "string", "description": "Return type of the function"},
				},
			}),
		),
		mcp.WithBoolean("create_if_not_exists",
			mcp.Description("Dev mode only. When true, creates databases on the server if they don't exist before connecting. "+
				"Works for PostgreSQL, MySQL/MariaDB, MSSQL, and Oracle. "+
				"SQLite and MongoDB create databases automatically. Snowflake is not supported for create_if_not_exists."),
		),
		mcp.WithArray("remove_databases",
			mcp.Description("Array of database names to remove from configuration."),
			mcp.WithStringItems(),
		),
		mcp.WithArray("remove_tables",
			mcp.Description("Array of table names to remove from configuration."),
			mcp.WithStringItems(),
		),
		mcp.WithArray("remove_roles",
			mcp.Description("Array of role names to remove from configuration."),
			mcp.WithStringItems(),
		),
		mcp.WithArray("remove_blocklist_items",
			mcp.Description("Array of blocklist entries to remove."),
			mcp.WithStringItems(),
		),
		mcp.WithArray("remove_functions",
			mcp.Description("Array of function names to remove from configuration."),
			mcp.WithStringItems(),
		),
		mcp.WithArray("resolvers",
			mcp.Description("Array of resolver configs to add/update. Resolvers join DB tables with remote APIs."),
			mcp.Items(map[string]any{
				"type":     "object",
				"required": []string{"name", "type", "table"},
				"properties": map[string]any{
					"name":         map[string]any{"type": "string", "description": "Resolver name, used as the virtual table name in queries (required)"},
					"type":         map[string]any{"type": "string", "description": "Resolver type: 'remote_api' (required)"},
					"table":        map[string]any{"type": "string", "description": "DB table whose column provides the $id value (required)"},
					"column":       map[string]any{"type": "string", "description": "DB column used as $id (defaults to primary key)"},
					"schema":       map[string]any{"type": "string", "description": "DB schema name"},
					"strip_path":   map[string]any{"type": "string", "description": "Dot-path to extract from API response (e.g., 'data')"},
					"url":          map[string]any{"type": "string", "description": "Remote API URL with $id placeholder (e.g., 'http://api/payments/$id')"},
					"debug":        map[string]any{"type": "boolean", "description": "Log HTTP request/response"},
					"pass_headers": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Headers to forward from original request"},
					"set_headers": map[string]any{
						"type":        "array",
						"description": "Headers to set on remote request",
						"items": map[string]any{
							"type":     "object",
							"required": []string{"name", "value"},
							"properties": map[string]any{
								"name":  map[string]any{"type": "string", "description": "Header name"},
								"value": map[string]any{"type": "string", "description": "Header value"},
							},
						},
					},
				},
			}),
		),
		mcp.WithArray("remove_resolvers",
			mcp.Description("Array of resolver names to remove from configuration."),
			mcp.WithStringItems(),
		),
		mcp.WithObject("serv",
			mcp.Description("Merge-patch for server-side settings (serv.Config). Writable v1 keys: agent (model, structured_output_mode, response_format [deprecated alias], max_steps, timeout_seconds, read_only, return_trace, seed_limit, catalog_default_limit), log_level, log_format, web_ui, http_compress, server_timing, rate_limiter (rate, bucket, ip_header). "+
				"agent changes are read live; the rest are persisted and take effect on the next restart (automatic when reload_on_config_change is enabled). "+
				"Secret-bearing sections (auth, redis, uploads) are read-only on gj_config and cannot be patched here. scope reports serv or mixed and reload_mode reports hot or restart."),
		),
	)

	// validate_config: dry-run through the exact update pipeline (validation,
	// staged runtime build, reload-impact classification), then discard the
	// staged runtime. Shares update_current_config's payload schema minus the
	// preview-flow control args, and stays available even when config updates
	// are disabled.
	validateTool := updateTool
	validateTool.Name = "validate_config"
	validateTool.Description = "Dry-run a proposed configuration change without applying it. " +
		"Runs the same pipeline as update_current_config — validation, staged runtime build (databases connected, schema discovered), and reload-impact classification — then discards the staged runtime. " +
		"Returns valid, errors, a change summary, scope (core, serv, or mixed), reload_mode (hot or restart), and reload_strategy (full or source_scoped for core changes). The running config, catalog revision, and config file are never mutated and no preview is created. " +
		"Accepts the same payload fields as update_current_config. expected_catalog_revision is optional and only checked when provided."
	validateProps := make(map[string]any, len(updateTool.InputSchema.Properties))
	for k, v := range updateTool.InputSchema.Properties {
		if k == "mode" || k == "preview_id" {
			continue
		}
		validateProps[k] = v
	}
	validateTool.InputSchema.Properties = validateProps
	ms.srv.AddTool(validateTool, ms.handleValidateConfig)

	// update_current_config - only when allow_config_updates is enabled
	if ms.service.conf.MCP.AllowConfigUpdates {
		ms.srv.AddTool(updateTool, ms.handleUpdateCurrentConfig)
	}
}

// handleValidateConfig runs the update pipeline in validate mode: full
// validation and reload classification with a staged runtime that is always
// discarded. See handleUpdateCurrentConfig for the mode: "validate" path.
func (ms *mcpServer) handleValidateConfig(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	if args == nil {
		args = map[string]interface{}{}
	}
	args["mode"] = "validate"
	delete(args, "preview_id")
	req.Params.Arguments = args
	return ms.handleUpdateCurrentConfig(ctx, req)
}

// MCPConfigResponse represents a section of the configuration for MCP
type MCPConfigResponse struct {
	ActiveDatabase string               `json:"active_database,omitempty"`
	Sources        any                  `json:"sources,omitempty"`
	System         core.SystemConfig    `json:"system,omitempty"`
	Workflows      core.WorkflowsConfig `json:"workflows,omitempty"`
	Databases      any                  `json:"databases,omitempty"`
	Relationships  any                  `json:"relationships,omitempty"`
	Metadata       core.MetadataConfig  `json:"metadata,omitempty"`
	Tables         any                  `json:"tables,omitempty"`
	Roles          any                  `json:"roles,omitempty"`
	Blocklist      []string             `json:"blocklist,omitempty"`
	Functions      any                  `json:"functions,omitempty"`
	Resolvers      any                  `json:"resolvers,omitempty"`
	MCP            MCPConfig            `json:"mcp,omitempty"`
}

// RoleInfo provides role information safe for JSON serialization
type RoleInfo struct {
	Name    string           `json:"name"`
	Comment string           `json:"comment,omitempty"`
	Match   string           `json:"match,omitempty"`
	Tables  []core.RoleTable `json:"tables,omitempty"`
}

// handleGetCurrentConfig returns the current configuration
func (ms *mcpServer) handleGetCurrentConfig(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx = ms.effectiveContext(ctx)
	args := req.GetArguments()
	section, _ := args["section"].(string)
	if section == "" {
		section = "all"
	}

	conf := &ms.service.conf.Core
	result := MCPConfigResponse{}

	// Determine the active database
	result.ActiveDatabase = ms.getActiveDatabase()

	switch strings.ToLower(section) {
	case "sources":
		result.Sources = redactedConfigValue(conf.Sources)
	case "system":
		result.System = conf.System
	case "workflows":
		result.Workflows = conf.Workflows
	case "relationships":
		result.Relationships = redactedConfigValue(conf.Relationships)
	case "databases":
		result.Databases = redactedConfigValue(conf.Databases)
	case "metadata":
		result.Metadata = conf.Metadata
	case "tables":
		result.Tables = redactedConfigValue(conf.Tables)
	case "roles":
		result.Roles = redactedConfigValue(convertRolesToInfo(conf.Roles))
	case "blocklist":
		result.Blocklist = conf.Blocklist
	case "functions":
		result.Functions = redactedConfigValue(conf.Functions)
	case "resolvers":
		result.Resolvers = redactedConfigValue(conf.Resolvers)
	case "mcp":
		result.MCP = ms.service.conf.MCP
	case "all":
		result.Sources = redactedConfigValue(conf.Sources)
		result.System = conf.System
		result.Workflows = conf.Workflows
		result.Databases = redactedConfigValue(conf.Databases)
		result.Relationships = redactedConfigValue(conf.Relationships)
		result.Metadata = conf.Metadata
		result.Tables = redactedConfigValue(conf.Tables)
		result.Roles = redactedConfigValue(convertRolesToInfo(conf.Roles))
		result.Blocklist = conf.Blocklist
		result.Functions = redactedConfigValue(conf.Functions)
		result.Resolvers = redactedConfigValue(conf.Resolvers)
		result.MCP = ms.service.conf.MCP
	default:
		return mcp.NewToolResultError(fmt.Sprintf("unknown section: %s. Valid sections: sources, system, workflows, databases, relationships, metadata, tables, roles, blocklist, functions, resolvers, mcp, all", section)), nil
	}
	return ms.toolResultJSON("get_current_config", args, result)
}

// getActiveDatabase returns the name of the first configured database (sorted).
// All databases are equal; this returns the first entry in sorted order for determinism.
func (ms *mcpServer) getActiveDatabase() string {
	names := make([]string, 0, len(ms.service.conf.Core.Databases))
	for name := range ms.service.conf.Core.Databases {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) > 0 {
		return names[0]
	}
	return ""
}

// convertRolesToInfo converts roles to a JSON-safe format
func convertRolesToInfo(roles []core.Role) []RoleInfo {
	result := make([]RoleInfo, len(roles))
	for i, r := range roles {
		result[i] = RoleInfo{
			Name:    r.Name,
			Comment: r.Comment,
			Match:   r.Match,
			Tables:  r.Tables,
		}
	}
	return result
}

// ConfigUpdateRequest represents the update request structure
type ConfigUpdateRequest struct {
	Databases map[string]DatabaseConfigInput `json:"databases,omitempty"`
	Metadata  *core.MetadataConfig           `json:"metadata,omitempty"`
	Tables    []TableConfigInput             `json:"tables,omitempty"`
	Roles     []RoleConfigInput              `json:"roles,omitempty"`
	Blocklist []string                       `json:"blocklist,omitempty"`
	Functions []FunctionConfigInput          `json:"functions,omitempty"`
}

// DatabaseConfigInput represents a database config for input
type DatabaseConfigInput struct {
	Type        string `json:"type"`
	ConnString  string `json:"connection_string,omitempty"`
	Host        string `json:"host,omitempty"`
	Port        int    `json:"port,omitempty"`
	DBName      string `json:"dbname,omitempty"`
	User        string `json:"user,omitempty"`
	Password    string `json:"password,omitempty"`
	Path        string `json:"path,omitempty"`
	Schema      string `json:"schema,omitempty"`
	ReadOnly    bool   `json:"read_only,omitempty"`
	InferDBRefs *bool  `json:"infer_db_refs,omitempty"`
}

// TableConfigInput represents a table config for input
type TableConfigInput struct {
	Name      string              `json:"name"`
	Source    string              `json:"source,omitempty"`
	Database  string              `json:"database,omitempty"`
	ReadOnly  bool                `json:"read_only,omitempty"`
	Blocklist []string            `json:"blocklist,omitempty"`
	Columns   []ColumnConfigInput `json:"columns,omitempty"`
	OrderBy   map[string][]string `json:"order_by,omitempty"`
}

// ColumnConfigInput represents a column config for input
type ColumnConfigInput struct {
	Name       string `json:"name"`
	Type       string `json:"type,omitempty"`
	Primary    bool   `json:"primary,omitempty"`
	Array      bool   `json:"array,omitempty"`
	FullText   bool   `json:"full_text,omitempty"`
	ForeignKey string `json:"related_to,omitempty"`
}

// RoleConfigInput represents a role config for input
type RoleConfigInput struct {
	Name    string                 `json:"name"`
	Comment string                 `json:"comment,omitempty"`
	Match   string                 `json:"match,omitempty"`
	Tables  []RoleTableConfigInput `json:"tables,omitempty"`
}

// RoleTableConfigInput represents a role table config for input
type RoleTableConfigInput struct {
	Name     string             `json:"name"`
	Schema   string             `json:"schema,omitempty"`
	Database string             `json:"database,omitempty"`
	ReadOnly bool               `json:"read_only,omitempty"`
	Query    *QueryConfigInput  `json:"query,omitempty"`
	Insert   *InsertConfigInput `json:"insert,omitempty"`
	Update   *UpdateConfigInput `json:"update,omitempty"`
	Upsert   *UpsertConfigInput `json:"upsert,omitempty"`
	Delete   *DeleteConfigInput `json:"delete,omitempty"`
}

// QueryConfigInput represents query permissions
type QueryConfigInput struct {
	Limit            int      `json:"limit,omitempty"`
	Filters          []string `json:"filters,omitempty"`
	Columns          []string `json:"columns,omitempty"`
	DisableFunctions bool     `json:"disable_functions,omitempty"`
	Block            bool     `json:"block,omitempty"`
}

// InsertConfigInput represents insert permissions
type InsertConfigInput struct {
	Filters []string          `json:"filters,omitempty"`
	Columns []string          `json:"columns,omitempty"`
	Presets map[string]string `json:"presets,omitempty"`
	Block   bool              `json:"block,omitempty"`
}

// UpdateConfigInput represents update permissions
type UpdateConfigInput struct {
	Filters []string          `json:"filters,omitempty"`
	Columns []string          `json:"columns,omitempty"`
	Presets map[string]string `json:"presets,omitempty"`
	Block   bool              `json:"block,omitempty"`
}

// UpsertConfigInput represents upsert permissions
type UpsertConfigInput struct {
	Filters []string          `json:"filters,omitempty"`
	Columns []string          `json:"columns,omitempty"`
	Presets map[string]string `json:"presets,omitempty"`
	Block   bool              `json:"block,omitempty"`
}

// DeleteConfigInput represents delete permissions
type DeleteConfigInput struct {
	Filters []string `json:"filters,omitempty"`
	Columns []string `json:"columns,omitempty"`
	Block   bool     `json:"block,omitempty"`
}

// FunctionConfigInput represents a function config for input
type FunctionConfigInput struct {
	Name       string `json:"name"`
	Schema     string `json:"schema,omitempty"`
	ReturnType string `json:"return_type"`
}

// ResolverConfigInput represents a resolver config for input
type ResolverConfigInput struct {
	Name        string           `json:"name"`
	Type        string           `json:"type"`
	Schema      string           `json:"schema,omitempty"`
	Table       string           `json:"table"`
	Column      string           `json:"column,omitempty"`
	StripPath   string           `json:"strip_path,omitempty"`
	URL         string           `json:"url,omitempty"`
	Debug       bool             `json:"debug,omitempty"`
	PassHeaders []string         `json:"pass_headers,omitempty"`
	SetHeaders  []SetHeaderInput `json:"set_headers,omitempty"`
}

// SetHeaderInput represents a header name-value pair for resolver config
type SetHeaderInput struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// ConfigUpdateResult represents the result of a config update
type ConfigUpdateResult struct {
	Success           bool          `json:"success"`
	Message           string        `json:"message"`
	Changes           []string      `json:"changes,omitempty"`
	Errors            []string      `json:"errors,omitempty"`
	Databases         []string      `json:"databases,omitempty"`
	Scope             string        `json:"scope,omitempty"`
	ReloadMode        string        `json:"reload_mode,omitempty"`
	ReloadStrategy    string        `json:"reload_strategy,omitempty"`
	ChangedSources    []string      `json:"changed_sources,omitempty"`
	ReloadFallback    bool          `json:"reload_fallback,omitempty"`
	Valid             bool          `json:"valid,omitempty"`
	Applied           bool          `json:"applied,omitempty"`
	Mode              string        `json:"mode,omitempty"`
	PreviewID         string        `json:"preview_id,omitempty"`
	ExpiresAt         string        `json:"expires_at,omitempty"`
	CatalogRevision   string        `json:"catalog_revision,omitempty"`
	ChangeSummaryJSON string        `json:"change_summary_json,omitempty"`
	FindingsJSON      string        `json:"findings_json,omitempty"`
	ErrorsJSON        string        `json:"errors_json,omitempty"`
	Next              *NextGuidance `json:"next,omitempty"`
}

type configUpdateImpact struct {
	scope          string
	reloadMode     string
	reloadStrategy string
	changedSources []string
	reloadFallback bool
}

func classifyConfigUpdateImpact(coreChanged bool, plan configRuntimeReloadPlan, mcpChanged, servChanged bool, servReload string) configUpdateImpact {
	servChanged = servChanged || mcpChanged
	if !coreChanged && !servChanged {
		return configUpdateImpact{}
	}

	impact := configUpdateImpact{reloadMode: servReloadHot}
	switch {
	case coreChanged && servChanged:
		impact.scope = ConfigScopeMixed
	case coreChanged:
		impact.scope = ConfigScopeCore
	default:
		impact.scope = ConfigScopeServ
	}
	if servReload == servReloadRestart {
		impact.reloadMode = servReloadRestart
	}
	if coreChanged {
		impact.reloadStrategy = plan.mode
		impact.changedSources = plan.changedSources
		impact.reloadFallback = plan.fallback
	}
	return impact
}

func (impact configUpdateImpact) apply(result *ConfigUpdateResult) {
	if result == nil || impact.scope == "" {
		return
	}
	result.Scope = impact.scope
	result.ReloadMode = impact.reloadMode
	result.ReloadStrategy = impact.reloadStrategy
	result.ChangedSources = impact.changedSources
	result.ReloadFallback = impact.reloadFallback
}

func (ms *mcpServer) ensureConfigPreviewStore() *configPreviewStore {
	if ms == nil || ms.service == nil {
		return nil
	}
	if ms.service.configPreviews == nil {
		ms.service.configPreviews = newConfigPreviewStore()
	}
	return ms.service.configPreviews
}

func configStringArg(args map[string]interface{}, key string) string {
	if args == nil {
		return ""
	}
	if s, ok := args[key].(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func configUpdatePatchHash(args map[string]interface{}) string {
	clean := make(map[string]any, len(args))
	for key, value := range args {
		switch key {
		case "mode", "preview_id", "valid", "applied", "expires_at", "change_summary_json", "findings_json", "errors_json":
			continue
		default:
			clean[key] = value
		}
	}
	data, _ := json.Marshal(clean)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func jsonStringValue(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(data)
}

func (ms *mcpServer) currentConfigCatalogRevision(ctx context.Context) string {
	if ms == nil {
		return ""
	}
	if ms.service != nil {
		if snap, err := ms.service.catalogSnapshot(); err == nil && snap != nil {
			return snap.Revision
		}
	}
	if rev := ms.catalogRevisionGraphQL(ctx); strings.TrimSpace(rev) != "" {
		return rev
	}
	return ""
}

func (ms *mcpServer) sourceModeConfigGate(ctx context.Context, args map[string]interface{}) (mode string, expectedRev string, patchHash string, previewID string, fail *ConfigUpdateResult) {
	mode = strings.ToLower(configStringArg(args, "mode"))
	expectedRev = configStringArg(args, "expected_catalog_revision")
	previewID = configStringArg(args, "preview_id")
	patchHash = configUpdatePatchHash(args)
	currentRev := ms.currentConfigCatalogRevision(ctx)
	failure := func(message string, errs ...string) *ConfigUpdateResult {
		if len(errs) == 0 {
			errs = []string{message}
		}
		return &ConfigUpdateResult{
			Success:         false,
			Message:         message,
			Errors:          errs,
			Valid:           false,
			Applied:         false,
			Mode:            mode,
			CatalogRevision: currentRev,
			ErrorsJSON:      jsonStringValue(errs),
		}
	}
	switch mode {
	case "preview", "apply":
	case "validate":
		// Stateless dry-run: no preview record is created and the revision
		// guard is optional; a provided expected_catalog_revision is still
		// checked for staleness.
		if expectedRev != "" && currentRev != "" && expectedRev != currentRev {
			return mode, expectedRev, patchHash, previewID, failure("expected_catalog_revision is stale; read gj_config again before retrying", fmt.Sprintf("expected_catalog_revision %q does not match current catalog_revision %q", expectedRev, currentRev))
		}
		return mode, expectedRev, patchHash, previewID, nil
	default:
		return mode, expectedRev, patchHash, previewID, failure("source-mode gj_config updates require mode: \"preview\" first, then mode: \"apply\" with preview_id", "missing or unsupported mode for source-mode config update; next_action: query_catalog(search: \"safe gj_config preview apply source_patches\")")
	}
	if currentRev != "" && expectedRev == "" {
		return mode, expectedRev, patchHash, previewID, failure("source-mode gj_config updates require expected_catalog_revision", "read gj_config(id: \"current\") { catalog_revision } before preview/apply")
	}
	if currentRev != "" && expectedRev != "" && expectedRev != currentRev {
		return mode, expectedRev, patchHash, previewID, failure("expected_catalog_revision is stale; read gj_config again before retrying", fmt.Sprintf("expected_catalog_revision %q does not match current catalog_revision %q", expectedRev, currentRev))
	}
	if mode == "apply" {
		if previewID == "" {
			return mode, expectedRev, patchHash, previewID, failure("source-mode config apply requires preview_id", "run mode: \"preview\" first, then resend the same payload with mode: \"apply\" and preview_id")
		}
		rec, ok := ms.ensureConfigPreviewStore().get(previewID)
		if !ok {
			return mode, expectedRev, patchHash, previewID, failure("config preview is unknown or expired; run preview again", "unknown or expired preview_id")
		}
		if rec.BaseCatalogRevision != expectedRev {
			return mode, expectedRev, patchHash, previewID, failure("config preview was created for a different catalog revision; run preview again", "preview catalog revision mismatch")
		}
		if rec.PatchHash != patchHash {
			return mode, expectedRev, patchHash, previewID, failure("config apply payload does not match preview payload; resend the exact same patch", "preview payload hash mismatch")
		}
	}
	return mode, expectedRev, patchHash, previewID, nil
}

func (ms *mcpServer) finishConfigUpdate(ctx context.Context, result ConfigUpdateResult) (*mcp.CallToolResult, error) {
	result.Next = ms.nextForConfigUpdate(result)
	ms.recordConfigUpdateRuntimeEvent(ctx, result)
	data, err := mcpMarshalJSON(result, true)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal result: %v", err)), nil
	}
	return mcpToolResultJSONBytes(data), nil
}

// handleUpdateCurrentConfig updates the configuration and reloads
func (ms *mcpServer) handleUpdateCurrentConfig(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx = ms.effectiveContext(ctx)
	if ms.service != nil {
		ms.service.configMu.Lock()
		defer ms.service.configMu.Unlock()
	}

	args := req.GetArguments()
	sourceMode := ms.service != nil && ms.service.conf != nil && ms.service.conf.Core.IsSourcesUsed()
	mode, expectedRev, patchHash, previewID, gateFailure := ms.sourceModeConfigGate(ctx, args)
	if sourceMode && gateFailure != nil {
		return ms.finishConfigUpdate(ctx, *gateFailure)
	}

	if paths := plaintextSecretUpdatePaths(args); len(paths) > 0 {
		var err error
		switch {
		case strings.TrimSpace(ms.service.conf.Secrets.Keystore.Key) == "":
			err = missingLocalKeystoreKeyError(paths)
		default:
			_, err = ms.service.localKeystore()
		}
		if err != nil {
			result := ConfigUpdateResult{
				Success: false,
				Message: "Secret config update rejected, changes not applied",
				Errors:  []string{redactRuntimeError(err)},
				Mode:    mode,
			}
			if sourceMode {
				result.Valid = false
				result.Applied = false
				result.CatalogRevision = expectedRev
				result.ChangeSummaryJSON = "[]"
				result.ErrorsJSON = jsonStringValue(result.Errors)
			}
			return ms.finishConfigUpdate(ctx, result)
		}
	}

	var changes []string
	var errors []string

	stagedCore := cloneCoreConfig(ms.service.conf.Core)
	conf := &stagedCore

	// Parse create_if_not_exists early
	createIfNotExists := false
	if ci, ok := args["create_if_not_exists"].(bool); ok {
		createIfNotExists = ci
	}
	if createIfNotExists && ms.service.conf.Serv.Production {
		errors = append(errors, "create_if_not_exists is only available in dev mode")
		createIfNotExists = false
	}

	var mcpPatch map[string]interface{}
	if rawMCP, ok := args["mcp"].(map[string]interface{}); ok && len(rawMCP) > 0 {
		if _, err := validateMCPConfigPatch(rawMCP); err != nil {
			errors = append(errors, err.Error())
		} else {
			mcpPatch = rawMCP
			changes = append(changes, "updated mcp config")
		}
	}

	// Server-side settings (serv.Config) flow through the same machinery as the
	// core sections: validated here, applied and persisted after the core stage
	// commits. Restart-class changes are persisted and take effect on the next
	// start (auto when reload_on_config_change is enabled).
	var servPatch map[string]any
	var servReload string
	if rawServ, ok := args["serv"].(map[string]interface{}); ok && len(rawServ) > 0 {
		servChanges, reload, err := validateServConfigPatch(rawServ)
		if err != nil {
			errors = append(errors, err.Error())
		} else {
			servPatch = rawServ
			servReload = reload
			changes = append(changes, servChanges...)
		}
	}

	if sources, ok := args["sources"].([]any); ok {
		parsed, err := parseSourceConfigList(sources)
		if err != nil {
			errors = append(errors, fmt.Sprintf("sources: %v", err))
		} else {
			preserved := preserveProtectedReadOnlySources(parsed, ms.readOnlySources)
			conf.Sources = parsed
			if err := conf.RenormalizeSources(); err != nil {
				errors = append(errors, fmt.Sprintf("sources: %v", err))
			} else {
				changes = append(changes, "updated sources")
				changes = append(changes, preserved...)
			}
		}
	}

	if patches, ok := args["update_sources"].([]any); ok {
		updated, patchChanges, err := applySourceConfigMergePatches(conf.Sources, patches)
		if err != nil {
			errors = append(errors, fmt.Sprintf("update_sources: %v", err))
		} else {
			preserved := preserveProtectedReadOnlySources(updated, ms.readOnlySources)
			conf.Sources = updated
			if err := conf.RenormalizeSources(); err != nil {
				errors = append(errors, fmt.Sprintf("update_sources: %v", err))
			} else {
				changes = append(changes, patchChanges...)
				changes = append(changes, preserved...)
			}
		}
	}

	if removeSources, ok := args["remove_sources"].([]any); ok {
		updated, removeChanges, err := removeSourceConfigs(conf.Sources, removeSources)
		if err != nil {
			errors = append(errors, fmt.Sprintf("remove_sources: %v", err))
		} else if len(removeChanges) != 0 {
			conf.Sources = updated
			if err := conf.RenormalizeSources(); err != nil {
				errors = append(errors, fmt.Sprintf("remove_sources: %v", err))
			} else {
				changes = append(changes, removeChanges...)
			}
		}
	}

	if sourcePatches, ok := args["source_patches"].([]any); ok {
		patchChanges, err := applySourceAccessConfigPatches(conf, sourcePatches)
		if err != nil {
			errors = append(errors, fmt.Sprintf("source_patches: %v", err))
		} else {
			changes = append(changes, patchChanges...)
		}
	}

	if patch, ok := args["system"].(map[string]interface{}); ok {
		var next core.SystemConfig
		if err := mergeConfigSection(conf.System, patch, &next); err != nil {
			errors = append(errors, fmt.Sprintf("system: %v", err))
		} else {
			conf.System = next
			changes = append(changes, "updated system feature configuration")
		}
	}
	if patch, ok := args["workflows"].(map[string]interface{}); ok {
		var next core.WorkflowsConfig
		if err := mergeConfigSection(conf.Workflows, patch, &next); err != nil {
			errors = append(errors, fmt.Sprintf("workflows: %v", err))
		} else {
			conf.Workflows = next
			changes = append(changes, "updated workflow feature configuration")
		}
	}
	if _, legacy := args["metadata"]; legacy {
		errors = append(errors, "metadata: removed; use system.capabilities.catalog.read")
	}

	if relationships, ok := args["relationships"].([]any); ok {
		parsed, err := parseRelationshipConfigList(relationships)
		if err != nil {
			errors = append(errors, fmt.Sprintf("relationships: %v", err))
		} else {
			conf.Relationships = parsed
			changes = append(changes, "updated relationships")
		}
	}

	// Process databases: parse, validate, test connections, then commit
	type parsedDB struct {
		name   string
		config core.DatabaseConfig
	}
	var parsedDBs []parsedDB

	if databases, ok := args["databases"].(map[string]any); ok && len(databases) > 0 {
		dbSortedNames := make([]string, 0, len(databases))
		for name := range databases {
			dbSortedNames = append(dbSortedNames, name)
		}
		sort.Strings(dbSortedNames)
		for _, name := range dbSortedNames {
			dbAny := databases[name]
			dbMap, ok := dbAny.(map[string]any)
			if !ok {
				errors = append(errors, fmt.Sprintf("invalid database config for '%s'", name))
				continue
			}
			dbConf, err := parseDBConfig(dbMap)
			if err != nil {
				errors = append(errors, fmt.Sprintf("database '%s': %v", name, err))
				continue
			}
			// Infer dbname from map key if not explicitly set
			if dbConf.DBName == "" && dbConf.ConnString == "" {
				dbConf.DBName = name
			}
			// Guard: reject system/default database names unless allowed
			dbType := strings.ToLower(dbConf.Type)
			effectiveDBName := dbConf.DBName
			if effectiveDBName == "" {
				effectiveDBName = name
			}
			if !ms.service.conf.MCP.DefaultDBAllowed && isSystemDatabase(dbType, effectiveDBName) {
				errors = append(errors, systemDatabaseError(dbType, effectiveDBName))
				continue
			}
			parsedDBs = append(parsedDBs, parsedDB{name: name, config: dbConf})
		}

		// Pre-commit: test each new/updated database connection
		var connErrors []string
		for _, pdb := range parsedDBs {
			dbConf := pdb.config
			dbType := strings.ToLower(dbConf.Type)
			host := dbConf.Host
			port := dbConf.Port
			user := dbConf.User
			password := dbConf.Password
			dbName := dbConf.DBName

			// Skip connection test for file-managed databases.
			if dbType == "sqlite" || isCodeSQLType(dbType) {
				continue
			}

			// If create_if_not_exists, try to create the database first
			if createIfNotExists {
				if err := createDatabaseOnServer(dbType, host, port, user, password, dbName, ms.service.log); err != nil {
					ms.service.log.Warnf("create_if_not_exists for '%s': %s", pdb.name, redactRuntimeError(err))
					if dbType == "snowflake" {
						changes = append(changes, fmt.Sprintf("database %s: create_if_not_exists is not supported for snowflake; continuing with connection test", pdb.name))
					}
				}
			}

			// Test connectivity
			_, err := testDatabaseConnection(dbType, host, port, user, password, dbName, dbConf.ConnString)
			if err != nil {
				connErrors = append(connErrors, fmt.Sprintf("database '%s' (%s@%s:%d/%s): connection failed: %v",
					pdb.name, user, host, port, dbName, redactRuntimeError(err)))
			}
		}

		// If ANY connection test failed, skip ALL database config changes
		if len(connErrors) > 0 {
			errors = append(errors, connErrors...)
			result := ConfigUpdateResult{
				Success: false,
				Message: "Database connection test failed — config changes not applied",
				Errors:  errors,
				Mode:    mode,
			}
			if sourceMode {
				result.Valid = false
				result.Applied = false
				result.CatalogRevision = expectedRev
				result.ErrorsJSON = jsonStringValue(errors)
			}
			return ms.finishConfigUpdate(ctx, result)
		}

		// All connections passed — commit database configs
		if conf.Databases == nil {
			conf.Databases = make(map[string]core.DatabaseConfig)
		}
		for _, pdb := range parsedDBs {
			// Tamper protection: if the startup snapshot says this DB is read-only,
			// preserve that setting regardless of what the LLM sends.
			if ms.readOnlyDBs[pdb.name] && !pdb.config.ReadOnly {
				pdb.config.ReadOnly = true
				ms.service.log.Warnf("database %q: read_only is protected and cannot be changed to false at runtime", pdb.name)
				changes = append(changes, fmt.Sprintf("database %s: read_only preserved as true (tamper-protected)", pdb.name))
			}
			conf.Databases[pdb.name] = pdb.config
			changes = append(changes, fmt.Sprintf("added/updated database: %s", pdb.name))
		}
	}

	// Process tables
	if tables, ok := args["tables"].([]any); ok && len(tables) > 0 {
		for _, tableAny := range tables {
			tableMap, ok := tableAny.(map[string]any)
			if !ok {
				errors = append(errors, "invalid table config")
				continue
			}
			table, err := parseTableConfig(tableMap)
			if err != nil {
				errors = append(errors, fmt.Sprintf("table: %v", err))
				continue
			}
			// Update existing or add new
			found := false
			for i, t := range conf.Tables {
				if strings.EqualFold(t.Name, table.Name) {
					conf.Tables[i] = table
					found = true
					changes = append(changes, fmt.Sprintf("updated table: %s", table.Name))
					break
				}
			}
			if !found {
				conf.Tables = append(conf.Tables, table)
				changes = append(changes, fmt.Sprintf("added table: %s", table.Name))
			}
		}
	}

	// Process roles
	if roles, ok := args["roles"].([]any); ok && len(roles) > 0 {
		for _, roleAny := range roles {
			roleMap, ok := roleAny.(map[string]any)
			if !ok {
				errors = append(errors, "invalid role config")
				continue
			}
			role, err := parseRoleConfig(roleMap)
			if err != nil {
				errors = append(errors, fmt.Sprintf("role: %v", err))
				continue
			}
			// Update existing or add new
			found := false
			for i, r := range conf.Roles {
				if strings.EqualFold(r.Name, role.Name) {
					conf.Roles[i] = role
					found = true
					changes = append(changes, fmt.Sprintf("updated role: %s", role.Name))
					break
				}
			}
			if !found {
				conf.Roles = append(conf.Roles, role)
				changes = append(changes, fmt.Sprintf("added role: %s", role.Name))
			}
		}
	}

	// Process blocklist
	if blocklist, ok := args["blocklist"].([]any); ok && len(blocklist) > 0 {
		for _, item := range blocklist {
			if s, ok := item.(string); ok && s != "" {
				// Check if already in blocklist
				found := false
				for _, existing := range conf.Blocklist {
					if strings.EqualFold(existing, s) {
						found = true
						break
					}
				}
				if !found {
					conf.Blocklist = append(conf.Blocklist, s)
					changes = append(changes, fmt.Sprintf("added to blocklist: %s", s))
				}
			}
		}
	}

	// Process functions
	if functions, ok := args["functions"].([]any); ok && len(functions) > 0 {
		for _, fnAny := range functions {
			fnMap, ok := fnAny.(map[string]any)
			if !ok {
				errors = append(errors, "invalid function config")
				continue
			}
			fn, err := parseFunctionConfig(fnMap)
			if err != nil {
				errors = append(errors, fmt.Sprintf("function: %v", err))
				continue
			}
			// Update existing or add new
			found := false
			for i, f := range conf.Functions {
				if strings.EqualFold(f.Name, fn.Name) {
					conf.Functions[i] = fn
					found = true
					changes = append(changes, fmt.Sprintf("updated function: %s", fn.Name))
					break
				}
			}
			if !found {
				conf.Functions = append(conf.Functions, fn)
				changes = append(changes, fmt.Sprintf("added function: %s", fn.Name))
			}
		}
	}

	// Process resolvers
	if resolvers, ok := args["resolvers"].([]any); ok && len(resolvers) > 0 {
		for _, rAny := range resolvers {
			rMap, ok := rAny.(map[string]any)
			if !ok {
				errors = append(errors, "invalid resolver config")
				continue
			}
			rc, err := parseResolverConfig(rMap)
			if err != nil {
				errors = append(errors, fmt.Sprintf("resolver: %v", err))
				continue
			}
			// Update existing or add new
			found := false
			for i, r := range conf.Resolvers {
				if strings.EqualFold(r.Name, rc.Name) {
					conf.Resolvers[i] = rc
					found = true
					changes = append(changes, fmt.Sprintf("updated resolver: %s", rc.Name))
					break
				}
			}
			if !found {
				conf.Resolvers = append(conf.Resolvers, rc)
				changes = append(changes, fmt.Sprintf("added resolver: %s", rc.Name))
			}
		}
	}

	// Process remove_databases
	if removeDBs, ok := args["remove_databases"].([]any); ok {
		for _, item := range removeDBs {
			if name, ok := item.(string); ok && name != "" {
				if _, exists := conf.Databases[name]; exists {
					delete(conf.Databases, name)
					changes = append(changes, fmt.Sprintf("removed database: %s", name))
				}
			}
		}
	}

	// Process remove_tables
	if removeTables, ok := args["remove_tables"].([]any); ok {
		for _, item := range removeTables {
			if name, ok := item.(string); ok && name != "" {
				for i, t := range conf.Tables {
					if strings.EqualFold(t.Name, name) {
						conf.Tables = append(conf.Tables[:i], conf.Tables[i+1:]...)
						changes = append(changes, fmt.Sprintf("removed table: %s", name))
						break
					}
				}
			}
		}
	}

	// Process remove_roles
	if removeRoles, ok := args["remove_roles"].([]any); ok {
		for _, item := range removeRoles {
			if name, ok := item.(string); ok && name != "" {
				for i, r := range conf.Roles {
					if strings.EqualFold(r.Name, name) {
						conf.Roles = append(conf.Roles[:i], conf.Roles[i+1:]...)
						changes = append(changes, fmt.Sprintf("removed role: %s", name))
						break
					}
				}
			}
		}
	}

	// Process remove_blocklist_items
	if removeBlocklist, ok := args["remove_blocklist_items"].([]any); ok {
		for _, item := range removeBlocklist {
			if s, ok := item.(string); ok && s != "" {
				for i, existing := range conf.Blocklist {
					if strings.EqualFold(existing, s) {
						conf.Blocklist = append(conf.Blocklist[:i], conf.Blocklist[i+1:]...)
						changes = append(changes, fmt.Sprintf("removed from blocklist: %s", s))
						break
					}
				}
			}
		}
	}

	// Process remove_functions
	if removeFunctions, ok := args["remove_functions"].([]any); ok {
		for _, item := range removeFunctions {
			if name, ok := item.(string); ok && name != "" {
				for i, f := range conf.Functions {
					if strings.EqualFold(f.Name, name) {
						conf.Functions = append(conf.Functions[:i], conf.Functions[i+1:]...)
						changes = append(changes, fmt.Sprintf("removed function: %s", name))
						break
					}
				}
			}
		}
	}

	// Process remove_resolvers
	if removeResolvers, ok := args["remove_resolvers"].([]any); ok {
		for _, item := range removeResolvers {
			if name, ok := item.(string); ok && name != "" {
				for i, r := range conf.Resolvers {
					if strings.EqualFold(r.Name, name) {
						conf.Resolvers = append(conf.Resolvers[:i], conf.Resolvers[i+1:]...)
						changes = append(changes, fmt.Sprintf("removed resolver: %s", name))
						break
					}
				}
			}
		}
	}

	// If no changes were made, return early
	if len(changes) == 0 && len(errors) == 0 {
		result := ConfigUpdateResult{
			Success: true,
			Message: "No changes provided",
			Mode:    mode,
		}
		if mode == "validate" {
			result.Valid = true
		}
		if sourceMode {
			result.Valid = true
			result.Applied = false
			result.CatalogRevision = expectedRev
			result.ChangeSummaryJSON = "[]"
			result.ErrorsJSON = "[]"
		}
		return ms.finishConfigUpdate(ctx, result)
	}
	if len(errors) > 0 {
		result := ConfigUpdateResult{
			Success: false,
			Message: "Config validation failed, changes not applied",
			Changes: changes,
			Errors:  errors,
			Mode:    mode,
		}
		if sourceMode {
			result.Valid = false
			result.Applied = false
			result.CatalogRevision = expectedRev
			result.ChangeSummaryJSON = jsonStringValue(changes)
			result.ErrorsJSON = jsonStringValue(errors)
		}
		return ms.finishConfigUpdate(ctx, result)
	}

	var availableDBs []string
	oldCore := cloneCoreConfig(ms.service.conf.Core)
	var sealedKeystore *localKeystore
	var sealedSecretRefs map[string]struct{}
	var reloadPlan configRuntimeReloadPlan
	var impact configUpdateImpact
	coreChanged := !reflect.DeepEqual(stagedCore, ms.service.conf.Core)
	if coreChanged {
		reloadPlan = classifyConfigRuntimeReload(oldCore, stagedCore)
		if reloadPlan.mode == "source_scoped" && ms.service.systemNanoDB == nil && ms.service.metadataGraphEnabledForCore(&stagedCore) {
			reloadPlan.mode = "full"
			reloadPlan.fallback = true
		}
		impact = classifyConfigUpdateImpact(true, reloadPlan, mcpPatch != nil, servPatch != nil, servReload)
		var stage *stagedRuntimeState
		var err error
		if reloadPlan.mode == "source_scoped" {
			stage, err = ms.prepareSourceScopedRuntime(conf, reloadPlan.changedSources, createIfNotExists)
		} else {
			stage, err = ms.prepareStagedRuntime(conf, createIfNotExists)
		}
		if err != nil {
			if stage != nil {
				availableDBs = stage.availableDBs
				stage.close()
			}

			errText := redactRuntimeError(err)
			message := fmt.Sprintf("Config reload failed, changes not persisted: %s", errText)
			errs := append(errors, fmt.Sprintf("reload error: %s", errText))
			if stage != nil && stage.schemaNotReady {
				message = "Config validation failed, changes not applied: database connected but schema discovery found no tables. Try a different database from the databases list, or create tables first."
				errs = append(errs, "schema not ready after staged reload")
			}

			result := ConfigUpdateResult{
				Success:   false,
				Message:   message,
				Changes:   changes,
				Errors:    errs,
				Databases: availableDBs,
				Mode:      mode,
			}
			impact.apply(&result)
			if sourceMode {
				result.Valid = false
				result.Applied = false
				result.CatalogRevision = expectedRev
				result.ChangeSummaryJSON = jsonStringValue(changes)
				result.ErrorsJSON = jsonStringValue(errs)
			}
			return ms.finishConfigUpdate(ctx, result)
		}

		availableDBs = stage.availableDBs
		if mode == "validate" {
			var findingsJSON string
			if sourceMode {
				findingsJSON = ms.stagedConfigSecurityFindingsJSON(conf)
			}
			stage.close()
			result := ConfigUpdateResult{
				Success:           true,
				Message:           "Config is valid; validate mode never applies changes",
				Changes:           changes,
				Databases:         availableDBs,
				Valid:             true,
				Applied:           false,
				Mode:              mode,
				CatalogRevision:   ms.currentConfigCatalogRevision(ctx),
				ChangeSummaryJSON: jsonStringValue(changes),
				FindingsJSON:      findingsJSON,
				ErrorsJSON:        "[]",
			}
			impact.apply(&result)
			return ms.finishConfigUpdate(ctx, result)
		}
		if sourceMode && mode == "preview" {
			findingsJSON := ms.stagedConfigSecurityFindingsJSON(conf)
			stage.close()
			rec := ms.ensureConfigPreviewStore().put(configPreviewRecord{
				PatchHash:           patchHash,
				BaseCatalogRevision: expectedRev,
				ChangeSummaryJSON:   jsonStringValue(changes),
				FindingsJSON:        findingsJSON,
				ErrorsJSON:          "[]",
			})
			result := ConfigUpdateResult{
				Success:           true,
				Message:           "Config preview is valid; resend the same payload with mode: \"apply\" and preview_id before expiry.",
				Changes:           changes,
				Databases:         availableDBs,
				Valid:             true,
				Applied:           false,
				Mode:              mode,
				PreviewID:         rec.ID,
				ExpiresAt:         rec.ExpiresAt.Format(time.RFC3339Nano),
				CatalogRevision:   expectedRev,
				ChangeSummaryJSON: rec.ChangeSummaryJSON,
				FindingsJSON:      rec.FindingsJSON,
				ErrorsJSON:        rec.ErrorsJSON,
			}
			impact.apply(&result)
			return ms.finishConfigUpdate(ctx, result)
		}

		persistedCore := cloneCoreConfig(stagedCore)
		if strings.TrimSpace(ms.service.conf.Secrets.Keystore.Key) != "" || configContainsSecretRefs(&persistedCore) {
			ks, err := ms.service.localKeystore()
			if err != nil {
				stage.close()
				result := ConfigUpdateResult{
					Success:   false,
					Message:   "Config secret sealing failed, changes not applied",
					Changes:   changes,
					Errors:    []string{redactRuntimeError(err)},
					Databases: availableDBs,
					Mode:      mode,
				}
				impact.apply(&result)
				if sourceMode {
					result.Valid = false
					result.Applied = false
					result.CatalogRevision = expectedRev
					result.ChangeSummaryJSON = jsonStringValue(changes)
					result.ErrorsJSON = jsonStringValue(result.Errors)
				}
				return ms.finishConfigUpdate(ctx, result)
			}
			if !ks.hasKey() && configContainsSecretRefs(&persistedCore) {
				stage.close()
				result := ConfigUpdateResult{
					Success:   false,
					Message:   "Config secret hydration failed, changes not applied",
					Changes:   changes,
					Errors:    []string{missingLocalKeystoreKeyError(secretRefsInConfig(&persistedCore)).Error()},
					Databases: availableDBs,
					Mode:      mode,
				}
				impact.apply(&result)
				if sourceMode {
					result.Valid = false
					result.Applied = false
					result.CatalogRevision = expectedRev
					result.ChangeSummaryJSON = jsonStringValue(changes)
					result.ErrorsJSON = jsonStringValue(result.Errors)
				}
				return ms.finishConfigUpdate(ctx, result)
			}
			if ks.hasKey() {
				usedRefs, err := sealCoreConfigSecrets(&persistedCore, ks)
				if err != nil {
					stage.close()
					result := ConfigUpdateResult{
						Success:   false,
						Message:   "Config secret sealing failed, changes not applied",
						Changes:   changes,
						Errors:    []string{redactRuntimeError(err)},
						Databases: availableDBs,
						Mode:      mode,
					}
					impact.apply(&result)
					if sourceMode {
						result.Valid = false
						result.Applied = false
						result.CatalogRevision = expectedRev
						result.ChangeSummaryJSON = jsonStringValue(changes)
						result.ErrorsJSON = jsonStringValue(result.Errors)
					}
					return ms.finishConfigUpdate(ctx, result)
				}
				if err := ks.Save(nil); err != nil {
					stage.close()
					result := ConfigUpdateResult{
						Success:   false,
						Message:   "Config secret keystore save failed, changes not applied",
						Changes:   changes,
						Errors:    []string{redactRuntimeError(err)},
						Databases: availableDBs,
						Mode:      mode,
					}
					impact.apply(&result)
					if sourceMode {
						result.Valid = false
						result.Applied = false
						result.CatalogRevision = expectedRev
						result.ChangeSummaryJSON = jsonStringValue(changes)
						result.ErrorsJSON = jsonStringValue(result.Errors)
					}
					return ms.finishConfigUpdate(ctx, result)
				}
				sealedKeystore = ks
				sealedSecretRefs = usedRefs
			}
		}
		if reloadPlan.mode == "source_scoped" {
			if err := ms.commitSourceScopedRuntime(persistedCore, stage, reloadPlan); err != nil {
				stage.close()
				errText := redactRuntimeError(err)
				result := ConfigUpdateResult{
					Success:   false,
					Message:   fmt.Sprintf("Config reload failed, changes not persisted: %s", errText),
					Changes:   changes,
					Errors:    append(errors, fmt.Sprintf("reload error: %s", errText)),
					Databases: availableDBs,
					Mode:      mode,
				}
				impact.apply(&result)
				if sourceMode {
					result.Valid = false
					result.Applied = false
					result.CatalogRevision = expectedRev
					result.ChangeSummaryJSON = jsonStringValue(changes)
					result.ErrorsJSON = jsonStringValue(result.Errors)
				}
				return ms.finishConfigUpdate(ctx, result)
			}
		} else {
			ms.commitStagedRuntime(persistedCore, stage)
		}
		if err := ms.service.reconfigureDiscoveryAfterConfigChange(ctx); err != nil {
			errors = append(errors, fmt.Sprintf("coordinated discovery refresh error: %s", redactRuntimeError(err)))
		}
		if err := ms.service.refreshCatalogAfterCoreConfigChange(oldCore, persistedCore, "config mutation"); err != nil {
			errors = append(errors, fmt.Sprintf("catalog refresh error: %s", redactRuntimeError(err)))
		}
		if ms.service.gj != nil && ms.service.gj.SchemaReady() {
			if reloadPlan.mode == "source_scoped" {
				changes = append(changes, "configuration validated and runtime reloaded source-scoped")
			} else {
				changes = append(changes, "configuration validated and runtime reloaded transactionally")
			}
		}
	}
	if !coreChanged {
		impact = classifyConfigUpdateImpact(false, reloadPlan, mcpPatch != nil, servPatch != nil, servReload)
	}

	if sourceMode && mode == "preview" {
		findingsJSON := ms.stagedConfigSecurityFindingsJSON(conf)
		rec := ms.ensureConfigPreviewStore().put(configPreviewRecord{
			PatchHash:           patchHash,
			BaseCatalogRevision: expectedRev,
			ChangeSummaryJSON:   jsonStringValue(changes),
			FindingsJSON:        findingsJSON,
			ErrorsJSON:          "[]",
		})
		result := ConfigUpdateResult{
			Success:           true,
			Message:           "Config preview is valid; resend the same payload with mode: \"apply\" and preview_id before expiry.",
			Changes:           changes,
			Databases:         availableDBs,
			Valid:             true,
			Applied:           false,
			Mode:              mode,
			PreviewID:         rec.ID,
			ExpiresAt:         rec.ExpiresAt.Format(time.RFC3339Nano),
			CatalogRevision:   expectedRev,
			ChangeSummaryJSON: rec.ChangeSummaryJSON,
			FindingsJSON:      rec.FindingsJSON,
			ErrorsJSON:        rec.ErrorsJSON,
		}
		impact.apply(&result)
		return ms.finishConfigUpdate(ctx, result)
	}

	// Validate mode with no core changes (e.g. an mcp-only patch or a no-op
	// payload): report validity without touching the running config.
	if mode == "validate" {
		var findingsJSON string
		if sourceMode {
			findingsJSON = ms.stagedConfigSecurityFindingsJSON(conf)
		}
		result := ConfigUpdateResult{
			Success:           true,
			Message:           "Config is valid; validate mode never applies changes",
			Changes:           changes,
			Databases:         availableDBs,
			Valid:             true,
			Applied:           false,
			Mode:              mode,
			CatalogRevision:   ms.currentConfigCatalogRevision(ctx),
			ChangeSummaryJSON: jsonStringValue(changes),
			FindingsJSON:      findingsJSON,
			ErrorsJSON:        "[]",
		}
		impact.apply(&result)
		return ms.finishConfigUpdate(ctx, result)
	}

	if mcpPatch != nil && len(errors) == 0 {
		mcpChanges, err := applyMCPConfigPatch(ms.service.conf, mcpPatch)
		if err != nil {
			errors = append(errors, err.Error())
		} else {
			changes = append(changes, mcpChanges...)
			ms.service.markCatalogChanged("config mutation")
		}
	}

	if servPatch != nil && len(errors) == 0 {
		applyServConfigPatch(ms.service.conf, servPatch)
		// Stage only the patched serv keys into viper so saveConfigToDisk
		// persists them without rewriting unrelated defaults.
		setServPatchViper(ms.service.conf.viper, ms.service.conf, servPatch)
		ms.service.markCatalogChanged("config mutation")
		if servReload == servReloadRestart {
			changes = append(changes, "restart required for serv changes to take effect (auto when reload_on_config_change is enabled)")
		}
	}

	// Save to disk only after successful reload (dev mode only)
	if len(changes) > 0 && !ms.service.conf.Serv.Production {
		if err := ms.saveConfigToDisk(); err != nil {
			msg := redactRuntimeError(err)
			ms.service.log.Warnf("Failed to save config to disk: %s", msg)
			changes = append(changes, fmt.Sprintf("config save warning: %s (changes applied in-memory only)", msg))
		} else {
			ms.service.log.Info("Configuration saved to disk")
			changes = append(changes, "configuration saved to disk")
			if sealedKeystore != nil && sealedSecretRefs != nil {
				if err := sealedKeystore.Save(sealedSecretRefs); err != nil {
					msg := redactRuntimeError(err)
					ms.service.log.Warnf("Failed to prune secrets keystore: %s", msg)
					changes = append(changes, fmt.Sprintf("secret keystore prune warning: %s (stale encrypted entries retained)", msg))
				}
			}
		}
	}

	result := ConfigUpdateResult{
		Success:   len(errors) == 0,
		Message:   "Configuration updated and reloaded successfully",
		Changes:   changes,
		Errors:    errors,
		Databases: availableDBs,
		Mode:      mode,
	}
	if sourceMode {
		result.Applied = len(errors) == 0 && mode == "apply"
		result.Valid = len(errors) == 0
		result.PreviewID = previewID
		result.CatalogRevision = ms.currentConfigCatalogRevision(ctx)
		result.ChangeSummaryJSON = jsonStringValue(changes)
		result.ErrorsJSON = jsonStringValue(errors)
		if result.Applied {
			ms.ensureConfigPreviewStore().delete(previewID)
		}
	}
	impact.apply(&result)
	if snap, err := ms.service.catalogSnapshot(); err == nil && snap != nil {
		result.CatalogRevision = snap.Revision
	}

	if len(errors) > 0 {
		result.Message = "Configuration partially updated with some errors"
	}
	return ms.finishConfigUpdate(ctx, result)
}

func parseSourceConfigList(items []any) ([]core.SourceConfig, error) {
	data, err := json.Marshal(items)
	if err != nil {
		return nil, err
	}
	var out []core.SourceConfig
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func preserveProtectedReadOnlySources(sources []core.SourceConfig, protected map[string]bool) []string {
	var changes []string
	for i := range sources {
		if protected[canonicalSourcePolicyName(sources[i].Name)] && !sources[i].ReadOnly {
			sources[i].ReadOnly = true
			changes = append(changes, fmt.Sprintf("source %s: read_only preserved as true (tamper-protected)", sources[i].Name))
		}
	}
	return changes
}

func canonicalSourcePolicyName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func mergeConfigSection(current any, patch map[string]interface{}, out any) error {
	data, err := json.Marshal(current)
	if err != nil {
		return err
	}
	base := make(map[string]any)
	if err := json.Unmarshal(data, &base); err != nil {
		return err
	}
	mergeJSONPatch(base, patch)
	data, err = json.Marshal(base)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func sourceConfigInputSchema(required []string) map[string]any {
	props := map[string]any{
		"name":              map[string]any{"type": "string", "description": "Source name"},
		"kind":              map[string]any{"type": "string", "enum": []string{"database", "code", "file", "api"}, "description": "External source kind"},
		"default":           map[string]any{"type": "boolean", "description": "Default source"},
		"type":              map[string]any{"type": "string", "description": "Database type"},
		"connection_string": map[string]any{"type": "string", "description": "Database connection string"},
		"host":              map[string]any{"type": "string", "description": "Database host"},
		"port":              map[string]any{"type": "number", "description": "Database port"},
		"dbname":            map[string]any{"type": "string", "description": "Database name"},
		"user":              map[string]any{"type": "string", "description": "Database user"},
		"password":          map[string]any{"type": "string", "description": "Database password"},
		"path":              map[string]any{"type": "string", "description": "Database/file path"},
		"schema":            map[string]any{"type": "string", "description": "Database schema"},
		"read_only":         map[string]any{"type": "boolean", "description": "Read-only source"},
		"analytics_mode":    map[string]any{"type": "boolean", "description": "Analytics mode"},
		"infer_db_refs":     map[string]any{"type": "boolean", "description": "Infer database references"},
		"backend":           map[string]any{"type": "string", "description": "Filesystem backend"},
		"bucket":            map[string]any{"type": "string", "description": "Filesystem bucket"},
		"region":            map[string]any{"type": "string", "description": "Cloud region"},
		"endpoint":          map[string]any{"type": "string", "description": "Cloud endpoint"},
		"prefix":            map[string]any{"type": "string", "description": "Filesystem prefix"},
		"root":              map[string]any{"type": "string", "description": "Filesystem root"},
		"specs_dir":         map[string]any{"type": "string", "description": "OpenAPI specs directory"},
		"specs": map[string]any{
			"type": "object", "description": "OpenAPI spec configurations keyed by spec name",
			"additionalProperties": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"base_url":           map[string]any{"type": "string"},
					"timeout":            map[string]any{"type": "integer", "minimum": 0, "description": "Upstream timeout in nanoseconds (Go duration); YAML accepts values such as 5s"},
					"max_request_bytes":  map[string]any{"type": "integer", "minimum": 0},
					"max_response_bytes": map[string]any{"type": "integer", "minimum": 0},
					"operations": map[string]any{
						"type": "object",
						"additionalProperties": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"expose_as":             map[string]any{"type": "string"},
								"expose_mutation":       map[string]any{"type": "boolean"},
								"allowed_roles":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
								"retry_on_auth_failure": map[string]any{"type": "boolean"},
							},
						},
					},
				},
			},
		},
		"capabilities":             map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "boolean"}, "description": "Source capabilities"},
		"access":                   map[string]any{"type": "object", "description": "Source access policy"},
		"max_open_conns":           map[string]any{"type": "number", "description": "Maximum open connections"},
		"max_idle_conns":           map[string]any{"type": "number", "description": "Maximum idle connections"},
		"pool_size":                map[string]any{"type": "number", "description": "Pool size"},
		"max_connections":          map[string]any{"type": "number", "description": "Maximum connections"},
		"enable_tls":               map[string]any{"type": "boolean", "description": "Enable TLS"},
		"server_name":              map[string]any{"type": "string", "description": "TLS server name"},
		"server_cert":              map[string]any{"type": "string", "description": "TLS server certificate"},
		"client_cert":              map[string]any{"type": "string", "description": "TLS client certificate"},
		"client_key":               map[string]any{"type": "string", "description": "TLS client key"},
		"encrypt":                  map[string]any{"type": "boolean", "description": "MSSQL encrypt"},
		"trust_server_certificate": map[string]any{"type": "boolean", "description": "MSSQL trust server certificate"},
		"private_key_path":         map[string]any{"type": "string", "description": "Snowflake private key path"},
		"private_key_pem":          map[string]any{"type": "string", "description": "Snowflake private key PEM"},
		"key_passphrase":           map[string]any{"type": "string", "description": "Snowflake private key passphrase"},
	}
	return map[string]any{
		"type":                 "object",
		"required":             required,
		"properties":           props,
		"additionalProperties": true,
	}
}

func applySourceConfigMergePatches(existing []core.SourceConfig, patches []any) ([]core.SourceConfig, []string, error) {
	out := append([]core.SourceConfig(nil), existing...)
	positions := make(map[string]int, len(out))
	for i, source := range out {
		name := strings.TrimSpace(source.Name)
		if name == "" {
			return nil, nil, fmt.Errorf("existing source at index %d is missing name", i)
		}
		key := strings.ToLower(name)
		if _, ok := positions[key]; ok {
			return nil, nil, fmt.Errorf("duplicate existing source %q", name)
		}
		positions[key] = i
	}

	seenPatches := make(map[string]struct{}, len(patches))
	changes := make([]string, 0, len(patches))
	for i, item := range patches {
		patch, name, err := sourcePatchMap(item)
		if err != nil {
			return nil, nil, fmt.Errorf("[%d]: %w", i, err)
		}
		key := strings.ToLower(name)
		if _, ok := seenPatches[key]; ok {
			return nil, nil, fmt.Errorf("[%d]: duplicate source patch %q", i, name)
		}
		seenPatches[key] = struct{}{}

		pos, exists := positions[key]
		var base map[string]any
		if exists {
			base, err = sourceConfigMap(out[pos])
			if err != nil {
				return nil, nil, fmt.Errorf("[%d]: source %q: %w", i, name, err)
			}
		} else {
			if strings.TrimSpace(sourcePatchString(patch, "kind")) == "" {
				return nil, nil, fmt.Errorf("[%d]: new source %q requires kind", i, name)
			}
			base = make(map[string]any)
		}

		mergeJSONPatch(base, patch)
		base["name"] = name
		if strings.TrimSpace(sourcePatchString(base, "kind")) == "" {
			return nil, nil, fmt.Errorf("[%d]: source %q kind cannot be empty", i, name)
		}
		parsed, err := parseSourceConfigList([]any{base})
		if err != nil {
			return nil, nil, fmt.Errorf("[%d]: source %q: %w", i, name, err)
		}
		if len(parsed) != 1 {
			return nil, nil, fmt.Errorf("[%d]: source %q parsed to %d entries", i, name, len(parsed))
		}
		if exists {
			out[pos] = parsed[0]
			changes = append(changes, fmt.Sprintf("updated source: %s", name))
			continue
		}
		positions[key] = len(out)
		out = append(out, parsed[0])
		changes = append(changes, fmt.Sprintf("added source: %s", name))
	}

	return out, changes, nil
}

func removeSourceConfigs(existing []core.SourceConfig, names []any) ([]core.SourceConfig, []string, error) {
	remove := make(map[string]string, len(names))
	for i, item := range names {
		name, ok := item.(string)
		if !ok {
			return nil, nil, fmt.Errorf("[%d]: source name must be a string", i)
		}
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, nil, fmt.Errorf("[%d]: source name is required", i)
		}
		key := strings.ToLower(name)
		if _, ok := remove[key]; !ok {
			remove[key] = name
		}
	}
	if len(remove) == 0 {
		return append([]core.SourceConfig(nil), existing...), nil, nil
	}

	out := make([]core.SourceConfig, 0, len(existing))
	changes := make([]string, 0, len(remove))
	for _, source := range existing {
		key := strings.ToLower(strings.TrimSpace(source.Name))
		name, shouldRemove := remove[key]
		if !shouldRemove {
			out = append(out, source)
			continue
		}
		changes = append(changes, fmt.Sprintf("removed source: %s", name))
		delete(remove, key)
	}
	return out, changes, nil
}

func sourcePatchMap(item any) (map[string]any, string, error) {
	patch, ok := item.(map[string]any)
	if !ok {
		return nil, "", fmt.Errorf("source patch must be an object")
	}
	rawName, ok := patch["name"]
	if !ok {
		return nil, "", fmt.Errorf("source patch requires name")
	}
	name, ok := rawName.(string)
	if !ok || strings.TrimSpace(name) == "" {
		return nil, "", fmt.Errorf("source patch name must be a non-empty string")
	}
	if patch["name"] == nil {
		return nil, "", fmt.Errorf("source patch name cannot be null")
	}
	cp := make(map[string]any, len(patch))
	for k, v := range patch {
		cp[k] = v
	}
	return cp, strings.TrimSpace(name), nil
}

func sourceConfigMap(source core.SourceConfig) (map[string]any, error) {
	data, err := json.Marshal(source)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func sourcePatchString(m map[string]any, key string) string {
	value, ok := m[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func mergeJSONPatch(dst, patch map[string]any) {
	for key, value := range patch {
		if value == nil {
			delete(dst, key)
			continue
		}
		pm, patchIsMap := value.(map[string]any)
		dm, dstIsMap := dst[key].(map[string]any)
		if patchIsMap && dstIsMap {
			mergeJSONPatch(dm, pm)
			dst[key] = dm
			continue
		}
		dst[key] = value
	}
}

type sourceConfigPatch struct {
	Name   string                   `json:"name"`
	Access *sourceAccessConfigPatch `json:"access,omitempty"`
}

type sourceAccessConfigPatch struct {
	Read                   *string           `json:"read,omitempty"`
	Write                  *string           `json:"write,omitempty"`
	Delete                 *string           `json:"delete,omitempty"`
	NamespaceColumn        *string           `json:"namespace_column,omitempty"`
	OwnerColumn            *string           `json:"owner_column,omitempty"`
	MissingNamespaceColumn *string           `json:"missing_namespace_column,omitempty"`
	PublicTablesAdd        []string          `json:"public_tables_add,omitempty"`
	PublicTablesRemove     []string          `json:"public_tables_remove,omitempty"`
	AdminTablesAdd         []string          `json:"admin_tables_add,omitempty"`
	AdminTablesRemove      []string          `json:"admin_tables_remove,omitempty"`
	BlockedTablesAdd       []string          `json:"blocked_tables_add,omitempty"`
	BlockedTablesRemove    []string          `json:"blocked_tables_remove,omitempty"`
	RootsSet               map[string]string `json:"roots_set,omitempty"`
	RootsRemove            []string          `json:"roots_remove,omitempty"`
}

func applySourceAccessConfigPatches(conf *core.Config, items []any) ([]string, error) {
	if conf == nil {
		return nil, fmt.Errorf("config is nil")
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("at least one source patch is required")
	}
	data, err := json.Marshal(items)
	if err != nil {
		return nil, err
	}
	var patches []sourceConfigPatch
	if err := json.Unmarshal(data, &patches); err != nil {
		return nil, err
	}
	if len(patches) == 0 {
		return nil, fmt.Errorf("at least one source patch is required")
	}

	exact := make(map[string]int, len(conf.Sources))
	folded := make(map[string][]string, len(conf.Sources))
	for i, source := range conf.Sources {
		name := strings.TrimSpace(source.Name)
		if name == "" {
			continue
		}
		if _, exists := exact[name]; exists {
			return nil, fmt.Errorf("duplicate configured source name %q", name)
		}
		exact[name] = i
		key := strings.ToLower(name)
		folded[key] = append(folded[key], name)
	}

	seenPatch := make(map[string]struct{}, len(patches))
	var changes []string
	for _, patch := range patches {
		name := strings.TrimSpace(patch.Name)
		if name == "" {
			return changes, fmt.Errorf("source patch name is required")
		}
		if _, exists := seenPatch[name]; exists {
			return changes, fmt.Errorf("duplicate source patch for %q", name)
		}
		seenPatch[name] = struct{}{}

		idx, ok := exact[name]
		if !ok {
			matches := folded[strings.ToLower(name)]
			switch len(matches) {
			case 0:
				return changes, fmt.Errorf("source %q is not configured", name)
			case 1:
				return changes, fmt.Errorf("source %q is not configured; source names must match exactly (did you mean %q?)", name, matches[0])
			default:
				return changes, fmt.Errorf("source %q is ambiguous; configured source names differ only by case: %s", name, strings.Join(matches, ", "))
			}
		}

		if patch.Access == nil {
			continue
		}
		source := &conf.Sources[idx]
		patchChanges, err := applySourceAccessPatch(source, *patch.Access)
		if err != nil {
			return changes, fmt.Errorf("%s.access: %w", name, err)
		}
		changes = append(changes, patchChanges...)
	}
	if err := conf.ValidateIsSourcesUsed(); err != nil {
		return changes, err
	}
	sort.Strings(changes)
	return changes, nil
}

func applySourceAccessPatch(source *core.SourceConfig, patch sourceAccessConfigPatch) ([]string, error) {
	var changes []string
	setMode := func(label string, ptr *string, valid func(string) bool) error {
		if ptr == nil {
			return nil
		}
		mode := strings.ToLower(strings.TrimSpace(*ptr))
		if mode == "" {
			return fmt.Errorf("%s must not be empty", label)
		}
		if !valid(mode) {
			return fmt.Errorf("%s: unsupported access mode %q", label, *ptr)
		}
		switch label {
		case "read":
			source.Access.Read = mode
		case "write":
			source.Access.Write = mode
		case "delete":
			source.Access.Delete = mode
		}
		changes = append(changes, fmt.Sprintf("updated source %s access.%s", source.Name, label))
		return nil
	}
	if err := setMode("read", patch.Read, validSourcePatchReadMode); err != nil {
		return changes, err
	}
	if err := setMode("write", patch.Write, validSourcePatchWriteMode); err != nil {
		if patch.Write != nil && strings.EqualFold(strings.TrimSpace(*patch.Write), core.AccessModePublic) {
			return changes, fmt.Errorf("write: public write is not supported")
		}
		return changes, err
	}
	if err := setMode("delete", patch.Delete, validSourcePatchWriteMode); err != nil {
		if patch.Delete != nil && strings.EqualFold(strings.TrimSpace(*patch.Delete), core.AccessModePublic) {
			return changes, fmt.Errorf("delete: public delete is not supported")
		}
		return changes, err
	}
	if patch.NamespaceColumn != nil {
		value := strings.TrimSpace(*patch.NamespaceColumn)
		if value == "" {
			return changes, fmt.Errorf("namespace_column must not be empty")
		}
		source.Access.NamespaceColumn = value
		changes = append(changes, fmt.Sprintf("updated source %s access.namespace_column", source.Name))
	}
	if patch.OwnerColumn != nil {
		value := strings.TrimSpace(*patch.OwnerColumn)
		if value == "" {
			return changes, fmt.Errorf("owner_column must not be empty")
		}
		source.Access.OwnerColumn = value
		changes = append(changes, fmt.Sprintf("updated source %s access.owner_column", source.Name))
	}
	if patch.MissingNamespaceColumn != nil {
		value := strings.ToLower(strings.TrimSpace(*patch.MissingNamespaceColumn))
		switch value {
		case core.MissingNamespaceBlock, core.MissingNamespaceAllow:
			source.Access.MissingNamespaceColumn = value
			changes = append(changes, fmt.Sprintf("updated source %s access.missing_namespace_column", source.Name))
		default:
			return changes, fmt.Errorf("missing_namespace_column: unsupported behavior %q", *patch.MissingNamespaceColumn)
		}
	}
	classChanges, err := applySourceClassificationPatch(source, patch)
	if err != nil {
		return changes, err
	}
	changes = append(changes, classChanges...)

	if len(patch.RootsSet) != 0 || len(patch.RootsRemove) != 0 {
		return changes, fmt.Errorf("roots_set and roots_remove were removed from source_patches; use system.root_access")
	}
	return changes, nil
}

func validSourcePatchReadMode(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case core.AccessModeBlocked, core.AccessModePublic, core.AccessModeAuthenticated, core.AccessModeAccount, core.AccessModeOwner, core.AccessModeAdmin:
		return true
	default:
		return false
	}
}

func validSourcePatchWriteMode(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case core.AccessModeBlocked, core.AccessModeAuthenticated, core.AccessModeAccount, core.AccessModeOwner, core.AccessModeAdmin:
		return true
	default:
		return false
	}
}

func applySourceClassificationPatch(source *core.SourceConfig, patch sourceAccessConfigPatch) ([]string, error) {
	adds := map[string][]string{
		"public_tables":  patch.PublicTablesAdd,
		"admin_tables":   patch.AdminTablesAdd,
		"blocked_tables": patch.BlockedTablesAdd,
	}
	removes := map[string][]string{
		"public_tables":  patch.PublicTablesRemove,
		"admin_tables":   patch.AdminTablesRemove,
		"blocked_tables": patch.BlockedTablesRemove,
	}
	addTargets := make(map[string]string)
	for group, values := range adds {
		seen, err := normalizedStringSet(values)
		if err != nil {
			return nil, fmt.Errorf("%s_add: %w", group, err)
		}
		for table := range seen {
			key := strings.ToLower(table)
			if other, exists := addTargets[key]; exists && other != group {
				return nil, fmt.Errorf("table %q appears in multiple classification add lists (%s, %s)", table, other, group)
			}
			addTargets[key] = group
		}
	}

	var changes []string
	for group, values := range removes {
		set, err := normalizedStringSet(values)
		if err != nil {
			return changes, fmt.Errorf("%s_remove: %w", group, err)
		}
		for table := range set {
			switch group {
			case "public_tables":
				source.Access.PublicTables = removeStringFold(source.Access.PublicTables, table)
			case "admin_tables":
				source.Access.AdminTables = removeStringFold(source.Access.AdminTables, table)
			case "blocked_tables":
				source.Access.BlockedTables = removeStringFold(source.Access.BlockedTables, table)
			}
			changes = append(changes, fmt.Sprintf("removed %s from source %s access.%s", table, source.Name, group))
		}
	}
	for group, values := range adds {
		set, err := normalizedStringSet(values)
		if err != nil {
			return changes, fmt.Errorf("%s_add: %w", group, err)
		}
		for table := range set {
			source.Access.PublicTables = removeStringFold(source.Access.PublicTables, table)
			source.Access.AdminTables = removeStringFold(source.Access.AdminTables, table)
			source.Access.BlockedTables = removeStringFold(source.Access.BlockedTables, table)
			switch group {
			case "public_tables":
				source.Access.PublicTables = appendUniqueFold(source.Access.PublicTables, table)
			case "admin_tables":
				source.Access.AdminTables = appendUniqueFold(source.Access.AdminTables, table)
			case "blocked_tables":
				source.Access.BlockedTables = appendUniqueFold(source.Access.BlockedTables, table)
			}
			changes = append(changes, fmt.Sprintf("added %s to source %s access.%s", table, source.Name, group))
		}
	}
	return changes, nil
}

func normalizedStringSet(values []string) (map[string]struct{}, error) {
	out := make(map[string]struct{}, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, fmt.Errorf("values must not be empty")
		}
		key := strings.ToLower(value)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out[value] = struct{}{}
	}
	return out, nil
}

func appendUniqueFold(values []string, value string) []string {
	for _, existing := range values {
		if strings.EqualFold(existing, value) {
			return values
		}
	}
	return append(values, value)
}

func removeStringFold(values []string, value string) []string {
	out := values[:0]
	for _, existing := range values {
		if strings.EqualFold(existing, value) {
			continue
		}
		out = append(out, existing)
	}
	return out
}

func parseRelationshipConfigList(items []any) ([]core.RelationshipConfig, error) {
	data, err := json.Marshal(items)
	if err != nil {
		return nil, err
	}
	var out []core.RelationshipConfig
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func validateMCPConfigPatch(patch map[string]interface{}) ([]string, error) {
	var changes []string
	for key, value := range patch {
		if _, ok := value.(bool); !ok {
			return changes, fmt.Errorf("mcp.%s must be boolean", key)
		}
		switch key {
		case "allow_workflow_updates", "allow_workflow_execution", "allow_config_updates", "allow_schema_reload", "allow_schema_updates", "allow_dev_tools", "allow_raw_queries", "legacy_discovery":
			changes = append(changes, "updated mcp."+key)
		default:
			return changes, fmt.Errorf("unsupported mcp config key: %s", key)
		}
	}
	sort.Strings(changes)
	return changes, nil
}

func applyMCPConfigPatch(conf *Config, patch map[string]interface{}) ([]string, error) {
	changes, err := validateMCPConfigPatch(patch)
	if err != nil {
		return changes, err
	}
	for key, value := range patch {
		v := value.(bool)
		switch key {
		case "allow_workflow_updates":
			conf.MCP.AllowWorkflowUpdates = v
		case "allow_workflow_execution":
			conf.MCP.AllowWorkflowExecution = v
		case "allow_config_updates":
			conf.MCP.AllowConfigUpdates = v
		case "allow_schema_reload":
			conf.MCP.AllowSchemaReload = v
		case "allow_schema_updates":
			conf.MCP.AllowSchemaUpdates = v
		case "allow_dev_tools":
			conf.MCP.AllowDevTools = v
		case "allow_raw_queries":
			conf.MCP.AllowRawQueries = v
		case "legacy_discovery":
			conf.MCP.LegacyDiscovery = v
		}
	}
	return changes, nil
}

// Serv-config patching lets the runtime config machinery reach server-side
// settings (serv.Config), not just the compiler core. The writable set is a
// deliberately small v1 allowlist; secret-bearing sections (auth, redis,
// uploads) stay read-only on gj_config until secret-ref handling lands.
const (
	servReloadHot     = "hot"     // read live, effective immediately
	servReloadRestart = "restart" // persisted, effective after a restart
)

// servWritableReload maps each writable serv key to how a change takes effect.
var servWritableReload = map[string]string{
	"agent":         servReloadHot, // the agent runtime reads config per request
	"log_level":     servReloadRestart,
	"log_format":    servReloadRestart,
	"web_ui":        servReloadRestart,
	"http_compress": servReloadRestart,
	"server_timing": servReloadRestart,
	"rate_limiter":  servReloadRestart,
}

// agentWritableFields is the subset of agent settings safe to change at
// runtime. Structural fields (enabled, provider, api_key_env, base_url) gate
// startup wiring or name secrets and are excluded.
var agentWritableFields = map[string]bool{
	"model": true, "max_steps": true, "timeout_seconds": true,
	"structured_output_mode": true,
	// response_format is the deprecated alias of structured_output_mode; it
	// stays writable so existing automation keeps working.
	"response_format": true,
	"read_only":       true, "return_trace": true,
	"seed_limit": true, "catalog_default_limit": true,
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func strInSet(s string, allowed ...string) bool {
	for _, a := range allowed {
		if s == a {
			return true
		}
	}
	return false
}

// validateServConfigPatch checks a serv patch against the v1 allowlist without
// mutating anything. It returns the change descriptions and the overall reload
// class ("restart" if any change needs a restart, otherwise "hot").
func validateServConfigPatch(patch map[string]any) (changes []string, reload string, err error) {
	reload = servReloadHot
	for _, key := range sortedKeys(patch) {
		cls, ok := servWritableReload[key]
		if !ok {
			return nil, "", fmt.Errorf("serv: %q is not a writable server setting; writable keys: %s", key, strings.Join(sortedKeys(servWritableReload), ", "))
		}
		switch key {
		case "agent":
			m, ok := patch[key].(map[string]any)
			if !ok {
				return nil, "", fmt.Errorf("serv.agent must be an object")
			}
			if _, removed := m["sampling"]; removed {
				return nil, "", fmt.Errorf("serv.agent.sampling was removed: configure GraphJin-owned agent.provider, agent.model, and agent.api_key_env credentials")
			}
			for f := range m {
				if !agentWritableFields[f] {
					return nil, "", fmt.Errorf("serv.agent.%s is not writable; writable agent fields: %s", f, strings.Join(sortedKeys(agentWritableFields), ", "))
				}
			}
			structuredMode, legacyFormat := "", ""
			if mode, ok := m["structured_output_mode"]; ok {
				value, ok := mode.(string)
				if !ok {
					return nil, "", fmt.Errorf("serv.agent.structured_output_mode must be a string")
				}
				structuredMode = value
			}
			if responseFormat, ok := m["response_format"]; ok {
				value, ok := responseFormat.(string)
				if !ok {
					return nil, "", fmt.Errorf("serv.agent.response_format must be a string")
				}
				legacyFormat = value
			}
			if structuredMode != "" || legacyFormat != "" {
				if err := gjagent.ValidateStructuredOutputMode(structuredMode, legacyFormat); err != nil {
					return nil, "", err
				}
			}
			changes = append(changes, "updated serv.agent")
		case "log_level":
			if s, ok := patch[key].(string); !ok || !strInSet(s, "debug", "error", "warn", "info") {
				return nil, "", fmt.Errorf("serv.log_level must be one of debug, error, warn, info")
			}
			changes = append(changes, "updated serv.log_level")
		case "log_format":
			if s, ok := patch[key].(string); !ok || !strInSet(s, "auto", "json", "simple") {
				return nil, "", fmt.Errorf("serv.log_format must be one of auto, json, simple")
			}
			changes = append(changes, "updated serv.log_format")
		case "web_ui", "http_compress", "server_timing":
			if _, ok := patch[key].(bool); !ok {
				return nil, "", fmt.Errorf("serv.%s must be a boolean", key)
			}
			changes = append(changes, "updated serv."+key)
		case "rate_limiter":
			m, ok := patch[key].(map[string]any)
			if !ok {
				return nil, "", fmt.Errorf("serv.rate_limiter must be an object")
			}
			for f := range m {
				if !strInSet(f, "rate", "bucket", "ip_header") {
					return nil, "", fmt.Errorf("serv.rate_limiter.%s is not writable; writable fields: bucket, ip_header, rate", f)
				}
			}
			changes = append(changes, "updated serv.rate_limiter")
		}
		if cls == servReloadRestart {
			reload = servReloadRestart
		}
	}
	return changes, reload, nil
}

// applyServConfigPatch mutates conf.Serv in place. It assumes the patch already
// passed validateServConfigPatch, so it coerces defensively but does not
// re-report type errors.
func applyServConfigPatch(conf *Config, patch map[string]any) {
	for key, raw := range patch {
		switch key {
		case "agent":
			applyAgentConfigPatch(&conf.Serv.Agent, raw.(map[string]any))
		case "log_level":
			conf.Serv.LogLevel, _ = raw.(string)
		case "log_format":
			conf.Serv.LogFormat, _ = raw.(string)
		case "web_ui":
			conf.Serv.WebUI, _ = raw.(bool)
		case "http_compress":
			conf.Serv.HTTPGZip, _ = raw.(bool)
		case "server_timing":
			conf.Serv.ServerTiming, _ = raw.(bool)
		case "rate_limiter":
			applyRateLimiterPatch(&conf.Serv.RateLimiter, raw.(map[string]any))
		}
	}
}

func applyAgentConfigPatch(a *AgentConfig, m map[string]any) {
	if v, ok := m["model"].(string); ok {
		a.Model = v
	}
	if v, ok := m["response_format"].(string); ok {
		a.ResponseFormat = strings.TrimSpace(v)
		a.StructuredOutputMode = gjagent.EffectiveStructuredOutputMode("", v)
	}
	// Applied after the alias so an explicit canonical value in the same patch
	// wins, matching the precedence config loading uses.
	if v, ok := m["structured_output_mode"].(string); ok {
		a.StructuredOutputMode = gjagent.EffectiveStructuredOutputMode(v, "")
	}
	if v, ok := configInt(m["max_steps"]); ok {
		a.MaxSteps = v
	}
	if v, ok := configInt(m["timeout_seconds"]); ok {
		a.TimeoutSeconds = v
	}
	if v, ok := configInt(m["seed_limit"]); ok {
		a.SeedLimit = v
	}
	if v, ok := configInt(m["catalog_default_limit"]); ok {
		a.CatalogDefaultLimit = v
	}
	if v, ok := m["read_only"].(bool); ok {
		a.ReadOnly = v
	}
	if v, ok := m["return_trace"].(bool); ok {
		a.ReturnTrace = v
	}
}

func applyRateLimiterPatch(r *RateLimiter, m map[string]any) {
	if v, ok := m["rate"].(float64); ok {
		r.Rate = v
	}
	if v, ok := configInt(m["bucket"]); ok {
		r.Bucket = v
	}
	if v, ok := m["ip_header"].(string); ok {
		r.IPHeader = v
	}
}

// configInt coerces a JSON number (float64) or int into an int.
func configInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	default:
		return 0, false
	}
}

// parseDBConfig parses a database config from a map
func parseDBConfig(m map[string]any) (core.DatabaseConfig, error) {
	var conf core.DatabaseConfig

	if t, ok := m["type"].(string); ok {
		conf.Type = t
	}
	if cs, ok := m["connection_string"].(string); ok {
		conf.ConnString = cs
	}
	if h, ok := m["host"].(string); ok {
		conf.Host = h
	}
	if p, ok := m["port"].(float64); ok {
		conf.Port = int(p)
	}
	if db, ok := m["dbname"].(string); ok {
		conf.DBName = db
	}
	if u, ok := m["user"].(string); ok {
		conf.User = u
	}
	if pw, ok := m["password"].(string); ok {
		conf.Password = pw
	}
	if path, ok := m["path"].(string); ok {
		conf.Path = path
	}
	if s, ok := m["schema"].(string); ok {
		conf.Schema = s
	}
	if ro, ok := m["read_only"].(bool); ok {
		conf.ReadOnly = ro
	}
	if infer, ok := m["infer_db_refs"].(bool); ok {
		conf.InferDBRefs = &infer
	}
	if pkp, ok := m["private_key_path"].(string); ok {
		conf.PrivateKeyPath = pkp
	}
	if pkpem, ok := m["private_key_pem"].(string); ok {
		conf.PrivateKeyPEM = pkpem
	}
	if kp, ok := m["key_passphrase"].(string); ok {
		conf.KeyPassphrase = kp
	}

	// Validate type
	if conf.Type == "" {
		return conf, fmt.Errorf("database type is required")
	}
	conf.Type = strings.ToLower(strings.TrimSpace(conf.Type))
	if err := validateServiceMultiDBType(conf.Type); err != nil {
		return conf, err
	}
	if isCodeSQLType(conf.Type) && strings.TrimSpace(conf.Path) == "" {
		return conf, fmt.Errorf("codesql requires path")
	}

	if conf.Type == "snowflake" && strings.TrimSpace(conf.ConnString) == "" {
		return conf, fmt.Errorf("snowflake requires connection_string")
	}

	return conf, nil
}

func parseMetadataConfig(m map[string]any, current core.MetadataConfig) core.MetadataConfig {
	if enabled, ok := m["enabled"].(bool); ok {
		current.Enabled = &enabled
	}
	if database, ok := m["database"].(string); ok {
		current.Database = database
	}
	if auto, ok := m["auto_code_relations"].(bool); ok {
		current.AutoCodeRelations = &auto
	}
	if raw, ok := m["code_databases"].([]any); ok {
		current.CodeDatabases = current.CodeDatabases[:0]
		for _, v := range raw {
			if s, ok := v.(string); ok && s != "" {
				current.CodeDatabases = append(current.CodeDatabases, s)
			}
		}
	}
	return current
}

// parseTableConfig parses a table config from a map
func parseTableConfig(m map[string]any) (core.Table, error) {
	var table core.Table

	if name, ok := m["name"].(string); ok && name != "" {
		table.Name = name
	} else {
		return table, fmt.Errorf("table name is required")
	}

	if db, ok := m["database"].(string); ok {
		table.Database = db
	}
	if source, ok := m["source"].(string); ok {
		table.Source = source
	}
	if readOnly, ok := m["read_only"].(bool); ok {
		table.ReadOnly = readOnly
	}
	if schema, ok := m["schema"].(string); ok {
		table.Schema = schema
	}
	if t, ok := m["type"].(string); ok {
		table.Type = t
	}

	if bl, ok := m["blocklist"].([]any); ok {
		for _, item := range bl {
			if s, ok := item.(string); ok {
				table.Blocklist = append(table.Blocklist, s)
			}
		}
	}

	if cols, ok := m["columns"].([]any); ok {
		for _, colAny := range cols {
			if colMap, ok := colAny.(map[string]any); ok {
				col := core.Column{}
				if name, ok := colMap["name"].(string); ok {
					col.Name = name
				}
				if t, ok := colMap["type"].(string); ok {
					col.Type = t
				}
				if primary, ok := colMap["primary"].(bool); ok {
					col.Primary = primary
				}
				if array, ok := colMap["array"].(bool); ok {
					col.Array = array
				}
				if ft, ok := colMap["full_text"].(bool); ok {
					col.FullText = ft
				}
				if fk, ok := colMap["related_to"].(string); ok {
					col.ForeignKey = fk
				}
				table.Columns = append(table.Columns, col)
			}
		}
	}

	if orderBy, ok := m["order_by"].(map[string]any); ok {
		table.OrderBy = make(map[string][]string)
		for key, val := range orderBy {
			if arr, ok := val.([]any); ok {
				for _, item := range arr {
					if s, ok := item.(string); ok {
						table.OrderBy[key] = append(table.OrderBy[key], s)
					}
				}
			}
		}
	}

	return table, nil
}

// parseRoleConfig parses a role config from a map
func parseRoleConfig(m map[string]any) (core.Role, error) {
	var role core.Role

	if name, ok := m["name"].(string); ok && name != "" {
		role.Name = name
	} else {
		return role, fmt.Errorf("role name is required")
	}

	if comment, ok := m["comment"].(string); ok {
		role.Comment = comment
	}
	if match, ok := m["match"].(string); ok {
		role.Match = match
	}

	if tables, ok := m["tables"].([]any); ok {
		for _, tableAny := range tables {
			if tableMap, ok := tableAny.(map[string]any); ok {
				rt, err := parseRoleTableConfig(tableMap)
				if err != nil {
					return role, err
				}
				role.Tables = append(role.Tables, rt)
			}
		}
	}

	return role, nil
}

// parseRoleTableConfig parses a role table config from a map
func parseRoleTableConfig(m map[string]any) (core.RoleTable, error) {
	var rt core.RoleTable

	if name, ok := m["name"].(string); ok && name != "" {
		rt.Name = name
	} else {
		return rt, fmt.Errorf("role table name is required")
	}

	if schema, ok := m["schema"].(string); ok {
		rt.Schema = schema
	}
	if database, ok := m["database"].(string); ok {
		rt.Database = database
	}
	if readOnly, ok := m["read_only"].(bool); ok {
		rt.ReadOnly = readOnly
	}

	if query, ok := m["query"].(map[string]any); ok {
		rt.Query = parseQueryConfig(query)
	}
	if insert, ok := m["insert"].(map[string]any); ok {
		rt.Insert = parseInsertConfig(insert)
	}
	if update, ok := m["update"].(map[string]any); ok {
		rt.Update = parseUpdateConfig(update)
	}
	if upsert, ok := m["upsert"].(map[string]any); ok {
		rt.Upsert = parseUpsertConfig(upsert)
	}
	if del, ok := m["delete"].(map[string]any); ok {
		rt.Delete = parseDeleteConfig(del)
	}

	return rt, nil
}

// parseQueryConfig parses query config from a map
func parseQueryConfig(m map[string]any) *core.Query {
	q := &core.Query{}
	if limit, ok := m["limit"].(float64); ok {
		q.Limit = int(limit)
	}
	if filters, ok := m["filters"].([]any); ok {
		for _, f := range filters {
			if s, ok := f.(string); ok {
				q.Filters = append(q.Filters, s)
			}
		}
	}
	if cols, ok := m["columns"].([]any); ok {
		for _, c := range cols {
			if s, ok := c.(string); ok {
				q.Columns = append(q.Columns, s)
			}
		}
	}
	if df, ok := m["disable_functions"].(bool); ok {
		q.DisableFunctions = df
	}
	if block, ok := m["block"].(bool); ok {
		q.Block = block
	}
	return q
}

// parseInsertConfig parses insert config from a map
func parseInsertConfig(m map[string]any) *core.Insert {
	i := &core.Insert{}
	if filters, ok := m["filters"].([]any); ok {
		for _, f := range filters {
			if s, ok := f.(string); ok {
				i.Filters = append(i.Filters, s)
			}
		}
	}
	if cols, ok := m["columns"].([]any); ok {
		for _, c := range cols {
			if s, ok := c.(string); ok {
				i.Columns = append(i.Columns, s)
			}
		}
	}
	if presets, ok := m["presets"].(map[string]any); ok {
		i.Presets = make(map[string]string)
		for k, v := range presets {
			if s, ok := v.(string); ok {
				i.Presets[k] = s
			}
		}
	}
	if block, ok := m["block"].(bool); ok {
		i.Block = block
	}
	return i
}

// parseUpdateConfig parses update config from a map
func parseUpdateConfig(m map[string]any) *core.Update {
	u := &core.Update{}
	if filters, ok := m["filters"].([]any); ok {
		for _, f := range filters {
			if s, ok := f.(string); ok {
				u.Filters = append(u.Filters, s)
			}
		}
	}
	if cols, ok := m["columns"].([]any); ok {
		for _, c := range cols {
			if s, ok := c.(string); ok {
				u.Columns = append(u.Columns, s)
			}
		}
	}
	if presets, ok := m["presets"].(map[string]any); ok {
		u.Presets = make(map[string]string)
		for k, v := range presets {
			if s, ok := v.(string); ok {
				u.Presets[k] = s
			}
		}
	}
	if block, ok := m["block"].(bool); ok {
		u.Block = block
	}
	return u
}

// parseUpsertConfig parses upsert config from a map
func parseUpsertConfig(m map[string]any) *core.Upsert {
	u := &core.Upsert{}
	if filters, ok := m["filters"].([]any); ok {
		for _, f := range filters {
			if s, ok := f.(string); ok {
				u.Filters = append(u.Filters, s)
			}
		}
	}
	if cols, ok := m["columns"].([]any); ok {
		for _, c := range cols {
			if s, ok := c.(string); ok {
				u.Columns = append(u.Columns, s)
			}
		}
	}
	if presets, ok := m["presets"].(map[string]any); ok {
		u.Presets = make(map[string]string)
		for k, v := range presets {
			if s, ok := v.(string); ok {
				u.Presets[k] = s
			}
		}
	}
	if block, ok := m["block"].(bool); ok {
		u.Block = block
	}
	return u
}

// parseDeleteConfig parses delete config from a map
func parseDeleteConfig(m map[string]any) *core.Delete {
	d := &core.Delete{}
	if filters, ok := m["filters"].([]any); ok {
		for _, f := range filters {
			if s, ok := f.(string); ok {
				d.Filters = append(d.Filters, s)
			}
		}
	}
	if cols, ok := m["columns"].([]any); ok {
		for _, c := range cols {
			if s, ok := c.(string); ok {
				d.Columns = append(d.Columns, s)
			}
		}
	}
	if block, ok := m["block"].(bool); ok {
		d.Block = block
	}
	return d
}

// parseFunctionConfig parses a function config from a map
func parseFunctionConfig(m map[string]any) (core.Function, error) {
	var fn core.Function

	if name, ok := m["name"].(string); ok && name != "" {
		fn.Name = name
	} else {
		return fn, fmt.Errorf("function name is required")
	}

	if schema, ok := m["schema"].(string); ok {
		fn.Schema = schema
	}
	if rt, ok := m["return_type"].(string); ok {
		fn.ReturnType = rt
	}

	return fn, nil
}

// parseResolverConfig parses a resolver config from a map
func parseResolverConfig(m map[string]any) (core.ResolverConfig, error) {
	var rc core.ResolverConfig

	if name, ok := m["name"].(string); ok && name != "" {
		rc.Name = name
	} else {
		return rc, fmt.Errorf("resolver name is required")
	}

	if t, ok := m["type"].(string); ok && t != "" {
		if !strings.EqualFold(t, "remote_api") {
			return rc, fmt.Errorf("invalid resolver type: %s (must be 'remote_api')", t)
		}
		rc.Type = t
	} else {
		return rc, fmt.Errorf("resolver type is required")
	}

	if table, ok := m["table"].(string); ok && table != "" {
		rc.Table = table
	} else {
		return rc, fmt.Errorf("resolver table is required")
	}

	if column, ok := m["column"].(string); ok {
		rc.Column = column
	}
	if schema, ok := m["schema"].(string); ok {
		rc.Schema = schema
	}
	if stripPath, ok := m["strip_path"].(string); ok {
		rc.StripPath = stripPath
	}

	// Build Props map from url, debug, pass_headers, set_headers
	props := make(core.ResolverProps)

	if url, ok := m["url"].(string); ok && url != "" {
		props["url"] = url
	}
	if debug, ok := m["debug"].(bool); ok {
		props["debug"] = debug
	}
	if passHeaders, ok := m["pass_headers"].([]any); ok {
		var headers []string
		for _, h := range passHeaders {
			if s, ok := h.(string); ok {
				headers = append(headers, s)
			}
		}
		if len(headers) > 0 {
			props["pass_headers"] = headers
		}
	}
	if setHeaders, ok := m["set_headers"].([]any); ok {
		headerMap := make(map[string]string)
		for _, sh := range setHeaders {
			if shMap, ok := sh.(map[string]any); ok {
				name, _ := shMap["name"].(string)
				value, _ := shMap["value"].(string)
				if name != "" {
					headerMap[name] = value
				}
			}
		}
		if len(headerMap) > 0 {
			props["set_headers"] = headerMap
		}
	}

	if len(props) > 0 {
		rc.Props = props
	}

	return rc, nil
}

type stagedRuntimeState struct {
	dbs               map[string]*sql.DB
	managedDBs        map[string]managedDB
	runtimeCore       *core.Config
	metadataDB        string
	managedArtifactDB string
	systemNanoDB      *core.NanoDB
	gj                *core.GraphJin
	availableDBs      []string
	newConnections    map[string]*sql.DB
	schemaNotReady    bool
}

func (ms *mcpServer) initStagedSystemNano(stagedCore *core.Config, stage *stagedRuntimeState) error {
	if ms == nil || ms.service == nil || stagedCore == nil || stage == nil || stage.runtimeCore == nil {
		return nil
	}
	scopedConf := *ms.service.conf
	scopedConf.Core = *stagedCore
	roots := registeredSystemRoots(&scopedConf)
	if len(roots) == 0 {
		stage.metadataDB = ""
		stage.systemNanoDB = nil
		return nil
	}
	metadataDB := allocateRuntimeDatabaseName(internalSystemDatabaseBase, stagedCore, stage.runtimeCore, stage.dbs)
	rows := make(map[string][]core.NanoRow, len(roots))
	if ms.service.systemNanoDB != nil {
		if current := ms.service.systemNanoDB.Snapshot(); current != nil {
			for _, root := range roots {
				if currentRows, ok := current.Rows("", root); ok {
					rows[root] = currentRows
				}
			}
		}
	}
	nano, err := core.NewNanoDB(systemNanoSnapshotForRoots("", rows, roots))
	if err != nil {
		return err
	}
	stage.metadataDB = metadataDB
	stage.systemNanoDB = nano
	if stage.runtimeCore.Databases == nil {
		stage.runtimeCore.Databases = make(map[string]core.DatabaseConfig)
	}
	stage.runtimeCore.Databases[metadataDB] = core.DatabaseConfig{Type: "nanodb", ReadOnly: true}
	injectSystemNanoTablesInto(&scopedConf, stage.runtimeCore, metadataDB)
	applySystemRoleQueryDefaults(&scopedConf, stage.runtimeCore, metadataDB)
	if stagedCore.MetadataAutoCodeRelationsEnabled() {
		codeDBs := ms.service.selectedCodeSQLDatabasesFor(stagedCore, stage.managedDBs)
		if len(codeDBs) == 1 {
			injectMetadataCodeRelationships(stage.runtimeCore, metadataDB, codeDBs[0])
		} else if len(codeDBs) > 1 {
			ms.service.log.Warnf("metadata auto_code_relations skipped: multiple CodeSQL databases selected: %s", strings.Join(codeDBs, ", "))
		}
	}
	return nil
}

func (ms *mcpServer) attachStagedManagedArtifactStore(stagedCore *core.Config, stage *stagedRuntimeState) {
	if ms == nil || ms.service == nil || stagedCore == nil || stage == nil || stage.runtimeCore == nil ||
		!ms.service.conf.managedArtifactStore || !stagedCore.Artifacts.Enabled {
		return
	}
	oldName := ms.service.managedArtifactDB
	db := ms.service.dbs[oldName]
	if oldName == "" || db == nil {
		return
	}
	if stage.dbs[oldName] == db {
		delete(stage.dbs, oldName)
	}
	delete(stage.runtimeCore.Databases, oldName)
	name := allocateRuntimeDatabaseName(internalArtifactDatabaseBase, stagedCore, stage.runtimeCore, stage.dbs)
	dbConf := core.DatabaseConfig{Type: "sqlite", MaxOpenConns: 1, MaxIdleConns: 1}
	if ms.service.runtimeCore != nil {
		if current, ok := ms.service.runtimeCore.Databases[oldName]; ok {
			dbConf = current
		}
	}
	stage.dbs[name] = db
	stage.runtimeCore.Databases[name] = dbConf
	stage.runtimeCore.Artifacts.Source = name
	stage.managedArtifactDB = name
}

func (ms *mcpServer) stagedCoreOptions(stagedCore *core.Config, stage *stagedRuntimeState) []core.Option {
	scopedConf := *ms.service.conf
	scopedConf.Core = *stagedCore
	return ms.service.buildCoreOptionsForState(stage.dbs, stage.managedDBs, &scopedConf, stage.metadataDB, stage.managedArtifactDB, stage.systemNanoDB)
}

func (st *stagedRuntimeState) close() {
	if st.gj != nil {
		st.gj.Close()
	}
	closedManaged := make(map[string]struct{})
	for name, managed := range st.managedDBs {
		if managed.handle != nil {
			if st.newConnections[name] != managed.handle.DB {
				continue
			}
			managed.handle.Close() //nolint:errcheck
			closedManaged[name] = struct{}{}
		}
	}
	for name, db := range st.newConnections {
		if _, ok := closedManaged[name]; ok {
			continue
		}
		if db != nil {
			db.Close() //nolint:errcheck
		}
	}
}

func cloneCoreConfig(src core.Config) core.Config {
	dst := src

	if src.Databases != nil {
		dst.Databases = make(map[string]core.DatabaseConfig, len(src.Databases))
		for name, dbConf := range src.Databases {
			dst.Databases[name] = dbConf
		}
	}
	if src.Sources != nil {
		dst.Sources = append([]core.SourceConfig(nil), src.Sources...)
		for i := range dst.Sources {
			dst.Sources[i].Access = cloneSourceAccessConfig(src.Sources[i].Access)
			if src.Sources[i].Capabilities != nil {
				dst.Sources[i].Capabilities = make(map[string]bool, len(src.Sources[i].Capabilities))
				for name, value := range src.Sources[i].Capabilities {
					dst.Sources[i].Capabilities[name] = value
				}
			}
			if src.Sources[i].Specs != nil {
				dst.Sources[i].Specs = make(map[string]openapi.SpecConfig, len(src.Sources[i].Specs))
				for name, spec := range src.Sources[i].Specs {
					dst.Sources[i].Specs[name] = spec.Clone()
				}
			}
		}
	}
	if src.OpenAPI != nil {
		dst.OpenAPI = make(map[string]openapi.SpecConfig, len(src.OpenAPI))
		for name, spec := range src.OpenAPI {
			dst.OpenAPI[name] = spec.Clone()
		}
	}
	if src.System.Capabilities != nil {
		dst.System.Capabilities = make(map[string]bool, len(src.System.Capabilities))
		for key, value := range src.System.Capabilities {
			dst.System.Capabilities[key] = value
		}
	}
	if src.System.RootAccess != nil {
		dst.System.RootAccess = make(map[string]string, len(src.System.RootAccess))
		for root, mode := range src.System.RootAccess {
			dst.System.RootAccess[root] = mode
		}
	}
	if src.Workflows.Capabilities != nil {
		dst.Workflows.Capabilities = make(map[string]bool, len(src.Workflows.Capabilities))
		for key, value := range src.Workflows.Capabilities {
			dst.Workflows.Capabilities[key] = value
		}
	}
	if src.Relationships != nil {
		dst.Relationships = append([]core.RelationshipConfig(nil), src.Relationships...)
	}
	if src.Metadata.CodeDatabases != nil {
		dst.Metadata.CodeDatabases = append([]string(nil), src.Metadata.CodeDatabases...)
	}
	if src.Tables != nil {
		dst.Tables = append([]core.Table(nil), src.Tables...)
		for i := range dst.Tables {
			if src.Tables[i].Blocklist != nil {
				dst.Tables[i].Blocklist = append([]string(nil), src.Tables[i].Blocklist...)
			}
			if src.Tables[i].Columns != nil {
				dst.Tables[i].Columns = append([]core.Column(nil), src.Tables[i].Columns...)
			}
			if src.Tables[i].OrderBy != nil {
				dst.Tables[i].OrderBy = make(map[string][]string, len(src.Tables[i].OrderBy))
				for key, cols := range src.Tables[i].OrderBy {
					dst.Tables[i].OrderBy[key] = append([]string(nil), cols...)
				}
			}
		}
	}
	if src.Roles != nil {
		dst.Roles = append([]core.Role(nil), src.Roles...)
		for i := range dst.Roles {
			dst.Roles[i].Tables = append([]core.RoleTable(nil), src.Roles[i].Tables...)
			for j := range dst.Roles[i].Tables {
				dst.Roles[i].Tables[j] = cloneRoleTable(src.Roles[i].Tables[j])
			}
		}
	}
	if src.Blocklist != nil {
		dst.Blocklist = append([]string(nil), src.Blocklist...)
	}
	if src.Functions != nil {
		dst.Functions = append([]core.Function(nil), src.Functions...)
	}
	if src.Resolvers != nil {
		dst.Resolvers = append([]core.ResolverConfig(nil), src.Resolvers...)
		for i := range dst.Resolvers {
			if src.Resolvers[i].Props != nil {
				dst.Resolvers[i].Props = make(core.ResolverProps, len(src.Resolvers[i].Props))
				for key, val := range src.Resolvers[i].Props {
					dst.Resolvers[i].Props[key] = val
				}
			}
		}
	}

	return dst
}

func cloneSourceAccessConfig(src core.SourceAccessConfig) core.SourceAccessConfig {
	dst := src
	dst.PublicTables = append([]string(nil), src.PublicTables...)
	dst.AdminTables = append([]string(nil), src.AdminTables...)
	dst.BlockedTables = append([]string(nil), src.BlockedTables...)
	return dst
}

func cloneRoleTable(src core.RoleTable) core.RoleTable {
	dst := src
	if src.Query != nil {
		query := *src.Query
		query.Filters = append([]string(nil), src.Query.Filters...)
		query.Columns = append([]string(nil), src.Query.Columns...)
		dst.Query = &query
	}
	if src.Insert != nil {
		insert := *src.Insert
		insert.Filters = append([]string(nil), src.Insert.Filters...)
		insert.Columns = append([]string(nil), src.Insert.Columns...)
		if src.Insert.Presets != nil {
			insert.Presets = make(map[string]string, len(src.Insert.Presets))
			for key, val := range src.Insert.Presets {
				insert.Presets[key] = val
			}
		}
		dst.Insert = &insert
	}
	if src.Update != nil {
		update := *src.Update
		update.Filters = append([]string(nil), src.Update.Filters...)
		update.Columns = append([]string(nil), src.Update.Columns...)
		if src.Update.Presets != nil {
			update.Presets = make(map[string]string, len(src.Update.Presets))
			for key, val := range src.Update.Presets {
				update.Presets[key] = val
			}
		}
		dst.Update = &update
	}
	if src.Upsert != nil {
		upsert := *src.Upsert
		upsert.Filters = append([]string(nil), src.Upsert.Filters...)
		upsert.Columns = append([]string(nil), src.Upsert.Columns...)
		if src.Upsert.Presets != nil {
			upsert.Presets = make(map[string]string, len(src.Upsert.Presets))
			for key, val := range src.Upsert.Presets {
				upsert.Presets[key] = val
			}
		}
		dst.Upsert = &upsert
	}
	if src.Delete != nil {
		del := *src.Delete
		del.Filters = append([]string(nil), src.Delete.Filters...)
		del.Columns = append([]string(nil), src.Delete.Columns...)
		dst.Delete = &del
	}
	return dst
}

func anyDBFromMap(conf *core.Config, dbs map[string]*sql.DB) *sql.DB {
	names := make([]string, 0, len(dbs))
	for name := range dbs {
		if !isInternalArtifactDatabase(conf, name) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if dbs[name] != nil {
			return dbs[name]
		}
	}
	return nil
}

func primaryDBTypeFromCore(conf *core.Config) string {
	if len(conf.Databases) == 0 {
		return conf.DBType
	}
	names := make([]string, 0, len(conf.Databases))
	for name := range conf.Databases {
		if !isInternalArtifactDatabase(conf, name) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return conf.DBType
	}
	return conf.Databases[names[0]].Type
}

type configRuntimeReloadPlan struct {
	mode           string
	changedSources []string
	fallback       bool
}

func classifyConfigRuntimeReload(oldCore, newCore core.Config) configRuntimeReloadPlan {
	plan := configRuntimeReloadPlan{mode: "full"}
	changed := changedCatalogSources(oldCore, newCore)
	if len(changed) == 0 {
		return plan
	}
	plan.changedSources = sortedSourceSet(changed)
	plan.fallback = true

	if !oldCore.IsSourcesUsed() || !newCore.IsSourcesUsed() {
		return plan
	}
	if !sourceScopedCatalogPatchAllowed(oldCore, newCore, changed) ||
		!coreConfigChangeScopedToSources(oldCore, newCore, changed) {
		return plan
	}
	if !databaseSourceRuntimePatchAllowed(oldCore, newCore, changed) {
		return plan
	}

	plan.mode = "source_scoped"
	plan.fallback = false
	return plan
}

func databaseSourceRuntimePatchAllowed(oldCore, newCore core.Config, sources map[string]struct{}) bool {
	oldSources := sourceConfigByCatalogName(oldCore.Sources)
	newSources := sourceConfigByCatalogName(newCore.Sources)
	for name := range sources {
		oldSource, hadOldSource := oldSources[name]
		newSource, hasNewSource := newSources[name]
		if !hadOldSource && !hasNewSource {
			return false
		}
		if hadOldSource && oldSource.CanonicalKind() != sourcecap.KindDatabase {
			return false
		}
		if hasNewSource && newSource.CanonicalKind() != sourcecap.KindDatabase {
			return false
		}
		if hasNewSource {
			dbConf, ok := newCore.Databases[name]
			if !ok {
				return false
			}
			if !reflect.DeepEqual(newSourceDatabaseConfig(newSource), dbConf) {
				return false
			}
		}
		if hadOldSource {
			dbConf, ok := oldCore.Databases[name]
			if !ok {
				return false
			}
			if !reflect.DeepEqual(newSourceDatabaseConfig(oldSource), dbConf) {
				return false
			}
		}
	}
	return true
}

func newSourceDatabaseConfig(source core.SourceConfig) core.DatabaseConfig {
	dbConf := core.DatabaseConfig{
		Type:                   source.Type,
		ConnString:             source.ConnString,
		Host:                   source.Host,
		Port:                   source.Port,
		DBName:                 source.DBName,
		User:                   source.User,
		Password:               source.Password,
		Path:                   source.Path,
		MaxOpenConns:           source.MaxOpenConns,
		MaxIdleConns:           source.MaxIdleConns,
		Schema:                 source.Schema,
		PoolSize:               source.PoolSize,
		MaxConnections:         source.MaxConnections,
		MaxConnIdleTime:        source.MaxConnIdleTime,
		MaxConnLifeTime:        source.MaxConnLifeTime,
		PingTimeout:            source.PingTimeout,
		EnableTLS:              source.EnableTLS,
		ServerName:             source.ServerName,
		ServerCert:             source.ServerCert,
		ClientCert:             source.ClientCert,
		ClientKey:              source.ClientKey,
		Encrypt:                source.Encrypt,
		TrustServerCertificate: source.TrustServerCertificate,
		PrivateKeyPath:         source.PrivateKeyPath,
		PrivateKeyPEM:          source.PrivateKeyPEM,
		KeyPassphrase:          source.KeyPassphrase,
		ReadOnly:               source.ReadOnly,
		AnalyticsMode:          source.AnalyticsMode,
		InferDBRefs:            source.InferDBRefs,
	}
	if dbConf.Type == "" {
		dbConf.Type = "postgres"
	}
	return dbConf
}

func (ms *mcpServer) prepareStagedRuntime(stagedCore *core.Config, createIfNotExists bool) (*stagedRuntimeState, error) {
	runtimeCore := cloneCoreConfig(*stagedCore)
	if err := ms.service.hydrateCoreConfigSecrets(&runtimeCore); err != nil {
		return nil, err
	}
	stage := &stagedRuntimeState{
		dbs:            make(map[string]*sql.DB),
		managedDBs:     make(map[string]managedDB),
		runtimeCore:    &runtimeCore,
		newConnections: make(map[string]*sql.DB),
	}

	if len(stagedCore.Databases) > 0 {
		currentDBs := ms.service.dbs
		currentConfigs := ms.service.conf.Core.Databases

		dbNames := make([]string, 0, len(stagedCore.Databases))
		for name := range stagedCore.Databases {
			dbNames = append(dbNames, name)
		}
		sort.Strings(dbNames)

		for _, name := range dbNames {
			dbConf := stagedCore.Databases[name]
			runtimeDBConf := dbConf
			if runtimeCore.Databases != nil {
				if hydrated, ok := runtimeCore.Databases[name]; ok {
					runtimeDBConf = hydrated
				}
			}
			if existing, ok := currentDBs[name]; ok && reflect.DeepEqual(currentConfigs[name], dbConf) {
				stage.dbs[name] = existing
				if managed, ok := ms.service.managedDBs[name]; ok {
					stage.managedDBs[name] = managed
				}
				if ms.service.runtimeCore != nil {
					if runtime, ok := ms.service.runtimeCore.Databases[name]; ok {
						stage.runtimeCore.Databases[name] = runtime
					}
				}
				continue
			}

			if createIfNotExists && !isCodeSQLType(dbConf.Type) {
				dbType := strings.ToLower(runtimeDBConf.Type)
				dbName := runtimeDBConf.DBName
				if dbName == "" {
					dbName = name
				}
				if err := createDatabaseOnServer(dbType, runtimeDBConf.Host, runtimeDBConf.Port, runtimeDBConf.User, runtimeDBConf.Password, dbName, ms.service.log); err != nil {
					ms.service.log.Warnf("create_if_not_exists for '%s': %s", name, redactRuntimeError(err))
				}
			}

			if runtimeDBConf.ConnString == "" && runtimeDBConf.Host == "" && runtimeDBConf.Path == "" {
				continue
			}

			db, err := ms.service.newDBFromDatabaseConfigInto(name, runtimeDBConf, stage.runtimeCore, stage.managedDBs)
			if err != nil {
				return stage, fmt.Errorf("database '%s' connection failed: %s", name, redactRuntimeError(err))
			}
			stage.dbs[name] = db
			stage.newConnections[name] = db
		}

		if len(stage.dbs) == 0 {
			return stage, fmt.Errorf("database connection failed: no connections established")
		}
	} else if len(ms.service.dbs) > 0 {
		for name, db := range ms.service.dbs {
			stage.dbs[name] = db
		}
	} else {
		return stage, nil
	}

	ms.attachStagedManagedArtifactStore(stagedCore, stage)
	if oldMetadataDB := ms.service.metadataDB; oldMetadataDB != "" {
		if _, configured := stagedCore.Databases[oldMetadataDB]; !configured {
			delete(stage.dbs, oldMetadataDB)
		}
	}
	if err := ms.initStagedSystemNano(stagedCore, stage); err != nil {
		return stage, err
	}

	gj, err := core.NewGraphJin(stage.runtimeCore, anyDBFromMap(stage.runtimeCore, stage.dbs), ms.stagedCoreOptions(stagedCore, stage)...)
	if err != nil {
		return stage, err
	}
	stage.gj = gj

	if db := anyDBFromMap(stage.runtimeCore, stage.dbs); db != nil {
		stage.availableDBs, _ = listDatabaseNames(db, primaryDBTypeFromCore(stage.runtimeCore))
		if !ms.service.conf.MCP.DefaultDBAllowed {
			stage.availableDBs = filterSystemDatabases(primaryDBTypeFromCore(stage.runtimeCore), stage.availableDBs)
		}
	}

	if stage.gj != nil && !stage.gj.SchemaReady() {
		stage.schemaNotReady = true
		return stage, fmt.Errorf("database connected but schema discovery found no tables")
	}
	if stage.metadataDB != "" && stagedRuntimeHasApplicationDatabase(stagedCore, stage.managedDBs) {
		snapshot, err := stage.gj.MetadataSnapshot(ms.service.metadataSnapshotExcludesFor(stage.metadataDB, stagedCore, stage.managedDBs)...)
		if err != nil {
			return stage, err
		}
		if len(snapshot.Tables) == 0 {
			stage.schemaNotReady = true
			return stage, fmt.Errorf("database connected but schema discovery found no tables")
		}
	}
	if stage.systemNanoDB != nil {
		scoped := *ms.service
		scopedConf := *ms.service.conf
		scopedConf.Core = *stagedCore
		scoped.conf = &scopedConf
		scoped.gj = stage.gj
		scoped.runtimeCore = stage.runtimeCore
		scoped.metadataDB = stage.metadataDB
		scoped.systemNanoDB = stage.systemNanoDB
		scoped.dbs = stage.dbs
		scoped.managedDBs = stage.managedDBs
		scoped.managedArtifactDB = stage.managedArtifactDB
		if err := scoped.refreshSystemNanoDB(); err != nil {
			return stage, err
		}
	}

	return stage, nil
}

func (ms *mcpServer) prepareSourceScopedRuntime(stagedCore *core.Config, changedSources []string, createIfNotExists bool) (*stagedRuntimeState, error) {
	runtimeCore := cloneCoreConfig(*stagedCore)
	if err := ms.service.hydrateCoreConfigSecrets(&runtimeCore); err != nil {
		return nil, err
	}
	stage := &stagedRuntimeState{
		dbs:            make(map[string]*sql.DB),
		managedDBs:     make(map[string]managedDB),
		runtimeCore:    &runtimeCore,
		newConnections: make(map[string]*sql.DB),
	}
	changed := make(map[string]struct{}, len(changedSources))
	for _, name := range changedSources {
		name = strings.TrimSpace(name)
		if name != "" {
			changed[name] = struct{}{}
		}
	}
	if len(changed) == 0 {
		return stage, fmt.Errorf("source-scoped reload requires at least one changed source")
	}

	currentDBs := ms.service.dbs
	currentConfigs := ms.service.conf.Core.Databases
	dbNames := make([]string, 0, len(stagedCore.Databases))
	for name := range stagedCore.Databases {
		dbNames = append(dbNames, name)
	}
	sort.Strings(dbNames)

	for _, name := range dbNames {
		dbConf := stagedCore.Databases[name]
		runtimeDBConf := dbConf
		if runtimeCore.Databases != nil {
			if hydrated, ok := runtimeCore.Databases[name]; ok {
				runtimeDBConf = hydrated
			}
		}
		_, isChanged := changed[name]
		if existing, ok := currentDBs[name]; ok && !isChanged && reflect.DeepEqual(currentConfigs[name], dbConf) {
			stage.dbs[name] = existing
			if managed, ok := ms.service.managedDBs[name]; ok {
				stage.managedDBs[name] = managed
			}
			if ms.service.runtimeCore != nil {
				if runtime, ok := ms.service.runtimeCore.Databases[name]; ok {
					stage.runtimeCore.Databases[name] = runtime
				}
			}
			continue
		}
		if !isChanged {
			return stage, fmt.Errorf("source-scoped reload encountered unclassified database change: %s", name)
		}

		if createIfNotExists && !isCodeSQLType(dbConf.Type) {
			dbType := strings.ToLower(runtimeDBConf.Type)
			dbName := runtimeDBConf.DBName
			if dbName == "" {
				dbName = name
			}
			if err := createDatabaseOnServer(dbType, runtimeDBConf.Host, runtimeDBConf.Port, runtimeDBConf.User, runtimeDBConf.Password, dbName, ms.service.log); err != nil {
				ms.service.log.Warnf("create_if_not_exists for '%s': %s", name, redactRuntimeError(err))
			}
		}

		if runtimeDBConf.ConnString == "" && runtimeDBConf.Host == "" && runtimeDBConf.Path == "" {
			continue
		}

		db, err := ms.service.newDBFromDatabaseConfigInto(name, runtimeDBConf, stage.runtimeCore, stage.managedDBs)
		if err != nil {
			return stage, fmt.Errorf("database '%s' connection failed: %s", name, redactRuntimeError(err))
		}
		stage.dbs[name] = db
		stage.newConnections[name] = db
	}

	if len(stage.dbs) == 0 && len(stagedCore.Databases) > 0 {
		return stage, fmt.Errorf("database connection failed: no connections established")
	}

	ms.attachStagedManagedArtifactStore(stagedCore, stage)
	if oldMetadataDB := ms.service.metadataDB; oldMetadataDB != "" {
		if _, configured := stagedCore.Databases[oldMetadataDB]; !configured {
			delete(stage.dbs, oldMetadataDB)
		}
	}
	if err := ms.initStagedSystemNano(stagedCore, stage); err != nil {
		return stage, err
	}

	if db := anyDBFromMap(stage.runtimeCore, stage.dbs); db != nil {
		stage.availableDBs, _ = listDatabaseNames(db, primaryDBTypeFromCore(stage.runtimeCore))
		if !ms.service.conf.MCP.DefaultDBAllowed {
			stage.availableDBs = filterSystemDatabases(primaryDBTypeFromCore(stage.runtimeCore), stage.availableDBs)
		}
	}

	return stage, nil
}

func stagedRuntimeHasApplicationDatabase(conf *core.Config, managedDBs map[string]managedDB) bool {
	if conf == nil {
		return false
	}
	for name, dbConf := range conf.Databases {
		if _, ok := managedDBs[name]; ok {
			continue
		}
		if isCodeSQLType(dbConf.Type) {
			continue
		}
		return true
	}
	return false
}

func (ms *mcpServer) recordConfigUpdateRuntimeEvent(ctx context.Context, result ConfigUpdateResult) {
	if ms == nil || ms.service == nil {
		return
	}
	phase := "config"
	switch result.Mode {
	case "preview":
		phase = "config.preview"
	case "apply":
		phase = "config.apply"
	}
	status := runtimeStatusReady
	severity := "info"
	summary := "Guarded config update completed."
	nextAction := "Query gj_runtime again before workflow, config, or schema actions if system state matters."
	errorCode := ""
	if !result.Success {
		status = runtimeStatusFailed
		severity = "warn"
		summary = "Guarded config update did not apply cleanly."
		nextAction = "Review config update errors and retry with a smaller, validated change."
		errorCode = "config_update_failed"
	}
	details := map[string]any{
		"message":        result.Message,
		"change_count":   len(result.Changes),
		"error_count":    len(result.Errors),
		"database_count": len(result.Databases),
	}
	if result.Mode != "" {
		details["mode"] = result.Mode
		details["valid"] = result.Valid
		details["applied"] = result.Applied
	}
	if result.Scope != "" {
		details["scope"] = result.Scope
	}
	if result.ReloadMode != "" {
		details["reload_mode"] = result.ReloadMode
	}
	if result.ReloadStrategy != "" {
		details["reload_strategy"] = result.ReloadStrategy
	}
	if len(result.ChangedSources) != 0 {
		details["changed_sources"] = result.ChangedSources
	}
	if result.ReloadFallback {
		details["reload_fallback"] = true
	}
	if result.CatalogRevision != "" {
		details["catalog_revision"] = result.CatalogRevision
	}
	ms.service.recordRuntimeEvent(ctx, runtimeEvent{
		Phase:      phase,
		Status:     status,
		Severity:   severity,
		Summary:    summary,
		NextAction: nextAction,
		ErrorCode:  errorCode,
		Details:    details,
	})
}

func (ms *mcpServer) commitSourceScopedRuntime(stagedCore core.Config, stage *stagedRuntimeState, plan configRuntimeReloadPlan) error {
	oldDBs := ms.service.dbs
	oldManagedDBs := ms.service.managedDBs
	hadDatabaseMap := len(ms.service.conf.Core.Databases) > 0
	prevLegacyDB := ms.service.conf.DB
	prevDBType := ms.service.conf.DBType

	opts := ms.stagedCoreOptions(&stagedCore, stage)
	if err := ms.service.gj.ReloadConfigDatabases(stage.runtimeCore, plan.changedSources, opts...); err != nil {
		return err
	}
	scoped := *ms.service
	scopedConf := *ms.service.conf
	scopedConf.Core = stagedCore
	scoped.conf = &scopedConf
	scoped.runtimeCore = stage.runtimeCore
	scoped.metadataDB = stage.metadataDB
	scoped.systemNanoDB = stage.systemNanoDB
	scoped.dbs = stage.dbs
	scoped.managedDBs = stage.managedDBs
	scoped.managedArtifactDB = stage.managedArtifactDB
	if err := scoped.refreshSystemNanoDBForSources(plan.changedSources); err != nil {
		return err
	}

	ms.service.runtimeEventsMu.Lock()
	ms.service.conf.Core = stagedCore
	switch {
	case len(stagedCore.Databases) > 0:
		syncRuntimeDBFromDatabases(ms.service.conf, stage.runtimeCore)
	case hadDatabaseMap:
		ms.service.conf.DB = Database{}
		ms.service.conf.DBType = ""
	default:
		ms.service.conf.DB = prevLegacyDB
		ms.service.conf.DBType = prevDBType
	}

	ms.service.dbs = stage.dbs
	ms.service.managedDBs = stage.managedDBs
	ms.service.runtimeCore = stage.runtimeCore
	ms.service.metadataDB = stage.metadataDB
	ms.service.managedArtifactDB = stage.managedArtifactDB
	ms.service.systemNanoDB = stage.systemNanoDB
	ms.service.closeRuntimeEventsLocked()
	if err := ms.service.initRuntimeObservabilityLocked(); err != nil && ms.service.log != nil {
		ms.service.log.Warnf("runtime observability init error: %s", err)
	}
	ms.service.recordRuntimeEventLocked(context.Background(), runtimeEvent{
		Phase:      "config",
		Status:     runtimeStatusReady,
		Severity:   "info",
		Summary:    "GraphJin runtime was source-scoped reloaded after a guarded config update.",
		NextAction: "Query gj_runtime before the next workflow, config, or schema action if system state matters.",
		Details: map[string]any{
			"database_count":  len(stage.dbs),
			"metadata_db":     stage.metadataDB,
			"reload_mode":     plan.mode,
			"changed_sources": plan.changedSources,
			"reload_fallback": plan.fallback,
		},
	})
	ms.service.runtimeEventsMu.Unlock()
	ms.service.registerRuntimeSchemaCallbacks()
	ms.closeSupersededConnections(oldDBs, oldManagedDBs, stage.dbs)
	return nil
}

func (ms *mcpServer) stagedConfigSecurityFindingsJSON(stagedCore *core.Config) string {
	if ms == nil || ms.service == nil || stagedCore == nil {
		return "[]"
	}
	conf := *ms.service.conf
	conf.Core = cloneCoreConfig(*stagedCore)
	temp := &graphjinService{conf: &conf, fs: ms.service.fs}
	now := nowNanoTimestamp()
	reportCtx := securityRuntimeContext(temp, now)
	policies := securityPolicyEvaluationsForContext(reportCtx)
	rows := securityFindingNanoRows(reportCtx, policies, now)
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		severity := strings.ToLower(fmt.Sprint(row["severity"]))
		if severity != "high" && severity != "critical" {
			continue
		}
		finding := map[string]any{}
		for _, key := range []string{"kind", "severity", "source", "source_kind", "table_name", "root", "surface", "capability", "action", "reason", "recommendation"} {
			if value, ok := row[key]; ok && strings.TrimSpace(fmt.Sprint(value)) != "" {
				finding[key] = value
			}
		}
		out = append(out, finding)
		if len(out) >= 20 {
			break
		}
	}
	return jsonStringValue(out)
}

func (ms *mcpServer) commitStagedRuntime(stagedCore core.Config, stage *stagedRuntimeState) {
	oldGJ := ms.service.gj
	oldDBs := ms.service.dbs
	oldManagedDBs := ms.service.managedDBs
	hadDatabaseMap := len(ms.service.conf.Core.Databases) > 0
	prevLegacyDB := ms.service.conf.DB
	prevDBType := ms.service.conf.DBType

	ms.service.runtimeEventsMu.Lock()
	ms.service.conf.Core = stagedCore
	switch {
	case len(stagedCore.Databases) > 0:
		syncRuntimeDBFromDatabases(ms.service.conf, stage.runtimeCore)
	case hadDatabaseMap:
		ms.service.conf.DB = Database{}
		ms.service.conf.DBType = ""
	default:
		ms.service.conf.DB = prevLegacyDB
		ms.service.conf.DBType = prevDBType
	}

	ms.service.dbs = stage.dbs
	ms.service.managedDBs = stage.managedDBs
	ms.service.runtimeCore = stage.runtimeCore
	ms.service.metadataDB = stage.metadataDB
	ms.service.managedArtifactDB = stage.managedArtifactDB
	ms.service.systemNanoDB = stage.systemNanoDB
	ms.service.gj = stage.gj
	ms.service.closeRuntimeEventsLocked()
	if err := ms.service.initRuntimeObservabilityLocked(); err != nil && ms.service.log != nil {
		ms.service.log.Warnf("runtime observability init error: %s", err)
	}
	ms.service.recordRuntimeEventLocked(context.Background(), runtimeEvent{
		Phase:      "config",
		Status:     runtimeStatusReady,
		Severity:   "info",
		Summary:    "GraphJin runtime was reloaded after a guarded config update.",
		NextAction: "Query gj_runtime before the next workflow, config, or schema action if system state matters.",
		Details: map[string]any{
			"database_count": len(stage.dbs),
			"metadata_db":    stage.metadataDB,
			"reload_mode":    "full",
		},
	})
	ms.service.runtimeEventsMu.Unlock()
	ms.service.registerRuntimeSchemaCallbacks()
	if oldGJ != nil && oldGJ != stage.gj {
		oldGJ.Close()
	}
	ms.closeSupersededConnections(oldDBs, oldManagedDBs, stage.dbs)
}

func (ms *mcpServer) closeSupersededConnections(oldDBs map[string]*sql.DB, oldManagedDBs map[string]managedDB, newDBs map[string]*sql.DB) {
	closedManaged := make(map[string]struct{})
	for name, managed := range oldManagedDBs {
		if managed.handle == nil {
			continue
		}
		if newDBs[name] == managed.handle.DB {
			continue
		}
		managed.handle.Close() //nolint:errcheck
		closedManaged[name] = struct{}{}
		ms.service.log.Infof("Closed replaced managed codesql database: %s", name)
	}
	for name, db := range oldDBs {
		if db == nil {
			continue
		}
		if _, ok := closedManaged[name]; ok {
			continue
		}
		if newDBs[name] == db || sqlDBMapContains(newDBs, db) {
			continue
		}
		db.Close() //nolint:errcheck
		ms.service.log.Infof("Closed replaced database connection: %s", name)
	}
}

func sqlDBMapContains(databases map[string]*sql.DB, target *sql.DB) bool {
	for _, db := range databases {
		if db == target {
			return true
		}
	}
	return false
}

// syncDBFromDatabases copies the first entry from conf.Core.Databases
// into conf.DB so that newDBOnce/newDB can use it (they read from conf.DB)
func syncDBFromDatabases(conf *Config) bool {
	if len(conf.Core.Databases) == 0 {
		return false
	}

	// Use the first entry (sorted for deterministic behavior)
	names := make([]string, 0, len(conf.Core.Databases))
	for name := range conf.Core.Databases {
		if !isInternalArtifactDatabase(&conf.Core, name) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return false
	}
	dbConf := conf.Core.Databases[names[0]]

	conf.DB.Type = dbConf.Type
	conf.DB.Host = dbConf.Host
	if dbConf.Port > 0 {
		conf.DB.Port = uint16(dbConf.Port)
	}
	conf.DB.DBName = dbConf.DBName
	conf.DB.User = dbConf.User
	conf.DB.Password = dbConf.Password
	conf.DB.Schema = dbConf.Schema
	conf.DB.Path = dbConf.Path
	conf.DB.ConnString = dbConf.ConnString

	// Connection pool settings
	if dbConf.PoolSize > 0 {
		conf.DB.PoolSize = dbConf.PoolSize
	}
	if dbConf.MaxConnections > 0 {
		conf.DB.MaxConnections = dbConf.MaxConnections
	}
	conf.DB.MaxConnIdleTime = dbConf.MaxConnIdleTime
	conf.DB.MaxConnLifeTime = dbConf.MaxConnLifeTime
	conf.DB.PingTimeout = dbConf.PingTimeout

	// TLS settings
	conf.DB.EnableTLS = dbConf.EnableTLS
	conf.DB.ServerName = dbConf.ServerName
	conf.DB.ServerCert = dbConf.ServerCert
	conf.DB.ClientCert = dbConf.ClientCert
	conf.DB.ClientKey = dbConf.ClientKey

	conf.DB.Encrypt = dbConf.Encrypt
	conf.DB.TrustServerCertificate = dbConf.TrustServerCertificate

	// Snowflake key pair auth
	conf.DB.PrivateKeyPath = dbConf.PrivateKeyPath
	conf.DB.PrivateKeyPEM = dbConf.PrivateKeyPEM
	conf.DB.KeyPassphrase = dbConf.KeyPassphrase

	conf.DBType = dbConf.Type
	return true
}

func syncRuntimeDBFromDatabases(conf *Config, runtimeCore *core.Config) bool {
	if runtimeCore == nil {
		return syncDBFromDatabases(conf)
	}
	tmp := &Config{Core: *runtimeCore}
	if !syncDBFromDatabases(tmp) {
		return false
	}
	conf.DB = tmp.DB
	conf.DBType = tmp.DBType
	return true
}

// ensureDBConnections creates connections for all configured databases that
// don't already have a live connection, and removes connections for databases
// that are no longer in the config.
func (ms *mcpServer) ensureDBConnections() {
	s := ms.service
	conf := &s.conf.Core
	if s.dbs == nil {
		s.dbs = make(map[string]*sql.DB)
	}
	if s.runtimeCore == nil {
		runtimeCore := cloneCoreConfig(*conf)
		if err := s.hydrateCoreConfigSecrets(&runtimeCore); err != nil {
			s.log.Warnf("Failed to hydrate encrypted config secrets: %s", redactRuntimeError(err))
			return
		}
		s.runtimeCore = &runtimeCore
	}

	// Remove connections for databases no longer in config
	for name, db := range s.dbs {
		if _, exists := conf.Databases[name]; !exists {
			if managed, ok := s.managedDBs[name]; ok && managed.handle != nil {
				managed.handle.Close() //nolint:errcheck
				delete(s.managedDBs, name)
			} else {
				db.Close() //nolint:errcheck
			}
			delete(s.dbs, name)
			s.log.Infof("Closed removed database connection: %s", name)
		}
	}

	// Create connections for databases that don't have one yet (sorted for deterministic order)
	dbConfNames := make([]string, 0, len(conf.Databases))
	for name := range conf.Databases {
		dbConfNames = append(dbConfNames, name)
	}
	sort.Strings(dbConfNames)
	for _, name := range dbConfNames {
		dbConf := conf.Databases[name]
		runtimeDBConf := dbConf
		if s.runtimeCore != nil && s.runtimeCore.Databases != nil {
			if hydrated, ok := s.runtimeCore.Databases[name]; ok {
				runtimeDBConf = hydrated
			}
		}
		if _, exists := s.dbs[name]; exists {
			continue // already connected
		}
		// Skip entries without connection info
		if runtimeDBConf.ConnString == "" && runtimeDBConf.Host == "" && runtimeDBConf.Path == "" {
			continue
		}
		db, err := s.newDBFromDatabaseConfigInto(name, runtimeDBConf, s.runtimeCore, s.managedDBs)
		if err != nil {
			s.log.Warnf("Database '%s' connection failed: %s", name, redactRuntimeError(err))
			continue
		}
		s.dbs[name] = db
		s.log.Infof("Connected to database: %s", name)
	}

	// Sync legacy conf.DB from first database
	if len(s.dbs) > 0 {
		syncRuntimeDBFromDatabases(s.conf, s.runtimeCore)
	}
}

// tryInitializeGraphJin attempts to connect to the database and initialize GraphJin core.
// This is called from the MCP handler when gj == nil (no DB was available at startup).
// Returns a list of databases found on the server (even on failure) alongside the error.
func (ms *mcpServer) tryInitializeGraphJin(createIfNotExists bool) ([]string, error) {
	s := ms.service

	if len(s.conf.Core.Databases) == 0 {
		return nil, fmt.Errorf("no database configuration found in databases map")
	}
	if s.runtimeCore == nil {
		runtimeCore := cloneCoreConfig(s.conf.Core)
		if err := s.hydrateCoreConfigSecrets(&runtimeCore); err != nil {
			return nil, err
		}
		s.runtimeCore = &runtimeCore
	}

	// Create the database on the server if requested
	if createIfNotExists && !isCodeSQLType(primaryDBTypeFromCore(&s.conf.Core)) {
		syncDBFromDatabases(s.conf)
		if err := createDatabaseIfNotExists(s.conf, s.log); err != nil {
			s.log.Warnf("create_if_not_exists: %s", redactRuntimeError(err))
			// Don't fail hard — the DB may already exist
		}
	}

	// Create connections for all configured databases
	ms.ensureDBConnections()
	if len(s.dbs) == 0 {
		return nil, fmt.Errorf("database connection failed: no connections established")
	}

	// Initialize GraphJin core
	if err := s.normalStart(); err != nil {
		// Clean up on failure
		closedManaged := s.closeManagedDBs(nil)
		for name, db := range s.dbs {
			if _, ok := closedManaged[name]; ok {
				delete(s.dbs, name)
				continue
			}
			db.Close() //nolint:errcheck
			delete(s.dbs, name)
		}
		s.gj = nil
		return nil, fmt.Errorf("GraphJin initialization failed: %w", err)
	}

	// Verify schema is ready before returning success
	if s.gj == nil || !s.gj.SchemaReady() {
		// Query available databases before cleanup
		var dbNames []string
		if db := s.anyDB(); db != nil {
			dbNames, _ = listDatabaseNames(db, s.conf.DBType)
			if !ms.service.conf.MCP.DefaultDBAllowed {
				dbNames = filterSystemDatabases(s.conf.DBType, dbNames)
			}
		}
		// Clean up so next call retries from scratch
		s.gj = nil
		closedManaged := s.closeManagedDBs(nil)
		for name, db := range s.dbs {
			if _, ok := closedManaged[name]; ok {
				delete(s.dbs, name)
				continue
			}
			db.Close() //nolint:errcheck
			delete(s.dbs, name)
		}
		return dbNames, fmt.Errorf("database connected but schema discovery found no tables — try a different database from the returned databases list, or create tables first")
	}

	// On success, also list databases for the response
	var dbNames []string
	if db := s.anyDB(); db != nil {
		dbNames, _ = listDatabaseNames(db, s.conf.DBType)
		if !ms.service.conf.MCP.DefaultDBAllowed {
			dbNames = filterSystemDatabases(s.conf.DBType, dbNames)
		}
	}

	s.log.Info("GraphJin initialized via MCP configuration")
	return dbNames, nil
}

// saveConfigToDisk persists the current configuration to the config file
func (ms *mcpServer) saveConfigToDisk() error {
	v := ms.service.conf.viper
	if v == nil {
		return fmt.Errorf("viper instance not available")
	}

	// Sync current config state to viper
	ms.syncConfigToViper(v)

	// In sources mode the viper tree still carries defaulted legacy keys
	// (database/metadata/catalog/filesystems/openapi) that were never on
	// disk. WriteConfig would serialize them and the next reload rejects
	// them via validateIsSourcesUsed. Write a sanitized copy instead.
	if ms.service.conf.Core.IsSourcesUsed() {
		settings := v.AllSettings()
		for _, k := range []string{"database", "databases", "metadata", "catalog", "filesystems", "openapi", "openapi_specs_dir"} {
			delete(settings, k)
		}
		if len(ms.service.conf.Core.System.Capabilities) == 0 && len(ms.service.conf.Core.System.RootAccess) == 0 {
			delete(settings, "system")
		}
		if strings.TrimSpace(ms.service.conf.Core.Workflows.Path) == "" && len(ms.service.conf.Core.Workflows.Capabilities) == 0 {
			delete(settings, "workflows")
		}
		nv := viper.New()
		nv.SetConfigFile(v.ConfigFileUsed())
		if err := nv.MergeConfigMap(settings); err != nil {
			return fmt.Errorf("failed to stage sanitized config: %w", err)
		}
		if err := nv.WriteConfig(); err != nil {
			return fmt.Errorf("failed to write config: %w", err)
		}
		return nil
	}

	// Write the config file
	if err := v.WriteConfig(); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

// syncConfigToViper updates viper with the current config values for sections that can be modified.
// Only sets values that are non-nil to avoid polluting viper with empty entries.
func (ms *mcpServer) syncConfigToViper(v *viper.Viper) {
	conf := &ms.service.conf.Core

	if conf.IsSourcesUsed() {
		if conf.Sources != nil {
			v.Set("sources", conf.Sources)
		}
		if conf.Relationships != nil {
			v.Set("relationships", conf.Relationships)
		}
		v.Set("system", conf.System)
		v.Set("workflows", conf.Workflows)
	} else if conf.Databases != nil {
		v.Set("databases", conf.Databases)
	}
	if !conf.IsSourcesUsed() {
		v.Set("metadata", conf.Metadata)
	}
	if conf.Tables != nil {
		v.Set("tables", conf.Tables)
	}
	if conf.Roles != nil {
		v.Set("roles", conf.Roles)
	}
	if conf.Blocklist != nil {
		v.Set("blocklist", conf.Blocklist)
	}
	if conf.Functions != nil {
		v.Set("functions", conf.Functions)
	}
	if conf.Resolvers != nil {
		v.Set("resolvers", conf.Resolvers)
	}
	v.Set("mcp", ms.service.conf.MCP)
}

// setServPatchViper stages only the serv keys named in a serv patch into viper,
// reading their new values from conf. Keeping it patch-scoped avoids rewriting
// unrelated serv defaults into the config file on save.
func setServPatchViper(v *viper.Viper, conf *Config, patch map[string]any) {
	if v == nil || conf == nil {
		return
	}
	for key := range patch {
		switch key {
		case "agent":
			v.Set("agent", conf.Serv.Agent)
		case "log_level":
			v.Set("log_level", conf.Serv.LogLevel)
		case "log_format":
			v.Set("log_format", conf.Serv.LogFormat)
		case "web_ui":
			v.Set("web_ui", conf.Serv.WebUI)
		case "http_compress":
			v.Set("http_compress", conf.Serv.HTTPGZip)
		case "server_timing":
			v.Set("server_timing", conf.Serv.ServerTiming)
		case "rate_limiter":
			v.Set("rate_limiter", conf.Serv.RateLimiter)
		}
	}
}
