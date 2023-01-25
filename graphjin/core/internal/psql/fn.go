package psql

import "github.com/dosco/graphjin/core/v3/internal/qcode"

func (c *compilerContext) renderFunctionSearchRank(sel *qcode.Select, f qcode.Field) {
	if c.ct == "mysql" {
		c.w.WriteString(`0`)
		return
	}

	c.w.WriteString(`ts_rank(`)
	for i, col := range sel.Ti.FullText {
		if i != 0 {
			c.w.WriteString(` || `)
		}
		c.colWithTable(sel.Table, col.Name)
	}
	if c.cv >= 110000 {
		c.w.WriteString(`, websearch_to_tsquery(`)
	} else {
		c.w.WriteString(`, to_tsquery(`)
	}
	arg, _ := sel.GetInternalArg("search")
	c.renderParam(Param{Name: arg.Val, Type: "text"})
	c.w.WriteString(`))`)
}

func (c *compilerContext) renderFunctionSearchHeadline(sel *qcode.Select, f qcode.Field) {
	if c.ct == "mysql" {
		c.w.WriteString(`''`)
		return
	}

	c.w.WriteString(`ts_headline(`)
	c.colWithTable(sel.Table, f.Col.Name)
	if c.cv >= 110000 {
		c.w.WriteString(`, websearch_to_tsquery(`)
	} else {
		c.w.WriteString(`, to_tsquery(`)
	}
	arg, _ := sel.GetInternalArg("search")
	c.renderParam(Param{Name: arg.Val, Type: "text"})
	c.w.WriteString(`))`)
}

func (c *compilerContext) renderTableFunction(sel *qcode.Select) {
	c.renderFunction(sel.Table, sel.Args)
	c.alias(sel.Table)
}

func (c *compilerContext) renderFieldFunction(sel *qcode.Select, f qcode.Field) {
	switch f.Func.Name {
	case "search_rank":
		c.renderFunctionSearchRank(sel, f)
	case "search_headline":
		c.renderFunctionSearchHeadline(sel, f)
	default:
		c.renderFunction(f.Func.Name, f.Args)
	}
}

func (c *compilerContext) renderFunction(name string, args []qcode.Arg) {
	c.w.WriteString(name)
	c.w.WriteString(`(`)

	i := 0
	for _, a := range args {
		if a.Name == "" {
			if i != 0 {
				c.w.WriteString(`, `)
			}
			c.renderFuncArgVal(a)
			i++
		}
	}
	for _, a := range args {
		if a.Name != "" {
			if i != 0 {
				c.w.WriteString(`, `)
			}
			c.w.WriteString(a.Name + ` => `)
			c.renderFuncArgVal(a)
			i++
		}
	}
	_, _ = c.w.WriteString(`)`)
}

func (c *compilerContext) renderFuncArgVal(a qcode.Arg) {
	switch a.Type {
	case qcode.ArgTypeCol:
		c.colWithTable(a.Col.Table, a.Col.Name)
	case qcode.ArgTypeVar:
		c.renderParam(Param{Name: a.Val, Type: a.DType})
	default:
		c.squoted(a.Val)
	}
}
