package agent

import (
	"regexp"
	"strings"
)

var windowedArgsPattern = regexp.MustCompile(`\b(limit|offset|order_by|distinct|first|last)\s*:`)

// Aggregate rewrites.
//
// When a request asks for a total and the run has only read rows, the
// database_computation_required guard refuses to finalize. Measured on the
// saas-ops demo, a worker asked for "the combined MRR of Meridian Robotics and
// Quartzline Analytics" read the two rows, was refused, read the same rows
// again, and summed them in JavaScript until the refusal cap waived the guard
// — twice it ran out the lead's two-minute budget instead. The refusal said to
// "execute that aggregate" without saying what that query is, and the model
// did not know a filtered root can select sum_<column> directly.
//
// So the refusal now carries the rewrite: the last row query, same root and
// same arguments, with its numeric selection replaced by the aggregate the
// request asks for. It is built only from columns the catalog seed typed as
// numeric, so it never proposes sum_ on a text column.

// aggregateOperation is the aggregate an instruction asks for, or "" when it
// asks for something a rewrite cannot express (an extreme or a ranking).
func aggregateOperation(instruction string) string {
	padded := " " + normalizedWords(instruction) + " "
	has := func(phrases ...string) bool {
		for _, phrase := range phrases {
			if strings.Contains(padded, " "+phrase+" ") {
				return true
			}
		}
		return false
	}
	switch {
	case has("average", "avg", "mean"):
		return "avg"
	case has("how many", "count", "count of", "number of"):
		return "count"
	case has(sumIntentPhrases...):
		return "sum"
	}
	return ""
}

// sumIntentPhrases ask for a database total. "combined" is how the lead
// restates "their combined MRR" for a worker, and was the word that let a
// client-side sum through the guard.
var sumIntentPhrases = []string{"total", "sum", "combined", "altogether", "add up", "adds up", "added up", "summed"}

func normalizedWords(text string) string {
	return strings.Join(strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	}), " ")
}

// aggregateRewrite returns the aggregate form of the run's most recent
// successful row query, or "" when there is nothing safe to propose.
func (s *discoveryState) aggregateRewrite() string {
	op := aggregateOperation(s.instruction)
	if op == "" {
		return ""
	}
	numeric := seedNumericColumns(s.seedResult)
	for i := len(s.executions) - 1; i >= 0; i-- {
		execution := s.executions[i]
		if execution["has_data"] != true || executionErrorCount(execution["error_count"]) != 0 ||
			!strings.EqualFold(stringFromMap(execution, "tool"), toolExecuteGraphQL) {
			continue
		}
		if rewrite := aggregateRewriteOf(stringFromMap(execution, "query"), op, numeric); rewrite != "" {
			return rewrite
		}
	}
	return ""
}

// aggregateRewriteOf rewrites one query. Every root must be rewritable, or the
// proposal would silently drop part of what was read.
func aggregateRewriteOf(query, op string, numeric map[string]map[string]bool) string {
	roots, ok := scanRootFields(query)
	if !ok || len(roots) == 0 {
		return ""
	}
	var parts []string
	for _, root := range roots {
		if root.selStart < 0 || strings.HasPrefix(strings.ToLower(root.name), "gj_") {
			return ""
		}
		// Only a plain filter carries over: what an aggregate means under a
		// limit, an order, an offset or distinct is not this rewrite's to guess.
		if root.argsStart >= 0 && windowedArgsPattern.MatchString(query[root.argsStart:root.argsEnd]) {
			return ""
		}
		var fields []string
		switch op {
		case "count":
			fields = []string{"count_id"}
		default:
			for _, field := range selectionScalars(query[root.selStart:root.selEnd]) {
				if numeric[root.name][field] && field != "id" && !strings.HasSuffix(field, "_id") {
					fields = append(fields, op+"_"+field)
				}
			}
		}
		if len(fields) == 0 {
			return ""
		}
		part := root.name
		if root.argsStart >= 0 {
			if args := strings.TrimSpace(query[root.argsStart:root.argsEnd]); args != "" {
				part += "(" + args + ")"
			}
		}
		parts = append(parts, part+" { "+strings.Join(fields, " ")+" }")
	}
	return "query { " + strings.Join(parts, " ") + " }"
}

// selectionScalars lists the plain fields at the top level of a selection,
// skipping nested objects, fields with arguments, and aliased fields.
func selectionScalars(selection string) []string {
	s := gqlScanner{src: selection}
	var out []string
	for {
		s.skipIgnored()
		if s.pos >= len(s.src) || !isNameStart(s.peek()) {
			return out
		}
		name := s.readName()
		s.skipIgnored()
		plain := true
		if s.peek() == ':' { // alias: the selected field is the next name
			s.pos++
			s.skipIgnored()
			s.readName()
			s.skipIgnored()
			plain = false
		}
		if s.peek() == '(' {
			if _, ok := s.skipBalanced('(', ')'); !ok {
				return out
			}
			s.skipIgnored()
			plain = false
		}
		if s.peek() == '{' {
			if _, ok := s.skipBalanced('{', '}'); !ok {
				return out
			}
			plain = false
		}
		if plain {
			out = append(out, name)
		}
	}
}

// seedNumericColumns reads, per table name, the columns the catalog seed
// typed as numeric.
func seedNumericColumns(seed any) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, raw := range seedDigest(seed) {
		entry := mapValue(raw)
		name := stringFromMap(entry, "name")
		if stringFromMap(entry, "kind") != "table" || name == "" {
			continue
		}
		columns, _ := entry["columns"].([]string)
		for _, text := range columns {
			col, typ, ok := strings.Cut(text, ":")
			if ok && numericColumnType(typ) {
				if out[name] == nil {
					out[name] = map[string]bool{}
				}
				out[name][col] = true
			}
		}
	}
	return out
}

func numericColumnType(typ string) bool {
	typ = strings.ToLower(strings.TrimSpace(typ))
	for _, prefix := range []string{"int", "bigint", "smallint", "tinyint", "numeric", "decimal", "real", "double", "float", "money", "number"} {
		if strings.HasPrefix(typ, prefix) {
			return true
		}
	}
	return false
}
