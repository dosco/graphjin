package agent

import (
	"encoding/json"
	"sort"
	"strings"
)

// blockingGuardViolationCodes is the authoritative vocabulary for protocol
// guards that refuse an action or answer before it can produce an effect.
// Eval scoring treats these as governance interventions: they may fail the
// requested behavior, but the successful block is not itself an unsafe effect.
var blockingGuardViolationCodes = map[string]struct{}{
	"access_blocked":                        {},
	"access_unauthorized":                   {},
	"annotation_tier_confirmation_required": {},
	"artifact_kind_locked":                  {},
	"authenticated_required":                {},
	"capability-disabled":                   {},
	"capability_disabled":                   {},
	"catalog_detail_required":               {},
	"catalog_seed_failed":                   {},
	"catalog_seed_required":                 {},
	"cross_source_detail_required":          {},
	"history_read_required":                 {},
	"identity_variable_missing":             {},
	"model_discovery_required":              {},
	"mutation_evidence_required":            {},
	"raw_graphql_catalog_required":          {},
	"raw_graphql_discovery_required":        {},
	"runtime_handoff_read_required":         {},
	"saved_query_detail_required":           {},
	"saved_query_execution_required":        {},
	"security_runtime_discovery_required":   {},
	"ungrounded_answer_fields":              {},
	"watch_action_confirmation_required":    {},
	"workflow_detail_required":              {},
}

// BlockingGuardViolationCodes returns the stable, sorted set of agent guard
// codes that prove GraphJin blocked a path before execution or finalization.
func BlockingGuardViolationCodes() []string {
	out := make([]string, 0, len(blockingGuardViolationCodes))
	for code := range blockingGuardViolationCodes {
		out = append(out, code)
	}
	sort.Strings(out)
	return out
}

// IsBlockingGuardViolationCode reports whether code names a governance guard
// that prevented an effect.
func IsBlockingGuardViolationCode(code string) bool {
	_, ok := blockingGuardViolationCodes[strings.TrimSpace(code)]
	return ok
}

// CatalogDetailIDs returns the catalog detail ids established by a response's
// protocol evidence. It accepts both the bare protocol evidence shape and the
// wrapped {protocol, model} shape produced when model evidence is also present.
func CatalogDetailIDs(resp Response) []string {
	evidence := responseProtocolEvidence(resp)
	return evidenceStringSlice(evidence["catalog_detail_ids"])
}

// ProtocolViolationCodes returns stable violation codes from protocol
// evidence, accepting both wrapped and bare evidence shapes.
func ProtocolViolationCodes(resp Response) []string {
	evidence := responseProtocolEvidence(resp)
	data, err := json.Marshal(evidence["violations"])
	if err != nil {
		return nil
	}
	var violations []map[string]any
	if err := json.Unmarshal(data, &violations); err != nil {
		return nil
	}
	out := make([]string, 0, len(violations))
	for _, violation := range violations {
		if code := strings.TrimSpace(stringValue(violation["code"])); code != "" {
			out = append(out, code)
		}
	}
	return out
}

func responseProtocolEvidence(resp Response) map[string]any {
	data, err := json.Marshal(resp.Evidence)
	if err != nil {
		return nil
	}
	var evidence map[string]any
	if err := json.Unmarshal(data, &evidence); err != nil {
		return nil
	}
	if protocol, ok := evidence["protocol"].(map[string]any); ok {
		return protocol
	}
	return evidence
}

func evidenceStringSlice(value any) []string {
	var values []any
	switch typed := value.(type) {
	case []any:
		values = typed
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if item = strings.TrimSpace(item); item != "" {
				out = append(out, item)
			}
		}
		return out
	default:
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if item := strings.TrimSpace(stringValue(value)); item != "" {
			out = append(out, item)
		}
	}
	return out
}
