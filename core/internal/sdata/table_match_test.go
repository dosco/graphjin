package sdata

import "testing"

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
