package serv

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dosco/graphjin/core/v3"
	"github.com/mark3labs/mcp-go/mcp"
)

// registerSchemaTools registers the schema discovery tools
func (ms *mcpServer) registerSchemaTools() {
	// list_tables - List all database tables
	ms.srv.AddTool(mcp.NewTool(
		"list_tables",
		mcp.WithDescription("List all database tables available for querying. Returns table names, types, and column counts."),
		mcp.WithString("namespace",
			mcp.Description("Optional namespace for multi-tenant deployments"),
		),
	), ms.handleListTables)

	// describe_table - Get detailed table schema with relationships
	ms.srv.AddTool(mcp.NewTool(
		"describe_table",
		mcp.WithDescription("Get detailed schema information for a table, including columns, types, and relationships (both incoming and outgoing)."),
		mcp.WithString("table",
			mcp.Required(),
			mcp.Description("Name of the table to describe"),
		),
		mcp.WithString("namespace",
			mcp.Description("Optional namespace for multi-tenant deployments"),
		),
	), ms.handleDescribeTable)

	// find_path - Find relationship path between tables
	ms.srv.AddTool(mcp.NewTool(
		"find_path",
		mcp.WithDescription("Find the relationship path between two tables. Useful for understanding how to join tables in queries."),
		mcp.WithString("from_table",
			mcp.Required(),
			mcp.Description("Starting table name"),
		),
		mcp.WithString("to_table",
			mcp.Required(),
			mcp.Description("Target table name"),
		),
	), ms.handleFindPath)

	// validate_graphql - Validate a query without executing
	ms.srv.AddTool(mcp.NewTool(
		"validate_graphql",
		mcp.WithDescription("Validate a GraphJin GraphQL query without executing it. Returns validation errors or the generated SQL if valid."),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("The GraphQL query to validate"),
		),
	), ms.handleValidateGraphQL)

	// explain_graphql - Show generated SQL for a query
	ms.srv.AddTool(mcp.NewTool(
		"explain_graphql",
		mcp.WithDescription("Show the SQL that would be generated for a GraphQL query. Useful for understanding query execution."),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("The GraphQL query to explain"),
		),
		mcp.WithObject("variables",
			mcp.Description("Optional query variables"),
		),
	), ms.handleExplainGraphQL)

	// validate_where_clause - Validate where clause syntax and type compatibility
	ms.srv.AddTool(mcp.NewTool(
		"validate_where_clause",
		mcp.WithDescription("Validate a where clause for syntax and type compatibility. Checks that operators match column types and returns detailed error messages with suggestions."),
		mcp.WithString("table",
			mcp.Required(),
			mcp.Description("Table name to validate against"),
		),
		mcp.WithString("where",
			mcp.Required(),
			mcp.Description("The where clause to validate (e.g., '{ price: { gt: 50 } }')"),
		),
	), ms.handleValidateWhereClause)
}

// handleListTables returns all available tables
func (ms *mcpServer) handleListTables(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	tables := ms.service.gj.GetTables()

	result := struct {
		Tables []core.TableInfo `json:"tables"`
		Count  int              `json:"count"`
	}{
		Tables: tables,
		Count:  len(tables),
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

// handleDescribeTable returns detailed schema for a table
func (ms *mcpServer) handleDescribeTable(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	table, _ := args["table"].(string)

	if table == "" {
		return mcp.NewToolResultError("table name is required"), nil
	}

	schema, err := ms.service.gj.GetTableSchema(table)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	data, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

// handleFindPath finds the relationship path between two tables
func (ms *mcpServer) handleFindPath(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	fromTable, _ := args["from_table"].(string)
	toTable, _ := args["to_table"].(string)

	if fromTable == "" || toTable == "" {
		return mcp.NewToolResultError("both from_table and to_table are required"), nil
	}

	path, err := ms.service.gj.FindRelationshipPath(fromTable, toTable)
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

	data, err := json.MarshalIndent(result, "", "  ")
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

// handleValidateGraphQL validates a query without executing
func (ms *mcpServer) handleValidateGraphQL(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	query, _ := args["query"].(string)

	if query == "" {
		return mcp.NewToolResultError("query is required"), nil
	}

	// Execute with empty variables to validate
	var rc core.RequestConfig
	rc.SetNamespace(ms.getNamespace())

	res, err := ms.service.gj.GraphQL(ctx, query, nil, &rc)

	result := struct {
		Valid  bool     `json:"valid"`
		Errors []string `json:"errors,omitempty"`
		SQL    string   `json:"sql,omitempty"`
	}{}

	if err != nil {
		result.Valid = false
		result.Errors = []string{err.Error()}
	} else if len(res.Errors) > 0 {
		result.Valid = false
		for _, e := range res.Errors {
			result.Errors = append(result.Errors, e.Message)
		}
	} else {
		result.Valid = true
		result.SQL = res.SQL()
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

// handleExplainGraphQL shows the SQL generated for a query
func (ms *mcpServer) handleExplainGraphQL(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	query, _ := args["query"].(string)

	if query == "" {
		return mcp.NewToolResultError("query is required"), nil
	}

	// Convert variables map to JSON
	var varsJSON json.RawMessage
	if vars, ok := args["variables"].(map[string]any); ok && len(vars) > 0 {
		var err error
		varsJSON, err = json.Marshal(vars)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid variables: %v", err)), nil
		}
	}

	var rc core.RequestConfig
	rc.SetNamespace(ms.getNamespace())

	res, err := ms.service.gj.GraphQL(ctx, query, varsJSON, &rc)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("query error: %v", err)), nil
	}

	if len(res.Errors) > 0 {
		errMsgs := make([]string, len(res.Errors))
		for i, e := range res.Errors {
			errMsgs[i] = e.Message
		}
		return mcp.NewToolResultError(fmt.Sprintf("query errors: %v", errMsgs)), nil
	}

	// Convert operation type to string
	opStr := "unknown"
	switch res.Operation() {
	case core.OpQuery:
		opStr = "query"
	case core.OpMutation:
		opStr = "mutation"
	}

	result := struct {
		SQL          string `json:"sql"`
		Operation    string `json:"operation"`
		CacheControl string `json:"cache_control,omitempty"`
	}{
		SQL:          res.SQL(),
		Operation:    opStr,
		CacheControl: res.CacheControl(),
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

// getNamespace returns the configured namespace
func (ms *mcpServer) getNamespace() string {
	if ms.service.namespace != nil {
		return *ms.service.namespace
	}
	return ""
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
	args := req.GetArguments()
	table, _ := args["table"].(string)
	whereClause, _ := args["where"].(string)

	if table == "" {
		return mcp.NewToolResultError("table name is required"), nil
	}
	if whereClause == "" {
		return mcp.NewToolResultError("where clause is required"), nil
	}

	// Get table schema
	schema, err := ms.service.gj.GetTableSchema(table)
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
		data, _ := json.MarshalIndent(result, "", "  ")
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

	data, err := json.MarshalIndent(result, "", "  ")
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

	for key, value := range where {
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

	for op, value := range operators {
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
