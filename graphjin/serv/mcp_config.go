package serv

import (
	"context"
	"fmt"
	"strings"

	"github.com/dosco/graphjin/core/v3"
	"github.com/mark3labs/mcp-go/mcp"
)

// registerConfigTools registers the configuration management tools
func (ms *mcpServer) registerConfigTools() {
	// get_current_config - Always available (read-only, safe)
	ms.srv.AddTool(mcp.NewTool(
		"get_current_config",
		mcp.WithDescription("Get current GraphJin configuration. Returns databases, tables, roles, blocklist, and functions. "+
			"Use this to understand the current configuration before making changes."),
		mcp.WithString("section",
			mcp.Description("Optional section to retrieve: 'databases', 'tables', 'roles', 'blocklist', 'functions', or 'all' (default)"),
		),
	), ms.handleGetCurrentConfig)

	// update_current_config - Only registered when allow_config_updates is true
	if ms.service.conf.MCP.AllowConfigUpdates {
		ms.srv.AddTool(mcp.NewTool(
			"update_current_config",
			mcp.WithDescription("Update GraphJin configuration and automatically reload. "+
				"Changes are applied in-memory and take effect immediately. "+
				"WARNING: Changes are lost on restart unless persisted separately. "+
				"Use get_current_config first to understand the current state."),
			mcp.WithObject("databases",
				mcp.Description("Map of database configs to add/update. Key is database name, value is DatabaseConfig with type, host, port, dbname, user, password, etc."),
			),
			mcp.WithArray("tables",
				mcp.Description("Array of table configs to add/update. Each table has name, database (optional), blocklist (optional), columns (optional), order_by (optional)."),
			),
			mcp.WithArray("roles",
				mcp.Description("Array of role configs to add/update. Each role has name, match (optional), and tables array with query/insert/update/delete permissions."),
			),
			mcp.WithArray("blocklist",
				mcp.Description("Array of tables/columns to block globally. Use 'table_name' to block entire table or 'table_name.column_name' to block specific column."),
			),
			mcp.WithArray("functions",
				mcp.Description("Array of database function configs. Each function has name and return_type."),
			),
		), ms.handleUpdateCurrentConfig)
	}
}

// MCPConfigResponse represents a section of the configuration for MCP
type MCPConfigResponse struct {
	Databases map[string]core.DatabaseConfig `json:"databases,omitempty"`
	Tables    []core.Table                   `json:"tables,omitempty"`
	Roles     []RoleInfo                     `json:"roles,omitempty"`
	Blocklist []string                       `json:"blocklist,omitempty"`
	Functions []core.Function                `json:"functions,omitempty"`
}

// RoleInfo provides role information safe for JSON serialization
type RoleInfo struct {
	Name    string          `json:"name"`
	Comment string          `json:"comment,omitempty"`
	Match   string          `json:"match,omitempty"`
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

	switch strings.ToLower(section) {
	case "databases":
		result.Databases = conf.Databases
	case "tables":
		result.Tables = conf.Tables
	case "roles":
		result.Roles = convertRolesToInfo(conf.Roles)
	case "blocklist":
		result.Blocklist = conf.Blocklist
	case "functions":
		result.Functions = conf.Functions
	case "all":
		result.Databases = conf.Databases
		result.Tables = conf.Tables
		result.Roles = convertRolesToInfo(conf.Roles)
		result.Blocklist = conf.Blocklist
		result.Functions = conf.Functions
	default:
		return mcp.NewToolResultError(fmt.Sprintf("unknown section: %s. Valid sections: databases, tables, roles, blocklist, functions, all", section)), nil
	}

	data, err := mcpMarshalJSON(result, true)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal config: %v", err)), nil
	}
	return mcp.NewToolResultText(string(data)), nil
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
	Tables    []TableConfigInput             `json:"tables,omitempty"`
	Roles     []RoleConfigInput              `json:"roles,omitempty"`
	Blocklist []string                       `json:"blocklist,omitempty"`
	Functions []FunctionConfigInput          `json:"functions,omitempty"`
}

// DatabaseConfigInput represents a database config for input
type DatabaseConfigInput struct {
	Type       string `json:"type"`
	Default    bool   `json:"default,omitempty"`
	ConnString string `json:"conn_string,omitempty"`
	Host       string `json:"host,omitempty"`
	Port       int    `json:"port,omitempty"`
	DBName     string `json:"dbname,omitempty"`
	User       string `json:"user,omitempty"`
	Password   string `json:"password,omitempty"`
	Path       string `json:"path,omitempty"`
	Schema     string `json:"schema,omitempty"`
}

// TableConfigInput represents a table config for input
type TableConfigInput struct {
	Name      string              `json:"name"`
	Database  string              `json:"database,omitempty"`
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
	Name    string                `json:"name"`
	Comment string                `json:"comment,omitempty"`
	Match   string                `json:"match,omitempty"`
	Tables  []RoleTableConfigInput `json:"tables,omitempty"`
}

// RoleTableConfigInput represents a role table config for input
type RoleTableConfigInput struct {
	Name     string             `json:"name"`
	Schema   string             `json:"schema,omitempty"`
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

// ConfigUpdateResult represents the result of a config update
type ConfigUpdateResult struct {
	Success bool     `json:"success"`
	Message string   `json:"message"`
	Changes []string `json:"changes,omitempty"`
	Errors  []string `json:"errors,omitempty"`
}

// handleUpdateCurrentConfig updates the configuration and reloads
func (ms *mcpServer) handleUpdateCurrentConfig(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()

	var changes []string
	var errors []string

	conf := &ms.service.conf.Core

	// Process databases
	if databases, ok := args["databases"].(map[string]any); ok && len(databases) > 0 {
		if conf.Databases == nil {
			conf.Databases = make(map[string]core.DatabaseConfig)
		}
		for name, dbAny := range databases {
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
			conf.Databases[name] = dbConf
			changes = append(changes, fmt.Sprintf("added/updated database: %s", name))
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

	// If no changes were made, return early
	if len(changes) == 0 && len(errors) == 0 {
		result := ConfigUpdateResult{
			Success: true,
			Message: "No changes provided",
		}
		data, _ := mcpMarshalJSON(result, true)
		return mcp.NewToolResultText(string(data)), nil
	}

	// Attempt to reload with new config
	if len(changes) > 0 {
		if err := ms.service.gj.Reload(); err != nil {
			result := ConfigUpdateResult{
				Success: false,
				Message: fmt.Sprintf("Config updated but reload failed: %v", err),
				Changes: changes,
				Errors:  append(errors, fmt.Sprintf("reload error: %v", err)),
			}
			data, _ := mcpMarshalJSON(result, true)
			return mcp.NewToolResultText(string(data)), nil
		}
	}

	result := ConfigUpdateResult{
		Success: len(errors) == 0,
		Message: "Configuration updated and reloaded successfully",
		Changes: changes,
		Errors:  errors,
	}

	if len(errors) > 0 {
		result.Message = "Configuration partially updated with some errors"
	}

	data, err := mcpMarshalJSON(result, true)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal result: %v", err)), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

// parseDBConfig parses a database config from a map
func parseDBConfig(m map[string]any) (core.DatabaseConfig, error) {
	var conf core.DatabaseConfig

	if t, ok := m["type"].(string); ok {
		conf.Type = t
	}
	if def, ok := m["default"].(bool); ok {
		conf.Default = def
	}
	if cs, ok := m["conn_string"].(string); ok {
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

	// Validate type
	if conf.Type == "" {
		return conf, fmt.Errorf("database type is required")
	}
	validTypes := map[string]bool{
		"postgres": true, "mysql": true, "mariadb": true,
		"sqlite": true, "oracle": true, "mongodb": true,
	}
	if !validTypes[strings.ToLower(conf.Type)] {
		return conf, fmt.Errorf("invalid database type: %s", conf.Type)
	}

	return conf, nil
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
