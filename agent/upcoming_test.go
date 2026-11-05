package agent

import (
	"context"
	"strings"
	"testing"
)

func TestUpcomingRewriteActsOnlyOnAPastLeadingRow(t *testing.T) {
	today := "2026-11-05"
	rows := func(dates ...string) map[string]any {
		var out []any
		for _, d := range dates {
			out = append(out, map[string]any{"account_id": 8.0, "renews_at": d})
		}
		return map[string]any{"data": map[string]any{"subscriptions": out}}
	}
	for _, tc := range []struct {
		name, query string
		out         any
		want        string
	}{
		{"past first row, no where", `query { subscriptions(order_by: {renews_at: asc}, limit: 1) { account_id renews_at } }`, rows("2026-10-06"),
			`query { subscriptions(where: {renews_at: {gte: "2026-11-05"}}, order_by: {renews_at: asc}, limit: 1) { account_id renews_at } }`},
		{"past first row, existing where is kept", `{ subscriptions(where: {status: {eq: "active"}}, order_by: {renews_at: asc}, limit: 3) { renews_at } }`, rows("2026-10-06T09:00:00Z"),
			`{ subscriptions(where: {and: [{status: {eq: "active"}}, {renews_at: {gte: "2026-11-05"}}]}, order_by: {renews_at: asc}, limit: 3) { renews_at } }`},
		// Left alone: the first row is already upcoming, the order is
		// descending, the field is not a date, the read is a page, or nothing
		// came back.
		{"first row upcoming", `{ subscriptions(order_by: {renews_at: asc}, limit: 1) { renews_at } }`, rows("2026-11-14"), ""},
		{"today counts as upcoming", `{ subscriptions(order_by: {renews_at: asc}, limit: 1) { renews_at } }`, rows("2026-11-05T08:00:00Z"), ""},
		{"descending", `{ subscriptions(order_by: {renews_at: desc}, limit: 1) { renews_at } }`, rows("2026-10-06"), ""},
		{"not a date", `{ subscriptions(order_by: {seats: asc}, limit: 1) { seats } }`, rows("2026-10-06"), ""},
		{"page, not a top-n", `{ subscriptions(order_by: {renews_at: asc}, limit: 50) { renews_at } }`, rows("2026-10-06"), ""},
		{"no rows", `{ subscriptions(order_by: {renews_at: asc}, limit: 1) { renews_at } }`, rows(), ""},
	} {
		got, fields := upcomingRewrite(tc.query, tc.out, today)
		if got != tc.want {
			t.Errorf("%s:\n got %q\nwant %q", tc.name, got, tc.want)
		}
		if tc.want != "" {
			if len(fields) != 1 || fields[0] != "subscriptions.renews_at" {
				t.Errorf("%s: fields = %v", tc.name, fields)
			}
			if _, ok := scanRootFields(got); !ok {
				t.Errorf("%s: the rewrite must stay a query the scanner reads", tc.name)
			}
		}
	}
	if !asksUpcoming("Which account has the soonest upcoming renewal?") || asksUpcoming("Which account renewed most recently?") {
		t.Fatal("upcoming intent misread")
	}
	if !strings.Contains(upcomingNotice([]string{"subscriptions.renews_at"}, today), "2026-11-05") {
		t.Fatal("the notice must say which day it held the read to")
	}
}

func TestLabelWideningFollowsTheForeignKey(t *testing.T) {
	state := newDiscoveryState("Which account has the soonest upcoming renewal?")
	state.recordRelationship("relationship:app:main.subscriptions.account_id->app:main.accounts.id")
	state.recordRelationship("edge:relationship:app:main.invoices.account_id->app:main.accounts.id")
	labels := map[string]string{"accounts": "name"}
	which := "Which account has the soonest upcoming renewal?"
	for _, tc := range []struct{ name, query, want string }{
		{"subscriptions through account_id", `{ subscriptions(order_by: {renews_at: asc}, limit: 1) { account_id renews_at } }`,
			`{ subscriptions(order_by: {renews_at: asc}, limit: 1) { account { name } account_id renews_at } }`},
		{"edge-recorded relation", `{ invoices(limit: 1) { account_id amount_cents } }`,
			`{ invoices(limit: 1) { account { name } account_id amount_cents } }`},
		{"relation already selected", `{ subscriptions { account_id account { id } } }`, `{ subscriptions { account_id account { id } } }`},
		{"no known relation", `{ payments { invoice_id } }`, `{ payments { invoice_id } }`},
		{"aggregate left alone", `{ subscriptions(order_by: {sum_mrr_cents: desc}, limit: 1) { plan sum_mrr_cents } }`,
			`{ subscriptions(order_by: {sum_mrr_cents: desc}, limit: 1) { plan sum_mrr_cents } }`},
	} {
		got, _ := labelWidening(tc.query, which, labels, state.relations)
		if got != tc.want {
			t.Errorf("%s:\n got %q\nwant %q", tc.name, got, tc.want)
		}
	}
}

// widenRejectingRuntime fails any read that selects name, the way an engine
// rejects a column the caller's role cannot see.
type widenRejectingRuntime struct {
	fakeRuntime
	queries []string
}

func (r *widenRejectingRuntime) ExecuteGraphQL(_ context.Context, args map[string]any) (any, error) {
	query := stringFromMap(args, "query")
	r.queries = append(r.queries, query)
	if strings.Contains(query, " name") {
		return map[string]any{"errors": []any{map[string]any{"message": "column name not found on invoices"}}}, nil
	}
	return map[string]any{"data": map[string]any{"invoices": []any{map[string]any{"id": "INV-1", "status": "paid"}}}}, nil
}

func TestFailedWideningFallsBackToTheModelsQuery(t *testing.T) {
	base := &widenRejectingRuntime{}
	runtime := newProtocolRuntime(base, "Which invoice is paid?", "", 8, nil, nil, CatalogSearchFeatures{})
	runtime.state.seedOK = true
	runtime.state.modelDiscoveryAction = true
	runtime.state.catalogDetails = []string{"table:main:invoices"}
	runtime.state.tableColumns["invoices"] = map[string]bool{"id": true, "name": true, "status": true}

	query := "query { invoices(limit: 1) { id status } }"
	out, err := runtime.ExecuteGraphQL(context.Background(), map[string]any{"query": query})
	if err != nil || executionFailed(out) {
		t.Fatalf("the model's own read must still answer: %+v err=%v", out, err)
	}
	if len(base.queries) != 2 || !strings.Contains(base.queries[0], " name") || base.queries[1] != query {
		t.Fatalf("want the widened read, then the model's query: %q", base.queries)
	}
}
