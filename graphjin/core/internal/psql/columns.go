package psql

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/dosco/super-graph/core/internal/qcode"
)

func (c *compilerContext) renderBaseColumns(
	sel *qcode.Select,
	ti *DBTableInfo,
	childCols []*qcode.Column) ([]int, bool, error) {

	var realColsRendered []int

	colcount := (len(sel.Cols) + len(sel.OrderBy) + 1)
	colmap := make(map[string]struct{}, colcount)

	isSearch := sel.Args["search"] != nil
	isCursorPaged := sel.Paging.Type != qcode.PtOffset
	isAgg := false

	i := 0
	for n, col := range sel.Cols {
		cn := col.Name
		colmap[cn] = struct{}{}

		_, isRealCol := ti.ColMap[cn]

		if isRealCol {
			c.renderComma(i)
			realColsRendered = append(realColsRendered, n)
			colWithTable(c.w, ti.Name, cn)

		} else {
			switch {
			case isSearch && cn == "search_rank":
				if err := c.renderColumnSearchRank(sel, ti, col, i); err != nil {
					return nil, false, err
				}

			case isSearch && strings.HasPrefix(cn, "search_headline_"):
				if err := c.renderColumnSearchHeadline(sel, ti, col, i); err != nil {
					return nil, false, err
				}

			case cn == "__typename":
				if err := c.renderColumnTypename(sel, ti, col, i); err != nil {
					return nil, false, err
				}

			case strings.HasSuffix(cn, "_cursor"):
				continue

			default:
				if err := c.renderColumnFunction(sel, ti, col, i); err != nil {
					return nil, false, err
				}

				isAgg = true
			}
		}
		i++

	}

	if isCursorPaged {
		if _, ok := colmap[ti.PrimaryCol.Key]; !ok {
			colmap[ti.PrimaryCol.Key] = struct{}{}
			c.renderComma(i)
			colWithTable(c.w, ti.Name, ti.PrimaryCol.Name)
		}
		i++
	}

	for _, ob := range sel.OrderBy {
		if _, ok := colmap[ob.Col]; ok {
			continue
		}
		colmap[ob.Col] = struct{}{}
		c.renderComma(i)
		colWithTable(c.w, ti.Name, ob.Col)
		i++
	}

	for _, col := range childCols {
		if _, ok := colmap[col.Name]; ok {
			continue
		}
		c.renderComma(i)
		colWithTable(c.w, col.Table, col.Name)
		i++
	}

	return realColsRendered, isAgg, nil
}

func (c *compilerContext) renderColumnSearchRank(sel *qcode.Select, ti *DBTableInfo, col qcode.Column, columnsRendered int) error {
	if isColumnBlocked(sel, col.Name) {
		return nil
	}

	if ti.TSVCol == nil {
		return errors.New("no ts_vector column found")
	}
	cn := ti.TSVCol.Name
	arg := sel.Args["search"]

	c.renderComma(columnsRendered)
	//fmt.Fprintf(w, `ts_rank("%s"."%s", websearch_to_tsquery('%s')) AS %s`,
	//c.sel.Name, cn, arg.Val, col.Name)
	_, _ = io.WriteString(c.w, `ts_rank(`)
	colWithTable(c.w, ti.Name, cn)
	if c.schema.ver >= 110000 {
		_, _ = io.WriteString(c.w, `, websearch_to_tsquery(`)
	} else {
		_, _ = io.WriteString(c.w, `, to_tsquery(`)
	}
	c.md.renderValueExp(c.w, Param{Name: arg.Val, Type: "string"})
	_, _ = io.WriteString(c.w, `))`)
	alias(c.w, col.Name)

	return nil
}

func (c *compilerContext) renderColumnSearchHeadline(sel *qcode.Select, ti *DBTableInfo, col qcode.Column, columnsRendered int) error {
	cn := col.Name[16:]

	if isColumnBlocked(sel, cn) {
		return nil
	}
	arg := sel.Args["search"]

	c.renderComma(columnsRendered)
	//fmt.Fprintf(w, `ts_headline("%s"."%s", websearch_to_tsquery('%s')) AS %s`,
	//c.sel.Name, cn, arg.Val, col.Name)
	_, _ = io.WriteString(c.w, `ts_headline(`)
	colWithTable(c.w, ti.Name, cn)
	if c.schema.ver >= 110000 {
		_, _ = io.WriteString(c.w, `, websearch_to_tsquery(`)
	} else {
		_, _ = io.WriteString(c.w, `, to_tsquery(`)
	}
	c.md.renderValueExp(c.w, Param{Name: arg.Val, Type: "string"})
	_, _ = io.WriteString(c.w, `))`)
	alias(c.w, col.Name)

	return nil
}

func (c *compilerContext) renderColumnTypename(sel *qcode.Select, ti *DBTableInfo, col qcode.Column, columnsRendered int) error {
	if isColumnBlocked(sel, col.Name) {
		return nil
	}

	c.renderComma(columnsRendered)
	_, _ = io.WriteString(c.w, `(`)
	squoted(c.w, ti.Name)
	_, _ = io.WriteString(c.w, ` :: text)`)
	alias(c.w, col.Name)

	return nil
}

func (c *compilerContext) renderColumnFunction(sel *qcode.Select, ti *DBTableInfo, col qcode.Column, columnsRendered int) error {
	pl := funcPrefixLen(c.schema.fm, col.Name)

	if pl == 0 || !sel.Functions {
		return fmt.Errorf("unknown field: %s.%s", sel.Name, col.Name)
	}

	cn := col.Name[pl:]

	if isColumnBlocked(sel, cn) {
		return nil
	}

	fn := col.Name[:pl-1]

	c.renderComma(columnsRendered)

	//fmt.Fprintf(w, `%s("%s"."%s") AS %s`, fn, c.sel.Name, cn, col.Name)
	_, _ = io.WriteString(c.w, fn)
	_, _ = io.WriteString(c.w, `(`)
	colWithTable(c.w, ti.Name, cn)
	_, _ = io.WriteString(c.w, `)`)
	alias(c.w, col.Name)

	return nil
}

func (c *compilerContext) renderComma(columnsRendered int) {
	if columnsRendered != 0 {
		_, _ = io.WriteString(c.w, `, `)
	}
}

func isColumnBlocked(sel *qcode.Select, name string) bool {
	if len(sel.Allowed) != 0 {
		if _, ok := sel.Allowed[name]; !ok {
			return true
		}
	}
	return false
}
