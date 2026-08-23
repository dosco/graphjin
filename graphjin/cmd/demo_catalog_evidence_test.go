package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
	gjagent "github.com/dosco/graphjin/agent/v3"
	gjeval "github.com/dosco/graphjin/agent/v3/eval"
)

// TestDemoCatalogServesJoinAndValueEvidence pins, against a fully booted demo
// service and with zero provider tokens, the two catalog payloads a week of agent
// fixes turned out to depend on:
//
//   - The status column card carries its sampled values. A startup race once made
//     this vanish at random for a whole process lifetime, and nothing noticed for
//     five benchmark runs because no test read the live card.
//
//   - The account_health table detail names its remote-join route in edges_json.
//     The redirect that saves models from doomed top-level queries learns the join
//     from exactly this payload.
//
// Both are asserted through the same bootstrapping real benchmark runs use, so a
// regression here is a regression there.
func TestDemoCatalogServesJoinAndValueEvidence(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded service integration")
	}
	project := t.TempDir()
	if err := extractDefaultDemo(project); err != nil {
		t.Fatal(err)
	}
	originalPath, originalConf, originalDB, originalOpened := cpath, conf, db, dbOpened
	defer func() {
		cpath, conf, db, dbOpened = originalPath, originalConf, originalDB, originalOpened
	}()
	t.Setenv("GO_ENV", "dev")
	client := &evalScriptClient{code: `await final({status:"blocked",answer:"not configured"});`}
	environment := evalEnvironment{ClientFactory: func(gjagent.Config) (ax.AIClient, error) { return client, nil }}
	instance, err := environment.Start(context.Background(), gjeval.EnvSpec{Target: gjeval.TargetDemo, ConfigPath: project, Seed: 23})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close() //nolint:errcheck

	baseURL := strings.TrimRight(strings.TrimSpace(instance.BaseURL()), "/")
	for _, suffix := range []string{"/api/v1/agent/status", "/api/v1/agent", "/api/v1/graphql"} {
		baseURL = strings.TrimSuffix(baseURL, suffix)
	}
	catalogCard := func(id string, fields string) map[string]any {
		t.Helper()
		body, _ := json.Marshal(map[string]any{
			"query": fmt.Sprintf(`query { gj_catalog(where: {id: {eq: %q}}) { %s } }`, id, fields),
		})
		request, err := http.NewRequest(http.MethodPost, baseURL+"/api/v1/graphql", bytes.NewReader(body))
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
				Cards []map[string]any `json:"gj_catalog"`
			} `json:"data"`
			Errors any `json:"errors"`
		}
		if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
			t.Fatal(err)
		}
		if len(decoded.Data.Cards) == 0 {
			t.Fatalf("no card for %s (errors=%v)", id, decoded.Errors)
		}
		return decoded.Data.Cards[0]
	}

	status := catalogCard("column:app:main.support_tickets.status", "id evidence_json examples_json")
	evidence, _ := status["evidence_json"].(string)
	for _, want := range []string{"observed_values", "open", "pending", "resolved"} {
		if !strings.Contains(evidence, want) {
			t.Fatalf("status column evidence missing %q: %s", want, evidence)
		}
	}
	if examples, _ := status["examples_json"].(string); !strings.Contains(examples, "open") {
		t.Fatalf("status examples should use a sampled value: %s", examples)
	}

	health := catalogCard("table:app:main.account_health", "id summary edges_json examples_json")
	if summary, _ := health["summary"].(string); !strings.Contains(summary, "4 columns") {
		t.Fatalf("account_health should publish its four columns: %q", summary)
	}
	edges, _ := health["edges_json"].(string)
	// The arrow arrives as -> or as Go's escaped -\u003e depending on the encoder.
	if !regexp.MustCompile(`relationship:[^"]*account_health\.__account_health_[^"]*-(>|\\u003e)[^"]*accounts`).MatchString(edges) {
		t.Fatalf("account_health edges must name its remote-join route: %s", edges)
	}
	// The example on this card is the one models copy. It used to show a
	// top-level read of a table that has no rows of its own, and benchmark
	// episodes reproduced that shape until their step budgets ran out.
	examples, _ := health["examples_json"].(string)
	if strings.Contains(examples, "{ account_health(limit") {
		t.Fatalf("account_health example still teaches the closed route: %s", examples)
	}
	if !strings.Contains(examples, "accounts(where:") || !strings.Contains(examples, "account_health {") {
		t.Fatalf("account_health example must teach the nested route: %s", examples)
	}

	// The file source is the other half of every cross-source question, and its
	// card used to dead-end: "file source read-only", config capability lines
	// for examples, and query_catalog as the next step. Models searching for the
	// SLA document landed here, invented a policies table, and answered from
	// nothing — that half scored zero in every benchmark run on record.
	policies := catalogCard("source:sla_policies", "id summary examples_json evidence_json suggested_next_json")
	if summary, _ := policies["summary"].(string); !strings.Contains(summary, "queryable as the sla_policies table") {
		t.Fatalf("the file source card must say it can be read: %q", summary)
	}
	policyExamples, _ := policies["examples_json"].(string)
	if !strings.Contains(policyExamples, "sla_policies(prefix:") || !strings.Contains(policyExamples, "inline_data: true") {
		t.Fatalf("the file source card must teach both reads: %s", policyExamples)
	}
	if next, _ := policies["suggested_next_json"].(string); !strings.Contains(next, "execute_graphql") {
		t.Fatalf("a card that teaches a query must offer the tool that runs it: %s", next)
	}
}

// TestDemoRemoteJoinRepairReturnsLiveRows drives the join repair end to end
// against real GraphJin, for zero provider tokens, because the unit tests around
// it could not have caught what shipped.
//
// The repair spent a whole benchmark round emitting `where: name: {eq: "..."}` —
// the braces dropped, because the span helper it spliced returns an object's
// interior — and told seven episodes to execute it exactly as given. Every unit
// test passed: they asserted substrings, which the broken form also contains,
// and the one named "...RepairedQueryExecutes" executed against a fake that
// counted calls without reading them. Nothing in the package had ever handed the
// string to something that parses.
//
// So this test does. The scripted model asks the closed question and reports
// what came back; the assertion is on live rows, which only exist if a real
// parser accepted the rewrite.
func TestDemoRemoteJoinRepairReturnsLiveRows(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded service integration")
	}
	project := t.TempDir()
	if err := extractDefaultDemo(project); err != nil {
		t.Fatal(err)
	}
	originalPath, originalConf, originalDB, originalOpened := cpath, conf, db, dbOpened
	defer func() {
		cpath, conf, db, dbOpened = originalPath, originalConf, originalDB, originalOpened
	}()
	t.Setenv("GO_ENV", "dev")
	// Reading the table card is what registers the remote join, exactly as a live
	// run does. Then the closed route is asked for on purpose.
	client := &evalScriptClient{code: `
const card = await query_catalog({id:"table:app:main.account_health"});
const probe = await execute_graphql({query:'query { account_health(where: {name: {eq: "Meridian Robotics"}}) { health open_risk_count } }'});
const rows = (probe && probe.data && probe.data.accounts) || [];
const first = rows[0] || {};
const nested = first.account_health || {};
const report = {
  rows: rows.length,
  name: first.name || "",
  health: nested.health || "",
  errors: (probe && probe.errors) ? probe.errors.length : 0,
  recovery: (probe && probe.recovery) ? probe.recovery.kind : ""
};
await final({status:"answered", answer:"probe " + JSON.stringify(report), data:{probe:probe}, evidence:[card]});
`}
	environment := evalEnvironment{ClientFactory: func(gjagent.Config) (ax.AIClient, error) { return client, nil }}
	instance, err := environment.Start(context.Background(), gjeval.EnvSpec{Target: gjeval.TargetDemo, ConfigPath: project, Seed: 23})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close() //nolint:errcheck

	base := strings.TrimRight(strings.TrimSpace(instance.BaseURL()), "/")
	for _, suffix := range []string{"/api/v1/agent/status", "/api/v1/agent", "/api/v1/graphql"} {
		base = strings.TrimSuffix(base, suffix)
	}
	trace := true
	payload, _ := json.Marshal(gjagent.Request{
		Instruction: "How healthy is Meridian Robotics right now?",
		ReturnTrace: &trace,
	})
	request, err := http.NewRequest(http.MethodPost, base+"/api/v1/agent", bytes.NewReader(payload))
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
	var response gjagent.Response
	if err := json.NewDecoder(httpResponse.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}

	var report struct {
		Rows     int    `json:"rows"`
		Name     string `json:"name"`
		Health   string `json:"health"`
		Errors   int    `json:"errors"`
		Recovery string `json:"recovery"`
	}
	_, encoded, found := strings.Cut(response.Answer, "probe ")
	if !found {
		t.Fatalf("scripted probe did not report (status=%s answer=%q errors=%v)", response.Status, response.Answer, response.Errors)
	}
	if err := json.Unmarshal([]byte(encoded), &report); err != nil {
		t.Fatalf("probe report %q: %v", encoded, err)
	}
	if report.Errors != 0 {
		t.Fatalf("the rewritten query was rejected by GraphJin: %+v", report)
	}
	if report.Rows == 0 {
		t.Fatalf("the rewrite returned no rows, so the repair does not work: %+v", report)
	}
	if report.Name == "" || report.Health == "" {
		t.Fatalf("the rewrite must return the parent row with the join nested inside it: %+v", report)
	}
	// The model asked for account_health and received accounts. Unannounced, that
	// reshaping reads as an empty answer.
	if report.Recovery != "remote_join_route_rewritten" {
		t.Fatalf("an executed rewrite must tell the model what it is looking at: %+v", report)
	}
}

// TestDemoRemoteJoinChildRejectsUnknownFields pins the other half of the same
// benchmark failure. The episode that asked account_health for open_risks and
// health_color — neither of which exists — was handed has_data:true with an
// empty object and no error at all, and answered that the account had an
// undefined number of open risks.
//
// A top-level query with invented columns has always errored. The remote join
// child did not: the selection rode the pass-through and jsn.FilterAliased
// dropped every key the API response did not carry. Now that a resolver which
// knows its response shape declares a closed column surface, the compile fails
// and names the columns that do exist.
func TestDemoRemoteJoinChildRejectsUnknownFields(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded service integration")
	}
	project := t.TempDir()
	if err := extractDefaultDemo(project); err != nil {
		t.Fatal(err)
	}
	originalPath, originalConf, originalDB, originalOpened := cpath, conf, db, dbOpened
	defer func() {
		cpath, conf, db, dbOpened = originalPath, originalConf, originalDB, originalOpened
	}()
	t.Setenv("GO_ENV", "dev")
	client := &evalScriptClient{code: `await final({status:"blocked",answer:"not configured"});`}
	environment := evalEnvironment{ClientFactory: func(gjagent.Config) (ax.AIClient, error) { return client, nil }}
	instance, err := environment.Start(context.Background(), gjeval.EnvSpec{Target: gjeval.TargetDemo, ConfigPath: project, Seed: 23})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close() //nolint:errcheck

	base := strings.TrimRight(strings.TrimSpace(instance.BaseURL()), "/")
	for _, suffix := range []string{"/api/v1/agent/status", "/api/v1/agent", "/api/v1/graphql"} {
		base = strings.TrimSuffix(base, suffix)
	}
	run := func(query string) (map[string]any, string) {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"query": query})
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
		raw, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		var envelope map[string]any
		if err := json.Unmarshal(raw, &envelope); err != nil {
			t.Fatalf("decode %q: %v (%s)", query, err, raw)
		}
		return envelope, string(raw)
	}

	// The real columns still work, nested under the parent.
	envelope, raw := run(`query { accounts(where: {name: {eq: "Meridian Robotics"}}, limit: 1) { id name account_health { health open_risk_count } } }`)
	if envelope["errors"] != nil {
		t.Fatalf("the real join columns must still resolve: %s", raw)
	}
	if !strings.Contains(raw, "open_risk_count") {
		t.Fatalf("the nested join should carry its data: %s", raw)
	}

	// The invented ones from the recorded episode must not come back empty.
	_, raw = run(`query { accounts(where: {name: {eq: "Meridian Robotics"}}, limit: 1) { id name account_health { open_risks health_color } } }`)
	if !strings.Contains(raw, "errors") {
		t.Fatalf("invented join columns must error rather than resolve to nothing: %s", raw)
	}
	if !strings.Contains(raw, "open_risks") {
		t.Fatalf("the error should name the column that does not exist: %s", raw)
	}
	// Naming the alternatives is what lets a model correct itself in one turn.
	if !strings.Contains(raw, "open_risk_count") {
		t.Fatalf("the error should list the columns that do exist: %s", raw)
	}
}
