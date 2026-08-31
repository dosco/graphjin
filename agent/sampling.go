package agent

import (
	"context"
	"fmt"

	ax "github.com/ax-llm/ax/packages/go"
)

// Pinning how the provider samples.
//
// ax builds every client with model_config.temperature = 0 when none is given,
// so the stack is greedy by default and every repeat of a task returns the same
// program. That is right for a benchmark and useless for training: rejection
// sampling learns from the spread between several attempts at one task, and
// with no spread there is nothing to select.
//
// The setting is injected at the client boundary rather than passed as a
// forward option because one call in an agent run does not carry forward
// options at all — the nested query the runtime issues mid-program forwards an
// empty option map. A wrapper here is the only place that sees every request.
type samplingClient struct {
	inner       ax.AIClient
	temperature *float64
	topP        *float64
}

// withSampling merges the pinned values into the request's model config.
//
// Request-level config wins the merge ax performs against the client's own, so
// what is set here is what the provider is asked for. Both spellings of top_p
// are written for the same reason the reasoning wrapper writes both of its
// own: providers disagree, and ax reads whichever the transport expects.
func (c *samplingClient) withSampling(req map[string]ax.Value) map[string]ax.Value {
	if req == nil {
		return req
	}
	config, _ := req["model_config"].(map[string]ax.Value)
	if config == nil {
		config = map[string]ax.Value{}
	}
	if c.temperature != nil {
		config["temperature"] = *c.temperature
	}
	if c.topP != nil {
		config["topP"] = *c.topP
		config["top_p"] = *c.topP
	}
	req["model_config"] = config
	return req
}

func (c *samplingClient) Chat(ctx context.Context, req map[string]ax.Value, opts map[string]ax.Value) (ax.Value, error) {
	return c.inner.Chat(ctx, c.withSampling(req), opts)
}

func (c *samplingClient) Embed(ctx context.Context, req map[string]ax.Value, opts map[string]ax.Value) (ax.Value, error) {
	return c.inner.Embed(ctx, req, opts)
}

func (c *samplingClient) Stream(ctx context.Context, req map[string]ax.Value, opts map[string]ax.Value) ([]ax.Value, error) {
	return c.inner.Stream(ctx, c.withSampling(req), opts)
}

// GetFeatures forwards the provider's capability report, for the reason spelled
// out on the reasoning wrapper: ax type-asserts this to choose the
// structured-output mechanism, and a wrapper that swallows it gets the
// permissive default — which once sent DeepSeek a request it rejects, 71 times
// in a row.
func (c *samplingClient) GetFeatures(model string) map[string]ax.Value {
	if inner, ok := c.inner.(interface {
		GetFeatures(string) map[string]ax.Value
	}); ok {
		return inner.GetFeatures(model)
	}
	return nil
}

// ValidateSampling checks configured sampling values.
//
// Nil is always fine — it means the stack's own default. A supplied value is
// bounded because a provider given an out-of-range one fails the whole run at
// request time, which reads as an outage rather than a typo.
func ValidateSampling(temperature, topP *float64) error {
	if temperature != nil && (*temperature < 0 || *temperature > 2) {
		return fmt.Errorf("agent.temperature must be between 0 and 2, got %v", *temperature)
	}
	if topP != nil && (*topP <= 0 || *topP > 1) {
		return fmt.Errorf("agent.top_p must be greater than 0 and at most 1, got %v", *topP)
	}
	return nil
}
