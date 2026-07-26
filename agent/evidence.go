package agent

import (
	"encoding/json"
	"strings"
)

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
