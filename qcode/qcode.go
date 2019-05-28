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
	idMask       = 5000
)

var (
	ArgID          = xxhash.Sum64String("id")
	ArgSearch      = xxhash.Sum64String("search")
	ArgWhere       = xxhash.Sum64String("where")
	ArgOrderBy1    = xxhash.Sum64String("orderby")
	ArgOrderBy2    = xxhash.Sum64String("order_by")
	ArgOrderBy3    = xxhash.Sum64String("order")
	ArgDistinctOn1 = xxhash.Sum64String("distinct_on")
	ArgDistinctOn2 = xxhash.Sum64String("distinct")
	ArgLimit       = xxhash.Sum64String("limit")
	ArgOffset      = xxhash.Sum64String("offset")
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
	ID          int
	ParentID    int
	WhereRootID int
	Args        map[uint64][]Node
	AsList      bool
	Table       string
	Singular    string
	FieldName   string
	Cols        []Column
	Where       []Exp
	OrderBy     []OrderBy
	DistinctOn  [][]byte
	Paging      Paging
	Children    []int
}

type Exp struct {
	ID        int
	ParentID  int
	Op        ExpOp
	Col       []byte
	NestedCol bool
	Type      ValType
	Val       []byte
	ListType  ValType
	ListVal   [][]byte
	Children  []int
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
	fl []Exp
	fm map[uint64][]Exp
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

	fm := make(map[uint64][]Exp, len(conf.FilterMap))

	for k, v := range conf.FilterMap {
		fil, err := compileFilter(v)
		if err != nil {
			return nil, err
		}
		k1 := xxhash.Sum64String(strings.ToLower(k))
		fm[k1] = append(fm[k1], fil...)
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
	var id, parentID int

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
		fid, ok := intf.(int)

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
			Children: make([]int, 0, 5),
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

			col := Column{Name: string(fn)}

			if len(f.Alias) != 0 {
				col.FieldName = string(f.Alias)
			} else {
				col.FieldName = string(f.Name)
			}
			s.Cols = append(s.Cols, col)
		}

		selects = append(selects, s)
		id++
	}

	var ok bool
	var fil []Exp

	if id == 0 {
		return nil, errors.New("invalid query")
	}

	root := &selects[0]
	fil, ok = com.fm[xxhash.Sum64String(root.Table)]

	if !ok || fil == nil {
		fil = com.fl
	}

	if len(fil) != 0 && fil[0].Op != OpNop {
		root.Where, root.WhereRootID = addExpListA(root.Where, root.WhereRootID, fil)
	}

	return &Query{selects[:id]}, nil
}

func (com *Compiler) compileArgs(sel *Select, args []Arg) error {
	var err error

	sel.Args = make(map[uint64][]Node, len(args))

	for i := range args {
		if len(args) == 0 {
			return fmt.Errorf("[Args] no argument found")
		}
		an := bytes.ToLower(args[i].Name)
		k := xxhash.Sum64(an)
		if _, ok := sel.Args[k]; ok {
			continue
		}

		switch k {
		case ArgID:
			if sel.ID == 0 {
				err = com.compileArgID(sel, &args[i])
			}
		case ArgSearch:
			err = com.compileArgSearch(sel, &args[i])
		case ArgWhere:
			err = com.compileArgWhere(sel, &args[i])
		case ArgOrderBy1, ArgOrderBy2, ArgOrderBy3:
			err = com.compileArgOrderBy(sel, &args[i])
		case ArgDistinctOn1, ArgDistinctOn2:
			err = com.compileArgDistinctOn(sel, &args[i])
		case ArgLimit:
			err = com.compileArgLimit(sel, &args[i])
		case ArgOffset:
			err = com.compileArgOffset(sel, &args[i])
		}

		if err != nil {
			return err
		}

		sel.Args[k] = args[i].Val
	}

	return nil
}

func (com *Compiler) compileArgObj(arg *Arg) ([]Exp, error) {
	if arg.Val[0].Type != nodeObj {
		return nil, fmt.Errorf("expecting an object")
	}

	return com.compileArgNode(arg.Val)
}

func (com *Compiler) compileArgNode(nodes []Node) ([]Exp, error) {
	st := util.NewStack()
	var list []Exp

	if len(nodes) == 0 || len(nodes[0].Children) == 0 {
		return nil, errors.New("invalid argument value")
	}

	st.Push(nodes[0].ID | (nodes[0].ParentID << 16))

	for {
		if st.Len() == 0 {
			break
		}

		intf := st.Pop()
		val, ok := intf.(int)

		if !ok {
			return nil, fmt.Errorf("16: unexpected value %v (%t)", intf, intf)
		}

		// ID and parentID is encoded into a single int
		id := val & 0xFFFF
		pid := (val >> 16) & 0xFFFF

		if (pid & (1 << 15)) != 0 {
			pid = -1
		}

		k := nodes[id].Name

		if len(k) != 0 {
			_, ok := com.bl[xxhash.Sum64(k)]
			if ok {
				continue
			}
		}

		ex := Exp{ID: len(list), ParentID: pid}
		err := buildExp(st, nodes, id, &ex)

		if ex.Op == OpNop {
			continue
		}

		if err != nil {
			return nil, err
		}

		if ex.ParentID == -1 {
			list = append(list, ex)

		} else {
			list[ex.ParentID].Children = append(list[ex.ParentID].Children, ex.ID)
			list = append(list, ex)
		}
	}

	return list, nil
}

func (com *Compiler) compileArgID(sel *Select, arg *Arg) error {
	if len(sel.Where) != 0 && sel.Where[0].Op == OpEqID {
		return nil
	}

	if arg == nil || len(arg.Val) == 0 {
		return errors.New("id param missing argument")
	}

	ex := Exp{
		ID:       0,
		ParentID: -1,
		Op:       OpEqID,
		Val:      arg.Val[0].Val,
	}

	switch arg.Val[0].Type {
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

	sel.Where = append(sel.Where, ex)
	return nil
}

func (com *Compiler) compileArgSearch(sel *Select, arg *Arg) error {
	if arg == nil || len(arg.Val) == 0 {
		return errors.New("search param missing argument")
	}

	ex := Exp{
		ID:       0,
		ParentID: -1,
		Op:       OpTsQuery,
		Type:     ValStr,
		Val:      arg.Val[0].Val,
	}

	sel.Where, sel.WhereRootID = addExp(sel.Where, sel.WhereRootID, ex)
	return nil
}

func (com *Compiler) compileArgWhere(sel *Select, arg *Arg) error {
	var err error

	exl, err := com.compileArgObj(arg)
	if err != nil {
		return err
	}

	sel.Where, sel.WhereRootID = addExpList(sel.Where, sel.WhereRootID, exl)
	return nil
}

func (com *Compiler) compileArgOrderBy(sel *Select, arg *Arg) error {
	if arg == nil || len(arg.Val) == 0 {
		return errors.New("order_by param missing argument")
	}

	if arg.Val[0].Type != nodeObj {
		return fmt.Errorf("expecting an object")
	}

	st := util.NewStack()
	st.Push(0)

	for {
		if st.Len() == 0 {
			break
		}

		intf := st.Pop()
		id, ok := intf.(int)

		if !ok {
			return fmt.Errorf("17: unexpected value %v (%t)", intf, intf)
		}
		node := arg.Val[id]

		if _, ok := com.bl[xxhash.Sum64(node.Name)]; ok {
			continue
		}

		if node.Type == nodeObj {
			for i := range node.Children {
				st.Push(node.Children[i])
			}
			continue
		}

		ob := OrderBy{}

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

		setOrderByColName(arg.Val, &node, &ob)
		sel.OrderBy = append(sel.OrderBy, ob)
	}
	return nil
}

func (com *Compiler) compileArgDistinctOn(sel *Select, arg *Arg) error {
	node := arg.Val[0]

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
		node := arg.Val[node.Children[i]]
		sel.DistinctOn = append(sel.DistinctOn, node.Val)
	}

	return nil
}

func (com *Compiler) compileArgLimit(sel *Select, arg *Arg) error {
	node := arg.Val[0]

	if node.Type != nodeInt {
		return fmt.Errorf("expecting an integer")
	}

	sel.Paging.Limit = node.Val

	return nil
}

func (com *Compiler) compileArgOffset(sel *Select, arg *Arg) error {
	node := arg.Val[0]

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

func buildExp(st *util.Stack, nodes []Node, nodeID int, ex *Exp) error {
	node := &nodes[nodeID]
	var err error

	if len(node.Name) == 0 {
		pushChildren(st, node, ex.ParentID)
		return nil // skip node
	}

	name := node.Name

	if name[0] == '_' {
		name = name[1:]
	}

	switch string(name) {
	case "and":
		ex.Op = OpAnd
		err = pushChildren(st, node, ex.ID)
	case "or":
		ex.Op = OpOr
		err = pushChildren(st, node, ex.ID)
	case "not":
		ex.Op = OpNot
		err = pushChild(st, node, ex.ID)
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
		setListVal(nodes, node, ex)
	case "nin", "not_in":
		ex.Op = OpNotIn
		setListVal(nodes, node, ex)
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
		pushChildren(st, node, ex.ParentID)
		return nil // skip node
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
			return fmt.Errorf("[Where] valid values include string, int, float, boolean and list: %s", node.Type)
		}
		setWhereColName(nodes, node, ex)
	}

	return err
}

func setListVal(nodes []Node, node *Node, ex *Exp) {
	if len(node.Children) != 0 {
		ch := nodes[node.Children[0]]
		switch ch.Type {
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
		ch := nodes[node.Children[i]]
		ex.ListVal = append(ex.ListVal, ch.Val)
	}
}

func setWhereColName(nodes []Node, node *Node, ex *Exp) {
	var list [][]byte
	pid := node.ParentID

	for pid != -1 {
		n := nodes[pid]
		pid = n.ParentID

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

func setOrderByColName(nodes []Node, node *Node, ob *OrderBy) {
	var list [][]byte

	for id := node.ID; id != -1; {
		n := nodes[id]
		id = n.ParentID

		if len(n.Name) != 0 {
			k := bytes.ToLower(n.Name)
			list = append([][]byte{k}, list...)
		}
	}
	if len(list) != 0 {
		ob.Col = buildPath(list)
	}
}

func pushChild(st *util.Stack, node *Node, parentID int) error {
	if len(node.Children) > 1 {
		return errors.New("too many expressions")
	}

	pushChildren(st, node, parentID)
	return nil
}

func pushChildren(st *util.Stack, node *Node, parentID int) error {
	if len(node.Children) == 0 {
		return errors.New("expression missing")
	}

	for i := range node.Children {
		// ID and parentID is encoded into a single int
		st.Push(node.Children[i] | (parentID << 16))
	}
	return nil
}

func compileFilter(filter []string) ([]Exp, error) {
	var oexl []Exp
	com := &Compiler{}

	if len(filter) == 0 {
		// force OpNop expression to overwrite (unset) filters
		return []Exp{{Op: OpNop, ParentID: -1}}, nil
	}

	and := Exp{
		ID:       0,
		ParentID: -1,
		Op:       OpAnd,
	}
	oexl = append(oexl, and)

	for i := range filter {
		nodes, err := ParseArgValue([]byte(filter[i]))
		if err != nil {
			return nil, err
		}

		exl, err := com.compileArgNode(nodes)
		if err != nil {
			return nil, err
		}

		n := len(oexl)
		oexl = append(oexl, exl...)
		incID(oexl, n, 0)
	}

	for i := range oexl {
		if oexl[i].ParentID == 0 {
			oexl[0].Children = append(oexl[0].Children, oexl[i].ID)
		}
	}

	return oexl, nil
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

func addExp(oexl []Exp, rootID int, ex Exp) ([]Exp, int) {
	if ex.Op == OpAnd || ex.Op == OpOr || ex.Op == OpNot {
		return oexl, rootID
	}

	if len(oexl) == 0 {
		return append(oexl, ex), 0
	}

	newRootID := len(oexl)
	ex.ID = newRootID + 1
	and := Exp{
		ID:       newRootID,
		Op:       OpAnd,
		Children: []int{ex.ID, rootID},
	}

	oexl = append(oexl, and, ex)
	oexl[rootID].ParentID = newRootID

	return oexl, newRootID
}

// Prepend version of addExpList. This is faster but mutates exl
// which cannot be shared memory
func addExpList(oexl []Exp, rootID int, exl []Exp) ([]Exp, int) {
	if len(oexl) == 0 {
		return exl, 0
	}

	newRootID := len(oexl)

	if exl[0].Op == OpAnd || exl[0].Op == OpOr {
		oexl = append(oexl, exl...)
		incID(oexl, newRootID, -1)
		oexl[newRootID].Children = append(oexl[newRootID].Children, rootID)

	} else {
		and := Exp{
			ID:       newRootID,
			ParentID: -1,
			Op:       OpAnd,
			Children: []int{rootID, (exl[0].ID + newRootID + 1)},
		}
		oexl = append(oexl, and)
		oexl = append(oexl, exl...)

		incID(oexl, (newRootID + 1), 0)
	}
	oexl[rootID].ParentID = newRootID

	return oexl, newRootID
}

// Append version of addExpList. This does not mutate shared
// memory in exl
func addExpListA(oexl []Exp, rootID int, exl []Exp) ([]Exp, int) {
	if len(oexl) == 0 {
		return exl, 0
	}

	nexl := make([]Exp, 0, (len(oexl) + len(exl) + 1))

	if exl[0].Op == OpAnd || exl[0].Op == OpOr {
		nexl = append(nexl, exl...)
	} else {
		and := Exp{
			ID:       0,
			ParentID: -1,
			Op:       OpAnd,
		}
		nexl = append(nexl, and)
		nexl = append(nexl, exl...)
	}

	nexl = append(nexl, oexl...)
	incID(nexl, len(exl), -1)
	e := &nexl[(len(exl) + rootID)]

	for i := 1; i < len(nexl); i++ {
		n := &nexl[i]

		if n.ParentID == -1 {
			if n.Op == OpAnd {
				nexl[0].Children = append(nexl[0].Children, n.Children...)
			} else {
				nexl[0].Children = append(nexl[0].Children, n.ID)
				n.ParentID = 0
			}
		}

		if n.ParentID == e.ID && e.Op == OpAnd {
			n.ParentID = 0
		}
	}

	return nexl, 0
}

func incID(exl []Exp, val, npid int) {
	for i := val; i < len(exl); i++ {
		exl[i].ID += val

		if exl[i].ParentID == -1 {
			exl[i].ParentID = npid
		} else {
			exl[i].ParentID += val
		}

		for n := range exl[i].Children {
			exl[i].Children[n] += val
		}
	}
}
