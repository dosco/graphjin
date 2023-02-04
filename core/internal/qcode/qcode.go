//go:generate stringer -linecomment -type=QType,MType,SelType,FieldType,SkipType,PagingType,AggregrateOp,ValType,ExpOp -output=./gen_string.go
package qcode

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/graph"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
	"github.com/dosco/graphjin/core/v3/internal/util"
)

const (
	maxSelectors        = 100
	singularSuffixCamel = "ByID"
	singularSuffixSnake = "_by_id"
)

type QType int8

const (
	QTUnknown      QType = iota // Unknown
	QTQuery                     // Query
	QTSubscription              // Subcription
	QTMutation                  // Mutation
	QTInsert                    // Insert
	QTUpdate                    // Update
	QTDelete                    // Delete
	QTUpsert                    // Upsert
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
	SkipTypeDrop
	SkipTypeNulled
	SkipTypeUserNeeded
	SkipTypeBlocked
	SkipTypeRemote
)

type ColKey struct {
	Name string
	Base bool
}

type QCode struct {
	Type      QType
	SType     QType
	Name      string
	ActionVar string
	ActionVal json.RawMessage
	Vars      []Var
	Selects   []Select
	Consts    []Constraint
	Roots     []int32
	rootsA    [5]int32
	Mutates   []Mutate
	MUnions   map[string][]int32
	Schema    *sdata.DBSchema
	Remotes   int32
	Cache     Cache
	Typename  bool
	Query     []byte
	Fragments []Fragment
	actionArg graph.Arg
}

type Fragment struct {
	Name  string
	Value []byte
}

type Select struct {
	Field
	Type       SelType
	Singular   bool
	Typename   bool
	Table      string
	Schema     string
	Fields     []Field
	BCols      []Column
	IArgs      []Arg
	Where      Filter
	OrderBy    []OrderBy
	DistinctOn []sdata.DBColumn
	GroupCols  bool
	Paging     Paging
	Children   []int32
	Ti         sdata.DBTable
	Rel        sdata.DBRel
	Joins      []Join
	order      Order
	through    string
	tc         TConfig
}

type Validation struct {
	Source string
	Type   string
}

type Script struct {
	Source string
	Name   string
}

type TableInfo struct {
	sdata.DBTable
}

type FieldType int8

const (
	FieldTypeTable FieldType = iota
	FieldTypeCol
	FieldTypeFunc
)

type Field struct {
	ID          int32
	ParentID    int32
	Type        FieldType
	Col         sdata.DBColumn
	Func        sdata.DBFunction
	FieldName   string
	FieldFilter Filter
	Args        []Arg
	SkipRender  SkipType
}

type Column struct {
	Col         sdata.DBColumn
	FieldFilter Filter
	FieldName   string
}

type Function struct {
	Name string
	// Col       sdata.DBColumn
	Func sdata.DBFunction
	Args []Arg
	Agg  bool
}

type Filter struct {
	*Exp
}

type Exp struct {
	Op    ExpOp
	Joins []Join
	Order
	OrderBy bool

	Left struct {
		ID    int32
		Table string
		Col   sdata.DBColumn
	}
	Right struct {
		ValType  ValType
		Val      string
		ID       int32
		Table    string
		Col      sdata.DBColumn
		ListType ValType
		ListVal  []string
		Path     []string
	}
	Children  []*Exp
	childrenA [5]*Exp
}

type Join struct {
	Filter *Exp
	Rel    sdata.DBRel
	Local  bool
}

type ArgType int8

const (
	ArgTypeVal ArgType = iota
	ArgTypeVar
	ArgTypeCol
)

type Arg struct {
	Type  ArgType
	DType string
	Name  string
	Val   string
	Col   sdata.DBColumn
}

type OrderBy struct {
	KeyVar string
	Key    string
	Col    sdata.DBColumn
	Var    string
	Order  Order
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

type Cache struct {
	Header string
}

type Var struct {
	Name string
	Val  json.RawMessage
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
	OpHasInCommon
	OpHasKey
	OpHasKeyAny
	OpHasKeyAll
	OpIsNull
	OpIsNotNull
	OpTsQuery
	OpFalse
	OpNotDistinct
	OpDistinct
	OpEqualsTrue
	OpNotEqualsTrue
	OpSelectExists
)

type ValType int8

const (
	ValStr ValType = iota + 1
	ValNum
	ValBool
	ValList
	ValObj
	ValVar
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
	OrderNone Order = iota
	OrderAsc
	OrderDesc
	OrderAscNullsFirst
	OrderAscNullsLast
	OrderDescNullsFirst
	OrderDescNullsLast
)

func (o Order) String() string {
	return []string{"None", "ASC", "DESC", "ASC NULLS FIRST", "ASC NULLS LAST", "DESC NULLLS FIRST", "DESC NULLS LAST"}[o]
}

type Compiler struct {
	c  Config
	s  *sdata.DBSchema
	tr map[string]trval
}

func NewCompiler(s *sdata.DBSchema, c Config) (*Compiler, error) {
	if c.DBSchema == "" {
		c.DBSchema = "public"
	}

	c.defTrv.query.block = c.DefaultBlock
	c.defTrv.insert.block = c.DefaultBlock
	c.defTrv.update.block = c.DefaultBlock
	c.defTrv.upsert.block = c.DefaultBlock
	c.defTrv.delete.block = c.DefaultBlock

	return &Compiler{c: c, s: s, tr: make(map[string]trval)}, nil
}

func (co *Compiler) Compile(
	query []byte,
	vmap map[string]json.RawMessage,
	role, namespace string,
) (qc *QCode, err error) {
	var op graph.Operation
	op, err = graph.Parse(query)
	if err != nil {
		return
	}

	qc = &QCode{
		Name:      op.Name,
		SType:     QTQuery,
		Schema:    co.s,
		Query:     op.Query,
		Fragments: make([]Fragment, len(op.Frags)),
		Vars:      make([]Var, len(op.VarDef)),
	}

	for i, f := range op.Frags {
		qc.Fragments[i] = Fragment{Name: f.Name, Value: f.Value}
	}

	var buf bytes.Buffer
	for i, v := range op.VarDef {
		graphNodeToJSON(v.Val, &buf)
		qc.Vars[i] = Var{Name: v.Name, Val: buf.Bytes()}
		buf.Reset()
	}

	qc.Roots = qc.rootsA[:0]
	qc.Type = GetQType(op.Type)

	if err = co.compileQuery(qc, &op, role); err != nil {
		return
	}

	if qc.Type == QTMutation {
		if err = co.compileMutation(qc, vmap, role); err != nil {
			return
		}
	}
	return
}

func (co *Compiler) compileQuery(qc *QCode, op *graph.Operation, role string) error {
	var id int32

	if len(op.Fields) == 0 {
		return errors.New("invalid graphql no query found")
	}

	if op.Type == graph.OpMutate {
		if err := co.setMutationType(qc, op, role); err != nil {
			return err
		}
	}
	if err := co.compileOpDirectives(qc, op.Directives); err != nil {
		return err
	}

	qc.Selects = make([]Select, 0, 5)
	st := util.NewStackInt32()

	if len(op.Fields) == 0 {
		return errors.New("empty query")
	}

	for _, f := range op.Fields {
		if f.ParentID == -1 {
			if f.Name == "__typename" && op.Name != "" {
				qc.Typename = true
			}
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

		field := op.Fields[fid]

		// A keyword is a cursor field at the top-level
		// For example posts_cursor in the root
		if field.Type == graph.FieldKeyword {
			continue
		}

		if field.ParentID == -1 {
			parentID = -1
		}

		s1 := Select{
			Field: Field{ID: id, ParentID: parentID, Type: FieldTypeTable},
		}

		sel := &s1

		name := co.ParseName(field.Name)

		if field.Alias != "" {
			sel.FieldName = field.Alias
		} else {
			sel.FieldName = field.Name
		}

		sel.Children = make([]int32, 0, 5)

		if err := co.compileSelectorDirectives(qc, sel, field.Directives, role); err != nil {
			return err
		}

		if err := co.addRelInfo(name, op, qc, sel, field); err != nil {
			return err
		}

		tr, err := co.setSelectorRoleConfig(role, name, qc, sel)
		if err != nil {
			return err
		}

		co.setLimit(tr, qc, sel)

		if err := co.compileSelectArgs(sel, field.Args, role); err != nil {
			return err
		}

		if err := co.compileFields(st, op, qc, sel, field, tr, role); err != nil {
			return err
		}

		// Order is important AddFilters must come after compileArgs
		if userNeeded := addFilters(qc, &sel.Where, tr); userNeeded && role == "anon" {
			sel.SkipRender = SkipTypeUserNeeded
		}

		// If an actual cursor is available
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

		// Compute and set the relevant where clause required to join
		// this table with its parent
		co.setRelFilters(qc, sel)

		if err := co.validateSelect(sel); err != nil {
			return err
		}

		qc.Selects = append(qc.Selects, s1)
		id++
	}

	if id == 0 {
		return errors.New("invalid query: no selectors found")
	}

	return nil
}

func (co *Compiler) addRelInfo(
	name string,
	op *graph.Operation,
	qc *QCode,
	sel *Select,
	field graph.Field,
) error {
	var psel *Select
	var childF, parentF graph.Field
	var err error

	childF = field

	if sel.ParentID == -1 {
		qc.Roots = append(qc.Roots, sel.ID)
	} else {
		psel = &qc.Selects[sel.ParentID]
		psel.Children = append(psel.Children, sel.ID)
		parentF = op.Fields[field.ParentID]
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

		childF = parentF
		parentF = op.Fields[int(parentF.ParentID)]
	}

	if sel.Rel.Type == sdata.RelSkip {
		sel.Rel.Type = sdata.RelNone
	} else if sel.ParentID != -1 {
		parentName := co.ParseName(parentF.Name)
		childName := co.ParseName(childF.Name)

		path, err := co.FindPath(childName, parentName, sel.through)
		if err != nil {
			return graphError(err, childName, parentName, sel.through)
		}
		sel.Rel = sdata.PathToRel(path[0])

		// for _, p := range path {
		// 	rel := sdata.PathToRel(p)
		// 	fmt.Println(childF.Name, parentF.Name,
		// 		"--->>>", rel.Left.Col.Table, rel.Left.Col.Name,
		// 		"|", rel.Right.Col.Table, rel.Right.Col.Name)
		// }

		rpath := path[1:]

		for i := len(rpath) - 1; i >= 0; i-- {
			p := rpath[i]
			rel := sdata.PathToRel(p)
			var pid int32
			if i == len(rpath)-1 {
				pid = sel.ParentID
			} else {
				pid = -1
			}
			sel.Joins = append(sel.Joins, Join{
				Rel:    rel,
				Filter: buildFilter(rel, pid),
			})
		}
	}

	if sel.ParentID == -1 ||
		sel.Rel.Type == sdata.RelPolymorphic ||
		sel.Rel.Type == sdata.RelNone {
		schema := co.c.DBSchema
		if sel.Schema != "" {
			schema = sel.Schema
		}
		if sel.Ti, err = co.Find(schema, name); err != nil {
			return err
		}
	} else {
		sel.Ti = sel.Rel.Left.Ti
	}

	if sel.Ti.Blocked {
		return fmt.Errorf("table: '%t' (%s) blocked", sel.Ti.Blocked, name)
	}

	sel.Table = sel.Ti.Name
	sel.tc = co.getTConfig(sel.Ti.Schema, sel.Ti.Name)

	if sel.Rel.Type == sdata.RelRemote {
		sel.Table = name
		qc.Remotes++
		return nil
	}

	co.setSingular(name, sel)
	return nil
}

func (co *Compiler) setRelFilters(qc *QCode, sel *Select) {
	rel := sel.Rel
	pid := sel.ParentID

	if len(sel.Joins) != 0 {
		pid = -1
	}

	switch rel.Type {
	case sdata.RelOneToOne, sdata.RelOneToMany:
		addAndFilter(&sel.Where, buildFilter(rel, pid))

	case sdata.RelEmbedded:
		addAndFilter(&sel.Where, buildFilter(rel, pid))

	case sdata.RelPolymorphic:
		pid = qc.Selects[sel.ParentID].ParentID
		ex := newExpOp(OpAnd)

		ex1 := newExpOp(OpEquals)
		ex1.Left.Table = sel.Ti.Name
		ex1.Left.Col = rel.Right.Col
		ex1.Right.ID = pid
		ex1.Right.Col = rel.Left.Col

		ex2 := newExpOp(OpEquals)
		ex2.Left.ID = pid
		ex2.Left.Col.Table = rel.Left.Col.Table
		ex2.Left.Col.Name = rel.Left.Col.FKeyCol
		ex2.Right.ValType = ValStr
		ex2.Right.Val = sel.Ti.Name

		ex.Children = []*Exp{ex1, ex2}
		addAndFilter(&sel.Where, ex)

	case sdata.RelRecursive:
		rcte := "__rcte_" + rel.Right.Ti.Name
		ex := newExpOp(OpAnd)
		ex1 := newExpOp(OpIsNotNull)
		ex2 := newExp()
		ex3 := newExp()

		v, _ := sel.GetInternalArg("find")
		switch v.Val {
		case "parents", "parent":
			ex1.Left.Table = rcte
			ex1.Left.Col = rel.Left.Col
			switch {
			case !rel.Left.Col.Array && rel.Right.Col.Array:
				ex2.Op = OpNotIn
				ex2.Left.Table = rcte
				ex2.Left.Col = rel.Left.Col
				ex2.Right.Table = rcte
				ex2.Right.Col = rel.Right.Col

				ex3.Op = OpIn
				ex3.Left.Table = rcte
				ex3.Left.Col = rel.Left.Col
				ex3.Right.Col = rel.Right.Col

			case rel.Left.Col.Array && !rel.Right.Col.Array:
				ex2.Op = OpNotIn
				ex2.Left.Table = rcte
				ex2.Left.Col = rel.Right.Col
				ex2.Right.Table = rcte
				ex2.Right.Col = rel.Left.Col

				ex3.Op = OpIn
				ex3.Left.Col = rel.Right.Col
				ex3.Right.Table = rcte
				ex3.Right.Col = rel.Left.Col

			default:
				ex2.Op = OpNotEquals
				ex2.Left.Table = rcte
				ex2.Left.Col = rel.Left.Col
				ex2.Right.Table = rcte
				ex2.Right.Col = rel.Right.Col

				ex3.Op = OpEquals
				ex3.Left.Col = rel.Right.Col
				ex3.Right.Table = rcte
				ex3.Right.Col = rel.Left.Col
			}

		default:
			ex1.Left.Col = rel.Left.Col
			switch {
			case !rel.Left.Col.Array && rel.Right.Col.Array:
				ex2.Op = OpNotIn
				ex2.Left.Col = rel.Left.Col
				ex2.Right.Col = rel.Right.Col

				ex3.Op = OpIn
				ex3.Left.Col = rel.Left.Col
				ex3.Right.Table = rcte
				ex3.Right.Col = rel.Right.Col

			case rel.Left.Col.Array && !rel.Right.Col.Array:
				ex2.Op = OpNotIn
				ex2.Left.Col = rel.Right.Col
				ex2.Right.Col = rel.Left.Col

				ex3.Op = OpIn
				ex3.Left.Table = rcte
				ex3.Left.Col = rel.Right.Col
				ex3.Right.Col = rel.Left.Col

			default:
				ex2.Op = OpNotEquals
				ex2.Left.Col = rel.Left.Col
				ex2.Right.Col = rel.Right.Col

				ex3.Op = OpEquals
				ex3.Left.Col = rel.Left.Col
				ex3.Right.Table = rcte
				ex3.Right.Col = rel.Right.Col
			}
		}

		ex.Children = []*Exp{ex1, ex2, ex3}
		addAndFilter(&sel.Where, ex)
	}
}

func (co *Compiler) Find(schema, name string) (sdata.DBTable, error) {
	if co.c.EnableCamelcase {
		name = strings.TrimSuffix(name, singularSuffixSnake)
	} else {
		name = strings.TrimSuffix(name, singularSuffixCamel)
	}
	return co.s.Find(schema, name)
}

func (co *Compiler) FindPath(from, to, through string) ([]sdata.TPath, error) {
	if co.c.EnableCamelcase {
		from = strings.TrimSuffix(from, singularSuffixSnake)
		to = strings.TrimSuffix(to, singularSuffixSnake)
	} else {
		from = strings.TrimSuffix(from, singularSuffixCamel)
		to = strings.TrimSuffix(to, singularSuffixCamel)
	}
	return co.s.FindPath(from, to, through)
}

func buildFilter(rel sdata.DBRel, pid int32) *Exp {
	switch rel.Type {
	case sdata.RelOneToOne, sdata.RelOneToMany:
		ex := newExp()
		switch {
		case !rel.Left.Col.Array && rel.Right.Col.Array:
			ex.Op = OpIn
			ex.Left.Col = rel.Left.Col
			ex.Right.ID = pid
			ex.Right.Col = rel.Right.Col

		case rel.Left.Col.Array && !rel.Right.Col.Array:
			ex.Op = OpIn
			ex.Left.ID = pid
			ex.Left.Col = rel.Right.Col
			ex.Right.Col = rel.Left.Col

		default:
			ex.Op = OpEquals
			ex.Left.Col = rel.Left.Col
			ex.Right.ID = pid
			ex.Right.Col = rel.Right.Col
		}
		return ex

	case sdata.RelEmbedded:
		ex := newExpOp(OpEquals)
		ex.Left.Col = rel.Right.Col
		ex.Right.ID = pid
		ex.Right.Col = rel.Right.Col
		return ex

	default:
		return nil
	}
}

func (co *Compiler) setSingular(fieldName string, sel *Select) {
	if sel.Singular {
		return
	}

	if len(sel.Joins) != 0 {
		return
	}

	if (sel.Rel.Type == sdata.RelOneToMany && !sel.Rel.Right.Col.Array) ||
		sel.Rel.Type == sdata.RelPolymorphic {
		sel.Singular = true
		return
	}
}

func (co *Compiler) setSelectorRoleConfig(role, fieldName string, qc *QCode, sel *Select) (trval, error) {
	tr := co.getRole(role, sel.Ti.Schema, sel.Ti.Name, fieldName)

	if tr.isBlocked(qc.SType) {
		if qc.SType != QTQuery {
			return tr, fmt.Errorf("%s blocked: %s (role: %s)", qc.SType, fieldName, role)
		}
		sel.SkipRender = SkipTypeBlocked
	}
	return tr, nil
}

func (co *Compiler) setLimit(tr trval, qc *QCode, sel *Select) {
	if sel.Paging.Limit != 0 {
		return
	}
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
		or = newExpOp(OpOr)

		isnull := newExpOp(OpIsNull)
		isnull.Left.Table = "__cur"
		isnull.Left.Col = sel.OrderBy[0].Col

		or.Children = []*Exp{isnull}
	}

	for i := 0; i < obLen; i++ {
		if i != 0 {
			and = newExpOp(OpAnd)
		}

		for n, ob := range sel.OrderBy {
			if n > i {
				break
			}

			f := newExp()
			f.Left.Col = ob.Col
			f.Right.Table = "__cur"
			f.Right.Col = ob.Col

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
	addAndFilter(&sel.Where, or)
}

func (co *Compiler) validateSelect(sel *Select) error {
	if sel.Rel.Type == sdata.RelRecursive {
		v, ok := sel.GetInternalArg("find")
		if !ok {
			return fmt.Errorf("argument 'find' needed for recursive queries")
		}
		if v.Val != "parents" && v.Val != "children" {
			return fmt.Errorf("valid values for 'find' are 'parents' and 'children'")
		}
	}
	return nil
}

func addFilters(qc *QCode, where *Filter, trv trval) bool {
	if fil, userNeeded := trv.filter(qc.SType); fil != nil {
		switch fil.Op {
		case OpNop:
		case OpFalse:
			where.Exp = fil
		default:
			addAndFilter(where, fil)
		}
		return userNeeded
	}

	return false
}

func (co *Compiler) setMutationType(qc *QCode, op *graph.Operation, role string) error {
	var err error

	setActionVar := func(arg graph.Arg) error {
		v := arg.Val
		if v.Type != graph.NodeVar && v.Type != graph.NodeObj &&
			(v.Type != graph.NodeList || len(v.Children) == 0 && v.Children[0].Type != graph.NodeObj) {
			return argErr(arg, "variable, an object or a list of objects")
		}
		qc.ActionVar = arg.Val.Val
		qc.actionArg = arg
		return nil
	}

	args := op.Fields[0].Args

	for _, arg := range args {
		switch arg.Name {
		case "insert":
			qc.SType = QTInsert
			err = setActionVar(arg)
		case "update":
			qc.SType = QTUpdate
			err = setActionVar(arg)
		case "upsert":
			qc.SType = QTUpsert
			err = setActionVar(arg)
		case "delete":
			qc.SType = QTDelete
			if ifNotArg(arg, graph.NodeBool) || ifNotArgVal(arg, "true") {
				err = errors.New("value for 'delete' must be 'true'")
			}
		}

		if err != nil {
			return err
		}
	}

	if qc.SType == QTUnknown {
		return errors.New(`mutations must contains one of the following arguments (insert, update, upsert or delete)`)
	}

	return nil
}

func (co *Compiler) compileArgFilter(sel *Select,
	selID int32, arg graph.Arg, role string,
) (ex *Exp, err error) {
	st := util.NewStackInf()
	var nu bool

	if arg.Val.Type != graph.NodeObj {
		err = fmt.Errorf("expecting an object")
		return
	}

	ex, nu, err = co.compileExpNode(sel.Table,
		sel.Ti, st, arg.Val, false, selID)
	if err != nil {
		return
	}

	if nu && role == "anon" {
		sel.SkipRender = SkipTypeUserNeeded
	}
	return
}

func addAndFilterLast(fil *Filter, ex *Exp) {
	if fil.Exp == nil {
		fil.Exp = ex
		return
	}
	// save exiting exp pointer (could be a common one from filter config)
	ow := fil.Exp

	// add a new `and` exp and hook the above saved exp pointer a child
	// we don't want to modify an exp object thats common (from filter config)
	fil.Exp = newExpOp(OpAnd)
	fil.Exp.Children = fil.Exp.childrenA[:2]

	// here we append the filter to the last child
	fil.Exp.Children[0] = ow
	fil.Exp.Children[1] = ex
}

func addAndFilter(fil *Filter, ex *Exp) {
	if fil.Exp == nil {
		fil.Exp = ex
		return
	}
	// save exiting exp pointer (could be a common one from filter config)
	ow := fil.Exp

	// add a new `and` exp and hook the above saved exp pointer a child
	// we don't want to modify an exp object thats common (from filter config)
	fil.Exp = newExpOp(OpAnd)
	fil.Exp.Children = fil.Exp.childrenA[:2]
	fil.Exp.Children[0] = ex
	fil.Exp.Children[1] = ow
}

func addNotFilter(fil *Filter, ex *Exp) {
	ex1 := newExpOp(OpNot)
	ex1.Children = ex1.childrenA[:1]
	ex1.Children[0] = ex

	if fil.Exp == nil {
		fil.Exp = ex1
		return
	}
	// save exiting exp pointer (could be a common one from filter config)
	ow := fil.Exp

	// add a new `and` exp and hook the above saved exp pointer a child
	// we don't want to modify an exp object thats common (from filter config)
	fil.Exp = newExpOp(OpAnd)
	fil.Exp.Children = fil.Exp.childrenA[:2]
	fil.Exp.Children[0] = ex1
	fil.Exp.Children[1] = ow
}

func compileFilter(s *sdata.DBSchema, ti sdata.DBTable, filter []string, isJSON bool) (*Exp, bool, error) {
	var fl *Exp
	var needsUser bool

	co := &Compiler{s: s}
	st := util.NewStackInf()

	if len(filter) == 0 {
		return newExp(), false, nil
	}

	for _, v := range filter {
		if v == "false" {
			return newExpOp(OpFalse), false, nil
		}

		node, err := graph.ParseArgValue(v, isJSON)
		if err != nil {
			return nil, false, err
		}

		f, nu, err := co.compileBaseExpNode("", ti, st, node, isJSON)
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
			if len(filter) == 1 {
				fl = f
				continue
			} else {
				fl = newExpOp(OpAnd)
			}
		}
		fl.Children = append(fl.Children, f)
	}

	return fl, needsUser, nil
}

func getArg(args []graph.Arg, name string, validTypes ...graph.ParserType,
) (arg graph.Arg, err error) {
	var ok bool
	arg, ok, err = getOptionalArg(args, name, validTypes...)
	if err != nil {
		return
	}
	if !ok {
		err = reqArgMissing(name)
	}
	return
}

func getOptionalArg(args []graph.Arg, name string, validTypes ...graph.ParserType,
) (arg graph.Arg, ok bool, err error) {
	for _, arg = range args {
		if arg.Name != name {
			continue
		}
		if err = validateArg(arg, validTypes...); err != nil {
			return
		}
		ok = true
		return
	}
	return
}

// todo: add support for list of types
func validateArg(arg graph.Arg, validTypes ...graph.ParserType) (err error) {
	n := len(validTypes)
	for i := 0; i < n; i++ {
		vt := validTypes[i]
		ty := arg.Val.Type

		switch {
		case vt == graph.NodeList && ty != vt:
			continue
		case vt == graph.NodeList && ty == vt:
			if len(arg.Val.Children) == 0 {
				return
			}
			if (i + 1) >= n {
				continue
			}
			vt = validTypes[(i + 1)]
			ty = arg.Val.Children[0].Type
			i++
		}

		if ty == graph.NodeStr && arg.Val.Val == "" {
			continue
		}
		if ty == vt {
			return
		}
	}
	err = argErr(arg, argTypes(validTypes))
	return
}

func reqArgMissing(name string) (err error) {
	return fmt.Errorf("required argument '%s' missing", name)
}

func unknownArg(arg graph.Arg) (err error) {
	return fmt.Errorf("unknown argument '%s'", arg.Name)
}

func ifNotArg(arg graph.Arg, ty graph.ParserType) (ok bool) {
	return arg.Val.Type != ty
}

func ifNotArgVal(arg graph.Arg, val string) bool {
	return arg.Val.Val != val
}

func argTypes(types []graph.ParserType) string {
	var sb strings.Builder
	var list bool
	lastIndex := len(types) - 1
	for i, t := range types {
		if !list {
			if i == lastIndex {
				sb.WriteString(" or ")
			} else if i != 0 {
				sb.WriteString(", ")
			}
		}
		if t == graph.NodeList {
			sb.WriteString("a list of ")
			list = true
			continue
		}
		if !list {
			sb.WriteString("a ")
		}
		switch t {
		case graph.NodeBool:
			sb.WriteString("boolean")
		case graph.NodeNum:
			sb.WriteString("number")
		case graph.NodeLabel, graph.NodeStr:
			sb.WriteString("string")
		case graph.NodeObj:
			sb.WriteString("object")
		case graph.NodeVar:
			sb.WriteString("variable")
		}
		if list {
			sb.WriteString("s")
			list = false
		}

	}
	return sb.String()
}

func argErr(arg graph.Arg, ty string) error {
	return fmt.Errorf("value for argument '%s' must be %s", arg.Name, ty)
}

func dbArgErr(name, ty, db string) error {
	return fmt.Errorf("%s: value for argument '%s' must be a %s", db, name, ty)
}

func (sel *Select) addIArg(arg Arg) {
	sel.IArgs = append(sel.IArgs, arg)
}

func (sel *Select) GetInternalArg(name string) (Arg, bool) {
	var arg Arg
	for _, v := range sel.IArgs {
		if v.Name == name {
			return v, true
		}
	}
	return arg, false
}
