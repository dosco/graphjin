package qcode

import (
	"errors"
	"fmt"

	"github.com/dosco/graphjin/core/internal/graph"
	"github.com/dosco/graphjin/core/internal/sdata"
	"github.com/dosco/graphjin/core/internal/util"
)

func (co *Compiler) compileArgObj(edge string, ti sdata.DBTable, st *util.StackInf, arg *graph.Arg) (*Exp, bool, error) {
	if arg.Val.Type != graph.NodeObj {
		return nil, false, fmt.Errorf("expecting an object")
	}

	return co.compileArgNode(edge, ti, st, arg.Val, false)
}

type aexpst struct {
	co       *Compiler
	st       *util.StackInf
	ti       sdata.DBTable
	edge     string
	savePath bool
}

type aexp struct {
	exp  *Exp
	node *graph.Node
	path []string
}

func (co *Compiler) compileArgNode(
	edge string,
	ti sdata.DBTable,
	st *util.StackInf,
	node *graph.Node,
	savePath bool) (*Exp, bool, error) {

	var root *Exp
	var needsUser bool

	if node == nil || len(node.Children) == 0 {
		return nil, false, errors.New("invalid argument value")
	}

	ast := &aexpst{co: co,
		st:       st,
		ti:       ti,
		edge:     edge,
		savePath: savePath,
	}
	ast.pushChildren(nil, node)

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
			ast.pushChildren(av.exp, av.node)
			continue
		}

		ex, err := ast.parseNode(av)
		if err != nil {
			return nil, needsUser, err
		}

		if ex == nil {
			continue
		}

		if ex.Right.ValType == ValVar &&
			(ex.Right.Val == "user_id" ||
				ex.Right.Val == "user_id_raw" ||
				ex.Right.Val == "user_id_provider") {
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

func (ast *aexpst) parseNode(av aexp) (*Exp, error) {
	var err error

	node := av.node
	name := node.Name

	if name[0] == '_' {
		name = name[1:]
	}

	ex := newExp()
	if ast.savePath {
		ex.Right.Path = append(av.path, node.Name)
	}

	guess := false

	switch name {
	case "and":
		if len(node.Children) == 0 {
			return nil, errors.New("missing expression after 'AND' operator")
		}
		ex.Op = OpAnd
		ast.pushChildren(ex, node)
	case "or":
		if len(node.Children) == 0 {
			return nil, errors.New("missing expression after 'OR' operator")
		}
		ex.Op = OpOr
		ast.pushChildren(ex, node)
	case "not":
		if len(node.Children) == 0 {
			return nil, errors.New("missing expression after 'NOT' operator")
		}
		ex.Op = OpNot
		ast.pushChildren(ex, node)
	case "eq", "equals":
		ex.Op = OpEquals
		ex.Right.Val = node.Val
	case "neq", "not_equals":
		ex.Op = OpNotEquals
		ex.Right.Val = node.Val
	case "gt", "greater_than":
		ex.Op = OpGreaterThan
		ex.Right.Val = node.Val
	case "lt", "lesser_than":
		ex.Op = OpLesserThan
		ex.Right.Val = node.Val
	case "gte", "gteq", "greater_or_equals":
		ex.Op = OpGreaterOrEquals
		ex.Right.Val = node.Val
	case "lte", "lteq", "lesser_or_equals":
		ex.Op = OpLesserOrEquals
		ex.Right.Val = node.Val
	case "in":
		ex.Op = OpIn
		setListVal(ex, node)
	case "nin", "not_in":
		ex.Op = OpNotIn
		setListVal(ex, node)
	case "like":
		ex.Op = OpLike
		ex.Right.Val = node.Val
	case "nlike", "not_like":
		ex.Op = OpNotLike
		ex.Right.Val = node.Val
	case "ilike":
		ex.Op = OpILike
		ex.Right.Val = node.Val
	case "nilike", "not_ilike":
		ex.Op = OpNotILike
		ex.Right.Val = node.Val
	case "similar":
		ex.Op = OpSimilar
		ex.Right.Val = node.Val
	case "nsimilar", "not_similar":
		ex.Op = OpNotSimilar
		ex.Right.Val = node.Val
	case "regex":
		ex.Op = OpRegex
		ex.Right.Val = node.Val
	case "nregex", "not_regex":
		ex.Op = OpNotRegex
		ex.Right.Val = node.Val
	case "iregex":
		ex.Op = OpIRegex
		ex.Right.Val = node.Val
	case "niregex", "not_iregex":
		ex.Op = OpNotIRegex
		ex.Right.Val = node.Val
	case "contains":
		ex.Op = OpContains
		setListVal(ex, node)
	case "contained_in":
		ex.Op = OpContainedIn
		setListVal(ex, node)
	case "has_key":
		ex.Op = OpHasKey
		ex.Right.Val = node.Val
	case "has_key_any":
		ex.Op = OpHasKeyAny
		ex.Right.Val = node.Val
	case "has_key_all":
		ex.Op = OpHasKeyAll
		ex.Right.Val = node.Val
	case "is_null":
		ex.Op = OpIsNull
		ex.Right.Val = node.Val
	case "null_eq", "ndis", "not_distinct":
		ex.Op = OpNotDistinct
		ex.Right.Val = node.Val
	case "null_neq", "dis", "distinct":
		ex.Op = OpDistinct
		ex.Right.Val = node.Val
	default:
		if node.Type == graph.NodeObj {
			if len(node.Children) == 0 {
				return nil, fmt.Errorf("[Where] invalid operation: %s", name)
			}
			ast.pushChildren(av.exp, node)
			return nil, nil // skip node
		}

		// Support existing { column: <value> } format
		switch node.Type {
		case graph.NodeList:
			ex.Op = OpIn
			setListVal(ex, node)
		default:
			ex.Op = OpEquals
			ex.Right.Val = node.Val
		}
		guess = true
	}

	if ex.Op != OpAnd && ex.Op != OpOr && ex.Op != OpNot {
		if ex.Right.ValType != ValList {
			if ex.Right.ValType, err = getExpType(node); err != nil {
				return nil, err
			}
		}
		if err := ast.setExpColName(ex, node); err != nil {
			return nil, err
		}
	}

	if guess && ex.Left.Col.Array {
		ex.Op = OpContains
		setListVal(ex, node)
	}

	return ex, nil
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
		return ValNone, fmt.Errorf("[Where] invalid values for: %s", node.Name)
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
		ex.Right.ListVal = append(ex.Right.ListVal, node.Children[i].Val)
	}

	if len(node.Children) == 0 {
		ex.Right.ValType = ValList
		ex.Right.ListVal = append(ex.Right.ListVal, node.Val)
	}
}

func (ast *aexpst) setExpColName(ex *Exp, node *graph.Node) error {
	var list []string
	var err error

	s := ast.co.s
	ti := ast.ti

	for n := node; n != nil; n = n.Parent {
		// if n.Type != graph.NodeObj {
		// 	continue
		// }
		if n.Name != "" {
			k := n.Name
			if k == "and" || k == "or" || k == "not" ||
				k == "_and" || k == "_or" || k == "_not" {
				continue
			}
			list = append([]string{k}, list...)
		}
	}

	var nn string

	switch len(list) {
	case 1:
		if ast.co.c.EnableCamelcase {
			nn = util.ToSnake(node.Name)
		} else {
			nn = node.Name
		}
		if col, err := ti.GetColumn(nn); err == nil {
			ex.Left.Col = col
		} else {
			return err
		}

	case 2:
		if ast.co.c.EnableCamelcase {
			nn = util.ToSnake(list[0])
		} else {
			nn = list[0]
		}
		if col, err := ti.GetColumn(nn); err == nil {
			ex.Left.Col = col
			return nil
		}
		fallthrough

	default:
		var prev, curr string
		if ast.edge == "" {
			prev = ti.Name
		} else {
			prev = ast.edge
		}

		for i := 0; i < len(list)-1; i++ {
			if ast.co.c.EnableCamelcase {
				curr = util.ToSnake(list[i])
			} else {
				curr = list[i]
			}

			if curr == ti.Name {
				continue
				// return fmt.Errorf("selector table not allowed in where: %s", ti.Name)
			}

			var paths []sdata.TPath
			paths, err = s.FindPath(curr, prev, "")
			if err == nil {
				rel := sdata.PathToRel(paths[0])
				ex.Joins = append(ex.Joins, Join{Rel: rel, Filter: buildFilter(rel, -1)})
				prev = curr
			} else {
				break
			}

			// return graphError(err, curr, prev, "")
		}

		if len(ex.Joins) == 0 {
			return graphError(err, curr, prev, "")
		}

		join := ex.Joins[len(ex.Joins)-1]

		for i := len(list) - 1; i > 0; i-- {
			var col sdata.DBColumn
			cn := list[i]

			if col, err = join.Rel.Left.Ti.GetColumn(cn); err == nil {
				ex.Left.Col = col
				break
			}
		}
	}

	return err
}

func (ast *aexpst) pushChildren(exp *Exp, node *graph.Node) {
	var path []string

	if ast.savePath && node.Name != "" {
		if exp != nil {
			path = append(exp.Right.Path, node.Name)
		} else {
			path = append(path, node.Name)
		}
	}

	for i := range node.Children {
		ast.st.Push(aexp{exp: exp, node: node.Children[i], path: path})
	}
}
