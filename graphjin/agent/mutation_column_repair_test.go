package agent

import (
	"context"
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
	queries   []string
}

func (r *strictWriteRuntime) ExecuteGraphQL(_ context.Context, args map[string]any) (any, error) {
	query, _ := args["query"].(string)
	r.execCalls++
	r.queries = append(r.queries, query)
	if err := checkGraphQLParses(query); err != nil {
		return nil, err
	}
	if ContainsMutationOperation(query) {
		clean := graphQLStructure(query)
		for _, root := range MutationRootFields(query) {
			// Core lowers a Hasura root before compiling, so the fake resolves
			// it the same way before checking columns.
			columns, tracked := r.columns[strings.ToLower(hasuraBaseRoot(root))]
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
	if !ContainsMutationOperation(query) {
		clean := graphQLStructure(query)
		for _, root := range QueryRootFields(query) {
			columns, tracked := r.columns[strings.ToLower(root)]
			if !tracked {
				continue
			}
			known := map[string]bool{}
			for _, column := range columns {
				known[strings.ToLower(column)] = true
			}
			if unknown := unknownSelectionField(clean, root, known); unknown != "" {
				return map[string]any{"errors": []any{map[string]any{
					"message": fmt.Sprintf("field '%s' is not a column or a function", unknown),
				}}}, nil
			}
		}
	}
	return map[string]any{"data": map[string]any{"payments": []any{map[string]any{"id": 900001}}}}, nil
}

// unknownSelectionField scans a root's selection for a flat field naming no
// real column, the way the engine's compiler does. Aggregate forms and
// protocol fields pass through.
func unknownSelectionField(clean, root string, known map[string]bool) string {
	lower := strings.ToLower(clean)
	index := strings.Index(lower, strings.ToLower(root))
	for index >= 0 {
		end := index + len(root)
		if (index > 0 && isGraphQLNameContinue(clean[index-1])) || (end < len(clean) && isGraphQLNameContinue(clean[end])) {
			next := strings.Index(lower[end:], strings.ToLower(root))
			if next < 0 {
				return ""
			}
			index = end + next
			continue
		}
		open := skipGraphQLSpace(clean, end)
		if open < len(clean) && clean[open] == '(' {
			open = skipGraphQLSpace(clean, matchingGraphQLDelimiter(clean, open, '(', ')')+1)
		}
		if open >= len(clean) || clean[open] != '{' {
			return ""
		}
		close := matchingGraphQLDelimiter(clean, open, '{', '}')
		body := clean[open+1 : close]
		depth := 0
		for i := 0; i < len(body); i++ {
			switch c := body[i]; {
			case c == '{' || c == '(':
				depth++
			case c == '}' || c == ')':
				depth--
			case depth == 0 && isGraphQLNameContinue(c):
				start := i
				for i < len(body) && isGraphQLNameContinue(body[i]) {
					i++
				}
				name := strings.ToLower(body[start:i])
				next := skipGraphQLSpace(body, i)
				if next < len(body) && (body[next] == ':' || body[next] == '{' || body[next] == '(') {
					i--
					continue
				}
				aggregate := false
				for _, prefix := range []string{"count_", "sum_", "avg_", "max_", "min_"} {
					if strings.HasPrefix(name, prefix) {
						aggregate = true
					}
				}
				if !aggregate && !known[name] {
					return body[start:i]
				}
				i--
			}
		}
		return ""
	}
	return ""
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

	_, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": brokenPaymentInsert})
	if err == nil {
		t.Fatal("a dataless engine failure must throw")
	}
	repaired := correctedMutationFromError(t, err)
	if repaired != runtime.state.lastMutationRepairedQuery {
		t.Fatalf("the thrown correction %q must match the retained one %q", repaired, runtime.state.lastMutationRepairedQuery)
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

	if _, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": brokenPaymentInsert}); err == nil || !strings.Contains(err.Error(), "did NOT return data") {
		t.Fatalf("a dataless engine failure must throw: %v", err)
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
	if _, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": brokenPaymentInsert}); err == nil {
		t.Fatal("the broken write must throw")
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
	if _, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": brokenPaymentInsert}); err == nil {
		t.Fatal("the broken write must throw")
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

	_, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{
		"query": `mutation { support_tickets(where: { id: { eq: 1 } }, update: { status: "closed", notes: "Sorted." }) { id status } }`,
	})
	if err == nil {
		t.Fatal("the out-of-vocabulary write must throw")
	}
	if !strings.Contains(err.Error(), "resolved_at") {
		t.Fatalf("the exception should teach the companion timestamp: %v", err)
	}
	valueRepair := correctedMutationFromError(t, err)
	if !strings.Contains(valueRepair, `status: "resolved"`) {
		t.Fatalf("the exception should carry the corrected vocabulary: %q", valueRepair)
	}
	_, err = runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": valueRepair})
	if err == nil {
		t.Fatal("the wrong-column write must throw")
	}
	renamed := runtime.state.lastMutationRepairedQuery
	if thrown := correctedMutationFromError(t, err); thrown != renamed {
		t.Fatalf("the thrown correction %q must match the retained one %q", thrown, renamed)
	}
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
	}); err == nil {
		t.Fatal("the out-of-vocabulary write should throw")
	}
	if !runtime.state.hasBlockingViolation() {
		t.Fatal("the refused write should record its violation")
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

// catalogOverrideForSupply makes the fake catalog answer the security/runtime
// supply with real-looking cards, so the supply path engages as it does live.
func (r *protocolRuntime) catalogOverrideForSupply(base *strictWriteRuntime) {
	base.catalogOverride = func(args map[string]any) any {
		ids, _ := args["ids"].([]any)
		cards := make([]any, 0, len(ids))
		for _, id := range ids {
			cards = append(cards, map[string]any{"id": id, "kind": "help", "summary": "guidance"})
		}
		return map[string]any{"cards": cards}
	}
}

// Run ab4-trt exposed the gate's blind spot: 26 action episodes made exactly
// one write call, had it consumed by the security-evidence supply, and
// finalized "Ticket 1 has been closed with a note" over a database that never
// changed — invisible because nothing marked the attempt. An intercepted write
// is an attempted write.
func TestSuppliedPrerequisiteLetsTheWriteLand(t *testing.T) {
	runtime, base := strictWriteTestRuntime(t)
	// The run has NOT read security/runtime guidance. GraphJin can fetch that
	// itself — the two ids never vary — so the prerequisite is satisfied and
	// the write runs in the same turn rather than paying a round trip to
	// re-arrive at a call already decided to be permitted.
	runtime.state.securityRuntimeEvidence = false
	runtime.catalogOverrideForSupply(base)

	out, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{
		"query": `mutation { payments(insert: { id: 900001, reference: "DEEPORG-PAY-001", invoice_id: 1, amount_cents: 480000, recorded_at: "2027-01-15T12:00:00Z" }) { id } }`,
	})
	if err != nil {
		t.Fatalf("a satisfiable prerequisite must not refuse the write: %v", err)
	}
	if len(base.writes) != 1 {
		t.Fatalf("the write should land once the prerequisite is met: %v", base.writes)
	}
	if !runtime.state.securityRuntimeEvidence {
		t.Fatal("the supplied guidance must count as evidence")
	}
	// It says so on the guidance channel, which informs without discarding the
	// result the way an exception would.
	if got := stringFromMap(mapValue(out), "guidance"); !strings.Contains(got, "security and runtime guidance") {
		t.Fatalf("guidance = %q, want it to name the supplied prerequisite", got)
	}
	if runtime.state.hasBlockingViolation() {
		t.Fatalf("a supplied prerequisite records no violation: %+v", runtime.state.violations)
	}
	// The write really happened, so reporting it done is now earned rather
	// than narrated over an interception.
	resp := runtime.state.finalize(Response{Status: StatusAnswered, Answer: "Successfully recorded payment DEEPORG-PAY-001."})
	if resp.Status != StatusAnswered {
		t.Fatalf("a landed write finalizes normally, got %s: %+v", resp.Status, resp.Errors)
	}
}

// The same run: a repeat after the write landed is served from the memo rather
// than executed twice, and the success report stays earned.
func TestSuppliedWriteRepeatDoesNotWriteTwice(t *testing.T) {
	runtime, base := strictWriteTestRuntime(t)
	runtime.state.securityRuntimeEvidence = false
	runtime.catalogOverrideForSupply(base)

	write := map[string]any{
		"query": `mutation { payments(insert: { id: 900001, reference: "DEEPORG-PAY-001", invoice_id: 1, amount_cents: 480000, recorded_at: "2027-01-15T12:00:00Z" }) { id } }`,
	}
	if _, err := runtime.ExecuteGraphQL(context.Background(), write); err != nil {
		t.Fatal(err)
	}
	// A repeat of a landed write is served from the memo, so it stays at one
	// write no matter how many times the model re-sends it.
	if _, err := runtime.ExecuteGraphQL(context.Background(), write); err != nil {
		t.Fatal(err)
	}
	if len(base.writes) != 1 {
		t.Fatalf("the write should land exactly once: %v", base.writes)
	}
	resp := runtime.state.finalize(Response{Status: StatusAnswered, Answer: "Recorded payment DEEPORG-PAY-001."})
	if resp.Status != StatusAnswered {
		t.Fatalf("a landed retry finalizes normally, got %s: %+v", resp.Status, resp.Errors)
	}
}

// Core lowers Hasura mutation syntax before compiling, so a `_set:` write
// executes exactly like a native `update:` one. That makes it a hole in every
// guard that reads a write's input by argument name: without the aliases, an
// out-of-vocabulary value, an invented column, or a write that never landed
// would all sail past the checks a native write cannot escape.
func TestHasuraSyntaxWritesAreGuardedLikeNativeOnes(t *testing.T) {
	// The value guard reads the written values.
	runtime, _ := ticketValueRuntime(t)
	runtime.state.seedOK = true
	runtime.state.modelDiscoveryAction = true
	runtime.state.securityRuntimeEvidence = true
	runtime.state.mutationEvidenceSupplied = true
	runtime.state.tablesDetailed["support_tickets"] = true
	runtime.state.catalogDetails = []string{"table:app:main.support_tickets"}

	_, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{
		"query": `mutation { update_support_tickets(where: {id: {_eq: 2}}, _set: {status: "closed"}) { affected_rows } }`,
	})
	if err == nil {
		t.Fatal("a Hasura-syntax write with an out-of-vocabulary value must be refused")
	}
	for _, want := range []string{"did NOT execute", "open, pending, resolved", "exactly as given"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal must carry %q: %v", want, err)
		}
	}
	// And the corrected write it hands back keeps the caller's own dialect.
	repaired := correctedMutationFromError(t, err)
	if !strings.Contains(repaired, "_set:") || !strings.Contains(repaired, `status: "resolved"`) {
		t.Fatalf("the corrected write should stay in the caller's dialect: %q", repaired)
	}
	// Offered as "execute exactly as given", so it has to survive the same
	// parse-level check every other handed-over query does.
	if err := checkGraphQLParses(repaired); err != nil {
		t.Fatalf("the corrected Hasura write must parse: %v", err)
	}
}

// The unknown-column repair reads the same input blocks.
func TestHasuraSyntaxWriteGetsTheColumnRepair(t *testing.T) {
	columns := []string{"id", "invoice_id", "amount_cents", "reference", "recorded_at"}
	repaired, renames, ok := repairUnknownMutationColumns(
		`mutation { insert_payments(objects: { id: 900001, payment_reference: "P-1", recorded_at: "t" }) { id } }`, columns)
	if !ok {
		t.Fatal("a Hasura-syntax insert must be repairable like a native one")
	}
	if !strings.Contains(repaired, `reference: "P-1"`) || strings.Contains(repaired, "payment_reference") {
		t.Fatalf("the repair should correct the column inside objects: %q", repaired)
	}
	if len(renames) != 1 {
		t.Fatalf("renames = %+v", renames)
	}
}

// And the write-accounting behind the honesty gate sees it too.
func TestHasuraSyntaxWriteCannotBeReportedAsDone(t *testing.T) {
	runtime, base := strictWriteTestRuntime(t)
	_, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{
		"query": `mutation { insert_payments(objects: { id: 900001, payment_reference: "P-1", invoice_id: 1, amount_cents: 1, recorded_at: "t" }) { id } }`,
	})
	if err == nil {
		t.Fatal("the invented column must be refused")
	}
	if len(base.writes) != 0 {
		t.Fatalf("nothing may land: %v", base.writes)
	}
	resp := runtime.state.finalize(Response{Status: StatusAnswered, Answer: "Recorded the payment."})
	if resp.Status != StatusBlocked {
		t.Fatalf("a failed Hasura-syntax write cannot be reported done, got %s", resp.Status)
	}
}

// hasuraBaseRoot strips the Hasura verb prefix and _by_pk suffix the way core's
// lowering does, so the strict fake validates columns against the real table.
func hasuraBaseRoot(root string) string {
	name := strings.ToLower(strings.TrimSpace(root))
	for _, prefix := range []string{"insert_", "update_", "delete_"} {
		if strings.HasPrefix(name, prefix) {
			return strings.TrimSuffix(strings.TrimPrefix(name, prefix), "_by_pk")
		}
	}
	return name
}
