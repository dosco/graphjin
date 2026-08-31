package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	ax "github.com/ax-llm/ax/packages/go"
)

// captureRunStages drives a real run through ax's own prompt assembly and
// returns the stage every rendered request classifies as, alongside the prompt
// itself so a failure can say what it actually saw.
func captureRunStages(t *testing.T, responses []string, instruction string) ([]string, []string) {
	t.Helper()
	rec := &recordingClient{responses: responses}
	runtime := &fakeRuntime{}
	runner := newAgent(
		Config{Provider: "openai", APIKeyEnv: "GRAPHJIN_UNUSED", TimeoutSeconds: 50, MaxSteps: 4},
		runtime,
		WithClientFactory(func(Config) (ax.AIClient, error) { return rec, nil }),
		WithNow(func() time.Time { return time.Date(2031, 5, 17, 9, 0, 0, 0, time.UTC) }),
	)
	_, _ = runner.Run(context.Background(), Request{
		Instruction: instruction, Capabilities: profileWithRoleAndRoots("user"),
	})
	if len(rec.calls) == 0 {
		t.Fatal("no Chat calls captured — ax never reached the client")
	}
	stages := make([]string, 0, len(rec.calls))
	prompts := make([]string, 0, len(rec.calls))
	for _, call := range rec.calls {
		stages = append(stages, StageOfChatRequest(call.values))
		prompts = append(prompts, firstPromptContent(call.values))
	}
	return stages, prompts
}

func firstPromptContent(values map[string]ax.Value) string {
	prompt, ok := values["chat_prompt"].([]ax.Value)
	if !ok || len(prompt) == 0 {
		return ""
	}
	message, ok := prompt[0].(map[string]ax.Value)
	if !ok {
		return ""
	}
	content, _ := message["content"].(string)
	return content
}

// Every call ax makes has to be placeable. A call nobody can classify is a call
// that gets routed to whatever the default is, which is how a stage silently
// ends up served by a model nobody chose.
func TestEveryRenderedStageIsRecognised(t *testing.T) {
	stages, prompts := captureRunStages(t, []string{
		`{"javascriptCode":"await final('Inspect the catalog and answer from evidence.', {})"}`,
		`{"javascriptCode":"const audit = await query_catalog({id:'help:discovery'}); console.log(audit);"}`,
		`{"javascriptCode":"await final('Answered from the inspected catalog.', {ok:true})"}`,
	}, "create a new order for a customer and update product inventory")

	seen := map[string]bool{}
	for index, stage := range stages {
		if stage == StageUnknown {
			t.Fatalf("call %d could not be placed; prompt opens:\n%s", index, head(prompts[index]))
		}
		seen[stage] = true
	}
	if !seen[StageExecutor] {
		t.Fatalf("no executor call in %v", stages)
	}
	t.Logf("stages: %v", stages)
}

// The markers have to be exclusive, or the order they are tried in silently
// decides the answer. Matching section headings instead of role sentences fails
// exactly here: the executor prompt has a section headed "Executor Request &
// Distilled Context", so a heading match classifies distiller prompts as
// executor prompts.
func TestStageMarkersAreExclusivePerPrompt(t *testing.T) {
	_, prompts := captureRunStages(t, []string{
		`{"javascriptCode":"await final('Inspect the catalog and answer from evidence.', {})"}`,
		`{"javascriptCode":"const audit = await query_catalog({id:'help:discovery'}); console.log(audit);"}`,
		`{"javascriptCode":"await final('Answered from the inspected catalog.', {ok:true})"}`,
	}, "list the accounts and summarise them")

	for index, prompt := range prompts {
		var matched []string
		for _, candidate := range stageMarkers {
			if strings.Contains(prompt, candidate.marker) {
				matched = append(matched, candidate.stage)
			}
		}
		distinct := map[string]bool{}
		for _, stage := range matched {
			distinct[stage] = true
		}
		if len(distinct) > 1 {
			t.Fatalf("prompt %d matches more than one stage %v; prompt opens:\n%s",
				index, matched, head(prompt))
		}
	}
}

// A request that carries no prompt at all is unknown rather than a panic, and
// unknown rather than a guess.
func TestStageOfMalformedRequestIsUnknown(t *testing.T) {
	cases := map[string]map[string]ax.Value{
		"no prompt":       {},
		"empty prompt":    {"chat_prompt": []ax.Value{}},
		"wrong type":      {"chat_prompt": "a string"},
		"no content":      {"chat_prompt": []ax.Value{map[string]ax.Value{"role": "system"}}},
		"unknown opening": {"chat_prompt": []ax.Value{map[string]ax.Value{"content": "Hello there."}}},
	}
	for name, request := range cases {
		if stage := StageOfChatRequest(request); stage != StageUnknown {
			t.Fatalf("%s: classified as %q", name, stage)
		}
	}
	// And the markers themselves classify, so the table is actually wired in.
	for _, candidate := range stageMarkers {
		request := map[string]ax.Value{"chat_prompt": []ax.Value{
			map[string]ax.Value{"role": "system", "content": "preamble\n" + candidate.marker + " does the work."},
		}}
		if stage := StageOfChatRequest(request); stage != candidate.stage {
			t.Fatalf("marker %q classified as %q, want %q", candidate.marker, stage, candidate.stage)
		}
	}
}

func head(text string) string {
	if len(text) > 400 {
		return text[:400] + "…"
	}
	return text
}
