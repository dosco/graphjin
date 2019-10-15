package qcode

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/dosco/super-graph/util"
	"github.com/gobuffalo/flect"
)

type QType int
type Action int

const (
	maxSelectors = 30

	QTQuery QType = iota + 1
	QTInsert
	QTUpdate
	QTDelete
	QTUpsert
)

type QCode struct {
	Type      QType
	ActionVar string
	Selects   []Select
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
	Functions  bool
	Allowed    map[string]struct{}
}

type Column struct {
	Table     string
	Name      string
	FieldName string
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
	Limit   string
	Offset  string
	NoLimit bool
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

type Compiler struct {
	tr map[string]map[string]*trval
	bl map[string]struct{}
	ka bool
}

var expPool = sync.Pool{
	New: func() interface{} { return &Exp{doFree: true} },
}

func NewCompiler(c Config) (*Compiler, error) {
	co := &Compiler{ka: c.KeepArgs}
	co.tr = make(map[string]map[string]*trval)
	co.bl = make(map[string]struct{}, len(c.Blocklist))

	for i := range c.Blocklist {
		co.bl[strings.ToLower(c.Blocklist[i])] = struct{}{}
	}

	seedExp := [100]Exp{}

	for i := range seedExp {
		seedExp[i].doFree = true
		expPool.Put(&seedExp[i])
	}

	return co, nil
}

func (com *Compiler) AddRole(role, table string, trc TRConfig) error {
	var err error
	trv := &trval{}

	toMap := func(cols []string) map[string]struct{} {
		m := make(map[string]struct{}, len(cols))
		for i := range cols {
			m[strings.ToLower(cols[i])] = struct{}{}
		}
		return m
	}

	// query config
	trv.query.fil, err = compileFilter(trc.Query.Filter)
	if err != nil {
		return err
	}
	if trc.Query.Limit > 0 {
		trv.query.limit = strconv.Itoa(trc.Query.Limit)
	}
	trv.query.cols = toMap(trc.Query.Columns)
	trv.query.disable.funcs = trc.Query.DisableFunctions

	// insert config
	if trv.insert.fil, err = compileFilter(trc.Insert.Filter); err != nil {
		return err
	}
	trv.insert.cols = toMap(trc.Insert.Columns)

	// update config
	if trv.update.fil, err = compileFilter(trc.Update.Filter); err != nil {
		return err
	}
	trv.insert.cols = toMap(trc.Insert.Columns)
	trv.insert.set = trc.Insert.Set

	// delete config
	if trv.delete.fil, err = compileFilter(trc.Delete.Filter); err != nil {
		return err
	}
	trv.delete.cols = toMap(trc.Delete.Columns)

	singular := flect.Singularize(table)
	plural := flect.Pluralize(table)

	if _, ok := com.tr[role]; !ok {
		com.tr[role] = make(map[string]*trval)
	}

	com.tr[role][singular] = trv
	com.tr[role][plural] = trv
	return nil
}

func (com *Compiler) Compile(query []byte, role string) (*QCode, error) {
	var err error

	qc := QCode{Type: QTQuery}

	op, err := Parse(query)
	if err != nil {
		return nil, err
	}

	if err = com.compileQuery(&qc, op, role); err != nil {
		return nil, err
	}

	opPool.Put(op)

	return &qc, nil
}

func (com *Compiler) compileQuery(qc *QCode, op *Operation, role string) error {
	id := int32(0)
	parentID := int32(0)

	if len(op.Fields) == 0 {
		return errors.New("invalid graphql no query found")
	}

	if op.Type == opMutate {
		if err := com.setMutationType(qc, op.Fields[0].Args); err != nil {
			return err
		}
	}

	selects := make([]Select, 0, 5)
	st := NewStack()
	action := qc.Type

	if len(op.Fields) == 0 {
		return errors.New("empty query")
	}
	st.Push(op.Fields[0].ID)

	for {
		if st.Len() == 0 {
			break
		}

		if id >= maxSelectors {
			return fmt.Errorf("selector limit reached (%d)", maxSelectors)
		}

		fid := st.Pop()
		field := &op.Fields[fid]

		if _, ok := com.bl[field.Name]; ok {
			continue
		}

		trv := com.getRole(role, field.Name)

		selects = append(selects, Select{
			ID:       id,
			ParentID: parentID,
			Table:    field.Name,
			Children: make([]int32, 0, 5),
			Allowed:  trv.allowedColumns(action),
		})
		s := &selects[(len(selects) - 1)]

		if action == QTQuery {
			s.Functions = !trv.query.disable.funcs

			if len(trv.query.limit) != 0 {
				s.Paging.Limit = trv.query.limit
			}
		}

		if s.ID != 0 {
			p := &selects[s.ParentID]
			p.Children = append(p.Children, s.ID)
		}

		if len(field.Alias) != 0 {
			s.FieldName = field.Alias
		} else {
			s.FieldName = s.Table
		}

		err := com.compileArgs(qc, s, field.Args)
		if err != nil {
			return err
		}

		s.Cols = make([]Column, 0, len(field.Children))
		action = QTQuery

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

	if id == 0 {
		return errors.New("invalid query")
	}

	var fil *Exp
	root := &selects[0]

	if trv, ok := com.tr[role][op.Fields[0].Name]; ok {
		fil = trv.filter(qc.Type)
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

	qc.Selects = selects[:id]
	return nil
}

func (com *Compiler) compileArgs(qc *QCode, sel *Select, args []Arg) error {
	var err error

	if com.ka {
		sel.Args = make(map[string]*Node, len(args))
	}

	for i := range args {
		arg := &args[i]

		switch arg.Name {
		case "id":
			err = com.compileArgID(sel, arg)
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

func (com *Compiler) setMutationType(qc *QCode, args []Arg) error {
	setActionVar := func(arg *Arg) error {
		if arg.Val.Type != nodeVar {
			return fmt.Errorf("value for argument '%s' must be a variable", arg.Name)
		}
		qc.ActionVar = arg.Val.Val
		return nil
	}

	for i := range args {
		arg := &args[i]

		switch arg.Name {
		case "insert":
			qc.Type = QTInsert
			return setActionVar(arg)
		case "update":
			qc.Type = QTUpdate
			return setActionVar(arg)
		case "upsert":
			qc.Type = QTUpsert
			return setActionVar(arg)
		case "delete":
			qc.Type = QTDelete

			if arg.Val.Type != nodeBool {
				return fmt.Errorf("value for argument '%s' must be a boolean", arg.Name)
			}

			if arg.Val.Val == "false" {
				qc.Type = QTQuery
			}
			return nil
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
	if sel.ID != 0 {
		return nil
	}

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

var zeroTrv = &trval{}

func (com *Compiler) getRole(role, field string) *trval {
	if trv, ok := com.tr[role][field]; ok {
		return trv
	} else {
		return zeroTrv
	}
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
		ex = &Exp{doFree: false}
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
		return &Exp{Op: OpNop, doFree: false}, nil
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
			fl = &Exp{Op: OpAnd, Children: []*Exp{fl, f}, doFree: false}
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
	//	fmt.Println(">", ex.doFree)
	if ex.doFree {
		expPool.Put(ex)
	}
}
