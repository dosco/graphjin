package eval

import (
	"strings"
	"testing"
)

// evidence mirrors what a live column card actually carries: a JSON string of
// the column's metadata under Go field names, with the observed closed set
// alongside it when the catalog sampled one.
func evidenceRow(table, column, typeName string, notNull, primary bool, ordinal int, observed ...string) ImportRow {
	fields := map[string]any{
		"ColumnName": column, "TableName": table, "Type": typeName,
		"NotNull": notNull, "PrimaryKey": primary, "UniqueKey": false, "Ordinal": ordinal,
	}
	if len(observed) != 0 {
		values := make([]any, 0, len(observed))
		for _, value := range observed {
			values = append(values, value)
		}
		fields["observed_values"] = values
	}
	return ImportRow{
		CatalogRow:   CatalogRow{ID: "column:" + table + "." + column, Kind: "column", TableName: table, ColumnName: column},
		EvidenceJSON: fields,
	}
}

func tableRow(name string) ImportRow {
	return ImportRow{CatalogRow: CatalogRow{ID: "table:" + name, Kind: "table", TableName: name}}
}

func importFixture() []ImportRow {
	return []ImportRow{
		tableRow("accounts"),
		evidenceRow("accounts", "id", "integer", true, true, 0),
		evidenceRow("accounts", "name", "text", true, false, 1),
		evidenceRow("accounts", "plan", "text", true, false, 2, "enterprise", "growth", "starter"),
		evidenceRow("accounts", "mrr_cents", "integer", true, false, 3),
		tableRow("invoices"),
		evidenceRow("invoices", "id", "integer", true, true, 0),
		evidenceRow("invoices", "account_id", "integer", true, false, 1),
		evidenceRow("invoices", "status", "text", true, false, 2, "paid", "failed"),
		{CatalogRow: CatalogRow{
			ID: "relationship:invoices.account_id", Kind: "relationship",
			Title: "invoices.account_id -> accounts.id", TableName: "invoices", ColumnName: "account_id",
		}},
		{CatalogRow: CatalogRow{
			ID: "saved_query:active_mrr", Kind: "saved_query", Name: "active_mrr",
			DetailsJSON: map[string]any{"query": "query active_mrr { accounts { sum_mrr_cents } }"},
		}},
	}
}

func TestImportSchemaRebuildsTablesColumnsAndEdges(t *testing.T) {
	schema, report, err := ImportSchema(importFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(schema.Tables) != 2 {
		t.Fatalf("expected 2 tables, got %d (%+v)", len(schema.Tables), report.Drops)
	}
	accounts := schema.Tables[0]
	if accounts.Name != "accounts" || accounts.PrimaryKey != "id" {
		t.Fatalf("unexpected table %+v", accounts)
	}
	// Column order follows the database's own ordinal, so a clone's DDL reads
	// like the schema it came from rather than alphabetically shuffled.
	if accounts.Columns[0].Name != "id" || accounts.Columns[1].Name != "name" {
		t.Fatalf("columns are not in ordinal order: %+v", accounts.Columns)
	}
	var plan ImportedColumn
	for _, column := range accounts.Columns {
		if column.Name == "plan" {
			plan = column
		}
	}
	if len(plan.ObservedValues) != 3 || plan.ObservedValues[0] != "enterprise" {
		t.Fatalf("closed set did not survive: %+v", plan)
	}
	if !plan.NotNull {
		t.Fatal("not-null did not survive")
	}
	if len(schema.Relationships) != 1 || schema.Relationships[0].ToTable != "accounts" {
		t.Fatalf("relationship did not survive: %+v", schema.Relationships)
	}
	if len(schema.SavedQueries) != 1 || schema.SavedQueries[0].Name != "active_mrr" {
		t.Fatalf("saved query did not survive: %+v", schema.SavedQueries)
	}
}

// A table nobody can identify a row in is useless: every write task and every
// collateral check needs a key. Dropping it loudly beats cloning something the
// task families will silently skip.
func TestImportSchemaDropsTablesWithNoKey(t *testing.T) {
	rows := []ImportRow{
		tableRow("events"),
		evidenceRow("events", "payload", "text", false, false, 0),
		tableRow("accounts"),
		evidenceRow("accounts", "id", "integer", true, true, 0),
	}
	schema, report, err := ImportSchema(rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(schema.Tables) != 1 || schema.Tables[0].Name != "accounts" {
		t.Fatalf("keyless table was kept: %+v", schema.Tables)
	}
	var explained bool
	for _, drop := range report.Drops {
		if drop.ID == "events" && strings.Contains(drop.Reason, "primary key") {
			explained = true
		}
	}
	if !explained {
		t.Fatalf("the drop was not explained: %+v", report.Drops)
	}
}

// A column named id is a key in practice even when the catalog did not flag one.
func TestImportSchemaFallsBackToAnIdColumn(t *testing.T) {
	rows := []ImportRow{
		tableRow("notes"),
		evidenceRow("notes", "id", "integer", true, false, 0),
		evidenceRow("notes", "body", "text", false, false, 1),
	}
	schema, _, err := ImportSchema(rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(schema.Tables) != 1 || schema.Tables[0].PrimaryKey != "id" {
		t.Fatalf("expected the id fallback, got %+v", schema.Tables)
	}
}

func TestImportSchemaDropsColumnsWithNoType(t *testing.T) {
	rows := importFixture()
	rows = append(rows, ImportRow{
		CatalogRow:   CatalogRow{ID: "column:accounts.mystery", Kind: "column", TableName: "accounts", ColumnName: "mystery"},
		EvidenceJSON: map[string]any{"ColumnName": "mystery"},
	})
	schema, report, err := ImportSchema(rows)
	if err != nil {
		t.Fatal(err)
	}
	for _, column := range schema.Tables[0].Columns {
		if column.Name == "mystery" {
			t.Fatal("a column with no type was imported")
		}
	}
	var explained bool
	for _, drop := range report.Drops {
		if strings.Contains(drop.ID, "mystery") {
			explained = true
		}
	}
	if !explained {
		t.Fatalf("the untyped column was dropped silently: %+v", report.Drops)
	}
}

// Nothing in an import may come from the gj_* system surface: those are
// GraphJin's own tables, not the organization's.
func TestImportSchemaIgnoresSystemTables(t *testing.T) {
	rows := append(importFixture(),
		tableRow("gj_watch"),
		evidenceRow("gj_watch", "id", "integer", true, true, 0),
	)
	schema, _, err := ImportSchema(rows)
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range schema.Tables {
		if strings.HasPrefix(table.Name, "gj_") {
			t.Fatalf("system table %s was imported", table.Name)
		}
	}
}

func TestImportSchemaFailsWhenNothingIsUsable(t *testing.T) {
	if _, _, err := ImportSchema([]ImportRow{tableRow("orphan")}); err == nil {
		t.Fatal("expected an import with no usable tables to fail")
	}
}

// The evidence carries the closed set as structured values; the older prose
// form on examples_json must still work for a server that predates it.
func TestImportSchemaFallsBackToProseObservedValues(t *testing.T) {
	row := ImportRow{
		CatalogRow: CatalogRow{
			ID: "column:accounts.plan", Kind: "column", TableName: "accounts", ColumnName: "plan",
			ExamplesJSON: []any{`where: { plan: { eq: "enterprise" } }`, "plan values: enterprise, growth"},
		},
		EvidenceJSON: map[string]any{"ColumnName": "plan", "TableName": "accounts", "Type": "text", "NotNull": true},
	}
	rows := []ImportRow{tableRow("accounts"), evidenceRow("accounts", "id", "integer", true, true, 0), row}
	schema, _, err := ImportSchema(rows)
	if err != nil {
		t.Fatal(err)
	}
	for _, column := range schema.Tables[0].Columns {
		if column.Name == "plan" {
			if len(column.ObservedValues) != 2 {
				t.Fatalf("prose closed set did not survive: %+v", column)
			}
			return
		}
	}
	t.Fatal("plan column missing")
}

// GraphJin exposes file and API sources as tables in the same namespace, with
// the same shape of card. Cloning one produces a table the schema cannot create
// and the seed cannot insert into — which is exactly how it failed the first
// time, on a file source called sla_policies.
func TestImportSchemaSkipsTablesServedByFileSources(t *testing.T) {
	rows := append(importFixture(),
		ImportRow{CatalogRow: CatalogRow{
			ID: "table:sla_policies", Kind: "table", TableName: "sla_policies",
			ExamplesJSON: []any{
				`{ sla_policies(prefix: "", limit: 10) { key size content_type modified_at } }`,
				`{ sla_policies(key: "<key>") { key content_type text data } }`,
			},
		}},
		evidenceRow("sla_policies", "key", "text", true, true, 0),
		evidenceRow("sla_policies", "size", "integer", false, false, 1),
	)
	schema, report, err := ImportSchema(rows)
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range schema.Tables {
		if table.Name == "sla_policies" {
			t.Fatal("a file-source table was cloned as a database table")
		}
	}
	var explained bool
	for _, drop := range report.Drops {
		if drop.ID == "sla_policies" && strings.Contains(drop.Reason, "file or API source") {
			explained = true
		}
	}
	if !explained {
		t.Fatalf("the file source was dropped without saying why: %+v", report.Drops)
	}
}

// The marker is the root argument, not the column names: an ordinary table with
// a column called key must still clone.
func TestImportSchemaKeepsOrdinaryTablesWithAKeyColumn(t *testing.T) {
	rows := []ImportRow{
		{CatalogRow: CatalogRow{
			ID: "table:settings", Kind: "table", TableName: "settings",
			ExamplesJSON: []any{`{ settings(where: { key: { eq: "theme" } }) { key value } }`},
		}},
		evidenceRow("settings", "id", "integer", true, true, 0),
		evidenceRow("settings", "key", "text", true, false, 1),
	}
	schema, _, err := ImportSchema(rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(schema.Tables) != 1 || schema.Tables[0].Name != "settings" {
		t.Fatalf("an ordinary table was dropped: %+v", schema.Tables)
	}
}
