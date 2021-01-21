//nolint:errcheck

package psql

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/dosco/graphjin/core/internal/qcode"
	"github.com/dosco/graphjin/core/internal/sdata"
)

func (co *Compiler) compileMutation(
	w *bytes.Buffer,
	qc *qcode.QCode,
	md *Metadata) {

	c := compilerContext{
		md:       md,
		w:        w,
		qc:       qc,
		Compiler: co,
	}

	if qc.SType != qcode.QTDelete {
		c.w.WriteString(`WITH _sg_input AS (SELECT `)
		c.renderParam(Param{Name: c.qc.ActionVar, Type: "json"})
		c.w.WriteString(` :: json AS j)`)
	}

	switch qc.SType {
	case qcode.QTInsert:
		c.renderInsert()
	case qcode.QTUpdate:
		c.renderUpdate()
	case qcode.QTUpsert:
		c.renderUpsert()
	case qcode.QTDelete:
		c.renderDelete()
	default:
		return
	}

	c.renderUnionStmt()
	co.CompileQuery(w, qc, c.md)
}

func (c *compilerContext) renderUnionStmt() {
	for k, cids := range c.qc.MUnions {
		if len(cids) < 2 {
			continue
		}
		c.w.WriteString(`, `)
		c.quoted(k)
		c.w.WriteString(` AS (`)

		i := 0
		for _, id := range cids {
			m := c.qc.Mutates[id]
			if m.Rel.Type == sdata.RelOneToMany &&
				(m.Type == qcode.MTConnect || m.Type == qcode.MTDisconnect) {
				continue
			}
			if i != 0 {
				c.w.WriteString(` UNION ALL `)
			}
			c.w.WriteString(`SELECT * FROM `)
			c.renderCteName(m)
			i++
		}

		c.w.WriteString(`)`)
	}
}

func (c *compilerContext) renderInsertUpdateColumns(m qcode.Mutate, values bool) int {
	i := 0
	for _, col := range m.Cols {
		if i != 0 {
			c.w.WriteString(`, `)
		}
		i++

		if values {
			// v will be a blank strings unless the value is from a preset
			v := col.Value

			if len(v) > 1 && v[0] == '$' {
				if v1, ok := c.svars[v[1:]]; ok {
					v = v1
				}
			}

			switch {
			case len(v) > 1 && v[0] == '$':
				c.renderParam(Param{Name: v[1:], Type: col.Col.Type})

			case strings.HasPrefix(v, "sql:"):
				c.w.WriteString(`(`)
				c.renderVar(v[4:])
				c.w.WriteString(`)`)

			case v != "":
				c.squoted(v)

			default:
				colWithTable(c.w, "t", col.FieldName)
				continue
			}

			c.w.WriteString(` :: `)
			c.w.WriteString(col.Col.Type)

		} else {
			c.quoted(col.Col.Name)
		}
	}
	return i
}

func (c *compilerContext) renderNestedRelColumns(m qcode.Mutate, values bool, prefix bool, n int) {
	for i, col := range m.RCols {
		if n != 0 || i != 0 {
			c.w.WriteString(`, `)
		}
		if values {
			if col.Col.Array {
				c.w.WriteString(`ARRAY(SELECT `)
				c.quoted(col.VCol.Name)
				c.w.WriteString(` FROM `)
				c.quoted(col.VCol.Table)
				c.w.WriteString(`)`)
			} else {
				if prefix {
					c.w.WriteString(`_x_`)
				}
				colWithTable(c.w, col.VCol.Table, col.VCol.Name)
			}
		} else {
			c.quoted(col.Col.Name)
		}
	}
}

func (c *compilerContext) renderNestedRelTables(m qcode.Mutate, prefix bool) {
	for id := range m.DependsOn {
		d := c.qc.Mutates[id]
		c.w.WriteString(`, `)
		if d.Multi {
			c.renderCteNameWithID(d)
		} else {
			c.quoted(d.Ti.Name)
		}
		if prefix {
			c.w.WriteString(` _x_`)
			c.w.WriteString(d.Ti.Name)
		} else {
			c.w.WriteString(` `)
			c.quoted(d.Ti.Name)
		}
	}
}

func (c *compilerContext) renderUpsert() {
	sel := c.qc.Selects[0]

	c.renderInsert()
	c.w.WriteString(` ON CONFLICT (`)
	m := c.qc.Mutates[0]

	i := 0
	for _, col := range m.Cols {
		if !col.Col.UniqueKey || !col.Col.PrimaryKey {
			continue
		}
		if i != 0 {
			c.w.WriteString(`, `)
		}
		c.w.WriteString(col.Col.Name)
		i++
	}
	if i == 0 {
		c.w.WriteString(sel.Ti.PrimaryCol.Name)
	}
	c.w.WriteString(`)`)

	c.w.WriteString(` DO UPDATE SET `)

	for i, col := range m.Cols {
		if i != 0 {
			c.w.WriteString(`, `)
		}
		c.w.WriteString(col.Col.Name)
		c.w.WriteString(` = EXCLUDED.`)
		c.w.WriteString(col.Col.Name)
	}

	c.w.WriteString(` WHERE `)
	c.renderExp(c.qc.Schema, m.Ti, c.qc.Selects[0].Where.Exp, false)
	c.w.WriteString(` RETURNING *) `)
}

func (c *compilerContext) renderDelete() {
	sel := c.qc.Selects[0]

	c.w.WriteString(`WITH `)
	c.quoted(sel.Table)

	c.w.WriteString(` AS (DELETE FROM `)
	c.quoted(sel.Table)
	c.w.WriteString(` WHERE `)
	c.renderExp(c.qc.Schema, sel.Ti, sel.Where.Exp, false)

	c.w.WriteString(` RETURNING `)
	c.quoted(sel.Table)
	c.w.WriteString(`.*) `)
}

func (c *compilerContext) renderOneToManyConnectStmt(m qcode.Mutate) {
	// Render only for parent-to-child relationship of one-to-one
	// For this to work the json child needs to found first so it's primary key
	// can be set in the related column on the parent object.
	// Eg. Create product and connect a user to it.
	c.renderCteName(m)
	c.w.WriteString(` AS (SELECT `)

	rel := m.Rel
	if rel.Right.Col.Array {
		c.w.WriteString(`array_agg(DISTINCT `)
		c.quoted(rel.Left.Col.Name)
		c.w.WriteString(`) AS `)
		c.quoted(rel.Left.Col.Name)
	} else {
		c.quoted(rel.Left.Col.Name)
	}

	c.w.WriteString(` FROM _sg_input i, `)
	c.quoted(m.Ti.Name)

	c.w.WriteString(` WHERE `)
	c.renderWhereFromJSON(m)
	c.w.WriteString(` LIMIT 1)`)
}

func (c *compilerContext) renderOneToOneConnectStmt(m qcode.Mutate) {
	c.w.WriteString(`, `)
	c.renderCteName(m)
	c.w.WriteString(` AS ( UPDATE `)

	c.quoted(m.Ti.Name)
	c.w.WriteString(` SET `)
	c.quoted(m.Rel.Left.Col.Name)
	c.w.WriteString(` = _x_`)
	colWithTable(c.w, m.Rel.Right.Col.Table, m.Rel.Right.Col.Name)

	c.w.WriteString(` FROM _sg_input i`)
	c.renderNestedRelTables(m, true)

	c.w.WriteString(` WHERE `)
	c.renderWhereFromJSON(m)

	c.w.WriteString(` RETURNING `)
	c.quoted(m.Ti.Name)
	c.w.WriteString(`.*)`)
}

func (c *compilerContext) renderOneToManyDisconnectStmt(m qcode.Mutate) {
	c.renderCteName(m)
	c.w.WriteString(` AS (`)

	rel := m.Rel
	if rel.Left.Col.Array {
		c.w.WriteString(`SELECT NULL AS `)
		c.quoted(rel.Left.Col.Name)
	} else {
		c.w.WriteString(`SELECT `)
		c.quoted(rel.Left.Col.Name)
		c.w.WriteString(` FROM _sg_input i,`)
		c.quoted(m.Ti.Name)
		c.w.WriteString(` WHERE `)
		c.renderWhereFromJSON(m)
	}

	c.w.WriteString(` LIMIT 1))`)
}

func (c *compilerContext) renderOneToOneDisconnectStmt(m qcode.Mutate) {
	// Render only for parent-to-child relationship of one-to-one
	// For this to work the child needs to found first so it's
	// null value can beset in the related column on the parent object.
	// Eg. Update product and diconnect the user from it.
	c.w.WriteString(`, `)
	c.renderCteName(m)
	c.w.WriteString(` AS ( UPDATE `)

	c.quoted(m.Ti.Name)
	c.w.WriteString(` SET `)
	c.quoted(m.Rel.Left.Col.Name)
	c.w.WriteString(` = `)

	if m.Rel.Left.Col.Array {
		c.w.WriteString(` array_remove(`)
		c.quoted(m.Rel.Left.Col.Name)
		c.w.WriteString(`, _x_`)
		colWithTable(c.w, m.Rel.Right.Col.Table, m.Rel.Right.Col.Name)
		c.w.WriteString(`)`)
	} else {
		c.w.WriteString(` NULL`)
	}

	c.w.WriteString(` FROM _sg_input i`)
	c.renderNestedRelTables(m, true)

	c.w.WriteString(` WHERE ((`)
	colWithTable(c.w, m.Rel.Left.Col.Table, m.Rel.Left.Col.Name)
	c.w.WriteString(`) = (_x_`)
	colWithTable(c.w, m.Rel.Right.Col.Table, m.Rel.Right.Col.Name)
	c.w.WriteString(`)`)

	if m.Rel.Type == sdata.RelOneToOne {
		c.w.WriteString(` AND `)
		c.renderWhereFromJSON(m)
	}
	c.w.WriteString(`)`)

	c.w.WriteString(` RETURNING `)
	c.quoted(m.Ti.Name)
	c.w.WriteString(`.*)`)
}

func (c *compilerContext) renderOneToManyModifiers(m qcode.Mutate) {
	renderPrefix := func(i int) {
		if i == 0 {
			c.w.WriteString(`WITH `)
		} else {
			c.w.WriteString(`, `)
		}
	}

	i := 0
	for id := range m.DependsOn {
		m1 := c.qc.Mutates[id]

		switch m1.Type {
		case qcode.MTConnect:
			renderPrefix(i)
			c.renderOneToManyConnectStmt(m1)
			i++
		case qcode.MTDisconnect:
			renderPrefix(i)
			c.renderOneToManyDisconnectStmt(m1)
			i++
		}
		if i != 0 {
			c.w.WriteString(` `)
		}
	}
}

func (c *compilerContext) renderWhereFromJSON(m qcode.Mutate) {
	var kv map[string]json.RawMessage

	//TODO: Move this json parsing into qcode
	if err := json.Unmarshal(m.Val, &kv); err != nil {
		return
	}

	i := 0
	for k, v := range kv {
		col, err := m.Ti.GetColumn(k)
		if err != nil {
			continue
		}
		if i != 0 {
			c.w.WriteString(` AND `)
		}

		if v[0] == '[' {
			colWithTable(c.w, col.Table, col.Name)

			if col.Array {
				c.w.WriteString(` && `)
			} else {
				c.w.WriteString(` = `)
			}

			c.w.WriteString(`ANY((select a::`)
			c.w.WriteString(col.Type)

			c.w.WriteString(` AS list from json_array_elements_text(`)
			renderPathJSON(c.w, m, k)
			c.w.WriteString(`::json) AS a))`)

		} else if col.Array {
			c.w.WriteString(`(`)
			renderPathJSON(c.w, m, k)
			c.w.WriteString(`)::`)
			c.w.WriteString(col.Type)

			c.w.WriteString(` = ANY(`)
			colWithTable(c.w, m.Ti.Name, k)
			c.w.WriteString(`)`)

		} else {
			colWithTable(c.w, m.Ti.Name, k)
			c.w.WriteString(` = (`)
			renderPathJSON(c.w, m, k)
			c.w.WriteString(`)::`)
			c.w.WriteString(col.Type)
		}
		i++
	}
}

func (c *compilerContext) renderCteName(m qcode.Mutate) {
	if m.Multi {
		c.renderCteNameWithID(m)
	} else {
		c.w.WriteString(m.Ti.Name)
	}
}

func (c *compilerContext) renderCteNameWithID(m qcode.Mutate) {
	c.w.WriteString(m.Ti.Name)
	c.w.WriteString(`_`)
	int32String(c.w, m.ID)
}

func renderPathJSON(w *bytes.Buffer, m qcode.Mutate, k string) {
	w.WriteString(`(i.j->`)
	joinPath(w, m.Path)
	w.WriteString(`->>'`)
	w.WriteString(k)
	w.WriteString(`')`)
}

func joinPath(w *bytes.Buffer, path []string) {
	for i := range path {
		if i != 0 {
			w.WriteString(`->`)
		}
		w.WriteString(`'`)
		w.WriteString(path[i])
		w.WriteString(`'`)
	}
}
