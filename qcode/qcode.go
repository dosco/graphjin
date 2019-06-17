package qcode

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/dosco/super-graph/util"
	"github.com/gobuffalo/flect"
)

const (
	maxSelectors = 30
)

type QCode struct {
	Query *Query
}

type Query struct {
	Selects []Select
}

type Column struct {
	Table     string
	Name      string
	FieldName string
}

type Select struct {
	ID         int32
	ParentID   int32
	Args       map[string]*Node
	Table      string
	FieldName  string
	Cols       []Column
	Where      *Exp
	OrderBy    []*OrderBy
	DistinctOn []string
	Paging     Paging
	Children   []int32
}

type Exp struct {
	Op        ExpOp
	Col       string
	NestedCol bool
	Type      ValType
	Val       string
	ListType  ValType
	ListVal   []string
	Children  []*Exp
	childrenA [5]*Exp
	doFree    bool
}

var zeroExp = Exp{doFree: true}

func (ex *Exp) Reset() {
	*ex = zeroExp
}

type OrderBy struct {
	Col   string
	Order Order
}

type Paging struct {
	Limit  string
	Offset string
}

type ExpOp int

const (
	OpNop ExpOp = iota
	OpAnd
	OpOr
	OpNot
	OpEquals
	OpNotEquals
	OpGreaterOrEquals
	OpLesserOrEquals
	OpGreaterThan
	OpLesserThan
	OpIn
	OpNotIn
	OpLike
	OpNotLike
	OpILike
	OpNotILike
	OpSimilar
	OpNotSimilar
	OpContains
	OpContainedIn
	OpHasKey
	OpHasKeyAny
	OpHasKeyAll
	OpIsNull
	OpEqID
	OpTsQuery
)

type ValType int

const (
	ValStr ValType = iota + 1
	ValInt
	ValFloat
	ValBool
	ValList
	ValVar
	ValNone
)

type AggregrateOp int

const (
	AgCount AggregrateOp = iota + 1
	AgSum
	AgAvg
	AgMax
	AgMin
)

type Order int

const (
	OrderAsc Order = iota + 1
	OrderDesc
	OrderAscNullsFirst
	OrderAscNullsLast
	OrderDescNullsFirst
	OrderDescNullsLast
)

type Config struct {
	DefaultFilter []string
	FilterMap     map[string][]string
	Blacklist     []string
	KeepArgs      bool
}

type Compiler struct {
	fl *Exp
	fm map[string]*Exp
	bl map[string]struct{}
	ka bool
}

var expPool = sync.Pool{
	New: func() interface{} { return new(Exp) },
}

func NewCompiler(c Config) (*Compiler, error) {
	bl := make(map[string]struct{}, len(c.Blacklist))

	for i := range c.Blacklist {
		bl[c.Blacklist[i]] = struct{}{}
	}

	fl, err := compileFilter(c.DefaultFilter)
	if err != nil {
		return nil, err
	}

	fm := make(map[string]*Exp, len(c.FilterMap))

	for k, v := range c.FilterMap {
		fil, err := compileFilter(v)
		if err != nil {
			return nil, err
		}
		singular := flect.Singularize(k)
		plural := flect.Pluralize(k)

		fm[singular] = fil
		fm[plural] = fil
	}

	seedExp := [100]Exp{}
	for i := range seedExp {
		expPool.Put(&seedExp[i])
	}

	return &Compiler{fl, fm, bl, c.KeepArgs}, nil
}

func (com *Compiler) Compile(query []byte) (*QCode, error) {
	var qc QCode
	var err error

	op, err := Parse(query)
	if err != nil {
		return nil, err
	}

	switch op.Type {
	case opQuery:
		qc.Query, err = com.compileQuery(op)
	case opMutate:
	case opSub:
	default:
		err = fmt.Errorf("Unknown operation type %d", op.Type)
	}

	if err != nil {
		return nil, err
	}

	opPool.Put(op)

	return &qc, nil
}

func (com *Compiler) CompileQuery(query []byte) (*QCode, error) {
	var err error

	op, err := ParseQuery(query)
	if err != nil {
		return nil, err
	}

	qc := &QCode{}
	qc.Query, err = com.compileQuery(op)
	opPool.Put(op)

	if err != nil {
		return nil, err
	}

	return qc, nil
}

func (com *Compiler) compileQuery(op *Operation) (*Query, error) {
	id := int32(0)
	parentID := int32(0)

	selects := make([]Select, 0, 5)
	st := NewStack()

	if len(op.Fields) == 0 {
		return nil, errors.New("empty query")
	}
	st.Push(op.Fields[0].ID)

	for {
		if st.Len() == 0 {
			break
		}

		if id >= maxSelectors {
			return nil, fmt.Errorf("selector limit reached (%d)", maxSelectors)
		}

		fid := st.Pop()
		field := &op.Fields[fid]

		if _, ok := com.bl[field.Name]; ok {
			continue
		}

		selects = append(selects, Select{
			ID:       id,
			ParentID: parentID,
			Table:    field.Name,
			Children: make([]int32, 0, 5),
		})
		s := &selects[(len(selects) - 1)]

		if s.ID != 0 {
			p := &selects[s.ParentID]
			p.Children = append(p.Children, s.ID)
		}

		if len(field.Alias) != 0 {
			s.FieldName = field.Alias
		} else {
			s.FieldName = s.Table
		}

		err := com.compileArgs(s, field.Args)
		if err != nil {
			return nil, err
		}

		s.Cols = make([]Column, 0, len(field.Children))

		for _, cid := range field.Children {
			f := op.Fields[cid]

			if _, ok := com.bl[f.Name]; ok {
				continue
			}

			if len(f.Children) != 0 {
				parentID = s.ID
				st.Push(f.ID)
				continue
			}

			col := Column{Name: f.Name}

			if len(f.Alias) != 0 {
				col.FieldName = f.Alias
			} else {
				col.FieldName = f.Name
			}
			s.Cols = append(s.Cols, col)
		}

		id++
	}

	var ok bool
	var fil *Exp

	if id > 0 {
		root := &selects[0]
		fil, ok = com.fm[root.Table]

		if !ok || fil == nil {
			fil = com.fl
		}

		if fil != nil && fil.Op != OpNop {

			if root.Where != nil {
				ow := root.Where

				root.Where = expPool.Get().(*Exp)
				root.Where.Reset()
				root.Where.Op = OpAnd
				root.Where.Children = root.Where.childrenA[:2]
				root.Where.Children[0] = fil
				root.Where.Children[1] = ow
			} else {
				root.Where = fil
			}
		}

	} else {
		return nil, errors.New("invalid query")
	}

	return &Query{selects[:id]}, nil
}

func (com *Compiler) compileArgs(sel *Select, args []Arg) error {
	var err error

	if com.ka {
		sel.Args = make(map[string]*Node, len(args))
	}

	for i := range args {
		arg := &args[i]

		switch arg.Name {
		case "id":
			if sel.ID == 0 {
				err = com.compileArgID(sel, arg)
			}
		case "search":
			err = com.compileArgSearch(sel, arg)
		case "where":
			err = com.compileArgWhere(sel, arg)
		case "orderby", "order_by", "order":
			err = com.compileArgOrderBy(sel, arg)
		case "distinct_on", "distinct":
			err = com.compileArgDistinctOn(sel, arg)
		case "limit":
			err = com.compileArgLimit(sel, arg)
		case "offset":
			err = com.compileArgOffset(sel, arg)
		}

		if err != nil {
			return err
		}

		if sel.Args != nil {
			sel.Args[arg.Name] = arg.Val
		} else {
			nodePool.Put(arg.Val)
		}
	}

	return nil
}

func (com *Compiler) compileArgObj(st *util.Stack, arg *Arg) (*Exp, error) {
	if arg.Val.Type != nodeObj {
		return nil, fmt.Errorf("expecting an object")
	}

	return com.compileArgNode(st, arg.Val, true)
}

func (com *Compiler) compileArgNode(st *util.Stack, node *Node, usePool bool) (*Exp, error) {
	var root *Exp

	if node == nil || len(node.Children) == 0 {
		return nil, errors.New("invalid argument value")
	}

	pushChild(st, nil, node)

	for {
		if st.Len() == 0 {
			break
		}

		intf := st.Pop()
		node, ok := intf.(*Node)
		if !ok || node == nil {
			return nil, fmt.Errorf("16: unexpected value %v (%t)", intf, intf)
		}

		// Objects inside a list
		if len(node.Name) == 0 {
			pushChildren(st, node.exp, node)
			continue

		} else {
			if _, ok := com.bl[node.Name]; ok {
				continue
			}
		}

		ex, err := newExp(st, node, usePool)
		if err != nil {
			return nil, err
		}

		if ex == nil {
			continue
		}

		if node.exp == nil {
			root = ex
		} else {
			node.exp.Children = append(node.exp.Children, ex)
		}

	}

	if com.ka {
		return root, nil
	}

	pushChild(st, nil, node)

	for {
		if st.Len() == 0 {
			break
		}
		intf := st.Pop()
		node, _ := intf.(*Node)

		for i := range node.Children {
			st.Push(node.Children[i])
		}
		nodePool.Put(node)
	}

	return root, nil
}

func (com *Compiler) compileArgID(sel *Select, arg *Arg) error {
	if sel.Where != nil && sel.Where.Op == OpEqID {
		return nil
	}

	ex := expPool.Get().(*Exp)
	ex.Reset()

	ex.Op = OpEqID
	ex.Val = arg.Val.Val

	switch arg.Val.Type {
	case nodeStr:
		ex.Type = ValStr
	case nodeInt:
		ex.Type = ValInt
	case nodeFloat:
		ex.Type = ValFloat
	case nodeVar:
		ex.Type = ValVar
	default:
		fmt.Errorf("expecting a string, int, float or variable")
	}

	sel.Where = ex
	return nil
}

func (com *Compiler) compileArgSearch(sel *Select, arg *Arg) error {
	ex := expPool.Get().(*Exp)
	ex.Reset()

	ex.Op = OpTsQuery
	ex.Type = ValStr
	ex.Val = arg.Val.Val

	if sel.Where != nil {
		ow := sel.Where

		sel.Where = expPool.Get().(*Exp)
		sel.Where.Reset()
		sel.Where.Op = OpAnd
		sel.Where.Children = sel.Where.childrenA[:2]
		sel.Where.Children[0] = ex
		sel.Where.Children[1] = ow
	} else {
		sel.Where = ex
	}
	return nil
}

func (com *Compiler) compileArgWhere(sel *Select, arg *Arg) error {
	st := util.NewStack()
	var err error

	ex, err := com.compileArgObj(st, arg)
	if err != nil {
		return err
	}

	if sel.Where != nil {
		ow := sel.Where

		sel.Where = expPool.Get().(*Exp)
		sel.Where.Reset()
		sel.Where.Op = OpAnd
		sel.Where.Children = sel.Where.childrenA[:2]
		sel.Where.Children[0] = ex
		sel.Where.Children[1] = ow
	} else {
		sel.Where = ex
	}

	return nil
}

func (com *Compiler) compileArgOrderBy(sel *Select, arg *Arg) error {
	if arg.Val.Type != nodeObj {
		return fmt.Errorf("expecting an object")
	}

	st := util.NewStack()

	for i := range arg.Val.Children {
		st.Push(arg.Val.Children[i])
	}

	for {
		if st.Len() == 0 {
			break
		}

		intf := st.Pop()
		node, ok := intf.(*Node)

		if !ok || node == nil {
			return fmt.Errorf("17: unexpected value %v (%t)", intf, intf)
		}

		if _, ok := com.bl[node.Name]; ok {
			if !com.ka {
				nodePool.Put(node)
			}
			continue
		}

		if node.Type == nodeObj {
			for i := range node.Children {
				st.Push(node.Children[i])
			}
			if !com.ka {
				nodePool.Put(node)
			}
			continue
		}

		ob := &OrderBy{}

		switch node.Val {
		case "asc":
			ob.Order = OrderAsc
		case "desc":
			ob.Order = OrderDesc
		case "asc_nulls_first":
			ob.Order = OrderAscNullsFirst
		case "desc_nulls_first":
			ob.Order = OrderDescNullsFirst
		case "asc_nulls_last":
			ob.Order = OrderAscNullsLast
		case "desc_nulls_last":
			ob.Order = OrderDescNullsLast
		default:
			return fmt.Errorf("valid values include asc, desc, asc_nulls_first and desc_nulls_first")
		}

		setOrderByColName(ob, node)
		sel.OrderBy = append(sel.OrderBy, ob)

		if !com.ka {
			nodePool.Put(node)
		}
	}
	return nil
}

func (com *Compiler) compileArgDistinctOn(sel *Select, arg *Arg) error {
	node := arg.Val

	if _, ok := com.bl[node.Name]; ok {
		return nil
	}

	if node.Type != nodeList && node.Type != nodeStr {
		return fmt.Errorf("expecting a list of strings or just a string")
	}

	if node.Type == nodeStr {
		sel.DistinctOn = append(sel.DistinctOn, node.Val)
	}

	for i := range node.Children {
		sel.DistinctOn = append(sel.DistinctOn, node.Children[i].Val)
		if !com.ka {
			nodePool.Put(node.Children[i])
		}
	}

	return nil
}

func (com *Compiler) compileArgLimit(sel *Select, arg *Arg) error {
	node := arg.Val

	if node.Type != nodeInt {
		return fmt.Errorf("expecting an integer")
	}

	sel.Paging.Limit = node.Val

	return nil
}

func (com *Compiler) compileArgOffset(sel *Select, arg *Arg) error {
	node := arg.Val

	if node.Type != nodeInt {
		return fmt.Errorf("expecting an integer")
	}

	sel.Paging.Offset = node.Val
	return nil
}

func compileMutate() (*Query, error) {
	return nil, nil
}

func compileSub() (*Query, error) {
	return nil, nil
}

func newExp(st *util.Stack, node *Node, usePool bool) (*Exp, error) {
	name := node.Name
	if name[0] == '_' {
		name = name[1:]
	}

	var ex *Exp

	if usePool {
		ex = expPool.Get().(*Exp)
		ex.Reset()
	} else {
		ex = &Exp{}
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
		ex.Op = OpILike
		ex.Val = node.Val
	case "similar":
		ex.Op = OpSimilar
		ex.Val = node.Val
	case "nsimilar", "not_similar":
		ex.Op = OpNotSimilar
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
	default:
		pushChildren(st, node.exp, node)
		return nil, nil // skip node
	}

	if ex.Op != OpAnd && ex.Op != OpOr && ex.Op != OpNot {
		switch node.Type {
		case nodeStr:
			ex.Type = ValStr
		case nodeInt:
			ex.Type = ValInt
		case nodeBool:
			ex.Type = ValBool
		case nodeFloat:
			ex.Type = ValFloat
		case nodeList:
			ex.Type = ValList
		case nodeVar:
			ex.Type = ValVar
		default:
			return nil, fmt.Errorf("[Where] valid values include string, int, float, boolean and list: %s", node.Type)
		}
		setWhereColName(ex, node)
	}

	return ex, nil
}

func setListVal(ex *Exp, node *Node) {
	if len(node.Children) != 0 {
		switch node.Children[0].Type {
		case nodeStr:
			ex.ListType = ValStr
		case nodeInt:
			ex.ListType = ValInt
		case nodeBool:
			ex.ListType = ValBool
		case nodeFloat:
			ex.ListType = ValFloat
		}
	}
	for i := range node.Children {
		ex.ListVal = append(ex.ListVal, node.Children[i].Val)
	}
}

func setWhereColName(ex *Exp, node *Node) {
	var list []string

	for n := node.Parent; n != nil; n = n.Parent {
		if n.Type != nodeObj {
			continue
		}
		if len(n.Name) != 0 {
			k := n.Name
			if k == "and" || k == "or" || k == "not" ||
				k == "_and" || k == "_or" || k == "_not" {
				continue
			}
			list = append([]string{k}, list...)
		}
	}
	if len(list) == 1 {
		ex.Col = list[0]

	} else if len(list) > 2 {
		ex.Col = buildPath(list)
		ex.NestedCol = true
	}
}

func setOrderByColName(ob *OrderBy, node *Node) {
	var list []string

	for n := node; n != nil; n = n.Parent {
		if len(n.Name) != 0 {
			list = append([]string{n.Name}, list...)
		}
	}
	if len(list) != 0 {
		ob.Col = buildPath(list)
	}
}

func pushChildren(st *util.Stack, exp *Exp, node *Node) {
	for i := range node.Children {
		node.Children[i].exp = exp
		st.Push(node.Children[i])
	}
}

func pushChild(st *util.Stack, exp *Exp, node *Node) {
	node.Children[0].exp = exp
	st.Push(node.Children[0])

}

func compileFilter(filter []string) (*Exp, error) {
	var fl *Exp
	com := &Compiler{}
	st := util.NewStack()

	if len(filter) == 0 {
		return &Exp{Op: OpNop}, nil
	}

	for i := range filter {
		node, err := ParseArgValue(filter[i])
		if err != nil {
			return nil, err
		}
		f, err := com.compileArgNode(st, node, false)
		if err != nil {
			return nil, err
		}
		if fl == nil {
			fl = f
		} else {
			fl = &Exp{Op: OpAnd, Children: []*Exp{fl, f}}
		}
	}
	return fl, nil
}

func buildPath(a []string) string {
	switch len(a) {
	case 0:
		return ""
	case 1:
		return a[0]
	}

	n := len(a) - 1
	for i := 0; i < len(a); i++ {
		n += len(a[i])
	}

	var b strings.Builder
	b.Grow(n)
	b.WriteString(a[0])
	for _, s := range a[1:] {
		b.WriteRune('.')
		b.WriteString(s)
	}
	return b.String()
}

func (t ExpOp) String() string {
	var v string

	switch t {
	case OpNop:
		v = "op-nop"
	case OpAnd:
		v = "op-and"
	case OpOr:
		v = "op-or"
	case OpNot:
		v = "op-not"
	case OpEquals:
		v = "op-equals"
	case OpNotEquals:
		v = "op-not-equals"
	case OpGreaterOrEquals:
		v = "op-greater-or-equals"
	case OpLesserOrEquals:
		v = "op-lesser-or-equals"
	case OpGreaterThan:
		v = "op-greater-than"
	case OpLesserThan:
		v = "op-lesser-than"
	case OpIn:
		v = "op-in"
	case OpNotIn:
		v = "op-not-in"
	case OpLike:
		v = "op-like"
	case OpNotLike:
		v = "op-not-like"
	case OpILike:
		v = "op-i-like"
	case OpNotILike:
		v = "op-not-i-like"
	case OpSimilar:
		v = "op-similar"
	case OpNotSimilar:
		v = "op-not-similar"
	case OpContains:
		v = "op-contains"
	case OpContainedIn:
		v = "op-contained-in"
	case OpHasKey:
		v = "op-has-key"
	case OpHasKeyAny:
		v = "op-has-key-any"
	case OpHasKeyAll:
		v = "op-has-key-all"
	case OpIsNull:
		v = "op-is-null"
	case OpEqID:
		v = "op-eq-id"
	case OpTsQuery:
		v = "op-ts-query"
	}
	return fmt.Sprintf("<%s>", v)
}

func FreeExp(ex *Exp) {
	if ex.doFree {
		expPool.Put(ex)
	}
}
