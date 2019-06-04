package psql

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/dosco/super-graph/qcode"
	"github.com/dosco/super-graph/util"
)

const (
	empty = ""
)

type Config struct {
	Schema   *DBSchema
	Vars     map[string]string
	TableMap map[string]string
}

type Compiler struct {
	schema *DBSchema
	vars   map[string]string
	tmap   map[string]string
}

func NewCompiler(conf Config) *Compiler {
	return &Compiler{conf.Schema, conf.Vars, conf.TableMap}
}

func (c *Compiler) AddRelationship(key uint64, val *DBRel) {
	c.schema.RelMap[key] = val
}

func (c *Compiler) IDColumn(table string) string {
	t, ok := c.schema.Tables[table]
	if !ok {
		return empty
	}
	return t.PrimaryCol
}

func (c *Compiler) CompileEx(qc *qcode.QCode) (uint32, []byte, error) {
	w := &bytes.Buffer{}
	skipped, err := c.Compile(qc, w)
	return skipped, w.Bytes(), err
}

func (c *Compiler) Compile(qc *qcode.QCode, w *bytes.Buffer) (uint32, error) {
	if len(qc.Query.Selects) == 0 {
		return 0, errors.New("empty query")
	}
	root := &qc.Query.Selects[0]

	st := util.NewStack()
	ti, err := c.getTable(root)
	if err != nil {
		return 0, err
	}

	st.Push(&selectBlockClose{nil, root})
	st.Push(&selectBlock{nil, root, qc, ti, c})

	//fmt.Fprintf(w, `SELECT json_object_agg('%s', %s) FROM (`,
	//root.FieldName, root.Table)
	w.WriteString(`SELECT json_object_agg('`)
	w.WriteString(root.FieldName)
	w.WriteString(`', `)
	w.WriteString(root.Table)
	w.WriteString(`) FROM (`)

	var ignored uint32

	for {
		if st.Len() == 0 {
			break
		}

		intf := st.Pop()

		switch v := intf.(type) {
		case *selectBlock:
			skipped, err := v.render(w)
			if err != nil {
				return 0, err
			}
			ignored |= skipped

			for _, id := range v.sel.Children {
				if hasBit(skipped, uint16(id)) {
					continue
				}
				child := &qc.Query.Selects[id]

				ti, err := c.getTable(child)
				if err != nil {
					return 0, err
				}

				st.Push(&joinClose{child})
				st.Push(&selectBlockClose{v.sel, child})
				st.Push(&selectBlock{v.sel, child, qc, ti, c})
				st.Push(&joinOpen{child})
			}
		case *selectBlockClose:
			err = v.render(w)

		case *joinOpen:
			err = v.render(w)

		case *joinClose:
			err = v.render(w)
		}

		if err != nil {
			return 0, err
		}
	}

	w.WriteString(`)`)
	alias(w, `done_1337`)
	w.WriteString(`;`)

	return ignored, nil
}

func (c *Compiler) getTable(sel *qcode.Select) (*DBTableInfo, error) {
	if tn, ok := c.tmap[sel.Table]; ok {
		return c.schema.GetTable(tn)
	}

	return c.schema.GetTable(sel.Table)
}

func (v *selectBlock) processChildren() (uint32, []*qcode.Column) {
	var skipped uint32

	cols := make([]*qcode.Column, 0, len(v.sel.Cols))
	colmap := make(map[string]struct{}, len(v.sel.Cols))

	for i := range v.sel.Cols {
		colmap[v.sel.Cols[i].Name] = struct{}{}
	}

	for _, id := range v.sel.Children {
		child := &v.qc.Query.Selects[id]

		rel, ok := v.schema.RelMap[child.RelID]
		if !ok {
			skipped |= (1 << uint(id))
			continue
		}

		switch rel.Type {
		case RelOneToMany:
			fallthrough
		case RelBelongTo:
			if _, ok := colmap[rel.Col2]; !ok {
				cols = append(cols, &qcode.Column{v.sel.Table, rel.Col2, rel.Col2})
			}
		case RelOneToManyThrough:
			if _, ok := colmap[rel.Col1]; !ok {
				cols = append(cols, &qcode.Column{v.sel.Table, rel.Col1, rel.Col1})
			}
		case RelRemote:
			if _, ok := colmap[rel.Col1]; !ok {
				cols = append(cols, &qcode.Column{v.sel.Table, rel.Col1, rel.Col2})
			}
			skipped |= (1 << uint(id))

		default:
			skipped |= (1 << uint(id))
		}
	}

	return skipped, cols
}

type selectBlock struct {
	parent *qcode.Select
	sel    *qcode.Select
	qc     *qcode.QCode
	ti     *DBTableInfo
	*Compiler
}

func (v *selectBlock) render(w *bytes.Buffer) (uint32, error) {
	skipped, childCols := v.processChildren()
	hasOrder := len(v.sel.OrderBy) != 0

	// SELECT
	if v.sel.AsList {
		//fmt.Fprintf(w, `SELECT coalesce(json_agg("%s"`, v.sel.Table)
		w.WriteString(`SELECT coalesce(json_agg("`)
		w.WriteString(v.sel.Table)
		w.WriteString(`"`)

		if hasOrder {
			err := renderOrderBy(w, v.sel)
			if err != nil {
				return skipped, err
			}
		}

		//fmt.Fprintf(w, `), '[]') AS "%s" FROM (`, v.sel.Table)
		w.WriteString(`), '[]')`)
		alias(w, v.sel.Table)
		w.WriteString(` FROM (`)
	}

	// ROW-TO-JSON
	w.WriteString(`SELECT `)

	if len(v.sel.DistinctOn) != 0 {
		v.renderDistinctOn(w)
	}

	w.WriteString(`row_to_json((`)

	//fmt.Fprintf(w, `SELECT "sel_%d" FROM (SELECT `, v.sel.ID)
	w.WriteString(`SELECT "sel_`)
	int2string(w, v.sel.ID)
	w.WriteString(`" FROM (SELECT `)

	// Combined column names
	v.renderColumns(w)

	v.renderRemoteRelColumns(w)

	err := v.renderJoinedColumns(w, skipped)
	if err != nil {
		return skipped, err
	}

	//fmt.Fprintf(w, `) AS "sel_%d"`, v.sel.ID)
	w.WriteString(`)`)
	aliasWithID(w, "sel", v.sel.ID)

	//fmt.Fprintf(w, `)) AS "%s"`, v.sel.Table)
	w.WriteString(`))`)
	alias(w, v.sel.Table)
	// END-ROW-TO-JSON

	if hasOrder {
		v.renderOrderByColumns(w)
	}
	// END-SELECT

	// FROM (SELECT .... )
	err = v.renderBaseSelect(w, childCols, skipped)
	if err != nil {
		return skipped, err
	}
	// END-FROM

	return skipped, nil
}

type selectBlockClose struct {
	parent *qcode.Select
	sel    *qcode.Select
}

func (v *selectBlockClose) render(w *bytes.Buffer) error {
	hasOrder := len(v.sel.OrderBy) != 0

	if hasOrder {
		err := renderOrderBy(w, v.sel)
		if err != nil {
			return err
		}
	}

	if len(v.sel.Paging.Limit) != 0 {
		//fmt.Fprintf(w, ` LIMIT ('%s') :: integer`, v.sel.Paging.Limit)
		w.WriteString(` LIMIT ('`)
		w.WriteString(v.sel.Paging.Limit)
		w.WriteString(`') :: integer`)
	} else {
		w.WriteString(` LIMIT ('20') :: integer`)
	}

	if len(v.sel.Paging.Offset) != 0 {
		//fmt.Fprintf(w, ` OFFSET ('%s') :: integer`, v.sel.Paging.Offset)
		w.WriteString(`OFFSET ('`)
		w.WriteString(v.sel.Paging.Offset)
		w.WriteString(`') :: integer`)
	}

	if v.sel.AsList {
		//fmt.Fprintf(w, `) AS "%s_%d"`, v.sel.Table, v.sel.ID)
		w.WriteString(`)`)
		aliasWithID(w, v.sel.Table, v.sel.ID)
	}

	return nil
}

type joinOpen struct {
	sel *qcode.Select
}

func (v joinOpen) render(w *bytes.Buffer) error {
	w.WriteString(` LEFT OUTER JOIN LATERAL (`)
	return nil
}

type joinClose struct {
	sel *qcode.Select
}

func (v *joinClose) render(w *bytes.Buffer) error {
	//fmt.Fprintf(w, `) AS "%s_%d_join" ON ('true')`, v.sel.Table, v.sel.ID)
	w.WriteString(`)`)
	aliasWithIDSuffix(w, v.sel.Table, v.sel.ID, "_join")
	w.WriteString(` ON ('true')`)
	return nil
}

func (v *selectBlock) renderJoinTable(w *bytes.Buffer) {
	rel, ok := v.schema.RelMap[v.sel.RelID]
	if !ok {
		panic(errors.New("no relationship found"))
	}

	if rel.Type != RelOneToManyThrough {
		return
	}

	//fmt.Fprintf(w, ` LEFT OUTER JOIN "%s" ON (("%s"."%s") = ("%s_%d"."%s"))`,
	//rel.Through, rel.Through, rel.ColT, v.parent.Table, v.parent.ID, rel.Col1)
	w.WriteString(` LEFT OUTER JOIN "`)
	w.WriteString(rel.Through)
	w.WriteString(`" ON ((`)
	colWithTable(w, rel.Through, rel.ColT)
	w.WriteString(`) = (`)
	colWithTableID(w, v.parent.Table, v.parent.ID, rel.Col1)
	w.WriteString(`))`)
}

func (v *selectBlock) renderColumns(w *bytes.Buffer) {
	for i, col := range v.sel.Cols {
		if i != 0 {
			io.WriteString(w, ", ")
		}
		//fmt.Fprintf(w, `"%s_%d"."%s" AS "%s"`,
		//v.sel.Table, v.sel.ID, col.Name, col.FieldName)
		colWithTableIDAlias(w, v.sel.Table, v.sel.ID, col.Name, col.FieldName)
	}
}

func (v *selectBlock) renderRemoteRelColumns(w *bytes.Buffer) {
	i := 0

	for _, id := range v.sel.Children {
		child := &v.qc.Query.Selects[id]

		rel, ok := v.schema.RelMap[child.RelID]
		if !ok || rel.Type != RelRemote {
			continue
		}
		if i != 0 || len(v.sel.Cols) != 0 {
			io.WriteString(w, ", ")
		}
		//fmt.Fprintf(w, `"%s_%d"."%s" AS "%s"`,
		//v.sel.Table, v.sel.ID, rel.Col1, rel.Col2)
		colWithTableID(w, v.sel.Table, v.sel.ID, rel.Col1)
		alias(w, rel.Col2)
		i++
	}
}

func (v *selectBlock) renderJoinedColumns(w *bytes.Buffer, skipped uint32) error {
	colsRendered := len(v.sel.Cols) != 0

	for _, id := range v.sel.Children {
		skipThis := hasBit(skipped, uint16(id))

		if colsRendered && !skipThis {
			io.WriteString(w, ", ")
		}
		if skipThis {
			continue
		}
		s := &v.qc.Query.Selects[id]

		//fmt.Fprintf(w, `"%s_%d_join"."%s" AS "%s"`,
		//s.Table, s.ID, s.Table, s.FieldName)
		colWithTableIDSuffixAlias(w, s.Table, s.ID, "_join", s.Table, s.FieldName)
	}

	return nil
}

func (v *selectBlock) renderBaseSelect(w *bytes.Buffer, childCols []*qcode.Column, skipped uint32) error {
	var groupBy []int

	isRoot := v.parent == nil
	isFil := v.sel.Where != nil
	isSearch := v.sel.Args["search"] != nil
	isAgg := false

	w.WriteString(` FROM (SELECT `)

	for i, col := range v.sel.Cols {
		cn := col.Name

		_, isRealCol := v.ti.Columns[cn]

		if !isRealCol {
			if isSearch {
				switch {
				case cn == "search_rank":
					cn = v.ti.TSVCol
					arg := v.sel.Args["search"]

					//fmt.Fprintf(w, `ts_rank("%s"."%s", to_tsquery('%s')) AS %s`,
					//v.sel.Table, cn, arg.Val, col.Name)
					w.WriteString(`ts_rank(`)
					colWithTable(w, v.sel.Table, cn)
					w.WriteString(`, to_tsquery('`)
					w.WriteString(arg.Val)
					w.WriteString(`')`)
					alias(w, col.Name)

				case strings.HasPrefix(cn, "search_headline_"):
					cn = cn[16:]
					arg := v.sel.Args["search"]

					//fmt.Fprintf(w, `ts_headline("%s"."%s", to_tsquery('%s')) AS %s`,
					//v.sel.Table, cn, arg.Val, col.Name)
					w.WriteString(`ts_headlinek(`)
					colWithTable(w, v.sel.Table, cn)
					w.WriteString(`, to_tsquery('`)
					w.WriteString(arg.Val)
					w.WriteString(`')`)
					alias(w, col.Name)
				}
			} else {
				pl := funcPrefixLen(cn)
				if pl == 0 {
					//fmt.Fprintf(w, `'%s not defined' AS %s`, cn, col.Name)
					w.WriteString(`'`)
					w.WriteString(cn)
					w.WriteString(` not defined'`)
					alias(w, col.Name)
				} else {
					isAgg = true
					fn := cn[0 : pl-1]
					cn := cn[pl:]
					//fmt.Fprintf(w, `%s("%s"."%s") AS %s`, fn, v.sel.Table, cn, col.Name)
					w.WriteString(fn)
					w.WriteString(`(`)
					colWithTable(w, v.sel.Table, cn)
					w.WriteString(`)`)
					alias(w, col.Name)
				}
			}
		} else {
			groupBy = append(groupBy, i)
			//fmt.Fprintf(w, `"%s"."%s"`, v.sel.Table, cn)
			colWithTable(w, v.sel.Table, cn)
		}

		if i < len(v.sel.Cols)-1 || len(childCols) != 0 {
			//io.WriteString(w, ", ")
			w.WriteString(`, `)
		}
	}

	for i, col := range childCols {
		if i != 0 {
			//io.WriteString(w, ", ")
			w.WriteString(`, `)
		}

		//fmt.Fprintf(w, `"%s"."%s"`, col.Table, col.Name)
		colWithTable(w, col.Table, col.Name)
	}

	w.WriteString(` FROM `)
	if tn, ok := v.tmap[v.sel.Table]; ok {
		//fmt.Fprintf(w, ` FROM "%s" AS "%s"`, tn, v.sel.Table)
		colWithAlias(w, tn, v.sel.Table)
	} else {
		//fmt.Fprintf(w, ` FROM "%s"`, v.sel.Table)
		w.WriteString(`"`)
		w.WriteString(v.sel.Table)
		w.WriteString(`"`)
	}

	if isRoot && isFil {
		w.WriteString(` WHERE (`)
		if err := v.renderWhere(w); err != nil {
			return err
		}
		w.WriteString(`)`)
	}

	if !isRoot {
		v.renderJoinTable(w)

		w.WriteString(` WHERE (`)
		v.renderRelationship(w)

		if isFil {
			w.WriteString(` AND `)
			if err := v.renderWhere(w); err != nil {
				return err
			}
		}
		w.WriteString(`)`)
	}

	if isAgg {
		if len(groupBy) != 0 {
			w.WriteString(` GROUP BY `)

			for i, id := range groupBy {
				if i != 0 {
					w.WriteString(`, `)
				}
				//fmt.Fprintf(w, `"%s"."%s"`, v.sel.Table, v.sel.Cols[id].Name)
				colWithTable(w, v.sel.Table, v.sel.Cols[id].Name)
			}
		}
	}

	if len(v.sel.Paging.Limit) != 0 {
		//fmt.Fprintf(w, ` LIMIT ('%s') :: integer`, v.sel.Paging.Limit)
		w.WriteString(` LIMIT ('`)
		w.WriteString(v.sel.Paging.Limit)
		w.WriteString(`') :: integer`)
	} else {
		w.WriteString(` LIMIT ('20') :: integer`)
	}

	if len(v.sel.Paging.Offset) != 0 {
		//fmt.Fprintf(w, ` OFFSET ('%s') :: integer`, v.sel.Paging.Offset)
		w.WriteString(` OFFSET ('`)
		w.WriteString(v.sel.Paging.Offset)
		w.WriteString(`') :: integer`)
	}

	//fmt.Fprintf(w, `) AS "%s_%d"`, v.sel.Table, v.sel.ID)
	w.WriteString(`)`)
	aliasWithID(w, v.sel.Table, v.sel.ID)
	return nil
}

func (v *selectBlock) renderOrderByColumns(w *bytes.Buffer) {
	colsRendered := len(v.sel.Cols) != 0

	for i := range v.sel.OrderBy {
		if colsRendered {
			//io.WriteString(w, ", ")
			w.WriteString(`, `)
		}

		c := v.sel.OrderBy[i].Col
		//fmt.Fprintf(w, `"%s_%d"."%s" AS "%s_%d_%s_ob"`,
		//v.sel.Table, v.sel.ID, c,
		//v.sel.Table, v.sel.ID, c)
		colWithTableID(w, v.sel.Table, v.sel.ID, c)
		w.WriteString(` AS `)
		tableIDColSuffix(w, v.sel.Table, v.sel.ID, c, "_ob")
	}
}

func (v *selectBlock) renderRelationship(w *bytes.Buffer) {
	rel, ok := v.schema.RelMap[v.sel.RelID]
	if !ok {
		panic(errors.New("no relationship found"))
	}

	switch rel.Type {
	case RelBelongTo:
		//fmt.Fprintf(w, `(("%s"."%s") = ("%s_%d"."%s"))`,
		//v.sel.Table, rel.Col1, v.parent.Table, v.parent.ID, rel.Col2)
		w.WriteString(`((`)
		colWithTable(w, v.sel.Table, rel.Col1)
		w.WriteString(`) = (`)
		colWithTableID(w, v.parent.Table, v.parent.ID, rel.Col2)
		w.WriteString(`))`)

	case RelOneToMany:
		//fmt.Fprintf(w, `(("%s"."%s") = ("%s_%d"."%s"))`,
		//v.sel.Table, rel.Col1, v.parent.Table, v.parent.ID, rel.Col2)
		w.WriteString(`((`)
		colWithTable(w, v.sel.Table, rel.Col1)
		w.WriteString(`) = (`)
		colWithTableID(w, v.parent.Table, v.parent.ID, rel.Col2)
		w.WriteString(`))`)

	case RelOneToManyThrough:
		//fmt.Fprintf(w, `(("%s"."%s") = ("%s"."%s"))`,
		//v.sel.Table, rel.Col1, rel.Through, rel.Col2)
		w.WriteString(`((`)
		colWithTable(w, v.sel.Table, rel.Col1)
		w.WriteString(`) = (`)
		colWithTable(w, rel.Through, rel.Col2)
		w.WriteString(`))`)
	}
}

func (v *selectBlock) renderWhere(w *bytes.Buffer) error {
	st := util.NewStack()

	if v.sel.Where != nil {
		st.Push(v.sel.Where)
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
				w.WriteString(` AND `)
			case qcode.OpOr:
				w.WriteString(` OR `)
			case qcode.OpNot:
				w.WriteString(`NOT `)
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
				continue
			case qcode.OpNot:
				st.Push(val.Children[0])
				st.Push(qcode.OpNot)
				continue
			}

			if val.NestedCol {
				//fmt.Fprintf(w, `(("%s") `, val.Col)
				w.WriteString(`(("`)
				w.WriteString(val.Col)
				w.WriteString(`") `)
			} else if len(val.Col) != 0 {
				//fmt.Fprintf(w, `(("%s"."%s") `, v.sel.Table, val.Col)
				w.WriteString(`((`)
				colWithTable(w, v.sel.Table, val.Col)
				w.WriteString(`) `)
			}
			valExists := true

			switch val.Op {
			case qcode.OpEquals:
				w.WriteString(`=`)
			case qcode.OpNotEquals:
				w.WriteString(`!=`)
			case qcode.OpGreaterOrEquals:
				w.WriteString(`>=`)
			case qcode.OpLesserOrEquals:
				w.WriteString(`<=`)
			case qcode.OpGreaterThan:
				w.WriteString(`>`)
			case qcode.OpLesserThan:
				w.WriteString(`<`)
			case qcode.OpIn:
				w.WriteString(`IN`)
			case qcode.OpNotIn:
				w.WriteString(`NOT IN`)
			case qcode.OpLike:
				w.WriteString(`LIKE`)
			case qcode.OpNotLike:
				w.WriteString(`NOT LIKE`)
			case qcode.OpILike:
				w.WriteString(`ILIKE`)
			case qcode.OpNotILike:
				w.WriteString(`NOT ILIKE`)
			case qcode.OpSimilar:
				w.WriteString(`SIMILAR TO`)
			case qcode.OpNotSimilar:
				w.WriteString(`NOT SIMILAR TO`)
			case qcode.OpContains:
				w.WriteString(`@>`)
			case qcode.OpContainedIn:
				w.WriteString(`<@`)
			case qcode.OpHasKey:
				w.WriteString(`?`)
			case qcode.OpHasKeyAny:
				w.WriteString(`?|`)
			case qcode.OpHasKeyAll:
				w.WriteString(`?&`)
			case qcode.OpIsNull:
				if strings.EqualFold(val.Val, "true") {
					w.WriteString(`IS NULL)`)
				} else {
					w.WriteString(`IS NOT NULL)`)
				}
				valExists = false
			case qcode.OpEqID:
				if len(v.ti.PrimaryCol) == 0 {
					return fmt.Errorf("no primary key column defined for %s", v.sel.Table)
				}
				//fmt.Fprintf(w, `(("%s") =`, v.ti.PrimaryCol)
				w.WriteString(`(("`)
				w.WriteString(v.ti.PrimaryCol)
				w.WriteString(`") =`)

			case qcode.OpTsQuery:
				if len(v.ti.TSVCol) == 0 {
					return fmt.Errorf("no tsv column defined for %s", v.sel.Table)
				}
				//fmt.Fprintf(w, `(("%s") @@ to_tsquery('%s'))`, v.ti.TSVCol, val.Val)
				w.WriteString(`(("`)
				w.WriteString(v.ti.TSVCol)
				w.WriteString(`") @@ to_tsquery('`)
				w.WriteString(val.Val)
				w.WriteString(`'))`)
				valExists = false

			default:
				return fmt.Errorf("[Where] unexpected op code %d", val.Op)
			}

			if valExists {
				if val.Type == qcode.ValList {
					renderList(w, val)
				} else {
					renderVal(w, val, v.vars)
				}
				w.WriteString(`)`)
			}

		default:
			return fmt.Errorf("12: unexpected value %v (%t)", intf, intf)
		}
	}

	return nil
}

func renderOrderBy(w *bytes.Buffer, sel *qcode.Select) error {
	w.WriteString(` ORDER BY `)
	for i := range sel.OrderBy {
		if i != 0 {
			w.WriteString(`, `)
		}
		ob := sel.OrderBy[i]

		switch ob.Order {
		case qcode.OrderAsc:
			//fmt.Fprintf(w, `"%s_%d.ob.%s" ASC`, sel.Table, sel.ID, ob.Col)
			tableIDColSuffix(w, sel.Table, sel.ID, ob.Col, "_ob")
			w.WriteString(` ASC`)
		case qcode.OrderDesc:
			//fmt.Fprintf(w, `"%s_%d.ob.%s" DESC`, sel.Table, sel.ID, ob.Col)
			tableIDColSuffix(w, sel.Table, sel.ID, ob.Col, "_ob")
			w.WriteString(` DESC`)
		case qcode.OrderAscNullsFirst:
			//fmt.Fprintf(w, `"%s_%d.ob.%s" ASC NULLS FIRST`, sel.Table, sel.ID, ob.Col)
			tableIDColSuffix(w, sel.Table, sel.ID, ob.Col, "_ob")
			w.WriteString(` ASC NULLS FIRST`)
		case qcode.OrderDescNullsFirst:
			//fmt.Fprintf(w, `%s_%d.ob.%s DESC NULLS FIRST`, sel.Table, sel.ID, ob.Col)
			tableIDColSuffix(w, sel.Table, sel.ID, ob.Col, "_ob")
			w.WriteString(` DESC NULLLS FIRST`)
		case qcode.OrderAscNullsLast:
			//fmt.Fprintf(w, `"%s_%d.ob.%s ASC NULLS LAST`, sel.Table, sel.ID, ob.Col)
			tableIDColSuffix(w, sel.Table, sel.ID, ob.Col, "_ob")
			w.WriteString(` ASC NULLS LAST`)
		case qcode.OrderDescNullsLast:
			//fmt.Fprintf(w, `%s_%d.ob.%s DESC NULLS LAST`, sel.Table, sel.ID, ob.Col)
			tableIDColSuffix(w, sel.Table, sel.ID, ob.Col, "_ob")
			w.WriteString(` DESC NULLS LAST`)
		default:
			return fmt.Errorf("13: unexpected value %v", ob.Order)
		}
	}
	return nil
}

func (v selectBlock) renderDistinctOn(w *bytes.Buffer) {
	io.WriteString(w, `DISTINCT ON (`)
	for i := range v.sel.DistinctOn {
		if i != 0 {
			w.WriteString(`, `)
		}
		//fmt.Fprintf(w, `"%s_%d.ob.%s"`, v.sel.Table, v.sel.ID, v.sel.DistinctOn[i])
		tableIDColSuffix(w, v.sel.Table, v.sel.ID, v.sel.DistinctOn[i], "_ob")
	}
	w.WriteString(`) `)
}

func renderList(w *bytes.Buffer, ex *qcode.Exp) {
	io.WriteString(w, ` (`)
	for i := range ex.ListVal {
		if i != 0 {
			w.WriteString(`, `)
		}
		switch ex.ListType {
		case qcode.ValBool, qcode.ValInt, qcode.ValFloat:
			w.WriteString(ex.ListVal[i])
		case qcode.ValStr:
			w.WriteString(`'`)
			w.WriteString(ex.ListVal[i])
			w.WriteString(`'`)
		}
	}
	w.WriteString(`)`)
}

func renderVal(w *bytes.Buffer, ex *qcode.Exp, vars map[string]string) {
	io.WriteString(w, ` (`)
	switch ex.Type {
	case qcode.ValBool, qcode.ValInt, qcode.ValFloat:
		if len(ex.Val) != 0 {
			w.WriteString(ex.Val)
		} else {
			w.WriteString(`''`)
		}
	case qcode.ValStr:
		w.WriteString(`'`)
		w.WriteString(ex.Val)
		w.WriteString(`'`)
	case qcode.ValVar:
		if val, ok := vars[ex.Val]; ok {
			w.WriteString(val)
		} else {
			//fmt.Fprintf(w, `'{{%s}}'`, ex.Val)
			w.WriteString(`'{{`)
			w.WriteString(ex.Val)
			w.WriteString(`}}'`)
		}
	}
	w.WriteString(`)`)
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

func hasBit(n uint32, pos uint16) bool {
	val := n & (1 << pos)
	return (val > 0)
}

func alias(w *bytes.Buffer, alias string) {
	w.WriteString(` AS "`)
	w.WriteString(alias)
	w.WriteString(`"`)
}

func aliasWithID(w *bytes.Buffer, alias string, id int16) {
	w.WriteString(` AS "`)
	w.WriteString(alias)
	w.WriteString(`_`)
	int2string(w, id)
	w.WriteString(`"`)
}

func aliasWithIDSuffix(w *bytes.Buffer, alias string, id int16, suffix string) {
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

func colWithTable(w *bytes.Buffer, table, col string) {
	w.WriteString(`"`)
	w.WriteString(table)
	w.WriteString(`"."`)
	w.WriteString(col)
	w.WriteString(`"`)
}

func colWithTableID(w *bytes.Buffer, table string, id int16, col string) {
	w.WriteString(`"`)
	w.WriteString(table)
	w.WriteString(`_`)
	int2string(w, id)
	w.WriteString(`"."`)
	w.WriteString(col)
	w.WriteString(`"`)
}

func colWithTableIDAlias(w *bytes.Buffer, table string, id int16, col, alias string) {
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

func colWithTableIDSuffixAlias(w *bytes.Buffer, table string, id int16,
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

func tableIDColSuffix(w *bytes.Buffer, table string, id int16, col, suffix string) {
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

func int2string(w *bytes.Buffer, val int16) {
	if val < 10 {
		w.WriteByte(charset[val])
		return
	}

	temp := int16(0)
	val2 := val
	for val2 > 0 {
		temp *= 10
		temp += val2 % 10
		val2 = int16(math.Floor(float64(val2 / 10)))
	}

	val3 := temp
	for val3 > 0 {
		d := val3 % 10
		val3 /= 10
		w.WriteByte(charset[d])
	}
}
