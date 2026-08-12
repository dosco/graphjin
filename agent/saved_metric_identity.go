package agent

import (
	"sort"
	"strings"
)

// A request that names a metric — "use the approved open ticket count saved metric
// and explain its current result" — is answered from whichever saved query the run
// happened to execute. Benchmark generation 2028.1 recorded the consequence: asked
// for open_ticket_count, whose value is 4, the agent reported 0 and said so plainly,
// "based on the cached execution result". The 0 came from
// open_critical_ticket_count, executed earlier in the same run.
//
// Nothing caught it. The number is real, it is present in this run's evidence, and
// the grounding check passes — it just answers a different question. The runtime
// makes this likelier by instructing the executor to finalize from the retained
// execution immediately without checking that it answers the request.
//
// This is worse than a wasted step: it is a confident wrong number attributed to a
// named metric. The check below fires only when the run's own evidence contradicts
// itself — the executed metric is not the one named, and exactly one other known
// metric is.

// humanizedMetricName renders a saved-query name the way a request refers to it:
// open_ticket_count -> "open ticket count".
func humanizedMetricName(name string) string {
	replaced := strings.NewReplacer("_", " ", "-", " ", ".", " ").Replace(strings.ToLower(strings.TrimSpace(name)))
	return strings.Join(strings.Fields(replaced), " ")
}

// instructionNamesMetric reports whether the instruction refers to this saved
// query, by its stored name or the spaced form a person would write.
func instructionNamesMetric(instruction, name string) bool {
	lower := strings.ToLower(instruction)
	trimmed := strings.ToLower(strings.TrimSpace(name))
	if trimmed == "" {
		return false
	}
	if strings.Contains(lower, trimmed) {
		return true
	}
	humanized := humanizedMetricName(trimmed)
	return humanized != "" && strings.Contains(lower, humanized)
}

// knownSavedQueryNames returns the saved-query names this run inspected, in a
// stable order. Only names backed by a detail card count: a name the run never saw
// cannot be what the request meant.
func (s *discoveryState) knownSavedQueryNames() []string {
	if s == nil {
		return nil
	}
	var out []string
	for name := range s.savedQueryGraphQL {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			out = appendUniqueString(out, trimmed)
		}
	}
	sort.Strings(out)
	return out
}

// executedSavedQueryName returns the saved query the run's completion evidence came
// from, or "" when the answer does not rest on one.
func (s *discoveryState) executedSavedQueryName() string {
	if s == nil || s.lastExecution == nil {
		return ""
	}
	execution := mapValue(s.lastExecution)
	if !strings.EqualFold(stringFromMap(execution, "tool"), toolExecuteSavedQuery) {
		return ""
	}
	args := mapValue(execution["args"])
	return strings.TrimSpace(stringFromMap(args, "name"))
}

// mismatchedSavedMetric returns the metric the request named when the run answered
// from a different one, or "" when there is nothing to correct.
//
// It requires three things to hold together, so a request that names no metric, or
// names the one that ran, is never touched: the answer rests on saved query A, the
// instruction does not refer to A, and it refers to exactly one other saved query
// this run inspected.
func (s *discoveryState) mismatchedSavedMetric() (executed, requested string) {
	if s == nil {
		return "", ""
	}
	executed = s.executedSavedQueryName()
	if executed == "" || s.instruction == "" {
		return "", ""
	}
	if instructionNamesMetric(s.instruction, executed) {
		return "", ""
	}
	var named []string
	for _, name := range s.knownSavedQueryNames() {
		if strings.EqualFold(name, executed) {
			continue
		}
		if instructionNamesMetric(s.instruction, name) {
			named = appendUniqueString(named, name)
		}
	}
	if len(named) != 1 {
		return "", ""
	}
	return executed, named[0]
}
