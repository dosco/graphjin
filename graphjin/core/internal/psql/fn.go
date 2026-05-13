package psql

import "github.com/dosco/graphjin/core/v3/internal/qcode"

func (c *compilerContext) renderFunctionSearchRank(sel *qcode.Select, f qcode.Field) {
	c.dialect.RenderSearchRank(c, sel, f)
}

func (c *compilerContext) renderFunctionSearchHeadline(sel *qcode.Select, f qcode.Field) {
	c.dialect.RenderSearchHeadline(c, sel, f)
}

func (co *Compiler) validateWindowFunctions(qc *qcode.QCode) error {
	for i := range qc.Selects {
		sel := &qc.Selects[i]
		for _, f := range sel.Fields {
			if f.Window == nil {
				continue
			}
			if err := co.dialect.ValidateWindowFunction(f); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *compilerContext) renderTableFunction(sel *qcode.Select) {
	c.renderFunction(sel.Table, sel.Args)
	c.alias(sel.Table)
}

func (c *compilerContext) renderFieldFunction(sel *qcode.Select, f qcode.Field) {
	if f.WindowFunc != qcode.WindowFuncNone {
		c.renderFieldWindowFunction(sel, f, sel.Table)
		c.renderWindowOver(sel, f)
		return
	}

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

func (c *compilerContext) renderFieldWindowFunction(sel *qcode.Select, f qcode.Field, table string) {
	c.w.WriteString(f.WindowFunc.String())
	c.w.WriteString("(")
	if f.WindowFunc.IsValueFunc() && len(f.Args) > 0 {
		c.colWithTable(table, f.Args[0].Col.Name)
	}
	c.w.WriteString(")")
}

// renderWindowOver appends the OVER (...) clause for fields tagged with
// the @window directive. No-op when f.Window is nil.
func (c *compilerContext) renderWindowOver(sel *qcode.Select, f qcode.Field) {
	c.renderWindowOverWithTable(sel, f, sel.Table)
}

func (c *compilerContext) renderWindowOverWithTable(sel *qcode.Select, f qcode.Field, table string) {
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
			c.colWithTable(table, col)
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
			c.colWithTable(table, ord.Col)
			if ord.Desc {
				c.w.WriteString(" DESC")
			}
			switch ord.Nulls {
			case qcode.NullsFirst:
				c.w.WriteString(" NULLS FIRST")
			case qcode.NullsLast:
				c.w.WriteString(" NULLS LAST")
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

func (c *compilerContext) renderFunctionWithTable(name string, args []qcode.Arg, table string) {
	c.w.WriteString(name)
	c.w.WriteString(`(`)

	i := 0
	for _, a := range args {
		if a.Name == "" {
			if i != 0 {
				c.w.WriteString(`, `)
			}
			c.renderFuncArgValWithTable(a, table)
			i++
		}
	}
	for _, a := range args {
		if a.Name != "" {
			if i != 0 {
				c.w.WriteString(`, `)
			}
			c.w.WriteString(a.Name + ` => `)
			c.renderFuncArgValWithTable(a, table)
			i++
		}
	}
	_, _ = c.w.WriteString(`)`)
}

func (c *compilerContext) renderFuncArgVal(a qcode.Arg) {
	c.renderFuncArgValWithTable(a, a.Col.Table)
}

func (c *compilerContext) renderFuncArgValWithTable(a qcode.Arg, table string) {
	switch a.Type {
	case qcode.ArgTypeCol:
		if table == "" {
			table = a.Col.Table
		}
		c.colWithTable(table, a.Col.Name)
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
