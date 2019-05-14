package qcode

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/cespare/xxhash/v2"
	"github.com/dosco/super-graph/util"
	"github.com/gobuffalo/flect"
)

const (
	maxSelectors = 30
)

var (
	idArg          = xxhash.Sum64String("id")
	searchArg      = xxhash.Sum64String("search")
	whereArg       = xxhash.Sum64String("where")
	orderBy1Arg    = xxhash.Sum64String("orderby")
	orderBy2Arg    = xxhash.Sum64String("order_by")
	orderBy3Arg    = xxhash.Sum64String("order")
	distinctOn1Arg = xxhash.Sum64String("distinct_on")
	distinctOn2Arg = xxhash.Sum64String("distinct")
	limitArg       = xxhash.Sum64String("limit")
	offsetArg      = xxhash.Sum64String("offset")
)

type QCode struct {
	Query *Query
}

type Query struct {
	Selects []Select
}

type Column struct {
	Table     []byte
	Name      []byte
	FieldName []byte
}

type Select struct {
	ID         uint16
	ParentID   uint16
	Args       map[uint64]*Node
	AsList     bool
	Table      string
	Singular   string
	FieldName  string
	Cols       []Column
	Where      *Exp
	OrderBy    []*OrderBy
	DistinctOn [][]byte
	Paging     Paging
	Children   []uint16
}

type Exp struct {
	Op        ExpOp
	Col       []byte
	NestedCol bool
	Type      ValType
	Val       []byte
	ListType  ValType
	ListVal   [][]byte
	Children  []*Exp
}

type OrderBy struct {
	Col   []byte
	Order Order
}

type Paging struct {
	Limit  []byte
	Offset []byte
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
}

type Compiler struct {
	fl *Exp
	fm map[uint64]*Exp
	bl map[uint64]struct{}
}

func NewCompiler(conf Config) (*Compiler, error) {
	bl := make(map[uint64]struct{}, len(conf.Blacklist))

	for i := range conf.Blacklist {
		k := xxhash.Sum64String(strings.ToLower(conf.Blacklist[i]))
		bl[k] = struct{}{}
	}

	fl, err := compileFilter(conf.DefaultFilter)
	if err != nil {
		return nil, err
	}

	fm := make(map[uint64]*Exp, len(conf.FilterMap))

	for k, v := range conf.FilterMap {
		fil, err := compileFilter(v)
		if err != nil {
			return nil, err
		}
		k1 := xxhash.Sum64String(strings.ToLower(k))
		fm[k1] = fil
	}

	return &Compiler{fl, fm, bl}, nil
}

func (com *Compiler) CompileQuery(query []byte) (*QCode, error) {
	var qc QCode
	var err error

	op, err := ParseQuery(query)
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

	return &qc, nil
}

func (com *Compiler) compileQuery(op *Operation) (*Query, error) {
	var id, parentID uint16

	selects := make([]Select, 0, 5)
	st := util.NewStack()

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

		intf := st.Pop()
		fid, ok := intf.(uint16)

		if !ok {
			return nil, fmt.Errorf("15: unexpected value %v (%t)", intf, intf)
		}
		field := &op.Fields[fid]

		fn := bytes.ToLower(field.Name)
		if _, ok := com.bl[xxhash.Sum64(fn)]; ok {
			continue
		}
		sfn := string(fn)
		tn := flect.Pluralize(sfn)

		s := Select{
			ID:       id,
			ParentID: parentID,
			Table:    tn,
			Children: make([]uint16, 0, 5),
		}

		if s.ID != 0 {
			p := &selects[s.ParentID]
			p.Children = append(p.Children, s.ID)
		}

		if sfn == tn {
			s.Singular = flect.Singularize(sfn)
		} else {
			s.Singular = sfn
		}

		if sfn == s.Table {
			s.AsList = true
		} else {
			s.Paging.Limit = []byte("1")
		}

		if len(field.Alias) != 0 {
			s.FieldName = string(field.Alias)
		} else if s.AsList {
			s.FieldName = s.Table
		} else {
			s.FieldName = s.Singular
		}

		err := com.compileArgs(&s, field.Args)
		if err != nil {
			return nil, err
		}

		s.Cols = make([]Column, 0, len(field.Children))

		for _, cid := range field.Children {
			f := op.Fields[cid]
			fn := bytes.ToLower(f.Name)

			if _, ok := com.bl[xxhash.Sum64(fn)]; ok {
				continue
			}

			if len(f.Children) != 0 {
				parentID = s.ID
				st.Push(f.ID)
				continue
			}

			col := Column{Name: fn}

			if len(f.Alias) != 0 {
				col.FieldName = f.Alias
			} else {
				col.FieldName = f.Name
			}
			s.Cols = append(s.Cols, col)
		}

		selects = append(selects, s)
		id++
	}

	var ok bool
	var fil *Exp

	if id > 0 {
		root := &selects[0]
		fil, ok = com.fm[xxhash.Sum64String(root.Table)]

		if !ok || fil == nil {
			fil = com.fl
		}

		if fil != nil && fil.Op != OpNop {

			if root.Where != nil {
				ex := &Exp{Op: OpAnd, Children: []*Exp{fil, root.Where}}
				root.Where = ex
			} else {
				root.Where = fil
			}
		}

	} else {
		return nil, errors.New("invalid query")
	}

	return &Query{selects[:id]}, nil
}

func (com *Compiler) compileArgs(sel *Select, args []*Arg) error {
	var err error

	sel.Args = make(map[uint64]*Node, len(args))

	for i := range args {
		if args[i] == nil {
			return fmt.Errorf("[Args] unexpected nil argument found")
		}
		an := bytes.ToLower(args[i].Name)
		k := xxhash.Sum64(an)
		if _, ok := sel.Args[k]; ok {
			continue
		}

		switch k {
		case idArg:
			if sel.ID == 0 {
				err = com.compileArgID(sel, args[i])
			}
		case searchArg:
			err = com.compileArgSearch(sel, args[i])
		case whereArg:
			err = com.compileArgWhere(sel, args[i])
		case orderBy1Arg, orderBy2Arg, orderBy3Arg:
			err = com.compileArgOrderBy(sel, args[i])
		case distinctOn1Arg, distinctOn2Arg:
			err = com.compileArgDistinctOn(sel, args[i])
		case limitArg:
			err = com.compileArgLimit(sel, args[i])
		case offsetArg:
			err = com.compileArgOffset(sel, args[i])
		}

		if err != nil {
			return err
		}

		sel.Args[k] = args[i].Val
	}

	return nil
}

type expT struct {
	parent *Exp
	node   *Node
}

func (com *Compiler) compileArgObj(arg *Arg) (*Exp, error) {
	if arg.Val.Type != nodeObj {
		return nil, fmt.Errorf("expecting an object")
	}

	return com.compileArgNode(arg.Val)
}

func (com *Compiler) compileArgNode(val *Node) (*Exp, error) {
	st := util.NewStack()
	var root *Exp

	if val == nil || len(val.Children) == 0 {
		return nil, errors.New("invalid argument value")
	}

	st.Push(&expT{nil, val.Children[0]})

	for {
		if st.Len() == 0 {
			break
		}

		intf := st.Pop()
		eT, ok := intf.(*expT)
		if !ok || eT == nil {
			return nil, fmt.Errorf("16: unexpected value %v (%t)", intf, intf)
		}

		if len(eT.node.Name) != 0 {
			k := xxhash.Sum64(bytes.ToLower(eT.node.Name))
			if _, ok := com.bl[k]; ok {
				continue
			}
		}

		ex, err := newExp(st, eT)

		if err != nil {
			return nil, err
		}

		if ex == nil {
			continue
		}

		if eT.parent == nil {
			root = ex
		} else {
			eT.parent.Children = append(eT.parent.Children, ex)
		}
	}

	return root, nil
}

func (com *Compiler) compileArgID(sel *Select, arg *Arg) error {
	if sel.Where != nil && sel.Where.Op == OpEqID {
		return nil
	}

	ex := &Exp{Op: OpEqID, Val: arg.Val.Val}

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
	ex := &Exp{
		Op:   OpTsQuery,
		Type: ValStr,
		Val:  arg.Val.Val,
	}

	if sel.Where != nil {
		sel.Where = &Exp{Op: OpAnd, Children: []*Exp{ex, sel.Where}}
	} else {
		sel.Where = ex
	}
	return nil
}

func (com *Compiler) compileArgWhere(sel *Select, arg *Arg) error {
	var err error

	ex, err := com.compileArgObj(arg)
	if err != nil {
		return err
	}

	if sel.Where != nil {
		sel.Where = &Exp{Op: OpAnd, Children: []*Exp{ex, sel.Where}}
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

		k := xxhash.Sum64(bytes.ToLower(node.Name))
		if _, ok := com.bl[k]; ok {
			continue
		}

		if node.Type == nodeObj {
			for i := range node.Children {
				st.Push(node.Children[i])
			}
			continue
		}

		ob := &OrderBy{}

		val := string(bytes.ToLower(node.Val))
		switch val {
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
	}
	return nil
}

func (com *Compiler) compileArgDistinctOn(sel *Select, arg *Arg) error {
	node := arg.Val

	k := xxhash.Sum64(bytes.ToLower(node.Name))
	if _, ok := com.bl[k]; ok {
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

func newExp(st *util.Stack, eT *expT) (*Exp, error) {
	ex := &Exp{}
	node := eT.node

	if len(node.Name) == 0 {
		pushChildren(st, eT.parent, node)
		return nil, nil
	}

	name := bytes.ToLower(node.Name)
	if name[0] == '_' {
		name = name[1:]
	}

	switch string(name) {
	case "and":
		ex.Op = OpAnd
		pushChildren(st, ex, node)
	case "or":
		ex.Op = OpOr
		pushChildren(st, ex, node)
	case "not":
		ex.Op = OpNot
		st.Push(&expT{ex, node.Children[0]})
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
		pushChildren(st, eT.parent, node)
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
	var list [][]byte

	for n := node.Parent; n != nil; n = n.Parent {
		if n.Type != nodeObj {
			continue
		}
		if len(n.Name) != 0 {
			k := string(bytes.ToLower(n.Name))
			if k == "and" || k == "or" || k == "not" ||
				k == "_and" || k == "_or" || k == "_not" {
				continue
			}
			list = append([][]byte{[]byte(k)}, list...)
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
	var list [][]byte

	for n := node; n != nil; n = n.Parent {
		if len(n.Name) != 0 {
			k := bytes.ToLower(n.Name)
			list = append([][]byte{k}, list...)
		}
	}
	if len(list) != 0 {
		ob.Col = buildPath(list)
	}
}

func pushChildren(st *util.Stack, ex *Exp, node *Node) {
	for i := range node.Children {
		st.Push(&expT{ex, node.Children[i]})
	}
}

func compileFilter(filter []string) (*Exp, error) {
	var fl *Exp
	com := &Compiler{}

	if len(filter) == 0 {
		return &Exp{Op: OpNop}, nil
	}

	for i := range filter {
		node, err := ParseArgValue([]byte(filter[i]))
		if err != nil {
			return nil, err
		}
		f, err := com.compileArgNode(node)
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

func buildPath(a [][]byte) []byte {
	switch len(a) {
	case 0:
		return nil
	case 1:
		return a[0]
	}

	n := len(a) - 1
	for i := 0; i < len(a); i++ {
		n += len(a[i])
	}

	b := bytes.NewBuffer(make([]byte, 0, n))

	b.Write(a[0])
	for _, s := range a[1:] {
		b.WriteRune('.')
		b.Write(s)
	}
	return b.Bytes()
}
