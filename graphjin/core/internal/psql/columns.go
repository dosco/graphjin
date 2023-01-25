package psql

import (
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func (c *compilerContext) renderColumns(sel *qcode.Select) {
	i := 0
	for _, f := range sel.Fields {
		if i != 0 {
			c.w.WriteString(", ")
		}

		switch {
		case f.SkipRender == qcode.SkipTypeNulled:
			c.w.WriteString(`NULL`)
		case f.Type == qcode.FieldTypeFunc:
			c.renderFuncColumn(sel, f)
		case f.Type == qcode.FieldTypeCol:
			c.renderStdColumn(sel, f)
		default:
			continue
		}
		c.alias(f.FieldName)
		i++
	}
	if sel.Typename {
		if i != 0 {
			c.w.WriteString(`, `)
		}
		c.renderTypename(sel)
		i++
	}
	c.renderJoinColumns(sel, i)
}

func (c *compilerContext) renderStdColumn(sel *qcode.Select, f qcode.Field) {
	if f.FieldFilter.Exp != nil {
		c.w.WriteString(`(CASE WHEN `)
		c.renderExp(sel.Ti, f.FieldFilter.Exp, false)
		c.w.WriteString(` THEN `)
	}

	c.colWithTableID(sel.Table, sel.ID, f.Col.Name)

	if f.FieldFilter.Exp != nil {
		c.w.WriteString(` ELSE null END)`)
	}
}

func (c *compilerContext) renderFuncColumn(sel *qcode.Select, f qcode.Field) {
	c.colWithTableID(sel.Table, sel.ID, f.FieldName)
}

func (c *compilerContext) renderJoinColumns(sel *qcode.Select, n int) {
	i := n
	for _, cid := range sel.Children {
		csel := &c.qc.Selects[cid]

		if csel.SkipRender == qcode.SkipTypeDrop ||
			csel.SkipRender == qcode.SkipTypeRemote {
			continue
		}

		if i != 0 {
			c.w.WriteString(", ")
		}

		// TODO: log what and why this is being skipped
		switch csel.SkipRender {
		case qcode.SkipTypeUserNeeded, qcode.SkipTypeBlocked,
			qcode.SkipTypeNulled:

			c.w.WriteString(`NULL`)
			c.alias(csel.FieldName)

			if sel.Paging.Cursor {
				c.w.WriteString(`, NULL`)
				c.alias(sel.FieldName)
			}

		default:
			switch csel.Rel.Type {
			case sdata.RelPolymorphic:
				c.renderUnionColumn(sel, csel)

			default:
				c.w.WriteString(`__sj_`)
				int32String(c.w, csel.ID)
				c.w.WriteString(`.json`)
				c.alias(csel.FieldName)
			}

			// return the cursor for the this child selector as part of the parents json
			if csel.Paging.Cursor {
				c.w.WriteString(`, __sj_`)
				int32String(c.w, csel.ID)
				c.w.WriteString(`.__cursor AS `)
				c.w.WriteString(csel.FieldName)
				c.w.WriteString(`_cursor`)
			}
		}
		i++
	}
	// when no columns are rendered for mysql
	if c.ct == "mysql" && i == 0 {
		c.w.WriteString(`NULL`)
	}
}

func (c *compilerContext) renderUnionColumn(sel, csel *qcode.Select) {
	c.w.WriteString(`(CASE `)
	for _, cid := range csel.Children {
		usel := &c.qc.Selects[cid]

		c.w.WriteString(`WHEN `)
		c.colWithTableID(sel.Table, sel.ID, csel.Rel.Left.Col.FKeyCol)
		c.w.WriteString(` = `)
		c.squoted(usel.Table)
		c.w.WriteString(` THEN `)

		switch usel.SkipRender {
		case qcode.SkipTypeUserNeeded, qcode.SkipTypeBlocked,
			qcode.SkipTypeNulled:
			c.w.WriteString(`NULL `)
		default:
			c.w.WriteString(`__sj_`)
			int32String(c.w, usel.ID)
			c.w.WriteString(`.json `)
		}
	}
	c.w.WriteString(`END)`)
	c.alias(csel.FieldName)
}

func (c *compilerContext) renderBaseColumns(sel *qcode.Select) {
	i := 0
	for _, col := range sel.BCols {
		if i != 0 {
			c.w.WriteString(`, `)
		}
		c.colWithTable(col.Col.Table, col.Col.Name)
		i++
	}

	// render only function columns
	for _, f := range sel.Fields {
		if f.Type != qcode.FieldTypeFunc {
			continue
		}
		if i != 0 {
			c.w.WriteString(`, `)
		}

		if f.FieldFilter.Exp != nil {
			c.w.WriteString(`(CASE WHEN `)
			c.renderExp(sel.Ti, f.FieldFilter.Exp, false)
			c.w.WriteString(` THEN `)
		}
		c.renderFieldFunction(sel, f)

		if f.FieldFilter.Exp != nil {
			c.w.WriteString(` ELSE null END)`)
		}
		c.alias(f.FieldName)
		i++
	}
}

func (c *compilerContext) renderTypename(sel *qcode.Select) {
	c.squoted(sel.Table)
	c.w.WriteString(` AS "__typename"`)
}

func (c *compilerContext) renderJSONFields(sel *qcode.Select) {
	i := 0
	for _, f := range sel.Fields {
		if i != 0 {
			c.w.WriteString(", ")
		}
		c.renderJSONField(f.FieldName, sel.ID)
		i++
	}

	if sel.Typename {
		if i != 0 {
			c.w.WriteString(`, `)
		}
		c.renderJSONField("__typename", sel.ID)
		i++
	}

	for _, cid := range sel.Children {
		csel := &c.qc.Selects[cid]

		if csel.SkipRender == qcode.SkipTypeRemote {
			continue
		}

		if i != 0 {
			c.w.WriteString(", ")
		}

		// TODO: log what and why this is being skipped
		if csel.SkipRender != qcode.SkipTypeNone {
			c.renderJSONNullField(csel.FieldName)

			if sel.Paging.Cursor {
				c.w.WriteString(", ")
				c.renderJSONNullField(sel.FieldName + `_cursor`)
			}

		} else {
			c.renderJSONField(csel.FieldName, sel.ID)

			// return the cursor for the this child selector as part of the parents json
			if csel.Paging.Cursor {
				c.w.WriteString(", ")
				c.renderJSONField(csel.FieldName+`_cursor`, sel.ID)
			}
		}
		i++
	}
}

func (c *compilerContext) renderJSONField(name string, selID int32) {
	c.squoted(name)
	c.w.WriteString(`, __sr_`)
	int32String(c.w, selID)
	c.w.WriteString(`.`)
	c.w.WriteString(name)
}

func (c *compilerContext) renderJSONNullField(name string) {
	c.squoted(name)
	c.w.WriteString(`, NULL`)
}
