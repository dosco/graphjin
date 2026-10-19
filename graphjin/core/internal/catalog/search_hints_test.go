package catalog

import (
	"fmt"
	"testing"
)

func TestQueryWithHintsExpandsRecallAndPreservesFilters(t *testing.T) {
	snapshot := Build(&MetadataSnapshot{
		Databases: []MetadataDatabase{{ID: "app", Name: "app", Type: "postgres"}},
		Tables: []MetadataTable{
			{ID: "app.public.customers", DatabaseName: "app", SchemaName: "public", TableName: "customers", Type: "table", TableKey: "app.public.customers"},
			{ID: "app.public.orders", DatabaseName: "app", SchemaName: "public", TableName: "orders", Type: "table", TableKey: "app.public.orders"},
		},
		Columns: []MetadataColumn{
			{ID: "app.public.customers.id", TableID: "app.public.customers", DatabaseName: "app", SchemaName: "public", TableName: "customers", ColumnName: "id", Type: "bigint", PrimaryKey: true},
		},
	}, nil)
	customerID := "table:app.public.customers"
	columnID := "column:app.public.customers.id"

	result, err := snapshot.QueryWithHints(Query{Search: "clients", Explain: true}, []CandidateHint{
		{CardID: customerID, Rank: 1, Weight: 0.7, Source: "semantic table recall", Why: "client means customer"},
	})
	if err != nil {
		t.Fatalf("query with hints: %v", err)
	}
	found := false
	for _, card := range result.Cards {
		found = found || card.ID == customerID
	}
	if !found {
		t.Fatalf("semantic recall did not return customer table: %+v", result.Cards)
	}
	if match := result.Matches[customerID]; match.Why == "" {
		t.Fatalf("expected semantic explanation, got %+v", match)
	}

	filtered, err := snapshot.QueryWithHints(Query{Search: "clients", Kind: "column"}, []CandidateHint{
		{CardID: customerID, Rank: 1, Weight: 0.7},
		{CardID: columnID, Rank: 2, Weight: 0.7},
	})
	if err != nil {
		t.Fatalf("filtered query: %v", err)
	}
	if len(filtered.Cards) != 1 || filtered.Cards[0].ID != columnID {
		t.Fatalf("kind filter leaked table candidate: %+v", filtered.Cards)
	}
}

func TestQueryWithHintsPinsExactIdentifierAndHonorsExplicitOrder(t *testing.T) {
	snapshot := Build(&MetadataSnapshot{
		Tables: []MetadataTable{
			{ID: "app.public.customers", DatabaseName: "app", SchemaName: "public", TableName: "customers", Type: "table"},
			{ID: "app.public.customer_events", DatabaseName: "app", SchemaName: "public", TableName: "customer_events", Type: "table"},
		},
	}, nil)
	customers := "table:app.public.customers"
	events := "table:app.public.customer_events"

	ranked, err := snapshot.QueryWithHints(Query{Search: "customers"}, []CandidateHint{{CardID: events, Rank: 1, Weight: 100}})
	if err != nil {
		t.Fatal(err)
	}
	if len(ranked.Cards) == 0 || ranked.Cards[0].ID != customers {
		t.Fatalf("exact identifier was not pinned: %+v", ranked.Cards)
	}
	paged, err := snapshot.QueryWithHints(Query{Search: "customers", Limit: 1, Offset: 1}, []CandidateHint{{CardID: events, Rank: 1, Weight: 100}})
	if err != nil {
		t.Fatal(err)
	}
	if len(paged.Cards) != 1 || paged.Cards[0].ID != events {
		t.Fatalf("hybrid offset did not advance to the second result: %+v", paged.Cards)
	}

	ordered, err := snapshot.QueryWithHints(Query{Search: "customers", Kind: "table", OrderBy: map[string]string{"title": "desc"}}, []CandidateHint{{CardID: events, Rank: 1, Weight: 0.7}})
	if err != nil {
		t.Fatal(err)
	}
	if len(ordered.Cards) < 2 || ordered.Cards[0].ID != customers {
		t.Fatalf("explicit title ordering not applied to candidate union: %+v", ordered.Cards)
	}
}

func BenchmarkCatalogPostingsWideSchema(b *testing.B) {
	const (
		tableCount  = 1000
		columnsEach = 300
	)
	snapshot := &Snapshot{Cards: make([]Card, 0, tableCount*(columnsEach+1))}
	for tableIndex := 0; tableIndex < tableCount; tableIndex++ {
		tableName := fmt.Sprintf("table%04d", tableIndex)
		snapshot.Cards = append(snapshot.Cards, Card{ID: "table:" + tableName, Kind: "table", TableName: tableName, Title: tableName})
		for columnIndex := 0; columnIndex < columnsEach; columnIndex++ {
			columnName := fmt.Sprintf("field%03d", columnIndex)
			snapshot.Cards = append(snapshot.Cards, Card{
				ID: "column:" + tableName + "." + columnName, Kind: "column",
				TableName: tableName, ColumnName: columnName, Title: columnName,
			})
		}
	}
	index := newSearchIndex(snapshot)
	snapshot.search = index
	query := "table0999 field299"
	candidates, narrowed := index.candidateIDs(query)
	if !narrowed {
		b.Fatal("wide-schema lookup fell back to a full-card scan")
	}
	if len(candidates) >= len(snapshot.Cards)/100 {
		b.Fatalf("postings returned %d of %d cards", len(candidates), len(snapshot.Cards))
	}
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		result, err := snapshot.Query(Query{Search: query, Limit: 20})
		if err != nil || len(result.Cards) == 0 {
			b.Fatalf("candidate query: cards=%d err=%v", len(result.Cards), err)
		}
	}
	b.ReportMetric(float64(len(candidates)), "candidates/op")
	b.ReportMetric(float64(len(snapshot.Cards)), "cards")
}
