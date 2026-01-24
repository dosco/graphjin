package serv

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

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
	), ms.handleWriteWhereClause)
}

// handleWriteWhereClause returns structured guidance for constructing where clauses
func (ms *mcpServer) handleWriteWhereClause(ctx context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	table := req.Params.Arguments["table"]
	intent := req.Params.Arguments["intent"]

	if table == "" {
		return nil, fmt.Errorf("table argument is required")
	}

	// Fetch table schema
	schema, err := ms.service.gj.GetTableSchema(table)
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
