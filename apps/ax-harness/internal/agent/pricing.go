package agent

import "github.com/open-neko/openneko/apps/ax-harness/internal/catalog"

// CostSummary is the admission charge in USD micros at Ax catalog rates.
// Missing provider usage retains its reservation. It is an estimate, not a
// provider invoice.
type CostSummary struct {
	CatalogVersion string `json:"catalog_version"`
	ChargedMicros  int64  `json:"charged_micros"`
	BudgetMicros   int64  `json:"budget_micros"`
}

const (
	pricedByCatalog = "catalog"
	unpriced        = "unpriced"
)

// price sets the catalog cost of one reported call.
func (u *ModelUsage) price(rates catalog.Rates, ok bool, version string) {
	u.CatalogVersion = version
	if !ok {
		u.PricedBy = unpriced
		return
	}
	u.PricedBy = pricedByCatalog
	u.priced = 1
	u.CostMicros = rates.Cost(catalog.Tokens{Input: u.InputTokens, Output: u.OutputTokens, Total: u.TotalTokens,
		CacheRead: u.CacheReadTokens, CacheWrite: u.CacheWriteTokens})
}
