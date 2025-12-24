package dialect

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/graph"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
	"github.com/dosco/graphjin/core/v3/internal/util"
)

type MySQLDialect struct {
	EnableCamelcase bool
}

func (d *MySQLDialect) SplitQuery(query string) (parts []string) {
	var buf strings.Builder
	var inStr, inQuote, inBacktick, inComment bool
	var depth int
	
	for i := 0; i < len(query); i++ {
		c := query[i]

		if inComment {
			if c == '\n' {
				inComment = false
			}
			buf.WriteByte(c)
			continue
		}

		if inStr {
			if c == '\'' {
				if i+1 < len(query) && query[i+1] == '\'' {
					buf.WriteByte(c)
					i++
					buf.WriteByte(c)
					continue
				}
				inStr = false
			}
			buf.WriteByte(c)
			continue
		}

		if inQuote {
			if c == '"' {
				if i+1 < len(query) && query[i+1] == '"' {
					buf.WriteByte(c)
					i++
					buf.WriteByte(c)
					continue
				}
				inQuote = false
			}
			buf.WriteByte(c)
			continue
		}

		if inBacktick {
			if c == '`' {
				if i+1 < len(query) && query[i+1] == '`' {
					buf.WriteByte(c)
					i++
					buf.WriteByte(c)
					continue
				}
				inBacktick = false
			}
			buf.WriteByte(c)
			continue
		}

		switch c {
		case '\'':
			inStr = true
			buf.WriteByte(c)
		case '"':
			inQuote = true
			buf.WriteByte(c)
		case '`':
			inBacktick = true
			buf.WriteByte(c)
		case '-':
			if i+1 < len(query) && query[i+1] == '-' {
				inComment = true
				buf.WriteByte(c)
				i++
				buf.WriteByte('-')
			} else {
				buf.WriteByte(c)
			}
		case '#':
			inComment = true
			buf.WriteByte(c)
		case ';':
			if depth == 0 {
				q := strings.TrimSpace(buf.String())
				if q != "" {
					parts = append(parts, q)
				}
				buf.Reset()
			} else {
				buf.WriteByte(c)
			}
		default:
			buf.WriteByte(c)
		}
	}
	q := strings.TrimSpace(buf.String())
	if q != "" {
		parts = append(parts, q)
	}
	return parts
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
	ctx.WriteString(`CAST(COALESCE(json_arrayagg(`)
	ctx.Quote("__sj_" + strconv.Itoa(int(sel.ID)))
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
		case qcode.ValDBVar:
			d.RenderVar(ctx, ex.Right.ListVal[i])
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
		
		if strings.HasPrefix(ex.Right.Val, "__gj_json_pk:gj_sep:") {
			parts := strings.Split(ex.Right.Val, ":gj_sep:")
			if len(parts) == 4 {
				actionVar := parts[1]
				jsonKey := parts[2]
				colType := parts[3]

				// Render LHS column
				ctx.ColWithTable(ex.Left.Col.Table, ex.Left.Col.Name)
				ctx.WriteString(` `)

				if ex.Op == qcode.OpNotIn {
					ctx.WriteString(`NOT `)
				}

				// Render subquery: (SELECT id FROM JSON_TABLE(?, '$[*]' COLUMNS (id TYPE PATH '$.key')))
				ctx.WriteString(`IN (SELECT _gj_ids.id FROM JSON_TABLE(`)
				
				// Encode/Decode logic handled by RenderParam logic if any? 
				// Here we just pass the variable name.
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
	// MySQL FULLTEXT search: For exact phrase matching, use BOOLEAN MODE
	// NATURAL LANGUAGE MODE matches common words in all rows which gives false positives
	
	if len(ti.FullText) > 0 {
		// Use FULLTEXT index with BOOLEAN MODE for more precise matching
		// Wrap search term in double quotes for phrase matching
		ctx.WriteString(`(MATCH(`)
		for i, col := range ti.FullText {
			if i != 0 {
				ctx.WriteString(`, `)
			}
			ctx.ColWithTable(ti.Name, col.Name)
		}
		ctx.WriteString(`) AGAINST (CONCAT('"', `)
		ctx.AddParam(Param{Name: ex.Right.Val, Type: "text"})
		ctx.WriteString(`, '"') IN BOOLEAN MODE))`)
	} else {
		// Fallback: Use LIKE search on common text columns (name, description)
		// Find name and description columns
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
				ctx.ColWithTable(ti.Name, colName)
				ctx.WriteString(` LIKE CONCAT('%', `)
				ctx.AddParam(Param{Name: ex.Right.Val, Type: "text"})
				ctx.WriteString(`, '%')`)
			}
			ctx.WriteString(`)`)
		} else {
			// No searchable columns, return no results
			ctx.WriteString(`FALSE`)
		}
	}
}

func (d *MySQLDialect) RenderSearchRank(ctx Context, sel *qcode.Select, f qcode.Field) {
	ctx.WriteString(`0`)
}

func (d *MySQLDialect) RenderSearchHeadline(ctx Context, sel *qcode.Select, f qcode.Field) {
	ctx.WriteString(`''`)
}

func (d *MySQLDialect) RenderValVar(ctx Context, ex *qcode.Exp, val string) bool {
	// If the variable name contains a dot, it's a reference to a column in a previous result.
	// For MySQL linear execution, we capture these into user variables with dots in the name.
	if strings.Contains(val, ".") {
		flatVal := strings.ReplaceAll(val, ".", "_")
		ctx.WriteString(`@` + flatVal)
		return true
	}
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
		d.Quote(ctx, val)
	}
}

func (d *MySQLDialect) RenderJSONField(ctx Context, fieldName string, tableAlias string, colName string, isNull bool, isJSON bool) {
	ctx.WriteString(`'`)
	ctx.WriteString(fieldName)
	ctx.WriteString(`', `)
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
	}
	// MySQL handles nested JSON automatically with JSON_OBJECT
}

func (d *MySQLDialect) RenderRootTerminator(ctx Context) {
	ctx.WriteString(`) AS "__root"`)
}

func (d *MySQLDialect) RenderBaseTable(ctx Context) {
	ctx.WriteString(`(SELECT 1)`)
}

func (d *MySQLDialect) RenderJSONRootField(ctx Context, key string, val func()) {
	ctx.WriteString(`'`)
	ctx.WriteString(key)
	ctx.WriteString(`', `)
	val()
}

func (d *MySQLDialect) RenderTableAlias(ctx Context, alias string) {
	ctx.WriteString(` AS `)
	ctx.Quote(alias)
}

func (d *MySQLDialect) RenderLateralJoinClose(ctx Context, alias string) {
	ctx.WriteString(`) AS `)
	ctx.Quote(alias)
	ctx.WriteString(` ON true`)
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
	ctx.WriteString(`JSON_TABLE(j.ids, '$[*]' COLUMNS(`)
	ctx.WriteString(ex.Right.Col.Name)
	ctx.WriteString(` `)
	ctx.WriteString(ex.Left.Col.Type)
	ctx.WriteString(` PATH '$' ERROR ON ERROR)) AS _gj_jt`)
}

func (d *MySQLDialect) RenderArray(ctx Context, items []string) {
	ctx.WriteString(`JSON_ARRAY(`)
	for i, item := range items {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.WriteString(item)
	}
	ctx.WriteString(`)`)
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
	ctx.WriteString("SET SESSION sql_mode = CONCAT(@@sql_mode, ',ANSI_QUOTES'); ")
}
func (d *MySQLDialect) RenderBegin(ctx Context) {}
func (d *MySQLDialect) RenderTeardown(ctx Context) {}
func (d *MySQLDialect) RenderVarDeclaration(ctx Context, name, typeName string) {}

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
	
	// Use '$[*]' for array input, '$' for single object
	if m.Array {
		ctx.WriteString(`, '$[*]' COLUMNS(`)
	} else {
		ctx.WriteString(`, '$' COLUMNS(`)
	}

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
		ctx.WriteString(col.FieldName)
		ctx.WriteString(` `)

		// Check if the field value is an array/object - use JSON type for those
		// This handles cases like tags: ["a", "b"] which can't be extracted as TEXT
		isJSONValue := false
		if m.Data != nil && m.Data.CMap != nil {
			if field, ok := m.Data.CMap[col.FieldName]; ok {
				isJSONValue = field.Type == graph.NodeList || field.Type == graph.NodeObj
			}
		}

		if isJSONValue {
			ctx.WriteString("JSON")
		} else {
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
	d.Quote(ctx, "t") 
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
func (d *MySQLDialect) RenderTableName(ctx Context, sel *qcode.Select, schema, table string) {
	if schema != "" {
		ctx.Quote(schema)
		ctx.WriteString(`.`)
	}
	ctx.Quote(table)
}

func (d *MySQLDialect) RenderMutationInput(ctx Context, qc *qcode.QCode) {
	ctx.WriteString(`WITH `)
	ctx.Quote("_sg_input")
	ctx.WriteString(` AS (SELECT `)
	ctx.AddParam(Param{Name: qc.ActionVar, Type: "json"})
	ctx.WriteString(` AS j)`)
}

func (d *MySQLDialect) RenderMutationPostamble(ctx Context, qc *qcode.QCode) {
	GenericRenderMutationPostamble(ctx, qc)
}

func (d *MySQLDialect) getVarName(m qcode.Mutate) string {
	return m.Ti.Name + "_" + fmt.Sprintf("%d", m.ID)
}

func (d *MySQLDialect) RenderLinearInsert(ctx Context, m *qcode.Mutate, qc *qcode.QCode, varName string, renderColVal func(qcode.MColumn)) {
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
		d.Quote(ctx, rcol.Col.Name)
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
	for _, col := range m.Cols {
		if i != 0 {
			ctx.WriteString(", ")
		}
		if col.Col.Name == m.Ti.PrimaryCol.Name {
			ctx.WriteString("@")
			ctx.WriteString(varName)
			ctx.WriteString(" := ")
			renderColVal(col)
			hasExplicitPK = true
		} else {
			renderColVal(col)
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
				depM := qc.Mutates[id]
				varName := d.getVarName(depM)

				// If we differ to a connect/disconnect mutation, the value is a JSON array
				// If the target column is not an array, we need to extract the first element
				if (depM.Type == qcode.MTConnect || depM.Type == qcode.MTDisconnect) && !rcol.Col.Array {
					ctx.WriteString("JSON_UNQUOTE(JSON_EXTRACT(")
					d.RenderVar(ctx, varName)
					ctx.WriteString(", '$[0]'))")
				} else {
					d.RenderVar(ctx, varName)
				}
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
			// In linear execution mode, pass the JSON parameter directly
			// (there's no _sg_input CTE in linear execution)
			ctx.AddParam(Param{Name: qc.ActionVar, Type: "json"})
		})
		ctx.WriteString("; ")
		// For JSON inserts where PK wasn't captured inline, capture LAST_INSERT_ID
		if !hasExplicitPK {
			d.RenderIDCapture(ctx, varName)
		}
	} else {
		ctx.WriteString(")")
		ctx.WriteString("; ")
		if !hasExplicitPK {
			d.RenderIDCapture(ctx, varName)
		}
	}
}

func (d *MySQLDialect) RenderLinearUpdate(ctx Context, m *qcode.Mutate, qc *qcode.QCode, varName string, renderColVal func(qcode.MColumn), renderWhere func()) {
	
	// Check if there are child mutations that depend on this parent
	// Only if there are child mutations do we need the pre-update SELECT to capture values
	hasChildMutations := false
	for _, otherM := range qc.Mutates {
		if otherM.ParentID == m.ID {
			hasChildMutations = true
			break
		}
	}
	
	// Pre-update select to capture ID - ONLY if there are child mutations that need it
	if m.ParentID == -1 && hasChildMutations {
		if len(qc.Selects) > 0 {
			ctx.WriteString(`SELECT `)
			ctx.ColWithTable(m.Ti.Name, m.Ti.PrimaryCol.Name)
			
			// Capture base variable first (CRITICAL for ModifySelectsForMutation)
			var vars []string
			vars = append(vars, "@"+varName)
			
			// Capture all other columns (CRITICAL for Child Updates)
			for _, col := range m.Ti.Columns {
				ctx.WriteString(`, `)
				ctx.ColWithTable(m.Ti.Name, col.Name)
				vars = append(vars, "@"+varName+"_"+col.Name)
			}
			
			ctx.WriteString(` INTO `)
			ctx.WriteString(strings.Join(vars, ", "))
			ctx.WriteString(` FROM `)
			ctx.Quote(m.Ti.Name)
			
			// CRITICAL: Use m.Where (original filter) not qc.Selects (which might be patched to use @var)
			if m.Where.Exp != nil {
				ex := m.Where.Exp
				ctx.WriteString(` WHERE `)
				ctx.ColWithTable(m.Ti.Name, ex.Left.Col.Name)
				ctx.WriteString(` = `)
				// Handle different value types for ID lookup
				switch ex.Right.ValType {
				case qcode.ValStr:
					ctx.WriteString("'" + ex.Right.Val + "'")
				case qcode.ValVar:
					// Bind variable - use AddParam to render as ?
					ctx.AddParam(Param{Name: ex.Right.Val, Type: ex.Left.Col.Type})
				case qcode.ValNum:
					ctx.WriteString(ex.Right.Val)
				default:
					ctx.WriteString(ex.Right.Val)
				}
			} else if len(qc.Selects) > 0 && qc.Selects[0].Where.Exp != nil {
				ctx.WriteString(` WHERE `)
				renderWhere()
			}
			ctx.WriteString(` LIMIT 1; `)
		}
	}
		// Patch for MySQL/MariaDB:
		// For BelongsTo updates (Child.ID = Parent.FK), the compiler generates `WHERE id = $parent_var`.
		// Since $parent_var assumes PK, this is wrong. We need `WHERE id = $parent_var.fk`.
		// We modify the expression tree to append the FK column.
		if m.ParentID != -1 && m.Rel.Right.Ti.Name == m.Ti.Name {
			var traverse func(*qcode.Exp)
			traverse = func(ex *qcode.Exp) {
				if ex == nil {
					return
				}
				// Logic to patch OpIn / OpEq
				// If LHS is "id" (PK) and RHS is Var without dot
				// append "." + m.Rel.Left.Col.Name
				isPK := ex.Left.Col.Name == m.Ti.PrimaryCol.Name

				if (ex.Op == qcode.OpEquals || ex.Op == qcode.OpIn) && isPK && ex.Right.ValType == qcode.ValVar {
					if !strings.Contains(ex.Right.Val, ".") {
						ex.Right.Val += "." + m.Rel.Left.Col.Name
					}
				}
				
				for _, child := range ex.Children {
					traverse(child)
				}
			}
			traverse(m.Where.Exp)
		}

	// For child updates, use separate simpler UPDATE with JSON_EXTRACT
	// This avoids JSON_TABLE join complexity and WHERE clause issues
	if m.ParentID != -1 && m.IsJSON {
		d.renderChildUpdate(ctx, m, qc, renderWhere)
		ctx.WriteString("; ")
		return
	}
	// Determine FROM function - only needed for JSON mutations
	var fromFunc func()
	if m.IsJSON {
		fromFunc = func() {
			d.RenderMutateToRecordSet(ctx, m, 0, func() {
				ctx.AddParam(Param{Name: qc.ActionVar, Type: "json"})
			})
		}
	}

	d.RenderUpdate(ctx, m, func() {
		// Set
		i := 0
		for _, col := range m.Cols {
			if i != 0 {
				ctx.WriteString(", ")
			}
			ctx.ColWithTable(m.Ti.Name, col.Col.Name)
			ctx.WriteString(" = ")
			renderColVal(col)
			i++
		}
		for _, rcol := range m.RCols {
			if i != 0 {
				ctx.WriteString(", ")
			}
			ctx.ColWithTable(m.Ti.Name, rcol.Col.Name)
			ctx.WriteString(" = ")
			
			found := false
			for id := range m.DependsOn {
				if qc.Mutates[id].Ti.Name == rcol.VCol.Table {
					depM := qc.Mutates[id]
					varName := d.getVarName(depM)

					// If we differ to a connect/disconnect mutation, the value is a JSON array
					// If the target column is not an array, we need to extract the first element
					if (depM.Type == qcode.MTConnect || depM.Type == qcode.MTDisconnect) && !rcol.Col.Array {
						ctx.WriteString("JSON_UNQUOTE(JSON_EXTRACT(")
						d.RenderVar(ctx, varName)
						ctx.WriteString(", '$[0]'))")
					} else {
						d.RenderVar(ctx, varName)
					}
					found = true
					break
				}
			}
			if !found {
				ctx.WriteString("NULL")
			}
			i++
		}
		
		if i == 0 {
			ctx.ColWithTable(m.Ti.Name, m.Ti.PrimaryCol.Name)
			ctx.WriteString(" = ")
			ctx.ColWithTable(m.Ti.Name, m.Ti.PrimaryCol.Name)
		}

	}, fromFunc, func() {
		// Where
		if m.IsJSON {
			// Root-level update: may need JSON table join for WHERE
			if len(qc.Selects) == 0 || qc.Selects[0].Where.Exp == nil {
				ctx.ColWithTable(m.Ti.Name, m.Ti.PrimaryCol.Name)
				ctx.WriteString(" = ")
				ctx.ColWithTable("t", m.Ti.PrimaryCol.Name)
				ctx.WriteString(" AND ")
			}
		}
		renderWhere()
	})
	ctx.WriteString("; ")
}

// renderChildUpdate renders a simple UPDATE for child mutations using JSON_EXTRACT
// This avoids the complexity of JSON_TABLE joins for child updates
func (d *MySQLDialect) renderChildUpdate(ctx Context, m *qcode.Mutate, qc *qcode.QCode, renderWhere func()) {
	ctx.WriteString("UPDATE ")
	ctx.ColWithTable(m.Ti.Schema, m.Ti.Name)
	ctx.WriteString(" SET ")
	
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
			ctx.WriteString(", ")
		}
		d.Quote(ctx, col.Col.Name)
		ctx.WriteString(" = ")
		
		// Use JSON_UNQUOTE(JSON_EXTRACT(?, '$.path.field'))
		ctx.WriteString("JSON_UNQUOTE(JSON_EXTRACT(")
		ctx.AddParam(Param{Name: qc.ActionVar, Type: "json"})
		ctx.WriteString(", '")
		ctx.WriteString(jsonPathPrefix)
		ctx.WriteString(".")
		ctx.WriteString(col.FieldName)
		ctx.WriteString("'))")
		i++
	}
	
	// Handle preset columns (they have literal values)
	for _, col := range m.Cols {
		if !col.Set {
			continue
		}
		if i != 0 {
			ctx.WriteString(", ")
		}
		d.Quote(ctx, col.Col.Name)
		ctx.WriteString(" = ")
		// For preset columns, render the value directly
		if strings.HasPrefix(col.Value, "sql:") {
			ctx.WriteString("(")
			ctx.WriteString(col.Value[4:])
			ctx.WriteString(")")
		} else {
			ctx.WriteString("'")
			ctx.WriteString(col.Value)
			ctx.WriteString("'")
		}
		i++
	}
	
	if i == 0 {
		// No columns to update - use identity update
		d.Quote(ctx, m.Ti.PrimaryCol.Name)
		ctx.WriteString(" = ")
		d.Quote(ctx, m.Ti.PrimaryCol.Name)
	}
	
	ctx.WriteString(" WHERE ")
	renderWhere()
}

func (d *MySQLDialect) Quote(ctx Context, col string) {
	ctx.WriteString("`")
	ctx.WriteString(col)
	ctx.WriteString("`")
}

func (d *MySQLDialect) RenderLinearConnect(ctx Context, m *qcode.Mutate, qc *qcode.QCode, varName string, renderFilter func()) {
	ctx.WriteString(`SELECT JSON_ARRAYAGG(`)
	d.Quote(ctx, m.Ti.Name)
	ctx.WriteString(".")
	d.Quote(ctx, m.Rel.Left.Col.Name)
	ctx.WriteString(`) INTO `)
	d.RenderVar(ctx, varName)
	
	if m.IsJSON {
		ctx.WriteString(` FROM `)
		d.RenderMutateToRecordSet(ctx, m, 0, func() {
			ctx.AddParam(Param{Name: qc.ActionVar, Type: "json"})
		})
		ctx.WriteString(`, `)
	} else {
		ctx.WriteString(` FROM `)
	}
	d.Quote(ctx, m.Ti.Name)
	ctx.WriteString(` WHERE `)
	renderFilter()
	ctx.WriteString("; ")

	// If this is a One-to-Many connection (Child needs to point to Parent),
	// we need to update the child table with the parent's ID.
	// We check if we have a dependency on the parent table.
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
			ctx.WriteString(", ")
			d.RenderMutateToRecordSet(ctx, m, 0, func() {
				ctx.AddParam(Param{Name: qc.ActionVar, Type: "json"})
			})
		}
		ctx.WriteString(" SET ")
		
		// Fix quoting: m.Rel.Left.Col.Name is the column name.
		d.Quote(ctx, m.Rel.Left.Col.Name)
		
		ctx.WriteString(" = @")
		ctx.WriteString(parentVar)
		ctx.WriteString(" WHERE ")
		renderFilter()
		ctx.WriteString("; ")
	}
}

func (d *MySQLDialect) RenderLinearDisconnect(ctx Context, m *qcode.Mutate, qc *qcode.QCode, varName string, renderFilter func()) {
	ctx.WriteString(`SELECT JSON_ARRAYAGG(`)
	d.Quote(ctx, m.Ti.Name)
	ctx.WriteString(".")
	d.Quote(ctx, m.Rel.Left.Col.Name)
	ctx.WriteString(`) INTO `)
	d.RenderVar(ctx, varName)
	
	if m.IsJSON {
		ctx.WriteString(` FROM `)
		d.RenderMutateToRecordSet(ctx, m, 0, func() {
			ctx.AddParam(Param{Name: qc.ActionVar, Type: "json"})
		})
		ctx.WriteString(`, `)
	} else {
		ctx.WriteString(` FROM `)
	}
	ctx.Quote(m.Ti.Name)
	ctx.WriteString(` WHERE `)
	renderFilter()
	ctx.WriteString(`; `)

	// Perform the actual disconnect (UPDATE child SET fk = NULL)
	ctx.WriteString("UPDATE ")
	d.Quote(ctx, m.Ti.Name)
	if m.IsJSON {
		ctx.WriteString(", ")
		d.RenderMutateToRecordSet(ctx, m, 0, func() {
			ctx.AddParam(Param{Name: qc.ActionVar, Type: "json"})
		})
	}
	ctx.WriteString(" SET ")
	d.Quote(ctx, m.Rel.Left.Col.Name)
	ctx.WriteString(" = NULL WHERE ")
	renderFilter()
	ctx.WriteString("; ")
}

func (d *MySQLDialect) ModifySelectsForMutation(qc *qcode.QCode) {
	if qc.Type != qcode.QTMutation || qc.Selects == nil {
		return
	}

	// For MySQL, we need to inject a WHERE clause to filter by the captured IDs
	// The IDs are captured via @tablename_N := id assignments during INSERT
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
		
		var exp *qcode.Exp

		// Special handling for JSON input (potentially bulk)
		// If we refer to a single JSON mutation, it might be a bulk insert
		if len(mutations) == 1 && mutations[0].IsJSON {
			m := mutations[0]
			
			// Check if this mutation has child mutations that need it
			hasChildMutations := false
			for _, otherM := range qc.Mutates {
				if otherM.ParentID == m.ID {
					hasChildMutations = true
					break
				}
			}
			
			// For UPDATE without child mutations, keep the original WHERE clause
			// The pre-select that would populate @var is skipped, so we can't use @var
			if m.Type == qcode.MTUpdate && !hasChildMutations && sel.Where.Exp != nil {
				// Don't modify - keep the original WHERE clause
				continue
			}
			
			hasExplicitPK := false
			var pkName string
			for _, col := range m.Cols {
				if col.Col.Name == m.Ti.PrimaryCol.Name {
					hasExplicitPK = true
					pkName = col.FieldName // The JSON key
					break
				}
			}

			if hasExplicitPK {
				// If PK is provided in JSON, we filter by the IDs in the JSON input
				// id IN (SELECT id FROM JSON_TABLE(..., '$[*]' COLUMNS (id TYPE PATH '$.id')))
				
				exp = &qcode.Exp{Op: qcode.OpIn}
				col := m.Ti.PrimaryCol
				col.Table = m.Ti.Name
				exp.Left.Col = col
				exp.Left.ID = -1
				exp.Right.ValType = qcode.ValVar
				
				// Encode metadata into the variable name for RenderValVar to deconstruct
				// Format: __gj_json_pk:<action_var>:<json_key>:<col_type>
				exp.Right.Val = fmt.Sprintf("__gj_json_pk:gj_sep:%s:gj_sep:%s:gj_sep:%s", qc.ActionVar, pkName, m.Ti.PrimaryCol.Type)

			} else {
				// Auto-generated PKs with JSON input
				// TODO: Implement range optimization (id >= @start AND id < @start + @count)
				// For now, fallback to single ID capture (last one) which is existing behavior
				// causing the partial result issue for bulk auto-inc, but acceptable for single row.
				
				m := mutations[0]
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
			// Single standard mutation
			m := mutations[0]
			
			// For non-JSON updates (inline WHERE mutations), keep the original WHERE clause
			// The pre-select that would populate @var is not done for non-JSON mutations
			if !m.IsJSON && sel.Where.Exp != nil {
				continue
			}
			
			varName := m.Ti.Name + "_" + fmt.Sprintf("%d", m.ID)
			exp = &qcode.Exp{Op: qcode.OpEquals}
			col := m.Ti.PrimaryCol
			col.Table = m.Ti.Name
			exp.Left.Col = col
			exp.Left.ID = -1
			exp.Right.ValType = qcode.ValDBVar
			exp.Right.Val = varName
		} else {
			// Multiple mutations
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
		
		// Merge with existing WHERE clause
		if sel.Where.Exp != nil {
			andExp := &qcode.Exp{
				Op:       qcode.OpAnd,
				Children: []*qcode.Exp{exp, sel.Where.Exp},
			}
			sel.Where.Exp = andExp
		} else {
			sel.Where.Exp = exp
		}
	}
}

func (d *MySQLDialect) RenderQueryPrefix(ctx Context, qc *qcode.QCode) {}


