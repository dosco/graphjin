package psql

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/cespare/xxhash/v2"
	"github.com/dosco/super-graph/qcode"
	"github.com/dosco/super-graph/util"
)

const (
	empty      = ""
	closeBlock = 500
)

type Config struct {
	Schema *DBSchema
	Vars   map[string]string
}

type Compiler struct {
	schema *DBSchema
	vars   map[string]string
}

func NewCompiler(conf Config) *Compiler {
	return &Compiler{conf.Schema, conf.Vars}
}

func (c *Compiler) AddRelationship(child, parent string, rel *DBRel) error {
	return c.schema.SetRel(child, parent, rel)
}

func (c *Compiler) IDColumn(table string) (string, error) {
	t, err := c.schema.GetTable(table)
	if err != nil {
		return empty, err
	}

	return t.PrimaryCol, nil
}

type compilerContext struct {
	w *bytes.Buffer
	s []qcode.Select
	*Compiler
}

func (co *Compiler) CompileEx(qc *qcode.QCode) (uint32, []byte, error) {
	w := &bytes.Buffer{}
	skipped, err := co.Compile(qc, w)
	return skipped, w.Bytes(), err
}

func (co *Compiler) Compile(qc *qcode.QCode, w *bytes.Buffer) (uint32, error) {
	if len(qc.Query.Selects) == 0 {
		return 0, errors.New("empty query")
	}

	c := &compilerContext{w, qc.Query.Selects, co}
	root := &qc.Query.Selects[0]

	st := NewStack()
	st.Push(root.ID + closeBlock)
	st.Push(root.ID)

	//fmt.Fprintf(w, `SELECT json_object_agg('%s', %s) FROM (`,
	//root.FieldName, root.Table)
	c.w.WriteString(`SELECT json_object_agg('`)
	c.w.WriteString(root.FieldName)
	c.w.WriteString(`', `)
	c.w.WriteString(root.Table)
	c.w.WriteString(`) FROM (`)

	var ignored uint32

	for {
		if st.Len() == 0 {
			break
		}

		id := st.Pop()

		if id < closeBlock {
			sel := &c.s[id]

			ti, err := c.schema.GetTable(sel.Table)
			if err != nil {
				return 0, err
			}

			if sel.ID != 0 {
				if err = c.renderJoin(sel); err != nil {
					return 0, err
				}
			}
			skipped, err := c.renderSelect(sel, ti)
			if err != nil {
				return 0, err
			}
			ignored |= skipped

			for _, cid := range sel.Children {
				if hasBit(skipped, uint32(cid)) {
					continue
				}
				child := &c.s[cid]

				st.Push(child.ID + closeBlock)
				st.Push(child.ID)
			}

		} else {
			sel := &c.s[(id - closeBlock)]

			ti, err := c.schema.GetTable(sel.Table)
			if err != nil {
				return 0, err
			}

			err = c.renderSelectClose(sel, ti)
			if err != nil {
				return 0, err
			}

			if sel.ID != 0 {
				if err = c.renderJoinClose(sel); err != nil {
					return 0, err
				}
			}
		}
	}

	c.w.WriteString(`)`)
	alias(c.w, `done_1337`)
	c.w.WriteString(`;`)

	return ignored, nil
}

func (c *compilerContext) processChildren(sel *qcode.Select, ti *DBTableInfo) (uint32, []*qcode.Column) {
	var skipped uint32

	cols := make([]*qcode.Column, 0, len(sel.Cols))
	colmap := make(map[string]struct{}, len(sel.Cols))

	for i := range sel.Cols {
		colmap[sel.Cols[i].Name] = struct{}{}
	}

	for _, id := range sel.Children {
		child := &c.s[id]

		rel, err := c.schema.GetRel(child.Table, ti.Name)
		if err != nil {
			skipped |= (1 << uint(id))
			continue
		}

		switch rel.Type {
		case RelOneToMany:
			fallthrough
		case RelBelongTo:
			if _, ok := colmap[rel.Col2]; !ok {
				cols = append(cols, &qcode.Column{sel.Table, rel.Col2, rel.Col2})
			}
		case RelOneToManyThrough:
			if _, ok := colmap[rel.Col1]; !ok {
				cols = append(cols, &qcode.Column{sel.Table, rel.Col1, rel.Col1})
			}
		case RelRemote:
			if _, ok := colmap[rel.Col1]; !ok {
				cols = append(cols, &qcode.Column{sel.Table, rel.Col1, rel.Col2})
			}
			skipped |= (1 << uint(id))

		default:
			skipped |= (1 << uint(id))
		}
	}

	return skipped, cols
}

func (c *compilerContext) renderSelect(sel *qcode.Select, ti *DBTableInfo) (uint32, error) {
	skipped, childCols := c.processChildren(sel, ti)
	hasOrder := len(sel.OrderBy) != 0

	// SELECT
	if ti.Singular == false {
		//fmt.Fprintf(w, `SELECT coalesce(json_agg("%s"`, c.sel.Table)
		c.w.WriteString(`SELECT coalesce(json_agg("`)
		c.w.WriteString(sel.Table)
		c.w.WriteString(`"`)

		if hasOrder {
			err := c.renderOrderBy(sel)
			if err != nil {
				return skipped, err
			}
		}

		//fmt.Fprintf(w, `), '[]') AS "%s" FROM (`, c.sel.Table)
		c.w.WriteString(`), '[]')`)
		alias(c.w, sel.Table)
		c.w.WriteString(` FROM (`)
	}

	// ROW-TO-JSON
	c.w.WriteString(`SELECT `)

	if len(sel.DistinctOn) != 0 {
		c.renderDistinctOn(sel)
	}

	c.w.WriteString(`row_to_json((`)

	//fmt.Fprintf(w, `SELECT "sel_%d" FROM (SELECT `, c.sel.ID)
	c.w.WriteString(`SELECT "sel_`)
	int2string(c.w, sel.ID)
	c.w.WriteString(`" FROM (SELECT `)

	// Combined column names
	c.renderColumns(sel)

	c.renderRemoteRelColumns(sel)

	err := c.renderJoinedColumns(sel, skipped)
	if err != nil {
		return skipped, err
	}

	//fmt.Fprintf(w, `) AS "sel_%d"`, c.sel.ID)
	c.w.WriteString(`)`)
	aliasWithID(c.w, "sel", sel.ID)

	//fmt.Fprintf(w, `)) AS "%s"`, c.sel.Table)
	c.w.WriteString(`))`)
	alias(c.w, sel.Table)
	// END-ROW-TO-JSON

	if hasOrder {
		c.renderOrderByColumns(sel)
	}
	// END-SELECT

	// FROM (SELECT .... )
	err = c.renderBaseSelect(sel, ti, childCols, skipped)
	if err != nil {
		return skipped, err
	}
	// END-FROM

	return skipped, nil
}

func (c *compilerContext) renderSelectClose(sel *qcode.Select, ti *DBTableInfo) error {
	hasOrder := len(sel.OrderBy) != 0

	if hasOrder {
		err := c.renderOrderBy(sel)
		if err != nil {
			return err
		}
	}

	if len(sel.Paging.Limit) != 0 {
		//fmt.Fprintf(w, ` LIMIT ('%s') :: integer`, c.sel.Paging.Limit)
		c.w.WriteString(` LIMIT ('`)
		c.w.WriteString(sel.Paging.Limit)
		c.w.WriteString(`') :: integer`)

	} else if ti.Singular {
		c.w.WriteString(` LIMIT ('1') :: integer`)

	} else {
		c.w.WriteString(` LIMIT ('20') :: integer`)
	}

	if len(sel.Paging.Offset) != 0 {
		//fmt.Fprintf(w, ` OFFSET ('%s') :: integer`, c.sel.Paging.Offset)
		c.w.WriteString(`OFFSET ('`)
		c.w.WriteString(sel.Paging.Offset)
		c.w.WriteString(`') :: integer`)
	}

	if ti.Singular == false {
		//fmt.Fprintf(w, `) AS "%s_%d"`, c.sel.Table, c.sel.ID)
		c.w.WriteString(`)`)
		aliasWithID(c.w, sel.Table, sel.ID)
	}

	return nil
}

func (c *compilerContext) renderJoin(sel *qcode.Select) error {
	c.w.WriteString(` LEFT OUTER JOIN LATERAL (`)
	return nil
}

func (c *compilerContext) renderJoinClose(sel *qcode.Select) error {
	//fmt.Fprintf(w, `) AS "%s_%d_join" ON ('true')`, c.sel.Table, c.sel.ID)
	c.w.WriteString(`)`)
	aliasWithIDSuffix(c.w, sel.Table, sel.ID, "_join")
	c.w.WriteString(` ON ('true')`)
	return nil
}

func (c *compilerContext) renderJoinTable(sel *qcode.Select) {
	parent := &c.s[sel.ParentID]

	rel, err := c.schema.GetRel(sel.Table, parent.Table)
	if err != nil {
		panic(err)
	}

	if rel.Type != RelOneToManyThrough {
		return
	}

	pt, err := c.schema.GetTable(parent.Table)
	if err != nil {
		return
	}

	//fmt.Fprintf(w, ` LEFT OUTER JOIN "%s" ON (("%s"."%s") = ("%s_%d"."%s"))`,
	//rel.Through, rel.Through, rel.ColT, c.parent.Table, c.parent.ID, rel.Col1)
	c.w.WriteString(` LEFT OUTER JOIN "`)
	c.w.WriteString(rel.Through)
	c.w.WriteString(`" ON ((`)
	colWithTable(c.w, rel.Through, rel.ColT)
	c.w.WriteString(`) = (`)
	colWithTableID(c.w, pt.Name, parent.ID, rel.Col1)
	c.w.WriteString(`))`)
}

func (c *compilerContext) renderColumns(sel *qcode.Select) {
	for i, col := range sel.Cols {
		if i != 0 {
			io.WriteString(c.w, ", ")
		}
		//fmt.Fprintf(w, `"%s_%d"."%s" AS "%s"`,
		//c.sel.Table, c.sel.ID, col.Name, col.FieldName)
		colWithTableIDAlias(c.w, sel.Table, sel.ID, col.Name, col.FieldName)
	}
}

func (c *compilerContext) renderRemoteRelColumns(sel *qcode.Select) {
	i := 0

	for _, id := range sel.Children {
		child := &c.s[id]

		rel, err := c.schema.GetRel(child.Table, sel.Table)
		if err != nil || rel.Type != RelRemote {
			continue
		}
		if i != 0 || len(sel.Cols) != 0 {
			io.WriteString(c.w, ", ")
		}
		//fmt.Fprintf(w, `"%s_%d"."%s" AS "%s"`,
		//c.sel.Table, c.sel.ID, rel.Col1, rel.Col2)
		colWithTableID(c.w, sel.Table, sel.ID, rel.Col1)
		alias(c.w, rel.Col2)
		i++
	}
}

func (c *compilerContext) renderJoinedColumns(sel *qcode.Select, skipped uint32) error {
	colsRendered := len(sel.Cols) != 0

	for _, id := range sel.Children {
		skipThis := hasBit(skipped, uint32(id))

		if colsRendered && !skipThis {
			io.WriteString(c.w, ", ")
		}
		if skipThis {
			continue
		}
		sel := &c.s[id]

		//fmt.Fprintf(w, `"%s_%d_join"."%s" AS "%s"`,
		//s.Table, s.ID, s.Table, s.FieldName)
		colWithTableIDSuffixAlias(c.w, sel.Table, sel.ID, "_join", sel.Table, sel.FieldName)
	}

	return nil
}

func (c *compilerContext) renderBaseSelect(sel *qcode.Select, ti *DBTableInfo,
	childCols []*qcode.Column, skipped uint32) error {
	var groupBy []int

	isRoot := sel.ID == 0
	isFil := sel.Where != nil
	isSearch := sel.Args["search"] != nil
	isAgg := false

	c.w.WriteString(` FROM (SELECT `)

	for i, col := range sel.Cols {
		cn := col.Name

		_, isRealCol := ti.Columns[cn]

		if !isRealCol {
			if isSearch {
				switch {
				case cn == "search_rank":
					cn = ti.TSVCol
					arg := sel.Args["search"]

					//fmt.Fprintf(w, `ts_rank("%s"."%s", to_tsquery('%s')) AS %s`,
					//c.sel.Table, cn, arg.Val, col.Name)
					c.w.WriteString(`ts_rank(`)
					colWithTable(c.w, sel.Table, cn)
					c.w.WriteString(`, to_tsquery('`)
					c.w.WriteString(arg.Val)
					c.w.WriteString(`')`)
					alias(c.w, col.Name)

				case strings.HasPrefix(cn, "search_headline_"):
					cn = cn[16:]
					arg := sel.Args["search"]

					//fmt.Fprintf(w, `ts_headline("%s"."%s", to_tsquery('%s')) AS %s`,
					//c.sel.Table, cn, arg.Val, col.Name)
					c.w.WriteString(`ts_headlinek(`)
					colWithTable(c.w, sel.Table, cn)
					c.w.WriteString(`, to_tsquery('`)
					c.w.WriteString(arg.Val)
					c.w.WriteString(`')`)
					alias(c.w, col.Name)
				}
			} else {
				pl := funcPrefixLen(cn)
				if pl == 0 {
					//fmt.Fprintf(w, `'%s not defined' AS %s`, cn, col.Name)
					c.w.WriteString(`'`)
					c.w.WriteString(cn)
					c.w.WriteString(` not defined'`)
					alias(c.w, col.Name)
				} else {
					isAgg = true
					fn := cn[0 : pl-1]
					cn := cn[pl:]
					//fmt.Fprintf(w, `%s("%s"."%s") AS %s`, fn, c.sel.Table, cn, col.Name)
					c.w.WriteString(fn)
					c.w.WriteString(`(`)
					colWithTable(c.w, sel.Table, cn)
					c.w.WriteString(`)`)
					alias(c.w, col.Name)
				}
			}
		} else {
			groupBy = append(groupBy, i)
			//fmt.Fprintf(w, `"%s"."%s"`, c.sel.Table, cn)
			colWithTable(c.w, sel.Table, cn)
		}

		if i < len(sel.Cols)-1 || len(childCols) != 0 {
			//io.WriteString(w, ", ")
			c.w.WriteString(`, `)
		}
	}

	for i, col := range childCols {
		if i != 0 {
			//io.WriteString(w, ", ")
			c.w.WriteString(`, `)
		}

		//fmt.Fprintf(w, `"%s"."%s"`, col.Table, col.Name)
		colWithTable(c.w, col.Table, col.Name)
	}

	c.w.WriteString(` FROM `)

	if c.schema.IsAlias(sel.Table) || ti.Singular {
		//fmt.Fprintf(w, ` FROM "%s" AS "%s"`, tn, c.sel.Table)
		tableWithAlias(c.w, ti.Name, sel.Table)
	} else {
		//fmt.Fprintf(w, ` FROM "%s"`, c.sel.Table)
		c.w.WriteString(`"`)
		c.w.WriteString(ti.Name)
		c.w.WriteString(`"`)
	}

	// if tn, ok := c.tmap[sel.Table]; ok {
	// 	//fmt.Fprintf(w, ` FROM "%s" AS "%s"`, tn, c.sel.Table)
	// 	tableWithAlias(c.w, ti.Name, sel.Table)
	// } else {
	// 	//fmt.Fprintf(w, ` FROM "%s"`, c.sel.Table)
	// 	c.w.WriteString(`"`)
	// 	c.w.WriteString(sel.Table)
	// 	c.w.WriteString(`"`)
	// }

	if isRoot && isFil {
		c.w.WriteString(` WHERE (`)
		if err := c.renderWhere(sel, ti); err != nil {
			return err
		}
		c.w.WriteString(`)`)
	}

	if !isRoot {
		c.renderJoinTable(sel)

		c.w.WriteString(` WHERE (`)
		c.renderRelationship(sel)

		if isFil {
			c.w.WriteString(` AND `)
			if err := c.renderWhere(sel, ti); err != nil {
				return err
			}
		}
		c.w.WriteString(`)`)
	}

	if isAgg {
		if len(groupBy) != 0 {
			c.w.WriteString(` GROUP BY `)

			for i, id := range groupBy {
				if i != 0 {
					c.w.WriteString(`, `)
				}
				//fmt.Fprintf(w, `"%s"."%s"`, c.sel.Table, c.sel.Cols[id].Name)
				colWithTable(c.w, sel.Table, sel.Cols[id].Name)
			}
		}
	}

	if len(sel.Paging.Limit) != 0 {
		//fmt.Fprintf(w, ` LIMIT ('%s') :: integer`, c.sel.Paging.Limit)
		c.w.WriteString(` LIMIT ('`)
		c.w.WriteString(sel.Paging.Limit)
		c.w.WriteString(`') :: integer`)

	} else if ti.Singular {
		c.w.WriteString(` LIMIT ('1') :: integer`)

	} else {
		c.w.WriteString(` LIMIT ('20') :: integer`)
	}

	if len(sel.Paging.Offset) != 0 {
		//fmt.Fprintf(w, ` OFFSET ('%s') :: integer`, c.sel.Paging.Offset)
		c.w.WriteString(` OFFSET ('`)
		c.w.WriteString(sel.Paging.Offset)
		c.w.WriteString(`') :: integer`)
	}

	//fmt.Fprintf(w, `) AS "%s_%d"`, c.sel.Table, c.sel.ID)
	c.w.WriteString(`)`)
	aliasWithID(c.w, sel.Table, sel.ID)
	return nil
}

func (c *compilerContext) renderOrderByColumns(sel *qcode.Select) {
	colsRendered := len(sel.Cols) != 0

	for i := range sel.OrderBy {
		if colsRendered {
			//io.WriteString(w, ", ")
			c.w.WriteString(`, `)
		}

		col := sel.OrderBy[i].Col
		//fmt.Fprintf(w, `"%s_%d"."%s" AS "%s_%d_%s_ob"`,
		//c.sel.Table, c.sel.ID, c,
		//c.sel.Table, c.sel.ID, c)
		colWithTableID(c.w, sel.Table, sel.ID, col)
		c.w.WriteString(` AS `)
		tableIDColSuffix(c.w, sel.Table, sel.ID, col, "_ob")
	}
}

func (c *compilerContext) renderRelationship(sel *qcode.Select) {
	parent := c.s[sel.ParentID]

	rel, err := c.schema.GetRel(sel.Table, parent.Table)
	if err != nil {
		panic(err)
	}

	switch rel.Type {
	case RelBelongTo:
		//fmt.Fprintf(w, `(("%s"."%s") = ("%s_%d"."%s"))`,
		//c.sel.Table, rel.Col1, c.parent.Table, c.parent.ID, rel.Col2)
		c.w.WriteString(`((`)
		colWithTable(c.w, sel.Table, rel.Col1)
		c.w.WriteString(`) = (`)
		colWithTableID(c.w, parent.Table, parent.ID, rel.Col2)
		c.w.WriteString(`))`)

	case RelOneToMany:
		//fmt.Fprintf(w, `(("%s"."%s") = ("%s_%d"."%s"))`,
		//c.sel.Table, rel.Col1, c.parent.Table, c.parent.ID, rel.Col2)
		c.w.WriteString(`((`)
		colWithTable(c.w, sel.Table, rel.Col1)
		c.w.WriteString(`) = (`)
		colWithTableID(c.w, parent.Table, parent.ID, rel.Col2)
		c.w.WriteString(`))`)

	case RelOneToManyThrough:
		//fmt.Fprintf(w, `(("%s"."%s") = ("%s"."%s"))`,
		//c.sel.Table, rel.Col1, rel.Through, rel.Col2)
		c.w.WriteString(`((`)
		colWithTable(c.w, sel.Table, rel.Col1)
		c.w.WriteString(`) = (`)
		colWithTable(c.w, rel.Through, rel.Col2)
		c.w.WriteString(`))`)
	}
}

func (c *compilerContext) renderWhere(sel *qcode.Select, ti *DBTableInfo) error {
	st := util.NewStack()

	if sel.Where != nil {
		st.Push(sel.Where)
	}

	for {
		if st.Len() == 0 {
			break
		}

		intf := st.Pop()

		switch val := intf.(type) {
		case qcode.ExpOp:
			switch val {
			case qcode.OpAnd:
				c.w.WriteString(` AND `)
			case qcode.OpOr:
				c.w.WriteString(` OR `)
			case qcode.OpNot:
				c.w.WriteString(`NOT `)
			default:
				return fmt.Errorf("11: unexpected value %v (%t)", intf, intf)
			}
		case *qcode.Exp:
			switch val.Op {
			case qcode.OpAnd, qcode.OpOr:
				for i := len(val.Children) - 1; i >= 0; i-- {
					st.Push(val.Children[i])
					if i > 0 {
						st.Push(val.Op)
					}
				}
				qcode.FreeExp(val)
				continue
			case qcode.OpNot:
				st.Push(val.Children[0])
				st.Push(qcode.OpNot)
				qcode.FreeExp(val)
				continue
			}

			if val.NestedCol {
				//fmt.Fprintf(w, `(("%s") `, val.Col)
				c.w.WriteString(`(("`)
				c.w.WriteString(val.Col)
				c.w.WriteString(`") `)

			} else if len(val.Col) != 0 {
				//fmt.Fprintf(w, `(("%s"."%s") `, c.sel.Table, val.Col)
				c.w.WriteString(`((`)
				colWithTable(c.w, sel.Table, val.Col)
				c.w.WriteString(`) `)
			}
			valExists := true

			switch val.Op {
			case qcode.OpEquals:
				c.w.WriteString(`=`)
			case qcode.OpNotEquals:
				c.w.WriteString(`!=`)
			case qcode.OpGreaterOrEquals:
				c.w.WriteString(`>=`)
			case qcode.OpLesserOrEquals:
				c.w.WriteString(`<=`)
			case qcode.OpGreaterThan:
				c.w.WriteString(`>`)
			case qcode.OpLesserThan:
				c.w.WriteString(`<`)
			case qcode.OpIn:
				c.w.WriteString(`IN`)
			case qcode.OpNotIn:
				c.w.WriteString(`NOT IN`)
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
			case qcode.OpIsNull:
				if strings.EqualFold(val.Val, "true") {
					c.w.WriteString(`IS NULL)`)
				} else {
					c.w.WriteString(`IS NOT NULL)`)
				}
				valExists = false
			case qcode.OpEqID:
				if len(ti.PrimaryCol) == 0 {
					return fmt.Errorf("no primary key column defined for %s", sel.Table)
				}
				//fmt.Fprintf(w, `(("%s") =`, c.ti.PrimaryCol)
				c.w.WriteString(`(("`)
				c.w.WriteString(ti.PrimaryCol)
				c.w.WriteString(`") =`)

			case qcode.OpTsQuery:
				if len(ti.TSVCol) == 0 {
					return fmt.Errorf("no tsv column defined for %s", sel.Table)
				}
				//fmt.Fprintf(w, `(("%s") @@ to_tsquery('%s'))`, c.ti.TSVCol, val.Val)
				c.w.WriteString(`(("`)
				c.w.WriteString(ti.TSVCol)
				c.w.WriteString(`") @@ to_tsquery('`)
				c.w.WriteString(val.Val)
				c.w.WriteString(`'))`)
				valExists = false

			default:
				return fmt.Errorf("[Where] unexpected op code %d", val.Op)
			}

			if valExists {
				if val.Type == qcode.ValList {
					c.renderList(val)
				} else {
					c.renderVal(val, c.vars)
				}
				c.w.WriteString(`)`)
			}

			qcode.FreeExp(val)

		default:
			return fmt.Errorf("12: unexpected value %v (%t)", intf, intf)
		}

	}

	return nil
}

func (c *compilerContext) renderOrderBy(sel *qcode.Select) error {
	c.w.WriteString(` ORDER BY `)
	for i := range sel.OrderBy {
		if i != 0 {
			c.w.WriteString(`, `)
		}
		ob := sel.OrderBy[i]

		switch ob.Order {
		case qcode.OrderAsc:
			//fmt.Fprintf(w, `"%s_%d.ob.%s" ASC`, sel.Table, sel.ID, ob.Col)
			tableIDColSuffix(c.w, sel.Table, sel.ID, ob.Col, "_ob")
			c.w.WriteString(` ASC`)
		case qcode.OrderDesc:
			//fmt.Fprintf(w, `"%s_%d.ob.%s" DESC`, sel.Table, sel.ID, ob.Col)
			tableIDColSuffix(c.w, sel.Table, sel.ID, ob.Col, "_ob")
			c.w.WriteString(` DESC`)
		case qcode.OrderAscNullsFirst:
			//fmt.Fprintf(w, `"%s_%d.ob.%s" ASC NULLS FIRST`, sel.Table, sel.ID, ob.Col)
			tableIDColSuffix(c.w, sel.Table, sel.ID, ob.Col, "_ob")
			c.w.WriteString(` ASC NULLS FIRST`)
		case qcode.OrderDescNullsFirst:
			//fmt.Fprintf(w, `%s_%d.ob.%s DESC NULLS FIRST`, sel.Table, sel.ID, ob.Col)
			tableIDColSuffix(c.w, sel.Table, sel.ID, ob.Col, "_ob")
			c.w.WriteString(` DESC NULLLS FIRST`)
		case qcode.OrderAscNullsLast:
			//fmt.Fprintf(w, `"%s_%d.ob.%s ASC NULLS LAST`, sel.Table, sel.ID, ob.Col)
			tableIDColSuffix(c.w, sel.Table, sel.ID, ob.Col, "_ob")
			c.w.WriteString(` ASC NULLS LAST`)
		case qcode.OrderDescNullsLast:
			//fmt.Fprintf(w, `%s_%d.ob.%s DESC NULLS LAST`, sel.Table, sel.ID, ob.Col)
			tableIDColSuffix(c.w, sel.Table, sel.ID, ob.Col, "_ob")
			c.w.WriteString(` DESC NULLS LAST`)
		default:
			return fmt.Errorf("13: unexpected value %v", ob.Order)
		}
	}
	return nil
}

func (c *compilerContext) renderDistinctOn(sel *qcode.Select) {
	io.WriteString(c.w, `DISTINCT ON (`)
	for i := range sel.DistinctOn {
		if i != 0 {
			c.w.WriteString(`, `)
		}
		//fmt.Fprintf(w, `"%s_%d.ob.%s"`, c.sel.Table, c.sel.ID, c.sel.DistinctOn[i])
		tableIDColSuffix(c.w, sel.Table, sel.ID, sel.DistinctOn[i], "_ob")
	}
	c.w.WriteString(`) `)
}

func (c *compilerContext) renderList(ex *qcode.Exp) {
	io.WriteString(c.w, ` (`)
	for i := range ex.ListVal {
		if i != 0 {
			c.w.WriteString(`, `)
		}
		switch ex.ListType {
		case qcode.ValBool, qcode.ValInt, qcode.ValFloat:
			c.w.WriteString(ex.ListVal[i])
		case qcode.ValStr:
			c.w.WriteString(`'`)
			c.w.WriteString(ex.ListVal[i])
			c.w.WriteString(`'`)
		}
	}
	c.w.WriteString(`)`)
}

func (c *compilerContext) renderVal(ex *qcode.Exp,
	vars map[string]string) {

	io.WriteString(c.w, ` (`)
	switch ex.Type {
	case qcode.ValBool, qcode.ValInt, qcode.ValFloat:
		if len(ex.Val) != 0 {
			c.w.WriteString(ex.Val)
		} else {
			c.w.WriteString(`''`)
		}
	case qcode.ValStr:
		c.w.WriteString(`'`)
		c.w.WriteString(ex.Val)
		c.w.WriteString(`'`)
	case qcode.ValVar:
		if val, ok := vars[ex.Val]; ok {
			c.w.WriteString(val)
		} else {
			//fmt.Fprintf(w, `'{{%s}}'`, ex.Val)
			c.w.WriteString(`'{{`)
			c.w.WriteString(ex.Val)
			c.w.WriteString(`}}'`)
		}
	}
	c.w.WriteString(`)`)
}

func funcPrefixLen(fn string) int {
	switch {
	case strings.HasPrefix(fn, "avg_"):
		return 4
	case strings.HasPrefix(fn, "count_"):
		return 6
	case strings.HasPrefix(fn, "max_"):
		return 4
	case strings.HasPrefix(fn, "min_"):
		return 4
	case strings.HasPrefix(fn, "sum_"):
		return 4
	case strings.HasPrefix(fn, "stddev_"):
		return 7
	case strings.HasPrefix(fn, "stddev_pop_"):
		return 11
	case strings.HasPrefix(fn, "stddev_samp_"):
		return 12
	case strings.HasPrefix(fn, "variance_"):
		return 9
	case strings.HasPrefix(fn, "var_pop_"):
		return 8
	case strings.HasPrefix(fn, "var_samp_"):
		return 9
	}
	return 0
}

func hasBit(n uint32, pos uint32) bool {
	val := n & (1 << pos)
	return (val > 0)
}

func alias(w *bytes.Buffer, alias string) {
	w.WriteString(` AS "`)
	w.WriteString(alias)
	w.WriteString(`"`)
}

func aliasWithID(w *bytes.Buffer, alias string, id int32) {
	w.WriteString(` AS "`)
	w.WriteString(alias)
	w.WriteString(`_`)
	int2string(w, id)
	w.WriteString(`"`)
}

func aliasWithIDSuffix(w *bytes.Buffer, alias string, id int32, suffix string) {
	w.WriteString(` AS "`)
	w.WriteString(alias)
	w.WriteString(`_`)
	int2string(w, id)
	w.WriteString(suffix)
	w.WriteString(`"`)
}

func colWithAlias(w *bytes.Buffer, col, alias string) {
	w.WriteString(`"`)
	w.WriteString(col)
	w.WriteString(`" AS "`)
	w.WriteString(alias)
	w.WriteString(`"`)
}

func tableWithAlias(w *bytes.Buffer, table, alias string) {
	w.WriteString(`"`)
	w.WriteString(table)
	w.WriteString(`" AS "`)
	w.WriteString(alias)
	w.WriteString(`"`)
}

func colWithTable(w *bytes.Buffer, table, col string) {
	w.WriteString(`"`)
	w.WriteString(table)
	w.WriteString(`"."`)
	w.WriteString(col)
	w.WriteString(`"`)
}

func colWithTableID(w *bytes.Buffer, table string, id int32, col string) {
	w.WriteString(`"`)
	w.WriteString(table)
	w.WriteString(`_`)
	int2string(w, id)
	w.WriteString(`"."`)
	w.WriteString(col)
	w.WriteString(`"`)
}

func colWithTableIDAlias(w *bytes.Buffer, table string, id int32, col, alias string) {
	w.WriteString(`"`)
	w.WriteString(table)
	w.WriteString(`_`)
	int2string(w, id)
	w.WriteString(`"."`)
	w.WriteString(col)
	w.WriteString(`" AS "`)
	w.WriteString(alias)
	w.WriteString(`"`)
}

func colWithTableIDSuffixAlias(w *bytes.Buffer, table string, id int32,
	suffix, col, alias string) {
	w.WriteString(`"`)
	w.WriteString(table)
	w.WriteString(`_`)
	int2string(w, id)
	w.WriteString(suffix)
	w.WriteString(`"."`)
	w.WriteString(col)
	w.WriteString(`" AS "`)
	w.WriteString(alias)
	w.WriteString(`"`)
}

func tableIDColSuffix(w *bytes.Buffer, table string, id int32, col, suffix string) {
	w.WriteString(`"`)
	w.WriteString(table)
	w.WriteString(`_`)
	int2string(w, id)
	w.WriteString(`_`)
	w.WriteString(col)
	w.WriteString(suffix)
	w.WriteString(`"`)
}

const charset = "0123456789"

func int2string(w *bytes.Buffer, val int32) {
	if val < 10 {
		w.WriteByte(charset[val])
		return
	}

	temp := int32(0)
	val2 := val
	for val2 > 0 {
		temp *= 10
		temp += val2 % 10
		val2 = int32(math.Floor(float64(val2 / 10)))
	}

	val3 := temp
	for val3 > 0 {
		d := val3 % 10
		val3 /= 10
		w.WriteByte(charset[d])
	}
}

func relID(h *xxhash.Digest, child, parent string) uint64 {
	h.WriteString(child)
	h.WriteString(parent)
	v := h.Sum64()
	h.Reset()
	return v
}
