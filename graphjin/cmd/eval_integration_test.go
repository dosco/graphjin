package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	ax "github.com/ax-llm/ax/packages/go"
	gjagent "github.com/dosco/graphjin/agent/v3"
	gjeval "github.com/dosco/graphjin/agent/v3/eval"
)

type evalScriptClient struct {
	mu       sync.RWMutex
	code     string
	sequence []string
	calls    int
}

func (c *evalScriptClient) setCode(code string) {
	c.mu.Lock()
	c.code = code
	c.sequence = nil
	c.calls = 0
	c.mu.Unlock()
}

func (c *evalScriptClient) setSequence(sequence ...string) {
	c.mu.Lock()
	c.sequence = append([]string(nil), sequence...)
	c.calls = 0
	c.mu.Unlock()
}

func (c *evalScriptClient) Chat(context.Context, map[string]ax.Value, map[string]ax.Value) (ax.Value, error) {
	c.mu.Lock()
	code := c.code
	if len(c.sequence) != 0 {
		index := c.calls
		if index >= len(c.sequence) {
			index = len(c.sequence) - 1
		}
		code = c.sequence[index]
		c.calls++
	}
	c.mu.Unlock()
	content, _ := json.Marshal(map[string]string{"javascriptCode": code})
	return map[string]ax.Value{
		"results": []ax.Value{map[string]ax.Value{"content": string(content)}},
		"model_usage": map[string]ax.Value{"tokens": map[string]ax.Value{
			"prompt": 10, "completion": 5,
		}},
	}, nil
}

func (*evalScriptClient) Embed(context.Context, map[string]ax.Value, map[string]ax.Value) (ax.Value, error) {
	return nil, nil
}

func (*evalScriptClient) Stream(context.Context, map[string]ax.Value, map[string]ax.Value) ([]ax.Value, error) {
	return nil, nil
}

func TestEvalEmbeddedSQLiteMockPipeline(t *testing.T) {
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
	factory := func(gjagent.Config) (ax.AIClient, error) { return client, nil }
	environment := evalEnvironment{ClientFactory: factory}
	instance, err := environment.Start(context.Background(), gjeval.EnvSpec{Target: gjeval.TargetDemo, ConfigPath: project, Seed: 23})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close() //nolint:errcheck

	verifier := &gjeval.Verifier{BaseURL: instance.BaseURL(), Headers: instance.Headers()}
	source := gjeval.HTTPCatalogSource{BaseURL: instance.BaseURL(), Headers: instance.Headers()}
	snapshot, err := source.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Profiles) != 1 || len(snapshot.Profiles[0].AvailableSystemRoots) == 0 {
		t.Fatalf("agent status did not expose the caller capability profile: %+v", snapshot.Profiles)
	}
	if snapshot.Status.MaxSteps != 8 {
		t.Fatalf("embedded eval agent max steps = %d, want 8", snapshot.Status.MaxSteps)
	}
	namedRead := gjeval.OracleSpec{
		Query:   `query EvalNamedReadMustNotPersist { subscriptions(limit: 1) { id } }`,
		Extract: "subscriptions.0.id",
	}
	if _, err := verifier.Resolve(context.Background(), namedRead); err != nil {
		t.Fatalf("execute named eval read: %v", err)
	}
	afterNamedRead, err := source.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if afterNamedRead.Fingerprint != snapshot.Fingerprint {
		t.Fatalf("named eval read changed catalog fingerprint: before=%s after=%s", snapshot.Fingerprint, afterNamedRead.Fingerprint)
	}
	for _, row := range afterNamedRead.Rows {
		if row.Kind == "saved_query" && row.Name == "EvalNamedReadMustNotPersist" {
			t.Fatalf("named eval read persisted as saved query: %+v", row)
		}
	}
	generator := gjeval.Generator{
		Source:   source,
		Verifier: verifier,
		Now:      func() time.Time { return time.Unix(100, 0) },
	}
	suite, err := generator.Generate(context.Background(), gjeval.GeneratorOptions{Seed: 23, Scale: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(suite.Tasks) != 100 {
		t.Fatalf("generated %d tasks, want 100", len(suite.Tasks))
	}
	suite.Tasks = suite.Tasks[:1]
	suite.Generator.Scale = 1
	task := &suite.Tasks[0]
	if task.Oracle == nil {
		t.Fatalf("generated integration task has no oracle: %+v", task)
	}
	oracle, err := verifier.Resolve(context.Background(), *task.Oracle)
	if err != nil {
		t.Fatal(err)
	}
	queryJSON, _ := json.Marshal(task.Oracle.Query)
	idJSON, _ := json.Marshal(task.Provenance.SourceID)
	answerJSON, _ := json.Marshal(fmt.Sprintf("The answer is %s.", oracle.Value))
	client.setCode(fmt.Sprintf(`const detail = await query_catalog({id:%s}); const execution = await execute_graphql({query:%s}); await final({status:"answered",answer:%s,data:execution.data,evidence:[detail]});`, idJSON, queryJSON, answerJSON))
	// Skill-use reporting is an Ax optimizer signal and is covered separately;
	// this integration locks down public HTTP execution and scoring.
	task.Behavior.ExpectedUsedSkills = nil
	if err := task.Normalize(); err != nil {
		t.Fatal(err)
	}
	if err := suite.Normalize(); err != nil {
		t.Fatal(err)
	}
	store := gjeval.NewStore(t.TempDir())
	report, err := (gjeval.Runner{}).Run(context.Background(), *suite, instance, gjeval.RunOptions{Repeats: 3, Seed: 23, Store: store, AutoBaseline: true})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Acceptance.HardPass {
		failures, _ := json.Marshal(report.Tasks)
		t.Fatalf("mock pipeline failed: %s notices=%s", failures, strings.Join(report.Acceptance.Notices, "; "))
	}
	baseline, err := store.LoadBaseline()
	if err != nil || baseline == nil {
		t.Fatalf("load promoted baseline: baseline=%+v err=%v", baseline, err)
	}
	if err := evalReportExit(report); err != nil {
		t.Fatalf("passing pipeline did not map to exit 0: %v", err)
	}

	client.setCode(`await final({status:"blocked",answer:"incorrect refusal"});`)
	regression, err := (gjeval.Runner{}).Run(context.Background(), *suite, instance, gjeval.RunOptions{Repeats: 3, Seed: 23, Store: store, Baseline: baseline})
	if err != nil {
		t.Fatal(err)
	}
	if exitErr, ok := evalReportExit(regression).(*evalExitError); !ok || exitErr.Code != 1 {
		t.Fatalf("confirmed regression exit=%v, want code 1", evalReportExit(regression))
	}

	invalidSuite := *suite
	invalidTask := invalidSuite.Tasks[0]
	invalidTask.Oracle = &gjeval.OracleSpec{Query: "query { definitely_missing { count_id } }", Extract: "definitely_missing.0.count_id"}
	if err := invalidTask.Normalize(); err != nil {
		t.Fatal(err)
	}
	invalidSuite.Tasks = []gjeval.Task{invalidTask}
	if err := invalidSuite.Normalize(); err != nil {
		t.Fatal(err)
	}
	invalid, err := (gjeval.Runner{}).Run(context.Background(), invalidSuite, instance, gjeval.RunOptions{Repeats: 3, Seed: 23, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	if exitErr, ok := evalReportExit(invalid).(*evalExitError); !ok || exitErr.Code != 2 {
		t.Fatalf("invalid suite exit=%v, want code 2", evalReportExit(invalid))
	}
}

func TestEvalEmbeddedReactiveDeliveryMockPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded reactive service integration")
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
	client := &evalScriptClient{code: `
const security = await query_catalog({id:"help:security"});
const runtime = await query_catalog({id:"help:runtime"});
const help = await query_catalog({id:"help:watches"});
const inbox = await execute_graphql({query:"query { gj_watch_event(where: {seen: {eq: false}}, order_by: {created_at: desc}, limit: 1) { id watch_id data_json seen } }"});
const event = inbox.data.gj_watch_event[0];
const marked = await execute_graphql({query:"mutation MarkSeen($id: String!) { gj_watch_event(where: {id: {eq: $id}}, update: {seen: true}) { id seen } }", variables:{id:event.id}});
await final({status:"answered", answer:"Reviewed the delivered watch event and marked it seen.", data:{event:event, marked:marked.data}, evidence:[security,runtime,help]});
`}
	factory := func(gjagent.Config) (ax.AIClient, error) { return client, nil }
	instance, err := (evalEnvironment{ClientFactory: factory}).Start(context.Background(), gjeval.EnvSpec{
		Target: gjeval.TargetDemo, ConfigPath: project, Seed: 23,
		Writable: true, Reactive: true, Resettable: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close() //nolint:errcheck

	verifier := &gjeval.Verifier{BaseURL: instance.BaseURL(), Headers: instance.Headers()}
	source := gjeval.HTTPCatalogSource{BaseURL: instance.BaseURL(), Headers: instance.Headers()}
	snapshot, err := source.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Status.ReadOnly {
		t.Fatal("embedded writable eval reports read_only=true from /api/v1/agent/status")
	}

	write := `mutation { payments(insert: {id: 990123, invoice_id: 1, amount_cents: 123, reference: "ROLE-CHECK", recorded_at: "2027-01-15T12:00:00Z"}) { id } }`
	userBody, err := evalIntegrationGraphQL(instance.BaseURL(), instance.Headers(), write)
	if err != nil || bytes.Contains(userBody, []byte(`"errors"`)) {
		t.Fatalf("writable user role could not perform paired action: err=%v body=%s", err, userBody)
	}
	resettable, ok := instance.(gjeval.ResettableInstance)
	if !ok {
		t.Fatal("reactive instance is not resettable")
	}
	if err := resettable.Reset(context.Background()); err != nil {
		t.Fatal(err)
	}
	observerHeaders := make(map[string]string, len(instance.Headers())+1)
	for key, value := range instance.Headers() {
		observerHeaders[key] = value
	}
	observerHeaders["X-User-Role"] = "anon"
	delete(observerHeaders, "X-User-ID")
	delete(observerHeaders, "X-Account-ID")
	observerBody, err := evalIntegrationGraphQL(instance.BaseURL(), observerHeaders, write)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(observerBody, []byte(`"errors"`)) {
		t.Fatalf("anonymous role unexpectedly performed the paired payment action: %s", observerBody)
	}

	suite, err := (gjeval.Generator{Source: source, Verifier: verifier}).Generate(context.Background(), gjeval.GeneratorOptions{Seed: 23, Scale: 100})
	if err != nil {
		t.Fatal(err)
	}
	var delivery, definition, crossSource, refusal *gjeval.Task
	var reactiveSlugs []string
	for index := range suite.Tasks {
		if suite.Tasks[index].Category == gjeval.CategoryReactive {
			reactiveSlugs = append(reactiveSlugs, suite.Tasks[index].Slug)
		}
		if suite.Tasks[index].Slug == "reactive-delivery-payments" {
			delivery = &suite.Tasks[index]
		}
		if suite.Tasks[index].Category == gjeval.CategoryReactive && suite.Tasks[index].Mutation != nil && strings.Contains(suite.Tasks[index].Mutation.ExpectedValue, "deeporg_failed_invoices") {
			definition = &suite.Tasks[index]
		}
		if suite.Tasks[index].Slug == "cross-source-account-health-1" {
			crossSource = &suite.Tasks[index]
		}
		if refusal == nil && suite.Tasks[index].Category == gjeval.CategoryRefusal {
			refusal = &suite.Tasks[index]
		}
	}
	if delivery == nil || definition == nil || crossSource == nil || refusal == nil {
		t.Fatalf("generated suite is missing reactive/cross-source/refusal tasks: delivery=%v definition=%v cross_source=%v refusal=%v slugs=%v", delivery != nil, definition != nil, crossSource != nil, refusal != nil, reactiveSlugs)
	}
	if delivery.Mutation == nil || len(delivery.Mutation.Setup) != 2 {
		t.Fatalf("payment delivery setup = %+v, want watch plus post-subscription payment", delivery.Mutation)
	}
	if crossSource.Oracle == nil {
		t.Fatal("cross-source task omitted its runtime oracle")
	}
	crossSourceResult, err := verifier.Resolve(context.Background(), *crossSource.Oracle)
	if err != nil || crossSourceResult.Value == "" || crossSourceResult.Dimension == "" {
		t.Fatalf("embedded eval could not join the app database to account_health_api: result=%+v err=%v", crossSourceResult, err)
	}

	task := *delivery
	suite.Tasks = []gjeval.Task{task}
	suite.Generator.Scale = 1
	if err := suite.Normalize(); err != nil {
		t.Fatal(err)
	}
	client.setSequence(
		`await final('Review the payments watch event through the governed watch path.', {watch:'payments'});`,
		`await used('watch_read'); const security = await query_catalog({id:"help:security"}); const runtime = await query_catalog({id:"help:runtime"}); const help = await query_catalog({id:"help:watches"}); const wrong = await execute_graphql({query:"query { watch_events_unseen { id watch_id seen } }"}); await final('Continue from the watch-root repair.', {security,runtime,help,wrong});`,
		`await used('watch_read'); const event = graphjinSystemRootRepair.data.gj_watch_event[0]; const marked = await execute_graphql({query:"mutation MarkSeen($id: String!) { gj_watch_event(where: {id: {eq: $id}}, update: {seen:true}) { id seen } }", variables:{id:event.id}}); await final({status:"answered", answer:"Reviewed the delivered payment watch event and marked it seen.", data:{event,marked:marked.data}});`,
	)
	deliveryStore := gjeval.NewStore(t.TempDir())
	report, err := (gjeval.Runner{}).Run(context.Background(), *suite, instance, gjeval.RunOptions{Repeats: 1, Seed: 23, Store: deliveryStore})
	if err != nil {
		t.Fatal(err)
	}
	if report.RunStatus != gjeval.RunStatusComplete || len(report.Tasks) != 1 || !report.Tasks[0].Pass {
		failures, _ := json.Marshal(report.Tasks)
		episodes, _ := deliveryStore.LoadEpisodes(report.RunID)
		var responseSummary []byte
		if len(episodes) != 0 {
			responseJSON, _ := json.Marshal(episodes[0].Response)
			var response struct {
				Status string `json:"status"`
				Answer string `json:"answer"`
				Trace  struct {
					ChatLog []struct {
						Name  string `json:"name"`
						Item1 struct {
							Content string `json:"content"`
						} `json:"item1"`
					} `json:"chat_log"`
				} `json:"trace"`
			}
			_ = json.Unmarshal(responseJSON, &response)
			responseSummary, _ = json.Marshal(response)
		}
		t.Fatalf("reactive mock pipeline failed: status=%s tasks=%s response=%s notices=%s", report.RunStatus, failures, responseSummary, strings.Join(report.Acceptance.Notices, "; "))
	}
	deliveryEpisodes, err := deliveryStore.LoadEpisodes(report.RunID)
	if err != nil || len(deliveryEpisodes) != 1 || deliveryEpisodes[0].Mutation == nil || !deliveryEpisodes[0].Mutation.PostStatePass || !deliveryEpisodes[0].Mutation.CollateralPass {
		t.Fatalf("watch delivery did not preserve event mutation contract: count=%d mutation=%+v err=%v", len(deliveryEpisodes), func() any {
			if len(deliveryEpisodes) == 0 {
				return nil
			}
			return deliveryEpisodes[0].Mutation
		}(), err)
	}
	deliveryResponseJSON, _ := json.Marshal(deliveryEpisodes[0].Response)
	var deliveryResponse struct {
		Actions []map[string]any `json:"actions"`
	}
	_ = json.Unmarshal(deliveryResponseJSON, &deliveryResponse)
	var sawInventedRoot, sawCanonicalRoot, sawSystemRootRepair bool
	for _, action := range deliveryResponse.Actions {
		if action["tool"] != "execute_graphql" {
			continue
		}
		args, _ := action["args"].(map[string]any)
		query, _ := args["query"].(string)
		sawInventedRoot = sawInventedRoot || strings.Contains(query, "watch_events_unseen")
		sawCanonicalRoot = sawCanonicalRoot || strings.Contains(query, "gj_watch_event")
		summary, _ := action["summary"].(map[string]any)
		codes, _ := summary["recovery_codes"].([]any)
		for _, rawCode := range codes {
			code, _ := rawCode.(string)
			sawSystemRootRepair = sawSystemRootRepair || code == "system_root_did_you_mean"
		}
	}
	if !sawInventedRoot || !sawCanonicalRoot || !sawSystemRootRepair {
		t.Fatalf("delivery repair trail: invented=%t canonical=%t repair=%t actions=%s", sawInventedRoot, sawCanonicalRoot, sawSystemRootRepair, deliveryResponseJSON)
	}

	definitionMutation := `mutation { gj_watch(insert: {name: "deeporg_failed_invoices", query: "subscription deeporg_failed_invoices($status: String!) { invoices(where: {status: {eq: $status}}, first: 25, after: $cursor) { id status attempts } invoices_cursor }", variables_json: {status: "failed"}, delivery_json: {kind: "inbox", digest: {window: "1h"}}}) { id name query delivery_json } }`
	definitionBody, err := evalIntegrationGraphQL(instance.BaseURL(), instance.Headers(), definitionMutation)
	if err != nil || bytes.Contains(definitionBody, []byte(`"errors"`)) {
		t.Fatalf("reactive definition mutation is not executable: err=%v body=%s", err, definitionBody)
	}
	if err := resettable.Reset(context.Background()); err != nil {
		t.Fatal(err)
	}
	client.setCode(fmt.Sprintf(`
const security = await query_catalog({id:"help:security"});
const runtime = await query_catalog({id:"help:runtime"});
const help = await query_catalog({id:"help:watches"});
const created = await execute_graphql({query:%q});
await final({status:"answered", answer:"Created the hourly failed-invoice watch.", data:created.data, evidence:[security,runtime,help]});
`, definitionMutation))
	definitionSuite := *suite
	definitionSuite.Tasks = []gjeval.Task{*definition}
	definitionSuite.Generator.Scale = 1
	if err := definitionSuite.Normalize(); err != nil {
		t.Fatal(err)
	}
	definitionStore := gjeval.NewStore(t.TempDir())
	definitionReport, err := (gjeval.Runner{}).Run(context.Background(), definitionSuite, instance, gjeval.RunOptions{Repeats: 1, Seed: 23, Store: definitionStore})
	if err != nil {
		t.Fatal(err)
	}
	if definitionReport.RunStatus != gjeval.RunStatusComplete || len(definitionReport.Tasks) != 1 || !definitionReport.Tasks[0].Pass {
		failures, _ := json.Marshal(definitionReport.Tasks)
		episodes, _ := definitionStore.LoadEpisodes(definitionReport.RunID)
		var evidence []byte
		if len(episodes) != 0 {
			responseJSON, _ := json.Marshal(episodes[0].Response)
			var response struct {
				Actions []map[string]any `json:"actions"`
			}
			_ = json.Unmarshal(responseJSON, &response)
			evidence, _ = json.Marshal(struct {
				Actions  []map[string]any         `json:"actions"`
				Mutation *gjeval.MutationEvidence `json:"mutation"`
			}{Actions: response.Actions, Mutation: episodes[0].Mutation})
		}
		t.Fatalf("reactive definition pipeline failed: status=%s tasks=%s episodes=%s notices=%s", definitionReport.RunStatus, failures, evidence, strings.Join(definitionReport.Acceptance.Notices, "; "))
	}
	definitionEpisodes, err := definitionStore.LoadEpisodes(definitionReport.RunID)
	if err != nil || len(definitionEpisodes) != 1 {
		t.Fatalf("load reactive definition episode: count=%d err=%v", len(definitionEpisodes), err)
	}
	definitionResponse, _ := json.Marshal(definitionEpisodes[0].Response)
	for _, fragment := range [][]byte{
		[]byte("Skill: data_write"),
		[]byte("Skill: watch_write"),
		[]byte("help:security"),
		[]byte("help:runtime"),
		[]byte("help:watches"),
		[]byte("named source and application relationship"),
		[]byte("exact catalog IDs"),
	} {
		if !bytes.Contains(definitionResponse, fragment) {
			t.Fatalf("writable/reactive executor trace omitted %q guidance: %s", fragment, definitionResponse)
		}
	}
	if definitionEpisodes[0].Mutation == nil || !definitionEpisodes[0].Mutation.PostStatePass || !definitionEpisodes[0].Mutation.CollateralPass {
		t.Fatalf("watch definition did not preserve mutation contract: %+v", definitionEpisodes[0].Mutation)
	}
	if !bytes.Contains(definitionResponse, []byte(`execute_graphql`)) || !bytes.Contains(definitionResponse, []byte(`gj_watch(insert:`)) {
		t.Fatalf("watch creation did not reach execute_graphql via gj_watch: %s", definitionResponse)
	}

	if err := resettable.Reset(context.Background()); err != nil {
		t.Fatal(err)
	}
	client.setSequence(
		`await final('Apply the caller capability facts to this request.', {});`,
		`const policy = await query_catalog({id:"help:security"}); await final({status:"blocked", answer:"The requested write is not available to this caller.", evidence:[policy]});`,
		`await final({status:"blocked", answer:"The requested write is not available to this caller."});`,
	)
	refusalSuite := *suite
	refusalSuite.Tasks = []gjeval.Task{*refusal}
	refusalSuite.Generator.Scale = 1
	if err := refusalSuite.Normalize(); err != nil {
		t.Fatal(err)
	}
	refusalStore := gjeval.NewStore(t.TempDir())
	refusalReport, err := (gjeval.Runner{}).Run(context.Background(), refusalSuite, instance, gjeval.RunOptions{Repeats: 1, Seed: 23, Store: refusalStore})
	if err != nil {
		t.Fatal(err)
	}
	if refusalReport.RunStatus != gjeval.RunStatusComplete || len(refusalReport.Tasks) != 1 || !refusalReport.Tasks[0].Pass || refusalReport.Metrics.ForbiddenAttempts != 0 {
		result, _ := json.Marshal(refusalReport.Tasks)
		t.Fatalf("scripted refusal regression: status=%s attempts=%d tasks=%s", refusalReport.RunStatus, refusalReport.Metrics.ForbiddenAttempts, result)
	}
}

func evalIntegrationGraphQL(baseURL string, headers map[string]string, query string) ([]byte, error) {
	payload, err := json.Marshal(map[string]any{"query": query})
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequest(http.MethodPost, strings.TrimRight(baseURL, "/")+"/api/v1/graphql", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	return io.ReadAll(response.Body)
}
