package agent

import (
	"sort"
	"strings"
)

// A failed write is the worst place to leave a model alone with an error
// message. Across the round-2 A/B, ten of fifteen action tasks were lost the
// same way in every repeat: the model invented a column name — payment_reference
// for reference, paid_at for recorded_at, notes for resolution_note — the
// engine rejected the write, and the model either resent the identical mutation
// until its step budget died or narrated success over a write that never
// happened. The catalog card with the real names had been read moments earlier;
// the error listed them again; neither moved the model.
//
// The value guard learned this lesson first: naming the exact answer is the
// only repair that measurably changes what a weak model does. So when every
// unknown key in a failed write resolves to exactly one real column, the
// corrected mutation is computed and handed over whole. It is offered, never
// executed — the column is our inference, and a write is not a place to act on
// inference — but the finalize gate refuses to accept "answered" until a write
// lands, so the offer arrives with leverage the join repair never had.

// mutationColumnRename is one unknown-key correction inside a failed write.
type mutationColumnRename struct {
	From, To string
}

// repairUnknownMutationColumns rewrites a failed mutation so that every
// depth-0 key of its insert/update/upsert blocks names a real column, when and
// only when each unknown key has exactly one plausible real column. Selection
// fields are renamed by the same map, and selection fields that remain unknown
// are pruned so the offered text executes as given. ok is false whenever any
// part of that cannot be done with certainty.
func repairUnknownMutationColumns(query string, columns []string) (string, []mutationColumnRename, bool) {
	if strings.TrimSpace(query) == "" || len(columns) == 0 || !ContainsMutationOperation(query) {
		return "", nil, false
	}
	clean := graphQLStructure(query)
	known := map[string]string{}
	for _, column := range columns {
		known[strings.ToLower(strings.TrimSpace(column))] = strings.TrimSpace(column)
	}

	// Collect the depth-0 keys of every write block, with spans.
	type keySpan struct {
		name       string
		start, end int
	}
	var keys []keySpan
	used := map[string]bool{}
	blocks := 0
	for _, root := range MutationRootFields(query) {
		for _, keyword := range []string{"insert", "update", "upsert"} {
			for _, span := range mutationInputBlocks(clean, root, keyword) {
				if span[0] < 0 || span[1] > len(clean) || span[0] >= span[1] {
					continue
				}
				blocks++
				body := clean[span[0]:span[1]]
				// A nested object value is a related-row write whose keys belong
				// to another table; renaming around one risks a wrong repair, so
				// the whole mutation is left alone.
				if strings.Contains(body, "{") {
					return "", nil, false
				}
				for _, k := range graphQLTopLevelKeys(body) {
					keys = append(keys, keySpan{name: k.name, start: span[0] + k.start, end: span[0] + k.end})
					used[strings.ToLower(k.name)] = true
				}
			}
		}
	}
	if blocks == 0 || len(keys) == 0 {
		return "", nil, false
	}

	renames := map[string]string{}
	for _, key := range keys {
		fold := strings.ToLower(key.name)
		if _, ok := known[fold]; ok {
			continue
		}
		candidate, ok := nearestColumn(key.name, columns, used)
		if !ok {
			// One unresolvable key makes the whole repair uncertain, and an
			// uncertain repair labelled "execute exactly as given" is the
			// round-2 mistake in new clothes.
			return "", nil, false
		}
		renames[fold] = candidate
		// The assigned column is spoken for: a second unknown key resolving to
		// the same place would write one value over the other.
		used[strings.ToLower(candidate)] = true
	}
	if len(renames) == 0 {
		return "", nil, false
	}

	repaired := renameGraphQLIdentifiers(query, renames)
	repaired = pruneUnknownSelectionFields(repaired, known)
	if strings.TrimSpace(repaired) == "" || repaired == query {
		return "", nil, false
	}
	out := make([]mutationColumnRename, 0, len(renames))
	for from, to := range renames {
		out = append(out, mutationColumnRename{From: from, To: to})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].From < out[j].From })
	return repaired, out, true
}

// graphQLTopLevelKeys returns the depth-0 `name:` keys of an object body whose
// strings have already been blanked, with byte spans relative to the body.
func graphQLTopLevelKeys(body string) []struct {
	name       string
	start, end int
} {
	var out []struct {
		name       string
		start, end int
	}
	depth := 0
	for i := 0; i < len(body); i++ {
		switch c := body[i]; {
		case c == '{' || c == '[' || c == '(':
			depth++
		case c == '}' || c == ']' || c == ')':
			depth--
		case depth == 0 && isGraphQLNameContinue(c):
			start := i
			for i < len(body) && isGraphQLNameContinue(body[i]) {
				i++
			}
			next := skipGraphQLSpace(body, i)
			if next < len(body) && body[next] == ':' {
				out = append(out, struct {
					name       string
					start, end int
				}{name: body[start:i], start: start, end: i})
			}
			i--
		}
	}
	return out
}

// nearestColumn finds the single real column an invented name was reaching
// for. Matching is deliberately narrow: the folded underscore-token set of one
// name must contain the other's, with plural s stripped — payment_reference
// carries reference, notes is resolution_note's note, amount is a prefix of
// amount_cents. One weaker rung exists for timestamps: an unknown *_at key
// matches the table's only unused *_at column, which is how paid_at finds
// recorded_at. Anything with two plausible answers, or none, refuses.
func nearestColumn(unknown string, columns []string, used map[string]bool) (string, bool) {
	unknownTokens := foldedNameTokens(unknown)
	if len(unknownTokens) == 0 {
		return "", false
	}
	var strong []string
	for _, column := range columns {
		fold := strings.ToLower(strings.TrimSpace(column))
		if used[fold] {
			continue
		}
		columnTokens := foldedNameTokens(column)
		if tokenSubset(columnTokens, unknownTokens) || tokenSubset(unknownTokens, columnTokens) {
			strong = append(strong, strings.TrimSpace(column))
		}
	}
	if len(strong) == 1 {
		return strong[0], true
	}
	if len(strong) > 1 {
		return "", false
	}
	if strings.HasSuffix(strings.ToLower(unknown), "_at") {
		var stamps []string
		for _, column := range columns {
			fold := strings.ToLower(strings.TrimSpace(column))
			if !used[fold] && strings.HasSuffix(fold, "_at") {
				stamps = append(stamps, strings.TrimSpace(column))
			}
		}
		if len(stamps) == 1 {
			return stamps[0], true
		}
	}
	return "", false
}

func foldedNameTokens(name string) map[string]bool {
	out := map[string]bool{}
	for _, token := range strings.Split(strings.ToLower(strings.TrimSpace(name)), "_") {
		token = strings.TrimSuffix(token, "s")
		if token != "" {
			out[token] = true
		}
	}
	return out
}

func tokenSubset(inner, outer map[string]bool) bool {
	if len(inner) == 0 || len(inner) > len(outer) {
		return false
	}
	for token := range inner {
		if !outer[token] {
			return false
		}
	}
	return true
}

// renameGraphQLIdentifiers replaces whole-identifier occurrences outside
// string literals, so a rename fixes the write block and the selection set in
// one pass without ever touching a quoted value.
func renameGraphQLIdentifiers(query string, renames map[string]string) string {
	var out strings.Builder
	out.Grow(len(query))
	for i := 0; i < len(query); i++ {
		c := query[i]
		if c == '"' {
			start := i
			_, next := graphQLStringLiteral(query, i)
			if next <= i {
				out.WriteString(query[start:])
				break
			}
			out.WriteString(query[start:next])
			i = next - 1
			continue
		}
		if isGraphQLNameContinue(c) {
			start := i
			for i < len(query) && isGraphQLNameContinue(query[i]) {
				i++
			}
			word := query[start:i]
			if to, ok := renames[strings.ToLower(word)]; ok {
				out.WriteString(to)
			} else {
				out.WriteString(word)
			}
			i--
			continue
		}
		out.WriteByte(c)
	}
	return out.String()
}

// pruneUnknownSelectionFields drops selection-set fields that still name no
// real column after renaming, so the offered mutation cannot fail on its own
// read-back. Only flat fields are considered; a field opening a nested block
// is a relation and stays. An emptied selection falls back to the table's id
// column, or its first column.
func pruneUnknownSelectionFields(query string, known map[string]string) string {
	clean := graphQLStructure(query)
	// The selection block is the last top-level {...} of the root field: find
	// the root's argument close, then its block.
	for _, root := range MutationRootFields(query) {
		lower := strings.ToLower(clean)
		index := strings.Index(lower, strings.ToLower(root))
		for index >= 0 {
			end := index + len(root)
			if (index > 0 && isGraphQLNameContinue(clean[index-1])) || (end < len(clean) && isGraphQLNameContinue(clean[end])) {
				next := strings.Index(lower[end:], strings.ToLower(root))
				if next < 0 {
					index = -1
				} else {
					index = end + next
				}
				continue
			}
			open := skipGraphQLSpace(clean, end)
			if open < len(clean) && clean[open] == '(' {
				open = skipGraphQLSpace(clean, matchingGraphQLDelimiter(clean, open, '(', ')')+1)
			}
			if open >= len(clean) || clean[open] != '{' {
				break
			}
			close := matchingGraphQLDelimiter(clean, open, '{', '}')
			if close <= open {
				break
			}
			body := clean[open+1 : close]
			kept, changed := prunedSelection(body, query[open+1:close], known)
			if changed {
				query = query[:open+1] + " " + kept + " " + query[close:]
			}
			break
		}
	}
	return query
}

func prunedSelection(clean, raw string, known map[string]string) (string, bool) {
	var kept []string
	changed := false
	depth := 0
	for i := 0; i < len(clean); i++ {
		switch c := clean[i]; {
		case c == '{' || c == '(':
			depth++
		case c == '}' || c == ')':
			depth--
		case depth == 0 && isGraphQLNameContinue(c):
			start := i
			for i < len(clean) && isGraphQLNameContinue(clean[i]) {
				i++
			}
			name := raw[start:i]
			next := skipGraphQLSpace(clean, i)
			if next < len(clean) && (clean[next] == '{' || clean[next] == '(' || clean[next] == ':') {
				// A relation, parameterized field, or alias: out of scope, keep
				// the mutation unrepaired rather than guess at nested shapes.
				return raw, false
			}
			if _, ok := known[strings.ToLower(name)]; ok {
				kept = append(kept, name)
			} else {
				changed = true
			}
			i--
		}
	}
	if !changed {
		return raw, false
	}
	if len(kept) == 0 {
		if id, ok := known["id"]; ok {
			kept = []string{id}
		} else {
			names := make([]string, 0, len(known))
			for _, column := range known {
				names = append(names, column)
			}
			sort.Strings(names)
			if len(names) != 0 {
				kept = names[:1]
			}
		}
	}
	return strings.Join(kept, " "), true
}
