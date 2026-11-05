package agent

import (
	"fmt"
	"strings"
)

// Upcoming reads.
//
// "Which account has the soonest upcoming renewal?" was read as
// subscriptions(order_by: {renews_at: asc}, limit: 1): soonest, but not
// upcoming. The first row was a cancelled account's renewal from a month ago,
// and the answer reported it. In the lead eval that was every run that read
// subscriptions without a date filter.
//
// So when a question asks for what is upcoming, a top-n read that orders a
// date ascending and comes back with its first row dated before today runs
// again with that date held to today or later. It only acts on evidence: the
// filter is added when a returned row proves the read reached into the past,
// so a correct query — one already filtered, or whose first row is in the
// future — is never touched.

// asksUpcoming reports whether an instruction asks for future rows.
func asksUpcoming(instruction string) bool {
	padded := " " + normalizedWords(instruction) + " "
	for _, phrase := range []string{"upcoming", "next", "soonest", "coming up", "future", "forthcoming"} {
		if strings.Contains(padded, " "+phrase+" ") {
			return true
		}
	}
	return false
}

// upcomingCut is one top-level root that orders a date ascending under a
// small limit.
type upcomingCut struct {
	key, field           string
	argsStart            int
	whereStart, whereEnd int // value span of where:, -1 when absent
}

// upcomingCuts finds the roots a past leading row would make wrong.
func upcomingCuts(query string) []upcomingCut {
	roots, ok := scanRootFields(query)
	if !ok {
		return nil
	}
	var cuts []upcomingCut
	for _, root := range roots {
		if root.argsStart < 0 {
			continue
		}
		cut, ok := upcomingArgs(query[root.argsStart:root.argsEnd])
		if !ok {
			continue
		}
		cut.key = root.key
		cut.argsStart = root.argsStart
		if cut.whereStart >= 0 {
			cut.whereStart += root.argsStart
			cut.whereEnd += root.argsStart
		}
		cuts = append(cuts, cut)
	}
	return cuts
}

func upcomingArgs(args string) (upcomingCut, bool) {
	s := gqlScanner{src: args}
	cut := upcomingCut{whereStart: -1, whereEnd: -1}
	limited := false
	for {
		s.skipIgnored()
		if s.pos >= len(s.src) {
			return cut, limited && cut.field != ""
		}
		if !isNameStart(s.peek()) {
			return cut, false
		}
		name := s.readName()
		s.skipIgnored()
		if s.peek() != ':' {
			return cut, false
		}
		s.pos++
		s.skipIgnored()
		start := s.pos
		if !s.skipValue() {
			return cut, false
		}
		value := strings.TrimSpace(s.src[start:s.pos])
		switch name {
		case "limit":
			n := 0
			if _, err := fmt.Sscanf(value, "%d", &n); err != nil || n < 1 || n > rankingTieMaxLimit {
				return cut, false
			}
			limited = true
		case "order_by":
			field, ascending := leadingOrder(value)
			if !ascending || !temporalName(field) {
				return cut, false
			}
			cut.field = field
		case "where":
			cut.whereStart, cut.whereEnd = start, start+len(value)
		}
	}
}

// leadingOrder reads the first field of a flat order_by and whether it sorts
// ascending.
func leadingOrder(value string) (string, bool) {
	if !strings.HasPrefix(value, "{") || !strings.HasSuffix(value, "}") {
		return "", false
	}
	s := gqlScanner{src: value[1 : len(value)-1]}
	s.skipIgnored()
	if !isNameStart(s.peek()) {
		return "", false
	}
	field := s.readName()
	s.skipIgnored()
	if s.peek() != ':' {
		return "", false
	}
	s.pos++
	s.skipIgnored()
	if !isNameStart(s.peek()) {
		return "", false
	}
	return field, strings.HasPrefix(strings.ToLower(s.readName()), "asc")
}

func temporalName(field string) bool {
	field = strings.ToLower(field)
	return strings.HasSuffix(field, "_at") || strings.HasSuffix(field, "_on") || strings.Contains(field, "date")
}

// upcomingRewrite returns query with each cut whose first returned row is
// dated before today held to today or later, or "" when no row reached into
// the past.
func upcomingRewrite(query string, out any, today string) (string, []string) {
	if len(today) != len("2006-01-02") {
		return "", nil
	}
	data := resultData(out)
	type edit struct {
		start, end int
		text       string
	}
	var edits []edit
	var fields []string
	for _, cut := range upcomingCuts(query) {
		rows, _ := data[cut.key].([]any)
		if len(rows) == 0 {
			continue
		}
		first := stringValue(mapValue(rows[0])[cut.field])
		if len(first) < len(today) || first[:len(today)] >= today {
			continue
		}
		bound := fmt.Sprintf("{%s: {gte: %q}}", cut.field, today)
		if cut.whereStart >= 0 {
			edits = append(edits, edit{cut.whereStart, cut.whereEnd, "{and: [" + query[cut.whereStart:cut.whereEnd] + ", " + bound + "]}"})
		} else {
			edits = append(edits, edit{cut.argsStart, cut.argsStart, "where: " + bound + ", "})
		}
		fields = append(fields, cut.key+"."+cut.field)
	}
	if len(edits) == 0 {
		return "", nil
	}
	// Edits are in query order; apply from the end so offsets stay valid.
	out2 := query
	for i := len(edits) - 1; i >= 0; i-- {
		e := edits[i]
		out2 = out2[:e.start] + e.text + out2[e.end:]
	}
	return out2, fields
}

func upcomingNotice(fields []string, today string) string {
	return fmt.Sprintf("The question asks for upcoming dates, but the first row this read returned was dated before today (%s), so it ran again with %s held to today or later; the rows above are the upcoming ones. Answer from them.",
		today, strings.Join(fields, ", "))
}

// resultData is the data object of an execute result, whatever its shape.
func resultData(out any) map[string]any {
	switch res := out.(type) {
	case executeResult:
		return mapValue(normalizeValue(res.Data))
	case *executeResult:
		return mapValue(normalizeValue(res.Data))
	}
	return mapValue(mapValue(normalizeValue(out))["data"])
}

// withGuidance appends text to an execute result's guidance.
func withGuidance(out any, text string) any {
	join := func(existing string) string {
		if existing == "" {
			return text
		}
		return existing + " " + text
	}
	switch res := out.(type) {
	case executeResult:
		res.Guidance = join(res.Guidance)
		return res
	case *executeResult:
		res.Guidance = join(res.Guidance)
		return res
	}
	mapped := mapValue(normalizeValue(out))
	if mapped == nil {
		return out
	}
	mapped = cloneAnyMap(mapped)
	mapped["guidance"] = join(stringValue(mapped["guidance"]))
	return mapped
}
