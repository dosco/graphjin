package psql

import (
	"bytes"
	"strconv"
)

func (c *compilerContext) alias(alias string) {
	c.w.WriteString(` AS `)
	c.quoted(alias)
}

func (c *compilerContext) aliasWithID(alias string, id int32) {
	c.w.WriteString(` AS `)
	c.quoted(alias + "_" + strconv.Itoa(int(id)))
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

func (c *compilerContext) table(schema, table string, alias bool) {
	if schema != "" {
		c.quoted(schema)
		c.w.WriteString(`.`)
	}
	c.quoted(table)
	if alias {
		c.w.WriteString(` AS `)
		c.quoted(table)
	}
}

func (c *compilerContext) colWithTable(table, col string) {
	c.quoted(table)
	c.w.WriteString(`.`)
	c.quoted(col)
}

func (c *compilerContext) quoted(identifier string) {
	switch c.ct {
	case "mysql":
		c.w.WriteByte('`')
		c.w.WriteString(identifier)
		c.w.WriteByte('`')
	default:
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
