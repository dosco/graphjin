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

func (d *OracleDialect) QuoteIdentifier(s string) string {
	return `"` + strings.ToUpper(s) + `"`
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
	// But NOT for __typename which is a simple string value, not JSON
	if key != "__typename" {
		ctx.WriteString(` FORMAT JSON`)
	}
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
	ctx.WriteString(`JSON_TABLE(`)
	ctx.ColWithTable(sel.Rel.Left.Col.Table, sel.Rel.Left.Col.Name)
	ctx.WriteString(`, '$[*]' COLUMNS(`)

	for i, col := range sel.Ti.Columns {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.Quote(col.Name)
		ctx.WriteString(` `)
		ctx.WriteString(d.oracleType(col.Type))
		ctx.WriteString(` PATH '$.`)
		ctx.WriteString(col.Name)
		ctx.WriteString(`'`)
	}
	ctx.WriteString(`)) `)
	ctx.Quote(sel.Table)
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
		default:
			ctx.WriteString(ex.Right.ListVal[i])
		}
		ctx.WriteString(` FROM DUAL`)
	}
	ctx.WriteString(`)`)
}

func (d *OracleDialect) RenderValPrefix(ctx Context, ex *qcode.Exp) bool {
	// Handle JSON key existence operations
	if ex.Op == qcode.OpHasKeyAny || ex.Op == qcode.OpHasKeyAll {
		op := " OR "
		if ex.Op == qcode.OpHasKeyAll {
			op = " AND "
		}
		ctx.WriteString(`(`)
		if ex.Right.ValType == qcode.ValVar {
			// Variable case: use JSON_TABLE to iterate keys from the variable array
			// For has_key_all: NOT EXISTS (keys where NOT JSON_EXISTS)
			// For has_key_any: EXISTS (keys where JSON_EXISTS)
			if ex.Op == qcode.OpHasKeyAll {
				ctx.WriteString(`NOT EXISTS (SELECT 1 FROM JSON_TABLE(`)
			} else {
				ctx.WriteString(`EXISTS (SELECT 1 FROM JSON_TABLE(`)
			}
			ctx.AddParam(Param{Name: ex.Right.Val, Type: "json", IsArray: true})
			ctx.WriteString(`, '$[*]' COLUMNS("VALUE" VARCHAR2(4000) PATH '$')) WHERE `)
			if ex.Op == qcode.OpHasKeyAll {
				ctx.WriteString(`NOT `)
			}
			ctx.WriteString(`JSON_EXISTS(`)
			var table string
			if ex.Left.Table == "" {
				table = ex.Left.Col.Table
			} else {
				table = ex.Left.Table
			}
			ctx.ColWithTable(table, ex.Left.Col.Name)
			ctx.WriteString(`, '$."' || "VALUE" || '"'))`)
		} else if ex.Right.ValType == qcode.ValList {
			// Static list case: generate JSON_EXISTS checks for each key
			for i, key := range ex.Right.ListVal {
				if i != 0 {
					ctx.WriteString(op)
				}
				ctx.WriteString(`JSON_EXISTS(`)
				var table string
				if ex.Left.Table == "" {
					table = ex.Left.Col.Table
				} else {
					table = ex.Left.Table
				}
				ctx.ColWithTable(table, ex.Left.Col.Name)
				ctx.WriteString(`, '$.`)
				ctx.WriteString(key)
				ctx.WriteString(`')`)
			}
		}
		ctx.WriteString(`)`)
		return true
	}

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

	// Handle regex operations - Oracle uses REGEXP_LIKE function syntax
	if ex.Op == qcode.OpRegex || ex.Op == qcode.OpIRegex ||
		ex.Op == qcode.OpNotRegex || ex.Op == qcode.OpNotIRegex {
		if ex.Op == qcode.OpNotRegex || ex.Op == qcode.OpNotIRegex {
			ctx.WriteString(`NOT `)
		}
		ctx.WriteString(`REGEXP_LIKE(`)

		// Render the column
		var table string
		if ex.Left.Table == "" {
			table = ex.Left.Col.Table
		} else {
			table = ex.Left.Table
		}
		ctx.ColWithTable(table, ex.Left.Col.Name)

		ctx.WriteString(`, `)
		// Render the pattern value
		if ex.Right.ValType == qcode.ValVar {
			ctx.AddParam(Param{Name: ex.Right.Val, Type: "text"})
		} else {
			ctx.WriteString(`'`)
			ctx.WriteString(ex.Right.Val)
			ctx.WriteString(`'`)
		}

		// Add 'i' flag for case-insensitive
		if ex.Op == qcode.OpIRegex || ex.Op == qcode.OpNotIRegex {
			ctx.WriteString(`, 'i'`)
		}
		ctx.WriteString(`)`)
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
	// Handle special __gj_json_pk format for bulk JSON inserts (explicit PK in JSON)
	if (ex.Op == qcode.OpIn || ex.Op == qcode.OpNotIn) &&
		strings.HasPrefix(ex.Right.Val, "__gj_json_pk:gj_sep:") {

		parts := strings.Split(ex.Right.Val, ":gj_sep:")
		if len(parts) == 4 {
			actionVar := parts[1]
			jsonKey := parts[2]
			colType := parts[3]

			// Render: IN (SELECT "id" FROM JSON_TABLE(:param, '$[*]' COLUMNS("id" TYPE PATH '$.id')))
			ctx.WriteString(`(SELECT `)
			ctx.Quote(jsonKey)
			ctx.WriteString(` FROM JSON_TABLE(`)
			ctx.AddParam(Param{Name: actionVar, Type: "json", WrapInArray: true})
			ctx.WriteString(`, '$[*]' COLUMNS(`)
			ctx.Quote(jsonKey)
			ctx.WriteString(` `)
			ctx.WriteString(d.oracleType(colType))
			ctx.WriteString(` PATH '$.`)
			ctx.WriteString(jsonKey)
			ctx.WriteString(`')))`)
			return true
		}
	}

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

func (d *OracleDialect) RenderBooleanEqualsTrue(ctx Context, paramName string) {
	// Oracle doesn't have native SQL boolean in SQL contexts
	// The Go driver converts Go bool to PL/SQL BOOLEAN which can't be used in SQL
	// Boolean values are converted to int (1/0) in args.go via RequiresBooleanAsInt()
	ctx.WriteString(`(`)
	ctx.AddParam(Param{Name: paramName, Type: "number"})
	ctx.WriteString(` = 1)`)
}

func (d *OracleDialect) RenderBooleanNotEqualsTrue(ctx Context, paramName string) {
	// Oracle doesn't have native SQL boolean in SQL contexts
	// Boolean values are converted to int (1/0) in args.go via RequiresBooleanAsInt()
	ctx.WriteString(`(`)
	ctx.AddParam(Param{Name: paramName, Type: "number"})
	ctx.WriteString(` <> 1)`)
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

// RenderInlineChild is not used for Oracle since it supports LATERAL joins
func (d *OracleDialect) RenderInlineChild(ctx Context, renderer InlineChildRenderer, psel, sel *qcode.Select) {
	// Oracle uses LATERAL joins, so this is not called
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

func (d *OracleDialect) SupportsSubscriptionBatching() bool {
	return true
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
	upperTyp := strings.ToUpper(typ)
	switch upperTyp {
	case "CLOB", "NCLOB", "JSON", "JSONB", "BLOB":
		// For LOB types, just pass the value directly - Oracle driver handles conversion
		// CAST and TO_CLOB don't work well with bind variables in PL/SQL contexts
		val()
	default:
		ctx.WriteString(`CAST(`)
		val()
		ctx.WriteString(` AS `)
		// Oracle's CAST requires size for certain types
		ctx.WriteString(d.castType(typ))
		ctx.WriteString(`)`)
	}
}

// castType converts GraphJin column types to Oracle CAST types
// Oracle's CAST requires size specifications for VARCHAR2, NVARCHAR2, etc.
func (d *OracleDialect) castType(typ string) string {
	upperTyp := strings.ToUpper(typ)
	switch upperTyp {
	case "VARCHAR2", "VARCHAR", "NVARCHAR2", "NVARCHAR":
		return "VARCHAR2(4000)"
	case "CHAR", "NCHAR":
		return "CHAR(255)"
	case "RAW":
		return "RAW(2000)"
	case "NUMBER", "INTEGER", "INT", "BIGINT", "SMALLINT", "FLOAT", "DOUBLE", "REAL", "NUMERIC", "DECIMAL":
		return "NUMBER"
	case "CLOB", "NCLOB", "BLOB":
		return upperTyp
	case "DATE", "TIMESTAMP", "TIMESTAMPTZ":
		return "TIMESTAMP"
	default:
		// If the type already has a size specification (e.g., "VARCHAR2(100)"), use as-is
		if strings.Contains(typ, "(") {
			return typ
		}
		// Default to VARCHAR2(4000) for unknown string types
		return typ
	}
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

func (d *OracleDialect) RenderSubscriptionUnbox(ctx Context, params []Param, innerSQL string) {
	// Oracle subscription batching with cursor CTE extraction
	// The cursor CTE may be nested inside LATERAL joins, so we search for it anywhere in the SQL.
	// We extract it, modify it to reference "_GJ_SUB" instead of DUAL, and move it to the outer level.
	//
	// Structure when cursor CTE exists:
	// WITH "_GJ_SUB" AS (SELECT * FROM JSON_TABLE(...)),
	//      "__CUR" AS (SELECT ... FROM "_GJ_SUB")  -- extracted and modified
	// SELECT "_GJ_SUB_DATA"."__ROOT"
	// FROM "_GJ_SUB" CROSS APPLY (...innerSQL without cursor CTE...) "_GJ_SUB_DATA"

	sql := innerSQL

	// Search for cursor CTE anywhere in the SQL: WITH "__CUR" AS (...)
	cursorCTE := ""
	cursorCTEMarker := `WITH "__CUR" AS (SELECT `
	if cteStart := strings.Index(sql, cursorCTEMarker); cteStart != -1 {
		// Find the end of the cursor CTE - it ends with "FROM DUAL) "
		dualMarker := "FROM DUAL) "
		if dualEnd := strings.Index(sql[cteStart:], dualMarker); dualEnd != -1 {
			cteEnd := cteStart + dualEnd + len(dualMarker)
			cursorCTE = sql[cteStart:cteEnd]
			// Remove the cursor CTE from the SQL, leaving the rest intact
			sql = sql[:cteStart] + sql[cteEnd:]

			// Modify cursor CTE: change FROM DUAL to FROM "_GJ_SUB"
			// This allows the cursor CTE to access the subscription parameters
			cursorCTE = strings.Replace(cursorCTE, "FROM DUAL)", `FROM "_GJ_SUB")`, 1)
			// Remove the WITH prefix since we'll add it ourselves
			cursorCTE = strings.TrimPrefix(cursorCTE, "WITH ")
			// Ensure it ends cleanly (trim trailing space)
			cursorCTE = strings.TrimSuffix(cursorCTE, " ")
		}
	}

	// Build outer WITH clause: "_GJ_SUB" (JSON_TABLE) and optionally "__CUR"
	ctx.WriteString(`WITH "_GJ_SUB" AS (SELECT * FROM JSON_TABLE(`)
	ctx.WriteString(d.BindVar(1))
	ctx.WriteString(`, '$[*]' COLUMNS (`)

	for i, p := range params {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.Quote(p.Name)
		ctx.WriteString(` `)
		// Map types
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
	ctx.WriteString(`)))`)

	// Add cursor CTE if it was extracted
	if cursorCTE != "" {
		ctx.WriteString(`, `)
		ctx.WriteString(cursorCTE)
	}

	// Main query using CROSS APPLY
	ctx.WriteString(` SELECT "_GJ_SUB_DATA"."__ROOT" FROM "_GJ_SUB" CROSS APPLY (`)
	ctx.WriteString(sql)
	ctx.WriteString(`) "_GJ_SUB_DATA"`)
}

func (d *OracleDialect) SupportsLinearExecution() bool {
	return true
}

func (d *OracleDialect) RenderIDCapture(ctx Context, varName string) {
}

func (d *OracleDialect) RenderVar(ctx Context, name string) {
	ctx.WriteString(`v_`)
	ctx.WriteString(name)
}

func (d *OracleDialect) RenderSetup(ctx Context) {
	ctx.WriteString("DECLARE\n")
	// Cursor for returning query results via DBMS_SQL.RETURN_RESULT
	ctx.WriteString("  c SYS_REFCURSOR;\n")
}

func (d *OracleDialect) RenderBegin(ctx Context) {
	ctx.WriteString("BEGIN\n")
	// Set NLS_TIMESTAMP_FORMAT to handle common timestamp formats in JSON input
	ctx.WriteString("  EXECUTE IMMEDIATE 'ALTER SESSION SET NLS_TIMESTAMP_FORMAT = ''YYYY-MM-DD HH24:MI:SS''';\n")
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
	// Return the cursor result set via DBMS_SQL.RETURN_RESULT (Oracle 12c+)
	ctx.WriteString("; DBMS_SQL.RETURN_RESULT(c); END;")
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
		// Note: CLOB is not supported in JSON_TABLE COLUMNS, use VARCHAR2 for JSON/text types
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
			// CLOB and FORMAT JSON not supported in JSON_TABLE COLUMNS for extraction
			// Use VARCHAR2 which can hold up to 4000 chars for typical JSON values
			ctx.WriteString("VARCHAR2(4000)")
		case "timestamp", "timestamptz", "timestamp without time zone", "timestamp with time zone":
			// Extract as VARCHAR2, Oracle will implicitly convert to TIMESTAMP on insert
			ctx.WriteString("VARCHAR2(30)")
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

	ctx.WriteString(`))) `)
	ctx.Quote("t")
}

func (d *OracleDialect) RenderSetSessionVar(ctx Context, name, value string) bool {
	ctx.WriteString(`DBMS_SESSION.SET_CONTEXT('CLIENTCONTEXT', '`)
	ctx.WriteString(name)
	ctx.WriteString(`', '`)
	ctx.WriteString(value)
	ctx.WriteString(`')`)
	return true
}

func (d *OracleDialect) RenderArray(ctx Context, items []string) {
	// Oracle has no direct array literal syntax simple enough for this context, 
	// unless PL/SQL or type constructor.
	// But GraphJin uses JSON mainly.
	// Use JSON_ARRAY(...)
	ctx.WriteString(`JSON_ARRAY(`)
	for i, item := range items {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.WriteString(item)
	}
	ctx.WriteString(`)`)
}
func (d *OracleDialect) RenderTableName(ctx Context, sel *qcode.Select, schema, table string) {
	if schema != "" {
		ctx.Quote(schema)
		ctx.WriteString(`.`)
	}
	ctx.Quote(table)
}

func (d *OracleDialect) RenderMutationInput(ctx Context, qc *qcode.QCode) {
	ctx.WriteString(`WITH "_sg_input" AS (SELECT `)
	ctx.AddParam(Param{Name: qc.ActionVar, Type: "json"})
	ctx.WriteString(` AS j FROM DUAL)`)
}

func (d *OracleDialect) RenderMutationPostamble(ctx Context, qc *qcode.QCode) {
	GenericRenderMutationPostamble(ctx, qc)
}

func (d *OracleDialect) getVarName(m qcode.Mutate) string {
	return m.Ti.Name + "_" + fmt.Sprintf("%d", m.ID)
}

func (d *OracleDialect) RenderLinearInsert(ctx Context, m *qcode.Mutate, qc *qcode.QCode, varName string, renderColVal func(qcode.MColumn)) {
    ctx.WriteString("INSERT INTO ")
	ctx.ColWithTable(m.Ti.Schema, m.Ti.Name)
	ctx.WriteString(" (")
	i := 0
	for _, col := range m.Cols {
		if i != 0 {
			ctx.WriteString(", ")
		}
		ctx.Quote(col.Col.Name)
		i++
	}
	for _, rcol := range m.RCols {
		if i != 0 {
			ctx.WriteString(", ")
		}
		ctx.Quote(rcol.Col.Name)
		i++
	}
	ctx.WriteString(")")

	if m.IsJSON {
		ctx.WriteString(" SELECT ")
	} else {
		ctx.WriteString(" VALUES (")
	}

	i = 0
	hasExplicitPK := false
	pkFieldName := ""
	for _, col := range m.Cols {
		if i != 0 {
			ctx.WriteString(", ")
		}
		renderColVal(col)
		if col.Col.Name == m.Ti.PrimaryCol.Name {
			hasExplicitPK = true
			pkFieldName = col.FieldName
		}
		i++
	}
	for _, rcol := range m.RCols {
		if i != 0 {
			ctx.WriteString(", ")
		}
		found := false
		for id := range m.DependsOn {
			if qc.Mutates[id].Ti.Name == rcol.VCol.Table {
				ctx.WriteString("v_")
				ctx.WriteString(d.getVarName(qc.Mutates[id]))
				found = true
				break
			}
		}
		if !found {
			ctx.WriteString("NULL")
		}
		i++
	}

	if m.IsJSON {
		ctx.WriteString(" FROM ")
		d.RenderMutateToRecordSet(ctx, m, 0, func() {
			// WrapInArray: Oracle JSON_TABLE with '$[*]' path expects array input
			ctx.AddParam(Param{Name: qc.ActionVar, Type: "json", WrapInArray: true})
		})
		ctx.WriteString("; ")

		// For JSON inserts with explicit PK, capture the ID from JSON
		// This is needed for dependent mutations that reference this ID
		if hasExplicitPK && m.Type == qcode.MTInsert {
			ctx.WriteString("SELECT JSON_VALUE(")
			ctx.AddParam(Param{Name: qc.ActionVar, Type: "json"})
			ctx.WriteString(", '$.")
			for _, p := range m.Path {
				ctx.WriteString(p)
				ctx.WriteString(".")
			}
			ctx.WriteString(pkFieldName)
			ctx.WriteString("') INTO v_")
			ctx.WriteString(varName)
			ctx.WriteString(" FROM DUAL")
		}
	} else {
		ctx.WriteString(")")
		// For VALUES inserts: always capture ID using RETURNING INTO
		// Works for both explicit and auto-generated PKs
		if m.Type == qcode.MTInsert {
			ctx.WriteString(` RETURNING `)
			ctx.Quote(m.Ti.PrimaryCol.Name)
			ctx.WriteString(` INTO v_`)
			ctx.WriteString(varName)
		}
	}
}

func (d *OracleDialect) RenderLinearUpdate(ctx Context, m *qcode.Mutate, qc *qcode.QCode, varName string, renderColVal func(qcode.MColumn), renderWhere func()) {
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
			// Oracle SELECT INTO syntax: SELECT col1, col2 INTO var1, var2 FROM table
			ctx.WriteString(`SELECT `)
			ctx.Quote(m.Ti.PrimaryCol.Name)

			// Capture all other columns for FK references
			for _, col := range m.Ti.Columns {
				ctx.WriteString(`, `)
				ctx.Quote(col.Name)
			}

			ctx.WriteString(` INTO v_`)
			ctx.WriteString(varName)
			for _, col := range m.Ti.Columns {
				ctx.WriteString(`, v_`)
				ctx.WriteString(varName + "_" + col.Name)
			}

			ctx.WriteString(` FROM `)
			ctx.Quote(m.Ti.Name)
			ctx.WriteString(` WHERE `)
			renderWhere()
			ctx.WriteString(` AND ROWNUM = 1; `)
		}
	}

	// For child updates with JSON data, use special handling with JSON_VALUE
	if m.ParentID != -1 && m.IsJSON {
		d.renderChildUpdate(ctx, m, qc, renderWhere)
		return
	}

	// Simple UPDATE statement - no FROM clause, no JSON_TABLE join
	ctx.WriteString(`UPDATE `)
	ctx.ColWithTable(m.Ti.Schema, m.Ti.Name)
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
				ctx.WriteString(`v_`)
				ctx.WriteString(d.getVarName(qc.Mutates[id]))
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
func (d *OracleDialect) renderChildUpdate(ctx Context, m *qcode.Mutate, qc *qcode.QCode, renderWhere func()) {
	ctx.WriteString(`UPDATE `)
	ctx.ColWithTable(m.Ti.Schema, m.Ti.Name)
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

		// Use JSON_VALUE(?, '$.path.field') for Oracle
		ctx.WriteString(`JSON_VALUE(`)
		ctx.AddParam(Param{Name: qc.ActionVar, Type: "json"})
		ctx.WriteString(`, '`)
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
		ctx.Quote(m.Ti.PrimaryCol.Name)
		ctx.WriteString(` = `)
		ctx.Quote(m.Ti.PrimaryCol.Name)
	}

	ctx.WriteString(` WHERE `)
	renderWhere()
}

func (d *OracleDialect) RenderLinearConnect(ctx Context, m *qcode.Mutate, qc *qcode.QCode, varName string, renderFilter func()) {
	// Oracle Connect: SELECT INTO for scalar value, or JSON_ARRAYAGG for array columns
	ctx.WriteString(`SELECT `)
	if m.Rel.Right.Col.Array {
		// Array column: aggregate multiple IDs into a JSON array
		ctx.WriteString(`JSON_ARRAYAGG(`)
		ctx.ColWithTable(m.Ti.Name, m.Rel.Left.Col.Name)
		ctx.WriteString(`)`)
	} else {
		ctx.ColWithTable(m.Ti.Name, m.Rel.Left.Col.Name)
	}
	ctx.WriteString(` INTO `)
	d.RenderVar(ctx, varName)

	if m.IsJSON {
		ctx.WriteString(` FROM `)
		d.RenderMutateToRecordSet(ctx, m, 0, func() {
			ctx.AddParam(Param{Name: qc.ActionVar, Type: "json", WrapInArray: true})
		})
		ctx.WriteString(`, `)
	} else {
		ctx.WriteString(` FROM `)
	}
	ctx.Quote(m.Ti.Name)
	ctx.WriteString(` WHERE `)
	renderFilter()

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
		ctx.WriteString("; UPDATE ")
		ctx.Quote(m.Ti.Name)
		ctx.WriteString(" SET ")
		ctx.Quote(m.Rel.Left.Col.Name)
		ctx.WriteString(" = v_")
		ctx.WriteString(parentVar)
		ctx.WriteString(" WHERE ")
		renderFilter()
	}
}

func (d *OracleDialect) RenderLinearDisconnect(ctx Context, m *qcode.Mutate, qc *qcode.QCode, varName string, renderFilter func()) {
	// Step 1: Capture the IDs being disconnected into a variable
	ctx.WriteString(`SELECT JSON_ARRAYAGG(`)
	ctx.ColWithTable(m.Ti.Name, m.Rel.Left.Col.Name)
	ctx.WriteString(`) INTO `)
	d.RenderVar(ctx, varName)

	if m.IsJSON {
		ctx.WriteString(` FROM `)
		d.RenderMutateToRecordSet(ctx, m, 0, func() {
			// WrapInArray: Oracle JSON_TABLE with '$[*]' path expects array input
			ctx.AddParam(Param{Name: qc.ActionVar, Type: "json", WrapInArray: true})
		})
		ctx.WriteString(`, `)
	} else {
		ctx.WriteString(` FROM `)
	}
	ctx.Quote(m.Ti.Name)
	ctx.WriteString(` WHERE `)
	renderFilter()

	// Step 2: Perform the actual disconnect (UPDATE child SET fk = NULL)
	ctx.WriteString(`; UPDATE `)
	ctx.Quote(m.Ti.Name)
	ctx.WriteString(` SET `)
	ctx.Quote(m.Rel.Left.Col.Name)
	ctx.WriteString(` = NULL WHERE `)
	renderFilter()
}


func (d *OracleDialect) ModifySelectsForMutation(qc *qcode.QCode) {
	if qc.Type != qcode.QTMutation || qc.Selects == nil {
		return
	}

	// For Oracle, we need to inject a WHERE clause to filter by the captured IDs
	// The IDs are captured via RETURNING INTO v_tablename_N
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
				// Filter by IDs from JSON: WHERE id IN (SELECT ... FROM JSON_TABLE(...))
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
			// Single non-JSON mutation - filter by id = v_varName
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
			// Multiple mutations - filter by id IN (v_var1, v_var2, ...)
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

func (d *OracleDialect) RenderQueryPrefix(ctx Context, qc *qcode.QCode) {
	// Open cursor for the SELECT query that will be returned via DBMS_SQL.RETURN_RESULT
	ctx.WriteString("OPEN c FOR ")
}

func (d *OracleDialect) SplitQuery(query string) (parts []string) { return []string{query} }

func (d *OracleDialect) RenderChildCursor(ctx Context, renderChild func()) {
	ctx.WriteString("NULL")
}

func (d *OracleDialect) RenderChildValue(ctx Context, sel *qcode.Select, renderChild func()) {
	renderChild()
}

// Role Statement rendering
func (d *OracleDialect) RoleSelectPrefix() string {
	return `(SELECT (CASE`
}

func (d *OracleDialect) RoleLimitSuffix() string {
	// Oracle doesn't support AS for table aliases
	return `) "_SG_AUTH_ROLES_QUERY" FETCH FIRST 1 ROWS ONLY) `
}

func (d *OracleDialect) RoleDummyTable() string {
	return `ELSE 'anon' END) FROM DUAL FETCH FIRST 1 ROWS ONLY; `
}

func (d *OracleDialect) TransformBooleanLiterals(match string) string {
	return match // Oracle uses true/false natively
}

// Driver Behavior
func (d *OracleDialect) RequiresJSONAsString() bool {
	return true // Oracle driver doesn't handle json.RawMessage properly
}

func (d *OracleDialect) RequiresLowercaseIdentifiers() bool {
	return true // Oracle requires lowercase identifiers in configuration
}

func (d *OracleDialect) RequiresBooleanAsInt() bool {
	return true // Oracle's PL/SQL BOOLEAN can't be used in SQL WHERE clauses
}

// Recursive CTE Syntax
func (d *OracleDialect) RequiresRecursiveKeyword() bool {
	return false // Oracle doesn't use RECURSIVE keyword
}

func (d *OracleDialect) RequiresRecursiveCTEColumnList() bool {
	return true // Oracle requires explicit column alias list in recursive CTEs
}

func (d *OracleDialect) RenderRecursiveOffset(ctx Context) {
	ctx.WriteString(` OFFSET 1 ROWS`)
}

func (d *OracleDialect) RenderRecursiveLimit1(ctx Context) {
	ctx.WriteString(` FETCH FIRST 1 ROWS ONLY`)
}

func (d *OracleDialect) WrapRecursiveSelect() bool {
	return false // Oracle doesn't need extra wrapping
}

func (d *OracleDialect) RenderRecursiveAnchorWhere(ctx Context, psel *qcode.Select, ti sdata.DBTable, pkCol string) bool {
	// Oracle doesn't support outer scope correlation in CTEs
	// Instead of correlating with outer table alias, inline the parent's WHERE expression
	if psel.Where.Exp != nil {
		ctx.RenderExp(ti, psel.Where.Exp)
		return true
	}
	return false
}

// JSON Null Fields
func (d *OracleDialect) RenderJSONNullField(ctx Context, fieldName string) {
	ctx.WriteString(`KEY '`)
	ctx.WriteString(fieldName)
	ctx.WriteString(`' VALUE NULL`)
}

func (d *OracleDialect) RenderJSONNullCursorField(ctx Context, fieldName string) {
	ctx.WriteString(`, KEY '`)
	ctx.WriteString(fieldName)
	ctx.WriteString(`_cursor' VALUE NULL`)
}

func (d *OracleDialect) RenderJSONRootSuffix(ctx Context) {
	// Oracle doesn't need any suffix
}

// Array Operations
func (d *OracleDialect) RenderArraySelectPrefix(ctx Context) {
	ctx.WriteString(`(SELECT JSON_ARRAYAGG(`)
}

func (d *OracleDialect) RenderArraySelectSuffix(ctx Context) {
	ctx.WriteString(`))`)
}

func (d *OracleDialect) RenderArrayAggPrefix(ctx Context, distinct bool) {
	if distinct {
		ctx.WriteString(`JSON_ARRAYAGG(DISTINCT `)
	} else {
		ctx.WriteString(`JSON_ARRAYAGG(`)
	}
}

func (d *OracleDialect) RenderArrayRemove(ctx Context, col string, val func()) {
	// Oracle: Use JSON_TABLE to unpack, filter out the value, and re-aggregate
	ctx.WriteString(` (SELECT JSON_ARRAYAGG(j."VALUE") FROM JSON_TABLE(`)
	ctx.Quote(col)
	ctx.WriteString(`, '$[*]' COLUMNS("VALUE" NUMBER PATH '$')) j WHERE j."VALUE" != `)
	val()
	ctx.WriteString(`)`)
}

// Column rendering
func (d *OracleDialect) RequiresJSONQueryWrapper() bool {
	return false // Oracle doesn't need JSON_QUERY wrapper
}

func (d *OracleDialect) RequiresNullOnEmptySelect() bool {
	return true // Oracle needs NULL when no columns rendered to avoid empty JSON_OBJECT()
}
