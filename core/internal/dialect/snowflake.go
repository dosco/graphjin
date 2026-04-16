package dialect

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

// SnowflakeDialect is implementation-ready but intentionally not wired into
// compiler/service selection yet (single-file delivery constraint).
//
// TODO(follow-up outside this file):
// 1) Add case "snowflake" in psql.NewCompiler (core/internal/psql/query.go)
// 2) Add case "snowflake" in core/subs.go getDialectForType
// 3) Add snowflake to DB type validation lists
// 4) Add Snowflake schema discovery SQL + service driver wiring
//
// TODO(parity):
// - GIS operators are intentionally unsupported in this phase.
// - Postgres-specific array/json key ops are intentionally unsupported.
// - RelEmbedded/RenderFromEdge parity may need a Snowflake-specific approach.
type SnowflakeDialect struct {
	PostgresDialect
}

var _ Dialect = (*SnowflakeDialect)(nil)

func (d *SnowflakeDialect) Name() string {
	return "snowflake"
}

func (d *SnowflakeDialect) QuoteIdentifier(s string) string {
	// Snowflake escapes embedded `"` in a quoted identifier by doubling it.
	// Unescaped `"` inside the identifier would terminate the identifier
	// early — rare in practice but a correctness requirement.
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func (d *SnowflakeDialect) BindVar(i int) string {
	return "?"
}

func (d *SnowflakeDialect) UseNamedParams() bool {
	return false
}

func (d *SnowflakeDialect) RenderJSONRoot(ctx Context, sel *qcode.Select) {
	// Real Snowflake: OBJECT_CONSTRUCT (DuckDB equivalent was json_object)
	ctx.WriteString(`SELECT CAST(OBJECT_CONSTRUCT_KEEP_NULL(`)
}

func (d *SnowflakeDialect) RenderJSONSelect(ctx Context, sel *qcode.Select) {
	// For inline child selections that resolve to a singular value
	// (e.g. `{ purchases { product { name } } }` — one product per
	// purchase), the compiler wraps this SELECT in a correlated scalar
	// subquery. Snowflake does NOT support correlated scalar subqueries
	// that return a derived-table row — it raises
	//   "Unsupported subquery type cannot be evaluated".
	// Snowflake DOES accept correlated subqueries whose outermost
	// projection is aggregated. Wrap OBJECT_CONSTRUCT with ANY_VALUE()
	// so the subquery is classified as an aggregate query; with the
	// inner LIMIT 1 there's always exactly one input row, so
	// ANY_VALUE returns it unchanged. This is the idiomatic
	// Snowflake pattern for per-row JSON construction in correlated
	// subqueries.
	//
	// Plural children use ARRAY_AGG at the outer level (emitted by
	// RenderJSONPlural) so their inner OBJECT_CONSTRUCT stays raw.
	// Root selections (Rel.Type == RelNone) are not correlated and
	// don't need wrapping.
	if sel.Singular && sel.Rel.Type != 0 { // RelNone == 0
		ctx.WriteString(`SELECT ANY_VALUE(OBJECT_CONSTRUCT_KEEP_NULL(`)
		ctx.RenderJSONFields(sel)
		ctx.WriteString(`))`)
		return
	}
	ctx.WriteString(`SELECT OBJECT_CONSTRUCT_KEEP_NULL(`)
	ctx.RenderJSONFields(sel)
	ctx.WriteString(`)`)
}

func (d *SnowflakeDialect) RenderJSONPlural(ctx Context, sel *qcode.Select) {
	// Real Snowflake: ARRAY_AGG / ARRAY_CONSTRUCT (DuckDB equivalents were array_agg / list_value).
	// Identifiers MUST be double-quoted: inner aliases emitted by the compiler
	// are quoted-lowercase ("__sj_0", "json"), and Snowflake's default folding
	// would otherwise uppercase the unquoted reference and miss the target.
	// Child plurals wrap with ARRAY_SLICE to apply the LIMIT here rather
	// than on the inner correlated base-SELECT — Snowflake rejects
	// LIMIT/ORDER BY/GROUP BY inside a correlated scalar subquery.
	// RenderLimit elides the inner LIMIT for child selects and we reapply
	// it post-aggregation via ARRAY_SLICE. Root plurals (Rel.Type ==
	// RelNone) aren't correlated so keep the plain ARRAY_AGG form.
	useSlice := sel.Rel.Type != sdata.RelNone && !sel.Paging.NoLimit
	if useSlice {
		ctx.WriteString(`COALESCE(ARRAY_SLICE(ARRAY_AGG("__sjb_`)
		ctx.WriteString(strconv.Itoa(int(sel.ID)))
		ctx.WriteString(`"."json"), `)
		if sel.Paging.Offset > 0 {
			ctx.WriteString(fmt.Sprintf("%d, %d", sel.Paging.Offset, sel.Paging.Offset+sel.Paging.Limit))
		} else {
			ctx.WriteString(fmt.Sprintf("0, %d", sel.Paging.Limit))
		}
		ctx.WriteString(`), ARRAY_CONSTRUCT())`)
		return
	}
	ctx.WriteString(`COALESCE(ARRAY_AGG("__sjb_`)
	ctx.WriteString(strconv.Itoa(int(sel.ID)))
	ctx.WriteString(`"."json"), ARRAY_CONSTRUCT())`)
}

func (d *SnowflakeDialect) RenderJSONField(ctx Context, fieldName string, tableAlias string, colName string, isNull bool, isJSON bool) {
	ctx.WriteString(`'`)
	ctx.WriteString(fieldName)
	ctx.WriteString(`', `)
	if isNull {
		ctx.WriteString(`NULL`)
		return
	}

	if tableAlias != "" {
		ctx.Quote(tableAlias)
		ctx.WriteString(`.`)
		ctx.Quote(colName)
		return
	}

	ctx.Quote(colName)
}

func (d *SnowflakeDialect) RenderJSONRootField(ctx Context, key string, val func()) {
	ctx.WriteString(`'`)
	ctx.WriteString(key)
	ctx.WriteString(`', `)
	val()
}

func (d *SnowflakeDialect) RenderJSONNullField(ctx Context, fieldName string) {
	ctx.WriteString(`'`)
	ctx.WriteString(fieldName)
	ctx.WriteString(`', NULL`)
}

func (d *SnowflakeDialect) RenderJSONNullCursorField(ctx Context, fieldName string) {
	ctx.WriteString(`, '`)
	ctx.WriteString(fieldName)
	ctx.WriteString(`_cursor', NULL`)
}

func (d *SnowflakeDialect) RenderJSONRootSuffix(ctx Context) {
	ctx.WriteString(`) AS VARCHAR`)
}

func (d *SnowflakeDialect) SupportsLateral() bool {
	// Re-tried SupportsLateral=true after the __sjb alias split; Snowflake
	// still aborts with internal error 300010 on nested LATERAL JSON
	// constructions. Keep false; the correlated-subquery form is the
	// reliable shape.
	return false
}

// RenderLimit moves LIMIT enforcement off the inner base-SELECT for any
// CHILD select (singular or plural). Snowflake rejects correlated scalar
// subqueries that contain a derived table with LIMIT/ORDER BY/GROUP BY
// ("Unsupported subquery type cannot be evaluated"). For child selects we
// drop the inner LIMIT here; the limit is still enforced — it's reapplied
// at the ARRAY_AGG layer via ARRAY_SLICE in RenderJSONPlural for plurals,
// and child singulars are FK-unique so at most one row matches. Root
// selects (Rel.Type == RelNone) are not correlated and keep their LIMIT
// intact.
func (d *SnowflakeDialect) RenderLimit(ctx Context, sel *qcode.Select) {
	if sel.Rel.Type != sdata.RelNone {
		if sel.Paging.OffsetVar != "" {
			ctx.WriteString(` OFFSET `)
			ctx.AddParam(Param{Name: sel.Paging.OffsetVar, Type: "integer"})
		} else if sel.Paging.Offset != 0 {
			ctx.WriteString(fmt.Sprintf(` OFFSET %d`, sel.Paging.Offset))
		}
		return
	}
	d.PostgresDialect.RenderLimit(ctx, sel)
}

func (d *SnowflakeDialect) RenderInlineChild(ctx Context, renderer InlineChildRenderer, psel, sel *qcode.Select) {
	renderer.RenderDefaultInlineChild(sel)
}

func (d *SnowflakeDialect) RenderChildCursor(ctx Context, renderChild func()) {
	// Real Snowflake: GET_PATH (DuckDB equivalent was json_extract with '$.path' syntax)
	ctx.WriteString(`GET_PATH(`)
	renderChild()
	ctx.WriteString(`, 'cursor')`)
}

func (d *SnowflakeDialect) RenderChildValue(ctx Context, sel *qcode.Select, renderChild func()) {
	if sel.Paging.Cursor {
		// Real Snowflake: GET_PATH (DuckDB equivalent was json_extract with '$.path' syntax)
		ctx.WriteString(`GET_PATH(`)
		renderChild()
		ctx.WriteString(`, 'json')`)
		return
	}
	renderChild()
}

func (d *SnowflakeDialect) RenderCursorCTE(ctx Context, sel *qcode.Select) {
	if !sel.Paging.Cursor {
		return
	}

	cursorVar := sel.Paging.CursorVar
	if cursorVar == "" {
		cursorVar = "cursor"
	}

	// Quote the CTE name — Snowflake folds unquoted `__cur` to `__CUR`
	// but the compiled references are always `"__cur"` quoted-lowercase
	// (via colWithTable). Without the double-quotes here the folded
	// uppercase `__CUR` doesn't match the reference and the query fails
	// with `Object '"__cur"' does not exist`.
	ctx.WriteString(`WITH "__cur" AS (SELECT `)
	for i, ob := range sel.OrderBy {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.WriteString(`TRY_CAST(SPLIT_PART(`)
		ctx.AddParam(Param{Name: cursorVar, Type: "text"})
		ctx.WriteString(`, ',', `)
		ctx.WriteString(strconv.Itoa(i + 2))
		ctx.WriteString(`) AS `)
		ctx.WriteString(d.snowflakeCastType(ob.Col.Type))
		ctx.WriteString(`) AS `)
		if ob.KeyVar != "" && ob.Key != "" {
			ctx.Quote(ob.Col.Name + "_" + ob.Key)
		} else {
			ctx.Quote(ob.Col.Name)
		}
	}
	ctx.WriteString(`) `)
}

func (d *SnowflakeDialect) RenderJoinTables(ctx Context, sel *qcode.Select) {
	for _, ob := range sel.OrderBy {
		if ob.Var == "" {
			continue
		}
		// Real Snowflake: LATERAL FLATTEN(input => PARSE_JSON(...)) exposes columns
		// `value` and `index` per row. (DuckDB equivalent was json_each(CAST(... AS JSON))
		// returning `value` and `key`.)
		// `value::type` rather than TRY_CAST(value AS type) — the
		// FLATTEN output's `value` column is VARIANT and Snowflake
		// rejects TRY_CAST from VARIANT to scalar types.
		ctx.WriteString(` JOIN (SELECT _gj_f.value::`)
		ctx.WriteString(d.snowflakeCastType(ob.Col.Type))
		ctx.WriteString(` AS id, _gj_f.index + 1 AS ord FROM LATERAL FLATTEN(input => PARSE_JSON(`)
		ctx.AddParam(Param{Name: ob.Var, Type: "json"})
		ctx.WriteString(`)) _gj_f) AS _gj_ob_`)
		ctx.WriteString(ob.Col.Name)
		ctx.WriteString(`(id, ord) USING (id)`)
	}
}

// RenderDistinctOn is intentionally a noop on Snowflake — Snowflake does not
// support Postgres's `SELECT DISTINCT ON (...)` syntax. The equivalent
// semantics are emitted via QUALIFY ROW_NUMBER() OVER (...) inside
// RenderOrderBy below, which runs at the end of the SELECT body just
// before LIMIT.
func (d *SnowflakeDialect) RenderDistinctOn(ctx Context, sel *qcode.Select) {}

func (d *SnowflakeDialect) RenderOrderBy(ctx Context, sel *qcode.Select) {
	d.renderOrderByCore(ctx, sel)
	d.renderQualifyDistinctOn(ctx, sel)
}

// renderQualifyDistinctOn emits a QUALIFY clause that reproduces
// `SELECT DISTINCT ON (...)` semantics using ROW_NUMBER() OVER. Only fires
// when the user asked for distinct_on AND the query isn't already grouped
// (GROUP BY + distinct_on would conflict).
func (d *SnowflakeDialect) renderQualifyDistinctOn(ctx Context, sel *qcode.Select) {
	if sel.GroupCols || len(sel.DistinctOn) == 0 {
		return
	}
	ctx.WriteString(` QUALIFY ROW_NUMBER() OVER (PARTITION BY `)
	for i, col := range sel.DistinctOn {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.ColWithTable(col.Table, col.Name)
	}
	// If the user supplied an ORDER BY, Snowflake requires the window's ORDER BY
	// to be deterministic — reuse the distinct_on cols as the window order.
	// This matches Postgres DISTINCT ON semantics (first row per partition).
	ctx.WriteString(` ORDER BY `)
	for i, col := range sel.DistinctOn {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.ColWithTable(col.Table, col.Name)
	}
	ctx.WriteString(`) = 1`)
}

func (d *SnowflakeDialect) renderOrderByCore(ctx Context, sel *qcode.Select) {
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
			ctx.WriteString(fmt.Sprintf("'%s'", strings.ReplaceAll(ob.Key, "'", "''")))
			ctx.WriteString(` THEN `)
		}
		if ob.Var != "" {
			ctx.ColWithTable(`_gj_ob_`+ob.Col.Name, "ord")
		} else if ob.Alias != "" {
			ctx.Quote(ob.Alias)
		} else {
			if ob.IsFunc {
				ctx.WriteString(strings.ToUpper(ob.Func.Name))
				ctx.WriteString(`(`)
			}
			ctx.ColWithTable(ob.Col.Table, ob.Col.Name)
			if ob.IsFunc {
				ctx.WriteString(`)`)
			}
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

func (d *SnowflakeDialect) RenderFromEdge(ctx Context, sel *qcode.Select) {
	// Snowflake JSON-virtual-table: expose the VARIANT column's elements as
	// rows via TABLE(FLATTEN()). Wrap in a LATERAL inline view so the
	// FLATTEN's `input => users.category_counts` can reference the parent
	// table from the outer FROM clause. Bare `(SELECT ... FROM LATERAL
	// FLATTEN(input => users.x))` fails with "invalid identifier 'USERS.X'"
	// because the inner SELECT is a separate scope — LATERAL is required to
	// thread the parent row in.
	ctx.WriteString(`LATERAL (SELECT `)
	for i, col := range sel.Ti.Columns {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.WriteString(`CAST(GET_PATH(j.value, '`)
		ctx.WriteString(col.Name)
		ctx.WriteString(`') AS `)
		ctx.WriteString(d.snowflakeCastType(col.Type))
		ctx.WriteString(`) AS `)
		ctx.Quote(col.Name)
	}
	ctx.WriteString(` FROM TABLE(FLATTEN(input => `)
	ctx.ColWithTable(sel.Rel.Left.Col.Table, sel.Rel.Left.Col.Name)
	ctx.WriteString(`)) AS j) AS `)
	ctx.Quote(sel.Table)
}

func (d *SnowflakeDialect) RenderJSONPath(ctx Context, table, col string, path []string) {
	if len(path) == 0 {
		ctx.ColWithTable(table, col)
		return
	}
	ctx.WriteString(`CAST(GET_PATH(`)
	ctx.ColWithTable(table, col)
	ctx.WriteString(`, '`)
	ctx.WriteString(strings.Join(path, "."))
	ctx.WriteString(`') AS VARCHAR)`)
}

func (d *SnowflakeDialect) RenderTryCast(ctx Context, val func(), typ string) {
	// VARIANT: Snowflake rejects TRY_CAST into VARIANT; PARSE_JSON is
	// the correct string→VARIANT conversion and returns NULL on bad
	// input (matching TRY_CAST semantics).
	// ARRAY: ARRAY_CONSTRUCT emits an ARRAY directly — wrapping it in
	// PARSE_JSON raises "Invalid argument types for PARSE_JSON: (ARRAY)".
	// Pass val() through unchanged (see RenderCast for the full rationale).
	target := d.snowflakeCastType(typ)
	switch target {
	case "VARIANT":
		ctx.WriteString(`PARSE_JSON(`)
		val()
		ctx.WriteString(`)`)
		return
	case "ARRAY":
		val()
		return
	}
	ctx.WriteString(`TRY_CAST(`)
	val()
	ctx.WriteString(` AS `)
	ctx.WriteString(target)
	ctx.WriteString(`)`)
}

func (d *SnowflakeDialect) RenderRecursiveAnchorWhere(ctx Context, psel *qcode.Select, ti sdata.DBTable, pkCol string) bool {
	// DuckDB/Snowflake emulator doesn't support outer scope correlation in recursive CTEs.
	// Inline the parent's WHERE expression instead (same approach as Oracle/MSSQL).
	if psel.Where.Exp != nil {
		ctx.RenderExp(ti, psel.Where.Exp)
		return true
	}
	return false
}

func (d *SnowflakeDialect) RenderArithOp(op qcode.ExpOp) (string, error) {
	return RenderStandardArithOp(op)
}

func (d *SnowflakeDialect) RenderOp(op qcode.ExpOp) (string, error) {
	switch op {
	case qcode.OpIn:
		return `IN`, nil
	case qcode.OpNotIn:
		return `NOT IN`, nil
	case qcode.OpLike:
		return `LIKE`, nil
	case qcode.OpILike:
		return `ILIKE`, nil
	case qcode.OpNotLike:
		return `NOT LIKE`, nil
	case qcode.OpNotILike:
		return `NOT ILIKE`, nil
	case qcode.OpContains, qcode.OpContainedIn, qcode.OpHasInCommon,
		qcode.OpHasKey, qcode.OpHasKeyAny, qcode.OpHasKeyAll:
		return "", fmt.Errorf("operator not supported in snowflake: %d", op)
	case qcode.OpSimilar, qcode.OpNotSimilar, qcode.OpRegex, qcode.OpNotRegex,
		qcode.OpIRegex, qcode.OpNotIRegex:
		return "", fmt.Errorf("pattern operator not supported in snowflake: %d", op)
	default:
		// Ops not handled here fall through to the generic operator switch
		// in psql/exp.go (equals, greater, less, etc.), which writes the
		// SQL operator directly. Returning an empty string signals "not
		// a dialect-specific op" rather than an error. If the op is truly
		// unknown the caller sees an empty op in the emitted SQL and a
		// clear syntax error at the DB.
		return "", nil
	}
}

func (d *SnowflakeDialect) RenderGeoOp(ctx Context, table, col string, ex *qcode.Exp) error {
	return fmt.Errorf("GIS operator not supported in snowflake dialect: %d", ex.Op)
}

func (d *SnowflakeDialect) RenderList(ctx Context, ex *qcode.Exp) {
	ctx.WriteString(`(`)
	for i := range ex.Right.ListVal {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		switch ex.Right.ListType {
		case qcode.ValBool, qcode.ValNum:
			ctx.WriteString(ex.Right.ListVal[i])
		case qcode.ValStr:
			ctx.WriteString(`'`)
			ctx.WriteString(strings.ReplaceAll(ex.Right.ListVal[i], `'`, `''`))
			ctx.WriteString(`'`)
		case qcode.ValDBVar:
			d.RenderVar(ctx, ex.Right.ListVal[i])
		default:
			ctx.WriteString(`'`)
			ctx.WriteString(strings.ReplaceAll(ex.Right.ListVal[i], `'`, `''`))
			ctx.WriteString(`'`)
		}
	}
	ctx.WriteString(`)`)
}

func (d *SnowflakeDialect) RenderValVar(ctx Context, ex *qcode.Exp, val string) bool {
	if strings.HasPrefix(ex.Right.Val, "__gj_ids_key:") {
		key := strings.TrimPrefix(ex.Right.Val, "__gj_ids_key:")
		ctx.WriteString(`(SELECT id FROM `)
		ctx.WriteString(d.idsTableName(ctx))
		ctx.WriteString(` WHERE k = '`)
		ctx.WriteString(strings.ReplaceAll(key, `'`, `''`))
		ctx.WriteString(`')`)
		return true
	}

	if ex.Op != qcode.OpIn && ex.Op != qcode.OpNotIn {
		return false
	}

	// Real Snowflake: LATERAL FLATTEN(input => PARSE_JSON(...)) over a
	// JSON array param. FLATTEN's `value` is VARIANT — use `::type`,
	// not TRY_CAST (which Snowflake rejects on VARIANT sources).
	ctx.WriteString(`(SELECT _gj.value::`)
	ctx.WriteString(d.snowflakeCastType(d.baseType(ex.Left.Col.Type)))
	ctx.WriteString(` FROM LATERAL FLATTEN(input => PARSE_JSON(`)
	ctx.AddParam(Param{Name: ex.Right.Val, Type: "json", IsArray: true})
	ctx.WriteString(`)) _gj)`)

	return true
}

func (d *SnowflakeDialect) RenderValPrefix(ctx Context, ex *qcode.Exp) bool {
	if ex.Op == qcode.OpHasInCommon && ex.Left.Col.Array {
		// Use ARRAYS_OVERLAP for array ∩ array tests. The earlier
		// EXISTS + LATERAL FLATTEN form was correlated against the
		// outer row and Snowflake rejected it with "Unsupported
		// subquery type cannot be evaluated" when nested inside a
		// scalar OBJECT_CONSTRUCT subquery. ARRAYS_OVERLAP is a
		// first-class array function, composable anywhere.
		ctx.WriteString(`ARRAYS_OVERLAP(`)
		d.renderOperand(ctx, ex.Left.Col.Table, ex.Left.Table, ex.Left.ID, ex.Left.Col.Name, ex.Left.ColName)
		ctx.WriteString(`, `)

		switch ex.Right.ValType {
		case qcode.ValVar:
			ctx.WriteString(`PARSE_JSON(`)
			ctx.AddParam(Param{Name: ex.Right.Val, Type: "json", IsArray: true})
			ctx.WriteString(`)`)
		case qcode.ValList:
			ctx.WriteString(`ARRAY_CONSTRUCT(`)
			for i := range ex.Right.ListVal {
				if i != 0 {
					ctx.WriteString(`, `)
				}
				switch ex.Right.ListType {
				case qcode.ValNum, qcode.ValBool:
					ctx.WriteString(ex.Right.ListVal[i])
				default:
					ctx.WriteString(`'`)
					ctx.WriteString(strings.ReplaceAll(ex.Right.ListVal[i], `'`, `''`))
					ctx.WriteString(`'`)
				}
			}
			ctx.WriteString(`)`)
		default:
			d.renderOperand(ctx, ex.Right.Col.Table, ex.Right.Table, ex.Right.ID, ex.Right.Col.Name, ex.Right.ColName)
		}

		ctx.WriteString(`)`)
		return true
	}

	if ex.Op == qcode.OpRegex || ex.Op == qcode.OpIRegex || ex.Op == qcode.OpNotRegex || ex.Op == qcode.OpNotIRegex {
		// Snowflake's REGEXP_LIKE requires the pattern to match the ENTIRE
		// string (full anchored match), which breaks the partial-match
		// semantics users expect from GraphQL regex: (and that Postgres/MySQL
		// provide). Use REGEXP_INSTR(col, pat [, 1, 1, 0, 'i']) > 0 instead —
		// it returns the 1-based position of the first substring match, or 0
		// if not found. This gives substring-match semantics while preserving
		// anchored patterns like `^foo` and `bar$`.
		if ex.Op == qcode.OpNotRegex || ex.Op == qcode.OpNotIRegex {
			ctx.WriteString(`(REGEXP_INSTR(`)
		} else {
			ctx.WriteString(`(REGEXP_INSTR(`)
		}
		d.renderOperand(ctx, ex.Left.Col.Table, ex.Left.Table, ex.Left.ID, ex.Left.Col.Name, ex.Left.ColName)
		ctx.WriteString(`, `)

		if ex.Right.ValType == qcode.ValVar {
			ctx.AddParam(Param{Name: ex.Right.Val, Type: "text"})
		} else {
			ctx.WriteString(`'`)
			ctx.WriteString(strings.ReplaceAll(ex.Right.Val, `'`, `''`))
			ctx.WriteString(`'`)
		}

		if ex.Op == qcode.OpIRegex || ex.Op == qcode.OpNotIRegex {
			// REGEXP_INSTR optional args: position=1, occurrence=1, option=0,
			// parameters='i' (case-insensitive).
			ctx.WriteString(`, 1, 1, 0, 'i'`)
		}

		if ex.Op == qcode.OpNotRegex || ex.Op == qcode.OpNotIRegex {
			ctx.WriteString(`) = 0)`)
		} else {
			ctx.WriteString(`) > 0)`)
		}
		return true
	}

	if ex.Op == qcode.OpHasKey || ex.Op == qcode.OpHasKeyAny || ex.Op == qcode.OpHasKeyAll {
		op := ` OR `
		if ex.Op == qcode.OpHasKeyAll {
			op = ` AND `
		}

		keys := ex.Right.ListVal
		if ex.Op == qcode.OpHasKey {
			if ex.Right.Val != "" {
				keys = []string{ex.Right.Val}
			}
		}
		if len(keys) == 0 {
			return false
		}

		ctx.WriteString(`(`)
		for i, key := range keys {
			if i != 0 {
				ctx.WriteString(op)
			}
			// Real Snowflake: GET_PATH (DuckDB equivalent was json_extract with '$.path').
			ctx.WriteString(`GET_PATH(`)
			d.renderOperand(ctx, ex.Left.Col.Table, ex.Left.Table, ex.Left.ID, ex.Left.Col.Name, ex.Left.ColName)
			ctx.WriteString(`, '`)
			ctx.WriteString(strings.ReplaceAll(key, `'`, `''`))
			ctx.WriteString(`') IS NOT NULL`)
		}
		ctx.WriteString(`)`)
		return true
	}

	if (ex.Op == qcode.OpIn || ex.Op == qcode.OpNotIn) && ex.Right.Col.Array && ex.Right.Col.Name != "" {
		// ARRAY_CONTAINS(<element>::VARIANT, <array>) — first-class
		// array-membership predicate. Replaces the earlier EXISTS +
		// LATERAL FLATTEN form which Snowflake rejected as
		// "Unsupported subquery type" whenever the outer column
		// reference crossed a correlated subquery scope (the common
		// case for array-column joins like
		// `EXISTS (FLATTEN(products.category_ids) WHERE value::NUMBER = categories.id)`
		// nested inside an OBJECT_CONSTRUCT scalar subquery).
		if ex.Op == qcode.OpNotIn {
			ctx.WriteString(`(NOT ARRAY_CONTAINS(`)
		} else {
			ctx.WriteString(`ARRAY_CONTAINS(`)
		}
		d.renderOperand(ctx, ex.Left.Col.Table, ex.Left.Table, ex.Left.ID, ex.Left.Col.Name, ex.Left.ColName)
		ctx.WriteString(`::VARIANT, `)
		d.renderOperand(ctx, ex.Right.Col.Table, ex.Right.Table, ex.Right.ID, ex.Right.Col.Name, ex.Right.ColName)
		if ex.Op == qcode.OpNotIn {
			ctx.WriteString(`))`)
		} else {
			ctx.WriteString(`)`)
		}
		return true
	}

	return false
}

func (d *SnowflakeDialect) RenderTsQuery(ctx Context, ti sdata.DBTable, ex *qcode.Exp) {
	ctx.WriteString(`(`)
	for i, col := range ti.FullText {
		if i != 0 {
			ctx.WriteString(` OR `)
		}
		ctx.ColWithTable(ti.Name, col.Name)
		ctx.WriteString(` ILIKE ('%' || `)
		ctx.AddParam(Param{Name: ex.Right.Val, Type: "text"})
		ctx.WriteString(` || '%')`)
	}
	ctx.WriteString(`)`)
}

func (d *SnowflakeDialect) RenderArray(ctx Context, items []string) {
	// Real Snowflake: ARRAY_CONSTRUCT (DuckDB equivalent was list_value).
	ctx.WriteString(`ARRAY_CONSTRUCT(`)
	for i, item := range items {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.WriteString(item)
	}
	ctx.WriteString(`)`)
}

func (d *SnowflakeDialect) RenderMutationInput(ctx Context, qc *qcode.QCode) {
	ctx.WriteString(`WITH `)
	ctx.Quote("_sg_input")
	ctx.WriteString(` AS (SELECT `)
	ctx.AddParam(Param{Name: qc.ActionVar, Type: "json"})
	ctx.WriteString(` AS j)`)
}

func (d *SnowflakeDialect) RenderSearchRank(ctx Context, sel *qcode.Select, f qcode.Field) {
	ctx.WriteString(`0`)
}

func (d *SnowflakeDialect) RenderSearchHeadline(ctx Context, sel *qcode.Select, f qcode.Field) {
	ctx.WriteString(`''`)
}

func (d *SnowflakeDialect) RequiresJSONAsString() bool {
	return true
}

func (d *SnowflakeDialect) RequiresLowercaseIdentifiers() bool {
	return false
}

func (d *SnowflakeDialect) RequiresBooleanAsInt() bool {
	return false
}

func (d *SnowflakeDialect) SupportsSubscriptionBatching() bool {
	return false
}

func (d *SnowflakeDialect) SupportsReturning() bool {
	return false
}

func (d *SnowflakeDialect) SupportsWritableCTE() bool {
	return false
}

func (d *SnowflakeDialect) SupportsLinearExecution() bool {
	return true
}

func (d *SnowflakeDialect) RenderSetup(ctx Context) {
	ctx.WriteString(`DROP TABLE IF EXISTS `)
	ctx.WriteString(d.idsTableName(ctx))
	ctx.WriteString(`; DROP TABLE IF EXISTS `)
	ctx.WriteString(d.prevIDsTableName(ctx))
	ctx.WriteString(`; `)
	// The `id` column is VARCHAR so it can hold any PK representation —
	// BIGINT, VARCHAR, UUID-as-text, composite-as-string, etc. A single
	// session may run mutations across tables with mixed PK types
	// (graph_node.id VARCHAR vs users.id BIGINT), and VARCHAR accepts
	// all of them via TO_VARCHAR() at the INSERT site. Readers rely on
	// Snowflake's implicit VARCHAR→BIGINT coercion in comparison
	// contexts, which always succeeds for well-formed numeric strings
	// (the BIGINT values we wrote via TO_VARCHAR are always valid
	// integer literals).
	//
	// Previous iteration used VARIANT, but Snowflake rejects the
	// implicit VARCHAR→VARIANT coercion on INSERT so every write site
	// would have needed a TO_VARIANT wrapper — VARCHAR keeps the INSERT
	// site simpler and still round-trips correctly.
	//
	// Retry-safe on the same Snowflake session: if a previous attempt
	// created the temp table before failing, CREATE OR REPLACE rebuilds
	// it empty instead of erroring on retry.
	// Use regular (non-TEMP) tables for _gj_ids / _gj_prev_ids. TEMP
	// tables are session-scoped on Snowflake and evaporate when the Go
	// sql.Conn hands the statement off to a new driver session — which
	// the gosnowflake driver does across multi-statement scripts, leaving
	// the downstream `SELECT id FROM _gj_ids` subqueries returning NULL.
	// The hex suffix still makes each GraphJin call's table unique and
	// RenderTeardown drops them afterwards, so the on-disk cost is a
	// short-lived scratch table.
	ctx.WriteString(`CREATE OR REPLACE TABLE `)
	ctx.WriteString(d.idsTableName(ctx))
	ctx.WriteString(` (k VARCHAR, id VARCHAR); `)
	ctx.WriteString(`CREATE OR REPLACE TABLE `)
	ctx.WriteString(d.prevIDsTableName(ctx))
	ctx.WriteString(` (k VARCHAR, id VARCHAR); `)
}

func (d *SnowflakeDialect) RenderBegin(ctx Context) {}

func (d *SnowflakeDialect) RenderTeardown(ctx Context) {
	ctx.WriteString(`; DROP TABLE IF EXISTS `)
	ctx.WriteString(d.prevIDsTableName(ctx))
	ctx.WriteString(`; DROP TABLE IF EXISTS `)
	ctx.WriteString(d.idsTableName(ctx))
	ctx.WriteString(`; `)
}

func (d *SnowflakeDialect) RenderVarDeclaration(ctx Context, name, typeName string) {}

func (d *SnowflakeDialect) RenderIDCapture(ctx Context, varName string) {}

func (d *SnowflakeDialect) RenderVar(ctx Context, name string) {
	ctx.WriteString(`(SELECT id FROM `)
	ctx.WriteString(d.idsTableName(ctx))
	ctx.WriteString(` WHERE k = '`)
	ctx.WriteString(strings.ReplaceAll(name, "'", "''"))
	ctx.WriteString(`' ORDER BY id DESC LIMIT 1)`)
}

func (d *SnowflakeDialect) RenderLinearInsert(ctx Context, m *qcode.Mutate, qc *qcode.QCode, varName string, renderColVal func(qcode.MColumn)) {
	ctx.WriteString(`DELETE FROM `)
	ctx.WriteString(d.prevIDsTableName(ctx))
	ctx.WriteString(` WHERE k = '`)
	ctx.WriteString(strings.ReplaceAll(varName, "'", "''"))
	ctx.WriteString(`'; `)
	ctx.WriteString(`INSERT INTO `)
	ctx.WriteString(d.prevIDsTableName(ctx))
	ctx.WriteString(` (k, id) SELECT '`)
	ctx.WriteString(strings.ReplaceAll(varName, "'", "''"))
	ctx.WriteString(`', TO_VARCHAR(`)
	ctx.Quote(m.Ti.PrimaryCol.Name)
	ctx.WriteString(`) FROM `)
	d.renderTableRef(ctx, m.Ti.Schema, m.Ti.Name)
	ctx.WriteString(`; `)

	ctx.WriteString(`INSERT INTO `)
	d.renderTableRef(ctx, m.Ti.Schema, m.Ti.Name)
	ctx.WriteString(` (`)

	i := 0
	for _, col := range m.Cols {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.Quote(col.Col.Name)
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

	// Always SELECT rather than VALUES — Snowflake's VALUES clause
	// rejects function calls like PARSE_JSON / ARRAY_CONSTRUCT
	// ("Invalid expression [PARSE_JSON('...')] in VALUES clause"),
	// which would otherwise break inserts into VARIANT / ARRAY
	// columns. A single-row SELECT from a dummy source is equivalent
	// in plan shape and accepts every expression Snowflake supports.
	ctx.WriteString(` SELECT `)

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
		found := false
		for id := range m.DependsOn {
			if qc.Mutates[id].Ti.Name == rcol.VCol.Table {
				d.RenderVar(ctx, d.getVarName(qc.Mutates[id]))
				found = true
				break
			}
		}
		if !found {
			ctx.WriteString(`NULL`)
		}
		i++
	}

	if m.IsJSON {
		ctx.WriteString(` FROM `)
		d.RenderMutateToRecordSet(ctx, m, 0, func() {
			ctx.AddParam(Param{Name: qc.ActionVar, Type: "json"})
		})
	} else {
		// Dummy FROM so the SELECT is well-formed. Snowflake requires
		// a FROM clause for projection-only SELECTs in INSERT ... SELECT.
		ctx.WriteString(` FROM (SELECT 1) AS _gj_dummy`)
	}

	ctx.WriteString(`; INSERT INTO `)
	ctx.WriteString(d.idsTableName(ctx))
	ctx.WriteString(` (k, id) SELECT '`)
	ctx.WriteString(strings.ReplaceAll(varName, "'", "''"))
	ctx.WriteString(`', TO_VARCHAR(`)
	ctx.Quote(m.Ti.PrimaryCol.Name)
	ctx.WriteString(`) FROM `)
	d.renderTableRef(ctx, m.Ti.Schema, m.Ti.Name)
	ctx.WriteString(` EXCEPT SELECT '`)
	ctx.WriteString(strings.ReplaceAll(varName, "'", "''"))
	ctx.WriteString(`', id FROM `)
	ctx.WriteString(d.prevIDsTableName(ctx))
	ctx.WriteString(` WHERE k = '`)
	ctx.WriteString(strings.ReplaceAll(varName, "'", "''"))
	ctx.WriteString(`'`)
}

func (d *SnowflakeDialect) RenderLinearUpdate(ctx Context, m *qcode.Mutate, qc *qcode.QCode, varName string, renderColVal func(qcode.MColumn), renderWhere func()) {
	if m.ParentID != -1 && m.IsJSON && !d.mutationHasExplicitPK(m) {
		d.renderChildUpdate(ctx, m, qc, varName, renderWhere)
		return
	}

	ctx.WriteString(`INSERT INTO `)
	ctx.WriteString(d.idsTableName(ctx))
	ctx.WriteString(` (k, id) SELECT '`)
	ctx.WriteString(strings.ReplaceAll(varName, "'", "''"))
	ctx.WriteString(`', TO_VARCHAR(`)
	ctx.ColWithTable(m.Ti.Name, m.Ti.PrimaryCol.Name)
	ctx.WriteString(`) FROM `)
	d.renderTableRef(ctx, m.Ti.Schema, m.Ti.Name)
	ctx.WriteString(` AS `)
	ctx.Quote(m.Ti.Name)
	if m.IsJSON {
		ctx.WriteString(`, `)
		d.RenderMutateToRecordSet(ctx, m, 0, func() {
			ctx.AddParam(Param{Name: qc.ActionVar, Type: "json"})
		})
	}
	ctx.WriteString(` WHERE `)
	renderWhere()
	ctx.WriteString(`; `)

	ctx.WriteString(`UPDATE `)
	d.renderTableRef(ctx, m.Ti.Schema, m.Ti.Name)
	ctx.WriteString(` SET `)

	i := 0
	for _, col := range m.Cols {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.Quote(col.Col.Name)
		ctx.WriteString(` = `)
		renderColVal(col)
		i++
	}
	// Skip RCols for UPDATE: the WHERE clause already identifies the child row
	// via the FK relationship. Setting the child's PK to the parent's ID is wrong.
	if i == 0 {
		for j, pkCol := range m.Ti.PrimaryCols {
			if j > 0 {
				ctx.WriteString(`, `)
			}
			ctx.Quote(pkCol.Name)
			ctx.WriteString(` = `)
			ctx.Quote(pkCol.Name)
		}
	}

	if m.IsJSON {
		ctx.WriteString(` FROM `)
		d.RenderMutateToRecordSet(ctx, m, 0, func() {
			ctx.AddParam(Param{Name: qc.ActionVar, Type: "json"})
		})
	}
	ctx.WriteString(` WHERE `)
	renderWhere()
}

func (d *SnowflakeDialect) renderChildUpdate(ctx Context, m *qcode.Mutate, qc *qcode.QCode, varName string, renderWhere func()) {
	ctx.WriteString(`INSERT INTO `)
	ctx.WriteString(d.idsTableName(ctx))
	ctx.WriteString(` (k, id) SELECT '`)
	ctx.WriteString(strings.ReplaceAll(varName, "'", "''"))
	ctx.WriteString(`', TO_VARCHAR(`)
	ctx.ColWithTable(m.Ti.Name, m.Ti.PrimaryCol.Name)
	ctx.WriteString(`) FROM `)
	d.renderTableRef(ctx, m.Ti.Schema, m.Ti.Name)
	ctx.WriteString(` AS `)
	ctx.Quote(m.Ti.Name)
	ctx.WriteString(` WHERE `)
	renderWhere()
	ctx.WriteString(`; `)

	ctx.WriteString(`UPDATE `)
	d.renderTableRef(ctx, m.Ti.Schema, m.Ti.Name)
	ctx.WriteString(` SET `)

	jsonPathPrefix := d.mutationJSONPathPrefix(m.Path)
	i := 0
	for _, col := range m.Cols {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.Quote(col.Col.Name)
		ctx.WriteString(` = `)
		if col.Set {
			d.renderMutationPresetValue(ctx, col)
		} else {
			d.renderMutationJSONValue(ctx, qc.ActionVar, jsonPathPrefix, col)
		}
		i++
	}

	if i == 0 {
		for j, pkCol := range m.Ti.PrimaryCols {
			if j > 0 {
				ctx.WriteString(`, `)
			}
			ctx.Quote(pkCol.Name)
			ctx.WriteString(` = `)
			ctx.Quote(pkCol.Name)
		}
	}

	ctx.WriteString(` WHERE `)
	renderWhere()
}

func (d *SnowflakeDialect) RenderLinearConnect(ctx Context, m *qcode.Mutate, qc *qcode.QCode, varName string, renderFilter func()) {
	if qc.SType != qcode.QTUpdate {
		// Insert-time connect keys are consumed by later insert values and final
		// readback, so only skip capture for update mutations.
		ctx.WriteString(`INSERT INTO `)
		ctx.WriteString(d.idsTableName(ctx))
		ctx.WriteString(` (k, id) SELECT '`)
		ctx.WriteString(strings.ReplaceAll(varName, "'", "''"))
		ctx.WriteString(`', TO_VARCHAR(`)
		ctx.ColWithTable(m.Ti.Name, m.Rel.Left.Col.Name)
		ctx.WriteString(`) FROM `)
		d.renderTableRef(ctx, m.Ti.Schema, m.Ti.Name)
		ctx.WriteString(` WHERE `)
		renderFilter()
	}

	// Find parent mutation to get its captured ID
	var parentVar string
	for id := range m.DependsOn {
		if qc.Mutates[id].Ti.Name == m.Rel.Right.Col.Table {
			parentVar = d.getVarName(qc.Mutates[id])
			break
		}
	}
	if parentVar != "" {
		// Update FK to point to parent. Snowflake doesn't need a pre-update
		// _gj_ids capture for update-time connect/disconnect, and skipping that
		// extra Exec removes the last flaky linear-mutation statement shape.
		if qc.SType != qcode.QTUpdate {
			ctx.WriteString(`; `)
		}
		ctx.WriteString(`UPDATE `)
		d.renderTableRef(ctx, m.Ti.Schema, m.Ti.Name)
		ctx.WriteString(` SET `)
		ctx.Quote(m.Rel.Left.Col.Name)
		ctx.WriteString(` = `)
		d.RenderVar(ctx, parentVar)
		ctx.WriteString(` WHERE `)
		renderFilter()
	}
}

func (d *SnowflakeDialect) RenderLinearDisconnect(ctx Context, m *qcode.Mutate, qc *qcode.QCode, varName string, renderFilter func()) {
	if qc.SType != qcode.QTUpdate {
		ctx.WriteString(`INSERT INTO `)
		ctx.WriteString(d.idsTableName(ctx))
		ctx.WriteString(` (k, id) SELECT '`)
		ctx.WriteString(strings.ReplaceAll(varName, "'", "''"))
		ctx.WriteString(`', TO_VARCHAR(`)
		ctx.ColWithTable(m.Ti.Name, m.Rel.Left.Col.Name)
		ctx.WriteString(`) FROM `)
		d.renderTableRef(ctx, m.Ti.Schema, m.Ti.Name)
		ctx.WriteString(` WHERE `)
		renderFilter()
		ctx.WriteString(`; `)
	}

	// Set FK to NULL. Snowflake final readback does not consume an update-time
	// disconnect capture key, so avoid emitting that separate Exec in QTUpdate.
	ctx.WriteString(`UPDATE `)
	d.renderTableRef(ctx, m.Ti.Schema, m.Ti.Name)
	ctx.WriteString(` SET `)
	ctx.Quote(m.Rel.Left.Col.Name)
	ctx.WriteString(` = NULL`)
	ctx.WriteString(` WHERE `)
	renderFilter()
}

func (d *SnowflakeDialect) ModifySelectsForMutation(qc *qcode.QCode) {
	if qc.Type != qcode.QTMutation || qc.Selects == nil {
		return
	}

	for i := range qc.Selects {
		sel := &qc.Selects[i]
		if sel.ParentID != -1 || sel.Where.Exp != nil {
			continue
		}

		var mutations []qcode.Mutate
		for _, m := range qc.Mutates {
			if m.Ti.Name == sel.Table && (m.Type == qcode.MTInsert || m.Type == qcode.MTUpdate || m.Type == qcode.MTUpsert) {
				mutations = append(mutations, m)
			}
		}
		if len(mutations) == 0 {
			continue
		}

		var exp *qcode.Exp
		if len(mutations) == 1 {
			m := mutations[0]
			col := m.Ti.PrimaryCol
			col.Table = m.Ti.Name
			if m.Array {
				exp = &qcode.Exp{Op: qcode.OpIn}
				exp.Left.Col = col
				exp.Left.ID = -1
				exp.Right.ValType = qcode.ValVar
				exp.Right.Val = "__gj_ids_key:" + d.getVarName(m)
			} else {
				exp = &qcode.Exp{Op: qcode.OpEquals}
				exp.Left.Col = col
				exp.Left.ID = -1
				exp.Right.ValType = qcode.ValDBVar
				exp.Right.Val = d.getVarName(m)
			}
		} else {
			m := mutations[0]
			col := m.Ti.PrimaryCol
			col.Table = m.Ti.Name
			exp = &qcode.Exp{Op: qcode.OpIn}
			exp.Left.Col = col
			exp.Left.ID = -1
			exp.Right.ValType = qcode.ValList
			exp.Right.ListType = qcode.ValDBVar
			for _, mut := range mutations {
				exp.Right.ListVal = append(exp.Right.ListVal, d.getVarName(mut))
			}
		}
		sel.Where.Exp = exp
	}
}

func (d *SnowflakeDialect) SplitQuery(query string) (parts []string) {
	var buf strings.Builder
	var inStr, inQuote, inLineComment, inBlockComment bool
	var depth int

	for i := 0; i < len(query); i++ {
		ch := query[i]
		if inLineComment {
			if ch == '\n' {
				inLineComment = false
			}
			buf.WriteByte(ch)
			continue
		}
		if inBlockComment {
			// Terminate on `*/`. A `;` inside a block comment must not
			// split the statement.
			if ch == '*' && i+1 < len(query) && query[i+1] == '/' {
				buf.WriteByte(ch)
				i++
				buf.WriteByte(query[i])
				inBlockComment = false
				continue
			}
			buf.WriteByte(ch)
			continue
		}
		if inStr {
			buf.WriteByte(ch)
			if ch == '\'' {
				if i+1 < len(query) && query[i+1] == '\'' {
					buf.WriteByte(query[i+1])
					i++
				} else {
					inStr = false
				}
			}
			continue
		}
		if inQuote {
			buf.WriteByte(ch)
			if ch == '"' {
				if i+1 < len(query) && query[i+1] == '"' {
					buf.WriteByte(query[i+1])
					i++
				} else {
					inQuote = false
				}
			}
			continue
		}

		switch ch {
		case '-':
			if i+1 < len(query) && query[i+1] == '-' {
				inLineComment = true
				buf.WriteByte(ch)
				i++
				buf.WriteByte(query[i])
				continue
			}
			buf.WriteByte(ch)
		case '/':
			if i+1 < len(query) && query[i+1] == '*' {
				inBlockComment = true
				buf.WriteByte(ch)
				i++
				buf.WriteByte(query[i])
				continue
			}
			buf.WriteByte(ch)
		case '\'':
			inStr = true
			buf.WriteByte(ch)
		case '"':
			inQuote = true
			buf.WriteByte(ch)
		case '(':
			depth++
			buf.WriteByte(ch)
		case ')':
			if depth > 0 {
				depth--
			}
			buf.WriteByte(ch)
		case ';':
			if depth == 0 {
				stmt := strings.TrimSpace(buf.String())
				if stmt != "" {
					parts = append(parts, stmt)
				}
				buf.Reset()
			} else {
				buf.WriteByte(ch)
			}
		default:
			buf.WriteByte(ch)
		}
	}

	stmt := strings.TrimSpace(buf.String())
	if stmt != "" {
		parts = append(parts, stmt)
	}
	return parts
}

func (d *SnowflakeDialect) RenderCast(ctx Context, val func(), typ string) {
	// Snowflake rejects CAST(<string> AS VARIANT) in VALUES /
	// projection contexts with "Invalid expression ... in VALUES
	// clause". PARSE_JSON is the documented conversion from a string
	// literal to VARIANT.
	//
	// ARRAY targets need special care: in mutation contexts val() is
	// RenderArray which emits ARRAY_CONSTRUCT(...) — wrapping that in
	// PARSE_JSON fails ("Invalid argument types for PARSE_JSON: (ARRAY)").
	// ARRAY_CONSTRUCT IS already an ARRAY so we pass it through
	// unchanged. For non-ARRAY_CONSTRUCT sources targeting ARRAY (e.g.
	// CAST('[1,2]' AS ARRAY) in an expression), the upstream caller
	// should route through expressions that eventually hit PARSE_JSON
	// themselves; the common mutation path sidesteps it entirely.
	target := d.snowflakeCastType(typ)
	switch target {
	case "VARIANT":
		ctx.WriteString(`PARSE_JSON(`)
		val()
		ctx.WriteString(`)`)
		return
	case "ARRAY":
		// ARRAY_CONSTRUCT emits an ARRAY directly — no coercion needed.
		// This matches every call site in mutate.go (renderArrayValue).
		val()
		return
	}
	ctx.WriteString(`CAST(`)
	val()
	ctx.WriteString(` AS `)
	ctx.WriteString(target)
	ctx.WriteString(`)`)
}

func (d *SnowflakeDialect) RenderMutateToRecordSet(ctx Context, m *qcode.Mutate, n int, renderRoot func()) {
	if n != 0 {
		ctx.WriteString(`, `)
	}

	if m.Array {
		// Real Snowflake: GET_PATH for field access, LATERAL FLATTEN for array iteration.
		// (DuckDB equivalents were json_extract / json_extract_string / json_each.)
		// Note: LATERAL FLATTEN exposes a per-row column accessed via the alias t.value.
		ctx.WriteString(`(SELECT `)
		hasPK := false
		first := true

		for _, col := range m.Cols {
			if !first {
				ctx.WriteString(`, `)
			}
			first = false

			if m.Ti.IsPKCol(col.Col.Name) {
				hasPK = true
			}

			if !col.Col.Array && !d.isJSONLikeType(col.Col.Type) {
				// Use `::type` (the colon-colon cast) rather than
				// TRY_CAST because Snowflake rejects TRY_CAST from
				// VARIANT to scalar types ("Function TRY_CAST cannot
				// be used with arguments of types VARIANT and
				// NUMBER/FLOAT/TIMESTAMP_NTZ"). The `::` syntax is the
				// canonical VARIANT-to-scalar extraction on Snowflake
				// and yields NULL on malformed input just like
				// TRY_CAST would on scalar inputs.
				ctx.WriteString(`GET_PATH(_gj_f.value, '`)
				ctx.WriteString(col.FieldName)
				ctx.WriteString(`')::`)
				ctx.WriteString(d.snowflakeCastType(col.Col.Type))
				ctx.WriteString(` AS `)
			} else {
				ctx.WriteString(`GET_PATH(_gj_f.value, '`)
				ctx.WriteString(col.FieldName)
				ctx.WriteString(`') AS `)
			}
			ctx.Quote(col.FieldName)
		}

		if !hasPK {
			if !first {
				ctx.WriteString(`, `)
			}
			ctx.WriteString(`GET_PATH(_gj_f.value, '`)
			ctx.WriteString(m.Ti.PrimaryCol.Name) // Use first PK col for implicit tracking
			ctx.WriteString(`') AS "_gj_pkt"`)
		}

		ctx.WriteString(` FROM LATERAL FLATTEN(input => `)
		if len(m.Path) > 0 {
			ctx.WriteString(`GET_PATH(PARSE_JSON(`)
			renderRoot()
			ctx.WriteString(`), '`)
			ctx.WriteString(strings.Join(m.Path, "."))
			ctx.WriteString(`')`)
		} else {
			ctx.WriteString(`PARSE_JSON(`)
			renderRoot()
			ctx.WriteString(`)`)
		}
		// `_gj_f` names the LATERAL FLATTEN output so the surrounding
		// SELECT can reach `_gj_f.value` / `.index` without ambiguity; the
		// outer `AS "t"` below is the derived-table alias the cross-dialect
		// psql caller references via `"t"."col"`. Without these two
		// aliases the FLATTEN's output columns aren't in scope inside the
		// SELECT list and Snowflake raises `invalid identifier '"t".VALUE'`.
		ctx.WriteString(`) AS _gj_f) AS "t"`)
		return
	}

	ctx.WriteString(`(SELECT `)
	hasPK := false
	first := true

	for _, col := range m.Cols {
		if !first {
			ctx.WriteString(`, `)
		}
		first = false

		if m.Ti.IsPKCol(col.Col.Name) {
			hasPK = true
		}

		pathPrefix := ""
		if len(m.Path) > 0 {
			pathPrefix = strings.Join(m.Path, ".") + `.`
		}
		if !col.Col.Array && !d.isJSONLikeType(col.Col.Type) {
			// Use `::type` (colon-colon cast) rather than TRY_CAST —
			// same reasoning as the array branch above. Snowflake
			// rejects TRY_CAST VARIANT → scalar; the `::` form is
			// the canonical VARIANT extraction and returns NULL on
			// malformed paths.
			ctx.WriteString(`GET_PATH(PARSE_JSON(`)
			renderRoot()
			ctx.WriteString(`), '`)
			ctx.WriteString(pathPrefix)
			ctx.WriteString(col.FieldName)
			ctx.WriteString(`')::`)
			ctx.WriteString(d.snowflakeCastType(col.Col.Type))
			ctx.WriteString(` AS `)
		} else {
			ctx.WriteString(`GET_PATH(PARSE_JSON(`)
			renderRoot()
			ctx.WriteString(`), '`)
			ctx.WriteString(pathPrefix)
			ctx.WriteString(col.FieldName)
			ctx.WriteString(`') AS `)
		}
		ctx.Quote(col.FieldName)
	}

	if !hasPK {
		if !first {
			ctx.WriteString(`, `)
		}
		// Same VARIANT → scalar `::BIGINT` pattern for the implicit
		// PK-tracking column added when the user didn't select the PK.
		ctx.WriteString(`GET_PATH(PARSE_JSON(`)
		renderRoot()
		ctx.WriteString(`), '`)
		if len(m.Path) > 0 {
			ctx.WriteString(strings.Join(m.Path, "."))
			ctx.WriteString(`.`)
		}
		ctx.WriteString(m.Ti.PrimaryCol.Name)
		ctx.WriteString(`')::BIGINT AS "_gj_pkt"`)
	}

	// Quoted-lowercase alias — see note in the Array branch above.
	ctx.WriteString(` FROM (SELECT 1) AS _gj_dummy) AS "t"`)
}

func (d *SnowflakeDialect) RequiresNullOnEmptySelect() bool {
	return true
}

func (d *SnowflakeDialect) idsTableName(ctx Context) string {
	return d.tempTableName(ctx, "_gj_ids")
}

func (d *SnowflakeDialect) prevIDsTableName(ctx Context) string {
	return d.tempTableName(ctx, "_gj_prev_ids")
}

func (d *SnowflakeDialect) tempTableName(ctx Context, base string) string {
	prefix := strings.TrimSuffix(ctx.GetSecPrefix(), ":")
	prefix = strings.TrimPrefix(prefix, "gj-")
	if prefix == "" {
		return base
	}

	var b strings.Builder
	b.Grow(len(base) + len(prefix) + 1)
	b.WriteString(base)
	b.WriteByte('_')
	for _, r := range prefix {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func (d *SnowflakeDialect) getVarName(m qcode.Mutate) string {
	return m.Ti.Name + "_" + fmt.Sprintf("%d", m.ID)
}

func (d *SnowflakeDialect) mutationHasExplicitPK(m *qcode.Mutate) bool {
	for _, col := range m.Cols {
		if m.Ti.IsPKCol(col.Col.Name) {
			return true
		}
	}
	return false
}

func (d *SnowflakeDialect) mutationJSONPathPrefix(path []string) string {
	if len(path) == 0 {
		return "$"
	}
	return "$." + strings.Join(path, ".")
}

func (d *SnowflakeDialect) renderMutationPresetValue(ctx Context, col qcode.MColumn) {
	if strings.HasPrefix(col.Value, "sql:") {
		ctx.WriteString(`(`)
		ctx.WriteString(col.Value[4:])
		ctx.WriteString(`)`)
		return
	}

	ctx.WriteString(`'`)
	ctx.WriteString(col.Value)
	ctx.WriteString(`'`)
}

func (d *SnowflakeDialect) renderMutationJSONValue(ctx Context, actionVar, jsonPathPrefix string, col qcode.MColumn) {
	// Real Snowflake: GET_PATH(PARSE_JSON(...)) (DuckDB equivalent was json_extract(...,'$.path')).
	// Path strings are stored with leading "$." (json_extract syntax) — strip for GET_PATH.
	path := strings.TrimPrefix(jsonPathPrefix+"."+col.FieldName, "$.")
	if !col.Col.Array && !d.isJSONLikeType(col.Col.Type) {
		if d.isStringType(col.Col.Type) {
			ctx.WriteString(`GET_PATH(PARSE_JSON(`)
			ctx.AddParam(Param{Name: actionVar, Type: "json"})
			ctx.WriteString(`), '`)
			ctx.WriteString(path)
			ctx.WriteString(`')::VARCHAR`)
			return
		}

		// VARIANT → scalar via `::type`; TRY_CAST from VARIANT is
		// rejected by Snowflake for non-string scalar types.
		ctx.WriteString(`GET_PATH(PARSE_JSON(`)
		ctx.AddParam(Param{Name: actionVar, Type: "json"})
		ctx.WriteString(`), '`)
		ctx.WriteString(path)
		ctx.WriteString(`')::`)
		ctx.WriteString(d.snowflakeCastType(col.Col.Type))
		return
	}

	ctx.WriteString(`GET_PATH(PARSE_JSON(`)
	ctx.AddParam(Param{Name: actionVar, Type: "json"})
	ctx.WriteString(`), '`)
	ctx.WriteString(path)
	ctx.WriteString(`')`)
}

func (d *SnowflakeDialect) renderTableRef(ctx Context, schema, table string) {
	if schema != "" {
		ctx.Quote(schema)
		ctx.WriteString(`.`)
	}
	ctx.Quote(table)
}

func (d *SnowflakeDialect) snowflakeCastType(t string) string {
	tt := strings.TrimSpace(t)
	if strings.HasSuffix(tt, "[]") {
		return d.snowflakeArrayCastType(strings.TrimSuffix(tt, "[]"))
	}

	switch strings.ToLower(strings.TrimSpace(d.baseType(tt))) {
	case "int", "integer", "int4", "int8", "bigint", "smallint":
		return "BIGINT"
	case "float", "float4", "float8", "double", "real":
		return "DOUBLE"
	case "numeric", "decimal", "number":
		return "NUMBER"
	case "boolean", "bool":
		return "BOOLEAN"
	case "variant", "object":
		return "VARIANT"
	case "array":
		return "ARRAY"
	case "json", "jsonb":
		// JSON-like Postgres types map to Snowflake VARIANT (the closest equivalent).
		return "VARIANT"
	case "timestamp", "timestamp without time zone", "timestamp_ntz":
		return "TIMESTAMP_NTZ"
	case "timestamptz", "timestamp with time zone", "timestamp_tz":
		return "TIMESTAMP_TZ"
	case "timestamp_ltz":
		return "TIMESTAMP_LTZ"
	case "date":
		return "DATE"
	case "time", "time without time zone", "time with time zone":
		return "TIME"
	case "text", "varchar", "character varying", "string", "uuid", "clob", "nclob":
		return "VARCHAR"
	case "geography":
		return "GEOGRAPHY"
	case "geometry":
		return "GEOMETRY"
	default:
		// Preserve unknown custom types in their original case (case-preservation goal).
		return strings.TrimSpace(t)
	}
}

func (d *SnowflakeDialect) snowflakeArrayCastType(_ string) string {
	// Snowflake has only an untyped ARRAY type. Element type ignored.
	return "ARRAY"
}

func (d *SnowflakeDialect) baseType(t string) string {
	t = strings.TrimSpace(t)
	for strings.HasSuffix(t, "[]") {
		t = strings.TrimSpace(strings.TrimSuffix(t, "[]"))
	}
	return t
}

func (d *SnowflakeDialect) isStringType(t string) bool {
	switch strings.ToLower(strings.TrimSpace(d.baseType(t))) {
	case "text", "varchar", "character varying", "string", "uuid", "clob", "nclob":
		return true
	default:
		return false
	}
}

func (d *SnowflakeDialect) isJSONLikeType(t string) bool {
	switch strings.ToLower(strings.TrimSpace(d.baseType(t))) {
	case "json", "jsonb", "variant", "object", "array":
		return true
	default:
		return false
	}
}

func (d *SnowflakeDialect) renderOperand(ctx Context, colTable, tableOverride string, id int32, colName, colNameOverride string) {
	table := colTable
	if tableOverride != "" {
		table = tableOverride
	}
	if id >= 0 {
		table = fmt.Sprintf("%s_%d", table, id)
	}

	col := colName
	if colNameOverride != "" {
		col = colNameOverride
	}

	ctx.ColWithTable(table, col)
}
