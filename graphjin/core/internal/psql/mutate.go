//nolint:errcheck

package psql

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/dosco/graphjin/core/internal/qcode"
	"github.com/dosco/graphjin/core/internal/sdata"
)

func (co *Compiler) compileMutation(
	w *bytes.Buffer,
	qc *qcode.QCode,
	md Metadata) Metadata {

	c := compilerContext{
		md:       md,
		w:        w,
		qc:       qc,
		Compiler: co,
	}

	if qc.SType != qcode.QTDelete {
		c.w.WriteString(`WITH "_sg_input" AS (SELECT `)
		c.md.renderParam(c.w, Param{Name: c.qc.ActionVar, Type: "json"})
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
		return c.md
	}

	c.renderMultiUnionStmt()
	return co.CompileQuery(w, qc, c.md)
}

func (c *compilerContext) renderMultiUnionStmt() {
	for k, n := range c.qc.MCounts {
		if n == 1 {
			continue
		}
		c.w.WriteString(`, `)
		quoted(c.w, k)
		c.w.WriteString(` AS (`)

		for i := int32(0); i < n; i++ {
			if i != 0 {
				c.w.WriteString(` UNION ALL `)
			}
			c.w.WriteString(`SELECT * FROM `)
			renderNameWithSuffix(c.w, k, strconv.Itoa(int(i)))
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
			v := col.Value

			if len(v) > 1 && v[0] == '$' {
				if v1, ok := c.vars[v[1:]]; ok {
					v = v1
				}
			}

			switch {
			case len(v) > 1 && v[0] == '$':
				c.md.renderParam(c.w, Param{Name: v[1:], Type: col.Col.Type})

			case strings.HasPrefix(v, "sql:"):
				c.w.WriteString(`(`)
				c.md.RenderVar(c.w, v[4:])
				c.w.WriteString(`)`)

			case v != "":
				squoted(c.w, v)

			default:
				colWithTable(c.w, "t", col.FieldName)
				continue
			}

			c.w.WriteString(` :: `)
			c.w.WriteString(col.Col.Type)

		} else {
			quoted(c.w, col.Col.Name)
		}
	}
	return i
}

func (c *compilerContext) renderNestedInsertUpdateRelColumns(m qcode.Mutate, values bool, n int) {
	for i, col := range m.RCols {
		if n != 0 || i != 0 {
			c.w.WriteString(`, `)
		}
		if values {
			if (col.CType & qcode.CTConnect) != 0 {
				c.w.WriteString(`"_x_`)
				c.w.WriteString(col.VCol.Table)
				c.w.WriteString(`".`)
				quoted(c.w, col.VCol.Name)

			} else if (col.CType & qcode.CTDisconnect) != 0 {
				c.w.WriteString(`NULL`)

			} else {
				colWithTable(c.w, col.VCol.Table, col.VCol.Name)
			}
		} else {
			quoted(c.w, col.Col.Name)
		}
	}
}

func (c *compilerContext) renderNestedInsertUpdateRelTables(m qcode.Mutate) {
	for _, t := range m.Tables {
		c.w.WriteString(`, `)
		if (t.CType & qcode.CTConnect) != 0 {
			c.w.WriteString(`"_x_`)
			c.w.WriteString(t.Ti.Name)
			c.w.WriteString(`"`)

		} else if (t.CType & qcode.CTDisconnect) != 0 {
			// do nothing

		} else {
			quoted(c.w, t.Ti.Name)
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
	quoted(c.w, sel.Table)

	c.w.WriteString(` AS (DELETE FROM `)
	quoted(c.w, sel.Table)
	c.w.WriteString(` WHERE `)
	c.renderExp(c.qc.Schema, sel.Ti, sel.Where.Exp, false)

	c.w.WriteString(` RETURNING `)
	quoted(c.w, sel.Table)
	c.w.WriteString(`.*) `)
}

func (c *compilerContext) renderConnectStmt(m qcode.Mutate) {
	rel := m.RelPC

	// Render only for parent-to-child relationship of one-to-one
	// For this to work the json child needs to found first so it's primary key
	// can be set in the related column on the parent object.
	// Eg. Create product and connect a user to it.
	if rel.Type == sdata.RelOneToOne {
		c.w.WriteString(`, "_x_`)
		c.w.WriteString(m.Ti.Name)
		c.w.WriteString(`" AS (SELECT `)

		if rel.Left.Col.Array {
			c.w.WriteString(`array_agg(DISTINCT `)
			quoted(c.w, rel.Right.Col.Name)
			c.w.WriteString(`) AS `)
			quoted(c.w, rel.Right.Col.Name)
		} else {
			quoted(c.w, rel.Right.Col.Name)
		}

		c.w.WriteString(` FROM "_sg_input" i,`)
		quoted(c.w, m.Ti.Name)

		c.w.WriteString(` WHERE `)
		c.renderWhereFromJSON(m, "connect")
		c.w.WriteString(` LIMIT 1)`)
	}

	if rel.Type == sdata.RelOneToMany {
		c.w.WriteString(`, `)
		if m.Multi {
			renderCteNameWithSuffix(c.w, m, strconv.Itoa(int(m.MID)))
		} else {
			renderCteName(c.w, m)
		}

		c.w.WriteString(` AS ( UPDATE `)
		quoted(c.w, m.Ti.Name)
		c.w.WriteString(` SET `)
		quoted(c.w, m.RelPC.Right.Col.Name)
		c.w.WriteString(` = `)

		// When setting the id of the connected table in a one-to-many setting
		// we always overwrite the value including for array columns
		if m.Multi {
			colWithTableID(c.w, m.RelPC.Left.Col.Table, (m.MID - 1), m.RelPC.Left.Col.Name)
		} else {
			colWithTable(c.w, m.RelPC.Left.Col.Table, m.RelPC.Left.Col.Name)
		}

		c.w.WriteString(`FROM "_sg_input" i,`)
		if m.Multi {
			renderCteNameWithSuffix(c.w, m, strconv.Itoa(int(m.MID-1)))
		} else {
			quoted(c.w, m.RelPC.Left.Col.Table)
		}
		c.w.WriteString(` WHERE`)
		c.renderWhereFromJSON(m, "connect")

		c.w.WriteString(` RETURNING `)
		quoted(c.w, m.Ti.Name)
		c.w.WriteString(`.*)`)
	}
}

func (c *compilerContext) renderDisconnectStmt(m qcode.Mutate) {
	rel := m.RelPC

	// Render only for parent-to-child relationship of one-to-one
	// For this to work the child needs to found first so it's
	// null value can beset in the related column on the parent object.
	// Eg. Update product and diconnect the user from it.
	if rel.Type == sdata.RelOneToOne {
		c.w.WriteString(`, "_x_`)
		c.w.WriteString(m.Ti.Name)
		c.w.WriteString(`" AS (`)

		if rel.Right.Col.Array {
			c.w.WriteString(`SELECT `)
			quoted(c.w, rel.Right.Col.Name)
			c.w.WriteString(` FROM "_sg_input" i,`)
			quoted(c.w, m.Ti.Name)
			c.w.WriteString(` WHERE `)
			c.renderWhereFromJSON(m, "disconnect")
			c.w.WriteString(` LIMIT 1))`)

		} else {
			c.w.WriteString(`SELECT * FROM (VALUES(NULL::"`)
			c.w.WriteString(rel.Right.Col.Type)
			c.w.WriteString(`")) AS LOOKUP(`)
			quoted(c.w, rel.Right.Col.Name)
			c.w.WriteString(`))`)
		}
	}

	if rel.Type == sdata.RelOneToMany {
		c.w.WriteString(`, `)
		if m.Multi {
			renderCteNameWithSuffix(c.w, m, strconv.Itoa(int(m.MID-1)))
			c.w.WriteString(` `)
			quoted(c.w, m.Ti.Name)
		} else {
			quoted(c.w, m.Ti.Name)
		}

		c.w.WriteString(` AS ( UPDATE `)
		quoted(c.w, m.Ti.Name)
		c.w.WriteString(` SET `)
		quoted(c.w, m.RelPC.Right.Col.Name)
		c.w.WriteString(` = `)

		if m.RelPC.Right.Col.Array {
			c.w.WriteString(` array_remove(`)
			quoted(c.w, m.RelPC.Right.Col.Name)
			c.w.WriteString(`, `)
			colWithTable(c.w, m.RelPC.Left.Col.Table, m.RelPC.Left.Col.Name)
			c.w.WriteString(`)`)

		} else {
			c.w.WriteString(` NULL`)
		}

		c.w.WriteString(`FROM "_sg_input" i,`)
		if m.Multi {
			renderCteNameWithSuffix(c.w, m, strconv.Itoa(int(m.MID-1)))
			c.w.WriteString(` `)
			quoted(c.w, m.RelPC.Left.Col.Table)
		} else {
			quoted(c.w, m.RelPC.Left.Col.Table)
		}
		c.w.WriteString(` WHERE`)
		c.renderWhereFromJSON(m, "disconnect")

		c.w.WriteString(` RETURNING `)
		quoted(c.w, m.Ti.Name)
		c.w.WriteString(`.*)`)
	}

}

func (c *compilerContext) renderWhereFromJSON(m qcode.Mutate, key string) {
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
			renderPathJSON(c.w, m, key, k)
			c.w.WriteString(`::json) AS a))`)

		} else if col.Array {
			c.w.WriteString(`(`)
			renderPathJSON(c.w, m, key, k)
			c.w.WriteString(`)::`)
			c.w.WriteString(col.Type)

			c.w.WriteString(` = ANY(`)
			colWithTable(c.w, m.Ti.Name, k)
			c.w.WriteString(`)`)

		} else {
			colWithTable(c.w, m.Ti.Name, k)
			c.w.WriteString(` = (`)
			renderPathJSON(c.w, m, key, k)
			c.w.WriteString(`)::`)
			c.w.WriteString(col.Type)
		}
		i++
	}
}

func renderPathJSON(w *bytes.Buffer, m qcode.Mutate, key1, key2 string) {
	w.WriteString(`(i.j->`)
	joinPath(w, m.Path)
	w.WriteString(`->'`)
	w.WriteString(key1)
	w.WriteString(`'->>'`)
	w.WriteString(key2)
	w.WriteString(`')`)
}

func renderCteName(w *bytes.Buffer, m qcode.Mutate) {
	w.WriteString(`"`)
	w.WriteString(m.Ti.Name)
	w.WriteString(`"`)
}

func renderCteNameWithSuffix(w *bytes.Buffer, m qcode.Mutate, suffix string) {
	w.WriteString(`"`)
	w.WriteString(m.Ti.Name)
	w.WriteString(`_`)
	w.WriteString(suffix)
	w.WriteString(`"`)
}

func renderNameWithSuffix(w *bytes.Buffer, name string, suffix string) {
	w.WriteString(`"`)
	w.WriteString(name)
	w.WriteString(`_`)
	w.WriteString(suffix)
	w.WriteString(`"`)
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
