//nolint:errcheck
package psql

import (
	"github.com/dosco/graphjin/core/internal/qcode"
	"github.com/dosco/graphjin/core/internal/sdata"
)

func (c *compilerContext) renderInsert() {
	i := 0
	for _, m := range c.qc.Mutates {
		switch {
		case m.Type == qcode.MTInsert:
			i = c.renderComma(i)
			c.renderInsertStmt(m, false)
		case m.Type == qcode.MTUpsert:
			i = c.renderComma(i)
			c.renderInsertStmt(m, true)
		case m.Rel.Type == sdata.RelOneToOne && m.Type == qcode.MTConnect:
			i = c.renderComma(i)
			c.renderOneToOneConnectStmt(m)
		}
	}
}

func (c *compilerContext) renderInsertStmt(m qcode.Mutate, embedded bool) {
	c.renderCteName(m)
	c.w.WriteString(` AS (`)

	c.renderOneToManyModifiers(m)

	c.w.WriteString(`INSERT INTO `)
	c.quoted(m.Ti.Name)

	c.w.WriteString(` (`)
	n := c.renderInsertUpdateColumns(m)
	c.renderNestedRelColumns(m, false, false, n)
	c.w.WriteString(`)`)

	c.renderValues(m, false)

	if !embedded {
		c.w.WriteString(` RETURNING *)`)
	}
}
