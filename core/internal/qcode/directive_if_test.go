package qcode_test

import (
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
)

// TestIncludeDirectiveLiteralBool exercises the standard GraphQL
// @include(if: Boolean!) directive argument. @include(if: true) keeps the
// field; @include(if: false) drops it. Literal Boolean is resolved at
// compile time, no SQL parameter involvement.
func TestIncludeDirectiveLiteralBool(t *testing.T) {
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{})
	if err := qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{Columns: []string{"id", "name"}},
	}); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		gql     string
		wantID  bool // whether the `id` field ends up in the compiled select
	}{
		{"include true keeps", `{ products { id @include(if: true) } }`, true},
		{"include false drops", `{ products { id @include(if: false) } }`, false},
		{"skip true drops", `{ products { id @skip(if: true) } }`, false},
		{"skip false keeps", `{ products { id @skip(if: false) } }`, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			qcomp, err := qc.Compile([]byte(c.gql), nil, "user", "")
			if err != nil {
				t.Fatalf("compile err: %v", err)
			}
			sel := &qcomp.Selects[0]
			has := false
			for _, f := range sel.Fields {
				if f.FieldName == "id" && f.SkipRender != qcode.SkipTypeDrop {
					has = true
					break
				}
			}
			if has != c.wantID {
				t.Errorf("id-kept = %v, want %v", has, c.wantID)
			}
		})
	}
}

// TestIncludeDirectiveIfVariable verifies that passing a variable to
// @include(if: $var) still works (not just literal booleans).
func TestIncludeDirectiveIfVariable(t *testing.T) {
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{})
	if err := qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{Columns: []string{"id", "name"}},
	}); err != nil {
		t.Fatal(err)
	}

	_, err := qc.Compile(
		[]byte(`query ($show: Boolean!) { products { id @include(if: $show) } }`),
		nil, "user", "")
	if err != nil {
		t.Fatalf("@include(if: $var) should compile; err = %v", err)
	}
}

// TestIncludeDirectiveRejectsNonBooleanLiteral asserts the error path for
// a string/number passed where a Boolean is expected.
func TestIncludeDirectiveRejectsNonBooleanLiteral(t *testing.T) {
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{})
	if err := qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{Columns: []string{"id", "name"}},
	}); err != nil {
		t.Fatal(err)
	}

	_, err := qc.Compile(
		[]byte(`{ products { id @include(if: "true") } }`),
		nil, "user", "")
	if err == nil {
		t.Fatal("expected error for non-Boolean literal in @include(if:)")
	}
	if !strings.Contains(err.Error(), "Boolean") {
		t.Errorf("error should mention Boolean; got %q", err.Error())
	}
}

// TestArgTypesNoLeadingSeparator is a regression guard for BUG-G4. The
// argErr formatter used to emit " or <type>" for single-type lists,
// producing "value for argument 'after' must be  or a variable" with a
// double-space. Fixed to produce "must be a variable" for a single type.
func TestArgTypesNoLeadingSeparator(t *testing.T) {
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{})
	if err := qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{Columns: []string{"id"}},
	}); err != nil {
		t.Fatal(err)
	}

	_, err := qc.Compile(
		[]byte(`{ products(first: 3, after: "abc") { id } }`),
		nil, "user", "")
	if err == nil {
		t.Fatal("expected error for cursor after: string literal")
	}
	msg := err.Error()
	// The bug produced a literal double-space between "be" and "or".
	if strings.Contains(msg, "be  or") || strings.Contains(msg, "be  a") {
		t.Errorf("double-space leaked into error message: %q", msg)
	}
	// Leading " or " with a space boundary before it is also a regression.
	if strings.Contains(msg, "must  be") {
		t.Errorf("double-space between 'must' and 'be': %q", msg)
	}
}
