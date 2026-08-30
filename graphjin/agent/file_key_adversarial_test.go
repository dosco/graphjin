package agent

import (
	"context"
	"strings"
	"testing"
)

// A regular table that happens to have a column named key, filtered to an
// empty result: the guard probes once, the probe errors (no prefix argument on
// tables), and the read must pass through untouched.
func TestRegularTableWithKeyColumnIsNotIntercepted(t *testing.T) {
	base := &fileSourceRuntime{files: map[string]string{"support-sla-policy.md": "x"}}
	profile := &CapabilityProfile{RoleClass: "user", AllowedActions: []string{}}
	runtime := newProtocolRuntime(base, "read api keys", "", 8, profile, nil, CatalogSearchFeatures{})
	runtime.state.seedOK = true
	runtime.state.modelDiscoveryAction = true
	runtime.state.tablesDetailed = map[string]bool{"api_keys": true}
	runtime.state.catalogDetails = []string{"table:app:main.api_keys"}
	runtime.state.securityRuntimeEvidence = true
	// api_keys is not a file source: the fake answers unknown roots with a
	// count row, so make it answer empty for this root instead.
	base.files = map[string]string{}

	out, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{
		"query": `query { api_keys(where: { key: { eq: "prod-key-7" } }) { id key } }`,
	})
	_ = out
	if err != nil {
		t.Fatalf("a regular-table empty filter must pass through: %v", err)
	}
	probes := 0
	for _, q := range base.queries {
		if strings.Contains(q, "prefix:") {
			probes++
		}
	}
	if probes > 1 {
		t.Fatalf("at most one cached probe per root, got %d: %v", probes, base.queries)
	}
}

// A real key excluded by an extra where filter is not a vocabulary problem:
// the key exists, so the guard must stand aside.
func TestRealKeyFilteredToEmptyIsNotRepaired(t *testing.T) {
	runtime, _ := fileReadRuntime(t)
	_, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{
		"query": `query { sla_policies(where: { key: { eq: "support-sla-policy.md" } }, key: "support-sla-policy.md", inline_data: true) { key text } }`,
	})
	if err != nil {
		t.Fatalf("a key the source holds must never be second-guessed: %v", err)
	}
}

// A where-only key filter naming a key the source does NOT hold gets the same
// correction as the argument form: half the recorded SLA queries wrote the
// filter this way, and the bridge answers both with the same silent empty
// list.
func TestWhereFilteredMissStillThrowsWithKeys(t *testing.T) {
	runtime, _ := fileReadRuntime(t)
	_, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{
		"query": `query { sla_policies(where: { key: { eq: "docs/support-sla.md" } }) { key text } }`,
	})
	if err == nil {
		t.Fatal("a where filter naming a key the source does not hold returns the same silent empty list; it must be intercepted too")
	}
	if !strings.Contains(err.Error(), "support-sla-policy.md") {
		t.Fatalf("the exception must name the real key: %v", err)
	}
	repaired := correctedMutationFromError(t, err)
	if !strings.Contains(repaired, `eq: "support-sla-policy.md"`) {
		t.Fatalf("the repair substitutes the real key inside the where filter: %q", repaired)
	}
	if !strings.Contains(repaired, "inline_data: true") {
		t.Fatalf("a selection asking for text still needs inline_data: %q", repaired)
	}
	if _, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": repaired}); err != nil {
		t.Fatalf("the where-form repair must execute as given: %v", err)
	}
}
