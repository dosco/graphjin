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
	// Expression-aggregate path: a single ArgTypeExpr arg means the
	// field was compiled via the new `expr:` syntax. f.Func.Name is
	// either an aggregate name (sum/avg/min/max/count) — wrap the
	// expression — or empty for the bare ratio-of-aggregates case.
	if len(f.Args) == 1 && f.Args[0].Type == qcode.ArgTypeExpr {
		c.renderFieldExprFunction(sel, f)
		c.renderWindowOver(sel, f)
		return
	}
	switch f.Func.Name {
	case "search_rank":
		c.renderFunctionSearchRank(sel, f)
	case "search_headline":
		c.renderFunctionSearchHeadline(sel, f)
	default:
		c.renderFunction(f.Func.Name, f.Args)
	}
	c.renderWindowOver(sel, f)
}

// renderWindowOver appends the OVER (...) clause for fields tagged with
// the @window directive. No-op when f.Window is nil.
func (c *compilerContext) renderWindowOver(sel *qcode.Select, f qcode.Field) {
	if f.Window == nil {
		return
	}
	w := f.Window
	c.w.WriteString(" OVER (")
	if len(w.Partition) > 0 {
		c.w.WriteString("PARTITION BY ")
		for i, col := range w.Partition {
			if i > 0 {
				c.w.WriteString(", ")
			}
			c.colWithTable(sel.Table, col)
		}
	}
	if len(w.OrderBy) > 0 {
		if len(w.Partition) > 0 {
			c.w.WriteString(" ")
		}
		c.w.WriteString("ORDER BY ")
		for i, ord := range w.OrderBy {
			if i > 0 {
				c.w.WriteString(", ")
			}
			c.colWithTable(sel.Table, ord.Col)
			if ord.Desc {
				c.w.WriteString(" DESC")
			}
		}
	}
	if w.Frame != "" {
		c.w.WriteString(" ")
		c.w.WriteString(w.Frame)
	}
	c.w.WriteString(")")
}

// renderFieldExprFunction emits SQL for a `<name>(expr: ...)` field.
// When f.Func.Name is a known aggregate, the expression is wrapped in
// AGG(...). When the field is a bare expression (ratio-of-aggregates),
// the expression is emitted unwrapped — its internal OpAggSum / etc.
// nodes provide their own aggregation.
func (c *compilerContext) renderFieldExprFunction(sel *qcode.Select, f qcode.Field) {
	expr := f.Args[0].Expr
	if f.Func.Name != "" {
		c.w.WriteString(f.Func.Name)
		c.w.WriteString("(")
		if err := c.renderScalarExp(sel, expr); err != nil {
			c.w.WriteString("/* expr error: ")
			c.w.WriteString(err.Error())
			c.w.WriteString(" */")
		}
		c.w.WriteString(")")
		return
	}
	// Bare expression: emit as-is. Always parenthesize so the result
	// composes safely as a single SELECT-list item.
	c.w.WriteString("(")
	if err := c.renderScalarExp(sel, expr); err != nil {
		c.w.WriteString("/* expr error: ")
		c.w.WriteString(err.Error())
		c.w.WriteString(" */")
	}
	c.w.WriteString(")")
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
