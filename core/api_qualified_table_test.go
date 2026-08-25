package core

import (
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

// A table card titles itself "<database>.<schema>.<table>", so that qualified
// name is what a model reads off the catalog and passes to any tool taking a
// table. These tests pin that the engine accepts the name its own catalog
// publishes.

func TestGetTableSchemaAcceptsCatalogQualifiedName(t *testing.T) {
	g := newGraphJinWithSchemas(t, map[string]*sdata.DBSchema{"app": mustTestSchema(t)})

	for _, name := range []string{"app.public.users", "public.users", "users"} {
		schema, err := g.GetTableSchema(name)
		if err != nil {
			t.Fatalf("GetTableSchema(%q) = %v, want the users table", name, err)
		}
		if schema.Name != "users" {
			t.Errorf("GetTableSchema(%q).Name = %q, want users", name, schema.Name)
		}
		if schema.Database != "app" {
			t.Errorf("GetTableSchema(%q).Database = %q, want app", name, schema.Database)
		}
	}
}

// The qualification is not cosmetic: it has to pick the database out, which is
// the case the bare name cannot express at all.
func TestQualifiedNameSelectsDatabaseWhenBareNameIsAmbiguous(t *testing.T) {
	g := newGraphJinWithSchemas(t, map[string]*sdata.DBSchema{
		"primary":   mustTestSchema(t),
		"analytics": mustTestSchema(t),
	})

	if _, err := g.GetTableSchema("users"); err == nil {
		t.Fatal("bare users should still be ambiguous across two databases")
	}

	for _, db := range []string{"primary", "analytics"} {
		schema, err := g.GetTableSchema(db + ".public.users")
		if err != nil {
			t.Fatalf("GetTableSchema(%q) = %v, want a resolved table", db+".public.users", err)
		}
		if schema.Database != db {
			t.Errorf("qualified lookup resolved to database %q, want %q", schema.Database, db)
		}
	}
}

// A name that is qualified but still wrong must report the same failure it
// always did, rather than a confusing one from the fallback's last attempt.
func TestUnknownQualifiedNameKeepsTheOriginalError(t *testing.T) {
	g := newGraphJinWithSchemas(t, map[string]*sdata.DBSchema{"app": mustTestSchema(t)})

	_, err := g.GetTableSchema("app.public.no_such_table")
	if err == nil {
		t.Fatal("expected an error for a table that does not exist")
	}
	if !strings.Contains(err.Error(), "no_such_table") || !strings.Contains(err.Error(), "searched all databases") {
		t.Fatalf("error = %v, want the original not-found error naming the table", err)
	}
}

func TestSplitQualifiedTableName(t *testing.T) {
	for _, tc := range []struct {
		name     string
		database string
		table    string
		want     []qualifiedTableName
	}{
		{name: "unqualified names are left to the plain lookup", table: "users"},
		{name: "empty trailing segment is not a table", table: "app.public."},
		{
			name:  "database.schema.table is the shape the catalog publishes",
			table: "app.public.users",
			want: []qualifiedTableName{
				{database: "app", schema: "public", table: "users"},
				{schema: "public", table: "users"},
			},
		},
		{
			name:  "two segments are ambiguous, so both readings are tried",
			table: "public.users",
			want: []qualifiedTableName{
				{schema: "public", table: "users"},
				{database: "public", table: "users"},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := splitQualifiedTableName(tc.database, tc.table)
			if len(got) != len(tc.want) {
				t.Fatalf("splitQualifiedTableName(%q, %q) = %+v, want %+v", tc.database, tc.table, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("candidate %d = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// The agent's validate_where_clause hands a failed lookup to this, so a model
// that guessed a near-miss name gets the real one back instead of a dead end.
func TestSuggestTableNames(t *testing.T) {
	g := newGraphJinWithSchemas(t, map[string]*sdata.DBSchema{"app": mustTestSchema(t)})

	got := g.SuggestTableNames("customer")
	if len(got) == 0 {
		t.Fatalf("SuggestTableNames(customer) found nothing; tables are %v", tableNamesOf(g))
	}
	found := false
	for _, n := range got {
		if n == "customers" {
			found = true
		}
	}
	if !found {
		t.Fatalf("SuggestTableNames(customer) = %v, want it to include customers", got)
	}
	if unrelated := g.SuggestTableNames("warehouses"); len(unrelated) != 0 {
		t.Fatalf("an unrelated name must stay unsuggested, got %v", unrelated)
	}
	if clause := DidYouMeanClause(g.SuggestTableNames("customer")); !strings.Contains(clause, "customers") {
		t.Fatalf("clause = %q, want it to name customers", clause)
	}
}

func tableNamesOf(g *GraphJin) []string {
	var out []string
	for _, t := range g.GetTables() {
		out = append(out, t.Name)
	}
	return out
}
