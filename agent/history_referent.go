package agent

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Follow-up turns name their subject with a pronoun: "How many users belong to
// it?" after "Use Harborlight Systems, account 3." GraphJin delivers that history
// and the runtime guidance tells the model to resolve the reference, but benchmark
// generation 2028.1 measured what actually happens — the subject is dropped and
// the unscoped question is answered instead.
//
// Every multi-turn failure in that run has this shape. Asked for one account's
// failed invoices the agent counted every failed invoice; asked how many users
// belong to account 3 it counted all ten users. The tell is precise: filters named
// in the current turn survive, and only the filter that requires resolving a
// pronoun against history goes missing.
//
// The resolved subject is sitting in Go. Naming it is the same correction applied
// throughout this protocol: never hand the model a directive to work out something
// the engine already knows.

// entityReference is a concrete subject binding read out of prior turns, such as
// account 3 from "Use Harborlight Systems, account 3."
type entityReference struct {
	Entity string `json:"entity"`
	ID     string `json:"id"`
}

func (e entityReference) String() string { return e.Entity + " " + e.ID }

var entityReferencePattern = regexp.MustCompile(`(?i)\b([a-z][a-z_]{2,})\s+#?(\d{1,9})\b`)

// quantityWords precede a number without naming an entity. "top 5" and "last 30
// days" are shapes, not subjects, and must never be offered as a retained
// reference.
var quantityWords = map[string]struct{}{
	"top": {}, "first": {}, "last": {}, "limit": {}, "page": {}, "next": {}, "previous": {},
	"past": {}, "least": {}, "most": {}, "under": {}, "over": {}, "above": {}, "below": {},
	"than": {}, "than_or": {}, "least_of": {}, "count": {}, "total": {}, "sum": {}, "avg": {},
	"and": {}, "the": {}, "all": {}, "any": {}, "with": {}, "within": {}, "about": {},
	"exactly": {}, "least_the": {}, "row": {}, "rows": {}, "record": {}, "records": {},
	"version": {}, "step": {}, "turn": {}, "day": {}, "days": {}, "week": {}, "weeks": {},
	"month": {}, "months": {}, "year": {}, "years": {}, "hour": {}, "hours": {}, "minute": {},
	"minutes": {}, "percent": {}, "usd": {}, "cents": {}, "dollars": {},
}

// referentWords mark a subject the current turn declines to name. A follow-up
// carrying one of these has to be resolved against history before it can be
// filtered.
var referentPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(?:that|this|the same|said)\s+(?:one\s+)?[a-z_]{3,}\b`),
	regexp.MustCompile(`(?i)\b(?:it|its|it's|them|their|theirs|these|those)\b`),
	regexp.MustCompile(`(?i)\bbelong(?:s)?\s+to\s+(?:it|them|that)\b`),
}

// historyEntityReferences returns the concrete subjects prior turns established,
// most recent first, deduplicated. Only bindings a caller could act on are
// returned: a bare number with no entity word is not a subject.
func historyEntityReferences(history []Turn) []entityReference {
	seen := map[string]struct{}{}
	var out []entityReference
	// Recent turns bind the current subject; older ones are likelier to be stale.
	for i := len(history) - 1; i >= 0; i-- {
		for _, match := range entityReferencePattern.FindAllStringSubmatch(history[i].Content, -1) {
			entity := strings.ToLower(strings.Trim(match[1], "_"))
			if _, skip := quantityWords[entity]; skip || entity == "" {
				continue
			}
			ref := entityReference{Entity: entity, ID: match[2]}
			key := ref.String()
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, ref)
		}
	}
	if len(out) > 6 {
		out = out[:6]
	}
	return out
}

// instructionNamesOwnSubject reports whether the current instruction carries its
// own identifier. "What is account 5's MRR?" needs no history and must not be
// second-guessed.
func instructionNamesOwnSubject(instruction string) bool {
	for _, match := range entityReferencePattern.FindAllStringSubmatch(instruction, -1) {
		entity := strings.ToLower(strings.Trim(match[1], "_"))
		if _, skip := quantityWords[entity]; !skip && entity != "" {
			return true
		}
	}
	return false
}

// instructionDefersToHistory reports whether the instruction points at a subject
// it does not name.
func instructionDefersToHistory(instruction string) bool {
	trimmed := strings.TrimSpace(instruction)
	if trimmed == "" {
		return false
	}
	for _, pattern := range referentPatterns {
		if pattern.MatchString(trimmed) {
			return true
		}
	}
	return false
}

// queryBindsReference reports whether the operation already filters on one of the
// retained ids. The id may appear in the query text or in variables, and the check
// deliberately accepts any of them: binding one subject is proof the model
// resolved the reference, and which filter path is correct is the model's call.
func queryBindsReference(query string, args map[string]any, refs []entityReference) bool {
	haystack := query
	if len(args) != 0 {
		haystack += " " + fmt.Sprint(args)
	}
	for _, ref := range refs {
		if regexp.MustCompile(`(?:\b|:|=|\s)` + regexp.QuoteMeta(ref.ID) + `\b`).MatchString(haystack) {
			return true
		}
	}
	return false
}

// unresolvedHistoryReferent returns the retained subjects a read operation failed
// to bind, or nil when there is nothing to correct. It reports only what is
// certain: the instruction defers to history, history holds concrete subjects, and
// the authored query binds none of them.
func unresolvedHistoryReferent(instruction, query string, args map[string]any, history []Turn) []entityReference {
	if strings.TrimSpace(query) == "" || len(history) == 0 {
		return nil
	}
	if !instructionDefersToHistory(instruction) || instructionNamesOwnSubject(instruction) {
		return nil
	}
	refs := historyEntityReferences(history)
	if len(refs) == 0 || queryBindsReference(query, args, refs) {
		return nil
	}
	return refs
}

// describeEntityReferences renders retained subjects for the repair message in a
// stable order, so the same run produces the same text.
func describeEntityReferences(refs []entityReference) string {
	parts := make([]string, 0, len(refs))
	for _, ref := range refs {
		parts = append(parts, ref.String())
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}
