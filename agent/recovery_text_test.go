package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// A benchmark run measured what the two generic recovery strings cost: the
// value guard computed the corrected write and told the model to execute it
// exactly as given, while recovery.instruction and the message directive both
// said to go do a discovery-only step. Models followed the louder generic text
// into catalog reads and resubmitted the same wrong write. These tests pin the
// derivation: when a caller supplies a kind-specific next.reason, THAT is the
// instruction and the directive; the generic sentences survive only as the
// fallback for a reason-less next.

func TestRecoverableProtocolFailureDerivesTextFromReason(t *testing.T) {
	// The vehicle is the unbindable-referent refusal: mutation interceptions
	// now throw (straight-line executor code reads any non-exception as
	// success), so the reason-derivation contract is pinned on a read-path
	// guard that still returns a recoverable payload.
	runtime, _ := referentTestRuntime(t, "How many rows does that widget have?",
		Turn{Role: "user", Content: "Look at widget 9."},
	)
	first, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": `query { invoices { id } }`})
	if err != nil {
		t.Fatalf("first attempt errored instead of returning a repair: %v", err)
	}
	payload, _ := json.Marshal(first)
	if strings.Contains(string(payload), "Gather the named evidence") {
		t.Fatalf("generic instruction must not drown the kind-specific reason: %s", payload)
	}
	if strings.Contains(string(payload), "discovery-only actor step") {
		t.Fatalf("generic directive must not contradict an execute_graphql reason: %s", payload)
	}

	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	recovery := mapValue(decoded["recovery"])
	instruction, _ := recovery["instruction"].(string)
	if !strings.Contains(instruction, "scopes this query") {
		t.Fatalf("instruction must carry the kind-specific reason, got %q", instruction)
	}
	next := mapValue(recovery["next"])
	if reason, _ := next["reason"].(string); instruction != reason {
		t.Fatalf("instruction %q must equal next.reason %q", instruction, reason)
	}
	errors, _ := decoded["errors"].([]any)
	if len(errors) == 0 {
		t.Fatalf("expected a recoverable failure: %s", payload)
	}
	message, _ := mapValue(errors[0])["message"].(string)
	if !strings.Contains(message, recoveryDirectivePrefix) {
		t.Fatalf("directive must keep the dedupe prefix: %q", message)
	}
	if !strings.Contains(message, "scopes this query") {
		t.Fatalf("message directive must carry the specific fix: %q", message)
	}
}
func TestRecoverableProtocolFailureKeepsGenericTextWithoutReason(t *testing.T) {
	out := recoverableProtocolFailure("some_code", "protocol violation: details", "some_kind",
		map[string]any{"recommended_tool": toolQueryCatalog}, nil)
	instruction, _ := mapValue(out.Recovery)["instruction"].(string)
	if !strings.Contains(instruction, "Gather the named evidence") {
		t.Fatalf("reason-less next must keep the generic instruction, got %q", instruction)
	}
	if len(out.Errors) == 0 || !strings.Contains(out.Errors[0].Message, "discovery-only actor step") {
		t.Fatalf("reason-less next must keep the generic directive: %+v", out.Errors)
	}
}

func TestRecoveryEvidenceCapturesNext(t *testing.T) {
	out := recoverableProtocolFailure("some_code", "msg", "some_kind",
		map[string]any{"recommended_tool": "execute_graphql", "reason": "Execute the corrected write once."}, nil)
	raw, _ := json.Marshal(out)
	var normalized map[string]any
	if err := json.Unmarshal(raw, &normalized); err != nil {
		t.Fatal(err)
	}
	captured := recoveryEvidence(normalized)
	payload, _ := json.Marshal(captured)
	for _, want := range []string{"next", "execute_graphql", "Execute the corrected write once."} {
		if !strings.Contains(string(payload), want) {
			t.Fatalf("evidence must record the recommended tool and reason, missing %q: %s", want, payload)
		}
	}
}
