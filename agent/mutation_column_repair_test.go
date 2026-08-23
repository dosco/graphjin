package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// Ten of fifteen action tasks in run c3-r2 were lost to the same episode shape:
// the model invented a column name, the engine rejected the write, and the
// model either resent the identical mutation until its step budget died or
// finalized "Successfully recorded..." over a database that never changed.
// These tests pin the two answers: the corrected mutation is computed and
// handed over, and a run whose writes all failed cannot report the work done.

func TestRepairUnknownMutationColumns(t *testing.T) {
	payments := []string{"id", "invoice_id", "amount_cents", "reference", "recorded_at"}
	tickets := []string{"id", "account_id", "user_id", "subject", "severity", "status", "resolution_note", "resolved_at", "opened_at", "sla_due_at"}
	for name, tc := range map[string]struct {
		query    string
		columns  []string
		want     string // "" means no repair
		contains []string
		absent   []string
	}{
		"the recorded payment episode": {
			// Verbatim from episode action-record-payment-deeporg-pay-001 rep1.
			query:    `mutation { payments(insert: { id: 900001, payment_reference: "DEEPORG-PAY-001", invoice_id: 1, amount_cents: 480000, paid_at: "2027-01-15T12:00:00Z" }) { id payment_reference invoice_id amount_cents paid_at } }`,
			columns:  payments,
			contains: []string{`reference: "DEEPORG-PAY-001"`, `recorded_at: "2027-01-15T12:00:00Z"`},
			absent:   []string{"payment_reference", "paid_at"},
		},
		"amount is a prefix of amount_cents": {
			query:    `mutation { payments(insert: { id: 900003, reference: "DEEPORG-PAY-003", invoice_id: 3, amount: 550000, recorded_at: "2027-01-15T12:00:00Z" }) { id } }`,
			columns:  payments,
			contains: []string{"amount_cents: 550000"},
			absent:   []string{"amount: 550000"},
		},
		"notes finds resolution_note": {
			query:    `mutation { support_tickets(where: { id: { eq: 2 } }, update: { status: "resolved", notes: "Sorted out and resolved." }) { id status notes } }`,
			columns:  tickets,
			contains: []string{`resolution_note: "Sorted out and resolved."`, "status resolution_note"},
			absent:   []string{"notes:"},
		},
		"plural resolution_notes folds to the singular": {
			query:    `mutation { support_tickets(where: { id: { eq: 3 } }, update: { status: "resolved", resolution_notes: "Done." }) { id resolution_notes } }`,
			columns:  tickets,
			contains: []string{`resolution_note: "Done."`},
		},
		"a value never renames even when it matches": {
			query:    `mutation { payments(insert: { id: 1, reference: "paid_at", amount: 5 }) { id } }`,
			columns:  payments,
			contains: []string{`reference: "paid_at"`, "amount_cents: 5"},
		},
		"no candidate refuses the whole repair": {
			// name has no lexical relation to reference; a partial repair
			// labelled exact is the round-2 mistake.
			query:   `mutation { payments(insert: { id: 1, name: "DEEPORG-PAY-001", amount: 5 }) { id } }`,
			columns: payments,
			want:    "",
		},
		"two plausible answers refuse": {
			query:   `mutation { orders(insert: { note: "x" }) { id } }`,
			columns: []string{"id", "internal_note", "customer_note"},
			want:    "",
		},
		"nested related-row writes are left alone": {
			query:   `mutation { payments(insert: { id: 1, paid_at: "t", invoice: { id: 2 } }) { id } }`,
			columns: payments,
			want:    "",
		},
		"a mutation with no unknown keys needs nothing": {
			query:   `mutation { payments(insert: { id: 1, reference: "r", recorded_at: "t" }) { id } }`,
			columns: payments,
			want:    "",
		},
	} {
		t.Run(name, func(t *testing.T) {
			repaired, renames, ok := repairUnknownMutationColumns(tc.query, tc.columns)
			if tc.want == "" && len(tc.contains) == 0 {
				if ok {
					t.Fatalf("expected no repair, got %q (%v)", repaired, renames)
				}
				return
			}
			if !ok {
				t.Fatalf("expected a repair for %q", tc.query)
			}
			// The offered string is labelled "execute exactly as given", so it
			// has to survive the same parse-level check the join repair pins.
			if err := checkGraphQLParses(repaired); err != nil {
				t.Fatalf("repaired mutation does not parse: %v", err)
			}
			for _, want := range tc.contains {
				if !strings.Contains(repaired, want) {
					t.Fatalf("repaired mutation missing %q: %q", want, repaired)
				}
			}
			for _, gone := range tc.absent {
				if strings.Contains(repaired, gone) {
					t.Fatalf("repaired mutation still carries %q: %q", gone, repaired)
				}
			}
		})
	}
}

// strictWriteRuntime fails like the real engine: a mutation naming an unknown
// column is rejected with the engine's own message, and only a fully-correct
// write lands. The recording fake this file used to rely on is how a broken
// repair shipped once already.
type strictWriteRuntime struct {
	fakeRuntime
	columns   map[string][]string
	execCalls int
	writes    []string
}

func (r *strictWriteRuntime) ExecuteGraphQL(_ context.Context, args map[string]any) (any, error) {
	query, _ := args["query"].(string)
	r.execCalls++
	if err := checkGraphQLParses(query); err != nil {
		return nil, err
	}
	if ContainsMutationOperation(query) {
		clean := graphQLStructure(query)
		for _, root := range MutationRootFields(query) {
			columns, tracked := r.columns[strings.ToLower(root)]
			if !tracked {
				continue
			}
			known := map[string]bool{}
			for _, column := range columns {
				known[strings.ToLower(column)] = true
			}
			for _, keyword := range []string{"insert", "update", "upsert"} {
				for _, span := range mutationInputBlocks(clean, root, keyword) {
					for _, key := range graphQLTopLevelKeys(clean[span[0]:span[1]]) {
						if !known[strings.ToLower(key.name)] {
							return map[string]any{"errors": []any{map[string]any{
								"message": fmt.Sprintf("field '%s' is not a column or a function", key.name),
							}}}, nil
						}
					}
				}
			}
		}
		r.writes = append(r.writes, query)
	}
	return map[string]any{"data": map[string]any{"payments": []any{map[string]any{"id": 900001}}}}, nil
}

func strictWriteTestRuntime(t *testing.T) (*protocolRuntime, *strictWriteRuntime) {
	t.Helper()
	base := &strictWriteRuntime{columns: map[string][]string{
		"payments": {"id", "invoice_id", "amount_cents", "reference", "recorded_at"},
	}}
	profile := &CapabilityProfile{RoleClass: "user", AllowedActions: []string{CapabilityActionDataInsert, CapabilityActionDataUpdate}}
	runtime := newProtocolRuntime(base, "Record payment DEEPORG-PAY-001 for invoice 1", "", 8, profile, nil, CatalogSearchFeatures{})
	runtime.state.seedOK = true
	runtime.state.modelDiscoveryAction = true
	runtime.state.tableColumnNames = map[string][]string{
		"payments": {"id", "invoice_id", "amount_cents", "reference", "recorded_at"},
	}
	runtime.state.tablesDetailed = map[string]bool{"payments": true}
	// Raw GraphQL requires same-run catalog detail and security/runtime
	// evidence; the scenario under test starts after discovery, exactly like
	// the recorded episodes, which had read every prerequisite card and still
	// invented the column names.
	runtime.state.catalogDetails = []string{"table:app:main.payments"}
	runtime.state.securityRuntimeEvidence = true
	return runtime, base
}

const brokenPaymentInsert = `mutation { payments(insert: { id: 900001, payment_reference: "DEEPORG-PAY-001", invoice_id: 1, amount_cents: 480000, paid_at: "2027-01-15T12:00:00Z" }) { id payment_reference } }`

func TestFailedMutationCarriesTheCorrectedWrite(t *testing.T) {
	runtime, base := strictWriteTestRuntime(t)

	out, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": brokenPaymentInsert})
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(out)
	if !strings.Contains(string(payload), "repaired_query") {
		t.Fatalf("a certain rename must hand over the corrected write: %s", payload)
	}
	repaired := runtime.state.lastMutationRepairedQuery
	if repaired == "" {
		t.Fatal("the corrected write should be retained for the finalize gate")
	}
	if err := checkGraphQLParses(repaired); err != nil {
		t.Fatalf("offered mutation does not parse: %v", err)
	}
	// The offer is executable against the strict engine, exactly as given.
	if _, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": repaired}); err != nil {
		t.Fatalf("the offered mutation should execute: %v", err)
	}
	if len(base.writes) != 1 || !strings.Contains(base.writes[0], `reference: "DEEPORG-PAY-001"`) {
		t.Fatalf("the corrected write should land: %v", base.writes)
	}
	// The write accounting settles, so finalize accepts the success report.
	resp := runtime.state.finalize(Response{Status: StatusAnswered, Answer: "Recorded payment DEEPORG-PAY-001."})
	if resp.Status != StatusAnswered {
		t.Fatalf("a landed write finalizes normally, got %s: %+v", resp.Status, resp.Errors)
	}
}

func TestFailedWriteCannotBeReportedAsDone(t *testing.T) {
	runtime, base := strictWriteTestRuntime(t)

	if _, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": brokenPaymentInsert}); err != nil {
		t.Fatal(err)
	}
	if len(base.writes) != 0 {
		t.Fatalf("the broken write must not land: %v", base.writes)
	}
	// The recorded episodes shipped exactly this claim through the exhausted-
	// loop rescue: answered, no violations, database unchanged.
	resp := runtime.state.finalize(Response{Status: StatusAnswered, Answer: "Successfully recorded payment DEEPORG-PAY-001 with id 900001."})
	if resp.Status != StatusBlocked {
		t.Fatalf("a run whose only write failed cannot answer success, got %s", resp.Status)
	}
	found := false
	for _, violation := range runtime.state.violations {
		if violation.Code == "mutation_execution_failed" {
			found = true
			if !violation.Blocking {
				t.Fatal("the false success report must block")
			}
			if repaired := stringFromMap(violation.Details, "repaired_query"); repaired == "" {
				t.Fatal("the violation should carry the corrected write it was refusing over")
			}
		}
	}
	if !found {
		t.Fatalf("expected mutation_execution_failed, got %+v", runtime.state.violations)
	}
	if resp.Refusal == nil {
		t.Fatal("the block should explain itself as a refusal")
	}
}

func TestReadOnlyRunsAreUntouchedByWriteAccounting(t *testing.T) {
	runtime, _ := strictWriteTestRuntime(t)
	if _, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": `query { payments(limit: 5) { id reference } }`}); err != nil {
		t.Fatal(err)
	}
	resp := runtime.state.finalize(Response{Status: StatusAnswered, Answer: "Five payments listed."})
	if resp.Status != StatusAnswered {
		t.Fatalf("a read-only run must finalize normally, got %s", resp.Status)
	}
}

func TestFailedThenFixedWriteFinalizesNormally(t *testing.T) {
	runtime, base := strictWriteTestRuntime(t)
	if _, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": brokenPaymentInsert}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{
		"query": `mutation { payments(insert: { id: 900001, reference: "DEEPORG-PAY-001", invoice_id: 1, amount_cents: 480000, recorded_at: "2027-01-15T12:00:00Z" }) { id } }`,
	}); err != nil {
		t.Fatal(err)
	}
	if len(base.writes) != 1 {
		t.Fatalf("the self-authored fix should land: %v", base.writes)
	}
	resp := runtime.state.finalize(Response{Status: StatusAnswered, Answer: "Recorded payment DEEPORG-PAY-001."})
	if resp.Status != StatusAnswered {
		t.Fatalf("a fixed write finalizes normally, got %s: %+v", resp.Status, resp.Errors)
	}
}

// The finalize bounce is what pushes a model to act on the offer before the
// step budget dies; when the corrected write exists, the bounce names it.
func TestPendingFinalizationNamesTheCorrectedWrite(t *testing.T) {
	runtime, _ := strictWriteTestRuntime(t)
	if _, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": brokenPaymentInsert}); err != nil {
		t.Fatal(err)
	}
	message := runtime.state.pendingRequiredFinalization()
	if !strings.HasPrefix(message, "execution_repair_required:") {
		t.Fatalf("the failed write must keep bouncing finals: %q", message)
	}
	if !strings.Contains(message, "repaired_query") || !strings.Contains(message, "exactly as given") {
		t.Fatalf("the bounce should point at the exact corrected write: %q", message)
	}
}

// The schema encodes the resolution idiom directly — status's observed value
// "resolved" sits beside a resolved_at column — and 75 of 75 recorded ticket
// episodes left that column null. The repairs now surface the fact; the value
// stays the model's to author.
func TestCompanionTimestampNoteNamesResolvedAt(t *testing.T) {
	base := &strictWriteRuntime{columns: map[string][]string{
		"support_tickets": {"id", "status", "resolution_note", "resolved_at", "opened_at"},
	}}
	runtime := newProtocolRuntime(base, "close ticket 2", "", 8, nil, nil, CatalogSearchFeatures{})
	runtime.state.tableColumnNames = map[string][]string{
		"support_tickets": {"id", "status", "resolution_note", "resolved_at", "opened_at"},
	}
	runtime.state.tablesDetailed = map[string]bool{"support_tickets": true}

	note := runtime.companionTimestampNote(context.Background(),
		`mutation { support_tickets(where: {id: {eq: 2}}, update: {status: "resolved", resolution_note: "done"}) { id } }`)
	if !strings.Contains(note, "resolved_at") || !strings.Contains(note, `"resolved"`) {
		t.Fatalf("the note should name the companion column and the transition: %q", note)
	}
	// Already stamped: nothing left to teach.
	if note := runtime.companionTimestampNote(context.Background(),
		`mutation { support_tickets(where: {id: {eq: 2}}, update: {status: "resolved", resolved_at: "2027-01-15T12:00:00Z"}) { id } }`); note != "" {
		t.Fatalf("a stamped transition needs no note: %q", note)
	}
	// A value with no companion column is silent.
	if note := runtime.companionTimestampNote(context.Background(),
		`mutation { support_tickets(where: {id: {eq: 2}}, update: {status: "open"}) { id } }`); strings.Contains(note, "open_at") {
		t.Fatalf("no invented companion columns: %q", note)
	}
}

// The full ticket-resolution chain, in process: the value guard corrects the
// vocabulary and teaches resolved_at, the rename repair corrects the column,
// and the stamped write finalizes cleanly. This is the sequence 0 of 75
// recorded episodes ever completed.
func TestTicketResolutionChainFinalizesAnswered(t *testing.T) {
	base := &strictWriteRuntime{columns: map[string][]string{
		"support_tickets": {"id", "status", "resolution_note", "resolved_at", "opened_at"},
	}}
	profile := &CapabilityProfile{RoleClass: "user", AllowedActions: []string{CapabilityActionDataUpdate}}
	runtime := newProtocolRuntime(base, "close ticket 1 with a note", "", 8, profile, nil, CatalogSearchFeatures{})
	runtime.state.seedOK = true
	runtime.state.modelDiscoveryAction = true
	runtime.state.tableColumnNames = map[string][]string{"support_tickets": {"id", "status", "resolution_note", "resolved_at", "opened_at"}}
	runtime.state.tablesDetailed = map[string]bool{"support_tickets": true}
	runtime.state.catalogDetails = []string{"table:app:main.support_tickets"}
	runtime.state.securityRuntimeEvidence = true
	runtime.state.observedValues = map[string]map[string][]string{
		"support_tickets": {"status": {"open", "pending", "resolved"}},
	}

	out, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{
		"query": `mutation { support_tickets(where: { id: { eq: 1 } }, update: { status: "closed", notes: "Sorted." }) { id status } }`,
	})
	if err != nil {
		t.Fatal(err)
	}
	recovery := mapValue(mapValue(out)["recovery"])
	valueRepair := stringFromMap(mapValue(mapValue(recovery["next"])["args"]), "query")
	if !strings.Contains(valueRepair, `status: "resolved"`) {
		t.Fatalf("the value repair should correct the vocabulary: %q", valueRepair)
	}
	if !strings.Contains(stringFromMap(mapValue(recovery["next"]), "reason"), "resolved_at") {
		t.Fatalf("the value repair should teach the companion timestamp: %+v", recovery)
	}
	out, err = runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": valueRepair})
	if err != nil {
		t.Fatal(err)
	}
	renamed := runtime.state.lastMutationRepairedQuery
	if !strings.Contains(renamed, "resolution_note:") {
		t.Fatalf("the rename repair should correct the column: %q", renamed)
	}
	stamped := strings.Replace(renamed, "resolution_note:", `resolved_at: "2027-01-15T12:00:00Z", resolution_note:`, 1)
	if _, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": stamped}); err != nil {
		t.Fatal(err)
	}
	if len(base.writes) != 1 || !strings.Contains(base.writes[0], "resolved_at") {
		t.Fatalf("the stamped resolution should land: %v", base.writes)
	}
	resp := runtime.state.finalize(Response{Status: StatusAnswered, Answer: "Ticket 1 is resolved with a note."})
	if resp.Status != StatusAnswered {
		t.Fatalf("the completed chain finalizes normally, got %s: %+v", resp.Status, resp.Errors)
	}
	_ = out
}

// A model that violates a guard AFTER its correct query already ran, then
// correctly re-selects that query, receives it from the execution cache — and
// the cached replay must discharge exactly as the original success did. Before
// this, the retry a guard demanded could only ever arrive from the cache, which
// never discharged, so the run was blocked with no path out.
func TestCachedSuccessDischargesLikeAFreshOne(t *testing.T) {
	runtime, base := strictWriteTestRuntime(t)
	good := `mutation { payments(insert: { id: 900001, reference: "DEEPORG-PAY-001", invoice_id: 1, amount_cents: 480000, recorded_at: "2027-01-15T12:00:00Z" }) { id } }`
	if _, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": good}); err != nil {
		t.Fatal(err)
	}
	if len(base.writes) != 1 {
		t.Fatalf("the write should land: %v", base.writes)
	}
	// A refusal recorded after the success: re-sending a write with a value
	// outside the observed set.
	runtime.state.observedValues = map[string]map[string][]string{
		"payments": {"reference": {"DEEPORG-PAY-001"}},
	}
	if _, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{
		"query": `mutation { payments(insert: { id: 900002, reference: "INVENTED-REF", invoice_id: 1, amount_cents: 1, recorded_at: "2027-01-15T12:00:00Z" }) { id } }`,
	}); err != nil {
		t.Fatal(err)
	}
	if !runtime.state.hasBlockingViolation() {
		t.Fatal("the out-of-vocabulary write should be refused")
	}
	// The model re-selects its earlier correct query; the cache serves it.
	if _, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": good}); err != nil {
		t.Fatal(err)
	}
	if len(base.writes) != 1 {
		t.Fatalf("the cached replay must not re-execute the write: %v", base.writes)
	}
	if runtime.state.hasBlockingViolation() {
		t.Fatal("a cached successful execution should discharge like a fresh one")
	}
}
