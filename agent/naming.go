package agent

import (
	"context"
	"sort"
	"strings"
)

// Rows a result identifies only by key get their names from GraphJin: the
// query is run again with the selection that names them, and that result is
// what the caller reads.
//
// The first such result used to be refused, with the corrected query in the
// error, because straight-line code finals in the block that ran the query
// and a notice on the value arrived too late. That held when the refused
// query was the answer. A look-up through a relationship is not one query: in
// routine runs (gpt-5.4-mini, 2026-11-12) the guard refused a query in 66 of
// 128 data-agent runs, most often the first hop (a ticket's account_id), and
// the run then took "execute this corrected query" for the whole task. Asked
// whether a ticket's account was at churn risk, a run saw the account's
// status in 6 of 24 where the guard fired and in 17 of 33 where it did not.
// One refusal that named a nested selection in words had the model swap the
// field it needed (status) for the one asked for (name).
//
// The fix was known exactly each time, so GraphJin applies it: nothing is
// refused, no step is spent, and the rows the code asked for come back with
// their names beside them.

// namingSelection is one selection that names rows: what to select, and the
// dotted path of response keys to the selection set it goes in.
type namingSelection struct {
	path      string
	selection string
	// table is the table the rows belong to; own says they are that table's
	// own rows, named by a column, rather than rows reached through a key.
	table string
	own   bool
}

// namingSelections reads the naming notice of a result: for each list of rows
// identified only by key, the selection that names them. It reads the
// notice from the result's JSON shape, so it serves the in-process runtime
// and the server runtime alike.
func namingSelections(out any) []namingSelection {
	naming := mapValue(mapValue(out)["naming"])
	if naming == nil {
		return nil
	}
	var gaps []namingSelection
	for _, item := range anySlice(naming["rows"]) {
		row := mapValue(item)
		if row == nil {
			continue
		}
		path, name := stringFromMap(row, "path"), stringFromMap(row, "name_column")
		if path == "" || name == "" || stringFromMap(row, "key_column") == "" {
			continue
		}
		gap := namingSelection{path: path, selection: name, table: stringFromMap(row, "table"), own: true}
		if rel := stringFromMap(row, "relationship"); rel != "" {
			gap.selection, gap.own = rel+" { "+name+" }", false
		}
		gaps = append(gaps, gap)
	}
	return withoutNamedBelow(gaps)
}

// withoutNamedBelow drops a selection through a key when the query already
// selects that table one level down and its rows there are being named: a
// ticket's account { id } needs name inside account, not a second join.
func withoutNamedBelow(gaps []namingSelection) []namingSelection {
	kept := gaps[:0:0]
	for _, gap := range gaps {
		redundant := false
		if !gap.own {
			for _, other := range gaps {
				if other.own && other.table == gap.table && strings.HasPrefix(other.path, gap.path+".") && !strings.Contains(other.path[len(gap.path)+1:], ".") {
					redundant = true
					break
				}
			}
		}
		if !redundant {
			kept = append(kept, gap)
		}
	}
	return kept
}

// nameRows returns the result of query with the names of the rows out
// identified only by key, and the arguments it ran. It reports false, and
// the caller keeps out, when no name can be added or the query with the
// names does not run.
func (r *protocolRuntime) nameRows(ctx context.Context, args map[string]any, query string, out any) (any, map[string]any, bool) {
	named, placed := addNamingSelections(query, namingSelections(out))
	if len(placed) == 0 {
		return nil, nil, false
	}
	namedArgs := cloneAnyMap(args)
	namedArgs["query"] = named
	again, err := r.base.ExecuteGraphQL(ctx, namedArgs)
	if err != nil || executionFailed(again) {
		return nil, nil, false
	}
	return again, namedArgs, true
}

// addNamingSelections inserts each selection at the end of the selection set
// its path leads to, and returns the query with the ones it could place. A
// path the query does not select with a body is left out.
func addNamingSelections(query string, gaps []namingSelection) (string, []namingSelection) {
	type insertion struct {
		at   int
		text string
	}
	clean := graphQLStructure(query)
	root, ok := operationBody(clean)
	if !ok {
		return query, nil
	}
	var inserts []insertion
	var placed []namingSelection
	for _, gap := range gaps {
		body, found := root, true
		for _, key := range strings.Split(gap.path, ".") {
			if body, found = selectionBodies(clean, body)[key]; !found {
				break
			}
		}
		if !found {
			continue
		}
		inserts = append(inserts, insertion{at: body.close, text: " " + gap.selection + " "})
		placed = append(placed, gap)
	}
	if len(placed) == 0 {
		return query, nil
	}
	sort.SliceStable(inserts, func(i, j int) bool { return inserts[i].at > inserts[j].at })
	repaired := query
	for _, ins := range inserts {
		repaired = repaired[:ins.at] + ins.text + repaired[ins.at:]
	}
	if !graphQLDelimitersBalance(repaired) {
		return query, nil
	}
	return strings.Join(strings.Fields(repaired), " "), placed
}

// selectionBody is a selection set: the indexes of its braces in a query
// whose strings and comments graphQLStructure has blanked.
type selectionBody struct{ open, close int }

// operationBody finds the operation's selection set.
func operationBody(clean string) (selectionBody, bool) {
	open := strings.IndexByte(clean, '{')
	if open < 0 {
		return selectionBody{}, false
	}
	// Skip an operation's variable definitions: "query ($d: String!) {".
	if paren := strings.IndexByte(clean, '('); paren >= 0 && paren < open {
		end := matchingGraphQLDelimiter(clean, paren, '(', ')')
		if end < 0 {
			return selectionBody{}, false
		}
		open = strings.IndexByte(clean[end:], '{')
		if open < 0 {
			return selectionBody{}, false
		}
		open += end
	}
	end := matchingGraphQLDelimiter(clean, open, '{', '}')
	if end < 0 {
		return selectionBody{}, false
	}
	return selectionBody{open: open, close: end}, true
}

// selectionBodies maps the response key (the alias when there is one) of each
// field in a selection set that has a selection set of its own to that set.
func selectionBodies(clean string, in selectionBody) map[string]selectionBody {
	out := map[string]selectionBody{}
	end := in.close
	for i := skipGraphQLSpace(clean, in.open+1); i < end; i = skipGraphQLSpace(clean, i) {
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
			out[key] = selectionBody{open: i, close: closing}
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
