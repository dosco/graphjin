package serv

import (
	"context"
	"fmt"
	"strings"

	"github.com/dosco/graphjin/core/v3"
	"github.com/mark3labs/mcp-go/mcp"
)

// capitalizeFirst capitalizes the first letter of a string
func capitalizeFirst(s string) string {
	if len(s) == 0 {
		return s
	}
	if s[0] >= 'a' && s[0] <= 'z' {
		return string(s[0]-'a'+'A') + s[1:]
	}
	return s
}

// operatorTypeMapping defines which operators are valid for each column type
var operatorTypeMapping = map[string][]string{
	"numeric":   {"eq", "neq", "gt", "gte", "lt", "lte", "in", "nin", "is_null"},
	"text":      {"eq", "neq", "like", "ilike", "regex", "iregex", "similar", "in", "nin", "is_null"},
	"boolean":   {"eq", "neq", "is_null"},
	"json":      {"has_key", "has_key_any", "has_key_all", "contains", "contained_in", "is_null"},
	"array":     {"contains", "contained_in", "has_in_common", "is_null"},
	"geometry":  {"st_dwithin", "st_within", "st_contains", "st_intersects", "st_coveredby", "st_covers", "st_touches", "st_overlaps", "near"},
	"timestamp": {"eq", "neq", "gt", "gte", "lt", "lte", "in", "is_null"},
	"uuid":      {"eq", "neq", "in", "nin", "is_null"},
}

// normalizeColumnType maps database-specific types to general categories
func normalizeColumnType(dbType string) string {
	dbType = strings.ToLower(strings.TrimSpace(dbType))
	compactedType := strings.ReplaceAll(dbType, " ", "")

	// Dialect-specific boolean aliases must win before numeric detection.
	if isBooleanColumnType(compactedType) {
		return "boolean"
	}

	// Numeric types
	if strings.Contains(compactedType, "int") ||
		strings.Contains(compactedType, "serial") ||
		strings.Contains(compactedType, "decimal") ||
		strings.Contains(compactedType, "numeric") ||
		strings.Contains(compactedType, "number") ||
		strings.Contains(compactedType, "float") ||
		strings.Contains(compactedType, "double") ||
		strings.Contains(compactedType, "real") ||
		strings.Contains(compactedType, "money") {
		return "numeric"
	}

	// JSON types
	if strings.Contains(compactedType, "json") {
		return "json"
	}

	// Array types
	if strings.HasSuffix(compactedType, "[]") || strings.Contains(compactedType, "array") {
		return "array"
	}

	// Geometry/Geography types
	if strings.Contains(compactedType, "geometry") ||
		strings.Contains(compactedType, "geography") ||
		strings.Contains(compactedType, "point") ||
		strings.Contains(compactedType, "polygon") ||
		strings.Contains(compactedType, "linestring") {
		return "geometry"
	}

	// Timestamp/Date types
	if strings.Contains(compactedType, "timestamp") ||
		strings.Contains(compactedType, "date") ||
		strings.Contains(compactedType, "time") {
		return "timestamp"
	}

	// UUID types
	if strings.Contains(compactedType, "uuid") {
		return "uuid"
	}

	// Default to text for varchar, char, text, etc.
	return "text"
}

func isBooleanColumnType(dbType string) bool {
	switch dbType {
	case "bool", "boolean", "bit":
		return true
	}

	if strings.HasPrefix(dbType, "tinyint(") && strings.HasSuffix(dbType, ")") {
		return dbType == "tinyint(1)"
	}

	if strings.HasPrefix(dbType, "number(") && strings.HasSuffix(dbType, ")") {
		precision := strings.TrimSuffix(strings.TrimPrefix(dbType, "number("), ")")
		return precision == "1" || precision == "1,0"
	}

	return false
}

// getValidOperators returns the valid operators for a given database column type
func getValidOperators(dbType string, isArray bool) []string {
	if isArray {
		return operatorTypeMapping["array"]
	}
	normalizedType := normalizeColumnType(dbType)
	if ops, ok := operatorTypeMapping[normalizedType]; ok {
		return ops
	}
	return operatorTypeMapping["text"] // Default to text operators
}

// registerPrompts registers all MCP prompts with the server
func (ms *mcpServer) registerPrompts() {
	if ms.service.conf.mcpDisabled() {
		return
	}

	if ms.service.conf.Core.IsSourcesUsed() {
		return
	}

	queryPromptDesc := "Generate a complete GraphJin query with proper syntax. Uses catalog-first schema/language guidance plus relationship, aggregation, and analytics patterns."
	if ms.service.conf.legacyMCPToolsEnabled() {
		queryPromptDesc = "Generate a complete GraphJin query with proper syntax. Returns table schema, relationship info, aggregation examples, and analytics directive patterns."
	}
	// write_where_clause - Help LLMs construct valid where clauses
	ms.srv.AddPrompt(mcp.NewPrompt(
		"write_where_clause",
		mcp.WithPromptDescription("Generate a valid GraphJin where clause for filtering data. Uses catalog/operator context and returns valid operators for each column."),
		mcp.WithArgument("table",
			mcp.ArgumentDescription("Table name to filter"),
			mcp.RequiredArgument(),
		),
		mcp.WithArgument("intent",
			mcp.ArgumentDescription("What you want to filter (e.g., 'products over $50', 'users created this week')"),
			mcp.RequiredArgument(),
		),
		mcp.WithArgument("database",
			mcp.ArgumentDescription("Optional database name for multi-database deployments"),
		),
	), ms.handleWriteWhereClause)

	// write_query - Help LLMs construct complete GraphJin queries
	ms.srv.AddPrompt(mcp.NewPrompt(
		"write_query",
		mcp.WithPromptDescription(queryPromptDesc),
		mcp.WithArgument("table",
			mcp.ArgumentDescription("Primary table to query"),
			mcp.RequiredArgument(),
		),
		mcp.WithArgument("fields",
			mcp.ArgumentDescription("Fields to select (e.g., 'id, name, price' or 'all')"),
		),
		mcp.WithArgument("relationships",
			mcp.ArgumentDescription("Related tables to include (e.g., 'owner, categories')"),
		),
		mcp.WithArgument("filter_intent",
			mcp.ArgumentDescription("What to filter (e.g., 'active products over $50')"),
		),
		mcp.WithArgument("pagination",
			mcp.ArgumentDescription("Pagination style: 'limit' for limit/offset, 'cursor' for cursor-based"),
		),
		mcp.WithArgument("database",
			mcp.ArgumentDescription("Optional database name for multi-database deployments"),
		),
	), ms.handleWriteQuery)

	// write_mutation - Help LLMs construct GraphJin mutations
	ms.srv.AddPrompt(mcp.NewPrompt(
		"write_mutation",
		mcp.WithPromptDescription("Generate a GraphJin mutation with proper syntax for insert, update, upsert, delete, or CodeSQL preview/apply edit operations."),
		mcp.WithArgument("operation",
			mcp.ArgumentDescription("Mutation type: insert, update, upsert, delete, or CodeSQL preview/apply/acquire/release"),
			mcp.RequiredArgument(),
		),
		mcp.WithArgument("table",
			mcp.ArgumentDescription("Table to modify"),
			mcp.RequiredArgument(),
		),
		mcp.WithArgument("data_intent",
			mcp.ArgumentDescription("What data to modify (e.g., 'create user with email and name')"),
		),
		mcp.WithArgument("nested",
			mcp.ArgumentDescription("Related records to create/connect (e.g., 'create order with products')"),
		),
		mcp.WithArgument("database",
			mcp.ArgumentDescription("Optional database name for multi-database deployments"),
		),
	), ms.handleWriteMutation)

	// fix_query_error - Help LLMs fix query errors
	ms.srv.AddPrompt(mcp.NewPrompt(
		"fix_query_error",
		mcp.WithPromptDescription("Analyze a GraphJin query error and provide guidance on how to fix it."),
		mcp.WithArgument("query",
			mcp.ArgumentDescription("The query that produced the error"),
			mcp.RequiredArgument(),
		),
		mcp.WithArgument("error",
			mcp.ArgumentDescription("The error message received"),
			mcp.RequiredArgument(),
		),
	), ms.handleFixQueryError)
}

func (ms *mcpServer) requirePromptDB() error {
	if ms.service.gj == nil || !ms.service.gj.SchemaReady() {
		return fmt.Errorf(errNoDB)
	}
	return nil
}

// handleWriteWhereClause returns structured guidance for constructing where clauses
func (ms *mcpServer) handleWriteWhereClause(ctx context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	table := req.Params.Arguments["table"]
	intent := req.Params.Arguments["intent"]
	database := req.Params.Arguments["database"]

	if table == "" {
		return nil, fmt.Errorf("table argument is required")
	}
	if err := ms.requirePromptDB(); err != nil {
		return nil, err
	}

	// Fetch table schema
	var schema *core.TableSchema
	var err error
	if database != "" {
		schema, err = ms.service.gj.GetTableSchemaForDatabase(database, table)
	} else {
		schema, err = ms.service.gj.GetTableSchema(table)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get schema for table '%s': %w", table, err)
	}

	// Build the prompt content
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# Where Clause Guide for Table: %s\n\n", table))
	sb.WriteString(fmt.Sprintf("**Filtering Intent**: %s\n\n", intent))

	sb.WriteString("## Available Columns and Valid Operators\n\n")
	sb.WriteString("| Column | Type | Nullable | Valid Operators |\n")
	sb.WriteString("|--------|------|----------|----------------|\n")

	for _, col := range schema.Columns {
		operators := getValidOperators(col.Type, col.Array)
		nullable := "No"
		if col.Nullable {
			nullable = "Yes"
		}
		sb.WriteString(fmt.Sprintf("| `%s` | %s | %s | %s |\n",
			col.Name, col.Type, nullable, strings.Join(operators, ", ")))
	}

	sb.WriteString("\n## Where Clause Syntax\n\n")
	sb.WriteString("GraphJin where clauses use this structure:\n")
	sb.WriteString("```\n")
	sb.WriteString("where: { column: { operator: value } }\n")
	sb.WriteString("```\n\n")

	sb.WriteString("### Operator Examples by Type\n\n")

	sb.WriteString("**Numeric columns** (eq, neq, gt, gte, lt, lte, in, nin):\n")
	sb.WriteString("```graphql\n")
	sb.WriteString("where: { price: { gt: 50 } }           # Greater than\n")
	sb.WriteString("where: { price: { gte: 50, lte: 100 } } # Range (AND implicit)\n")
	sb.WriteString("where: { id: { in: [1, 2, 3] } }       # In list\n")
	sb.WriteString("```\n\n")

	sb.WriteString("**Text columns** (eq, neq, like, ilike, regex):\n")
	sb.WriteString("```graphql\n")
	sb.WriteString("where: { name: { eq: \"iPhone\" } }     # Exact match\n")
	sb.WriteString("where: { name: { ilike: \"%phone%\" } } # Case-insensitive contains\n")
	sb.WriteString("where: { email: { regex: \".*@gmail.com$\" } } # Regex match\n")
	sb.WriteString("```\n\n")

	sb.WriteString("**Boolean columns** (eq, neq):\n")
	sb.WriteString("```graphql\n")
	sb.WriteString("where: { is_active: { eq: true } }\n")
	sb.WriteString("```\n\n")

	sb.WriteString("**Null checks** (any column):\n")
	sb.WriteString("```graphql\n")
	sb.WriteString("where: { deleted_at: { is_null: true } }  # IS NULL\n")
	sb.WriteString("where: { deleted_at: { is_null: false } } # IS NOT NULL\n")
	sb.WriteString("```\n\n")

	sb.WriteString("**JSON/JSONB columns** (has_key, contains, contained_in):\n")
	sb.WriteString("```graphql\n")
	sb.WriteString("where: { metadata: { has_key: \"color\" } }      # Key exists\n")
	sb.WriteString("where: { tags: { contains: [\"sale\"] } }        # Contains values\n")
	sb.WriteString("```\n\n")

	sb.WriteString("**Spatial columns** (st_dwithin, st_within, st_contains):\n")
	sb.WriteString("```graphql\n")
	sb.WriteString("where: { location: { st_dwithin: { point: [-122.4, 37.7], distance: 1000 } } }\n")
	sb.WriteString("```\n\n")

	sb.WriteString("### Logical Operators\n\n")
	sb.WriteString("```graphql\n")
	sb.WriteString("# AND (implicit when multiple conditions on same level)\n")
	sb.WriteString("where: { price: { gt: 10 }, stock: { gt: 0 } }\n\n")
	sb.WriteString("# Explicit AND\n")
	sb.WriteString("where: { and: [{ price: { gt: 10 } }, { price: { lt: 100 } }] }\n\n")
	sb.WriteString("# OR\n")
	sb.WriteString("where: { or: [{ status: { eq: \"active\" } }, { status: { eq: \"pending\" } }] }\n\n")
	sb.WriteString("# NOT\n")
	sb.WriteString("where: { not: { status: { eq: \"deleted\" } } }\n")
	sb.WriteString("```\n\n")

	sb.WriteString("### Filter on Related Tables\n\n")
	sb.WriteString("```graphql\n")
	sb.WriteString("where: { owner: { email: { eq: \"admin@example.com\" } } }\n")
	sb.WriteString("```\n\n")

	sb.WriteString("## Type Validation Rules\n\n")
	sb.WriteString("- **Numeric operators** (gt, gte, lt, lte) require numeric values, not strings\n")
	sb.WriteString("- **Text operators** (like, ilike, regex) require string values\n")
	sb.WriteString("- **Boolean operators** require true/false, not strings\n")
	sb.WriteString("- **in/nin operators** require arrays: `{ in: [1, 2, 3] }` not `{ in: 1 }`\n")

	return mcp.NewGetPromptResult(
		fmt.Sprintf("Where clause guide for %s", table),
		[]mcp.PromptMessage{
			mcp.NewPromptMessage(
				mcp.RoleAssistant,
				mcp.NewTextContent(sb.String()),
			),
		},
	), nil
}

// handleWriteQuery returns structured guidance for constructing complete queries
func (ms *mcpServer) handleWriteQuery(ctx context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	table := req.Params.Arguments["table"]
	fields := req.Params.Arguments["fields"]
	relationships := req.Params.Arguments["relationships"]
	filterIntent := req.Params.Arguments["filter_intent"]
	pagination := req.Params.Arguments["pagination"]
	database := req.Params.Arguments["database"]

	if table == "" {
		return nil, fmt.Errorf("table argument is required")
	}
	if err := ms.requirePromptDB(); err != nil {
		return nil, err
	}

	// Fetch table schema
	var schema *core.TableSchema
	var err error
	if database != "" {
		schema, err = ms.service.gj.GetTableSchemaForDatabase(database, table)
	} else {
		schema, err = ms.service.gj.GetTableSchema(table)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get schema for table '%s': %w", table, err)
	}

	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# Query Guide for Table: %s\n\n", table))

	// Intent summary
	if fields != "" || filterIntent != "" {
		sb.WriteString("**Intent**: ")
		if fields != "" {
			sb.WriteString(fmt.Sprintf("Select %s", fields))
		}
		if filterIntent != "" {
			sb.WriteString(fmt.Sprintf(", filter by %s", filterIntent))
		}
		sb.WriteString("\n\n")
	}

	// Table schema
	sb.WriteString("## Table Schema\n\n")
	sb.WriteString("| Column | Type | Nullable | Key |\n")
	sb.WriteString("|--------|------|----------|-----|\n")
	for _, col := range schema.Columns {
		key := ""
		if col.PrimaryKey {
			key = "PK"
		} else if col.ForeignKey != "" {
			key = "FK → " + col.ForeignKey
		}
		nullable := "No"
		if col.Nullable {
			nullable = "Yes"
		}
		sb.WriteString(fmt.Sprintf("| `%s` | %s | %s | %s |\n",
			col.Name, col.Type, nullable, key))
	}
	sb.WriteString("\n")

	// Relationships
	if relationships != "" {
		sb.WriteString("## Relationships\n\n")
		if len(schema.Relationships.Outgoing) > 0 {
			sb.WriteString("**Outgoing** (this table references):\n")
			for _, rel := range schema.Relationships.Outgoing {
				sb.WriteString(fmt.Sprintf("- `%s` → %s (%s)\n", rel.Name, rel.Table, rel.Type))
			}
		}
		if len(schema.Relationships.Incoming) > 0 {
			sb.WriteString("\n**Incoming** (tables that reference this):\n")
			for _, rel := range schema.Relationships.Incoming {
				sb.WriteString(fmt.Sprintf("- `%s` ← %s (%s)\n", rel.Name, rel.Table, rel.Type))
			}
		}
		sb.WriteString("\n")
	}

	// Query template
	sb.WriteString("## Query Template\n\n")
	sb.WriteString("```graphql\n")

	// Build field list
	fieldList := "id"
	if fields != "" && fields != "all" {
		fieldList = fields
	} else if fields == "all" {
		var cols []string
		for _, col := range schema.Columns {
			cols = append(cols, col.Name)
		}
		fieldList = strings.Join(cols, " ")
	}

	// Build relationship includes
	relIncludes := ""
	if relationships != "" {
		rels := strings.Split(relationships, ",")
		for _, r := range rels {
			r = strings.TrimSpace(r)
			relIncludes += fmt.Sprintf("\n    %s { id }", r)
		}
	}

	// Build pagination
	paginationStr := "limit: 10"
	cursorNote := ""
	if pagination == "cursor" {
		paginationStr = "first: 10, after: $cursor"
		cursorNote = "\n  " + table + "_cursor  # Returns cursor for next page"
	}

	// Build where clause hint
	whereStr := ""
	if filterIntent != "" {
		whereStr = ", where: { /* see filter operators below */ }"
	}

	sb.WriteString(fmt.Sprintf(`{
  %s(%s%s) {
    %s%s
  }%s
}`, table, paginationStr, whereStr, fieldList, relIncludes, cursorNote))
	sb.WriteString("\n```\n\n")

	// Filter operators (if filter intent provided)
	if filterIntent != "" {
		sb.WriteString("## Available Filter Operators\n\n")
		sb.WriteString("```graphql\n")
		sb.WriteString("# Comparison: eq, neq, gt, gte, lt, lte\n")
		sb.WriteString("where: { price: { gt: 50 } }\n\n")
		sb.WriteString("# Text: like, ilike (case-insensitive), regex\n")
		sb.WriteString("where: { name: { ilike: \"%search%\" } }\n\n")
		sb.WriteString("# List: in, nin\n")
		sb.WriteString("where: { id: { in: [1, 2, 3] } }\n\n")
		sb.WriteString("# Null: is_null\n")
		sb.WriteString("where: { deleted_at: { is_null: true } }\n\n")
		sb.WriteString("# Logical: and, or, not\n")
		sb.WriteString("where: { or: [{ status: { eq: \"active\" } }, { featured: { eq: true } }] }\n")
		sb.WriteString("```\n\n")
	}

	// Aggregations
	sb.WriteString("## Available Aggregations\n\n")
	sb.WriteString("```graphql\n")
	sb.WriteString(fmt.Sprintf("{ %s { count_id } }  # Count rows\n", table))
	// Find two numeric columns for a richer example (arithmetic needs at
	// least one; the second demonstrates the expression aggregate form).
	var firstNumeric, secondNumeric string
	partitionCol := ""
	orderCol := ""
	for _, col := range schema.Columns {
		if partitionCol == "" && col.ForeignKey != "" {
			partitionCol = col.Name
		}
		if orderCol == "" && normalizeColumnType(col.Type) == "timestamp" {
			orderCol = col.Name
		}
		if normalizeColumnType(col.Type) != "numeric" {
			continue
		}
		if firstNumeric == "" {
			firstNumeric = col.Name
			continue
		}
		if secondNumeric == "" {
			secondNumeric = col.Name
			break
		}
	}
	if partitionCol == "" {
		for _, col := range schema.Columns {
			if col.PrimaryKey {
				partitionCol = col.Name
				break
			}
		}
	}
	if orderCol == "" {
		for _, col := range schema.Columns {
			if col.PrimaryKey {
				orderCol = col.Name
				break
			}
		}
	}
	if partitionCol == "" && len(schema.Columns) > 0 {
		partitionCol = schema.Columns[0].Name
	}
	if orderCol == "" && len(schema.Columns) > 0 {
		orderCol = schema.Columns[0].Name
	}
	if firstNumeric != "" {
		sb.WriteString(fmt.Sprintf("{ %s { sum_%s avg_%s min_%s max_%s } }  # Numeric aggregations\n",
			table, firstNumeric, firstNumeric, firstNumeric, firstNumeric))
	}
	sb.WriteString("```\n\n")

	// Analytics directives
	if firstNumeric != "" && partitionCol != "" && orderCol != "" {
		sb.WriteString("## Analytics Directives (reporting rows)\n\n")
		sb.WriteString("Use analytics directives for running metrics, moving averages, ranks, previous/next values, ")
		sb.WriteString("and first/last values without collapsing rows like a plain aggregate.\n\n")
		sb.WriteString("```graphql\n")
		sb.WriteString(fmt.Sprintf("{ %s(limit: 100) {\n", table))
		sb.WriteString(fmt.Sprintf("    %s\n", partitionCol))
		if orderCol != partitionCol {
			sb.WriteString(fmt.Sprintf("    %s\n", orderCol))
		}
		sb.WriteString(fmt.Sprintf("    %s\n", firstNumeric))
		sb.WriteString(fmt.Sprintf("    running_%s: %s @running(aggregate: sum, by: \"%s\", orderBy: { %s: asc })\n", firstNumeric, firstNumeric, partitionCol, orderCol))
		sb.WriteString(fmt.Sprintf("    moving_avg_%s: %s @moving(aggregate: avg, rows: 6, by: \"%s\", orderBy: { %s: asc })\n", firstNumeric, firstNumeric, partitionCol, orderCol))
		sb.WriteString(fmt.Sprintf("    previous_%s: %s @previous(by: \"%s\", orderBy: { %s: asc })\n", firstNumeric, firstNumeric, partitionCol, orderCol))
		sb.WriteString(fmt.Sprintf("    rank_by_%s: %s @rank(by: \"%s\", order: desc)\n", firstNumeric, firstNumeric, partitionCol))
		sb.WriteString("  }\n")
		sb.WriteString("}\n")
		sb.WriteString("```\n\n")
		sb.WriteString("Use grouped aggregates with `distinct` for one-row-per-group summaries; use analytics directives when each original row should remain visible.\n\n")
	}

	// Expression aggregates (the `<fn>_<col>` form only handles single
	// columns — any arithmetic across columns needs the expr: syntax).
	if firstNumeric != "" && secondNumeric != "" {
		sb.WriteString("## Expression Aggregates (for arithmetic metrics)\n\n")
		sb.WriteString("For metrics that multiply, subtract, or divide across columns (revenue, ")
		sb.WriteString("margin, weighted averages), use `sum(expr: ...)` — not `sum_<col>` × `sum_<col>`, ")
		sb.WriteString("which is mathematically wrong: `AVG(a) × SUM(b) ≠ SUM(a × b)`.\n\n")
		sb.WriteString("Leaves: bare identifier (column), quoted string (qualified column), ")
		sb.WriteString("number (literal), `$var` (bind).\n\n")
		sb.WriteString("```graphql\n")
		sb.WriteString(fmt.Sprintf("# SUM(%s × %s) as one server-side aggregate\n", firstNumeric, secondNumeric))
		sb.WriteString(fmt.Sprintf("{ %s { revenue: sum(expr: { mul: [%s, %s] }) } }\n\n", table, firstNumeric, secondNumeric))
		sb.WriteString("# Top N by computed metric — order_by on the alias\n")
		sb.WriteString(fmt.Sprintf("{ %s(distinct: [id], order_by: { revenue: desc }, limit: 10) {\n", table))
		sb.WriteString(fmt.Sprintf("    id revenue: sum(expr: { mul: [%s, %s] })\n", firstNumeric, secondNumeric))
		sb.WriteString("  }\n")
		sb.WriteString("}\n")
		sb.WriteString("```\n")
		if ms.service.conf.legacyMCPToolsEnabled() {
			sb.WriteString("\nCall `get_query_syntax` for the full expression grammar ")
		} else {
			sb.WriteString("\nCall `query_catalog` with `search: \"expression aggregate\"` and `where: { kind: { eq: \"query_pattern\" } }` for the full expression grammar ")
		}
		sb.WriteString("(add/sub/div, case, cast, coalesce, dot-notation for joined columns).\n")
	}

	return mcp.NewGetPromptResult(
		fmt.Sprintf("Query guide for %s", table),
		[]mcp.PromptMessage{
			mcp.NewPromptMessage(
				mcp.RoleAssistant,
				mcp.NewTextContent(sb.String()),
			),
		},
	), nil
}

// handleWriteMutation returns structured guidance for constructing mutations
func (ms *mcpServer) handleWriteMutation(ctx context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	operation := req.Params.Arguments["operation"]
	table := req.Params.Arguments["table"]
	dataIntent := req.Params.Arguments["data_intent"]
	nested := req.Params.Arguments["nested"]
	database := req.Params.Arguments["database"]

	if operation == "" {
		return nil, fmt.Errorf("operation argument is required (insert, update, upsert, delete)")
	}
	if table == "" {
		return nil, fmt.Errorf("table argument is required")
	}
	if err := ms.requirePromptDB(); err != nil {
		return nil, err
	}
	if isCodeSQLManagedMutationTable(table) {
		return ms.handleWriteCodeSQLMutationPrompt(operation, table)
	}

	// Validate operation
	validOps := map[string]bool{"insert": true, "update": true, "upsert": true, "delete": true}
	if !validOps[operation] {
		return nil, fmt.Errorf("invalid operation '%s', must be one of: insert, update, upsert, delete", operation)
	}

	// Fetch table schema
	var schema *core.TableSchema
	var err error
	if database != "" {
		schema, err = ms.service.gj.GetTableSchemaForDatabase(database, table)
	} else {
		schema, err = ms.service.gj.GetTableSchema(table)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get schema for table '%s': %w", table, err)
	}

	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# %s Mutation Guide for Table: %s\n\n", capitalizeFirst(operation), table))

	if dataIntent != "" {
		sb.WriteString(fmt.Sprintf("**Intent**: %s\n\n", dataIntent))
	}

	// Table schema (columns that can be set)
	sb.WriteString("## Settable Columns\n\n")
	sb.WriteString("| Column | Type | Nullable | Notes |\n")
	sb.WriteString("|--------|------|----------|-------|\n")
	for _, col := range schema.Columns {
		notes := ""
		if col.PrimaryKey {
			if operation == "insert" {
				notes = "Auto-generated (usually)"
			} else {
				notes = "Use for identification"
			}
		} else if col.ForeignKey != "" {
			notes = "FK → " + col.ForeignKey + " (use connect to link)"
		}
		nullable := "No"
		if col.Nullable {
			nullable = "Yes"
		}
		sb.WriteString(fmt.Sprintf("| `%s` | %s | %s | %s |\n",
			col.Name, col.Type, nullable, notes))
	}
	sb.WriteString("\n")

	// Operation-specific syntax
	sb.WriteString("## Mutation Syntax\n\n")

	switch operation {
	case "insert":
		sb.WriteString("### Insert (single record)\n")
		sb.WriteString("```graphql\n")
		sb.WriteString(fmt.Sprintf("mutation {\n  %s(insert: {\n", table))
		for _, col := range schema.Columns {
			if !col.PrimaryKey {
				sb.WriteString(fmt.Sprintf("    %s: $%s\n", col.Name, col.Name))
			}
		}
		sb.WriteString("  }) {\n    id\n  }\n}\n")
		sb.WriteString("```\n\n")

		sb.WriteString("### Bulk Insert (multiple records)\n")
		sb.WriteString("```graphql\n")
		sb.WriteString(fmt.Sprintf("mutation {\n  %s(insert: $items) {  # $items is an array\n    id\n  }\n}\n", table))
		sb.WriteString("```\n\n")

	case "update":
		sb.WriteString("### Update by ID\n")
		sb.WriteString("```graphql\n")
		sb.WriteString(fmt.Sprintf("mutation {\n  %s(id: $id, update: {\n    # fields to update\n  }) {\n    id\n  }\n}\n", table))
		sb.WriteString("```\n\n")

		sb.WriteString("### Update with Where clause\n")
		sb.WriteString("```graphql\n")
		sb.WriteString(fmt.Sprintf("mutation {\n  %s(where: { status: { eq: \"pending\" } }, update: {\n    status: \"processed\"\n  }) {\n    id\n  }\n}\n", table))
		sb.WriteString("```\n\n")

	case "upsert":
		sb.WriteString("### Upsert (Insert or Update)\n")
		sb.WriteString("```graphql\n")
		sb.WriteString(fmt.Sprintf("mutation {\n  %s(upsert: {\n    id: $id  # If exists: update, else: insert\n", table))
		for _, col := range schema.Columns {
			if !col.PrimaryKey {
				sb.WriteString(fmt.Sprintf("    %s: $%s\n", col.Name, col.Name))
			}
		}
		sb.WriteString("  }) {\n    id\n  }\n}\n")
		sb.WriteString("```\n\n")

	case "delete":
		sb.WriteString("### Delete by ID\n")
		sb.WriteString("```graphql\n")
		sb.WriteString(fmt.Sprintf("mutation {\n  %s(delete: true, where: { id: { eq: $id } }) {\n    id\n  }\n}\n", table))
		sb.WriteString("```\n\n")

		sb.WriteString("### Delete with Where clause\n")
		sb.WriteString("```graphql\n")
		sb.WriteString(fmt.Sprintf("mutation {\n  %s(delete: true, where: { status: { eq: \"cancelled\" } }) {\n    id\n  }\n}\n", table))
		sb.WriteString("```\n\n")
	}

	// Nested mutations
	if nested != "" || len(schema.Relationships.Outgoing) > 0 {
		sb.WriteString("## Nested Mutations\n\n")

		sb.WriteString("### Create with nested record\n")
		sb.WriteString("```graphql\n")
		sb.WriteString(fmt.Sprintf("mutation {\n  %s(insert: {\n    name: $name\n", table))
		if len(schema.Relationships.Outgoing) > 0 {
			rel := schema.Relationships.Outgoing[0]
			sb.WriteString(fmt.Sprintf("    %s: { name: $related_name }  # Creates new %s\n", rel.Name, rel.Table))
		}
		sb.WriteString("  }) {\n    id\n  }\n}\n")
		sb.WriteString("```\n\n")

		sb.WriteString("### Connect to existing record\n")
		sb.WriteString("```graphql\n")
		sb.WriteString(fmt.Sprintf("mutation {\n  %s(insert: {\n    name: $name\n", table))
		if len(schema.Relationships.Outgoing) > 0 {
			rel := schema.Relationships.Outgoing[0]
			sb.WriteString(fmt.Sprintf("    %s: { connect: { id: $%s_id } }  # Links to existing %s\n", rel.Name, rel.Name, rel.Table))
		}
		sb.WriteString("  }) {\n    id\n  }\n}\n")
		sb.WriteString("```\n\n")

		sb.WriteString("### Disconnect related record\n")
		sb.WriteString("```graphql\n")
		sb.WriteString(fmt.Sprintf("mutation {\n  %s(id: $id, update: {\n", table))
		if len(schema.Relationships.Outgoing) > 0 {
			rel := schema.Relationships.Outgoing[0]
			sb.WriteString(fmt.Sprintf("    %s: { disconnect: { id: $%s_id } }\n", rel.Name, rel.Name))
		}
		sb.WriteString("  }) {\n    id\n  }\n}\n")
		sb.WriteString("```\n\n")
	}

	// Validation directives
	sb.WriteString("## Validation Directives\n\n")
	sb.WriteString("Add validation to your mutation:\n")
	sb.WriteString("```graphql\n")
	sb.WriteString("mutation @constraint(variable: \"email\", format: \"email\")\n")
	sb.WriteString("         @constraint(variable: \"name\", min: 1, max: 100) {\n")
	sb.WriteString(fmt.Sprintf("  %s(insert: { email: $email, name: $name }) { id }\n", table))
	sb.WriteString("}\n")
	sb.WriteString("```\n\n")
	sb.WriteString("Available validation options: `format`, `min`, `max`, `required`, `requiredIf`, `greaterThan`, `lessThan`\n")

	return mcp.NewGetPromptResult(
		fmt.Sprintf("%s mutation guide for %s", capitalizeFirst(operation), table),
		[]mcp.PromptMessage{
			mcp.NewPromptMessage(
				mcp.RoleAssistant,
				mcp.NewTextContent(sb.String()),
			),
		},
	), nil
}

func isCodeSQLManagedMutationTable(table string) bool {
	switch table {
	case "gj_code":
		return true
	default:
		return false
	}
}

func (ms *mcpServer) handleWriteCodeSQLMutationPrompt(operation, table string) (*mcp.GetPromptResult, error) {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# %s Mutation Guide for Table: %s\n\n", capitalizeFirst(operation), table))
	sb.WriteString("## Workflow\n\n")
	sb.WriteString("1. Query `gj_code` by `kind` first and request `code` or `code_context` plus `path` and `hash`.\n")
	sb.WriteString("2. Use `kind: \"symbol\"`, `kind: \"file\"`, or `kind: \"db_reference\"` to find the right source item.\n")
	sb.WriteString("3. Preview the edit with `gj_code(kind: \"change_set\", action: \"preview\")`.\n")
	sb.WriteString("4. Apply only after the preview diff looks correct.\n\n")
	sb.WriteString("## Rules\n\n")
	sb.WriteString("- Use `op: \"replace\"`, `op: \"create\"`, `op: \"delete\"`, or `op: \"rename\"` in each edit.\n")
	sb.WriteString("- Include the exact current `expected_hash` for replace/delete/rename. Create does not use `expected_hash` and fails if the target exists.\n")
	sb.WriteString("- Include the exact `old_text` for every replacement range.\n")
	sb.WriteString("- For create/rename into new directories, set `mkdirs: true`.\n")
	sb.WriteString("- If apply reports stale hash, re-read CodeSQL source and submit a fresh preview.\n")
	sb.WriteString("- Never query or mutate raw CodeSQL roots like `code_files`, `code_symbols`, `code_nodes`, or `code_captures` directly.\n")
	sb.WriteString("- Use `gj_code` with `kind: \"lock\"` only for longer edit sessions that need an explicit lease; reserve create/rename targets with `whole_file: true`.\n\n")
	sb.WriteString("## Query Before Editing\n\n")
	sb.WriteString("```graphql\n")
	sb.WriteString(`query {
  gj_code(where: { kind: { eq: "symbol" }, name: { eq: "LoadUser" } }) {
    name
    start_byte
    end_byte
    code
    code_context
    path
    hash
  }
}`)
	sb.WriteString("\n```\n\n")

	switch table {
	case "gj_code":
		sb.WriteString("## Mutation Template\n\n```graphql\n")
		switch operation {
		case "apply", "update":
			sb.WriteString(`mutation {
  gj_code(id: "change_set:123", update: { kind: "change_set", id: 123, action: "apply" }) {
    id
    kind
    status
    files_changed
    files_reindexed
    errors_json
  }
}`)
		default:
			sb.WriteString(`mutation {
  gj_code(insert: {
    kind: "change_set"
    action: "preview"
    title: "update LoadUser"
    edits: [{
      op: "replace"
      path: "main.go"
      expected_hash: "current-file-hash"
      replacements: [{
        start_byte: 10
        end_byte: 20
        old_text: "old code"
        new_text: "new code"
      }]
    }, {
      op: "create"
      path: "pkg/new_file.go"
      content: "package pkg\n"
      mkdirs: true
    }, {
      op: "delete"
      path: "old_file.go"
      expected_hash: "current-file-hash"
    }, {
      op: "rename"
      path: "old_name.go"
      new_path: "pkg/new_name.go"
      expected_hash: "current-file-hash"
      mkdirs: true
    }]
  }) {
    id
    kind
    status
    diff
    errors_json
  }
}`)
		}
		sb.WriteString("\n```\n")
		sb.WriteString("\n## Explicit Lock Template\n\n```graphql\n")
		if operation == "release" || operation == "unlock" {
			sb.WriteString(`mutation {
  gj_code(id: "lock:7", update: { kind: "lock", id: 7, action: "release", lease_token: "lock-token" }) {
    id
    kind
    status
    path
  }
}`)
		} else {
			sb.WriteString(`mutation {
  gj_code(insert: {
    kind: "lock"
    action: "acquire"
    path: "main.go"
    owner: "agent"
    ranges: [{ start_byte: 10, end_byte: 20 }]
  }) {
    id
    kind
    lease_token
    status
  }
}`)
		}
		sb.WriteString("\n```\n")
	}

	return mcp.NewGetPromptResult(
		fmt.Sprintf("Mutation guide for %s", table),
		[]mcp.PromptMessage{
			mcp.NewPromptMessage(
				mcp.RoleAssistant,
				mcp.NewTextContent(sb.String()),
			),
		},
	), nil
}

// handleFixQueryError analyzes query errors and provides structured fix
// suggestions. The repair logic lives in buildFixQueryErrorRepair so the
// tool variant can return both the markdown guide AND the structured
// fields (kind, diagnosis, repaired_query, follow_up_tools).
func (ms *mcpServer) handleFixQueryError(ctx context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	query := req.Params.Arguments["query"]
	errorMsg := req.Params.Arguments["error"]

	if query == "" {
		return nil, fmt.Errorf("query argument is required")
	}
	if errorMsg == "" {
		return nil, fmt.Errorf("error argument is required")
	}

	repair := buildFixQueryErrorRepair(query, errorMsg, ms.analyticsModeOn())
	return mcp.NewGetPromptResult(
		repair.Title,
		[]mcp.PromptMessage{
			mcp.NewPromptMessage(
				mcp.RoleAssistant,
				mcp.NewTextContent(repair.GuideMarkdown),
			),
		},
	), nil
}
