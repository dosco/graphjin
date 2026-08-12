package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Asked to close a ticket that had been "sorted out", the agent writes
// status: "closed". The schema's statuses are open, pending, and resolved. The
// column is plain text with no constraint, so the write lands, the ticket ends up
// in a state nothing else recognises, and the failure only shows up when something
// later asks for open work.
//
// Sampled values have been in the catalog since the enum sampler landed, and they
// do not help here: measured over 30 action episodes, 26 never fetched the status
// column's card and 26 never fetched the table's either. The model writes without
// inspecting, and 19 of the 30 writes went through.
//
// So the values have to arrive on the path the write actually takes. The protocol
// layer can read the catalog itself, which makes this checkable at the moment it
// matters instead of hoping the model looked first.
//
// Observed values are evidence, not schema: the sampler publishes them only for a
// small closed set, but a column can legitimately accept a value absent from the
// data. So this interrupts once with the facts and then lets the same write
// through, rather than deciding the value is wrong.

const (
	observedValueMismatchLimit = 4
	// observedValueCardLimit bounds one table's column cards; wide tables exist and
	// the lookup must stay a single bounded read.
	observedValueCardLimit = 60
)

// columnAssignment is one column set by a mutation to a literal string.
type columnAssignment struct {
	Table  string
	Column string
	Value  string
}

// mutationStringAssignments returns the string-literal column assignments in a
// mutation's update or insert input, keyed by the root field being written.
func mutationStringAssignments(query string) []columnAssignment {
	if !ContainsMutationOperation(query) {
		return nil
	}
	clean := graphQLStructure(query)
	var out []columnAssignment
	for _, root := range MutationRootFields(query) {
		if strings.HasPrefix(strings.ToLower(root), "gj_") {
			continue
		}
		for _, keyword := range []string{"update", "insert"} {
			// clean blanks string literals but preserves every offset, so it is safe
			// for delimiter matching while the values are read from the raw query.
			for _, span := range mutationInputBlocks(clean, root, keyword) {
				for column, value := range graphQLStringFields(query[span[0]:span[1]]) {
					out = append(out, columnAssignment{Table: root, Column: column, Value: value})
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Table != out[j].Table {
			return out[i].Table < out[j].Table
		}
		return out[i].Column < out[j].Column
	})
	return out
}

// mutationInputBlocks returns the offset spans of `<keyword>: { ... }` argument
// bodies on a root field. Spans rather than substrings, because the caller reads
// the values from the unblanked query.
func mutationInputBlocks(clean, root, keyword string) [][2]int {
	var out [][2]int
	lower := strings.ToLower(clean)
	target := strings.ToLower(root)
	for start := 0; start < len(clean); {
		index := strings.Index(lower[start:], target)
		if index < 0 {
			break
		}
		index += start
		end := index + len(target)
		if (index > 0 && isGraphQLNameContinue(clean[index-1])) || (end < len(clean) && isGraphQLNameContinue(clean[end])) {
			start = end
			continue
		}
		open := skipGraphQLSpace(clean, end)
		if open >= len(clean) || clean[open] != '(' {
			start = end
			continue
		}
		close := matchingGraphQLDelimiter(clean, open, '(', ')')
		if close <= open {
			start = end
			continue
		}
		if from, to, ok := graphQLNamedObject(clean, keyword, open+1, close); ok {
			out = append(out, [2]int{from, to})
		}
		start = close
	}
	return out
}

// graphQLNamedObject finds `name: { ... }` within [from, to) and returns the body's
// span. Offsets are absolute so the caller can read the same range from the raw
// query.
func graphQLNamedObject(clean, name string, from, to int) (int, int, bool) {
	if from < 0 || to > len(clean) || from >= to {
		return 0, 0, false
	}
	lower := strings.ToLower(clean)
	target := strings.ToLower(name)
	for start := from; start < to; {
		index := strings.Index(lower[start:to], target)
		if index < 0 {
			return 0, 0, false
		}
		index += start
		end := index + len(target)
		if (index > 0 && isGraphQLNameContinue(clean[index-1])) || (end < len(clean) && isGraphQLNameContinue(clean[end])) {
			start = end
			continue
		}
		colon := skipGraphQLSpace(clean, end)
		if colon >= to || clean[colon] != ':' {
			start = end
			continue
		}
		open := skipGraphQLSpace(clean, colon+1)
		if open >= to || clean[open] != '{' {
			start = end
			continue
		}
		close := matchingGraphQLDelimiter(clean, open, '{', '}')
		if close <= open {
			return 0, 0, false
		}
		return open + 1, close, true
	}
	return 0, 0, false
}

// graphQLStringFields returns the `name: "value"` pairs directly in an input
// object body, skipping nested objects so a where clause inside an insert does not
// contribute assignments.
func graphQLStringFields(body string) map[string]string {
	out := map[string]string{}
	for i := 0; i < len(body); {
		switch {
		case body[i] == '{' || body[i] == '[':
			closing := byte('}')
			opening := byte('{')
			if body[i] == '[' {
				closing, opening = ']', '['
			}
			end := matchingGraphQLDelimiter(body, i, opening, closing)
			if end <= i {
				return out
			}
			i = end + 1
		case isGraphQLNameStart(body[i]):
			name := i
			for i < len(body) && isGraphQLNameContinue(body[i]) {
				i++
			}
			field := body[name:i]
			colon := skipGraphQLSpace(body, i)
			if colon >= len(body) || body[colon] != ':' {
				continue
			}
			value := skipGraphQLSpace(body, colon+1)
			if value >= len(body) || body[value] != '"' {
				i = colon + 1
				continue
			}
			text, end := graphQLStringLiteral(body, value)
			if end <= value {
				return out
			}
			out[field] = text
			i = end
		default:
			i++
		}
	}
	return out
}

// graphQLStringLiteral reads a quoted value starting at open, returning its
// unescaped text and the index just past the closing quote.
func graphQLStringLiteral(body string, open int) (string, int) {
	var text strings.Builder
	for i := open + 1; i < len(body); i++ {
		switch body[i] {
		case '\\':
			if i+1 < len(body) {
				text.WriteByte(body[i+1])
				i++
			}
		case '"':
			return text.String(), i + 1
		default:
			text.WriteByte(body[i])
		}
	}
	return "", open
}

// observedColumnValues reads the sampled value sets for a table's columns straight
// from the catalog. The model may never have asked for these cards — that is the
// whole reason this exists — so the lookup happens here.
func (r *protocolRuntime) observedColumnValues(ctx context.Context, table string) map[string][]string {
	if r == nil || r.base == nil || strings.TrimSpace(table) == "" {
		return nil
	}
	if cached, ok := r.state.observedValues[table]; ok {
		return cached
	}
	out := map[string][]string{}
	// The kind and table shorthands are read as explicit arguments by the catalog
	// tool; a raw where object has to survive field-vocabulary validation and comes
	// back empty when it does not, with no error. Measured across a full run, no
	// call using where ever returned a column card, while the shorthand did — which
	// is why this check silently found nothing and never fired.
	for _, request := range []map[string]any{
		{"kind": "column", "table": table, "limit": observedValueCardLimit},
		{"table": table, "limit": observedValueCardLimit},
	} {
		// Every other internal catalog read scopes itself to the run's namespace.
		// This one did not, which returns nothing wherever a namespace is configured.
		r.addNamespace(request)
		result, err := r.base.QueryCatalog(ctx, request)
		if err != nil {
			continue
		}
		for _, card := range catalogCards(mapValue(result)) {
			entry := mapValue(card)
			// The card id always carries the column; column_name depends on which
			// fields the catalog surface projects, so it is the fallback rather than
			// the source.
			column := columnNameFromCatalogID(stringFromMap(entry, "id"))
			if column == "" {
				column = stringFromMap(entry, "column_name")
			}
			if column == "" {
				continue
			}
			if values := observedValuesFromEvidence(entry["evidence_json"]); len(values) != 0 {
				out[strings.ToLower(column)] = values
			}
		}
		if len(out) != 0 {
			break
		}
	}
	if r.state.observedValues == nil {
		r.state.observedValues = map[string]map[string][]string{}
	}
	r.state.observedValues[table] = out
	// Record what the lookup found. This check has now stayed silent across three
	// full benchmark runs and three wrong diagnoses, each made by inspection because
	// a quiet check leaves no trace to read. An empty result and a matching value are
	// indistinguishable from the outside; this separates them.
	r.state.recordObservedValueLookup(table, out)
	return out
}

// columnNameFromCatalogID reads the column out of column:<db>:<schema>.<table>.<column>.
func columnNameFromCatalogID(id string) string {
	trimmed := strings.TrimSpace(id)
	if !strings.HasPrefix(strings.ToLower(trimmed), "column:") {
		return ""
	}
	if index := strings.LastIndex(trimmed, "."); index >= 0 && index < len(trimmed)-1 {
		return trimmed[index+1:]
	}
	return ""
}

func observedValuesFromEvidence(raw any) []string {
	text, ok := raw.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return nil
	}
	var evidence map[string]any
	if err := json.Unmarshal([]byte(text), &evidence); err != nil {
		return nil
	}
	return evidenceStringSlice(evidence["observed_values"])
}

// unobservedWrittenValues returns the assignments whose value is absent from a
// column's observed set. A column with no sampled set is skipped: absence of
// evidence is not evidence of a mismatch.
func (r *protocolRuntime) unobservedWrittenValues(ctx context.Context, query string) []columnAssignment {
	assignments := mutationStringAssignments(query)
	if len(assignments) == 0 {
		return nil
	}
	var out []columnAssignment
	for _, assignment := range assignments {
		values := r.observedColumnValues(ctx, assignment.Table)[strings.ToLower(assignment.Column)]
		if len(values) == 0 {
			continue
		}
		if containsStringFold(values, assignment.Value) {
			continue
		}
		out = append(out, assignment)
		if len(out) == observedValueMismatchLimit {
			break
		}
	}
	return out
}

// describeUnobservedValues renders the mismatch and the values that were seen.
func (r *protocolRuntime) describeUnobservedValues(ctx context.Context, mismatches []columnAssignment) string {
	parts := make([]string, 0, len(mismatches))
	for _, m := range mismatches {
		values := r.observedColumnValues(ctx, m.Table)[strings.ToLower(m.Column)]
		parts = append(parts, fmt.Sprintf("%s.%s is being set to %q, but the values present in that column are %s",
			m.Table, m.Column, m.Value, strings.Join(values, ", ")))
	}
	return strings.Join(parts, "; ")
}
