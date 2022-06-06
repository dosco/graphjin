//go:generate stringer -linecomment -type=QType,MType,SelType,SkipType,PagingType,AggregrateOp,ValType,ExpOp -output=./gen_string.go
package qcode

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"github.com/dosco/graphjin/core/internal/allow"
	"github.com/dosco/graphjin/core/internal/graph"
	"github.com/dosco/graphjin/core/internal/sdata"
	"github.com/dosco/graphjin/core/internal/util"
	"github.com/gobuffalo/flect"
)

const (
	maxSelectors = 100
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
	SkipTypeUserNeeded
	SkipTypeBlocked
	SkipTypeRemote
)

type ColKey struct {
	Name string
	Base bool
}

type QCode struct {
	Type       QType
	SType      QType
	Name       string
	ActionVar  string
	ActionArg  graph.Arg
	Selects    []Select
	Vars       Variables
	Consts     Constraints
	Roots      []int32
	rootsA     [5]int32
	Mutates    []Mutate
	MUnions    map[string][]int32
	Schema     *sdata.DBSchema
	Remotes    int32
	Script     string
	Metadata   allow.Metadata
	Cache      Cache
	Validation *Validation
}

type Select struct {
	ID         int32
	ParentID   int32
	Type       SelType
	Singular   bool
	Typename   bool
	Table      string
	FieldName  string
	Cols       []Column
	BCols      []Column
	Args       map[string]Arg
	Funcs      []Function
	Where      Filter
	OrderBy    []OrderBy
	GroupCols  bool
	DistinctOn []sdata.DBColumn
	Paging     Paging
	Children   []int32
	SkipRender SkipType
	Ti         sdata.DBTable
	Rel        sdata.DBRel
	Joins      []Join
	order      Order
	through    string
	tc         TConfig
}
type Validation struct {
	Cue  graph.Node
	Cuev cue.Value
}
type TableInfo struct {
	sdata.DBTable
}

type Column struct {
	Col       sdata.DBColumn
	FieldName string
}

type Function struct {
	Name      string
	Col       sdata.DBColumn
	FieldName string
	Alias     string
	skip      bool
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

type Arg struct {
	Val string
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

type Cache struct {
	Header string
}

type Variables map[string]json.RawMessage
type Constraints map[string]interface{}

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
	ValVar
	ValNone
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

func (o Order) String() string {
	return []string{"ASC", "DESC", "ASC NULLS FIRST", "ASC NULLS LAST", "DESC NULLLS FIRST", "DESC NULLS LAST"}[o-1]
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
	query []byte, vars Variables, role, namespace string) (*QCode, error) {
	var err error
	var fragFetch func(string) (string, error)

	if co.c.FragmentFetcher != nil {
		fragFetch = co.c.FragmentFetcher(namespace)
	}

	op, err := graph.Parse(query, fragFetch)
	if err != nil {
		return nil, err
	}

	qc := QCode{Name: op.Name, SType: QTQuery, Schema: co.s, Vars: vars}
	qc.Roots = qc.rootsA[:0]

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
		if err := co.compileMutation(&qc, role); err != nil {
			return nil, err
		}
	}

	if qc.Validation != nil {
		var (
			cuec *cue.Context
			cuev cue.Value
		)
		cuec = cuecontext.New()
		switch qc.Validation.Cue.Type {
		case graph.NodeVar:
			var o string
			if err = json.Unmarshal([]byte(qc.Vars[qc.Validation.Cue.Val]), &o); err != nil {
				return nil, errors.New("cue validation variable value is not valid")
			}
			cuev = cuec.CompileString(o)
		default:
			cuev = cuec.CompileString(qc.Validation.Cue.Val)
		}
		qc.Validation = &Validation{Cuev: cuev}
	}

	return &qc, nil
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
			ID:       id,
			ParentID: parentID,
		}
		sel := &s1

		if co.c.EnableCamelcase {
			if field.Alias == "" {
				field.Alias = field.Name
			}
			field.Name = util.ToSnake(field.Name)
		}

		if field.Alias != "" {
			sel.FieldName = field.Alias
		} else {
			sel.FieldName = field.Name
		}

		sel.Children = make([]int32, 0, 5)

		if err := co.compileDirectives(qc, sel, field.Directives); err != nil {
			return err
		}

		if err := co.addRelInfo(op, qc, sel, field); err != nil {
			return err
		}

		tr, err := co.setSelectorRole(role, field.Name, qc, sel)
		if err != nil {
			return err
		}

		co.setLimit(tr, qc, sel)

		if err := co.compileArgs(qc, sel, field.Args, role); err != nil {
			return err
		}

		if err := co.compileColumns(st, op, qc, sel, field, tr); err != nil {
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
		return errors.New("invalid query")
	}

	return nil
}

func (co *Compiler) addRelInfo(
	op *graph.Operation, qc *QCode, sel *Select, field graph.Field) error {
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
		if co.c.EnableCamelcase {
			parentF.Name = util.ToSnake(parentF.Name)
		}
		path, err := co.s.FindPath(childF.Name, parentF.Name, sel.through)
		if err != nil {
			return graphError(err, childF.Name, parentF.Name, sel.through)
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
		if sel.Ti, err = co.s.Find(schema, field.Name); err != nil {
			return err
		}
	} else {
		sel.Ti = sel.Rel.Left.Ti
	}

	if sel.Ti.Blocked {
		return fmt.Errorf("table: '%t' (%s) blocked", sel.Ti.Blocked, field.Name)
	}

	sel.Table = sel.Ti.Name
	sel.tc = co.getTConfig(sel.Ti.Schema, sel.Ti.Name)

	if sel.Rel.Type == sdata.RelRemote {
		sel.Table = field.Name
		qc.Remotes++
		return nil
	}

	co.setSingular(field.Name, sel)
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
		setFilter(&sel.Where, buildFilter(rel, pid))

	case sdata.RelEmbedded:
		setFilter(&sel.Where, buildFilter(rel, pid))

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
		setFilter(&sel.Where, ex)

	case sdata.RelRecursive:
		rcte := "__rcte_" + rel.Right.Ti.Name
		ex := newExpOp(OpAnd)
		ex1 := newExpOp(OpIsNotNull)
		ex2 := newExp()
		ex3 := newExp()

		v := sel.Args["find"]
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
		setFilter(&sel.Where, ex)
	}
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

	if co.c.EnableInflection {
		sel.Singular = (flect.Singularize(fieldName) == fieldName)
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

func (co *Compiler) setSelectorRole(role, fieldName string, qc *QCode, sel *Select) (trval, error) {
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

	setFilter(&sel.Where, or)
}

func addFilters(qc *QCode, where *Filter, trv trval) bool {
	if fil, userNeeded := trv.filter(qc.SType); fil != nil {
		switch fil.Op {
		case OpNop:
		case OpFalse:
			where.Exp = fil
		default:
			setFilter(where, fil)
		}
		return userNeeded
	}

	return false
}

func (co *Compiler) compileOpDirectives(qc *QCode, dirs []graph.Directive) error {
	var err error

	for i := range dirs {
		d := &dirs[i]

		switch d.Name {
		case "cacheControl":
			err = co.compileDirectiveCacheControl(qc, d)

		case "script":
			err = co.compileDirectiveScript(qc, d)

		case "constraint", "validate":
			err = co.compileDirectiveConstraint(qc, d)

		case "validation":
			err = co.compileDirectiveValidation(qc, d)

		default:
			err = fmt.Errorf("unknown operation level directive: %s", d.Name)
		}

		if err != nil {
			return err
		}
	}
	return nil
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

		case "notRelated", "not_related":
			err = co.compileDirectiveNotRelated(sel, d)

		case "through":
			err = co.compileDirectiveThrough(sel, d)

		case "object":
			sel.Singular = true
			sel.Paging.Limit = 1

		default:
			err = fmt.Errorf("unknown selector level directive: %s", d.Name)
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
			err = co.compileArgOrderBy(qc, sel, arg)

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
		v, ok := sel.Args["find"]
		if !ok {
			return fmt.Errorf("arguments: 'find' needed for recursive queries")
		}
		if v.Val != "parents" && v.Val != "children" {
			return fmt.Errorf("find: valid values are 'parents' and 'children'")
		}
	}
	return nil
}

func (co *Compiler) setMutationType(qc *QCode, op *graph.Operation, role string) error {
	var err error

	setActionVar := func(arg graph.Arg) error {
		v := arg.Val
		if v.Type != graph.NodeVar &&
			v.Type != graph.NodeObj &&
			(v.Type != graph.NodeList || len(v.Children) == 0 && v.Children[0].Type != graph.NodeObj) {
			return argErr(arg.Name, "variable, an object or a list of objects")
		}
		qc.ActionVar = arg.Val.Val
		qc.ActionArg = arg
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
				err = errors.New("value for argument 'delete' must be 'true'")
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

func (co *Compiler) compileDirectiveSkip(sel *Select, d *graph.Directive) error {
	if len(d.Args) == 0 || d.Args[0].Name != "if" {
		return fmt.Errorf("@skip: required argument 'if' missing")
	}
	arg := d.Args[0]

	if ifNotArg(arg, graph.NodeVar) {
		return argErr("if", "variable")
	}

	ex := newExpOp(OpNotEqualsTrue)
	ex.Right.ValType = ValVar
	ex.Right.Val = arg.Val.Val

	setFilter(&sel.Where, ex)
	return nil
}

func (co *Compiler) compileDirectiveCacheControl(qc *QCode, d *graph.Directive) error {
	var maxAge string
	var scope string

	for _, arg := range d.Args {
		switch arg.Name {
		case "maxAge":
			if ifNotArg(arg, graph.NodeNum) {
				return argErr("maxAge", "number")
			}
			maxAge = arg.Val.Val
		case "scope":
			if ifNotArg(arg, graph.NodeStr) {
				return argErr("scope", "string")
			}
			scope = arg.Val.Val
		default:
			return fmt.Errorf("@cacheControl: invalid argument: %s", d.Args[0].Name)
		}
	}

	if len(d.Args) == 0 || maxAge == "" {
		return fmt.Errorf("@cacheControl: required argument 'maxAge' missing")
	}

	hdr := []string{"max-age=" + maxAge}

	if scope != "" {
		hdr = append(hdr, scope)
	}

	qc.Cache.Header = strings.Join(hdr, " ")
	return nil
}

func (co *Compiler) compileDirectiveScript(qc *QCode, d *graph.Directive) error {
	if len(d.Args) == 0 {
		return argErr("name", "string")
	}

	if d.Args[0].Name == "name" {
		if ifNotArg(d.Args[0], graph.NodeStr) {
			return argErr("name", "string")
		}
		qc.Script = d.Args[0].Val.Val
	}

	if qc.Script == "" {
		qc.Script = qc.Name
	}

	if qc.Script == "" {
		return fmt.Errorf("@script: required argument 'name' missing")
	}

	if path.Ext(qc.Script) == "" {
		qc.Script += ".js"
	}

	return nil
}

type validator struct {
	name   string
	types  []graph.ParserType
	single bool
}

var validators = map[string]validator{
	"variable":                 {name: "variable", types: []graph.ParserType{graph.NodeStr}},
	"error":                    {name: "error", types: []graph.ParserType{graph.NodeStr}},
	"unique":                   {name: "unique", types: []graph.ParserType{graph.NodeBool}, single: true},
	"format":                   {name: "format", types: []graph.ParserType{graph.NodeStr}, single: true},
	"required":                 {name: "required", types: []graph.ParserType{graph.NodeBool}, single: true},
	"requiredIf":               {name: "required_if", types: []graph.ParserType{graph.NodeObj}},
	"requiredUnless":           {name: "required_unless", types: []graph.ParserType{graph.NodeObj}},
	"requiredWith":             {name: "required_with", types: []graph.ParserType{graph.NodeList, graph.NodeStr}},
	"requiredWithAll":          {name: "required_with_all", types: []graph.ParserType{graph.NodeList, graph.NodeStr}},
	"requiredWithout":          {name: "required_without", types: []graph.ParserType{graph.NodeList, graph.NodeStr}},
	"requiredWithoutAll":       {name: "required_without_all", types: []graph.ParserType{graph.NodeList, graph.NodeStr}},
	"length":                   {name: "len", types: []graph.ParserType{graph.NodeStr, graph.NodeNum}},
	"max":                      {name: "max", types: []graph.ParserType{graph.NodeStr, graph.NodeNum}},
	"min":                      {name: "min", types: []graph.ParserType{graph.NodeStr, graph.NodeNum}},
	"equals":                   {name: "eq", types: []graph.ParserType{graph.NodeStr, graph.NodeNum}},
	"notEquals":                {name: "neq", types: []graph.ParserType{graph.NodeStr, graph.NodeNum}},
	"oneOf":                    {name: "oneof", types: []graph.ParserType{graph.NodeList, graph.NodeNum, graph.NodeList, graph.NodeStr}},
	"greaterThan":              {name: "gt", types: []graph.ParserType{graph.NodeStr, graph.NodeNum}},
	"greaterThanOrEquals":      {name: "gte", types: []graph.ParserType{graph.NodeStr, graph.NodeNum}},
	"lessThan":                 {name: "lt", types: []graph.ParserType{graph.NodeStr, graph.NodeNum}},
	"lessThanOrEquals":         {name: "lte", types: []graph.ParserType{graph.NodeStr, graph.NodeNum}},
	"equalsField":              {name: "eqfield", types: []graph.ParserType{graph.NodeStr}},
	"notEqualsField":           {name: "nefield", types: []graph.ParserType{graph.NodeStr}},
	"greaterThanField":         {name: "gtfield", types: []graph.ParserType{graph.NodeStr}},
	"greaterThanOrEqualsField": {name: "gtefield", types: []graph.ParserType{graph.NodeStr}},
	"lessThanField":            {name: "ltfield", types: []graph.ParserType{graph.NodeStr}},
	"lessThanOrEqualsField":    {name: "ltefield", types: []graph.ParserType{graph.NodeStr}},
}

func (co *Compiler) compileDirectiveConstraint(qc *QCode, d *graph.Directive) error {
	var varName string
	var errMsg string
	var vals []string

	for _, a := range d.Args {
		if a.Name == "variable" && ifNotArgVal(a, "") {
			if a.Val.Val[0] == '$' {
				varName = a.Val.Val[1:]
			} else {
				varName = a.Val.Val
			}
			continue
		}

		if a.Name == "error" && ifNotArgVal(a, "") {
			errMsg = a.Val.Val
		}

		if a.Name == "format" && ifNotArgVal(a, "") {
			vals = append(vals, a.Val.Val)
			continue
		}

		v, ok := validators[a.Name]
		if !ok {
			continue
		}

		if err := validateConstraint(a, v); err != nil {
			return err
		}

		if v.single {
			vals = append(vals, v.name)
			continue
		}

		var value string
		switch a.Val.Type {
		case graph.NodeStr, graph.NodeNum, graph.NodeBool:
			if ifNotArgVal(a, "") {
				value = a.Val.Val
			}

		case graph.NodeObj:
			var items []string
			for _, v := range a.Val.Children {
				items = append(items, v.Name, v.Val)
			}
			value = strings.Join(items, " ")

		case graph.NodeList:
			var items []string
			for _, v := range a.Val.Children {
				items = append(items, v.Val)
			}
			value = strings.Join(items, " ")
		}

		vals = append(vals, (v.name + "=" + value))
	}

	if varName == "" {
		return errors.New("invalid @constraint no variable name specified")
	}

	if qc.Consts == nil {
		qc.Consts = make(map[string]interface{})
	}

	opt := strings.Join(vals, ",")
	if errMsg != "" {
		opt += "~" + errMsg
	}

	qc.Consts[varName] = opt
	return nil
}

func validateConstraint(a graph.Arg, v validator) error {
	list := false
	for _, t := range v.types {
		switch {
		case t == graph.NodeList:
			list = true
		case list && ifArgList(a, t):
			return nil
		case ifArg(a, t):
			return nil
		}
	}

	list = false
	err := "value must be of type: "

	for i, t := range v.types {
		if i != 0 {
			err += ", "
		}
		if !list && t == graph.NodeList {
			err += "a list of "
			list = true
		}
		err += t.String()
	}
	return errors.New(err)
}

func (co *Compiler) compileDirectiveInclude(sel *Select, d *graph.Directive) error {
	if len(d.Args) == 0 || d.Args[0].Name != "if" {
		return fmt.Errorf("@include: required argument 'if' missing")
	}
	arg := d.Args[0]

	if arg.Val.Type != graph.NodeVar {
		return argErr("if", "variable")
	}

	ex := newExpOp(OpEqualsTrue)
	ex.Right.ValType = ValVar
	ex.Right.Val = arg.Val.Val

	setFilter(&sel.Where, ex)
	return nil
}

func (co *Compiler) compileDirectiveNotRelated(sel *Select, d *graph.Directive) error {
	sel.Rel.Type = sdata.RelSkip
	return nil
}

func (co *Compiler) compileDirectiveThrough(sel *Select, d *graph.Directive) error {
	if len(d.Args) == 0 {
		return fmt.Errorf("@through: required argument 'table' or 'column'")
	}
	arg := d.Args[0]

	if arg.Name == "table" || arg.Name == "column" {
		if arg.Val.Type != graph.NodeStr {
			return argErr(arg.Name, "string")
		}
		sel.through = arg.Val.Val
	}

	return nil
}
func (co *Compiler) compileDirectiveValidation(qc *QCode, d *graph.Directive) error {
	if len(d.Args) == 0 {
		return fmt.Errorf("@validation: required cue schema")
	}
	arg := d.Args[0]

	if arg.Name == "cue" {
		qc.Validation = &Validation{Cue: *arg.Val}
	}

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
	node := arg.Val

	if sel.ParentID != -1 {
		return fmt.Errorf("argument 'id' can only be specified at the query root")
	}

	if node.Type != graph.NodeNum &&
		node.Type != graph.NodeStr &&
		node.Type != graph.NodeVar {
		return argErr("id", "number, string or variable")
	}

	if sel.Ti.PrimaryCol.Name == "" {
		return fmt.Errorf("no primary key column defined for %s", sel.Table)
	}

	ex := newExpOp(OpEquals)
	ex.Left.Col = sel.Ti.PrimaryCol

	switch node.Type {
	case graph.NodeNum:
		if _, err := strconv.ParseInt(node.Val, 10, 32); err != nil {
			return err
		} else {
			ex.Right.ValType = ValNum
			ex.Right.Val = node.Val
		}

	case graph.NodeStr:
		ex.Right.ValType = ValStr
		ex.Right.Val = node.Val

	case graph.NodeVar:
		ex.Right.ValType = ValVar
		ex.Right.Val = node.Val
	}

	sel.Where.Exp = ex
	sel.Singular = true
	return nil
}

func (co *Compiler) compileArgSearch(sel *Select, arg *graph.Arg) error {
	if len(sel.Ti.FullText) == 0 {
		switch co.s.DBType() {
		case "mysql":
			return fmt.Errorf("no fulltext indexes defined for table '%s'", sel.Table)
		default:
			return fmt.Errorf("no tsvector column defined on table '%s'", sel.Table)
		}
	}

	if arg.Val.Type != graph.NodeVar {
		return argErr("search", "variable")
	}

	ex := newExpOp(OpTsQuery)
	ex.Right.ValType = ValVar
	ex.Right.Val = arg.Val.Val

	sel.addArg(arg)
	setFilter(&sel.Where, ex)
	return nil
}

func (co *Compiler) compileArgWhere(ti sdata.DBTable, sel *Select, arg *graph.Arg, role string) error {
	st := util.NewStackInf()
	ex, nu, err := co.compileArgObj(sel.Table, ti, st, arg)
	if err != nil {
		return err
	}

	if nu && role == "anon" {
		sel.SkipRender = SkipTypeUserNeeded
	}
	setFilter(&sel.Where, ex)
	return nil
}

func (co *Compiler) compileArgOrderBy(qc *QCode, sel *Select, arg *graph.Arg) error {
	node := arg.Val

	if node.Type != graph.NodeObj &&
		node.Type != graph.NodeVar {
		return argErr("order_by", "object or variable")
	}

	var cm map[string]struct{}

	if len(sel.OrderBy) != 0 {
		cm = make(map[string]struct{})
		for _, ob := range sel.OrderBy {
			cm[ob.Col.Name] = struct{}{}
		}
	}

	switch node.Type {
	case graph.NodeObj:
		return co.compileArgOrderByObj(sel, node, cm)

	case graph.NodeVar:
		return co.compileArgOrderByVar(qc, sel, node, cm)
	}

	return nil
}

func (co *Compiler) compileArgOrderByObj(sel *Select, node *graph.Node, cm map[string]struct{}) error {
	var err error
	st := util.NewStackInf()

	for i := range node.Children {
		st.Push(node.Children[i])
	}

	obList := make([]OrderBy, 0, 2)

	for {
		if st.Len() == 0 {
			break
		}

		intf := st.Pop()
		node, ok := intf.(*graph.Node)
		if !ok {
			return fmt.Errorf("17: unexpected value %v (%t)", intf, intf)
		}

		// Check for type
		if node.Type != graph.NodeStr && node.Type != graph.NodeObj {
			return fmt.Errorf("expecting a string or object")
		}

		var ob OrderBy

		switch node.Type {
		case graph.NodeStr:
			if ob.Order, err = toOrder(node.Val); err != nil { // sets the asc desc etc
				return err
			}
			if err := setOrderByColName(sel.Ti, &ob, node); err != nil {
				return err
			}
		case graph.NodeObj:
			var path []sdata.TPath
			if path, err = co.s.FindPath(node.Name, sel.Ti.Name, ""); err != nil {
				return err
			}
			ti := path[0].LT

			cn := node.Children[0]
			if ob.Order, err = toOrder(cn.Val); err != nil { // sets the asc desc etc
				return err
			}

			for i := len(path) - 1; i >= 0; i-- {
				p := path[i]
				rel := sdata.PathToRel(p)
				sel.Joins = append(sel.Joins, Join{
					Rel:    rel,
					Filter: buildFilter(rel, -1),
					Local:  true,
				})
			}

			if err := setOrderByColName(ti, &ob, cn); err != nil {
				return err
			}
		}

		if _, ok := cm[ob.Col.Name]; ok {
			return fmt.Errorf("duplicate column in order by: %s", ob.Col.Name)
		}
		obList = append(obList, ob)
	}

	for i := len(obList) - 1; i >= 0; i-- {
		sel.OrderBy = append(sel.OrderBy, obList[i])
	}

	return err
}

func (co *Compiler) compileArgOrderByVar(qc *QCode, sel *Select, node *graph.Node, cm map[string]struct{}) error {
	obList := make([]OrderBy, 0, 2)
	k := string(qc.Vars[node.Val])
	if k[0] != '"' {
		return fmt.Errorf("Order by variable must be a string: %s", k)
	}
	k = k[1:(len(k) - 1)]

	values, ok := sel.tc.OrderBy[k]
	if !ok {
		return fmt.Errorf("Order by not found: %s", k)
	}

	var mval []string
	for k := range sel.tc.OrderBy {
		mval = append(mval, k)
	}
	qc.Metadata.Order.Var = node.Val
	qc.Metadata.Order.Values = mval

	for _, v := range values {
		ob := OrderBy{}
		ob.Order, _ = toOrder(v[1])

		col, err := sel.Ti.GetColumn(v[0])
		if err != nil {
			return err
		}
		ob.Col = col
		if _, ok := cm[ob.Col.Name]; ok {
			return fmt.Errorf("duplicate column in order by: %s", ob.Col.Name)
		}
		obList = append(obList, ob)
	}
	sel.OrderBy = obList
	return nil
}

func toOrder(val string) (Order, error) {
	switch val {
	case "asc":
		return OrderAsc, nil
	case "desc":
		return OrderDesc, nil
	case "asc_nulls_first":
		return OrderAscNullsFirst, nil
	case "desc_nulls_first":
		return OrderDescNullsFirst, nil
	case "asc_nulls_last":
		return OrderAscNullsLast, nil
	case "desc_nulls_last":
		return OrderDescNullsLast, nil
	default:
		return OrderAsc, fmt.Errorf("valid values include asc, desc, asc_nulls_first and desc_nulls_first")
	}
}

func (co *Compiler) compileArgDistinctOn(sel *Select, arg *graph.Arg) error {
	node := arg.Val

	if node.Type != graph.NodeList && node.Type != graph.NodeStr {
		return fmt.Errorf("expecting a list of strings or just a string")
	}

	if node.Type == graph.NodeStr {
		if col, err := sel.Ti.GetColumn(node.Val); err == nil {
			switch co.s.DBType() {
			case "mysql":
				sel.OrderBy = append(sel.OrderBy, OrderBy{Order: OrderAsc, Col: col})
			default:
				sel.DistinctOn = append(sel.DistinctOn, col)
			}
		} else {
			return err
		}
	}

	for _, cn := range node.Children {
		if col, err := sel.Ti.GetColumn(cn.Val); err == nil {
			switch co.s.DBType() {
			case "mysql":
				sel.OrderBy = append(sel.OrderBy, OrderBy{Order: OrderAsc, Col: col})
			default:
				sel.DistinctOn = append(sel.DistinctOn, col)
			}
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
		if co.s.DBType() == "mysql" {
			return dbArgErr("limit", "number", "mysql")
		}
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
		if co.s.DBType() == "mysql" {
			return dbArgErr("limit", "number", "mysql")
		}
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

func setFilter(where *Filter, fil *Exp) {
	if where.Exp != nil {
		// save exiting exp pointer (could be a common one from filter config)
		ow := where.Exp

		// add a new `and` exp and hook the above saved exp pointer a child
		// we don't want to modify an exp object thats common (from filter config)
		where.Exp = newExpOp(OpAnd)
		where.Children = where.childrenA[:2]
		where.Children[0] = fil
		where.Children[1] = ow

	} else {
		where.Exp = fil
	}
}

func setOrderByColName(ti sdata.DBTable, ob *OrderBy, node *graph.Node) error {
	col, err := ti.GetColumn(node.Name)
	if err != nil {
		return err
	}
	ob.Col = col
	return nil
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

		f, nu, err := co.compileArgNode("", ti, st, node, isJSON)
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

// func buildPath(a []string) string {
// 	switch len(a) {
// 	case 0:
// 		return ""
// 	case 1:
// 		return a[0]
// 	}

// 	n := len(a) - 1
// 	for i := 0; i < len(a); i++ {
// 		n += len(a[i])
// 	}

// 	var b strings.Builder
// 	b.Grow(n)
// 	b.WriteString(a[0])
// 	for _, s := range a[1:] {
// 		b.WriteRune('.')
// 		b.WriteString(s)
// 	}
// 	return b.String()
// }

func ifArgList(arg graph.Arg, lty graph.ParserType) bool {
	return arg.Val.Type == graph.NodeList &&
		len(arg.Val.Children) != 0 &&
		arg.Val.Children[0].Type == lty
}

func ifArg(arg graph.Arg, ty graph.ParserType) bool {
	return arg.Val.Type == ty
}

func ifNotArg(arg graph.Arg, ty graph.ParserType) bool {
	return arg.Val.Type != ty
}

// func ifArgVal(arg graph.Arg, val string) bool {
// 	return arg.Val.Val == val
// }

func ifNotArgVal(arg graph.Arg, val string) bool {
	return arg.Val.Val != val
}

func argErr(name, ty string) error {
	return fmt.Errorf("value for argument '%s' must be a %s", name, ty)
}

func dbArgErr(name, ty, db string) error {
	return fmt.Errorf("%s: value for argument '%s' must be a %s", db, name, ty)
}

func (sel *Select) addArg(arg *graph.Arg) {
	if sel.Args == nil {
		sel.Args = make(map[string]Arg)
	}
	sel.Args[arg.Name] = Arg{Val: arg.Val.Val}
}
