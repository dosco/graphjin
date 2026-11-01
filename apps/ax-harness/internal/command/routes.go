package command

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"

	ax "github.com/ax-llm/ax/packages/go"
	"github.com/open-neko/openneko/apps/ax-harness/internal/agent"
	"github.com/open-neko/openneko/apps/ax-harness/internal/catalog"
)

type modelRoute struct {
	Key       string `json:"key"`
	Provider  string `json:"provider,omitempty"`
	Model     string `json:"model"`
	URL       string `json:"url,omitempty"`
	APIKeyEnv string `json:"api_key_env"`
	// Options are Ax provider settings, for example Azure's resource_name,
	// deployment_name and api_version.
	Options map[string]string `json:"options,omitempty"`
}

// reservedOptions belong to the harness: the key, the endpoint, the model and the transport.
var reservedOptions = map[string]bool{"api_key": true, "apiKey": true, "base_url": true, "baseUrl": true, "model": true,
	"credential_provider": true, "credentialProvider": true, "transport": true, "runtimeHooks": true, "retry": true}

func validOptions(options map[string]string) bool {
	if len(options) > 16 {
		return false
	}
	for key, value := range options {
		if reservedOptions[key] || !validOptionKey(key) || len(value) > 256 {
			return false
		}
	}
	return true
}

func validOptionKey(key string) bool {
	if key == "" || len(key) > 64 {
		return false
	}
	for _, c := range key {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}

const openAICompatible = "openai-compatible"

func (r modelRoute) provider() string {
	if r.Provider == "" {
		return openAICompatible
	}
	return r.Provider
}

type modelFallback struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type routeConfig struct {
	Context             string          `json:"context"`
	Executor            string          `json:"executor"`
	ExecutorEscalation  string          `json:"executor_escalation,omitempty"`
	ExecutorAfterErrors int             `json:"executor_after_errors,omitempty"`
	Responder           string          `json:"responder"`
	Fallbacks           []modelFallback `json:"fallbacks,omitempty"`
	Routes              []modelRoute    `json:"routes"`
}

// noClientRetry turns off Ax's request-layer retry. The harness owns retries,
// route fallback and the model-call ceiling; a hidden retry would bypass all three.
func noClientRetry() ax.Value { return ax.Object("max_retries", 0) }

// newClient builds one provider client with Ax client retry off. A native
// provider without a URL uses Ax's default base URL.
func newClient(provider, base, key, model string, extra map[string]string) (client ax.AxAIService, err error) {
	// Ax checks Azure's endpoint only at the first request; fail at start instead.
	if provider == "azure-openai" && base == "" && (extra["resource_name"] == "" || extra["deployment_name"] == "") {
		return nil, fmt.Errorf("azure-openai needs a URL, or resource_name and deployment_name options")
	}
	options := ax.Object("api_key", key, "model", model, "retry", noClientRetry())
	for name, value := range extra {
		options[name] = value
	}
	if base != "" {
		options["base_url"] = base
	}
	if provider == openAICompatible {
		return ax.NewOpenAICompatibleClient(options), nil
	}
	defer func() {
		if recover() != nil {
			client, err = nil, fmt.Errorf("model provider %q is unsupported or needs a URL", provider)
		}
	}()
	service, ok := ax.NewAI(provider, options).(ax.AxAIService)
	if !ok {
		return nil, fmt.Errorf("model provider %q is unsupported", provider)
	}
	return service, nil
}

// setRouteName makes the rate-limit hook report the route key, which keys
// prices, fallbacks and model names.
func setRouteName(client ax.AxAIService, key string) error {
	switch c := client.(type) {
	case *ax.OpenAICompatibleClient:
		c.Name = key
	case *ax.AnthropicClient:
		c.Name = key
	case *ax.GoogleGeminiClient:
		c.Name = key
	case *ax.OpenAIResponsesClient:
		c.Name = key
	default:
		return fmt.Errorf("model provider cannot carry a route key")
	}
	return nil
}

func validProviderURL(provider, base string) error {
	if base == "" && provider != openAICompatible {
		return nil
	}
	return validModelURL(base)
}

// loadModelClient reads a host-owned allowlist. The run JSON cannot add a
// provider, change a stage model, or name a credential.
func loadModelClient(getenv func(string) string) (ax.AIClient, error) {
	raw := getenv("HARNESS_MODEL_ROUTES")
	if raw == "" {
		route := modelRoute{Key: "default", Provider: getenv("HARNESS_MODEL_PROVIDER"), Model: getenv("HARNESS_MODEL"), URL: getenv("HARNESS_MODEL_URL")}
		key := getenv("HARNESS_MODEL_API_KEY")
		if err := validProviderURL(route.provider(), route.URL); err != nil || route.Model == "" || key == "" {
			return nil, fmt.Errorf("configure HARNESS_MODEL_URL, HARNESS_MODEL and HARNESS_MODEL_API_KEY")
		}
		if raw := getenv("HARNESS_MODEL_OPTIONS"); raw != "" {
			if len(raw) > 8192 || json.Unmarshal([]byte(raw), &route.Options) != nil || !validOptions(route.Options) {
				return nil, fmt.Errorf("invalid HARNESS_MODEL_OPTIONS")
			}
		}
		client, err := newClient(route.provider(), route.URL, key, route.Model, route.Options)
		if err != nil {
			return nil, err
		}
		name := route.provider()
		if err := setRouteName(client, name); err != nil {
			return nil, err
		}
		return &agent.RoutedClient{AIClient: client, ModelNames: map[string]string{name: route.Model},
			Providers: map[string]string{name: route.provider()}, DefaultProvider: route.provider(),
			CatalogVersion: catalog.Version(), Prices: routePrices([]modelRoute{{Key: name, Provider: route.Provider, Model: route.Model}})}, nil
	}
	cfg, err := parseRouteConfig(raw)
	if err != nil {
		return nil, err
	}
	entries := make([]ax.Value, 0, len(cfg.Routes))
	for _, route := range cfg.Routes {
		if getenv(route.APIKeyEnv) == "" {
			return nil, fmt.Errorf("HARNESS_MODEL_ROUTES route missing credential")
		}
		service, err := newClient(route.provider(), route.URL, getenv(route.APIKeyEnv), route.Model, route.Options)
		if err != nil {
			return nil, err
		}
		// A logical key can distinguish two accounts that expose the same model.
		// Ax's explicit router entry strips the key before the provider call, so
		// the provider uses its configured actual model.
		if err := setRouteName(service, route.Key); err != nil {
			return nil, err
		}
		entries = append(entries, ax.RouterServiceEntry{Key: route.Key, Description: route.Model, Service: service})
	}
	router, err := ax.NewMultiServiceRouter(entries)
	if err != nil {
		return nil, fmt.Errorf("invalid HARNESS_MODEL_ROUTES: %w", err)
	}
	modelNames := make(map[string]string, len(cfg.Routes))
	providers := make(map[string]string, len(cfg.Routes))
	fallbacks := make(map[string]string, len(cfg.Fallbacks))
	for _, route := range cfg.Routes {
		modelNames[route.Key] = route.Model
		providers[route.Key] = route.provider()
	}
	for _, fallback := range cfg.Fallbacks {
		fallbacks[fallback.From] = fallback.To
	}
	return &agent.RoutedClient{AIClient: router, Stages: agent.StageModels{
		Context: cfg.Context, Executor: cfg.Executor, Responder: cfg.Responder,
		ExecutorEscalation: cfg.ExecutorEscalation, ExecutorAfterErrors: cfg.ExecutorAfterErrors,
	}, ModelNames: modelNames, Providers: providers, Fallbacks: fallbacks, CatalogVersion: catalog.Version(), Prices: routePrices(cfg.Routes)}, nil
}

// routePrices looks up each route's provider and model in the Ax catalog.
func routePrices(routes []modelRoute) map[string]catalog.Rates {
	prices := make(map[string]catalog.Rates, len(routes))
	for _, route := range routes {
		if rates, ok := catalog.Lookup(route.provider(), route.Model); ok {
			prices[route.Key] = rates
		}
	}
	return prices
}

// KeyEnvNames lists the environment variables that hold model credentials,
// so a child process can run without them.
func KeyEnvNames(getenv func(string) string) []string {
	names := []string{"HARNESS_MODEL_API_KEY", "HARNESS_MODEL_ROUTES"}
	if raw := getenv("HARNESS_MODEL_ROUTES"); raw != "" {
		if cfg, err := parseRouteConfig(raw); err == nil {
			for _, route := range cfg.Routes {
				names = append(names, route.APIKeyEnv)
			}
		}
	}
	return names
}

func parseRouteConfig(raw string) (routeConfig, error) {
	if len(raw) > 65536 {
		return routeConfig{}, fmt.Errorf("HARNESS_MODEL_ROUTES exceeds limit")
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	var cfg routeConfig
	if err := decoder.Decode(&cfg); err != nil {
		return routeConfig{}, fmt.Errorf("invalid HARNESS_MODEL_ROUTES")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return routeConfig{}, fmt.Errorf("invalid HARNESS_MODEL_ROUTES")
	}
	if len(cfg.Routes) == 0 || len(cfg.Routes) > 8 || cfg.Context == "" || cfg.Executor == "" || cfg.Responder == "" {
		return routeConfig{}, fmt.Errorf("invalid HARNESS_MODEL_ROUTES profile")
	}
	known := make(map[string]bool, len(cfg.Routes))
	for _, route := range cfg.Routes {
		if !validRouteKey(route.Key) || route.Model == "" || len(route.Model) > 128 || known[route.Key] || len(route.Provider) > 64 || validProviderURL(route.provider(), route.URL) != nil ||
			!validEnvName(route.APIKeyEnv) || !validOptions(route.Options) {
			return routeConfig{}, fmt.Errorf("invalid HARNESS_MODEL_ROUTES route")
		}
		known[route.Key] = true
	}
	for _, model := range []string{cfg.Context, cfg.Executor, cfg.Responder, cfg.ExecutorEscalation} {
		if model == "" {
			continue
		}
		if !known[model] {
			return routeConfig{}, fmt.Errorf("HARNESS_MODEL_ROUTES stage has no approved route")
		}
	}
	if cfg.ExecutorEscalation == "" && cfg.ExecutorAfterErrors != 0 ||
		cfg.ExecutorEscalation != "" && (cfg.ExecutorAfterErrors < 1 || cfg.ExecutorAfterErrors > 8 ||
			cfg.ExecutorEscalation == cfg.Executor || cfg.Executor == cfg.Context || cfg.Executor == cfg.Responder) {
		return routeConfig{}, fmt.Errorf("invalid HARNESS_MODEL_ROUTES executor escalation")
	}
	if len(cfg.Fallbacks) > len(cfg.Routes) {
		return routeConfig{}, fmt.Errorf("invalid HARNESS_MODEL_ROUTES fallback profile")
	}
	active := map[string]bool{cfg.Context: true, cfg.Executor: true, cfg.Responder: true}
	if cfg.ExecutorEscalation != "" {
		active[cfg.ExecutorEscalation] = true
	}
	fallbackSources := map[string]bool{}
	for _, fallback := range cfg.Fallbacks {
		if !active[fallback.From] || !known[fallback.To] || fallback.From == fallback.To || fallbackSources[fallback.From] {
			return routeConfig{}, fmt.Errorf("invalid HARNESS_MODEL_ROUTES fallback profile")
		}
		fallbackSources[fallback.From] = true
	}
	return cfg, nil
}

func validModelURL(base string) error {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("invalid model URL")
	}
	return nil
}

func validEnvName(name string) bool {
	if len(name) < 2 || len(name) > 128 || name[0] < 'A' || name[0] > 'Z' {
		return false
	}
	for _, c := range name[1:] {
		if !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}

func validRouteKey(key string) bool {
	if len(key) < 1 || len(key) > 64 || key[0] < 'a' || key[0] > 'z' {
		return false
	}
	for _, c := range key[1:] {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}
