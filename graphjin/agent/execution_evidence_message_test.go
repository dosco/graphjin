package agent

import (
	"strings"
	"testing"
)

// This requirement reaches the model as a thrown exception, so it must carry
// the next step in its text. Pointing at errors[].extensions.graphjin_repair
// named a payload that does not exist on the throw path: recorded runs
// deadlocked, re-running the same read up to eight times because the refusal
// said what was wrong and never what to do.
func TestExecutionEvidenceRequirementNamesTheLookup(t *testing.T) {
	s := newDiscoveryState("mark the unseen watch event as seen")
	s.seedOK = true
	s.modelDiscoveryAction = true
	s.addViolation("mutation_evidence_required",
		"this write did NOT execute: gather mutation-shape evidence for gj_watch_event first",
		"execute_graphql", true, map[string]any{"tables": []any{"gj_watch_event"}})

	got := s.pendingRequiredFinalization()
	if got == "" {
		t.Fatal("a recoverable write violation must produce an outstanding requirement")
	}
	if strings.Contains(got, "errors[].extensions") {
		t.Fatalf("a thrown requirement must not point at a payload the caller cannot read: %s", got)
	}
	if !strings.Contains(got, "query_catalog({ids:[") {
		t.Fatalf("the requirement must name the lookup to run: %s", got)
	}
	// The deadlock was the caller re-reading to comply; say that plainly.
	if !strings.Contains(got, "Re-running an earlier read does not satisfy") {
		t.Fatalf("the requirement must rule out the re-read: %s", got)
	}
}

// The blocked-repeat message carries the requirement, so the two together must
// leave a legal move.
func TestBlockedRepeatCarriesAnActionableRequirement(t *testing.T) {
	s := newDiscoveryState("mark the unseen watch event as seen")
	s.seedOK = true
	s.modelDiscoveryAction = true
	s.addViolation("mutation_evidence_required",
		"this write did NOT execute: gather mutation-shape evidence for gj_watch_event first",
		"execute_graphql", true, map[string]any{"tables": []any{"gj_watch_event"}})

	requirement := s.pendingRequiredFinalization()
	combined := "this call did NOT execute: the identical successful query already ran and its rows cannot satisfy the outstanding requirement. " + requirement
	if !strings.Contains(combined, "query_catalog({ids:[") {
		t.Fatalf("the blocked repeat leaves no legal move: %s", combined)
	}
}
