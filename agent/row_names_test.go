package agent

import (
	"context"
	"strings"
	"testing"
)

func TestUnnamedRowsReadsOnlyIDCitations(t *testing.T) {
	names := []rowName{{entity: "account", id: "1", name: "Meridian Robotics"}}
	for _, tc := range []struct {
		answer string
		flag   bool
	}{
		{"The account that renews soonest is account ID **1**, on 2026-11-14.", true},
		{"Account 1 renews soonest on 2026-11-14.", true},
		{"Soonest renewal: account #1 (2026-11-14)", true},
		{"The soonest renewal belongs to ID 1.", true},
		{"Meridian Robotics (account ID 1) renews soonest.", false},
		{"1 account renews this month.", false},
		{"Account 10 renews soonest.", false},
		{"There is 1 open ticket.", false},
	} {
		if got := len(unnamedRows(tc.answer, names)) == 1; got != tc.flag {
			t.Errorf("%q: flagged=%v, want %v", tc.answer, got, tc.flag)
		}
	}
}

func TestRecordRowNamesKeepsOwnAndReferencedLabels(t *testing.T) {
	state := &discoveryState{instruction: "Which account renews soonest?", relations: map[string]map[string]string{
		"subscriptions": {"account_id": "accounts"},
	}}
	labels := map[string]string{"accounts": "name"}
	state.recordRowNames(`query { subscriptions(limit: 1) { account_id renews_at account { name } } }`,
		map[string]any{"data": map[string]any{"subscriptions": []any{
			map[string]any{"account_id": float64(1), "renews_at": "2026-11-14", "account": map[string]any{"name": "Meridian Robotics"}},
		}}}, labels)
	state.recordRowNames(`query { accounts(limit: 1) { id name } }`,
		map[string]any{"data": map[string]any{"accounts": []any{map[string]any{"id": float64(4), "name": "Quartzline Analytics"}}}}, labels)
	want := []rowName{{"account", "1", "Meridian Robotics"}, {"account", "4", "Quartzline Analytics"}}
	if len(state.rowNames) != len(want) || state.rowNames[0] != want[0] || state.rowNames[1] != want[1] {
		t.Fatalf("rowNames = %+v, want %+v", state.rowNames, want)
	}
}

func TestNameCitedRowsFallsBackToANote(t *testing.T) {
	protocol := &protocolRuntime{state: &discoveryState{rowNames: []rowName{{"account", "1", "Meridian Robotics"}}}}
	resp := (&Agent{}).nameCitedRows(context.Background(), nil, protocol, Config{}, Request{}, Response{Status: StatusAnswered, Answer: "Account 1 renews soonest."}, nil)
	if !strings.Contains(resp.Answer, "Account 1 is Meridian Robotics") {
		t.Fatalf("answer = %q, want the row named", resp.Answer)
	}
	again := (&Agent{}).nameCitedRows(context.Background(), nil, protocol, Config{}, Request{}, Response{Status: StatusAnswered, Answer: "Account 1 renews soonest."}, nil)
	if again.Answer != "Account 1 renews soonest." {
		t.Fatalf("the restatement must fire once per run: %q", again.Answer)
	}
}
