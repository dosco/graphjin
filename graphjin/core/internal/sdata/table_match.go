package sdata

import (
	"sort"
	"strings"
)

// MaxTableSuggestions caps how many names a "did you mean" offers. A short list
// gets acted on; a long one gets skimmed and the name guessed again.
const MaxTableSuggestions = 3

// MatchTableNames returns the candidates whose names are related to want,
// sorted and capped, for a "did you mean" clause.
//
// Matching is deliberately loose rather than a spelling distance, because the
// misses worth catching are not misspellings: a model reading "the support SLA
// policy file" asks for `policies` when the table is `sla_policies`, and a
// model naming one row asks for `ticket` when it is `support_tickets`.
// Substring and suffix cover exactly that, so a dropped, added or singularised
// qualifier still resolves to the real name.
//
// It lives here because both the compiler and the public API need the same
// rule, and they cannot import each other.
func MatchTableNames(want string, candidates []string) []string {
	want = strings.ToLower(strings.TrimSpace(want))
	if want == "" {
		return nil
	}
	var names []string
	seen := make(map[string]bool)
	for _, candidate := range candidates {
		name := strings.ToLower(candidate)
		if !strings.Contains(name, want) && !strings.Contains(want, name) && !strings.HasSuffix(name, "_"+want) {
			continue
		}
		if seen[candidate] {
			continue
		}
		seen[candidate] = true
		names = append(names, candidate)
	}
	sort.Strings(names)
	if len(names) > MaxTableSuggestions {
		names = names[:MaxTableSuggestions]
	}
	return names
}

// DidYouMeanClause renders suggestions as an error suffix, and renders nothing
// when there are none — an error trailing off into "did you mean?" with no
// names is worse than one that stops.
func DidYouMeanClause(suggestions []string) string {
	switch len(suggestions) {
	case 0:
		return ""
	case 1:
		return "; did you mean \"" + suggestions[0] + "\"?"
	default:
		return "; did you mean one of [\"" + strings.Join(suggestions, "\" \"") + "\"]?"
	}
}
