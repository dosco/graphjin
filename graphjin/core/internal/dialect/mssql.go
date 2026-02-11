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
	NameMap         map[string]string // normalized→original identifier mapping
}

func (d *MSSQLDialect) Name() string {
	return "mssql"
}

func (d *MSSQLDialect) QuoteIdentifier(s string) string {
	if d.NameMap != nil {
		if orig, ok := d.NameMap[s]; ok {
			return "[" + orig + "]"
		}
	}
	return "[" + s + "]"
}

// SetNameMap builds a normalized→original name mapping from discovered tables.
func (d *MSSQLDialect) SetNameMap(tables []sdata.DBTable) {
	d.NameMap = make(map[string]string)
	for _, t := range tables {
		if t.OrigName != "" && t.OrigName != t.Name {
			d.NameMap[t.Name] = t.OrigName
		}
		if t.OrigSchema != "" && t.OrigSchema != t.Schema {
			d.NameMap[t.Schema] = t.OrigSchema
		}
		for _, c := range t.Columns {
			if c.OrigName != "" && c.OrigName != c.Name {
				d.NameMap[c.Name] = c.OrigName
			}
			if c.OrigFKeyCol != "" && c.OrigFKeyCol != c.FKeyCol {
				d.NameMap[c.FKeyCol] = c.OrigFKeyCol
			}
			if c.OrigFKeyTable != "" && c.OrigFKeyTable != c.FKeyTable {
				d.NameMap[c.FKeyTable] = c.OrigFKeyTable
			}
			if c.OrigFKeySchema != "" && c.OrigFKeySchema != c.FKeySchema {
				d.NameMap[c.FKeySchema] = c.OrigFKeySchema
			}
		}
	}
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
	for _, ob := range sel.OrderBy {
		if ob.Var != "" {
			// MSSQL: Use OPENJSON to parse the order by array
			// OPENJSON returns [key] (array index) and [value] (element value)
			ctx.WriteString(` JOIN (SELECT CAST([value] AS `)
			ctx.WriteString(d.mssqlType(ob.Col.Type))
			ctx.WriteString(`) AS [id], CAST([key] AS INT) AS [ord] FROM OPENJSON(`)
			ctx.AddParam(Param{Name: ob.Var, Type: "json"})
			ctx.WriteString(`)) AS [_gj_ob_`)
			ctx.WriteString(ob.Col.Table)
			ctx.WriteString(`_`)
			ctx.WriteString(ob.Col.Name)
			ctx.WriteString(`] ON [_gj_ob_`)
			ctx.WriteString(ob.Col.Table)
			ctx.WriteString(`_`)
			ctx.WriteString(ob.Col.Name)
			ctx.WriteString(`].[id] = `)
			// Use aliased table name for the join condition
			t := sel.Ti.Name
			if sel.ID >= 0 {
				t = fmt.Sprintf("%s_%d", t, sel.ID)
			}
			ctx.ColWithTable(t, ob.Col.Name)
		}
	}
}

func (d *MSSQLDialect) RenderCursorCTE(ctx Context, sel *qcode.Select) {
	if !sel.Paging.Cursor {
		return
	}
	// Parse cursor value: format is "selectID:val1:val2:..."
	// The gj-hexTimestamp: prefix SHOULD be stripped during encryption/decryption,
	// but if encryption fails, the cursor may still contain the prefix.
	// To be robust, we strip the prefix if present before parsing.
	// For example: "0:110.50:1" -> selID="0", val1="110.50", val2="1"
	// For element i, we extract from position (i+1) after colon (i+1)

	// Use a single CTE with inline prefix stripping using CROSS APPLY
	// The CROSS APPLY ensures the cleaned cursor is computed once and accessible
	ctx.WriteString(`WITH [__cur] AS (SELECT `)
	for i, ob := range sel.OrderBy {
		if i != 0 {
			ctx.WriteString(`, `)
		}

		// TRY_CAST the parsed value to the column type
		// Use the cleaned cursor [c].[v] which has the prefix stripped
		ctx.WriteString(`TRY_CAST(NULLIF(CASE WHEN [c].[v] IS NULL OR LEN([c].[v]) = 0 THEN NULL ELSE `)

		// For element i, extract from colon #(i+1) to colon #(i+2) or end of string
		ctx.WriteString(`SUBSTRING([c].[v], `)
		// start position = position of colon #(i+1) + 1
		d.renderNthColonPosFromClean(ctx, i+1)
		ctx.WriteString(` + 1, `)
		// length = position of colon #(i+2) - position of colon #(i+1) - 1, or to end of string
		ctx.WriteString(`ISNULL(NULLIF(`)
		d.renderNthColonPosFromClean(ctx, i+2)
		ctx.WriteString(`, 0), LEN([c].[v]) + 1) - `)
		d.renderNthColonPosFromClean(ctx, i+1)
		ctx.WriteString(` - 1)`)

		ctx.WriteString(` END, '') AS `)
		// Cast to column type
		ctx.WriteString(d.mssqlType(ob.Col.Type))
		ctx.WriteString(`) AS `)

		if ob.KeyVar != "" && ob.Key != "" {
			ctx.Quote(ob.Col.Name + "_" + ob.Key)
		} else {
			ctx.Quote(ob.Col.Name)
		}
	}
	// Use VALUES to pass the cursor parameter once, then CROSS APPLY to strip prefix
	// This avoids multiple parameter placeholders for the same value
	ctx.WriteString(` FROM (VALUES (`)
	cursorVar := sel.Paging.CursorVar
	if cursorVar == "" {
		cursorVar = "cursor"
	}
	ctx.AddParam(Param{Name: cursorVar, Type: "text"})
	ctx.WriteString(`)) AS [_p]([v]) CROSS APPLY (SELECT CASE WHEN [_p].[v] LIKE 'gj-%' THEN STUFF([_p].[v], 1, CHARINDEX(':', [_p].[v], 4), '') ELSE [_p].[v] END AS [v]) AS [c]) `)
}

// renderNthColonPos renders a CHARINDEX expression to find the position of the n-th colon
// in the cursor parameter. Returns 0 if there aren't enough colons.
func (d *MSSQLDialect) renderNthColonPos(ctx Context, n int) {
	if n <= 0 {
		ctx.WriteString(`0`)
		return
	}
	if n == 1 {
		ctx.WriteString(`CHARINDEX(':', `)
		ctx.AddParam(Param{Name: "cursor", Type: "text"})
		ctx.WriteString(`)`)
		return
	}
	// For n > 1, we need to nest: CHARINDEX(':', @cursor, prev_pos + 1)
	// where prev_pos is the position of colon (n-1)
	ctx.WriteString(`CHARINDEX(':', `)
	ctx.AddParam(Param{Name: "cursor", Type: "text"})
	ctx.WriteString(`, `)
	d.renderNthColonPos(ctx, n-1)
	ctx.WriteString(` + 1)`)
}

// renderNthColonPosFromCol renders a CHARINDEX expression to find the position of the n-th colon
// in the [c].[cursor] column (from the __cur_clean CTE). Returns 0 if there aren't enough colons.
func (d *MSSQLDialect) renderNthColonPosFromCol(ctx Context, n int) {
	if n <= 0 {
		ctx.WriteString(`0`)
		return
	}
	if n == 1 {
		ctx.WriteString(`CHARINDEX(':', [c].[cursor])`)
		return
	}
	// For n > 1, we need to nest: CHARINDEX(':', [c].[cursor], prev_pos + 1)
	// where prev_pos is the position of colon (n-1)
	ctx.WriteString(`CHARINDEX(':', [c].[cursor], `)
	d.renderNthColonPosFromCol(ctx, n-1)
	ctx.WriteString(` + 1)`)
}

// renderNthColonPosInline renders a CHARINDEX expression to find the position of the n-th colon
// in the [cursor] column (inline within the __cur CTE). Returns 0 if there aren't enough colons.
func (d *MSSQLDialect) renderNthColonPosInline(ctx Context, n int) {
	if n <= 0 {
		ctx.WriteString(`0`)
		return
	}
	if n == 1 {
		ctx.WriteString(`CHARINDEX(':', [cursor])`)
		return
	}
	// For n > 1, we need to nest: CHARINDEX(':', [cursor], prev_pos + 1)
	// where prev_pos is the position of colon (n-1)
	ctx.WriteString(`CHARINDEX(':', [cursor], `)
	d.renderNthColonPosInline(ctx, n-1)
	ctx.WriteString(` + 1)`)
}

// renderNthColonPosFromClean renders a CHARINDEX expression to find the position of the n-th colon
// in the [c].[v] column (the cleaned cursor from CROSS APPLY). Returns 0 if there aren't enough colons.
func (d *MSSQLDialect) renderNthColonPosFromClean(ctx Context, n int) {
	if n <= 0 {
		ctx.WriteString(`0`)
		return
	}
	if n == 1 {
		ctx.WriteString(`CHARINDEX(':', [c].[v])`)
		return
	}
	// For n > 1, we need to nest: CHARINDEX(':', [c].[v], prev_pos + 1)
	// where prev_pos is the position of colon (n-1)
	ctx.WriteString(`CHARINDEX(':', [c].[v], `)
	d.renderNthColonPosFromClean(ctx, n-1)
	ctx.WriteString(` + 1)`)
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
	case qcode.OpRegex:
		return "LIKE", nil // MSSQL doesn't have native regex, use LIKE
	case qcode.OpNotRegex:
		return "NOT LIKE", nil
	case qcode.OpIRegex:
		return "LIKE", nil // MSSQL uses case-insensitive collation by default
	case qcode.OpNotIRegex:
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

// RenderGeoOp renders MSSQL Spatial operations
func (d *MSSQLDialect) RenderGeoOp(ctx Context, table, col string, ex *qcode.Exp) error {
	geo := ex.Geo
	if geo == nil {
		return fmt.Errorf("GIS expression missing geometry data")
	}

	switch ex.Op {
	case qcode.OpGeoDistance, qcode.OpGeoNear:
		// MSSQL: column.STDistance(geography) <= distance
		ctx.ColWithTable(table, col)
		ctx.WriteString(`.STDistance(`)
		d.renderGeoGeometry(ctx, geo)
		ctx.WriteString(`) <= `)
		distance := geo.Unit.ToMeters(geo.Distance)
		ctx.WriteString(fmt.Sprintf(`%f`, distance))

	case qcode.OpGeoWithin:
		// geometry.STContains(column) = 1 (inverse of within)
		d.renderGeoGeometry(ctx, geo)
		ctx.WriteString(`.STContains(`)
		ctx.ColWithTable(table, col)
		ctx.WriteString(`) = 1`)

	case qcode.OpGeoContains:
		ctx.ColWithTable(table, col)
		ctx.WriteString(`.STContains(`)
		d.renderGeoGeometry(ctx, geo)
		ctx.WriteString(`) = 1`)

	case qcode.OpGeoIntersects:
		ctx.ColWithTable(table, col)
		ctx.WriteString(`.STIntersects(`)
		d.renderGeoGeometry(ctx, geo)
		ctx.WriteString(`) = 1`)

	case qcode.OpGeoCoveredBy:
		// MSSQL doesn't have STCoveredBy, use STWithin
		d.renderGeoGeometry(ctx, geo)
		ctx.WriteString(`.STContains(`)
		ctx.ColWithTable(table, col)
		ctx.WriteString(`) = 1`)

	case qcode.OpGeoCovers:
		ctx.ColWithTable(table, col)
		ctx.WriteString(`.STContains(`)
		d.renderGeoGeometry(ctx, geo)
		ctx.WriteString(`) = 1`)

	case qcode.OpGeoTouches:
		ctx.ColWithTable(table, col)
		ctx.WriteString(`.STTouches(`)
		d.renderGeoGeometry(ctx, geo)
		ctx.WriteString(`) = 1`)

	case qcode.OpGeoOverlaps:
		ctx.ColWithTable(table, col)
		ctx.WriteString(`.STOverlaps(`)
		d.renderGeoGeometry(ctx, geo)
		ctx.WriteString(`) = 1`)

	default:
		return fmt.Errorf("unsupported GIS operator in MSSQL: %v", ex.Op)
	}
	return nil
}

// renderGeoGeometry renders the geometry expression for MSSQL
// Note: MSSQL geography::Point uses (lat, lon) order, not (lon, lat)
func (d *MSSQLDialect) renderGeoGeometry(ctx Context, geo *qcode.GeoExp) {
	if len(geo.Point) == 2 {
		// geography::Point(lat, lon, SRID) - note lat/lon order
		ctx.WriteString(fmt.Sprintf(`geography::Point(%f, %f, %d)`,
			geo.Point[1], geo.Point[0], geo.SRID))
	} else if len(geo.Polygon) > 0 {
		ctx.WriteString(`geography::STGeomFromText('POLYGON((`)
		for i, pt := range geo.Polygon {
			if i > 0 {
				ctx.WriteString(`, `)
			}
			ctx.WriteString(fmt.Sprintf(`%f %f`, pt[0], pt[1]))
		}
		ctx.WriteString(fmt.Sprintf(`))', %d)`, geo.SRID))
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
	ctx.WriteString(`JSON_ARRAY(`)
	for i, item := range items {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.WriteString(item)
	}
	ctx.WriteString(`)`)
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

func (d *MSSQLDialect) RenderBooleanEqualsTrue(ctx Context, paramName string) {
	ctx.WriteString(`(`)
	ctx.AddParam(Param{Name: paramName, Type: "boolean"})
	ctx.WriteString(` = 1)`)
}

func (d *MSSQLDialect) RenderBooleanNotEqualsTrue(ctx Context, paramName string) {
	ctx.WriteString(`(`)
	ctx.AddParam(Param{Name: paramName, Type: "boolean"})
	ctx.WriteString(` <> 1)`)
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
			// Polymorphic UNION type - render CASE WHEN to check subject_type
			ctx.WriteString(`(CASE `)
			for _, cid := range sel.Children {
				csel := r.GetChild(cid)
				if csel == nil {
					continue
				}
				ctx.WriteString(`WHEN `)
				// Reference the parent's type column (e.g., notifications.subject_type)
				if psel != nil {
					t := psel.Ti.Name
					if psel.ID >= 0 {
						t = fmt.Sprintf("%s_%d", t, psel.ID)
					}
					r.ColWithTable(t, sel.Rel.Left.Col.FKeyCol)
				} else {
					ctx.WriteString(sel.Rel.Left.Col.FKeyCol)
				}
				ctx.WriteString(` = `)
				r.Squoted(csel.Ti.Name)
				ctx.WriteString(` THEN `)
				// Pass psel (grandparent) as the parent because polymorphic children
				// reference the grandparent's columns (e.g., notifications.subject_id)
				r.RenderInlineChild(psel, csel)
				ctx.WriteString(` `)
			}
			ctx.WriteString(`END)`)
			return
		}

		// For singular, return a single JSON object using FOR JSON PATH
		if !d.hasRenderableFields(sel, r) {
			// No fields to render - return empty object
			ctx.WriteString(`'{}'`)
			return
		}

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
		// Render ORDER BY list join tables
		d.RenderJoinTables(ctx, sel)

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
		if !d.hasRenderableFields(sel, r) {
			// No fields to render - use STRING_AGG to produce array of empty objects
			ctx.WriteString(`COALESCE('[' + STRING_AGG('{}', ',') + ']', '[]')`)
			ctx.WriteString(` FROM `)
			d.renderFromTable(ctx, r, sel, psel)
			if sel.Rel.Type != sdata.RelEmbedded {
				t := sel.Ti.Name
				if sel.ID >= 0 {
					t = fmt.Sprintf("%s_%d", t, sel.ID)
				}
				d.RenderTableAlias(ctx, t)
			}
			// Render WHERE clause for filtering
			if sel.Where.Exp != nil {
				ctx.WriteString(` WHERE `)
				d.renderWhereExp(ctx, r, psel, sel, sel.Where.Exp)
			}
			ctx.WriteString(`)`)
		} else {
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
			// Render ORDER BY list join tables
			d.RenderJoinTables(ctx, sel)

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
		}
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

		if !d.hasRenderableFields(sel, r) {
			// No fields to render - use STRING_AGG to produce array of empty objects
			if sel.Singular {
				ctx.WriteString(`'{}'`)
			} else {
				// Use subquery to limit rows BEFORE STRING_AGG aggregation
				ctx.WriteString(`(SELECT COALESCE('[' + STRING_AGG('{}', ',') + ']', '[]')`)
				ctx.WriteString(` FROM (SELECT 1 AS __x FROM `)
				d.renderFromTable(ctx, r, sel, psel)
				if sel.Rel.Type != sdata.RelEmbedded {
					t := sel.Ti.Name
					if sel.ID >= 0 {
						t = fmt.Sprintf("%s_%d", t, sel.ID)
					}
					d.RenderTableAlias(ctx, t)
				}
				// Render WHERE clause for filtering
				if sel.Where.Exp != nil {
					ctx.WriteString(` WHERE `)
					d.renderWhereExp(ctx, r, nil, sel, sel.Where.Exp)
				}
				// Render ORDER BY and OFFSET/FETCH inside the subquery
				d.renderOrderBy(ctx, r, sel, "")
				if sel.Paging.Limit != 0 {
					r.RenderLimit(sel)
				}
				ctx.WriteString(`) AS __rows)`)
			}
		} else {
			// Use COALESCE to handle empty result set
			// Use renderInlineJSONFields instead of renderBaseColumns to exclude _ord_ columns from JSON

		if sel.Paging.Cursor {
				// Cursor pagination: wrap output with json/cursor structure
				// Structure: (SELECT [json], [cursor] FOR JSON PATH, WITHOUT_ARRAY_WRAPPER)
				// where [json] = JSON array of results, [cursor] = cursor string
				ctx.WriteString(`(SELECT `)

				// Build the JSON array of results
				// Use JSON_QUERY to prevent the inner JSON from being escaped as a string
				ctx.WriteString(`JSON_QUERY(COALESCE((SELECT `)
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
				// Add cursor CTE join for filtering
				ctx.WriteString(`, [__cur]`)
				// Render joins
				for _, join := range sel.Joins {
					d.renderJoinWithAlias(ctx, r, nil, sel, join)
				}
				d.RenderJoinTables(ctx, sel)
				// Render WHERE clause
				if sel.Where.Exp != nil {
					ctx.WriteString(` WHERE `)
					d.renderWhereExp(ctx, r, nil, sel, sel.Where.Exp)
				}
				d.renderGroupBy(ctx, r, sel)
				d.renderOrderBy(ctx, r, sel, "")
				if sel.Paging.Limit != 0 {
					r.RenderLimit(sel)
				}
				ctx.WriteString(` FOR JSON PATH, INCLUDE_NULL_VALUES), '[]')) AS [json], `)

				// Build cursor value using a subquery to get the last row's ORDER BY column values
				// IMPORTANT: Each subquery must use the FULL ORDER BY clause (all columns) to ensure
				// we get values from the SAME row, not just the column-specific extreme value
				ctx.WriteString(`CONCAT('`)
				ctx.WriteString(r.GetSecPrefix())
				ctx.WriteString(fmt.Sprintf(`%d`, sel.ID))
				ctx.WriteString(`'`)
				for i, ob := range sel.OrderBy {
					ctx.WriteString(`, ':', CAST((SELECT `)
					t := sel.Ti.Name
					if sel.ID >= 0 {
						t = fmt.Sprintf("%s_%d", t, sel.ID)
					}
					// Use table-qualified column name
					r.ColWithTable(t, ob.Col.Name)
					ctx.WriteString(` FROM `)
					d.renderFromTable(ctx, r, sel, psel)
					if sel.Rel.Type != sdata.RelEmbedded {
						d.RenderTableAlias(ctx, t)
					}
					ctx.WriteString(`, [__cur]`)
					for _, join := range sel.Joins {
						d.renderJoinWithAlias(ctx, r, nil, sel, join)
					}
					d.RenderJoinTables(ctx, sel)
					if sel.Where.Exp != nil {
						ctx.WriteString(` WHERE `)
						d.renderWhereExp(ctx, r, nil, sel, sel.Where.Exp)
					}
					// Use the FULL ORDER BY clause (all columns) to ensure we get values from the same row
					// This is critical: if ORDER BY is (price DESC, id ASC), we must order by BOTH
					// to get the correct row, not just order by the current column
					ctx.WriteString(` ORDER BY `)
					for j, orderCol := range sel.OrderBy {
						if j != 0 {
							ctx.WriteString(`, `)
						}
						r.ColWithTable(t, orderCol.Col.Name)
						if orderCol.Order == qcode.OrderDesc {
							ctx.WriteString(` DESC`)
						} else {
							ctx.WriteString(` ASC`)
						}
					}
					// Skip to last row: OFFSET (limit-1) ROWS FETCH NEXT 1 ROWS ONLY
					if sel.Paging.Limit > 0 {
						ctx.WriteString(fmt.Sprintf(` OFFSET %d ROWS FETCH NEXT 1 ROWS ONLY`, sel.Paging.Limit-1))
					} else {
						// No limit means we need a different approach - just get first row
						ctx.WriteString(` OFFSET 0 ROWS FETCH NEXT 1 ROWS ONLY`)
					}
					ctx.WriteString(`) AS NVARCHAR(MAX))`)
					_ = i // avoid unused variable
				}
				// Add dummy FROM clause - FOR JSON requires a FROM in MSSQL
				ctx.WriteString(`) AS [cursor] FROM (SELECT 1 AS _x) AS _ FOR JSON PATH, WITHOUT_ARRAY_WRAPPER)`)
			} else {
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
					d.renderJoinWithAlias(ctx, r, nil, sel, join)
				}
				// Render ORDER BY list join tables
				d.RenderJoinTables(ctx, sel)

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
			}
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
	ctx.WriteString(` INNER JOIN `)
	ctx.Quote(join.Rel.Left.Ti.Name)
	// Alias the join table with _0 suffix to match what renderExp produces
	d.RenderTableAlias(ctx, fmt.Sprintf("%s_0", join.Rel.Left.Ti.Name))
	ctx.WriteString(` ON ((`)
	// Use MSSQL's renderExp so the table references get consistent aliasing
	d.renderExp(ctx, r, psel, sel, join.Filter)
	ctx.WriteString(`))`)
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

		// Handle dynamic order by (KeyVar) - wrap in CASE WHEN to select the right column
		if ob.KeyVar != "" && ob.Key != "" {
			ctx.WriteString(`CASE WHEN `)
			ctx.AddParam(Param{Name: ob.KeyVar, Type: "text"})
			ctx.WriteString(` = `)
			ctx.WriteString(fmt.Sprintf("'%s'", ob.Key))
			ctx.WriteString(` THEN `)
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

		if ob.KeyVar != "" && ob.Key != "" {
			ctx.WriteString(` END`)
		}

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
		d.renderValue(ctx, r, psel, sel, ex)
		ctx.WriteString(`)`)

	case qcode.OpLike, qcode.OpNotLike, qcode.OpILike, qcode.OpNotILike:
		ctx.WriteString(`(`)
		d.renderColumn(ctx, r, psel, sel, ex)
		op, _ := d.RenderOp(ex.Op)
		ctx.WriteString(` `)
		ctx.WriteString(op)
		ctx.WriteString(` `)
		d.renderValue(ctx, r, psel, sel, ex)
		ctx.WriteString(`)`)

	case qcode.OpHasInCommon:
		// Handle array column overlap operations
		// OpHasInCommon is used when comparing array columns to a list
		d.renderArrayColumnExists(ctx, r, psel, sel, ex, false)

	case qcode.OpHasKeyAny, qcode.OpHasKeyAll:
		// Handle JSON key existence checks
		op := " OR "
		if ex.Op == qcode.OpHasKeyAll {
			op = " AND "
		}
		ctx.WriteString(`(`)
		if ex.Right.ValType == qcode.ValVar {
			// Variable case: use OPENJSON to iterate keys
			if ex.Op == qcode.OpHasKeyAll {
				ctx.WriteString(`NOT EXISTS (SELECT 1 FROM OPENJSON(`)
			} else {
				ctx.WriteString(`EXISTS (SELECT 1 FROM OPENJSON(`)
			}
			ctx.AddParam(Param{Name: ex.Right.Val, Type: "json"})
			ctx.WriteString(`) WHERE JSON_VALUE(`)
			d.renderColumn(ctx, r, psel, sel, ex)
			ctx.WriteString(`, '$."' + [value] + '"') IS `)
			if ex.Op == qcode.OpHasKeyAll {
				ctx.WriteString(`NULL)`)
			} else {
				ctx.WriteString(`NOT NULL)`)
			}
		} else {
			// Literal list case
			for i, val := range ex.Right.ListVal {
				if i != 0 {
					ctx.WriteString(op)
				}
				ctx.WriteString(`JSON_VALUE(`)
				d.renderColumn(ctx, r, psel, sel, ex)
				ctx.WriteString(`, '$."` + val + `"') IS NOT NULL`)
			}
		}
		ctx.WriteString(`)`)

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

		// Handle regular variable arrays - MSSQL needs OPENJSON to iterate
		if ex.Right.ValType == qcode.ValVar {
			ctx.WriteString(`(`)
			d.renderColumn(ctx, r, psel, sel, ex)
			ctx.WriteString(` `)
			if ex.Op == qcode.OpNotIn {
				ctx.WriteString(`NOT `)
			}
			// MSSQL: IN (SELECT CAST([value] AS TYPE) FROM OPENJSON(@param))
			ctx.WriteString(`IN (SELECT CAST([value] AS `)
			ctx.WriteString(d.mssqlType(ex.Left.Col.Type))
			ctx.WriteString(`) FROM OPENJSON(`)
			ctx.AddParam(Param{Name: ex.Right.Val, Type: "json"})
			ctx.WriteString(`))`)
			ctx.WriteString(`)`)
			return
		}

		ctx.WriteString(`(`)
		d.renderColumn(ctx, r, psel, sel, ex)
		op, _ := d.RenderOp(ex.Op)
		ctx.WriteString(` `)
		ctx.WriteString(op)
		ctx.WriteString(` `)
		d.renderValue(ctx, r, psel, sel, ex)
		ctx.WriteString(`)`)

	case qcode.OpEqualsTrue, qcode.OpNotEqualsTrue:
		ctx.WriteString(`(`)
		if ex.Right.ValType == qcode.ValVar {
			// For @skip/@include(ifVar: $var), render as parameter comparison
			ctx.AddParam(Param{Name: ex.Right.Val, Type: "bit"})
		} else {
			d.renderColumn(ctx, r, psel, sel, ex)
		}
		if ex.Op == qcode.OpEqualsTrue {
			ctx.WriteString(` = 1)`)
		} else {
			ctx.WriteString(` != 1)`)
		}

	case qcode.OpTsQuery:
		ti := sel.Ti
		d.RenderTsQuery(ctx, ti, ex)

	case qcode.OpRegex, qcode.OpNotRegex, qcode.OpIRegex, qcode.OpNotIRegex:
		// MSSQL doesn't have native regex support, use LIKE with wildcards for partial matching
		ctx.WriteString(`(`)
		d.renderColumn(ctx, r, psel, sel, ex)
		op, _ := d.RenderOp(ex.Op)
		ctx.WriteString(` `)
		ctx.WriteString(op)
		ctx.WriteString(` `)
		d.renderRegexValue(ctx, r, psel, sel, ex)
		ctx.WriteString(`)`)

	case qcode.OpSelectExists:
		// WHERE on related tables - generate EXISTS subquery
		if len(ex.Joins) == 0 {
			return
		}
		first := ex.Joins[0]
		relatedTable := first.Rel.Left.Col.Table
		relatedAlias := fmt.Sprintf("%s_0", relatedTable)

		ctx.WriteString(`EXISTS (SELECT 1 FROM `)
		ctx.Quote(relatedTable)
		d.RenderTableAlias(ctx, relatedAlias)

		// Handle nested joins if any
		if len(ex.Joins) > 1 {
			for i := 1; i < len(ex.Joins); i++ {
				j := ex.Joins[i]
				ctx.WriteString(` LEFT JOIN `)
				ctx.Quote(j.Rel.Left.Col.Table)
				d.RenderTableAlias(ctx, fmt.Sprintf("%s_0", j.Rel.Left.Col.Table))
				ctx.WriteString(` ON `)
				if j.Filter != nil {
					d.renderExistsExp(ctx, r, psel, sel, j.Filter, relatedTable, relatedAlias)
				}
			}
		}

		ctx.WriteString(` WHERE `)
		d.renderExistsExp(ctx, r, psel, sel, first.Filter, relatedTable, relatedAlias)

		if len(ex.Children) > 0 {
			ctx.WriteString(` AND `)
			for i, child := range ex.Children {
				if i > 0 {
					ctx.WriteString(` AND `)
				}
				d.renderExistsExp(ctx, r, psel, sel, child, relatedTable, relatedAlias)
			}
		}
		ctx.WriteString(`)`)

	case qcode.OpGeoDistance, qcode.OpGeoWithin, qcode.OpGeoContains,
		qcode.OpGeoIntersects, qcode.OpGeoCoveredBy, qcode.OpGeoCovers,
		qcode.OpGeoTouches, qcode.OpGeoOverlaps, qcode.OpGeoNear:
		// Handle GIS operators with proper table aliasing
		// Determine the correct table alias using the same logic as renderColumn
		t := ex.Left.Table
		if t == "" {
			t = ex.Left.Col.Table
			if t == "" {
				t = sel.Ti.Name
			}
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

		ctx.WriteString(`(`)
		if err := d.RenderGeoOp(ctx, t, colName, ex); err != nil {
			// Error handling - render FALSE with error message
			ctx.WriteString(`0=1 /* GIS error: ` + err.Error() + ` */`)
		}
		ctx.WriteString(`)`)

	default:
		ctx.WriteString(`(`)
		d.renderColumn(ctx, r, psel, sel, ex)
		op, _ := d.RenderOp(ex.Op)
		ctx.WriteString(` `)
		ctx.WriteString(op)
		ctx.WriteString(` `)
		d.renderValue(ctx, r, psel, sel, ex)
		ctx.WriteString(`)`)
	}
}

// renderExistsExp renders expressions inside EXISTS subqueries with proper table aliasing.
// relatedTable is the table being queried in the EXISTS (e.g., "users")
// relatedAlias is the alias used for it (e.g., "users_0")
func (d *MSSQLDialect) renderExistsExp(ctx Context, r InlineChildRenderer, psel, sel *qcode.Select, ex *qcode.Exp, relatedTable, relatedAlias string) {
	if ex == nil {
		return
	}

	switch ex.Op {
	case qcode.OpNop:
		return

	case qcode.OpAnd:
		ctx.WriteString(`(`)
		for i, child := range ex.Children {
			if i > 0 {
				ctx.WriteString(` AND `)
			}
			d.renderExistsExp(ctx, r, psel, sel, child, relatedTable, relatedAlias)
		}
		ctx.WriteString(`)`)

	case qcode.OpOr:
		ctx.WriteString(`(`)
		for i, child := range ex.Children {
			if i > 0 {
				ctx.WriteString(` OR `)
			}
			d.renderExistsExp(ctx, r, psel, sel, child, relatedTable, relatedAlias)
		}
		ctx.WriteString(`)`)

	case qcode.OpNot:
		ctx.WriteString(`NOT `)
		d.renderExistsExp(ctx, r, psel, sel, ex.Children[0], relatedTable, relatedAlias)

	default:
		// For other operators, render column = value with proper aliasing
		ctx.WriteString(`(`)
		d.renderExistsColumn(ctx, r, psel, sel, ex, relatedTable, relatedAlias)
		op, _ := d.RenderOp(ex.Op)
		ctx.WriteString(` `)
		ctx.WriteString(op)
		ctx.WriteString(` `)
		d.renderExistsValue(ctx, r, psel, sel, ex, relatedTable, relatedAlias)
		ctx.WriteString(`)`)
	}
}

// renderExistsColumn renders a column reference inside an EXISTS subquery
func (d *MSSQLDialect) renderExistsColumn(ctx Context, r InlineChildRenderer, psel, sel *qcode.Select, ex *qcode.Exp, relatedTable, relatedAlias string) {
	if ex.Left.Col.Name == "" {
		return
	}

	t := ex.Left.Col.Table
	if t == "" {
		t = sel.Ti.Name
	}

	// If the column is from the related table, use the alias
	if t == relatedTable {
		t = relatedAlias
	} else if t == sel.Ti.Name && sel.ID >= 0 {
		// Reference to the outer query's table
		t = fmt.Sprintf("%s_%d", t, sel.ID)
	} else if psel != nil && t == psel.Ti.Name && psel.ID >= 0 {
		// Reference to the parent select's table
		t = fmt.Sprintf("%s_%d", t, psel.ID)
	}

	colName := ex.Left.Col.Name
	if ex.Left.ColName != "" {
		colName = ex.Left.ColName
	}

	r.ColWithTable(t, colName)
}

// renderExistsValue renders the right side of an expression inside an EXISTS subquery
func (d *MSSQLDialect) renderExistsValue(ctx Context, r InlineChildRenderer, psel, sel *qcode.Select, ex *qcode.Exp, relatedTable, relatedAlias string) {
	// Handle column references (for relationship joins)
	if ex.Right.Col.Name != "" {
		var t string
		if ex.Right.Table != "" {
			t = ex.Right.Table
		} else {
			t = ex.Right.Col.Table
		}

		// If the column is from the related table, use the alias
		if t == relatedTable {
			t = relatedAlias
		} else if t == sel.Ti.Name && sel.ID >= 0 {
			t = fmt.Sprintf("%s_%d", t, sel.ID)
		} else if psel != nil && t == psel.Ti.Name && psel.ID >= 0 {
			t = fmt.Sprintf("%s_%d", t, psel.ID)
		}

		colName := ex.Right.Col.Name
		if ex.Right.ColName != "" {
			colName = ex.Right.ColName
		}

		r.ColWithTable(t, colName)
		return
	}

	// For non-column values, use the standard renderValue
	d.renderValue(ctx, r, psel, sel, ex)
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
		r.ColWithTable("__cur", colName)
		return
	}

	// Check if column references the parent table
	// For self-referential tables (psel.Ti.Name == sel.Ti.Name), we need extra checks
	// to avoid incorrectly matching the parent when ex.Left.ID defaults to 0
	// Also check ex.Left.Table - if it's explicitly set and doesn't match the parent,
	// it's not a parent reference (e.g., polymorphic relationships set this to the child table)
	if psel != nil && (ex.Left.Table == "" || ex.Left.Table == psel.Ti.Name) &&
		((ex.Left.ID >= 0 && ex.Left.ID == psel.ID &&
			(ex.Left.Col.Table == "" || (ex.Left.Col.Table == psel.Ti.Name && ex.Left.Col.Table != sel.Ti.Name))) ||
			(ex.Left.ID == -1 && ex.Left.Col.Table == psel.Ti.Name && ex.Left.Col.Table != sel.Ti.Name)) {
		t := psel.Ti.Name
		if psel.ID >= 0 {
			t = fmt.Sprintf("%s_%d", t, psel.ID)
		}
		r.ColWithTable(t, ex.Left.Col.Name)
		return
	}

	// Prefer ex.Left.Table (explicitly set) over ex.Left.Col.Table
	// This is important for polymorphic relationships where ex.Left.Table
	// is set to the child table but ex.Left.Col.Table may contain the parent table
	t := ex.Left.Table
	if t == "" {
		t = ex.Left.Col.Table
		if t == "" {
			t = sel.Ti.Name
		}
	}

	if t == sel.Ti.Name {
		if sel.ID >= 0 {
			t = fmt.Sprintf("%s_%d", t, sel.ID)
		}
	} else if ex.Left.ID >= 0 {
		t = fmt.Sprintf("%s_%d", t, ex.Left.ID)
	} else if ex.Left.ID == -1 && t != "" && t != sel.Ti.Name {
		// Fallback: handle join table references (e.g., many-to-many through tables)
		// The join table is aliased as table_0 in the JOIN clause
		if t != "__cur" {
			t = fmt.Sprintf("%s_0", t)
		}
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

func (d *MSSQLDialect) renderValue(ctx Context, r InlineChildRenderer, psel, sel *qcode.Select, ex *qcode.Exp) {
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
		} else if ex.Right.ID == -1 && psel != nil && table == psel.Ti.Name {
			// Reference to parent table
			if psel.ID >= 0 {
				table = fmt.Sprintf("%s_%d", table, psel.ID)
			}
		} else if ex.Right.ID == -1 && table != "" && table != sel.Ti.Name {
			// Fallback: handle join table references (e.g., many-to-many through tables)
			if table != "__cur" {
				table = fmt.Sprintf("%s_0", table)
			}
		} else if ex.Right.ID == -1 && table == sel.Ti.Name {
			// Reference to current select's table
			if sel.ID >= 0 {
				table = fmt.Sprintf("%s_%d", table, sel.ID)
			}
		}
		r.ColWithTable(table, colName)
		return
	}

	switch ex.Right.ValType {
	case qcode.ValDBVar:
		// Database variable - render with @ prefix
		d.RenderVar(ctx, ex.Right.Val)
	case qcode.ValVar:
		if val, ok := r.GetConfigVar(ex.Right.Val); ok {
			// Config variable - render as literal
			d.RenderLiteral(ctx, val, qcode.ValNum)
		} else {
			ctx.AddParam(Param{Name: ex.Right.Val, Type: ex.Left.Col.Type})
		}
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

// renderRegexValue renders a value wrapped with % wildcards for LIKE-based regex emulation
func (d *MSSQLDialect) renderRegexValue(ctx Context, r InlineChildRenderer, psel, sel *qcode.Select, ex *qcode.Exp) {
	switch ex.Right.ValType {
	case qcode.ValVar:
		if val, ok := r.GetConfigVar(ex.Right.Val); ok {
			// Config variable - embed as literal with wildcards
			ctx.WriteString(`N'%`)
			ctx.WriteString(strings.ReplaceAll(val, "'", "''"))
			ctx.WriteString(`%'`)
		} else {
			// For variables, use string concatenation: '%' + @param + '%'
			ctx.WriteString(`'%' + `)
			ctx.AddParam(Param{Name: ex.Right.Val, Type: ex.Left.Col.Type})
			ctx.WriteString(` + '%'`)
		}
	case qcode.ValStr:
		// For string literals, embed the wildcards directly
		ctx.WriteString(`N'%`)
		ctx.WriteString(strings.ReplaceAll(ex.Right.Val, "'", "''"))
		ctx.WriteString(`%'`)
	default:
		// Fallback to regular rendering
		d.renderValue(ctx, r, psel, sel, ex)
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

// hasRenderableFields checks if a select has any fields that will be rendered
// (i.e., not dropped). Returns false when all fields are SkipTypeDrop.
func (d *MSSQLDialect) hasRenderableFields(sel *qcode.Select, r InlineChildRenderer) bool {
	for _, f := range sel.Fields {
		if f.SkipRender != qcode.SkipTypeDrop {
			return true
		}
	}
	for _, cid := range sel.Children {
		csel := r.GetChild(cid)
		if csel != nil && csel.SkipRender != qcode.SkipTypeRemote &&
			csel.SkipRender != qcode.SkipTypeDrop {
			return true
		}
	}
	return sel.Typename
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

	// Check if the inner SQL starts with a CTE (e.g., WITH [__cur] AS (...))
	// CTEs cannot be inside CROSS APPLY, so we need to extract and merge them
	cursorCTE := ""
	if strings.HasPrefix(strings.ToUpper(sql), "WITH [__CUR]") || strings.HasPrefix(sql, "WITH [__cur]") {
		// Find the end of the CTE definition - need to match balanced parentheses
		// The CTE ends with ") " followed by "SELECT"
		depth := 0
		inCTE := false
		cteEnd := -1
		for i := 0; i < len(sql); i++ {
			if sql[i] == '(' {
				depth++
				inCTE = true
			} else if sql[i] == ')' {
				depth--
				if inCTE && depth == 0 {
					// Found end of CTE - look for following SELECT
					rest := strings.TrimSpace(sql[i+1:])
					if strings.HasPrefix(strings.ToUpper(rest), "SELECT") {
						cursorCTE = sql[:i+1] // Include the closing paren
						sql = rest
						cteEnd = i
						break
					}
				}
			}
		}
		_ = cteEnd // avoid unused variable warning
	}

	// If we extracted a cursor CTE, we need to:
	// 1. Replace the cursor parameter placeholder (?) with [_gj_sub].[cursor]
	// 2. Merge it with the subscription CTE
	if cursorCTE != "" {
		// The cursor CTE has parameter placeholders (?) for the cursor value
		// In subscriptions, the cursor comes from [_gj_sub].[cursor] not from a direct param
		// Replace the placeholder: "(VALUES (?))" -> "(VALUES ([_gj_sub].[cursor]))"
		cursorCTE = strings.Replace(cursorCTE, "(VALUES (?))", "(VALUES ([_gj_sub].[cursor]))", 1)

		// cursorCTE is like "WITH [__cur] AS (...)"
		// We need: "WITH [_gj_sub] AS (...), [__cur] AS (...)"
		// Note: [_gj_sub] must come FIRST since [__cur] references it
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
		ctx.WriteString(`)), `)
		// Strip "WITH " from cursorCTE since we're adding it as a second CTE
		cursorCTE = strings.TrimPrefix(cursorCTE, "WITH ")
		ctx.WriteString(cursorCTE)
		ctx.WriteString(` SELECT [_gj_sub_data].[__root] FROM [_gj_sub] CROSS APPLY (`)
		ctx.WriteString(sql)
		ctx.WriteString(`) AS [_gj_sub_data]`)
	} else {
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

func (d *MSSQLDialect) getVarName(m qcode.Mutate) string {
	return m.Ti.Name + "_" + fmt.Sprintf("%d", m.ID)
}

func (d *MSSQLDialect) RenderLinearUpdate(ctx Context, m *qcode.Mutate, qc *qcode.QCode, varName string, renderColVal func(qcode.MColumn), renderWhere func()) {
	// Check if there are child mutations that need parent values
	hasChildMutations := false
	for _, otherM := range qc.Mutates {
		if otherM.ParentID == m.ID {
			hasChildMutations = true
			break
		}
	}

	// Pre-update SELECT to capture values for child updates
	if m.ParentID == -1 && hasChildMutations {
		if len(qc.Selects) > 0 {
			ctx.WriteString(`SELECT @`)
			ctx.WriteString(varName)
			ctx.WriteString(` = `)
			ctx.Quote(m.Ti.PrimaryCol.Name)

			// Capture all other columns for FK references
			for _, col := range m.Ti.Columns {
				ctx.WriteString(`, @`)
				ctx.WriteString(varName + "_" + col.Name)
				ctx.WriteString(` = `)
				ctx.Quote(col.Name)
			}

			ctx.WriteString(` FROM `)
			ctx.Quote(m.Ti.Name)
			ctx.WriteString(` WHERE `)
			renderWhere()
			ctx.WriteString(`; `)
		}
	}

	// For child updates with JSON data, use special handling with JSON_VALUE
	if m.ParentID != -1 && m.IsJSON {
		d.renderChildUpdate(ctx, m, qc, renderWhere)
		return
	}

	// UPDATE statement
	ctx.WriteString(`UPDATE `)
	ctx.Quote(m.Ti.Name)
	ctx.WriteString(` SET `)

	i := 0
	// Regular columns
	for _, col := range m.Cols {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.Quote(col.Col.Name)
		ctx.WriteString(` = `)
		renderColVal(col)
		i++
	}

	// Related columns from dependent mutations
	for _, rcol := range m.RCols {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.Quote(rcol.Col.Name)
		ctx.WriteString(` = `)

		found := false
		for id := range m.DependsOn {
			if qc.Mutates[id].Ti.Name == rcol.VCol.Table {
				depM := qc.Mutates[id]
				depVarName := d.getVarName(depM)
				ctx.WriteString(`@`)
				ctx.WriteString(depVarName)
				found = true
				break
			}
		}
		if !found {
			ctx.WriteString(`NULL`)
		}
		i++
	}

	// Identity fallback if no columns to update
	if i == 0 {
		ctx.Quote(m.Ti.PrimaryCol.Name)
		ctx.WriteString(` = `)
		ctx.Quote(m.Ti.PrimaryCol.Name)
	}

	ctx.WriteString(` WHERE `)
	renderWhere()
}

// renderChildUpdate renders a simple UPDATE for child mutations using JSON_VALUE
// This extracts values from the JSON input for child table updates
func (d *MSSQLDialect) renderChildUpdate(ctx Context, m *qcode.Mutate, qc *qcode.QCode, renderWhere func()) {
	ctx.WriteString(`UPDATE `)
	ctx.Quote(m.Ti.Name)
	ctx.WriteString(` SET `)

	// Build JSON path prefix from m.Path (e.g., ["customer"] -> "$.customer")
	jsonPathPrefix := "$"
	for _, p := range m.Path {
		jsonPathPrefix += "." + p
	}

	i := 0
	for _, col := range m.Cols {
		if col.Set {
			// Preset columns - skip, they don't come from JSON
			continue
		}
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.Quote(col.Col.Name)
		ctx.WriteString(` = `)

		// Use JSON_VALUE(?, N'$.path.field') for MSSQL
		ctx.WriteString(`JSON_VALUE(`)
		ctx.AddParam(Param{Name: qc.ActionVar, Type: "json"})
		ctx.WriteString(`, N'`)
		ctx.WriteString(jsonPathPrefix)
		ctx.WriteString(`.`)
		ctx.WriteString(col.FieldName)
		ctx.WriteString(`')`)
		i++
	}

	// Handle preset columns (they have literal values)
	for _, col := range m.Cols {
		if !col.Set {
			continue
		}
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.Quote(col.Col.Name)
		ctx.WriteString(` = `)
		// For preset columns, render the value directly
		if strings.HasPrefix(col.Value, "sql:") {
			ctx.WriteString(`(`)
			ctx.WriteString(col.Value[4:])
			ctx.WriteString(`)`)
		} else {
			ctx.WriteString(`'`)
			ctx.WriteString(col.Value)
			ctx.WriteString(`'`)
		}
		i++
	}

	if i == 0 {
		// No columns to update - use identity update
		ctx.Quote(m.Ti.PrimaryCol.Name)
		ctx.WriteString(` = `)
		ctx.Quote(m.Ti.PrimaryCol.Name)
	}

	ctx.WriteString(` WHERE `)
	renderWhere()
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
	// Step 1: Capture the IDs being disconnected into a variable
	ctx.WriteString(`SET @`)
	ctx.WriteString(varName)
	ctx.WriteString(` = (SELECT `)
	ctx.Quote(m.Rel.Left.Col.Name)
	ctx.WriteString(` FROM `)
	ctx.Quote(m.Ti.Name)
	ctx.WriteString(` WHERE `)
	renderFilter()
	ctx.WriteString(`); `)

	// Step 2: Perform the actual disconnect (UPDATE child SET fk = NULL)
	ctx.WriteString(`UPDATE `)
	ctx.Quote(m.Ti.Name)
	ctx.WriteString(` SET `)
	ctx.Quote(m.Rel.Left.Col.Name)
	ctx.WriteString(` = NULL WHERE `)
	renderFilter()
	ctx.WriteString(`; `)
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

// Role Statement rendering
func (d *MSSQLDialect) RoleSelectPrefix() string {
	return `(SELECT TOP 1 (CASE` // MSSQL uses TOP instead of LIMIT
}

func (d *MSSQLDialect) RoleLimitSuffix() string {
	return `) AS _sg_auth_roles_query) ` // No LIMIT, uses TOP in prefix
}

func (d *MSSQLDialect) RoleDummyTable() string {
	return `ELSE 'anon' END) FROM (SELECT 1 AS _sg_auth_filler) AS _sg_auth_filler; `
}

func (d *MSSQLDialect) TransformBooleanLiterals(match string) string {
	// MSSQL uses 1/0 for boolean literals instead of true/false
	match = strings.ReplaceAll(match, " true", " 1")
	match = strings.ReplaceAll(match, " false", " 0")
	match = strings.ReplaceAll(match, "=true", "=1")
	match = strings.ReplaceAll(match, "=false", "=0")
	return match
}

// Driver Behavior
func (d *MSSQLDialect) RequiresJSONAsString() bool {
	return true // MSSQL driver doesn't handle json.RawMessage properly
}

func (d *MSSQLDialect) RequiresLowercaseIdentifiers() bool {
	return false // MSSQL doesn't require lowercase identifiers
}

func (d *MSSQLDialect) RequiresBooleanAsInt() bool {
	return false // MSSQL handles boolean as BIT natively
}

// Recursive CTE Syntax
func (d *MSSQLDialect) RequiresRecursiveKeyword() bool {
	return true // MSSQL uses WITH RECURSIVE (actually just WITH, but keyword is used)
}

func (d *MSSQLDialect) RequiresRecursiveCTEColumnList() bool {
	return true // MSSQL requires explicit column alias list in recursive CTEs
}

func (d *MSSQLDialect) RenderRecursiveOffset(ctx Context) {
	ctx.WriteString(` OFFSET 1 ROWS`)
}

func (d *MSSQLDialect) RenderRecursiveLimit1(ctx Context) {
	ctx.WriteString(` FETCH FIRST 1 ROWS ONLY`)
}

func (d *MSSQLDialect) WrapRecursiveSelect() bool {
	return false // MSSQL doesn't need extra wrapping
}

func (d *MSSQLDialect) RenderRecursiveAnchorWhere(ctx Context, psel *qcode.Select, ti sdata.DBTable, pkCol string) bool {
	// MSSQL doesn't support outer scope correlation in CTEs
	// Instead of correlating with outer table alias, inline the parent's WHERE expression
	if psel.Where.Exp != nil {
		ctx.RenderExp(ti, psel.Where.Exp)
		return true
	}
	return false
}

// JSON Null Fields
func (d *MSSQLDialect) RenderJSONNullField(ctx Context, fieldName string) {
	ctx.WriteString(`NULL AS `)
	ctx.Quote(fieldName)
}

func (d *MSSQLDialect) RenderJSONNullCursorField(ctx Context, fieldName string) {
	ctx.WriteString(`, NULL AS `)
	ctx.Quote(fieldName + "_cursor")
}

func (d *MSSQLDialect) RenderJSONRootSuffix(ctx Context) {
	ctx.WriteString(` FOR JSON PATH, INCLUDE_NULL_VALUES, WITHOUT_ARRAY_WRAPPER`)
}

// Array Operations
func (d *MSSQLDialect) RenderArraySelectPrefix(ctx Context) {
	ctx.WriteString(`(SELECT JSON_ARRAYAGG(`)
}

func (d *MSSQLDialect) RenderArraySelectSuffix(ctx Context) {
	ctx.WriteString(`))`)
}

func (d *MSSQLDialect) RenderArrayAggPrefix(ctx Context, distinct bool) {
	if distinct {
		ctx.WriteString(`JSON_ARRAYAGG(DISTINCT `)
	} else {
		ctx.WriteString(`JSON_ARRAYAGG(`)
	}
}

func (d *MSSQLDialect) RenderArrayRemove(ctx Context, col string, val func()) {
	// MSSQL doesn't have a direct array_remove function
	// Use JSON_MODIFY approach
	ctx.WriteString(` JSON_MODIFY(`)
	ctx.Quote(col)
	ctx.WriteString(`, `)
	val()
	ctx.WriteString(`, NULL)`)
}

// Column rendering
func (d *MSSQLDialect) RequiresJSONQueryWrapper() bool {
	return false // MSSQL doesn't need JSON_QUERY wrapper (uses FOR JSON PATH)
}

func (d *MSSQLDialect) RequiresNullOnEmptySelect() bool {
	return false // MSSQL doesn't need NULL when no columns rendered
}
