package agent

import (
	"context"
	"strings"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"

	"github.com/dosco/graphjin/core/v3"
)

func TestRankingTieProbeRewritesOnlyTopLevelRankingCuts(t *testing.T) {
	for _, tc := range []struct {
		name, query, want string
		cuts              []rankingCut
	}{
		{
			name:  "top one by a grouped count",
			query: `query TopCustomer { production_orders(order_by: { count_id: desc }, limit: 1) { customer_id count_id } }`,
			want:  `query TopCustomer { production_orders(order_by: { count_id: desc }, limit: 6) { customer_id count_id } }`,
			cuts:  []rankingCut{{Key: "production_orders", Limit: 1, Order: []string{"count_id"}}},
		},
		{
			name:  "aliased root, nested limit untouched, two order fields",
			query: `{ top: customers(limit: 3, order_by: {sum_total: desc, id: asc}) { id orders(limit: 1) { id } } }`,
			want:  `{ top: customers(limit: 8, order_by: {sum_total: desc, id: asc}) { id orders(limit: 1) { id } } }`,
			cuts:  []rankingCut{{Key: "top", Limit: 3, Order: []string{"sum_total", "id"}}},
		},
		{
			name:  "strings and comments inside arguments",
			query: "{ tickets(where: {subject: {eq: \"a (limit: 2) }\"}}, # note )\n order_by: {created_at: desc}, limit: 2) { id created_at } }",
			want:  "{ tickets(where: {subject: {eq: \"a (limit: 2) }\"}}, # note )\n order_by: {created_at: desc}, limit: 7) { id created_at } }",
			cuts:  []rankingCut{{Key: "tickets", Limit: 2, Order: []string{"created_at"}}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, cuts, ok := rankingTieProbe(tc.query)
			if !ok || got != tc.want {
				t.Fatalf("probe = %v %q, want %q", ok, got, tc.want)
			}
			if len(cuts) != len(tc.cuts) || cuts[0].Key != tc.cuts[0].Key || cuts[0].Limit != tc.cuts[0].Limit ||
				strings.Join(cuts[0].Order, ",") != strings.Join(tc.cuts[0].Order, ",") {
				t.Fatalf("cuts = %+v, want %+v", cuts, tc.cuts)
			}
		})
	}

	// Anything the scanner does not fully understand runs as written.
	for name, query := range map[string]string{
		"mutation":          `mutation { orders(insert: {id: 1}, order_by: {id: asc}, limit: 1) { id } }`,
		"variable limit":    `query($n: Int) { orders(order_by: {id: desc}, limit: $n) { id } }`,
		"no order":          `{ orders(limit: 1) { id } }`,
		"page, not a cut":   `{ orders(order_by: {id: desc}, limit: 50) { id } }`,
		"nested order":      `{ orders(order_by: {customer: {name: asc}}, limit: 1) { id } }`,
		"list order":        `{ orders(order_by: [{id: asc}], limit: 1) { id } }`,
		"directive":         `{ orders(order_by: {id: desc}, limit: 1) @cached { id } }`,
		"fragment spread":   `{ ...Top } fragment Top on Query { orders(order_by: {id: desc}, limit: 1) { id } }`,
		"second definition": `{ orders(order_by: {id: desc}, limit: 1) { id } } fragment F on orders { id }`,
		"unterminated":      `{ orders(order_by: {id: desc}, limit: 1 { id } }`,
	} {
		if got, _, ok := rankingTieProbe(query); ok || got != query {
			t.Errorf("%s: rewrote %q to %q", name, query, got)
		}
	}
}

func rankingRows(values ...float64) []any {
	rows := make([]any, 0, len(values))
	for i, v := range values {
		rows = append(rows, map[string]any{"customer_id": float64(i + 1), "count_id": v})
	}
	return rows
}

func settled(out any, cuts []rankingCut) any {
	result, _ := settleRankingTies(out, cuts)
	return result
}

func TestSettleRankingTies(t *testing.T) {
	cut := []rankingCut{{Key: "production_orders", Limit: 1, Order: []string{"count_id"}}}
	truncated := &TruncationInfo{Roots: []core.TruncatedRootInfo{{Path: "production_orders", Rows: 6, Limit: 6}, {Path: "other", Rows: 2, Limit: 2}}}

	// Three customers share the top count: all three come back, with a notice.
	out := settled(executeResult{Data: map[string]any{"production_orders": rankingRows(1, 1, 1)}, Truncation: truncated}, cut).(executeResult)
	if rows := mapValue(out.Data)["production_orders"].([]any); len(rows) != 3 {
		t.Fatalf("tied rows = %v, want all 3", rows)
	}
	if !strings.Contains(out.Guidance, "3 rows share the deciding value") || mapValue(out.Recovery)["kind"] != "ranking_tie" {
		t.Fatalf("tie notice = %q / %v", out.Guidance, out.Recovery)
	}
	if out.Truncation == nil || len(out.Truncation.Roots) != 1 || out.Truncation.Roots[0].Path != "other" {
		t.Fatalf("truncation = %+v, want only the unprobed root", out.Truncation)
	}

	// A clear winner is exactly what was asked, with no notice.
	out = settled(executeResult{Data: map[string]any{"production_orders": rankingRows(9, 4, 4)}}, cut).(executeResult)
	if rows := mapValue(out.Data)["production_orders"].([]any); len(rows) != 1 || out.Guidance != "" || out.Recovery != nil {
		t.Fatalf("clear winner = %v / %q", rows, out.Guidance)
	}

	// A top-2 cut that splits a tie keeps the tied row past it.
	top2 := []rankingCut{{Key: "production_orders", Limit: 2, Order: []string{"count_id"}}}
	out = settled(executeResult{Data: map[string]any{"production_orders": rankingRows(9, 4, 4, 2)}}, top2).(executeResult)
	if rows := mapValue(out.Data)["production_orders"].([]any); len(rows) != 3 {
		t.Fatalf("tied cut = %v, want 3 rows", rows)
	}

	// Every probed row tied: say more may share it.
	out = settled(executeResult{Data: map[string]any{"production_orders": rankingRows(1, 1, 1, 1, 1, 1)}}, cut).(executeResult)
	if !strings.Contains(out.Guidance, "More rows may share it") {
		t.Fatalf("saturated tie notice = %q", out.Guidance)
	}

	// Without the ordering field in the rows nothing can be compared: the
	// result is trimmed to what was asked and claims nothing.
	rows := []any{map[string]any{"customer_id": 1.0}, map[string]any{"customer_id": 2.0}}
	out = settled(executeResult{Data: map[string]any{"production_orders": rows}}, cut).(executeResult)
	if got := mapValue(out.Data)["production_orders"].([]any); len(got) != 1 || out.Guidance != "" {
		t.Fatalf("uncomparable rows = %v / %q", got, out.Guidance)
	}

	// Foreign result shapes are settled too.
	mapped := settled(map[string]any{"data": map[string]any{"production_orders": rankingRows(2, 2)}}, cut).(map[string]any)
	if got := mapValue(mapped["data"])["production_orders"].([]any); len(got) != 2 || mapped["guidance"] == nil {
		t.Fatalf("foreign result = %v", mapped)
	}
}

func customerTie() rankingTie {
	row := func(id float64, name string) map[string]any {
		return map[string]any{"customer_id": id, "count_id": 1.0, "customer": map[string]any{"name": name}}
	}
	return rankingTie{Root: "production_orders", Rows: []map[string]any{
		row(3, "Dawn Patrol Coffee Club"), row(2, "Harbor Cafe Group"), row(1, "Northstar Grocers"),
	}}
}

func TestUnreportedRankingTies(t *testing.T) {
	ties := []rankingTie{customerTie()}
	for answer, want := range map[string]bool{
		"**Dawn Patrol Coffee Club** has placed the most orders, with 1 order.":                        true,
		"Dawn Patrol Coffee Club, Harbor Cafe Group and Northstar Grocers are tied with 1 order each.": false,
		"There are 4 open tickets.": false, // not about this ranking
		"Harbor Cafe Group and Northstar Grocers lead with one order; Dawn Patrol Coffee Club also has one order.": false,
	} {
		if got := len(unreportedRankingTies(answer, ties)) != 0; got != want {
			t.Errorf("%q: unreported = %v, want %v", answer, got, want)
		}
	}
	// A row whose id equals the tied value is still told apart by its field.
	ordersTie := []rankingTie{{Root: "production_orders", Rows: []map[string]any{
		{"customer_id": 3.0, "count_id": 1.0}, {"customer_id": 2.0, "count_id": 1.0}, {"customer_id": 9.0, "count_id": 1.0},
	}}}
	if len(rankingTieLabels(ordersTie[0])) != 3 || len(unreportedRankingTies("Customer 3 placed the most orders.", ordersTie)) == 0 {
		t.Fatal("a tie told apart by customer_id must be judged")
	}
	// Rows told apart only by ids are matched as whole numbers.
	idTie := []rankingTie{{Root: "users", Rows: []map[string]any{{"id": 4.0, "account_id": 1.0}, {"id": 14.0, "account_id": 1.0}}}}
	if len(unreportedRankingTies("User 14 is first.", idTie)) == 0 || len(unreportedRankingTies("Users 4 and 14 tie.", idTie)) != 0 {
		t.Fatal("numeric labels must match whole numbers")
	}
}

func TestReportRankingTiesRestatesOrNotes(t *testing.T) {
	run := func(rewrite string) Response {
		t.Helper()
		program := &fakeProgram{output: map[string]ax.Value{"answer": rewrite}}
		a := newAgent(Config{}, &fakeRuntime{}, WithFinalizerFactory(func(string, map[string]ax.Value) Program { return program }))
		protocol := newProtocolRuntime(&fakeRuntime{}, "Which customer ordered most?", "", 10, nil, nil, CatalogSearchFeatures{})
		protocol.state.rankingTies = []rankingTie{customerTie()}
		return a.reportRankingTies(context.Background(), fakeClient{}, protocol, Config{}, Request{Instruction: "Which customer ordered most?"},
			Response{Status: StatusAnswered, Answer: "Dawn Patrol Coffee Club placed the most orders."}, nil)
	}
	fixed := run("Dawn Patrol Coffee Club, Harbor Cafe Group and Northstar Grocers are tied with 1 order each.")
	if !strings.HasPrefix(fixed.Answer, "Dawn Patrol Coffee Club, Harbor Cafe Group and Northstar Grocers are tied") {
		t.Fatalf("restated answer = %q", fixed.Answer)
	}
	// A restatement that still crowns one row gives way to a plain note.
	noted := run("Dawn Patrol Coffee Club is the top customer.")
	if !strings.HasPrefix(noted.Answer, "Dawn Patrol Coffee Club placed the most orders.") ||
		!strings.Contains(noted.Answer, "Note: this is a tie — 3 rows share the top value") ||
		!strings.Contains(noted.Answer, "harbor cafe group") {
		t.Fatalf("noted answer = %q", noted.Answer)
	}
}
