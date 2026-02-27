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
	return `"` + s + `"`
}

func (d *SnowflakeDialect) BindVar(i int) string {
	return "?"
}

func (d *SnowflakeDialect) UseNamedParams() bool {
	return false
}

func (d *SnowflakeDialect) RenderJSONRoot(ctx Context, sel *qcode.Select) {
	ctx.WriteString(`SELECT CAST(json_object(`)
}

func (d *SnowflakeDialect) RenderJSONSelect(ctx Context, sel *qcode.Select) {
	ctx.WriteString(`SELECT json_object(`)
	ctx.RenderJSONFields(sel)
	ctx.WriteString(`)`)
}

func (d *SnowflakeDialect) RenderJSONPlural(ctx Context, sel *qcode.Select) {
	ctx.WriteString(`COALESCE(array_agg(__sj_`)
	ctx.WriteString(strconv.Itoa(int(sel.ID)))
	ctx.WriteString(`.json), list_value())`)
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
	return false
}

func (d *SnowflakeDialect) RenderInlineChild(ctx Context, renderer InlineChildRenderer, psel, sel *qcode.Select) {
	renderer.RenderDefaultInlineChild(sel)
}

func (d *SnowflakeDialect) RenderChildCursor(ctx Context, renderChild func()) {
	ctx.WriteString(`json_extract(`)
	renderChild()
	ctx.WriteString(`, '$.cursor')`)
}

func (d *SnowflakeDialect) RenderChildValue(ctx Context, sel *qcode.Select, renderChild func()) {
	if sel.Paging.Cursor {
		ctx.WriteString(`json_extract(`)
		renderChild()
		ctx.WriteString(`, '$.json')`)
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

	ctx.WriteString(`WITH __cur AS (SELECT `)
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
		ctx.WriteString(` JOIN (SELECT TRY_CAST(value AS `)
		ctx.WriteString(d.snowflakeCastType(ob.Col.Type))
		ctx.WriteString(`) AS id, TRY_CAST(key AS BIGINT) + 1 AS ord FROM json_each(CAST(`)
		ctx.AddParam(Param{Name: ob.Var, Type: "json"})
		ctx.WriteString(` AS JSON))) AS _gj_ob_`)
		ctx.WriteString(ob.Col.Name)
		ctx.WriteString(`(id, ord) USING (id)`)
	}
}

func (d *SnowflakeDialect) RenderOrderBy(ctx Context, sel *qcode.Select) {
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
			ctx.ColWithTable(`_gj_ob_`+ob.Col.Name, "ord")
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
	ctx.WriteString(`(SELECT `)
	for i, col := range sel.Ti.Columns {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.WriteString(`CAST(json_extract(j.value, '$.`)
		ctx.WriteString(col.Name)
		ctx.WriteString(`') AS `)
		ctx.WriteString(d.snowflakeCastType(col.Type))
		ctx.WriteString(`) AS `)
		ctx.Quote(col.Name)
	}
	ctx.WriteString(` FROM json_each(`)
	ctx.ColWithTable(sel.Rel.Left.Col.Table, sel.Rel.Left.Col.Name)
	ctx.WriteString(`) AS j) AS `)
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
	ctx.WriteString(`TRY_CAST(`)
	val()
	ctx.WriteString(` AS `)
	ctx.WriteString(d.snowflakeCastType(typ))
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
		ctx.WriteString(`(SELECT id FROM _gj_ids WHERE k = '`)
		ctx.WriteString(strings.ReplaceAll(key, `'`, `''`))
		ctx.WriteString(`')`)
		return true
	}

	if ex.Op != qcode.OpIn && ex.Op != qcode.OpNotIn {
		return false
	}

	ctx.WriteString(`(SELECT TRY_CAST(x AS `)
	ctx.WriteString(d.snowflakeCastType(d.baseType(ex.Left.Col.Type)))
	ctx.WriteString(`) FROM UNNEST(CAST(json(`)
	ctx.AddParam(Param{Name: ex.Right.Val, Type: "json", IsArray: true})
	ctx.WriteString(`) AS `)
	ctx.WriteString(d.snowflakeArrayCastType(d.baseType(ex.Left.Col.Type)))
	ctx.WriteString(`)) AS _gj(x))`)

	return true
}

func (d *SnowflakeDialect) RenderValPrefix(ctx Context, ex *qcode.Exp) bool {
	if ex.Op == qcode.OpHasInCommon && ex.Left.Col.Array {
		ctx.WriteString(`EXISTS (SELECT 1 FROM UNNEST(`)
		d.renderOperand(ctx, ex.Left.Col.Table, ex.Left.Table, ex.Left.ID, ex.Left.Col.Name, ex.Left.ColName)
		ctx.WriteString(`) AS __gj_l("x") WHERE TRY_CAST(__gj_l."x" AS `)
		ctx.WriteString(d.snowflakeCastType(d.baseType(ex.Left.Col.Type)))
		ctx.WriteString(`) IN `)

		switch ex.Right.ValType {
		case qcode.ValVar:
			ctx.WriteString(`(SELECT TRY_CAST(x AS `)
			ctx.WriteString(d.snowflakeCastType(d.baseType(ex.Left.Col.Type)))
			ctx.WriteString(`) FROM UNNEST(CAST(json(`)
			ctx.AddParam(Param{Name: ex.Right.Val, Type: "json", IsArray: true})
			ctx.WriteString(`) AS `)
			ctx.WriteString(d.snowflakeArrayCastType(d.baseType(ex.Left.Col.Type)))
			ctx.WriteString(`)) AS __gj_r(x))`)
		case qcode.ValList:
			ctx.WriteString(`(`)
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
			ctx.WriteString(`(SELECT TRY_CAST(__gj_r."x" AS `)
			ctx.WriteString(d.snowflakeCastType(d.baseType(ex.Left.Col.Type)))
			ctx.WriteString(`) FROM UNNEST(`)
			d.renderOperand(ctx, ex.Right.Col.Table, ex.Right.Table, ex.Right.ID, ex.Right.Col.Name, ex.Right.ColName)
			ctx.WriteString(`) AS __gj_r("x"))`)
		}

		ctx.WriteString(`)`)
		return true
	}

	if ex.Op == qcode.OpRegex || ex.Op == qcode.OpIRegex || ex.Op == qcode.OpNotRegex || ex.Op == qcode.OpNotIRegex {
		if ex.Op == qcode.OpNotRegex || ex.Op == qcode.OpNotIRegex {
			ctx.WriteString(`(NOT `)
		} else {
			ctx.WriteString(`(`)
		}

		ctx.WriteString(`regexp_matches(`)
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
			ctx.WriteString(`, 'i'`)
		}

		ctx.WriteString(`))`)
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
			ctx.WriteString(`json_extract(`)
			d.renderOperand(ctx, ex.Left.Col.Table, ex.Left.Table, ex.Left.ID, ex.Left.Col.Name, ex.Left.ColName)
			ctx.WriteString(`, '$.`)
			ctx.WriteString(strings.ReplaceAll(key, `'`, `''`))
			ctx.WriteString(`') IS NOT NULL`)
		}
		ctx.WriteString(`)`)
		return true
	}

	if (ex.Op == qcode.OpIn || ex.Op == qcode.OpNotIn) && ex.Right.Col.Array && ex.Right.Col.Name != "" {
		if ex.Op == qcode.OpNotIn {
			ctx.WriteString(`(NOT `)
		} else {
			ctx.WriteString(`(`)
		}

		ctx.WriteString(`EXISTS (SELECT 1 FROM UNNEST(`)
		d.renderOperand(ctx, ex.Right.Col.Table, ex.Right.Table, ex.Right.ID, ex.Right.Col.Name, ex.Right.ColName)
		ctx.WriteString(`) AS __gj_flat("value") WHERE TRY_CAST(__gj_flat."value" AS `)
		ctx.WriteString(d.snowflakeCastType(ex.Left.Col.Type))
		ctx.WriteString(`) = `)
		d.renderOperand(ctx, ex.Left.Col.Table, ex.Left.Table, ex.Left.ID, ex.Left.Col.Name, ex.Left.ColName)
		ctx.WriteString(`))`)

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
	ctx.WriteString(`list_value(`)
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
	ctx.WriteString(`DROP TABLE IF EXISTS _gj_ids; DROP TABLE IF EXISTS _gj_prev_ids; `)
	ctx.WriteString(`CREATE TEMP TABLE _gj_ids (k VARCHAR, id BIGINT); `)
	ctx.WriteString(`CREATE TEMP TABLE _gj_prev_ids (k VARCHAR, id BIGINT); `)
}

func (d *SnowflakeDialect) RenderBegin(ctx Context) {}

func (d *SnowflakeDialect) RenderTeardown(ctx Context) {
	ctx.WriteString(`; DROP TABLE IF EXISTS _gj_prev_ids; DROP TABLE IF EXISTS _gj_ids; `)
}

func (d *SnowflakeDialect) RenderVarDeclaration(ctx Context, name, typeName string) {}

func (d *SnowflakeDialect) RenderIDCapture(ctx Context, varName string) {}

func (d *SnowflakeDialect) RenderVar(ctx Context, name string) {
	ctx.WriteString(`(SELECT id FROM _gj_ids WHERE k = '`)
	ctx.WriteString(name)
	ctx.WriteString(`' ORDER BY id DESC LIMIT 1)`)
}

func (d *SnowflakeDialect) RenderLinearInsert(ctx Context, m *qcode.Mutate, qc *qcode.QCode, varName string, renderColVal func(qcode.MColumn)) {
	ctx.WriteString(`DELETE FROM _gj_prev_ids WHERE k = '`)
	ctx.WriteString(varName)
	ctx.WriteString(`'; `)
	ctx.WriteString(`INSERT INTO _gj_prev_ids (k, id) SELECT '`)
	ctx.WriteString(varName)
	ctx.WriteString(`', `)
	ctx.Quote(m.Ti.PrimaryCol.Name)
	ctx.WriteString(` FROM `)
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

	if m.IsJSON {
		ctx.WriteString(` SELECT `)
	} else {
		ctx.WriteString(` VALUES (`)
	}

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
		ctx.WriteString(`)`)
	}

	ctx.WriteString(`; INSERT INTO _gj_ids (k, id) SELECT '`)
	ctx.WriteString(varName)
	ctx.WriteString(`', `)
	ctx.Quote(m.Ti.PrimaryCol.Name)
	ctx.WriteString(` FROM `)
	d.renderTableRef(ctx, m.Ti.Schema, m.Ti.Name)
	ctx.WriteString(` EXCEPT SELECT '`)
	ctx.WriteString(varName)
	ctx.WriteString(`', id FROM _gj_prev_ids WHERE k = '`)
	ctx.WriteString(varName)
	ctx.WriteString(`'`)
}

func (d *SnowflakeDialect) RenderLinearUpdate(ctx Context, m *qcode.Mutate, qc *qcode.QCode, varName string, renderColVal func(qcode.MColumn), renderWhere func()) {
	ctx.WriteString(`INSERT INTO _gj_ids (k, id) SELECT '`)
	ctx.WriteString(varName)
	ctx.WriteString(`', `)
	ctx.ColWithTable(m.Ti.Name, m.Ti.PrimaryCol.Name)
	ctx.WriteString(` FROM `)
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
		ctx.Quote(m.Ti.PrimaryCol.Name)
		ctx.WriteString(` = `)
		ctx.Quote(m.Ti.PrimaryCol.Name)
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

func (d *SnowflakeDialect) RenderLinearConnect(ctx Context, m *qcode.Mutate, qc *qcode.QCode, varName string, renderFilter func()) {
	// Capture current FK value before updating
	ctx.WriteString(`INSERT INTO _gj_ids (k, id) SELECT '`)
	ctx.WriteString(varName)
	ctx.WriteString(`', `)
	ctx.ColWithTable(m.Ti.Name, m.Rel.Left.Col.Name)
	ctx.WriteString(` FROM `)
	ctx.Quote(m.Ti.Name)
	ctx.WriteString(` WHERE `)
	renderFilter()

	// Find parent mutation to get its captured ID
	var parentVar string
	for id := range m.DependsOn {
		if qc.Mutates[id].Ti.Name == m.Rel.Right.Col.Table {
			parentVar = d.getVarName(qc.Mutates[id])
			break
		}
	}
	if parentVar != "" {
		// Update FK to point to parent (no FROM recordset needed -
		// the parent ID comes from _gj_ids via RenderVar, filter is a literal condition)
		ctx.WriteString(`; UPDATE `)
		ctx.Quote(m.Ti.Name)
		ctx.WriteString(` SET `)
		ctx.Quote(m.Rel.Left.Col.Name)
		ctx.WriteString(` = `)
		d.RenderVar(ctx, parentVar)
		ctx.WriteString(` WHERE `)
		renderFilter()
	}
}

func (d *SnowflakeDialect) RenderLinearDisconnect(ctx Context, m *qcode.Mutate, qc *qcode.QCode, varName string, renderFilter func()) {
	// Capture current FK value before nullifying
	ctx.WriteString(`INSERT INTO _gj_ids (k, id) SELECT '`)
	ctx.WriteString(varName)
	ctx.WriteString(`', `)
	ctx.ColWithTable(m.Ti.Name, m.Rel.Left.Col.Name)
	ctx.WriteString(` FROM `)
	ctx.Quote(m.Ti.Name)
	ctx.WriteString(` WHERE `)
	renderFilter()
	// Set FK to NULL (no FROM recordset needed)
	ctx.WriteString(`; UPDATE `)
	ctx.Quote(m.Ti.Name)
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
	var inStr, inQuote, inComment bool
	var depth int

	for i := 0; i < len(query); i++ {
		ch := query[i]
		if inComment {
			if ch == '\n' {
				inComment = false
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
				inComment = true
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
	ctx.WriteString(`CAST(`)
	val()
	ctx.WriteString(` AS `)
	ctx.WriteString(d.snowflakeCastType(typ))
	ctx.WriteString(`)`)
}

func (d *SnowflakeDialect) RenderMutateToRecordSet(ctx Context, m *qcode.Mutate, n int, renderRoot func()) {
	if n != 0 {
		ctx.WriteString(`, `)
	}

	if m.Array {
		ctx.WriteString(`(SELECT `)
		hasPK := false
		first := true

		for _, col := range m.Cols {
			if !first {
				ctx.WriteString(`, `)
			}
			first = false

			if col.Col.Name == m.Ti.PrimaryCol.Name {
				hasPK = true
			}

			if !col.Col.Array && !d.isJSONLikeType(col.Col.Type) {
				if d.isStringType(col.Col.Type) {
					ctx.WriteString(`json_extract_string(value, '$.`)
					ctx.WriteString(col.FieldName)
					ctx.WriteString(`') AS `)
				} else {
					ctx.WriteString(`TRY_CAST(json_extract(value, '$.`)
					ctx.WriteString(col.FieldName)
					ctx.WriteString(`') AS `)
					ctx.WriteString(d.snowflakeCastType(col.Col.Type))
					ctx.WriteString(`) AS `)
				}
			} else {
				ctx.WriteString(`json_extract(value, '$.`)
				ctx.WriteString(col.FieldName)
				ctx.WriteString(`') AS `)
			}
			ctx.Quote(col.FieldName)
		}

		if !hasPK {
			if !first {
				ctx.WriteString(`, `)
			}
			ctx.WriteString(`json_extract(value, '$.`)
			ctx.WriteString(m.Ti.PrimaryCol.Name)
			ctx.WriteString(`') AS "_gj_pkt"`)
		}

			ctx.WriteString(` FROM `)
			ctx.WriteString(`json_each(`)
			renderRoot()
			if len(m.Path) > 0 {
				ctx.WriteString(`, '$.`)
			ctx.WriteString(strings.Join(m.Path, "."))
			ctx.WriteString(`'`)
		}
		ctx.WriteString(`)) AS t`)
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

			if col.Col.Name == m.Ti.PrimaryCol.Name {
				hasPK = true
			}

			pathPrefix := ""
			if len(m.Path) > 0 {
				pathPrefix = strings.Join(m.Path, ".") + `.`
			}
			if !col.Col.Array && !d.isJSONLikeType(col.Col.Type) {
				if d.isStringType(col.Col.Type) {
					ctx.WriteString(`json_extract_string(`)
					renderRoot()
					ctx.WriteString(`, '$.`)
					ctx.WriteString(pathPrefix)
					ctx.WriteString(col.FieldName)
					ctx.WriteString(`') AS `)
				} else {
					ctx.WriteString(`TRY_CAST(json_extract(`)
					renderRoot()
					ctx.WriteString(`, '$.`)
					ctx.WriteString(pathPrefix)
					ctx.WriteString(col.FieldName)
					ctx.WriteString(`') AS `)
					ctx.WriteString(d.snowflakeCastType(col.Col.Type))
					ctx.WriteString(`) AS `)
				}
			} else {
				ctx.WriteString(`json_extract(`)
				renderRoot()
				ctx.WriteString(`, '$.`)
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
		ctx.WriteString(`CAST(json_extract(`)
		renderRoot()
		ctx.WriteString(`, '$.`)
		if len(m.Path) > 0 {
			ctx.WriteString(strings.Join(m.Path, "."))
			ctx.WriteString(`.`)
		}
		ctx.WriteString(m.Ti.PrimaryCol.Name)
		ctx.WriteString(`') AS BIGINT) AS "_gj_pkt"`)
	}

	ctx.WriteString(` FROM (SELECT 1) AS _gj_dummy) AS t`)
}

func (d *SnowflakeDialect) RequiresNullOnEmptySelect() bool {
	return true
}

func (d *SnowflakeDialect) getVarName(m qcode.Mutate) string {
	return m.Ti.Name + "_" + fmt.Sprintf("%d", m.ID)
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
	case "float", "float4", "float8", "double", "real", "numeric", "decimal", "number":
		return "DOUBLE"
	case "boolean", "bool":
		return "BOOLEAN"
	case "json", "jsonb", "variant", "object", "array":
		return "JSON"
	case "timestamp", "timestamptz", "timestamp without time zone", "timestamp with time zone":
		return "TIMESTAMP"
	case "date":
		return "DATE"
	case "time", "time without time zone", "time with time zone":
		return "TIME"
	case "text", "varchar", "character varying", "string", "uuid", "clob", "nclob":
		return "VARCHAR"
	default:
		// Preserve unknown custom types as upper-case identifier.
		return strings.ToUpper(strings.TrimSpace(t))
	}
}

func (d *SnowflakeDialect) snowflakeArrayCastType(t string) string {
	switch strings.ToLower(strings.TrimSpace(d.baseType(t))) {
	case "int", "integer", "int4", "int8", "bigint", "smallint":
		return "BIGINT[]"
	case "float", "float4", "float8", "double", "real", "numeric", "decimal", "number":
		return "DOUBLE[]"
	case "boolean", "bool":
		return "BOOLEAN[]"
	case "json", "jsonb", "variant", "object", "array":
		return "JSON[]"
	default:
		return "VARCHAR[]"
	}
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
