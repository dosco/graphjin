package main

import (
	"context"
	"strings"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
	gjagent "github.com/dosco/graphjin/agent/v3"
	gjeval "github.com/dosco/graphjin/agent/v3/eval"
)

// Authoring reaches the database twice: once to learn the schema, once to prove
// every task it produced can actually be graded. This drives both against a
// real booted demo, with the model call faked, so the whole path is exercised
// without provider traffic.
func TestAuthoredTasksSurviveVerificationAgainstALiveDatabase(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded service integration")
	}
	project := t.TempDir()
	if err := extractDefaultDemo(project); err != nil {
		t.Fatal(err)
	}
	originalPath, originalConf, originalDB, originalOpened := cpath, conf, db, dbOpened
	defer func() { cpath, conf, db, dbOpened = originalPath, originalConf, originalDB, originalOpened }()
	t.Setenv("GO_ENV", "dev")

	client := &evalScriptClient{code: `await final({status:"blocked",answer:"not configured"});`}
	environment := evalEnvironment{ClientFactory: func(gjagent.Config) (ax.AIClient, error) { return client, nil }}
	instance, err := environment.Start(context.Background(), gjeval.EnvSpec{
		Target: gjeval.TargetDemo, ConfigPath: project, Seed: 23,
		Writable: true, Reactive: true, Resettable: true, FreezeTime: "2026-08-01T12:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close() //nolint:errcheck

	source := gjeval.HTTPCatalogSource{BaseURL: instance.BaseURL(), Headers: instance.Headers()}
	verifier := &gjeval.Verifier{BaseURL: instance.BaseURL(), Headers: instance.Headers()}
	snapshot, err := source.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	census := gjeval.BuildCensus(snapshot)

	// Stand in for the model with picks a capable one would plausibly return.
	call := func(_ context.Context, signature string, _ map[string]any) (map[string]any, error) {
		switch {
		case strings.Contains(signature, "standing questions"):
			return map[string]any{"picks_json": gjeval.MarshalAuthoringPicks([]gjeval.WatchPick{{
				Table: "invoices", Column: "status", Value: "failed", Name: "failed_invoices",
				Intent: "Finance keeps finding out about payments that did not go through days after the fact. They want to hear about new ones within the hour without having to check anything.",
			}})}, nil
		case strings.Contains(signature, "two turns before"):
			return map[string]any{"picks_json": gjeval.MarshalAuthoringPicks([]gjeval.ConfirmationPick{{
				Table: "invoices", Column: "status", Value: "failed",
				Need:     "We keep missing payments that fail, and nobody notices until the end of the week.",
				Proposal: "I can set up an alert called failed_invoices that sends you an hourly digest of new failures.",
			}})}, nil
		}
		return map[string]any{"picks_json": "[]"}, nil
	}

	authored, report, err := gjeval.AuthorFamilies(context.Background(), call, census, nil,
		gjeval.AuthoringOptions{
			Kinds:      []gjeval.AuthoringKind{gjeval.AuthoringWatch, gjeval.AuthoringConfirmation},
			Seed:       23,
			AuthoredBy: "test/model prompts@" + gjeval.AuthoringPromptsHash(),
		})
	if err != nil {
		t.Fatal(err)
	}
	if len(authored) == 0 {
		t.Fatalf("nothing was authored: %+v", report)
	}

	generator := gjeval.Generator{Source: source, Verifier: verifier}
	verified, err := generator.VerifyTasks(context.Background(), authored, 23, 4)
	if err != nil {
		t.Fatal(err)
	}
	// Verification is the whole point: a watch post-state that cannot be
	// resolved against this database must not reach a suite.
	if len(verified) == 0 {
		t.Fatalf("no authored task survived verification (%d proposed)", len(authored))
	}
	var sawReactive, sawMultiTurn bool
	for _, task := range verified {
		switch task.Category {
		case gjeval.CategoryReactive:
			sawReactive = true
		case gjeval.CategoryMultiTurn:
			sawMultiTurn = true
		}
		if task.Provenance.AuthoredBy == "" {
			t.Fatalf("an authored task lost its authorship: %+v", task.Provenance)
		}
		if task.Mutation == nil {
			t.Fatalf("an authored task lost its write check: %s", task.Slug)
		}
	}
	if !sawReactive || !sawMultiTurn {
		t.Fatalf("expected both a watch and a confirmation to survive, got reactive=%v multi-turn=%v",
			sawReactive, sawMultiTurn)
	}

	// The whole reason this family was hardcoded: these categories previously
	// existed only for the one demo schema.
	t.Logf("verified %d authored tasks across %d proposals", len(verified), len(authored))
}

// Authored tasks join generation as ordinary candidates: they are sampled with
// everything else rather than bypassing it.
func TestAuthoredTasksAreSampledAsCandidates(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded service integration")
	}
	project := t.TempDir()
	if err := extractDefaultDemo(project); err != nil {
		t.Fatal(err)
	}
	originalPath, originalConf, originalDB, originalOpened := cpath, conf, db, dbOpened
	defer func() { cpath, conf, db, dbOpened = originalPath, originalConf, originalDB, originalOpened }()
	t.Setenv("GO_ENV", "dev")

	client := &evalScriptClient{code: `await final({status:"blocked",answer:"x"});`}
	environment := evalEnvironment{ClientFactory: func(gjagent.Config) (ax.AIClient, error) { return client, nil }}
	instance, err := environment.Start(context.Background(), gjeval.EnvSpec{
		Target: gjeval.TargetDemo, ConfigPath: project, Seed: 23, FreezeTime: "2026-08-01T12:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close() //nolint:errcheck

	source := gjeval.HTTPCatalogSource{BaseURL: instance.BaseURL(), Headers: instance.Headers()}
	verifier := &gjeval.Verifier{BaseURL: instance.BaseURL(), Headers: instance.Headers()}
	generator := gjeval.Generator{Source: source, Verifier: verifier}

	// An authored read task, verifiable against the demo.
	authored := gjeval.Task{
		Slug: "authored-account-count", Category: gjeval.CategoryAggregate, Difficulty: gjeval.DifficultyT2,
		Prompt:     "The board meets shortly and someone will ask how big the customer base is. How many are there?",
		Provenance: gjeval.Provenance{Source: "authored-scenario", AuthoredBy: "test/model"},
		Oracle: &gjeval.OracleSpec{
			Query: "query { accounts { count_id } }", Extract: "accounts.0.count_id",
		},
		Answer:         gjeval.AnswerRule{Kind: "number"},
		ExpectedStatus: gjagent.StatusAnswered,
		Behavior:       gjeval.BehaviorRule{RequiredActions: []string{"execute_graphql"}},
	}
	if err := authored.Normalize(); err != nil {
		t.Fatal(err)
	}

	// The scale is deliberately larger than the candidate pool: this asserts
	// that an authored task survives verification and dedupe, not that it wins
	// a sampling contest against several hundred derived ones.
	suite, err := generator.Generate(context.Background(), gjeval.GeneratorOptions{
		Seed: 23, Scale: 5000, Composition: gjeval.CompositionCoverage,
		VerifyConcurrency: 8, Authored: []gjeval.Task{authored},
	})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, task := range suite.Tasks {
		if task.Provenance.Source == "authored-scenario" {
			found = true
			if task.Provenance.AuthoredBy != "test/model" {
				t.Fatalf("authorship was lost through generation: %+v", task.Provenance)
			}
		}
	}
	if !found {
		t.Fatal("the authored task never reached the suite")
	}
}
