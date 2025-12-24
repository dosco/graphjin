package psql

import (
	"fmt"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
	"github.com/dosco/graphjin/core/v3/internal/util"
)

type expContext struct {
	*compilerContext
	ti         sdata.DBTable
	prefixPath []string
	skipNested bool
}

func (c *compilerContext) renderExp(ti sdata.DBTable, ex *qcode.Exp, skipNested bool) {
	c.renderExpPath(ti, ex, skipNested, nil)
}

func (c *compilerContext) renderExpPath(ti sdata.DBTable, ex *qcode.Exp, skipNested bool, prefixPath []string) {
	ec := expContext{
		compilerContext: c,
		ti:              ti,
		prefixPath:      prefixPath,
		skipNested:      skipNested,
	}
	ec.render(ex)
}

func (c *expContext) render(ex *qcode.Exp) {
	st := util.NewStackInf()
	st.Push(ex)

	for {
		if st.Len() == 0 {
			break
		}

		intf := st.Pop()

		switch val := intf.(type) {
		case int32:
			switch val {
			case '(':
				c.w.WriteString(`(`)
			case ')':
				c.w.WriteString(`)`)
			}

		case qcode.ExpOp:
			switch val {
			case qcode.OpAnd:
				c.w.WriteString(` AND `)
			case qcode.OpOr:
				c.w.WriteString(` OR `)
			case qcode.OpNot:
				c.w.WriteString(`NOT `)
			case qcode.OpFalse:
				c.w.WriteString(`false`)
			}

		case *qcode.Exp:
			if val == nil {
				return
			}
			switch val.Op {
			case qcode.OpFalse:
				st.Push(val.Op)

			case qcode.OpAnd, qcode.OpOr:
				st.Push(')')
				for i := len(val.Children) - 1; i >= 0; i-- {
					st.Push(val.Children[i])
					if i > 0 {
						st.Push(val.Op)
					}
				}
				st.Push('(')

			case qcode.OpNot:
				st.Push(val.Children[0])
				st.Push(qcode.OpNot)

			case qcode.OpSelectExists:
				if !c.skipNested {
					c.renderNestedExp(st, val)
				}
			default:
				c.renderOp(val)
			}
		}
	}
}

func (c *expContext) renderNestedExp(st *util.StackInf, ex *qcode.Exp) {
	firstJoin := ex.Joins[0]
	c.w.WriteString(`EXISTS (SELECT 1 FROM `)
	c.table(nil, firstJoin.Rel.Left.Col.Schema, firstJoin.Rel.Left.Col.Table, true)

	if len(ex.Joins) > 1 {
		for i := 1; i < len(ex.Joins); i++ {
			c.renderJoin(ex.Joins[i])
		}
	}

	c.w.WriteString(` WHERE `)
	c.render(firstJoin.Filter)

	c.w.WriteString(` AND `)
	st.Push(')')
	for i := len(ex.Children) - 1; i >= 0; i-- {
		st.Push(ex.Children[i])
	}
}

func (c *expContext) renderOp(ex *qcode.Exp) {
	if ex.Op == qcode.OpNop {
		return
	}

	if c.renderValPrefix(ex) {
		return
	}

	// Handle TsQuery early - it generates a self-contained condition without column prefix
	if ex.Op == qcode.OpTsQuery {
		c.dialect.RenderTsQuery(c, c.ti, ex)
		return
	}

	if ex.Left.Col.Name != "" {
		var table string
		if ex.Left.Table == "" {
			table = ex.Left.Col.Table
		} else {
			table = ex.Left.Table
		}

		var colName string
		if ex.Left.ColName != "" {
			colName = ex.Left.ColName
		} else {
			colName = ex.Left.Col.Name
		}

		c.w.WriteString(`((`)

		// Handle JSON path operations
		if len(ex.Left.Path) > 0 {
			switch ex.Right.ValType {
			case qcode.ValBool:
				c.dialect.RenderTryCast(c, func() {
					c.renderJSONPathColumn(table, colName, ex.Left.Path, ex.Left.ID)
				}, "boolean")

			case qcode.ValNum:
				c.dialect.RenderTryCast(c, func() {
					c.renderJSONPathColumn(table, colName, ex.Left.Path, ex.Left.ID)
				}, "numeric")

			default:
				c.renderJSONPathColumn(table, colName, ex.Left.Path, ex.Left.ID)
			}

		} else {
			if ex.Left.ID == -1 {
				c.colWithTable(table, colName)
			} else {
				c.colWithTableID(table, ex.Left.ID, colName)
			}
		}
		c.w.WriteString(`) `)
	}

	// Handle standard operators first
	switch ex.Op {
	case qcode.OpEquals:
		c.w.WriteString(`=`)
	case qcode.OpNotEquals:
		c.w.WriteString(`!=`)
	case qcode.OpNotDistinct:
		c.w.WriteString(`IS NOT DISTINCT FROM`)
	case qcode.OpDistinct:
		c.w.WriteString(`IS DISTINCT FROM`)
	case qcode.OpGreaterOrEquals:
		c.w.WriteString(`>=`)
	case qcode.OpLesserOrEquals:
		c.w.WriteString(`<=`)
	case qcode.OpGreaterThan:
		c.w.WriteString(`>`)
	case qcode.OpLesserThan:
		c.w.WriteString(`<`)
	
	case qcode.OpEqualsTrue:
		c.w.WriteString(`(`)
		c.renderParam(Param{Name: ex.Right.Val, Type: "boolean"})
		c.w.WriteString(` IS TRUE)`)
		return

	case qcode.OpNotEqualsTrue:
		c.w.WriteString(`(`)
		c.renderParam(Param{Name: ex.Right.Val, Type: "boolean"})
		c.w.WriteString(` IS NOT TRUE)`)
		return

	case qcode.OpIsNull:
		if strings.EqualFold(ex.Right.Val, "false") {
			c.w.WriteString(`IS NOT NULL)`)
		} else {
			c.w.WriteString(`IS NULL)`)
		}
		return

	case qcode.OpIsNotNull:
		if strings.EqualFold(ex.Right.Val, "false") {
			c.w.WriteString(`IS NULL)`)
		} else {
			c.w.WriteString(`IS NOT NULL)`)
		}
		return


	// Note: OpTsQuery is handled early in renderOp, before column prefix logic

	default:
		opStr, err := c.dialect.RenderOp(ex.Op)
		if err != nil {
			c.err = err
			return
		}
		if opStr != "" {
			c.w.WriteString(opStr)
		} else {
			// If not handled, logic error or unknown op?
			// Just ignore or error?
		}
	}
	c.w.WriteString(` `)

	switch ex.Right.ValType {
	case qcode.ValList:
		c.renderList(ex)
	default:
		c.renderVal(ex)
	}
	c.w.WriteString(`)`)
}

func (c *expContext) renderValPrefix(ex *qcode.Exp) bool {
	if c.dialect.RenderValPrefix(c, ex) {
		return true
	}
	return false
}

func (c *expContext) renderVal(ex *qcode.Exp) {
	switch {
	case ex.Right.ValType == qcode.ValDBVar:
		c.dialect.RenderVar(c, ex.Right.Val)

	case ex.Right.ValType == qcode.ValVar:
		c.renderValVar(ex)

	case ex.Right.ValType == qcode.ValSubQuery:
		c.w.WriteString(`(SELECT `)
		if ex.Right.ColName != "" {
			c.colWithTable(ex.Right.Table, ex.Right.ColName)
		} else {
			c.colWithTable(ex.Right.Table, ex.Right.Col.Name)
		}
		c.w.WriteString(` FROM `)
		c.quoted(ex.Right.Table)
		c.w.WriteString(`)`)

	case !ex.Right.Col.Array && (ex.Op == qcode.OpContains ||
		ex.Op == qcode.OpContainedIn ||
		ex.Op == qcode.OpHasInCommon):
		c.w.WriteString(`CAST(ARRAY[`)
		c.colWithTable(c.ti.Name, ex.Right.Col.Name)
		c.w.WriteString(`] AS `)
		c.w.WriteString(ex.Right.Col.Type)
		c.w.WriteString(`[])`)

	case ex.Right.Col.Name != "":
		var table string
		if ex.Right.Table == "" {
			table = ex.Right.Col.Table
		} else {
			table = ex.Right.Table
		}

		var colName string
		if ex.Right.ColName != "" {
			colName = ex.Right.ColName
		} else {
			colName = ex.Right.Col.Name
		}

		pid := ex.Right.ID
		if ex.Right.ID != -1 {
			pid = ex.Right.ID
		}

		c.w.WriteString(`(`)
		if ex.Right.Col.Array {
			c.renderValArrayColumn(ex, table, pid)
		} else {
			if pid == -1 {
				c.colWithTable(table, colName)
			} else {
				c.colWithTableID(table, pid, colName)
			}
		}
		c.w.WriteString(`)`)

	default:
		if len(ex.Right.Path) == 0 {
			c.dialect.RenderLiteral(c, ex.Right.Val, ex.Right.ValType)
			return
		} else if c.dialect.Name() == "postgres" && (len(c.prefixPath) != 0 || len(ex.Right.Path) != 0) {
			// Build path from JSON input (i.j), matching original Postgres behavior
			path := append(c.prefixPath, ex.Right.Path...)
			j := (len(path) - 1)

			c.w.WriteString(`CAST(i.j`)
			for i := 0; i < j; i++ {
				c.w.WriteString(`->`)
				c.squoted(path[i])
			}
			c.w.WriteString(`->>`)
			c.squoted(path[j])
			c.w.WriteString(` AS `)
			c.w.WriteString(ex.Left.Col.Type)
			c.w.WriteString(`)`)
		} else {
			// If not postgres or path is empty, render as a literal.
			// This handles cases where JSON path is not supported or not applicable.
			c.dialect.RenderLiteral(c, ex.Right.Val, ex.Right.ValType)
		}
	}
}

func (c *expContext) renderValVar(ex *qcode.Exp) {
	val, isVal := c.svars[ex.Right.Val]
	switch {
	case c.dialect.RenderValVar(c, ex, val):
		return

	case isVal && strings.HasPrefix(val, "sql:"):
		c.w.WriteString(`(`)
		c.renderVar(val[4:])
		c.w.WriteString(`)`)

	case isVal:
		c.w.WriteString(`'`)
		c.renderVar(val)
		c.w.WriteString(`'`)

	default:
		c.renderParam(Param{Name: ex.Right.Val, Type: ex.Left.Col.Type, IsArray: false})
	}
}

func (c *expContext) renderList(ex *qcode.Exp) {
	c.dialect.RenderList(c, ex)
}



func (c *compilerContext) renderValArrayColumn(ex *qcode.Exp, table string, pid int32) {
	c.dialect.RenderValArrayColumn(c, ex, table, pid)
}


func (c *expContext) renderJSONPathColumn(table, colName string, path []string, selID int32) {
	// Build the JSON path
	t := table
	if selID != -1 {
		t = fmt.Sprintf("%s_%d", table, selID)
	}
	c.dialect.RenderJSONPath(c, t, colName, path)
}
