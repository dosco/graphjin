package agent

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	ax "github.com/ax-llm/ax/packages/go"
)

const providerRateLimitWindow = time.Minute

// RateLimitConfig sets process-local limits for model calls made with the
// server-owned agent provider. Each zero value disables that dimension.
type RateLimitConfig struct {
	RequestsPerMinute int `mapstructure:"requests_per_minute" json:"requests_per_minute" yaml:"requests_per_minute" jsonschema:"title=Agent Provider Requests Per Minute,description=Process-local rolling-minute request ceiling; zero disables request limiting,minimum=0,default=0"`
	TokensPerMinute   int `mapstructure:"tokens_per_minute" json:"tokens_per_minute" yaml:"tokens_per_minute" jsonschema:"title=Agent Provider Tokens Per Minute,description=Process-local rolling-minute provider-reported token ceiling; zero disables token limiting,minimum=0,default=0"`
}

// Validate rejects ambiguous negative limits instead of silently disabling
// them. Zero is the explicit disabled value for either dimension.
func (c RateLimitConfig) Validate() error {
	if c.RequestsPerMinute < 0 {
		return fmt.Errorf("agent.rate_limit.requests_per_minute must be zero or greater")
	}
	if c.TokensPerMinute < 0 {
		return fmt.Errorf("agent.rate_limit.tokens_per_minute must be zero or greater")
	}
	return nil
}

func (c RateLimitConfig) enabled() bool {
	return c.RequestsPerMinute > 0 || c.TokensPerMinute > 0
}

type providerTokenEvent struct {
	at     time.Time
	tokens int64
}

// ProviderRateLimiter is one shared, concurrency-safe rolling-window limiter.
// The service keeps one instance so independently constructed REST, MCP, watch,
// and task agents consume the same process-local provider allowance.
type ProviderRateLimiter struct {
	mu       sync.Mutex
	config   RateLimitConfig
	requests []time.Time
	tokens   []providerTokenEvent
	notify   chan struct{}
	now      func() time.Time
	wait     func(context.Context, time.Duration, <-chan struct{}) error
}

// NewProviderRateLimiter constructs a limiter whose recent usage survives
// later Update calls. The caller may keep a disabled limiter and enable it via
// a hot configuration update.
func NewProviderRateLimiter(config RateLimitConfig) (*ProviderRateLimiter, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	l := &ProviderRateLimiter{
		config: config,
		notify: make(chan struct{}),
		now:    time.Now,
		wait:   waitForProviderCapacity,
	}
	return l, nil
}

// Update changes the ceilings without discarding recent request or token
// usage. Existing waiters wake immediately and re-evaluate the new limits.
func (l *ProviderRateLimiter) Update(config RateLimitConfig) error {
	if err := config.Validate(); err != nil {
		return err
	}
	if l == nil {
		return nil
	}
	l.mu.Lock()
	if l.notify == nil {
		l.notify = make(chan struct{})
	}
	if l.now == nil {
		l.now = time.Now
	}
	if l.wait == nil {
		l.wait = waitForProviderCapacity
	}
	l.config = config
	l.signalLocked()
	l.mu.Unlock()
	return nil
}

// Config returns the currently effective ceilings.
func (l *ProviderRateLimiter) Config() RateLimitConfig {
	if l == nil {
		return RateLimitConfig{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.config
}

// Enabled reports whether either rate-limit dimension is active.
func (l *ProviderRateLimiter) Enabled() bool {
	return l != nil && l.Config().enabled()
}

// Hook returns a context-bound Ax runtime hook. Ax carries a forward-scoped
// hook through every internal agent generator and direct model call.
func (l *ProviderRateLimiter) Hook(ctx context.Context) ax.AxRateLimiter {
	if l == nil || !l.Enabled() {
		return nil
	}
	return ax.AxRateLimiterFunc(func(next ax.AxRequestExecutor, info ax.AxRateLimitInfo) (ax.Value, error) {
		return l.run(ctx, next, info)
	})
}

// WrapAIClient applies the same limiter to an injected/private AI adapter.
// Such clients may not implement Ax's runtime-hook machinery themselves, so
// the wrapper keeps that extension seam covered without double-limiting normal
// clients created by Ax.
func (l *ProviderRateLimiter) WrapAIClient(client ax.AIClient, provider, model string) ax.AIClient {
	if l == nil || !l.Enabled() || client == nil {
		return client
	}
	return &rateLimitedAIClient{inner: client, limiter: l, provider: provider, model: model}
}

func (l *ProviderRateLimiter) run(ctx context.Context, next ax.AxRequestExecutor, info ax.AxRateLimitInfo) (ax.Value, error) {
	if err := l.acquire(ctx); err != nil {
		return nil, err
	}
	value, err := next()
	if tokens := providerResponseTokens(value); tokens > 0 {
		l.recordTokens(tokens)
	}
	return value, err
}

func (l *ProviderRateLimiter) acquire(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		now := l.now()
		l.mu.Lock()
		l.pruneLocked(now)
		config := l.config
		if !config.enabled() {
			l.mu.Unlock()
			return nil
		}

		delay := l.requestDelayLocked(now, config.RequestsPerMinute)
		if tokenDelay := l.tokenDelayLocked(now, int64(config.TokensPerMinute)); tokenDelay > delay {
			delay = tokenDelay
		}
		if delay <= 0 {
			// Keep request history whenever either dimension is active. That lets
			// a hot update enable RPM without forgetting calls already admitted
			// while only TPM was configured.
			l.requests = append(l.requests, now)
			l.mu.Unlock()
			return nil
		}
		notify := l.notify
		l.mu.Unlock()
		if err := l.wait(ctx, delay, notify); err != nil {
			return err
		}
	}
}

func (l *ProviderRateLimiter) requestDelayLocked(now time.Time, limit int) time.Duration {
	if limit <= 0 || len(l.requests) < limit {
		return 0
	}
	return positiveDuration(l.requests[0].Add(providerRateLimitWindow).Sub(now))
}

func (l *ProviderRateLimiter) tokenDelayLocked(now time.Time, limit int64) time.Duration {
	if limit <= 0 {
		return 0
	}
	var total int64
	for _, event := range l.tokens {
		total += event.tokens
	}
	if total < limit {
		return 0
	}
	for _, event := range l.tokens {
		total -= event.tokens
		if total < limit {
			return positiveDuration(event.at.Add(providerRateLimitWindow).Sub(now))
		}
	}
	return providerRateLimitWindow
}

func (l *ProviderRateLimiter) recordTokens(tokens int64) {
	if l == nil || tokens <= 0 {
		return
	}
	now := l.now()
	l.mu.Lock()
	l.pruneLocked(now)
	l.tokens = append(l.tokens, providerTokenEvent{at: now, tokens: tokens})
	l.signalLocked()
	l.mu.Unlock()
}

func (l *ProviderRateLimiter) pruneLocked(now time.Time) {
	cutoff := now.Add(-providerRateLimitWindow)
	requestStart := 0
	for requestStart < len(l.requests) && !l.requests[requestStart].After(cutoff) {
		requestStart++
	}
	if requestStart != 0 {
		l.requests = append(l.requests[:0], l.requests[requestStart:]...)
	}
	tokenStart := 0
	for tokenStart < len(l.tokens) && !l.tokens[tokenStart].at.After(cutoff) {
		tokenStart++
	}
	if tokenStart != 0 {
		l.tokens = append(l.tokens[:0], l.tokens[tokenStart:]...)
	}
}

func (l *ProviderRateLimiter) signalLocked() {
	if l.notify != nil {
		close(l.notify)
	}
	l.notify = make(chan struct{})
}

func waitForProviderCapacity(ctx context.Context, delay time.Duration, notify <-chan struct{}) error {
	timer := time.NewTimer(positiveDuration(delay))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-notify:
		return nil
	case <-timer.C:
		return nil
	}
}

func positiveDuration(value time.Duration) time.Duration {
	if value <= 0 {
		return time.Nanosecond
	}
	return value
}

func providerResponseTokens(value ax.Value) int64 {
	if values, ok := value.([]ax.Value); ok {
		for i := len(values) - 1; i >= 0; i-- {
			if tokens := providerResponseTokens(values[i]); tokens > 0 {
				return tokens
			}
		}
		return 0
	}
	mapped := mapValue(normalizeValue(value))
	usage := mapValue(mapped["model_usage"])
	if len(usage) == 0 {
		usage = mapValue(mapped["modelUsage"])
	}
	tokens := mapValue(usage["tokens"])
	if len(tokens) == 0 {
		return 0
	}
	if total := tokenCount(tokens, "total_tokens", "totalTokens", "total"); total > 0 {
		return total
	}
	prompt := tokenCount(tokens, "prompt_tokens", "promptTokens", "prompt", "input_tokens", "inputTokens")
	completion := tokenCount(tokens, "completion_tokens", "completionTokens", "completion", "output_tokens", "outputTokens")
	return prompt + completion
}

func tokenCount(tokens map[string]any, keys ...string) int64 {
	for _, key := range keys {
		if value, ok := tokens[key]; ok {
			count := floatFromAny(value)
			if count > 0 {
				return int64(math.Ceil(count))
			}
		}
	}
	return 0
}

type rateLimitedAIClient struct {
	inner    ax.AIClient
	limiter  *ProviderRateLimiter
	provider string
	model    string
}

func (c *rateLimitedAIClient) Chat(ctx context.Context, request map[string]ax.Value, options map[string]ax.Value) (ax.Value, error) {
	info := ax.AxRateLimitInfo{Operation: "chat", Provider: c.provider, Model: requestModel(request, c.model)}
	return c.limiter.run(ctx, func() (ax.Value, error) { return c.inner.Chat(ctx, request, options) }, info)
}

func (c *rateLimitedAIClient) Embed(ctx context.Context, request map[string]ax.Value, options map[string]ax.Value) (ax.Value, error) {
	info := ax.AxRateLimitInfo{Operation: "embed", Provider: c.provider, Model: requestModel(request, c.model)}
	return c.limiter.run(ctx, func() (ax.Value, error) { return c.inner.Embed(ctx, request, options) }, info)
}

func (c *rateLimitedAIClient) Stream(ctx context.Context, request map[string]ax.Value, options map[string]ax.Value) ([]ax.Value, error) {
	info := ax.AxRateLimitInfo{Operation: "chat", Provider: c.provider, Model: requestModel(request, c.model), Streaming: true}
	value, err := c.limiter.run(ctx, func() (ax.Value, error) { return c.inner.Stream(ctx, request, options) }, info)
	if value == nil {
		return nil, err
	}
	values, ok := value.([]ax.Value)
	if !ok {
		return nil, fmt.Errorf("rate-limited AI stream returned %T", value)
	}
	return values, err
}

// GetFeatures preserves Ax's optional deployment-capability seam so wrapping a
// private client cannot silently change structured-output selection.
func (c *rateLimitedAIClient) GetFeatures(model string) map[string]ax.Value {
	if inner, ok := c.inner.(interface {
		GetFeatures(string) map[string]ax.Value
	}); ok {
		return inner.GetFeatures(model)
	}
	return nil
}

func requestModel(request map[string]ax.Value, fallback string) string {
	if model, ok := request["model"].(string); ok && model != "" {
		return model
	}
	return fallback
}
