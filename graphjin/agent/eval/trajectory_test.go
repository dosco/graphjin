package eval

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// chatEntry builds one chat_log entry in the shape the agent really records.
//
// This is captured from a live trace, not invented. An earlier version of this
// fixture used a tuple shape that no current trace produces, which is exactly
// why the exporter could read a key nothing wrote and still pass its tests.
func chatEntry(name string, prompt bool, program string) map[string]any {
	entry := map[string]any{"name": name, "stage": "task"}
	if name == "distiller" {
		entry["stage"] = "ctx"
	}
	if prompt {
		entry["messages"] = []any{
			map[string]any{"role": "system", "content": "You (`" + name + "`) do the work."},
			map[string]any{"role": "user", "content": "How many accounts are there?"},
		}
	}
	completion, _ := json.Marshal(map[string]string{"javascriptCode": program})
	entry["response"] = map[string]any{
		"results":     []any{map[string]any{"content": string(completion)}},
		"model_usage": map[string]any{"tokens": map[string]any{"prompt": 10, "completion": 5}},
	}
	return entry
}

// traceEpisode builds an episode carrying a trace of the shape the agent really
// records: chat_log entries holding the rendered prompt and the completion, and
// ordered events holding each executed program with its result.
func traceEpisode(prompts bool, programs ...string) Episode {
	chatLog := make([]any, 0, len(programs))
	events := make([]any, 0, len(programs))
	for _, program := range programs {
		chatLog = append(chatLog, chatEntry("executor", prompts, program))
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
		entry["response"] = map[string]any{}
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

// The merged chat log is grouped by stage, not ordered by time: every
// distiller entry precedes every executor entry however they interleaved in
// reality. A single counter over one flattened list therefore hands an
// executor step the distiller's prompt as soon as the distiller has spoken —
// and it speaks whenever a tool result is large.
//
// This is the case a key rename alone would not have fixed.
func TestTrajectoryAssociatesPromptsWithTheirOwnStage(t *testing.T) {
	const first = `await execute_graphql({query: "query { accounts { count_id } }"});`
	const second = `await final({status: "answered", answer: "8"});`

	episode := traceEpisode(true, first, second)
	trace := episode.Response.(map[string]any)["trace"].(map[string]any)
	// Two distiller calls land ahead of the executor entries, as the merge
	// produces them.
	trace["chat_log"] = append([]any{
		chatEntry("distiller", true, "condense one"),
		chatEntry("distiller", true, "condense two"),
	}, trace["chat_log"].([]any)...)
	// The events keep real time, and that is the whole point: the distiller ran
	// BETWEEN the two executor programs. Grouping put its entries at the front
	// of the chat log, so a single counter over one flattened list reaches for
	// the distiller's prompt at exactly the moment the first executor program
	// runs.
	events := trace["events"].([]any)
	stageEvent := func(kind, stage string) map[string]any {
		return map[string]any{"kind": kind, "component_id": "agent.stage." + stage}
	}
	trace["events"] = append(append(append([]any{},
		events[0:3]...),
		stageEvent("stage_request", "distiller"), stageEvent("stage_response", "distiller"),
		stageEvent("stage_request", "distiller"), stageEvent("stage_response", "distiller")),
		events[3:]...)

	trajectory, err := BuildTrajectory(episode, TrajectoryOptions{Stage: "executor"})
	if err != nil {
		t.Fatal(err)
	}
	if len(trajectory.Steps) != 2 {
		t.Fatalf("expected both executor steps, got %d", len(trajectory.Steps))
	}
	for index, step := range trajectory.Steps {
		if len(step.Prompt) == 0 {
			t.Fatalf("step %d carries no prompt", index)
		}
		// The distiller's prompts must never reach an executor step. Under the
		// old global counter both steps got them, because four distiller
		// entries were sitting at the front of one flattened list.
		for _, message := range step.Prompt {
			if strings.Contains(message.Content, "`distiller`") {
				t.Fatalf("step %d was given the distiller's prompt: %q", index, message.Content)
			}
		}
	}
	if len(trajectory.TraceNotes) != 0 {
		t.Fatalf("a complete trace must raise no doubts: %v", trajectory.TraceNotes)
	}
}

// When a stage made more calls than the chat log recorded prompts for, some
// step is carrying a prompt that did not produce it. That doubt travels with
// the corpus rather than being dropped.
func TestTrajectoryReportsWhenTheTraceDisagreesWithItself(t *testing.T) {
	episode := traceEpisode(true, "one", "two")
	trace := episode.Response.(map[string]any)["trace"].(map[string]any)
	// Two executor calls in the event stream, one recorded prompt.
	trace["chat_log"] = trace["chat_log"].([]any)[:1]

	trajectory, err := BuildTrajectory(episode, TrajectoryOptions{Stage: "executor"})
	if err != nil {
		t.Fatal(err)
	}
	if len(trajectory.TraceNotes) == 0 {
		t.Fatal("a trace missing prompts must say so")
	}
	if !strings.Contains(trajectory.TraceNotes[0], "executor") {
		t.Fatalf("the note must name the stage: %v", trajectory.TraceNotes)
	}
}
