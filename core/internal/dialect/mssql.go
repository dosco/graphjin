package dialect

import (
	"fmt"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/graph"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

// MSSQLDialect implements the Dialect interface for Microsoft SQL Server.
// Target version: SQL Server 2017+ (for STRING_AGG and JSON support)
//
// # Working Features
//
// Basic queries work including:
//   - Simple queries with limit, offset, order_by, distinct
//   - Parent-child and child-parent relationships
//   - Many-to-many via join tables (partial support)
//   - Aggregation (count, sum, avg, etc.)
//   - Fragments
//   - WHERE clauses with simple conditions
//   - Remote API joins
//   - Query caching (APQ)
//   - Allow lists and namespace support
//
// ## Basic Mutations
// Simple INSERT/UPDATE/DELETE mutations work:
//   - Single table inserts with SCOPE_IDENTITY() for auto-generated IDs
//   - Inserts with explicit ID values
//   - Transaction support
//   - Inline bulk inserts
//
// # Known Limitations
//
// The following features are not yet fully implemented for MSSQL:
//
// ## Nested/Related Table Mutations
// Mutations involving related tables fail with "t.id could not be bound".
// The table alias reference pattern used for nested inserts needs MSSQL-specific handling.
//
// ## Functions
// Table-returning functions and field functions are not discovered from schema.
// MSSQL uses different system tables for function metadata.
//
// ## Array Columns
// MSSQL does not have native array column support like PostgreSQL.
// WHERE IN with array columns fails.
//
// ## Cursor Pagination
// Cursor pagination fails with "Invalid object name '__cur'".
// The cursor CTE implementation needs MSSQL-specific syntax.
//
// ## Subscriptions
// Real-time subscriptions are not yet implemented for MSSQL.
//
// ## Synthetic Tables
// Virtual/synthetic table support needs more work.
//
// ## Full-Text Search
// MSSQL uses different full-text search syntax (CONTAINS/FREETEXT)
// instead of PostgreSQL's tsvector. Not yet implemented.
//
// ## JSON Column Detection
// MSSQL stores JSON in NVARCHAR(MAX) columns, which aren't automatically
// detected as JSON type during schema introspection.
//
// ## Polymorphic Relationships (Unions)
// Union type queries are not yet fully working.
//
// ## Variable LIMIT
// Dynamic LIMIT from variables may not apply correctly.
//
// ## Skip/Include Directives
// Some skip/include directive patterns fail.
//
// # MSSQL-Specific Implementation Notes
//
// - Uses [brackets] for identifier quoting instead of "double quotes"
// - Uses @p1, @p2, etc. for parameter binding
// - Uses OFFSET/FETCH for pagination (requires ORDER BY)
// - Uses FOR JSON PATH for JSON generation
// - Does not support LATERAL joins (uses inline subqueries)
// - Boolean values render as 1/0 (BIT type)
type MSSQLDialect struct {
	DBVersion       int
	EnableCamelcase bool
}

func (d *MSSQLDialect) Name() string {
	return "mssql"
}

func (d *MSSQLDialect) QuoteIdentifier(s string) string {
	return "[" + s + "]"
}

// BindVar returns the parameter placeholder for MSSQL.
// go-mssqldb uses @p1, @p2, etc. for positional parameters.
func (d *MSSQLDialect) BindVar(i int) string {
	return fmt.Sprintf("@p%d", i)
}

func (d *MSSQLDialect) UseNamedParams() bool {
	return false
}

// SupportsLateral returns false for MSSQL because it doesn't support LATERAL joins.
// We use inline subqueries via RenderInlineChild instead.
func (d *MSSQLDialect) SupportsLateral() bool {
	return false
}

// SupportsReturning returns true because MSSQL has OUTPUT clause.
func (d *MSSQLDialect) SupportsReturning() bool {
	return true
}

func (d *MSSQLDialect) SupportsWritableCTE() bool {
	return false
}

func (d *MSSQLDialect) SupportsConflictUpdate() bool {
	return true // MSSQL has MERGE INTO
}

func (d *MSSQLDialect) SupportsSubscriptionBatching() bool {
	return false
}

func (d *MSSQLDialect) SupportsLinearExecution() bool {
	return true
}

func (d *MSSQLDialect) SplitQuery(query string) []string {
	// MSSQL uses GO as batch separator, but for our purposes we return single query
	return []string{query}
}

// RenderLimit renders pagination using OFFSET/FETCH syntax.
// MSSQL requires ORDER BY when using OFFSET/FETCH.
// If no ORDER BY is specified, we add a fallback ORDER BY (SELECT NULL).
func (d *MSSQLDialect) RenderLimit(ctx Context, sel *qcode.Select) {
	// MSSQL uses OFFSET n ROWS FETCH NEXT m ROWS ONLY
	// This requires ORDER BY clause to be present
	// If no ORDER BY, add a fallback ORDER BY (SELECT NULL)
	if len(sel.OrderBy) == 0 {
		ctx.WriteString(` ORDER BY (SELECT NULL)`)
	}

	ctx.WriteString(` OFFSET `)

	switch {
	case sel.Paging.OffsetVar != "":
		ctx.WriteString(`CAST(`)
		ctx.AddParam(Param{Name: sel.Paging.OffsetVar, Type: "int"})
		ctx.WriteString(` AS INT)`)
	case sel.Paging.Offset != 0:
		ctx.Write(fmt.Sprintf("%d", sel.Paging.Offset))
	default:
		ctx.WriteString(`0`)
	}
	ctx.WriteString(` ROWS`)

	if !sel.Paging.NoLimit {
		ctx.WriteString(` FETCH NEXT `)
		if sel.Singular {
			ctx.WriteString(`1`)
		} else if sel.Paging.LimitVar != "" {
			ctx.WriteString(`CAST(`)
			ctx.AddParam(Param{Name: sel.Paging.LimitVar, Type: "int"})
			ctx.WriteString(` AS INT)`)
		} else {
			ctx.Write(fmt.Sprintf("%d", sel.Paging.Limit))
		}
		ctx.WriteString(` ROWS ONLY`)
	}
}

func (d *MSSQLDialect) RenderJSONRoot(ctx Context, sel *qcode.Select) {
	ctx.WriteString(`SELECT (SELECT `)
}

func (d *MSSQLDialect) RenderJSONSelect(ctx Context, sel *qcode.Select) {
	ctx.WriteString(`SELECT (SELECT `)
	ctx.RenderJSONFields(sel)
	ctx.WriteString(` FOR JSON PATH, INCLUDE_NULL_VALUES, WITHOUT_ARRAY_WRAPPER) AS [json] `)
}

// RenderJSONPlural renders JSON array aggregation for MSSQL.
// Uses STRING_AGG to aggregate JSON objects into an array.
func (d *MSSQLDialect) RenderJSONPlural(ctx Context, sel *qcode.Select) {
	ctx.WriteString(`COALESCE('[' + STRING_AGG(`)
	ctx.Quote("__sj_" + fmt.Sprintf("%d", sel.ID))
	ctx.WriteString(`.[json], ',') + ']', '[]')`)
}

func (d *MSSQLDialect) RenderLateralJoin(ctx Context, sel *qcode.Select, multi bool) {
	// MSSQL doesn't support LATERAL joins - we use inline subqueries
	if sel.Rel.Type == sdata.RelNone && !multi {
		return
	}
	ctx.WriteString(` CROSS APPLY (`)
}

func (d *MSSQLDialect) RenderLateralJoinClose(ctx Context, alias string) {
	ctx.WriteString(`) AS `)
	ctx.Quote(alias)
}

func (d *MSSQLDialect) RenderJoinTables(ctx Context, sel *qcode.Select) {
	// MSSQL does not render extra joins for order by lists
}

func (d *MSSQLDialect) RenderCursorCTE(ctx Context, sel *qcode.Select) {
	if !sel.Paging.Cursor {
		return
	}
	ctx.WriteString(`WITH [__cur] AS (SELECT `)
	for i, ob := range sel.OrderBy {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		// Parse cursor value using STRING_SPLIT equivalent
		// Cursor format: selID:val1:val2:...
		// We use PARSENAME or custom parsing with CHARINDEX
		ctx.WriteString(`NULLIF(`)
		ctx.WriteString(fmt.Sprintf(`(SELECT value FROM STRING_SPLIT(a.i, ':') ORDER BY (SELECT NULL) OFFSET %d ROWS FETCH NEXT 1 ROWS ONLY)`, i+1))
		ctx.WriteString(`, '') AS `)

		if ob.KeyVar != "" && ob.Key != "" {
			ctx.Quote(ob.Col.Name + "_" + ob.Key)
		} else {
			ctx.Quote(ob.Col.Name)
		}
	}
	ctx.WriteString(` FROM (SELECT `)
	ctx.AddParam(Param{Name: "cursor", Type: "text"})
	ctx.WriteString(` AS i) AS a) `)
}

func (d *MSSQLDialect) RenderOrderBy(ctx Context, sel *qcode.Select) {
	if len(sel.OrderBy) == 0 {
		return
	}
	ctx.WriteString(` ORDER BY `)

	for i, ob := range sel.OrderBy {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		if ob.KeyVar != "" && ob.Key != "" {
			ctx.WriteString(` CASE WHEN `)
			ctx.AddParam(Param{Name: ob.KeyVar, Type: "text"})
			ctx.WriteString(` = `)
			ctx.WriteString(fmt.Sprintf("'%s'", ob.Key))
			ctx.WriteString(` THEN `)
		}
		if ob.Var != "" {
			// MSSQL equivalent of FIND_IN_SET using CHARINDEX
			ctx.WriteString(`CHARINDEX(',' + CAST(`)
			ctx.ColWithTable(ob.Col.Table, ob.Col.Name)
			ctx.WriteString(` AS NVARCHAR(MAX)) + ',', ',' + `)
			ctx.AddParam(Param{Name: ob.Var, Type: "text"})
			ctx.WriteString(` + ',')`)
		} else {
			ctx.ColWithTable(ob.Col.Table, ob.Col.Name)
		}
		if ob.KeyVar != "" && ob.Key != "" {
			ctx.WriteString(` END `)
		}

		switch ob.Order {
		case qcode.OrderAsc:
			ctx.WriteString(` ASC`)
		case qcode.OrderDesc:
			ctx.WriteString(` DESC`)
		case qcode.OrderAscNullsFirst:
			// MSSQL doesn't support NULLS FIRST/LAST - use CASE
			ctx.WriteString(` ASC`) // NULLs are sorted first by default in ASC
		case qcode.OrderDescNullsFirst:
			ctx.WriteString(` DESC`)
		case qcode.OrderAscNullsLast:
			ctx.WriteString(` ASC`)
		case qcode.OrderDescNullsLast:
			ctx.WriteString(` DESC`) // NULLs are sorted last by default in DESC
		}
	}
}

func (d *MSSQLDialect) RenderDistinctOn(ctx Context, sel *qcode.Select) {
	// MSSQL does not support DISTINCT ON
}

func (d *MSSQLDialect) RenderFromEdge(ctx Context, sel *qcode.Select) {
	// Use OPENJSON for embedded JSON columns
	ctx.WriteString(`OPENJSON(`)
	ctx.ColWithTable(sel.Rel.Left.Col.Table, sel.Rel.Left.Col.Name)
	ctx.WriteString(`) WITH (`)

	for i, col := range sel.Ti.Columns {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.Quote(col.Name)
		ctx.WriteString(` `)
		ctx.WriteString(d.mssqlType(col.Type))
		ctx.WriteString(` '$.`)
		ctx.WriteString(col.Name)
		ctx.WriteString(`'`)
	}
	ctx.WriteString(`) AS `)
	ctx.Quote(sel.Table)
}

// RenderJSONPath renders JSON path extraction for MSSQL.
func (d *MSSQLDialect) RenderJSONPath(ctx Context, table, col string, path []string) {
	ctx.WriteString(`JSON_VALUE(`)
	ctx.ColWithTable(table, col)
	ctx.WriteString(`, '$.`)
	for i, p := range path {
		if i > 0 {
			ctx.WriteString(`.`)
		}
		ctx.WriteString(p)
	}
	ctx.WriteString(`')`)
}

func (d *MSSQLDialect) RenderList(ctx Context, ex *qcode.Exp) {
	ctx.WriteString(`(`)
	for i := range ex.Right.ListVal {
		if i != 0 {
			ctx.WriteString(` UNION ALL `)
		}
		ctx.WriteString(`SELECT `)
		switch ex.Right.ListType {
		case qcode.ValBool, qcode.ValNum:
			ctx.WriteString(ex.Right.ListVal[i])
		case qcode.ValStr:
			ctx.WriteString(`'`)
			ctx.WriteString(ex.Right.ListVal[i])
			ctx.WriteString(`'`)
		case qcode.ValDBVar:
			d.RenderVar(ctx, ex.Right.ListVal[i])
		}
	}
	ctx.WriteString(`)`)
}

func (d *MSSQLDialect) RenderOp(op qcode.ExpOp) (string, error) {
	switch op {
	case qcode.OpEquals:
		return "=", nil
	case qcode.OpNotEquals:
		return "!=", nil
	case qcode.OpGreaterThan:
		return ">", nil
	case qcode.OpGreaterOrEquals:
		return ">=", nil
	case qcode.OpLesserThan:
		return "<", nil
	case qcode.OpLesserOrEquals:
		return "<=", nil
	case qcode.OpLike:
		return "LIKE", nil
	case qcode.OpNotLike:
		return "NOT LIKE", nil
	case qcode.OpILike:
		return "LIKE", nil // MSSQL uses case-insensitive collation
	case qcode.OpNotILike:
		return "NOT LIKE", nil
	case qcode.OpIn:
		return "IN", nil
	case qcode.OpNotIn:
		return "NOT IN", nil
	case qcode.OpContains:
		return "LIKE", nil // Handled specially
	case qcode.OpContainedIn:
		return "IN", nil
	case qcode.OpHasKey:
		return "IS NOT NULL", nil
	case qcode.OpIsNull:
		return "IS NULL", nil
	case qcode.OpTsQuery:
		return "CONTAINS", nil
	default:
		return "", fmt.Errorf("unsupported operator: %v", op)
	}
}

func (d *MSSQLDialect) RenderValPrefix(ctx Context, ex *qcode.Exp) bool {
	// Handle array column overlap operations
	// OpHasInCommon is used when comparing array columns to a list
	// It checks if any element in the column's JSON array exists in the provided list
	if ex.Left.Col.Array && (ex.Op == qcode.OpHasInCommon || ex.Op == qcode.OpIn || ex.Op == qcode.OpNotIn) {
		// For MSSQL, array columns contain JSON arrays like ["Tag 1", "Tag 2"]
		// We need to check if any element in the column's array exists in the provided list
		if ex.Op == qcode.OpNotIn {
			ctx.WriteString(`(NOT `)
		} else {
			ctx.WriteString(`(`)
		}
		ctx.WriteString(`EXISTS (SELECT 1 FROM OPENJSON(`)

		// Render the column
		var table string
		if ex.Left.Table == "" {
			table = ex.Left.Col.Table
		} else {
			table = ex.Left.Table
		}
		ctx.ColWithTable(table, ex.Left.Col.Name)

		ctx.WriteString(`) WHERE [value] IN (`)

		if ex.Right.ValType == qcode.ValVar {
			// Variable list: use OPENJSON to unpack
			ctx.WriteString(`SELECT [value] FROM OPENJSON(`)
			ctx.AddParam(Param{Name: ex.Right.Val, Type: "json", IsArray: true})
			ctx.WriteString(`)`)
		} else if ex.Right.ValType == qcode.ValList {
			// Static list: render inline values
			for i := range ex.Right.ListVal {
				if i != 0 {
					ctx.WriteString(`, `)
				}
				d.RenderLiteral(ctx, ex.Right.ListVal[i], ex.Right.ListType)
			}
		}
		ctx.WriteString(`)))`)
		return true
	}

	// Handle special __gj_json_pk format for bulk JSON inserts
	if ex.Right.ValType == qcode.ValVar &&
		(ex.Op == qcode.OpIn || ex.Op == qcode.OpNotIn) &&
		strings.HasPrefix(ex.Right.Val, "__gj_json_pk:gj_sep:") {

		parts := strings.Split(ex.Right.Val, ":gj_sep:")
		if len(parts) == 4 {
			actionVar := parts[1]
			jsonKey := parts[2]
			colType := parts[3]

			// Render: column IN (SELECT [value] FROM OPENJSON(@param) WITH ([value] TYPE '$.key'))
			ctx.ColWithTable(ex.Left.Col.Table, ex.Left.Col.Name)
			ctx.WriteString(` `)

			if ex.Op == qcode.OpNotIn {
				ctx.WriteString(`NOT `)
			}

			ctx.WriteString(`IN (SELECT [value] FROM OPENJSON(`)
			ctx.AddParam(Param{Name: actionVar, Type: "json", WrapInArray: true})
			ctx.WriteString(`) WITH ([value] `)
			ctx.WriteString(d.mssqlType(colType))
			ctx.WriteString(` '$."`)
			ctx.WriteString(jsonKey)
			ctx.WriteString(`"'))`)

			return true
		}
	}

	return false
}

func (d *MSSQLDialect) RenderTsQuery(ctx Context, ti sdata.DBTable, ex *qcode.Exp) {
	// MSSQL uses CONTAINS for full-text search
	if len(ti.FullText) > 0 {
		ctx.WriteString(`CONTAINS((`)
		for i, col := range ti.FullText {
			if i != 0 {
				ctx.WriteString(`, `)
			}
			ctx.Quote(col.Name)
		}
		ctx.WriteString(`), `)
		ctx.AddParam(Param{Name: ex.Right.Val, Type: "text"})
		ctx.WriteString(`)`)
	}
}

func (d *MSSQLDialect) RenderSearchRank(ctx Context, sel *qcode.Select, f qcode.Field) {
	// MSSQL doesn't have ts_rank equivalent
	ctx.WriteString(`0`)
}

func (d *MSSQLDialect) RenderSearchHeadline(ctx Context, sel *qcode.Select, f qcode.Field) {
	// MSSQL doesn't have ts_headline equivalent
	ctx.ColWithTable(sel.Ti.Name, f.Col.Name)
}

func (d *MSSQLDialect) RenderValVar(ctx Context, ex *qcode.Exp, val string) bool {
	// Return false to use default GraphQL variable handling
	// This is for rendering GraphQL variables ($var), not database variables (@var)
	return false
}

func (d *MSSQLDialect) RenderValArrayColumn(ctx Context, ex *qcode.Exp, table string, pid int32) {
	// MSSQL uses OPENJSON for JSON array columns
	ctx.WriteString(`SELECT value FROM OPENJSON(`)
	t := table
	if pid > 0 {
		t = fmt.Sprintf("%s_%d", table, pid)
	}
	ctx.ColWithTable(t, ex.Right.Col.Name)
	ctx.WriteString(`)`)
}

func (d *MSSQLDialect) RenderArray(ctx Context, items []string) {
	ctx.WriteString(`'[`)
	for i, item := range items {
		if i != 0 {
			ctx.WriteString(`,`)
		}
		ctx.WriteString(item)
	}
	ctx.WriteString(`]'`)
}

func (d *MSSQLDialect) RenderLiteral(ctx Context, val string, valType qcode.ValType) {
	switch valType {
	case qcode.ValBool:
		if val == "true" {
			ctx.WriteString("1")
		} else {
			ctx.WriteString("0")
		}
	case qcode.ValNum:
		ctx.WriteString(val)
	case qcode.ValStr:
		ctx.WriteString(`N'`)
		ctx.WriteString(strings.ReplaceAll(val, "'", "''"))
		ctx.WriteString(`'`)
	default:
		ctx.WriteString(val)
	}
}

// RenderJSONField renders a field for MSSQL's FOR JSON PATH.
// Unlike MySQL's json_object which uses 'key', value pairs,
// MSSQL FOR JSON PATH uses column aliases: col AS [key]
func (d *MSSQLDialect) RenderJSONField(ctx Context, fieldName string, tableAlias string, colName string, isNull bool, isJSON bool) {
	if isNull {
		ctx.WriteString(`NULL AS [`)
		ctx.WriteString(fieldName)
		ctx.WriteString(`]`)
	} else if isJSON {
		ctx.WriteString(`JSON_QUERY(`)
		if tableAlias != "" {
			ctx.Quote(tableAlias)
			ctx.WriteString(`.`)
		}
		ctx.Quote(colName)
		ctx.WriteString(`, '$') AS [`)
		ctx.WriteString(fieldName)
		ctx.WriteString(`]`)
	} else {
		if tableAlias != "" {
			ctx.Quote(tableAlias)
			ctx.WriteString(`.`)
		}
		ctx.Quote(colName)
		ctx.WriteString(` AS [`)
		ctx.WriteString(fieldName)
		ctx.WriteString(`]`)
	}
}

func (d *MSSQLDialect) RenderRootTerminator(ctx Context) {
	// Close the inner SELECT with FOR JSON PATH to produce JSON object
	// e.g., SELECT (SELECT <fields> FOR JSON PATH, INCLUDE_NULL_VALUES, WITHOUT_ARRAY_WRAPPER) AS [__root]
	ctx.WriteString(` FOR JSON PATH, INCLUDE_NULL_VALUES, WITHOUT_ARRAY_WRAPPER) AS [__root]`)
}

func (d *MSSQLDialect) RenderBaseTable(ctx Context) {
	// MSSQL requires a FROM clause - use a dummy SELECT with named column
	ctx.WriteString(`SELECT 1 AS [x]`)
}

// RenderJSONRootField renders a root-level JSON field for MSSQL's FOR JSON PATH.
// Uses column AS [key] format instead of 'key', value format.
func (d *MSSQLDialect) RenderJSONRootField(ctx Context, key string, val func()) {
	if key == "__typename" {
		val()
		ctx.WriteString(` AS [`)
		ctx.WriteString(key)
		ctx.WriteString(`]`)
	} else {
		ctx.WriteString(`JSON_QUERY(`)
		val()
		ctx.WriteString(`) AS [`)
		ctx.WriteString(key)
		ctx.WriteString(`]`)
	}
}

func (d *MSSQLDialect) RenderTableName(ctx Context, sel *qcode.Select, schema, table string) {
	if schema != "" && schema != "dbo" {
		ctx.Quote(schema)
		ctx.WriteString(`.`)
	}
	ctx.Quote(table)
}

func (d *MSSQLDialect) RenderTableAlias(ctx Context, alias string) {
	ctx.WriteString(` AS `)
	ctx.Quote(alias)
	ctx.WriteString(` `)
}

// RenderInlineChild renders an inline subquery for MSSQL.
// MSSQL doesn't support LATERAL joins, so we generate correlated subqueries.
func (d *MSSQLDialect) RenderInlineChild(ctx Context, r InlineChildRenderer, psel, sel *qcode.Select) {
	// For recursive relationships, use recursive CTE approach
	if sel.Rel.Type == sdata.RelRecursive {
		d.renderRecursiveInlineChild(ctx, r, psel, sel)
		return
	}

	if sel.Singular {
		if sel.Type == qcode.SelTypeUnion {
			ctx.WriteString(`(SELECT COALESCE(`)
			first := true
			for _, cid := range sel.Children {
				csel := r.GetChild(cid)
				if csel == nil {
					continue
				}
				if !first {
					ctx.WriteString(`, `)
				}
				first = false
				r.RenderInlineChild(sel, csel)
			}
			ctx.WriteString(`))`)
			return
		}

		// For singular, return a single JSON object using FOR JSON PATH
		ctx.WriteString(`(SELECT `)
		d.renderInlineJSONFields(ctx, r, sel)

		ctx.WriteString(` FROM `)
		d.renderFromTable(ctx, r, sel, psel)
		if sel.Rel.Type != sdata.RelEmbedded {
			t := sel.Ti.Name
			if sel.ID >= 0 {
				t = fmt.Sprintf("%s_%d", t, sel.ID)
			}
			d.RenderTableAlias(ctx, t)
		}

		// Render joins
		for _, join := range sel.Joins {
			d.renderJoinWithAlias(ctx, r, psel, sel, join)
		}

		// Render WHERE clause
		if sel.Where.Exp != nil {
			ctx.WriteString(` WHERE `)
			d.renderWhereExp(ctx, r, psel, sel, sel.Where.Exp)
		}
		d.renderGroupBy(ctx, r, sel)
		// Close the singular select with FOR JSON PATH
		ctx.WriteString(` FOR JSON PATH, INCLUDE_NULL_VALUES, WITHOUT_ARRAY_WRAPPER)`)
		return
	}

	// For plural, aggregate into array using FOR JSON PATH directly
	// MSSQL's FOR JSON PATH naturally produces an array, no need for STRING_AGG
	if psel != nil {
		// Correlated subquery - use FOR JSON PATH to produce array
		ctx.WriteString(`COALESCE((SELECT `)
		d.renderInlineJSONFields(ctx, r, sel)

		ctx.WriteString(` FROM `)
		d.renderFromTable(ctx, r, sel, psel)
		if sel.Rel.Type != sdata.RelEmbedded {
			t := sel.Ti.Name
			if sel.ID >= 0 {
				t = fmt.Sprintf("%s_%d", t, sel.ID)
			}
			d.RenderTableAlias(ctx, t)
		}

		// Render joins
		for _, join := range sel.Joins {
			d.renderJoinWithAlias(ctx, r, psel, sel, join)
		}

		// Render WHERE clause
		if sel.Where.Exp != nil {
			ctx.WriteString(` WHERE `)
			d.renderWhereExp(ctx, r, psel, sel, sel.Where.Exp)
		}
		d.renderGroupBy(ctx, r, sel)

		// Add ORDER BY if needed
		if len(sel.OrderBy) > 0 {
			ctx.WriteString(` ORDER BY `)
			for i, ob := range sel.OrderBy {
				if i != 0 {
					ctx.WriteString(`, `)
				}
				t := sel.Ti.Name
				if sel.ID >= 0 {
					t = fmt.Sprintf("%s_%d", t, sel.ID)
				}
				r.ColWithTable(t, ob.Col.Name)
				switch ob.Order {
				case qcode.OrderAsc:
					ctx.WriteString(` ASC`)
				case qcode.OrderDesc:
					ctx.WriteString(` DESC`)
				}
			}
		}

		ctx.WriteString(` FOR JSON PATH, INCLUDE_NULL_VALUES), '[]')`)
	} else {
		// Root query - use FOR JSON PATH directly instead of STRING_AGG with subquery
		// MSSQL doesn't allow subqueries inside STRING_AGG
		skipVarName, isSkip, hasSkipVar := d.findSkipVarExp(sel.Where.Exp)
		if hasSkipVar {
			ctx.WriteString(`CASE WHEN (`)
			ctx.AddParam(Param{Name: skipVarName, Type: "bit"})
			if isSkip {
				ctx.WriteString(` != 1) THEN `)
			} else {
				ctx.WriteString(` = 1) THEN `)
			}
		}

		// Use COALESCE to handle empty result set
		// Use renderInlineJSONFields instead of renderBaseColumns to exclude _ord_ columns from JSON
		ctx.WriteString(`COALESCE((SELECT `)
		d.renderInlineJSONFields(ctx, r, sel)

		ctx.WriteString(` FROM `)
		d.renderFromTable(ctx, r, sel, psel)
		if sel.Rel.Type != sdata.RelEmbedded {
			t := sel.Ti.Name
			if sel.ID >= 0 {
				t = fmt.Sprintf("%s_%d", t, sel.ID)
			}
			d.RenderTableAlias(ctx, t)
		}

		// Add cursor CTE join
		if sel.Paging.Cursor {
			ctx.WriteString(`, [__cur]`)
		}

		// Render joins
		for _, join := range sel.Joins {
			d.renderJoinWithAlias(ctx, r, nil, sel, join)
		}

		// Render WHERE clause
		if sel.Where.Exp != nil {
			ctx.WriteString(` WHERE `)
			d.renderWhereExp(ctx, r, nil, sel, sel.Where.Exp)
		}
		d.renderGroupBy(ctx, r, sel)

		// Render ORDER BY
		d.renderOrderBy(ctx, r, sel, "")

		// Render OFFSET/FETCH
		if sel.Paging.Limit != 0 {
			r.RenderLimit(sel)
		}

		// For singular root, use WITHOUT_ARRAY_WRAPPER and null fallback
		// For plural root, use array format and '[]' fallback
		if sel.Singular {
			ctx.WriteString(` FOR JSON PATH, INCLUDE_NULL_VALUES, WITHOUT_ARRAY_WRAPPER), NULL)`)
		} else {
			ctx.WriteString(` FOR JSON PATH, INCLUDE_NULL_VALUES), '[]')`)
		}

		if hasSkipVar {
			ctx.WriteString(` ELSE NULL END`)
		}
	}
}

func (d *MSSQLDialect) renderRecursiveInlineChild(ctx Context, r InlineChildRenderer, psel, sel *qcode.Select) {
	// MSSQL doesn't support CTEs inside subqueries, so we use nested OR conditions
	// (same approach as MariaDB) to traverse recursive relationships up to a max depth.

	// 1. Determine direction: parents vs children
	v, _ := sel.GetInternalArg("find")
	findParents := v.Val == "parents" || v.Val == "parent"

	// 2. Get max depth (default 20, can be reduced by limit)
	maxDepth := 20
	if sel.Paging.Limit > 0 && int(sel.Paging.Limit) < maxDepth {
		maxDepth = int(sel.Paging.Limit)
	}

	// 3. Build table aliases
	t := sel.Ti.Name
	if sel.ID >= 0 {
		t = fmt.Sprintf("%s_%d", t, sel.ID)
	}
	pt := psel.Ti.Name
	if psel.ID >= 0 {
		pt = fmt.Sprintf("%s_%d", pt, psel.ID)
	}

	// 4. Get column names
	pkCol := sel.Ti.PrimaryCol.Name
	fkCol := sel.Rel.Left.Col.Name // e.g., reply_to_id

	// 5. Render the query
	ctx.WriteString(`(SELECT COALESCE((SELECT `)
	d.renderInlineJSONFields(ctx, r, sel)
	ctx.WriteString(` FROM `)
	r.RenderTable(sel, sel.Ti.Schema, sel.Ti.Name, false)
	d.RenderTableAlias(ctx, t)
	ctx.WriteString(` WHERE (`)

	// 6. Generate nested OR conditions for each depth level
	for depth := 1; depth <= maxDepth; depth++ {
		if depth > 1 {
			ctx.WriteString(` OR `)
		}
		ctx.Quote(t)
		ctx.WriteString(`.`)
		if findParents {
			ctx.Quote(pkCol)
			ctx.WriteString(` = `)
			d.renderNestedFKSubquery(ctx, sel.Ti.Schema, sel.Ti.Name, pkCol, fkCol, pt, depth)
		} else {
			ctx.Quote(fkCol)
			ctx.WriteString(` = `)
			d.renderNestedPKSubquery(ctx, sel.Ti.Schema, sel.Ti.Name, pkCol, fkCol, pt, depth)
		}
	}

	ctx.WriteString(`)`)

	// For parent traversal, order by PK descending (closest parent first)
	if findParents {
		ctx.WriteString(` ORDER BY `)
		ctx.Quote(t)
		ctx.WriteString(`.`)
		ctx.Quote(pkCol)
		ctx.WriteString(` DESC`)
	}

	ctx.WriteString(` FOR JSON PATH, INCLUDE_NULL_VALUES), '[]'))`)
}

// renderNestedFKSubquery generates nested subqueries for parent traversal.
// For depth=1: (SELECT [reply_to_id] FROM [table] WHERE [id] = [parent].[id])
// For depth>1: wraps the previous level recursively.
func (d *MSSQLDialect) renderNestedFKSubquery(ctx Context, schema, table, pkCol, fkCol, parentTable string, depth int) {
	if depth == 1 {
		// Base case: get the FK value from the parent row
		ctx.WriteString(`(SELECT `)
		ctx.Quote(fkCol)
		ctx.WriteString(` FROM `)
		d.RenderTableName(ctx, nil, schema, table)
		ctx.WriteString(` WHERE `)
		ctx.Quote(pkCol)
		ctx.WriteString(` = `)
		ctx.Quote(parentTable)
		ctx.WriteString(`.`)
		ctx.Quote(pkCol)
		ctx.WriteString(`)`)
	} else {
		// Recursive case: wrap previous level
		ctx.WriteString(`(SELECT `)
		ctx.Quote(fkCol)
		ctx.WriteString(` FROM `)
		d.RenderTableName(ctx, nil, schema, table)
		ctx.WriteString(` WHERE `)
		ctx.Quote(pkCol)
		ctx.WriteString(` = `)
		d.renderNestedFKSubquery(ctx, schema, table, pkCol, fkCol, parentTable, depth-1)
		ctx.WriteString(`)`)
	}
}

// renderNestedPKSubquery generates nested subqueries for children traversal.
// For depth=1: just references [parent].[id]
// For depth>1: (SELECT [id] FROM [table] WHERE [fk] = nested_subquery)
func (d *MSSQLDialect) renderNestedPKSubquery(ctx Context, schema, table, pkCol, fkCol, parentTable string, depth int) {
	if depth == 1 {
		// Base case: reference parent's PK directly
		ctx.Quote(parentTable)
		ctx.WriteString(`.`)
		ctx.Quote(pkCol)
	} else {
		// Recursive case: find children of the previous level
		ctx.WriteString(`(SELECT `)
		ctx.Quote(pkCol)
		ctx.WriteString(` FROM `)
		d.RenderTableName(ctx, nil, schema, table)
		ctx.WriteString(` WHERE `)
		ctx.Quote(fkCol)
		ctx.WriteString(` = `)
		d.renderNestedPKSubquery(ctx, schema, table, pkCol, fkCol, parentTable, depth-1)
		ctx.WriteString(`)`)
	}
}

func (d *MSSQLDialect) renderOuterOrderBy(ctx Context, r InlineChildRenderer, sel *qcode.Select, subqueryAlias string) {
	if len(sel.OrderBy) == 0 {
		return
	}
	ctx.WriteString(` WITHIN GROUP (ORDER BY `)
	for i, ob := range sel.OrderBy {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		if subqueryAlias != "" {
			ctx.WriteString(subqueryAlias)
			ctx.WriteString(`.`)
		}
		ctx.WriteString(fmt.Sprintf("[_ord_%d]", i))
		switch ob.Order {
		case qcode.OrderAsc:
			ctx.WriteString(` ASC`)
		case qcode.OrderDesc:
			ctx.WriteString(` DESC`)
		}
	}
	ctx.WriteString(`)`)
}

func (d *MSSQLDialect) RenderChildCursor(ctx Context, renderChild func()) {
	ctx.WriteString(`JSON_VALUE(`)
	renderChild()
	ctx.WriteString(`, '$.cursor')`)
}

func (d *MSSQLDialect) RenderChildValue(ctx Context, sel *qcode.Select, renderChild func()) {
	if sel.Paging.Cursor {
		ctx.WriteString(`JSON_QUERY(`)
		renderChild()
		ctx.WriteString(`, '$.json')`)
	} else {
		renderChild()
	}
}

func (d *MSSQLDialect) renderInlineJSONFields(ctx Context, r InlineChildRenderer, sel *qcode.Select) {
	i := 0
	for _, f := range sel.Fields {
		if f.SkipRender == qcode.SkipTypeDrop {
			continue
		}
		if i != 0 {
			ctx.WriteString(`, `)
		}

		if f.SkipRender == qcode.SkipTypeNulled || f.SkipRender == qcode.SkipTypeBlocked || f.SkipRender == qcode.SkipTypeUserNeeded {
			ctx.WriteString(`NULL AS `)
			ctx.Quote(f.FieldName)
			i++
			continue
		}

		t := sel.Ti.Name
		if sel.ID >= 0 {
			t = fmt.Sprintf("%s_%d", t, sel.ID)
		}

		// Handle skipIf/includeIf field filters
		if f.FieldFilter.Exp != nil {
			ctx.WriteString(`(CASE WHEN `)
			d.renderFieldFilterExp(ctx, r, sel, f.FieldFilter.Exp)
			ctx.WriteString(` THEN `)
		}

		if f.Func.Name != "" {
			// MSSQL requires user-defined functions to be called with at least a two-part name
			// Built-in aggregates (count, sum, max, etc.) have Agg=true and empty Schema - no prefix needed
			if f.Func.Schema != "" {
				ctx.Quote(f.Func.Schema)
				ctx.WriteString(`.`)
				ctx.Quote(f.Func.Name)
			} else if !f.Func.Agg {
				// User-defined function without explicit schema, use dbo
				ctx.WriteString(`[dbo].`)
				ctx.Quote(f.Func.Name)
			} else {
				// Built-in aggregates (Schema == "" && Agg == true) - write directly without quoting
				ctx.WriteString(f.Func.Name)
			}
			ctx.WriteString(`(`)
			if len(f.Args) != 0 {
				for k, arg := range f.Args {
					if k != 0 {
						ctx.WriteString(`, `)
					}
					switch arg.Type {
					case qcode.ArgTypeCol:
						r.ColWithTable(t, arg.Col.Name)
					case qcode.ArgTypeVal:
						ctx.WriteString(arg.Val)
					default:
						ctx.WriteString(arg.Val)
					}
				}
			} else if f.Col.Name == "" || f.Col.Name == "*" {
				ctx.WriteString(`*`)
			} else {
				r.ColWithTable(t, f.Col.Name)
			}
			ctx.WriteString(`)`)
		} else {
			// Schema detection now returns "json" for NVARCHAR(MAX) columns with ISJSON constraints
			isJSON := f.Col.Type == "json" || f.Col.Array
			if isJSON {
				ctx.WriteString(`JSON_QUERY(`)
			}
			r.ColWithTable(t, f.Col.Name)
			if isJSON {
				ctx.WriteString(`, '$')`)
			}
		}

		if f.FieldFilter.Exp != nil {
			ctx.WriteString(` ELSE null END)`)
		}

		ctx.WriteString(` AS `)
		ctx.Quote(f.FieldName)
		i++
	}

	// Handle __typename
	if sel.Typename {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.WriteString(`'`)
		ctx.WriteString(sel.Table)
		ctx.WriteString(`' AS [__typename]`)
	}

	// Handle nested children
	for _, cid := range sel.Children {
		csel := r.GetChild(cid)
		if csel == nil || csel.SkipRender == qcode.SkipTypeRemote || csel.SkipRender == qcode.SkipTypeDrop {
			continue
		}
		if i != 0 {
			ctx.WriteString(`, `)
		}

		if csel.SkipRender == qcode.SkipTypeNulled || csel.SkipRender == qcode.SkipTypeBlocked || csel.SkipRender == qcode.SkipTypeUserNeeded {
			ctx.WriteString(`NULL AS `)
			ctx.Quote(csel.FieldName)
			i++
			continue
		}

		ctx.WriteString(`JSON_QUERY(`)
		r.RenderInlineChild(sel, csel)
		ctx.WriteString(`) AS `)
		ctx.Quote(csel.FieldName)
		i++
	}
}

func (d *MSSQLDialect) renderSubqueryJSONFields(ctx Context, r InlineChildRenderer, sel *qcode.Select) {
	i := 0
	for _, f := range sel.Fields {
		if f.SkipRender == qcode.SkipTypeDrop {
			continue
		}
		if i != 0 {
			ctx.WriteString(`, `)
		}

		if f.SkipRender == qcode.SkipTypeNulled || f.SkipRender == qcode.SkipTypeBlocked || f.SkipRender == qcode.SkipTypeUserNeeded {
			ctx.WriteString(`NULL AS `)
			ctx.Quote(f.FieldName)
			i++
			continue
		}

		isJSON := f.Col.Type == "json" || f.Col.Type == "nvarchar(max)" || f.Col.Array
		if isJSON {
			ctx.WriteString(`JSON_QUERY(`)
		}
		r.ColWithTable("_gj_t", f.FieldName)
		if isJSON {
			ctx.WriteString(`, '$')`)
		} else {
			ctx.WriteString(` AS `)
			ctx.Quote(f.FieldName)
		}
		i++
	}

	// Handle __typename
	if sel.Typename {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.WriteString(`'`)
		ctx.WriteString(sel.Table)
		ctx.WriteString(`' AS [__typename]`)
	}

	// Handle nested children
	for _, cid := range sel.Children {
		csel := r.GetChild(cid)
		if csel == nil || csel.SkipRender == qcode.SkipTypeRemote || csel.SkipRender == qcode.SkipTypeDrop {
			continue
		}
		if i != 0 {
			ctx.WriteString(`, `)
		}
		r.ColWithTable("_gj_t", csel.FieldName)
		ctx.WriteString(` AS `)
		ctx.Quote(csel.FieldName)
		i++
	}
}

func (d *MSSQLDialect) renderBaseColumns(ctx Context, r InlineChildRenderer, sel *qcode.Select) {
	i := 0
	for _, f := range sel.Fields {
		if f.SkipRender != qcode.SkipTypeNone {
			continue
		}
		if i != 0 {
			ctx.WriteString(`, `)
		}
		t := sel.Ti.Name
		if sel.ID >= 0 {
			t = fmt.Sprintf("%s_%d", t, sel.ID)
		}

		// Handle skipIf/includeIf field filters
		if f.FieldFilter.Exp != nil {
			ctx.WriteString(`(CASE WHEN `)
			d.renderFieldFilterExp(ctx, r, sel, f.FieldFilter.Exp)
			ctx.WriteString(` THEN `)
		}

		if f.Func.Name != "" {
			// MSSQL requires user-defined functions to be called with at least a two-part name
			// Built-in aggregates (count, sum, max, etc.) have Agg=true and empty Schema - no prefix needed
			if f.Func.Schema != "" {
				ctx.Quote(f.Func.Schema)
				ctx.WriteString(`.`)
				ctx.Quote(f.Func.Name)
			} else if !f.Func.Agg {
				// User-defined function without explicit schema, use dbo
				ctx.WriteString(`[dbo].`)
				ctx.Quote(f.Func.Name)
			} else {
				// Built-in aggregates (Schema == "" && Agg == true) - write directly without quoting
				ctx.WriteString(f.Func.Name)
			}
			ctx.WriteString(`(`)
			if len(f.Args) != 0 {
				for k, arg := range f.Args {
					if k != 0 {
						ctx.WriteString(`, `)
					}
					switch arg.Type {
					case qcode.ArgTypeCol:
						r.ColWithTable(t, arg.Col.Name)
					case qcode.ArgTypeVal:
						ctx.WriteString(arg.Val)
					default:
						ctx.WriteString(arg.Val)
					}
				}
			} else if f.Col.Name == "" || f.Col.Name == "*" {
				ctx.WriteString(`*`)
			} else {
				r.ColWithTable(t, f.Col.Name)
			}
			ctx.WriteString(`)`)
		} else {
			r.ColWithTable(t, f.Col.Name)
		}

		if f.FieldFilter.Exp != nil {
			ctx.WriteString(` ELSE null END)`)
		}

		ctx.WriteString(` AS `)
		ctx.Quote(f.FieldName)
		i++
	}

	for j, ob := range sel.OrderBy {
		if i != 0 || j > 0 {
			ctx.WriteString(`, `)
		}
		t := ob.Col.Table
		col := ob.Col.Name

		if ob.Var != "" {
			t = fmt.Sprintf("_gj_ob_%s_%s", ob.Col.Table, ob.Col.Name)
			col = "ord"
		} else {
			if t == "" {
				t = sel.Ti.Name
			}
			if t == sel.Ti.Name {
				if sel.ID >= 0 {
					t = fmt.Sprintf("%s_%d", t, sel.ID)
				}
			} else {
				t = fmt.Sprintf("%s_0", t)
			}
		}
		r.ColWithTable(t, col)
		ctx.WriteString(fmt.Sprintf(` AS [_ord_%d]`, j))
	}

	// Render nested children as inline subqueries
	for _, cid := range sel.Children {
		csel := r.GetChild(cid)
		if csel == nil || csel.SkipRender == qcode.SkipTypeRemote || csel.SkipRender == qcode.SkipTypeDrop {
			continue
		}
		if i != 0 {
			ctx.WriteString(`, `)
		}

		if csel.SkipRender == qcode.SkipTypeUserNeeded || csel.SkipRender == qcode.SkipTypeBlocked ||
			csel.SkipRender == qcode.SkipTypeNulled {
			ctx.WriteString(`NULL AS `)
			ctx.Quote(csel.FieldName)
			i++
			continue
		}

		ctx.WriteString(`JSON_QUERY(`)
		r.RenderInlineChild(sel, csel)
		ctx.WriteString(`) AS `)
		ctx.Quote(csel.FieldName)
		i++
	}

	// Add cursor columns for pagination
	if sel.Paging.Cursor {
		for j, ob := range sel.OrderBy {
			ctx.WriteString(`, `)
			t := sel.Ti.Name
			if sel.ID >= 0 {
				t = fmt.Sprintf("%s_%d", t, sel.ID)
			}
			r.ColWithTable(t, ob.Col.Name)
			ctx.WriteString(fmt.Sprintf(" AS [__cur_%d]", j))
		}
	}
}

func (d *MSSQLDialect) renderFromTable(ctx Context, r InlineChildRenderer, sel *qcode.Select, psel *qcode.Select) {
	if sel.Rel.Type == sdata.RelEmbedded {
		// Use OPENJSON for embedded JSON columns
		ctx.WriteString(`OPENJSON(`)
		parentAlias := sel.Rel.Left.Col.Table
		if psel != nil && psel.ID >= 0 {
			parentAlias = fmt.Sprintf("%s_%d", sel.Rel.Left.Col.Table, psel.ID)
		}
		ctx.Quote(parentAlias)
		ctx.WriteString(`.`)
		ctx.Quote(sel.Rel.Left.Col.Name)
		ctx.WriteString(`) WITH (`)
		for i, col := range sel.Ti.Columns {
			if i != 0 {
				ctx.WriteString(`, `)
			}
			ctx.Quote(col.Name)
			ctx.WriteString(` `)
			ctx.WriteString(d.mssqlType(col.Type))
			ctx.WriteString(` '$.`)
			ctx.WriteString(col.Name)
			ctx.WriteString(`'`)
		}
		ctx.WriteString(`) AS `)
		t := sel.Ti.Name
		if sel.ID >= 0 {
			t = fmt.Sprintf("%s_%d", t, sel.ID)
		}
		ctx.Quote(t)
	} else if sel.Ti.Type == "function" {
		// Table-valued function call - needs schema prefix and parentheses with args
		if sel.Ti.Func.Schema != "" {
			ctx.Quote(sel.Ti.Func.Schema)
			ctx.WriteString(`.`)
		} else {
			ctx.WriteString(`[dbo].`)
		}
		ctx.Quote(sel.Ti.Name)
		ctx.WriteString(`(`)
		// Render function arguments
		for i, arg := range sel.Args {
			if i != 0 {
				ctx.WriteString(`, `)
			}
			switch arg.Type {
			case qcode.ArgTypeCol:
				r.ColWithTable(arg.Col.Table, arg.Col.Name)
			case qcode.ArgTypeVar:
				ctx.AddParam(Param{Name: arg.Val, Type: arg.DType})
			default:
				ctx.WriteString(arg.Val)
			}
		}
		ctx.WriteString(`)`)
	} else {
		r.RenderTable(sel, sel.Ti.Schema, sel.Ti.Name, false)
	}
}

func (d *MSSQLDialect) renderJoinWithAlias(ctx Context, r InlineChildRenderer, psel, sel *qcode.Select, join qcode.Join) {
	ctx.WriteString(` LEFT JOIN `)
	ctx.Quote(join.Rel.Left.Col.Table)
	ctx.WriteString(` AS `)
	ctx.Quote(fmt.Sprintf("%s_0", join.Rel.Left.Col.Table))
	ctx.WriteString(` ON `)

	t := sel.Ti.Name
	if sel.ID >= 0 {
		t = fmt.Sprintf("%s_%d", t, sel.ID)
	}
	r.ColWithTable(t, join.Rel.Right.Col.Name)
	ctx.WriteString(` = `)
	r.ColWithTable(fmt.Sprintf("%s_0", join.Rel.Left.Col.Table), join.Rel.Left.Col.Name)
}

func (d *MSSQLDialect) renderGroupBy(ctx Context, r InlineChildRenderer, sel *qcode.Select) {
	if !sel.GroupCols || len(sel.BCols) == 0 {
		return
	}
	ctx.WriteString(` GROUP BY `)
	for i, col := range sel.BCols {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		t := sel.Ti.Name
		if sel.ID >= 0 {
			t = fmt.Sprintf("%s_%d", t, sel.ID)
		}
		r.ColWithTable(t, col.Col.Name)
	}
}

func (d *MSSQLDialect) renderOrderBy(ctx Context, r InlineChildRenderer, sel *qcode.Select, alias string) {
	if len(sel.OrderBy) == 0 {
		return
	}
	ctx.WriteString(` ORDER BY `)
	for i, ob := range sel.OrderBy {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		t := ob.Col.Table
		col := ob.Col.Name

		if ob.Var != "" {
			t = fmt.Sprintf("_gj_ob_%s_%s", ob.Col.Table, ob.Col.Name)
			col = "ord"
		} else if alias != "" {
			t = alias
		} else {
			if t == "" {
				t = sel.Ti.Name
			}
			if t == sel.Ti.Name {
				if sel.ID >= 0 {
					t = fmt.Sprintf("%s_%d", t, sel.ID)
				}
			} else {
				t = fmt.Sprintf("%s_0", t)
			}
		}
		r.ColWithTable(t, col)
		switch ob.Order {
		case qcode.OrderAsc:
			ctx.WriteString(` ASC`)
		case qcode.OrderDesc:
			ctx.WriteString(` DESC`)
		}
	}
}

func (d *MSSQLDialect) renderWhereExp(ctx Context, r InlineChildRenderer, psel, sel *qcode.Select, ex *qcode.Exp) {
	d.renderExp(ctx, r, psel, sel, ex)
}

func (d *MSSQLDialect) renderExp(ctx Context, r InlineChildRenderer, psel, sel *qcode.Select, ex *qcode.Exp) {
	if ex == nil {
		return
	}

	switch ex.Op {
	case qcode.OpNop:
		// No-op - don't render anything
		return

	case qcode.OpAnd:
		ctx.WriteString(`(`)
		for i, child := range ex.Children {
			if i > 0 {
				ctx.WriteString(` AND `)
			}
			d.renderExp(ctx, r, psel, sel, child)
		}
		ctx.WriteString(`)`)

	case qcode.OpOr:
		ctx.WriteString(`(`)
		for i, child := range ex.Children {
			if i > 0 {
				ctx.WriteString(` OR `)
			}
			d.renderExp(ctx, r, psel, sel, child)
		}
		ctx.WriteString(`)`)

	case qcode.OpNot:
		ctx.WriteString(`NOT `)
		d.renderExp(ctx, r, psel, sel, ex.Children[0])

	case qcode.OpIsNull:
		ctx.WriteString(`(`)
		d.renderColumn(ctx, r, psel, sel, ex)
		if strings.EqualFold(ex.Right.Val, "false") {
			ctx.WriteString(` IS NOT NULL)`)
		} else {
			ctx.WriteString(` IS NULL)`)
		}

	case qcode.OpIsNotNull:
		ctx.WriteString(`(`)
		d.renderColumn(ctx, r, psel, sel, ex)
		if strings.EqualFold(ex.Right.Val, "false") {
			ctx.WriteString(` IS NULL)`)
		} else {
			ctx.WriteString(` IS NOT NULL)`)
		}

	case qcode.OpEquals, qcode.OpNotEquals, qcode.OpGreaterThan, qcode.OpLesserThan,
		qcode.OpGreaterOrEquals, qcode.OpLesserOrEquals:
		ctx.WriteString(`(`)
		d.renderColumn(ctx, r, psel, sel, ex)
		op, _ := d.RenderOp(ex.Op)
		ctx.WriteString(` `)
		ctx.WriteString(op)
		ctx.WriteString(` `)
		d.renderValue(ctx, r, sel, ex)
		ctx.WriteString(`)`)

	case qcode.OpLike, qcode.OpNotLike, qcode.OpILike, qcode.OpNotILike:
		ctx.WriteString(`(`)
		d.renderColumn(ctx, r, psel, sel, ex)
		op, _ := d.RenderOp(ex.Op)
		ctx.WriteString(` `)
		ctx.WriteString(op)
		ctx.WriteString(` `)
		d.renderValue(ctx, r, sel, ex)
		ctx.WriteString(`)`)

	case qcode.OpHasInCommon:
		// Handle array column overlap operations
		// OpHasInCommon is used when comparing array columns to a list
		d.renderArrayColumnExists(ctx, r, psel, sel, ex, false)

	case qcode.OpIn, qcode.OpNotIn:
		// Handle array column IN operations
		if ex.Left.Col.Array {
			d.renderArrayColumnExists(ctx, r, psel, sel, ex, ex.Op == qcode.OpNotIn)
			return
		}

		// Check for special __gj_json_pk format for bulk JSON inserts
		if ex.Right.ValType == qcode.ValVar &&
			strings.HasPrefix(ex.Right.Val, "__gj_json_pk:gj_sep:") {
			parts := strings.Split(ex.Right.Val, ":gj_sep:")
			if len(parts) == 4 {
				actionVar := parts[1]
				jsonKey := parts[2]
				colType := parts[3]

				ctx.WriteString(`(`)
				// Render column
				d.renderColumn(ctx, r, psel, sel, ex)
				ctx.WriteString(` `)

				if ex.Op == qcode.OpNotIn {
					ctx.WriteString(`NOT `)
				}

				// Render: IN (SELECT [id] FROM OPENJSON(@param) WITH ([id] TYPE '$.key'))
				// Note: OPENJSON automatically iterates arrays, no WrapInArray needed
				ctx.WriteString(`IN (SELECT [`)
				ctx.WriteString(jsonKey)
				ctx.WriteString(`] FROM OPENJSON(`)
				ctx.AddParam(Param{Name: actionVar, Type: "json"})
				ctx.WriteString(`) WITH ([`)
				ctx.WriteString(jsonKey)
				ctx.WriteString(`] `)
				ctx.WriteString(d.mssqlType(colType))
				ctx.WriteString(` '$.`)
				ctx.WriteString(jsonKey)
				ctx.WriteString(`'))`)
				ctx.WriteString(`)`)
				return
			}
		}

		ctx.WriteString(`(`)
		d.renderColumn(ctx, r, psel, sel, ex)
		op, _ := d.RenderOp(ex.Op)
		ctx.WriteString(` `)
		ctx.WriteString(op)
		ctx.WriteString(` `)
		d.renderValue(ctx, r, sel, ex)
		ctx.WriteString(`)`)

	case qcode.OpEqualsTrue, qcode.OpNotEqualsTrue:
		ctx.WriteString(`(`)
		d.renderColumn(ctx, r, psel, sel, ex)
		if ex.Op == qcode.OpEqualsTrue {
			ctx.WriteString(` = 1)`)
		} else {
			ctx.WriteString(` != 1)`)
		}

	case qcode.OpTsQuery:
		ti := sel.Ti
		d.RenderTsQuery(ctx, ti, ex)

	default:
		ctx.WriteString(`(`)
		d.renderColumn(ctx, r, psel, sel, ex)
		op, _ := d.RenderOp(ex.Op)
		ctx.WriteString(` `)
		ctx.WriteString(op)
		ctx.WriteString(` `)
		d.renderValue(ctx, r, sel, ex)
		ctx.WriteString(`)`)
	}
}

func (d *MSSQLDialect) renderColumn(ctx Context, r InlineChildRenderer, psel, sel *qcode.Select, ex *qcode.Exp) {
	if ex.Left.Col.Name == "" {
		return
	}

	if ex.Left.Table == "__cur" {
		colName := ex.Left.Col.Name
		if ex.Left.ColName != "" {
			colName = ex.Left.ColName
		}
		r.ColWithTable("[__cur]", colName)
		return
	}

	if (ex.Left.ID >= 0 && psel != nil && ex.Left.ID == psel.ID) ||
		(ex.Left.ID == -1 && psel != nil && ex.Left.Col.Table == psel.Ti.Name) {
		t := psel.Ti.Name
		if psel.ID >= 0 {
			t = fmt.Sprintf("%s_%d", t, psel.ID)
		}
		r.ColWithTable(t, ex.Left.Col.Name)
		return
	}

	t := ex.Left.Col.Table
	if t == "" {
		t = sel.Ti.Name
	}

	if t == sel.Ti.Name {
		if sel.ID >= 0 {
			t = fmt.Sprintf("%s_%d", t, sel.ID)
		}
	} else if ex.Left.ID >= 0 {
		t = fmt.Sprintf("%s_%d", t, ex.Left.ID)
	}

	colName := ex.Left.Col.Name
	if ex.Left.ColName != "" {
		colName = ex.Left.ColName
	}

	// Handle JSON path operations
	if len(ex.Left.Path) > 0 {
		switch ex.Right.ValType {
		case qcode.ValBool:
			// Cast JSON_VALUE result to BIT for boolean comparison
			ctx.WriteString(`CAST(`)
			d.RenderJSONPath(ctx, t, colName, ex.Left.Path)
			ctx.WriteString(` AS BIT)`)
		case qcode.ValNum:
			// Cast JSON_VALUE result to NUMERIC for number comparison
			ctx.WriteString(`CAST(`)
			d.RenderJSONPath(ctx, t, colName, ex.Left.Path)
			ctx.WriteString(` AS NUMERIC)`)
		default:
			d.RenderJSONPath(ctx, t, colName, ex.Left.Path)
		}
		return
	}

	r.ColWithTable(t, colName)
}

func (d *MSSQLDialect) renderValue(ctx Context, r InlineChildRenderer, sel *qcode.Select, ex *qcode.Exp) {
	// Handle column references (for relationship joins)
	if ex.Right.Col.Name != "" {
		var table string
		if ex.Right.Table != "" {
			table = ex.Right.Table
		} else {
			table = ex.Right.Col.Table
		}

		var colName string
		if ex.Right.ColName != "" {
			colName = ex.Right.ColName
		} else {
			colName = ex.Right.Col.Name
		}

		// Add table ID suffix if the reference is to a parent table
		if ex.Right.ID >= 0 {
			table = fmt.Sprintf("%s_%d", table, ex.Right.ID)
		}
		r.ColWithTable(table, colName)
		return
	}

	switch ex.Right.ValType {
	case qcode.ValDBVar:
		// Database variable - render with @ prefix
		d.RenderVar(ctx, ex.Right.Val)
	case qcode.ValVar:
		ctx.AddParam(Param{Name: ex.Right.Val, Type: ex.Left.Col.Type})
	case qcode.ValNum:
		ctx.WriteString(ex.Right.Val)
	case qcode.ValBool:
		if ex.Right.Val == "true" {
			ctx.WriteString("1")
		} else {
			ctx.WriteString("0")
		}
	case qcode.ValStr:
		ctx.WriteString(`N'`)
		ctx.WriteString(strings.ReplaceAll(ex.Right.Val, "'", "''"))
		ctx.WriteString(`'`)
	case qcode.ValList:
		d.RenderList(ctx, ex)
	default:
		ctx.WriteString(ex.Right.Val)
	}
}

// renderArrayColumnExists renders EXISTS with OPENJSON for array column IN operations
func (d *MSSQLDialect) renderArrayColumnExists(ctx Context, r InlineChildRenderer, psel, sel *qcode.Select, ex *qcode.Exp, isNot bool) {
	// For MSSQL, array columns contain JSON arrays like ["Tag 1", "Tag 2"]
	// We need to check if any element in the column's array exists in the provided list
	if isNot {
		ctx.WriteString(`(NOT `)
	} else {
		ctx.WriteString(`(`)
	}
	ctx.WriteString(`EXISTS (SELECT 1 FROM OPENJSON(`)

	// Render the column with proper table alias
	t := ex.Left.Col.Table
	if t == "" {
		t = sel.Ti.Name
	}
	if t == sel.Ti.Name && sel.ID >= 0 {
		t = fmt.Sprintf("%s_%d", t, sel.ID)
	} else if ex.Left.ID >= 0 {
		t = fmt.Sprintf("%s_%d", t, ex.Left.ID)
	}
	r.ColWithTable(t, ex.Left.Col.Name)

	ctx.WriteString(`) WHERE [value] IN (`)

	if ex.Right.ValType == qcode.ValVar {
		// Variable list: use OPENJSON to unpack
		ctx.WriteString(`SELECT [value] FROM OPENJSON(`)
		ctx.AddParam(Param{Name: ex.Right.Val, Type: "json", IsArray: true})
		ctx.WriteString(`)`)
	} else if ex.Right.ValType == qcode.ValList {
		// Static list: render inline values
		for i := range ex.Right.ListVal {
			if i != 0 {
				ctx.WriteString(`, `)
			}
			d.RenderLiteral(ctx, ex.Right.ListVal[i], ex.Right.ListType)
		}
	}
	ctx.WriteString(`)))`)
}

func (d *MSSQLDialect) findSkipVarExp(exp *qcode.Exp) (varName string, isSkip bool, found bool) {
	if exp == nil {
		return "", false, false
	}

	switch exp.Op {
	case qcode.OpEqualsTrue:
		if exp.Right.ValType == qcode.ValVar {
			return exp.Right.Val, false, true
		}
	case qcode.OpNotEqualsTrue:
		if exp.Right.ValType == qcode.ValVar {
			return exp.Right.Val, true, true
		}
	case qcode.OpAnd:
		if len(exp.Children) > 0 {
			return d.findSkipVarExp(exp.Children[0])
		}
	}

	return "", false, false
}

// Mutation methods

func (d *MSSQLDialect) RenderMutationCTE(ctx Context, m *qcode.Mutate, renderBody func()) {
	ctx.Quote(m.Ti.Name)
	ctx.WriteString(` AS (`)
	renderBody()
	ctx.WriteString(`)`)
}

func (d *MSSQLDialect) RenderMutationInput(ctx Context, qc *qcode.QCode) {
	// MSSQL mutation input handling
}

func (d *MSSQLDialect) RenderMutationPostamble(ctx Context, qc *qcode.QCode) {
	GenericRenderMutationPostamble(ctx, qc)
}

func (d *MSSQLDialect) RenderInsert(ctx Context, m *qcode.Mutate, values func()) {
	ctx.WriteString(`INSERT INTO `)
	if m.Ti.Schema != "" && m.Ti.Schema != "dbo" {
		ctx.Quote(m.Ti.Schema)
		ctx.WriteString(`.`)
	}
	ctx.Quote(m.Ti.Name)
	ctx.WriteString(` (`)
	for i, col := range m.Cols {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.Quote(col.Col.Name)
	}
	ctx.WriteString(`) `)
	ctx.WriteString(`OUTPUT INSERTED.* `)
	values()
}

func (d *MSSQLDialect) RenderUpdate(ctx Context, m *qcode.Mutate, set func(), from func(), where func()) {
	ctx.WriteString(`UPDATE `)
	ctx.Quote(m.Ti.Name)
	ctx.WriteString(` SET `)
	set()
	ctx.WriteString(` OUTPUT INSERTED.* `)
	if from != nil {
		from()
	}
	if where != nil {
		ctx.WriteString(` WHERE `)
		where()
	}
}

func (d *MSSQLDialect) RenderDelete(ctx Context, m *qcode.Mutate, where func()) {
	ctx.WriteString(`DELETE FROM `)
	if m.Ti.Schema != "" && m.Ti.Schema != "dbo" {
		ctx.Quote(m.Ti.Schema)
		ctx.WriteString(`.`)
	}
	ctx.Quote(m.Ti.Name)
	ctx.WriteString(` OUTPUT DELETED.* `)
	if where != nil {
		ctx.WriteString(` WHERE `)
		where()
	}
}

func (d *MSSQLDialect) RenderUpsert(ctx Context, m *qcode.Mutate, insert func(), updateSet func()) {
	// MSSQL uses MERGE for upsert
	ctx.WriteString(`MERGE INTO `)
	if m.Ti.Schema != "" && m.Ti.Schema != "dbo" {
		ctx.Quote(m.Ti.Schema)
		ctx.WriteString(`.`)
	}
	ctx.Quote(m.Ti.Name)
	ctx.WriteString(` AS target USING (SELECT `)
	insert()
	ctx.WriteString(`) AS source ON target.`)
	ctx.Quote(m.Ti.PrimaryCol.Name)
	ctx.WriteString(` = source.`)
	ctx.Quote(m.Ti.PrimaryCol.Name)
	ctx.WriteString(` WHEN MATCHED THEN UPDATE SET `)
	updateSet()
	ctx.WriteString(` WHEN NOT MATCHED THEN INSERT (`)
	for i, col := range m.Cols {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.Quote(col.Col.Name)
	}
	ctx.WriteString(`) VALUES (`)
	for i, col := range m.Cols {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.WriteString(`source.`)
		ctx.Quote(col.Col.Name)
	}
	ctx.WriteString(`) OUTPUT INSERTED.*;`)
}

func (d *MSSQLDialect) RenderReturning(ctx Context, m *qcode.Mutate) {
	// MSSQL uses OUTPUT clause inline, not RETURNING
}

func (d *MSSQLDialect) RenderAssign(ctx Context, col string, val string) {
	ctx.Quote(col)
	ctx.WriteString(` = `)
	ctx.WriteString(val)
}

func (d *MSSQLDialect) RenderCast(ctx Context, val func(), typ string) {
	ctx.WriteString(`CAST(`)
	val()
	ctx.WriteString(` AS `)
	ctx.WriteString(d.mssqlType(typ))
	ctx.WriteString(`)`)
}

func (d *MSSQLDialect) RenderTryCast(ctx Context, val func(), typ string) {
	ctx.WriteString(`TRY_CAST(`)
	val()
	ctx.WriteString(` AS `)
	ctx.WriteString(d.mssqlType(typ))
	ctx.WriteString(`)`)
}

func (d *MSSQLDialect) RenderSubscriptionUnbox(ctx Context, params []Param, innerSQL string) {
	// MSSQL subscription unboxing using OPENJSON
	sql := strings.TrimSpace(innerSQL)
	if strings.HasPrefix(sql, "/*") {
		if end := strings.Index(sql, "*/"); end != -1 {
			sql = strings.TrimSpace(sql[end+2:])
		}
	}

	ctx.WriteString(`WITH [_gj_sub] AS (SELECT * FROM OPENJSON(?) WITH (`)
	for i, p := range params {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.Quote(p.Name)
		ctx.WriteString(` `)
		ctx.WriteString(d.mssqlType(p.Type))
		ctx.WriteString(` '$[`)
		ctx.Write(fmt.Sprintf("%d", i))
		ctx.WriteString(`]'`)
	}
	ctx.WriteString(`)) SELECT [_gj_sub_data].[__root] FROM [_gj_sub] CROSS APPLY (`)
	ctx.WriteString(sql)
	ctx.WriteString(`) AS [_gj_sub_data]`)
}

// Linear execution methods

func (d *MSSQLDialect) RenderIDCapture(ctx Context, varName string) {
	ctx.WriteString(`SET @`)
	ctx.WriteString(varName)
	ctx.WriteString(` = SCOPE_IDENTITY();`)
}

func (d *MSSQLDialect) RenderVar(ctx Context, name string) {
	ctx.WriteString(`@`)
	ctx.WriteString(name)
}

func (d *MSSQLDialect) RenderSetup(ctx Context) {
	// MSSQL setup - variable declarations will be added here
}

func (d *MSSQLDialect) RenderBegin(ctx Context) {
	// MSSQL doesn't need explicit BEGIN for this
}

func (d *MSSQLDialect) RenderTeardown(ctx Context) {
	// MSSQL teardown
}

func (d *MSSQLDialect) RenderVarDeclaration(ctx Context, name, typeName string) {
	ctx.WriteString(`DECLARE @`)
	ctx.WriteString(name)
	ctx.WriteString(` `)
	ctx.WriteString(d.mssqlType(typeName))
	ctx.WriteString(`;`)
}

func (d *MSSQLDialect) RenderMutateToRecordSet(ctx Context, m *qcode.Mutate, n int, renderRoot func()) {
	if n != 0 {
		ctx.WriteString(`, `)
	}

	// For MSSQL we use OPENJSON WITH to convert JSON input to a derived table
	// Wrap in subquery to avoid potential parser issues in UPDATE statements
	ctx.WriteString(`(SELECT * FROM OPENJSON(`)

	if len(m.Path) > 0 {
		ctx.WriteString(`JSON_QUERY(`)
		renderRoot()
		ctx.WriteString(`, '$.`)
		for i, p := range m.Path {
			if i > 0 {
				ctx.WriteString(`.`)
			}
			ctx.WriteString(p)
		}
		ctx.WriteString(`')`)
	} else {
		renderRoot()
	}

	ctx.WriteString(`) WITH (`)

	i := 0
	hasPK := false
	for _, col := range m.Cols {
		// Skip preset columns - they get values from parameters, not JSON input
		if col.Set {
			continue
		}
		if col.FieldName == m.Ti.PrimaryCol.Name {
			hasPK = true
		}
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.Quote(col.FieldName)
		ctx.WriteString(` `)

		// Check if the field value is an array/object - use NVARCHAR(MAX) AS JSON for those
		isJSONValue := false
		if m.Data != nil && m.Data.CMap != nil {
			if field, ok := m.Data.CMap[col.FieldName]; ok {
				isJSONValue = field.Type == graph.NodeList || field.Type == graph.NodeObj
			}
		}

		if isJSONValue {
			ctx.WriteString("NVARCHAR(MAX) '$.")
			ctx.WriteString(col.FieldName)
			ctx.WriteString(`' AS JSON`)
		} else {
			// Map types for MSSQL OPENJSON WITH columns
			ctx.WriteString(d.mssqlType(col.Col.Type))
			ctx.WriteString(` '$.`)
			ctx.WriteString(col.FieldName)
			ctx.WriteString(`'`)
		}
		i++
	}

	if !hasPK {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.Quote(m.Ti.PrimaryCol.Name)
		ctx.WriteString(` `)
		ctx.WriteString(d.mssqlType(m.Ti.PrimaryCol.Type))
		ctx.WriteString(` '$.`)
		ctx.WriteString(m.Ti.PrimaryCol.Name)
		ctx.WriteString(`'`)
	}

	ctx.WriteString(`)) AS t`)
}

func (d *MSSQLDialect) RenderSetSessionVar(ctx Context, name, value string) bool {
	return false
}

func (d *MSSQLDialect) RenderLinearInsert(ctx Context, m *qcode.Mutate, qc *qcode.QCode, varName string, renderColVal func(qcode.MColumn)) {
	// For linear execution, we don't use OUTPUT INSERTED.* because we need to capture
	// the ID into a variable using SCOPE_IDENTITY()
	ctx.WriteString(`INSERT INTO `)
	if m.Ti.Schema != "" && m.Ti.Schema != "dbo" {
		ctx.Quote(m.Ti.Schema)
		ctx.WriteString(`.`)
	}
	ctx.Quote(m.Ti.Name)
	ctx.WriteString(` (`)

	i := 0
	hasExplicitPK := false
	pkFieldName := ""
	for _, col := range m.Cols {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.Quote(col.Col.Name)
		if col.Col.Name == m.Ti.PrimaryCol.Name {
			hasExplicitPK = true
			pkFieldName = col.FieldName
		}
		i++
	}
	for _, rcol := range m.RCols {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.Quote(rcol.Col.Name)
		i++
	}
	ctx.WriteString(`)`)

	if m.IsJSON {
		// Bulk insert from JSON array using OPENJSON
		ctx.WriteString(` SELECT `)

		i = 0
		for _, col := range m.Cols {
			if i != 0 {
				ctx.WriteString(`, `)
			}
			if col.Set {
				// Preset columns: use the preset value, not from OPENJSON
				renderColVal(col)
			} else {
				// Reference column from the derived table 't' created by OPENJSON
				ctx.ColWithTable("t", col.FieldName)
			}
			i++
		}
		for _, rcol := range m.RCols {
			if i != 0 {
				ctx.WriteString(`, `)
			}
			// Find the parent mutation that provides this value
			found := false
			for id := range m.DependsOn {
				if qc.Mutates[id].Ti.Name == rcol.VCol.Table {
					depM := qc.Mutates[id]
					depVarName := depM.Ti.Name + "_" + fmt.Sprintf("%d", depM.ID)
					d.RenderVar(ctx, depVarName)
					found = true
					break
				}
			}
			if !found {
				ctx.WriteString("NULL")
			}
			i++
		}

		ctx.WriteString(` FROM `)
		d.RenderMutateToRecordSet(ctx, m, 0, func() {
			ctx.AddParam(Param{Name: qc.ActionVar, Type: "json"})
		})
		ctx.WriteString(`; `)

		// Capture the ID for dependent mutations
		if !hasExplicitPK {
			// Use SCOPE_IDENTITY() for auto-generated IDs
			d.RenderIDCapture(ctx, varName)
		} else {
			// For explicit PK in JSON, capture using JSON_VALUE
			// This is needed for dependent mutations that reference this ID
			ctx.WriteString(`SET @`)
			ctx.WriteString(varName)
			ctx.WriteString(` = JSON_VALUE(`)
			ctx.AddParam(Param{Name: qc.ActionVar, Type: "json"})
			ctx.WriteString(`, '$.`)
			// Build JSON path to the PK field
			for _, p := range m.Path {
				ctx.WriteString(p)
				ctx.WriteString(`.`)
			}
			ctx.WriteString(pkFieldName)
			ctx.WriteString(`'); `)
		}
	} else {
		// Single row insert with VALUES
		ctx.WriteString(` VALUES (`)

		i = 0
		for _, col := range m.Cols {
			if i != 0 {
				ctx.WriteString(`, `)
			}
			renderColVal(col)
			i++
		}
		for _, rcol := range m.RCols {
			if i != 0 {
				ctx.WriteString(`, `)
			}
			// Find the parent mutation that provides this value
			found := false
			for id := range m.DependsOn {
				if qc.Mutates[id].Ti.Name == rcol.VCol.Table {
					depM := qc.Mutates[id]
					depVarName := depM.Ti.Name + "_" + fmt.Sprintf("%d", depM.ID)
					d.RenderVar(ctx, depVarName)
					found = true
					break
				}
			}
			if !found {
				ctx.WriteString("NULL")
			}
			i++
		}
		ctx.WriteString(`); `)

		// Capture the inserted ID
		if !hasExplicitPK {
			// Use SCOPE_IDENTITY() for auto-generated IDs
			d.RenderIDCapture(ctx, varName)
		} else {
			// For explicit PK, we need to find the PK column value and set the variable
			// Find the PK column and set the variable to its value
			for _, col := range m.Cols {
				if col.Col.Name == m.Ti.PrimaryCol.Name {
					ctx.WriteString(`SET @`)
					ctx.WriteString(varName)
					ctx.WriteString(` = `)
					renderColVal(col)
					ctx.WriteString(`; `)
					break
				}
			}
		}
	}
}

func (d *MSSQLDialect) RenderLinearUpdate(ctx Context, m *qcode.Mutate, qc *qcode.QCode, varName string, renderColVal func(qcode.MColumn), renderWhere func()) {
	d.RenderUpdate(ctx, m, func() {
		for i, col := range m.Cols {
			if i != 0 {
				ctx.WriteString(`, `)
			}
			ctx.Quote(col.Col.Name)
			ctx.WriteString(` = `)
			renderColVal(col)
		}
	}, nil, renderWhere)
}

func (d *MSSQLDialect) RenderLinearConnect(ctx Context, m *qcode.Mutate, qc *qcode.QCode, varName string, renderFilter func()) {
	// Select the ID(s) matching the filter and store in variable
	// For MSSQL: SET @var = (SELECT id FROM table WHERE filter)
	ctx.WriteString(`SET @`)
	ctx.WriteString(varName)
	ctx.WriteString(` = (SELECT `)
	ctx.Quote(m.Rel.Left.Col.Name)
	ctx.WriteString(` FROM `)
	ctx.ColWithTable(m.Ti.Schema, m.Ti.Name)
	ctx.WriteString(` WHERE `)
	renderFilter()
	ctx.WriteString(`); `)

	// If One-to-Many relationship and parent exists, update child table
	var parentVar string
	for id := range m.DependsOn {
		if qc.Mutates[id].Ti.Name == m.Rel.Right.Col.Table {
			parentVar = qc.Mutates[id].Ti.Name + "_" + fmt.Sprintf("%d", qc.Mutates[id].ID)
			break
		}
	}

	if parentVar != "" {
		// UPDATE table SET fk_col = @parentVar WHERE filter
		ctx.WriteString(`UPDATE `)
		ctx.ColWithTable(m.Ti.Schema, m.Ti.Name)
		ctx.WriteString(` SET `)
		ctx.Quote(m.Rel.Left.Col.Name)
		ctx.WriteString(` = @`)
		ctx.WriteString(parentVar)
		ctx.WriteString(` WHERE `)
		renderFilter()
		ctx.WriteString(`; `)
	}
}

func (d *MSSQLDialect) RenderLinearDisconnect(ctx Context, m *qcode.Mutate, qc *qcode.QCode, varName string, renderFilter func()) {
	// Disconnect operation for MSSQL
}

func (d *MSSQLDialect) ModifySelectsForMutation(qc *qcode.QCode) {
	if qc.Type != qcode.QTMutation || qc.Selects == nil {
		return
	}

	// For MSSQL, we need to inject a WHERE clause to filter by the captured IDs
	// The IDs are captured via SET @tablename_N = SCOPE_IDENTITY() or explicit value
	for i := range qc.Selects {
		sel := &qc.Selects[i]

		// Only modify the root-level selects that correspond to mutated tables
		if sel.ParentID != -1 {
			continue
		}

		// Collect ALL mutations for this table
		var mutations []qcode.Mutate
		for _, m := range qc.Mutates {
			if m.Ti.Name == sel.Table && (m.Type == qcode.MTInsert || m.Type == qcode.MTUpdate || m.Type == qcode.MTUpsert) {
				mutations = append(mutations, m)
			}
		}

		if len(mutations) == 0 {
			continue
		}

		// If the user provided a WHERE clause, don't override it
		if sel.Where.Exp != nil {
			continue
		}

		var exp *qcode.Exp

		// Special handling for JSON bulk inserts
		if len(mutations) == 1 && mutations[0].IsJSON {
			m := mutations[0]

			// Check if PK is provided in JSON input
			hasExplicitPK := false
			var pkName string
			for _, col := range m.Cols {
				if col.Col.Name == m.Ti.PrimaryCol.Name {
					hasExplicitPK = true
					pkName = col.FieldName
					break
				}
			}

			if hasExplicitPK {
				// Filter by IDs from JSON: WHERE id IN (SELECT ... FROM OPENJSON(...))
				exp = &qcode.Exp{Op: qcode.OpIn}
				col := m.Ti.PrimaryCol
				col.Table = m.Ti.Name
				exp.Left.Col = col
				exp.Left.ID = -1
				exp.Right.ValType = qcode.ValVar
				// Special format for RenderValVar to parse
				exp.Right.Val = fmt.Sprintf("__gj_json_pk:gj_sep:%s:gj_sep:%s:gj_sep:%s", qc.ActionVar, pkName, m.Ti.PrimaryCol.Type)
			} else {
				// Auto-generated PKs - use captured variable (existing behavior)
				varName := m.Ti.Name + "_" + fmt.Sprintf("%d", m.ID)
				exp = &qcode.Exp{Op: qcode.OpEquals}
				col := m.Ti.PrimaryCol
				col.Table = m.Ti.Name
				exp.Left.Col = col
				exp.Left.ID = -1
				exp.Right.ValType = qcode.ValDBVar
				exp.Right.Val = varName
			}
		} else if len(mutations) == 1 {
			// Single non-JSON mutation - filter by id = @varName
			m := mutations[0]
			varName := m.Ti.Name + "_" + fmt.Sprintf("%d", m.ID)
			exp = &qcode.Exp{Op: qcode.OpEquals}
			col := m.Ti.PrimaryCol
			col.Table = m.Ti.Name
			exp.Left.Col = col
			exp.Left.ID = -1
			exp.Right.ValType = qcode.ValDBVar
			exp.Right.Val = varName
		} else {
			// Multiple mutations - filter by id IN (@var1, @var2, ...)
			m := mutations[0]
			exp = &qcode.Exp{Op: qcode.OpIn}
			col := m.Ti.PrimaryCol
			col.Table = m.Ti.Name
			exp.Left.Col = col
			exp.Left.ID = -1
			exp.Right.ValType = qcode.ValList
			exp.Right.ListType = qcode.ValDBVar
			for _, mut := range mutations {
				varName := mut.Ti.Name + "_" + fmt.Sprintf("%d", mut.ID)
				exp.Right.ListVal = append(exp.Right.ListVal, varName)
			}
		}

		// Set the WHERE clause
		if exp != nil {
			sel.Where.Exp = exp
		}
	}
}

func (d *MSSQLDialect) RenderQueryPrefix(ctx Context, qc *qcode.QCode) {
	// MSSQL query prefix
}

// Helper function to convert types to MSSQL equivalents
func (d *MSSQLDialect) mssqlType(t string) string {
	tLower := strings.ToLower(t)

	// Handle types with length specifications like nvarchar(255)
	if strings.HasPrefix(tLower, "nvarchar") {
		return "NVARCHAR(MAX)"
	}
	if strings.HasPrefix(tLower, "varchar") {
		return "NVARCHAR(MAX)"
	}
	if strings.HasPrefix(tLower, "nchar") {
		return "NVARCHAR(MAX)"
	}
	if strings.HasPrefix(tLower, "decimal") || strings.HasPrefix(tLower, "numeric") {
		return "DECIMAL(18,6)"
	}

	switch tLower {
	case "int", "integer", "int4":
		return "INT"
	case "int8", "bigint":
		return "BIGINT"
	case "smallint", "int2":
		return "SMALLINT"
	case "float", "float4", "real":
		return "REAL"
	case "float8", "double", "double precision":
		return "FLOAT"
	case "text", "character varying", "string":
		return "NVARCHAR(MAX)"
	case "char", "character":
		return "NCHAR(1)"
	case "boolean", "bool", "bit":
		return "BIT"
	case "timestamp", "timestamptz", "timestamp without time zone", "timestamp with time zone", "datetime", "datetime2":
		return "DATETIME2"
	case "date":
		return "DATE"
	case "time", "timetz":
		return "TIME"
	case "json", "jsonb":
		return "NVARCHAR(MAX)"
	case "uuid", "uniqueidentifier":
		return "UNIQUEIDENTIFIER"
	case "bytea", "binary", "varbinary":
		return "VARBINARY(MAX)"
	default:
		return strings.ToUpper(t)
	}
}

func (d *MSSQLDialect) renderFieldFilterExp(ctx Context, r InlineChildRenderer, sel *qcode.Select, ex *qcode.Exp) {
	// Field filter expression rendering for @skip/@include directives
	if ex == nil {
		return
	}

	t := sel.Ti.Name
	if sel.ID >= 0 {
		t = fmt.Sprintf("%s_%d", t, sel.ID)
	}

	switch ex.Op {
	case qcode.OpNot:
		ctx.WriteString(`NOT `)
		d.renderFieldFilterExp(ctx, r, sel, ex.Children[0])

	case qcode.OpAnd:
		ctx.WriteString(`(`)
		for i, child := range ex.Children {
			if i > 0 {
				ctx.WriteString(` AND `)
			}
			d.renderFieldFilterExp(ctx, r, sel, child)
		}
		ctx.WriteString(`)`)

	case qcode.OpOr:
		ctx.WriteString(`(`)
		for i, child := range ex.Children {
			if i > 0 {
				ctx.WriteString(` OR `)
			}
			d.renderFieldFilterExp(ctx, r, sel, child)
		}
		ctx.WriteString(`)`)

	case qcode.OpEquals:
		ctx.WriteString(`(`)
		r.ColWithTable(t, ex.Left.Col.Name)
		ctx.WriteString(`) = `)
		d.renderFieldFilterVal(ctx, ex)

	case qcode.OpNotEquals:
		ctx.WriteString(`(`)
		r.ColWithTable(t, ex.Left.Col.Name)
		ctx.WriteString(`) != `)
		d.renderFieldFilterVal(ctx, ex)

	case qcode.OpGreaterThan:
		ctx.WriteString(`(`)
		r.ColWithTable(t, ex.Left.Col.Name)
		ctx.WriteString(`) > `)
		d.renderFieldFilterVal(ctx, ex)

	case qcode.OpLesserThan:
		ctx.WriteString(`(`)
		r.ColWithTable(t, ex.Left.Col.Name)
		ctx.WriteString(`) < `)
		d.renderFieldFilterVal(ctx, ex)

	case qcode.OpGreaterOrEquals:
		ctx.WriteString(`(`)
		r.ColWithTable(t, ex.Left.Col.Name)
		ctx.WriteString(`) >= `)
		d.renderFieldFilterVal(ctx, ex)

	case qcode.OpLesserOrEquals:
		ctx.WriteString(`(`)
		r.ColWithTable(t, ex.Left.Col.Name)
		ctx.WriteString(`) <= `)
		d.renderFieldFilterVal(ctx, ex)

	case qcode.OpIsNull:
		ctx.WriteString(`(`)
		r.ColWithTable(t, ex.Left.Col.Name)
		if strings.EqualFold(ex.Right.Val, "false") {
			ctx.WriteString(`) IS NOT NULL`)
		} else {
			ctx.WriteString(`) IS NULL`)
		}

	case qcode.OpEqualsTrue:
		// For @include(ifVar: $varName) - show when variable is true
		ctx.WriteString(`(`)
		ctx.AddParam(Param{Name: ex.Right.Val, Type: "bit"})
		ctx.WriteString(` = 1)`)

	case qcode.OpNotEqualsTrue:
		// For @skip(ifVar: $varName) - show when variable is NOT true (i.e., skip when true)
		ctx.WriteString(`(`)
		ctx.AddParam(Param{Name: ex.Right.Val, Type: "bit"})
		ctx.WriteString(` != 1)`)

	default:
		// Fallback: just render 1=1 (true) for unsupported ops
		ctx.WriteString(`1=1`)
	}
}

// renderFieldFilterVal renders the right-hand side value for field filter expressions
func (d *MSSQLDialect) renderFieldFilterVal(ctx Context, ex *qcode.Exp) {
	switch ex.Right.ValType {
	case qcode.ValStr:
		ctx.WriteString(`'`)
		ctx.WriteString(ex.Right.Val)
		ctx.WriteString(`'`)
	case qcode.ValNum, qcode.ValBool:
		ctx.WriteString(ex.Right.Val)
	case qcode.ValVar:
		ctx.AddParam(Param{Name: ex.Right.Val, Type: ex.Left.Col.Type})
	default:
		ctx.WriteString(ex.Right.Val)
	}
}

// isRecursiveRelationshipExp checks if an expression references __rcte_ tables
// (recursive CTE tables from qcode that need special handling in MSSQL)
// Also returns true for OpNop which should be skipped
func (d *MSSQLDialect) isRecursiveRelationshipExp(exp *qcode.Exp) bool {
	if exp == nil {
		return false
	}
	// OpNop should be skipped (treated as a "recursive" expression to filter out)
	if exp.Op == qcode.OpNop {
		return true
	}
	// Check if left side references __rcte_ table
	if exp.Left.Table != "" && len(exp.Left.Table) > 7 && exp.Left.Table[:7] == "__rcte_" {
		return true
	}
	// Check if right side references __rcte_ table
	if exp.Right.Table != "" && len(exp.Right.Table) > 7 && exp.Right.Table[:7] == "__rcte_" {
		return true
	}
	return false
}

// hasNonRecursiveChildren checks if an AND/OR expression has children that are not recursive relationship filters
func (d *MSSQLDialect) hasNonRecursiveChildren(exp *qcode.Exp) bool {
	if exp == nil {
		return false
	}
	if exp.Op == qcode.OpAnd || exp.Op == qcode.OpOr {
		for _, child := range exp.Children {
			if !d.isRecursiveRelationshipExp(child) {
				return true
			}
		}
		return false
	}
	return !d.isRecursiveRelationshipExp(exp)
}

// renderRecursiveBaseWhere renders WHERE clause for recursive CTE base case,
// filtering out recursive relationship conditions (those referencing __rcte_ tables)
func (d *MSSQLDialect) renderRecursiveBaseWhere(ctx Context, r InlineChildRenderer, psel, sel *qcode.Select, exp *qcode.Exp) {
	if exp == nil {
		return
	}

	if exp.Op == qcode.OpAnd {
		first := true
		for _, child := range exp.Children {
			if d.isRecursiveRelationshipExp(child) {
				continue
			}
			if !first {
				ctx.WriteString(` AND `)
			}
			d.renderExp(ctx, r, psel, sel, child)
			first = false
		}
	} else if exp.Op == qcode.OpOr {
		first := true
		for _, child := range exp.Children {
			if d.isRecursiveRelationshipExp(child) {
				continue
			}
			if !first {
				ctx.WriteString(` OR `)
			}
			d.renderExp(ctx, r, psel, sel, child)
			first = false
		}
	} else if !d.isRecursiveRelationshipExp(exp) {
		d.renderExp(ctx, r, psel, sel, exp)
	}
}
