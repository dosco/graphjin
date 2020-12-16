//nolint:errcheck

package psql

import (
	"strconv"

	"github.com/dosco/graphjin/core/internal/qcode"
	"github.com/dosco/graphjin/core/internal/sdata"
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
		}
	}
	c.w.WriteString(` `)
}

func (c *compilerContext) renderUpdateStmt(m qcode.Mutate) {
	c.w.WriteString(`, `)
	if m.Multi {
		renderCteNameWithSuffix(c.w, m, strconv.Itoa(int(m.MID)))
	} else {
		renderCteName(c.w, m)
	}
	c.w.WriteString(` AS (`)

	c.w.WriteString(`UPDATE `)
	quoted(c.w, m.Ti.Name)

	c.w.WriteString(` SET (`)
	n := c.renderInsertUpdateColumns(m, false)
	c.renderNestedInsertUpdateRelColumns(m, true, n)

	c.w.WriteString(`) = (SELECT `)
	n = c.renderInsertUpdateColumns(m, true)
	c.renderNestedInsertUpdateRelColumns(m, true, n)

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
		c.w.WriteString(`", i.j) t)`)
	} else {
		c.w.WriteString(`", i.j->`)
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
			}
		}
		c.w.WriteString(`)`)
	}

	c.w.WriteString(` RETURNING `)
	quoted(c.w, m.Ti.Name)
	c.w.WriteString(`.*)`)
}
