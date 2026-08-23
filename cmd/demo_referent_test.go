package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	gjeval "github.com/dosco/graphjin/agent/v3/eval"
)

// TestDemoReferentRewriteScopesTheFollowUp drives the multi-turn shape that
// produced confidently wrong numbers in run c3-r2, against a booted demo at
// zero provider cost. History retains account 3; the scripted model asks the
// unscoped question — exactly what flash-lite did — and the old guard's escape
// hatch then let the unscoped query run, so "how many users belong to it?" was
// answered with the count of every user in the database.
//
// Now the certain binding executes: the query runs scoped to account 3 through
// users.account_id, the result carries the notice naming the rewrite, and the
// rows are the subject's alone. The script is a pure read, so the distiller
// stage re-running it is harmless.
func TestDemoReferentRewriteScopesTheFollowUp(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded service integration")
	}
	program := `
const res = await execute_graphql({query: "query { users { id name account_id } }"});
const rows = (res && res.data && res.data.users) || [];
await final({status:"answered", answer:"That account has " + rows.length + " users.", data:{res:res}, evidence:[res]});
`
	client := &evalScriptClient{}
	client.setSequence(
		`const seed = await query_catalog({search:"users belong to account"}); console.log(seed);`,
		`const detail = await query_catalog({id:"table:app:main.users"}); console.log(detail);`,
		program, program, program,
	)
	instance := demoFailedWriteInstance(t, client)

	response, _, _, err := gjeval.PostAgentForReplay(
		context.Background(), http.DefaultClient, instance.BaseURL(), instance.Headers(),
		"How many users belong to it?",
		[]gjeval.TurnSpec{
			{Role: "user", Content: "Use Harborlight Systems, account 3."},
			{Role: "assistant", Content: "The retained account id is 3."},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(response)
	if response.Status != "answered" {
		t.Fatalf("the scoped rewrite should finalize normally, got %s: %s", response.Status, payload)
	}
	// The executed query is the rewritten one, and the run says so.
	if !strings.Contains(string(payload), "history_referent_bound") {
		t.Fatalf("the rewrite must announce itself: %s", payload)
	}
	if !strings.Contains(string(payload), "account_id: {eq: 3}") {
		t.Fatalf("the executed query should be scoped to account 3: %s", payload)
	}
	// The number reported is the subject's, not the whole table's. The demo
	// seeds ten users across four accounts, so the global count reading back
	// would be the recorded failure reproduced.
	if strings.Contains(response.Answer, "10 users") {
		t.Fatalf("the whole-table count must not come back: %q", response.Answer)
	}
}
