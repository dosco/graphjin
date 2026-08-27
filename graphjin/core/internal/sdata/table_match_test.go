package sdata

import (
	"fmt"
	"strings"
	"testing"
)

// The names here are the ones the recorded benchmark runs actually failed on.
func TestMatchTableNames(t *testing.T) {
	tables := []string{"support_tickets", "sla_policies", "payments", "accounts", "invoices"}
	for _, tc := range []struct {
		want string
		got  []string
	}{
		{"tickets", []string{"support_tickets"}},        // plural fragment
		{"support_ticket", []string{"support_tickets"}}, // singular
		{"payment", []string{"payments"}},               // singular
		{"policies", []string{"sla_policies"}},          // dropped qualifier
		{"warehouses", nil},                             // unrelated: stay silent
		{"", nil},                                       // nothing asked
	} {
		got := MatchTableNames(tc.want, tables)
		if len(got) != len(tc.got) {
			t.Fatalf("MatchTableNames(%q) = %v, want %v", tc.want, got, tc.got)
		}
		for i := range got {
			if got[i] != tc.got[i] {
				t.Fatalf("MatchTableNames(%q) = %v, want %v", tc.want, got, tc.got)
			}
		}
	}
}

// A suggestion that names nothing is worse than an error that stops.
func TestDidYouMeanClause(t *testing.T) {
	if got := DidYouMeanClause(nil); got != "" {
		t.Fatalf("empty suggestions rendered %q", got)
	}
	if got := DidYouMeanClause([]string{"support_tickets"}); got != `; did you mean "support_tickets"?` {
		t.Fatalf("single suggestion = %q", got)
	}
	if got := DidYouMeanClause([]string{"a", "b"}); got != `; did you mean one of ["a" "b"]?` {
		t.Fatalf("multiple suggestions = %q", got)
	}
}

// The synonym case, which is what semantic catalog search produces: the model
// reaches the right subject under a word the schema does not use. No edit
// distance turns `companies` into `accounts`, so the suggestion finds nothing
// and the list is the only thing that ends the guessing.
func TestTableHint(t *testing.T) {
	tables := []string{"accounts", "invoices", "sla_policies", "support_tickets"}

	if got := TableHint("policies", tables); got != `; did you mean "sla_policies"?` {
		t.Fatalf("a near miss should get the suggestion alone, got %q", got)
	}
	if got := TableHint("companies", tables); got != "; available tables: accounts, invoices, sla_policies, support_tickets" {
		t.Fatalf("a synonym should be handed the real tables, got %q", got)
	}
	if got := TableHint("companies", nil); got != "" {
		t.Fatalf("no tables means nothing to say, got %q", got)
	}

	wide := make([]string, 0, 40)
	for n := 0; n < 40; n++ {
		wide = append(wide, fmt.Sprintf("table_%02d", n))
	}
	got := TableHint("companies", wide)
	if !strings.HasPrefix(got, "; available tables include: table_00, ") {
		t.Fatalf("wide schema should be bounded, got %q", got)
	}
	if !strings.HasSuffix(got, "(16 more)") {
		t.Fatalf("wide schema should name what it hides, got %q", got)
	}
	if strings.Contains(got, "table_24") {
		t.Fatalf("wide schema should stop at %d names, got %q", maxListedTables, got)
	}
}
