package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3"
)

// A failed payments insert with created_at (real column: recorded_at) retried
// the identical query until the duplicate guard locked it out, because the
// recovery neither resolved the mutation's table nor named its columns. These
// tests pin the three repairs: mutation roots resolve to catalog ids, the
// recovery carries a machine-readable kind, and unknown-column failures name
// the table's real columns from the catalog.

func TestExecutionRecoveryNamesMutationTargetTables(t *testing.T) {
	state := newDiscoveryState("Record payment DEEPORG-PAY-002 for invoice 2.")
	state.catalogIDs["table:app:main.payments"] = true

	recovery := executionRecovery(state, `mutation { payments(insert: { id: 900002, invoice_id: 2 }) { id } }`)
	if recovery["kind"] != "execution_error" || recovery["code"] != "execution_error" {
		t.Fatalf("execution recovery must carry its kind for summaries and replay: %v", recovery)
	}
	next, _ := recovery["next"].(map[string]any)
	args, _ := next["args"].(map[string]any)
	ids, _ := args["ids"].([]any)
	if len(ids) == 0 || ids[0] != "table:app:main.payments" {
		t.Fatalf("a failed write must name its table's catalog id: %v", next)
	}
}

// paymentsColumnRuntime fails the write with a chosen engine error and serves
// payments column cards, the way the live catalog does.
type paymentsColumnRuntime struct {
	fakeRuntime
	errorMessage string
	catalogCalls int
}

func (r *paymentsColumnRuntime) ExecuteGraphQL(_ context.Context, args map[string]any) (any, error) {
	return map[string]any{"errors": []any{map[string]any{"message": r.errorMessage}}}, nil
}

func (r *paymentsColumnRuntime) QueryCatalog(_ context.Context, args map[string]any) (any, error) {
	r.catalogCalls++
	if table, _ := args["table"].(string); table != "payments" {
		return map[string]any{"cards": []any{}}, nil
	}
	cards := []any{}
	for _, column := range []string{"id", "invoice_id", "amount_cents", "reference", "recorded_at"} {
		cards = append(cards, map[string]any{
			"id": "column:app:main.payments." + column, "kind": "column", "table_name": "payments",
		})
	}
	return map[string]any{"cards": cards}, nil
}

func TestExecutionRecoveryNamesRealColumnsOnUnknownColumn(t *testing.T) {
	for _, errorMessage := range []string{
		`insert: column: 'payments.created_at' not found`,
		`NOT NULL constraint failed: payments.recorded_at`,
	} {
		base := &paymentsColumnRuntime{errorMessage: errorMessage}
		profile := &CapabilityProfile{RoleClass: "user", AllowedActions: []string{CapabilityActionDataInsert}}
		runtime := newProtocolRuntime(base, "Record payment DEEPORG-PAY-002.", "", 8, profile, nil, CatalogSearchFeatures{})
		runtime.state.seedOK = true
		runtime.state.modelDiscoveryAction = true
		runtime.state.securityRuntimeEvidence = true
		runtime.state.mutationEvidenceSupplied = true
		runtime.state.tablesDetailed["payments"] = true
		runtime.state.catalogDetails = []string{"table:app:main.payments"}

		_, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{
			"query": `mutation { payments(insert: { id: 900002, invoice_id: 2, created_at: "2027-01-15T12:00:00Z" }) { id } }`,
		})
		if err == nil {
			t.Fatalf("%q: a dataless engine failure must throw", errorMessage)
		}
		for _, want := range []string{"did NOT return data", errorMessage, "recorded_at"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("%q: the exception must name the real columns, missing %q: %v", errorMessage, want, err)
			}
		}
		repaired := correctedMutationFromError(t, err)
		if !strings.Contains(repaired, "recorded_at:") {
			t.Fatalf("%q: the corrected write must use the real column: %q", errorMessage, repaired)
		}
		if columns, ok := runtime.state.tableColumnNames["payments"]; !ok || len(columns) != 5 {
			t.Fatalf("%q: column names must be cached for the run: %v", errorMessage, runtime.state.tableColumnNames)
		}
	}
}

func TestExecuteResultFromCorePrefersStructuredErrors(t *testing.T) {
	// Mirrors what core returns on a compile failure: err non-nil AND
	// res.Errors populated with repair extensions.
	compileErr := errors.New("column: 'payments.created_at' not found")
	res := &core.Result{Errors: []core.Error{{
		Message:    compileErr.Error(),
		Extensions: map[string]any{"graphjin_repair": map[string]any{"kind": "column_not_found"}},
	}}}
	out := executeResultFromCore(res, compileErr)
	if len(out.Errors) != 1 {
		t.Fatalf("structured errors must survive, got %+v", out.Errors)
	}
	if out.Errors[0].Extensions == nil {
		t.Fatalf("extensions (graphjin_repair) must survive: %+v", out.Errors[0])
	}
	// Without structured errors the message fallback remains.
	fallback := executeResultFromCore(nil, compileErr)
	if len(fallback.Errors) != 1 || fallback.Errors[0].Message != compileErr.Error() {
		t.Fatalf("nil result must fall back to the plain message: %+v", fallback.Errors)
	}
}
