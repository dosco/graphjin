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

	sel.Cols = make([]Column, 0, len(field.Children))
	aggExist := false

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
			if dbc, err := sel.Ti.GetColumnB(f.Name); err == nil {
				sel.addCol(Column{Col: dbc, FieldName: fname})
			} else {
				return err
			}
			// is a function
		} else {
			if agg {
				aggExist = true
			}
			fn.FieldName = fname
			sel.Funcs = append(sel.Funcs, fn)
		}
	}

	if aggExist && len(sel.Cols) != 0 {
		sel.GroupCols = true
	}

	if err := validateSelector(qc, sel, tr); err != nil {
		return err
	}

	if err := co.addRelColumns(qc, sel); err != nil {
		return err
	}

	co.addOrderByColumns(sel)
	return nil
}

func (co *Compiler) addOrderByColumns(sel *Select) {
	for _, ob := range sel.OrderBy {
		sel.addCol(Column{Col: ob.Col, Base: true})
	}
}

func (co *Compiler) addRelColumns(qc *QCode, sel *Select) error {
	if sel.Rel == nil {
		return nil
	}

	rel := sel.Rel
	psel := &qc.Selects[sel.ParentID]

	switch rel.Type {
	case sdata.RelOneToOne, sdata.RelOneToMany:
		psel.addCol(Column{Col: rel.Right.Col, Base: true})

	case sdata.RelOneToManyThrough:
		psel.addCol(Column{Col: rel.Left.Col, Base: true})

	case sdata.RelEmbedded:
		psel.addCol(Column{Col: rel.Left.Col, Base: true})

	case sdata.RelRemote:
		psel.addCol(Column{Col: rel.Right.Col, Base: true})
		sel.SkipRender = SkipTypeRemote

	case sdata.RelPolymorphic:
		psel.addCol(Column{Col: rel.Left.Col, Base: true})

		if v, err := rel.Left.Col.Ti.GetColumn(rel.Right.VTable); err == nil {
			psel.addCol(Column{Col: v, Base: true})
		} else {
			return err
		}

	case sdata.RelRecursive:
		sel.addCol(Column{Col: rel.Left.Col, Base: true})
		sel.addCol(Column{Col: rel.Right.Col, Base: true})
	}

	return nil
}

func (co *Compiler) orderByIDCol(sel *Select) error {
	idCol := sel.Ti.PrimaryCol
	if idCol.Name == "" {
		return fmt.Errorf("table requires primary key: %s", sel.Table)
	}

	sel.addCol(Column{Col: idCol, Base: true})

	for _, ob := range sel.OrderBy {
		if ob.Col.Name == idCol.Name {
			return nil
		}
	}

	sel.OrderBy = append(sel.OrderBy, OrderBy{Col: idCol, Order: sel.order})
	return nil
}

func validateSelector(qc *QCode, sel *Select, tr trval) error {
	for _, col := range sel.Cols {
		if !tr.columnAllowed(qc, col.Col.Name) {
			return fmt.Errorf("column blocked: %s (%s)", col.Col.Name, tr.role)
		}

		// if _, ok := sel.ColMap[col.FieldName]; ok {
		// 	return fmt.Errorf("duplicate field: %s", col.FieldName)
		// }
		// sel.ColMap[col.FieldName] = struct{}{}

		// if col.FieldName != col.Col.Name {
		// 	if _, ok := sel.ColMap[col.Col.Name]; ok {
		// 		return fmt.Errorf("duplicate column: %s", col.Col.Name)
		// 	}
		// 	sel.ColMap[col.Col.Name] = struct{}{}
		// }
	}

	if len(sel.Funcs) != 0 && tr.isFuncsBlocked() {
		return fmt.Errorf("functions blocked: %s (%s)", sel.Funcs[0].Col.Name, tr.role)
	}

	for _, fn := range sel.Funcs {
		var blocked bool
		var fnID string

		if fn.Col.Name != "" {
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
			sel.ColMap[fn.FieldName] = -1
		}

		if fn.FieldName != fnID {
			if _, ok := sel.ColMap[fnID]; ok {
				return fmt.Errorf("duplicate function: %s(%s)", fn.Name, fn.Col.Name)
			}
			sel.ColMap[fnID] = -1
		}
	}
	return nil
}

func (sel *Select) addCol(col Column) {
	if i, ok := sel.ColMap[col.Col.Name]; ok {
		// Replace column if re-added as not base only.
		if i != -1 && !col.Base {
			sel.Cols[i] = col
		}

	} else {
		// Else its new and just add it to the map and columns
		sel.Cols = append(sel.Cols, col)
		sel.ColMap[col.Col.Name] = len(sel.Cols) - 1
	}
}
