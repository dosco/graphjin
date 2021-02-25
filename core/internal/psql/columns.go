package psql

import (
	"github.com/dosco/graphjin/core/internal/qcode"
	"github.com/dosco/graphjin/core/internal/sdata"
)

func (c *compilerContext) renderColumns(sel *qcode.Select) {
	i := 0
	for _, col := range sel.Cols {
		if i != 0 {
			c.w.WriteString(", ")
		}
		colWithTableID(c.w, sel.Table, sel.ID, col.Col.Name)
		c.alias(col.FieldName)
		i++
	}
	for _, fn := range sel.Funcs {
		if i != 0 {
			c.w.WriteString(", ")
		}
		colWithTableID(c.w, sel.Table, sel.ID, fn.FieldName)
		c.alias(fn.FieldName)
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

func (c *compilerContext) renderJoinColumns(sel *qcode.Select, n int) {
	i := n
	for _, cid := range sel.Children {
		csel := &c.qc.Selects[cid]

		if csel.SkipRender == qcode.SkipTypeRemote {
			continue
		}

		if i != 0 {
			c.w.WriteString(", ")
		}

		//TODO: log what and why this is being skipped
		if csel.SkipRender != qcode.SkipTypeNone {
			c.w.WriteString(`NULL`)
			c.alias(csel.FieldName)

			if sel.Paging.Cursor {
				c.w.WriteString(`, NULL`)
				c.alias(sel.FieldName)
			}

		} else {
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
}

func (c *compilerContext) renderUnionColumn(sel, csel *qcode.Select) {
	c.w.WriteString(`(CASE `)
	for _, cid := range csel.Children {
		usel := &c.qc.Selects[cid]

		c.w.WriteString(`WHEN `)
		colWithTableID(c.w, sel.Table, sel.ID, csel.Rel.Right.Col.Name)
		c.w.WriteString(` = `)
		c.squoted(usel.Table)
		c.w.WriteString(` THEN `)

		if usel.SkipRender == qcode.SkipTypeUserNeeded {
			c.w.WriteString(`NULL `)
		} else {
			c.w.WriteString(`__sj_`)
			int32String(c.w, usel.ID)
			c.w.WriteString(`.json `)
		}
	}
	c.w.WriteString(`END)`)
	c.alias(csel.FieldName)
}

func (c *compilerContext) renderFunction(sel *qcode.Select, fn qcode.Function) {
	switch fn.Name {
	case "search_rank":
		c.renderFunctionSearchRank(sel, fn)
	case "search_headline":
		c.renderFunctionSearchHeadline(sel, fn)
	default:
		c.renderOtherFunction(sel, fn)
	}
	c.alias(fn.FieldName)
}

func (c *compilerContext) renderFunctionSearchRank(sel *qcode.Select, fn qcode.Function) {
	if c.ct == "mysql" {
		c.w.WriteString(`0`)
		return
	}

	c.w.WriteString(`ts_rank(`)
	for i, col := range sel.Ti.FullText {
		if i != 0 {
			c.w.WriteString(` || `)
		}
		colWithTable(c.w, sel.Table, col.Name)
	}
	if c.cv >= 110000 {
		c.w.WriteString(`, websearch_to_tsquery(`)
	} else {
		c.w.WriteString(`, to_tsquery(`)
	}
	arg := sel.ArgMap["search"]
	c.renderParam(Param{Name: arg.Val, Type: "text"})
	c.w.WriteString(`))`)
}

func (c *compilerContext) renderFunctionSearchHeadline(sel *qcode.Select, fn qcode.Function) {
	if c.ct == "mysql" {
		c.w.WriteString(`''`)
		return
	}

	hasIndex := false
	for _, col := range sel.Ti.FullText {
		if col.ID == fn.Col.ID {
			hasIndex = true
		}
	}

	c.w.WriteString(`ts_headline(`)
	if hasIndex {
		colWithTable(c.w, sel.Table, fn.Col.Name)
	} else {
		c.w.WriteString(`to_tsvector(`)
		colWithTable(c.w, sel.Table, fn.Col.Name)
		c.w.WriteString(`)`)
	}
	if c.cv >= 110000 {
		c.w.WriteString(`, websearch_to_tsquery(`)
	} else {
		c.w.WriteString(`, to_tsquery(`)
	}
	arg := sel.ArgMap["search"]
	c.renderParam(Param{Name: arg.Val, Type: "text"})
	c.w.WriteString(`))`)
}

func (c *compilerContext) renderOtherFunction(sel *qcode.Select, fn qcode.Function) {
	c.w.WriteString(fn.Name)
	c.w.WriteString(`(`)
	colWithTable(c.w, sel.Table, fn.Col.Name)
	_, _ = c.w.WriteString(`)`)
}

func (c *compilerContext) renderBaseColumns(sel *qcode.Select, skipFuncs bool) {
	i := 0

	for _, col := range sel.BCols {
		if i != 0 {
			c.w.WriteString(`, `)
		}
		if col.Col.Array && c.ct == "mysql" {
			c.w.WriteString(`CAST(`)
			colWithTable(c.w, sel.Table, col.Col.Name)
			c.w.WriteString(` AS JSON) AS `)
			c.w.WriteString(col.Col.Name)
		} else {
			colWithTable(c.w, sel.Table, col.Col.Name)
		}
		i++
	}
	if skipFuncs {
		return
	}
	for _, fn := range sel.Funcs {
		if i != 0 {
			c.w.WriteString(`, `)
		}
		c.renderFunction(sel, fn)
		i++
	}
}

func (c *compilerContext) renderTypename(sel *qcode.Select) {
	c.w.WriteString(`(`)
	c.squoted(sel.Table)
	c.w.WriteString(` :: text) AS "__typename"`)
}

func (c *compilerContext) renderJSONFields(sel *qcode.Select) {
	i := 0
	for _, col := range sel.Cols {
		if i != 0 {
			c.w.WriteString(", ")
		}
		c.renderJSONField(col.FieldName, sel.ID)
		i++
	}
	for _, fn := range sel.Funcs {
		if i != 0 {
			c.w.WriteString(", ")
		}
		c.renderJSONField(fn.FieldName, sel.ID)
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

		//TODO: log what and why this is being skipped
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
