package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
)

type recordedCall struct {
	values  map[string]ax.Value
	options map[string]ax.Value
}

// recordingClient implements ax.AIClient and captures every Chat request ax renders,
// so we can inspect exactly what the model receives per pipeline stage.
type recordingClient struct {
	calls []recordedCall
}

func (c *recordingClient) Chat(_ context.Context, values map[string]ax.Value, options map[string]ax.Value) (ax.Value, error) {
	c.calls = append(c.calls, recordedCall{values: values, options: options})
	if len(c.calls) > 6 { // safety cap: don't loop forever
		return nil, fmt.Errorf("capture cap reached")
	}
	// Return a minimal valid result so ax advances to the next pipeline stage: ax reads
	// results[].content (axllm.go:1096) and parses it against the requested response_format.
	// Every JS-runtime stage requests a { javascriptCode } output; final() ends the stage.
	return map[string]ax.Value{
		"results": []ax.Value{
			map[string]ax.Value{"content": `{"javascriptCode":"await final('done', {})"}`},
		},
	}, nil
}

func (c *recordingClient) Embed(context.Context, map[string]ax.Value, map[string]ax.Value) (ax.Value, error) {
	return nil, nil
}

func (c *recordingClient) Stream(context.Context, map[string]ax.Value, map[string]ax.Value) ([]ax.Value, error) {
	return nil, nil
}

// TestCaptureRenderedPromptPerStage is a DIAGNOSTIC (not an assertion). It dumps what the
// real ax pipeline sends to the LLM so we can see whether defaultAgentMessage and
// runtimeUsageInstructions land in the same stage or different stages, and whether tool
// descriptions reach the model. Set PROMPT_CAPTURE_DIR to also write full per-call dumps.
//
//	go test ./agent -run TestCaptureRenderedPromptPerStage -v
func TestCaptureRenderedPromptPerStage(t *testing.T) {
	rec := &recordingClient{}
	runner := newAgent(
		Config{Provider: "openai", APIKeyEnv: "GRAPHJIN_UNUSED", TimeoutSeconds: 50, MaxSteps: 4},
		&fakeRuntime{},
		WithClientFactory(func(Config) (ax.AIClient, error) { return rec, nil }),
		// Intentionally NO WithProgramFactory: we want ax's real prompt assembly.
	)

	// Ignore Run's outcome; we only need the captured Chat requests. A normal
	// writable profile preloads data_write alongside the other core guides.
	_, _ = runner.Run(context.Background(), Request{Instruction: "create a new order for a customer and update product inventory"})

	if len(rec.calls) == 0 {
		t.Fatal("no Chat calls captured — ax did not reach the client (Seed may have failed)")
	}

	markers := []struct{ name, marker string }{
		{"base:evidence-loop", "evidence loop"},
		{"base:breadth", "Favor BREADTH"},
		{"base:count0", "count: 0"},
		{"base:next.args.id", "next.args.id"},
		{"runtimeUsage", "goja runtime profile"},
		{"SKILL:data_write", "Skill: data_write"},
		{"responder:markdown", "markdown table"},
		{"toolDesc:saved_query", "pre-approved saved query"},
		{"toolDesc:execute_graphql", "Execute raw GraphJin GraphQL"},
		{"primitive:final", "final("},
		{"primitive:askClarification", "askClarification"},
	}

	outDir := os.Getenv("PROMPT_CAPTURE_DIR")
	t.Logf("captured %d Chat call(s)", len(rec.calls))
	var runtimeReached, skillReached, markdownReached bool
	for i, call := range rec.calls {
		blob := "===VALUES===\n" + dumpAXValue(call.values) + "\n===OPTIONS===\n" + dumpAXValue(call.options)
		var present []string
		for _, m := range markers {
			if strings.Contains(blob, m.marker) {
				present = append(present, m.name)
			}
		}
		if strings.Contains(blob, "goja runtime profile") {
			runtimeReached = true
		}
		if strings.Contains(blob, "Skill: data_write") {
			skillReached = true
		}
		if strings.Contains(blob, "markdown table") { // responder answer-field formatting guidance
			markdownReached = true
		}
		t.Logf("call #%d: len=%d markers=%v", i, len(blob), present)
		if outDir != "" {
			path := filepath.Join(outDir, fmt.Sprintf("prompt_call_%d.txt", i))
			if err := os.WriteFile(path, []byte(blob), 0o644); err == nil {
				t.Logf("  full dump -> %s", path)
			}
		}
	}

	// Regression guards. Universal GraphJin guidance arrives through
	// runtime.usageInstructions; capability-filtered guides arrive through Ax's
	// constructor skills. Both channels must reach the rendered prompts.
	if !runtimeReached {
		t.Error("runtime usage instructions reached no stage — the live prompt channel is broken")
	}
	if !skillReached {
		t.Error("the data_write skill fragment reached no stage — skills are not wired to the live channel")
	}
	if !markdownReached {
		t.Error("markdown answer-formatting guidance reached no stage — the responder answer-field guidance regressed")
	}
}

func TestCaptureRenderedSkillsPerCapabilityProfile(t *testing.T) {
	allRoots := []string{
		systemRootSecurity,
		systemRootRuntime,
		systemRootConfig,
		systemRootWorkflow,
		systemRootWorkflowExec,
		systemRootWatch,
		systemRootWatchEvent,
	}
	for _, tc := range []struct {
		name     string
		readOnly bool
		profile  *CapabilityProfile
	}{
		{name: "anonymous"},
		{name: "user", profile: profileWithRoleAndRoots("user")},
		{name: "read-only", readOnly: true, profile: profileWithRoleAndRoots("user")},
		{name: "watch-enabled", profile: profileWithRoleAndRoots("user", systemRootWatch, systemRootWatchEvent)},
		{name: "workflow-enabled", profile: profileWithRoleAndRoots("user", systemRootWorkflow, systemRootWorkflowExec)},
		{name: "admin", profile: profileWithRoleAndRoots("admin", allRoots...)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := &recordingClient{}
			runner := newAgent(
				Config{Provider: "openai", APIKeyEnv: "GRAPHJIN_UNUSED", ReadOnly: tc.readOnly, TimeoutSeconds: 50, MaxSteps: 4},
				&fakeRuntime{},
				WithClientFactory(func(Config) (ax.AIClient, error) { return rec, nil }),
			)
			_, _ = runner.Run(context.Background(), Request{
				Instruction:  "inspect the available GraphJin capabilities",
				Capabilities: tc.profile,
			})
			if len(rec.calls) == 0 {
				t.Fatal("no rendered Ax prompt captured")
			}
			var prompt strings.Builder
			for _, call := range rec.calls {
				prompt.WriteString(dumpAXValue(call.values))
				prompt.WriteString(dumpAXValue(call.options))
			}
			rendered := prompt.String()
			allowed := skillIDs(allowedSkills(tc.readOnly, tc.profile))
			for _, definition := range builtinSkills {
				marker := "Skill: " + definition.id
				if containsString(allowed, definition.id) && !strings.Contains(rendered, marker) {
					t.Errorf("allowed skill %q did not reach an Ax prompt", definition.id)
				}
				if !containsString(allowed, definition.id) && strings.Contains(rendered, marker) {
					t.Errorf("forbidden skill %q leaked into an Ax prompt", definition.id)
				}
			}
		})
	}
}

func dumpAXValue(v any) string {
	data, err := json.MarshalIndent(normalizeValue(v), "", "  ")
	if err != nil {
		return fmt.Sprintf("%#v", v)
	}
	return string(data)
}
