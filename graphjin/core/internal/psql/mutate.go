//nolint:errcheck

package psql

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/dosco/super-graph/core/internal/qcode"
	"github.com/dosco/super-graph/core/internal/sdata"
)

func (co *Compiler) compileMutation(
	w *bytes.Buffer,
	qc *qcode.QCode,
	metad Metadata) Metadata {

	c := compilerContext{
		md:       metad,
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

	return co.CompileQuery(w, qc, c.md)
}

func (c *compilerContext) renderUnionStmt(m qcode.Mutate) {
	var connect, disconnect bool

	// Render only for parent-to-child relationship of one-to-many
	if m.RelPC.Type != sdata.RelOneToMany {
		return
	}

	for _, v := range m.Items {
		if v.Type == qcode.MTConnect {
			connect = true
		} else if v.Type == qcode.MTDisconnect {
			disconnect = true
		}
		if connect && disconnect {
			break
		}
	}

	if connect {
		c.w.WriteString(`, `)
		if connect && disconnect {
			renderCteNameWithSuffix(c.w, m, "c")
		} else {
			quoted(c.w, m.Ti.Name)
		}
		c.w.WriteString(` AS ( UPDATE `)
		quoted(c.w, m.Ti.Name)
		c.w.WriteString(` SET `)
		quoted(c.w, m.RelPC.Right.Col.Name)
		c.w.WriteString(` = `)

		// When setting the id of the connected table in a one-to-many setting
		// we always overwrite the value including for array columns
		colWithTable(c.w, m.RelPC.Left.Col.Table, m.RelPC.Left.Col.Name)

		c.w.WriteString(` FROM `)
		quoted(c.w, m.RelPC.Left.Col.Table)
		c.w.WriteString(` WHERE`)

		for i, v := range m.Items {
			if v.Type == qcode.MTConnect {
				if i != 0 {
					c.w.WriteString(` OR (`)
				} else {
					c.w.WriteString(` (`)
				}
				c.renderWhereFromJSON(v, "connect")
				c.w.WriteString(`)`)
			}
		}
		c.w.WriteString(` RETURNING `)
		quoted(c.w, m.Ti.Name)
		c.w.WriteString(`.*)`)
	}

	if disconnect {
		c.w.WriteString(`, `)
		if connect && disconnect {
			renderCteNameWithSuffix(c.w, m, "d")
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

		c.w.WriteString(` FROM `)
		quoted(c.w, m.RelPC.Left.Col.Table)
		c.w.WriteString(` WHERE`)

		for i, v := range m.Items {
			if v.Type == qcode.MTDisconnect {
				if i != 0 {
					c.w.WriteString(` OR (`)
				} else {
					c.w.WriteString(` (`)
				}
				c.renderWhereFromJSON(v, "disconnect")
				c.w.WriteString(`)`)
			}
		}
		c.w.WriteString(` RETURNING `)
		quoted(c.w, m.Ti.Name)
		c.w.WriteString(`.*)`)
	}

	if connect && disconnect {
		c.w.WriteString(`, `)
		quoted(c.w, m.Ti.Name)
		c.w.WriteString(` AS (`)
		c.w.WriteString(`SELECT * FROM `)
		renderCteNameWithSuffix(c.w, m, "c")
		c.w.WriteString(` UNION ALL `)
		c.w.WriteString(`SELECT * FROM `)
		renderCteNameWithSuffix(c.w, m, "d")
		c.w.WriteString(`)`)
	}
}

func (c *compilerContext) renderInsertUpdateColumns(m qcode.Mutate, values bool) {
	for i, col := range m.Cols {
		if i != 0 {
			c.w.WriteString(`, `)
		}

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
}

func (c *compilerContext) renderNestedInsertUpdateRelColumns(m qcode.Mutate, values bool) {
	for _, col := range m.RCols {
		c.w.WriteString(`, `)
		if values {
			if col.CType != 0 {
				c.w.WriteString(`"_x_`)
				c.w.WriteString(col.VCol.Table)
				c.w.WriteString(`".`)
				quoted(c.w, col.VCol.Name)
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
		if t.CType != 0 {
			c.w.WriteString(`"_x_`)
			c.w.WriteString(t.Ti.Name)
			c.w.WriteString(`"`)
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
	quoted(c.w, sel.Ti.Name)

	c.w.WriteString(` AS (DELETE FROM `)
	quoted(c.w, sel.Ti.Name)
	c.w.WriteString(` WHERE `)
	c.renderExp(c.qc.Schema, sel.Ti, sel.Where.Exp, false)

	c.w.WriteString(` RETURNING `)
	quoted(c.w, sel.Ti.Name)
	c.w.WriteString(`.*) `)
}

func (c *compilerContext) renderConnectStmt(m qcode.Mutate) {
	rel := m.RelPC

	// Render only for parent-to-child relationship of one-to-one
	// For this to work the child needs to found first so it's primary key
	// can be set in the related column on the parent object.
	// Eg. Create product and connect a user to it.
	if rel.Type != sdata.RelOneToOne {
		return
	}

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

func (c *compilerContext) renderDisconnectStmt(m qcode.Mutate) {
	rel := m.RelPC

	// Render only for parent-to-child relationship of one-to-one
	// For this to work the child needs to found first so it's
	// null value can beset in the related column on the parent object.
	// Eg. Update product and diconnect the user from it.
	if rel.Type != sdata.RelOneToOne {
		return
	}
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
	if m.Type == qcode.MTConnect || m.Type == qcode.MTDisconnect {
		w.WriteString(`_`)
		int32String(w, m.ID)
	}
	w.WriteString(`"`)
}

func renderCteNameWithSuffix(w *bytes.Buffer, m qcode.Mutate, suffix string) {
	w.WriteString(`"`)
	w.WriteString(m.Ti.Name)
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
