package dialect

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

// ClickHouseDialect compiles a qcode tree into the JSON DSL the clickhousedriver
// executes as SQL. Like Cassandra/MongoDB it bypasses SQL generation via the
// FullQueryCompiler/FullMutationCompiler seam; it embeds MongoDBDialect to inherit
// the no-op SQL Render* methods. Unlike Cassandra, ClickHouse is a scan engine, so
// there is no servability gate — OR/NOT/ranges/LIKE are all served.
type ClickHouseDialect struct {
	MongoDBDialect
}

func (d *ClickHouseDialect) Name() string { return "clickhouse" }

func (d *ClickHouseDialect) SupportsSubscriptionBatching() bool { return false }

// ---- DSL structs (mirror clickhousedriver's JSON; core must not import the driver) ----

type chDSL struct {
	Operation string      `json:"operation"`
	Root      *chNode     `json:"root,omitempty"`
	Mutation  *chMutation `json:"mutation,omitempty"`
}

type chNode struct {
	Database    string        `json:"database,omitempty"`
	Table       string        `json:"table"`
	Columns     []string      `json:"columns"`
	Filters     []chFilter    `json:"filters,omitempty"`
	OrderBy     []chOrder     `json:"order_by,omitempty"`
	Limit       int           `json:"limit,omitempty"`
	Offset      int           `json:"offset,omitempty"`
	OffsetParam string        `json:"offset_param,omitempty"`
	GroupBy     []string      `json:"group_by,omitempty"`
	Aggregates  []chAggregate `json:"aggregates,omitempty"`
	Windows     []chWindow    `json:"windows,omitempty"`
	Keyset      *chKeyset     `json:"keyset,omitempty"`
	Rel         *chRel        `json:"rel,omitempty"`
	FieldName   string        `json:"field_name,omitempty"`
	Singular    bool          `json:"singular,omitempty"`
	Typename    string        `json:"typename,omitempty"`
	Children    []*chNode     `json:"children,omitempty"`
}

type chFilter struct {
	Col   string     `json:"col,omitempty"`
	Op    string     `json:"op,omitempty"`
	Param string     `json:"param,omitempty"`
	Value any        `json:"value,omitempty"`
	And   []chFilter `json:"and,omitempty"`
	Or    []chFilter `json:"or,omitempty"`
	Not   []chFilter `json:"not,omitempty"`
}

type chOrder struct {
	Col      string `json:"col"`
	Order    string `json:"order"`
	Nullable bool   `json:"nullable,omitempty"`
}

type chAggregate struct {
	Fn    string `json:"fn"`
	Col   string `json:"col,omitempty"`
	Expr  string `json:"expr,omitempty"` // pre-rendered SQL scalar expression
	Alias string `json:"alias"`
}

// chWindow is an analytic window field (@rank/@running/@previous/…) rendered in
// the SELECT list as fn(arg) OVER (PARTITION BY … ORDER BY … frame).
type chWindow struct {
	Fn        string    `json:"fn"`
	Arg       string    `json:"arg,omitempty"`
	Partition []string  `json:"partition,omitempty"`
	OrderBy   []chOrder `json:"order_by,omitempty"`
	Frame     string    `json:"frame,omitempty"`
	Alias     string    `json:"alias"`
}

type chKeyset struct {
	SelID       int       `json:"sel_id"`
	Prefix      string    `json:"prefix"`
	CursorParam string    `json:"cursor_param,omitempty"`
	Columns     []chOrder `json:"columns"`
	Backward    bool      `json:"backward,omitempty"`
}

type chRel struct {
	ParentCol string `json:"parent_col"`
	ChildCol  string `json:"child_col"`
	Kind      string `json:"kind,omitempty"`
}

type chMutation struct {
	Type        string            `json:"type"`
	Database    string            `json:"database,omitempty"`
	Table       string            `json:"table"`
	Set         []chAssign        `json:"set,omitempty"`
	Where       []chFilter        `json:"where,omitempty"`
	RawInput    string            `json:"raw_input,omitempty"`
	Lightweight bool              `json:"lightweight,omitempty"`
	PrimaryKey  string            `json:"primary_key,omitempty"`
	ColumnTypes map[string]string `json:"column_types,omitempty"`
	Returning   *chNode           `json:"returning,omitempty"`
}

type chAssign struct {
	Col   string `json:"col"`
	Param string `json:"param,omitempty"`
	Value any    `json:"value,omitempty"`
}

const (
	chRelOneToOne  = "one_to_one"
	chRelOneToMany = "one_to_many"
)

// chBuilder accumulates parameter specs while building the DSL tree. Params are
// embedded as sentinel tokens and resolved to `$N` placeholders at emit time.
type chBuilder struct {
	specs  []Param
	prefix string // security prefix for cursor tokens
}

const (
	chParamOpen  = "~~gjp:"
	chParamClose = "~~"
)

func (b *chBuilder) param(p Param) string {
	tok := chParamOpen + strconv.Itoa(len(b.specs)) + chParamClose
	b.specs = append(b.specs, p)
	return tok
}

func (b *chBuilder) emit(ctx Context, v any) {
	raw, err := json.Marshal(v)
	if err != nil {
		ctx.SetError(fmt.Errorf("clickhouse: encoding DSL: %w", err))
		return
	}
	s := string(raw)
	for {
		i := strings.Index(s, chParamOpen)
		if i < 0 {
			break
		}
		rest := s[i+len(chParamOpen):]
		k := strings.Index(rest, chParamClose)
		if k < 0 {
			break
		}
		n, err := strconv.Atoi(rest[:k])
		if err != nil {
			break
		}
		ctx.WriteString(s[:i])
		if n >= 0 && n < len(b.specs) {
			ctx.AddParam(b.specs[n]) // registers the param and writes its $N here
		}
		s = rest[k+len(chParamClose):]
	}
	ctx.WriteString(s)
}

// ---- Query compilation ----

func (d *ClickHouseDialect) CompileFullQuery(ctx Context, qc *qcode.QCode) bool {
	if len(qc.Roots) == 0 {
		ctx.SetError(fmt.Errorf("clickhouse: empty query"))
		return true
	}
	if len(qc.Roots) > 1 {
		ctx.SetError(fmt.Errorf("clickhouse: only a single root selection is supported per query"))
		return true
	}
	b := &chBuilder{prefix: ctx.GetSecPrefix()}
	node, err := b.buildNode(qc, &qc.Selects[qc.Roots[0]])
	if err != nil {
		ctx.SetError(err)
		return true
	}
	b.emit(ctx, chDSL{Operation: "query", Root: node})
	return true
}

func (b *chBuilder) buildNode(qc *qcode.QCode, sel *qcode.Select) (*chNode, error) {
	n := &chNode{
		Database:  sel.Schema,
		Table:     sel.Table,
		FieldName: sel.FieldName,
		Singular:  sel.Singular,
	}
	if sel.Typename {
		n.Typename = sel.Table
	}

	if err := b.collectFields(sel, n); err != nil {
		return nil, err
	}

	if sel.Where.Exp != nil {
		f, ok, err := b.buildFilter(sel.Ti, sel.Where.Exp)
		if err != nil {
			return nil, err
		}
		if ok {
			n.Filters = []chFilter{f}
		}
	}

	for _, ob := range sel.OrderBy {
		n.OrderBy = append(n.OrderBy, chOrder{Col: ob.Col.Name, Order: orderString(ob.Order)})
	}

	b.applyPaging(sel, n)

	for _, cid := range sel.Children {
		child := &qc.Selects[cid]
		if child.SkipRender != qcode.SkipTypeNone {
			continue
		}
		cn, err := b.buildNode(qc, child)
		if err != nil {
			return nil, err
		}
		rel, err := chRelColumns(sel, child)
		if err != nil {
			return nil, err
		}
		cn.Rel = rel
		// An aggregate child must group by its join column so each parent gets its
		// own aggregate row (the N+1 fetch spans many parents at once).
		if len(cn.Aggregates) > 0 {
			cn.GroupBy = ensureCol(cn.GroupBy, rel.ChildCol)
			cn.Columns = ensureCol(cn.Columns, rel.ChildCol)
			cn.Limit = 0    // per-parent aggregate: one row per parent, no row limit
			cn.Keyset = nil // no cursor on an aggregate child
		}
		n.Columns = ensureCol(n.Columns, rel.ParentCol) // parent must select its join column
		n.Children = append(n.Children, cn)
	}

	return n, nil
}

// collectFields splits the selection into scalar columns, aggregate functions
// (count/sum/avg/min/max), and group-by columns (distinct). Order-by and group-by
// columns are also added to the column set so cursors and child joins work.
func (b *chBuilder) collectFields(sel *qcode.Select, n *chNode) error {
	seen := map[string]bool{}
	add := func(name string) {
		if name != "" && !seen[name] {
			seen[name] = true
			n.Columns = append(n.Columns, name)
		}
	}
	for _, f := range sel.Fields {
		switch f.Type {
		case qcode.FieldTypeCol:
			add(f.Col.Name)
		case qcode.FieldTypeFunc:
			// Analytic window fields (@rank/@running/@previous/…) are per-row.
			if f.Window != nil {
				w, err := buildWindow(f)
				if err != nil {
					return err
				}
				n.Windows = append(n.Windows, w)
				continue
			}
			if !f.Func.Agg {
				return fmt.Errorf("clickhouse: non-aggregate function field %q is not yet supported", f.FieldName)
			}
			// Expression aggregates (sum(price*qty)) carry a scalar expr tree.
			if len(f.Args) > 0 && f.Args[0].Type == qcode.ArgTypeExpr {
				if f.Func.Name == "" {
					return fmt.Errorf("clickhouse: bare ratio-of-aggregate expressions are not yet supported (field %q)", f.FieldName)
				}
				exprSQL, err := renderScalarExpr(f.Args[0].Expr)
				if err != nil {
					return err
				}
				n.Aggregates = append(n.Aggregates, chAggregate{Fn: f.Func.Name, Expr: exprSQL, Alias: f.FieldName})
				continue
			}
			// The aggregated column rides in Args (ArgTypeCol); count(*) has an empty column.
			col := "*"
			if len(f.Args) > 0 && f.Args[0].Col.Name != "" {
				col = f.Args[0].Col.Name
			}
			n.Aggregates = append(n.Aggregates, chAggregate{Fn: f.Func.Name, Col: col, Alias: f.FieldName})
		}
	}
	for _, dc := range sel.DistinctOn {
		add(dc.Name)
		n.GroupBy = append(n.GroupBy, dc.Name)
	}
	for _, ob := range sel.OrderBy {
		add(ob.Col.Name)
	}
	if len(n.Columns) == 0 && len(n.Aggregates) == 0 {
		return fmt.Errorf("clickhouse: selection %q has no columns", sel.FieldName)
	}
	return nil
}

// buildFilter translates a qcode Exp tree to a chFilter. ClickHouse serves the
// full operator set, so nothing is rejected on servability grounds.
func (b *chBuilder) buildFilter(ti sdata.DBTable, ex *qcode.Exp) (chFilter, bool, error) {
	switch ex.Op {
	case qcode.OpAnd:
		parts, err := b.buildChildren(ti, ex.Children)
		if err != nil {
			return chFilter{}, false, err
		}
		switch len(parts) {
		case 0:
			return chFilter{}, false, nil
		case 1:
			return parts[0], true, nil
		default:
			return chFilter{And: parts}, true, nil
		}
	case qcode.OpOr:
		parts, err := b.buildChildren(ti, ex.Children)
		if err != nil {
			return chFilter{}, false, err
		}
		switch len(parts) {
		case 0:
			return chFilter{}, false, nil
		case 1:
			return parts[0], true, nil
		default:
			return chFilter{Or: parts}, true, nil
		}
	case qcode.OpNot:
		parts, err := b.buildChildren(ti, ex.Children)
		if err != nil {
			return chFilter{}, false, err
		}
		switch len(parts) {
		case 0:
			return chFilter{}, false, nil
		case 1:
			return chFilter{Not: parts}, true, nil
		default:
			return chFilter{Not: []chFilter{{And: parts}}}, true, nil
		}
	case qcode.OpEqualsTrue, qcode.OpNotEqualsTrue, qcode.OpFalse:
		return chFilter{}, false, nil // @skip/@include variable conditions, not predicates
	default:
		return b.buildLeaf(ti, ex)
	}
}

func (b *chBuilder) buildChildren(ti sdata.DBTable, children []*qcode.Exp) ([]chFilter, error) {
	var parts []chFilter
	for _, c := range children {
		// The whole cursor-seek sub-tree (which mixes __cur comparisons with bare
		// `col IS NULL` branches) is dropped as a unit — the driver rebuilds the
		// seek from the keyset.
		if containsCur(c) {
			continue
		}
		f, ok, err := b.buildFilter(ti, c)
		if err != nil {
			return nil, err
		}
		if ok {
			parts = append(parts, f)
		}
	}
	return parts, nil
}

// containsCur reports whether any node in the expression references the synthetic
// __cur table that qcode injects for cursor seek predicates.
func containsCur(ex *qcode.Exp) bool {
	if ex == nil {
		return false
	}
	if ex.Left.Table == "__cur" || ex.Right.Table == "__cur" || ex.Right.Col.Table == "__cur" {
		return true
	}
	for _, c := range ex.Children {
		if containsCur(c) {
			return true
		}
	}
	return false
}

func (b *chBuilder) buildLeaf(ti sdata.DBTable, ex *qcode.Exp) (chFilter, bool, error) {
	// Cursor seek predicate: qcode injects comparisons (and IS NULL branches)
	// against a synthetic __cur table. Drop them — the driver rebuilds the seek
	// from the Keyset using the inbound cursor value.
	if ex.Left.Table == "__cur" || ex.Right.Table == "__cur" || ex.Right.Col.Table == "__cur" {
		return chFilter{}, false, nil
	}
	// A column-reference RHS is the parent↔child join correlation GraphJin injects
	// into a nested select; skip it here — the N+1 rel path handles it.
	if isColRef(ex) {
		refTable := ex.Right.Col.Table
		if refTable == "" {
			refTable = ex.Right.Table
		}
		if refTable != "" && refTable != ti.Name {
			return chFilter{}, false, nil
		}
		return chFilter{}, false, fmt.Errorf("clickhouse: column-to-column comparison on %q is not supported", leafCol(ex))
	}

	op, ok := opToCh(ex.Op)
	if !ok {
		return chFilter{}, false, fmt.Errorf("clickhouse: filter %s on column %q is not supported", opName(ex.Op), leafCol(ex))
	}

	f := chFilter{Col: leafCol(ex), Op: op}
	if ex.Op == qcode.OpIsNull || ex.Op == qcode.OpIsNotNull {
		return f, true, nil
	}
	switch ex.Right.ValType {
	case qcode.ValVar:
		f.Param = b.param(Param{
			Name:    ex.Right.Val,
			Type:    ex.Left.Col.Type,
			IsArray: ex.Op == qcode.OpIn || ex.Op == qcode.OpNotIn,
		})
	case qcode.ValList:
		vals := make([]any, len(ex.Right.ListVal))
		for i, v := range ex.Right.ListVal {
			vals[i] = literalValue(ex.Right.ListType, v)
		}
		f.Value = vals
	default:
		f.Value = literalValue(ex.Right.ValType, ex.Right.Val)
	}
	return f, true, nil
}

func (b *chBuilder) applyPaging(sel *qcode.Select, n *chNode) {
	p := sel.Paging
	if p.Limit > 0 {
		n.Limit = int(p.Limit)
	}
	if p.Type == qcode.PTOffset {
		if p.OffsetVar != "" {
			n.OffsetParam = b.param(Param{Name: p.OffsetVar, Type: "integer"})
		} else if p.Offset > 0 {
			n.Offset = int(p.Offset)
		}
	}
	if p.Cursor {
		ks := &chKeyset{SelID: int(sel.ID), Prefix: b.prefix, Backward: p.Type == qcode.PTBackward}
		for _, ob := range sel.OrderBy {
			ks.Columns = append(ks.Columns, chOrder{Col: ob.Col.Name, Order: orderString(ob.Order), Nullable: !ob.Col.NotNull})
		}
		cursorVar := p.CursorVar
		if cursorVar == "" {
			cursorVar = "cursor"
		}
		ks.CursorParam = b.param(Param{Name: cursorVar, Type: "text"})
		n.Keyset = ks
	}
}

// chRelColumns resolves the parent/child join columns and cardinality for a child
// select. ClickHouse has no FKs; the rel was resolved upstream by GraphJin.
func chRelColumns(parent, child *qcode.Select) (*chRel, error) {
	rel := child.Rel
	kind := chRelOneToMany
	if child.Singular {
		kind = chRelOneToOne
	}
	switch rel.Type {
	case sdata.RelOneToOne, sdata.RelOneToMany:
		if rel.Right.Ti.Name == parent.Table {
			return &chRel{ParentCol: rel.Right.Col.Name, ChildCol: rel.Left.Col.Name, Kind: kind}, nil
		}
		return &chRel{ParentCol: rel.Left.Col.Name, ChildCol: rel.Right.Col.Name, Kind: kind}, nil
	default:
		return nil, fmt.Errorf("clickhouse: relationship kind for %q is not supported (only one-to-one / one-to-many)", child.FieldName)
	}
}

// buildWindow maps a qcode analytic field to a ClickHouse window descriptor.
func buildWindow(f qcode.Field) (chWindow, error) {
	w := chWindow{Alias: f.FieldName, Frame: f.Window.Frame, Partition: append([]string{}, f.Window.Partition...)}
	switch {
	case f.WindowFunc != qcode.WindowFuncNone:
		switch f.WindowFunc {
		case qcode.WindowFuncLag:
			w.Fn = "lagInFrame" // ClickHouse has no bare lag()
		case qcode.WindowFuncLead:
			w.Fn = "leadInFrame"
		default:
			w.Fn = f.WindowFunc.String() // row_number, rank, dense_rank, first_value, last_value
		}
		if f.WindowFunc.IsValueFunc() && len(f.Args) > 0 {
			w.Arg = f.Args[0].Col.Name
		}
		// lagInFrame/leadInFrame only see other rows inside the frame.
		if (f.WindowFunc == qcode.WindowFuncLag || f.WindowFunc == qcode.WindowFuncLead) && w.Frame == "" {
			w.Frame = "ROWS BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING"
		}
	case f.Func.Agg:
		w.Fn = f.Func.Name // running/moving aggregate: sum/avg/min/max/count OVER (...)
		if len(f.Args) > 0 && f.Args[0].Col.Name != "" {
			w.Arg = f.Args[0].Col.Name
		}
	default:
		return chWindow{}, fmt.Errorf("clickhouse: unsupported window field %q", f.FieldName)
	}
	for _, ob := range f.Window.OrderBy {
		ord := "asc"
		if ob.Desc {
			ord = "desc"
		}
		w.OrderBy = append(w.OrderBy, chOrder{Col: ob.Col, Order: ord})
	}
	return w, nil
}

func chQuoteIdent(s string) string {
	return "`" + strings.ReplaceAll(s, "`", "``") + "`"
}

// renderScalarExpr renders a qcode scalar expression tree (arithmetic over columns
// and literals) into a ClickHouse SQL string, for use inside an aggregate.
func renderScalarExpr(ex *qcode.Exp) (string, error) {
	if ex == nil {
		return "", fmt.Errorf("clickhouse: nil scalar expression")
	}
	switch ex.Op {
	case qcode.OpColRef:
		return chQuoteIdent(ex.Left.Col.Name), nil
	case qcode.OpLiteral:
		switch ex.Lit.ValType {
		case qcode.ValNum, qcode.ValBool:
			return ex.Lit.Val, nil
		default:
			return "'" + strings.ReplaceAll(ex.Lit.Val, "'", "''") + "'", nil
		}
	case qcode.OpAdd, qcode.OpSub, qcode.OpMul, qcode.OpDiv, qcode.OpMod:
		sym, err := RenderStandardArithOp(ex.Op)
		if err != nil {
			return "", err
		}
		if len(ex.Children) < 2 {
			return "", fmt.Errorf("clickhouse: arithmetic op %s needs at least 2 operands", ex.Op)
		}
		parts := make([]string, len(ex.Children))
		for i, c := range ex.Children {
			s, err := renderScalarExpr(c)
			if err != nil {
				return "", err
			}
			parts[i] = s
		}
		return "(" + strings.Join(parts, " "+sym+" ") + ")", nil
	case qcode.OpNeg:
		if len(ex.Children) != 1 {
			return "", fmt.Errorf("clickhouse: neg needs exactly 1 operand")
		}
		s, err := renderScalarExpr(ex.Children[0])
		if err != nil {
			return "", err
		}
		return "(-" + s + ")", nil
	case qcode.OpCoalesce:
		parts := make([]string, len(ex.Children))
		for i, c := range ex.Children {
			s, err := renderScalarExpr(c)
			if err != nil {
				return "", err
			}
			parts[i] = s
		}
		return "coalesce(" + strings.Join(parts, ", ") + ")", nil
	default:
		return "", fmt.Errorf("clickhouse: scalar expression op %s is not supported in aggregates yet", ex.Op)
	}
}

func opToCh(op qcode.ExpOp) (string, bool) {
	switch op {
	case qcode.OpEquals:
		return "eq", true
	case qcode.OpNotEquals:
		return "neq", true
	case qcode.OpIn:
		return "in", true
	case qcode.OpNotIn:
		return "nin", true
	case qcode.OpGreaterThan:
		return "gt", true
	case qcode.OpGreaterOrEquals:
		return "gte", true
	case qcode.OpLesserThan:
		return "lt", true
	case qcode.OpLesserOrEquals:
		return "lte", true
	case qcode.OpLike:
		return "like", true
	case qcode.OpILike:
		return "ilike", true
	case qcode.OpIsNull:
		return "isNull", true
	case qcode.OpIsNotNull:
		return "isNotNull", true
	default:
		return "", false
	}
}

// ---- Mutation compilation (insert + best-effort async update/delete) ----

func (d *ClickHouseDialect) CompileFullMutation(ctx Context, qc *qcode.QCode) bool {
	if len(qc.Mutates) == 0 {
		ctx.SetError(fmt.Errorf("clickhouse: empty mutation"))
		return true
	}
	if len(qc.Mutates) > 1 {
		ctx.SetError(fmt.Errorf("clickhouse: nested/related writes are not supported (single-table only)"))
		return true
	}
	b := &chBuilder{}
	mut, err := b.buildMutation(qc, &qc.Mutates[0])
	if err != nil {
		ctx.SetError(err)
		return true
	}
	b.emit(ctx, chDSL{Operation: mut.Type, Mutation: mut})
	return true
}

func (b *chBuilder) buildMutation(qc *qcode.QCode, m *qcode.Mutate) (*chMutation, error) {
	typ, err := mTypeToCh(m.Type)
	if err != nil {
		return nil, err
	}
	mut := &chMutation{
		Type:        typ,
		Database:    m.Ti.Schema,
		Table:       m.Ti.Name,
		PrimaryKey:  m.Ti.PrimaryCol.Name,
		ColumnTypes: columnTypes(m.Ti),
	}

	jsonInput := m.IsJSON && qc.ActionVar != ""

	switch typ {
	case "insert":
		if jsonInput {
			mut.RawInput = b.param(Param{Name: qc.ActionVar, Type: "json"})
		} else {
			for _, c := range m.Cols {
				if c.Set {
					mut.Set = append(mut.Set, b.chAssign(c))
				}
			}
		}
	case "update":
		if jsonInput {
			mut.RawInput = b.param(Param{Name: qc.ActionVar, Type: "json"})
		} else {
			for _, c := range m.Cols {
				if c.Set {
					mut.Set = append(mut.Set, b.chAssign(c))
				}
			}
		}
		if err := b.buildMutWhere(qc, m, mut); err != nil {
			return nil, err
		}
		if len(mut.Where) == 0 {
			return nil, fmt.Errorf("clickhouse: update requires a where filter (refusing to rewrite the whole table)")
		}
	case "delete":
		if err := b.buildMutWhere(qc, m, mut); err != nil {
			return nil, err
		}
		if len(mut.Where) == 0 {
			return nil, fmt.Errorf("clickhouse: delete requires a where filter (refusing to clear the whole table)")
		}
		mut.Lightweight = true
	}

	// The DSL mutation path binds row data from a JSON variable or presets only;
	// inline per-column variables arrive with Set=false (data lives on m.Data).
	if (typ == "insert" || typ == "update") && mut.RawInput == "" && len(mut.Set) == 0 {
		return nil, fmt.Errorf("clickhouse: %s requires column values — pass the row as a single JSON variable (e.g. insert: $data); inline per-column variables are not supported", typ)
	}

	mut.Returning = b.buildMutReturning(qc, m)
	if (typ == "update" || typ == "delete") && len(mut.Where) > 0 {
		mut.Returning.Filters = mut.Where
	}
	return mut, nil
}

func (b *chBuilder) chAssign(c qcode.MColumn) chAssign {
	name := strings.TrimPrefix(c.Value, "$")
	return chAssign{Col: c.Col.Name, Param: b.param(Param{Name: name, Type: c.Col.Type})}
}

func (b *chBuilder) buildMutWhere(qc *qcode.QCode, m *qcode.Mutate, mut *chMutation) error {
	ex := mutateWhereExp(qc, m)
	if ex == nil {
		return nil
	}
	f, ok, err := b.buildFilter(m.Ti, ex)
	if err != nil {
		return err
	}
	if ok {
		mut.Where = flattenChWhere(f)
	}
	return nil
}

// buildMutReturning builds the read-after-write SELECT from the mutation's
// associated select, so GraphJin gets the mutated object back.
func (b *chBuilder) buildMutReturning(qc *qcode.QCode, m *qcode.Mutate) *chNode {
	sel := selectForMutate(qc, m)
	rn := &chNode{Database: m.Ti.Schema, Table: m.Ti.Name, Singular: true}
	if sel != nil {
		rn.FieldName = sel.FieldName
		rn.Singular = sel.Singular
		for _, f := range sel.Fields {
			if f.Type == qcode.FieldTypeCol {
				rn.Columns = ensureCol(rn.Columns, f.Col.Name)
			}
		}
	}
	if m.Ti.PrimaryCol.Name != "" {
		rn.Columns = ensureCol(rn.Columns, m.Ti.PrimaryCol.Name)
	}
	return rn
}

func flattenChWhere(f chFilter) []chFilter {
	if len(f.And) > 0 {
		out := make([]chFilter, 0, len(f.And))
		for _, c := range f.And {
			out = append(out, flattenChWhere(c)...)
		}
		return out
	}
	return []chFilter{f}
}

func mTypeToCh(t qcode.MType) (string, error) {
	switch t {
	case qcode.MTInsert:
		return "insert", nil
	case qcode.MTUpdate:
		return "update", nil
	case qcode.MTDelete:
		return "delete", nil
	case qcode.MTUpsert:
		return "", fmt.Errorf("clickhouse: upsert is not supported (no atomic upsert; ReplacingMergeTree dedupes only at merge time)")
	case qcode.MTConnect, qcode.MTDisconnect:
		return "", fmt.Errorf("clickhouse: connect/disconnect writes are not supported (no foreign keys)")
	default:
		return "", fmt.Errorf("clickhouse: unsupported mutation type")
	}
}
