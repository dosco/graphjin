//nolint:errcheck
package psql

import (
	"github.com/dosco/super-graph/core/internal/qcode"
)

func (c *compilerContext) renderInsert() {
	for _, m := range c.qc.Mutates {
		switch m.Type {
		case qcode.MTInsert:
			c.renderInsertStmt(m, false)
		case qcode.MTUpsert:
			c.renderInsertStmt(m, true)
		case qcode.MTConnect:
			c.renderConnectStmt(m)
		case qcode.MTUnion:
			c.renderUnionStmt(m)
		}
	}
	c.w.WriteString(` `)
}

func (c *compilerContext) renderInsertStmt(m qcode.Mutate, embedded bool) {
	c.w.WriteString(`, `)
	renderCteName(c.w, m)
	c.w.WriteString(` AS (`)

	c.w.WriteString(`INSERT INTO `)
	quoted(c.w, m.Ti.Name)

	c.w.WriteString(` (`)
	c.renderInsertUpdateColumns(m, false)
	c.renderNestedInsertUpdateRelColumns(m, false)
	c.w.WriteString(`)`)

	c.w.WriteString(` SELECT `)
	c.renderInsertUpdateColumns(m, true)
	c.renderNestedInsertUpdateRelColumns(m, true)

	c.w.WriteString(` FROM "_sg_input" i`)
	c.renderNestedInsertUpdateRelTables(m)

	if m.Array {
		c.w.WriteString(`, json_populate_recordset`)
	} else {
		c.w.WriteString(`, json_populate_record`)
	}

	c.w.WriteString(`(NULL::"`)
	c.w.WriteString(m.Ti.Name)

	if len(m.Path) == 0 {
		c.w.WriteString(`", i.j) t`)
	} else {
		c.w.WriteString(`", i.j->`)
		joinPath(c.w, m.Path)
		c.w.WriteString(`) t`)
	}

	if !embedded {
		c.w.WriteString(` RETURNING *)`)
	}
}
