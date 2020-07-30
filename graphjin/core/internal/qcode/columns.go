package qcode

import (
	"fmt"

	"github.com/dosco/super-graph/core/internal/graph"
	"github.com/dosco/super-graph/core/internal/sdata"
	"github.com/dosco/super-graph/core/internal/util"
)

func (co *Compiler) compileColumns(
	field *graph.Field,
	op *graph.Operation,
	st *util.StackInt32,
	qc *QCode,
	sel *Select,
	tr trval) error {

	if sel.Rel != nil && sel.Rel.Type == sdata.RelRemote {
		return nil
	}

	sel.ColMap = make(map[string]struct{}, len(field.Children))
	sel.Cols = make([]Column, 0, len(field.Children))

	for _, cid := range field.Children {
		var fname string
		f := op.Fields[cid]

		if f.Alias != "" {
			fname = f.Alias
		} else {
			fname = f.Name
		}

		if len(f.Children) != 0 {
			val := f.ID | (sel.ID << 16)
			st.Push(val)
			continue
		}

		fn, agg, err := co.isFunction(sel, f.Name)
		if err != nil {
			return err
		}
		if fn.skip {
			continue
		}

		// not a function
		if fn.Name == "" {
			if dbc, err := sel.Ti.GetColumn(f.Name); err == nil {
				sel.Cols = append(sel.Cols, Column{Col: dbc, FieldName: fname})
			} else {
				return err
			}
			// is a function
		} else {
			if agg {
				sel.GroupCols = true
			}
			sel.Funcs = append(sel.Funcs, fn)
		}
	}

	if err := validateSelector(qc, sel, tr); err != nil {
		return err
	}

	if err := co.addRelColumns(qc, sel); err != nil {
		return err
	}

	co.addOrderByColumns(sel)
	co.addPagingColumns(sel)

	return nil
}

func (co *Compiler) addOrderByColumns(sel *Select) {
	for _, ob := range sel.OrderBy {
		if _, ok := sel.ColMap[ob.Col.Name]; !ok {
			sel.Cols = append(sel.Cols, Column{Col: ob.Col, Base: true})
			sel.ColMap[ob.Col.Name] = struct{}{}
		}
	}
}

func (co *Compiler) addRelColumns(qc *QCode, sel *Select) error {
	if sel.Rel == nil {
		return nil
	}

	rel := sel.Rel
	psel := &qc.Selects[sel.ParentID]

	var col1, col2 Column

	switch rel.Type {
	case sdata.RelOneToOne, sdata.RelOneToMany:
		col1 = Column{Col: rel.Right.Col, Base: true}

	case sdata.RelOneToManyThrough:
		col1 = Column{Col: rel.Left.Col, Base: true}

	case sdata.RelEmbedded:
		col1 = Column{Col: rel.Left.Col, Base: true}

	case sdata.RelRemote:
		col1 = Column{Col: rel.Right.Col, Base: true}
		sel.SkipRender = SkipTypeRemote

	case sdata.RelPolymorphic:
		col1 = Column{Col: rel.Left.Col, Base: true}
		if v, err := rel.Left.Col.Ti.GetColumn(rel.Right.VTable); err == nil {
			col2 = Column{Col: v, Base: true}
		} else {
			return err
		}
	}

	if col1.Col != nil {
		if _, ok := psel.ColMap[col1.Col.Name]; !ok {
			psel.Cols = append(psel.Cols, col1)
			psel.ColMap[col1.Col.Name] = struct{}{}
		}
	}

	if col2.Col != nil {
		if _, ok := psel.ColMap[col2.Col.Name]; !ok {
			psel.Cols = append(psel.Cols, col2)
			psel.ColMap[col2.Col.Name] = struct{}{}
		}
	}
	return nil
}

func (co *Compiler) addPagingColumns(sel *Select) {
	if sel.Paging.Type != PTForward && sel.Paging.Type != PTBackward {
		return
	}
	picol := sel.Ti.PrimaryCol

	if _, ok := sel.ColMap[picol.Name]; !ok {
		sel.Cols = append(sel.Cols, Column{Col: picol, Base: true})
		sel.ColMap[picol.Name] = struct{}{}
	}

	picolFound := false
	for _, ob := range sel.OrderBy {
		if ob.Col.Key == picol.Key {
			picolFound = true
			break
		}
	}

	if !picolFound {
		var ob OrderBy

		switch sel.Paging.Type {
		case PTForward:
			ob = OrderBy{Col: picol, Order: OrderAsc}
		case PTBackward:
			ob = OrderBy{Col: picol, Order: OrderDesc}
		}
		sel.OrderBy = append(sel.OrderBy, ob)
	}
}

func validateSelector(qc *QCode, sel *Select, tr trval) error {
	for _, col := range sel.Cols {
		if !tr.columnAllowed(qc, col.Col.Name) {
			return fmt.Errorf("column blocked: %s (%s)", col.Col.Name, tr.role)
		}

		if _, ok := sel.ColMap[col.FieldName]; ok {
			return fmt.Errorf("duplicate field: %s", col.FieldName)
		}
		sel.ColMap[col.FieldName] = struct{}{}

		if col.FieldName != col.Col.Name {
			if _, ok := sel.ColMap[col.Col.Name]; ok {
				return fmt.Errorf("duplicate column: %s", col.Col.Name)
			}
			sel.ColMap[col.Col.Name] = struct{}{}
		}
	}

	if len(sel.Funcs) != 0 && tr.isFuncsBlocked() {
		return fmt.Errorf("functions blocked: %s (%s)", sel.Funcs[0].Col.Name, tr.role)
	}

	for _, fn := range sel.Funcs {
		var blocked bool
		var fnID string

		if fn.Col != nil {
			blocked = !tr.columnAllowed(qc, fn.Col.Name)
			fnID = (fn.Name + fn.Col.Name)
		} else {
			blocked = !tr.columnAllowed(qc, fn.Name)
			fnID = fn.Name
		}

		if blocked {
			return fmt.Errorf("column blocked: %s (%s)", fn.Name, tr.role)
		}

		if fn.FieldName != "" {
			if _, ok := sel.ColMap[fn.FieldName]; ok {
				return fmt.Errorf("duplicate field: %s", fn.FieldName)
			}
			sel.ColMap[fn.FieldName] = struct{}{}
		}

		if fn.FieldName != fnID {
			if _, ok := sel.ColMap[fnID]; ok {
				return fmt.Errorf("duplicate function: %s(%s)", fn.Name, fn.Col.Name)
			}
			sel.ColMap[fnID] = struct{}{}
		}
	}
	return nil
}
