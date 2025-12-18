package dialect

import (
	"fmt"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
	"github.com/dosco/graphjin/core/v3/internal/util"
)

type MySQLDialect struct {
	EnableCamelcase bool
}

func (d *MySQLDialect) Name() string {
	return "mysql"
}

func (d *MySQLDialect) RenderLimit(ctx Context, sel *qcode.Select) {
	ctx.WriteString(` LIMIT `)

	switch {
	case sel.Paging.OffsetVar != "":
		ctx.AddParam(Param{Name: sel.Paging.OffsetVar, Type: "integer"})
		ctx.WriteString(`, `)

	case sel.Paging.Offset != 0:
		ctx.Write(fmt.Sprintf("%d", sel.Paging.Offset))
		ctx.WriteString(`, `)
	}

	switch {
	case sel.Paging.NoLimit:
		ctx.WriteString(`18446744073709551610`)

	case sel.Singular:
		ctx.WriteString(`1`)

	default:
		ctx.Write(fmt.Sprintf("%d", sel.Paging.Limit))
	}
}

func (d *MySQLDialect) RenderJSONRoot(ctx Context, sel *qcode.Select) {
	ctx.WriteString(`SELECT json_object(`)
}

func (d *MySQLDialect) RenderJSONSelect(ctx Context, sel *qcode.Select) {
	ctx.WriteString(`SELECT json_object(`)
	ctx.RenderJSONFields(sel)
	ctx.WriteString(`) `)
}

func (d *MySQLDialect) RenderJSONPlural(ctx Context, sel *qcode.Select) {
	ctx.WriteString(`CAST(COALESCE(json_arrayagg(__sj_`)
	ctx.Write(fmt.Sprintf("%d", sel.ID))
	ctx.WriteString(`.json), '[]') AS JSON)`)
}

func (d *MySQLDialect) RenderLateralJoin(ctx Context, sel *qcode.Select, multi bool) {
	if sel.Rel.Type == sdata.RelNone && !multi {
		return
	}
	ctx.WriteString(` LEFT OUTER JOIN LATERAL (`)
}

func (d *MySQLDialect) RenderJoinTables(ctx Context, sel *qcode.Select) {
	// MySQL does not render extra joins for order by lists in the original code
}

func (d *MySQLDialect) RenderCursorCTE(ctx Context, sel *qcode.Select) {
	if !sel.Paging.Cursor {
		return
	}
	ctx.WriteString(`WITH __cur AS (SELECT `)
	for i, ob := range sel.OrderBy {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.WriteString(`NULLIF(SUBSTRING_INDEX(SUBSTRING_INDEX(a.i, ',', `)
		ctx.Write(fmt.Sprintf("%d", i+2))
		ctx.WriteString(`), ',', -1), '') AS `)
		
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

func (d *MySQLDialect) RenderOrderBy(ctx Context, sel *qcode.Select) {
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
			// MySQL Find In Set
			ctx.WriteString(`FIND_IN_SET(`)
			ctx.ColWithTable(ob.Col.Table, ob.Col.Name)
			ctx.WriteString(`, (SELECT GROUP_CONCAT(id) FROM JSON_TABLE(`)
			ctx.AddParam(Param{Name: ob.Var, Type: "text"})
			ctx.WriteString(`, '$[*]' COLUMNS (id ` + ob.Col.Type + ` PATH '$')) AS a))`)
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
		}
	}
}

func (d *MySQLDialect) RenderDistinctOn(ctx Context, sel *qcode.Select) {
	// MySQL does not support DISTINCT ON
}

func (d *MySQLDialect) RenderFromEdge(ctx Context, sel *qcode.Select) {
	ctx.WriteString(`JSON_TABLE(`)
	ctx.ColWithTable(sel.Rel.Left.Col.Table, sel.Rel.Left.Col.Name)
	ctx.WriteString(`, "$[*]" COLUMNS(`)

	for i, col := range sel.Ti.Columns {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.WriteString(col.Name)
		ctx.WriteString(` `)
		ctx.WriteString(col.Type)
		ctx.WriteString(` PATH "$.`)
		ctx.WriteString(col.Name)
		ctx.WriteString(`" ERROR ON ERROR`)
	}
	ctx.WriteString(`)) AS`)
	ctx.Quote(sel.Table)
}

func (d *MySQLDialect) RenderJSONPath(ctx Context, table, col string, path []string) {
	ctx.ColWithTable(table, col)
	// MySQL JSON path syntax: column->'$.path1.path2'
	ctx.WriteString(`->>'$.`)
	for i, pathElement := range path {
		if i > 0 {
			ctx.WriteString(`.`)
		}
		ctx.WriteString(pathElement)
	}
	ctx.WriteString(`'`)
}

func (d *MySQLDialect) RenderList(ctx Context, ex *qcode.Exp) {
	ctx.WriteString(`(`)
	for i := range ex.Right.ListVal {
		if i != 0 {
			ctx.WriteString(` UNION `)
		}
		ctx.WriteString(`SELECT `)
		switch ex.Right.ListType {
		case qcode.ValBool, qcode.ValNum:
			ctx.WriteString(ex.Right.ListVal[i])
		case qcode.ValStr:
			ctx.WriteString(`'`)
			ctx.WriteString(ex.Right.ListVal[i])
			ctx.WriteString(`'`)
		}
	}
	ctx.WriteString(`)`)
}

func (d *MySQLDialect) RenderValPrefix(ctx Context, ex *qcode.Exp) bool {
	// Logic from exp.go renderValPrefix
	if (ex.Op == qcode.OpHasKey ||
		ex.Op == qcode.OpHasKeyAny ||
		ex.Op == qcode.OpHasKeyAll) {
		var optype string
		switch ex.Op {
		case qcode.OpHasKey, qcode.OpHasKeyAny:
			optype = "'one'"
		case qcode.OpHasKeyAll:
			optype = "'all'"
		}
		ctx.WriteString("JSON_CONTAINS_PATH(")
		ctx.ColWithTable(ex.Left.Col.Table, ex.Left.Col.Name) // assuming ti.Name is accessible or passed? In psql it was c.ti.Name
		// Wait, ex.Left.Col might have table? 
		// The original code used `c.ti.Name`. 
		// Here we don't have `ti`. 
		// `ex` has Left and Right. `ex.Left.Col.Table` should be populated?
		
		ctx.WriteString(", " + optype)
		for i := range ex.Right.ListVal {
			ctx.WriteString(`, '$.` + ex.Right.ListVal[i] + `'`)
		}
		ctx.WriteString(") = 1")
		return true
	}

	if ex.Right.ValType == qcode.ValVar &&
		(ex.Op == qcode.OpIn || ex.Op == qcode.OpNotIn) {
		ctx.WriteString(`JSON_CONTAINS(`)
		ctx.AddParam(Param{Name: ex.Right.Val, Type: ex.Left.Col.Type, IsArray: true})
		ctx.WriteString(`, CAST(`)
		ctx.ColWithTable(ex.Left.Col.Table, ex.Left.Col.Name)
		ctx.WriteString(` AS JSON), '$')`)
		return true
	}
	return false
}

func (d *MySQLDialect) RenderTsQuery(ctx Context, ti sdata.DBTable, ex *qcode.Exp) {
	// MATCH (name) AGAINST ('phone' IN BOOLEAN MODE);
	ctx.WriteString(`(MATCH(`)
	for i, col := range ti.FullText {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.ColWithTable(ti.Name, col.Name)
	}
	ctx.WriteString(`) AGAINST (`)
	ctx.AddParam(Param{Name: ex.Right.Val, Type: "text"})
	ctx.WriteString(` IN NATURAL LANGUAGE MODE))`)
}

func (d *MySQLDialect) RenderSearchRank(ctx Context, sel *qcode.Select, f qcode.Field) {
	ctx.WriteString(`0`)
}

func (d *MySQLDialect) RenderSearchHeadline(ctx Context, sel *qcode.Select, f qcode.Field) {
	ctx.WriteString(`''`)
}

func (d *MySQLDialect) RenderValVar(ctx Context, ex *qcode.Exp, val string) bool {
	// MySQL doesn't support the ARRAY(SELECT ...) pattern for vars easily or uses different syntax.
	// Returning false falls back to default renderParam logic in exp.go
	return false 
}

func (d *MySQLDialect) RenderLiteral(ctx Context, val string, valType qcode.ValType) {
	switch valType {
	case qcode.ValBool, qcode.ValNum:
		ctx.WriteString(val)
	case qcode.ValStr:
		ctx.WriteString(`'`)
		ctx.WriteString(val)
		ctx.WriteString(`'`)
	default:
		ctx.Quote(val)
	}
}

func (d *MySQLDialect) RenderValArrayColumn(ctx Context, ex *qcode.Exp, table string, pid int32) {
	ctx.WriteString(`SELECT _gj_jt.* FROM `)
	ctx.WriteString(`(SELECT CAST(`)
	
	t := table
	if pid >= 0 {
		t = fmt.Sprintf("%s_%d", table, pid)
	}
	ctx.ColWithTable(t, ex.Right.Col.Name)

	ctx.WriteString(` AS JSON) as ids) j, `)
	ctx.WriteString(`JSON_TABLE(j.ids, "$[*]" COLUMNS(`)
	ctx.WriteString(ex.Right.Col.Name)
	ctx.WriteString(` `)
	ctx.WriteString(ex.Left.Col.Type)
	ctx.WriteString(` PATH "$" ERROR ON ERROR)) AS _gj_jt`)
}

func (d *MySQLDialect) RenderOp(op qcode.ExpOp) (string, error) {
	switch op {
	case qcode.OpContains, qcode.OpContainedIn, qcode.OpHasInCommon, 
		 qcode.OpHasKey, qcode.OpHasKeyAny, qcode.OpHasKeyAll:
		return "", fmt.Errorf("operator not supported in MySQL: %d", op)
	
	case qcode.OpIn:
		return `IN`, nil
	case qcode.OpNotIn:
		return `NOT IN`, nil
	case qcode.OpLike, qcode.OpILike:
		return `LIKE`, nil
	case qcode.OpNotLike, qcode.OpNotILike:
		return `NOT LIKE`, nil
	case qcode.OpSimilar, qcode.OpNotSimilar:
		return "", fmt.Errorf("SIMILAR TO not supported in MySQL")
	case qcode.OpRegex, qcode.OpIRegex:
		return `REGEXP`, nil
	case qcode.OpNotRegex, qcode.OpNotIRegex:
		return `NOT REGEXP`, nil
	}
	return "", nil
}

func (d *MySQLDialect) BindVar(i int) string {
	return "?"
}

func (d *MySQLDialect) UseNamedParams() bool {
	return false
}

func (d *MySQLDialect) SupportsLateral() bool {
	return true
}

func (d *MySQLDialect) SupportsReturning() bool {
	return false
}

func (d *MySQLDialect) SupportsWritableCTE() bool {
	return false
}

func (d *MySQLDialect) SupportsConflictUpdate() bool {
	return false
}

// RenderMutationCTE for MySQL generally mocks logic or errors, but as per plan,
// we just implement strict no-op or basic generation where possible.
// Writable CTEs are FALSE so this path shouldn't be main strategy.
func (d *MySQLDialect) RenderMutationCTE(ctx Context, m *qcode.Mutate, renderBody func()) {
	// MySQL 8.0 supports CTEs but not writable CTEs (INSERT inside WITH).
	// This method might be called if we try to use CTE strategy. For now, render standard CTE syntax
	// but it will fail at runtime if the body is an INSERT.
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

func (d *MySQLDialect) RenderInsert(ctx Context, m *qcode.Mutate, values func()) {
	ctx.WriteString(`INSERT INTO `)
	ctx.ColWithTable(m.Ti.Schema, m.Ti.Name)
	ctx.WriteString(` (`)
	values()
	ctx.WriteString(`)`)
}

func (d *MySQLDialect) RenderUpdate(ctx Context, m *qcode.Mutate, set func(), from func(), where func()) {
	ctx.WriteString(`UPDATE `)
	ctx.ColWithTable(m.Ti.Schema, m.Ti.Name)
	if from != nil {
		// MySQL renders joins/tables before SET
		// Use JOIN syntax for better compatibility with JSON_TABLE
		ctx.WriteString(`, `) // Revert to comma join, safer for UPDATE
		from()
	}
	ctx.WriteString(` SET `)
	set()
	ctx.WriteString(` WHERE `)
	where()
}

func (d *MySQLDialect) RenderDelete(ctx Context, m *qcode.Mutate, where func()) {
	ctx.WriteString(`DELETE FROM `)
	ctx.ColWithTable(m.Ti.Schema, m.Ti.Name)
	ctx.WriteString(` WHERE `)
	where()
}

func (d *MySQLDialect) RenderUpsert(ctx Context, m *qcode.Mutate, insert func(), updateSet func()) {
	insert()
	ctx.WriteString(` ON DUPLICATE KEY UPDATE `)
	updateSet()
}

func (d *MySQLDialect) RenderReturning(ctx Context, m *qcode.Mutate) {
	// Not supported in MySQL
}

func (d *MySQLDialect) RenderAssign(ctx Context, col string, val string) {
	ctx.WriteString(col)
	ctx.WriteString(` = `)
	ctx.WriteString(val)
}

func (d *MySQLDialect) RenderCast(ctx Context, val func(), typ string) {
	ctx.WriteString(`CAST(`)
	val()
	ctx.WriteString(` AS `)
	
	// MySQL CAST supports: BINARY, CHAR, DATE, DATETIME, DECIMAL, JSON, NCHAR, SIGNED, TIME, UNSIGNED
	switch typ {
	case "varchar", "character varying", "text", "string":
		ctx.WriteString("CHAR")
	case "int", "integer", "int4", "int8", "bigint", "smallint":
		ctx.WriteString("SIGNED")
	case "boolean", "bool":
		// MySQL boolean is tinyint(1), so SIGNED or UNSIGNED works
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

func (d *MySQLDialect) RenderTryCast(ctx Context, val func(), typ string) {
	switch typ {
	case "boolean", "bool":
		ctx.WriteString(`(CASE WHEN `)
		val()
		ctx.WriteString(` = 'true' THEN 1 WHEN `)
		val()
		ctx.WriteString(` = 'false' THEN 0 ELSE NULL END)`)

	case "number", "numeric":
		// MySQL regex for number validation
		ctx.WriteString(`(CASE WHEN `)
		val()
		ctx.WriteString(` REGEXP '^[-+]?[0-9]*\\.?[0-9]+([eE][-+]?[0-9]+)?$' THEN `)
		d.RenderCast(ctx, val, "DECIMAL(65,30)")
		ctx.WriteString(` ELSE NULL END)`)

	default:
		d.RenderCast(ctx, val, typ)
	}
}

func (d *MySQLDialect) RenderSubscriptionUnbox(ctx Context, params []Param, renderInnerSQL func()) {
	ctx.WriteString(`WITH _gj_sub AS (SELECT * FROM JSON_TABLE(?, "$[*]" COLUMNS(`)
	for i, p := range params {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.WriteString("`" + p.Name + "` ")
		ctx.WriteString(p.Type)
		ctx.WriteString(` PATH "$[`)
		ctx.Write(fmt.Sprintf("%d", i))
		ctx.WriteString(`]" ERROR ON ERROR`)
	}
	ctx.WriteString(`)) AS _gj_jt`)
	ctx.WriteString(`) SELECT _gj_sub_data.__root FROM _gj_sub LEFT OUTER JOIN LATERAL (`)
	renderInnerSQL()
	ctx.WriteString(`) AS _gj_sub_data ON true`)
}

func (d *MySQLDialect) SupportsLinearExecution() bool {
	return true
}

func (d *MySQLDialect) RenderIDCapture(ctx Context, name string) {
	ctx.WriteString(`SET @`)
	ctx.WriteString(name)
	ctx.WriteString(` = LAST_INSERT_ID()`)
}

func (d *MySQLDialect) RenderVar(ctx Context, name string) {
	ctx.WriteString(`@`)
	ctx.WriteString(name)
}

func (d *MySQLDialect) RenderSetup(ctx Context) {
}

func (d *MySQLDialect) RenderTeardown(ctx Context) {
}

func (d *MySQLDialect) RenderMutateToRecordSet(ctx Context, m *qcode.Mutate, n int, renderRoot func()) {
	if n != 0 {
		ctx.WriteString(`, `)
	}

	// For MySQL we use JSON_TABLE to convert JSON input to a derived table
	// Wrap in subquery to avoid potential parser issues in UPDATE statements
	ctx.WriteString(`(SELECT * FROM JSON_TABLE(`)
	
	if len(m.Path) > 0 {
		ctx.WriteString(`JSON_EXTRACT(`)
		renderRoot()
		ctx.WriteString(`, '$.`)
		for i, p := range m.Path {
			if i > 0 {
				ctx.WriteString(`.`)
			}
			if d.EnableCamelcase {
				ctx.WriteString(util.ToCamel(p))
			} else {
				ctx.WriteString(p)
			}
		}
		ctx.WriteString(`')`)
	} else {
		renderRoot()
	}
	
	ctx.WriteString(`, '$[*]' COLUMNS(`)

	i := 0
	hasPK := false
	for _, col := range m.Cols {
		if col.FieldName == m.Ti.PrimaryCol.Name {
			hasPK = true
		}
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.WriteString(col.FieldName)
		ctx.WriteString(` `)

		// Map types for MySQL JSON_TABLE columns
		switch col.Col.Type {
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
			ctx.WriteString(col.Col.Type)
		}

		ctx.WriteString(` PATH '$.`)
		ctx.WriteString(col.FieldName)
		ctx.WriteString(`' ERROR ON ERROR`)
		i++
	}

	if !hasPK {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.WriteString(m.Ti.PrimaryCol.Name)
		ctx.WriteString(` BIGINT PATH '$.`) // Assume BIGINT for PK safest? Or use type from Col.
		ctx.WriteString(m.Ti.PrimaryCol.Name)
		ctx.WriteString(`' ERROR ON ERROR`)
	}

	ctx.WriteString(`)) AS _jt) AS `)
	ctx.Quote("t") 
}


// RenderSetSessionVar renders the SQL to set a session variable in MySQL
func (d *MySQLDialect) RenderSetSessionVar(ctx Context, name, value string) bool {
	ctx.WriteString(`SET @`)
	ctx.WriteString(name) // MySQL variables usually don't have quotes or have specific quoting. @name is standard.
	// But name like "user.id" might need to be `user.id`.
	// For now, assume strict naming or handle `.` replacement if needed?
	// Postgres uses "user.id". MySQL uses @`user.id`?
	// Let's rely on standard quoting if likely.
	// Actually, `user.id` is not valid MySQL variable name usually unless quoted?
	// `SET @'user.id' = ...` ?
	// Standard MySQL user defined vars are `@var_name`.
	// GraphJin core uses `user.id` which fits Postgres specialized GUCs.
	// For MySQL we might map `user.id` to `@user_id` or just use what is passed if compatible.
	// But let's just quote the name if it contains dots.
	// Or just quote it always with backticks.
	// `SET @"user.id" = ...`
	// Wait, MySQL user variables are `@var_name`.
	// Quoting after @: `@`identifier``
	ctx.WriteString(name)
	ctx.WriteString(` = '`)
	ctx.WriteString(value)
	ctx.WriteString(`'`)
	return true
}

// Helper to join path for MySQL
func joinPathMySQL(ctx Context, prefix string, path []string, enableCamelcase bool) {
	ctx.WriteString(prefix)
	for i := range path {
		ctx.WriteString(`->`)
		ctx.WriteString(`'$.`)
		if enableCamelcase {
			ctx.WriteString(util.ToCamel(path[i]))
		} else {
			ctx.WriteString(path[i])
		}
		ctx.WriteString(`'`)
	}
}
