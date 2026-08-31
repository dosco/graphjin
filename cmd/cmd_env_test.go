package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
	gjagent "github.com/dosco/graphjin/agent/v3"
	gjeval "github.com/dosco/graphjin/agent/v3/eval"
)

func envTestSuite(t *testing.T) gjeval.Suite {
	t.Helper()
	read := gjeval.Task{
		Slug: "count-accounts", Category: gjeval.CategoryAggregate, Difficulty: gjeval.DifficultyT1,
		Prompt: "How many accounts are there?", ExpectedStatus: gjagent.StatusAnswered,
		Provenance: gjeval.Provenance{Source: "catalog-entity"},
		Oracle:     &gjeval.OracleSpec{Query: "query { accounts { count_id } }", Extract: "accounts.0.count_id"},
		Answer:     gjeval.AnswerRule{Kind: "number"},
		Behavior:   gjeval.BehaviorRule{RequiredActions: []string{"execute_graphql"}},
	}
	other := read
	other.Slug = "count-invoices"
	other.Prompt = "How many invoices are there?"
	other.Oracle = &gjeval.OracleSpec{Query: "query { invoices { count_id } }", Extract: "invoices.0.count_id"}
	suite := gjeval.Suite{
		SchemaVersion: gjeval.SuiteSchemaVersion, Name: "env",
		Generator: gjeval.GeneratorMeta{Version: gjeval.GeneratorVersion, Seed: 23, Scale: 2},
		Tasks:     []gjeval.Task{read, other},
	}
	if err := suite.Normalize(); err != nil {
		t.Fatal(err)
	}
	return suite
}

// startEnvTestServer boots one real world and serves it, so the handlers are
// exercised against the same environment a training loop would drive.
func startEnvTestServer(t *testing.T, program string) (*envServer, func()) {
	t.Helper()
	project := t.TempDir()
	if err := extractDefaultDemo(project); err != nil {
		t.Fatal(err)
	}
	originalPath, originalConf, originalDB, originalOpened := cpath, conf, db, dbOpened
	t.Setenv("GO_ENV", "dev")
	client := &evalScriptClient{code: program}
	environment := evalEnvironment{ClientFactory: func(gjagent.Config) (ax.AIClient, error) { return client, nil }}
	pool, err := newEvalInstancePool(context.Background(), func(int) evalEnvironment { return environment }, gjeval.EnvSpec{
		Target: gjeval.TargetDemo, ConfigPath: project, Seed: 23, FreezeTime: "2026-08-01T12:00:00Z",
	}, 1)
	if err != nil {
		cpath, conf, db, dbOpened = originalPath, originalConf, originalDB, originalOpened
		t.Fatal(err)
	}
	suite := envTestSuite(t)
	server := &envServer{
		pool: pool, suite: suite, profile: gjeval.RewardProfileRL, side: "train",
		byID: map[string]gjeval.Task{}, bySlug: map[string]gjeval.Task{},
	}
	if err := server.indexTasks(); err != nil {
		t.Fatal(err)
	}
	return server, func() {
		_ = pool.Close()
		cpath, conf, db, dbOpened = originalPath, originalConf, originalDB, originalOpened
	}
}

func TestEnvServerReportsWhatItIsServing(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded service integration")
	}
	server, stop := startEnvTestServer(t, `await final({status:"blocked",answer:"not configured"});`)
	defer stop()

	rec := httptest.NewRecorder()
	server.handleHealth(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var health envHealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &health); err != nil {
		t.Fatal(err)
	}
	if health.Status != "ready" || health.Workers != 1 || health.Tasks != 2 {
		t.Fatalf("health = %+v", health)
	}
	// A trainer has to be able to tell which world and which contract it is
	// being graded against, or two runs are not comparable.
	if health.RewardVersion == "" || health.Dataset.CatalogHash == "" {
		t.Fatalf("health must identify the dataset and reward contract: %+v", health)
	}

	rec = httptest.NewRecorder()
	server.handleTasks(rec, httptest.NewRequest(http.MethodGet, "/tasks", nil))
	var listing struct {
		Tasks []envTaskSummary `json:"tasks"`
		Count int              `json:"count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listing); err != nil {
		t.Fatal(err)
	}
	if listing.Count != 2 || listing.Tasks[0].Prompt == "" || listing.Tasks[0].TaskID == "" {
		t.Fatalf("task listing = %+v", listing)
	}
}

// The whole point of the server: one request runs a graded episode against a
// real world and returns what it was worth.
func TestEnvServerRunsAndGradesAnEpisode(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded service integration")
	}
	// The agent must discover a specific catalog card before running raw
	// GraphQL; a broad search alone is refused by the protocol guard, which is
	// the environment behaving as it does for a real policy.
	server, stop := startEnvTestServer(t, `
		const detail = await query_catalog({id: "table:app:main.accounts"});
		const res = await execute_graphql({query: "query { accounts { count_id } }"});
		await final({status: "answered", answer: "There are " + res.data.accounts[0].count_id + " accounts.", data: res.data, evidence: [detail]});
	`)
	defer stop()

	body, _ := json.Marshal(envEpisodeRequest{Slug: "count-accounts", IncludeTrajectory: true})
	rec := httptest.NewRecorder()
	server.handleEpisode(rec, httptest.NewRequest(http.MethodPost, "/episodes", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var episode envEpisodeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &episode); err != nil {
		t.Fatal(err)
	}
	if episode.Slug != "count-accounts" {
		t.Fatalf("wrong task served: %q", episode.Slug)
	}
	if episode.Answer == "" {
		t.Fatalf("episode produced no answer: %+v", episode)
	}
	// The agent read the real database, so the graded answer must be the one the
	// oracle independently computes.
	if !episode.Pass {
		t.Fatalf("a correct answer read from the live database did not pass: %+v (%s)", episode.Score.Vector, episode.Score.FailureCategory)
	}
	if episode.Reward <= 0 {
		t.Fatalf("a passing episode must be worth something, got %v", episode.Reward)
	}
	if episode.Trajectory == nil || len(episode.Trajectory.Steps) == 0 {
		t.Fatalf("a trajectory was requested and must come back with steps: %+v", episode.Trajectory)
	}
}

func TestEnvServerRefusesAnUnknownTask(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded service integration")
	}
	server, stop := startEnvTestServer(t, `await final({status:"blocked",answer:"x"});`)
	defer stop()

	body, _ := json.Marshal(envEpisodeRequest{Slug: "no-such-task"})
	rec := httptest.NewRecorder()
	server.handleEpisode(rec, httptest.NewRequest(http.MethodPost, "/episodes", bytes.NewReader(body)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
}

// A split exists so training and measurement never see the same task. The
// server picks the side rather than trusting the caller to filter, because a
// caller that filtered wrongly would produce a number that looks fine.
func TestEnvServerServesOnlyTheRequestedSideOfASplit(t *testing.T) {
	suite := envTestSuite(t)
	split, err := gjeval.SplitSuite(suite, 0.5, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(split.Train) == 0 || len(split.Eval) == 0 {
		t.Skip("split placed every task on one side; nothing to distinguish")
	}
	for _, side := range []string{"train", "eval"} {
		server := &envServer{
			suite: suite, split: &split, side: side,
			byID: map[string]gjeval.Task{}, bySlug: map[string]gjeval.Task{},
		}
		if err := server.indexTasks(); err != nil {
			t.Fatal(err)
		}
		wanted := split.Train
		if side == "eval" {
			wanted = split.Eval
		}
		if len(server.byID) != len(wanted) {
			t.Fatalf("%s side served %d tasks, want %d", side, len(server.byID), len(wanted))
		}
		for _, id := range wanted {
			if _, ok := server.byID[id]; !ok {
				t.Fatalf("%s side is missing task %s", side, id)
			}
		}
	}
}
