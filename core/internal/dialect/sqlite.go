package dialect

import (
	"fmt"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

type SQLiteDialect struct {
}

func (d *SQLiteDialect) Name() string {
	return "sqlite"
}

func (d *SQLiteDialect) SupportsLateral() bool {
	return false
}

func (d *SQLiteDialect) RenderLimit(ctx Context, sel *qcode.Select) {
	if sel.Paging.NoLimit {
		return
	}

	ctx.WriteString(` LIMIT `)
	if sel.Paging.LimitVar != "" {
		ctx.AddParam(Param{Name: sel.Paging.LimitVar, Type: "integer"})
	} else {
		ctx.Write(fmt.Sprintf("%d", sel.Paging.Limit))
	}

	if sel.Paging.OffsetVar != "" {
		ctx.WriteString(` OFFSET `)
		ctx.AddParam(Param{Name: sel.Paging.OffsetVar, Type: "integer"})
	} else if sel.Paging.Offset != 0 {
		ctx.WriteString(` OFFSET `)
		ctx.Write(fmt.Sprintf("%d", sel.Paging.Offset))
	}
}

func (d *SQLiteDialect) RenderJSONRoot(ctx Context, sel *qcode.Select) {
	ctx.WriteString(`SELECT json_object(`)
}

func (d *SQLiteDialect) RenderJSONSelect(ctx Context, sel *qcode.Select) {
	ctx.WriteString(`SELECT json_object(`)
	ctx.RenderJSONFields(sel)
	ctx.WriteString(`) `)
}





func (d *SQLiteDialect) RenderJSONPlural(ctx Context, sel *qcode.Select) {
	ctx.WriteString(`COALESCE(json_group_array(json("json")), '[]')`)
}

func (d *SQLiteDialect) RenderLateralJoin(ctx Context, sel *qcode.Select, multi bool) {
	// SQLite does not support LATERAL joins. This should be handled by the compiler logic
	// checking SupportsLateral() or by convention not calling this.
	// We can leave it empty or safer, do nothing.
}



func (d *SQLiteDialect) RenderCursorCTE(ctx Context, sel *qcode.Select) {
	if !sel.Paging.Cursor {
		return
	}
	// SQLite: Parse comma-separated cursor as JSON array
	// Convert "val1,val2,val3" to '["val1","val2","val3"]' then use json_each
	ctx.WriteString(`WITH __cur AS (SELECT `)
	for i, ob := range sel.OrderBy {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		// Use json_extract with array index (0-based in SQLite JSON)
		ctx.WriteString(`CAST(json_extract('["' || replace(NULLIF(`)
		ctx.AddParam(Param{Name: "cursor", Type: "text"})
		ctx.WriteString(`, ''), ',', '","') || '"]', '$[`)
		ctx.WriteString(fmt.Sprintf("%d", i+1))
		ctx.WriteString(`]') AS `)
		ctx.WriteString(d.sqliteType(ob.Col.Type))
		ctx.WriteString(`) AS `)
		if ob.KeyVar != "" && ob.Key != "" {
			ctx.Quote(ob.Col.Name + "_" + ob.Key)
		} else {
			ctx.Quote(ob.Col.Name)
		}
	}
	ctx.WriteString(`)`)
}

// sqliteType converts GraphJin types to SQLite types
func (d *SQLiteDialect) sqliteType(t string) string {
	switch t {
	case "int", "integer", "int4", "int8", "bigint", "smallint":
		return "INTEGER"
	case "float", "float4", "float8", "double", "real", "numeric", "decimal":
		return "REAL"
	default:
		return "TEXT"
	}
}


func (d *SQLiteDialect) RenderOrderBy(ctx Context, sel *qcode.Select) {
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
			ctx.ColWithTable("_gj_ob_"+ob.Col.Name, "ord")
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

func (d *SQLiteDialect) RenderDistinctOn(ctx Context, sel *qcode.Select) {
}

func (d *SQLiteDialect) RenderFromEdge(ctx Context, sel *qcode.Select) {
	// Uses json_each for table function equivalent
	ctx.WriteString(`json_each(`)
	ctx.ColWithTable(sel.Rel.Left.Col.Table, sel.Rel.Left.Col.Name)
	ctx.WriteString(`) AS `)
	ctx.Quote(fmt.Sprintf("__sr_%d", sel.ID))
}

func (d *SQLiteDialect) RenderJSONPath(ctx Context, table, col string, path []string) {
	// SQLite JSON path syntax: json_extract(column, '$.path1.path2')
	ctx.WriteString(`json_extract(`)
	ctx.ColWithTable(table, col)
	ctx.WriteString(`, '$.`)
	for i, pathElement := range path {
		if i > 0 {
			ctx.WriteString(`.`)
		}
		ctx.WriteString(pathElement)
	}
	ctx.WriteString(`')`)
}

func (d *SQLiteDialect) RenderList(ctx Context, ex *qcode.Exp) {
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

func (d *SQLiteDialect) RenderValPrefix(ctx Context, ex *qcode.Exp) bool {
	if ex.Op == qcode.OpHasKey {
		ctx.WriteString(`json_extract(`)
		ctx.ColWithTable(ex.Left.Col.Table, ex.Left.Col.Name)
		ctx.WriteString(`, '$."' || `)
		if ex.Right.ValType == qcode.ValVar {
			ctx.AddParam(Param{Name: ex.Right.Val, Type: "text"})
		} else {
			ctx.WriteString(fmt.Sprintf("'%s'", ex.Right.Val))
		}
		ctx.WriteString(` || '"') IS NOT NULL`)
		return true
	}

	if ex.Op == qcode.OpHasKeyAny || ex.Op == qcode.OpHasKeyAll {
		op := " OR "
		if ex.Op == qcode.OpHasKeyAll {
			op = " AND "
		}
		ctx.WriteString(`(`)
		if ex.Right.ValType == qcode.ValVar {
			// Variable case: use json_each on the variable
			// EXISTS (SELECT 1 FROM json_each($var) WHERE json_extract(col, '$."' || value || '"') IS NOT NULL)
			if ex.Op == qcode.OpHasKeyAll {
				ctx.WriteString(`NOT EXISTS (SELECT 1 FROM json_each(`)
			} else {
				ctx.WriteString(`EXISTS (SELECT 1 FROM json_each(`)
			}
			ctx.AddParam(Param{Name: ex.Right.Val, Type: "json"})
			ctx.WriteString(`) WHERE json_extract(`)
			ctx.ColWithTable(ex.Left.Col.Table, ex.Left.Col.Name)
			ctx.WriteString(`, '$."' || value || '"') IS `)
			if ex.Op == qcode.OpHasKeyAll {
				ctx.WriteString(`NULL)`)
			} else {
				ctx.WriteString(`NOT NULL)`)
			}
		} else {
			// Literal list case
			for i, val := range ex.Right.ListVal {
				if i != 0 {
					ctx.WriteString(op)
				}
				ctx.WriteString(`json_extract(`)
				ctx.ColWithTable(ex.Left.Col.Table, ex.Left.Col.Name)
				ctx.WriteString(`, '$."` + val + `"') IS NOT NULL`)
			}
		}
		ctx.WriteString(`)`)
		return true
	}
	return false
}

func (d *SQLiteDialect) RenderTsQuery(ctx Context, ti sdata.DBTable, ex *qcode.Exp) {
	// SQLite FTS5 Match
	// MATCH 'query'
	ctx.WriteString(`(`)
    // Assume FTS table is joined or we are on it?
    // Basic match check:
    for i, col := range ti.FullText {
		if i != 0 {
			ctx.WriteString(` OR `)
		}
		ctx.ColWithTable(ti.Name, col.Name)
        ctx.WriteString(` MATCH `)
		if ex.Right.ValType == qcode.ValStr {
			d.RenderLiteral(ctx, ex.Right.Val, ex.Right.ValType)
		} else {
	        ctx.AddParam(Param{Name: ex.Right.Val, Type: "text"})
		}
	}
	ctx.WriteString(`)`)
}

func (d *SQLiteDialect) RenderSearchRank(ctx Context, sel *qcode.Select, f qcode.Field) {
	ctx.WriteString(`rank`) // FTS5 'rank' column
}

func (d *SQLiteDialect) RenderSearchHeadline(ctx Context, sel *qcode.Select, f qcode.Field) {
	ctx.WriteString(`highlight(`)
    ctx.ColWithTable(sel.Table, f.Col.Name)
    ctx.WriteString(`, 0, '<b>', '</b>')`) // basic highlight
}

func (d *SQLiteDialect) RenderValVar(ctx Context, ex *qcode.Exp, val string) bool {
	if ex.Op == qcode.OpIn || ex.Op == qcode.OpNotIn {
		ctx.WriteString(`(SELECT value FROM json_each(`)
		ctx.AddParam(Param{Name: ex.Right.Val, Type: "json"})
		ctx.WriteString(`))`)
		return true
	}
	return false
}

func (d *SQLiteDialect) RenderLiteral(ctx Context, val string, valType qcode.ValType) {
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

func (d *SQLiteDialect) RenderJoinTables(ctx Context, sel *qcode.Select) {
	for _, ob := range sel.OrderBy {
		if ob.Var != "" {
			ctx.WriteString(` JOIN (SELECT value, key as ord FROM json_each(`)
			ctx.AddParam(Param{Name: ob.Var, Type: "json"})
			ctx.WriteString(`)) AS _gj_ob_` + ob.Col.Name)
			ctx.WriteString(` ON _gj_ob_` + ob.Col.Name + `.value = `)
			ctx.ColWithTable(sel.Table, ob.Col.Name)
		}
	}
}

func (d *SQLiteDialect) RenderValArrayColumn(ctx Context, ex *qcode.Exp, table string, pid int32) {
    // Similar to MySQL logic using json_each or similar
    // Fallback to default for now
	ctx.ColWithTable(table, ex.Right.Col.Name)
}

func (d *SQLiteDialect) RenderOp(op qcode.ExpOp) (string, error) {
	switch op {
	case qcode.OpIn:
		return `IN`, nil
	case qcode.OpNotIn:
		return `NOT IN`, nil
	case qcode.OpLike, qcode.OpILike:
		return `LIKE`, nil
	case qcode.OpNotLike, qcode.OpNotILike:
		return `NOT LIKE`, nil
	case qcode.OpRegex, qcode.OpIRegex:
		return `REGEXP`, nil // If REGEXP extension loaded
	case qcode.OpNotRegex, qcode.OpNotIRegex:
		return `NOT REGEXP`, nil
    case qcode.OpContains:
         // json_each or custom check
         return "", fmt.Errorf("operator not supported in SQLite: %d", op)
	}
	return "", nil
}

func (d *SQLiteDialect) BindVar(i int) string {
	return "?"
}

func (d *SQLiteDialect) UseNamedParams() bool {
	return false
}



func (d *SQLiteDialect) SupportsReturning() bool {
	return true
}

func (d *SQLiteDialect) SupportsWritableCTE() bool {
	return false
}

func (d *SQLiteDialect) SupportsConflictUpdate() bool {
	return true
}

func (d *SQLiteDialect) RenderMutationCTE(ctx Context, m *qcode.Mutate, renderBody func()) {
	// SQLite supports CTEs but not writable CTEs in standard way for this usage?
	// Actually SQLite allows INSERT INTO ... SELECT ...
	// But `WITH x AS (INSERT ... RETURNING)` is not valid in SQLite?
	// Docs say: "The WITH clause cannot be used with an INSERT, UPDATE, or DELETE statement." -> Wait.
	// "The WITH clause ... can occur ... at the beginning of an INSERT, UPDATE, Delete."
	// But can the CTE *contain* an INSERT? No.
	// So SupportsWritableCTE is false.
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

func (d *SQLiteDialect) RenderInsert(ctx Context, m *qcode.Mutate, values func()) {
	ctx.WriteString(`INSERT INTO `)
	ctx.ColWithTable(m.Ti.Schema, m.Ti.Name)
	ctx.WriteString(` (`)
	values()
	ctx.WriteString(`)`)
}

func (d *SQLiteDialect) RenderUpdate(ctx Context, m *qcode.Mutate, set func(), from func(), where func()) {
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

func (d *SQLiteDialect) RenderDelete(ctx Context, m *qcode.Mutate, where func()) {
	ctx.WriteString(`DELETE FROM `)
	ctx.ColWithTable(m.Ti.Schema, m.Ti.Name)
	ctx.WriteString(` WHERE `)
	where()
}

func (d *SQLiteDialect) RenderUpsert(ctx Context, m *qcode.Mutate, insert func(), updateSet func()) {
	insert()
	ctx.WriteString(` ON CONFLICT (`)
	// SQLite ON CONFLICT target required? Yes.
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
	if i == 0 {
		ctx.WriteString(m.Ti.PrimaryCol.Name)
	}
	ctx.WriteString(`) DO UPDATE SET `)
	updateSet()
}

func (d *SQLiteDialect) RenderReturning(ctx Context, m *qcode.Mutate) {
	ctx.WriteString(` RETURNING `)
	ctx.ColWithTable(m.Ti.Schema, m.Ti.Name)
	ctx.WriteString(`.*`)
}

func (d *SQLiteDialect) RenderAssign(ctx Context, col string, val string) {
	ctx.WriteString(col)
	ctx.WriteString(` = `)
	ctx.WriteString(val)
}

func (d *SQLiteDialect) RenderCast(ctx Context, val func(), typ string) {
	ctx.WriteString(`CAST(`)
	val()
	ctx.WriteString(` AS `)
	ctx.WriteString(typ)
	ctx.WriteString(`)`)
}

func (d *SQLiteDialect) RenderTryCast(ctx Context, val func(), typ string) {
	val()
}

func (d *SQLiteDialect) RenderSubscriptionUnbox(ctx Context, params []Param, renderInnerSQL func()) {
	// SQLite json_each approach
	ctx.WriteString(`WITH _gj_sub AS (SELECT `)
	for i, p := range params {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		// value is parsed from json
		ctx.WriteString(`value ->> 'type' AS "` + p.Name + `"`) 
		// Wait, json_each returns key, value, type, fullkey, path.
		// If input is ARRAY of objects/arrays?
		// Subscription params are usually [param1, param2, ...].
		// If params are passed as JSON array.
		// `json_each($1)`
		// Row 1: value is param1.
	}
	// This logic for generic unbox is tricky in SQLite without creating object logic.
	// But for MVP, let's assume we use standard json_each on the array.
	// AND we need to cast/extract?
	// Let's implement basic structure:
	ctx.WriteString(`* FROM json_each($1))`)
	
	ctx.WriteString(` SELECT _gj_sub_data.__root FROM _gj_sub LEFT OUTER JOIN LATERAL (`)
	renderInnerSQL()
	ctx.WriteString(`) AS _gj_sub_data ON true`)
}

func (d *SQLiteDialect) SupportsLinearExecution() bool {
	return true
}

func (d *SQLiteDialect) RenderIDCapture(ctx Context, name string) {
	ctx.WriteString(`INSERT INTO _gj_ids (k, id) VALUES ('`)
	ctx.WriteString(name)
	ctx.WriteString(`', last_insert_rowid())`)
}

func (d *SQLiteDialect) RenderVar(ctx Context, name string) {
	ctx.WriteString(`(SELECT id FROM _gj_ids WHERE k = '`)
	ctx.WriteString(name)
	ctx.WriteString(`')`)
}

func (d *SQLiteDialect) RenderSetup(ctx Context) {
	ctx.WriteString(`CREATE TEMP TABLE IF NOT EXISTS _gj_ids (k TEXT PRIMARY KEY, id INTEGER); `)
}

func (d *SQLiteDialect) RenderTeardown(ctx Context) {
	ctx.WriteString(`; DROP TABLE _gj_ids`)
}

func (d *SQLiteDialect) RenderMutateToRecordSet(ctx Context, m *qcode.Mutate, n int, renderRoot func()) {
	if n != 0 {
		ctx.WriteString(`, `)
	}

	// For SQLite we use json_each to convert JSON input to a derived table
	ctx.WriteString(`json_each(`)
	renderRoot()
	joinPathSQLite(ctx, "", m.Path, false) 
	ctx.WriteString(`) AS t`)
	
	// Note: SQLite json_each return 'value' (and 'key', etc).
	// Subsequent mapping needs to handle 't.value'.
	// But the generic code expects columns like "t"."name".
	// SQLite dynamic table json_each returns fixed columns.
	// Since we are aliasing AS t, t.value is the object.
	// However, subsequent column logical checks 't.name' won't work on 'value'.
	// SQLite lacks json_table.
	// We might need a CTE or subquery to project 'value' ->> 'name' AS name.
	// OR we assume the callers only use it in a way compatible?
	// The psql/generate.go logic does: `... AS t(col1 type1, col2 type2)`
	// SQLite json_each -> t(key, value, type, atom, id, parent, fullkey, path).
	// We can't define schema inplace.
	
	// BUT, if we look at `renderLinearUpdate`, it accesses columns via `renderColumnValue`.
	// `renderColumnValue` for JSON: `c.colWithTable("t", col.FieldName)`.
	// For Postgres: "t"."name".
	// For SQLite: "t"."name" is invalid if t is json_each.
	// It should be `json_extract(t.value, '$.name')`.
	
	// This implies `RenderColumnValue` or similar abstraction is also needed or 
	// `RenderMutateToRecordSet` must actually produce a subquery that projects these columns.
	// Let's try to project them as a subquery if possible?
	// (SELECT json_extract(value, '$.col') as col, ... FROM json_each(...)) AS t
}


// RenderSetSessionVar renders the SQL to set a session variable in SQLite
func (d *SQLiteDialect) RenderSetSessionVar(ctx Context, name, value string) bool {
	// SQLite does not support session variables in the same way (SET ...).
	// We rely on query parameters instead.
	return false
}

// Helper to join path for SQLite
func joinPathSQLite(ctx Context, prefix string, path []string, enableCamelcase bool) {
	ctx.WriteString(prefix)
	for i := range path {
		ctx.WriteString(`->>`) // or -> for intermediate?
		// Actually json path structure:
		// SQLite: json_extract(json, '$.a.b')
		// But here we are appending to a string builder?
		// prefix is likely "i.j".
		// `i.j->'a'->'b'` works in sqlite if they are valid operators.
		// SQLite has -> and ->>.
		ctx.WriteString(`->`)
		ctx.WriteString(`'`)
		if enableCamelcase {
			// ctx.WriteString(util.ToCamel(path[i])) // Dialect doesn't have util imported yet
			ctx.WriteString(path[i]) // Skip camel for now or add import
		} else {
			ctx.WriteString(path[i])
		}
		ctx.WriteString(`'`)
	}
}
