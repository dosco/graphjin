package agent

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Ranking ties.
//
// A top-n question ("which customer ordered most?") is authored as order_by
// plus limit: n, and when rows n and n+1 share the ordering value, limit: n
// silently picks one of them. Measured on the coffee demo: three customers had
// one order each, the query took the first, and the answer named it as the
// customer with the most orders.
//
// So a query whose top-level root orders by fields and takes a small literal
// limit runs with a few extra rows. The result keeps the requested rows plus
// every extra row tied with the last one, and says so when the cut fell inside
// a tie. With no tie the result is exactly the requested rows — and since the
// extra rows prove where the ranking ends, the "reached its row limit" warning
// no longer applies to that root.

const (
	// rankingTieMaxLimit is the largest requested limit treated as a ranking
	// cut; larger limits are pages, not "top n" questions.
	rankingTieMaxLimit = 10
	// rankingTieProbeExtra is how many rows past the cut are fetched to see
	// whether the cut splits a tie.
	rankingTieProbeExtra = 5
)

// rankingCut is one top-level root whose limit cuts an ordered list.
type rankingCut struct {
	Key   string   // response key: the alias, else the field name
	Limit int      // the limit the caller asked for
	Order []string // order_by fields, in order
}

// rankingTieProbe returns query with each ranking root's limit raised by
// rankingTieProbeExtra, and the cuts to settle on the result. ok is false
// when the query has no such root or is not a plain query this scanner can
// rewrite safely.
func rankingTieProbe(query string) (string, []rankingCut, bool) {
	roots, ok := scanRootFields(query)
	if !ok {
		return query, nil, false
	}
	type edit struct {
		start, end int
		text       string
	}
	var edits []edit
	var cuts []rankingCut
	for _, root := range roots {
		if root.argsStart < 0 {
			continue
		}
		limit, limitStart, limitEnd, order, ok := rankingArgs(query[root.argsStart:root.argsEnd])
		if !ok || limit < 1 || limit > rankingTieMaxLimit || len(order) == 0 {
			continue
		}
		edits = append(edits, edit{root.argsStart + limitStart, root.argsStart + limitEnd, strconv.Itoa(limit + rankingTieProbeExtra)})
		cuts = append(cuts, rankingCut{Key: root.key, Limit: limit, Order: order})
	}
	if len(edits) == 0 {
		return query, nil, false
	}
	out := query
	for i := len(edits) - 1; i >= 0; i-- {
		e := edits[i]
		out = out[:e.start] + e.text + out[e.end:]
	}
	return out, cuts, true
}

// rankingTie is a cut that fell inside a tie: the rows sharing the deciding
// value, as returned.
type rankingTie struct {
	Root string
	Rows []map[string]any
}

// settleRankingTies trims each probed root back to its requested rows plus
// the rows tied with the last one, and attaches a notice when a cut was tied.
func settleRankingTies(out any, cuts []rankingCut) (any, []rankingTie) {
	var ties []rankingTie
	switch res := out.(type) {
	case executeResult:
		res.Data, res.Truncation, res.Recovery, res.Guidance, ties = settleRankingData(res.Data, res.Truncation, cuts, res.Recovery, res.Guidance)
		return res, ties
	case *executeResult:
		res.Data, res.Truncation, res.Recovery, res.Guidance, ties = settleRankingData(res.Data, res.Truncation, cuts, res.Recovery, res.Guidance)
		return res, ties
	default:
		mapped := mapValue(normalizeValue(out))
		if mapped == nil {
			return out, nil
		}
		mapped = cloneAnyMap(mapped)
		var truncation *TruncationInfo
		data, _, recovery, guidance, found := settleRankingData(mapped["data"], truncation, cuts, mapped["recovery"], stringValue(mapped["guidance"]))
		ties = found
		mapped["data"] = data
		if recovery != nil {
			mapped["recovery"] = recovery
		}
		if guidance != "" {
			mapped["guidance"] = guidance
		}
		if t := mapValue(mapped["truncation"]); t != nil {
			mapped["truncation"] = dropTruncatedRootsMap(t, cuts)
		}
		return mapped, ties
	}
}

func settleRankingData(data any, truncation *TruncationInfo, cuts []rankingCut, recovery any, guidance string) (any, *TruncationInfo, any, string, []rankingTie) {
	root := mapValue(data)
	if root == nil {
		return data, truncation, recovery, guidance, nil
	}
	root = cloneAnyMap(root)
	var ties []map[string]any
	var found []rankingTie
	for _, cut := range cuts {
		rows, ok := root[cut.Key].([]any)
		if !ok {
			continue
		}
		truncation = dropTruncatedRoot(truncation, cut.Key)
		if len(rows) <= cut.Limit {
			continue
		}
		last := mapValue(rows[cut.Limit-1])
		keep := cut.Limit
		comparable := last != nil && hasOrderValues(last, cut.Order)
		if comparable {
			for keep < len(rows) && sameOrderValues(mapValue(rows[keep]), last, cut.Order) {
				keep++
			}
		}
		root[cut.Key] = rows[:keep]
		if keep == cut.Limit {
			continue
		}
		value := map[string]any{}
		for _, field := range cut.Order {
			value[field] = last[field]
		}
		tie := rankingTie{Root: cut.Key}
		for _, row := range rows[cut.Limit-1 : keep] {
			tie.Rows = append(tie.Rows, mapValue(row))
		}
		found = append(found, tie)
		ties = append(ties, map[string]any{
			"root":            cut.Key,
			"requested_limit": cut.Limit,
			"rows":            keep,
			"tied_value":      value,
			"more_may_tie":    keep == len(rows) && len(rows) == cut.Limit+rankingTieProbeExtra,
		})
	}
	if len(ties) == 0 {
		return root, truncation, recovery, guidance, nil
	}
	instruction := rankingTieInstruction(ties)
	if recovery == nil {
		recovery = map[string]any{"kind": "ranking_tie", "code": "ranking_tie", "instruction": instruction, "ties": ties}
	}
	if guidance == "" {
		guidance = instruction
	} else {
		guidance += " " + instruction
	}
	return root, truncation, recovery, guidance, found
}

func rankingTieInstruction(ties []map[string]any) string {
	var b strings.Builder
	for i, tie := range ties {
		if i > 0 {
			b.WriteString(" ")
		}
		limit, rows := tie["requested_limit"].(int), tie["rows"].(int)
		fmt.Fprintf(&b, "The %s ranking is tied at its cut: the query asked for %d row(s), but %d rows share the deciding value %s, so all of them are returned.",
			tie["root"], limit, rows, stringify(tie["tied_value"]))
		if more, _ := tie["more_may_tie"].(bool); more {
			b.WriteString(" More rows may share it as well.")
		}
	}
	b.WriteString(" Report the tie — name every tied row — instead of presenting one of them as the single answer.")
	return b.String()
}

func hasOrderValues(row map[string]any, order []string) bool {
	for _, field := range order {
		if _, ok := row[field]; !ok {
			return false
		}
	}
	return true
}

func sameOrderValues(row, last map[string]any, order []string) bool {
	if row == nil {
		return false
	}
	for _, field := range order {
		value, ok := row[field]
		if !ok || stringify(value) != stringify(last[field]) {
			return false
		}
	}
	return true
}

func dropTruncatedRoot(truncation *TruncationInfo, path string) *TruncationInfo {
	if truncation == nil {
		return nil
	}
	kept := truncation.Roots[:0:0]
	for _, root := range truncation.Roots {
		if root.Path != path {
			kept = append(kept, root)
		}
	}
	if len(kept) == 0 {
		return nil
	}
	return &TruncationInfo{Roots: kept, Message: TruncationMessage(kept)}
}

func dropTruncatedRootsMap(truncation map[string]any, cuts []rankingCut) any {
	roots := anySlice(truncation["roots"])
	kept := make([]any, 0, len(roots))
	for _, root := range roots {
		path := stringValue(mapValue(root)["path"])
		probed := false
		for _, cut := range cuts {
			if cut.Key == path {
				probed = true
			}
		}
		if !probed {
			kept = append(kept, root)
		}
	}
	if len(kept) == 0 {
		return nil
	}
	out := cloneAnyMap(truncation)
	out["roots"] = kept
	return out
}

// --- answers -----------------------------------------------------------------

// unreportedRankingTies returns the ties the answer reports only in part: it
// names at least one tied row — so it is about this ranking — but not all of
// them. Rows are told apart by the values that differ between them; a tie
// whose rows cannot be told apart is never flagged.
func unreportedRankingTies(answer string, ties []rankingTie) []rankingTie {
	lower := strings.ToLower(answer)
	var out []rankingTie
	for _, tie := range ties {
		labels := rankingTieLabels(tie)
		if len(labels) == 0 {
			continue
		}
		named := 0
		for _, rowLabels := range labels {
			for _, label := range rowLabels {
				if mentionsLabel(lower, label) {
					named++
					break
				}
			}
		}
		if named > 0 && named < len(labels) {
			out = append(out, tie)
		}
	}
	return out
}

// rankingTieLabels returns, per tied row, the values that identify it: the
// leaf fields (one level of nesting) whose values differ across the tied
// rows. Fields are compared by path, so a customer_id of 1 still names its row
// when the tied count is also 1. Text fields are preferred; numbers are used
// only when no distinguishing field holds text.
func rankingTieLabels(tie rankingTie) [][]string {
	flat := make([]map[string]string, len(tie.Rows))
	paths := map[string]bool{}
	for i, row := range tie.Rows {
		flat[i] = map[string]string{}
		collectLeafValues(row, "", 0, func(path, v string) {
			flat[i][path] = v
			paths[path] = true
		})
	}
	var distinguishing []string
	textual := false
	for path := range paths {
		seen := map[string]bool{}
		unique := true
		for _, row := range flat {
			v, ok := row[path]
			if !ok || seen[v] {
				unique = false
				break
			}
			seen[v] = true
		}
		if !unique {
			continue
		}
		distinguishing = append(distinguishing, path)
		for _, row := range flat {
			if !isNumericLabel(row[path]) && len(row[path]) >= 3 {
				textual = true
			}
		}
	}
	if len(distinguishing) == 0 {
		return nil
	}
	sort.Strings(distinguishing)
	labels := make([][]string, len(flat))
	for i, row := range flat {
		for _, path := range distinguishing {
			v := row[path]
			if textual && (isNumericLabel(v) || len(v) < 3) {
				continue
			}
			labels[i] = append(labels[i], v)
		}
		if len(labels[i]) == 0 {
			return nil
		}
	}
	return labels
}

func collectLeafValues(value any, path string, depth int, add func(path, value string)) {
	switch v := value.(type) {
	case map[string]any:
		if depth > 1 {
			return
		}
		for key, child := range v {
			collectLeafValues(child, path+"."+key, depth+1, add)
		}
	case string:
		if s := strings.ToLower(strings.TrimSpace(v)); s != "" {
			add(path, s)
		}
	case float64, int, int64, json.Number:
		add(path, strings.ToLower(stringify(v)))
	}
}

func isNumericLabel(v string) bool {
	_, err := strconv.ParseFloat(v, 64)
	return err == nil
}

// mentionsLabel matches a label as a whole word, so id 1 is not found
// inside 10 and "pro" is not found inside "process".
func mentionsLabel(lower, label string) bool {
	for from := 0; ; {
		i := strings.Index(lower[from:], label)
		if i < 0 {
			return false
		}
		start, end := from+i, from+i+len(label)
		if (start == 0 || !isNameByte(lower[start-1])) && (end == len(lower) || !isNameByte(lower[end])) {
			return true
		}
		from = start + 1
	}
}

const rankingTieRewriteSignature = `"The draft answer presents some rows of a ranking as the answer, but the ranking is tied: every row in tied_rows shares the deciding value. Restate the answer so it names every tied row and says they are tied. Keep what the draft got right, and use only values from tied_rows and the draft."
instruction:string "The user's original request.",
draft_answer:string "The draft answer.",
tied_rows:string "The tied rows, as JSON."
-> answer:string "The restated answer."`

// rankingTieNote states a tie in plain words when a restatement could not:
// engine-observed values, no model involved.
func rankingTieNote(ties []rankingTie) string {
	var b strings.Builder
	for _, tie := range ties {
		labels := rankingTieLabels(tie)
		names := make([]string, 0, len(labels))
		for _, l := range labels {
			names = append(names, l[0])
		}
		fmt.Fprintf(&b, "\n\nNote: this is a tie — %d rows share the top value: %s.", len(tie.Rows), strings.Join(names, ", "))
	}
	return b.String()
}

// --- scanner -----------------------------------------------------------------
//
// GraphJin's parser is internal to core, so this is a deliberately small
// scanner: it reads only a query operation's top-level fields and their
// argument lists, and gives up (no rewrite) on anything it does not fully
// understand — fragments, directives, variables in the limit, list-shaped or
// nested order_by. Giving up is always safe: the query then runs as written.

type rootField struct {
	key                string
	argsStart, argsEnd int // byte span inside the parentheses; -1 when no args
}

func scanRootFields(query string) ([]rootField, bool) {
	s := gqlScanner{src: query}
	s.skipIgnored()
	switch {
	case s.peek() == '{':
	case s.word() == "query":
		s.pos += len("query")
		s.skipIgnored()
		if isNameStart(s.peek()) {
			s.readName()
			s.skipIgnored()
		}
		if s.peek() == '(' {
			if _, ok := s.skipBalanced('(', ')'); !ok {
				return nil, false
			}
			s.skipIgnored()
		}
		if s.peek() != '{' {
			return nil, false
		}
	default:
		return nil, false
	}
	s.pos++ // the operation's selection set
	var roots []rootField
	for {
		s.skipIgnored()
		switch c := s.peek(); {
		case c == '}':
			s.pos++
			s.skipIgnored()
			// A second definition (another operation or a fragment) means the
			// executed operation is ambiguous to this scanner.
			return roots, s.pos >= len(s.src)
		case isNameStart(c):
		default:
			return nil, false
		}
		name := s.readName()
		s.skipIgnored()
		key := name
		if s.peek() == ':' {
			s.pos++
			s.skipIgnored()
			if !isNameStart(s.peek()) {
				return nil, false
			}
			s.readName()
			s.skipIgnored()
		}
		field := rootField{key: key, argsStart: -1, argsEnd: -1}
		if s.peek() == '(' {
			start := s.pos + 1
			end, ok := s.skipBalanced('(', ')')
			if !ok {
				return nil, false
			}
			field.argsStart, field.argsEnd = start, end-1
			s.skipIgnored()
		}
		if s.peek() == '@' {
			return nil, false
		}
		if s.peek() == '{' {
			if _, ok := s.skipBalanced('{', '}'); !ok {
				return nil, false
			}
		}
		roots = append(roots, field)
	}
}

// rankingArgs reads limit (an integer literal) and order_by (a flat object of
// field: direction) from one argument list.
func rankingArgs(args string) (limit, limitStart, limitEnd int, order []string, ok bool) {
	s := gqlScanner{src: args}
	limit = -1
	for {
		s.skipIgnored()
		if s.pos >= len(s.src) {
			return limit, limitStart, limitEnd, order, limit >= 0
		}
		if !isNameStart(s.peek()) {
			return 0, 0, 0, nil, false
		}
		name := s.readName()
		s.skipIgnored()
		if s.peek() != ':' {
			return 0, 0, 0, nil, false
		}
		s.pos++
		s.skipIgnored()
		valueStart := s.pos
		if !s.skipValue() {
			return 0, 0, 0, nil, false
		}
		value := strings.TrimSpace(s.src[valueStart:s.pos])
		switch name {
		case "limit":
			n, err := strconv.Atoi(value)
			if err != nil {
				return 0, 0, 0, nil, false
			}
			limit, limitStart, limitEnd = n, valueStart, valueStart+len(value)
		case "order_by":
			fields, ok := flatOrderBy(value)
			if !ok {
				return 0, 0, 0, nil, false
			}
			order = fields
		}
	}
}

func flatOrderBy(value string) ([]string, bool) {
	if !strings.HasPrefix(value, "{") || !strings.HasSuffix(value, "}") {
		return nil, false
	}
	s := gqlScanner{src: value[1 : len(value)-1]}
	var fields []string
	for {
		s.skipIgnored()
		if s.pos >= len(s.src) {
			return fields, len(fields) != 0
		}
		if !isNameStart(s.peek()) {
			return nil, false
		}
		field := s.readName()
		s.skipIgnored()
		if s.peek() != ':' {
			return nil, false
		}
		s.pos++
		s.skipIgnored()
		if !isNameStart(s.peek()) {
			return nil, false // nested or variable ordering
		}
		direction := strings.ToLower(s.readName())
		if !strings.HasPrefix(direction, "asc") && !strings.HasPrefix(direction, "desc") {
			return nil, false
		}
		fields = append(fields, field)
	}
}

type gqlScanner struct {
	src string
	pos int
}

func (s *gqlScanner) peek() byte {
	if s.pos >= len(s.src) {
		return 0
	}
	return s.src[s.pos]
}

func (s *gqlScanner) word() string {
	end := s.pos
	for end < len(s.src) && isNameByte(s.src[end]) {
		end++
	}
	return s.src[s.pos:end]
}

func (s *gqlScanner) readName() string {
	w := s.word()
	s.pos += len(w)
	return w
}

// skipIgnored skips whitespace, commas and comments.
func (s *gqlScanner) skipIgnored() {
	for s.pos < len(s.src) {
		switch c := s.src[s.pos]; {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == ',':
			s.pos++
		case c == '#':
			for s.pos < len(s.src) && s.src[s.pos] != '\n' {
				s.pos++
			}
		default:
			return
		}
	}
}

// skipBalanced skips from an opening bracket through its match, honouring
// strings, and returns the position just past the closing bracket.
func (s *gqlScanner) skipBalanced(open, close byte) (int, bool) {
	if s.peek() != open {
		return s.pos, false
	}
	depth := 0
	for s.pos < len(s.src) {
		switch c := s.src[s.pos]; c {
		case '"':
			if !s.skipString() {
				return s.pos, false
			}
			continue
		case '#':
			for s.pos < len(s.src) && s.src[s.pos] != '\n' {
				s.pos++
			}
			continue
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				s.pos++
				return s.pos, true
			}
		}
		s.pos++
	}
	return s.pos, false
}

func (s *gqlScanner) skipString() bool {
	if strings.HasPrefix(s.src[s.pos:], `"""`) {
		end := strings.Index(s.src[s.pos+3:], `"""`)
		if end < 0 {
			return false
		}
		s.pos += 3 + end + 3
		return true
	}
	s.pos++
	for s.pos < len(s.src) {
		switch s.src[s.pos] {
		case '\\':
			s.pos += 2
			continue
		case '"':
			s.pos++
			return true
		}
		s.pos++
	}
	return false
}

// skipValue skips one argument value.
func (s *gqlScanner) skipValue() bool {
	switch c := s.peek(); {
	case c == '"':
		return s.skipString()
	case c == '{':
		_, ok := s.skipBalanced('{', '}')
		return ok
	case c == '[':
		_, ok := s.skipBalanced('[', ']')
		return ok
	case c == '$':
		s.pos++
		return s.readName() != ""
	case c == '-' || c == '+' || ('0' <= c && c <= '9') || c == '.':
		for s.pos < len(s.src) && strings.IndexByte("+-.eE0123456789", s.src[s.pos]) >= 0 {
			s.pos++
		}
		return true
	case isNameStart(c):
		s.readName()
		return true
	}
	return false
}

func isNameStart(c byte) bool {
	return c == '_' || ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z')
}

func isNameByte(c byte) bool {
	return isNameStart(c) || ('0' <= c && c <= '9')
}
