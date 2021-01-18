package psql

import (
	"github.com/dosco/graphjin/core/internal/qcode"
	"github.com/dosco/graphjin/core/internal/sdata"
)

func (c *compilerContext) renderRel(
	ti sdata.DBTableInfo,
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
			c.renderRelArrayRight("", pid, rel.Right.Col, rel.Left.Col.Type)

		case rel.Left.Col.Array && !rel.Right.Col.Array:
			colWithTableID(c.w, rel.Right.Col.Table, pid, rel.Right.Col.Name)
			c.w.WriteString(`) = any (`)
			c.renderRelArrayRight("", -1, rel.Left.Col, rel.Right.Col.Type)

		default:
			colWithTable(c.w, rel.Left.Col.Table, rel.Left.Col.Name)
			c.w.WriteString(`) = (`)
			colWithTableID(c.w, rel.Right.Col.Table, pid, rel.Right.Col.Name)
		}

	case sdata.RelOneToManyThrough:
		// This requires the through table to be joined onto this select
		// this where clause is the right side of the clause the left side
		// of it is on the ON clause for the though table join.

		switch {
		case !rel.Right.Col.Array && rel.Through.ColR.Array:
			colWithTableID(c.w, rel.Right.Col.Table, pid, rel.Right.Col.Name)
			c.w.WriteString(`) = any (`)
			c.renderRelArrayRight("", -1, rel.Through.ColR, rel.Right.Col.Type)

		case rel.Right.Col.Array && !rel.Through.ColR.Array:
			colWithTable(c.w, rel.Through.ColR.Table, rel.Through.ColR.Name)
			c.w.WriteString(`) = any (`)
			c.renderRelArrayRight("", -1, rel.Right.Col, rel.Through.ColR.Type)
		default:
			colWithTableID(c.w, rel.Right.Col.Table, pid, rel.Right.Col.Name)
			c.w.WriteString(`) = (`)
			colWithTable(c.w, rel.Through.ColR.Table, rel.Through.ColR.Name)
		}

	case sdata.RelEmbedded:
		colWithTable(c.w, rel.Left.Col.Table, rel.Left.Col.Name)
		c.w.WriteString(`) = (`)
		colWithTableID(c.w, rel.Left.Col.Table, pid, rel.Left.Col.Name)

	case sdata.RelPolymorphic:
		colWithTable(c.w, ti.Name, rel.Right.Col.Name)
		c.w.WriteString(`) = (`)
		colWithTableID(c.w, rel.Left.Col.Table, pid, rel.Left.Col.Name)
		c.w.WriteString(`) AND (`)
		colWithTableID(c.w, rel.Left.Col.Table, pid, rel.Right.VTable)
		c.w.WriteString(`) = (`)
		squoted(c.w, ti.Name)

	case sdata.RelRecursive:
		if v, ok := args["find"]; ok {
			switch v.Val {
			case "parents", "parent":
				// Ensure fk is not null
				colWithTable(c.w, rel.Right.VTable, rel.Left.Col.Name)
				c.w.WriteString(` IS NOT NULL) AND (`)

				switch {
				case !rel.Left.Col.Array && rel.Right.Col.Array:
					// Ensure it's not a loop
					colWithTable(c.w, rel.Right.VTable, rel.Left.Col.Name)
					c.w.WriteString(`) != any (`)
					c.renderRelArrayRight(rel.Right.VTable, -1, rel.Right.Col, rel.Left.Col.Type)
					c.w.WriteString(`) AND (`)
					// Recursive relationship
					colWithTable(c.w, rel.Right.VTable, rel.Left.Col.Name)
					c.w.WriteString(`) = any (`)
					c.renderRelArrayRight(rel.Left.Col.Table, -1, rel.Right.Col, rel.Left.Col.Type)

				case rel.Left.Col.Array && !rel.Right.Col.Array:
					// Ensure it's not a loop
					colWithTable(c.w, rel.Right.VTable, rel.Right.Col.Name)
					c.w.WriteString(`) != any (`)
					c.renderRelArrayRight(rel.Right.VTable, -1, rel.Left.Col, rel.Right.Col.Type)
					c.w.WriteString(`) AND (`)
					// Recursive relationship
					colWithTable(c.w, rel.Left.Col.Table, rel.Right.Col.Name)
					c.w.WriteString(`) = any (`)
					c.renderRelArrayRight(rel.Right.VTable, -1, rel.Left.Col, rel.Right.Col.Type)

				default:
					// Ensure it's not a loop
					colWithTable(c.w, rel.Right.VTable, rel.Left.Col.Name)
					c.w.WriteString(`) != (`)
					colWithTable(c.w, rel.Right.VTable, rel.Right.Col.Name)
					c.w.WriteString(`) AND (`)
					// Recursive relationship
					colWithTable(c.w, rel.Left.Col.Table, rel.Right.Col.Name)
					c.w.WriteString(`) = (`)
					colWithTable(c.w, rel.Right.VTable, rel.Left.Col.Name)
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
					c.renderRelArrayRight("", -1, rel.Right.Col, rel.Left.Col.Type)
					c.w.WriteString(`) AND (`)
					// Recursive relationship
					colWithTable(c.w, rel.Left.Col.Table, rel.Left.Col.Name)
					c.w.WriteString(`) = any (`)
					c.renderRelArrayRight(rel.Right.VTable, -1, rel.Right.Col, rel.Left.Col.Type)

				case rel.Left.Col.Array && !rel.Right.Col.Array:
					// Ensure it's not a loop
					colWithTable(c.w, rel.Right.Col.Table, rel.Right.Col.Name)
					c.w.WriteString(`) != any (`)
					c.renderRelArrayRight("", -1, rel.Left.Col, rel.Right.Col.Type)
					c.w.WriteString(`) AND (`)
					// Recursive relationship
					colWithTable(c.w, rel.Right.VTable, rel.Right.Col.Name)
					c.w.WriteString(`) = any (`)
					c.renderRelArrayRight("", -1, rel.Left.Col, rel.Right.Col.Type)

				default:
					// Ensure it's not a loop
					colWithTable(c.w, rel.Left.Col.Table, rel.Left.Col.Name)
					c.w.WriteString(`) != (`)
					colWithTable(c.w, rel.Right.Col.Table, rel.Right.Col.Name)
					c.w.WriteString(`) AND (`)
					// Recursive relationship
					colWithTable(c.w, rel.Left.Col.Table, rel.Left.Col.Name)
					c.w.WriteString(`) = (`)
					colWithTable(c.w, rel.Right.VTable, rel.Right.Col.Name)
				}
			}
		}
	}

	c.w.WriteString(`))`)
}

func (c *compilerContext) renderRelArrayRight(table string, pid int32, col sdata.DBColumn, ty string) {
	colTable := col.Table
	if table != "" {
		colTable = table
	}

	switch c.md.ct {
	case "mysql":
		c.w.WriteString(`(SELECT * FROM JSON_TABLE(`)
		if pid == -1 {
			colWithTable(c.w, colTable, col.Name)
		} else {
			colWithTableID(c.w, colTable, pid, col.Name)
		}
		c.w.WriteString(`, '$[*]' COLUMNS(`)
		c.w.WriteString(col.Name)
		c.w.WriteString(` `)
		c.w.WriteString(ty)
		c.w.WriteString(` PATH '$' ERROR ON ERROR)) AS jt)`)

	default:
		if pid == -1 {
			colWithTable(c.w, colTable, col.Name)
		} else {
			colWithTableID(c.w, colTable, pid, col.Name)
		}
	}

}
