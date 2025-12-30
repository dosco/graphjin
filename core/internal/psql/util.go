package psql

import (
	"bytes"
	"strconv"

	"github.com/dosco/graphjin/core/v3/internal/dialect"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func (c *compilerContext) alias(alias string) {
	c.dialect.RenderTableAlias(c, alias)
}

func (c *compilerContext) aliasWithID(alias string, id int32) {
	c.dialect.RenderTableAlias(c, alias+"_"+strconv.Itoa(int(id)))
}

func (c *compilerContext) colWithTableID(table string, id int32, col string) {
	if id >= 0 {
		c.quoted(table + "_" + strconv.Itoa(int(id)))
	} else {
		c.quoted(table)
	}
	c.w.WriteString(`.`)
	c.quoted(col)
}

func (c *compilerContext) table(sel *qcode.Select, schema, table string, alias bool) {
	if schema != "" {
		c.quoted(schema)
		c.w.WriteString(`.`)
	}
	c.quoted(table)
	if alias {
		c.dialect.RenderTableAlias(c, table)
	}
}

func (c *compilerContext) colWithTable(table, col string) {
	c.quoted(table)
	c.w.WriteString(`.`)
	c.quoted(col)
}

func (c *compilerContext) quoted(identifier string) {
	c.w.WriteString(c.dialect.QuoteIdentifier(identifier))
}

func (c *compilerContext) squoted(identifier string) {
	c.w.WriteByte('\'')
	c.w.WriteString(identifier)
	c.w.WriteByte('\'')
}

func int32String(w *bytes.Buffer, val int32) {
	w.WriteString(strconv.FormatInt(int64(val), 10))
}

func (c *compilerContext) Write(s string) (int, error) {
	return c.w.WriteString(s)
}

func (c *compilerContext) WriteString(s string) (int, error) {
	return c.w.WriteString(s)
}

func (c *compilerContext) AddParam(p dialect.Param) string {
	pp := Param{
		Name:        p.Name,
		Type:        p.Type,
		IsArray:     p.IsArray,
		IsNotNull:   p.IsNotNull,
		WrapInArray: p.WrapInArray,
	}
	c.renderParam(pp)
	return ""
}

func (c *compilerContext) Quote(s string) {
	c.quoted(s)
}

func (c *compilerContext) ColWithTable(table, col string) {
	c.colWithTable(table, col)
}

func (c *compilerContext) RenderJSONFields(sel *qcode.Select) {
	c.renderJSONFields(sel)
}

// InlineChildRenderer interface implementations

func (c *compilerContext) RenderTable(sel *qcode.Select, schema, table string, alias bool) {
	c.table(sel, schema, table, alias)
}

func (c *compilerContext) RenderJoin(join qcode.Join) {
	c.renderJoin(join)
}

func (c *compilerContext) RenderLimit(sel *qcode.Select) {
	c.dialect.RenderLimit(c, sel)
}

func (c *compilerContext) RenderOrderBy(sel *qcode.Select) {
	c.dialect.RenderOrderBy(c, sel)
}

func (c *compilerContext) RenderWhereExp(psel, sel *qcode.Select, ex interface{}) {
	if exp, ok := ex.(*qcode.Exp); ok {
		c.renderExp(sel.Ti, exp, false)
	}
}

func (c *compilerContext) RenderExp(ti sdata.DBTable, ex *qcode.Exp) {
	c.renderExp(ti, ex, false)
}

func (c *compilerContext) RenderInlineChild(psel, sel *qcode.Select) {
	c.dialect.RenderInlineChild(c, c, psel, sel)
}

func (c *compilerContext) RenderDefaultInlineChild(sel *qcode.Select) {
	c.renderInlineChild(sel)
}

func (c *compilerContext) GetChild(id int32) *qcode.Select {
	return &c.qc.Selects[id]
}

func (c *compilerContext) Quoted(s string) {
	c.quoted(s)
}

func (c *compilerContext) Squoted(s string) {
	c.squoted(s)
}

func (c *compilerContext) GetConfigVar(name string) (string, bool) {
	val, ok := c.svars[name]
	return val, ok
}

func (c *compilerContext) GetSecPrefix() string {
	return string(c.pf)
}

func (c *compilerContext) GetRootWithCursor() *qcode.Select {
	for _, id := range c.qc.Roots {
		sel := &c.qc.Selects[id]
		if sel.Paging.Cursor {
			return sel
		}
	}
	return nil
}
