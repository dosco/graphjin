package catalog

import (
	"testing"

	"github.com/dosco/graphjin/core/v3/sourcecap"
)

// Regression: ordering a catalog search by search_rank (the canonical rank
// column exposed by the nanoDB projection and used by MCP query_catalog's
// order_by) must behave like ordering by score, not error. A rejected
// order_by field makes Snapshot.Query fail, and the control plane then
// silently falls back to a raw where-filter that drops the search matches.
func TestQueryOrderBySearchRankMatchesScore(t *testing.T) {
	snap := BuildWithOptions(&MetadataSnapshot{}, nil, BuildOptions{
		Sources: []Source{
			{Name: "app", Kind: sourcecap.KindDatabase, Type: "sqlite", Default: true},
			{Name: "business_code", Kind: sourcecap.KindCode, ReadOnly: true},
		},
	})

	plain, err := snap.Query(Query{Search: "code symbols"})
	if err != nil {
		t.Fatalf("unordered search: %v", err)
	}
	if len(plain.Cards) == 0 {
		t.Fatalf("unordered search returned no cards")
	}

	for _, field := range []string{"search_rank", "score"} {
		ranked, err := snap.Query(Query{Search: "code symbols", OrderBy: map[string]string{field: "desc"}})
		if err != nil {
			t.Fatalf("order_by %q returned error: %v", field, err)
		}
		if len(ranked.Cards) != len(plain.Cards) {
			t.Fatalf("order_by %q returned %d cards, want %d (same as unordered)", field, len(ranked.Cards), len(plain.Cards))
		}
	}
}
