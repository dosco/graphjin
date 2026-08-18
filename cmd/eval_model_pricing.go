package main

import (
	_ "embed"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Provider list prices live beside the frozen suite rather than in the publish
// command line. Cost is the column that turns a score into a decision, and
// passing it as a flag meant every publisher had to remember it — the entire
// 2028.2 cohort shipped without prices, so the leaderboard's cost axis read
// zero for every model. A committed price card cannot be forgotten, and it
// carries its own citation.
//
//go:embed benchmark/model-pricing.yaml
var modelPricingData []byte

type modelPrice struct {
	Provider             string  `yaml:"provider"`
	Model                string  `yaml:"model"`
	PromptPerMillion     float64 `yaml:"prompt_per_million"`
	CompletionPerMillion float64 `yaml:"completion_per_million"`
	Source               string  `yaml:"source"`
	AsOf                 string  `yaml:"as_of"`
}

type modelPricingCard struct {
	Version int          `yaml:"version"`
	Models  []modelPrice `yaml:"models"`
}

// lookupModelPrice returns the committed list price for one provider and model.
// Matching is case-insensitive and ignores surrounding space so a provenance
// value recorded as "DeepSeek" still resolves.
func lookupModelPrice(provider, model string) (modelPrice, bool) {
	var card modelPricingCard
	if err := yaml.Unmarshal(modelPricingData, &card); err != nil {
		return modelPrice{}, false
	}
	want := func(value string) string { return strings.ToLower(strings.TrimSpace(value)) }
	for _, entry := range card.Models {
		if want(entry.Provider) == want(provider) && want(entry.Model) == want(model) {
			if entry.PromptPerMillion <= 0 && entry.CompletionPerMillion <= 0 {
				return modelPrice{}, false
			}
			return entry, true
		}
	}
	return modelPrice{}, false
}

// modelPricingCitation renders the source line stored on the published row.
func (p modelPrice) citation() string {
	source := strings.TrimSpace(p.Source)
	if source == "" {
		return "provider list pricing"
	}
	if strings.TrimSpace(p.AsOf) == "" || strings.Contains(source, p.AsOf) {
		return source
	}
	return fmt.Sprintf("%s (%s)", source, p.AsOf)
}
