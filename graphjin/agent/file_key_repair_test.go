package agent

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// fileSourceRuntime answers like core's filesystem bridge: an unknown key is
// an empty list and no error (core/fstable_bridge.go, the ErrNotFound branch),
// a prefix read lists what the source holds, and the object's contents arrive
// only when inline_data was asked for.
type fileSourceRuntime struct {
	fakeRuntime
	files   map[string]string
	queries []string
}

func (r *fileSourceRuntime) ExecuteGraphQL(_ context.Context, args map[string]any) (any, error) {
	query, _ := args["query"].(string)
	r.queries = append(r.queries, query)
	if err := checkGraphQLParses(query); err != nil {
		return nil, err
	}
	data := map[string]any{}
	clean := graphQLStructure(query)
	for _, root := range QueryRootFields(query) {
		if root != "sla_policies" {
			data[root] = []any{map[string]any{"count_id": 1}}
			continue
		}
		open := skipGraphQLSpace(clean, strings.Index(clean, root)+len(root))
		if open >= len(clean) || clean[open] != '(' {
			data[root] = []any{}
			continue
		}
		closing := matchingGraphQLDelimiter(clean, open, '(', ')')
		body := query[open+1 : closing]
		spans := graphQLStringFieldSpans(body)
		inline := strings.Contains(strings.ToLower(body), "inline_data")
		if key, ok := spans["key"]; ok {
			text, exists := r.files[key.value]
			if !exists {
				data[root] = []any{} // the miss: empty, never an error
				continue
			}
			row := map[string]any{"key": key.value}
			if inline {
				row["text"] = text
			}
			data[root] = []any{row}
			continue
		}
		rows := []any{}
		for name := range r.files {
			rows = append(rows, map[string]any{"key": name})
		}
		data[root] = rows
	}
	return map[string]any{"data": data}, nil
}

func fileReadRuntime(t *testing.T) (*protocolRuntime, *fileSourceRuntime) {
	t.Helper()
	base := &fileSourceRuntime{files: map[string]string{
		"support-sla-policy.md": "# Support SLA policy\n\n- Urgent tickets must be resolved within 4 hours.",
	}}
	profile := &CapabilityProfile{RoleClass: "user", AllowedActions: []string{}}
	runtime := newProtocolRuntime(base, "read the SLA policy and count open urgent tickets", "", 8, profile, nil, CatalogSearchFeatures{})
	runtime.state.seedOK = true
	runtime.state.modelDiscoveryAction = true
	runtime.state.tablesDetailed = map[string]bool{"sla_policies": true, "support_tickets": true}
	runtime.state.catalogDetails = []string{"source:sla_policies", "table:app:main.support_tickets"}
	runtime.state.securityRuntimeEvidence = true
	return runtime, base
}

// TestGuessedFileKeyIsCorrected pins the defect behind the worst category in
// run r4. Every SLA episode asked sla_policies for docs/support-sla.md — a path
// that exists nowhere in the repo — and the bridge answered with an empty list
// and no error, so the model could not tell "no such object" from "the object
// is empty". None of the twelve ever executed the real key; one scored a full
// pass on a figure it invented.
func TestGuessedFileKeyIsCorrected(t *testing.T) {
	runtime, base := fileReadRuntime(t)
	guessed := `query { support_tickets(where: { status: { eq: "open" } }) { count_id } sla_policies(key: "docs/support-sla.md") { key text } }`

	_, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": guessed})
	if err == nil {
		t.Fatal("a read that named a key the source does not hold must not pass for an empty object")
	}
	if !strings.Contains(err.Error(), "support-sla-policy.md") {
		t.Fatalf("the exception must name the key that exists: %v", err)
	}
	repaired := correctedMutationFromError(t, err)
	if !strings.Contains(repaired, `key: "support-sla-policy.md"`) {
		t.Fatalf("the corrected read must carry the real key: %q", repaired)
	}
	if !strings.Contains(repaired, "inline_data: true") {
		t.Fatalf("a selection asking for text needs inline_data to return it: %q", repaired)
	}
	if !strings.Contains(repaired, "support_tickets") {
		t.Fatalf("the repair must keep the read's other roots: %q", repaired)
	}

	// Obeying the imperative verbatim must produce the policy text, and must
	// clear the guard rather than leaving the run blocked at finalization.
	out, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": repaired})
	if err != nil {
		t.Fatalf("executing the repair exactly as given: %v", err)
	}
	if !strings.Contains(fmt.Sprint(out), "4 hours") {
		t.Fatalf("the corrected read must return the file's contents: %v", out)
	}
	resp := runtime.state.finalize(Response{Status: StatusAnswered, Answer: "Urgent tickets must be resolved within 4 hours; 1 is open."})
	if resp.Status != StatusAnswered {
		t.Fatalf("the corrected read must discharge the guard, got %s: %+v", resp.Status, resp.Errors)
	}
	if len(base.queries) == 0 {
		t.Fatal("expected the reads to reach the runtime")
	}
}

// A key the source actually holds is never second-guessed, even when the read
// returns nothing — that is a filter matching no rows, not a wrong vocabulary.
func TestRealFileKeyReturningNothingIsNotRepaired(t *testing.T) {
	runtime, _ := fileReadRuntime(t)
	runtime2, _ := fileReadRuntime(t)
	if _, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{
		"query": `query { sla_policies(key: "support-sla-policy.md", inline_data: true) { key text } }`,
	}); err != nil {
		t.Fatalf("a correct read must not be intercepted: %v", err)
	}
	// And a source with no near match lists rather than inventing a suggestion.
	runtime2.state.fileSourceKeys = map[string][]string{"sla_policies": {"alpha.md", "beta.md"}}
	_, err := runtime2.ExecuteGraphQL(context.Background(), map[string]any{
		"query": `query { sla_policies(key: "totally-unrelated.md") { key } }`,
	})
	if err == nil {
		t.Fatal("an unknown key must still be reported")
	}
	if !strings.Contains(err.Error(), "alpha.md") || !strings.Contains(err.Error(), "beta.md") {
		t.Fatalf("with no clear match the exception lists the real keys: %v", err)
	}
	if regexp.MustCompile(`repaired`).MatchString(err.Error()) {
		t.Fatalf("no single candidate means no invented repair: %v", err)
	}
}
