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
	// database. With interceptions thrown, the false claim now dies inside its
	// own turn — the run ends unanswered without the finalize gate even
	// needing to fire.
	if response.Status == "answered" {
		t.Fatalf("a run whose only write failed cannot answer success: %q", response.Answer)
	}
	payload, _ := json.Marshal(response)
	if !strings.Contains(string(payload), "did NOT execute") {
		t.Fatalf("the run should carry the thrown teaching: %s", payload)
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
let repaired = "";
try {
  await execute_graphql({query:'mutation { payments(insert: { id: 900001, payment_reference: "DEEPORG-PAY-001", invoice_id: 1, amount_cents: 480000, paid_at: "2027-01-15T12:00:00Z" }) { id } }'});
} catch (e) {
  const text = String((e && e.message) || e);
  const m = text.match(/exactly as given: (.*?)(?: \u2014 |$)/);
  if (m) { repaired = m[1]; }
}
if (!repaired) { throw new Error("no corrected mutation was thrown"); }
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

// The other five stable action losses are ticket resolutions: 0 of 75 recorded
// episodes ever landed one. Three stacked faults — "closed" for a status whose
// vocabulary is open|pending|resolved, notes for resolution_note, and
// resolved_at left null. The run's repairs now answer all three in sequence:
// the value guard corrects the vocabulary and its note names resolved_at, the
// unknown-column repair corrects the column, and a model that acts on the
// three teachings lands the exact row the benchmark oracle checks.
func TestDemoTicketResolutionChainConverts(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded service integration")
	}
	// A model's first write-like call is intercepted once by the security and
	// runtime evidence supply; a real model reads the supplied cards and simply
	// retries. And ax runs the same scripted program in the distiller stage
	// before the executor stage, so progress is kept on the shared session's
	// globals — the exact handoff mechanism real runs use — rather than
	// re-deriving state and tripping the duplicate-execution guards.
	chain := `
async function run(q) { try { return {ok:true, res: await execute_graphql({query:q})}; } catch (e) { return {ok:false, text: String((e && e.message) || e)}; } }
const closed = 'mutation { support_tickets(where: { id: { eq: 1 } }, update: { status: "closed", notes: "Sorted out and resolved." }) { id status } }';
let corrected = "";
for (let i = 0; i < 3 && !corrected; i++) {
  const r = await run(closed);
  if (!r.ok) {
    const m = r.text.match(/exactly as given: (.*?)(?: \u2014 |$)/);
    if (m) {
      corrected = m[1];
      if (r.text.indexOf("resolved_at") < 0) { throw new Error("value repair did not teach resolved_at: " + r.text); }
    }
  }
}
if (!corrected) { throw new Error("no corrected mutation was thrown"); }
let stamped = "";
const second = await run(corrected);
if (second.ok) {
  const rep = second.res && second.res.recovery && second.res.recovery.details && second.res.recovery.details.repaired_query;
  if (rep) { stamped = rep.replace('resolution_note:', 'resolved_at: "2027-01-15T12:00:00Z", resolution_note:'); }
}
if (!stamped) { stamped = corrected.replace('notes:', 'resolved_at: "2027-01-15T12:00:00Z", resolution_note:'); }
const done = await execute_graphql({query: stamped});
await final({status:"answered", answer:"Ticket 1 is resolved with a resolution note recorded.", data:{done:done}, evidence:[done]});
`
	client := &evalScriptClient{}
	client.setSequence(
		`const seed = await query_catalog({search:"close support ticket resolution"}); console.log(seed);`,
		`const detail = await query_catalog({id:"table:app:main.support_tickets"}); console.log(detail);`,
		chain, chain, chain,
	)
	instance := demoFailedWriteInstance(t, client)

	response, _, _, err := gjeval.PostAgentForReplay(
		context.Background(), http.DefaultClient, instance.BaseURL(), instance.Headers(),
		"Ticket 1 has been sorted out. Close it off and record a note saying what resolved it, without touching any other ticket.", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != "answered" {
		payload, _ := json.Marshal(response)
		t.Fatalf("the taught chain should finalize normally, got %s: %s", response.Status, payload)
	}
	// The benchmark oracle's own condition, byte for byte.
	base := strings.TrimRight(strings.TrimSpace(instance.BaseURL()), "/")
	for _, suffix := range []string{"/api/v1/agent/status", "/api/v1/agent", "/api/v1/graphql"} {
		base = strings.TrimSuffix(base, suffix)
	}
	body, _ := json.Marshal(map[string]any{
		"query": `query { support_tickets(where: {and: [{id: {eq: 1}}, {status: {eq: "resolved"}}, {resolution_note: {is_null: false}}, {resolution_note: {neq: ""}}, {resolved_at: {is_null: false}}]}) { count_id } }`,
	})
	request, err := http.NewRequest(http.MethodPost, base+"/api/v1/graphql", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	for key, value := range instance.Headers() {
		request.Header.Set(key, value)
	}
	httpResponse, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer httpResponse.Body.Close() //nolint:errcheck
	var decoded struct {
		Data struct {
			Tickets []struct {
				Count int `json:"count_id"`
			} `json:"support_tickets"`
		} `json:"data"`
	}
	if err := json.NewDecoder(httpResponse.Body).Decode(&decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Data.Tickets) == 0 || decoded.Data.Tickets[0].Count != 1 {
		t.Fatalf("the resolved ticket should satisfy the oracle condition: %+v", decoded.Data)
	}
}
