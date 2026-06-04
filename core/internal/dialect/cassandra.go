package dialect

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

// CassandraDialect compiles a qcode tree into the JSON DSL the cassandradriver
// package executes as CQL. Like MongoDB it bypasses SQL generation entirely via
// the FullQueryCompiler/FullMutationCompiler seam; it embeds MongoDBDialect only
// to inherit the no-op SQL Render* methods, which are dead code once the full
// compilers return true.
type CassandraDialect struct {
	MongoDBDialect
}

func (d *CassandraDialect) Name() string { return "cassandra" }

// SupportsSubscriptionBatching is false: each subscription member is polled
// individually; there is no CQL IN-batched subscription wrapper.
func (d *CassandraDialect) SupportsSubscriptionBatching() bool { return false }

// ---- DSL structs (mirror cassandradriver's JSON; core must not import the driver) ----

type casDSL struct {
	Operation string       `json:"operation"`
	Root      *casNode     `json:"root,omitempty"`
	Mutation  *casMutation `json:"mutation,omitempty"`
}

type casNode struct {
	Keyspace        string            `json:"keyspace,omitempty"`
	Table           string            `json:"table"`
	Columns         []string          `json:"columns"`
	Filters         []casFilter       `json:"filters,omitempty"`
	OrderBy         []casOrder        `json:"order_by,omitempty"`
	Limit           int               `json:"limit,omitempty"`
	PageSize        int               `json:"page_size,omitempty"`
	CursorParam     string            `json:"cursor_param,omitempty"`
	Paging          string            `json:"paging,omitempty"`
	AllowFiltering  bool              `json:"allow_filtering,omitempty"`
	Rel             *casRel           `json:"rel,omitempty"`
	FieldName       string            `json:"field_name,omitempty"`
	Singular        bool              `json:"singular,omitempty"`
	Typename        string            `json:"typename,omitempty"`
	PartitionKeys   []string          `json:"partition_keys,omitempty"`
	ClusteringKeys  []string          `json:"clustering_keys,omitempty"`
	ClusteringOrder map[string]string `json:"clustering_order,omitempty"`
	ColumnTypes     map[string]string `json:"column_types,omitempty"`
	Children        []*casNode        `json:"children,omitempty"`
}

type casFilter struct {
	Col   string      `json:"col,omitempty"`
	Op    string      `json:"op,omitempty"`
	Param string      `json:"param,omitempty"`
	Value any         `json:"value,omitempty"`
	And   []casFilter `json:"and,omitempty"`
}

type casOrder struct {
	Col   string `json:"col"`
	Order string `json:"order"`
}

type casRel struct {
	ParentCol string `json:"parent_col"`
	ChildCol  string `json:"child_col"`
	Kind      string `json:"kind,omitempty"`
}

type casMutation struct {
	Type           string            `json:"type"`
	Keyspace       string            `json:"keyspace,omitempty"`
	Table          string            `json:"table"`
	Set            []casAssign       `json:"set,omitempty"`
	Where          []casFilter       `json:"where,omitempty"`
	PartitionKeys  []string          `json:"partition_keys,omitempty"`
	ClusteringKeys []string          `json:"clustering_keys,omitempty"`
	CounterCols    []string          `json:"counter_cols,omitempty"`
	ColumnTypes    map[string]string `json:"column_types,omitempty"`
	IfNotExists    bool              `json:"if_not_exists,omitempty"`
	IfExists       bool              `json:"if_exists,omitempty"`
	RawInput       string            `json:"raw_input,omitempty"` // param holding the input JSON document
	Returning      *casNode          `json:"returning,omitempty"`
}

type casAssign struct {
	Col     string `json:"col"`
	Param   string `json:"param,omitempty"`
	Value   any    `json:"value,omitempty"`
	Counter bool   `json:"counter,omitempty"`
}

// casBuilder accumulates parameter specs while building the DSL tree. Params are
// embedded as sentinel tokens and resolved to `$N` placeholders at emit time, so
// each AddParam (which both registers and renders the placeholder) fires at the
// exact JSON position the placeholder belongs.
type casBuilder struct {
	specs []Param
}

const (
	paramOpen  = "~~gjp:"
	paramClose = "~~"
)

func (b *casBuilder) param(p Param) string {
	tok := paramOpen + strconv.Itoa(len(b.specs)) + paramClose
	b.specs = append(b.specs, p)
	return tok
}

func (b *casBuilder) emit(ctx Context, v any) {
	raw, err := json.Marshal(v)
	if err != nil {
		ctx.SetError(fmt.Errorf("cassandra: encoding DSL: %w", err))
		return
	}
	s := string(raw)
	for {
		i := strings.Index(s, paramOpen)
		if i < 0 {
			break
		}
		rest := s[i+len(paramOpen):]
		k := strings.Index(rest, paramClose)
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
		s = rest[k+len(paramClose):]
	}
	ctx.WriteString(s)
}

// ---- Query compilation ----

func (d *CassandraDialect) CompileFullQuery(ctx Context, qc *qcode.QCode) bool {
	if len(qc.Roots) == 0 {
		ctx.SetError(fmt.Errorf("cassandra: empty query"))
		return true
	}
	if len(qc.Roots) > 1 {
		ctx.SetError(fmt.Errorf("cassandra: only a single root selection is supported per query"))
		return true
	}
	b := &casBuilder{}
	node, err := b.buildNode(qc, &qc.Selects[qc.Roots[0]])
	if err != nil {
		ctx.SetError(err)
		return true
	}
	b.emit(ctx, casDSL{Operation: "query", Root: node})
	return true
}

func (b *casBuilder) buildNode(qc *qcode.QCode, sel *qcode.Select) (*casNode, error) {
	if sel.GroupCols || sel.GlobalAgg {
		return nil, fmt.Errorf("cassandra: aggregate/group queries are not supported")
	}

	n := &casNode{
		Keyspace:        sel.Schema,
		Table:           sel.Table,
		FieldName:       sel.FieldName,
		Singular:        sel.Singular,
		AllowFiltering:  sel.Ti.AllowFiltering,
		PartitionKeys:   sel.Ti.PartitionKeys,
		ClusteringKeys:  sel.Ti.ClusteringKeys,
		ClusteringOrder: sel.Ti.ClusteringOrder,
		ColumnTypes:     columnTypes(sel.Ti),
	}
	if sel.Typename {
		n.Typename = sel.Table
	}

	cols, err := collectColumns(sel)
	if err != nil {
		return nil, err
	}
	n.Columns = cols

	if sel.Where.Exp != nil {
		f, ok, err := b.buildFilter(sel.Ti, sel.Where.Exp)
		if err != nil {
			return nil, err
		}
		if ok {
			n.Filters = []casFilter{f}
		}
	}

	for _, ob := range sel.OrderBy {
		// Partition-key columns are pinned by the WHERE (=/IN), so ordering by them
		// is a constant — and CQL rejects ORDER BY on the partition key. Drop them;
		// GraphJin's cursor pagination injects the full PK into ORDER BY.
		if isPartitionKey(sel.Ti, ob.Col.Name) {
			continue
		}
		n.OrderBy = append(n.OrderBy, casOrder{Col: ob.Col.Name, Order: orderString(ob.Order)})
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
		rel, err := relColumns(sel, child)
		if err != nil {
			return nil, err
		}
		cn.Rel = rel
		n.Columns = ensureCol(n.Columns, rel.ParentCol) // parent must select its join column
		n.Children = append(n.Children, cn)
	}

	return n, nil
}

// collectColumns is the union of selected scalar fields and key columns (needed
// for cursors and child joins). Aggregate/function fields are rejected.
func collectColumns(sel *qcode.Select) ([]string, error) {
	seen := map[string]bool{}
	var cols []string
	add := func(name string) {
		if name != "" && !seen[name] {
			seen[name] = true
			cols = append(cols, name)
		}
	}
	for _, f := range sel.Fields {
		switch f.Type {
		case qcode.FieldTypeCol:
			add(f.Col.Name)
		case qcode.FieldTypeFunc:
			return nil, fmt.Errorf("cassandra: aggregate/function field %q is not supported", f.FieldName)
		}
	}
	for _, k := range sel.Ti.PartitionKeys {
		add(k)
	}
	for _, k := range sel.Ti.ClusteringKeys {
		add(k)
	}
	if len(cols) == 0 {
		return nil, fmt.Errorf("cassandra: selection %q has no scalar columns", sel.FieldName)
	}
	return cols, nil
}

// buildFilter translates a qcode Exp tree to a casFilter, rejecting operators CQL
// can't serve (OR/NOT/!=/LIKE/IS NULL/…) as compile errors. ok=false drops a node
// (e.g. @skip/@include variable conditions).
func (b *casBuilder) buildFilter(ti sdata.DBTable, ex *qcode.Exp) (casFilter, bool, error) {
	switch ex.Op {
	case qcode.OpAnd:
		var parts []casFilter
		for _, c := range ex.Children {
			f, ok, err := b.buildFilter(ti, c)
			if err != nil {
				return casFilter{}, false, err
			}
			if ok {
				parts = append(parts, f)
			}
		}
		switch len(parts) {
		case 0:
			return casFilter{}, false, nil
		case 1:
			return parts[0], true, nil
		default:
			return casFilter{And: parts}, true, nil
		}

	case qcode.OpOr:
		return casFilter{}, false, fmt.Errorf("cassandra: OR is not servable — CQL WHERE is a conjunction (a cross-partition scan)")
	case qcode.OpNot:
		return casFilter{}, false, fmt.Errorf("cassandra: NOT is not servable in CQL WHERE")
	case qcode.OpEqualsTrue, qcode.OpNotEqualsTrue, qcode.OpFalse:
		return casFilter{}, false, nil // @skip/@include variable conditions, not real predicates

	case qcode.OpEquals, qcode.OpIn,
		qcode.OpGreaterThan, qcode.OpGreaterOrEquals,
		qcode.OpLesserThan, qcode.OpLesserOrEquals:
		return b.buildLeaf(ti, ex)

	default:
		col := leafCol(ex)
		return casFilter{}, false, fmt.Errorf(
			"cassandra: filter %s on column %q is not servable in CQL", opName(ex.Op), col)
	}
}

func (b *casBuilder) buildLeaf(ti sdata.DBTable, ex *qcode.Exp) (casFilter, bool, error) {
	// A column-reference RHS is either the parent↔child join correlation GraphJin
	// injects into a nested select (handled by the N+1 rel, so skip it) or a
	// same-table column comparison (not servable in CQL).
	if isColRef(ex) {
		refTable := ex.Right.Col.Table
		if refTable == "" {
			refTable = ex.Right.Table
		}
		if refTable != "" && refTable != ti.Name {
			return casFilter{}, false, nil
		}
		return casFilter{}, false, fmt.Errorf("cassandra: column-to-column comparison on %q is not servable in CQL", leafCol(ex))
	}

	f := casFilter{Col: leafCol(ex), Op: opToCas(ex.Op)}
	switch ex.Right.ValType {
	case qcode.ValVar:
		f.Param = b.param(Param{
			Name:    ex.Right.Val,
			Type:    ex.Left.Col.Type,
			IsArray: ex.Op == qcode.OpIn,
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

func (b *casBuilder) applyPaging(sel *qcode.Select, n *casNode) {
	p := sel.Paging
	if p.Limit > 0 {
		n.PageSize = int(p.Limit)
	}
	switch p.Type {
	case qcode.PTForward:
		n.Paging = "forward"
	case qcode.PTBackward:
		n.Paging = "backward"
	case qcode.PTOffset:
		if p.Offset > 0 || p.OffsetVar != "" {
			n.Paging = "offset"
		}
	}
	if p.Cursor && p.CursorVar != "" {
		n.CursorParam = b.param(Param{Name: p.CursorVar, Type: "text"})
	}
}

// ---- Mutation compilation ----

func (d *CassandraDialect) CompileFullMutation(ctx Context, qc *qcode.QCode) bool {
	if len(qc.Mutates) == 0 {
		ctx.SetError(fmt.Errorf("cassandra: empty mutation"))
		return true
	}
	if len(qc.Mutates) > 1 {
		ctx.SetError(fmt.Errorf("cassandra: nested/related writes are not supported (single-table by primary key only)"))
		return true
	}
	b := &casBuilder{}
	mut, err := b.buildMutation(qc, &qc.Mutates[0])
	if err != nil {
		ctx.SetError(err)
		return true
	}
	b.emit(ctx, casDSL{Operation: mut.Type, Mutation: mut})
	return true
}

func (b *casBuilder) buildMutation(qc *qcode.QCode, m *qcode.Mutate) (*casMutation, error) {
	typ, err := mTypeToCas(m.Type)
	if err != nil {
		return nil, err
	}
	mut := &casMutation{
		Type:           typ,
		Keyspace:       m.Ti.Schema,
		Table:          m.Ti.Name,
		PartitionKeys:  m.Ti.PartitionKeys,
		ClusteringKeys: m.Ti.ClusteringKeys,
		CounterCols:    counterCols(m.Ti),
		ColumnTypes:    columnTypes(m.Ti),
	}
	pk := pkSet(m.Ti)

	// The row-identifying WHERE for update/delete lives on the associated select
	// (the GraphQL `where`), not on m.Cols/m.Where.
	buildWhere := func() error {
		ex := mutateWhereExp(qc, m)
		if ex == nil {
			return nil
		}
		f, ok, err := b.buildFilter(m.Ti, ex)
		if err != nil {
			return err
		}
		if ok {
			mut.Where = flattenWhere(f)
		}
		return nil
	}

	jsonInput := m.IsJSON && qc.ActionVar != ""

	switch typ {
	case "insert", "upsert":
		if jsonInput {
			mut.RawInput = b.param(Param{Name: qc.ActionVar, Type: "json"})
		} else {
			for _, c := range m.Cols {
				if c.Set {
					mut.Set = append(mut.Set, b.assign(c))
				}
			}
		}
	case "update":
		if jsonInput {
			mut.RawInput = b.param(Param{Name: qc.ActionVar, Type: "json"})
		} else {
			for _, c := range m.Cols {
				if c.Set && !pk[c.Col.Name] {
					mut.Set = append(mut.Set, b.assign(c))
				}
			}
		}
		if err := buildWhere(); err != nil {
			return nil, err
		}
	case "delete":
		if err := buildWhere(); err != nil {
			return nil, err
		}
	}

	mut.Returning = b.buildReturning(qc, m)
	if (typ == "update" || typ == "delete") && len(mut.Where) > 0 {
		mut.Returning.Filters = mut.Where // read-after-write / pre-read SELECT-by-PK
	}
	return mut, nil
}

func selectForMutate(qc *qcode.QCode, m *qcode.Mutate) *qcode.Select {
	for i := range qc.Selects {
		if qc.Selects[i].ID == m.SelID {
			return &qc.Selects[i]
		}
	}
	if len(qc.Roots) > 0 {
		return &qc.Selects[qc.Roots[0]]
	}
	return nil
}

func mutateWhereExp(qc *qcode.QCode, m *qcode.Mutate) *qcode.Exp {
	if m.Where.Exp != nil {
		return m.Where.Exp
	}
	if sel := selectForMutate(qc, m); sel != nil {
		return sel.Where.Exp
	}
	return nil
}

func (b *casBuilder) assign(c qcode.MColumn) casAssign {
	return casAssign{Col: c.Col.Name, Param: b.colParam(c)}
}

func (b *casBuilder) colParam(c qcode.MColumn) string {
	name := strings.TrimPrefix(c.Value, "$")
	return b.param(Param{Name: name, Type: c.Col.Type})
}

// buildReturning constructs the read-after-write SELECT-by-PK from the mutation's
// associated select, so GraphJin gets the mutated object back.
func (b *casBuilder) buildReturning(qc *qcode.QCode, m *qcode.Mutate) *casNode {
	sel := selectForMutate(qc, m)
	rn := &casNode{
		Keyspace:       m.Ti.Schema,
		Table:          m.Ti.Name,
		Singular:       true,
		PartitionKeys:  m.Ti.PartitionKeys,
		ClusteringKeys: m.Ti.ClusteringKeys,
		ColumnTypes:    columnTypes(m.Ti),
	}
	if sel != nil {
		rn.FieldName = sel.FieldName
		rn.Singular = sel.Singular
		for _, f := range sel.Fields {
			if f.Type == qcode.FieldTypeCol {
				rn.Columns = ensureCol(rn.Columns, f.Col.Name)
			}
		}
	}
	for _, k := range m.Ti.PartitionKeys {
		rn.Columns = ensureCol(rn.Columns, k)
	}
	for _, k := range m.Ti.ClusteringKeys {
		rn.Columns = ensureCol(rn.Columns, k)
	}
	return rn
}

// ---- helpers ----

func leafCol(ex *qcode.Exp) string {
	if ex.Left.Col.Name != "" {
		return ex.Left.Col.Name
	}
	return ex.Left.ColName
}

// isColRef reports whether the RHS of a comparison is a column reference rather
// than a literal/variable value.
func isColRef(ex *qcode.Exp) bool {
	return ex.Right.Col.Name != "" && ex.Right.ValType != qcode.ValVar && ex.Right.ValType != qcode.ValList
}

func columnTypes(ti sdata.DBTable) map[string]string {
	if len(ti.Columns) == 0 {
		return nil
	}
	m := make(map[string]string, len(ti.Columns))
	for _, c := range ti.Columns {
		m[c.Name] = c.Type
	}
	return m
}

func counterCols(ti sdata.DBTable) []string {
	var out []string
	for _, c := range ti.Columns {
		if strings.EqualFold(c.Type, "counter") {
			out = append(out, c.Name)
		}
	}
	return out
}

func isPartitionKey(ti sdata.DBTable, col string) bool {
	for _, k := range ti.PartitionKeys {
		if k == col {
			return true
		}
	}
	return false
}

func pkSet(ti sdata.DBTable) map[string]bool {
	m := map[string]bool{}
	for _, k := range ti.PartitionKeys {
		m[k] = true
	}
	for _, k := range ti.ClusteringKeys {
		m[k] = true
	}
	return m
}

// relColumns resolves the parent/child join columns for a child select, the same
// orientation MongoDB uses for $lookup.
func relColumns(parent, child *qcode.Select) (*casRel, error) {
	rel := child.Rel
	switch rel.Type {
	case sdata.RelOneToOne, sdata.RelOneToMany:
		if rel.Right.Ti.Name == parent.Table {
			return &casRel{ParentCol: rel.Right.Col.Name, ChildCol: rel.Left.Col.Name}, nil
		}
		return &casRel{ParentCol: rel.Left.Col.Name, ChildCol: rel.Right.Col.Name}, nil
	default:
		return nil, fmt.Errorf("cassandra: relationship kind for %q is not supported (only one-to-one / one-to-many)", child.FieldName)
	}
}

func ensureCol(cols []string, c string) []string {
	for _, x := range cols {
		if x == c {
			return cols
		}
	}
	return append(cols, c)
}

func flattenWhere(f casFilter) []casFilter {
	if len(f.And) > 0 {
		out := make([]casFilter, 0, len(f.And))
		for _, c := range f.And {
			out = append(out, flattenWhere(c)...)
		}
		return out
	}
	return []casFilter{f}
}

func opToCas(op qcode.ExpOp) string {
	switch op {
	case qcode.OpEquals:
		return "eq"
	case qcode.OpIn:
		return "in"
	case qcode.OpGreaterThan:
		return "gt"
	case qcode.OpGreaterOrEquals:
		return "gte"
	case qcode.OpLesserThan:
		return "lt"
	case qcode.OpLesserOrEquals:
		return "lte"
	default:
		return ""
	}
}

func opName(op qcode.ExpOp) string {
	switch op {
	case qcode.OpNotEquals:
		return "not-equals"
	case qcode.OpNotIn:
		return "not-in"
	case qcode.OpLike, qcode.OpILike, qcode.OpSimilar, qcode.OpRegex, qcode.OpIRegex:
		return "pattern-match"
	case qcode.OpIsNull, qcode.OpIsNotNull:
		return "is-null"
	case qcode.OpContains, qcode.OpContainedIn:
		return "collection-containment"
	default:
		return "operator"
	}
}

func orderString(o qcode.Order) string {
	switch o {
	case qcode.OrderDesc, qcode.OrderDescNullsFirst, qcode.OrderDescNullsLast:
		return "desc"
	default:
		return "asc"
	}
}

func literalValue(vt qcode.ValType, v string) any {
	switch vt {
	case qcode.ValNum:
		if i, err := strconv.ParseInt(v, 10, 64); err == nil {
			return i
		}
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
		return v
	case qcode.ValBool:
		return v == "true"
	default:
		return v
	}
}

func mTypeToCas(t qcode.MType) (string, error) {
	switch t {
	case qcode.MTInsert:
		return "insert", nil
	case qcode.MTUpdate:
		return "update", nil
	case qcode.MTUpsert:
		return "upsert", nil
	case qcode.MTDelete:
		return "delete", nil
	case qcode.MTConnect, qcode.MTDisconnect:
		return "", fmt.Errorf("cassandra: connect/disconnect writes are not supported (no FKs, no cross-partition writes)")
	default:
		return "", fmt.Errorf("cassandra: unsupported mutation type")
	}
}
