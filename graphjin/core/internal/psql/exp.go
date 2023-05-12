package psql

import (
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
	c.table(firstJoin.Rel.Left.Col.Schema, firstJoin.Rel.Left.Col.Table, true)

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

	if ex.Left.Col.Name != "" {
		var table string
		if ex.Left.Table == "" {
			table = ex.Left.Col.Table
		} else {
			table = ex.Left.Table
		}

		c.w.WriteString(`((`)
		if ex.Left.ID == -1 {
			c.colWithTable(table, ex.Left.Col.Name)
		} else {
			c.colWithTableID(table, ex.Left.ID, ex.Left.Col.Name)
		}
		c.w.WriteString(`) `)
	}

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
	case qcode.OpIn:
		c.w.WriteString(`= ANY`)
	case qcode.OpNotIn:
		c.w.WriteString(`!= ALL`)
	case qcode.OpLike:
		c.w.WriteString(`LIKE`)
	case qcode.OpNotLike:
		c.w.WriteString(`NOT LIKE`)
	case qcode.OpILike:
		c.w.WriteString(`ILIKE`)
	case qcode.OpNotILike:
		c.w.WriteString(`NOT ILIKE`)
	case qcode.OpSimilar:
		c.w.WriteString(`SIMILAR TO`)
	case qcode.OpNotSimilar:
		c.w.WriteString(`NOT SIMILAR TO`)
	case qcode.OpRegex:
		switch c.ct {
		case "mysql":
			c.w.WriteString(`REGEXP`)
		default:
			c.w.WriteString(`~`)
		}
	case qcode.OpNotRegex:
		switch c.ct {
		case "mysql":
			c.w.WriteString(`NOT REGEXP`)
		default:
			c.w.WriteString(`!~`)
		}
	case qcode.OpIRegex:
		switch c.ct {
		case "mysql":
			c.w.WriteString(`REGEXP`)
		default:
			c.w.WriteString(`~*`)
		}
	case qcode.OpNotIRegex:
		switch c.ct {
		case "mysql":
			c.w.WriteString(`NOT REGEXP`)
		default:
			c.w.WriteString(`!~*`)
		}
	case qcode.OpContains:
		c.w.WriteString(`@>`)
	case qcode.OpContainedIn:
		c.w.WriteString(`<@`)
	case qcode.OpHasInCommon:
		c.w.WriteString(`&&`)
	case qcode.OpHasKey:
		c.w.WriteString(`?`)
	case qcode.OpHasKeyAny:
		c.w.WriteString(`?|`)
	case qcode.OpHasKeyAll:
		c.w.WriteString(`?&`)

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

	case qcode.OpTsQuery:
		switch c.ct {
		case "mysql":
			// MATCH (name) AGAINST ('phone' IN BOOLEAN MODE);
			c.w.WriteString(`(MATCH(`)
			for i, col := range c.ti.FullText {
				if i != 0 {
					c.w.WriteString(`, `)
				}
				c.colWithTable(c.ti.Name, col.Name)
			}
			c.w.WriteString(`) AGAINST (`)
			c.renderParam(Param{Name: ex.Right.Val, Type: "text"})
			c.w.WriteString(` IN NATURAL LANGUAGE MODE))`)

		default:
			// fmt.Fprintf(w, `(("%s") @@ websearch_to_tsquery('%s'))`, c.ti.TSVCol, val.Val)
			c.w.WriteString(`((`)
			for i, col := range c.ti.FullText {
				if i != 0 {
					c.w.WriteString(` OR (`)
				}
				c.colWithTable(c.ti.Name, col.Name)
				if c.cv >= 110000 {
					c.w.WriteString(`) @@ websearch_to_tsquery(`)
				} else {
					c.w.WriteString(`) @@ to_tsquery(`)
				}
				c.renderParam(Param{Name: ex.Right.Val, Type: "text"})
				c.w.WriteString(`)`)
			}
			c.w.WriteString(`)`)
		}
		return
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
	switch {
	case c.ct == "mysql" && (ex.Op == qcode.OpHasKey ||
		ex.Op == qcode.OpHasKeyAny ||
		ex.Op == qcode.OpHasKeyAll):
		var optype string
		switch ex.Op {
		case qcode.OpHasKey, qcode.OpHasKeyAny:
			optype = "'one'"
		case qcode.OpHasKeyAll:
			optype = "'all'"
		}
		c.w.WriteString("JSON_CONTAINS_PATH(")
		c.colWithTable(c.ti.Name, ex.Left.Col.Name)
		c.w.WriteString(", " + optype)
		for i := range ex.Right.ListVal {
			c.w.WriteString(`, '$.` + ex.Right.ListVal[i] + `'`)
		}
		c.w.WriteString(") = 1")
		return true

	case c.ct == "mysql" && ex.Right.ValType == qcode.ValVar &&
		(ex.Op == qcode.OpIn || ex.Op == qcode.OpNotIn):
		c.w.WriteString(`JSON_CONTAINS(`)
		c.renderParam(Param{Name: ex.Right.Val, Type: ex.Left.Col.Type, IsArray: true})
		c.w.WriteString(`, CAST(`)
		c.colWithTable(c.ti.Name, ex.Left.Col.Name)
		c.w.WriteString(` AS JSON), '$')`)
		return true
	}
	return false
}

func (c *expContext) renderVal(ex *qcode.Exp) {
	switch {
	case ex.Right.ValType == qcode.ValVar:
		c.renderValVar(ex)

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

		pid := ex.Right.ID
		if ex.Right.ID != -1 {
			pid = ex.Right.ID
		}

		c.w.WriteString(`(`)
		if ex.Right.Col.Array {
			c.renderValArrayColumn(ex, table, pid)
		} else {
			if pid == -1 {
				c.colWithTable(table, ex.Right.Col.Name)
			} else {
				c.colWithTableID(table, pid, ex.Right.Col.Name)
			}
		}
		c.w.WriteString(`)`)

	default:
		if len(ex.Right.Path) == 0 {
			c.squoted(ex.Right.Val)
			return
		}

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
	}
}

func (c *expContext) renderValVar(ex *qcode.Exp) {
	val, isVal := c.svars[ex.Right.Val]

	switch {
	case isVal && strings.HasPrefix(val, "sql:"):
		c.w.WriteString(`(`)
		c.renderVar(val[4:])
		c.w.WriteString(`)`)

	case isVal:
		c.w.WriteString(`'`)
		c.renderVar(val)
		c.w.WriteString(`'`)

	case ex.Op == qcode.OpIn || ex.Op == qcode.OpNotIn || ex.Op == qcode.OpContains || ex.Op == qcode.OpHasInCommon:
		c.w.WriteString(`(ARRAY(SELECT json_array_elements_text(`)
		c.renderParam(Param{Name: ex.Right.Val, Type: ex.Left.Col.Type, IsArray: true})
		c.w.WriteString(`))`)
		c.w.WriteString(` :: `)
		c.w.WriteString(ex.Left.Col.Type)
		c.w.WriteString(`[])`)

	default:
		c.renderParam(Param{Name: ex.Right.Val, Type: ex.Left.Col.Type, IsArray: false})
	}
}

func (c *expContext) renderList(ex *qcode.Exp) {
	switch c.ct {
	case "mysql":
		c.renderListMysql(ex)
	default:
		c.renderListPostgres(ex)
	}
}

func (c *expContext) renderListPostgres(ex *qcode.Exp) {
	if strings.HasPrefix(ex.Left.Col.Type, "json") {
		c.w.WriteString(`(ARRAY[`)
		c.renderListBodyPostgres(ex)
		c.w.WriteString(`])`)
	} else {
		c.w.WriteString(`(CAST(ARRAY[`)
		c.renderListBodyPostgres(ex)
		c.w.WriteString(`] AS `)
		c.w.WriteString(ex.Left.Col.Type)
		c.w.WriteString(`[]))`)
	}
}

func (c *expContext) renderListBodyPostgres(ex *qcode.Exp) {
	for i := range ex.Right.ListVal {
		if i != 0 {
			c.w.WriteString(`, `)
		}
		switch ex.Right.ListType {
		case qcode.ValBool, qcode.ValNum:
			c.w.WriteString(ex.Right.ListVal[i])
		case qcode.ValStr:
			c.w.WriteString(`'`)
			c.w.WriteString(ex.Right.ListVal[i])
			c.w.WriteString(`'`)
		}
	}
}

func (c *expContext) renderListMysql(ex *qcode.Exp) {
	c.w.WriteString(`(`)
	for i := range ex.Right.ListVal {
		if i != 0 {
			c.w.WriteString(` UNION `)
		}
		c.w.WriteString(`SELECT `)
		switch ex.Right.ListType {
		case qcode.ValBool, qcode.ValNum:
			c.w.WriteString(ex.Right.ListVal[i])
		case qcode.ValStr:
			c.w.WriteString(`'`)
			c.w.WriteString(ex.Right.ListVal[i])
			c.w.WriteString(`'`)
		}
	}
	c.w.WriteString(`)`)
}

func (c *compilerContext) renderValArrayColumn(ex *qcode.Exp, table string, pid int32) {
	col := ex.Right.Col
	switch c.ct {
	case "mysql":
		c.w.WriteString(`SELECT _gj_jt.* FROM `)
		c.w.WriteString(`(SELECT CAST(`)
		if pid == -1 {
			c.colWithTable(table, col.Name)
		} else {
			c.colWithTableID(table, pid, col.Name)
		}
		c.w.WriteString(` AS JSON) as ids) j, `)
		c.w.WriteString(`JSON_TABLE(j.ids, "$[*]" COLUMNS(`)
		c.w.WriteString(col.Name)
		c.w.WriteString(` `)
		c.w.WriteString(ex.Left.Col.Type)
		c.w.WriteString(` PATH "$" ERROR ON ERROR)) AS _gj_jt`)

	default:
		if pid == -1 {
			c.colWithTable(table, col.Name)
		} else {
			c.colWithTableID(table, pid, col.Name)
		}
	}
}
