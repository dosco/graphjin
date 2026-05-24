package dialect

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
)

// BigQueryDialect renders GoogleSQL for BigQuery. It intentionally starts from
// the Snowflake warehouse path because both dialects need inline children,
// string JSON results, and linear mutation execution.
type BigQueryDialect struct {
	SnowflakeDialect
}

var _ Dialect = (*BigQueryDialect)(nil)

func (d *BigQueryDialect) Name() string {
	return "bigquery"
}

func (d *BigQueryDialect) QuoteIdentifier(s string) string {
	if d.NameMap != nil {
		if orig, ok := d.NameMap[s]; ok {
			s = orig
		}
	}
	return "`" + strings.ReplaceAll(s, "`", "\\`") + "`"
}

func (d *BigQueryDialect) RenderJSONRoot(ctx Context, sel *qcode.Select) {
	ctx.WriteString(`SELECT TO_JSON_STRING(JSON_OBJECT(`)
}

func (d *BigQueryDialect) RenderJSONSelect(ctx Context, sel *qcode.Select) {
	if sel.Singular && sel.Rel.Type != 0 {
		ctx.WriteString(`SELECT ANY_VALUE(JSON_OBJECT(`)
		ctx.RenderJSONFields(sel)
		ctx.WriteString(`))`)
		return
	}
	ctx.WriteString(`SELECT JSON_OBJECT(`)
	ctx.RenderJSONFields(sel)
	ctx.WriteString(`)`)
}

func (d *BigQueryDialect) RenderJSONPlural(ctx Context, sel *qcode.Select) {
	ctx.WriteString(`COALESCE(ARRAY_AGG(`)
	ctx.Quote("__sj_" + strconv.Itoa(int(sel.ID)))
	ctx.WriteString(`.`)
	ctx.Quote("json")
	ctx.WriteString(`), [])`)
}

func (d *BigQueryDialect) RenderJSONField(ctx Context, fieldName string, tableAlias string, colName string, isNull bool, isJSON bool) {
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

func (d *BigQueryDialect) RenderJSONRootField(ctx Context, key string, val func()) {
	ctx.WriteString(`'`)
	ctx.WriteString(key)
	ctx.WriteString(`', `)
	val()
}

func (d *BigQueryDialect) RenderJSONNullField(ctx Context, fieldName string) {
	ctx.WriteString(`'`)
	ctx.WriteString(fieldName)
	ctx.WriteString(`', NULL`)
}

func (d *BigQueryDialect) RenderJSONNullCursorField(ctx Context, fieldName string) {
	ctx.WriteString(`, '`)
	ctx.WriteString(fieldName)
	ctx.WriteString(`_cursor', NULL`)
}

func (d *BigQueryDialect) RenderJSONRootSuffix(ctx Context) {
	ctx.WriteString(`)`)
}

func (d *BigQueryDialect) RenderChildCursor(ctx Context, renderChild func()) {
	ctx.WriteString(`JSON_VALUE(`)
	renderChild()
	ctx.WriteString(`, '$.cursor')`)
}

func (d *BigQueryDialect) RenderChildValue(ctx Context, sel *qcode.Select, renderChild func()) {
	if sel.Paging.Cursor {
		ctx.WriteString(`JSON_QUERY(`)
		renderChild()
		ctx.WriteString(`, '$.json')`)
		return
	}
	renderChild()
}

func (d *BigQueryDialect) RenderCursorCTE(ctx Context, sel *qcode.Select) {
	if !sel.Paging.Cursor {
		return
	}
	cursorVar := sel.Paging.CursorVar
	if cursorVar == "" {
		cursorVar = "cursor"
	}
	ctx.WriteString(`WITH `)
	ctx.Quote("__cur")
	ctx.WriteString(` AS (SELECT `)
	for i, ob := range sel.OrderBy {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.WriteString(`SAFE_CAST(SPLIT(`)
		ctx.AddParam(Param{Name: cursorVar, Type: "text"})
		ctx.WriteString(`, ',')[SAFE_OFFSET(`)
		ctx.WriteString(strconv.Itoa(i + 1))
		ctx.WriteString(`)] AS `)
		ctx.WriteString(d.bigQueryCastType(ob.Col.Type))
		ctx.WriteString(`) AS `)
		if ob.KeyVar != "" && ob.Key != "" {
			ctx.Quote(ob.Col.Name + "_" + ob.Key)
		} else {
			ctx.Quote(ob.Col.Name)
		}
	}
	ctx.WriteString(`) `)
}

func (d *BigQueryDialect) RenderJoinTables(ctx Context, sel *qcode.Select) {
	for _, ob := range sel.OrderBy {
		if ob.Var == "" {
			continue
		}
		ctx.WriteString(` JOIN (SELECT SAFE_CAST(_gj_value AS `)
		ctx.WriteString(d.bigQueryCastType(ob.Col.Type))
		ctx.WriteString(`) AS id, _gj_off + 1 AS ord FROM UNNEST(JSON_VALUE_ARRAY(PARSE_JSON(`)
		ctx.AddParam(Param{Name: ob.Var, Type: "json"})
		ctx.WriteString(`))) AS _gj_value WITH OFFSET AS _gj_off) AS `)
		ctx.Quote("_gj_ob_" + ob.Col.Name)
		ctx.WriteString(` ON `)
		ctx.Quote("_gj_ob_" + ob.Col.Name)
		ctx.WriteString(`.`)
		ctx.Quote("id")
		ctx.WriteString(` = `)
		ctx.ColWithTable(ob.Col.Table, ob.Col.Name)
	}
}

func (d *BigQueryDialect) RenderFromEdge(ctx Context, sel *qcode.Select) {
	ctx.WriteString(`(SELECT `)
	for i, col := range sel.Ti.Columns {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		if d.isJSONLikeType(col.Type) || col.Array {
			ctx.WriteString(`JSON_QUERY(j, '$.`)
			ctx.WriteString(col.Name)
			ctx.WriteString(`') AS `)
		} else {
			ctx.WriteString(`SAFE_CAST(JSON_VALUE(j, '$.`)
			ctx.WriteString(col.Name)
			ctx.WriteString(`') AS `)
			ctx.WriteString(d.bigQueryCastType(col.Type))
			ctx.WriteString(`) AS `)
		}
		ctx.Quote(col.Name)
	}
	ctx.WriteString(` FROM UNNEST(JSON_QUERY_ARRAY(`)
	ctx.ColWithTable(sel.Rel.Left.Col.Table, sel.Rel.Left.Col.Name)
	ctx.WriteString(`)) AS j) AS `)
	ctx.Quote(sel.Table)
}

func (d *BigQueryDialect) RenderJSONPath(ctx Context, table, col string, path []string) {
	if len(path) == 0 {
		ctx.ColWithTable(table, col)
		return
	}
	ctx.WriteString(`JSON_VALUE(`)
	ctx.ColWithTable(table, col)
	ctx.WriteString(`, '$.`)
	ctx.WriteString(strings.Join(path, "."))
	ctx.WriteString(`')`)
}

func (d *BigQueryDialect) RenderCast(ctx Context, val func(), typ string) {
	target := d.bigQueryCastType(typ)
	if target == "JSON" {
		ctx.WriteString(`PARSE_JSON(`)
		val()
		ctx.WriteString(`)`)
		return
	}
	ctx.WriteString(`CAST(`)
	val()
	ctx.WriteString(` AS `)
	ctx.WriteString(target)
	ctx.WriteString(`)`)
}

func (d *BigQueryDialect) RenderTryCast(ctx Context, val func(), typ string) {
	target := d.bigQueryCastType(typ)
	if target == "JSON" {
		ctx.WriteString(`PARSE_JSON(`)
		val()
		ctx.WriteString(`)`)
		return
	}
	ctx.WriteString(`SAFE_CAST(`)
	val()
	ctx.WriteString(` AS `)
	ctx.WriteString(target)
	ctx.WriteString(`)`)
}

func (d *BigQueryDialect) RenderMutationInput(ctx Context, qc *qcode.QCode) {
	ctx.WriteString(`WITH `)
	ctx.Quote("_sg_input")
	ctx.WriteString(` AS (SELECT PARSE_JSON(`)
	ctx.AddParam(Param{Name: qc.ActionVar, Type: "json"})
	ctx.WriteString(`) AS j)`)
}

func (d *BigQueryDialect) RenderArray(ctx Context, items []string) {
	ctx.WriteString(`[`)
	for i, item := range items {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.WriteString(item)
	}
	ctx.WriteString(`]`)
}

func (d *BigQueryDialect) RenderValVar(ctx Context, ex *qcode.Exp, val string) bool {
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
	ctx.WriteString(`(SELECT SAFE_CAST(_gj_value AS `)
	ctx.WriteString(d.bigQueryCastType(d.baseType(ex.Left.Col.Type)))
	ctx.WriteString(`) FROM UNNEST(JSON_VALUE_ARRAY(PARSE_JSON(`)
	ctx.AddParam(Param{Name: ex.Right.Val, Type: "json", IsArray: true})
	ctx.WriteString(`))) AS _gj_value)`)
	return true
}

func (d *BigQueryDialect) RenderValPrefix(ctx Context, ex *qcode.Exp) bool {
	switch ex.Op {
	case qcode.OpRegex, qcode.OpIRegex, qcode.OpNotRegex, qcode.OpNotIRegex:
		if ex.Op == qcode.OpNotRegex || ex.Op == qcode.OpNotIRegex {
			ctx.WriteString(`NOT `)
		}
		ctx.WriteString(`REGEXP_CONTAINS(CAST(`)
		d.renderBQOperand(ctx, ex.Left.Col.Table, ex.Left.Table, ex.Left.ID, ex.Left.Col.Name, ex.Left.ColName)
		ctx.WriteString(` AS STRING), `)
		if ex.Right.ValType == qcode.ValVar {
			ctx.AddParam(Param{Name: ex.Right.Val, Type: "text"})
		} else {
			pattern := ex.Right.Val
			if ex.Op == qcode.OpIRegex || ex.Op == qcode.OpNotIRegex {
				pattern = "(?i)" + pattern
			}
			ctx.WriteString(`'`)
			ctx.WriteString(strings.ReplaceAll(pattern, `'`, `''`))
			ctx.WriteString(`'`)
		}
		ctx.WriteString(`)`)
		return true
	case qcode.OpILike, qcode.OpNotILike:
		if ex.Op == qcode.OpNotILike {
			ctx.WriteString(`NOT `)
		}
		ctx.WriteString(`(LOWER(CAST(`)
		d.renderBQOperand(ctx, ex.Left.Col.Table, ex.Left.Table, ex.Left.ID, ex.Left.Col.Name, ex.Left.ColName)
		ctx.WriteString(` AS STRING)) LIKE LOWER(`)
		if ex.Right.ValType == qcode.ValVar {
			ctx.AddParam(Param{Name: ex.Right.Val, Type: "text"})
		} else {
			ctx.WriteString(`'`)
			ctx.WriteString(strings.ReplaceAll(ex.Right.Val, `'`, `''`))
			ctx.WriteString(`'`)
		}
		ctx.WriteString(`))`)
		return true
	case qcode.OpHasKey, qcode.OpHasKeyAny, qcode.OpHasKeyAll:
		op := ` OR `
		if ex.Op == qcode.OpHasKeyAll {
			op = ` AND `
		}
		keys := ex.Right.ListVal
		if ex.Op == qcode.OpHasKey && ex.Right.Val != "" {
			keys = []string{ex.Right.Val}
		}
		if len(keys) == 0 {
			return false
		}
		ctx.WriteString(`(`)
		for i, key := range keys {
			if i != 0 {
				ctx.WriteString(op)
			}
			ctx.WriteString(`JSON_QUERY(`)
			d.renderBQOperand(ctx, ex.Left.Col.Table, ex.Left.Table, ex.Left.ID, ex.Left.Col.Name, ex.Left.ColName)
			ctx.WriteString(`, '$.`)
			ctx.WriteString(strings.ReplaceAll(key, `'`, `''`))
			ctx.WriteString(`') IS NOT NULL`)
		}
		ctx.WriteString(`)`)
		return true
	default:
		return false
	}
}

func (d *BigQueryDialect) RenderOp(op qcode.ExpOp) (string, error) {
	switch op {
	case qcode.OpIn:
		return `IN`, nil
	case qcode.OpNotIn:
		return `NOT IN`, nil
	case qcode.OpLike:
		return `LIKE`, nil
	case qcode.OpNotLike:
		return `NOT LIKE`, nil
	case qcode.OpILike, qcode.OpNotILike:
		return "", fmt.Errorf("case-insensitive LIKE should be handled before RenderOp in bigquery")
	case qcode.OpContains, qcode.OpContainedIn, qcode.OpHasInCommon:
		return "", fmt.Errorf("array operator not supported in bigquery yet: %d", op)
	default:
		return "", nil
	}
}

func (d *BigQueryDialect) RenderMutateToRecordSet(ctx Context, m *qcode.Mutate, n int, renderRoot func()) {
	if n != 0 {
		ctx.WriteString(`, `)
	}
	if m.Array {
		ctx.WriteString(`(SELECT `)
		d.renderBigQueryMutationColumns(ctx, m, func(field string) {
			ctx.WriteString(`_gj_f`)
			ctx.WriteString(`, '$.`)
			ctx.WriteString(field)
			ctx.WriteString(`'`)
		})
		ctx.WriteString(` FROM UNNEST(JSON_QUERY_ARRAY(`)
		if len(m.Path) > 0 {
			ctx.WriteString(`JSON_QUERY(PARSE_JSON(`)
			renderRoot()
			ctx.WriteString(`), '$.`)
			ctx.WriteString(strings.Join(m.Path, "."))
			ctx.WriteString(`')`)
		} else {
			ctx.WriteString(`PARSE_JSON(`)
			renderRoot()
			ctx.WriteString(`)`)
		}
		ctx.WriteString(`)) AS _gj_f) AS `)
		ctx.Quote("t")
		return
	}

	ctx.WriteString(`(SELECT `)
	prefix := ""
	if len(m.Path) > 0 {
		prefix = strings.Join(m.Path, ".") + "."
	}
	d.renderBigQueryMutationColumns(ctx, m, func(field string) {
		ctx.WriteString(`PARSE_JSON(`)
		renderRoot()
		ctx.WriteString(`), '$.`)
		ctx.WriteString(prefix)
		ctx.WriteString(field)
		ctx.WriteString(`'`)
	})
	ctx.WriteString(` FROM (SELECT 1) AS _gj_dummy) AS `)
	ctx.Quote("t")
}

func (d *BigQueryDialect) renderBigQueryMutationColumns(ctx Context, m *qcode.Mutate, pathArg func(string)) {
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
		if col.Col.Array || d.isJSONLikeType(col.Col.Type) {
			ctx.WriteString(`JSON_QUERY(`)
			pathArg(col.FieldName)
			ctx.WriteString(`) AS `)
		} else {
			ctx.WriteString(`SAFE_CAST(JSON_VALUE(`)
			pathArg(col.FieldName)
			ctx.WriteString(`) AS `)
			ctx.WriteString(d.bigQueryCastType(col.Col.Type))
			ctx.WriteString(`) AS `)
		}
		ctx.Quote(col.FieldName)
	}
	if !hasPK {
		if !first {
			ctx.WriteString(`, `)
		}
		ctx.WriteString(`SAFE_CAST(JSON_VALUE(`)
		pathArg(m.Ti.PrimaryCol.Name)
		ctx.WriteString(`) AS `)
		ctx.WriteString(d.bigQueryCastType(m.Ti.PrimaryCol.Type))
		ctx.WriteString(`) AS `)
		ctx.Quote("_gj_pkt")
	}
}

func (d *BigQueryDialect) bigQueryCastType(t string) string {
	tt := strings.TrimSpace(t)
	if strings.HasSuffix(tt, "[]") {
		return "ARRAY<" + d.bigQueryCastType(strings.TrimSuffix(tt, "[]")) + ">"
	}
	upper := strings.ToUpper(strings.TrimSpace(tt))
	if strings.HasPrefix(upper, "ARRAY<") {
		return upper
	}
	switch strings.ToLower(strings.TrimSpace(d.baseType(tt))) {
	case "int", "integer", "int4", "int8", "bigint", "smallint", "int64":
		return "INT64"
	case "float", "float4", "float8", "double", "real", "float64":
		return "FLOAT64"
	case "numeric", "decimal", "number", "bignumeric":
		return "NUMERIC"
	case "boolean", "bool":
		return "BOOL"
	case "json", "jsonb", "variant", "object":
		return "JSON"
	case "timestamp", "timestamp without time zone", "timestamp_ntz", "datetime":
		return "TIMESTAMP"
	case "timestamptz", "timestamp with time zone", "timestamp_tz", "timestamp_ltz":
		return "TIMESTAMP"
	case "date":
		return "DATE"
	case "time", "time without time zone", "time with time zone":
		return "TIME"
	case "text", "varchar", "character varying", "string", "uuid", "clob", "nclob":
		return "STRING"
	case "geography":
		return "GEOGRAPHY"
	default:
		return strings.TrimSpace(t)
	}
}

func (d *BigQueryDialect) renderBQOperand(ctx Context, colTable, tableOverride string, id int32, colName, colNameOverride string) {
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
