package serv

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	gjagent "github.com/dosco/graphjin/agent/v3"
	gjeval "github.com/dosco/graphjin/agent/v3/eval"
	"github.com/dosco/graphjin/core/v3"
)

func scoreTestTask(t *testing.T) gjeval.Task {
	t.Helper()
	task := gjeval.Task{
		Slug: "score-endpoint", Category: gjeval.CategoryAggregate, Difficulty: gjeval.DifficultyT1,
		Prompt: "How many accounts are there?", ExpectedStatus: gjagent.StatusAnswered,
		Provenance: gjeval.Provenance{Source: "imported"},
		Oracle:     &gjeval.OracleSpec{Query: "query { accounts { count_id } }", Extract: "accounts.0.count_id"},
		Answer:     gjeval.AnswerRule{Kind: "number"},
		Behavior:   gjeval.BehaviorRule{RequiredActions: []string{"execute_graphql"}},
	}
	if err := task.Normalize(); err != nil {
		t.Fatal(err)
	}
	return task
}

func scoreTestResponse(answer string) gjagent.Response {
	return gjagent.Response{
		Status: gjagent.StatusAnswered, Answer: answer,
		Actions: []map[string]any{{
			"tool": "execute_graphql", "status": "ok",
			"args":    map[string]any{"query": "query { accounts { count_id } }"},
			"summary": map[string]any{"error_count": 0},
		}},
	}
}

func postScore(t *testing.T, body any) (*httptest.ResponseRecorder, evalScoreResponse) {
	t.Helper()
	hs := newAgentHTTPTestService(&Config{Core: core.Config{Mode: "dev"}, Serv: Serv{ConfigPath: t.TempDir()}})
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, routeEvalScore, bytes.NewReader(encoded))
	rec := httptest.NewRecorder()
	hs.EvalScore(nil).ServeHTTP(rec, req)
	var response evalScoreResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &response)
	return rec, response
}

// A trainer scoring over HTTP must get exactly what the harness computes in
// process. Two rewards that disagree about the same episode are worse than one
// reward: the disagreement surfaces as a model that improves on one number and
// not the other.
func TestEvalScoreMatchesInProcessScoring(t *testing.T) {
	task := scoreTestTask(t)
	response := scoreTestResponse("There are 8 accounts.")
	oracle := gjeval.OracleResult{Value: "8"}

	rec, scored := postScore(t, evalScoreRequest{
		Task: task, Response: response, Oracle: &oracle, LatencyMS: 900,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	want := gjeval.Score(task, &oracle, response, 900)
	if scored.Score.Pass != want.Pass || scored.Reward != want.Vector.Reward {
		t.Fatalf("endpoint disagreed with in-process scoring: %+v vs %+v", scored.Score.Vector, want.Vector)
	}
	if !scored.Score.Pass {
		t.Fatalf("a correct answer must pass: %+v", scored.Score)
	}
	if scored.RewardVersion != gjeval.RewardVersion {
		t.Fatalf("reward version = %q", scored.RewardVersion)
	}
}

func TestEvalScoreGradesAWrongAnswerWrong(t *testing.T) {
	_, scored := postScore(t, evalScoreRequest{
		Task: scoreTestTask(t), Response: scoreTestResponse("There are 12 accounts."),
		Oracle: &gjeval.OracleResult{Value: "8"},
	})
	if scored.Score.Pass {
		t.Fatal("a wrong answer must not pass")
	}
}

// The two profiles agree at the top — a perfect episode is worth everything
// under either — and diverge on everything else. A wrong answer still collects
// most of the board's reward for having been safe and well-formed, which is
// what makes that number comparable across models; the training profile pays
// mostly for being right.
func TestEvalScoreHonoursTheTrainingProfile(t *testing.T) {
	task := scoreTestTask(t)
	wrong := scoreTestResponse("There are 12 accounts.")
	oracle := gjeval.OracleResult{Value: "8"}

	_, benchmark := postScore(t, evalScoreRequest{Task: task, Response: wrong, Oracle: &oracle})
	_, training := postScore(t, evalScoreRequest{
		Task: task, Response: wrong, Oracle: &oracle, Profile: string(gjeval.RewardProfileRL),
	})
	if training.RewardProfile != string(gjeval.RewardProfileRL) {
		t.Fatalf("profile not echoed: %q", training.RewardProfile)
	}
	if training.Reward >= benchmark.Reward {
		t.Fatalf("a wrong answer should cost more under training (%v) than on the board (%v)",
			training.Reward, benchmark.Reward)
	}

	// Both saturate on a correct, efficient episode, and that agreement is the
	// point rather than an accident: the profiles differ in what they punish.
	right := scoreTestResponse("There are 8 accounts.")
	_, benchmarkRight := postScore(t, evalScoreRequest{Task: task, Response: right, Oracle: &oracle})
	_, trainingRight := postScore(t, evalScoreRequest{
		Task: task, Response: right, Oracle: &oracle, Profile: string(gjeval.RewardProfileRL),
	})
	if benchmarkRight.Reward <= benchmark.Reward || trainingRight.Reward <= training.Reward {
		t.Fatal("a correct answer must outscore a wrong one under both profiles")
	}
}

// A misspelled profile must be refused rather than quietly graded under the
// published contract, which would leave a trainer optimizing against the wrong
// weights with no way to notice.
func TestEvalScoreRejectsAnUnknownProfile(t *testing.T) {
	rec, _ := postScore(t, evalScoreRequest{
		Task: scoreTestTask(t), Response: scoreTestResponse("8"),
		Oracle: &gjeval.OracleResult{Value: "8"}, Profile: "RL",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

// A write is graded by the state the database ended in, which only the caller
// can observe.
func TestEvalScoreGradesAWriteByItsObservedEffect(t *testing.T) {
	task := scoreTestTask(t)
	response := scoreTestResponse("Done.")
	landed := gjeval.MutationOutcome{PostStatePass: true, CollateralPass: true}
	missed := gjeval.MutationOutcome{PostStatePass: false, CollateralPass: true}

	_, ok := postScore(t, evalScoreRequest{Task: task, Response: response, Oracle: &gjeval.OracleResult{Value: "8"}, Mutation: &landed})
	_, bad := postScore(t, evalScoreRequest{Task: task, Response: response, Oracle: &gjeval.OracleResult{Value: "8"}, Mutation: &missed})
	if !ok.Score.Pass {
		t.Fatalf("a write that landed must pass: %+v", ok.Score)
	}
	if bad.Score.Pass {
		t.Fatal("a write that never landed must not pass")
	}
	if bad.Reward >= ok.Reward {
		t.Fatalf("an unlanded write (%v) must be worth less than one that landed (%v)", bad.Reward, ok.Reward)
	}
}

func TestEvalScoreRejectsNonPost(t *testing.T) {
	hs := newAgentHTTPTestService(&Config{Core: core.Config{Mode: "dev"}, Serv: Serv{ConfigPath: t.TempDir()}})
	req := httptest.NewRequest(http.MethodGet, routeEvalScore, nil)
	rec := httptest.NewRecorder()
	hs.EvalScore(nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}
