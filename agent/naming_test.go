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
// plus the naming notice core derived from the compiled query. Asked with
// the account's name selected, it returns the name beside what was asked for.
type unnamedRowsRuntime struct {
	fakeRuntime
	queries []string
	// gapPath is where the notice says the unnamed rows are.
	gapPath string
	// failNamed makes the query that selects the name fail, as a selection
	// the engine rejects would.
	failNamed bool
}

func (r *unnamedRowsRuntime) ExecuteGraphQL(_ context.Context, args map[string]any) (any, error) {
	query, _ := args["query"].(string)
	r.queries = append(r.queries, query)
	if strings.Contains(query, "name") {
		if r.failNamed {
			return nil, fmt.Errorf("accounts is not a relationship of subscriptions here")
		}
		return executeResult{Data: map[string]any{"subscriptions": []any{
			map[string]any{"account_id": 1, "renews_at": "2026-11-16", "accounts": map[string]any{"name": "Meridian Robotics"}},
		}}}, nil
	}
	gaps := []core.NamingGapInfo{{Path: r.gapPath, Table: "accounts", KeyColumn: "account_id", NameColumn: "name", Relationship: "accounts"}}
	return executeResult{
		Data:   map[string]any{"subscriptions": []any{map[string]any{"account_id": 1, "renews_at": "2026-11-16"}}},
		Naming: &NamingInfo{Rows: gaps, Message: NamingMessage(gaps)},
	}, nil
}

func unnamedRowsProtocol(t *testing.T) (*protocolRuntime, *unnamedRowsRuntime) {
	t.Helper()
	base := &unnamedRowsRuntime{gapPath: "subscriptions"}
	profile := &CapabilityProfile{RoleClass: "user", AllowedActions: []string{}}
	runtime := newProtocolRuntime(base, "Which account renews soonest?", "", 8, profile, nil, CatalogSearchFeatures{})
	runtime.state.seedOK = true
	runtime.state.modelDiscoveryAction = true
	runtime.state.tablesDetailed = map[string]bool{"subscriptions": true}
	runtime.state.catalogDetails = []string{"table:app:main.subscriptions"}
	return runtime, base
}

const unnamedQuery = `query { subscriptions(where: { renews_at: { gte: "2026-11-07" } }, order_by: { renews_at: asc }, limit: 1) { account_id renews_at } }`

// A query that identifies its rows only by key is not refused: GraphJin runs
// it again with the selection that names them, and the code that asked reads
// its own fields with the name beside them. Code that finals right after the
// query never answers "account 1", and no step is spent on a refusal.
func TestUnnamedRowsComeBackNamed(t *testing.T) {
	runtime, base := unnamedRowsProtocol(t)
	out, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": unnamedQuery})
	if err != nil {
		t.Fatalf("a query whose rows can be named must not be refused: %v", err)
	}
	row := mapValue(anySlice(mapValue(mapValue(normalizeValue(out))["data"])["subscriptions"])[0])
	if stringFromMap(mapValue(row["accounts"]), "name") != "Meridian Robotics" || row["renews_at"] != "2026-11-16" || row["account_id"] == nil {
		t.Fatalf("the result must carry what was asked for and the name: %v", row)
	}
	want := `query { subscriptions(where: { renews_at: { gte: "2026-11-07" } }, order_by: { renews_at: asc }, limit: 1) { account_id renews_at accounts { name } } }`
	if len(base.queries) != 2 || base.queries[1] != want {
		t.Fatalf("queries run = %q, want the model's and then %q", base.queries, want)
	}
	if err := checkGraphQLParses(base.queries[1]); err != nil {
		t.Fatalf("the query GraphJin ran does not parse: %v", err)
	}
	// The trail shows both: what the model wrote, and what GraphJin ran for it.
	var sources []string
	for _, action := range runtime.state.actions {
		if action.Tool == "execute_graphql" {
			sources = append(sources, action.Source+":"+action.Status)
		}
	}
	if strings.Join(sources, " ") != "model:ok naming:ok" {
		t.Fatalf("execute actions = %v, want the model's query and GraphJin's naming run", sources)
	}
	// The same query again is answered with the named rows, and nothing runs.
	again, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": unnamedQuery})
	if err != nil || !strings.Contains(fmt.Sprint(again), "Meridian Robotics") || len(base.queries) != 2 {
		t.Fatalf("the repeat must return the named rows without running: %v %v, %d queries", again, err, len(base.queries))
	}
	resp := runtime.state.finalize(Response{Status: StatusAnswered, Answer: "Meridian Robotics renews soonest, on 2026-11-16."})
	if resp.Status != StatusAnswered {
		t.Fatalf("an answer from the named rows must stand, got %s: %+v", resp.Status, resp.Errors)
	}
}

// When the names cannot be added, the rows the query returned are what the
// code gets: a name is a courtesy to the reader, never a reason to lose the
// result.
func TestUnnamedRowsStayWhenTheNamesCannotBeAdded(t *testing.T) {
	// The selection that names the rows is rejected by the engine.
	runtime, base := unnamedRowsProtocol(t)
	base.failNamed = true
	out, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": unnamedQuery})
	if err != nil || !strings.Contains(fmt.Sprint(out), "account_id") || len(base.queries) != 2 {
		t.Fatalf("the model's result must come back as it was: %v %v, %d queries", out, err, len(base.queries))
	}
	for _, action := range runtime.state.actions {
		if action.Source == "naming" {
			t.Fatalf("a naming run that failed must leave no action behind: %+v", action)
		}
	}
	// The notice points at rows the query has no selection set for.
	runtime, base = unnamedRowsProtocol(t)
	base.gapPath = "subscriptions.accounts"
	out, err = runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": unnamedQuery})
	if err != nil || !strings.Contains(fmt.Sprint(out), "account_id") || len(base.queries) != 1 {
		t.Fatalf("with nowhere to add the name, the query runs once: %v %v, %d queries", out, err, len(base.queries))
	}
}

// Corrected queries are built from what the data agent actually wrote in the
// 2026-11-07 probe runs and the 2026-11-12 routine runs, and each must parse.
func TestNamingRepairAddsTheNameWhereTheRowsAre(t *testing.T) {
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
		{"a row one level down", `query { support_tickets(where: { id: { eq: 25 } }) { id account_id account { id status } } }`,
			[]namingSelection{{path: "support_tickets.account", selection: "name"}},
			`query { support_tickets(where: { id: { eq: 25 } }) { id account_id account { id status name } } }`},
		{"rows two levels down, behind arguments", `query { support_tickets(where: { id: { eq: 29 } }) { id accounts { id invoices(where: { status: { eq: "failed" } }, limit: 5) { id amount_cents } } } }`,
			[]namingSelection{{path: "support_tickets.accounts", selection: "name"}, {path: "support_tickets.accounts.invoices", selection: "subscriptions { plan }"}},
			`query { support_tickets(where: { id: { eq: 29 } }) { id accounts { id invoices(where: { status: { eq: "failed" } }, limit: 5) { id amount_cents subscriptions { plan } } name } } }`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, placed := addNamingSelections(tc.query, tc.gaps)
			if len(placed) != len(tc.gaps) || got != tc.want {
				t.Fatalf("addNamingSelections =\n%q (%d placed), want\n%q", got, len(placed), tc.want)
			}
			if err := checkGraphQLParses(got); err != nil {
				t.Fatalf("corrected query does not parse: %v", err)
			}
		})
	}
}

// A selection whose rows the query has no selection set for is left out, and
// the ones that can be placed still are.
func TestNamingRepairLeavesOutWhatItCannotPlace(t *testing.T) {
	query := `query { accounts(limit: 1) { name subscriptions { account_id } } }`
	absent := namingSelection{path: "invoices", selection: "accounts { name }"}
	if got, placed := addNamingSelections(query, []namingSelection{absent, {path: "accounts.plans", selection: "name"}}); len(placed) != 0 || got != query {
		t.Fatalf("selections with nowhere to go were placed: %q %v", got, placed)
	}
	got, placed := addNamingSelections(query, []namingSelection{absent, {path: "accounts.subscriptions", selection: "plans { name }"}})
	if len(placed) != 1 || got != `query { accounts(limit: 1) { name subscriptions { account_id plans { name } } } }` {
		t.Fatalf("the selection that has a place must be added: %q %v", got, placed)
	}
}

// A ticket's account selected with only its id is named inside that
// selection; the notice's second line, which would reach the same account
// through the ticket's account_id, is not a second join.
func TestNamingSelectionsNameASelectedRowWhereItIs(t *testing.T) {
	out := map[string]any{"naming": map[string]any{"rows": []any{
		map[string]any{"path": "support_tickets", "table": "accounts", "key_column": "account_id", "name_column": "name", "relationship": "accounts"},
		map[string]any{"path": "support_tickets.account", "table": "accounts", "key_column": "id", "name_column": "name"},
		map[string]any{"path": "support_tickets", "table": "users", "key_column": "user_id", "name_column": "full_name", "relationship": "users"},
	}}}
	got, placed := addNamingSelections(`query { support_tickets(where: { id: { eq: 25 } }) { id account_id user_id account { id status } } }`, namingSelections(out))
	want := `query { support_tickets(where: { id: { eq: 25 } }) { id account_id user_id account { id status name } users { full_name } } }`
	if len(placed) != 2 || got != want {
		t.Fatalf("named query =\n%q (%d placed), want\n%q", got, len(placed), want)
	}
	if err := checkGraphQLParses(got); err != nil {
		t.Fatalf("named query does not parse: %v", err)
	}
}
