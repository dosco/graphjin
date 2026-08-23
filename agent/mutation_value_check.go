package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
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

// stringLiteralSpan is one string value with the byte range of its contents in
// the enclosing text, quotes excluded, so a repair can rewrite exactly that value.
type stringLiteralSpan struct {
	value      string
	start, end int
}

// graphQLStringFieldSpans returns the `name: "value"` pairs directly in an input
// object body, skipping nested objects so a where clause inside an insert does not
// contribute assignments.
func graphQLStringFieldSpans(body string) map[string]stringLiteralSpan {
	out := map[string]stringLiteralSpan{}
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
			out[field] = stringLiteralSpan{value: text, start: value + 1, end: end - 1}
			i = end
		default:
			i++
		}
	}
	return out
}

func graphQLStringFields(body string) map[string]string {
	out := map[string]string{}
	for field, literal := range graphQLStringFieldSpans(body) {
		out[field] = literal.value
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
	cardsSeen := 0
	sampleID := ""

	// Two calls, and the second is the one that matters. A filtered catalog read
	// returns summaries: every card comes back with evidence_json blanked, by
	// design, because payloads travel only on detail lookups by id. This check spent
	// four full benchmark runs finding nothing for that reason alone — the cards
	// arrived, correctly filtered, deliberately stripped of the values being asked
	// for.
	//
	// So the filter supplies what it does carry, the ids, and those are then fetched
	// in full.
	ids := r.columnCardIDs(ctx, table, &cardsSeen, &sampleID)
	if len(ids) != 0 {
		detail := map[string]any{"ids": ids}
		r.addNamespace(detail)
		if result, err := r.base.QueryCatalog(ctx, detail); err == nil {
			for _, card := range catalogCards(mapValue(result)) {
				entry := mapValue(card)
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
		}
	}

	if r.state.observedValues == nil {
		r.state.observedValues = map[string]map[string][]string{}
	}
	r.state.observedValues[table] = out
	// Record what the lookup found. A quiet check leaves no trace to read, and this
	// one stayed quiet across four runs while every layer it depends on tested clean.
	r.state.recordObservedValueLookup(table, out, cardsSeen, sampleID)
	return out
}

// columnCardIDs lists a table's column card ids. The kind and table shorthands are
// read as explicit arguments by every catalog surface; a raw where object has to
// survive field-vocabulary validation and comes back empty when it does not, with
// no error.
func (r *protocolRuntime) columnCardIDs(ctx context.Context, table string, cardsSeen *int, sampleID *string) []any {
	var ids []any
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
			*cardsSeen++
			id := stringFromMap(entry, "id")
			if *sampleID == "" {
				*sampleID = id
			}
			if columnNameFromCatalogID(id) != "" {
				ids = append(ids, id)
			}
		}
		if len(ids) != 0 {
			break
		}
	}
	if len(ids) > observedValueCardLimit {
		ids = ids[:observedValueCardLimit]
	}
	return ids
}

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
		line := fmt.Sprintf("%s.%s is being set to %q, but the values present in that column are %s",
			m.Table, m.Column, m.Value, strings.Join(values, ", "))
		if suggested, ok := closestObservedValue(m.Value, values); ok {
			line += fmt.Sprintf("; the closest of these to %q is %q", m.Value, suggested)
		}
		parts = append(parts, line)
	}
	return strings.Join(parts, "; ")
}

// The list alone did not move behaviour. Run 2cae6c1c handed eleven episodes the
// values open, pending and resolved; ten resubmitted "closed" and across two runs
// those writes rose from 46 to 64. Every repair today that measurably changed what
// a model did named the exact answer — the repaired watch mutation, the canonical
// catalog id, the retained subject — and every one that named a list did not. So
// when one existing value is clearly nearest to the rejected one, the repair now
// says which, and carries the corrected write. It is offered, never executed: the
// mapping from "close it off" to a vocabulary is a semantic call, and it stays the
// model's.

// closestObservedValue returns the observed value nearest to the rejected one, and
// only when there is a clear winner: a tie or a weak best is no suggestion at all,
// which falls back to the plain list.
func closestObservedValue(written string, values []string) (string, bool) {
	target := strings.ToLower(strings.TrimSpace(written))
	if target == "" || len(values) == 0 {
		return "", false
	}
	best, second := 0.0, 0.0
	bestValue := ""
	for _, value := range values {
		score := valueSimilarity(target, strings.ToLower(value))
		if score > best {
			second = best
			best = score
			bestValue = value
		} else if score > second {
			second = score
		}
	}
	if best < 0.2 || best <= second {
		return "", false
	}
	return bestValue, true
}

// valueSimilarity blends character-bigram overlap with shared affixes, which is
// what recognises closed/resolved as kin (the shared "ed") while leaving unrelated
// words unmatched.
func valueSimilarity(a, b string) float64 {
	if a == b {
		return 1
	}
	aGrams, bGrams := valueBigrams(a), valueBigrams(b)
	if len(aGrams) == 0 || len(bGrams) == 0 {
		return 0
	}
	shared := 0
	for gram := range aGrams {
		if bGrams[gram] {
			shared++
		}
	}
	score := 2 * float64(shared) / float64(len(aGrams)+len(bGrams))
	for l := 2; l <= 3; l++ {
		if len(a) >= l && len(b) >= l {
			if a[:l] == b[:l] {
				score += 0.15
			}
			if a[len(a)-l:] == b[len(b)-l:] {
				score += 0.15
			}
		}
	}
	return score
}

func valueBigrams(value string) map[string]bool {
	out := map[string]bool{}
	for i := 0; i+2 <= len(value); i++ {
		out[value[i:i+2]] = true
	}
	return out
}

// writtenValueSpan is a string assignment plus the byte range of its literal in
// the raw mutation, so the suggested value can replace exactly that literal.
type writtenValueSpan struct {
	columnAssignment
	start, end int
}

func mutationWrittenValueSpans(query string) []writtenValueSpan {
	if !ContainsMutationOperation(query) {
		return nil
	}
	clean := graphQLStructure(query)
	var out []writtenValueSpan
	for _, root := range MutationRootFields(query) {
		if strings.HasPrefix(strings.ToLower(root), "gj_") {
			continue
		}
		for _, keyword := range []string{"update", "insert"} {
			for _, span := range mutationInputBlocks(clean, root, keyword) {
				for column, literal := range graphQLStringFieldSpans(query[span[0]:span[1]]) {
					out = append(out, writtenValueSpan{
						columnAssignment: columnAssignment{Table: root, Column: column, Value: literal.value},
						start:            span[0] + literal.start,
						end:              span[0] + literal.end,
					})
				}
			}
		}
	}
	return out
}

// substituteWrittenValues returns the mutation with every mismatched literal
// replaced by its suggested value. All or nothing: a partly substituted mutation
// would look corrected while still carrying a rejected value.
func substituteWrittenValues(query string, mismatches []columnAssignment, suggestions map[string]string) (string, bool) {
	spans := mutationWrittenValueSpans(query)
	type edit struct {
		start, end int
		text       string
	}
	var edits []edit
	for _, mismatch := range mismatches {
		suggested, ok := suggestions[mismatch.Table+"."+strings.ToLower(mismatch.Column)]
		if !ok {
			return "", false
		}
		found := false
		for _, span := range spans {
			if span.Table == mismatch.Table && strings.EqualFold(span.Column, mismatch.Column) && span.Value == mismatch.Value {
				edits = append(edits, edit{start: span.start, end: span.end, text: escapeGraphQLStringValue(suggested)})
				found = true
			}
		}
		if !found {
			return "", false
		}
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	out := query
	for _, e := range edits {
		out = out[:e.start] + e.text + out[e.end:]
	}
	return out, true
}

func escapeGraphQLStringValue(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	return strings.ReplaceAll(value, `"`, `\"`)
}

// suggestedWrittenValues maps each mismatch to its closest observed value, keyed
// table.column. Mismatches with no clear candidate are simply absent.
func (r *protocolRuntime) suggestedWrittenValues(ctx context.Context, mismatches []columnAssignment) map[string]string {
	out := map[string]string{}
	for _, mismatch := range mismatches {
		values := r.observedColumnValues(ctx, mismatch.Table)[strings.ToLower(mismatch.Column)]
		if suggested, ok := closestObservedValue(mismatch.Value, values); ok {
			out[mismatch.Table+"."+strings.ToLower(mismatch.Column)] = suggested
		}
	}
	return out
}

// A failed write's error used to name the fault ("column: 'payments.created_at'
// not found", or the NOT NULL constraint two steps downstream of a silently
// dropped key) without ever naming what the table actually has — and the model
// retried the identical wrong column until the duplicate guard locked it out.
// The catalog knows the columns; put them in the recovery.

var unknownColumnErrorPatterns = []*regexp.Regexp{
	// column: 'payments.created_at' not found  (optionally schema-qualified)
	regexp.MustCompile(`column: '(?:[A-Za-z0-9_]+\.)?([A-Za-z0-9_]+)\.([A-Za-z0-9_]+)' not found`),
	// NOT NULL constraint failed: payments.recorded_at
	regexp.MustCompile(`NOT NULL constraint failed: ([A-Za-z0-9_]+)\.([A-Za-z0-9_]+)`),
	// field 'created_at' is not a column or a function  (selection-set form; no table)
	regexp.MustCompile(`field '([A-Za-z0-9_]+)' is not a column or a function`),
}

// attachUnknownColumnRecovery extends an attached execution recovery with the
// failed table's real column names when the error is of the unknown-column
// family. It reuses the value check's phase-one catalog read — the returned
// card ids encode every column name — so this costs no extra catalog calls
// beyond the one bounded list.
//
// For a failed mutation whose every unknown key resolves to exactly one real
// column, the corrected write itself is computed and returned alongside —
// benchmark evidence is unambiguous that the exact string is the only repair
// weak models take. The second return is that corrected mutation, empty when
// no certain repair exists.
func (r *protocolRuntime) attachUnknownColumnRecovery(ctx context.Context, out any, query string) (any, string) {
	if r == nil || !executionFailed(out) {
		return out, ""
	}
	table, column := "", ""
	for _, message := range executionErrorMessages(out) {
		for _, pattern := range unknownColumnErrorPatterns {
			match := pattern.FindStringSubmatch(message)
			if match == nil {
				continue
			}
			if len(match) == 3 {
				table, column = match[1], match[2]
			} else {
				column = match[1]
			}
			break
		}
		if column != "" {
			break
		}
	}
	if column == "" {
		return out, ""
	}
	if table == "" {
		for _, root := range append(MutationRootFields(query), QueryRootFields(query)...) {
			// The bool reports a Hasura-style remapping, not existence; a plain
			// root comes back as itself. A root that names no real table simply
			// yields no observed columns below, which bails the same way.
			if target, _ := r.state.mutationTargetTable(root); target != "" {
				table = target
				break
			}
		}
	}
	if table == "" {
		return out, ""
	}
	columns := r.observedColumnNames(ctx, table)
	if len(columns) == 0 {
		return out, ""
	}
	note := fmt.Sprintf(" The column %q does not exist on %s; its columns are: %s.", column, table, strings.Join(columns, ", "))
	details := map[string]any{"unknown_column": column, "table_columns": map[string]any{table: columns}}
	repaired := ""
	var next map[string]any
	if ContainsMutationOperation(query) {
		if fixed, renames, ok := repairUnknownMutationColumns(query, columns); ok {
			repaired = fixed
			renamed := make([]string, 0, len(renames))
			for _, rename := range renames {
				renamed = append(renamed, rename.From+" -> "+rename.To)
			}
			// Run 969337b6 measured the conditional framing ("if this matches
			// your intent") losing to the factual one on the value repair, so
			// the schema's vocabulary is stated as fact here too.
			note = fmt.Sprintf(" This table spells those fields differently: %s. The corrected mutation is in details.repaired_query — execute it exactly as given.", strings.Join(renamed, ", "))
			reason := "Execute details.repaired_query exactly as given; it is this same write expressed with the column names this table actually has."
			if companion := r.companionTimestampNote(ctx, repaired); companion != "" {
				note += " " + companion
				reason += " " + companion
			}
			details["repaired_query"] = repaired
			details["renamed_columns"] = renamed
			next = map[string]any{
				"recommended_tool": "execute_graphql",
				"args":             map[string]any{"query": repaired},
				"reason":           reason,
			}
		}
	}
	amend := func(recovery map[string]any) {
		if recovery == nil {
			return
		}
		recovery["instruction"] = stringValue(recovery["instruction"]) + note
		recovery["details"] = details
		if next != nil {
			recovery["next"] = next
		}
	}
	switch res := out.(type) {
	case executeResult:
		amend(mapValue(res.Recovery))
		return res, repaired
	case *executeResult:
		amend(mapValue(res.Recovery))
		return res, repaired
	case map[string]any:
		amend(mapValue(res["recovery"]))
		return res, repaired
	default:
		return out, repaired
	}
}

// observedColumnNames lists a table's column names from the catalog, cached
// per run. Phase one of the value lookup already carries them: every column
// card id ends in its column name.
func (r *protocolRuntime) observedColumnNames(ctx context.Context, table string) []string {
	if r == nil || r.base == nil || strings.TrimSpace(table) == "" {
		return nil
	}
	if cached, ok := r.state.tableColumnNames[table]; ok {
		return cached
	}
	cardsSeen := 0
	sampleID := ""
	var columns []string
	for _, id := range r.columnCardIDs(ctx, table, &cardsSeen, &sampleID) {
		if name := columnNameFromCatalogID(stringValue(id)); name != "" {
			columns = append(columns, name)
		}
	}
	if r.state.tableColumnNames == nil {
		r.state.tableColumnNames = map[string][]string{}
	}
	r.state.tableColumnNames[table] = columns
	return columns
}
