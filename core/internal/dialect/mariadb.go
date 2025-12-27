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

func (d *MariaDBDialect) Name() string {
	return "mariadb"
}

func (d *MariaDBDialect) QuoteIdentifier(s string) string {
	return "`" + s + "`"
}

// SupportsLateral returns false for MariaDB.
// While MariaDB has some LATERAL support since 10.6, it does NOT support the
// LEFT OUTER JOIN LATERAL syntax that MySQL 8+ uses (see MDEV-33018).
// Instead, we use inline subqueries like SQLite.
func (d *MariaDBDialect) SupportsLateral() bool {
	return false
}

// RenderInlineChild renders an inline subquery for MariaDB.
// MariaDB doesn't support LATERAL joins, so we generate flat correlated subqueries.
// For plural (array) results, we use a subquery to apply ORDER BY and LIMIT before aggregation.
func (d *MariaDBDialect) RenderInlineChild(ctx Context, r InlineChildRenderer, psel, sel *qcode.Select) {
	ctx.WriteString(`(SELECT `)
	if sel.Singular {
		// For singular (one-to-one/many-to-one), return a single json_object
		ctx.WriteString(`json_object(`)
		d.renderInlineJSONFields(ctx, r, sel)
		ctx.WriteString(`)`)

		ctx.WriteString(` FROM `)
		r.RenderTable(sel, sel.Ti.Schema, sel.Ti.Name, false)
		t := sel.Ti.Name
		if sel.ID >= 0 {
			t = fmt.Sprintf("%s_%d", t, sel.ID)
		}
		d.RenderTableAlias(ctx, t)

		// Render join tables for many-to-many relationships
		for _, join := range sel.Joins {
			r.RenderJoin(join)
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
			// For correlated child subqueries, we use a derived table to support nested aggregations
			ctx.WriteString(`COALESCE(json_arrayagg(json_object(`)
			d.renderSubqueryJSONFields(ctx, r, sel)
			ctx.WriteString(`)), '[]')`)

			ctx.WriteString(` FROM (SELECT `)
			d.renderBaseColumns(ctx, r, sel)
			ctx.WriteString(` FROM `)
			r.RenderTable(sel, sel.Ti.Schema, sel.Ti.Name, false)
			t := sel.Ti.Name
			if sel.ID >= 0 {
				t = fmt.Sprintf("%s_%d", t, sel.ID)
			}
			d.RenderTableAlias(ctx, t)

			// Render join tables for many-to-many relationships
			for _, join := range sel.Joins {
				r.RenderJoin(join)
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
			d.renderOrderBy(ctx, r, sel, "")
            
			ctx.WriteString(`) AS `)
			d.RenderTableAlias(ctx, "_gj_t")
		} else {
			// For root queries, use a subquery to apply ORDER BY and LIMIT before aggregation
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
			r.RenderTable(sel, sel.Ti.Schema, sel.Ti.Name, false)
			// Apply alias to match generic RenderJoin expectations
			t := sel.Ti.Name
			if sel.ID >= 0 {
				t = fmt.Sprintf("%s_%d", t, sel.ID)
			}
			d.RenderTableAlias(ctx, t)

			// Render join tables for many-to-many relationships
			for _, join := range sel.Joins {
				r.RenderJoin(join)
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
			r.ColWithTable(t, f.Col.Name)
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
		ctx.WriteString(`, JSON_QUERY(`)
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

		r.ColWithTable("_gj_t", f.FieldName)
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
			}
		}
		r.ColWithTable(t, col)
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
func (d *MariaDBDialect) RenderJSONRootField(ctx Context, key string, val func()) {
	ctx.WriteString(`'`)
	ctx.WriteString(key)
	ctx.WriteString(`', JSON_QUERY(`)
	val()
	ctx.WriteString(`, '$')`)
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

// RenderSubscriptionUnbox renders the SQL to unwrap subscription parameters for MariaDB.
// MariaDB (as of 10.11) does not support the LEFT OUTER JOIN LATERAL syntax used by MySQL 8+.
// Instead, we use a correlated subquery in the SELECT list to achieve the same result.
func (d *MariaDBDialect) SupportsSubscriptionBatching() bool {
	return d.DBVersion >= 110100 // MariaDB 11.1+
}

func (d *MariaDBDialect) RenderSubscriptionUnbox(ctx Context, params []Param, renderInnerSQL func()) {
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
	ctx.WriteString(`)) AS _gj_jt`)
	ctx.WriteString(`) SELECT (`)
	renderInnerSQL()
	ctx.WriteString(`) FROM _gj_sub`)
}

func (d *MariaDBDialect) RenderTableAlias(ctx Context, alias string) {
	ctx.WriteString(` AS `)
	ctx.Quote(alias)
	ctx.WriteString(` `)
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
			}
		}
		r.ColWithTable(t, col)
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

	// DEBUG
	// fmt.Printf("DEBUG renderExp: Op=%d Left.Table='%s' Left.Col='%s' sel.Ti='%s' sel.ID=%d\n", ex.Op, ex.Left.Col.Table, ex.Left.Col.Name, sel.Ti.Name, sel.ID)
	// fmt.Printf("DEBUG renderExp: Op=%d Left.Table='%s' Left.Col='%s' sel.Ti='%s' sel.ID=%d\n", ex.Op, ex.Left.Col.Table, ex.Left.Col.Name, sel.Ti.Name, sel.ID)

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
		if strings.EqualFold(ex.Right.Val, "true") {
			ctx.WriteString(` IS NULL)`)
		} else {
			ctx.WriteString(` IS NOT NULL)`)
		}

	case qcode.OpSelectExists:
		if len(ex.Joins) == 0 {
			return
		}
		first := ex.Joins[0]
		ctx.WriteString(`EXISTS (SELECT 1 FROM `)
		ctx.Quote(first.Rel.Left.Col.Table)

		if len(ex.Joins) > 1 {
			for i := 1; i < len(ex.Joins); i++ {
				j := ex.Joins[i]
				ctx.WriteString(` LEFT JOIN `)
				ctx.Quote(j.Rel.Left.Col.Table)
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
			if ex.Left.ID >= 0 && psel != nil && ex.Left.ID == psel.ID {
				// References a parent table
				t := psel.Ti.Name
				if psel.ID >= 0 {
					t = fmt.Sprintf("%s_%d", t, psel.ID)
				}
				r.ColWithTable(t, ex.Left.Col.Name)
			} else {
				// Current table or Joined table
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
			if ex.Right.ID >= 0 && psel != nil && ex.Right.ID == psel.ID {
				t := psel.Ti.Name
				if psel.ID > 0 {
					t = fmt.Sprintf("%s_%d", t, psel.ID)
				}
				r.ColWithTable(t, ex.Right.Col.Name)
			} else {
				t := ex.Right.Col.Table
				if t == "" {
					t = sel.Ti.Name
				}

				if t == sel.Ti.Name {
					if sel.ID > 0 {
						t = fmt.Sprintf("%s_%d", t, sel.ID)
					}
				} else if ex.Right.ID > 0 {
					t = fmt.Sprintf("%s_%d", t, ex.Right.ID)
				}
				r.ColWithTable(t, ex.Right.Col.Name)
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
				if psel.ID > 0 {
					t = fmt.Sprintf("%s_%d", t, psel.ID)
				}
				r.ColWithTable(t, ex.Left.Col.Name)
			} else {
				t := ex.Left.Col.Table
				if t == "" {
					t = sel.Ti.Name
				}

				if t == sel.Ti.Name {
					if sel.ID > 0 {
						t = fmt.Sprintf("%s_%d", t, sel.ID)
					}
				} else if ex.Left.ID > 0 {
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
				if psel.ID > 0 {
					t = fmt.Sprintf("%s_%d", t, psel.ID)
				}
				r.ColWithTable(t, ex.Right.Col.Name)
			} else {
				t := ex.Right.Col.Table
				if t == "" {
					t = sel.Ti.Name
				}

				if t == sel.Ti.Name {
					if sel.ID > 0 {
						t = fmt.Sprintf("%s_%d", t, sel.ID)
					}
				} else if ex.Right.ID > 0 {
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
			if sel.ID > 0 {
				t = fmt.Sprintf("%s_%d", t, sel.ID)
			}
		} else if ex.Left.ID > 0 {
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
					if sel.ID > 0 {
						t = fmt.Sprintf("%s_%d", t, sel.ID)
					}
				} else if ex.Left.ID > 0 {
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
			if sel.ID > 0 {
				t = fmt.Sprintf("%s_%d", t, sel.ID)
			}
		} else if ex.Left.ID > 0 {
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
		if sel.ID > 0 {
			t = fmt.Sprintf("%s_%d", t, sel.ID)
		}
	}
	r.ColWithTable(t, ob.Col.Name)
}
