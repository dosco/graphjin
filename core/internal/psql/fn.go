package psql

import "github.com/dosco/graphjin/core/v3/internal/qcode"

func (c *compilerContext) renderFunctionSearchRank(sel *qcode.Select, f qcode.Field) {
	c.dialect.RenderSearchRank(c, sel, f)
}

func (c *compilerContext) renderFunctionSearchHeadline(sel *qcode.Select, f qcode.Field) {
	c.dialect.RenderSearchHeadline(c, sel, f)
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
		// Add proper casting for JSON/JSONB parameters
		if a.DType == "json" || a.DType == "jsonb" {
			c.w.WriteString(" :: ")
			c.w.WriteString(a.DType)
		}
	default:
		c.squoted(a.Val)
	}
}
