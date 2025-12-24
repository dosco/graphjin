package psql

import (
	"bytes"
	"strconv"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/dialect"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
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
	if c.dialect.Name() == "mysql" {
		c.w.WriteByte('`')
		c.w.WriteString(identifier)
		c.w.WriteByte('`')
	} else if c.dialect.Name() == "oracle" {
		c.w.WriteByte('"')
		c.w.WriteString(strings.ToUpper(identifier))
		c.w.WriteByte('"')
	} else {
		c.w.WriteByte('"')
		c.w.WriteString(identifier)
		c.w.WriteByte('"')
	}
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
