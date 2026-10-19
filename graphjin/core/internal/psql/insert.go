//nolint:errcheck
package psql

import (
	"fmt"

	"github.com/dosco/graphjin/core/v3/internal/dialect"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
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
	n := c.renderOneToManyModifiers(m)
	if n != 0 {
		c.w.WriteString(`, `)
	}

	if m.ConflictAction == qcode.ConflictGet {
		c.renderInsertConflictGet(m)
		return
	}

	c.dialect.RenderMutationCTE(c, &m, func() {

		c.dialect.RenderInsert(c, &m, func() {
			n := c.renderInsertUpdateColumns(m)
			c.renderNestedRelColumns(m, false, false, n)
		})

		c.renderValues(m, false)

		if !embedded {
			c.dialect.RenderReturning(c, &m)
		}
	})
}

func (c *compilerContext) renderInsertConflictGet(m qcode.Mutate) {
	renderer, ok := c.dialect.(dialect.InsertConflictGetRenderer)
	if !ok {
		c.SetError(fmt.Errorf("on_conflict: get is not supported by the %s dialect", c.dialect.Name()))
		return
	}

	renderInsert := func() {
		c.dialect.RenderInsert(c, &m, func() {
			n := c.renderInsertUpdateColumns(m)
			c.renderNestedRelColumns(m, false, false, n)
		})
		c.renderValues(m, false)
		renderer.RenderInsertConflictGetClause(c, &m)
		c.dialect.RenderReturning(c, &m)
	}

	if renderer.InsertConflictGetMode() == dialect.InsertConflictGetNative {
		c.dialect.RenderMutationCTE(c, &m, renderInsert)
		return
	}

	if renderer.InsertConflictGetMode() != dialect.InsertConflictGetWritableCTE {
		c.SetError(fmt.Errorf("on_conflict: get cannot use the %s non-linear mutation path", c.dialect.Name()))
		return
	}

	c.qc.InsertConflictFallback = true
	inserted := fmt.Sprintf("_gj_inserted_%d", m.ID)
	c.quoted(inserted)
	c.w.WriteString(` AS (`)
	renderInsert()
	c.w.WriteString(`), `)
	c.renderCteName(m)
	c.w.WriteString(` AS (SELECT * FROM `)
	c.quoted(inserted)
	c.w.WriteString(` UNION ALL SELECT * FROM `)
	c.colWithTable(m.Ti.Schema, m.Ti.Name)
	c.w.WriteString(` AS `)
	c.quoted("_gj_existing")
	c.w.WriteString(` WHERE `)
	c.renderInsertConflictWhere(m, "_gj_existing")
	c.w.WriteString(` LIMIT 1)`)
}

func (c *compilerContext) renderInsertConflictWhere(m qcode.Mutate, table string) {
	for i, col := range m.ConflictCols {
		if i != 0 {
			c.w.WriteString(` AND `)
		}
		c.colWithTable(table, col.Col.Name)
		c.w.WriteString(` IS NOT DISTINCT FROM `)
		if !m.IsJSON {
			c.renderColumnValue(m, col)
			continue
		}

		c.w.WriteString(`(SELECT `)
		c.renderColumnValue(m, col)
		c.w.WriteString(` FROM `)
		c.quoted("_sg_input")
		c.w.WriteString(` i, `)
		c.renderMutateToRecordSet(m, 0)
		c.w.WriteString(` LIMIT 1)`)
	}
}
