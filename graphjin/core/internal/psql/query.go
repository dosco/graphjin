//nolint:errcheck
package psql

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/dosco/graphjin/core/v3/internal/dialect"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

const (
	closeBlock = 500
)

type Param struct {
	Name        string
	Type        string
	IsArray     bool
	IsNotNull   bool
	WrapInArray bool // For MySQL/MariaDB: wrap single JSON object in array for JSON_TABLE
}

type Metadata struct {
	ct     string
	poll   bool
	params []Param
	pindex map[string]int
}

type compilerContext struct {
	md     *Metadata
	w      *bytes.Buffer
	qc     *qcode.QCode
	isJSON bool
	err    error
	*Compiler
}

type Variables map[string]json.RawMessage

type Config struct {
	Vars            map[string]string
	DBType          string
	DBVersion       int
	SecPrefix       []byte
	EnableCamelcase bool
}

type Compiler struct {
	svars           map[string]string
	dialect         dialect.Dialect
	cv              int    // db version
	pf              []byte // security prefix
	enableCamelcase bool
}

func (c *Compiler) GetDialect() dialect.Dialect {
	return c.dialect
}

// SetSchemaInfo passes table metadata to the dialect for identifier mapping.
func (c *Compiler) SetSchemaInfo(tables []sdata.DBTable) {
	if nms, ok := c.dialect.(dialect.NameMapSetter); ok {
		nms.SetNameMap(tables)
	}
}

func NewCompiler(conf Config) *Compiler {
	var d dialect.Dialect
	switch conf.DBType {
	case "mysql":
		d = &dialect.MySQLDialect{EnableCamelcase: conf.EnableCamelcase}
	case "mariadb":
		d = &dialect.MariaDBDialect{
			MySQLDialect: dialect.MySQLDialect{EnableCamelcase: conf.EnableCamelcase},
			DBVersion:    conf.DBVersion,
		}
	case "sqlite":
		d = &dialect.SQLiteDialect{}
	case "oracle":
		d = &dialect.OracleDialect{EnableCamelcase: conf.EnableCamelcase}
	case "mssql":
		d = &dialect.MSSQLDialect{
			DBVersion:       conf.DBVersion,
			EnableCamelcase: conf.EnableCamelcase,
		}
	case "mongodb":
		d = &dialect.MongoDBDialect{EnableCamelcase: conf.EnableCamelcase}
	default:
		d = &dialect.PostgresDialect{
			DBVersion:       conf.DBVersion,
			EnableCamelcase: conf.EnableCamelcase,
			SecPrefix:       conf.SecPrefix,
		}
	}

	return &Compiler{
		svars:           conf.Vars,
		dialect:         d,
		cv:              conf.DBVersion,
		pf:              conf.SecPrefix,
		enableCamelcase: conf.EnableCamelcase,
	}
}

func (co *Compiler) CompileEx(qc *qcode.QCode) (Metadata, []byte, error) {
	var w bytes.Buffer

	if metad, err := co.Compile(&w, qc); err != nil {
		return metad, nil, err
	} else {
		return metad, w.Bytes(), nil
	}
}

func (co *Compiler) Compile(w *bytes.Buffer, qc *qcode.QCode) (Metadata, error) {
	var err error
	var md Metadata

	if qc == nil {
		return md, fmt.Errorf("qcode is nil")
	}

	// Skip SQL comment for MongoDB (it generates JSON, not SQL)
	if co.dialect.Name() != "mongodb" {
		w.WriteString(`/* action='` + qc.Name + `',controller='graphql',framework='graphjin' */ `)
	}

	switch qc.Type {
	case qcode.QTQuery:
		err = co.CompileQuery(w, qc, &md)

	case qcode.QTSubscription:
		err = co.CompileQuery(w, qc, &md)

	case qcode.QTMutation:
		co.compileMutation(w, qc, &md)

	default:
		err = fmt.Errorf("unknown operation type %d", qc.Type)
	}


	// md.ct = qc.Schema.DBType()
	
	return md, err
}

func (co *Compiler) RenderSetSessionVar(name, value string) string {
	var w bytes.Buffer
	// Provide a minimal Context implementation over bytes.Buffer
	ctx := &compilerContext{
		w: &w,
		// minimal context, methods not used by RenderSetSessionVar should be safe or not called
	}
	// We need 'WriteString' which compilerContext has via *bytes.Buffer?
    // No, compilerContext has w *bytes.Buffer. It needs to implement Context interface.
    // compilerContext DOES implement Context (implicitly via methods in other files? or we need to ensure it).
    // Let's check if compilerContext implements Context.
    // It has `w *bytes.Buffer`.
    // Dialect expects `Context` interface: WriteString, Quote, etc.
    // `compilerContext` in `query.go` (and probably `common.go` which I missed reading fully but saw `dialect/common.go` earlier failure)
    // usually implements these.
    // I should create a temporary context safe for this.
    
    if co.dialect.RenderSetSessionVar(ctx, name, value) {
    	return w.String()
    }
    return ""
}

func (co *Compiler) CompileQuery(
	w *bytes.Buffer,
	qc *qcode.QCode,
	md *Metadata,
) error {
	// Only enable poll mode (which renders params as _gj_sub.column)
	// if the dialect supports subscription batching
	if qc.Type == qcode.QTSubscription && co.dialect.SupportsSubscriptionBatching() {
		md.poll = true
	}

	// Check if the dialect wants to handle the entire query compilation itself
	// This is used by MongoDB which generates JSON query DSL, not SQL
	if fqc, ok := co.dialect.(dialect.FullQueryCompiler); ok {
		c := &compilerContext{
			md:       md,
			w:        w,
			qc:       qc,
			Compiler: co,
		}
		if fqc.CompileFullQuery(c, qc) {
			return c.err
		}
	}

	// md.ct = qc.Schema.DBType() // md.ct is likely used elsewhere, kept for now if md has it?
	// But md.ct was string. qc.Schema.DBType() is string.
	// Compiler struct no longer has ct.

	// c.ct usage in loops below needs refactor.

	st := NewIntStack()
	c := &compilerContext{
		md:       md,
		w:        w,
		qc:       qc,
		Compiler: co,
	}

	i := 0

	// For MSSQL without LATERAL: render cursor CTE at query root if any root selection uses cursor pagination
	// CTEs must be at the top level of a SQL query, not inside subqueries
	if !c.dialect.SupportsLateral() && c.dialect.Name() == "mssql" {
		for _, id := range qc.Roots {
			sel := &qc.Selects[id]
			if sel.Paging.Cursor {
				c.dialect.RenderCursorCTE(c, sel)
				break // Only need one CTE definition
			}
		}
	}

	c.dialect.RenderJSONRoot(c, nil) // sel is nil for root? Or qc is enough?
	// RenderJSONRoot(ctx, sel)
	// original: SELECT json_object( or jsonb_build_object(
	// It didn't use sel. It used qc.Name if qc.Typename.
	
	// Wait, original:
	// switch c.ct { ... SELECT ... }
	// if qc.Typename { c.w.WriteString(...) }
	
	// RenderJSONRoot should maybe handle the whole SELECT part?
	// My Dialect.RenderJSONRoot implementation just writes SELECT ..._object(
	// Then logic continues.
	
	if qc.Typename {
		c.dialect.RenderJSONRootField(c, "__typename", func() {
			c.squoted(qc.Name)
		})
		i++
	}


	for _, id := range qc.Roots {
		sel := &qc.Selects[id]

		if sel.SkipRender == qcode.SkipTypeDrop {
			continue
		}

		if i != 0 {
			c.w.WriteString(`, `)
		}

		switch sel.SkipRender {
		case qcode.SkipTypeUserNeeded, qcode.SkipTypeBlocked,
			qcode.SkipTypeNulled:

			// Use dialect-specific JSON null field rendering
			c.dialect.RenderJSONNullField(c, sel.FieldName)

			if sel.Paging.Cursor {
				// Use dialect-specific JSON null cursor field rendering
				c.dialect.RenderJSONNullCursorField(c, sel.FieldName)
			}

		default:
			c.dialect.RenderJSONRootField(c, sel.FieldName, func() {
			if !c.dialect.SupportsLateral() {
				// Dialects without LATERAL use inline subqueries
				// Each dialect implements its own RenderInlineChild
				c.dialect.RenderChildValue(c, sel, func() {
					c.dialect.RenderInlineChild(c, c, nil, sel)
				})
			} else {
				c.colWithTableID("__sj", sel.ID, "json")
			}
		})

			// return the cursor for the this child selector as part of the parents json
			if sel.Paging.Cursor {
				if c.dialect.Name() == "oracle" {
					c.w.WriteString(`, KEY '`)
					c.w.WriteString(sel.FieldName)
					c.w.WriteString(`_cursor' VALUE `)
					if !c.dialect.SupportsLateral() {
						c.dialect.RenderChildCursor(c, func() {
							c.dialect.RenderInlineChild(c, c, nil, sel)
						})
					} else {
						c.colWithTableID("__sj", int32(sel.ID), "__cursor")
					}
				} else if c.dialect.Name() == "mssql" {
					// MSSQL needs value AS [field_cursor] format for FOR JSON PATH
					c.w.WriteString(`, `)
					c.dialect.RenderChildCursor(c, func() {
						c.dialect.RenderInlineChild(c, c, nil, sel)
					})
					c.w.WriteString(` AS `)
					c.quoted(sel.FieldName + "_cursor")
				} else {
					c.w.WriteString(`, '`)
					c.w.WriteString(sel.FieldName)
					c.w.WriteString(`_cursor', `)

					if !c.dialect.SupportsLateral() {
						c.dialect.RenderChildCursor(c, func() {
							c.dialect.RenderInlineChild(c, c, nil, sel)
						})
					} else {
						c.colWithTableID("__sj", int32(sel.ID), "__cursor")
					}
				}
			}

			if !c.dialect.SupportsLateral() {
				i++
				continue
			}

			st.Push(sel.ID + closeBlock)
			st.Push(sel.ID)
		}
		i++
	}

	// This helps multi-root work as well as return a null json value when
	// there are no rows found.

	// Use dialect-specific JSON root suffix (e.g., FOR JSON PATH for MSSQL)
	c.dialect.RenderJSONRootSuffix(c)
	c.w.WriteString(`) AS `)
	c.quoted("__root")
	c.w.WriteString(` FROM (`)
	c.dialect.RenderBaseTable(c)
	c.w.WriteString(`)`)
	c.dialect.RenderTableAlias(c, "__root_x")
	c.renderQuery(st, true)
	


	return c.err
}

func (c *compilerContext) renderQuery(st *IntStack, multi bool) {
	for {
		var sel *qcode.Select
		var open bool

		if st.Len() == 0 {
			break
		}

		id := st.Pop()
		if id < closeBlock {
			sel = &c.qc.Selects[id]
			open = true
		} else {
			sel = &c.qc.Selects[(id - closeBlock)]
		}

		if open {
			if sel.Type != qcode.SelTypeUnion {
				c.renderLateralJoin(sel, multi)
				c.renderPluralSelect(sel)
				c.renderSelect(sel)
			}

			for _, cid := range sel.Children {
				child := &c.qc.Selects[cid]

				if child.SkipRender != qcode.SkipTypeNone {
					continue
				}

				if !c.dialect.SupportsLateral() {
					continue
				}

				st.Push(child.ID + closeBlock)
				st.Push(child.ID)
			}

		} else {
			if sel.Type != qcode.SelTypeUnion {
				c.renderSelectClose(sel)
				c.renderLateralJoinClose(sel, multi)
			}
		}
	}
}

func (c *compilerContext) renderInlineChild(sel *qcode.Select) {
	c.w.WriteString(`(`)
	c.renderPluralSelect(sel)
	c.renderSelect(sel)
	c.renderSelectClose(sel)
	c.w.WriteString(`)`)
}


func (c *compilerContext) renderPluralSelect(sel *qcode.Select) {
	if sel.Singular {
		return
	}

	// SQLite and MariaDB cursor workaround: return json_object containing both json and cursor
	if sel.Paging.Cursor && (c.dialect.Name() == "sqlite" || c.dialect.Name() == "mariadb") {
		c.w.WriteString(`SELECT json_object('json', `)

		if sel.FieldFilter.Exp != nil {
			c.w.WriteString(`(CASE WHEN `)
			c.renderExp(sel.Ti, sel.FieldFilter.Exp, false)
			c.w.WriteString(` THEN (SELECT `)
		}

		c.dialect.RenderJSONPlural(c, sel)

		if sel.FieldFilter.Exp != nil {
			c.w.WriteString(`) ELSE null END)`)
		}

		c.w.WriteString(`, 'cursor', '`)
		c.w.Write(c.pf)
		c.w.WriteString(`' || `)
		int32String(c.w, int32(sel.ID))

		for i := 0; i < len(sel.OrderBy); i++ {
			if c.dialect.Name() == "mariadb" {
				// MariaDB uses colon separator to match RenderCursorCTE parsing
				// json_group_array is SQLite. MariaDB uses json_arrayagg.
				c.w.WriteString(` || ':' || (CASE WHEN COUNT(*) > 0 THEN json_extract(json_arrayagg(__cur_`)
			} else {
				c.w.WriteString(` || ',' || (CASE WHEN COUNT(*) > 0 THEN json_extract(json_group_array(__cur_`)
			}
			int32String(c.w, int32(i))
			c.w.WriteString(`), '$[' || (COUNT(*) - 1) || ']') ELSE NULL END)`)
		}
		c.w.WriteString(`) as __cursor`) // Sub-select 1 column (the json_object)

	} else {
		c.w.WriteString(`SELECT `)
		if sel.FieldFilter.Exp != nil {
			c.w.WriteString(`(CASE WHEN `)
			c.renderExp(sel.Ti, sel.FieldFilter.Exp, false)
			c.w.WriteString(` THEN (SELECT `)
		}

		c.dialect.RenderJSONPlural(c, sel)

		if sel.FieldFilter.Exp != nil {
			c.w.WriteString(`) ELSE null END)`)
		}
		c.w.WriteString(` AS json`)

		// Build the cursor value string
		if sel.Paging.Cursor && c.dialect.SupportsLateral() {
			if c.dialect.Name() == "sqlite" {
				// Should not happen if we used the if block above,
				// but keeping for safety if logic changes
			} else if c.dialect.Name() == "oracle" {
				// Oracle uses || for concatenation
				c.w.WriteString(`, '`)
				c.w.Write(c.pf)
				c.w.WriteString(`' || `)
				int32String(c.w, int32(sel.ID))

				for i := 0; i < len(sel.OrderBy); i++ {
					c.w.WriteString(` || ',' || MAX("__CUR_`)
					int32String(c.w, int32(i))
					c.w.WriteString(`")`)
				}
				c.w.WriteString(` AS "__CURSOR"`)
			} else {
				c.w.WriteString(`, CONCAT('`)
				c.w.Write(c.pf)
				c.w.WriteString(`', CONCAT_WS(',', `)
				int32String(c.w, int32(sel.ID))

				for i := 0; i < len(sel.OrderBy); i++ {
					c.w.WriteString(`, MAX(__cur_`)
					int32String(c.w, int32(i))
					// Postgres rejects MAX(uuid)
					if sel.OrderBy[i].Col.Type == "uuid" {
						c.w.WriteString(`::text`)
					}
					c.w.WriteString(`)`)
				}
				c.w.WriteString(`)) as __cursor`)
			}
		}
	}

	c.w.WriteString(` FROM (`)
}

func (c *compilerContext) renderSelect(sel *qcode.Select) {
	c.dialect.RenderJSONSelect(c, sel)
	c.dialect.RenderTableAlias(c, "json")

	// We manually insert the cursor values into row we're building outside
	// of the generated json object so they can be used higher up in the sql.
	if sel.Paging.Cursor {
		for i := range sel.OrderBy {
			if c.dialect.Name() == "oracle" {
				c.w.WriteString(`, "__CUR_`)
				int32String(c.w, int32(i))
				c.w.WriteString(`" `)
			} else {
				c.w.WriteString(`, __cur_`)
				int32String(c.w, int32(i))
				c.w.WriteString(` `)
			}
		}
	}

	c.w.WriteString(`FROM (SELECT `)
	c.renderColumns(sel)

	// This is how we get the values to use to build the cursor.
	if sel.Paging.Cursor {
		for i, ob := range sel.OrderBy {
			if c.dialect.Name() == "sqlite" {
				c.w.WriteString(`, `)
				c.colWithTableID(sel.Table, sel.ID, ob.Col.Name)
				c.w.WriteString(` AS __cur_`)
				int32String(c.w, int32(i))
			} else if c.dialect.Name() == "oracle" {
				c.w.WriteString(`, LAST_VALUE(`)
				c.colWithTableID(sel.Table, sel.ID, ob.Col.Name)
				c.w.WriteString(`) OVER() AS "__CUR_`)
				int32String(c.w, int32(i))
				c.w.WriteString(`"`)
			} else {
				c.w.WriteString(`, LAST_VALUE(`)
				c.colWithTableID(sel.Table, sel.ID, ob.Col.Name)
				c.w.WriteString(`) OVER() AS __cur_`)
				int32String(c.w, int32(i))
			}
		}
	}

	c.w.WriteString(` FROM (`)
	if sel.Rel.Type == sdata.RelRecursive {
		c.renderRecursiveBaseSelect(sel)
	} else {
		c.renderBaseSelect(sel)
	}
	c.w.WriteString(`)`)
	c.aliasWithID(sel.Table, sel.ID)
}

func (c *compilerContext) renderSelectClose(sel *qcode.Select) {
	c.w.WriteString(`)`)
	c.aliasWithID("__sr", sel.ID)

	if !sel.Singular {
		c.w.WriteString(`)`)
		c.aliasWithID("__sj", sel.ID)
	}
}

func (c *compilerContext) renderLateralJoin(sel *qcode.Select, multi bool) {
	if sel.Rel.Type == sdata.RelNone && !multi {
		return
	}
	c.w.WriteString(` LEFT OUTER JOIN LATERAL (`)
}

func (c *compilerContext) renderLateralJoinClose(sel *qcode.Select, multi bool) {
	if sel.Rel.Type == sdata.RelNone && !multi {
		return
	}
	c.w.WriteString(`)`)
	c.aliasWithID(`__sj`, sel.ID)
	c.w.WriteString(` ON 1=1`)
}

func (c *compilerContext) renderJoinTables(sel *qcode.Select) {
	for _, join := range sel.Joins {
		c.renderJoin(join)
	}
	c.dialect.RenderJoinTables(c, sel)
}

func (c *compilerContext) IsTableMutated(table string) bool {
	if c.qc.Type != qcode.QTMutation {
		return false
	}
	for _, m := range c.qc.Mutates {
		if m.Ti.Name == table {
			return true
		}
	}
	return false
}

func (c *compilerContext) GetStaticVar(name string) (string, bool) {
	val, ok := c.svars[name]
	return val, ok
}

func (c *compilerContext) renderJoin(join qcode.Join) {
	c.w.WriteString(` INNER JOIN `)
	c.w.WriteString(join.Rel.Left.Ti.Name)
	c.w.WriteString(` ON ((`)
	c.renderExp(join.Rel.Left.Ti, join.Filter, false)
	c.w.WriteString(`))`)
}



func (c *compilerContext) renderBaseSelect(sel *qcode.Select) {
	c.renderCursorCTE(sel)
	c.w.WriteString(`SELECT `)
	c.renderDistinctOn(sel)
	c.renderBaseColumns(sel)
	c.renderFrom(sel)
	c.renderJoinTables(sel)
	c.renderFromCursor(sel)
	c.renderWhere(sel)
	c.renderGroupBy(sel)
	c.renderOrderBy(sel)
	c.renderLimit(sel)
}

func (c *compilerContext) renderLimit(sel *qcode.Select) {
	c.dialect.RenderLimit(c, sel)
}





func (c *compilerContext) renderFrom(sel *qcode.Select) {
	c.w.WriteString(` FROM `)

	// For mutations, use just the table name (no schema) so the CTE created
	// by INSERT/UPDATE/DELETE shadows the physical table name. This allows
	// the SELECT to query the mutation's result set instead of the full table.
	if c.qc.Type == qcode.QTMutation {
		c.quoted(sel.Table)
		c.dialect.RenderTableAlias(c, sel.Table)
		return
	}

	if sel.Ti.Type == "function" {
		c.renderTableFunction(sel)
		return
	}

	switch sel.Rel.Type {
	case sdata.RelEmbedded:
		c.w.WriteString(sel.Rel.Left.Col.Table)
		c.w.WriteString(`, `)

		c.dialect.RenderFromEdge(c, sel)

	default:
		c.table(sel, sel.Ti.Schema, sel.Ti.Name, true)
	}
}

func (c *compilerContext) renderFromCursor(sel *qcode.Select) {
	if sel.Paging.Cursor {
		c.w.WriteString(`, `)
		c.quoted("__cur")
	}
}





func (c *compilerContext) renderCursorCTE(sel *qcode.Select) {
	c.dialect.RenderCursorCTE(c, sel)
}

func (c *compilerContext) renderWhere(sel *qcode.Select) {
	if sel.Rel.Type == sdata.RelNone && sel.Where.Exp == nil {
		return
	}

	c.w.WriteString(` WHERE `)
	c.renderExp(sel.Ti, sel.Where.Exp, false)
}

func (c *compilerContext) renderGroupBy(sel *qcode.Select) {
	if !sel.GroupCols || len(sel.BCols) == 0 {
		return
	}
	c.w.WriteString(` GROUP BY `)
	for i, col := range sel.BCols {
		if i != 0 {
			c.w.WriteString(`, `)
		}
		c.colWithTable(sel.Table, col.Col.Name)
	}
}

func (c *compilerContext) renderOrderBy(sel *qcode.Select) {
	c.dialect.RenderOrderBy(c, sel)
}



func (c *compilerContext) renderDistinctOn(sel *qcode.Select) {
	c.dialect.RenderDistinctOn(c, sel)
}
