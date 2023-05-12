package qcode

import (
	"errors"
	"fmt"

	"github.com/dosco/graphjin/core/v3/internal/graph"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
	"github.com/dosco/graphjin/core/v3/internal/util"
)

type aexpst struct {
	co       *Compiler
	st       *util.StackInf
	ti       sdata.DBTable
	edge     string
	savePath bool
}

type aexp struct {
	exp  *Exp
	ti   sdata.DBTable
	node *graph.Node
	path []string
}

func (co *Compiler) compileBaseExpNode(edge string,
	ti sdata.DBTable,
	st *util.StackInf,
	node *graph.Node,
	savePath bool,
) (*Exp, bool, error) {
	return co.compileExpNode(edge, ti, st, node, savePath, -1)
}

func (co *Compiler) compileExpNode(
	edge string,
	ti sdata.DBTable,
	st *util.StackInf,
	node *graph.Node,
	savePath bool,
	selID int32,
) (*Exp, bool, error) {
	if node == nil || len(node.Children) == 0 {
		return nil, false, errors.New("invalid argument value")
	}

	needsUser := false

	ast := &aexpst{
		co:       co,
		st:       st,
		ti:       ti,
		edge:     edge,
		savePath: savePath,
	}

	var root *Exp

	st.Push(aexp{
		ti:   ti,
		node: node,
	})

	for {
		if st.Len() == 0 {
			break
		}

		intf := st.Pop()

		av, ok := intf.(aexp)
		if !ok {
			return nil, needsUser, fmt.Errorf("16: unexpected value %v (%t)", intf, intf)
		}

		ex, err := ast.parseNode(av, av.node, selID)
		if err != nil {
			return nil, needsUser, err
		}

		if ex == nil {
			continue
		}

		if ex.Right.ValType == ValVar {
			v := ex.Right.Val
			needsUser = (v == "user_id" || v == "userID" || v == "userId" ||
				v == "user_id_raw" || v == "userIDRaw" || v == "userIdRaw" ||
				v == "user_id_provider" || v == "userIDProvider" || v == "userIdProvider")
		}

		switch {
		case root == nil:
			root = ex
		case av.exp == nil:
			tmp := root
			root = newExpOp(OpAnd)
			root.Children = []*Exp{tmp, ex}
		default:
			av.exp.Children = append(av.exp.Children, ex)
		}
	}

	return root, needsUser, nil
}

func newExp() *Exp {
	ex := &Exp{Op: OpNop}
	ex.Left.ID = -1
	ex.Right.ID = -1
	ex.Children = ex.childrenA[:0]
	return ex
}

func newExpOp(op ExpOp) *Exp {
	ex := newExp()
	ex.Op = op
	return ex
}

func (ast *aexpst) parseNode(av aexp, node *graph.Node, selID int32) (*Exp, error) {
	var ex *Exp
	var err error

	name := node.Name

	if name == "" {
		ast.pushChildren(av, av.exp, av.node)
		return nil, nil
	}

	switch {
	case av.exp == nil:
		ex = newExp()
	case av.exp.Op != OpNop:
		ex = newExp()
	default:
		ex = av.exp
	}

	// Objects inside a list

	if ast.savePath {
		ex.Right.Path = append(av.path, node.Name)
	}

	if ok, err := ast.processBoolOps(av, ex, node, nil); err != nil {
		return nil, err
	} else if ok {
		return ex, nil
	}

	switch node.Type {
	// { column: { op: value } }
	case graph.NodeObj:
		if len(node.Children) != 1 {
			return nil, fmt.Errorf("[Where] invalid operation: %s", name)
		}

		if ok, err := ast.processNestedTable(av, ex, node); err != nil {
			return nil, err
		} else if ok {
			return ex, nil
		}

		// TODO: Make this function work with schemas
		if _, err := ast.processColumn(av, ex, node, selID); err != nil {
			return nil, err
		}
		vn := node.Children[0]

		if ok, err := ast.processOpAndVal(av, ex, vn); err != nil {
			return nil, err
		} else if !ok {
			if ok, err := ast.processBoolOps(av, ex, vn, node); err != nil {
				return nil, err
			} else if ok {
				return ex, nil
			}
			return nil, fmt.Errorf("[Where] unknown operator: %s", name)
		}

		if ast.savePath {
			ex.Right.Path = append(ex.Right.Path, vn.Name)
		}

		if ex.Right.ValType, err = getExpType(vn); err != nil {
			return nil, err
		}

	// { column: [value1, value2, value3] }
	case graph.NodeList:
		if len(node.Children) == 0 {
			return nil, fmt.Errorf("[Where] invalid empty list: %s", name)
		}
		if _, err := ast.processColumn(av, ex, node, selID); err != nil {
			return nil, err
		}
		setListVal(ex, node)
		if ex.Left.Col.Array {
			ex.Op = OpHasInCommon
		} else {
			ex.Op = OpIn
		}

	// { column: value }
	default:
		if _, err := ast.processColumn(av, ex, node, selID); err != nil {
			return nil, err
		}
		if ex.Left.Col.Array {
			ex.Op = OpHasInCommon
			setListVal(ex, node)
		} else {
			if ex.Right.ValType, err = getExpType(node); err != nil {
				return nil, err
			}
			ex.Op = OpEquals
			ex.Right.Val = node.Val
		}
	}

	return ex, nil
}

func (ast *aexpst) processBoolOps(av aexp, ex *Exp, node, anode *graph.Node) (bool, error) {
	var name string

	if node.Name != "" && node.Name[0] == '_' {
		name = node.Name[1:]
	} else {
		name = node.Name
	}

	// insert attach nodes between the current node and its children
	if anode != nil {
		n := *node
		for i := range n.Children {
			an := *anode
			v := n.Children[i]
			if v.Name == "" && len(v.Children) != 0 {
				an.Children = []*graph.Node{v.Children[0]}
			} else {
				an.Children = []*graph.Node{v}
			}
			n.Children[i] = &an
		}
		node = &n
	}

	switch name {
	case "and":
		if len(node.Children) == 0 {
			return false, errors.New("missing expression after 'and' operator")
		}
		if len(node.Children) == 1 {
			return false, fmt.Errorf("expression does not need an 'and' operator: %s",
				av.ti.Name)
		}
		ex.Op = OpAnd
		ast.pushChildren(av, ex, node)
		return true, nil

	case "or":
		if len(node.Children) == 0 {
			return false, errors.New("missing expression after 'OR' operator")
		}
		if len(node.Children) == 1 {
			return false, fmt.Errorf("expression does not need an 'or' operator: %s",
				av.ti.Name)
		}
		ex.Op = OpOr
		ast.pushChildren(av, ex, node)
		return true, nil

	case "not":
		if len(node.Children) == 0 {
			return false, errors.New("missing expression after 'not' operator")
		}
		ex.Op = OpNot
		ast.pushChildren(av, ex, node)
		return true, nil
	}
	return false, nil
}

func (ast *aexpst) processOpAndVal(av aexp, ex *Exp, node *graph.Node) (bool, error) {
	var name string

	if node.Name != "" && node.Name[0] == '_' {
		name = node.Name[1:]
	} else {
		name = node.Name
	}

	switch name {
	case "eq", "equals":
		ex.Op = OpEquals
		ex.Right.Val = node.Val
	case "neq", "notEquals", "not_equals":
		ex.Op = OpNotEquals
		ex.Right.Val = node.Val
	case "gt", "greaterThan", "greater_than":
		ex.Op = OpGreaterThan
		ex.Right.Val = node.Val
	case "lt", "lesserThan", "lesser_than":
		ex.Op = OpLesserThan
		ex.Right.Val = node.Val
	case "gte", "gteq", "greaterOrEquals", "greater_or_equals":
		ex.Op = OpGreaterOrEquals
		ex.Right.Val = node.Val
	case "lte", "lteq", "lesserOrEquals", "lesser_or_equals":
		ex.Op = OpLesserOrEquals
		ex.Right.Val = node.Val
	case "in":
		if ex.Left.Col.Array {
			ex.Op = OpHasInCommon
		} else {
			ex.Op = OpIn
		}
		setListVal(ex, node)
	case "nin", "notIn", "not_in":
		ex.Op = OpNotIn
		setListVal(ex, node)
	case "like":
		ex.Op = OpLike
		ex.Right.Val = node.Val
	case "nlike", "notLike", "not_like":
		ex.Op = OpNotLike
		ex.Right.Val = node.Val
	case "ilike", "iLike":
		ex.Op = OpILike
		ex.Right.Val = node.Val
	case "nilike", "notILike", "not_ilike":
		ex.Op = OpNotILike
		ex.Right.Val = node.Val
	case "similar":
		ex.Op = OpSimilar
		ex.Right.Val = node.Val
	case "nsimilar", "notSimiliar", "not_similar":
		ex.Op = OpNotSimilar
		ex.Right.Val = node.Val
	case "regex":
		ex.Op = OpRegex
		ex.Right.Val = node.Val
	case "nregex", "notRegex", "not_regex":
		ex.Op = OpNotRegex
		ex.Right.Val = node.Val
	case "iregex":
		ex.Op = OpIRegex
		ex.Right.Val = node.Val
	case "niregex", "notIRegex", "not_iregex":
		ex.Op = OpNotIRegex
		ex.Right.Val = node.Val
	case "contains":
		ex.Op = OpContains
		setListVal(ex, node)
	case "containedIn", "contained_in":
		ex.Op = OpContainedIn
		setListVal(ex, node)
	case "hasInCommon", "has_in_common":
		ex.Op = OpHasInCommon
		setListVal(ex, node)
	case "hasKey", "has_key":
		ex.Op = OpHasKey
		ex.Right.Val = node.Val
	case "hasKeyAny", "has_key_any":
		ex.Op = OpHasKeyAny
		setListVal(ex, node)
	case "hasKeyAll", "has_key_all":
		ex.Op = OpHasKeyAll
		setListVal(ex, node)
	case "isNull", "is_null":
		ex.Op = OpIsNull
		ex.Right.Val = node.Val
	case "notDistinct", "ndis", "not_distinct":
		ex.Op = OpNotDistinct
		ex.Right.Val = node.Val
	case "dis", "distinct":
		ex.Op = OpDistinct
		ex.Right.Val = node.Val
	default:
		return false, nil
	}

	return true, nil
}

func getExpType(node *graph.Node) (ValType, error) {
	switch node.Type {
	case graph.NodeStr:
		return ValStr, nil
	case graph.NodeNum:
		return ValNum, nil
	case graph.NodeBool:
		return ValBool, nil
	case graph.NodeList:
		return ValList, nil
	case graph.NodeVar:
		return ValVar, nil
	default:
		return -1, fmt.Errorf("[Where] invalid values for: %s", node.Name)
	}
}

func setListVal(ex *Exp, node *graph.Node) {
	var t graph.ParserType

	if len(node.Children) != 0 {
		t = node.Children[0].Type
	} else {
		t = node.Type
	}

	switch t {
	case graph.NodeStr:
		ex.Right.ListType = ValStr
	case graph.NodeNum:
		ex.Right.ListType = ValNum
	case graph.NodeBool:
		ex.Right.ListType = ValBool
	default:
		ex.Right.Val = node.Val
		return
	}

	for i := range node.Children {
		ex.Right.ValType = ValList
		ex.Right.ListVal = append(ex.Right.ListVal, node.Children[i].Val)
	}

	if len(node.Children) == 0 {
		ex.Right.ValType = ValList
		ex.Right.ListVal = append(ex.Right.ListVal, node.Val)
	}
}

func (ast *aexpst) processColumn(av aexp, ex *Exp, node *graph.Node, selID int32) (bool, error) {
	nn := ast.co.ParseName(node.Name)

	col, err := av.ti.GetColumn(nn)
	if err != nil {
		return false, err
	}
	ex.Left.ID = selID
	ex.Left.Col = col
	return true, err
}

func (ast *aexpst) processNestedTable(av aexp, ex *Exp, node *graph.Node) (bool, error) {
	var joins []Join
	var err error

	ti := av.ti

	var prev, curr string
	if ast.edge == "" {
		prev = ti.Name
	} else {
		prev = ast.edge
	}

	var n, ln *graph.Node
	for n = node; ; {
		if len(n.Children) != 1 {
			break
		}
		k := n.Name
		if k == "" || k == "and" || k == "or" || k == "not" ||
			k == "_and" || k == "_or" || k == "_not" {
			break
		}
		curr = ast.co.ParseName(k)

		if curr == ti.Name {
			continue
			// return fmt.Errorf("selector table not allowed in where: %s", ti.Name)
		}

		var path []sdata.TPath
		// TODO: Make this function work with schemas
		if path, err = ast.co.FindPath(curr, prev, ""); err != nil {
			break
		}

		for i := len(path) - 1; i >= 0; i-- {
			rel := sdata.PathToRel(path[i])
			joins = append(joins, Join{
				Rel:    rel,
				Filter: buildFilter(rel, -1),
			})
		}

		prev = curr
		ln = n
		n = n.Children[0]
	}

	if len(joins) != 0 {
		ex.Op = OpSelectExists
		ex.Joins = joins
		ast.pushChildren(av, ex, ln)
		return true, nil
	}
	return false, nil
}

func (ast *aexpst) pushChildren(av aexp, ex *Exp, node *graph.Node) {
	var path []string
	var ti sdata.DBTable

	if ast.savePath && node.Name != "" {
		if av.exp != nil {
			path = append(av.exp.Right.Path, node.Name)
		} else {
			path = append(path, node.Name)
		}
	}

	// TODO: Remove ex from av (aexp)
	if ex != nil && len(ex.Joins) != 0 {
		ti = ex.Joins[len(ex.Joins)-1].Rel.Left.Ti
	} else {
		ti = av.ti
	}

	for i := range node.Children {
		ast.st.Push(aexp{
			exp:  ex,
			ti:   ti,
			node: node.Children[i],
			path: path,
		})
	}
}
