// Package cassandradriver provides a database/sql compatible driver for
// Apache Cassandra / Amazon Keyspaces. It accepts the JSON query DSL emitted by
// GraphJin's Cassandra dialect and turns it into CQL, executing it over a gocql
// session. The DSL is a selection tree: GraphJin has no joins on Cassandra, so
// nested data is assembled here via bounded, batched N+1 by partition key.
package cassandradriver

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
	OpUpsert            = "upsert"
	OpDelete            = "delete"
	OpIntrospectInfo    = "introspect_info"
	OpIntrospectColumns = "introspect_columns"
	OpIntrospectKeys    = "introspect_keys"
)

// QueryDSL is the root of the JSON DSL GraphJin's Cassandra dialect emits — the
// "SQL" of this driver.
type QueryDSL struct {
	Operation string    `json:"operation"`
	Root      *Node     `json:"root,omitempty"`     // read tree (OpQuery)
	Mutation  *Mutation `json:"mutation,omitempty"` // write (OpInsert/Update/Upsert/Delete)
	Params    []string  `json:"params,omitempty"`   // declared param order, informational

	// Introspection targets (OpIntrospectInfo / OpIntrospectColumns).
	Keyspace string `json:"keyspace,omitempty"`
	Table    string `json:"table,omitempty"`
}

// Node is one level of the read selection tree.
type Node struct {
	Keyspace string   `json:"keyspace,omitempty"`
	Table    string   `json:"table"`
	Columns  []string `json:"columns"`

	Filters  []Filter  `json:"filters,omitempty"` // implicit AND across entries
	OrderBy  []OrderBy `json:"order_by,omitempty"`
	Limit    int       `json:"limit,omitempty"`
	PageSize int       `json:"page_size,omitempty"`
	Distinct []string  `json:"distinct,omitempty"`

	CursorParam    string `json:"cursor_param,omitempty"` // placeholder for incoming page-state
	Paging         string `json:"paging,omitempty"`       // forward|backward|offset
	AllowFiltering bool   `json:"allow_filtering,omitempty"`

	Rel *Rel `json:"rel,omitempty"` // relationship to parent; nil for root

	FieldName string `json:"field_name,omitempty"`
	Singular  bool   `json:"singular,omitempty"`
	Typename  string `json:"typename,omitempty"`

	// Introspected key metadata, carried so plan.go can judge servability and
	// resolve.go can build the child IN-fetch.
	PartitionKeys   []string          `json:"partition_keys,omitempty"`
	ClusteringKeys  []string          `json:"clustering_keys,omitempty"`
	ClusteringOrder map[string]string `json:"clustering_order,omitempty"` // col -> asc|desc
	ColumnTypes     map[string]string `json:"column_types,omitempty"`     // col -> CQL type

	Children []*Node `json:"children,omitempty"`

	resolvedCursor string // inlined cursor string after SubstituteParams (not serialized)
}

// Paging modes mirroring qcode Paging.Type.
const (
	PagingForward  = "forward"
	PagingBackward = "backward"
	PagingOffset   = "offset"
)

// Filter is a predicate node. A leaf carries Col+Op (+Param or Value); a branch
// carries one of And/Or/Not. Or/Not exist only so the planner can reject them —
// CQL's WHERE is a pure conjunction.
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

// Operator strings the dialect maps qcode ExpOp onto. The planner gates each one
// by column key-role; the unsupported ones are listed so rejections are explicit.
const (
	OpEq          = "eq"
	OpIn          = "in"
	OpGt          = "gt"
	OpGte         = "gte"
	OpLt          = "lt"
	OpLte         = "lte"
	OpContains    = "contains"
	OpContainedIn = "containedIn"
	OpNeq         = "neq"
	OpNin         = "nin"
	OpLike        = "like"
	OpILike       = "ilike"
	OpIsNull      = "isNull"
	OpIsNotNull   = "isNotNull"
)

// OrderBy is a single sort key.
type OrderBy struct {
	Col   string `json:"col"`
	Order string `json:"order"` // asc|desc (nulls variants rejected by planner)
}

// Rel describes a child node's relationship to its parent. Cassandra has no FKs,
// so this is config-derived. Kind may be empty — resolve.go derives it from the
// child's key roles.
type Rel struct {
	ParentCol string `json:"parent_col"` // join column on the parent row
	ChildCol  string `json:"child_col"`  // join column on the child table (IN-fetched)
	Kind      string `json:"kind,omitempty"`
}

// Relationship cardinalities.
const (
	RelOneToOne  = "one_to_one"
	RelOneToMany = "one_to_many"
)

// Mutation is a single-table write. Nested/related writes are rejected upstream.
type Mutation struct {
	Type     string       `json:"type"` // insert|update|upsert|delete
	Keyspace string       `json:"keyspace,omitempty"`
	Table    string       `json:"table"`
	Set      []Assignment `json:"set,omitempty"`   // INSERT cols/vals or UPDATE SET
	Where    []Filter     `json:"where,omitempty"` // full-PK eq predicates (update/delete)

	IfNotExists bool `json:"if_not_exists,omitempty"` // LWT on insert
	IfExists    bool `json:"if_exists,omitempty"`     // LWT on update/delete

	PartitionKeys  []string          `json:"partition_keys,omitempty"`
	ClusteringKeys []string          `json:"clustering_keys,omitempty"`
	CounterCols    []string          `json:"counter_cols,omitempty"`
	ColumnTypes    map[string]string `json:"column_types,omitempty"` // col -> CQL type
	RawInput       string            `json:"raw_input,omitempty"`    // param holding the input JSON document

	Returning *Node `json:"returning,omitempty"` // read-after-write SELECT-by-PK tree

	rawDoc map[string]any // parsed RawInput document (not serialized)
}

// Assignment is one column binding in an INSERT or UPDATE SET.
type Assignment struct {
	Col     string `json:"col"`
	Param   string `json:"param,omitempty"`
	Value   any    `json:"value,omitempty"`
	Counter bool   `json:"counter,omitempty"` // emit `col = col + ?` (value carries sign)
}

// ParseQuery parses the JSON DSL into a QueryDSL. A leading GraphJin metadata
// comment (/* ... */) is tolerated and stripped.
func ParseQuery(query string) (*QueryDSL, error) {
	query = stripLeadingComment(query)
	var q QueryDSL
	if err := json.Unmarshal([]byte(query), &q); err != nil {
		return nil, fmt.Errorf("cassandradriver: invalid query DSL: %w", err)
	}
	if q.Operation == "" {
		return nil, fmt.Errorf("cassandradriver: missing operation in query DSL")
	}
	return &q, nil
}

// stripLeadingComment removes a leading /* ... */ block comment (GraphJin prepends
// query metadata as one) so the remaining text is parseable JSON.
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
// values into the filter / assignment / cursor fields so the rest of the pipeline
// works on concrete values.
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
	if n.CursorParam != "" {
		if v, ok := pm[n.CursorParam]; ok {
			if s, ok := v.(string); ok {
				n.CursorParam = "" // consumed; carry the resolved cursor via a synthetic field
				n.resolvedCursor = s
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

// BindValues converts inlined filter/assignment values to gocql-native Go values
// using the per-column CQL types, so the live driver binds blob/timestamp/integer
// columns correctly. Run after SubstituteParams. Best-effort per value; a hard
// conversion error (e.g. malformed base64 blob) is returned.
func (q *QueryDSL) BindValues() error {
	if q.Root != nil {
		if err := q.Root.bindValues(); err != nil {
			return err
		}
	}
	if q.Mutation != nil {
		if err := q.Mutation.bindValues(); err != nil {
			return err
		}
	}
	return nil
}

func (n *Node) bindValues() error {
	for i := range n.Filters {
		if err := n.Filters[i].bindValues(n.ColumnTypes); err != nil {
			return err
		}
	}
	for _, c := range n.Children {
		if err := c.bindValues(); err != nil {
			return err
		}
	}
	return nil
}

func (f *Filter) bindValues(types map[string]string) error {
	if f.isLeaf() {
		t := types[f.Col]
		v, err := bindFilterValue(t, f.Value)
		if err != nil {
			return err
		}
		f.Value = v
		return nil
	}
	for _, group := range [][]Filter{f.And, f.Or, f.Not} {
		for i := range group {
			if err := group[i].bindValues(types); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *Mutation) bindValues() error {
	for i := range m.Set {
		t := m.ColumnTypes[m.Set[i].Col]
		v, err := bindFilterValue(t, m.Set[i].Value)
		if err != nil {
			return err
		}
		m.Set[i].Value = v
	}
	for i := range m.Where {
		if err := m.Where[i].bindValues(m.ColumnTypes); err != nil {
			return err
		}
	}
	if m.Returning != nil {
		return m.Returning.bindValues()
	}
	return nil
}

// applyRawDoc turns a JSON-variable mutation input into concrete SET/WHERE
// bindings (typed via ColumnTypes) and injects the primary-key filters the
// read-after-write SELECT needs. Called before planning/building.
func (m *Mutation) applyRawDoc() error {
	if m.rawDoc == nil {
		return nil
	}
	pk := make(map[string]bool, len(m.PartitionKeys)+len(m.ClusteringKeys))
	for _, k := range m.PartitionKeys {
		pk[k] = true
	}
	for _, k := range m.ClusteringKeys {
		pk[k] = true
	}

	keys := make([]string, 0, len(m.rawDoc))
	for k := range m.rawDoc {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var retFilters []Filter
	for _, k := range keys {
		v, err := bindFilterValue(m.ColumnTypes[k], m.rawDoc[k])
		if err != nil {
			return err
		}
		switch m.Type {
		case OpInsert, OpUpsert:
			m.Set = append(m.Set, Assignment{Col: k, Value: v})
			if pk[k] {
				retFilters = append(retFilters, Filter{Col: k, Op: OpEq, Value: v})
			}
		case OpUpdate:
			if pk[k] {
				m.Where = append(m.Where, Filter{Col: k, Op: OpEq, Value: v})
				retFilters = append(retFilters, Filter{Col: k, Op: OpEq, Value: v})
			} else {
				m.Set = append(m.Set, Assignment{Col: k, Value: v})
			}
		}
	}
	if m.Returning != nil && len(retFilters) > 0 {
		m.Returning.Filters = retFilters
	}
	return nil
}

// bindFilterValue applies BindValue to a scalar, or element-wise to an IN list.
func bindFilterValue(cqlType string, v any) (any, error) {
	if cqlType == "" || v == nil {
		return v, nil
	}
	if arr, ok := v.([]any); ok {
		out := make([]any, len(arr))
		for i, e := range arr {
			bv, err := BindValue(cqlType, e)
			if err != nil {
				return nil, err
			}
			out[i] = bv
		}
		return out, nil
	}
	return BindValue(cqlType, v)
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

// parseDoc coerces a raw input param into a column->value map.
func parseDoc(v any) map[string]any {
	switch x := v.(type) {
	case map[string]any:
		return x
	case json.RawMessage:
		var d map[string]any
		if json.Unmarshal(x, &d) == nil {
			return d
		}
	case []byte:
		var d map[string]any
		if json.Unmarshal(x, &d) == nil {
			return d
		}
	case string:
		var d map[string]any
		if json.Unmarshal([]byte(x), &d) == nil {
			return d
		}
	}
	return nil
}

// parseJSONValue unwraps json.RawMessage / []byte params into Go values so binds
// carry real types rather than raw bytes.
func parseJSONValue(v any) any {
	switch val := v.(type) {
	case json.RawMessage:
		var parsed any
		if err := json.Unmarshal(val, &parsed); err != nil {
			return val
		}
		return parsed
	case []byte:
		var parsed any
		if err := json.Unmarshal(val, &parsed); err != nil {
			return val
		}
		return parsed
	default:
		return v
	}
}
