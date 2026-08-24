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

// A confirmation turn names nothing. "Yes, go ahead and set that up." is the
// whole request, and the seed searches the catalog with it — which returns
// generic cards and no idea what is being set up. Every recorded confirmation
// episode then invented a table that does not exist (a watch over `orders` in
// a schema whose tables are invoices, accounts, support_tickets) and spent its
// entire step budget looping on it, 0 of 6 across two benchmark runs.
//
// What the run is actually about is one turn back, in the proposal the user is
// answering. So when the current turn carries no subject of its own, the seed
// searches the conversation instead of the acknowledgement. The detector is
// deliberately narrow: it fires only when nothing but confirmation and filler
// words remain, so every instruction that names anything at all keeps today's
// behaviour exactly.

// confirmationWords are the acknowledgement and filler tokens a bare
// confirmation is built from. A turn made only of these asks for something the
// turn itself never names.
var confirmationWords = map[string]bool{
	"yes": true, "yeah": true, "yep": true, "yup": true, "sure": true, "ok": true,
	"okay": true, "alright": true, "please": true, "go": true, "ahead": true,
	"do": true, "doit": true, "it": true, "that": true, "this": true, "these": true,
	"those": true, "them": true, "set": true, "up": true, "thanks": true,
	"thank": true, "you": true, "confirm": true, "confirmed": true, "proceed": true,
	"continue": true, "sounds": true, "good": true, "great": true, "perfect": true,
	"fine": true, "works": true, "correct": true, "right": true, "agreed": true,
	"affirmative": true, "now": true, "then": true, "and": true, "the": true,
	"a": true, "an": true, "for": true, "me": true, "us": true, "we": true,
	"i": true, "lets": true, "let": true, "make": true, "create": true, "one": true,
}

// instructionCarriesNoSubject reports whether an instruction is built entirely
// from acknowledgement and filler, naming nothing that could be searched for.
func instructionCarriesNoSubject(instruction string) bool {
	trimmed := strings.TrimSpace(instruction)
	if trimmed == "" {
		return false
	}
	if instructionNamesOwnSubject(trimmed) {
		return false
	}
	words := 0
	for _, field := range strings.FieldsFunc(strings.ToLower(trimmed), func(r rune) bool {
		return !('a' <= r && r <= 'z') && !('0' <= r && r <= '9')
	}) {
		if field == "" {
			continue
		}
		words++
		if !confirmationWords[field] {
			return false
		}
	}
	return words != 0
}

// seedSearchText returns the text the initial catalog search should run on:
// the instruction itself whenever it names anything, and otherwise the most
// recent prior turns that do. Only the seed's search text changes — the
// instruction stays the instruction everywhere else, because every other guard
// reads it to decide what the caller actually asked for.
func seedSearchText(instruction string, history []Turn) string {
	if !instructionCarriesNoSubject(instruction) {
		return instruction
	}
	var parts []string
	for i := len(history) - 1; i >= 0 && len(parts) < 2; i-- {
		content := strings.TrimSpace(history[i].Content)
		if content == "" || instructionCarriesNoSubject(content) {
			continue
		}
		parts = append([]string{content}, parts...)
	}
	if len(parts) == 0 {
		return instruction
	}
	// The retained context leads: it is the part that names tables, filters and
	// actions, and the search ranks on what it reads first.
	combined := strings.Join(append(parts, strings.TrimSpace(instruction)), " ")
	if len(combined) > seedSearchTextLimit {
		combined = strings.TrimSpace(combined[:seedSearchTextLimit])
	}
	return combined
}

// seedSearchTextLimit keeps the widened seed search close to the length of an
// ordinary one-sentence instruction; the seed ranks on phrasing, and pasting a
// whole transcript into it would dilute every term.
const seedSearchTextLimit = 400
