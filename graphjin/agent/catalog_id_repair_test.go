package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// The ids here are the ones models actually asked for in benchmark generation
// 2028.1, matched against the card ids that run published.
var publishedIDs = []string{
	"table:app:main.accounts",
	"table:app:main.support_tickets",
	"table:app:main.invoices",
	"table:app:main.usage_events",
	"table:app:main.users",
	"table:app:main.account_health",
	"column:app:main.support_tickets.severity",
	"saved_query:open_critical_ticket_count",
	"saved_query:ticket_sla_context",
	"workflow:account_health_report",
	"mutation_pattern:update_row",
}

func TestResolveCatalogIDQualifier(t *testing.T) {
	for _, tc := range []struct {
		missed, want string
		resolvable   bool
	}{
		// Measured: 29% of missed ids are a real card minus its qualifiers.
		{missed: "table:accounts", want: "table:app:main.accounts", resolvable: true},
		{missed: "table:usage_events", want: "table:app:main.usage_events", resolvable: true},
		{missed: "table:support_tickets", want: "table:app:main.support_tickets", resolvable: true},
		{missed: "TABLE:Accounts", want: "table:app:main.accounts", resolvable: true},
		// These name entities that do not exist and must never resolve to something
		// plausible-looking; table:tickets is not support_tickets.
		{missed: "table:tickets", resolvable: false},
		{missed: "table:risks", resolvable: false},
		{missed: "table:harborlight_systems", resolvable: false},
		{missed: "saved_query:ListTickets", resolvable: false},
		{missed: "mutation_pattern:update_support_ticket", resolvable: false},
		// A kind mismatch is not a qualifier problem.
		{missed: "column:accounts", resolvable: false},
		{missed: "malformed", resolvable: false},
	} {
		got, ok := resolveCatalogIDQualifier(tc.missed, publishedIDs)
		if ok != tc.resolvable {
			t.Errorf("%s: resolvable = %v, want %v (got %q)", tc.missed, ok, tc.resolvable, got)
			continue
		}
		if ok && got != tc.want {
			t.Errorf("%s: resolved to %q, want %q", tc.missed, got, tc.want)
		}
	}
}

// TestResolveCatalogIDQualifierRefusesAmbiguity is the safety property: the same
// table name in two schemas must be reported, never picked.
func TestResolveCatalogIDQualifierRefusesAmbiguity(t *testing.T) {
	ambiguous := []string{"table:app:main.accounts", "table:billing:public.accounts"}
	if got, ok := resolveCatalogIDQualifier("table:accounts", ambiguous); ok {
		t.Fatalf("two schemas publishing accounts must not resolve, picked %q", got)
	}
}

func TestSuggestCatalogIDsNamesClosestCards(t *testing.T) {
	// table:tickets cannot be resolved, but support_tickets is the card meant.
	got := suggestCatalogIDs("table:tickets", publishedIDs, catalogIDSuggestionLimit)
	if len(got) == 0 {
		t.Fatal("expected candidates for table:tickets")
	}
	if got[0] != "table:app:main.support_tickets" {
		t.Fatalf("leading candidate = %q, want the support_tickets table; all = %v", got[0], got)
	}
	if len(got) > catalogIDSuggestionLimit {
		t.Fatalf("candidates must stay bounded, got %d", len(got))
	}

	// A saved-query miss should prefer saved queries over same-named tables.
	saved := suggestCatalogIDs("saved_query:ticket_count", publishedIDs, catalogIDSuggestionLimit)
	if len(saved) == 0 || !strings.HasPrefix(saved[0], "saved_query:") {
		t.Fatalf("saved-query miss should lead with a saved query, got %v", saved)
	}

	// Nothing plausible must produce nothing rather than noise.
	if got := suggestCatalogIDs("table:zzzz", publishedIDs, catalogIDSuggestionLimit); len(got) != 0 {
		t.Fatalf("unrelated name must yield no candidates, got %v", got)
	}
}

func TestRepairCatalogDetailIDsRequiresEveryID(t *testing.T) {
	repairs, ok := repairCatalogDetailIDs([]string{"table:accounts", "table:users"}, publishedIDs)
	if !ok || repairs["table:accounts"] != "table:app:main.accounts" || repairs["table:users"] != "table:app:main.users" {
		t.Fatalf("a fully resolvable batch must resolve, got %v ok=%v", repairs, ok)
	}
	// A batch where one id is invented is reported, not half-served: serving only
	// part of it hands back evidence the caller did not ask for.
	if _, ok := repairCatalogDetailIDs([]string{"table:accounts", "table:tickets"}, publishedIDs); ok {
		t.Fatal("a batch containing an unresolvable id must not resolve")
	}
}

func TestRewriteCatalogIDArgs(t *testing.T) {
	repairs := map[string]string{"table:accounts": "table:app:main.accounts"}

	single := rewriteCatalogIDArgs(map[string]any{"id": "table:accounts", "limit": 5}, repairs)
	if single["id"] != "table:app:main.accounts" {
		t.Fatalf("id not rewritten: %v", single)
	}
	if single["limit"] != 5 {
		t.Fatalf("unrelated args must survive: %v", single)
	}

	batch := rewriteCatalogIDArgs(map[string]any{"ids": []any{"table:accounts", "table:app:main.users"}}, repairs)
	list, _ := batch["ids"].([]any)
	if len(list) != 2 || list[0] != "table:app:main.accounts" || list[1] != "table:app:main.users" {
		t.Fatalf("ids not rewritten correctly: %v", batch["ids"])
	}

	// The original args must not be mutated; the caller still reports what was asked.
	original := map[string]any{"id": "table:accounts"}
	rewriteCatalogIDArgs(original, repairs)
	if original["id"] != "table:accounts" {
		t.Fatal("rewrite must not mutate the caller's args")
	}
}

// qualifiedOnlyRuntime serves a card only when asked by its canonical id, which is
// how the real catalog behaves and what makes the retry observable.
type qualifiedOnlyRuntime struct {
	requested []string
}

func (r *qualifiedOnlyRuntime) QueryCatalog(_ context.Context, args map[string]any) (any, error) {
	id, _ := args["id"].(string)
	r.requested = append(r.requested, id)
	for _, published := range publishedIDs {
		if published == id {
			return map[string]any{"cards": []any{map[string]any{"id": id, "summary": "served"}}}, nil
		}
	}
	return map[string]any{"cards": []any{}}, nil
}

func (r *qualifiedOnlyRuntime) GraphQLHelp(context.Context, map[string]any) (any, error) {
	return map[string]any{"cards": []any{}}, nil
}

func (r *qualifiedOnlyRuntime) ValidateWhereClause(context.Context, map[string]any) (any, error) {
	return map[string]any{}, nil
}

func (r *qualifiedOnlyRuntime) ExecuteSavedQuery(context.Context, map[string]any) (any, error) {
	return map[string]any{}, nil
}

func (r *qualifiedOnlyRuntime) ExecuteGraphQL(context.Context, map[string]any) (any, error) {
	return map[string]any{}, nil
}

// TestQueryCatalogServesQualifiedIDAfterShortMiss is the end-to-end property: a
// short id returns the card in the same step instead of a directive, so the
// following execution has its discovery evidence and is never refused.
func TestQueryCatalogServesQualifiedIDAfterShortMiss(t *testing.T) {
	base := &qualifiedOnlyRuntime{}
	runtime := newProtocolRuntime(base, "Show me the accounts table.", "", 0, nil, nil, CatalogSearchFeatures{})
	// Seed the run's known id space the way a prior broad search would.
	for _, id := range publishedIDs {
		runtime.state.catalogIDs[id] = true
	}

	out, err := runtime.QueryCatalog(context.Background(), map[string]any{"id": "table:accounts"})
	if err != nil {
		t.Fatalf("query_catalog returned an error: %v", err)
	}
	result := mapValue(out)
	cards := catalogCards(result)
	if len(cards) != 1 {
		payload, _ := json.Marshal(result)
		t.Fatalf("expected the canonical card to be served, got %s", payload)
	}
	if id, _ := mapValue(cards[0])["id"].(string); id != "table:app:main.accounts" {
		t.Fatalf("served card id = %q, want the canonical id", id)
	}
	recovery := mapValue(result["recovery"])
	if kind, _ := recovery["kind"].(string); kind != "catalog_id_qualified" {
		t.Fatalf("recovery must state the id was qualified, got %v", recovery["kind"])
	}
	// The canonical id has to be visible so later calls stop using the short form.
	payload, _ := json.Marshal(recovery)
	if !strings.Contains(string(payload), "table:app:main.accounts") {
		t.Fatalf("recovery must name the canonical id: %s", payload)
	}
	if len(base.requested) != 2 || base.requested[0] != "table:accounts" || base.requested[1] != "table:app:main.accounts" {
		t.Fatalf("expected the miss then one qualified retry, got %v", base.requested)
	}
}

// TestQueryCatalogReportsInventedIDWithCandidates keeps the unresolvable case
// honest: no card is invented, and the caller is handed named candidates.
func TestQueryCatalogReportsInventedIDWithCandidates(t *testing.T) {
	base := &qualifiedOnlyRuntime{}
	runtime := newProtocolRuntime(base, "How many open tickets are there?", "", 0, nil, nil, CatalogSearchFeatures{})
	for _, id := range publishedIDs {
		runtime.state.catalogIDs[id] = true
	}

	out, err := runtime.QueryCatalog(context.Background(), map[string]any{"id": "table:tickets"})
	if err != nil {
		t.Fatalf("query_catalog returned an error: %v", err)
	}
	result := mapValue(out)
	if len(catalogCards(result)) != 0 {
		t.Fatal("an invented id must not be served a card")
	}
	recovery := mapValue(result["recovery"])
	if kind, _ := recovery["kind"].(string); kind != "empty_detail" {
		t.Fatalf("expected an empty_detail recovery, got %v", recovery["kind"])
	}
	payload, _ := json.Marshal(recovery["did_you_mean"])
	if !strings.Contains(string(payload), "table:app:main.support_tickets") {
		t.Fatalf("candidates must name the closest published card: %s", payload)
	}
	if len(base.requested) != 1 {
		t.Fatalf("an unresolvable id must not trigger a retry, got %v", base.requested)
	}
}

// TestRecoveryEvidenceCapturesWhatTheModelReceived closes a diagnostic gap that has
// repeatedly forced conclusions to be drawn from source rather than trajectories.
// Action summaries carry recovery codes but not the payload, so whether the
// candidates or the resolved subject actually reached the model was unanswerable.
func TestRecoveryEvidenceCapturesWhatTheModelReceived(t *testing.T) {
	base := &qualifiedOnlyRuntime{}
	runtime := newProtocolRuntime(base, "How many open tickets are there?", "", 0, nil, nil, CatalogSearchFeatures{})
	for _, id := range publishedIDs {
		runtime.state.catalogIDs[id] = true
	}

	if _, err := runtime.QueryCatalog(context.Background(), map[string]any{"id": "table:tickets"}); err != nil {
		t.Fatal(err)
	}
	last := runtime.state.actions[len(runtime.state.actions)-1]
	payload, _ := json.Marshal(last.Evidence)
	for _, want := range []string{"recovery", "did_you_mean", "table:app:main.support_tickets"} {
		if !strings.Contains(string(payload), want) {
			t.Fatalf("action evidence must record %q: %s", want, payload)
		}
	}
}

// TestRecoveryEvidenceStaysAbsentWithoutARecovery keeps clean actions clean.
func TestRecoveryEvidenceStaysAbsentWithoutARecovery(t *testing.T) {
	if got := recoveryEvidence(map[string]any{"cards": []any{}}); got != nil {
		t.Fatalf("a call with no recovery must record nothing, got %v", got)
	}
	// Oversized payloads are truncated rather than dropped or stored whole.
	long := strings.Repeat("x", catalogEvidenceFieldLimit*2)
	captured := recoveryEvidence(map[string]any{"recovery": map[string]any{"kind": "empty_detail", "instruction": long}})
	recovery := mapValue(captured["recovery"])
	text, _ := recovery["instruction"].(string)
	if len(text) > catalogEvidenceFieldLimit+8 || !strings.HasSuffix(text, "…") {
		t.Fatalf("oversized payload must be truncated, got %d bytes", len(text))
	}
}
