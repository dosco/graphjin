package qcode

import (
	"fmt"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/graph"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
	"github.com/dosco/graphjin/core/v3/internal/util"
)

func (co *Compiler) compileArgOrderByObj(sel *Select, parent *graph.Node, cm map[string]struct{}) error {
	st := util.NewStackInf()

	for i := range parent.Children {
		st.Push(parent.Children[i])
	}

	var obList []OrderBy

	var node *graph.Node
	var ok bool
	var err error

	for {
		if err != nil {
			return fmt.Errorf("argument '%s', %w", node.Name, err)
		}

		if st.Len() == 0 {
			break
		}

		intf := st.Pop()
		node, ok = intf.(*graph.Node)
		if !ok {
			err = fmt.Errorf("unexpected value '%v' (%t)", intf, intf)
			continue
		}

		// Check for type
		if node.Type != graph.NodeStr &&
			node.Type != graph.NodeObj &&
			node.Type != graph.NodeList &&
			node.Type != graph.NodeLabel {
			err = fmt.Errorf("expecting a string, object or list")
			continue
		}

		var ob OrderBy
		ti := sel.Ti
		cn := node

		switch node.Type {
		case graph.NodeStr, graph.NodeLabel:
			if ob.Order, err = toOrder(node.Val); err != nil { // sets the asc desc etc
				continue
			}

		case graph.NodeList:
			if ob, err = orderByFromList(node); err != nil {
				continue
			}

		case graph.NodeObj:
			var path []sdata.TPath
			if path, err = co.FindPath(node.Name, sel.Ti.Name, ""); err != nil {
				continue
			}
			ti = path[0].LT

			cn = node.Children[0]
			if ob.Order, err = toOrder(cn.Val); err != nil { // sets the asc desc etc
				continue
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
		}

		if err = co.setOrderByColName(ti, &ob, cn); err != nil {
			continue
		}

		// Dedup key: use alias when ordering by a SELECT-list alias
		// (Col.Name would be empty in that case).
		dedupKey := ob.Col.Name
		if ob.Alias != "" {
			dedupKey = "alias:" + ob.Alias
		}
		if _, ok := cm[dedupKey]; ok {
			err = fmt.Errorf("can only be defined once")
			continue
		}
		cm[dedupKey] = struct{}{}
		obList = append(obList, ob)
	}

	for i := len(obList) - 1; i >= 0; i-- {
		sel.OrderBy = append(sel.OrderBy, obList[i])
	}

	return err
}

func orderByFromList(parent *graph.Node) (ob OrderBy, err error) {
	if len(parent.Children) != 2 {
		return ob, fmt.Errorf(`valid format is [values, order] (eg. [$list, "desc"])`)
	}

	valNode := parent.Children[0]
	orderNode := parent.Children[1]

	ob.Var = valNode.Val

	if ob.Order, err = toOrder(orderNode.Val); err != nil {
		return ob, err
	}
	return ob, nil
}

func (co *Compiler) compileArgOrderByVar(sel *Select, node *graph.Node, cm map[string]struct{}) (err error) {
	for k, v := range sel.tc.OrderBy {
		if err = compileOrderBy(sel, node.Val, k, v, cm); err != nil {
			return
		}
	}
	return
}

func (co *Compiler) setOrderByColName(ti sdata.DBTable, ob *OrderBy, node *graph.Node) (err error) {
	name := co.ParseName(node.Name)
	col, err := ti.GetColumn(name)
	if err == nil {
		ob.Col = col
		return nil
	}
	// Check if it's an aggregation function prefix (e.g., sum_orderqty → SUM(orderqty))
	for k, v := range co.s.GetFunctions() {
		if strings.HasPrefix(name, k+"_") {
			col, err = ti.GetColumn(name[len(k)+1:])
			if err != nil {
				return err
			}
			ob.Col = col
			ob.Func = v
			ob.IsFunc = true
			return nil
		}
	}
	// Last resort: treat as a SELECT-list alias. Common for expression
	// aggregates like `order_by: { revenue: desc }` where `revenue` is
	// the alias of a sum(expr: ...) field. The column list isn't
	// populated yet at this point in compilation (compileFields runs
	// later), so the actual alias-resolution check is deferred to
	// validateOrderByAliases.
	ob.Alias = node.Name
	return nil
}

func compileOrderBy(sel *Select,
	keyVar, key string,
	values [][2]string,
	cm map[string]struct{},
) error {
	obList := make([]OrderBy, 0, len(values))

	for _, v := range values {
		ob := OrderBy{KeyVar: keyVar, Key: key}
		ob.Order, _ = toOrder(v[1])

		col, err := sel.Ti.GetColumn(v[0])
		if err != nil {
			return err
		}
		ob.Col = col
		if _, ok := cm[ob.Col.Name]; ok {
			return fmt.Errorf("duplicate column '%s'", ob.Col.Name)
		}
		obList = append(obList, ob)
	}
	sel.OrderBy = append(sel.OrderBy, obList...)
	return nil
}

// validateOrderBy enforces the same authorization on ORDER BY and
// DISTINCT ON columns that SELECT-list fields get in validateField and
// compileChildColumns. Without it a role restricted to a column
// allowlist (or a config-level blocked column) could still observe a
// column's values through sorted or deduplicated output — value
// ordering is a side channel.
//
// It runs after compileSelectArgs/compileFields and before the cursor
// block appends system entries (PK tie-breakers, clustering keys), so
// only user-driven entries are checked:
//   - compileArgOrderByObj: order_by argument (plain columns, aggregate
//     sum_<col> forms, nested-relation objects, [values, order] lists)
//   - compileArgDistinctOn: distinct argument (MySQL emulates DISTINCT ON
//     with ORDER BY entries; other dialects populate sel.DistinctOn)
//
// Two kinds of entries are exempt from the role checks:
//   - Alias entries: validateOrderByAliases ties them to a compiled
//     SELECT-list field, which validateField has already authorized.
//   - Named variants from the table config (KeyVar != ""): these are
//     dev-authored like presets, and every variant compiles into the SQL
//     regardless of which one the client's variable picks at runtime, so
//     a per-role allowlist could only ever reject the whole feature.
//     The config-level Blocked flag still applies — a column marked
//     "never expose" wins over a config ordering variant that names it.
func (co *Compiler) validateOrderBy(qc *QCode, sel *Select, tr trval) error {
	for _, ob := range sel.OrderBy {
		if ob.Alias != "" {
			continue
		}
		if ob.Col.Blocked {
			return fmt.Errorf("order_by: column: '%s.%s.%s' blocked",
				ob.Col.Schema, ob.Col.Table, ob.Col.Name)
		}
		if ob.KeyVar != "" {
			continue
		}
		trv := tr
		if !strings.EqualFold(ob.Col.Table, sel.Ti.Name) ||
			!strings.EqualFold(ob.Col.Schema, sel.Ti.Schema) {
			// Nested-relation ordering (order_by: { rel: { col: asc } })
			// references another table's column; check that table's role
			// config, same as a child selector on it would.
			trv = co.getRole(tr.role, ob.Col.Schema, ob.Col.Table, ob.Col.Table)
		}
		if ob.IsFunc {
			// Same message isFunction emits for aggregate SELECT fields
			// when aggregation is disabled compiler-wide.
			if co.c.DisableAgg && ob.Func.Agg {
				return fmt.Errorf("order_by: aggreation disabled: db function '%s' cannot be used",
					ob.Func.Name)
			}
			if trv.isFuncsBlocked() {
				return fmt.Errorf("order_by: %w",
					validateErr(trv, ob.Func.Name, "all db functions blocked"))
			}
		}
		if !trv.columnAllowed(qc, ob.Col.Name) {
			return fmt.Errorf("order_by: %w",
				validateErr(trv, ob.Col.Name, "db column blocked"))
		}
	}

	for _, col := range sel.DistinctOn {
		if col.Blocked {
			return fmt.Errorf("distinct: column: '%s.%s.%s' blocked",
				col.Schema, col.Table, col.Name)
		}
		if !tr.columnAllowed(qc, col.Name) {
			return fmt.Errorf("distinct: %w",
				validateErr(tr, col.Name, "db column blocked"))
		}
	}
	return nil
}

// validateOrderByAliases resolves deferred alias-based ORDER BY entries
// after compileFields has populated sel.Fields. Each OrderBy with a
// non-empty Alias must match a compiled field's FieldName; unmatched
// aliases produce a compile-time error so the user sees the problem
// up-front rather than getting a DB-side "column not found".
func validateOrderByAliases(sel *Select) error {
	for i := range sel.OrderBy {
		ob := &sel.OrderBy[i]
		if ob.Alias == "" {
			continue
		}
		found := false
		for _, f := range sel.Fields {
			if f.FieldName == ob.Alias {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("order_by: alias %q is not a column or a field alias on %q",
				ob.Alias, sel.Ti.Name)
		}
	}
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
