package agent

import (
	"encoding/json"

	ax "github.com/ax-llm/ax/packages/go"
)

// ModelUsage is content-free accounting. Requests includes calls with no
// provider usage, so coverage remains honest after a crash or missing report.
type ModelUsage struct {
	Requests         int    `json:"requests,omitempty"`
	Reported         int    `json:"reported,omitempty"`
	InputTokens      int64  `json:"input_tokens,omitempty"`
	OutputTokens     int64  `json:"output_tokens,omitempty"`
	TotalTokens      int64  `json:"total_tokens,omitempty"`
	CacheReadTokens  int64  `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int64  `json:"cache_write_tokens,omitempty"`
	ReasoningTokens  int64  `json:"reasoning_tokens,omitempty"`
	Coverage         string `json:"coverage,omitempty"`
	// CostMicros sums the catalog cost of the reported calls. PricedBy is
	// "catalog" only when every reported call had a catalog price.
	CostMicros     int64  `json:"cost_micros,omitempty"`
	PricedBy       string `json:"priced_by,omitempty"`
	CatalogVersion string `json:"catalog_version,omitempty"`
	priced         int
}

func (u *ModelUsage) AddReported(other ModelUsage) {
	u.Reported += other.Reported
	u.InputTokens += other.InputTokens
	u.OutputTokens += other.OutputTokens
	u.TotalTokens += other.TotalTokens
	u.CacheReadTokens += other.CacheReadTokens
	u.CacheWriteTokens += other.CacheWriteTokens
	u.ReasoningTokens += other.ReasoningTokens
	u.CostMicros += other.CostMicros
	u.priced += other.priced
}

func (u *ModelUsage) setCoverage() {
	switch {
	case u.Requests > 0 && u.Reported == u.Requests:
		u.Coverage = "complete"
	case u.Reported > 0:
		u.Coverage = "partial"
	default:
		u.Coverage = "unavailable"
	}
	u.PricedBy = unpriced
	if u.Reported > 0 && u.priced == u.Reported {
		u.PricedBy = pricedByCatalog
	}
}

func modelTokens(response ax.Value) (ModelUsage, bool) {
	value, ok := response.(map[string]ax.Value)
	if !ok {
		return ModelUsage{}, false
	}
	model, ok := value["model_usage"].(map[string]ax.Value)
	if !ok {
		return ModelUsage{}, false
	}
	raw, err := json.Marshal(model["tokens"])
	if err != nil || string(raw) == "null" {
		return ModelUsage{}, false
	}
	var tokens struct {
		Input      *int64 `json:"prompt_tokens"`
		Output     *int64 `json:"completion_tokens"`
		Total      *int64 `json:"total_tokens"`
		CacheRead  *int64 `json:"cache_read_tokens"`
		CacheWrite *int64 `json:"cache_creation_tokens"`
		Reasoning  *int64 `json:"reasoning_tokens"`
	}
	if json.Unmarshal(raw, &tokens) != nil || tokens.Input == nil || tokens.Output == nil || tokens.Total == nil {
		return ModelUsage{}, false
	}
	usage := ModelUsage{Reported: 1, InputTokens: *tokens.Input, OutputTokens: *tokens.Output, TotalTokens: *tokens.Total}
	if tokens.CacheRead != nil {
		usage.CacheReadTokens = *tokens.CacheRead
	}
	if tokens.CacheWrite != nil {
		usage.CacheWriteTokens = *tokens.CacheWrite
	}
	if tokens.Reasoning != nil {
		usage.ReasoningTokens = *tokens.Reasoning
	}
	const maxTokens = 1_000_000_000_000
	for _, count := range []int64{usage.InputTokens, usage.OutputTokens, usage.TotalTokens, usage.CacheReadTokens, usage.CacheWriteTokens, usage.ReasoningTokens} {
		if count < 0 || count > maxTokens {
			return ModelUsage{}, false
		}
	}
	return usage, true
}
