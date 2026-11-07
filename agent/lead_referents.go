package agent

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// A delegated question must stand alone: the data agent sees no conversation.
// On a follow-up the lead is told to resolve "their invoices" or "that plan"
// into names from the conversation, but its stages paraphrase the request
// between them, and the referent is lost on the way. A lead-corpus stage dump
// (2026-11-07, flash-lite) shows the context stage reading a history that
// named Meridian Robotics and still forwarding "What is the total amount of
// their failed invoices?". The data agent cannot know who "they" are, so it
// answers about nobody, or about every account.
//
// The lead holds the conversation, so it can tell when a follow-up question
// takes nothing from it. A question is held back, once per turn, when all of
// these hold:
//
//   - the turn has a conversation, and the user's message points into it with
//     a referring phrase ("their", "that plan");
//   - the question names no subject of its own: no proper name, number or
//     quoted value;
//   - the question uses none of the words the conversation adds beyond the
//     user's message, so nothing in it came from the turns that say what the
//     phrase means.
//
// That covers both ways the referent is lost: the phrase carried through
// as-is, and the phrase dropped so the question widens to every account
// ("What is the total amount of all failed invoices?"), which the replayed
// runs showed as often as the first.
//
// Asking again goes through: the check runs once per turn, so it can cost a
// step but never block a question the lead stands by.

var (
	leadReferentDeterminer = regexp.MustCompile(`(?i)\b(?:that|this|these|those|the same|said)\s+[a-z][a-z_-]{2,}\b`)
	leadReferentPronoun    = regexp.MustCompile(`(?i)\b(?:it|its|they|them|their|theirs|these|those|he|she|him|his|her|hers)\b`)
	// A title-case word after the first: "Meridian", not "MRR" or "What".
	leadQuestionProperName = regexp.MustCompile(`\s[A-Z][a-z]`)
	leadQuestionWord       = regexp.MustCompile(`[a-z0-9]+`)
)

// leadStopWords carry no subject; a question sharing only these with the
// conversation took nothing from it.
var leadStopWords = func() map[string]bool {
	words := strings.Fields(`a an the and or but nor of to in on at by for from with without into onto
		over under about above below between across after before since until during per via as than then
		is are was were be been being am do does did done have has had having will would shall should can
		could may might must not no yes all any each every some many much more most less least few other
		what which who whom whose when where why how whether if so such only also just very too again
		there here this that these those it its they them their theirs he she him his her hers we us our
		you your i me my mine one ones get got give show list tell find please let make made`)
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}()

type leadReferentCheck struct {
	instructionRefs map[string]bool
	historyWords    map[string]bool
	// names are the multi-word proper names the conversation mentions, most
	// recent first: what "their" most likely means, offered in the message.
	names []string
}

// leadConversationName is a run of two or more title-case words: "Meridian
// Robotics", "Harborlight Systems".
var leadConversationName = regexp.MustCompile(`\b[A-Z][a-z]+(?:\s+[A-Z][a-z]+)+\b`)

// conversationNames returns up to three proper names from the conversation,
// most recent turn first. A leading stop word ("The Meridian") is not part of
// a name.
func conversationNames(history []Turn) []string {
	var out []string
	seen := map[string]bool{}
	for i := len(history) - 1; i >= 0 && len(out) < 3; i-- {
		content := history[i].Content
		for _, loc := range leadConversationName.FindAllStringIndex(content, -1) {
			words := strings.Fields(content[loc[0]:loc[1]])
			for len(words) != 0 && leadStopWords[strings.ToLower(words[0])] {
				words = words[1:]
			}
			// A sentence's first word is capitalized anyway: "Compare Solvex
			// Dynamics" is a verb and a name.
			if len(words) >= 3 && sentenceStart(content, loc[0]) {
				words = words[1:]
			}
			if len(words) < 2 {
				continue
			}
			name := strings.Join(words, " ")
			if !seen[name] && len(out) < 3 {
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	return out
}

// newLeadReferentCheck returns nil when there is nothing to check: no
// conversation, or a user message that refers to nothing.
func newLeadReferentCheck(instruction string, history []Turn) *leadReferentCheck {
	history = normalizeHistory(history)
	if len(history) == 0 {
		return nil
	}
	refs := leadReferringPhrases(instruction)
	if len(refs) == 0 {
		return nil
	}
	instructionWords := leadContentWords(instruction)
	added := map[string]bool{}
	for _, turn := range history {
		for w := range leadContentWords(turn.Content) {
			if !instructionWords[w] {
				added[w] = true
			}
		}
	}
	return &leadReferentCheck{instructionRefs: refs, historyWords: added, names: conversationNames(history)}
}

// unresolved reports a question that takes nothing from the conversation the
// user's message points at: it returns the user's referring phrases, and
// whether the question carries them through as-is ("their failed invoices")
// or dropped them ("all failed invoices"). It returns nil when the question
// names a subject or uses a word only the conversation supplied.
func (c *leadReferentCheck) unresolved(question string) (refs []string, carried bool) {
	if c == nil || leadQuestionNamesSubject(question) {
		return nil, false
	}
	for w := range leadContentWords(question) {
		if c.historyWords[w] {
			return nil, false
		}
	}
	for ref := range c.instructionRefs {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	for ref := range leadReferringPhrases(question) {
		if c.instructionRefs[ref] {
			carried = true
		}
	}
	return refs, carried
}

func leadReferringPhrases(text string) map[string]bool {
	out := map[string]bool{}
	for _, m := range leadReferentDeterminer.FindAllString(text, -1) {
		out[strings.ToLower(strings.Join(strings.Fields(m), " "))] = true
	}
	for _, m := range leadReferentPronoun.FindAllString(text, -1) {
		out[strings.ToLower(m)] = true
	}
	return out
}

func leadQuestionNamesSubject(question string) bool {
	return strings.ContainsAny(question, "0123456789\"`“”") || leadQuestionProperName.MatchString(question)
}

func leadContentWords(text string) map[string]bool {
	out := map[string]bool{}
	for _, w := range leadQuestionWord.FindAllString(strings.ToLower(text), -1) {
		if len(w) >= 3 && !leadStopWords[w] {
			out[w] = true
		}
	}
	return out
}

func (c *leadReferentCheck) refusal(question string, refs []string, carried bool) error {
	quoted := make([]string, len(refs))
	for i, ref := range refs {
		quoted[i] = `"` + ref + `"`
	}
	phrases := strings.Join(quoted, " and ")
	var msg string
	if carried {
		msg = fmt.Sprintf("not asked: %q says %s, but the data agent sees none of this conversation, so it cannot tell who or what that is.", question, phrases)
	} else {
		msg = fmt.Sprintf("not asked: the user's message says %s, but %q names nothing from the conversation, so the data agent would answer about everything instead of what the user means.", phrases, question)
	}
	if len(c.names) != 0 {
		msg += " The conversation names " + strings.Join(c.names, ", ") + "."
	}
	return errors.New(msg + " Restate the question with the name, id or date the user refers to and ask again")
}

func sentenceStart(text string, at int) bool {
	prefix := strings.TrimRight(text[:at], " \t\n")
	return prefix == "" || strings.HasSuffix(prefix, ".") || strings.HasSuffix(prefix, "?") || strings.HasSuffix(prefix, "!") || strings.HasSuffix(prefix, ":")
}
