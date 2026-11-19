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

// TestDemoQuestionIsShownTheValuesItsColumnHolds pins, on a fully booted demo
// and with zero provider tokens, that a run starts knowing how a status is
// spelled.
//
// Asked about "churn-risk accounts", a model filters status on "churn-risk";
// the demo stores churn_risk, the read is empty, and six of six runs never
// recovered. The values are sampled into the catalog, but a filtered catalog
// read returns cards with their evidence blanked, so they reach a run only if
// something fetches the column cards by id. This checks that the run's first
// input carries them from the live catalog, and that the spelling it shows is
// the one the engine matches.
func TestDemoQuestionIsShownTheValuesItsColumnHolds(t *testing.T) {
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
	// The program can run in more than one stage; what it has done is kept on
	// globalThis so a second pass repeats nothing.
	client := &evalScriptClient{code: `
const S = globalThis.__seedValues = globalThis.__seedValues || {};
const digest = (inputs.catalog_facets && inputs.catalog_facets.seed_digest) || [];
const accounts = digest.find(function (t) { return t.name === "accounts"; }) || {};
const shown = (accounts.stored_values && accounts.stored_values.status) || [];
const spelled = shown.find(function (v) { return v.replace(/[^a-z]/g, "") === "churnrisk"; }) || "";
if (!S.card) S.card = await query_catalog({id:"table:app:main.accounts"});
if (spelled && !S.rows) S.rows = await execute_graphql({query:'query { accounts(where: { status: { eq: "' + spelled + '" } }, order_by: { mrr_cents: desc }, limit: 1) { id name mrr_cents } }'});
const rows = (S.rows && S.rows.data && S.rows.data.accounts) || [];
const top = rows[0] || {};
const report = {
  columns: accounts.columns || [],
  shown: shown,
  spelled: spelled,
  rows: rows.length,
  name: top.name || "",
  mrr_cents: top.mrr_cents || 0
};
await final({status:"answered", answer:"probe " + JSON.stringify(report), data:{probe:S.rows}, evidence:[S.card]});
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
		Instruction: "Among churn-risk accounts, which one has the most revenue at stake and how much monthly revenue is that?",
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
		Columns  []string `json:"columns"`
		Shown    []string `json:"shown"`
		Spelled  string   `json:"spelled"`
		Rows     int      `json:"rows"`
		Name     string   `json:"name"`
		MRRCents float64  `json:"mrr_cents"`
	}
	_, encoded, found := strings.Cut(response.Answer, "probe ")
	if !found {
		t.Fatalf("scripted probe did not report (status=%s answer=%q errors=%v)", response.Status, response.Answer, response.Errors)
	}
	if err := json.Unmarshal([]byte(encoded), &report); err != nil {
		t.Fatalf("probe report %q: %v", encoded, err)
	}
	if !strings.Contains(strings.Join(report.Columns, " "), "status:") {
		t.Fatalf("the run's starting list does not name accounts.status: %+v", report)
	}
	if strings.Join(report.Shown, ",") != "active,cancelled,churn_risk" {
		t.Fatalf("the starting list shows status values %v, want the three the demo stores", report.Shown)
	}
	// The spelling shown is the one the engine matches.
	if report.Spelled != "churn_risk" || report.Rows != 1 || report.Name != "Meridian Robotics" || report.MRRCents != 480000 {
		t.Fatalf("filtering on the shown value must return the live row: %+v", report)
	}
}
