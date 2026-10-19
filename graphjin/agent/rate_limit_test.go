package agent

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ax "github.com/ax-llm/ax/packages/go"
)

func TestRateLimitConfigValidation(t *testing.T) {
	for _, config := range []RateLimitConfig{
		{RequestsPerMinute: -1},
		{TokensPerMinute: -1},
	} {
		if err := config.Validate(); err == nil {
			t.Fatalf("expected negative limit to fail: %+v", config)
		}
	}
	if err := (RateLimitConfig{}).Validate(); err != nil {
		t.Fatalf("zero limits should be disabled, got %v", err)
	}
}

func TestProviderRateLimiterEnforcesRollingRequestsPerMinute(t *testing.T) {
	limiter := newTestProviderRateLimiter(t, RateLimitConfig{RequestsPerMinute: 1})
	now := time.Unix(100, 0)
	limiter.now = func() time.Time { return now }
	var waits []time.Duration
	limiter.wait = func(_ context.Context, delay time.Duration, _ <-chan struct{}) error {
		waits = append(waits, delay)
		now = now.Add(delay)
		return nil
	}

	called := 0
	for range 2 {
		if _, err := limiter.Hook(context.Background()).Run(func() (ax.Value, error) {
			called++
			return map[string]ax.Value{}, nil
		}, ax.AxRateLimitInfo{Operation: "chat"}); err != nil {
			t.Fatalf("limited call: %v", err)
		}
	}
	if called != 2 || len(waits) != 1 || waits[0] != time.Minute {
		t.Fatalf("calls=%d waits=%v, want two calls with one minute wait", called, waits)
	}
}

func TestProviderRateLimiterChargesCompletedTokenUsage(t *testing.T) {
	limiter := newTestProviderRateLimiter(t, RateLimitConfig{TokensPerMinute: 10})
	now := time.Unix(200, 0)
	limiter.now = func() time.Time { return now }
	var waits []time.Duration
	limiter.wait = func(_ context.Context, delay time.Duration, _ <-chan struct{}) error {
		waits = append(waits, delay)
		now = now.Add(delay)
		return nil
	}

	first := limiter.Hook(context.Background())
	if _, err := first.Run(func() (ax.Value, error) {
		return map[string]ax.Value{"model_usage": map[string]ax.Value{"tokens": map[string]ax.Value{"total_tokens": 10}}}, nil
	}, ax.AxRateLimitInfo{Operation: "chat"}); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if _, err := limiter.Hook(context.Background()).Run(func() (ax.Value, error) {
		return map[string]ax.Value{}, nil
	}, ax.AxRateLimitInfo{Operation: "chat"}); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if len(waits) != 1 || waits[0] != time.Minute {
		t.Fatalf("waits=%v, want a one-minute token wait", waits)
	}
}

func TestProviderRateLimiterDoesNotGuessMissingTokenUsage(t *testing.T) {
	limiter := newTestProviderRateLimiter(t, RateLimitConfig{TokensPerMinute: 1})
	for range 2 {
		if _, err := limiter.Hook(context.Background()).Run(func() (ax.Value, error) {
			return map[string]ax.Value{"results": []ax.Value{}}, nil
		}, ax.AxRateLimitInfo{Operation: "chat"}); err != nil {
			t.Fatalf("call without usage: %v", err)
		}
	}
}

func TestProviderRateLimiterCountsFailedProviderAttempt(t *testing.T) {
	limiter := newTestProviderRateLimiter(t, RateLimitConfig{RequestsPerMinute: 1})
	providerErr := errors.New("provider unavailable")
	if _, err := limiter.Hook(context.Background()).Run(func() (ax.Value, error) {
		return nil, providerErr
	}, ax.AxRateLimitInfo{Operation: "chat"}); !errors.Is(err, providerErr) {
		t.Fatalf("first call error=%v, want provider error", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	_, err := limiter.Hook(ctx).Run(func() (ax.Value, error) {
		called = true
		return nil, nil
	}, ax.AxRateLimitInfo{Operation: "chat"})
	if !errors.Is(err, context.Canceled) || called {
		t.Fatalf("failed attempt did not consume RPM: err=%v called=%v", err, called)
	}
}

func TestProviderRateLimiterChargesUsageReturnedWithError(t *testing.T) {
	limiter := newTestProviderRateLimiter(t, RateLimitConfig{TokensPerMinute: 10})
	now := time.Unix(250, 0)
	limiter.now = func() time.Time { return now }
	var waits int
	limiter.wait = func(_ context.Context, delay time.Duration, _ <-chan struct{}) error {
		waits++
		now = now.Add(delay)
		return nil
	}
	providerErr := errors.New("provider rejected completion")
	_, err := limiter.Hook(context.Background()).Run(func() (ax.Value, error) {
		return map[string]ax.Value{"model_usage": map[string]ax.Value{"tokens": map[string]ax.Value{"input_tokens": 6, "output_tokens": 4}}}, providerErr
	}, ax.AxRateLimitInfo{Operation: "chat"})
	if !errors.Is(err, providerErr) {
		t.Fatalf("first call error=%v, want provider error", err)
	}
	if _, err := limiter.Hook(context.Background()).Run(func() (ax.Value, error) { return nil, nil }, ax.AxRateLimitInfo{}); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if waits != 1 {
		t.Fatalf("token usage returned with an error caused %d waits, want 1", waits)
	}
}

func TestProviderRateLimiterWaitHonorsCancellation(t *testing.T) {
	limiter := newTestProviderRateLimiter(t, RateLimitConfig{RequestsPerMinute: 1})
	if _, err := limiter.Hook(context.Background()).Run(func() (ax.Value, error) { return nil, nil }, ax.AxRateLimitInfo{}); err != nil {
		t.Fatalf("first call: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	_, err := limiter.Hook(ctx).Run(func() (ax.Value, error) {
		called = true
		return nil, nil
	}, ax.AxRateLimitInfo{})
	if !errors.Is(err, context.Canceled) || called {
		t.Fatalf("err=%v called=%v, want cancellation before provider call", err, called)
	}
}

func TestProviderRateLimiterHotUpdatePreservesUsageAndWakesWaiter(t *testing.T) {
	limiter := newTestProviderRateLimiter(t, RateLimitConfig{RequestsPerMinute: 1})
	if _, err := limiter.Hook(context.Background()).Run(func() (ax.Value, error) { return nil, nil }, ax.AxRateLimitInfo{}); err != nil {
		t.Fatalf("first call: %v", err)
	}
	entered := make(chan struct{})
	var once sync.Once
	limiter.wait = func(ctx context.Context, _ time.Duration, notify <-chan struct{}) error {
		once.Do(func() { close(entered) })
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-notify:
			return nil
		}
	}
	done := make(chan error, 1)
	go func() {
		_, err := limiter.Hook(context.Background()).Run(func() (ax.Value, error) { return nil, nil }, ax.AxRateLimitInfo{})
		done <- err
	}()
	<-entered
	if err := limiter.Update(RateLimitConfig{RequestsPerMinute: 2}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("waiter after update: %v", err)
	}
	limiter.mu.Lock()
	requestCount := len(limiter.requests)
	limiter.mu.Unlock()
	if requestCount != 2 {
		t.Fatalf("request history was reset on update: %d", requestCount)
	}
}

func TestProviderRateLimiterPreservesRequestsWhenRPMIsHotEnabled(t *testing.T) {
	limiter := newTestProviderRateLimiter(t, RateLimitConfig{TokensPerMinute: 100})
	if _, err := limiter.Hook(context.Background()).Run(func() (ax.Value, error) { return nil, nil }, ax.AxRateLimitInfo{}); err != nil {
		t.Fatalf("TPM-only call: %v", err)
	}
	if err := limiter.Update(RateLimitConfig{RequestsPerMinute: 1, TokensPerMinute: 100}); err != nil {
		t.Fatalf("enable RPM: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	_, err := limiter.Hook(ctx).Run(func() (ax.Value, error) {
		called = true
		return nil, nil
	}, ax.AxRateLimitInfo{})
	if !errors.Is(err, context.Canceled) || called {
		t.Fatalf("hot-enabled RPM forgot recent request: err=%v called=%v", err, called)
	}
}

func TestProviderRateLimiterSerializesConcurrentAdmission(t *testing.T) {
	limiter := newTestProviderRateLimiter(t, RateLimitConfig{RequestsPerMinute: 1})
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	results := make(chan error, 2)
	var calls atomic.Int32
	for range 2 {
		go func() {
			_, err := limiter.Hook(ctx).Run(func() (ax.Value, error) {
				calls.Add(1)
				started <- struct{}{}
				<-release
				return nil, nil
			}, ax.AxRateLimitInfo{})
			results <- err
		}()
	}
	<-started
	cancel()
	close(release)
	first, second := <-results, <-results
	if calls.Load() != 1 {
		t.Fatalf("provider calls=%d, want one admitted call", calls.Load())
	}
	if !(first == nil && errors.Is(second, context.Canceled)) && !(second == nil && errors.Is(first, context.Canceled)) {
		t.Fatalf("results=(%v, %v), want one success and one cancellation", first, second)
	}
}

func TestAgentPassesProviderLimiterAsAxForwardHook(t *testing.T) {
	program := &fakeProgram{output: map[string]ax.Value{"status": StatusAnswered, "answer": "ok"}}
	runner := newAgent(Config{RateLimit: RateLimitConfig{RequestsPerMinute: 5}}, &fakeRuntime{},
		WithProgramFactory(func(string, map[string]ax.Value) Program { return program }),
	)
	// Keep this as a normal Ax-client path: assigning directly avoids marking
	// the deterministic fake as a private injected adapter.
	runner.newClient = func(Config) (ax.AIClient, error) { return fakeClient{}, nil }
	if _, err := runner.Run(context.Background(), Request{Instruction: "count customers"}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if _, ok := program.forwardOptions["rateLimiter"].(ax.AxRateLimiter); !ok {
		t.Fatalf("forward options missing Ax rate limiter: %#v", program.forwardOptions)
	}
}

func TestProviderRateLimiterRunsThroughAxForwardScope(t *testing.T) {
	transport := ax.NewScriptedTransport([]ax.Value{
		ax.Object("status", float64(200), "json", ax.Object(
			"choices", ax.Array(ax.Object("message", ax.Object("content", `{"answer":"ok"}`))),
		)),
	})
	client := ax.NewAI("openai", map[string]ax.Value{
		"apiKey": "test-token", "api_key": "test-token", "model": "gpt-4.1-mini", "transport": transport,
	})
	limiter := newTestProviderRateLimiter(t, RateLimitConfig{RequestsPerMinute: 1})
	program := ax.NewAx("question:string -> answer:string", nil)
	if _, err := program.Forward(context.Background(), client,
		map[string]ax.Value{"question": "say ok"},
		map[string]ax.Value{"rateLimiter": limiter.Hook(context.Background()), "structured_output_mode": StructuredOutputJSONObject},
	); err != nil {
		t.Fatalf("first scripted Ax forward: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _ = program.Forward(ctx, client,
		map[string]ax.Value{"question": "say ok again"},
		map[string]ax.Value{"rateLimiter": limiter.Hook(ctx), "structured_output_mode": StructuredOutputJSONObject},
	)
	if len(transport.Requests) != 1 {
		t.Fatalf("scripted provider requests=%d, want canceled second Ax call blocked locally", len(transport.Requests))
	}
}

func TestAgentRateLimitsInjectedClient(t *testing.T) {
	client := &rateLimitCountingClient{}
	runner := newAgent(Config{RateLimit: RateLimitConfig{RequestsPerMinute: 1}}, &fakeRuntime{},
		WithClientFactory(func(Config) (ax.AIClient, error) { return client, nil }),
		WithProgramFactory(func(string, map[string]ax.Value) Program { return rateLimitCallingProgram{} }),
	)
	if _, err := runner.Run(context.Background(), Request{Instruction: "first call"}); err != nil {
		t.Fatalf("first run: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _ = runner.Run(ctx, Request{Instruction: "second call"})
	if client.calls.Load() != 1 {
		t.Fatalf("injected provider calls=%d, want canceled second call blocked locally", client.calls.Load())
	}
}

func TestAgentPassesProviderLimiterToForcedFinalizer(t *testing.T) {
	program := &fakeProgram{err: errors.New("agent actor loop exceeded max steps")}
	finalizer := &fakeProgram{output: map[string]ax.Value{"answer": "INV-1 is paid."}}
	runner := newAgent(Config{RateLimit: RateLimitConfig{RequestsPerMinute: 5}}, &successfulExecutionRuntime{},
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
	// Keep the deterministic fake on the Ax-hook path instead of the injected
	// adapter path; the finalizer must inherit the same forward-scoped hook.
	runner.newClient = func(Config) (ax.AIClient, error) { return fakeClient{}, nil }
	if _, err := runner.Run(context.Background(), Request{Instruction: "show invoices"}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if _, ok := finalizer.forwardOptions["rateLimiter"].(ax.AxRateLimiter); !ok {
		t.Fatalf("forced finalizer missing Ax rate limiter: %#v", finalizer.forwardOptions)
	}
}

type rateLimitCountingClient struct {
	fakeClient
	calls atomic.Int32
}

func (c *rateLimitCountingClient) Chat(context.Context, map[string]ax.Value, map[string]ax.Value) (ax.Value, error) {
	c.calls.Add(1)
	return map[string]ax.Value{}, nil
}

type rateLimitCallingProgram struct{}

func (rateLimitCallingProgram) Forward(ctx context.Context, client ax.AIClient, _ map[string]ax.Value, _ map[string]ax.Value) (ax.Value, error) {
	if _, err := client.Chat(ctx, map[string]ax.Value{}, nil); err != nil {
		return nil, err
	}
	return map[string]ax.Value{"status": StatusAnswered, "answer": "ok"}, nil
}

func (rateLimitCallingProgram) GetActionLog() ax.Value { return nil }
func (rateLimitCallingProgram) GetUsage() ax.Value     { return nil }
func (rateLimitCallingProgram) GetChatLog() ax.Value   { return nil }
func (rateLimitCallingProgram) ExportTrace() ax.Value  { return nil }

func TestProviderRateLimiterWrapperPreservesClientFeatures(t *testing.T) {
	limiter := newTestProviderRateLimiter(t, RateLimitConfig{RequestsPerMinute: 1})
	client := limiter.WrapAIClient(rateLimitFeatureClient{}, "test", "model")
	features, ok := client.(interface {
		GetFeatures(string) map[string]ax.Value
	})
	if !ok || features.GetFeatures("model")["structured_outputs"] != true {
		t.Fatalf("wrapped client lost provider features: %#v", client)
	}
}

type rateLimitFeatureClient struct{ fakeClient }

func (rateLimitFeatureClient) GetFeatures(string) map[string]ax.Value {
	return map[string]ax.Value{"structured_outputs": true}
}

func newTestProviderRateLimiter(t *testing.T, config RateLimitConfig) *ProviderRateLimiter {
	t.Helper()
	limiter, err := NewProviderRateLimiter(config)
	if err != nil {
		t.Fatalf("NewProviderRateLimiter: %v", err)
	}
	return limiter
}
