package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
	gjagent "github.com/dosco/graphjin/agent/v3"
	gjeval "github.com/dosco/graphjin/agent/v3/eval"
)

// Ten of fifteen action tasks in run c3-r2 were lost to one episode shape: the
// model wrote payment_reference and paid_at into a table whose columns are
// reference and recorded_at, the engine rejected the write, the finalize bounce
// demanded a repair, the model resent the same mutation until the step budget
// died — and the exhausted-loop rescue then shipped its "Successfully recorded
// payment..." claim as a clean answered response. In production that is a user
// being told their payment is recorded when the database holds nothing.
//
// These two tests drive that exact trajectory against a booted demo at zero
// provider cost: the stubborn model is refused honestly, and a model that takes
// the corrected mutation from the failure lands the write and passes.

func demoFailedWriteInstance(t *testing.T, client *evalScriptClient) gjeval.Instance {
	t.Helper()
	project := t.TempDir()
	if err := extractDefaultDemo(project); err != nil {
		t.Fatal(err)
	}
	originalPath, originalConf, originalDB, originalOpened := cpath, conf, db, dbOpened
	t.Cleanup(func() {
		cpath, conf, db, dbOpened = originalPath, originalConf, originalDB, originalOpened
	})
	t.Setenv("GO_ENV", "dev")
	environment := evalEnvironment{ClientFactory: func(gjagent.Config) (ax.AIClient, error) { return client, nil }}
	instance, err := environment.Start(context.Background(), gjeval.EnvSpec{Target: gjeval.TargetDemo, ConfigPath: project, Seed: 23, Writable: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = instance.Close() })
	return instance
}

const demoFailedWritePrompt = "Record payment DEEPORG-PAY-001 with id 900001 for invoice 1, amount 480000 cents, at 2027-01-15T12:00:00Z. Do not change the invoice or any other record."

const demoBrokenPaymentStep = `
const detail = await query_catalog({id:"table:app:main.payments"});
const res = await execute_graphql({query:'mutation { payments(insert: { id: 900001, payment_reference: "DEEPORG-PAY-001", invoice_id: 1, amount_cents: 480000, paid_at: "2027-01-15T12:00:00Z" }) { id } }'});
console.log(res);
await final({status:"answered", answer:"Successfully recorded payment DEEPORG-PAY-001 with id 900001.", data:{res:res}, evidence:[detail]});
`

func demoCountPayment(t *testing.T, instance gjeval.Instance) int {
	t.Helper()
	base := strings.TrimRight(strings.TrimSpace(instance.BaseURL()), "/")
	for _, suffix := range []string{"/api/v1/agent/status", "/api/v1/agent", "/api/v1/graphql"} {
		base = strings.TrimSuffix(base, suffix)
	}
	body, _ := json.Marshal(map[string]any{
		"query": `query { payments(where: {id: {eq: 900001}, reference: {eq: "DEEPORG-PAY-001"}, recorded_at: {eq: "2027-01-15T12:00:00Z"}}) { count_id } }`,
	})
	request, err := http.NewRequest(http.MethodPost, base+"/api/v1/graphql", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	for key, value := range instance.Headers() {
		request.Header.Set(key, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close() //nolint:errcheck
	var decoded struct {
		Data struct {
			Payments []struct {
				Count int `json:"count_id"`
			} `json:"payments"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Data.Payments) == 0 {
		return 0
	}
	return decoded.Data.Payments[0].Count
}

func TestDemoFailedWriteIsNotReportedDone(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded service integration")
	}
	client := &evalScriptClient{}
	client.setSequence(
		`const seed = await query_catalog({search:"record payment"}); console.log(seed);`,
		demoBrokenPaymentStep, demoBrokenPaymentStep, demoBrokenPaymentStep, demoBrokenPaymentStep, demoBrokenPaymentStep,
	)
	instance := demoFailedWriteInstance(t, client)

	response, _, _, err := gjeval.PostAgentForReplay(
		context.Background(), http.DefaultClient, instance.BaseURL(), instance.Headers(), demoFailedWritePrompt, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := demoCountPayment(t, instance); got != 0 {
		t.Fatalf("the broken write must not land, count=%d", got)
	}
	// At 40edb30d this came out status=answered with zero violations: the
	// exhausted-loop rescue shipped the model's claim over an unchanged
	// database. The claim must not survive as an answer.
	if response.Status == "answered" {
		t.Fatalf("a run whose only write failed cannot answer success: %q", response.Answer)
	}
	payload, _ := json.Marshal(response)
	if !strings.Contains(string(payload), "mutation_execution_failed") {
		t.Fatalf("the refusal should name the failed write: %s", payload)
	}
	// The refusal is also the last teaching moment, so it carries the exact
	// corrected mutation the model declined to run.
	if !strings.Contains(string(payload), `reference: \"DEEPORG-PAY-001\"`) {
		t.Fatalf("the refusal should carry the corrected write: %s", payload)
	}
}

func TestDemoFailedWriteRepairConverts(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded service integration")
	}
	// The same weak model, one behavior stronger: on the retry turn it takes
	// recovery.details.repaired_query from the failed result and executes it
	// exactly as given — the action the recovery text asks for.
	takeRepair := `
const res = await execute_graphql({query:'mutation { payments(insert: { id: 900001, payment_reference: "DEEPORG-PAY-001", invoice_id: 1, amount_cents: 480000, paid_at: "2027-01-15T12:00:00Z" }) { id } }'});
const repaired = res && res.recovery && res.recovery.details && res.recovery.details.repaired_query;
if (!repaired) { throw new Error("no repaired_query in " + JSON.stringify(res)); }
const done = await execute_graphql({query: repaired});
await final({status:"answered", answer:"Recorded payment DEEPORG-PAY-001 with id 900001 for invoice 1.", data:{done:done}, evidence:[done]});
`
	client := &evalScriptClient{}
	client.setSequence(
		`const seed = await query_catalog({search:"record payment"}); console.log(seed);`,
		`const detail = await query_catalog({id:"table:app:main.payments"}); console.log(detail);`,
		takeRepair, takeRepair, takeRepair,
	)
	instance := demoFailedWriteInstance(t, client)

	response, _, _, err := gjeval.PostAgentForReplay(
		context.Background(), http.DefaultClient, instance.BaseURL(), instance.Headers(), demoFailedWritePrompt, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != "answered" {
		payload, _ := json.Marshal(response)
		t.Fatalf("taking the corrected write should finalize normally, got %s: %s", response.Status, payload)
	}
	// The oracle's own condition: the row exists with the reference and
	// timestamp under their real column names.
	if got := demoCountPayment(t, instance); got != 1 {
		t.Fatalf("the corrected write should land exactly once, count=%d", got)
	}
}
