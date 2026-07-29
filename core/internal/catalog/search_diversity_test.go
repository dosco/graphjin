package catalog

import (
	"fmt"
	"testing"
)

func TestQueryReservesKindDiversityAndReportsFullPoolFacets(t *testing.T) {
	snapshot := junkHeavyCatalogSnapshot()

	ranked, err := snapshot.Query(Query{Search: "orders", Limit: 10, Explain: true})
	if err != nil {
		t.Fatal(err)
	}
	unreserved, err := snapshot.Query(Query{
		Search: "orders", Limit: 10, Explain: true,
		OrderBy: map[string]string{"score": "desc"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ranked.Cards) != 10 || len(unreserved.Cards) != 10 {
		t.Fatalf("card counts = reserved:%d unreserved:%d, want 10 each", len(ranked.Cards), len(unreserved.Cards))
	}
	if ranked.Cards[0].ID != unreserved.Cards[0].ID {
		t.Fatalf("top card changed: reserved=%s unreserved=%s", ranked.Cards[0].ID, unreserved.Cards[0].ID)
	}
	if !catalogResultHasKind(ranked, "table") {
		t.Fatalf("reserved page has no table card: %+v", ranked.Cards)
	}
	if catalogResultHasKind(unreserved, "table") {
		t.Fatalf("explicit order_by must skip reservation: %+v", unreserved.Cards)
	}
	if ranked.Facets["saved_query"] != 100 || ranked.Facets["table"] != 1 || ranked.Facets["relationship"] != 1 {
		t.Fatalf("facets = %+v, want full filtered pool counts", ranked.Facets)
	}
}

func TestQueryKindReservationSkipsOffsetPages(t *testing.T) {
	snapshot := junkHeavyCatalogSnapshot()
	result, err := snapshot.Query(Query{Search: "orders", Limit: 10, Offset: 1})
	if err != nil {
		t.Fatal(err)
	}
	if catalogResultHasKind(result, "table") || catalogResultHasKind(result, "relationship") {
		t.Fatalf("offset page must preserve the original ranking without re-reservation: %+v", result.Cards)
	}
	if result.Facets["saved_query"] != 100 || result.Facets["table"] != 1 {
		t.Fatalf("offset facets = %+v, want full filtered pool counts", result.Facets)
	}
}

func TestQueryWithHintsReservesKindDiversity(t *testing.T) {
	snapshot := junkHeavyCatalogSnapshot()
	result, err := snapshot.QueryWithHints(Query{Search: "orders", Limit: 10}, []CandidateHint{
		{CardID: "table:app.public.production_orders", Rank: 1, Weight: 0.1, Source: "semantic"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Cards) != 10 || !catalogResultHasKind(result, "table") {
		t.Fatalf("hybrid reserved page = %+v, want 10 cards including a table", result.Cards)
	}
	if result.Facets["saved_query"] != 100 || result.Facets["table"] != 1 || result.Facets["relationship"] != 1 {
		t.Fatalf("hybrid facets = %+v, want full fused pool counts", result.Facets)
	}
}

func junkHeavyCatalogSnapshot() *Snapshot {
	cards := make([]Card, 0, 102)
	for index := 0; index < 100; index++ {
		cards = append(cards, Card{
			ID:      fmt.Sprintf("saved_query:orders_%03d", index),
			Kind:    "saved_query",
			Title:   "orders",
			Summary: "Read orders for a generated watch probe.",
		})
	}
	cards = append(cards,
		Card{
			ID:        "table:app.public.production_orders",
			Kind:      "table",
			Title:     "app:public.production_orders",
			Summary:   "Committed production orders and shipment quantities.",
			TableName: "production_orders",
		},
		Card{
			ID:      "relationship:orders_to_shipments",
			Kind:    "relationship",
			Title:   "production orders to committed shipments",
			Summary: "Relationship between orders and shipments.",
		},
	)
	return &Snapshot{Cards: cards}
}

func catalogResultHasKind(result QueryResult, kind string) bool {
	for _, card := range result.Cards {
		if card.Kind == kind {
			return true
		}
	}
	return false
}
