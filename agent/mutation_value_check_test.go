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

// TestObservedValueNoticeFiresOnceThenAllowsTheWrite is the property that keeps
// sampled values honest as evidence: a caller who means the new value must be able
// to proceed, because the sample is what the data contains, not what the column
// allows.
func TestObservedValueNoticeFiresOnceThenAllowsTheWrite(t *testing.T) {
	runtime, base := ticketValueRuntime(t)
	runtime.state.seedOK = true
	runtime.state.modelDiscoveryAction = true
	runtime.state.securityRuntimeEvidence = true
	runtime.state.mutationEvidenceSupplied = true
	runtime.state.tablesDetailed["support_tickets"] = true
	runtime.state.catalogDetails = []string{"table:app:main.support_tickets"}
	args := map[string]any{"query": `mutation { support_tickets(where: {id: {eq: 2}}, update: {status: "closed"}) { id status } }`}

	first, err := runtime.ExecuteGraphQL(context.Background(), cloneAnyMap(args))
	if err != nil {
		t.Fatalf("first attempt errored instead of returning a repair: %v", err)
	}
	payload, _ := json.Marshal(first)
	if !strings.Contains(string(payload), "observed_value_mismatch") {
		t.Fatalf("first attempt should surface the vocabulary notice: %s", payload)
	}
	if base.mutationCalls != 0 {
		t.Fatalf("the write must not reach the database on the notice, calls=%d", base.mutationCalls)
	}

	if _, err := runtime.ExecuteGraphQL(context.Background(), cloneAnyMap(args)); err != nil {
		t.Fatalf("the repeated write should proceed: %v", err)
	}
	if base.mutationCalls != 1 {
		t.Fatalf("expected the second attempt to execute, calls=%d", base.mutationCalls)
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
