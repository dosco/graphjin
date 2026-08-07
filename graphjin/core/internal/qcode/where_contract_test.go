package qcode_test

import (
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
)

func TestWhereNotEqualNullNamesIsNullRepair(t *testing.T) {
	compiler, err := qcode.NewCompiler(dbs, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = compiler.Compile([]byte(`query { users(where: { email: { neq: null } }) { id } }`), nil, "user", "")
	if err == nil {
		t.Fatal("expected neq: null to fail")
	}
	if !strings.Contains(err.Error(), "use `is_null: false`") {
		t.Fatalf("missing executable repair: %v", err)
	}
}

func TestWhereAggregateOperandNamesTwoStepRepair(t *testing.T) {
	compiler, err := qcode.NewCompiler(dbs, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = compiler.Compile([]byte(`query { users(where: { id: { gte: { max: id } } }) { id } }`), nil, "user", "")
	if err == nil {
		t.Fatal("expected embedded aggregate operand to fail")
	}
	for _, want := range []string{"first query `max_id`", "returned literal"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("missing %q in repair: %v", want, err)
		}
	}
}

func TestWhereLiteralComparisonStillCompiles(t *testing.T) {
	compiler, err := qcode.NewCompiler(dbs, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compiler.Compile([]byte(`query { users(where: { id: { gte: 10 } }) { id } }`), nil, "user", ""); err != nil {
		t.Fatalf("literal comparison regressed: %v", err)
	}
}
