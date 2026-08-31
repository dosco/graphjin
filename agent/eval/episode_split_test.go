package eval

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	gjagent "github.com/dosco/graphjin/agent/v3"
)

// resettableStatic is a static instance that counts how often it was put back.
type resettableStatic struct {
	*StaticInstance
	mu     sync.Mutex
	resets int
}

func (r *resettableStatic) Reset(context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.resets++
	return nil
}

// writingDoer answers the agent with a compliant write and answers every oracle
// read with the value a landed write would leave behind.
type writingDoer struct {
	mu sync.Mutex
}

func (d *writingDoer) Do(request *http.Request) (*http.Response, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if strings.HasSuffix(request.URL.Path, "/graphql") {
		return jsonResponse(200, `{"data":{"accounts":[{"count_id":1,"plan":"enterprise"}]}}`), nil
	}
	response := responseWithAnswer(gjagent.StatusAnswered, "Updated the account.")
	response.Actions = []map[string]any{
		{"tool": "query_catalog", "status": "ok"},
		{"tool": "execute_graphql", "status": "ok",
			"args": map[string]any{"query": `mutation { accounts(update: {plan: "enterprise"}, where: {id: {eq: 1}}) { id } }`}},
	}
	data, _ := json.Marshal(response)
	return jsonResponse(200, string(data)), nil
}

func splitTestTask(t *testing.T) Task {
	t.Helper()
	task := Task{
		Slug: "set-plan", Category: CategoryAction, Difficulty: DifficultyT3,
		Prompt:     "Move account 1 onto the enterprise plan.",
		Provenance: Provenance{Source: "row-update"}, ExpectedStatus: gjagent.StatusAnswered,
		Method:   MethodRule{RequireQueryMatch: []string{`(?s)mutation.*accounts.*update`}},
		Behavior: BehaviorRule{RequiredActions: []string{"query_catalog", "execute_graphql"}},
		Mutation: &MutationSpec{
			ResetStrategy: "sqlite-copy",
			PostState: OracleSpec{
				Query:   `query { accounts(where: {id: {eq: 1}}, limit: 1) { count_id plan } }`,
				Extract: "accounts.0.count_id",
			},
			ExpectedValue: "1",
			Collateral: []OracleSpec{{
				Query: `query { accounts(order_by: {id: asc}) { id plan } }`, Extract: "accounts",
			}},
		},
	}
	if err := task.Normalize(); err != nil {
		t.Fatal(err)
	}
	return task
}

// Splitting the episode into "prepare the world" and "grade what happened" is
// only safe if it produces the same verdict as running the whole thing. A
// caller driving the agent itself gets exactly the score the benchmark would
// have given, or the two measure different things while claiming not to.
func TestBeginAndFinishAgreeWithRunEpisode(t *testing.T) {
	task := splitTestTask(t)
	runner := Runner{Client: &writingDoer{}}

	whole := &resettableStatic{StaticInstance: &StaticInstance{URL: "http://graphjin.test", TargetLabel: "test"}}
	episode, err := runner.RunEpisode(context.Background(), whole, task, EpisodeOptions{Profile: RewardProfileRL})
	if err != nil {
		t.Fatal(err)
	}

	// The same work, driven a step at a time.
	split := &resettableStatic{StaticInstance: &StaticInstance{URL: "http://graphjin.test", TargetLabel: "test"}}
	prep, err := runner.BeginEpisodeEnvironment(context.Background(), split, task)
	if err != nil {
		t.Fatal(err)
	}
	if prep.Resettable == nil {
		t.Fatal("a write task must carry the means to put the world back")
	}
	if len(prep.CollateralBefore) != 1 {
		t.Fatalf("collateral was not recorded before the agent ran: %+v", prep.CollateralBefore)
	}
	// The same response both paths saw, so any difference in the verdict is the
	// grading and not the agent.
	response, ok := episode.Response.(gjagent.Response)
	if !ok {
		t.Fatalf("unexpected response type %T", episode.Response)
	}
	detail, evidence, err := runner.FinishEpisodeScoring(context.Background(), split, task, prep,
		response, episode.LatencyMS, RewardProfileRL)
	if err != nil {
		t.Fatal(err)
	}

	if detail.Pass != episode.Score.Pass || detail.Vector.Reward != episode.Score.Vector.Reward {
		t.Fatalf("the split path graded differently: %+v vs %+v", detail, episode.Score)
	}
	if detail.FailureCategory != episode.Score.FailureCategory {
		t.Fatalf("failure category differs: %q vs %q", detail.FailureCategory, episode.Score.FailureCategory)
	}
	if evidence == nil || episode.Mutation == nil {
		t.Fatal("both paths must produce write evidence")
	}
	if evidence.PostStatePass != episode.Mutation.PostStatePass ||
		evidence.CollateralPass != episode.Mutation.CollateralPass {
		t.Fatalf("write evidence differs: %+v vs %+v", evidence, episode.Mutation)
	}
	// Both put the world back the same number of times: once before, once after.
	if split.resets != whole.resets {
		t.Fatalf("reset counts differ: split %d, whole %d", split.resets, whole.resets)
	}
}

// A read-only task needs no reset and no collateral, and must still grade.
func TestBeginAndFinishHandleAReadOnlyTask(t *testing.T) {
	task := scoredTask(t)
	runner := Runner{Client: &scriptedEvalDoer{alwaysPass: true}}
	instance := &StaticInstance{URL: "http://graphjin.test", TargetLabel: "test"}

	prep, err := runner.BeginEpisodeEnvironment(context.Background(), instance, task)
	if err != nil {
		t.Fatal(err)
	}
	if prep.Resettable != nil {
		t.Fatal("a read must not claim it needs the world put back")
	}
	if prep.Oracle == nil || prep.Oracle.Value != "42" {
		t.Fatalf("ground truth was not resolved: %+v", prep.Oracle)
	}
	response := responseWithAnswer(gjagent.StatusAnswered, "The total is 42.")
	response.Skills = []gjagent.SkillUsage{{ID: "data_discovery"}}
	response.Actions = []map[string]any{
		{"tool": "query_catalog", "status": "ok"},
		{"tool": "execute_graphql", "status": "ok", "args": map[string]any{"query": "query { accounts { sum_mrr } }"}},
	}
	detail, evidence, err := runner.FinishEpisodeScoring(context.Background(), instance, task, prep, response, 900, RewardProfileRL)
	if err != nil {
		t.Fatal(err)
	}
	if !detail.Pass || evidence != nil {
		t.Fatalf("a correct read must pass with no write evidence: %+v %+v", detail, evidence)
	}
}

// An unknown profile has to be refused before anything is graded, not after.
func TestFinishEpisodeScoringRejectsAnUnknownProfile(t *testing.T) {
	task := scoredTask(t)
	runner := Runner{Client: &scriptedEvalDoer{alwaysPass: true}}
	instance := &StaticInstance{URL: "http://graphjin.test", TargetLabel: "test"}
	prep, err := runner.BeginEpisodeEnvironment(context.Background(), instance, task)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := runner.FinishEpisodeScoring(context.Background(), instance, task, prep,
		responseWithAnswer(gjagent.StatusAnswered, "42"), 900, RewardProfile("greedy")); err == nil {
		t.Fatal("an unknown reward profile must be refused")
	}
}
