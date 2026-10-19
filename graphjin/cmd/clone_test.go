package main

import (
	"strings"
	"testing"

	gjeval "github.com/dosco/graphjin/agent/v3/eval"
)

func TestCloneTypeMappingCoversRealDatabaseTypes(t *testing.T) {
	cases := map[string]string{
		"bigint":                      "Bigint",
		"BIGINT":                      "Bigint",
		"big int":                     "Bigint",
		"integer":                     "Int",
		"character varying(255)":      "Varchar",
		"timestamp with time zone":    "TimestampWithTimeZone",
		"timestamp without time zone": "Timestamp",
		"datetime":                    "Timestamp",
		"numeric(10,2)":               "Numeric",
		"boolean":                     "Boolean",
		"jsonb":                       "Jsonb",
		"uuid":                        "Uuid",
		"text":                        "Text",
	}
	for raw, want := range cases {
		mapping := mapCloneType(raw)
		if mapping.Base != want {
			t.Fatalf("%q mapped to %q, want %q", raw, mapping.Base, want)
		}
		if !mapping.Mapped {
			t.Fatalf("%q should be a known type", raw)
		}
	}

	// The schema parser splits PascalCase on capitals, so UUID would become the
	// type "u u i d". The mapping must never produce it.
	if mapCloneType("uuid").Base == "UUID" {
		t.Fatal("uuid must map to Uuid, never UUID")
	}

	// A size survives as a type argument rather than being silently dropped.
	if args := mapCloneType("numeric(10,2)").Args; args != "10,2" {
		t.Fatalf("precision was lost: %q", args)
	}
	if ddl := mapCloneType("character varying(255)").DDL(true); !strings.Contains(ddl, `@type(args: "255")`) || !strings.Contains(ddl, "Varchar!") {
		t.Fatalf("unexpected DDL %q", ddl)
	}
}

// An unmapped type must not fail the clone: the column is still countable,
// filterable and readable as text, and the original is recorded.
func TestCloneUnmappedTypeFallsBackToTextAndIsReported(t *testing.T) {
	mapping := mapCloneType("geography(Point,4326)")
	if mapping.Base != "Text" {
		t.Fatalf("expected the Text fallback, got %q", mapping.Base)
	}
	if mapping.Mapped {
		t.Fatal("the fallback must be reported as unmapped")
	}
	if mapping.Original != "geography(Point,4326)" {
		t.Fatalf("the original type was lost: %q", mapping.Original)
	}
}

// SQLite has no date type, so a column declared Date introspects as "text".
// Losing that would leave a clone with no dates and therefore no questions
// about a period, so the column's name recovers what the type could not say.
func TestCloneRecoversDatesSQLiteCannotExpress(t *testing.T) {
	if got := mapCloneColumnType("last_active_at", "text").Base; got != "TimestampWithTimeZone" {
		t.Fatalf("_at column mapped to %q", got)
	}
	if got := mapCloneColumnType("renewal_date", "text").Base; got != "Date" {
		t.Fatalf("_date column mapped to %q", got)
	}
	// A type the database did report is never second-guessed.
	if got := mapCloneColumnType("created_at", "bigint").Base; got != "Bigint" {
		t.Fatalf("a reported type was overridden: %q", got)
	}
	// An ordinary text column stays text.
	if got := mapCloneColumnType("notes", "text").Base; got != "Text" {
		t.Fatalf("an ordinary column became %q", got)
	}
}

func cloneTestSchema() gjeval.ImportedSchema {
	return gjeval.ImportedSchema{
		Tables: []gjeval.ImportedTable{
			{Name: "invoices", PrimaryKey: "id", Columns: []gjeval.ImportedColumn{
				{Name: "id", Type: "integer", NotNull: true, PrimaryKey: true},
				{Name: "account_id", Type: "integer", NotNull: true},
				{Name: "status", Type: "text", NotNull: true, ObservedValues: []string{"paid", "failed"}},
			}},
			{Name: "accounts", PrimaryKey: "id", Columns: []gjeval.ImportedColumn{
				{Name: "id", Type: "integer", NotNull: true, PrimaryKey: true},
				{Name: "name", Type: "text", NotNull: true, UniqueKey: true},
			}},
		},
		Relationships: []gjeval.ImportedRelationship{
			{FromTable: "invoices", FromColumn: "account_id", ToTable: "accounts", ToColumn: "id"},
		},
	}
}

// A seed inserts rows in table order, so a table must never be written before
// the one it points at exists.
func TestCloneOrdersParentsBeforeChildren(t *testing.T) {
	world, _ := cloneWorldSpec(cloneTestSchema(), cloneOptions{Rows: 5, Seed: 3}, "Clone")
	var order []string
	for _, table := range world.Tables {
		order = append(order, table.Name)
	}
	if len(order) != 2 || order[0] != "accounts" {
		t.Fatalf("parents must come first, got %v", order)
	}
}

func TestCloneEmitsKeysRelationsAndClosedSets(t *testing.T) {
	world, unmapped := cloneWorldSpec(cloneTestSchema(), cloneOptions{Rows: 5, Seed: 3}, "Clone")
	if len(unmapped) != 0 {
		t.Fatalf("nothing here should be unmapped: %v", unmapped)
	}
	ddl := renderWorldDDL(world)
	for _, want := range []string{
		"type accounts {", "type invoices {",
		"id: Int! @id",
		"@relation(type: accounts, field: id)",
		"@unique",
	} {
		if !strings.Contains(ddl, want) {
			t.Fatalf("DDL missing %q:\n%s", want, ddl)
		}
	}
	// The closed set the catalog published has to reach the synthetic rows, or
	// filters would ask about states the clone does not contain.
	seed := renderWorldSeed(world)
	if !strings.Contains(seed, `status: "paid"`) && !strings.Contains(seed, `status: "failed"`) {
		t.Fatalf("observed values did not reach the seed:\n%s", seed)
	}
	// Foreign keys must land inside the parent's row range.
	if !strings.Contains(seed, "account_id:") {
		t.Fatal("the foreign key was not seeded")
	}
}

// A unique column cannot risk two rows drawing the same generated name; a
// collision fails the seed and the whole clone never boots.
func TestCloneGivesUniqueColumnsDistinctValues(t *testing.T) {
	world, _ := cloneWorldSpec(cloneTestSchema(), cloneOptions{Rows: 20, Seed: 11}, "Clone")
	seed := renderWorldSeed(world)
	seen := map[string]bool{}
	for _, line := range strings.Split(seed, "\n") {
		index := strings.Index(line, `name: "`)
		if index < 0 {
			continue
		}
		rest := line[index+len(`name: "`):]
		value := rest[:strings.IndexByte(rest, '"')]
		if seen[value] {
			t.Fatalf("duplicate value %q in a unique column", value)
		}
		seen[value] = true
	}
	if len(seen) < 20 {
		t.Fatalf("expected 20 distinct names, got %d", len(seen))
	}
}

func TestCloneIsReproducibleFromItsSeed(t *testing.T) {
	first, _ := cloneWorldSpec(cloneTestSchema(), cloneOptions{Rows: 8, Seed: 5}, "Clone")
	second, _ := cloneWorldSpec(cloneTestSchema(), cloneOptions{Rows: 8, Seed: 5}, "Clone")
	if renderWorldSeed(first) != renderWorldSeed(second) {
		t.Fatal("the same seed produced different synthetic data")
	}
	other, _ := cloneWorldSpec(cloneTestSchema(), cloneOptions{Rows: 8, Seed: 6}, "Clone")
	if renderWorldSeed(first) == renderWorldSeed(other) {
		t.Fatal("two seeds produced the same data")
	}
}
