package qcode_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

// Authorization tests for user-authored filters. A WHERE predicate can reveal
// a column through repeated probes even when that column is absent from the
// result, so its column references must follow the same role checks as fields.

func TestWhereRoleAllowlist(t *testing.T) {
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{})
	if err := qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{Columns: []string{"id", "name"}},
	}); err != nil {
		t.Fatal(err)
	}

	_, err := qc.Compile([]byte(`
		query { products(where: { price: { gt: 100 } }) {
			id
		} }`), nil, "user", "")
	if err == nil {
		t.Fatal("expected compile error filtering by disallowed column, got nil")
	}
	if !strings.Contains(err.Error(), "price") ||
		!strings.Contains(err.Error(), "db column blocked") {
		t.Errorf("error should report the blocked column, got: %v", err)
	}

	if _, err := qc.Compile([]byte(`
		query { products(where: { name: { eq: "Widget" } }) {
			id
		} }`), nil, "user", ""); err != nil {
		t.Fatalf("where on allowed column should compile: %v", err)
	}
}

// TestWhereMatchesFieldAuthorization pins the intended invariant for queries
// and mutation selections: filtering by a column is allowed exactly when
// selecting that column is allowed for the active operation type.
func TestWhereMatchesFieldAuthorization(t *testing.T) {
	for _, tc := range []struct {
		name      string
		role      qcode.TRConfig
		selectGQL string
		whereGQL  string
		allowed   bool
	}{
		{
			name: "query/query.cols blocked",
			role: qcode.TRConfig{
				Query: qcode.QueryConfig{Columns: []string{"id", "name"}},
			},
			selectGQL: `query { products { price } }`,
			whereGQL:  `query { products(where: { price: { gt: 100 } }) { id } }`,
		},
		{
			name: "query/query.cols allowed",
			role: qcode.TRConfig{
				Query: qcode.QueryConfig{Columns: []string{"id", "name", "price"}},
			},
			selectGQL: `query { products { price } }`,
			whereGQL:  `query { products(where: { price: { gt: 100 } }) { id } }`,
			allowed:   true,
		},
		{
			name: "mutation/update.cols blocked",
			role: qcode.TRConfig{
				Update: qcode.UpdateConfig{Columns: []string{"id", "name"}},
			},
			selectGQL: `mutation { users(update: $data, where: { id: { eq: 1 } }) {
				id
				products { price }
			} }`,
			whereGQL: `mutation { users(update: $data, where: { id: { eq: 1 } }) {
				id
				products(where: { price: { gt: 100 } }) { id }
			} }`,
		},
		{
			name: "mutation/update.cols allowed",
			role: qcode.TRConfig{
				Update: qcode.UpdateConfig{Columns: []string{"id", "name", "price"}},
			},
			selectGQL: `mutation { users(update: $data, where: { id: { eq: 1 } }) {
				id
				products { price }
			} }`,
			whereGQL: `mutation { users(update: $data, where: { id: { eq: 1 } }) {
				id
				products(where: { price: { gt: 100 } }) { id }
			} }`,
			allowed: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compile := func(gql string) error {
				qc, _ := qcode.NewCompiler(dbs, qcode.Config{})
				if err := qc.AddRole("user", "public", "products", tc.role); err != nil {
					t.Fatal(err)
				}
				vars := map[string]json.RawMessage{
					"data": json.RawMessage(`{"full_name":"Updated"}`),
				}
				_, err := qc.Compile([]byte(gql), vars, "user", "")
				return err
			}

			errSel := compile(tc.selectGQL)
			errWhere := compile(tc.whereGQL)
			if (errSel == nil) != tc.allowed {
				t.Fatalf("select authorization = %v, want allowed=%v", errSel, tc.allowed)
			}
			if errSel == nil && errWhere != nil {
				t.Errorf("where rejected a column that select allows: %v", errWhere)
			}
			if errSel != nil && errWhere == nil {
				t.Errorf("where allowed a column that select rejects (%v)", errSel)
			}
		})
	}
}

func TestWhereNestedRelationAllowlist(t *testing.T) {
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{})
	if err := qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{Columns: []string{"id", "name"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := qc.AddRole("user", "public", "users", qcode.TRConfig{
		Query: qcode.QueryConfig{Columns: []string{"id", "full_name"}},
	}); err != nil {
		t.Fatal(err)
	}

	_, err := qc.Compile([]byte(`
		query { products(where: { users: { email: { eq: "test@example.com" } } }) {
			id
		} }`), nil, "user", "")
	if err == nil {
		t.Fatal("expected compile error filtering by disallowed related column, got nil")
	}
	if !strings.Contains(err.Error(), "email") ||
		!strings.Contains(err.Error(), "db column blocked") {
		t.Errorf("error should report the blocked related column, got: %v", err)
	}

	if _, err := qc.Compile([]byte(`
		query { products(where: { users: { full_name: { eq: "Alice" } } }) {
			id
		} }`), nil, "user", ""); err != nil {
		t.Fatalf("where on allowed related column should compile: %v", err)
	}
}

// A blocked related selector normally renders as null, but using that table in
// a parent WHERE would still expose whether matching rows exist. Reject that
// user-authored relation filter instead of silently applying it.
func TestWhereNestedRelationBlocked(t *testing.T) {
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{})
	if err := qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{Columns: []string{"id", "name"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := qc.AddRole("user", "public", "users", qcode.TRConfig{
		Query: qcode.QueryConfig{Block: true},
	}); err != nil {
		t.Fatal(err)
	}

	_, err := qc.Compile([]byte(`
		query { products(where: { users: { id: { eq: 1 } } }) {
			id
		} }`), nil, "user", "")
	if err == nil {
		t.Fatal("expected compile error filtering through a blocked relation, got nil")
	}
	if !strings.Contains(err.Error(), "blocked: users") {
		t.Errorf("error should report the blocked related table, got: %v", err)
	}
}

// Column-reference operands are user-controlled filter expressions too. Both
// sides must be authorized; checking only the predicate's left column leaves
// the same leak reachable as `id: { gt: { col: "price" } }`.
func TestWhereColumnReferenceOperandAllowlist(t *testing.T) {
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{})
	if err := qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{Columns: []string{"id", "name"}},
	}); err != nil {
		t.Fatal(err)
	}

	_, err := qc.Compile([]byte(`
		query { products(where: { id: { gt: { col: "price" } } }) {
			id
		} }`), nil, "user", "")
	if err == nil {
		t.Fatal("expected compile error for disallowed column-reference operand, got nil")
	}
	if !strings.Contains(err.Error(), "price") ||
		!strings.Contains(err.Error(), "db column blocked") {
		t.Errorf("error should report the blocked operand column, got: %v", err)
	}
}

func TestWhereBlockedColumn(t *testing.T) {
	di := sdata.GetTestDBInfo()
	col, err := di.GetColumn("public", "users", "encrypted_password")
	if err != nil {
		t.Fatal(err)
	}
	col.Blocked = true
	blockedSchema, err := sdata.NewDBSchema(di, nil)
	if err != nil {
		t.Fatal(err)
	}

	qc, _ := qcode.NewCompiler(blockedSchema, qcode.Config{})
	_, err = qc.Compile([]byte(`
		query { users(where: { encrypted_password: { eq: "secret" } }) {
			id
		} }`), nil, "user", "")
	if err == nil {
		t.Fatal("expected compile error filtering by blocked column, got nil")
	}
	if !strings.Contains(err.Error(), "encrypted_password") ||
		!strings.Contains(err.Error(), "blocked") {
		t.Errorf("error should report the blocked column, got: %v", err)
	}
}

func TestFieldFilterRoleAllowlist(t *testing.T) {
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{})
	if err := qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{Columns: []string{"id", "name"}},
	}); err != nil {
		t.Fatal(err)
	}

	_, err := qc.Compile([]byte(`
		query { products {
			id(includeIf: { price: { gt: 100 } })
		} }`), nil, "user", "")
	if err == nil {
		t.Fatal("expected compile error filtering a field by disallowed column, got nil")
	}
	if !strings.Contains(err.Error(), "price") ||
		!strings.Contains(err.Error(), "db column blocked") {
		t.Errorf("error should report the blocked field-filter column, got: %v", err)
	}
}

// Developer-authored row filters are compiled at AddRole time and must remain
// independent of the columns a client may reference. The same column remains
// forbidden when it comes from the client's own WHERE argument.
func TestWhereRoleFilterExemptFromColumnAllowlist(t *testing.T) {
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{})
	if err := qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "name"},
			Filters: []string{`{ user_id: { eq: $user_id } }`},
		},
	}); err != nil {
		t.Fatalf("role filter may reference a column outside the client allowlist: %v", err)
	}

	q, err := qc.Compile([]byte(`query { products { id name } }`), nil, "user", "")
	if err != nil {
		t.Fatalf("role with columns and a row filter should compile: %v", err)
	}
	if q.Selects[0].Where.Exp == nil {
		t.Fatal("expected the role filter to be present in the compiled selector")
	}

	_, err = qc.Compile([]byte(`
		query { products(where: { user_id: { eq: 1 } }) { id } }`), nil, "user", "")
	if err == nil {
		t.Fatal("expected client-authored where on role-filter-only column to be rejected")
	}
	if !strings.Contains(err.Error(), "user_id") ||
		!strings.Contains(err.Error(), "db column blocked") {
		t.Errorf("error should report the blocked client column, got: %v", err)
	}
}

// Filter operators are fixed compiler syntax, not selectable DB functions.
// DisableFunctions therefore must not disable an otherwise authorized WHERE.
func TestWhereAllowedWithFunctionsDisabled(t *testing.T) {
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{})
	if err := qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns:          []string{"id", "price"},
			DisableFunctions: true,
		},
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := qc.Compile([]byte(`
		query { products(where: { price: { gt: 100 } }) { id } }`), nil, "user", ""); err != nil {
		t.Fatalf("fixed filter operators should not be treated as DB functions: %v", err)
	}
}
