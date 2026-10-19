package core

import (
	"errors"
	"strings"
	"testing"
)

func TestNewErrorIncludesGraphJinRepairExtension(t *testing.T) {
	errs := newError(`query { orders_aggregate { count } }`, errors.New(`table "orders_aggregate" not found`))
	if len(errs) != 1 {
		t.Fatalf("expected one error, got %d", len(errs))
	}
	if errs[0].Message != `table "orders_aggregate" not found` {
		t.Fatalf("message changed: %q", errs[0].Message)
	}
	raw := errs[0].Extensions["graphjin_repair"]
	repair, ok := raw.(ErrorRepair)
	if !ok {
		t.Fatalf("expected graphjin_repair extension, got %#v", raw)
	}
	if repair.Kind != repairKindTableNotFound {
		t.Fatalf("expected table-not-found repair, got %+v", repair)
	}
}

func TestBuildGraphJinErrorRepairNullComparison(t *testing.T) {
	query := `query { users(where: { deleted_at: { neq: null } }) { id } }`
	repair := BuildGraphJinErrorRepair(query, "[Where] `neq: null` is not a valid null comparison; use `is_null: false`")
	if repair.Kind != repairKindNullComparison {
		t.Fatalf("unexpected repair: %+v", repair)
	}
	if !strings.Contains(repair.RepairedQuery, "is_null: false") || strings.Contains(repair.RepairedQuery, "neq: null") {
		t.Fatalf("repair is not executable: %q", repair.RepairedQuery)
	}
}

func TestBuildGraphJinErrorRepairAggregateFilter(t *testing.T) {
	errMessage := "[Where] aggregate token `max: created_at` cannot be embedded in a `gte` filter; first query `max_created_at`, then filter `gte` with the returned literal"
	repair := BuildGraphJinErrorRepair("query { users { id } }", errMessage)
	if repair.Kind != repairKindAggregateFilter {
		t.Fatalf("unexpected repair: %+v", repair)
	}
	if !strings.Contains(repair.Diagnosis, "second query") {
		t.Fatalf("missing two-step diagnosis: %+v", repair)
	}
}

func TestBuildGraphJinErrorRepairQualifiedRoot(t *testing.T) {
	query := `query { app.accounts { id } }`
	repair := BuildGraphJinErrorRepair(query, "invalid GraphQL root \"app.accounts\": roots are unqualified table names: write `accounts`, not `app.accounts`")
	if repair.Kind != repairKindQualifiedRoot {
		t.Fatalf("unexpected repair: %+v", repair)
	}
	if repair.RepairedQuery != `query { accounts { id } }` {
		t.Fatalf("unexpected repaired query: %q", repair.RepairedQuery)
	}
}

// The compiler now works out what a missing root probably meant, and the
// structured repair a model is told to read has to say the same thing. A
// diagnosis that only says "check spelling" is what let the recorded runs
// re-send `policies` until the step budget ran out.
func TestTableNotFoundRepairCarriesTheSuggestedName(t *testing.T) {
	repair := BuildGraphJinErrorRepair(
		`query { policies { key } }`,
		`table not found: main.policies; did you mean "sla_policies"?`)

	if repair.Kind != repairKindTableNotFound {
		t.Fatalf("expected table-not-found repair, got %+v", repair)
	}
	if !strings.Contains(repair.Diagnosis, "sla_policies") {
		t.Fatalf("diagnosis = %q, want it to name sla_policies", repair.Diagnosis)
	}
}

func TestTableNotFoundRepairWithoutASuggestionIsUnchanged(t *testing.T) {
	repair := BuildGraphJinErrorRepair(
		`query { warehouses { id } }`,
		`table not found: main.warehouses`)

	if repair.Kind != repairKindTableNotFound {
		t.Fatalf("expected table-not-found repair, got %+v", repair)
	}
	if !strings.Contains(repair.Diagnosis, "Check spelling") {
		t.Fatalf("diagnosis = %q, want the generic guidance when nothing was matched", repair.Diagnosis)
	}
}

func TestTableNotFoundRepairCarriesSeveralSuggestedNames(t *testing.T) {
	repair := BuildGraphJinErrorRepair(
		`query { ticket { id } }`,
		`table not found: main.ticket; did you mean one of ["support_tickets" "ticket_events"]?`)

	for _, want := range []string{"support_tickets", "ticket_events"} {
		if !strings.Contains(repair.Diagnosis, want) {
			t.Fatalf("diagnosis = %q, want it to name %s", repair.Diagnosis, want)
		}
	}
}
