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
			colWithTableID(c.w, rel.Right.Col.Table, pid, rel.Right.Col.Name)

		case rel.Left.Col.Array && !rel.Right.Col.Array:
			colWithTableID(c.w, rel.Right.Col.Table, pid, rel.Right.Col.Name)
			c.w.WriteString(`) = any (`)
			colWithTable(c.w, rel.Left.Col.Table, rel.Left.Col.Name)

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
			colWithTable(c.w, rel.Through.ColR.Table, rel.Through.ColR.Name)

		case rel.Right.Col.Array && !rel.Through.ColR.Array:
			colWithTable(c.w, rel.Through.ColR.Table, rel.Through.ColR.Name)
			c.w.WriteString(`) = any (`)
			colWithTableID(c.w, rel.Right.Col.Table, pid, rel.Right.Col.Name)
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
					colWithTable(c.w, rel.Right.VTable, rel.Right.Col.Name)
					c.w.WriteString(`) AND (`)
					// Recursive relationship
					colWithTable(c.w, rel.Right.VTable, rel.Left.Col.Name)
					c.w.WriteString(`) = any (`)
					colWithTable(c.w, rel.Left.Col.Table, rel.Right.Col.Name)

				case rel.Left.Col.Array && !rel.Right.Col.Array:
					// Ensure it's not a loop
					colWithTable(c.w, rel.Right.VTable, rel.Right.Col.Name)
					c.w.WriteString(`) != any (`)
					colWithTable(c.w, rel.Right.VTable, rel.Left.Col.Name)
					c.w.WriteString(`) AND (`)
					// Recursive relationship
					colWithTable(c.w, rel.Left.Col.Table, rel.Right.Col.Name)
					c.w.WriteString(`) = any (`)
					colWithTable(c.w, rel.Right.VTable, rel.Left.Col.Name)

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
					colWithTable(c.w, rel.Right.Col.Table, rel.Right.Col.Name)
					c.w.WriteString(`) AND (`)
					// Recursive relationship
					colWithTable(c.w, rel.Left.Col.Table, rel.Left.Col.Name)
					c.w.WriteString(`) = any (`)
					colWithTable(c.w, rel.Right.VTable, rel.Right.Col.Name)

				case rel.Left.Col.Array && !rel.Right.Col.Array:
					// Ensure it's not a loop
					colWithTable(c.w, rel.Right.Col.Table, rel.Right.Col.Name)
					c.w.WriteString(`) != any (`)
					colWithTable(c.w, rel.Left.Col.Table, rel.Left.Col.Name)
					c.w.WriteString(`) AND (`)
					// Recursive relationship
					colWithTable(c.w, rel.Right.VTable, rel.Right.Col.Name)
					c.w.WriteString(`) = any (`)
					colWithTable(c.w, rel.Left.Col.Table, rel.Left.Col.Name)

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
