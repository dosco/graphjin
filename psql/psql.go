package psql

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/dosco/super-graph/qcode"
	"github.com/dosco/super-graph/util"
)

const (
	empty = ""
	opKey = 5000
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

func (c *Compiler) AddRelationship(key TTKey, val *DBRel) {
	c.schema.RelMap[key] = val
}

func (c *Compiler) IDColumn(table string) string {
	t, ok := c.schema.Tables[table]
	if !ok {
		return empty
	}
	return t.PrimaryCol
}

func (c *Compiler) Compile(qc *qcode.QCode) (uint32, []string, error) {
	if len(qc.Query.Selects) == 0 {
		return 0, nil, errors.New("empty query")
	}
	root := &qc.Query.Selects[0]

	st := util.NewStack()
	ti, err := c.getTable(root)
	if err != nil {
		return 0, nil, err
	}

	buf := strings.Builder{}
	buf.Grow(2048)

	sql := make([]string, 0, 3)
	w := io.Writer(&buf)

	st.Push(&selectBlockClose{nil, root})
	st.Push(&selectBlock{nil, root, qc, ti, c})

	fmt.Fprintf(w, `SELECT json_object_agg('%s', %s) FROM (`,
		root.FieldName, root.Table)

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
				return 0, nil, err
			}
			ignored |= skipped

			for _, id := range v.sel.Children {
				if hasBit(skipped, id) {
					continue
				}
				child := &qc.Query.Selects[id]

				ti, err := c.getTable(child)
				if err != nil {
					return 0, nil, err
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
			return 0, nil, err
		}
	}

	io.WriteString(w, `) AS "done_1337";`)
	sql = append(sql, buf.String())

	return ignored, sql, nil
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
		k := TTKey{child.Table, v.sel.Table}

		rel, ok := v.schema.RelMap[k]
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

func (v *selectBlock) render(w io.Writer) (uint32, error) {
	skipped, childCols := v.processChildren()
	hasOrder := len(v.sel.OrderBy) != 0

	// SELECT
	if v.sel.AsList {
		fmt.Fprintf(w, `SELECT coalesce(json_agg("%s"`, v.sel.Table)

		if hasOrder {
			err := renderOrderBy(w, v.sel)
			if err != nil {
				return skipped, err
			}
		}

		fmt.Fprintf(w, `), '[]') AS "%s" FROM (`, v.sel.Table)
	}

	// ROW-TO-JSON
	io.WriteString(w, `SELECT `)

	if len(v.sel.DistinctOn) != 0 {
		v.renderDistinctOn(w)
	}

	io.WriteString(w, `row_to_json((`)

	fmt.Fprintf(w, `SELECT "sel_%d" FROM (SELECT `, v.sel.ID)

	// Combined column names
	v.renderColumns(w)

	v.renderRemoteRelColumns(w)

	err := v.renderJoinedColumns(w, skipped)
	if err != nil {
		return skipped, err
	}

	fmt.Fprintf(w, `) AS "sel_%d"`, v.sel.ID)

	fmt.Fprintf(w, `)) AS "%s"`, v.sel.Table)
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

func (v *selectBlockClose) render(w io.Writer) error {
	hasOrder := len(v.sel.OrderBy) != 0

	if hasOrder {
		err := renderOrderBy(w, v.sel)
		if err != nil {
			return err
		}
	}

	if len(v.sel.Paging.Limit) != 0 {
		fmt.Fprintf(w, ` LIMIT ('%s') :: integer`, v.sel.Paging.Limit)
	} else {
		io.WriteString(w, ` LIMIT ('20') :: integer`)
	}

	if len(v.sel.Paging.Offset) != 0 {
		fmt.Fprintf(w, ` OFFSET ('%s') :: integer`, v.sel.Paging.Offset)
	}

	if v.sel.AsList {
		fmt.Fprintf(w, `) AS "%s_%d"`, v.sel.Table, v.sel.ID)
	}

	return nil
}

type joinOpen struct {
	sel *qcode.Select
}

func (v joinOpen) render(w io.Writer) error {
	io.WriteString(w, ` LEFT OUTER JOIN LATERAL (`)
	return nil
}

type joinClose struct {
	sel *qcode.Select
}

func (v *joinClose) render(w io.Writer) error {
	fmt.Fprintf(w, `) AS "%s_%d_join" ON ('true')`, v.sel.Table, v.sel.ID)
	return nil
}

func (v *selectBlock) renderJoinTable(w io.Writer) {
	k := TTKey{v.sel.Table, v.parent.Table}
	rel, ok := v.schema.RelMap[k]
	if !ok {
		panic(errors.New("no relationship found"))
	}

	if rel.Type != RelOneToManyThrough {
		return
	}

	fmt.Fprintf(w, ` LEFT OUTER JOIN "%s" ON (("%s"."%s") = ("%s_%d"."%s"))`,
		rel.Through, rel.Through, rel.ColT, v.parent.Table, v.parent.ID, rel.Col1)
}

func (v *selectBlock) renderColumns(w io.Writer) {
	for i, col := range v.sel.Cols {
		if i != 0 {
			io.WriteString(w, ", ")
		}
		fmt.Fprintf(w, `"%s_%d"."%s" AS "%s"`,
			v.sel.Table, v.sel.ID, col.Name, col.FieldName)
	}
}

func (v *selectBlock) renderRemoteRelColumns(w io.Writer) {
	k := TTKey{Table2: v.sel.Table}
	i := 0

	for _, id := range v.sel.Children {
		child := &v.qc.Query.Selects[id]
		k.Table1 = child.Table

		rel, ok := v.schema.RelMap[k]
		if !ok || rel.Type != RelRemote {
			continue
		}
		if i != 0 || len(v.sel.Cols) != 0 {
			io.WriteString(w, ", ")
		}
		fmt.Fprintf(w, `"%s_%d"."%s" AS "%s"`,
			v.sel.Table, v.sel.ID, rel.Col1, rel.Col2)
		i++
	}
}

func (v *selectBlock) renderJoinedColumns(w io.Writer, skipped uint32) error {
	colsRendered := len(v.sel.Cols) != 0

	for _, id := range v.sel.Children {
		skipThis := hasBit(skipped, id)

		if colsRendered && !skipThis {
			io.WriteString(w, ", ")
		}
		if skipThis {
			continue
		}
		s := &v.qc.Query.Selects[id]

		fmt.Fprintf(w, `"%s_%d_join"."%s" AS "%s"`,
			s.Table, s.ID, s.Table, s.FieldName)
	}

	return nil
}

func (v *selectBlock) renderBaseSelect(w io.Writer, childCols []*qcode.Column, skipped uint32) error {
	var groupBy []int

	isRoot := v.parent == nil
	isFil := v.sel.Where != nil
	isSearch := len(v.sel.Args[qcode.ArgSearch]) != 0
	isAgg := false

	io.WriteString(w, " FROM (SELECT ")

	for i, col := range v.sel.Cols {
		cn := col.Name

		_, isRealCol := v.ti.Columns[cn]

		if !isRealCol {
			if isSearch {
				switch {
				case cn == "search_rank":
					cn = v.ti.TSVCol
					arg := v.sel.Args[qcode.ArgSearch]

					fmt.Fprintf(w, `ts_rank("%s"."%s", to_tsquery('%s')) AS %s`,
						v.sel.Table, cn, arg[0].Val, col.Name)

				case strings.HasPrefix(cn, "search_headline_"):
					cn = cn[16:]
					arg := v.sel.Args[qcode.ArgSearch]

					fmt.Fprintf(w, `ts_headline("%s"."%s", to_tsquery('%s')) AS %s`,
						v.sel.Table, cn, arg[0].Val, col.Name)
				}
			} else {
				pl := funcPrefixLen(cn)
				if pl == 0 {
					fmt.Fprintf(w, `'%s not defined' AS %s`, cn, col.Name)
				} else {
					isAgg = true
					fn := cn[0 : pl-1]
					cn := cn[pl:]
					fmt.Fprintf(w, `%s("%s"."%s") AS %s`, fn, v.sel.Table, cn, col.Name)
				}
			}
		} else {
			groupBy = append(groupBy, i)
			fmt.Fprintf(w, `"%s"."%s"`, v.sel.Table, cn)
		}

		if i < len(v.sel.Cols)-1 || len(childCols) != 0 {
			io.WriteString(w, ", ")
		}
	}

	for i, col := range childCols {
		if i != 0 {
			io.WriteString(w, ", ")
		}

		fmt.Fprintf(w, `"%s"."%s"`, col.Table, col.Name)
	}

	if tn, ok := v.tmap[v.sel.Table]; ok {
		fmt.Fprintf(w, ` FROM "%s" AS "%s"`, tn, v.sel.Table)
	} else {
		fmt.Fprintf(w, ` FROM "%s"`, v.sel.Table)
	}

	if isRoot && isFil {
		io.WriteString(w, ` WHERE (`)
		if err := v.renderWhere(w); err != nil {
			return err
		}
		io.WriteString(w, `)`)
	}

	if !isRoot {
		v.renderJoinTable(w)

		io.WriteString(w, ` WHERE (`)
		v.renderRelationship(w)

		if isFil {
			io.WriteString(w, ` AND `)
			if err := v.renderWhere(w); err != nil {
				return err
			}
		}
		io.WriteString(w, `)`)
	}

	if isAgg {
		if len(groupBy) != 0 {
			fmt.Fprintf(w, ` GROUP BY `)

			for i, id := range groupBy {
				if i != 0 {
					io.WriteString(w, ", ")
				}
				fmt.Fprintf(w, `"%s"."%s"`, v.sel.Table, v.sel.Cols[id].Name)
			}
		}
	}

	if len(v.sel.Paging.Limit) != 0 {
		fmt.Fprintf(w, ` LIMIT ('%s') :: integer`, v.sel.Paging.Limit)
	} else {
		io.WriteString(w, ` LIMIT ('20') :: integer`)
	}

	if len(v.sel.Paging.Offset) != 0 {
		fmt.Fprintf(w, ` OFFSET ('%s') :: integer`, v.sel.Paging.Offset)
	}

	fmt.Fprintf(w, `) AS "%s_%d"`, v.sel.Table, v.sel.ID)
	return nil
}

func (v *selectBlock) renderOrderByColumns(w io.Writer) {
	colsRendered := len(v.sel.Cols) != 0

	for i := range v.sel.OrderBy {
		if colsRendered {
			io.WriteString(w, ", ")
		}

		c := v.sel.OrderBy[i].Col
		fmt.Fprintf(w, `"%s_%d"."%s" AS "%s_%d.ob.%s"`,
			v.sel.Table, v.sel.ID, c,
			v.sel.Table, v.sel.ID, c)
	}
}

func (v *selectBlock) renderRelationship(w io.Writer) {
	k := TTKey{v.sel.Table, v.parent.Table}
	rel, ok := v.schema.RelMap[k]
	if !ok {
		panic(errors.New("no relationship found"))
	}

	switch rel.Type {
	case RelBelongTo:
		fmt.Fprintf(w, `(("%s"."%s") = ("%s_%d"."%s"))`,
			v.sel.Table, rel.Col1, v.parent.Table, v.parent.ID, rel.Col2)

	case RelOneToMany:
		fmt.Fprintf(w, `(("%s"."%s") = ("%s_%d"."%s"))`,
			v.sel.Table, rel.Col1, v.parent.Table, v.parent.ID, rel.Col2)

	case RelOneToManyThrough:
		fmt.Fprintf(w, `(("%s"."%s") = ("%s"."%s"))`,
			v.sel.Table, rel.Col1, rel.Through, rel.Col2)
	}
}

func (v *selectBlock) renderWhere(w io.Writer) error {
	st := util.NewStack()

	if v.sel.Where != nil {
		st.Push(v.sel.WhereRootID)
	}

	for {
		if st.Len() == 0 {
			break
		}

		intf := st.Pop()

		id, ok := intf.(int)

		if !ok {
			return fmt.Errorf("18: unexpected value %v (%t)", intf, intf)
		}

		if id >= opKey {
			switch qcode.ExpOp(id - opKey) {
			case qcode.OpAnd:
				io.WriteString(w, ` AND `)
			case qcode.OpOr:
				io.WriteString(w, ` OR `)
			case qcode.OpNot:
				io.WriteString(w, `NOT `)
			default:
				return fmt.Errorf("11: unexpected value %d", (id - opKey))
			}
			continue
		}

		ex := v.sel.Where[id]

		switch ex.Op {
		case qcode.OpAnd, qcode.OpOr:
			for i := len(ex.Children) - 1; i >= 0; i-- {
				st.Push(ex.Children[i])
				if i > 0 {
					st.Push(int(ex.Op + opKey))
				}
			}
			continue
		case qcode.OpNot:
			st.Push(ex.Children[0])
			st.Push(int(qcode.OpNot + opKey))
			continue
		}

		if ex.NestedCol {
			fmt.Fprintf(w, `(("%s") `, ex.Col)
		} else if len(ex.Col) != 0 {
			fmt.Fprintf(w, `(("%s"."%s") `, v.sel.Table, ex.Col)
		}
		valExists := true

		switch ex.Op {
		case qcode.OpEquals:
			io.WriteString(w, `=`)
		case qcode.OpNotEquals:
			io.WriteString(w, `!=`)
		case qcode.OpGreaterOrEquals:
			io.WriteString(w, `>=`)
		case qcode.OpLesserOrEquals:
			io.WriteString(w, `<=`)
		case qcode.OpGreaterThan:
			io.WriteString(w, `>`)
		case qcode.OpLesserThan:
			io.WriteString(w, `<`)
		case qcode.OpIn:
			io.WriteString(w, `IN`)
		case qcode.OpNotIn:
			io.WriteString(w, `NOT IN`)
		case qcode.OpLike:
			io.WriteString(w, `LIKE`)
		case qcode.OpNotLike:
			io.WriteString(w, `NOT LIKE`)
		case qcode.OpILike:
			io.WriteString(w, `ILIKE`)
		case qcode.OpNotILike:
			io.WriteString(w, `NOT ILIKE`)
		case qcode.OpSimilar:
			io.WriteString(w, `SIMILAR TO`)
		case qcode.OpNotSimilar:
			io.WriteString(w, `NOT SIMILAR TO`)
		case qcode.OpContains:
			io.WriteString(w, `@>`)
		case qcode.OpContainedIn:
			io.WriteString(w, `<@`)
		case qcode.OpHasKey:
			io.WriteString(w, `?`)
		case qcode.OpHasKeyAny:
			io.WriteString(w, `?|`)
		case qcode.OpHasKeyAll:
			io.WriteString(w, `?&`)
		case qcode.OpIsNull:
			if bytes.EqualFold(ex.Val, []byte("true")) {
				io.WriteString(w, `IS NULL)`)
			} else {
				io.WriteString(w, `IS NOT NULL)`)
			}
			valExists = false
		case qcode.OpEqID:
			if len(v.ti.PrimaryCol) == 0 {
				return fmt.Errorf("no primary key column defined for %s", v.sel.Table)
			}
			fmt.Fprintf(w, `(("%s") =`, v.ti.PrimaryCol)
		case qcode.OpTsQuery:
			if len(v.ti.TSVCol) == 0 {
				return fmt.Errorf("no tsv column defined for %s", v.sel.Table)
			}

			fmt.Fprintf(w, `(("%s") @@ to_tsquery('%s'))`, v.ti.TSVCol, ex.Val)
			valExists = false

		default:
			return fmt.Errorf("[Where] unexpected op code %d", ex.Op)
		}

		if valExists {
			if ex.Type == qcode.ValList {
				renderList(w, &ex)
			} else {
				renderVal(w, &ex, v.vars)
			}
			io.WriteString(w, `)`)
		}
	}

	return nil
}

func renderOrderBy(w io.Writer, sel *qcode.Select) error {
	io.WriteString(w, ` ORDER BY `)
	for i := range sel.OrderBy {
		if i != 0 {
			io.WriteString(w, ", ")
		}
		ob := sel.OrderBy[i]

		switch ob.Order {
		case qcode.OrderAsc:
			fmt.Fprintf(w, `"%s_%d.ob.%s" ASC`, sel.Table, sel.ID, ob.Col)
		case qcode.OrderDesc:
			fmt.Fprintf(w, `"%s_%d.ob.%s" DESC`, sel.Table, sel.ID, ob.Col)
		case qcode.OrderAscNullsFirst:
			fmt.Fprintf(w, `"%s_%d.ob.%s" ASC NULLS FIRST`, sel.Table, sel.ID, ob.Col)
		case qcode.OrderDescNullsFirst:
			fmt.Fprintf(w, `%s_%d.ob.%s DESC NULLS FIRST`, sel.Table, sel.ID, ob.Col)
		case qcode.OrderAscNullsLast:
			fmt.Fprintf(w, `"%s_%d.ob.%s ASC NULLS LAST`, sel.Table, sel.ID, ob.Col)
		case qcode.OrderDescNullsLast:
			fmt.Fprintf(w, `%s_%d.ob.%s DESC NULLS LAST`, sel.Table, sel.ID, ob.Col)
		default:
			return fmt.Errorf("13: unexpected value %v", ob.Order)
		}
	}
	return nil
}

func (v selectBlock) renderDistinctOn(w io.Writer) {
	io.WriteString(w, ` DISTINCT ON (`)
	for i := range v.sel.DistinctOn {
		if i != 0 {
			io.WriteString(w, ", ")
		}
		fmt.Fprintf(w, `"%s_%d.ob.%s"`,
			v.sel.Table, v.sel.ID, v.sel.DistinctOn[i])
	}
	io.WriteString(w, `) `)
}

func renderList(w io.Writer, ex *qcode.Exp) {
	io.WriteString(w, ` (`)
	for i := range ex.ListVal {
		if i != 0 {
			io.WriteString(w, ", ")
		}
		switch ex.ListType {
		case qcode.ValBool, qcode.ValInt, qcode.ValFloat:
			w.Write(ex.ListVal[i])
		case qcode.ValStr:
			fmt.Fprintf(w, `'%s'`, ex.ListVal[i])
		}
	}
	io.WriteString(w, `)`)
}

func renderVal(w io.Writer, ex *qcode.Exp, vars map[string]string) {
	io.WriteString(w, ` (`)
	switch ex.Type {
	case qcode.ValBool, qcode.ValInt, qcode.ValFloat:
		if len(ex.Val) != 0 {
			fmt.Fprintf(w, `%s`, ex.Val)
		} else {
			io.WriteString(w, `''`)
		}
	case qcode.ValStr:
		fmt.Fprintf(w, `'%s'`, ex.Val)
	case qcode.ValVar:
		if val, ok := vars[string(ex.Val)]; ok {
			io.WriteString(w, val)
		} else {
			fmt.Fprintf(w, `'{{%s}}'`, ex.Val)
		}
	}
	io.WriteString(w, `)`)
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

func hasBit(n uint32, pos int) bool {
	val := n & (1 << uint(pos))
	return (val > 0)
}
