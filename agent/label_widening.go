package agent

import "strings"

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
// and whether anything changed.
func labelWidening(query, instruction string, labels map[string]string) (string, bool) {
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
		label := labels[root.name]
		if label == "" || root.selStart < 0 {
			continue
		}
		if root.argsStart >= 0 && strings.Contains(query[root.argsStart:root.argsEnd], "distinct") {
			continue
		}
		scalars := selectionScalars(query[root.selStart:root.selEnd])
		if len(scalars) == 0 || hasString(scalars, label) || selectsAggregate(scalars) {
			continue
		}
		inserts = append(inserts, root.selStart)
		columns = append(columns, label)
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
