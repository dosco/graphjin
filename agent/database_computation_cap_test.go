package agent

import (
	"strings"
	"testing"
)

// A correct ranking whose column the request names in other words is refused
// twice, then let through and recorded — never refused until the step budget
// runs out. A new execution earns a fresh pair of refusals.
func TestDatabaseComputationRefusalsAreCapped(t *testing.T) {
	state := newDiscoveryState("Which green coffee lot has the most kilograms in stock?")
	state.executions = append(state.executions, map[string]any{
		"tool": toolExecuteGraphQL, "has_data": true, "error_count": 0,
		"query": `query { green_lots(order_by: { remaining_kg: desc }, limit: 1) { lot_code remaining_kg } }`,
	})
	if !strings.HasPrefix(state.gateFinalization(), "database_computation_required:") ||
		!strings.HasPrefix(state.gateFinalization(), "database_computation_required:") {
		t.Fatal("the first two finals should be refused")
	}
	if got := state.gateFinalization(); got != "" {
		t.Fatalf("third final = %q, want it let through", got)
	}
	if !state.databaseComputationWaived || state.pendingDatabaseComputation() != "" {
		t.Fatal("the waiver must hold for the rest of the run")
	}
	if evidence := mapValue(state.mergeEvidence(nil)); evidence["database_computation_waived"] != true {
		t.Fatalf("evidence = %v, want the waiver recorded", evidence)
	}

	fresh := newDiscoveryState("Which green coffee lot has the most kilograms in stock?")
	fresh.executions = append(fresh.executions, state.executions[0])
	fresh.gateFinalization()
	fresh.gateFinalization()
	fresh.executions = append(fresh.executions, state.executions[0])
	if !strings.HasPrefix(fresh.gateFinalization(), "database_computation_required:") {
		t.Fatal("a new execution resets the refusal count")
	}
}
