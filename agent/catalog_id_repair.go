package agent

import (
	"sort"
	"strings"
)

// Catalog ids carry their database and schema: table:app:main.accounts, not
// table:accounts. Models write the short form constantly, and until now the miss
// returned a directive — "use a known catalog id instead of guessing another id" —
// alongside a list to scan.
//
// Benchmark generation 2028.1 shows what that costs. A missed detail leaves the
// following execute_graphql without discovery evidence, so it is refused; the
// refusal envelope has no data field; and executor code that assumed res.data
// throws a TypeError instead of reading the recovery. One qualifier typo becomes a
// dead run. Across that run 71 of 339 episodes hit an executor TypeError, several
// hundred occurrences deep, because the step repeats.
//
// Of the missed ids measured there, 29% differ from a real card only by their
// qualifiers and are unambiguous. Those are resolved rather than reported. The rest
// name entities that do not exist — table:tickets where the table is
// support_tickets, or table:harborlight_systems, which is a row value mistaken for
// a table — and those get named candidates instead of a scan list.

const catalogIDSuggestionLimit = 3

// splitCatalogID separates an id's kind from its remainder. Ids are
// kind:qualifier.name, and only the kind is a fixed vocabulary.
func splitCatalogID(id string) (kind, rest string, ok bool) {
	trimmed := strings.TrimSpace(id)
	index := strings.Index(trimmed, ":")
	if index <= 0 || index == len(trimmed)-1 {
		return "", "", false
	}
	return strings.ToLower(trimmed[:index]), strings.ToLower(trimmed[index+1:]), true
}

// catalogIDLeaf returns the unqualified entity name: app:main.accounts -> accounts.
func catalogIDLeaf(rest string) string {
	if index := strings.LastIndex(rest, "."); index >= 0 && index < len(rest)-1 {
		return rest[index+1:]
	}
	if index := strings.LastIndex(rest, ":"); index >= 0 && index < len(rest)-1 {
		return rest[index+1:]
	}
	return rest
}

// resolveCatalogIDQualifier returns the one published id that a missed id names
// once qualifiers are ignored. It resolves only when exactly one card matches, so
// an id that could mean two tables in different schemas is never silently picked.
func resolveCatalogIDQualifier(missed string, known []string) (string, bool) {
	kind, rest, ok := splitCatalogID(missed)
	if !ok {
		return "", false
	}
	var matches []string
	for _, candidate := range known {
		candidateKind, candidateRest, ok := splitCatalogID(candidate)
		if !ok || candidateKind != kind {
			continue
		}
		if candidateRest == rest {
			// An exact match differing only in case is already served by the caller;
			// treat it as resolved so the retry is a no-op rather than a miss.
			matches = appendUniqueString(matches, candidate)
			continue
		}
		if catalogIDLeaf(candidateRest) == rest {
			matches = appendUniqueString(matches, candidate)
		}
	}
	if len(matches) != 1 {
		return "", false
	}
	return matches[0], true
}

// suggestCatalogIDs names the closest published ids for a missed id that no
// qualifier fix reaches. Suggestions are ordered by how much of the requested name
// they share, and are offered as candidates rather than asserted as the answer.
func suggestCatalogIDs(missed string, known []string, limit int) []string {
	kind, rest, ok := splitCatalogID(missed)
	if !ok || limit <= 0 {
		return nil
	}
	leaf := catalogIDLeaf(rest)
	wanted := nameTokens(leaf)
	if len(wanted) == 0 {
		return nil
	}
	type scored struct {
		id    string
		score int
	}
	var ranked []scored
	for _, candidate := range known {
		candidateKind, candidateRest, ok := splitCatalogID(candidate)
		if !ok {
			continue
		}
		candidateLeaf := catalogIDLeaf(candidateRest)
		score := 0
		if candidateKind == kind {
			// Same kind is the strongest signal available: a missing table is far
			// likelier to be another table than a saved query of the same name.
			score += 2
		}
		if strings.Contains(candidateLeaf, leaf) || strings.Contains(leaf, candidateLeaf) {
			score += 3
		}
		for _, token := range nameTokens(candidateLeaf) {
			for _, want := range wanted {
				if token == want {
					score += 2
				} else if len(want) > 3 && strings.Contains(token, want) {
					score++
				}
			}
		}
		if score < 3 {
			continue
		}
		ranked = append(ranked, scored{id: candidate, score: score})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].id < ranked[j].id
	})
	out := make([]string, 0, limit)
	for _, item := range ranked {
		out = appendUniqueString(out, item.id)
		if len(out) == limit {
			break
		}
	}
	return out
}

func nameTokens(name string) []string {
	fields := strings.FieldsFunc(strings.ToLower(name), func(r rune) bool {
		return r == '_' || r == '-' || r == '.' || r == ' '
	})
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if len(field) < 2 {
			continue
		}
		out = appendUniqueString(out, field)
	}
	return out
}

// repairCatalogDetailIDs maps every missed id onto the published id it named.
// It reports ok only when all of them resolve, so a partly-guessed batch is
// reported rather than half-served.
func repairCatalogDetailIDs(missedIDs, known []string) (map[string]string, bool) {
	if len(missedIDs) == 0 || len(known) == 0 {
		return nil, false
	}
	out := make(map[string]string, len(missedIDs))
	for _, missed := range missedIDs {
		canonical, ok := resolveCatalogIDQualifier(missed, known)
		if !ok {
			return nil, false
		}
		out[missed] = canonical
	}
	return out, true
}

// catalogIDSuggestions collects candidates for the ids that could not be resolved,
// keyed by the id the caller asked for.
func catalogIDSuggestions(missedIDs, known []string) map[string][]string {
	out := map[string][]string{}
	for _, missed := range missedIDs {
		if candidates := suggestCatalogIDs(missed, known, catalogIDSuggestionLimit); len(candidates) != 0 {
			out[missed] = candidates
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// rewriteCatalogIDArgs returns a copy of args with requested ids replaced by their
// canonical form, leaving every other argument untouched.
func rewriteCatalogIDArgs(args map[string]any, repairs map[string]string) map[string]any {
	if len(repairs) == 0 {
		return args
	}
	out := make(map[string]any, len(args))
	for key, value := range args {
		out[key] = value
	}
	canonical := func(value any) any {
		text, ok := value.(string)
		if !ok {
			return value
		}
		if fixed, ok := repairs[strings.TrimSpace(text)]; ok {
			return fixed
		}
		return value
	}
	if value, ok := out["id"]; ok {
		out["id"] = canonical(value)
	}
	for _, key := range []string{"ids", "detail_ids"} {
		list, ok := out[key].([]any)
		if !ok {
			continue
		}
		fixed := make([]any, len(list))
		for i, item := range list {
			fixed[i] = canonical(item)
		}
		out[key] = fixed
	}
	return out
}
