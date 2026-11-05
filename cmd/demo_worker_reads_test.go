package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	gjeval "github.com/dosco/graphjin/agent/v3/eval"
)

// TestDemoComputationRefusalCarriesARunnableAggregate checks the rewrite the
// database_computation_required refusal proposes against the real engine on
// the built-in demo, at zero provider cost.
//
// A worker asked for "combined" revenue read rows and summed them in
// JavaScript, because the refusal said to run an aggregate without saying
// which. The refusal now quotes one, built from the worker's own row query, so
// that query has to be something the engine actually executes and whose sum
// is the right one — a string comparison would pass a query that does not
// parse.
func TestDemoComputationRefusalCarriesARunnableAggregate(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded service integration")
	}
	// The scripted question and the first, are the lead's own wording and the
	// worker's own row reads from lead-eval runs that summed in JavaScript.
	for _, tc := range []struct {
		name, instruction, rows, want string
	}{
		{"churn risk by status", "What is the combined monthly recurring revenue of our churn-risk accounts?",
			`query { accounts(where: {status: {eq: \"churn_risk\"}}) { id name mrr_cents } }`, "520000"},
		{"top three by id", "What is the combined monthly recurring revenue (MRR) for the top three accounts: Solvex Dynamics (ID 2), Harborlight Systems (ID 3), and Meridian Robotics (ID 1)?",
			`query { accounts(where: { id: { in: [1, 2, 3] } }) { id name mrr_cents } }`, "1610000"},
		{"churn risk by name", "What is the combined monthly recurring revenue (MRR) for the accounts currently at churn risk (Meridian Robotics and Quartzline Analytics)?",
			`query { accounts(where: { name: { in: [\"Meridian Robotics\", \"Quartzline Analytics\"] } }) { id name mrr_cents status } }`, "520000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rowRead := `
const detail = await query_catalog({id:"table:app:main.accounts"});
const rows = await execute_graphql({query: "` + tc.rows + `"});
await final({status:"answered", answer:"done", data:{rows:rows}, evidence:[detail, rows]});
`
			client := &evalScriptClient{}
			client.setSequence(rowRead, rowRead, rowRead, rowRead, rowRead)
			instance := demoFailedWriteInstance(t, client)
			checkRefusalRewrite(t, instance, tc.instruction, tc.want)
		})
	}
}

// checkRefusalRewrite asks instruction, finds the aggregate the refusal quotes,
// and runs it against the same engine.
func checkRefusalRewrite(t *testing.T, instance gjeval.Instance, instruction, want string) {
	t.Helper()
	response, _, _, err := gjeval.PostAgentForReplay(context.Background(), http.DefaultClient,
		instance.BaseURL(), instance.Headers(), instruction, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(response)
	var decoded any
	_ = json.Unmarshal(raw, &decoded)

	// "combined" alone must trip the guard: it is how the lead phrases a total.
	if !strings.Contains(string(raw), "database_computation_required") {
		t.Fatalf("a combined total read from rows must be refused: %.600s", raw)
	}
	rewrite := ""
	walkStrings(decoded, func(text string) {
		if _, after, ok := strings.Cut(text, "run exactly: "); ok && rewrite == "" {
			rewrite = strings.TrimSpace(after)
		}
	})
	if rewrite == "" {
		t.Fatalf("the refusal must quote the aggregate to run: %.600s", raw)
	}
	if !strings.Contains(rewrite, "sum_mrr_cents") || strings.Contains(rewrite, "sum_name") || strings.Contains(rewrite, "sum_id") {
		t.Fatalf("rewrite = %q, want sum_mrr_cents only", rewrite)
	}

	// The proposed query runs as written and computes the right total.
	body, _ := json.Marshal(map[string]any{"query": rewrite})
	request, _ := http.NewRequest(http.MethodPost, instance.BaseURL()+"/api/v1/graphql", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	for key, value := range instance.Headers() {
		request.Header.Set(key, value)
	}
	answer, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer answer.Body.Close() //nolint:errcheck
	var result struct {
		Data struct {
			Accounts []struct {
				Sum json.Number `json:"sum_mrr_cents"`
			} `json:"accounts"`
		} `json:"data"`
		Errors []any `json:"errors"`
	}
	if err := json.NewDecoder(answer.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if len(result.Errors) != 0 || len(result.Data.Accounts) != 1 || result.Data.Accounts[0].Sum.String() != want {
		t.Fatalf("rewrite %q returned %+v, want the total %s", rewrite, result, want)
	}
}

func walkStrings(value any, visit func(string)) {
	switch v := value.(type) {
	case string:
		visit(v)
	case []any:
		for _, item := range v {
			walkStrings(item, visit)
		}
	case map[string]any:
		for _, item := range v {
			walkStrings(item, visit)
		}
	}
}

// TestDemoWhichQuestionReadsReturnTheRowsName runs a "which account" read that
// selected only the id against the real engine. The widened query has to
// execute, return the same row, and carry its name — the answer said
// "Account 1" because the row it had held nothing else.
func TestDemoWhichQuestionReadsReturnTheRowsName(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded service integration")
	}
	const idOnly = `
const detail = await query_catalog({id:"table:app:main.accounts"});
const rows = await execute_graphql({query: 'query { accounts(where: {status: {in: ["active", "churn_risk"]}}, order_by: {renewal_date: asc}, limit: 1) { id renewal_date } }'});
await final({status:"answered", answer:"done", data:{rows:rows}, evidence:[detail, rows]});
`
	client := &evalScriptClient{}
	client.setSequence(idOnly, idOnly, idOnly)
	instance := demoFailedWriteInstance(t, client)

	response, _, _, err := gjeval.PostAgentForReplay(context.Background(), http.DefaultClient,
		instance.BaseURL(), instance.Headers(), "Which account has the soonest upcoming renewal?", nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(response)
	if response.Status != "answered" {
		t.Fatalf("status = %s: %s", response.Status, raw)
	}
	rows, _ := response.Data.(map[string]any)["rows"].(map[string]any)["data"].(map[string]any)["accounts"].([]any)
	if len(rows) != 1 {
		t.Fatalf("the widened read must return the same single row: %s", raw)
	}
	row := rows[0].(map[string]any)
	if row["name"] != "Meridian Robotics" || row["id"] == nil || row["renewal_date"] == nil {
		t.Fatalf("row = %v, want Meridian Robotics with its id and renewal date", row)
	}
}

// TestDemoUpcomingReadFromSubscriptionsNamesTheAccount replays the read behind
// "Account 8, renewing 2026-10-06": the soonest renewal from subscriptions,
// with no date filter and no name. On the real engine the read must come back
// held to today, with the account named through account_id — so the relation
// field the widening adds has to be one GraphJin actually resolves.
func TestDemoUpcomingReadFromSubscriptionsNamesTheAccount(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded service integration")
	}
	const read = `
const detail = await query_catalog({id:"table:app:main.subscriptions"});
const rows = await execute_graphql({query: 'query { subscriptions(order_by: {renews_at: asc}, limit: 1) { account_id renews_at } }'});
const row = rows.data.subscriptions[0];
await final({status:"answered", answer:"Account ID " + row.account_id + " renews soonest, on " + row.renews_at + ".", data:{rows:rows}, evidence:[detail, rows]});
`
	client := &evalScriptClient{}
	client.setSequence(read, read, read)
	instance := demoFailedWriteInstance(t, client)

	response, _, _, err := gjeval.PostAgentForReplay(context.Background(), http.DefaultClient,
		instance.BaseURL(), instance.Headers(), "Which account has the soonest upcoming renewal?", nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(response)
	if response.Status != "answered" {
		t.Fatalf("status = %s: %.800s", response.Status, raw)
	}
	result, _ := response.Data.(map[string]any)["rows"].(map[string]any)
	rows, _ := result["data"].(map[string]any)["subscriptions"].([]any)
	if len(rows) != 1 {
		t.Fatalf("want the single soonest upcoming row: %.800s", raw)
	}
	row := rows[0].(map[string]any)
	today := time.Now().UTC().Format("2006-01-02")
	if renews, _ := row["renews_at"].(string); len(renews) < 10 || renews[:10] < today {
		t.Fatalf("renews_at = %v, want today (%s) or later", row["renews_at"], today)
	}
	account, _ := row["account"].(map[string]any)
	if account["name"] != "Meridian Robotics" {
		t.Fatalf("row = %v, want the account named through account_id", row)
	}
	t.Logf("answer: %s", response.Answer)
	// The scripted answer cites the account by id, as flash-lite did; the
	// finished answer must still name it.
	if !strings.Contains(response.Answer, "Meridian Robotics") {
		t.Fatalf("answer = %q, want the account named", response.Answer)
	}
	if guidance, _ := result["guidance"].(string); !strings.Contains(guidance, "upcoming") {
		t.Fatalf("the result must say why past rows were dropped: %.400s", raw)
	}
}
