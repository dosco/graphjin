package agent

import (
	"context"
	"strings"
)

// Label widening.
//
// "Which account renews soonest?" was answered "Account 1, on November 13":
// the worker selected { id renewal_date } and an id is all it could report.
// Measured in the lead eval, 3 of 9 runs that had to name an account gave its
// id instead, with the date right every time.
//
// So when a question asks which or who, a plain read of a table that has a
// label column (name, title, …) but did not select it runs with the label
// added. It is a read of one more scalar on rows the query already returns —
// the same rows, the same order, the same filter — so nothing the model asked
// for changes except that the answer can name what it found. Grouped and
// aggregate selections are left alone: adding a column there changes the
// grouping, and with it the numbers.

// labelColumns are the columns that name a row, in order of preference.
var labelColumns = []string{"name", "title", "full_name", "display_name", "label"}

// asksWhich reports whether an instruction asks to identify rows.
func asksWhich(instruction string) bool {
	padded := " " + normalizedWords(instruction) + " "
	for _, word := range []string{"which", "who", "whose", "whom"} {
		if strings.Contains(padded, " "+word+" ") {
			return true
		}
	}
	return false
}

// labelWidening returns query with each eligible root's label column added,
// and whether anything changed. A root with no label of its own that selects
// a foreign key gets the referenced row's label through the relation instead:
// "which account renews soonest" read from subscriptions selected account_id,
// and subscriptions has no name, so it now also selects account { name }.
// GraphJin names a many-to-one relation after its key column without _id.
func labelWidening(query, instruction string, labels map[string]string, relations ...map[string]map[string]string) (string, bool) {
	if len(labels) == 0 || !asksWhich(instruction) {
		return query, false
	}
	roots, ok := scanRootFields(query)
	if !ok {
		return query, false
	}
	var inserts []int
	var columns []string
	for _, root := range roots {
		if root.selStart < 0 {
			continue
		}
		if root.argsStart >= 0 && strings.Contains(query[root.argsStart:root.argsEnd], "distinct") {
			continue
		}
		selection := query[root.selStart:root.selEnd]
		scalars := selectionScalars(selection)
		if len(scalars) == 0 || selectsAggregate(scalars) {
			continue
		}
		if label := labels[root.name]; label != "" {
			if !hasString(scalars, label) {
				inserts = append(inserts, root.selStart)
				columns = append(columns, label)
			}
			continue
		}
		if len(relations) == 0 {
			continue
		}
		for _, field := range scalars {
			target := relations[0][root.name][field]
			label := labels[target]
			relation := strings.TrimSuffix(field, "_id")
			if target == "" || label == "" || relation == field || selectsField(selection, relation) {
				continue
			}
			inserts = append(inserts, root.selStart)
			columns = append(columns, relation+" { "+label+" }")
			break
		}
	}
	if len(inserts) == 0 {
		return query, false
	}
	out := query
	for i := len(inserts) - 1; i >= 0; i-- {
		out = out[:inserts[i]] + " " + columns[i] + out[inserts[i]:]
	}
	return out, true
}

func selectsAggregate(fields []string) bool {
	for _, field := range fields {
		for _, prefix := range []string{"count_", "sum_", "avg_", "min_", "max_", "stddev_", "variance_"} {
			if strings.HasPrefix(field, prefix) {
				return true
			}
		}
	}
	return false
}

func hasString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// recordTableColumns keeps the column names a table card lists. Its summary
// reads "7 columns; primary key id; columns: id, name, …" — or "key columns:"
// for a table too wide to list, where a label may be missing and then nothing
// is widened.
func (s *discoveryState) recordTableColumns(card map[string]any) {
	table := strings.ToLower(stringFromMap(card, "table_name"))
	_, listed, ok := strings.Cut(stringFromMap(card, "summary"), "columns: ")
	if table == "" || !ok {
		return
	}
	listed, _, _ = strings.Cut(listed, ";")
	if s.tableColumns[table] == nil {
		s.tableColumns[table] = map[string]bool{}
	}
	for _, column := range strings.Split(listed, ",") {
		if column = strings.TrimSpace(column); column != "" {
			s.tableColumns[table][column] = true
		}
	}
}

// labelColumnsByTable is, per table, the label column a table card showed it
// has.
func (s *discoveryState) labelColumnsByTable() map[string]string {
	out := map[string]string{}
	for table, columns := range s.tableColumns {
		for _, label := range labelColumns {
			if columns[label] {
				out[table] = label
				break
			}
		}
	}
	return out
}

// selectsField reports whether a selection already names field at its top
// level, as a scalar or a nested object.
func selectsField(selection, field string) bool {
	sc := gqlScanner{src: selection}
	for {
		sc.skipIgnored()
		if sc.pos >= len(sc.src) || !isNameStart(sc.peek()) {
			return false
		}
		name := sc.readName()
		sc.skipIgnored()
		if sc.peek() == ':' {
			sc.pos++
			sc.skipIgnored()
			name = sc.readName()
			sc.skipIgnored()
		}
		if name == field {
			return true
		}
		if sc.peek() == '(' {
			if _, ok := sc.skipBalanced('(', ')'); !ok {
				return false
			}
			sc.skipIgnored()
		}
		if sc.peek() == '{' {
			if _, ok := sc.skipBalanced('{', '}'); !ok {
				return false
			}
		}
	}
}

// recordRelationship keeps one foreign key from a relationship card or a
// reference edge id: relationship:app:main.subscriptions.account_id->app:main.accounts.id.
func (s *discoveryState) recordRelationship(id string) {
	_, rest, ok := strings.Cut(id, "relationship:")
	if !ok {
		return
	}
	from, to, ok := strings.Cut(rest, "->")
	if !ok {
		return
	}
	fromTable, fromColumn := lastTwoSegments(from)
	toTable, _ := lastTwoSegments(to)
	if fromTable == "" || fromColumn == "" || toTable == "" {
		return
	}
	if s.relations[fromTable] == nil {
		s.relations[fromTable] = map[string]string{}
	}
	s.relations[fromTable][fromColumn] = toTable
}

// lastTwoSegments splits "app:main.accounts.id" into ("accounts", "id").
func lastTwoSegments(ref string) (string, string) {
	parts := strings.Split(ref, ".")
	if len(parts) < 2 {
		return "", ""
	}
	return strings.ToLower(parts[len(parts)-2]), strings.ToLower(parts[len(parts)-1])
}

// lookupLabelRelations fetches, without a model call, the foreign keys of each
// root a which-question reads that has no label column of its own but selects
// a key — and the referenced table's columns when no card listed them. A table
// detail lists only the table's own columns: its foreign keys are column-level
// reference edges, which no table lookup returns, so a run that inspected
// subscriptions still could not tell that account_id names an account.
func (r *protocolRuntime) lookupLabelRelations(ctx context.Context, query string) {
	if !asksWhich(r.state.instruction) {
		return
	}
	roots, ok := scanRootFields(query)
	if !ok {
		return
	}
	labels := r.state.labelColumnsByTable()
	for _, root := range roots {
		table := strings.ToLower(root.name)
		if root.selStart < 0 || labels[table] != "" || r.state.relationsLooked[table] || strings.HasPrefix(table, "gj_") {
			continue
		}
		keys := map[string]bool{}
		for _, field := range selectionScalars(query[root.selStart:root.selEnd]) {
			if strings.HasSuffix(field, "_id") {
				keys[field] = true
			}
		}
		if len(keys) == 0 {
			continue
		}
		r.state.relationsLooked[table] = true
		args := map[string]any{"kind": "relationship", "table": root.name, "limit": 20}
		r.addNamespace(args)
		out, err := r.base.QueryCatalog(ctx, args)
		if err != nil {
			continue
		}
		var targets []string
		for _, card := range catalogCards(normalizeValue(out)) {
			id := stringFromMap(card, "id")
			r.state.recordRelationship(id)
			if from, to, ok := strings.Cut(strings.TrimPrefix(id, "relationship:"), "->"); ok {
				if _, column := lastTwoSegments(from); keys[column] {
					if cut := strings.LastIndex(to, "."); cut > 0 {
						targets = append(targets, "table:"+to[:cut])
					}
				}
			}
		}
		var missing []string
		for _, target := range targets {
			name, _ := lastTwoSegments(strings.TrimPrefix(target, "table:") + ".x")
			if labels[name] == "" && r.state.tableColumns[name] == nil {
				missing = append(missing, target)
			}
		}
		if len(missing) == 0 {
			continue
		}
		ids := make([]any, len(missing))
		for i, id := range missing {
			ids[i] = id
		}
		detailArgs := map[string]any{"ids": ids}
		r.addNamespace(detailArgs)
		if detail, err := r.base.QueryCatalog(ctx, detailArgs); err == nil {
			for _, card := range catalogCards(normalizeValue(detail)) {
				if strings.EqualFold(stringFromMap(card, "kind"), "table") {
					r.state.recordTableColumns(card)
				}
			}
		}
		labels = r.state.labelColumnsByTable()
	}
}
