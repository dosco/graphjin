package qcode_test

import (
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

// Authorization tests for ORDER BY / DISTINCT ON: ordering (or deduping)
// by a column reveals its values through row order, so order_by must go
// through the same role checks as SELECT-list fields (validateField).
// These mirror the blocked-column / blocked-function example tests in
// tests/query_test.go at the compiler level.

// TestOrderByRoleAllowlist: a role restricted to a column allowlist must
// not be able to order by a column outside it.
func TestOrderByRoleAllowlist(t *testing.T) {
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{})
	if err := qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{Columns: []string{"id", "name"}},
	}); err != nil {
		t.Fatal(err)
	}

	_, err := qc.Compile([]byte(`
		query { products(order_by: { price: desc }) {
			id
		} }`), nil, "user", "")
	if err == nil {
		t.Fatal("expected compile error ordering by disallowed column, got nil")
	}
	if !strings.Contains(err.Error(), "price") ||
		!strings.Contains(err.Error(), "db column blocked") {
		t.Errorf("error should report the blocked column, got: %v", err)
	}

	// Sanity: ordering by a column inside the allowlist still compiles.
	q, err := qc.Compile([]byte(`
		query { products(order_by: { name: asc }) {
			id
		} }`), nil, "user", "")
	if err != nil {
		t.Fatalf("order_by on allowed column should compile: %v", err)
	}
	if len(q.Selects[0].OrderBy) != 1 || q.Selects[0].OrderBy[0].Col.Name != "name" {
		t.Errorf("expected one order_by entry on name, got %+v", q.Selects[0].OrderBy)
	}
}

// TestOrderByAggregateFuncsDisabled: a role with DisableFunctions must
// not be able to order by an aggregate (sum_price), even when the
// selection itself contains no aggregate field.
func TestOrderByAggregateFuncsDisabled(t *testing.T) {
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{})
	if err := qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{DisableFunctions: true},
	}); err != nil {
		t.Fatal(err)
	}

	_, err := qc.Compile([]byte(`
		query { products(order_by: { sum_price: desc }) {
			id
		} }`), nil, "user", "")
	if err == nil {
		t.Fatal("expected compile error ordering by aggregate with functions disabled, got nil")
	}
	if !strings.Contains(err.Error(), "all db functions blocked") {
		t.Errorf("error should report functions blocked, got: %v", err)
	}
}

// TestOrderByAggregateColumnAllowlist: ordering by sum_<col> where <col>
// is outside the role's allowlist must fail, matching how a sum_<col>
// SELECT field is rejected by validateField.
func TestOrderByAggregateColumnAllowlist(t *testing.T) {
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{})
	if err := qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{Columns: []string{"id", "name"}},
	}); err != nil {
		t.Fatal(err)
	}

	_, err := qc.Compile([]byte(`
		query { products(order_by: { sum_price: desc }) {
			id
		} }`), nil, "user", "")
	if err == nil {
		t.Fatal("expected compile error ordering by aggregate over disallowed column, got nil")
	}
	if !strings.Contains(err.Error(), "price") ||
		!strings.Contains(err.Error(), "db column blocked") {
		t.Errorf("error should report the blocked column, got: %v", err)
	}
}

// TestOrderByBlockedColumn: a config-level blocked column (DBColumn.Blocked,
// the flag compileChildColumns enforces for SELECT fields) must be
// rejected in order_by for every role, including unrestricted ones.
func TestOrderByBlockedColumn(t *testing.T) {
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
	// No AddRole: the role is unrestricted, so only the Blocked flag can
	// reject this.
	_, err = qc.Compile([]byte(`
		query { users(order_by: { encrypted_password: asc }) {
			id
		} }`), nil, "user", "")
	if err == nil {
		t.Fatal("expected compile error ordering by blocked column, got nil")
	}
	if !strings.Contains(err.Error(), "encrypted_password") ||
		!strings.Contains(err.Error(), "blocked") {
		t.Errorf("error should report the blocked column, got: %v", err)
	}

	// Sanity: the same query on the stock schema (no Blocked flag)
	// compiles, so the rejection above is due to Blocked alone.
	qc2, _ := qcode.NewCompiler(dbs, qcode.Config{})
	if _, err := qc2.Compile([]byte(`
		query { users(order_by: { encrypted_password: asc }) {
			id
		} }`), nil, "user", ""); err != nil {
		t.Fatalf("unblocked schema should compile: %v", err)
	}
}

// TestOrderByNestedRelationAllowlist: ordering a parent selection by a
// related table's column (order_by: { users: { email: asc } }) must be
// checked against the related table's role config — ordering products
// by users.email leaks email ordering just like selecting it would.
func TestOrderByNestedRelationAllowlist(t *testing.T) {
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{})
	if err := qc.AddRole("user", "public", "users", qcode.TRConfig{
		Query: qcode.QueryConfig{Columns: []string{"id", "full_name"}},
	}); err != nil {
		t.Fatal(err)
	}

	_, err := qc.Compile([]byte(`
		query { products(order_by: { users: { email: asc } }) {
			id
		} }`), nil, "user", "")
	if err == nil {
		t.Fatal("expected compile error ordering by disallowed related column, got nil")
	}
	if !strings.Contains(err.Error(), "email") ||
		!strings.Contains(err.Error(), "db column blocked") {
		t.Errorf("error should report the blocked related column, got: %v", err)
	}

	// Sanity: a related column inside that table's allowlist compiles.
	if _, err := qc.Compile([]byte(`
		query { products(order_by: { users: { full_name: asc } }) {
			id
		} }`), nil, "user", ""); err != nil {
		t.Fatalf("order_by on allowed related column should compile: %v", err)
	}
}

// TestOrderByConfigVariantExemptFromAllowlist: named order_by variants
// from the table config (order_by: $var) are dev-authored, and every
// variant compiles into the SQL no matter which one the client's
// variable selects — so the role allowlist does not apply to them.
func TestOrderByConfigVariantExemptFromAllowlist(t *testing.T) {
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{
		TConfig: map[string]qcode.TConfig{
			"publicproducts": {OrderBy: map[string][][2]string{
				"price_ord": {{"price", "desc"}},
			}},
		},
	})
	if err := qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{Columns: []string{"id", "name"}},
	}); err != nil {
		t.Fatal(err)
	}

	q, err := qc.Compile([]byte(`
		query { products(order_by: $order) {
			id
		} }`), nil, "user", "")
	if err != nil {
		t.Fatalf("config-defined order_by variant should be exempt from the role allowlist: %v", err)
	}
	sel := q.Selects[0]
	if len(sel.OrderBy) != 1 || sel.OrderBy[0].KeyVar == "" {
		t.Fatalf("expected one config-variant (KeyVar) order_by entry, got %+v", sel.OrderBy)
	}
	if sel.OrderBy[0].Col.Name != "price" {
		t.Errorf("variant column = %q, want price", sel.OrderBy[0].Col.Name)
	}
}

// TestOrderByConfigVariantBlockedColumn: the config-level Blocked flag
// is absolute — it wins even over a dev-authored order_by variant that
// names the column.
func TestOrderByConfigVariantBlockedColumn(t *testing.T) {
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

	qc, _ := qcode.NewCompiler(blockedSchema, qcode.Config{
		TConfig: map[string]qcode.TConfig{
			"publicusers": {OrderBy: map[string][][2]string{
				"secret_ord": {{"encrypted_password", "asc"}},
			}},
		},
	})

	_, err = qc.Compile([]byte(`
		query { users(order_by: $order) {
			id
		} }`), nil, "user", "")
	if err == nil {
		t.Fatal("expected compile error for blocked column in config order_by variant, got nil")
	}
	if !strings.Contains(err.Error(), "encrypted_password") ||
		!strings.Contains(err.Error(), "blocked") {
		t.Errorf("error should report the blocked column, got: %v", err)
	}
}

// TestDistinctOnRoleAllowlist: distinct columns dedupe (and on some
// dialects order) rows by the named column, so they go through the same
// allowlist check as order_by.
func TestDistinctOnRoleAllowlist(t *testing.T) {
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{})
	if err := qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{Columns: []string{"id", "name"}},
	}); err != nil {
		t.Fatal(err)
	}

	_, err := qc.Compile([]byte(`
		query { products(distinct: [price]) {
			id
		} }`), nil, "user", "")
	if err == nil {
		t.Fatal("expected compile error for distinct on disallowed column, got nil")
	}
	if !strings.Contains(err.Error(), "price") ||
		!strings.Contains(err.Error(), "db column blocked") {
		t.Errorf("error should report the blocked column, got: %v", err)
	}

	// Sanity: distinct on an allowed column compiles.
	if _, err := qc.Compile([]byte(`
		query { products(distinct: [name]) {
			id
		} }`), nil, "user", ""); err != nil {
		t.Fatalf("distinct on allowed column should compile: %v", err)
	}
}

// TestOrderByAggregateDisabledCompilerWide: qcode.Config.DisableAgg
// rejects aggregate SELECT fields via isFunction; ordering by an
// aggregate must not slip past the same switch.
func TestOrderByAggregateDisabledCompilerWide(t *testing.T) {
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{DisableAgg: true})

	_, err := qc.Compile([]byte(`
		query { products(order_by: { sum_price: desc }) {
			id
		} }`), nil, "user", "")
	if err == nil {
		t.Fatal("expected compile error ordering by aggregate with DisableAgg, got nil")
	}
	if !strings.Contains(err.Error(), "disabled") {
		t.Errorf("error should report aggregation disabled, got: %v", err)
	}
}

// TestOrderByCursorPKExempt: cursor pagination appends the primary key
// to ORDER BY (orderByIDCol) after validateOrderBy runs. That entry is
// compiler-generated, not user-driven, so a role whose allowlist omits
// the PK must still be able to paginate.
func TestOrderByCursorPKExempt(t *testing.T) {
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{})
	if err := qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{Columns: []string{"name"}},
	}); err != nil {
		t.Fatal(err)
	}

	q, err := qc.Compile([]byte(`
		query { products(first: 20, after: $cursor, order_by: { name: asc }) {
			name
		} }`), nil, "user", "")
	if err != nil {
		t.Fatalf("cursor pagination must work when the PK is outside the allowlist: %v", err)
	}

	var sawPK bool
	for _, ob := range q.Selects[0].OrderBy {
		if ob.Col.Name == "id" {
			sawPK = true
		}
	}
	if !sawPK {
		t.Error("expected the cursor tie-breaker PK in OrderBy")
	}
}

// TestOrderByMatchesFieldAuthorization pins the invariant the whole pass
// exists to hold: ordering by a column is allowed exactly when selecting
// it is. columnAllowed keys off qc.SType, so this must hold for mutation
// selections (which consult update.cols) as well as plain queries —
// otherwise order_by would be either a leak or a false rejection.
func TestOrderByMatchesFieldAuthorization(t *testing.T) {
	for _, tc := range []struct {
		name      string
		role      qcode.TRConfig
		selectGQL string
		orderGQL  string
	}{
		{
			name: "query/query.cols",
			role: qcode.TRConfig{
				Query: qcode.QueryConfig{Columns: []string{"id", "name"}},
			},
			selectGQL: `query { products { price } }`,
			orderGQL:  `query { products(order_by: { price: desc }) { id } }`,
		},
		{
			name: "mutation/update.cols",
			role: qcode.TRConfig{
				Update: qcode.UpdateConfig{Columns: []string{"id", "name"}},
			},
			selectGQL: `mutation { users(update: $data, where: { id: { eq: 1 } }) {
				id
				products { price }
			} }`,
			orderGQL: `mutation { users(update: $data, where: { id: { eq: 1 } }) {
				id
				products(order_by: { price: desc }) { id }
			} }`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compile := func(gql string) error {
				qc, _ := qcode.NewCompiler(dbs, qcode.Config{})
				if err := qc.AddRole("user", "public", "products", tc.role); err != nil {
					t.Fatal(err)
				}
				_, err := qc.Compile([]byte(gql), nil, "user", "")
				return err
			}
			errSel := compile(tc.selectGQL)
			errOrd := compile(tc.orderGQL)

			if errSel == nil && errOrd != nil {
				t.Errorf("order_by rejected a column that select allows: %v", errOrd)
			}
			if errSel != nil && errOrd == nil {
				t.Errorf("order_by allowed a column that select rejects (%v) — the leak this pass closes", errSel)
			}
		})
	}
}

// TestOrderByAliasStillWorks: ordering by a SELECT-list alias of a field
// the role can read keeps working — the alias path is authorized through
// the compiled field it points at, not re-checked as a column.
func TestOrderByAliasStillWorks(t *testing.T) {
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{})
	if err := qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{Columns: []string{"id", "price"}},
	}); err != nil {
		t.Fatal(err)
	}

	q, err := qc.Compile([]byte(`
		query { products(distinct: [id], order_by: { revenue: desc }) {
			id
			revenue: sum(expr: { mul: [{ col: "price" }, { num: 2 }] })
		} }`), nil, "user", "")
	if err != nil {
		t.Fatalf("alias order_by should compile: %v", err)
	}
	sel := q.Selects[0]
	if len(sel.OrderBy) != 1 || sel.OrderBy[0].Alias != "revenue" {
		t.Errorf("expected alias order_by entry, got %+v", sel.OrderBy)
	}
}
