package agent

import (
	"reflect"
	"testing"
)

func TestResponseEvidenceHelpersAcceptBareAndWrappedProtocolShapes(t *testing.T) {
	protocol := map[string]any{
		"catalog_detail_ids": []any{"table:app:public.orders", "saved_query:late_orders"},
		"violations": []any{
			map[string]any{"code": "catalog_detail_required"},
			map[string]any{"code": "mutation_evidence_required"},
		},
	}
	for _, tc := range []struct {
		name     string
		evidence any
	}{
		{name: "bare", evidence: protocol},
		{name: "wrapped", evidence: map[string]any{"protocol": protocol, "model": map[string]any{"note": "advisory"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := Response{Evidence: tc.evidence}
			if got, want := CatalogDetailIDs(resp), []string{"table:app:public.orders", "saved_query:late_orders"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("CatalogDetailIDs = %v, want %v", got, want)
			}
			if got, want := ProtocolViolationCodes(resp), []string{"catalog_detail_required", "mutation_evidence_required"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("ProtocolViolationCodes = %v, want %v", got, want)
			}
		})
	}
}

func TestResponseEvidenceHelpersIgnoreMalformedValues(t *testing.T) {
	resp := Response{Evidence: map[string]any{
		"catalog_detail_ids": "not-a-list",
		"violations":         map[string]any{"code": "not-a-list"},
	}}
	if got := CatalogDetailIDs(resp); got != nil {
		t.Fatalf("CatalogDetailIDs malformed = %v, want nil", got)
	}
	if got := ProtocolViolationCodes(resp); got != nil {
		t.Fatalf("ProtocolViolationCodes malformed = %v, want nil", got)
	}
}
