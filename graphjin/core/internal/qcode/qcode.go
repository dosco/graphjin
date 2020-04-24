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

type QType int
type Action int

const (
	maxSelectors = 30
)

const (
	QTQuery QType = iota + 1
	QTMutation
	QTInsert
	QTUpdate
	QTDelete
	QTUpsert
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
	SkipRender bool
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
	ValInt
	ValFloat
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
	db bool // default block tables if not defined in anon role
	tr map[string]map[string]*trval
	bl map[string]struct{}
}

var expPool = sync.Pool{
	New: func() interface{} { return &Exp{doFree: true} },
}

func NewCompiler(c Config) (*Compiler, error) {
	co := &Compiler{db: c.DefaultBlock}
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

func NewFilter() *Exp {
	ex := expPool.Get().(*Exp)
	ex.Reset()

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

	// insert config
	trv.insert.fil, trv.insert.filNU, err = compileFilter(trc.Insert.Filters)
	if err != nil {
		return err
	}
	trv.insert.cols = listToMap(trc.Insert.Columns)
	trv.insert.psmap = parsePresets(trc.Insert.Presets)
	trv.insert.pslist = mapToList(trv.insert.psmap)

	// update config
	trv.update.fil, trv.update.filNU, err = compileFilter(trc.Update.Filters)
	if err != nil {
		return err
	}
	trv.update.cols = listToMap(trc.Update.Columns)
	trv.update.psmap = parsePresets(trc.Update.Presets)
	trv.update.pslist = mapToList(trv.update.psmap)

	// delete config
	trv.delete.fil, trv.delete.filNU, err = compileFilter(trc.Delete.Filters)
	if err != nil {
		return err
	}
	trv.delete.cols = listToMap(trc.Delete.Columns)

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

		if _, ok := com.bl[field.Name]; ok {
			continue
		}

		if field.ParentID == -1 {
			parentID = -1
		}

		trv := com.getRole(role, field.Name)

		selects = append(selects, Select{
			ID:        id,
			ParentID:  parentID,
			Name:      field.Name,
			Children:  make([]int32, 0, 5),
			Allowed:   trv.allowedColumns(action),
			Functions: true,
		})
		s := &selects[(len(selects) - 1)]

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

		if len(field.Alias) != 0 {
			s.FieldName = field.Alias
		} else {
			s.FieldName = s.Name
		}

		err := com.compileArgs(qc, s, field.Args, role)
		if err != nil {
			return err
		}

		// Order is important AddFilters must come after compileArgs
		com.AddFilters(qc, s, role)

		if s.ParentID == -1 {
			qc.Roots = append(qc.Roots, s.ID)
		} else {
			p := &selects[s.ParentID]
			p.Children = append(p.Children, s.ID)
		}

		s.Cols = make([]Column, 0, len(field.Children))
		action = QTQuery

		for _, cid := range field.Children {
			f := op.Fields[cid]

			if _, ok := com.bl[f.Name]; ok {
				continue
			}

			if len(f.Children) != 0 {
				val := f.ID | (s.ID << 16)
				st.Push(val)
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

	qc.Selects = selects[:id]
	return nil
}

func (com *Compiler) AddFilters(qc *QCode, sel *Select, role string) {
	var fil *Exp
	var nu bool // user required (or not) in this filter

	if trv, ok := com.tr[role][sel.Name]; ok {
		fil, nu = trv.filter(qc.Type)

	} else if com.db && role == "anon" {
		// Tables not defined under the anon role will not be rendered
		sel.SkipRender = true
	}

	if fil == nil {
		return
	}

	if nu && role == "anon" {
		sel.SkipRender = true
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

	// don't free this arg either previously done or will be free'd
	// in the future like in psql
	var df bool

	for i := range args {
		arg := &args[i]

		switch arg.Name {
		case "id":
			err, df = com.compileArgID(sel, arg)

		case "search":
			err, df = com.compileArgSearch(sel, arg)

		case "where":
			err, df = com.compileArgWhere(sel, arg, role)

		case "orderby", "order_by", "order":
			err, df = com.compileArgOrderBy(sel, arg)

		case "distinct_on", "distinct":
			err, df = com.compileArgDistinctOn(sel, arg)

		case "limit":
			err, df = com.compileArgLimit(sel, arg)

		case "offset":
			err, df = com.compileArgOffset(sel, arg)

		case "first":
			err, df = com.compileArgFirstLast(sel, arg, PtForward)

		case "last":
			err, df = com.compileArgFirstLast(sel, arg, PtBackward)

		case "after":
			err, df = com.compileArgAfterBefore(sel, arg, PtForward)

		case "before":
			err, df = com.compileArgAfterBefore(sel, arg, PtBackward)
		}

		if !df {
			FreeNode(arg.Val, 5)
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

	if usePool {
		st.Push(node)

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
			FreeNode(node, 1)
		}
	}

	return root, needsUser, nil
}

func (com *Compiler) compileArgID(sel *Select, arg *Arg) (error, bool) {
	if sel.ID != 0 {
		return nil, false
	}

	if sel.Where != nil && sel.Where.Op == OpEqID {
		return nil, false
	}

	if arg.Val.Type != NodeVar {
		return argErr("id", "variable"), false
	}

	ex := expPool.Get().(*Exp)
	ex.Reset()

	ex.Op = OpEqID
	ex.Type = ValVar
	ex.Val = arg.Val.Val

	sel.Where = ex
	return nil, false
}

func (com *Compiler) compileArgSearch(sel *Select, arg *Arg) (error, bool) {
	if arg.Val.Type != NodeVar {
		return argErr("search", "variable"), false
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
	AddFilter(sel, ex)

	return nil, true
}

func (com *Compiler) compileArgWhere(sel *Select, arg *Arg, role string) (error, bool) {
	st := util.NewStack()
	var err error

	ex, nu, err := com.compileArgObj(st, arg)
	if err != nil {
		return err, false
	}

	if nu && role == "anon" {
		sel.SkipRender = true
	}
	AddFilter(sel, ex)

	return nil, true
}

func (com *Compiler) compileArgOrderBy(sel *Select, arg *Arg) (error, bool) {
	if arg.Val.Type != NodeObj {
		return fmt.Errorf("expecting an object"), false
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
			return fmt.Errorf("17: unexpected value %v (%t)", intf, intf), false
		}

		if _, ok := com.bl[node.Name]; ok {
			FreeNode(node, 2)
			continue
		}

		if node.Type != NodeStr && node.Type != NodeVar {
			return fmt.Errorf("expecting a string or variable"), false
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
			return fmt.Errorf("valid values include asc, desc, asc_nulls_first and desc_nulls_first"), false
		}

		setOrderByColName(ob, node)
		sel.OrderBy = append(sel.OrderBy, ob)
		FreeNode(node, 3)
	}
	return nil, false
}

func (com *Compiler) compileArgDistinctOn(sel *Select, arg *Arg) (error, bool) {
	node := arg.Val

	if _, ok := com.bl[node.Name]; ok {
		return nil, false
	}

	if node.Type != NodeList && node.Type != NodeStr {
		return fmt.Errorf("expecting a list of strings or just a string"), false
	}

	if node.Type == NodeStr {
		sel.DistinctOn = append(sel.DistinctOn, node.Val)
	}

	for i := range node.Children {
		sel.DistinctOn = append(sel.DistinctOn, node.Children[i].Val)
		FreeNode(node.Children[i], 5)
	}

	return nil, false
}

func (com *Compiler) compileArgLimit(sel *Select, arg *Arg) (error, bool) {
	node := arg.Val

	if node.Type != NodeInt {
		return argErr("limit", "number"), false
	}

	sel.Paging.Limit = node.Val

	return nil, false
}

func (com *Compiler) compileArgOffset(sel *Select, arg *Arg) (error, bool) {
	node := arg.Val

	if node.Type != NodeVar {
		return argErr("offset", "variable"), false
	}

	sel.Paging.Offset = node.Val
	return nil, false
}

func (com *Compiler) compileArgFirstLast(sel *Select, arg *Arg, pt PagingType) (error, bool) {
	node := arg.Val

	if node.Type != NodeInt {
		return argErr(arg.Name, "number"), false
	}

	sel.Paging.Type = pt
	sel.Paging.Limit = node.Val

	return nil, false
}

func (com *Compiler) compileArgAfterBefore(sel *Select, arg *Arg, pt PagingType) (error, bool) {
	node := arg.Val

	if node.Type != NodeVar || node.Val != "cursor" {
		return fmt.Errorf("value for argument '%s' must be a variable named $cursor", arg.Name), false
	}
	sel.Paging.Type = pt
	sel.Paging.Cursor = true

	return nil, false
}

var zeroTrv = &trval{}

func (com *Compiler) getRole(role, field string) *trval {
	if trv, ok := com.tr[role][field]; ok {
		return trv
	} else {
		return zeroTrv
	}
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
	case "null_eq", "ndis", "not_distinct":
		ex.Op = OpNotDistinct
		ex.Val = node.Val
	case "null_neq", "dis", "distinct":
		ex.Op = OpDistinct
		ex.Val = node.Val
	default:
		pushChildren(st, node.exp, node)
		return nil, nil // skip node
	}

	if ex.Op != OpAnd && ex.Op != OpOr && ex.Op != OpNot {
		switch node.Type {
		case NodeStr:
			ex.Type = ValStr
		case NodeInt:
			ex.Type = ValInt
		case NodeBool:
			ex.Type = ValBool
		case NodeFloat:
			ex.Type = ValFloat
		case NodeList:
			ex.Type = ValList
		case NodeVar:
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
		case NodeStr:
			ex.ListType = ValStr
		case NodeInt:
			ex.ListType = ValInt
		case NodeBool:
			ex.ListType = ValBool
		case NodeFloat:
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
		if n.Type != NodeObj {
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
