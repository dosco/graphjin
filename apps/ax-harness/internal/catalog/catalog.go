// Package catalog prices model calls from the model catalog of the pinned Ax module.
package catalog

import (
	"encoding/json"
	"fmt"
	"math"
	"runtime/debug"
	"strings"
	"sync"

	ax "github.com/ax-llm/ax/packages/go"
)

const axModule = "github.com/ax-llm/ax/packages/go"

// Price is one catalog model's published rates in USD per million tokens.
// Absent cache and long-context rates fall back as in Ax's estimator.
type Price struct {
	Prompt               float64  `json:"promptTokenCostPer1M"`
	Completion           float64  `json:"completionTokenCostPer1M"`
	CacheRead            *float64 `json:"cacheReadTokenCostPer1M,omitempty"`
	CacheWrite           *float64 `json:"cacheWriteTokenCostPer1M,omitempty"`
	LongContextThreshold *int64   `json:"longContextThreshold,omitempty"`
	LongPrompt           *float64 `json:"longContextPromptTokenCostPer1M,omitempty"`
	LongCompletion       *float64 `json:"longContextCompletionTokenCostPer1M,omitempty"`
	LongCacheRead        *float64 `json:"longContextCacheReadTokenCostPer1M,omitempty"`
	LongCacheWrite       *float64 `json:"longContextCacheWriteTokenCostPer1M,omitempty"`
}

// Snapshot is the exported catalog: provider, then model or alias, then price.
type Snapshot struct {
	Version   string                      `json:"version"`
	Providers map[string]map[string]Price `json:"providers"`
}

var load = sync.OnceValues(parse)

func parse() (map[string]map[string]Price, error) {
	raw, err := json.Marshal(ax.GetSupportedAIModels(map[string]ax.Value{}))
	if err != nil {
		return nil, err
	}
	var entries []struct {
		Name   string `json:"name"`
		Models []struct {
			Price
			Name     string   `json:"name"`
			Aliases  []string `json:"aliases"`
			Currency string   `json:"currency"`
		} `json:"models"`
	}
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("read Ax model catalog: %w", err)
	}
	providers := map[string]map[string]Price{}
	for _, provider := range entries {
		for _, model := range provider.Models {
			if !strings.EqualFold(model.Currency, "usd") || model.Prompt <= 0 && model.Completion <= 0 {
				continue
			}
			if providers[provider.Name] == nil {
				providers[provider.Name] = map[string]Price{}
			}
			// Ax uses the first catalog entry that matches a name or an alias.
			for _, name := range append([]string{model.Name}, model.Aliases...) {
				if _, seen := providers[provider.Name][name]; !seen && name != "" {
					providers[provider.Name][name] = model.Price
				}
			}
		}
	}
	return providers, nil
}

// Version is the pinned Ax module version, which versions the catalog.
func Version() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, dep := range info.Deps {
			if dep.Path == axModule {
				if dep.Replace != nil {
					dep = dep.Replace
				}
				return dep.Version
			}
		}
	}
	return ""
}

// Export returns the catalog prices as indented JSON with stable key order.
func Export(version string) ([]byte, error) {
	providers, err := load()
	if err != nil {
		return nil, err
	}
	out, err := json.MarshalIndent(Snapshot{Version: version, Providers: providers}, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// catalogProviders maps a route provider onto the catalog providers that price
// it. Ax prices openai-compatible routes as OpenAI; Azure deployments serve
// OpenAI models, and Vertex AI serves Gemini and Claude.
func catalogProviders(provider string) []string {
	switch provider = strings.ToLower(provider); provider {
	case "openai-compatible", "azure-openai":
		return []string{"openai"}
	case "googlegeminiai":
		return []string{"google-gemini"}
	case "vertex-ai":
		return []string{"google-gemini", "anthropic"}
	default:
		return []string{provider}
	}
}

// Lookup finds the catalog price for a provider and model.
func Lookup(provider, model string) (Rates, bool) {
	providers, err := load()
	if err != nil {
		return Rates{}, false
	}
	for _, name := range catalogProviders(provider) {
		if price, ok := providers[name][model]; ok {
			return price.rates(), true
		}
	}
	return Rates{}, false
}

// Rates holds catalog prices in USD micros per million tokens, so a call's
// cost is exact integer arithmetic.
type Rates struct {
	Prompt, Completion, CacheRead, CacheWrite                 int64
	LongContextThreshold                                      int64
	LongPrompt, LongCompletion, LongCacheRead, LongCacheWrite int64
}

func micros(usd float64) int64 { return int64(math.Round(usd * 1_000_000)) }

func or(value *float64, fallback int64) int64 {
	if value == nil {
		return fallback
	}
	return micros(*value)
}

func (p Price) rates() Rates {
	r := Rates{Prompt: micros(p.Prompt), Completion: micros(p.Completion)}
	r.CacheRead = or(p.CacheRead, r.Prompt)
	r.CacheWrite = or(p.CacheWrite, r.Prompt)
	r.LongPrompt, r.LongCompletion, r.LongCacheRead, r.LongCacheWrite = r.Prompt, r.Completion, r.CacheRead, r.CacheWrite
	if p.LongContextThreshold != nil && *p.LongContextThreshold > 0 {
		r.LongContextThreshold = *p.LongContextThreshold
		r.LongPrompt = or(p.LongPrompt, r.Prompt)
		r.LongCompletion = or(p.LongCompletion, r.Completion)
		r.LongCacheRead = or(p.LongCacheRead, r.CacheRead)
		r.LongCacheWrite = or(p.LongCacheWrite, r.CacheWrite)
	}
	return r
}

const maxRateMicrosPerMillion int64 = 1_000_000_000

// Valid bounds every rate, so token products fit in int64.
func (r Rates) Valid() bool {
	for _, rate := range []int64{r.Prompt, r.Completion, r.CacheRead, r.CacheWrite, r.LongPrompt, r.LongCompletion, r.LongCacheRead, r.LongCacheWrite} {
		if rate < 0 || rate > maxRateMicrosPerMillion {
			return false
		}
	}
	return r.Prompt > 0 || r.Completion > 0
}

// Tokens is one model call's usage as Ax reports it: Input excludes cache
// reads and writes, and Total can hold reasoning tokens beyond the split.
type Tokens struct {
	Input, Output, Total, CacheRead, CacheWrite int64
}

// Cost prices one call. Input above the long-context threshold uses the
// long-context rates; tokens the total holds beyond the split bill as output.
func (r Rates) Cost(t Tokens) int64 {
	if !r.Valid() || t.Input < 0 || t.Output < 0 || t.Total < 0 || t.CacheRead < 0 || t.CacheWrite < 0 {
		return 0
	}
	prompt, completion, read, write := r.Prompt, r.Completion, r.CacheRead, r.CacheWrite
	if r.LongContextThreshold > 0 && t.Input+t.CacheRead+t.CacheWrite > r.LongContextThreshold {
		prompt, completion, read, write = r.LongPrompt, r.LongCompletion, r.LongCacheRead, r.LongCacheWrite
	}
	extra := t.Total - t.Input - t.Output - t.CacheRead - t.CacheWrite
	if extra < 0 {
		extra = 0
	}
	return millionthCeil(t.Input, prompt) + millionthCeil(t.CacheRead, read) +
		millionthCeil(t.CacheWrite, write) + millionthCeil(t.Output+extra, completion)
}

// Reservation prices tokens at the highest rate the call could incur.
func (r Rates) Reservation(tokens int64) int64 {
	if tokens <= 0 || !r.Valid() {
		return 0
	}
	var rate int64
	for _, candidate := range []int64{r.Prompt, r.Completion, r.CacheRead, r.CacheWrite, r.LongPrompt, r.LongCompletion, r.LongCacheRead, r.LongCacheWrite} {
		rate = max(rate, candidate)
	}
	return millionthCeil(tokens, rate)
}

// Rates are bounded at 1e9 micros per million and usage fields at 1e12
// tokens, so these products fit in int64 without floating point.
func millionthCeil(tokens, rate int64) int64 {
	if tokens <= 0 || rate <= 0 {
		return 0
	}
	const million int64 = 1_000_000
	whole := tokens / million * rate
	remainder := tokens % million * rate
	return whole + (remainder+million-1)/million
}
