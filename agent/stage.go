package agent

import (
	"strings"

	ax "github.com/ax-llm/ax/packages/go"
)

// Telling one pipeline stage from another.
//
// An agent run is not one model call. A distiller decides what is worth keeping
// from a large tool result, an executor writes the code that does the work, and
// a responder turns what was found into an answer. They have different jobs and
// are not equally worth a large model's time — which matters when the point is
// to train a small model to be the executor while a fixed model does the rest,
// or to hand a trainer only the calls it is teaching.
//
// Nothing in the request says which stage it is: ax sends a model name, a
// prompt, the functions and a response format, and nothing else. What does say
// it is how each stage's prompt addresses the model. Those openings are
// exclusive to their stage, which the live pin in stage_test.go proves against
// the prompts ax actually renders.
const (
	StageExecutor  = "executor"
	StageDistiller = "distiller"
	StageResponder = "responder"
	StageFinalize  = "finalize"
	StageUnknown   = "unknown"
)

// stageMarkers are matched in order. Each is the sentence its stage's prompt
// opens the model's role with.
//
// Section headings look like better markers and are not: the executor prompt
// contains a section called "Executor Request & Distilled Context", so matching
// "## Executor" classifies distiller prompts as executor prompts. The role
// sentence is what only one stage says.
var stageMarkers = []struct {
	marker string
	stage  string
}{
	{"You (`distiller`)", StageDistiller},
	{"You (`executor`)", StageExecutor},
	{"You synthesize the final answer", StageResponder},
	// The closing calls: the loop has run out of steps, or the draft answer
	// named something the run never observed and has to be rewritten.
	{"No more tool calls are available.", StageFinalize},
	{"The draft answer named identifiers", StageFinalize},
}

// StageOfChatRequest names which stage of an agent run a model request belongs
// to, or StageUnknown when it recognises nothing.
//
// Unknown is a real answer and not an error. A caller routing on this has to
// decide what to do with a call it cannot place, and quietly guessing on its
// behalf is how a stage silently ends up served by the wrong model.
func StageOfChatRequest(request map[string]ax.Value) string {
	prompt, ok := request["chat_prompt"].([]ax.Value)
	if !ok || len(prompt) == 0 {
		return StageUnknown
	}
	message, ok := prompt[0].(map[string]ax.Value)
	if !ok {
		return StageUnknown
	}
	content, ok := message["content"].(string)
	if !ok {
		return StageUnknown
	}
	for _, candidate := range stageMarkers {
		if strings.Contains(content, candidate.marker) {
			return candidate.stage
		}
	}
	return StageUnknown
}
