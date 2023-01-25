package qcode

import (
	"fmt"

	"github.com/dosco/graphjin/core/v3/internal/graph"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
	"github.com/dosco/graphjin/core/v3/internal/util"
)

func (co *Compiler) compileArgOrderByObj(sel *Select, parent *graph.Node, cm map[string]struct{}) error {
	st := util.NewStackInf()

	for i := range parent.Children {
		st.Push(parent.Children[i])
	}

	var obList []OrderBy

	var node *graph.Node
	var ok bool
	var err error

	for {
		if err != nil {
			return fmt.Errorf("argument '%s', %w", node.Name, err)
		}

		if st.Len() == 0 {
			break
		}

		intf := st.Pop()
		node, ok = intf.(*graph.Node)
		if !ok {
			err = fmt.Errorf("unexpected value '%v' (%t)", intf, intf)
			continue
		}

		// Check for type
		if node.Type != graph.NodeStr &&
			node.Type != graph.NodeObj &&
			node.Type != graph.NodeList &&
			node.Type != graph.NodeLabel {
			err = fmt.Errorf("expecting a string, object or list")
			continue
		}

		var ob OrderBy
		ti := sel.Ti
		cn := node

		switch node.Type {
		case graph.NodeStr, graph.NodeLabel:
			if ob.Order, err = toOrder(node.Val); err != nil { // sets the asc desc etc
				continue
			}

		case graph.NodeList:
			if ob, err = orderByFromList(node); err != nil {
				continue
			}

		case graph.NodeObj:
			var path []sdata.TPath
			if path, err = co.FindPath(node.Name, sel.Ti.Name, ""); err != nil {
				continue
			}
			ti = path[0].LT

			cn = node.Children[0]
			if ob.Order, err = toOrder(cn.Val); err != nil { // sets the asc desc etc
				continue
			}

			for i := len(path) - 1; i >= 0; i-- {
				p := path[i]
				rel := sdata.PathToRel(p)
				sel.Joins = append(sel.Joins, Join{
					Rel:    rel,
					Filter: buildFilter(rel, -1),
					Local:  true,
				})
			}
		}

		if err = co.setOrderByColName(ti, &ob, cn); err != nil {
			continue
		}

		if _, ok := cm[ob.Col.Name]; ok {
			err = fmt.Errorf("can only be defined once")
			continue
		}
		cm[ob.Col.Name] = struct{}{}
		obList = append(obList, ob)
	}

	for i := len(obList) - 1; i >= 0; i-- {
		sel.OrderBy = append(sel.OrderBy, obList[i])
	}

	return err
}

func orderByFromList(parent *graph.Node) (ob OrderBy, err error) {
	if len(parent.Children) != 2 {
		return ob, fmt.Errorf(`valid format is [values, order] (eg. [$list, "desc"])`)
	}

	valNode := parent.Children[0]
	orderNode := parent.Children[1]

	ob.Var = valNode.Val

	if ob.Order, err = toOrder(orderNode.Val); err != nil {
		return ob, err
	}
	return ob, nil
}

func (co *Compiler) compileArgOrderByVar(sel *Select, node *graph.Node, cm map[string]struct{}) (err error) {
	for k, v := range sel.tc.OrderBy {
		if err = compileOrderBy(sel, node.Val, k, v, cm); err != nil {
			return
		}
	}
	return
}

func (co *Compiler) setOrderByColName(ti sdata.DBTable, ob *OrderBy, node *graph.Node) (err error) {
	col, err := ti.GetColumn(co.ParseName(node.Name))
	if err != nil {
		return err
	}
	ob.Col = col
	return nil
}

func compileOrderBy(sel *Select,
	keyVar, key string,
	values [][2]string,
	cm map[string]struct{},
) error {
	obList := make([]OrderBy, 0, len(values))

	for _, v := range values {
		ob := OrderBy{KeyVar: keyVar, Key: key}
		ob.Order, _ = toOrder(v[1])

		col, err := sel.Ti.GetColumn(v[0])
		if err != nil {
			return err
		}
		ob.Col = col
		if _, ok := cm[ob.Col.Name]; ok {
			return fmt.Errorf("duplicate column '%s'", ob.Col.Name)
		}
		obList = append(obList, ob)
	}
	sel.OrderBy = append(sel.OrderBy, obList...)
	return nil
}

func toOrder(val string) (Order, error) {
	switch val {
	case "asc":
		return OrderAsc, nil
	case "desc":
		return OrderDesc, nil
	case "asc_nulls_first":
		return OrderAscNullsFirst, nil
	case "desc_nulls_first":
		return OrderDescNullsFirst, nil
	case "asc_nulls_last":
		return OrderAscNullsLast, nil
	case "desc_nulls_last":
		return OrderDescNullsLast, nil
	default:
		return OrderAsc, fmt.Errorf("valid values include asc, desc, asc_nulls_first and desc_nulls_first")
	}
}
