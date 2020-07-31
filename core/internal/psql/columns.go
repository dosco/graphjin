package psql

import (
	"github.com/dosco/super-graph/core/internal/qcode"
	"github.com/dosco/super-graph/core/internal/sdata"
)

func (c *compilerContext) renderColumns(sel *qcode.Select) {
	i := 0
	for _, col := range sel.Cols {
		if col.Base {
			continue
		}
		if i != 0 {
			c.w.WriteString(", ")
		}
		colWithTableID(c.w, sel.Table, sel.ID, col.Col.Name)
		alias(c.w, col.FieldName)
		i++
	}
	for _, fn := range sel.Funcs {
		if i != 0 {
			c.w.WriteString(", ")
		}
		colWithTableID(c.w, sel.Table, sel.ID, fn.FieldName)
		alias(c.w, fn.FieldName)
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

		if i != 0 {
			c.w.WriteString(", ")
		}

		//TODO: log what and why this is being skipped
		if csel.SkipRender != qcode.SkipTypeNone {
			c.w.WriteString(`NULL`)
			alias(c.w, csel.FieldName)

			if sel.Paging.Cursor {
				c.w.WriteString(`, NULL`)
				alias(c.w, sel.FieldName)
			}

		} else {
			switch csel.Rel.Type {
			case sdata.RelRemote:
				c.renderRemoteRelColumns(sel, csel)

			case sdata.RelPolymorphic:
				c.renderUnionColumn(sel, csel)

			default:
				c.w.WriteString(`"__sj_`)
				int32String(c.w, csel.ID)
				c.w.WriteString(`"."json"`)
				alias(c.w, csel.FieldName)
			}

			// return the cursor for the this child selector as part of the parents json
			if csel.Paging.Cursor {
				c.w.WriteString(`, "__sj_`)
				int32String(c.w, csel.ID)
				c.w.WriteString(`"."cursor" AS "`)
				c.w.WriteString(csel.FieldName)
				c.w.WriteString(`_cursor"`)
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
		colWithTableID(c.w, sel.Table, sel.ID, csel.Rel.Right.VTable)
		c.w.WriteString(` = `)
		squoted(c.w, usel.Ti.Name)
		c.w.WriteString(` THEN `)
		c.w.WriteString(`"__sj_`)
		int32String(c.w, usel.ID)
		c.w.WriteString(`"."json" `)
	}
	c.w.WriteString(`END)`)
	alias(c.w, csel.FieldName)
}

func (c *compilerContext) renderRemoteRelColumns(sel, csel *qcode.Select) {
	colWithTableID(c.w, sel.Ti.Name, sel.ID, csel.Rel.Left.Col.Name)
	alias(c.w, csel.Rel.Right.VTable)
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
	alias(c.w, fn.FieldName)
}

func (c *compilerContext) renderFunctionSearchRank(sel *qcode.Select, fn qcode.Function) {
	cn := sel.Ti.TSVCol.Name
	arg := sel.Args["search"]

	c.w.WriteString(`ts_rank(`)
	colWithTable(c.w, sel.Ti.Name, cn)
	if sel.Ti.Schema.DBVersion() >= 110000 {
		c.w.WriteString(`, websearch_to_tsquery(`)
	} else {
		c.w.WriteString(`, to_tsquery(`)
	}
	c.md.renderParam(c.w, Param{Name: arg.Val, Type: "text"})
	c.w.WriteString(`))`)
}

func (c *compilerContext) renderFunctionSearchHeadline(sel *qcode.Select, fn qcode.Function) {
	arg := sel.Args["search"]

	c.w.WriteString(`ts_headline(`)
	colWithTable(c.w, sel.Ti.Name, fn.Col.Name)
	if sel.Ti.Schema.DBVersion() >= 110000 {
		c.w.WriteString(`, websearch_to_tsquery(`)
	} else {
		c.w.WriteString(`, to_tsquery(`)
	}
	c.md.renderParam(c.w, Param{Name: arg.Val, Type: "text"})
	c.w.WriteString(`))`)
}

func (c *compilerContext) renderOtherFunction(sel *qcode.Select, fn qcode.Function) {
	c.w.WriteString(fn.Name)
	c.w.WriteString(`(`)
	colWithTable(c.w, sel.Ti.Name, fn.Col.Name)
	_, _ = c.w.WriteString(`)`)
}

func (c *compilerContext) renderBaseColumns(sel *qcode.Select) {
	i := 0
	for _, col := range sel.Cols {
		if i != 0 {
			c.w.WriteString(`, `)
		}
		colWithTable(c.w, sel.Ti.Name, col.Col.Name)
		i++
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
	squoted(c.w, sel.Ti.Name)
	c.w.WriteString(` :: text) AS "__typename"`)
}
