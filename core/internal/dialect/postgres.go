package dialect

import (
	"fmt"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
	"github.com/dosco/graphjin/core/v3/internal/util"
)

type PostgresDialect struct {
	DBVersion       int
	EnableCamelcase bool
	SecPrefix       []byte
}

func (d *PostgresDialect) Name() string {
	return "postgres"
}

func (d *PostgresDialect) QuoteIdentifier(s string) string {
	return `"` + s + `"`
}

func (d *PostgresDialect) RenderLimit(ctx Context, sel *qcode.Select) {
	switch {
	case sel.Paging.NoLimit:
		break

	case sel.Singular:
		ctx.WriteString(` LIMIT 1`)

	case sel.Paging.LimitVar != "":
		ctx.WriteString(` LIMIT LEAST(`)
		ctx.AddParam(Param{Name: sel.Paging.LimitVar, Type: "integer"})
		ctx.WriteString(`, `)
		ctx.Write(fmt.Sprintf("%d", sel.Paging.Limit))
		ctx.WriteString(`)`)

	default:
		ctx.WriteString(` LIMIT `)
		ctx.Write(fmt.Sprintf("%d", sel.Paging.Limit))
	}

	switch {
	case sel.Paging.OffsetVar != "":
		ctx.WriteString(` OFFSET `)
		ctx.AddParam(Param{Name: sel.Paging.OffsetVar, Type: "integer"})

	case sel.Paging.Offset != 0:
		ctx.WriteString(` OFFSET `)
		ctx.Write(fmt.Sprintf("%d", sel.Paging.Offset))
	}
}

func (d *PostgresDialect) RenderJSONRoot(ctx Context, sel *qcode.Select) {
	ctx.WriteString(`SELECT jsonb_build_object(`)
}

func (d *PostgresDialect) RenderJSONSelect(ctx Context, sel *qcode.Select) {
	ctx.WriteString(`SELECT to_jsonb(__sr_`)
	ctx.Write(fmt.Sprintf("%d", sel.ID))
	ctx.WriteString(`.*) `)

	if sel.Paging.Cursor {
		for i := range sel.OrderBy {
			ctx.WriteString(`- '__cur_`)
			ctx.Write(fmt.Sprintf("%d", i))
			ctx.WriteString(`' `)
		}
	}
}

func (d *PostgresDialect) RenderJSONPlural(ctx Context, sel *qcode.Select) {
	ctx.WriteString(`COALESCE(jsonb_agg(__sj_`)
	ctx.Write(fmt.Sprintf("%d", sel.ID))
	ctx.WriteString(`.json), '[]')`)
}

func (d *PostgresDialect) RenderLateralJoin(ctx Context, sel *qcode.Select, multi bool) {
	if sel.Rel.Type == sdata.RelNone && !multi {
		return
	}
	ctx.WriteString(` LEFT OUTER JOIN LATERAL (`)
}

func (d *PostgresDialect) RenderJoinTables(ctx Context, sel *qcode.Select) {
	for _, ob := range sel.OrderBy {
		if ob.Var != "" {
			ctx.WriteString(` JOIN (SELECT id ::` + ob.Col.Type + `, ord FROM json_array_elements_text(`)
			ctx.AddParam(Param{Name: ob.Var, Type: ob.Col.Type})
			ctx.WriteString(`) WITH ORDINALITY a(id, ord)) AS _gj_ob_` + ob.Col.Name + `(id, ord) USING (id)`)
		}
	}
}

func (d *PostgresDialect) RenderCursorCTE(ctx Context, sel *qcode.Select) {
	if !sel.Paging.Cursor {
		return
	}
	ctx.WriteString(`WITH __cur AS (SELECT `)
	for i, ob := range sel.OrderBy {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.WriteString(`a[`)
		ctx.Write(fmt.Sprintf("%d", i+2))
		ctx.WriteString(`] :: `)
		ctx.WriteString(ob.Col.Type)
		ctx.WriteString(` AS `)
		if ob.KeyVar != "" && ob.Key != "" {
			ctx.Quote(ob.Col.Name + "_" + ob.Key)
		} else {
			ctx.Quote(ob.Col.Name)
		}
	}
	ctx.WriteString(` FROM STRING_TO_ARRAY(`)
	ctx.AddParam(Param{Name: "cursor", Type: "text"})
	ctx.WriteString(`, ',') AS a) `)
}

func (d *PostgresDialect) RenderOrderBy(ctx Context, sel *qcode.Select) {
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
			// ctx.squoted(ob.Key) // TODO: How to quote value?
			ctx.WriteString(fmt.Sprintf("'%s'", ob.Key)) // Simple quote for now, careful with injections but these are keys
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
			ctx.WriteString(` DESC NULLLS FIRST`)
		case qcode.OrderAscNullsLast:
			ctx.WriteString(` ASC NULLS LAST`)
		case qcode.OrderDescNullsLast:
			ctx.WriteString(` DESC NULLS LAST`)
		}
	}
}

func (d *PostgresDialect) RenderDistinctOn(ctx Context, sel *qcode.Select) {
	if len(sel.DistinctOn) == 0 {
		return
	}
	ctx.WriteString(`DISTINCT ON (`)
	for i, col := range sel.DistinctOn {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.ColWithTable(sel.Table, col.Name)
	}
	ctx.WriteString(`) `)
}

func (d *PostgresDialect) RenderFromEdge(ctx Context, sel *qcode.Select) {
	// jsonb_to_recordset
	ctx.WriteString(sel.Ti.Type)
	ctx.WriteString(`_to_recordset(`)
	ctx.ColWithTable(sel.Rel.Left.Col.Table, sel.Rel.Left.Col.Name)
	ctx.WriteString(`) AS `)
	ctx.Quote(sel.Table)

	ctx.WriteString(`(`)
	for i, col := range sel.Ti.Columns {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.WriteString(col.Name)
		ctx.WriteString(` `)
		ctx.WriteString(col.Type)
	}
	ctx.WriteString(`)`)
}

func (d *PostgresDialect) RenderJSONPath(ctx Context, table, col string, path []string) {
	ctx.ColWithTable(table, col)
	// PostgreSQL JSON path syntax: column->'path1'->>'path2'
	for i, pathElement := range path {
		if i == len(path)-1 {
			ctx.WriteString(`->>'`)
		} else {
			ctx.WriteString(`->'`)
		}
		ctx.WriteString(pathElement)
		ctx.WriteString(`'`)
	}
}

func (d *PostgresDialect) RenderList(ctx Context, ex *qcode.Exp) {
	if strings.HasPrefix(ex.Left.Col.Type, "json") {
		ctx.WriteString(`(ARRAY[`)
		d.renderListBodyPostgres(ctx, ex)
		ctx.WriteString(`])`)
	} else {
		ctx.WriteString(`(CAST(ARRAY[`)
		d.renderListBodyPostgres(ctx, ex)
		ctx.WriteString(`] AS `)
		ctx.WriteString(ex.Left.Col.Type)
		ctx.WriteString(`[]))`)
	}
}

func (d *PostgresDialect) renderListBodyPostgres(ctx Context, ex *qcode.Exp) {
	for i := range ex.Right.ListVal {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		switch ex.Right.ListType {
		case qcode.ValBool, qcode.ValNum:
			ctx.WriteString(ex.Right.ListVal[i])
		case qcode.ValStr:
			ctx.WriteString(`'`)
			ctx.WriteString(ex.Right.ListVal[i])
			ctx.WriteString(`'`)
		}
	}
}

func (d *PostgresDialect) RenderValPrefix(ctx Context, ex *qcode.Exp) bool {
	return false
}

func (d *PostgresDialect) RenderTsQuery(ctx Context, ti sdata.DBTable, ex *qcode.Exp) {
	ctx.WriteString(`((`)
	for i, col := range ti.FullText {
		if i != 0 {
			ctx.WriteString(` OR (`)
		}
		ctx.ColWithTable(ti.Name, col.Name)
		if d.DBVersion >= 110000 {
			ctx.WriteString(`) @@ websearch_to_tsquery(`)
		} else {
			ctx.WriteString(`) @@ to_tsquery(`)
		}
		ctx.AddParam(Param{Name: ex.Right.Val, Type: "text"})
		ctx.WriteString(`)`)
	}
	ctx.WriteString(`)`)
}

func (d *PostgresDialect) RenderSearchRank(ctx Context, sel *qcode.Select, f qcode.Field) {
	ctx.WriteString(`ts_rank(`)
	for i, col := range sel.Ti.FullText {
		if i != 0 {
			ctx.WriteString(` || `)
		}
		ctx.ColWithTable(sel.Table, col.Name)
	}
	if d.DBVersion >= 110000 {
		ctx.WriteString(`, websearch_to_tsquery(`)
	} else {
		ctx.WriteString(`, to_tsquery(`)
	}
	arg, _ := sel.GetInternalArg("search")
	ctx.AddParam(Param{Name: arg.Val, Type: "text"})
	ctx.WriteString(`))`)
}

func (d *PostgresDialect) RenderSearchHeadline(ctx Context, sel *qcode.Select, f qcode.Field) {
	ctx.WriteString(`ts_headline(`)
	ctx.ColWithTable(sel.Table, f.Col.Name)
	if d.DBVersion >= 110000 {
		ctx.WriteString(`, websearch_to_tsquery(`)
	} else {
		ctx.WriteString(`, to_tsquery(`)
	}
	arg, _ := sel.GetInternalArg("search")
	ctx.AddParam(Param{Name: arg.Val, Type: "text"})
	ctx.WriteString(`))`)
}

func (d *PostgresDialect) RenderValVar(ctx Context, ex *qcode.Exp, val string) bool {
	if ex.Op == qcode.OpIn || ex.Op == qcode.OpNotIn || ex.Op == qcode.OpContains || ex.Op == qcode.OpHasInCommon {
		ctx.WriteString(`(ARRAY(SELECT json_array_elements_text(`)
		ctx.AddParam(Param{Name: ex.Right.Val, Type: ex.Left.Col.Type, IsArray: true})
		ctx.WriteString(`))`)
		ctx.WriteString(` :: `)
		ctx.WriteString(ex.Left.Col.Type)
		ctx.WriteString(`[])`)
		return true
	}
	return false
}

func (d *PostgresDialect) RenderLiteral(ctx Context, val string, valType qcode.ValType) {
	switch valType {
	case qcode.ValBool, qcode.ValNum:
		ctx.WriteString(val)
	default:
		// Default to single-quoted string literal (not double-quoted identifier)
		ctx.WriteString(`'`)
		ctx.WriteString(val)
		ctx.WriteString(`'`)
	}
}

func (d *PostgresDialect) RenderBooleanEqualsTrue(ctx Context, paramName string) {
	ctx.WriteString(`(`)
	ctx.AddParam(Param{Name: paramName, Type: "boolean"})
	ctx.WriteString(` IS TRUE)`)
}

func (d *PostgresDialect) RenderBooleanNotEqualsTrue(ctx Context, paramName string) {
	ctx.WriteString(`(`)
	ctx.AddParam(Param{Name: paramName, Type: "boolean"})
	ctx.WriteString(` IS NOT TRUE)`)
}

func (d *PostgresDialect) RenderJSONField(ctx Context, fieldName string, tableAlias string, colName string, isNull bool, isJSON bool) {
	ctx.WriteString(`'`)
	ctx.WriteString(fieldName)
	ctx.WriteString(`', `)
	if isNull {
		ctx.WriteString(`NULL`)
	} else {
		if tableAlias != "" {
			ctx.WriteString(tableAlias)
			ctx.WriteString(`.`)
		}
		ctx.Quote(colName)
	}
	// Postgres handles nested JSON automatically with jsonb_build_object
}

func (d *PostgresDialect) RenderValArrayColumn(ctx Context, ex *qcode.Exp, table string, pid int32) {
	if pid == -1 {
		ctx.ColWithTable(table, ex.Right.Col.Name)
	} else {
	// ctx.ColWithTableID not available in Context interface directly? 
		// Context has ColWithTable(table, col).
		// psql had colWithTableID.
		// I should check Context interface.
		// Context interface has only ColWithTable(table, col).
		// But I can construct the table name with ID manually if needed or update Context interface.
		// psql.colWithTableID logic: if id >= 0 { quoted(table + "_" + val) } else { quoted(table) }
		
		// Let's assume passed 'table' is already the table name or alias we want?
		// No, psql passes 'table' (schema.table) and 'pid'.
		// I should probably update Context interface or helper.
		// But wait, psql/exp.go line 428 calls c.colWithTableID.
		
		// For now, let's just replicate the logic if possible or trust the table arg.
		// The caller in exp.go `renderValArrayColumn` passes `table` and `pid`.
		// It calls `c.colWithTableID(table, pid, col.Name)`.
		
		// I'll replicate simple string construction here or better, add ColWithTableID to Context?
		// Modifying Context implies modifying psql/query.go impl of Context.
		// Let's try to do it with existing methods if possible.
		// `ColWithTable` takes table and col.
		
		t := table
		if pid >= 0 {
			t = fmt.Sprintf("%s_%d", table, pid)
		}
		ctx.ColWithTable(t, ex.Right.Col.Name)
	}
}

func (d *PostgresDialect) RenderArray(ctx Context, items []string) {
	ctx.WriteString(`ARRAY [`)
	for i, item := range items {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.WriteString(item)
	}
	ctx.WriteString(`]`)
}

func (d *PostgresDialect) RenderOp(op qcode.ExpOp) (string, error) {
	switch op {
	case qcode.OpContains:
		return `@>`, nil
	case qcode.OpContainedIn:
		return `<@`, nil
	case qcode.OpHasInCommon:
		return `&&`, nil
	case qcode.OpHasKey:
		return `?`, nil
	case qcode.OpHasKeyAny:
		return `?|`, nil
	case qcode.OpHasKeyAll:
		return `?&`, nil
	case qcode.OpIn:
		return `= ANY`, nil
	case qcode.OpNotIn:
		return `!= ALL`, nil
	case qcode.OpLike, qcode.OpILike: // Postgres ILIKE is handled by key word ILIKE, but standard LIKE is case sensitive.
		if op == qcode.OpLike {
			return `LIKE`, nil
		}
		return `ILIKE`, nil
	case qcode.OpNotLike, qcode.OpNotILike:
		if op == qcode.OpNotLike {
			return `NOT LIKE`, nil
		}
		return `NOT ILIKE`, nil
	case qcode.OpSimilar:
		return `SIMILAR TO`, nil
	case qcode.OpNotSimilar:
		return `NOT SIMILAR TO`, nil
	case qcode.OpRegex:
		return `~`, nil
	case qcode.OpNotRegex:
		return `!~`, nil
	case qcode.OpIRegex:
		return `~*`, nil
	case qcode.OpNotIRegex:
		return `!~*`, nil
	}
	return "", nil
}

func (d *PostgresDialect) BindVar(i int) string {
	return fmt.Sprintf("$%d", i)
}

func (d *PostgresDialect) RenderRootTerminator(ctx Context) {
	ctx.WriteString(`) AS "__root"`)
}

func (d *PostgresDialect) RenderBaseTable(ctx Context) {
	ctx.WriteString(`(SELECT true)`)
}

func (d *PostgresDialect) RenderJSONRootField(ctx Context, key string, val func()) {
	ctx.WriteString(`'`)
	ctx.WriteString(key)
	ctx.WriteString(`', `)
	val()
}

func (d *PostgresDialect) RenderTableAlias(ctx Context, alias string) {
	ctx.WriteString(` AS `)
	ctx.Quote(alias)
}

func (d *PostgresDialect) RenderLateralJoinClose(ctx Context, alias string) {
	ctx.WriteString(`) AS `)
	ctx.Quote(alias)
	ctx.WriteString(` ON true`)
}

func (d *PostgresDialect) UseNamedParams() bool {
	return true
}

func (d *PostgresDialect) SupportsLateral() bool {
	return true
}

// RenderInlineChild is not used for PostgreSQL since it supports LATERAL joins
func (d *PostgresDialect) RenderInlineChild(ctx Context, renderer InlineChildRenderer, psel, sel *qcode.Select) {
	// PostgreSQL uses LATERAL joins, so this is not called
}

func (d *PostgresDialect) SupportsReturning() bool {
	return true
}

func (d *PostgresDialect) SupportsWritableCTE() bool {
	return true
}

func (d *PostgresDialect) SupportsConflictUpdate() bool {
	return true
}

func (d *PostgresDialect) SupportsSubscriptionBatching() bool {
	return true
}

func (d *PostgresDialect) RenderMutationCTE(ctx Context, m *qcode.Mutate, renderBody func()) {
	if m.Multi {
		ctx.WriteString(m.Ti.Name)
		ctx.WriteString(`_`)
		ctx.Write(fmt.Sprintf("%d", m.ID))
	} else {
		ctx.Quote(m.Ti.Name)
	}
	ctx.WriteString(` AS (`)
	renderBody()
	ctx.WriteString(`)`)
}

func (d *PostgresDialect) RenderInsert(ctx Context, m *qcode.Mutate, values func()) {
	ctx.WriteString(`INSERT INTO `)
	ctx.ColWithTable(m.Ti.Schema, m.Ti.Name)
	ctx.WriteString(` (`)
	values()
	ctx.WriteString(`)`)
}

func (d *PostgresDialect) RenderUpdate(ctx Context, m *qcode.Mutate, set func(), from func(), where func()) {
	ctx.WriteString(`UPDATE `)
	ctx.ColWithTable(m.Ti.Schema, m.Ti.Name)
	ctx.WriteString(` SET `)
	set()
	if from != nil {
		ctx.WriteString(` FROM `)
		from()
	}
	ctx.WriteString(` WHERE `)
	where()
}

func (d *PostgresDialect) RenderDelete(ctx Context, m *qcode.Mutate, where func()) {
	ctx.WriteString(`DELETE FROM `)
	ctx.ColWithTable(m.Ti.Schema, m.Ti.Name)
	ctx.WriteString(` WHERE `)
	where()
}

func (d *PostgresDialect) RenderUpsert(ctx Context, m *qcode.Mutate, insert func(), updateSet func()) {
	insert()
	ctx.WriteString(` ON CONFLICT (`)
	
	i := 0
	for _, col := range m.Cols {
		if !col.Col.UniqueKey && !col.Col.PrimaryKey {
			continue
		}
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.WriteString(col.Col.Name)
		i++
	}
	// Fallback to primary key if no unique keys found in cols
	// This mirrors psql/mutate.go behavior
	// But we need access to Ti.PrimaryCol
	if i == 0 {
		ctx.WriteString(m.Ti.PrimaryCol.Name)
	}
	ctx.WriteString(`) DO UPDATE SET `)
	updateSet()
}

func (d *PostgresDialect) RenderReturning(ctx Context, m *qcode.Mutate) {
	ctx.WriteString(` RETURNING `)
	ctx.ColWithTable(m.Ti.Schema, m.Ti.Name)
	ctx.WriteString(`.*`)
}

func (d *PostgresDialect) RenderAssign(ctx Context, col string, val string) {
	ctx.WriteString(col)
	ctx.WriteString(` = `)
	ctx.WriteString(val)
}

func (d *PostgresDialect) RenderCast(ctx Context, val func(), typ string) {
	val()
	ctx.WriteString(` :: `)
	ctx.WriteString(typ)
}

func (d *PostgresDialect) RenderTryCast(ctx Context, val func(), typ string) {
	switch typ {
	case "boolean", "bool":
		ctx.WriteString(`(CASE WHEN `)
		val()
		ctx.WriteString(` = 'true' THEN true WHEN `)
		val()
		ctx.WriteString(` = 'false' THEN false ELSE NULL END)`)

	case "number", "numeric":
		ctx.WriteString(`(CASE WHEN `)
		val()
		ctx.WriteString(` ~ '^[-+]?[0-9]*\.?[0-9]+([eE][-+]?[0-9]+)?$' THEN `)
		d.RenderCast(ctx, val, "numeric")
		ctx.WriteString(` ELSE NULL END)`)

	default:
		d.RenderCast(ctx, val, typ)
	}
}

func (d *PostgresDialect) RenderSubscriptionUnbox(ctx Context, params []Param, innerSQL string) {
	ctx.WriteString(`WITH _gj_sub AS (SELECT `)
	for i, p := range params {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.WriteString(`CAST(x->>`)
		ctx.Write(fmt.Sprintf("%d", i))
		ctx.WriteString(` AS `)
		ctx.WriteString(p.Type)
		ctx.WriteString(`) AS "` + p.Name + `"`)
	}
	ctx.WriteString(` FROM json_array_elements($1::json) AS x`)
	ctx.WriteString(`) SELECT _gj_sub_data.__root FROM _gj_sub LEFT OUTER JOIN LATERAL (`)

	ctx.WriteString(innerSQL)

	ctx.WriteString(`) AS _gj_sub_data ON true`)
}

func (d *PostgresDialect) SupportsLinearExecution() bool {
	return false
}

func (d *PostgresDialect) RenderMutationInput(ctx Context, qc *qcode.QCode) {
	ctx.WriteString(`WITH `)
	ctx.Quote("_sg_input")
	ctx.WriteString(` AS (SELECT `)
	ctx.AddParam(Param{Name: qc.ActionVar, Type: "json"})
	ctx.WriteString(` :: json AS j)`)
}

func (d *PostgresDialect) RenderMutationPostamble(ctx Context, qc *qcode.QCode) {
	GenericRenderMutationPostamble(ctx, qc)
}

func (d *PostgresDialect) RenderLinearInsert(ctx Context, m *qcode.Mutate, qc *qcode.QCode, varName string, renderColVal func(qcode.MColumn)) {
	// Not supported in Postgres yet
}

func (d *PostgresDialect) RenderLinearUpdate(ctx Context, m *qcode.Mutate, qc *qcode.QCode, varName string, renderColVal func(qcode.MColumn), renderWhere func()) {
	// Not supported in Postgres yet
}

func (d *PostgresDialect) RenderLinearConnect(ctx Context, m *qcode.Mutate, qc *qcode.QCode, varName string, renderFilter func()) {
	// Not supported in Postgres yet
}

func (d *PostgresDialect) RenderLinearDisconnect(ctx Context, m *qcode.Mutate, qc *qcode.QCode, varName string, renderFilter func()) {
	// Not supported in Postgres yet
}


func (d *PostgresDialect) RenderIDCapture(ctx Context, varName string) {
}

func (d *PostgresDialect) RenderVar(ctx Context, name string) {
	// Not used for Postgres
}

func (d *PostgresDialect) RenderSetup(ctx Context) {}
func (d *PostgresDialect) RenderBegin(ctx Context) {}
func (d *PostgresDialect) RenderTeardown(ctx Context) {}
func (d *PostgresDialect) RenderVarDeclaration(ctx Context, name, typeName string) {}
func (d *PostgresDialect) RenderMutateToRecordSet(ctx Context, m *qcode.Mutate, n int, renderRoot func()) {
	if n != 0 {
		ctx.WriteString(`, `)
	}
	if m.Array {
		ctx.WriteString(`json_to_recordset`)
	} else {
		ctx.WriteString(`json_to_record`)
	}

	ctx.WriteString(`(`)
	// For Postgres, we expect joinPath to start with the root object.
	// But `renderRoot` typically renders the source (e.g. i.j).
	// joinPathPostgres expects `prefix`.
	// Let's modify joinPathPostgres or how we call it?
	// If `renderRoot` renders `i.j`, then we can't pass it as string prefix to joinPath.
	
	// Option A: RenderRoot into a buffer? No.
	// Option B: Change joinPath to accept func?
	// Option C: Let `renderRoot` handle the first part, joinPath handles the rest?
	
	// `joinPathPostgres` writes `prefix` then loops path.
	// We can pass empty prefix to joinPathPostgres and call renderRoot first.
	renderRoot()
	joinPathPostgres(ctx, "", m.Path, d.EnableCamelcase)

	ctx.WriteString(`) as t(`)

	i := 0
	for _, col := range m.Cols {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.Quote(col.FieldName)
		ctx.WriteString(` `)
		ctx.WriteString(col.Col.Type)
		i++
	}
	ctx.WriteString(`)`)
}


// RenderSetSessionVar renders the SQL to set a session variable in Postgres
func (d *PostgresDialect) RenderSetSessionVar(ctx Context, name, value string) bool {
	ctx.WriteString(`SET SESSION "`)
	ctx.WriteString(name)
	ctx.WriteString(`" = '`)
	ctx.WriteString(value)
	ctx.WriteString(`'`)
	return true
}

// Helper to join path for Postgres
func joinPathPostgres(ctx Context, prefix string, path []string, enableCamelcase bool) {
	ctx.WriteString(prefix)
	for i := range path {
		ctx.WriteString(`->`)
		ctx.WriteString(`'`)
		if enableCamelcase {
			ctx.WriteString(util.ToCamel(path[i]))
		} else {
			ctx.WriteString(path[i])
		}
		ctx.WriteString(`'`)
	}
}
func (d *PostgresDialect) RenderTableName(ctx Context, sel *qcode.Select, schema, table string) {
	if schema != "" {
		ctx.Quote(schema)
		ctx.WriteString(`.`)
	}
	ctx.Quote(table)
}

func (d *PostgresDialect) ModifySelectsForMutation(qc *qcode.QCode) {}

func (d *PostgresDialect) RenderQueryPrefix(ctx Context, qc *qcode.QCode) {}

func (d *PostgresDialect) SplitQuery(query string) (parts []string) { return []string{query} }

func (d *PostgresDialect) RenderChildCursor(ctx Context, renderChild func()) {}

func (d *PostgresDialect) RenderChildValue(ctx Context, sel *qcode.Select, renderChild func()) {
	renderChild()
}

// Role Statement rendering
func (d *PostgresDialect) RoleSelectPrefix() string {
	return `(SELECT (CASE`
}

func (d *PostgresDialect) RoleLimitSuffix() string {
	return `) AS _sg_auth_roles_query LIMIT 1) `
}

func (d *PostgresDialect) RoleDummyTable() string {
	return `ELSE 'anon' END) FROM (VALUES (1)) AS _sg_auth_filler LIMIT 1; `
}

func (d *PostgresDialect) TransformBooleanLiterals(match string) string {
	return match // PostgreSQL uses true/false natively
}

// Driver Behavior
func (d *PostgresDialect) RequiresJSONAsString() bool {
	return false // PostgreSQL driver handles json.RawMessage properly
}

func (d *PostgresDialect) RequiresLowercaseIdentifiers() bool {
	return false // PostgreSQL doesn't require lowercase identifiers
}

func (d *PostgresDialect) RequiresBooleanAsInt() bool {
	return false // PostgreSQL has native boolean support
}

// Recursive CTE Syntax
func (d *PostgresDialect) RequiresRecursiveKeyword() bool {
	return true // PostgreSQL uses WITH RECURSIVE
}

func (d *PostgresDialect) RequiresRecursiveCTEColumnList() bool {
	return false // PostgreSQL doesn't require explicit column list
}

func (d *PostgresDialect) RenderRecursiveOffset(ctx Context) {
	ctx.WriteString(` OFFSET 1`)
}

func (d *PostgresDialect) RenderRecursiveLimit1(ctx Context) {
	ctx.WriteString(` LIMIT 1`)
}

func (d *PostgresDialect) WrapRecursiveSelect() bool {
	return false // PostgreSQL doesn't need extra wrapping
}

func (d *PostgresDialect) RenderRecursiveAnchorWhere(ctx Context, psel *qcode.Select, ti sdata.DBTable, pkCol string) bool {
	return false // PostgreSQL supports outer scope correlation in CTEs
}

// JSON Null Fields
func (d *PostgresDialect) RenderJSONNullField(ctx Context, fieldName string) {
	ctx.WriteString(`'`)
	ctx.WriteString(fieldName)
	ctx.WriteString(`', NULL`)
}

func (d *PostgresDialect) RenderJSONNullCursorField(ctx Context, fieldName string) {
	ctx.WriteString(`, '`)
	ctx.WriteString(fieldName)
	ctx.WriteString(`_cursor', NULL`)
}

func (d *PostgresDialect) RenderJSONRootSuffix(ctx Context) {
	// PostgreSQL doesn't need any suffix
}

// Array Operations
func (d *PostgresDialect) RenderArraySelectPrefix(ctx Context) {
	ctx.WriteString(`ARRAY(SELECT `)
}

func (d *PostgresDialect) RenderArraySelectSuffix(ctx Context) {
	ctx.WriteString(`)`)
}

func (d *PostgresDialect) RenderArrayAggPrefix(ctx Context, distinct bool) {
	if distinct {
		ctx.WriteString(`ARRAY_AGG(DISTINCT `)
	} else {
		ctx.WriteString(`ARRAY_AGG(`)
	}
}

func (d *PostgresDialect) RenderArrayRemove(ctx Context, col string, val func()) {
	ctx.WriteString(` array_remove(`)
	ctx.Quote(col)
	ctx.WriteString(`, `)
	val()
	ctx.WriteString(`)`)
}

// Column rendering
func (d *PostgresDialect) RequiresJSONQueryWrapper() bool {
	return false // PostgreSQL doesn't need JSON_QUERY wrapper
}

func (d *PostgresDialect) RequiresNullOnEmptySelect() bool {
	return false // PostgreSQL doesn't need NULL when no columns rendered
}

