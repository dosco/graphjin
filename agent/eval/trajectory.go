package eval

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
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
	AuthorshipResolved bool `json:"authorship_resolved"`
	// TraceNotes records where the trace disagreed with itself — for instance a
	// stage whose model calls and whose recorded prompts do not tally. A corpus
	// built from a trace nobody could fully read is not obviously wrong, which is
	// why the doubt travels with it rather than being dropped.
	TraceNotes []string           `json:"trace_notes,omitempty"`
	Provenance RunProvenance      `json:"provenance"`
	Dataset    DatasetFingerprint `json:"dataset"`
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
	stageLogs := stageChatLogs(trace)
	authored := map[string]bool{}
	recordedPrompts := false
	for _, log := range stageLogs {
		for _, prompt := range log.prompts {
			if len(prompt) != 0 {
				recordedPrompts = true
			}
		}
		for _, program := range log.completions {
			if program != "" {
				authored[normalizeProgram(program)] = true
			}
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
		PromptsRecorded:    recordedPrompts,
		AuthorshipResolved: len(authored) != 0,
		Provenance:         episode.Provenance, Dataset: episode.Dataset,
	}
	if profile == RewardProfileRL {
		trajectory.Reward = rewardForProfile(profile, episode.Score.Vector)
	}

	index := 0
	stage := ""
	calls := map[string]int{}
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
				calls[stage]++
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
		// The prompt that produced a program is the one from that stage's most
		// recent model call. The count is kept per stage because the chat log is
		// grouped by stage and not ordered by time: a single counter over one
		// flattened list hands a step another stage's prompt the moment the
		// distiller speaks, which it does whenever a tool result is large.
		if prompts := stageLogs[stage].prompts; len(prompts) != 0 {
			if promptIndex := calls[stage] - 1; promptIndex >= 0 && promptIndex < len(prompts) {
				step.Prompt = prompts[promptIndex]
			}
		}
		trajectory.Steps = append(trajectory.Steps, step)
		index++
	}
	trajectory.TraceNotes = chatLogDisagreements(stageLogs, calls)
	return trajectory, nil
}

// stageLog is one stage's model calls, in the order that stage made them.
//
// Prompts and completions are kept as parallel slices with a slot per call,
// including calls whose prompt came back empty. Dropping the empty ones would
// shorten one list and silently shift every later prompt onto the wrong
// program.
type stageLog struct {
	prompts     [][]TrajectoryMessage
	completions []string
}

// stageChatLogs groups the trace's model calls by the stage that made them.
//
// The merged chat log is grouped by stage rather than ordered by time: every
// distiller entry precedes every executor entry whatever order they ran in.
// The stage is read from each entry's name, which is the only key that tells
// the three apart — the entry's own "stage" field distinguishes only context
// work from task work, so executor and responder share a value there.
func stageChatLogs(trace map[string]any) map[string]stageLog {
	out := map[string]stageLog{}
	for _, raw := range toSlice(normalizeJSON(trace["chat_log"])) {
		entry := toMap(raw)
		name := valueString(entry["name"])
		if name == "" {
			continue
		}
		log := out[name]
		log.prompts = append(log.prompts, chatEntryMessages(entry))
		log.completions = append(log.completions, programFromCompletion(chatCompletionContent(entry)))
		out[name] = log
	}
	return out
}

// chatLogDisagreements reports stages whose recorded calls and recorded
// prompts do not tally.
//
// The two come from different halves of the trace — the event stream and the
// chat log — and nothing guarantees a provider filled in both. When they
// disagree some step is carrying a prompt that did not produce it, and the
// only honest thing to do with a corpus like that is to say so on it.
func chatLogDisagreements(stageLogs map[string]stageLog, calls map[string]int) []string {
	var notes []string
	for stage, count := range calls {
		if recorded := len(stageLogs[stage].prompts); recorded < count {
			notes = append(notes, fmt.Sprintf(
				"stage %s made %d model call(s) but only %d prompt(s) were recorded; "+
					"steps beyond the recorded ones carry no prompt", stage, count, recorded))
		}
	}
	sort.Strings(notes)
	return notes
}

// chatEntryMessages reads the rendered prompt out of one chat-log entry.
func chatEntryMessages(entry map[string]any) []TrajectoryMessage {
	var out []TrajectoryMessage
	for _, raw := range toSlice(normalizeJSON(entry["messages"])) {
		message := toMap(raw)
		content := strings.TrimSpace(valueString(message["content"]))
		if content == "" {
			continue
		}
		out = append(out, TrajectoryMessage{Role: valueString(message["role"]), Content: content})
	}
	return out
}

// chatCompletionContent reads what the model actually returned for one call.
func chatCompletionContent(entry map[string]any) string {
	results := toSlice(normalizeJSON(toMap(entry["response"])["results"]))
	if len(results) == 0 {
		return ""
	}
	return valueString(toMap(results[0])["content"])
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
	return BuildTrajectories(episodes, opts)
}

// BuildTrajectories rewrites a set of episodes, so a caller that has already
// decided which episodes belong in a corpus can build from exactly those.
func BuildTrajectories(episodes []Episode, opts TrajectoryOptions) ([]Trajectory, error) {
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
