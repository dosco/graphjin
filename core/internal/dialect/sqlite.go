package dialect

import (
	"fmt"
	"strings"

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


func (d *SQLiteDialect) RenderSetup(ctx Context) {
	ctx.WriteString(`CREATE TEMP TABLE IF NOT EXISTS _gj_ids (k TEXT, id INTEGER, PRIMARY KEY (k, id)); `)
}

func (d *SQLiteDialect) RenderBegin(ctx Context) {
}

func (d *SQLiteDialect) RenderTeardown(ctx Context) {
	ctx.WriteString(`; DROP TABLE IF EXISTS _gj_ids; DROP TRIGGER IF EXISTS gj_capture; `)
}

func (d *SQLiteDialect) RenderVarDeclaration(ctx Context, name, typeName string) {}

func (d *SQLiteDialect) RenderVar(ctx Context, name string) {
	ctx.WriteString(`(SELECT id FROM _gj_ids WHERE k = '`)
	ctx.WriteString(name)
	ctx.WriteString(`')`)
}

func (d *SQLiteDialect) RenderIDCapture(ctx Context, name string) {
	ctx.WriteString(`INSERT OR IGNORE INTO _gj_ids (k, id) VALUES ('`)
	ctx.WriteString(name)
	ctx.WriteString(`', last_insert_rowid())`)
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
	// SQLite FTS5: For content-backed FTS, we need to query the FTS virtual table
	// and match rowid with the main table's primary key.
	// The FTS table name is typically the main table name suffixed with "_fts"
	ftsTableName := ti.Name + "_fts"
	
	ctx.WriteString(`(`)
	ctx.ColWithTable(ti.Name, ti.PrimaryCol.Name)
	ctx.WriteString(` IN (SELECT rowid FROM `)
	ctx.Quote(ftsTableName)
	ctx.WriteString(` WHERE `)
	ctx.Quote(ftsTableName)
	ctx.WriteString(` MATCH `)
	if ex.Right.ValType == qcode.ValStr {
		d.RenderLiteral(ctx, ex.Right.Val, ex.Right.ValType)
	} else {
		ctx.AddParam(Param{Name: ex.Right.Val, Type: "text"})
	}
	ctx.WriteString(`))`)
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

func (d *SQLiteDialect) RenderJSONField(ctx Context, fieldName string, tableAlias string, colName string, isNull bool, isJSON bool) {
	// Not used by SQLite in current implementation (handled in columns.go)
}

func (d *SQLiteDialect) RenderRootTerminator(ctx Context) {
	ctx.WriteString(`) AS "__root"`)
}

func (d *SQLiteDialect) RenderBaseTable(ctx Context) {
	ctx.WriteString(`(SELECT 1)`)
}

func (d *SQLiteDialect) RenderJSONRootField(ctx Context, key string, val func()) {
	ctx.WriteString(`'`)
	ctx.WriteString(key)
	ctx.WriteString(`', `)
	val()
}

func (d *SQLiteDialect) RenderTableAlias(ctx Context, alias string) {
	ctx.WriteString(` AS `)
	ctx.Quote(alias)
}

func (d *SQLiteDialect) RenderLateralJoinClose(ctx Context, alias string) {
	ctx.WriteString(`) AS `)
	ctx.Quote(alias)
	ctx.WriteString(` ON true`)
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

func (d *SQLiteDialect) RenderArray(ctx Context, items []string) {
	ctx.WriteString(`json_array(`)
	for i, item := range items {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.WriteString(item)
	}
	ctx.WriteString(`)`)
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
	return false
}



func (d *SQLiteDialect) SupportsWritableCTE() bool {
	return false
}

func (d *SQLiteDialect) SupportsConflictUpdate() bool {
	return true
}

func (d *SQLiteDialect) RenderMutationCTE(ctx Context, m *qcode.Mutate, renderBody func()) {
	// SQLite supports CTEs but not writable CTEs data-modifying CTEs (INSERT inside WITH).
	// So we render the body directly (INSERT ...) so it becomes the main statement.
	// We inject a dummy CTE to consume the trailing comma from the previous CTE (e.g. input variables).
	// Result: `WITH input AS (...), "ignored_<table>_<id>" AS (SELECT 1) INSERT ...`
	var cteName string
	if m.Multi {
		cteName = fmt.Sprintf("ignored_%s_%d", m.Ti.Name, m.ID)
	} else {
		cteName = "ignored_" + m.Ti.Name
	}
	ctx.Quote(cteName)
	ctx.WriteString(` AS (SELECT 1) `)
	renderBody()
}

func (d *SQLiteDialect) RenderInsert(ctx Context, m *qcode.Mutate, values func()) {
	// Capture all inserted IDs using a temporary trigger
	// This works for both Single and Bulk inserts
	vName := getVarName(m)
    
	ctx.WriteString(`DROP TRIGGER IF EXISTS gj_capture; `)
	ctx.WriteString(`CREATE TEMP TRIGGER gj_capture AFTER INSERT ON `)
	ctx.Quote(m.Ti.Name)
	ctx.WriteString(` BEGIN INSERT INTO _gj_ids (k, id) VALUES ('`)
	ctx.WriteString(vName)
	ctx.WriteString(`', NEW.`)
	ctx.Quote(m.Ti.PrimaryCol.Name)
	ctx.WriteString(`); END; `)

	ctx.WriteString(`INSERT INTO `)
	ctx.ColWithTable(m.Ti.Schema, m.Ti.Name)
	ctx.WriteString(` (`)
	values()
	ctx.WriteString(`) `)
}

func (d *SQLiteDialect) RenderUpdate(ctx Context, m *qcode.Mutate, set func(), from func(), where func()) {
	// Pre-select IDs into _gj_ids for later use by the SELECT query
	vName := getVarName(m)
	ctx.WriteString(`INSERT INTO _gj_ids (k, id) SELECT '`)
	ctx.WriteString(vName)
	ctx.WriteString(`', `)
	ctx.ColWithTable(m.Ti.Name, m.Ti.PrimaryCol.Name)
	ctx.WriteString(` FROM `)
	ctx.ColWithTable(m.Ti.Schema, m.Ti.Name)
    ctx.WriteString(` AS `)
    ctx.Quote(m.Ti.Name)
	if from != nil {
		ctx.WriteString(`, `) // Comma for implicit join in SELECT
		from()
	}
	ctx.WriteString(` WHERE `)

	// Add implicit join condition for JSON updates (only for Arrays where ID is in Input)
	if m.IsJSON && m.Array {
		pkAlias := m.Ti.PrimaryCol.Name
        isExplicitPK := false
		for _, col := range m.Cols {
			if col.Col.Name == m.Ti.PrimaryCol.Name {
				pkAlias = col.FieldName
                isExplicitPK = true
				break
			}
		}

        // If PK is implicit, we aliased it as "_gj_pkt" in RenderMutateToRecordSet
        if !isExplicitPK {
            pkAlias = "_gj_pkt"
        }

		ctx.ColWithTable(m.Ti.Name, m.Ti.PrimaryCol.Name)
		ctx.WriteString(` = t.`)
		ctx.Quote(pkAlias)
		ctx.WriteString(` AND `)
	}

	where()
	ctx.WriteString(`; `)

	ctx.WriteString(`UPDATE `)
	ctx.ColWithTable(m.Ti.Schema, m.Ti.Name)
    ctx.WriteString(` AS `)
    ctx.Quote(m.Ti.Name)
	ctx.WriteString(` SET `)
	set()
	if from != nil {
		ctx.WriteString(` FROM `) // SQLite UPDATE FROM syntax
		from()
	}
	ctx.WriteString(` WHERE `)

	// Add implicit join condition for JSON updates (only for Arrays where ID is in Input)
	if m.IsJSON && m.Array {
		pkAlias := m.Ti.PrimaryCol.Name
        isExplicitPK := false
		for _, col := range m.Cols {
			if col.Col.Name == m.Ti.PrimaryCol.Name {
				pkAlias = col.FieldName
                isExplicitPK = true
				break
			}
		}

        // If PK is implicit, we aliased it as "_gj_pkt" in RenderMutateToRecordSet
        if !isExplicitPK {
            pkAlias = "_gj_pkt"
        }

		ctx.ColWithTable(m.Ti.Name, m.Ti.PrimaryCol.Name)
		ctx.WriteString(` = t.`)
		ctx.Quote(pkAlias)
		ctx.WriteString(` AND `)
	}

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
	// SQLite 3.35+ supports RETURNING clause
	// Return a JSON object with the ID for the execution layer to parse
	ctx.WriteString(` RETURNING json_object('id', `)
	ctx.Quote(m.Ti.PrimaryCol.Name)
	ctx.WriteString(`)`)
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
	// SQLite doesn't support LATERAL joins, use subquery approach
	ctx.WriteString(`WITH _gj_sub AS (SELECT `)
	seen := make(map[string]int)
	for i, p := range params {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		name := p.Name
		if count, ok := seen[name]; ok {
			seen[name] = count + 1
			name = fmt.Sprintf("%s_%d", name, count)
		} else {
			seen[name] = 1
		}
		ctx.WriteString(fmt.Sprintf(`json_extract(value, '$[%d]') AS "%s"`, i, name))
	}
	ctx.WriteString(` FROM json_each(?))`)
	ctx.WriteString(` SELECT (`)
	renderInnerSQL()
	ctx.WriteString(`) AS "__root" FROM _gj_sub`)
}

func (d *SQLiteDialect) SupportsLinearExecution() bool {
	return true
}

func (d *SQLiteDialect) RenderMutationInput(ctx Context, qc *qcode.QCode) {
	ctx.WriteString(`WITH `)
	ctx.Quote("_sg_input")
	ctx.WriteString(` AS (SELECT `)
	ctx.AddParam(Param{Name: qc.ActionVar, Type: "json"})
	ctx.WriteString(` AS j)`)
}

func (d *SQLiteDialect) RenderMutationPostamble(ctx Context, qc *qcode.QCode) {
	// SQLite does nothing at the end of mutation
}

func (d *SQLiteDialect) getVarName(m qcode.Mutate) string {
	return m.Ti.Name + "_" + fmt.Sprintf("%d", m.ID)
}

func (d *SQLiteDialect) RenderLinearInsert(ctx Context, m *qcode.Mutate, qc *qcode.QCode, varName string, renderColVal func(qcode.MColumn)) {
	// Capture all inserted IDs using a temporary trigger (if not capturing via simple RETURNING)
	// But SQLite now supports RETURNING so we use that at end.
	
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
	for _, col := range m.Cols {
		if i != 0 {
			ctx.WriteString(", ")
		}
		renderColVal(col)
		i++
	}
	for _, rcol := range m.RCols {
		if i != 0 {
			ctx.WriteString(", ")
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
			ctx.WriteString("NULL")
		}
		i++
	}

	if m.IsJSON {
		ctx.WriteString(" FROM ")
		d.RenderLinearValues(ctx, m, func() {
             ctx.AddParam(Param{Name: qc.ActionVar, Type: "json"})
        })
	} else {
		ctx.WriteString(")")
	}

    // Render RETURNING clause - execution layer (gstate.go) captures IDs via @gj_ids hint
    // For inline inserts (!m.IsJSON), RETURNING works directly
    // For JSON inserts (m.IsJSON), RETURNING doesn't work with INSERT...SELECT, 
    // so we use a separate SELECT statement
    if !m.IsJSON {
        d.RenderReturning(ctx, m)
    }

	ctx.WriteString(" -- @gj_ids=")
	ctx.WriteString(varName)
	ctx.WriteString("\n; ")
    
    // Note: ID capture into _gj_ids is handled by gstate.go execution layer
    // It parses the RETURNING/SELECT JSON output and inserts into _gj_ids
    // We do NOT use last_insert_rowid() for inline inserts because it returns 
    // SQLite's internal rowid, not the user-provided explicit ID value

    // For JSON inserts (INSERT SELECT), RETURNING doesn't work, so we manually 
    // return the IDs via a SELECT. gstate.go will capture these via @gj_ids hint.
    if m.IsJSON {
        if m.Array {
            // Bulk insert: Extract ALL IDs from the JSON array input
            ctx.WriteString("SELECT json_object('id', json_extract(value, '$.")
            ctx.WriteString(m.Ti.PrimaryCol.Name)
            ctx.WriteString("')) FROM json_each(")
            ctx.AddParam(Param{Name: qc.ActionVar, Type: "json"})
            if len(m.Path) > 0 {
                ctx.WriteString(", '$.")
                ctx.WriteString(strings.Join(m.Path, "."))
                ctx.WriteString("'")
            }
            ctx.WriteString(") -- @gj_ids=")
            ctx.WriteString(varName)
            ctx.WriteString("\n; ")
        } else {
            // Single JSON insert: last_insert_rowid() is correct for one row
            ctx.WriteString("SELECT json_object('id', last_insert_rowid()) -- @gj_ids=")
            ctx.WriteString(varName)
            ctx.WriteString("\n; ")
        }
    }
}

func (d *SQLiteDialect) RenderLinearUpdate(ctx Context, m *qcode.Mutate, qc *qcode.QCode, varName string, renderColVal func(qcode.MColumn), renderWhere func()) {
    d.RenderUpdate(ctx, m, func() {
		// Set
		i := 0
		for _, col := range m.Cols {
			if i != 0 {
				ctx.WriteString(", ")
			}
            // SQLite restriction on qualified column names in SET
			ctx.Quote(col.Col.Name)
			ctx.WriteString(" = ")
			renderColVal(col)
			i++
		}
		for range m.RCols {
			// For SQLite updates, we don't want to update the relationship columns
			// in the SET clause, as we handle the join in the WHERE clause?
            // mutate.go logic: line 329: if c.dialect.Name() == "sqlite" { continue }
            // So we skip them here.
            continue
		}
		
		if i == 0 {
			ctx.Quote(m.Ti.PrimaryCol.Name)
			ctx.WriteString(" = ")
			ctx.Quote(m.Ti.PrimaryCol.Name)
		}
	}, func() {
        // From
        if m.IsJSON {
			d.RenderMutateToRecordSet(ctx, m, 0, func() {
				ctx.AddParam(Param{Name: qc.ActionVar, Type: "json"})
			})
		}
    }, func() {
        // Where
        // Logic from mutate.go lines 402+
        // c.renderExp(path...)
        
        // Also handle join conditions.
        // mutate.go: if m.ParentID != -1 ... AND childCol = (SELECT parentCol FROM ... WHERE ...)
        
        renderWhere() // Renders m.Where.Exp
        
        if m.ParentID != -1 {
            if m.Where.Exp != nil {
				ctx.WriteString(" AND ")
			}
			var childCol, parentCol string
			if m.Rel.Left.Ti.Name == m.Ti.Name {
				childCol = m.Rel.Left.Col.Name
				parentCol = m.Rel.Right.Col.Name
			} else {
				childCol = m.Rel.Right.Col.Name
				parentCol = m.Rel.Left.Col.Name
			}
			pm := qc.Mutates[m.ParentID]

			ctx.Quote(childCol)
			ctx.WriteString(" = (SELECT ")
			ctx.Quote(parentCol)
			ctx.WriteString(" FROM ")
			ctx.Quote(pm.Ti.Name)
			ctx.WriteString(" WHERE ")
			ctx.Quote(pm.Ti.PrimaryCol.Name)
			ctx.WriteString(" = ")
			d.RenderVar(ctx, d.getVarName(pm))
			ctx.WriteString(")")
        }
    })
    
    d.RenderReturning(ctx, m)
	ctx.WriteString(" -- @gj_ids=")
	ctx.WriteString(varName)
	ctx.WriteString("\n; ")
}

func (d *SQLiteDialect) RenderLinearConnect(ctx Context, m *qcode.Mutate, qc *qcode.QCode, varName string, renderFilter func()) {
	var colToUpdate string
	var tableToUpdate string
    _ = tableToUpdate
	
	// Default to Target table
	tableToUpdate = m.Ti.Name
	
	// Check if Foreign Key is on Parent (BelongsTo relationship)
	if m.ParentID != -1 {
		pm := qc.Mutates[m.ParentID]
		if pm.Ti.Name == m.Rel.Left.Col.Table && !m.Rel.Left.Col.PrimaryKey {
			colToUpdate = m.Rel.Left.Col.Name
			tableToUpdate = pm.Ti.Name
		} else if pm.Ti.Name == m.Rel.Right.Col.Table && !m.Rel.Right.Col.PrimaryKey {
			colToUpdate = m.Rel.Right.Col.Name
			tableToUpdate = pm.Ti.Name
		}
	}
	
	// If not found on Parent, check if FK is on Target
	if colToUpdate == "" {
		if m.Ti.Name == m.Rel.Right.Col.Table && !m.Rel.Right.Col.PrimaryKey {
			colToUpdate = m.Rel.Right.Col.Name
		} else if m.Ti.Name == m.Rel.Left.Col.Table && !m.Rel.Left.Col.PrimaryKey {
			colToUpdate = m.Rel.Left.Col.Name
		}
	}

	if colToUpdate != "" {
        var sb strings.Builder
		sb.WriteString(`UPDATE `)
		sb.WriteString(`"`)
        sb.WriteString(tableToUpdate)
        sb.WriteString(`"`)
		sb.WriteString(` SET `)
		sb.WriteString(`"`)
        sb.WriteString(colToUpdate)
        sb.WriteString(`"`)
		
		valToSet := "NULL"
		if len(m.Cols) > 0 {
			val := m.Cols[0].Value
			if strings.HasPrefix(val, "$") {
				valToSet = fmt.Sprintf("(SELECT id FROM _gj_ids WHERE k='%s')", val[1:])
			} else {
				valToSet = "'" + strings.ReplaceAll(val, "'", "''") + "'"
			}
		}

		// Determine if we are updating Parent or Child
		isParentUpdate := false
		if m.ParentID != -1 && tableToUpdate == qc.Mutates[m.ParentID].Ti.Name {
			isParentUpdate = true
		}

        var retID string

		if isParentUpdate {
			// Updating Parent (BelongsTo): SET FK = ChildID (valToSet)
			sb.WriteString(` = `)
			sb.WriteString(valToSet)
			
			// WHERE Parent.PK = ParentVar
			sb.WriteString(` WHERE `)
			pm := qc.Mutates[m.ParentID]
			sb.WriteString(`"`)
            sb.WriteString(pm.Ti.PrimaryCol.Name)
            sb.WriteString(`"`)
			sb.WriteString(` = (SELECT id FROM _gj_ids WHERE k = '`)
			sb.WriteString(d.getVarName(pm))
			sb.WriteString(`')`)

			// Return the Child ID (valToSet) so GraphJin knows what we linked
            retID = valToSet

		} else {
			// Updating Target (HasMany/One): SET FK = ParentVar
			sb.WriteString(` = `)
			if m.ParentID != -1 {
				pm := qc.Mutates[m.ParentID]
				sb.WriteString(`(SELECT id FROM _gj_ids WHERE k = '`)
				sb.WriteString(d.getVarName(pm))
				sb.WriteString(`')`)
			} else {
				sb.WriteString("NULL")
			}
			
			// WHERE Target.PK = ChildID (valToSet) OR Filter
			sb.WriteString(` WHERE `)
			
			// If we have a specific ID to connect (valToSet derived from m.Cols)
			if len(m.Cols) > 0 {
				sb.WriteString(`"`)
                sb.WriteString(m.Ti.PrimaryCol.Name)
                sb.WriteString(`"`)
				sb.WriteString(` = `)
				sb.WriteString(valToSet)
			} else {
				// Otherwise rely on filter (e.g. where inputs)
				renderFilter()
			}
			
			// Return Target ID (PK)
            // Use Primary Key name for return
            retID = `"` + m.Ti.PrimaryCol.Name + `"`
		}

		sb.WriteString(" -- @gj_ids=")
		sb.WriteString(varName)
		sb.WriteString("\n; ")
        
        // Append SELECT for RETURNING replacement
        sb.WriteString(`SELECT json_object('id', `)
        sb.WriteString(retID)
        sb.WriteString(`)`)
        sb.WriteString(" -- @gj_ids=")
		sb.WriteString(varName)
		sb.WriteString("\n; ")
        
        q := sb.String()
        ctx.WriteString(q)
		return
	}

	// Fallback: Select (Branch 2)
	ctx.WriteString(`SELECT `)
	ctx.WriteString(`json_object('id', `)
    ctx.ColWithTable(m.Ti.Name, m.Rel.Left.Col.Name)
    ctx.WriteString(`)`)
	
	if m.IsJSON {
		ctx.WriteString(` FROM `)
		d.RenderLinearValues(ctx, m, func() {
			ctx.AddParam(Param{Name: qc.ActionVar, Type: "json"})
		})
		ctx.WriteString(`, `)
	} else {
		ctx.WriteString(` FROM `)
	}
	ctx.Quote(m.Ti.Name)

	ctx.WriteString(` WHERE `)
    renderFilter()
    
	ctx.WriteString(" -- @gj_ids=")
	ctx.WriteString(varName)
	ctx.WriteString("\n; ")
}

func (d *SQLiteDialect) RenderLinearDisconnect(ctx Context, m *qcode.Mutate, qc *qcode.QCode, varName string, renderFilter func()) {
    // Logic from mutate.go lines 516+
    var childCol, parentCol string
    if m.Rel.Left.Ti.Name == m.Ti.Name {
        childCol = m.Rel.Left.Col.Name
        parentCol = m.Rel.Right.Col.Name
    } else {
        childCol = m.Rel.Right.Col.Name
        parentCol = m.Rel.Left.Col.Name
    }
    pm := qc.Mutates[m.ParentID]

    ctx.WriteString(`UPDATE `)
    ctx.Quote(m.Ti.Name)
    ctx.WriteString(` SET `)
    ctx.Quote(childCol)
    ctx.WriteString(` = NULL WHERE `)
    ctx.Quote(childCol)
    ctx.WriteString(` = (SELECT `)
    ctx.Quote(parentCol)
    ctx.WriteString(` FROM `)
    ctx.Quote(pm.Ti.Name)
    ctx.WriteString(` WHERE `)
    ctx.Quote(pm.Ti.PrimaryCol.Name)
    ctx.WriteString(` = `)
    d.RenderVar(ctx, d.getVarName(pm))
    ctx.WriteString(`) AND `)
    renderFilter()

    ctx.WriteString(" -- @gj_ids=")
    ctx.WriteString(varName)
    ctx.WriteString("\n; ")
}


// Package-level map to track mutated tables for the current mutation
// Package-level map removed - using Context.IsTableMutated instead

// RenderTableName renders table names for SQLite.
// For mutated tables in mutations, omits the schema so the scoping CTE is used.
func (d *SQLiteDialect) RenderTableName(ctx Context, sel *qcode.Select, schema, table string) {
	
	// Only omit schema for mutated tables that are:
	// 1. In a mutation query
	// 2. The table is mutated
	// 3. This is NOT a relationship join (RelNone or RelRecursive)
	if sel != nil && ctx.IsTableMutated(table) {
		// Check if this is a relationship join (not the main table)
		if sel.Rel.Type != sdata.RelNone {
			// This is a related table - use schema-qualified name
			if schema != "" {
				ctx.Quote(schema)
				ctx.WriteString(`.`)
			}
			ctx.Quote(table)
			return
		}
		// This is the main table in a mutation - omit schema to use CTE
		ctx.Quote(table)
	} else {
		// Normal rendering with schema
		if schema != "" {
			ctx.Quote(schema)
			ctx.WriteString(`.`)
		}
		ctx.Quote(table)
	}
}

// ModifySelectsForMutation tracks mutated tables for SQLite mutations.
// The scoping CTE handles all filtering, so we don't inject WHERE clauses.
func (d *SQLiteDialect) ModifySelectsForMutation(qc *qcode.QCode) {
	if qc.Type != qcode.QTMutation || qc.Selects == nil {
		return
	}
	// No need to populate global variable anymore
	// This works correctly for both single inserts and bulk inserts
}
// getVarName returns the variable name for a mutation's captured ID
func getVarName(m *qcode.Mutate) string {
return m.Ti.Name + "_" + fmt.Sprintf("%d", m.ID)
}
func (d *SQLiteDialect) RenderMutateToRecordSet(ctx Context, m *qcode.Mutate, n int, renderRoot func()) {
	if n != 0 {
		ctx.WriteString(`, `)
	}

	if m.Array {
        // Bulk inserts are wrapped by mutate.go in a SELECT ... FROM (...) AS t
        // So we MUST return a valid subquery with alias 't'.
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
			ctx.WriteString(`json_extract(value, '$.`)
			ctx.WriteString(col.FieldName)
			ctx.WriteString(`') AS `)
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
		if !d.SupportsLinearExecution() {
			ctx.WriteString(`_sg_input AS i, `)
		}
		ctx.WriteString(`json_each(`)
		renderRoot()
		if len(m.Path) > 0 {
			ctx.WriteString(`, '$.`)
			ctx.WriteString(strings.Join(m.Path, "."))
			ctx.WriteString(`'`)
		}
		ctx.WriteString(`)) AS t`)
	} else {
		// Single object case - always output (SELECT ...) AS t for valid FROM clause
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
			ctx.WriteString(`json_extract(`)
			renderRoot()
			ctx.WriteString(`, '$.`)
			if len(m.Path) > 0 {
				ctx.WriteString(strings.Join(m.Path, "."))
				ctx.WriteString(`.`)
			}
			ctx.WriteString(col.FieldName)
			ctx.WriteString(`') AS `)
			ctx.Quote(col.FieldName)
		}
// ... Inside RenderMutateToRecordSet Single Object Block
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
			ctx.WriteString(`') AS INTEGER) AS "_gj_pkt"`)
		}
		if !d.SupportsLinearExecution() {
			ctx.WriteString(` FROM _sg_input AS i`)
		}
        
		ctx.WriteString(`) AS t`)
	}
}

func (d *SQLiteDialect) RenderQueryPrefix(ctx Context, qc *qcode.QCode) {
	if qc.Type != qcode.QTMutation {
		return
	}
	// Group mutations by table
	tableMutations := make(map[string][]int32)
	for _, m := range qc.Mutates {
		if m.Type == qcode.MTNone || m.Type == qcode.MTKeyword {
			continue
		}
		tableMutations[m.Ti.Name] = append(tableMutations[m.Ti.Name], m.ID)
	}

    first := true
	for table, ids := range tableMutations {
		if !ctx.IsTableMutated(table) {
			continue
		}
		
		if first {
			ctx.WriteString(`WITH `)
			first = false
		} else {
			ctx.WriteString(`, `)
		}

		// Use the metadata from the first mutation for the table (schema, primary key)
		// This assumes schema/pk is consistent for the table across mutations.
		var m *qcode.Mutate
		// Find 'm' for details
		for _, mut := range qc.Mutates {
			if mut.ID == ids[0] {
				m = &mut
				break
			}
		}

		ctx.Quote(table)
		ctx.WriteString(` AS (SELECT * FROM `)
		if m.Ti.Schema != "" {
			ctx.Quote(m.Ti.Schema)
			ctx.WriteString(`.`)
		}
		ctx.Quote(table)
		ctx.WriteString(` WHERE `)
		ctx.Quote(m.Ti.PrimaryCol.Name)
		ctx.WriteString(` IN (SELECT id FROM _gj_ids WHERE k LIKE '`)
		ctx.WriteString(table)
		ctx.WriteString(`_%')) `)
	}
}

func (d *SQLiteDialect) SplitQuery(query string) (parts []string) {
	var buf strings.Builder
	var inStr, inQuote, inComment bool
    var depth int

    // Helper to check if we are at a keyword
    isKeyword := func(q string, i int, kw string) bool {
        if len(q)-i < len(kw) {
            return false
        }
        // Check word match
        if !strings.EqualFold(q[i:i+len(kw)], kw) {
            return false
        }
        // Check boundaries
        if i > 0 {
            c := q[i-1]
            if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' {
                return false
            }
        }
        if i+len(kw) < len(q) {
            c := q[i+len(kw)]
            if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' {
                return false
            }
        }
        return true
    }

	for i := 0; i < len(query); i++ {
		c := query[i]

		if inComment {
			if c == '\n' {
				inComment = false
			}
            // SQLite single-line comments don't end with semicolon technically, but graphjin gen might rely on it.
            // Stick to standard newline termination for safety.
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
        
        // Detect BEGIN/END for Triggers and Case statements (simple nesting)
        // Only check if not in string/quote/comment
        if c == 'B' || c == 'b' {
            if isKeyword(query, i, "BEGIN") {
                depth++
            }
        }
        if c == 'E' || c == 'e' {
            if isKeyword(query, i, "END") {
                if depth > 0 {
                    depth--
                }
            }
        }

		switch c {
		case '\'':
			inStr = true
			buf.WriteByte(c)
		case '"':
			inQuote = true
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
		case ';':
            // Only split if we are at depth 0 (not inside BEGIN...END)
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


func (d *SQLiteDialect) RenderSetSessionVar(ctx Context, name, value string) bool {
	return false
}

func (d *SQLiteDialect) RenderLinearValues(ctx Context, m *qcode.Mutate, renderRoot func()) {
	// Custom implementation for Linear Execution values that handles variable injection
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

		if col.Set && col.Value != "" {
			if strings.HasPrefix(col.Value, "$") {
				ctx.WriteString(`(SELECT id FROM _gj_ids WHERE k='`)
				ctx.WriteString(col.Value[1:])
				ctx.WriteString(`')`)
			} else {
				if strings.HasPrefix(col.Value, "sql:") {
					ctx.WriteString(`(`)
					ctx.WriteString(col.Value[4:])
					ctx.WriteString(`)`)
				} else {
					ctx.WriteString("'")
					ctx.WriteString(strings.ReplaceAll(col.Value, "'", "''"))
					ctx.WriteString("'")
				}
			}
		} else {
            if m.Array {
			    ctx.WriteString(`json_extract(value, '$.`)
            } else {
                ctx.WriteString(`json_extract(`)
                renderRoot()
                ctx.WriteString(`, '$.`)
                if len(m.Path) > 0 {
                    ctx.WriteString(strings.Join(m.Path, "."))
                    ctx.WriteString(`.`)
                }
            }
			ctx.WriteString(col.FieldName)
			ctx.WriteString(`')`)
		}
		ctx.WriteString(` AS `)
		ctx.Quote(col.FieldName)
	}
	
	if !hasPK {
		if !first {
			ctx.WriteString(`, `)
		}
        if m.Array {
		    ctx.WriteString(`json_extract(value, '$.`)
        } else {
            ctx.WriteString(`json_extract(`)
            renderRoot()
            ctx.WriteString(`, '$.`)
            if len(m.Path) > 0 {
                ctx.WriteString(strings.Join(m.Path, "."))
                ctx.WriteString(`.`)
            }
        }
		ctx.WriteString(m.Ti.PrimaryCol.Name) 
		ctx.WriteString(`') AS "_gj_pkt"`)
	}

    if m.Array {
	    ctx.WriteString(` FROM `)
	    ctx.WriteString(`json_each(`)
	    renderRoot()
	    if len(m.Path) > 0 {
		    ctx.WriteString(`, '$.`)
		    ctx.WriteString(strings.Join(m.Path, "."))
		    ctx.WriteString(`'`)
	    }
	    ctx.WriteString(`)`)
    }
	ctx.WriteString(`) AS t`)
}
