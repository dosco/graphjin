package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	core "github.com/dosco/graphjin/core/v3"
)

// The notice names the exact selection that adds each name, for a foreign key
// and for a table's own rows, and says when it matters: before an answer that
// says which rows these are.
func TestNamingMessageNamesTheSelectionToAdd(t *testing.T) {
	msg := NamingMessage([]core.NamingGapInfo{
		{Path: "subscriptions", Table: "accounts", KeyColumn: "account_id", NameColumn: "name", Relationship: "accounts"},
		{Path: "accounts", Table: "accounts", KeyColumn: "id", NameColumn: "name"},
	})
	for _, want := range []string{
		"subscriptions identifies accounts only by account_id; select accounts { name } inside subscriptions",
		"accounts rows carry id without name; select name",
		"if the answer says which rows these are",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message missing %q:\n%s", want, msg)
		}
	}
}

func TestNamingIsAbsentWithoutGaps(t *testing.T) {
	if NamingFor(nil) != nil || NamingFor(&core.Result{}) != nil {
		t.Fatal("naming notice for a result without gaps")
	}
	if got := attachNaming(executeResult{}, &core.Result{}); got.Naming != nil {
		t.Fatalf("attachNaming added a notice: %+v", got)
	}
}

// unnamedRowsRuntime answers the way executeResultFromCore does for a query
// selecting subscriptions' account_id without the account's name: the rows,
// plus the naming notice core derived from the compiled query.
type unnamedRowsRuntime struct {
	fakeRuntime
	executed int
}

func (r *unnamedRowsRuntime) ExecuteGraphQL(_ context.Context, args map[string]any) (any, error) {
	r.executed++
	query, _ := args["query"].(string)
	if strings.Contains(query, "name") {
		return executeResult{Data: map[string]any{"subscriptions": []any{
			map[string]any{"renews_at": "2026-11-16", "accounts": map[string]any{"name": "Meridian Robotics"}},
		}}}, nil
	}
	gaps := []core.NamingGapInfo{{Path: "subscriptions", Table: "accounts", KeyColumn: "account_id", NameColumn: "name", Relationship: "accounts"}}
	return executeResult{
		Data:   map[string]any{"subscriptions": []any{map[string]any{"account_id": 1, "renews_at": "2026-11-16"}}},
		Naming: &NamingInfo{Rows: gaps, Message: NamingMessage(gaps)},
	}, nil
}

func unnamedRowsProtocol(t *testing.T) (*protocolRuntime, *unnamedRowsRuntime) {
	t.Helper()
	base := &unnamedRowsRuntime{}
	profile := &CapabilityProfile{RoleClass: "user", AllowedActions: []string{}}
	runtime := newProtocolRuntime(base, "Which account renews soonest?", "", 8, profile, nil, CatalogSearchFeatures{})
	runtime.state.seedOK = true
	runtime.state.modelDiscoveryAction = true
	runtime.state.tablesDetailed = map[string]bool{"subscriptions": true}
	runtime.state.catalogDetails = []string{"table:app:main.subscriptions"}
	return runtime, base
}

const unnamedQuery = `query { subscriptions(where: { renews_at: { gte: "2026-11-07" } }, order_by: { renews_at: asc }, limit: 1) { account_id renews_at } }`

// The first result that names rows only by key is thrown with the selection
// that names them, so code that finals right after the query never answers
// "account 1"; the query restated with the name returns the name.
func TestUnnamedRowsAreHeldBackWithTheSelectionThatNamesThem(t *testing.T) {
	runtime, _ := unnamedRowsProtocol(t)
	_, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": unnamedQuery})
	if err == nil {
		t.Fatal("rows named only by account_id were returned as an answerable result")
	}
	if !strings.Contains(err.Error(), "select accounts { name } inside subscriptions (it has only account_id)") {
		t.Fatalf("the refusal must name the selection to add: %v", err)
	}
	// Obeying the refusal verbatim must return the name.
	_, repaired, found := strings.Cut(err.Error(), "exactly as given: ")
	repaired, _, _ = strings.Cut(repaired, ". An id names")
	if !found || checkGraphQLParses(repaired) != nil {
		t.Fatalf("the refusal must carry a corrected query that parses: %v", err)
	}
	out, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": repaired})
	if err != nil || !strings.Contains(fmt.Sprint(out), "Meridian Robotics") {
		t.Fatalf("the restated query must return the name: %v %v", out, err)
	}
	resp := runtime.state.finalize(Response{Status: StatusAnswered, Answer: "Meridian Robotics renews soonest, on 2026-11-16."})
	if resp.Status != StatusAnswered {
		t.Fatalf("a held-back result must not block the answer, got %s: %+v", resp.Status, resp.Errors)
	}
}

// When the ids are what the step needs, running the same query again goes
// through: the check holds back one result per run, never a second.
func TestUnnamedRowsGoThroughTheSecondTime(t *testing.T) {
	runtime, base := unnamedRowsProtocol(t)
	if _, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": unnamedQuery}); err == nil {
		t.Fatal("expected the first unnamed result to be held back")
	}
	out, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": unnamedQuery})
	if err != nil {
		t.Fatalf("the repeated query must go through: %v", err)
	}
	if !strings.Contains(fmt.Sprint(out), "account_id") || base.executed != 2 {
		t.Fatalf("the repeat must execute and return the rows: executed=%d out=%v", base.executed, out)
	}
}

// Corrected queries are built from what the data agent actually wrote in the
// 2026-11-07 probe runs, and each must parse.
func TestNamingRepairAddsTheNameToTheRootSelection(t *testing.T) {
	accounts := []namingSelection{{path: "subscriptions", selection: "accounts { name }"}}
	cases := []struct {
		name, query string
		gaps        []namingSelection
		want        string
	}{
		{"a ranked pick", `query { subscriptions(where: { renews_at: { gte: "2026-11-07" } }, order_by: { renews_at: asc }, limit: 1) { account_id renews_at } }`, accounts,
			`query { subscriptions(where: { renews_at: { gte: "2026-11-07" } }, order_by: { renews_at: asc }, limit: 1) { account_id renews_at accounts { name } } }`},
		{"variables and first", `query ($today: String!) { subscriptions(where: { renews_at: { gte: $today } }, order_by: { renews_at: asc }, first: 1) { account_id renews_at } }`, accounts,
			`query ($today: String!) { subscriptions(where: { renews_at: { gte: $today } }, order_by: { renews_at: asc }, first: 1) { account_id renews_at accounts { name } } }`},
		{"an aliased root", `{ soonest: subscriptions(order_by: { renews_at: asc }, limit: 1) { account_id } }`, []namingSelection{{path: "soonest", selection: "accounts { name }"}},
			`{ soonest: subscriptions(order_by: { renews_at: asc }, limit: 1) { account_id accounts { name } } }`},
		{"the table's own name", "query {\n  accounts(order_by: { last_active_at: desc }, limit: 1) {\n    id\n    last_active_at\n  }\n}", []namingSelection{{path: "accounts", selection: "name"}},
			`query { accounts(order_by: { last_active_at: desc }, limit: 1) { id last_active_at name } }`},
		{"two roots", `query { a: support_tickets(limit: 1) { account_id } b: invoices(limit: 1) { account_id } }`,
			[]namingSelection{{path: "a", selection: "accounts { name }"}, {path: "b", selection: "accounts { name }"}},
			`query { a: support_tickets(limit: 1) { account_id accounts { name } } b: invoices(limit: 1) { account_id accounts { name } } }`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := addNamingSelections(tc.query, tc.gaps)
			if !ok || got != tc.want {
				t.Fatalf("addNamingSelections =\n%q (%v), want\n%q", got, ok, tc.want)
			}
			if err := checkGraphQLParses(got); err != nil {
				t.Fatalf("corrected query does not parse: %v", err)
			}
		})
	}
}

// A gap below the root, or at a root the query does not select, gets the
// description only: a corrected query never fixes half the rows.
func TestNamingRepairDeclinesWhatItCannotPlace(t *testing.T) {
	query := `query { accounts(limit: 1) { name subscriptions { account_id } } }`
	if got, ok := addNamingSelections(query, []namingSelection{{path: "accounts.subscriptions", selection: "accounts { name }"}}); ok {
		t.Fatalf("nested gap repaired: %q", got)
	}
	if got, ok := addNamingSelections(query, []namingSelection{{path: "invoices", selection: "accounts { name }"}}); ok {
		t.Fatalf("absent root repaired: %q", got)
	}
}
