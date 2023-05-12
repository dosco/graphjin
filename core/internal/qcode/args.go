package qcode

import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/dosco/graphjin/core/v3/internal/graph"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func (co *Compiler) compileSelectArgs(sel *Select, args []graph.Arg, role string) (err error) {
	for _, a := range args {
		switch a.Name {
		case "id":
			err = co.compileArgID(sel, a)

		case "search":
			err = co.compileArgSearch(sel, a)

		case "where":
			err = co.compileArgWhere(sel, a, role)

		case "orderBy", "order_by", "order":
			err = co.compileArgOrderBy(sel, a)

		case "distinctOn", "distinct_on", "distinct":
			err = co.compileArgDistinctOn(sel, a)

		case "limit":
			err = co.compileArgLimit(sel, a)

		case "offset":
			err = co.compileArgOffset(sel, a)

		case "first":
			err = co.compileArgFirstLast(sel, a, OrderAsc)

		case "last":
			err = co.compileArgFirstLast(sel, a, OrderDesc)

		case "after":
			err = co.compileArgAfterBefore(sel, a, PTForward)

		case "before":
			err = co.compileArgAfterBefore(sel, a, PTBackward)

		case "find":
			err = co.compileArgFind(sel, a)

		case "args":
			err = co.compileArgArgs(sel, a)

		// case "includeIf", "include_if":
		// 	err = co.compileArgSkipIncludeIf(false, sel, &sel.Field, a, role)

		// case "skipIf", "skip_if":
		// 	err = co.compileArgSkipIncludeIf(true, sel, &sel.Field, a, role)

		case "insert", "update", "upsert", "delete":

		default:
			return unknownArg(a)
		}

		if err != nil {
			return fmt.Errorf("%s: %w", a.Name, err)
		}
	}
	return
}

func (co *Compiler) compileArgFind(sel *Select, arg graph.Arg) (err error) {
	if err = validateArg(arg, graph.NodeStr); err != nil {
		return err
	}

	// Only allow on recursive relationship selectors
	if sel.Rel.Type != sdata.RelRecursive {
		return fmt.Errorf("selector '%s' is not recursive", sel.FieldName)
	}
	if arg.Val.Val != "parents" && arg.Val.Val != "children" {
		return fmt.Errorf("valid values 'parents' or 'children'")
	}
	sel.addIArg(Arg{Name: arg.Name, Val: arg.Val.Val})
	return nil
}

func (co *Compiler) compileArgID(sel *Select, arg graph.Arg) (err error) {
	if sel.ParentID != -1 {
		return fmt.Errorf("can only be specified at the query root")
	}
	if err = validateArg(arg, graph.NodeNum, graph.NodeStr, graph.NodeVar); err != nil {
		return
	}
	node := arg.Val

	if sel.Ti.PrimaryCol.Name == "" {
		return fmt.Errorf("no primary key column defined for '%s'", sel.Table)
	}

	ex := newExpOp(OpEquals)
	ex.Left.Col = sel.Ti.PrimaryCol

	switch node.Type {
	case graph.NodeNum:
		if _, err := strconv.ParseInt(node.Val, 10, 32); err != nil {
			return err
		} else {
			ex.Right.ValType = ValNum
			ex.Right.Val = node.Val
		}

	case graph.NodeStr:
		ex.Right.ValType = ValStr
		ex.Right.Val = node.Val

	case graph.NodeVar:
		ex.Right.ValType = ValVar
		ex.Right.Val = node.Val
	}

	sel.Where.Exp = ex
	sel.Singular = true
	return nil
}

func (co *Compiler) compileArgSearch(sel *Select, arg graph.Arg) (err error) {
	if len(sel.Ti.FullText) == 0 {
		switch co.s.DBType() {
		case "mysql":
			return fmt.Errorf("no fulltext indexes defined for table '%s'", sel.Table)
		default:
			return fmt.Errorf("no tsvector column defined on table '%s'", sel.Table)
		}
	}
	if err = validateArg(arg, graph.NodeStr, graph.NodeVar); err != nil {
		return
	}

	ex := newExpOp(OpTsQuery)
	ex.Right.ValType = ValVar
	ex.Right.Val = arg.Val.Val

	sel.addIArg(Arg{Name: arg.Name, Val: arg.Val.Val})
	addAndFilter(&sel.Where, ex)
	return nil
}

func (co *Compiler) compileArgWhere(sel *Select, arg graph.Arg, role string) (err error) {
	if err = validateArg(arg, graph.NodeObj); err != nil {
		return
	}

	ex, err := co.compileArgFilter(sel, -1, arg, role)
	if err != nil {
		return
	}
	addAndFilterLast(&sel.Where, ex)
	return
}

func (co *Compiler) compileArgOrderBy(sel *Select, arg graph.Arg) (err error) {
	if err = validateArg(arg, graph.NodeObj, graph.NodeVar); err != nil {
		return
	}

	node := arg.Val
	cm := make(map[string]struct{})

	for _, ob := range sel.OrderBy {
		cm[ob.Col.Name] = struct{}{}
	}

	switch node.Type {
	case graph.NodeObj:
		return co.compileArgOrderByObj(sel, node, cm)

	case graph.NodeVar:
		return co.compileArgOrderByVar(sel, node, cm)
	}

	return nil
}

func (co *Compiler) compileArgSkipIncludeIf(skip bool, sel *Select, f *Field, arg graph.Arg, role string) (err error) {
	if err = validateArg(arg, graph.NodeObj); err != nil {
		return
	}
	sid := sel.ID

	// functions are rendered in the base select hence
	// no column suffix is needed when rendering the filter
	// expression
	if f.Type == FieldTypeFunc {
		sid = -1
	}

	ex, err := co.compileArgFilter(sel, sid, arg, role)
	if err != nil {
		return
	}
	if skip {
		addNotFilter(&f.FieldFilter, ex)
	} else {
		addAndFilter(&f.FieldFilter, ex)
	}
	return
}

func (co *Compiler) compileArgArgs(sel *Select, arg graph.Arg) (err error) {
	if sel.Ti.Type != "function" {
		return fmt.Errorf("'%s' does not have any argument", sel.Ti.Name)
	}

	if err = validateArg(arg, graph.NodeObj); err != nil {
		return
	}
	args, err := newArgs(sel, sel.Ti.Func, arg)
	if err != nil {
		return
	}
	sel.Args = args
	return
}

func (co *Compiler) compileArgDistinctOn(sel *Select, arg graph.Arg) (err error) {
	if err = validateArg(arg,
		graph.NodeList, graph.NodeLabel,
		graph.NodeList, graph.NodeStr,
		graph.NodeStr); err != nil {
		return
	}

	node := arg.Val

	if node.Type == graph.NodeStr {
		var col sdata.DBColumn
		if col, err = sel.Ti.GetColumn(node.Val); err != nil {
			return
		}
		switch co.s.DBType() {
		case "mysql":
			sel.OrderBy = append(sel.OrderBy, OrderBy{Order: OrderAsc, Col: col})
		default:
			sel.DistinctOn = append(sel.DistinctOn, col)
		}
	}

	for _, cn := range node.Children {
		var col sdata.DBColumn
		if col, err = sel.Ti.GetColumn(cn.Val); err != nil {
			return
		}
		switch co.s.DBType() {
		case "mysql":
			sel.OrderBy = append(sel.OrderBy, OrderBy{Order: OrderAsc, Col: col})
		default:
			sel.DistinctOn = append(sel.DistinctOn, col)
		}
	}

	return
}

func (co *Compiler) compileArgLimit(sel *Select, arg graph.Arg) (err error) {
	if err = validateArg(arg, graph.NodeNum, graph.NodeVar); err != nil {
		return
	}

	node := arg.Val

	switch node.Type {
	case graph.NodeNum:
		var n int64
		if n, err = strconv.ParseInt(node.Val, 10, 32); err != nil {
			return
		}
		sel.Paging.Limit = int32(n)

	case graph.NodeVar:
		if co.s.DBType() == "mysql" {
			return dbArgErr("limit", "number", "mysql")
		}
		sel.Paging.LimitVar = node.Val
	}
	return
}

func (co *Compiler) compileArgOffset(sel *Select, arg graph.Arg) (err error) {
	if err = validateArg(arg, graph.NodeNum, graph.NodeVar); err != nil {
		return
	}

	node := arg.Val

	switch node.Type {
	case graph.NodeNum:
		var n int64
		if n, err = strconv.ParseInt(node.Val, 10, 32); err != nil {
			return
		}
		sel.Paging.Offset = int32(n)

	case graph.NodeVar:
		if co.s.DBType() == "mysql" {
			return dbArgErr("offset", "number", "mysql")
		}
		sel.Paging.OffsetVar = node.Val
	}
	return nil
}

func (co *Compiler) compileArgFirstLast(sel *Select, arg graph.Arg, order Order) (err error) {
	if err := co.compileArgLimit(sel, arg); err != nil {
		return err
	}

	if !sel.Singular {
		sel.Paging.Cursor = true
	}

	sel.order = order
	return
}

func (co *Compiler) compileArgAfterBefore(sel *Select, arg graph.Arg, pt PagingType) (err error) {
	if err = validateArg(arg, graph.NodeVar); err != nil {
		return
	}

	node := arg.Val

	if node.Val != "cursor" {
		return fmt.Errorf("value for argument '%s' must be a variable named $cursor", arg.Name)
	}

	sel.Paging.Type = pt
	if !sel.Singular {
		sel.Paging.Cursor = true
	}
	return
}

func (co *Compiler) compileFieldArgs(sel *Select, f *Field, args []graph.Arg, role string) (err error) {
	for _, a := range args {
		switch a.Name {
		case "args":
			err = co.compileFuncArgArgs(sel, f, a)

		case "includeIf", "include_if":
			err = co.compileArgSkipIncludeIf(false, sel, f, a, role)

		case "skipIf", "skip_if":
			err = co.compileArgSkipIncludeIf(true, sel, f, a, role)

		default:
			err = unknownArg(a)
		}

		if err != nil {
			return err
		}
	}

	return nil
}

var numArgKeyRe = regexp.MustCompile(`^[a_]\d+`)

func (co *Compiler) compileFuncArgArgs(sel *Select, f *Field, arg graph.Arg) (err error) {
	if f.Type == FieldTypeFunc && len(f.Func.Inputs) == 0 {
		return fmt.Errorf("db function '%s': has no arguments", f.Func.Name)
	}

	if err = validateArg(arg, graph.NodeObj); err != nil {
		return
	}

	args, err := newArgs(sel, f.Func, arg)
	if err != nil {
		return
	}
	f.Args = args
	return
}
