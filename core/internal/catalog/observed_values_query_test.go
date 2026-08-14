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

// TestRemoteJoinEdgeAttachesToTables pins where the join route is published. The
// edge used to hang off the synthetic key column — a node that is deliberately
// never created — so it dangled, no table detail carried it, and the only card
// naming the route was the relationship card nothing routinely fetches. The live
// integration probe found account_health's edges holding has_column entries only.
func TestRemoteJoinEdgeAttachesToTables(t *testing.T) {
	snap := Build(&MetadataSnapshot{
		Databases: []MetadataDatabase{{ID: "app", Name: "app", Type: "sqlite"}},
		Tables: []MetadataTable{
			{ID: "app:main.accounts", DatabaseName: "app", SchemaName: "main", TableName: "accounts", ColumnCount: 1},
			{ID: "app:main.account_health", DatabaseName: "app", SchemaName: "main", TableName: "account_health", ColumnCount: 1, Type: "remote"},
		},
		Relationships: []MetadataRelationship{{
			ID:            "app:main.account_health.__account_health_id->app:main.accounts.id",
			FromTableName: "account_health",
			FromColumnID:  "app:main.account_health.__account_health_id",
			ToTableName:   "accounts",
			ToColumnID:    "app:main.accounts.id",
			Source:        "remote_join",
		}, {
			ID:            "app:main.invoices.account_id->app:main.accounts.id",
			FromTableName: "invoices",
			FromColumnID:  "app:main.invoices.account_id",
			ToTableName:   "accounts",
			ToColumnID:    "app:main.accounts.id",
			Source:        "foreign_key",
		}},
	}, nil)

	var joinEdge, fkEdge *Edge
	for i := range snap.Edges {
		switch {
		case snap.Edges[i].Kind == "served_under":
			joinEdge = &snap.Edges[i]
		case snap.Edges[i].Kind == "references":
			fkEdge = &snap.Edges[i]
		}
	}
	if joinEdge == nil {
		t.Fatalf("no served_under edge emitted: %+v", snap.Edges)
	}
	if joinEdge.FromID != "node:table:app:main.account_health" || joinEdge.ToID != "node:table:app:main.accounts" {
		t.Fatalf("join edge must connect table nodes, got %s -> %s", joinEdge.FromID, joinEdge.ToID)
	}
	for _, want := range []string{"nested under accounts", "accounts { account_health"} {
		if !strings.Contains(joinEdge.Summary, want) {
			t.Fatalf("join edge summary must teach the route, missing %q: %s", want, joinEdge.Summary)
		}
	}
	// Ordinary foreign keys keep their column-level shape untouched.
	if fkEdge == nil || fkEdge.FromID != "node:column:app:main.invoices.account_id" {
		t.Fatalf("foreign-key edge changed shape: %+v", fkEdge)
	}
}

// TestTableExamplesForFilesystemTables pins the example a model actually copies.
// The generic form led with an id column filesystem tables do not have and never
// showed the content read; episodes show models copying it verbatim.
func TestTableExamplesForFilesystemTables(t *testing.T) {
	fsCols := []MetadataColumn{}
	for i, name := range []string{"key", "size", "content_type", "etag", "modified_at", "url", "data", "text"} {
		fsCols = append(fsCols, MetadataColumn{
			ID: "app:main.sla_policies." + name, TableID: "app:main.sla_policies",
			DatabaseName: "app", SchemaName: "main", TableName: "sla_policies",
			ColumnName: name, Type: "text", Ordinal: i,
		})
	}
	snap := Build(&MetadataSnapshot{
		Databases: []MetadataDatabase{{ID: "app", Name: "app", Type: "sqlite"}},
		Tables: []MetadataTable{{
			ID: "app:main.sla_policies", DatabaseName: "app", SchemaName: "main",
			TableName: "sla_policies", ColumnCount: len(fsCols), Type: "remote",
		}},
		Columns: fsCols,
	}, nil)
	card, ok := findCatalogCard(snap, "table:app:main.sla_policies")
	if !ok {
		t.Fatal("sla_policies card missing")
	}
	// The JSON encoder HTML-escapes angle brackets, so <key> arrives as
	// \u003ckey\u003e; assert on the stable fragments.
	for _, want := range []string{"(key:", "u003ckey", "text data", "prefix:"} {
		if !strings.Contains(card.ExamplesJSON, want) {
			t.Fatalf("filesystem example must teach the content read, missing %q: %s", want, card.ExamplesJSON)
		}
	}
	if strings.Contains(card.ExamplesJSON, "id") && !strings.Contains(card.ExamplesJSON, "modified_at") {
		t.Fatalf("filesystem example must not invent columns: %s", card.ExamplesJSON)
	}
	if strings.Contains(card.ExamplesJSON, "{ id ") {
		t.Fatalf("filesystem example must not lead with a nonexistent id: %s", card.ExamplesJSON)
	}

	// An API-join remote (spec-derived columns, no key/url/data) keeps the
	// generic shape without an invented id.
	apiCols := []MetadataColumn{}
	for i, name := range []string{"account_id", "health", "executive_owner", "open_risk_count"} {
		apiCols = append(apiCols, MetadataColumn{
			ID: "app:main.account_health." + name, TableID: "app:main.account_health",
			DatabaseName: "app", SchemaName: "main", TableName: "account_health",
			ColumnName: name, Type: "text", Ordinal: i,
		})
	}
	snap = Build(&MetadataSnapshot{
		Databases: []MetadataDatabase{{ID: "app", Name: "app", Type: "sqlite"}},
		Tables: []MetadataTable{{
			ID: "app:main.account_health", DatabaseName: "app", SchemaName: "main",
			TableName: "account_health", ColumnCount: len(apiCols), Type: "remote",
		}},
		Columns: apiCols,
	}, nil)
	card, ok = findCatalogCard(snap, "table:app:main.account_health")
	if !ok {
		t.Fatal("account_health card missing")
	}
	if strings.Contains(card.ExamplesJSON, "id ") && !strings.Contains(card.ExamplesJSON, "account_id") {
		t.Fatalf("api-join example must not invent an id column: %s", card.ExamplesJSON)
	}
	if !strings.Contains(card.ExamplesJSON, "account_id") {
		t.Fatalf("api-join example should use real columns: %s", card.ExamplesJSON)
	}

	// A plain DB table with an id keeps the historical shape.
	dbCols := []MetadataColumn{
		{ID: "app:main.users.id", TableID: "app:main.users", DatabaseName: "app", SchemaName: "main",
			TableName: "users", ColumnName: "id", Type: "bigint", PrimaryKey: true, Ordinal: 0},
		{ID: "app:main.users.email", TableID: "app:main.users", DatabaseName: "app", SchemaName: "main",
			TableName: "users", ColumnName: "email", Type: "text", Ordinal: 1},
	}
	snap = Build(&MetadataSnapshot{
		Databases: []MetadataDatabase{{ID: "app", Name: "app", Type: "sqlite"}},
		Tables: []MetadataTable{{
			ID: "app:main.users", DatabaseName: "app", SchemaName: "main",
			TableName: "users", ColumnCount: len(dbCols), PrimaryKey: "id",
		}},
		Columns: dbCols,
	}, nil)
	card, ok = findCatalogCard(snap, "table:app:main.users")
	if !ok {
		t.Fatal("users card missing")
	}
	if !strings.Contains(card.ExamplesJSON, "id") || !strings.Contains(card.ExamplesJSON, "email") {
		t.Fatalf("db table example regressed: %s", card.ExamplesJSON)
	}
}
