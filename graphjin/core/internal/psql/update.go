//nolint:errcheck

package psql

import (
	"github.com/dosco/graphjin/core/internal/qcode"
	"github.com/dosco/graphjin/core/internal/sdata"
)

func (c *compilerContext) renderUpdate() {
	for _, m := range c.qc.Mutates {
		switch {
		case m.Type == qcode.MTUpdate:
			c.renderUpdateStmt(m)
		case m.Rel.Type == sdata.RelOneToOne && m.Type == qcode.MTConnect:
			c.renderOneToOneConnectStmt(m)
		case m.Rel.Type == sdata.RelOneToOne && m.Type == qcode.MTDisconnect:
			c.renderOneToOneDisconnectStmt(m)
		}
	}
	c.w.WriteString(` `)
}

func (c *compilerContext) renderUpdateStmt(m qcode.Mutate) {
	sel := c.qc.Selects[0]

	c.w.WriteString(`, `)
	c.renderCteName(m)
	c.w.WriteString(` AS (`)

	c.renderOneToManyModifiers(m)

	c.w.WriteString(`UPDATE `)
	c.quoted(m.Ti.Name)

	c.w.WriteString(` SET (`)
	n := c.renderInsertUpdateColumns(m, false)
	c.renderNestedRelColumns(m, false, false, n)

	c.w.WriteString(`) = (SELECT `)
	n = c.renderInsertUpdateColumns(m, true)
	c.renderNestedRelColumns(m, true, true, n)

	c.w.WriteString(` FROM _sg_input i`)
	c.renderNestedRelTables(m, true)

	if m.Array {
		c.w.WriteString(`, json_populate_recordset`)
	} else {
		c.w.WriteString(`, json_populate_record`)
	}

	c.w.WriteString(`(NULL::"`)
	c.w.WriteString(m.Ti.Name)
	joinPath(c.w, `", i.j`, m.Path)
	c.w.WriteString(`) t)`)
	// inner select ended

	if m.ParentID == -1 {
		c.w.WriteString(` WHERE `)
		c.renderExp(m.Ti, sel.Where.Exp, false)
	} else {
		// Render sql to set id values if child-to-parent
		// relationship is one-to-one
		rel := m.Rel

		c.w.WriteString(` FROM _sg_input i`)
		// c.quoted(rel.Right.Col.Table)
		// c.w.WriteString(` _x_`)
		// c.w.WriteString(rel.Right.Col.Table)
		c.renderNestedRelTables(m, true)

		if m.Array {
			c.w.WriteString(`, json_populate_recordset`)
		} else {
			c.w.WriteString(`, json_populate_record`)
		}

		c.w.WriteString(`(NULL::"`)
		c.w.WriteString(m.Ti.Name)
		joinPath(c.w, `", i.j`, m.Path)
		c.w.WriteString(`) t`)

		c.w.WriteString(` WHERE ((`)
		c.colWithTable(rel.Left.Col.Table, rel.Left.Col.Name)
		c.w.WriteString(`) = (`)
		c.colWithTable(("_x_" + rel.Right.Col.Table), rel.Right.Col.Name)
		c.w.WriteString(`)`)

		if m.Rel.Type == sdata.RelOneToOne {
			c.w.WriteString(` AND `)
			c.renderExpPath(m.Ti, m.Where.Exp, false, append(m.Path, "where"))
		}
		c.w.WriteString(`)`)
	}

	c.w.WriteString(` RETURNING `)
	c.quoted(m.Ti.Name)
	c.w.WriteString(`.*)`)
}
