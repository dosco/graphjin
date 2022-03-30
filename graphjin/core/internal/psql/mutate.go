//nolint:errcheck

package psql

import (
	"bytes"
	"strings"

	"github.com/dosco/graphjin/core/internal/graph"
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
		isJSON:   qc.Mutates[0].IsJSON,
		Compiler: co,
	}

	if qc.SType != qcode.QTDelete {
		if c.isJSON {
			c.w.WriteString(`WITH _sg_input AS (SELECT `)
			c.renderParam(Param{Name: qc.ActionVar, Type: "json"})
			c.w.WriteString(` :: json AS j), `)
		} else {
			c.w.WriteString(`WITH `)
		}
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

func (c *compilerContext) renderInsertUpdateValues(m qcode.Mutate) int {
	i := 0

	render := func(data *graph.Node) {
		for _, col := range m.Cols {
			if i != 0 {
				c.w.WriteString(`, `)
			}
			i++

			field := m.Data.CMap[col.FieldName]
			// v will be a blank strings unless the value is from a preset
			v := field.Val

			if field.Type == graph.NodeVar {
				if v1, ok := c.svars[v]; ok {
					v = v1
				}
			}

			switch {
			case field.Type == graph.NodeVar:
				c.renderParam(Param{Name: v, Type: col.Col.Type})

			case strings.HasPrefix(v, "sql:"):
				c.w.WriteString(`(`)
				c.renderVar(v[4:])
				c.w.WriteString(`)`)

			default:
				c.squoted(v)
			}

			c.w.WriteString(` :: `)
			c.w.WriteString(col.Col.Type)

		}
	}

	c.w.WriteString(` (`)
	render(m.Data)
	c.w.WriteString(`)`)

	return i
}

func (c *compilerContext) renderInsertUpdateColumns(m qcode.Mutate, values bool) int {
	i := 0

	for _, col := range m.Cols {
		if i != 0 {
			c.w.WriteString(`, `)
		}
		i++

		if !values {
			c.quoted(col.Col.Name)
			continue
		}

		// v will be a blank strings unless the value is from a preset
		v := col.Value

		if v != "" && v[0] == '$' {
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

		case m.IsJSON:
			c.colWithTable("t", col.FieldName)

		default:
			c.w.WriteString(v)
		}

		c.w.WriteString(` :: `)
		c.w.WriteString(col.Col.Type)
	}
	return i
}

func (c *compilerContext) willBeArray(index int) bool {
	m1 := c.qc.Mutates[index]

	if m1.Type == qcode.MTConnect || m1.Type == qcode.MTDisconnect {
		return true
	}
	return false
}

func (c *compilerContext) renderNestedRelColumns(m qcode.Mutate, values bool, prefix bool, n int) {
	for i, col := range m.RCols {
		if n != 0 || i != 0 {
			c.w.WriteString(`, `)
		}
		if values {
			if col.Col.Array {
				if !c.willBeArray(i) {
					c.w.WriteString(`ARRAY(SELECT `)
				} else {
					c.w.WriteString(`(SELECT `)
				}
				c.quoted(col.VCol.Name)
				c.w.WriteString(` FROM `)
				c.quoted(col.VCol.Table)
				c.w.WriteString(`)`)
			} else {
				if prefix {
					c.colWithTable(("_x_" + col.VCol.Table), col.VCol.Name)
				} else {
					c.colWithTable(col.VCol.Table, col.VCol.Name)
				}
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
		if !col.Col.UniqueKey && !col.Col.PrimaryKey {
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
	c.renderExp(m.Ti, sel.Where.Exp, false)
	c.w.WriteString(` RETURNING *) `)
}

func (c *compilerContext) renderDelete() {
	sel := c.qc.Selects[0]

	c.w.WriteString(`WITH `)
	c.quoted(sel.Table)

	c.w.WriteString(` AS (DELETE FROM `)
	c.quoted(sel.Table)
	c.w.WriteString(` WHERE `)
	c.renderExp(sel.Ti, sel.Where.Exp, false)

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
		c.w.WriteString(`ARRAY_AGG(DISTINCT `)
		c.quoted(rel.Left.Col.Name)
		c.w.WriteString(`) AS `)
		c.quoted(rel.Left.Col.Name)
	} else {
		c.quoted(rel.Left.Col.Name)
	}

	c.w.WriteString(` FROM _sg_input i, `)
	c.quoted(m.Ti.Name)

	c.w.WriteString(` WHERE `)
	c.renderExpPath(m.Ti, m.Where.Exp, false, m.Path)
	c.w.WriteString(` LIMIT 1)`)
}

func (c *compilerContext) renderOneToOneConnectStmt(m qcode.Mutate) {
	c.renderCteName(m)
	c.w.WriteString(` AS ( UPDATE `)

	c.quoted(m.Ti.Name)
	c.w.WriteString(` SET `)
	c.quoted(m.Rel.Left.Col.Name)
	c.w.WriteString(` = `)
	c.colWithTable(("_x_" + m.Rel.Right.Col.Table), m.Rel.Right.Col.Name)

	c.w.WriteString(` FROM _sg_input i`)
	c.renderNestedRelTables(m, true)

	c.w.WriteString(` WHERE `)
	c.renderExpPath(m.Ti, m.Where.Exp, false, m.Path)

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

		c.w.WriteString(` FROM _sg_input i, `)
		c.quoted(m.Ti.Name)

		c.w.WriteString(` WHERE `)
		c.renderExpPath(m.Ti, m.Where.Exp, false, m.Path)
	}

	c.w.WriteString(` LIMIT 1))`)
}

func (c *compilerContext) renderOneToOneDisconnectStmt(m qcode.Mutate) {
	// Render only for parent-to-child relationship of one-to-one
	// For this to work the child needs to found first so it's
	// null value can beset in the related column on the parent object.
	// Eg. Update product and diconnect the user from it.
	c.renderCteName(m)
	c.w.WriteString(` AS ( UPDATE `)

	c.quoted(m.Ti.Name)
	c.w.WriteString(` SET `)
	c.quoted(m.Rel.Left.Col.Name)
	c.w.WriteString(` = `)

	if m.Rel.Left.Col.Array {
		c.w.WriteString(` array_remove(`)
		c.quoted(m.Rel.Left.Col.Name)
		c.w.WriteString(`, `)
		c.colWithTable(("_x_" + m.Rel.Right.Col.Table), m.Rel.Right.Col.Name)
		c.w.WriteString(`)`)
	} else {
		c.w.WriteString(` NULL`)
	}

	c.w.WriteString(` FROM _sg_input i`)
	c.renderNestedRelTables(m, true)

	c.w.WriteString(` WHERE ((`)
	c.colWithTable(m.Rel.Left.Col.Table, m.Rel.Left.Col.Name)
	c.w.WriteString(`) = (`)
	c.colWithTable(("_x_" + m.Rel.Right.Col.Table), m.Rel.Right.Col.Name)
	c.w.WriteString(`)`)

	if m.Rel.Type == sdata.RelOneToOne {
		c.w.WriteString(` AND `)
		c.renderExpPath(m.Ti, m.Where.Exp, false, m.Path)
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

func (c *compilerContext) renderCteName(m qcode.Mutate) {
	if m.Multi {
		c.renderCteNameWithID(m)
	} else {
		c.quoted(m.Ti.Name)
	}
}

func (c *compilerContext) renderCteNameWithID(m qcode.Mutate) {
	c.w.WriteString(m.Ti.Name)
	c.w.WriteString(`_`)
	int32String(c.w, m.ID)
}

func (c *compilerContext) renderValues(m qcode.Mutate, prefix bool) {
	if m.IsJSON {
		c.w.WriteString(` SELECT `)
		n := c.renderInsertUpdateColumns(m, true)
		c.renderNestedRelColumns(m, true, prefix, n)

		c.w.WriteString(` FROM _sg_input i`)
		c.renderNestedRelTables(m, prefix)
		c.renderMutateToRecordSet(m)
	} else {
		c.w.WriteString(` VALUES `)
		c.renderInsertUpdateValues(m)
	}
}

func (c *compilerContext) renderMutateToRecordSet(m qcode.Mutate) {
	if m.IsArray {
		c.w.WriteString(`, json_to_recordset`)
	} else {
		c.w.WriteString(`, json_to_record`)
	}

	c.w.WriteString(`(`)
	joinPath(c.w, `i.j`, m.Path)
	c.w.WriteString(`) as t(`)

	i := 0
	for _, col := range m.Cols {
		if col.Value != "" {
			continue
		}
		if i != 0 {
			c.w.WriteString(`, `)
		}
		c.quoted(col.FieldName)
		c.w.WriteString(` `)
		c.w.WriteString(col.Col.Type)
		i++
	}
	c.w.WriteString(`)`)
}

func (c *compilerContext) renderComma(i int) int {
	if i != 0 {
		c.w.WriteString(`, `)
	}
	return i + 1
}

func joinPath(w *bytes.Buffer, prefix string, path []string) {
	w.WriteString(prefix)
	for i := range path {
		w.WriteString(`->`)
		w.WriteString(`'`)
		w.WriteString(path[i])
		w.WriteString(`'`)
	}
}
