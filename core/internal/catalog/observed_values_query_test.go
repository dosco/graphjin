package catalog

import (
	"encoding/json"
	"strings"
	"testing"
)

// The agent's write-time value check reads a column's sampled values by asking the
// catalog for that table's columns. It found nothing across three full benchmark
// runs while the values were demonstrably present on other cards, and each guess at
// the cause was wrong: the card id was fine, the argument types were fine, and both
// catalog read paths project evidence_json.
//
// This pins the contract the check actually depends on, end to end: a sampled column
// publishes observed_values, and a kind+table filter returns that card with them
// intact.

func snapshotWithObservedValues(t *testing.T) *Snapshot {
	t.Helper()
	cols := []MetadataColumn{
		{
			ID: "app:main.support_tickets.id", TableID: "app:main.support_tickets",
			DatabaseName: "app", SchemaName: "main", TableName: "support_tickets",
			ColumnName: "id", Type: "bigint", PrimaryKey: true, Ordinal: 0,
		},
		{
			ID: "app:main.support_tickets.status", TableID: "app:main.support_tickets",
			DatabaseName: "app", SchemaName: "main", TableName: "support_tickets",
			ColumnName: "status", Type: "text", Ordinal: 1,
		},
		{
			ID: "app:main.support_tickets.resolution_note", TableID: "app:main.support_tickets",
			DatabaseName: "app", SchemaName: "main", TableName: "support_tickets",
			ColumnName: "resolution_note", Type: "text", Ordinal: 2,
		},
	}
	return BuildWithOptions(&MetadataSnapshot{
		Databases: []MetadataDatabase{{ID: "app", Name: "app", Type: "sqlite"}},
		Tables: []MetadataTable{{
			ID: "app:main.support_tickets", DatabaseName: "app", SchemaName: "main",
			TableName: "support_tickets", ColumnCount: len(cols), PrimaryKey: "id",
		}},
		Columns: cols,
	}, nil, BuildOptions{
		ObservedColumnValues: map[string][]string{
			"column:app:main.support_tickets.status": {"open", "pending", "resolved"},
		},
	})
}

func TestObservedValuesReachTheColumnCard(t *testing.T) {
	snap := snapshotWithObservedValues(t)
	card, ok := findCatalogCard(snap, "column:app:main.support_tickets.status")
	if !ok {
		t.Fatal("status column card missing")
	}

	var evidence map[string]any
	if err := json.Unmarshal([]byte(card.EvidenceJSON), &evidence); err != nil {
		t.Fatalf("evidence_json is not an object: %v", err)
	}
	values, _ := evidence["observed_values"].([]any)
	if len(values) != 3 {
		t.Fatalf("observed_values = %v, want the three sampled statuses", evidence["observed_values"])
	}
	// The example has to name a real value; a placeholder is what sent the agent
	// looking for one.
	if !strings.Contains(card.ExamplesJSON, "open") {
		t.Errorf("examples should use a sampled value, got %s", card.ExamplesJSON)
	}

	// A column with no sampled set must stay clean rather than carry an empty key.
	plain, ok := findCatalogCard(snap, "column:app:main.support_tickets.resolution_note")
	if !ok {
		t.Fatal("resolution_note column card missing")
	}
	if strings.Contains(plain.EvidenceJSON, "observed_values") {
		t.Errorf("unsampled column must not carry observed_values: %s", plain.EvidenceJSON)
	}
}

// TestObservedValuesSurviveAKindAndTableFilter is the retrieval half. The agent
// cannot fetch a column card by id without already knowing the id, so it filters by
// kind and table; if that filter drops evidence_json or the card, the check goes
// quiet with no error.
func TestObservedValuesSurviveAKindAndTableFilter(t *testing.T) {
	snap := snapshotWithObservedValues(t)
	result, err := snap.Query(Query{Where: map[string]any{
		"kind":       map[string]any{"eq": "column"},
		"table_name": map[string]any{"eq": "support_tickets"},
	}})
	if err != nil {
		t.Fatalf("filtered catalog query failed: %v", err)
	}
	if len(result.Cards) == 0 {
		t.Fatal("a kind+table filter returned no column cards")
	}

	var found bool
	for _, card := range result.Cards {
		if card.ColumnName != "status" {
			continue
		}
		found = true
		if !strings.Contains(card.EvidenceJSON, "observed_values") {
			t.Fatalf("the filtered card lost its observed values: %s", card.EvidenceJSON)
		}
		if card.ID == "" {
			t.Error("the filtered card must carry its id; the value lookup reads the column from it")
		}
	}
	if !found {
		var ids []string
		for _, card := range result.Cards {
			ids = append(ids, card.ID)
		}
		t.Fatalf("status column absent from the filtered result: %v", ids)
	}
}
