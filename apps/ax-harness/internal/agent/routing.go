package agent

import (
	"context"
	"sync/atomic"

	ax "github.com/ax-llm/ax/packages/go"
	"github.com/open-neko/openneko/apps/ax-harness/internal/catalog"
)

// StageModels is host configuration, never a field supplied by a run or a tool.
// Empty models retain Ax's existing single-client behavior.
type StageModels struct {
	Context   string
	Executor  string
	Responder string
	// An optional approved route for later executor turns after actor-code
	// errors. The host chooses both aliases; the model cannot change them.
	ExecutorEscalation  string
	ExecutorAfterErrors int
}

// RoutedClient keeps route selection with the model transport. The agent reads
// only the stage profile; capability admission remains entirely in Tools.
type RoutedClient struct {
	ax.AIClient
	Stages StageModels
	// ModelNames comes only from the trusted host route configuration. Ax's
	// OpenAI-compatible rate-limit callback can omit the actual model name.
	ModelNames map[string]string
	// Providers maps a route key to its provider type, for example "anthropic".
	Providers       map[string]string
	DefaultProvider string
	Fallbacks       map[string]string
	// Prices holds the Ax catalog rates of each priced route key.
	CatalogVersion string
	Prices         map[string]catalog.Rates
}

// transientRouteFallback changes only a failed model-generation request. Ax
// classifies provider errors; a nonretryable denial, cancellation, or a call
// that returned content never reaches the alternate route. The second call
// enters the selected provider's own admission hook and is charged separately.
// Stream is intentionally not retried: the AIClient slice API cannot prove
// whether a failed stream already emitted content.
type transientRouteFallback struct {
	ax.AIClient
	fallbacks map[string]string
	before    func(from, to string) error
}

func (r *transientRouteFallback) Chat(ctx context.Context, request, options map[string]ax.Value) (ax.Value, error) {
	result, err := r.AIClient.Chat(ctx, request, options)
	if err == nil || result != nil || ctx.Err() != nil || !ax.IsRetryable(err) {
		return result, err
	}
	from, _ := request["model"].(string)
	to := r.fallbacks[from]
	if to == "" || to == from {
		return result, err
	}
	if r.before != nil {
		if recordErr := r.before(from, to); recordErr != nil {
			return nil, recordErr
		}
	}
	copy := make(map[string]ax.Value, len(request))
	for key, value := range request {
		copy[key] = value
	}
	copy["model"] = to
	return r.AIClient.Chat(ctx, copy, options)
}

func (r *transientRouteFallback) StreamEvents(ctx context.Context, request, options map[string]ax.Value) (ax.AxChatStream, error) {
	// A stream error may follow visible content. Do not retry on another route.
	return streamEvents(ctx, r.AIClient, request, options)
}

func (r *transientRouteFallback) GetFeatures(model string) map[string]ax.Value {
	if features, ok := r.AIClient.(interface {
		GetFeatures(string) map[string]ax.Value
	}); ok {
		return features.GetFeatures(model)
	}
	return nil
}

// Keep Ax's model-aware capabilities visible through the Harness wrapper.
func (r *RoutedClient) GetFeatures(model string) map[string]ax.Value {
	if features, ok := r.AIClient.(interface {
		GetFeatures(string) map[string]ax.Value
	}); ok {
		return features.GetFeatures(model)
	}
	return nil
}

func (r *RoutedClient) StreamEvents(ctx context.Context, request, options map[string]ax.Value) (ax.AxChatStream, error) {
	return streamEvents(ctx, r.AIClient, request, options)
}

// providerFor returns the provider type behind a stage route.
func (r *RoutedClient) providerFor(route string) string {
	if provider := r.Providers[route]; provider != "" {
		return provider
	}
	return r.DefaultProvider
}

// executorErrorRoute rewrites only an executor alias after a committed error
// turn. The underlying Ax router still selects the provider, and its rate
// limiter sees the actual route before the request is dispatched. It does not
// retry a request or repeat a tool effect.
type executorErrorRoute struct {
	ax.AIClient
	baseline   string
	escalation string
	after      int32
	errors     *atomic.Int32
}

func (r *executorErrorRoute) selected(model string) string {
	if model == r.baseline && r.errors.Load() >= r.after {
		return r.escalation
	}
	return model
}

func (r *executorErrorRoute) request(request map[string]ax.Value) map[string]ax.Value {
	model, _ := request["model"].(string)
	selected := r.selected(model)
	if selected == model {
		return request
	}
	copy := make(map[string]ax.Value, len(request))
	for key, value := range request {
		copy[key] = value
	}
	copy["model"] = selected
	return copy
}

func (r *executorErrorRoute) Chat(ctx context.Context, request, options map[string]ax.Value) (ax.Value, error) {
	return r.AIClient.Chat(ctx, r.request(request), options)
}

func (r *executorErrorRoute) Stream(ctx context.Context, request, options map[string]ax.Value) ([]ax.Value, error) {
	return r.AIClient.Stream(ctx, r.request(request), options)
}

func (r *executorErrorRoute) StreamEvents(ctx context.Context, request, options map[string]ax.Value) (ax.AxChatStream, error) {
	return streamEvents(ctx, r.AIClient, r.request(request), options)
}

// AxGen checks provider features before transport. Preserve the router's
// model-aware capabilities when decorating it.
func (r *executorErrorRoute) GetFeatures(model string) map[string]ax.Value {
	if features, ok := r.AIClient.(interface {
		GetFeatures(string) map[string]ax.Value
	}); ok {
		return features.GetFeatures(r.selected(model))
	}
	return nil
}

// fullyPriced reports whether every route has a catalog price.
func (r *RoutedClient) fullyPriced() bool {
	if r == nil || len(r.ModelNames) == 0 {
		return false
	}
	for route := range r.ModelNames {
		if rates, ok := r.Prices[route]; !ok || !rates.Valid() {
			return false
		}
	}
	return true
}
