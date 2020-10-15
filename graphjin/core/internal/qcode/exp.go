package qcode

import (
	"errors"
	"fmt"

	"github.com/dosco/super-graph/core/internal/graph"
	"github.com/dosco/super-graph/core/internal/sdata"
	"github.com/dosco/super-graph/core/internal/util"
)

func (co *Compiler) compileArgObj(ti sdata.DBTableInfo, st *util.StackInf, arg *graph.Arg) (*Exp, bool, error) {
	if arg.Val.Type != graph.NodeObj {
		return nil, false, fmt.Errorf("expecting an object")
	}

	return co.compileArgNode(ti, st, arg.Val, true)
}

type aexp struct {
	exp  *Exp
	node *graph.Node
}

func (co *Compiler) compileArgNode(
	ti sdata.DBTableInfo,
	st *util.StackInf,
	node *graph.Node,
	usePool bool) (*Exp, bool, error) {

	var root *Exp
	var needsUser bool

	if node == nil || len(node.Children) == 0 {
		return nil, false, errors.New("invalid argument value")
	}

	pushChild(st, nil, node)

	for {
		if st.Len() == 0 {
			break
		}

		intf := st.Pop()

		av, ok := intf.(aexp)
		if !ok {
			return nil, needsUser, fmt.Errorf("16: unexpected value %v (%t)", intf, intf)
		}

		// Objects inside a list
		if av.node.Name == "" {
			pushChildren(st, av.exp, av.node)
			continue
		}

		ex, err := newExp(ti, st, av, usePool)
		if err != nil {
			return nil, needsUser, err
		}

		if ex == nil {
			continue
		}

		if ex.Type == ValVar && ex.Val == "user_id" {
			needsUser = true
		}

		if av.exp == nil {
			root = ex
		} else {
			av.exp.Children = append(av.exp.Children, ex)
		}
	}

	return root, needsUser, nil
}

func newExp(ti sdata.DBTableInfo, st *util.StackInf, av aexp, usePool bool) (*Exp, error) {
	node := av.node
	name := node.Name

	if name[0] == '_' {
		name = name[1:]
	}

	var ex *Exp

	if usePool {
		ex = expPool.Get().(*Exp)
		ex.Reset()
	} else {
		ex = &Exp{doFree: false, internal: true}
	}

	ex.Children = ex.childrenA[:0]

	switch name {
	case "and":
		if len(node.Children) == 0 {
			return nil, errors.New("missing expression after 'AND' operator")
		}
		ex.Op = OpAnd
		pushChildren(st, ex, node)
	case "or":
		if len(node.Children) == 0 {
			return nil, errors.New("missing expression after 'OR' operator")
		}
		ex.Op = OpOr
		pushChildren(st, ex, node)
	case "not":
		if len(node.Children) == 0 {
			return nil, errors.New("missing expression after 'NOT' operator")
		}
		ex.Op = OpNot
		pushChild(st, ex, node)
	case "eq", "equals":
		ex.Op = OpEquals
		ex.Val = node.Val
	case "neq", "not_equals":
		ex.Op = OpNotEquals
		ex.Val = node.Val
	case "gt", "greater_than":
		ex.Op = OpGreaterThan
		ex.Val = node.Val
	case "lt", "lesser_than":
		ex.Op = OpLesserThan
		ex.Val = node.Val
	case "gte", "greater_or_equals":
		ex.Op = OpGreaterOrEquals
		ex.Val = node.Val
	case "lte", "lesser_or_equals":
		ex.Op = OpLesserOrEquals
		ex.Val = node.Val
	case "in":
		ex.Op = OpIn
		setListVal(ex, node)
	case "nin", "not_in":
		ex.Op = OpNotIn
		setListVal(ex, node)
	case "like":
		ex.Op = OpLike
		ex.Val = node.Val
	case "nlike", "not_like":
		ex.Op = OpNotLike
		ex.Val = node.Val
	case "ilike":
		ex.Op = OpILike
		ex.Val = node.Val
	case "nilike", "not_ilike":
		ex.Op = OpNotILike
		ex.Val = node.Val
	case "similar":
		ex.Op = OpSimilar
		ex.Val = node.Val
	case "nsimilar", "not_similar":
		ex.Op = OpNotSimilar
		ex.Val = node.Val
	case "regex":
		ex.Op = OpRegex
		ex.Val = node.Val
	case "nregex", "not_regex":
		ex.Op = OpNotRegex
		ex.Val = node.Val
	case "iregex":
		ex.Op = OpIRegex
		ex.Val = node.Val
	case "niregex", "not_iregex":
		ex.Op = OpNotIRegex
		ex.Val = node.Val
	case "contains":
		ex.Op = OpContains
		ex.Val = node.Val
	case "contained_in":
		ex.Op = OpContainedIn
		ex.Val = node.Val
	case "has_key":
		ex.Op = OpHasKey
		ex.Val = node.Val
	case "has_key_any":
		ex.Op = OpHasKeyAny
		ex.Val = node.Val
	case "has_key_all":
		ex.Op = OpHasKeyAll
		ex.Val = node.Val
	case "is_null":
		ex.Op = OpIsNull
		ex.Val = node.Val
	case "null_eq", "ndis", "not_distinct":
		ex.Op = OpNotDistinct
		ex.Val = node.Val
	case "null_neq", "dis", "distinct":
		ex.Op = OpDistinct
		ex.Val = node.Val
	default:
		if len(node.Children) == 0 {
			return nil, fmt.Errorf("[Where] invalid operation: %s", name)
		}
		pushChildren(st, av.exp, node)
		return nil, nil // skip node
	}

	if ex.Op != OpAnd && ex.Op != OpOr && ex.Op != OpNot {
		switch node.Type {
		case graph.NodeStr:
			ex.Type = ValStr
		case graph.NodeNum:
			ex.Type = ValNum
		case graph.NodeBool:
			ex.Type = ValBool
		case graph.NodeList:
			ex.Type = ValList
		case graph.NodeVar:
			ex.Type = ValVar
		default:
			return nil, fmt.Errorf("[Where] invalid values for: %s", name)
		}

		if err := setWhereColName(ti, ex, node); err != nil {
			return nil, err
		}
	}

	return ex, nil
}

func setListVal(ex *Exp, node *graph.Node) {
	if len(node.Children) != 0 {
		switch node.Children[0].Type {
		case graph.NodeStr:
			ex.ListType = ValStr
		case graph.NodeNum:
			ex.ListType = ValNum
		case graph.NodeBool:
			ex.ListType = ValBool
		}
	} else {
		ex.Val = node.Val
		return
	}

	for i := range node.Children {
		ex.ListVal = append(ex.ListVal, node.Children[i].Val)
	}

}

func setWhereColName(ti sdata.DBTableInfo, ex *Exp, node *graph.Node) error {
	var list []string

	for n := node.Parent; n != nil; n = n.Parent {
		if n.Type != graph.NodeObj {
			continue
		}
		if n.Name != "" {
			k := n.Name
			if k == "and" || k == "or" || k == "not" ||
				k == "_and" || k == "_or" || k == "_not" {
				continue
			}
			list = append([]string{k}, list...)
		}
	}

	switch len(list) {
	case 0:
		return fmt.Errorf("invalid where clause")

	case 1:
		if col, err := ti.GetColumn(list[0]); err == nil {
			ex.Col = col
		} else {
			return err
		}

	default:
		prev := ti.Name
		for i := 0; i < len(list)-1; i++ {
			if rel, err := ti.Schema.GetRel(list[i], prev, ""); err == nil {
				ex.Rels = append(ex.Rels, rel)
				prev = list[i]
			} else {
				return err
			}
		}
		rel := ex.Rels[len(ex.Rels)-1]
		if col, err := rel.Left.Ti.GetColumn(list[len(list)-1]); err == nil {
			ex.Col = col
		} else {
			return err
		}
	}

	return nil
}

func pushChildren(st *util.StackInf, exp *Exp, node *graph.Node) {
	for i := range node.Children {
		st.Push(aexp{exp: exp, node: node.Children[i]})
	}
}

func pushChild(st *util.StackInf, exp *Exp, node *graph.Node) {
	st.Push(aexp{exp: exp, node: node.Children[0]})
}
