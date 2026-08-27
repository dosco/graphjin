package sdata

import (
	"sort"
	"strconv"
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

// maxListedTables bounds the table list in a missing-table error. A large
// schema would otherwise put hundreds of names in front of a model that needs
// one, and an arbitrary prefix is rarely the one it wants — so the count of
// what is hidden matters as much as the names shown.
const maxListedTables = 24

// TableHint is the whole suffix for a table that was not found: the suggestion
// when a name is close, and otherwise the names that do exist.
//
// A near miss is a spelling problem and MatchTableNames solves it. The other
// failure is a synonym — semantic catalog search leads a model to the right
// subject under a word the schema does not use, `companies` or `organizations`
// for `accounts` — and no edit distance bridges that. Measured on a paired run,
// 82% of missing-table errors under semantic search carried no suggestion at
// all, against 36% without it, and the model spent its budget re-guessing.
// Naming the real tables is the only thing left that helps, and it is what the
// column path already does.
func TableHint(want string, candidates []string) string {
	if hint := DidYouMeanClause(MatchTableNames(want, candidates)); hint != "" {
		// A suggestion is the answer, so it travels alone.
		return hint
	}
	names := make([]string, 0, len(candidates))
	seen := make(map[string]bool, len(candidates))
	for _, name := range candidates {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	if len(names) == 0 {
		return ""
	}
	sort.Strings(names)
	if len(names) > maxListedTables {
		return "; available tables include: " + strings.Join(names[:maxListedTables], ", ") +
			" (" + strconv.Itoa(len(names)-maxListedTables) + " more)"
	}
	return "; available tables: " + strings.Join(names, ", ")
}
