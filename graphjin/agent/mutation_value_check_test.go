package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// The mutations here are verbatim from benchmark generation 2028.1, where 19 of 30
// action episodes wrote status "closed" into a column whose values are open,
// pending, and resolved.

func TestMutationStringAssignments(t *testing.T) {
	got := mutationStringAssignments(`mutation { support_tickets(where: {id: {eq: 2}}, update: {status: "closed", resolution_notes: "Resolved and sorted out"}) { id status } }`)
	if len(got) != 2 {
		t.Fatalf("expected two assignments, got %+v", got)
	}
	byColumn := map[string]string{}
	for _, a := range got {
		if a.Table != "support_tickets" {
			t.Errorf("assignment %+v has the wrong table", a)
		}
		byColumn[a.Column] = a.Value
	}
	if byColumn["status"] != "closed" || byColumn["resolution_notes"] != "Resolved and sorted out" {
		t.Fatalf("assignments = %v", byColumn)
	}
}

func TestMutationStringAssignmentsIgnoresWhereAndReads(t *testing.T) {
	// A where clause is a filter, not an assignment: treating it as one would
	// flag every correctly-filtered update.
	got := mutationStringAssignments(`mutation { support_tickets(where: {status: {eq: "nonexistent"}}, update: {status: "resolved"}) { id } }`)
	if len(got) != 1 || got[0].Column != "status" || got[0].Value != "resolved" {
		t.Fatalf("only the update assignment counts, got %+v", got)
	}

	if got := mutationStringAssignments(`query { support_tickets(where: {status: {eq: "open"}}) { id } }`); len(got) != 0 {
		t.Fatalf("a read has no assignments, got %+v", got)
	}
	// System roots carry their own guards and shapes.
	if got := mutationStringAssignments(`mutation { gj_watch(insert: {name: "w", query: "subscription { x }"}) { id } }`); len(got) != 0 {
		t.Fatalf("system roots are out of scope, got %+v", got)
	}
	// Escapes must survive so the compared value is what the database receives.
	got = mutationStringAssignments(`mutation { support_tickets(update: {notes: "he said \"done\""}) { id } }`)
	if len(got) != 1 || got[0].Value != `he said "done"` {
		t.Fatalf("escaped value not decoded: %+v", got)
	}
}

// columnCardRuntime serves column cards carrying sampled values, as the live
// catalog does once the enum sampler has run.
type columnCardRuntime struct {
	fakeRuntime
	catalogCalls  int
	mutationCalls int
	values        map[string][]string
}

func (r *columnCardRuntime) ExecuteGraphQL(_ context.Context, args map[string]any) (any, error) {
	r.mutationCalls++
	return map[string]any{"data": map[string]any{"support_tickets": []any{map[string]any{"id": 2, "status": "closed"}}}}, nil
}

func (r *columnCardRuntime) QueryCatalog(_ context.Context, args map[string]any) (any, error) {
	r.catalogCalls++
	// The live catalog reads kind and table as explicit arguments and returns
	// nothing for a raw where object it cannot validate — silently, with no error.
	// Answering only the shorthand is what makes that regression visible here.
	if _, ok := args["where"]; ok {
		return map[string]any{"cards": []any{}}, nil
	}
	// A filtered read returns summaries with evidence_json blanked; payloads travel
	// only on detail lookups by id. Serving evidence only for an ids request is what
	// makes that distinction fail loudly here instead of silently finding nothing,
	// which is what happened across four benchmark runs.
	if _, byID := args["ids"]; !byID {
		if table, _ := args["table"].(string); table != "support_tickets" {
			return map[string]any{"cards": []any{}}, nil
		}
		return map[string]any{"cards": []any{map[string]any{
			"id": "column:app:main.support_tickets.status", "kind": "column", "table_name": "support_tickets",
		}}}, nil
	}
	cards := []any{}
	for column, values := range r.values {
		evidence, _ := json.Marshal(map[string]any{"ColumnName": column, "observed_values": values})
		cards = append(cards, map[string]any{
			"id":            "column:app:main.support_tickets." + column,
			"kind":          "column",
			"table_name":    "support_tickets",
			"evidence_json": string(evidence),
		})
	}
	return map[string]any{"cards": cards}, nil
}

func ticketValueRuntime(t *testing.T) (*protocolRuntime, *columnCardRuntime) {
	t.Helper()
	base := &columnCardRuntime{values: map[string][]string{"status": {"open", "pending", "resolved"}}}
	profile := &CapabilityProfile{RoleClass: "user", AllowedActions: []string{CapabilityActionDataUpdate}}
	return newProtocolRuntime(base, "Ticket 2 has been sorted out, close it off.", "", 8, profile, nil, CatalogSearchFeatures{}), base
}

func TestUnobservedWrittenValuesCatchesClosedStatus(t *testing.T) {
	runtime, base := ticketValueRuntime(t)
	query := `mutation { support_tickets(where: {id: {eq: 2}}, update: {status: "closed", notes: "sorted out"}) { id status } }`

	mismatches := runtime.unobservedWrittenValues(context.Background(), query)
	if len(mismatches) != 1 || mismatches[0].Column != "status" || mismatches[0].Value != "closed" {
		t.Fatalf("expected the status mismatch, got %+v", mismatches)
	}
	// notes has no sampled set, so it must not be second-guessed.
	described := runtime.describeUnobservedValues(context.Background(), mismatches)
	for _, want := range []string{"closed", "open, pending, resolved", "support_tickets.status"} {
		if !strings.Contains(described, want) {
			t.Errorf("message must contain %q: %s", want, described)
		}
	}
	// The lookup is cached: a write touching several columns of one table costs one
	// catalog read, not one per column.
	before := base.catalogCalls
	runtime.unobservedWrittenValues(context.Background(), query)
	if base.catalogCalls != before {
		t.Fatalf("expected the value lookup to be cached, calls went %d -> %d", before, base.catalogCalls)
	}
}

func TestUnobservedWrittenValuesStaysQuiet(t *testing.T) {
	for _, tc := range []struct{ name, query string }{
		{"value already in use", `mutation { support_tickets(where: {id: {eq: 2}}, update: {status: "resolved"}) { id } }`},
		{"case-insensitive match", `mutation { support_tickets(where: {id: {eq: 2}}, update: {status: "Resolved"}) { id } }`},
		{"column has no sampled set", `mutation { support_tickets(where: {id: {eq: 2}}, update: {notes: "anything at all"}) { id } }`},
		{"read-only query", `query { support_tickets { id status } }`},
	} {
		runtime, _ := ticketValueRuntime(t)
		if got := runtime.unobservedWrittenValues(context.Background(), tc.query); len(got) != 0 {
			t.Errorf("%s: must stay quiet, got %+v", tc.name, got)
		}
	}
}

// TestObservedValueNoticeRefusesAnIdenticalRetry corrects the rule this test used to
// assert. Letting the next attempt through, whatever it contained, was meant to keep
// sampled values honest as evidence — but the measured behaviour was the model
// re-sending the same write unchanged. Of eleven episodes told a ticket status column
// holds open, pending and resolved, ten resubmitted "closed" verbatim and it landed.
//
// A different write still proceeds, which is what keeps the sample evidence rather
// than schema; repeating a rejected one is not a considered decision.
func TestObservedValueNoticeRefusesAnIdenticalRetry(t *testing.T) {
	runtime, base := ticketValueRuntime(t)
	runtime.state.seedOK = true
	runtime.state.modelDiscoveryAction = true
	runtime.state.securityRuntimeEvidence = true
	runtime.state.mutationEvidenceSupplied = true
	runtime.state.tablesDetailed["support_tickets"] = true
	runtime.state.catalogDetails = []string{"table:app:main.support_tickets"}
	args := map[string]any{"query": `mutation { support_tickets(where: {id: {eq: 2}}, update: {status: "closed"}) { id status } }`}

	// The refusal now THROWS: executor code is straight-line JavaScript, so a
	// value-return cannot interrupt `await execute_graphql(m); await
	// final("done")` — only an exception can. The message carries everything
	// the old recovery payload did, corrected mutation included.
	_, err := runtime.ExecuteGraphQL(context.Background(), cloneAnyMap(args))
	if err == nil {
		t.Fatal("the out-of-vocabulary write must throw")
	}
	if !strings.Contains(err.Error(), "did NOT execute") {
		t.Fatalf("the exception must lead with the fact that nothing ran: %v", err)
	}
	// The message must not advertise the bypass. Offering "or re-execute this
	// write unchanged" alongside the instruction is the option models took.
	if strings.Contains(err.Error(), "unchanged") {
		t.Fatalf("the exception must not present re-sending unchanged as an equal choice: %v", err)
	}
	if !strings.Contains(err.Error(), "open, pending, resolved") {
		t.Fatalf("the exception must name the values to choose from: %v", err)
	}
	if base.mutationCalls != 0 {
		t.Fatalf("the write must not reach the database, calls=%d", base.mutationCalls)
	}

	// The identical retry is refused again — and it consumes the second strike,
	// exactly like a reworded one.
	if _, err := runtime.ExecuteGraphQL(context.Background(), cloneAnyMap(args)); err == nil {
		t.Fatal("an identical retry must be refused again")
	}
	if base.mutationCalls != 0 {
		t.Fatalf("an identical retry must not reach the database, calls=%d", base.mutationCalls)
	}

	// With a clear-winner repair in hand the refusal stands past the strike
	// cap, and the exception text itself carries the corrected mutation.
	persisted := map[string]any{"query": `mutation { support_tickets(where: {id: {eq: 2}}, update: {status: "closed", resolution_note: "the customer insists on closed"}) { id status } }`}
	_, err = runtime.ExecuteGraphQL(context.Background(), persisted)
	if err == nil {
		t.Fatal("third attempt with a repair in hand must keep refusing")
	}
	if base.mutationCalls != 0 {
		t.Fatalf("an out-of-vocabulary write with a known repair must never reach the database, calls=%d", base.mutationCalls)
	}
	repairedQuery := correctedMutationFromError(t, err)
	if !strings.Contains(repairedQuery, `"resolved"`) {
		t.Fatalf("the exception must carry the corrected query, got %q", repairedQuery)
	}
	if _, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": repairedQuery}); err != nil {
		t.Fatalf("the repaired write should execute: %v", err)
	}
	if base.mutationCalls != 1 {
		t.Fatalf("expected the repaired write to execute, calls=%d", base.mutationCalls)
	}
}

// correctedMutationFromError extracts the corrected mutation the exception
// text carries, from "Execute this corrected mutation exactly as given: ..."
// to the end of the mutation (the message may append a schema note after an
// em dash separator).
func correctedMutationFromError(t *testing.T, err error) string {
	t.Helper()
	_, after, found := strings.Cut(err.Error(), "exactly as given: ")
	if !found {
		t.Fatalf("exception does not carry the corrected mutation: %v", err)
	}
	if query, _, split := strings.Cut(after, " — "); split {
		return strings.TrimSpace(query)
	}
	return strings.TrimSpace(after)
}

// When no clear-winner repair exists — the written value resembles nothing the
// sampler observed — the dcea36f8 let-through survives: two corrections are
// friction, not schema, and the caller may genuinely know a value the sampler
// has not seen.
func TestObservedValueListOnlyMismatchStillLandsOnThirdAttempt(t *testing.T) {
	runtime, base := ticketValueRuntime(t)
	runtime.state.seedOK = true
	runtime.state.modelDiscoveryAction = true
	runtime.state.securityRuntimeEvidence = true
	runtime.state.mutationEvidenceSupplied = true
	runtime.state.tablesDetailed["support_tickets"] = true
	runtime.state.catalogDetails = []string{"table:app:main.support_tickets"}
	args := map[string]any{"query": `mutation { support_tickets(where: {id: {eq: 2}}, update: {status: "xx"}) { id status } }`}

	for attempt := 1; attempt <= 2; attempt++ {
		_, err := runtime.ExecuteGraphQL(context.Background(), cloneAnyMap(args))
		if err == nil {
			t.Fatalf("attempt %d should be refused with an exception", attempt)
		}
		if !strings.Contains(err.Error(), "did NOT execute") {
			t.Fatalf("attempt %d must lead with the fact that nothing ran: %v", attempt, err)
		}
		if strings.Contains(err.Error(), "exactly as given") {
			t.Fatalf("no repair should exist for %q: %v", "xx", err)
		}
	}
	if base.mutationCalls != 0 {
		t.Fatalf("refused attempts must not reach the database, calls=%d", base.mutationCalls)
	}
	if _, err := runtime.ExecuteGraphQL(context.Background(), cloneAnyMap(args)); err != nil {
		t.Fatalf("the third list-only attempt should proceed: %v", err)
	}
	if base.mutationCalls != 1 {
		t.Fatalf("expected the third list-only attempt to execute, calls=%d", base.mutationCalls)
	}
}

// TestColumnNameFromCatalogID pins the identifier the lookup relies on. The card id
// always carries the column; column_name depends on which fields a catalog surface
// projects, so reading only that field made the check silently find nothing.
func TestColumnNameFromCatalogID(t *testing.T) {
	for id, want := range map[string]string{
		"column:app:main.support_tickets.status": "status",
		"column:app:public.orders.total_cents":   "total_cents",
		"table:app:main.support_tickets":         "",
		"":                                       "",
		"column:":                                "",
	} {
		if got := columnNameFromCatalogID(id); got != want {
			t.Errorf("columnNameFromCatalogID(%q) = %q, want %q", id, got, want)
		}
	}
}

// TestObservedValueLookupIsRecorded makes a silent check legible. This one stayed
// quiet across three full benchmark runs and three wrong diagnoses, every one of
// them reached by inspection because nothing in the trajectory distinguished "found
// no columns" from "found them and the value matched".
func TestObservedValueLookupIsRecorded(t *testing.T) {
	runtime, _ := ticketValueRuntime(t)
	runtime.unobservedWrittenValues(context.Background(),
		`mutation { support_tickets(where: {id: {eq: 2}}, update: {status: "resolved"}) { id } }`)

	if len(runtime.state.observedValueLookups) != 1 {
		t.Fatalf("expected one recorded lookup, got %+v", runtime.state.observedValueLookups)
	}
	record := runtime.state.observedValueLookups[0]
	if record["table"] != "support_tickets" {
		t.Errorf("lookup should name the table, got %v", record["table"])
	}
	if record["columns_with_values"] != 1 {
		t.Errorf("lookup should report the columns it found values for, got %v", record["columns_with_values"])
	}
	// Cards returned is what separates a filter that finds nothing from cards that
	// arrive without their evidence — the two remaining explanations for a silent check.
	if record["cards_returned"] != 1 {
		t.Errorf("lookup should report how many cards came back, got %v", record["cards_returned"])
	}
	if id, _ := record["first_card_id"].(string); id == "" {
		t.Error("lookup should record a card id so the returned rows can be identified")
	}

	// A table the catalog knows nothing about records the empty result rather than
	// nothing at all — that is the case three diagnoses could not see.
	other, _ := ticketValueRuntime(t)
	other.observedColumnValues(context.Background(), "unknown_table")
	if len(other.state.observedValueLookups) != 1 || other.state.observedValueLookups[0]["columns_with_values"] != 0 {
		t.Fatalf("an empty lookup must still be recorded, got %+v", other.state.observedValueLookups)
	}
}

// TestClosestObservedValuePicksTheMeasuredCase pins the mapping the whole family
// turns on — "closed" against a column holding open, pending and resolved — and the
// shapes where suggesting would be guessing.
func TestClosestObservedValuePicksTheMeasuredCase(t *testing.T) {
	ticket := []string{"open", "pending", "resolved"}
	if got, ok := closestObservedValue("closed", ticket); !ok || got != "resolved" {
		t.Fatalf(`closest to "closed" = %q ok=%v, want "resolved"`, got, ok)
	}
	// Nothing plausibly near: no suggestion, so the plain list stands.
	if got, ok := closestObservedValue("done", ticket); ok {
		t.Fatalf(`"done" has no clear neighbour, got %q`, got)
	}
	// A tie is ambiguity, and ambiguity is not a suggestion.
	if got, ok := closestObservedValue("ab", []string{"abc", "abd"}); ok {
		t.Fatalf("a tie must not suggest, got %q", got)
	}
}

// TestSubstituteWrittenValuesRewritesOnlyTheMismatch pins the surgical property:
// the suggested value replaces exactly the rejected literal, and the rest of the
// mutation — the note, the where clause — comes through byte for byte.
func TestSubstituteWrittenValuesRewritesOnlyTheMismatch(t *testing.T) {
	query := `mutation { support_tickets(where: {id: {eq: 2}}, update: {status: "closed", resolution_note: "closed it as asked"}) { id status } }`
	mismatches := []columnAssignment{{Table: "support_tickets", Column: "status", Value: "closed"}}
	repaired, ok := substituteWrittenValues(query, mismatches, map[string]string{"support_tickets.status": "resolved"})
	if !ok {
		t.Fatal("expected a substitution")
	}
	if !strings.Contains(repaired, `status: "resolved"`) {
		t.Fatalf("status not rewritten: %s", repaired)
	}
	// The same word elsewhere is untouched: only the mismatched literal moves.
	if !strings.Contains(repaired, `resolution_note: "closed it as asked"`) {
		t.Fatalf("note must survive byte for byte: %s", repaired)
	}
	if !strings.Contains(repaired, `where: {id: {eq: 2}}`) {
		t.Fatalf("where clause must survive: %s", repaired)
	}

	// All or nothing: a mismatch without a suggestion withholds the repair.
	two := []columnAssignment{
		{Table: "support_tickets", Column: "status", Value: "closed"},
		{Table: "support_tickets", Column: "severity", Value: "sev1"},
	}
	if _, ok := substituteWrittenValues(query, two, map[string]string{"support_tickets.status": "resolved"}); ok {
		t.Fatal("a partly suggested batch must not produce a repaired query")
	}

	// A suggested value with a quote must arrive escaped, not break the mutation.
	quoted, ok := substituteWrittenValues(query, mismatches, map[string]string{"support_tickets.status": `won"t fix`})
	if !ok || !strings.Contains(quoted, `status: "won\"t fix"`) {
		t.Fatalf("escaping lost: %s ok=%v", quoted, ok)
	}
}

// TestObservedValueNoticeCarriesTheCorrectedWrite is the end-to-end shape: the
// first refusal names the nearest value and hands back the corrected mutation.
func TestObservedValueNoticeCarriesTheCorrectedWrite(t *testing.T) {
	runtime, _ := ticketValueRuntime(t)
	runtime.state.seedOK = true
	runtime.state.modelDiscoveryAction = true
	runtime.state.securityRuntimeEvidence = true
	runtime.state.mutationEvidenceSupplied = true
	runtime.state.tablesDetailed["support_tickets"] = true
	runtime.state.catalogDetails = []string{"table:app:main.support_tickets"}
	args := map[string]any{"query": `mutation { support_tickets(where: {id: {eq: 2}}, update: {status: "closed"}) { id status } }`}

	// The refusal throws, and the exception text is the whole channel: it
	// names the observed vocabulary and carries the corrected mutation inline.
	_, err := runtime.ExecuteGraphQL(context.Background(), cloneAnyMap(args))
	if err == nil {
		t.Fatal("the out-of-vocabulary write must throw")
	}
	for _, want := range []string{"did NOT execute", "closest of these", "exactly as given"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("exception must carry %q: %v", want, err)
		}
	}
	repairedText := correctedMutationFromError(t, err)
	if !strings.Contains(repairedText, `status: "resolved"`) {
		t.Fatalf("corrected mutation should carry the suggested value: %q", repairedText)
	}
	// The violation record still carries the structured details for scoring.
	found := false
	for _, violation := range runtime.state.violations {
		if violation.Code == "observed_value_mismatch" {
			found = true
			if stringFromMap(violation.Details, "repaired_query") == "" {
				t.Fatalf("the violation should retain the repaired query: %+v", violation.Details)
			}
		}
	}
	if !found {
		t.Fatal("the refusal should record its violation")
	}
}
