package psql

import (
	"github.com/dosco/graphjin/core/internal/qcode"
	"github.com/dosco/graphjin/core/internal/sdata"
)

func (c *compilerContext) renderRel(
	ti sdata.DBTable,
	rel sdata.DBRel,
	pid int32,
	args map[string]qcode.Arg) {

	if rel.Type == sdata.RelNone {
		return
	}
	c.w.WriteString(`((`)

	switch rel.Type {
	case sdata.RelOneToOne, sdata.RelOneToMany:
		//fmt.Fprintf(w, `(("%s"."%s") = ("%s_%d"."%s"))`,
		//c.sel.Name, rel.Left.Col, c.parent.Name, c.parent.ID, rel.Right.Col)

		switch {
		case !rel.Left.Col.Array && rel.Right.Col.Array:
			colWithTable(c.w, rel.Left.Col.Table, rel.Left.Col.Name)
			c.w.WriteString(`) = any (`)
			c.renderRelArrayRight(ti, "", pid, rel.Right.Col, rel.Left.Col.Type)

		case rel.Left.Col.Array && !rel.Right.Col.Array:
			colWithTableID(c.w, rel.Right.Col.Table, pid, rel.Right.Col.Name)
			c.w.WriteString(`) = any (`)
			c.renderRelArrayRight(ti, "", -1, rel.Left.Col, rel.Right.Col.Type)

		default:
			colWithTable(c.w, rel.Left.Col.Table, rel.Left.Col.Name)
			c.w.WriteString(`) = (`)
			colWithTableID(c.w, rel.Right.Col.Table, pid, rel.Right.Col.Name)
		}

	case sdata.RelEmbedded:
		colWithTable(c.w, rel.Right.Col.Table, rel.Right.Col.Name)
		c.w.WriteString(`) = (`)
		colWithTableID(c.w, rel.Right.Col.Table, pid, rel.Right.Col.Name)

	case sdata.RelPolymorphic:
		colWithTable(c.w, ti.Name, rel.Right.Col.Name)
		c.w.WriteString(`) = (`)
		colWithTableID(c.w, rel.Left.Col.Table, pid, rel.Left.Col.Name)
		c.w.WriteString(`) AND (`)
		colWithTableID(c.w, rel.Left.Col.Table, pid, rel.Left.Col.FKeyCol)
		c.w.WriteString(`) = (`)
		c.squoted(ti.Name)

	case sdata.RelRecursive:
		rcte := "__rcte_" + rel.Right.Ti.Name
		if v, ok := args["find"]; ok {
			switch v.Val {
			case "parents", "parent":
				// Ensure fk is not null
				colWithTable(c.w, rcte, rel.Left.Col.Name)
				c.w.WriteString(` IS NOT NULL) AND (`)

				switch {
				case !rel.Left.Col.Array && rel.Right.Col.Array:
					// Ensure it's not a loop
					colWithTable(c.w, rcte, rel.Left.Col.Name)
					c.w.WriteString(`) != any (`)
					c.renderRelArrayRight(ti, rcte, -1, rel.Right.Col, rel.Left.Col.Type)
					c.w.WriteString(`) AND (`)
					// Recursive relationship
					colWithTable(c.w, rcte, rel.Left.Col.Name)
					c.w.WriteString(`) = any (`)
					c.renderRelArrayRight(ti, rel.Left.Col.Table, -1, rel.Right.Col, rel.Left.Col.Type)

				case rel.Left.Col.Array && !rel.Right.Col.Array:
					// Ensure it's not a loop
					colWithTable(c.w, rcte, rel.Right.Col.Name)
					c.w.WriteString(`) != any (`)
					c.renderRelArrayRight(ti, rcte, -1, rel.Left.Col, rel.Right.Col.Type)
					c.w.WriteString(`) AND (`)
					// Recursive relationship
					colWithTable(c.w, rel.Left.Col.Table, rel.Right.Col.Name)
					c.w.WriteString(`) = any (`)
					c.renderRelArrayRight(ti, rcte, -1, rel.Left.Col, rel.Right.Col.Type)

				default:
					// Ensure it's not a loop
					colWithTable(c.w, rcte, rel.Left.Col.Name)
					c.w.WriteString(`) != (`)
					colWithTable(c.w, rcte, rel.Right.Col.Name)
					c.w.WriteString(`) AND (`)
					// Recursive relationship
					colWithTable(c.w, rel.Left.Col.Table, rel.Right.Col.Name)
					c.w.WriteString(`) = (`)
					colWithTable(c.w, rcte, rel.Left.Col.Name)
				}

			default:
				// Ensure fk is not null
				colWithTable(c.w, rel.Left.Col.Table, rel.Left.Col.Name)
				c.w.WriteString(` IS NOT NULL) AND (`)

				switch {
				case !rel.Left.Col.Array && rel.Right.Col.Array:
					// Ensure it's not a loop
					colWithTable(c.w, rel.Left.Col.Table, rel.Left.Col.Name)
					c.w.WriteString(`) != any (`)
					c.renderRelArrayRight(ti, "", -1, rel.Right.Col, rel.Left.Col.Type)
					c.w.WriteString(`) AND (`)
					// Recursive relationship
					colWithTable(c.w, rel.Left.Col.Table, rel.Left.Col.Name)
					c.w.WriteString(`) = any (`)
					c.renderRelArrayRight(ti, rcte, -1, rel.Right.Col, rel.Left.Col.Type)

				case rel.Left.Col.Array && !rel.Right.Col.Array:
					// Ensure it's not a loop
					colWithTable(c.w, rel.Right.Col.Table, rel.Right.Col.Name)
					c.w.WriteString(`) != any (`)
					c.renderRelArrayRight(ti, "", -1, rel.Left.Col, rel.Right.Col.Type)
					c.w.WriteString(`) AND (`)
					// Recursive relationship
					colWithTable(c.w, rcte, rel.Right.Col.Name)
					c.w.WriteString(`) = any (`)
					c.renderRelArrayRight(ti, "", -1, rel.Left.Col, rel.Right.Col.Type)

				default:
					// Ensure it's not a loop
					colWithTable(c.w, rel.Left.Col.Table, rel.Left.Col.Name)
					c.w.WriteString(`) != (`)
					colWithTable(c.w, rel.Right.Col.Table, rel.Right.Col.Name)
					c.w.WriteString(`) AND (`)
					// Recursive relationship
					colWithTable(c.w, rel.Left.Col.Table, rel.Left.Col.Name)
					c.w.WriteString(`) = (`)
					colWithTable(c.w, rcte, rel.Right.Col.Name)
				}
			}
		}
	}

	c.w.WriteString(`))`)
}

func (c *compilerContext) renderRelArrayRight(
	ti sdata.DBTable,
	table string, pid int32, col sdata.DBColumn, ty string) {
	colTable := col.Table
	if table != "" {
		colTable = table
	}

	switch c.ct {
	case "mysql":
		c.w.WriteString(`SELECT _gj_jt.* FROM `)
		c.w.WriteString(`(SELECT CAST(`)
		if pid == -1 {
			colWithTable(c.w, colTable, col.Name)
		} else {
			colWithTableID(c.w, colTable, pid, col.Name)
		}
		c.w.WriteString(` AS JSON) as ids) j, `)
		c.w.WriteString(`JSON_TABLE(j.ids, "$[*]" COLUMNS(`)
		c.w.WriteString(col.Name)
		c.w.WriteString(` `)
		c.w.WriteString(ty)
		c.w.WriteString(` PATH "$" ERROR ON ERROR)) AS _gj_jt`)

	default:
		if pid == -1 {
			colWithTable(c.w, colTable, col.Name)
		} else {
			colWithTableID(c.w, colTable, pid, col.Name)
		}
	}
}
