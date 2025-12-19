package dialect

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

type OracleDialect struct {
	DBVersion       int
	EnableCamelcase bool
}

func (d *OracleDialect) Name() string {
	return "oracle"
}

func (d *OracleDialect) RenderLimit(ctx Context, sel *qcode.Select) {
	if sel.Paging.NoLimit {
		return
	}

	if sel.Singular {
		ctx.WriteString(` FETCH FIRST 1 ROWS ONLY`)
		return
	}

	if sel.Paging.OffsetVar != "" || sel.Paging.Offset != 0 {
		ctx.WriteString(` OFFSET `)
		if sel.Paging.OffsetVar != "" {
			ctx.AddParam(Param{Name: sel.Paging.OffsetVar, Type: "integer"})
		} else {
			ctx.Write(fmt.Sprintf("%d", sel.Paging.Offset))
		}
		ctx.WriteString(` ROWS`)
	}

	if sel.Paging.LimitVar != "" || sel.Paging.Limit != 0 {
		ctx.WriteString(` FETCH NEXT `)
		if sel.Paging.LimitVar != "" {
			ctx.AddParam(Param{Name: sel.Paging.LimitVar, Type: "integer"})
		} else {
			ctx.Write(fmt.Sprintf("%d", sel.Paging.Limit))
		}
		ctx.WriteString(` ROWS ONLY`)
	}
}

func (d *OracleDialect) RenderJSONRoot(ctx Context, sel *qcode.Select) {
	ctx.WriteString(`SELECT JSON_OBJECT(`)
}

func (d *OracleDialect) RenderJSONSelect(ctx Context, sel *qcode.Select) {
	ctx.WriteString(`SELECT JSON_OBJECT(`)
	ctx.RenderJSONFields(sel)
	ctx.WriteString(`) `)
}

func (d *OracleDialect) RenderJSONPlural(ctx Context, sel *qcode.Select) {
	ctx.WriteString(`COALESCE(JSON_ARRAYAGG(`)
	ctx.Quote("__sj_" + strconv.Itoa(int(sel.ID)))
	ctx.WriteString(`.json), '[]')`)
}

func (d *OracleDialect) RenderJSONField(ctx Context, fieldName string, tableAlias string, colName string, isNull bool, isJSON bool) {
	ctx.WriteString(`KEY '`)
	ctx.WriteString(fieldName)
	ctx.WriteString(`' VALUE `)
	if isNull {
		ctx.WriteString(`NULL`)
	} else {
		if tableAlias != "" {
			ctx.Quote(tableAlias)
			ctx.WriteString(`.`)
			ctx.Quote(colName)
		} else {
			ctx.WriteString(colName)
		}
		// Add FORMAT JSON for nested JSON values to prevent double-escaping
		if isJSON {
			ctx.WriteString(` FORMAT JSON`)
		}
	}
}

func (d *OracleDialect) RenderRootTerminator(ctx Context) {
	ctx.WriteString(`) AS "__ROOT" FROM DUAL`)
}

func (d *OracleDialect) RenderBaseTable(ctx Context) {
	ctx.WriteString(`(SELECT 1 FROM DUAL)`)
}

func (d *OracleDialect) RenderJSONRootField(ctx Context, key string, val func()) {
	ctx.WriteString(`KEY '`)
	ctx.WriteString(key)
	ctx.WriteString(`' VALUE `)
	val()
	// Add FORMAT JSON for nested JSON values to prevent double-escaping
	ctx.WriteString(` FORMAT JSON`)
}

func (d *OracleDialect) RenderTableAlias(ctx Context, alias string) {
	ctx.WriteString(` `)
	ctx.Quote(alias)
}

func (d *OracleDialect) RenderLateralJoinClose(ctx Context, alias string) {
	ctx.WriteString(`) `)
	ctx.Quote(alias)
	ctx.WriteString(` ON 1=1`)
}

func (d *OracleDialect) RenderLateralJoin(ctx Context, sel *qcode.Select, multi bool) {
	if sel.Rel.Type == sdata.RelNone && !multi {
		return
	}
	ctx.WriteString(` LEFT OUTER JOIN LATERAL (`)
}

func (d *OracleDialect) RenderJoinTables(ctx Context, sel *qcode.Select) {
	for _, ob := range sel.OrderBy {
		if ob.Var != "" {
			// Oracle: Use JSON_TABLE to parse the order by array
			ctx.WriteString(` JOIN (SELECT "ID", ROWNUM AS "ORD" FROM JSON_TABLE(`)
			ctx.AddParam(Param{Name: ob.Var, Type: "json"})
			ctx.WriteString(`, '$[*]' COLUMNS("ID" `)
			ctx.WriteString(d.oracleType(ob.Col.Type))
			ctx.WriteString(` PATH '$'))) "_GJ_OB_`)
			ctx.WriteString(strings.ToUpper(ob.Col.Name))
			ctx.WriteString(`" ON "_GJ_OB_`)
			ctx.WriteString(strings.ToUpper(ob.Col.Name))
			ctx.WriteString(`"."ID" = `)
			ctx.ColWithTable(ob.Col.Table, ob.Col.Name)
		}
	}
}

func (d *OracleDialect) RenderCursorCTE(ctx Context, sel *qcode.Select) {
	if !sel.Paging.Cursor {
		return
	}
	// Oracle: Parse comma-separated cursor using REGEXP_SUBSTR
	ctx.WriteString(`WITH "__CUR" AS (SELECT `)
	for i, ob := range sel.OrderBy {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.WriteString(`CAST(REGEXP_SUBSTR(`)
		ctx.AddParam(Param{Name: "cursor", Type: "text"})
		ctx.WriteString(`, '[^,]+', 1, `)
		ctx.Write(fmt.Sprintf("%d", i+2)) // Skip first element (ID)
		ctx.WriteString(`) AS `)
		ctx.WriteString(d.oracleType(ob.Col.Type))
		ctx.WriteString(`) AS `)
		if ob.KeyVar != "" && ob.Key != "" {
			ctx.Quote(ob.Col.Name + "_" + ob.Key)
		} else {
			ctx.Quote(ob.Col.Name)
		}
	}
	ctx.WriteString(` FROM DUAL) `)
}

// oracleType converts GraphJin types to Oracle types
func (d *OracleDialect) oracleType(t string) string {
	switch t {
	case "int", "integer", "int4", "int8", "bigint", "smallint":
		return "NUMBER"
	case "float", "float4", "float8", "double", "real", "numeric", "decimal":
		return "NUMBER"
	case "timestamp", "timestamptz", "timestamp without time zone", "timestamp with time zone":
		return "TIMESTAMP"
	case "date":
		return "DATE"
	default:
		return "VARCHAR2(4000)"
	}
}

func (d *OracleDialect) RenderOrderBy(ctx Context, sel *qcode.Select) {
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
			ctx.WriteString(` = '`)
			ctx.WriteString(ob.Key)
			ctx.WriteString(`' THEN `)
		}
		if ob.Var != "" {
			// Reference the join table for dynamic ordering
			ctx.WriteString(`"_GJ_OB_`)
			ctx.WriteString(strings.ToUpper(ob.Col.Name))
			ctx.WriteString(`"."ORD"`)
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

func (d *OracleDialect) RenderDistinctOn(ctx Context, sel *qcode.Select) {
	// Oracle doesn't support DISTINCT ON
}

func (d *OracleDialect) RenderFromEdge(ctx Context, sel *qcode.Select) {
	// Not implemented for Oracle yet
}

func (d *OracleDialect) RenderJSONPath(ctx Context, table, col string, path []string) {
	ctx.WriteString(`JSON_VALUE(`)
	ctx.ColWithTable(table, col)
	ctx.WriteString(`, '$.`)
	ctx.WriteString(strings.Join(path, "."))
	ctx.WriteString(`')`)
}

func (d *OracleDialect) RenderList(ctx Context, ex *qcode.Exp) {
	ctx.WriteString(`(`)
	for i := range ex.Right.ListVal {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		d.RenderLiteral(ctx, ex.Right.ListVal[i], ex.Right.ListType)
	}
	ctx.WriteString(`)`)
}

func (d *OracleDialect) RenderValPrefix(ctx Context, ex *qcode.Exp) bool {
	// Handle array column overlap operations
	// OpHasInCommon is used when comparing array columns to a list
	// It checks if any element in the column's JSON array exists in the provided list
	if ex.Left.Col.Array && (ex.Op == qcode.OpHasInCommon || ex.Op == qcode.OpIn || ex.Op == qcode.OpNotIn) {
		// For Oracle, array columns contain JSON arrays like ["Tag 1", "Tag 2"]
		// We need to check if any element in the column's array exists in the provided list
		if ex.Op == qcode.OpNotIn {
			ctx.WriteString(`(NOT `)
		} else {
			ctx.WriteString(`(`)
		}
		ctx.WriteString(`EXISTS (SELECT 1 FROM JSON_TABLE(`)

		// Render the column
		var table string
		if ex.Left.Table == "" {
			table = ex.Left.Col.Table
		} else {
			table = ex.Left.Table
		}
		ctx.ColWithTable(table, ex.Left.Col.Name)

		ctx.WriteString(`, '$[*]' COLUMNS("VALUE" `)
		// Map the column type
		colType := "VARCHAR2(4000)"
		switch ex.Left.Col.Type {
		case "int", "integer", "int4", "int8", "bigint", "smallint", "number":
			colType = "NUMBER"
		}
		ctx.WriteString(colType)
		ctx.WriteString(` PATH '$')) WHERE "VALUE" IN (`)

		if ex.Right.ValType == qcode.ValVar {
			// Variable list: use JSON_TABLE to unpack
			ctx.WriteString(`SELECT "VALUE" FROM JSON_TABLE(`)
			ctx.AddParam(Param{Name: ex.Right.Val, Type: "json", IsArray: true})
			ctx.WriteString(`, '$[*]' COLUMNS("VALUE" `)
			ctx.WriteString(colType)
			ctx.WriteString(` PATH '$'))`)
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
	return false
}

func (d *OracleDialect) RenderTsQuery(ctx Context, ti sdata.DBTable, ex *qcode.Exp) {
	// Oracle Full Text Search (shorthand)
	ctx.WriteString(`CONTAINS(`)
	for i, col := range ti.FullText {
		if i != 0 {
			ctx.WriteString(` || ' ' || `)
		}
		ctx.ColWithTable(ti.Name, col.Name)
	}
	ctx.WriteString(`, `)
	ctx.AddParam(Param{Name: ex.Right.Val, Type: "text"})
	ctx.WriteString(`, 1) > 0`)
}

func (d *OracleDialect) RenderSearchRank(ctx Context, sel *qcode.Select, f qcode.Field) {
	ctx.WriteString(`SCORE(1)`)
}

func (d *OracleDialect) RenderSearchHeadline(ctx Context, sel *qcode.Select, f qcode.Field) {
	// Not implemented for Oracle yet
}

func (d *OracleDialect) RenderValVar(ctx Context, ex *qcode.Exp, val string) bool {
	if ex.Op == qcode.OpIn || ex.Op == qcode.OpNotIn {
		// Oracle can't bind arrays directly to SQL, use JSON_TABLE to unpack JSON array
		ctx.WriteString(`(SELECT "VALUE" FROM JSON_TABLE(`)
		ctx.AddParam(Param{Name: ex.Right.Val, Type: "json", IsArray: true})
		ctx.WriteString(`, '$[*]' COLUMNS("VALUE" `)
		// Map the column type
		switch ex.Left.Col.Type {
		case "int", "integer", "int4", "int8", "bigint", "smallint", "number":
			ctx.WriteString("NUMBER")
		case "varchar", "varchar2", "text", "character varying":
			ctx.WriteString("VARCHAR2(4000)")
		default:
			ctx.WriteString("VARCHAR2(4000)")
		}
		ctx.WriteString(` PATH '$')))`)
		return true
	}
	return false
}

func (d *OracleDialect) RenderLiteral(ctx Context, val string, valType qcode.ValType) {
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
		ctx.WriteString(`'`)
		ctx.WriteString(val)
		ctx.WriteString(`'`)
	default:
		ctx.Quote(val)
	}
}

func (d *OracleDialect) RenderValArrayColumn(ctx Context, ex *qcode.Exp, table string, pid int32) {
	t := table
	if pid >= 0 {
		t = fmt.Sprintf("%s_%d", table, pid)
	}
	// For Oracle, JSON array columns need to be unpacked using JSON_TABLE
	// The column is typically a CLOB containing JSON array like [1,2,3]
	ctx.WriteString(`(SELECT "VALUE" FROM JSON_TABLE(`)
	ctx.ColWithTable(t, ex.Right.Col.Name)
	ctx.WriteString(`, '$[*]' COLUMNS("VALUE" `)
	// Map the column type
	switch ex.Right.Col.Type {
	case "int", "integer", "int4", "int8", "bigint", "smallint", "number":
		ctx.WriteString("NUMBER")
	case "varchar", "varchar2", "text", "character varying":
		ctx.WriteString("VARCHAR2(4000)")
	default:
		ctx.WriteString("VARCHAR2(4000)")
	}
	ctx.WriteString(` PATH '$')))`)
}

func (d *OracleDialect) RenderOp(op qcode.ExpOp) (string, error) {
	switch op {
	case qcode.OpIn:
		return `IN`, nil
	case qcode.OpNotIn:
		return `NOT IN`, nil
	case qcode.OpLike:
		return `LIKE`, nil
	case qcode.OpNotLike:
		return `NOT LIKE`, nil
	}
	return "", nil
}

func (d *OracleDialect) BindVar(i int) string {
	return fmt.Sprintf(":%d", i)
}

func (d *OracleDialect) UseNamedParams() bool {
	return false
}

func (d *OracleDialect) SupportsLateral() bool {
	return true
}

func (d *OracleDialect) SupportsReturning() bool {
	return false // Oracle supports RETURNING INTO but it's different
}

func (d *OracleDialect) SupportsWritableCTE() bool {
	return false
}

func (d *OracleDialect) SupportsConflictUpdate() bool {
	return true // MERGE INTO
}

func (d *OracleDialect) RenderMutationCTE(ctx Context, m *qcode.Mutate, renderBody func()) {
	// Not implemented
}

func (d *OracleDialect) RenderInsert(ctx Context, m *qcode.Mutate, values func()) {
	ctx.WriteString(`INSERT INTO `)
	ctx.ColWithTable(m.Ti.Schema, m.Ti.Name)
	ctx.WriteString(` (`)
	values()
	ctx.WriteString(`)`)
}

func (d *OracleDialect) RenderUpdate(ctx Context, m *qcode.Mutate, set func(), from func(), where func()) {
	ctx.WriteString(`UPDATE `)
	ctx.ColWithTable(m.Ti.Schema, m.Ti.Name)
	ctx.WriteString(` SET `)
	set()
	ctx.WriteString(` WHERE `)
	where()
}

func (d *OracleDialect) RenderDelete(ctx Context, m *qcode.Mutate, where func()) {
	ctx.WriteString(`DELETE FROM `)
	ctx.ColWithTable(m.Ti.Schema, m.Ti.Name)
	ctx.WriteString(` WHERE `)
	where()
}

func (d *OracleDialect) RenderUpsert(ctx Context, m *qcode.Mutate, insert func(), updateSet func()) {
	// Oracle MERGE INTO
}

func (d *OracleDialect) RenderReturning(ctx Context, m *qcode.Mutate) {
}

func (d *OracleDialect) RenderAssign(ctx Context, col string, val string) {
	ctx.WriteString(col)
	ctx.WriteString(` = `)
	ctx.WriteString(val)
}

func (d *OracleDialect) RenderCast(ctx Context, val func(), typ string) {
	ctx.WriteString(`CAST(`)
	val()
	ctx.WriteString(` AS `)
	ctx.WriteString(typ)
	ctx.WriteString(`)`)
}

func (d *OracleDialect) RenderTryCast(ctx Context, val func(), typ string) {
	switch typ {
	case "boolean", "bool":
		// Oracle doesn't have boolean type, use CASE expression
		ctx.WriteString(`(CASE WHEN `)
		val()
		ctx.WriteString(` = 'true' THEN 1 WHEN `)
		val()
		ctx.WriteString(` = 'false' THEN 0 ELSE NULL END)`)

	case "number", "numeric", "integer", "int":
		// Try to cast to number, return NULL if not valid
		ctx.WriteString(`TO_NUMBER(`)
		val()
		ctx.WriteString(` DEFAULT NULL ON CONVERSION ERROR)`)

	default:
		d.RenderCast(ctx, val, typ)
	}
}

func (d *OracleDialect) RenderSubscriptionUnbox(ctx Context, params []Param, renderInnerSQL func()) {
	// Oracle JSON_TABLE unbox approach
	// SELECT _gj_sub_data."__root" FROM JSON_TABLE(?, '$[*]' COLUMNS (
	//   "col1" TYPE PATH '$[0]', ...
	// )) _gj_sub, LATERAL (...) _gj_sub_data
	
	ctx.WriteString(`SELECT "_GJ_SUB_DATA"."__ROOT" FROM JSON_TABLE(`)
	ctx.WriteString(d.BindVar(1))
	ctx.WriteString(`, '$[*]' COLUMNS (`)
	
	for i, p := range params {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.Quote(p.Name)
		ctx.WriteString(` `)
		// Map types?
		switch p.Type {
		case "integer", "int4", "int8", "bigint":
			ctx.WriteString("NUMBER")
		default:
			ctx.WriteString("VARCHAR2(4000)")
		}
		ctx.WriteString(` PATH '$[`)
		ctx.WriteString(fmt.Sprintf("%d", i))
		ctx.WriteString(`]'`)
	}
	ctx.WriteString(`)) "_GJ_SUB" CROSS APPLY (`)
	renderInnerSQL()
	ctx.WriteString(`) "_GJ_SUB_DATA"`)
}

func (d *OracleDialect) SupportsLinearExecution() bool {
	return true
}

func (d *OracleDialect) RenderIDCapture(ctx Context, name string) {
	ctx.WriteString(` RETURNING "id" INTO `)
	d.RenderVar(ctx, name)
}

func (d *OracleDialect) RenderVar(ctx Context, name string) {
	ctx.WriteString(`v_`)
	ctx.WriteString(name)
}

func (d *OracleDialect) RenderSetup(ctx Context) {
	ctx.WriteString("DECLARE\n")
}

func (d *OracleDialect) RenderBegin(ctx Context) {
	ctx.WriteString("BEGIN\n")
}

func (d *OracleDialect) RenderVarDeclaration(ctx Context, name, typeName string) {
	ctx.WriteString("  v_")
	ctx.WriteString(name)
	ctx.WriteString(" ")
	// Map types?
	// Simplified mapping or pass through if standard SQL
	// GraphJin types: integer, text, boolean...
	switch typeName {
	case "integer", "int4", "int8", "bigint":
		ctx.WriteString("NUMBER")
	case "text", "varchar":
		ctx.WriteString("VARCHAR2(4000)") 
	default:
		ctx.WriteString("VARCHAR2(4000)") // Safe default? Or NUMBER?
	}
	ctx.WriteString(";\n")
}

func (d *OracleDialect) RenderTeardown(ctx Context) {
	ctx.WriteString("END;")
}

func (d *OracleDialect) RenderMutateToRecordSet(ctx Context, m *qcode.Mutate, n int, renderRoot func()) {
	if n != 0 {
		ctx.WriteString(`, `)
	}

	// For Oracle we use JSON_TABLE to convert JSON input to a derived table
	ctx.WriteString(`(SELECT * FROM JSON_TABLE(`)

	if len(m.Path) > 0 {
		ctx.WriteString(`JSON_QUERY(`)
		renderRoot()
		ctx.WriteString(`, '$.`)
		for i, p := range m.Path {
			if i > 0 {
				ctx.WriteString(`.`)
			}
			if d.EnableCamelcase {
				ctx.WriteString(strings.Title(p))
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
		ctx.Quote(col.FieldName)
		ctx.WriteString(` `)

		// Map types for Oracle JSON_TABLE columns
		switch col.Col.Type {
		case "varchar", "character varying", "text", "string", "varchar2":
			ctx.WriteString("VARCHAR2(4000)")
		case "int", "integer", "int4", "int8", "bigint", "smallint", "number":
			ctx.WriteString("NUMBER")
		case "boolean", "bool":
			ctx.WriteString("NUMBER(1)")
		case "float", "double", "numeric", "real":
			ctx.WriteString("NUMBER")
		case "json", "jsonb", "clob":
			ctx.WriteString("CLOB")
		case "timestamp", "timestamptz", "timestamp without time zone", "timestamp with time zone":
			ctx.WriteString("TIMESTAMP")
		case "date":
			ctx.WriteString("DATE")
		default:
			ctx.WriteString("VARCHAR2(4000)")
		}

		ctx.WriteString(` PATH '$.`)
		ctx.WriteString(col.FieldName)
		ctx.WriteString(`'`)
		i++
	}

	if !hasPK {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.Quote(m.Ti.PrimaryCol.Name)
		ctx.WriteString(` NUMBER PATH '$.`)
		ctx.WriteString(m.Ti.PrimaryCol.Name)
		ctx.WriteString(`'`)
	}

	ctx.WriteString(`)) `)
	ctx.Quote("t")
	ctx.WriteString(`)`)
}

func (d *OracleDialect) RenderSetSessionVar(ctx Context, name, value string) bool {
	ctx.WriteString(`DBMS_SESSION.SET_CONTEXT('CLIENTCONTEXT', '`)
	ctx.WriteString(name)
	ctx.WriteString(`', '`)
	ctx.WriteString(value)
	ctx.WriteString(`')`)
	return true
}
