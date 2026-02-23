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
			c.renderUpdateStmt(m, false)
		case m.Rel.Type == sdata.RelOneToOne && m.Type == qcode.MTConnect:
			i = c.renderComma(i)
			c.renderOneToOneConnectStmt(m)
		case m.Rel.Type == sdata.RelOneToOne && m.Type == qcode.MTDisconnect:
			i = c.renderComma(i)
			c.renderOneToOneDisconnectStmt(m)
		}
	}
}

func (c *compilerContext) renderUpdateStmt(m qcode.Mutate, embedded bool) {
	sel := c.qc.Selects[m.SelID]

	n := c.renderOneToManyModifiers(m)
	if n != 0 {
		c.w.WriteString(`, `)
	}

	c.dialect.RenderMutationCTE(c, &m, func() {

		var fromFunc func()

		if m.ParentID != -1 {
			fromFunc = func() {
				if m.IsJSON {
					c.quoted("_sg_input")
					c.w.WriteString(` i`)
					n := c.renderNestedRelTables(m, true, 1)
					c.renderMutateToRecordSet(m, n)
				} else {
					c.renderNestedRelTables(m, true, 0)
				}
			}
		} else if m.IsJSON {
			fromFunc = func() {
				c.quoted("_sg_input")
				c.w.WriteString(` i`)
				c.renderMutateToRecordSet(m, 1)
			}
		}

		c.dialect.RenderUpdate(c, &m, func() {
			i := 0
			for _, col := range m.Cols {
				if i != 0 {
					c.w.WriteString(`, `)
				}
				c.w.WriteString(col.Col.Name)
				c.w.WriteString(` = `)
				c.renderColumnValue(m, col)
				i++
			}
		}, fromFunc, func() {
			if m.ParentID != -1 {
				rel := m.Rel
				c.w.WriteString(`((`)
				c.colWithTable(rel.Left.Col.Table, rel.Left.Col.Name)
				c.w.WriteString(`) = (`)
				c.colWithTable(("_x_" + rel.Right.Col.Table), rel.Right.Col.Name)
				c.w.WriteString(`))`)

				if m.Rel.Type == sdata.RelOneToOne {
					c.w.WriteString(` AND `)
					c.renderExpPath(m.Ti, m.Where.Exp, false, append(m.Path, "where"))
				}
			} else {
				c.renderExp(m.Ti, sel.Where.Exp, false)
			}
		})

		if !embedded {
			c.dialect.RenderReturning(c, &m)
		}
	})
}
