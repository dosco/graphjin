package agent

import (
	"strings"
)

// A watch's subscription travels as a string inside the mutation, so any filter
// forces nested quotes. Naming the fault and pointing at the variable form was not
// enough: after the false-positive in the comma handling was fixed, 28 attempts in
// one run of the frozen suite still arrived with the inner quotes unescaped, and
// only two watches were created across 36 episodes.
//
// The intent is never in doubt. The string was meant to hold that subscription, and
// the engine can see exactly which quotes need escaping. So rather than describing
// the fix, hand back the corrected mutation.
//
// It is offered rather than executed. Rewriting a caller's mutation and running it
// means executing something they did not write; a repaired copy they re-send keeps
// the decision theirs, costs one step, and cannot execute a subscription nobody
// authored.

// repairWatchSubscriptionString returns the mutation with the inlined subscription
// correctly escaped. It reports false when the intended boundary is not
// unambiguous, so a genuinely unparseable mutation is still reported as such.
func repairWatchSubscriptionString(query string) (string, bool) {
	at := strings.Index(strings.ToLower(query), "query:")
	if at == -1 {
		return "", false
	}
	open := skipGraphQLSpace(query, at+len("query:"))
	if open >= len(query) || query[open] != '"' {
		return "", false
	}
	end, ok := watchSubscriptionStringEnd(query, open)
	if !ok {
		return "", false
	}

	// Everything between the quotes is the subscription; re-escape it from scratch
	// so already-escaped quotes are not doubled.
	inner := query[open+1 : end]
	var escaped strings.Builder
	for i := 0; i < len(inner); i++ {
		switch inner[i] {
		case '\\':
			if i+1 < len(inner) {
				escaped.WriteByte('\\')
				escaped.WriteByte(inner[i+1])
				i++
				continue
			}
			escaped.WriteString(`\\`)
		case '"':
			escaped.WriteString(`\"`)
		default:
			escaped.WriteByte(inner[i])
		}
	}

	repaired := query[:open+1] + escaped.String() + query[end:]
	// The repair is only offered if it actually parses; otherwise the original
	// complaint is the honest answer.
	if malformedWatchSubscriptionString(repaired) {
		return "", false
	}
	return repaired, true
}

// watchSubscriptionStringEnd finds the quote that closes the subscription value.
//
// Two conditions identify it together, and neither is sufficient alone. The
// remainder has to continue the input object, which rules out quotes sitting mid
// subscription before a bare word. And the remainder has to carry an even number of
// unescaped quotes, which rules out quotes inside the subscription that happen to
// be followed by a brace — those leave one quote unpaired in what follows. The
// earliest position satisfying both is the boundary, for a malformed mutation and
// an already-correct one alike.
func watchSubscriptionStringEnd(query string, open int) (int, bool) {
	for i := open + 1; i < len(query); i++ {
		if query[i] != '"' {
			continue
		}
		if i > 0 && query[i-1] == '\\' && !escapedBackslashRun(query, i-1) {
			continue
		}
		remainder := query[i+1:]
		if closesInputStringValue(remainder) && unescapedQuoteCount(remainder)%2 == 0 {
			return i, true
		}
	}
	return 0, false
}

// unescapedQuoteCount counts quotes that would open or close a string value.
func unescapedQuoteCount(value string) int {
	count := 0
	for i := 0; i < len(value); i++ {
		switch value[i] {
		case '\\':
			i++
		case '"':
			count++
		}
	}
	return count
}

// escapedBackslashRun reports whether the backslash at index is itself escaped, so
// a value ending in a literal backslash is not mistaken for an escaped quote.
func escapedBackslashRun(query string, index int) bool {
	count := 0
	for i := index; i >= 0 && query[i] == '\\'; i-- {
		count++
	}
	return count%2 == 0
}
