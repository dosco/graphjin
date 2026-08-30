package eval

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// traceEpisode builds an episode carrying a trace of the shape the agent really
// records: chat_log entries holding the rendered prompt and the completion, and
// ordered events holding each executed program with its result.
func traceEpisode(prompts bool, programs ...string) Episode {
	chatLog := make([]any, 0, len(programs))
	events := make([]any, 0, len(programs))
	for _, program := range programs {
		entry := map[string]any{"name": "executor"}
		if prompts {
			entry["item0"] = map[string]any{"chat_prompt": []any{
				map[string]any{"role": "system", "content": "You are GraphJin's executor."},
				map[string]any{"role": "user", "content": "How many accounts are there?"},
			}}
		} else {
			entry["item0"] = map[string]any{}
		}
		completion, _ := json.Marshal(map[string]string{"javascriptCode": program})
		entry["item1"] = map[string]any{"content": string(completion)}
		chatLog = append(chatLog, entry)
	}
	// A real trace brackets each program with the stage that produced it, and
	// the stage is what the export reads. A fixture without those events would
	// let a broken stage filter pass here and fail against a live run.
	for _, program := range programs {
		events = append(events,
			map[string]any{"kind": "stage_request", "component_id": "agent.stage.executor",
				"payload": map[string]any{"stage": "task"}},
			map[string]any{"kind": "stage_response", "component_id": "agent.stage.executor",
				"payload": map[string]any{"stage": "task"}},
			map[string]any{
				"kind": "runtime_execute",
				"payload": map[string]any{
					"code": program, "is_error": false,
					"result": map[string]any{"data": map[string]any{"accounts": []any{map[string]any{"count_id": 8}}}},
				},
			})
	}
	return Episode{
		RunID: "run-1", TaskID: "gjv1_abc", TaskSlug: "aggregate-accounts", Repeat: 1,
		Request: EpisodeRequest{Instruction: "How many accounts are there?"},
		Task:    Task{Category: CategoryAggregate, Difficulty: DifficultyT1},
		Score:   ScoreDetail{Pass: true, Vector: ScoreVector{Safety: true, Behavior: true, GroundTruth: boolPointer(true), Method: boolPointer(true), Reward: 0.95}},
		Response: map[string]any{
			"status": "answered", "answer": "There are 8 accounts.",
			"trace": map[string]any{"chat_log": chatLog, "events": events},
		},
	}
}

func TestTrajectoryCarriesProgramsAndObservations(t *testing.T) {
	episode := traceEpisode(true, `await execute_graphql({query: "query { accounts { count_id } }"});`)
	trajectory, err := BuildTrajectory(episode, TrajectoryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(trajectory.Steps) != 1 {
		t.Fatalf("expected one step, got %d", len(trajectory.Steps))
	}
	step := trajectory.Steps[0]
	if !strings.Contains(step.Program, "execute_graphql") {
		t.Fatalf("the program did not survive: %q", step.Program)
	}
	if !strings.Contains(step.Observation, "count_id") {
		t.Fatalf("the observation did not survive: %q", step.Observation)
	}
	if step.Author != AuthorModel {
		t.Fatalf("a program the model emitted must be attributed to it, got %q", step.Author)
	}
	if len(step.Prompt) == 0 {
		t.Fatal("the rendered prompt must be carried when the trace has one")
	}
	if !trajectory.PromptsRecorded || !trajectory.AuthorshipResolved {
		t.Fatalf("a complete trace must report itself complete: %+v", trajectory)
	}
	if trajectory.Instruction == "" || trajectory.RewardVersion == "" {
		t.Fatalf("trajectory is missing provenance: %+v", trajectory)
	}
}

// The runtime writes and runs programs of its own, and they appear in the trace
// exactly like the policy's. Exporting them unmarked would train a model to
// imitate the environment's corrections rather than to stop needing them.
func TestTrajectorySeparatesEnvironmentAuthoredPrograms(t *testing.T) {
	episode := traceEpisode(true, `await execute_graphql({query: "query { accounts { count_id } }"});`)
	// The runtime executed a continuation the model never emitted.
	trace := episode.Response.(map[string]any)["trace"].(map[string]any)
	trace["events"] = append(trace["events"].([]any), map[string]any{
		"kind": "runtime_execute",
		"payload": map[string]any{
			"code": `await final({status:"answered", answer:"recovered"});`, "is_error": false,
			"result": map[string]any{"ok": true},
		},
	})

	excluded, err := BuildTrajectory(episode, TrajectoryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(excluded.Steps) != 1 {
		t.Fatalf("environment programs must be dropped by default, got %d steps", len(excluded.Steps))
	}

	included, err := BuildTrajectory(episode, TrajectoryOptions{IncludeEnvironmentSteps: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(included.Steps) != 2 {
		t.Fatalf("expected both steps when environment steps are kept, got %d", len(included.Steps))
	}
	var sawEnvironment bool
	for _, step := range included.Steps {
		if step.Author == AuthorEnvironment {
			sawEnvironment = true
			if !strings.Contains(step.Program, "recovered") {
				t.Fatalf("the wrong step was marked environment-authored: %q", step.Program)
			}
		}
	}
	if !sawEnvironment {
		t.Fatal("the runtime's own program was attributed to the model")
	}
}

// A trace produced through an injected model client records what ran but not
// what the model was asked or emitted. That export is usable for inspection and
// unusable for supervised training, and it has to say so rather than look
// complete.
func TestTrajectoryReportsATraceThatRecordedNoCompletions(t *testing.T) {
	episode := traceEpisode(false, `await execute_graphql({query: "query { accounts { count_id } }"});`)
	trace := episode.Response.(map[string]any)["trace"].(map[string]any)
	for _, raw := range trace["chat_log"].([]any) {
		entry := raw.(map[string]any)
		entry["item1"] = map[string]any{}
	}
	trajectory, err := BuildTrajectory(episode, TrajectoryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if trajectory.PromptsRecorded {
		t.Fatal("a trace with no rendered prompts must not claim to have them")
	}
	if trajectory.AuthorshipResolved {
		t.Fatal("a trace with no completions cannot resolve authorship")
	}
	for _, step := range trajectory.Steps {
		if step.Author != AuthorUnknown {
			t.Fatalf("authorship must be unknown, got %q", step.Author)
		}
	}
}

// The three stages are three different policies sharing one trace. Mixing them
// into one training set teaches none of them.
func TestTrajectoryCanRestrictToOneStage(t *testing.T) {
	episode := traceEpisode(true, `await query_catalog({search: "accounts"});`, `await final({status:"answered"});`)
	trace := episode.Response.(map[string]any)["trace"].(map[string]any)
	// The second program was produced by a different stage.
	events := trace["events"].([]any)
	for _, raw := range events[3:] {
		event := raw.(map[string]any)
		if event["component_id"] == "agent.stage.executor" {
			event["component_id"] = "agent.stage.responder"
		}
	}

	executor, err := BuildTrajectory(episode, TrajectoryOptions{Stage: "executor"})
	if err != nil {
		t.Fatal(err)
	}
	if len(executor.Steps) != 1 || executor.Steps[0].Stage != "executor" {
		t.Fatalf("expected only executor steps, got %+v", executor.Steps)
	}
	all, err := BuildTrajectory(episode, TrajectoryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Steps) != 2 {
		t.Fatalf("expected both stages without a filter, got %d", len(all.Steps))
	}
}

func TestTrajectoryUsesTheRequestedRewardProfile(t *testing.T) {
	episode := traceEpisode(true, `await final({status:"answered"});`)
	benchmark, err := BuildTrajectory(episode, TrajectoryOptions{Profile: RewardProfileBenchmark})
	if err != nil {
		t.Fatal(err)
	}
	if benchmark.Reward != episode.Score.Vector.Reward {
		t.Fatalf("the benchmark profile must carry the recorded reward, got %v", benchmark.Reward)
	}
	rl, err := BuildTrajectory(episode, TrajectoryOptions{Profile: RewardProfileRL})
	if err != nil {
		t.Fatal(err)
	}
	if rl.RewardProfile != string(RewardProfileRL) {
		t.Fatalf("profile not recorded: %q", rl.RewardProfile)
	}
	if rl.Reward == benchmark.Reward {
		t.Fatal("the training profile must reprice the episode rather than copy the board's number")
	}
	if _, err := BuildTrajectory(episode, TrajectoryOptions{Profile: "greedy"}); err == nil {
		t.Fatal("an unknown profile must be rejected")
	}
}

func TestTrajectoriesWriteOnePerLine(t *testing.T) {
	episode := traceEpisode(true, `await final({status:"answered"});`)
	one, err := BuildTrajectory(episode, TrajectoryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var buffer bytes.Buffer
	if err := WriteTrajectoriesJSONL(&buffer, []Trajectory{one, one}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(buffer.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected two lines, got %d", len(lines))
	}
	for _, line := range lines {
		var decoded Trajectory
		if err := json.Unmarshal([]byte(line), &decoded); err != nil {
			t.Fatalf("line is not valid JSON: %v", err)
		}
		if decoded.SchemaVersion != TrajectorySchemaVersion {
			t.Fatalf("missing schema version: %q", decoded.SchemaVersion)
		}
	}
}
