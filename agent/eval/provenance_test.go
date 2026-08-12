package eval

import (
	"strings"
	"testing"
)

// Provenance is how a consumer walks a task back to the catalog item it was
// generated from. Column-derived tasks used to record table:<table>:<column>,
// which reads like a card id and resolves to nothing: query_catalog returned an
// empty detail, and an agent that trusted provenance was then refused for
// authoring GraphQL without discovery. These tests pin that every generated
// source_id names a card the catalog actually publishes.
func TestGeneratedProvenanceNamesResolvableCards(t *testing.T) {
	// The table card's own detail lists its columns, and the generator walks that
	// before it reaches the column cards. A merge that keeps the first occurrence
	// and discards the later card id is how provenance lost its ids in the first
	// place, so the table detail here carries the same columns.
	rows := []CatalogRow{
		{ID: "table:app:main.subscriptions", Kind: "table", Name: "subscriptions", TableName: "subscriptions",
			DetailsJSON: map[string]any{"complete": true, "columns": []any{
				map[string]any{"column_name": "id", "type": "integer", "primary_key": true},
				map[string]any{"column_name": "mrr_cents", "type": "integer"},
				map[string]any{"column_name": "started_at", "type": "timestamp"},
				map[string]any{"column_name": "name", "type": "text"},
			}}},
		{ID: "column:app:main.subscriptions.id", Kind: "column", TableName: "subscriptions", ColumnName: "id",
			DetailsJSON: map[string]any{"type": "integer", "primary_key": true}},
		{ID: "column:app:main.subscriptions.mrr_cents", Kind: "column", TableName: "subscriptions", ColumnName: "mrr_cents",
			DetailsJSON: map[string]any{"type": "integer"}},
		{ID: "column:app:main.subscriptions.started_at", Kind: "column", TableName: "subscriptions", ColumnName: "started_at",
			DetailsJSON: map[string]any{"type": "timestamp"}},
		{ID: "column:app:main.subscriptions.name", Kind: "column", TableName: "subscriptions", ColumnName: "name",
			DetailsJSON: map[string]any{"type": "text"}},
	}
	published := map[string]bool{}
	for _, row := range rows {
		published[row.ID] = true
	}

	tasks := generateCatalogCandidates(CatalogSnapshot{Rows: rows}, 23)
	if len(tasks) == 0 {
		t.Fatal("no catalog-derived tasks generated")
	}

	checked := 0
	for _, task := range tasks {
		if task.Provenance.Source != "catalog-entity" {
			continue
		}
		id := task.Provenance.SourceID
		checked++
		if !published[id] {
			t.Errorf("task %s records source_id %q, which is not a published card id", task.Slug, id)
		}
	}
	if checked == 0 {
		t.Fatal("no catalog-entity provenance to check")
	}
}

// TestGeneratedSlugsStayDistinctPerColumn guards the reason the composite string
// existed: slugs must still separate one column's task from another's, now that
// provenance no longer carries the composite.
func TestGeneratedSlugsStayDistinctPerColumn(t *testing.T) {
	rows := []CatalogRow{
		{ID: "table:app:main.subscriptions", Kind: "table", Name: "subscriptions", TableName: "subscriptions"},
		{ID: "column:app:main.subscriptions.id", Kind: "column", TableName: "subscriptions", ColumnName: "id",
			DetailsJSON: map[string]any{"type": "integer", "primary_key": true}},
		{ID: "column:app:main.subscriptions.mrr_cents", Kind: "column", TableName: "subscriptions", ColumnName: "mrr_cents",
			DetailsJSON: map[string]any{"type": "integer"}},
		{ID: "column:app:main.subscriptions.seat_count", Kind: "column", TableName: "subscriptions", ColumnName: "seat_count",
			DetailsJSON: map[string]any{"type": "integer"}},
	}
	seen := map[string]string{}
	for _, task := range generateCatalogCandidates(CatalogSnapshot{Rows: rows}, 23) {
		if prior, ok := seen[task.Slug]; ok && prior != task.Prompt {
			t.Fatalf("slug %q reused across different prompts: %q vs %q", task.Slug, prior, task.Prompt)
		}
		seen[task.Slug] = task.Prompt
	}
	var mrr, seat int
	for slug := range seen {
		if strings.Contains(slug, "mrr-cents") || strings.Contains(slug, "mrr_cents") {
			mrr++
		}
		if strings.Contains(slug, "seat-count") || strings.Contains(slug, "seat_count") {
			seat++
		}
	}
	if mrr == 0 || seat == 0 {
		t.Fatalf("slugs must still name their column: mrr=%d seat=%d", mrr, seat)
	}
}
