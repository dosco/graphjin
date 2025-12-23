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

	w.WriteString(`/* action='` + qc.Name + `',controller='graphql',framework='graphjin' */ `)

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
	if qc.Type == qcode.QTSubscription {
		md.poll = true
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

			if c.dialect.Name() == "oracle" {
				c.w.WriteString(`KEY '`)
				c.w.WriteString(sel.FieldName)
				c.w.WriteString(`' VALUE NULL`)
			} else {
				c.w.WriteString(`'`)
				c.w.WriteString(sel.FieldName)
				c.w.WriteString(`', NULL`)
			}

			if sel.Paging.Cursor {
				if c.dialect.Name() == "oracle" {
					c.w.WriteString(`, KEY '`)
					c.w.WriteString(sel.FieldName)
					c.w.WriteString(`_cursor' VALUE NULL`)
				} else {
					c.w.WriteString(`, '`)
					c.w.WriteString(sel.FieldName)
					c.w.WriteString(`_cursor', NULL`)
				}
			}

		default:
			c.dialect.RenderJSONRootField(c, sel.FieldName, func() {
				if !c.dialect.SupportsLateral() {
					if c.dialect.Name() == "sqlite" && !sel.Singular && sel.Paging.Cursor {
						c.w.WriteString(`json_extract(`)
						var buf bytes.Buffer
						oldW := c.w
						c.w = &buf
						c.renderInlineChild(sel)
						c.w = oldW
						subQuery := buf.String()
	
						c.w.WriteString(subQuery)
						c.w.WriteString(`, '$.json')`)
					} else if c.dialect.Name() == "mariadb" && !sel.Singular && sel.Paging.Cursor {
						// MariaDB workaround identical to SQLite if using the same json_object wrapper
						c.w.WriteString(`JSON_QUERY(`)
						var buf bytes.Buffer
						oldW := c.w
						c.w = &buf
						c.renderMariaDBInlineChild(nil, sel) // Pass nil as parent if effectively root usage or self-contained?
						// Wait, renderMariaDBInlineChild expects psel.
                        // Ideally we should use the proper render method. 
                        // But here we are inside RenderJSONRootField call.
                        // renderMariaDBInlineChild is typically called from inside renderMariaDBJSONFields.
                        // Here we are likely at the root or top level where we need this workaround?
                        // Actually, if we are in RenderJSONRootField, clean implementation might be in dialect.
						c.w = oldW
						c.w.WriteString(buf.String())
						c.w.WriteString(`, '$.json')`)
					} else {
						c.renderInlineChild(sel) // Default fallback
					}
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
				} else {
					c.w.WriteString(`, '`)
					c.w.WriteString(sel.FieldName)
					c.w.WriteString(`_cursor', `)
				}

				if !c.dialect.SupportsLateral() {
					if c.dialect.Name() == "sqlite" {
						c.w.WriteString(`json_extract(`)
						var buf bytes.Buffer
						oldW := c.w
						c.w = &buf
						c.renderInlineChild(sel)
						c.w = oldW
						c.w.WriteString(buf.String())
						c.w.WriteString(`, '$.cursor')`)
					} else {
						c.w.WriteString(`NULL`) // Cursors not fully supported inline yet
					}
				} else {
					c.colWithTableID("__sj", int32(sel.ID), "__cursor")
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

// renderMariaDBInlineChild generates a flat correlated subquery for MariaDB.
// MariaDB doesn't allow derived tables to reference outer query columns
// (unlike SQLite which is more permissive), so we generate simpler SQL
// that avoids nesting the correlated subquery inside derived tables.
func (c *compilerContext) renderMariaDBInlineChild(psel, sel *qcode.Select) {
	c.w.WriteString(`(SELECT `)

	if sel.Singular {
		// For singular (one-to-one/many-to-one), return a single json_object
		c.w.WriteString(`json_object(`)
		c.renderMariaDBJSONFields(sel)
		c.w.WriteString(`)`)
	} else {
		// For plural (one-to-many/many-to-many), aggregate into array
		c.w.WriteString(`COALESCE(json_arrayagg(json_object(`)
		c.renderMariaDBJSONFields(sel)
		c.w.WriteString(`)), '[]')`)
	}

	c.w.WriteString(` FROM `)
	c.table(sel, sel.Ti.Schema, sel.Ti.Name, true)

	// Render join tables for many-to-many relationships
	for _, join := range sel.Joins {
		c.renderJoin(join)
	}

	// Render the relationship filter (WHERE clause)
	// The relationship filter references the parent table with ID suffix
	if sel.Where.Exp != nil {
		c.w.WriteString(` WHERE `)
		c.renderMariaDBWhereExp(psel, sel, sel.Where.Exp)
	}

	// Render ORDER BY
	c.renderMariaDBOrderBy(sel)

	// Render LIMIT
	c.dialect.RenderLimit(c, sel)

	c.w.WriteString(`)`)
}

// renderMariaDBJSONFields renders the field list for json_object()
func (c *compilerContext) renderMariaDBJSONFields(sel *qcode.Select) {
	i := 0
	for _, f := range sel.Fields {
		if f.SkipRender != qcode.SkipTypeNone {
			continue
		}
		if i != 0 {
			c.w.WriteString(`, `)
		}
		c.squoted(f.FieldName)
		c.w.WriteString(`, `)

		switch f.Type {
		case qcode.FieldTypeCol:
			if c.dialect.Name() == "sqlite" {
				if f.Col.Array {
					c.w.WriteString(`(CASE WHEN json_valid(`)
					c.colWithTable(sel.Ti.Name, f.Col.Name)
					c.w.WriteString(`) THEN json(`)
					c.colWithTable(sel.Ti.Name, f.Col.Name)
					c.w.WriteString(`) ELSE `)
					c.colWithTable(sel.Ti.Name, f.Col.Name)
					c.w.WriteString(` END)`)
				} else {
					c.colWithTable(sel.Ti.Name, f.Col.Name)
				}
			} else {
				c.colWithTable(sel.Ti.Name, f.Col.Name)
			}
		case qcode.FieldTypeFunc:
			c.colWithTable(sel.Ti.Name, f.FieldName)
		default:
			c.w.WriteString(`NULL`)
		}
		i++
	}

	// Handle __typename if requested
	if sel.Typename {
		if i != 0 {
			c.w.WriteString(`, `)
		}
		c.w.WriteString(`'__typename', `)
		c.squoted(sel.Table)
	}

	// Handle nested children
	for _, cid := range sel.Children {
		csel := &c.qc.Selects[cid]
		if csel.SkipRender == qcode.SkipTypeRemote || csel.SkipRender == qcode.SkipTypeDrop {
			continue
		}
		if i != 0 {
			c.w.WriteString(`, `)
		}
		c.squoted(csel.FieldName)
		c.w.WriteString(`, `)

		// For MariaDB, wrap nested children with JSON_QUERY to prevent double-escaping
		// since MariaDB treats JSON as LONGTEXT and json_object would escape it as a string
		c.w.WriteString(`JSON_QUERY(`)
		// Render nested child as another inline subquery
		c.renderMariaDBInlineChild(sel, csel)
		c.w.WriteString(`, '$')`)
		i++
	}
}

// renderMariaDBWhereExp renders the WHERE expression for MariaDB inline children.
// This handles the relationship filter that references the parent table.
func (c *compilerContext) renderMariaDBWhereExp(psel, sel *qcode.Select, ex *qcode.Exp) {
	// For relationship filter, we need to render the expression but with
	// the parent table reference using the parent's ID suffix
	c.renderMariaDBExp(psel, sel, ex)
}

// renderMariaDBExp renders an expression for MariaDB, handling parent table references
func (c *compilerContext) renderMariaDBExp(psel, sel *qcode.Select, ex *qcode.Exp) {
	if ex == nil {
		return
	}

	switch ex.Op {
	case qcode.OpAnd:
		c.w.WriteString(`(`)
		for i, child := range ex.Children {
			if i > 0 {
				c.w.WriteString(` AND `)
			}
			c.renderMariaDBExp(psel, sel, child)
		}
		c.w.WriteString(`)`)

	case qcode.OpOr:
		c.w.WriteString(`(`)
		for i, child := range ex.Children {
			if i > 0 {
				c.w.WriteString(` OR `)
			}
			c.renderMariaDBExp(psel, sel, child)
		}
		c.w.WriteString(`)`)

	case qcode.OpNot:
		c.w.WriteString(`NOT `)
		c.renderMariaDBExp(psel, sel, ex.Children[0])

	case qcode.OpEquals, qcode.OpNotEquals, qcode.OpGreaterThan, qcode.OpLesserThan,
		qcode.OpGreaterOrEquals, qcode.OpLesserOrEquals:
		c.w.WriteString(`((`)

		// Render left side
		if ex.Left.Col.Name != "" {
			if ex.Left.ID >= 0 {
				// References a parent table
				c.colWithTableID(ex.Left.Col.Table, ex.Left.ID, ex.Left.Col.Name)
			} else {
				// Current table
				c.colWithTable(sel.Ti.Name, ex.Left.Col.Name)
			}
		}
		c.w.WriteString(`) `)

		// Render operator
		switch ex.Op {
		case qcode.OpEquals:
			c.w.WriteString(`=`)
		case qcode.OpNotEquals:
			c.w.WriteString(`!=`)
		case qcode.OpGreaterThan:
			c.w.WriteString(`>`)
		case qcode.OpLesserThan:
			c.w.WriteString(`<`)
		case qcode.OpGreaterOrEquals:
			c.w.WriteString(`>=`)
		case qcode.OpLesserOrEquals:
			c.w.WriteString(`<=`)
		}

		c.w.WriteString(` (`)

		// Render right side
		if ex.Right.Col.Name != "" {
			if ex.Right.ID >= 0 {
				c.colWithTableID(ex.Right.Col.Table, ex.Right.ID, ex.Right.Col.Name)
			} else {
				c.colWithTable(ex.Right.Col.Table, ex.Right.Col.Name)
			}
		} else if ex.Right.ValType == qcode.ValVar {
			c.renderParam(Param{Name: ex.Right.Val, Type: ex.Left.Col.Type})
		} else {
			c.dialect.RenderLiteral(c, ex.Right.Val, ex.Right.ValType)
		}

		c.w.WriteString(`))`)

	case qcode.OpIn, qcode.OpNotIn:
		c.w.WriteString(`((`)
		if ex.Left.Col.Name != "" {
			if ex.Left.ID >= 0 {
				c.colWithTableID(ex.Left.Col.Table, ex.Left.ID, ex.Left.Col.Name)
			} else {
				c.colWithTable(sel.Ti.Name, ex.Left.Col.Name)
			}
		}
		c.w.WriteString(`) `)
		if ex.Op == qcode.OpIn {
			c.w.WriteString(`IN`)
		} else {
			c.w.WriteString(`NOT IN`)
		}
		c.w.WriteString(` (`)

		if ex.Right.Col.Name != "" {
			if ex.Right.ID >= 0 {
				c.colWithTableID(ex.Right.Col.Table, ex.Right.ID, ex.Right.Col.Name)
			} else {
				c.colWithTable(ex.Right.Col.Table, ex.Right.Col.Name)
			}
		} else if ex.Right.ValType == qcode.ValVar {
			c.renderParam(Param{Name: ex.Right.Val, Type: ex.Left.Col.Type, IsArray: true})
		} else {
			c.dialect.RenderList(c, ex)
		}
		c.w.WriteString(`))`)

	default:
		c.renderExp(sel.Ti, ex, false)
	}
}

// renderMariaDBOrderBy renders ORDER BY for MariaDB inline child
func (c *compilerContext) renderMariaDBOrderBy(sel *qcode.Select) {
	if len(sel.OrderBy) == 0 {
		return
	}
	c.w.WriteString(` ORDER BY `)
	for i, ob := range sel.OrderBy {
		if i != 0 {
			c.w.WriteString(`, `)
		}
		c.colWithTable(sel.Ti.Name, ob.Col.Name)
		switch ob.Order {
		case qcode.OrderAsc:
			c.w.WriteString(` ASC`)
		case qcode.OrderDesc:
			c.w.WriteString(` DESC`)
		case qcode.OrderAscNullsFirst:
			c.w.WriteString(` ASC NULLS FIRST`)
		case qcode.OrderDescNullsFirst:
			c.w.WriteString(` DESC NULLS FIRST`)
		case qcode.OrderAscNullsLast:
			c.w.WriteString(` ASC NULLS LAST`)
		case qcode.OrderDescNullsLast:
			c.w.WriteString(` DESC NULLS LAST`)
		}
	}
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
				// MariaDB uses slightly different syntax or functions if needed, but standard SQL + JSON functions overlap well with SQLite here.
				// However, json_extract path in MariaDB is '$[0]' syntax too.
				// json_group_array is SQLite. MariaDB uses json_arrayagg.
				c.w.WriteString(` || ',' || (CASE WHEN COUNT(*) > 0 THEN json_extract(json_arrayagg(__cur_`)
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
