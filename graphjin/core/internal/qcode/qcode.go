//go:generate stringer -type=QType,MType,SelType,SkipType,PagingType,AggregrateOp,ValType -output=./gen_string.go
package qcode

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/dosco/graphjin/core/internal/graph"
	"github.com/dosco/graphjin/core/internal/sdata"
	"github.com/dosco/graphjin/core/internal/util"
)

const (
	maxSelectors = 30
)

type QType int8

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

type SelType int8

const (
	SelTypeNone SelType = iota
	SelTypeUnion
	SelTypeMember
)

type SkipType int8

const (
	SkipTypeNone SkipType = iota
	SkipTypeUserNeeded
	SkipTypeRemote
)

type QCode struct {
	Type      QType
	SType     QType
	ActionVar string
	Selects   []Select
	Vars      Variables
	Roots     []int32
	rootsA    [5]int32
	Mutates   []Mutate
	MUnions   map[string][]int32
	Schema    *sdata.DBSchema
	Remotes   int32
}

type Select struct {
	ID         int32
	ParentID   int32
	UParentID  int32
	Type       SelType
	Singular   bool
	Typename   bool
	Table      string
	FieldName  string
	Cols       []Column
	ColMap     map[string]int
	ArgMap     map[string]Arg
	Funcs      []Function
	Where      Filter
	OrderBy    []OrderBy
	GroupCols  bool
	DistinctOn []sdata.DBColumn
	Paging     Paging
	Children   []int32
	SkipRender SkipType
	Ti         sdata.DBTableInfo
	Rel        sdata.DBRel
	order      Order
	through    string
}

type Column struct {
	Col       sdata.DBColumn
	FieldName string
	Base      bool
}

type Function struct {
	Name      string
	Col       sdata.DBColumn
	FieldName string
	skip      bool
}

type Filter struct {
	*Exp
}

type Exp struct {
	Op        ExpOp
	Table     string
	Rels      []sdata.DBRel
	Col       sdata.DBColumn
	Type      ValType
	Val       string
	ListType  ValType
	ListVal   []string
	Children  []*Exp
	childrenA [5]*Exp
	internal  bool
	doFree    bool
}

type Arg struct {
	Val string
}

var zeroExp = Exp{doFree: true}

func (ex *Exp) Reset() {
	*ex = zeroExp
}

func (ex *Exp) Free() {
	if !ex.doFree {
		expPool.Put(ex)
	}
}

type OrderBy struct {
	Col   sdata.DBColumn
	Order Order
}

type PagingType int8

const (
	PTOffset PagingType = iota
	PTForward
	PTBackward
)

type Paging struct {
	Type      PagingType
	LimitVar  string
	Limit     int32
	OffsetVar string
	Offset    int32
	Cursor    bool
	NoLimit   bool
}

type ExpOp int8

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
	OpRegex
	OpNotRegex
	OpIRegex
	OpNotIRegex
	OpContains
	OpContainedIn
	OpHasKey
	OpHasKeyAny
	OpHasKeyAll
	OpIsNull
	OpTsQuery
	OpFalse
	OpNotDistinct
	OpDistinct
	OpEqualsTrue
	OpNotEqualsTrue
)

type ValType int8

const (
	ValStr ValType = iota + 1
	ValNum
	ValBool
	ValList
	ValVar
	ValNone
	ValRef
)

type AggregrateOp int8

const (
	AgCount AggregrateOp = iota + 1
	AgSum
	AgAvg
	AgMax
	AgMin
)

type Order int8

const (
	OrderAsc Order = iota + 1
	OrderDesc
	OrderAscNullsFirst
	OrderAscNullsLast
	OrderDescNullsFirst
	OrderDescNullsLast
)

type Compiler struct {
	c  Config
	s  *sdata.DBSchema
	tr map[string]trval
}

var expPool = sync.Pool{
	New: func() interface{} { return &Exp{doFree: true} },
}

func NewCompiler(s *sdata.DBSchema, c Config) (*Compiler, error) {
	seedExp := [100]Exp{}

	for i := range seedExp {
		seedExp[i].doFree = true
		expPool.Put(&seedExp[i])
	}

	c.defTrv.query.block = c.DefaultBlock
	c.defTrv.insert.block = c.DefaultBlock
	c.defTrv.update.block = c.DefaultBlock
	c.defTrv.upsert.block = c.DefaultBlock
	c.defTrv.delete.block = c.DefaultBlock

	return &Compiler{c: c, s: s, tr: make(map[string]trval)}, nil
}

func NewFilter() *Exp {
	ex := expPool.Get().(*Exp)
	ex.Reset()
	ex.internal = true

	return ex
}

type Variables map[string]json.RawMessage

func (co *Compiler) Compile(query []byte, vars Variables, role string) (*QCode, error) {
	var err error

	qc := QCode{SType: QTQuery, Schema: co.s, Vars: vars}
	qc.Roots = qc.rootsA[:0]

	op, err := graph.Parse(query, co.c.FragmentFetcher)
	if err != nil {
		return nil, err
	}

	switch op.Type {
	case graph.OpQuery:
		qc.Type = QTQuery
	case graph.OpSub:
		qc.Type = QTSubscription
	case graph.OpMutate:
		qc.Type = QTMutation
	default:
		return nil, fmt.Errorf("invalid operation: %s", op.Type)
	}

	if err := co.compileQuery(&qc, &op, role); err != nil {
		return nil, err
	}

	if qc.Type == QTMutation {
		if err = co.compileMutation(&qc, &op, role); err != nil {
			return nil, err
		}
	}

	return &qc, nil
}

func (co *Compiler) compileQuery(qc *QCode, op *graph.Operation, role string) error {
	var id int32

	if len(op.Fields) == 0 {
		return errors.New("invalid graphql no query found")
	}

	if op.Type == graph.OpMutate {
		if err := co.setMutationType(qc, op.Fields[0].Args); err != nil {
			return err
		}
	}

	qc.Selects = make([]Select, 0, 5)
	st := util.NewStackInt32()

	if len(op.Fields) == 0 {
		return errors.New("empty query")
	}

	for _, f := range op.Fields {
		if f.ParentID == -1 {
			val := f.ID | (-1 << 16)
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

		// A keyword is a cursor field at the top-level
		// For example posts_cursor in the root
		if field.Type == graph.FieldKeyword {
			continue
		}

		if field.ParentID == -1 {
			parentID = -1
		}

		s1 := Select{
			ID:       id,
			ParentID: parentID,
			ColMap:   make(map[string]int, len(field.Children)),
		}
		sel := &s1

		if field.Alias != "" {
			sel.FieldName = field.Alias
		} else {
			sel.FieldName = field.Name
		}

		sel.Children = make([]int32, 0, 5)

		if err := co.compileDirectives(qc, sel, field.Directives); err != nil {
			return err
		}

		if err := co.addRelInfo(field, qc, sel); err != nil {
			return err
		}

		var tr trval

		if sel.Ti.IsAlias {
			tr = co.getRole(role, sel.Ti.Name)
		} else {
			tr = co.getRole(role, field.Name)
		}

		if tr.isSkipped(qc.Type) {
			sel.SkipRender = SkipTypeUserNeeded
		} else {
			if err := tr.isBlocked(qc.SType, field.Name); err != nil {
				return err
			}
		}

		co.setLimit(tr, qc, sel)

		if err := co.compileArgs(qc, sel, field.Args, role); err != nil {
			return err
		}

		if err := co.compileColumns(field, op, st, qc, sel, tr); err != nil {
			return err
		}

		// Order is important AddFilters must come after compileArgs
		if un := addFilters(qc, sel, tr); un && role == "anon" {
			sel.SkipRender = SkipTypeUserNeeded
		}

		// If an actual cursor is avalable
		if sel.Paging.Cursor {
			// Set tie-breaker order column for the cursor direction
			// this column needs to be the last in the order series.
			if err := co.orderByIDCol(sel); err != nil {
				return err
			}

			// Set filter chain needed to make the cursor work
			if sel.Paging.Type != PTOffset {
				co.addSeekPredicate(sel)
			}
		}

		if err := co.validateSelect(sel); err != nil {
			return err
		}

		qc.Selects = append(qc.Selects, s1)
		id++
	}

	if id == 0 {
		return errors.New("invalid query")
	}

	return nil
}

func (co *Compiler) addRelInfo(field *graph.Field, qc *QCode, sel *Select) error {
	var psel *Select
	var parent string
	var sinset bool
	var err error

	child := field.Name
	through := sel.through

	if sel.ParentID == -1 {
		qc.Roots = append(qc.Roots, sel.ID)
	} else {
		psel = &qc.Selects[sel.ParentID]
		psel.Children = append(psel.Children, sel.ID)
		parent = psel.Table
	}

	switch field.Type {
	case graph.FieldUnion:
		sel.Type = SelTypeUnion
		if psel == nil {
			return fmt.Errorf("union types are only valid with polymorphic relationships")
		}

	case graph.FieldMember:
		// TODO: Fix this
		// if sel.Table != sel.Table {
		// 	return fmt.Errorf("inline fragment: 'on %s' should be 'on %s'", sel.Table, sel.Table)
		// }
		sel.Type = SelTypeMember
		sel.Singular = psel.Singular
		sel.UParentID = psel.ParentID

		ppsel := qc.Selects[sel.UParentID]
		child = psel.Table
		parent = ppsel.Table
		through = ""
		sinset = true
	}

	if sel.Rel.Type == sdata.RelSkip {
		sel.Rel.Type = sdata.RelNone
		return nil
	}

	// if alias, ok := co.s.GetAliasTable(child, parent); ok {
	// 	child = alias
	// }

	if parent != "" {
		if sel.Rel, err = co.s.GetRel(child, parent, through); err != nil {
			return err
		}
	}

	if field.Type == graph.FieldMember {
		child = field.Name
	}

	if sel.Rel.Type == sdata.RelRemote {
		sel.Table = child
		qc.Remotes++
		return nil
	}

	// if alias, ok := co.s.GetAliasTable(child, parent); ok && alias != "" {
	// 	child = alias
	// }

	if sel.Ti, err = co.s.GetTableInfoB(child, parent); err != nil {
		return err
	}
	sel.Table = sel.Ti.Name

	if !sinset {
		sel.Singular = (sel.Ti.IsSingular)
	}
	return nil
}

func (co *Compiler) setLimit(tr trval, qc *QCode, sel *Select) {
	// Use limit from table role config
	if l := tr.limit(qc.Type); l != 0 {
		sel.Paging.Limit = l

		// Else use default limit from config
	} else if co.c.DefaultLimit != 0 {
		sel.Paging.Limit = int32(co.c.DefaultLimit)

		// Else just go with 20
	} else {
		sel.Paging.Limit = 20
	}
}

// This
// (A, B, C) >= (X, Y, Z)
//
// Becomes
// (A > X)
//   OR ((A = X) AND (B > Y))
//   OR ((A = X) AND (B = Y) AND (C > Z))
//   OR ((A = X) AND (B = Y) AND (C = Z)

func (co *Compiler) addSeekPredicate(sel *Select) {
	var or, and *Exp
	obLen := len(sel.OrderBy)

	if obLen != 0 {
		isnull := NewFilter()
		isnull.Op = OpIsNull
		isnull.Type = ValRef
		isnull.Table = "__cur"
		isnull.Col = sel.OrderBy[0].Col
		isnull.Val = "true"

		or = NewFilter()
		or.Op = OpOr
		or.Children = append(or.Children, isnull)
	}

	for i := 0; i < obLen; i++ {
		if i != 0 {
			and = NewFilter()
			and.Op = OpAnd
		}

		for n, ob := range sel.OrderBy {
			if n > i {
				break
			}

			f := NewFilter()
			f.Type = ValRef
			f.Table = "__cur"
			f.Col = ob.Col
			f.Val = ob.Col.Name

			switch {
			case i > 0 && n != i:
				f.Op = OpEquals
			case ob.Order == OrderDesc:
				f.Op = OpLesserThan
			default:
				f.Op = OpGreaterThan
			}

			if and != nil {
				and.Children = append(and.Children, f)
			} else {
				or.Children = append(or.Children, f)
			}
		}

		if and != nil {
			or.Children = append(or.Children, and)
		}
	}

	setFilter(sel, or)
}

func addFilters(qc *QCode, sel *Select, tr trval) bool {
	if fil, userNeeded := tr.filter(qc.SType); fil != nil {
		switch fil.Op {
		case OpNop:
		case OpFalse:
			sel.Where.Exp = fil
		default:
			setFilter(sel, fil)
		}
		return userNeeded
	}

	return false
}

func (co *Compiler) compileDirectives(qc *QCode, sel *Select, dirs []graph.Directive) error {
	var err error

	for i := range dirs {
		d := &dirs[i]

		switch d.Name {
		case "skip":
			err = co.compileDirectiveSkip(sel, d)

		case "include":
			err = co.compileDirectiveInclude(sel, d)

		case "not_related":
			err = co.compileDirectiveNotRelated(sel, d)

		case "through":
			err = co.compileDirectiveThrough(sel, d)
		}

		if err != nil {
			return err
		}
	}

	return nil
}

func (co *Compiler) compileArgs(qc *QCode, sel *Select, args []graph.Arg, role string) error {
	var err error

	for i := range args {
		arg := &args[i]

		switch arg.Name {
		case "id":
			err = co.compileArgID(sel, arg)

		case "search":
			err = co.compileArgSearch(sel, arg)

		case "where":
			err = co.compileArgWhere(sel.Ti, sel, arg, role)

		case "orderby", "order_by", "order":
			err = co.compileArgOrderBy(sel, arg)

		case "distinct_on", "distinct":
			err = co.compileArgDistinctOn(sel, arg)

		case "limit":
			err = co.compileArgLimit(sel, arg)

		case "offset":
			err = co.compileArgOffset(sel, arg)

		case "first":
			err = co.compileArgFirstLast(sel, arg, OrderAsc)

		case "last":
			err = co.compileArgFirstLast(sel, arg, OrderDesc)

		case "after":
			err = co.compileArgAfterBefore(sel, arg, PTForward)

		case "before":
			err = co.compileArgAfterBefore(sel, arg, PTBackward)

		case "find":
			err = co.compileArgFind(sel, arg)
		}

		if err != nil {
			return err
		}
	}

	return nil
}

func (co *Compiler) validateSelect(sel *Select) error {
	if sel.Rel.Type == sdata.RelRecursive {
		v, ok := sel.ArgMap["find"]
		if !ok {
			return fmt.Errorf("arguments: 'find' needed for recursive queries")
		}
		if v.Val != "parents" && v.Val != "children" {
			return fmt.Errorf("find: valid values are 'parents' and 'children'")
		}
	}
	return nil
}

func (co *Compiler) setMutationType(qc *QCode, args []graph.Arg) error {
	setActionVar := func(arg *graph.Arg) error {
		if arg.Val.Type != graph.NodeVar {
			return argErr(arg.Name, "variable")
		}
		qc.ActionVar = arg.Val.Val
		return nil
	}

	for i := range args {
		arg := &args[i]

		switch arg.Name {
		case "insert":
			qc.SType = QTInsert
			return setActionVar(arg)
		case "update":
			qc.SType = QTUpdate
			return setActionVar(arg)
		case "upsert":
			qc.SType = QTUpsert
			return setActionVar(arg)
		case "delete":
			qc.SType = QTDelete

			if arg.Val.Type != graph.NodeBool {
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

func (co *Compiler) compileDirectiveSkip(sel *Select, d *graph.Directive) error {
	if len(d.Args) == 0 || d.Args[0].Name != "if" {
		return fmt.Errorf("@skip: required argument 'if' missing")
	}
	arg := d.Args[0]

	if arg.Val.Type != graph.NodeVar {
		return argErr("if", "variable")
	}

	ex := expPool.Get().(*Exp)
	ex.Reset()

	ex.Op = OpNotEqualsTrue
	ex.Type = ValVar
	ex.Val = arg.Val.Val

	setFilter(sel, ex)
	return nil
}

func (co *Compiler) compileDirectiveInclude(sel *Select, d *graph.Directive) error {
	if len(d.Args) == 0 || d.Args[0].Name != "if" {
		return fmt.Errorf("@include: required argument 'if' missing")
	}
	arg := d.Args[0]

	if arg.Val.Type != graph.NodeVar {
		return argErr("if", "variable")
	}

	ex := expPool.Get().(*Exp)
	ex.Reset()

	ex.Op = OpEqualsTrue
	ex.Type = ValVar
	ex.Val = arg.Val.Val

	setFilter(sel, ex)
	return nil
}

func (co *Compiler) compileDirectiveNotRelated(sel *Select, d *graph.Directive) error {
	sel.Rel.Type = sdata.RelSkip
	return nil
}

func (co *Compiler) compileDirectiveThrough(sel *Select, d *graph.Directive) error {
	if len(d.Args) == 0 || d.Args[0].Name != "table" {
		return fmt.Errorf("@through: required argument 'table' missing")
	}
	arg := d.Args[0]

	if arg.Val.Type != graph.NodeStr {
		return argErr("table", "string")
	}
	sel.through = arg.Val.Val
	return nil
}

func (co *Compiler) compileArgFind(sel *Select, arg *graph.Arg) error {
	// Only allow on recursive relationship selectors
	if sel.Rel.Type != sdata.RelRecursive {
		return fmt.Errorf("find: selector '%s' is not recursive", sel.FieldName)
	}
	if arg.Val.Val != "parents" && arg.Val.Val != "children" {
		return fmt.Errorf("find: valid values 'parents' or 'children'")
	}
	sel.addArg(arg)
	return nil
}

func (co *Compiler) compileArgID(sel *Select, arg *graph.Arg) error {
	if sel.ParentID != -1 {
		return fmt.Errorf("argument 'id' can only be specified at the query root")
	}

	if sel.Ti.PrimaryCol.Name == "" {
		return fmt.Errorf("no primary key column defined for %s", sel.Table)
	}

	if arg.Val.Type != graph.NodeVar {
		return argErr("id", "variable")
	}

	ex := expPool.Get().(*Exp)
	ex.Reset()

	ex.Op = OpEquals
	ex.Type = ValVar
	ex.Val = arg.Val.Val
	ex.Col = sel.Ti.PrimaryCol

	sel.Where.Exp = ex
	return nil
}

func (co *Compiler) compileArgSearch(sel *Select, arg *graph.Arg) error {
	if co.s.Type() == "mysql" {
		return fmt.Errorf("mysql: search not supported")
	}
	if sel.Ti.TSVCol.Name == "" {
		return fmt.Errorf("no tsv column defined for %s", sel.Table)
	}

	if arg.Val.Type != graph.NodeVar {
		return argErr("search", "variable")
	}

	ex := expPool.Get().(*Exp)
	ex.Reset()

	ex.Op = OpTsQuery
	ex.Type = ValVar
	ex.Val = arg.Val.Val

	sel.addArg(arg)
	setFilter(sel, ex)

	return nil
}

func (co *Compiler) compileArgWhere(ti sdata.DBTableInfo, sel *Select, arg *graph.Arg, role string) error {
	st := util.NewStackInf()
	var err error

	ex, nu, err := co.compileArgObj(ti, st, arg)
	if err != nil {
		return err
	}

	if nu && role == "anon" {
		sel.SkipRender = SkipTypeUserNeeded
	}
	setFilter(sel, ex)
	return nil
}

func (co *Compiler) compileArgOrderBy(sel *Select, arg *graph.Arg) error {
	if arg.Val.Type != graph.NodeObj {
		return fmt.Errorf("expecting an object")
	}

	var cm map[string]struct{}

	if len(sel.OrderBy) != 0 {
		cm = make(map[string]struct{})
		for _, ob := range sel.OrderBy {
			cm[ob.Col.Name] = struct{}{}
		}
	}

	st := util.NewStackInf()

	for i := range arg.Val.Children {
		st.Push(arg.Val.Children[i])
	}

	obList := make([]OrderBy, 0, 2)

	for {
		if st.Len() == 0 {
			break
		}

		intf := st.Pop()
		node, ok := intf.(*graph.Node)

		if !ok || node == nil {
			return fmt.Errorf("17: unexpected value %v (%t)", intf, intf)
		}

		if node.Type != graph.NodeStr && node.Type != graph.NodeVar {
			return fmt.Errorf("expecting a string or variable")
		}

		ob := OrderBy{}

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

		if err := setOrderByColName(sel.Ti, &ob, node); err != nil {
			return err
		}
		if _, ok := cm[ob.Col.Name]; ok {
			return fmt.Errorf("duplicate column in order by: %s", ob.Col.Name)
		}
		obList = append(obList, ob)
	}

	for i := len(obList) - 1; i >= 0; i-- {
		sel.OrderBy = append(sel.OrderBy, obList[i])
	}

	return nil
}

func (co *Compiler) compileArgDistinctOn(sel *Select, arg *graph.Arg) error {
	node := arg.Val

	if node.Type != graph.NodeList && node.Type != graph.NodeStr {
		return fmt.Errorf("expecting a list of strings or just a string")
	}

	if node.Type == graph.NodeStr {
		if col, err := sel.Ti.GetColumn(node.Val); err == nil {
			sel.DistinctOn = append(sel.DistinctOn, col)
		} else {
			return err
		}
	}

	for _, node := range node.Children {
		if col, err := sel.Ti.GetColumn(node.Val); err == nil {
			sel.DistinctOn = append(sel.DistinctOn, col)
		} else {
			return err
		}
	}

	return nil
}

func (co *Compiler) compileArgLimit(sel *Select, arg *graph.Arg) error {
	node := arg.Val

	if node.Type != graph.NodeNum && node.Type != graph.NodeVar {
		return argErr("limit", "number or variable")
	}

	switch node.Type {
	case graph.NodeNum:
		if n, err := strconv.ParseInt(node.Val, 10, 32); err != nil {
			return err
		} else {
			sel.Paging.Limit = int32(n)
		}

	case graph.NodeVar:
		sel.Paging.LimitVar = node.Val
	}
	return nil
}

func (co *Compiler) compileArgOffset(sel *Select, arg *graph.Arg) error {
	node := arg.Val

	if node.Type != graph.NodeNum && node.Type != graph.NodeVar {
		return argErr("offset", "number or variable")
	}

	switch node.Type {
	case graph.NodeNum:
		if n, err := strconv.ParseInt(node.Val, 10, 32); err != nil {
			return err
		} else {
			sel.Paging.Offset = int32(n)
		}

	case graph.NodeVar:
		sel.Paging.OffsetVar = node.Val
	}
	return nil
}

func (co *Compiler) compileArgFirstLast(sel *Select, arg *graph.Arg, order Order) error {
	if err := co.compileArgLimit(sel, arg); err != nil {
		return err
	}

	if !sel.Singular {
		sel.Paging.Cursor = true
	}

	sel.order = order
	return nil
}

func (co *Compiler) compileArgAfterBefore(sel *Select, arg *graph.Arg, pt PagingType) error {
	node := arg.Val

	if node.Type != graph.NodeVar || node.Val != "cursor" {
		return fmt.Errorf("value for argument '%s' must be a variable named $cursor", arg.Name)
	}
	sel.Paging.Type = pt
	if !sel.Singular {
		sel.Paging.Cursor = true
	}

	return nil
}

func setFilter(sel *Select, fil *Exp) {
	if sel.Where.Exp != nil {
		ow := sel.Where.Exp

		if sel.Where.Op != OpAnd || !sel.Where.doFree {
			sel.Where.Exp = expPool.Get().(*Exp)
			sel.Where.Reset()
			sel.Where.Op = OpAnd
			sel.Where.Children = sel.Where.childrenA[:2]
			sel.Where.Children[0] = fil
			sel.Where.Children[1] = ow

		} else {
			sel.Where.Children = append(sel.Where.Children, fil)
		}

	} else {
		sel.Where.Exp = fil
	}
}

func setOrderByColName(ti sdata.DBTableInfo, ob *OrderBy, node *graph.Node) error {
	var list []string

	for n := node; n != nil; n = n.Parent {
		if n.Name != "" {
			list = append([]string{n.Name}, list...)
		}
	}
	if len(list) != 0 {
		col, err := ti.GetColumn(buildPath(list))
		if err != nil {
			return err
		}
		ob.Col = col
	}
	return nil
}

func compileFilter(ti sdata.DBTableInfo, filter []string) (*Exp, bool, error) {
	var fl *Exp
	var needsUser bool

	co := &Compiler{}
	st := util.NewStackInf()

	if len(filter) == 0 {
		return &Exp{Op: OpNop, doFree: false}, false, nil
	}

	for _, v := range filter {
		if v == "false" {
			return &Exp{Op: OpFalse, doFree: false}, false, nil
		}

		node, err := graph.ParseArgValue(v)
		if err != nil {
			return nil, false, err
		}

		f, nu, err := co.compileArgNode(ti, st, node, false)
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

// func isQueryBlocked(qc *QCode, k string, tr trval) error {
// 	switch {
// 	case qc.Type == QTQuery && tr.query.block:
// 		return fmt.Errorf("query blocked: %s", k)

// 	case qc.SType == QTUpsert && tr.insert.block || tr.update.block:
// 		return fmt.Errorf("upsert blocked: %s", k)

// 	case qc.SType == QTDelete && tr.delete.block:
// 		return fmt.Errorf("delete blocked: %s", k)
// 	}
// 	return nil
// }

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
	case OpTsQuery:
		v = "op-ts-query"
	}
	return fmt.Sprintf("<%s>", v)
}

func argErr(name, ty string) error {
	return fmt.Errorf("value for argument '%s' must be a %s", name, ty)
}

// func freeNodes(op *graph.Operation) {
// 	var st *util.StackInf
// 	fm := make(map[*graph.Node]struct{})

// 	for i := range op.Args {
// 		arg := op.Args[i]

// 		for i := range arg.Val.Children {
// 			if st == nil {
// 				st = util.NewStackInf()
// 			}
// 			c := arg.Val.Children[i]
// 			if _, ok := fm[c]; !ok {
// 				st.Push(c)
// 			}
// 		}

// 		if _, ok := fm[arg.Val]; !ok {
// 			arg.Val.Free()
// 			fm[arg.Val] = struct{}{}
// 		}
// 	}

// 	for i := range op.Fields {
// 		f := op.Fields[i]

// 		for j := range f.Args {
// 			arg := f.Args[j]

// 			for k := range arg.Val.Children {
// 				if st == nil {
// 					st = util.NewStackInf()
// 				}
// 				c := arg.Val.Children[k]
// 				if _, ok := fm[c]; !ok {
// 					st.Push(c)
// 				}
// 			}

// 			if _, ok := fm[arg.Val]; !ok {
// 				arg.Val.Free()
// 				fm[arg.Val] = struct{}{}
// 			}
// 		}
// 	}

// 	if st == nil {
// 		return
// 	}

// 	for {
// 		if st.Len() == 0 {
// 			break
// 		}
// 		intf := st.Pop()
// 		node, ok := intf.(*graph.Node)
// 		if !ok || node == nil {
// 			continue
// 		}

// 		for i := range node.Children {
// 			st.Push(node.Children[i])
// 		}

// 		if _, ok := fm[node]; !ok {
// 			node.Free()
// 			fm[node] = struct{}{}
// 		}
// 	}
// }

func (sel *Select) addArg(arg *graph.Arg) {
	if sel.ArgMap == nil {
		sel.ArgMap = make(map[string]Arg)
	}
	sel.ArgMap[arg.Name] = Arg{Val: arg.Val.Val}
}

func (ex *Exp) IsFromQuery() bool {
	return !ex.internal
}
