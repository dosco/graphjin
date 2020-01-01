//nolint:errcheck
package psql

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/dosco/super-graph/qcode"
	"github.com/dosco/super-graph/util"
)

const (
	closeBlock = 500
)

type Variables map[string]json.RawMessage

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

func (c *Compiler) IDColumn(table string) (*DBColumn, error) {
	ti, err := c.schema.GetTable(table)
	if err != nil {
		return nil, err
	}

	if ti.PrimaryCol == nil {
		return nil, fmt.Errorf("no primary key column found")
	}

	return ti.PrimaryCol, nil
}

type compilerContext struct {
	w io.Writer
	s []qcode.Select
	*Compiler
}

func (co *Compiler) CompileEx(qc *qcode.QCode, vars Variables) (uint32, []byte, error) {
	w := &bytes.Buffer{}
	skipped, err := co.Compile(qc, w, vars)
	return skipped, w.Bytes(), err
}

func (co *Compiler) Compile(qc *qcode.QCode, w io.Writer, vars Variables) (uint32, error) {
	switch qc.Type {
	case qcode.QTQuery:
		return co.compileQuery(qc, w)
	case qcode.QTInsert, qcode.QTUpdate, qcode.QTDelete, qcode.QTUpsert:
		return co.compileMutation(qc, w, vars)
	}

	return 0, fmt.Errorf("Unknown operation type %d", qc.Type)
}

func (co *Compiler) compileQuery(qc *qcode.QCode, w io.Writer) (uint32, error) {
	if len(qc.Selects) == 0 {
		return 0, errors.New("empty query")
	}

	c := &compilerContext{w, qc.Selects, co}
	multiRoot := (len(qc.Roots) > 1)

	st := NewIntStack()

	if multiRoot {
		io.WriteString(c.w, `SELECT row_to_json("json_root") FROM (SELECT `)

		for i, id := range qc.Roots {
			root := qc.Selects[id]

			st.Push(root.ID + closeBlock)
			st.Push(root.ID)

			if i != 0 {
				io.WriteString(c.w, `, `)
			}

			io.WriteString(c.w, `"sel_`)
			int2string(c.w, root.ID)
			io.WriteString(c.w, `"."json_`)
			int2string(c.w, root.ID)
			io.WriteString(c.w, `"`)

			alias(c.w, root.FieldName)
		}

		io.WriteString(c.w, ` FROM `)

	} else {
		root := qc.Selects[0]

		io.WriteString(c.w, `SELECT json_object_agg(`)
		io.WriteString(c.w, `'`)
		io.WriteString(c.w, root.FieldName)
		io.WriteString(c.w, `', `)
		io.WriteString(c.w, `json_`)
		int2string(c.w, root.ID)

		st.Push(root.ID + closeBlock)
		st.Push(root.ID)

		io.WriteString(c.w, `) FROM `)
	}

	var ignored uint32

	for {
		if st.Len() == 0 {
			break
		}

		id := st.Pop()

		if id < closeBlock {
			sel := &c.s[id]

			if sel.ParentID == -1 {
				io.WriteString(c.w, `(`)
			}

			ti, err := c.schema.GetTable(sel.Name)
			if err != nil {
				return 0, err
			}

			if sel.ParentID != -1 {
				if err = c.renderLateralJoin(sel); err != nil {
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

			ti, err := c.schema.GetTable(sel.Name)
			if err != nil {
				return 0, err
			}

			err = c.renderSelectClose(sel, ti)
			if err != nil {
				return 0, err
			}

			if sel.ParentID != -1 {
				if err = c.renderLateralJoinClose(sel); err != nil {
					return 0, err
				}
			} else {
				io.WriteString(c.w, `)`)
				aliasWithID(c.w, `sel`, sel.ID)

				if st.Len() != 0 {
					io.WriteString(c.w, `, `)
				}
			}

			if len(sel.Args) != 0 {
				for _, v := range sel.Args {
					qcode.FreeNode(v)
				}
			}
		}
	}

	if multiRoot {
		io.WriteString(c.w, `) AS "json_root"`)
	}

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

		rel, err := c.schema.GetRel(child.Name, ti.Name)
		if err != nil {
			skipped |= (1 << uint(id))
			continue
		}

		switch rel.Type {
		case RelOneToOne, RelOneToMany:
			if _, ok := colmap[rel.Right.Col]; !ok {
				cols = append(cols, &qcode.Column{Table: ti.Name, Name: rel.Right.Col, FieldName: rel.Right.Col})
			}
			colmap[rel.Right.Col] = struct{}{}

		case RelOneToManyThrough:
			if _, ok := colmap[rel.Left.Col]; !ok {
				cols = append(cols, &qcode.Column{Table: ti.Name, Name: rel.Left.Col, FieldName: rel.Left.Col})
			}
			colmap[rel.Left.Col] = struct{}{}

		case RelRemote:
			if _, ok := colmap[rel.Left.Col]; !ok {
				cols = append(cols, &qcode.Column{Table: ti.Name, Name: rel.Left.Col, FieldName: rel.Right.Col})
			}
			colmap[rel.Left.Col] = struct{}{}
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
	if !ti.Singular {
		//fmt.Fprintf(w, `SELECT coalesce(json_agg("%s"`, c.sel.Name)
		io.WriteString(c.w, `SELECT coalesce(json_agg("`)
		io.WriteString(c.w, "json_")
		int2string(c.w, sel.ID)
		io.WriteString(c.w, `"`)

		if hasOrder {
			err := c.renderOrderBy(sel, ti)
			if err != nil {
				return skipped, err
			}
		}

		//fmt.Fprintf(w, `), '[]') AS "%s" FROM (`, c.sel.Name)
		io.WriteString(c.w, `), '[]')`)
		aliasWithID(c.w, "json", sel.ID)
		io.WriteString(c.w, ` FROM (`)
	}

	// ROW-TO-JSON
	io.WriteString(c.w, `SELECT `)

	if len(sel.DistinctOn) != 0 {
		c.renderDistinctOn(sel, ti)
	}

	io.WriteString(c.w, `row_to_json((`)

	//fmt.Fprintf(w, `SELECT "%d" FROM (SELECT `, c.sel.ID)
	io.WriteString(c.w, `SELECT "json_row_`)
	int2string(c.w, sel.ID)
	io.WriteString(c.w, `" FROM (SELECT `)

	// Combined column names
	c.renderColumns(sel, ti)

	c.renderRemoteRelColumns(sel, ti)

	err := c.renderJoinedColumns(sel, ti, skipped)
	if err != nil {
		return skipped, err
	}

	//fmt.Fprintf(w, `) AS "%d"`, c.sel.ID)
	io.WriteString(c.w, `)`)
	aliasWithID(c.w, "json_row", sel.ID)

	//fmt.Fprintf(w, `)) AS "%s"`, c.sel.Name)
	io.WriteString(c.w, `))`)
	aliasWithID(c.w, "json", sel.ID)
	// END-ROW-TO-JSON

	if hasOrder {
		c.renderOrderByColumns(sel, ti)
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
		err := c.renderOrderBy(sel, ti)
		if err != nil {
			return err
		}
	}

	switch {
	case ti.Singular:
		io.WriteString(c.w, ` LIMIT ('1') :: integer`)

	case len(sel.Paging.Limit) != 0:
		//fmt.Fprintf(w, ` LIMIT ('%s') :: integer`, c.sel.Paging.Limit)
		io.WriteString(c.w, ` LIMIT ('`)
		io.WriteString(c.w, sel.Paging.Limit)
		io.WriteString(c.w, `') :: integer`)

	case sel.Paging.NoLimit:
		break

	default:
		io.WriteString(c.w, ` LIMIT ('20') :: integer`)
	}

	if len(sel.Paging.Offset) != 0 {
		//fmt.Fprintf(w, ` OFFSET ('%s') :: integer`, c.sel.Paging.Offset)
		io.WriteString(c.w, `OFFSET ('`)
		io.WriteString(c.w, sel.Paging.Offset)
		io.WriteString(c.w, `') :: integer`)
	}

	if !ti.Singular {
		//fmt.Fprintf(w, `) AS "json_agg_%d"`, c.sel.ID)
		io.WriteString(c.w, `)`)
		aliasWithID(c.w, "json_agg", sel.ID)
	}

	return nil
}

func (c *compilerContext) renderLateralJoin(sel *qcode.Select) error {
	io.WriteString(c.w, ` LEFT OUTER JOIN LATERAL (`)
	return nil
}

func (c *compilerContext) renderLateralJoinClose(sel *qcode.Select) error {
	//fmt.Fprintf(w, `) AS "%s_%d_join" ON ('true')`, c.sel.Name, c.sel.ID)
	io.WriteString(c.w, `)`)
	aliasWithIDSuffix(c.w, sel.Name, sel.ID, "_join")
	io.WriteString(c.w, ` ON ('true')`)
	return nil
}

func (c *compilerContext) renderJoin(sel *qcode.Select, ti *DBTableInfo) error {
	parent := &c.s[sel.ParentID]
	return c.renderJoinByName(ti.Name, parent.Name, parent.ID)
}

func (c *compilerContext) renderJoinByName(table, parent string, id int32) error {
	rel, err := c.schema.GetRel(table, parent)
	if err != nil {
		return err
	}

	// This join is only required for one-to-many relations since
	// these make use of join tables that need to be pulled in.
	if rel.Type != RelOneToManyThrough {
		return err
	}

	pt, err := c.schema.GetTable(parent)
	if err != nil {
		return err
	}

	//fmt.Fprintf(w, ` LEFT OUTER JOIN "%s" ON (("%s"."%s") = ("%s_%d"."%s"))`,
	//rel.Through, rel.Through, rel.ColT, c.parent.Name, c.parent.ID, rel.Left.Col)
	io.WriteString(c.w, ` LEFT OUTER JOIN "`)
	io.WriteString(c.w, rel.Through)
	io.WriteString(c.w, `" ON ((`)
	colWithTable(c.w, rel.Through, rel.ColT)
	io.WriteString(c.w, `) = (`)
	colWithTableID(c.w, pt.Name, id, rel.Left.Col)
	io.WriteString(c.w, `))`)

	return nil
}

func (c *compilerContext) renderColumns(sel *qcode.Select, ti *DBTableInfo) {
	i := 0
	for _, col := range sel.Cols {
		n := funcPrefixLen(col.Name)
		if n != 0 {
			if !sel.Functions {
				continue
			}
			if len(sel.Allowed) != 0 {
				if _, ok := sel.Allowed[col.Name[n:]]; !ok {
					continue
				}
			}
		} else {
			if len(sel.Allowed) != 0 {
				if _, ok := sel.Allowed[col.Name]; !ok {
					continue
				}
			}
		}

		if i != 0 {
			io.WriteString(c.w, ", ")
		}
		//fmt.Fprintf(w, `"%s_%d"."%s" AS "%s"`,
		//c.sel.Name, c.sel.ID, col.Name, col.FieldName)
		colWithTableIDAlias(c.w, ti.Name, sel.ID, col.Name, col.FieldName)
		i++
	}
}

func (c *compilerContext) renderRemoteRelColumns(sel *qcode.Select, ti *DBTableInfo) {
	i := 0

	for _, id := range sel.Children {
		child := &c.s[id]

		rel, err := c.schema.GetRel(child.Name, sel.Name)
		if err != nil || rel.Type != RelRemote {
			continue
		}
		if i != 0 || len(sel.Cols) != 0 {
			io.WriteString(c.w, ", ")
		}
		//fmt.Fprintf(w, `"%s_%d"."%s" AS "%s"`,
		//c.sel.Name, c.sel.ID, rel.Left.Col, rel.Right.Col)
		colWithTableID(c.w, ti.Name, sel.ID, rel.Left.Col)
		alias(c.w, rel.Right.Col)
		i++
	}
}

func (c *compilerContext) renderJoinedColumns(sel *qcode.Select, ti *DBTableInfo, skipped uint32) error {
	colsRendered := len(sel.Cols) != 0

	for _, id := range sel.Children {
		skipThis := hasBit(skipped, uint32(id))

		if colsRendered && !skipThis {
			io.WriteString(c.w, ", ")
		}
		if skipThis {
			continue
		}
		childSel := &c.s[id]

		//fmt.Fprintf(w, `"%s_%d_join"."%s" AS "%s"`,
		//s.Name, s.ID, s.Name, s.FieldName)
		//if cti.Singular {
		io.WriteString(c.w, `"`)
		io.WriteString(c.w, childSel.Name)
		io.WriteString(c.w, `_`)
		int2string(c.w, childSel.ID)
		io.WriteString(c.w, `_join"."json_`)
		int2string(c.w, childSel.ID)
		io.WriteString(c.w, `" AS "`)
		io.WriteString(c.w, childSel.FieldName)
		io.WriteString(c.w, `"`)
	}

	return nil
}

func (c *compilerContext) renderBaseSelect(sel *qcode.Select, ti *DBTableInfo,
	childCols []*qcode.Column, skipped uint32) error {
	var groupBy []int

	isRoot := sel.ParentID == -1
	isFil := (sel.Where != nil && sel.Where.Op != qcode.OpNop)
	isSearch := sel.Args["search"] != nil
	isAgg := false

	io.WriteString(c.w, ` FROM (SELECT `)

	i := 0
	for n, col := range sel.Cols {
		cn := col.Name

		_, isRealCol := ti.ColMap[cn]

		if !isRealCol {
			if isSearch {
				switch {
				case cn == "search_rank":
					if len(sel.Allowed) != 0 {
						if _, ok := sel.Allowed[cn]; !ok {
							continue
						}
					}
					if ti.TSVCol == nil {
						return errors.New("no ts_vector column found")
					}
					cn = ti.TSVCol.Name
					arg := sel.Args["search"]

					if i != 0 {
						io.WriteString(c.w, `, `)
					}
					//fmt.Fprintf(w, `ts_rank("%s"."%s", websearch_to_tsquery('%s')) AS %s`,
					//c.sel.Name, cn, arg.Val, col.Name)
					io.WriteString(c.w, `ts_rank(`)
					colWithTable(c.w, ti.Name, cn)
					if c.schema.ver >= 110000 {
						io.WriteString(c.w, `, websearch_to_tsquery('`)
					} else {
						io.WriteString(c.w, `, to_tsquery('`)
					}
					io.WriteString(c.w, arg.Val)
					io.WriteString(c.w, `'))`)
					alias(c.w, col.Name)
					i++

				case strings.HasPrefix(cn, "search_headline_"):
					cn1 := cn[16:]
					if len(sel.Allowed) != 0 {
						if _, ok := sel.Allowed[cn1]; !ok {
							continue
						}
					}
					arg := sel.Args["search"]

					if i != 0 {
						io.WriteString(c.w, `, `)
					}
					//fmt.Fprintf(w, `ts_headline("%s"."%s", websearch_to_tsquery('%s')) AS %s`,
					//c.sel.Name, cn, arg.Val, col.Name)
					io.WriteString(c.w, `ts_headline(`)
					colWithTable(c.w, ti.Name, cn1)
					if c.schema.ver >= 110000 {
						io.WriteString(c.w, `, websearch_to_tsquery('`)
					} else {
						io.WriteString(c.w, `, to_tsquery('`)
					}
					io.WriteString(c.w, arg.Val)
					io.WriteString(c.w, `'))`)
					alias(c.w, col.Name)
					i++

				}
			} else {
				pl := funcPrefixLen(cn)
				if pl == 0 {
					if i != 0 {
						io.WriteString(c.w, `, `)
					}
					//fmt.Fprintf(w, `'%s not defined' AS %s`, cn, col.Name)
					io.WriteString(c.w, `'`)
					io.WriteString(c.w, cn)
					io.WriteString(c.w, ` not defined'`)
					alias(c.w, col.Name)
					i++

				} else if sel.Functions {
					cn1 := cn[pl:]
					if len(sel.Allowed) != 0 {
						if _, ok := sel.Allowed[cn1]; !ok {
							continue
						}
					}
					if i != 0 {
						io.WriteString(c.w, `, `)
					}
					fn := cn[0 : pl-1]
					isAgg = true

					//fmt.Fprintf(w, `%s("%s"."%s") AS %s`, fn, c.sel.Name, cn, col.Name)
					io.WriteString(c.w, fn)
					io.WriteString(c.w, `(`)
					colWithTable(c.w, ti.Name, cn1)
					io.WriteString(c.w, `)`)
					alias(c.w, col.Name)
					i++

				}
			}
		} else {
			groupBy = append(groupBy, n)
			//fmt.Fprintf(w, `"%s"."%s"`, c.sel.Name, cn)
			if i != 0 {
				io.WriteString(c.w, `, `)
			}
			colWithTable(c.w, ti.Name, cn)
			i++

		}
	}

	for _, col := range childCols {
		if i != 0 {
			io.WriteString(c.w, `, `)
		}

		//fmt.Fprintf(w, `"%s"."%s"`, col.Table, col.Name)
		colWithTable(c.w, col.Table, col.Name)
		i++
	}

	io.WriteString(c.w, ` FROM `)

	//fmt.Fprintf(w, ` FROM "%s"`, c.sel.Name)
	io.WriteString(c.w, `"`)
	io.WriteString(c.w, ti.Name)
	io.WriteString(c.w, `"`)

	// if tn, ok := c.tmap[sel.Name]; ok {
	// 	//fmt.Fprintf(w, ` FROM "%s" AS "%s"`, tn, c.sel.Name)
	// 	tableWithAlias(c.w, ti.Name, sel.Name)
	// } else {
	// 	//fmt.Fprintf(w, ` FROM "%s"`, c.sel.Name)
	// 	io.WriteString(c.w, `"`)
	// 	io.WriteString(c.w, sel.Name)
	// 	io.WriteString(c.w, `"`)
	// }

	if isRoot && isFil {
		io.WriteString(c.w, ` WHERE (`)
		if err := c.renderWhere(sel, ti); err != nil {
			return err
		}
		io.WriteString(c.w, `)`)
	}

	if !isRoot {
		if err := c.renderJoin(sel, ti); err != nil {
			return err
		}

		io.WriteString(c.w, ` WHERE (`)

		if err := c.renderRelationship(sel, ti); err != nil {
			return err
		}

		if isFil {
			io.WriteString(c.w, ` AND `)
			if err := c.renderWhere(sel, ti); err != nil {
				return err
			}
		}
		io.WriteString(c.w, `)`)
	}

	if isAgg {
		if len(groupBy) != 0 {
			io.WriteString(c.w, ` GROUP BY `)

			for i, id := range groupBy {
				if i != 0 {
					io.WriteString(c.w, `, `)
				}
				//fmt.Fprintf(w, `"%s"."%s"`, c.sel.Name, c.sel.Cols[id].Name)
				colWithTable(c.w, ti.Name, sel.Cols[id].Name)
			}
		}
	}

	switch {
	case ti.Singular:
		io.WriteString(c.w, ` LIMIT ('1') :: integer`)

	case len(sel.Paging.Limit) != 0:
		//fmt.Fprintf(w, ` LIMIT ('%s') :: integer`, c.sel.Paging.Limit)
		io.WriteString(c.w, ` LIMIT ('`)
		io.WriteString(c.w, sel.Paging.Limit)
		io.WriteString(c.w, `') :: integer`)

	case sel.Paging.NoLimit:
		break

	default:
		io.WriteString(c.w, ` LIMIT ('20') :: integer`)
	}

	if len(sel.Paging.Offset) != 0 {
		//fmt.Fprintf(w, ` OFFSET ('%s') :: integer`, c.sel.Paging.Offset)
		io.WriteString(c.w, ` OFFSET ('`)
		io.WriteString(c.w, sel.Paging.Offset)
		io.WriteString(c.w, `') :: integer`)
	}

	//fmt.Fprintf(w, `) AS "%s_%d"`, c.sel.Name, c.sel.ID)
	io.WriteString(c.w, `)`)
	aliasWithID(c.w, ti.Name, sel.ID)

	return nil
}

func (c *compilerContext) renderOrderByColumns(sel *qcode.Select, ti *DBTableInfo) {
	colsRendered := len(sel.Cols) != 0

	for i := range sel.OrderBy {
		if colsRendered {
			//io.WriteString(w, ", ")
			io.WriteString(c.w, `, `)
		}

		col := sel.OrderBy[i].Col
		//fmt.Fprintf(w, `"%s_%d"."%s" AS "%s_%d_%s_ob"`,
		//c.sel.Name, c.sel.ID, c,
		//c.sel.Name, c.sel.ID, c)
		colWithTableID(c.w, ti.Name, sel.ID, col)
		io.WriteString(c.w, ` AS `)
		tableIDColSuffix(c.w, sel.Name, sel.ID, col, "_ob")
	}
}

func (c *compilerContext) renderRelationship(sel *qcode.Select, ti *DBTableInfo) error {
	parent := c.s[sel.ParentID]

	pti, err := c.schema.GetTable(parent.Name)
	if err != nil {
		return err
	}

	return c.renderRelationshipByName(ti.Name, pti.Name, parent.ID)
}

func (c *compilerContext) renderRelationshipByName(table, parent string, id int32) error {
	rel, err := c.schema.GetRel(table, parent)
	if err != nil {
		return err
	}

	io.WriteString(c.w, `((`)

	switch rel.Type {
	case RelOneToOne, RelOneToMany:

		//fmt.Fprintf(w, `(("%s"."%s") = ("%s_%d"."%s"))`,
		//c.sel.Name, rel.Left.Col, c.parent.Name, c.parent.ID, rel.Right.Col)

		switch {
		case !rel.Left.Array && rel.Right.Array:
			colWithTable(c.w, table, rel.Left.Col)
			io.WriteString(c.w, `) = any (`)
			colWithTableID(c.w, parent, id, rel.Right.Col)

		case rel.Left.Array && !rel.Right.Array:
			colWithTableID(c.w, parent, id, rel.Right.Col)
			io.WriteString(c.w, `) = any (`)
			colWithTable(c.w, table, rel.Left.Col)

		default:
			colWithTable(c.w, table, rel.Left.Col)
			io.WriteString(c.w, `) = (`)
			colWithTableID(c.w, parent, id, rel.Right.Col)
		}

	case RelOneToManyThrough:
		// This requires the through table to be joined onto this select
		//fmt.Fprintf(w, `(("%s"."%s") = ("%s"."%s"))`,
		//c.sel.Name, rel.Left.Col, rel.Through, rel.Right.Col)

		switch {
		case !rel.Left.Array && rel.Right.Array:
			colWithTable(c.w, table, rel.Left.Col)
			io.WriteString(c.w, `) = any (`)
			colWithTable(c.w, rel.Through, rel.Right.Col)

		case rel.Left.Array && !rel.Right.Array:
			colWithTable(c.w, rel.Through, rel.Right.Col)
			io.WriteString(c.w, `) = any (`)
			colWithTable(c.w, table, rel.Left.Col)

		default:
			colWithTable(c.w, table, rel.Left.Col)
			io.WriteString(c.w, `) = (`)
			colWithTable(c.w, rel.Through, rel.Right.Col)
		}
	}
	io.WriteString(c.w, `))`)

	return nil
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
				io.WriteString(c.w, ` AND `)
			case qcode.OpOr:
				io.WriteString(c.w, ` OR `)
			case qcode.OpNot:
				io.WriteString(c.w, `NOT `)
			case qcode.OpFalse:
				io.WriteString(c.w, `false`)
			default:
				return fmt.Errorf("11: unexpected value %v (%t)", intf, intf)
			}

		case *qcode.Exp:
			switch val.Op {
			case qcode.OpFalse:
				st.Push(val.Op)
				qcode.FreeExp(val)

			case qcode.OpAnd, qcode.OpOr:
				for i := len(val.Children) - 1; i >= 0; i-- {
					st.Push(val.Children[i])
					if i > 0 {
						st.Push(val.Op)
					}
				}
				qcode.FreeExp(val)

			case qcode.OpNot:
				st.Push(val.Children[0])
				st.Push(qcode.OpNot)
				qcode.FreeExp(val)

			default:
				if len(val.NestedCols) != 0 {
					io.WriteString(c.w, `EXISTS `)

					if err := c.renderNestedWhere(val, sel, ti); err != nil {
						return err
					}

				} else {
					//fmt.Fprintf(w, `(("%s"."%s") `, c.sel.Name, val.Col)
					if err := c.renderOp(val, sel, ti); err != nil {
						return err
					}
					qcode.FreeExp(val)
				}
			}

		default:
			return fmt.Errorf("12: unexpected value %v (%t)", intf, intf)
		}

	}

	return nil
}

func (c *compilerContext) renderNestedWhere(ex *qcode.Exp, sel *qcode.Select, ti *DBTableInfo) error {
	for i := 0; i < len(ex.NestedCols)-1; i++ {
		cti, err := c.schema.GetTable(ex.NestedCols[i])
		if err != nil {
			return err
		}

		if i != 0 {
			io.WriteString(c.w, ` AND `)
		}

		io.WriteString(c.w, `(SELECT 1 FROM `)
		io.WriteString(c.w, cti.Name)

		if err := c.renderJoinByName(cti.Name, ti.Name, -1); err != nil {
			return err
		}

		io.WriteString(c.w, ` WHERE `)

		if err := c.renderRelationshipByName(cti.Name, ti.Name, -1); err != nil {
			return err
		}

	}

	for i := 0; i < len(ex.NestedCols)-1; i++ {
		io.WriteString(c.w, `)`)
	}

	return nil
}

func (c *compilerContext) renderOp(ex *qcode.Exp, sel *qcode.Select, ti *DBTableInfo) error {
	var col *DBColumn
	var ok bool

	if ex.Op == qcode.OpNop {
		return nil
	}

	if len(ex.Col) != 0 {
		if col, ok = ti.ColMap[ex.Col]; !ok {
			return fmt.Errorf("no column '%s' found ", ex.Col)
		}

		io.WriteString(c.w, `((`)
		colWithTable(c.w, ti.Name, ex.Col)
		io.WriteString(c.w, `) `)
	}

	switch ex.Op {
	case qcode.OpEquals:
		io.WriteString(c.w, `=`)
	case qcode.OpNotEquals:
		io.WriteString(c.w, `!=`)
	case qcode.OpGreaterOrEquals:
		io.WriteString(c.w, `>=`)
	case qcode.OpLesserOrEquals:
		io.WriteString(c.w, `<=`)
	case qcode.OpGreaterThan:
		io.WriteString(c.w, `>`)
	case qcode.OpLesserThan:
		io.WriteString(c.w, `<`)
	case qcode.OpIn:
		io.WriteString(c.w, `IN`)
	case qcode.OpNotIn:
		io.WriteString(c.w, `NOT IN`)
	case qcode.OpLike:
		io.WriteString(c.w, `LIKE`)
	case qcode.OpNotLike:
		io.WriteString(c.w, `NOT LIKE`)
	case qcode.OpILike:
		io.WriteString(c.w, `ILIKE`)
	case qcode.OpNotILike:
		io.WriteString(c.w, `NOT ILIKE`)
	case qcode.OpSimilar:
		io.WriteString(c.w, `SIMILAR TO`)
	case qcode.OpNotSimilar:
		io.WriteString(c.w, `NOT SIMILAR TO`)
	case qcode.OpContains:
		io.WriteString(c.w, `@>`)
	case qcode.OpContainedIn:
		io.WriteString(c.w, `<@`)
	case qcode.OpHasKey:
		io.WriteString(c.w, `?`)
	case qcode.OpHasKeyAny:
		io.WriteString(c.w, `?|`)
	case qcode.OpHasKeyAll:
		io.WriteString(c.w, `?&`)
	case qcode.OpIsNull:
		if strings.EqualFold(ex.Val, "true") {
			io.WriteString(c.w, `IS NULL)`)
		} else {
			io.WriteString(c.w, `IS NOT NULL)`)
		}
		return nil

	case qcode.OpEqID:
		if ti.PrimaryCol == nil {
			return fmt.Errorf("no primary key column defined for %s", ti.Name)
		}
		col = ti.PrimaryCol
		//fmt.Fprintf(w, `(("%s") =`, c.ti.PrimaryCol)
		io.WriteString(c.w, `((`)
		colWithTable(c.w, ti.Name, ti.PrimaryCol.Name)
		//io.WriteString(c.w, ti.PrimaryCol)
		io.WriteString(c.w, `) =`)

	case qcode.OpTsQuery:
		if ti.PrimaryCol == nil {
			return fmt.Errorf("no tsv column defined for %s", ti.Name)
		}
		//fmt.Fprintf(w, `(("%s") @@ websearch_to_tsquery('%s'))`, c.ti.TSVCol, val.Val)
		io.WriteString(c.w, `((`)
		colWithTable(c.w, ti.Name, ti.TSVCol.Name)
		if c.schema.ver >= 110000 {
			io.WriteString(c.w, `) @@ websearch_to_tsquery('`)
		} else {
			io.WriteString(c.w, `) @@ to_tsquery('`)
		}
		io.WriteString(c.w, ex.Val)
		io.WriteString(c.w, `'))`)
		return nil

	default:
		return fmt.Errorf("[Where] unexpected op code %d", ex.Op)
	}

	if ex.Type == qcode.ValList {
		c.renderList(ex)
	} else {
		if col == nil {
			return errors.New("no column found for expression value")
		}
		c.renderVal(ex, c.vars, col)
	}

	io.WriteString(c.w, `)`)
	return nil
}

func (c *compilerContext) renderOrderBy(sel *qcode.Select, ti *DBTableInfo) error {
	io.WriteString(c.w, ` ORDER BY `)
	for i := range sel.OrderBy {
		if i != 0 {
			io.WriteString(c.w, `, `)
		}
		ob := sel.OrderBy[i]

		switch ob.Order {
		case qcode.OrderAsc:
			//fmt.Fprintf(w, `"%s_%d.ob.%s" ASC`, sel.Name, sel.ID, ob.Col)
			tableIDColSuffix(c.w, ti.Name, sel.ID, ob.Col, "_ob")
			io.WriteString(c.w, ` ASC`)
		case qcode.OrderDesc:
			//fmt.Fprintf(w, `"%s_%d.ob.%s" DESC`, sel.Name, sel.ID, ob.Col)
			tableIDColSuffix(c.w, ti.Name, sel.ID, ob.Col, "_ob")
			io.WriteString(c.w, ` DESC`)
		case qcode.OrderAscNullsFirst:
			//fmt.Fprintf(w, `"%s_%d.ob.%s" ASC NULLS FIRST`, sel.Name, sel.ID, ob.Col)
			tableIDColSuffix(c.w, ti.Name, sel.ID, ob.Col, "_ob")
			io.WriteString(c.w, ` ASC NULLS FIRST`)
		case qcode.OrderDescNullsFirst:
			//fmt.Fprintf(w, `%s_%d.ob.%s DESC NULLS FIRST`, sel.Name, sel.ID, ob.Col)
			tableIDColSuffix(c.w, ti.Name, sel.ID, ob.Col, "_ob")
			io.WriteString(c.w, ` DESC NULLLS FIRST`)
		case qcode.OrderAscNullsLast:
			//fmt.Fprintf(w, `"%s_%d.ob.%s ASC NULLS LAST`, sel.Name, sel.ID, ob.Col)
			tableIDColSuffix(c.w, ti.Name, sel.ID, ob.Col, "_ob")
			io.WriteString(c.w, ` ASC NULLS LAST`)
		case qcode.OrderDescNullsLast:
			//fmt.Fprintf(w, `%s_%d.ob.%s DESC NULLS LAST`, sel.Name, sel.ID, ob.Col)
			tableIDColSuffix(c.w, ti.Name, sel.ID, ob.Col, "_ob")
			io.WriteString(c.w, ` DESC NULLS LAST`)
		default:
			return fmt.Errorf("13: unexpected value %v", ob.Order)
		}
	}
	return nil
}

func (c *compilerContext) renderDistinctOn(sel *qcode.Select, ti *DBTableInfo) {
	io.WriteString(c.w, `DISTINCT ON (`)
	for i := range sel.DistinctOn {
		if i != 0 {
			io.WriteString(c.w, `, `)
		}
		//fmt.Fprintf(w, `"%s_%d.ob.%s"`, c.sel.Name, c.sel.ID, c.sel.DistinctOn[i])
		tableIDColSuffix(c.w, ti.Name, sel.ID, sel.DistinctOn[i], "_ob")
	}
	io.WriteString(c.w, `) `)
}

func (c *compilerContext) renderList(ex *qcode.Exp) {
	io.WriteString(c.w, ` (`)
	for i := range ex.ListVal {
		if i != 0 {
			io.WriteString(c.w, `, `)
		}
		switch ex.ListType {
		case qcode.ValBool, qcode.ValInt, qcode.ValFloat:
			io.WriteString(c.w, ex.ListVal[i])
		case qcode.ValStr:
			io.WriteString(c.w, `'`)
			io.WriteString(c.w, ex.ListVal[i])
			io.WriteString(c.w, `'`)
		}
	}
	io.WriteString(c.w, `)`)
}

func (c *compilerContext) renderVal(ex *qcode.Exp, vars map[string]string, col *DBColumn) {
	io.WriteString(c.w, ` `)

	switch ex.Type {
	case qcode.ValBool, qcode.ValInt, qcode.ValFloat:
		if len(ex.Val) != 0 {
			io.WriteString(c.w, ex.Val)
		} else {
			io.WriteString(c.w, `''`)
		}

	case qcode.ValStr:
		io.WriteString(c.w, `'`)
		io.WriteString(c.w, ex.Val)
		io.WriteString(c.w, `'`)

	case qcode.ValVar:
		io.WriteString(c.w, `'`)
		if val, ok := vars[ex.Val]; ok {
			io.WriteString(c.w, val)
		} else {
			//fmt.Fprintf(w, `'{{%s}}'`, ex.Val)
			io.WriteString(c.w, `{{`)
			io.WriteString(c.w, ex.Val)
			io.WriteString(c.w, `}}`)
		}
		io.WriteString(c.w, `' :: `)
		io.WriteString(c.w, col.Type)
	}
	//io.WriteString(c.w, `)`)
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

func alias(w io.Writer, alias string) {
	io.WriteString(w, ` AS "`)
	io.WriteString(w, alias)
	io.WriteString(w, `"`)
}

func aliasWithID(w io.Writer, alias string, id int32) {
	io.WriteString(w, ` AS "`)
	io.WriteString(w, alias)
	io.WriteString(w, `_`)
	int2string(w, id)
	io.WriteString(w, `"`)
}

func aliasWithIDSuffix(w io.Writer, alias string, id int32, suffix string) {
	io.WriteString(w, ` AS "`)
	io.WriteString(w, alias)
	io.WriteString(w, `_`)
	int2string(w, id)
	io.WriteString(w, suffix)
	io.WriteString(w, `"`)
}

func colWithTable(w io.Writer, table, col string) {
	io.WriteString(w, `"`)
	io.WriteString(w, table)
	io.WriteString(w, `"."`)
	io.WriteString(w, col)
	io.WriteString(w, `"`)
}

func colWithTableID(w io.Writer, table string, id int32, col string) {
	io.WriteString(w, `"`)
	io.WriteString(w, table)
	if id >= 0 {
		io.WriteString(w, `_`)
		int2string(w, id)
	}
	io.WriteString(w, `"."`)
	io.WriteString(w, col)
	io.WriteString(w, `"`)
}

func colWithTableIDAlias(w io.Writer, table string, id int32, col, alias string) {
	io.WriteString(w, `"`)
	io.WriteString(w, table)
	io.WriteString(w, `_`)
	int2string(w, id)
	io.WriteString(w, `"."`)
	io.WriteString(w, col)
	io.WriteString(w, `" AS "`)
	io.WriteString(w, alias)
	io.WriteString(w, `"`)
}

func tableIDColSuffix(w io.Writer, table string, id int32, col, suffix string) {
	io.WriteString(w, `"`)
	io.WriteString(w, table)
	io.WriteString(w, `_`)
	int2string(w, id)
	io.WriteString(w, `_`)
	io.WriteString(w, col)
	io.WriteString(w, suffix)
	io.WriteString(w, `"`)
}

const charset = "0123456789"

func int2string(w io.Writer, val int32) {
	if val < 10 {
		w.Write([]byte{charset[val]})
		return
	}

	temp := int32(0)
	val2 := val
	for val2 > 0 {
		temp *= 10
		temp += val2 % 10
		val2 = int32(float64(val2 / 10))
	}

	val3 := temp
	for val3 > 0 {
		d := val3 % 10
		val3 /= 10
		w.Write([]byte{charset[d]})
	}
}
