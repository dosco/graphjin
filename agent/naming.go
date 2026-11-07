package agent

import (
	"fmt"
	"sort"
	"strings"
)

// namingRefusal returns the error for a result whose rows carry keys without
// the names the schema has for them, or "" when the result names its rows.
// It reads the naming notice from the result's JSON shape, so it serves the
// in-process runtime and the server runtime alike.
//
// When every unnamed list is a root of the query, the error carries the query
// with the names added. Told only which selection to add, the model went back
// to the catalog, whose table card lists columns and not relationships, and
// guessed a sibling table instead; a ready query is what the file-key and
// unknown-column repairs hand over for the same reason.
func namingRefusal(query string, out any) string {
	naming := mapValue(mapValue(out)["naming"])
	if naming == nil {
		return ""
	}
	var fixes []string
	var gaps []namingSelection
	for _, item := range anySlice(naming["rows"]) {
		row := mapValue(item)
		if row == nil {
			continue
		}
		path := stringFromMap(row, "path")
		key, name := stringFromMap(row, "key_column"), stringFromMap(row, "name_column")
		if path == "" || key == "" || name == "" {
			continue
		}
		selection := name
		if rel := stringFromMap(row, "relationship"); rel != "" {
			selection = rel + " { " + name + " }"
			fixes = append(fixes, fmt.Sprintf("select %s inside %s (it has only %s)", selection, path, key))
		} else {
			fixes = append(fixes, fmt.Sprintf("select %s in %s (it has only %s)", name, path, key))
		}
		gaps = append(gaps, namingSelection{path: path, selection: selection})
	}
	if len(fixes) == 0 {
		return ""
	}
	// The repeat goes through (one refusal per run), but the message does not
	// offer it: offered, the shortcut was taken over the fix.
	msg := "not answered yet: " + strings.Join(fixes, "; ")
	if repaired, ok := addNamingSelections(query, gaps); ok {
		msg += ". Execute this corrected query exactly as given: " + repaired
	} else {
		msg += ", then run the query again"
	}
	return msg + ". An id names nothing to the user, and the answer can state only what the query selected"
}

type namingSelection struct {
	path      string
	selection string
}

// addNamingSelections inserts each selection at the end of its root field's
// selection set. It succeeds only when every gap is at a root the query
// selects with a body, so a corrected query never fixes half the rows.
func addNamingSelections(query string, gaps []namingSelection) (string, bool) {
	type insertion struct {
		at   int
		text string
	}
	clean := graphQLStructure(query)
	bodies := rootSelectionBodies(clean)
	var inserts []insertion
	for _, gap := range gaps {
		if strings.Contains(gap.path, ".") {
			return "", false
		}
		closing, ok := bodies[gap.path]
		if !ok {
			return "", false
		}
		inserts = append(inserts, insertion{at: closing, text: " " + gap.selection + " "})
	}
	sort.SliceStable(inserts, func(i, j int) bool { return inserts[i].at > inserts[j].at })
	repaired := query
	for _, ins := range inserts {
		repaired = repaired[:ins.at] + ins.text + repaired[ins.at:]
	}
	if !graphQLDelimitersBalance(repaired) {
		return "", false
	}
	return strings.Join(strings.Fields(repaired), " "), true
}

// rootSelectionBodies maps each root field's response key (its alias when it
// has one) to the index of the closing brace of its selection set, in a query
// whose strings and comments graphQLStructure has blanked.
func rootSelectionBodies(clean string) map[string]int {
	out := map[string]int{}
	open := strings.IndexByte(clean, '{')
	if open < 0 {
		return out
	}
	// Skip an operation's variable definitions: "query ($d: String!) {".
	if paren := strings.IndexByte(clean, '('); paren >= 0 && paren < open {
		end := matchingGraphQLDelimiter(clean, paren, '(', ')')
		if end < 0 {
			return out
		}
		open = strings.IndexByte(clean[end:], '{')
		if open < 0 {
			return out
		}
		open += end
	}
	end := matchingGraphQLDelimiter(clean, open, '{', '}')
	if end < 0 {
		return out
	}
	for i := skipGraphQLSpace(clean, open+1); i < end; i = skipGraphQLSpace(clean, i) {
		name, next := graphQLName(clean, i)
		if name == "" {
			i++
			continue
		}
		key := name
		i = skipGraphQLSpace(clean, next)
		if i < end && clean[i] == ':' {
			_, next = graphQLName(clean, skipGraphQLSpace(clean, i+1))
			i = skipGraphQLSpace(clean, next)
		}
		if i < end && clean[i] == '(' {
			if i = matchingGraphQLDelimiter(clean, i, '(', ')'); i < 0 {
				return out
			}
			i = skipGraphQLSpace(clean, i+1)
		}
		for i < end && clean[i] == '@' { // directives
			_, next = graphQLName(clean, i+1)
			i = skipGraphQLSpace(clean, next)
			if i < end && clean[i] == '(' {
				if i = matchingGraphQLDelimiter(clean, i, '(', ')'); i < 0 {
					return out
				}
				i = skipGraphQLSpace(clean, i+1)
			}
		}
		if i < end && clean[i] == '{' {
			closing := matchingGraphQLDelimiter(clean, i, '{', '}')
			if closing < 0 {
				return out
			}
			out[key] = closing
			i = closing + 1
		}
	}
	return out
}

func graphQLName(value string, at int) (string, int) {
	end := at
	for end < len(value) {
		c := value[end]
		if c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (end > at && c >= '0' && c <= '9') {
			end++
			continue
		}
		break
	}
	return value[at:end], end
}

func graphQLDelimitersBalance(query string) bool {
	clean := graphQLStructure(query)
	closers := map[byte]byte{'}': '{', ')': '(', ']': '['}
	var stack []byte
	for i := 0; i < len(clean); i++ {
		switch c := clean[i]; c {
		case '{', '(', '[':
			stack = append(stack, c)
		case '}', ')', ']':
			if len(stack) == 0 || stack[len(stack)-1] != closers[c] {
				return false
			}
			stack = stack[:len(stack)-1]
		}
	}
	return len(stack) == 0
}
