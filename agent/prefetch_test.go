package agent

import (
	"context"
	"testing"
)

// A run the server already knows the subject of starts with that table's
// catalog detail prepared; consuming it is discovery, and it marks the table
// inspected for a write, so a routine does not spend its steps rediscovering
// what its watch knows.
func TestPrefetchedCatalogIsConsumedAsDiscovery(t *testing.T) {
	rt := &fakeRuntime{}
	a := &Agent{runtime: rt}
	prepared := a.prefetchCatalog(context.Background(), Request{PrefetchCatalogIDs: []string{"table:app:main.support_tickets"}})
	if prepared == nil || len(prepared.reads) != 1 {
		t.Fatalf("prepared = %+v", prepared)
	}
	if ctx := mapValue(prepared.context()); len(anySlice(ctx["detail_ids"])) != 1 && len(prepared.stats.DetailIDs) != 1 {
		t.Fatalf("prepared context = %v", ctx)
	}
	protocol := newProtocolRuntime(rt, "Close duplicate ticket 1.", "", 8, nil, nil, CatalogSearchFeatures{})
	if _, err := prepared.consume(protocol); err != nil {
		t.Fatal(err)
	}
	if !protocol.state.modelDiscoveryAction || !protocol.state.tablesDetailed["support_tickets"] {
		t.Fatalf("discovery=%v detailed=%v", protocol.state.modelDiscoveryAction, protocol.state.tablesDetailed)
	}
	// A detail the catalog does not return prepares nothing.
	empty := &fakeRuntime{catalogOverride: func(map[string]any) any { return map[string]any{"cards": []any{}} }}
	if got := (&Agent{runtime: empty}).prefetchCatalog(context.Background(), Request{PrefetchCatalogIDs: []string{"table:app:main.gone"}}); got != nil {
		t.Fatalf("an unknown table prepared %+v", got)
	}
}

// Rows GraphJin read and handed the run are evidence it already holds: a
// verdict drawn from them answers without more discovery, and citing them is
// grounded. Nothing handed, nothing excused.
func TestHandedRowsGroundAnAnswerWithoutDiscovery(t *testing.T) {
	answer := "No change is needed: the subject \"Is this a duplicate charge on my invoice?\" does not start with DUPLICATE:."
	rows := map[string]any{"support_tickets": []any{map[string]any{"id": 1, "subject": "Is this a duplicate charge on my invoice?"}}}

	bare := newDiscoveryState("triage the ticket")
	bare.seedOK = true
	if resp := bare.finalize(Response{Status: StatusAnswered, Answer: answer}); resp.Status != StatusBlocked {
		t.Fatalf("an answer with no discovery and nothing handed = %s", resp.Status)
	}

	handed := newDiscoveryState("triage the ticket")
	handed.seedOK = true
	handed.observeHandedRows(rows)
	if resp := handed.finalize(Response{Status: StatusAnswered, Answer: answer}); resp.Status != StatusAnswered {
		t.Fatalf("an answer drawn from handed rows = %s %+v", resp.Status, resp.Refusal)
	}

	// They ground what they contain, not a field nobody observed.
	invented := newDiscoveryState("triage the ticket")
	invented.seedOK = true
	invented.observeHandedRows(rows)
	if resp := invented.finalize(Response{Status: StatusAnswered, Answer: "No change: its duplicate_of_ticket_id is empty."}); resp.Status != StatusBlocked {
		t.Fatalf("an answer citing an unobserved field = %s", resp.Status)
	}

	for _, empty := range []any{nil, map[string]any{}, []any{}, " "} {
		s := newDiscoveryState("triage the ticket")
		s.seedOK = true
		s.observeHandedRows(empty)
		if s.handedRows {
			t.Fatalf("%#v counted as handed rows", empty)
		}
	}
}
