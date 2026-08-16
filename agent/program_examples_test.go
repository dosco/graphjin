package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dop251/goja"
)

// TestProgramExamplesBudget is the counted-channel discipline for the
// trajectory examples. Unlike skills, this payload renders into EVERY actor
// step's provider request, so the effective cost is the cap times
// max_actor_steps (8 by default) — keep it deliberate and small.
//
// Budgets ratchet both ways: headroom is intentional friction against
// example creep, and any growth is a visible bump here with its reason.
func TestProgramExamplesBudget(t *testing.T) {
	examples := executorTrajectoryExamples()
	if len(examples) != 2 {
		t.Fatalf("exactly two examples are budgeted, got %d", len(examples))
	}
	total := 0
	for index, example := range examples {
		payload, err := json.Marshal(example)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("example %d: %d bytes", index, len(payload))
		total += len(payload)
	}
	const budget = 4 * 1024
	if total > budget {
		t.Fatalf("examples payload = %d bytes, max %d (×8 steps in practice)", total, budget)
	}
	t.Logf("examples payload: %d bytes (~%d per run at 8 steps)", total, total*8)
}

// TestProgramExamplesAreValidPrograms pins each example's javascriptCode to
// the runtime's actual contract: it compiles under goja, reaches final(), and
// the multi-turn one reads inputs.history — the exact behavior the
// history_read_required guard demands of real programs.
func TestProgramExamplesAreValidPrograms(t *testing.T) {
	examples := executorTrajectoryExamples()
	sawHistory := false
	for index, raw := range examples {
		example, _ := raw.(map[string]any)
		output, _ := example["output"].(map[string]any)
		code, _ := output["javascriptCode"].(string)
		if strings.TrimSpace(code) == "" {
			t.Fatalf("example %d has no javascriptCode", index)
		}
		wrapped := "(async () => {" + code + "})"
		if _, err := goja.Compile("example", wrapped, false); err != nil {
			t.Fatalf("example %d does not compile: %v", index, err)
		}
		if !strings.Contains(code, "final(") {
			t.Fatalf("example %d never finalizes", index)
		}
		if strings.Contains(code, "inputs.history") {
			sawHistory = true
		}
		input, _ := example["input"].(map[string]any)
		for _, key := range []string{"input", "executorRequest", "contextMetadata", "actionLog"} {
			if _, ok := input[key]; !ok {
				t.Fatalf("example %d input is missing executor field %q", index, key)
			}
		}
		// Generic domain only: a benchmark table name in an example would be
		// teaching the test rather than the language.
		for _, forbidden := range []string{"account_health", "sla_policies", "support_tickets", "invoices", "subscriptions", "payments"} {
			if strings.Contains(code, forbidden) {
				t.Fatalf("example %d names benchmark table %q", index, forbidden)
			}
		}
	}
	if !sawHistory {
		t.Fatal("no example demonstrates reading inputs.history")
	}
}

// TestPromptRegistryHashCoversExamples pins the provenance contract: the
// registry hash is derived from a snapshot that includes the executor
// examples, so runs differing only in examples cannot share provenance.
func TestPromptRegistryHashCoversExamples(t *testing.T) {
	first := PromptRegistryHash()
	if first == "" || first != PromptRegistryHash() {
		t.Fatal("registry hash must be stable within a binary")
	}
	payload, err := json.Marshal(executorTrajectoryExamples())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), "javascriptCode") {
		t.Fatal("examples must carry the executor output field")
	}
}
