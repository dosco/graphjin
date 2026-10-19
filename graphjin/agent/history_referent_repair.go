package agent

import (
	"context"
	"fmt"
	"strings"
)

// The referent guard's refusal came with an escape hatch: "if the retained
// subject genuinely does not apply, execute the unscoped query again and it
// will run." Measured across the c3-r2 benchmark run, that hatch is the first
// thing a weak model reaches for — four of the six stable multi-turn losses
// are the guard refusing an unscoped count, the model resending the identical
// query, the let-through executing it, and the model reporting the whole
// table's number as the retained subject's. Asked how many failed invoices
// account 1 has, the user is told 3 — the count for every account in the
// database — with full confidence.
//
// The binding the model kept dropping is mechanical. The retained subject
// names an entity and an id; the query names a root; when the root IS that
// entity's table the filter is id = N, and when the root carries exactly one
// <entity>_id column the filter is that column = N. Both facts come from the
// catalog, not from guessing. So when the binding is certain, the query is
// rewritten and executed — the remote-join precedent: the subject is the
// user's own, stated in their turns; only the route into the schema was ever
// ours to fix. Anything less certain keeps the old refusal, escape hatch and
// all, because a run must never be stranded on a guard that guessed wrong.

// referentBinding is the mechanical scope a rewrite applied: which retained
// subject, on which root, through which column.
type referentBinding struct {
	Ref    entityReference
	Root   string
	Column string
}

// referentScopedRewrite rewrites an unscoped follow-up query onto its retained
// subject when exactly one certain binding exists. ok is false whenever the
// binding or the splice is anything short of certain.
func (r *protocolRuntime) referentScopedRewrite(ctx context.Context, query string, refs []entityReference) (string, referentBinding, bool) {
	roots := QueryRootFields(query)
	if len(roots) != 1 {
		// Multi-root queries would need one binding decision per root; refuse
		// the whole rewrite rather than scope half an answer.
		return "", referentBinding{}, false
	}
	root := roots[0]
	columns := r.observedColumnNames(ctx, root)
	if len(columns) == 0 {
		return "", referentBinding{}, false
	}
	for _, ref := range refs {
		column, ok := referentBindingColumn(root, columns, ref)
		if !ok {
			continue
		}
		rewritten, spliced := spliceReferentFilter(query, root, column, ref.ID)
		if !spliced || rewritten == query {
			return "", referentBinding{}, false
		}
		return rewritten, referentBinding{Ref: ref, Root: root, Column: column}, true
	}
	return "", referentBinding{}, false
}

// referentBindingColumn finds the one column that binds the retained subject
// to this root: the root's own id when the root is the subject's table, or the
// root's single <entity>_id foreign key. Two candidates, or none, refuse.
func referentBindingColumn(root string, columns []string, ref entityReference) (string, bool) {
	entity := singularToken(ref.Entity)
	if entity == "" {
		return "", false
	}
	known := map[string]string{}
	for _, column := range columns {
		known[strings.ToLower(strings.TrimSpace(column))] = strings.TrimSpace(column)
	}
	rootIsEntity := false
	for _, token := range strings.Split(strings.ToLower(strings.TrimSpace(root)), "_") {
		if singularToken(token) == entity {
			rootIsEntity = true
			break
		}
	}
	if rootIsEntity {
		if id, ok := known["id"]; ok {
			return id, true
		}
		return "", false
	}
	if fk, ok := known[entity+"_id"]; ok {
		return fk, true
	}
	return "", false
}

func singularToken(token string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(token)), "s")
}

// spliceReferentFilter injects `column: {eq: id}` into the root's where
// clause: merged into an existing where object (sibling fields AND together),
// added to existing arguments, or as the root's first argument list.
func spliceReferentFilter(query, root, column, id string) (string, bool) {
	clean := graphQLStructure(query)
	lower := strings.ToLower(clean)
	target := strings.ToLower(root)
	filter := fmt.Sprintf("%s: {eq: %s}", column, id)
	for start := 0; start < len(clean); {
		index := strings.Index(lower[start:], target)
		if index < 0 {
			return "", false
		}
		index += start
		end := index + len(target)
		if (index > 0 && isGraphQLNameContinue(clean[index-1])) || (end < len(clean) && isGraphQLNameContinue(clean[end])) {
			start = end
			continue
		}
		open := skipGraphQLSpace(clean, end)
		if open < len(clean) && clean[open] == '(' {
			close := matchingGraphQLDelimiter(clean, open, '(', ')')
			if close <= open {
				return "", false
			}
			if from, _, ok := graphQLNamedObject(clean, "where", open+1, close); ok {
				// from is the first byte inside the where object's braces.
				return query[:from] + filter + ", " + query[from:], true
			}
			return query[:open+1] + "where: {" + filter + "}, " + query[open+1:], true
		}
		if open < len(clean) && clean[open] == '{' {
			return query[:end] + "(where: {" + filter + "})" + query[end:], true
		}
		return "", false
	}
	return "", false
}

// referentBindableToKnownTable reports whether any retained subject names a
// table this run knows about. It decides whether the saved-query refusal may
// stand past the strike cap: a subject the schema can bind means the scoped
// raw query the refusal asks for actually exists, so letting the unscoped
// saved query through would trade a reachable right answer for a wrong one.
func (s *discoveryState) referentBindableToKnownTable(refs []entityReference) bool {
	if s == nil {
		return false
	}
	for _, ref := range refs {
		entity := singularToken(ref.Entity)
		if entity == "" {
			continue
		}
		for _, candidate := range []string{entity, entity + "s", entity + "es"} {
			if s.catalogIDForTable(candidate) != "" {
				return true
			}
			if _, ok := s.tableColumnNames[candidate]; ok {
				return true
			}
		}
	}
	return false
}

// attachReferentBindingNotice tells the model what it is looking at: the query
// executed scoped to the retained subject, so the rows below are that
// subject's, and future queries should carry the same filter.
func attachReferentBindingNotice(out any, rewritten string, binding referentBinding) any {
	instruction := fmt.Sprintf("This follow-up inherits its subject from prior turns, so the query executed scoped to %s via %s.%s, as shown in repaired_query. The results below are for that subject only; keep the same filter on further queries about it.", binding.Ref.String(), binding.Root, binding.Column)
	recovery := map[string]any{
		"kind":           "history_referent_bound",
		"code":           "history_referent_bound",
		"instruction":    instruction,
		"repaired_query": rewritten,
	}
	switch res := out.(type) {
	case executeResult:
		res.Recovery = recovery
		res.Guidance = instruction
		return res
	case *executeResult:
		res.Recovery = recovery
		res.Guidance = instruction
		return res
	case map[string]any:
		res = cloneAnyMap(res)
		res["recovery"] = recovery
		res["guidance"] = instruction
		return res
	default:
		return attachNoticeToForeignResult(out, recovery, instruction)
	}
}
