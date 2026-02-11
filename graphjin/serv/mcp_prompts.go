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
	dbType = strings.ToLower(dbType)

	// Numeric types
	if strings.Contains(dbType, "int") ||
		strings.Contains(dbType, "serial") ||
		strings.Contains(dbType, "decimal") ||
		strings.Contains(dbType, "numeric") ||
		strings.Contains(dbType, "float") ||
		strings.Contains(dbType, "double") ||
		strings.Contains(dbType, "real") ||
		strings.Contains(dbType, "money") {
		return "numeric"
	}

	// Boolean types
	if strings.Contains(dbType, "bool") {
		return "boolean"
	}

	// JSON types
	if strings.Contains(dbType, "json") {
		return "json"
	}

	// Array types
	if strings.HasSuffix(dbType, "[]") || strings.Contains(dbType, "array") {
		return "array"
	}

	// Geometry/Geography types
	if strings.Contains(dbType, "geometry") ||
		strings.Contains(dbType, "geography") ||
		strings.Contains(dbType, "point") ||
		strings.Contains(dbType, "polygon") ||
		strings.Contains(dbType, "linestring") {
		return "geometry"
	}

	// Timestamp/Date types
	if strings.Contains(dbType, "timestamp") ||
		strings.Contains(dbType, "date") ||
		strings.Contains(dbType, "time") {
		return "timestamp"
	}

	// UUID types
	if strings.Contains(dbType, "uuid") {
		return "uuid"
	}

	// Default to text for varchar, char, text, etc.
	return "text"
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
	// write_where_clause - Help LLMs construct valid where clauses
	ms.srv.AddPrompt(mcp.NewPrompt(
		"write_where_clause",
		mcp.WithPromptDescription("Generate a valid GraphJin where clause for filtering data. Returns table schema with column types and valid operators for each column."),
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
		mcp.WithPromptDescription("Generate a complete GraphJin query with proper syntax. Returns table schema, relationship info, and a query template."),
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
		mcp.WithPromptDescription("Generate a GraphJin mutation with proper syntax for insert, update, upsert, or delete operations."),
		mcp.WithArgument("operation",
			mcp.ArgumentDescription("Mutation type: insert, update, upsert, or delete"),
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

// handleWriteWhereClause returns structured guidance for constructing where clauses
func (ms *mcpServer) handleWriteWhereClause(ctx context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	table := req.Params.Arguments["table"]
	intent := req.Params.Arguments["intent"]
	database := req.Params.Arguments["database"]

	if table == "" {
		return nil, fmt.Errorf("table argument is required")
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
	// Find a numeric column for example
	for _, col := range schema.Columns {
		normalizedType := normalizeColumnType(col.Type)
		if normalizedType == "numeric" {
			sb.WriteString(fmt.Sprintf("{ %s { sum_%s avg_%s min_%s max_%s } }  # Numeric aggregations\n",
				table, col.Name, col.Name, col.Name, col.Name))
			break
		}
	}
	sb.WriteString("```\n")

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

// handleFixQueryError analyzes query errors and provides fix suggestions
func (ms *mcpServer) handleFixQueryError(ctx context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	query := req.Params.Arguments["query"]
	errorMsg := req.Params.Arguments["error"]

	if query == "" {
		return nil, fmt.Errorf("query argument is required")
	}
	if errorMsg == "" {
		return nil, fmt.Errorf("error argument is required")
	}

	var sb strings.Builder

	sb.WriteString("# Query Error Analysis\n\n")
	sb.WriteString("## Error Message\n")
	sb.WriteString("```\n")
	sb.WriteString(errorMsg)
	sb.WriteString("\n```\n\n")

	sb.WriteString("## Original Query\n")
	sb.WriteString("```graphql\n")
	sb.WriteString(query)
	sb.WriteString("\n```\n\n")

	// Analyze the error and provide suggestions
	sb.WriteString("## Diagnosis & Suggestions\n\n")

	errorLower := strings.ToLower(errorMsg)

	switch {
	case strings.Contains(errorLower, "table") && (strings.Contains(errorLower, "not found") || strings.Contains(errorLower, "unknown")):
		sb.WriteString("**Problem**: Table name not found in schema\n\n")
		sb.WriteString("**Solutions**:\n")
		sb.WriteString("1. Check spelling of the table name\n")
		sb.WriteString("2. Use `list_tables` tool to see available tables\n")
		sb.WriteString("3. Table names are case-sensitive in some databases\n")

	case strings.Contains(errorLower, "column") && (strings.Contains(errorLower, "not found") || strings.Contains(errorLower, "unknown")):
		sb.WriteString("**Problem**: Column name not found in table\n\n")
		sb.WriteString("**Solutions**:\n")
		sb.WriteString("1. Check spelling of the column name\n")
		sb.WriteString("2. Use `describe_table` tool to see available columns\n")
		sb.WriteString("3. For JSON fields, use underscore notation: `metadata_key` for `metadata->key`\n")

	case strings.Contains(errorLower, "operator") || strings.Contains(errorLower, "invalid"):
		sb.WriteString("**Problem**: Invalid operator or syntax\n\n")
		sb.WriteString("**Solutions**:\n")
		sb.WriteString("1. Check operator spelling (eq, neq, gt, gte, lt, lte, in, nin, like, ilike)\n")
		sb.WriteString("2. Numeric operators (gt, lt, etc.) need numeric values, not strings\n")
		sb.WriteString("3. `in`/`nin` operators need arrays: `{ in: [1, 2] }` not `{ in: 1 }`\n")
		sb.WriteString("4. Use `get_query_syntax` tool for complete operator reference\n")

	case strings.Contains(errorLower, "syntax") || strings.Contains(errorLower, "parse"):
		sb.WriteString("**Problem**: Query syntax error\n\n")
		sb.WriteString("**Solutions**:\n")
		sb.WriteString("1. Check for missing braces `{ }` or parentheses `( )`\n")
		sb.WriteString("2. Ensure proper comma usage between fields\n")
		sb.WriteString("3. Verify string values are quoted: `{ eq: \"value\" }`\n")
		sb.WriteString("4. Check that `where:` is properly formatted as an object\n")

	case strings.Contains(errorLower, "permission") || strings.Contains(errorLower, "access") || strings.Contains(errorLower, "denied"):
		sb.WriteString("**Problem**: Permission or access denied\n\n")
		sb.WriteString("**Solutions**:\n")
		sb.WriteString("1. Check if mutations are enabled in config (`allow_mutations`)\n")
		sb.WriteString("2. Check if raw queries are enabled (`allow_raw_queries`)\n")
		sb.WriteString("3. Verify user has proper role for the operation\n")
		sb.WriteString("4. Try using a saved query with `execute_saved_query` instead\n")

	case strings.Contains(errorLower, "mutation") && strings.Contains(errorLower, "not allowed"):
		sb.WriteString("**Problem**: Mutations are disabled\n\n")
		sb.WriteString("**Solutions**:\n")
		sb.WriteString("1. Enable mutations in config: `mcp.allow_mutations: true`\n")
		sb.WriteString("2. Use a pre-approved saved mutation with `execute_saved_query`\n")

	case strings.Contains(errorLower, "variable") || strings.Contains(errorLower, "$"):
		sb.WriteString("**Problem**: Variable error\n\n")
		sb.WriteString("**Solutions**:\n")
		sb.WriteString("1. Ensure all `$variable` references have corresponding values\n")
		sb.WriteString("2. Pass variables in the `variables` parameter\n")
		sb.WriteString("3. Check variable types match expected values\n")

	default:
		sb.WriteString("**General debugging steps**:\n\n")
		sb.WriteString("1. Use `list_tables` and `describe_table` to verify schema\n")
		sb.WriteString("2. Use `get_query_syntax` or `get_mutation_syntax` for syntax reference\n")
		sb.WriteString("3. Simplify the query and add complexity incrementally\n")
		sb.WriteString("4. Check if a working saved query exists with `list_saved_queries`\n")
	}

	sb.WriteString("\n## Recommended Next Steps\n\n")
	sb.WriteString("1. Fix the identified issue\n")
	sb.WriteString("2. Execute the corrected query\n")

	return mcp.NewGetPromptResult(
		"Query error analysis",
		[]mcp.PromptMessage{
			mcp.NewPromptMessage(
				mcp.RoleAssistant,
				mcp.NewTextContent(sb.String()),
			),
		},
	), nil
}
