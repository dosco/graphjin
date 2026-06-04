package cassandradriver

import (
	"fmt"
	"sort"
	"strings"
)

// cqlOp maps a DSL operator to its CQL symbol. isIn marks the IN operator, whose
// bind is a single slice value rather than a scalar.
func cqlOp(op string) (sym string, isIn bool, ok bool) {
	switch op {
	case OpEq:
		return "=", false, true
	case OpIn:
		return "IN", true, true
	case OpGt:
		return ">", false, true
	case OpGte:
		return ">=", false, true
	case OpLt:
		return "<", false, true
	case OpLte:
		return "<=", false, true
	case OpContains, OpContainedIn:
		return "CONTAINS", false, true
	default:
		return "", false, false
	}
}

// asSlice coerces an IN operand into a slice; a scalar becomes a single-element list.
func asSlice(v any) []any {
	switch x := v.(type) {
	case []any:
		return x
	case nil:
		return nil
	default:
		return []any{x}
	}
}

func qualified(keyspace, table string) string {
	if keyspace == "" {
		return table
	}
	return keyspace + "." + table
}

func orderDir(order string) string {
	if strings.HasPrefix(strings.ToLower(order), "desc") {
		return "DESC"
	}
	return "ASC"
}

// BuildSelect emits the SELECT for one validated read node and the ordered binds.
func BuildSelect(p *ReadPlan) (string, []any, error) {
	n := p.Node
	var b strings.Builder
	b.WriteString("SELECT ")
	if len(n.Distinct) > 0 {
		b.WriteString("DISTINCT ")
		b.WriteString(strings.Join(n.Distinct, ", "))
	} else {
		b.WriteString(strings.Join(n.Columns, ", "))
	}
	b.WriteString(" FROM ")
	b.WriteString(qualified(n.Keyspace, n.Table))

	var args []any
	if len(p.Where) > 0 {
		clauses := make([]string, 0, len(p.Where))
		for _, w := range p.Where {
			sym, isIn, ok := cqlOp(w.Op)
			if !ok {
				return "", nil, fmt.Errorf("cassandradriver: cannot emit CQL for operator %q", w.Op)
			}
			if isIn {
				// Expand IN into discrete placeholders — gocql binds each scalar by
				// its column type, avoiding the slice-bind ambiguity of `IN ?`.
				vals := asSlice(w.Value)
				marks := make([]string, len(vals))
				for i := range vals {
					marks[i] = "?"
				}
				clauses = append(clauses, fmt.Sprintf("%s IN (%s)", w.Col, strings.Join(marks, ", ")))
				args = append(args, vals...)
			} else {
				clauses = append(clauses, fmt.Sprintf("%s %s ?", w.Col, sym))
				args = append(args, w.Value)
			}
		}
		b.WriteString(" WHERE ")
		b.WriteString(strings.Join(clauses, " AND "))
	}

	if len(n.OrderBy) > 0 {
		parts := make([]string, len(n.OrderBy))
		for i, o := range n.OrderBy {
			parts[i] = o.Col + " " + orderDir(o.Order)
		}
		b.WriteString(" ORDER BY ")
		b.WriteString(strings.Join(parts, ", "))
	}

	if n.Limit > 0 {
		fmt.Fprintf(&b, " LIMIT %d", n.Limit)
	}

	if p.AllowFiltering {
		b.WriteString(" ALLOW FILTERING")
	}

	return b.String(), args, nil
}

// mutationKeyOrder ranks a column by its position in the full primary key for
// deterministic WHERE/INSERT ordering.
func mutationKeyOrder(m *Mutation) map[string]int {
	rank := make(map[string]int)
	i := 0
	for _, c := range m.PartitionKeys {
		rank[c] = i
		i++
	}
	for _, c := range m.ClusteringKeys {
		rank[c] = i
		i++
	}
	return rank
}

// BuildInsert emits an INSERT (CQL upsert) and its binds.
func BuildInsert(m *Mutation) (string, []any, error) {
	if len(m.Set) == 0 {
		return "", nil, fmt.Errorf("cassandradriver: INSERT has no columns")
	}
	cols := make([]string, len(m.Set))
	placeholders := make([]string, len(m.Set))
	args := make([]any, len(m.Set))
	for i, a := range m.Set {
		cols[i] = a.Col
		placeholders[i] = "?"
		args[i] = a.Value
	}
	cql := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		qualified(m.Keyspace, m.Table),
		strings.Join(cols, ", "),
		strings.Join(placeholders, ", "))
	if m.IfNotExists {
		cql += " IF NOT EXISTS"
	}
	return cql, args, nil
}

// BuildUpdate emits an UPDATE (also an upsert) and its binds: SET then WHERE.
func BuildUpdate(m *Mutation) (string, []any, error) {
	if len(m.Set) == 0 {
		return "", nil, fmt.Errorf("cassandradriver: UPDATE has no SET assignments")
	}
	var args []any
	sets := make([]string, len(m.Set))
	for i, a := range m.Set {
		if a.Counter {
			sets[i] = fmt.Sprintf("%s = %s + ?", a.Col, a.Col)
		} else {
			sets[i] = fmt.Sprintf("%s = ?", a.Col)
		}
		args = append(args, a.Value)
	}
	where, wargs := buildPKWhere(m)
	args = append(args, wargs...)

	cql := fmt.Sprintf("UPDATE %s SET %s WHERE %s",
		qualified(m.Keyspace, m.Table),
		strings.Join(sets, ", "),
		where)
	if m.IfExists {
		cql += " IF EXISTS"
	}
	return cql, args, nil
}

// BuildDelete emits a DELETE by full primary key and its binds.
func BuildDelete(m *Mutation) (string, []any, error) {
	where, args := buildPKWhere(m)
	cql := fmt.Sprintf("DELETE FROM %s WHERE %s", qualified(m.Keyspace, m.Table), where)
	if m.IfExists {
		cql += " IF EXISTS"
	}
	return cql, args, nil
}

func buildPKWhere(m *Mutation) (string, []any) {
	rank := mutationKeyOrder(m)
	ws := make([]Filter, len(m.Where))
	copy(ws, m.Where)
	sort.SliceStable(ws, func(i, j int) bool { return rank[ws[i].Col] < rank[ws[j].Col] })

	clauses := make([]string, len(ws))
	args := make([]any, len(ws))
	for i, w := range ws {
		clauses[i] = w.Col + " = ?"
		args[i] = w.Value
	}
	return strings.Join(clauses, " AND "), args
}
