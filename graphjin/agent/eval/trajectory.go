package eval

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const TrajectorySchemaVersion = "graphjin.eval.trajectory/v1"

// A trajectory is one episode rewritten as the sequence a policy could be
// trained on.
//
// The agent's action space is a JavaScript program, not a tool call, so this is
// not the messages-with-tool-calls shape and cannot be losslessly converted to
// it. Each step is a program the policy emitted and what running it returned.
type TrajectoryMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type TrajectoryStep struct {
	Index int    `json:"index"`
	Stage string `json:"stage,omitempty"`
	// Author says whether the policy wrote this program or GraphJin did.
	//
	// The runtime writes and executes programs of its own — repairs, forced
	// continuations, protocol handoffs — and they appear in the trace exactly
	// like the policy's. Training on them unmarked teaches a model to imitate
	// the environment's corrections rather than to avoid needing them.
	Author      string              `json:"author"`
	Prompt      []TrajectoryMessage `json:"prompt,omitempty"`
	Program     string              `json:"program"`
	Observation string              `json:"observation,omitempty"`
	Guidance    string              `json:"guidance,omitempty"`
	IsError     bool                `json:"is_error,omitempty"`
}

const (
	AuthorModel       = "model"
	AuthorEnvironment = "environment"
	AuthorUnknown     = "unknown"
)

type Trajectory struct {
	SchemaVersion string `json:"schema_version"`
	RunID         string `json:"run_id"`
	TaskID        string `json:"task_id"`
	TaskSlug      string `json:"task_slug,omitempty"`
	Repeat        int    `json:"repeat"`
	Instruction   string `json:"instruction"`
	Category      string `json:"category,omitempty"`
	Difficulty    string `json:"difficulty,omitempty"`

	Steps []TrajectoryStep `json:"steps"`

	Status string  `json:"status,omitempty"`
	Answer string  `json:"answer,omitempty"`
	Pass   bool    `json:"pass"`
	Reward float64 `json:"reward"`

	RewardVersion string `json:"reward_version"`
	RewardProfile string `json:"reward_profile"`
	// PromptsRecorded is false when the trace carried no rendered prompts.
	//
	// A trace produced through an injected model client records the programs but
	// not what the model was asked, so the export is usable for inspection and
	// for reward work and not for supervised fine-tuning. Saying so here is the
	// difference between a corpus someone can trust and one whose gaps are
	// discovered after a training run.
	PromptsRecorded bool `json:"prompts_recorded"`
	// AuthorshipResolved is false when the trace did not record what the model
	// emitted, so the environment's own programs cannot be told apart.
	AuthorshipResolved bool               `json:"authorship_resolved"`
	Provenance         RunProvenance      `json:"provenance"`
	Dataset            DatasetFingerprint `json:"dataset"`
}

// TrajectoryOptions selects what an export contains.
type TrajectoryOptions struct {
	// Stage restricts steps to one stage. Empty keeps every stage.
	//
	// One episode is produced by three different policies sharing a trace —
	// distiller, executor and responder — with different prompts and different
	// jobs. Mixing them into one training set teaches none of them.
	Stage string
	// IncludeEnvironmentSteps keeps the programs GraphJin wrote itself.
	IncludeEnvironmentSteps bool
	Profile                 RewardProfile
}

// BuildTrajectory rewrites a stored episode as a trainable sequence.
func BuildTrajectory(episode Episode, opts TrajectoryOptions) (Trajectory, error) {
	profile, err := opts.Profile.normalize()
	if err != nil {
		return Trajectory{}, err
	}
	response := toMap(episode.Response)
	trace := toMap(response["trace"])
	prompts, completions := tracePromptsAndCompletions(trace)
	authored := map[string]bool{}
	for _, program := range completions {
		if program != "" {
			authored[normalizeProgram(program)] = true
		}
	}
	trajectory := Trajectory{
		SchemaVersion: TrajectorySchemaVersion,
		RunID:         episode.RunID, TaskID: episode.TaskID, TaskSlug: episode.TaskSlug,
		Repeat:      episode.Repeat,
		Instruction: episode.Request.Instruction,
		Category:    string(episode.Task.Category), Difficulty: string(episode.Task.Difficulty),
		Status: valueString(response["status"]), Answer: valueString(response["answer"]),
		Pass: episode.Score.Pass, Reward: episode.Score.Vector.Reward,
		RewardVersion: RewardVersion, RewardProfile: string(profile),
		PromptsRecorded:    len(prompts) != 0,
		AuthorshipResolved: len(authored) != 0,
		Provenance:         episode.Provenance, Dataset: episode.Dataset,
	}
	if profile == RewardProfileRL {
		trajectory.Reward = rewardForProfile(profile, episode.Score.Vector)
	}

	index := 0
	stage := ""
	calls := 0
	for _, raw := range toSlice(normalizeJSON(trace["events"])) {
		event := toMap(raw)
		kind := valueString(event["kind"])
		// The stage is read from the event stream rather than by counting model
		// calls: a stage can run several programs, and one program can follow
		// several calls, so any positional mapping between the two drifts.
		if kind == "stage_request" || kind == "stage_response" {
			if named := stageFromComponent(valueString(event["component_id"])); named != "" {
				stage = named
			}
			if kind == "stage_response" {
				calls++
			}
			continue
		}
		if kind != "runtime_execute" {
			continue
		}
		payload := toMap(event["payload"])
		program := valueString(payload["code"])
		if strings.TrimSpace(program) == "" {
			continue
		}
		author := AuthorUnknown
		if len(authored) != 0 {
			author = AuthorEnvironment
			if authored[normalizeProgram(program)] {
				author = AuthorModel
			}
		}
		if author == AuthorEnvironment && !opts.IncludeEnvironmentSteps {
			continue
		}
		if opts.Stage != "" && !strings.EqualFold(stage, opts.Stage) {
			continue
		}
		step := TrajectoryStep{
			Index: index, Stage: stage, Author: author, Program: program,
			Observation: compactJSON(payload["result"], 8192),
			Guidance:    compactJSON(payload["guidance_payload"], 2048),
			IsError:     boolValue(payload["is_error"]),
		}
		// The prompt that produced a program is the one from the model call just
		// before it ran.
		if promptIndex := calls - 1; promptIndex >= 0 && promptIndex < len(prompts) {
			step.Prompt = prompts[promptIndex]
		}
		trajectory.Steps = append(trajectory.Steps, step)
		index++
	}
	return trajectory, nil
}

// tracePromptsAndCompletions reads the rendered prompt and the raw completion
// for each model call, in order.
func tracePromptsAndCompletions(trace map[string]any) ([][]TrajectoryMessage, []string) {
	entries := toSlice(normalizeJSON(trace["chat_log"]))
	var prompts [][]TrajectoryMessage
	completions := make([]string, 0, len(entries))
	for _, raw := range entries {
		entry := toMap(raw)
		messages := renderedPrompt(toMap(entry["item0"]))
		if len(messages) != 0 {
			prompts = append(prompts, messages)
		}
		completions = append(completions, programFromCompletion(valueString(toMap(entry["item1"])["content"])))
	}
	return prompts, completions
}

func renderedPrompt(item map[string]any) []TrajectoryMessage {
	var out []TrajectoryMessage
	for _, raw := range toSlice(normalizeJSON(item["chat_prompt"])) {
		message := toMap(raw)
		content := strings.TrimSpace(valueString(message["content"]))
		if content == "" {
			continue
		}
		out = append(out, TrajectoryMessage{Role: valueString(message["role"]), Content: content})
	}
	return out
}

// stageFromComponent reads the stage out of an event's component id, which the
// trace spells "agent.stage.executor".
func stageFromComponent(component string) string {
	const prefix = "agent.stage."
	if strings.HasPrefix(component, prefix) {
		return strings.TrimPrefix(component, prefix)
	}
	return ""
}

// normalizeProgram makes two spellings of the same program compare equal, so a
// whitespace difference between what the model emitted and what the runtime
// executed does not read as the environment having authored it.
func normalizeProgram(program string) string {
	return strings.Join(strings.Fields(program), " ")
}

func boolValue(raw any) bool {
	value, _ := raw.(bool)
	return value
}

// WriteTrajectoriesJSONL writes one trajectory per line.
func WriteTrajectoriesJSONL(w io.Writer, trajectories []Trajectory) error {
	encoder := json.NewEncoder(w)
	for i := range trajectories {
		if err := encoder.Encode(trajectories[i]); err != nil {
			return fmt.Errorf("encode trajectory %d: %w", i, err)
		}
	}
	return nil
}

// ExportRunTrajectories reads a completed run's episodes and rewrites them.
func ExportRunTrajectories(store *Store, runID string, opts TrajectoryOptions) ([]Trajectory, error) {
	if store == nil {
		return nil, fmt.Errorf("export needs an evaluation store")
	}
	episodes, err := store.LoadEpisodes(runID)
	if err != nil {
		return nil, err
	}
	out := make([]Trajectory, 0, len(episodes))
	for i := range episodes {
		trajectory, err := BuildTrajectory(episodes[i], opts)
		if err != nil {
			return nil, err
		}
		out = append(out, trajectory)
	}
	return out, nil
}
