//nolint:errcheck

package psql

import (
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func (c *compilerContext) renderUpdate() {
	i := 0
	for _, m := range c.qc.Mutates {
		switch {
		case m.Type == qcode.MTUpdate:
			i = c.renderComma(i)
			c.renderUpdateStmt(m)
		case m.Rel.Type == sdata.RelOneToOne && m.Type == qcode.MTConnect:
			i = c.renderComma(i)
			c.renderOneToOneConnectStmt(m)
		case m.Rel.Type == sdata.RelOneToOne && m.Type == qcode.MTDisconnect:
			i = c.renderComma(i)
			c.renderOneToOneDisconnectStmt(m)
		}
	}
}

func (c *compilerContext) renderUpdateStmt(m qcode.Mutate) {
	sel := c.qc.Selects[0]

	c.renderCteName(m)
	c.w.WriteString(` AS (`)

	c.renderOneToManyModifiers(m)

	c.w.WriteString(`UPDATE `)
	c.table(m.Ti.Schema, m.Ti.Name, false)

	c.w.WriteString(` SET (`)
	n := c.renderInsertUpdateColumns(m)
	c.renderNestedRelColumns(m, false, false, n)

	c.w.WriteString(`) = (`)
	c.renderValues(m, true)
	c.w.WriteString(`)`)
	// inner select ended

	if m.ParentID == -1 {
		c.w.WriteString(` WHERE `)
		c.renderExp(m.Ti, sel.Where.Exp, false)
	} else {
		// Render sql to set id values if child-to-parent
		// relationship is one-to-one
		rel := m.Rel

		if m.IsJSON {
			c.w.WriteString(` FROM _sg_input i`)
			n = c.renderNestedRelTables(m, true, 1)
			c.renderMutateToRecordSet(m, n)
		} else {
			c.w.WriteString(` FROM `)
			c.renderNestedRelTables(m, true, 0)
		}

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
	c.renderReturning(m)
}
