package serv

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/dosco/graphjin/core/v3"
	"github.com/mark3labs/mcp-go/mcp"
)

const errNoDB = "No databases have been configured yet. " +
	"Use the discover_databases tool to find available databases, " +
	"then update_current_config to set up a connection."

// requireDB checks that GraphJin is initialized with a usable schema.
// Returns an error result if not ready, or nil if ready to proceed.
func (ms *mcpServer) requireDB() *mcp.CallToolResult {
	if ms.service.gj == nil || !ms.service.gj.SchemaReady() {
		return mcp.NewToolResultError(errNoDB)
	}
	return nil
}

// registerSchemaTools registers the schema discovery tools
func (ms *mcpServer) registerSchemaTools() {
	// list_tables - List all database tables
	ms.srv.AddTool(mcp.NewTool(
		"list_tables",
		mcp.WithDescription("List all database tables. Call this FIRST to discover available data. "+
			"Returns table names, types, and column counts. "+
			"Follow up with describe_table for column details and available aggregation functions."),
		mcp.WithString("namespace",
			mcp.Description("Optional namespace for multi-tenant deployments"),
		),
		mcp.WithString("database",
			mcp.Description("Optional database name to filter tables. Omit to see tables from ALL databases."),
		),
	), ms.handleListTables)

	// describe_table - Get detailed table schema with relationships
	ms.srv.AddTool(mcp.NewTool(
		"describe_table",
		mcp.WithDescription("Get detailed schema for a table including columns, types, relationships, "+
			"and available aggregation functions (count, sum, avg, min, max). "+
			"Call this BEFORE writing queries to understand the schema and what aggregations are available."),
		mcp.WithString("table",
			mcp.Required(),
			mcp.Description("Name of the table to describe"),
		),
		mcp.WithString("namespace",
			mcp.Description("Optional namespace for multi-tenant deployments"),
		),
		mcp.WithString("database",
			mcp.Description("Optional database name. Omit to search all databases."),
		),
	), ms.handleDescribeTable)

	// find_path - Find relationship path between tables
	ms.srv.AddTool(mcp.NewTool(
		"find_path",
		mcp.WithDescription("Find relationship path between two tables. Use this when you need to "+
			"join tables that aren't directly related - it shows the join path through intermediate tables "+
			"and generates an example query showing the nesting structure."),
		mcp.WithString("from_table",
			mcp.Required(),
			mcp.Description("Starting table name"),
		),
		mcp.WithString("to_table",
			mcp.Required(),
			mcp.Description("Target table name"),
		),
		mcp.WithString("database",
			mcp.Description("Optional database name. Omit to search all databases."),
		),
	), ms.handleFindPath)

	// validate_where_clause - Validate where clause syntax and type compatibility
	ms.srv.AddTool(mcp.NewTool(
		"validate_where_clause",
		mcp.WithDescription("Validate a where clause for syntax and type compatibility. "+
			"Checks that operators match column types and returns detailed error messages with suggestions. "+
			"Use this to verify your filter logic before including it in a full query."),
		mcp.WithString("table",
			mcp.Required(),
			mcp.Description("Table name to validate against"),
		),
		mcp.WithString("where",
			mcp.Required(),
			mcp.Description("The where clause to validate (e.g., '{ price: { gt: 50 } }')"),
		),
		mcp.WithString("database",
			mcp.Description("Optional database name. Omit to search all databases."),
		),
	), ms.handleValidateWhereClause)

	// get_workflow_guide - Returns recommended workflow for using MCP tools
	ms.srv.AddTool(mcp.NewTool(
		"get_workflow_guide",
		mcp.WithDescription("Get the recommended workflow for using GraphJin MCP tools effectively. "+
			"Call this if you're unsure about the right sequence of tool calls for queries or mutations."),
	), ms.handleGetWorkflowGuide)

	// reload_schema - Only registered when allow_schema_reload is true
	if ms.service.conf.MCP.AllowSchemaReload {
		ms.srv.AddTool(mcp.NewTool(
			"reload_schema",
			mcp.WithDescription("Reload the database schema to discover new or modified tables. "+
				"Use this tool when: (1) the user says a table exists but list_tables doesn't show it, "+
				"(2) the user has just created new tables or modified the database structure, "+
				"(3) the user explicitly asks to reload, refresh, or recheck the database schema. "+
				"This triggers immediate discovery without waiting for the automatic polling interval."),
		), ms.handleReloadSchema)
	}
}

// handleListTables returns all available tables
func (ms *mcpServer) handleListTables(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := ms.requireDB(); err != nil {
		return err, nil
	}
	args := req.GetArguments()
	database, _ := args["database"].(string)

	var tables []core.TableInfo
	if database != "" {
		tables = ms.service.gj.GetTablesForDatabase(database)
	} else {
		tables = ms.service.gj.GetTables()
	}

	result := struct {
		Tables []core.TableInfo `json:"tables"`
		Count  int              `json:"count"`
	}{
		Tables: tables,
		Count:  len(tables),
	}

	data, err := mcpMarshalJSON(result, true)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

// AggregationInfo describes available aggregation functions for a table
type AggregationInfo struct {
	Available []string `json:"available"`
	Usage     string   `json:"usage"`
}

// TableSchemaWithAggregations extends TableSchema with aggregation information
type TableSchemaWithAggregations struct {
	*core.TableSchema
	Aggregations   AggregationInfo `json:"aggregations"`
	ExampleQueries []ExampleQuery  `json:"example_queries,omitempty"`
}

// ExampleQuery represents an example GraphQL query for a table
type ExampleQuery struct {
	Description string `json:"description"`
	Query       string `json:"query"`
}

// handleDescribeTable returns detailed schema for a table including aggregations
func (ms *mcpServer) handleDescribeTable(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := ms.requireDB(); err != nil {
		return err, nil
	}
	args := req.GetArguments()
	table, _ := args["table"].(string)
	database, _ := args["database"].(string)

	if table == "" {
		return mcp.NewToolResultError("table name is required"), nil
	}

	var schema *core.TableSchema
	var err error
	if database != "" {
		schema, err = ms.service.gj.GetTableSchemaForDatabase(database, table)
	} else {
		schema, err = ms.service.gj.GetTableSchema(table)
	}
	if err != nil {
		return mcp.NewToolResultError(enhanceError(err.Error(), "describe_table")), nil
	}

	// Generate available aggregations based on column types
	aggregations := generateAggregations(schema)

	// Generate example queries
	examples := generateExampleQueries(schema)

	result := TableSchemaWithAggregations{
		TableSchema:    schema,
		Aggregations:   aggregations,
		ExampleQueries: examples,
	}

	data, err := mcpMarshalJSON(result, true)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

// generateAggregations creates the list of available aggregation functions based on column types
func generateAggregations(schema *core.TableSchema) AggregationInfo {
	var available []string

	for _, col := range schema.Columns {
		// All columns support count
		available = append(available, fmt.Sprintf("count_%s", col.Name))

		// Numeric columns support sum, avg, min, max
		normalizedType := normalizeColumnType(col.Type)
		if normalizedType == "numeric" {
			available = append(available,
				fmt.Sprintf("sum_%s", col.Name),
				fmt.Sprintf("avg_%s", col.Name),
				fmt.Sprintf("min_%s", col.Name),
				fmt.Sprintf("max_%s", col.Name),
			)
		}
	}

	return AggregationInfo{
		Available: available,
		Usage:     fmt.Sprintf("{ %s { count_id sum_<numeric_col> avg_<numeric_col> } }", schema.Name),
	}
}

// generateExampleQueries creates example GraphQL queries for a table
func generateExampleQueries(schema *core.TableSchema) []ExampleQuery {
	var examples []ExampleQuery
	name := schema.Name

	// Collect column names for the basic query (up to 5)
	var colNames []string
	for _, col := range schema.Columns {
		colNames = append(colNames, col.Name)
		if len(colNames) >= 5 {
			break
		}
	}
	colList := "id"
	if len(colNames) > 0 {
		colList = strings.Join(colNames, " ")
	}

	// 1. Basic fetch
	examples = append(examples, ExampleQuery{
		Description: fmt.Sprintf("Fetch %s with limit", name),
		Query:       fmt.Sprintf("{ %s(limit: 10) { %s } }", name, colList),
	})

	// 2. Relationship join (if any relationships exist)
	allRels := append(schema.Relationships.Outgoing, schema.Relationships.Incoming...)
	if len(allRels) > 0 {
		rel := allRels[0]
		examples = append(examples, ExampleQuery{
			Description: fmt.Sprintf("Fetch %s with related %s", name, rel.Table),
			Query:       fmt.Sprintf("{ %s(limit: 10) { %s %s { id } } }", name, colList, rel.Table),
		})
	}

	// 3. Aggregation (if numeric columns exist)
	for _, col := range schema.Columns {
		normalizedType := normalizeColumnType(col.Type)
		if normalizedType == "numeric" {
			examples = append(examples, ExampleQuery{
				Description: fmt.Sprintf("Aggregate %s statistics", name),
				Query:       fmt.Sprintf("{ %s { count_id sum_%s avg_%s } }", name, col.Name, col.Name),
			})
			break
		}
	}

	return examples
}

// handleFindPath finds the relationship path between two tables
func (ms *mcpServer) handleFindPath(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := ms.requireDB(); err != nil {
		return err, nil
	}
	args := req.GetArguments()
	fromTable, _ := args["from_table"].(string)
	toTable, _ := args["to_table"].(string)
	database, _ := args["database"].(string)

	if fromTable == "" || toTable == "" {
		return mcp.NewToolResultError("both from_table and to_table are required"), nil
	}

	var path []core.PathStep
	var err error
	if database != "" {
		path, err = ms.service.gj.FindRelationshipPathForDatabase(database, fromTable, toTable)
	} else {
		path, err = ms.service.gj.FindRelationshipPath(fromTable, toTable)
	}
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	// Generate an example query
	exampleQuery := generatePathExampleQuery(fromTable, toTable, path)

	result := struct {
		Path         []core.PathStep `json:"path"`
		ExampleQuery string          `json:"example_query"`
	}{
		Path:         path,
		ExampleQuery: exampleQuery,
	}

	data, err := mcpMarshalJSON(result, true)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

// generatePathExampleQuery generates an example GraphQL query based on the path
func generatePathExampleQuery(from, to string, path []core.PathStep) string {
	if len(path) == 0 {
		return ""
	}

	// Simple nested query structure
	query := "{ " + from + " { id "
	for _, step := range path {
		query += step.To + " { id "
	}

	// Close all the braces
	for range path {
		query += "} "
	}
	query += "} }"

	return query
}

// getNamespace returns the configured namespace
func (ms *mcpServer) getNamespace() string {
	if ms.service.namespace != nil {
		return *ms.service.namespace
	}
	return ""
}

// WorkflowGuide contains the recommended workflow for using GraphJin MCP tools
type WorkflowGuide struct {
	QueryWorkflow    []string          `json:"query_workflow"`
	MutationWorkflow []string          `json:"mutation_workflow"`
	Tips             []string          `json:"tips"`
	ToolSequences    map[string]string `json:"tool_sequences"`
}

// handleGetWorkflowGuide returns the recommended workflow for MCP tool usage
func (ms *mcpServer) handleGetWorkflowGuide(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	guide := WorkflowGuide{
		QueryWorkflow: []string{
			"1. Call get_query_syntax to learn GraphJin DSL (it differs from standard GraphQL)",
			"1.5 If no database is configured, use plan_database_setup → test_database_connection → apply_database_setup",
			"2. Call list_tables to see available data",
			"3. Call describe_table for schema details + available aggregation functions",
			"4. Check list_saved_queries - a saved query may already exist for your need",
			"5. Call execute_graphql (or execute_saved_query if one exists)",
			"6. For pagination, save cursor IDs from response for next page requests",
		},
		MutationWorkflow: []string{
			"1. Call get_mutation_syntax to learn insert/update/upsert/delete syntax",
			"2. Call describe_table to understand required columns and types",
			"3. Check list_saved_queries for existing mutation queries",
			"4. Call execute_graphql with the mutation",
		},
		Tips: []string{
			"PREFER execute_saved_query over execute_graphql when a matching saved query exists",
			"Use plan_database_setup for ranked discover results and explicit candidate selection",
			"Use test_database_connection before apply_database_setup when credentials are uncertain",
			"Use find_path when joining tables that aren't directly related",
			"Aggregations like count_id, sum_price are available on all tables (see describe_table)",
			"Use the write_where_clause prompt for help building complex filters",
			"Use @object directive when you expect a single result: { user @object { id } }",
			"Use resolvers to join DB tables with remote APIs - configure via update_current_config with resolvers parameter",
			"For multi-database deployments, use the `database` parameter in list_tables and describe_table to filter by database. Omitting it returns results from all databases.",
			"Use explain_query to see the exact compiled query that will run before executing — great for debugging and optimization",
			"Use explore_relationships to map out the data model neighborhood around any table",
			"Use audit_role_permissions to understand what each role can access",
		},
		ToolSequences: map[string]string{
			"db_onboarding_guided":       "discover_databases → plan_database_setup → test_database_connection → apply_database_setup → list_tables",
			"simple_query":               "get_query_syntax → list_tables → describe_table → execute_graphql",
			"complex_query":              "get_query_syntax → list_tables → describe_table → find_path → execute_graphql",
			"use_saved_query":            "list_saved_queries → get_saved_query → execute_saved_query",
			"mutation":                   "get_mutation_syntax → describe_table → execute_graphql",
			"explore_schema":             "list_tables → describe_table (for each relevant table) → find_path",
			"build_where_clause":         "describe_table → use write_where_clause prompt → validate_where_clause",
			"configure_resolver":         "get_current_config(section: resolvers) → update_current_config(resolvers: [...]) → reload_schema → execute_graphql",
			"multi_database_exploration": "list_tables → describe_table(database: 'db_name') → execute_graphql",
			"debug_query":                "explain_query → (fix issues) → execute_graphql",
			"explore_data_model":         "list_tables → explore_relationships(depth: 2) → describe_table",
			"security_audit":             "audit_role_permissions(role: 'all') → update_current_config(roles: [...]) → audit_role_permissions (verify)",
		},
	}

	data, err := mcpMarshalJSON(guide, true)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

// handleReloadSchema triggers a schema reload
func (ms *mcpServer) handleReloadSchema(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := ms.requireDB(); err != nil {
		return err, nil
	}
	err := ms.service.gj.Reload()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to reload schema: %s", err.Error())), nil
	}

	// Get updated table list to confirm
	tables := ms.service.gj.GetTables()

	result := struct {
		Success    bool     `json:"success"`
		Message    string   `json:"message"`
		TableCount int      `json:"table_count"`
		Tables     []string `json:"tables,omitempty"`
	}{
		Success:    true,
		Message:    "Schema reloaded successfully",
		TableCount: len(tables),
	}

	// Include table names if not too many
	if len(tables) <= 20 {
		for _, t := range tables {
			result.Tables = append(result.Tables, t.Name)
		}
	}

	data, err := mcpMarshalJSON(result, true)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	return mcp.NewToolResultText(string(data)), nil
}

// EnhancedError represents an error with recovery suggestions
type EnhancedError struct {
	Message     string `json:"message"`
	Suggestion  string `json:"suggestion,omitempty"`
	RelatedTool string `json:"related_tool,omitempty"`
}

// enhanceError adds helpful suggestions to common error messages
func enhanceError(errMsg, currentTool string) string {
	enhanced := EnhancedError{Message: errMsg}

	// Pattern matching for common errors
	switch {
	case contains(errMsg, "table not found", "unknown table", "does not exist", "no such table", "table doesn't exist"):
		enhanced.Suggestion = "Check spelling or use list_tables to see available tables. The table may exist in a different database - use list_tables to see all databases."
		enhanced.RelatedTool = "list_tables"
	case contains(errMsg, "column not found", "unknown column", "column does not exist", "no such column", "unknown field"):
		enhanced.Suggestion = "Check spelling or use describe_table to see available columns"
		enhanced.RelatedTool = "describe_table"
	case contains(errMsg, "invalid operator", "unknown operator", "unsupported operator"):
		enhanced.Suggestion = "Use get_query_syntax to see valid operators for each type"
		enhanced.RelatedTool = "get_query_syntax"
	case contains(errMsg, "syntax error", "parse error", "unexpected"):
		enhanced.Suggestion = "Check get_query_syntax for correct syntax"
		enhanced.RelatedTool = "get_query_syntax"
	case contains(errMsg, "permission", "access denied", "not allowed"):
		enhanced.Suggestion = "Check if mutations are enabled in config or if the operation requires authentication"
		enhanced.RelatedTool = ""
	default:
		// No enhancement for unrecognized errors
		return errMsg
	}

	data, err := json.Marshal(enhanced)
	if err != nil {
		return errMsg
	}
	return string(data)
}

// contains checks if the message contains any of the substrings (case-insensitive)
func contains(msg string, substrs ...string) bool {
	msgLower := stringToLower(msg)
	for _, s := range substrs {
		if stringContains(msgLower, stringToLower(s)) {
			return true
		}
	}
	return false
}

// stringToLower converts a string to lowercase
func stringToLower(s string) string {
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		result[i] = c
	}
	return string(result)
}

// stringContains checks if s contains substr
func stringContains(s, substr string) bool {
	return len(substr) <= len(s) && (s == substr || len(substr) == 0 ||
		(len(substr) <= len(s) && findSubstring(s, substr) >= 0))
}

// findSubstring finds the index of substr in s, returns -1 if not found
func findSubstring(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// WhereValidationResult represents the result of where clause validation
type WhereValidationResult struct {
	Valid      bool                      `json:"valid"`
	Errors     []WhereValidationError    `json:"errors,omitempty"`
	ColumnInfo map[string]ColumnTypeInfo `json:"column_info,omitempty"`
}

// WhereValidationError represents a single validation error
type WhereValidationError struct {
	Path       string `json:"path"`
	Error      string `json:"error"`
	Message    string `json:"message"`
	ColumnType string `json:"column_type,omitempty"`
	Suggestion string `json:"suggestion,omitempty"`
}

// ColumnTypeInfo provides information about a column's type and valid operators
type ColumnTypeInfo struct {
	Type           string   `json:"type"`
	ValidOperators []string `json:"valid_operators"`
}

// handleValidateWhereClause validates a where clause for syntax and type compatibility
func (ms *mcpServer) handleValidateWhereClause(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := ms.requireDB(); err != nil {
		return err, nil
	}
	args := req.GetArguments()
	table, _ := args["table"].(string)
	whereClause, _ := args["where"].(string)
	database, _ := args["database"].(string)

	if table == "" {
		return mcp.NewToolResultError("table name is required"), nil
	}
	if whereClause == "" {
		return mcp.NewToolResultError("where clause is required"), nil
	}

	// Get table schema
	var schema *core.TableSchema
	var err error
	if database != "" {
		schema, err = ms.service.gj.GetTableSchemaForDatabase(database, table)
	} else {
		schema, err = ms.service.gj.GetTableSchema(table)
	}
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to get schema for table '%s': %v", table, err)), nil
	}

	// Build column info map
	columnTypes := make(map[string]core.ColumnInfo)
	for _, col := range schema.Columns {
		columnTypes[col.Name] = col
	}

	// Parse the where clause as JSON
	var whereData map[string]any
	if err := json.Unmarshal([]byte(whereClause), &whereData); err != nil {
		// Return parse error
		result := WhereValidationResult{
			Valid: false,
			Errors: []WhereValidationError{
				{
					Path:       "",
					Error:      "parse_error",
					Message:    fmt.Sprintf("Failed to parse where clause as JSON: %v", err),
					Suggestion: "Ensure the where clause is valid JSON, e.g., { \"price\": { \"gt\": 50 } }",
				},
			},
		}
		data, _ := mcpMarshalJSON(result, true)
		return mcp.NewToolResultText(string(data)), nil
	}

	// Validate the where clause
	errors := validateWhereClause(whereData, columnTypes, "")

	// Build column info for response
	columnInfo := make(map[string]ColumnTypeInfo)
	for name, col := range columnTypes {
		columnInfo[name] = ColumnTypeInfo{
			Type:           col.Type,
			ValidOperators: getValidOperators(col.Type, col.Array),
		}
	}

	result := WhereValidationResult{
		Valid:      len(errors) == 0,
		Errors:     errors,
		ColumnInfo: columnInfo,
	}

	data, err := mcpMarshalJSON(result, true)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

// validateWhereClause recursively validates a where clause structure
func validateWhereClause(where map[string]any, columnTypes map[string]core.ColumnInfo, path string) []WhereValidationError {
	var errors []WhereValidationError

	// Logical operators
	logicalOps := map[string]bool{"and": true, "or": true, "not": true}

	whereKeys := make([]string, 0, len(where))
	for key := range where {
		whereKeys = append(whereKeys, key)
	}
	sort.Strings(whereKeys)
	for _, key := range whereKeys {
		value := where[key]
		currentPath := key
		if path != "" {
			currentPath = path + "." + key
		}

		// Handle logical operators
		if logicalOps[key] {
			switch v := value.(type) {
			case []any:
				// and/or with array of conditions
				for i, item := range v {
					if itemMap, ok := item.(map[string]any); ok {
						errors = append(errors, validateWhereClause(itemMap, columnTypes, fmt.Sprintf("%s[%d]", currentPath, i))...)
					}
				}
			case map[string]any:
				// not with single condition, or or with object
				errors = append(errors, validateWhereClause(v, columnTypes, currentPath)...)
			}
			continue
		}

		// Handle column conditions
		col, colExists := columnTypes[key]
		if !colExists {
			// Check if this might be a nested relationship
			// We'll skip validation for potential relationship filters
			if valueMap, ok := value.(map[string]any); ok {
				// Check if any key looks like an operator
				hasOperator := false
				for k := range valueMap {
					if isOperator(k) {
						hasOperator = true
						break
					}
				}
				if !hasOperator {
					// Likely a relationship filter, skip
					continue
				}
			}

			errors = append(errors, WhereValidationError{
				Path:       currentPath,
				Error:      "unknown_column",
				Message:    fmt.Sprintf("Column '%s' does not exist in table", key),
				Suggestion: "Check column name spelling or use describe_table to see available columns",
			})
			continue
		}

		// Validate operator and value type
		if valueMap, ok := value.(map[string]any); ok {
			colErrors := validateColumnOperators(valueMap, col, currentPath)
			errors = append(errors, colErrors...)
		}
	}

	return errors
}

// isOperator returns true if the string is a known GraphJin operator
func isOperator(s string) bool {
	operators := map[string]bool{
		"eq": true, "neq": true, "gt": true, "gte": true, "lt": true, "lte": true,
		"in": true, "nin": true, "is_null": true,
		"like": true, "ilike": true, "regex": true, "iregex": true, "similar": true,
		"has_key": true, "has_key_any": true, "has_key_all": true, "contains": true, "contained_in": true,
		"st_dwithin": true, "st_within": true, "st_contains": true, "st_intersects": true,
		"st_coveredby": true, "st_covers": true, "st_touches": true, "st_overlaps": true, "near": true,
		"has_in_common": true,
	}
	return operators[s]
}

// validateColumnOperators validates operators and values for a column
func validateColumnOperators(operators map[string]any, col core.ColumnInfo, path string) []WhereValidationError {
	var errors []WhereValidationError

	validOps := getValidOperators(col.Type, col.Array)
	validOpsMap := make(map[string]bool)
	for _, op := range validOps {
		validOpsMap[op] = true
	}

	normalizedType := normalizeColumnType(col.Type)

	opKeys := make([]string, 0, len(operators))
	for op := range operators {
		opKeys = append(opKeys, op)
	}
	sort.Strings(opKeys)
	for _, op := range opKeys {
		value := operators[op]
		opPath := path + "." + op

		// Check if operator is valid for this column type
		if !validOpsMap[op] {
			errors = append(errors, WhereValidationError{
				Path:       opPath,
				Error:      "invalid_operator",
				Message:    fmt.Sprintf("Operator '%s' is not valid for column type '%s'", op, col.Type),
				ColumnType: col.Type,
				Suggestion: fmt.Sprintf("Valid operators for %s: %v", col.Type, validOps),
			})
			continue
		}

		// Validate value type matches operator expectations
		valueErr := validateOperatorValue(op, value, normalizedType, opPath)
		if valueErr != nil {
			errors = append(errors, *valueErr)
		}
	}

	return errors
}

// validateOperatorValue checks that the value type is appropriate for the operator and column type
func validateOperatorValue(op string, value any, colType string, path string) *WhereValidationError {
	// Handle is_null specially - must be boolean
	if op == "is_null" {
		if _, ok := value.(bool); !ok {
			return &WhereValidationError{
				Path:       path,
				Error:      "type_mismatch",
				Message:    fmt.Sprintf("Operator 'is_null' expects boolean value, got %T", value),
				ColumnType: colType,
				Suggestion: "Use: { is_null: true } or { is_null: false }",
			}
		}
		return nil
	}

	// Handle in/nin - must be arrays
	if op == "in" || op == "nin" {
		if _, ok := value.([]any); !ok {
			return &WhereValidationError{
				Path:       path,
				Error:      "type_mismatch",
				Message:    fmt.Sprintf("Operator '%s' expects array value, got %T", op, value),
				ColumnType: colType,
				Suggestion: fmt.Sprintf("Use: { %s: [value1, value2, ...] }", op),
			}
		}
		return nil
	}

	// Validate numeric operators require numeric values
	numericOps := map[string]bool{"gt": true, "gte": true, "lt": true, "lte": true}
	if numericOps[op] && colType == "numeric" {
		switch value.(type) {
		case float64, int, int64:
			// Valid
		case string:
			return &WhereValidationError{
				Path:       path,
				Error:      "type_mismatch",
				Message:    fmt.Sprintf("Operator '%s' expects numeric value, got string", op),
				ColumnType: colType,
				Suggestion: fmt.Sprintf("Use a number: { %s: 50 } not { %s: \"50\" }", op, op),
			}
		}
	}

	// Validate text operators require string values
	textOps := map[string]bool{"like": true, "ilike": true, "regex": true, "iregex": true, "similar": true}
	if textOps[op] {
		if _, ok := value.(string); !ok {
			return &WhereValidationError{
				Path:       path,
				Error:      "type_mismatch",
				Message:    fmt.Sprintf("Operator '%s' expects string value, got %T", op, value),
				ColumnType: colType,
				Suggestion: fmt.Sprintf("Use a string: { %s: \"pattern\" }", op),
			}
		}
	}

	// Validate boolean column with eq/neq requires boolean value
	if colType == "boolean" && (op == "eq" || op == "neq") {
		if _, ok := value.(bool); !ok {
			return &WhereValidationError{
				Path:       path,
				Error:      "type_mismatch",
				Message:    fmt.Sprintf("Boolean column with '%s' expects boolean value, got %T", op, value),
				ColumnType: colType,
				Suggestion: fmt.Sprintf("Use: { %s: true } or { %s: false }", op, op),
			}
		}
	}

	return nil
}
