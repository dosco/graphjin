package serv

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/core/v3/openapi"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/spf13/viper"
)

// registerConfigTools registers the configuration management tools
func (ms *mcpServer) registerConfigTools() {
	// get_current_config - Dev mode only (read-only)
	if !ms.service.conf.Serv.Production {
		ms.srv.AddTool(mcp.NewTool(
			"get_current_config",
			mcp.WithDescription("Get current GraphJin configuration. Returns sources, databases, relationships, tables, roles, blocklist, functions, resolvers, and MCP settings. "+
				"Use this to understand the current configuration before making changes."),
			mcp.WithString("section",
				mcp.Description("Optional section to retrieve: 'sources', 'databases', 'relationships', 'metadata', 'tables', 'roles', 'blocklist', 'functions', 'resolvers', 'mcp', or 'all' (default)"),
			),
		), ms.handleGetCurrentConfig)
	}

	// update_current_config - Only registered when allow_config_updates is true
	if ms.service.conf.MCP.AllowConfigUpdates {
		ms.srv.AddTool(mcp.NewTool(
			"update_current_config",
			mcp.WithDescription("Compatibility tool for the GraphQL control-plane mutation gj_config(id: \"current\", update: ...). Update GraphJin configuration and automatically reload. "+
				"Changes are applied in-memory and take effect immediately. "+
				"Supports sources, databases, relationships, MCP settings, metadata, tables, roles, blocklist, functions, and resolvers. "+
				"System database names (postgres, mysql, information_schema, master, etc.) "+
				"are rejected by default — use a user database name instead. "+
				"Use create_if_not_exists: true to create a new database on the server before connecting (dev mode only). "+
				"Response includes machine-readable next-step guidance in the `next` field. "+
				"WARNING: Changes are lost on restart unless persisted separately. "+
				"Use get_current_config first to understand the current state."),
			mcp.WithObject("databases",
				mcp.Description("Map of database configs to add/update. Key is database name, value is DatabaseConfig with type, host, port, dbname, user, password, read_only, infer_db_refs for CodeSQL, etc. NOTE: read_only cannot be changed from true to false at runtime if it was set in the config file."),
			),
			mcp.WithObject("metadata",
				mcp.Description("Metadata graph config: enabled, database, auto_code_relations, and code_databases. Dev defaults on; production defaults off."),
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
									"database":  map[string]any{"type": "string", "description": "Database/source name for multi-database or system NanoDB tables such as graphjin.gj_security"},
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
		), ms.handleUpdateCurrentConfig)
	}
}

// MCPConfigResponse represents a section of the configuration for MCP
type MCPConfigResponse struct {
	ActiveDatabase string              `json:"active_database,omitempty"`
	Sources        any                 `json:"sources,omitempty"`
	Databases      any                 `json:"databases,omitempty"`
	Relationships  any                 `json:"relationships,omitempty"`
	Metadata       core.MetadataConfig `json:"metadata,omitempty"`
	Tables         any                 `json:"tables,omitempty"`
	Roles          any                 `json:"roles,omitempty"`
	Blocklist      []string            `json:"blocklist,omitempty"`
	Functions      any                 `json:"functions,omitempty"`
	Resolvers      any                 `json:"resolvers,omitempty"`
	MCP            MCPConfig           `json:"mcp,omitempty"`
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
		return mcp.NewToolResultError(fmt.Sprintf("unknown section: %s. Valid sections: sources, databases, relationships, metadata, tables, roles, blocklist, functions, resolvers, mcp, all", section)), nil
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
	Success   bool          `json:"success"`
	Message   string        `json:"message"`
	Changes   []string      `json:"changes,omitempty"`
	Errors    []string      `json:"errors,omitempty"`
	Databases []string      `json:"databases,omitempty"`
	Next      *NextGuidance `json:"next,omitempty"`
}

// handleUpdateCurrentConfig updates the configuration and reloads
func (ms *mcpServer) handleUpdateCurrentConfig(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if ms.service != nil {
		ms.service.configMu.Lock()
		defer ms.service.configMu.Unlock()
	}

	args := req.GetArguments()

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
			}
			result.Next = ms.nextForConfigUpdate(result)
			ms.recordConfigUpdateRuntimeEvent(ctx, result)
			data, _ := mcpMarshalJSON(result, true)
			return mcpToolResultJSONBytes(data), nil
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

	if sources, ok := args["sources"].([]any); ok {
		parsed, err := parseSourceConfigList(sources)
		if err != nil {
			errors = append(errors, fmt.Sprintf("sources: %v", err))
		} else {
			conf.Sources = parsed
			if err := conf.RenormalizeSources(); err != nil {
				errors = append(errors, fmt.Sprintf("sources: %v", err))
			} else {
				changes = append(changes, "updated sources")
			}
		}
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
			}
			result.Next = ms.nextForConfigUpdate(result)
			ms.recordConfigUpdateRuntimeEvent(ctx, result)
			data, _ := mcpMarshalJSON(result, true)
			return mcpToolResultJSONBytes(data), nil
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

	// Process metadata graph config
	if metadata, ok := args["metadata"].(map[string]any); ok && len(metadata) > 0 {
		conf.Metadata = parseMetadataConfig(metadata, conf.Metadata)
		changes = append(changes, "updated metadata graph config")
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
		}
		result.Next = ms.nextForConfigUpdate(result)
		ms.recordConfigUpdateRuntimeEvent(ctx, result)
		data, _ := mcpMarshalJSON(result, true)
		return mcpToolResultJSONBytes(data), nil
	}
	if len(errors) > 0 {
		result := ConfigUpdateResult{
			Success: false,
			Message: "Config validation failed, changes not applied",
			Changes: changes,
			Errors:  errors,
		}
		result.Next = ms.nextForConfigUpdate(result)
		ms.recordConfigUpdateRuntimeEvent(ctx, result)
		data, _ := mcpMarshalJSON(result, true)
		return mcpToolResultJSONBytes(data), nil
	}

	var availableDBs []string
	oldCore := cloneCoreConfig(ms.service.conf.Core)
	var sealedKeystore *localKeystore
	var sealedSecretRefs map[string]struct{}
	coreChanged := !reflect.DeepEqual(stagedCore, ms.service.conf.Core)
	if coreChanged {
		stage, err := ms.prepareStagedRuntime(conf, createIfNotExists)
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
			}
			result.Next = ms.nextForConfigUpdate(result)
			ms.recordConfigUpdateRuntimeEvent(ctx, result)
			data, _ := mcpMarshalJSON(result, true)
			return mcpToolResultJSONBytes(data), nil
		}

		availableDBs = stage.availableDBs
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
				}
				result.Next = ms.nextForConfigUpdate(result)
				ms.recordConfigUpdateRuntimeEvent(ctx, result)
				data, _ := mcpMarshalJSON(result, true)
				return mcpToolResultJSONBytes(data), nil
			}
			if !ks.hasKey() && configContainsSecretRefs(&persistedCore) {
				stage.close()
				result := ConfigUpdateResult{
					Success:   false,
					Message:   "Config secret hydration failed, changes not applied",
					Changes:   changes,
					Errors:    []string{missingLocalKeystoreKeyError(secretRefsInConfig(&persistedCore)).Error()},
					Databases: availableDBs,
				}
				result.Next = ms.nextForConfigUpdate(result)
				ms.recordConfigUpdateRuntimeEvent(ctx, result)
				data, _ := mcpMarshalJSON(result, true)
				return mcpToolResultJSONBytes(data), nil
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
					}
					result.Next = ms.nextForConfigUpdate(result)
					ms.recordConfigUpdateRuntimeEvent(ctx, result)
					data, _ := mcpMarshalJSON(result, true)
					return mcpToolResultJSONBytes(data), nil
				}
				if err := ks.Save(nil); err != nil {
					stage.close()
					result := ConfigUpdateResult{
						Success:   false,
						Message:   "Config secret keystore save failed, changes not applied",
						Changes:   changes,
						Errors:    []string{redactRuntimeError(err)},
						Databases: availableDBs,
					}
					result.Next = ms.nextForConfigUpdate(result)
					ms.recordConfigUpdateRuntimeEvent(ctx, result)
					data, _ := mcpMarshalJSON(result, true)
					return mcpToolResultJSONBytes(data), nil
				}
				sealedKeystore = ks
				sealedSecretRefs = usedRefs
			}
		}
		ms.commitStagedRuntime(persistedCore, stage)
		if err := ms.service.refreshCatalogAfterCoreConfigChange(oldCore, persistedCore, "config mutation"); err != nil {
			errors = append(errors, fmt.Sprintf("catalog refresh error: %s", redactRuntimeError(err)))
		}
		if ms.service.gj != nil && ms.service.gj.SchemaReady() {
			changes = append(changes, "configuration validated and runtime reloaded transactionally")
		}
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
	}

	if len(errors) > 0 {
		result.Message = "Configuration partially updated with some errors"
	}
	result.Next = ms.nextForConfigUpdate(result)
	ms.recordConfigUpdateRuntimeEvent(ctx, result)

	data, err := mcpMarshalJSON(result, true)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal result: %v", err)), nil
	}
	return mcpToolResultJSONBytes(data), nil
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
	dbs            map[string]*sql.DB
	managedDBs     map[string]managedDB
	runtimeCore    *core.Config
	metadataDB     string
	gj             *core.GraphJin
	availableDBs   []string
	newConnections map[string]*sql.DB
	schemaNotReady bool
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
			if src.Sources[i].Capabilities != nil {
				dst.Sources[i].Capabilities = make(map[string]bool, len(src.Sources[i].Capabilities))
				for name, value := range src.Sources[i].Capabilities {
					dst.Sources[i].Capabilities[name] = value
				}
			}
			if src.Sources[i].Specs != nil {
				dst.Sources[i].Specs = make(map[string]openapi.SpecConfig, len(src.Sources[i].Specs))
				for name, spec := range src.Sources[i].Specs {
					dst.Sources[i].Specs[name] = spec
				}
			}
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

func anyDBFromMap(dbs map[string]*sql.DB) *sql.DB {
	names := make([]string, 0, len(dbs))
	for name := range dbs {
		names = append(names, name)
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
		names = append(names, name)
	}
	sort.Strings(names)
	return conf.Databases[names[0]].Type
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

	if oldMetadataDB := ms.service.metadataDB; oldMetadataDB != "" {
		if _, configured := stagedCore.Databases[oldMetadataDB]; !configured {
			delete(stage.dbs, oldMetadataDB)
		}
	}
	metadataDB, err := ms.service.initMetadataGraphForRuntime(stagedCore, stage.runtimeCore, stage.dbs, stage.managedDBs)
	if err != nil {
		return stage, err
	}
	stage.metadataDB = metadataDB
	if metadataDB != "" {
		stage.newConnections[metadataDB] = stage.dbs[metadataDB]
	}

	gj, err := core.NewGraphJin(stage.runtimeCore, anyDBFromMap(stage.dbs), ms.service.buildCoreOptionsFor(stage.dbs, stage.managedDBs)...)
	if err != nil {
		return stage, err
	}
	stage.gj = gj

	if db := anyDBFromMap(stage.dbs); db != nil {
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
	if ms.service.systemNanoDB == nil {
		if err := ms.service.refreshMetadataGraphForRuntime(stage.gj, stagedCore, stage.metadataDB, stage.dbs, stage.managedDBs); err != nil {
			return stage, err
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
	ms.service.recordRuntimeEvent(ctx, runtimeEvent{
		Phase:      "config",
		Status:     status,
		Severity:   severity,
		Summary:    summary,
		NextAction: nextAction,
		ErrorCode:  errorCode,
		Details: map[string]any{
			"message":        result.Message,
			"change_count":   len(result.Changes),
			"error_count":    len(result.Errors),
			"database_count": len(result.Databases),
		},
	})
}

func (ms *mcpServer) commitStagedRuntime(stagedCore core.Config, stage *stagedRuntimeState) {
	oldGJ := ms.service.gj
	oldDBs := ms.service.dbs
	oldManagedDBs := ms.service.managedDBs
	hadDatabaseMap := len(ms.service.conf.Core.Databases) > 0
	prevLegacyDB := ms.service.conf.DB
	prevDBType := ms.service.conf.DBType

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
	ms.service.gj = stage.gj
	ms.service.reinitRuntimeObservability()
	ms.service.recordRuntimeEvent(context.Background(), runtimeEvent{
		Phase:      "config",
		Status:     runtimeStatusReady,
		Severity:   "info",
		Summary:    "GraphJin runtime was reloaded after a guarded config update.",
		NextAction: "Query gj_runtime before the next workflow, config, or schema action if system state matters.",
		Details: map[string]any{
			"database_count": len(stage.dbs),
			"metadata_db":    stage.metadataDB,
		},
	})
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
		if newDBs[name] == db {
			continue
		}
		db.Close() //nolint:errcheck
		ms.service.log.Infof("Closed replaced database connection: %s", name)
	}
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
		names = append(names, name)
	}
	sort.Strings(names)
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
