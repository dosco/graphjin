package qcode

import (
	"strings"
)

// unionRoleSeparator joins the component roles of a union role key. It must
// match core.UnionRoleSeparator.
const unionRoleSeparator = "+"

// Union roles merge the table rules of several roles. The merged rule never
// allows a row, a column or an operation that no single component allows:
//
//   - reads may join row filters with OR only when every component exposes
//     the same columns, or join columns only when every component has the same
//     row filter; otherwise the columns are intersected
//   - writes never widen columns, because a written row must be allowed by one
//     component as a whole, and components with different presets block the
//     operation
//
// A component with no rule for a table is unrestricted for that table, which
// matches how a single role without a table rule behaves.

type unionOp struct {
	fil     *Exp
	filNU   bool
	filKey  string
	cols    map[string]struct{}
	presets map[string]string
	limit   int32
	funcs   bool
}

func (co *Compiler) getUnionRole(role, schema, table, field string) trval {
	key := role + ":" + schema + ":" + table + ":" + field
	if cached, ok := co.union.Load(key); ok {
		return cached.(trval)
	}

	components := strings.Split(role, unionRoleSeparator)
	trvs := make([]trval, 0, len(components))
	for _, component := range components {
		trvs = append(trvs, co.getRole(component, schema, table, field))
	}

	merged := mergeRoleRules(trvs)
	merged.role = role
	merged.set = true

	actual, _ := co.union.LoadOrStore(key, merged)
	return actual.(trval)
}

func mergeRoleRules(trvs []trval) trval {
	var out trval

	queryOps := make([]unionOp, 0, len(trvs))
	insertOps := make([]unionOp, 0, len(trvs))
	updateOps := make([]unionOp, 0, len(trvs))
	upsertOps := make([]unionOp, 0, len(trvs))
	deleteOps := make([]unionOp, 0, len(trvs))

	for _, t := range trvs {
		if !t.query.block {
			queryOps = append(queryOps, unionOp{
				fil: t.query.fil, filNU: t.query.filNU, filKey: t.query.filKey,
				cols: t.query.cols, limit: t.query.limit, funcs: t.query.disable.funcs,
			})
		}
		if !t.insert.block {
			insertOps = append(insertOps, unionOp{cols: t.insert.cols, presets: t.insert.presets})
		}
		if !t.update.block {
			updateOps = append(updateOps, unionOp{
				fil: t.update.fil, filNU: t.update.filNU, filKey: t.update.filKey,
				cols: t.update.cols, presets: t.update.presets,
			})
		}
		if !t.upsert.block {
			upsertOps = append(upsertOps, unionOp{
				fil: t.upsert.fil, filNU: t.upsert.filNU, filKey: t.upsert.filKey,
				cols: t.upsert.cols, presets: t.upsert.presets,
			})
		}
		if !t.delete.block {
			deleteOps = append(deleteOps, unionOp{
				fil: t.delete.fil, filNU: t.delete.filNU, filKey: t.delete.filKey,
				cols: t.delete.cols,
			})
		}
	}

	if q, ok := mergeReadOps(queryOps); ok {
		out.query.fil, out.query.filNU, out.query.filKey = q.fil, q.filNU, q.filKey
		out.query.cols, out.query.limit, out.query.disable.funcs = q.cols, q.limit, q.funcs
	} else {
		out.query.block = true
	}
	if w, ok := mergeWriteOps(insertOps); ok {
		out.insert.cols, out.insert.presets = w.cols, w.presets
	} else {
		out.insert.block = true
	}
	if w, ok := mergeWriteOps(updateOps); ok {
		out.update.fil, out.update.filNU, out.update.filKey = w.fil, w.filNU, w.filKey
		out.update.cols, out.update.presets = w.cols, w.presets
	} else {
		out.update.block = true
	}
	if w, ok := mergeWriteOps(upsertOps); ok {
		out.upsert.fil, out.upsert.filNU, out.upsert.filKey = w.fil, w.filNU, w.filKey
		out.upsert.cols, out.upsert.presets = w.cols, w.presets
	} else {
		out.upsert.block = true
	}
	if w, ok := mergeWriteOps(deleteOps); ok {
		out.delete.fil, out.delete.filNU, out.delete.filKey = w.fil, w.filNU, w.filKey
		out.delete.cols = w.cols
	} else {
		out.delete.block = true
	}

	return out
}

// mergeReadOps merges the query rules of the components that do not block
// the read. It returns false when the merged read must be blocked.
func mergeReadOps(ops []unionOp) (unionOp, bool) {
	ops, allFalse := dropFalseFilters(ops)
	if allFalse != nil {
		return *allFalse, true
	}
	if len(ops) == 0 {
		return unionOp{}, false
	}
	if len(ops) == 1 {
		return ops[0], true
	}
	if d, ok := dominantOp(ops); ok {
		return d, true
	}

	out := unionOp{limit: mergedLimit(ops), funcs: anyFuncsDisabled(ops)}
	switch {
	case allColumnsEqual(ops):
		out.cols = ops[0].cols
		out.fil, out.filNU, out.filKey = orFilters(ops)
	case allFilterKeysEqual(ops):
		out.fil, out.filNU, out.filKey = ops[0].fil, anyFilterNeedsUser(ops), ops[0].filKey
		out.cols = unionColumns(ops)
	default:
		cols, empty := intersectColumns(ops)
		if empty {
			return unionOp{}, false
		}
		out.cols = cols
		out.fil, out.filNU, out.filKey = orFilters(ops)
	}
	return out, true
}

// mergeWriteOps merges insert, update, upsert or delete rules. Writes never
// take a union of columns. It returns false when the write must be blocked.
func mergeWriteOps(ops []unionOp) (unionOp, bool) {
	ops, allFalse := dropFalseFilters(ops)
	if allFalse != nil {
		return *allFalse, true
	}
	if len(ops) == 0 {
		return unionOp{}, false
	}
	for _, op := range ops[1:] {
		if !presetsEqual(ops[0].presets, op.presets) {
			return unionOp{}, false
		}
	}
	if len(ops) == 1 {
		return ops[0], true
	}
	if d, ok := dominantOp(ops); ok {
		return d, true
	}

	cols, empty := intersectColumns(ops)
	if empty {
		return unionOp{}, false
	}
	out := unionOp{cols: cols, presets: ops[0].presets}
	out.fil, out.filNU, out.filKey = orFilters(ops)
	return out, true
}

// dropFalseFilters removes components whose filter matches no rows. When every
// component matches no rows, the first one is returned so the operation keeps
// its current empty-result behaviour instead of becoming an error.
func dropFalseFilters(ops []unionOp) ([]unionOp, *unionOp) {
	kept := ops[:0:0]
	var firstFalse *unionOp
	for i := range ops {
		if ops[i].fil != nil && ops[i].fil.Op == OpFalse {
			if firstFalse == nil {
				op := ops[i]
				firstFalse = &op
			}
			continue
		}
		kept = append(kept, ops[i])
	}
	if len(kept) == 0 && firstFalse != nil {
		return nil, firstFalse
	}
	return kept, nil
}

func unfiltered(op unionOp) bool {
	return op.fil == nil || op.fil.Op == OpNop
}

// dominantOp finds a component with no row filter whose columns cover every
// other component. That component alone already allows everything the others
// allow.
func dominantOp(ops []unionOp) (unionOp, bool) {
	for i, candidate := range ops {
		if !unfiltered(candidate) {
			continue
		}
		covers := true
		for j, other := range ops {
			if i != j && !columnsCover(candidate.cols, other.cols) {
				covers = false
				break
			}
		}
		if covers {
			return candidate, true
		}
	}
	return unionOp{}, false
}

// columnsCover reports whether column set a includes column set b. An empty
// set means every column.
func columnsCover(a, b map[string]struct{}) bool {
	if len(a) == 0 {
		return true
	}
	if len(b) == 0 {
		return false
	}
	for col := range b {
		if _, ok := a[col]; !ok {
			return false
		}
	}
	return true
}

func allColumnsEqual(ops []unionOp) bool {
	for _, op := range ops[1:] {
		if !columnsCover(ops[0].cols, op.cols) || !columnsCover(op.cols, ops[0].cols) {
			return false
		}
	}
	return true
}

func allFilterKeysEqual(ops []unionOp) bool {
	for _, op := range ops[1:] {
		if op.filKey != ops[0].filKey {
			return false
		}
	}
	return true
}

func unionColumns(ops []unionOp) map[string]struct{} {
	out := make(map[string]struct{})
	for _, op := range ops {
		if len(op.cols) == 0 {
			return nil
		}
		for col := range op.cols {
			out[col] = struct{}{}
		}
	}
	return out
}

// intersectColumns returns the columns every component allows. The second
// result is true when the intersection is empty, which must block the
// operation because an empty set would otherwise mean every column.
func intersectColumns(ops []unionOp) (map[string]struct{}, bool) {
	var out map[string]struct{}
	for _, op := range ops {
		if len(op.cols) == 0 {
			continue
		}
		if out == nil {
			out = make(map[string]struct{}, len(op.cols))
			for col := range op.cols {
				out[col] = struct{}{}
			}
			continue
		}
		for col := range out {
			if _, ok := op.cols[col]; !ok {
				delete(out, col)
			}
		}
	}
	if out == nil {
		return nil, false
	}
	return out, len(out) == 0
}

// orFilters joins component row filters with OR. A component without a
// filter allows every row, so the result is unfiltered.
func orFilters(ops []unionOp) (*Exp, bool, string) {
	children := make([]*Exp, 0, len(ops))
	keys := make([]string, 0, len(ops))
	needsUser := false
	for _, op := range ops {
		if unfiltered(op) {
			return newExp(), false, ""
		}
		children = append(children, op.fil)
		keys = append(keys, op.filKey)
		needsUser = needsUser || op.filNU
	}
	if len(children) == 1 {
		return children[0], needsUser, keys[0]
	}
	ex := newExpOp(OpOr)
	ex.Children = children
	return ex, needsUser, "or(" + strings.Join(keys, "\x1f") + ")"
}

func anyFilterNeedsUser(ops []unionOp) bool {
	for _, op := range ops {
		if op.filNU {
			return true
		}
	}
	return false
}

func mergedLimit(ops []unionOp) int32 {
	var max int32
	for _, op := range ops {
		if op.limit == 0 {
			return 0
		}
		if op.limit > max {
			max = op.limit
		}
	}
	return max
}

func anyFuncsDisabled(ops []unionOp) bool {
	for _, op := range ops {
		if op.funcs {
			return true
		}
	}
	return false
}

func presetsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
}
