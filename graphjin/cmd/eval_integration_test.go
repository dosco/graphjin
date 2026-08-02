package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	ax "github.com/ax-llm/ax/packages/go"
	gjagent "github.com/dosco/graphjin/agent/v3"
	gjeval "github.com/dosco/graphjin/agent/v3/eval"
)

type evalScriptClient struct {
	mu   sync.RWMutex
	code string
}

func (c *evalScriptClient) setCode(code string) {
	c.mu.Lock()
	c.code = code
	c.mu.Unlock()
}

func (c *evalScriptClient) Chat(context.Context, map[string]ax.Value, map[string]ax.Value) (ax.Value, error) {
	c.mu.RLock()
	code := c.code
	c.mu.RUnlock()
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
