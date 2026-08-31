package agent

import (
	"context"
	"testing"
	"time"

	ax "github.com/ax-llm/ax/packages/go"
)

// samplingClient must forward the capability report, or ax falls back to its
// permissive structured-output default. This is a compile-time pin as much as
// a behavioural one.
func TestSamplingClientForwardsFeatures(t *testing.T) {
	inner := &featureClient{features: map[string]ax.Value{"structuredOutputs": true}}
	client := &samplingClient{inner: inner}
	var _ interface {
		GetFeatures(string) map[string]ax.Value
	} = client

	features := client.GetFeatures("some-model")
	if features == nil || features["structuredOutputs"] != true {
		t.Fatalf("the provider's features did not survive the wrapper: %+v", features)
	}
	if inner.asked != "some-model" {
		t.Fatalf("the model name did not reach the provider: %q", inner.asked)
	}
	// A provider that reports nothing must not become a panic.
	if (&samplingClient{inner: &recordingClient{}}).GetFeatures("m") != nil {
		t.Fatal("a featureless provider must report nothing, not something invented")
	}
}

func TestValidateSampling(t *testing.T) {
	value := func(v float64) *float64 { return &v }
	cases := map[string]struct {
		temperature, topP *float64
		ok                bool
	}{
		"both unset":     {nil, nil, true},
		"explicit zero":  {value(0), nil, true},
		"upper bound":    {value(2), nil, true},
		"over the top":   {value(2.1), nil, false},
		"negative":       {value(-0.1), nil, false},
		"top_p mid":      {nil, value(0.5), true},
		"top_p one":      {nil, value(1), true},
		"top_p zero":     {nil, value(0), false},
		"top_p over one": {nil, value(1.1), false},
		"both together":  {value(0.8), value(0.95), true},
	}
	for name, item := range cases {
		err := ValidateSampling(item.temperature, item.topP)
		if item.ok && err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !item.ok && err == nil {
			t.Fatalf("%s: expected a refusal", name)
		}
	}
}

// The load-bearing test: a configured temperature must reach EVERY model call
// ax makes during a run.
//
// This is why the setting is injected at the client boundary. One call in a run
// — the query the runtime issues from inside a program — is forwarded with an
// empty option map, so an implementation that passed temperature as a forward
// option would set it on most requests and silently miss that one. Asserting
// "every captured request" rather than "the first" is what makes the
// difference visible.
func TestConfiguredTemperatureReachesEveryRequest(t *testing.T) {
	temperature, topP := 0.7, 0.9
	rec := &recordingClient{}
	runner := newAgent(
		Config{
			Provider: "openai", APIKeyEnv: "GRAPHJIN_UNUSED", TimeoutSeconds: 50, MaxSteps: 4,
			Temperature: &temperature, TopP: &topP,
		},
		&fakeRuntime{},
		// The factory is what DefaultClientFactory would have wrapped; wrap it
		// the same way so the test exercises the real composition.
		WithClientFactory(func(cfg Config) (ax.AIClient, error) {
			var client ax.AIClient = rec
			if cfg.Temperature != nil || cfg.TopP != nil {
				client = &samplingClient{inner: client, temperature: cfg.Temperature, topP: cfg.TopP}
			}
			return client, nil
		}),
		WithNow(func() time.Time { return time.Date(2031, 5, 17, 9, 0, 0, 0, time.UTC) }),
	)
	_, _ = runner.Run(context.Background(), Request{
		Instruction: "count the accounts", Capabilities: profileWithRoleAndRoots("user"),
	})
	if len(rec.calls) == 0 {
		t.Fatal("no Chat calls captured — ax never reached the client")
	}
	for index, call := range rec.calls {
		config, _ := call.values["model_config"].(map[string]ax.Value)
		if config == nil {
			t.Fatalf("call %d carried no model config, so it was sampled at the stack default", index)
		}
		if config["temperature"] != temperature {
			t.Fatalf("call %d temperature = %v, want %v", index, config["temperature"], temperature)
		}
		// Both spellings, because providers disagree on which they read.
		if config["top_p"] != topP || config["topP"] != topP {
			t.Fatalf("call %d top_p = %v/%v, want %v", index, config["top_p"], config["topP"], topP)
		}
	}
	t.Logf("temperature reached %d model call(s)", len(rec.calls))
}

// featureClient reports capabilities and records what it was asked about.
type featureClient struct {
	features map[string]ax.Value
	asked    string
}

func (c *featureClient) Chat(context.Context, map[string]ax.Value, map[string]ax.Value) (ax.Value, error) {
	return nil, nil
}

func (c *featureClient) Embed(context.Context, map[string]ax.Value, map[string]ax.Value) (ax.Value, error) {
	return nil, nil
}

func (c *featureClient) Stream(context.Context, map[string]ax.Value, map[string]ax.Value) ([]ax.Value, error) {
	return nil, nil
}

func (c *featureClient) GetFeatures(model string) map[string]ax.Value {
	c.asked = model
	return c.features
}
