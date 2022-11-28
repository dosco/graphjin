package qcode

import (
	"fmt"
	"strings"

	"github.com/dosco/graphjin/core/internal/graph"
	"github.com/dosco/graphjin/core/internal/sdata"
	"github.com/dosco/graphjin/core/internal/util"
)

func (co *Compiler) compileColumns(
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
	field graph.Field,
	tr trval) error {

	aggExist := false

	for _, cid := range field.Children {
		var fname string
		f := op.Fields[cid]

		if co.c.EnableCamelcase {
			if f.Alias == "" {
				f.Alias = f.Name
			}
			f.Name = util.ToSnake(f.Name)
		}

		if f.Alias != "" {
			fname = f.Alias
		} else {
			fname = f.Name
		}

		// these are all remote fields we use
		// these later to strip the response json
		if sel.Rel.Type == sdata.RelRemote {
			sel.Fields = append(sel.Fields, Field{FieldName: fname})
			continue
		}

		if len(f.Children) != 0 {
			val := f.ID | (sel.ID << 16)
			st.Push(val)
			continue
		}

		fn, agg, err := co.isFunction(sel, f.Name, f.Alias)
		if err != nil {
			return err
		}
		if fn.skip {
			continue
		}

		// not a function
		if fn.Name == "" {
			dbc, err := sel.Ti.GetColumn(f.Name)
			if err == nil {
				sel.addField(Field{Col: dbc, FieldName: fname})
			} else {
				return err
			}
			if dbc.Blocked {
				return fmt.Errorf("column: '%s.%s.%s' blocked",
					dbc.Schema, dbc.Table, dbc.Name)
			}
			// is a function
		} else {
			if agg {
				aggExist = true
			}
			sel.addFunc(fn)
		}
	}

	if aggExist && len(sel.Fields) != 0 {
		sel.GroupCols = true
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

// func (co *Compiler) addFuncColumns(qc *QCode, sel *Select) {
// 	for _, fn := range sel.Funcs {
// 		sel.addCol(Column{Col: fn.Col}, true)
// 	}
// }

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
	for _, col := range sel.Fields {
		if !tr.columnAllowed(qc, col.Col.Name) {
			return fmt.Errorf("column blocked: %s (%s)", col.Col.Name, tr.role)
		}
	}

	if len(sel.Funcs) != 0 && tr.isFuncsBlocked() {
		return fmt.Errorf("functions blocked: %s (%s)", sel.Funcs[0].Col.Name, tr.role)
	}

	for _, fn := range sel.Funcs {
		var blocked bool

		if fn.Col.Name != "" {
			blocked = !tr.columnAllowed(qc, fn.Col.Name)
		} else {
			blocked = !tr.columnAllowed(qc, fn.Name)
		}

		if blocked {
			return fmt.Errorf("column blocked: %s (%s)", fn.Name, tr.role)
		}
	}
	return nil
}

func (sel *Select) addField(f Field) {
	if sel.bcolExists(f.Col.Name) == -1 {
		sel.BCols = append(sel.BCols, Column(f))
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

func (sel *Select) addFunc(fn Function) {
	if sel.fieldExists(fn.FieldName) == -1 {
		sel.Funcs = append(sel.Funcs, fn)
	}
}
