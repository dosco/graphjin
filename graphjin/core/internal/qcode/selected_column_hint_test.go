package qcode_test

import (
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func ticketSchema(t *testing.T) *sdata.DBSchema {
	t.Helper()
	columns := []sdata.DBColumn{
		{Schema: "public", Table: "support_tickets", Name: "id", Type: "bigint", PrimaryKey: true, NotNull: true},
		{Schema: "public", Table: "support_tickets", Name: "status", Type: "text"},
		{Schema: "public", Table: "support_tickets", Name: "severity", Type: "text"},
		{Schema: "public", Table: "support_tickets", Name: "resolution_note", Type: "text"},
	}
	info := sdata.NewDBInfo("postgres", 160000, "public", "test", columns, nil, nil)
	schema, err := sdata.NewDBSchema(info, nil)
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

// Driven through Compile rather than the helper: testing the helper alone does
// not prove the compiler calls it. An earlier mutation check passed against a
// reverted call site for exactly that reason.
func TestSelectedColumnMissNamesTheLikelyColumn(t *testing.T) {
	compiler, err := qcode.NewCompiler(ticketSchema(t), qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = compiler.Compile([]byte(`query { support_tickets { id resolution } }`), nil, "user", "")
	if err == nil {
		t.Fatal("selecting a column that does not exist must fail")
	}
	if !strings.Contains(err.Error(), `did you mean "resolution_note"`) {
		t.Fatalf("error must name the likely column: %v", err)
	}
	// The original message still leads; it is what names the failure.
	if !strings.Contains(err.Error(), "is not a column or a function") {
		t.Fatalf("original message lost: %v", err)
	}
	// The suggestion travels alone: dumping the column list behind an answer
	// the caller already has is pure cost, and on a wide table it is an
	// arbitrary prefix that rarely contains the column anyway.
	if strings.Contains(err.Error(), "available columns") {
		t.Fatalf("a named suggestion should not also carry the column list: %v", err)
	}
}

// A selected name unrelated to anything gets the column list but no invented
// suggestion — a matcher that always finds something teaches the caller to
// trust names that are not real.
func TestUnrelatedSelectedColumnIsNotGivenASuggestion(t *testing.T) {
	compiler, err := qcode.NewCompiler(ticketSchema(t), qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = compiler.Compile([]byte(`query { support_tickets { id warehouse_zone } }`), nil, "user", "")
	if err == nil {
		t.Fatal("selecting a column that does not exist must fail")
	}
	if strings.Contains(err.Error(), "did you mean") {
		t.Fatalf("an unrelated name must not be given a suggestion: %v", err)
	}
	if !strings.Contains(err.Error(), "available columns:") {
		t.Fatalf("the column list is still useful: %v", err)
	}
}

// A valid selection keeps compiling.
func TestValidSelectionStillCompiles(t *testing.T) {
	compiler, err := qcode.NewCompiler(ticketSchema(t), qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compiler.Compile([]byte(`query { support_tickets { id resolution_note } }`), nil, "user", ""); err != nil {
		t.Fatalf("a valid selection must compile: %v", err)
	}
}
