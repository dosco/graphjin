package qcode_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func suggestionSchema(t *testing.T) *sdata.DBSchema {
	t.Helper()
	columns := []sdata.DBColumn{
		{Schema: "public", Table: "sla_policies", Name: "key", Type: "text", PrimaryKey: true, NotNull: true},
		{Schema: "public", Table: "sla_policies", Name: "text", Type: "text"},
		{Schema: "public", Table: "support_tickets", Name: "id", Type: "bigint", PrimaryKey: true, NotNull: true},
	}
	info := sdata.NewDBInfo("postgres", 160000, "public", "test", columns, nil, nil)
	schema, err := sdata.NewDBSchema(info, nil)
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func mustCompiler(t *testing.T, schema *sdata.DBSchema) *qcode.Compiler {
	t.Helper()
	compiler, err := qcode.NewCompiler(schema, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}
	return compiler
}

// A model reading "the support SLA policy file" asks for `policies`. Before the
// suggestion it got back only the name it already knew was wrong, so it guessed
// again until the step budget ran out — the recorded loops re-sent `policies`
// and `files` while the table sat there as `sla_policies`.
func TestUnknownRootSuggestsTheRealTable(t *testing.T) {
	compiler := mustCompiler(t, suggestionSchema(t))

	_, err := compiler.Compile([]byte(`query { policies { key } }`), nil, "user", "")
	if err == nil {
		t.Fatal("expected an error for an unknown root")
	}
	if !strings.Contains(err.Error(), `did you mean "sla_policies"`) {
		t.Fatalf("error = %v, want a sla_policies suggestion", err)
	}
	// The original message has to survive: it is what names the failure.
	if !strings.Contains(err.Error(), "table not found") {
		t.Fatalf("error = %v, want the not-found message kept", err)
	}
}

// A partial noun should still reach the real table.
func TestUnknownRootSuggestsOnPartialName(t *testing.T) {
	compiler := mustCompiler(t, suggestionSchema(t))

	_, err := compiler.Compile([]byte(`query { ticket { id } }`), nil, "user", "")
	if err == nil || !strings.Contains(err.Error(), `did you mean "support_tickets"`) {
		t.Fatalf("error = %v, want a support_tickets suggestion", err)
	}
}

// A guess unrelated to anything must not acquire a suggestion. A matcher that
// always finds something teaches the model to trust names that are not real.
func TestUnrelatedRootIsNotGivenASuggestion(t *testing.T) {
	compiler := mustCompiler(t, suggestionSchema(t))

	_, err := compiler.Compile([]byte(`query { warehouses { id } }`), nil, "user", "")
	if err == nil {
		t.Fatal("expected an error for an unknown root")
	}
	if strings.Contains(err.Error(), "did you mean") {
		t.Fatalf("error = %v, want no suggestion for an unrelated name", err)
	}
}

// The synonym case, which is what semantic catalog search produces: the model
// reaches the right subject under a word the schema does not use. No edit
// distance turns `companies` into `accounts`, so the did-you-mean finds nothing
// and the list is the only thing that can end the guessing. Measured on a
// paired run, 82% of missing-table errors under semantic search carried no
// suggestion at all.
func TestUnrelatedRootIsNamedTheRealTables(t *testing.T) {
	compiler := mustCompiler(t, suggestionSchema(t))

	_, err := compiler.Compile([]byte(`query { companies { id } }`), nil, "user", "")
	if err == nil {
		t.Fatal("expected an error for an unknown root")
	}
	if !strings.Contains(err.Error(), "available tables: sla_policies, support_tickets") {
		t.Fatalf("error = %v, want the real tables named", err)
	}
}

// A suggestion is the answer, so it travels alone: appending the schema behind
// it would put the whole table list on every near miss.
func TestSuggestionSuppressesTheTableList(t *testing.T) {
	compiler := mustCompiler(t, suggestionSchema(t))

	_, err := compiler.Compile([]byte(`query { policies { key } }`), nil, "user", "")
	if err == nil || !strings.Contains(err.Error(), "did you mean") {
		t.Fatalf("error = %v, want a suggestion", err)
	}
	if strings.Contains(err.Error(), "available tables") {
		t.Fatalf("error = %v, want no table list behind a suggestion", err)
	}
}

// A large schema must not put hundreds of names in front of a model that needs
// one, and it must say what it is not showing rather than implying the schema
// ends there.
func TestUnrelatedRootBoundsTheTableList(t *testing.T) {
	columns := make([]sdata.DBColumn, 0, 40)
	for n := 0; n < 40; n++ {
		columns = append(columns, sdata.DBColumn{
			Schema: "public", Table: fmt.Sprintf("table_%02d", n),
			Name: "id", Type: "bigint", PrimaryKey: true, NotNull: true,
		})
	}
	info := sdata.NewDBInfo("postgres", 160000, "public", "test", columns, nil, nil)
	schema, err := sdata.NewDBSchema(info, nil)
	if err != nil {
		t.Fatal(err)
	}
	compiler := mustCompiler(t, schema)

	_, err = compiler.Compile([]byte(`query { warehouses { id } }`), nil, "user", "")
	if err == nil {
		t.Fatal("expected an error for an unknown root")
	}
	if !strings.Contains(err.Error(), "available tables include:") {
		t.Fatalf("error = %v, want a bounded list", err)
	}
	if !strings.Contains(err.Error(), "(16 more)") {
		t.Fatalf("error = %v, want the hidden count named", err)
	}
	if strings.Contains(err.Error(), "table_25") {
		t.Fatalf("error = %v, want the list truncated at 24", err)
	}
}

// The aggregate root had the same gap as the plain one, and it was the whole
// remainder after the plain one was closed: every genuine dead end left in the
// measured run was `users_aggregate`, a base name no edit distance reaches from
// any real table. The roots here are decorated, so the fallback has to name them
// the way they would have to be written.
func TestUnknownAggregateRootIsNamedTheRealRoots(t *testing.T) {
	compiler := mustCompiler(t, suggestionSchema(t))

	_, err := compiler.Compile([]byte(`query { users_aggregate { count } }`), nil, "user", "")
	if err == nil {
		t.Fatal("expected an error for an unknown aggregate root")
	}
	if !strings.Contains(err.Error(), "sla_policies_aggregate") ||
		!strings.Contains(err.Error(), "support_tickets_aggregate") {
		t.Fatalf("error = %v, want the real aggregate roots named", err)
	}
}
