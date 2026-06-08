package clickhousedriver

import (
	"fmt"
	"strings"
)

func quoteIdent(s string) string {
	return "`" + strings.ReplaceAll(s, "`", "``") + "`"
}

func qualified(database, table string) string {
	if database == "" {
		return quoteIdent(table)
	}
	return quoteIdent(database) + "." + quoteIdent(table)
}

func orderDir(order string) string {
	if strings.HasPrefix(strings.ToLower(order), "desc") {
		return "DESC"
	}
	return "ASC"
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

func opSymbol(op string) (string, bool) {
	switch op {
	case OpEq:
		return "=", true
	case OpNeq:
		return "!=", true
	case OpGt:
		return ">", true
	case OpGte:
		return ">=", true
	case OpLt:
		return "<", true
	case OpLte:
		return "<=", true
	case OpLike:
		return "LIKE", true
	case OpILike:
		return "ILIKE", true
	default:
		return "", false
	}
}

func renderAggregate(a Aggregate) string {
	arg := "*"
	if a.Expr != "" {
		arg = a.Expr // pre-rendered ClickHouse SQL expression
	} else if a.Col != "" && a.Col != "*" {
		arg = quoteIdent(a.Col)
	}
	expr := strings.ToLower(a.Fn) + "(" + arg + ")"
	if a.Alias != "" {
		expr += " AS " + quoteIdent(a.Alias)
	}
	return expr
}

// renderWindow renders an analytic field as fn(arg) OVER (PARTITION BY … ORDER BY … frame) AS alias.
func renderWindow(w Window) string {
	arg := ""
	if w.Arg != "" {
		arg = quoteIdent(w.Arg)
	}
	var over []string
	if len(w.Partition) > 0 {
		cols := make([]string, len(w.Partition))
		for i, p := range w.Partition {
			cols[i] = quoteIdent(p)
		}
		over = append(over, "PARTITION BY "+strings.Join(cols, ", "))
	}
	if ob := orderByClause(w.OrderBy); ob != "" {
		over = append(over, "ORDER BY "+ob)
	}
	if w.Frame != "" {
		over = append(over, w.Frame)
	}
	expr := w.Fn + "(" + arg + ") OVER (" + strings.Join(over, " ") + ")"
	if w.Alias != "" {
		expr += " AS " + quoteIdent(w.Alias)
	}
	return expr
}

// selectList is the projection: aggregates + group-by columns when present,
// otherwise the plain column list plus any analytic window expressions.
func selectList(n *Node) []string {
	var out []string
	if len(n.Aggregates) > 0 || len(n.GroupBy) > 0 {
		for _, g := range n.GroupBy {
			out = append(out, quoteIdent(g))
		}
		for _, a := range n.Aggregates {
			out = append(out, renderAggregate(a))
		}
	} else {
		for _, c := range n.Columns {
			out = append(out, quoteIdent(c))
		}
		for _, w := range n.Windows {
			out = append(out, renderWindow(w))
		}
	}
	if len(out) == 0 {
		return []string{"1"}
	}
	return out
}

// BuildSelect emits the SELECT for one read node (plus optional extra predicates
// for a child IN-fetch) and the ordered positional binds.
func BuildSelect(n *Node, database string, extra []Filter) (string, []any, error) {
	var b strings.Builder
	args := []any{}

	b.WriteString("SELECT ")
	b.WriteString(strings.Join(selectList(n), ", "))
	b.WriteString(" FROM ")
	b.WriteString(qualified(database, n.Table))

	filters := n.Filters
	if len(extra) > 0 {
		filters = append(append([]Filter{}, n.Filters...), extra...)
	}
	var whereParts []string
	if len(filters) > 0 {
		where, err := renderFilters(filters, &args)
		if err != nil {
			return "", nil, err
		}
		if where != "" {
			whereParts = append(whereParts, where)
		}
	}
	if n.Keyset != nil {
		if seek, ok := buildSeekClause(n.Keyset, &args); ok {
			whereParts = append(whereParts, seek)
		}
	}
	if len(whereParts) > 0 {
		b.WriteString(" WHERE ")
		b.WriteString(strings.Join(whereParts, " AND "))
	}

	if len(n.GroupBy) > 0 {
		parts := make([]string, len(n.GroupBy))
		for i, g := range n.GroupBy {
			parts[i] = quoteIdent(g)
		}
		b.WriteString(" GROUP BY ")
		b.WriteString(strings.Join(parts, ", "))
	}

	if ob := orderByClause(n.OrderBy); ob != "" {
		b.WriteString(" ORDER BY ")
		b.WriteString(ob)
	}

	if n.Limit > 0 {
		fmt.Fprintf(&b, " LIMIT %d", n.Limit)
		off := n.Offset
		if n.resolvedOffset > 0 {
			off = n.resolvedOffset
		}
		if off > 0 {
			fmt.Fprintf(&b, " OFFSET %d", off)
		}
	}

	return b.String(), args, nil
}

func orderByClause(orders []OrderBy) string {
	if len(orders) == 0 {
		return ""
	}
	parts := make([]string, len(orders))
	for i, o := range orders {
		parts[i] = quoteIdent(o.Col) + " " + orderDir(o.Order)
	}
	return strings.Join(parts, ", ")
}

// BuildWindowedChildSelect emits a per-parent-limited child fetch: each parent
// (PARTITION BY the join column) keeps only its first n rows. Used for one-to-many
// children with a limit, where a plain LIMIT would cap the whole IN-chunk instead.
func BuildWindowedChildSelect(n *Node, database string, extra []Filter) (string, []any, error) {
	colList := strings.Join(selectList(n), ", ")
	args := []any{}

	filters := n.Filters
	if len(extra) > 0 {
		filters = append(append([]Filter{}, n.Filters...), extra...)
	}
	where, err := renderFilters(filters, &args)
	if err != nil {
		return "", nil, err
	}

	ob := orderByClause(n.OrderBy)
	var b strings.Builder
	b.WriteString("SELECT ")
	b.WriteString(colList)
	b.WriteString(" FROM (SELECT ")
	b.WriteString(colList)
	b.WriteString(", row_number() OVER (PARTITION BY ")
	b.WriteString(quoteIdent(n.Rel.ChildCol))
	if ob != "" {
		b.WriteString(" ORDER BY ")
		b.WriteString(ob)
	}
	b.WriteString(") AS `__gj_rn` FROM ")
	b.WriteString(qualified(database, n.Table))
	if where != "" {
		b.WriteString(" WHERE ")
		b.WriteString(where)
	}
	fmt.Fprintf(&b, ") AS __gj_w WHERE `__gj_rn` <= %d", n.Limit)
	if ob != "" {
		b.WriteString(" ORDER BY ")
		b.WriteString(ob)
	}
	return b.String(), args, nil
}

func renderFilters(filters []Filter, args *[]any) (string, error) {
	parts := make([]string, 0, len(filters))
	for _, f := range filters {
		s, err := renderFilter(f, args)
		if err != nil {
			return "", err
		}
		if s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " AND "), nil
}

func renderFilter(f Filter, args *[]any) (string, error) {
	if !f.isLeaf() {
		switch {
		case len(f.And) > 0:
			return renderJunction(f.And, " AND ", args)
		case len(f.Or) > 0:
			return renderJunction(f.Or, " OR ", args)
		case len(f.Not) > 0:
			inner, err := renderFilter(f.Not[0], args)
			if err != nil {
				return "", err
			}
			if inner == "" {
				return "", nil
			}
			return "(NOT " + inner + ")", nil
		default:
			return "", nil
		}
	}

	col := quoteIdent(f.Col)
	switch f.Op {
	case OpIsNull:
		return col + " IS NULL", nil
	case OpIsNotNull:
		return col + " IS NOT NULL", nil
	case OpIn, OpNin:
		vals := asSlice(f.Value)
		if len(vals) == 0 {
			if f.Op == OpIn {
				return "0", nil // empty IN matches nothing
			}
			return "1", nil
		}
		marks := make([]string, len(vals))
		for i, v := range vals {
			marks[i] = "?"
			*args = append(*args, v)
		}
		op := "IN"
		if f.Op == OpNin {
			op = "NOT IN"
		}
		return col + " " + op + " (" + strings.Join(marks, ", ") + ")", nil
	default:
		sym, ok := opSymbol(f.Op)
		if !ok {
			return "", fmt.Errorf("clickhousedriver: unsupported operator %q", f.Op)
		}
		*args = append(*args, f.Value)
		return col + " " + sym + " ?", nil
	}
}

// BuildInsert emits an INSERT for one row and its positional binds.
func BuildInsert(m *Mutation, database string) (string, []any, error) {
	if len(m.Set) == 0 {
		return "", nil, fmt.Errorf("clickhousedriver: insert has no columns")
	}
	cols := make([]string, len(m.Set))
	marks := make([]string, len(m.Set))
	args := make([]any, len(m.Set))
	for i, a := range m.Set {
		cols[i] = quoteIdent(a.Col)
		marks[i] = "?"
		args[i] = a.Value
	}
	sqlStr := "INSERT INTO " + qualified(database, m.Table) +
		" (" + strings.Join(cols, ", ") + ") VALUES (" + strings.Join(marks, ", ") + ")"
	return sqlStr, args, nil
}

// BuildUpdate emits a synchronous ALTER TABLE ... UPDATE (mutations_sync=1 so the
// write is visible to a read-after-write).
func BuildUpdate(m *Mutation, database string) (string, []any, error) {
	if len(m.Set) == 0 {
		return "", nil, fmt.Errorf("clickhousedriver: update has no SET assignments")
	}
	if len(m.Where) == 0 {
		return "", nil, fmt.Errorf("clickhousedriver: update requires a where filter")
	}
	var args []any
	sets := make([]string, len(m.Set))
	for i, a := range m.Set {
		sets[i] = quoteIdent(a.Col) + " = ?"
		args = append(args, a.Value)
	}
	where, err := renderFilters(m.Where, &args)
	if err != nil {
		return "", nil, err
	}
	sqlStr := "ALTER TABLE " + qualified(database, m.Table) + " UPDATE " +
		strings.Join(sets, ", ") + " WHERE " + where + " SETTINGS mutations_sync = 1"
	return sqlStr, args, nil
}

// BuildDelete emits a lightweight DELETE (applies to query results synchronously).
func BuildDelete(m *Mutation, database string) (string, []any, error) {
	if len(m.Where) == 0 {
		return "", nil, fmt.Errorf("clickhousedriver: delete requires a where filter")
	}
	var args []any
	where, err := renderFilters(m.Where, &args)
	if err != nil {
		return "", nil, err
	}
	return "DELETE FROM " + qualified(database, m.Table) + " WHERE " + where, args, nil
}

func renderJunction(fs []Filter, sep string, args *[]any) (string, error) {
	parts := make([]string, 0, len(fs))
	for _, f := range fs {
		s, err := renderFilter(f, args)
		if err != nil {
			return "", err
		}
		if s != "" {
			parts = append(parts, s)
		}
	}
	switch len(parts) {
	case 0:
		return "", nil
	case 1:
		return parts[0], nil
	default:
		return "(" + strings.Join(parts, sep) + ")", nil
	}
}
