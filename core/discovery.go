package core

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

// DiscoveryDocument holds a generated schema Bible for a database.
type DiscoveryDocument struct {
	Database    string    `json:"database"`
	Hash        string    `json:"hash"`
	GeneratedAt time.Time `json:"generated_at"`
	Markdown    string    `json:"content"`
}

// GetDiscovery returns the cached discovery document for a database.
// Returns nil if no document has been generated yet.
func (g *GraphJin) GetDiscovery(database string) *DiscoveryDocument {
	if v, ok := g.discovery.Load(database); ok {
		return v.(*DiscoveryDocument)
	}
	return nil
}

// GetAllDiscovery returns discovery documents for all databases.
func (g *GraphJin) GetAllDiscovery() []*DiscoveryDocument {
	var docs []*DiscoveryDocument
	g.discovery.Range(func(key, value any) bool {
		docs = append(docs, value.(*DiscoveryDocument))
		return true
	})
	return docs
}

// GetCombinedDiscovery returns a single combined markdown Bible covering all databases.
// This is the primary method consumers should use.
func (g *GraphJin) GetCombinedDiscovery() string {
	gj, err := g.getEngine()
	if err != nil {
		return ""
	}

	var sb strings.Builder
	names := gj.sortedDatabaseNames()

	for _, name := range names {
		if v, ok := g.discovery.Load(name); ok {
			doc := v.(*DiscoveryDocument)
			sb.WriteString(doc.Markdown)
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

// GenerateDiscovery generates (or regenerates) the discovery document for a database.
// It builds Layer 1 (raw schema), Layer 3 (live enrichment), and deterministic Layer 2
// (query templates, namespace routing, data quality flags).
// The context is used for executing Layer 3 enrichment queries.
func (g *GraphJin) GenerateDiscovery(ctx context.Context, database string) (*DiscoveryDocument, error) {
	gj, err := g.getEngine()
	if err != nil {
		return nil, err
	}

	dbCtx, ok := gj.databases[database]
	if !ok {
		return nil, fmt.Errorf("database not found: %s", database)
	}
	if dbCtx.schema == nil {
		return nil, fmt.Errorf("database %s: schema not ready", database)
	}

	tables := dbCtx.schema.GetTables()
	var visibleTables []sdata.DBTable
	for _, t := range tables {
		if t.Type != "virtual" && !t.Blocked {
			visibleTables = append(visibleTables, t)
		}
	}

	// Sort tables by name for deterministic output
	sort.Slice(visibleTables, func(i, j int) bool {
		return visibleTables[i].Name < visibleTables[j].Name
	})

	// Build Layer 3 enrichment data
	enrichment := g.buildEnrichment(ctx, database, visibleTables)

	// Generate markdown
	var sb strings.Builder

	totalRows := int64(0)
	for _, e := range enrichment {
		totalRows += e.RowCount
	}

	hash := fmt.Sprintf("%x", dbCtx.dbinfo.Hash())
	now := time.Now().UTC()

	// Header
	sb.WriteString(fmt.Sprintf("# Schema Bible: %s\n", database))
	sb.WriteString(fmt.Sprintf("> Generated: %s | Hash: %s | Type: %s | Tables: %d | Rows: ~%s\n\n",
		now.Format(time.RFC3339), hash, dbCtx.dbtype, len(visibleTables), formatCount(totalRows)))

	// Table of contents
	writeTableOfContents(&sb, visibleTables, len(gj.databases) > 1, len(dbCtx.schema.GetFunctions()) > 0)

	// Query syntax cheat sheet (so agents know the DSL)
	writeQuerySyntaxReference(&sb)

	// Tables section
	sb.WriteString("## Tables\n\n")
	for _, t := range visibleTables {
		g.writeTableMarkdown(&sb, dbCtx.schema, database, t, enrichment[t.Name])
	}

	// Relationship paths between key tables
	g.writeRelationshipPaths(&sb, dbCtx.schema, visibleTables)

	// Namespace routing
	g.writeNamespaceRouting(&sb, gj)

	// Query templates
	g.writeQueryTemplates(&sb, visibleTables, enrichment)

	// Data quality summary
	g.writeDataQuality(&sb, visibleTables, enrichment)

	// Functions
	g.writeFunctions(&sb, dbCtx.schema)

	doc := &DiscoveryDocument{
		Database:    database,
		Hash:        hash,
		GeneratedAt: now,
		Markdown:    sb.String(),
	}

	g.discovery.Store(database, doc)
	return doc, nil
}

// tableEnrichment holds Layer 3 live data for a table.
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
func (g *GraphJin) buildEnrichment(ctx context.Context, database string, tables []sdata.DBTable) map[string]*tableEnrichment {
	result := make(map[string]*tableEnrichment)

	for _, t := range tables {
		e := &tableEnrichment{
			DateRanges:     make(map[string][2]string),
			DistinctValues: make(map[string][]string),
			ValueStats:     make(map[string]numStats),
		}

		// Identify column types
		var numericCols, dateCols, enumCols []sdata.DBColumn
		var allColNames []string
		for _, col := range t.Columns {
			allColNames = append(allColNames, col.Name)
			if isNumericType(col.Type) && !col.PrimaryKey && !strings.HasSuffix(col.Name, "_id") {
				numericCols = append(numericCols, col)
			}
			if isDateType(col.Type) {
				dateCols = append(dateCols, col)
			}
			if isEnumCandidate(col) {
				enumCols = append(enumCols, col)
			}
		}

		rc := &RequestConfig{}
		rc.SetNamespace(database)

		// Row count
		if t.PrimaryCol.Name != "" {
			q := fmt.Sprintf("{ %s(limit: 1) { count_%s } }", t.Name, t.PrimaryCol.Name)
			if res, err := g.GraphQL(ctx, q, nil, rc); err == nil && res.Data != nil {
				e.RowCount = extractCountFromResult(res.Data, t.Name, t.PrimaryCol.Name)
			}
		}

		// Date ranges
		for _, col := range dateCols {
			q := fmt.Sprintf("{ %s(limit: 1) { min_%s max_%s } }", t.Name, col.Name, col.Name)
			if res, err := g.GraphQL(ctx, q, nil, rc); err == nil && res.Data != nil {
				min, max := extractMinMaxFromResult(res.Data, t.Name, col.Name)
				if min != "" || max != "" {
					e.DateRanges[col.Name] = [2]string{min, max}
				}
			}
		}

		// Distinct values for enum columns
		for _, col := range enumCols {
			q := fmt.Sprintf("{ %s(distinct: [%s], limit: 50) { %s } }", t.Name, col.Name, col.Name)
			if res, err := g.GraphQL(ctx, q, nil, rc); err == nil && res.Data != nil {
				vals := extractDistinctFromResult(res.Data, t.Name, col.Name)
				if len(vals) > 0 {
					e.DistinctValues[col.Name] = vals
				}
			}
		}

		// Value stats for numeric columns
		for _, col := range numericCols {
			q := fmt.Sprintf("{ %s(limit: 1) { min_%s max_%s avg_%s sum_%s count_%s } }",
				t.Name, col.Name, col.Name, col.Name, col.Name, col.Name)
			if res, err := g.GraphQL(ctx, q, nil, rc); err == nil && res.Data != nil {
				e.ValueStats[col.Name] = extractStatsFromResult(res.Data, t.Name, col.Name)
			}
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
		q := fmt.Sprintf("{ %s(limit: 5%s) { %s } }", t.Name, orderClause, strings.Join(sampleCols, " "))
		if res, err := g.GraphQL(ctx, q, nil, rc); err == nil && res.Data != nil {
			e.SampleRows = extractRowsFromResult(res.Data, t.Name)
		}

		result[t.Name] = e
	}

	return result
}

// writeTableOfContents writes a navigable index of the discovery document sections.
func writeTableOfContents(sb *strings.Builder, tables []sdata.DBTable, multiDB bool, hasFunctions bool) {
	sb.WriteString("## Table of Contents\n\n")
	sb.WriteString("- [Query Syntax Reference](#query-syntax-reference)\n")
	sb.WriteString(fmt.Sprintf("- [Tables](#tables) (%d tables)\n", len(tables)))
	for _, t := range tables {
		sb.WriteString(fmt.Sprintf("  - [%s](#%s)\n", t.Name, t.Name))
	}
	sb.WriteString("- [Relationship Paths](#relationship-paths)\n")
	if multiDB {
		sb.WriteString("- [Namespace Routing](#namespace-routing)\n")
	}
	sb.WriteString("- [Query Templates](#query-templates)\n")
	sb.WriteString("- [Data Quality](#data-quality)\n")
	if hasFunctions {
		sb.WriteString("- [Functions](#functions)\n")
	}
	sb.WriteString("\n")
}

// writeQuerySyntaxReference writes the GraphJin DSL cheat sheet into the discovery document.
func writeQuerySyntaxReference(sb *strings.Builder) {
	sb.WriteString("## Query Syntax Reference\n\n")

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
	sb.WriteString("\n---\n\n")
}

// writeTableMarkdown writes the markdown section for a single table.
func (g *GraphJin) writeTableMarkdown(sb *strings.Builder, schema *sdata.DBSchema, dbName string, t sdata.DBTable, e *tableEnrichment) {
	sb.WriteString(fmt.Sprintf("### %s\n", t.Name))
	if t.Comment != "" {
		sb.WriteString(fmt.Sprintf("%s\n", t.Comment))
	}

	meta := fmt.Sprintf("Type: %s", t.Type)
	if t.Type == "" {
		meta = "Type: table"
	}
	if t.Schema != "" {
		meta += fmt.Sprintf(" | Schema: %s", t.Schema)
	}
	if e != nil && e.RowCount > 0 {
		meta += fmt.Sprintf(" | Rows: %s", formatCount(e.RowCount))
	}
	if t.PrimaryCol.Name != "" {
		meta += fmt.Sprintf(" | PK: %s", t.PrimaryCol.Name)
	}
	sb.WriteString(meta + "\n\n")

	// Columns table
	sb.WriteString("#### Columns\n")
	sb.WriteString("| Column | Type | Nullable | Default | Key | FK | Index | Notes |\n")
	sb.WriteString("|--------|------|----------|---------|-----|----|-------|-------|\n")
	for _, col := range t.Columns {
		colType := col.Type
		if col.Array {
			colType += "[]"
		}

		nullable := "YES"
		if col.NotNull {
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
		if col.FKeyTable != "" {
			if col.FKeyDatabase != "" {
				fk = fmt.Sprintf("%s:%s.%s", col.FKeyDatabase, col.FKeyTable, col.FKeyCol)
			} else {
				fk = fmt.Sprintf("%s.%s", col.FKeyTable, col.FKeyCol)
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
		if col.FKRecursive {
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
	firstDegree, err := schema.GetFirstDegree(t)
	if err == nil && len(firstDegree) > 0 {
		sb.WriteString("#### Relationships\n")
		for _, rel := range firstDegree {
			arrow := "→"
			if rel.Type == sdata.RelOneToMany {
				arrow = "←"
			}
			sb.WriteString(fmt.Sprintf("- %s %s (%s via %s)\n",
				arrow, rel.Table.Name, relTypeToString(rel.Type), rel.Name))
		}
		sb.WriteString("\n")
	}

	// Aggregations
	var aggs []string
	for _, col := range t.Columns {
		aggs = append(aggs, fmt.Sprintf("count_%s", col.Name))
	}
	for _, col := range t.Columns {
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
	if len(t.FullText) > 0 {
		var ftCols []string
		for _, col := range t.FullText {
			ftCols = append(ftCols, col.Name)
		}
		sb.WriteString(fmt.Sprintf("#### Full-Text Search\n%s\n\n", strings.Join(ftCols, ", ")))
	}

	// Live data profile (Layer 3)
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

// writeRelationshipPaths writes relationship paths between hub tables.
func (g *GraphJin) writeRelationshipPaths(sb *strings.Builder, schema *sdata.DBSchema, tables []sdata.DBTable) {
	// Find tables with FKs (hub tables)
	type tableWithFKs struct {
		t       sdata.DBTable
		fkCount int
	}
	var hubs []tableWithFKs
	for _, t := range tables {
		fkCount := 0
		for _, col := range t.Columns {
			if col.FKeyTable != "" {
				fkCount++
			}
		}
		if fkCount > 0 {
			hubs = append(hubs, tableWithFKs{t, fkCount})
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
			paths, err := schema.FindPath(hubs[i].t.Name, hubs[j].t.Name, "")
			if err != nil || len(paths) == 0 {
				continue
			}
			var steps []string
			for _, p := range paths {
				steps = append(steps, fmt.Sprintf("%s.%s → %s.%s (%s)",
					p.LT.Name, p.LC.Name, p.RT.Name, p.RC.Name, relTypeToString(p.Rel)))
			}
			sb.WriteString(fmt.Sprintf("- %s ↔ %s: %s\n", hubs[i].t.Name, hubs[j].t.Name, strings.Join(steps, " → ")))
			pathsWritten++
		}
	}
	sb.WriteString("\n")
}

// writeNamespaceRouting writes the namespace/database routing section.
func (g *GraphJin) writeNamespaceRouting(sb *strings.Builder, gj *graphjinEngine) {
	if len(gj.databases) <= 1 {
		return
	}

	sb.WriteString("## Namespace Routing\n\n")
	for _, name := range gj.sortedDatabaseNames() {
		ctx := gj.databases[name]
		isDefault := ""
		if name == gj.defaultDB {
			isDefault = " **(default)**"
		}
		tableCount := 0
		if ctx.schema != nil {
			tableCount = len(ctx.schema.GetTables())
		}
		sb.WriteString(fmt.Sprintf("- `%s`: %s, %d tables%s\n", name, ctx.dbtype, tableCount, isDefault))
	}
	sb.WriteString("\n")
}

// writeQueryTemplates generates query templates based on schema patterns.
func (g *GraphJin) writeQueryTemplates(sb *strings.Builder, tables []sdata.DBTable, enrichment map[string]*tableEnrichment) {
	sb.WriteString("## Query Templates\n\n")
	templatesWritten := 0

	for _, t := range tables {
		if templatesWritten >= 15 {
			break
		}

		var dateCols, numericCols, enumCols []sdata.DBColumn
		for _, col := range t.Columns {
			if isDateType(col.Type) {
				dateCols = append(dateCols, col)
			}
			if isNumericType(col.Type) && !col.PrimaryKey && !strings.HasSuffix(col.Name, "_id") {
				numericCols = append(numericCols, col)
			}
			if isEnumCandidate(col) {
				enumCols = append(enumCols, col)
			}
		}

		hasFKs := false
		var fkCols []sdata.DBColumn
		for _, col := range t.Columns {
			if col.FKeyTable != "" {
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
			aggFields = append(aggFields, "count_"+t.PrimaryCol.Name)

			sb.WriteString(fmt.Sprintf("### Time-series: %s by %s\n", t.Name, dc.Name))
			sb.WriteString("```graphql\n")
			sb.WriteString(fmt.Sprintf("{\n  %s(\n    where: { %s: { gte: \"$START_DATE\" } }\n    distinct: [%s]\n    order_by: { %s: asc }\n    limit: 100\n  ) {\n    %s\n    %s\n  }\n}\n",
				t.Name, dc.Name, dc.Name, dc.Name, dc.Name, strings.Join(aggFields, "\n    ")))
			sb.WriteString("```\n\n")
			templatesWritten++
		}

		// Breakdown template: table with enum/status column
		if len(enumCols) > 0 && templatesWritten < 15 {
			ec := enumCols[0]
			countField := "count_" + t.PrimaryCol.Name
			if t.PrimaryCol.Name == "" {
				countField = "count_" + t.Columns[0].Name
			}

			sb.WriteString(fmt.Sprintf("### Breakdown: %s by %s\n", t.Name, ec.Name))
			sb.WriteString("```graphql\n")
			sb.WriteString(fmt.Sprintf("{\n  %s(distinct: [%s]) {\n    %s\n    %s\n  }\n}\n",
				t.Name, ec.Name, ec.Name, countField))
			sb.WriteString("```\n\n")
			templatesWritten++
		}

		// Join template: table with FKs
		if hasFKs && templatesWritten < 15 {
			fk := fkCols[0]
			sb.WriteString(fmt.Sprintf("### Join: %s with %s\n", t.Name, fk.FKeyTable))
			sb.WriteString("```graphql\n")

			// Build field list for parent
			var parentFields []string
			parentFields = append(parentFields, t.PrimaryCol.Name)
			for _, col := range t.Columns {
				if !col.PrimaryKey && col.FKeyTable == "" && len(parentFields) < 4 {
					parentFields = append(parentFields, col.Name)
				}
			}

			sb.WriteString(fmt.Sprintf("{\n  %s(limit: 10) {\n    %s\n    %s {\n      id\n    }\n  }\n}\n",
				t.Name, strings.Join(parentFields, "\n    "), fk.FKeyTable))
			sb.WriteString("```\n\n")
			templatesWritten++
		}
	}
}

// writeDataQuality writes data quality flags.
func (g *GraphJin) writeDataQuality(sb *strings.Builder, tables []sdata.DBTable, enrichment map[string]*tableEnrichment) {
	var flags []string

	for _, t := range tables {
		e := enrichment[t.Name]
		if e == nil {
			continue
		}

		// Flag nullable columns
		for _, col := range t.Columns {
			if !col.NotNull && !col.PrimaryKey {
				flags = append(flags, fmt.Sprintf("- `%s.%s`: nullable", t.Name, col.Name))
			}
		}

		// Flag enum columns with very few distinct values
		for col, vals := range e.DistinctValues {
			if len(vals) <= 2 {
				flags = append(flags, fmt.Sprintf("- `%s.%s`: only %d distinct values (%s)",
					t.Name, col, len(vals), strings.Join(vals, ", ")))
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
func (g *GraphJin) writeFunctions(sb *strings.Builder, schema *sdata.DBSchema) {
	funcs := schema.GetFunctions()
	if len(funcs) == 0 {
		return
	}

	sb.WriteString("## Functions\n\n")
	sb.WriteString("| Function | Schema | Type | Aggregate | Inputs | Outputs |\n")
	sb.WriteString("|----------|--------|------|-----------|--------|---------|\n")

	for _, fn := range funcs {
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
		if fn.Agg {
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

func isEnumCandidate(col sdata.DBColumn) bool {
	if col.PrimaryKey || col.FKeyTable != "" {
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

// DiscoverySubscription receives discovery document updates when the schema changes.
type DiscoverySubscription struct {
	Result   chan *DiscoveryDocument
	done     chan struct{}
	database string
}

// Unsubscribe stops receiving discovery updates and cleans up resources.
func (ds *DiscoverySubscription) Unsubscribe() {
	select {
	case <-ds.done:
	default:
		close(ds.done)
	}
}

// SubscribeDiscovery returns a subscription that receives the full discovery document
// whenever the schema changes. The initial document is sent immediately.
// If database is empty, updates for all databases are sent.
func (g *GraphJin) SubscribeDiscovery(ctx context.Context, database string) (*DiscoverySubscription, error) {
	ds := &DiscoverySubscription{
		Result:   make(chan *DiscoveryDocument, 4),
		done:     make(chan struct{}),
		database: database,
	}

	// Send initial document
	if database != "" {
		doc, err := g.GenerateDiscovery(ctx, database)
		if err != nil {
			return nil, err
		}
		ds.Result <- doc
	} else {
		gj, err := g.getEngine()
		if err != nil {
			return nil, err
		}
		for _, name := range gj.sortedDatabaseNames() {
			if gj.databases[name].schema != nil {
				doc, err := g.GenerateDiscovery(ctx, name)
				if err != nil {
					continue
				}
				ds.Result <- doc
			}
		}
	}

	// Register callback for future schema changes
	g.OnSchemaChange(func(dbName string, hash string) {
		select {
		case <-ds.done:
			return
		default:
		}

		if database != "" && dbName != database {
			return
		}

		doc, err := g.GenerateDiscovery(ctx, dbName)
		if err != nil {
			return
		}

		select {
		case ds.Result <- doc:
		case <-ds.done:
		default:
			// Drop if channel is full to avoid blocking
		}
	})

	return ds, nil
}
