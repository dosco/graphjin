package qcode

import (
	"fmt"
	"strings"

	"github.com/dosco/graphjin/core/internal/graph"
	"github.com/dosco/graphjin/core/internal/sdata"
	"github.com/dosco/graphjin/core/internal/util"
)

func (co *Compiler) compileFields(
	st *util.StackInt32,
	op *graph.Operation,
	qc *QCode,
	sel *Select,
	field graph.Field,
	tr trval) error {

	sel.Fields = make([]Field, 0, len(field.Children))
	sel.BCols = make([]Column, 0, len(field.Children))

	if err := co.compileChildColumns(st, op, qc, sel, field, tr); err != nil {
		return err
	}

	if err := validateSelector(qc, sel, tr); err != nil {
		return err
	}

	if err := co.addColumns(qc, sel); err != nil {
		return err
	}

	co.addOrderByColumns(sel)
	return nil
}

func (co *Compiler) compileChildColumns(
	st *util.StackInt32,
	op *graph.Operation,
	qc *QCode,
	sel *Select,
	gf graph.Field,
	tr trval) error {

	var aggExists bool
	for _, cid := range gf.Children {
		var field Field
		f := op.Fields[cid]

		if co.c.EnableCamelcase {
			if f.Alias == "" {
				f.Alias = f.Name
			}
			f.Name = util.ToSnake(f.Name)
		}

		if f.Alias != "" {
			field.FieldName = f.Alias
		} else {
			field.FieldName = f.Name
		}

		// these are all remote fields we use
		// these later to strip the response json
		if sel.Rel.Type == sdata.RelRemote {
			sel.Fields = append(sel.Fields, field)
			continue
		}

		if len(f.Children) != 0 {
			val := f.ID | (sel.ID << 16)
			st.Push(val)
			continue
		}

		if err := co.compileFieldDirectives(&field, f.Directives); err != nil {
			return err
		}

		switch {
		case f.Name == "__typename":
			sel.Typename = true
			continue

		case strings.HasSuffix(f.Name, "_cursor"):
			continue
		}

		fn, isFunc, err := co.isFunction(sel, f)
		if err != nil {
			return err
		}

		if isFunc {
			field.Type = FieldTypeFunc
			field.Func = fn.Func
			field.Args = fn.Args

			if err := co.compileFuncArgs(&field, f.Args); err != nil {
				return err
			}

			if fn.Agg && sel.Rel.Type == sdata.RelRecursive {
				sel.addBaseCol(Column{Col: fn.Args[0].Col})
			}

			aggExists = fn.Agg

		} else { // not a function
			if field.Col, err = sel.Ti.GetColumn(f.Name); err != nil {
				return err
			}

			if field.Col.Blocked {
				return fmt.Errorf("column: '%s.%s.%s' blocked",
					field.Col.Schema,
					field.Col.Table,
					field.Col.Name)
			}
		}
		sel.addField(field)
	}

	if aggExists {
		sel.GroupCols = true
	}
	return nil
}

func (co *Compiler) compileFuncArgs(f *Field, args []graph.Arg) error {
	if len(args) != 0 && len(f.Func.Inputs) == 0 {
		return fmt.Errorf("db function '%s' does not have any arguments", f.Func.Name)
	}

	for _, arg := range args {
		if arg.Name == "args" {
			if err := co.compileFuncArgArgs(f, arg); err != nil {
				return err
			}
			continue
		}
		input, err := f.Func.GetInput(arg.Name)
		if err != nil {
			return fmt.Errorf("db function %s: %w", f.Func.Name, err)
		}
		a := Arg{
			Name:    arg.Name,
			Val:     arg.Val.Val,
			ValType: input.Type,
		}
		if arg.Val.Type == graph.NodeVar {
			a.Type = ArgTypeVar
		}
		// if arg.Val.Type = graph.
		// fn.Col, err = sel.Ti.GetColumn(fname[(len(fn.Name) + 1):])
		// if err != nil {
		// 	return
		// }
		f.Args = append(f.Args, a)
	}

	return nil
}

func (co *Compiler) compileFuncArgArgs(f *Field, arg graph.Arg) error {
	if len(f.Func.Inputs) == 0 {
		return fmt.Errorf("db function '%s' does not have any arguments", f.Func.Name)
	}

	node := arg.Val

	if node.Type != graph.NodeList {
		return argErr("args", "list")
	}

	for i, n := range node.Children {
		a := Arg{Val: n.Val, ValType: f.Func.Inputs[i].Type}
		if n.Type == graph.NodeVar {
			a.Type = ArgTypeVar
		}
		f.Args = append(f.Args, a)
	}

	return nil
}

func (co *Compiler) addOrderByColumns(sel *Select) {
	for _, ob := range sel.OrderBy {
		sel.addBaseCol(Column{Col: ob.Col})
	}
}

func (co *Compiler) addColumns(qc *QCode, sel *Select) error {
	var rel sdata.DBRel

	switch {
	case len(sel.Joins) == 0:
		rel = sel.Rel
	case sel.Joins[0].Local:
		return nil
	default:
		rel = sel.Joins[0].Rel
	}
	if err := co.addRelColumns(qc, sel, rel); err != nil {
		return err
	}

	//co.addFuncColumns(qc, sel)
	return nil
}

func (co *Compiler) addRelColumns(qc *QCode, sel *Select, rel sdata.DBRel) error {
	var psel *Select

	if sel.ParentID != -1 {
		psel = &qc.Selects[sel.ParentID]
	} else {
		return nil
	}

	switch rel.Type {
	case sdata.RelNone:
		return nil

	case sdata.RelOneToOne, sdata.RelOneToMany:
		psel.addBaseCol(Column{Col: rel.Right.Col})

	case sdata.RelEmbedded:
		psel.addBaseCol(Column{Col: rel.Right.Col})

	case sdata.RelRemote:
		psel.addField(Field{Col: rel.Right.Col, FieldName: rel.Left.Col.Name})
		sel.SkipRender = SkipTypeRemote

	case sdata.RelPolymorphic:
		typeCol := rel.Left.Col
		typeCol.Name = rel.Left.Col.FKeyCol

		psel.addBaseCol(Column{Col: rel.Left.Col})
		psel.addBaseCol(Column{Col: typeCol})

	case sdata.RelRecursive:
		sel.addBaseCol(Column{Col: rel.Left.Col})
		sel.addBaseCol(Column{Col: rel.Right.Col})
	}
	return nil
}

func (co *Compiler) orderByIDCol(sel *Select) error {
	idCol := sel.Ti.PrimaryCol

	if idCol.Name == "" {
		return fmt.Errorf("table requires primary key: %s", sel.Ti.Name)
	}

	sel.addBaseCol(Column{Col: idCol})

	for _, ob := range sel.OrderBy {
		if ob.Col.Name == idCol.Name {
			return nil
		}
	}

	sel.OrderBy = append(sel.OrderBy, OrderBy{Col: idCol, Order: sel.order})
	return nil
}

func validateSelector(qc *QCode, sel *Select, tr trval) error {
	for _, f := range sel.Fields {
		if err := validateField(qc, f, tr); err != nil {
			return err
		}
	}
	return nil
}

func validateField(qc *QCode, f Field, tr trval) error {
	switch f.Type {
	case FieldTypeCol:
		if !tr.columnAllowed(qc, f.Col.Name) {
			return validateErr(tr, f.Col.Name, "db column blocked")
		}
	case FieldTypeFunc:
		if tr.isFuncsBlocked() {
			return validateErr(tr, f.Func.Name, "all db functions blocked")
		}
		if len(f.Args) != 0 && !tr.columnAllowed(qc, f.Args[0].Col.Name) {
			return validateErr(tr, f.Args[0].Col.Name, "db column blocked")
		}
	}

	return nil
}

func validateErr(tr trval, name, msg string) error {
	return fmt.Errorf("%s: %s (role: '%s')", msg, name, tr.role)
}

func (sel *Select) addField(f Field) {
	if f.Type == FieldTypeCol && sel.bcolExists(f.Col.Name) == -1 {
		sel.BCols = append(sel.BCols, Column{Col: f.Col, FieldName: f.FieldName})
	}
	if sel.fieldExists(f.FieldName) == -1 {
		sel.Fields = append(sel.Fields, f)
	}
}

func (sel *Select) addBaseCol(col Column) {
	if sel.bcolExists(col.Col.Name) == -1 {
		sel.BCols = append(sel.BCols, col)
	}
}

func (sel *Select) fieldExists(name string) int {
	for i, c := range sel.Fields {
		if strings.EqualFold(c.FieldName, name) {
			return i
		}
	}
	return -1
}

func (sel *Select) bcolExists(name string) int {
	for i, c := range sel.BCols {
		if strings.EqualFold(c.Col.Name, name) {
			return i
		}
	}
	return -1
}
