package agent

import (
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
