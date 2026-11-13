package agent

import (
	"context"
	"strings"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
)

// A run the server already knows the subject of starts with that table's
// catalog detail prepared; consuming it is discovery, and it marks the table
// inspected for a write, so a routine does not spend its steps rediscovering
// what its watch knows.
func TestPrefetchedCatalogIsConsumedAsDiscovery(t *testing.T) {
	rt := &fakeRuntime{}
	a := &Agent{runtime: rt}
	prepared := a.prefetchCatalog(context.Background(), Request{PrefetchCatalogIDs: []string{"table:app:main.support_tickets"}}, nil)
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
	if got := (&Agent{runtime: empty}).prefetchCatalog(context.Background(), Request{PrefetchCatalogIDs: []string{"table:app:main.gone"}}, nil); got != nil {
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

// A run that starts from prefetched detail has no selection model when
// agent.selection is off. A catalog search it makes later comes back as the
// catalog returned it; it must not fail the run.
func TestPrefetchedRunSearchesTheCatalogWithSelectionOff(t *testing.T) {
	rt := &fakeRuntime{catalogOverride: func(args map[string]any) any {
		if len(detailIDsFromArgs(args)) != 0 {
			return fakeCatalogResult(args)
		}
		return map[string]any{"cards": []any{map[string]any{"id": "table:app:main.accounts", "kind": "table", "title": "accounts"}}}
	}}
	a := &Agent{runtime: rt}
	req := Request{Instruction: "Flag the ticket if its account is at churn risk.", PrefetchCatalogIDs: []string{"table:app:main.support_tickets"}}
	prepared := a.prefetchCatalog(context.Background(), req, nil)
	if prepared == nil {
		t.Fatal("nothing was prepared")
	}
	protocol := newProtocolRuntime(rt, req.Instruction, "", 8, nil, nil, CatalogSearchFeatures{})
	search, err := rt.QueryCatalog(context.Background(), map[string]any{"search": "accounts"})
	if err != nil || len(catalogCards(search)) != 1 {
		t.Fatalf("search = %v, %v", search, err)
	}
	got := a.enrichCatalog(context.Background(), req, protocol, prepared, search)
	if cards := catalogCards(got); len(cards) != 1 || stringValue(cards[0]["id"]) != "table:app:main.accounts" {
		t.Fatalf("a search with selection off came back as %v", got)
	}
	if _, enriched := mapValue(got)["prefetched_catalog"]; enriched {
		t.Fatalf("nothing can be selected without a selection model: %v", got)
	}
}

// A prefetched run has no selection model to choose its guides, so it keeps
// every guide its caller is permitted. Left with none, a routine's data agent
// never saw GraphJin's mutation syntax and made its own up.
func TestPrefetchedRunKeepsItsPermittedGuides(t *testing.T) {
	permitted := allowedSkills(false, profileWithRoleAndRoots("user"))
	if len(permitted) == 0 {
		t.Fatal("the test profile permits no guides")
	}
	prepared := (&Agent{runtime: &fakeRuntime{}}).prefetchCatalog(context.Background(),
		Request{PrefetchCatalogIDs: []string{"table:app:main.support_tickets"}}, permitted)
	if prepared == nil || !prepared.unselected || len(prepared.stats.LoadedSkillIDs) != len(permitted) {
		t.Fatalf("prepared = %+v, want it unselected with the %d permitted guides recorded", prepared, len(permitted))
	}

	// End to end, through Ax's own prompt assembly: the guide reaches the
	// model of a prefetched run, as it does without the prefetch.
	for name, ids := range map[string][]string{"prefetched": {"table:app:main.support_tickets"}, "plain": nil} {
		rec := &recordingClient{}
		runner := newAgent(
			Config{Provider: "openai", APIKeyEnv: "GRAPHJIN_UNUSED", TimeoutSeconds: 50, MaxSteps: 4},
			&fakeRuntime{},
			WithClientFactory(func(Config) (ax.AIClient, error) { return rec, nil }),
		)
		_, _ = runner.Run(context.Background(), Request{
			Instruction:        "Set support ticket 7 to resolved.",
			Capabilities:       profileWithRoleAndRoots("user"),
			PrefetchCatalogIDs: ids,
		})
		reached := false
		for i, call := range rec.calls {
			// Once per prompt: the executor's loaded guides, not a second
			// copy in the runtime instructions.
			switch n := strings.Count(dumpAXValue(call.values), "Skill: data_write"); {
			case n == 1:
				reached = true
			case n > 1:
				t.Errorf("%s run: model call %d carries the data_write guide %d times", name, i, n)
			}
		}
		if !reached {
			t.Errorf("%s run: the data_write guide reached no prompt (%d model calls)", name, len(rec.calls))
		}
	}
}
