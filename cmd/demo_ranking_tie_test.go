package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	gjeval "github.com/dosco/graphjin/agent/v3/eval"
)

// TestDemoRankingTieReturnsEveryTiedRow runs a top-1 ranking against the real
// engine on the built-in demo at zero provider cost. The worker raises the
// query's limit to see past the cut, so the rewritten GraphQL has to be
// something the engine actually executes — a fake runtime would accept any
// string. The demo seeds several users on account 1, so "the user with the
// lowest account id" is a tie and every tied user must come back, flagged.
func TestDemoRankingTieReturnsEveryTiedRow(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded service integration")
	}
	run := func(query string) map[string]any {
		t.Helper()
		program := `
const res = await execute_graphql({query: ` + "`" + query + "`" + `});
await final({status:"answered", answer:"done", data:{res:res}, evidence:[res]});
`
		client := &evalScriptClient{}
		client.setSequence(
			`const seed = await query_catalog({search:"users accounts"}); console.log(seed);`,
			`const detail = await query_catalog({id:"table:app:main.users"}); console.log(detail);`,
			program, program, program,
		)
		instance := demoFailedWriteInstance(t, client)
		response, _, _, err := gjeval.PostAgentForReplay(context.Background(), http.DefaultClient,
			instance.BaseURL(), instance.Headers(), "Which user has the lowest account id?", nil)
		if err != nil {
			t.Fatal(err)
		}
		payload, _ := json.Marshal(response)
		if response.Status != "answered" {
			t.Fatalf("status = %s: %s", response.Status, payload)
		}
		var out map[string]any
		_ = json.Unmarshal(payload, &out)
		return out
	}
	rowsOf := func(out map[string]any, root string) []any {
		res, _ := out["data"].(map[string]any)["res"].(map[string]any)
		data, _ := res["data"].(map[string]any)
		rows, _ := data[root].([]any)
		return rows
	}

	tied := run(`query { users(order_by: {account_id: asc}, limit: 1) { id account_id } }`)
	rows := rowsOf(tied, "users")
	if len(rows) < 2 {
		t.Fatalf("tied ranking returned %d rows, want every user on the lowest account: %v", len(rows), tied["data"])
	}
	for _, row := range rows {
		if row.(map[string]any)["account_id"] != rows[0].(map[string]any)["account_id"] {
			t.Fatalf("a non-tied row came back: %v", rows)
		}
	}
	raw, _ := json.Marshal(tied)
	if !strings.Contains(string(raw), "ranking_tie") {
		t.Fatalf("the tie must be flagged: %s", raw)
	}

	clear := run(`query { users(order_by: {id: asc}, limit: 1) { id account_id } }`)
	if rows := rowsOf(clear, "users"); len(rows) != 1 {
		t.Fatalf("a unique ordering returned %d rows, want exactly the one asked for", len(rows))
	}
	raw, _ = json.Marshal(clear)
	if strings.Contains(string(raw), "ranking_tie") {
		t.Fatalf("no tie, no notice: %s", raw)
	}
}
