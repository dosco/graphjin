package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// These mutations are verbatim from run f768e0ad's predecessor on frozen suite
// 48948f16, where 28 watch attempts arrived with unescaped inner quotes after the
// comma false-positive was already fixed. Only two watches were created across 36
// episodes, so the quoting is the binding constraint, not the guidance.
func TestRepairWatchSubscriptionString(t *testing.T) {
	for _, tc := range []struct{ name, query string }{
		{
			name:  "filtered tickets subscription",
			query: `mutation CreateWatch { gj_watch(insert: { name: "deeporg_urgent_tickets", query: "subscription deeporg_urgent_tickets { support_tickets(where: { status: { eq: "urgent" } }, first: 25) { id subject status } support_tickets_cursor }", delivery_json: { kind: "inbox", digest: { window: "1h" } } }) { id name status } }`,
		},
		{
			name:  "filtered invoices with cursor",
			query: `mutation CreateFailedInvoicesWatch { gj_watch(insert: { name: "deeporg_failed_invoices", query: "subscription failed_invoices { invoices(where: { status: { eq: "failed" } }, first: 25, after: $cursor) { id status total } invoices_cursor }", delivery_json: { kind: "inbox", digest: { window: "1h" } } }) { id status name } }`,
		},
		{
			name:  "no trailing comma before the next field",
			query: `mutation { gj_watch(insert: { name: "w" query: "subscription { invoices(where: { status: { eq: "failed" } }) { id } }" delivery_json: { kind: "inbox" } }) { id } }`,
		},
	} {
		repaired, ok := repairWatchSubscriptionString(tc.query)
		if !ok {
			t.Errorf("%s: expected a repair", tc.name)
			continue
		}
		if malformedWatchSubscriptionString(repaired) {
			t.Errorf("%s: repaired mutation still reads as malformed: %s", tc.name, repaired)
		}
		// The subscription's own content must survive intact.
		for _, want := range []string{`\"`, "subscription", "delivery_json"} {
			if !strings.Contains(repaired, want) {
				t.Errorf("%s: repaired mutation lost %q: %s", tc.name, want, repaired)
			}
		}
		if strings.Contains(repaired, `\\"`) {
			t.Errorf("%s: escaping was applied twice: %s", tc.name, repaired)
		}
	}
}

func TestRepairWatchSubscriptionStringLeavesValidMutationsAlone(t *testing.T) {
	// An already-correct mutation is not malformed, so the guard never reaches the
	// repair; asking for one anyway must return it unchanged rather than double-escape.
	valid := `mutation { gj_watch(insert: { name: "w", query: "subscription { invoices(where: { status: { eq: \"failed\" } }) { id } }", delivery_json: { kind: "inbox" } }) { id } }`
	repaired, ok := repairWatchSubscriptionString(valid)
	if !ok {
		t.Fatal("a valid mutation should still round-trip")
	}
	if repaired != valid {
		t.Fatalf("a correctly escaped mutation must be returned unchanged:\n got %s\nwant %s", repaired, valid)
	}
}

func TestRepairWatchSubscriptionStringDeclinesWhenAmbiguous(t *testing.T) {
	for _, tc := range []struct{ name, query string }{
		// Nothing closes the string, so there is no boundary to trust.
		{"unterminated", `mutation { gj_watch(insert: { name: "w", query: "subscription { invoices { id }`},
		{"no query field", `mutation { gj_watch(insert: { name: "w" }) { id } }`},
		{"variable form needs no repair", `mutation W($q: String!) { gj_watch(insert: { name: "w", query: $q }) { id } }`},
	} {
		if repaired, ok := repairWatchSubscriptionString(tc.query); ok {
			t.Errorf("%s: expected no repair, got %s", tc.name, repaired)
		}
	}
}

// watchNormalizeRuntime accepts any executed mutation and records what arrived.
type watchNormalizeRuntime struct {
	fakeRuntime
	mutationCalls int
	lastQuery     string
}

func (r *watchNormalizeRuntime) ExecuteGraphQL(_ context.Context, args map[string]any) (any, error) {
	r.mutationCalls++
	r.lastQuery, _ = args["query"].(string)
	return map[string]any{"data": map[string]any{"gj_watch": map[string]any{"id": "watch:abc", "status": "active"}}}, nil
}

// TestMalformedWatchSubscriptionExecutesNormalized pins accept-and-normalize:
// a repairable unescaped subscription string executes as its parse-verified
// re-escaping in the same step, carrying a recovery notice instead of a
// blocking violation. Offering the repair back was measured across a
// benchmark run at 3 recoveries out of 13 blocks, with 5 episodes narrating
// the repair to the user as their final answer.
func TestMalformedWatchSubscriptionExecutesNormalized(t *testing.T) {
	base := &watchNormalizeRuntime{}
	profile := &CapabilityProfile{RoleClass: "user", AllowedActions: []string{"gj_watch.insert", "gj_watch.update"}}
	runtime := newProtocolRuntime(base, "Watch failed invoices.", "", 8, profile, nil, CatalogSearchFeatures{})
	runtime.state.seedOK = true
	runtime.state.modelDiscoveryAction = true
	runtime.state.securityRuntimeEvidence = true
	runtime.state.mutationEvidenceSupplied = true
	runtime.state.tablesDetailed["invoices"] = true
	runtime.state.catalogDetails = []string{"help:watches", "table:app:main.invoices"}

	out, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{
		"query": `mutation { gj_watch(insert: { name: "failed_invoices", query: "subscription { invoices(where: { status: { eq: "failed" } }, first: 25, after: $cursor) { id } invoices_cursor }", delivery_json: { kind: "inbox" } }) { id status } }`,
	})
	if err != nil {
		t.Fatalf("normalized execution errored: %v", err)
	}
	if base.mutationCalls != 1 {
		t.Fatalf("the repaired mutation must execute in the same step, calls=%d", base.mutationCalls)
	}
	if !strings.Contains(base.lastQuery, `\"failed\"`) {
		t.Fatalf("dispatch must receive the escaped form: %q", base.lastQuery)
	}
	payload, _ := json.Marshal(out)
	for _, want := range []string{"watch_subscription_normalized", "repaired_query"} {
		if !strings.Contains(string(payload), want) {
			t.Fatalf("result must carry the normalization notice, missing %q: %s", want, payload)
		}
	}
	if strings.Contains(string(payload), "watch_query_invalid") {
		t.Fatalf("a repairable string must not record the blocking violation: %s", payload)
	}
	if runtime.state.hasBlockingViolation() {
		t.Fatal("normalization must not leave a blocking violation behind")
	}

	// An unrepairable string keeps the historical blocking refusal.
	blockedOut, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{
		"query": `mutation { gj_watch(insert: { name: "broken", query: "subscription { invoices { id }`,
	})
	if err != nil {
		t.Fatalf("unrepairable case should return a refusal, not an error: %v", err)
	}
	payload, _ = json.Marshal(blockedOut)
	if !strings.Contains(string(payload), "watch_query_invalid") {
		t.Fatalf("an unrepairable string must still block: %s", payload)
	}
	if base.mutationCalls != 1 {
		t.Fatalf("the unrepairable mutation must not dispatch, calls=%d", base.mutationCalls)
	}
}
