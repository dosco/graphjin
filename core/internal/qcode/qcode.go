package qcode

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/dosco/super-graph/core/internal/util"
	"github.com/gobuffalo/flect"
)

type QType int8
type SType int8
type Action int8
type SkipType int8

const (
	maxSelectors = 30
)

const (
	QTUnknown QType = iota
	QTQuery
	QTSubscription
	QTMutation
	QTInsert
	QTUpdate
	QTDelete
	QTUpsert
)

const (
	STNone SType = iota
	STUnion
	STMember
)

const (
	SkipTypeNone SkipType = iota
	SkipTypeUserNeeded
	SkipTypeTableNotFound
	SkipTypeBlocked
	SkipTypeRemote
)

type QCode struct {
	Type      QType
	ActionVar string
	Selects   []Select
	Roots     []int32
	rootsA    [5]int32
}

type Select struct {
	ID         int32
	ParentID   int32
	UParentID  int32
	Type       SType
	Args       map[string]*Node
	Name       string
	FieldName  string
	Cols       []Column
	Where      *Exp
	OrderBy    []*OrderBy
	DistinctOn []string
	Paging     Paging
	Children   []int32
	Functions  bool
	Allowed    map[string]struct{}
	PresetMap  map[string]string
	PresetList []string
	SkipRender SkipType
}

type Column struct {
	Table     string
	Name      string
	FieldName string
}

type Exp struct {
	Op         ExpOp
	Col        string
	NestedCols []string
	Type       ValType
	Table      string
	Val        string
	ListType   ValType
	ListVal    []string
	Children   []*Exp
	childrenA  [5]*Exp
	internal   bool
	doFree     bool
}

var zeroExp = Exp{doFree: true}

func (ex *Exp) Reset() {
	*ex = zeroExp
}

type OrderBy struct {
	Col   string
	Order Order
}

type PagingType int

const (
	PtOffset PagingType = iota
	PtForward
	PtBackward
)

type Paging struct {
	Type    PagingType
	Limit   string
	Offset  string
	Cursor  bool
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
	OpFalse
	OpNotDistinct
	OpDistinct
)

type ValType int

const (
	ValStr ValType = iota + 1
	ValNum
	ValBool
	ValList
	ValVar
	ValNone
	ValRef
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
	tr       map[string]map[string]*trval
	defBlock bool
}

var expPool = sync.Pool{
	New: func() interface{} { return &Exp{doFree: true} },
}

func NewCompiler(c Config) (*Compiler, error) {
	co := &Compiler{defBlock: c.DefaultBlock}
	co.tr = make(map[string]map[string]*trval)
	seedExp := [100]Exp{}

	for i := range seedExp {
		seedExp[i].doFree = true
		expPool.Put(&seedExp[i])
	}

	return co, nil
}

func NewFilter() *Exp {
	ex := expPool.Get().(*Exp)
	ex.Reset()
	ex.internal = true

	return ex
}

func (com *Compiler) AddRole(role, table string, trc TRConfig) error {
	var err error
	trv := &trval{}

	// query config
	trv.query.fil, trv.query.filNU, err = compileFilter(trc.Query.Filters)
	if err != nil {
		return err
	}
	if trc.Query.Limit > 0 {
		trv.query.limit = strconv.Itoa(trc.Query.Limit)
	}
	trv.query.cols = listToMap(trc.Query.Columns)
	trv.query.disable.funcs = trc.Query.DisableFunctions
	trv.query.block = trc.Query.Block

	// insert config
	trv.insert.fil, trv.insert.filNU, err = compileFilter(trc.Insert.Filters)
	if err != nil {
		return err
	}
	trv.insert.cols = listToMap(trc.Insert.Columns)
	trv.insert.psmap = trc.Insert.Presets
	trv.insert.pslist = mapToList(trv.insert.psmap)
	trv.insert.block = trc.Insert.Block

	// update config
	trv.update.fil, trv.update.filNU, err = compileFilter(trc.Update.Filters)
	if err != nil {
		return err
	}
	trv.update.cols = listToMap(trc.Update.Columns)
	trv.update.psmap = trc.Update.Presets
	trv.update.pslist = mapToList(trv.update.psmap)
	trv.update.block = trc.Update.Block

	// delete config
	trv.delete.fil, trv.delete.filNU, err = compileFilter(trc.Delete.Filters)
	if err != nil {
		return err
	}
	trv.delete.cols = listToMap(trc.Delete.Columns)
	trv.delete.block = trc.Delete.Block

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
	qc.Roots = qc.rootsA[:0]

	op, err := Parse(query)
	if err != nil {
		return nil, err
	}

	if err = com.compileQuery(&qc, op, role); err != nil {
		return nil, err
	}

	freeNodes(op)
	opPool.Put(op)

	return &qc, nil
}

func (com *Compiler) compileQuery(qc *QCode, op *Operation, role string) error {
	id := int32(0)

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

	for i := range op.Fields {
		if op.Fields[i].ParentID == -1 {
			val := op.Fields[i].ID | (-1 << 16)
			st.Push(val)
		}
	}

	for {
		if st.Len() == 0 {
			break
		}

		if id >= maxSelectors {
			return fmt.Errorf("selector limit reached (%d)", maxSelectors)
		}

		val := st.Pop()
		fid := val & 0xFFFF
		parentID := (val >> 16) & 0xFFFF

		field := &op.Fields[fid]

		if field.ParentID == -1 {
			parentID = -1
		}

		trv := com.getRole(role, field.Name)
		skipRender := SkipTypeNone

		if trv != nil {
			switch action {
			case QTQuery:
				if trv.query.block {
					skipRender = SkipTypeBlocked
				}

			case QTInsert:
				if trv.insert.block {
					return fmt.Errorf("%s, insert blocked: %s", role, field.Name)
				}

			case QTUpdate:
				if trv.update.block {
					return fmt.Errorf("%s, update blocked: %s", role, field.Name)
				}

			case QTDelete:
				if trv.delete.block {
					return fmt.Errorf("%s, delete blocked: %s", role, field.Name)
				}
			}

		} else if com.defBlock && role == "anon" {
			skipRender = SkipTypeTableNotFound
		}

		selects = append(selects, Select{
			ID:         id,
			ParentID:   parentID,
			Name:       field.Name,
			SkipRender: skipRender,
		})
		s := &selects[(len(selects) - 1)]

		if field.Union {
			s.Type = STUnion
		}

		if field.Alias != "" {
			s.FieldName = field.Alias
		} else {
			s.FieldName = s.Name
		}

		if s.ParentID == -1 {
			qc.Roots = append(qc.Roots, s.ID)
		} else {
			p := &selects[s.ParentID]
			p.Children = append(p.Children, s.ID)

			if p.Type == STUnion {
				s.Type = STMember
				s.UParentID = p.ParentID
			}
		}

		if skipRender != SkipTypeNone {
			id++
			continue
		}

		s.Children = make([]int32, 0, 5)
		s.Functions = true

		if trv != nil {
			s.Allowed = trv.allowedColumns(action)

			switch action {
			case QTQuery:
				s.Functions = !trv.query.disable.funcs
				s.Paging.Limit = trv.query.limit

			case QTInsert:
				s.PresetMap = trv.insert.psmap
				s.PresetList = trv.insert.pslist

			case QTUpdate:
				s.PresetMap = trv.update.psmap
				s.PresetList = trv.update.pslist
			}
		}

		err := com.compileArgs(qc, s, field.Args, role)
		if err != nil {
			return err
		}

		// Order is important AddFilters must come after compileArgs
		com.AddFilters(qc, s, role)

		s.Cols = make([]Column, 0, len(field.Children))
		cm := make(map[string]struct{})
		action = QTQuery

		for _, cid := range field.Children {
			f := op.Fields[cid]

			var fname string

			if f.Alias != "" {
				fname = f.Alias
			} else {
				fname = f.Name
			}

			if _, ok := cm[fname]; ok {
				continue
			} else {
				cm[fname] = struct{}{}
			}

			if len(f.Children) != 0 {
				val := f.ID | (s.ID << 16)
				st.Push(val)
				continue
			}

			col := Column{Name: f.Name, FieldName: fname}
			s.Cols = append(s.Cols, col)
		}

		id++
	}

	if id == 0 {
		return errors.New("invalid query")
	}

	qc.Selects = selects[:id]

	return nil
}

func (com *Compiler) AddFilters(qc *QCode, sel *Select, role string) {
	var fil *Exp
	var nu bool // need user_id (or not) in this filter

	if trv, ok := com.tr[role][sel.Name]; ok {
		fil, nu = trv.filter(qc.Type)
	}

	if fil == nil {
		return
	}

	if nu && role == "anon" {
		sel.SkipRender = SkipTypeUserNeeded
	}

	switch fil.Op {
	case OpNop:
	case OpFalse:
		sel.Where = fil
	default:
		AddFilter(sel, fil)
	}
}

func (com *Compiler) compileArgs(qc *QCode, sel *Select, args []Arg, role string) error {
	var err error

	for i := range args {
		arg := &args[i]

		switch arg.Name {
		case "id":
			err = com.compileArgID(sel, arg)

		case "search":
			err = com.compileArgSearch(sel, arg)

		case "where":
			err = com.compileArgWhere(sel, arg, role)

		case "orderby", "order_by", "order":
			err = com.compileArgOrderBy(sel, arg)

		case "distinct_on", "distinct":
			err = com.compileArgDistinctOn(sel, arg)

		case "limit":
			err = com.compileArgLimit(sel, arg)

		case "offset":
			err = com.compileArgOffset(sel, arg)

		case "first":
			err = com.compileArgFirstLast(sel, arg, PtForward)

		case "last":
			err = com.compileArgFirstLast(sel, arg, PtBackward)

		case "after":
			err = com.compileArgAfterBefore(sel, arg, PtForward)

		case "before":
			err = com.compileArgAfterBefore(sel, arg, PtBackward)
		}

		if err != nil {
			return err
		}
	}

	return nil
}

func (com *Compiler) setMutationType(qc *QCode, args []Arg) error {
	setActionVar := func(arg *Arg) error {
		if arg.Val.Type != NodeVar {
			return argErr(arg.Name, "variable")
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

			if arg.Val.Type != NodeBool {
				return argErr(arg.Name, "boolen")
			}

			if arg.Val.Val == "false" {
				qc.Type = QTQuery
			}
			return nil
		}
	}

	return nil
}

func (com *Compiler) compileArgObj(st *util.Stack, arg *Arg) (*Exp, bool, error) {
	if arg.Val.Type != NodeObj {
		return nil, false, fmt.Errorf("expecting an object")
	}

	return com.compileArgNode(st, arg.Val, true)
}

func (com *Compiler) compileArgNode(st *util.Stack, node *Node, usePool bool) (*Exp, bool, error) {
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

		node, ok := intf.(*Node)
		if !ok || node == nil {
			return nil, needsUser, fmt.Errorf("16: unexpected value %v (%t)", intf, intf)
		}

		// Objects inside a list
		if node.Name == "" {
			pushChildren(st, node.exp, node)
			continue
		}

		ex, err := newExp(st, node, usePool)
		if err != nil {
			return nil, needsUser, err
		}

		if ex == nil {
			continue
		}

		if ex.Type == ValVar && ex.Val == "user_id" {
			needsUser = true
		}

		if node.exp == nil {
			root = ex
		} else {
			node.exp.Children = append(node.exp.Children, ex)
		}
	}

	return root, needsUser, nil
}

func (com *Compiler) compileArgID(sel *Select, arg *Arg) error {
	if sel.ID != 0 {
		return nil
	}

	if sel.Where != nil && sel.Where.Op == OpEqID {
		return nil
	}

	if arg.Val.Type != NodeVar {
		return argErr("id", "variable")
	}

	ex := expPool.Get().(*Exp)
	ex.Reset()

	ex.Op = OpEqID
	ex.Type = ValVar
	ex.Val = arg.Val.Val

	sel.Where = ex
	return nil
}

func (com *Compiler) compileArgSearch(sel *Select, arg *Arg) error {
	if arg.Val.Type != NodeVar {
		return argErr("search", "variable")
	}

	ex := expPool.Get().(*Exp)
	ex.Reset()

	ex.Op = OpTsQuery
	ex.Type = ValVar
	ex.Val = arg.Val.Val

	if sel.Args == nil {
		sel.Args = make(map[string]*Node)
	}

	sel.Args[arg.Name] = arg.Val
	arg.df = true
	AddFilter(sel, ex)

	return nil
}

func (com *Compiler) compileArgWhere(sel *Select, arg *Arg, role string) error {
	st := util.NewStack()
	var err error

	ex, nu, err := com.compileArgObj(st, arg)
	if err != nil {
		return err
	}

	if nu && role == "anon" {
		sel.SkipRender = SkipTypeUserNeeded
	}
	AddFilter(sel, ex)

	return nil
}

func (com *Compiler) compileArgOrderBy(sel *Select, arg *Arg) error {
	if arg.Val.Type != NodeObj {
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

		if node.Type != NodeStr && node.Type != NodeVar {
			return fmt.Errorf("expecting a string or variable")
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
	}
	return nil
}

func (com *Compiler) compileArgDistinctOn(sel *Select, arg *Arg) error {
	node := arg.Val

	if node.Type != NodeList && node.Type != NodeStr {
		return fmt.Errorf("expecting a list of strings or just a string")
	}

	if node.Type == NodeStr {
		sel.DistinctOn = append(sel.DistinctOn, node.Val)
	}

	for i := range node.Children {
		sel.DistinctOn = append(sel.DistinctOn, node.Children[i].Val)
	}

	return nil
}

func (com *Compiler) compileArgLimit(sel *Select, arg *Arg) error {
	node := arg.Val

	if node.Type != NodeNum {
		return argErr("limit", "number")
	}

	sel.Paging.Limit = node.Val

	return nil
}

func (com *Compiler) compileArgOffset(sel *Select, arg *Arg) error {
	node := arg.Val

	if node.Type != NodeVar {
		return argErr("offset", "variable")
	}

	sel.Paging.Offset = node.Val
	return nil
}

func (com *Compiler) compileArgFirstLast(sel *Select, arg *Arg, pt PagingType) error {
	node := arg.Val

	if node.Type != NodeNum {
		return argErr(arg.Name, "number")
	}

	sel.Paging.Type = pt
	sel.Paging.Limit = node.Val

	return nil
}

func (com *Compiler) compileArgAfterBefore(sel *Select, arg *Arg, pt PagingType) error {
	node := arg.Val

	if node.Type != NodeVar || node.Val != "cursor" {
		return fmt.Errorf("value for argument '%s' must be a variable named $cursor", arg.Name)
	}
	sel.Paging.Type = pt
	sel.Paging.Cursor = true

	return nil
}

// var zeroTrv = &trval{}

func (com *Compiler) getRole(role, field string) *trval {
	if trv, ok := com.tr[role][field]; ok {
		return trv
	}

	return nil
}

func AddFilter(sel *Select, fil *Exp) {
	if sel.Where != nil {
		ow := sel.Where

		if sel.Where.Op != OpAnd || !sel.Where.doFree {
			sel.Where = expPool.Get().(*Exp)
			sel.Where.Reset()
			sel.Where.Op = OpAnd
			sel.Where.Children = sel.Where.childrenA[:2]
			sel.Where.Children[0] = fil
			sel.Where.Children[1] = ow

		} else {
			sel.Where.Children = append(sel.Where.Children, fil)
		}

	} else {
		sel.Where = fil
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
		pushChildren(st, node.exp, node)
		return nil, nil // skip node
	}

	if ex.Op != OpAnd && ex.Op != OpOr && ex.Op != OpNot {
		switch node.Type {
		case NodeStr:
			ex.Type = ValStr
		case NodeNum:
			ex.Type = ValNum
		case NodeBool:
			ex.Type = ValBool
		case NodeList:
			ex.Type = ValList
		case NodeVar:
			ex.Type = ValVar
		default:
			return nil, fmt.Errorf("[Where] invalid values for: %s", name)
		}

		setWhereColName(ex, node)
	}

	return ex, nil
}

func setListVal(ex *Exp, node *Node) {
	if len(node.Children) != 0 {
		switch node.Children[0].Type {
		case NodeStr:
			ex.ListType = ValStr
		case NodeNum:
			ex.ListType = ValNum
		case NodeBool:
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

func setWhereColName(ex *Exp, node *Node) {
	var list []string

	for n := node.Parent; n != nil; n = n.Parent {
		if n.Type != NodeObj {
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
	listlen := len(list)

	if listlen == 1 {
		ex.Col = list[0]
	} else if listlen > 1 {
		ex.Col = list[listlen-1]
		ex.NestedCols = list[:listlen]
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

func compileFilter(filter []string) (*Exp, bool, error) {
	var fl *Exp
	var needsUser bool

	com := &Compiler{}
	st := util.NewStack()

	if len(filter) == 0 {
		return &Exp{Op: OpNop, doFree: false}, false, nil
	}

	for i := range filter {
		if filter[i] == "false" {
			return &Exp{Op: OpFalse, doFree: false}, false, nil
		}

		node, err := ParseArgValue(filter[i])
		if err != nil {
			return nil, false, err
		}
		f, nu, err := com.compileArgNode(st, node, false)
		if err != nil {
			return nil, false, err
		}
		if nu {
			needsUser = true
		}

		// TODO: Invalid table names in nested where causes fail silently
		// returning a nil 'f' this needs to be fixed

		// TODO: Invalid where clauses such as missing op (eg. eq) also fail silently

		if fl == nil {
			fl = f
		} else {
			fl = &Exp{Op: OpAnd, Children: []*Exp{fl, f}, doFree: false}
		}
	}
	return fl, needsUser, nil
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

func argErr(name, ty string) error {
	return fmt.Errorf("value for argument '%s' must be a %s", name, ty)
}

func freeNodes(op *Operation) {
	var st *util.Stack
	fm := make(map[*Node]struct{})

	for i := range op.Args {
		arg := op.Args[i]
		if arg.df {
			continue
		}

		for i := range arg.Val.Children {
			if st == nil {
				st = util.NewStack()
			}
			c := arg.Val.Children[i]
			if _, ok := fm[c]; !ok {
				st.Push(c)
			}
		}

		if _, ok := fm[arg.Val]; !ok {
			nodePool.Put(arg.Val)
			fm[arg.Val] = struct{}{}
		}

	}

	for i := range op.Fields {
		f := op.Fields[i]

		for j := range f.Args {
			arg := f.Args[j]
			if arg.df {
				continue
			}

			for k := range arg.Val.Children {
				if st == nil {
					st = util.NewStack()
				}
				c := arg.Val.Children[k]
				if _, ok := fm[c]; !ok {
					st.Push(c)
				}
			}

			if _, ok := fm[arg.Val]; !ok {
				nodePool.Put(arg.Val)
				fm[arg.Val] = struct{}{}
			}
		}
	}

	if st == nil {
		return
	}

	for {
		if st.Len() == 0 {
			break
		}
		intf := st.Pop()
		node, ok := intf.(*Node)
		if !ok || node == nil {
			continue
		}

		for i := range node.Children {
			st.Push(node.Children[i])
		}

		if _, ok := fm[node]; !ok {
			nodePool.Put(node)
			fm[node] = struct{}{}
		}
	}
}

func (ex *Exp) IsFromQuery() bool {
	return !ex.internal
}
