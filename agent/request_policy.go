package agent

import (
	"strings"
	"unicode"
)

// preDispatchPolicyRefusal handles only high-confidence write requests whose
// authorization outcome is already a fact in the caller capability profile.
// Keeping this before the model and tool runtime prevents a denied operation
// from being counted as an attempted side effect. Ambiguous language stays on
// the normal catalog-first path.
func preDispatchPolicyRefusal(instruction string, readOnly bool, profile *CapabilityProfile) (Response, bool) {
	action, policyFinal := requestedWriteAction(instruction)
	if action == "" {
		return Response{}, false
	}
	if !policyFinal && !readOnly && (profile == nil || profileHasAction(profile, action)) {
		return Response{}, false
	}

	reason := "the caller capability profile does not grant " + action
	answer := "I can’t perform this write because " + reason + "."
	alternative := "Ask an authorized operator to perform the scoped change, or request the required capability."
	code := "capability_disabled"
	if policyFinal {
		code = "policy_final"
		answer = "I can’t perform this request because unbounded deletion or disabling access controls is policy-final."
		alternative = "Narrow the request to a specific, reviewable record or policy change that preserves access controls and audit history."
		reason = "the requested operation is unbounded or disables authorization controls"
	}

	return Response{
		Status: StatusBlocked,
		Answer: answer,
		Evidence: map[string]any{
			"policy": map[string]any{
				"code":           code,
				"blocked_action": action,
				"policy_final":   true,
				"retryable":      false,
			},
		},
		Refusal: &Refusal{
			Code:              code,
			BlockedAction:     action,
			Because:           []string{reason},
			LawfulAlternative: alternative,
			PolicyFinal:       true,
			Retryable:         false,
		},
		Next: []string{alternative},
	}, true
}

func requestedWriteAction(instruction string) (action string, policyFinal bool) {
	words := instructionWords(instruction)
	if len(words) == 0 {
		return "", false
	}

	if securityDisableIntent(words) {
		return systemRootConfig + ".update", true
	}
	if hasAnyWord(words, "delete", "erase", "purge", "remove", "wipe") {
		unbounded := hasAnyWord(words, "all", "every", "entire") || strings.Contains(strings.ToLower(instruction), "audit history")
		return CapabilityActionDataDelete, unbounded
	}
	if (words["record"] && hasAnyWord(words, "payment", "transaction")) || hasAnyWord(words, "insert") {
		return CapabilityActionDataInsert, false
	}
	if hasAnyWord(words, "update", "modify") || (words["close"] && hasAnyWord(words, "ticket", "task")) ||
		(words["mark"] && hasAnyWord(words, "paid", "resolved", "closed", "seen")) {
		return CapabilityActionDataUpdate, false
	}
	return "", false
}

func securityDisableIntent(words map[string]bool) bool {
	disable := hasAnyWord(words, "disable", "bypass") || (words["turn"] && words["off"])
	controls := words["rls"] || (words["row"] && words["level"] && words["access"]) ||
		(words["access"] && words["controls"]) || words["authorization"]
	return disable && controls
}

func instructionWords(instruction string) map[string]bool {
	out := map[string]bool{}
	for _, word := range strings.FieldsFunc(strings.ToLower(instruction), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
	}) {
		out[word] = true
	}
	return out
}

func hasAnyWord(words map[string]bool, candidates ...string) bool {
	for _, candidate := range candidates {
		if words[candidate] {
			return true
		}
	}
	return false
}
