package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
	gjagent "github.com/dosco/graphjin/agent/v3"
	gjeval "github.com/dosco/graphjin/agent/v3/eval"
)

// End-to-end checks for the two families v16 adds, against a real booted
// instance and with no provider traffic: the model's part is a canned pick.
//
// The unit tests prove the constructors build what they claim. These prove the
// thing the constructors cannot: that what they build actually resolves against
// a live database, and that an episode doing the work passes.

type authoredE2E struct {
	instance  gjeval.Instance
	verifier  *gjeval.Verifier
	generator gjeval.Generator
	census    gjeval.SchemaCensus
	client    *evalScriptClient
	project   string
}

func startAuthoredE2E(t *testing.T) (*authoredE2E, func()) {
	t.Helper()
	project := t.TempDir()
	if err := extractDefaultDemo(project); err != nil {
		t.Fatal(err)
	}
	originalPath, originalConf, originalDB, originalOpened := cpath, conf, db, dbOpened
	t.Setenv("GO_ENV", "dev")

	client := &evalScriptClient{code: `await final({status:"blocked",answer:"not configured"});`}
	environment := evalEnvironment{
		ClientFactory: func(gjagent.Config) (ax.AIClient, error) { return client, nil },
	}
	instance, err := environment.Start(context.Background(), gjeval.EnvSpec{
		Target: gjeval.TargetDemo, ConfigPath: project, Seed: 23,
		Writable: true, Reactive: true, Resettable: true,
	})
	if err != nil {
		cpath, conf, db, dbOpened = originalPath, originalConf, originalDB, originalOpened
		t.Fatal(err)
	}
	verifier := &gjeval.Verifier{BaseURL: instance.BaseURL(), Headers: instance.Headers()}
	source := gjeval.HTTPCatalogSource{BaseURL: instance.BaseURL(), Headers: instance.Headers()}
	snapshot, err := source.Snapshot(context.Background())
	if err != nil {
		_ = instance.Close()
		cpath, conf, db, dbOpened = originalPath, originalConf, originalDB, originalOpened
		t.Fatal(err)
	}
	return &authoredE2E{
		instance: instance, verifier: verifier, client: client, project: project,
		generator: gjeval.Generator{Source: source, Verifier: verifier},
		census:    gjeval.BuildCensus(snapshot),
	}, func() {
		_ = instance.Close()
		cpath, conf, db, dbOpened = originalPath, originalConf, originalDB, originalOpened
	}
}

// cannedPicks answers one authoring call without a provider.
func cannedPicks(picks any) gjeval.OneShotFunc {
	return func(context.Context, string, map[string]any) (map[string]any, error) {
		return map[string]any{"picks_json": gjeval.MarshalAuthoringPicks(picks)}, nil
	}
}

// The whole file family stands on one claim: the engine writes the document, so
// the answer it grades against is true by construction. This checks that the
// document really is written, that the two-root oracle really resolves against
// the running instance, and that an episode reading both really passes.
func TestAuthoredFileTaskResolvesAndPassesEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded service integration")
	}
	env, stop := startAuthoredE2E(t)
	defer stop()

	if len(env.census.FileTables) == 0 {
		t.Fatal("the demo serves documents; the census should have found them")
	}
	fileRoot := env.census.FileTables[0]

	pick := gjeval.FilePick{
		FileRoot: fileRoot, Table: "support_tickets", Column: "severity", Value: "urgent",
		PolicyTopic: "urgent ticket response", PolicyAnswer: "4 hours",
		Intent:    "Support leadership wants to know whether we are behind on the most serious tickets, and how quickly we are actually meant to deal with them.",
		Execution: "Check the written response standard for the most serious tickets and count how many of those are open right now.",
	}
	authored, report, err := gjeval.AuthorFamilies(context.Background(), cannedPicks([]gjeval.FilePick{pick}),
		env.census, nil, gjeval.AuthoringOptions{
			Kinds: []gjeval.AuthoringKind{gjeval.AuthoringFile}, AuthoredBy: "test/model",
			ResolveOracle: env.verifier.Resolve,
		})
	if err != nil {
		t.Fatal(err)
	}
	if len(authored) != 2 || len(report.Files) != 1 {
		t.Fatalf("expected two tasks and one document: tasks=%d files=%d rejections=%v",
			len(authored), len(report.Files), report.Rejections)
	}

	// The document has to exist before the tasks that read it can be verified.
	written, err := writeAuthoredFiles(env.project, gjeval.TargetDemo, report.Files)
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(written[0])
	if err != nil || !strings.Contains(string(body), "Requirement: 4 hours.") {
		t.Fatalf("the requirement is not on disk: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(written[0]) })

	// Verification is the real check: it resolves the oracle — which spans the
	// database and the document — against the running instance.
	verified, err := env.generator.VerifyTasks(context.Background(), authored, 23, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(verified) != 2 {
		t.Fatalf("the two-source oracle did not resolve against the live instance: %d survived", len(verified))
	}

	// And an episode that reads both sources and answers correctly passes.
	task := verified[1]
	oracle, err := env.verifier.Resolve(context.Background(), *task.Oracle)
	if err != nil {
		t.Fatal(err)
	}
	if oracle.Dimension != "4 hours" {
		t.Fatalf("the planted requirement did not reach the oracle: %q", oracle.Dimension)
	}
	queryJSON, _ := json.Marshal(task.Oracle.Query)
	answerJSON, _ := json.Marshal(fmt.Sprintf(
		"There are %s open urgent tickets, and the standard requires a response within %s.",
		oracle.Value, oracle.Dimension))
	env.client.setCode(fmt.Sprintf(
		`const detail = await query_catalog({id:"table:app:main.support_tickets"});
		 const execution = await execute_graphql({query:%s});
		 await final({status:"answered",answer:%s,data:execution.data,evidence:[detail]});`,
		queryJSON, answerJSON))

	episode, err := (gjeval.Runner{}).RunEpisode(context.Background(), env.instance, task,
		gjeval.EpisodeOptions{Profile: gjeval.RewardProfileRL})
	if err != nil {
		t.Fatal(err)
	}
	if !episode.Score.Pass {
		detail, _ := json.Marshal(episode.Score)
		t.Fatalf("an episode that read both sources failed: %s", detail)
	}
}

// A delivery task is the one family whose environment has to do something
// before the agent is asked anything: install the watch, wait for the runner to
// fire it, and only then hand over. Nothing but a live run checks that.
func TestAuthoredDeliveryTaskDeliversAndPassesEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded service integration")
	}
	env, stop := startAuthoredE2E(t)
	defer stop()

	pick := gjeval.WatchPick{
		Table: "invoices", Column: "status", Value: "failed", Name: "e2e_failed_invoices",
		Intent: "Finance keeps finding out about payments that did not go through days later, and wants to hear about new ones without checking anything.",
	}
	authored, report, err := gjeval.AuthorFamilies(context.Background(), cannedPicks([]gjeval.WatchPick{pick}),
		env.census, nil, gjeval.AuthoringOptions{
			Kinds: []gjeval.AuthoringKind{gjeval.AuthoringWatch}, AuthoredBy: "test/model",
			ResolveOracle: env.verifier.Resolve,
		})
	if err != nil {
		t.Fatal(err)
	}
	// Authored tasks go through the same verification `eval author` applies, so
	// this exercises the path a real suite takes rather than a shortcut.
	verified, err := env.generator.VerifyTasks(context.Background(), authored, 23, 2)
	if err != nil {
		t.Fatal(err)
	}
	var delivery *gjeval.Task
	for i := range verified {
		if verified[i].Provenance.Source == "authored-watch-delivery" {
			delivery = &verified[i]
		}
	}
	if delivery == nil {
		t.Fatalf("no delivery task survived authoring and verification: notes=%v rejections=%v",
			report.Notes, report.Rejections)
	}

	// Read the delivered event and mark it seen — the work the task asks for.
	// The event is cleared by id, which is the shape the runtime accepts.
	env.client.setCode(`const help = await query_catalog({id:"help:watches"});
		const inbox = await execute_graphql({query:"query { gj_watch_event(where: {seen: {eq: false}}, order_by: {created_at: desc}, limit: 1) { id watch_id data_json seen } }"});
		const event = inbox.data.gj_watch_event[0];
		const marked = await execute_graphql({query:"mutation MarkSeen($id: String!) { gj_watch_event(where: {id: {eq: $id}}, update: {seen: true}) { id seen } }", variables:{id:event.id}});
		await final({status:"answered",answer:"Reviewed the delivered invoice event and marked it seen.",data:{event,marked:marked.data},evidence:[help]});`)

	episode, err := (gjeval.Runner{}).RunEpisode(context.Background(), env.instance, *delivery,
		gjeval.EpisodeOptions{Profile: gjeval.RewardProfileRL})
	if err != nil {
		t.Fatal(err)
	}
	if episode.Mutation == nil {
		t.Fatalf("no write evidence: %+v", episode.Score)
	}
	// The event has to have been delivered at all: without that the episode
	// would have timed out waiting and there would be nothing to mark seen.
	if !episode.Mutation.PostStatePass {
		detail, _ := json.Marshal(episode.Mutation)
		t.Fatalf("the watch never delivered an event the agent could clear: %s", detail)
	}
	if !episode.Score.Pass {
		detail, _ := json.Marshal(episode.Score)
		t.Fatalf("a compliant delivery episode failed: %s", detail)
	}
}

// The clone path end to end: a project with documents boots, and its file
// source really serves them.
func TestClonedFileSourceServesItsDocuments(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded service integration")
	}
	world, _ := cloneWorldSpec(fileSourceSchema(), cloneOptions{Rows: 6, Seed: 3}, "Clone")
	project := t.TempDir()
	inner := filepath.Join(project, "world")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeWorld(world, inner); err != nil {
		t.Fatal(err)
	}
	originalPath, originalConf, originalDB, originalOpened := cpath, conf, db, dbOpened
	t.Setenv("GO_ENV", "dev")
	defer func() { cpath, conf, db, dbOpened = originalPath, originalConf, originalDB, originalOpened }()

	instance, err := (evalEnvironment{}).Start(context.Background(), gjeval.EnvSpec{
		Target: gjeval.TargetDemo, ConfigPath: inner, Seed: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close() //nolint:errcheck

	verifier := &gjeval.Verifier{BaseURL: instance.BaseURL(), Headers: instance.Headers()}
	result, err := verifier.Resolve(context.Background(), gjeval.OracleSpec{
		Query:   `query { sla_policies(key: "policy-overview.md", inline_data: true) { key text } }`,
		Extract: "sla_policies.0.key",
	})
	if err != nil {
		t.Fatalf("the cloned file source did not serve its documents: %v", err)
	}
	if result.Value != "policy-overview.md" {
		t.Fatalf("unexpected document: %q", result.Value)
	}
}
