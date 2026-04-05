package serv

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	core "github.com/dosco/graphjin/core/v3"
)

// DiscoveryDocument holds a generated schema discovery document for a database.
// Content is split into focused sections so agents load only what they need.
type DiscoveryDocument struct {
	Database    string    `json:"database"`
	Hash        string    `json:"hash"`
	GeneratedAt time.Time `json:"generated_at"`

	// Sections — served as individual MCP resources (graphjin://discovery/*)
	Syntax     string `json:"syntax"`      // query syntax reference
	Tables     string `json:"tables"`      // compact table index (names, FKs, key columns)
	FullTables string `json:"full_tables"` // detailed table definitions (columns, types, live data)
	Insights   string `json:"insights"`    // relationship paths, templates, data quality, functions
}

// generateDiscovery generates the discovery document for a database using core's public API.
// generateDiscoveryBase builds the discovery document using only schema metadata
// (no live enrichment queries). This is fast — pure in-memory work.
func generateDiscoveryBase(gj *core.GraphJin, database string, schemas []*core.TableSchema) *DiscoveryDocument {
	hash := fmt.Sprintf("%x", time.Now().UnixNano())
	now := time.Now().UTC()

	defaultLimit := 20
	var syntaxSB strings.Builder
	writeQuerySyntaxReference(&syntaxSB, defaultLimit)

	duplicateSchemas := buildDuplicateIndex(schemas)

	var tableIndexSB strings.Builder
	tableIndexSB.WriteString("## Tables\n\n")
	for _, s := range schemas {
		writeTableIndexEntry(&tableIndexSB, s, nil, duplicateSchemas)
	}

	var fullTablesSB strings.Builder
	fullTablesSB.WriteString("## Tables (Full Detail)\n\n")
	for _, s := range schemas {
		writeTableMarkdown(&fullTablesSB, s, nil, duplicateSchemas)
	}

	var insightsSB strings.Builder
	writeDuplicateTableWarnings(&insightsSB, schemas)
	writeRelationshipPaths(&insightsSB, gj, database, schemas)
	writeNamespaceRouting(&insightsSB, gj)
	writeQueryTemplates(&insightsSB, schemas, nil)
	writeDataQuality(&insightsSB, schemas, nil)

	functions := gj.GetFunctionsForDatabase(database)
	writeFunctions(&insightsSB, functions)

	return &DiscoveryDocument{
		Database:    database,
		Hash:        hash,
		GeneratedAt: now,
		Syntax:      syntaxSB.String(),
		Tables:      tableIndexSB.String(),
		FullTables:  fullTablesSB.String(),
		Insights:    insightsSB.String(),
	}
}

// generateDiscoveryEnriched rebuilds the discovery document with live enrichment
// data (row counts, date ranges, distinct values, sample rows).
func generateDiscoveryEnriched(ctx context.Context, gj *core.GraphJin, database string, schemas []*core.TableSchema) *DiscoveryDocument {
	enrichment := buildEnrichment(ctx, gj, database, schemas)

	hash := fmt.Sprintf("%x", time.Now().UnixNano())
	now := time.Now().UTC()

	defaultLimit := 20
	var syntaxSB strings.Builder
	writeQuerySyntaxReference(&syntaxSB, defaultLimit)

	duplicateSchemas := buildDuplicateIndex(schemas)

	var tableIndexSB strings.Builder
	tableIndexSB.WriteString("## Tables\n\n")
	for _, s := range schemas {
		writeTableIndexEntry(&tableIndexSB, s, enrichment[s.Name], duplicateSchemas)
	}

	var fullTablesSB strings.Builder
	fullTablesSB.WriteString("## Tables (Full Detail)\n\n")
	for _, s := range schemas {
		writeTableMarkdown(&fullTablesSB, s, enrichment[s.Name], duplicateSchemas)
	}

	var insightsSB strings.Builder
	writeDuplicateTableWarnings(&insightsSB, schemas)
	writeRelationshipPaths(&insightsSB, gj, database, schemas)
	writeNamespaceRouting(&insightsSB, gj)
	writeQueryTemplates(&insightsSB, schemas, enrichment)
	writeDataQuality(&insightsSB, schemas, enrichment)

	functions := gj.GetFunctionsForDatabase(database)
	writeFunctions(&insightsSB, functions)

	return &DiscoveryDocument{
		Database:    database,
		Hash:        hash,
		GeneratedAt: now,
		Syntax:      syntaxSB.String(),
		Tables:      tableIndexSB.String(),
		FullTables:  fullTablesSB.String(),
		Insights:    insightsSB.String(),
	}
}

// tableEnrichment holds live data for a table.
type tableEnrichment struct {
	RowCount       int64
	DateRanges     map[string][2]string // column -> [min, max]
	DistinctValues map[string][]string  // column -> values
	ValueStats     map[string]numStats  // column -> stats
	SampleRows     []map[string]any
}

type numStats struct {
	Min   string
	Max   string
	Avg   string
	Sum   string
	Count int64
}

// buildEnrichment executes GraphQL queries against the database to gather live data.
func buildEnrichment(ctx context.Context, gj *core.GraphJin, database string, schemas []*core.TableSchema) map[string]*tableEnrichment {
	result := make(map[string]*tableEnrichment)

	// Per-query timeout to prevent individual queries from hanging
	const queryTimeout = 10 * time.Second

	for _, schema := range schemas {
		e := &tableEnrichment{
			DateRanges:     make(map[string][2]string),
			DistinctValues: make(map[string][]string),
			ValueStats:     make(map[string]numStats),
		}

		// Identify column types
		var numericCols, dateCols, enumCols []core.ColumnInfo
		var allColNames []string
		for _, col := range schema.Columns {
			allColNames = append(allColNames, col.Name)
			if isNumericType(col.Type) && !col.PrimaryKey && !strings.HasSuffix(col.Name, "_id") {
				numericCols = append(numericCols, col)
			}
			if isDateType(col.Type) {
				dateCols = append(dateCols, col)
			}
			if isEnumCandidateCol(col) {
				enumCols = append(enumCols, col)
			}
		}

		rc := &core.RequestConfig{}
		rc.SetNamespace(database)

		// Row count
		if schema.PrimaryKey != "" {
			qctx, cancel := context.WithTimeout(ctx, queryTimeout)
			q := fmt.Sprintf("{ %s(limit: 1) { count_%s } }", schema.Name, schema.PrimaryKey)
			if res, err := gj.GraphQL(qctx, q, nil, rc); err == nil && res.Data != nil {
				e.RowCount = extractCountFromResult(res.Data, schema.Name, schema.PrimaryKey)
			}
			cancel()
		}

		// Date ranges
		for _, col := range dateCols {
			qctx, cancel := context.WithTimeout(ctx, queryTimeout)
			q := fmt.Sprintf("{ %s(limit: 1) { min_%s max_%s } }", schema.Name, col.Name, col.Name)
			if res, err := gj.GraphQL(qctx, q, nil, rc); err == nil && res.Data != nil {
				min, max := extractMinMaxFromResult(res.Data, schema.Name, col.Name)
				if min != "" || max != "" {
					e.DateRanges[col.Name] = [2]string{min, max}
				}
			}
			cancel()
		}

		// Distinct values for enum columns
		for _, col := range enumCols {
			qctx, cancel := context.WithTimeout(ctx, queryTimeout)
			q := fmt.Sprintf("{ %s(distinct: [%s], limit: 50) { %s } }", schema.Name, col.Name, col.Name)
			if res, err := gj.GraphQL(qctx, q, nil, rc); err == nil && res.Data != nil {
				vals := extractDistinctFromResult(res.Data, schema.Name, col.Name)
				if len(vals) > 0 {
					e.DistinctValues[col.Name] = vals
				}
			}
			cancel()
		}

		// Value stats for numeric columns
		for _, col := range numericCols {
			qctx, cancel := context.WithTimeout(ctx, queryTimeout)
			q := fmt.Sprintf("{ %s(limit: 1) { min_%s max_%s avg_%s sum_%s count_%s } }",
				schema.Name, col.Name, col.Name, col.Name, col.Name, col.Name)
			if res, err := gj.GraphQL(qctx, q, nil, rc); err == nil && res.Data != nil {
				e.ValueStats[col.Name] = extractStatsFromResult(res.Data, schema.Name, col.Name)
			}
			cancel()
		}

		// Sample rows (5 most recent)
		sampleCols := allColNames
		if len(sampleCols) > 10 {
			// Limit to first 10 columns for readability
			sampleCols = sampleCols[:10]
		}
		orderClause := ""
		if len(dateCols) > 0 {
			orderClause = fmt.Sprintf(", order_by: { %s: desc }", dateCols[0].Name)
		}
		qctx, cancel := context.WithTimeout(ctx, queryTimeout)
		q := fmt.Sprintf("{ %s(limit: 5%s) { %s } }", schema.Name, orderClause, strings.Join(sampleCols, " "))
		if res, err := gj.GraphQL(qctx, q, nil, rc); err == nil && res.Data != nil {
			e.SampleRows = extractRowsFromResult(res.Data, schema.Name)
		}
		cancel()

		result[schema.Name] = e
	}

	return result
}

// writeQuerySyntaxReference writes the GraphJin DSL cheat sheet into the discovery document.
func writeQuerySyntaxReference(sb *strings.Builder, defaultLimit int) {
	sb.WriteString("## Query Syntax Reference\n\n")

	// ── Critical operational rules — must be first so agents see them ──
	sb.WriteString("### IMPORTANT: How to answer data questions\n\n")
	sb.WriteString("**ALWAYS use workflows** (`execute_workflow`) to answer data questions.\n")
	sb.WriteString("Do NOT use `execute_graphql` directly — tables can have hundreds of thousands\n")
	sb.WriteString("of rows and you cannot predict result sizes in advance. Workflows paginate\n")
	sb.WriteString("through data server-side and aggregate in JavaScript.\n\n")
	sb.WriteString("1. Check `list_workflows` first — reuse an existing workflow if one fits.\n")
	sb.WriteString("2. If none fits, write a new workflow using `execute_workflow`.\n")
	sb.WriteString("3. Inside workflow queries, use **top-down nesting** (see below).\n\n")

	sb.WriteString("### IMPORTANT: Query direction — ALWAYS top-down\n\n")
	sb.WriteString("Start from the grouping/parent table and nest downward into children.\n")
	sb.WriteString("NEVER start from a leaf table and filter upward through relationships.\n\n")
	sb.WriteString("```graphql\n")
	sb.WriteString("# CORRECT — top-down from territory into orders into details:\n")
	sb.WriteString("{ salesterritory { name\n")
	sb.WriteString("    salesorderheader {\n")
	sb.WriteString("      salesorderdetail(distinct: [productid]) {\n")
	sb.WriteString("        productid sum_orderqty\n")
	sb.WriteString("      }\n")
	sb.WriteString("    }\n")
	sb.WriteString("  }\n")
	sb.WriteString("}\n\n")
	sb.WriteString("# WRONG — bottom-up filtering from detail through header to territory:\n")
	sb.WriteString("{ salesorderdetail(where: { salesorderheader: { territoryid: { eq: 1 } } }) { ... } }\n")
	sb.WriteString("```\n\n")

	sb.WriteString("### IMPORTANT: Known limitations\n\n")
	sb.WriteString(fmt.Sprintf("- **Default row limit is %d** — every query level (top AND nested) is silently\n", defaultLimit))
	sb.WriteString("  capped unless you set an explicit `limit`. Always set limits on every level.\n")
	sb.WriteString("- **order_by on aggregation columns is supported** — `order_by: { sum_price: desc }` works\n")
	sb.WriteString("  when used with `distinct`. Cursor pagination is disabled for aggregated queries.\n")
	sb.WriteString("- **Use `find_path` or `explore_relationships`** to discover join paths between\n")
	sb.WriteString("  tables — never guess at foreign key relationships.\n\n")
	sb.WriteString("---\n\n")

	sb.WriteString("### Filter Operators (where clause)\n")
	sb.WriteString("```\n")
	sb.WriteString("Comparison: eq, neq, gt, gte, lt, lte\n")
	sb.WriteString("List:       in, nin (not_in)          — MUST be arrays: { id: { in: [1,2,3] } }\n")
	sb.WriteString("Null:       is_null                   — { col: { is_null: true } }\n")
	sb.WriteString("Text:       like, ilike, regex        — ilike needs % wildcards: { name: { ilike: \"%bike%\" } }\n")
	sb.WriteString("JSON:       has_key, has_key_any, has_key_all, contains, contained_in\n")
	sb.WriteString("Logical:    and, or, not              — { and: [{ price: { gt: 10 } }, { price: { lt: 100 } }] }\n")
	sb.WriteString("```\n\n")

	sb.WriteString("### Aggregation Functions\n")
	sb.WriteString("Use as field names with `<fn>_<column>` pattern:\n")
	sb.WriteString("```graphql\n")
	sb.WriteString("{ products { count_id sum_price avg_price min_price max_price } }\n")
	sb.WriteString("```\n\n")

	sb.WriteString("### Grouping (distinct, NOT group_by)\n")
	sb.WriteString("GraphJin uses `distinct` (not `group_by`) to group aggregation results:\n")
	sb.WriteString("```graphql\n")
	sb.WriteString("# Group by category — returns one row per category with aggregates\n")
	sb.WriteString("{ products(distinct: [category_id]) { category_id count_id sum_price avg_price } }\n")
	sb.WriteString("```\n")
	sb.WriteString("> **IMPORTANT:** `group_by` does NOT exist. Always use `distinct: [columns]`.\n")
	sb.WriteString("> `distinct` only works on columns from the base table, not joined tables.\n\n")

	sb.WriteString("### Nested Aggregation (aggregating child tables)\n")
	sb.WriteString("You can aggregate on nested/child tables. Each level has its own GROUP BY:\n")
	sb.WriteString("```graphql\n")
	sb.WriteString("# Revenue by product within a filtered parent\n")
	sb.WriteString("{ orders(where: { region_id: { eq: 1 } }) {\n")
	sb.WriteString("    order_items(distinct: [product_id]) {\n")
	sb.WriteString("      product_id\n")
	sb.WriteString("      sum_quantity\n")
	sb.WriteString("      sum_revenue\n")
	sb.WriteString("      count_id\n")
	sb.WriteString("    }\n")
	sb.WriteString("  }\n")
	sb.WriteString("}\n")
	sb.WriteString("```\n")
	sb.WriteString("> This pushes the aggregation to the database — no need to paginate and\n")
	sb.WriteString("> aggregate client-side. Use this instead of workflows that fetch all rows.\n\n")

	sb.WriteString("### Default Row Limit\n")
	sb.WriteString(fmt.Sprintf("> **CRITICAL:** Every query level (top-level AND nested) has a default limit of **%d rows**.\n", defaultLimit))
	sb.WriteString("> If you do not specify an explicit `limit`, only the first ")
	sb.WriteString(fmt.Sprintf("%d rows are returned — **silently, with no warning**.\n", defaultLimit))
	sb.WriteString("> Always set an explicit `limit` on every level of your query, especially nested children.\n\n")
	sb.WriteString("```graphql\n")
	sb.WriteString("# BAD — nested salesorderdetail silently capped at ")
	sb.WriteString(fmt.Sprintf("%d rows per parent:\n", defaultLimit))
	sb.WriteString("{ salesorderheader { salesorderdetail { productid orderqty } } }\n\n")
	sb.WriteString("# GOOD — explicit limit on nested children:\n")
	sb.WriteString("{ salesorderheader { salesorderdetail(limit: 100) { productid orderqty } } }\n")
	sb.WriteString("```\n\n")

	sb.WriteString("### Pagination\n")
	sb.WriteString("```graphql\n")
	sb.WriteString("# Limit/offset\n")
	sb.WriteString("{ products(limit: 10, offset: 20) { id name } }\n\n")
	sb.WriteString("# Cursor pagination (preferred for large datasets)\n")
	sb.WriteString("{ products(first: 10, after: $products_cursor) { id name } products_cursor }\n")
	sb.WriteString("# Variables: {\"products_cursor\": null}  — cursor field MUST be at query root level\n")
	sb.WriteString("```\n\n")

	sb.WriteString("### Ordering\n")
	sb.WriteString("```graphql\n")
	sb.WriteString("{ products(order_by: { price: desc }) { id name } }\n")
	sb.WriteString("{ products(order_by: { price: desc, id: asc }) { id name } }  # multiple\n")
	sb.WriteString("{ products(order_by: { owner: { name: asc } }) { id } }       # nested\n")
	sb.WriteString("```\n\n")

	sb.WriteString("### Relationships (automatic via foreign keys)\n")
	sb.WriteString("```graphql\n")
	sb.WriteString("# Parent → children (one-to-many)\n")
	sb.WriteString("{ users { email products { name price } } }\n\n")
	sb.WriteString("# Child → parent (many-to-one)\n")
	sb.WriteString("{ products { name owner { email } } }\n")
	sb.WriteString("```\n\n")

	sb.WriteString("### Common Mistakes\n")
	sb.WriteString("| Wrong | Right | Why |\n")
	sb.WriteString("|-------|-------|-----|\n")
	sb.WriteString("| `group_by: [col]` | `distinct: [col]` | group_by does not exist |\n")
	sb.WriteString("| `{ id: { in: 1 } }` | `{ id: { in: [1] } }` | in/nin need arrays |\n")
	sb.WriteString("| `{ price: { gt: \"50\" } }` | `{ price: { gt: 50 } }` | numeric ops need numbers |\n")
	sb.WriteString("| `{ name: { ilike: \"test\" } }` | `{ name: { ilike: \"%test%\" } }` | ilike needs % wildcards |\n")
	sb.WriteString("| `{ is_active: { eq: \"true\" } }` | `{ is_active: { eq: true } }` | booleans not strings |\n")
	sb.WriteString("| `products(first: 10) { products_cursor }` | `products(first: 10) { id } products_cursor` | cursor at root level |\n")
	sb.WriteString(fmt.Sprintf("| `{ orders { items { id } } }` | `{ orders { items(limit: 100) { id } } }` | nested default is %d — set explicit limit |\n", defaultLimit))
	sb.WriteString("| `order_by: { sum_price: desc }` without `distinct` | `order_by: { sum_price: desc }` with `distinct: [col]` | order_by on aggregations requires distinct grouping |\n")
	sb.WriteString("\n---\n\n")
}

// writeTableIndexEntry writes a compact index entry for a table.
// buildDuplicateIndex returns a map from table name to a list of schemas it appears in.
// Only names that appear in more than one schema are included.
func buildDuplicateIndex(schemas []*core.TableSchema) map[string][]string {
	byName := make(map[string][]string)
	for _, s := range schemas {
		byName[s.Name] = append(byName[s.Name], s.Schema)
	}
	// Remove non-duplicates
	for name, schemaList := range byName {
		if len(schemaList) <= 1 {
			delete(byName, name)
		}
	}
	return byName
}

// duplicateWarning returns a warning string if this table exists in multiple schemas.
func duplicateWarning(schema *core.TableSchema, duplicateSchemas map[string][]string) string {
	schemaList, isDuplicate := duplicateSchemas[schema.Name]
	if !isDuplicate {
		return ""
	}

	hasFKs := false
	for _, col := range schema.Columns {
		if col.ForeignKey != "" {
			hasFKs = true
			break
		}
	}

	if hasFKs {
		return fmt.Sprintf(
			"> **PREFERRED** — this schema has foreign key relationships. Use `@schema(name: \"%s\")` for joins. Also in: %s\n",
			schema.Schema, strings.Join(schemaList, ", "))
	}
	return fmt.Sprintf(
		"> **WARNING: duplicate table** — also exists in schemas: %s. This version has NO foreign keys — joins will not work. Use `@schema(name: ...)` to target the schema with FKs.\n",
		strings.Join(schemaList, ", "))
}

func writeTableIndexEntry(sb *strings.Builder, schema *core.TableSchema, e *tableEnrichment, duplicateSchemas map[string][]string) {
	sb.WriteString(fmt.Sprintf("### %s\n", schema.Name))
	if schema.Comment != "" {
		sb.WriteString(fmt.Sprintf("%s\n", schema.Comment))
	}

	// Meta line: type, schema, rows, PK
	meta := "Type: table"
	if schema.Type != "" {
		meta = fmt.Sprintf("Type: %s", schema.Type)
	}
	if schema.Schema != "" {
		meta += fmt.Sprintf(" | Schema: %s", schema.Schema)
	}
	if e != nil && e.RowCount > 0 {
		meta += fmt.Sprintf(" | Rows: %s", formatCount(e.RowCount))
	}
	if len(schema.PrimaryKeys) > 0 {
		meta += fmt.Sprintf(" | PK: %s", strings.Join(schema.PrimaryKeys, ", "))
	} else if schema.PrimaryKey != "" {
		meta += fmt.Sprintf(" | PK: %s", schema.PrimaryKey)
	}
	sb.WriteString(meta + "\n")

	// Duplicate table warning
	if w := duplicateWarning(schema, duplicateSchemas); w != "" {
		sb.WriteString(w)
	}

	// Foreign keys — critical for understanding joins
	var fks []string
	for _, col := range schema.Columns {
		if col.ForeignKey != "" {
			target := col.ForeignKey
			if col.ForeignKeyDatabase != "" {
				target = col.ForeignKeyDatabase + ":" + target
			}
			fks = append(fks, fmt.Sprintf("%s → %s", col.Name, target))
		}
	}
	if len(fks) > 0 {
		sb.WriteString(fmt.Sprintf("FKs: %s\n", strings.Join(fks, ", ")))
	}

	// Key columns — just names, grouped by role
	var numericCols, dateCols, textCols, otherCols []string
	for _, col := range schema.Columns {
		if col.PrimaryKey || col.ForeignKey != "" {
			continue // already shown in PK/FKs
		}
		name := col.Name
		if isNumericType(col.Type) {
			numericCols = append(numericCols, name)
		} else if isDateType(col.Type) {
			dateCols = append(dateCols, name)
		} else if col.FullText {
			textCols = append(textCols, name+" (fulltext)")
		} else if col.Type == "text" || strings.HasPrefix(col.Type, "character") || strings.HasPrefix(col.Type, "varchar") || strings.HasPrefix(col.Type, "nvarchar") {
			textCols = append(textCols, name)
		} else {
			otherCols = append(otherCols, name)
		}
	}

	var colParts []string
	if len(numericCols) > 0 {
		colParts = append(colParts, fmt.Sprintf("numeric: %s", strings.Join(numericCols, ", ")))
	}
	if len(dateCols) > 0 {
		colParts = append(colParts, fmt.Sprintf("dates: %s", strings.Join(dateCols, ", ")))
	}
	if len(textCols) > 0 {
		colParts = append(colParts, fmt.Sprintf("text: %s", strings.Join(textCols, ", ")))
	}
	if len(otherCols) > 0 {
		colParts = append(colParts, fmt.Sprintf("other: %s", strings.Join(otherCols, ", ")))
	}
	if len(colParts) > 0 {
		sb.WriteString(fmt.Sprintf("Columns: %s\n", strings.Join(colParts, " | ")))
	}

	// Relationships — one-line summary
	allRels := append(schema.Relationships.Outgoing, schema.Relationships.Incoming...)
	if len(allRels) > 0 {
		var rels []string
		for _, rel := range allRels {
			arrow := "→"
			if rel.Type == "one_to_many" {
				arrow = "←"
			}
			rels = append(rels, fmt.Sprintf("%s %s", arrow, rel.Table))
		}
		sb.WriteString(fmt.Sprintf("Joins: %s\n", strings.Join(rels, ", ")))
	}

	sb.WriteString("\n")
}

// writeTableMarkdown writes the full markdown section for a single table.
func writeTableMarkdown(sb *strings.Builder, schema *core.TableSchema, e *tableEnrichment, duplicateSchemas map[string][]string) {
	sb.WriteString(fmt.Sprintf("### %s\n", schema.Name))
	if schema.Comment != "" {
		sb.WriteString(fmt.Sprintf("%s\n", schema.Comment))
	}

	meta := fmt.Sprintf("Type: %s", schema.Type)
	if schema.Type == "" {
		meta = "Type: table"
	}
	if schema.Schema != "" {
		meta += fmt.Sprintf(" | Schema: %s", schema.Schema)
	}
	if e != nil && e.RowCount > 0 {
		meta += fmt.Sprintf(" | Rows: %s", formatCount(e.RowCount))
	}
	if len(schema.PrimaryKeys) > 0 {
		meta += fmt.Sprintf(" | PK: %s", strings.Join(schema.PrimaryKeys, ", "))
	} else if schema.PrimaryKey != "" {
		meta += fmt.Sprintf(" | PK: %s", schema.PrimaryKey)
	}
	sb.WriteString(meta + "\n\n")

	// Duplicate table warning
	if w := duplicateWarning(schema, duplicateSchemas); w != "" {
		sb.WriteString(w + "\n")
	}

	// Columns table
	sb.WriteString("#### Columns\n")
	sb.WriteString("| Column | Type | Nullable | Default | Key | FK | Index | Notes |\n")
	sb.WriteString("|--------|------|----------|---------|-----|----|-------|-------|\n")
	for _, col := range schema.Columns {
		colType := col.Type
		if col.Array {
			colType += "[]"
		}

		nullable := "YES"
		if !col.Nullable {
			nullable = "NO"
		}

		def := col.Default
		if def == "" {
			def = "-"
		}

		key := "-"
		if col.PrimaryKey {
			key = "PK"
		} else if col.UniqueKey {
			key = "UK"
		}

		fk := "-"
		if col.ForeignKey != "" {
			if col.ForeignKeyDatabase != "" {
				fk = col.ForeignKeyDatabase + ":" + col.ForeignKey
			} else {
				fk = col.ForeignKey
			}
		}

		idx := "-"
		if col.Index {
			idx = "YES"
			if col.IndexName != "" {
				idx = col.IndexName
			}
		}

		var notes []string
		if col.FullText {
			notes = append(notes, "fulltext")
		}
		if col.ForeignKeyRecursive {
			notes = append(notes, "recursive")
		}
		noteStr := "-"
		if len(notes) > 0 {
			noteStr = strings.Join(notes, ", ")
		}

		sb.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s | %s | %s | %s |\n",
			col.Name, colType, nullable, def, key, fk, idx, noteStr))
	}
	sb.WriteString("\n")

	// Relationships
	allRels := append(schema.Relationships.Outgoing, schema.Relationships.Incoming...)
	if len(allRels) > 0 {
		sb.WriteString("#### Relationships\n")
		for _, rel := range allRels {
			arrow := "→"
			if rel.Type == "one_to_many" {
				arrow = "←"
			}
			sb.WriteString(fmt.Sprintf("- %s %s (%s via %s)\n",
				arrow, rel.Table, rel.Type, rel.Name))
		}
		sb.WriteString("\n")
	}

	// Aggregations
	var aggs []string
	for _, col := range schema.Columns {
		aggs = append(aggs, fmt.Sprintf("count_%s", col.Name))
	}
	for _, col := range schema.Columns {
		if isNumericType(col.Type) {
			aggs = append(aggs,
				fmt.Sprintf("sum_%s", col.Name),
				fmt.Sprintf("avg_%s", col.Name),
				fmt.Sprintf("min_%s", col.Name),
				fmt.Sprintf("max_%s", col.Name))
		}
		if isDateType(col.Type) {
			aggs = append(aggs,
				fmt.Sprintf("min_%s", col.Name),
				fmt.Sprintf("max_%s", col.Name))
		}
	}
	if len(aggs) > 0 {
		sb.WriteString("#### Aggregations\n")
		sb.WriteString(strings.Join(aggs, ", ") + "\n\n")
	}

	// Full-text search columns
	if len(schema.FullTextColumns) > 0 {
		sb.WriteString(fmt.Sprintf("#### Full-Text Search\n%s\n\n", strings.Join(schema.FullTextColumns, ", ")))
	}

	// Live data profile
	if e != nil {
		hasData := len(e.DateRanges) > 0 || len(e.DistinctValues) > 0 || len(e.ValueStats) > 0 || len(e.SampleRows) > 0
		if hasData {
			sb.WriteString("#### Live Data Profile\n")

			// Date ranges
			for col, rng := range e.DateRanges {
				sb.WriteString(fmt.Sprintf("- **Date range** %s: %s → %s\n", col, rng[0], rng[1]))
			}

			// Distinct values
			for col, vals := range e.DistinctValues {
				sb.WriteString(fmt.Sprintf("- **%s values**: %s\n", col, strings.Join(vals, ", ")))
			}

			// Value stats
			for col, stats := range e.ValueStats {
				sb.WriteString(fmt.Sprintf("- **%s stats**: min %s | max %s | avg %s | sum %s (count: %d)\n",
					col, stats.Min, stats.Max, stats.Avg, stats.Sum, stats.Count))
			}

			// Sample rows
			if len(e.SampleRows) > 0 && len(e.SampleRows[0]) > 0 {
				sb.WriteString("\n**Sample rows** (most recent):\n")
				// Get column order from first row
				var cols []string
				for k := range e.SampleRows[0] {
					cols = append(cols, k)
				}
				sort.Strings(cols)

				// Header
				sb.WriteString("| " + strings.Join(cols, " | ") + " |\n")
				sb.WriteString("|" + strings.Repeat("------|", len(cols)) + "\n")
				// Rows
				for _, row := range e.SampleRows {
					var vals []string
					for _, c := range cols {
						vals = append(vals, fmt.Sprintf("%v", row[c]))
					}
					sb.WriteString("| " + strings.Join(vals, " | ") + " |\n")
				}
			}
			sb.WriteString("\n")
		}
	}

	sb.WriteString("---\n\n")
}

// writeDuplicateTableWarnings detects tables that appear in multiple schemas and
// warns agents about which schema has foreign keys (real table) vs which doesn't
// (likely a view). This prevents agents from querying FK-less views by default.
func writeDuplicateTableWarnings(sb *strings.Builder, schemas []*core.TableSchema) {
	// Group tables by name
	byName := make(map[string][]*core.TableSchema)
	for _, s := range schemas {
		byName[s.Name] = append(byName[s.Name], s)
	}

	// Find names that appear in multiple schemas
	type duplicate struct {
		name    string
		entries []*core.TableSchema
	}
	var duplicates []duplicate
	for name, entries := range byName {
		if len(entries) > 1 {
			duplicates = append(duplicates, duplicate{name: name, entries: entries})
		}
	}
	if len(duplicates) == 0 {
		return
	}

	sort.Slice(duplicates, func(i, j int) bool {
		return duplicates[i].name < duplicates[j].name
	})

	sb.WriteString("## Duplicate Tables Across Schemas\n\n")
	sb.WriteString("> **WARNING:** The following tables exist in multiple schemas. ")
	sb.WriteString("When a table appears in more than one schema, the default schema may resolve to a ")
	sb.WriteString("**view** with no foreign key relationships. Use `@schema(name: \"...\")` to target ")
	sb.WriteString("the correct schema.\n\n")

	sb.WriteString("| Table | Schema | Has FKs | Has Relationships | Recommendation |\n")
	sb.WriteString("|-------|--------|---------|-------------------|----------------|\n")

	for _, dup := range duplicates {
		// Find which entry has FKs and relationships
		for _, entry := range dup.entries {
			hasFKs := false
			for _, col := range entry.Columns {
				if col.ForeignKey != "" {
					hasFKs = true
					break
				}
			}
			hasRels := len(entry.Relationships.Outgoing) > 0 || len(entry.Relationships.Incoming) > 0
			rec := ""
			if hasFKs && hasRels {
				rec = fmt.Sprintf("Use `@schema(name: \"%s\")`", entry.Schema)
			} else if !hasFKs && !hasRels {
				rec = "Avoid — no FK joins possible"
			}
			sb.WriteString(fmt.Sprintf("| %s | %s | %v | %v | %s |\n",
				entry.Name, entry.Schema, hasFKs, hasRels, rec))
		}
	}
	sb.WriteString("\n")
}

// writeRelationshipPaths writes relationship paths between hub tables.
func writeRelationshipPaths(sb *strings.Builder, gj *core.GraphJin, database string, schemas []*core.TableSchema) {
	// Find tables with FKs (hub tables)
	type tableWithFKs struct {
		name    string
		fkCount int
	}
	var hubs []tableWithFKs
	for _, s := range schemas {
		fkCount := 0
		for _, col := range s.Columns {
			if col.ForeignKey != "" {
				fkCount++
			}
		}
		if fkCount > 0 {
			hubs = append(hubs, tableWithFKs{s.Name, fkCount})
		}
	}
	sort.Slice(hubs, func(i, j int) bool { return hubs[i].fkCount > hubs[j].fkCount })

	if len(hubs) == 0 {
		return
	}

	sb.WriteString("## Relationship Paths\n\n")

	// Find paths between top hub tables (limit to avoid explosion)
	limit := 10
	if len(hubs) < limit {
		limit = len(hubs)
	}
	pathsWritten := 0
	for i := 0; i < limit && pathsWritten < 20; i++ {
		for j := i + 1; j < limit && pathsWritten < 20; j++ {
			paths, err := gj.FindRelationshipPathForDatabase(database, hubs[i].name, hubs[j].name)
			if err != nil || len(paths) == 0 {
				continue
			}
			var steps []string
			for _, p := range paths {
				step := fmt.Sprintf("%s → %s (%s", p.From, p.To, p.Relation)
				if p.Via != "" {
					step += " via " + p.Via
				}
				step += ")"
				steps = append(steps, step)
			}
			sb.WriteString(fmt.Sprintf("- %s ↔ %s: %s\n", hubs[i].name, hubs[j].name, strings.Join(steps, " → ")))
			pathsWritten++
		}
	}
	sb.WriteString("\n")
}

// writeNamespaceRouting writes the namespace/database routing section.
func writeNamespaceRouting(sb *strings.Builder, gj *core.GraphJin) {
	names := gj.DatabaseNames()
	if len(names) <= 1 {
		return
	}

	defaultDB := gj.DefaultDatabase()

	sb.WriteString("## Namespace Routing\n\n")
	for _, name := range names {
		isDefault := ""
		if name == defaultDB {
			isDefault = " **(default)**"
		}
		tables := gj.GetTablesForDatabase(name)
		tableCount := len(tables)
		sb.WriteString(fmt.Sprintf("- `%s`: %d tables%s\n", name, tableCount, isDefault))
	}
	sb.WriteString("\n")
}

// writeQueryTemplates generates query templates based on schema patterns.
func writeQueryTemplates(sb *strings.Builder, schemas []*core.TableSchema, enrichment map[string]*tableEnrichment) {
	sb.WriteString("## Query Templates\n\n")
	templatesWritten := 0

	for _, schema := range schemas {
		if templatesWritten >= 15 {
			break
		}

		var dateCols, numericCols, enumCols []core.ColumnInfo
		for _, col := range schema.Columns {
			if isDateType(col.Type) {
				dateCols = append(dateCols, col)
			}
			if isNumericType(col.Type) && !col.PrimaryKey && !strings.HasSuffix(col.Name, "_id") {
				numericCols = append(numericCols, col)
			}
			if isEnumCandidateCol(col) {
				enumCols = append(enumCols, col)
			}
		}

		hasFKs := false
		var fkCols []core.ColumnInfo
		for _, col := range schema.Columns {
			if col.ForeignKey != "" {
				hasFKs = true
				fkCols = append(fkCols, col)
			}
		}

		// Time-series template: table with timestamp + numeric
		if len(dateCols) > 0 && len(numericCols) > 0 && templatesWritten < 15 {
			dc := dateCols[0]
			var aggFields []string
			for _, nc := range numericCols {
				aggFields = append(aggFields, fmt.Sprintf("sum_%s", nc.Name))
				if len(aggFields) >= 4 {
					break
				}
			}
			pkName := schema.PrimaryKey
			if pkName == "" && len(schema.Columns) > 0 {
				pkName = schema.Columns[0].Name
			}
			aggFields = append(aggFields, "count_"+pkName)

			sb.WriteString(fmt.Sprintf("### Time-series: %s by %s\n", schema.Name, dc.Name))
			sb.WriteString("```graphql\n")
			sb.WriteString(fmt.Sprintf("{\n  %s(\n    where: { %s: { gte: \"$START_DATE\" } }\n    distinct: [%s]\n    order_by: { %s: asc }\n    limit: 100\n  ) {\n    %s\n    %s\n  }\n}\n",
				schema.Name, dc.Name, dc.Name, dc.Name, dc.Name, strings.Join(aggFields, "\n    ")))
			sb.WriteString("```\n\n")
			templatesWritten++
		}

		// Breakdown template: table with enum/status column
		if len(enumCols) > 0 && templatesWritten < 15 {
			ec := enumCols[0]
			countField := "count_" + schema.PrimaryKey
			if schema.PrimaryKey == "" {
				if len(schema.Columns) > 0 {
					countField = "count_" + schema.Columns[0].Name
				}
			}

			sb.WriteString(fmt.Sprintf("### Breakdown: %s by %s\n", schema.Name, ec.Name))
			sb.WriteString("```graphql\n")
			sb.WriteString(fmt.Sprintf("{\n  %s(distinct: [%s]) {\n    %s\n    %s\n  }\n}\n",
				schema.Name, ec.Name, ec.Name, countField))
			sb.WriteString("```\n\n")
			templatesWritten++
		}

		// Join template: table with FKs
		if hasFKs && templatesWritten < 15 {
			fk := fkCols[0]
			// Extract target table from ForeignKey string "table.col"
			fkTarget := fk.ForeignKey
			if idx := strings.Index(fkTarget, "."); idx >= 0 {
				fkTarget = fkTarget[:idx]
			}

			sb.WriteString(fmt.Sprintf("### Join: %s with %s\n", schema.Name, fkTarget))
			sb.WriteString("```graphql\n")

			// Build field list for parent
			var parentFields []string
			if schema.PrimaryKey != "" {
				parentFields = append(parentFields, schema.PrimaryKey)
			}
			for _, col := range schema.Columns {
				if !col.PrimaryKey && col.ForeignKey == "" && len(parentFields) < 4 {
					parentFields = append(parentFields, col.Name)
				}
			}

			sb.WriteString(fmt.Sprintf("{\n  %s(limit: 10) {\n    %s\n    %s {\n      id\n    }\n  }\n}\n",
				schema.Name, strings.Join(parentFields, "\n    "), fkTarget))
			sb.WriteString("```\n\n")
			templatesWritten++
		}
	}
}

// writeDataQuality writes data quality flags.
func writeDataQuality(sb *strings.Builder, schemas []*core.TableSchema, enrichment map[string]*tableEnrichment) {
	var flags []string

	for _, schema := range schemas {
		e := enrichment[schema.Name]
		if e == nil {
			continue
		}

		// Flag nullable columns
		for _, col := range schema.Columns {
			if col.Nullable && !col.PrimaryKey {
				flags = append(flags, fmt.Sprintf("- `%s.%s`: nullable", schema.Name, col.Name))
			}
		}

		// Flag enum columns with very few distinct values
		for col, vals := range e.DistinctValues {
			if len(vals) <= 2 {
				flags = append(flags, fmt.Sprintf("- `%s.%s`: only %d distinct values (%s)",
					schema.Name, col, len(vals), strings.Join(vals, ", ")))
			}
		}
	}

	if len(flags) > 0 {
		sb.WriteString("## Data Quality\n\n")
		// Limit to avoid massive output
		if len(flags) > 50 {
			flags = flags[:50]
			flags = append(flags, "- ... (truncated)")
		}
		for _, f := range flags {
			sb.WriteString(f + "\n")
		}
		sb.WriteString("\n")
	}
}

// writeFunctions writes database functions section.
func writeFunctions(sb *strings.Builder, functions []core.FunctionInfo) {
	if len(functions) == 0 {
		return
	}

	sb.WriteString("## Functions\n\n")
	sb.WriteString("| Function | Schema | Type | Aggregate | Inputs | Outputs |\n")
	sb.WriteString("|----------|--------|------|-----------|--------|---------|\n")

	for _, fn := range functions {
		var inputs, outputs []string
		for _, p := range fn.Inputs {
			pType := p.Type
			if p.Array {
				pType += "[]"
			}
			inputs = append(inputs, fmt.Sprintf("%s %s", p.Name, pType))
		}
		for _, p := range fn.Outputs {
			pType := p.Type
			if p.Array {
				pType += "[]"
			}
			outputs = append(outputs, fmt.Sprintf("%s %s", p.Name, pType))
		}

		agg := "NO"
		if fn.Aggregate {
			agg = "YES"
		}

		sb.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s | %s |\n",
			fn.Name, fn.Schema, fn.Type, agg,
			strings.Join(inputs, ", "), strings.Join(outputs, ", ")))
	}
	sb.WriteString("\n")
}

// --- Type classification helpers ---

func isNumericType(colType string) bool {
	t := strings.ToLower(colType)
	return strings.Contains(t, "int") ||
		strings.Contains(t, "serial") ||
		strings.Contains(t, "decimal") ||
		strings.Contains(t, "numeric") ||
		strings.Contains(t, "number") ||
		strings.Contains(t, "float") ||
		strings.Contains(t, "double") ||
		strings.Contains(t, "real") ||
		strings.Contains(t, "money")
}

func isDateType(colType string) bool {
	t := strings.ToLower(colType)
	return strings.Contains(t, "timestamp") ||
		strings.Contains(t, "date") ||
		strings.Contains(t, "time")
}

func isEnumCandidateCol(col core.ColumnInfo) bool {
	if col.PrimaryKey || col.ForeignKey != "" {
		return false
	}
	name := strings.ToLower(col.Name)
	enumKeywords := []string{"status", "state", "type", "category", "kind", "role",
		"stage", "priority", "level", "grade", "tier", "plan", "mode"}
	for _, kw := range enumKeywords {
		if strings.Contains(name, kw) {
			return true
		}
	}
	// Short varchar/text types
	t := strings.ToLower(col.Type)
	if (strings.Contains(t, "varchar") || strings.Contains(t, "char")) && !strings.Contains(t, "text") {
		return true
	}
	return false
}

// --- Result extraction helpers ---

func extractCountFromResult(data json.RawMessage, tableName, colName string) int64 {
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return 0
	}
	tableData, ok := parsed[tableName]
	if !ok {
		return 0
	}

	switch v := tableData.(type) {
	case []any:
		if len(v) > 0 {
			if row, ok := v[0].(map[string]any); ok {
				if count, ok := row["count_"+colName]; ok {
					return toInt64(count)
				}
			}
		}
	case map[string]any:
		if count, ok := v["count_"+colName]; ok {
			return toInt64(count)
		}
	}
	return 0
}

func extractMinMaxFromResult(data json.RawMessage, tableName, colName string) (string, string) {
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return "", ""
	}
	row := extractFirstRow(parsed, tableName)
	if row == nil {
		return "", ""
	}
	minVal := fmt.Sprintf("%v", row["min_"+colName])
	maxVal := fmt.Sprintf("%v", row["max_"+colName])
	if minVal == "<nil>" {
		minVal = ""
	}
	if maxVal == "<nil>" {
		maxVal = ""
	}
	return minVal, maxVal
}

func extractDistinctFromResult(data json.RawMessage, tableName, colName string) []string {
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil
	}
	tableData, ok := parsed[tableName]
	if !ok {
		return nil
	}
	rows, ok := tableData.([]any)
	if !ok {
		return nil
	}
	var vals []string
	for _, r := range rows {
		if row, ok := r.(map[string]any); ok {
			if v, ok := row[colName]; ok && v != nil {
				vals = append(vals, fmt.Sprintf("%v", v))
			}
		}
	}
	return vals
}

func extractStatsFromResult(data json.RawMessage, tableName, colName string) numStats {
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return numStats{}
	}
	row := extractFirstRow(parsed, tableName)
	if row == nil {
		return numStats{}
	}
	return numStats{
		Min:   fmtVal(row["min_"+colName]),
		Max:   fmtVal(row["max_"+colName]),
		Avg:   fmtVal(row["avg_"+colName]),
		Sum:   fmtVal(row["sum_"+colName]),
		Count: toInt64(row["count_"+colName]),
	}
}

func extractRowsFromResult(data json.RawMessage, tableName string) []map[string]any {
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil
	}
	tableData, ok := parsed[tableName]
	if !ok {
		return nil
	}
	rows, ok := tableData.([]any)
	if !ok {
		return nil
	}
	var result []map[string]any
	for _, r := range rows {
		if row, ok := r.(map[string]any); ok {
			result = append(result, row)
		}
	}
	return result
}

func extractFirstRow(parsed map[string]any, tableName string) map[string]any {
	tableData, ok := parsed[tableName]
	if !ok {
		return nil
	}
	switch v := tableData.(type) {
	case []any:
		if len(v) > 0 {
			if row, ok := v[0].(map[string]any); ok {
				return row
			}
		}
	case map[string]any:
		return v
	}
	return nil
}

func toInt64(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return i
		}
	}
	return 0
}

func fmtVal(v any) string {
	if v == nil {
		return "-"
	}
	return fmt.Sprintf("%v", v)
}

func formatCount(n int64) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}
