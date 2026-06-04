package cassandradriver

import (
	"fmt"
	"sort"
	"strings"
)

// Predicate is a flattened, validated leaf filter ready for CQL emission.
type Predicate struct {
	Col   string
	Op    string
	Param string
	Value any
}

// ReadPlan is the servable shape of a read node: validated WHERE predicates in
// CQL key order, plus paging direction.
type ReadPlan struct {
	Node           *Node
	Where          []Predicate
	AllowFiltering bool
	Reversed       bool // ORDER BY reverses the table's clustering order
}

// keyRoles indexes a node's columns by role for matrix checks.
type keyRoles struct {
	partition map[string]bool
	cluster   map[string]int // col -> clustering position (0-based)
	order     map[string]string
}

func rolesOf(n *Node) keyRoles {
	r := keyRoles{
		partition: make(map[string]bool, len(n.PartitionKeys)),
		cluster:   make(map[string]int, len(n.ClusteringKeys)),
		order:     n.ClusteringOrder,
	}
	for _, c := range n.PartitionKeys {
		r.partition[c] = true
	}
	for i, c := range n.ClusteringKeys {
		r.cluster[c] = i
	}
	return r
}

func (r keyRoles) isPartition(c string) bool { _, ok := r.partition[c]; return ok }
func (r keyRoles) clusterPos(c string) (int, bool) {
	i, ok := r.cluster[c]
	return i, ok
}

func keyDesc(n *Node) string {
	return fmt.Sprintf("partition key (%s)", strings.Join(n.PartitionKeys, ", ")) +
		func() string {
			if len(n.ClusteringKeys) == 0 {
				return ""
			}
			return ", clustering key (" + strings.Join(n.ClusteringKeys, ", ") + ")"
		}()
}

// PlanRead validates a read node against CQL/Keyspaces servability rules and
// returns its servable plan, or a compile error naming the offending column,
// operator, and the table's key.
func PlanRead(n *Node) (*ReadPlan, error) {
	roles := rolesOf(n)

	preds, err := flattenFilters(n.Filters)
	if err != nil {
		return nil, err
	}

	for _, p := range preds {
		if err := checkOperator(n, roles, p); err != nil {
			return nil, err
		}
	}

	if !n.AllowFiltering {
		if err := checkFullPartitionKey(n, roles, preds); err != nil {
			return nil, err
		}
		if err := checkClusteringPrefix(n, roles, preds); err != nil {
			return nil, err
		}
	}

	reversed, err := checkOrderBy(n, roles)
	if err != nil {
		return nil, err
	}
	if err := checkPaging(n); err != nil {
		return nil, err
	}

	return &ReadPlan{
		Node:           n,
		Where:          orderPredicates(n, roles, preds),
		AllowFiltering: n.AllowFiltering,
		Reversed:       reversed,
	}, nil
}

// flattenFilters collapses the implicit-AND filter list into leaf predicates and
// rejects any OR/NOT — CQL's WHERE is a pure conjunction.
func flattenFilters(fs []Filter) ([]Predicate, error) {
	var out []Predicate
	var walk func(f Filter) error
	walk = func(f Filter) error {
		switch {
		case len(f.Or) > 0:
			return fmt.Errorf("cassandradriver: OR is not servable — CQL WHERE is a conjunction; an OR across partitions is a multi-partition scan")
		case len(f.Not) > 0:
			return fmt.Errorf("cassandradriver: NOT is not servable in CQL WHERE")
		case len(f.And) > 0:
			for _, c := range f.And {
				if err := walk(c); err != nil {
					return err
				}
			}
			return nil
		case f.isLeaf():
			out = append(out, Predicate{Col: f.Col, Op: f.Op, Param: f.Param, Value: f.Value})
			return nil
		default:
			return nil // empty filter
		}
	}
	for _, f := range fs {
		if err := walk(f); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// checkOperator applies the operator × key-role matrix to one predicate.
func checkOperator(n *Node, roles keyRoles, p Predicate) error {
	reject := func(reason string) error {
		return fmt.Errorf("cassandradriver: filter %q on column %q is not servable (%s) — table key: %s",
			p.Op, p.Col, reason, keyDesc(n))
	}

	switch p.Op {
	case OpNeq, OpNin:
		return reject("CQL WHERE has no != / NOT IN")
	case OpLike, OpILike, "similar", "regex", "iregex", "nregex", "niregex":
		return reject("CQL has no LIKE/regex without SASI; absent on Keyspaces")
	case OpIsNull, OpIsNotNull:
		return reject("CQL rows have no queryable NULL state")
	case OpContains, OpContainedIn:
		if n.AllowFiltering {
			return nil
		}
		return reject("collection containment needs a CQL index")
	case OpEq:
		if roles.isPartition(p.Col) {
			return nil
		}
		if _, ok := roles.clusterPos(p.Col); ok {
			return nil
		}
		if n.AllowFiltering {
			return nil
		}
		return reject("non-key column requires allow_filtering")
	case OpIn:
		if roles.isPartition(p.Col) {
			return nil
		}
		if pos, ok := roles.clusterPos(p.Col); ok {
			if pos == len(n.ClusteringKeys)-1 {
				return nil
			}
			return reject("IN on a clustering key is only servable on the terminal clustering column")
		}
		if n.AllowFiltering {
			return nil
		}
		return reject("non-key column requires allow_filtering")
	case OpGt, OpGte, OpLt, OpLte:
		if roles.isPartition(p.Col) {
			return reject("range predicates are not servable on the partition key")
		}
		if _, ok := roles.clusterPos(p.Col); ok {
			return nil // prefix/terminal position checked in checkClusteringPrefix
		}
		if n.AllowFiltering {
			return nil
		}
		return reject("range on a non-key column requires allow_filtering")
	default:
		return reject("unsupported operator for CQL")
	}
}

// checkFullPartitionKey enforces that every partition-key column is pinned by =/IN.
func checkFullPartitionKey(n *Node, roles keyRoles, preds []Predicate) error {
	pinned := make(map[string]bool)
	for _, p := range preds {
		if roles.isPartition(p.Col) && (p.Op == OpEq || p.Op == OpIn) {
			pinned[p.Col] = true
		}
	}
	var missing []string
	for _, c := range n.PartitionKeys {
		if !pinned[c] {
			missing = append(missing, c)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("cassandradriver: partial partition key — column(s) %s not pinned by =/IN; "+
			"a read must restrict the full partition key. table key: %s",
			strings.Join(missing, ", "), keyDesc(n))
	}
	return nil
}

// checkClusteringPrefix enforces contiguous prefix restriction and terminal-only ranges.
func checkClusteringPrefix(n *Node, roles keyRoles, preds []Predicate) error {
	restricted := make(map[int]string) // clustering pos -> op
	for _, p := range preds {
		if pos, ok := roles.clusterPos(p.Col); ok {
			restricted[pos] = p.Op
		}
	}
	if len(restricted) == 0 {
		return nil
	}
	maxPos := -1
	for pos := range restricted {
		if pos > maxPos {
			maxPos = pos
		}
	}
	for i := 0; i <= maxPos; i++ {
		op, ok := restricted[i]
		if !ok {
			return fmt.Errorf("cassandradriver: clustering key %q is restricted without restricting the "+
				"preceding clustering column(s) — restrictions must form a prefix. table key: %s",
				n.ClusteringKeys[maxPos], keyDesc(n))
		}
		isRange := op == OpGt || op == OpGte || op == OpLt || op == OpLte
		if isRange && i != maxPos {
			return fmt.Errorf("cassandradriver: range on clustering key %q is only servable on the terminal "+
				"restricted clustering column. table key: %s", n.ClusteringKeys[i], keyDesc(n))
		}
	}
	return nil
}

// checkOrderBy permits ordering only on a clustering-key prefix, uniformly in the
// table's clustering order or uniformly reversed. Returns whether it's reversed.
func checkOrderBy(n *Node, roles keyRoles) (bool, error) {
	if len(n.OrderBy) == 0 {
		return false, nil
	}
	var reversed *bool
	for i, o := range n.OrderBy {
		if strings.Contains(o.Order, "nulls") {
			return false, fmt.Errorf("cassandradriver: ORDER BY %q is not servable — CQL has no NULLS FIRST/LAST", o.Col)
		}
		pos, ok := roles.clusterPos(o.Col)
		if !ok {
			return false, fmt.Errorf("cassandradriver: ORDER BY %q is not servable — only clustering columns are orderable. table key: %s",
				o.Col, keyDesc(n))
		}
		if pos != i {
			return false, fmt.Errorf("cassandradriver: ORDER BY must follow clustering order; %q is out of position. table key: %s",
				o.Col, keyDesc(n))
		}
		defOrder := "asc"
		if roles.order != nil && roles.order[o.Col] != "" {
			defOrder = roles.order[o.Col]
		}
		rev := !strings.EqualFold(o.Order, defOrder)
		if reversed == nil {
			reversed = &rev
		} else if *reversed != rev {
			return false, fmt.Errorf("cassandradriver: ORDER BY mixes directions — CQL allows only the clustering order or its full reverse")
		}
	}
	return reversed != nil && *reversed, nil
}

func checkPaging(n *Node) error {
	switch n.Paging {
	case "", PagingForward:
		return nil
	case PagingOffset:
		return fmt.Errorf("cassandradriver: offset paging is not servable — Cassandra paging is page-state based, never OFFSET")
	case PagingBackward:
		if len(n.ClusteringKeys) == 0 {
			return fmt.Errorf("cassandradriver: backward paging needs reverse clustering order, which this table lacks")
		}
		return nil
	default:
		return fmt.Errorf("cassandradriver: unknown paging mode %q", n.Paging)
	}
}

// orderPredicates sorts WHERE predicates into CQL key order (partition, then
// clustering prefix, then any allow_filtering extras) for deterministic CQL.
func orderPredicates(n *Node, roles keyRoles, preds []Predicate) []Predicate {
	rank := func(p Predicate) int {
		for i, c := range n.PartitionKeys {
			if c == p.Col {
				return i
			}
		}
		if pos, ok := roles.clusterPos(p.Col); ok {
			return 1000 + pos
		}
		return 2000
	}
	out := make([]Predicate, len(preds))
	copy(out, preds)
	sort.SliceStable(out, func(i, j int) bool { return rank(out[i]) < rank(out[j]) })
	return out
}

// PlanMutation validates a single-table write against the full-PK and counter rules.
func PlanMutation(m *Mutation) error {
	counters := make(map[string]bool, len(m.CounterCols))
	for _, c := range m.CounterCols {
		counters[c] = true
	}
	fullPK := append(append([]string{}, m.PartitionKeys...), m.ClusteringKeys...)

	switch m.Type {
	case OpInsert, OpUpsert:
		set := make(map[string]bool, len(m.Set))
		for _, a := range m.Set {
			if counters[a.Col] {
				return fmt.Errorf("cassandradriver: counter column %q cannot be set by INSERT/UPSERT — use an increment UPDATE", a.Col)
			}
			set[a.Col] = true
		}
		for _, k := range fullPK {
			if !set[k] {
				return fmt.Errorf("cassandradriver: %s must bind the full primary key; column %q is missing", m.Type, k)
			}
		}
	case OpUpdate:
		if err := requireFullPKWhere(m, fullPK); err != nil {
			return err
		}
	case OpDelete:
		if len(m.Set) != 0 {
			return fmt.Errorf("cassandradriver: DELETE takes no SET assignments")
		}
		if err := requireFullPKWhere(m, fullPK); err != nil {
			return err
		}
	default:
		return fmt.Errorf("cassandradriver: unsupported mutation type %q (nested/connect writes are rejected)", m.Type)
	}
	return nil
}

func requireFullPKWhere(m *Mutation, fullPK []string) error {
	pinned := make(map[string]bool, len(m.Where))
	for _, w := range m.Where {
		if !w.isLeaf() {
			return fmt.Errorf("cassandradriver: %s WHERE must be a flat conjunction of primary-key equalities", m.Type)
		}
		if w.Op != OpEq {
			return fmt.Errorf("cassandradriver: %s WHERE allows only =, got %q on %q (by-PK writes only)", m.Type, w.Op, w.Col)
		}
		pinned[w.Col] = true
	}
	for _, k := range fullPK {
		if !pinned[k] {
			return fmt.Errorf("cassandradriver: %s must pin the full primary key; column %q is missing from WHERE", m.Type, k)
		}
	}
	return nil
}
