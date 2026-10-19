package clickhousedriver

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Operation discriminators for the root of the DSL.
const (
	OpQuery             = "query"
	OpInsert            = "insert"
	OpUpdate            = "update"
	OpDelete            = "delete"
	OpIntrospectInfo    = "introspect_info"
	OpIntrospectColumns = "introspect_columns"
)

// Operator strings the dialect maps qcode ExpOp onto. ClickHouse serves the full
// set (no servability gate like Cassandra).
const (
	OpEq        = "eq"
	OpNeq       = "neq"
	OpIn        = "in"
	OpNin       = "nin"
	OpGt        = "gt"
	OpGte       = "gte"
	OpLt        = "lt"
	OpLte       = "lte"
	OpLike      = "like"
	OpILike     = "ilike"
	OpIsNull    = "isNull"
	OpIsNotNull = "isNotNull"
)

// Relationship cardinalities (set by the dialect from the resolved rel type).
const (
	RelOneToOne  = "one_to_one"
	RelOneToMany = "one_to_many"
)

// QueryDSL is the root of the JSON DSL GraphJin's ClickHouse dialect emits.
type QueryDSL struct {
	Operation string    `json:"operation"`
	Root      *Node     `json:"root,omitempty"`
	Mutation  *Mutation `json:"mutation,omitempty"`

	// Introspection targets.
	Database string `json:"database,omitempty"`
	Table    string `json:"table,omitempty"`
}

// Node is one level of the read selection tree.
type Node struct {
	Database string   `json:"database,omitempty"`
	Table    string   `json:"table"`
	Columns  []string `json:"columns"`

	Filters []Filter  `json:"filters,omitempty"` // implicit AND across entries
	OrderBy []OrderBy `json:"order_by,omitempty"`
	Limit   int       `json:"limit,omitempty"`
	Offset  int       `json:"offset,omitempty"`

	OffsetParam string      `json:"offset_param,omitempty"`
	GroupBy     []string    `json:"group_by,omitempty"`
	Aggregates  []Aggregate `json:"aggregates,omitempty"`
	Windows     []Window    `json:"windows,omitempty"`

	Keyset *Keyset `json:"keyset,omitempty"`

	Rel       *Rel   `json:"rel,omitempty"` // relationship to parent; nil for root
	FieldName string `json:"field_name,omitempty"`
	Singular  bool   `json:"singular,omitempty"`
	Typename  string `json:"typename,omitempty"`

	Children []*Node `json:"children,omitempty"`

	resolvedOffset int // inlined offset_param after SubstituteParams
}

// Filter is a predicate node: a leaf carries Col+Op (+Param or Value); a branch
// carries one of And/Or/Not.
type Filter struct {
	Col   string `json:"col,omitempty"`
	Op    string `json:"op,omitempty"`
	Param string `json:"param,omitempty"`
	Value any    `json:"value,omitempty"`

	And []Filter `json:"and,omitempty"`
	Or  []Filter `json:"or,omitempty"`
	Not []Filter `json:"not,omitempty"`
}

func (f Filter) isLeaf() bool { return f.Col != "" }

// OrderBy is a single sort key.
type OrderBy struct {
	Col      string `json:"col"`
	Order    string `json:"order"` // asc|desc
	Nullable bool   `json:"nullable,omitempty"`
}

// Aggregate is a leaf aggregate function over a column or a pre-rendered SQL
// scalar expression (count/sum/avg/min/max).
type Aggregate struct {
	Fn    string `json:"fn"`
	Col   string `json:"col,omitempty"`
	Expr  string `json:"expr,omitempty"`
	Alias string `json:"alias"`
}

// Window is an analytic window field rendered as fn(arg) OVER (...).
type Window struct {
	Fn        string    `json:"fn"`
	Arg       string    `json:"arg,omitempty"`
	Partition []string  `json:"partition,omitempty"`
	OrderBy   []OrderBy `json:"order_by,omitempty"`
	Frame     string    `json:"frame,omitempty"`
	Alias     string    `json:"alias"`
}

// Keyset captures the ORDER BY seek so the driver rebuilds the SQL seek ladder.
type Keyset struct {
	SelID       int       `json:"sel_id"`
	Prefix      string    `json:"prefix"`
	CursorParam string    `json:"cursor_param,omitempty"`
	Columns     []OrderBy `json:"columns"`
	Backward    bool      `json:"backward,omitempty"`

	resolvedCursor string // inlined cursor_param after SubstituteParams
}

// Rel describes a child node's relationship to its parent. ClickHouse has no FKs,
// so this is resolved upstream by GraphJin and carried here.
type Rel struct {
	ParentCol string `json:"parent_col"` // join column on the parent row
	ChildCol  string `json:"child_col"`  // join column on the child table (IN-fetched)
	Kind      string `json:"kind,omitempty"`
}

// Mutation is a single-table write. Nested/related writes are rejected upstream.
type Mutation struct {
	Type     string       `json:"type"` // insert|update|delete
	Database string       `json:"database,omitempty"`
	Table    string       `json:"table"`
	Set      []Assignment `json:"set,omitempty"`
	Where    []Filter     `json:"where,omitempty"`

	RawInput    string `json:"raw_input,omitempty"`   // param holding input JSON document
	Lightweight bool   `json:"lightweight,omitempty"` // DELETE FROM vs ALTER..DELETE
	PrimaryKey  string `json:"primary_key,omitempty"` // for read-after-write filter

	ColumnTypes map[string]string `json:"column_types,omitempty"`

	Returning *Node `json:"returning,omitempty"`

	rawDoc map[string]any // parsed RawInput document (not serialized)
}

// applyRawDoc turns a JSON-input mutation document into Set assignments and, for
// insert, the read-after-write filter on the primary key. Values are coerced to
// the Go types clickhouse-go binds for the column.
func (m *Mutation) applyRawDoc() {
	if m.rawDoc == nil {
		return
	}
	keys := make([]string, 0, len(m.rawDoc))
	for k := range m.rawDoc {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var retFilters []Filter
	for _, k := range keys {
		v := coerceForCol(m.ColumnTypes[k], m.rawDoc[k])
		switch m.Type {
		case OpInsert:
			m.Set = append(m.Set, Assignment{Col: k, Value: v})
			if k == m.PrimaryKey {
				retFilters = append(retFilters, Filter{Col: k, Op: OpEq, Value: v})
			}
		case OpUpdate:
			m.Set = append(m.Set, Assignment{Col: k, Value: v})
		}
	}
	if m.Returning != nil && len(retFilters) > 0 {
		m.Returning.Filters = retFilters
	}
}

// Assignment is one column binding in an INSERT or UPDATE SET.
type Assignment struct {
	Col   string `json:"col"`
	Param string `json:"param,omitempty"`
	Value any    `json:"value,omitempty"`
}

// ParseQuery parses the JSON DSL into a QueryDSL, tolerating a leading GraphJin
// metadata comment (/* ... */).
func ParseQuery(query string) (*QueryDSL, error) {
	query = stripLeadingComment(query)
	var q QueryDSL
	if err := json.Unmarshal([]byte(query), &q); err != nil {
		return nil, fmt.Errorf("clickhousedriver: invalid query DSL: %w", err)
	}
	if q.Operation == "" {
		return nil, fmt.Errorf("clickhousedriver: missing operation in query DSL")
	}
	return &q, nil
}

func stripLeadingComment(s string) string {
	t := strings.TrimSpace(s)
	if strings.HasPrefix(t, "/*") {
		if i := strings.Index(t, "*/"); i >= 0 {
			return strings.TrimSpace(t[i+2:])
		}
	}
	return t
}

// SubstituteParams resolves $N placeholders against positional args, inlining the
// values into filter / assignment / offset / cursor fields.
func (q *QueryDSL) SubstituteParams(args []any) error {
	pm := make(map[string]any, len(args))
	for i, a := range args {
		pm[fmt.Sprintf("$%d", i+1)] = a
	}
	if q.Root != nil {
		q.Root.substitute(pm)
	}
	if q.Mutation != nil {
		q.Mutation.substitute(pm)
	}
	return nil
}

func (n *Node) substitute(pm map[string]any) {
	for i := range n.Filters {
		n.Filters[i].substitute(pm)
	}
	if n.OffsetParam != "" {
		if v, ok := pm[n.OffsetParam]; ok {
			n.resolvedOffset = toInt(v)
		}
	}
	if n.Keyset != nil && n.Keyset.CursorParam != "" {
		if v, ok := pm[n.Keyset.CursorParam]; ok {
			if s, ok := asStringVal(v); ok {
				n.Keyset.resolvedCursor = s
			}
		}
	}
	for _, c := range n.Children {
		c.substitute(pm)
	}
}

func (f *Filter) substitute(pm map[string]any) {
	if f.isLeaf() {
		if f.Param != "" {
			if v, ok := pm[f.Param]; ok {
				f.Value = parseJSONValue(v)
			}
		}
		return
	}
	for i := range f.And {
		f.And[i].substitute(pm)
	}
	for i := range f.Or {
		f.Or[i].substitute(pm)
	}
	for i := range f.Not {
		f.Not[i].substitute(pm)
	}
}

func (m *Mutation) substitute(pm map[string]any) {
	for i := range m.Set {
		if m.Set[i].Param != "" {
			if v, ok := pm[m.Set[i].Param]; ok {
				m.Set[i].Value = parseJSONValue(v)
			}
		}
	}
	for i := range m.Where {
		m.Where[i].substitute(pm)
	}
	if m.RawInput != "" {
		if v, ok := pm[m.RawInput]; ok {
			m.rawDoc = parseDoc(v)
		}
	}
	if m.Returning != nil {
		m.Returning.substitute(pm)
	}
}
