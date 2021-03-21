package psql

import (
	"strings"

	"github.com/dosco/graphjin/core/internal/qcode"
	"github.com/dosco/graphjin/core/internal/sdata"
	"github.com/dosco/graphjin/core/internal/util"
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

			default:
				if !c.skipNested && len(val.Rels) != 0 {
					c.renderNestedExp(val)
				} else {
					c.renderOp(val)
				}
			}
		}
	}
}

func (c *expContext) renderNestedExp(ex *qcode.Exp) {
	firstRel := ex.Rels[0]
	c.w.WriteString(`EXISTS (SELECT 1 FROM `)
	c.w.WriteString(firstRel.Left.Col.Table)

	if len(ex.Rels) > 1 {
		for _, rel := range ex.Rels[1:(len(ex.Rels) - 1)] {
			c.renderJoin(rel, -1)
		}
	}

	c.w.WriteString(` WHERE `)
	lastRel := ex.Rels[(len(ex.Rels) - 1)]
	c.renderExp(lastRel.Left.Ti, ex, true)

	c.w.WriteString(` AND (`)
	c.renderRel(c.ti, firstRel, -1, nil)
	c.w.WriteString(`))`)
}

func (c *expContext) renderOp(ex *qcode.Exp) {
	if ex.Op == qcode.OpNop {
		return
	}

	if c.renderValPrefix(ex) {
		return
	}

	if ex.Col.Name != "" {
		c.w.WriteString(`((`)
		if ex.Type == qcode.ValRef && ex.Op == qcode.OpIsNull {
			colWithTable(c.w, ex.Table, ex.Col.Name)
		} else {
			colWithTable(c.w, c.ti.Name, ex.Col.Name)
		}
		c.w.WriteString(`) `)
	}

	op := ex.Op
	if op == qcode.OpAutoEq {
		switch {
		case ex.Col.Array && ex.Type == qcode.ValList:
			op = qcode.OpContainedIn
		case ex.Col.Array:
			op = qcode.OpContains
		case ex.Type == qcode.ValList:
			op = qcode.OpIn
		}
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
		c.w.WriteString(`~`)
	case qcode.OpNotRegex:
		c.w.WriteString(`!~`)
	case qcode.OpIRegex:
		c.w.WriteString(`~*`)
	case qcode.OpNotIRegex:
		c.w.WriteString(`!~*`)
	case qcode.OpContains:
		c.w.WriteString(`@>`)
	case qcode.OpContainedIn:
		c.w.WriteString(`<@`)
	case qcode.OpHasKey:
		c.w.WriteString(`?`)
	case qcode.OpHasKeyAny:
		c.w.WriteString(`?|`)
	case qcode.OpHasKeyAll:
		c.w.WriteString(`?&`)

	case qcode.OpEqualsTrue:
		c.w.WriteString(`(`)
		c.renderParam(Param{Name: ex.Val, Type: "boolean"})
		c.w.WriteString(` IS TRUE)`)
		return

	case qcode.OpNotEqualsTrue:
		c.w.WriteString(`(`)
		c.renderParam(Param{Name: ex.Val, Type: "boolean"})
		c.w.WriteString(` IS NOT TRUE)`)
		return

	case qcode.OpIsNull:
		if strings.EqualFold(ex.Val, "true") {
			c.w.WriteString(`IS NULL)`)
		} else {
			c.w.WriteString(`IS NOT NULL)`)
		}
		return

	case qcode.OpTsQuery:
		switch c.ct {
		case "mysql":
			//MATCH (name) AGAINST ('phone' IN BOOLEAN MODE);
			c.w.WriteString(`(MATCH(`)
			for i, col := range c.ti.FullText {
				if i != 0 {
					c.w.WriteString(`, `)
				}
				colWithTable(c.w, c.ti.Name, col.Name)
			}
			c.w.WriteString(`) AGAINST (`)
			c.renderParam(Param{Name: ex.Val, Type: "text"})
			c.w.WriteString(` IN NATURAL LANGUAGE MODE))`)

		default:
			//fmt.Fprintf(w, `(("%s") @@ websearch_to_tsquery('%s'))`, c.ti.TSVCol, val.Val)
			c.w.WriteString(`((`)
			for i, col := range c.ti.FullText {
				if i != 0 {
					c.w.WriteString(` OR (`)
				}
				colWithTable(c.w, c.ti.Name, col.Name)
				if c.cv >= 110000 {
					c.w.WriteString(`) @@ websearch_to_tsquery(`)
				} else {
					c.w.WriteString(`) @@ to_tsquery(`)
				}
				c.renderParam(Param{Name: ex.Val, Type: "text"})
				c.w.WriteString(`)`)
			}
			c.w.WriteString(`)`)
		}
		return
	}
	c.w.WriteString(` `)

	switch {
	case ex.Type == qcode.ValList:
		c.renderList(ex)
	default:
		c.renderVal(ex)
	}
	c.w.WriteString(`)`)
}

func (c *expContext) renderValPrefix(ex *qcode.Exp) bool {
	if ex.Type == qcode.ValVar {
		return c.renderValVarPrefix(ex)
	}
	return false
}

func (c *expContext) renderValVarPrefix(ex *qcode.Exp) bool {
	if ex.Op == qcode.OpIn || ex.Op == qcode.OpNotIn {
		if c.ct == "mysql" {
			c.w.WriteString(`JSON_CONTAINS(`)
			c.renderParam(Param{Name: ex.Val, Type: ex.Col.Type, IsArray: true})
			c.w.WriteString(`, CAST(`)
			colWithTable(c.w, c.ti.Name, ex.Col.Name)
			c.w.WriteString(` AS JSON), '$')`)
			return true
		}
	}
	return false
}

func (c *expContext) renderVal(ex *qcode.Exp) {
	switch ex.Type {
	case qcode.ValVar:
		c.renderValVar(ex)

	case qcode.ValRef:
		colWithTable(c.w, ex.Table, ex.Col.Name)

	default:
		if len(ex.Path) == 0 {
			c.squoted(ex.Val)
			return
		}

		path := append(c.prefixPath, ex.Path...)
		j := (len(path) - 1)

		c.w.WriteString(`CAST(i.j`)
		for i := 0; i < j; i++ {
			c.w.WriteString(`->`)
			c.squoted(path[i])
		}
		c.w.WriteString(`->>`)
		c.squoted(path[j])
		c.w.WriteString(` AS `)
		c.w.WriteString(ex.Col.Type)
		c.w.WriteString(`)`)
	}
}

func (c *expContext) renderValVar(ex *qcode.Exp) {
	val, isVal := c.svars[ex.Val]

	switch {
	case isVal && strings.HasPrefix(val, "sql:"):
		c.w.WriteString(`(`)
		c.renderVar(val[4:])
		c.w.WriteString(`)`)

	case isVal:
		c.w.WriteString(`'`)
		c.renderVar(val)
		c.w.WriteString(`'`)

	case ex.Op == qcode.OpIn || ex.Op == qcode.OpNotIn:
		c.w.WriteString(`(ARRAY(SELECT json_array_elements_text(`)
		c.renderParam(Param{Name: ex.Val, Type: ex.Col.Type, IsArray: true})
		c.w.WriteString(`))`)
		c.w.WriteString(` :: `)
		c.w.WriteString(ex.Col.Type)
		c.w.WriteString(`[])`)

	default:
		c.renderParam(Param{Name: ex.Val, Type: ex.Col.Type, IsArray: false})
	}
}

func (c *expContext) renderList(ex *qcode.Exp) {
	c.w.WriteString(`(ARRAY[`)
	for i := range ex.ListVal {
		if i != 0 {
			c.w.WriteString(`, `)
		}
		switch ex.ListType {
		case qcode.ValBool, qcode.ValNum:
			c.w.WriteString(ex.ListVal[i])
		case qcode.ValStr:
			c.w.WriteString(`'`)
			c.w.WriteString(ex.ListVal[i])
			c.w.WriteString(`'`)
		}
	}
	c.w.WriteString(`])`)
}
