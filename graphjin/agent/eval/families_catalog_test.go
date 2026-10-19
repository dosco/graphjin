package eval

import (
	"reflect"
	"testing"
)

func columnRow(table, column string, examples any) CatalogRow {
	return CatalogRow{
		ID: "column:" + table + "." + column, Kind: "column",
		TableName: table, ColumnName: column, ExamplesJSON: examples,
	}
}

// The catalog publishes the closed set and one quoted value together. Both are
// required: the quoted value is what proves the set was split correctly.
func TestObservedColumnValuesReadsPublishedClosedSet(t *testing.T) {
	row := columnRow("accounts", "status", `["where: { status: { eq: \"active\" } }","status values: active, trial, churned"]`)
	got := observedColumnValues(row)
	want := []string{"active", "trial", "churned"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestObservedColumnValuesAcceptsDecodedPayload(t *testing.T) {
	row := columnRow("accounts", "status", []any{
		`where: { status: { eq: "active" } }`,
		"status values: active, trial",
	})
	if got := observedColumnValues(row); !reflect.DeepEqual(got, []string{"active", "trial"}) {
		t.Fatalf("expected the decoded payload to parse, got %v", got)
	}
}

// A value containing the ", " separator cannot be told apart from two values.
// The quoted cross-check catches it and the whole set is refused, because a
// filter built on half a value would assert something untrue about the data.
func TestObservedColumnValuesRefusesSetWhenSeparatorIsAmbiguous(t *testing.T) {
	row := columnRow("accounts", "plan", []any{
		`where: { plan: { eq: "pro, annual" } }`,
		"plan values: pro, annual, starter",
	})
	if got := observedColumnValues(row); got != nil {
		t.Fatalf("expected an ambiguous set to be refused, got %v", got)
	}
}

func TestObservedColumnValuesRequiresTheQuotedCrossCheck(t *testing.T) {
	row := columnRow("accounts", "status", []any{"status values: active, trial"})
	if got := observedColumnValues(row); got != nil {
		t.Fatalf("expected a set with no quoted example to be refused, got %v", got)
	}
}

func TestObservedColumnValuesIgnoresAnotherColumnsSet(t *testing.T) {
	row := columnRow("accounts", "status", []any{
		`where: { plan: { eq: "pro" } }`,
		"plan values: pro, starter",
	})
	if got := observedColumnValues(row); got != nil {
		t.Fatalf("expected another column's set to be ignored, got %v", got)
	}
}

func TestObservedColumnValuesIgnoresNonClosedSetExamples(t *testing.T) {
	for _, examples := range []any{
		[]any{`where: { created_at: { gte: $from, lt: $to } }`},
		[]any{`{ accounts(limit: 10) { note } }`},
		[]any{`where: { status: { eq: "<status>" } }`},
		nil,
	} {
		if got := observedColumnValues(columnRow("accounts", "note", examples)); got != nil {
			t.Fatalf("expected no closed set for %v, got %v", examples, got)
		}
	}
}

func relTables() []generatorTable {
	return []generatorTable{
		{Name: "invoices", ID: "table:invoices", PrimaryKey: "id", Columns: []generatorColumn{
			{Name: "id"}, {Name: "account_id"}, {Name: "amount_cents"},
		}},
		{Name: "accounts", ID: "table:accounts", PrimaryKey: "id", Columns: []generatorColumn{
			{Name: "id"}, {Name: "name"},
		}},
	}
}

func relRow(id, title, table, column string) CatalogRow {
	return CatalogRow{ID: id, Kind: "relationship", Title: title, TableName: table, ColumnName: column}
}

func TestCatalogRelationshipsReadsPublishedEdge(t *testing.T) {
	rows := []CatalogRow{relRow("rel:1", "invoices.account_id -> accounts.id", "invoices", "account_id")}
	got := catalogRelationships(rows, relTables())
	if len(got) != 1 {
		t.Fatalf("expected 1 relationship, got %d", len(got))
	}
	want := generatorRelationship{
		ID: "rel:1", FromTable: "invoices", FromColumn: "account_id",
		ToTable: "accounts", ToColumn: "id", FromTableID: "table:invoices",
	}
	if got[0] != want {
		t.Fatalf("expected %+v, got %+v", want, got[0])
	}
}

// The structured fields are what the card claims to be about. A title that
// disagrees means the format drifted, and reinterpreting it would silently
// generate tasks about a different edge.
func TestCatalogRelationshipsDropsTitleDisagreeingWithStructuredFields(t *testing.T) {
	rows := []CatalogRow{relRow("rel:1", "payments.account_id -> accounts.id", "invoices", "account_id")}
	if got := catalogRelationships(rows, relTables()); len(got) != 0 {
		t.Fatalf("expected the mismatched card to be dropped, got %+v", got)
	}
	rows = []CatalogRow{relRow("rel:2", "invoices.customer_id -> accounts.id", "invoices", "account_id")}
	if got := catalogRelationships(rows, relTables()); len(got) != 0 {
		t.Fatalf("expected the mismatched column to be dropped, got %+v", got)
	}
}

func TestCatalogRelationshipsDropsUnknownAndPartialTables(t *testing.T) {
	cases := map[string]CatalogRow{
		"unknown referenced table":  relRow("rel:1", "invoices.supplier_id -> suppliers.id", "invoices", "supplier_id"),
		"unknown referencing table": relRow("rel:2", "shipments.account_id -> accounts.id", "shipments", "account_id"),
		"column absent from table":  relRow("rel:3", "invoices.owner_id -> accounts.id", "invoices", "owner_id"),
		"self reference":            relRow("rel:4", "invoices.id -> invoices.id", "invoices", "id"),
	}
	for name, row := range cases {
		if got := catalogRelationships([]CatalogRow{row}, relTables()); len(got) != 0 {
			t.Fatalf("%s: expected the edge to be dropped, got %+v", name, got)
		}
	}
}

func TestCatalogRelationshipsDropsMalformedTitles(t *testing.T) {
	for _, title := range []string{
		"invoices account_id accounts id",
		"invoices.account_id => accounts.id",
		"Relationship discovered from database metadata.",
		"",
	} {
		row := relRow("rel:x", title, "invoices", "account_id")
		if got := catalogRelationships([]CatalogRow{row}, relTables()); len(got) != 0 {
			t.Fatalf("title %q: expected no relationship, got %+v", title, got)
		}
	}
}

func TestCatalogRelationshipsDedupesRepeatedEdges(t *testing.T) {
	rows := []CatalogRow{
		relRow("rel:1", "invoices.account_id -> accounts.id", "invoices", "account_id"),
		relRow("rel:2", "invoices.account_id -> accounts.id", "invoices", "account_id"),
	}
	if got := catalogRelationships(rows, relTables()); len(got) != 1 {
		t.Fatalf("expected the duplicate edge to collapse, got %d", len(got))
	}
}
