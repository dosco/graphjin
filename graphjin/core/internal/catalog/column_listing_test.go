package catalog

import (
	"encoding/json"
	"strings"
	"testing"
)

// Benchmark generation 2028.1 found every cross-source episode answering the
// numeric half of a two-part question and guessing the qualitative half. The
// account_health API join publishes four synthesised columns; only account_id (a
// foreign key) and open_risk_count (a metric) matched the key-column heuristics,
// so the card listed the count and hid the health value. Agents then invented
// health_score, health_color, and status. These tests pin the two properties that
// close it: a narrow table lists every column, and a truncated list says so.

func columnFor(table, name, colType string) MetadataColumn {
	return MetadataColumn{
		ID:           "app:main." + table + "." + name,
		TableID:      "app:main." + table,
		DatabaseName: "app",
		SchemaName:   "main",
		TableName:    table,
		ColumnName:   name,
		Type:         colType,
	}
}

func apiJoinSnapshot() *MetadataSnapshot {
	cols := []MetadataColumn{
		columnFor("account_health", "account_id", "integer"),
		columnFor("account_health", "health", "text"),
		columnFor("account_health", "executive_owner", "text"),
		columnFor("account_health", "open_risk_count", "integer"),
	}
	for i := range cols {
		cols[i].Ordinal = i
	}
	return &MetadataSnapshot{
		Databases: []MetadataDatabase{{ID: "app", Name: "app", Type: "postgres"}},
		Tables: []MetadataTable{{
			ID: "app:main.account_health", DatabaseName: "app", SchemaName: "main",
			TableName: "account_health", Type: "remote", ColumnCount: len(cols),
		}},
		Columns: cols,
	}
}

func columnListDetail(t *testing.T, snap *Snapshot, cardID string) map[string]any {
	t.Helper()
	for _, d := range snap.Details {
		if d.CardID == cardID && d.Section == "key_columns" {
			var out map[string]any
			if err := json.Unmarshal([]byte(d.DataJSON), &out); err != nil {
				t.Fatalf("column detail is not an object: %v", err)
			}
			out["_content"] = d.Content
			return out
		}
	}
	t.Fatalf("no key_columns detail for %s", cardID)
	return nil
}

func listedColumnNames(t *testing.T, detail map[string]any) []string {
	t.Helper()
	raw, _ := json.Marshal(detail["columns"])
	var cols []MetadataColumn
	if err := json.Unmarshal(raw, &cols); err != nil {
		t.Fatalf("columns payload: %v", err)
	}
	names := make([]string, 0, len(cols))
	for _, c := range cols {
		names = append(names, c.ColumnName)
	}
	return names
}

func TestNarrowTableListsEveryColumn(t *testing.T) {
	snap := Build(apiJoinSnapshot(), nil)
	detail := columnListDetail(t, snap, "table:app:main.account_health")

	got := strings.Join(listedColumnNames(t, detail), ",")
	// health and executive_owner match no heuristic; the whole point is that a
	// four-column table has no reason to filter them out.
	if want := "account_id,health,executive_owner,open_risk_count"; got != want {
		t.Fatalf("listed columns = %s, want %s", got, want)
	}
	if complete, _ := detail["complete"].(bool); !complete {
		t.Fatal("a fully listed table must report complete")
	}
	if via, _ := detail["remaining_via"].(string); via != "" {
		t.Fatalf("a complete list needs no remaining_via, got %q", via)
	}
	if content, _ := detail["_content"].(string); !strings.Contains(content, "complete set") {
		t.Fatalf("content must state the list is complete: %q", content)
	}

	card, ok := findCatalogCard(snap, "table:app:main.account_health")
	if !ok {
		t.Fatal("table card missing")
	}
	// The summary is what search returns; calling a complete list "key columns"
	// implies columns were withheld.
	if !strings.Contains(card.Summary, "columns: account_id, health") || strings.Contains(card.Summary, "key columns") {
		t.Fatalf("summary should label a complete list plainly: %q", card.Summary)
	}
}

// TestWideTableDeclaresTruncation keeps the summarisation behaviour for tables
// that genuinely need it, while making the omission legible and recoverable.
func TestWideTableDeclaresTruncation(t *testing.T) {
	var cols []MetadataColumn
	cols = append(cols, columnFor("events", "id", "integer"))
	cols[0].PrimaryKey = true
	cols = append(cols, columnFor("events", "status", "text"))
	cols = append(cols, columnFor("events", "created_at", "timestamp"))
	for i := 0; i < 14; i++ {
		cols = append(cols, columnFor("events", "note_"+string(rune('a'+i)), "text"))
	}
	for i := range cols {
		cols[i].Ordinal = i
	}
	snap := Build(&MetadataSnapshot{
		Databases: []MetadataDatabase{{ID: "app", Name: "app", Type: "postgres"}},
		Tables: []MetadataTable{{
			ID: "app:main.events", DatabaseName: "app", SchemaName: "main",
			TableName: "events", ColumnCount: len(cols), PrimaryKey: "id",
		}},
		Columns: cols,
	}, nil)

	detail := columnListDetail(t, snap, "table:app:main.events")
	if complete, _ := detail["complete"].(bool); complete {
		t.Fatal("a table with 17 columns and 3 interesting ones must not report complete")
	}
	if total, _ := detail["total_columns"].(float64); int(total) != len(cols) {
		t.Fatalf("total_columns = %v, want %d", detail["total_columns"], len(cols))
	}
	if listed, _ := detail["listed_columns"].(float64); int(listed) >= len(cols) || listed == 0 {
		t.Fatalf("listed_columns = %v, want a nonzero subset of %d", detail["listed_columns"], len(cols))
	}
	via, _ := detail["remaining_via"].(string)
	if !strings.Contains(via, `kind: { eq: "column" }`) || !strings.Contains(via, `"events"`) {
		t.Fatalf("remaining_via must name the call returning every column, got %q", via)
	}
	if content, _ := detail["_content"].(string); !strings.Contains(content, "subset") {
		t.Fatalf("content must admit the list is partial: %q", content)
	}
}
