package psql

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/dosco/super-graph/qcode"
	"github.com/dosco/super-graph/util"
)

type Variables map[string]string

type Compiler struct {
	schema *DBSchema
	vars   Variables
}

func NewCompiler(schema *DBSchema, vars Variables) *Compiler {
	return &Compiler{schema, vars}
}

func (c *Compiler) Compile(w io.Writer, qc *qcode.QCode) error {
	st := util.NewStack()

	st.Push(&selectBlockClose{nil, qc.Query.Select})
	st.Push(&selectBlock{nil, qc.Query.Select, c})

	fmt.Fprintf(w, `SELECT json_object_agg('%s', %s) FROM (`,
		qc.Query.Select.FieldName, qc.Query.Select.Table)

	for {
		if st.Len() == 0 {
			break
		}

		intf := st.Pop()

		switch v := intf.(type) {
		case *selectBlock:
			childCols, childIDs := c.relationshipColumns(v.sel)
			v.render(w, c.schema, childCols, childIDs)

			for i := range childIDs {
				sub := v.sel.Joins[childIDs[i]]
				st.Push(&joinClose{sub})
				st.Push(&selectBlockClose{v.sel, sub})
				st.Push(&selectBlock{v.sel, sub, c})
				st.Push(&joinOpen{sub})
			}
		case *selectBlockClose:
			v.render(w)

		case *joinOpen:
			v.render(w)

		case *joinClose:
			v.render(w)

		}
	}

	io.WriteString(w, `) AS "done_1337";`)

	return nil
}

func (c *Compiler) relationshipColumns(parent *qcode.Select) (
	cols []*qcode.Column, childIDs []int) {

	colmap := make(map[string]struct{}, len(parent.Cols))
	for i := range parent.Cols {
		colmap[parent.Cols[i].Name] = struct{}{}
	}

	for i, sub := range parent.Joins {
		k := TTKey{sub.Table, parent.Table}

		rel, ok := c.schema.RelMap[k]
		if !ok {
			continue
		}

		if rel.Type == RelBelongTo || rel.Type == RelOneToMany {
			if _, ok := colmap[rel.Col2]; !ok {
				cols = append(cols, &qcode.Column{parent.Table, rel.Col2, rel.Col2})
			}
			childIDs = append(childIDs, i)
		}

		if rel.Type == RelOneToManyThrough {
			if _, ok := colmap[rel.Col1]; !ok {
				cols = append(cols, &qcode.Column{parent.Table, rel.Col1, rel.Col1})
			}
			childIDs = append(childIDs, i)
		}
	}

	return cols, childIDs
}

type selectBlock struct {
	parent *qcode.Select
	sel    *qcode.Select
	*Compiler
}

func (v *selectBlock) render(w io.Writer,
	schema *DBSchema, childCols []*qcode.Column, childIDs []int) error {

	isNotRoot := (v.parent != nil)
	hasFilters := (v.sel.Where != nil)
	hasOrder := len(v.sel.OrderBy) != 0

	// SELECT
	if v.sel.AsList {
		fmt.Fprintf(w, `SELECT coalesce(json_agg("%s"`, v.sel.Table)

		if hasOrder {
			err := renderOrderBy(w, v.sel)
			if err != nil {
				return err
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

	err := v.renderJoinedColumns(w, childIDs)
	if err != nil {
		return err
	}

	fmt.Fprintf(w, `) AS "sel_%d"`, v.sel.ID)

	fmt.Fprintf(w, `)) AS "%s"`, v.sel.Table)
	// END-ROW-TO-JSON

	if hasOrder {
		v.renderOrderByColumns(w)
	}
	// END-SELECT

	// FROM
	io.WriteString(w, " FROM (SELECT ")

	// Local column names
	v.renderLocalColumns(w, append(v.sel.Cols, childCols...))

	fmt.Fprintf(w, ` FROM "%s"`, v.sel.Table)

	if isNotRoot || hasFilters {
		if isNotRoot {
			v.renderJoinTable(w, schema, childIDs)
		}

		io.WriteString(w, ` WHERE (`)

		if isNotRoot {
			v.renderRelationship(w, schema)
		}

		if hasFilters {
			err := v.renderWhere(w)
			if err != nil {
				return err
			}
		}

		io.WriteString(w, `)`)
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
	// END-FROM

	return nil
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
	fmt.Fprintf(w, `) AS "%s_%d.join" ON ('true')`, v.sel.Table, v.sel.ID)
	return nil
}

func (v *selectBlock) renderJoinTable(w io.Writer, schema *DBSchema, childIDs []int) {
	k := TTKey{v.sel.Table, v.parent.Table}
	rel, ok := schema.RelMap[k]
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
		fmt.Fprintf(w, `"%s_%d"."%s" AS "%s"`,
			v.sel.Table, v.sel.ID, col.Name, col.FieldName)

		if i < len(v.sel.Cols)-1 {
			io.WriteString(w, ", ")
		}
	}
}

func (v *selectBlock) renderJoinedColumns(w io.Writer, childIDs []int) error {
	if len(v.sel.Cols) != 0 && len(childIDs) != 0 {
		io.WriteString(w, ", ")
	}

	for i := range childIDs {
		s := v.sel.Joins[childIDs[i]]

		fmt.Fprintf(w, `"%s_%d.join"."%s" AS "%s"`,
			s.Table, s.ID, s.Table, s.FieldName)

		if i < len(childIDs)-1 {
			io.WriteString(w, ", ")
		}
	}

	return nil
}

func (v *selectBlock) renderLocalColumns(w io.Writer, columns []*qcode.Column) {
	for i, col := range columns {
		if len(col.Table) != 0 {
			fmt.Fprintf(w, `"%s"."%s"`, col.Table, col.Name)
		} else {
			fmt.Fprintf(w, `"%s"."%s"`, v.sel.Table, col.Name)
		}

		if i < len(columns)-1 {
			io.WriteString(w, ", ")
		}
	}
}

func (v *selectBlock) renderOrderByColumns(w io.Writer) {
	if len(v.sel.Cols) != 0 {
		io.WriteString(w, ", ")
	}

	for i := range v.sel.OrderBy {
		c := v.sel.OrderBy[i].Col
		fmt.Fprintf(w, `"%s_%d"."%s" AS "%s_%d.ob.%s"`,
			v.sel.Table, v.sel.ID, c,
			v.sel.Table, v.sel.ID, c)

		if i < len(v.sel.OrderBy)-1 {
			io.WriteString(w, ", ")
		}
	}
}

func (v *selectBlock) renderRelationship(w io.Writer, schema *DBSchema) {
	hasFilters := (v.sel.Where != nil)

	k := TTKey{v.sel.Table, v.parent.Table}
	rel, ok := schema.RelMap[k]
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

	if hasFilters {
		io.WriteString(w, ` AND `)
	}
}

func (v *selectBlock) renderWhere(w io.Writer) error {
	st := util.NewStack()

	if v.sel.Where == nil {
		return nil
	}
	st.Push(v.sel.Where)

	for {
		if st.Len() == 0 {
			break
		}

		intf := st.Pop()

		switch val := intf.(type) {
		case qcode.ExpOp:
			switch val {
			case qcode.OpAnd:
				io.WriteString(w, ` AND `)
			case qcode.OpOr:
				io.WriteString(w, ` OR `)
			case qcode.OpNot:
				io.WriteString(w, `NOT `)
			default:
				return fmt.Errorf("[Where] unexpected value encountered %v", intf)
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
				fmt.Fprintf(w, `(("%s") `, val.Col)
			} else {
				fmt.Fprintf(w, `(("%s"."%s") `, v.sel.Table, val.Col)
			}
			valExists := true

			switch val.Op {
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
				if strings.EqualFold(val.Val, "true") {
					io.WriteString(w, `IS NULL`)
				} else {
					io.WriteString(w, `IS NOT NULL`)
				}
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
			}

			io.WriteString(w, `)`)

		default:
			return fmt.Errorf("[Where] unexpected value encountered %v", intf)
		}
	}

	return nil
}

func renderOrderBy(w io.Writer, sel *qcode.Select) error {
	io.WriteString(w, ` ORDER BY `)
	for i := range sel.OrderBy {
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
			return fmt.Errorf("[qcode.Order By] unexpected value encountered %v", ob.Order)
		}
		if i < len(sel.OrderBy)-1 {
			io.WriteString(w, ", ")
		}
	}
	return nil
}

func (v selectBlock) renderDistinctOn(w io.Writer) {
	io.WriteString(w, ` DISTINCT ON (`)
	for i := range v.sel.DistinctOn {
		fmt.Fprintf(w, `"%s_%d.ob.%s"`,
			v.sel.Table, v.sel.ID, v.sel.DistinctOn[i])

		if i < len(v.sel.DistinctOn)-1 {
			io.WriteString(w, ", ")
		}
	}
	io.WriteString(w, `) `)
}

func renderList(w io.Writer, ex *qcode.Exp) {
	io.WriteString(w, ` (`)
	for i := range ex.ListVal {
		switch ex.ListType {
		case qcode.ValBool, qcode.ValInt, qcode.ValFloat:
			io.WriteString(w, ex.ListVal[i])
		case qcode.ValStr:
			fmt.Fprintf(w, `'%s'`, ex.ListVal[i])
		}

		if i < len(ex.ListVal)-1 {
			io.WriteString(w, ", ")
		}
	}
	io.WriteString(w, `)`)
}

func renderVal(w io.Writer, ex *qcode.Exp, vars Variables) {
	io.WriteString(w, ` (`)
	switch ex.Type {
	case qcode.ValBool, qcode.ValInt, qcode.ValFloat:
		io.WriteString(w, ex.Val)
	case qcode.ValStr:
		fmt.Fprintf(w, `'%s'`, ex.Val)
	case qcode.ValVar:
		if val, ok := vars[ex.Val]; ok {
			io.WriteString(w, val)
		} else {
			fmt.Fprintf(w, `'{{%s}}'`, ex.Val)
		}
	}
	io.WriteString(w, `)`)
}
