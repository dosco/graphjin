package qcode_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

var dbs *sdata.DBSchema

func init() {
	var err error

	dbs, err = sdata.NewDBSchema(sdata.GetTestDBInfo(), nil)
	if err != nil {
		panic(err)
	}
}

func TestCompile1(t *testing.T) {
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{})
	err := qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "name"},
		},
	})
	if err != nil {
		t.Error(err)
		return
	}

	_, err = qc.Compile([]byte(`
	query { products(id: 15) {
			id
			name
		} }`), nil, "user", "")

	if err != nil {
		t.Fatal(err)
	}
}

func TestCompile2(t *testing.T) {
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{})
	err := qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"ID"},
		},
	})
	if err != nil {
		t.Error(err)
		return
	}

	_, err = qc.Compile([]byte(`
	query { product(id: $id) {
			id
			price	
		} }`), nil, "user", "")

	if err == nil {
		t.Fatal(errors.New("expected an error: 'products.price' blocked"))
	}
}

func TestCompile3(t *testing.T) {
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{})
	err := qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"ID"},
		},
	})
	if err != nil {
		t.Error(err)
		return
	}

	vars := json.RawMessage(`
		{ "data": { "name": "my_name", "description": "my_desc"  } }`)

	vars1 := make(map[string]json.RawMessage)
	if err := json.Unmarshal(vars, &vars1); err != nil {
		t.Error(err)
	}

	_, err = qc.Compile([]byte(`
	mutation {
		products(insert: $data) {
			id
			name
		}
	}`), vars1, "user", "")

	if err != nil {
		t.Fatal(err)
	}
}

func TestCompile4(t *testing.T) {
	gql := `mutation {
		users(insert: { email: $email, full_name: $full_name}) {
			id
		}
	}`

	vars := json.RawMessage(`{
		"email":     "reannagreenholt@orn.com",
		"full_name": "Flo Barton"
	}`)

	vars1 := make(map[string]json.RawMessage)
	if err := json.Unmarshal(vars, &vars1); err != nil {
		t.Error(err)
	}

	qc, _ := qcode.NewCompiler(dbs, qcode.Config{})
	_, err := qc.Compile([]byte(gql), vars1, "user", "")
	if err != nil {
		t.Fatal(err)
	}
}

// TestWhereFKColumnNotMisinterpretedAsRelationship verifies that filtering on a
// foreign key column (e.g. customer_id on purchases) uses a simple column filter,
// not a relationship join to the customers table.
func TestWhereFKColumnNotMisinterpretedAsRelationship(t *testing.T) {
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{})

	// purchases.customer_id is an FK to customers.id — filtering on it should
	// produce a column WHERE clause, not a nested EXISTS join to customers.
	_, err := qc.Compile([]byte(`
	query {
		purchases(where: { customer_id: { eq: 5 } }) {
			id
			quantity
		}
	}`), nil, "user", "")

	if err != nil {
		t.Fatalf("expected FK column filter to compile, got: %v", err)
	}
}

// TestWhereFKColumnProduct verifies product_id FK column filter on purchases.
func TestWhereFKColumnProduct(t *testing.T) {
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{})

	// purchases.product_id is an FK to products.id
	_, err := qc.Compile([]byte(`
	query {
		purchases(where: { product_id: { eq: 10 } }) {
			id
			sale_type
		}
	}`), nil, "user", "")

	if err != nil {
		t.Fatalf("expected FK column filter to compile, got: %v", err)
	}
}

// TestWhereNestedRelationshipStillWorks ensures genuine nested table where
// clauses continue to work after the FK column disambiguation fix.
func TestWhereNestedRelationshipStillWorks(t *testing.T) {
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{})

	// "users" here is NOT a column on products — it should still be treated
	// as a nested relationship filter (products → users via user_id FK).
	_, err := qc.Compile([]byte(`
	query {
		products(where: { users: { email: { eq: "test@test.com" } } }) {
			id
			name
		}
	}`), nil, "user", "")

	if err != nil {
		t.Fatalf("expected nested relationship filter to compile, got: %v", err)
	}
}

// TestWhereNestedFKColumnNotMisinterpreted verifies that filtering through a
// relationship using an FK column on the intermediate table works correctly.
// e.g. purchases → customers where customers.user_id = 5
// user_id is an FK on customers pointing to users — it must be treated as a
// column filter, not navigated further to the users table.
func TestWhereNestedFKColumnNotMisinterpreted(t *testing.T) {
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{})

	_, err := qc.Compile([]byte(`
	query {
		purchases(where: { customers: { user_id: { eq: 5 } } }) {
			id
			quantity
		}
	}`), nil, "user", "")

	if err != nil {
		t.Fatalf("expected nested FK column filter to compile, got: %v", err)
	}
}

func TestInvalidCompile1(t *testing.T) {
	qcompile, _ := qcode.NewCompiler(dbs, qcode.Config{})
	_, err := qcompile.Compile([]byte(`#`), nil, "user", "")

	if err == nil {
		t.Fatal(errors.New("expecting an error"))
	}
}

func TestInvalidCompile2(t *testing.T) {
	qcompile, _ := qcode.NewCompiler(dbs, qcode.Config{})
	_, err := qcompile.Compile([]byte(`{u(where:{not:0})}`), nil, "user", "")

	if err == nil {
		t.Fatal(errors.New("expecting an error"))
	}
}

func TestEmptyCompile(t *testing.T) {
	qcompile, _ := qcode.NewCompiler(dbs, qcode.Config{})
	_, err := qcompile.Compile([]byte(``), nil, "user", "")

	if err == nil {
		t.Fatal(errors.New("expecting an error"))
	}
}

func TestInvalidPostfixCompile(t *testing.T) {
	gql := `mutation 
updateThread {
  thread(update: $data, where: { slug: { eq: $slug } }) {
    slug
    title
    published
    createdAt : created_at
    totalVotes : cached_votes_total
    totalPosts : cached_posts_total
    vote : thread_vote(where: { user_id: { eq: $user_id } }) {
     id
    }
    topics {
      slug
      name
    }
	}
}
}}`
	qcompile, _ := qcode.NewCompiler(dbs, qcode.Config{})
	_, err := qcompile.Compile([]byte(gql), nil, "anon", "")

	if err == nil {
		t.Fatal(errors.New("expecting an error"))
	}
}

func TestFragmentsCompile1(t *testing.T) {
	gql := `
	fragment userFields1 on user {
		id
		email
	}

	query {
		users {
			...userFields2
	
			created_at
			...userFields1
		}
	}
	
	fragment userFields2 on user {
		full_name
		phone
	}
	`
	qcompile, _ := qcode.NewCompiler(dbs, qcode.Config{})
	_, err := qcompile.Compile([]byte(gql), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}
}

func TestFragmentsCompile2(t *testing.T) {
	gql := `
	query {
		users {
			...userFields2
	
			created_at
			...userFields1
		}
	}

	fragment userFields1 on user {
		id
		email
	}
	
	fragment userFields2 on user {
		full_name
		phone
	}`
	qcompile, _ := qcode.NewCompiler(dbs, qcode.Config{})
	_, err := qcompile.Compile([]byte(gql), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}
}

func TestFragmentsCompile3(t *testing.T) {
	gql := `
	fragment userFields1 on user {
		id
		email
	}
	
	fragment userFields2 on user {
		full_name
		phone
	}

	query {
		users {
			...userFields2
	
			created_at
			...userFields1
		}
	}

	`
	qcompile, _ := qcode.NewCompiler(dbs, qcode.Config{})
	_, err := qcompile.Compile([]byte(gql), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}
}

var gql = []byte(`
	{products(
		# returns only 30 items
		limit: 30,

		# starts from item 10, commented out for now
		# offset: 10,

		# orders the response items by highest price
		order_by: { price: desc },

		# no duplicate prices returned
		distinct: [ price ]

		# only items with an id >= 30 and < 30 are returned
		where: { id: { greater_or_equals: 20, lt: 28 } }) {
		id
		name
		price
	}}`)

var gqlWithFragments = []byte(`
fragment userFields1 on user {
	id
	email
	__typename
}

query {
	users {
		...userFields2

		created_at
		...userFields1
		__typename
	}
}

fragment userFields2 on user {
	full_name
	__typename
}`)

func BenchmarkQCompile(b *testing.B) {
	qcompile, _ := qcode.NewCompiler(dbs, qcode.Config{})

	b.ResetTimer()
	b.ReportAllocs()

	for n := 0; n < b.N; n++ {
		_, err := qcompile.Compile(gql, nil, "user", "")
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkQCompileP(b *testing.B) {
	qcompile, _ := qcode.NewCompiler(dbs, qcode.Config{})

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := qcompile.Compile(gql, nil, "user", "")
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

func TestClusteringKeysCursorPagination(t *testing.T) {
	// Set up a Snowflake schema with clustering keys on the products table
	sfSchema, err := sdata.GetTestSnowflakeSchema()
	if err != nil {
		t.Fatal(err)
	}

	qc, err := qcode.NewCompiler(sfSchema, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}
	err = qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "name", "price", "created_at", "user_id"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Cursor pagination query with no explicit order_by
	result, err := qc.Compile([]byte(`
		query {
			products(first: 20, after: $cursor) {
				id
				name
				price
			}
		}`), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}

	sel := result.Selects[0]

	// Expect ORDER BY: created_at (cluster key), user_id (cluster key), id (PK tie-breaker)
	if len(sel.OrderBy) < 3 {
		t.Fatalf("expected at least 3 ORDER BY columns, got %d: %v",
			len(sel.OrderBy), orderByNames(sel.OrderBy))
	}

	expectedOrder := []string{"created_at", "user_id", "id"}
	for i, expected := range expectedOrder {
		if sel.OrderBy[i].Col.Name != expected {
			t.Errorf("ORDER BY[%d]: expected %q, got %q (full: %v)",
				i, expected, sel.OrderBy[i].Col.Name, orderByNames(sel.OrderBy))
		}
	}
}

func TestClusteringKeysNotUsedWithExplicitOrderBy(t *testing.T) {
	sfSchema, err := sdata.GetTestSnowflakeSchema()
	if err != nil {
		t.Fatal(err)
	}

	qc, err := qcode.NewCompiler(sfSchema, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}
	err = qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "name", "price", "created_at", "user_id"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Cursor pagination with explicit order_by — clustering keys should NOT be injected
	result, err := qc.Compile([]byte(`
		query {
			products(first: 20, after: $cursor, order_by: { price: desc }) {
				id
				name
				price
			}
		}`), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}

	sel := result.Selects[0]

	// Expect ORDER BY: price (user-specified), id (PK tie-breaker) — no clustering cols injected
	if len(sel.OrderBy) != 2 {
		t.Fatalf("expected 2 ORDER BY columns, got %d: %v",
			len(sel.OrderBy), orderByNames(sel.OrderBy))
	}
	if sel.OrderBy[0].Col.Name != "price" {
		t.Errorf("ORDER BY[0]: expected %q, got %q", "price", sel.OrderBy[0].Col.Name)
	}
	if sel.OrderBy[1].Col.Name != "id" {
		t.Errorf("ORDER BY[1]: expected %q, got %q", "id", sel.OrderBy[1].Col.Name)
	}
}

func TestClusteringKeysNotUsedForPostgres(t *testing.T) {
	// The default test DB is postgres — clustering keys should be ignored
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{})
	_ = qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "name", "price", "created_at"},
		},
	})

	result, err := qc.Compile([]byte(`
		query {
			products(first: 20, after: $cursor) {
				id
				name
			}
		}`), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}

	sel := result.Selects[0]

	// For postgres, should only have PK tie-breaker
	if len(sel.OrderBy) != 1 {
		t.Fatalf("expected 1 ORDER BY column for postgres, got %d: %v",
			len(sel.OrderBy), orderByNames(sel.OrderBy))
	}
	if sel.OrderBy[0].Col.Name != "id" {
		t.Errorf("ORDER BY[0]: expected %q, got %q", "id", sel.OrderBy[0].Col.Name)
	}
}

func TestClusteringKeysNonexistentColumnSkipped(t *testing.T) {
	// Create a Snowflake DBInfo where clustering keys reference a column that
	// doesn't exist in the table — should gracefully skip those columns.
	di := sdata.GetTestSnowflakeDBInfo()
	for i := range di.Tables {
		if di.Tables[i].Name == "products" {
			di.Tables[i].ClusteringKeys = []string{"nonexistent_col", "created_at"}
		}
	}

	sfSchema, err := sdata.NewDBSchema(di, nil)
	if err != nil {
		t.Fatal(err)
	}

	qc, err := qcode.NewCompiler(sfSchema, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}
	_ = qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "name", "price", "created_at", "user_id"},
		},
	})

	result, err := qc.Compile([]byte(`
		query {
			products(first: 20, after: $cursor) {
				id
				name
			}
		}`), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}

	sel := result.Selects[0]

	// Expect: created_at (valid cluster key), id (PK tie-breaker)
	// nonexistent_col should be silently skipped
	if len(sel.OrderBy) != 2 {
		t.Fatalf("expected 2 ORDER BY columns, got %d: %v",
			len(sel.OrderBy), orderByNames(sel.OrderBy))
	}
	if sel.OrderBy[0].Col.Name != "created_at" {
		t.Errorf("ORDER BY[0]: expected %q, got %q", "created_at", sel.OrderBy[0].Col.Name)
	}
	if sel.OrderBy[1].Col.Name != "id" {
		t.Errorf("ORDER BY[1]: expected %q, got %q", "id", sel.OrderBy[1].Col.Name)
	}
}

func TestPartitionFilterInjected(t *testing.T) {
	// Table with partition key and default range of 30 days
	pSchema, err := sdata.GetTestPartitionedSchema()
	if err != nil {
		t.Fatal(err)
	}

	qc, err := qcode.NewCompiler(pSchema, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}
	_ = qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "name", "price", "created_at"},
		},
	})

	// Query without any filter on the partition column
	result, err := qc.Compile([]byte(`
		query {
			products {
				id
				name
			}
		}`), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}

	sel := result.Selects[0]

	// The WHERE clause should have a partition filter injected
	if sel.Where.Exp == nil {
		t.Fatal("expected WHERE clause to have partition filter, got nil")
	}

	// Should have no warnings (filter was injected, not just warned)
	if len(result.Warnings) > 0 {
		t.Errorf("expected no warnings when default range is set, got: %v", result.Warnings)
	}

	// Verify the injected filter references the partition column
	if !qcode.HasFilterOnColumn(sel.Where.Exp, "created_at") {
		t.Error("expected injected filter to reference partition column 'created_at'")
	}
}

func TestPartitionFilterNotInjectedWhenUserFilters(t *testing.T) {
	pSchema, err := sdata.GetTestPartitionedSchema()
	if err != nil {
		t.Fatal(err)
	}

	qc, err := qcode.NewCompiler(pSchema, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}
	_ = qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "name", "price", "created_at"},
		},
	})

	// Query WITH a filter on the partition column
	result, err := qc.Compile([]byte(`
		query {
			products(where: { created_at: { gte: $start_date } }) {
				id
				name
			}
		}`), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}

	// No warnings — user already filtered on the partition column
	if len(result.Warnings) > 0 {
		t.Errorf("expected no warnings when user filters on partition column, got: %v",
			result.Warnings)
	}
}

func TestPartitionWarningWhenNoFilter(t *testing.T) {
	// Table with partition key but NO default range (warn only)
	pSchema, err := sdata.GetTestPartitionedWarnOnlySchema()
	if err != nil {
		t.Fatal(err)
	}

	qc, err := qcode.NewCompiler(pSchema, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}
	_ = qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "name", "price", "created_at"},
		},
	})

	// Query without filter on partition column, no default range
	result, err := qc.Compile([]byte(`
		query {
			products {
				id
				name
			}
		}`), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}

	// Should have a warning
	if len(result.Warnings) == 0 {
		t.Fatal("expected a warning about missing partition filter")
	}

	found := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "partition column") && strings.Contains(w, "created_at") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning about partition column 'created_at', got: %v", result.Warnings)
	}
}

func TestSnowflakeAutoPartitionFilterInjected(t *testing.T) {
	// Snowflake schema: clustering keys auto-derive partition key with a
	// 60-day default range. Queries without an explicit filter on the
	// leading temporal clustering key get a time-range predicate injected.
	sfSchema, err := sdata.GetTestSnowflakeSchema()
	if err != nil {
		t.Fatal(err)
	}

	qc, err := qcode.NewCompiler(sfSchema, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}
	_ = qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "name", "price", "created_at", "user_id"},
		},
	})

	// Query WITHOUT filtering on the auto-derived partition column
	result, err := qc.Compile([]byte(`
		query {
			products {
				id
				name
			}
		}`), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}

	// Should auto-inject a filter on created_at — no warning, no full scan
	sel := result.Selects[0]
	if !qcode.HasFilterOnColumn(sel.Where.Exp, "created_at") {
		t.Fatal("expected auto-injected filter on partition column 'created_at'")
	}

	// No partition warning since the filter was injected
	for _, w := range result.Warnings {
		if strings.Contains(w, "partition") {
			t.Errorf("unexpected partition warning after auto-injection: %s", w)
		}
	}
}

func TestSnowflakeAutoPartitionNoWarningWhenFiltered(t *testing.T) {
	sfSchema, err := sdata.GetTestSnowflakeSchema()
	if err != nil {
		t.Fatal(err)
	}

	qc, err := qcode.NewCompiler(sfSchema, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}
	_ = qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "name", "price", "created_at", "user_id"},
		},
	})

	// Query WITH a filter on the auto-derived partition column
	result, err := qc.Compile([]byte(`
		query {
			products(where: { created_at: { gte: $start_date } }) {
				id
				name
			}
		}`), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Warnings) > 0 {
		t.Errorf("expected no warnings when filtering on partition column, got: %v", result.Warnings)
	}
}

func TestWarehouseOrderByColumnsNotProjected(t *testing.T) {
	// Snowflake: ORDER BY columns should NOT be added to BCols when there's
	// no cursor pagination, to avoid scanning extra columns in columnar storage.
	sfSchema, err := sdata.GetTestSnowflakeSchema()
	if err != nil {
		t.Fatal(err)
	}

	qc, err := qcode.NewCompiler(sfSchema, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}
	_ = qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "name", "price", "created_at", "user_id"},
		},
	})

	// ORDER BY price but no cursor pagination — price should NOT be in BCols
	result, err := qc.Compile([]byte(`
		query {
			products(order_by: { price: desc }) {
				id
				name
			}
		}`), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}

	sel := result.Selects[0]

	// BCols should only contain user-requested columns (id, name), not price
	for _, bc := range sel.BCols {
		if bc.Col.Name == "price" {
			t.Error("Snowflake: ORDER BY column 'price' should NOT be in BCols without cursor pagination")
		}
	}
}

func TestWarehouseOrderByProjectedWithCursor(t *testing.T) {
	// When cursor pagination IS active, ORDER BY columns MUST be in BCols
	// for LAST_VALUE extraction.
	sfSchema, err := sdata.GetTestSnowflakeSchema()
	if err != nil {
		t.Fatal(err)
	}

	qc, err := qcode.NewCompiler(sfSchema, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}
	_ = qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "name", "price", "created_at", "user_id"},
		},
	})

	// With cursor pagination, ORDER BY columns MUST be projected
	result, err := qc.Compile([]byte(`
		query {
			products(first: 20, after: $cursor, order_by: { price: desc }) {
				id
				name
			}
		}`), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}

	sel := result.Selects[0]

	found := false
	for _, bc := range sel.BCols {
		if bc.Col.Name == "price" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Snowflake with cursor: ORDER BY column 'price' SHOULD be in BCols for LAST_VALUE")
	}
}

func TestPostgresOrderByAlwaysProjected(t *testing.T) {
	// For non-warehouse databases, ORDER BY columns should still be in BCols
	// (existing behavior unchanged).
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{})
	_ = qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "name", "price"},
		},
	})

	result, err := qc.Compile([]byte(`
		query {
			products(order_by: { price: desc }) {
				id
				name
			}
		}`), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}

	sel := result.Selects[0]

	found := false
	for _, bc := range sel.BCols {
		if bc.Col.Name == "price" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Postgres: ORDER BY column 'price' SHOULD still be in BCols (no projection optimization)")
	}
}

func TestPartitionNoWarningForNonPartitionedTable(t *testing.T) {
	// Standard postgres schema — no partition keys
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{})
	_ = qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "name", "price"},
		},
	})

	result, err := qc.Compile([]byte(`
		query {
			products {
				id
				name
			}
		}`), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Warnings) > 0 {
		t.Errorf("expected no warnings for non-partitioned table, got: %v", result.Warnings)
	}
}

// --- Dangerous query detection tests (Snowflake) ---

func TestSnowflakeAggWithoutFilterAutoFixed(t *testing.T) {
	// Aggregation without explicit filter on a clustered Snowflake table.
	// The auto-derived partition filter (60-day range on created_at) is
	// injected by checkPartitionFilter, so checkDangerousQuery sees a
	// filter and does NOT warn about aggregation.
	sfSchema, err := sdata.GetTestSnowflakeSchema()
	if err != nil {
		t.Fatal(err)
	}

	qc, err := qcode.NewCompiler(sfSchema, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}
	_ = qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "name", "price", "created_at", "user_id"},
		},
	})

	result, err := qc.Compile([]byte(`
		query {
			products {
				count_id
			}
		}`), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}

	// Partition filter auto-injected on created_at
	sel := result.Selects[0]
	if !qcode.HasFilterOnColumn(sel.Where.Exp, "created_at") {
		t.Fatal("expected auto-injected partition filter on 'created_at'")
	}

	// No "aggregation without filter" warning since the partition filter was injected
	for _, w := range result.Warnings {
		if strings.Contains(w, "aggregation") && strings.Contains(w, "no WHERE filter") {
			t.Errorf("unexpected aggregation warning — partition filter should have been injected: %s", w)
		}
	}
}

func TestSnowflakeAggWithFilterNoWarning(t *testing.T) {
	sfSchema, err := sdata.GetTestSnowflakeSchema()
	if err != nil {
		t.Fatal(err)
	}

	qc, err := qcode.NewCompiler(sfSchema, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}
	_ = qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "name", "price", "created_at", "user_id"},
		},
	})

	// Aggregation WITH a clustering key filter — should NOT warn about aggregation
	result, err := qc.Compile([]byte(`
		query {
			products(where: { created_at: { gt: "2024-01-01" } }) {
				count_id
			}
		}`), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}

	for _, w := range result.Warnings {
		if strings.Contains(w, "aggregation") && strings.Contains(w, "no WHERE filter") {
			t.Errorf("unexpected aggregation warning when WHERE filter is present: %s", w)
		}
	}
}

func TestSnowflakeNonClusteringFilterAutoFixed(t *testing.T) {
	// User filters on 'name' (non-clustering), but the auto-derived partition
	// filter on 'created_at' (first clustering key) is injected automatically.
	// Since created_at IS a clustering key, no clustering warning fires.
	sfSchema, err := sdata.GetTestSnowflakeSchema()
	if err != nil {
		t.Fatal(err)
	}

	qc, err := qcode.NewCompiler(sfSchema, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}
	_ = qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "name", "price", "created_at", "user_id"},
		},
	})

	result, err := qc.Compile([]byte(`
		query {
			products(where: { name: { eq: "Widget" } }) {
				id
				name
			}
		}`), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}

	// Auto-injected partition filter covers the clustering key
	sel := result.Selects[0]
	if !qcode.HasFilterOnColumn(sel.Where.Exp, "created_at") {
		t.Fatal("expected auto-injected partition filter on 'created_at'")
	}

	// No clustering warning because created_at (a clustering key) has a filter
	for _, w := range result.Warnings {
		if strings.Contains(w, "clustering") {
			t.Errorf("unexpected clustering warning — partition filter covers clustering key: %s", w)
		}
	}
}

func TestSnowflakeClusteringFilterNoWarning(t *testing.T) {
	sfSchema, err := sdata.GetTestSnowflakeSchema()
	if err != nil {
		t.Fatal(err)
	}

	qc, err := qcode.NewCompiler(sfSchema, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}
	_ = qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "name", "price", "created_at", "user_id"},
		},
	})

	// Filter on 'created_at' which IS a clustering key — no clustering warning
	result, err := qc.Compile([]byte(`
		query {
			products(where: { created_at: { gt: "2024-01-01" } }) {
				id
				name
			}
		}`), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}

	for _, w := range result.Warnings {
		if strings.Contains(w, "clustering") {
			t.Errorf("unexpected clustering warning when filtering on clustering key: %s", w)
		}
	}
}

func TestSnowflakeNoFilterAutoFixed(t *testing.T) {
	// No explicit filter on a Snowflake clustered table. The auto-derived
	// partition filter prevents a full scan — no dangerous query warnings.
	sfSchema, err := sdata.GetTestSnowflakeSchema()
	if err != nil {
		t.Fatal(err)
	}

	qc, err := qcode.NewCompiler(sfSchema, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}
	_ = qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "name", "price", "created_at", "user_id"},
		},
	})

	result, err := qc.Compile([]byte(`
		query {
			products {
				id
				name
			}
		}`), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}

	// Partition filter auto-injected
	sel := result.Selects[0]
	if !qcode.HasFilterOnColumn(sel.Where.Exp, "created_at") {
		t.Fatal("expected auto-injected partition filter on 'created_at'")
	}

	// No full scan or clustering warnings
	for _, w := range result.Warnings {
		if strings.Contains(w, "full table scan") || strings.Contains(w, "clustering") {
			t.Errorf("unexpected dangerous query warning after auto-fix: %s", w)
		}
	}
}

func TestPostgresNoDangerousQueryWarnings(t *testing.T) {
	// Postgres should never get dangerous query warnings
	qc, _ := qcode.NewCompiler(dbs, qcode.Config{})
	_ = qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "name", "price"},
		},
	})

	result, err := qc.Compile([]byte(`
		query {
			products {
				count_id
			}
		}`), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}

	for _, w := range result.Warnings {
		if strings.Contains(w, "aggregation") || strings.Contains(w, "clustering") || strings.Contains(w, "full table scan") {
			t.Errorf("Postgres should not get dangerous query warnings, got: %s", w)
		}
	}
}

func orderByNames(obs []qcode.OrderBy) []string {
	names := make([]string, len(obs))
	for i, ob := range obs {
		names[i] = ob.Col.Name
	}
	return names
}

func BenchmarkQCompileFragment(b *testing.B) {
	qcompile, _ := qcode.NewCompiler(dbs, qcode.Config{})

	b.ResetTimer()
	b.ReportAllocs()
	for n := 0; n < b.N; n++ {
		_, err := qcompile.Compile(gqlWithFragments, nil, "user", "")
		if err != nil {
			b.Fatal(err)
		}
	}
}
