//nolint:errcheck

package psql

import (
	"github.com/dosco/super-graph/core/internal/qcode"
	"github.com/dosco/super-graph/core/internal/sdata"
)

func (c *compilerContext) renderUpdate() {
	for _, m := range c.qc.Mutates {
		switch m.Type {
		case qcode.MTUpdate:
			c.renderUpdateStmt(m)
		case qcode.MTConnect:
			c.renderConnectStmt(m)
		case qcode.MTDisconnect:
			c.renderDisconnectStmt(m)
		case qcode.MTUnion:
			c.renderUnionStmt(m)
		}
	}
	c.w.WriteString(` `)
}

func (c *compilerContext) renderUpdateStmt(m qcode.Mutate) {
	c.w.WriteString(`, `)
	renderCteName(c.w, m)
	c.w.WriteString(` AS (`)

	c.w.WriteString(`UPDATE `)
	quoted(c.w, m.Ti.Name)

	c.w.WriteString(` SET (`)
	c.renderInsertUpdateColumns(m, false)
	c.renderNestedInsertUpdateRelColumns(m, true)

	c.w.WriteString(`) = (SELECT `)
	c.renderInsertUpdateColumns(m, true)
	c.renderNestedInsertUpdateRelColumns(m, true)

	c.w.WriteString(` FROM "_sg_input" i`)
	c.renderNestedInsertUpdateRelTables(m)

	if m.Array {
		c.w.WriteString(`, json_populate_recordset`)
	} else {
		c.w.WriteString(`, json_populate_record`)
	}

	c.w.WriteString(`(NULL::`)
	c.w.WriteString(m.Ti.Name)

	if len(m.Path) == 0 {
		c.w.WriteString(`, i.j) t)`)
	} else {
		c.w.WriteString(`, i.j->`)
		joinPath(c.w, m.Path)
		c.w.WriteString(`) t) `)
	}

	if m.ID == 0 {
		c.w.WriteString(` WHERE `)
		c.renderExp(c.qc.Schema, m.Ti, c.qc.Selects[0].Where.Exp, false)
	} else {
		// Render sql to set id values if child-to-parent
		// relationship is one-to-one
		rel := m.RelCP

		c.w.WriteString(`FROM `)
		quoted(c.w, rel.Right.Col.Table)

		c.w.WriteString(` WHERE ((`)
		colWithTable(c.w, rel.Left.Col.Table, rel.Left.Col.Name)
		c.w.WriteString(`) = (`)
		colWithTable(c.w, rel.Right.Col.Table, rel.Right.Col.Name)
		c.w.WriteString(`)`)

		if m.RelPC.Type == sdata.RelOneToMany {
			if _, ok := m.Data["where"]; ok {
				c.w.WriteString(` AND `)
				c.renderWhereFromJSON(m, "where")
			} else if _, ok := m.Data["_where"]; ok {
				c.w.WriteString(` AND `)
				c.renderWhereFromJSON(m, "_where")
			}
		}
		c.w.WriteString(`)`)
	}

	c.w.WriteString(` RETURNING `)
	quoted(c.w, m.Ti.Name)
	c.w.WriteString(`.*)`)
}
