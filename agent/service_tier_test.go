package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	ax "github.com/ax-llm/ax/packages/go"
	axgoja "github.com/ax-llm/ax/packages/go/runtime/goja"
)

func forwardedServiceTier(t *testing.T, cfg Config) string {
	t.Helper()
	cfg.TimeoutSeconds = 5
	program := &fakeProgram{output: map[string]ax.Value{"status": StatusAnswered, "answer": "ok"}}
	runner := newAgent(cfg, &fakeRuntime{},
		WithClientFactory(func(Config) (ax.AIClient, error) { return fakeClient{}, nil }),
		WithProgramFactory(func(string, map[string]ax.Value) Program { return program }),
		WithNow(func() time.Time { return time.Unix(10, 0) }),
	)
	if _, err := runner.Run(context.Background(), Request{Instruction: "count customers"}); err != nil {
		t.Fatalf("run: %v", err)
	}
	tier, _ := program.forwardOptions["service_tier"].(string)
	return tier
}

func TestServiceTierDefaultsToAuto(t *testing.T) {
	if got := forwardedServiceTier(t, Config{}); got != ServiceTierAuto {
		t.Fatalf("service_tier = %q, want %q", got, ServiceTierAuto)
	}
}

func TestServiceTierReachesForwardOptions(t *testing.T) {
	for _, want := range []string{ServiceTierAuto, ServiceTierStandard, ServiceTierFlex, ServiceTierPriority} {
		t.Run(want, func(t *testing.T) {
			if got := forwardedServiceTier(t, Config{ServiceTier: want}); got != want {
				t.Fatalf("service_tier = %q, want %q", got, want)
			}
		})
	}
}

func TestInvalidServiceTierFailsBeforeTheModel(t *testing.T) {
	called := false
	runner := newAgent(Config{ServiceTier: "express", TimeoutSeconds: 5}, &fakeRuntime{},
		WithClientFactory(func(Config) (ax.AIClient, error) { called = true; return fakeClient{}, nil }),
		WithProgramFactory(func(string, map[string]ax.Value) Program {
			called = true
			return &fakeProgram{}
		}),
	)
	_, err := runner.Run(context.Background(), Request{Instruction: "count customers"})
	if err == nil || !strings.Contains(err.Error(), "service_tier") {
		t.Fatalf("invalid service tier error = %v", err)
	}
	if called {
		t.Fatal("an invalid service tier must fail before the client or program is built")
	}
}

func TestServiceTierReachesForcedFinalizer(t *testing.T) {
	program := &fakeProgram{err: errActorStepsExhaustedForServiceTierTest{}}
	finalizer := &fakeProgram{output: map[string]ax.Value{"answer": "INV-1 is paid."}}
	runner := newAgent(Config{ServiceTier: ServiceTierPriority}, &successfulExecutionRuntime{},
		WithClientFactory(func(Config) (ax.AIClient, error) { return fakeClient{}, nil }),
		WithProgramFactory(func(_ string, options map[string]ax.Value) Program {
			program.options = options
			program.onForward = func(p *fakeProgram) {
				callProgramTool(t, p, "query_catalog", map[string]ax.Value{"id": "table:app:main.invoices"})
				callProgramTool(t, p, "execute_graphql", map[string]ax.Value{"query": `query { invoices { id } }`})
			}
			return program
		}),
		WithFinalizerFactory(func(string, map[string]ax.Value) Program { return finalizer }),
	)
	if _, err := runner.Run(context.Background(), Request{Instruction: "show invoices"}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := finalizer.forwardOptions["service_tier"]; got != ServiceTierPriority {
		t.Fatalf("forced finalizer service_tier = %#v, want %q", got, ServiceTierPriority)
	}
}

type serviceTierCaptureClient struct {
	chatOptions map[string]ax.Value
}

func (c *serviceTierCaptureClient) Chat(_ context.Context, _ map[string]ax.Value, options map[string]ax.Value) (ax.Value, error) {
	c.chatOptions = options
	return nil, nil
}

func (*serviceTierCaptureClient) Embed(context.Context, map[string]ax.Value, map[string]ax.Value) (ax.Value, error) {
	return nil, nil
}

func (*serviceTierCaptureClient) Stream(context.Context, map[string]ax.Value, map[string]ax.Value) ([]ax.Value, error) {
	return nil, nil
}

func TestServiceTierReachesNestedClientCalls(t *testing.T) {
	client := &serviceTierCaptureClient{}
	program := &fakeProgram{output: map[string]ax.Value{"status": StatusAnswered, "answer": "ok"}}
	program.onForward = func(p *fakeProgram) {
		// Ax's runtime llmQuery primitive invokes its nested AxGen with a fresh
		// options map. Calling the handed-through client the same way proves the
		// boundary wrapper covers that path instead of relying on top-level options.
		if _, err := p.forwardClient.Chat(context.Background(), nil, nil); err != nil {
			t.Fatalf("nested chat: %v", err)
		}
	}
	runner := newAgent(Config{ServiceTier: ServiceTierPriority, TimeoutSeconds: 5}, &fakeRuntime{},
		WithClientFactory(func(Config) (ax.AIClient, error) { return client, nil }),
		WithProgramFactory(func(string, map[string]ax.Value) Program { return program }),
	)
	if _, err := runner.Run(context.Background(), Request{Instruction: "interpret the narrowed results"}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := client.chatOptions["service_tier"]; got != ServiceTierPriority {
		t.Fatalf("nested service_tier = %#v, want %q", got, ServiceTierPriority)
	}
}

func TestServiceTierReachesAxAgentLlmQuery(t *testing.T) {
	client := &recordingClient{responses: []string{
		`{"javascriptCode":"await final('Interpret the supplied evidence.', {text:'alpha'});"}`,
		`{"javascriptCode":"const answer = await llmQuery({query:'Classify the text.', context:{text:'alpha'}}); await final('Return the classification.', {answer});"}`,
		`{"answer":"classified"}`,
		`{"answer":"classified"}`,
	}}
	program := ax.NewAgent("question:string -> answer:string", map[string]ax.Value{
		"runtime": map[string]ax.Value{"language": "JavaScript"},
	})
	_, err := program.Forward(context.Background(), wrapServiceTierAIClient(client, ServiceTierPriority),
		map[string]ax.Value{"question": "classify alpha"},
		map[string]ax.Value{"runtime": axgoja.NewRuntime()},
	)
	if err != nil {
		t.Fatalf("AxAgent llmQuery forward: %v", err)
	}
	if len(client.calls) < 4 {
		t.Fatalf("model calls = %d, want distiller, executor, llmQuery, and responder", len(client.calls))
	}
	for i, call := range client.calls {
		if got := call.options["service_tier"]; got != ServiceTierPriority {
			t.Fatalf("model call %d service_tier = %#v, want %q", i+1, got, ServiceTierPriority)
		}
	}
}

type errActorStepsExhaustedForServiceTierTest struct{}

func (errActorStepsExhaustedForServiceTierTest) Error() string {
	return "agent actor loop exceeded max steps"
}

func TestServiceTierReachesGeminiWire(t *testing.T) {
	transport := ax.NewScriptedTransport([]ax.Value{
		ax.Object("status", float64(200), "json", ax.Object(
			"candidates", ax.Array(ax.Object(
				"content", ax.Object("parts", ax.Array(ax.Object("text", `{"answer":"ok"}`))),
				"finishReason", "STOP",
			)),
		)),
	})
	client := ax.NewAI("google-gemini", map[string]ax.Value{
		"apiKey": "test-token", "api_key": "test-token",
		"model": "gemini-3.5-flash", "transport": transport,
	})
	client = wrapServiceTierAIClient(client, ServiceTierFlex)
	program := ax.NewAx("question:string -> answer:string", nil)
	if _, err := program.Forward(context.Background(), client,
		map[string]ax.Value{"question": "say ok"},
		nil,
	); err != nil {
		t.Fatalf("scripted Gemini forward: %v", err)
	}
	if len(transport.Requests) != 1 {
		t.Fatalf("scripted provider requests = %d, want 1", len(transport.Requests))
	}
	raw, err := json.Marshal(transport.Requests[0])
	if err != nil {
		t.Fatal(err)
	}
	var request map[string]any
	if err := json.Unmarshal(raw, &request); err != nil {
		t.Fatal(err)
	}
	body, _ := request["json"].(map[string]any)
	if body["service_tier"] != ServiceTierFlex {
		t.Fatalf("Gemini service_tier = %#v, want %q; request=%s", body["service_tier"], ServiceTierFlex, raw)
	}
}

func TestUnsupportedServiceTierFailsBeforeTransport(t *testing.T) {
	transport := ax.NewScriptedTransport(nil)
	client := ax.NewAI("ollama", map[string]ax.Value{
		"apiKey": "test-token", "api_key": "test-token",
		"model": "llama3", "transport": transport,
	})
	client = wrapServiceTierAIClient(client, ServiceTierPriority)
	program := ax.NewAx("question:string -> answer:string", nil)
	_, err := program.Forward(context.Background(), client,
		map[string]ax.Value{"question": "say ok"},
		nil,
	)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "service tier") {
		t.Fatalf("unsupported service tier error = %v", err)
	}
	if len(transport.Requests) != 0 {
		t.Fatalf("unsupported tier reached transport: %#v", transport.Requests)
	}
}
