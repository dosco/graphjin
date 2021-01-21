package psql

import (
	"bytes"
	"strconv"
)

func (c *compilerContext) alias(alias string) {
	c.w.WriteString(` AS `)
	c.quoted(alias)
}

func aliasWithID(w *bytes.Buffer, alias string, id int32) {
	w.WriteString(` AS `)
	w.WriteString(alias)
	w.WriteString(`_`)
	int32String(w, id)
}

func colWithTable(w *bytes.Buffer, table, col string) {
	w.WriteString(table)
	w.WriteString(`.`)
	w.WriteString(col)
}

func colWithTableID(w *bytes.Buffer, table string, id int32, col string) {
	w.WriteString(table)
	if id >= 0 {
		w.WriteString(`_`)
		int32String(w, id)
	}
	w.WriteString(`.`)
	w.WriteString(col)
}

func (c *compilerContext) quoted(identifier string) {
	switch c.md.ct {
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
