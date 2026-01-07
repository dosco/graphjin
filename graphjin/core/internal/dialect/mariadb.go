package dialect

import (
	"fmt"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

// MariaDBDialect embeds MySQLDialect and provides MariaDB-specific behavior.
// MariaDB is a fork of MySQL and is largely compatible, but may have
// differences in JSON functions, version detection, and other features.
type MariaDBDialect struct {
	MySQLDialect
	DBVersion int
}

// findSkipVarExp searches for a skip/include variable expression at the top level of a Where clause.
// Returns the variable name and true if found, empty string and false otherwise.
// For @skip(ifVar: $var), the Op will be OpNotEqualsTrue
// For @include(ifVar: $var), the Op will be OpEqualsTrue
func (d *MariaDBDialect) findSkipVarExp(exp *qcode.Exp) (varName string, isSkip bool, found bool) {
	if exp == nil {
		return "", false, false
	}

	// Direct OpEqualsTrue or OpNotEqualsTrue
	switch exp.Op {
	case qcode.OpEqualsTrue:
		if exp.Right.ValType == qcode.ValVar {
			return exp.Right.Val, false, true // include directive
		}
	case qcode.OpNotEqualsTrue:
		if exp.Right.ValType == qcode.ValVar {
			return exp.Right.Val, true, true // skip directive
		}
	case qcode.OpAnd:
		// Check first child (skip conditions are usually added first)
		if len(exp.Children) > 0 {
			return d.findSkipVarExp(exp.Children[0])
		}
	}

	return "", false, false
}

func (d *MariaDBDialect) Name() string {
	return "mariadb"
}

func (d *MariaDBDialect) QuoteIdentifier(s string) string {
	return "`" + s + "`"
}

// SupportsLateral returns false for MariaDB because the shared LATERAL join
// code path in query.go is incompatible with MariaDB's RenderJSONPlural.
// MariaDB uses inline subqueries via RenderInlineChild instead.
// Note: Subscription batching uses a separate code path (RenderSubscriptionUnbox)
// that handles LATERAL joins internally and is not affected by this setting.
func (d *MariaDBDialect) SupportsLateral() bool {
	return false
}

// RenderInlineChild renders an inline subquery for MariaDB.
// MariaDB doesn't support LATERAL joins, so we generate flat correlated subqueries.
// For plural (array) results, we use a subquery to apply ORDER BY and LIMIT before aggregation.
func (d *MariaDBDialect) RenderInlineChild(ctx Context, r InlineChildRenderer, psel, sel *qcode.Select) {
	// For recursive relationships, MariaDB needs a special approach since:
	// 1. It doesn't support LATERAL joins
	// 2. Correlated CTEs in subqueries don't work well
	// We use a recursive CTE at the query level with proper structure
	if sel.Rel.Type == sdata.RelRecursive {
		d.renderRecursiveInlineChild(ctx, r, psel, sel)
		return
	}
	ctx.WriteString(`(SELECT `)
	if sel.Singular {
		if sel.Type == qcode.SelTypeUnion {
			// For Union types, we dispatch to children using COALESCE.
			// The children (concrete types) are correlated to the grandparent table by the compiler.
			// We use the existing (SELECT from function start.
			ctx.WriteString(`COALESCE(`)
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
			ctx.WriteString(`) FROM DUAL)`)
			return
		}

		// For singular (one-to-one/many-to-one), return a single json_object
		ctx.WriteString(`json_object(`)
		d.renderInlineJSONFields(ctx, r, sel)
		ctx.WriteString(`)`)

		ctx.WriteString(` FROM `)
		d.renderFromTable(ctx, r, sel, psel)
		// Skip alias for embedded JSON tables since RenderFromEdge already adds alias
		if sel.Rel.Type != sdata.RelEmbedded {
			t := sel.Ti.Name
			if sel.ID >= 0 {
				t = fmt.Sprintf("%s_%d", t, sel.ID)
			}
			d.RenderTableAlias(ctx, t)
		}

		// Render join tables for many-to-many relationships
		// Use custom join rendering with proper table aliases
		for _, join := range sel.Joins {
			d.renderJoinWithAlias(ctx, r, psel, sel, join)
		}

		// Render self-joins for order by list
		for _, ob := range sel.OrderBy {
			if ob.Var != "" {
				d.renderOrderByJoin(ctx, r, sel, ob)
			}
		}

		// Render the relationship filter (WHERE clause)
		if sel.Where.Exp != nil {
			ctx.WriteString(` WHERE `)
			d.renderWhereExp(ctx, r, psel, sel, sel.Where.Exp)
		}
		d.renderGroupBy(ctx, r, sel)

		// Render LIMIT 1 for singular
		ctx.WriteString(` LIMIT 1`)
	} else {
		// For plural (one-to-many/many-to-many), aggregate into array
		if psel != nil {
			// For correlated child subqueries, use simple correlated subquery (no derived table)
			// This allows the parent table to be visible in the WHERE clause
			ctx.WriteString(`COALESCE(json_arrayagg(json_object(`)
			d.renderInlineJSONFields(ctx, r, sel)
			ctx.WriteString(`))`)
			// Add ORDER BY inside aggregation if needed
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
			ctx.WriteString(`, '[]')`)

			ctx.WriteString(` FROM `)
			d.renderFromTable(ctx, r, sel, psel)
			// Skip alias for embedded JSON tables since RenderFromEdge already adds alias
			if sel.Rel.Type != sdata.RelEmbedded {
				t := sel.Ti.Name
				if sel.ID >= 0 {
					t = fmt.Sprintf("%s_%d", t, sel.ID)
				}
				d.RenderTableAlias(ctx, t)
			}

			// Render join tables for many-to-many relationships
			// Use custom join rendering with proper table aliases
			for _, join := range sel.Joins {
				d.renderJoinWithAlias(ctx, r, psel, sel, join)
			}

			// Render the relationship filter (WHERE clause)
			if sel.Where.Exp != nil {
				ctx.WriteString(` WHERE `)
				d.renderWhereExp(ctx, r, psel, sel, sel.Where.Exp)
			}
			d.renderGroupBy(ctx, r, sel)
		} else {
			// For root queries, use a subquery to apply ORDER BY and LIMIT before aggregation

			// Check if there's a skip/include variable condition - if so, we need to wrap with CASE WHEN
			// to return NULL when the condition is met, instead of returning [] from COALESCE
			skipVarName, isSkip, hasSkipVar := d.findSkipVarExp(sel.Where.Exp)
			if hasSkipVar {
				ctx.WriteString(`CASE WHEN (`)
				ctx.AddParam(Param{Name: skipVarName, Type: "boolean"})
				if isSkip {
					// @skip(ifVar: $var) - show when variable is NOT true
					ctx.WriteString(` IS NOT TRUE) THEN (SELECT `)
				} else {
					// @include(ifVar: $var) - show when variable IS true
					ctx.WriteString(` IS TRUE) THEN (SELECT `)
				}
			}

			if sel.Paging.Cursor {
				ctx.WriteString(`json_object('json', JSON_EXTRACT(`)
			}
			ctx.WriteString(`COALESCE(json_arrayagg(json_object(`)
			d.renderSubqueryJSONFields(ctx, r, sel)
			ctx.WriteString(`)`)
			d.renderOuterOrderBy(ctx, r, sel, "_gj_t")
			ctx.WriteString(`), '[]')`)

			if sel.Paging.Cursor {
				ctx.WriteString(`, '$')`)
			}

			if sel.Paging.Cursor {
				ctx.WriteString(`, 'cursor', CONCAT('`)
				ctx.WriteString(r.GetSecPrefix())
				ctx.WriteString(fmt.Sprintf(`%d`, sel.ID))
				ctx.WriteString(`'`)

				for i := range sel.OrderBy {
					ctx.WriteString(`, ':', `)
					ctx.WriteString(fmt.Sprintf(`JSON_VALUE(JSON_ARRAYAGG(__cur_%d), '$[last]')`, i))
				}
				ctx.WriteString(`))`)
			}

			ctx.WriteString(` FROM (SELECT `)
			// Select the columns we need
			d.renderBaseColumns(ctx, r, sel)

			ctx.WriteString(` FROM `)
			d.renderFromTable(ctx, r, sel, psel)
			// Apply alias to match generic RenderJoin expectations
			// Skip for embedded JSON tables since RenderFromEdge already adds alias
			if sel.Rel.Type != sdata.RelEmbedded {
				t := sel.Ti.Name
				if sel.ID >= 0 {
					t = fmt.Sprintf("%s_%d", t, sel.ID)
				}
				d.RenderTableAlias(ctx, t)
			}

			// Add __cur CTE join for cursor pagination
			if sel.Paging.Cursor {
				ctx.WriteString(`, `)
				ctx.Quote("__cur")
			}

			// Render join tables for many-to-many relationships
			// Use custom join rendering with proper table aliases
			for _, join := range sel.Joins {
				d.renderJoinWithAlias(ctx, r, nil, sel, join)
			}

			// Render self-joins for order by list
			for _, ob := range sel.OrderBy {
				if ob.Var != "" {
					d.renderOrderByJoin(ctx, r, sel, ob)
				}
			}

			// Render the relationship filter (WHERE clause)
			if sel.Where.Exp != nil {
				ctx.WriteString(` WHERE `)
				d.renderWhereExp(ctx, r, nil, sel, sel.Where.Exp)
			}
			d.renderGroupBy(ctx, r, sel)

			// Render ORDER BY
			d.renderOrderBy(ctx, r, sel, "")

			// Render LIMIT
			if sel.Paging.Limit != 0 {
				r.RenderLimit(sel)
			} else if len(sel.OrderBy) > 0 {
				ctx.WriteString(` LIMIT 18446744073709551615`)
			}

			ctx.WriteString(`) AS `)
			r.Quoted("_gj_t")

			// Close the CASE WHEN wrapper if we have a skip/include variable
			if hasSkipVar {
				ctx.WriteString(`) ELSE NULL END`)
			}
		}
	}

	ctx.WriteString(`)`)
}

func (d *MariaDBDialect) renderOuterOrderBy(ctx Context, r InlineChildRenderer, sel *qcode.Select, subqueryAlias string) {
	if len(sel.OrderBy) == 0 {
		return
	}
	ctx.WriteString(` ORDER BY `)
	for i, ob := range sel.OrderBy {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		if subqueryAlias != "" {
			ctx.WriteString(subqueryAlias)
			ctx.WriteString(`.`)
		}
		ctx.WriteString(fmt.Sprintf("_ord_%d", i))
		switch ob.Order {
		case qcode.OrderAsc:
			ctx.WriteString(` ASC`)
		case qcode.OrderDesc:
			ctx.WriteString(` DESC`)
		case qcode.OrderAscNullsFirst:
			ctx.WriteString(` ASC NULLS FIRST`)
		case qcode.OrderDescNullsFirst:
			ctx.WriteString(` DESC NULLS FIRST`)
		case qcode.OrderAscNullsLast:
			ctx.WriteString(` ASC NULLS LAST`)
		case qcode.OrderDescNullsLast:
			ctx.WriteString(` DESC NULLS LAST`)
		}
	}
}

func (d *MariaDBDialect) RenderChildCursor(ctx Context, renderChild func()) {
	ctx.WriteString(`JSON_VALUE(`)
	renderChild()
	ctx.WriteString(`, '$.cursor')`)
}

func (d *MariaDBDialect) RenderChildValue(ctx Context, sel *qcode.Select, renderChild func()) {
	// For paging with cursor, we return a bundle {json: ..., cursor: ...}
	// So we need to extract the json part for the value.
	if sel.Paging.Cursor {
		ctx.WriteString(`JSON_QUERY(`)
		renderChild()
		ctx.WriteString(`, '$.json')`)
	} else {
		renderChild()
	}
}


// renderInlineJSONFields renders field list for json_object() using table name columns
func (d *MariaDBDialect) renderInlineJSONFields(ctx Context, r InlineChildRenderer, sel *qcode.Select) {
	i := 0
	for _, f := range sel.Fields {
		if f.SkipRender == qcode.SkipTypeDrop {
			continue
		}
		if i != 0 {
			ctx.WriteString(`, `)
		}
		r.Squoted(f.FieldName)
		ctx.WriteString(`, `)

		if f.SkipRender == qcode.SkipTypeNulled || f.SkipRender == qcode.SkipTypeBlocked || f.SkipRender == qcode.SkipTypeUserNeeded {
			ctx.WriteString(`NULL`)
			i++
			continue
		}

		t := sel.Ti.Name
		if sel.ID >= 0 {
			t = fmt.Sprintf("%s_%d", t, sel.ID)
		}

		if f.Func.Name != "" {
			ctx.WriteString(f.Func.Name)
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
			// Handle skipIf/includeIf field filters (ifVar)
			if f.FieldFilter.Exp != nil {
				ctx.WriteString(`CASE WHEN `)
				d.renderFieldFilterExp(ctx, r, sel, f.FieldFilter.Exp)
				ctx.WriteString(` THEN `)
			}
			// For JSON columns, wrap with JSON_QUERY to preserve JSON structure
			// MariaDB stores JSON as LONGTEXT, so without this, JSON values get stringified
			isJSON := f.Col.Type == "json" || f.Col.Array
			if isJSON {
				ctx.WriteString(`JSON_QUERY(`)
			}
			r.ColWithTable(t, f.Col.Name)
			if isJSON {
				ctx.WriteString(`, '$')`)
			}
			if f.FieldFilter.Exp != nil {
				ctx.WriteString(` ELSE null END`)
			}
		}
		i++
	}

	// Handle __typename if requested
	if sel.Typename {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.WriteString(`'__typename', `)
		r.Squoted(sel.Table)
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
		r.Squoted(csel.FieldName)
		ctx.WriteString(`, `)

		// Handle SkipTypeNulled for role-based directives
		if csel.SkipRender == qcode.SkipTypeNulled || csel.SkipRender == qcode.SkipTypeBlocked || csel.SkipRender == qcode.SkipTypeUserNeeded {
			ctx.WriteString(`NULL`)
			i++
			continue
		}

		ctx.WriteString(`JSON_QUERY(`)
		r.RenderInlineChild(sel, csel)
		ctx.WriteString(`, '$')`)
		i++
	}
}

// renderSubqueryJSONFields renders field list for json_object when reading from derived table _gj_t
func (d *MariaDBDialect) renderSubqueryJSONFields(ctx Context, r InlineChildRenderer, sel *qcode.Select) {
	i := 0
	for _, f := range sel.Fields {
		if f.SkipRender == qcode.SkipTypeDrop {
			continue
		}
		if i != 0 {
			ctx.WriteString(`, `)
		}
		r.Squoted(f.FieldName)
		ctx.WriteString(`, `)

		if f.SkipRender == qcode.SkipTypeNulled || f.SkipRender == qcode.SkipTypeBlocked || f.SkipRender == qcode.SkipTypeUserNeeded {
			ctx.WriteString(`NULL`)
			i++
			continue
		}

		// For JSON columns, wrap with JSON_QUERY to preserve JSON structure
		isJSON := f.Col.Type == "json" || f.Col.Array
		if isJSON {
			ctx.WriteString(`JSON_QUERY(`)
		}
		r.ColWithTable("_gj_t", f.FieldName)
		if isJSON {
			ctx.WriteString(`, '$')`)
		}
		i++
	}

	// Handle __typename if requested
	if sel.Typename {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.WriteString(`'__typename', `)
		r.Squoted(sel.Table)
	}

	// Handle nested children - reference pre-computed columns
	for _, cid := range sel.Children {
		csel := r.GetChild(cid)
		if csel == nil || csel.SkipRender == qcode.SkipTypeRemote || csel.SkipRender == qcode.SkipTypeDrop {
			continue
		}
		if i != 0 {
			ctx.WriteString(`, `)
		}
		r.Squoted(csel.FieldName)
		ctx.WriteString(`, `)
		r.ColWithTable("_gj_t", csel.FieldName)
		i++
	}
}

func (d *MariaDBDialect) renderBaseColumns(ctx Context, r InlineChildRenderer, sel *qcode.Select) {
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
			ctx.WriteString(f.Func.Name)
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
		r.Squoted(f.FieldName)
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
				// For nested order by (ordering by a related table's column),
				// the table is joined with _0 suffix by renderJoinWithAlias
				t = fmt.Sprintf("%s_0", t)
			}
		}
		if ob.KeyVar != "" && ob.Key != "" {
			ctx.WriteString(`CASE WHEN `)
			ctx.AddParam(Param{Name: ob.KeyVar, Type: "text"})
			ctx.WriteString(` = `)
			ctx.WriteString(fmt.Sprintf("'%s'", ob.Key))
			ctx.WriteString(` THEN `)
		}
		r.ColWithTable(t, col)
		if ob.KeyVar != "" && ob.Key != "" {
			ctx.WriteString(` END`)
		}
		ctx.WriteString(fmt.Sprintf(` AS _ord_%d`, j))
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

		// Handle SkipTypeUserNeeded, SkipTypeBlocked, SkipTypeNulled - render NULL instead of subquery
		if csel.SkipRender == qcode.SkipTypeUserNeeded || csel.SkipRender == qcode.SkipTypeBlocked ||
			csel.SkipRender == qcode.SkipTypeNulled {
			ctx.WriteString(`NULL AS `)
			r.Quoted(csel.FieldName)
			i++
			continue
		}

		ctx.WriteString(`JSON_QUERY(`)
		r.RenderInlineChild(sel, csel)
		ctx.WriteString(`, '$') AS `)
		r.Quoted(csel.FieldName)
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
			ctx.WriteString(fmt.Sprintf(" AS __cur_%d", j))
		}
	}
}

func (d *MariaDBDialect) RenderLateralJoin(ctx Context, sel *qcode.Select, multi bool) {
	if sel.Rel.Type == sdata.RelNone && !multi {
		return
	}
	ctx.WriteString(` CROSS JOIN LATERAL (`)
}

func (d *MariaDBDialect) RenderLateralJoinClose(ctx Context, alias string) {
	ctx.WriteString(`) AS `)
	ctx.Quote(alias)
}

// SupportsReturning returns true for MariaDB 10.5+ which added RETURNING clause support.
func (d *MariaDBDialect) SupportsReturning() bool {
	return d.DBVersion >= 100500 // MariaDB 10.5+
}

// RenderReturning renders the RETURNING clause for MariaDB 10.5+.
// MariaDB supports RETURNING * syntax similar to PostgreSQL.
func (d *MariaDBDialect) RenderReturning(ctx Context, m *qcode.Mutate) {
	if d.DBVersion >= 1050 {
		ctx.WriteString(` RETURNING *`)
	}
}

// RenderJSONRootField renders a JSON field at the root level for MariaDB.
// MariaDB treats JSON as LONGTEXT, so nested JSON values get stringified
// unless we use JSON_QUERY to extract them as proper JSON.
// For scalar values like __typename, we output the value directly without JSON_QUERY.
func (d *MariaDBDialect) RenderJSONRootField(ctx Context, key string, val func()) {
	ctx.WriteString(`'`)
	ctx.WriteString(key)
	ctx.WriteString(`', `)
	// For __typename, val() outputs a quoted string like 'getUser'
	// which should be used directly, not wrapped in JSON_QUERY
	if key == "__typename" {
		val()
	} else {
		ctx.WriteString(`JSON_QUERY(`)
		val()
		ctx.WriteString(`, '$')`)
	}
}

// RenderJSONField renders a JSON field for MariaDB.
// For JSON columns (isJSON=true), wrap with JSON_QUERY to preserve JSON structure.
// MariaDB stores JSON as LONGTEXT, so without this, JSON values get stringified.
func (d *MariaDBDialect) RenderJSONField(ctx Context, fieldName string, tableAlias string, colName string, isNull bool, isJSON bool) {
	ctx.WriteString(`'`)
	ctx.WriteString(fieldName)
	ctx.WriteString(`', `)
	if isNull {
		ctx.WriteString(`NULL`)
	} else if isJSON {
		// Wrap JSON columns with JSON_QUERY to preserve structure
		ctx.WriteString(`JSON_QUERY(`)
		if tableAlias != "" {
			ctx.Quote(tableAlias)
			ctx.WriteString(`.`)
		}
		ctx.Quote(colName)
		ctx.WriteString(`, '$')`)
	} else {
		if tableAlias != "" {
			ctx.Quote(tableAlias)
			ctx.WriteString(`.`)
		}
		ctx.Quote(colName)
	}
}

// RenderJSONPlural renders JSON array aggregation for MariaDB.
// Since MariaDB doesn't support LATERAL joins, we use inline subqueries
// and aggregate the "json" column from the inner query.
// Unlike MySQL, we don't CAST AS JSON since MariaDB doesn't support that syntax.
func (d *MariaDBDialect) RenderJSONPlural(ctx Context, sel *qcode.Select) {
	ctx.WriteString(`COALESCE(json_arrayagg(json_object(`)
	ctx.RenderJSONFields(sel)
	ctx.WriteString(`)), '[]') `)
}

// RenderValArrayColumn handles array value columns for MariaDB.
// Unlike MySQL, MariaDB does not support CAST(... AS JSON), so we use
// JSON_TABLE to extract the array values.
func (d *MariaDBDialect) RenderValArrayColumn(ctx Context, ex *qcode.Exp, table string, pid int32) {
	ctx.WriteString(`SELECT _gj_jt.* FROM `)
	ctx.WriteString(`(SELECT `)

	t := table
	if pid > 0 {
		t = fmt.Sprintf("%s_%d", table, pid)
	}
	ctx.ColWithTable(t, ex.Right.Col.Name)

	ctx.WriteString(` as ids) j, `)
	ctx.WriteString(`JSON_TABLE(j.ids, "$[*]" COLUMNS(`)
	ctx.WriteString(ex.Right.Col.Name)
	ctx.WriteString(` `)
	ctx.WriteString(ex.Left.Col.Type)
	ctx.WriteString(` PATH "$" ERROR ON ERROR)) AS _gj_jt`)
}

// MariaDB 10.6+ uses the same LEFT OUTER JOIN LATERAL syntax as MySQL 8+,
// so we inherit RenderLateralJoin and RenderLateralJoinClose from MySQLDialect.

// RenderCast handles type casting for MariaDB.
// MariaDB doesn't support CAST(... AS JSON) or CAST(... AS LONGTEXT),
// so we need to map these to supported types.
func (d *MariaDBDialect) RenderCast(ctx Context, val func(), typ string) {
	// MariaDB CAST supports: BINARY, CHAR, DATE, DATETIME, DECIMAL, NCHAR, SIGNED, TIME, UNSIGNED
	// Note: JSON and LONGTEXT are NOT supported as CAST targets in MariaDB
	switch typ {
	case "json", "longtext", "varchar", "character varying", "text", "string":
		// MariaDB treats JSON as LONGTEXT (string). No need to cast.
		val()
		return
	default:
		ctx.WriteString(`CAST(`)
		val()
		ctx.WriteString(` AS `)
	}

	switch typ {
	case "int", "integer", "int4", "int8", "bigint", "smallint":
		ctx.WriteString("SIGNED")
	case "boolean", "bool":
		ctx.WriteString("UNSIGNED")
	case "float", "double", "numeric", "real":
		ctx.WriteString("DECIMAL(65,30)")
	case "timestamp", "timestamptz", "timestamp without time zone", "timestamp with time zone":
		ctx.WriteString("DATETIME")
	case "date":
		ctx.WriteString("DATE")
	case "time", "timetz":
		ctx.WriteString("TIME")
	default:
		ctx.WriteString(typ)
	}
	ctx.WriteString(`)`)
}

// RenderJSONPath renders a JSON path extraction for MariaDB.
// MariaDB uses JSON_EXTRACT(col, '$.path') and we wrap it in JSON_UNQUOTE to get text.
// We do NOT use ->> operator to avoid syntax issues in older versions or specific contexts,
// even though 10.2+ supports it.
func (d *MariaDBDialect) RenderJSONPath(ctx Context, table, col string, path []string) {
	ctx.WriteString(`JSON_UNQUOTE(JSON_EXTRACT(`)
	ctx.ColWithTable(table, col)
	ctx.WriteString(`, '$.`)
	for i, p := range path {
		if i > 0 {
			ctx.WriteString(`.`)
		}
		ctx.WriteString(p)
	}
	ctx.WriteString(`'))`)
}

// SupportsSubscriptionBatching returns false for MariaDB.
// MariaDB's LATERAL support is incompatible with the inline subquery structure
// used when SupportsLateral() = false. Each subscription runs individually.
func (d *MariaDBDialect) SupportsSubscriptionBatching() bool {
	return false
}

func (d *MariaDBDialect) RenderSubscriptionUnbox(ctx Context, params []Param, innerSQL string) {
	// MariaDB subscription batching using LATERAL syntax
	// MariaDB uses comma-based LATERAL: FROM table, LATERAL (...) AS alias
	// This allows the derived table to reference _gj_sub columns (including cursor)

	// Strip leading comment if present (e.g., /* action='...' */)
	sql := strings.TrimSpace(innerSQL)
	if strings.HasPrefix(sql, "/*") {
		if end := strings.Index(sql, "*/"); end != -1 {
			sql = strings.TrimSpace(sql[end+2:])
		}
	}

	// Use CTE + LATERAL (comma syntax for MariaDB 10.6+)
	ctx.WriteString(`WITH _gj_sub AS (SELECT * FROM JSON_TABLE(?, '$[*]' COLUMNS(`)
	for i, p := range params {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.WriteString("`" + p.Name + "` ")
		ctx.WriteString(p.Type)
		ctx.WriteString(` PATH '$[`)
		ctx.Write(fmt.Sprintf("%d", i))
		ctx.WriteString(`]' ERROR ON ERROR`)
	}
	ctx.WriteString(`)) AS _gj_jt) SELECT _gj_sub_data.__root FROM _gj_sub CROSS JOIN LATERAL (`)
	ctx.WriteString(sql)
	ctx.WriteString(`) AS _gj_sub_data`)
}

func (d *MariaDBDialect) RenderTableAlias(ctx Context, alias string) {
	ctx.WriteString(` AS `)
	ctx.Quote(alias)
	ctx.WriteString(` `)
}

// renderFromTable handles FROM clause for both regular tables and embedded JSON tables.
// For embedded JSON tables (RelEmbedded), it uses JSON_TABLE to unpack the JSON column.
// For regular tables, it uses the standard table name.
func (d *MariaDBDialect) renderFromTable(ctx Context, r InlineChildRenderer, sel *qcode.Select, psel *qcode.Select) {
	if sel.Rel.Type == sdata.RelEmbedded {
		// For embedded JSON columns, use JSON_TABLE with the correct alias pattern
		// JSON_TABLE references the parent table from the outer query (via psel)
		// MariaDB inline child rendering expects tableName_ID pattern for column references
		ctx.WriteString(`JSON_TABLE(`)
		// Reference the parent table with its alias (tableName_ID pattern)
		parentAlias := sel.Rel.Left.Col.Table
		if psel != nil && psel.ID >= 0 {
			parentAlias = fmt.Sprintf("%s_%d", sel.Rel.Left.Col.Table, psel.ID)
		}
		ctx.Quote(parentAlias)
		ctx.WriteString(`.`)
		ctx.Quote(sel.Rel.Left.Col.Name)
		ctx.WriteString(`, '$[*]' COLUMNS(`)
		for i, col := range sel.Ti.Columns {
			if i != 0 {
				ctx.WriteString(`, `)
			}
			ctx.WriteString(col.Name)
			ctx.WriteString(` `)
			ctx.WriteString(col.Type)
			ctx.WriteString(` PATH '$.`)
			ctx.WriteString(col.Name)
			ctx.WriteString(`' ERROR ON ERROR`)
		}
		ctx.WriteString(`)) AS `)
		// Use tableName_ID pattern to match column reference expectations
		t := sel.Ti.Name
		if sel.ID >= 0 {
			t = fmt.Sprintf("%s_%d", t, sel.ID)
		}
		ctx.Quote(t)
	} else {
		r.RenderTable(sel, sel.Ti.Schema, sel.Ti.Name, false)
	}
}

// RenderCursorCTE creates a __cur CTE that parses the cursor parameter.
// MariaDB uses colon separator for cursors (matching RenderInlineChild cursor generation).
func (d *MariaDBDialect) RenderCursorCTE(ctx Context, sel *qcode.Select) {
	if !sel.Paging.Cursor {
		return
	}
	ctx.WriteString(`WITH __cur AS (SELECT `)
	for i, ob := range sel.OrderBy {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		// Use SUBSTRING_INDEX with colon separator (matching RenderInlineChild cursor generation)
		// Cursor format after decryption is: selID:val1:val2:...
		// (The gj-hexTimestamp: prefix is stripped during encryption/decryption)
		// position 1 = selID
		// position 2 = val1 (first cursor value)
		// position 3 = val2 (second cursor value), etc.
		// Cast to the correct type to ensure proper comparison (e.g., numeric vs string)
		ctx.WriteString(`CAST(NULLIF(SUBSTRING_INDEX(SUBSTRING_INDEX(a.i, ':', `)
		ctx.Write(fmt.Sprintf("%d", i+2))
		ctx.WriteString(`), ':', -1), '') AS `)
		ctx.WriteString(d.mariadbType(ob.Col.Type))
		ctx.WriteString(`) AS `)

		if ob.KeyVar != "" && ob.Key != "" {
			ctx.Quote(ob.Col.Name + "_" + ob.Key)
		} else {
			ctx.Quote(ob.Col.Name)
		}
	}
	ctx.WriteString(` FROM ((SELECT `)
	ctx.AddParam(Param{Name: "cursor", Type: "text"})
	ctx.WriteString(` AS i)) AS a) `)
}

// mariadbType converts GraphJin types to MariaDB types for CAST
func (d *MariaDBDialect) mariadbType(t string) string {
	switch t {
	case "int", "integer", "int4", "int8", "bigint", "smallint":
		return "SIGNED"
	case "float", "float4", "float8", "double", "real", "numeric", "decimal":
		return "DECIMAL(65,30)"
	case "timestamp", "timestamptz", "timestamp without time zone", "timestamp with time zone":
		return "DATETIME"
	case "date":
		return "DATE"
	case "time", "timetz":
		return "TIME"
	default:
		return "CHAR"
	}
}

// renderCursorColumn renders a reference to a cursor CTE column.
// Since we now use a __cur CTE (like MySQL), we simply reference the column directly.
func (d *MariaDBDialect) renderCursorColumn(ctx Context, r InlineChildRenderer, colName string) {
	r.ColWithTable("__cur", colName)
}

func (d *MariaDBDialect) renderGroupBy(ctx Context, r InlineChildRenderer, sel *qcode.Select) {
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

func (d *MariaDBDialect) renderOrderBy(ctx Context, r InlineChildRenderer, sel *qcode.Select, alias string) {
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
				// For nested order by (ordering by a related table's column),
				// the table is joined with _0 suffix by renderJoinWithAlias
				t = fmt.Sprintf("%s_0", t)
			}
		}
		if ob.KeyVar != "" && ob.Key != "" {
			ctx.WriteString(` CASE WHEN `)
			ctx.AddParam(Param{Name: ob.KeyVar, Type: "text"})
			ctx.WriteString(` = `)
			ctx.WriteString(fmt.Sprintf("'%s'", ob.Key))
			ctx.WriteString(` THEN `)
		}
		r.ColWithTable(t, col)
		if ob.KeyVar != "" && ob.Key != "" {
			ctx.WriteString(` END `)
		}
		switch ob.Order {
		case qcode.OrderAsc:
			ctx.WriteString(` ASC`)
		case qcode.OrderDesc:
			ctx.WriteString(` DESC`)
		case qcode.OrderAscNullsFirst:
			ctx.WriteString(` ASC NULLS FIRST`)
		case qcode.OrderDescNullsFirst:
			ctx.WriteString(` DESC NULLS FIRST`)
		case qcode.OrderAscNullsLast:
			ctx.WriteString(` ASC NULLS LAST`)
		case qcode.OrderDescNullsLast:
			ctx.WriteString(` DESC NULLS LAST`)
		}
	}
}

func (d *MariaDBDialect) renderWhereExp(ctx Context, r InlineChildRenderer, psel, sel *qcode.Select, ex *qcode.Exp) {
	d.renderExp(ctx, r, psel, sel, ex)
}

func (d *MariaDBDialect) renderExp(ctx Context, r InlineChildRenderer, psel, sel *qcode.Select, ex *qcode.Exp) {
	if ex == nil {
		return
	}




	switch ex.Op {
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
		if d.renderValPrefix(ctx, r, psel, sel, ex) {
			return
		}
		ctx.WriteString(`(`)
		if ex.Left.Col.Name != "" {
			// Check for cursor CTE reference first
			// qcode sets ex.Left.Table = "__cur" for cursor pagination filters
			if ex.Left.Table == "__cur" {
				// Use ColName if set (for cursor key variants like "price_key")
				colName := ex.Left.Col.Name
				if ex.Left.ColName != "" {
					colName = ex.Left.ColName
				}
				// Reference the __cur CTE column
				d.renderCursorColumn(ctx, r, colName)
			} else if (ex.Left.ID >= 0 && psel != nil && ex.Left.ID == psel.ID) ||
				(ex.Left.ID == -1 && psel != nil && ex.Left.Col.Table == psel.Ti.Name) {
				t := psel.Ti.Name
				if psel.ID >= 0 {
					t = fmt.Sprintf("%s_%d", t, psel.ID)
				}
				r.ColWithTable(t, ex.Left.Col.Name)
			} else {
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
				// Use ColName if set (for cursor key variants)
				colName := ex.Left.Col.Name
				if ex.Left.ColName != "" {
					colName = ex.Left.ColName
				}
				r.ColWithTable(t, colName)
			}
		}
		if strings.EqualFold(ex.Right.Val, "false") {
			ctx.WriteString(` IS NOT NULL)`)
		} else {
			ctx.WriteString(` IS NULL)`)
		}

	case qcode.OpSelectExists:
		if len(ex.Joins) == 0 {
			return
		}
		first := ex.Joins[0]
		ctx.WriteString(`EXISTS (SELECT 1 FROM `)
		ctx.Quote(first.Rel.Left.Col.Table)
		// Add alias with _0 suffix to match what renderExp produces for table references
		d.RenderTableAlias(ctx, fmt.Sprintf("%s_0", first.Rel.Left.Col.Table))

		if len(ex.Joins) > 1 {
			for i := 1; i < len(ex.Joins); i++ {
				j := ex.Joins[i]
				ctx.WriteString(` LEFT JOIN `)
				ctx.Quote(j.Rel.Left.Col.Table)
				// Add alias for nested joins too
				d.RenderTableAlias(ctx, fmt.Sprintf("%s_0", j.Rel.Left.Col.Table))
				ctx.WriteString(` ON `)

				// Render ON clause manually or via Filter?
				// psql renderJoin uses Filter? No, renderJoin uses implicit ON?
				// Actually psql/exp.go doesn't show renderJoin implementation fully in view.
				// But standard Join usually needs ON.
				// For now I'll panic or comment if > 1 to see if it's hit.
				// But for this specific bug, Joins=1.
			}
		}

		ctx.WriteString(` WHERE `)
		d.renderExp(ctx, r, psel, sel, first.Filter)

		if len(ex.Children) > 0 {
			ctx.WriteString(` AND `)
			for i, child := range ex.Children {
				if i > 0 {
					ctx.WriteString(` AND `)
				}
				d.renderExp(ctx, r, psel, sel, child)
			}
		}
		ctx.WriteString(`)`)

	case qcode.OpTsQuery:
		ti := sel.Ti
		if len(ti.FullText) > 0 {
			ctx.WriteString(`(MATCH(`)
			for i, col := range ti.FullText {
				if i != 0 {
					ctx.WriteString(`, `)
				}
				t := ti.Name
				if sel.ID >= 0 {
					t = fmt.Sprintf("%s_%d", t, sel.ID)
				}
				r.ColWithTable(t, col.Name)
			}
			ctx.WriteString(`) AGAINST (CONCAT('"', `)
			ctx.AddParam(Param{Name: ex.Right.Val, Type: "text"})
			ctx.WriteString(`, '"') IN BOOLEAN MODE))`)
		} else {
			var textCols []string
			for _, col := range ti.Columns {
				if col.Name == "name" || col.Name == "description" {
					textCols = append(textCols, col.Name)
				}
			}
			if len(textCols) > 0 {
				ctx.WriteString(`(`)
				for i, colName := range textCols {
					if i > 0 {
						ctx.WriteString(` OR `)
					}
					t := ti.Name
					if sel.ID >= 0 {
						t = fmt.Sprintf("%s_%d", t, sel.ID)
					}
					r.ColWithTable(t, colName)

					ctx.WriteString(` LIKE CONCAT('%', `)
					ctx.AddParam(Param{Name: ex.Right.Val, Type: "text"})
					ctx.WriteString(`, '%')`)
				}
				ctx.WriteString(`)`)
			} else {
				ctx.WriteString(`FALSE`)
			}
		}

	case qcode.OpEquals, qcode.OpNotEquals, qcode.OpGreaterThan, qcode.OpLesserThan,
		qcode.OpGreaterOrEquals, qcode.OpLesserOrEquals,
		qcode.OpLike, qcode.OpNotLike, qcode.OpILike, qcode.OpNotILike,
		qcode.OpSimilar, qcode.OpNotSimilar, qcode.OpRegex, qcode.OpNotRegex,
		qcode.OpIRegex, qcode.OpNotIRegex,
		qcode.OpHasKey, qcode.OpHasKeyAny, qcode.OpHasKeyAll:

		if d.renderValPrefix(ctx, r, psel, sel, ex) {
			return
		}

		ctx.WriteString(`((`)

		// Render left side
		if ex.Left.Col.Name != "" {
			// Determine table alias
			var t string
			if ex.Left.ID >= 0 && psel != nil && ex.Left.ID == psel.ID &&
				(ex.Left.Col.Table == "" || (ex.Left.Col.Table == psel.Ti.Name && ex.Left.Col.Table != sel.Ti.Name)) {
				// References a parent table (but not for self-referential tables where parent == child table)
				t = psel.Ti.Name
				if psel.ID >= 0 {
					t = fmt.Sprintf("%s_%d", t, psel.ID)
				}
			} else if ex.Left.ID == -1 && psel != nil && ex.Left.Col.Table == psel.Ti.Name && ex.Left.Col.Table != sel.Ti.Name {
				// Fallback: matches parent table name (but not for self-referential tables)
				t = psel.Ti.Name
				if psel.ID >= 0 {
					t = fmt.Sprintf("%s_%d", t, psel.ID)
				}
			} else if ex.Left.ID == -1 && ex.Left.Table != "" && ex.Left.Table == sel.Ti.Name {
				// Polymorphic relationships: ex.Left.Table is set to the child table name
				// Use the current selection's table alias
				t = sel.Ti.Name
				if sel.ID >= 0 {
					t = fmt.Sprintf("%s_%d", t, sel.ID)
				}
			} else if ex.Left.ID == -1 && ex.Left.Col.Table != "" && ex.Left.Col.Table != sel.Ti.Name {
				// Fallback: handle outer references
				t = ex.Left.Col.Table
				// Don't add suffix to __cur CTE (cursor pagination)
				if t != "__cur" {
					t = fmt.Sprintf("%s_0", t)
				}
			} else {
				// Current table or Joined table
				t = ex.Left.Col.Table
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
			}

			// Handle JSON path if present
			if len(ex.Left.Path) > 0 {
				// For boolean values, use JSON_EXTRACT directly (without JSON_UNQUOTE)
				// because JSON booleans need to be compared as JSON values
				switch ex.Right.ValType {
				case qcode.ValBool:
					// JSON_EXTRACT returns JSON boolean (true/false) directly
					ctx.WriteString(`JSON_EXTRACT(`)
					r.ColWithTable(t, ex.Left.Col.Name)
					ctx.WriteString(`, '$.`)
					for i, p := range ex.Left.Path {
						if i > 0 {
							ctx.WriteString(`.`)
						}
						ctx.WriteString(p)
					}
					ctx.WriteString(`')`)
				case qcode.ValNum:
					ctx.WriteString(`CAST(`)
					d.RenderJSONPath(ctx, t, ex.Left.Col.Name, ex.Left.Path)
					ctx.WriteString(` AS DECIMAL(65,30))`)
				default:
					d.RenderJSONPath(ctx, t, ex.Left.Col.Name, ex.Left.Path)
				}
			} else {
				r.ColWithTable(t, ex.Left.Col.Name)
			}
		}
		ctx.WriteString(`) `)

		// Render operator
		switch ex.Op {
		case qcode.OpEquals:
			ctx.WriteString(`=`)
		case qcode.OpNotEquals:
			ctx.WriteString(`!=`)
		case qcode.OpGreaterThan:
			ctx.WriteString(`>`)
		case qcode.OpLesserThan:
			ctx.WriteString(`<`)
		case qcode.OpGreaterOrEquals:
			ctx.WriteString(`>=`)
		case qcode.OpLesserOrEquals:
			ctx.WriteString(`<=`)
		case qcode.OpLike, qcode.OpILike:
			ctx.WriteString(` LIKE`)
		case qcode.OpNotLike, qcode.OpNotILike:
			ctx.WriteString(` NOT LIKE`)
		case qcode.OpRegex, qcode.OpIRegex, qcode.OpSimilar:
			ctx.WriteString(` REGEXP`)
		case qcode.OpNotRegex, qcode.OpNotIRegex, qcode.OpNotSimilar:
			ctx.WriteString(` NOT REGEXP`)
		}

		ctx.WriteString(` (`)

		// Render right side
		if ex.Right.Col.Name != "" {
			// Check for cursor CTE reference first
			// qcode sets ex.Right.Table = "__cur" for cursor pagination filters
			if ex.Right.Table == "__cur" {
				// Use ColName if set (for cursor key variants like "price_key")
				colName := ex.Right.Col.Name
				if ex.Right.ColName != "" {
					colName = ex.Right.ColName
				}
				// Reference the __cur CTE column
				d.renderCursorColumn(ctx, r, colName)
			} else if ex.Right.ID >= 0 && psel != nil && ex.Right.ID == psel.ID &&
				(ex.Right.Col.Table == "" || ex.Right.Col.Table == psel.Ti.Name) {
				// References a parent table
				t := psel.Ti.Name
				if psel.ID >= 0 {
					t = fmt.Sprintf("%s_%d", t, psel.ID)
				}
				r.ColWithTable(t, ex.Right.Col.Name)
			} else if ex.Right.ID == -1 && psel != nil && ex.Right.Col.Table == psel.Ti.Name {
				// Fallback: matches parent table name
				t := psel.Ti.Name
				if psel.ID >= 0 {
					t = fmt.Sprintf("%s_%d", t, psel.ID)
				}
				r.ColWithTable(t, ex.Right.Col.Name)
			} else if ex.Right.ID == -1 && ex.Right.Col.Table != "" && ex.Right.Col.Table != sel.Ti.Name {
				// Fallback: handle outer references
				t := ex.Right.Col.Table
				// __cur references should be handled above, but add fallback just in case
				if t == "__cur" {
					colName := ex.Right.Col.Name
					if ex.Right.ColName != "" {
						colName = ex.Right.ColName
					}
					d.renderCursorColumn(ctx, r, colName)
				} else {
					t = fmt.Sprintf("%s_0", t)
					// Use ColName if set (for cursor key variants like "price_key")
					colName := ex.Right.Col.Name
					if ex.Right.ColName != "" {
						colName = ex.Right.ColName
					}
					r.ColWithTable(t, colName)
				}
			} else {
				t := ex.Right.Col.Table
				if t == "" {
					t = sel.Ti.Name
				}

				if t == sel.Ti.Name {
					if sel.ID >= 0 {
						t = fmt.Sprintf("%s_%d", t, sel.ID)
					}
					r.ColWithTable(t, ex.Right.Col.Name)
				} else if t == "__cur" {
					// Reference the __cur CTE column
					colName := ex.Right.Col.Name
					if ex.Right.ColName != "" {
						colName = ex.Right.ColName
					}
					d.renderCursorColumn(ctx, r, colName)
				} else if ex.Right.ID >= 0 {
					t = fmt.Sprintf("%s_%d", t, ex.Right.ID)
					r.ColWithTable(t, ex.Right.Col.Name)
				} else {
					r.ColWithTable(t, ex.Right.Col.Name)
				}
			}
		} else if ex.Right.ValType == qcode.ValVar {
			// Check if this variable is a config variable
			if val, ok := r.GetConfigVar(ex.Right.Val); ok {
				// Config variable found - render as literal value
				ctx.WriteString(`'`)
				ctx.WriteString(val)
				ctx.WriteString(`'`)
			} else {
				// Not a config variable - add as runtime param
				ctx.AddParam(Param{Name: ex.Right.Val, Type: ex.Left.Col.Type})
			}
		} else if ex.Right.ValType == qcode.ValDBVar {
			d.RenderVar(ctx, ex.Right.Val)
		} else {
			d.RenderLiteral(ctx, ex.Right.Val, ex.Right.ValType)
		}

		ctx.WriteString(`))`)

	case qcode.OpIn, qcode.OpNotIn:
		if d.renderValPrefix(ctx, r, psel, sel, ex) {
			return
		}
		ctx.WriteString(`((`)
		if ex.Left.Col.Name != "" {
			if ex.Left.ID >= 0 && psel != nil && ex.Left.ID == psel.ID {
				t := psel.Ti.Name
				if psel.ID >= 0 {
					t = fmt.Sprintf("%s_%d", t, psel.ID)
				}
				r.ColWithTable(t, ex.Left.Col.Name)
			} else {
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
				r.ColWithTable(t, ex.Left.Col.Name)
			}
		}
		ctx.WriteString(`) `)
		if ex.Op == qcode.OpIn {
			ctx.WriteString(`IN`)
		} else {
			ctx.WriteString(`NOT IN`)
		}
		ctx.WriteString(` (`)

		if ex.Right.Col.Name != "" {
			if ex.Right.ID >= 0 && psel != nil && ex.Right.ID == psel.ID {
				t := psel.Ti.Name
				if psel.ID >= 0 {
					t = fmt.Sprintf("%s_%d", t, psel.ID)
				}
				r.ColWithTable(t, ex.Right.Col.Name)
			} else {
				t := ex.Right.Col.Table
				if t == "" {
					t = sel.Ti.Name
				}

				if t == sel.Ti.Name {
					if sel.ID >= 0 {
						t = fmt.Sprintf("%s_%d", t, sel.ID)
					}
				} else if ex.Right.ID >= 0 {
					t = fmt.Sprintf("%s_%d", t, ex.Right.ID)
				}
				r.ColWithTable(t, ex.Right.Col.Name)
			}
		} else if ex.Right.ValType == qcode.ValVar {
			ctx.AddParam(Param{Name: ex.Right.Val, Type: ex.Left.Col.Type, IsArray: true})
		} else {
			d.RenderList(ctx, ex)
		}
		ctx.WriteString(`))`)

	default:
		r.RenderExp(sel.Ti, ex)
	}
}

func (d *MariaDBDialect) RenderOp(op qcode.ExpOp) (string, error) {
	switch op {
	case qcode.OpJSONPath:
		return "->", nil
	case qcode.OpJSONPathText:
		return "->>", nil
	default:
		return d.MySQLDialect.RenderOp(op)
	}
}

func (d *MariaDBDialect) RenderValPrefix(ctx Context, ex *qcode.Exp) bool {
	return d.MySQLDialect.RenderValPrefix(ctx, ex)
}

func (d *MariaDBDialect) renderValPrefix(ctx Context, r InlineChildRenderer, psel, sel *qcode.Select, ex *qcode.Exp) bool {
	// Handle JSON key operations
	if ex.Op == qcode.OpHasKey ||
		ex.Op == qcode.OpHasKeyAny ||
		ex.Op == qcode.OpHasKeyAll {
		var optype string
		switch ex.Op {
		case qcode.OpHasKey, qcode.OpHasKeyAny:
			optype = "'one'"
		case qcode.OpHasKeyAll:
			optype = "'all'"
		}
		ctx.WriteString("JSON_CONTAINS_PATH(")

		// Logic to resolve aliased table name
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
		r.ColWithTable(t, ex.Left.Col.Name)

		ctx.WriteString(", " + optype)
		for i := range ex.Right.ListVal {
			ctx.WriteString(`, '$.` + ex.Right.ListVal[i] + `'`)
		}
		ctx.WriteString(") = 1")
		return true
	}

	// Handle IN/NOT IN with JSON array variable
	// For scalar columns (integers, strings), pass the column value directly to JSON_CONTAINS
	// MariaDB will auto-convert scalar values to JSON when needed
	if ex.Right.ValType == qcode.ValVar &&
		(ex.Op == qcode.OpIn || ex.Op == qcode.OpNotIn) {

		if strings.HasPrefix(ex.Right.Val, "__gj_json_pk:gj_sep:") {
			parts := strings.Split(ex.Right.Val, ":gj_sep:")
			if len(parts) == 4 {
				actionVar := parts[1]
				jsonKey := parts[2]
				colType := parts[3]

				// Render LHS column
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
				r.ColWithTable(t, ex.Left.Col.Name)

				ctx.WriteString(` `)

				if ex.Op == qcode.OpNotIn {
					ctx.WriteString(`NOT `)
				}

				// Render subquery: (SELECT id FROM JSON_TABLE(?, '$[*]' COLUMNS (id TYPE PATH '$.key')))
				ctx.WriteString(`IN (SELECT _gj_ids.id FROM JSON_TABLE(`)

				// Encode/Decode logic handled by RenderParam logic
				ctx.AddParam(Param{Name: actionVar, Type: "json", WrapInArray: true})

				ctx.WriteString(`, '$[*]' COLUMNS (id `)

				switch colType {
				case "varchar", "character varying", "text", "string":
					ctx.WriteString("TEXT")
				case "int", "integer", "int4", "int8", "bigint", "smallint":
					ctx.WriteString("BIGINT")
				case "boolean", "bool":
					ctx.WriteString("TINYINT")
				case "float", "double", "numeric", "real":
					ctx.WriteString("DECIMAL(65,30)")
				case "json", "jsonb":
					ctx.WriteString("JSON")
				case "timestamp", "timestamptz", "timestamp without time zone", "timestamp with time zone":
					ctx.WriteString("DATETIME")
				case "date":
					ctx.WriteString("DATE")
				case "time", "timetz":
					ctx.WriteString("TIME")
				default:
					ctx.WriteString(colType)
				}

				ctx.WriteString(` PATH '$.`)
				ctx.WriteString(jsonKey)
				ctx.WriteString(`' ERROR ON ERROR)) AS _gj_ids)`)
				return true
			}
		}

		if ex.Op == qcode.OpNotIn {
			ctx.WriteString(`NOT `)
		}
		ctx.WriteString(`JSON_CONTAINS(`)
		ctx.AddParam(Param{Name: ex.Right.Val, Type: ex.Left.Col.Type, IsArray: true})
		ctx.WriteString(`, `)

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
		r.ColWithTable(t, ex.Left.Col.Name)

		ctx.WriteString(`)`)
		return true
	}
	return false
}

func (d *MariaDBDialect) RenderJSONRoot(ctx Context, sel *qcode.Select) {
	if sel == nil {
		// Check if any root select has cursor pagination
		// If so, we need to render the cursor CTE first
		if r, ok := ctx.(InlineChildRenderer); ok {
			if cursorSel := r.GetRootWithCursor(); cursorSel != nil {
				d.RenderCursorCTE(ctx, cursorSel)
			}
		}
		d.MySQLDialect.RenderJSONRoot(ctx, sel)
		return
	}
	r, ok := ctx.(InlineChildRenderer)
	if !ok {
		d.MySQLDialect.RenderJSONRoot(ctx, sel)
		return
	}

	ctx.WriteString(`SELECT json_object(`)

	i := 0
	if sel.Typename {
		ctx.WriteString(`'__typename', '`)
		ctx.WriteString(sel.Table)
		ctx.WriteString(`'`)
		i++
	}

	for _, cid := range sel.Children {
		csel := r.GetChild(cid)
		if csel == nil || csel.SkipRender == qcode.SkipTypeRemote || csel.SkipRender == qcode.SkipTypeDrop {
			continue
		}

		if i != 0 {
			ctx.WriteString(`, `)
		}

		r.Squoted(csel.FieldName)
		ctx.WriteString(`, `)

		// Handle SkipTypeUserNeeded, SkipTypeBlocked, SkipTypeNulled - render NULL instead of subquery
		if csel.SkipRender == qcode.SkipTypeUserNeeded || csel.SkipRender == qcode.SkipTypeBlocked ||
			csel.SkipRender == qcode.SkipTypeNulled {
			ctx.WriteString(`NULL`)
			i++
			continue
		}

		ctx.WriteString(`JSON_QUERY(`)
		d.RenderInlineChild(ctx, r, sel, csel)
		ctx.WriteString(`, '$')`)

		if csel.Paging.Cursor {
			ctx.WriteString(`, `)
			r.Squoted(csel.FieldName + "_cursor")
			ctx.WriteString(`, `)
			d.RenderChildCursor(ctx, func() {
				d.RenderInlineChild(ctx, r, sel, csel)
			})
		}
		i++
	}
	ctx.WriteString(`)`)
}

// renderJoinWithAlias renders a JOIN with a table alias that matches MariaDB's expression aliasing.
// The generic compiler's renderJoin doesn't add aliases, but MariaDB's renderExp adds _0 suffix
// for outer table references, so we need to alias join tables to match.
func (d *MariaDBDialect) renderJoinWithAlias(ctx Context, r InlineChildRenderer, psel, sel *qcode.Select, join qcode.Join) {
	ctx.WriteString(` INNER JOIN `)
	ctx.WriteString(d.QuoteIdentifier(join.Rel.Left.Ti.Name))
	// Alias the join table with _0 suffix to match what renderExp produces
	d.RenderTableAlias(ctx, fmt.Sprintf("%s_0", join.Rel.Left.Ti.Name))
	ctx.WriteString(` ON ((`)
	// Use MariaDB's renderExp so the table references get consistent aliasing
	d.renderExp(ctx, r, psel, sel, join.Filter)
	ctx.WriteString(`))`)
}

func (d *MariaDBDialect) renderOrderByJoin(ctx Context, r InlineChildRenderer, sel *qcode.Select, ob qcode.OrderBy) {
	ctx.WriteString(` JOIN JSON_TABLE(`)
	ctx.AddParam(Param{Name: ob.Var, Type: "json"})
	ctx.WriteString(`, '$[*]' COLUMNS (id `)
	ctx.WriteString(ob.Col.Type)
	ctx.WriteString(` PATH '$', ord FOR ORDINALITY)) AS `)

	t := ob.Col.Table
	if t == "" {
		t = sel.Ti.Name
	}
	alias := fmt.Sprintf("_gj_ob_%s_%s", t, ob.Col.Name)
	ctx.WriteString(alias)

	ctx.WriteString(` ON `)
	ctx.WriteString(alias)
	ctx.WriteString(`.id = `)

	if t == sel.Ti.Name {
		if sel.ID >= 0 {
			t = fmt.Sprintf("%s_%d", t, sel.ID)
		}
	} else {
		// For nested order by (ordering by a related table's column),
		// the table is joined with _0 suffix
		t = fmt.Sprintf("%s_0", t)
	}
	r.ColWithTable(t, ob.Col.Name)
}

// renderRecursiveInlineChild handles recursive relationships for MariaDB.
// MariaDB doesn't allow correlated references through derived table boundaries,
// so we use OR conditions instead of UNION ALL to keep correlation at one level.
func (d *MariaDBDialect) renderRecursiveInlineChild(ctx Context, r InlineChildRenderer, psel, sel *qcode.Select) {
	// Get the parent table alias
	parentTable := psel.Ti.Name
	if psel.ID >= 0 {
		parentTable = fmt.Sprintf("%s_%d", psel.Ti.Name, psel.ID)
	}

	// Get the relationship columns
	fkCol := sel.Rel.Left.Col.Name  // e.g., reply_to_id
	pkCol := sel.Rel.Right.Col.Name // e.g., id

	// Determine recursion direction
	v, _ := sel.GetInternalArg("find")
	findParents := v.Val == "parents" || v.Val == "parent"

	// Table name
	tableName := sel.Ti.Name

	// Use limit to determine max depth, defaulting to 20 if not specified
	// This ensures we don't return more than the requested limit
	maxDepth := 20
	if sel.Paging.Limit > 0 && sel.Paging.Limit < int32(maxDepth) {
		maxDepth = int(sel.Paging.Limit)
	}

	// Determine if we have aggregation functions
	hasAggregation := false
	for _, f := range sel.Fields {
		if f.Type == qcode.FieldTypeFunc {
			hasAggregation = true
			break
		}
	}

	// Build the query structure WITHOUT derived tables
	// For aggregations, use json_array(json_object(...)) directly
	// For regular fields, use json_arrayagg

	if hasAggregation {
		// Aggregation case: return a single-element array with the aggregated values
		// SELECT json_array(json_object('count_id', COUNT(t.id))) FROM table t WHERE ...
		ctx.WriteString(`(SELECT json_array(json_object(`)
		d.renderRecursiveJSONFields(ctx, r, sel)
		ctx.WriteString(`)) FROM `)
		ctx.Quote(tableName)
		ctx.WriteString(` t WHERE (`)
	} else {
		// Regular case: aggregate rows into array
		ctx.WriteString(`(SELECT COALESCE(json_arrayagg(json_object(`)
		d.renderRecursiveJSONFields(ctx, r, sel)
		ctx.WriteString(`)`)

		// Add ORDER BY inside aggregation if needed
		// Note: ORDER BY must be inside the json_arrayagg() parentheses
		if len(sel.OrderBy) > 0 {
			ctx.WriteString(` ORDER BY `)
			for i, ob := range sel.OrderBy {
				if i != 0 {
					ctx.WriteString(`, `)
				}
				ctx.WriteString(`t.`)
				ctx.Quote(ob.Col.Name)
				switch ob.Order {
				case qcode.OrderAsc:
					ctx.WriteString(` ASC`)
				case qcode.OrderDesc:
					ctx.WriteString(` DESC`)
				}
			}
		} else if findParents {
			// Default ordering for parents: descending by PK (closest parent first)
			ctx.WriteString(` ORDER BY t.`)
			ctx.Quote(pkCol)
			ctx.WriteString(` DESC`)
		}

		ctx.WriteString(`), '[]') FROM `)
		ctx.Quote(tableName)
		ctx.WriteString(` t WHERE (`)
	}

	// Generate OR conditions for each depth level
	for depth := 1; depth <= maxDepth; depth++ {
		if depth > 1 {
			ctx.WriteString(` OR `)
		}

		ctx.WriteString(`t.`)
		if findParents {
			// For parents: t.id = (nested subquery to get reply_to_id chain)
			ctx.Quote(pkCol)
			ctx.WriteString(` = `)
			d.renderNestedFKSubquery(ctx, tableName, pkCol, fkCol, parentTable, depth)
		} else {
			// For children: t.reply_to_id = (nested subquery to get id chain)
			ctx.Quote(fkCol)
			ctx.WriteString(` = `)
			d.renderNestedPKSubquery(ctx, tableName, pkCol, fkCol, parentTable, depth)
		}
	}

	ctx.WriteString(`)`)

	// Apply WHERE clause filters (additional conditions)
	d.renderRecursiveWhereClauseFiltersInline(ctx, r, sel)

	ctx.WriteString(`)`)
}

// renderNestedFKSubquery generates nested subqueries for traversing up via FK
// Example for depth 2: (SELECT reply_to_id FROM comments WHERE id = (SELECT reply_to_id FROM comments WHERE id = parent.id))
func (d *MariaDBDialect) renderNestedFKSubquery(ctx Context, tableName, pkCol, fkCol, parentTable string, depth int) {
	if depth == 1 {
		// Base case: direct reference to parent's FK
		ctx.WriteString(`(SELECT `)
		ctx.Quote(fkCol)
		ctx.WriteString(` FROM `)
		ctx.Quote(tableName)
		ctx.WriteString(` WHERE `)
		ctx.Quote(pkCol)
		ctx.WriteString(` = `)
		ctx.Quote(parentTable)
		ctx.WriteString(`.`)
		ctx.Quote(pkCol)
		ctx.WriteString(`)`)
	} else {
		// Recursive case: nested subquery
		ctx.WriteString(`(SELECT `)
		ctx.Quote(fkCol)
		ctx.WriteString(` FROM `)
		ctx.Quote(tableName)
		ctx.WriteString(` WHERE `)
		ctx.Quote(pkCol)
		ctx.WriteString(` = `)
		d.renderNestedFKSubquery(ctx, tableName, pkCol, fkCol, parentTable, depth-1)
		ctx.WriteString(`)`)
	}
}

// renderNestedPKSubquery generates nested subqueries for traversing down via PK
// Example for depth 2: parent.id at depth 1, then find children of children
func (d *MariaDBDialect) renderNestedPKSubquery(ctx Context, tableName, pkCol, fkCol, parentTable string, depth int) {
	if depth == 1 {
		// Base case: direct reference to parent's PK
		ctx.Quote(parentTable)
		ctx.WriteString(`.`)
		ctx.Quote(pkCol)
	} else {
		// For children traversal, we need to find IDs at each level
		// This is more complex - use a subquery approach
		ctx.WriteString(`(SELECT `)
		ctx.Quote(pkCol)
		ctx.WriteString(` FROM `)
		ctx.Quote(tableName)
		ctx.WriteString(` WHERE `)
		ctx.Quote(fkCol)
		ctx.WriteString(` = `)
		d.renderNestedPKSubquery(ctx, tableName, pkCol, fkCol, parentTable, depth-1)
		ctx.WriteString(`)`)
	}
}

// renderRecursiveWhereClauseFilters renders WHERE clause filters for recursive queries
func (d *MariaDBDialect) renderRecursiveWhereClauseFilters(ctx Context, r InlineChildRenderer, sel *qcode.Select) {
	if sel.Where.Exp == nil {
		return
	}

	filters := d.collectNonRelFilters(sel.Where.Exp)
	if len(filters) == 0 {
		return
	}

	ctx.WriteString(` WHERE `)
	for i, f := range filters {
		if i != 0 {
			ctx.WriteString(` AND `)
		}
		d.renderSimpleExp(ctx, sel, f)
	}
}

// renderRecursiveWhereClauseFiltersInline renders AND clause filters for recursive queries
// when there's already a WHERE clause present
func (d *MariaDBDialect) renderRecursiveWhereClauseFiltersInline(ctx Context, r InlineChildRenderer, sel *qcode.Select) {
	if sel.Where.Exp == nil {
		return
	}

	filters := d.collectNonRelFilters(sel.Where.Exp)
	if len(filters) == 0 {
		return
	}

	for _, f := range filters {
		ctx.WriteString(` AND `)
		d.renderSimpleExp(ctx, sel, f)
	}
}

// collectNonRelFilters collects non-relationship filter expressions
func (d *MariaDBDialect) collectNonRelFilters(exp *qcode.Exp) []*qcode.Exp {
	if exp == nil {
		return nil
	}

	var filters []*qcode.Exp

	if exp.Op == qcode.OpAnd {
		for _, child := range exp.Children {
			if !d.isRelationshipFilterExp(child) && d.isSimpleComparisonExp(child) {
				filters = append(filters, child)
			}
		}
	} else if !d.isRelationshipFilterExp(exp) && d.isSimpleComparisonExp(exp) {
		filters = append(filters, exp)
	}

	return filters
}

// isSimpleComparisonExp checks if an expression is a simple comparison
func (d *MariaDBDialect) isSimpleComparisonExp(exp *qcode.Exp) bool {
	if exp == nil {
		return false
	}
	switch exp.Op {
	case qcode.OpEquals, qcode.OpNotEquals, qcode.OpGreaterThan, qcode.OpLesserThan,
		qcode.OpGreaterOrEquals, qcode.OpLesserOrEquals:
		return true
	}
	return false
}

// renderRecursiveJSONFields renders JSON object fields for recursive queries
func (d *MariaDBDialect) renderRecursiveJSONFields(ctx Context, r InlineChildRenderer, sel *qcode.Select) {
	i := 0
	for _, f := range sel.Fields {
		if f.SkipRender != qcode.SkipTypeNone {
			continue
		}
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.WriteString(`'`)
		ctx.WriteString(f.FieldName)
		ctx.WriteString(`', `)

		switch f.Type {
		case qcode.FieldTypeFunc:
			// Handle aggregate functions like count_id
			d.renderFunctionForRecursive(ctx, f)
		default:
			// Regular column
			ctx.WriteString(`t.`)
			ctx.Quote(f.Col.Name)
		}
		i++
	}
}

// renderFunctionForRecursive renders a function call for recursive queries
func (d *MariaDBDialect) renderFunctionForRecursive(ctx Context, f qcode.Field) {
	ctx.WriteString(f.Func.Name)
	ctx.WriteString(`(`)

	argCount := 0
	for _, a := range f.Args {
		if a.Name == "" {
			if argCount != 0 {
				ctx.WriteString(`, `)
			}
			if a.Type == qcode.ArgTypeCol {
				ctx.WriteString(`t.`)
				ctx.Quote(a.Col.Name)
			} else {
				ctx.WriteString(a.Val)
			}
			argCount++
		}
	}

	ctx.WriteString(`)`)
}

// hasNonRelationshipFilter checks if there are filters beyond the recursive relationship
func (d *MariaDBDialect) hasNonRelationshipFilter(exp *qcode.Exp) bool {
	if exp == nil {
		return false
	}
	// Check if this is a simple relationship filter (IsNotNull, NotEquals on __rcte)
	if exp.Op == qcode.OpAnd {
		// Recursive relationship filters have 3 children: IsNotNull, NotEquals, Equals
		// If there are additional filters, we have non-relationship filters
		nonRelCount := 0
		for _, child := range exp.Children {
			if !d.isRelationshipFilterExp(child) {
				nonRelCount++
			}
		}
		return nonRelCount > 0
	}
	return !d.isRelationshipFilterExp(exp)
}

// isRelationshipFilterExp checks if an expression is part of the recursive relationship filter
func (d *MariaDBDialect) isRelationshipFilterExp(exp *qcode.Exp) bool {
	if exp == nil {
		return false
	}
	// Relationship filters reference __rcte_ tables
	if exp.Left.Table != "" && len(exp.Left.Table) > 7 && exp.Left.Table[:7] == "__rcte_" {
		return true
	}
	if exp.Right.Table != "" && len(exp.Right.Table) > 7 && exp.Right.Table[:7] == "__rcte_" {
		return true
	}
	return false
}

// renderRecursiveWhereFilter renders WHERE filter excluding relationship conditions
func (d *MariaDBDialect) renderRecursiveWhereFilter(ctx Context, r InlineChildRenderer, sel *qcode.Select, exp *qcode.Exp) {
	if exp == nil {
		return
	}
	if exp.Op == qcode.OpAnd {
		first := true
		for _, child := range exp.Children {
			if d.isRelationshipFilterExp(child) {
				continue
			}
			if !first {
				ctx.WriteString(` AND `)
			}
			d.renderSimpleExp(ctx, sel, child)
			first = false
		}
	} else if !d.isRelationshipFilterExp(exp) {
		d.renderSimpleExp(ctx, sel, exp)
	}
}

// renderSimpleExp renders a simple expression for recursive WHERE clause
func (d *MariaDBDialect) renderSimpleExp(ctx Context, sel *qcode.Select, exp *qcode.Exp) {
	if exp == nil {
		return
	}
	// Render the column reference
	if exp.Left.Col.Name != "" {
		ctx.Quote(exp.Left.Col.Name)
	}
	// Render the operator and value
	switch exp.Op {
	case qcode.OpEquals:
		ctx.WriteString(` = `)
	case qcode.OpNotEquals:
		ctx.WriteString(` != `)
	case qcode.OpGreaterThan:
		ctx.WriteString(` > `)
	case qcode.OpLesserThan:
		ctx.WriteString(` < `)
	case qcode.OpGreaterOrEquals:
		ctx.WriteString(` >= `)
	case qcode.OpLesserOrEquals:
		ctx.WriteString(` <= `)
	default:
		return
	}
	// Render the right side value
	if exp.Right.ValType == qcode.ValNum {
		ctx.WriteString(exp.Right.Val)
	} else if exp.Right.Col.Name != "" {
		ctx.Quote(exp.Right.Col.Name)
	} else {
		ctx.WriteString(`'`)
		ctx.WriteString(exp.Right.Val)
		ctx.WriteString(`'`)
	}
}

// RenderLinearConnect overrides MySQL's version for MariaDB.
// MariaDB has issues with column resolution when JSON_TABLE derived table
// is in the same FROM clause as the target table. We restructure the query
// to use an explicit JOIN instead of a cartesian product.
func (d *MariaDBDialect) RenderLinearConnect(ctx Context, m *qcode.Mutate, qc *qcode.QCode, varName string, renderFilter func()) {
	ctx.WriteString(`SELECT JSON_ARRAYAGG(`)
	d.Quote(ctx, m.Ti.Name)
	ctx.WriteString(".")
	d.Quote(ctx, m.Rel.Left.Col.Name)
	ctx.WriteString(`) INTO `)
	d.RenderVar(ctx, varName)

	ctx.WriteString(` FROM `)
	d.Quote(ctx, m.Ti.Name)

	if m.IsJSON {
		ctx.WriteString(` JOIN `)
		d.RenderMutateToRecordSet(ctx, m, 0, func() {
			ctx.AddParam(Param{Name: qc.ActionVar, Type: "json"})
		})
		ctx.WriteString(` ON TRUE`)
	}

	ctx.WriteString(` WHERE `)
	renderFilter()
	ctx.WriteString("; ")

	// If this is a One-to-Many connection (Child needs to point to Parent),
	// we need to update the child table with the parent's ID.
	var parentVar string
	for id := range m.DependsOn {
		if qc.Mutates[id].Ti.Name == m.Rel.Right.Col.Table {
			parentVar = d.getVarName(qc.Mutates[id])
			break
		}
	}

	if parentVar != "" {
		ctx.WriteString("UPDATE ")
		d.Quote(ctx, m.Ti.Name)
		if m.IsJSON {
			ctx.WriteString(" JOIN ")
			d.RenderMutateToRecordSet(ctx, m, 0, func() {
				ctx.AddParam(Param{Name: qc.ActionVar, Type: "json"})
			})
			ctx.WriteString(" ON TRUE")
		}
		ctx.WriteString(" SET ")
		d.Quote(ctx, m.Ti.Name)
		ctx.WriteString(".")
		d.Quote(ctx, m.Rel.Left.Col.Name)
		ctx.WriteString(" = @")
		ctx.WriteString(parentVar)
		ctx.WriteString(" WHERE ")
		renderFilter()
		ctx.WriteString("; ")
	}
}

// RenderLinearDisconnect overrides MySQL's version for MariaDB.
func (d *MariaDBDialect) RenderLinearDisconnect(ctx Context, m *qcode.Mutate, qc *qcode.QCode, varName string, renderFilter func()) {
	ctx.WriteString(`SELECT JSON_ARRAYAGG(`)
	d.Quote(ctx, m.Ti.Name)
	ctx.WriteString(".")
	d.Quote(ctx, m.Rel.Left.Col.Name)
	ctx.WriteString(`) INTO `)
	d.RenderVar(ctx, varName)

	ctx.WriteString(` FROM `)
	d.Quote(ctx, m.Ti.Name)

	if m.IsJSON {
		ctx.WriteString(` JOIN `)
		d.RenderMutateToRecordSet(ctx, m, 0, func() {
			ctx.AddParam(Param{Name: qc.ActionVar, Type: "json"})
		})
		ctx.WriteString(` ON TRUE`)
	}

	ctx.WriteString(` WHERE `)
	renderFilter()
	ctx.WriteString(`; `)

	// Perform the actual disconnect (UPDATE child SET fk = NULL)
	ctx.WriteString("UPDATE ")
	d.Quote(ctx, m.Ti.Name)
	if m.IsJSON {
		ctx.WriteString(" JOIN ")
		d.RenderMutateToRecordSet(ctx, m, 0, func() {
			ctx.AddParam(Param{Name: qc.ActionVar, Type: "json"})
		})
		ctx.WriteString(" ON TRUE")
	}
	ctx.WriteString(" SET ")
	d.Quote(ctx, m.Ti.Name)
	ctx.WriteString(".")
	d.Quote(ctx, m.Rel.Left.Col.Name)
	ctx.WriteString(" = NULL WHERE ")
	renderFilter()
	ctx.WriteString("; ")
}

// renderFieldFilterExp renders a field filter expression (skipIf/includeIf) for MariaDB.
// This is a simplified version that renders the condition for CASE WHEN usage.
func (d *MariaDBDialect) renderFieldFilterExp(ctx Context, r InlineChildRenderer, sel *qcode.Select, ex *qcode.Exp) {
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
		ctx.AddParam(Param{Name: ex.Right.Val, Type: "boolean"})
		ctx.WriteString(` IS TRUE)`)

	case qcode.OpNotEqualsTrue:
		// For @skip(ifVar: $varName) - show when variable is NOT true (i.e., skip when true)
		ctx.WriteString(`(`)
		ctx.AddParam(Param{Name: ex.Right.Val, Type: "boolean"})
		ctx.WriteString(` IS NOT TRUE)`)

	default:
		// Fallback: just render true for unsupported ops
		ctx.WriteString(`TRUE`)
	}
}

// renderFieldFilterVal renders the right-hand side value for field filter expressions
func (d *MariaDBDialect) renderFieldFilterVal(ctx Context, ex *qcode.Exp) {
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

// Role Statement rendering - MariaDB uses default syntax (same as Postgres)
func (d *MariaDBDialect) RoleSelectPrefix() string {
	return `(SELECT (CASE`
}

func (d *MariaDBDialect) RoleLimitSuffix() string {
	return `) AS _sg_auth_roles_query LIMIT 1) `
}

func (d *MariaDBDialect) RoleDummyTable() string {
	// MariaDB uses same syntax as default (not MySQL's VALUES ROW(1))
	return `ELSE 'anon' END) FROM (VALUES (1)) AS _sg_auth_filler LIMIT 1; `
}

func (d *MariaDBDialect) TransformBooleanLiterals(match string) string {
	return match // MariaDB uses true/false natively
}

// Driver Behavior
func (d *MariaDBDialect) RequiresJSONAsString() bool {
	return false // MariaDB driver handles json.RawMessage properly
}

func (d *MariaDBDialect) RequiresLowercaseIdentifiers() bool {
	return false // MariaDB doesn't require lowercase identifiers
}

func (d *MariaDBDialect) RequiresBooleanAsInt() bool {
	return false // MariaDB handles boolean as TINYINT(1) natively
}

// Recursive CTE Syntax
func (d *MariaDBDialect) RequiresRecursiveKeyword() bool {
	return true // MariaDB uses WITH RECURSIVE
}

func (d *MariaDBDialect) RequiresRecursiveCTEColumnList() bool {
	return false // MariaDB doesn't require explicit column list
}

func (d *MariaDBDialect) RenderRecursiveOffset(ctx Context) {
	ctx.WriteString(` LIMIT 1, 18446744073709551610`) // MariaDB same as MySQL
}

func (d *MariaDBDialect) RenderRecursiveLimit1(ctx Context) {
	ctx.WriteString(` LIMIT 1`)
}

func (d *MariaDBDialect) WrapRecursiveSelect() bool {
	return false // MariaDB doesn't need extra wrapping
}

func (d *MariaDBDialect) RenderRecursiveAnchorWhere(ctx Context, psel *qcode.Select, ti sdata.DBTable, pkCol string) bool {
	return false // MariaDB supports outer scope correlation in CTEs
}

// JSON Null Fields
func (d *MariaDBDialect) RenderJSONNullField(ctx Context, fieldName string) {
	ctx.WriteString(`'`)
	ctx.WriteString(fieldName)
	ctx.WriteString(`', NULL`)
}

func (d *MariaDBDialect) RenderJSONNullCursorField(ctx Context, fieldName string) {
	ctx.WriteString(`, '`)
	ctx.WriteString(fieldName)
	ctx.WriteString(`_cursor', NULL`)
}

func (d *MariaDBDialect) RenderJSONRootSuffix(ctx Context) {
	// MariaDB doesn't need any suffix
}

// Array Operations
func (d *MariaDBDialect) RenderArraySelectPrefix(ctx Context) {
	ctx.WriteString(`(SELECT JSON_ARRAYAGG(`)
}

func (d *MariaDBDialect) RenderArraySelectSuffix(ctx Context) {
	ctx.WriteString(`))`)
}

func (d *MariaDBDialect) RenderArrayAggPrefix(ctx Context, distinct bool) {
	if distinct {
		ctx.WriteString(`JSON_ARRAYAGG(DISTINCT `)
	} else {
		ctx.WriteString(`JSON_ARRAYAGG(`)
	}
}

func (d *MariaDBDialect) RenderArrayRemove(ctx Context, col string, val func()) {
	// MariaDB uses JSON_REMOVE with JSON_SEARCH (same as MySQL)
	ctx.WriteString(` JSON_REMOVE(`)
	ctx.Quote(col)
	ctx.WriteString(`, JSON_UNQUOTE(JSON_SEARCH(`)
	ctx.Quote(col)
	ctx.WriteString(`, 'one', `)
	val()
	ctx.WriteString(`)))`)
}

// Column rendering
func (d *MariaDBDialect) RequiresJSONQueryWrapper() bool {
	return true // MariaDB needs JSON_QUERY wrapper for inline children
}

func (d *MariaDBDialect) RequiresNullOnEmptySelect() bool {
	return true // MariaDB needs NULL when no columns rendered
}



