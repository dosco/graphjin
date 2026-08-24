package qcode_test

import (
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
