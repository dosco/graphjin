package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	ax "github.com/ax-llm/ax/packages/go"
)

// forwardedMode runs the agent with the given config and reports the
// structured_output_mode Ax received in its forward options, plus the client
// the program was handed.
func forwardedMode(t *testing.T, cfg Config) (string, ax.AIClient, ax.AIClient) {
	t.Helper()
	cfg.TimeoutSeconds = 5
	program := &fakeProgram{output: map[string]ax.Value{"status": StatusAnswered, "answer": "ok"}}
	built := fakeClient{}
	var seen ax.AIClient
	runner := newAgent(cfg, &fakeRuntime{},
		WithClientFactory(func(Config) (ax.AIClient, error) { return built, nil }),
		WithProgramFactory(func(_ string, _ map[string]ax.Value) Program {
			program.onForward = func(p *fakeProgram) { seen = p.forwardClient }
			return program
		}),
		WithNow(func() time.Time { return time.Unix(10, 0) }),
	)
	if _, err := runner.Run(context.Background(), Request{Instruction: "count customers"}); err != nil {
		t.Fatalf("run: %v", err)
	}
	mode, _ := program.forwardOptions["structured_output_mode"].(string)
	return mode, built, seen
}

// Ax owns structured-output selection from v24: the deployment profile and its
// model rules choose the mechanism. GraphJin's whole job is to carry the
// operator's choice into forward options, so that is what these tests pin.
func TestStructuredOutputModeDefaultsToAuto(t *testing.T) {
	mode, _, _ := forwardedMode(t, Config{})
	if mode != StructuredOutputAuto {
		t.Fatalf("structured_output_mode = %q, want %q", mode, StructuredOutputAuto)
	}
}

func TestStructuredOutputModeReachesForwardOptions(t *testing.T) {
	for _, want := range []string{StructuredOutputNative, StructuredOutputFunction, StructuredOutputJSONObject, StructuredOutputAuto} {
		mode, _, _ := forwardedMode(t, Config{StructuredOutputMode: want})
		if mode != want {
			t.Fatalf("structured_output_mode = %q, want %q", mode, want)
		}
	}
}

// The deprecated agent.response_format keeps working so existing configuration
// loads unchanged.
func TestLegacyResponseFormatMapsOntoModes(t *testing.T) {
	for _, tc := range []struct{ legacy, want string }{
		{ResponseFormatJSONSchema, StructuredOutputNative},
		{ResponseFormatJSONObject, StructuredOutputJSONObject},
	} {
		mode, _, _ := forwardedMode(t, Config{ResponseFormat: tc.legacy})
		if mode != tc.want {
			t.Fatalf("response_format %q produced mode %q, want %q", tc.legacy, mode, tc.want)
		}
	}
}

// An operator migrating one key at a time must never have to reason about
// which of the two settings is in effect.
func TestCanonicalModeWinsOverLegacyAlias(t *testing.T) {
	mode, _, _ := forwardedMode(t, Config{StructuredOutputMode: StructuredOutputFunction, ResponseFormat: ResponseFormatJSONSchema})
	if mode != StructuredOutputFunction {
		t.Fatalf("mode = %q, want the canonical %q to win", mode, StructuredOutputFunction)
	}
}

func TestInvalidStructuredOutputModeFailsBeforeTheModel(t *testing.T) {
	called := false
	runner := newAgent(Config{StructuredOutputMode: "strict-json", TimeoutSeconds: 5}, &fakeRuntime{},
		WithClientFactory(func(Config) (ax.AIClient, error) { called = true; return fakeClient{}, nil }),
		WithProgramFactory(func(_ string, _ map[string]ax.Value) Program {
			called = true
			return &fakeProgram{output: map[string]ax.Value{"status": StatusAnswered}}
		}),
	)
	_, err := runner.Run(context.Background(), Request{Instruction: "count customers"})
	if err == nil {
		t.Fatal("expected an invalid mode to be rejected")
	}
	if !strings.Contains(err.Error(), "structured_output_mode") {
		t.Fatalf("error should name the setting, got %v", err)
	}
	if called {
		t.Fatal("an invalid mode must fail before the client or program is built")
	}
}

// GraphJin used to decorate the Ax client to rewrite response_format and append
// a second schema prompt. Ax owns that now; the client must reach the program
// exactly as the factory returned it.
func TestAgentDoesNotWrapTheAIClient(t *testing.T) {
	_, built, seen := forwardedMode(t, Config{StructuredOutputMode: StructuredOutputJSONObject})
	if seen == nil {
		t.Fatal("program never received a client")
	}
	if seen != ax.AIClient(built) {
		t.Fatalf("client was wrapped: got %T, want the factory's %T", seen, built)
	}
}

// Profile generality: any Ax deployment profile name is valid input, so an
// unusable one must fail as configuration rather than panic the process.
// ax.NewAI panics on an unknown profile (axllm.go NewAI).
func TestUnknownProviderProfileFailsCleanly(t *testing.T) {
	t.Setenv("STRUCTURED_OUTPUT_TEST_KEY", "secret")
	client, err := DefaultClientFactory(Config{Provider: "definitely-not-a-profile", APIKeyEnv: "STRUCTURED_OUTPUT_TEST_KEY"})
	if err == nil {
		t.Fatal("expected an unknown deployment profile to be rejected")
	}
	if client != nil {
		t.Fatal("no client should be returned for an unusable profile")
	}
	if !strings.Contains(err.Error(), "definitely-not-a-profile") {
		t.Fatalf("error should name the offending provider, got %v", err)
	}
}

// A profile GraphJin has no key-env inference for must still work when the
// operator names the variable explicitly. This is the Vertex case, and it must
// hold without any provider or model branching in GraphJin.
func TestProfileWithoutKeyInferenceStillBuilds(t *testing.T) {
	t.Setenv("VERTEX_TEST_TOKEN", "secret")
	client, err := DefaultClientFactory(Config{
		Provider:  "vertex-ai",
		Model:     "google/gemma-4-26b-a4b-it-maas",
		APIKeyEnv: "VERTEX_TEST_TOKEN",
		BaseURL:   "https://example.invalid/v1beta1/projects/p/locations/l/endpoints/openapi",
	})
	if err != nil {
		t.Fatalf("vertex-ai profile should build: %v", err)
	}
	if client == nil {
		t.Fatal("expected a client")
	}
}
