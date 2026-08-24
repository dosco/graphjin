package agent

import (
	"strings"
	"testing"

	core "github.com/dosco/graphjin/core/v3"
)

// validate_where_clause is handed whatever name the catalog showed the model,
// and a table card's title is the qualified one. The lookup resolves it; these
// pin that the rest of the result switches to the resolved bare name, because
// the example query is GraphQL and its roots are unqualified.
func TestResolvedValidationTarget(t *testing.T) {
	resolved := &core.TableSchema{Name: "users", Schema: "public", Database: "app"}

	for _, tc := range []struct {
		name         string
		table        string
		database     string
		schema       *core.TableSchema
		wantTable    string
		wantDatabase string
	}{
		{
			name:  "qualified name becomes the resolved bare table",
			table: "app.public.users", schema: resolved,
			wantTable: "users", wantDatabase: "app",
		},
		{
			name:  "an explicit database is not overridden",
			table: "app.public.users", database: "analytics", schema: resolved,
			wantTable: "users", wantDatabase: "analytics",
		},
		{
			name:  "a bare name is left exactly as it was",
			table: "users", schema: resolved,
			wantTable: "users", wantDatabase: "",
		},
		{
			name:      "no schema means nothing to resolve from",
			table:     "app.public.users",
			wantTable: "app.public.users",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gotTable, gotDatabase := resolvedValidationTarget(tc.table, tc.database, tc.schema)
			if gotTable != tc.wantTable || gotDatabase != tc.wantDatabase {
				t.Fatalf("resolvedValidationTarget(%q, %q) = (%q, %q), want (%q, %q)",
					tc.table, tc.database, gotTable, gotDatabase, tc.wantTable, tc.wantDatabase)
			}
		})
	}
}

// The reason the rewrite has to happen: a dotted root is not valid GraphQL, so
// leaving it in place would only move the failure to query building.
func TestBuildWhereValidationQueryRejectsQualifiedRoot(t *testing.T) {
	if _, err := buildWhereValidationQuery("app.public.users", "", "{id: {eq: 1}}", "id"); err == nil {
		t.Fatal("expected a dotted root to be rejected as an unsupported table name")
	}

	query, err := buildWhereValidationQuery("users", "app", "{id: {eq: 1}}", "id")
	if err != nil {
		t.Fatalf("bare root should build: %v", err)
	}
	if !strings.Contains(query, "users(where:") || strings.Contains(query, "app.public.users") {
		t.Fatalf("query = %q, want an unqualified users root", query)
	}
}
